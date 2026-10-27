#!/bin/sh
# After-read hook: reports the path just read and prints a signpost when an entry anchors it.
# TRAILS_PATH_FIELD names the stdin JSON field holding the path; by default the first present of tool_input.file_path, tool_input.path (Grep and Glob) and tool_response.file.filePath.
. "$(dirname "$0")/trails-common.sh"
input=$(cat)
trails_init "$input"
id=$(trails_session_id "$input")
[ -n "$id" ] || exit 0
if [ -n "$TRAILS_PATH_FIELD" ]; then
  body=$(printf '%s' "$input" | jq -c --arg s "$id" --arg f "$TRAILS_PATH_FIELD" '{runtime_session_id: $s, path: (getpath($f | split(".")) // "" | tostring)}' 2>/dev/null) || exit 0
else
  body=$(printf '%s' "$input" | jq -c --arg s "$id" '{runtime_session_id: $s, path: ((.tool_input.file_path // .tool_input.path // .tool_response.file.filePath) // "" | tostring)}' 2>/dev/null) || exit 0
fi
[ "$(printf '%s' "$body" | jq -r .path)" != "" ] || exit 0
reply=$(printf '%s' "$body" | post off-trail) || exit 0
line=$(printf '%s' "$reply" | jq -r 'select(.entry_id) | "Trails has an entry for this file: \(.entry_id), \(.read_when)"' 2>/dev/null)
[ -n "$line" ] || exit 0
emit_context PostToolUse "$line"
