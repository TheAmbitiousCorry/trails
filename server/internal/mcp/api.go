package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

// forceAfter is the stop-check call that closes a session the agent never ended.
const forceAfter = 3

// registerAPI mounts the hook endpoints; they are plain JSON so a shell script with curl can call them.
func registerAPI(mux *http.ServeMux, st *store.Store, addr string) {
	mux.Handle("/api/off-trail", guard(addr, postJSON(st, offTrail)))
	mux.Handle("/api/stop-check", guard(addr, postJSON(st, stopCheck)))
}

func postJSON(st *store.Store, h func(context.Context, *store.Store, map[string]any) (any, error)) http.Handler {
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
		out, err := h(r.Context(), st, in)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})
}

func str(in map[string]any, key string) string {
	s, _ := in[key].(string)
	return strings.TrimSpace(s)
}

// openSession finds the most recent open session for a runtime's own session id.
func openSession(ctx context.Context, db *sql.DB, runtimeID string) (string, bool, error) {
	if runtimeID == "" {
		return "", false, nil
	}
	var id string
	err := db.QueryRowContext(ctx, `SELECT id FROM sessions WHERE runtime_session_id = ? AND ended_at IS NULL ORDER BY started_at DESC, rowid DESC LIMIT 1`, runtimeID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

type signpost struct {
	EntryID  string `json:"entry_id"`
	ReadWhen string `json:"read_when"`
}

func offTrail(ctx context.Context, st *store.Store, in map[string]any) (any, error) {
	sid, ok, err := openSession(ctx, st.DB, str(in, "runtime_session_id"))
	p := str(in, "path")
	if err != nil || !ok || p == "" {
		return struct{}{}, err
	}
	rel := repoRelative(st.Repo, p)
	if _, err := st.DB.ExecContext(ctx, `INSERT INTO events (session_id, at, tool, target, result) VALUES (?, ?, 'off_trail', ?, 'ok')`, sid, store.Now(), rel); err != nil {
		return nil, err
	}
	sp, found, err := anchoredEntry(ctx, st.DB, rel)
	if err != nil || !found {
		return struct{}{}, err
	}
	return sp, nil
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
	Open   bool     `json:"open"`
	Blocks int      `json:"blocks"`
	Forced bool     `json:"forced,omitempty"`
	Gaps   []gapRef `json:"gaps"`
}

func stopCheck(ctx context.Context, st *store.Store, in map[string]any) (any, error) {
	sid, ok, err := openSession(ctx, st.DB, str(in, "runtime_session_id"))
	if err != nil || !ok {
		return map[string]bool{"open": false}, err
	}
	var blocks int
	err = st.DB.QueryRowContext(ctx, `UPDATE sessions SET blocks = blocks + 1 WHERE id = ? RETURNING blocks`, sid).Scan(&blocks)
	if err != nil {
		return nil, err
	}
	if blocks >= forceAfter {
		if _, err := st.DB.ExecContext(ctx, `UPDATE sessions SET ended_at = ?, ended_by = 'forced' WHERE id = ? AND ended_at IS NULL`, store.Now(), sid); err != nil {
			return nil, err
		}
		return stopReply{Blocks: blocks, Forced: true, Gaps: []gapRef{}}, nil
	}
	rows, err := st.DB.QueryContext(ctx, `SELECT id, COALESCE(query, paths_json, '') FROM gaps WHERE session_id = ? AND state = 'open' ORDER BY id`, sid)
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
	return stopReply{Open: true, Blocks: blocks, Gaps: gaps}, rows.Err()
}
