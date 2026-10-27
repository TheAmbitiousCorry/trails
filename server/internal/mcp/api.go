package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/project"
	"github.com/TheAmbitiousCorry/trails/internal/store"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

// forceAfter is the stop-check call that closes a session the agent never ended.
const forceAfter = 3

// registerAPI mounts the project and hook endpoints; they are plain JSON so a shell script with curl can call them.
func registerAPI(mux *http.ServeMux, reg *project.Registry, addr string) {
	mux.Handle("GET /api/projects", guard(addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := []projectRow{}
		for _, p := range reg.List() {
			row, err := rowOf(r.Context(), p)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			out = append(out, row)
		}
		writeJSON(w, http.StatusOK, map[string]any{"projects": out})
	})))
	mux.Handle("POST /api/projects", guard(addr, registerProject(reg)))
	mux.Handle("/api/pre-read", guard(addr, hookJSONMiss(reg, preReadReply{Decision: decisionUnmapped}, preRead, preReadMiss)))
	mux.Handle("/api/off-trail", guard(addr, hookJSON(reg, struct{}{}, offTrail)))
	mux.Handle("/api/edited", guard(addr, hookJSON(reg, editedReply{Entries: []signpost{}}, edited)))
	mux.Handle("/api/stop-check", guard(addr, hookJSON(reg, map[string]bool{"open": false}, stopCheck)))
}

type projectRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// projectRow is a listed project with the counts the browser's project list shows.
type projectRow struct {
	projectRef
	Entries      int `json:"entries"`
	OpenSessions int `json:"open_sessions"`
	OpenGaps     int `json:"open_gaps"`
}

func rowOf(ctx context.Context, p *project.Project) (projectRow, error) {
	ref, err := refOf(ctx, p)
	if err != nil {
		return projectRow{}, err
	}
	row := projectRow{projectRef: ref}
	err = p.DB().QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM entries WHERE tier != 'dormant'),
		(SELECT COUNT(*) FROM sessions WHERE ended_at IS NULL),
		(SELECT COUNT(*) FROM gaps WHERE state = 'open')`).Scan(&row.Entries, &row.OpenSessions, &row.OpenGaps)
	return row, err
}

func refOf(ctx context.Context, p *project.Project) (projectRef, error) {
	t, err := p.Title(ctx)
	return projectRef{p.ID, t}, err
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// registerProject opens the repo named in the body and replies its id and title.
// It never writes into the repo: any local process can reach the port, so only a repo that already holds .trail/id is accepted.
func registerProject(reg *project.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
			http.Error(w, "body must be a JSON object", http.StatusBadRequest)
			return
		}
		repo := str(in, "repo")
		if !filepath.IsAbs(repo) {
			http.Error(w, "repo must be an absolute path", http.StatusBadRequest)
			return
		}
		if fi, err := os.Stat(repo); err != nil || !fi.IsDir() {
			http.Error(w, "repo is not a directory", http.StatusBadRequest)
			return
		}
		p, err := reg.Open(repo)
		if errors.Is(err, store.ErrNoID) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "not_opted_in", "fix": "run `trails register` in the repo to create .trail/id and register it"})
			return
		}
		var ref projectRef
		if err == nil {
			ref, err = refOf(r.Context(), p)
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, ref)
	})
}

// hookJSON serves a hook endpoint. The body's project names the project and its runtime_session_id is looked up there; without project the registry looks across all projects.
// An unmatched session gets the empty reply, which is what a hook that never fails a turn expects; an unknown project is a 404.
func hookJSON(reg *project.Registry, empty any, h func(context.Context, *project.Project, string, map[string]any) (any, error)) http.Handler {
	return hookJSONMiss(reg, empty, h, nil)
}

// hookJSONMiss is hookJSON for an endpoint that still answers when no session matches: miss gets the project the body named, nil when it named none.
func hookJSONMiss(reg *project.Registry, empty any, h func(context.Context, *project.Project, string, map[string]any) (any, error), miss func(context.Context, *project.Project, map[string]any) (any, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var in map[string]any
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
			http.Error(w, "body must be a JSON object", http.StatusBadRequest)
			return
		}
		var p *project.Project
		var sid string
		var found bool
		var err error
		if id := str(in, "project"); id != "" {
			var ok bool
			if p, ok = reg.ByID(id); !ok {
				http.Error(w, "project_unknown", http.StatusNotFound)
				return
			}
			sid, _, found, err = p.OpenSession(r.Context(), str(in, "runtime_session_id"))
		} else {
			p, sid, found, err = reg.ByRuntime(r.Context(), str(in, "runtime_session_id"))
		}
		var out any = empty
		switch {
		case err != nil:
		case found:
			out, err = h(r.Context(), p, sid, in)
		case miss != nil:
			out, err = miss(r.Context(), p, in)
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func str(in map[string]any, key string) string {
	s, _ := in[key].(string)
	return strings.TrimSpace(s)
}

const (
	decisionChecked    = "checked"
	decisionTurnedBack = "turned_back"
	decisionUnstarted  = "unstarted"
	decisionUnmapped   = "unmapped"
)

// maxSignposts caps the entries a pre-read reply lists; total still counts them all.
const maxSignposts = 8

// preReadReply is the answer to a hook that asks before a native read. The entry fields are set for checked, turned_back and unstarted;
// entries lists the anchoring entries by id (at most maxSignposts), and entry_id and read_when repeat the first of them.
type preReadReply struct {
	Decision string     `json:"decision"`
	EntryID  string     `json:"entry_id,omitempty"`
	ReadWhen string     `json:"read_when,omitempty"`
	Entries  []signpost `json:"entries,omitempty"`
	Total    int        `json:"total,omitempty"`
}

// signpostReply fills the entry fields of a pre-read reply from anchors, which must be sorted by id and non-empty.
func signpostReply(decision string, anchors []signpost) preReadReply {
	entries := anchors
	if len(entries) > maxSignposts {
		entries = entries[:maxSignposts]
	}
	return preReadReply{Decision: decision, EntryID: anchors[0].EntryID, ReadWhen: anchors[0].ReadWhen, Entries: entries, Total: len(anchors)}
}

// preRead decides whether a native read may go through. A read of a path an entry anchors is checked once the session has read at least one of the entries anchoring it, and turned back otherwise.
// Every call records an off_trail event with the decision as its result, except a turned_back on a path the session was already turned back from.
func preRead(ctx context.Context, st *project.Project, sid string, in map[string]any) (any, error) {
	p := str(in, "path")
	if p == "" {
		return preReadReply{Decision: decisionUnmapped}, nil
	}
	rel := repoRelative(st.Repo(), p)
	anchors, err := anchoringEntries(ctx, st.DB(), rel)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(anchors, func(i, j int) bool { return anchors[i].EntryID < anchors[j].EntryID })
	reply := preReadReply{Decision: decisionUnmapped}
	shown := "[]"
	if len(anchors) > 0 {
		reply.Decision = decisionTurnedBack
		for _, a := range anchors {
			var n int
			if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE session_id = ? AND tool = 'read' AND result = 'ok' AND target = ?`, sid, a.EntryID).Scan(&n); err != nil {
				return nil, err
			}
			if n > 0 {
				reply.Decision = decisionChecked
				break
			}
		}
		reply = signpostReply(reply.Decision, anchors)
		ids := make([]string, 0, len(reply.Entries))
		for _, e := range reply.Entries {
			ids = append(ids, e.EntryID)
		}
		b, _ := json.Marshal(ids)
		shown = string(b)
	}
	if reply.Decision == decisionTurnedBack {
		var n int
		if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE session_id = ? AND tool = 'off_trail' AND result = 'turned_back' AND target = ?`, sid, rel).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return reply, nil
		}
	}
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO events (session_id, at, tool, target, result, shown_json) VALUES (?, ?, 'off_trail', ?, ?, ?)`, sid, store.Now(), rel, reply.Decision, shown); err != nil {
		return nil, err
	}
	return reply, nil
}

