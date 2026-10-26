package trail

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
)

const (
	expiryAfter = 30 * 24 * time.Hour
	sessionIdle = 4 * time.Hour
	sweepActor  = "sweep"
	sweepTool   = "sweep"
)

// SweepReport counts what one pass changed.
type SweepReport struct {
	Checked, Dormant, Deleted, Closed int
}

// Sweep re-checks every live entry's anchors, expires provisional entries nobody reads, and closes idle sessions.
// A provisional entry unread for 30 days goes dormant; a dormant one untouched for 30 more is deleted and its id recorded in ids.
// Both moves are logged as 'sweep' events, and that event's time is what dates the dormancy. now is injected so tests control the clock.
func Sweep(ctx context.Context, db *sql.DB, checker anchor.Checker, now func() time.Time) (SweepReport, error) {
	var rep SweepReport
	at := now().UTC()

	ids, err := queryStrings(ctx, db, `SELECT id FROM entries WHERE tier != 'dormant'`)
	if err != nil {
		return rep, err
	}
	// One entry that cannot be checked must not stop the expiry pass; its error is returned after the pass.
	var checkErr error
	for _, id := range ids {
		if _, err := anchor.RefreshEntry(ctx, db, checker, id); err != nil {
			checkErr = errors.Join(checkErr, err)
			continue
		}
		rep.Checked++
	}

	// last_touch is the newest of the entry's last read, its dormancy event and its creation.
	rows, err := db.QueryContext(ctx, `SELECT e.id, e.tier, e.created_at,
			COALESCE((SELECT MAX(at) FROM events WHERE tool = 'read' AND target = e.id AND result = 'ok'), ''),
			COALESCE((SELECT MAX(at) FROM events WHERE tool = 'sweep' AND target = e.id AND result = 'dormant'), '')
		FROM entries e
		WHERE e.superseded_by IS NULL AND (e.tier = 'provisional' OR e.tier = 'dormant')`)
	if err != nil {
		return rep, err
	}
	type cand struct {
		id, tier string
		touch    time.Time
	}
	var cands []cand
	for rows.Next() {
		var id, tier, created, read, dormant string
		if err := rows.Scan(&id, &tier, &created, &read, &dormant); err != nil {
			rows.Close()
			return rep, err
		}
		c := cand{id: id, tier: tier}
		for _, s := range []string{created, read, dormant} {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil && t.After(c.touch) {
				c.touch = t
			}
		}
		cands = append(cands, c)
	}
	if err := rows.Close(); err != nil {
		return rep, err
	}
	stamp := at.Format(time.RFC3339Nano)
	for _, c := range cands {
		if at.Sub(c.touch) < expiryAfter {
			continue
		}
		if c.tier == "provisional" {
			if _, err := db.ExecContext(ctx, `UPDATE entries SET tier = 'dormant' WHERE id = ?`, c.id); err != nil {
				return rep, err
			}
			if err := sweepEvent(ctx, db, stamp, c.id, "dormant"); err != nil {
				return rep, err
			}
			rep.Dormant++
			continue
		}
		if err := deleteEntry(ctx, db, stamp, c.id); err != nil {
			return rep, err
		}
		rep.Deleted++
	}

	res, err := db.ExecContext(ctx, `UPDATE sessions SET ended_at = ?, ended_by = 'timeout' WHERE ended_at IS NULL AND
		COALESCE((SELECT MAX(at) FROM events WHERE session_id = sessions.id), started_at) <= ?`,
		stamp, at.Add(-sessionIdle).Format(time.RFC3339Nano))
	if err != nil {
		return rep, err
	}
	n, _ := res.RowsAffected()
	rep.Closed = int(n)
	return rep, checkErr
}

// deleteEntry removes an expired entry with its anchors, versions, links and aliases, and keeps its id out of reuse.
func deleteEntry(ctx context.Context, db *sql.DB, stamp, id string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	steps := []struct {
		q    string
		args []any
	}{
		{`INSERT OR IGNORE INTO ids (id, freed_at) VALUES (?, ?)`, []any{id, stamp}},
		{`DELETE FROM entries_fts WHERE entry_id = ?`, []any{id}},
		{`DELETE FROM entries WHERE id = ?`, []any{id}},
		{`INSERT INTO events (session_id, at, tool, target, result) VALUES (?, ?, ?, ?, 'deleted')`, []any{sweepActor, stamp, sweepTool, id}},
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.q, st.args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func sweepEvent(ctx context.Context, db *sql.DB, stamp, id, result string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO events (session_id, at, tool, target, result) VALUES (?, ?, ?, ?, ?)`, sweepActor, stamp, sweepTool, id, result)
	return err
}

func queryStrings(ctx context.Context, db *sql.DB, q string) ([]string, error) {
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
