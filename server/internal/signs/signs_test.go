package signs_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/signs"
	"github.com/TheAmbitiousCorry/trails/internal/store"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

func open(t *testing.T) *sql.DB {
	t.Helper()
	st, err := store.Open(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st.DB
}

func addSign(t *testing.T, db *sql.DB, id, parent string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO signs (id, parent_id, title) VALUES (?, ?, ?)`, id, parent, "Sign "+id); err != nil {
		t.Fatal(err)
	}
}

func addEntry(t *testing.T, db *sql.DB, id, sign, kind, text string, tags ...string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}},
	}})
	tj, _ := json.Marshal(append([]string{}, tags...))
	now := store.Now()
	if _, err := db.Exec(`INSERT INTO entries (id, sign_id, kind, title, read_when, body_json, body_schema, tags_json, tier, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'when needed', ?, 1, ?, 'provisional', ?, ?)`, id, sign, kind, "Entry "+id, string(body), string(tj), now, now); err != nil {
		t.Fatal(err)
	}
}

func TestEighthEdgeRefused(t *testing.T) {
	db := open(t)
	db.Exec(`UPDATE settings SET value = '7' WHERE key = 'fan_out_cap'`)
	ctx := context.Background()
	for i := 1; i <= 7; i++ {
		addSign(t, db, fmt.Sprintf("c%d", i), "root")
		// Tags give the refusal something to group by.
		tag := "billing"
		if i > 4 {
			tag = "deploy"
		}
		addEntry(t, db, fmt.Sprintf("e%d", i), fmt.Sprintf("c%d", i), "reference", "pointer", tag)
	}
	// Root now holds more than seven edges, so its refusal carries a tag grouping; "fork" holds exactly seven.
	addSign(t, db, "fork", "root")
	for i := 1; i <= 7; i++ {
		addSign(t, db, fmt.Sprintf("f%d", i), "fork")
	}
	err := signs.CheckFanOut(ctx, db, "fork")
	var ref *trail.Refusal
	if !errorsAs(err, &ref) || ref.Rule != "split this fork" {
		t.Fatalf("eighth insert: %v", err)
	}
	if err := signs.CheckFanOut(ctx, db, "c1"); err != nil {
		t.Errorf("sign with one edge refused: %v", err)
	}
	err = signs.CheckFanOut(ctx, db, "root")
	if !errorsAs(err, &ref) || !strings.Contains(ref.Fix, "billing") || !strings.Contains(ref.Fix, "deploy") {
		t.Errorf("fix should group by tag: %v", err)
	}
	db.Exec(`UPDATE settings SET value = '20' WHERE key = 'fan_out_cap'`)
	if err := signs.CheckFanOut(ctx, db, "fork"); err != nil {
		t.Errorf("cap setting ignored: %v", err)
	}
}

func errorsAs(err error, ref **trail.Refusal) bool {
	r, ok := err.(*trail.Refusal)
	if ok {
		*ref = r
	}
	return ok
}

func TestSummaryChildIsTheSummaryLine(t *testing.T) {
	db := open(t)
	addSign(t, db, "billing", "root")
	addEntry(t, db, "b-sum", "billing", "summary", "Billing owns invoices.\nIt must never touch payments.")
	v, err := signs.Look(context.Background(), db, signs.LookRequest{SignID: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Summary != "Billing owns invoices." {
		t.Errorf("summary = %q", v.Summary)
	}
	if len(v.Edges) != 1 || v.Edges[0].ID != "b-sum" || v.Parent == "" {
		t.Errorf("edges = %+v parent = %q", v.Edges, v.Parent)
	}
	bare := open(t)
	addSign(t, bare, "empty", "root")
	v, _ = signs.Look(context.Background(), bare, signs.LookRequest{SignID: "empty"})
	if v.Summary != "Sign empty" {
		t.Errorf("fallback summary = %q", v.Summary)
	}
}

func TestTaskTypeFoldsOutsiders(t *testing.T) {
	db := open(t)
	for _, id := range []string{"a", "b", "c", "d"} {
		addSign(t, db, id, "root")
	}
	addEntry(t, db, "ea", "a", "reference", "report template", "report")
	addEntry(t, db, "eb", "b", "reference", "deploy steps", "deploy")
	addEntry(t, db, "ec", "c", "reference", "invoice rules", "billing")
	addEntry(t, db, "ed", "d", "reference", "more report text", "report")
	v, err := signs.Look(context.Background(), db, signs.LookRequest{SignID: "root", TaskType: "report"})
	if err != nil {
		t.Fatal(err)
	}
	// Kept: a, d. Folded: b, c and the untagged seed entry.
	last := v.Edges[len(v.Edges)-1]
	if !last.Folded || last.FoldedCount != 3 || last.Title != "3 more edges outside report" {
		t.Errorf("fold edge = %+v", last)
	}
	if len(v.Edges) != 3 {
		t.Errorf("edges = %d", len(v.Edges))
	}
}

func TestTagLookGroupsByHomeSign(t *testing.T) {
	db := open(t)
	for _, s := range []string{"x", "y", "z"} {
		addSign(t, db, s, "root")
		for i := 1; i <= 3; i++ {
			addEntry(t, db, fmt.Sprintf("%s%d", s, i), s, "reference", "billing note "+s, "billing")
		}
	}
	v, err := signs.Look(context.Background(), db, signs.LookRequest{Tag: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Virtual || len(v.Edges) != 3 || !strings.Contains(v.Summary, "billing") {
		t.Fatalf("view = %+v", v)
	}
	for _, e := range v.Edges {
		if e.Type != "sign" || e.MatchCount != 3 {
			t.Errorf("edge = %+v", e)
		}
	}
	v, _ = signs.Look(context.Background(), db, signs.LookRequest{Tag: "billing", Within: "x"})
	if len(v.Edges) != 3 || v.Edges[0].Type != "entry" {
		t.Errorf("within x = %+v", v.Edges)
	}
}

func TestDepthAndScent(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	addSign(t, db, "m", "root")
	addEntry(t, db, "m1", "m", "reference", "kubernetes kubernetes rollout", "deploy")
	addEntry(t, db, "m2", "root", "reference", "invoice invoice ledger", "billing")
	v, err := signs.Look(ctx, db, signs.LookRequest{SignID: "root", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	var m *signs.Edge
	for i := range v.Edges {
		if v.Edges[i].ID == "m" {
			m = &v.Edges[i]
		}
	}
	if m == nil || m.Children == nil || len(m.Children.Edges) != 1 {
		t.Fatalf("depth 2 did not nest: %+v", v.Edges)
	}
	if m.Scent[0] != "deploy" || !strings.Contains(strings.Join(m.Scent, " "), "kubernetes") {
		t.Errorf("scent = %v", m.Scent)
	}
	if v.Lines < 10 {
		t.Errorf("lines = %d", v.Lines)
	}
	_, err = signs.Look(ctx, db, signs.LookRequest{SignID: "root", Depth: 4})
	var ref *trail.Refusal
	if !errorsAs(err, &ref) || ref.Rule != "depth_cap" {
		t.Errorf("depth 4: %v", err)
	}
}

func TestLoneSummaryIsNoEdge(t *testing.T) {
	db := open(t)
	db.Exec(`UPDATE settings SET value = '7' WHERE key = 'fan_out_cap'`)
	ctx := context.Background()
	addSign(t, db, "fork", "root")
	for i := 1; i <= 7; i++ {
		addSign(t, db, fmt.Sprintf("f%d", i), "fork")
	}
	addEntry(t, db, "sum1", "fork", "summary", "first description")
	if n, _ := signs.EdgeCount(ctx, db, "fork"); n != 7 {
		t.Errorf("edges with a lone summary = %d, want 7", n)
	}
	if err := signs.CheckFanOut(ctx, db, "fork"); err == nil {
		t.Error("eighth edge allowed beside a lone summary")
	}
	addEntry(t, db, "sum2", "fork", "summary", "second description")
	if n, _ := signs.EdgeCount(ctx, db, "fork"); n != 8 {
		t.Errorf("edges with two summaries = %d, want 8", n)
	}
	addSign(t, db, "small", "root")
	addEntry(t, db, "only", "small", "summary", "alone")
	if n, _ := signs.EdgeCount(ctx, db, "small"); n != 0 {
		t.Errorf("sign holding only a summary has %d edges", n)
	}
	if err := signs.CheckFanOut(ctx, db, "small"); err != nil {
		t.Error(err)
	}
}
