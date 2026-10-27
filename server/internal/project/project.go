// Package project holds the projects one server serves: each owns a store, a repo path and a sweep, and the registry finds one by id or by session.
package project

import (
	"database/sql"
	"net"
	"sync"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

// Project is everything one project owns while the server runs.
type Project struct {
	ID    string
	Store *store.Store

	fetch anchor.Fetcher
	mu    sync.RWMutex
	repo  string
}

func newProject(st *store.Store, repo string) *Project {
	return &Project{ID: st.ProjectID, Store: st, repo: repo, fetch: anchor.NewFetch(&net.Resolver{})}
}

func (p *Project) DB() *sql.DB { return p.Store.DB }

// Repo is the checkout anchors resolve against. Read it per call: a later open of the same id replaces it.
func (p *Project) Repo() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.repo
}

func (p *Project) setRepo(repo string) (old string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	old, p.repo = p.repo, repo
	return old
}

// Checker is an anchor checker for the project's current repo.
func (p *Project) Checker() anchor.Checker {
	return anchor.Checker{Repo: p.Repo(), Fetcher: p.fetch}
}
