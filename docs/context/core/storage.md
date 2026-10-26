---
domain: storage
last-verified: 2026-10-27
---

# Storage

One SQLite database per project at `~/.trail/<project-id>/trail.db`, opened with WAL mode, by a pure-Go driver (no C toolchain). Media sits beside it in `~/.trail/<project-id>/media/`. The project id is a random slug stored in the repo at `.trail/id`, which is committed; the repo path is learned from where the server is started and stored in the Trail home so anchors resolve. One server process serves many projects, one database per project.

## Tables

- `signs(id, parent_id, title, position)`: the tree's structure is stored; a sign's summary, edges and scent are computed from its children at read time and never stored
- `entries(id, sign_id, kind, title, read_when, body_json, body_schema, tags_json, tier, tier_before_stale, promoted_by, superseded_by, distinct_reason, decided_by, decided_at, created_at, updated_at)`; `superseded_by` is a denormalised copy of the `supersedes` link for cheap filtering
- `entry_aliases(entry_id, read_when)` for merged gap queries
- `anchors(id, entry_id, type, path_or_url, range_start, range_end, hash, fetched_at, last_checked_at, ok, extra)`
- `links(from_id, to_id, relation)` with relation in `requires`, `see_also`, `supersedes`, `conflicts_with`; symmetric relations are stored once and queried both ways
- `versions(id, entry_id, body_json, title, read_when, anchors_json, actor, created_at)`
- `sessions(id, actor, task, task_type, started_at, ended_at, ended_by)`
- `events(id, session_id, at, tool, target, depth, result, shown_json)`
- `gaps(id, sign_id, session_id, query, paths_json, state, resolution, target_id, reason, opened_at, resolved_at)`
- `tags(id, description, defined)`: the tag vocabulary; a tag written before it is defined has `defined = 0` and is absent from orient until a human describes it
- `settings(key, value)` for caps and thresholds
- `schema_version(version, applied_at)`; migrations are numbered and forward-only, and every entry carries a `body_schema` integer column naming the content schema its body was validated against

## Search

`entries_fts` is an FTS5 table over title (weight A), `read_when` plus aliases (weight A) and plain-text body (weight B), refreshed by one Go function in the single write path. There is no trigger: SQL cannot walk the JSON body. Ranking is bm25 adjusted by tier. The relevance threshold for a miss is a setting.

## Process

One Go binary, `trails`, serving on a local port: the Vue build embedded at `/`, the MCP at `/mcp`, a JSON API at `/api` for the front end. There is no authentication: Trails is a single-user local tool and nothing from any permission system reaches it. The one check is that requests whose Host or Origin is not the local port are refused, so a web page open in the browser on another site cannot drive the server. The MCP transport is stateless; the session is identified by the `session_id` argument, never by transport state. Anchor re-checks run on read and on a scheduled sweep every six hours. There is no second process.

## Content schema

Allowed nodes: paragraph, heading (levels 2 to 4; a heading with no level is refused), bullet and ordered lists with list items, code block, blockquote, image, table with its row, cell and header nodes, hard break, text. A Markdown construct outside this list is refused by name (`unsupported: html block`), never dropped silently. Allowed marks: bold, italic, code, link (http, https, mailto, or a relative href with no scheme). An image src is http, https, or a path under `/media/`. Depth cap 32, byte cap 64 KB. The walker lives in `internal/content` and refuses with a rule named `schema:<rule>` and a fix. It mirrors `frontend/src/editor/schema.ts`, and both are pinned by the fixtures in `server/internal/content/testdata/`, one accepted document per node and mark and one refused document per rule. The Markdown adapter's `unsupported: <construct>` refusal fires before the walker, on constructs Markdown can express that the schema cannot hold: HTML blocks and inline HTML, task lists, strikethrough, headings at level 1 or above 4, thematic breaks, footnotes, ordered lists starting anywhere but 1, link and image titles, inline images, table column alignment, and anything after the language in a code fence. The emitter is canonical and the round-trip guarantee is structural equality through JSON plus byte identity on a second emit, for every body the walker accepts; two adjacent lists of one kind merge on re-parse, marks are re-ordered to link, bold, italic, code, and a table whose first row has no header cells comes back with one. `read` validates a stored body before converting it and returns the `schema:*` refusal when a body fails, which only happens if the store was written around the write path.
