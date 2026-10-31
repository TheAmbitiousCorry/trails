package entries_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/content/markdown"
	"github.com/TheAmbitiousCorry/trails/internal/entries"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

// editOf is the request the browser sends when it saves an entry as loaded: every field, anchors as the form shows them.
func editOf(t *testing.T, st *store.Store, id string) entries.WriteRequest {
	t.Helper()
	var title, readWhen, body, tags, by, at string
	if err := st.DB.QueryRow(`SELECT title, read_when, body_json, tags_json, COALESCE(decided_by,''), COALESCE(decided_at,'') FROM entries WHERE id = ?`, id).
		Scan(&title, &readWhen, &body, &tags, &by, &at); err != nil {
		t.Fatal(err)
	}
	md, err := markdown.ToMarkdown(json.RawMessage(body))
	if err != nil {
		t.Fatal(err)
	}
	req := entries.WriteRequest{Actor: "user", Edit: true, EntryID: id, Title: title, ReadWhen: readWhen, BodyMarkdown: md, DecidedBy: by, DecidedAt: at, Anchors: []anchor.Anchor{}}
	if err := json.Unmarshal([]byte(tags), &req.Tags); err != nil {
		t.Fatal(err)
	}
	stored, err := anchor.Load(context.Background(), st.DB, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range stored {
		req.Anchors = append(req.Anchors, anchor.Anchor{Type: a.Type, PathOrURL: a.PathOrURL, RangeStart: a.RangeStart, RangeEnd: a.RangeEnd, Extra: a.Extra})
	}
	return req
}

func column(t *testing.T, st *store.Store, query string, args ...any) string {
	t.Helper()
	var s *string
	if err := st.DB.QueryRow(query, args...).Scan(&s); err != nil {
		t.Fatal(err)
	}
	if s == nil {
		return "<null>"
	}
	return *s
}

func writeFile(t *testing.T, st *store.Store, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(st.Repo, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEditKeepsTier(t *testing.T) {
	st, c := open(t)
	prov := mustWrite(t, st, c, decision("Provisional note", "Written by an agent.")).EntryID
	req := editOf(t, st, prov)
	req.BodyMarkdown = "Reworded by a person."
	res := mustWrite(t, st, c, req)
	if res.Tier != "provisional" || column(t, st, `SELECT tier FROM entries WHERE id = ?`, prov) != "provisional" {
		t.Errorf("an edit of a provisional entry stored tier %q (result %q)", column(t, st, `SELECT tier FROM entries WHERE id = ?`, prov), res.Tier)
	}

	hum := decision("Stale note", "Vouched for once.")
	hum.Actor = "user"
	stale := mustWrite(t, st, c, hum).EntryID
	st.DB.Exec(`UPDATE entries SET tier = 'stale', tier_before_stale = 'verified' WHERE id = ?`, stale)
	req = editOf(t, st, stale)
	req.BodyMarkdown = "Reworded, still unchecked."
	mustWrite(t, st, c, req)
	if got := column(t, st, `SELECT tier || '/' || COALESCE(tier_before_stale,'') FROM entries WHERE id = ?`, stale); got != "stale/verified" {
		t.Errorf("an edit of a stale entry left tier/tier_before_stale = %q, want stale/verified", got)
	}
}

func TestEditKeepsHashesOfUnchangedAnchors(t *testing.T) {
	st, c := open(t)
	writeFile(t, st, "a.txt", "one\n")
	writeFile(t, st, "b.txt", "two\n")
	req := decision("Anchored note", "Describes why a.txt exists.")
	req.Kind, req.Actor = "summary", "user"
	req.Anchors = []anchor.Anchor{{Type: "file", PathOrURL: "a.txt"}}
	id := mustWrite(t, st, c, req).EntryID
	hashOf := func(path string) string {
		return column(t, st, `SELECT hash || '/' || ok FROM anchors WHERE entry_id = ? AND path_or_url = ?`, id, path)
	}
	before := hashOf("a.txt")
	writeFile(t, st, "a.txt", "changed\n")
	st.DB.Exec(`UPDATE anchors SET ok = 0 WHERE entry_id = ?`, id)
	before = strings.TrimSuffix(before, "/1") + "/0"

	edit := editOf(t, st, id)
	edit.BodyMarkdown = "Describes why a.txt and b.txt exist."
	edit.Anchors = append(edit.Anchors, anchor.Anchor{Type: "file", PathOrURL: "b.txt"})
	mustWrite(t, st, c, edit)
	if got := hashOf("a.txt"); got != before {
		t.Errorf("unchanged anchor hash/ok = %q, want the stored %q", got, before)
	}
	if got := hashOf("b.txt"); !strings.HasSuffix(got, "/1") || strings.HasPrefix(got, "/") {
		t.Errorf("new anchor was not resolved: %q", got)
	}

	// The agent path is unchanged: a write by a human without Edit re-resolves every anchor.
	plain := editOf(t, st, id)
	plain.Edit = false
	plain.BodyMarkdown = "Describes why both files exist."
	mustWrite(t, st, c, plain)
	if got := hashOf("a.txt"); got == before || !strings.HasSuffix(got, "/1") {
		t.Errorf("a write without Edit kept the old hash: %q", got)
	}
}

func TestEditWithUnchangedAnchorsSkipsTheAnchorRule(t *testing.T) {
	st, c := open(t)
	writeFile(t, st, "a.txt", "one\n")
	req := decision("Anchored note", "Describes why a.txt exists.")
	req.Kind, req.Actor = "summary", "user"
	req.Anchors = []anchor.Anchor{{Type: "file", PathOrURL: "a.txt"}}
	id := mustWrite(t, st, c, req).EntryID
	kept := column(t, st, `SELECT hash FROM anchors WHERE entry_id = ?`, id)
	os.Remove(filepath.Join(st.Repo, "a.txt"))

	edit := editOf(t, st, id)
	edit.BodyMarkdown = "Reworded after the file went away."
	mustWrite(t, st, c, edit)
	if got := column(t, st, `SELECT hash FROM anchors WHERE entry_id = ?`, id); got != kept {
		t.Errorf("anchor hash after the edit = %q, want %q", got, kept)
	}

	// Changing the set brings the rule back, and the agent path never had the skip.
	edit = editOf(t, st, id)
	edit.Anchors = append(edit.Anchors, anchor.Anchor{Type: "file", PathOrURL: "gone.txt"})
	if res := writeRaw(t, st, c, edit); res.Refusal == nil || res.Refusal.Rule != "anchor" {
		t.Errorf("a changed anchor set that does not resolve = %+v", res.Refusal)
	}
	agent := editOf(t, st, id)
	agent.Edit, agent.Actor = false, "s-1"
	if res := writeRaw(t, st, c, agent); res.Refusal == nil || res.Refusal.Rule != "anchor" {
		t.Errorf("an agent update with an unresolvable anchor = %+v", res.Refusal)
	}

	// The anchorless root summary can have its wording edited.
	st.DB.Exec(`DELETE FROM anchors WHERE entry_id = ?`, id)
	edit = editOf(t, st, id)
	edit.BodyMarkdown = "Wording of an entry with no anchors."
	mustWrite(t, st, c, edit)
	agent = editOf(t, st, id)
	agent.Edit, agent.Actor = false, "s-1"
	if res := writeRaw(t, st, c, agent); res.Refusal == nil || res.Refusal.Rule != "anchor" {
		t.Errorf("an agent update of an anchorless summary = %+v", res.Refusal)
	}
}

func TestEditSkipsTheDuplicateRuleOnlyWhileTheSearchedTextIsUnchanged(t *testing.T) {
	st, c := open(t)
	fillers(t, st, c)
	const first = "Round each invoice line to the nearest cent before summing totals."
	mustWrite(t, st, c, decision("Quarterly invoice rounding", first))
	dup := decision("Quarterly invoice rounding", first+"\n\nSecond paragraph about ledgers.")
	dup.Resolution, dup.DistinctReason = "distinct", "the annual run"
	y := mustWrite(t, st, c, dup).EntryID

	tags := editOf(t, st, y)
	tags.Tags = []string{"billing"}
	mustWrite(t, st, c, tags)

	second := editOf(t, st, y)
	second.BodyMarkdown = first + "\n\nA different second paragraph."
	mustWrite(t, st, c, second)

	agent := editOf(t, st, y)
	agent.Edit, agent.Actor = false, "s-1"
	agent.Tags = []string{"billing", "money"}
	if res := writeRaw(t, st, c, agent); res.Refusal == nil || res.Refusal.Rule != "duplicate" {
		t.Errorf("an agent update of a near-duplicate = %+v", res.Refusal)
	}

	changed := editOf(t, st, y)
	changed.BodyMarkdown = "Round each invoice line to the nearest cent before summing every total.\n\nA different second paragraph."
	if res := writeRaw(t, st, c, changed); res.Refusal == nil || res.Refusal.Rule != "duplicate" {
		t.Errorf("an edit that changes the first paragraph but stays a duplicate = %+v", res.Refusal)
	}
}

func TestEditDuplicateRefusalOffersOnlyWhatAnUpdateCanUse(t *testing.T) {
	st, c := open(t)
	fillers(t, st, c)
	const first = "Round each invoice line to the nearest cent before summing totals."
	mustWrite(t, st, c, decision("Quarterly invoice rounding", first))
	dup := decision("Quarterly invoice rounding", first+"\n\nSecond paragraph about ledgers.")
	dup.Resolution, dup.DistinctReason = "distinct", "the annual run"
	y := mustWrite(t, st, c, dup).EntryID

	changed := editOf(t, st, y)
	changed.BodyMarkdown = "Round each invoice line to the nearest cent before summing every total.\n\nA different second paragraph."
	res := writeRaw(t, st, c, changed)
	if res.Refusal == nil || res.Refusal.Rule != "duplicate" {
		t.Fatalf("edit refusal = %+v", res.Refusal)
	}
	fix := res.Refusal.Fix
	if strings.Contains(fix, "merge") || !strings.Contains(fix, "replace") || !strings.Contains(fix, "distinct_reason") {
		t.Errorf("an edit's duplicate fix must offer replace or distinct only: %q", fix)
	}

	fresh := decision("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing every total.")
	if res := writeRaw(t, st, c, fresh); res.Refusal == nil || !strings.Contains(res.Refusal.Fix, "merge") {
		t.Errorf("a new write still offers merge: %+v", res.Refusal)
	}
}

func TestEditOfAStagedEntryIsRefused(t *testing.T) {
	st, c := open(t)
	id := mustWrite(t, st, c, decision("Branch note", "Written on a branch.")).EntryID
	st.DB.Exec(`UPDATE entries SET staged_branch = 'feat/x' WHERE id = ?`, id)

	res := writeRaw(t, st, c, editOf(t, st, id))
	if res.Refusal == nil || res.Refusal.Rule != "staged_elsewhere" || !strings.Contains(res.Refusal.Fix, "feat/x") {
		t.Fatalf("edit of a staged entry = %+v", res.Refusal)
	}
	agent := decision("Branch note", "Agent rewrite, no checkout.")
	agent.EntryID = id
	if res := writeRaw(t, st, c, agent); res.Refusal != nil {
		t.Errorf("an agent update with no checkout was refused: %+v", res.Refusal)
	}
}

func TestIfMatchRefusesAnEditOfAChangedEntry(t *testing.T) {
	st, c := open(t)
	id := mustWrite(t, st, c, decision("Shared note", "Original words.")).EntryID
	token := func() string { return column(t, st, `SELECT updated_at FROM entries WHERE id = ?`, id) }
	loaded := token()

	other := editOf(t, st, id)
	other.IfMatch = loaded
	other.Tags = []string{"billing"}
	mustWrite(t, st, c, other)
	if token() == loaded {
		t.Fatal("a tags-only edit left updated_at where it was")
	}

	mine := editOf(t, st, id)
	mine.IfMatch = loaded
	mine.BodyMarkdown = "My words, written on the old text."
	mine.Tags = nil
	res := writeRaw(t, st, c, mine)
	if res.Refusal == nil || res.Refusal.Rule != "stale_edit" || !strings.Contains(res.Refusal.Fix, id) {
		t.Fatalf("edit on an old token = %+v", res.Refusal)
	}
	if got := column(t, st, `SELECT tags_json FROM entries WHERE id = ?`, id); got != `["billing"]` {
		t.Errorf("a refused edit changed the entry: tags %s", got)
	}

	mine.IfMatch = token()
	mustWrite(t, st, c, mine)
	agent := decision("Shared note", "An agent rewrite sends no token.")
	agent.EntryID = id
	mustWrite(t, st, c, agent)
}

func TestEditNeedsAHumanAndAnEntry(t *testing.T) {
	st, c := open(t)
	id := mustWrite(t, st, c, decision("Some note", "Words.")).EntryID
	byAgent := editOf(t, st, id)
	byAgent.Actor = "s-1"
	if res := writeRaw(t, st, c, byAgent); res.Refusal == nil || res.Refusal.Rule != "schema" {
		t.Errorf("Edit by an agent = %+v", res.Refusal)
	}
	create := decision("Other note", "Words.")
	create.Actor, create.Edit = "user", true
	if res := writeRaw(t, st, c, create); res.Refusal == nil || res.Refusal.Rule != "schema" {
		t.Errorf("Edit without an entry_id = %+v", res.Refusal)
	}
}

func TestNoopEditChangesNothingButTheToken(t *testing.T) {
	st, c := open(t)
	writeFile(t, st, "a.txt", "one\n")
	req := decision("Anchored note", "Describes why a.txt exists.")
	req.Kind = "summary"
	req.Anchors = []anchor.Anchor{{Type: "file", PathOrURL: "a.txt"}}
	id := mustWrite(t, st, c, req).EntryID
	writeFile(t, st, "a.txt", "changed\n")
	st.DB.Exec(`UPDATE entries SET tier = 'stale', tier_before_stale = 'provisional' WHERE id = ?`, id)
	st.DB.Exec(`UPDATE anchors SET ok = 0 WHERE entry_id = ?`, id)
	snap := func() string {
		return column(t, st, `SELECT e.tier || '|' || COALESCE(e.tier_before_stale,'') || '|' || e.body_json || '|' || a.hash || '|' || a.ok || '|' || (SELECT COUNT(*) FROM versions WHERE entry_id = e.id)
			FROM entries e JOIN anchors a ON a.entry_id = e.id WHERE e.id = ?`, id)
	}
	before := snap()
	res := mustWrite(t, st, c, editOf(t, st, id))
	if res.Tier != "stale" || snap() != before {
		t.Errorf("a no-op edit changed the entry:\nbefore %s\nafter  %s", before, snap())
	}
}

func TestEditStoresNoNewEntryAndSoAddsNoEdge(t *testing.T) {
	st, c := open(t)
	st.DB.Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES ('fan_out_cap', '7')`)
	st.DB.Exec(`INSERT INTO signs (id, parent_id, title) VALUES ('fork', 'root', 'Fork')`)
	for _, id := range []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7"} {
		if _, err := st.DB.Exec(`INSERT INTO signs (id, parent_id, title) VALUES (?, 'fork', 'c')`, id); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, st, "a.txt", "one\n")
	req := decision("Lone summary", "Describes why a.txt exists.")
	req.Kind, req.ParentSignID, req.Actor = "summary", "fork", "user"
	req.Anchors = []anchor.Anchor{{Type: "file", PathOrURL: "a.txt"}}
	id := mustWrite(t, st, c, req).EntryID

	edit := editOf(t, st, id)
	edit.BodyMarkdown = "Describes why a.txt exists, reworded."
	mustWrite(t, st, c, edit)
}

func TestEditSkipsParaphraseOnlyWhileTheBodyIsUnchanged(t *testing.T) {
	st, c := open(t)
	const body = "The cache is flushed whenever the queue drains fully."
	writeFile(t, st, "a.txt", "one\n")
	req := decision("Cache flush", body)
	req.Kind = "summary"
	req.Anchors = []anchor.Anchor{{Type: "file", PathOrURL: "a.txt"}}
	id := mustWrite(t, st, c, req).EntryID
	// The anchored file later comes to contain the body's words, as a doc edited after the entry was written can.
	writeFile(t, st, "a.txt", body+"\n")

	agent := editOf(t, st, id)
	agent.Edit, agent.Actor, agent.Tags = false, "s-1", []string{"cache"}
	if res := writeRaw(t, st, c, agent); res.Refusal == nil || res.Refusal.Rule != "paraphrase" {
		t.Fatalf("an agent update beside a file that now holds the body = %+v", res.Refusal)
	}
	tags := editOf(t, st, id)
	tags.Tags = []string{"cache"}
	mustWrite(t, st, c, tags)

	changed := editOf(t, st, id)
	changed.BodyMarkdown = body + " It is also flushed on exit."
	if res := writeRaw(t, st, c, changed); res.Refusal == nil || res.Refusal.Rule != "paraphrase" {
		t.Errorf("an edit that changes a body still copied from the file = %+v", res.Refusal)
	}
}

func TestASeeAlsoLinkWrittenFromTheOtherEndMovesTheToken(t *testing.T) {
	st, c := open(t)
	a := mustWrite(t, st, c, decision("Ledger close", "The ledger closes monthly.")).EntryID
	token := func() string { return column(t, st, `SELECT updated_at FROM entries WHERE id = ?`, a) }
	loaded := token()

	b := decision("Ledger audit", "The audit reads the closed ledger.")
	b.SeeAlso = []string{a}
	bid := mustWrite(t, st, c, b).EntryID
	if token() == loaded {
		t.Fatal("a see_also link added from the other end left the token where it was")
	}

	linked := token()
	b.EntryID, b.SeeAlso = bid, nil
	mustWrite(t, st, c, b)
	if token() == linked {
		t.Fatal("a see_also link removed from the other end left the token where it was")
	}
}
