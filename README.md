# Trails

Trails is a project wiki that AI agents walk one sign at a time and humans read in a browser. An agent reaches it only through its MCP server, every call is logged, and the browser shows the map, the route each session took, and the gaps it found.

The repo holds three things:

- `server/`: the Go binary. One SQLite database per project, the MCP endpoint, the recorder, and the embedded web UI.
- `skills/trails/`: the Claude Code skill that teaches an agent the walking protocol.
- `mods/`: the Claude Code hooks, the off-trail signpost on native file reads and the Stop hook that holds a session open until its gaps are resolved. They land in step 10 of the walking skeleton and the directory does not exist yet.

What the system does is specified in `docs/context/`; start at [the index](docs/context/index.md). Live work is in `docs/plans/`. Intent is in `docs/roadmap.md`.

## Run the server

You need Go 1.25 or newer. The web UI needs Node 22 or newer when you build it.

To start the server for the repo in your current directory:

```bash
cd server && go run ./cmd/trails --repo /path/to/your/project
```

The first run writes a `.trail/id` file into the project and creates `~/.trail/<id>/trail.db`. Commit the id file; the database stays on your machine.

To run the tests:

```bash
cd server && go test ./...
```

## Connect an agent

Add the server to a Claude Code MCP config:

```json
{
  "mcpServers": {
    "trails": { "type": "http", "url": "http://127.0.0.1:7777/mcp" }
  }
}
```

Then install the skill and hooks from `skills/` and `mods/` into your Claude Code plugins. The skill tells the agent to start every task with `session_start`, search before it greps, and look at a module's sign before editing it.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--repo` | current directory | The project the database describes |
| `--addr` | `127.0.0.1:7777` | Listening address. Requests whose Host or Origin differ are refused |
| `--home` | `~/.trail` | Where databases and media live |

## Status

The walking skeleton is in progress. Step 1 of 12 is done: the store, the recorder, and the `session_start` and `read` tools. See `docs/plans/walking-skeleton.md` for the rest.
