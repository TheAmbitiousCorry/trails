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

A sign is computed, never authored. Its summary is the `summary` entry hanging directly under it, or its title when none exists. Its edges are its children.

## Edges

An edge is one row per child showing:

- the child's title
- the child's `read_when` line
- two or three scent keywords, generated from the child's tags and the top terms of its body, so a reader can pick a branch without opening it
- the child's tier
- for a child sign: its child count and stale count
- for an entry: its kind and word count

## Fan-out cap

A sign holds at most seven edges by default. A write that would create an eighth is refused with `split this fork`, and the response proposes a grouping of the existing edges from their tags. The cap is a project setting. Entries grouped by kind under a module count as one edge per kind group when the group has more than one member.

## Orient and local view

- **Orient.** The session's first call returns the root sign with sizes: edge count, total entries, stale count, open gaps, and the task-type tags present. It is the trailhead board. It is at most 40 lines.
- **Local view.** Any `look` call returns one sign: its summary, its parent's title, and its edges. Depth is one. A `depth` parameter up to three is allowed and the response says how many lines it cost.
- **Search** returns matching entries each with the path of sign titles above it, so a jump lands with its route filled in. Results are capped at five by default, ranked verified above provisional above stale.

## Prerequisites

An entry may list `requires`: entry ids a reader needs before this one makes sense. A `read` of the entry returns the whole chain in full, earliest prerequisite first, then the entry itself. Prerequisites this session has already read at `full` are listed by id and title instead of repeated. Nothing in the chain is summarised, truncated or capped: a prerequisite is a declaration that the entry cannot be understood without it, so it is required reading by definition.

The response states how many entries it carried and its total lines, so the cost of the hop is visible. The sign page shows each entry's chain length, so a chain that has grown long is corrected by editing the map, never by the server trimming it at read time.

Prerequisites are for ordering that matters: a client's constraints before its report template, a decision before the module it shaped.

## Virtual signs

The tree is one axis. Tags are every other axis. `look` accepts a tag, a kind, or both with an optional `within` sign, and returns a computed sign in exactly the shape of a stored one: a summary line naming the query, and edges for every matching entry. When more than the fan-out cap match, the edges are the home signs that contain matches, each with its match count, so the reader descends one more hop. `look(tag: billing, within: shopify-app)` is one cell of a matrix; `look(kind: decision)` gathers every decision in the project.

A virtual sign is never stored. An entry has one home in the tree and appears on any number of virtual signs. Scent keywords on a tree edge include the tags found below it, so a reader on the domain axis can see which branches touch a tag before descending.

## Filtering by task type

`orient`, `look` and `search` accept a `task_type`. Edges whose subtree holds no entry with that tag are folded into one line: `n more edges outside <task_type>`.
