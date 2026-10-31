package web_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// formOf turns the GET response into the body the form posts back, unchanged.
func formOf(t *testing.T, page map[string]any) map[string]any {
	t.Helper()
	e := page["entry"].(map[string]any)
	return map[string]any{
		"etag": page["etag"], "title": e["title"], "read_when": e["read_when"], "body_markdown": e["body_markdown"], "tags": e["tags"],
		"anchors": page["form_anchors"], "requires": page["requires"], "see_also": page["see_also"],
		"decided_by": page["decided_by"], "decided_at": page["decided_at"],
	}
}

func (f *fixture) col1(q string, args ...any) string {
	f.t.Helper()
	var s *string
	if err := f.st.DB.QueryRow(q, args...).Scan(&s); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	if s == nil {
		return ""
	}
	return *s
}

func TestEntryResponseCarriesTheEditForm(t *testing.T) {
	f := seed(t)
	_, page := f.get("/api/entries/round-helper")
	e := page["entry"].(map[string]any)
	if page["etag"] != e["updated_at"] || page["etag"] == "" {
		t.Errorf("etag = %v, updated_at = %v", page["etag"], e["updated_at"])
	}
	anchors := page["form_anchors"].([]any)
	if len(anchors) != 1 {
		t.Fatalf("form_anchors = %v", anchors)
	}
	a := anchors[0].(map[string]any)
	if a["type"] != "range" || a["path"] != "src/round.go" || a["start"] != float64(3) || a["end"] != float64(6) {
		t.Errorf("form anchor = %v", a)
	}
	if got := page["requires"].([]any); len(got) != 1 || got[0] != "invoices-round-to-cents" {
		t.Errorf("requires = %v", got)
	}
	if got := page["see_also"].([]any); len(got) != 0 {
		t.Errorf("see_also = %v", got)
	}
	if got := page["staged_copies"].([]any); len(got) != 0 {
		t.Errorf("staged_copies = %v", got)
	}
	_, d := f.get("/api/entries/invoices-round-to-cents")
	if d["decided_by"] != "ana" || d["decided_at"] != "2026-10-27" {
		t.Errorf("decision fields = %v %v", d["decided_by"], d["decided_at"])
	}
}

func TestEditFromTheBrowserKeepsTheTier(t *testing.T) {
	f := seed(t)
	for _, tier := range []string{"provisional", "verified"} {
		if _, err := f.st.DB.Exec(`UPDATE entries SET tier = ? WHERE id = 'round-helper'`, tier); err != nil {
			t.Fatal(err)
		}
		_, page := f.get("/api/entries/round-helper")
		form := formOf(t, page)
		form["body_markdown"] = "Call `Round` before storing a total, and never after (" + tier + ")."
		code, body := f.post("/api/entries/round-helper", form)
		if code != 200 {
			t.Fatalf("%s edit: %d %s", tier, code, body)
		}
		var out map[string]any
		json.Unmarshal([]byte(body), &out)
		saved := out["entry"].(map[string]any)
		if saved["tier"] != tier || !strings.Contains(saved["body_markdown"].(string), "("+tier+")") {
			t.Errorf("%s: saved entry = tier %v body %v", tier, saved["tier"], saved["body_markdown"])
		}
		if out["etag"] == page["etag"] || out["etag"] != saved["updated_at"] {
			t.Errorf("%s: etag %v after %v", tier, out["etag"], page["etag"])
		}
		if got := f.col1(`SELECT tier FROM entries WHERE id = 'round-helper'`); got != tier {
			t.Errorf("stored tier = %q, want %q", got, tier)
		}
		if got := f.col1(`SELECT result FROM events WHERE session_id = 'user' AND tool = 'edit' AND target = 'round-helper' ORDER BY id DESC LIMIT 1`); got != "ok" {
			t.Errorf("%s: edit event = %q", tier, got)
		}
	}
	if got := f.col1(`SELECT actor FROM versions WHERE entry_id = 'round-helper' ORDER BY id DESC LIMIT 1`); got != "user" {
		t.Errorf("newest version actor = %q", got)
	}
}

func TestEditWithAnOldEtagIs409WithTheCurrentEntry(t *testing.T) {
	f := seed(t)
	_, page := f.get("/api/entries/round-helper")
	mine, theirs := formOf(t, page), formOf(t, page)
	theirs["body_markdown"] = "Call `Round` before storing, written by the other tab."
	if code, body := f.post("/api/entries/round-helper", theirs); code != 200 {
		t.Fatalf("first save: %d %s", code, body)
	}
	mine["body_markdown"] = "Call `Round` before storing, written by the stale tab."
	code, body := f.post("/api/entries/round-helper", mine)
	if code != 409 {
		t.Fatalf("stale save: %d %s", code, body)
	}
	var cur map[string]any
	if err := json.Unmarshal([]byte(body), &cur); err != nil {
		t.Fatalf("409 body is not JSON: %q", body)
	}
	if got := cur["entry"].(map[string]any)["body_markdown"]; strings.TrimSpace(got.(string)) != theirs["body_markdown"] {
		t.Errorf("409 carries body %v, want the other tab's", got)
	}
	if cur["etag"] == page["etag"] || cur["etag"] == nil {
		t.Errorf("409 etag = %v, want the current one", cur["etag"])
	}
	if r, _ := cur["refusal"].(string); !strings.HasPrefix(r, "stale_edit: ") {
		t.Errorf("refusal = %q", r)
	}
	if got := f.col1(`SELECT result FROM events WHERE session_id = 'user' AND tool = 'edit' ORDER BY id DESC LIMIT 1`); got != "refused:stale_edit" {
		t.Errorf("event = %q", got)
	}
}

