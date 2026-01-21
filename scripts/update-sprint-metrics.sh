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

# --- Review Quality Metrics ---
# review_verdict_approval_rate = approvals / (approvals + rejections)
# task_outcome_approval_rate = approvals / submitted_for_review_count

review_verdict_approvals=$(yq '[.tasks[].history[] | select(.event == "approved")] | length' "$STATE" 2>/dev/null || echo 0)
review_verdict_rejections=$(yq '[.tasks[].history[] | select(.event == "rejected")] | length' "$STATE" 2>/dev/null || echo 0)
review_verdict_count=$((review_verdict_approvals + review_verdict_rejections))
task_submitted_for_review_count=$(yq '[.tasks[].history[] | select(.event == "ready_for_review")] | length' "$STATE" 2>/dev/null || echo 0)

if [ "$review_verdict_count" -gt 0 ]; then
    review_verdict_approval_rate=$((review_verdict_approvals * 100 / review_verdict_count))
else
    review_verdict_approval_rate=0
fi

if [ "$task_submitted_for_review_count" -gt 0 ]; then
    task_outcome_approval_rate=$((review_verdict_approvals * 100 / task_submitted_for_review_count))
else
    task_outcome_approval_rate=0
fi

# --- Update State ---

flock -x "$STATE_LOCK" -c "
    yq -i '.sprint.metrics.tasks_done = $tasks_done' '$STATE'
    yq -i '.sprint.metrics.tasks_in_progress = $tasks_in_progress' '$STATE'
    yq -i '.sprint.metrics.tasks_blocked = $tasks_blocked' '$STATE'
    yq -i '.sprint.metrics.iterations_total = $iterations_total' '$STATE'
    yq -i '.sprint.metrics.review_cycles_total = $review_cycles_total' '$STATE'
    yq -i '.sprint.metrics.review_verdict_approvals = $review_verdict_approvals' '$STATE'
    yq -i '.sprint.metrics.review_verdict_rejections = $review_verdict_rejections' '$STATE'
    yq -i '.sprint.metrics.review_verdict_count = $review_verdict_count' '$STATE'
    yq -i '.sprint.metrics.review_verdict_approval_rate_percent = $review_verdict_approval_rate' '$STATE'
    yq -i '.sprint.metrics.task_submitted_for_review_count = $task_submitted_for_review_count' '$STATE'
    yq -i '.sprint.metrics.task_outcome_approval_rate_percent = $task_outcome_approval_rate' '$STATE'
"

echo "Sprint metrics updated: done=$tasks_done, in_progress=$tasks_in_progress, blocked=$tasks_blocked, iterations=$iterations_total, review_cycles=$review_cycles_total"
echo "Review metrics: approvals=$review_verdict_approvals, rejections=$review_verdict_rejections, review_verdict_rate=${review_verdict_approval_rate}%"
echo "Review outcomes: submitted=$task_submitted_for_review_count, task_outcome_rate=${task_outcome_approval_rate}%"

# --- Review Quality Warning ---
# Flag suspiciously high approval rates (potential rubber-stamping)
# Threshold: >95% approval with at least 5 reviews

if [ "$review_verdict_count" -ge 5 ] && [ "$review_verdict_approval_rate" -gt 95 ]; then
    echo ""
    echo "⚠️  WARNING: High approval rate detected (${review_verdict_approval_rate}% over ${review_verdict_count} reviews)"
    echo "    This may indicate Code Reviewer rubber-stamping. Consider:"
    echo "    - Human spot-check of recent merged tasks"
    echo "    - Review Code Reviewer's rejection rationale quality"
    echo ""
fi
