// Package mcp serves Trail's agent tools over stateless Streamable HTTP behind a Host and Origin guard.
package mcp

import (
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TheAmbitiousCorry/trails/internal/project"
	"github.com/TheAmbitiousCorry/trails/internal/web"
)

// Version is what initialize reports as serverInfo.version; the release commit bumps it with .claude-plugin/plugin.json.
const Version = "0.7.3"

// Handler serves /mcp, /api and the embedded front end for a server listening on addr (host:port as the client dials it).
func Handler(reg *project.Registry, addr string) http.Handler {
	srv := sdk.NewServer(&sdk.Implementation{Name: "trail", Title: "Trail", Version: Version}, nil)
	registerTools(srv, reg)

	mcpHandler := sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return srv },
		&sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", guard(addr, mcpHandler))
	registerAPI(mux, reg, addr)
	web.Register(mux, reg, func(next http.Handler) http.Handler { return guard(addr, next) })
	// An unknown /api path is a 404, never the front end's index.
	mux.Handle("/api/", guard(addr, http.NotFoundHandler()))
	return mux
}
