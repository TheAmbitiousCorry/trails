package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/store"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

var (
	// ErrAmbiguous is returned by Sole when more than one project is open.
	ErrAmbiguous = errors.New("project_ambiguous")
	// ErrNone is returned by Sole when no project is open.
	ErrNone = errors.New("no project open")
)

// Registry is the set of open projects. It is safe for concurrent use.
type Registry struct {
	home       string
	sweepEvery time.Duration
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup

	mu       sync.Mutex
	byID     map[string]*Project
	sessions map[string]string
	runtimes map[string]string
}

// NewRegistry opens nothing. A zero sweepEvery runs no sweeps, which tests use to keep the clock still.
func NewRegistry(home string, sweepEvery time.Duration) *Registry {
	ctx, cancel := context.WithCancel(context.Background())
	return &Registry{home: home, sweepEvery: sweepEvery, ctx: ctx, cancel: cancel,
		byID: map[string]*Project{}, sessions: map[string]string{}, runtimes: map[string]string{}}
}

// Open registers the project in repo, which must hold .trail/id, and returns it.
// A second open of an id already held keeps the open store and moves the project to the new repo path.
func (r *Registry) Open(repo string) (*Project, error) {
	repo, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	id, err := store.ReadID(repo)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.byID[id]; ok {
		if old := p.setRepo(repo); old != repo {
			log.Printf("trails: project %s moved from %s to %s", id, old, repo)
		}
		return p, writeRepoFile(r.home, id, repo)
	}
	st, err := store.Open(r.home, repo)
	if err != nil {
		return nil, err
	}
	if err := writeRepoFile(r.home, id, repo); err != nil {
		st.Close()
		return nil, err
	}
	p := newProject(st, repo)
	r.byID[id] = p
	r.startSweep(p)
	return p, nil
}

func writeRepoFile(home, id, repo string) error {
	return os.WriteFile(filepath.Join(home, id, "repo"), []byte(repo+"\n"), 0o600)
}

// OpenKnown opens every project under home whose repo file names a directory that still holds its id.
func (r *Registry) OpenKnown() {
	dirs, err := os.ReadDir(r.home)
	if err != nil {
		return
	}
	for _, d := range dirs {
		b, err := os.ReadFile(filepath.Join(r.home, d.Name(), "repo"))
		if err != nil {
			continue
		}
		repo := strings.TrimSpace(string(b))
		if fi, err := os.Stat(repo); err != nil || !fi.IsDir() {
			continue
		}
		if id, err := store.ReadID(repo); err != nil || id != d.Name() {
			log.Printf("trails: skipping %s: %s does not hold id %s", d.Name(), repo, d.Name())
			continue
		}
		if _, err := r.Open(repo); err != nil {
			log.Printf("trails: skipping %s: %v", d.Name(), err)
		}
	}
}

