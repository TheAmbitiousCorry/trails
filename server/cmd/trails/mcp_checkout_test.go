package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShimSendsTheCheckoutItRunsIn(t *testing.T) {
	var headers []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" {
			headers = append(headers, r.Header.Get("X-Trails-Checkout"))
		}
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".trail"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".trail", "id"), []byte("abc123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "pkg")
	os.MkdirAll(sub, 0o755)

	var out, errb strings.Builder
	code := runMCP([]string{"--addr", addr, "--repo", sub, "--no-autostart"}, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n"), &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if len(headers) != 1 || headers[0] != repo {
		t.Errorf("X-Trails-Checkout = %q, want the directory holding .trail/id: %q", headers, repo)
	}
}

func TestShimSendsNoCheckoutWithoutAProject(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("X-Trails-Checkout"))
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer srv.Close()
	var out, errb strings.Builder
	runMCP([]string{"--addr", strings.TrimPrefix(srv.URL, "http://"), "--repo", t.TempDir(), "--no-autostart"}, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n"), &out, &errb)
	if len(got) != 1 || got[0] != "" {
		t.Errorf("X-Trails-Checkout = %q, want none", got)
	}
}
