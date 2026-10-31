package trail_test

import (
	"context"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/entries"
	"github.com/TheAmbitiousCorry/trails/internal/store"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

const longAgo = "2000-01-01T00:00:00Z"

func age(t *testing.T, st *store.Store, ids ...string) {
	t.Helper()
	for _, id := range ids {
		exec(t, st.DB, `UPDATE entries SET updated_at = ? WHERE id = ?`, longAgo, id)
	}
}

func moved(t *testing.T, st *store.Store, what, id string) {
	t.Helper()
	var at string
	if err := st.DB.QueryRow(`SELECT updated_at FROM entries WHERE id = ?`, id).Scan(&at); err != nil {
		t.Fatal(err)
	}
	if at == longAgo {
		t.Errorf("%s left updated_at of %s where it was; the edit token would not move", what, id)
	}
}

func TestConfirmsMoveUpdatedAt(t *testing.T) {
	ctx := context.Background()
	st, c := open(t)

	human := write(t, st, c, "s-1", "Human confirmed")
	age(t, st, human)
	if _, err := trail.ConfirmHuman(ctx, st.DB, c, human); err != nil {
		t.Fatal(err)
	}
	moved(t, st, "ConfirmHuman", human)

	agents := write(t, st, c, "s-1", "Agents confirmed")
	for _, s := range []string{"s-2", "s-3"} {
		exec(t, st.DB, `INSERT INTO events (session_id, at, tool, target, depth, result) VALUES (?, ?, 'read', ?, 'full', 'ok')`, s, ts(t0), agents)
	}
	exec(t, st.DB, `INSERT INTO events (session_id, at, tool, target, result) VALUES ('s-2', ?, 'confirm', ?, 'ok')`, ts(t0), agents)
	age(t, st, agents)
	res, err := trail.Confirm(ctx, st.DB, c, "s-3", agents)
	if err != nil || res.Tier != "verified" {
		t.Fatalf("second confirm = %+v %v", res, err)
	}
	moved(t, st, "agent promotion", agents)
}

func TestSweepMovesUpdatedAtOnATierChange(t *testing.T) {
	ctx := context.Background()
	st, c := open(t)
	clock := func() time.Time { return t0 }

	unread := write(t, st, c, "s-1", "Old and unread")
	exec(t, st.DB, `UPDATE entries SET created_at = ? WHERE id = ?`, ts(t0.Add(-31*24*time.Hour)), unread)
	file := filepath.Join(st.Repo, "a.txt")
	os.WriteFile(file, []byte("one\n"), 0o644)
	res, err := entries.Write(ctx, st.DB, c, entries.WriteRequest{Actor: "user", ParentSignID: "root", Kind: "summary", Title: "Anchored", ReadWhen: "you need the file",
		BodyMarkdown: "Why the file exists, not what is in it.", Anchors: []anchor.Anchor{{Type: "file", PathOrURL: "a.txt"}}})
	if err != nil || res.Refusal != nil {
		t.Fatalf("anchored write: %v %v", err, res.Refusal)
	}
	os.WriteFile(file, []byte("two\n"), 0o644)
	age(t, st, unread, res.EntryID)

	if _, err := trail.Sweep(ctx, st.DB, c, clock); err != nil {
		t.Fatal(err)
	}
	moved(t, st, "the sweep making an entry dormant", unread)
	moved(t, st, "the sweep making an entry stale", res.EntryID)

	// A pass that finds the same state changes nothing, so a viewer's token is not moved by every read.
	age(t, st, res.EntryID)
	if _, err := trail.Sweep(ctx, st.DB, c, clock); err != nil {
		t.Fatal(err)
	}
	var at string
	st.DB.QueryRow(`SELECT updated_at FROM entries WHERE id = ?`, res.EntryID).Scan(&at)
	if at != longAgo {
		t.Errorf("a sweep that changed nothing moved updated_at to %s", at)
	}

	os.WriteFile(file, []byte("one\n"), 0o644)
	if _, err := trail.Sweep(ctx, st.DB, c, clock); err != nil {
		t.Fatal(err)
	}
	moved(t, st, "the sweep restoring a stale entry", res.EntryID)
}

func gitRun(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := osexec.Command("git", append([]string{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestStagedPromotionAndAbandonMoveUpdatedAt(t *testing.T) {
	ctx := context.Background()
	st, c := open(t)
	clock := func() time.Time { return t0 }
	gitRun(t, st.Repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(st.Repo, "a.txt"), []byte("one\n"), 0o644)
	gitRun(t, st.Repo, "add", ".")
	gitRun(t, st.Repo, "commit", "-q", "-m", "first")

	orig := write(t, st, c, "s-1", "Original note")
	copyID := write(t, st, c, "s-2", "Copy on a branch")
	exec(t, st.DB, `UPDATE entries SET staged_branch = 'feat', staged_base_name = 'main', stages_over = ?, stages_over_version = 1 WHERE id = ?`, orig, copyID)
	age(t, st, orig, copyID)
	if err := trail.PromoteByHand(ctx, st.DB, c, copyID, clock); err != nil {
		t.Fatal(err)
	}
	moved(t, st, "promoting a staged entry", copyID)
	moved(t, st, "a promoted copy retiring its original", orig)

	gone := write(t, st, c, "s-2", "Branch that vanished")
	exec(t, st.DB, `UPDATE entries SET staged_branch = 'deleted-branch', staged_base_name = 'main' WHERE id = ?`, gone)
	age(t, st, gone)
	if _, err := trail.Sweep(ctx, st.DB, c, clock); err != nil {
		t.Fatal(err)
	}
	var tier string
	st.DB.QueryRow(`SELECT tier FROM entries WHERE id = ?`, gone).Scan(&tier)
	if tier != "dormant" {
		t.Fatalf("the sweep did not abandon the entry: tier %s", tier)
	}
	moved(t, st, "abandoning a staged entry", gone)
}

func TestResolveGapMergeMovesUpdatedAt(t *testing.T) {
	ctx := context.Background()
	st, c := open(t)
	id := write(t, st, c, "s-1", "Gap target")
	exec(t, st.DB, `INSERT INTO sessions (id, actor, task, task_type, started_at) VALUES ('s-9', 'agent', 't', 't', ?)`, ts(t0))
	exec(t, st.DB, `INSERT INTO gaps (id, session_id, query, state, opened_at) VALUES (1, 's-9', 'pennies disagree with the ledger', 'open', ?)`, ts(t0))
	age(t, st, id)
	if err := trail.ResolveGap(ctx, st.DB, "s-9", trail.Resolution{GapID: 1, Action: "merge", TargetID: id}); err != nil {
		t.Fatal(err)
	}
	moved(t, st, "resolve_gap merge (alias)", id)
}
