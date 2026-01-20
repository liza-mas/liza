#!/bin/bash
# Recompute sprint.metrics from current task state
# Usage: update-sprint-metrics.sh [project_root]
#
# Called after state-changing operations to keep sprint.metrics current.
# Metrics are derived from task states — this script ensures consistency.

set -euo pipefail

# --- Path Setup ---

PROJECT_ROOT="${1:-$(git rev-parse --show-toplevel)}"
readonly PROJECT_ROOT
readonly STATE="$PROJECT_ROOT/.liza/state.yaml"
readonly STATE_LOCK="$STATE.lock"

# --- Helper Functions ---

die() {
    echo "Error: $*" >&2
    exit 1
}

count_tasks() {
    local filter="$1"
    yq "[.tasks[] | select($filter)] | length" "$STATE" 2>/dev/null || echo 0
}

sum_field() {
    local field="$1"
    yq "[.tasks[].$field // 0] | add // 0" "$STATE" 2>/dev/null || echo 0
}

# --- Validation ---

if [ ! -f "$STATE" ]; then
    die "$STATE not found"
fi

# --- Compute Metrics ---

# Task counts by status category (per sprint-governance.md:347-349)
tasks_done=$(count_tasks '.status == "MERGED" or .status == "ABANDONED" or .status == "SUPERSEDED"')
tasks_in_progress=$(count_tasks '.status == "CLAIMED" or .status == "READY_FOR_REVIEW" or .status == "REJECTED" or .status == "APPROVED"')
tasks_blocked=$(count_tasks '.status == "BLOCKED" or .status == "INTEGRATION_FAILED"')

# Aggregate metrics across all tasks
iterations_total=$(sum_field "iteration")
review_cycles_total=$(sum_field "review_cycles_total")

# --- Update State ---

flock -x "$STATE_LOCK" -c "
    yq -i '.sprint.metrics.tasks_done = $tasks_done' '$STATE'
    yq -i '.sprint.metrics.tasks_in_progress = $tasks_in_progress' '$STATE'
    yq -i '.sprint.metrics.tasks_blocked = $tasks_blocked' '$STATE'
    yq -i '.sprint.metrics.iterations_total = $iterations_total' '$STATE'
    yq -i '.sprint.metrics.review_cycles_total = $review_cycles_total' '$STATE'
"

echo "Sprint metrics updated: done=$tasks_done, in_progress=$tasks_in_progress, blocked=$tasks_blocked, iterations=$iterations_total, review_cycles=$review_cycles_total"
