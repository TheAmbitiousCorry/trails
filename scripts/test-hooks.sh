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
# The hooks read these; a developer shell inside Claude Code sets them.
unset CLAUDE_PROJECT_DIR TRAILS_PORT TRAILS_URL TRAILS_ADDR

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
# The hooks find the project from the working directory when a payload has no cwd; run them inside the opted-in repo.
cd "$tmp/repo"
rid=$(tr -d '\n' < .trail/id)

# tool NAME ARGS_JSON calls an MCP tool and prints its structured result.
tool() {
  jq -cn --arg n "$1" --argjson a "$2" '{jsonrpc: "2.0", id: 1, method: "tools/call", params: {name: $n, arguments: $a}}' |
    curl -sf -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' --data-binary @- "$url/mcp" |
    jq -c 'if .result.isError then error(.result.content[0].text) else .result.structuredContent end'
}

for rt in claude codex gemini cursor; do
  tool session_start "{\"task\":\"hook test\",\"task_type\":\"test\",\"runtime_session_id\":\"rt-$rt\"}" > "$tmp/start-$rt.json"
done
csid=$(jq -r .session_id "$tmp/start-claude.json")
sid=$(tool session_start '{"task":"anchor","task_type":"test"}' | jq -r .session_id)
tool write "{\"session_id\":\"$sid\",\"parent_sign_id\":\"root\",\"kind\":\"decision\",\"decided_by\":\"test\",\"decided_at\":\"2026-10-27\",\"title\":\"Package a\",\"read_when\":\"you touch package a\",\"body\":\"Package a is tiny.\",\"anchors\":[{\"type\":\"file\",\"path\":\"src/a.go\"}]}" >/dev/null
entry=$(tool look "{\"session_id\":\"$sid\",\"sign_id\":\"root\"}" | jq -r '[.. | .id? // empty | select(. != "trail")][0]')

# Claude Code: stdin session_id and tool_input.file_path, hook-json context, decision block.
out=$(printf '{"session_id":"rt-claude","hook_event_name":"SessionStart"}' | TRAILS_CONTEXT_FORMAT=hook-json "$hooks/trails-session-start.sh")
expect "claude session-start" '.hookSpecificOutput.hookEventName == "SessionStart" and .hookSpecificOutput.additionalContext == "Trails project: '"$rid"'\nTrails runtime_session_id: rt-claude"' "$out"
out=$(printf '{"session_id":"rt-claude","tool_input":{"file_path":"%s/src/a.go"}}' "$tmp/repo" | TRAILS_CONTEXT_FORMAT=hook-json "$hooks/trails-off-trail.sh")
expect "claude off-trail" ".hookSpecificOutput.hookEventName == \"PostToolUse\" and .hookSpecificOutput.additionalContext == \"Trails has an entry for this file: $entry, you touch package a\"" "$out"
out=$(printf '{"session_id":"rt-claude","tool_input":{"file_path":"%s/other.txt"}}' "$tmp/repo" | TRAILS_CONTEXT_FORMAT=hook-json "$hooks/trails-off-trail.sh")
expect_empty "claude off-trail unanchored" "$out"
# Claude Code before-read gate: an unread anchoring entry denies the read with the exact reason, an unanchored path passes, and reading the entry lets the same read through.
want=$(jq -cn --arg r "Trails has an entry for this file: $entry, you touch package a. Read it with read($entry) first, then open the file." '{hookSpecificOutput: {hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: $r}}')
out=$(printf '{"session_id":"rt-claude","hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"%s/src/a.go"}}' "$tmp/repo" | "$hooks/trails-pre-read.sh")
[ "$out" = "$want" ] || fail "claude pre-read deny: got '$out', want '$want'"
out=$(printf '{"session_id":"rt-claude","tool_name":"Grep","tool_input":{"pattern":"x","path":"%s/src/a.go"}}' "$tmp/repo" | "$hooks/trails-pre-read.sh")
[ "$out" = "$want" ] || fail "claude pre-read deny on Grep path: got '$out'"
out=$(printf '{"session_id":"rt-claude","tool_input":{"file_path":"%s/other.txt"}}' "$tmp/repo" | "$hooks/trails-pre-read.sh")
expect_empty "claude pre-read unanchored" "$out"
out=$(printf '{"session_id":"rt-claude","tool_name":"Grep","tool_input":{"pattern":"x"}}' | "$hooks/trails-pre-read.sh")
expect_empty "claude pre-read without a path" "$out"
# No session has this runtime id yet: an anchored path is unstarted and denied with the session_start pointer; an unanchored path passes.
want=$(jq -cn --arg r "Trails: this repo has a map and an entry for this file ($entry, you touch package a). Start with session_start(task, project: $rid) and read($entry) before opening it." '{hookSpecificOutput: {hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: $r}}')
out=$(printf '{"session_id":"nobody","tool_input":{"file_path":"%s/src/a.go"}}' "$tmp/repo" | "$hooks/trails-pre-read.sh")
[ "$out" = "$want" ] || fail "pre-read unstarted before any session: got '$out', want '$want'"
out=$(printf '{"session_id":"nobody","tool_input":{"file_path":"%s/other.txt"}}' "$tmp/repo" | "$hooks/trails-pre-read.sh")
expect_empty "pre-read unknown session, unanchored path" "$out"
tool read "{\"session_id\":\"$csid\",\"entry_id\":\"$entry\",\"depth\":\"summary\"}" >/dev/null
out=$(printf '{"session_id":"rt-claude","tool_input":{"file_path":"%s/src/a.go"}}' "$tmp/repo" | "$hooks/trails-pre-read.sh")
expect_empty "claude pre-read after the entry is read" "$out"

