package main

import (
	"bytes"
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
	"strings"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

const fallbackAddr = "127.0.0.1:7777"

// defaultAddr is the server address when --addr is not given: $TRAILS_ADDR, else fallbackAddr.
func defaultAddr() string {
	if a := os.Getenv("TRAILS_ADDR"); a != "" {
		return a
	}
	return fallbackAddr
}

var (
	errNoServer = errors.New("no server")
	errNotFound = errors.New("not found")
)

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
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// projectID reads the id from the nearest .trail/id at or above repo, or above the working directory when repo is empty, as the hook scripts do.
func projectID(repo string) (string, error) {
	if repo == "" {
		repo, _ = os.Getwd()
	}
	dir, err := filepath.Abs(repo)
	if err != nil {
		return "", err
	}
	for d := dir; ; d = filepath.Dir(d) {
		id, err := store.ReadID(d)
		if !errors.Is(err, store.ErrNoID) {
			return id, err
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("no .trail/id at or above %s; run: trails register", dir)
		}
	}
}

// getProject is getJSON for a route under /api/p/<id>; a project the server does not hold gets the way to register it.
func getProject(addr, id, route string, out any) error {
	err := getJSON(addr, "/api/p/"+url.PathEscape(id)+route, out)
	if errors.Is(err, errNotFound) {
		return fmt.Errorf("the server at %s does not hold project %s; run: trails register", addr, id)
	}
	return err
}

func failServer(stderr io.Writer, addr string, err error) int {
	if errors.Is(err, errNoServer) {
		return noServer(stderr, addr)
	}
	return fail(stderr, err)
}

func noServer(stderr io.Writer, addr string) int {
	fmt.Fprintf(stderr, "no Trails server at %s; start it with: trails serve\n", addr)
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
	addr := fs.String("addr", defaultAddr(), "server address (default $TRAILS_ADDR, else "+fallbackAddr+")")
	repo := fs.String("repo", "", "repository whose project to open (default the working directory)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if target == "" && fs.NArg() > 0 {
		target = fs.Arg(0)
	}
	id, err := projectID(*repo)
	if err != nil {
		return fail(stderr, err)
	}
	prefix := "/p/" + url.PathEscape(id)

	var path string
	switch target {
	case "", "map":
		path = prefix + "/"
	case "graph":
		path = prefix + "/graph"
	case "trail":
		var body struct {
			Sessions []struct {
				ID      string `json:"id"`
				EndedAt string `json:"ended_at"`
			} `json:"sessions"`
		}
		if err := getProject(*addr, id, "/sessions", &body); err != nil {
			return failServer(stderr, *addr, err)
		}
		path = prefix + "/sessions"
		for i, s := range body.Sessions {
			if i == 0 || s.EndedAt == "" {
				path = prefix + "/sessions/" + url.PathEscape(s.ID)
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
		if err := getProject(*addr, id, "/map", &probe); err != nil {
			return failServer(stderr, *addr, err)
		}
	}

	u := "http://" + *addr + path
	fmt.Fprintln(stdout, u)
	if os.Getenv("TRAILS_NO_BROWSER") != "1" {
		_ = openBrowser(u)
	}
	return 0
}

// runStatus prints one line per project the server holds, or the one --repo names.
func runStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", defaultAddr(), "server address (default $TRAILS_ADDR, else "+fallbackAddr+")")
	repo := fs.String("repo", "", "show only the project of this repository")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var ids []string
	if *repo != "" {
		id, err := projectID(*repo)
		if err != nil {
			return fail(stderr, err)
		}
		ids = []string{id}
	} else {
		var list struct {
			Projects []struct {
				ID string `json:"id"`
			} `json:"projects"`
		}
		if err := getJSON(*addr, "/api/projects", &list); err != nil {
			return failServer(stderr, *addr, err)
		}
		for _, p := range list.Projects {
			ids = append(ids, p.ID)
		}
		if len(ids) == 0 {
			fmt.Fprintf(stdout, "no projects at %s; register one with: trails register\n", *addr)
			return 0
		}
	}
	for _, id := range ids {
		var m struct {
			Project struct {
				Title string `json:"title"`
			} `json:"project"`
			Root struct {
				Entries  int `json:"entries"`
				OpenGaps int `json:"open_gaps"`
			} `json:"root"`
		}
		if err := getProject(*addr, id, "/map", &m); err != nil {
			return failServer(stderr, *addr, err)
		}
		fmt.Fprintf(stdout, "%s (%s): %d entries, %d open gaps\n", m.Project.Title, id, m.Root.Entries, m.Root.OpenGaps)
	}
	return 0
}

// runRegister creates .trail/id in the repo when it has none, then asks the server to open the project.
func runRegister(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", defaultAddr(), "server address (default $TRAILS_ADDR, else "+fallbackAddr+")")
	repo := fs.String("repo", "", "repository to register (default the working directory)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *repo == "" {
		*repo, _ = os.Getwd()
	}
	abs, err := filepath.Abs(*repo)
	if err != nil {
		return fail(stderr, err)
	}
	if _, err := store.EnsureID(abs); err != nil {
		return fail(stderr, err)
	}
	body, _ := json.Marshal(map[string]string{"repo": abs})
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Post("http://"+*addr+"/api/projects", "application/json", bytes.NewReader(body))
	if err != nil {
		return noServer(stderr, *addr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return fail(stderr, fmt.Errorf("register %s: %s %s", abs, resp.Status, strings.TrimSpace(string(msg))))
	}
	var ref struct{ ID, Title string }
	if err := json.NewDecoder(resp.Body).Decode(&ref); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "registered %s (%s) at %s\n", ref.Title, ref.ID, *addr)
	return 0
}
