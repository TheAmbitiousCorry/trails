---
created: 2026-10-27
spec-impact: docs/context/core/adapters.md, docs/context/core/entries.md, docs/context/core/signs.md, docs/context/core/trail.md, docs/context/core/mcp.md, docs/context/core/web_ui.md, docs/context/core/storage.md
---

# Walking skeleton: an agent walks a sign, reads a leaf, writes a provisional entry, and a human sees the route

## Goal

A Claude Code session pointed at the MyDot repo can `session_start`, `look` at the root sign, `read` one entry, `write` one provisional entry that passes every write rule in [entries](../context/core/entries.md#write-rules), and `session_end`. A browser at the Trail port shows the map, that sign page, and that session's route per [web_ui](../context/core/web_ui.md). The specs are greenfield and describe the target; each step lands the part of the spec it names, and corrects the spec in the same commit where building it proves a sentence wrong.

## Non-goals

- The in-place Tiptap editor. This plan ships read-only pages. Rejected for now because the MCP, not the editor, is the product thesis; the editor is the next plan.
- Prerequisites, gap detection from off-trail reads, and the expiry sweep. Their tables arrive with the step that first uses them; the behaviour waits for trail data showing which of them earn their cost.
- Renders and image upload. The `render` kind validates but no page displays it.
- Multi-project views. One server opens many project databases by id, but no page or tool spans projects.
- Plain Markdown files in git instead of SQLite. Rejected: the trail, gaps and tiers are relational and per-call, and a hand-editable file store invites the second authoring path principle 2 forbids. A Markdown export can come later.
- Any authentication. Rejected: single user, single machine; only the Host and Origin check stays, because a web page on another site can otherwise drive a localhost server.
- Vector search instead of FTS5. Rejected for the skeleton: the corpus is hundreds of entries, bm25 over title and `read_when` is cheap to tune, and an embedding model is a dependency the single binary does not want yet.
- Porting MyDot's RBAC, tenancy, audit triggers, Yjs, Hocuspocus, callouts, mentions, button links, link previews, colour marks, and the media-key check.

## Unknowns

1. **Duplicate and paraphrase thresholds.** No literature value. Step 7 ships them as settings with placeholder values and logs the score on every write. Not blocking.
2. **Scent keyword generation.** Top body terms by tf-idf over the project is the first attempt. First knob if agents open wrong branches. Not blocking.
3. **Agent compliance with `session_end` and gap resolution.** An agent may simply stop without calling `session_end`, leaving gaps open and the session to time out. Step 9's unscripted run measures it. If compliance is poor, gaps resolve in the browser and `session_end` becomes advisory. Not blocking for ordering, blocking for the gap design beyond this plan.
4. **Markdown round-trip.** MyDot's adapter at `server/internal/api/richtext/markdown.go` has no details construct and degrades unknown constructs to plain text. Trail drops details from its schema and defines round-trip as JSON to Markdown to JSON equality with a canonical emitter. Step 4 is new design on a borrowed base, and its fixture suite is where the remaining risk surfaces. Not blocking.
5. **Hook latency.** Whether a PostToolUse hook posting to the local server adds noticeable time to native reads. Measured in step 10. Not blocking.
6. **Does the map beat grep.** The product thesis. Step 9 compares a cold agent on a real report task with and without Trails. Not blocking for ordering; blocking for whether the next plan is the editor or a redesign of signs.

## Steps

Conventions: `conventions.md` does not exist yet in this repo; the first review that produces a repeat finding creates it. Each step that changes spec-described behaviour updates the spec in the same commit.

- [x] **1. Vertical thread.** Go module, Makefile, pure-Go SQLite with `schema_version`, forward-only numbered migrations for `signs`, `entries`, `sessions`, `events` only. One MCP endpoint on the Go SDK (stateless transport, `session_id` as an argument) with two tools, `session_start` and `read`, behind a recorder middleware that writes one event row per call before any handler runs. Database opened at `~/.trail/<id>/` from the repo's committed `.trail/id`. Host and Origin checks, no token. A hard-coded root sign and one entry. Domains: storage, mcp, trail.
  Verification: an HTTP test starts the server on a random port, calls `session_start` then `read`, asserts the entry body comes back inside `untrusted` with `contract: 1`, asserts two event rows with the right `tool` values, and asserts a call with a foreign Origin is refused.

- [x] **2. Content validator.** New package `internal/content` built from the limits, depth and walk machinery of MyDot's `richtext.go`, with the MyDot-only nodes and the media import deleted and the [Trail schema](../context/core/storage.md#content-schema) imposed. Fixtures in `server/internal/content/testdata/`: one accepted document per node and mark type, one refused document per rule. Domain: storage.
  Verification: `go test ./internal/content/...` green; a level-1 heading and a document over the depth cap each refuse with the named rule; `grep -rn "media\.\|callout\|mention\|buttonLink\|linkPreview" internal/content` returns nothing.

- [ ] **3. Look and signs.** `internal/signs` computes a sign from its children with edges, scent keywords, task-type folding, and the fan-out cap as the single owner of that rule. Virtual signs for a tag, a kind, or both within a sign. `look` tool added. Domain: signs, mcp.
  Verification: a test builds a sign with seven children and asserts the eighth insert refuses with `split this fork`; a test asserts a sign with a `summary` child uses that child's body as its summary; a test asserts a `task_type` filter folds non-matching edges into one line with the right count; a test asserts `look(tag)` over nine tagged entries under three signs returns three home-sign edges with match counts; the HTTP test from step 1 gains a `look` call.

- [ ] **4. Markdown adapter.** `internal/content/markdown` from MyDot's `markdown.go`, trimmed to the Trail schema, with the `blockNode` default branch changed from drop to a named refusal, and a canonical emitter. Domain: mcp.
  Verification: for every accepted fixture from step 2, JSON to Markdown to JSON is structurally equal and Markdown emitted twice is byte-identical; a fixture with an HTML block, a task list, and strikethrough each refuse with `unsupported: <construct>`.

- [ ] **5. Anchors.** `internal/anchor`: file, range, dir and commit checks against a temp git repo; url check through a port of MyDot's `safefetch` resolver guard with a fake resolver. Tier computation. `anchors` table arrives here. Domain: entries.
  Verification: a test edits an anchored file and asserts the entry flips to `stale` on next `read`; a test asserts a `summary` with one stale child reports stale count 1 and stays verified when its own anchors hold; a test asserts a url anchor to a loopback or private address refuses.

- [ ] **6. Write path, rules 1 to 3 and 7.** `internal/entries.Write` running schema, cap, anchor and reachability, appending a version, setting `provisional`. `write` tool added. `versions` and `links` tables arrive here; `read` serves the `requires` chain in full and `see_also` plus backlinks as edge lines. Domain: entries, mcp.
  Verification: one test per rule asserting the rule name in the refusal; a test that an agent write to a `verified` entry returns the conflict offer with `conflicts_with`; the HTTP test gains a `write` call and asserts the entry is `provisional`; a test that reading Z with `requires` A and B returns A, B, Z in full and lists a `see_also` entry and one backlink as edge lines only.

- [ ] **7. Search and the remaining rules.** FTS5 table refreshed from the write path, `search` tool with bm25 adjusted by tier and sign paths on each hit, write rules 4 to 6 (duplicate with three options, paraphrase, fan-out via signs), scores logged on every write. `entry_aliases` table arrives here. Domain: entries, storage, mcp.
  Verification: a test that a second identical write returns the duplicate hit with `merge`, `replace`, `distinct`; a test that a `summary` whose body is the anchored file's first 300 words refuses as paraphrase; a test that `search` returns the sign path for each hit and `miss: true` below threshold.

- [ ] **8. Confirm, gaps, session end, tags.** `tags` table and vocabulary in orient with the how-to-walk block; undefined tags accepted and flagged. `confirm` tool with the two-session promotion rule; `gaps` table; a miss opens a gap at the sign the session last looked at; `resolve_gap` with fill, merge, dismiss and the three-dismissal limit; `session_end` refusing while gaps are open. Golden file of the seven tool schemas and refusal strings. Domain: trail, mcp.
  Verification: a test that two distinct sessions calling `confirm` after a `full` read flip `provisional` to `verified` and one session does not; a test that `session_end` refuses with the open gap listed, then closes after `resolve_gap(merge)` and the alias is searchable; a test that the fourth dismissal refuses; the golden-file test fails on any changed refusal string.

- [ ] **9. Seed and the real run.** Hand-seed six entries under two domain signs from MyDot's `docs/context/`, anchored to its spec files and code. Register the server in MyDot's Claude Code MCP config. Run two sessions on the same real MyDot task: one with Trails and the map-first rule in CLAUDE.md, one without. Domain: none; this step produces data.
  Verification: the Trail session reads the right entry through `look` and `read`, recorded in events; the write-up records tool calls to first correct read for both sessions and whether `session_end` was called unprompted. This is the measurement for Unknowns 3 and 6, not a pass or fail.

- [ ] **10. Hook endpoints and adapters.** `/api/off-trail` and `/api/stop-check` per [adapters](../context/core/adapters.md); two shared shell scripts in `adapters/generic/` that call them with `curl`; per-runtime wiring for Claude Code (`PostToolUse`, `Stop`), Codex (`.codex/hooks.json`), Gemini CLI (`AfterTool`, `SessionEnd`) and Cursor (`afterFileEdit`, `stop`), each event name verified against that runtime's current documentation before wiring; a `make test` check that adapter protocol copies match `protocol/PROTOCOL.md`. Domain: adapters, trail.
  Verification: an HTTP test posts to both endpoints and asserts the `off_trail` row, the signpost reply for an anchored path, and `blocks` 1, 2, then `ended_by: forced`; in Claude Code a session reads one file natively and the hook output names the entry, ten timed runs show under 50 ms added per call, and a session that never calls `session_end` is blocked twice and ends `forced`; with the server stopped, the scripts exit zero; the drift check fails when an adapter copy is edited by hand.

- [ ] **11. Read-only web pages.** Vue 3 app embedded in the binary: map, sign page with entries by kind and example snippets rendered from anchors, session trail with route and off-trail column, gap queue read-only. Domain: web_ui.
  Verification: `verify:visual-proof` captures of the three pages against the step 9 data under `.proof/`; the trail page shows the `miss` marker and the off-trail rows from the step 9 and 10 sessions.

- [ ] **12. Close.** Re-read every spec file named in `spec-impact` against the code and the HTTP test, sentence by sentence. Correct what the build contradicted, naming the drift in the commit. Delete this plan.
  Verification: a reviewer who has not built it reads each spec and runs the HTTP test, and finds no sentence the test contradicts; every spec `last-verified` is the close date; this file is gone.

## Deviation log

- 2026-10-27, before step 1, decisions with the user: hard gate at session end backed by a Stop hook that blocks at most twice; two agent sessions or a human promote, flagged and no self-confirm; database in `~/.trail/<id>/` with a committed id file; identical caps for humans and agents. Text export to git and the start-gate fallback recorded in `docs/roadmap.md`. Prerequisites serve the chain instead of gating. Seed and measurement project is MyDot, not Nibble reports.

- 2026-10-27, before step 1, roast-plan bench. Reordered from horizontal layers to a vertical thread (Beck). Corrected the premise that the goldmark adapter sits near the MCP tools and supports details: it is `richtext/markdown.go`, has no details, and drops unknown constructs; details removed from the Trail schema and the adapter's drop replaced by a named refusal (all four judges). Moved anchors before the write path, which needed them (Beck). Added recorder middleware so logging is structural (Ousterhout). Added the bearer token, Host and Origin checks, the `untrusted` body envelope, and the private-range fetch guard (Munger). Added `schema_version`, the `schema` integer on bodies, `contract: 1` and the golden file (Winters). Made signs the single owner of the fan-out cap (Winters). Replaced the grep-based close verification with a reading (Beck). Added Unknowns 3 and 6 and the grep comparison in step 9 (Munger). Rewrote Non-goals to name rejected alternatives (Munger).
- 2026-10-27, step 1. Built with a `budget` argument on `read` from an earlier brief; removed, the body is served whole per the user's decision that prerequisite chains are never trimmed. Spec drift resolved as spec stale: `session_start` added to the event tool list (orient is its response, not a tool); `body_schema` is a column, not a JSON field; signs table stores structure only; `read` depth `summary` defined as the first paragraph; the root summary is seeded without an anchor as the one exception; CLAUDE.md run line corrected to the Trail home path.
- 2026-10-27, after step 1, decision with the user: relationships are typed links in one table (`requires`, `see_also`, `supersedes`, `conflicts_with`) with computed backlinks; only `requires` brings bodies, the rest are edge lines; the second axis of the map is tags served as virtual signs, one home per entry, no multiple parents. Lands in steps 3 and 6.
- 2026-10-27, after step 1, decision with the user: no token, no auth of any kind; the Host and Origin check stays. Code, test and specs updated. Tag vocabulary with definitions in orient, undefined tags flagged, and the hook signpost added to steps 8 and 10 as the mechanisms that teach the agent the protocol.
- 2026-10-27, after step 1, decision with the user: Trails is agent-neutral. Protocol text moved to `protocol/PROTOCOL.md`, the Claude Code skill became one adapter among five, hooks became two HTTP endpoints plus shared shell scripts, and `adapters.md` was added to the spec. Step 10 widened accordingly.
- 2026-10-27, step 2. Fixture path corrected to the package's own testdata directory; `body_schema` already landed as a column in step 1 so the sentence was stale; storage.md now names the table's child nodes, the no-level heading refusal, schemeless hrefs, the `/media/` image path, and how the adapter's `unsupported` refusal relates to the walker's `schema:*` rules.