# Two entries anchor one file: the deny lists both by id in id order, and reading either one lets the read through.
echo 'package multi' > "$tmp/repo/src/multi.go"
m1=$(tool write "{\"session_id\":\"$sid\",\"parent_sign_id\":\"root\",\"kind\":\"decision\",\"decided_by\":\"test\",\"decided_at\":\"2026-10-27\",\"title\":\"Multi one\",\"read_when\":\"you touch multi one\",\"body\":\"Rounding is set in one function.\",\"anchors\":[{\"type\":\"file\",\"path\":\"src/multi.go\"}]}" | jq -r .entry_id)
m2=$(tool write "{\"session_id\":\"$sid\",\"parent_sign_id\":\"root\",\"kind\":\"decision\",\"decided_by\":\"test\",\"decided_at\":\"2026-10-27\",\"title\":\"Multi two\",\"read_when\":\"you touch multi two\",\"body\":\"Tax rates live in a separate table.\",\"anchors\":[{\"type\":\"file\",\"path\":\"src/multi.go\"}]}" | jq -r .entry_id)
pairs=$(printf '%s\n' "$m1 (you touch multi one)" "$m2 (you touch multi two)" | LC_ALL=C sort | awk 'BEGIN { ORS = "" } NR > 1 { printf "; " } { printf "%s", $0 }')
want=$(jq -cn --arg r "Trails has 2 entries for this file; read the one whose read_when matches your task, then open the file: $pairs" '{hookSpecificOutput: {hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: $r}}')
out=$(printf '{"session_id":"rt-claude","tool_input":{"file_path":"%s/src/multi.go"}}' "$tmp/repo" | "$hooks/trails-pre-read.sh")
[ "$out" = "$want" ] || fail "claude pre-read lists every entry: got '$out', want '$want'"
tool read "{\"session_id\":\"$csid\",\"entry_id\":\"$m2\",\"depth\":\"summary\"}" >/dev/null
out=$(printf '{"session_id":"rt-claude","tool_input":{"file_path":"%s/src/multi.go"}}' "$tmp/repo" | "$hooks/trails-pre-read.sh")
expect_empty "claude pre-read after one of the entries is read" "$out"

out=$(printf '{"session_id":"rt-claude","stop_hook_active":false}' | TRAILS_BLOCK_FORMAT=block "$hooks/trails-stop.sh")
expect "claude stop" '.decision == "block" and (.reason | startswith("Trails session still open with 0 gaps: call session_end"))' "$out"

# Codex: same stdin shape, session start and stop only.
out=$(printf '{"session_id":"rt-codex"}' | TRAILS_CONTEXT_FORMAT=hook-json "$hooks/trails-session-start.sh")
expect "codex session-start" '.hookSpecificOutput.additionalContext == "Trails project: '"$rid"'\nTrails runtime_session_id: rt-codex"' "$out"
out=$(printf '{"session_id":"rt-codex"}' | "$hooks/trails-stop.sh")
expect "codex stop" '.decision == "block"' "$out"

