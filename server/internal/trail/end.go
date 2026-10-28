package trail

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/search"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

// OpenGapRow is one unresolved gap as session_end lists it.
type OpenGapRow struct {
	ID    int64
	Query string
}

// EndCounts is what a closed session did.
type EndCounts struct {
	Reads, Writes, Confirms int
}

// OpenGaps lists the session's unresolved gaps, oldest first.
func OpenGaps(ctx context.Context, db *sql.DB, sessionID string) ([]OpenGapRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(query, paths_json, '') FROM gaps WHERE session_id = ? AND state = 'open' ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OpenGapRow
	for rows.Next() {
		var g OpenGapRow
		if err := rows.Scan(&g.ID, &g.Query); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

type writtenEntry struct {
	id, readWhen string
}

// writtenThisSession lists the live entries the session wrote a version of, in entry id order.
func writtenThisSession(ctx context.Context, db *sql.DB, sessionID string) ([]writtenEntry, error) {
	rows, err := db.QueryContext(ctx, `SELECT e.id, e.read_when FROM entries e
		WHERE e.tier != 'dormant' AND e.superseded_by IS NULL
			AND EXISTS (SELECT 1 FROM versions v WHERE v.entry_id = e.id AND v.actor = ?)
		ORDER BY e.id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []writtenEntry
	for rows.Next() {
		var w writtenEntry
		if err := rows.Scan(&w.id, &w.readWhen); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// fillCandidates are the written entries whose read_when shares a content term with the gap query. They are suggestions; nothing closes the gap.
func fillCandidates(query string, written []writtenEntry) []string {
	terms := map[string]bool{}
	for _, t := range search.ContentTerms(query) {
		terms[t] = true
	}
	var ids []string
	for _, w := range written {
		for _, t := range search.ContentTerms(w.readWhen) {
			if terms[t] {
				ids = append(ids, w.id)
				break
			}
		}
	}
	return ids
}

// EditedEntry is a live entry anchored to a file the session edited and has since neither written nor confirmed.
type EditedEntry struct {
	ID, Path string
}

// UnconfirmedEdits lists the session's edited and unresolved entries, in entry id order. An entry is unresolved when the latest ok edit event on a path it anchors is newer than the latest ok write, scores or confirm event that targets it.
func UnconfirmedEdits(ctx context.Context, db *sql.DB, sessionID string) ([]EditedEntry, error) {
	rows, err := db.QueryContext(ctx, `SELECT e.id, a.type, a.path_or_url, ed.id, ed.target,
			COALESCE((SELECT MAX(r.id) FROM events r WHERE r.session_id = ed.session_id AND r.target = e.id AND r.result = 'ok' AND r.tool IN ('write', 'write_scores', 'confirm')), 0)
		FROM events ed, anchors a JOIN entries e ON e.id = a.entry_id
		WHERE ed.session_id = ? AND ed.tool = 'edit' AND ed.result = 'ok'
			AND a.type IN ('file', 'range', 'dir') AND e.tier != 'dormant' AND e.superseded_by IS NULL
		ORDER BY e.id, ed.id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	latest := map[string]struct {
		edit int64
		path string
	}{}
	resolved := map[string]int64{}
	var order []string
	for rows.Next() {
		var id, typ, ap, target string
		var editID, resolvedID int64
		if err := rows.Scan(&id, &typ, &ap, &editID, &target, &resolvedID); err != nil {
			return nil, err
		}
		ap = strings.TrimSuffix(path.Clean(ap), "/")
		if !(typ != "dir" && ap == target) && !(typ == "dir" && strings.HasPrefix(target, ap+"/")) {
			continue
		}
		resolved[id] = resolvedID
		if cur, seen := latest[id]; !seen {
			order = append(order, id)
		} else if cur.edit > editID {
			continue
		}
		latest[id] = struct {
			edit int64
			path string
		}{editID, target}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []EditedEntry
	for _, id := range order {
		if latest[id].edit > resolved[id] {
			out = append(out, EditedEntry{id, latest[id].path})
		}
	}
	return out, nil
}

// EndSession closes the session for the agent, or refuses with its open gaps listed. A session already closed is reported, not changed.
func EndSession(ctx context.Context, db *sql.DB, sessionID string) (EndCounts, error) {
	gaps, err := OpenGaps(ctx, db, sessionID)
	if err != nil {
		return EndCounts{}, err
	}
	if len(gaps) > 0 {
		written, err := writtenThisSession(ctx, db, sessionID)
		if err != nil {
			return EndCounts{}, err
		}
		parts := make([]string, len(gaps))
		for i, g := range gaps {
			parts[i] = fmt.Sprintf("gap %d: %q", g.ID, g.Query)
			if ids := fillCandidates(g.Query, written); len(ids) > 0 {
				parts[i] += "; fill candidates: " + strings.Join(ids, ", ")
			}
		}
		return EndCounts{}, &Refusal{Rule: "gaps_open", Fix: "resolve each with resolve_gap (gap_id as an integer, action fill, merge or dismiss; entry_id for fill or merge, reason for dismiss), then call session_end again. " + strings.Join(parts, "; ")}
	}
	edits, err := UnconfirmedEdits(ctx, db, sessionID)
	if err != nil {
		return EndCounts{}, err
	}
	if len(edits) > 0 {
		parts := make([]string, len(edits))
		for i, e := range edits {
			parts[i] = fmt.Sprintf("%s (%s)", e.ID, e.Path)
		}
		return EndCounts{}, &Refusal{Rule: "unconfirmed_edits", Fix: "re-read each entry, then write it with the change or confirm it unchanged, and call session_end again. " + strings.Join(parts, "; ")}
	}
	if _, err := db.ExecContext(ctx, `UPDATE sessions SET ended_at = ?, ended_by = 'agent' WHERE id = ? AND ended_at IS NULL`, store.Now(), sessionID); err != nil {
		return EndCounts{}, err
	}
	var c EndCounts
	err = db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(tool = 'read'), 0), COALESCE(SUM(tool = 'write'), 0), COALESCE(SUM(tool = 'confirm'), 0)
		FROM events WHERE session_id = ? AND result = 'ok'`, sessionID).Scan(&c.Reads, &c.Writes, &c.Confirms)
	return c, err
}
