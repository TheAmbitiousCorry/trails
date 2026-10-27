# claude-code adapter

The repo is itself a Claude Code plugin: `.claude-plugin/plugin.json`, `skills/trails` and `skills/trail`, `hooks/hooks.json` and `.mcp.json` at the root. The repo is also its own marketplace: run `claude plugin marketplace add TheAmbitiousCorry/trails` then `claude plugin install trails@trails` and skip the steps below. Manual wiring:

1. Copy `skills/trails/` and `skills/trail/` from the repo root to `.claude/skills/` in the project; the second gives the `/trail` command.
2. Register the MCP server. .mcp.json:
```json
{ "mcpServers": { "trails": { "type": "http", "url": "http://127.0.0.1:7777/mcp" } } }
```
3. Copy `../generic/hooks/` to `.trail/hooks/` in the project (the wiring below expects that path).
4. Merge the `hooks` key of `hooks/settings.json` from this directory into `.claude/settings.json`. It wires `SessionStart`, `PostToolUse` (matcher `Read|Grep|Glob`) and `Stop`.

The read path is taken from `tool_input.file_path`, which is unverified for `Read` and absent for `Grep` and `Glob`: capture one real `PostToolUse` payload and set `TRAILS_PATH_FIELD` in the hook command if the field differs. Calls with no path are ignored.
