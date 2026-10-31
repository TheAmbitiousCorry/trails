package mcp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

func list(t *testing.T, m map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%s is not a list in %v", key, m)
	}
	out := make([]map[string]any, len(raw))
	for i, r := range raw {
		out[i] = r.(map[string]any)
	}
	return out
}

func refused(t *testing.T, call caller, args map[string]any) string {
	t.Helper()
	res, err := call("coverage", args)
	return refusal(t, res, err)
}

func TestCoverageGapsStaleAndOffTrail(t *testing.T) {
	st, addr := startServer(t)
	call := connect(t, addr)
	start := func(task, rt string) string {
		res, err := call("session_start", map[string]any{"task": task, "runtime_session_id": rt})
		return structured(t, res, err)["session_id"].(string)
	}
	a, b, c := start("a", "rt-a"), start("b", "rt-b"), start("c", "rt-c")

	// Two signs, api under root and api-http under api, and a live entry under each anchoring a file.
	for _, s := range [][2]string{{"api", "root"}, {"api-http", "api"}} {
		res, err := call("write_sign", map[string]any{"session_id": a, "parent_sign_id": s[1], "title": s[0]})
		structured(t, res, err)
	}
	now := store.Now()
	entry := func(id, sign, tier string) {
		if _, err := st.DB.Exec(`INSERT INTO entries (id, sign_id, kind, title, read_when, body_json, body_schema, tier, created_at, updated_at) VALUES (?, ?, 'summary', ?, 'when', '{"type":"doc","content":[]}', 1, ?, ?, ?)`, id, sign, "Title "+id, tier, now, now); err != nil {
			t.Fatal(err)
		}
	}
	anchorOn := func(id, typ, path string, ok int) {
		if _, err := st.DB.Exec(`INSERT INTO anchors (entry_id, type, path_or_url, ok) VALUES (?, ?, ?, ?)`, id, typ, path, ok); err != nil {
			t.Fatal(err)
		}
	}
	entry("anch", "api-http", "provisional")
	anchorOn("anch", "file", "src/http/server.go", 1)
	entry("old", "api", "stale")
	anchorOn("old", "file", "src/old.go", 0)
	anchorOn("old", "file", "src/kept.go", 1)

	// Gaps from two sessions: one at a sign, one with no sign recorded.
	st.DB.Exec(`INSERT INTO gaps (sign_id, session_id, query, opened_at) VALUES ('api-http', ?, 'how do retries work', ?)`, a, now)
	st.DB.Exec(`INSERT INTO gaps (sign_id, session_id, query, opened_at) VALUES (NULL, ?, 'where is billing', ?)`, b, now)
	st.DB.Exec(`INSERT INTO gaps (sign_id, session_id, query, state, opened_at) VALUES ('api', ?, 'closed one', 'resolved', ?)`, b, now)

	// Off-trail reads: an unanchored path beside a covered one, an unanchored path with no covered ancestor, an anchored path,
	// a path outside any checkout, and an old read outside the window.
	for _, p := range []string{"src/http/retry.go", "docs/notes.md", "src/http/server.go", "/etc/hosts"} {
		post(t, addr, "/api/off-trail", map[string]any{"runtime_session_id": "rt-a", "path": p})
	}
	post(t, addr, "/api/off-trail", map[string]any{"runtime_session_id": "rt-b", "path": "src/http/retry.go"})
	longAgo := time.Now().UTC().AddDate(0, 0, -200).Format(time.RFC3339Nano)
	st.DB.Exec(`INSERT INTO events (session_id, at, tool, target, result) VALUES (?, ?, 'off_trail', 'src/ancient.go', 'ok')`, a, longAgo)

	res, err := call("coverage", map[string]any{"session_id": c})
	out := structured(t, res, err)

	gaps := list(t, out, "gaps")
	if len(gaps) != 2 {
		t.Fatalf("gaps = %v", gaps)
	}
	if gaps[0]["query"] != "how do retries work" || gaps[0]["sign"] != "api-http" || gaps[1]["query"] != "where is billing" || gaps[1]["sign"] != "root" {
		t.Errorf("gaps = %v", gaps)
	}
	if _, ok := gaps[0]["id"].(float64); !ok || gaps[0]["age_days"] == nil {
		t.Errorf("gap lacks id or age_days: %v", gaps[0])
	}

	stale := list(t, out, "stale")
	if len(stale) != 1 || stale[0]["id"] != "old" || stale[0]["title"] != "Title old" || stale[0]["sign"] != "api" {
		t.Fatalf("stale = %v", stale)
	}
	failed, _ := stale[0]["failed_anchors"].([]any)
	if len(failed) != 1 || failed[0] != "file src/old.go" {
		t.Errorf("failed anchors = %v", failed)
	}
	if p, _ := stale[0]["sign_path"].([]any); len(p) != 2 || p[1] != "api" {
		t.Errorf("sign_path = %v", stale[0]["sign_path"])
	}

	off := list(t, out, "off_trail")
	got := map[string]map[string]any{}
	for _, o := range off {
		got[o["path"].(string)] = o
	}
	if len(off) != 2 {
		t.Fatalf("off_trail = %v, want src/http/retry.go and docs/notes.md only", off)
	}
	if r := got["src/http/retry.go"]; r == nil || r["count"] != 2.0 || r["sign"] != "api-http" {
		t.Errorf("retry.go = %v", r)
	}
	if n := got["docs/notes.md"]; n == nil || n["count"] != 1.0 || n["sign"] != "root" {
		t.Errorf("notes.md = %v", n)
	}
	if off[0]["path"] != "src/http/retry.go" {
		t.Errorf("off_trail is not ordered by count: %v", off)
	}

	if _, has := out["unmapped"]; has {
		t.Error("unmapped is not built yet and must not be in the output")
	}
	if out["lines"] == nil {
		t.Error("response lacks lines")
	}
}

