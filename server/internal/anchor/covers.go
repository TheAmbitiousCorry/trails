package anchor

import (
	"context"
	"database/sql"
	"path"
	"sort"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/gitx"
	"github.com/TheAmbitiousCorry/trails/internal/view"
)

// Live is a file, range or dir anchor of a live entry, with the entry fields a reader needs to name it.
type Live struct {
	EntryID   string
	ReadWhen  string
	SignID    string
	Type      string
	Path      string
	CreatedAt string
}

// LiveAnchors lists the file, range and dir anchors of live entries (not dormant, not superseded) that the Viewer in ctx is shown, oldest entry first.
func LiveAnchors(ctx context.Context, db *sql.DB) ([]Live, error) {
	rows, err := db.QueryContext(ctx, `SELECT e.id, e.read_when, e.sign_id, a.type, a.path_or_url, e.created_at
		FROM anchors a JOIN entries e ON e.id = a.entry_id
		WHERE a.type IN ('file', 'range', 'dir') AND e.tier != 'dormant' AND e.superseded_by IS NULL
		ORDER BY e.created_at, e.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	viewer := view.From(ctx)
	var out []Live
	for rows.Next() {
		var l Live
		if err := rows.Scan(&l.EntryID, &l.ReadWhen, &l.SignID, &l.Type, &l.Path, &l.CreatedAt); err != nil {
			return nil, err
		}
		if viewer.Sees(l.EntryID) {
			l.Path = strings.TrimSuffix(path.Clean(l.Path), "/")
			out = append(out, l)
		}
	}
	return out, rows.Err()
}

// Counter reports how many tracked files lie below a directory.
type Counter interface{ Count(dir string) int }

// Scope is what Covers needs beyond the anchor: where tracked-file counts come from and the largest subtree a dir anchor may cover.
// A nil Tracked cannot measure a directory, so a dir anchor covers its whole subtree.
type Scope struct {
	Tracked Counter
	DirMax  int
}

// Covers is the one coverage predicate: a file or range anchor covers its own path, and a dir anchor covers every path below it
// as long as its subtree holds at most DirMax tracked files, so one anchor on src/ cannot mark a whole tree covered.
// The coverage tool and the pre-read, off-trail and edited hooks all decide through it.
func (s Scope) Covers(a Live, rel string) bool {
	switch a.Type {
	case "file", "range":
		return a.Path == rel
	case "dir":
		dir := strings.TrimSuffix(a.Path, "/")
		if !strings.HasPrefix(rel, dir+"/") {
			return false
		}
		return s.Tracked == nil || s.Tracked.Count(dir) <= s.DirMax
	}
	return false
}

// Tracked is the sorted set of a checkout's tracked files, which answers subtree counts.
type Tracked struct{ paths []string }

// NewTracked sorts and de-duplicates paths (an unmerged path is listed once per stage).
func NewTracked(paths []string) *Tracked {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	t := &Tracked{}
	for _, p := range sorted {
		if n := len(t.paths); n == 0 || t.paths[n-1] != p {
			t.paths = append(t.paths, p)
		}
	}
	return t
}

// Paths lists the tracked paths in order.
func (t *Tracked) Paths() []string { return t.paths }

// Count is the number of tracked files below dir; "" and "." are the whole checkout.
func (t *Tracked) Count(dir string) int {
	if dir == "" || dir == "." {
		return len(t.paths)
	}
	lo := sort.SearchStrings(t.paths, dir+"/")
	hi := sort.SearchStrings(t.paths, dir+"0") // "0" is the byte after "/"
	return hi - lo
}

// ListTracked reads the tracked files of the checkout at repo with one git ls-files. ok is false when repo is not a git checkout.
func ListTracked(ctx context.Context, repo string) (t *Tracked, ok bool, err error) {
	out, ok, err := gitx.Run(ctx, repo, nil, "ls-files", "--full-name", "-z")
	if err != nil || !ok {
		return nil, false, err
	}
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return NewTracked(paths), true, nil
}

// BinaryPaths lists the tracked files whose index side git calls non-text. It reads every blob, which takes about half a second on a few thousand files, so callers cache it.
func BinaryPaths(ctx context.Context, repo string) (map[string]bool, error) {
	out, _, err := gitx.Run(ctx, repo, nil, "ls-files", "--full-name", "--eol", "-z")
	if err != nil {
		return nil, err
	}
	bin := map[string]bool{}
	for _, rec := range strings.Split(out, "\x00") {
		// "i/-text w/-text attr/                 \tpath"
		if meta, p, found := strings.Cut(rec, "\t"); found && strings.HasPrefix(meta, "i/-text") {
			bin[p] = true
		}
	}
	return bin, nil
}

// LazyTracked lists the checkout's tracked files the first time a count is asked for, so a hook that never meets a dir anchor never runs git.
// It is for one call and is not safe for concurrent use.
type LazyTracked struct {
	ctx    context.Context
	repo   string
	loaded bool
	t      *Tracked
}

// NewLazyTracked is a Counter over the checkout at repo. A repo that is not a git checkout reads as zero files, so its dir anchors cover.
func NewLazyTracked(ctx context.Context, repo string) *LazyTracked {
	return &LazyTracked{ctx: ctx, repo: repo}
}

func (l *LazyTracked) Count(dir string) int {
	if !l.loaded {
		l.loaded = true
		if l.repo != "" {
			l.t, _, _ = ListTracked(l.ctx, l.repo)
		}
	}
	if l.t == nil {
		return 0
	}
	return l.t.Count(dir)
}
