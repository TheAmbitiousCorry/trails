#!/bin/sh
# SessionStart hook: starts the server when the repo opted in and none answers, then tells the agent its runtime session id so it can pass it to session_start.
# TRAILS_SESSION_FIELD names the stdin JSON field (default session_id); TRAILS_SESSION_ENV names an environment variable that wins when set.
# A repo opts in to autostart by committing .trail/id. Without it this hook starts nothing and says nothing about the server.
# The server address is TRAILS_URL, else 127.0.0.1 on the port in <repo>/.trail/port, else 7777. The repo is CLAUDE_PROJECT_DIR, else the payload's cwd, else the working directory.
# TRAILS_BIN names the binary (default trails on PATH). The common path, a server already up, must stay one curl with a 1 second cap.
. "$(dirname "$0")/trails-common.sh"
input=$(cat)
trails_init "$input"
repo=$TRAILS_REPO
note=
if [ -f "$repo/.trail/id" ] && ! curl -sf --max-time 1 -o /dev/null "$TRAILS_URL/api/map"; then
  bin=${TRAILS_BIN:-trails}
  if command -v "$bin" >/dev/null 2>&1; then
    addr=${TRAILS_URL#*://}
    addr=${addr%%/*}
    log=$HOME/.trail/server-$(tr -d ' \n' < "$repo/.trail/id").log
    mkdir -p "$HOME/.trail"
    # Output goes to the log so the runtime's hook pipe closes while the server keeps running.
    if command -v setsid >/dev/null 2>&1; then
      setsid nohup "$bin" serve --repo "$repo" --addr "$addr" >>"$log" 2>&1 </dev/null &
    else
      nohup "$bin" serve --repo "$repo" --addr "$addr" >>"$log" 2>&1 </dev/null &
    fi
    i=0
    until curl -sf --max-time 1 -o /dev/null "$TRAILS_URL/api/map"; do
      i=$((i + 1)); [ "$i" -lt 15 ] || break
      sleep 0.2
    done
  else
    note="Trails: no server and no binary; see adapters/README.md"
  fi
fi

# Claude Code reads the MCP port from TRAILS_PORT; say so when the repo's port file disagrees.
if [ -n "${CLAUDE_PROJECT_DIR:-}" ] && [ -n "$TRAILS_FILE_PORT" ] && [ "$TRAILS_FILE_PORT" != "${TRAILS_PORT:-7777}" ]; then
  note=${note:+$note
}"Trails: this repo's .trail/port is $TRAILS_FILE_PORT but the MCP entry points at ${TRAILS_PORT:-7777}; add \"env\": {\"TRAILS_PORT\": \"$TRAILS_FILE_PORT\"} to .claude/settings.json"
fi

id=$(trails_session_id "$input")
text=$note
[ -z "$id" ] || text=${text:+$text
}"Trails runtime_session_id: $id"
[ -n "$text" ] || exit 0
emit_context SessionStart "$text"
