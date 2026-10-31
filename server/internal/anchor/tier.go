package anchor

import (
	"context"
	"database/sql"
	"errors"
	"github.com/TheAmbitiousCorry/trails/internal/view"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

const cols = `id, entry_id, type, COALESCE(path_or_url,''), COALESCE(range_start,0), COALESCE(range_end,0), COALESCE(hash,''),
	COALESCE(fetched_at,''), COALESCE(last_checked_at,''), ok, COALESCE(extra,'')`

// Load returns an entry's anchors in insertion order.
func Load(ctx context.Context, db *sql.DB, entryID string) ([]Anchor, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+cols+` FROM anchors WHERE entry_id = ? ORDER BY id`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Anchor
	for rows.Next() {
		var a Anchor
		var ok int
		if err := rows.Scan(&a.ID, &a.EntryID, &a.Type, &a.PathOrURL, &a.RangeStart, &a.RangeEnd, &a.Hash, &a.FetchedAt, &a.LastCheckedAt, &ok, &a.Extra); err != nil {
			return nil, err
		}
		a.OK = ok != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

// Insert stores a resolved anchor and returns its id.
func Insert(ctx context.Context, db *sql.DB, a Anchor) (int64, error) {
	ok := 0
	if a.OK {
		ok = 1
	}
	res, err := db.ExecContext(ctx, `INSERT INTO anchors (entry_id, type, path_or_url, range_start, range_end, hash, fetched_at, last_checked_at, ok, extra)
		VALUES (?, ?, NULLIF(?,''), NULLIF(?,0), NULLIF(?,0), NULLIF(?,''), NULLIF(?,''), NULLIF(?,''), ?, NULLIF(?,''))`,
		a.EntryID, a.Type, a.PathOrURL, a.RangeStart, a.RangeEnd, a.Hash, a.FetchedAt, a.LastCheckedAt, ok, a.Extra)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Tier is stale when any anchor last failed its check, else the stored tier.
func Tier(ctx context.Context, db *sql.DB, entryID string) (string, error) {
	var tier string
	var failed bool
	err := db.QueryRowContext(ctx, `SELECT tier, EXISTS(SELECT 1 FROM anchors WHERE entry_id = entries.id AND ok = 0) FROM entries WHERE id = ?`, entryID).Scan(&tier, &failed)
	if failed {
		return "stale", err
	}
	return tier, err
}

// RefreshEntry re-checks every anchor of the entry and returns the ones that failed.
// A failure parks the current tier in tier_before_stale; all passing again restores it.
//
// An entry staged on a branch is checked where its work is: against a bound checkout on that branch, else the branch ref's objects. Once the branch is gone it is left as it was.
func RefreshEntry(ctx context.Context, db *sql.DB, c Checker, entryID string) ([]Anchor, error) {
	c, ok, err := stagedSource(ctx, db, c, entryID)
	if err != nil || !ok {
		return nil, err
	}
	as, err := Load(ctx, db, entryID)
	if err != nil || len(as) == 0 {
		return nil, err
	}
	now := store.Now()
	var failed []Anchor
	for i := range as {
		ok, err := c.Check(ctx, as[i])
		if err != nil {
			return nil, err
		}
		as[i].OK, as[i].LastCheckedAt = ok, now
		if !ok {
			failed = append(failed, as[i])
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := storeChecks(ctx, tx, as, now); err != nil {
		return nil, err
	}
	if err := setTier(ctx, tx, entryID, len(failed) > 0); err != nil {
		return nil, err
	}
	return failed, tx.Commit()
}

// Reattach resolves every anchor of the entry against the repository and the web, stores the resolved hashes in place of the stored ones, and sets the tier as RefreshEntry does.
// An anchor that does not resolve keeps its stored hash, is marked failed and is returned, so the entry stays stale.
func Reattach(ctx context.Context, db *sql.DB, c Checker, entryID string) ([]Anchor, error) {
	if src, ok, err := stagedSource(ctx, db, c, entryID); err != nil {
		return nil, err
	} else if ok {
		c = src
	}
	as, err := Load(ctx, db, entryID)
	if err != nil || len(as) == 0 {
		return nil, err
	}
	now := store.Now()
	var failed []Anchor
	for i := range as {
		r, err := c.Resolve(ctx, as[i])
		if cerr := ctx.Err(); cerr != nil {
			return nil, cerr
		}
		if err != nil {
			as[i].OK, as[i].LastCheckedAt = false, now
			failed = append(failed, as[i])
			continue
		}
		as[i].Hash, as[i].FetchedAt, as[i].OK, as[i].LastCheckedAt = r.Hash, r.FetchedAt, true, now
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := storeChecks(ctx, tx, as, now); err != nil {
		return nil, err
	}
	if err := setTier(ctx, tx, entryID, len(failed) > 0); err != nil {
		return nil, err
	}
	return failed, tx.Commit()
}

// stagedSource is the checker to use for the entry: c itself for a normal entry, the branch's checker for a staged one.
// ok is false when the entry is staged on a branch that no longer exists, which has nothing to check against.
func stagedSource(ctx context.Context, db *sql.DB, c Checker, entryID string) (Checker, bool, error) {
	var branch sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT staged_branch FROM entries WHERE id = ?`, entryID).Scan(&branch); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return c, false, err
	}
	if !branch.Valid {
		return c, true, nil
	}
	src, ok := c.ForBranch(ctx, branch.String)
	return src, ok, nil
}

// storeChecks writes each anchor's hash, fetch time, check time and result.
func storeChecks(ctx context.Context, tx *sql.Tx, as []Anchor, now string) error {
	for _, a := range as {
		ok := 0
		if a.OK {
			ok = 1
		}
		if _, err := tx.ExecContext(ctx, `UPDATE anchors SET hash = NULLIF(?,''), fetched_at = NULLIF(?,''), last_checked_at = ?, ok = ? WHERE id = ?`,
			a.Hash, a.FetchedAt, now, ok, a.ID); err != nil {
			return err
		}
	}
	return nil
}

// setTier parks the entry's tier as stale when an anchor failed, and restores it once none has.
func setTier(ctx context.Context, tx *sql.Tx, entryID string, failed bool) error {
	var err error
	if failed {
		_, err = tx.ExecContext(ctx, `UPDATE entries SET tier_before_stale = tier, tier = 'stale' WHERE id = ? AND tier NOT IN ('stale', 'dormant')`, entryID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE entries SET tier = COALESCE(tier_before_stale, 'provisional'), tier_before_stale = NULL WHERE id = ? AND tier = 'stale'`, entryID)
	}
	return err
}

// StaleCount counts the stale entries anywhere below a sign, whether the tier or a failed anchor says so.
func StaleCount(ctx context.Context, db *sql.DB, signID string) (int, error) {
	rows, err := db.QueryContext(ctx, `WITH RECURSIVE sub(id) AS (
			SELECT id FROM signs WHERE id = ?
			UNION ALL
			SELECT s.id FROM signs s JOIN sub ON s.parent_id = sub.id)
		SELECT e.id FROM entries e
		WHERE e.sign_id IN (SELECT id FROM sub) AND e.tier != 'dormant'
		  AND (e.tier = 'stale' OR EXISTS(SELECT 1 FROM anchors a WHERE a.entry_id = e.id AND a.ok = 0))`, signID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	viewer, n := view.From(ctx), 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		if viewer.Sees(id) {
			n++
		}
	}
	return n, rows.Err()
}
