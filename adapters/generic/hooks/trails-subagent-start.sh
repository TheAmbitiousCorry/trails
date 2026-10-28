#!/bin/sh
# SubagentStart hook: tells a subagent its project id and its own runtime session id (the parent's session id with /agent_id appended), so it can call session_start with both.
# It registers nothing and starts no server; it prints nothing when the repo has no .trail/id.
. "$(dirname "$0")/trails-common.sh"
input=$(cat)
trails_init "$input"
[ -n "$TRAILS_PROJECT" ] || exit 0
id=$(trails_session_id "$input")
text="Trails project: $TRAILS_PROJECT"
[ -z "$id" ] || text="$text
Trails runtime_session_id: $id"
emit_context SubagentStart "$text"
