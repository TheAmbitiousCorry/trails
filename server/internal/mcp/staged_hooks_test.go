package mcp_test

import (
	"context"
	"net/http"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// agentWithRuntime is agentAt for a session a hook can name by its runtime id.
func agentWithRuntime(t *testing.T, addr, checkout, runtime string) *agent {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	a := &agent{t: t, sess: sess}
	a.sid = a.ok("session_start", map[string]any{"task": "hook walk", "checkout": checkout, "runtime_session_id": runtime})["session_id"].(string)
	return a
}

func TestHooksThroughTheSessionsViewerHideStagedEntriesFromBase(t *testing.T) {
	main, addr, projectID := stagedServer(t)
	wx := worktree(t, main, "feat/x", "main")
	putFile(t, wx, "src/new.go", "package a\n\nfunc New() {}\n")
	branch := agentWithRuntime(t, addr, wx, "rt-branch")
	base := agentWithRuntime(t, addr, main, "rt-base")
	id := mustZebra(t, branch, "src/new.go")["entry_id"].(string)

	call := func(route, runtime string) map[string]any {
		return postAPI(t, addr, route, map[string]any{"project": projectID, "runtime_session_id": runtime, "path": "src/new.go"})
	}
	if got := call("/api/pre-read", "rt-base"); got["decision"] != "unmapped" {
		t.Errorf("base pre-read = %v, want unmapped", got)
	}
	got := call("/api/pre-read", "rt-branch")
	if got["decision"] != "turned_back" || got["entry_id"] != id {
		t.Errorf("branch pre-read = %v, want turned_back to %s", got, id)
	}
	if got := call("/api/off-trail", "rt-base"); len(got) != 0 {
		t.Errorf("base off-trail = %v, want empty", got)
	}
	if got := call("/api/off-trail", "rt-branch"); got["entry_id"] != id {
		t.Errorf("branch off-trail = %v, want a signpost to %s", got, id)
	}
	if got := call("/api/edited", "rt-base"); len(got["entries"].([]any)) != 0 {
		t.Errorf("base edited = %v, want no entries", got)
	}
	if got := call("/api/edited", "rt-branch"); len(got["entries"].([]any)) != 1 {
		t.Errorf("branch edited = %v, want the staged entry", got)
	}
	_ = base
}
