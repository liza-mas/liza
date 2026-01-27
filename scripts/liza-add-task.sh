#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  liza-add-task.sh --id TASK_ID --desc DESCRIPTION --spec SPEC_REF \
    --done DONE_WHEN --scope SCOPE [--priority N] [--depends "task-a,task-b"] \
    [--state PATH] [--log PATH]

Defaults:
  --state .liza/state.yaml
  --log   .liza/log.yaml
  --priority 1

Notes:
  - --depends is a comma-separated list of task IDs (optional).
  - Updates sprint.scope.planned, goal.alignment_history, and appends a log entry.
EOF
}

STATE=""
LOG=""
PRIORITY="1"
DEPENDS=""

TASK_ID=""
DESC=""
SPEC_REF=""
DONE_WHEN=""
SCOPE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --state) STATE="$2"; shift 2 ;;
    --log) LOG="$2"; shift 2 ;;
    --id) TASK_ID="$2"; shift 2 ;;
    --desc) DESC="$2"; shift 2 ;;
    --spec) SPEC_REF="$2"; shift 2 ;;
    --done) DONE_WHEN="$2"; shift 2 ;;
    --scope) SCOPE="$2"; shift 2 ;;
    --priority) PRIORITY="$2"; shift 2 ;;
    --depends) DEPENDS="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown arg: $1" >&2; usage; exit 1 ;;
  esac
done

# Resolve defaults from git root only if neither is explicitly provided
if [[ -n "$STATE" && -z "$LOG" ]] || [[ -z "$STATE" && -n "$LOG" ]]; then
  echo "Error: provide both --state and --log together." >&2
  exit 1
fi
if [[ -z "$STATE" && -z "$LOG" ]]; then
  PROJECT_ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || {
    echo "Error: not in a git repo; provide --state and --log explicitly." >&2
    exit 1
  }
  STATE="$PROJECT_ROOT/.liza/state.yaml"
  LOG="$PROJECT_ROOT/.liza/log.yaml"
fi

if [[ -z "$TASK_ID" || -z "$DESC" || -z "$SPEC_REF" || -z "$DONE_WHEN" || -z "$SCOPE" ]]; then
  echo "Missing required args." >&2
  usage
  exit 1
fi

# Expand literal \n sequences to actual newlines (agents pass multi-line
# values as single-line strings with escaped newlines)
DONE_WHEN="${DONE_WHEN//\\n/$'\n'}"
SCOPE="${SCOPE//\\n/$'\n'}"

NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

DEP_JSON="[]"
if [[ -n "$DEPENDS" ]]; then
  DEP_JSON=$(printf '%s\n' "$DEPENDS" | tr ',' '\n' | yq -n '[inputs | trim | select(length > 0)]' -o=json)
fi

export TASK_ID DESC SPEC_REF DONE_WHEN SCOPE PRIORITY DEP_JSON NOW LOG
export LIZA_AGENT_ID="${LIZA_AGENT_ID:-planner-1}"

if ! [[ "$PRIORITY" =~ ^[0-9]+$ ]]; then
  echo "Error: --priority must be numeric, got '$PRIORITY'" >&2
  exit 1
fi

# Check for duplicate task ID
if yq -e ".tasks[] | select(.id == \"$TASK_ID\")" "$STATE" &>/dev/null; then
  echo "Error: task '$TASK_ID' already exists in $STATE" >&2
  exit 1
fi

"$SCRIPT_DIR/liza-lock.sh" modify \
  env TASK_ID="$TASK_ID" DESC="$DESC" SPEC_REF="$SPEC_REF" DONE_WHEN="$DONE_WHEN" \
      SCOPE="$SCOPE" PRIORITY="$PRIORITY" DEP_JSON="$DEP_JSON" NOW="$NOW" \
      STATE="$STATE" \
  yq -i '
    .tasks += [{
      "id": strenv(TASK_ID),
      "description": strenv(DESC),
      "status": "UNCLAIMED",
      "priority": (strenv(PRIORITY) | tonumber),
      "created": strenv(NOW),
      "spec_ref": strenv(SPEC_REF),
      "done_when": strenv(DONE_WHEN),
      "scope": strenv(SCOPE),
      "depends_on": (strenv(DEP_JSON) | fromjson)
    }]
    | .sprint.scope.planned |= (. + [strenv(TASK_ID)] | unique)
    | .goal.alignment_history += [{
        "timestamp": strenv(NOW),
        "event": "planning",
        "summary": "Added task " + strenv(TASK_ID) + ": " + strenv(DESC)
      }]
  ' "$STATE"

env DETAIL="$DESC" LIZA_AGENT_ID="$LIZA_AGENT_ID" NOW="$NOW" TASK_ID="$TASK_ID" \
  yq -i '. += [{
    "timestamp": strenv(NOW),
    "agent": strenv(LIZA_AGENT_ID),
    "action": "task_added",
    "task": strenv(TASK_ID),
    "detail": strenv(DETAIL)
  }]' "$LOG"

"$SCRIPT_DIR/liza-validate.sh" "$STATE"
