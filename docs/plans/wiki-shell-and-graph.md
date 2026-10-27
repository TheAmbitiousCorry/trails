---
created: 2026-10-27
spec-impact: docs/context/core/web_ui.md
---

# Wiki shell and graph view

## Goal

The browser reads like a wiki and shows the map as a graph. A left sidebar carries a search box and the whole tree of signs and entries, with the current page highlighted. An article pane shows the sign or entry with breadcrumbs, backlinks and a last-updated line, as [web_ui](../context/core/web_ui.md) already describes. A graph page draws every sign and entry as a node, home edges and typed links as edges, and overlays one session's route as a numbered path, following the local-over-global rule from the research (Obsidian graph: local view with depth 1 to 2, filterable; global view secondary).

## Non-goals

- Editing in the browser (roadmap item).
- Hover previews, a command palette and a right-hand outline. Entries are under 300 words; the outline earns nothing. The palette is roadmap material once the sidebar filter proves insufficient.
- A new search engine. The sidebar search calls the same FTS the agents use.
- No graph at all, only the sidebar plus the backlinks the entry page already has. Rejected: the session route is a path through the map and only a drawing shows where it went; the user asked for the drawing.
- Creating signs from the browser or the MCP. The MyDot seed inserts its sign tree by SQL; sign creation for humans is a roadmap item.

## Unknowns

- Whether a force layout stays readable at MyDot's size (about 60 to 80 entries, 8 signs). Kill criterion: the global view never draws more than 150 nodes. Above that it draws signs only, with each sign's entries folded into its disc, and entries appear when a sign is expanded or a node is focused. The local view (focus plus depth 1 or 2) is the view the sign, entry and session pages link to.
- Whether one route can feed both the sidebar tree and the graph. Decision: `/api/graph` without a session returns every sign and entry plus the edges, about 100 rows for MyDot; the sidebar reads the same response and ignores the edges, so there is one tree source. The Map page stays as the overview (tier mix, heat, stale and gap counts, tag crossed with sign); the sidebar is plain navigation and carries none of those.

## Steps

1. [ ] Extract the session route builder from the session handler into `buildRoute(ctx, db, id)` with no behaviour change. Domain: web_ui. Verification: the existing `/api/sessions/{id}` test passes unchanged; a new test pins the route JSON for the fixture session before and after.
2. [ ] `GET /api/search?q=` (hits with sign path, tier, kind, from the same FTS the agents use) and `GET /api/graph?session=&tag=&focus=&depth=`. Nodes: signs and entries with tier, kind, reads in 30 days, home sign and tags. Edges: home (sign to child sign, sign to entry), requires, see_also, supersedes, conflicts_with. With focus and depth 1 or 2, only nodes within that many edges of the focus. With a session, `route`: the session's hops in order, each with its tool, target node id (query hops carry no node and are markers), result and the prerequisite entry ids served with it. Domain: web_ui. Verification: `go test ./internal/web/...` covers both routes with the fixture: node and edge counts, focus at depth 1 trims to neighbours, unknown session or focus returns 404, empty q returns 400.
3. [ ] Shell: the app becomes a left sidebar plus an article column, pages unchanged. The sidebar carries the brand and the page links (Map, Graph, Sessions, Gaps, Provisional, Tags, Rejections). Under 900 px the sidebar collapses behind a toggle that stays visible at the top. Domain: web_ui. Verification: `npm run build` passes; screenshots at 1280 and 390.
4. [ ] Sidebar tree: a `useGraph` composable fetches `/api/graph` once per app load; the tree lists signs with their entries nested, collapsed by default, with the current page's ancestors open and the current page highlighted; each entry carries a tier dot. Domain: web_ui. Verification: build passes; screenshot of an entry page with its ancestors open and itself highlighted.
5. [ ] Sidebar search: a text box above the tree filters it by title as you type; from three characters it also calls `/api/search` and lists hits with their sign path under the box; Escape clears. Domain: web_ui. Verification: build passes; screenshot with a query showing filtered tree and hits.
6. [ ] Graph page at `/graph`: `d3-force` (only that package, plus `d3-zoom`, `d3-selection`, `d3-drag`) renders sign nodes as squares and entry nodes as discs coloured by tier and sized by reads in 30 days; home edges grey, typed links coloured by relation with arrowheads; zoom and pan; click opens the page; `focus` and `depth` queries draw the local graph; `tag` filters; `session` draws the route as numbered hops with arrows in order, prerequisite hops dashed, misses as markers on the route list beside the drawing. Sign, entry and session pages link into the graph with the right query. Global view above 150 nodes folds entries into signs. Domain: web_ui. Verification: build passes; the built JS bundle stays under 600 KB; screenshots of the global graph, a local graph and a session overlay on the MyDot database.
7. [ ] Spec: web_ui.md gains the Graph page, the sidebar and the `/api/graph` and `/api/search` routes. Verification: a reviewer reads each page sentence against the rendered page on the MyDot database; the deviation log records any sentence that did not hold.

## Deviation log

- Roast before step 1 (Beck, Munger, Winters, Ousterhout): the route builder was not reusable as written, so its extraction became step 1; `/api/graph` gained `focus` and `depth` before any page binds to it; the shell, tree and search split into three steps; the Map page stays as the overview and the sidebar reads `/api/graph`; a 150-node kill criterion folds entries into signs; the bundle gets a size cap. The MyDot sign tree was seeded by SQL before the plan ran (seven signs under root, six defined tags) and the population workflow is writing entries while this plan executes.
