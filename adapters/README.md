# Adapters

Trails speaks MCP and plain HTTP. Nothing in the server knows which agent is calling. Each adapter here wires one agent runtime to the server with three pieces:

1. **The protocol text** from `../protocol/PROTOCOL.md`, placed where that agent reads project instructions.
2. **The MCP server entry** in that agent's configuration, pointing at `http://127.0.0.1:7777/mcp`.
3. **Three hooks**, when the agent supports them: a session-start hook that prints the runtime's session id into context, an after-file-read hook that posts the path to `/api/off-trail` and prints the server's reply, and a stop hook that asks `/api/stop-check` whether the session is open and blocks the turn at most twice.

The hook scripts are shell and `curl`, so every adapter shares them from `generic/`; the per-agent part is only the event wiring. An agent with no hook support works with pieces 1 and 2 alone: the session times out server-side, its gaps go to the browser queue, and the orient view carries the protocol.

| Adapter | Instructions file | MCP config | Hook events |
|---|---|---|---|
| `claude-code/` | skill in `skills/trails/SKILL.md` | `.mcp.json` | `SessionStart`, `PostToolUse`, `Stop` |
| `codex/` | `AGENTS.md` section | `.codex/config.toml` | `SessionStart`, `Stop` in `.codex/hooks.json` |
| `gemini-cli/` | `GEMINI.md` section | `.gemini/settings.json` | `SessionStart`, `AfterTool`, `AfterAgent` |
| `cursor/` | `.cursor/rules` | `.cursor/mcp.json` | `sessionStart`, `postToolUse`, `stop` in `.cursor/hooks.json` |
| `generic/` | any instructions file | any MCP client | the shared scripts, called from whatever the runtime offers |

The shared scripts live in `generic/hooks/` and are copied to `.trail/hooks/` in the project. Event names and payload shapes per runtime are in `HOOKS.md`.
