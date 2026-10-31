package web_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TheAmbitiousCorry/trails/internal/mcp"
	"github.com/TheAmbitiousCorry/trails/internal/project"
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

func worktree(t *testing.T, main, branch string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "wt")
	gitIn(t, main, "worktree", "add", "-q", "-b", branch, dir, "main")
	return dir
}

// writeAt writes an entry anchored to path (or updates entry id) from a session working in checkout.
func writeAt(t *testing.T, addr, checkout string, args map[string]any) map[string]any {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	call := func(tool string, a map[string]any) map[string]any {
		res, err := sess.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: a})
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %+v", tool, err, res)
		}
		b, _ := json.Marshal(res.StructuredContent)
		var m map[string]any
		json.Unmarshal(b, &m)
		return m
	}
	sid := call("session_start", map[string]any{"task": "staged page", "checkout": checkout})["session_id"].(string)
	args["session_id"] = sid
	return call("write", args)
}

func summary(title, path string) map[string]any {
	return map[string]any{"parent_sign_id": "root", "kind": "summary", "title": title, "read_when": "you work on " + strings.ToLower(title),
		"body":    "The " + strings.ToLower(title) + " owns its crossing rules, because pedestrians outrank cars here and always did.",
		"anchors": []map[string]any{{"type": "file", "path": path}}}
}

type stagedFixture struct {
	*fixture
	main, wx, wy, orig, onlyX, copyX, onlyY string
}

// stagedSeed builds a repo on main with three files, a normal entry about a.go, and two feature branches:
// feat/x has uncommitted edits to b.go and a.go (an entry about b.go and an update copy of the a.go entry),
// feat/y has c.go committed (an entry about c.go).
func stagedSeed(t *testing.T) *stagedFixture {
	t.Helper()
	main := t.TempDir()
	for _, f := range []string{"a", "b", "c"} {
		putFile(t, main, "src/"+f+".go", "package "+f+"\n")
	}
	putFile(t, main, ".trail/id", "stw1\n")
	gitIn(t, main, "init", "-q", "-b", "main")
	gitIn(t, main, "add", ".")
	gitIn(t, main, "commit", "-qm", "init")

	reg := project.NewRegistry(t.TempDir(), 0)
	t.Cleanup(func() { reg.Close() })
	if _, err := reg.Open(main); err != nil {
		t.Fatal(err)
	}
	p := reg.List()[0]
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mcp.Handler(reg, ln.Addr().String())}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	f := &stagedFixture{fixture: &fixture{t: t, st: p.Store, addr: ln.Addr().String()}, main: main}

	f.orig = writeAt(t, f.addr, main, summary("Crossing rules", "src/a.go"))["entry_id"].(string)
	f.wx = worktree(t, main, "feat/x")
	putFile(t, f.wx, "src/b.go", "package b\n\n// reworked on feat/x\n")
	putFile(t, f.wx, "src/a.go", "package a\n\n// reworked on feat/x\n")
	onlyX := writeAt(t, f.addr, f.wx, summary("Pedestrian priority", "src/b.go"))
	f.onlyX = onlyX["entry_id"].(string)
	up := summary("Crossing rules", "src/a.go")
	up["entry_id"] = f.orig
	up["body"] = "The crossing rules now let a pedestrian hold the light, because pedestrians outrank cars here."
	f.copyX = writeAt(t, f.addr, f.wx, up)["entry_id"].(string)

	f.wy = worktree(t, main, "feat/y")
	putFile(t, f.wy, "src/c.go", "package c\n\n// reworked on feat/y\n")
	gitIn(t, f.wy, "add", ".")
	gitIn(t, f.wy, "commit", "-qm", "c on y")
	f.onlyY = writeAt(t, f.addr, f.wy, summary("Signal timing", "src/c.go"))["entry_id"].(string)
	return f
}

func entryOf(t *testing.T, branch map[string]any, id string) map[string]any {
	t.Helper()
	for _, e := range branch["entries"].([]any) {
		if m := e.(map[string]any); m["id"] == id {
			return m
		}
	}
	t.Fatalf("branch %v does not list %s", branch["branch"], id)
	return nil
}

