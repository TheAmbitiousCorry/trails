# codex adapter

1. Append `AGENTS.md` from this directory to the project's instructions file.
2. Register the MCP server. .codex/config.toml:
```toml
[mcp_servers.trails]
url = "http://127.0.0.1:7777/mcp"
```
   This runtime's MCP config cannot read an environment variable, so 7777 is the default port and a repo that pins another in `.trail/port` edits the URL by hand to match. Claude Code's hint line (`Trails: this repo's .trail/port is <n> but the MCP entry points at <m>; ...`) does not apply here.
3. Copy `../generic/hooks/` to `.trail/hooks/` in the project (the wiring below expects that path).
4. Merge `hooks.json` from this directory into `.codex/hooks.json` (the project must be trusted). It wires `SessionStart` and `Stop`.

Off-trail recording is unsupported on Codex: its file reads go through the shell, so no hook payload carries a path. The edit gate is not available there: `adapters/HOOKS.md` records no after-edit event for Codex.
