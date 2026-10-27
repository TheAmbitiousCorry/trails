package mcp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

func startRepoServer(t *testing.T) (*store.Store, string, string) {
	t.Helper()
	repo := t.TempDir()
	reg, addr := serveRepos(t, repo)
	return reg.List()[0].Store, addr, repo
}

func postAPI(t *testing.T, addr, route string, body map[string]any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post("http://"+addr+route, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("%s: status %d", route, resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOffTrailEndpoint(t *testing.T) {
	st, addr, repo := startRepoServer(t)
	if err := os.MkdirAll(filepath.Join(repo, "src", "billing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "billing", "round.go"), []byte("package billing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newAgent(t, addr, "rt-1")
	res, err := a.raw("write", map[string]any{"parent_sign_id": "root", "kind": "decision", "decided_by": "ana", "decided_at": "2026-10-27", "title": "Billing rounding", "read_when": "you change invoice rounding",
		"body": "Rounding lives in one file.", "anchors": []map[string]any{{"type": "file", "path": "src/billing/round.go"}}})
	if err != nil || res.IsError {
		t.Fatalf("write: %v %v", err, res.Content[0])
	}
	var id string
	if err := st.DB.QueryRow(`SELECT entry_id FROM anchors WHERE path_or_url = 'src/billing/round.go'`).Scan(&id); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{filepath.Join(repo, "src/billing/round.go"), "src/billing/round.go"} {
		got := postAPI(t, addr, "/api/off-trail", map[string]any{"runtime_session_id": "rt-1", "path": p})
		if got["entry_id"] != id || got["read_when"] != "you change invoice rounding" {
			t.Errorf("signpost for %s = %v", p, got)
		}
	}
	var target string
	st.DB.QueryRow(`SELECT target FROM events WHERE tool = 'off_trail' AND session_id = ? LIMIT 1`, a.sid).Scan(&target)
	if target != "src/billing/round.go" {
		t.Errorf("off_trail target = %q, want repo-relative", target)
	}

	if got := postAPI(t, addr, "/api/off-trail", map[string]any{"runtime_session_id": "rt-1", "path": "/etc/hosts"}); len(got) != 0 {
		t.Errorf("unanchored path replied %v", got)
	}
	st.DB.QueryRow(`SELECT target FROM events WHERE tool = 'off_trail' AND session_id = ? ORDER BY id DESC LIMIT 1`, a.sid).Scan(&target)
	if target != "/etc/hosts" {
		t.Errorf("outside path logged as %q", target)
	}

	var before, after int
	st.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE tool = 'off_trail'`).Scan(&before)
	if got := postAPI(t, addr, "/api/off-trail", map[string]any{"runtime_session_id": "nope", "path": "src/billing/round.go"}); len(got) != 0 {
		t.Errorf("unknown runtime id replied %v", got)
	}
	st.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE tool = 'off_trail'`).Scan(&after)
	if before != after {
		t.Errorf("unknown runtime id recorded %d rows", after-before)
	}

	resp, err := http.Get("http://" + addr + "/api/off-trail")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status %d", resp.StatusCode)
	}
}

func TestStopCheckBlocksThenForces(t *testing.T) {
	st, addr, _ := startRepoServer(t)
	a := newAgent(t, addr, "rt-2")

	if got := postAPI(t, addr, "/api/stop-check", map[string]any{"runtime_session_id": "ghost"}); got["open"] != false {
		t.Errorf("no session: %v", got)
	}
	for want := 1; want <= 2; want++ {
		got := postAPI(t, addr, "/api/stop-check", map[string]any{"runtime_session_id": "rt-2"})
		if got["open"] != true || got["blocks"] != float64(want) {
			t.Fatalf("call %d: %v", want, got)
		}
	}
	got := postAPI(t, addr, "/api/stop-check", map[string]any{"runtime_session_id": "rt-2"})
	if got["open"] != false || got["forced"] != true || got["blocks"] != float64(3) {
		t.Fatalf("third call: %v", got)
	}
	var endedBy string
	st.DB.QueryRow(`SELECT ended_by FROM sessions WHERE id = ?`, a.sid).Scan(&endedBy)
	if endedBy != "forced" {
		t.Errorf("ended_by = %q", endedBy)
	}
	if got := postAPI(t, addr, "/api/stop-check", map[string]any{"runtime_session_id": "rt-2"}); got["open"] != false {
		t.Errorf("after forced: %v", got)
	}
}

func TestAPIRefusesForeignHost(t *testing.T) {
	_, addr, _ := startRepoServer(t)
	req, _ := http.NewRequest("POST", "http://"+addr+"/api/stop-check", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func touch(t *testing.T, repo string, rels ...string) {
	t.Helper()
	for _, r := range rels {
		p := filepath.Join(repo, r)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEditedEndpoint(t *testing.T) {
	st, addr, repo := startRepoServer(t)
	touch(t, repo, "src/billing/round.go", "docs/guide/intro.md")
	a := newAgent(t, addr, "rt-3")
	file := a.ok("write", map[string]any{"parent_sign_id": "root", "kind": "decision", "decided_by": "ana", "decided_at": "2026-10-27", "title": "Billing rounding", "read_when": "you change invoice rounding",
		"body": "Rounding lives in one file.", "anchors": []map[string]any{{"type": "file", "path": "src/billing/round.go"}}})["entry_id"].(string)
	dir := a.ok("write", map[string]any{"parent_sign_id": "root", "kind": "decision", "decided_by": "ana", "decided_at": "2026-10-27", "title": "Docs layout", "read_when": "you reorganise the docs",
		"body": "Docs are flat.", "anchors": []map[string]any{{"type": "dir", "path": "docs"}}})["entry_id"].(string)

	entryIDs := func(got map[string]any) []string {
		var ids []string
		for _, e := range got["entries"].([]any) {
			ids = append(ids, e.(map[string]any)["entry_id"].(string))
		}
		return ids
	}

	got := postAPI(t, addr, "/api/edited", map[string]any{"runtime_session_id": "rt-3", "path": filepath.Join(repo, "src/billing/round.go")})
	if ids := entryIDs(got); len(ids) != 1 || ids[0] != file {
		t.Errorf("file anchor: %v", got)
	}
	if e := got["entries"].([]any)[0].(map[string]any); e["read_when"] != "you change invoice rounding" {
		t.Errorf("read_when = %v", e["read_when"])
	}
	got = postAPI(t, addr, "/api/edited", map[string]any{"runtime_session_id": "rt-3", "path": "docs/guide/intro.md"})
	if ids := entryIDs(got); len(ids) != 1 || ids[0] != dir {
		t.Errorf("dir anchor: %v", got)
	}
	got = postAPI(t, addr, "/api/edited", map[string]any{"runtime_session_id": "rt-3", "path": "src/other.go"})
	if e, ok := got["entries"].([]any); !ok || len(e) != 0 {
		t.Errorf("unanchored path: %v", got)
	}

	var target string
	st.DB.QueryRow(`SELECT target FROM events WHERE tool = 'edit' AND session_id = ? ORDER BY id LIMIT 1`, a.sid).Scan(&target)
	if target != "src/billing/round.go" {
		t.Errorf("edit target = %q, want repo-relative", target)
	}

	var before, after int
	st.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE tool = 'edit'`).Scan(&before)
	got = postAPI(t, addr, "/api/edited", map[string]any{"runtime_session_id": "nope", "path": "src/billing/round.go"})
	if e, ok := got["entries"].([]any); !ok || len(e) != 0 {
		t.Errorf("unknown session replied %v", got)
	}
	st.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE tool = 'edit'`).Scan(&after)
	if before != after {
		t.Errorf("unknown session recorded %d rows", after-before)
	}
}

func TestSessionEndRefusesUnconfirmedEdits(t *testing.T) {
	_, addr, repo := startRepoServer(t)
	touch(t, repo, "src/billing/round.go", "src/tax/tax.go")
	author := newAgent(t, addr, "")
	anchors := []map[string]any{{"type": "file", "path": "src/billing/round.go"}}
	id := author.ok("write", map[string]any{"parent_sign_id": "root", "kind": "decision", "decided_by": "ana", "decided_at": "2026-10-27", "title": "Billing rounding", "read_when": "you change invoice rounding",
		"body": "Rounding lives in one file.", "anchors": anchors})["entry_id"].(string)

	// Confirming resolves the edit.
	a := newAgent(t, addr, "rt-4")
	postAPI(t, addr, "/api/edited", map[string]any{"runtime_session_id": "rt-4", "path": "src/billing/round.go"})
	txt := a.refused("session_end", map[string]any{}, "unconfirmed_edits")
	if !strings.Contains(txt, id+" (src/billing/round.go)") {
		t.Errorf("refusal does not list the entry and path: %q", txt)
	}
	a.readFull(id)
	a.ok("confirm", map[string]any{"entry_id": id})
	if end := a.ok("session_end", map[string]any{}); end["closed"] != true {
		t.Errorf("session_end after confirm = %v", end)
	}

	// Writing an update resolves it too, and an edit made after the write reopens it.
	b := newAgent(t, addr, "rt-5")
	postAPI(t, addr, "/api/edited", map[string]any{"runtime_session_id": "rt-5", "path": "src/billing/round.go"})
	b.refused("session_end", map[string]any{}, "unconfirmed_edits")
	b.ok("write", map[string]any{"entry_id": id, "body": "Rounding lives in one file, now with a cap."})
	postAPI(t, addr, "/api/edited", map[string]any{"runtime_session_id": "rt-5", "path": "src/billing/round.go"})
	b.refused("session_end", map[string]any{}, "unconfirmed_edits")
	b.ok("write", map[string]any{"entry_id": id, "body": "Rounding lives in one file, capped at ten cents."})
	if end := b.ok("session_end", map[string]any{}); end["closed"] != true {
		t.Errorf("session_end after write = %v", end)
	}

	// An entry the session creates after its edit is already current.
	c := newAgent(t, addr, "rt-6")
	postAPI(t, addr, "/api/edited", map[string]any{"runtime_session_id": "rt-6", "path": "src/tax/tax.go"})
	c.ok("write", map[string]any{"parent_sign_id": "root", "kind": "decision", "decided_by": "ana", "decided_at": "2026-10-27", "title": "Tax rules", "read_when": "you change tax",
		"body": "Tax is flat.", "anchors": []map[string]any{{"type": "file", "path": "src/tax/tax.go"}}})
	if end := c.ok("session_end", map[string]any{}); end["closed"] != true {
		t.Errorf("session_end after create = %v", end)
	}
}

func TestStopCheckCarriesEdits(t *testing.T) {
	_, addr, repo := startRepoServer(t)
	touch(t, repo, "src/billing/round.go")
	a := newAgent(t, addr, "rt-7")
	id := a.ok("write", map[string]any{"parent_sign_id": "root", "kind": "decision", "decided_by": "ana", "decided_at": "2026-10-27", "title": "Billing rounding", "read_when": "you change invoice rounding",
		"body": "Rounding lives in one file.", "anchors": []map[string]any{{"type": "file", "path": "src/billing/round.go"}}})["entry_id"].(string)
	// The writer's own create is current, so the edit comes after it.
	postAPI(t, addr, "/api/edited", map[string]any{"runtime_session_id": "rt-7", "path": "src/billing/round.go"})

	got := postAPI(t, addr, "/api/stop-check", map[string]any{"runtime_session_id": "rt-7"})
	edits, _ := got["edits"].([]any)
	if got["open"] != true || got["blocks"] != float64(1) || len(edits) != 1 {
		t.Fatalf("first check: %v", got)
	}
	if e := edits[0].(map[string]any); e["id"] != id || e["path"] != "src/billing/round.go" {
		t.Errorf("edit = %v", e)
	}
	postAPI(t, addr, "/api/stop-check", map[string]any{"runtime_session_id": "rt-7"})
	got = postAPI(t, addr, "/api/stop-check", map[string]any{"runtime_session_id": "rt-7"})
	if got["open"] != false || got["forced"] != true {
		t.Errorf("third check: %v", got)
	}
}

func offTrailRows(t *testing.T, st *store.Store, sid, result string) int {
	t.Helper()
	var n int
	st.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE tool = 'off_trail' AND session_id = ? AND result = ?`, sid, result).Scan(&n)
	return n
}

func TestPreReadEndpoint(t *testing.T) {
	st, addr, repo := startRepoServer(t)
	if err := os.MkdirAll(filepath.Join(repo, "src", "billing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "billing", "round.go"), []byte("package billing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newAgent(t, addr, "rt-pre")
	res, err := a.raw("write", map[string]any{"parent_sign_id": "root", "kind": "decision", "decided_by": "ana", "decided_at": "2026-10-27", "title": "Billing rounding", "read_when": "you change invoice rounding",
		"body": "Rounding lives in one file.", "anchors": []map[string]any{{"type": "file", "path": "src/billing/round.go"}}})
	if err != nil || res.IsError {
		t.Fatalf("write: %v %v", err, res.Content[0])
	}
	var id string
	if err := st.DB.QueryRow(`SELECT entry_id FROM anchors WHERE path_or_url = 'src/billing/round.go'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	pre := func(runtime, p string) map[string]any {
		return postAPI(t, addr, "/api/pre-read", map[string]any{"runtime_session_id": runtime, "path": p})
	}

	// The agent wrote the entry but has not read it: the read is turned back, once however many attempts.
	for _, p := range []string{filepath.Join(repo, "src/billing/round.go"), "src/billing/round.go", "src/billing/round.go"} {
		got := pre("rt-pre", p)
		if got["decision"] != "turned_back" || got["entry_id"] != id || got["read_when"] != "you change invoice rounding" {
			t.Errorf("unread %s = %v", p, got)
		}
	}
	if n := offTrailRows(t, st, a.sid, "turned_back"); n != 1 {
		t.Errorf("turned_back recorded %d times, want 1", n)
	}
	var target, shown string
	st.DB.QueryRow(`SELECT target, shown_json FROM events WHERE tool = 'off_trail' AND result = 'turned_back' AND session_id = ?`, a.sid).Scan(&target, &shown)
	if target != "src/billing/round.go" || shown != `["`+id+`"]` {
		t.Errorf("turned_back row = %q %q", target, shown)
	}

	// A summary read counts: after it the same path is checked, and each call records a row.
	a.ok("read", map[string]any{"entry_id": id, "depth": "summary"})
	for i := 0; i < 2; i++ {
		if got := pre("rt-pre", "src/billing/round.go"); got["decision"] != "checked" || got["entry_id"] != id {
			t.Errorf("after read = %v", got)
		}
	}
	if n := offTrailRows(t, st, a.sid, "checked"); n != 2 {
		t.Errorf("checked recorded %d times, want 2", n)
	}

	// A new session has not read it, so it is turned back again and records its own row.
	b := newAgent(t, addr, "rt-pre-b")
	if got := pre("rt-pre-b", "src/billing/round.go"); got["decision"] != "turned_back" {
		t.Errorf("second session = %v", got)
	}
	if n := offTrailRows(t, st, b.sid, "turned_back"); n != 1 {
		t.Errorf("second session recorded %d turn-backs", n)
	}

	// A plain path is unmapped and has no entry fields.
	got := pre("rt-pre", "docs/plain.md")
	if got["decision"] != "unmapped" || got["entry_id"] != nil {
		t.Errorf("plain path = %v", got)
	}
	if n := offTrailRows(t, st, a.sid, "unmapped"); n != 1 {
		t.Errorf("unmapped recorded %d times", n)
	}

	// No session matched: unmapped, nothing recorded.
	var before, after int
	st.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE tool = 'off_trail'`).Scan(&before)
	if got := pre("nope", "src/billing/round.go"); got["decision"] != "unmapped" || got["entry_id"] != nil {
		t.Errorf("no session = %v", got)
	}
	st.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE tool = 'off_trail'`).Scan(&after)
	if before != after {
		t.Errorf("no session recorded %d rows", after-before)
	}
}

func TestPreReadWithoutSession(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "src", "billing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "billing", "round.go"), []byte("package billing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, addr := serveRepos(t, repo)
	pid := reg.List()[0].ID
	st := reg.List()[0].Store
	a := newAgent(t, addr, "rt-walk")
	res, err := a.raw("write", map[string]any{"parent_sign_id": "root", "kind": "decision", "decided_by": "ana", "decided_at": "2026-10-27", "title": "Billing rounding", "read_when": "you change invoice rounding",
		"body": "Rounding lives in one file.", "anchors": []map[string]any{{"type": "file", "path": "src/billing/round.go"}}})
	if err != nil || res.IsError {
		t.Fatalf("write: %v %v", err, res.Content[0])
	}
	var id string
	if err := st.DB.QueryRow(`SELECT entry_id FROM anchors WHERE path_or_url = 'src/billing/round.go'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	pre := func(runtime, project, p string) map[string]any {
		body := map[string]any{"runtime_session_id": runtime, "path": p}
		if project != "" {
			body["project"] = project
		}
		return postAPI(t, addr, "/api/pre-read", body)
	}
	var before int
	st.DB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&before)

	// No session matches the runtime id, but the project's entry anchors the path: unstarted, with the entry, and nothing recorded.
	got := pre("rt-fresh", pid, "src/billing/round.go")
	if got["decision"] != "unstarted" || got["entry_id"] != id || got["read_when"] != "you change invoice rounding" {
		t.Errorf("unstarted = %v", got)
	}
	// A plain path in the same project is unmapped.
	if got := pre("rt-fresh", pid, "docs/plain.md"); got["decision"] != "unmapped" || got["entry_id"] != nil {
		t.Errorf("plain path with no session = %v", got)
	}
	// Without a project in the body the registry cannot tell which repo the path belongs to: unmapped.
	if got := pre("rt-fresh", "", "src/billing/round.go"); got["decision"] != "unmapped" || got["entry_id"] != nil {
		t.Errorf("no project = %v", got)
	}
	var after int
	st.DB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&after)
	if before != after {
		t.Errorf("no-session reads recorded %d events", after-before)
	}
}

func TestPreReadListsEveryEntry(t *testing.T) {
	st, addr, repo := startRepoServer(t)
	if err := os.MkdirAll(filepath.Join(repo, "src", "billing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "billing", "round.go"), []byte("package billing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newAgent(t, addr, "rt-list")
	for _, title := range []string{"Billing rounding", "Billing tax", "Billing audit"} {
		res, err := a.raw("write", map[string]any{"parent_sign_id": "root", "kind": "decision", "decided_by": "ana", "decided_at": "2026-10-27", "title": title, "read_when": "you change " + title,
			"body": "Billing is one file.", "anchors": []map[string]any{{"type": "file", "path": "src/billing/round.go"}}})
		if err != nil || res.IsError {
			t.Fatalf("write %s: %v %v", title, err, res.Content[0])
		}
	}
	rows, err := st.DB.Query(`SELECT entry_id FROM anchors WHERE path_or_url = 'src/billing/round.go' ORDER BY entry_id`)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		want = append(want, id)
	}
	rows.Close()
	if len(want) != 3 {
		t.Fatalf("anchored entries = %v, want 3", want)
	}
	pre := func() map[string]any {
		return postAPI(t, addr, "/api/pre-read", map[string]any{"runtime_session_id": "rt-list", "path": "src/billing/round.go"})
	}
	// signpostIDs returns the entry ids a reply lists, in order.
	signpostIDs := func(got map[string]any) []string {
		var ids []string
		list, _ := got["entries"].([]any)
		for _, e := range list {
			m, _ := e.(map[string]any)
			id, _ := m["entry_id"].(string)
			ids = append(ids, id)
		}
		return ids
	}

	// Unread: turned back, the reply lists all three by id, and entry_id and read_when repeat the first.
	got := pre()
	if got["decision"] != "turned_back" || got["total"] != float64(3) || got["entry_id"] != want[0] {
		t.Fatalf("unread = %v", got)
	}
	if ids := signpostIDs(got); strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("listed %v, want %v", ids, want)
	}
	// Repeated attempts on the same path record one turned_back.
	pre()
	if n := offTrailRows(t, st, a.sid, "turned_back"); n != 1 {
		t.Errorf("repeated turned_back recorded %d times, want 1", n)
	}

	// A read of any one of them checks the path; the reply still lists all three.
	a.ok("read", map[string]any{"entry_id": want[2], "depth": "summary"})
	got = pre()
	if got["decision"] != "checked" || got["total"] != float64(3) || len(signpostIDs(got)) != 3 || got["entry_id"] != want[0] {
		t.Errorf("after reading one = %v", got)
	}
}
