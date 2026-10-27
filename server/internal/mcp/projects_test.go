package mcp_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TheAmbitiousCorry/trails/internal/project"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

type caller func(name string, args map[string]any) (*sdk.CallToolResult, error)

func connect(t *testing.T, addr string) caller {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	return func(name string, args map[string]any) (*sdk.CallToolResult, error) {
		return sess.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	}
}

func refusal(t *testing.T, res *sdk.CallToolResult, err error) string {
	t.Helper()
	if err != nil || !res.IsError {
		t.Fatalf("not refused: %v %+v", err, res)
	}
	return res.Content[0].(*sdk.TextContent).Text
}

func eventCount(t *testing.T, p *project.Project) int {
	t.Helper()
	var n int
	if err := p.DB().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOneServerKeepsProjectsApart(t *testing.T) {
	home := t.TempDir()
	repoA, repoB := t.TempDir(), t.TempDir()
	reg, addr := serveHome(t, home, repoA, repoB)
	idA, _ := store.ReadID(repoA)
	idB, _ := store.ReadID(repoB)
	pa, _ := reg.ByID(idA)
	pb, _ := reg.ByID(idB)
	call := connect(t, addr)

	// With two projects open, session_start must say which.
	res, err := call("session_start", map[string]any{"task": "no project"})
	if txt := refusal(t, res, err); !strings.HasPrefix(txt, "project_unknown:") || !strings.Contains(txt, idA) || !strings.Contains(txt, idB) {
		t.Errorf("missing project = %q", txt)
	}
	res, err = call("session_start", map[string]any{"task": "wrong project", "project": "nope"})
	if txt := refusal(t, res, err); !strings.HasPrefix(txt, "project_unknown:") {
		t.Errorf("unknown project = %q", txt)
	}

	start := func(id string) string {
		res, err := call("session_start", map[string]any{"task": "work in " + id, "project": id})
		return structured(t, res, err)["session_id"].(string)
	}
	sidA, sidB := start(idA), start(idB)
	write := func(sid, title string) {
		res, err := call("write", map[string]any{"session_id": sid, "parent_sign_id": "root", "kind": "decision", "title": title,
			"read_when": "you ask about " + title, "body": "Body of " + title + ".", "decided_by": "ana", "decided_at": "2026-10-27"})
		structured(t, res, err)
	}
	write(sidA, "Alpha rule")
	write(sidB, "Bravo rule")

	// Each session reads its own entry and sees only its own entries.
	res, err = call("read", map[string]any{"session_id": sidA, "entry_id": "alpha-rule"})
	structured(t, res, err)
	res, err = call("read", map[string]any{"session_id": sidA, "entry_id": "bravo-rule"})
	if txt := refusal(t, res, err); !strings.HasPrefix(txt, "unknown_entry:") {
		t.Errorf("other project's entry = %q", txt)
	}
	res, err = call("look", map[string]any{"session_id": sidB, "sign_id": "root"})
	look := structured(t, res, err)
	edges, _ := look["edges"].([]any)
	var titles []string
	for _, e := range edges {
		titles = append(titles, e.(map[string]any)["title"].(string))
	}
	if got := strings.Join(titles, ","); !strings.Contains(got, "Bravo rule") || strings.Contains(got, "Alpha") {
		t.Errorf("project B look = %v", titles)
	}
	for _, p := range []*project.Project{pa, pb} {
		var n int
		p.DB().QueryRow(`SELECT COUNT(*) FROM entries WHERE id IN ('alpha-rule','bravo-rule')`).Scan(&n)
		if n != 1 {
			t.Errorf("project %s holds %d of the two entries, want 1", p.ID, n)
		}
	}

	// A session id no project holds is refused and logs nothing anywhere.
	before := eventCount(t, pa) + eventCount(t, pb)
	res, err = call("look", map[string]any{"session_id": "s-nothing", "sign_id": "root"})
	if txt := refusal(t, res, err); !strings.HasPrefix(txt, "unknown_session:") {
		t.Errorf("unknown session = %q", txt)
	}
	if after := eventCount(t, pa) + eventCount(t, pb); after != before {
		t.Errorf("unknown_session logged %d events", after-before)
	}

	// The hook routes no longer need a sole project: an unmatched runtime id is a quiet pass.
	if got := postAPI(t, addr, "/api/stop-check", map[string]any{"runtime_session_id": "x"}); got["open"] != false {
		t.Errorf("stop-check with two projects and no match = %v", got)
	}
	resp, err := http.Get("http://" + addr + "/api/map")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("map with two projects: status %d", resp.StatusCode)
	}

	// A restart over the same home finds both projects and both open sessions.
	reg.Close()
	reg2, addr2 := serveHome(t, home)
	if got := len(reg2.List()); got != 2 {
		t.Fatalf("restart opened %d projects, want 2", got)
	}
	call2 := connect(t, addr2)
	for sid, entry := range map[string]string{sidA: "alpha-rule", sidB: "bravo-rule"} {
		res, err := call2("read", map[string]any{"session_id": sid, "entry_id": entry})
		structured(t, res, err)
	}
}

func TestOpenNeedsAnID(t *testing.T) {
	reg := project.NewRegistry(t.TempDir(), 0)
	defer reg.Close()
	repo := t.TempDir()
	if _, err := reg.Open(repo); err == nil {
		t.Fatal("a repo without .trail/id was opened")
	}
	if _, err := os.Stat(filepath.Join(repo, ".trail")); err == nil {
		t.Error("Open wrote into the repo")
	}
}
