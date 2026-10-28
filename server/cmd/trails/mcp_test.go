package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

type mcpSeen struct {
	body    []byte
	project string
	accept  string
	ctype   string
	host    string
}

// fakeServer answers /mcp with reply(body) and records what it received.
func mcpServer(t *testing.T, reply func(w http.ResponseWriter, body []byte)) (addr string, got *[]mcpSeen) {
	t.Helper()
	got = &[]mcpSeen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*got = append(*got, mcpSeen{b, r.Header.Get("X-Trails-Project"), r.Header.Get("Accept"), r.Header.Get("Content-Type"), r.Host})
		reply(w, b)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://"), got
}

func relayRun(t *testing.T, addr, stdin string, extra ...string) (stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	args := append([]string{"--addr", addr, "--project", "abc123", "--repo", t.TempDir(), "--no-autostart"}, extra...)
	if code := runMCP(args, strings.NewReader(stdin), &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	return out.String(), errb.String()
}

func TestRelaysToolsListByteForByte(t *testing.T) {
	const reply = `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"read"}]}}`
	addr, got := mcpServer(t, func(w http.ResponseWriter, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, reply)
	})
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	out, errOut := relayRun(t, addr, req+"\n")
	if out != reply+"\n" {
		t.Errorf("stdout = %q", out)
	}
	g := (*got)[0]
	if string(g.body) != req || g.project != "abc123" || g.host != addr || g.ctype != "application/json" || g.accept != "application/json, text/event-stream" {
		t.Errorf("request = %+v", g)
	}
	if !strings.Contains(errOut, "trails mcp: cwd ") || !strings.Contains(errOut, "project abc123, server "+addr) {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestRelaysA100KBLine(t *testing.T) {
	addr, got := mcpServer(t, func(w http.ResponseWriter, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{}}`)
	})
	req := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"body":"` + strings.Repeat("x", 100*1024) + `"}}`
	out, _ := relayRun(t, addr, req+"\n")
	if len((*got)[0].body) != len(req) || string((*got)[0].body) != req {
		t.Errorf("server saw %d bytes, want %d", len((*got)[0].body), len(req))
	}
	if out != `{"jsonrpc":"2.0","id":2,"result":{}}`+"\n" {
		t.Errorf("stdout = %q", out)
	}
}

func TestDecodesAnSSEReplyToOneLine(t *testing.T) {
	addr, _ := mcpServer(t, func(w http.ResponseWriter, _ []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":3,\"result\":{}}\n\nevent: message\ndata: {\"second\":true}\n\n")
	})
	out, _ := relayRun(t, addr, `{"jsonrpc":"2.0","id":3,"method":"ping"}`+"\n")
	if out != `{"jsonrpc":"2.0","id":3,"result":{}}`+"\n" {
		t.Errorf("stdout = %q", out)
	}
}

func TestRelaysABatchAsOneLine(t *testing.T) {
	const reply = `[{"jsonrpc":"2.0","id":1,"result":{}},{"jsonrpc":"2.0","id":2,"result":{}}]`
	addr, got := mcpServer(t, func(w http.ResponseWriter, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, reply)
	})
	req := `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`
	out, _ := relayRun(t, addr, req+"\n")
	if string((*got)[0].body) != req || out != reply+"\n" {
		t.Errorf("body %q, stdout %q", (*got)[0].body, out)
	}
}

func TestAcceptedReplyWritesNothing(t *testing.T) {
	addr, _ := mcpServer(t, func(w http.ResponseWriter, _ []byte) { w.WriteHeader(http.StatusAccepted) })
	out, _ := relayRun(t, addr, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
}

func shortWaits(t *testing.T) {
	t.Helper()
	a, b, c := connectRetryFor, autostartRetryFor, retryEvery
	connectRetryFor, autostartRetryFor, retryEvery = 150*time.Millisecond, 150*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { connectRetryFor, autostartRetryFor, retryEvery = a, b, c })
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func TestNotificationProducesNoOutputWhenThePostFails(t *testing.T) {
	shortWaits(t)
	out, _ := relayRun(t, freeAddr(t), `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
}

func TestRequestWithNoServerGetsAnErrorInsideTheBound(t *testing.T) {
	shortWaits(t)
	addr := freeAddr(t)
	begin := time.Now()
	out, _ := relayRun(t, addr, `{"jsonrpc":"2.0","id":7,"method":"tools/list"}`+"\n")
	if time.Since(begin) > 2*time.Second {
		t.Errorf("took %v", time.Since(begin))
	}
	var reply struct {
		ID    int `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil || reply.ID != 7 || reply.Error.Code != -32000 || reply.Error.Message != "trails: no server at "+addr {
		t.Errorf("stdout = %q (%v)", out, err)
	}
}

func TestRegistersTheRepoOnceAfterTheFirstConnect(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/projects" {
			posts++
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer srv.Close()
	repo := t.TempDir()
	id, err := store.EnsureID(repo)
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	line := `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"
	runMCP([]string{"--addr", strings.TrimPrefix(srv.URL, "http://"), "--repo", repo, "--no-autostart"}, strings.NewReader(line+line), &out, &errb)
	if posts != 1 || !strings.Contains(errb.String(), "project "+id) {
		t.Errorf("posts %d, stderr %q", posts, errb.String())
	}
}
