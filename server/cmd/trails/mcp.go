package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

// The waits are variables so a test can shorten them.
var (
	connectRetryFor   = 3 * time.Second
	autostartRetryFor = 10 * time.Second
	retryEvery        = 200 * time.Millisecond
)

// projectRoot finds the nearest .trail/id at or above start and returns the id and the directory holding it.
func projectRoot(start string) (id, dir string) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", ""
	}
	for d := dir; ; d = filepath.Dir(d) {
		if id, err := store.ReadID(d); err == nil {
			return id, d
		}
		if filepath.Dir(d) == d {
			return "", ""
		}
	}
}

type relay struct {
	addr      func() string
	project   string
	root      string
	autostart bool
	started   bool
	connected bool
	client    *http.Client
	stdout    io.Writer
	stderr    io.Writer
	serveArgs []string
}

// runMCP relays newline-delimited JSON-RPC from stdin to the server's /mcp endpoint and writes each reply as one line.
func runMCP(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addrFlag := fs.String("addr", "", "server address (default $TRAILS_ADDR, else 127.0.0.1 on $TRAILS_PORT, else the live server in ~/.trail/server, else "+fallbackAddr+")")
	projectFlag := fs.String("project", "", "project id to send (default the nearest .trail/id)")
	repo := fs.String("repo", "", "directory to look for .trail/id from (default $CLAUDE_PROJECT_DIR, else the working directory)")
	noAutostart := fs.Bool("no-autostart", false, "do not start a server when none answers")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	start := *repo
	if start == "" {
		// Claude Code promises the project root in this variable, not as the working directory.
		start = os.Getenv("CLAUDE_PROJECT_DIR")
	}
	if start == "" {
		start, _ = os.Getwd()
	}
	id, root := projectRoot(start)
	if *projectFlag != "" {
		id = *projectFlag
	}
	r := &relay{
		project:   id,
		root:      root,
		autostart: !*noAutostart,
		client:    &http.Client{Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext}},
		stdout:    stdout,
		stderr:    stderr,
	}
	if *addrFlag != "" {
		a := *addrFlag
		r.addr = func() string { return a }
		r.serveArgs = []string{"--addr", a}
	} else {
		r.addr = defaultAddr
	}
	shown := id
	if shown == "" {
		shown = "none"
	}
	fmt.Fprintf(stderr, "trails mcp: cwd %s, project %s, server %s\n", start, shown, r.addr())

	in := bufio.NewReader(stdin)
	for {
		line, err := in.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			r.handle(line)
		}
		if err != nil {
			return 0
		}
	}
}

func (r *relay) write(b []byte) {
	if bytes.IndexByte(b, '\n') >= 0 {
		var c bytes.Buffer
		if json.Compact(&c, b) == nil {
			b = c.Bytes()
		}
	}
	r.stdout.Write(append(append([]byte{}, b...), '\n'))
}

// handle sends one stdin line and writes its reply, or the error reply when the line is a request that cannot be sent.
func (r *relay) handle(line []byte) {
	body, err := r.send(line)
	if err != nil {
		if reply := errorReply(line, err); reply != nil {
			r.write(reply)
		}
		return
	}
	if len(body) > 0 {
		r.write(body)
	}
	if !r.connected {
		r.connected = true
		r.registerRepo()
	}
}

// registerRepo tells the server about the repo once, so a server the shim started holds the project.
func (r *relay) registerRepo() {
	if r.root == "" {
		return
	}
	b, _ := json.Marshal(map[string]string{"repo": r.root})
	resp, err := r.client.Post("http://"+r.addr()+"/api/projects", "application/json", bytes.NewReader(b))
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// errNoConnect marks a POST that never reached the server.
type errNoConnect struct{ addr string }

func (e errNoConnect) Error() string { return "trails: no server at " + e.addr }

// send posts the line, waiting for a server when none answers, and returns the single-line reply (nil for none).
func (r *relay) send(line []byte) ([]byte, error) {
	body, err := r.post(line)
	if !isDown(err) {
		return body, err
	}
	deadline := time.Now().Add(connectRetryFor)
	for time.Now().Before(deadline) {
		time.Sleep(retryEvery)
		if body, err = r.post(line); !isDown(err) {
			return body, err
		}
	}
	if r.autostart && !r.started {
		r.started = true
		r.startServer()
	}
	if r.autostart {
		deadline = time.Now().Add(autostartRetryFor)
		for time.Now().Before(deadline) {
			time.Sleep(retryEvery)
			if body, err = r.post(line); !isDown(err) {
				return body, err
			}
		}
	}
	return nil, err
}

func isDown(err error) bool {
	_, down := err.(errNoConnect)
	return down
}

// startServer runs trails serve detached from this process, as the session-start hook does.
func (r *relay) startServer() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	args := append([]string{"serve"}, r.serveArgs...)
	if r.root != "" {
		args = append(args, "--repo", r.root)
	}
	cmd := exec.Command(exe, args...)
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".trail")
		os.MkdirAll(dir, 0o700)
		if log, err := os.OpenFile(filepath.Join(dir, "server.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			defer log.Close()
			cmd.Stdout, cmd.Stderr = log, log
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		cmd.Process.Release()
	}
}

func (r *relay) post(line []byte) ([]byte, error) {
	addr := r.addr()
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/mcp", bytes.NewReader(line))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if r.project != "" {
		req.Header.Set("X-Trails-Project", r.project)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, errNoConnect{addr}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		io.Copy(io.Discard, resp.Body)
		return nil, nil
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return firstEventData(resp.Body), nil
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errNoConnect{addr}
	}
	b = bytes.TrimSpace(b)
	if resp.StatusCode >= 400 && !json.Valid(b) {
		return nil, fmt.Errorf("trails: server replied %s: %s", resp.Status, truncate(string(b), 200))
	}
	return b, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// firstEventData returns the data lines of the first SSE event that has any, joined by newlines.
func firstEventData(body io.Reader) []byte {
	br := bufio.NewReader(body)
	var data [][]byte
	for {
		line, err := br.ReadBytes('\n')
		line = bytes.TrimRight(line, "\r\n")
		switch {
		case bytes.HasPrefix(line, []byte("data:")):
			data = append(data, bytes.TrimPrefix(bytes.TrimPrefix(line, []byte("data:")), []byte(" ")))
		case len(line) == 0 && len(data) > 0:
			return bytes.Join(data, []byte("\n"))
		}
		if err != nil {
			return bytes.Join(data, []byte("\n"))
		}
	}
}

// errorReply is the JSON-RPC error for the requests in line; nil when line holds only notifications or is not JSON-RPC.
func errorReply(line []byte, cause error) []byte {
	msg := cause.Error()
	one := func(id json.RawMessage) map[string]any {
		return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32000, "message": msg}}
	}
	if line[0] == '[' {
		var batch []struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(line, &batch) != nil {
			return nil
		}
		var out []map[string]any
		for _, m := range batch {
			if len(m.ID) > 0 {
				out = append(out, one(m.ID))
			}
		}
		if len(out) == 0 {
			return nil
		}
		b, _ := json.Marshal(out)
		return b
	}
	var m struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(line, &m) != nil || len(m.ID) == 0 {
		return nil
	}
	b, _ := json.Marshal(one(m.ID))
	return b
}