# Gemini CLI: id from GEMINI_SESSION_ID, AfterTool event name, decision deny.
out=$(printf '{"session_id":"ignored"}' | GEMINI_SESSION_ID=rt-gemini TRAILS_SESSION_ENV=GEMINI_SESSION_ID TRAILS_CONTEXT_FORMAT=hook-json "$hooks/trails-session-start.sh")
expect "gemini session-start" '.hookSpecificOutput.additionalContext == "Trails project: '"$rid"'\nTrails runtime_session_id: rt-gemini"' "$out"
out=$(printf '{"tool_input":{"file_path":"src/a.go"}}' | GEMINI_SESSION_ID=rt-gemini TRAILS_SESSION_ENV=GEMINI_SESSION_ID TRAILS_CONTEXT_FORMAT=hook-json TRAILS_EVENT=AfterTool "$hooks/trails-off-trail.sh")
expect "gemini off-trail" ".hookSpecificOutput.hookEventName == \"AfterTool\" and (.hookSpecificOutput.additionalContext | startswith(\"Trails has an entry for this file: $entry\"))" "$out"
out=$(printf '{}' | GEMINI_SESSION_ID=rt-gemini TRAILS_SESSION_ENV=GEMINI_SESSION_ID TRAILS_BLOCK_FORMAT=deny "$hooks/trails-stop.sh")
expect "gemini stop" '.decision == "deny" and (.reason | startswith("Trails session still open"))' "$out"

# Cursor: id from conversation_id, additional_context, followup_message.
out=$(printf '{"conversation_id":"rt-cursor"}' | TRAILS_SESSION_FIELD=conversation_id TRAILS_CONTEXT_FORMAT=cursor "$hooks/trails-session-start.sh")
expect "cursor session-start" '.additional_context == "Trails project: '"$rid"'\nTrails runtime_session_id: rt-cursor"' "$out"
out=$(printf '{"conversation_id":"rt-cursor","tool_input":{"file_path":"src/a.go"}}' | TRAILS_SESSION_FIELD=conversation_id TRAILS_CONTEXT_FORMAT=cursor "$hooks/trails-off-trail.sh")
expect "cursor off-trail" ".additional_context | startswith(\"Trails has an entry for this file: $entry\")" "$out"
out=$(printf '{"conversation_id":"rt-cursor"}' | TRAILS_SESSION_FIELD=conversation_id TRAILS_BLOCK_FORMAT=cursor "$hooks/trails-stop.sh")
expect "cursor stop" '.followup_message | startswith("Trails session still open")' "$out"

