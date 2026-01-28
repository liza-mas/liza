#!/bin/bash
# Atomically submit a review verdict and update review fields
# Usage: liza-submit-verdict.sh <task-id> <APPROVED|REJECTED> [rejection_reason]

set -euo pipefail

TASK_ID="${1:-}"
VERDICT="${2:-}"
REJECTION_REASON="${3:-}"

if [ -z "$TASK_ID" ] || [ -z "$VERDICT" ]; then
    echo "Usage: $0 <task-id> <APPROVED|REJECTED> [rejection_reason]" >&2
    exit 1
fi

if [ -z "${LIZA_AGENT_ID:-}" ]; then
    echo "ERROR: LIZA_AGENT_ID is required" >&2
    exit 1
fi

case "$VERDICT" in
    APPROVED|REJECTED) ;;
    *)
        echo "ERROR: verdict must be APPROVED or REJECTED" >&2
        exit 1
        ;;
 esac

if [ "$VERDICT" = "REJECTED" ] && [ -z "$REJECTION_REASON" ]; then
    echo "ERROR: rejection_reason is required for REJECTED" >&2
    exit 1
fi

SCRIPT_DIR=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
source "$SCRIPT_DIR/liza-common.sh"
PROJECT_ROOT=$(get_project_root)
STATE="$PROJECT_ROOT/.liza/state.yaml"
TIMESTAMP=$(iso_timestamp)

if [ "$VERDICT" = "APPROVED" ]; then
    verdict_patch=".status = \"APPROVED\" | .approved_by = \"$LIZA_AGENT_ID\" | .rejection_reason = null"
    event_name="approved"
    # Approved: simple history entry
    history_entry="{\"time\": \"$TIMESTAMP\", \"event\": \"$event_name\", \"agent\": \"$LIZA_AGENT_ID\"}"
else
    verdict_patch=".status = \"REJECTED\" | .rejection_reason = strenv(REJECTION_REASON)"
    event_name="rejected"
    # Rejected: include reason in history for oscillation tracking
    history_entry="{\"time\": \"$TIMESTAMP\", \"event\": \"$event_name\", \"agent\": \"$LIZA_AGENT_ID\", \"reason\": strenv(REJECTION_REASON)}"
fi

REJECTION_REASON="$REJECTION_REASON" "$SCRIPT_DIR/liza-lock.sh" modify \
  yq -i "(.tasks[] | select(.id == \"$TASK_ID\")) |= ($verdict_patch | .reviewing_by = null | .review_lease_expires = null | .history = ((.history // []) + [$history_entry]))" "$STATE"
