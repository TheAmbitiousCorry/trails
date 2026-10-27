#!/bin/sh
# Starts the built server against a temp repo and feeds each shared hook script a sample payload per runtime, asserting stdout shapes.
set -eu
cd "$(dirname "$0")/.."
root=$(pwd)
hooks=$root/adapters/generic/hooks
tmp=$(mktemp -d)
pid=
pid2=
cleanup() { [ -z "$pid" ] || kill "$pid" 2>/dev/null || true; [ -z "$pid2" ] || kill "$pid2" 2>/dev/null || true; rm -rf "$tmp"; }
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
# expect LABEL JQ_FILTER OUTPUT: the filter must print true on the JSON output.
expect() { [ "$(printf '%s' "$3" | jq -r "$2" 2>/dev/null)" = true ] || fail "$1: got '$3'"; }
expect_empty() { [ -z "$2" ] || fail "$1: expected no output, got '$2'"; }

(cd server && go build -o "$tmp/trails" ./cmd/trails)
mkdir -p "$tmp/repo/src"
echo 'package a' > "$tmp/repo/src/a.go"

port=$(( 20000 + $(od -An -N2 -tu2 /dev/urandom | tr -d ' ') % 30000 ))
url=http://127.0.0.1:$port
"$tmp/trails" --repo "$tmp/repo" --home "$tmp/home" --addr "127.0.0.1:$port" >"$tmp/server.log" 2>&1 &
pid=$!
i=0
until curl -s -o /dev/null "$url/api/ping"; do
  i=$((i + 1)); [ "$i" -lt 100 ] || { cat "$tmp/server.log" >&2; fail "server did not start"; }
  sleep 0.1
done
export TRAILS_URL=$url

# tool NAME ARGS_JSON calls an MCP tool and prints its structured result.
tool() {
  jq -cn --arg n "$1" --argjson a "$2" '{jsonrpc: "2.0", id: 1, method: "tools/call", params: {name: $n, arguments: $a}}' |
    curl -sf -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' --data-binary @- "$url/mcp" |
    jq -c 'if .result.isError then error(.result.content[0].text) else .result.structuredContent end'
}

for rt in claude codex gemini cursor; do
  tool session_start "{\"task\":\"hook test\",\"task_type\":\"test\",\"runtime_session_id\":\"rt-$rt\"}" >/dev/null
done
sid=$(tool session_start '{"task":"anchor","task_type":"test"}' | jq -r .session_id)
tool write "{\"session_id\":\"$sid\",\"parent_sign_id\":\"root\",\"kind\":\"decision\",\"decided_by\":\"test\",\"decided_at\":\"2026-10-27\",\"title\":\"Package a\",\"read_when\":\"you touch package a\",\"body\":\"Package a is tiny.\",\"anchors\":[{\"type\":\"file\",\"path\":\"src/a.go\"}]}" >/dev/null
entry=$(tool look "{\"session_id\":\"$sid\",\"sign_id\":\"root\"}" | jq -r '[.. | .id? // empty | select(. != "trail")][0]')

# Claude Code: stdin session_id and tool_input.file_path, hook-json context, decision block.
out=$(printf '{"session_id":"rt-claude","hook_event_name":"SessionStart"}' | TRAILS_CONTEXT_FORMAT=hook-json "$hooks/trails-session-start.sh")
expect "claude session-start" '.hookSpecificOutput.hookEventName == "SessionStart" and .hookSpecificOutput.additionalContext == "Trails runtime_session_id: rt-claude"' "$out"
out=$(printf '{"session_id":"rt-claude","tool_input":{"file_path":"%s/src/a.go"}}' "$tmp/repo" | TRAILS_CONTEXT_FORMAT=hook-json "$hooks/trails-off-trail.sh")
expect "claude off-trail" ".hookSpecificOutput.hookEventName == \"PostToolUse\" and .hookSpecificOutput.additionalContext == \"Trails has an entry for this file: $entry, you touch package a\"" "$out"
out=$(printf '{"session_id":"rt-claude","tool_input":{"file_path":"%s/other.txt"}}' "$tmp/repo" | TRAILS_CONTEXT_FORMAT=hook-json "$hooks/trails-off-trail.sh")
expect_empty "claude off-trail unanchored" "$out"
out=$(printf '{"session_id":"rt-claude","stop_hook_active":false}' | TRAILS_BLOCK_FORMAT=block "$hooks/trails-stop.sh")
expect "claude stop" '.decision == "block" and (.reason | startswith("Trails session still open with 0 gaps: call session_end"))' "$out"

# Codex: same stdin shape, session start and stop only.
out=$(printf '{"session_id":"rt-codex"}' | TRAILS_CONTEXT_FORMAT=hook-json "$hooks/trails-session-start.sh")
expect "codex session-start" '.hookSpecificOutput.additionalContext == "Trails runtime_session_id: rt-codex"' "$out"
out=$(printf '{"session_id":"rt-codex"}' | "$hooks/trails-stop.sh")
expect "codex stop" '.decision == "block"' "$out"

