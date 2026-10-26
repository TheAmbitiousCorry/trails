// Package signs computes a sign from its children and owns the fan-out cap. Nothing here is stored: a sign is a view.
package signs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

const MaxDepth = 3

type LookRequest struct {
	SignID   string
	EntryID  string
	Tag      string
	Kind     string
	Within   string
	Depth    int
	TaskType string
}

// Edge is one row of a sign. Type is sign, entry, group (several entries of one kind) or folded.
type Edge struct {
	Type        string   `json:"type"`
	ID          string   `json:"id,omitempty"`
	Title       string   `json:"title"`
	ReadWhen    string   `json:"read_when,omitempty"`
	Scent       []string `json:"scent,omitempty"`
	Tier        string   `json:"tier,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	WordCount   int      `json:"word_count,omitempty"`
	ChildCount  int      `json:"child_count,omitempty"`
	StaleCount  int      `json:"stale_count,omitempty"`
	MatchCount  int      `json:"match_count,omitempty"`
	MemberCount int      `json:"member_count,omitempty"`
	Folded      bool     `json:"folded,omitempty"`
	FoldedCount int      `json:"folded_count,omitempty"`
	Children    *View    `json:"children,omitempty"`
}

// View is a sign as an agent sees it. Lines is set on the top-level view only.
type View struct {
	SignID  string `json:"sign_id,omitempty"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Parent  string `json:"parent,omitempty"`
	Virtual bool   `json:"virtual,omitempty"`
	Edges   []Edge `json:"edges"`
	Lines   int    `json:"lines,omitempty"`
}

