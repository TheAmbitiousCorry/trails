package view_test

import (
	"context"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/view"
)

func TestZeroViewerSeesEverything(t *testing.T) {
	if !view.From(context.Background()).Sees("anything") {
		t.Fatal("the base Viewer hid an entry")
	}
}

func TestSeesRules(t *testing.T) {
	v := view.Viewer{
		Staged:  map[string]bool{"in": true, "out": true},
		Visible: map[string]bool{"in": true},
		Hidden:  map[string]bool{"orig": true},
	}
	for id, want := range map[string]bool{"plain": true, "in": true, "out": false, "orig": false} {
		if got := v.Sees(id); got != want {
			t.Errorf("Sees(%q) = %v, want %v", id, got, want)
		}
	}
	if got := view.From(view.With(context.Background(), v)); got.Sees("out") {
		t.Error("the Viewer put in ctx did not come back out")
	}
}
