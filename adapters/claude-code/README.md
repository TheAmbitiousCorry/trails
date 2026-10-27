# claude-code adapter

1. Copy `skills/trails/` from this directory to `.claude/skills/trails/` in the project.
1b. Copy `skills/trail/` to `.claude/skills/trail/` as well; it gives the `/trail` command.
2. Register the MCP server. .mcp.json:
```json
{ "mcpServers": { "trails": { "type": "http", "url": "http://127.0.0.1:7777/mcp" } } }
```
3. Copy `../generic/hooks/` to `.trail/hooks/` in the project (the wiring below expects that path).
4. Merge the `hooks` key of `hooks/settings.json` from this directory into `.claude/settings.json`. It wires `SessionStart`, `PostToolUse` (matcher `Read|Grep|Glob`) and `Stop`.

The read path is taken from `tool_input.file_path`, which is unverified for `Read` and absent for `Grep` and `Glob`: capture one real `PostToolUse` payload and set `TRAILS_PATH_FIELD` in the hook command if the field differs. Calls with no path are ignored.
