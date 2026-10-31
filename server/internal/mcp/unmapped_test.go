package mcp_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

func commitAll(t *testing.T, repo, msg string) {
	t.Helper()
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-qm", msg)
}

// anchoredLive seeds a live entry with one anchor, as the coverage tests do.
func anchoredLive(t *testing.T, st *store.Store, id, typ, path string) {
	t.Helper()
	now := store.Now()
	if _, err := st.DB.Exec(`INSERT INTO entries (id, sign_id, kind, title, read_when, body_json, body_schema, tier, created_at, updated_at) VALUES (?, 'root', 'summary', ?, 'when', '{"type":"doc","content":[]}', 1, 'provisional', ?, ?)`, id, "Title "+id, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO anchors (entry_id, type, path_or_url, ok) VALUES (?, ?, ?, 1)`, id, typ, path); err != nil {
		t.Fatal(err)
	}
}

func unmappedFor(t *testing.T, repo string, seed func(*store.Store)) []map[string]any {
	t.Helper()
	reg, addr := serveRepos(t, repo)
	st := reg.List()[0].Store
	seed(st)
	call := connect(t, addr)
	res, err := call("session_start", map[string]any{"task": "map"})
	sid := structured(t, res, err)["session_id"].(string)
	res, err = call("coverage", map[string]any{"session_id": sid, "limit": 25})
	return list(t, structured(t, res, err), "unmapped")
}

// The plan's worked example: a/x.go covered, a/y.go not, b/p.go and b/q/r.go not, go.sum and README.md at the root.
func TestUnmappedWorkedExample(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	for _, f := range []string{"a/x.go", "a/y.go", "b/p.go", "b/q/r.go", "go.sum", "README.md"} {
		putFile(t, repo, f, "one\n")
	}
	commitAll(t, repo, "init")
	putFile(t, repo, "a/y.go", "two\n")
	putFile(t, repo, "b/p.go", "two\n")
	commitAll(t, repo, "second")
	putFile(t, repo, "b/p.go", "three\n")
	commitAll(t, repo, "third")

	items := unmappedFor(t, repo, func(st *store.Store) { anchoredLive(t, st, "ax", "file", "a/x.go") })
	type want struct {
		path            string
		loose           bool
		files, unmapped float64
		commits         float64
		names           string
	}
	wants := []want{{"b", false, 2, 2, 3, ""}, {"a", true, 1, 1, 2, "y.go"}, {".", true, 1, 1, 1, "README.md"}}
	if len(items) != len(wants) {
		t.Fatalf("unmapped = %v, want exactly %d items", items, len(wants))
	}
	for i, w := range wants {
		it := items[i]
		loose, _ := it["loose"].(bool)
		var names []string
		raw, _ := it["names"].([]any)
		for _, n := range raw {
			names = append(names, n.(string))
		}
		if it["path"] != w.path || loose != w.loose || it["files"] != w.files || it["unmapped"] != w.unmapped || it["commits_90d"] != w.commits || strings.Join(names, ",") != w.names {
			t.Errorf("item %d = %v, want %+v", i, it, w)
		}
		if it["sign"] != "root" {
			t.Errorf("item %d sign = %v", i, it["sign"])
		}
	}
}

func TestUnmappedDirCeilingAndExclusions(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	for i := 0; i < 51; i++ {
		putFile(t, repo, "big/f"+strconv.Itoa(i)+".go", "x\n")
	}
	for i := 0; i < 3; i++ {
		putFile(t, repo, "small/f"+strconv.Itoa(i)+".go", "x\n")
	}
	putFile(t, repo, "src/main.go", "x\n")
	putFile(t, repo, "go.sum", "x\n")
	putFile(t, repo, "web/package-lock.json", "{}\n")
	putFile(t, repo, "gen/out.go", "x\n")
	putFile(t, repo, "third/lib.js", "x\n")
	putFile(t, repo, "img/logo.png", "\x00\x01\x02\x00binary")
	putFile(t, repo, ".gitattributes", "gen/** linguist-generated\nthird/** linguist-vendored\n")
	commitAll(t, repo, "init")

	items := unmappedFor(t, repo, func(st *store.Store) {
		anchoredLive(t, st, "dbig", "dir", "big")
		anchoredLive(t, st, "dsmall", "dir", "small")
	})
	got := map[string]map[string]any{}
	for _, it := range items {
		got[it["path"].(string)] = it
	}
	if b := got["big"]; b == nil || b["unmapped"] != 51.0 || b["files"] != 51.0 {
		t.Errorf("a dir anchor over 50 files must not cover; big = %v", b)
	}
	if got["small"] != nil {
		t.Errorf("a dir anchor over 3 files covers them; got %v", got["small"])
	}
	for _, p := range []string{"web", "gen", "third", "img", "go.sum"} {
		if got[p] != nil {
			t.Errorf("excluded path %s listed: %v", p, got[p])
		}
	}
	for _, it := range items {
		if names, ok := it["names"].([]any); ok {
			for _, n := range names {
				if strings.Contains(n.(string), "lock") || n == "go.sum" || n == "logo.png" || n == "out.go" || n == "lib.js" {
					t.Errorf("excluded file named: %v", it)
				}
			}
		}
	}
	if src := got["src"]; src == nil || src["files"] != 1.0 {
		t.Errorf("src = %v", src)
	}
}

func TestLooseItemNamesAtMostFive(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	for i := 0; i < 7; i++ {
		putFile(t, repo, "d/f"+strconv.Itoa(i)+".go", "x\n")
	}
	putFile(t, repo, "d/sub/z.go", "x\n")
	commitAll(t, repo, "init")
	items := unmappedFor(t, repo, func(st *store.Store) { anchoredLive(t, st, "dz", "file", "d/sub/z.go") })
	if len(items) != 1 || items[0]["path"] != "d" || items[0]["loose"] != true || items[0]["unmapped"] != 7.0 || len(items[0]["names"].([]any)) != 5 {
		t.Fatalf("items = %v", items)
	}
}

func TestPreReadIgnoresADirAnchorOverTheCeiling(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	for i := 0; i < 51; i++ {
		putFile(t, repo, "big/f"+strconv.Itoa(i)+".go", "x\n")
	}
	putFile(t, repo, "small/a.go", "x\n")
	commitAll(t, repo, "init")
	reg, addr := serveRepos(t, repo)
	st := reg.List()[0].Store
	anchoredLive(t, st, "dbig", "dir", "big")
	anchoredLive(t, st, "dsmall", "dir", "small")
	newAgent(t, addr, "rt-ceiling")
	pre := func(p string) map[string]any {
		return postAPI(t, addr, "/api/pre-read", map[string]any{"runtime_session_id": "rt-ceiling", "path": p})
	}
	if got := pre("big/f1.go"); got["decision"] != "unmapped" {
		t.Errorf("a dir anchor over the ceiling still gates the read: %v", got)
	}
	if got := pre("small/a.go"); got["decision"] != "turned_back" || got["entry_id"] != "dsmall" {
		t.Errorf("a small dir anchor stopped gating: %v", got)
	}
}

// The git facts are cached per HEAD, but the entries are not: a new anchor or a new commit shows on the next call.
// The second commit also picks up .trail/id, which the server wrote after the first.
func TestUnmappedFollowsNewAnchorsAndCommits(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	putFile(t, repo, "pkg/a.go", "x\n")
	commitAll(t, repo, "init")
	reg, addr := serveRepos(t, repo)
	st := reg.List()[0].Store
	call := connect(t, addr)
	res, err := call("session_start", map[string]any{"task": "map"})
	sid := structured(t, res, err)["session_id"].(string)
	items := func() []map[string]any {
		res, err := call("coverage", map[string]any{"session_id": sid})
		return list(t, structured(t, res, err), "unmapped")
	}
	if got := items(); len(got) != 1 || got[0]["path"] != "." || got[0]["files"] != 1.0 {
		t.Fatalf("before = %v", got)
	}
	putFile(t, repo, "pkg/b.go", "x\n")
	commitAll(t, repo, "second")
	if got := items(); len(got) != 1 || got[0]["files"] != 3.0 || got[0]["commits_90d"] != 2.0 {
		t.Fatalf("after a commit = %v", got)
	}
	anchoredLive(t, st, "dpkg", "dir", "pkg")
	if got := items(); len(got) != 1 || got[0]["path"] != ".trail" {
		t.Fatalf("after anchoring pkg only .trail should remain: %v", got)
	}
}
