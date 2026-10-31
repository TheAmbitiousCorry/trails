package trail

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/gitx"
	"github.com/TheAmbitiousCorry/trails/internal/staged"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

type heldEntry struct {
	id, branch, baseName, base, landing, over string
	// overVersion is the original's version count when the copy was staged.
	overVersion sql.NullInt64
}

// stagedPass is what one pass over the staged entries did.
type stagedPass struct {
	promoted, abandoned int
	// baseUnresolved is set when an entry's base branch has no ref at all; that entry is left exactly as it was.
	baseUnresolved bool
}

// promoteStaged pins the landing commit of each staged entry whose work is now committed, promotes the entries whose work reached the base branch, and abandons the ones whose branch is gone.
// Work reached the base branch when the landing commit is an ancestor of a base ref (merged), or when every file and range anchor exists on one base ref with its stored hash (landed by content: a squash, rebase or cherry-pick).
// checker.Repo is the primary checkout; refs are shared by every worktree of the repository, so it sees every branch. The pass never fetches.
// A promoted entry is an ordinary entry from here on: the sweep's anchor pass that follows checks it against the primary like any other, so a revert or later work makes it stale.
func promoteStaged(ctx context.Context, db *sql.DB, checker anchor.Checker, stamp string) (stagedPass, error) {
	var pass stagedPass
	top := checker.Repo
	rows, err := db.QueryContext(ctx, `SELECT id, staged_branch, COALESCE(staged_base_name, ''), COALESCE(staged_base, ''), COALESCE(staged_landing, ''), COALESCE(stages_over, ''), stages_over_version
		FROM entries WHERE staged_branch IS NOT NULL AND tier != 'dormant' ORDER BY created_at, id`)
	if err != nil {
		return pass, err
	}
	var held []heldEntry
	for rows.Next() {
		var h heldEntry
		if err := rows.Scan(&h.id, &h.branch, &h.baseName, &h.base, &h.landing, &h.over, &h.overVersion); err != nil {
			rows.Close()
			return pass, err
		}
		held = append(held, h)
	}
	if err := rows.Close(); err != nil {
		return pass, err
	}
	for _, h := range held {
		refs := staged.BaseRefs(ctx, top, h.baseName)
		if len(refs) == 0 {
			pass.baseUnresolved = true
			continue
		}
		if h.landing == "" {
			if h.landing, err = pinLanding(ctx, db, top, h); err != nil {
				return pass, err
			}
		}
		as, err := anchor.Load(ctx, db, h.id)
		if err != nil {
			return pass, err
		}
		if h.landing != "" && staged.AncestorOfBase(ctx, top, h.landing, refs) || staged.LandedByContent(ctx, checker, refs, h.base, as) {
			if err := promote(ctx, db, h, func(x execer) error { return sweepEvent(ctx, x, stamp, h.id, "promoted") }); err != nil {
				return pass, err
			}
			pass.promoted++
			continue
		}
		if _, alive := gitx.BranchRef(ctx, top, h.branch); alive {
			continue
		}
		if err := abandon(ctx, db, h.id, stamp); err != nil {
			return pass, err
		}
		pass.abandoned++
	}
	return pass, nil
}