func (r *Registry) startSweep(p *Project) {
	if r.sweepEvery <= 0 {
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		tick := time.NewTicker(r.sweepEvery)
		defer tick.Stop()
		for {
			rep, err := trail.Sweep(r.ctx, p.DB(), p.Checker(), time.Now)
			if err != nil {
				log.Printf("trails: %s: sweep: %v", p.ID, err)
			} else {
				log.Printf("trails: %s: sweep checked %d, dormant %d, deleted %d, sessions closed %d", p.ID, rep.Checked, rep.Dormant, rep.Deleted, rep.Closed)
			}
			select {
			case <-r.ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

// Close stops the sweeps and closes every store.
func (r *Registry) Close() error {
	r.cancel()
	r.wg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	var err error
	for id, p := range r.byID {
		err = errors.Join(err, p.Store.Close())
		delete(r.byID, id)
	}
	return err
}

func (r *Registry) ByID(id string) (*Project, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[id]
	return p, ok
}

// List returns the open projects ordered by id.
func (r *Registry) List() []*Project {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Project, 0, len(r.byID))
	for _, p := range r.byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Sole returns the only open project.
func (r *Registry) Sole() (*Project, error) {
	ps := r.List()
	switch len(ps) {
	case 0:
		return nil, ErrNone
	case 1:
		return ps[0], nil
	}
	return nil, ErrAmbiguous
}

// BySession finds the project whose sessions table holds id. Sessions are never deleted, so a remembered match stays right.
func (r *Registry) BySession(ctx context.Context, id string) (*Project, bool, error) {
	r.mu.Lock()
	p, ok := r.byID[r.sessions[id]]
	r.mu.Unlock()
	if ok {
		return p, true, nil
	}
	for _, p := range r.List() {
		var n int
		if err := p.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, id).Scan(&n); err != nil {
			return nil, false, err
		}
		if n > 0 {
			r.mu.Lock()
			r.sessions[id] = p.ID
			r.mu.Unlock()
			return p, true, nil
		}
	}
	return nil, false, nil
}

// ByRuntime finds the open session an agent runtime's own session id maps to.
// The project that last answered for the id is asked first and wins while it still holds an open session for it; otherwise every project is asked and the most recently started session wins.
func (r *Registry) ByRuntime(ctx context.Context, runtimeID string) (*Project, string, bool, error) {
	if runtimeID == "" {
		return nil, "", false, nil
	}
	r.mu.Lock()
	cached, ok := r.byID[r.runtimes[runtimeID]]
	r.mu.Unlock()
	if ok {
		if sid, _, found, err := cached.OpenSession(ctx, runtimeID); err != nil || found {
			return cached, sid, found, err
		}
	}
	var best *Project
	var bestID, bestAt string
	for _, p := range r.List() {
		sid, at, found, err := p.OpenSession(ctx, runtimeID)
		if err != nil {
			return nil, "", false, err
		}
		if found && (best == nil || at > bestAt) {
			best, bestID, bestAt = p, sid, at
		}
	}
	if best != nil {
		r.mu.Lock()
		r.runtimes[runtimeID] = best.ID
		r.mu.Unlock()
	}
	return best, bestID, best != nil, nil
}

// Resolve places a call in its project: by the named project for session_start, else by session id.
func (r *Registry) Resolve(ctx context.Context, c trail.Call) (*Project, *sql.DB, error) {
	var p *Project
	if c.Mint {
		var err error
		if p, err = r.forStart(c.Project, c.Runtime); err != nil {
			return nil, nil, err
		}
	} else {
		var ok bool
		var err error
		if p, ok, err = r.BySession(ctx, c.SessionID); err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, &trail.Refusal{Rule: "unknown_session", Fix: "call session_start and use the session_id it returns"}
		}
	}
	return p, p.DB(), nil
}

// RememberRuntime records that the agent runtime's session id belongs to the project id, so a later session_start carrying only that id is placed.
func (r *Registry) RememberRuntime(runtimeID, projectID string) {
	if runtimeID == "" {
		return
	}
	r.mu.Lock()
	r.runtimes[runtimeID] = projectID
	r.mu.Unlock()
}

// forStart places a session_start: the named project wins, else the project the session-start hook registered for the runtime id, else the only open project.
func (r *Registry) forStart(id, runtime string) (*Project, error) {
	if id != "" {
		if p, ok := r.ByID(id); ok {
			return p, nil
		}
	} else {
		r.mu.Lock()
		p, ok := r.byID[r.runtimes[runtime]]
		r.mu.Unlock()
		if ok && runtime != "" {
			return p, nil
		}
		if p, err := r.Sole(); err == nil {
			return p, nil
		}
	}
	ids := make([]string, 0)
	for _, p := range r.List() {
		ids = append(ids, p.ID)
	}
	held := "this server holds no project"
	if len(ids) > 0 {
		held = "this server holds: " + strings.Join(ids, ", ")
	}
	return nil, &trail.Refusal{Rule: "project_unknown", Fix: fmt.Sprintf("pass project, the id in the repo's .trail/id, or pass runtime_session_id from the session-start hook; %s", held)}
}
