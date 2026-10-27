---
name: trail
description: Open the Trails browser at the current session's route, the map or the graph
---

# Trail

When the user asks to see the trail, run `trails open trail` with the Bash tool. Run `trails open map` when they ask for the map and `trails open graph` when they ask for the graph.

The command opens the browser and prints a URL. Tell the user that URL. If it exits 1, no server answered: relay the message it printed and do not retry.
