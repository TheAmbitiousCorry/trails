# Hook facts per runtime

Verified against each runtime's official documentation on 2026-10-27. Anything marked unverified needs one real stdin payload captured before the wiring is trusted. Step 10 of the walking skeleton builds from this file.

| | Claude Code | Codex CLI | Gemini CLI | Cursor |
|---|---|---|---|---|
| Hooks file | `.claude/settings.json`, key `hooks` | `.codex/hooks.json` (trusted project) | `.gemini/settings.json`, key `hooks` | `.cursor/hooks.json`, `version: 1` |
| MCP file and entry | `.mcp.json`: `{"type":"http","url":...}` | `.codex/config.toml`: `[mcp_servers.trails] url = ...` | `.gemini/settings.json`: `{"httpUrl": ...}` (`url` means SSE, wrong) | `.cursor/mcp.json`: `{"url": ...}` |
| After-read event | `PostToolUse`, matcher `Read\|Grep\|Glob` | `PostToolUse`, but reads go through shell, no path field: off-trail unsupported | `AfterTool`, matcher `read_.*`, tools `read_file`, `grep_search`, `glob` | `postToolUse`, matcher `Read` |
| Reply the agent sees | stdout JSON `hookSpecificOutput.additionalContext` | `hookSpecificOutput.additionalContext` | `hookSpecificOutput.additionalContext` | `additional_context` |
| Stop event | `Stop` | `Stop` | `AfterAgent` | `stop` |
| Block the stop | `{"decision":"block","reason":...}` or exit 2 | same | `{"decision":"deny","reason":...}` or exit 2 | `{"followup_message":...}`, a follow-up turn, capped by `loop_limit` |
| Re-entry visible | `stop_hook_active` | `stop_hook_active` | `stop_hook_active` | `loop_count` |
| Runtime session id | stdin `session_id` | stdin `session_id` | stdin `session_id`, env `GEMINI_SESSION_ID` | stdin `conversation_id` |
| Path field on read | verified: Read `tool_input.file_path`, Grep and Glob `tool_input.path` | n/a | unverified, tool param is `file_path` | unverified |

The session-start hook reads the repo from stdin `cwd` when the payload has it and from its own working directory otherwise; the field is unverified outside Claude Code.

Known limits: Codex gets the stop check only. Gemini CLI's docs carry a notice that it was replaced by Antigravity CLI in June 2026; check the target before wiring. Cursor's stop hook cannot block, it schedules a follow-up turn, which the server's `blocks` counter still bounds.

The hooks know the runtime's session id, not the Trails one. A session-start hook (Claude Code `SessionStart`, Codex `SessionStart`, Gemini `SessionStart`, Cursor `sessionStart`) prints `Trails runtime_session_id: <id>` into context; the agent passes it to `session_start`, and `/api/off-trail` and `/api/stop-check` take the runtime id and resolve the open Trails session from it.

Sources: code.claude.com/docs/en/hooks, code.claude.com/docs/en/mcp, developers.openai.com/codex/hooks, developers.openai.com/codex/mcp, geminicli.com/docs/hooks/reference/, geminicli.com/docs/tools/mcp-server/, cursor.com/docs/hooks, cursor.com/docs/mcp.
