---
domain: trail
last-verified: 2026-10-27
---

# Trail

The trail is the record of what every reader did. The MCP writes it; nothing else does. Humans read it in the browser.

## Sessions

A session starts with `session_start(task, task_type)` and ends with `session_end`. The server issues the session id and every later call carries it. A session with no call for four hours is closed by the server with `ended_by: timeout`.

Human browser activity is also a session, with `actor: user` and the task `browse`.

## Events

One row per call, append-only:

| Field | |
|---|---|
| `session_id` | Minted by the recorder before the handler runs, so the `session_start` event carries the id its session row is created with. |
| `at` | timestamp |
| `tool` | `session_start`, `look`, `search`, `read`, `write`, `write_scores` (the duplicate and paraphrase scores beside each write), `confirm`, `resolve_gap`, `session_end`, `off_trail`. The orient view is the response to `session_start`, not a tool of its own. |
| `target` | sign or entry id, or query text for search |
| `depth` | `summary` or `full` for reads |
| `result` | `ok`, `refused:<rule>`, `miss` |
| `shown` | for search and look: the ids returned; for read: the entry and every prerequisite served with it, each logged as its own read |

`off_trail` events come from a Claude Code hook that forwards native Read and Grep calls with the file path. They are how a bypass of the map becomes visible, and they feed gap detection. When the path is anchored by an entry, the hook's reply names that entry, see [mcp](./mcp.md#teaching-the-protocol).

## Gaps

A gap is opened by the server when:

- a `search` returns nothing above the relevance threshold
- a `look` is followed within the session by three or more `off_trail` reads under paths no entry anchors to
- a `write` lands with no `read` of its parent sign in the session

The gap row holds the sign the reader was standing at (the target of the session's latest successful `look`, or the home sign when that target was an entry), the query or paths, and the session, through which the task is reachable. A gap is resolved by `resolve_gap` with one of:

- `fill`: a new entry id, written in the same call or just before
- `merge`: an existing entry id; the query is added to that entry's `read_when` aliases
- `dismiss`: a reason, 200 characters max

`session_end` returns the session's open gaps and refuses to close while any remain. A Claude Code Stop hook refuses to end a turn whose Trail session is open, at most twice; the third attempt ends the session with `ended_by: forced` and its gaps go to the browser queue. When the server is unreachable the hook passes through. A session may dismiss at most three gaps; the fourth dismissal is refused and the gap stays open for a human.

Open gaps are capped at twenty per project. When the cap is reached, new misses are still logged as events but no gap row opens, and `orient` says so.

## Views computed from events

- **Route.** For a session: the ordered signs and entries, with turn-backs marked and prerequisite chains shown as one hop that landed several entries.
- **Heat.** Per entry and sign: reads in the last 30 days, cite ratio (reads followed by a write or a confirm that names the entry), days since last read, tier.
- **Rejections.** Which write rule fired, how often, per actor kind. A rejection log dominated by one rule means that cap is doing its job or is mistuned; the human decides which.
- **Dismissals.** Dismissed gaps with reasons, so a lazy pattern is visible.
- **Expiry.** Provisional entries unread for 30 days go dormant; dormant provisional entries unread for a further 30 days are deleted. Both appear as events.
