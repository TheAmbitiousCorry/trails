package search_test

import (
	"context"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/search"
	"github.com/TheAmbitiousCorry/trails/internal/view"
)

func TestRankSkipsWhatTheViewerIsNotShown(t *testing.T) {
	st, c := open(t)
	res := write(t, st, c, decision("user", "root", "Ledger rounding rule", "Round every ledger line to the nearest cent."))
	q := "ledger rounding rule nearest cent"
	if hits, err := search.Rank(context.Background(), st.DB, q, ""); err != nil || len(hits) == 0 || hits[0].EntryID != res.EntryID {
		t.Fatalf("base Viewer: hits = %v, err = %v", hits, err)
	}
	hide := view.With(context.Background(), view.Viewer{Hidden: map[string]bool{res.EntryID: true}})
	hits, err := search.Rank(hide, st.DB, q, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.EntryID == res.EntryID {
			t.Errorf("hidden entry %s was ranked", res.EntryID)
		}
	}
}
