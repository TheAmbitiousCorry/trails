package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	runtimes map[string][]string
}

// NewRegistry opens nothing. A zero sweepEvery runs no sweeps, which tests use to keep the clock still.
func NewRegistry(home string, sweepEvery time.Duration) *Registry {
	ctx, cancel := context.WithCancel(context.Background())
	return &Registry{home: home, sweepEvery: sweepEvery, ctx: ctx, cancel: cancel,
		byID: map[string]*Project{}, sessions: map[string]string{}, runtimes: map[string][]string{}}
}

// IDBound is returned by Open when the repo's id is bound to a different repository whose checkout still exists.
type IDBound struct{ ID, Bound, Repo string }

func (e *IDBound) Error() string {
	return fmt.Sprintf(".trail/id %s belongs to the repository at %s; %s is a different repository, so give it its own id (delete its .trail/id and run trails register), or run trails register --move if the project moved", e.ID, e.Bound, e.Repo)
}

// Open registers the project in repo, which must hold .trail/id, and returns it.
// A repo that is another checkout of the repository already bound to the id is attached beside it; a different repository is refused with *IDBound. Bound paths gone from disk do not count.
func (r *Registry) Open(repo string) (*Project, error) { return r.open(repo, false) }

// OpenMove is Open that makes repo the primary checkout even when it is a different repository.
func (r *Registry) OpenMove(repo string) (*Project, error) { return r.open(repo, true) }

func (r *Registry) open(repo string, move bool) (*Project, error) {
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
	p, held := r.byID[id]
	var bound []string
	if held {
		bound = p.Paths()
	} else {
		bound = readRepoFile(r.home, id)
	}
	paths := existingDirs(bound)
	switch {
	case move:
		paths = append([]string{repo}, without(paths, repo)...)
	case slices.Contains(paths, repo):
	case len(paths) == 0:
		paths = []string{repo}
	case sameRepository(paths[0], repo):
		paths = append(paths, repo)
		log.Printf("trails: project %s also at %s", id, repo)
	default:
		return nil, &IDBound{ID: id, Bound: paths[0], Repo: repo}
	}
	if held {
		if old := p.Repo(); old != paths[0] {
			log.Printf("trails: project %s moved from %s to %s", id, old, paths[0])
		}
		p.setPaths(paths)
		return p, writeRepoFile(r.home, id, paths)
	}
	st, err := store.Open(r.home, repo)
	if err != nil {
		return nil, err
	}
	if err := writeRepoFile(r.home, id, paths); err != nil {
		st.Close()
		return nil, err
	}
	p = newProject(st, paths)
	r.byID[id] = p
	r.startSweep(p)
	return p, nil
}

