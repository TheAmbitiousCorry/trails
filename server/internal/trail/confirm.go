package trail

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

// agentPromotion is how many distinct sessions must confirm a provisional entry to verify it.
const agentPromotion = 2

// ConfirmResult is the entry's tier after a confirm and how many distinct agent sessions have confirmed it.
type ConfirmResult struct {
	Tier      string
	Confirmed int
}

// Confirm records the session's confirmation of an entry and promotes it to verified once two distinct sessions have confirmed.
// The caller must have read the entry at full in this session and must not be the session that wrote it.
// A confirm attests the body against the files as they are now, so it re-anchors the entry first: a stale entry whose anchors resolve again is restored before the confirmation counts, and an entry with an anchor that does not resolve is refused with rule stale.
func Confirm(ctx context.Context, db *sql.DB, c anchor.Checker, sessionID, entryID string) (ConfirmResult, error) {
	var tier string
	var author sql.NullString
	err := db.QueryRowContext(ctx, `SELECT tier, (SELECT actor FROM versions WHERE entry_id = entries.id ORDER BY id LIMIT 1) FROM entries WHERE id = ?`, entryID).Scan(&tier, &author)
	if errors.Is(err, sql.ErrNoRows) {
		return ConfirmResult{}, &Refusal{Rule: "unknown_entry", Fix: "entry ids come from a sign or a read; check the id"}
	}
	if err != nil {
		return ConfirmResult{}, err
	}
	if author.String == sessionID {
		return ConfirmResult{}, &Refusal{Rule: "self", Fix: "a session cannot confirm an entry it wrote; leave it for another session or a human"}
	}
	seen, err := ReadAtFull(ctx, db, sessionID, entryID)
	if err != nil {
		return ConfirmResult{}, err
	}
	if !seen {
		return ConfirmResult{}, &Refusal{Rule: "unread", Fix: "read the entry at depth full in this session, check it against its anchors, then confirm"}
	}
	if err := reanchor(ctx, db, c, entryID); err != nil {
		return ConfirmResult{}, err
	}
	if tier, err = currentTier(ctx, db, entryID); err != nil {
		return ConfirmResult{}, err
	}
	// The recorder marks this call's own event 'pending' until the handler returns, so the caller is added by hand.
	var n int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
			SELECT session_id FROM events WHERE tool = 'confirm' AND target = ? AND result = 'ok' AND session_id != 'user'
			UNION SELECT ?)`, entryID, sessionID).Scan(&n)
	if err != nil {
		return ConfirmResult{}, err
	}
	if tier == "provisional" && n >= agentPromotion {
		if _, err := db.ExecContext(ctx, `UPDATE entries SET tier = 'verified', promoted_by = 'agents' WHERE id = ? AND tier = 'provisional'`, entryID); err != nil {
			return ConfirmResult{}, err
		}
		tier = "verified"
	}
	return ConfirmResult{Tier: tier, Confirmed: n}, nil
}

// ConfirmHuman is the browser's confirm: a human promotes a provisional entry alone, after the same re-anchoring as Confirm.
func ConfirmHuman(ctx context.Context, db *sql.DB, c anchor.Checker, entryID string) (string, error) {
	var tier string
	err := db.QueryRowContext(ctx, `SELECT tier FROM entries WHERE id = ?`, entryID).Scan(&tier)
	if errors.Is(err, sql.ErrNoRows) {
		return "", &Refusal{Rule: "unknown_entry", Fix: "entry ids come from a sign or a read; check the id"}
	}
	if err != nil {
		return "", err
	}
	if err := reanchor(ctx, db, c, entryID); err != nil {
		return "", err
	}
	if tier, err = currentTier(ctx, db, entryID); err != nil {
		return "", err
	}
	if tier == "provisional" {
		if _, err := db.ExecContext(ctx, `UPDATE entries SET tier = 'verified', promoted_by = 'user' WHERE id = ?`, entryID); err != nil {
			return "", err
		}
		tier = "verified"
	}
	_, err = db.ExecContext(ctx, `INSERT INTO events (session_id, at, tool, target, result) VALUES ('user', ?, 'confirm', ?, 'ok')`, store.Now(), entryID)
	return tier, err
}

// reanchor re-resolves the entry's anchors and refuses with rule stale, naming each one, when any does not resolve.
func reanchor(ctx context.Context, db *sql.DB, c anchor.Checker, entryID string) error {
	failed, err := anchor.Reattach(ctx, db, c, entryID)
	if err != nil || len(failed) == 0 {
		return err
	}
	labels := make([]string, len(failed))
	for i, a := range failed {
		labels[i] = a.Label()
	}
	return &Refusal{Rule: "stale", Fix: "these anchors do not resolve: " + strings.Join(labels, "; ") + "; restore the source or correct the anchors with write, then confirm"}
}

func currentTier(ctx context.Context, db *sql.DB, entryID string) (string, error) {
	var tier string
	err := db.QueryRowContext(ctx, `SELECT tier FROM entries WHERE id = ?`, entryID).Scan(&tier)
	return tier, err
}
