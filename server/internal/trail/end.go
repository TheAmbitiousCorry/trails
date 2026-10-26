package trail

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

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

// EndSession closes the session for the agent, or refuses with its open gaps listed. A session already closed is reported, not changed.
func EndSession(ctx context.Context, db *sql.DB, sessionID string) (EndCounts, error) {
	gaps, err := OpenGaps(ctx, db, sessionID)
	if err != nil {
		return EndCounts{}, err
	}
	if len(gaps) > 0 {
		parts := make([]string, len(gaps))
		for i, g := range gaps {
			parts[i] = fmt.Sprintf("gap %d: %q", g.ID, g.Query)
		}
		return EndCounts{}, &Refusal{Rule: "gaps_open", Fix: "resolve each with resolve_gap (fill, merge or dismiss), then call session_end again. " + strings.Join(parts, "; ")}
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
