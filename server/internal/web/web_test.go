package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TheAmbitiousCorry/trails/internal/mcp"
	"github.com/TheAmbitiousCorry/trails/internal/project"
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
	if _, err := store.EnsureID(repo); err != nil {
		t.Fatal(err)
	}
	reg := project.NewRegistry(t.TempDir(), 0)
	t.Cleanup(func() { reg.Close() })
	p, err := reg.Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	st := p.Store
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
	srv := &http.Server{Handler: mcp.Handler(reg, ln.Addr().String())}
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

	if _, err := st.DB.Exec(`INSERT INTO events (session_id, at, tool, target, result, shown_json) VALUES (?, ?, 'off_trail', 'src/other.go', 'turned_back', '["round-helper"]')`, a.sid, store.Now()); err != nil {
		t.Fatal(err)
	}

	b := newAgent(t, f.addr, "find the animal policy", "rt-other")
	f.other = b.sid
	b.must("search", map[string]any{"query": "giraffe policy"})
	b.must("search", map[string]any{"query": "ostrich policy"})
	b.must("resolve_gap", map[string]any{"gap_id": 2, "action": "dismiss", "reason": "not a real need"})
	return f
}

// url puts a data route (/api/map) under the fixture's project (/api/p/<id>/map).
func (f *fixture) url(route string) string {
	return "http://" + f.addr + "/api/p/" + f.st.ProjectID + strings.TrimPrefix(route, "/api")
}

func (f *fixture) get(route string) (int, map[string]any) {
	f.t.Helper()
	resp, err := http.Get(f.url(route))
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
	if off := m["off_trail"].([]any); len(off) != 2 || off[0].(map[string]any)["result"] != "ok" || off[1].(map[string]any)["result"] != "turned_back" || off[1].(map[string]any)["entry_id"] != "round-helper" {
		t.Errorf("off_trail results = %v", m["off_trail"])
	}
	if len(m["touched"].([]any)) != 2 {
		t.Errorf("touched = %v", m["touched"])
	}
	if code, _ := f.get("/api/sessions/nope"); code != 404 {
		t.Errorf("unknown session: %d", code)
	}
}

