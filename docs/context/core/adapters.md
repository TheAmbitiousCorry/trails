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
  - `POST /api/off-trail {session_id, path}`: records an `off_trail` event and replies with the anchored entry's id and `read_when` when one exists, or an empty body.
  - `POST /api/stop-check {session_id}`: replies `{open: bool, blocks: n, gaps: [...]}` and increments `blocks` on each call while the session is open. The hook blocks while `open` and `blocks <= 2`; on the third call the server closes the session with `ended_by: forced`.
- Fallbacks when a runtime has no hooks: a session with no call for four hours closes with `ended_by: timeout`, its gaps go to the browser queue, and off-trail reads are simply not recorded.

## What an adapter contains

1. The protocol text from `protocol/PROTOCOL.md`, formatted for where that runtime reads project instructions.
2. The MCP server entry for that runtime's configuration.
3. Hook wiring that calls the two shared shell scripts on that runtime's after-file-read and stop events, when it has them.

`protocol/PROTOCOL.md` is the single source of the protocol text. Adapter copies are generated from it and a check in `make test` fails when a copy drifts.
