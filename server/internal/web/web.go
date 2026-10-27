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
// Every data route lives under /api/p/{project}/ and acts on the project that id names in reg.
func Register(mux *http.ServeMux, reg *project.Registry, guard func(http.Handler) http.Handler) {
	routes := map[string]func(*api, *http.Request) (any, error){
		"GET /map":                     (*api).siteMap,
		"GET /search":                  (*api).search,
		"GET /graph":                   (*api).graph,
		"GET /signs/{id}":              (*api).sign,
		"GET /entries/{id}":            (*api).entry,
		"GET /entries/{id}/snippet":    (*api).snippet,
		"GET /sessions":                (*api).sessions,
		"GET /sessions/{id}":           (*api).session,
		"GET /gaps":                    (*api).gaps,
		"GET /provisional":             (*api).provisional,
		"GET /tags":                    (*api).tags,
		"GET /rejections":              (*api).rejections,
		"GET /dismissals":              (*api).dismissals,
		"POST /signs":                  (*api).createSign,
		"POST /entries/{id}/supersede": (*api).supersede,
	}
	for pattern, fn := range routes {
		method, route, _ := strings.Cut(pattern, " ")
		mux.Handle(method+" /api/p/{project}"+route, guard(withProject(reg, fn)))
	}
	mux.Handle("/", guard(spa(reg)))
}

// withProject resolves the {project} path value and builds the api for that project; an unknown id is a 404.
func withProject(reg *project.Registry, fn func(*api, *http.Request) (any, error)) http.Handler {
	return handle(func(r *http.Request) (any, error) {
		p, ok := reg.ByID(r.PathValue("project"))
		if !ok {
			return nil, notFound("unknown project")
		}
		return fn(&api{st: p.Store, repo: p.Repo()}, r)
	})
}

// spa serves the embedded build, and index.html for / and for any path under /p/ that is not a file so client-side routes survive a reload.
// Any other page path is a link from before pages had a project prefix: it redirects to the same path under the only project, or to / when the server holds none or several.
func spa(reg *project.Registry) http.Handler {
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
		if !strings.HasPrefix(p, "p/") {
			http.Redirect(w, r, unprefixed(reg, r), http.StatusFound)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	})
}

func unprefixed(reg *project.Registry, r *http.Request) string {
	p, err := reg.Sole()
	if err != nil {
		return "/"
	}
	u := "/p/" + p.ID + path.Clean(r.URL.Path)
	if r.URL.RawQuery != "" {
		u += "?" + r.URL.RawQuery
	}
	return u
}
