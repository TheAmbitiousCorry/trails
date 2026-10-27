package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const defaultAddr = "127.0.0.1:7777"

// repoAddr is the address of a repo's server: 127.0.0.1 on the port in <repo>/.trail/port, else defaultAddr.
func repoAddr(repo string) (string, error) {
	file := filepath.Join(repo, ".trail", "port")
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return defaultAddr, nil
	}
	if err != nil {
		return "", fmt.Errorf("%s: %v", file, err)
	}
	text := strings.TrimSpace(string(raw))
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("%s: want one port number from 1 to 65535, got %q", file, text)
	}
	return "127.0.0.1:" + strconv.Itoa(n), nil
}

// pickAddr returns the explicit --addr when one was given, else the address from the repo's port file.
func pickAddr(fs *flag.FlagSet, addr, repo string) (string, error) {
	given := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "addr" {
			given = true
		}
	})
	if given {
		return addr, nil
	}
	if repo == "" {
		repo, _ = os.Getwd()
	}
	return repoAddr(repo)
}

var errNoServer = errors.New("no server")

// openBrowser starts the platform opener; it is a variable so tests never launch one.
var openBrowser = func(u string) error {
	var name string
	switch runtime.GOOS {
	case "linux":
		name = "xdg-open"
	case "darwin":
		name = "open"
	default:
		return errors.New("no opener")
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return err
	}
	return exec.Command(path, u).Start()
}

// splitTarget lets the target word come before or after the flags.
func splitTarget(args []string) (string, []string) {
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		return args[0], args[1:]
	}
	return "", args
}

func getJSON(addr, path string, out any) error {
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get("http://" + addr + path)
	if err != nil {
		return errNoServer
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func noServer(stderr io.Writer, addr string) int {
	cwd, _ := os.Getwd()
	fmt.Fprintf(stderr, "no Trails server at %s; start it with: trails serve --repo %s\n", addr, cwd)
	return 1
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "trails: %v\n", err)
	return 1
}

func runOpen(args []string, stdout, stderr io.Writer) int {
	target, rest := splitTarget(args)
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addrFlag := fs.String("addr", defaultAddr, "server address (default from .trail/port)")
	repo := fs.String("repo", "", "repository whose .trail/port names the address (default the working directory)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	resolved, err := pickAddr(fs, *addrFlag, *repo)
	if err != nil {
		return fail(stderr, err)
	}
	addr := &resolved
	if target == "" && fs.NArg() > 0 {
		target = fs.Arg(0)
	}

	var path string
	switch target {
	case "", "map":
		path = "/"
	case "graph":
		path = "/graph"
	case "trail":
		var body struct {
			Sessions []struct {
				ID      string `json:"id"`
				EndedAt string `json:"ended_at"`
			} `json:"sessions"`
		}
		if err := getJSON(*addr, "/api/sessions", &body); err != nil {
			if errors.Is(err, errNoServer) {
				return noServer(stderr, *addr)
			}
			return fail(stderr, err)
		}
		path = "/sessions"
		for i, s := range body.Sessions {
			if i == 0 || s.EndedAt == "" {
				path = "/sessions/" + url.PathEscape(s.ID)
			}
			if s.EndedAt == "" {
				break
			}
		}
	default:
		fmt.Fprintf(stderr, "trails open: unknown target %q, want map, trail or graph\n", target)
		return 2
	}

	if target != "trail" {
		var probe json.RawMessage
		if err := getJSON(*addr, "/api/map", &probe); err != nil {
			if errors.Is(err, errNoServer) {
				return noServer(stderr, *addr)
			}
			return fail(stderr, err)
		}
	}

	u := "http://" + *addr + path
	fmt.Fprintln(stdout, u)
	if os.Getenv("TRAILS_NO_BROWSER") != "1" {
		_ = openBrowser(u)
	}
	return 0
}

func runStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addrFlag := fs.String("addr", defaultAddr, "server address (default from .trail/port)")
	repo := fs.String("repo", "", "repository whose .trail/port names the address (default the working directory)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	resolved, err := pickAddr(fs, *addrFlag, *repo)
	if err != nil {
		return fail(stderr, err)
	}
	addr := &resolved
	var m struct {
		Project struct {
			Title string `json:"title"`
		} `json:"project"`
		Root struct {
			Entries  int `json:"entries"`
			OpenGaps int `json:"open_gaps"`
		} `json:"root"`
	}
	if err := getJSON(*addr, "/api/map", &m); err != nil {
		if errors.Is(err, errNoServer) {
			return noServer(stderr, *addr)
		}
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "%s: %d entries, %d open gaps\n", m.Project.Title, m.Root.Entries, m.Root.OpenGaps)
	return 0
}
