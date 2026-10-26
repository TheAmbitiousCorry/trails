---
domain: principles
last-verified: 2026-10-27
---

# Principles

Trails exists so an agent working in a project can find what it does not know, read only that, and leave a record a human can follow. Every design rule below serves one of those three.

1. **The server is the editor.** Caps, duplicate checks, anchor rules and trust tiers are enforced in the write path, not requested in a prompt. A prompt says what good looks like; the server refuses what is not.
2. **Nothing is authored twice.** Signs, indexes, summaries-of-children and routing are computed from entries. There is no hand-maintained index.
3. **Agents see the next sign, never the whole map.** An agent call returns a local view: where it stands, what is above, what is below. The full map exists only in the browser.
4. **The server does the lifting.** One call returns everything the reader needs for that step: a read brings its whole prerequisite chain in full, a search brings its sign paths, a refusal brings its fix. An agent never needs a second call to understand the first, and the server never substitutes a summary for a body the reader needs.
5. **Small units, hard caps.** A leaf is at most 300 words. A sign has at most seven edges. Over the cap, the write is refused with "split this". Caps are configuration per kind, and the default is tuned from trail data, not argued from literature.
6. **Staleness is computed, not remembered.** Every leaf has an anchor the server can re-check. No date in Trails depends on a human or an agent remembering to bump it.
7. **Agent writes are provisional.** They rank below verified, carry the label in every response, and expire unread. A human write or a human confirm is what makes an entry verified. An agent never overwrites a verified entry; it proposes a conflicting provisional one.
8. **Every read is a trail event.** The MCP is the only agent path, so the trail is complete by construction. Native file reads forwarded by a hook appear as off-trail events.
9. **A gap has a location.** A miss is recorded at the sign where the missing edge should have been, and is resolved there: filled, merged, or dismissed with a reason.
10. **Describe the why, point at the what.** An entry that paraphrases its anchor is refused. Entries hold decisions, constraints, who asked, what failed. Code is read live from the anchor.
11. **Supersede, do not tombstone.** A replaced entry gets `superseded_by` and leaves default search. Nothing in the map says "removed". History lives in versions and events.

12. **Any agent, same map.** The server speaks MCP and HTTP and knows nothing about the caller. Runtime-specific text and configuration live in adapters; a runtime with no hooks still gets the protocol from `session_start` and the server-side fallbacks.

## Non-goals

- Trails is not a collaborative editor. One human edits one entry at a time. There is no CRDT and no realtime sidecar.
- Trails has no users, roles or login. One database per project, one trusted local machine. Nothing from MyDot's permission system reaches it.
- Trails does not store code. It anchors to code and renders it live.
- Trails does not run in the cloud. It runs next to the repo it describes.
