# gemini-cli adapter

1. Append `GEMINI.md` from this directory to the project's instructions file.
2. Register the MCP server. .gemini/settings.json:
```json
{ "mcpServers": { "trails": { "httpUrl": "http://127.0.0.1:7777/mcp" } } }
```
   This runtime's MCP config cannot read an environment variable, so 7777 is the default port and a repo that pins another in `.trail/port` edits the URL by hand to match. Claude Code's hint line (`Trails: this repo's .trail/port is <n> but the MCP entry points at <m>; ...`) does not apply here.
3. Copy `../generic/hooks/` to `.trail/hooks/` in the project (the wiring below expects that path).
4. Merge the `hooks` key of `settings-hooks.json` from this directory into `.gemini/settings.json`. It wires `SessionStart`, `AfterTool` (matcher `read_.*`) and `AfterAgent`, and reads the session id from `GEMINI_SESSION_ID`.

The edit gate is not available on Gemini CLI: `adapters/HOOKS.md` records no after-edit event for it. The read path is taken from `tool_input.file_path`, which is unverified: capture one real `AfterTool` payload and set `TRAILS_PATH_FIELD` in the hook command if the field differs. Gemini CLI's documentation carries a notice that Antigravity CLI replaced it in June 2026; check which one you run before wiring.