# Plain text is the default injection form.
out=$(printf '{"session_id":"rt-claude"}' | "$hooks/trails-session-start.sh")
[ "$out" = "Trails project: $rid
Trails runtime_session_id: rt-claude" ] || fail "plain session-start: got '$out'"

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

# Autostart: a repo with .trail/id and no answering server gets TRAILS_BIN started with no repo and is then registered over HTTP; a repo without the id contacts and starts nothing.
fakehome=$tmp/fakehome
mkdir -p "$fakehome" "$tmp/auto/.trail" "$tmp/noid" "$tmp/fake"
echo 'autotest' > "$tmp/auto/.trail/id"
cat > "$tmp/fake/trails" <<'FAKE'
#!/bin/sh
# Stand-in for the trails binary: records its arguments and pid, then becomes the real server.
echo "$*" > "$FAKE_DIR/args"
echo $$ > "$FAKE_DIR/pid"
exec "$REAL_TRAILS" "$@"
FAKE
chmod +x "$tmp/fake/trails"
export FAKE_DIR=$tmp/fake REAL_TRAILS=$tmp/trails
aport=$(( port + 1 ))
[ "$aport" -lt 65535 ] || aport=$(( port - 1 ))
aurl=http://127.0.0.1:$aport
auto_hook() { # CWD_DIR -> runs the session-start hook with cwd given in the payload
  printf '{"session_id":"rt-auto","cwd":"%s"}' "$1" | HOME=$fakehome TRAILS_URL=$aurl TRAILS_BIN=$tmp/fake/trails "$hooks/trails-session-start.sh"
}
out=$(auto_hook "$tmp/noid")
[ "$out" = "Trails runtime_session_id: rt-auto" ] || fail "no-id session-start: got '$out'"
[ ! -e "$tmp/fake/args" ] || fail "no-id repo started the server"
out=$(auto_hook "$tmp/auto")
pid2=$(cat "$tmp/fake/pid" 2>/dev/null || true)
[ "$out" = "Trails project: autotest
Trails runtime_session_id: rt-auto" ] || fail "autostart session-start: got '$out'"
[ "$(cat "$tmp/fake/args")" = "serve --addr 127.0.0.1:$aport" ] || fail "autostart args: got '$(cat "$tmp/fake/args")'"
[ -f "$fakehome/.trail/server.log" ] || fail "autostart wrote no log under ~/.trail"
[ "$(curl -sf --max-time 1 "$aurl/api/projects" | jq -c '[.projects[].id]')" = '["autotest"]' ] || fail "autostarted server does not hold exactly the opted-in repo"
out=$(printf '{"session_id":"rt-auto","cwd":"%s"}' "$tmp/auto" | HOME=$fakehome TRAILS_URL=$aurl TRAILS_BIN=/nonexistent/trails "$hooks/trails-session-start.sh")
[ "$out" = "Trails project: autotest
Trails runtime_session_id: rt-auto" ] || fail "server up, binary missing: got '$out'"
[ -z "${pid2:-}" ] || { kill "$pid2" 2>/dev/null || true; pid2=; }
i=0
while curl -sf --max-time 1 -o /dev/null "$aurl/api/projects"; do
  i=$((i + 1)); [ "$i" -lt 50 ] || fail "autostarted server did not stop"
  sleep 0.1
done
out=$(printf '{"session_id":"rt-auto","cwd":"%s"}' "$tmp/auto" | HOME=$fakehome TRAILS_URL=$aurl TRAILS_BIN=/nonexistent/trails "$hooks/trails-session-start.sh")
[ "$out" = "Trails: no server and no binary; see adapters/README.md
Trails project: autotest
Trails runtime_session_id: rt-auto" ] || fail "no server, no binary: got '$out'"

# The address comes from TRAILS_URL, else TRAILS_ADDR, else 127.0.0.1:7777; nothing is read per repo.
got=$(env -u TRAILS_URL TRAILS_ADDR=127.0.0.1:9123 sh -c '. "$1/trails-common.sh"; trails_init "{\"cwd\":\"$2\"}"; printf %s "$TRAILS_URL"' sh "$hooks" "$tmp/auto")
[ "$got" = "http://127.0.0.1:9123" ] || fail "common TRAILS_URL from TRAILS_ADDR: got '$got'"
got=$(env -u TRAILS_URL -u TRAILS_ADDR sh -c '. "$1/trails-common.sh"; trails_init "{\"cwd\":\"$2\"}"; printf %s "$TRAILS_URL"' sh "$hooks" "$tmp/auto")
[ "$got" = "http://127.0.0.1:7777" ] || fail "common TRAILS_URL default: got '$got'"
got=$(env -u TRAILS_URL -u TRAILS_ADDR TRAILS_PORT=9124 sh -c '. "$1/trails-common.sh"; trails_init "{\"cwd\":\"$2\"}"; printf %s "$TRAILS_URL"' sh "$hooks" "$tmp/auto")
[ "$got" = "http://127.0.0.1:9124" ] || fail "common TRAILS_URL from TRAILS_PORT alone: got '$got'"
# The project id is found by walking up from a subdirectory.
mkdir -p "$tmp/auto/deep/er"
got=$(sh -c '. "$1/trails-common.sh"; trails_init "{\"cwd\":\"$2\"}"; printf "%s %s" "$TRAILS_PROJECT" "$TRAILS_ROOT"' sh "$hooks" "$tmp/auto/deep/er")
[ "$got" = "autotest $tmp/auto" ] || fail "project id from a subdirectory: got '$got'"

# Mismatch line: under Claude Code, when the server the hook reached is on another port than the MCP entry's TRAILS_PORT (7777 when unset).
mm="Trails: the server answered on port $port but the MCP entry points at port 7777; add \"env\": {\"TRAILS_PORT\": \"$port\"} to ~/.claude/settings.json"
mm_hook() { # EXTRA_ENV... -> session-start against the main server, repo from CLAUDE_PROJECT_DIR
  printf '{"session_id":"rt-mm"}' | env CLAUDE_PROJECT_DIR="$tmp/repo" TRAILS_BIN=/nonexistent/trails TRAILS_URL="$url" "$@" "$hooks/trails-session-start.sh"
}
out=$(mm_hook)
[ "$out" = "$mm
Trails project: $rid
Trails runtime_session_id: rt-mm" ] || fail "mismatch line, TRAILS_PORT unset: got '$out'"
out=$(mm_hook TRAILS_PORT=$port)
case "$out" in *"MCP entry points at"*) fail "mismatch line printed though TRAILS_PORT matches: '$out'" ;; esac
out=$(mm_hook TRAILS_PORT=1234)
case "$out" in *"MCP entry points at port 1234; add"*) ;; *) fail "mismatch line, TRAILS_PORT differs: got '$out'" ;; esac
out=$(printf '{"session_id":"rt-mm"}' | env -u CLAUDE_PROJECT_DIR "$hooks/trails-session-start.sh")
case "$out" in *"MCP entry points at"*) fail "mismatch line printed outside Claude Code: '$out'" ;; esac
out=$(mm_hook TRAILS_URL=http://127.0.0.1:1)
case "$out" in *"MCP entry points at"*) fail "mismatch line printed for a server the hook never reached: '$out'" ;; esac

# Two repos, one server: each registers over HTTP and each hook call lands in its own project.
mkdir -p "$tmp/repo2/.trail" "$tmp/repo2/src"
echo 'package b' > "$tmp/repo2/src/b.go"
echo 'second' > "$tmp/repo2/.trail/id"
out=$(printf '{"session_id":"rt-two","cwd":"%s"}' "$tmp/repo2" | "$hooks/trails-session-start.sh")
[ "$out" = "Trails project: second
Trails runtime_session_id: rt-two" ] || fail "second repo session-start: got '$out'"
[ "$(curl -sf "$url/api/projects" | jq -c '[.projects[].id] | sort')" = "$(jq -cn --arg a "$rid" '[$a, "second"] | sort')" ] || fail "the server does not hold both repos"
sid2=$(tool session_start '{"task":"second repo","task_type":"test","project":"second","runtime_session_id":"rt-two"}' | jq -r .session_id)
tool write "{\"session_id\":\"$sid2\",\"parent_sign_id\":\"root\",\"kind\":\"decision\",\"decided_by\":\"test\",\"decided_at\":\"2026-10-27\",\"title\":\"Package b\",\"read_when\":\"you touch package b\",\"body\":\"Package b is tiny.\",\"anchors\":[{\"type\":\"file\",\"path\":\"src/b.go\"}]}" >/dev/null
entry2=$(tool look "{\"session_id\":\"$sid2\",\"sign_id\":\"root\"}" | jq -r '[.. | .id? // empty | select(. != "trail")][0]')
two() { printf '{"session_id":"rt-two","cwd":"%s","tool_input":{"file_path":"src/b.go"}}' "$1"; }
out=$(two "$tmp/repo2" | "$hooks/trails-off-trail.sh")
[ "$out" = "Trails has an entry for this file: $entry2, you touch package b" ] || fail "second repo off-trail: got '$out'"
out=$(two "$tmp/repo" | "$hooks/trails-off-trail.sh")
expect_empty "first repo's hook for the second repo's session" "$out"
out=$(two "$tmp/repo2" | "$hooks/trails-edited.sh")
[ "$out" = "Trails has 1 entries anchored to this file; update or confirm each before ending the session: $entry2 (you touch package b)" ] || fail "second repo edited: got '$out'"
out=$(printf '{"session_id":"rt-two","cwd":"%s"}' "$tmp/repo" | "$hooks/trails-stop.sh")
expect_empty "first repo's stop hook for the second repo's session" "$out"
out=$(printf '{"session_id":"rt-two","cwd":"%s"}' "$tmp/repo2" | TRAILS_BLOCK_FORMAT=block "$hooks/trails-stop.sh")
expect "second repo stop" ".decision == \"block\" and (.reason | contains(\"$entry2 (src/b.go)\"))" "$out"

# No project in the body: a directory without .trail/id still finds the session by its runtime id.
out=$(two "$tmp/noid" | "$hooks/trails-off-trail.sh")
[ "$out" = "Trails has an entry for this file: $entry2, you touch package b" ] || fail "off-trail without a project: got '$out'"

# A repo with no .trail/id is refused over HTTP and left untouched; the CLI is what creates the id.
code=$(curl -s -o "$tmp/refusal.json" -w '%{http_code}' -H 'Content-Type: application/json' -d "{\"repo\":\"$tmp/noid\"}" "$url/api/projects")
[ "$code" = 422 ] && [ "$(jq -r .error "$tmp/refusal.json")" = not_opted_in ] || fail "registering a repo without .trail/id: $code $(cat "$tmp/refusal.json")"
[ -z "$(ls -A "$tmp/noid")" ] || fail "a refused registration touched the repo: $(ls -A "$tmp/noid")"
mkdir -p "$tmp/repo3"
"$tmp/trails" register --repo "$tmp/repo3" --addr "127.0.0.1:$port" >/dev/null
id3=$(tr -d '\n' < "$tmp/repo3/.trail/id")
curl -sf "$url/api/projects" | jq -e --arg i "$id3" '[.projects[].id] | index($i) != null' >/dev/null || fail "trails register did not register the repo"

# An unknown session and an unreachable server both pass through silently.
out=$(printf '{"session_id":"nobody"}' | "$hooks/trails-stop.sh")
expect_empty "unknown session stop" "$out"
for s in trails-session-start trails-pre-read trails-off-trail trails-edited trails-stop; do
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
