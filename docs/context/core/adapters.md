---
domain: adapters
last-verified: 2026-10-27
---

# Adapters

Trails is agent-neutral. The server speaks MCP for agents and HTTP for hooks and the browser, and no code path depends on which runtime is calling. Everything specific to a runtime lives in `adapters/<runtime>/` and consists of placing text and configuration, never of code the server needs.

## What the server offers every runtime

- The MCP endpoint at `/mcp`, stateless Streamable HTTP, session identified by argument, see [mcp](./mcp.md).
- The protocol in the `session_start` response: a short how-to-walk block and the tag vocabulary, so an agent that was given only the MCP entry still learns the protocol on first contact.
- Three hook endpoints under `/api`, all plain JSON over HTTP so a shell script with `curl` can call them, at the repo's address: `TRAILS_URL` when set, else `http://127.0.0.1:<port>` with the port from `<repo>/.trail/port` and 7777 (the default) when the file is absent, see [storage](./storage.md#port-file). The repo is `CLAUDE_PROJECT_DIR` when set, else the `cwd` field of the hook payload, else the hook's working directory, the same for all four scripts:
  - `POST /api/off-trail {runtime_session_id, path}`: records an `off_trail` event and replies `{entry_id, read_when}` for the anchored entry when one exists, or `{}`.
  - `POST /api/edited {runtime_session_id, path}`: records an `edit` event with the path as its target and replies `{entries: [{entry_id, read_when}]}` for every live entry with a file, range or dir anchor whose path is the edited path or a directory containing it, or `{entries: []}`. When no session matches, it is dropped and replies `{entries: []}`.
  - `POST /api/stop-check {runtime_session_id}`: replies `{open: bool, blocks: n, gaps: [{id, query}], edits: [{id, path}]}`, `edits` listing the entries [trail](./trail.md#edited-entries) holds the session open for, and increments `blocks` on each call while the session is open. The hook blocks while the reply says `open`; on the third call the server closes the session with `ended_by: forced` and replies `open: false, forced: true`. When several open sessions share a runtime id the most recently started wins.
  - Hooks know the runtime's own session id, not the Trails one, so `session_start` accepts an optional `runtime_session_id`, and all three hook endpoints resolve the open Trails session by it. A session-start hook prints the runtime id into the agent's context so it can pass it along. When no session matches, off-trail and edited are dropped and stop-check replies `open: false`.
- Fallbacks when a runtime has no hooks: the sweep closes a session with no call for four hours with `ended_by: timeout`, its gaps show in the browser's gap queue, and off-trail reads and edits are simply not recorded, and no edit gate holds the session open.

## Autostart

The session-start hook starts the server when the repo contains `.trail/id` and nothing answers `GET /api/map` at the hook's address within one second; the address comes from `.trail/port` as above, so the server starts on the repo's port. It runs `$TRAILS_BIN` (default `trails` on `PATH`) with `serve --repo <repo> --addr <addr>`, detached, with output appended to `~/.trail/server-<id>.log`, and waits up to three seconds for `/api/map`. A repo with no `.trail/id` has not opted in: the hook starts nothing and prints nothing about the server. When the binary is missing it prints `Trails: no server and no binary; see adapters/README.md` into the agent's context. With a server already up the check is one `curl` with a one-second cap and nothing else changes.

## Opening the browser

Every adapter tells its agent to run `trails open trail` when the user asks to see the trail, `trails open map` or `trails open graph` for those pages, and to report the URL printed. Claude Code ships this as the user-invocable skill `trail` (`/trail`); the other runtimes carry a `Seeing the trail` section after the protocol text in their instructions file, which `scripts/check-adapters.sh` excludes from the protocol comparison. The command is the one described in [storage](./storage.md).

## What an adapter contains

1. The protocol text from `protocol/PROTOCOL.md`, formatted for where that runtime reads project instructions.
2. The MCP server entry for that runtime's configuration, pointing at the repo's port (7777 by default). The Claude Code plugin entry is `http://127.0.0.1:${TRAILS_PORT:-7777}/mcp`: Claude Code expands `${VAR}` and `${VAR:-default}` in `.mcp.json`, so a repo on another port sets `TRAILS_PORT` in the `env` block of its `.claude/settings.json`. The session-start hook, when run by Claude Code, compares `.trail/port` with `TRAILS_PORT` (7777 when unset) and on a difference prints `Trails: this repo's .trail/port is <n> but the MCP entry points at <m>; add "env": {"TRAILS_PORT": "<n>"} to .claude/settings.json`. Runtimes whose MCP config cannot read an environment variable edit the URL by hand to match the file.
3. The `Seeing the trail` section (Claude Code: the `trail` skill).
4. Hook wiring that calls the four shared shell scripts in `adapters/generic/hooks/` (they source `trails-common.sh`) on that runtime's session-start, after-file-read, after-file-edit and stop events, when it has them. A runtime with no after-file-edit event says so in its README and has no edit gate. `adapters/HOOKS.md` holds the verified event names and shapes per runtime.

`protocol/PROTOCOL.md` is the single source of the protocol text. Adapter copies are verbatim copies of it and `scripts/check-adapters.sh`, run by `make test`, fails when a copy drifts.
