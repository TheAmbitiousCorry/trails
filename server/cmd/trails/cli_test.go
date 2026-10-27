package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoWithID is a directory holding .trail/id demo, with a subdirectory sub.
func repoWithID(t *testing.T, id string) (repo, sub string) {
	t.Helper()
	repo = t.TempDir()
	sub = filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".trail"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".trail", "id"), []byte(id+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo, sub
}

// fakeServer holds the projects demo ("Demo", 12 entries, 3 gaps) and other ("Other", 1 entry, 0 gaps).
func fakeServer(t *testing.T, sessions string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"projects":[{"id":"demo","title":"Demo"},{"id":"other","title":"Other"}]}`))
	})
	mux.HandleFunc("/api/p/demo/map", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"project":{"title":"Demo"},"root":{"entries":12,"open_gaps":3}}`))
	})
	mux.HandleFunc("/api/p/other/map", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"project":{"title":"Other"},"root":{"entries":1,"open_gaps":0}}`))
	})
	mux.HandleFunc("/api/p/demo/sessions", func(w http.ResponseWriter, r *http.Request) {
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
		{"default is map", closedFirst, nil, "/p/demo/"},
		{"map", closedFirst, []string{"map"}, "/p/demo/"},
		{"graph", closedFirst, []string{"graph"}, "/p/demo/graph"},
		{"trail prefers open", closedFirst, []string{"trail"}, "/p/demo/sessions/s2"},
		{"trail falls back to latest", allClosed, []string{"trail"}, "/p/demo/sessions/s3"},
		{"trail without sessions", `{"sessions":[]}`, []string{"trail"}, "/p/demo/sessions"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addr := fakeServer(t, c.sessions)
			_, sub := repoWithID(t, "demo")
			t.Chdir(sub)
			var out, errb bytes.Buffer
			code := runOpen(append(c.args, "--addr", addr), &out, &errb)
			want := "http://" + addr + c.path + "\n"
			if code != 0 || out.String() != want {
				t.Fatalf("code %d, out %q, stderr %q, want %q", code, out.String(), errb.String(), want)
			}
		})
	}
}

func TestOpenNamesTheRepoProject(t *testing.T) {
	t.Setenv("TRAILS_NO_BROWSER", "1")
	addr := fakeServer(t, `{"sessions":[]}`)
	repo, _ := repoWithID(t, "other")
	t.Chdir(t.TempDir())
	var out, errb bytes.Buffer
	if code := runOpen([]string{"graph", "--repo", repo, "--addr", addr}, &out, &errb); code != 0 || out.String() != "http://"+addr+"/p/other/graph\n" {
		t.Fatalf("code %d, out %q, stderr %q", code, out.String(), errb.String())
	}
}

func TestOpenRefusals(t *testing.T) {
	t.Setenv("TRAILS_NO_BROWSER", "1")
	addr := fakeServer(t, `{"sessions":[]}`)
	t.Chdir(t.TempDir())
	var out, errb bytes.Buffer
	if code := runOpen([]string{"--addr", addr}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "no .trail/id at or above") || out.Len() != 0 {
		t.Fatalf("no id: code %d, out %q, stderr %q", code, out.String(), errb.String())
	}
	repo, _ := repoWithID(t, "stranger")
	errb.Reset()
	if code := runOpen([]string{"--repo", repo, "--addr", addr}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "does not hold project stranger") || out.Len() != 0 {
		t.Fatalf("unregistered: code %d, out %q, stderr %q", code, out.String(), errb.String())
	}
}

func TestNoServer(t *testing.T) {
	t.Setenv("TRAILS_NO_BROWSER", "1")
	_, sub := repoWithID(t, "demo")
	t.Chdir(sub)
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
		if !strings.HasPrefix(errb.String(), "no Trails server at "+addr+"; start it with: trails serve\n") {
			t.Fatalf("%s: stderr %q", name, errb.String())
		}
	}
}

func TestStatusListsEveryProjectOrTheRepos(t *testing.T) {
	addr := fakeServer(t, `{"sessions":[]}`)
	t.Chdir(t.TempDir())
	var out, errb bytes.Buffer
	if code := runStatus([]string{"--addr", addr}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if want := "Demo (demo): 12 entries, 3 open gaps\nOther (other): 1 entries, 0 open gaps\n"; out.String() != want {
		t.Fatalf("out %q", out.String())
	}
	repo, _ := repoWithID(t, "other")
	out.Reset()
	if code := runStatus([]string{"--addr", addr, "--repo", repo}, &out, &errb); code != 0 || out.String() != "Other (other): 1 entries, 0 open gaps\n" {
		t.Fatalf("--repo: code %d, out %q, stderr %q", code, out.String(), errb.String())
	}
}

func TestDefaultAddrFromEnv(t *testing.T) {
	t.Setenv("TRAILS_NO_BROWSER", "1")
	addr := fakeServer(t, `{"sessions":[]}`)
	t.Setenv("TRAILS_ADDR", addr)
	_, sub := repoWithID(t, "demo")
	t.Chdir(sub)
	var out, errb bytes.Buffer
	if code := runStatus([]string{"--repo", "."}, &out, &errb); code != 0 || out.String() != "Demo (demo): 12 entries, 3 open gaps\n" {
		t.Fatalf("status code %d, out %q, stderr %q", code, out.String(), errb.String())
	}
	out.Reset()
	if code := runOpen([]string{"map"}, &out, &errb); code != 0 || out.String() != "http://"+addr+"/p/demo/\n" {
		t.Fatalf("open code %d, out %q, stderr %q", code, out.String(), errb.String())
	}
	t.Setenv("TRAILS_ADDR", "")
	if got := defaultAddr(); got != fallbackAddr {
		t.Fatalf("default %q", got)
	}
}

func TestRegisterCreatesIDAndPosts(t *testing.T) {
	var got string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/projects", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		got = in["repo"]
		w.Write([]byte(`{"id":"abc","title":"Demo"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	addr := strings.TrimPrefix(srv.URL, "http://")

	repo := t.TempDir()
	var out, errb bytes.Buffer
	if code := runRegister([]string{"--repo", repo, "--addr", addr}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if got != repo || out.String() != "registered Demo (abc) at "+addr+"\n" {
		t.Fatalf("posted %q, out %q", got, out.String())
	}
	if _, err := os.Stat(filepath.Join(repo, ".trail", "id")); err != nil {
		t.Fatalf("register left no .trail/id: %v", err)
	}
}

func TestRegisterWithoutServer(t *testing.T) {
	dead := httptest.NewServer(http.NewServeMux())
	addr := strings.TrimPrefix(dead.URL, "http://")
	dead.Close()
	var out, errb bytes.Buffer
	if code := runRegister([]string{"--repo", t.TempDir(), "--addr", addr}, &out, &errb); code != 1 || !strings.HasPrefix(errb.String(), "no Trails server at "+addr) {
		t.Fatalf("code %d, stderr %q", code, errb.String())
	}
}
