# Trails

Trails is a project wiki that AI agents walk one sign at a time and humans read in a browser. An agent reaches it only through its MCP server, every call is logged, and the browser shows the map, the route each session took, and the gaps it found.

Trails works with any agent that speaks MCP. The repo holds three things:

- `server/`: the Go binary. One SQLite database per project, the MCP endpoint, the recorder, two hook endpoints, and the embedded web UI.
- `protocol/PROTOCOL.md`: the walking protocol, the single source every adapter embeds.
- `adapters/`: wiring for Claude Code, Codex, Gemini CLI, Cursor, and a generic MCP client. Each is an instructions snippet, an MCP config entry, and hook wiring for the off-trail signpost and the stop check, built on three shared shell scripts.

What the system does is specified in `docs/context/`; start at [the index](docs/context/index.md). Live work is in `docs/plans/`. Intent is in `docs/roadmap.md`.

## Run the server

You need Go 1.25 or newer. The web UI needs Node 22 or newer when you build it.

To start the server for the repo in your current directory:

```bash
cd server && go run ./cmd/trails --repo /path/to/your/project
```

The first run writes a `.trail/id` file into the project and creates `~/.trail/<id>/trail.db`. Commit the id file; the database stays on your machine. A committed `.trail/map.md` can declare the sign tree and tag vocabulary, and the server applies it on every start.

With the server running, open its pages from a terminal:

```bash
trails open trail    # the latest session; also: map (default), graph
trails status        # every project: entry count, open gaps
```

`serve` is the default subcommand, so the command above is the same as `trails serve --repo /path/to/your/project`.

To run the tests:

```bash
cd server && go test ./...
```

## Connect an agent

One server per machine holds every project that has opted in, and each agent session works inside its own project. It listens on 127.0.0.1:7777 by default; set `TRAILS_ADDR` or pass `--addr` to change that, and make the MCP entry below name the same port. A repo opts in with `trails register` (it creates `.trail/id` and tells the running server), or by the first `trails serve --repo`.

Pick your runtime under `adapters/` and follow its README: paste the protocol section into the project's instructions file and add the MCP entry. For Claude Code that is:

```json
{
  "mcpServers": {
    "trails": { "type": "http", "url": "http://127.0.0.1:7777/mcp" }
  }
}
```

An agent with only the MCP entry still gets the protocol: the `session_start` response carries it.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--repo` | none | A project to open at start, repeatable; its `.trail/id` is created when absent. Projects the server has served before reopen on their own, and others join through `trails register` |
| `--addr` | `$TRAILS_ADDR`, else `127.0.0.1:7777` | Listening address. Requests whose Host or Origin differ are refused. `register`, `open` and `status` take the same flag and default |
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

The plugin carries the walking protocol skill, the `/trail` command, the three hooks and the MCP server entry on 127.0.0.1:7777 (the default port). A machine that needs another port sets `TRAILS_ADDR` and, once, `TRAILS_PORT` to the same number in the `env` block of `~/.claude/settings.json`, which the plugin's MCP entry reads; the session-start hook prints the line to add when the two disagree. It does not carry the binary: build it with `make build` and put `bin/trails` on your PATH, then the session-start hook starts the one server if none runs and registers any repo with a committed `.trail/id`.
