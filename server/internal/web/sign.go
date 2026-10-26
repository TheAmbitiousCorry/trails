package web

import (
	"database/sql"
	"errors"
	"net/http"
	"sort"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/entries"
	"github.com/TheAmbitiousCorry/trails/internal/signs"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

type signEntry struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Kind        string          `json:"kind"`
	Tier        string          `json:"tier"`
	ReadWhen    string          `json:"read_when"`
	Tags        []string        `json:"tags"`
	Body        string          `json:"body_markdown"`
	BodyProblem string          `json:"body_problem,omitempty"`
	ChainLength int             `json:"chain_length"`
	Anchors     []anchor.Anchor `json:"anchors"`
	Reads30d    int             `json:"reads_30d"`
	UpdatedAt   string          `json:"updated_at"`
}

type kindGroup struct {
	Kind    string      `json:"kind"`
	Entries []signEntry `json:"entries"`
}

var kindOrder = map[string]int{"summary": 0, "decision": 1, "example": 2, "reference": 3, "render": 4}

func (a *api) sign(r *http.Request) (any, error) {
	ctx, db := r.Context(), a.st.DB
	id := r.PathValue("id")
	view, err := signs.Look(ctx, db, signs.LookRequest{SignID: id, Depth: 1})
	var ref *trail.Refusal
	if errors.As(err, &ref) {
		return nil, notFound("unknown sign")
	}
	if err != nil {
		return nil, err
	}
	path, err := signPath(ctx, db, id)
	if err != nil {
		return nil, err
	}
	stale, err := anchor.StaleCount(ctx, db, id)
	if err != nil {
		return nil, err
	}
	var openGaps int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gaps WHERE state = 'open' AND sign_id = ?`, id).Scan(&openGaps); err != nil {
		return nil, err
	}

	type row struct {
		signEntry
		body string
		tags string
	}
	rows, err := collect(ctx, db, `SELECT e.id, e.title, e.kind, `+effTier+`, e.read_when, e.tags_json, e.body_json, `+readsSince+`, e.updated_at
		FROM entries e WHERE e.sign_id = ? AND e.tier != 'dormant' ORDER BY e.created_at, e.id`, []any{since30(), id},
		func(rs *sql.Rows) (row, error) {
			var x row
			err := rs.Scan(&x.ID, &x.Title, &x.Kind, &x.Tier, &x.ReadWhen, &x.tags, &x.body, &x.Reads30d, &x.UpdatedAt)
			return x, err
		})
	if err != nil {
		return nil, err
	}
	groups := []*kindGroup{}
	index := map[string]*kindGroup{}
	for _, x := range rows {
		e := x.signEntry
		e.Tags = decodeTags(x.tags)
		e.Body, e.BodyProblem = bodyMarkdown(x.body)
		chain, err := entries.Chain(ctx, db, e.ID)
		if err != nil {
			return nil, err
		}
		e.ChainLength = len(chain)
		if e.Anchors, err = anchor.Load(ctx, db, e.ID); err != nil {
			return nil, err
		}
		if e.Anchors == nil {
			e.Anchors = []anchor.Anchor{}
		}
		g := index[e.Kind]
		if g == nil {
			g = &kindGroup{Kind: e.Kind, Entries: []signEntry{}}
			index[e.Kind] = g
			groups = append(groups, g)
		}
		g.Entries = append(g.Entries, e)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		oi, iok := kindOrder[groups[i].Kind]
		oj, jok := kindOrder[groups[j].Kind]
		if iok != jok {
			return iok
		}
		return oi < oj
	})

	return map[string]any{
		"view":      view,
		"path":      path,
		"stale":     stale,
		"open_gaps": openGaps,
		"groups":    groups,
	}, nil
}
