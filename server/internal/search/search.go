// Package search ranks entries with FTS5 bm25 adjusted by trust tier, and keeps the index in step with the write path.
package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/content"
)

// DBTX is satisfied by *sql.DB and *sql.Tx, so the write path can refresh the index inside its own transaction.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Hit is one ranked entry. Score is the bm25 relevance before the tier factor and Rank is what the order uses.
type Hit struct {
	EntryID  string   `json:"id"`
	Title    string   `json:"title"`
	ReadWhen string   `json:"read_when"`
	Tier     string   `json:"tier"`
	Kind     string   `json:"kind"`
	SignPath []string `json:"sign_path"`
	Score    float64  `json:"-"`
	Rank     float64  `json:"-"`
	signID   string
}

var defaults = map[string]float64{
	"search_threshold":     0.5,
	"duplicate_threshold":  0.8,
	"paraphrase_threshold": 0.5,
	"search_min_corpus":    20,
	"open_gap_cap":         20,
	"dismiss_cap":          3,
}

// Setting reads a numeric threshold from settings, falling back to the migration's default when the row is missing or malformed.
func Setting(ctx context.Context, db DBTX, key string) (float64, error) {
	var v sql.NullString
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if f, convErr := strconv.ParseFloat(strings.TrimSpace(v.String), 64); convErr == nil {
		return f, nil
	}
	return defaults[key], nil
}

// Refresh rebuilds the entry's FTS row from its title, read_when plus aliases (weight A) and plain-text body (weight B).
func Refresh(ctx context.Context, db DBTX, entryID string) error {
	var title, readWhen, body string
	err := db.QueryRowContext(ctx, `SELECT title, read_when, body_json FROM entries WHERE id = ?`, entryID).Scan(&title, &readWhen, &body)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = db.ExecContext(ctx, `DELETE FROM entries_fts WHERE entry_id = ?`, entryID)
		return err
	}
	if err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, `SELECT read_when FROM entry_aliases WHERE entry_id = ? ORDER BY rowid`, entryID)
	if err != nil {
		return err
	}
	all := []string{readWhen}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			rows.Close()
			return err
		}
		all = append(all, a)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM entries_fts WHERE entry_id = ?`, entryID); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO entries_fts (entry_id, title, read_when_all, body_text) VALUES (?, ?, ?, ?)`,
		entryID, title, strings.Join(all, "\n"), content.PlainText(json.RawMessage(body)))
	return err
}

// Reindex refreshes every entry that has no FTS row, which covers the seeded entry and databases migrated from before search existed.
func Reindex(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT id FROM entries WHERE id NOT IN (SELECT entry_id FROM entries_fts)`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := Refresh(ctx, db, id); err != nil {
			return err
		}
	}
	return nil
}

// EntryQuery is the text a duplicate check searches for an entry: its title, read_when and first body paragraph.
func EntryQuery(ctx context.Context, db DBTX, entryID string) (string, error) {
	var title, readWhen, body string
	err := db.QueryRowContext(ctx, `SELECT title, read_when, body_json FROM entries WHERE id = ?`, entryID).Scan(&title, &readWhen, &body)
	if err != nil {
		return "", err
	}
	return DuplicateQuery(title, readWhen, content.PlainText(json.RawMessage(body))), nil
}

// DuplicateQuery joins title, read_when and the first paragraph of plain text into one query.
func DuplicateQuery(title, readWhen, plain string) string {
	first, _, _ := strings.Cut(plain, "\n")
	return title + " " + readWhen + " " + first
}

var wordRe = regexp.MustCompile(`[\p{L}\p{N}]+`)

const maxTerms = 64

// matchExpr turns free text into an OR of quoted terms, so no character of the input is read as FTS5 syntax.
func matchExpr(q string) string {
	seen := map[string]bool{}
	var terms []string
	for _, w := range wordRe.FindAllString(strings.ToLower(q), -1) {
		if !seen[w] && len(terms) < maxTerms {
			seen[w] = true
			terms = append(terms, `"`+w+`"`)
		}
	}
	return strings.Join(terms, " OR ")
}

// tierFactor makes verified beat provisional beat stale when the text matches equally well.
var tierFactor = map[string]float64{"verified": 1, "provisional": 0.8, "stale": 0.5}

const taskTypeBoost = 1.25

// Rank returns every live entry matching q, best first, with no threshold applied. Dormant and superseded entries are excluded.
// An entry tagged with taskType is boosted; nothing is filtered by it.
func Rank(ctx context.Context, db DBTX, q, taskType string) ([]Hit, error) {
	expr := matchExpr(q)
	if expr == "" {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT e.id, e.title, e.read_when, e.tier, e.kind, e.tags_json, e.sign_id, -bm25(entries_fts, 0, 2, 2, 1)
		FROM entries_fts JOIN entries e ON e.id = entries_fts.entry_id
		WHERE entries_fts MATCH ? AND e.tier != 'dormant' AND e.superseded_by IS NULL`, expr)
	if err != nil {
		return nil, err
	}
	var hits []Hit
	for rows.Next() {
		var h Hit
		var tags string
		if err := rows.Scan(&h.EntryID, &h.Title, &h.ReadWhen, &h.Tier, &h.Kind, &tags, &h.signID, &h.Score); err != nil {
			rows.Close()
			return nil, err
		}
		h.Rank = h.Score
		if f, ok := tierFactor[h.Tier]; ok {
			h.Rank *= f
		}
		if taskType != "" {
			var tl []string
			if json.Unmarshal([]byte(tags), &tl) == nil {
				for _, t := range tl {
					if t == taskType {
						h.Rank *= taskTypeBoost
						break
					}
				}
			}
		}
		hits = append(hits, h)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Rank != hits[j].Rank {
			return hits[i].Rank > hits[j].Rank
		}
		return hits[i].EntryID < hits[j].EntryID
	})
	for i := range hits {
		if hits[i].SignPath, err = signPath(ctx, db, hits[i].signID); err != nil {
			return nil, err
		}
	}
	return hits, nil
}

