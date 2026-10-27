---
domain: trail
last-verified: 2026-10-27
---

# Trail

The trail is the record of what every reader did. The MCP, the three hook endpoints, the sweep and the browser's sign creation write it; nothing else does. Humans read it in the browser.

## Sessions

A session starts with `session_start(task, task_type)`, where `task_type` is optional and stored as an empty string when absent, and ends with `session_end`. The server issues the session id and every later call carries it. Every session has `actor: agent`. The sweep closes a session with no call for four hours with `ended_by: timeout`; since the sweep runs every six hours, that happens at the first sweep after the four hours. `ended_by` is `agent`, `forced` or `timeout`. An ended session accepts no further call: the recorder refuses any tool but `session_start` that carries its id with `closed_session` ("this session has ended; start a new one with session_start") and logs `refused:closed_session`.

## Events

One row per call, append-only:

| Field | |
|---|---|
| `session_id` | Minted by the recorder before the handler runs, so the `session_start` event carries the id its session row is created with. The browser logs its writes under the session id `user`, which has no session row; the rejection log reads it as actor kind `user`. |
| `at` | timestamp |
| `tool` | `session_start`, `look`, `search`, `read`, `write`, `write_sign` (target: the new sign id), `write_scores` (the duplicate and paraphrase scores beside each write), `confirm`, `resolve_gap`, `session_end`, `off_trail`, `edit`, and `sweep` for the expiry pass, whose rows carry result `dormant` or `deleted` and whose dormancy time dates the further 30 days before deletion. The orient view is the response to `session_start`, not a tool of its own. |
| `target` | sign or entry id, or query text for search |
| `depth` | `summary` or `full` for reads |
| `result` | `ok`, `refused:<rule>`, `miss`, `error` |
| `shown` | for search: the ids returned; for `write_scores`: the measured scores as JSON. A `read` that serves prerequisites logs each as its own `read` event right after the requested one |

`off_trail` events come from a runtime's after-read hook, wired per [adapters](./adapters.md), that forwards native file reads and searches with the path; Codex has no such hook and records none. They are how a bypass of the map becomes visible, and they feed gap detection. When the path is anchored by an entry, the hook's reply names that entry, see [mcp](./mcp.md#teaching-the-protocol).

`edit` events come from a runtime's after-edit hook, wired per [adapters](./adapters.md), that forwards native file edits with the path made repo-relative; the target is that path. Runtimes with no such hook record none.

## Gaps

A gap is opened by the server when a `search` returns nothing above the relevance threshold.

The gap row holds the sign the reader was standing at (the target of the session's latest successful `look`, or the home sign when that target was an entry; none when the session has not looked or its last look was a tag or kind query), the query, and the session, through which the task is reachable. A gap is resolved through `resolve_gap` by the session that opened it, with one of:

- `fill`: an entry id this session wrote
- `merge`: an existing entry id; the query is added to that entry's `read_when` aliases
- `dismiss`: a reason, 200 characters max

`session_end` refuses with the session's open gaps listed while any remain. The runtime's stop hook refuses to end a turn whose Trails session is open (gaps or [edited entries](#edited-entries)), at most twice; the third attempt ends the session with `ended_by: forced` and its gaps stay open and show in the browser's gap queue. When the server is unreachable the hook passes through. A session may dismiss at most three gaps; the fourth dismissal is refused and the gap stays open.

Open gaps are capped at twenty per project. When the cap is reached, new misses are still logged as events but no gap row opens, and the orient view carries a notice saying so.

## Edited entries

An entry is edited and unresolved for a session when the session has an `ok` `edit` event on a path the entry anchors (a `file` or `range` anchor with that path, or a `dir` anchor whose directory contains it), and no later `ok` event that targets the entry id and is a `write` or `confirm`. A create counts as a write of the new entry, through its scores event. Dormant and superseded entries are never listed.

`session_end` also refuses with `unconfirmed_edits` while any remain, its fix listing each entry id with the path edited. The stop-check reply carries them as `edits`, stays `open` while gaps or edits remain, and counts blocks and forces the end on the third call as it does for gaps; the stop hook names both when both exist. The hook that reports an edit replies with the anchored entries at once, see [adapters](./adapters.md). A forced end leaves the entries unconfirmed, nothing else.

## Views computed from events

- **Route.** For a session: the ordered signs, entries and queries, with turn-backs marked (a repeat visit, or a look at the parent of the previous look), misses marked and linked to their gap, and prerequisite chains shown as one hop that landed several entries.
- **Heat.** Per entry and sign: reads in the last 30 days.
- **Rejections.** Which rule fired, how often, per actor kind. A rejection log dominated by one rule means that cap is doing its job or is mistuned; the human decides which.
- **Dismissals.** Dismissed gaps with reasons, so a lazy pattern is visible.
- **Expiry.** Provisional entries unread for 30 days go dormant; dormant provisional entries unread for a further 30 days are deleted. Both appear as events.
