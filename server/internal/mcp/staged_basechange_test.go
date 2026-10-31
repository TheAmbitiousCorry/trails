package mcp_test

import (
	"database/sql"
	"testing"
)

// settled is true once the entry is no longer held by a branch. The primary here stays on main, so the anchor pass that follows may mark the entry stale; that is not what these tests judge.
func settled(t *testing.T, db *sql.DB, id string) bool {
	t.Helper()
	return row(t, db, `SELECT COALESCE(staged_branch, '-') FROM entries WHERE id = ?`, id) == "-"
}

// setBase writes a base: line into the primary checkout's .trail/map.md, uncommitted; staged.Base reads the file from the working tree.
func setBase(t *testing.T, main, branch string) {
	t.Helper()
	putFile(t, main, ".trail/map.md", "base: "+branch+"\n")
}

func TestEntryStagedOnTheNewBaseBranchPromotesWhenTheBaseChanges(t *testing.T) {
	main, addr, pid, db := stagedServerDB(t)
	ws := worktree(t, main, "staging", "main")
	putFile(t, ws, "src/new.go", "package a\n\nfunc New() {}\n")
	id := mustZebra(t, agentAt(t, addr, ws), "src/new.go")["entry_id"].(string)
	if got := tierAndBranch(t, db, id); got != "provisional/staging" {
		t.Fatalf("entry = %s, want staged on staging", got)
	}

	setBase(t, main, "staging")
	if rep := forceSweep(t, addr, pid); rep["promoted"] != float64(1) {
		t.Fatalf("sweep = %v, want promoted 1", rep)
	}
	if !settled(t, db, id) {
		t.Errorf("entry %s is still staged", id)
	}
}

func TestStageOverCopyOnTheNewBaseBranchReplacesItsOriginal(t *testing.T) {
	main, addr, pid, db := stagedServerDB(t)
	orig := mustZebra(t, agentAt(t, addr, main), "src/a.go")["entry_id"].(string)
	ws := worktree(t, main, "staging", "main")
	putFile(t, ws, "src/a.go", "package a\n\n// reworked on staging\n")
	copyID := agentAt(t, addr, ws).ok("write", map[string]any{"entry_id": orig, "body": zebraV2})["entry_id"].(string)

	setBase(t, main, "staging")
	forceSweep(t, addr, pid)

	if got := row(t, db, `SELECT COALESCE(superseded_by, '') FROM entries WHERE id = ?`, orig); got != copyID {
		t.Errorf("original superseded_by = %q, want %q", got, copyID)
	}
	if !settled(t, db, copyID) {
		t.Errorf("copy %s is still staged", copyID)
	}
}

func TestStageOverCopyOnTheNewBaseBranchOverAVerifiedOriginalIsAPair(t *testing.T) {
	main, addr, pid, db := stagedServerDB(t)
	orig := mustZebra(t, agentAt(t, addr, main), "src/a.go")["entry_id"].(string)
	db.Exec(`UPDATE entries SET tier = 'verified' WHERE id = ?`, orig)
	ws := worktree(t, main, "staging", "main")
	putFile(t, ws, "src/a.go", "package a\n\n// reworked on staging\n")
	copyID := agentAt(t, addr, ws).ok("write", map[string]any{"entry_id": orig, "body": zebraV2})["entry_id"].(string)

	setBase(t, main, "staging")
	forceSweep(t, addr, pid)
	lo, hi := orig, copyID
	if lo > hi {
		lo, hi = hi, lo
	}
	if got := row(t, db, `SELECT COUNT(*) FROM links WHERE from_id = ? AND to_id = ? AND relation = 'conflicts_with'`, lo, hi); got != "1" {
		t.Errorf("conflicts_with links = %s, want 1", got)
	}
	if !settled(t, db, copyID) {
		t.Errorf("copy %s is still staged", copyID)
	}
}

func TestMergedAndLandedAreJudgedAgainstTheCurrentBaseNotTheStoredName(t *testing.T) {
	main, addr, pid, db := stagedServerDB(t)
	gitIn(t, main, "branch", "staging", "main")
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	id := mustZebra(t, agentAt(t, addr, wx), "src/new.go")["entry_id"].(string)
	gitIn(t, wx, "add", ".")
	gitIn(t, wx, "commit", "-qm", "add new")
	forceSweep(t, addr, pid)

	// feat/x reaches staging only; main never sees it.
	ws := worktree(t, main, "stg-work", "staging")
	gitIn(t, ws, "merge", "-q", "--no-ff", "-m", "merge feat/x", "feat/x")
	gitIn(t, ws, "branch", "-f", "staging", "HEAD")
	if rep := forceSweep(t, addr, pid); rep["promoted"] != float64(0) {
		t.Fatalf("promoted against the stored base main: %v", rep)
	}

	setBase(t, main, "staging")
	if rep := forceSweep(t, addr, pid); rep["promoted"] != float64(1) {
		t.Fatalf("sweep = %v, want promoted 1 against the current base", rep)
	}
	if !settled(t, db, id) {
		t.Errorf("entry %s is still staged", id)
	}
}
