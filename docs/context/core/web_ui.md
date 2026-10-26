---
domain: web_ui
last-verified: 2026-10-27
---

# Web UI

The browser is the human's read-only view of the same database the MCP serves. It is a Vue 3 app embedded in the binary and served at `/`; its data comes from `GET` routes under `/api` that run queries and never write. A reload on any page works because the server returns the app for any path that is not a file or an `/api` route.

## Pages

- **Map** (`/`). The full tree, collapsed to its root's children, each node a disc coloured by the tier mix of its subtree, with its entry count, stale count and open-gap count. Click a twist to expand. A heat toggle sizes each disc by reads in the last 30 days and prints the count. A tag selector re-draws the tree with only the entries carrying that tag, listed under their home signs; a second selector limits the tree to the subtree of one sign, so a tag crossed with a sign shows one matrix cell as a sign tree.
- **Sign page** (`/signs/:id`). The path from the root, the sign's summary, its stale and open-gap counts, its edges as an agent sees them (kind of edge, tier, counts, `read_when`, scent keywords) with the line count of that agent response, then the entries grouped by kind. Each entry shows its title, tier, chain length, `read_when`, tags, body and anchors, with a failed anchor flagged; an example also shows the snippet read live from its range anchor. A stale entry's body is shown in full.
- **Entry page** (`/entries/:id`). One entry: path, tier, kind, chain length, reads in 30 days, `read_when`, tags, who decided and when, a superseded notice, the body, snippets for file and range anchors, each anchor with its last check time and a failed flag, typed links and backlinks, and the versions by author and time.
- **Sessions** (`/sessions`). The 200 most recent sessions with task, actor, start, how each ended, and counts of looks, searches, reads, writes, confirms, off-trail reads and misses.
- **Session trail** (`/sessions/:id`). The route for one session: task at the top, then each call in order with the sign, entry or query it named, turn-backs, misses (linked to their gap), refusals and prerequisite chains marked. An off-trail column lists the native reads the hooks forwarded and the entry that anchors each path, if any. A second block lists the entries the session wrote or confirmed, which are also highlighted in the route.
- **Gap queue** (`/gaps`). Open gaps with the sign they belong to, the query or paths, the session that opened them and when.
- **Provisional queue** (`/provisional`). Provisional entries, most read first, with all-time and 30-day read counts, the number of sessions that confirmed each, and its sign path.
- **Tags** (`/tags`). Every tag with its description and use count; undefined tags come first and are marked. A tag links to the map filtered by it.
- **Rejections and dismissals** (`/logs`). Refused calls grouped by rule and actor kind with the 50 most recent, and the gaps agents dismissed with their reasons.
