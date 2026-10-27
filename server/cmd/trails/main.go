package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"

	"github.com/TheAmbitiousCorry/trails/internal/mcp"
	"github.com/TheAmbitiousCorry/trails/internal/store"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

const sweepEvery = 6 * time.Hour

// sweepLoop runs the sweep once now and then on every tick until ctx ends.
func sweepLoop(ctx context.Context, st *store.Store) {
	checker := anchor.Checker{Repo: st.Repo, Fetcher: anchor.NewFetch(&net.Resolver{})}
	tick := time.NewTicker(sweepEvery)
	defer tick.Stop()
	for {
		rep, err := trail.Sweep(ctx, st.DB, checker, time.Now)
		if err != nil {
			log.Printf("trails: sweep: %v", err)
		} else {
			log.Printf("trails: sweep checked %d, dormant %d, deleted %d, sessions closed %d", rep.Checked, rep.Dormant, rep.Deleted, rep.Closed)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func main() {
	args := os.Args[1:]
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd := args[0]
		args = args[1:]
		switch cmd {
		case "serve":
		case "open":
			os.Exit(runOpen(args, os.Stdout, os.Stderr))
		case "status":
			os.Exit(runStatus(args, os.Stdout, os.Stderr))
		default:
			fmt.Fprintf(os.Stderr, "trails: unknown command %q, want serve, open or status\n", cmd)
			os.Exit(2)
		}
	}
	serve(args)
}

func serve(args []string) {
	cwd, _ := os.Getwd()
	userHome, _ := os.UserHomeDir()
	repo := flag.String("repo", cwd, "repository to serve")
	addr := flag.String("addr", defaultAddr, "listen address")
	home := flag.String("home", filepath.Join(userHome, ".trail"), "Trail home")
	flag.CommandLine.Parse(args)

	if err := run(*repo, *addr, *home); err != nil {
		log.Fatal(err)
	}
}

func run(repo, addr, home string) error {
	repo, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	st, err := store.Open(home, repo)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go sweepLoop(ctx, st)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Printf("trails: project %s, db %s, listening on http://%s\n", st.ProjectID, st.Path, ln.Addr())
	return http.Serve(ln, mcp.Handler(st, ln.Addr().String()))
}
