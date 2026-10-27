package entries_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/entries"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

func open(t *testing.T) (*store.Store, anchor.Checker) {
	t.Helper()
	st, err := store.Open(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, anchor.Checker{Repo: st.Repo}
}

func decision(title, body string) entries.WriteRequest {
	return entries.WriteRequest{Actor: "s-1", ParentSignID: "root", Kind: "decision", Title: title, ReadWhen: "you need " + title,
		BodyMarkdown: body, DecidedBy: "ana", DecidedAt: "2026-10-27"}
}

func mustWrite(t *testing.T, st *store.Store, c anchor.Checker, req entries.WriteRequest) entries.WriteResult {
	t.Helper()
	res, err := entries.Write(context.Background(), st.DB, c, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Refusal != nil {
		t.Fatalf("write refused: %v", res.Refusal)
	}
	return res
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRuleNames(t *testing.T) {
	st, c := open(t)
	if err := os.WriteFile(filepath.Join(st.Repo, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st.DB.Exec(`INSERT INTO settings (key, value) VALUES ('cap_decision_words', '5')`)
	st.DB.Exec(`INSERT INTO signs (id, parent_id, title) VALUES ('fork', 'root', 'Fork')`)
	for i := 1; i <= 7; i++ {
		if _, err := st.DB.Exec(`INSERT INTO signs (id, parent_id, title) VALUES (?, 'fork', 'c')`, fmt.Sprintf("c%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	good := "A short body."
	cases := []struct {
		name string
		edit func(r *entries.WriteRequest)
		rule string
	}{
		{"schema", func(r *entries.WriteRequest) { r.BodyMarkdown = "<div>x</div>" }, "unsupported: html block"},
		{"schema title", func(r *entries.WriteRequest) { r.Title = strings.Repeat("t", 81) }, "schema:title"},
		{"cap", func(r *entries.WriteRequest) { r.BodyMarkdown = "one two three four five six" }, "split this"},
		{"anchor missing", func(r *entries.WriteRequest) { r.Kind = "summary"; r.Anchors = nil }, "anchor"},
		{"anchor decided", func(r *entries.WriteRequest) { r.DecidedBy = "" }, "anchor"},
		{"anchor unresolved", func(r *entries.WriteRequest) {
			r.Kind = "summary"
			r.Anchors = []anchor.Anchor{{Type: "file", PathOrURL: "gone.txt"}}
		}, "anchor"},
		{"anchor range count", func(r *entries.WriteRequest) {
			r.Kind = "example"
			r.Anchors = []anchor.Anchor{{Type: "file", PathOrURL: "a.txt"}}
		}, "anchor"},
		{"fan-out", func(r *entries.WriteRequest) { r.ParentSignID = "fork" }, "split this fork"},
		{"unreachable unknown", func(r *entries.WriteRequest) { r.ParentSignID = "nowhere" }, "unreachable"},
		{"unreachable empty", func(r *entries.WriteRequest) { r.ParentSignID = "" }, "unreachable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := decision("Rule probe", good)
			tc.edit(&req)
			res, err := entries.Write(context.Background(), st.DB, c, req)
			if err != nil {
				t.Fatal(err)
			}
			if res.Refusal == nil || res.Refusal.Rule != tc.rule {
				t.Fatalf("refusal = %+v, want rule %q", res.Refusal, tc.rule)
			}
			if res.Refusal.Fix == "" {
				t.Error("refusal carries no fix")
			}
		})
	}
	if n := count(t, st.DB, `SELECT COUNT(*) FROM entries`); n != 1 {
		t.Errorf("a refused write stored an entry: %d entries", n)
	}
}

func TestAcceptedWriteTiersAndIDs(t *testing.T) {
	st, c := open(t)
	a := mustWrite(t, st, c, decision("Use SQLite", "Chosen for one file per project."))
	if a.EntryID != "use-sqlite" || a.Tier != "provisional" {
		t.Errorf("agent write = %+v", a)
	}
	b := mustWrite(t, st, c, decision("Use SQLite", "A second one."))
	if b.EntryID != "use-sqlite-2" {
		t.Errorf("collision id = %q", b.EntryID)
	}
	req := decision("Human note", "Written by a person.")
	req.Actor = "user"
	if h := mustWrite(t, st, c, req); h.Tier != "verified" {
		t.Errorf("human write tier = %q", h.Tier)
	}
}

func TestAgentWriteToVerifiedOffersConflict(t *testing.T) {
	st, c := open(t)
	req := decision("Pick Go", "Go for the server.")
	req.Actor = "user"
	v := mustWrite(t, st, c, req)

	upd := decision("Pick Go", "Rust for the server.")
	upd.EntryID = v.EntryID
	res, err := entries.Write(context.Background(), st.DB, c, upd)
	if err != nil {
		t.Fatal(err)
	}
	if res.Refusal == nil || res.Refusal.Rule != "verified" {
		t.Fatalf("refusal = %+v", res.Refusal)
	}
	if res.Conflict == nil || res.Conflict.VerifiedID != v.EntryID || res.Conflict.NewID == "" || res.Conflict.NewID == v.EntryID {
		t.Fatalf("conflict = %+v", res.Conflict)
	}
	var tier, body string
	st.DB.QueryRow(`SELECT tier, body_json FROM entries WHERE id = ?`, res.Conflict.NewID).Scan(&tier, &body)
	if tier != "provisional" || !strings.Contains(body, "Rust") {
		t.Errorf("new entry = %s %s", tier, body)
	}
	st.DB.QueryRow(`SELECT tier, body_json FROM entries WHERE id = ?`, v.EntryID).Scan(&tier, &body)
	if tier != "verified" || strings.Contains(body, "Rust") {
		t.Errorf("verified entry changed: %s %s", tier, body)
	}
	if n := count(t, st.DB, `SELECT COUNT(*) FROM links WHERE relation = 'conflicts_with' AND ((from_id = ?1 AND to_id = ?2) OR (from_id = ?2 AND to_id = ?1))`, v.EntryID, res.Conflict.NewID); n != 1 {
		t.Errorf("conflicts_with links = %d", n)
	}
}

func TestVersionPerAcceptedWrite(t *testing.T) {
	st, c := open(t)
	res := mustWrite(t, st, c, decision("Versioned", "First body."))
	versions := func() int { return count(t, st.DB, `SELECT COUNT(*) FROM versions WHERE entry_id = ?`, res.EntryID) }
	if versions() != 1 {
		t.Fatalf("versions after create = %d", versions())
	}
	same := decision("Versioned", "First body.")
	same.EntryID = res.EntryID
	mustWrite(t, st, c, same)
	if versions() != 1 {
		t.Errorf("identical rewrite added a version: %d", versions())
	}
	changed := decision("Versioned", "Second body.")
	changed.EntryID = res.EntryID
	mustWrite(t, st, c, changed)
	if versions() != 2 {
		t.Errorf("changed rewrite versions = %d", versions())
	}
	var actor string
	st.DB.QueryRow(`SELECT actor FROM versions WHERE entry_id = ? ORDER BY id DESC LIMIT 1`, res.EntryID).Scan(&actor)
	if actor != "s-1" {
		t.Errorf("actor = %q", actor)
	}
}

func TestLinksAndChain(t *testing.T) {
	st, c := open(t)
	a := mustWrite(t, st, c, decision("A", "a"))
	b := decision("B", "b")
	b.Requires = []string{a.EntryID}
	bid := mustWrite(t, st, c, b).EntryID
	z := decision("Z", "z")
	z.Requires = []string{a.EntryID, bid}
	zid := mustWrite(t, st, c, z).EntryID

	chain, err := entries.Chain(context.Background(), st.DB, zid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(chain, ",") != "a,b" {
		t.Errorf("chain = %v", chain)
	}

	// A cycle is tolerated: A now requires Z.
	upd := decision("A", "a")
	upd.EntryID = a.EntryID
	upd.Requires = []string{zid}
	mustWrite(t, st, c, upd)
	if chain, err = entries.Chain(context.Background(), st.DB, zid); err != nil || len(chain) != 2 {
		t.Errorf("cyclic chain = %v, %v", chain, err)
	}

	bad := decision("Bad link", "x")
	bad.SeeAlso = []string{"missing"}
	if res, _ := entries.Write(context.Background(), st.DB, c, bad); res.Refusal == nil || res.Refusal.Rule != "unknown_entry" {
		t.Errorf("link to a missing entry: %+v", res.Refusal)
	}
}

func TestUpdateKeepsOmittedFields(t *testing.T) {
	st, c := open(t)
	if err := os.WriteFile(filepath.Join(st.Repo, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := mustWrite(t, st, c, decision("Other", "Other body.")).EntryID
	first := decision("Kept", "Kept body.")
	first.Tags = []string{"billing", "deploy"}
	first.Anchors = []anchor.Anchor{{Type: "file", PathOrURL: "a.txt"}}
	id := mustWrite(t, st, c, first).EntryID

	snapshot := func() string {
		var title, readWhen, body, tags, by, at string
		if err := st.DB.QueryRow(`SELECT title, read_when, body_json, tags_json, decided_by, decided_at FROM entries WHERE id = ?`, id).
			Scan(&title, &readWhen, &body, &tags, &by, &at); err != nil {
			t.Fatal(err)
		}
		var anchors string
		rows, err := st.DB.Query(`SELECT type, path_or_url, hash FROM anchors WHERE entry_id = ? ORDER BY id`, id)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var a, p, h string
			rows.Scan(&a, &p, &h)
			anchors += a + "|" + p + "|" + h + ";"
		}
		return strings.Join([]string{title, readWhen, body, tags, by, at, anchors}, "\n")
	}
	before := snapshot()

	res, err := entries.Write(context.Background(), st.DB, c, entries.WriteRequest{Actor: "s-2", EntryID: id, Requires: []string{other}, SeeAlso: []string{other}})
	if err != nil || res.Refusal != nil {
		t.Fatalf("link-only update: %v %v", err, res.Refusal)
	}
	if after := snapshot(); after != before {
		t.Errorf("update changed omitted fields:\nbefore %q\nafter  %q", before, after)
	}
	if n := count(t, st.DB, `SELECT COUNT(*) FROM links WHERE from_id = ? AND to_id = ? AND relation = 'requires'`, id, other); n != 1 {
		t.Errorf("requires links = %d", n)
	}
	if n := count(t, st.DB, `SELECT COUNT(*) FROM versions WHERE entry_id = ?`, id); n != 1 {
		t.Errorf("link-only update appended a version: %d", n)
	}

	// Omitting both link lists clears them.
	if res, err := entries.Write(context.Background(), st.DB, c, entries.WriteRequest{Actor: "s-2", EntryID: id}); err != nil || res.Refusal != nil {
		t.Fatalf("clearing update: %v %v", err, res.Refusal)
	}
	if n := count(t, st.DB, `SELECT COUNT(*) FROM links WHERE from_id = ? OR to_id = ?`, id, id); n != 0 {
		t.Errorf("links after clearing update = %d", n)
	}
}

func TestCreateWithoutRequiredFieldsIsRefused(t *testing.T) {
	st, c := open(t)
	for rule, edit := range map[string]func(*entries.WriteRequest){
		"schema:title":     func(r *entries.WriteRequest) { r.Title = "" },
		"schema:read_when": func(r *entries.WriteRequest) { r.ReadWhen = "" },
		"schema:body":      func(r *entries.WriteRequest) { r.BodyMarkdown = "" },
	} {
		req := decision("Needs fields", "A body.")
		edit(&req)
		res, err := entries.Write(context.Background(), st.DB, c, req)
		if err != nil {
			t.Fatal(err)
		}
		if res.Refusal == nil || res.Refusal.Rule != rule {
			t.Errorf("refusal = %+v, want %s", res.Refusal, rule)
		}
	}
}
