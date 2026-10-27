package mcp_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/entries"
)

func ids(t *testing.T, v any) []string {
	t.Helper()
	list, _ := v.([]any)
	var out []string
	for _, e := range list {
		out = append(out, e.(map[string]any)["id"].(string))
	}
	return out
}

func TestWriteThenReadChain(t *testing.T) {
	st, addr := startServer(t)
	ctx := context.Background()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	call := func(name string, args map[string]any) map[string]any {
		res, err := sess.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		return structured(t, res, err)
	}
	sid := call("session_start", map[string]any{"task": "walk", "task_type": "report"})["session_id"].(string)

	w := call("write", map[string]any{"session_id": sid, "parent_sign_id": "root", "kind": "decision", "title": "Ship weekly",
		"read_when": "you plan a release", "body": "We ship on Fridays.", "decided_by": "ana", "decided_at": "2026-10-27"})
	if w["tier"] != "provisional" || w["entry_id"] != "ship-weekly" {
		t.Fatalf("write = %v", w)
	}
	r := call("read", map[string]any{"session_id": sid, "entry_id": "ship-weekly", "depth": "full"})
	if r["tier"] != "provisional" {
		t.Errorf("written entry reads as %v", r["tier"])
	}

	res, err := sess.CallTool(ctx, &sdk.CallToolParams{Name: "write", Arguments: map[string]any{"session_id": sid, "parent_sign_id": "nowhere", "kind": "decision",
		"title": "Lost", "read_when": "never", "body": "x", "decided_by": "ana", "decided_at": "2026-10-27"}})
	if err != nil || !res.IsError {
		t.Errorf("unreachable parent not refused: %v", err)
	}
	var result string
	st.DB.QueryRow(`SELECT result FROM events WHERE tool = 'write' ORDER BY id DESC LIMIT 1`).Scan(&result)
	if result != "refused:unreachable" {
		t.Errorf("event result = %q", result)
	}
}

