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

# post_hook ROUTE is post with the repo's project id added to the body, so the server needs no search across projects.
post_hook() {
  if [ -n "$TRAILS_PROJECT" ]; then
    jq -c --arg p "$TRAILS_PROJECT" '. + {project: $p}' 2>/dev/null | post "$1"
  else
    post "$1"
  fi
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

# trails_init JSON sets TRAILS_REPO, TRAILS_ROOT, TRAILS_PROJECT and, when TRAILS_URL is unset, TRAILS_URL from TRAILS_ADDR, else 127.0.0.1 on TRAILS_PORT, else the address in $HOME/.trail/server while its pid is alive, else 127.0.0.1:7777.
# The repo is CLAUDE_PROJECT_DIR when set, else the payload's cwd, else the working directory. TRAILS_ROOT is the nearest directory at or above it that holds .trail/id
# (empty when none does) and TRAILS_PROJECT is the id in that file. Every script calls this right after reading stdin.
trails_init() {
  TRAILS_REPO=${CLAUDE_PROJECT_DIR:-}
  [ -n "$TRAILS_REPO" ] || TRAILS_REPO=$(printf '%s' "$1" | field_of cwd)
  [ -n "$TRAILS_REPO" ] || TRAILS_REPO=$PWD
  if [ -z "${TRAILS_URL:-}" ]; then
    _addr=${TRAILS_ADDR:-}
    [ -n "$_addr" ] || [ -z "${TRAILS_PORT:-}" ] || _addr=127.0.0.1:$TRAILS_PORT
    if [ -z "$_addr" ] && [ -f "$HOME/.trail/server" ]; then
      read -r _saddr _spid < "$HOME/.trail/server" || true
      [ -z "${_saddr:-}" ] || [ -z "${_spid:-}" ] || ! kill -0 "$_spid" 2>/dev/null || _addr=$_saddr
    fi
    TRAILS_URL=http://${_addr:-127.0.0.1:7777}
  fi
  TRAILS_ROOT=$TRAILS_REPO
  while [ ! -f "$TRAILS_ROOT/.trail/id" ]; do
    [ "$TRAILS_ROOT" != / ] && [ "$TRAILS_ROOT" != . ] || { TRAILS_ROOT=; break; }
    TRAILS_ROOT=$(dirname "$TRAILS_ROOT")
  done
  TRAILS_PROJECT=
  [ -z "$TRAILS_ROOT" ] || TRAILS_PROJECT=$(tr -d ' \t\r\n' < "$TRAILS_ROOT/.trail/id" 2>/dev/null) || TRAILS_PROJECT=
}
