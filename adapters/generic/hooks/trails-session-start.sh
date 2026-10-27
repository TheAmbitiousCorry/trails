#!/bin/sh
# SessionStart hook: registers the repo with the server (starting the server first when none answers), then tells the agent its project id and runtime session id so it can pass both to session_start.
# TRAILS_SESSION_FIELD names the stdin JSON field (default session_id); TRAILS_SESSION_ENV names an environment variable that wins when set.
# A repo opts in by committing .trail/id, found at or above the repo. Without it this hook contacts no server and says nothing about the project.
# The server address is TRAILS_URL, else TRAILS_ADDR, else 127.0.0.1:7777. The repo is CLAUDE_PROJECT_DIR, else the payload's cwd, else the working directory.
# TRAILS_BIN names the binary (default trails on PATH). The common path, a server already up, is one curl with a 1 second cap.
. "$(dirname "$0")/trails-common.sh"
input=$(cat)
trails_init "$input"
note=
reached=
# register sets code to the HTTP status of the registration, 000 when nothing answered.
register() {
  code=$(jq -cn --arg r "$TRAILS_ROOT" '{repo: $r}' | curl -s --max-time 1 -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' --data-binary @- "$TRAILS_URL/api/projects")
}
if [ -n "$TRAILS_ROOT" ]; then
  register
  if [ "$code" = 000 ]; then
    bin=${TRAILS_BIN:-trails}
    if command -v "$bin" >/dev/null 2>&1; then
      addr=${TRAILS_URL#*://}
      addr=${addr%%/*}
      log=$HOME/.trail/server.log
      mkdir -p "$HOME/.trail"
      # Output goes to the log so the runtime's hook pipe closes while the server keeps running.
      if command -v setsid >/dev/null 2>&1; then
        setsid nohup "$bin" serve --addr "$addr" >>"$log" 2>&1 </dev/null &
      else
        nohup "$bin" serve --addr "$addr" >>"$log" 2>&1 </dev/null &
      fi
      i=0
      while [ "$code" = 000 ] && [ "$i" -lt 15 ]; do
        i=$((i + 1))
        sleep 0.2
        register
      done
    else
      note="Trails: no server and no binary; see adapters/README.md"
    fi
  fi
  [ "$code" = 000 ] || reached=1
fi

# Claude Code reads the MCP port from TRAILS_PORT; say so when the server the hook reached is on another port.
if [ -n "$reached" ] && [ -n "${CLAUDE_PROJECT_DIR:-}" ]; then
  hostport=${TRAILS_URL#*://}
  hostport=${hostport%%/*}
  port=${hostport##*:}
  if [ "$port" != "${TRAILS_PORT:-7777}" ]; then
    note=${note:+$note
}"Trails: the server answered on port $port but the MCP entry points at port ${TRAILS_PORT:-7777}; add \"env\": {\"TRAILS_PORT\": \"$port\"} to ~/.claude/settings.json"
  fi
fi

id=$(trails_session_id "$input")
text=$note
[ -z "$TRAILS_PROJECT" ] || text=${text:+$text
}"Trails project: $TRAILS_PROJECT"
[ -z "$id" ] || text=${text:+$text
}"Trails runtime_session_id: $id"
[ -n "$text" ] || exit 0
emit_context SessionStart "$text"
