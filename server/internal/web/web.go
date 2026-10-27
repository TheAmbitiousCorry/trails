// Package web serves the JSON API, with one write route, and the embedded Vue build.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/project"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

//go:embed all:dist
var distFS embed.FS

// api is built per request from the project the request resolves to.
type api struct {
	st   *store.Store
	repo string
}

// statusError carries the HTTP status a handler wants; anything else is a 500.
type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

func notFound(msg string) error   { return &statusError{http.StatusNotFound, msg} }
func badRequest(msg string) error { return &statusError{http.StatusBadRequest, msg} }

func handle(fn func(*http.Request) (any, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := fn(r)
		var se *statusError
		switch {
		case errors.As(err, &se):
			http.Error(w, se.msg, se.code)
			return
		case err != nil:
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})
}

// Register mounts the API and the embedded front end on mux, each behind guard.
// A route acts on the project sole returns; sole's error becomes a 409 for project_ambiguous and a 404 otherwise.
func Register(mux *http.ServeMux, sole func() (*project.Project, error), guard func(http.Handler) http.Handler) {
	routes := map[string]func(*api, *http.Request) (any, error){
		"GET /api/map":                     (*api).siteMap,
		"GET /api/search":                  (*api).search,
		"GET /api/graph":                   (*api).graph,
		"GET /api/signs/{id}":              (*api).sign,
		"GET /api/entries/{id}":            (*api).entry,
		"GET /api/entries/{id}/snippet":    (*api).snippet,
		"GET /api/sessions":                (*api).sessions,
		"GET /api/sessions/{id}":           (*api).session,
		"GET /api/gaps":                    (*api).gaps,
		"GET /api/provisional":             (*api).provisional,
		"GET /api/tags":                    (*api).tags,
		"GET /api/rejections":              (*api).rejections,
		"GET /api/dismissals":              (*api).dismissals,
		"POST /api/signs":                  (*api).createSign,
		"POST /api/entries/{id}/supersede": (*api).supersede,
	}
	for pattern, fn := range routes {
		mux.Handle(pattern, guard(handle(func(r *http.Request) (any, error) {
			p, err := sole()
			switch {
			case errors.Is(err, project.ErrAmbiguous):
				return nil, &statusError{http.StatusConflict, err.Error()}
			case err != nil:
				return nil, notFound(err.Error())
			}
			return fn(&api{st: p.Store, repo: p.Repo()}, r)
		})))
	}
	mux.Handle("/", guard(spa()))
}

// spa serves the embedded build, and index.html for any path that is not a file so client-side routes survive a reload.
func spa() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if info, err := fs.Stat(sub, p); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(p, "assets/") || path.Ext(p) != "" {
			http.NotFound(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	})
}
