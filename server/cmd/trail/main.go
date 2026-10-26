package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/TheAmbitiousCorry/trails/internal/mcp"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

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

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Printf("trail: project %s, db %s, listening on http://%s\n", st.ProjectID, st.Path, ln.Addr())
	return http.Serve(ln, mcp.Handler(st, ln.Addr().String()))
}
