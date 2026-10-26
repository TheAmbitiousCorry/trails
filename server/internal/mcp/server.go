// Package mcp serves Trail's agent tools over stateless Streamable HTTP behind a Host and Origin guard.
package mcp

import (
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TheAmbitiousCorry/trails/internal/store"
	"github.com/TheAmbitiousCorry/trails/internal/web"
)

// Handler serves /mcp, /api and the embedded front end for a server listening on addr (host:port as the client dials it).
func Handler(st *store.Store, addr string) http.Handler {
	srv := sdk.NewServer(&sdk.Implementation{Name: "trail", Title: "Trail", Version: "0.1.0"}, nil)
	registerTools(srv, st)

	mcpHandler := sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return srv },
		&sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", guard(addr, mcpHandler))
	registerAPI(mux, st, addr)
	web.Register(mux, st, func(next http.Handler) http.Handler { return guard(addr, next) })
	// An unknown /api path is a 404, never the front end's index.
	mux.Handle("/api/", guard(addr, http.NotFoundHandler()))
	return mux
}
