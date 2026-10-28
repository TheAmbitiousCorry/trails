# Hook facts per runtime

Verified against each runtime's official documentation on 2026-10-27. Anything marked unverified needs one real stdin payload captured before the wiring is trusted. Step 10 of the walking skeleton builds from this file.

| | Claude Code | Codex CLI | Gemini CLI | Cursor |
|---|---|---|---|---|
| Hooks file | `.claude/settings.json`, key `hooks` | `.codex/hooks.json` (trusted project) | `.gemini/settings.json`, key `hooks` | `.cursor/hooks.json`, `version: 1` |
| MCP file and entry | `.mcp.json`: `{"type":"http","url":...}` | `.codex/config.toml`: `[mcp_servers.trails] url = ...` | `.gemini/settings.json`: `{"httpUrl": ...}` (`url` means SSE, wrong) | `.cursor/mcp.json`: `{"url": ...}` |
| After-read event | `PostToolUse`, matcher `Read\|Grep\|Glob` | `PostToolUse`, but reads go through shell, no path field: off-trail unsupported | `AfterTool`, matcher `read_.*`, tools `read_file`, `grep_search`, `glob` | `postToolUse`, matcher `Read` |
| Before-read event | `PreToolUse`, matcher `Read\|Grep\|Glob`; unverified, taken from memory and not yet checked against the docs | none recorded: after-read only | none recorded: after-read only | none recorded: after-read only |
| Deny the read | `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":...}}`, exit 0; unverified, taken from memory (exit 2 with the reason on stderr is the older form) | n/a | n/a | n/a |
| Reply the agent sees | stdout JSON `hookSpecificOutput.additionalContext` | `hookSpecificOutput.additionalContext` | `hookSpecificOutput.additionalContext` | `additional_context` |
| Shell read | `PostToolUse`, matcher `Bash`; paths taken from `tool_input.command` tokens that exist as files, recorded as off-trail and never denied | n/a | n/a | n/a |
| After-edit event | `PostToolUse`, matcher `Edit\|Write\|MultiEdit\|NotebookEdit` | none recorded: no edit gate | none recorded: no edit gate | `afterFileEdit`, unverified |
| Subagent start | `SubagentStart`, prints `Trails project` and `Trails runtime_session_id` | none recorded | none recorded | none recorded |
| Stop event | `Stop` | `Stop` | `AfterAgent` | `stop` |
| Block the stop | `{"decision":"block","reason":...}` or exit 2 | same | `{"decision":"deny","reason":...}` or exit 2 | `{"followup_message":...}`, a follow-up turn, capped by `loop_limit` |
| Subagent stop | `SubagentStop`, same block form as `Stop` | none recorded | none recorded | none recorded |
| Re-entry visible | `stop_hook_active` | `stop_hook_active` | `stop_hook_active` | `loop_count` |
| Runtime session id | stdin `session_id`, with `/agent_id` appended inside a subagent | stdin `session_id` | stdin `session_id`, env `GEMINI_SESSION_ID` | stdin `conversation_id` |
| Path field on read | verified: Read `tool_input.file_path`, Grep and Glob `tool_input.path` | n/a | unverified, tool param is `file_path` | unverified |
| Path field on edit | `tool_input.file_path` | n/a | n/a | `file_path` (unverified) |

All five hook scripts take the repo from `CLAUDE_PROJECT_DIR` when set, else stdin `cwd` when the payload has it, else their own working directory; `cwd` is unverified outside Claude Code. The nearest `.trail/id` at or above the repo gives the project id the scripts send as `project`; the server address is `TRAILS_URL`, else `TRAILS_ADDR`, else `127.0.0.1:7777`.

The before-read rows were added with `trails-pre-read.sh` and are not part of the 2026-10-27 verification: a runtime may have a before-tool event that this file does not record (Gemini CLI `BeforeTool`, Cursor `preToolUse`, Codex `PreToolUse` are believed to exist, all unverified), and until one is checked against the docs and a real payload is captured it keeps the after-read signpost. The pre-read script reads the same path fields as the after-read one, `tool_input.file_path` for `Read` and `tool_input.path` for `Grep` and `Glob`, which stay unverified for `PreToolUse`; a `Grep` or `Glob` with no `path` has nothing to ask about and passes.

Known limits: Codex gets the stop check only. Gemini CLI's docs carry a notice that it was replaced by Antigravity CLI in June 2026; check the target before wiring. Cursor's stop hook cannot block, it schedules a follow-up turn, which the server's `blocks` counter still bounds.

The hooks know the runtime's session id, not the Trails one. A session-start hook (Claude Code `SessionStart`, Codex `SessionStart`, Gemini `SessionStart`, Cursor `sessionStart`) prints `Trails project: <id>` and `Trails runtime_session_id: <id>` into context; the agent passes both to `session_start`, and the four hook endpoints take the project and the runtime id and resolve the open Trails session from them.

Sources: code.claude.com/docs/en/hooks, code.claude.com/docs/en/mcp, developers.openai.com/codex/hooks, developers.openai.com/codex/mcp, geminicli.com/docs/hooks/reference/, geminicli.com/docs/tools/mcp-server/, cursor.com/docs/hooks, cursor.com/docs/mcp.
