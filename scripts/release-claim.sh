#!/bin/bash
# Manually release claims on a task (reviewer, coder, or both).
# Usage: release-claim.sh <task-id> [--role reviewer|coder|both] [--full] [--force] [--reason "text"]

set -euo pipefail

if [ $# -lt 1 ]; then
    echo "Usage: release-claim.sh <task-id> [--role reviewer|coder|both] [--full] [--force] [--reason \"text\"]" >&2
    exit 1
fi

TASK_ID="$1"
shift

ROLE="reviewer"
FORCE=false
REASON="manual release"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --role) ROLE="$2"; shift 2 ;;
        --full) ROLE="both"; shift ;;
        --force) FORCE=true; shift ;;
        --reason) REASON="$2"; shift 2 ;;
        *) echo "Unknown argument: $1" >&2; exit 1 ;;
    esac
done

case "$ROLE" in
    reviewer|coder|both) ;;
    *) echo "ERROR: --role must be reviewer, coder, or both." >&2; exit 1 ;;
esac

PROJECT_ROOT=$(git rev-parse --show-toplevel)
STATE="$PROJECT_ROOT/.liza/state.yaml"
STATE_LOCK="$STATE.lock"

if [ ! -f "$STATE" ]; then
    echo "ERROR: state.yaml not found at $STATE" >&2
    exit 1
fi

exists=$(yq -r ".tasks[] | select(.id == \"$TASK_ID\") | .id" "$STATE" 2>/dev/null || true)
if [ -z "$exists" ] || [ "$exists" = "null" ]; then
    echo "ERROR: Task $TASK_ID not found." >&2
    exit 1
fi

now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
agent="${LIZA_AGENT_ID:-human}"

reviewing_by=$(yq -r ".tasks[] | select(.id == \"$TASK_ID\") | .reviewing_by // \"\"" "$STATE")
review_lease=$(yq -r ".tasks[] | select(.id == \"$TASK_ID\") | .review_lease_expires // \"\"" "$STATE")

assigned_to=$(yq -r ".tasks[] | select(.id == \"$TASK_ID\") | .assigned_to // \"\"" "$STATE")
lease=$(yq -r ".tasks[] | select(.id == \"$TASK_ID\") | .lease_expires // \"\"" "$STATE")

did_any=false

if [ "$ROLE" = "reviewer" ] || [ "$ROLE" = "both" ]; then
    if [ -z "$reviewing_by" ] && [ -z "$review_lease" ]; then
        echo "WARN: Task $TASK_ID has no review claim to release."
    else
        if [ -n "$reviewing_by" ] && [ -z "$review_lease" ] && [ "$FORCE" = false ]; then
            echo "ERROR: review_lease_expires missing for $TASK_ID. Use --force to clear." >&2
            exit 1
        fi
        if [ -n "$review_lease" ] && [ "$FORCE" = false ]; then
            if [ "$(date -d "$review_lease" +%s 2>/dev/null || echo 0)" -gt "$(date +%s)" ]; then
                echo "ERROR: Review lease still valid until $review_lease. Use --force to clear." >&2
                exit 1
            fi
        fi

        flock -x "$STATE_LOCK" -c "
            yq -i '
                (.tasks[] | select(.id == \"$TASK_ID\")).reviewing_by = null |
                (.tasks[] | select(.id == \"$TASK_ID\")).review_lease_expires = null |
                (.tasks[] | select(.id == \"$TASK_ID\")).history =
                    ((.tasks[] | select(.id == \"$TASK_ID\")).history // []) + [
                        {\"time\": \"$now\", \"event\": \"review_claim_released\", \"agent\": \"$agent\", \"reason\": \"$REASON\"}
                    ]
            ' \"$STATE\"
        "
        did_any=true
        echo "Released review claim for $TASK_ID."
    fi
fi

if [ "$ROLE" = "coder" ] || [ "$ROLE" = "both" ]; then
    if [ -z "$assigned_to" ] && [ -z "$lease" ]; then
        echo "WARN: Task $TASK_ID has no coder claim to release."
    else
        if [ -n "$assigned_to" ] && [ -z "$lease" ] && [ "$FORCE" = false ]; then
            echo "ERROR: lease_expires missing for $TASK_ID. Use --force to clear." >&2
            exit 1
        fi
        if [ -n "$lease" ] && [ "$FORCE" = false ]; then
            if [ "$(date -d "$lease" +%s 2>/dev/null || echo 0)" -gt "$(date +%s)" ]; then
                echo "ERROR: Coder lease still valid until $lease. Use --force to clear." >&2
                exit 1
            fi
        fi

        status=$(yq -r ".tasks[] | select(.id == \"$TASK_ID\") | .status" "$STATE")
        new_status="$status"
        if [ "$status" = "CLAIMED" ]; then
            new_status="UNCLAIMED"
        fi

        flock -x "$STATE_LOCK" -c "
            yq -i '
                (.tasks[] | select(.id == \"$TASK_ID\")).assigned_to = null |
                (.tasks[] | select(.id == \"$TASK_ID\")).lease_expires = null |
                (.tasks[] | select(.id == \"$TASK_ID\")).status = \"$new_status\" |
                (.tasks[] | select(.id == \"$TASK_ID\")).history =
                    ((.tasks[] | select(.id == \"$TASK_ID\")).history // []) + [
                        {\"time\": \"$now\", \"event\": \"coder_claim_released\", \"agent\": \"$agent\", \"reason\": \"$REASON\"}
                    ]
            ' \"$STATE\"
        "
        did_any=true
        echo "Released coder claim for $TASK_ID."
    fi
fi

if [ "$did_any" = false ]; then
    echo "ERROR: No claims released for $TASK_ID." >&2
    exit 1
fi
