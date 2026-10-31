package signs_test

import (
	"context"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/signs"
	"github.com/TheAmbitiousCorry/trails/internal/view"
)

func TestLookAndTheCapCountOnlyWhatTheViewerSees(t *testing.T) {
	db := open(t)
	addSign(t, db, "area", "root")
	addEntry(t, db, "shown", "area", "decision", "visible text")
	addEntry(t, db, "hidden", "area", "procedure", "hidden text")
	hide := view.With(context.Background(), view.Viewer{Hidden: map[string]bool{"hidden": true}})

	v, err := signs.Look(hide, db, signs.LookRequest{SignID: "area"})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Edges) != 1 || v.Edges[0].ID != "shown" {
		t.Errorf("edges = %+v, want only shown", v.Edges)
	}
	if n, _ := signs.EdgeCount(hide, db, "area"); n != 1 {
		t.Errorf("edge count under the Viewer = %d, want 1", n)
	}
	if n, _ := signs.EdgeCount(context.Background(), db, "area"); n != 2 {
		t.Errorf("edge count under the base Viewer = %d, want 2", n)
	}
}
