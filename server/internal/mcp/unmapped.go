package mcp

import (
	"bytes"
	"context"
	"os/exec"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/gitx"
	"github.com/TheAmbitiousCorry/trails/internal/project"
	"github.com/TheAmbitiousCorry/trails/internal/search"
	"github.com/TheAmbitiousCorry/trails/internal/view"
)

// coverageUnmapped is a directory (or, when Loose, the files directly in a directory) of tracked files no live entry covers.
// Files is the item's non-excluded files and Unmapped the uncovered ones; for a loose item both count the uncovered files directly in the directory.
type coverageUnmapped struct {
	Path       string   `json:"path"`
	Loose      bool     `json:"loose,omitempty"`
	Files      int      `json:"files"`
	Unmapped   int      `json:"unmapped"`
	Commits90d int      `json:"commits_90d"`
	Names      []string `json:"names,omitempty"`
	Sign       string   `json:"sign"`
}

var lockfiles = map[string]bool{"go.sum": true, "package-lock.json": true, "pnpm-lock.yaml": true, "yarn.lock": true, "Cargo.lock": true, "composer.lock": true, "poetry.lock": true}

const maxNames = 5

// gitFacts are the expensive git answers an unmapped call needs, which depend on the checkout's HEAD and the date, not on the entries.
// Caching them (the plan's per-HEAD cache, narrowed: the list itself depends on the entries, which change between calls) brings a call on a few thousand files under the budget.
type gitFacts struct {
	head, day string
	days      int
	excluded  map[string]bool // binary, linguist-generated or linguist-vendored, of the files tracked at HEAD
	commits   [][]string      // the files of each commit in the window
}

var (
	factsMu    sync.Mutex
	factsCache = map[string]*gitFacts{}
)

// factsFor returns the cached facts for the checkout's HEAD today, or computes them. A files list the cache has not seen (uncommitted additions) is treated as not excluded.
func factsFor(ctx context.Context, repo string, days int, tracked *anchor.Tracked) *gitFacts {
	head, _ := gitx.Verify(ctx, repo, "HEAD")
	day := time.Now().UTC().Format("2006-01-02")
	factsMu.Lock()
	defer factsMu.Unlock()
	if f := factsCache[repo]; f != nil && f.head == head && f.day == day && f.days == days {
		return f
	}
	f := &gitFacts{head: head, day: day, days: days, excluded: map[string]bool{}}
	// The three git runs are independent and the binary scan dominates, so they overlap.
	var bin, attr map[string]bool
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); bin, _ = anchor.BinaryPaths(ctx, repo) }()
	go func() {
		defer wg.Done()
		var names []string
		for _, p := range tracked.Paths() {
			if !lockfiles[path.Base(p)] {
				names = append(names, p)
			}
		}
		attr = linguistExcluded(ctx, repo, names)
	}()
	go func() { defer wg.Done(); f.commits = commitFiles(ctx, repo, days) }()
	wg.Wait()
	for _, p := range tracked.Paths() {
		if bin[p] || attr[p] || lockfiles[path.Base(p)] {
			f.excluded[p] = true
		}
	}
	factsCache[repo] = f
	return f
}

// workFiles is the tracked files that are not excluded.
func workFiles(tracked *anchor.Tracked, excluded map[string]bool) []string {
	var out []string
	for _, p := range tracked.Paths() {
		if !excluded[p] {
			out = append(out, p)
		}
	}
	return out
}

// linguistExcluded asks git check-attr, once for all paths, which have linguist-generated or linguist-vendored set.
func linguistExcluded(ctx context.Context, repo string, paths []string) map[string]bool {
	skip := map[string]bool{}
	if len(paths) == 0 {
		return skip
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "check-attr", "-z", "--stdin", "linguist-generated", "linguist-vendored")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if cmd.Run() != nil {
		return skip
	}
	f := strings.Split(stdout.String(), "\x00")
	for i := 0; i+2 < len(f); i += 3 {
		if v := f[i+2]; v == "set" || v == "true" {
			skip[f[i]] = true
		}
	}
	return skip
}

type dirNode struct {
	children map[string]*dirNode
	direct   []string // non-excluded files directly in the directory
	covered  map[string]bool
	files    []string // every non-excluded file below, filled by tally
	unmapped int
}

func buildDirs(files []string) *dirNode {
	root := &dirNode{children: map[string]*dirNode{}}
	for _, f := range files {
		n := root
		parts := strings.Split(f, "/")
		for _, d := range parts[:len(parts)-1] {
			c := n.children[d]
			if c == nil {
				c = &dirNode{children: map[string]*dirNode{}}
				n.children[d] = c
			}
			n = c
		}
		n.direct = append(n.direct, f)
	}
	return root
}