func TestCoverageLimitWithinAndRefusals(t *testing.T) {
	st, addr := startServer(t)
	call := connect(t, addr)
	res, err := call("session_start", map[string]any{"task": "a"})
	sid := structured(t, res, err)["session_id"].(string)
	res, err = call("write_sign", map[string]any{"session_id": sid, "parent_sign_id": "root", "title": "api"})
	structured(t, res, err)
	now := store.Now()
	st.DB.Exec(`INSERT INTO gaps (sign_id, session_id, query, opened_at) VALUES ('api', ?, 'one', ?), ('root', ?, 'two', ?), ('root', ?, 'three', ?)`, sid, now, sid, now, sid, now)

	res, err = call("coverage", map[string]any{"session_id": sid, "limit": 2})
	if g := list(t, structured(t, res, err), "gaps"); len(g) != 2 {
		t.Errorf("limit 2 returned %d gaps", len(g))
	}
	res, err = call("coverage", map[string]any{"session_id": sid})
	if g := list(t, structured(t, res, err), "gaps"); len(g) != 3 {
		t.Errorf("default limit returned %d gaps", len(g))
	}
	// within keeps the items whose sign lies in that sign's subtree.
	res, err = call("coverage", map[string]any{"session_id": sid, "within": "api"})
	if g := list(t, structured(t, res, err), "gaps"); len(g) != 1 || g[0]["query"] != "one" {
		t.Errorf("within api = %v", g)
	}
	if msg := refused(t, call, map[string]any{"session_id": sid, "within": "nowhere"}); !strings.HasPrefix(msg, "unknown_sign") {
		t.Errorf("unknown within = %q", msg)
	}
	if msg := refused(t, call, map[string]any{"session_id": "s-nope"}); !strings.HasPrefix(msg, "unknown_session") {
		t.Errorf("unknown session = %q", msg)
	}
}

func TestCoverageLimitIsCappedByTheSetting(t *testing.T) {
	st, addr := startServer(t)
	call := connect(t, addr)
	res, err := call("session_start", map[string]any{"task": "a"})
	sid := structured(t, res, err)["session_id"].(string)
	now := store.Now()
	for i := 0; i < 30; i++ {
		st.DB.Exec(`INSERT INTO gaps (sign_id, session_id, query, opened_at) VALUES ('root', ?, ?, ?)`, sid, "q"+string(rune('a'+i)), now)
	}
	for _, c := range []struct {
		in   any
		want int
	}{{nil, 10}, {99, 25}, {0, 10}} {
		args := map[string]any{"session_id": sid}
		if c.in != nil {
			args["limit"] = c.in
		}
		res, err := call("coverage", args)
		if g := list(t, structured(t, res, err), "gaps"); len(g) != c.want {
			t.Errorf("limit %v returned %d gaps, want %d", c.in, len(g), c.want)
		}
	}
}

func TestCoverageSettingsAreSeeded(t *testing.T) {
	st, _ := startServer(t)
	for key, want := range map[string]string{"coverage_limit": "10", "coverage_limit_max": "25", "coverage_window_days": "90", "coverage_dir_max": "50"} {
		var got string
		if err := st.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&got); err != nil || got != want {
			t.Errorf("%s = %q (%v), want %q", key, got, err, want)
		}
	}
}
