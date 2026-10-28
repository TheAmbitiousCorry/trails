package mcp_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/mcp"
	"github.com/TheAmbitiousCorry/trails/internal/project"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

// serveRepos opens each repo as a project in one registry and serves the registry on a loopback port.
func serveRepos(t *testing.T, repos ...string) (*project.Registry, string) {
	t.Helper()
	return serveHome(t, t.TempDir(), repos...)
}

// serveHome is serveRepos over a given Trail home, after OpenKnown has reopened what an earlier server left there.
func serveHome(t *testing.T, home string, repos ...string) (*project.Registry, string) {
	t.Helper()
	reg := project.NewRegistry(home, 0)
	t.Cleanup(func() { reg.Close() })
	reg.OpenKnown()
	for _, repo := range repos {
		if _, err := store.EnsureID(repo); err != nil {
			t.Fatal(err)
		}
		if _, err := reg.Open(repo); err != nil {
			t.Fatal(err)
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mcp.Handler(reg, ln.Addr().String())}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return reg, ln.Addr().String()
}

func startServer(t *testing.T) (*store.Store, string) {
	t.Helper()
	reg, addr := serveRepos(t, t.TempDir())
	return reg.List()[0].Store, addr
}

func structured(t *testing.T, res *sdk.CallToolResult, err error) map[string]any {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %+v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestVerticalThread(t *testing.T) {
	st, addr := startServer(t)
	ctx := context.Background()

	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:   "http://" + addr + "/mcp",
		HTTPClient: http.DefaultClient,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	res, err := sess.CallTool(ctx, &sdk.CallToolParams{Name: "session_start", Arguments: map[string]any{"task": "write the monthly report", "task_type": "report"}})
	start := structured(t, res, err)
	if start["contract"] != float64(mcp.Contract) {
		t.Errorf("contract = %v", start["contract"])
	}
	sid, _ := start["session_id"].(string)
	if sid == "" {
		t.Fatal("no session_id")
	}
	o := start["orient"].(map[string]any)
	if o["entry_count"] != float64(1) || o["edge_count"] != float64(0) {
		t.Errorf("orient = %v", o)
	}

	res, err = sess.CallTool(ctx, &sdk.CallToolParams{Name: "read", Arguments: map[string]any{"session_id": sid, "entry_id": "trail", "depth": "full"}})
	read := structured(t, res, err)
	u, ok := read["untrusted"].(map[string]any)
	if !ok || !strings.Contains(u["body_markdown"].(string), "Every call an agent makes") {
		t.Errorf("body not inside untrusted: %v", read)
	}
	if _, leaked := read["body_markdown"]; leaked {
		t.Error("body_markdown outside untrusted")
	}
	if read["lines"].(float64) < 1 {
		t.Errorf("lines = %v", read["lines"])
	}

	res, err = sess.CallTool(ctx, &sdk.CallToolParams{Name: "look", Arguments: map[string]any{"session_id": sid, "sign_id": "root"}})
	look := structured(t, res, err)
	if edges, _ := look["edges"].([]any); len(edges) != 1 {
		t.Errorf("look edges = %v", look["edges"])
	}
	if look["lines"].(float64) < 1 {
		t.Errorf("look lines = %v", look["lines"])
	}
	res, err = sess.CallTool(ctx, &sdk.CallToolParams{Name: "look", Arguments: map[string]any{"session_id": sid, "sign_id": "root", "depth": 4}})
	if err != nil || !res.IsError {
		t.Errorf("depth 4 not refused: %v", err)
	}

	// Anchor the seeded entry to a file, edit the file, and the next read serves it stale with its body and a notice.
	file := filepath.Join(st.Repo, "notes.txt")
	if err := os.WriteFile(file, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checker := anchor.Checker{Repo: st.Repo}
	a, err := checker.Resolve(ctx, anchor.Anchor{EntryID: "trail", Type: "file", PathOrURL: "notes.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := anchor.Insert(ctx, st.DB, a); err != nil {
		t.Fatal(err)
	}
	res, err = sess.CallTool(ctx, &sdk.CallToolParams{Name: "read", Arguments: map[string]any{"session_id": sid, "entry_id": "trail", "depth": "full"}})
	if fresh := structured(t, res, err); fresh["tier"] != "verified" || fresh["untrusted"].(map[string]any)["body_markdown"] == "" {
		t.Errorf("anchored entry unread-able while the anchor holds: %v", fresh)
	}
	if err := os.WriteFile(file, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = sess.CallTool(ctx, &sdk.CallToolParams{Name: "read", Arguments: map[string]any{"session_id": sid, "entry_id": "trail", "depth": "full"}})
	stale := structured(t, res, err)
	if stale["tier"] != "stale" || stale["untrusted"].(map[string]any)["body_markdown"] == "" {
		t.Errorf("stale read = %v", stale)
	}
	if n, _ := stale["notice"].(string); !strings.Contains(n, "notes.txt") {
		t.Errorf("notice does not name the failed anchor: %q", n)
	}

	rows, err := st.DB.Query(`SELECT tool, session_id, result FROM events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tools []string
	for rows.Next() {
		var tool, s, result string
		rows.Scan(&tool, &s, &result)
		if s != sid || (result != "ok" && result != "refused:depth_cap") {
			t.Errorf("event %s: session %q result %q", tool, s, result)
		}
		tools = append(tools, tool)
	}
	if strings.Join(tools, ",") != "session_start,read,look,look,read,read" {
		t.Errorf("events = %v", tools)
	}

	// Refused calls must not add events: they never reach the recorder.
	post := func(h map[string]string) int {
		req, _ := http.NewRequest("POST", "http://"+addr+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		for k, v := range h {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := post(map[string]string{"Origin": "http://evil.example"}); c != http.StatusForbidden {
		t.Errorf("foreign origin: status %d", c)
	}
	if c := post(map[string]string{"Origin": "http://" + addr}); c != http.StatusOK {
		t.Errorf("own origin: status %d", c)
	}

	var n int
	st.DB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n)
	if n != 6 {
		t.Errorf("events = %d, want 6", n)
	}
}

func TestSearchHitMissAndGap(t *testing.T) {
	st, addr := startServer(t)
	ctx := context.Background()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	call := func(name string, args map[string]any) (*sdk.CallToolResult, error) {
		return sess.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	}

	res, err := call("session_start", map[string]any{"task": "find the invoice rule", "task_type": "billing"})
	sid := structured(t, res, err)["session_id"].(string)

	write := func(title, body string) (*sdk.CallToolResult, error) {
		return call("write", map[string]any{"session_id": sid, "parent_sign_id": "root", "kind": "decision", "title": title,
			"read_when": "you study " + title, "body": body, "decided_by": "ana", "decided_at": "2026-10-27"})
	}
	for i, topic := range []string{"harbour tides", "orchard grafting", "violin varnish", "glacier survey", "pottery kiln", "beekeeping winter"} {
		res, err := write("Notes on "+topic, "Filler "+string(rune('a'+i))+" covers "+topic+" only.")
		structured(t, res, err)
	}
	res, err = write("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals.")
	structured(t, res, err)

	// A second identical write is refused with the three options named.
	res, err = write("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals.")
	if err != nil || !res.IsError {
		t.Fatalf("duplicate write not refused: %v", err)
	}
	if txt := res.Content[0].(*sdk.TextContent).Text; !strings.HasPrefix(txt, "duplicate:") || !strings.Contains(txt, "merge") || !strings.Contains(txt, "replace") || !strings.Contains(txt, "distinct") {
		t.Errorf("duplicate refusal = %q", txt)
	}

	// The miss gap stands where the session last looked.
	res, err = call("look", map[string]any{"session_id": sid, "sign_id": "root"})
	structured(t, res, err)

	res, err = call("search", map[string]any{"session_id": sid, "query": "invoice rounding cent"})
	hit := structured(t, res, err)
	hits, _ := hit["hits"].([]any)
	if hit["miss"] != false || len(hits) != 1 {
		t.Fatalf("search hit = %v", hit)
	}
	h := hits[0].(map[string]any)
	path, _ := h["sign_path"].([]any)
	if h["id"] != "quarterly-invoice-rounding" || h["tier"] != "provisional" || h["kind"] != "decision" || len(path) != 1 {
		t.Errorf("hit = %v", h)
	}
	var gaps int
	st.DB.QueryRow(`SELECT COUNT(*) FROM gaps`).Scan(&gaps)
	if gaps != 0 {
		t.Errorf("a search hit opened %d gaps", gaps)
	}

	res, err = call("search", map[string]any{"session_id": sid, "query": "qzxv wplk jnrt", "limit": 50})
	miss := structured(t, res, err)
	if miss["miss"] != true || len(miss["hits"].([]any)) != 0 {
		t.Fatalf("search miss = %v", miss)
	}
	var signID, session, query, state string
	if err := st.DB.QueryRow(`SELECT sign_id, session_id, query, state FROM gaps`).Scan(&signID, &session, &query, &state); err != nil {
		t.Fatalf("no gap row: %v", err)
	}
	if signID != "root" || session != sid || query != "qzxv wplk jnrt" || state != "open" {
		t.Errorf("gap = %q %q %q %q", signID, session, query, state)
	}
	var results []string
	rows, _ := st.DB.Query(`SELECT result FROM events WHERE tool = 'search' ORDER BY id`)
	defer rows.Close()
	for rows.Next() {
		var r string
		rows.Scan(&r)
		results = append(results, r)
	}
	if strings.Join(results, ",") != "ok,miss" {
		t.Errorf("search event results = %v", results)
	}
	var shown string
	st.DB.QueryRow(`SELECT shown_json FROM events WHERE tool = 'search' ORDER BY id LIMIT 1`).Scan(&shown)
	if shown != `["quarterly-invoice-rounding"]` {
		t.Errorf("shown = %s", shown)
	}
}
