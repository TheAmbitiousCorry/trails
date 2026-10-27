package web_test

import (
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/mcp"
	"github.com/TheAmbitiousCorry/trails/internal/project"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

// serveRepos serves a registry of n fresh projects, each with one sign named after its position, and returns the address and the ids.
func serveRepos(t *testing.T, n int) (addr string, ids []string) {
	t.Helper()
	reg := project.NewRegistry(t.TempDir(), 0)
	t.Cleanup(func() { reg.Close() })
	for i := range n {
		repo := t.TempDir()
		if _, err := store.EnsureID(repo); err != nil {
			t.Fatal(err)
		}
		p, err := reg.Open(repo)
		if err != nil {
			t.Fatal(err)
		}
		sign := "only-" + string(rune('a'+i))
		if _, err := p.Store.DB.Exec(`INSERT INTO signs (id, parent_id, title) VALUES (?, 'root', ?)`, sign, "Sign "+sign); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mcp.Handler(reg, ln.Addr().String())}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().String(), ids
}

func status(t *testing.T, addr, route string) (int, string) {
	t.Helper()
	c := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get("http://" + addr + route)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, resp.Header.Get("Location")
}

func TestRoutesAreScopedToTheirProject(t *testing.T) {
	addr, ids := serveRepos(t, 2)
	for i, id := range ids {
		own, other := "only-"+string(rune('a'+i)), "only-"+string(rune('a'+1-i))
		if code, _ := status(t, addr, "/api/p/"+id+"/signs/"+own); code != 200 {
			t.Errorf("%s holds %s: %d", id, own, code)
		}
		if code, _ := status(t, addr, "/api/p/"+id+"/signs/"+other); code != 404 {
			t.Errorf("%s sees %s: %d", id, other, code)
		}
	}
	if code, _ := status(t, addr, "/api/p/nope/map"); code != 404 {
		t.Errorf("unknown project: %d", code)
	}
	if code, _ := status(t, addr, "/api/map"); code != 404 {
		t.Errorf("unprefixed data route: %d", code)
	}
}

func TestUnprefixedPagesRedirectToTheOnlyProject(t *testing.T) {
	addr, ids := serveRepos(t, 1)
	for route, want := range map[string]string{
		"/graph":             "/p/" + ids[0] + "/graph",
		"/entries/x?tab=1":   "/p/" + ids[0] + "/entries/x?tab=1",
		"/p/" + ids[0] + "/": "",
		"/":                  "",
	} {
		code, loc := status(t, addr, route)
		if want == "" && code != 200 || want != "" && (code != http.StatusFound || loc != want) {
			t.Errorf("%s: %d %q, want %q", route, code, loc, want)
		}
	}
}

func TestUnprefixedPagesRedirectHomeWhenSeveralProjects(t *testing.T) {
	addr, _ := serveRepos(t, 2)
	for _, route := range []string{"/graph", "/entries/x"} {
		if code, loc := status(t, addr, route); code != http.StatusFound || loc != "/" {
			t.Errorf("%s: %d %q", route, code, loc)
		}
	}
	if code, _ := status(t, addr, "/"); code != 200 {
		t.Errorf("/: %d", code)
	}
	if code, _ := status(t, addr, "/assets/missing.js"); code != 404 {
		t.Errorf("missing asset: %d", code)
	}
}
