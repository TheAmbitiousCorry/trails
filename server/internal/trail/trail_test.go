package trail_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/entries"
	"github.com/TheAmbitiousCorry/trails/internal/store"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

var t0 = time.Date(2026, 11, 30, 12, 0, 0, 0, time.UTC)

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func open(t *testing.T) (*store.Store, anchor.Checker) {
	t.Helper()
	st, err := store.Open(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, anchor.Checker{Repo: st.Repo}
}

func write(t *testing.T, st *store.Store, c anchor.Checker, actor, title string) string {
	t.Helper()
	res, err := entries.Write(context.Background(), st.DB, c, entries.WriteRequest{Actor: actor, ParentSignID: "root", Kind: "decision", Title: title,
		ReadWhen: "you study " + title, BodyMarkdown: "Body about " + title + ".", DecidedBy: "ana", DecidedAt: "2026-10-27"})
	if err != nil || res.Refusal != nil {
		t.Fatalf("write %q: %v %v", title, err, res.Refusal)
	}
	return res.EntryID
}

func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestHumanConfirmPromotesAlone(t *testing.T) {
	st, c := open(t)
	id := write(t, st, c, "s-1", "Ship weekly")
	tier, err := trail.ConfirmHuman(context.Background(), st.DB, c, id)
	if err != nil || tier != "verified" {
		t.Fatalf("tier %q err %v", tier, err)
	}
	var by string
	st.DB.QueryRow(`SELECT promoted_by FROM entries WHERE id = ?`, id).Scan(&by)
	if by != "user" {
		t.Errorf("promoted_by = %q", by)
	}
	if _, err := trail.ConfirmHuman(context.Background(), st.DB, c, "nope"); err == nil {
		t.Error("unknown entry confirmed")
	}
}

func TestSweepExpiresAndClosesWithInjectedClock(t *testing.T) {
	st, c := open(t)
	ctx := context.Background()
	now := t0
	clock := func() time.Time { return now }

	stale := write(t, st, c, "s-1", "Old and unread")
	readRecently := write(t, st, c, "s-1", "Old but read")
	fresh := write(t, st, c, "s-1", "Fresh")
	for id, age := range map[string]time.Duration{stale: 31 * 24 * time.Hour, readRecently: 40 * 24 * time.Hour, fresh: 2 * 24 * time.Hour} {
		exec(t, st.DB, `UPDATE entries SET created_at = ? WHERE id = ?`, ts(t0.Add(-age)), id)
	}
	exec(t, st.DB, `INSERT INTO events (session_id, at, tool, target, depth, result) VALUES ('s-9', ?, 'read', ?, 'full', 'ok')`, ts(t0.Add(-10*24*time.Hour)), readRecently)
	// The seeded, verified root entry is old and unread, and must stay.
	exec(t, st.DB, `UPDATE entries SET created_at = ? WHERE id = 'trail'`, ts(t0.Add(-90*24*time.Hour)))

	exec(t, st.DB, `INSERT INTO sessions (id, actor, task, task_type, started_at) VALUES ('idle', 'agent', 't', 't', ?), ('busy', 'agent', 't', 't', ?)`,
		ts(t0.Add(-9*time.Hour)), ts(t0.Add(-9*time.Hour)))
	exec(t, st.DB, `INSERT INTO events (session_id, at, tool) VALUES ('idle', ?, 'look'), ('busy', ?, 'look')`, ts(t0.Add(-5*time.Hour)), ts(t0.Add(-1*time.Hour)))

	// An anchored entry whose file changes goes stale in the same pass.
	file := filepath.Join(st.Repo, "a.txt")
	os.WriteFile(file, []byte("one\n"), 0o644)
	anchored, err := entries.Write(ctx, st.DB, c, entries.WriteRequest{Actor: "user", ParentSignID: "root", Kind: "summary", Title: "Anchored", ReadWhen: "you need the file",
		BodyMarkdown: "Why the file exists, not what is in it.", Anchors: []anchor.Anchor{{Type: "file", PathOrURL: "a.txt"}}})
	if err != nil || anchored.Refusal != nil {
		t.Fatalf("anchored write: %v %v", err, anchored.Refusal)
	}
	os.WriteFile(file, []byte("two\n"), 0o644)

	rep, err := trail.Sweep(ctx, st.DB, c, clock)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Dormant != 1 || rep.Deleted != 0 || rep.Closed != 1 {
		t.Errorf("first sweep = %+v", rep)
	}
	tier := func(id string) string {
		var s string
		if err := st.DB.QueryRow(`SELECT tier FROM entries WHERE id = ?`, id).Scan(&s); err != nil {
			return "deleted"
		}
		return s
	}
	for id, want := range map[string]string{stale: "dormant", readRecently: "provisional", fresh: "provisional", "trail": "verified", anchored.EntryID: "stale"} {
		if got := tier(id); got != want {
			t.Errorf("%s tier = %s, want %s", id, got, want)
		}
	}
	var endedBy sql.NullString
	st.DB.QueryRow(`SELECT ended_by FROM sessions WHERE id = 'idle'`).Scan(&endedBy)
	if endedBy.String != "timeout" {
		t.Errorf("idle session ended_by = %q", endedBy.String)
	}
	if count(t, st.DB, `SELECT COUNT(*) FROM sessions WHERE id = 'busy' AND ended_at IS NULL`) != 1 {
		t.Error("a session active an hour ago was closed")
	}

	// Twenty-nine days on, the dormant entry is still waiting; thirty-one and it goes.
	now = t0.Add(29 * 24 * time.Hour)
	if _, err := trail.Sweep(ctx, st.DB, c, clock); err != nil || tier(stale) != "dormant" {
		t.Fatalf("dormant entry after 29 days: %s %v", tier(stale), err)
	}
	now = t0.Add(31 * 24 * time.Hour)
	rep, err = trail.Sweep(ctx, st.DB, c, clock)
	if err != nil || tier(stale) != "deleted" || rep.Deleted < 1 {
		t.Fatalf("dormant entry after 31 days: %s %+v %v", tier(stale), rep, err)
	}
	if count(t, st.DB, `SELECT COUNT(*) FROM ids WHERE id = ?`, stale) != 1 || count(t, st.DB, `SELECT COUNT(*) FROM entries_fts WHERE entry_id = ?`, stale) != 0 {
		t.Error("deleted entry's id not recorded or still indexed")
	}
	for _, result := range []string{"dormant", "deleted"} {
		if count(t, st.DB, `SELECT COUNT(*) FROM events WHERE tool = 'sweep' AND target = ? AND result = ?`, stale, result) != 1 {
			t.Errorf("no %s event", result)
		}
	}
}

func TestFreedIDIsNotReused(t *testing.T) {
	st, c := open(t)
	ctx := context.Background()
	id := write(t, st, c, "s-1", "Ship weekly")
	exec(t, st.DB, `UPDATE entries SET created_at = ? WHERE id = ?`, ts(t0.Add(-100*24*time.Hour)), id)
	if _, err := trail.Sweep(ctx, st.DB, c, func() time.Time { return t0 }); err != nil {
		t.Fatal(err)
	}
	if _, err := trail.Sweep(ctx, st.DB, c, func() time.Time { return t0.Add(31 * 24 * time.Hour) }); err != nil {
		t.Fatal(err)
	}
	if count(t, st.DB, `SELECT COUNT(*) FROM entries WHERE id = ?`, id) != 0 {
		t.Fatal("entry was not deleted")
	}
	if again := write(t, st, c, "s-1", "Ship weekly"); again == id {
		t.Errorf("slug %q reused after expiry", id)
	}
}
