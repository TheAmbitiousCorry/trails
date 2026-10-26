# codex adapter

1. Append `AGENTS.md` from this directory to the project's instructions file.
2. Register the MCP server. .codex/config.toml:
```toml
[mcp_servers.trails]
url = "http://127.0.0.1:7777/mcp"
```
3. Copy `../generic/hooks/` to `.trail/hooks/` in the project (the wiring below expects that path).
4. Merge `hooks.json` from this directory into `.codex/hooks.json` (the project must be trusted). It wires `SessionStart` and `Stop`.

Off-trail recording is unsupported on Codex: its file reads go through the shell, so no hook payload carries a path.