# Gemini CLI: id from GEMINI_SESSION_ID, AfterTool event name, decision deny.
out=$(printf '{"session_id":"ignored"}' | GEMINI_SESSION_ID=rt-gemini TRAILS_SESSION_ENV=GEMINI_SESSION_ID TRAILS_CONTEXT_FORMAT=hook-json "$hooks/trails-session-start.sh")
expect "gemini session-start" '.hookSpecificOutput.additionalContext == "Trails runtime_session_id: rt-gemini"' "$out"
out=$(printf '{"tool_input":{"file_path":"src/a.go"}}' | GEMINI_SESSION_ID=rt-gemini TRAILS_SESSION_ENV=GEMINI_SESSION_ID TRAILS_CONTEXT_FORMAT=hook-json TRAILS_EVENT=AfterTool "$hooks/trails-off-trail.sh")
expect "gemini off-trail" ".hookSpecificOutput.hookEventName == \"AfterTool\" and (.hookSpecificOutput.additionalContext | startswith(\"Trails has an entry for this file: $entry\"))" "$out"
out=$(printf '{}' | GEMINI_SESSION_ID=rt-gemini TRAILS_SESSION_ENV=GEMINI_SESSION_ID TRAILS_BLOCK_FORMAT=deny "$hooks/trails-stop.sh")
expect "gemini stop" '.decision == "deny" and (.reason | startswith("Trails session still open"))' "$out"

# Cursor: id from conversation_id, additional_context, followup_message.
out=$(printf '{"conversation_id":"rt-cursor"}' | TRAILS_SESSION_FIELD=conversation_id TRAILS_CONTEXT_FORMAT=cursor "$hooks/trails-session-start.sh")
expect "cursor session-start" '.additional_context == "Trails runtime_session_id: rt-cursor"' "$out"
out=$(printf '{"conversation_id":"rt-cursor","tool_input":{"file_path":"src/a.go"}}' | TRAILS_SESSION_FIELD=conversation_id TRAILS_CONTEXT_FORMAT=cursor "$hooks/trails-off-trail.sh")
expect "cursor off-trail" ".additional_context | startswith(\"Trails has an entry for this file: $entry\")" "$out"
out=$(printf '{"conversation_id":"rt-cursor"}' | TRAILS_SESSION_FIELD=conversation_id TRAILS_BLOCK_FORMAT=cursor "$hooks/trails-stop.sh")
expect "cursor stop" '.followup_message | startswith("Trails session still open")' "$out"

# Plain text is the default injection form.
out=$(printf '{"session_id":"rt-claude"}' | "$hooks/trails-session-start.sh")
[ "$out" = "Trails runtime_session_id: rt-claude" ] || fail "plain session-start: got '$out'"

# Edit gate: an edit to an anchored file replies the entry and the stop hook names it.
out=$(printf '{"session_id":"rt-claude","tool_input":{"file_path":"%s/src/a.go"}}' "$tmp/repo" | TRAILS_CONTEXT_FORMAT=hook-json "$hooks/trails-edited.sh")
expect "claude edited" ".hookSpecificOutput.hookEventName == \"PostToolUse\" and .hookSpecificOutput.additionalContext == \"Trails has 1 entries anchored to this file; update or confirm each before ending the session: $entry (you touch package a)\"" "$out"
out=$(printf '{"session_id":"rt-claude","tool_input":{"file_path":"%s/other.txt"}}' "$tmp/repo" | TRAILS_CONTEXT_FORMAT=hook-json "$hooks/trails-edited.sh")
expect_empty "claude edited unanchored" "$out"
out=$(printf '{"conversation_id":"rt-cursor","file_path":"src/a.go"}' | TRAILS_SESSION_FIELD=conversation_id TRAILS_PATH_FIELD=file_path TRAILS_CONTEXT_FORMAT=cursor "$hooks/trails-edited.sh")
expect "cursor edited" ".additional_context | startswith(\"Trails has 1 entries anchored to this file\")" "$out"
printf '{"session_id":"rt-codex","tool_input":{"file_path":"src/a.go"}}' | "$hooks/trails-edited.sh" >/dev/null
out=$(printf '{"session_id":"rt-codex"}' | TRAILS_BLOCK_FORMAT=block "$hooks/trails-stop.sh")
expect "stop with edits" ".decision == \"block\" and (.reason | contains(\"Trails: entries anchored to files you edited, not yet updated or confirmed: $entry (src/a.go)\"))" "$out"

# The claude session has had one block; two more force it closed and the third prints nothing.
printf '{"session_id":"rt-claude"}' | "$hooks/trails-stop.sh" >/dev/null
out=$(printf '{"session_id":"rt-claude"}' | "$hooks/trails-stop.sh")
expect_empty "stop after forced" "$out"

