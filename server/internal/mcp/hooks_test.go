package mcp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

// post sends body to route and returns the status and decoded JSON reply.
func post(t *testing.T, addr, route string, body map[string]any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post("http://"+addr+route, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestRegisterOverHTTP(t *testing.T) {
	reg, addr := serveRepos(t)
	if got := len(reg.List()); got != 0 {
		t.Fatalf("started with %d projects", got)
	}

	repo := t.TempDir()
	id, err := store.EnsureID(repo)
	if err != nil {
		t.Fatal(err)
	}
	code, out := post(t, addr, "/api/projects", map[string]any{"repo": repo})
	if code != 200 || out["id"] != id || out["title"] != filepath.Base(repo) {
		t.Fatalf("register = %d %v", code, out)
	}
	// A second registration returns the same project and opens nothing new.
	if code, out := post(t, addr, "/api/projects", map[string]any{"repo": repo}); code != 200 || out["id"] != id || len(reg.List()) != 1 {
		t.Fatalf("re-register = %d %v, %d projects", code, out, len(reg.List()))
	}

	resp, err := http.Get("http://" + addr + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list struct {
		Projects []map[string]any `json:"projects"`
	}
	json.NewDecoder(resp.Body).Decode(&list)
	if len(list.Projects) != 1 || list.Projects[0]["id"] != id || list.Projects[0]["title"] != filepath.Base(repo) {
		t.Fatalf("list = %v", list.Projects)
	}
	// A fresh project holds its seeded entry and no session or gap.
	if list.Projects[0]["entries"] != 1.0 || list.Projects[0]["open_sessions"] != 0.0 || list.Projects[0]["open_gaps"] != 0.0 {
		t.Fatalf("counts = %v", list.Projects[0])
	}
}

func TestRegisterNeverCreatesTheID(t *testing.T) {
	reg, addr := serveRepos(t)
	repo := t.TempDir()
	code, out := post(t, addr, "/api/projects", map[string]any{"repo": repo})
	if code != http.StatusUnprocessableEntity || out["error"] != "not_opted_in" {
		t.Fatalf("no .trail/id = %d %v", code, out)
	}
	if entries, _ := os.ReadDir(repo); len(entries) != 0 {
		t.Errorf("a refused registration left %d entries in the repo", len(entries))
	}
	if len(reg.List()) != 0 {
		t.Error("a refused registration opened a project")
	}
	for _, bad := range []string{"", "relative/path", filepath.Join(repo, "missing")} {
		if code, _ := post(t, addr, "/api/projects", map[string]any{"repo": bad}); code != http.StatusBadRequest {
			t.Errorf("repo %q: status %d, want 400", bad, code)
		}
	}
}

func TestHookEndpointsFindTheirProject(t *testing.T) {
	repoA, repoB := t.TempDir(), t.TempDir()
	reg, addr := serveRepos(t, repoA, repoB)
	idA, _ := store.ReadID(repoA)
	idB, _ := store.ReadID(repoB)
	call := connect(t, addr)
	start := func(id, rt string) string {
		res, err := call("session_start", map[string]any{"task": "work", "project": id, "runtime_session_id": rt})
		return structured(t, res, err)["session_id"].(string)
	}
	sidA, sidB := start(idA, "rt-a"), start(idB, "rt-b")

	events := func(id, sid string) int {
		p, _ := reg.ByID(id)
		var n int
		p.DB().QueryRow(`SELECT COUNT(*) FROM events WHERE session_id = ? AND tool = 'off_trail'`, sid).Scan(&n)
		return n
	}

	// With project in the body the endpoint looks only in that project.
	post(t, addr, "/api/off-trail", map[string]any{"project": idA, "runtime_session_id": "rt-a", "path": "src/a.go"})
	if events(idA, sidA) != 1 {
		t.Error("off-trail with project did not land in A")
	}
	post(t, addr, "/api/off-trail", map[string]any{"project": idA, "runtime_session_id": "rt-b", "path": "src/a.go"})
	if events(idB, sidB) != 0 {
		t.Error("off-trail naming A recorded into B's session")
	}
	if code, out := post(t, addr, "/api/stop-check", map[string]any{"project": idA, "runtime_session_id": "rt-b"}); code != 200 || out["open"] != false {
		t.Errorf("stop-check naming the wrong project = %d %v", code, out)
	}

	// Without project the runtime id is found across projects.
	post(t, addr, "/api/off-trail", map[string]any{"runtime_session_id": "rt-b", "path": "src/b.go"})
	if events(idB, sidB) != 1 {
		t.Error("off-trail without project did not land in B")
	}
	if code, out := post(t, addr, "/api/stop-check", map[string]any{"runtime_session_id": "rt-b"}); code != 200 || out["open"] != true {
		t.Errorf("stop-check without project = %d %v", code, out)
	}

	// An unknown project is a 404, not a quiet pass.
	if code, _ := post(t, addr, "/api/stop-check", map[string]any{"project": "nope", "runtime_session_id": "rt-a"}); code != http.StatusNotFound {
		t.Errorf("unknown project: status %d", code)
	}
}
