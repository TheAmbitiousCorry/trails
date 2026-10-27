---
created: 2026-10-27
spec-impact: docs/context/core/web_ui.md
---

# Instrument look, the constellation map and the walk

## Goal

The browser takes the Instrument look: dark-first with a light variant, dense sidebar, mono metadata, surfaces separated by elevation rather than borders. The Graph page draws the map with three switchable layouts. Two are trail maps: Ridge (the tree left to right, trailhead at the left, columns by the same step rule as the rings: a domain's entries in the column after it, a sub-sign one column beyond those entries and its entries one beyond that, each column starting past the longest title before it, each sub-sign in its own vertical band) and Topo (the same tree radial and drawn as a road map: the trailhead at the centre, domains on ring one, a domain's entries on the ring outside it, a sub-sign pushed one ring beyond those entries with its own entries one ring beyond that, each ring starting beyond the previous ring's longest title, trails drawn as roads with casing and highways widest). The third, Cluster, is the force layout reworked: each domain is an island sized by the area its names need, the islands are packed as circles with a margin so they never touch, towns inside an island are spaced by the width of their own name and kept inside the island's circle, a soft hull marks each island, the layout is seeded so it is the same on every load, and it is the one view that draws the typed links, as faint lines lit on hover; the two trail maps draw no typed links, and hovering a node lights only that node's links. The session page shows the same map with the session's walk replayed on it: every hop is walked from the trailhead down to its target, the forks passed on the way drawn faint with the chosen prong solid, a scrubber and play button step through hops, and the current hop's entry shows beside the drawing. See [web_ui](../context/core/web_ui.md).

## Non-goals

- Editing beyond sign creation.
- Replaying what a sign looked like at the time. A fork shows the sign's edges as they are now and the drawing says so in its caption. Storing the served view on look events is a trail-domain change for later.
- A second walk drawing. The map and the replay are one drawing; the walk-columns code in GraphPage is deleted. The force layout stays as the Cluster view.
- A metro layout. Tried on MyDot's data and dropped: 23-waypoint lines did not stay readable.

## Unknowns

- Fork prongs: the route carries no home sign or edges. Decision: `forkOf(signId)` in graph.ts rebuilds the prongs from the graph the way an agent sees them (child signs plus one prong per entry kind, a lone summary free, cap eight) and the page derives each hop's home sign from the tree. Verified against `look` on three MyDot signs before step 3 lands.
- Fonts offline: the binary serves a local address and the proof script fails on any console error. Decision: IBM Plex Sans and Plex Mono are bundled from the @fontsource packages into the Vite build and embedded; no CDN. Fallback stack is the system UI stack.
- Label collision is checked by a script, not by eye: proof.mjs measures the bounding boxes of visible labels and fails on any overlap.

## Steps

1. [ ] Shared map model. graph.ts gains `tree()` (root to leaves from home edges, signs before entries, stable order), `pathTo(id)`, `forkOf(signId)` and `homeOf(id)`; a `TrailMap.vue` component renders the tree in Ridge or Topo layout from props (layout, focus subtree, highlighted ids) and emits node clicks; no page uses it yet. Domain: web_ui. Verification: `npm run build`; a vitest or plain node test of `forkOf` against the fixture-shaped graph returning signs plus kinds with a lone summary free; a comparison of `forkOf` for three MyDot signs against `look` on the live server pasted in the deviation log.
2. [ ] Graph page on the trail map. GraphPage uses TrailMap with a Ridge/Topo switch (remembered per viewer), `focus` trims to that subtree, `tag` filters, click opens the page, zoom and pan, shortcuts faint, fit to view. The walk columns and the session overlay code are deleted. Domain: web_ui. Verification: build; `make proof` with the new overlap assertion green on MyDot in both layouts.
3. [ ] Session page replay. SessionPage shows TrailMap with the walk: hops in order, each walked from the trailhead (the path's segments drawn in sequence, forks from `forkOf` drawn faint at each sign passed, chosen prong solid, numbered badge at the target), a scrubber and play button at 650 ms per hop, arrow keys move the hop, the current hop's entry fetched and shown beside the map (title, read_when, first paragraph, link), the hop list kept below as text with each row selectable, searches and the session end as rows without a node, prerequisites served with a hop drawn as spurs from the hop's target. Caption: "forks as the map is now". Domain: web_ui. Verification: build; proof shots mid-replay and at the end; keyboard check in proof.mjs.
4. [ ] read returns the trail. The `read` tool adds `path`: the signs from the root to the entry's home, each with id, title and summary line, so an entry reached by search or a link carries the forks the reader skipped. Domain: mcp, signs. Verification: go test with a golden regeneration; mcp.md and signs.md updated.
5. [ ] Theme. style.css gets the Instrument tokens dark-first with the light variant, bundled Plex fonts, the compact sidebar, chips in place of pills (every page's pill classes migrated), mono metadata at 12px, article measure 70ch, elevation surfaces, borders only where elevation cannot separate. Domain: web_ui. Verification: build; `make proof` green; every page screenshot at 1280 and 390 reviewed once.
6. [ ] Spec and close. web_ui.md describes the look, the Graph page with its two layouts, the session page replay and the proof script's overlap check; proof.mjs carries the overlap assertion and replay shots; plan deleted. Verification: each sentence checked against the rendered page.

## Deviation log

- The owner asked that child signs come first in their parent's fan so they can take the room their subtrees need, and that Ridge follow the same step rule as Topo: a domain's entries in the column after it, a sub-sign one column beyond those entries, its entries one column beyond that. The tree builder orders child signs before entries. A sub-sign's trail takes the same curve as its sibling entries up to their column or ring, through an invisible point there, then runs straight on to the sub-sign, so no two trails cross; the replay draws the same shape.
- The replay route is drawn in the same colour (the foreground token) in every layout; Topo had used the accent.
- The owner dropped the dashed typed-link lines from the two trail maps and kept them on the Cluster view, which stays as a third layout.
- The owner merged the road-map drawing into Topo after seeing both: Topo keeps D's rings and takes H's road rendering. The mock file's drawH(elId) is the reference.
- Roast before step 1 reconciled: the walk and the map are one drawing (TrailMap), forks come from a shared `forkOf` that rebuilds agent edges rather than per-entry home edges, fonts are bundled, the overlap check is a script, the former step 3 is split into map then replay, and the theme moves last because it changes nothing structural. The owner chose the Ridge layout with Topo as a switch, and asked that every replay hop start at the trailhead and that `read` carry the path.
