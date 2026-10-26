#!/bin/sh
# SessionStart hook: tells the agent its runtime session id so it can pass it to session_start.
# TRAILS_SESSION_FIELD names the stdin JSON field (default session_id); TRAILS_SESSION_ENV names an environment variable that wins when set.
. "$(dirname "$0")/trails-common.sh"
input=$(cat)
id=$(trails_session_id "$input")
[ -n "$id" ] || exit 0
emit_context SessionStart "Trails runtime_session_id: $id"
