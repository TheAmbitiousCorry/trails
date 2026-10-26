package trail

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

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
func OpenGap(ctx context.Context, db *sql.DB, sessionID, query string) (int64, error) {
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
