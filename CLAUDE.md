# Trails

Trails is a project wiki that AI agents navigate one sign at a time and humans read in a browser. One Go binary, one SQLite file per project, a Vue front end embedded in the binary. Agents reach it only through its MCP; every call is logged as a trail event. The server is agent-neutral: Claude Code, Codex, Gemini CLI, Cursor and any other MCP client use the same map through a thin adapter.

`docs/context/` is the source of truth for what the system does. Read the governing file before changing code in its domain.

| When working on | Read first |
|---|---|
| Why it is shaped this way, what it refuses to do | [principles](docs/context/core/principles.md) |
| Entry kinds, caps, anchors, trust tiers, versions | [entries](docs/context/core/entries.md) |
| The tree, signs, edges, fan-out, prerequisites | [signs](docs/context/core/signs.md) |
| Sessions, events, gaps, heat | [trail](docs/context/core/trail.md) |
| Agent-facing tools and their responses | [mcp](docs/context/core/mcp.md) |
| Browser pages | [web_ui](docs/context/core/web_ui.md) |
| Storage, search, process layout | [storage](docs/context/core/storage.md) |
| Agent runtimes, hook endpoints, adapters | [adapters](docs/context/core/adapters.md) |

Full index: [docs/context/index.md](docs/context/index.md). Live plans: `docs/plans/`.

## Layout

```
server/      Go module: cmd/trails (binary), internal/{store,content,entries,signs,search,trail,mcp,web,anchor}
frontend/    Vue 3, built into server/internal/web/dist
protocol/    PROTOCOL.md, the single source of the walking protocol text
adapters/    per-runtime wiring: claude-code, codex, gemini-cli, cursor, generic
docs/        context (specs), plans (live work only), roadmap
```

## Commands

```bash
make run        # server on 127.0.0.1:7777 for the repo in the current directory, database in ~/.trail/<id>/
make test       # go test ./... in server/, adapter drift check, hook script tests
make build      # frontend build + embed + go build -> bin/trails
make proof      # screenshots of every page into .proof/, against ADDR (default 127.0.0.1:7777)
```
