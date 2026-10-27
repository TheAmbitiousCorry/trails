---
domain: adapters
last-verified: 2026-10-27
---

# Adapters

Trails is agent-neutral. The server speaks MCP for agents and HTTP for hooks and the browser, and no code path depends on which runtime is calling. Everything specific to a runtime lives in `adapters/<runtime>/` and consists of placing text and configuration, never of code the server needs.

## What the server offers every runtime

- The MCP endpoint at `/mcp`, stateless Streamable HTTP, session identified by argument, see [mcp](./mcp.md).
- The protocol in the `session_start` response: a short how-to-walk block and the tag vocabulary, so an agent that was given only the MCP entry still learns the protocol on first contact.
- Project endpoints, `POST /api/projects {repo}` and `GET /api/projects`, see [storage](./storage.md#registry): a hook registers its repo with the server, and the server never creates `.trail/id` for it.
- Three hook endpoints under `/api`, all plain JSON over HTTP so a shell script with `curl` can call them, at `TRAILS_URL` when set, else `http://` plus `TRAILS_ADDR`, else `http://127.0.0.1:7777`, see [storage](./storage.md#address). The repo is `CLAUDE_PROJECT_DIR` when set, else the `cwd` field of the hook payload, else the hook's working directory, the same for all four scripts. The project is the id in the nearest `.trail/id` at or above the repo. Each body below also carries `project`, that id, when the scripts found one:
  - `POST /api/off-trail {project, runtime_session_id, path}`: records an `off_trail` event and replies `{entry_id, read_when}` for the anchored entry when one exists, or `{}`.
  - `POST /api/edited {project, runtime_session_id, path}`: records an `edit` event with the path as its target and replies `{entries: [{entry_id, read_when}]}` for every live entry with a file, range or dir anchor whose path is the edited path or a directory containing it, or `{entries: []}`. When no session matches, it is dropped and replies `{entries: []}`.
  - `POST /api/stop-check {project, runtime_session_id}`: replies `{open: bool, blocks: n, gaps: [{id, query}], edits: [{id, path}]}`, `edits` listing the entries [trail](./trail.md#edited-entries) holds the session open for, and increments `blocks` on each call while the session is open. The hook blocks while the reply says `open`; on the third call the server closes the session with `ended_by: forced` and replies `open: false, forced: true`. When several open sessions share a runtime id the most recently started wins.
  - Hooks know the runtime's own session id, not the Trails one, so `session_start` accepts an optional `runtime_session_id`, and all three hook endpoints resolve the open Trails session by it. A body with `project` is served by that project alone, so a repo's hook can never reach another repo's session; a `project` the server does not hold is refused `404 project_unknown`. A body without `project` (a runtime that cannot run the scripts from inside the repo, or a repo with no `.trail/id`) looks for the runtime id in every project's open sessions, the most recently started winning, and the server remembers in memory which project answered for an id and asks it first while it still holds an open session for that id. A session-start hook prints the project id and the runtime id into the agent's context so it can pass both along. When no session matches, off-trail and edited are dropped and stop-check replies `open: false`.
- Fallbacks when a runtime has no hooks: the sweep closes a session with no call for four hours with `ended_by: timeout`, its gaps show in the browser's gap queue, and off-trail reads and edits are simply not recorded, and no edit gate holds the session open.

## Autostart

The session-start hook acts only when `.trail/id` exists at or above the repo; a repo with no id has not opted in, and the hook contacts no server, starts nothing and prints nothing about the project or the server. Otherwise it registers the repo by `POST /api/projects {repo}` at the hook's address with a one-second cap, `repo` being the directory that holds `.trail/id`. When nothing answers, it runs `$TRAILS_BIN` (default `trails` on `PATH`) with `serve --addr <addr>`, no repo, detached, with output appended to `~/.trail/server.log`, retries the registration for up to three seconds, and the one server then holds this repo beside every project it already knew. When the binary is missing it prints `Trails: no server and no binary; see adapters/README.md` into the agent's context. With a server already up the whole check is the one registration `curl`.

The hook then prints, in order and each only when it applies: the binary line above; under Claude Code (`CLAUDE_PROJECT_DIR` set), when the server answered and the port in `TRAILS_URL` differs from `TRAILS_PORT` (7777 when unset), `Trails: the server answered on port <n> but the MCP entry points at port <m>; add "env": {"TRAILS_PORT": "<n>"} to ~/.claude/settings.json`; `Trails project: <id>`; and `Trails runtime_session_id: <id>`. The agent passes both ids to `session_start`. The mismatch check reads nothing from the repo: one machine sets its port once.

## Opening the browser

Every adapter tells its agent to run `trails open trail` when the user asks to see the trail, `trails open map` or `trails open graph` for those pages, and to report the URL printed. Claude Code ships this as the user-invocable skill `trail` (`/trail`); the other runtimes carry a `Seeing the trail` section after the protocol text in their instructions file, which `scripts/check-adapters.sh` excludes from the protocol comparison. The command is the one described in [storage](./storage.md).

## What an adapter contains

1. The protocol text from `protocol/PROTOCOL.md`, formatted for where that runtime reads project instructions.
2. The MCP server entry for that runtime's configuration, pointing at the machine's server (port 7777 by default). The Claude Code plugin entry is `http://127.0.0.1:${TRAILS_PORT:-7777}/mcp`: Claude Code expands `${VAR}` and `${VAR:-default}` in `.mcp.json`, so a machine on another port sets `TRAILS_PORT` once in the `env` block of `~/.claude/settings.json`, and the session-start hook prints the line to add when the port it reached differs. Runtimes whose MCP config cannot read an environment variable edit the URL by hand to match the server's port.
3. The `Seeing the trail` section (Claude Code: the `trail` skill).
4. Hook wiring that calls the four shared shell scripts in `adapters/generic/hooks/` (they source `trails-common.sh`) on that runtime's session-start, after-file-read, after-file-edit and stop events, when it has them. A runtime with no after-file-edit event says so in its README and has no edit gate. `adapters/HOOKS.md` holds the verified event names and shapes per runtime.

`protocol/PROTOCOL.md` is the single source of the protocol text. Adapter copies are verbatim copies of it and `scripts/check-adapters.sh`, run by `make test`, fails when a copy drifts.
