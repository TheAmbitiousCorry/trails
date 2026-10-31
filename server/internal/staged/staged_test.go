package staged_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/staged"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func put(t *testing.T, dir, rel, text string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", branch)
	put(t, dir, "a.txt", "a\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "init")
	return dir
}

func file(p string) anchor.Anchor { return anchor.Anchor{Type: "file", PathOrURL: p} }

var ctx = context.Background()

func TestBaseNameOrder(t *testing.T) {
	t.Run("main then master", func(t *testing.T) {
		if name, refs := staged.Base(ctx, repo(t, "master")); name != "master" || len(refs) != 1 {
			t.Errorf("base = %q %v", name, refs)
		}
		if name, _ := staged.Base(ctx, repo(t, "main")); name != "main" {
			t.Errorf("base = %q", name)
		}
	})
	t.Run("map.md line wins", func(t *testing.T) {
		dir := repo(t, "main")
		git(t, dir, "branch", "develop")
		put(t, dir, ".trail/map.md", "# Signs\n\nbase: develop\n")
		if name, refs := staged.Base(ctx, dir); name != "develop" || len(refs) != 1 {
			t.Errorf("base = %q %v", name, refs)
		}
	})
	t.Run("origin HEAD names the base, and remote refs count", func(t *testing.T) {
		origin := repo(t, "trunk")
		clone := filepath.Join(t.TempDir(), "c")
		if out, err := exec.Command("git", "clone", "-q", origin, clone).CombinedOutput(); err != nil {
			t.Fatalf("clone: %v\n%s", err, out)
		}
		git(t, clone, "branch", "-m", "trunk", "local")
		name, refs := staged.Base(ctx, clone)
		if name != "trunk" || len(refs) != 1 || refs[0] != "refs/remotes/origin/trunk" {
			t.Errorf("base = %q %v, want trunk on its remote ref alone", name, refs)
		}
	})
	t.Run("no base ref at all", func(t *testing.T) {
		if name, refs := staged.Base(ctx, repo(t, "trunk")); name != "" || len(refs) != 0 {
			t.Errorf("base = %q %v, want unresolved", name, refs)
		}
	})
}

func TestDecideOnBaseAndOutsideGit(t *testing.T) {
	dir := repo(t, "main")
	put(t, dir, "new.txt", "n\n")
	if d, err := staged.Decide(ctx, dir, []anchor.Anchor{file("new.txt")}); err != nil || d.Staged {
		t.Errorf("a write on the base branch = %+v, %v, want normal", d, err)
	}
	if d, err := staged.Decide(ctx, t.TempDir(), []anchor.Anchor{file("new.txt")}); err != nil || d.Staged {
		t.Errorf("a write outside a repository = %+v, %v, want normal", d, err)
	}
	if d, err := staged.Decide(ctx, "", nil); err != nil || d.Staged {
		t.Errorf("a write with no checkout = %+v, %v, want normal", d, err)
	}
}

func TestDecideNoBaseRefIsNormal(t *testing.T) {
	dir := repo(t, "trunk")
	git(t, dir, "checkout", "-q", "-b", "feat")
	put(t, dir, "new.txt", "n\n")
	if d, err := staged.Decide(ctx, dir, []anchor.Anchor{file("new.txt")}); err != nil || d.Staged {
		t.Errorf("decision = %+v, %v, want normal when no base ref exists", d, err)
	}
}

func TestDecideEdits(t *testing.T) {
	dir := repo(t, "main")
	git(t, dir, "checkout", "-q", "-b", "feat")
	main := git(t, dir, "rev-parse", "main")

	// An uncommitted edit to a tracked file, an untracked file and a renamed file are all changed.
	put(t, dir, "a.txt", "changed\n")
	put(t, dir, "fresh.txt", "f\n")
	for _, p := range []string{"a.txt", "fresh.txt"} {
		d, err := staged.Decide(ctx, dir, []anchor.Anchor{file(p)})
		if err != nil || !d.Staged || d.Branch != "feat" || d.BaseName != "main" || d.Base != main || d.Landing != "" {
			t.Errorf("%s: decision = %+v, %v", p, d, err)
		}
	}
	git(t, dir, "checkout", "-q", "--", "a.txt")
	git(t, dir, "mv", "a.txt", "b.txt")
	if d, _ := staged.Decide(ctx, dir, []anchor.Anchor{file("a.txt")}); !d.Staged {
		t.Error("the old path of a rename is not changed on the branch (--no-renames)")
	}

	// A range counts like a file; a url or transcript alone never stages.
	if d, _ := staged.Decide(ctx, dir, []anchor.Anchor{{Type: "range", PathOrURL: "b.txt", RangeStart: 1, RangeEnd: 1}}); !d.Staged {
		t.Error("a range anchor on a changed file did not stage")
	}
	if d, _ := staged.Decide(ctx, dir, []anchor.Anchor{{Type: "url", PathOrURL: "https://example.com"}}); d.Staged {
		t.Error("a url anchor staged")
	}
}

