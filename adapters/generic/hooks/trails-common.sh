# Sourced by the trails-*.sh hook scripts. Needs only sh, curl and jq.
TRAILS_URL=${TRAILS_URL:-http://127.0.0.1:7777}

# The hook scripts never fail a turn: a missing tool or unreachable server ends the script quietly.
command -v jq >/dev/null 2>&1 && command -v curl >/dev/null 2>&1 || exit 0

# field_of NAME prints the dotted JSON path NAME (for example tool_input.file_path) from stdin, or nothing.
field_of() {
  jq -r --arg f "$1" 'getpath($f | split(".")) // empty | tostring' 2>/dev/null
}

# post ROUTE reads a JSON body on stdin and prints the reply; fails when the server does.
post() {
  curl -sf --connect-timeout 1 --max-time 3 -H 'Content-Type: application/json' --data-binary @- "$TRAILS_URL/api/$1"
}

# emit_context EVENT TEXT prints TEXT in the runtime's injection form. TRAILS_EVENT overrides EVENT in the hook-json form.
emit_context() {
  case "${TRAILS_CONTEXT_FORMAT:-plain}" in
    hook-json) jq -cn --arg e "${TRAILS_EVENT:-$1}" --arg t "$2" '{hookSpecificOutput: {hookEventName: $e, additionalContext: $t}}' ;;
    cursor) jq -cn --arg t "$2" '{additional_context: $t}' ;;
    *) printf '%s\n' "$2" ;;
  esac
}

# trails_session_id JSON prints the runtime session id from the env variable named by TRAILS_SESSION_ENV, else from the stdin field TRAILS_SESSION_FIELD.
trails_session_id() {
  case "${TRAILS_SESSION_ENV:-}" in
    ''|*[!A-Za-z0-9_]*) ;;
    *) eval "v=\${$TRAILS_SESSION_ENV:-}"; [ -z "$v" ] || { printf '%s' "$v"; return; } ;;
  esac
  printf '%s' "$1" | field_of "${TRAILS_SESSION_FIELD:-session_id}"
}
