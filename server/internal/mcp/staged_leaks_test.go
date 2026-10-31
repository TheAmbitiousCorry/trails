package mcp_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBaseReadDoesNotNameAStagedEntryAmongItsLinks(t *testing.T) {
	main, addr, _, _ := stagedServerDB(t)
	base := agentAt(t, addr, main)
	n := mustZebra(t, base, "src/a.go")["entry_id"].(string)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	out := agentAt(t, addr, wx).ok("write", map[string]any{"parent_sign_id": "root", "kind": "summary", "title": "Secret staged sibling",
		"read_when": "you poke at the secret staged sibling thing", "body": "Secret staged sibling body is a wholly different matter about gadgets.",
		"anchors": []map[string]any{{"type": "file", "path": "src/new.go"}}, "see_also": []string{n}})
	if out["staged"] != "feat/x" {
		t.Fatalf("not staged: %v", out)
	}
	b, _ := json.Marshal(base.ok("read", map[string]any{"entry_id": n}))
	if s := string(b); strings.Contains(s, "Secret staged sibling") || strings.Contains(s, out["entry_id"].(string)) {
		t.Errorf("base read names the staged entry: %s", s)
	}
}

func TestBaseWriteCannotLinkToAStagedEntry(t *testing.T) {
	main, addr, _, _ := stagedServerDB(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	sid := mustZebra(t, agentAt(t, addr, wx), "src/new.go")["entry_id"].(string)
	agentAt(t, addr, main).refused("write", map[string]any{"parent_sign_id": "root", "kind": "summary", "title": "Other thing",
		"read_when": "you handle unrelated gadget flow", "body": "Gadget flow is handled elsewhere entirely, nothing to do with crossings.",
		"anchors": []map[string]any{{"type": "file", "path": "src/a.go"}}, "requires": []string{sid}}, "unknown_entry")
}

func TestAbandonedEntryIsDeletedThirtyDaysAfterAbandonmentNotBefore(t *testing.T) {
	for _, tc := range []struct {
		days int
		want string
	}{{10, "1"}, {31, "0"}} {
		main, wx, addr, pid, id, db := stagedOnX(t)
		dropBranch(t, main, wx, "feat/x")
		forceSweep(t, addr, pid)
		if g := tierAndBranch(t, db, id); g != "dormant/feat/x" {
			t.Fatal(g)
		}
		old := time.Now().UTC().Add(-time.Duration(tc.days) * 24 * time.Hour).Format(time.RFC3339Nano)
		db.Exec(`UPDATE events SET at = ? WHERE tool = 'sweep' AND target = ?`, old, id)
		db.Exec(`UPDATE entries SET created_at = ? WHERE id = ?`, "2020-01-01T00:00:00Z", id)
		forceSweep(t, addr, pid)
		if n := row(t, db, `SELECT COUNT(*) FROM entries WHERE id = ?`, id); n != tc.want {
			t.Errorf("%d days after abandonment: count %s, want %s", tc.days, n, tc.want)
		}
	}
}

func TestReadAndSearchNameTheBranchOfAStagedEntry(t *testing.T) {
	main, addr, _, _ := stagedServerDB(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	onX := agentAt(t, addr, wx)
	id := mustZebra(t, onX, "src/new.go")["entry_id"].(string)
	if got := agentAt(t, addr, main).ok("read", map[string]any{"entry_id": id})["staged"]; got != "feat/x" {
		t.Errorf("base read by id: staged = %v, want feat/x", got)
	}
	hits := onX.ok("search", map[string]any{"query": "zebra crossing scheduler"})["hits"].([]any)
	if len(hits) == 0 || hits[0].(map[string]any)["staged"] != "feat/x" {
		t.Errorf("branch search hit = %v, want staged feat/x", hits)
	}
	normal := mustZebra(t, agentAt(t, addr, main), "src/a.go")["entry_id"].(string)
	if got, ok := agentAt(t, addr, main).ok("read", map[string]any{"entry_id": normal})["staged"]; ok {
		t.Errorf("normal entry read carries staged = %v", got)
	}
}

func TestCoverageListsAStaleStagedEntryOnlyToCheckoutsThatSeeIt(t *testing.T) {
	main, addr, _, db := stagedServerDB(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	onX := agentAt(t, addr, wx)
	id := mustZebra(t, onX, "src/new.go")["entry_id"].(string)
	if _, err := db.Exec(`UPDATE anchors SET ok = 0 WHERE entry_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	listed := func(a *agent) bool {
		b, _ := json.Marshal(a.ok("coverage", map[string]any{})["stale"])
		return strings.Contains(string(b), id)
	}
	if !listed(onX) {
		t.Errorf("branch coverage does not list its stale staged entry")
	}
	if listed(agentAt(t, addr, main)) {
		t.Errorf("base coverage lists a staged entry it cannot see")
	}
}

func TestGapCannotBeClosedOntoAStagedEntryTheCallerCannotSee(t *testing.T) {
	main, addr, _, _ := stagedServerDB(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	staged := mustZebra(t, agentAt(t, addr, wx), "src/new.go")["entry_id"].(string)
	opener, base := agentAt(t, addr, main), agentAt(t, addr, main)
	gap := openGap(t, opener, "zebra crossing scheduler")
	base.refused("resolve_gap", map[string]any{"gap_id": gap, "action": "merge", "entry_id": staged}, "unknown_entry")
}

func TestSessionEndEditGateIgnoresADirAnchorOverTheCeiling(t *testing.T) {
	main, addr, _, db := stagedServerDB(t)
	for i := 0; i < 51; i++ {
		putFile(t, main, fmt.Sprintf("big/f%02d.go", i), "package big\n")
	}
	gitIn(t, main, "add", ".")
	gitIn(t, main, "commit", "-qm", "big")
	author := agentAt(t, addr, main)
	res, err := author.raw("write", map[string]any{"parent_sign_id": "root", "kind": "summary", "title": "Big package", "read_when": "you change the big package",
		"body": "The big package holds generated shims, because the vendor API changes weekly.", "anchors": []map[string]any{{"type": "dir", "path": "big"}}})
	if err != nil || res.IsError {
		b, _ := json.Marshal(res)
		t.Fatalf("write: %v %s", err, b)
	}
	editor := agentAt(t, addr, main)
	if _, err := db.Exec(`INSERT INTO events (session_id, at, tool, target, result) VALUES (?, ?, 'edit', 'big/f01.go', 'ok')`, editor.sid, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if end, err := editor.raw("session_end", map[string]any{}); err != nil || end.IsError {
		b, _ := json.Marshal(end.Content)
		t.Errorf("session_end gated on a dir anchor over the ceiling: %v %s", err, b)
	}
}

func TestAStagedAnchorCoversOnlyForCheckoutsThatSeeIt(t *testing.T) {
	main, addr, _, _ := stagedServerDB(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/a.go", "package a\n\n// changed on the branch\n")
	onX := agentAt(t, addr, wx)
	if out := mustZebra(t, onX, "src/a.go"); out["staged"] != "feat/x" {
		t.Fatalf("write = %v, want staged", out)
	}
	unmapped := func(a *agent) string {
		b, _ := json.Marshal(a.ok("coverage", map[string]any{})["unmapped"])
		return string(b)
	}
	if got := unmapped(onX); strings.Contains(got, `"src"`) {
		t.Errorf("branch coverage lists src as unmapped though its staged entry anchors src/a.go: %s", got)
	}
	// With nothing else anchored, base sees the whole tree unmapped, src/a.go included, as one item at the root.
	if got := unmapped(agentAt(t, addr, main)); !strings.Contains(got, `"path":"."`) || !strings.Contains(got, `"unmapped":2`) {
		t.Errorf("base coverage treats src/a.go as covered by a staged entry it cannot see: %s", got)
	}
}
