package trail

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/TheAmbitiousCorry/trails/internal/search"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

const maxDismissReason = 200

// Resolution is one resolve_gap call: Action is fill, merge or dismiss.
type Resolution struct {
	GapID    int64
	Action   string
	TargetID string
	Reason   string
}

// ResolveGap closes one of the session's open gaps. Fill points at an entry the session wrote, merge adds the gap's query as an alias on an existing entry, dismiss states a reason and is limited to dismiss_cap per session.
func ResolveGap(ctx context.Context, db *sql.DB, sessionID string, r Resolution) error {
	var state string
	var query sql.NullString
	err := db.QueryRowContext(ctx, `SELECT state, query FROM gaps WHERE id = ? AND session_id = ?`, r.GapID, sessionID).Scan(&state, &query)
	if errors.Is(err, sql.ErrNoRows) {
		return &Refusal{Rule: "unknown_gap", Fix: "gap ids come from the session_end refusal; this session can resolve only its own gaps"}
	}
	if err != nil {
		return err
	}
	if state != "open" {
		return &Refusal{Rule: "gap_closed", Fix: "this gap is already resolved"}
	}

	target, reason := strings.TrimSpace(r.TargetID), strings.TrimSpace(r.Reason)
	switch r.Action {
	case "fill":
		var wrote int
		err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM versions WHERE entry_id = ? AND actor = ?`, target, sessionID).Scan(&wrote)
		if err != nil {
			return err
		}
		if wrote == 0 {
			return &Refusal{Rule: "fill_target", Fix: "fill needs target_id: an entry this session wrote; write the entry first, then resolve the gap with its id"}
		}
		reason = ""
	case "merge":
		if err := mergeAlias(ctx, db, target, query.String); err != nil {
			return err
		}
		reason = ""
	case "dismiss":
		if reason == "" || utf8.RuneCountInString(reason) > maxDismissReason {
			return &Refusal{Rule: "reason", Fix: "dismiss needs a reason of 1 to 200 characters"}
		}
		limit, err := search.Setting(ctx, db, "dismiss_cap")
		if err != nil {
			return err
		}
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gaps WHERE session_id = ? AND resolution = 'dismiss'`, sessionID).Scan(&n); err != nil {
			return err
		}
		if float64(n) >= limit {
			return &Refusal{Rule: "dismiss_cap", Fix: "this session has used its dismissals; fill or merge the gap, or leave it open for a human"}
		}
		target = ""
	default:
		return &Refusal{Rule: "action", Fix: "action is fill, merge or dismiss"}
	}

	_, err = db.ExecContext(ctx, `UPDATE gaps SET state = 'resolved', resolution = ?, target_id = NULLIF(?,''), reason = NULLIF(?,''), resolved_at = ? WHERE id = ?`,
		r.Action, target, reason, store.Now(), r.GapID)
	return err
}

// mergeAlias adds the gap's query to the entry's read_when aliases so a search for it now hits.
func mergeAlias(ctx context.Context, db *sql.DB, entryID, query string) error {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE id = ?`, entryID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return &Refusal{Rule: "unknown_entry", Fix: "merge needs target_id: the id of an existing entry that answers the query"}
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO entry_aliases (entry_id, read_when) SELECT ?1, ?2
		WHERE ?2 != (SELECT read_when FROM entries WHERE id = ?1)
		  AND NOT EXISTS (SELECT 1 FROM entry_aliases WHERE entry_id = ?1 AND read_when = ?2)`, entryID, query)
	if err != nil {
		return err
	}
	if err := search.Refresh(ctx, tx, entryID); err != nil {
		return err
	}
	return tx.Commit()
}