// preReadMiss answers a read whose runtime id matches no open session. When the body's project holds a live entry anchoring the path the reply is unstarted, so the hook can send the agent to session_start;
// nothing is recorded, as there is no session to record on.
func preReadMiss(ctx context.Context, p *project.Project, in map[string]any) (any, error) {
	path := str(in, "path")
	if p == nil || path == "" {
		return preReadReply{Decision: decisionUnmapped}, nil
	}
	anchors, err := anchoringEntries(ctx, p.DB(), repoRelative(p.Repo(), path))
	if err != nil {
		return nil, err
	}
	if len(anchors) == 0 {
		return preReadReply{Decision: decisionUnmapped}, nil
	}
	sort.SliceStable(anchors, func(i, j int) bool { return anchors[i].EntryID < anchors[j].EntryID })
	return signpostReply(decisionUnstarted, anchors), nil
}

type signpost struct {
	EntryID  string `json:"entry_id"`
	ReadWhen string `json:"read_when"`
}

func offTrail(ctx context.Context, st *project.Project, sid string, in map[string]any) (any, error) {
	p := str(in, "path")
	if p == "" {
		return struct{}{}, nil
	}
	rel := repoRelative(st.Repo(), p)
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO events (session_id, at, tool, target, result) VALUES (?, ?, 'off_trail', ?, 'ok')`, sid, store.Now(), rel); err != nil {
		return nil, err
	}
	sp, found, err := anchoredEntry(ctx, st.DB(), rel)
	if err != nil || !found {
		return struct{}{}, err
	}
	return sp, nil
}

type editedReply struct {
	Entries []signpost `json:"entries"`
}

// edited records an edit event and replies with every live entry whose file, range or dir anchor covers the path.
func edited(ctx context.Context, st *project.Project, sid string, in map[string]any) (any, error) {
	reply := editedReply{Entries: []signpost{}}
	p := str(in, "path")
	if p == "" {
		return reply, nil
	}
	rel := repoRelative(st.Repo(), p)
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO events (session_id, at, tool, target, result) VALUES (?, ?, 'edit', ?, 'ok')`, sid, store.Now(), rel); err != nil {
		return nil, err
	}
	all, err := anchoringEntries(ctx, st.DB(), rel)
	if err != nil {
		return nil, err
	}
	reply.Entries = append(reply.Entries, all...)
	return reply, nil
}

