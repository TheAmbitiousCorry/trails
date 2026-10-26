# Shared hook scripts

Three POSIX `sh` scripts that need only `curl` and `jq`. Copy this directory to `.trail/hooks/` in the project and point each runtime's hook wiring at them. All three exit 0 with no output when `jq` or `curl` is missing, the server is unreachable, or no session matches, so a stopped server never blocks a turn.

| Script | Event | Does |
|---|---|---|
| `trails-session-start.sh` | session start | Prints `Trails runtime_session_id: <id>` into the agent's context. |
| `trails-off-trail.sh` | after a file read | Posts the path to `/api/off-trail`; prints a signpost when an entry anchors it. |
| `trails-stop.sh` | stop | Posts to `/api/stop-check`; prints the runtime's block form while the session is open. |

Environment, set in the hook command:

| Variable | Default | Meaning |
|---|---|---|
| `TRAILS_URL` | `http://127.0.0.1:7777` | Server address. |
| `TRAILS_SESSION_FIELD` | `session_id` | Dotted stdin JSON path holding the runtime session id. |
| `TRAILS_SESSION_ENV` | unset | Name of an environment variable that holds the id; wins over the stdin field when set. |
| `TRAILS_PATH_FIELD` | `tool_input.file_path` | Dotted stdin JSON path holding the read path. |
| `TRAILS_CONTEXT_FORMAT` | `plain` | `plain` prints text, `hook-json` prints `hookSpecificOutput.additionalContext`, `cursor` prints `additional_context`. |
| `TRAILS_EVENT` | per script | `hookEventName` in the `hook-json` form, for runtimes that name the event differently (Gemini CLI `AfterTool`). |
| `TRAILS_BLOCK_FORMAT` | `block` | `block` prints `{"decision":"block"}`, `deny` prints `{"decision":"deny"}`, `cursor` prints `followup_message`. |
