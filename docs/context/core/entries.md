---
domain: entries
last-verified: 2026-10-27
---

# Entries

An entry is one leaf of the tree: one fact, one decision, one example, one render, or one pointer. It is the only thing agents write.

## Fields

| Field | Rule |
|---|---|
| `id` | Stable slug, unique per project, never reused. Set at creation from the title (lowercase, hyphens, 60 characters at most, `-2`, `-3` appended when the slug is taken by an entry, a sign or an expired entry); a later title change does not change the id. The `write` tool derives the id and cannot pin one. |
| `kind` | One of `summary`, `decision`, `example`, `render`, `reference`. |
| `title` | 80 characters max. |
| `read_when` | One line, 200 characters max: the trigger phrase that tells a reader this entry answers their need. Shown on every sign that lists the entry. |
| `body` | ProseMirror JSON validated against the Trail schema (see [storage](./storage.md)). Cap per kind below. |
| `anchors` | One or more, see Anchors. Required for every kind except `decision`. |
| `tags` | Task types this entry serves (`report`, `client-comms`, `billing`, `deploy`, free-form). Used to filter signs. |
| `tier` | `provisional`, `verified`, `stale`, `dormant`. Computed, see Trust tiers. |
| `parent` | The sign this entry hangs under. Exactly one. |
| `links` | Typed edges to other entries, see Links. |

## Links

Every relationship between entries is a typed edge in one table; backlinks are a query over it, never authored.

| Relation | Direction | Served on `read` |
|---|---|---|
| `requires` | directed | At depth `full`, the target's body in full before this entry, see [signs](./signs.md#prerequisites) |
| `see_also` | symmetric | As an edge line: id, title, `read_when`, tier |
| `supersedes` | directed | The superseded entry leaves default search and carries the link back |
| `conflicts_with` | symmetric | Set by the server when an agent write collides with a verified entry; shown as a link on the entry page |

A `read` also lists the entry's backlinks, every entry that links to it by any relation, as edge lines; a symmetric link therefore appears under both `related` and `backlinks`. Related is not required: only `requires` brings a body, because that relation is the author's declaration that the entry cannot be understood without it.

## Kinds and caps

| Kind | Holds | Body cap | Anchor rule |
|---|---|---|---|
| `summary` | What a module or domain owns, why it exists, what it must not do | 300 words | At least one file, directory or commit anchor. The one exception is the project's root summary, which the server seeds on first open as `verified` with `decided_by: seed` and no anchor |
| `decision` | Who decided, when, why, what was rejected | 200 words | No anchor needed; `decided_by` and `decided_at` required |
| `example` | A code snippet with one sentence of context | 40 words of prose; the browser renders the snippet live from the anchor range | Exactly one range anchor |
| `render` | A screenshot, story URL or diagram, described in the body | 60 words | Exactly one `produced_by` anchor naming the producing command or URL; the server records the repository HEAD at write time |
| `reference` | A pointer to a long external document | 100 words | Exactly one URL or transcript anchor |

Caps are per-kind settings in the project config. The defaults above hold until trail data argues otherwise.

## Anchors

An anchor is the checkable claim behind an entry. The server re-checks an entry's anchors when an agent reads it through the MCP and on a sweep that runs at startup and every six hours. The tier column is stored and rewritten by those checks, so a sign's stale count reflects an entry only after it has been read or swept.

Column use per type: `file`, `range` and `dir` keep the path in `path_or_url` and the sha256 in `hash`; `commit` and `produced_by` keep the sha in `hash`, and `produced_by` keeps the command in `path_or_url`; `url` keeps the url in `path_or_url`, the body hash in `hash` and the fetch time in `fetched_at`; `transcript` keeps `provider:id` in `extra`.

| Type | Fields | Stale when |
|---|---|---|
| `file` | path, content hash | hash of the file at path differs |
| `range` | path, start line, end line, hash of the range | hash of the range differs, or the file is gone |
| `dir` | path, hash of the sorted file list | listing differs |
| `commit` | sha | sha is not an ancestor of HEAD |
| `url` | url, fetched_at, hash of fetched body | body hash differs on re-fetch, or fetch fails. Fetches go through a resolver guard that refuses loopback, private and link-local addresses |
| `transcript` | provider, id | never; the check always passes |
| `produced_by` | command, HEAD sha at write time | HEAD differs from the stored sha (renders go stale on every commit until re-written) |

A `summary` whose own anchors match keeps its tier whatever its children do; the sign it sits under carries the count of stale entries anywhere below it, see [signs](./signs.md#edges).

## Trust tiers

| Tier | Set when | Served as |
|---|---|---|
| `provisional` | Written by an agent | Ranked below verified in search; labelled in every response |
| `verified` | Confirmed by two distinct agent sessions that read it at `full` and called `confirm`; promotion sets `promoted_by: agents`. A session cannot confirm an entry it wrote. The write path also stores an actor `user` write as verified, and `trail.ConfirmHuman` promotes alone with `promoted_by: user`; no interface calls either. | Default rank |
| `stale` | Any anchor fails its check, including an uncommitted edit to an anchored file; the tier it had before is kept in `tier_before_stale` and restored when every anchor passes again | Ranked below provisional; `read` returns the body with tier `stale` and a `notice` listing the failed anchors by type and path, so the reader can check the source instead of guessing |
| `dormant` | Superseded, or provisional and unread for 30 days | Out of default search and off signs; reachable by id and in the browser |

A dormant provisional entry is deleted after a further 30 days. A dormant superseded entry is kept.

## Write rules

Every write passes these in order. The first failure returns the rule name and the fix.

1. **Schema.** Kind, title (one line) and `read_when` (one line) fit their fields, every linked id exists, and the body, converted from Markdown, validates against the allow-list walker: node and mark types, URL schemes, depth, byte cap.
2. **Cap.** Word count within the kind's cap. Refusal: `split this`.
3. **Anchor.** Anchor rule for the kind holds and every anchor resolves right now.
4. **Duplicate.** The title, `read_when` and first body paragraph are searched. A hit above the duplicate threshold returns the hit and three options: `merge` (add this `read_when` as an alias on the hit; new entries only), `replace` (supersede the hit, which goes dormant with `superseded_by` set; an agent may not replace a verified hit), `distinct` (write anyway, with a reason stored on the entry).
5. **Paraphrase.** For `summary`, the share of the body's three-word shingles found in its `file` anchors, when above the paraphrase threshold, is refused: `describe the why, point at the what`.
6. **Fan-out.** The parent sign stays within its edge cap. The cap is owned by the signs package and the write path asks it only when the entry's kind is new under that parent, since kinds are the edges, see [signs](./signs.md#fan-out-cap).
7. **Reachability.** The entry has exactly one parent sign and the parent is reachable from the root. An entry never hangs under two signs; a second axis is a tag, see [signs](./signs.md#virtual-signs).

An update may change title, `read_when`, body, tags, anchors and links; it replaces the entry's `requires` and `see_also` links with the request's lists. Kind and parent are immutable, rule `immutable`. An agent write to an entry that is `verified` is refused with rule `verified` and stored as a new provisional entry with `conflicts_with` set.

## Versions

Every accepted write appends a version row: full body, title, `read_when`, anchors, author (session id or user), timestamp. An update that leaves body, title, `read_when` and anchors as the last version had them appends none. The entry page lists versions by author and time and never carries bodies.