func TestReadServesRequiresChainAndEdges(t *testing.T) {
	st, addr := startServer(t)
	ctx := context.Background()
	checker := anchor.Checker{Repo: st.Repo}
	put := func(title string, edit func(*entries.WriteRequest)) string {
		req := entries.WriteRequest{Actor: "s-seed", ParentSignID: "root", Kind: "decision", Title: title, ReadWhen: "when " + title,
			BodyMarkdown: "Body of " + title + ".", DecidedBy: "ana", DecidedAt: "2026-10-27"}
		if edit != nil {
			edit(&req)
		}
		res, err := entries.Write(ctx, st.DB, checker, req)
		if err != nil || res.Refusal != nil {
			t.Fatalf("seed %s: %v %v", title, err, res.Refusal)
		}
		return res.EntryID
	}
	a := put("A", nil)
	b := put("B", func(r *entries.WriteRequest) { r.Requires = []string{a} })
	s := put("Sibling", nil)
	z := put("Z", func(r *entries.WriteRequest) { r.Requires = []string{a, b}; r.SeeAlso = []string{s} })
	put("Pointer", func(r *entries.WriteRequest) { r.Requires = []string{z} })

	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	call := func(name string, args map[string]any) map[string]any {
		res, err := sess.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		return structured(t, res, err)
	}
	sid := call("session_start", map[string]any{"task": "walk", "task_type": "report"})["session_id"].(string)

	first := call("read", map[string]any{"session_id": sid, "entry_id": z, "depth": "full"})
	if got := ids(t, first["prerequisites"]); len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("prerequisites = %v", got)
	}
	for _, p := range first["prerequisites"].([]any) {
		if p.(map[string]any)["untrusted"].(map[string]any)["body_markdown"] == "" {
			t.Errorf("prerequisite served without a body: %v", p)
		}
	}
	if first["id"] != z || strings.TrimSpace(first["untrusted"].(map[string]any)["body_markdown"].(string)) != "Body of Z." {
		t.Errorf("entry = %v", first)
	}
	if got := ids(t, first["related"]); len(got) != 1 || got[0] != s {
		t.Errorf("related = %v", got)
	}
	back := ids(t, first["backlinks"])
	if len(back) != 2 {
		t.Errorf("backlinks = %v", back)
	}
	// Edge lines carry no body.
	for _, e := range first["related"].([]any) {
		if _, has := e.(map[string]any)["untrusted"]; has {
			t.Error("edge line carries a body")
		}
	}

	second := call("read", map[string]any{"session_id": sid, "entry_id": z, "depth": "full"})
	if second["prerequisites"] != nil {
		t.Errorf("second read repeated prerequisites: %v", second["prerequisites"])
	}
	if got := ids(t, second["already_read"]); len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("already_read = %v", got)
	}
	for i, r := range second["already_read"].([]any) {
		m := r.(map[string]any)
		title := []string{"A", "B"}[i]
		if m["title"] != title || m["read_when"] != "when "+title || m["tier"] == nil || m["tier"] == "" {
			t.Errorf("already_read item = %v", m)
		}
	}
	if second["prerequisite_note"] != "These prerequisites still apply to this entry. They were served earlier in this session; read one directly if it is no longer in your context." {
		t.Errorf("prerequisite_note = %v", second["prerequisite_note"])
	}
	if _, has := first["prerequisite_note"]; has {
		t.Error("read with nothing already read carries a note")
	}

	var n int
	st.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE session_id = ? AND tool = 'read' AND depth = 'full' AND target IN (?, ?)`, sid, a, b).Scan(&n)
	if n != 2 {
		t.Errorf("prerequisite read events = %d, want 2", n)
	}
}

func TestWriteSignThroughTheRecorder(t *testing.T) {
	st, addr := startServer(t)
	ctx := context.Background()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	call := func(name string, args map[string]any) *sdk.CallToolResult {
		res, err := sess.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	sid := structured(t, call("session_start", map[string]any{"task": "split"}), nil)["session_id"].(string)
	call("write", map[string]any{"session_id": sid, "parent_sign_id": "root", "kind": "decision", "title": "Ship weekly",
		"read_when": "you plan a release", "body": "We ship on Fridays.", "decided_by": "ana", "decided_at": "2026-10-27"})

	out := structured(t, call("write_sign", map[string]any{"session_id": sid, "parent_sign_id": "root", "title": "Releases", "move": []string{"ship-weekly"}}), nil)
	if out["sign_id"] != "releases" || out["moved"] != float64(1) || out["lines"].(float64) < 1 {
		t.Fatalf("write_sign = %v", out)
	}
	var target, result string
	st.DB.QueryRow(`SELECT target, result FROM events WHERE tool = 'write_sign' ORDER BY id DESC LIMIT 1`).Scan(&target, &result)
	if target != "releases" || result != "ok" {
		t.Errorf("event target %q result %q", target, result)
	}

	res := call("write_sign", map[string]any{"session_id": sid, "parent_sign_id": "root", "title": "Again", "move": []string{"ghost"}})
	if !res.IsError {
		t.Fatal("move_scope not refused")
	}
	st.DB.QueryRow(`SELECT target, result FROM events WHERE tool = 'write_sign' ORDER BY id DESC LIMIT 1`).Scan(&target, &result)
	if target != "root" || result != "refused:move_scope" {
		t.Errorf("refused event target %q result %q", target, result)
	}

	call("session_end", map[string]any{"session_id": sid})
	if res := call("write_sign", map[string]any{"session_id": sid, "parent_sign_id": "root", "title": "Late"}); !res.IsError {
		t.Error("closed session accepted write_sign")
	}
	st.DB.QueryRow(`SELECT result FROM events WHERE tool = 'write_sign' ORDER BY id DESC LIMIT 1`).Scan(&result)
	if result != "refused:closed_session" {
		t.Errorf("closed result = %q", result)
	}
}

func TestReadReturnsAnchorsAndTags(t *testing.T) {
	st, addr := startServer(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(st.Repo, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := entries.Write(ctx, st.DB, anchor.Checker{Repo: st.Repo}, entries.WriteRequest{Actor: "s-seed", ParentSignID: "root", Kind: "example", Title: "Anchored",
		ReadWhen: "you need the anchored thing", BodyMarkdown: "A body.\n\nSecond paragraph.", Tags: []string{"billing"},
		Anchors: []anchor.Anchor{{Type: "range", PathOrURL: "a.txt", RangeStart: 1, RangeEnd: 2}}})
	if err != nil || res.Refusal != nil {
		t.Fatalf("seed: %v %v", err, res.Refusal)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	call := func(name string, args map[string]any) map[string]any {
		r, err := sess.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		return structured(t, r, err)
	}
	sid := call("session_start", map[string]any{"task": "walk"})["session_id"].(string)
	for _, depth := range []string{"summary", "full"} {
		r := call("read", map[string]any{"session_id": sid, "entry_id": res.EntryID, "depth": depth})
		if tags, _ := r["tags"].([]any); len(tags) != 1 || tags[0] != "billing" {
			t.Errorf("%s: tags = %v", depth, r["tags"])
		}
		as, _ := r["anchors"].([]any)
		if len(as) != 1 {
			t.Fatalf("%s: anchors = %v", depth, r["anchors"])
		}
		a := as[0].(map[string]any)
		if a["type"] != "range" || a["path"] != "a.txt" || a["start"] != float64(1) || a["end"] != float64(2) || a["ok"] != true {
			t.Errorf("%s: anchor = %v", depth, a)
		}
		if _, has := r["untrusted"].(map[string]any)["tags"]; has {
			t.Errorf("%s: tags leaked into untrusted", depth)
		}
	}
}
