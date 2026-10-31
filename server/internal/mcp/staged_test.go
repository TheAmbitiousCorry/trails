package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func putFile(t *testing.T, dir, rel, text string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stagedServer serves a repo whose base branch is main and which holds src/a.go, with .trail/id committed so worktrees carry it.
func stagedServer(t *testing.T) (main, addr, projectID string) {
	t.Helper()
	main = t.TempDir()
	putFile(t, main, "src/a.go", "package a\n")
	putFile(t, main, ".trail/id", "stg1\n")
	gitIn(t, main, "init", "-q", "-b", "main")
	gitIn(t, main, "add", ".")
	gitIn(t, main, "commit", "-qm", "init")
	reg, addr := serveRepos(t, main)
	return main, addr, reg.List()[0].ID
}

func worktree(t *testing.T, main, branch, start string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "wt")
	gitIn(t, main, "worktree", "add", "-q", "-b", branch, dir, start)
	return dir
}

// agentAt starts a session that works in the given checkout.
func agentAt(t *testing.T, addr, checkout string) *agent {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	a := &agent{t: t, sess: sess}
	a.sid = a.ok("session_start", map[string]any{"task": "staged walk", "checkout": checkout})["session_id"].(string)
	return a
}

func forceSweep(t *testing.T, addr, id string) map[string]any {
	t.Helper()
	resp, err := http.Post("http://"+addr+"/api/p/"+id+"/sweep", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("POST sweep: status %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

const zebraReadWhen = "you tune the zebra crossing scheduler"

func writeZebra(a *agent, path string, extra ...map[string]any) (map[string]any, error) {
	anchors := []map[string]any{{"type": "file", "path": path}}
	for _, e := range extra {
		anchors = append(anchors, e)
	}
	res, err := a.raw("write", map[string]any{"parent_sign_id": "root", "kind": "summary", "title": "Zebra scheduler", "read_when": zebraReadWhen,
		"body": "The zebra scheduler decides when crossings open, because pedestrians outrank cars here.", "anchors": anchors})
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out map[string]any
	json.Unmarshal(b, &out)
	if res.IsError {
		out = map[string]any{"error": res.Content[0].(*sdk.TextContent).Text}
	}
	return out, nil
}

func mustZebra(t *testing.T, a *agent, path string) map[string]any {
	t.Helper()
	out, err := writeZebra(a, path)
	if err != nil || out["error"] != nil {
		t.Fatalf("write = %v, err %v", out, err)
	}
	return out
}

func sees(a *agent, id string) (searchSees, lookSees bool) {
	a.t.Helper()
	hits, _ := a.ok("search", map[string]any{"query": zebraReadWhen})["hits"].([]any)
	for _, h := range hits {
		if h.(map[string]any)["id"] == id {
			searchSees = true
		}
	}
	edges, _ := a.ok("look", map[string]any{"kind": "summary", "within": "root"})["edges"].([]any)
	for _, e := range edges {
		if e.(map[string]any)["id"] == id {
			lookSees = true
		}
	}
	return searchSees, lookSees
}

func wantSees(t *testing.T, who string, a *agent, id string, want bool) {
	t.Helper()
	s, l := sees(a, id)
	if s != want || l != want {
		t.Errorf("%s: search sees = %v, look sees = %v, want both %v", who, s, l, want)
	}
}

func TestStagedWriteIsHiddenFromBaseAndShownOnItsBranch(t *testing.T) {
	main, addr, _ := stagedServer(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")

	onX := agentAt(t, addr, wx)
	out := mustZebra(t, onX, "src/new.go")
	if out["staged"] != "feat/x" {
		t.Fatalf("write reply = %v, want staged: feat/x", out)
	}
	id := out["entry_id"].(string)

	wantSees(t, "session on feat/x", onX, id, true)
	wantSees(t, "base session", agentAt(t, addr, main), id, false)
}

func TestEntryWrittenOverUncommittedWorkOnABranchWithNoCommitsStaysHiddenFromEveryOtherCheckout(t *testing.T) {
	main, addr, _ := stagedServer(t)
	wx := worktree(t, main, "feat/x", "main")
	cut := worktree(t, main, "feat/other", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")

	id := mustZebra(t, agentAt(t, addr, wx), "src/new.go")["entry_id"].(string)

	wantSees(t, "base session", agentAt(t, addr, main), id, false)
	wantSees(t, "a branch cut from the same base commit", agentAt(t, addr, cut), id, false)
}

func TestFreshBranchWithNoCommitsDoesNotPromote(t *testing.T) {
	main, addr, pid := stagedServer(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	id := mustZebra(t, agentAt(t, addr, wx), "src/new.go")["entry_id"].(string)

	// feat/x tip is the base tip, so it is an ancestor of main; only a pinned landing commit may promote.
	rep := forceSweep(t, addr, pid)
	if rep["promoted"] != float64(0) {
		t.Errorf("sweep = %v, want promoted 0", rep)
	}
	wantSees(t, "base session after the sweep", agentAt(t, addr, main), id, false)
	wantSees(t, "session on feat/x after the sweep", agentAt(t, addr, wx), id, true)
}

func TestBranchWriteAboutAnUnchangedFileIsNormal(t *testing.T) {
	main, addr, _ := stagedServer(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")

	out := mustZebra(t, agentAt(t, addr, wx), "src/a.go")
	if _, staged := out["staged"]; staged {
		t.Fatalf("write about an unchanged file was staged: %v", out)
	}
	wantSees(t, "base session", agentAt(t, addr, main), out["entry_id"].(string), true)
}

func TestDirAnchorOnAStagedWriteIsRefused(t *testing.T) {
	main, addr, _ := stagedServer(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")

	out, err := writeZebra(agentAt(t, addr, wx), "src/new.go", map[string]any{"type": "dir", "path": "src"})
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := out["error"].(string)
	if !strings.HasPrefix(msg, "anchor:") || !strings.Contains(msg, "anchor the files") {
		t.Fatalf("write = %v, want an anchor refusal telling the writer to anchor the files", out)
	}
}

func TestDetachedCheckoutWithBranchWorkIsRefused(t *testing.T) {
	main, addr, _ := stagedServer(t)
	wx := worktree(t, main, "feat/x", "main")
	gitIn(t, wx, "checkout", "-q", "--detach")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")

	out, err := writeZebra(agentAt(t, addr, wx), "src/new.go")
	if err != nil {
		t.Fatal(err)
	}
	if msg, _ := out["error"].(string); !strings.HasPrefix(msg, "detached_checkout:") {
		t.Fatalf("write = %v, want detached_checkout", out)
	}
	// With no changed anchor the same checkout writes a normal entry.
	if out, _ := writeZebra(agentAt(t, addr, wx), "src/a.go"); out["error"] != nil || out["staged"] != nil {
		t.Fatalf("write about an unchanged file from a detached HEAD = %v, want normal", out)
	}
}

func TestLandingCommitShowsTheEntryToBranchesCutAfterIt(t *testing.T) {
	main, addr, pid := stagedServer(t)
	wx := worktree(t, main, "feat/x", "main")
	before := worktree(t, main, "feat/before", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	id := mustZebra(t, agentAt(t, addr, wx), "src/new.go")["entry_id"].(string)

	gitIn(t, wx, "add", ".")
	gitIn(t, wx, "commit", "-qm", "add new")
	if rep := forceSweep(t, addr, pid); rep["promoted"] != float64(0) {
		t.Fatalf("sweep promoted before the branch merged: %v", rep)
	}
	after := worktree(t, main, "feat/after", "feat/x")

	wantSees(t, "branch cut after the landing commit", agentAt(t, addr, after), id, true)
	wantSees(t, "branch cut before the landing commit", agentAt(t, addr, before), id, false)
	wantSees(t, "base session", agentAt(t, addr, main), id, false)
}

func TestMergedBranchPromotesOnSweep(t *testing.T) {
	main, addr, pid := stagedServer(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	id := mustZebra(t, agentAt(t, addr, wx), "src/new.go")["entry_id"].(string)
	gitIn(t, wx, "add", ".")
	gitIn(t, wx, "commit", "-qm", "add new")
	forceSweep(t, addr, pid)

	base := agentAt(t, addr, main)
	wantSees(t, "base session before the merge", base, id, false)
	gitIn(t, main, "merge", "-q", "--no-ff", "-m", "merge feat/x", "feat/x")
	if rep := forceSweep(t, addr, pid); rep["promoted"] != float64(1) {
		t.Fatalf("sweep after the merge = %v, want promoted 1", rep)
	}
	wantSees(t, "base session after the merge", base, id, true)
	if got := base.ok("read", map[string]any{"entry_id": id}); got["tier"] != "provisional" {
		t.Errorf("promoted entry tier = %v, want provisional", got["tier"])
	}
}

func TestUpdateOfAStagedEntryFromItsBranchStaysStaged(t *testing.T) {
	main, addr, _ := stagedServer(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	onX := agentAt(t, addr, wx)
	id := mustZebra(t, onX, "src/new.go")["entry_id"].(string)

	out := onX.ok("write", map[string]any{"entry_id": id, "body": "The zebra scheduler decides when crossings open, because pedestrians outrank cars in this town."})
	if out["entry_id"] != id {
		t.Fatalf("update = %v", out)
	}
	wantSees(t, "base session", agentAt(t, addr, main), id, false)
	wantSees(t, "session on feat/x", onX, id, true)
}

func TestStagedSummaryIsNotShownAsASignSummaryToBase(t *testing.T) {
	main, addr, _ := stagedServer(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	base := agentAt(t, addr, main)
	sign := base.ok("write_sign", map[string]any{"parent_sign_id": "root", "title": "Crossings"})["sign_id"].(string)

	onX := agentAt(t, addr, wx)
	res := onX.ok("write", map[string]any{"parent_sign_id": sign, "kind": "summary", "title": "Zebra scheduler", "read_when": zebraReadWhen,
		"body":    "The zebra scheduler decides when crossings open, because pedestrians outrank cars here.",
		"anchors": []map[string]any{{"type": "file", "path": "src/new.go"}}})
	if res["staged"] != "feat/x" {
		t.Fatalf("write = %v, want staged", res)
	}
	dec := base.ok("write", map[string]any{"parent_sign_id": sign, "kind": "decision", "title": "Crossing priority",
		"read_when": "you decide who crosses first", "body": "Pedestrians cross first.", "decided_by": "ana", "decided_at": "2026-10-31"})
	read := base.ok("read", map[string]any{"entry_id": dec["entry_id"]})
	b, _ := json.Marshal(read["path"])
	if strings.Contains(string(b), "zebra") {
		t.Errorf("base read path carries the staged summary: %s", b)
	}
	look := base.ok("look", map[string]any{"sign_id": sign})
	if lb, _ := json.Marshal(look); strings.Contains(string(lb), "zebra") {
		t.Errorf("base look carries the staged summary: %s", lb)
	}
}

func TestVisibilityFollowsTheLandingCommitNotTheWriteHead(t *testing.T) {
	main, addr, pid := stagedServer(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/other.go", "package a\n")
	gitIn(t, wx, "add", ".")
	gitIn(t, wx, "commit", "-qm", "c1 unrelated")
	atC1 := worktree(t, main, "feat/atc1", "feat/x") // cut at c1, before the work
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	id := mustZebra(t, agentAt(t, addr, wx), "src/new.go")["entry_id"].(string)
	forceSweep(t, addr, pid)
	wantSees(t, "cut at c1 (write-time HEAD) before commit", agentAt(t, addr, atC1), id, false)
	gitIn(t, wx, "add", ".")
	gitIn(t, wx, "commit", "-qm", "c2 new")
	putFile(t, wx, "src/later.go", "package a\n")
	gitIn(t, wx, "add", ".")
	gitIn(t, wx, "commit", "-qm", "c3 later")
	forceSweep(t, addr, pid)
	wantSees(t, "cut at c1 after landing", agentAt(t, addr, atC1), id, false)
	atC3 := worktree(t, main, "feat/atc3", "feat/x")
	wantSees(t, "cut at c3", agentAt(t, addr, atC3), id, true)
	gitIn(t, wx, "branch", "-f", "mid", "HEAD~1")
	mid := worktree(t, main, "feat/mid", "mid")
	wantSees(t, "cut at c2 (L)", agentAt(t, addr, mid), id, true)
}

func TestBranchSessionLeavesANormalEntrysAnchorsAsStored(t *testing.T) {
	main := t.TempDir()
	putFile(t, main, "src/a.go", "package a\n")
	putFile(t, main, ".trail/id", "stg1\n")
	gitIn(t, main, "init", "-q", "-b", "main")
	gitIn(t, main, "add", ".")
	gitIn(t, main, "commit", "-qm", "init")
	reg, addr := serveRepos(t, main)
	db := reg.List()[0].DB()
	author := agentAt(t, addr, main)
	out := mustZebra(t, author, "src/a.go")
	if out["staged"] != nil {
		t.Fatal("staged")
	}
	id := out["entry_id"].(string)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/a.go", "package a\n\n// changed on branch\n")
	b := agentAt(t, addr, wx)
	c := agentAt(t, addr, wx)
	snap := func() string {
		var ok int
		var h, tier string
		if err := db.QueryRow(`SELECT a.ok, COALESCE(a.hash,''), e.tier FROM anchors a JOIN entries e ON e.id=a.entry_id WHERE e.id=?`, id).Scan(&ok, &h, &tier); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%d %s %s", ok, h, tier)
	}
	before := snap()
	b.ok("read", map[string]any{"entry_id": id, "depth": "full"})
	c.ok("read", map[string]any{"entry_id": id, "depth": "full"})
	b.ok("confirm", map[string]any{"entry_id": id})
	after := snap()
	if strings.Fields(before)[0] != strings.Fields(after)[0] || strings.Fields(before)[1] != strings.Fields(after)[1] {
		t.Fatalf("anchors changed: %s -> %s", before, after)
	}
}
