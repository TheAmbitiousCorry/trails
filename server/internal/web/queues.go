package web

import (
	"database/sql"
	"net/http"
)

type gapOut struct {
	ID         int64  `json:"id"`
	SignID     string `json:"sign_id"`
	SignTitle  string `json:"sign_title"`
	SessionID  string `json:"session_id"`
	Task       string `json:"task"`
	Query      string `json:"query"`
	Paths      string `json:"paths"`
	State      string `json:"state"`
	Resolution string `json:"resolution"`
	TargetID   string `json:"target_id"`
	Reason     string `json:"reason"`
	OpenedAt   string `json:"opened_at"`
	ResolvedAt string `json:"resolved_at"`
}

const gapSelect = `SELECT g.id, COALESCE(g.sign_id, ''), COALESCE(sg.title, ''), g.session_id, COALESCE(s.task, ''), COALESCE(g.query, ''), COALESCE(g.paths_json, ''),
	g.state, COALESCE(g.resolution, ''), COALESCE(g.target_id, ''), COALESCE(g.reason, ''), g.opened_at, COALESCE(g.resolved_at, '')
	FROM gaps g LEFT JOIN signs sg ON sg.id = g.sign_id LEFT JOIN sessions s ON s.id = g.session_id`

func scanGap(rs *sql.Rows) (gapOut, error) {
	var g gapOut
	err := rs.Scan(&g.ID, &g.SignID, &g.SignTitle, &g.SessionID, &g.Task, &g.Query, &g.Paths, &g.State, &g.Resolution, &g.TargetID, &g.Reason, &g.OpenedAt, &g.ResolvedAt)
	return g, err
}

func (a *api) gaps(r *http.Request) (any, error) {
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "open"
	}
	if state != "open" && state != "resolved" && state != "all" {
		return nil, badRequest("state is open, resolved or all")
	}
	q, args := gapSelect, []any(nil)
	if state != "all" {
		q += ` WHERE g.state = ?`
		args = append(args, state)
	}
	list, err := collect(r.Context(), a.st.DB, q+` ORDER BY g.id`, args, scanGap)
	return map[string]any{"state": state, "gaps": list}, err
}

func (a *api) dismissals(r *http.Request) (any, error) {
	list, err := collect(r.Context(), a.st.DB, gapSelect+` WHERE g.resolution = 'dismiss' ORDER BY g.resolved_at DESC, g.id DESC`, nil, scanGap)
	return map[string]any{"dismissals": list}, err
}

type provisionalRow struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Kind        string     `json:"kind"`
	ReadWhen    string     `json:"read_when"`
	SignID      string     `json:"sign_id"`
	Path        []pathStep `json:"path"`
	Reads       int        `json:"reads"`
	Reads30d    int        `json:"reads_30d"`
	ConfirmedBy int        `json:"confirmed_by"`
	CreatedAt   string     `json:"created_at"`
}

func (a *api) provisional(r *http.Request) (any, error) {
	ctx, db := r.Context(), a.st.DB
	list, err := collect(ctx, db, `SELECT e.id, e.title, e.kind, e.read_when, e.sign_id, e.created_at, `+readsSince+`,
			(SELECT COUNT(*) FROM events ev WHERE ev.tool = 'read' AND ev.result = 'ok' AND ev.target = e.id),
			(SELECT COUNT(DISTINCT ev.session_id) FROM events ev WHERE ev.tool = 'confirm' AND ev.result = 'ok' AND ev.target = e.id)
		FROM entries e WHERE `+effTier+` = 'provisional' ORDER BY 8 DESC, e.created_at, e.id`, []any{since30()},
		func(rs *sql.Rows) (provisionalRow, error) {
			var p provisionalRow
			return p, rs.Scan(&p.ID, &p.Title, &p.Kind, &p.ReadWhen, &p.SignID, &p.CreatedAt, &p.Reads30d, &p.Reads, &p.ConfirmedBy)
		})
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Path, err = signPath(ctx, db, list[i].SignID); err != nil {
			return nil, err
		}
	}
	return map[string]any{"entries": list}, nil
}

type tagRow struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Defined     bool   `json:"defined"`
	Uses        int    `json:"uses"`
}

func (a *api) tags(r *http.Request) (any, error) {
	list, err := collect(r.Context(), a.st.DB, `WITH used(tag, n) AS (
			SELECT j.value, COUNT(*) FROM entries e, json_each(e.tags_json) j WHERE e.tier != 'dormant' GROUP BY j.value),
		names(tag) AS (SELECT tag FROM used UNION SELECT id FROM tags)
		SELECT names.tag, COALESCE(t.description, ''), COALESCE(t.defined, 0), COALESCE(used.n, 0)
		FROM names LEFT JOIN tags t ON t.id = names.tag LEFT JOIN used ON used.tag = names.tag
		ORDER BY COALESCE(t.defined, 0), COALESCE(used.n, 0) DESC, names.tag`, nil,
		func(rs *sql.Rows) (tagRow, error) {
			var t tagRow
			var defined int
			err := rs.Scan(&t.ID, &t.Description, &defined, &t.Uses)
			t.Defined = defined != 0
			return t, err
		})
	return map[string]any{"tags": list}, err
}

type rejectionGroup struct {
	Rule      string `json:"rule"`
	ActorKind string `json:"actor_kind"`
	Count     int    `json:"count"`
}

type rejectionEvent struct {
	At        string `json:"at"`
	Tool      string `json:"tool"`
	Target    string `json:"target"`
	Rule      string `json:"rule"`
	ActorKind string `json:"actor_kind"`
	SessionID string `json:"session_id"`
}

const actorKind = `CASE WHEN e.session_id = 'user' THEN 'user' ELSE COALESCE(s.actor, 'unknown') END`

func (a *api) rejections(r *http.Request) (any, error) {
	ctx, db := r.Context(), a.st.DB
	const from = ` FROM events e LEFT JOIN sessions s ON s.id = e.session_id WHERE e.result LIKE 'refused:%'`
	groups, err := collect(ctx, db, `SELECT substr(e.result, 9), `+actorKind+`, COUNT(*)`+from+` GROUP BY 1, 2 ORDER BY 3 DESC, 1, 2`, nil,
		func(rs *sql.Rows) (rejectionGroup, error) {
			var g rejectionGroup
			return g, rs.Scan(&g.Rule, &g.ActorKind, &g.Count)
		})
	if err != nil {
		return nil, err
	}
	recent, err := collect(ctx, db, `SELECT e.at, e.tool, e.target, substr(e.result, 9), `+actorKind+`, e.session_id`+from+` ORDER BY e.id DESC LIMIT 50`, nil,
		func(rs *sql.Rows) (rejectionEvent, error) {
			var x rejectionEvent
			return x, rs.Scan(&x.At, &x.Tool, &x.Target, &x.Rule, &x.ActorKind, &x.SessionID)
		})
	if err != nil {
		return nil, err
	}
	total := 0
	for _, g := range groups {
		total += g.Count
	}
	return map[string]any{"total": total, "groups": groups, "recent": recent}, nil
}
