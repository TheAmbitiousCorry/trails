// Package web serves the read-only JSON API and the embedded Vue build.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

//go:embed all:dist
var distFS embed.FS

type api struct{ st *store.Store }

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

// Register mounts the read-only API and the embedded front end on mux, each behind guard.
func Register(mux *http.ServeMux, st *store.Store, guard func(http.Handler) http.Handler) {
	a := &api{st: st}
	routes := map[string]func(*http.Request) (any, error){
		"GET /api/map":                  a.siteMap,
		"GET /api/signs/{id}":           a.sign,
		"GET /api/entries/{id}":         a.entry,
		"GET /api/entries/{id}/snippet": a.snippet,
		"GET /api/sessions":             a.sessions,
		"GET /api/sessions/{id}":        a.session,
		"GET /api/gaps":                 a.gaps,
		"GET /api/provisional":          a.provisional,
		"GET /api/tags":                 a.tags,
		"GET /api/rejections":           a.rejections,
		"GET /api/dismissals":           a.dismissals,
	}
	for pattern, fn := range routes {
		mux.Handle(pattern, guard(handle(fn)))
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
