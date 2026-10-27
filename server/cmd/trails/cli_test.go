package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeServer(t *testing.T, sessions string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/map", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"project":{"title":"Demo"},"root":{"entries":12,"open_gaps":3}}`))
	})
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sessions))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestOpenPrintsURL(t *testing.T) {
	t.Setenv("TRAILS_NO_BROWSER", "1")
	closedFirst := `{"sessions":[{"id":"s3","ended_at":"2026-01-03"},{"id":"s2","ended_at":""},{"id":"s1","ended_at":""}]}`
	allClosed := `{"sessions":[{"id":"s3","ended_at":"x"},{"id":"s2","ended_at":"y"}]}`
	cases := []struct {
		name     string
		sessions string
		args     []string
		path     string
	}{
		{"default is map", closedFirst, nil, "/"},
		{"map", closedFirst, []string{"map"}, "/"},
		{"graph", closedFirst, []string{"graph"}, "/graph"},
		{"trail prefers open", closedFirst, []string{"trail"}, "/sessions/s2"},
		{"trail falls back to latest", allClosed, []string{"trail"}, "/sessions/s3"},
		{"trail without sessions", `{"sessions":[]}`, []string{"trail"}, "/sessions"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addr := fakeServer(t, c.sessions)
			var out, errb bytes.Buffer
			code := runOpen(append(c.args, "--addr", addr), &out, &errb)
			want := "http://" + addr + c.path + "\n"
			if code != 0 || out.String() != want {
				t.Fatalf("code %d, out %q, stderr %q, want %q", code, out.String(), errb.String(), want)
			}
		})
	}
}

func TestNoServer(t *testing.T) {
	t.Setenv("TRAILS_NO_BROWSER", "1")
	dead := httptest.NewServer(http.NewServeMux())
	addr := strings.TrimPrefix(dead.URL, "http://")
	dead.Close()
	for name, run := range map[string]func([]string, *bytes.Buffer, *bytes.Buffer) int{
		"open":   func(a []string, o, e *bytes.Buffer) int { return runOpen(a, o, e) },
		"trail":  func(a []string, o, e *bytes.Buffer) int { return runOpen(append([]string{"trail"}, a...), o, e) },
		"status": func(a []string, o, e *bytes.Buffer) int { return runStatus(a, o, e) },
	} {
		var out, errb bytes.Buffer
		if code := run([]string{"--addr", addr}, &out, &errb); code != 1 {
			t.Fatalf("%s: code %d", name, code)
		}
		if !strings.HasPrefix(errb.String(), "no Trails server at "+addr+"; start it with: trails serve --repo ") {
			t.Fatalf("%s: stderr %q", name, errb.String())
		}
	}
}

func TestStatus(t *testing.T) {
	addr := fakeServer(t, `{"sessions":[]}`)
	var out, errb bytes.Buffer
	if code := runStatus([]string{"--addr", addr}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if out.String() != "Demo: 12 entries, 3 open gaps\n" {
		t.Fatalf("out %q", out.String())
	}
}

func repoWithPort(t *testing.T, contents string) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".trail"), 0o755); err != nil {
		t.Fatal(err)
	}
	if contents != "" {
		if err := os.WriteFile(filepath.Join(repo, ".trail", "port"), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func TestRepoAddr(t *testing.T) {
	cases := []struct{ name, file, want string }{
		{"absent file is the default", "", defaultAddr},
		{"port file", "9123\n", "127.0.0.1:9123"},
		{"surrounding space", "  9123  \n\n", "127.0.0.1:9123"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := repoAddr(repoWithPort(t, c.file))
			if err != nil || got != c.want {
				t.Fatalf("got %q, %v, want %q", got, err, c.want)
			}
		})
	}
}

func TestBadPortFile(t *testing.T) {
	t.Setenv("TRAILS_NO_BROWSER", "1")
	for _, bad := range []string{"abc\n", "0", "65536", "-1", "80 81", "127.0.0.1:9000"} {
		repo := repoWithPort(t, bad)
		file := filepath.Join(repo, ".trail", "port")
		if _, err := repoAddr(repo); err == nil || !strings.Contains(err.Error(), file) {
			t.Fatalf("%q: error %v does not name %s", bad, err, file)
		}
		var out, errb bytes.Buffer
		if code := runStatus([]string{"--repo", repo}, &out, &errb); code != 1 || !strings.Contains(errb.String(), file) {
			t.Fatalf("%q: status code %d, stderr %q", bad, code, errb.String())
		}
		errb.Reset()
		if code := runOpen([]string{"--repo", repo}, &out, &errb); code != 1 || !strings.Contains(errb.String(), file) {
			t.Fatalf("%q: open code %d, stderr %q", bad, code, errb.String())
		}
	}
}

func TestDefaultAddrFromPortFile(t *testing.T) {
	t.Setenv("TRAILS_NO_BROWSER", "1")
	addr := fakeServer(t, `{"sessions":[]}`)
	port := addr[strings.LastIndex(addr, ":")+1:]
	repo := repoWithPort(t, port+"\n")

	var out, errb bytes.Buffer
	if code := runStatus([]string{"--repo", repo}, &out, &errb); code != 0 || out.String() != "Demo: 12 entries, 3 open gaps\n" {
		t.Fatalf("status code %d, out %q, stderr %q", code, out.String(), errb.String())
	}
	out.Reset()
	if code := runOpen([]string{"map", "--repo", repo}, &out, &errb); code != 0 || out.String() != "http://"+addr+"/\n" {
		t.Fatalf("open code %d, out %q, stderr %q", code, out.String(), errb.String())
	}

	// The working directory is the repo when --repo is absent.
	t.Chdir(repo)
	out.Reset()
	if code := runStatus(nil, &out, &errb); code != 0 {
		t.Fatalf("status from cwd code %d, stderr %q", code, errb.String())
	}

	// An explicit --addr wins over the file, even a bad one.
	bad := repoWithPort(t, "nope")
	out.Reset()
	if code := runStatus([]string{"--repo", bad, "--addr", addr}, &out, &errb); code != 0 {
		t.Fatalf("explicit addr code %d, stderr %q", code, errb.String())
	}
}
