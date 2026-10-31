// Package staged decides whether a write is about work its branch has changed since it left the base branch, and finds the commit that work landed in.
// Every git call runs as git -C <checkout toplevel> through gitx, and an unknown object or ref reads as false.
package staged

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/gitx"
)

var (
	// ErrDetached is returned for a write about branch work from a detached HEAD, which has no branch to hold the entry.
	ErrDetached = errors.New("detached HEAD")
	// ErrDirAnchor is returned for a staged write that anchors a directory: its hash walks the working tree and cannot be reproduced from git objects.
	ErrDirAnchor = errors.New("directory anchor on a staged write")
)

// Decision is the outcome for one write. Only Staged writes carry the rest.
type Decision struct {
	Staged   bool
	Branch   string
	BaseName string
	// Base is the merge-base with the base branch when the write happened.
	Base string
	// Landing is the first commit on the branch touching the entry's anchored paths; empty while the work is uncommitted.
	Landing string
}

var baseLine = regexp.MustCompile(`(?m)^base:[ \t]*(\S+)[ \t]*$`)

// Base resolves the base branch name and the base refs that exist for it: a `base:` line in .trail/map.md, else the short name of refs/remotes/origin/HEAD, else main, else master.
// refs is empty when no base ref exists at all.
func Base(ctx context.Context, top string) (name string, refs []string) {
	if b, err := os.ReadFile(filepath.Join(top, ".trail", "map.md")); err == nil {
		if m := baseLine.FindSubmatch(b); m != nil {
			return string(m[1]), BaseRefs(ctx, top, string(m[1]))
		}
	}
	if short, ok, _ := gitx.Out(ctx, top, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); ok {
		if _, n, found := strings.Cut(short, "/"); found {
			if refs := BaseRefs(ctx, top, n); len(refs) > 0 {
				return n, refs
			}
		}
	}
	for _, n := range []string{"main", "master"} {
		if refs := BaseRefs(ctx, top, n); len(refs) > 0 {
			return n, refs
		}
	}
	return "", nil
}

// BaseRefs lists refs/heads/<name> and refs/remotes/<remote>/<name> that exist. A test against the base passes when it passes against any of them.
func BaseRefs(ctx context.Context, top, name string) []string {
	if name == "" {
		return nil
	}
	var refs []string
	if _, ok := gitx.Verify(ctx, top, "refs/heads/"+name+"^{commit}"); ok {
		refs = append(refs, "refs/heads/"+name)
	}
	out, ok, _ := gitx.Out(ctx, top, "for-each-ref", "--format=%(refname)", "refs/remotes/")
	if !ok {
		return refs
	}
	for _, r := range gitx.Lines(out) {
		rest := strings.TrimPrefix(r, "refs/remotes/")
		if _, branch, found := strings.Cut(rest, "/"); found && branch == name {
			refs = append(refs, r)
		}
	}
	return refs
}

// AncestorOfBase reports whether sha is an ancestor of any base ref.
func AncestorOfBase(ctx context.Context, top, sha string, refs []string) bool {
	for _, r := range refs {
		if ok, _ := gitx.IsAncestor(ctx, top, sha, r); ok {
			return true
		}
	}
	return false
}

// mergeBase is the merge-base of tip with the base refs; with several refs the one nearest the tip wins, so a local base behind its remote does not widen the branch's work.
func mergeBase(ctx context.Context, top, tip string, refs []string) (string, bool) {
	best, bestCount := "", -1
	for _, r := range refs {
		sha, ok, _ := gitx.Run(ctx, top, []int{1}, "merge-base", tip, r)
		if !ok || sha == "" {
			continue
		}
		count := 0
		if len(refs) > 1 {
			if n, ok, _ := gitx.Out(ctx, top, "rev-list", "--count", sha+".."+tip); ok {
				count, _ = strconv.Atoi(n)
			}
		}
		if bestCount < 0 || count < bestCount {
			best, bestCount = sha, count
		}
	}
	return best, best != ""
}