// abandon retires an entry whose branch is gone without its work reaching the base branch. It stays held by the branch name, readable by id and promotable by hand.
func abandon(ctx context.Context, db *sql.DB, id, stamp string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE entries SET tier = 'dormant', updated_at = ? WHERE id = ?`, store.Now(), id); err != nil {
		return err
	}
	if err := sweepEvent(ctx, tx, stamp, id, "abandoned"); err != nil {
		return err
	}
	return tx.Commit()
}

// pinLanding stores the first commit on the branch that touches the entry's anchored paths, or for a copy the paths of the original it stages over as well.
// Once stored it is never moved.
func pinLanding(ctx context.Context, db *sql.DB, top string, h heldEntry) (string, error) {
	tip, ok := gitx.BranchRef(ctx, top, h.branch)
	if !ok || h.base == "" {
		return "", nil
	}
	as, err := anchor.Load(ctx, db, h.id)
	if err != nil {
		return "", err
	}
	if h.over != "" {
		original, err := anchor.Load(ctx, db, h.over)
		if err != nil {
			return "", err
		}
		as = append(as, original...)
	}
	landing := staged.Landing(ctx, top, h.base, tip, as)
	if landing == "" {
		return "", nil
	}
	_, err = db.ExecContext(ctx, `UPDATE entries SET staged_landing = ? WHERE id = ? AND staged_landing IS NULL`, landing, h.id)
	return landing, err
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// promote makes one staged entry normal and settles the original it stages over, in one transaction. record writes the event that names who did it.
// An abandoned entry is dormant; promoting it by hand returns it to the provisional tier, where the normal checks take over.
func promote(ctx context.Context, db *sql.DB, h heldEntry, record func(execer) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE entries SET staged_branch = NULL, tier = CASE WHEN tier = 'dormant' THEN 'provisional' ELSE tier END, updated_at = ? WHERE id = ?`, store.Now(), h.id); err != nil {
		return err
	}
	if h.over != "" {
		if err := settleOriginal(ctx, tx, h); err != nil {
			return err
		}
	}
	if err := record(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ErrNotStaged is returned when a hand promotion names an entry that is not held by a branch.
var ErrNotStaged = errors.New("entry is not staged")

// ErrUnknownStaged is returned when a hand promotion names no entry.
var ErrUnknownStaged = errors.New("unknown entry")

// PromoteByHand promotes one staged entry whatever its branch has done, by the same code the sweep runs, logged as the user's 'promote' event.
// Settling the original follows the sweep's rule, so a verified or changed original still ends as a conflicts_with pair. The entry's anchors are then checked like any normal entry's.
func PromoteByHand(ctx context.Context, db *sql.DB, checker anchor.Checker, id string, now func() time.Time) error {
	var h heldEntry
	err := db.QueryRowContext(ctx, `SELECT id, COALESCE(staged_branch, ''), COALESCE(staged_base_name, ''), COALESCE(staged_base, ''), COALESCE(staged_landing, ''), COALESCE(stages_over, ''), stages_over_version
		FROM entries WHERE id = ?`, id).Scan(&h.id, &h.branch, &h.baseName, &h.base, &h.landing, &h.over, &h.overVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnknownStaged
	}
	if err != nil {
		return err
	}
	if h.branch == "" {
		return ErrNotStaged
	}
	stamp := now().UTC().Format(time.RFC3339Nano)
	if err := promote(ctx, db, h, func(x execer) error {
		_, err := x.ExecContext(ctx, `INSERT INTO events (session_id, at, tool, target, result) VALUES ('user', ?, 'promote', ?, 'ok')`, stamp, id)
		return err
	}); err != nil {
		return err
	}
	_, err = anchor.RefreshEntry(ctx, db, checker, id)
	return err
}

// settleOriginal never overrides a human. The promoted copy replaces its original (as an agent write with resolution replace does) only when the original is not verified and has not been written since the copy was staged;
// otherwise the copy stays a provisional entry linked conflicts_with and the human resolves the pair on the entry page. An original that is gone or already retired leaves the copy as a plain entry.
func settleOriginal(ctx context.Context, tx *sql.Tx, h heldEntry) error {
	var tier string
	var versions int
	var retired sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(tier_before_stale, tier), superseded_by, (SELECT COUNT(*) FROM versions WHERE entry_id = entries.id) FROM entries WHERE id = ?`, h.over).Scan(&tier, &retired, &versions)
	if errors.Is(err, sql.ErrNoRows) || retired.Valid {
		return nil
	}
	if err != nil {
		return err
	}
	if tier != "verified" && h.overVersion.Valid && int64(versions) == h.overVersion.Int64 {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO links (from_id, to_id, relation) VALUES (?, ?, 'supersedes')`, h.id, h.over); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE entries SET superseded_by = ?, tier = 'dormant', updated_at = ? WHERE id = ?`, h.id, store.Now(), h.over)
		return err
	}
	lo, hi := h.id, h.over
	if lo > hi {
		lo, hi = hi, lo
	}
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO links (from_id, to_id, relation) VALUES (?, ?, 'conflicts_with')`, lo, hi)
	return err
}
