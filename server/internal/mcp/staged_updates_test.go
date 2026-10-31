package mcp_test

import (
	"database/sql"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const zebraV2 = "The zebra scheduler decides when crossings open, because pedestrians outrank cars here and always did."

// stagedOriginal writes a normal zebra entry about src/a.go from the base checkout and edits src/a.go on a worktree for feat/x.
func stagedOriginal(t *testing.T) (main, wx, addr, pid, orig string, db *sql.DB) {
	t.Helper()
	main, addr, pid, db = stagedServerDB(t)
	orig = mustZebra(t, agentAt(t, addr, main), "src/a.go")["entry_id"].(string)
	wx = worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/a.go", "package a\n\n// reworked on feat/x\n")
	return main, wx, addr, pid, orig, db
}

func row(t *testing.T, db *sql.DB, q string, args ...any) string {
	t.Helper()
	var s sql.NullString
	if err := db.QueryRow(q, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return s.String
}

func TestUpdateFromABranchIsAStagedCopyThatHidesTheOriginalOnThatBranchOnly(t *testing.T) {
	main, wx, addr, _, orig, db := stagedOriginal(t)
	onX := agentAt(t, addr, wx)

	out := onX.ok("write", map[string]any{"entry_id": orig, "body": zebraV2})
	copyID, _ := out["entry_id"].(string)
	if out["staged"] != "feat/x" || copyID == "" || copyID == orig {
		t.Fatalf("update = %v, want a new entry staged on feat/x", out)
	}
	if got := row(t, db, `SELECT stages_over FROM entries WHERE id = ?`, copyID); got != orig {
		t.Errorf("stages_over = %q, want %q", got, orig)
	}
	if got := row(t, db, `SELECT stages_over_version FROM entries WHERE id = ?`, copyID); got != "1" {
		t.Errorf("stages_over_version = %q, want 1", got)
	}
	if got := row(t, db, `SELECT COUNT(*) FROM versions WHERE entry_id = ?`, orig); got != "1" {
		t.Errorf("the original gained a version (%s); the branch must not touch it", got)
	}

	wantSees(t, "session on feat/x, the copy", onX, copyID, true)
	wantSees(t, "session on feat/x, the original", onX, orig, false)
	base := agentAt(t, addr, main)
	wantSees(t, "base session, the original", base, orig, true)
	wantSees(t, "base session, the copy", base, copyID, false)
	other := worktree(t, main, "feat/y", "main")
	wantSees(t, "another branch, the original", agentAt(t, addr, other), orig, true)
}

func TestUpdateAnchorsAreTheUnionOfTheRequestsAndTheOriginals(t *testing.T) {
	main, addr, _, db := stagedServerDB(t)
	putFile(t, main, "src/b.go", "package a\n")
	gitIn(t, main, "add", ".")
	gitIn(t, main, "commit", "-qm", "b")
	orig := mustZebra(t, agentAt(t, addr, main), "src/a.go")["entry_id"].(string)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/a.go", "package a\n\n// reworked on feat/x\n")

	// The request re-anchors to a file the branch did not touch; the original's file was changed on the branch.
	out := agentAt(t, addr, wx).ok("write", map[string]any{"entry_id": orig, "body": zebraV2, "anchors": []map[string]any{{"type": "file", "path": "src/b.go"}}})
	if out["staged"] != "feat/x" || out["entry_id"] == orig {
		t.Fatalf("update = %v, want a staged copy", out)
	}
	if got := row(t, db, `SELECT path_or_url FROM anchors WHERE entry_id = ?`, out["entry_id"]); got != "src/b.go" {
		t.Errorf("the copy's anchor = %q, want the request's src/b.go", got)
	}
}

func TestUpdateOfAnEntryStagedOnAnotherBranchIsRefused(t *testing.T) {
	main, addr, _ := stagedServer(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	id := mustZebra(t, agentAt(t, addr, wx), "src/new.go")["entry_id"].(string)

	for who, checkout := range map[string]string{"base": main, "feat/y": worktree(t, main, "feat/y", "main")} {
		a := agentAt(t, addr, checkout)
		msg := a.refused("write", map[string]any{"entry_id": id, "body": zebraV2}, "staged_elsewhere")
		if !strings.Contains(msg, "feat/x") {
			t.Errorf("%s: refusal %q does not name the branch", who, msg)
		}
	}
}

func merge(t *testing.T, main, addr, pid, branch string) {
	t.Helper()
	gitIn(t, branch2dir(t, main, branch), "add", ".")
	gitIn(t, branch2dir(t, main, branch), "commit", "-qm", "work on "+branch)
	forceSweep(t, addr, pid)
	gitIn(t, main, "merge", "-q", "--no-ff", "-m", "merge "+branch, branch)
	forceSweep(t, addr, pid)
}

// branch2dir is the worktree checked out on branch.
func branch2dir(t *testing.T, main, branch string) string {
	t.Helper()
	var dir string
	for _, l := range strings.Split(gitIn(t, main, "worktree", "list", "--porcelain"), "\n") {
		if p, ok := strings.CutPrefix(l, "worktree "); ok {
			dir = p
		}
		if l == "branch refs/heads/"+branch {
			return dir
		}
	}
	t.Fatalf("no worktree on %s", branch)
	return ""
}

func TestPromotionReplacesAnUnverifiedUnchangedOriginal(t *testing.T) {
	main, wx, addr, pid, orig, db := stagedOriginal(t)
	copyID := agentAt(t, addr, wx).ok("write", map[string]any{"entry_id": orig, "body": zebraV2})["entry_id"].(string)

	merge(t, main, addr, pid, "feat/x")

	if got := row(t, db, `SELECT superseded_by FROM entries WHERE id = ?`, orig); got != copyID {
		t.Errorf("original superseded_by = %q, want %q", got, copyID)
	}
	if got := row(t, db, `SELECT tier FROM entries WHERE id = ?`, orig); got != "dormant" {
		t.Errorf("original tier = %q, want dormant", got)
	}
	if got := row(t, db, `SELECT COUNT(*) FROM links WHERE from_id = ? AND to_id = ? AND relation = 'supersedes'`, copyID, orig); got != "1" {
		t.Errorf("supersedes links = %s, want 1", got)
	}
	base := agentAt(t, addr, main)
	wantSees(t, "base session, the copy", base, copyID, true)
	wantSees(t, "base session, the original", base, orig, false)
}

func TestPromotionOverAVerifiedOriginalIsAConflictsWithPair(t *testing.T) {
	main, wx, addr, pid, orig, db := stagedOriginal(t)
	db.Exec(`UPDATE entries SET tier = 'verified' WHERE id = ?`, orig)
	out := agentAt(t, addr, wx).ok("write", map[string]any{"entry_id": orig, "body": zebraV2})
	copyID := out["entry_id"].(string)
	if out["staged"] != "feat/x" || out["tier"] != "provisional" {
		t.Fatalf("update of a verified original from a branch = %v, want a provisional staged copy", out)
	}

	merge(t, main, addr, pid, "feat/x")
	assertPair(t, db, orig, copyID, "verified")
}

func TestPromotionOverAnOriginalUpdatedOnBaseSinceStagingIsAConflictsWithPair(t *testing.T) {
	main, wx, addr, pid, orig, db := stagedOriginal(t)
	copyID := agentAt(t, addr, wx).ok("write", map[string]any{"entry_id": orig, "body": zebraV2})["entry_id"].(string)
	agentAt(t, addr, main).ok("write", map[string]any{"entry_id": orig, "body": "The zebra scheduler decides when crossings open, because pedestrians outrank cars on every street."})

	merge(t, main, addr, pid, "feat/x")
	assertPair(t, db, orig, copyID, "provisional")
}

func assertPair(t *testing.T, db *sql.DB, orig, copyID, origTier string) {
	t.Helper()
	lo, hi := orig, copyID
	if lo > hi {
		lo, hi = hi, lo
	}
	if got := row(t, db, `SELECT COUNT(*) FROM links WHERE from_id = ? AND to_id = ? AND relation = 'conflicts_with'`, lo, hi); got != "1" {
		t.Errorf("conflicts_with links = %s, want 1", got)
	}
	if got := row(t, db, `SELECT COALESCE(superseded_by, '') FROM entries WHERE id = ?`, orig); got != "" {
		t.Errorf("original was superseded by %q; promotion must not override it", got)
	}
	// The merge changed the original's anchored file, so it may read stale; the tier it had before is what promotion must leave alone.
	if got := row(t, db, `SELECT COALESCE(tier_before_stale, tier) FROM entries WHERE id = ?`, orig); got != origTier {
		t.Errorf("original tier = %q, want it left as %s", got, origTier)
	}
	if got := row(t, db, `SELECT tier || ' ' || COALESCE(staged_branch, '-') FROM entries WHERE id = ?`, copyID); got != "provisional -" {
		t.Errorf("copy = %q, want a normal provisional entry", got)
	}
}

// fillCorpus gives the duplicate rule's idf something to work with; a corpus of one entry scores near zero and nothing is a duplicate.
func fillCorpus(t *testing.T, a *agent) {
	t.Helper()
	for i, topic := range []string{"invoice rounding", "login throttling", "cache eviction", "mail templating", "backup rotation"} {
		a.ok("write", map[string]any{"parent_sign_id": "root", "kind": "decision", "title": "Rule " + topic, "read_when": "you touch " + topic,
			"body": "We settled " + topic + " in review round " + string(rune('a'+i)) + ".", "decided_by": "ana", "decided_at": "2026-10-31"})
	}
}

func TestBaseWriterIsRefusedAsDuplicateOnlyOfWhatItSees(t *testing.T) {
	main, addr, _ := stagedServer(t)
	fillCorpus(t, agentAt(t, addr, main))
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	mustZebra(t, agentAt(t, addr, wx), "src/new.go")

	// The same text about an unchanged file is a normal write: a base writer cannot see the staged one.
	if out, err := writeZebra(agentAt(t, addr, main), "src/a.go"); err != nil || out["error"] != nil {
		t.Fatalf("base write = %v, err %v, want accepted", out, err)
	}
}

func TestBranchWriterIsRefusedAsDuplicateOfItsStagedEntry(t *testing.T) {
	main, addr, _ := stagedServer(t)
	fillCorpus(t, agentAt(t, addr, main))
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	onX := agentAt(t, addr, wx)
	mustZebra(t, onX, "src/new.go")

	out, err := writeZebra(onX, "src/new.go")
	if msg, _ := out["error"].(string); err != nil || !strings.HasPrefix(msg, "duplicate:") {
		t.Fatalf("branch write = %v, err %v, want duplicate", out, err)
	}
}

// fanOutArea builds a sign under a cap of 3 and returns it with a base agent and the branch worktree.
func fanOutArea(t *testing.T) (area string, base *agent, onX *agent, db *sql.DB) {
	t.Helper()
	main, addr, _, db := stagedServerDB(t)
	db.Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES ('fan_out_cap', '3')`)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	base, onX = agentAt(t, addr, main), agentAt(t, addr, wx)
	area = base.ok("write_sign", map[string]any{"parent_sign_id": "root", "title": "Crossings"})["sign_id"].(string)
	return area, base, onX, db
}

func addChild(t *testing.T, a *agent, parent, title string) {
	t.Helper()
	a.ok("write_sign", map[string]any{"parent_sign_id": parent, "title": title})
}

func TestBaseFanOutDoesNotCountAStagedEntryOfTheSameKind(t *testing.T) {
	area, base, onX, _ := fanOutArea(t)
	addChild(t, base, area, "Lights")
	staged := onX.ok("write", map[string]any{"parent_sign_id": area, "kind": "procedure", "title": "Retime the crossings", "read_when": "you retime the crossings",
		"body": "Open the scheduler and shorten each phase by one second.", "anchors": []map[string]any{{"type": "file", "path": "src/new.go"}}})
	if staged["staged"] != "feat/x" {
		t.Fatalf("write = %v, want staged", staged)
	}
	addChild(t, base, area, "Signs")
	addChild(t, base, area, "Poles")

	// Base sees three child signs, the cap. A procedure is a new edge for it, though a staged procedure exists.
	msg := base.refused("write", map[string]any{"parent_sign_id": area, "kind": "procedure", "title": "Repaint the stripes", "read_when": "you repaint the stripes",
		"body": "Mask the old stripes and roll on white paint.", "anchors": []map[string]any{{"type": "file", "path": "src/a.go"}}}, "split this fork")
	if !strings.Contains(msg, "3 edges") {
		t.Errorf("refusal %q should count the three edges base sees", msg)
	}
}

func TestBaseFirstSummaryAddsNoEdgeWhenOnlyAStagedSummaryExists(t *testing.T) {
	area, base, onX, _ := fanOutArea(t)
	addChild(t, base, area, "Lights")
	onX.ok("write", map[string]any{"parent_sign_id": area, "kind": "summary", "title": "Zebra scheduler", "read_when": zebraReadWhen,
		"body": "The zebra scheduler decides when crossings open, because pedestrians outrank cars here.", "anchors": []map[string]any{{"type": "file", "path": "src/new.go"}}})
	addChild(t, base, area, "Signs")
	addChild(t, base, area, "Poles")

	// A lone summary is no edge. Base sees none, so its first summary is the lone one and the sign at the cap takes it.
	res, err := base.raw("write", map[string]any{"parent_sign_id": area, "kind": "summary", "title": "Stripe paint", "read_when": "you repaint the stripes",
		"body": "Stripes are repainted yearly because traffic wears them thin.", "anchors": []map[string]any{{"type": "file", "path": "src/a.go"}}})
	if err != nil || res.IsError {
		t.Fatalf("base write refused (err %v): %+v", err, res.Content[0].(*sdk.TextContent).Text)
	}
}
