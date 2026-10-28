package project_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/project"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

func repoWithID(t *testing.T, id string) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".trail"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".trail", "id"), []byte(id+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestSameIDMoveForm(t *testing.T) {
	home := t.TempDir()
	reg := project.NewRegistry(home, 0)
	defer reg.Close()
	first, second := repoWithID(t, "shared"), repoWithID(t, "shared")
	p1, err := reg.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := reg.OpenMove(second)
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 || len(reg.List()) != 1 {
		t.Fatalf("same id gave %d projects", len(reg.List()))
	}
	if p1.Repo() != second || p1.Checker().Repo != second {
		t.Errorf("repo = %q, want the later open %q", p1.Repo(), second)
	}
	b, _ := os.ReadFile(filepath.Join(home, "shared", "repo"))
	if string(b) != second+"\n"+first+"\n" {
		t.Errorf("repo file = %q", b)
	}
}

func TestSecondRepoWithSameIDIsRefused(t *testing.T) {
	home := t.TempDir()
	reg := project.NewRegistry(home, 0)
	defer reg.Close()
	first, second := repoWithID(t, "shared"), repoWithID(t, "shared")
	if _, err := reg.Open(first); err != nil {
		t.Fatal(err)
	}
	_, err := reg.Open(second)
	var bound *project.IDBound
	if !errors.As(err, &bound) || bound.ID != "shared" || bound.Bound != first || bound.Repo != second {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "trails register --move") {
		t.Errorf("message lacks the fix: %v", err)
	}
	if p, _ := reg.ByID("shared"); p.Repo() != first {
		t.Errorf("repo = %q after refusal", p.Repo())
	}
	reg.Close()
	reg2 := project.NewRegistry(home, 0)
	defer reg2.Close()
	if _, err := reg2.Open(second); !errors.As(err, &bound) {
		t.Errorf("refusal from the repo file alone: %v", err)
	}
}

func TestBoundPathGoneAllowsTheMove(t *testing.T) {
	home := t.TempDir()
	reg := project.NewRegistry(home, 0)
	defer reg.Close()
	first, second := repoWithID(t, "shared"), repoWithID(t, "shared")
	if _, err := reg.Open(first); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(first)
	p, err := reg.Open(second)
	if err != nil {
		t.Fatal(err)
	}
	if p.Repo() != second {
		t.Errorf("repo = %q, want %q", p.Repo(), second)
	}
}

func TestOpenKnownSkipsAMovedRepo(t *testing.T) {
	home := t.TempDir()
	reg := project.NewRegistry(home, 0)
	keep, gone := repoWithID(t, "keep"), repoWithID(t, "gone")
	for _, r := range []string{keep, gone} {
		if _, err := reg.Open(r); err != nil {
			t.Fatal(err)
		}
	}
	reg.Close()
	os.RemoveAll(gone)

	reg2 := project.NewRegistry(home, 0)
	defer reg2.Close()
	reg2.OpenKnown()
	if ps := reg2.List(); len(ps) != 1 || ps[0].ID != "keep" {
		t.Errorf("reopened %d projects", len(ps))
	}
}

func TestForStartUsesTheRegisteredRuntime(t *testing.T) {
	ctx := context.Background()
	reg := project.NewRegistry(t.TempDir(), 0)
	defer reg.Close()
	pa, _ := reg.Open(repoWithID(t, "aaa"))
	pb, _ := reg.Open(repoWithID(t, "bbb"))
	start := trail.Call{Mint: true, Runtime: "rt-9"}
	_, _, err := reg.Resolve(ctx, start)
	var ref *trail.Refusal
	if !errors.As(err, &ref) || ref.Rule != "project_unknown" || !strings.Contains(ref.Fix, "runtime_session_id") {
		t.Fatalf("unregistered runtime = %v", err)
	}
	if want := "this server holds: aaa (" + filepath.Base(pa.Repo()) + "), bbb (" + filepath.Base(pb.Repo()) + ")"; !strings.Contains(ref.Fix, want) {
		t.Errorf("fix = %q, want it to contain %q", ref.Fix, want)
	}
	reg.RememberRuntime("rt-9", pb.ID)
	if p, _, err := reg.Resolve(ctx, start); err != nil || p != pb {
		t.Errorf("registered runtime = %v %v, want bbb", p, err)
	}
	if p, _, err := reg.Resolve(ctx, trail.Call{Mint: true, Project: "aaa", Runtime: "rt-9"}); err != nil || p.ID != "aaa" {
		t.Errorf("explicit project must win, got %v %v", p, err)
	}
}

func TestForStartRuntimeMappedToSeveralProjects(t *testing.T) {
	ctx := context.Background()
	reg := project.NewRegistry(t.TempDir(), 0)
	defer reg.Close()
	repoA, repoB := repoWithID(t, "aaa"), repoWithID(t, "bbb")
	reg.Open(repoA)
	reg.Open(repoB)
	start := trail.Call{Mint: true, Runtime: "rt-m"}
	reg.RememberRuntime("rt-m", "aaa")
	if p, _, err := reg.Resolve(ctx, start); err != nil || p.ID != "aaa" {
		t.Errorf("one mapped project = %v %v, want aaa", p, err)
	}
	reg.RememberRuntime("rt-m", "bbb")
	reg.RememberRuntime("rt-m", "aaa")
	reg.RememberRuntime("rt-m", "aaa")
	_, _, err := reg.Resolve(ctx, start)
	var ref *trail.Refusal
	want := "pass project, one of: aaa (" + filepath.Base(repoA) + "), bbb (" + filepath.Base(repoB) + ")"
	if !errors.As(err, &ref) || ref.Rule != "project_unknown" || ref.Fix != want {
		t.Errorf("two mapped projects = %v, want fix %q", err, want)
	}
	if p, _, err := reg.Resolve(ctx, trail.Call{Mint: true, Project: "bbb", Runtime: "rt-m"}); err != nil || p.ID != "bbb" {
		t.Errorf("explicit project must win, got %v %v", p, err)
	}
}

func TestSoleAndRuntimeLookup(t *testing.T) {
	ctx := context.Background()
	reg := project.NewRegistry(t.TempDir(), 0)
	defer reg.Close()
	if _, err := reg.Sole(); err != project.ErrNone {
		t.Errorf("empty Sole = %v", err)
	}
	pa, _ := reg.Open(repoWithID(t, "aaa"))
	if p, err := reg.Sole(); err != nil || p != pa {
		t.Errorf("one project Sole = %v", err)
	}
	pb, _ := reg.Open(repoWithID(t, "bbb"))
	if _, err := reg.Sole(); err != project.ErrAmbiguous {
		t.Errorf("two project Sole = %v", err)
	}

	if err := trail.StartSession(ctx, pa.DB(), "s-a", "agent", "task", "", "rt-1"); err != nil {
		t.Fatal(err)
	}
	if err := trail.StartSession(ctx, pb.DB(), "s-b", "agent", "task", "", "rt-1"); err != nil {
		t.Fatal(err)
	}
	p, sid, ok, err := reg.ByRuntime(ctx, "rt-1")
	if err != nil || !ok || p != pb || sid != "s-b" {
		t.Errorf("ByRuntime = %v %q %v %v, want the later session in bbb", p, sid, ok, err)
	}
	// The project that answered stays the answer while its session is open, and the search resumes when it closes.
	if p, _, _, _ := reg.ByRuntime(ctx, "rt-1"); p != pb {
		t.Error("cached runtime id lost its project")
	}
	if _, err := pb.DB().Exec(`UPDATE sessions SET ended_at = '2099-01-01' WHERE id = 's-b'`); err != nil {
		t.Fatal(err)
	}
	if p, sid, ok, _ := reg.ByRuntime(ctx, "rt-1"); !ok || p != pa || sid != "s-a" {
		t.Errorf("after s-b closed ByRuntime = %v %q %v, want s-a in aaa", p, sid, ok)
	}
	if _, _, ok, _ := reg.ByRuntime(ctx, "rt-none"); ok {
		t.Error("ByRuntime found an unknown runtime id")
	}
	if p, ok, _ := reg.BySession(ctx, "s-a"); !ok || p != pa {
		t.Error("BySession missed s-a")
	}
	if _, ok, _ := reg.BySession(ctx, "s-none"); ok {
		t.Error("BySession found an unknown id")
	}
}

func gitWorktrees(t *testing.T, id string) (main, linked string) {
	t.Helper()
	main = repoWithID(t, id)
	linked = filepath.Join(t.TempDir(), "wt")
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init"}, {"worktree", "add", "-q", linked}} {
		if out, err := exec.Command("git", append([]string{"-C", main}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return main, linked
}

func TestWorktreeOfTheSameRepositoryAttaches(t *testing.T) {
	home := t.TempDir()
	reg := project.NewRegistry(home, 0)
	defer reg.Close()
	main, linked := gitWorktrees(t, "wt")
	p, err := reg.Open(main)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := reg.Open(linked)
	if err != nil {
		t.Fatal(err)
	}
	if p != p2 || !slices.Equal(p.Paths(), []string{main, linked}) || p.Repo() != main {
		t.Fatalf("paths = %v, repo = %q", p.Paths(), p.Repo())
	}
	b, _ := os.ReadFile(filepath.Join(home, "wt", "repo"))
	if string(b) != main+"\n"+linked+"\n" {
		t.Errorf("repo file = %q", b)
	}
	reg.Close()
	reg2 := project.NewRegistry(home, 0)
	defer reg2.Close()
	reg2.OpenKnown()
	if q, ok := reg2.ByID("wt"); !ok || !slices.Equal(q.Paths(), []string{main, linked}) {
		t.Errorf("restored paths = %v", q)
	}
}

func TestCopiedIDInAnotherRepositoryIsRefused(t *testing.T) {
	reg := project.NewRegistry(t.TempDir(), 0)
	defer reg.Close()
	main, _ := gitWorktrees(t, "dup")
	other, _ := gitWorktrees(t, "dup")
	if _, err := reg.Open(main); err != nil {
		t.Fatal(err)
	}
	var bound *project.IDBound
	if _, err := reg.Open(other); !errors.As(err, &bound) || bound.Bound != main {
		t.Fatalf("err = %v", err)
	}
}

func TestMoveMakesTheSecondCheckoutPrimary(t *testing.T) {
	reg := project.NewRegistry(t.TempDir(), 0)
	defer reg.Close()
	main, linked := gitWorktrees(t, "mv")
	if _, err := reg.Open(main); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Open(linked); err != nil {
		t.Fatal(err)
	}
	p, err := reg.OpenMove(linked)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.Paths(), []string{linked, main}) || p.Repo() != linked {
		t.Errorf("paths = %v", p.Paths())
	}
}
