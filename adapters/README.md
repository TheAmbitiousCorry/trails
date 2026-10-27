# Adapters

Trails speaks MCP and plain HTTP. Nothing in the server knows which agent is calling. Each adapter here wires one agent runtime to the server with three pieces:

1. **The protocol text** from `../protocol/PROTOCOL.md`, placed where that agent reads project instructions.
2. **The MCP server entry** in that agent's configuration, pointing at `http://127.0.0.1:7777/mcp` (7777 is the default port; a repo that pins another in `.trail/port` uses it in the URL, and the Claude Code entry reads it from `TRAILS_PORT`).
3. **Three hooks**, when the agent supports them: a session-start hook that starts the server when the repo opted in and prints the runtime's session id into context, an after-file-read hook that posts the path to `/api/off-trail` and prints the server's reply, and a stop hook that asks `/api/stop-check` whether the session is open and blocks the turn at most twice.

The hook scripts are shell and `curl`, so every adapter shares them from `generic/`; the per-agent part is only the event wiring. An agent with no hook support works with pieces 1 and 2 alone: the session times out server-side, its gaps go to the browser queue, and the orient view carries the protocol.

| Adapter | Instructions file | MCP config | Hook events |
|---|---|---|---|
| `claude-code/` | skill in `skills/trails/SKILL.md` | `.mcp.json` | `SessionStart`, `PostToolUse`, `Stop` |
| `codex/` | `AGENTS.md` section | `.codex/config.toml` | `SessionStart`, `Stop` in `.codex/hooks.json` |
| `gemini-cli/` | `GEMINI.md` section | `.gemini/settings.json` | `SessionStart`, `AfterTool`, `AfterAgent` |
| `cursor/` | `.cursor/rules` | `.cursor/mcp.json` | `sessionStart`, `postToolUse`, `stop` in `.cursor/hooks.json` |
| `generic/` | any instructions file | any MCP client | the shared scripts, called from whatever the runtime offers |

## Starting the server

The session-start hook starts the server for repos with a committed `.trail/id`, when the `trails` binary is on `PATH` or `TRAILS_BIN` names it. The address is `TRAILS_URL` when set, else the port in `<repo>/.trail/port`, else 7777. It does nothing when a server already answers there, and nothing when the repo has no `.trail/id`. Without a binary it prints one line into the agent's context: `Trails: no server and no binary; see adapters/README.md`. The server's output goes to `~/.trail/server-<id>.log`. Otherwise run `trails serve` in the repo yourself.

## Seeing the trail

The agent runs `trails open trail`, or `trails open map` or `trails open graph`, and reports the URL it prints.

| Adapter | How the user asks |
|---|---|
| `claude-code/` | `/trail`, the skill in `skills/trail/SKILL.md` |
| `codex/` | "show me the trail", per the `AGENTS.md` section |
| `gemini-cli/` | "show me the trail", per the `GEMINI.md` section |
| `cursor/` | "show me the trail", per `rules.md` |
| `generic/` | "show me the trail", per `INSTRUCTIONS.md` |

The shared scripts live in `generic/hooks/` and are copied to `.trail/hooks/` in the project. Event names and payload shapes per runtime are in `HOOKS.md`.