// changedPaths is every path the checkout changes against m: tracked files differing in the working tree (uncommitted edits included) and untracked files not ignored.
func changedPaths(ctx context.Context, top, m string) map[string]bool {
	set := map[string]bool{}
	if out, ok, _ := gitx.Out(ctx, top, "diff", "--no-renames", "--name-only", "-z", m); ok {
		for _, p := range strings.Split(out, "\x00") {
			if p != "" {
				set[p] = true
			}
		}
	}
	if out, ok, _ := gitx.Out(ctx, top, "ls-files", "--others", "--exclude-standard", "--full-name", "-z"); ok {
		for _, p := range strings.Split(out, "\x00") {
			if p != "" {
				set[p] = true
			}
		}
	}
	return set
}

// anchoredPaths are the file and range anchor paths, cleaned.
func anchoredPaths(as []anchor.Anchor) []string {
	var out []string
	seen := map[string]bool{}
	for _, a := range as {
		if a.Type != "file" && a.Type != "range" {
			continue
		}
		p := path.Clean(filepath.ToSlash(a.PathOrURL))
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// Decide settles whether a write from the checkout at top, anchoring as, is staged.
// It is staged when the checkout is on a branch other than the base and at least one file or range anchor is changed on the branch, or a commit anchor is not on the base.
// A detached HEAD with a changed anchor is ErrDetached, and a staged write that anchors a directory is ErrDirAnchor. Anything else, including no base ref at all, is a normal write.
func Decide(ctx context.Context, top string, as []anchor.Anchor) (Decision, error) {
	return decide(ctx, top, as, nil, false)
}

// DecideUpdate is Decide for an update: the entry's anchors for the changed-on-branch test are the union of the request's and the original's, so re-anchoring away from a file the branch changed cannot hide the update from base.
// Only the request's anchors can be refused as a directory, since only they are stored on the copy.
func DecideUpdate(ctx context.Context, top string, as, original []anchor.Anchor) (Decision, error) {
	return decide(ctx, top, as, original, false)
}

// DecideFor is DecideUpdate with the write's staged flag: an entry with no anchors at all, which nothing could show as changed on the branch, is staged when force is set and the checkout is on a branch other than the base.
// The flag changes nothing for an entry that has anchors.
func DecideFor(ctx context.Context, top string, as, original []anchor.Anchor, force bool) (Decision, error) {
	return decide(ctx, top, as, original, force)
}

func decide(ctx context.Context, top string, as, original []anchor.Anchor, force bool) (Decision, error) {
	if top == "" {
		return Decision{}, nil
	}
	name, refs := Base(ctx, top)
	if len(refs) == 0 {
		return Decision{}, nil
	}
	branch, onBranch, _ := gitx.Out(ctx, top, "symbolic-ref", "--short", "HEAD")
	if onBranch && branch == name {
		return Decision{}, nil
	}
	if _, ok := gitx.Verify(ctx, top, "HEAD^{commit}"); !ok {
		return Decision{}, nil
	}
	m, ok := mergeBase(ctx, top, "HEAD", refs)
	if !ok {
		return Decision{}, nil
	}
	union := append(append([]anchor.Anchor{}, as...), original...)
	if !changedOnBranch(ctx, top, m, refs, union) && !(force && len(union) == 0) {
		return Decision{}, nil
	}
	if !onBranch {
		return Decision{}, ErrDetached
	}
	for _, a := range as {
		if a.Type == "dir" {
			return Decision{}, ErrDirAnchor
		}
	}
	return Decision{Staged: true, Branch: branch, BaseName: name, Base: m, Landing: Landing(ctx, top, m, "HEAD", union)}, nil
}

// changedOnBranch applies the changed-on-branch predicate to the anchors.
func changedOnBranch(ctx context.Context, top, m string, refs []string, as []anchor.Anchor) bool {
	var changed map[string]bool
	for _, a := range as {
		switch a.Type {
		case "file", "range":
			if changed == nil {
				changed = changedPaths(ctx, top, m)
			}
			if changed[path.Clean(filepath.ToSlash(a.PathOrURL))] {
				return true
			}
		case "commit":
			if !AncestorOfBase(ctx, top, a.Hash, refs) {
				return true
			}
		}
	}
	return false
}

// Landing is the first commit in m..tip touching any file or range path in as, else the earliest commit anchor that lies on the branch. It is empty while the work is uncommitted.
// With no anchors at all it is the first commit in m..tip, since nothing narrows the branch's work.
func Landing(ctx context.Context, top, m, tip string, as []anchor.Anchor) string {
	if len(as) == 0 {
		if out, ok, _ := gitx.Out(ctx, top, "log", "--reverse", "--format=%H", m+".."+tip); ok {
			if lines := gitx.Lines(out); len(lines) > 0 {
				return lines[0]
			}
		}
		return ""
	}
	var byPath string
	if paths := anchoredPaths(as); len(paths) > 0 {
		args := []string{"log", "--reverse", "--no-renames", "--format=%H", m + ".." + tip, "--"}
		for _, p := range paths {
			args = append(args, ":(literal)"+p)
		}
		if out, ok, _ := gitx.Out(ctx, top, args...); ok {
			if lines := gitx.Lines(out); len(lines) > 0 {
				byPath = lines[0]
			}
		}
	}
	byCommit := ""
	for _, a := range as {
		if a.Type != "commit" {
			continue
		}
		full, ok := gitx.Verify(ctx, top, a.Hash+"^{commit}")
		if !ok {
			continue
		}
		onBranch, _ := gitx.IsAncestor(ctx, top, full, tip)
		beforeBranch, _ := gitx.IsAncestor(ctx, top, full, m)
		if !onBranch || beforeBranch {
			continue
		}
		if byCommit == "" {
			byCommit = full
		} else if first, _ := gitx.IsAncestor(ctx, top, full, byCommit); first {
			byCommit = full
		}
	}
	switch {
	case byPath == "":
		return byCommit
	case byCommit == "":
		return byPath
	}
	if first, _ := gitx.IsAncestor(ctx, top, byCommit, byPath); first {
		return byCommit
	}
	return byPath
}

// Merged reports whether the pinned landing commit is an ancestor of any base ref of the named base branch.
func Merged(ctx context.Context, top, landing, baseName string) bool {
	return landing != "" && AncestorOfBase(ctx, top, landing, BaseRefs(ctx, top, baseName))
}

// LandedByContent reports whether the work reached the base branch without its commits (a squash, rebase or cherry-pick): the entry has at least one file or range anchor,
// on a single base ref every one of them names a path that exists with the stored hash, and at least one of those paths holds different content there than at the merge-base m.
// That last test keeps an entry whose anchors the branch never changed (a copy re-anchored to an untouched file) from reading as landed the moment it is written. Anchors of other types do not count either way.
func LandedByContent(ctx context.Context, c anchor.Checker, refs []string, m string, as []anchor.Anchor) bool {
	var files []anchor.Anchor
	for _, a := range as {
		if a.Type == "file" || a.Type == "range" {
			files = append(files, a)
		}
	}
	if len(files) == 0 || m == "" {
		return false
	}
refs:
	for _, r := range refs {
		on := c.OnRef(r)
		changed := false
		for _, a := range files {
			if ok, _ := on.Check(ctx, a); !ok {
				continue refs
			}
			p := path.Clean(filepath.ToSlash(a.PathOrURL))
			now, _ := gitx.Verify(ctx, c.Repo, r+":"+p)
			then, _ := gitx.Verify(ctx, c.Repo, m+":"+p)
			changed = changed || now != then
		}
		if changed {
			return true
		}
	}
	return false
}
