# Trails

Trails is a project wiki that AI agents navigate one sign at a time and humans read in a browser. One Go binary, one SQLite file per project, a Vue front end embedded in the binary. Agents reach it only through its MCP; every call is logged as a trail event.

`docs/context/` is the source of truth for what the system does. Read the governing file before changing code in its domain.

| When working on | Read first |
|---|---|
| Why it is shaped this way, what it refuses to do | [principles](docs/context/core/principles.md) |
| Entry kinds, caps, anchors, trust tiers, versions | [entries](docs/context/core/entries.md) |
| The tree, signs, edges, fan-out, prerequisites | [signs](docs/context/core/signs.md) |
| Sessions, events, gaps, heat | [trail](docs/context/core/trail.md) |
| Agent-facing tools and their responses | [mcp](docs/context/core/mcp.md) |
| Browser pages and in-place editing | [web_ui](docs/context/core/web_ui.md) |
| Storage, search, process layout | [storage](docs/context/core/storage.md) |

Full index: [docs/context/index.md](docs/context/index.md). Live plans: `docs/plans/`.

## Layout

```
server/      Go module: cmd/trail (binary), internal/{store,entries,signs,trail,mcp,web,anchor}
frontend/    Vue 3 + Tiptap, built into server/internal/web/dist
skills/      Claude Code skill that teaches agents the walking protocol
mods/        Claude Code hooks: off-trail signpost, Stop hook
docs/        context (specs), plans (live work only), roadmap
```

## Commands

```bash
make run        # server on 127.0.0.1:7777 for the repo in the current directory, database in ~/.trail/<id>/
make test       # go test ./... in server/
make build      # frontend build + embed + go build -> bin/trail
```
