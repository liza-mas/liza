#!/bin/bash
# PreToolUse hook: block destructive git operations.
# Compensates for Bash(git:*) permission — forces manual execution for dangerous commands.
# No external dependencies (no jq). Portable across Linux and macOS.

# Only active for __BRAND_NAME_TITLE__ agents (pairing sessions have human oversight).
brand_agent_id_var="__BRAND_ENV_PREFIX__""_AGENT_ID"
agent_id_value="${!brand_agent_id_var:-${LIZA_AGENT_ID:-}}"
[[ -z "$agent_id_value" ]] && exit 0

input=$(cat)

# Extract JSON string values without jq — matches "key": "value" patterns.
json_val() { echo "$input" | grep -o "\"$1\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -1 | sed 's/.*:[[:space:]]*"//;s/"$//'; }

command=$(json_val command)

# Match subcommand + destructive flag independently (flags can appear anywhere).
blocked=""
if echo "$command" | grep -qE '\bgit\s'; then
  echo "$command" | grep -qE '\bpush\b'  && echo "$command" | grep -qE '\s(--force\b|-f\b)' && blocked="force push"
  echo "$command" | grep -qE '\breset\b' && echo "$command" | grep -qE '\s--hard\b'                && blocked="hard reset"
  echo "$command" | grep -qE '\bclean\b' && echo "$command" | grep -qE '\s-[a-z]*f'                && blocked="clean with -f"
fi

if [[ -n "$blocked" ]]; then
  cat <<EOF
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"Destructive git operation ($blocked) requires manual execution"}}
EOF
fi
