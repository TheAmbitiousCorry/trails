package search_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/entries"
	"github.com/TheAmbitiousCorry/trails/internal/search"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

func open(t *testing.T) (*store.Store, anchor.Checker) {
	t.Helper()
	st, err := store.Open(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	c := anchor.Checker{Repo: st.Repo}
	// Unrelated entries keep bm25 idf positive for terms unique to the entry under test.
	for i, topic := range []string{"harbour tides", "orchard grafting", "violin varnish", "glacier survey", "pottery kiln", "beekeeping winter"} {
		write(t, st, c, entries.WriteRequest{Actor: "user", ParentSignID: "root", Kind: "decision", Title: "Notes on " + topic,
			ReadWhen: "you study " + topic, BodyMarkdown: fmt.Sprintf("Filler %d covers %s only.", i, topic), DecidedBy: "ana", DecidedAt: "2026-10-27"})
	}
	return st, c
}

func write(t *testing.T, st *store.Store, c anchor.Checker, req entries.WriteRequest) entries.WriteResult {
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

func decision(actor, parent, title, body string) entries.WriteRequest {
	return entries.WriteRequest{Actor: actor, ParentSignID: parent, Kind: "decision", Title: title, ReadWhen: "you need " + title,
		BodyMarkdown: body, DecidedBy: "ana", DecidedAt: "2026-10-27"}
}

func TestHitsCarrySignPath(t *testing.T) {
	st, c := open(t)
	st.DB.Exec(`INSERT INTO signs (id, parent_id, title) VALUES ('billing', 'root', 'Billing')`)
	st.DB.Exec(`INSERT INTO signs (id, parent_id, title) VALUES ('invoices', 'billing', 'Invoices')`)
	write(t, st, c, decision("user", "invoices", "Credit note numbering", "Credit notes continue the invoice sequence."))
	write(t, st, c, decision("user", "billing", "Refund approval limits", "Refunds above the limit need a second approver."))

	var rootTitle string
	st.DB.QueryRow(`SELECT title FROM signs WHERE id = 'root'`).Scan(&rootTitle)
	hits, miss, err := search.Query(context.Background(), st.DB, "credit note numbering", "", 5)
	if err != nil || miss || len(hits) != 1 {
		t.Fatalf("hits = %+v miss=%v err=%v", hits, miss, err)
	}
	if got := strings.Join(hits[0].SignPath, " > "); got != rootTitle+" > Billing > Invoices" {
		t.Errorf("sign path = %q", got)
	}
	if h := hits[0]; h.EntryID != "credit-note-numbering" || h.Title == "" || h.ReadWhen == "" || h.Tier != "verified" || h.Kind != "decision" {
		t.Errorf("hit fields = %+v", h)
	}
	hits, _, _ = search.Query(context.Background(), st.DB, "refund approver", "", 5)
	if len(hits) != 1 || strings.Join(hits[0].SignPath, " > ") != rootTitle+" > Billing" {
		t.Errorf("refund hit = %+v", hits)
	}
}

func TestVerifiedOutranksProvisionalOutranksStale(t *testing.T) {
	st, c := open(t)
	body := "Late fees accrue daily after the due date."
	// Written in the reverse of the expected order, so insertion order cannot explain the ranking.
	stale := write(t, st, c, decision("s-1", "root", "Late fee schedule", body))
	st.DB.Exec(`UPDATE entries SET tier = 'stale', tier_before_stale = 'provisional' WHERE id = ?`, stale.EntryID)
	prov := decision("s-1", "root", "Late fee schedule", body)
	prov.Resolution, prov.DistinctReason = "distinct", "agent copy"
	provID := write(t, st, c, prov).EntryID
	ver := decision("user", "root", "Late fee schedule", body)
	ver.Resolution, ver.DistinctReason = "distinct", "human copy"
	verID := write(t, st, c, ver).EntryID

	hits, miss, err := search.Query(context.Background(), st.DB, "late fee schedule due date", "", 5)
	if err != nil || miss || len(hits) != 3 {
		t.Fatalf("hits = %+v miss=%v err=%v", hits, miss, err)
	}
	got := []string{hits[0].EntryID, hits[1].EntryID, hits[2].EntryID}
	if want := []string{verID, provID, stale.EntryID}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", got, want)
	}
	if hits, _, _ = search.Query(context.Background(), st.DB, "late fee schedule due date", "", 2); len(hits) != 2 {
		t.Errorf("limit 2 returned %d hits", len(hits))
	}
}

func TestMissAndMalformedQueries(t *testing.T) {
	st, _ := open(t)
	for _, q := range []string{"qzxv wplk jnrt", "", "   ", `"qzxv" (zzzq NEAR/x * -`} {
		hits, miss, err := search.Query(context.Background(), st.DB, q, "", 5)
		if err != nil || !miss || len(hits) != 0 {
			t.Errorf("query %q: hits=%+v miss=%v err=%v", q, hits, miss, err)
		}
	}
}

func TestSeededEntryIsSearchable(t *testing.T) {
	st, _ := open(t)
	hits, miss, err := search.Query(context.Background(), st.DB, "agents walk wiki sign", "", 5)
	if err != nil || miss || hits[0].EntryID != "trail" {
		t.Errorf("seed search = %+v miss=%v err=%v", hits, miss, err)
	}
}
