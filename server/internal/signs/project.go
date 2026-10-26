package signs

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/TheAmbitiousCorry/trails/internal/content"
)

type signRow struct {
	ID, ParentID, Title string
}

type entryRow struct {
	ID, SignID, Kind, Title, ReadWhen, Tier string
	Tags                                    []string
	Words                                   int
	Text                                    string
	terms                                   map[string]int
}

// project is the whole tree in memory; a sign is computed from it, never stored.
type project struct {
	signs   map[string]*signRow
	kids    map[string][]string
	bySign  map[string][]*entryRow
	entries []*entryRow
	df      map[string]int
}

// load skips dormant entries: they are off signs by definition.
func load(ctx context.Context, db *sql.DB) (*project, error) {
	p := &project{signs: map[string]*signRow{}, kids: map[string][]string{}, bySign: map[string][]*entryRow{}, df: map[string]int{}}
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(parent_id, ''), title FROM signs ORDER BY position, title, id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s signRow
		if err := rows.Scan(&s.ID, &s.ParentID, &s.Title); err != nil {
			rows.Close()
			return nil, err
		}
		p.signs[s.ID] = &s
		if s.ParentID != "" {
			p.kids[s.ParentID] = append(p.kids[s.ParentID], s.ID)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	rows, err = db.QueryContext(ctx, `SELECT id, sign_id, kind, title, read_when, tier, tags_json, body_json FROM entries WHERE tier != 'dormant' ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e entryRow
		var tags, body string
		if err := rows.Scan(&e.ID, &e.SignID, &e.Kind, &e.Title, &e.ReadWhen, &e.Tier, &tags, &body); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(tags), &e.Tags)
		e.Text = content.PlainText(json.RawMessage(body))
		e.Words = len(strings.Fields(e.Text))
		e.terms = countTerms(e.Text)
		for t := range e.terms {
			p.df[t]++
		}
		p.entries = append(p.entries, &e)
		p.bySign[e.SignID] = append(p.bySign[e.SignID], &e)
	}
	return p, rows.Err()
}

// subtree returns the sign's own entries and those of every descendant sign.
func (p *project) subtree(signID string) []*entryRow {
	out := append([]*entryRow(nil), p.bySign[signID]...)
	for _, k := range p.kids[signID] {
		out = append(out, p.subtree(k)...)
	}
	return out
}

func (p *project) inSubtree(entrySign, root string) bool {
	for id := entrySign; id != ""; id = p.parentOf(id) {
		if id == root {
			return true
		}
	}
	return false
}

func (p *project) parentOf(id string) string {
	if s := p.signs[id]; s != nil {
		return s.ParentID
	}
	return ""
}

var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`the and for are but not you all any can had her was one our out has have this that with from they will would there their what about which when your said each she how its into than then them these some could other more also only over such use used using not does did been being were where while should must may per via`) {
		stop[w] = true
	}
}

func countTerms(text string) map[string]int {
	m := map[string]int{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) >= 3 && !stop[w] {
			m[w]++
		}
	}
	return m
}

// topTerms ranks terms by tf-idf against every entry in the project.
func (p *project) topTerms(entries []*entryRow, n int) []string {
	counts := map[string]int{}
	total := 0
	for _, e := range entries {
		for t, c := range e.terms {
			counts[t] += c
			total += c
		}
	}
	if total == 0 {
		return nil
	}
	type scored struct {
		term  string
		score float64
	}
	var all []scored
	N := float64(len(p.entries))
	for t, c := range counts {
		idf := math.Log((N+1)/float64(p.df[t]+1)) + 1
		all = append(all, scored{t, float64(c) / float64(total) * idf})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].term < all[j].term
	})
	var out []string
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, all[i].term)
	}
	return out
}

// scent merges the tags found in entries with their top three terms.
func (p *project) scent(entries []*entryRow) []string {
	freq := map[string]int{}
	for _, e := range entries {
		for _, t := range e.Tags {
			freq[t]++
		}
	}
	tags := make([]string, 0, len(freq))
	for t := range freq {
		tags = append(tags, t)
	}
	sort.Slice(tags, func(i, j int) bool {
		if freq[tags[i]] != freq[tags[j]] {
			return freq[tags[i]] > freq[tags[j]]
		}
		return tags[i] < tags[j]
	})
	seen := map[string]bool{}
	var out []string
	for _, t := range append(tags, p.topTerms(entries, 3)...) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func hasTag(tags []string, t string) bool {
	for _, x := range tags {
		if x == t {
			return true
		}
	}
	return false
}
