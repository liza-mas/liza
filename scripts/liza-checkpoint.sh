#!/bin/bash
# Create checkpoint and generate sprint summary
# Usage: liza-checkpoint.sh [project_root]
#
# Creates CHECKPOINT file to halt agents and generates a sprint summary
# for human review. Human removes CHECKPOINT to resume.

set -euo pipefail

# --- Path Setup ---
PROJECT_ROOT="${1:-$(git rev-parse --show-toplevel)}"
readonly PROJECT_ROOT
readonly STATE="$PROJECT_ROOT/.liza/state.yaml"
readonly CHECKPOINT="$PROJECT_ROOT/.liza/CHECKPOINT"
readonly SUMMARY="$PROJECT_ROOT/.liza/sprint_summary.md"

# --- Helper Functions ---

iso_timestamp() {
    date -u +%Y-%m-%dT%H:%M:%SZ
}

count_tasks() {
    local filter="$1"
    yq "[.tasks[] | select($filter)] | length" "$STATE" 2>/dev/null || echo 0
}

# --- Validation ---

if [ -f "$CHECKPOINT" ]; then
    echo "CHECKPOINT already exists. Remove it first to create a new one."
    exit 1
fi

# --- Create Checkpoint ---

timestamp=$(iso_timestamp)
echo "$timestamp" > "$CHECKPOINT"
echo "CHECKPOINT created at $timestamp"

# --- Gather Metrics ---

tasks_total=$(yq '.tasks | length' "$STATE")
tasks_done=$(count_tasks '.status == "MERGED" or .status == "ABANDONED" or .status == "SUPERSEDED"')
tasks_in_progress=$(count_tasks '.status == "CLAIMED" or .status == "READY_FOR_REVIEW" or .status == "REJECTED"')
tasks_blocked=$(count_tasks '.status == "BLOCKED"')
tasks_unclaimed=$(count_tasks '.status == "UNCLAIMED"')
anomalies_count=$(yq '.anomalies | length' "$STATE")

sprint_id=$(yq '.sprint.id // "none"' "$STATE")
sprint_started=$(yq '.sprint.timeline.started // "unknown"' "$STATE")
sprint_deadline=$(yq '.sprint.timeline.deadline // "unknown"' "$STATE")

# --- Compute Durations ---

sprint_elapsed_hours="unknown"
if [ "$sprint_started" != "unknown" ]; then
    start_epoch=$(date -d "$sprint_started" +%s 2>/dev/null || echo 0)
    now_epoch=$(date +%s)
    if [ "$start_epoch" -gt 0 ]; then
        elapsed_seconds=$((now_epoch - start_epoch))
        sprint_elapsed_hours=$(echo "scale=1; $elapsed_seconds / 3600" | bc)
    fi
fi

# Per-task durations (created → merged, for completed tasks)
task_durations=$(yq -r '
    .tasks[] |
    select(.status == "MERGED") |
    select(.history != null) |
    (.history | map(select(.event == "created")) | .[0].time) as $created |
    (.history | map(select(.event == "merged")) | .[0].time) as $merged |
    select($created != null and $merged != null) |
    "\(.id):\($created):\($merged)"
' "$STATE" 2>/dev/null | while IFS=: read -r task created merged; do
    if [ -n "$task" ] && [ -n "$created" ] && [ -n "$merged" ]; then
        created_epoch=$(date -d "$created" +%s 2>/dev/null || echo 0)
        merged_epoch=$(date -d "$merged" +%s 2>/dev/null || echo 0)
        if [ "$created_epoch" -gt 0 ] && [ "$merged_epoch" -gt 0 ]; then
            duration_hours=$(echo "scale=1; ($merged_epoch - $created_epoch) / 3600" | bc)
            echo "| $task | ${duration_hours}h |"
        fi
    fi
done)

# --- Generate Summary ---

cat > "$SUMMARY" << EOF
# Sprint Summary

**Generated:** $timestamp
**Sprint:** $sprint_id
**Started:** $sprint_started
**Deadline:** $sprint_deadline
**Elapsed:** ${sprint_elapsed_hours}h

## Task Status

| Status | Count |
|--------|-------|
| Done (MERGED/ABANDONED/SUPERSEDED) | $tasks_done |
| In Progress (CLAIMED/READY_FOR_REVIEW/REJECTED) | $tasks_in_progress |
| Blocked | $tasks_blocked |
| Unclaimed | $tasks_unclaimed |
| **Total** | $tasks_total |

## Task Durations (Completed)

| Task | Duration |
|------|----------|
$task_durations

## Anomalies

Total logged: $anomalies_count

$(yq -r '.anomalies[] | "- [\(.type)] \(.task): \(.details | to_entries | map("\(.key)=\(.value)") | join(", "))"' "$STATE" 2>/dev/null || echo "None")

## Blocked Tasks

$(yq -r '.tasks[] | select(.status == "BLOCKED") | "### \(.id)\n**Reason:** \(.blocked_reason // "no reason")\n**Questions:**\n\(.blocked_questions // [] | map("- " + .) | join("\n"))\n"' "$STATE" 2>/dev/null || echo "None")

## Human Review Checklist

- [ ] Review task progress
- [ ] Review anomalies for patterns
- [ ] Check goal alignment
- [ ] Decide: CONTINUE / ADJUST / REPLAN / STOP

## To Resume

Remove the CHECKPOINT file:
\`\`\`bash
rm $CHECKPOINT
\`\`\`

Agents will restart automatically.
EOF

echo ""
echo "Sprint summary written to: $SUMMARY"
echo ""
echo "Agents will halt at next safe point."
echo "Review $SUMMARY, then remove $CHECKPOINT to resume."