func TestDecideRefusals(t *testing.T) {
	dir := repo(t, "main")
	git(t, dir, "checkout", "-q", "-b", "feat")
	put(t, dir, "fresh.txt", "f\n")
	if _, err := staged.Decide(ctx, dir, []anchor.Anchor{file("fresh.txt"), {Type: "dir", PathOrURL: "."}}); !errors.Is(err, staged.ErrDirAnchor) {
		t.Errorf("dir anchor: err = %v", err)
	}
	git(t, dir, "checkout", "-q", "--detach")
	if _, err := staged.Decide(ctx, dir, []anchor.Anchor{file("fresh.txt")}); !errors.Is(err, staged.ErrDetached) {
		t.Errorf("detached: err = %v", err)
	}
	if d, err := staged.Decide(ctx, dir, []anchor.Anchor{file("a.txt")}); err != nil || d.Staged {
		t.Errorf("detached with nothing changed = %+v, %v, want normal", d, err)
	}
}

func TestLandingIsTheFirstCommitTouchingTheAnchoredPaths(t *testing.T) {
	dir := repo(t, "main")
	git(t, dir, "checkout", "-q", "-b", "feat")
	put(t, dir, "other.txt", "o\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "unrelated")
	put(t, dir, "x.txt", "1\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "adds x")
	first := git(t, dir, "rev-parse", "HEAD")
	put(t, dir, "x.txt", "2\n")
	git(t, dir, "commit", "-qam", "edits x")

	d, err := staged.Decide(ctx, dir, []anchor.Anchor{file("x.txt")})
	if err != nil || !d.Staged || d.Landing != first {
		t.Fatalf("decision = %+v, %v, want landing %s", d, err, first)
	}
	if staged.Merged(ctx, dir, first, "main") {
		t.Error("an unmerged landing commit reads as merged")
	}
	git(t, dir, "checkout", "-q", "main")
	git(t, dir, "merge", "-q", "--no-ff", "-m", "merge", "feat")
	if !staged.Merged(ctx, dir, first, "main") {
		t.Error("a merged landing commit does not read as merged")
	}
	if staged.Merged(ctx, dir, "", "main") {
		t.Error("an empty landing reads as merged")
	}
}

func TestPrunedCommitAnchorReadsFalseNotError(t *testing.T) {
	dir := repo(t, "main")
	git(t, dir, "checkout", "-q", "-b", "feat")
	d, err := staged.Decide(ctx, dir, []anchor.Anchor{{Type: "commit", Hash: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}})
	if err != nil || !d.Staged || d.Landing != "" {
		t.Errorf("decision = %+v, %v, want staged with no landing", d, err)
	}
}

func TestLocalBaseBehindItsRemoteUsesTheNearestMergeBase(t *testing.T) {
	origin := repo(t, "main")
	clone := filepath.Join(t.TempDir(), "c")
	if out, err := exec.Command("git", "clone", "-q", origin, clone).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	// origin/main moves on; the local main stays behind, and feat is cut from the remote tip.
	put(t, origin, "later.txt", "l\n")
	git(t, origin, "add", ".")
	git(t, origin, "commit", "-qm", "later")
	git(t, clone, "fetch", "-q")
	git(t, clone, "checkout", "-q", "-b", "feat", "origin/main")
	put(t, clone, "n.txt", "n\n")
	remoteTip := git(t, clone, "rev-parse", "origin/main")
	d, err := staged.Decide(ctx, clone, []anchor.Anchor{file("later.txt"), file("n.txt")})
	if err != nil || !d.Staged || d.Base != remoteTip {
		t.Fatalf("decision = %+v, %v, want base %s", d, err, remoteTip)
	}
	// later.txt arrived on origin/main before the branch was cut, so it alone is not branch work.
	if d, _ := staged.Decide(ctx, clone, []anchor.Anchor{file("later.txt")}); d.Staged {
		t.Error("a file already on the remote base staged")
	}
}
