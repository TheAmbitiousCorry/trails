package mcp_test

import (
	"database/sql"
	"os/exec"
	"strings"
	"testing"
)

// stagedOnX writes the zebra summary about a new file on feat/x and commits the work, then sweeps so the landing commit is pinned.
func stagedOnX(t *testing.T) (main, wx, addr, pid, id string, db *sql.DB) {
	t.Helper()
	main, addr, pid, db = stagedServerDB(t)
	wx = worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	id = mustZebra(t, agentAt(t, addr, wx), "src/new.go")["entry_id"].(string)
	gitIn(t, wx, "add", ".")
	gitIn(t, wx, "commit", "-qm", "add new")
	forceSweep(t, addr, pid)
	if row(t, db, `SELECT COALESCE(staged_landing,'') FROM entries WHERE id = ?`, id) == "" {
		t.Fatal("the sweep did not pin the landing commit")
	}
	return main, wx, addr, pid, id, db
}

func tierAndBranch(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	return row(t, db, `SELECT tier || '/' || COALESCE(staged_branch,'-') FROM entries WHERE id = ?`, id)
}

func dropBranch(t *testing.T, main, wx, branch string) {
	t.Helper()
	gitIn(t, main, "worktree", "remove", "--force", wx)
	gitIn(t, main, "branch", "-D", branch)
}

func TestSquashMergedBranchPromotesByContent(t *testing.T) {
	main, _, addr, pid, id, db := stagedOnX(t)
	gitIn(t, main, "merge", "--squash", "feat/x")
	gitIn(t, main, "commit", "-qm", "squash feat/x")

	if rep := forceSweep(t, addr, pid); rep["promoted"] != float64(1) {
		t.Fatalf("sweep = %v, want promoted 1", rep)
	}
	if got := tierAndBranch(t, db, id); got != "provisional/-" {
		t.Errorf("entry = %s, want a normal provisional entry", got)
	}
	wantSees(t, "base session", agentAt(t, addr, main), id, true)
}

func TestRebaseMergedBranchPromotesByContent(t *testing.T) {
	main, wx, addr, pid, id, db := stagedOnX(t)
	putFile(t, main, "src/later.go", "package a\n")
	gitIn(t, main, "add", ".")
	gitIn(t, main, "commit", "-qm", "base moves on")
	gitIn(t, wx, "rebase", "-q", "main")
	gitIn(t, main, "merge", "-q", "--ff-only", "feat/x")

	if rep := forceSweep(t, addr, pid); rep["promoted"] != float64(1) {
		t.Fatalf("sweep = %v, want promoted 1", rep)
	}
	if got := tierAndBranch(t, db, id); got != "provisional/-" {
		t.Errorf("entry = %s, want a normal provisional entry", got)
	}
}

func TestContentRuleNeedsEveryAnchorToMatch(t *testing.T) {
	main, wx, addr, pid, _, db := stagedOnX(t)
	putFile(t, wx, "src/two.go", "package a\n\nfunc Two() {}\n")
	id2 := mustZebra2(t, agentAt(t, addr, wx), "src/new.go", "src/two.go")
	gitIn(t, wx, "add", ".")
	gitIn(t, wx, "commit", "-qm", "add two")
	forceSweep(t, addr, pid)
	// Squash only the first commit's file onto base by hand: src/two.go is absent there.
	gitIn(t, main, "checkout", "feat/x", "--", "src/new.go")
	gitIn(t, main, "commit", "-qm", "partial")

	forceSweep(t, addr, pid)
	if got := tierAndBranch(t, db, id2); got != "provisional/feat/x" {
		t.Errorf("entry = %s, want still staged: one of its two files is missing on base", got)
	}
}

func mustZebra2(t *testing.T, a *agent, p1, p2 string) string {
	t.Helper()
	out, err := a.raw("write", map[string]any{"parent_sign_id": "root", "kind": "procedure", "title": "Crossing release steps",
		"read_when": "you release the crossing queue by hand", "body": "Open the first file, then the second, then confirm the queue drained before closing.",
		"anchors": []map[string]any{{"type": "file", "path": p1}, {"type": "file", "path": p2}}})
	if err != nil || out.IsError {
		t.Fatalf("write: %v %+v", err, out)
	}
	return structured(t, out, err)["entry_id"].(string)
}

