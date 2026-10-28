package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/mcp"
	"github.com/TheAmbitiousCorry/trails/internal/project"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

const sweepEvery = 6 * time.Hour

func main() {
	args := os.Args[1:]
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] == '-' {
		fmt.Fprint(os.Stderr, usage)
		if args[0] == "-h" || args[0] == "--help" || args[0] == "-help" {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "trails: %s came before the command; flags follow it, as in: trails status %s\n", args[0], args[0])
		os.Exit(2)
	}
	if len(args) > 0 && len(args[0]) > 0 {
		cmd := args[0]
		args = args[1:]
		switch cmd {
		case "serve":
		case "register":
			os.Exit(runRegister(args, os.Stdout, os.Stderr))
		case "open":
			os.Exit(runOpen(args, os.Stdout, os.Stderr))
		case "status":
			os.Exit(runStatus(args, os.Stdout, os.Stderr))
		case "mcp":
			os.Exit(runMCP(args, os.Stdin, os.Stdout, os.Stderr))
		default:
			fmt.Fprintf(os.Stderr, "trails: unknown command %q, want serve, register, open, status or mcp\n", cmd)
			os.Exit(2)
		}
	}
	serve(args)
}

const usage = `usage: trails <command> [flags]

  serve     run the server (--repo <path> repeatable, --addr host:port)
  register  tell the running server about this repository
  open      open the browser at the trail, map or graph
  status    list every project the server holds
  mcp       stdio relay for an MCP client, started in the project directory

Flags go after the command; each command takes --help.
`

// repoFlags collects every --repo value.
type repoFlags []string

func (r *repoFlags) String() string     { return strings.Join(*r, ",") }
func (r *repoFlags) Set(v string) error { *r = append(*r, v); return nil }

func serve(args []string) {
	userHome, _ := os.UserHomeDir()
	var repos repoFlags
	flag.Var(&repos, "repo", "repository to serve, its .trail/id created when absent; repeatable")
	listenDefault := envAddr()
	if listenDefault == "" {
		listenDefault = fallbackAddr
	}
	addr := flag.String("addr", listenDefault, "listen address (default $TRAILS_ADDR, else 127.0.0.1 on $TRAILS_PORT, else "+fallbackAddr+")")
	home := flag.String("home", filepath.Join(userHome, ".trail"), "Trail home")
	flag.CommandLine.Parse(args)
	if err := run(repos, *addr, *home); err != nil {
		log.Fatal(err)
	}
}

func run(repos []string, addr, home string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	reg := project.NewRegistry(home, sweepEvery)
	defer reg.Close()
	reg.OpenKnown()
	for _, repo := range repos {
		repo, err := filepath.Abs(repo)
		if err != nil {
			return err
		}
		if _, err := store.EnsureID(repo); err != nil {
			return err
		}
		if _, err := reg.Open(repo); err != nil {
			return err
		}
	}

	if held, pid, ok := readServerFile(home); ok && answers(held) {
		fmt.Fprintf(os.Stderr, "Trails: a server already holds %s at %s (pid %d); use it, or stop it first\n", home, held, pid)
		os.Exit(1)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	serverFile := filepath.Join(home, "server")
	line := fmt.Sprintf("%s %d\n", ln.Addr(), os.Getpid())
	if err := os.WriteFile(serverFile, []byte(line), 0o600); err != nil {
		return err
	}
	defer func() {
		if b, err := os.ReadFile(serverFile); err == nil && string(b) == line {
			os.Remove(serverFile)
		}
	}()
	for _, p := range reg.List() {
		fmt.Printf("trails: project %s, db %s, repo %s\n", p.ID, p.Store.Path, p.Repo())
	}
	fmt.Printf("trails: listening on http://%s\n", ln.Addr())
	srv := &http.Server{Handler: mcp.Handler(reg, ln.Addr().String())}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		srv.Shutdown(context.Background())
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// answers reports whether a Trails server replies 200 on /api/projects at addr.
func answers(addr string) bool {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://" + addr + "/api/projects")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
