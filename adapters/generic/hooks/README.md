# Shared hook scripts

Four POSIX `sh` scripts that need only `curl` and `jq`. Copy this directory to `.trail/hooks/` in the project and point each runtime's hook wiring at them. All four exit 0 with no output when `jq` or `curl` is missing, the server is unreachable, or no session matches, so a stopped server never blocks a turn.

| Script | Event | Does |
|---|---|---|
| `trails-session-start.sh` | session start | When the repo has `.trail/id` at or above it, registers the repo with the server, starting the server first when none answers; under Claude Code prints the `TRAILS_PORT` line when the server it reached is on another port than the MCP entry; prints `Trails project: <id>` and `Trails runtime_session_id: <id>` into the agent's context. |
| `trails-off-trail.sh` | after a file read | Posts the path to `/api/off-trail`; prints a signpost when an entry anchors it. |
| `trails-edited.sh` | after a file edit | Posts the path to `/api/edited`; prints the entries anchored to it, to update or confirm before the session ends. |
| `trails-stop.sh` | stop | Posts to `/api/stop-check`; prints the runtime's block form while gaps or edited entries remain. |

Environment, set in the hook command:

| Variable | Default | Meaning |
|---|---|---|
| `TRAILS_URL` | `http://` plus `TRAILS_ADDR`, else `http://127.0.0.1:7777` | Server address. The repo is `CLAUDE_PROJECT_DIR` when set, else the payload `cwd`, else the working directory; its project id is read from the nearest `.trail/id` at or above it and sent as `project`. |
| `TRAILS_ADDR` | `127.0.0.1:7777` | Host and port of the server, the same variable the `trails` commands read. |
| `TRAILS_BIN` | `trails` | Binary the session-start hook runs as `serve --addr <addr>` when it autostarts. |
| `TRAILS_SESSION_FIELD` | `session_id` | Dotted stdin JSON path holding the runtime session id. |
| `TRAILS_SESSION_ENV` | unset | Name of an environment variable that holds the id; wins over the stdin field when set. |
| `TRAILS_PATH_FIELD` | `tool_input.file_path` | Dotted stdin JSON path holding the read or edited path. |
| `TRAILS_CONTEXT_FORMAT` | `plain` | `plain` prints text, `hook-json` prints `hookSpecificOutput.additionalContext`, `cursor` prints `additional_context`. |
| `TRAILS_EVENT` | per script | `hookEventName` in the `hook-json` form, for runtimes that name the event differently (Gemini CLI `AfterTool`). |
| `TRAILS_BLOCK_FORMAT` | `block` | `block` prints `{"decision":"block"}`, `deny` prints `{"decision":"deny"}`, `cursor` prints `followup_message`. |
