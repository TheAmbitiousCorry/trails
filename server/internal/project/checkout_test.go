package project_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/project"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
	"github.com/TheAmbitiousCorry/trails/internal/view"
)

func placed(t *testing.T, reg *project.Registry, p *project.Project, c trail.Call) view.Viewer {
	t.Helper()
	return view.From(reg.Place(context.Background(), p, c))
}

func TestClaimedCheckoutIsHonouredOnlyWhenBoundOrListed(t *testing.T) {
	reg := project.NewRegistry(t.TempDir(), 0)
	defer reg.Close()
	main, linked := gitWorktrees(t, "co")
	p, err := reg.Open(main)
	if err != nil {
		t.Fatal(err)
	}
	// The worktree is listed by git but not bound: the claim attaches it through Open.
	if got := placed(t, reg, p, trail.Call{CheckoutHint: linked}).Checkout; got != linked {
		t.Errorf("listed worktree: checkout = %q, want %q", got, linked)
	}
	if len(p.Paths()) != 2 {
		t.Errorf("the listed worktree was not attached: %v", p.Paths())
	}
	if got := placed(t, reg, p, trail.Call{Checkout: main}).Checkout; got != main {
		t.Errorf("bound path: checkout = %q, want %q", got, main)
	}
	// A directory git does not list is ignored, so the session uses the primary.
	elsewhere := t.TempDir()
	for _, claim := range []string{elsewhere, "relative/path", filepath.Join(main, "..", "..")} {
		if got := placed(t, reg, p, trail.Call{CheckoutHint: claim}).Checkout; got != main {
			t.Errorf("claim %q: checkout = %q, want the primary %q", claim, got, main)
		}
	}
	if len(p.Paths()) != 2 {
		t.Errorf("an unlisted claim changed the bound paths: %v", p.Paths())
	}
}

func TestListedWorktreeWithoutAnIDIsIgnored(t *testing.T) {
	reg := project.NewRegistry(t.TempDir(), 0)
	defer reg.Close()
	main, linked := gitWorktrees(t, "noid")
	p, err := reg.Open(main)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(linked, ".trail")); err != nil {
		t.Fatal(err)
	}
	if got := placed(t, reg, p, trail.Call{CheckoutHint: linked}).Checkout; got != main {
		t.Errorf("checkout = %q, want the primary %q", got, main)
	}
}

func TestSessionKeepsItsCheckoutForLaterCallsWithoutAClaim(t *testing.T) {
	reg := project.NewRegistry(t.TempDir(), 0)
	defer reg.Close()
	main, linked := gitWorktrees(t, "keep")
	p, _ := reg.Open(main)
	placed(t, reg, p, trail.Call{Mint: true, SessionID: "s-1", Checkout: linked})
	if got := placed(t, reg, p, trail.Call{SessionID: "s-1"}).Checkout; got != linked {
		t.Errorf("later call: checkout = %q, want %q", got, linked)
	}
	if got := placed(t, reg, p, trail.Call{SessionID: "s-other"}).Checkout; got != main {
		t.Errorf("another session: checkout = %q, want the primary", got)
	}
	// A bad claim on a later call drops the remembered checkout rather than keeping a stale one.
	placed(t, reg, p, trail.Call{SessionID: "s-1", CheckoutHint: t.TempDir()})
	if got := placed(t, reg, p, trail.Call{SessionID: "s-1"}).Checkout; got != main {
		t.Errorf("after a bad claim: checkout = %q, want the primary", got)
	}
}

func TestViewerReadsBranchAndHeadFromTheCheckout(t *testing.T) {
	reg := project.NewRegistry(t.TempDir(), 0)
	defer reg.Close()
	main, linked := gitWorktrees(t, "br")
	p, _ := reg.Open(main)
	v := p.Viewer(context.Background(), linked)
	if v.Checkout != linked || v.Head == "" {
		t.Fatalf("viewer = %+v", v)
	}
	if !v.Sees("any") {
		t.Error("step 1 Viewer hid an entry")
	}
}
