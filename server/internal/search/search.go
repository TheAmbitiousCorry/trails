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

// SelfScore is the bm25 an entry earns when its own title, read_when and first paragraph are the query.
func SelfScore(ctx context.Context, db DBTX, entryID string) (float64, error) {
	q, err := EntryQuery(ctx, db, entryID)
	if err != nil {
		return 0, err
	}
	hits, err := Rank(ctx, db, q, "")
	if err != nil {
		return 0, err
	}
	for _, h := range hits {
		if h.EntryID == entryID {
			return h.Score, nil
		}
	}
	return 0, nil
}

// minSelfScore is the self score under which the corpus cannot say how good a match is.
const minSelfScore = 1e-3

// Query returns up to limit hits whose bm25 score, as a fraction of that entry's score against its own text, reaches search_threshold.
// miss is true when none does. A self score under minSelfScore keeps the hit.
// Below search_min_corpus live entries bm25 gives common terms no weight, so any FTS match is a hit and the threshold is skipped.
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
	small := float64(live) < minCorpus
	for _, h := range all {
		if small {
			hits = append(hits, h)
			continue
		}
		self, err := SelfScore(ctx, db, h.EntryID)
		if err != nil {
			return nil, false, err
		}
		if self < minSelfScore || h.Score/self >= threshold {
			hits = append(hits, h)
		}
	}
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, len(hits) == 0, nil
}
