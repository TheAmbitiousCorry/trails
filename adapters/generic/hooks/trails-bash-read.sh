#!/bin/sh
# After-shell hook: a shell read (cat, sed, head, rg, grep) bypasses the read gate, so each repo file named in the command is recorded as off-trail. Nothing is denied.
. "$(dirname "$0")/trails-common.sh"
input=$(cat)
trails_init "$input"
[ -n "$TRAILS_ROOT" ] || exit 0
id=$(trails_session_id "$input")
[ -n "$id" ] || exit 0
cmd=$(printf '%s' "$input" | field_of tool_input.command)
[ -n "$cmd" ] || exit 0
seen=
count=0
lines=
tokens=$(printf '%s' "$cmd" | tr ' \t|&;<>()"`'"'"'=,' '\n\n\n\n\n\n\n\n\n\n\n\n\n\n')
while IFS= read -r tok; do
  [ "$count" -lt 5 ] || break
  [ -n "$tok" ] || continue
  case "$tok" in
    "$TRAILS_ROOT"/*) abs=$tok ;;
    /*) continue ;;
    ./*) abs=$TRAILS_ROOT/${tok#./} ;;
    *) abs=$TRAILS_ROOT/$tok ;;
  esac
  case "/$abs/" in */../*) continue ;; esac
  [ -f "$abs" ] || continue
  case "
$seen
" in *"
$abs
"*) continue ;; esac
  seen="$seen
$abs"
  count=$((count + 1))
  reply=$(jq -cn --arg s "$id" --arg p "$abs" '{runtime_session_id: $s, path: $p}' | post_hook off-trail) || continue
  line=$(printf '%s' "$reply" | jq -r 'select(.entry_id) | "Trails has an entry for this file: \(.entry_id), \(.read_when)"' 2>/dev/null)
  [ -z "$line" ] || lines=${lines:+$lines
}$line
done <<TOKENS
$tokens
TOKENS
[ -n "$lines" ] || exit 0
emit_context PostToolUse "$lines"