func TestBranchDeletedUnmergedIsAbandoned(t *testing.T) {
	main, wx, addr, pid, id, db := stagedOnX(t)
	dropBranch(t, main, wx, "feat/x")

	rep := forceSweep(t, addr, pid)
	if rep["abandoned"] != float64(1) || rep["promoted"] != float64(0) {
		t.Fatalf("sweep = %v, want abandoned 1", rep)
	}
	if got := tierAndBranch(t, db, id); got != "dormant/feat/x" {
		t.Errorf("entry = %s, want dormant and still held by its branch", got)
	}
	if got := row(t, db, `SELECT result FROM events WHERE tool = 'sweep' AND target = ? ORDER BY id DESC LIMIT 1`, id); got != "abandoned" {
		t.Errorf("sweep event result = %q, want abandoned", got)
	}
	base := agentAt(t, addr, main)
	wantSees(t, "base session", base, id, false)
	if got := base.ok("read", map[string]any{"entry_id": id}); got["tier"] != "dormant" {
		t.Errorf("read by id = %v, want the dormant entry", got["tier"])
	}
	if rep := forceSweep(t, addr, pid); rep["abandoned"] != float64(0) {
		t.Errorf("a second sweep abandoned it again: %v", rep)
	}
}

func TestBranchDeletedAfterASquashWhoseFileLaterChangedOnBaseIsAbandoned(t *testing.T) {
	main, wx, addr, pid, id, db := stagedOnX(t)
	gitIn(t, main, "merge", "--squash", "feat/x")
	gitIn(t, main, "commit", "-qm", "squash feat/x")
	dropBranch(t, main, wx, "feat/x")
	putFile(t, main, "src/new.go", "package a\n\nfunc New() { /* edited on base before any sweep */ }\n")
	gitIn(t, main, "commit", "-qam", "base edits new.go")

	rep := forceSweep(t, addr, pid)
	if rep["abandoned"] != float64(1) || rep["promoted"] != float64(0) {
		t.Fatalf("sweep = %v, want abandoned 1 and promoted 0", rep)
	}
	if got := tierAndBranch(t, db, id); got != "dormant/feat/x" {
		t.Errorf("entry = %s, want dormant, held for a human to promote", got)
	}
}

func TestPrunedLandingCommitReadsFalseAndTheBranchIsStillAbandoned(t *testing.T) {
	main, wx, addr, pid, id, db := stagedOnX(t)
	landing := row(t, db, `SELECT staged_landing FROM entries WHERE id = ?`, id)
	dropBranch(t, main, wx, "feat/x")
	gitIn(t, main, "reflog", "expire", "--expire=now", "--all")
	gitIn(t, main, "gc", "-q", "--prune=now")
	if out, err := gitErr(main, "cat-file", "-e", landing); err == nil {
		t.Fatalf("the landing commit survived the prune: %s", out)
	}

	rep := forceSweep(t, addr, pid)
	if rep["abandoned"] != float64(1) {
		t.Fatalf("sweep = %v, want abandoned 1", rep)
	}
}

func TestRevertAfterMergeIsPromotedStale(t *testing.T) {
	main, _, addr, pid, id, db := stagedOnX(t)
	gitIn(t, main, "merge", "-q", "--no-ff", "-m", "merge feat/x", "feat/x")
	gitIn(t, main, "rm", "-q", "src/new.go")
	gitIn(t, main, "commit", "-qm", "revert the new file")

	if rep := forceSweep(t, addr, pid); rep["promoted"] != float64(1) {
		t.Fatalf("sweep = %v, want promoted 1", rep)
	}
	if got := tierAndBranch(t, db, id); got != "stale/-" {
		t.Errorf("entry = %s, want promoted and stale in the same sweep", got)
	}
}

func TestLocalBaseBehindItsRemoteRefPromotesOnTheRemoteRef(t *testing.T) {
	main, wx, addr, pid, id, db := stagedOnX(t)
	gitIn(t, main, "update-ref", "refs/remotes/origin/main", gitIn(t, wx, "rev-parse", "HEAD"))

	if rep := forceSweep(t, addr, pid); rep["promoted"] != float64(1) {
		t.Fatalf("sweep = %v, want promoted 1 on origin/main while main is behind", rep)
	}
	// The primary checkout is still behind the remote ref, so the normal check that follows finds the file missing and the entry reads stale until the checkout catches up.
	if got := row(t, db, `SELECT COALESCE(staged_branch,'-') FROM entries WHERE id = ?`, id); got != "-" {
		t.Errorf("staged_branch = %s, want promoted", got)
	}
}

func TestUnresolvedBaseIsReportedAndChangesNothing(t *testing.T) {
	main, wx, addr, pid, id, db := stagedOnX(t)
	dropBranch(t, main, wx, "feat/x")
	gitIn(t, main, "branch", "-m", "main", "trunk")

	rep := forceSweep(t, addr, pid)
	if rep["base_unresolved"] != true {
		t.Fatalf("sweep = %v, want base_unresolved", rep)
	}
	if rep["abandoned"] != float64(0) || rep["promoted"] != float64(0) {
		t.Errorf("sweep = %v, want nothing changed", rep)
	}
	if got := tierAndBranch(t, db, id); got != "provisional/feat/x" {
		t.Errorf("entry = %s, want untouched", got)
	}
}

