---
domain: web_ui
last-verified: 2026-10-27
---

# Web UI

The browser is the human's view of the same database the MCP serves. It calls the same internal write path, with `actor: user`, so every edit is a logged event and the same caps apply to humans and agents.

## Pages

- **Map.** The full tree, collapsed to domains, each node coloured by tier with its stale count and open-gap count. Click to expand. Heat toggles node size by reads in the last 30 days. A tag selector re-draws the map as a virtual sign tree for that tag, and a second selector crosses it with a sign to show one matrix cell.
- **Sign page.** A sign's summary and edges, as the agent sees them, plus the entries grouped by kind with renders shown inline, examples rendered live from their anchors, and each entry's prerequisite chain length.
- **Entry editor.** Click an entry to edit it in place with Tiptap. Save runs the write rules; a refusal shows the rule and fix next to the save button. A human save sets tier `verified`.
- **Session trail.** The route for one session: task at the top, then each sign and entry in order, turn-backs and prerequisite chains marked, off-trail reads shown in a side column, and the entries the session later wrote or confirmed highlighted.
- **Gap queue.** Open gaps with the sign they belong to, the query or paths, and fill, merge, dismiss actions.
- **Provisional queue.** Provisional entries sorted by reads, with confirm, edit and delete.
- **Rejections and dismissals.** Two tables over the event log.
- **Tags.** The vocabulary with descriptions and use counts; undefined tags at the top, with describe and merge actions.

## Editor

Tiptap v3 with StarterKit, Link, Image, Table, code blocks with highlighting, and a slash menu. No collaboration extension, no mentions. Images upload to the local media directory and the body references them by opaque key. The schema is defined once in TypeScript and mirrored in the Go validator, pinned by shared fixture files; a fixture that fails in either validator fails the build.
