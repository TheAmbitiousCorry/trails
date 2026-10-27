# Sourced by the trails-*.sh hook scripts. Needs only sh, curl and jq.
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

# trails_init JSON sets TRAILS_REPO and, when TRAILS_URL is unset, TRAILS_URL from <repo>/.trail/port (7777 when the file is absent or not a port).
# The repo is CLAUDE_PROJECT_DIR when set, else the payload's cwd, else the working directory. Every script calls this right after reading stdin.
trails_init() {
  TRAILS_REPO=${CLAUDE_PROJECT_DIR:-}
  [ -n "$TRAILS_REPO" ] || TRAILS_REPO=$(printf '%s' "$1" | field_of cwd)
  [ -n "$TRAILS_REPO" ] || TRAILS_REPO=$PWD
  TRAILS_FILE_PORT=$(trails_file_port "$TRAILS_REPO")
  TRAILS_URL=${TRAILS_URL:-http://127.0.0.1:${TRAILS_FILE_PORT:-7777}}
}

# trails_file_port REPO prints the port in REPO/.trail/port, or nothing when the file is absent or not a number from 1 to 65535.
trails_file_port() {
  [ -f "$1/.trail/port" ] || return 0
  p=$(tr -d ' \t\r\n' < "$1/.trail/port" 2>/dev/null) || return 0
  case "$p" in
    ''|*[!0-9]*) return 0 ;;
  esac
  [ "$p" -ge 1 ] 2>/dev/null && [ "$p" -le 65535 ] 2>/dev/null && printf '%s' "$p"
  return 0
}
