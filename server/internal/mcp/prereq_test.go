package mcp_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/entries"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

type chainWalk struct {
	t                   *testing.T
	st                  *store.Store
	sess                *sdk.ClientSession
	sid                 string
	green, blue, yellow string
	loner               string
}

// newChainWalk seeds green <- blue <- yellow (blue requires green, yellow requires blue) and a loner without requires, then opens a session.
func newChainWalk(t *testing.T) *chainWalk {
	t.Helper()
	st, addr := startServer(t)
	ctx := context.Background()
	put := func(title string, requires ...string) string {
		res, err := entries.Write(ctx, st.DB, anchor.Checker{Repo: st.Repo}, entries.WriteRequest{Actor: "s-seed", ParentSignID: "root", Kind: "decision",
			Title: title, ReadWhen: "when " + title, BodyMarkdown: "Body of " + title + ".\n\nSecond paragraph of " + title + ".",
			DecidedBy: "ana", DecidedAt: "2026-10-28", Requires: requires})
		if err != nil || res.Refusal != nil {
			t.Fatalf("seed %s: %v %v", title, err, res.Refusal)
		}
		return res.EntryID
	}
	w := &chainWalk{t: t, st: st}
	w.green = put("Green")
	w.blue = put("Blue", w.green)
	w.yellow = put("Yellow", w.blue)
	w.loner = put("Loner")
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	w.sess = sess
	w.sid = w.call("session_start", map[string]any{"task": "walk"}).StructuredContent.(map[string]any)["session_id"].(string)
	return w
}

func (w *chainWalk) call(name string, args map[string]any) *sdk.CallToolResult {
	w.t.Helper()
	if name != "session_start" {
		args["session_id"] = w.sid
	}
	res, err := w.sess.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		w.t.Fatal(err)
	}
	return res
}

func (w *chainWalk) ok(name string, args map[string]any) map[string]any {
	w.t.Helper()
	return structured(w.t, w.call(name, args), nil)
}

func (w *chainWalk) read(id, depth string) *sdk.CallToolResult {
	return w.call("read", map[string]any{"entry_id": id, "depth": depth})
}

func TestSummaryReadRefusedUntilChainReadAtFull(t *testing.T) {
	w := newChainWalk(t)

	res := w.read(w.yellow, "summary")
	if !res.IsError {
		t.Fatal("summary read of an entry with unread prerequisites was served")
	}
	want := "prerequisites_unread: read at depth full first, earliest first: green (Green), blue (Blue); or read this entry at depth full to receive the chain"
	if got := res.Content[0].(*sdk.TextContent).Text; got != want {
		t.Errorf("refusal = %q\nwant      %q", got, want)
	}
	var result string
	w.st.DB.QueryRow(`SELECT result FROM events WHERE session_id = ? AND tool = 'read' AND target = ? ORDER BY id DESC LIMIT 1`, w.sid, w.yellow).Scan(&result)
	if result != "refused:prerequisites_unread" {
		t.Errorf("refused read logged as %q", result)
	}
	// A summary read of a prerequisite does not satisfy the chain.
	w.ok("read", map[string]any{"entry_id": w.green, "depth": "summary"})
	if res := w.read(w.yellow, "summary"); !res.IsError {
		t.Error("a summary read of green satisfied the chain")
	}

	// Reading green at full leaves blue as the only unread prerequisite.
	w.ok("read", map[string]any{"entry_id": w.green, "depth": "full"})
	res = w.read(w.yellow, "summary")
	if !res.IsError || !strings.Contains(res.Content[0].(*sdk.TextContent).Text, "earliest first: blue (Blue); or") {
		t.Errorf("after green, refusal = %+v", res.Content)
	}

	w.ok("read", map[string]any{"entry_id": w.blue, "depth": "full"})
	got := w.ok("read", map[string]any{"entry_id": w.yellow, "depth": "summary"})
	if r := ids(t, got["already_read"]); len(r) != 2 || r[0] != w.green || r[1] != w.blue {
		t.Errorf("already_read = %v", got["already_read"])
	}
	if got["prerequisite_note"] == nil || got["prerequisites"] != nil {
		t.Errorf("served summary: note %v prerequisites %v", got["prerequisite_note"], got["prerequisites"])
	}
	if body := got["untrusted"].(map[string]any)["body_markdown"].(string); strings.Contains(body, "Second paragraph") {
		t.Errorf("summary carried the full body: %q", body)
	}
}

func TestSummaryReadWithoutRequiresIsServedAsBefore(t *testing.T) {
	w := newChainWalk(t)
	got := w.ok("read", map[string]any{"entry_id": w.loner, "depth": "summary"})
	if got["already_read"] != nil || got["prerequisite_note"] != nil || got["id"] != w.loner {
		t.Errorf("loner summary = %v", got)
	}
}

func TestFullReadStillServesTheChain(t *testing.T) {
	w := newChainWalk(t)
	got := w.ok("read", map[string]any{"entry_id": w.yellow, "depth": "full"})
	if r := ids(t, got["prerequisites"]); len(r) != 2 || r[0] != w.green || r[1] != w.blue {
		t.Errorf("full read prerequisites = %v", r)
	}
}

func TestSearchHitListsPrerequisiteChainWithReadState(t *testing.T) {
	w := newChainWalk(t)
	requires := func() []any {
		t.Helper()
		out := w.ok("search", map[string]any{"query": "Yellow"})
		for _, h := range out["hits"].([]any) {
			if h.(map[string]any)["id"] == w.yellow {
				r, _ := h.(map[string]any)["requires"].([]any)
				return r
			}
		}
		t.Fatalf("yellow not among hits: %v", out["hits"])
		return nil
	}
	check := func(wantRead ...bool) {
		t.Helper()
		got := requires()
		if len(got) != 2 {
			t.Fatalf("requires = %v", got)
		}
		for i, id := range []string{w.green, w.blue} {
			m := got[i].(map[string]any)
			if m["id"] != id || m["read"] != wantRead[i] || m["title"] == "" || m["tier"] == "" || m["read_when"] == "" {
				t.Errorf("requires[%d] = %v, want %s read=%v", i, m, id, wantRead[i])
			}
		}
	}
	check(false, false)
	w.ok("read", map[string]any{"entry_id": w.green, "depth": "full"})
	check(true, false)

	out := w.ok("search", map[string]any{"query": "Loner"})
	for _, h := range out["hits"].([]any) {
		if _, has := h.(map[string]any)["requires"]; has && h.(map[string]any)["id"] == w.loner {
			t.Errorf("a hit without requires carries the key: %v", h)
		}
	}
}
