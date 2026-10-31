package project_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/project"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

func TestViewerVisibilityAndHiddenOriginals(t *testing.T) {
	reg := project.NewRegistry(t.TempDir(), 0)
	defer reg.Close()
	main, linked := gitWorktrees(t, "vis")
	p, err := reg.Open(main)
	if err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// linked is on branch "wt" cut from the initial commit; add a commit there that a later branch contains.
	git(linked, "commit", "-q", "--allow-empty", "-m", "landing")
	landing := git(linked, "rev-parse", "HEAD")

	add := func(id, branch, landing, over string) {
		now := store.Now()
		if _, err := p.DB().Exec(`INSERT INTO entries (id, sign_id, kind, title, read_when, body_json, body_schema, tier, created_at, updated_at, staged_branch, staged_landing, stages_over)
			VALUES (?, 'root', 'decision', ?, 'x', '{"type":"doc","content":[]}', 1, 'provisional', ?, ?, ?, NULLIF(?,''), NULLIF(?,''))`, id, id, now, now, branch, landing, over); err != nil {
			t.Fatal(err)
		}
	}
	add("on-wt", "wt", "", "trail")
	add("landed", "elsewhere", landing, "")
	add("uncommitted", "elsewhere", "", "")

	ctx := context.Background()
	onBranch := p.Viewer(ctx, linked)
	if !onBranch.Sees("on-wt") || !onBranch.Sees("landed") || onBranch.Sees("uncommitted") {
		t.Errorf("session on the entry's branch: %+v", onBranch)
	}
	if onBranch.Sees("trail") {
		t.Error("the original the visible staged entry stages over was not hidden")
	}

	// The primary is on the base branch with the landing commit absent.
	base := p.Viewer(ctx, main)
	if base.Sees("on-wt") || base.Sees("landed") || base.Sees("uncommitted") || !base.Sees("trail") {
		t.Errorf("base session: %+v", base)
	}
	if b := p.BaseViewer(ctx); b.Sees("on-wt") || b.Sees("landed") || !b.Sees("trail") {
		t.Errorf("BaseViewer: %+v", b)
	}

	// A detached checkout at the landing commit sees the landed entry by ancestry, and no entry by branch name.
	git(linked, "checkout", "-q", "--detach", landing)
	detached := p.Viewer(ctx, linked)
	if detached.Branch != "" || !detached.Sees("landed") || detached.Sees("on-wt") {
		t.Errorf("detached at the landing commit: %+v", detached)
	}
}
