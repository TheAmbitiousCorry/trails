package project

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/gitx"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
	"github.com/TheAmbitiousCorry/trails/internal/view"
)

// Viewer builds the Viewer of a call from the checkout it runs in, read at the time of the call so a branch switch mid-session is seen. An empty checkout is the primary.
// A staged entry is visible when the checkout is on the entry's branch, or when the entry's pinned landing commit is an ancestor of the checkout's HEAD; before a landing commit exists only the branch name shows it.
// A visible staged entry hides the entry it stages over.
func (p *Project) Viewer(ctx context.Context, checkout string) view.Viewer {
	top := checkout
	if top == "" {
		top = p.Repo()
	}
	v := view.Viewer{Checkout: top}
	if top == "" {
		return v
	}
	if b, ok, _ := gitx.Out(ctx, top, "symbolic-ref", "--short", "HEAD"); ok {
		v.Branch = b
	}
	if h, ok, _ := gitx.Out(ctx, top, "rev-parse", "HEAD"); ok {
		v.Head = h
	}
	p.markStaged(ctx, &v, top)
	return v
}

// BaseViewer is the Viewer of a caller with no checkout, such as a hook with no open session: it sees no staged entry, so a base session is never told about one.
func (p *Project) BaseViewer(ctx context.Context) view.Viewer {
	var v view.Viewer
	p.markStaged(ctx, &v, "")
	return v
}

// markStaged fills the Viewer's staged, visible and hidden sets from the entries still held on a branch.
func (p *Project) markStaged(ctx context.Context, v *view.Viewer, top string) {
	rows, err := p.DB().QueryContext(ctx, `SELECT id, staged_branch, COALESCE(staged_landing, ''), COALESCE(stages_over, '') FROM entries WHERE staged_branch IS NOT NULL`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, branch, landing, over string
		if rows.Scan(&id, &branch, &landing, &over) != nil {
			continue
		}
		if v.Staged == nil {
			v.Staged, v.Visible, v.Hidden = map[string]bool{}, map[string]bool{}, map[string]bool{}
		}
		v.Staged[id] = true
		if v.Branch != "" && v.Branch == branch || landing != "" && v.Head != "" && p.contains(ctx, top, landing, v.Head) {
			v.Visible[id] = true
			if over != "" {
				v.Hidden[over] = true
			}
		}
	}
}

type ancestry struct{ landing, head string }

// contains reports whether landing is an ancestor of head. Ancestry between two shas never changes, so the answer is cached for the life of the process.
func (p *Project) contains(ctx context.Context, top, landing, head string) bool {
	key := ancestry{landing, head}
	if v, ok := p.ancestors.Load(key); ok {
		return v.(bool)
	}
	ok, err := gitx.IsAncestor(ctx, top, landing, head)
	if err == nil {
		p.ancestors.Store(key, ok)
	}
	return ok
}

// Place implements trail.Placer: it settles the call's checkout and returns ctx carrying that checkout's Viewer.
// A claimed checkout (the call's input, else the X-Trails-Checkout header) is honoured only when it is bound to the project or git lists it as a worktree of the primary; anything else is ignored and the session uses the primary.
// A call with no claim keeps the checkout its session started with, which is how a raw HTTP caller and the hook endpoints stay on one checkout.
func (r *Registry) Place(ctx context.Context, p *Project, c trail.Call) context.Context {
	claimed := c.Checkout
	if claimed == "" {
		claimed = c.CheckoutHint
	}
	checkout := ""
	if claimed != "" {
		checkout = r.trustedCheckout(ctx, p, claimed)
	} else if c.SessionID != "" {
		r.mu.Lock()
		checkout = r.checkouts[c.SessionID]
		r.mu.Unlock()
	}
	if c.SessionID != "" && claimed != "" {
		r.mu.Lock()
		if checkout == "" {
			delete(r.checkouts, c.SessionID)
		} else {
			r.checkouts[c.SessionID] = checkout
		}
		r.mu.Unlock()
	}
	return view.With(ctx, p.Viewer(ctx, checkout))
}

// trustedCheckout returns the bound path claimed names, attaching it first when it is an unbound worktree of the primary, else "".
func (r *Registry) trustedCheckout(ctx context.Context, p *Project, claimed string) string {
	if !filepath.IsAbs(claimed) {
		return ""
	}
	want := realPath(claimed)
	for _, b := range p.Paths() {
		if realPath(b) == want {
			return b
		}
	}
	primary := p.Repo()
	if primary == "" {
		return ""
	}
	for _, w := range listedWorktrees(ctx, primary) {
		if realPath(w) != want {
			continue
		}
		if _, err := r.Open(w); err != nil {
			return ""
		}
		return w
	}
	return ""
}

// listedWorktrees is the path of every worktree git lists for the repository at top, the main one included.
func listedWorktrees(ctx context.Context, top string) []string {
	out, ok, err := gitx.Out(ctx, top, "worktree", "list", "--porcelain")
	if err != nil || !ok {
		return nil
	}
	var paths []string
	for _, l := range gitx.Lines(out) {
		if rest, found := strings.CutPrefix(l, "worktree "); found {
			paths = append(paths, rest)
		}
	}
	return paths
}

func realPath(p string) string {
	p = filepath.Clean(p)
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return p
}
