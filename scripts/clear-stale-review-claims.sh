#!/bin/bash
# Clear stale review claims on READY_FOR_REVIEW tasks
# Usage: clear-stale-review-claims.sh [project_root]
#
# When a Code Reviewer crashes mid-review, reviewing_by and review_lease_expires
# remain set. This script clears expired claims so other reviewers can claim the task.
#
# Typically called by:
# - Code Reviewer supervisor on startup
# - Periodically by cron or monitoring
# - liza-watch.sh could call this (though watch shouldn't mutate state by default)

set -euo pipefail

# --- Path Setup ---
PROJECT_ROOT="${1:-$(git rev-parse --show-toplevel)}"
readonly PROJECT_ROOT
readonly STATE="$PROJECT_ROOT/.liza/state.yaml"
readonly STATE_LOCK="$STATE.lock"
readonly LOG="$PROJECT_ROOT/.liza/log.yaml"

# --- Validation ---

if [ ! -f "$STATE" ]; then
    echo "Error: $STATE not found" >&2
    exit 1
fi

# --- Helper Functions ---

iso_timestamp() {
    date -u +%Y-%m-%dT%H:%M:%SZ
}

epoch_now() {
    date +%s
}

to_epoch() {
    local timestamp="$1"
    date -d "$timestamp" +%s 2>/dev/null || echo 0
}

# --- Main ---

now=$(epoch_now)
timestamp=$(iso_timestamp)
cleared=0

# Find READY_FOR_REVIEW tasks with expired review leases
while IFS=$'\t' read -r task_id reviewer lease_expires; do
    if [ -z "$lease_expires" ] || [ "$lease_expires" == "null" ]; then
        continue
    fi

    lease_epoch=$(to_epoch "$lease_expires")

    if [ "$now" -gt "$lease_epoch" ]; then
        echo "Clearing stale review claim: $task_id (reviewer: $reviewer, expired: $lease_expires)"

        # Clear reviewing_by and review_lease_expires
        flock -x "$STATE_LOCK" -c "
            yq -i '(.tasks[] | select(.id == \"$task_id\")).reviewing_by = null' '$STATE'
            yq -i '(.tasks[] | select(.id == \"$task_id\")).review_lease_expires = null' '$STATE'
        "

        # Log the cleanup
        cat >> "$LOG" << EOF
- timestamp: $timestamp
  agent: system
  action: stale_review_cleared
  task: $task_id
  stale_reviewer: $reviewer
  detail: "Review claim expired at $lease_expires"
EOF

        cleared=$((cleared + 1))
    fi
done < <(yq -r '.tasks[] | select(.status == "READY_FOR_REVIEW" and .reviewing_by != null) | "\(.id)\t\(.reviewing_by)\t\(.review_lease_expires)"' "$STATE" 2>/dev/null)

if [ "$cleared" -gt 0 ]; then
    echo "Cleared $cleared stale review claim(s)"
else
    echo "No stale review claims found"
fi
