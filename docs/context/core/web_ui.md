---
domain: web_ui
last-verified: 2026-10-27
---

# Web UI

The browser is the human's view of the same database the MCP serves. It is a Vue 3 app embedded in the binary and served at `/`; its data comes from `GET` routes under `/api`; the one write is `POST /api/signs`, which creates a sign as a human actor. A reload on any page works because the server returns the app for any path that is not a file or an `/api` route.

## Shell

Every page is a left sidebar beside an article column. The sidebar holds the brand, a search box, the tree and the page links (Map, Graph, Sessions, Gaps, Provisional, Tags, Rejections). Below 900 px the sidebar is hidden behind a Menu toggle at the top of the page; the toggle stays visible and opens the sidebar as an overlay.

- **Tree.** Every sign with its entries nested, fetched once per app load from `/api/graph`. Signs are collapsed by default. Opening a sign or entry page opens its ancestors and marks the page as current. Each entry carries a tier dot.
- **Search.** Typing filters the tree to titles containing the text and opens the matching branches. From three characters the box also calls `/api/search` and lists up to eight hits above the tree, each with its tier and sign path, linking to the entry. Escape clears the box.

## Pages

- **Map** (`/`). The full tree, collapsed to its root's children, each node a disc coloured by the tier mix of its subtree, with its entry count, stale count and open-gap count. Click a twist to expand. A heat toggle sizes each disc by reads in the last 30 days and prints the count. A tag selector re-draws the tree with only the entries carrying that tag, listed under their home signs; a second selector limits the tree to the subtree of one sign, so a tag crossed with a sign shows one matrix cell as a sign tree.
- **Graph** (`/graph`). Every sign and entry drawn as a force-directed graph: signs as squares, entries as discs coloured by tier and sized by reads in the last 30 days; home edges in grey, typed links coloured by relation with arrowheads. The view zooms and pans, nodes drag, a click opens the sign or entry page, hovering a node highlights its edges, and the drawing fits itself to the view once the layout settles. A `focus` query with `depth` 1 or 2 shows only the nodes within that many edges of one node; the sign and entry pages link here that way. A `tag` query keeps entries carrying the tag and the signs on their paths. A `session` query draws that session's walk: only the visited nodes, the prerequisite entries each hop served and the signs above them to the root, laid out left to right in hop order, the visited nodes outlined and numbered and joined by arrows, prerequisites joined to their hop by dashed lines, and the hops listed beside the drawing with the same wording as the session trail; a toggle shows the same route over the whole map. The session page links here. Query hops have no node and appear in the list only. Above 150 nodes with no focus the drawing shows signs only, each labelled with its entry count.
- **Sign page** (`/signs/:id`). The path from the root, the sign's summary, a link to its local graph, its stale and open-gap counts, its edges as an agent sees them (kind of edge, tier, counts, `read_when`, scent keywords) with the line count of that agent response, then the entries grouped by kind. Each entry shows its title, tier, chain length, `read_when`, tags, body and anchors, with a failed anchor flagged; an example also shows the snippet read live from its range anchor. A stale entry's body is shown in full. A form at the foot creates a child sign and can move the sign's child signs and direct entries under it; a refusal shows beside the form with its rule and fix.
- **Entry page** (`/entries/:id`). One entry: path, tier, kind, chain length, reads in 30 days, last update, `read_when`, tags, who decided and when, a superseded notice, a link to its local graph, the body, snippets for file and range anchors, each anchor with its last check time and a failed flag, typed links and backlinks, and the versions by author and time.
- **Sessions** (`/sessions`). The 200 most recent sessions with task, actor, start, how each ended, and counts of looks, searches, reads, writes, confirms, off-trail reads and misses.
- **Session trail** (`/sessions/:id`). The route for one session: task at the top, a link to the route on the graph, then each call in order with the sign, entry or query it named, turn-backs, misses (linked to their gap), refusals and prerequisite chains marked. An off-trail column lists the native reads the hooks forwarded and the entry that anchors each path, if any. A second block lists the entries the session wrote or confirmed, which are also highlighted in the route.
- **Gap queue** (`/gaps`). Open gaps with the sign they belong to, the query or paths, the session that opened them and when.
- **Provisional queue** (`/provisional`). Provisional entries, most read first, with all-time and 30-day read counts, the number of sessions that confirmed each, and its sign path.
- **Tags** (`/tags`). Every tag with its description and use count; undefined tags come first and are marked. A tag links to the map filtered by it.
- **Rejections and dismissals** (`/logs`). Refused calls grouped by rule and actor kind with the 50 most recent, and the gaps agents dismissed with their reasons.

## Routes the shell and the graph use

- `GET /api/graph` returns `nodes` (every sign with its parent, and every live entry with kind, tier, reads in 30 days, home sign and tags) and `edges` (`home` from a sign to each child sign and entry, and the typed links as stored). `tag` keeps entries carrying the tag and the signs on their paths to the root. `focus` with `depth` 1 or 2 keeps the nodes within that many edges of the focus; an unknown focus is 404 and a depth outside 1 to 2 is 400. `session` adds the session row and `route`: its hops in order, each with tool, target, kind (sign, entry or query), result, miss, turn-back and the prerequisite entry ids served with it; an unknown session is 404. Without `session`, `route` is null.
- `GET /api/search?q=` runs the agents' search with no task type and a limit of ten, returning the hits with their sign paths and `miss`. An empty `q` is 400.
- `POST /api/signs` takes `parent_id`, `title` and `move` (child sign ids and entry ids to move under the new sign) and creates a sign with the `write_sign` rules; a refusal is 400 with its rule and fix, an unknown parent 404.

scripts/proof.mjs captures every page listed here at 1280 and 390 px into .proof/ and fails on any page or console error; run make proof against a server with data.
