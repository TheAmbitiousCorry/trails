package web_test

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

	"github.com/TheAmbitiousCorry/trails/internal/mcp"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

type fixture struct {
	t       *testing.T
	st      *store.Store
	addr    string
	session string
	// other is the second agent's session, whose last search miss stays open
	other string
}

type agent struct {
	t    *testing.T
	sess *sdk.ClientSession
	sid  string
}

func newAgent(t *testing.T, addr, task, runtimeID string) *agent {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	a := &agent{t: t, sess: sess}
	out, _ := a.call("session_start", map[string]any{"task": task, "task_type": "report", "runtime_session_id": runtimeID})
	a.sid = out["session_id"].(string)
	return a
}

func (a *agent) call(tool string, args map[string]any) (map[string]any, bool) {
	a.t.Helper()
	if tool != "session_start" {
		args["session_id"] = a.sid
	}
	res, err := a.sess.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		a.t.Fatal(err)
	}
	if res.IsError {
		return nil, false
	}
	b, _ := json.Marshal(res.StructuredContent)
	var m map[string]any
	json.Unmarshal(b, &m)
	return m, true
}

func (a *agent) must(tool string, args map[string]any) map[string]any {
	a.t.Helper()
	m, ok := a.call(tool, args)
	if !ok {
		a.t.Fatalf("%s refused", tool)
	}
	return m
}

