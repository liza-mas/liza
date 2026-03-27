#!/bin/bash
# PreToolUse hook: block non-Read tool calls until mandatory docs are read.
#
# Required reads:
#   - AGENT_TOOLS.md
#   - GUARDRAILS.md (or verified absent)
#   - One of: PAIRING_MODE.md, MULTI_AGENT_MODE.md, SUBAGENT_MODE.md
#
# No external dependencies (no jq, no sed -i). Portable across Linux and macOS.

input=$(cat)

# Extract JSON string values without jq — matches "key": "value" patterns.
json_val() { echo "$input" | grep -o "\"$1\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -1 | sed 's/.*:[[:space:]]*"//;s/"$//'; }

tool_name=$(json_val tool_name)
session_id=$(json_val session_id)

# Fallback: $PPID is the Claude Code process PID (POSIX — the parent that forked this shell).
# Stable for the session lifetime since all hook invocations share the same parent process.
STATE_DIR="/tmp/liza-init-gate-${session_id:-ppid-$PPID}"
if ! mkdir -p "$STATE_DIR" 2>/dev/null; then
  echo "enforce-init: cannot create state dir $STATE_DIR, failing open" >&2
  exit 0
fi

# Fast path: gate already cleared.
if [[ -f "$STATE_DIR/CLEARED" ]]; then
  exit 0
fi

# Initialize: auto-clear GUARDRAILS.md if absent from project root.
if [[ ! -f "$STATE_DIR/GUARDRAILS.done" ]] && \
   [[ ! -f "${CLAUDE_PROJECT_DIR:-.}/GUARDRAILS.md" ]]; then
  touch "$STATE_DIR/GUARDRAILS.done"
fi

# Bootstrap tool: allow ToolSearch to load Read schema.
if [[ "$tool_name" == "ToolSearch" ]]; then
  exit 0
fi

# Read calls always pass through; track mandatory doc reads.
if [[ "$tool_name" == "Read" ]]; then
  file_path=$(json_val file_path)
  base=$(basename "$file_path")

  case "$base" in
    AGENT_TOOLS.md)   touch "$STATE_DIR/AGENT_TOOLS.done" ;;
    PAIRING_MODE.md|MULTI_AGENT_MODE.md|SUBAGENT_MODE.md)
                      touch "$STATE_DIR/MODE.done" ;;
    GUARDRAILS.md)    touch "$STATE_DIR/GUARDRAILS.done" ;;
  esac

  # Check if all requirements met.
  if [[ -f "$STATE_DIR/AGENT_TOOLS.done" ]] && \
     [[ -f "$STATE_DIR/MODE.done" ]] && \
     [[ -f "$STATE_DIR/GUARDRAILS.done" ]]; then
    touch "$STATE_DIR/CLEARED"
  fi

  exit 0
fi

# Non-Read tool call: block if gate not cleared.
# Stdout is shown to the agent as the block reason; stderr is swallowed.
missing=""
[[ ! -f "$STATE_DIR/AGENT_TOOLS.done" ]] && missing="$missing
  - ~/.liza/AGENT_TOOLS.md"
[[ ! -f "$STATE_DIR/MODE.done" ]] && missing="$missing
  - One of: ~/.liza/PAIRING_MODE.md, ~/.liza/MULTI_AGENT_MODE.md, or ~/.liza/SUBAGENT_MODE.md"
[[ ! -f "$STATE_DIR/GUARDRAILS.done" ]] && missing="$missing
  - GUARDRAILS.md (project root, or confirm absent)"

cat <<EOF
BLOCKED — session initialization incomplete.

You must Read these files before using any other tool:
$missing

Use the Read tool on each path above, then retry your action.
EOF
exit 2
