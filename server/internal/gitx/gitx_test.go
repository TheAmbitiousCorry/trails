package gitx_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/gitx"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "one")
	return dir
}

func TestUnknownObjectReadsFalseNotError(t *testing.T) {
	dir := repo(t)
	out, ok, err := gitx.Out(context.Background(), dir, "cat-file", "-t", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if err != nil || ok || out != "" {
		t.Fatalf("pruned sha: out=%q ok=%v err=%v, want empty, false, nil", out, ok, err)
	}
	if ok, err := gitx.IsAncestor(context.Background(), dir, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "HEAD"); ok || err != nil {
		t.Fatalf("IsAncestor of an unknown sha = %v, %v, want false, nil", ok, err)
	}
	_, ok, err = gitx.Out(context.Background(), t.TempDir(), "rev-parse", "HEAD")
	if ok || err != nil {
		t.Fatalf("outside a repository: ok=%v err=%v, want false, nil", ok, err)
	}
}

func TestIsAncestor(t *testing.T) {
	dir := repo(t)
	first := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "two")
	second := git(t, dir, "rev-parse", "HEAD")
	ctx := context.Background()
	if ok, err := gitx.IsAncestor(ctx, dir, first, second); !ok || err != nil {
		t.Errorf("first of second = %v, %v", ok, err)
	}
	if ok, err := gitx.IsAncestor(ctx, dir, second, first); ok || err != nil {
		t.Errorf("second of first = %v, %v", ok, err)
	}
}

func TestOtherExitCodesStayErrors(t *testing.T) {
	dir := repo(t)
	if _, _, err := gitx.Out(context.Background(), dir, "config", "--get", "no.such.key"); err == nil {
		t.Fatal("exit 1 from git config was swallowed")
	}
}
