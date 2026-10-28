#!/bin/sh
# SessionStart hook: registers the repo with the server (starting the server first when none answers), sending the runtime session id so the server can place a session_start that carries only that id, then tells the agent its project id and runtime session id.
# TRAILS_SESSION_FIELD names the stdin JSON field (default session_id); TRAILS_SESSION_ENV names an environment variable that wins when set.
# A repo opts in by committing .trail/id, found at or above the repo. With none there, the hook registers the repos one or two levels below (skipping hidden directories, node_modules and vendor) and lists them; with none anywhere it contacts no server and only prints a nudge to run trails register.
# The server address is TRAILS_URL, else TRAILS_ADDR, else 127.0.0.1 on TRAILS_PORT, else 127.0.0.1:7777. The repo is CLAUDE_PROJECT_DIR, else the payload's cwd, else the working directory.
# TRAILS_BIN names the binary (default trails on PATH). The common path, a server already up, is one curl with a 1 second cap.
. "$(dirname "$0")/trails-common.sh"
input=$(cat)
trails_init "$input"
id=$(trails_session_id "$input")
# repos holds the directories to register, one per line; below holds the "<id> (<relative path>)" list when they were found under the directory.
nl='
'
repos=
below=
if [ -n "$TRAILS_ROOT" ]; then
  repos=$TRAILS_ROOT
else
  for f in "$TRAILS_REPO"/*/.trail/id "$TRAILS_REPO"/*/*/.trail/id; do
    [ -f "$f" ] || continue
    rel=${f#"$TRAILS_REPO"/}
    rel=${rel%/.trail/id}
    case "/$rel/" in */node_modules/*|*/vendor/*) continue ;; esac
    rid=$(tr -d ' \t\r\n' < "$f" 2>/dev/null) || rid=
    [ -n "$rid" ] || continue
    repos=${repos:+$repos$nl}$TRAILS_REPO/$rel
    below=${below:+$below, }"$rid ($rel)"
  done
  if [ -z "$repos" ]; then
    emit_context SessionStart "Trails: this repo has no .trail/id; run trails register to put it on the trail"
    exit 0
  fi
fi
note=
reached=
# register REPO sets code to the HTTP status of the registration, 000 when nothing answered, and reply to the body.
register() {
  resp=$(jq -cn --arg r "$1" --arg s "$id" '{repo: $r} + (if $s == "" then {} else {runtime_session_id: $s} end)' | curl -s --max-time 1 -w '\n%{http_code}' -H 'Content-Type: application/json' --data-binary @- "$TRAILS_URL/api/projects")
  code=${resp##*
}
  reply=${resp%
*}
  if [ "$code" = 409 ]; then
    fix=$(printf '%s' "$reply" | field_of fix)
    [ -z "$fix" ] || note=${note:+$note
}"Trails: $fix"
  fi
}
first=${repos%%
*}
register "$first"
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
      register "$first"
    done
  else
    note="Trails: no server and no binary; see adapters/README.md"
  fi
fi
[ "$code" = 000 ] || reached=1
if [ -n "$reached" ]; then
  rest=${repos#"$first"}
  rest=${rest#"$nl"}
  while [ -n "$rest" ]; do
    r=${rest%%
*}
    register "$r"
    case "$rest" in *"$nl"*) rest=${rest#*"$nl"} ;; *) rest= ;; esac
  done
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

text=$note
if [ -n "$below" ]; then
  text=${text:+$text
}"Trails projects under this directory: $below"
elif [ -n "$TRAILS_PROJECT" ]; then
  text=${text:+$text
}"Trails project: $TRAILS_PROJECT"
fi
[ -z "$id" ] || text=${text:+$text
}"Trails runtime_session_id: $id"
[ -n "$text" ] || exit 0
emit_context SessionStart "$text"
