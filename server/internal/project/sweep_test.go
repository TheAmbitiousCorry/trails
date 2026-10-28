package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSweepDropsMissingPaths(t *testing.T) {
	home := t.TempDir()
	reg := NewRegistry(home, 0)
	defer reg.Close()
	main := t.TempDir()
	if err := os.MkdirAll(filepath.Join(main, ".trail"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, ".trail", "id"), []byte("sw\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "wt")
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init"}, {"worktree", "add", "-q", linked}} {
		if out, err := exec.Command("git", append([]string{"-C", main}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	reg.Open(main)
	p, err := reg.Open(linked)
	if err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(linked)
	reg.dropMissingPaths(p)
	if !slices.Equal(p.Paths(), []string{main}) {
		t.Errorf("paths = %v", p.Paths())
	}
	b, _ := os.ReadFile(filepath.Join(home, "sw", "repo"))
	if strings.TrimSpace(string(b)) != main {
		t.Errorf("repo file = %q", b)
	}
}
