package entries_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/entries"
	"github.com/TheAmbitiousCorry/trails/internal/search"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

// fillers gives the corpus enough unrelated entries that bm25 idf is positive for terms unique to one of them.
func fillers(t *testing.T, st *store.Store, c anchor.Checker) {
	t.Helper()
	topics := []string{"harbour tides", "orchard grafting", "violin varnish", "glacier survey", "pottery kiln", "beekeeping winter", "telescope mirror", "sourdough starter"}
	for i, topic := range topics {
		mustWrite(t, st, c, decision("Notes on "+topic, fmt.Sprintf("Filler %d covers %s and nothing else about %s.", i, topic, strings.ToUpper(topic[:3]))))
	}
}

func writeRaw(t *testing.T, st *store.Store, c anchor.Checker, req entries.WriteRequest) entries.WriteResult {
	t.Helper()
	res, err := entries.Write(context.Background(), st.DB, c, req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestDuplicateNamesThreeOptions(t *testing.T) {
	st, c := open(t)
	fillers(t, st, c)
	first := mustWrite(t, st, c, decision("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals."))

	res := writeRaw(t, st, c, decision("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals."))
	if res.Refusal == nil || res.Refusal.Rule != "duplicate" {
		t.Fatalf("second identical write = %+v", res)
	}
	for _, want := range []string{first.EntryID, "merge", "replace", "distinct"} {
		if !strings.Contains(res.Refusal.Fix, want) {
			t.Errorf("fix does not name %q: %s", want, res.Refusal.Fix)
		}
	}

	req := decision("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals.")
	req.Resolution = "distinct"
	if r := writeRaw(t, st, c, req); r.Refusal == nil || r.Refusal.Rule != "duplicate" {
		t.Errorf("distinct without a reason = %+v", r.Refusal)
	}
	req.DistinctReason = "covers the annual run, not the quarterly one"
	got := mustWrite(t, st, c, req)
	var reason string
	st.DB.QueryRow(`SELECT distinct_reason FROM entries WHERE id = ?`, got.EntryID).Scan(&reason)
	if got.EntryID == first.EntryID || reason != req.DistinctReason {
		t.Errorf("distinct write = %+v, reason %q", got, reason)
	}
}

func TestMergeAddsSearchableAlias(t *testing.T) {
	st, c := open(t)
	fillers(t, st, c)
	first := mustWrite(t, st, c, decision("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals."))

	req := decision("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals.")
	req.ReadWhen = "pennies disagree with the ledger"
	req.Resolution = "merge"
	res := mustWrite(t, st, c, req)
	if !res.Merged || res.EntryID != first.EntryID {
		t.Fatalf("merge result = %+v", res)
	}
	if n := count(t, st.DB, `SELECT COUNT(*) FROM entries WHERE title = 'Quarterly invoice rounding'`); n != 1 {
		t.Errorf("merge stored %d entries", n)
	}
	if n := count(t, st.DB, `SELECT COUNT(*) FROM entry_aliases WHERE entry_id = ? AND read_when = ?`, first.EntryID, req.ReadWhen); n != 1 {
		t.Errorf("aliases = %d", n)
	}
	hits, miss, err := search.Query(context.Background(), st.DB, "pennies ledger", "", 5)
	if err != nil || miss || len(hits) == 0 || hits[0].EntryID != first.EntryID {
		t.Errorf("alias search = %+v miss=%v err=%v", hits, miss, err)
	}
}

func TestReplaceSupersedes(t *testing.T) {
	st, c := open(t)
	fillers(t, st, c)
	old := mustWrite(t, st, c, decision("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals."))

	req := decision("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals.")
	req.Resolution = "replace"
	res := mustWrite(t, st, c, req)
	if res.EntryID == old.EntryID || res.Merged {
		t.Fatalf("replace result = %+v", res)
	}
	if n := count(t, st.DB, `SELECT COUNT(*) FROM links WHERE from_id = ? AND to_id = ? AND relation = 'supersedes'`, res.EntryID, old.EntryID); n != 1 {
		t.Errorf("supersedes links = %d", n)
	}
	var by, tier string
	st.DB.QueryRow(`SELECT superseded_by, tier FROM entries WHERE id = ?`, old.EntryID).Scan(&by, &tier)
	if by != res.EntryID || tier != "dormant" {
		t.Errorf("old entry superseded_by=%q tier=%q", by, tier)
	}
	hits, _, err := search.Query(context.Background(), st.DB, "invoice rounding nearest cent", "", 5)
	if err != nil || len(hits) != 1 || hits[0].EntryID != res.EntryID {
		t.Errorf("default search after replace = %+v err=%v", hits, err)
	}
}

func TestAgentCannotReplaceVerified(t *testing.T) {
	st, c := open(t)
	fillers(t, st, c)
	h := decision("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals.")
	h.Actor = "user"
	mustWrite(t, st, c, h)
	req := decision("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals.")
	req.Resolution = "replace"
	if r := writeRaw(t, st, c, req); r.Refusal == nil || r.Refusal.Rule != "verified" {
		t.Errorf("agent replace of verified = %+v", r.Refusal)
	}
}

func TestParaphraseRefusesCopiedSource(t *testing.T) {
	st, c := open(t)
	fillers(t, st, c)
	var words []string
	for i := 0; i < 500; i++ {
		words = append(words, fmt.Sprintf("w%dx", i*7919%1009))
	}
	src := strings.Join(words, " ")
	if err := os.WriteFile(filepath.Join(st.Repo, "module.txt"), []byte(src+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	summary := func(body string) entries.WriteRequest {
		return entries.WriteRequest{Actor: "s-1", ParentSignID: "root", Kind: "summary", Title: "Module summary", ReadWhen: "you open the module",
			BodyMarkdown: body, Anchors: []anchor.Anchor{{Type: "file", PathOrURL: "module.txt"}}}
	}
	res := writeRaw(t, st, c, summary(strings.Join(words[:300], " ")))
	if res.Refusal == nil || res.Refusal.Rule != "paraphrase" || res.Refusal.Fix != "describe the why, point at the what" {
		t.Fatalf("copied source = %+v", res.Refusal)
	}
	mustWrite(t, st, c, summary("This module owns billing rounding because ledgers must agree with invoices; read the file for the mechanics."))
}

func TestScoresLoggedOnEveryWrite(t *testing.T) {
	st, c := open(t)
	fillers(t, st, c)
	mustWrite(t, st, c, decision("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals."))
	writeRaw(t, st, c, decision("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals."))
	// 8 fillers, the accepted write and the duplicate refusal each logged scores.
	if n := count(t, st.DB, `SELECT COUNT(*) FROM events WHERE tool = 'write_scores'`); n != 10 {
		t.Errorf("write_scores rows = %d, want 10", n)
	}
	var shown, result string
	st.DB.QueryRow(`SELECT shown_json, result FROM events WHERE tool = 'write_scores' ORDER BY id DESC LIMIT 1`).Scan(&shown, &result)
	if result != "refused:duplicate" || !strings.Contains(shown, `"duplicate":`) || !strings.Contains(shown, `"duplicate_of":"quarterly-invoice-rounding"`) {
		t.Errorf("last scores row = %s %s", result, shown)
	}
}