func readRepoFile(home, id string) []string {
	b, err := os.ReadFile(filepath.Join(home, id, "repo"))
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func writeRepoFile(home, id string, paths []string) error {
	return os.WriteFile(filepath.Join(home, id, "repo"), []byte(strings.Join(paths, "\n")+"\n"), 0o600)
}

func existingDirs(paths []string) []string {
	var out []string
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func without(paths []string, drop string) []string {
	var out []string
	for _, p := range paths {
		if p != drop {
			out = append(out, p)
		}
	}
	return out
}

func gitOut(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(b)), err
}

func sameRepository(a, b string) bool {
	ca, errA := gitOut(a, "rev-parse", "--path-format=absolute", "--git-common-dir")
	cb, errB := gitOut(b, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if errA == nil && errB == nil {
		return filepath.Clean(ca) == filepath.Clean(cb)
	}
	ua, errA := gitOut(a, "config", "--get", "remote.origin.url")
	ub, errB := gitOut(b, "config", "--get", "remote.origin.url")
	return errA == nil && errB == nil && ua != "" && ua == ub
}

// dropMissingPaths forgets bound checkouts that are gone from disk, keeping the last one so the project always has a repo.
func (r *Registry) dropMissingPaths(p *Project) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur := p.Paths()
	live := existingDirs(cur)
	if len(live) == 0 || len(live) == len(cur) {
		return
	}
	p.setPaths(live)
	if err := writeRepoFile(r.home, p.ID, live); err != nil {
		log.Printf("trails: %s: %v", p.ID, err)
	}
}

// OpenKnown opens every project under home whose repo file names a directory that still holds its id.
func (r *Registry) OpenKnown() {
	dirs, err := os.ReadDir(r.home)
	if err != nil {
		return
	}
	for _, d := range dirs {
		for _, repo := range readRepoFile(r.home, d.Name()) {
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
			r.dropMissingPaths(p)
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
	var cached *Project
	ok := false
	if ids := r.runtimes[runtimeID]; len(ids) > 0 {
		cached, ok = r.byID[ids[0]]
	}
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
		r.runtimes[runtimeID] = prepend(r.runtimes[runtimeID], best.ID)
		r.mu.Unlock()
	}
	return best, bestID, best != nil, nil
}

// Resolve places a call in its project: by the named project for session_start, else by session id.
func (r *Registry) Resolve(ctx context.Context, c trail.Call) (*Project, *sql.DB, error) {
	var p *Project
	if c.Mint {
		var err error
		if p, err = r.forStart(c.Project, c.Hint, c.Runtime); err != nil {
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

func prepend(ids []string, id string) []string {
	out := []string{id}
	for _, o := range ids {
		if o != id {
			out = append(out, o)
		}
	}
	return out
}

// RememberRuntime records that the agent runtime's session id belongs to the project id, most recently registered first, so a later session_start carrying only that id is placed.
func (r *Registry) RememberRuntime(runtimeID, projectID string) {
	if runtimeID == "" {
		return
	}
	r.mu.Lock()
	r.runtimes[runtimeID] = prepend(r.runtimes[runtimeID], projectID)
	r.mu.Unlock()
}

// forStart places a session_start: the named project wins, else the project the request header named, else the project the session-start hook registered for the runtime id, else the only open project.
// A header id the server does not hold is refused rather than skipped, so a copied id cannot place a session elsewhere.
func (r *Registry) forStart(id, hint, runtime string) (*Project, error) {
	if id == "" && hint != "" {
		if p, ok := r.ByID(hint); ok {
			return p, nil
		}
		return nil, &trail.Refusal{Rule: "project_unknown", Fix: fmt.Sprintf("the server does not hold project %s from the %s header; run: trails register, or pass project", hint, trail.ProjectHeader)}
	}
	if id != "" {
		if p, ok := r.ByID(id); ok {
			return p, nil
		}
	} else {
		var held []*Project
		r.mu.Lock()
		if runtime != "" {
			for _, mid := range r.runtimes[runtime] {
				if p, ok := r.byID[mid]; ok {
					held = append(held, p)
				}
			}
		}
		r.mu.Unlock()
		if len(held) == 1 {
			return held[0], nil
		}
		if len(held) > 1 {
			names := make([]string, len(held))
			for i, p := range held {
				names[i] = fmt.Sprintf("%s (%s)", p.ID, filepath.Base(p.Repo()))
			}
			return nil, &trail.Refusal{Rule: "project_unknown", Fix: "pass project, one of: " + strings.Join(names, ", ")}
		}
		if p, err := r.Sole(); err == nil {
			return p, nil
		}
	}
	names := make([]string, 0)
	for _, p := range r.List() {
		names = append(names, fmt.Sprintf("%s (%s)", p.ID, filepath.Base(p.Repo())))
	}
	held := "this server holds no project"
	if len(names) > 0 {
		held = "this server holds: " + strings.Join(names, ", ")
	}
	return nil, &trail.Refusal{Rule: "project_unknown", Fix: fmt.Sprintf("pass project, the id in the repo's .trail/id, or pass runtime_session_id from the session-start hook; %s", held)}
}
