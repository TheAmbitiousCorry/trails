# Trails roadmap

Intent, ordered. Present truth lives in `docs/context/`; live work lives in `docs/plans/`.

## Now

## Next

- **A second machine or person can clone a repo and have its map**: today the map lives only in one Trail home directory, so a clone has no signs and a reviewer cannot see entry changes beside the code they describe. Why next: the walking skeleton proves the map on one machine first; this is the first thing that hurts after it works. Domains: storage, entries, principles. Appetite: large.
- **A human can edit an entry in place from the sign page**: the skeleton ships read-only pages, so every correction goes through an agent or the database. Why next: the provisional queue and gap queue are useless without a way to act on them in the browser. Domains: web_ui, entries. Appetite: large.

## Later

- **Gaps left by one session are filled by the next session that needs the same area**: the end-of-session hard gate depends on agents calling `session_end`; if step 9 of the walking skeleton shows poor compliance, gating `orient` on inherited gaps is the fallback. What would promote it: a forced-end rate above a quarter of sessions in the trail log. Domains: trail, mcp, signs. Appetite: small.
- **Humans can write longer entries than agents without widening what agents read**: the caps are identical for everyone now; if browser edits are refused often enough to deter writing, a separate body cap for human writes, with structural caps unchanged, is the candidate. What would promote it: the rejections view showing the word cap firing mostly on human writes. Domains: entries, web_ui. Appetite: small.
