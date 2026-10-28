# Trails

Trails is a project wiki that AI agents walk one sign at a time and humans read in a browser. An agent reaches it only through its MCP server, every call is logged, and the browser shows the map, the route each session took, and the gaps it found.

Trails works with any agent that speaks MCP. The repo holds three things:

- `server/`: the Go binary. One SQLite database per project, the MCP endpoint, the recorder, two hook endpoints, and the embedded web UI.
- `protocol/PROTOCOL.md`: the walking protocol, the single source every adapter embeds.
- `adapters/`: wiring for Claude Code, Codex, Gemini CLI, Cursor, and a generic MCP client. Each is an instructions snippet, an MCP config entry, and hook wiring for the read gate (a turn-back to the anchoring entry where the runtime has a before-read event, a signpost elsewhere) and the stop check, built on five shared shell scripts.

What the system does is specified in `docs/context/`; start at [the index](docs/context/index.md). Live work is in `docs/plans/`. Intent is in `docs/roadmap.md`.

## Run the server

You need Go 1.25 or newer. The web UI needs Node 22 or newer when you build it.

One server process serves every project on the machine. A repo opts in with `trails serve --repo` or `trails register`; to start the server with one project:

```bash
cd server && go run ./cmd/trails serve --repo /path/to/your/project
```

The first run writes a `.trail/id` file into the project and creates `~/.trail/<id>/trail.db`. Add more projects with more `--repo` flags, or run `trails register` inside another repo while the server is up. Commit the id file; the database stays on your machine. A committed `.trail/map.md` can declare the sign tree and tag vocabulary, and the server applies it on every start.

With the server running, open its pages from a terminal:

```bash
trails open trail    # the latest session; also: map (default), graph
trails status        # every project: entry count, open gaps
```

To run the tests:

```bash
cd server && go test ./...
```

## Connect an agent

One server per machine holds every project that has opted in, and each agent session works inside its own project. It listens on 127.0.0.1:7777 by default. Set `TRAILS_PORT` to move the port, or `TRAILS_ADDR` or `--addr` for a full address. Under Claude Code, `TRAILS_PORT` in the `env` block of `~/.claude/settings.json` moves the server the hooks and the CLI reach; the MCP entry below finds the server itself. A repo opts in with `trails register` (it creates `.trail/id` and tells the running server), or by the first `trails serve --repo`.

Pick your runtime under `adapters/` and follow its README: paste the protocol section into the project's instructions file and add the MCP entry. For Claude Code the entry is the stdio shim `trails mcp`:

```json
{
  "mcpServers": {
    "trails": { "command": "trails", "args": ["mcp"] }
  }
}
```

The `trails` binary must be on `PATH`: run `go install ./cmd/trails` from `server/`, or copy `bin/trails` (from `make build`) to a directory on `PATH`. Claude Code starts the shim in the project; it finds the repo from `CLAUDE_PROJECT_DIR`, else its working directory, relays every message to the server's `/mcp`, and sends the id in the nearest `.trail/id` as `X-Trails-Project`, so `session_start` needs no `project`. When no server answers it waits for one and starts `trails serve --repo` itself. Its flags are `--repo`, `--project`, `--addr` and `--no-autostart`.

The HTTP endpoint at `http://127.0.0.1:7777/mcp` stays for scripts and for runtimes without stdio. It is stateless: every `tools/call` is a plain POST with a JSON answer, no `Mcp-Session-Id`, so a shell script can drive it with `curl` and carry only the `session_id` that `session_start` returns. Over HTTP, `session_start` needs `project` when the server holds several projects, unless the call carries `runtime_session_id` from the session-start hook or an `X-Trails-Project` header; the refusal lists the ids the server holds.

An agent with only the MCP entry still gets the protocol: the `session_start` response carries it.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--repo` | none | A project to open at start, repeatable; its `.trail/id` is created when absent. Projects the server has served before reopen on their own, and others join through `trails register` |
| `--addr` | `$TRAILS_ADDR`, else `127.0.0.1:$TRAILS_PORT` when `TRAILS_PORT` is set, else `127.0.0.1:7777` | Listening address. Requests whose Host or Origin differ are refused. `register`, `open` and `status` take the same flag and default |
| `--home` | `~/.trail` | Where databases and media live |

## Status

The walking skeleton is complete: the server with nine MCP tools, anchors, search, write rules, the sweep, hook endpoints with adapters for four runtimes, and browser pages for the map, graph, signs, session routes and queues, with sign creation as the one browser write. It has been run against one real repo. The in-place editor and browser actions are next; see `docs/roadmap.md`.

## License

AGPL 3.0. See `LICENSE`.

## Install as a plugin

The repo is a Claude Code plugin. Add it through a marketplace that lists it, or straight from GitHub:

```bash
claude plugin marketplace add TheAmbitiousCorry/trails
claude plugin install trails@trails
```

The plugin carries the walking protocol skill, the `/trail` command, the hooks and the MCP server entry, which runs `trails mcp`. It does not carry the binary: build it with `make build` and put `bin/trails` on your PATH (or `go install` it), then the shim and the session-start hook start the one server if none runs, and the hook registers any repo with a committed `.trail/id`. A machine that needs another port sets `TRAILS_PORT` once, in the `env` block of `~/.claude/settings.json`; that moves the server, the hooks and the CLI, and the shim reads the same address rules, so the MCP entry needs no port. When `trails` is not on `PATH`, the session-start hook says so in the agent's context on every run.
