#!/bin/bash
# Atomically mark a task READY_FOR_REVIEW with review_commit and history entry
# Usage: liza-submit-for-review.sh <task-id> <commit-sha>

set -euo pipefail

TASK_ID="${1:-}"
COMMIT_SHA="${2:-}"

if [ -z "$TASK_ID" ] || [ -z "$COMMIT_SHA" ]; then
    echo "Usage: $0 <task-id> <commit-sha>" >&2
    exit 1
fi

if [ -z "${LIZA_AGENT_ID:-}" ]; then
    echo "ERROR: LIZA_AGENT_ID is required" >&2
    exit 1
fi

SCRIPT_DIR=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
source "$SCRIPT_DIR/liza-common.sh"
PROJECT_ROOT=$(get_project_root)
STATE="$PROJECT_ROOT/.liza/state.yaml"
TIMESTAMP=$(iso_timestamp)

"$SCRIPT_DIR/liza-lock.sh" modify \
  yq -i "(.tasks[] | select(.id == \"$TASK_ID\")) |= (.status = \"READY_FOR_REVIEW\" | .review_commit = \"$COMMIT_SHA\" | .history = ((.history // []) + [{\"time\": \"$TIMESTAMP\", \"event\": \"submitted_for_review\", \"agent\": \"$LIZA_AGENT_ID\"}]))" "$STATE"
