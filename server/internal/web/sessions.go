package web

import (
	"context"
	"database/sql"
	"net/http"
	"path"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/entries"
)

type sessionRow struct {
	ID        string `json:"id"`
	Actor     string `json:"actor"`
	Task      string `json:"task"`
	TaskType  string `json:"task_type"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
	EndedBy   string `json:"ended_by"`
	Counts    struct {
		Looks    int `json:"looks"`
		Searches int `json:"searches"`
		Reads    int `json:"reads"`
		Writes   int `json:"writes"`
		Confirms int `json:"confirms"`
		OffTrail int `json:"off_trail"`
		Misses   int `json:"misses"`
	} `json:"counts"`
	OpenGaps int `json:"open_gaps"`
}

const sessionSelect = `SELECT s.id, s.actor, s.task, s.task_type, s.started_at, COALESCE(s.ended_at, ''), COALESCE(s.ended_by, ''),
	COALESCE(SUM(e.tool = 'look'), 0), COALESCE(SUM(e.tool = 'search'), 0), COALESCE(SUM(e.tool = 'read' AND e.result = 'ok'), 0),
	COALESCE(SUM(e.tool = 'write'), 0), COALESCE(SUM(e.tool = 'confirm' AND e.result = 'ok'), 0),
	COALESCE(SUM(e.tool = 'off_trail'), 0), COALESCE(SUM(e.result = 'miss'), 0),
	(SELECT COUNT(*) FROM gaps g WHERE g.session_id = s.id AND g.state = 'open')
	FROM sessions s LEFT JOIN events e ON e.session_id = s.id`

func scanSession(rs *sql.Rows) (sessionRow, error) {
	var s sessionRow
	c := &s.Counts
	err := rs.Scan(&s.ID, &s.Actor, &s.Task, &s.TaskType, &s.StartedAt, &s.EndedAt, &s.EndedBy,
		&c.Looks, &c.Searches, &c.Reads, &c.Writes, &c.Confirms, &c.OffTrail, &c.Misses, &s.OpenGaps)
	return s, err
}

func (a *api) sessions(r *http.Request) (any, error) {
	list, err := collect(r.Context(), a.st.DB, sessionSelect+` GROUP BY s.id ORDER BY s.started_at DESC, s.rowid DESC LIMIT 200`, nil, scanSession)
	return map[string]any{"sessions": list}, err
}

type routeStep struct {
	N        int        `json:"n"`
	At       string     `json:"at"`
	Tool     string     `json:"tool"`
	Target   string     `json:"target"`
	Title    string     `json:"title"`
	Kind     string     `json:"kind"`
	Depth    string     `json:"depth,omitempty"`
	Result   string     `json:"result"`
	Miss     bool       `json:"miss"`
	GapID    int64      `json:"gap_id,omitempty"`
	TurnBack bool       `json:"turn_back"`
	Touched  bool       `json:"touched"`
	Chain    []pathStep `json:"prerequisites"`
	Shown    []pathStep `json:"shown"`
}

type offTrailRow struct {
	At      string `json:"at"`
	Path    string `json:"path"`
	Result  string `json:"result"`
	EntryID string `json:"entry_id"`
	Title   string `json:"title"`
}

type touchedEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Tier  string `json:"tier"`
	How   string `json:"how"`
	At    string `json:"at"`
}

type rawEvent struct {
	at, tool, target, depth, result, shown string
}

func (a *api) session(r *http.Request) (any, error) {
	ctx, db := r.Context(), a.st.DB
	id := r.PathValue("id")
	list, err := collect(ctx, db, sessionSelect+` WHERE s.id = ? GROUP BY s.id`, []any{id}, scanSession)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, notFound("unknown session")
	}

	route, offTrail, touched, err := buildRoute(ctx, db, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"session":   list[0],
		"route":     route,
		"off_trail": offTrail,
		"touched":   touched,
	}, nil
}

// buildRoute returns a session's hops in order, its off-trail reads, and the entries it wrote or confirmed.
func buildRoute(ctx context.Context, db *sql.DB, id string) (route []routeStep, offTrail []offTrailRow, touched []touchedEntry, err error) {
	events, err := collect(ctx, db, `SELECT at, tool, target, depth, result, shown_json FROM events WHERE session_id = ? ORDER BY id`, []any{id},
		func(rs *sql.Rows) (rawEvent, error) {
			var e rawEvent
			return e, rs.Scan(&e.at, &e.tool, &e.target, &e.depth, &e.result, &e.shown)
		})
	if err != nil {
		return nil, nil, nil, err
	}

	// Titles and parents for every sign and entry the route can name.
	signTitle, signParent, entryTitle, entrySign := map[string]string{}, map[string]string{}, map[string]string{}, map[string]string{}
	if _, err := collect(ctx, db, `SELECT id, COALESCE(parent_id, ''), title FROM signs`, nil, func(rs *sql.Rows) (struct{}, error) {
		var i, p, t string
		err := rs.Scan(&i, &p, &t)
		signTitle[i], signParent[i] = t, p
		return struct{}{}, err
	}); err != nil {
		return nil, nil, nil, err
	}
	if _, err := collect(ctx, db, `SELECT id, sign_id, title FROM entries`, nil, func(rs *sql.Rows) (struct{}, error) {
		var i, s, t string
		err := rs.Scan(&i, &s, &t)
		entryTitle[i], entrySign[i] = t, s
		return struct{}{}, err
	}); err != nil {
		return nil, nil, nil, err
	}
	titleOf := func(target string) (title, kind string) {
		if t, ok := signTitle[target]; ok {
			return t, "sign"
		}
		if t, ok := entryTitle[target]; ok {
			return t, "entry"
		}
		return "", "query"
	}
	ref := func(id string) pathStep {
		t, _ := titleOf(id)
		return pathStep{ID: id, Title: t}
	}

	touched, err = collect(ctx, db, `SELECT e.id, e.title, `+effTier+`, 'wrote', MIN(v.created_at)
		FROM versions v JOIN entries e ON e.id = v.entry_id WHERE v.actor = ? GROUP BY e.id ORDER BY MIN(v.created_at)`, []any{id},
		func(rs *sql.Rows) (touchedEntry, error) {
			var t touchedEntry
			return t, rs.Scan(&t.ID, &t.Title, &t.Tier, &t.How, &t.At)
		})
	if err != nil {
		return nil, nil, nil, err
	}
	confirms, err := collect(ctx, db, `SELECT e.id, e.title, `+effTier+`, 'confirmed', ev.at
		FROM events ev JOIN entries e ON e.id = ev.target WHERE ev.session_id = ? AND ev.tool = 'confirm' AND ev.result = 'ok' ORDER BY ev.id`, []any{id},
		func(rs *sql.Rows) (touchedEntry, error) {
			var t touchedEntry
			return t, rs.Scan(&t.ID, &t.Title, &t.Tier, &t.How, &t.At)
		})
	if err != nil {
		return nil, nil, nil, err
	}
	touched = append(touched, confirms...)
	isTouched := map[string]bool{}
	for _, t := range touched {
		isTouched[t.ID] = true
	}

	gapIDs := map[string]int64{}
	if _, err := collect(ctx, db, `SELECT id, COALESCE(query, '') FROM gaps WHERE session_id = ? ORDER BY id DESC`, []any{id}, func(rs *sql.Rows) (struct{}, error) {
		var gid int64
		var q string
		err := rs.Scan(&gid, &q)
		gapIDs[q] = gid
		return struct{}{}, err
	}); err != nil {
		return nil, nil, nil, err
	}

	route = []routeStep{}
	offTrail = []offTrailRow{}
	seen := map[string]bool{}
	prevLook := ""
	for i := 0; i < len(events); i++ {
		ev := events[i]
		switch ev.tool {
		case "session_start", "write_scores", "sweep", "edit":
			continue
		case "off_trail":
			row := offTrailRow{At: ev.at, Path: ev.target, Result: ev.result}
			if ids := decodeTags(ev.shown); len(ids) > 0 {
				// A pre-read event names the entry it pointed at.
				row.EntryID, row.Title = ids[0], entryTitle[ids[0]]
			} else if eid, ok, err := anchoredEntry(ctx, db, ev.target); err != nil {
				return nil, nil, nil, err
			} else if ok {
				row.EntryID, row.Title = eid, entryTitle[eid]
			}
			offTrail = append(offTrail, row)
			continue
		}
		title, kind := titleOf(ev.target)
		s := routeStep{N: len(route) + 1, At: ev.at, Tool: ev.tool, Target: ev.target, Title: title, Kind: kind, Depth: ev.depth,
			Result: ev.result, Miss: ev.result == "miss", Chain: []pathStep{}, Shown: []pathStep{}, Touched: isTouched[ev.target]}
		if s.Miss {
			s.GapID = gapIDs[ev.target]
		}
		switch ev.tool {
		case "look":
			home := ev.target
			if _, ok := signTitle[home]; !ok {
				home = entrySign[home]
			}
			if home != "" {
				s.TurnBack = seen["look:"+home] || (prevLook != "" && signParent[prevLook] == home)
				seen["look:"+home] = true
				prevLook = home
			}
		case "read":
			s.TurnBack = seen["read:"+ev.target]
			seen["read:"+ev.target] = true
			if ev.depth == "full" && ev.result == "ok" {
				chain, err := entries.Chain(ctx, db, ev.target)
				if err != nil {
					return nil, nil, nil, err
				}
				inChain := map[string]bool{}
				for _, c := range chain {
					inChain[c] = true
				}
				// The server logs each prerequisite it serves as its own read right after the main one; fold them into this hop.
				for i+1 < len(events) && events[i+1].tool == "read" && events[i+1].depth == "full" && inChain[events[i+1].target] {
					i++
					s.Chain = append(s.Chain, ref(events[i].target))
					seen["read:"+events[i].target] = true
				}
			}
		case "search":
			for _, shown := range decodeTags(ev.shown) {
				s.Shown = append(s.Shown, ref(shown))
			}
		}
		route = append(route, s)
	}
	return route, offTrail, touched, nil
}

// anchoredEntry names the live entry whose file, range or dir anchor covers rel, preferring an exact file match over the deepest dir.
func anchoredEntry(ctx context.Context, db *sql.DB, rel string) (string, bool, error) {
	rel = path.Clean(rel)
	rows, err := db.QueryContext(ctx, `SELECT e.id, a.type, a.path_or_url FROM anchors a JOIN entries e ON e.id = a.entry_id
		WHERE a.type IN ('file', 'range', 'dir') AND e.tier != 'dormant' AND e.superseded_by IS NULL ORDER BY e.created_at, e.id`)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	best, bestLen := "", -1
	for rows.Next() {
		var id, typ, ap string
		if err := rows.Scan(&id, &typ, &ap); err != nil {
			return "", false, err
		}
		ap = strings.TrimSuffix(path.Clean(ap), "/")
		switch {
		case typ != "dir" && ap == rel:
			return id, true, rows.Err()
		case typ == "dir" && strings.HasPrefix(rel, ap+"/") && len(ap) > bestLen:
			best, bestLen = id, len(ap)
		}
	}
	return best, bestLen >= 0, rows.Err()
}
