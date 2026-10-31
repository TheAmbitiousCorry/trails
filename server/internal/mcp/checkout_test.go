package mcp_test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type checkoutHeader struct{ header, value string }

func (h checkoutHeader) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set(h.header, h.value)
	return http.DefaultTransport.RoundTrip(r)
}

func startWith(t *testing.T, addr string, client *http.Client, args map[string]any) {
	t.Helper()
	c := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := c.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: client}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	args["task"] = "checkout walk"
	res, err := sess.CallTool(context.Background(), &sdk.CallToolParams{Name: "session_start", Arguments: args})
	structured(t, res, err)
}

func TestCheckoutInputAndHeaderAreTrustedOnlyForListedWorktrees(t *testing.T) {
	main, linked := gitWorktrees(t)
	reg, addr := serveRepos(t, main)
	p := reg.List()[0]

	// An unlisted directory, by input and by header, is ignored: the session starts and nothing is attached.
	elsewhere := t.TempDir()
	startWith(t, addr, http.DefaultClient, map[string]any{"checkout": elsewhere})
	startWith(t, addr, &http.Client{Transport: checkoutHeader{"X-Trails-Checkout", elsewhere}}, map[string]any{})
	if !slices.Equal(p.Paths(), []string{main}) {
		t.Fatalf("an unlisted checkout changed the bound paths: %v", p.Paths())
	}

	// A worktree git lists is attached through Open, by the header or by the input.
	startWith(t, addr, &http.Client{Transport: checkoutHeader{"X-Trails-Checkout", linked}}, map[string]any{})
	if !slices.Equal(p.Paths(), []string{main, linked}) {
		t.Fatalf("the listed worktree was not attached: %v", p.Paths())
	}
}

func TestCheckoutInputOutranksTheHeader(t *testing.T) {
	main, linked := gitWorktrees(t)
	reg, addr := serveRepos(t, main)
	startWith(t, addr, &http.Client{Transport: checkoutHeader{"X-Trails-Checkout", t.TempDir()}}, map[string]any{"checkout": linked})
	if p := reg.List()[0]; !slices.Equal(p.Paths(), []string{main, linked}) {
		t.Fatalf("paths = %v, want the input's worktree attached", p.Paths())
	}
}