func seed(t *testing.T) *fixture {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	code := "package billing\n\n// Round rounds to cents.\nfunc Round(x float64) float64 {\n\treturn math.Round(x*100) / 100\n}\n"
	if err := os.WriteFile(filepath.Join(repo, "src", "round.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir(), repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, q := range []string{
		`INSERT INTO signs (id, parent_id, title) VALUES ('billing', 'root', 'Billing')`,
		`INSERT INTO signs (id, parent_id, title) VALUES ('invoices', 'billing', 'Invoices')`,
		`INSERT INTO tags (id, description, defined) VALUES ('billing', 'Money going out to clients', 1)`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mcp.Handler(st, ln.Addr().String())}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	f := &fixture{t: t, st: st, addr: ln.Addr().String()}

	a := newAgent(t, f.addr, "write the invoice report", "rt-web")
	f.session = a.sid
	a.must("look", map[string]any{"sign_id": "root"})
	a.must("look", map[string]any{"sign_id": "billing"})
	if m := a.must("search", map[string]any{"query": "zebra migration"}); m["miss"] != true {
		t.Fatalf("search did not miss: %v", m)
	}
	a.must("write", map[string]any{"parent_sign_id": "invoices", "kind": "decision", "decided_by": "ana", "decided_at": "2026-10-27",
		"title": "Invoices round to cents", "read_when": "you change invoice rounding", "body": "Amounts round half away from zero.", "tags": []string{"billing", "undefined-tag"}})
	a.must("write", map[string]any{"parent_sign_id": "invoices", "kind": "example", "title": "Round helper", "read_when": "you call the rounding helper",
		"body": "Call `Round` before storing.", "tags": []string{"billing"}, "requires": []string{"invoices-round-to-cents"},
		"anchors": []map[string]any{{"type": "range", "path": "src/round.go", "start": 3, "end": 6}}})
	a.must("read", map[string]any{"entry_id": "round-helper", "depth": "full"})
	// A write the rules refuse, so the rejection log has a row.
	if _, ok := a.call("write", map[string]any{"parent_sign_id": "invoices", "kind": "decision", "title": "No author", "read_when": "never", "body": "x"}); ok {
		t.Fatal("decision without decided_by was accepted")
	}
	a.must("resolve_gap", map[string]any{"gap_id": 1, "action": "merge", "target_id": "invoices-round-to-cents"})
	a.must("session_end", map[string]any{})

	// The session is closed, so the hook endpoint would ignore it; record the off-trail row directly.
	if _, err := st.DB.Exec(`INSERT INTO events (session_id, at, tool, target, result) VALUES (?, ?, 'off_trail', 'src/round.go', 'ok')`, a.sid, store.Now()); err != nil {
		t.Fatal(err)
	}

	b := newAgent(t, f.addr, "find the animal policy", "rt-other")
	f.other = b.sid
	b.must("search", map[string]any{"query": "giraffe policy"})
	b.must("search", map[string]any{"query": "ostrich policy"})
	b.must("resolve_gap", map[string]any{"gap_id": 2, "action": "dismiss", "reason": "not a real need"})
	return f
}

func (f *fixture) get(route string) (int, map[string]any) {
	f.t.Helper()
	resp, err := http.Get("http://" + f.addr + route)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

func keys(m map[string]any, names ...string) []string {
	var missing []string
	for _, n := range names {
		if _, ok := m[n]; !ok {
			missing = append(missing, n)
		}
	}
	return missing
}

func TestEveryRouteReturnsItsFields(t *testing.T) {
	f := seed(t)
	cases := []struct {
		route  string
		fields []string
	}{
		{"/api/map", []string{"project", "root", "tags", "signs", "tag", "within"}},
		{"/api/map?tag=billing", []string{"root", "tags"}},
		{"/api/map?tag=billing&within=billing", []string{"root"}},
		{"/api/signs/invoices", []string{"view", "path", "stale", "open_gaps", "groups"}},
		{"/api/entries/round-helper", []string{"entry", "path", "versions", "links", "backlinks", "chain"}},
		{"/api/entries/round-helper/snippet", []string{"text", "start", "end", "path", "ok"}},
		{"/api/sessions", []string{"sessions"}},
		{"/api/sessions/" + f.session, []string{"session", "route", "off_trail", "touched"}},
		{"/api/gaps?state=open", []string{"state", "gaps"}},
		{"/api/provisional", []string{"entries"}},
		{"/api/tags", []string{"tags"}},
		{"/api/rejections", []string{"total", "groups", "recent"}},
		{"/api/dismissals", []string{"dismissals"}},
	}
	for _, c := range cases {
		code, m := f.get(c.route)
		if code != 200 {
			t.Errorf("%s: status %d", c.route, code)
			continue
		}
		if missing := keys(m, c.fields...); len(missing) > 0 {
			t.Errorf("%s: missing %v in %v", c.route, missing, m)
		}
	}
}

func TestMapCountsAndVirtualTree(t *testing.T) {
	f := seed(t)
	_, m := f.get("/api/map")
	root := m["root"].(map[string]any)
	if root["entries"] != float64(3) || root["open_gaps"] != float64(1) {
		t.Errorf("root = entries %v, open_gaps %v", root["entries"], root["open_gaps"])
	}
	if root["reads_30d"].(float64) < 1 {
		t.Errorf("reads_30d = %v", root["reads_30d"])
	}
	_, v := f.get("/api/map?tag=billing")
	vroot := v["root"].(map[string]any)
	if vroot["entries"] != float64(2) || len(vroot["children"].([]any)) != 1 {
		t.Errorf("tag view root = %v", vroot)
	}
	if code, _ := f.get("/api/map?within=nope"); code != 404 {
		t.Errorf("unknown within: %d", code)
	}
}

func TestSignPageGroupsEntriesByKind(t *testing.T) {
	f := seed(t)
	_, m := f.get("/api/signs/invoices")
	groups := m["groups"].([]any)
	if len(groups) != 2 || groups[0].(map[string]any)["kind"] != "decision" || groups[1].(map[string]any)["kind"] != "example" {
		t.Fatalf("groups = %v", groups)
	}
	ex := groups[1].(map[string]any)["entries"].([]any)[0].(map[string]any)
	if ex["chain_length"] != float64(1) || !strings.Contains(ex["body_markdown"].(string), "Round") || len(ex["anchors"].([]any)) != 1 {
		t.Errorf("example = %v", ex)
	}
	if code, _ := f.get("/api/signs/nope"); code != 404 {
		t.Errorf("unknown sign: %d", code)
	}
}

func TestSnippetIsTheAnchoredRange(t *testing.T) {
	f := seed(t)
	_, m := f.get("/api/entries/round-helper/snippet")
	text, _ := m["text"].(string)
	if !strings.HasPrefix(text, "// Round rounds to cents.") || strings.Contains(text, "package billing") || m["end"] != float64(6) {
		t.Errorf("snippet = %v", m)
	}
	if code, _ := f.get("/api/entries/invoices-round-to-cents/snippet"); code != 404 {
		t.Errorf("entry without a file anchor: %d", code)
	}
}

func TestSessionRouteMarksMissOffTrailAndWrites(t *testing.T) {
	f := seed(t)
	_, m := f.get("/api/sessions/" + f.session)
	var miss, read bool
	for _, s := range m["route"].([]any) {
		step := s.(map[string]any)
		if step["tool"] == "search" && step["miss"] == true && step["gap_id"].(float64) > 0 {
			miss = true
		}
		if step["tool"] == "read" && step["target"] == "round-helper" && len(step["prerequisites"].([]any)) == 1 && step["touched"] == true {
			read = true
		}
	}
	if !miss || !read {
		t.Errorf("miss marker %v, chained read %v in %v", miss, read, m["route"])
	}
	if off := m["off_trail"].([]any); len(off) == 0 || off[0].(map[string]any)["entry_id"] != "round-helper" {
		t.Errorf("off_trail = %v", m["off_trail"])
	}
	if len(m["touched"].([]any)) != 2 {
		t.Errorf("touched = %v", m["touched"])
	}
	if code, _ := f.get("/api/sessions/nope"); code != 404 {
		t.Errorf("unknown session: %d", code)
	}
}

func TestQueuesAndLogs(t *testing.T) {
	f := seed(t)
	_, g := f.get("/api/gaps")
	if gaps := g["gaps"].([]any); len(gaps) != 1 || gaps[0].(map[string]any)["query"] != "ostrich policy" {
		t.Errorf("open gaps = %v", gaps)
	}
	if code, _ := f.get("/api/gaps?state=bogus"); code != 400 {
		t.Errorf("bad state: %d", code)
	}
	_, d := f.get("/api/dismissals")
	if ds := d["dismissals"].([]any); len(ds) != 1 || ds[0].(map[string]any)["reason"] != "not a real need" {
		t.Errorf("dismissals = %v", ds)
	}
	_, r := f.get("/api/rejections")
	if r["total"].(float64) < 1 || r["groups"].([]any)[0].(map[string]any)["actor_kind"] != "agent" {
		t.Errorf("rejections = %v", r)
	}
	_, p := f.get("/api/provisional")
	if len(p["entries"].([]any)) != 2 {
		t.Errorf("provisional = %v", p)
	}
	_, tg := f.get("/api/tags")
	tags := tg["tags"].([]any)
	if tags[0].(map[string]any)["id"] != "undefined-tag" || tags[0].(map[string]any)["defined"] != false {
		t.Errorf("undefined tags must lead: %v", tags)
	}
}

func TestFrontEndServedWithFallbackAndGuard(t *testing.T) {
	f := seed(t)
	for _, route := range []string{"/", "/sessions/abc", "/signs/billing"} {
		resp, err := http.Get("http://" + f.addr + route)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 64)
		n, _ := resp.Body.Read(buf)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(strings.ToLower(string(buf[:n])), "<!doctype html>") {
			t.Errorf("%s: %d %q", route, resp.StatusCode, buf[:n])
		}
	}
	if code, _ := f.get("/api/nope"); code != 404 {
		t.Errorf("unknown api path: %d", code)
	}
	req, _ := http.NewRequest("GET", "http://"+f.addr+"/api/map", nil)
	req.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign origin: %d", resp.StatusCode)
	}
}
