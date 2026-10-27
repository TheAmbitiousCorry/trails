package signs_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/signs"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

func TestCreateSignMovesEntries(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	addSign(t, db, "ops", "root")
	addEntry(t, db, "a", "ops", "reference", "alpha")
	addEntry(t, db, "b", "ops", "reference", "bravo")
	addEntry(t, db, "c", "ops", "decision", "charlie")

	got, err := signs.CreateSign(ctx, db, signs.CreateRequest{ParentID: "ops", Title: "Billing Notes", Move: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.SignID != "billing-notes" || got.Moved != 2 {
		t.Fatalf("created = %+v", got)
	}
	for id, want := range map[string]string{"a": "billing-notes", "b": "billing-notes", "c": "ops"} {
		var sign string
		db.QueryRow(`SELECT sign_id FROM entries WHERE id = ?`, id).Scan(&sign)
		if sign != want {
			t.Errorf("entry %s under %q, want %q", id, sign, want)
		}
	}
	var parent string
	var pos int
	db.QueryRow(`SELECT parent_id, position FROM signs WHERE id = 'billing-notes'`).Scan(&parent, &pos)
	if parent != "ops" || pos != 0 {
		t.Errorf("parent %q position %d", parent, pos)
	}
	again, err := signs.CreateSign(ctx, db, signs.CreateRequest{ParentID: "ops", Title: "Billing Notes"})
	if err != nil || again.SignID != "billing-notes-2" || again.Moved != 0 {
		t.Fatalf("second = %+v %v", again, err)
	}
	db.QueryRow(`SELECT position FROM signs WHERE id = 'billing-notes-2'`).Scan(&pos)
	if pos != 1 {
		t.Errorf("second position = %d", pos)
	}
}

func TestCreateSignRefusals(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	addSign(t, db, "ops", "root")
	addSign(t, db, "other", "root")
	addEntry(t, db, "a", "ops", "reference", "alpha")
	addEntry(t, db, "far", "other", "reference", "elsewhere")

	cases := []struct {
		name string
		req  signs.CreateRequest
		rule string
	}{
		{"unknown parent", signs.CreateRequest{ParentID: "nowhere", Title: "X"}, "unknown_sign"},
		{"empty title", signs.CreateRequest{ParentID: "ops", Title: "  "}, "schema:title"},
		{"long title", signs.CreateRequest{ParentID: "ops", Title: strings.Repeat("x", 81)}, "schema:title"},
		{"entry elsewhere", signs.CreateRequest{ParentID: "ops", Title: "X", Move: []string{"a", "far"}}, "move_scope"},
		{"unknown entry", signs.CreateRequest{ParentID: "ops", Title: "X", Move: []string{"ghost"}}, "move_scope"},
	}
	for _, c := range cases {
		_, err := signs.CreateSign(ctx, db, c.req)
		var ref *trail.Refusal
		if !errorsAs(err, &ref) || ref.Rule != c.rule {
			t.Errorf("%s: %v", c.name, err)
		}
		if c.rule == "move_scope" && ref != nil && ref.Fix != "entries to move must hang under parent_sign_id" {
			t.Errorf("%s fix = %q", c.name, ref.Fix)
		}
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM signs WHERE parent_id = 'ops'`).Scan(&n)
	var sign string
	db.QueryRow(`SELECT sign_id FROM entries WHERE id = 'a'`).Scan(&sign)
	if n != 0 || sign != "ops" {
		t.Errorf("refused call left changes: %d child signs, a under %q", n, sign)
	}
}

func TestCreateSignRefusedOnFullParent(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	addSign(t, db, "fork", "root")
	for i := 1; i <= 5; i++ {
		addSign(t, db, fmt.Sprintf("f%d", i), "fork")
	}
	addEntry(t, db, "e1", "fork", "reference", "one", "billing")
	addEntry(t, db, "e2", "fork", "decision", "two", "billing")
	addEntry(t, db, "e3", "fork", "example", "three", "deploy")

	// Seven edges: five signs and three kinds is eight, so any new sign that leaves a kind behind overflows.
	_, err := signs.CreateSign(ctx, db, signs.CreateRequest{ParentID: "fork", Title: "Group", Move: []string{"e1"}})
	var ref *trail.Refusal
	if !errorsAs(err, &ref) || ref.Rule != "split this fork" {
		t.Fatalf("full parent: %v", err)
	}
	for _, want := range []string{"e1", "e2", "create a sign for a group with write_sign and pass its entry ids in move"} {
		if !strings.Contains(ref.Fix, want) {
			t.Errorf("fix lacks %q: %s", want, ref.Fix)
		}
	}
	if !strings.HasSuffix(ref.Fix, "pass its entry ids in move") {
		t.Errorf("fix does not end with the action: %s", ref.Fix)
	}

	// Moving every entry of two kinds leaves five signs, one kind and the new sign: seven edges.
	got, err := signs.CreateSign(ctx, db, signs.CreateRequest{ParentID: "fork", Title: "Group", Move: []string{"e1", "e2"}})
	if err != nil || got.Moved != 2 {
		t.Fatalf("move that makes room: %+v %v", got, err)
	}
}

func TestCreateSignChecksItsOwnFanOut(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	db.Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES ('fan_out_cap', '2')`)
	addSign(t, db, "ops", "root")
	addEntry(t, db, "a", "ops", "reference", "alpha", "x")
	addEntry(t, db, "b", "ops", "decision", "bravo", "x")
	addEntry(t, db, "c", "ops", "example", "charlie", "x")
	_, err := signs.CreateSign(ctx, db, signs.CreateRequest{ParentID: "ops", Title: "All", Move: []string{"a", "b", "c"}})
	var ref *trail.Refusal
	if !errorsAs(err, &ref) || ref.Rule != "split this fork" || !strings.Contains(ref.Fix, "the new sign would hold") {
		t.Fatalf("own fan-out: %v", err)
	}
}