func TestEditRefusalIs400RuleColonFix(t *testing.T) {
	f := seed(t)
	_, page := f.get("/api/entries/round-helper")
	form := formOf(t, page)
	form["title"] = strings.Repeat("long title ", 10)
	code, body := f.post("/api/entries/round-helper", form)
	rule, fix, ok := strings.Cut(strings.TrimSpace(body), ": ")
	if code != 400 || !ok || rule == "" || fix == "" || strings.ContainsAny(rule, " \n") {
		t.Fatalf("refusal = %d %q", code, body)
	}
	if got := f.col1(`SELECT result FROM events WHERE session_id = 'user' AND tool = 'edit' ORDER BY id DESC LIMIT 1`); got != "refused:"+rule {
		t.Errorf("event = %q, want refused:%s", got, rule)
	}
	if code, _ := f.post("/api/entries/nope", map[string]any{"etag": "x", "title": "t"}); code != 404 {
		t.Errorf("unknown entry = %d, want 404", code)
	}
	if code, _ := f.post("/api/entries/round-helper", map[string]any{"title": "no etag"}); code != 400 {
		t.Errorf("save without etag = %d, want 400", code)
	}
}

func TestEditOfAStagedEntryIs400StagedElsewhere(t *testing.T) {
	f := stagedSeed(t)
	_, page := f.get("/api/entries/" + f.onlyX)
	code, body := f.post("/api/entries/"+f.onlyX, formOf(t, page))
	if code != 400 || !strings.HasPrefix(body, "staged_elsewhere: ") || !strings.Contains(body, "feat/x") {
		t.Errorf("staged edit = %d %q", code, body)
	}
	_, orig := f.get("/api/entries/" + f.orig)
	copies := orig["staged_copies"].([]any)
	if len(copies) != 1 || copies[0].(map[string]any)["id"] != f.copyX || copies[0].(map[string]any)["branch"] != "feat/x" {
		t.Errorf("original's staged_copies = %v", copies)
	}
	if code, body := f.post("/api/entries/"+f.orig, formOf(t, orig)); code != 200 {
		t.Errorf("edit of an original with a staged copy = %d %s", code, body)
	}
}

func TestConfirmFromTheBrowser(t *testing.T) {
	f := seed(t)
	if err := os.Remove(filepath.Join(f.st.Repo, "src", "round.go")); err != nil {
		t.Fatal(err)
	}
	code, body := f.post("/api/entries/round-helper/confirm", map[string]any{})
	if code != 400 || !strings.HasPrefix(body, "stale: ") || !strings.Contains(body, "src/round.go") {
		t.Errorf("confirm of a stale entry = %d %q", code, body)
	}
	if got := f.col1(`SELECT tier FROM entries WHERE id = 'round-helper'`); got == "verified" {
		t.Error("a refused confirm verified the entry")
	}
	if got := f.col1(`SELECT result FROM events WHERE session_id = 'user' AND tool = 'confirm' AND target = 'round-helper'`); got != "refused:stale" {
		t.Errorf("refusal event = %q", got)
	}
	if code, _ := f.post("/api/entries/nope/confirm", map[string]any{}); code != 404 {
		t.Errorf("confirm of an unknown entry = %d, want 404", code)
	}

	code, body = f.post("/api/entries/invoices-round-to-cents/confirm", map[string]any{})
	if code != 200 {
		t.Fatalf("confirm: %d %s", code, body)
	}
	var out map[string]any
	json.Unmarshal([]byte(body), &out)
	if out["entry"].(map[string]any)["tier"] != "verified" || out["etag"] == nil {
		t.Errorf("confirm response = %v", out)
	}
	if got := f.col1(`SELECT COUNT(*) FROM events WHERE session_id = 'user' AND tool = 'confirm' AND target = 'invoices-round-to-cents' AND result = 'ok'`); got != "1" {
		t.Errorf("confirm events = %s, want 1", got)
	}
}

func TestEditRouteRefusesAForeignOrigin(t *testing.T) {
	f := seed(t)
	_, page := f.get("/api/entries/round-helper")
	form := formOf(t, page)
	form["body_markdown"] = "written by another site"
	b, _ := json.Marshal(form)
	for route, want := range map[string]int{"/api/entries/round-helper": 403, "/api/entries/round-helper/confirm": 403} {
		req, _ := http.NewRequest("POST", f.url(route), strings.NewReader(string(b)))
		req.Header.Set("Origin", "http://evil.example")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s with a foreign Origin = %d, want %d", route, resp.StatusCode, want)
		}
	}
	if got := f.col1(`SELECT COUNT(*) FROM events WHERE session_id = 'user'`); got != "0" {
		t.Errorf("a refused request logged %s user events", got)
	}
	_, after := f.get("/api/entries/round-helper")
	if after["etag"] != page["etag"] {
		t.Error("the entry changed")
	}
}
