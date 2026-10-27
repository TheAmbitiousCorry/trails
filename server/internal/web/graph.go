package web

import (
	"database/sql"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/search"
)

func (a *api) search(r *http.Request) (any, error) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		return nil, badRequest("q is required")
	}
	hits, miss, err := search.Query(r.Context(), a.st.DB, q, "", 10)
	if err != nil {
		return nil, err
	}
	if hits == nil {
		hits = []search.Hit{}
	}
	return map[string]any{"q": q, "hits": hits, "miss": miss}, nil
}

type graphNode struct {
	Type     string   `json:"type"`
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Parent   string   `json:"parent,omitempty"`
	Kind     string   `json:"kind,omitempty"`
	Tier     string   `json:"tier,omitempty"`
	Reads30d *int     `json:"reads_30d,omitempty"`
	SignID   string   `json:"sign_id,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type graphEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
}

type graphHop struct {
	N             int      `json:"n"`
	Tool          string   `json:"tool"`
	Target        string   `json:"target"`
	Kind          string   `json:"kind"`
	Result        string   `json:"result"`
	Miss          bool     `json:"miss"`
	TurnBack      bool     `json:"turn_back"`
	Prerequisites []string `json:"prerequisites"`
}

func (a *api) graph(r *http.Request) (any, error) {
	ctx, db := r.Context(), a.st.DB
	qs := r.URL.Query()
	tag, focus, sessionID := qs.Get("tag"), qs.Get("focus"), qs.Get("session")
	depth := 1
	if d := qs.Get("depth"); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil || n < 1 || n > 2 {
			return nil, badRequest("depth must be 1 or 2")
		}
		depth = n
	}

	var sessionOut map[string]any
	var route []graphHop
	if sessionID != "" {
		list, err := collect(ctx, db, sessionSelect+` WHERE s.id = ? GROUP BY s.id`, []any{sessionID}, scanSession)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, notFound("unknown session")
		}
		s := list[0]
		sessionOut = map[string]any{"id": s.ID, "task": s.Task, "actor": s.Actor, "started_at": s.StartedAt, "ended_at": s.EndedAt, "ended_by": s.EndedBy}
		steps, _, _, err := buildRoute(ctx, db, sessionID)
		if err != nil {
			return nil, err
		}
		route = make([]graphHop, 0, len(steps))
		for _, st := range steps {
			h := graphHop{N: st.N, Tool: st.Tool, Target: st.Target, Kind: st.Kind, Result: st.Result, Miss: st.Miss, TurnBack: st.TurnBack, Prerequisites: []string{}}
			for _, c := range st.Chain {
				h.Prerequisites = append(h.Prerequisites, c.ID)
			}
			route = append(route, h)
		}
	}

	nodes, err := collect(ctx, db, `SELECT id, COALESCE(parent_id, ''), title FROM signs ORDER BY position, id`, nil,
		func(rs *sql.Rows) (graphNode, error) {
			n := graphNode{Type: "sign"}
			return n, rs.Scan(&n.ID, &n.Parent, &n.Title)
		})
	if err != nil {
		return nil, err
	}
	entryNodes, err := collect(ctx, db, `SELECT e.id, e.title, e.kind, `+effTier+`, `+readsSince+`, e.sign_id, e.tags_json
		FROM entries e WHERE e.tier != 'dormant' AND e.superseded_by IS NULL ORDER BY e.created_at, e.id`, []any{since30()},
		func(rs *sql.Rows) (graphNode, error) {
			n := graphNode{Type: "entry"}
			var reads int
			var tags string
			err := rs.Scan(&n.ID, &n.Title, &n.Kind, &n.Tier, &reads, &n.SignID, &tags)
			n.Reads30d, n.Tags = &reads, decodeTags(tags)
			return n, err
		})
	if err != nil {
		return nil, err
	}
	nodes = append(nodes, entryNodes...)

	known := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		known[n.ID] = true
	}
	edges := []graphEdge{}
	for _, n := range nodes {
		switch {
		case n.Type == "sign" && n.Parent != "":
			edges = append(edges, graphEdge{n.Parent, n.ID, "home"})
		case n.Type == "entry":
			edges = append(edges, graphEdge{n.SignID, n.ID, "home"})
		}
	}
	links, err := collect(ctx, db, `SELECT from_id, to_id, relation FROM links ORDER BY from_id, to_id, relation`, nil,
		func(rs *sql.Rows) (graphEdge, error) {
			var e graphEdge
			return e, rs.Scan(&e.From, &e.To, &e.Relation)
		})
	if err != nil {
		return nil, err
	}
	for _, e := range links {
		if known[e.From] && known[e.To] {
			edges = append(edges, e)
		}
	}

	if focus != "" && !known[focus] {
		return nil, notFound("unknown focus")
	}
	if tag != "" {
		keep := map[string]bool{}
		parent := map[string]string{}
		for _, n := range nodes {
			if n.Type == "sign" {
				parent[n.ID] = n.Parent
			}
		}
		for _, n := range nodes {
			if n.Type == "entry" && slices.Contains(n.Tags, tag) {
				keep[n.ID] = true
				for s := n.SignID; s != "" && !keep[s]; s = parent[s] {
					keep[s] = true
				}
			}
		}
		nodes, edges = restrict(nodes, edges, keep)
	}
	if focus != "" {
		adj := map[string][]string{}
		for _, e := range edges {
			adj[e.From] = append(adj[e.From], e.To)
			adj[e.To] = append(adj[e.To], e.From)
		}
		keep := map[string]bool{focus: true}
		frontier := []string{focus}
		for d := 0; d < depth; d++ {
			var next []string
			for _, id := range frontier {
				for _, nb := range adj[id] {
					if !keep[nb] {
						keep[nb] = true
						next = append(next, nb)
					}
				}
			}
			frontier = next
		}
		nodes, edges = restrict(nodes, edges, keep)
	}

	out := map[string]any{"nodes": nodes, "edges": edges, "route": nil}
	if sessionID != "" {
		out["session"], out["route"] = sessionOut, route
	}
	return out, nil
}

// restrict keeps the nodes in keep and the edges whose both ends survive.
func restrict(nodes []graphNode, edges []graphEdge, keep map[string]bool) ([]graphNode, []graphEdge) {
	n2 := []graphNode{}
	for _, n := range nodes {
		if keep[n.ID] {
			n2 = append(n2, n)
		}
	}
	e2 := []graphEdge{}
	for _, e := range edges {
		if keep[e.From] && keep[e.To] {
			e2 = append(e2, e)
		}
	}
	return n2, e2
}
