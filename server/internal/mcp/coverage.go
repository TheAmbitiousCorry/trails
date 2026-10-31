package mcp

import (
	"context"
	"database/sql"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/search"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
	"github.com/TheAmbitiousCorry/trails/internal/view"
)

type coverageIn struct {
	SessionID string `json:"session_id" jsonschema:"the id session_start returned"`
	Within    string `json:"within,omitempty" jsonschema:"a sign id: keep only items whose sign lies under it"`
	Limit     int    `json:"limit,omitempty" jsonschema:"items per list; default 10, max 25"`
}

func (c coverageIn) Call() trail.Call { return trail.Call{SessionID: c.SessionID, Target: c.Within} }

// Every item's sign is the one to pass as parent_sign_id when writing for it.
type coverageGap struct {
	ID      int64  `json:"id"`
	Query   string `json:"query"`
	Sign    string `json:"sign"`
	AgeDays int    `json:"age_days"`
}

type coverageStale struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Sign          string   `json:"sign"`
	SignPath      []string `json:"sign_path"`
	FailedAnchors []string `json:"failed_anchors"`
}

type coverageOffTrail struct {
	Path string `json:"path"`
	// Count is the number of sessions that read the path natively in the window.
	Count int    `json:"count"`
	Sign  string `json:"sign"`
}

type coverageOut struct {
	Gaps     []coverageGap      `json:"gaps"`
	Stale    []coverageStale    `json:"stale"`
	OffTrail []coverageOffTrail `json:"off_trail"`
	Lines    int                `json:"lines"`
}

// signTree is the sign hierarchy, loaded once per call.
type signTree struct {
	root   string
	parent map[string]string
	title  map[string]string
}

func loadSignTree(ctx context.Context, db *sql.DB) (signTree, error) {
	t := signTree{parent: map[string]string{}, title: map[string]string{}}
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(parent_id, ''), title FROM signs ORDER BY position, id`)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, parent, title string
		if err := rows.Scan(&id, &parent, &title); err != nil {
			return t, err
		}
		t.parent[id], t.title[id] = parent, title
		if parent == "" && t.root == "" {
			t.root = id
		}
	}
	return t, rows.Err()
}

// chain is the sign and its ancestors, the sign first.
func (t signTree) chain(id string) []string {
	var out []string
	for seen := map[string]bool{}; id != "" && !seen[id]; id = t.parent[id] {
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (t signTree) under(id, within string) bool {
	if within == "" {
		return true
	}
	for _, s := range t.chain(id) {
		if s == within {
			return true
		}
	}
	return false
}

func (t signTree) titles(id string) []string {
	c := t.chain(id)
	out := make([]string, len(c))
	for i, s := range c {
		out[len(c)-1-i] = t.title[s]
	}
	return out
}

// coveredAnchor is a live entry's file, range or dir anchor with the sign the entry hangs under.
type coveredAnchor struct {
	path string
	sign string
}

// placer finds the sign to write under for a path: the deepest sign whose entries anchor the nearest ancestor directory that holds anchored work, else the root.
type placer struct {
	tree    signTree
	anchors []coveredAnchor
}

func newPlacer(ctx context.Context, db *sql.DB, tree signTree) (*placer, error) {
	rows, err := db.QueryContext(ctx, `SELECT e.id, e.sign_id, a.path_or_url
		FROM anchors a JOIN entries e ON e.id = a.entry_id
		WHERE a.type IN ('file', 'range', 'dir') AND e.tier != 'dormant' AND e.superseded_by IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	p := &placer{tree: tree}
	viewer := view.From(ctx)
	for rows.Next() {
		var id, sign, ap string
		if err := rows.Scan(&id, &sign, &ap); err != nil {
			return nil, err
		}
		if viewer.Sees(id) {
			p.anchors = append(p.anchors, coveredAnchor{strings.TrimSuffix(path.Clean(ap), "/"), sign})
		}
	}
	return p, rows.Err()
}

func (p *placer) signFor(rel string) string {
	for dir := path.Dir(rel); dir != "." && dir != "/"; dir = path.Dir(dir) {
		best, bestDepth := "", -1
		for _, a := range p.anchors {
			if a.path != dir && !strings.HasPrefix(a.path, dir+"/") {
				continue
			}
			if d := len(p.tree.chain(a.sign)); d > bestDepth || d == bestDepth && a.sign < best {
				best, bestDepth = a.sign, d
			}
		}
		if best != "" {
			return best
		}
	}
	return p.tree.root
}