// anchoringEntries lists every live entry whose file, range or dir anchor is rel or a directory containing it, oldest first.
func anchoringEntries(ctx context.Context, db *sql.DB, rel string) ([]signpost, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT e.id, e.read_when, a.type, a.path_or_url, e.created_at
		FROM anchors a JOIN entries e ON e.id = a.entry_id
		WHERE a.type IN ('file', 'range', 'dir') AND e.tier != 'dormant' AND e.superseded_by IS NULL
		ORDER BY e.created_at, e.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []signpost
	for rows.Next() {
		var id, readWhen, typ, ap, created string
		if err := rows.Scan(&id, &readWhen, &typ, &ap, &created); err != nil {
			return nil, err
		}
		ap = strings.TrimSuffix(path.Clean(ap), "/")
		if (typ != "dir" && ap == rel || typ == "dir" && strings.HasPrefix(rel, ap+"/")) && !seen[id] {
			seen[id] = true
			out = append(out, signpost{id, readWhen})
		}
	}
	return out, rows.Err()
}

// repoRelative reports p relative to the repo when it lies under it, else as given.
func repoRelative(repo, p string) string {
	if filepath.IsAbs(p) {
		if rel, err := filepath.Rel(repo, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
		return p
	}
	return path.Clean(filepath.ToSlash(p))
}

// anchoredEntry returns the live entry whose file, range or dir anchor covers rel; a file or range match beats a dir.
func anchoredEntry(ctx context.Context, db *sql.DB, rel string) (signpost, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT e.id, e.read_when, a.type, a.path_or_url
		FROM anchors a JOIN entries e ON e.id = a.entry_id
		WHERE a.type IN ('file', 'range', 'dir') AND e.tier != 'dormant' AND e.superseded_by IS NULL
		ORDER BY e.created_at, e.id`)
	if err != nil {
		return signpost{}, false, err
	}
	defer rows.Close()
	var best signpost
	bestLen := -1
	for rows.Next() {
		var id, readWhen, typ, ap string
		if err := rows.Scan(&id, &readWhen, &typ, &ap); err != nil {
			return signpost{}, false, err
		}
		ap = strings.TrimSuffix(path.Clean(ap), "/")
		switch {
		case typ != "dir" && ap == rel:
			return signpost{id, readWhen}, true, rows.Err()
		case typ == "dir" && strings.HasPrefix(rel, ap+"/") && len(ap) > bestLen:
			best, bestLen = signpost{id, readWhen}, len(ap)
		}
	}
	return best, bestLen >= 0, rows.Err()
}

type gapRef struct {
	ID    int64  `json:"id"`
	Query string `json:"query"`
}

type stopReply struct {
	Open   bool      `json:"open"`
	Blocks int       `json:"blocks"`
	Forced bool      `json:"forced,omitempty"`
	Gaps   []gapRef  `json:"gaps"`
	Edits  []editRef `json:"edits"`
}

type editRef struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

func stopCheck(ctx context.Context, st *project.Project, sid string, in map[string]any) (any, error) {
	var blocks int
	err := st.DB().QueryRowContext(ctx, `UPDATE sessions SET blocks = blocks + 1 WHERE id = ? RETURNING blocks`, sid).Scan(&blocks)
	if err != nil {
		return nil, err
	}
	if blocks >= forceAfter {
		if _, err := st.DB().ExecContext(ctx, `UPDATE sessions SET ended_at = ?, ended_by = 'forced' WHERE id = ? AND ended_at IS NULL`, store.Now(), sid); err != nil {
			return nil, err
		}
		return stopReply{Blocks: blocks, Forced: true, Gaps: []gapRef{}, Edits: []editRef{}}, nil
	}
	rows, err := st.DB().QueryContext(ctx, `SELECT id, COALESCE(query, paths_json, '') FROM gaps WHERE session_id = ? AND state = 'open' ORDER BY id`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	gaps := []gapRef{}
	for rows.Next() {
		var g gapRef
		if err := rows.Scan(&g.ID, &g.Query); err != nil {
			return nil, err
		}
		gaps = append(gaps, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	pending, err := trail.UnconfirmedEdits(ctx, st.DB(), sid)
	if err != nil {
		return nil, err
	}
	edits := []editRef{}
	for _, e := range pending {
		edits = append(edits, editRef{e.ID, e.Path})
	}
	return stopReply{Open: true, Blocks: blocks, Gaps: gaps, Edits: edits}, nil
}
