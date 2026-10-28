// Package project holds the projects one server serves: each owns a store, a repo path and a sweep, and the registry finds one by id or by session.
package project

import (
	"context"
	"database/sql"
	"errors"
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
	paths []string
}

func newProject(st *store.Store, paths []string) *Project {
	return &Project{ID: st.ProjectID, Store: st, paths: paths, fetch: anchor.NewFetch(&net.Resolver{})}
}

func (p *Project) DB() *sql.DB { return p.Store.DB }

// Repo is the primary checkout, the one anchors resolve against. Read it per call: a later open of the same id can replace it.
func (p *Project) Repo() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.paths) == 0 {
		return ""
	}
	return p.paths[0]
}

// Paths lists every checkout bound to the project, primary first.
func (p *Project) Paths() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return append([]string(nil), p.paths...)
}

func (p *Project) setPaths(paths []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paths = paths
}

// Checker is an anchor checker for the project's current repo.
func (p *Project) Checker() anchor.Checker {
	return anchor.Checker{Repo: p.Repo(), Fetcher: p.fetch}
}

// Title is the root sign's title.
func (p *Project) Title(ctx context.Context) (string, error) {
	var t string
	err := p.DB().QueryRowContext(ctx, `SELECT title FROM signs WHERE parent_id IS NULL`).Scan(&t)
	return t, err
}

// OpenSession finds the most recently started open session for an agent runtime's own session id, and when it started.
func (p *Project) OpenSession(ctx context.Context, runtimeID string) (id, startedAt string, found bool, err error) {
	if runtimeID == "" {
		return "", "", false, nil
	}
	err = p.DB().QueryRowContext(ctx, `SELECT id, started_at FROM sessions WHERE runtime_session_id = ? AND ended_at IS NULL ORDER BY started_at DESC, rowid DESC LIMIT 1`, runtimeID).Scan(&id, &startedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	return id, startedAt, err == nil, err
}
