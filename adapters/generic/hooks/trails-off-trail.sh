#!/bin/sh
# After-read hook: reports the path just read and prints a signpost when an entry anchors it.
# TRAILS_PATH_FIELD names the stdin JSON field holding the path (default tool_input.file_path).
. "$(dirname "$0")/trails-common.sh"
input=$(cat)
id=$(trails_session_id "$input")
[ -n "$id" ] || exit 0
body=$(printf '%s' "$input" | jq -c --arg s "$id" --arg f "${TRAILS_PATH_FIELD:-tool_input.file_path}" '{runtime_session_id: $s, path: (getpath($f | split(".")) // "" | tostring)}' 2>/dev/null) || exit 0
reply=$(printf '%s' "$body" | post off-trail) || exit 0
line=$(printf '%s' "$reply" | jq -r 'select(.entry_id) | "Trails has an entry for this file: \(.entry_id), \(.read_when)"' 2>/dev/null)
[ -n "$line" ] || exit 0
emit_context PostToolUse "$line"