func branchOf(t *testing.T, page map[string]any, name string) map[string]any {
	t.Helper()
	for _, b := range page["branches"].([]any) {
		if m := b.(map[string]any); m["branch"] == name {
			return m
		}
	}
	t.Fatalf("no branch %s in %v", name, page["branches"])
	return nil
}

func TestStagedPageGroupsEntriesByBranch(t *testing.T) {
	f := stagedSeed(t)
	code, page := f.get("/api/staged")
	if code != 200 {
		t.Fatalf("staged: %d", code)
	}
	bs := page["branches"].([]any)
	if len(bs) != 2 || bs[0].(map[string]any)["branch"] != "feat/x" || bs[1].(map[string]any)["branch"] != "feat/y" {
		t.Fatalf("branches = %v, want feat/x then feat/y", bs)
	}
	x := branchOf(t, page, "feat/x")
	if x["base"] != "main" || x["alive"] != true || len(x["entries"].([]any)) != 2 {
		t.Errorf("feat/x = %v", x)
	}
	only := entryOf(t, x, f.onlyX)
	if only["state"] != "branch_only" || only["landing"] != "" || only["stages_over"] != nil || only["title"] != "Pedestrian priority" {
		t.Errorf("uncommitted branch entry = %v", only)
	}
	if only["last_check"] == "" {
		t.Errorf("last_check empty: %v", only)
	}
	cp := entryOf(t, x, f.copyX)
	over, _ := cp["stages_over"].(map[string]any)
	if over["id"] != f.orig || over["title"] != "Crossing rules" {
		t.Errorf("stage-over original = %v, want %s", cp["stages_over"], f.orig)
	}
	y := entryOf(t, branchOf(t, page, "feat/y"), f.onlyY)
	if y["state"] != "committed" || len(y["landing"].(string)) != 40 {
		t.Errorf("committed branch entry = %v, want its landing commit", y)
	}
}

func TestStagedPageEmptyWhenNothingIsHeld(t *testing.T) {
	f := seed(t)
	code, page := f.get("/api/staged")
	bs, ok := page["branches"].([]any)
	if code != 200 || !ok || len(bs) != 0 {
		t.Fatalf("staged on a project with none = %d %v, want an empty list", code, page)
	}
}

func TestEntryPageNamesItsBranchOnlyWhenStaged(t *testing.T) {
	f := stagedSeed(t)
	_, page := f.get("/api/entries/" + f.onlyX)
	st, _ := page["staged"].(map[string]any)
	if st["branch"] != "feat/x" || st["base"] != "main" {
		t.Errorf("staged entry page = %v", page["staged"])
	}
	_, page = f.get("/api/entries/" + f.copyX)
	cs, _ := page["staged"].(map[string]any)
	if over, _ := cs["stages_over"].(string); over != f.orig {
		t.Errorf("copy page staged = %v, want stages_over %s", page["staged"], f.orig)
	}
	_, page = f.get("/api/entries/" + f.orig)
	if page["staged"] != nil {
		t.Errorf("normal entry has staged = %v", page["staged"])
	}
}

func (f *fixture) col(q string, args ...any) string {
	f.t.Helper()
	var s *string
	if err := f.st.DB.QueryRow(q, args...).Scan(&s); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	if s == nil {
		return ""
	}
	return *s
}

func TestPromoteByHandHoldsNoLongerAndLogsTheUser(t *testing.T) {
	f := stagedSeed(t)
	code, body := f.post("/api/entries/"+f.onlyY+"/promote", map[string]any{})
	if code != 200 {
		t.Fatalf("promote: %d %s", code, body)
	}
	if got := f.col(`SELECT staged_branch FROM entries WHERE id = ?`, f.onlyY); got != "" {
		t.Errorf("staged_branch = %q after promotion", got)
	}
	if got := f.col(`SELECT result FROM events WHERE session_id = 'user' AND tool = 'promote' AND target = ?`, f.onlyY); got != "ok" {
		t.Errorf("user event = %q, want ok", got)
	}
	_, page := f.get("/api/staged")
	for _, b := range page["branches"].([]any) {
		if b.(map[string]any)["branch"] == "feat/y" {
			t.Errorf("feat/y still listed: %v", b)
		}
	}
	if _, e := f.get("/api/entries/" + f.onlyY); e["staged"] != nil {
		t.Errorf("promoted entry still staged: %v", e["staged"])
	}
}

