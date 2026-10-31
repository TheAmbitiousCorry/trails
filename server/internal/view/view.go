// Package view is the Viewer: what one reader's checkout is allowed to see of staged entries. It is a leaf package so every reader can take it without an import cycle; internal/project builds it.
package view

import "context"

// Viewer is built once per call. The zero value is the base Viewer: it sees every entry, which is what the browser and the sweep use.
type Viewer struct {
	// Checkout is the trusted session checkout; empty means the primary.
	Checkout string
	// Branch is the checkout's branch, empty when HEAD is detached or the checkout is unknown.
	Branch string
	// Head is the checkout's HEAD sha.
	Head string
	// Staged holds the id of every staged entry, Visible those of them this viewer sees, Hidden the originals a visible staged update stands over.
	Staged, Visible, Hidden map[string]bool
}

// Sees reports whether the viewer is shown the entry.
func (v Viewer) Sees(id string) bool {
	if v.Hidden[id] {
		return false
	}
	return !v.Staged[id] || v.Visible[id]
}

type ctxKey struct{}

// With returns ctx carrying v for every reader called with it.
func With(ctx context.Context, v Viewer) context.Context { return context.WithValue(ctx, ctxKey{}, v) }

// From is the Viewer in ctx, else the base Viewer.
func From(ctx context.Context) Viewer {
	v, _ := ctx.Value(ctxKey{}).(Viewer)
	return v
}
