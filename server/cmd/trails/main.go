package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/mcp"
	"github.com/TheAmbitiousCorry/trails/internal/project"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

const sweepEvery = 6 * time.Hour

func main() {
	args := os.Args[1:]
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
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
		default:
			fmt.Fprintf(os.Stderr, "trails: unknown command %q, want serve, register, open or status\n", cmd)
			os.Exit(2)
		}
	}
	serve(args)
}

// repoFlags collects every --repo value.
type repoFlags []string

func (r *repoFlags) String() string     { return strings.Join(*r, ",") }
func (r *repoFlags) Set(v string) error { *r = append(*r, v); return nil }

func serve(args []string) {
	userHome, _ := os.UserHomeDir()
	var repos repoFlags
	flag.Var(&repos, "repo", "repository to serve, its .trail/id created when absent; repeatable")
	addr := flag.String("addr", defaultAddr(), "listen address (default $TRAILS_ADDR, else "+fallbackAddr+")")
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

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	for _, p := range reg.List() {
		fmt.Printf("trails: project %s, db %s, repo %s\n", p.ID, p.Store.Path, p.Repo())
	}
	fmt.Printf("trails: listening on http://%s\n", ln.Addr())
	return http.Serve(ln, mcp.Handler(reg, ln.Addr().String()))
}
