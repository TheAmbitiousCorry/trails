#!/bin/sh
# Before-read hook: asks the server whether the agent has read the entry that anchors the path it is about to open, and denies the read when it has not.
# TRAILS_PATH_FIELD names the stdin JSON field holding the path; by default the first present of tool_input.file_path and tool_input.path (Grep and Glob).
# Prints the Claude Code PreToolUse deny form on turned_back and unstarted; prints nothing, and so lets the read through, on checked, unmapped or an unreachable server.
. "$(dirname "$0")/trails-common.sh"
input=$(cat)
trails_init "$input"
id=$(trails_session_id "$input")
[ -n "$id" ] || exit 0
if [ -n "$TRAILS_PATH_FIELD" ]; then
  body=$(printf '%s' "$input" | jq -c --arg s "$id" --arg f "$TRAILS_PATH_FIELD" '{runtime_session_id: $s, path: (getpath($f | split(".")) // "" | tostring)}' 2>/dev/null) || exit 0
else
  body=$(printf '%s' "$input" | jq -c --arg s "$id" '{runtime_session_id: $s, path: ((.tool_input.file_path // .tool_input.path) // "" | tostring)}' 2>/dev/null) || exit 0
fi
[ "$(printf '%s' "$body" | jq -r .path)" != "" ] || exit 0
reply=$(printf '%s' "$body" | post_hook pre-read) || exit 0
reason=$(printf '%s' "$reply" | jq -r --arg p "$TRAILS_PROJECT" 'if .decision == "turned_back" then "Trails has an entry for this file: \(.entry_id), \(.read_when). Read it with read(\(.entry_id)) first, then open the file." elif .decision == "unstarted" then "Trails: this repo has a map and an entry for this file (\(.entry_id), \(.read_when)). Start with session_start(task, project: \($p)) and read(\(.entry_id)) before opening it." else empty end' 2>/dev/null)
[ -n "$reason" ] || exit 0
jq -cn --arg r "$reason" '{hookSpecificOutput: {hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: $r}}'
