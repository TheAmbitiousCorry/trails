# generic adapter

1. Append `INSTRUCTIONS.md` from this directory to the project's instructions file.
2. Register the MCP server. Register a Streamable HTTP MCP server at `http://127.0.0.1:7777/mcp` in whatever way your client supports. 7777 is the default port; a machine whose server runs on another needs the URL edited by hand to match, unless your client expands an environment variable. A client with no hooks registers each repo with `trails register`.
3. Copy `../generic/hooks/` to `.trail/hooks/` in the project (the wiring below expects that path).
4. Call the scripts from whatever hook events the client offers: `trails-session-start.sh` at session start, `trails-pre-read.sh` before a file read (deny the read when it prints a deny form your client understands; `TRAILS_PATH_FIELD` and the session variables adapt the input), `trails-off-trail.sh` after a file read when the client has no before-read event (a signpost, not a turn-back), `trails-stop.sh` at stop. Each reads the event's JSON on stdin; `hooks/README.md` lists the environment variables that adapt field names and output forms.