func coverage(ctx context.Context, db *sql.DB, in coverageIn) (coverageOut, error) {
	out := coverageOut{Gaps: []coverageGap{}, Stale: []coverageStale{}, OffTrail: []coverageOffTrail{}}
	limit, err := coverageLimit(ctx, db, in.Limit)
	if err != nil {
		return out, err
	}
	tree, err := loadSignTree(ctx, db)
	if err != nil {
		return out, err
	}
	if in.Within != "" {
		if _, ok := tree.parent[in.Within]; !ok {
			return out, &trail.Refusal{Rule: "unknown_sign", Fix: "sign ids come from the orient view or a sign's edges; check within"}
		}
	}

	gaps, err := trail.AllOpenGaps(ctx, db)
	if err != nil {
		return out, err
	}
	now := time.Now().UTC()
	for _, g := range gaps {
		sign := g.SignID
		if sign == "" {
			sign = tree.root
		}
		if len(out.Gaps) == limit || !tree.under(sign, in.Within) {
			continue
		}
		age := 0
		if at, err := time.Parse(time.RFC3339Nano, g.OpenedAt); err == nil {
			age = int(now.Sub(at).Hours() / 24)
		}
		out.Gaps = append(out.Gaps, coverageGap{ID: g.ID, Query: g.Query, Sign: sign, AgeDays: age})
	}

	if out.Stale, err = staleItems(ctx, db, tree, in.Within, limit); err != nil {
		return out, err
	}
	if out.OffTrail, err = offTrailItems(ctx, db, tree, in.Within, limit); err != nil {
		return out, err
	}
	out.Lines = countLines(out)
	return out, nil
}

// coverageLimit is the per-list size: the setting default when limit is unset, never above the setting maximum.
func coverageLimit(ctx context.Context, db *sql.DB, asked int) (int, error) {
	def, err := search.Setting(ctx, db, "coverage_limit")
	if err != nil {
		return 0, err
	}
	max, err := search.Setting(ctx, db, "coverage_limit_max")
	if err != nil {
		return 0, err
	}
	n := int(def)
	if asked > 0 {
		n = asked
	}
	return min(n, int(max)), nil
}

// staleItems lists live entries whose anchors failed, most failed anchors first.
func staleItems(ctx context.Context, db *sql.DB, tree signTree, within string, limit int) ([]coverageStale, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, title, sign_id FROM entries e WHERE e.tier != 'dormant' AND e.superseded_by IS NULL
		AND (e.tier = 'stale' OR EXISTS(SELECT 1 FROM anchors a WHERE a.entry_id = e.id AND a.ok = 0)) ORDER BY id`)
	if err != nil {
		return nil, err
	}
	type row struct{ id, title, sign string }
	var found []row
	viewer := view.From(ctx)
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.title, &r.sign); err != nil {
			rows.Close()
			return nil, err
		}
		if viewer.Sees(r.id) && tree.under(r.sign, within) {
			found = append(found, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []coverageStale{}
	for _, r := range found {
		as, err := anchor.Load(ctx, db, r.id)
		if err != nil {
			return nil, err
		}
		failed := []string{}
		for _, a := range as {
			if !a.OK {
				failed = append(failed, a.Label())
			}
		}
		out = append(out, coverageStale{ID: r.id, Title: r.title, Sign: r.sign, SignPath: tree.titles(r.sign), FailedAnchors: failed})
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].FailedAnchors) > len(out[j].FailedAnchors) })
	return out[:min(limit, len(out))], nil
}

// offTrailItems lists paths sessions read natively in the window that no live entry anchors, most-read first.
// Anchored is the pre-read hook's own decision, so the two never disagree.
func offTrailItems(ctx context.Context, db *sql.DB, tree signTree, within string, limit int) ([]coverageOffTrail, error) {
	days, err := search.Setting(ctx, db, "coverage_window_days")
	if err != nil {
		return nil, err
	}
	since := time.Now().UTC().AddDate(0, 0, -int(days)).Format(time.RFC3339Nano)
	rows, err := db.QueryContext(ctx, `SELECT target, COUNT(DISTINCT session_id) AS n FROM events
		WHERE tool = 'off_trail' AND at >= ? AND target != '' GROUP BY target ORDER BY n DESC, target`, since)
	if err != nil {
		return nil, err
	}
	type row struct {
		path string
		n    int
	}
	var read []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.path, &r.n); err != nil {
			rows.Close()
			return nil, err
		}
		// An absolute path is outside every checkout: repoRelative left it as given.
		if !filepath.IsAbs(r.path) {
			read = append(read, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []coverageOffTrail{}
	var pl *placer
	for _, r := range read {
		if len(out) == limit {
			break
		}
		anchored, err := anchoringEntries(ctx, db, r.path)
		if err != nil {
			return nil, err
		}
		if len(anchored) > 0 {
			continue
		}
		if pl == nil {
			if pl, err = newPlacer(ctx, db, tree); err != nil {
				return nil, err
			}
		}
		if sign := pl.signFor(r.path); tree.under(sign, within) {
			out = append(out, coverageOffTrail{Path: r.path, Count: r.n, Sign: sign})
		}
	}
	return out, nil
}
