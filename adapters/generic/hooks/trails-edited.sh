#!/bin/sh
# After-edit hook: reports the path just edited and prints the entries anchored to it, to be updated or confirmed before the session ends.
# TRAILS_PATH_FIELD names the stdin JSON field holding the path; by default tool_input.file_path, else tool_input.notebook_path (NotebookEdit).
. "$(dirname "$0")/trails-common.sh"
input=$(cat)
id=$(trails_session_id "$input")
[ -n "$id" ] || exit 0
if [ -n "$TRAILS_PATH_FIELD" ]; then
  body=$(printf '%s' "$input" | jq -c --arg s "$id" --arg f "$TRAILS_PATH_FIELD" '{runtime_session_id: $s, path: (getpath($f | split(".")) // "" | tostring)}' 2>/dev/null) || exit 0
else
  body=$(printf '%s' "$input" | jq -c --arg s "$id" '{runtime_session_id: $s, path: (.tool_input.file_path // .tool_input.notebook_path // "" | tostring)}' 2>/dev/null) || exit 0
fi
[ "$(printf '%s' "$body" | jq -r .path)" != "" ] || exit 0
reply=$(printf '%s' "$body" | post edited) || exit 0
line=$(printf '%s' "$reply" | jq -r 'select((.entries // []) | length > 0) | "Trails has \(.entries | length) entries anchored to this file; update or confirm each before ending the session: \([.entries[] | "\(.entry_id) (\(.read_when))"] | join(", "))"' 2>/dev/null)
[ -n "$line" ] || exit 0
emit_context PostToolUse "$line"