// tally fills each directory's files below and its uncovered count.
func (n *dirNode) tally(isCovered func(string) bool) {
	n.covered = map[string]bool{}
	n.files = append(n.files, n.direct...)
	for _, f := range n.direct {
		if isCovered(f) {
			n.covered[f] = true
		} else {
			n.unmapped++
		}
	}
	for _, c := range n.children {
		c.tally(isCovered)
		n.files = append(n.files, c.files...)
		n.unmapped += c.unmapped
	}
}

type rawItem struct {
	coverageUnmapped
	members []string
}

// walk is the plan's unmapped walk from the directory dir: a directory whose non-excluded files are all uncovered is one item and the walk stops;
// otherwise it recurses, and the uncovered files directly in it become one loose item.
func (n *dirNode) walk(dir string, out *[]rawItem) {
	if len(n.files) > 0 && n.unmapped == len(n.files) {
		*out = append(*out, rawItem{coverageUnmapped{Path: dir, Files: len(n.files), Unmapped: n.unmapped}, n.files})
		return
	}
	names := make([]string, 0, len(n.children))
	for name := range n.children {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		n.children[name].walk(path.Join(dir, name), out)
	}
	var loose []string
	for _, f := range n.direct {
		if !n.covered[f] {
			loose = append(loose, f)
		}
	}
	if len(loose) > 0 {
		item := rawItem{coverageUnmapped{Path: dir, Loose: true, Files: len(loose), Unmapped: len(loose)}, loose}
		for _, f := range loose[:min(maxNames, len(loose))] {
			item.Names = append(item.Names, path.Base(f))
		}
		*out = append(*out, item)
	}
}

// unmappedItems walks the checkout the Viewer reads from and lists, ranked by commits in the window then unmapped files, the places no live entry covers.
// A checkout that is not a git repository has none.
func unmappedItems(ctx context.Context, p *project.Project, tree signTree, within string, limit int) ([]coverageUnmapped, error) {
	repo := view.From(ctx).Checkout
	if repo == "" {
		repo = p.Repo()
	}
	tracked, ok, err := anchor.ListTracked(ctx, repo)
	if err != nil || !ok {
		return []coverageUnmapped{}, nil
	}
	dirMax, err := search.Setting(ctx, p.DB(), "coverage_dir_max")
	if err != nil {
		return nil, err
	}
	days, err := search.Setting(ctx, p.DB(), "coverage_window_days")
	if err != nil {
		return nil, err
	}
	live, err := anchor.LiveAnchors(ctx, p.DB())
	if err != nil {
		return nil, err
	}
	scope := anchor.Scope{Tracked: tracked, DirMax: int(dirMax)}
	facts := factsFor(ctx, repo, int(days), tracked)
	root := buildDirs(workFiles(tracked, facts.excluded))
	root.tally(func(rel string) bool {
		for _, a := range live {
			if scope.Covers(a, rel) {
				return true
			}
		}
		return false
	})
	var items []rawItem
	root.walk(".", &items)
	if len(items) == 0 {
		return []coverageUnmapped{}, nil
	}

	pl, err := newPlacer(ctx, p.DB(), tree)
	if err != nil {
		return nil, err
	}
	for i := range items {
		// The pseudo file sits in the item's directory, so the sign is the one nearest the work that is already covered there.
		items[i].Sign = pl.signFor(path.Join(items[i].Path, "_"))
	}
	countCommits(facts.commits, items)

	kept := items[:0]
	for _, it := range items {
		if tree.under(it.Sign, within) {
			kept = append(kept, it)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		a, b := kept[i], kept[j]
		switch {
		case a.Commits90d != b.Commits90d:
			return a.Commits90d > b.Commits90d
		case a.Unmapped != b.Unmapped:
			return a.Unmapped > b.Unmapped
		case a.Loose != b.Loose:
			return !a.Loose
		}
		return a.Path < b.Path
	})
	out := make([]coverageUnmapped, 0, min(limit, len(kept)))
	for _, it := range kept[:min(limit, len(kept))] {
		out = append(out, it.coverageUnmapped)
	}
	return out, nil
}

// commitFiles is one git log: the files each commit of the last days days touched.
func commitFiles(ctx context.Context, repo string, days int) [][]string {
	since := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	out, ok, err := gitx.Run(ctx, repo, nil, "-c", "core.quotepath=off", "log", "--since="+since, "--name-only", "--format=%x01")
	if err != nil || !ok {
		return nil
	}
	var commits [][]string
	for _, commit := range strings.Split(out, "\x01") {
		if files := gitx.Lines(commit); len(files) > 0 {
			commits = append(commits, files)
		}
	}
	return commits
}

// countCommits sets Commits90d on every item: the distinct commits of the window that touch any of the item's files.
func countCommits(commits [][]string, items []rawItem) {
	owners := map[string][]int{}
	for i, it := range items {
		for _, f := range it.members {
			owners[f] = append(owners[f], i)
		}
	}
	for _, files := range commits {
		hit := map[int]bool{}
		for _, f := range files {
			for _, i := range owners[f] {
				hit[i] = true
			}
		}
		for i := range hit {
			items[i].Commits90d++
		}
	}
}
