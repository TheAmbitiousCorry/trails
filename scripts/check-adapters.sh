#!/bin/sh
# Fails when an adapter's protocol copy no longer matches protocol/PROTOCOL.md. The compared part starts at the first protocol sentence, so each copy may carry its own header.
set -e
cd "$(dirname "$0")/.."
body() { awk '/^Trails is the project.s map/{p=1} p' "$1"; }
body protocol/PROTOCOL.md > "${TMPDIR:-/tmp}/trails-protocol-body"
for f in adapters/claude-code/skills/trails/SKILL.md adapters/codex/AGENTS.md adapters/gemini-cli/GEMINI.md adapters/cursor/rules.md adapters/generic/INSTRUCTIONS.md; do
  body "$f" | diff -q - "${TMPDIR:-/tmp}/trails-protocol-body" >/dev/null || { echo "drift: $f"; exit 1; }
done
echo "adapters match protocol"