func TestSessionRoutePinsHopSequence(t *testing.T) {
	f := seed(t)
	_, m := f.get("/api/sessions/" + f.session)
	var got []string
	for _, s := range m["route"].([]any) {
		step := s.(map[string]any)
		got = append(got, fmt.Sprintf("%v|%v|%v|%v|%d", step["tool"], step["target"], step["miss"], step["turn_back"], len(step["prerequisites"].([]any))))
	}
	want := []string{
		"look|root|false|false|0",
		"look|billing|false|false|0",
		"search|zebra migration|true|false|0",
		"write|invoices|false|false|0",
		"write|invoices|false|false|0",
		"read|round-helper|false|false|1",
		"write|invoices|false|false|0",
		"resolve_gap|1|false|false|0",
		"session_end||false|false|0",
	}
	if !slices.Equal(got, want) {
		t.Errorf("route = %q", got)
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
	for _, route := range []string{"/", "/p/" + f.st.ProjectID + "/", "/p/" + f.st.ProjectID + "/sessions/abc", "/p/nope/signs/billing"} {
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
	for _, route := range []string{"/api/nope", "/api/map", "/api/p/nope/map"} {
		resp, err := http.Get("http://" + f.addr + route)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Errorf("%s: %d", route, resp.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", f.url("/api/map"), nil)
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

func TestSearchFindsFixtureTitleAndRejectsEmpty(t *testing.T) {
	f := seed(t)
	_, m := f.get("/api/search?q=" + "invoices+round+cents")
	hits := m["hits"].([]any)
	if m["q"] != "invoices round cents" || m["miss"] != false || len(hits) == 0 || hits[0].(map[string]any)["id"] != "invoices-round-to-cents" {
		t.Errorf("search = %v", m)
	}
	_, miss := f.get("/api/search?q=xyzzyplugh")
	if miss["miss"] != true || len(miss["hits"].([]any)) != 0 {
		t.Errorf("miss = %v", miss)
	}
	if code, _ := f.get("/api/search?q="); code != 400 {
		t.Errorf("empty q: %d", code)
	}
	if code, _ := f.get("/api/search"); code != 400 {
		t.Errorf("missing q: %d", code)
	}
}

func graphIDs(m map[string]any) []string {
	var ids []string
	for _, n := range m["nodes"].([]any) {
		ids = append(ids, n.(map[string]any)["id"].(string))
	}
	slices.Sort(ids)
	return ids
}

func TestGraphCountsAndFilters(t *testing.T) {
	f := seed(t)
	code, m := f.get("/api/graph")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	// 3 signs + 3 entries; edges: 2 sign->sign, 3 sign->entry, 1 requires.
	if len(m["nodes"].([]any)) != 6 || len(m["edges"].([]any)) != 6 || m["route"] != nil {
		t.Errorf("nodes %d, edges %d, route %v", len(m["nodes"].([]any)), len(m["edges"].([]any)), m["route"])
	}
	var requires int
	for _, e := range m["edges"].([]any) {
		if e.(map[string]any)["relation"] == "requires" {
			requires++
		}
	}
	if requires != 1 {
		t.Errorf("requires edges = %d", requires)
	}

	_, local := f.get("/api/graph?focus=round-helper")
	if got, want := graphIDs(local), []string{"invoices", "invoices-round-to-cents", "round-helper"}; !slices.Equal(got, want) {
		t.Errorf("depth 1 = %v, want %v", got, want)
	}
	_, wide := f.get("/api/graph?focus=round-helper&depth=2")
	if len(wide["nodes"].([]any)) <= len(local["nodes"].([]any)) {
		t.Errorf("depth 2 did not widen: %v", graphIDs(wide))
	}

	_, tagged := f.get("/api/graph?tag=undefined-tag")
	if got, want := graphIDs(tagged), []string{"billing", "invoices", "invoices-round-to-cents", "root"}; !slices.Equal(got, want) {
		t.Errorf("tag = %v, want %v", got, want)
	}

	for route, want := range map[string]int{
		"/api/graph?focus=nope":            404,
		"/api/graph?session=nope":          404,
		"/api/graph?focus=billing&depth=3": 400,
		"/api/graph?focus=billing&depth=0": 400,
		"/api/graph?focus=billing&depth=x": 400,
	} {
		if code, _ := f.get(route); code != want {
			t.Errorf("%s: %d, want %d", route, code, want)
		}
	}
}

func TestGraphSessionRoute(t *testing.T) {
	f := seed(t)
	_, m := f.get("/api/graph?session=" + f.session)
	sess := m["session"].(map[string]any)
	if sess["id"] != f.session || sess["task"] != "write the invoice report" || sess["ended_by"] == "" {
		t.Errorf("session = %v", sess)
	}
	var read, query bool
	for _, s := range m["route"].([]any) {
		h := s.(map[string]any)
		if h["tool"] == "read" && h["kind"] == "entry" && len(h["prerequisites"].([]any)) == 1 {
			read = true
		}
		if h["tool"] == "search" && h["kind"] == "query" && h["miss"] == true {
			query = true
		}
	}
	if !read || !query {
		t.Errorf("chained read %v, query marker %v in %v", read, query, m["route"])
	}
}

func (f *fixture) post(route string, body any) (int, string) {
	f.t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(f.url(route), "application/json", strings.NewReader(string(b)))
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := new(strings.Builder)
	io.Copy(buf, resp.Body)
	return resp.StatusCode, buf.String()
}

func TestCreateSignFromTheBrowser(t *testing.T) {
	f := seed(t)
	code, body := f.post("/api/signs", map[string]any{"parent_id": "root", "title": "Deploys", "move": []string{}})
	if code != 200 {
		t.Fatalf("create: %d %s", code, body)
	}
	var out struct {
		SignID string `json:"sign_id"`
		Title  string `json:"title"`
		Moved  int    `json:"moved"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.SignID != "deploys" || out.Title != "Deploys" || out.Moved != 0 {
		t.Fatalf("response %q: %v", body, err)
	}
	_, g := f.get("/api/graph")
	if !slices.Contains(graphIDs(g), "deploys") {
		t.Errorf("graph lacks the new sign: %v", graphIDs(g))
	}
	var ev int
	f.st.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE session_id = 'user' AND tool = 'write_sign' AND target = 'deploys' AND result = 'ok'`).Scan(&ev)
	if ev != 1 {
		t.Errorf("human write_sign events: %d", ev)
	}

	code, body = f.post("/api/signs", map[string]any{"parent_id": "root", "title": "Moved", "move": []string{"round-helper"}})
	if code != 400 || !strings.HasPrefix(body, "move_scope: ") {
		t.Errorf("move outside the parent: %d %q", code, body)
	}
	code, body = f.post("/api/signs", map[string]any{"parent_id": "root", "title": "  "})
	if code != 400 || !strings.HasPrefix(body, "schema:title: ") {
		t.Errorf("blank title: %d %q", code, body)
	}
	if code, _ = f.post("/api/signs", map[string]any{"parent_id": "nope", "title": "X"}); code != 404 {
		t.Errorf("unknown parent: %d", code)
	}
	var refused int
	f.st.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE session_id = 'user' AND tool = 'write_sign' AND result = 'refused:move_scope'`).Scan(&refused)
	if refused != 1 {
		t.Errorf("refused events: %d", refused)
	}
}

func TestSupersedeFromTheBrowser(t *testing.T) {
	f := seed(t)
	if _, err := f.st.DB.Exec(`UPDATE entries SET tier = 'verified' WHERE id = 'invoices-round-to-cents'`); err != nil {
		t.Fatal(err)
	}
	c := newAgent(t, f.addr, "change the rounding rule", "rt-conflict")
	c.call("write", map[string]any{"entry_id": "invoices-round-to-cents", "body": "Amounts round half to even."})
	var copyID string
	if err := f.st.DB.QueryRow(`SELECT CASE WHEN from_id = 'invoices-round-to-cents' THEN to_id ELSE from_id END FROM links WHERE relation = 'conflicts_with'`).Scan(&copyID); err != nil {
		t.Fatalf("no conflicting copy: %v", err)
	}

	if code, body := f.post("/api/entries/round-helper/supersede", map[string]any{"by": copyID}); code != 400 {
		t.Errorf("unlinked pair: %d %q", code, body)
	}
	if code, _ := f.post("/api/entries/nope/supersede", map[string]any{"by": copyID}); code != 404 {
		t.Errorf("unknown original: %d", code)
	}
	if code, _ := f.post("/api/entries/invoices-round-to-cents/supersede", map[string]any{"by": "nope"}); code != 404 {
		t.Errorf("unknown copy: %d", code)
	}
	code, body := f.post("/api/entries/invoices-round-to-cents/supersede", map[string]any{"by": copyID})
	if code != 200 {
		t.Fatalf("supersede: %d %s", code, body)
	}
	var orig, copyTier, by string
	f.st.DB.QueryRow(`SELECT tier, COALESCE(superseded_by, '') FROM entries WHERE id = 'invoices-round-to-cents'`).Scan(&orig, &by)
	f.st.DB.QueryRow(`SELECT tier FROM entries WHERE id = ?`, copyID).Scan(&copyTier)
	if orig != "dormant" || by != copyID || copyTier != "provisional" {
		t.Errorf("original %q superseded by %q, copy %q", orig, by, copyTier)
	}
	var links, ev int
	f.st.DB.QueryRow(`SELECT COUNT(*) FROM links WHERE relation = 'supersedes' AND from_id = ? AND to_id = 'invoices-round-to-cents'`, copyID).Scan(&links)
	f.st.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE session_id = 'user' AND tool = 'supersede' AND result = 'ok'`).Scan(&ev)
	if links != 1 || ev != 1 {
		t.Errorf("supersedes links %d, events %d", links, ev)
	}
}
