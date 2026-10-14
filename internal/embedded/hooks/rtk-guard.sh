#!/bin/bash
# PreToolUse hook: block RTK anti-patterns in both multi-agent and pairing modes.
# Enforces AGENT_TOOLS.md rules:
#   - Do not read RTK tee files — the compressed summary is the authoritative output.
#   - Do not use rtk proxy.
# No external dependencies (no jq). Portable across Linux and macOS.

input=$(cat)

# Extract JSON string values without jq — matches "key": "value" patterns.
json_val() { echo "$input" | grep -o "\"$1\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -1 | sed 's/.*:[[:space:]]*"//;s/"$//'; }

command=$(json_val command)

blocked=""
reason=""

if echo "$command" | grep -qE '~?/?\.local/share/rtk/tee'; then
  blocked="read RTK tee file"
  reason="RTK compressed output is authoritative. Do not read tee files — diagnose from the summary. Do not try to work around RTK."
elif echo "$command" | grep -qE '\brtk\s+proxy\b' && ! echo "$command" | grep -qE '(^|\s)--collect-only(\s|$)'; then  # https://github.com/rtk-ai/rtk/pull/925
  blocked="rtk proxy"
  reason="RTK compressed output is authoritative. In case of error, fix your command. Do not try to work around RTK."
fi

if [[ -n "$blocked" ]]; then
  if [[ "${1:-}" == "copilot" ]]; then
    printf '{"permissionDecision":"deny","permissionDecisionReason":"%s"}\n' "$reason"
  else
    cat <<EOF
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"$reason"}}
EOF
  fi
fi