// Look returns the sign named by the request, or a virtual sign when Tag or Kind is set.
func Look(ctx context.Context, db *sql.DB, req LookRequest) (View, error) {
	if req.Depth == 0 {
		req.Depth = 1
	}
	if req.Depth < 1 || req.Depth > MaxDepth {
		return View{}, &trail.Refusal{Rule: "depth_cap", Fix: "depth is 1 to 3; look at one sign, then descend"}
	}
	p, err := load(ctx, db)
	if err != nil {
		return View{}, err
	}
	limit, err := FanOutCap(ctx, db)
	if err != nil {
		return View{}, err
	}
	var v View
	switch {
	case req.Tag != "" || req.Kind != "":
		v, err = p.virtual(req, limit)
	default:
		id := req.SignID
		if id == "" && req.EntryID != "" {
			for _, e := range p.entries {
				if e.ID == req.EntryID {
					id = e.SignID
				}
			}
			if id == "" {
				return View{}, &trail.Refusal{Rule: "unknown_entry", Fix: "entry ids come from a sign; check the id"}
			}
		}
		if id == "" {
			return View{}, &trail.Refusal{Rule: "look_target", Fix: "pass sign_id or entry_id, or a tag or kind to gather entries across the tree"}
		}
		if p.signs[id] == nil {
			return View{}, &trail.Refusal{Rule: "unknown_sign", Fix: "sign ids come from the orient view or a sign's edges; check the id"}
		}
		v = p.view(id, req.Depth, req.TaskType)
	}
	if err != nil {
		return View{}, err
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	v.Lines = strings.Count(string(b), "\n") + 1
	return v, nil
}

// summaryLine is the first paragraph of the summary entry directly under the sign, else the sign's title.
func (p *project) summaryLine(signID string) (string, *entryRow) {
	for _, e := range p.bySign[signID] {
		if e.Kind == "summary" {
			line, _, _ := strings.Cut(e.Text, "\n")
			if line = strings.TrimSpace(line); line != "" {
				return line, e
			}
			return p.signs[signID].Title, e
		}
	}
	return p.signs[signID].Title, nil
}

func (p *project) view(signID string, depth int, taskType string) View {
	s := p.signs[signID]
	summary, _ := p.summaryLine(signID)
	v := View{SignID: s.ID, Title: s.Title, Summary: summary, Edges: []Edge{}}
	if par := p.signs[s.ParentID]; par != nil {
		v.Parent = par.Title
	}

	type candidate struct {
		edge    Edge
		members []*entryRow
	}
	var cands []candidate
	for _, k := range p.kids[signID] {
		e := p.signEdge(k)
		if depth > 1 {
			child := p.view(k, depth-1, taskType)
			e.Children = &child
		}
		cands = append(cands, candidate{e, p.subtree(k)})
	}
	byKind := map[string][]*entryRow{}
	var kinds []string
	for _, e := range p.bySign[signID] {
		if _, ok := byKind[e.Kind]; !ok {
			kinds = append(kinds, e.Kind)
		}
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	for _, kind := range kinds {
		ms := byKind[kind]
		if len(ms) == 1 {
			cands = append(cands, candidate{entryEdge(p, ms[0]), ms})
			continue
		}
		cands = append(cands, candidate{Edge{
			Type: "group", Kind: kind, Title: fmt.Sprintf("%d %s entries", len(ms), kind),
			ReadWhen: fmt.Sprintf("look(kind: %s, within: %s) lists them", kind, signID),
			Scent:    p.scent(ms), MemberCount: len(ms),
		}, ms})
	}

	folded := 0
	for _, c := range cands {
		if taskType != "" && !anyTagged(c.members, taskType) {
			folded++
			continue
		}
		v.Edges = append(v.Edges, c.edge)
	}
	if folded > 0 {
		v.Edges = append(v.Edges, Edge{
			Type: "folded", Folded: true, FoldedCount: folded,
			Title: fmt.Sprintf("%d more edges outside %s", folded, taskType),
		})
	}
	return v
}

func anyTagged(es []*entryRow, tag string) bool {
	for _, e := range es {
		if hasTag(e.Tags, tag) {
			return true
		}
	}
	return false
}

func entryEdge(p *project, e *entryRow) Edge {
	return Edge{Type: "entry", ID: e.ID, Title: e.Title, ReadWhen: e.ReadWhen, Scent: p.scent([]*entryRow{e}),
		Tier: e.Tier, Kind: e.Kind, WordCount: e.Words}
}

func (p *project) signEdge(id string) Edge {
	sub := p.subtree(id)
	e := Edge{Type: "sign", ID: id, Title: p.signs[id].Title, Scent: p.scent(sub),
		ChildCount: len(p.kids[id]) + len(p.bySign[id])}
	if _, sum := p.summaryLine(id); sum != nil {
		e.Tier, e.ReadWhen = sum.Tier, sum.ReadWhen
	}
	for _, x := range sub {
		if x.Tier == "stale" {
			e.StaleCount++
		}
	}
	return e
}

func (p *project) virtual(req LookRequest, limit int) (View, error) {
	if req.Within != "" && p.signs[req.Within] == nil {
		return View{}, &trail.Refusal{Rule: "unknown_sign", Fix: "within takes a sign id from the orient view or a sign's edges"}
	}
	var query []string
	if req.Tag != "" {
		query = append(query, "tag "+req.Tag)
	}
	if req.Kind != "" {
		query = append(query, "kind "+req.Kind)
	}
	var matches []*entryRow
	for _, e := range p.entries {
		if (req.Tag != "" && !hasTag(e.Tags, req.Tag)) || (req.Kind != "" && e.Kind != req.Kind) {
			continue
		}
		if req.Within != "" && !p.inSubtree(e.SignID, req.Within) {
			continue
		}
		matches = append(matches, e)
	}
	q := strings.Join(query, ", ")
	if req.Within != "" {
		q += " within " + p.signs[req.Within].Title
	}
	v := View{Title: q, Virtual: true, Edges: []Edge{},
		Summary: fmt.Sprintf("%d %s matching %s", len(matches), plural(len(matches), "entry", "entries"), q)}
	if req.Within != "" {
		v.Parent = p.signs[req.Within].Title
	}

	if len(matches) <= limit {
		for _, e := range matches {
			v.Edges = append(v.Edges, entryEdge(p, e))
		}
		return v, nil
	}
	count := map[string]int{}
	var homes []string
	for _, e := range matches {
		if count[e.SignID] == 0 {
			homes = append(homes, e.SignID)
		}
		count[e.SignID]++
	}
	sort.SliceStable(homes, func(i, j int) bool { return count[homes[i]] > count[homes[j]] })
	for _, h := range homes {
		e := p.signEdge(h)
		e.MatchCount = count[h]
		v.Edges = append(v.Edges, e)
	}
	return v, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
