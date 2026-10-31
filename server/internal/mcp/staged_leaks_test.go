package mcp_test

import (
	"encoding/json"
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