func TestStagedEntryIsExemptFromExpiryWhileItsBranchLives(t *testing.T) {
	main, wx, addr, pid, id, db := stagedOnX(t)
	old := "2020-01-01T00:00:00Z"
	if _, err := db.Exec(`UPDATE entries SET created_at = ?`, old); err != nil {
		t.Fatal(err)
	}
	normal := mustZebra(t, agentAt(t, addr, main), "src/a.go")["entry_id"].(string)
	if _, err := db.Exec(`UPDATE entries SET created_at = ? WHERE id = ?`, old, normal); err != nil {
		t.Fatal(err)
	}
	_ = wx

	forceSweep(t, addr, pid)
	if got := tierAndBranch(t, db, id); got != "provisional/feat/x" {
		t.Errorf("staged entry = %s, want it kept while feat/x exists", got)
	}
	if got := tierAndBranch(t, db, normal); got != "dormant/-" {
		t.Errorf("control normal entry = %s, want expired", got)
	}
}

func TestStagedEntryIsCheckedAgainstTheCheckoutOnItsBranch(t *testing.T) {
	main, addr, _, _ := stagedServerDB(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	onX := agentAt(t, addr, wx)
	id := mustZebra(t, onX, "src/new.go")["entry_id"].(string)
	read := func() any { return onX.ok("read", map[string]any{"entry_id": id, "depth": "full"})["tier"] }

	if got := read(); got != "provisional" {
		t.Fatalf("fresh staged entry tier = %v", got)
	}
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() { /* changed */ }\n")
	if got := read(); got != "stale" {
		t.Errorf("after the anchored file changed in the branch checkout, tier = %v, want stale", got)
	}
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	if got := read(); got != "provisional" {
		t.Errorf("after the file is restored, tier = %v, want provisional", got)
	}
}

func TestStagedEntryIsNotStaleAfterItsWorktreeIsRemovedWhileTheBranchLives(t *testing.T) {
	main, wx, addr, pid, id, db := stagedOnX(t)
	gitIn(t, main, "worktree", "remove", "--force", wx)

	forceSweep(t, addr, pid)
	if got := tierAndBranch(t, db, id); got != "provisional/feat/x" {
		t.Errorf("entry = %s, want checked against the branch ref and still current", got)
	}
	base := agentAt(t, addr, main)
	if got := base.ok("read", map[string]any{"entry_id": id})["tier"]; got != "provisional" {
		t.Errorf("read tier = %v, want provisional", got)
	}
	if got := row(t, db, `SELECT ok FROM anchors WHERE entry_id = ?`, id); got != "1" {
		t.Errorf("anchor ok = %s, want 1", got)
	}
}

func TestStagedEntryChangedOnTheBranchRefGoesStaleWithoutAWorktree(t *testing.T) {
	main, wx, addr, pid, id, db := stagedOnX(t)
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() { /* committed change */ }\n")
	gitIn(t, wx, "commit", "-qam", "change new")
	gitIn(t, main, "worktree", "remove", "--force", wx)

	forceSweep(t, addr, pid)
	if got := tierAndBranch(t, db, id); got != "stale/feat/x" {
		t.Errorf("entry = %s, want stale: the branch ref's blob no longer matches", got)
	}
}

func TestConfirmOfAStagedEntryReAnchorsAgainstItsBranch(t *testing.T) {
	main, addr, _, _ := stagedServerDB(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	id := mustZebra(t, agentAt(t, addr, wx), "src/new.go")["entry_id"].(string)
	b, c := agentAt(t, addr, wx), agentAt(t, addr, wx)
	b.readFull(id)
	b.ok("confirm", map[string]any{"entry_id": id})
	c.readFull(id)
	if got := c.ok("confirm", map[string]any{"entry_id": id}); got["tier"] != "verified" {
		t.Errorf("second confirm = %v, want verified", got)
	}
}

func TestCopyWhoseOwnAnchorsNeverTouchTheBranchWorkIsPinnedAndPromoted(t *testing.T) {
	main, addr, pid, db := stagedServerDB(t)
	putFile(t, main, "src/b.go", "package a\n")
	gitIn(t, main, "add", ".")
	gitIn(t, main, "commit", "-qm", "b")
	orig := mustZebra(t, agentAt(t, addr, main), "src/a.go")["entry_id"].(string)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/a.go", "package a\n\n// reworked on feat/x\n")
	cp := agentAt(t, addr, wx).ok("write", map[string]any{"entry_id": orig, "body": zebraV2,
		"anchors": []map[string]any{{"type": "file", "path": "src/b.go"}}})["entry_id"].(string)
	// The work is committed only after the write, so the write could not pin a landing commit.
	gitIn(t, wx, "add", ".")
	gitIn(t, wx, "commit", "-qm", "rework a")
	forceSweep(t, addr, pid)
	if got := tierAndBranch(t, db, cp); got != "provisional/feat/x" {
		t.Fatalf("copy = %s, want still staged: its own anchor is an untouched file that matches base trivially", got)
	}
	if row(t, db, `SELECT COALESCE(staged_landing,'') FROM entries WHERE id = ?`, cp) == "" {
		t.Fatal("the sweep did not pin the copy's landing commit over the original's anchors")
	}
	gitIn(t, main, "merge", "-q", "--no-ff", "-m", "merge feat/x", "feat/x")
	if rep := forceSweep(t, addr, pid); rep["promoted"] != float64(1) {
		t.Fatalf("sweep = %v, want the copy promoted", rep)
	}
}

func writeAnchorlessDecision(a *agent, title string, staged bool) (map[string]any, error) {
	args := map[string]any{"parent_sign_id": "root", "kind": "decision", "title": title, "read_when": "you decide about " + title,
		"body": "We chose this on the branch because the old way broke the nightly run.", "decided_by": "ana", "decided_at": "2026-10-31"}
	if staged {
		args["staged"] = true
	}
	res, err := a.raw("write", args)
	if err != nil {
		return nil, err
	}
	out := structured(a.t, res, nil)
	return out, nil
}

func hasHit(a *agent, readWhen, id string) bool {
	hits, _ := a.ok("search", map[string]any{"query": readWhen})["hits"].([]any)
	for _, h := range hits {
		if h.(map[string]any)["id"] == id {
			return true
		}
	}
	return false
}

func TestAnchorlessDecisionIsStagedOnlyWithTheFlagAndPromotesOnMerge(t *testing.T) {
	main, addr, pid, db := stagedServerDB(t)
	wx := worktree(t, main, "feat/x", "main")
	onX, base := agentAt(t, addr, wx), agentAt(t, addr, main)

	if out, _ := writeAnchorlessDecision(onX, "unflagged cache policy", false); out["staged"] != nil {
		t.Fatalf("an unflagged anchorless write was staged: %v", out)
	}
	if out, _ := writeAnchorlessDecision(base, "base cache policy", true); out["staged"] != nil {
		t.Fatalf("the flag staged a write from the base branch: %v", out)
	}
	out, _ := writeAnchorlessDecision(onX, "retry budget", true)
	id, _ := out["entry_id"].(string)
	if out["staged"] != "feat/x" {
		t.Fatalf("flagged write = %v, want staged on feat/x", out)
	}
	if hasHit(base, "you decide about retry budget", id) || !hasHit(onX, "you decide about retry budget", id) {
		t.Fatal("the staged decision must show on feat/x and not on base")
	}

	putFile(t, wx, "src/work.go", "package a\n")
	gitIn(t, wx, "add", ".")
	gitIn(t, wx, "commit", "-qm", "first commit on feat/x")
	forceSweep(t, addr, pid)
	if row(t, db, `SELECT COALESCE(staged_landing,'') FROM entries WHERE id = ?`, id) == "" {
		t.Fatal("the sweep did not pin the first commit on the branch")
	}
	gitIn(t, main, "merge", "-q", "--no-ff", "-m", "merge feat/x", "feat/x")
	if rep := forceSweep(t, addr, pid); rep["promoted"] != float64(1) {
		t.Fatalf("sweep = %v, want promoted 1", rep)
	}
	if !hasHit(base, "you decide about retry budget", id) {
		t.Error("the promoted decision is not visible to base")
	}
}

func TestAnchorlessDecisionStaysDormantAfterSquashAndDelete(t *testing.T) {
	main, addr, pid, db := stagedServerDB(t)
	wx := worktree(t, main, "feat/x", "main")
	out, _ := writeAnchorlessDecision(agentAt(t, addr, wx), "retry budget", true)
	id := out["entry_id"].(string)
	putFile(t, wx, "src/work.go", "package a\n")
	gitIn(t, wx, "add", ".")
	gitIn(t, wx, "commit", "-qm", "work")
	forceSweep(t, addr, pid)
	gitIn(t, main, "merge", "--squash", "feat/x")
	gitIn(t, main, "commit", "-qm", "squash")
	dropBranch(t, main, wx, "feat/x")

	rep := forceSweep(t, addr, pid)
	if rep["abandoned"] != float64(1) || rep["promoted"] != float64(0) {
		t.Fatalf("sweep = %v, want abandoned: an anchorless entry has no content rule", rep)
	}
	if got := tierAndBranch(t, db, id); got != "dormant/feat/x" {
		t.Errorf("entry = %s, want dormant until a human promotes it", got)
	}
}

func gitErr(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