// signPath returns the sign titles from the root down to signID; a cycle or a missing parent ends the walk.
func signPath(ctx context.Context, db DBTX, signID string) ([]string, error) {
	var path []string
	seen := map[string]bool{}
	for cur := signID; cur != "" && !seen[cur]; {
		seen[cur] = true
		var title string
		var parent sql.NullString
		err := db.QueryRowContext(ctx, `SELECT title, parent_id FROM signs WHERE id = ?`, cur).Scan(&title, &parent)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, err
		}
		path = append([]string{title}, path...)
		cur = parent.String
	}
	return path, nil
}

// stopwords are the question words and function words a query carries that name nothing an entry is about.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields("a an and are as at be but by can did do does for from has have how i in is it its me my of on or our should that the their this to was we what when where which who why will with would you your") {
		stopwords[w] = true
	}
}

// ContentTerms are the query's distinct words of three letters or more that are not stopwords, lowercased.
func ContentTerms(q string) []string {
	seen := map[string]bool{}
	var terms []string
	for _, w := range wordRe.FindAllString(strings.ToLower(q), -1) {
		if len(w) < 3 || stopwords[w] || seen[w] || len(terms) >= maxTerms {
			continue
		}
		seen[w] = true
		terms = append(terms, w)
	}
	return terms
}

// coverage maps each live entry matching any content term of q to the share of those terms it contains.
func coverage(ctx context.Context, db DBTX, q string) (map[string]float64, error) {
	terms := ContentTerms(q)
	counts := map[string]int{}
	for _, t := range terms {
		rows, err := db.QueryContext(ctx, `SELECT entry_id FROM entries_fts WHERE entries_fts MATCH ?`, `"`+t+`"`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			counts[id]++
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	out := make(map[string]float64, len(counts))
	for id, n := range counts {
		out[id] = float64(n) / float64(len(terms))
	}
	return out, nil
}

// Query returns up to limit hits that contain at least search_threshold of the query's content terms (the words left after stopwords
// and words under three letters), best first by bm25. miss is true when none does. bm25 alone cannot tell a miss: one shared noun in
// a corpus of a hundred entries scores like a real hit, while a two-word question against a long entry scores like noise.
// A query with no content terms is matched on its raw words and never misses while something matches.
// Below search_min_corpus live entries any FTS match is a hit and the threshold is skipped.
func Query(ctx context.Context, db DBTX, q, taskType string, limit int) (hits []Hit, miss bool, err error) {
	all, err := Rank(ctx, db, q, taskType)
	if err != nil {
		return nil, false, err
	}
	threshold, err := Setting(ctx, db, "search_threshold")
	if err != nil {
		return nil, false, err
	}
	var live int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE tier != 'dormant' AND superseded_by IS NULL`).Scan(&live); err != nil {
		return nil, false, err
	}
	minCorpus, err := Setting(ctx, db, "search_min_corpus")
	if err != nil {
		return nil, false, err
	}
	small := float64(live) < minCorpus || len(ContentTerms(q)) == 0
	var cover map[string]float64
	if !small {
		if cover, err = coverage(ctx, db, q); err != nil {
			return nil, false, err
		}
	}
	for _, h := range all {
		if small || cover[h.EntryID] >= threshold {
			hits = append(hits, h)
		}
	}
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, len(hits) == 0, nil
}
