---
domain: adapters
last-verified: 2026-10-27
---

# Adapters

Trails is agent-neutral. The server speaks MCP for agents and HTTP for hooks and the browser, and no code path depends on which runtime is calling. Everything specific to a runtime lives in `adapters/<runtime>/` and consists of placing text and configuration, never of code the server needs.

## What the server offers every runtime

- The MCP endpoint at `/mcp`, stateless Streamable HTTP, session identified by argument, see [mcp](./mcp.md).
- The protocol in the `session_start` response: a short how-to-walk block and the tag vocabulary, so an agent that was given only the MCP entry still learns the protocol on first contact.
- Two hook endpoints under `/api`, both plain JSON over HTTP so a shell script with `curl` can call them:
  - `POST /api/off-trail {runtime_session_id, path}`: records an `off_trail` event and replies `{entry_id, read_when}` for the anchored entry when one exists, or `{}`.
  - `POST /api/stop-check {runtime_session_id}`: replies `{open: bool, blocks: n, gaps: [{id, query}]}` and increments `blocks` on each call while the session is open. The hook blocks while the reply says `open`; on the third call the server closes the session with `ended_by: forced` and replies `open: false, forced: true`. When several open sessions share a runtime id the most recently started wins.
  - Hooks know the runtime's own session id, not the Trails one, so `session_start` accepts an optional `runtime_session_id`, and both hook endpoints resolve the open Trails session by it. A session-start hook prints the runtime id into the agent's context so it can pass it along. When no session matches, off-trail is dropped and stop-check replies `open: false`.
- Fallbacks when a runtime has no hooks: the sweep closes a session with no call for four hours with `ended_by: timeout`, its gaps show in the browser's gap queue, and off-trail reads are simply not recorded.

## What an adapter contains

1. The protocol text from `protocol/PROTOCOL.md`, formatted for where that runtime reads project instructions.
2. The MCP server entry for that runtime's configuration.
3. Hook wiring that calls the three shared shell scripts in `adapters/generic/hooks/` (they source `trails-common.sh`) on that runtime's session-start, after-file-read and stop events, when it has them. `adapters/HOOKS.md` holds the verified event names and shapes per runtime.

`protocol/PROTOCOL.md` is the single source of the protocol text. Adapter copies are verbatim copies of it and `scripts/check-adapters.sh`, run by `make test`, fails when a copy drifts.
