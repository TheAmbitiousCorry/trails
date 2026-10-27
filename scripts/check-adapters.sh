#!/bin/sh
# Fails when an adapter's protocol copy no longer matches protocol/PROTOCOL.md. The compared part starts at the first protocol sentence and ends before a "## Seeing the trail" heading, so each copy may carry its own header and that trailing section.
set -e
cd "$(dirname "$0")/.."
body() { awk '/^Trails is the project.s map/{p=1} /^## Seeing the trail/{p=0} p{b[n++]=$0} END{while(n>0 && b[n-1]=="")n--; for(i=0;i<n;i++)print b[i]}' "$1"; }
body protocol/PROTOCOL.md > "${TMPDIR:-/tmp}/trails-protocol-body"
for f in adapters/claude-code/skills/trails/SKILL.md adapters/codex/AGENTS.md adapters/gemini-cli/GEMINI.md adapters/cursor/rules.md adapters/generic/INSTRUCTIONS.md; do
  body "$f" | diff -q - "${TMPDIR:-/tmp}/trails-protocol-body" >/dev/null || { echo "drift: $f"; exit 1; }
done
echo "adapters match protocol"
