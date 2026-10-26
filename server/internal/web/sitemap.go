package web

import (
	"net/http"
	"sort"
)

type tierCounts struct {
	Verified    int `json:"verified"`
	Provisional int `json:"provisional"`
	Stale       int `json:"stale"`
}

func (t *tierCounts) add(tier string) {
	switch tier {
	case "verified":
		t.Verified++
	case "stale":
		t.Stale++
	default:
		t.Provisional++
	}
}

func (t *tierCounts) merge(o tierCounts) {
	t.Verified += o.Verified
	t.Provisional += o.Provisional
	t.Stale += o.Stale
}

type mapEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
	Tier  string `json:"tier"`
	Reads int    `json:"reads_30d"`
}

// mapNode counts cover the node's whole subtree; OwnEntries is only the entries directly under it.
type mapNode struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Tiers      tierCounts `json:"tiers"`
	Entries    int        `json:"entries"`
	OwnEntries int        `json:"own_entries"`
	Stale      int        `json:"stale"`
	OpenGaps   int        `json:"open_gaps"`
	Reads30d   int        `json:"reads_30d"`
	Children   []*mapNode `json:"children"`
	// Matches lists the entries of a tag view; it is empty on the plain tree.
	Matches []mapEntry `json:"matches"`
}

type signOption struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Depth int    `json:"depth"`
}

type mapEntryRow struct {
	mapEntry
	signID string
	tags   []string
}

func (a *api) siteMap(r *http.Request) (any, error) {
	ctx, db := r.Context(), a.st.DB
	tag, within := r.URL.Query().Get("tag"), r.URL.Query().Get("within")

	type signRow struct{ id, parent, title string }
	signs, err := collect(ctx, db, `SELECT id, COALESCE(parent_id, ''), title FROM signs ORDER BY position, title, id`, nil,
		func(rs rowScanner) (signRow, error) {
			var s signRow
			return s, rs.Scan(&s.id, &s.parent, &s.title)
		})
	if err != nil {
		return nil, err
	}
	byID := map[string]signRow{}
	kids := map[string][]string{}
	rootID := ""
	for _, s := range signs {
		byID[s.id] = s
		if s.parent == "" {
			rootID = s.id
		} else {
			kids[s.parent] = append(kids[s.parent], s.id)
		}
	}
	topID := rootID
	projectTitle := byID[rootID].title
	if within != "" {
		if _, ok := byID[within]; !ok {
			return nil, notFound("unknown sign")
		}
		rootID = within
	}

	entries, err := collect(ctx, db, `SELECT e.id, e.title, e.kind, `+effTier+`, `+readsSince+`, e.sign_id, e.tags_json
		FROM entries e WHERE e.tier != 'dormant' ORDER BY e.created_at, e.id`, []any{since30()},
		func(rs rowScanner) (mapEntryRow, error) {
			var e mapEntryRow
			var tags string
			err := rs.Scan(&e.ID, &e.Title, &e.Kind, &e.Tier, &e.Reads, &e.signID, &tags)
			e.tags = decodeTags(tags)
			return e, err
		})
	if err != nil {
		return nil, err
	}
	gapCount := map[string]int{}
	type gapRow struct {
		sign string
		n    int
	}
	gaps, err := collect(ctx, db, `SELECT COALESCE(sign_id, ''), COUNT(*) FROM gaps WHERE state = 'open' GROUP BY 1`, nil,
		func(rs rowScanner) (gapRow, error) {
			var g gapRow
			return g, rs.Scan(&g.sign, &g.n)
		})
	if err != nil {
		return nil, err
	}
	for _, g := range gaps {
		id := g.sign
		if _, ok := byID[id]; !ok {
			id = topID
		}
		gapCount[id] += g.n
	}

	bySign := map[string][]mapEntryRow{}
	tagSet := map[string]bool{}
	for _, e := range entries {
		for _, t := range e.tags {
			tagSet[t] = true
		}
		if tag != "" && !hasString(e.tags, tag) {
			continue
		}
		bySign[e.signID] = append(bySign[e.signID], e)
	}

	var build func(id string) *mapNode
	build = func(id string) *mapNode {
		n := &mapNode{ID: id, Title: byID[id].title, Children: []*mapNode{}, Matches: []mapEntry{}, OpenGaps: gapCount[id]}
		for _, e := range bySign[id] {
			n.Tiers.add(e.Tier)
			n.OwnEntries++
			n.Reads30d += e.Reads
			if tag != "" {
				n.Matches = append(n.Matches, e.mapEntry)
			}
		}
		for _, k := range kids[id] {
			c := build(k)
			if tag != "" && c.Entries == 0 {
				continue
			}
			n.Children = append(n.Children, c)
			n.Tiers.merge(c.Tiers)
			n.Reads30d += c.Reads30d
			n.OpenGaps += c.OpenGaps
			n.Entries += c.Entries
		}
		n.Entries += n.OwnEntries
		n.Stale = n.Tiers.Stale
		return n
	}
	root := build(rootID)

	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	options := []signOption{}
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		options = append(options, signOption{id, byID[id].title, depth})
		for _, k := range kids[id] {
			walk(k, depth+1)
		}
	}
	for _, s := range signs {
		if s.parent == "" {
			walk(s.id, 0)
		}
	}

	return map[string]any{
		"project": map[string]string{"id": a.st.ProjectID, "title": projectTitle},
		"tag":     tag,
		"within":  within,
		"root":    root,
		"tags":    tags,
		"signs":   options,
	}, nil
}

func hasString(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