# Autostart: a repo with .trail/id and no answering server gets TRAILS_BIN started and registration proceeds; a repo without the id starts nothing.
fakehome=$tmp/fakehome
mkdir -p "$fakehome" "$tmp/auto/.trail" "$tmp/noid" "$tmp/fake/api"
echo 'autotest' > "$tmp/auto/.trail/id"
echo '{}' > "$tmp/fake/api/map"
cat > "$tmp/fake/trails" <<'FAKE'
#!/bin/sh
# Stand-in for trails serve --repo R --addr A: records its arguments and pid, then answers GET /api/map on A.
echo "$*" > "$FAKE_DIR/args"
echo $$ > "$FAKE_DIR/pid"
cd "$FAKE_DIR"
exec python3 -m http.server "${5##*:}" --bind 127.0.0.1
FAKE
chmod +x "$tmp/fake/trails"
export FAKE_DIR=$tmp/fake
aport=$(( port + 1 ))
[ "$aport" -lt 65535 ] || aport=$(( port - 1 ))
auto_hook() { # CWD_DIR -> runs the session-start hook with cwd given in the payload
  printf '{"session_id":"rt-auto","cwd":"%s"}' "$1" | HOME=$fakehome TRAILS_URL=http://127.0.0.1:$aport TRAILS_BIN=$tmp/fake/trails "$hooks/trails-session-start.sh"
}
out=$(auto_hook "$tmp/noid")
[ "$out" = "Trails runtime_session_id: rt-auto" ] || fail "no-id session-start: got '$out'"
[ ! -e "$tmp/fake/args" ] || fail "no-id repo started the server"
out=$(auto_hook "$tmp/auto")
pid2=$(cat "$tmp/fake/pid" 2>/dev/null || true)
[ "$out" = "Trails runtime_session_id: rt-auto" ] || fail "autostart session-start: got '$out'"
[ "$(cat "$tmp/fake/args")" = "serve --repo $tmp/auto --addr 127.0.0.1:$aport" ] || fail "autostart args: got '$(cat "$tmp/fake/args")'"
[ -f "$fakehome/.trail/server-autotest.log" ] || fail "autostart wrote no log under ~/.trail"
curl -sf --max-time 1 -o /dev/null "http://127.0.0.1:$aport/api/map" || fail "autostarted server does not answer"
out=$(printf '{"session_id":"rt-auto","cwd":"%s"}' "$tmp/auto" | HOME=$fakehome TRAILS_URL=http://127.0.0.1:$aport TRAILS_BIN=/nonexistent/trails "$hooks/trails-session-start.sh")
[ "$out" = "Trails runtime_session_id: rt-auto" ] || fail "server up, binary missing: got '$out'"
[ -z "${pid2:-}" ] || { kill "$pid2" 2>/dev/null || true; pid2=; }
out=$(printf '{"session_id":"rt-auto","cwd":"%s"}' "$tmp/auto" | HOME=$fakehome TRAILS_URL=http://127.0.0.1:$aport TRAILS_BIN=/nonexistent/trails "$hooks/trails-session-start.sh")
[ "$out" = "Trails: no server and no binary; see adapters/README.md
Trails runtime_session_id: rt-auto" ] || fail "no server, no binary: got '$out'"

# An unknown session and an unreachable server both pass through silently.
out=$(printf '{"session_id":"nobody"}' | "$hooks/trails-stop.sh")
expect_empty "unknown session stop" "$out"
for s in trails-session-start trails-off-trail trails-edited trails-stop; do
  set +e
  out=$(printf '{"session_id":"rt-claude","tool_input":{"file_path":"src/a.go"}}' | TRAILS_BIN=/nonexistent TRAILS_URL=http://127.0.0.1:1 "$hooks/$s.sh" 2>&1)
  rc=$?
  set -e
  [ "$rc" -eq 0 ] || fail "$s with server down exited $rc"
  [ "$s" = trails-session-start ] || expect_empty "$s with server down" "$out"
done

# Timing: ten off-trail hook runs against the live server.
payload=$(printf '{"session_id":"rt-codex","tool_input":{"file_path":"%s/src/a.go"}}' "$tmp/repo")
start=$(date +%s%N)
n=0
while [ "$n" -lt 10 ]; do
  printf '%s' "$payload" | TRAILS_CONTEXT_FORMAT=hook-json "$hooks/trails-off-trail.sh" >/dev/null
  n=$((n + 1))
done
mean_us=$(( ($(date +%s%N) - start) / 10000 ))
echo "trails-off-trail.sh mean wall time over 10 runs: $((mean_us / 1000)).$(printf '%03d' $((mean_us % 1000))) ms"
[ "$mean_us" -lt 50000 ] || fail "mean wall time over 50 ms"

echo "hooks ok"
