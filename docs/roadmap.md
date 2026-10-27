# Trails roadmap

Intent, ordered. Present truth lives in `docs/context/`; live work lives in `docs/plans/`.

## Now

## Next

- **A second machine or person can clone a repo and have its map**: today the map lives only in one Trail home directory, so a clone has no signs and a reviewer cannot see entry changes beside the code they describe. Why next: the walking skeleton proves the map on one machine first; this is the first thing that hurts after it works. Domains: storage, entries, principles. Appetite: large.
- **A human can act on the map from the browser**: edit an entry in place, confirm or delete a provisional one, fill, merge or dismiss a gap, describe or merge a tag, resolve a conflicting pair, restore a version. The skeleton ships read-only pages, so every correction goes through an agent or the database, and the human write and confirm paths in the server have no caller. Why next: the queues are lists nobody can act on. Domains: web_ui, entries, trail. Appetite: large.

## Later

- **An agent can attach a screenshot or diagram to a module page**: the `render` kind validates but nothing stores or shows an image, and there is no media directory. What would promote it: the first UI project walked with Trails. Domains: entries, storage, web_ui. Appetite: small.
- **The measurement shows whether the map beats grep**: two runs against MyDot were correct both ways and cheaper with Trails, but the tasks named their modules so the plain session guessed the spec filenames. What would promote it: a run on a task that does not name its modules, with the stale front-door routing in play. Domains: none, measurement. Appetite: small.

- **Reading around the map opens a gap where the map was thin**: three or more native reads under unanchored paths after a look should open a gap at that sign, so the map learns where agents bypass it. What would promote it: off-trail rows clustering under one sign in the trail view. Domains: trail, adapters. Appetite: small.

- **Gaps left by one session are filled by the next session that needs the same area**: the end-of-session hard gate depends on agents calling `session_end`; if step 9 of the walking skeleton shows poor compliance, gating `orient` on inherited gaps is the fallback. What would promote it: a forced-end rate above a quarter of sessions in the trail log. Domains: trail, mcp, signs. Appetite: small.
- **Humans can write longer entries than agents without widening what agents read**: the caps are identical for everyone now; if browser edits are refused often enough to deter writing, a separate body cap for human writes, with structural caps unchanged, is the candidate. What would promote it: the rejections view showing the word cap firing mostly on human writes. Domains: entries, web_ui. Appetite: small.
- **A human can add a sign without SQL**: the MyDot map's seven domain signs were inserted by hand because neither the MCP nor the browser creates signs; the fan-out rule proposes a split but nobody can act on it. What would promote it: the first split-this-fork refusal in a real session. Domains: signs, web_ui, mcp. Appetite: small.
- **A reader jumps to any entry from the keyboard**: the sidebar search filters the tree, but a Ctrl-K palette with recents is what every wiki in the research ships. What would promote it: the map passing about 300 entries, where scrolling the tree stops working. Domains: web_ui. Appetite: small.
