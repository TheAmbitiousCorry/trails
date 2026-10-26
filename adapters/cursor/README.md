# cursor adapter

1. Append `rules.md` from this directory to the project's instructions file.
2. Register the MCP server. .cursor/mcp.json:
```json
{ "mcpServers": { "trails": { "url": "http://127.0.0.1:7777/mcp" } } }
```
3. Copy `../generic/hooks/` to `.trail/hooks/` in the project (the wiring below expects that path).
4. Copy `hooks.json` from this directory to `.cursor/hooks.json`, or merge its `hooks` into yours. It wires `sessionStart`, `postToolUse` (matcher `Read`) and `stop`, and reads the session id from `conversation_id`. The commands are relative to the project root.

The read path is taken from `tool_input.file_path`, which is unverified: capture one real `postToolUse` payload and set `TRAILS_PATH_FIELD` if the field differs. Cursor's stop hook cannot block; it schedules a follow-up turn, and the server's block counter still ends the session on the third check.
