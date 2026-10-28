---
domain: signs
last-verified: 2026-10-27
---

# Signs

A sign is a fork in the trail: a node with a one-line summary of where the reader stands and a short list of outgoing edges. Agents navigate by reading one sign, choosing an edge, and reading the next. The tree is the map; the agent only ever holds the sign in front of it.

## Structure

- The root sign is the project. Its edges are domains.
- A domain sign's edges are modules or sub-domains.
- A module sign's edges are entries, grouped by kind.
- Any sign may hold both child signs and entries. Depth is unbounded; width is capped.

A sign is computed, never authored. Its summary is the first paragraph of the `summary` entry hanging directly under it, or its title when none exists. Its edges are its children.

## Edges

An edge is one row per child showing:

- the child's title
- the child's `read_when` line
- scent keywords, so a reader can pick a branch without opening it: the tags found in the child, most common first, then its three highest tf-idf body terms
- the child's tier; for a child sign, the tier and `read_when` of its own summary entry, empty when it has none
- for a child sign: its child count (signs and entries directly under it) and stale count (stale entries anywhere below it)
- for an entry: its kind and word count

A sign's entries of one kind show as one edge each when alone, and as one `group` edge naming the count when there are two or more; `look(kind, within)` lists the group.

## Fan-out cap

A sign holds at most eight edges by default. A write that would create a ninth is refused with `split this fork`, and the response names the cap and the project setting that holds it (`fan_out_cap`), proposes a grouping of the existing edges from their tags, naming the child sign ids and entry ids in each group, and ends with how to act on it: create a sign for a group with `write_sign` and pass their ids in `move`. Eight follows the menu breadth studies and the tree-walking agents that plateau between five and eight children; the setting exists because a project's domain count is given, not chosen. Entries under a sign count as one edge per kind present, except that a lone summary is the sign's own description and counts for nothing; so a sign's edge count is its child signs plus its distinct entry kinds, with the summary kind counting only from the second summary.

## Creating a sign

A sign is created with `write_sign` under an existing parent, with a title and an optional `move` list of entry ids and child sign ids that hang directly under that parent, which move under the new sign. A moved sign takes its subtree with it, keeps its own children and entries, and takes the next position under the new sign; no cycle is possible because the new sign is a fresh child of the parent. The id is a slug of the title. The parent's fan-out is checked as it will stand after the move plus the new sign, and the new sign's own fan-out is checked over the moved signs and the kinds among the moved entries; either over the cap refuses with `split this fork`, and its proposal names both child sign ids and entry ids. Ids that do not hang directly under the parent refuse with `move_scope`. A human creates a sign the same way from the sign page in the browser. The tree can also be declared in `.trail/map.md`, which the server applies on every start; see [storage](./storage.md#map-file).

## Orient and local view

- **Orient.** The `session_start` response returns the root sign's id and title, its edge count (child signs plus entry kinds directly under it, by the fan-out rule above), the project's entry count, the defined tag vocabulary and the how-to-walk block, plus a notice when open gaps are at the cap. It is the trailhead board.
- **Local view.** Any `look` call returns one sign: its summary, its parent's title, and its edges. Depth is one. A `depth` parameter up to three nests child signs, and the response says in `lines` how many lines it cost.
- **Search** returns matching entries each with the path of sign titles above it, so a jump lands with its route filled in. A `read` carries the signs from the root to the entry, each with its id, title and summary line, so a reader who arrived by search or a link sees the forks it skipped. Results are capped at five by default (ten at most) and ranked by text match with a tier factor, so at equal text verified beats provisional beats stale.

## Prerequisites

An entry may list `requires`: entry ids a reader needs before this one makes sense. A `read` of the entry at depth `full` returns the whole chain in full, earliest prerequisite first, then the entry itself. Prerequisites this session has already read at `full` are listed by id, title, tier and `read_when` instead of repeated, with a note that they still apply to the entry and are to be read directly if no longer in the reader's context. Nothing in the chain is summarised, truncated or capped: a prerequisite is a declaration that the entry cannot be understood without it, so it is required reading by definition.

The response states its total lines, so the cost of the hop is visible. The sign page shows each entry's chain length, so a chain that has grown long is corrected by rewriting the entry's `requires` list, never by the server trimming it at read time.

Prerequisites are for ordering that matters: a client's constraints before its report template, a decision before the module it shaped.

## Virtual signs

The tree is one axis. Tags are every other axis. `look` accepts a tag, a kind, or both with an optional `within` sign, and returns a computed sign in exactly the shape of a stored one: a summary line naming the query, and edges for the matching entries. For a query across the tree (no `within`), when more than the fan-out cap match, the edges are the home signs that contain matches, each with its match count, so the reader descends one more hop. A query with `within` lists every match as an entry edge, whatever the count. `look(tag: billing, within: shopify-app)` is one cell of a matrix; `look(kind: decision)` gathers every decision in the project.

A virtual sign is never stored. An entry has one home in the tree and appears on any number of virtual signs. Scent keywords on a tree edge include the tags found below it, so a reader on the domain axis can see which branches touch a tag before descending.

## Filtering by task type

`look` accepts a `task_type`; edges whose subtree holds no entry with that tag are folded into one line: `n more edges outside <task_type>`. `search` takes it as a rank boost for entries carrying the tag, filtering nothing.
