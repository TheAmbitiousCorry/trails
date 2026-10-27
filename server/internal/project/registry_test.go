package project_test

import (
	"context"
	"os"
	"path/filepath"
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

func TestSameIDMovesTheProject(t *testing.T) {
	home := t.TempDir()
	reg := project.NewRegistry(home, 0)
	defer reg.Close()
	first, second := repoWithID(t, "shared"), repoWithID(t, "shared")
	p1, err := reg.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := reg.Open(second)
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
	if strings.TrimSpace(string(b)) != second {
		t.Errorf("repo file = %q", b)
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
