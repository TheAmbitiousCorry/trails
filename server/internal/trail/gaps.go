package trail

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/search"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

// GapsFull reports whether open gaps have reached open_gap_cap.
func GapsFull(ctx context.Context, db *sql.DB) (bool, error) {
	limit, err := search.Setting(ctx, db, "open_gap_cap")
	if err != nil {
		return false, err
	}
	var n int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gaps WHERE state = 'open'`).Scan(&n)
	return float64(n) >= limit, err
}

// LastLookSign is the sign the session last looked at: the target of its most recent successful look, or the home sign when that target was an entry.
// It is empty when the session has not looked, or its last look was a tag or kind query, which stands at no one sign.
func LastLookSign(ctx context.Context, db *sql.DB, sessionID string) (string, error) {
	var target string
	err := db.QueryRowContext(ctx, `SELECT target FROM events WHERE session_id = ? AND tool = 'look' AND result = 'ok' ORDER BY id DESC LIMIT 1`, sessionID).Scan(&target)
	if errors.Is(err, sql.ErrNoRows) || strings.ContainsAny(target, " :") {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var id string
	err = db.QueryRowContext(ctx, `SELECT id FROM signs WHERE id = ? UNION ALL SELECT sign_id FROM entries WHERE id = ? LIMIT 1`, target, target).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// OpenGap records a search miss as an open gap at the sign the session last looked at.
// At open_gap_cap it opens nothing and returns 0: the miss stays in the event log.
func OpenGap(ctx context.Context, db *sql.DB, sessionID, query string) (int64, error) {
	if full, err := GapsFull(ctx, db); err != nil || full {
		return 0, err
	}
	sign, err := LastLookSign(ctx, db, sessionID)
	if err != nil {
		return 0, err
	}
	res, err := db.ExecContext(ctx, `INSERT INTO gaps (sign_id, session_id, query, opened_at) VALUES (NULLIF(?,''), ?, ?, ?)`, sign, sessionID, query, store.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GapRow is one open gap of any session.
type GapRow struct {
	ID       int64
	Query    string
	SignID   string
	OpenedAt string
}

// AllOpenGaps lists every open gap of every session, oldest first. SignID is empty when the gap recorded no sign.
func AllOpenGaps(ctx context.Context, db *sql.DB) ([]GapRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(query, paths_json, ''), COALESCE(sign_id, ''), opened_at FROM gaps WHERE state = 'open' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GapRow
	for rows.Next() {
		var g GapRow
		if err := rows.Scan(&g.ID, &g.Query, &g.SignID, &g.OpenedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