func TestPromoteReplacesAnUnverifiedUnchangedOriginal(t *testing.T) {
	f := stagedSeed(t)
	if code, body := f.post("/api/entries/"+f.copyX+"/promote", map[string]any{}); code != 200 {
		t.Fatalf("promote: %d %s", code, body)
	}
	if got := f.col(`SELECT superseded_by FROM entries WHERE id = ?`, f.orig); got != f.copyX {
		t.Errorf("original superseded_by = %q, want %s", got, f.copyX)
	}
}

func TestPromoteOverAVerifiedOriginalEndsAsAConflictsPair(t *testing.T) {
	f := stagedSeed(t)
	if _, err := f.st.DB.Exec(`UPDATE entries SET tier = 'verified' WHERE id = ?`, f.orig); err != nil {
		t.Fatal(err)
	}
	if code, body := f.post("/api/entries/"+f.copyX+"/promote", map[string]any{}); code != 200 {
		t.Fatalf("promote: %d %s", code, body)
	}
	if got := f.col(`SELECT superseded_by FROM entries WHERE id = ?`, f.orig); got != "" {
		t.Errorf("a verified original was superseded by %q", got)
	}
	if got := f.col(`SELECT COUNT(*) FROM links WHERE relation = 'conflicts_with' AND ((from_id = ?1 AND to_id = ?2) OR (from_id = ?2 AND to_id = ?1))`, f.orig, f.copyX); got != "1" {
		t.Errorf("conflicts_with links = %s, want 1", got)
	}
	if got := f.col(`SELECT staged_branch FROM entries WHERE id = ?`, f.copyX); got != "" {
		t.Errorf("promoted copy still held by %q", got)
	}
}

func TestPromoteRefusesWhatIsNotStaged(t *testing.T) {
	f := stagedSeed(t)
	if code, _ := f.post("/api/entries/"+f.orig+"/promote", map[string]any{}); code != 400 {
		t.Errorf("promote of a normal entry = %d, want 400", code)
	}
	if code, _ := f.post("/api/entries/nope/promote", map[string]any{}); code != 404 {
		t.Errorf("promote of an unknown entry = %d, want 404", code)
	}
	if code, _ := f.post("/api/entries/"+f.onlyY+"/promote", map[string]any{}); code != 200 {
		t.Fatal("first promote failed")
	}
	if code, _ := f.post("/api/entries/"+f.onlyY+"/promote", map[string]any{}); code != 400 {
		t.Errorf("second promote = %d, want 400", code)
	}
	if got := f.col(`SELECT result FROM events WHERE session_id = 'user' AND tool = 'promote' AND target = ? ORDER BY id DESC LIMIT 1`, f.orig); got != "refused:not_staged" {
		t.Errorf("refusal event = %q", got)
	}
}

func TestAbandonedEntryIsListedAndPromotableByHand(t *testing.T) {
	f := stagedSeed(t)
	gitIn(t, f.main, "worktree", "remove", "--force", f.wy)
	gitIn(t, f.main, "branch", "-D", "feat/y")
	if code, body := f.post("/api/sweep", map[string]any{}); code != 200 {
		t.Fatalf("sweep: %d %s", code, body)
	}
	_, page := f.get("/api/staged")
	y := branchOf(t, page, "feat/y")
	if y["alive"] != false || entryOf(t, y, f.onlyY)["state"] != "abandoned" {
		t.Fatalf("feat/y = %v, want a dead branch with an abandoned entry", y)
	}
	if code, body := f.post("/api/entries/"+f.onlyY+"/promote", map[string]any{}); code != 200 {
		t.Fatalf("promote abandoned: %d %s", code, body)
	}
	if got := f.col(`SELECT tier FROM entries WHERE id = ?`, f.onlyY); got == "dormant" {
		t.Error("a hand-promoted entry stayed dormant")
	}
}
