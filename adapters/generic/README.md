# generic adapter

1. Append `INSTRUCTIONS.md` from this directory to the project's instructions file.
2. Register the MCP server. Register a Streamable HTTP MCP server at `http://127.0.0.1:7777/mcp` in whatever way your client supports.
3. Copy `../generic/hooks/` to `.trail/hooks/` in the project (the wiring below expects that path).
4. Call the scripts from whatever hook events the client offers: `trails-session-start.sh` at session start, `trails-off-trail.sh` after a file read, `trails-stop.sh` at stop. Each reads the event's JSON on stdin; `hooks/README.md` lists the environment variables that adapt field names and output forms.
