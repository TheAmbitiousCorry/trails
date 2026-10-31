package trail

import (
	"context"
	"database/sql"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/gitx"
	"github.com/TheAmbitiousCorry/trails/internal/staged"
)

type heldEntry struct {
	id, branch, baseName, base, landing string
}

// promoteStaged pins the landing commit of each staged entry whose work is now committed, then promotes the entries whose landing commit is on the base branch.
// top is the primary checkout; refs are shared by every worktree of the repository, so it sees every branch.
// A promoted entry is an ordinary entry from here on: the sweep's anchor pass that follows checks it against the primary like any other.
func promoteStaged(ctx context.Context, db *sql.DB, top, stamp string) (int, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, staged_branch, COALESCE(staged_base_name, ''), COALESCE(staged_base, ''), COALESCE(staged_landing, '')
		FROM entries WHERE staged_branch IS NOT NULL AND tier != 'dormant' ORDER BY created_at, id`)
	if err != nil {
		return 0, err
	}
	var held []heldEntry
	for rows.Next() {
		var h heldEntry
		if err := rows.Scan(&h.id, &h.branch, &h.baseName, &h.base, &h.landing); err != nil {
			rows.Close()
			return 0, err
		}
		held = append(held, h)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	promoted := 0
	for _, h := range held {
		if h.landing == "" {
			if h.landing, err = pinLanding(ctx, db, top, h); err != nil {
				return promoted, err
			}
		}
		if !staged.Merged(ctx, top, h.landing, h.baseName) {
			continue
		}
		if _, err := db.ExecContext(ctx, `UPDATE entries SET staged_branch = NULL WHERE id = ?`, h.id); err != nil {
			return promoted, err
		}
		if err := sweepEvent(ctx, db, stamp, h.id, "promoted"); err != nil {
			return promoted, err
		}
		promoted++
	}
	return promoted, nil
}

// pinLanding stores the first commit on the branch that touches the entry's anchored paths. Once stored it is never moved.
func pinLanding(ctx context.Context, db *sql.DB, top string, h heldEntry) (string, error) {
	tip := "refs/heads/" + h.branch
	if _, ok := gitx.Verify(ctx, top, tip+"^{commit}"); !ok || h.base == "" {
		return "", nil
	}
	as, err := anchor.Load(ctx, db, h.id)
	if err != nil {
		return "", err
	}
	landing := staged.Landing(ctx, top, h.base, tip, as)
	if landing == "" {
		return "", nil
	}
	_, err = db.ExecContext(ctx, `UPDATE entries SET staged_landing = ? WHERE id = ? AND staged_landing IS NULL`, landing, h.id)
	return landing, err
}
