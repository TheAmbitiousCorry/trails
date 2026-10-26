#!/bin/sh
# Stop hook: blocks the stop while the Trails session is open. The server counts the calls and force-closes the session on the third.
# TRAILS_BLOCK_FORMAT is block (default), deny or cursor.
. "$(dirname "$0")/trails-common.sh"
input=$(cat)
id=$(trails_session_id "$input")
[ -n "$id" ] || exit 0
reply=$(jq -cn --arg s "$id" '{runtime_session_id: $s}' | post stop-check) || exit 0
n=$(printf '%s' "$reply" | jq -r 'select(.open == true) | .gaps | length' 2>/dev/null)
[ -n "$n" ] || exit 0
reason="Trails session still open with $n gaps: call session_end, resolve each gap with resolve_gap, then stop"
case "${TRAILS_BLOCK_FORMAT:-block}" in
  deny) jq -cn --arg r "$reason" '{decision: "deny", reason: $r}' ;;
  cursor) jq -cn --arg r "$reason" '{followup_message: $r}' ;;
  *) jq -cn --arg r "$reason" '{decision: "block", reason: $r}' ;;
esac
