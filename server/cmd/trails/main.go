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
	cwd, _ := os.Getwd()
	userHome, _ := os.UserHomeDir()
	repo := flag.String("repo", cwd, "repository to serve")
	addr := flag.String("addr", "127.0.0.1:7777", "listen address")
	home := flag.String("home", filepath.Join(userHome, ".trail"), "Trail home")
	flag.Parse()

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
