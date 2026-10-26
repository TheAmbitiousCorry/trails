package anchor_test

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/signs"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

func git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, repo, rel, content string) {
	t.Helper()
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	write(t, repo, "src/a.txt", "one\ntwo\nthree\n")
	write(t, repo, "src/b.txt", "bee\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "first")
	return repo
}

func open(t *testing.T, repo string) (*store.Store, anchor.Checker) {
	t.Helper()
	st, err := store.Open(t.TempDir(), repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, anchor.Checker{Repo: repo, Fetcher: anchor.NewFetch(fakeResolver{})}
}

func addEntry(t *testing.T, db *sql.DB, id, sign, kind, tier string) {
	t.Helper()
	now := store.Now()
	_, err := db.Exec(`INSERT INTO entries (id, sign_id, kind, title, read_when, body_json, body_schema, tier, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'when needed', '{"type":"doc","content":[]}', 1, ?, ?, ?)`, id, sign, kind, "Entry "+id, tier, now, now)
	if err != nil {
		t.Fatal(err)
	}
}

func anchorTo(t *testing.T, c anchor.Checker, db *sql.DB, a anchor.Anchor) {
	t.Helper()
	ctx := context.Background()
	a, err := c.Resolve(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := anchor.Insert(ctx, db, a); err != nil {
		t.Fatal(err)
	}
}

type fakeResolver map[string][]string

func (f fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	ips, ok := f[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	var out []net.IPAddr
	for _, s := range ips {
		out = append(out, net.IPAddr{IP: net.ParseIP(s)})
	}
	return out, nil
}

type stubFetcher struct {
	body string
	err  error
}

func (s *stubFetcher) GetBytes(context.Context, string) ([]byte, error) { return []byte(s.body), s.err }

func tier(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var s string
	if err := db.QueryRow(`SELECT tier FROM entries WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEditingAnchoredFileFlipsStaleAndBack(t *testing.T) {
	repo := newRepo(t)
	st, c := open(t, repo)
	ctx := context.Background()
	addEntry(t, st.DB, "e1", "root", "reference", "verified")
	anchorTo(t, c, st.DB, anchor.Anchor{EntryID: "e1", Type: "file", PathOrURL: "src/a.txt"})

	if _, err := anchor.RefreshEntry(ctx, st.DB, c, "e1"); err != nil || tier(t, st.DB, "e1") != "verified" {
		t.Fatalf("fresh anchor: tier %q err %v", tier(t, st.DB, "e1"), err)
	}
	write(t, repo, "src/a.txt", "one\nchanged\nthree\n")
	failed, err := anchor.RefreshEntry(ctx, st.DB, c, "e1")
	if err != nil || len(failed) != 1 || failed[0].PathOrURL != "src/a.txt" {
		t.Fatalf("failed = %+v err %v", failed, err)
	}
	if got := tier(t, st.DB, "e1"); got != "stale" {
		t.Errorf("tier after edit = %q", got)
	}
	if got, _ := anchor.Tier(ctx, st.DB, "e1"); got != "stale" {
		t.Errorf("Tier = %q", got)
	}
	// A second failing refresh must not overwrite the parked tier with "stale".
	anchor.RefreshEntry(ctx, st.DB, c, "e1")

	write(t, repo, "src/a.txt", "one\ntwo\nthree\n")
	if failed, err := anchor.RefreshEntry(ctx, st.DB, c, "e1"); err != nil || len(failed) != 0 {
		t.Fatalf("restored: failed %+v err %v", failed, err)
	}
	if got := tier(t, st.DB, "e1"); got != "verified" {
		t.Errorf("tier after restore = %q", got)
	}
	if got, _ := anchor.Tier(ctx, st.DB, "e1"); got != "verified" {
		t.Errorf("Tier after restore = %q", got)
	}
}

func TestEveryAnchorType(t *testing.T) {
	repo := newRepo(t)
	_, c := open(t, repo)
	ctx := context.Background()
	stub := &stubFetcher{body: "page v1"}
	c.Fetcher = stub
	head := git(t, repo, "rev-parse", "HEAD")

	cases := []struct {
		name   string
		a      anchor.Anchor
		mutate func()
	}{
		{"range", anchor.Anchor{Type: "range", PathOrURL: "src/a.txt", RangeStart: 2, RangeEnd: 3}, func() { write(t, repo, "src/a.txt", "one\ntwo\nTHREE\n") }},
		{"dir", anchor.Anchor{Type: "dir", PathOrURL: "src"}, func() { write(t, repo, "src/c.txt", "new\n") }},
		{"url", anchor.Anchor{Type: "url", PathOrURL: "https://example.com/doc"}, func() { stub.body = "page v2" }},
		{"produced_by", anchor.Anchor{Type: "produced_by", PathOrURL: "make render"}, func() {
			git(t, repo, "add", ".")
			git(t, repo, "commit", "-q", "-m", "second")
		}},
	}
	for _, tc := range cases {
		a, err := c.Resolve(ctx, tc.a)
		if err != nil {
			t.Fatalf("%s resolve: %v", tc.name, err)
		}
		if a.Hash == "" || a.FetchedAt == "" {
			t.Errorf("%s: hash %q fetched_at %q", tc.name, a.Hash, a.FetchedAt)
		}
		if ok, err := c.Check(ctx, a); !ok || err != nil {
			t.Errorf("%s fresh: ok %v err %v", tc.name, ok, err)
		}
		tc.mutate()
		if ok, err := c.Check(ctx, a); ok || err != nil {
			t.Errorf("%s after change: ok %v err %v", tc.name, ok, err)
		}
	}

	if ok, _ := c.Check(ctx, anchor.Anchor{Type: "commit", Hash: head}); !ok {
		t.Error("commit anchor to an ancestor failed")
	}
	if ok, _ := c.Check(ctx, anchor.Anchor{Type: "transcript", Extra: "claude:abc"}); !ok {
		t.Error("transcript must always hold")
	}
	if ok, _ := c.Check(ctx, anchor.Anchor{Type: "range", PathOrURL: "src/a.txt", RangeStart: 1, RangeEnd: 99}); ok {
		t.Error("range past the end of the file held")
	}
	if ok, _ := c.Check(ctx, anchor.Anchor{Type: "file", PathOrURL: "src/gone.txt", Hash: "x"}); ok {
		t.Error("missing file held")
	}
	if ok, _ := c.Check(ctx, anchor.Anchor{Type: "file", PathOrURL: "../outside", Hash: "x"}); ok {
		t.Error("path outside the repo held")
	}
	stub.err = errors.New("down")
	if ok, err := c.Check(ctx, anchor.Anchor{Type: "url", PathOrURL: "https://example.com/doc", Hash: "x"}); ok || err != nil {
		t.Errorf("failed fetch: ok %v err %v", ok, err)
	}
}

func TestCommitAnchorToNonAncestorFails(t *testing.T) {
	repo := newRepo(t)
	_, c := open(t, repo)
	ctx := context.Background()
	git(t, repo, "checkout", "-q", "-b", "side")
	write(t, repo, "side.txt", "side\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "side")
	side := git(t, repo, "rev-parse", "HEAD")
	git(t, repo, "checkout", "-q", "-")

	if ok, err := c.Check(ctx, anchor.Anchor{Type: "commit", Hash: side}); ok || err != nil {
		t.Errorf("non-ancestor: ok %v err %v", ok, err)
	}
	if ok, err := c.Check(ctx, anchor.Anchor{Type: "commit", Hash: strings.Repeat("0", 40)}); ok || err != nil {
		t.Errorf("unknown sha: ok %v err %v", ok, err)
	}
	if ok, err := c.Check(ctx, anchor.Anchor{Type: "commit", Hash: "--help"}); ok || err != nil {
		t.Errorf("option-shaped sha: ok %v err %v", ok, err)
	}
	if _, err := c.Resolve(ctx, anchor.Anchor{Type: "commit", Hash: side}); err == nil {
		t.Error("Resolve accepted a non-ancestor commit")
	}
}

func TestURLAnchorRefusesInternalAddresses(t *testing.T) {
	repo := newRepo(t)
	_, c := open(t, repo)
	c.Fetcher = anchor.NewFetch(fakeResolver{"intranet.example": {"10.1.2.3"}, "mixed.example": {"93.184.216.34", "192.168.0.9"}})
	ctx := context.Background()
	for _, u := range []string{"http://127.0.0.1/", "http://10.0.0.5/", "http://intranet.example/", "http://mixed.example/", "http://unknown.example/", "http://[::1]/", "http://169.254.169.254/"} {
		if _, err := c.Fetcher.GetBytes(ctx, u); !errors.Is(err, anchor.ErrBlockedHost) {
			t.Errorf("%s: err = %v", u, err)
		}
		if ok, err := c.Check(ctx, anchor.Anchor{Type: "url", PathOrURL: u, Hash: "x"}); ok || err != nil {
			t.Errorf("%s check: ok %v err %v", u, ok, err)
		}
		if _, err := c.Resolve(ctx, anchor.Anchor{Type: "url", PathOrURL: u}); err == nil {
			t.Errorf("%s: Resolve accepted it", u)
		}
	}
	if _, err := c.Fetcher.GetBytes(ctx, "ftp://example.com/"); !errors.Is(err, anchor.ErrBadScheme) {
		t.Errorf("ftp: %v", err)
	}
}

func TestSummaryCarriesChildStaleCountAndKeepsItsTier(t *testing.T) {
	repo := newRepo(t)
	st, c := open(t, repo)
	ctx := context.Background()
	if _, err := st.DB.Exec(`INSERT INTO signs (id, parent_id, title) VALUES ('mod', 'root', 'Module')`); err != nil {
		t.Fatal(err)
	}
	addEntry(t, st.DB, "mod-sum", "mod", "summary", "verified")
	addEntry(t, st.DB, "mod-ref", "mod", "reference", "provisional")
	anchorTo(t, c, st.DB, anchor.Anchor{EntryID: "mod-sum", Type: "file", PathOrURL: "src/a.txt"})
	anchorTo(t, c, st.DB, anchor.Anchor{EntryID: "mod-ref", Type: "file", PathOrURL: "src/b.txt"})

	write(t, repo, "src/b.txt", "changed\n")
	for _, id := range []string{"mod-sum", "mod-ref"} {
		if _, err := anchor.RefreshEntry(ctx, st.DB, c, id); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := anchor.StaleCount(ctx, st.DB, "mod"); err != nil || n != 1 {
		t.Fatalf("StaleCount = %d err %v", n, err)
	}
	if n, _ := anchor.StaleCount(ctx, st.DB, "root"); n != 1 {
		t.Errorf("StaleCount at root = %d", n)
	}
	v, err := signs.Look(ctx, st.DB, signs.LookRequest{SignID: "root"})
	if err != nil {
		t.Fatal(err)
	}
	var edge *signs.Edge
	for i := range v.Edges {
		if v.Edges[i].ID == "mod" {
			edge = &v.Edges[i]
		}
	}
	if edge == nil || edge.StaleCount != 1 || edge.Tier != "verified" {
		t.Errorf("edge = %+v", edge)
	}
	if got, _ := anchor.Tier(ctx, st.DB, "mod-sum"); got != "verified" {
		t.Errorf("summary tier = %q", got)
	}
}
