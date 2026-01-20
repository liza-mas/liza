#!/bin/bash
# Validate blackboard state against schema
# Usage: liza-validate.sh [state.yaml]
#
# Error messages include the field/task causing the issue for quick debugging.
# Format: "INVALID: [issue description]" — exits with code 1 on first error.

set -euo pipefail

# --- Path Setup ---
STATE="${1:-}"
if [ -z "$STATE" ]; then
    PROJECT_ROOT=$(git rev-parse --show-toplevel)
    STATE="$PROJECT_ROOT/.liza/state.yaml"
else
    # STATE is like /path/to/project/.liza/state.yaml
    liza_dir=$(dirname "$STATE")
    PROJECT_ROOT=$(dirname "$liza_dir")
fi

# --- Constants ---
readonly VALID_TASK_STATES="DRAFT UNCLAIMED CLAIMED READY_FOR_REVIEW REJECTED APPROVED MERGED BLOCKED ABANDONED SUPERSEDED INTEGRATION_FAILED"
readonly VALID_SPRINT_STATUSES="IN_PROGRESS CHECKPOINT COMPLETED ABORTED"
readonly VALID_ANOMALY_TYPES="retry_loop trade_off spec_ambiguity external_blocker assumption_violated scope_deviation workaround debt_created spec_changed hypothesis_exhaustion spec_gap review_deadlock system_ambiguity"
readonly GRACE_PERIOD=60

# --- Helper Functions ---

invalid() {
    echo "INVALID: $*"
    exit 1
}

# Find tasks matching a yq filter and return their IDs
find_bad_tasks() {
    local filter="$1"
    yq -r ".tasks[] | select($filter) | .id" "$STATE" 2>/dev/null
}

# Count items matching a yq expression
count_yq() {
    local expr="$1"
    yq "$expr" "$STATE" 2>/dev/null || echo 0
}

# --- Required Fields Check ---

for field in version goal tasks agents config sprint; do
    if ! yq -e ".$field" "$STATE" > /dev/null 2>&1; then
        invalid "missing required field '$field'"
    fi
done

# --- Task State Validity ---

for status in $(yq '.tasks[].status' "$STATE" 2>/dev/null); do
    if ! echo "$VALID_TASK_STATES" | grep -qw "$status"; then
        invalid "unknown task status '$status'"
    fi
done

# --- Task Invariants ---

# DRAFT cannot have assigned_to
bad_tasks=$(find_bad_tasks '.status == "DRAFT" and .assigned_to != null')
[ -n "$bad_tasks" ] && invalid "DRAFT task with assigned_to: $bad_tasks"

# CLAIMED must have assigned_to
bad_tasks=$(find_bad_tasks '.status == "CLAIMED" and .assigned_to == null')
[ -n "$bad_tasks" ] && invalid "CLAIMED task without assigned_to: $bad_tasks"

# READY_FOR_REVIEW must have review_commit
bad_tasks=$(find_bad_tasks '.status == "READY_FOR_REVIEW" and .review_commit == null')
[ -n "$bad_tasks" ] && invalid "READY_FOR_REVIEW task without review_commit: $bad_tasks"

# CLAIMED must have worktree field
bad_tasks=$(find_bad_tasks '.status == "CLAIMED" and .worktree == null')
[ -n "$bad_tasks" ] && invalid "CLAIMED task without worktree field: $bad_tasks"

# CLAIMED must have base_commit (except integration_fix tasks)
bad_tasks=$(find_bad_tasks '.status == "CLAIMED" and .integration_fix != true and .base_commit == null')
[ -n "$bad_tasks" ] && invalid "CLAIMED task without base_commit: $bad_tasks"

# CLAIMED must have lease_expires
bad_tasks=$(find_bad_tasks '.status == "CLAIMED" and .lease_expires == null')
[ -n "$bad_tasks" ] && invalid "CLAIMED task without lease_expires: $bad_tasks"

# MERGED task must NOT have worktree (should be cleaned up)
bad_tasks=$(find_bad_tasks '.status == "MERGED" and .worktree != null')
[ -n "$bad_tasks" ] && invalid "MERGED task still has worktree: $bad_tasks"

# No two tasks can have same assigned_to (excluding null and terminal states)
duplicate_assignments=$(count_yq '[.tasks[] | select(.assigned_to != null and (.status == "CLAIMED" or .status == "READY_FOR_REVIEW" or .status == "REJECTED"))] | group_by(.assigned_to) | map(select(length > 1)) | length')
[ "$duplicate_assignments" -gt 0 ] && invalid "Agent assigned to multiple active tasks simultaneously"

# CLAIMED worktree path must exist
while IFS=$'\t' read -r task_id worktree_path; do
    if [ -n "$worktree_path" ] && [ "$worktree_path" != "null" ]; then
        if [ ! -d "$PROJECT_ROOT/$worktree_path" ]; then
            invalid "CLAIMED task $task_id has worktree=$worktree_path but directory does not exist"
        fi
    fi
done < <(yq -r '.tasks[] | select(.status == "CLAIMED" and .worktree != null) | "\(.id)\t\(.worktree)"' "$STATE" 2>/dev/null)

# BLOCKED must have blocked_reason and blocked_questions
bad_tasks=$(find_bad_tasks '.status == "BLOCKED" and (.blocked_reason == null or .blocked_questions == null)')
[ -n "$bad_tasks" ] && invalid "BLOCKED task without blocked_reason or blocked_questions: $bad_tasks"

# REJECTED must have rejection_reason
bad_tasks=$(find_bad_tasks '.status == "REJECTED" and .rejection_reason == null')
[ -n "$bad_tasks" ] && invalid "REJECTED task without rejection_reason: $bad_tasks"

# SUPERSEDED must have superseded_by and rescope_reason
bad_tasks=$(find_bad_tasks '.status == "SUPERSEDED" and (.superseded_by == null or .rescope_reason == null)')
[ -n "$bad_tasks" ] && invalid "SUPERSEDED task without superseded_by or rescope_reason: $bad_tasks"

# --- Dependency Validation ---

# Get all task IDs for reference checking
all_task_ids=$(yq -r '.tasks[].id' "$STATE" 2>/dev/null | tr '\n' ' ')

# depends_on must reference existing task IDs
while IFS=$'\t' read -r task_id dep_id; do
    if [ -n "$dep_id" ] && [ "$dep_id" != "null" ]; then
        if ! echo "$all_task_ids" | grep -qw "$dep_id"; then
            invalid "Task $task_id has depends_on referencing non-existent task '$dep_id'"
        fi
    fi
done < <(yq -r '.tasks[] | select(.depends_on != null) | .id as $tid | .depends_on[] | "\($tid)\t\(.)"' "$STATE" 2>/dev/null)

# CLAIMED task must have all depends_on tasks in MERGED status
while IFS=$'\t' read -r task_id; do
    unmet=$(yq -r "
        (.tasks[] | select(.id == \"$task_id\") | .depends_on // []) as \$deps |
        if (\$deps | length) == 0 then
            \"\"
        else
            [.tasks[] | select(.id as \$id | \$deps | contains([\$id])) | select(.status != \"MERGED\") | .id] | join(\", \")
        end
    " "$STATE" 2>/dev/null)
    if [ -n "$unmet" ] && [ "$unmet" != "null" ]; then
        invalid "CLAIMED task $task_id has unmet dependencies: $unmet (must be MERGED)"
    fi
done < <(yq -r '.tasks[] | select(.status == "CLAIMED" and .depends_on != null and (.depends_on | length) > 0) | .id' "$STATE" 2>/dev/null)

# Check for circular dependencies (simple cycle detection)
# For each task with depends_on, verify it doesn't transitively depend on itself
check_circular() {
    local start="$1"
    local current="$2"
    local visited="$3"

    # Get dependencies of current task
    local deps
    deps=$(yq -r ".tasks[] | select(.id == \"$current\") | .depends_on // [] | .[]" "$STATE" 2>/dev/null)

    for dep in $deps; do
        if [ "$dep" = "$start" ]; then
            invalid "Circular dependency detected: $start eventually depends on itself"
        fi
        if ! echo "$visited" | grep -qw "$dep"; then
            check_circular "$start" "$dep" "$visited $dep"
        fi
    done
}

for task_id in $(yq -r '.tasks[] | select(.depends_on != null and (.depends_on | length) > 0) | .id' "$STATE" 2>/dev/null); do
    check_circular "$task_id" "$task_id" "$task_id"
done

# Task with integration_fix:true must have prior INTEGRATION_FAILED in history
while IFS=$'\t' read -r task_id; do
    has_failed=$(yq ".tasks[] | select(.id == \"$task_id\") | .history[] | select(.event == \"integration_failed\") | length" "$STATE" 2>/dev/null || echo 0)
    if [ "$has_failed" = "0" ] || [ -z "$has_failed" ]; then
        invalid "Task $task_id has integration_fix:true but no INTEGRATION_FAILED event in history"
    fi
done < <(yq -r '.tasks[] | select(.integration_fix == true) | .id' "$STATE" 2>/dev/null)

# --- Agent Invariants ---

# Agent WORKING must have current_task assigned
working_no_task=$(count_yq '[.agents | to_entries[] | select(.value.status == "WORKING" and .value.current_task == null)] | length')
[ "$working_no_task" -gt 0 ] && invalid "Agent has status WORKING but no current_task assigned"

# Task failed_by list must contain unique agent IDs
while IFS=$'\t' read -r task_id failed_count unique_count; do
    if [ "$failed_count" != "$unique_count" ]; then
        invalid "Task $task_id has duplicate agent IDs in failed_by"
    fi
done < <(yq -r '.tasks[] | select(.failed_by != null) | "\(.id)\t\(.failed_by | length)\t\(.failed_by | unique | length)"' "$STATE" 2>/dev/null)

# Non-DRAFT tasks must have done_when (required for finalization)
missing_done_when=$(count_yq '[.tasks[] | select(.status != "DRAFT" and .status != "SUPERSEDED" and .status != "ABANDONED" and .done_when == null)] | length')
[ "$missing_done_when" -gt 0 ] && invalid "Non-DRAFT task missing done_when"

# Non-DRAFT tasks must have spec_ref
missing_spec_ref=$(count_yq '[.tasks[] | select(.status != "DRAFT" and .status != "SUPERSEDED" and .status != "ABANDONED" and .spec_ref == null)] | length')
[ "$missing_spec_ref" -gt 0 ] && invalid "Non-DRAFT task missing spec_ref"

# --- Spec File Validation ---

if [ "${SKIP_SPEC_FILE_CHECK:-false}" != "true" ]; then
    spec_errors=0
    while read -r spec_ref; do
        # Strip anchor if present (specs/api.md#section → specs/api.md)
        spec_file="${spec_ref%%#*}"
        if [ -n "$spec_file" ] && [ "$spec_file" != "null" ] && [ ! -f "$PROJECT_ROOT/$spec_file" ]; then
            echo "INVALID: spec_ref file not found: $spec_file"
            spec_errors=$((spec_errors + 1))
        fi
    done < <(yq -r '.tasks[] | select(.spec_ref != null) | .spec_ref' "$STATE" 2>/dev/null)
    if [ "$spec_errors" -gt 0 ]; then
        echo "($spec_errors spec_ref file(s) missing — create specs or set SKIP_SPEC_FILE_CHECK=true)"
        exit 1
    fi
fi

# --- Handoff Validation ---

handoff_missing=$(count_yq '[.handoff | to_entries[] | select(.value.summary == null or .value.next_action == null)] | length')
[ "$handoff_missing" -gt 0 ] && invalid "handoff entry missing required fields (summary, next_action)"

# --- Discovered Items Validation ---

invalid_urgency=$(count_yq '[.discovered[] | select(.urgency != null and .urgency != "deferred" and .urgency != "immediate")] | length')
[ "$invalid_urgency" -gt 0 ] && invalid "discovered item has invalid urgency (must be 'deferred' or 'immediate')"

# --- Agent Lease Validation ---

now=$(date +%s)
while read -r agent_info; do
    [ -z "$agent_info" ] && continue
    agent_id=$(echo "$agent_info" | cut -d: -f1)
    [ -z "$agent_id" ] && continue
    lease_expires=$(echo "$agent_info" | cut -d: -f2)
    if [ -n "$lease_expires" ] && [ "$lease_expires" != "null" ]; then
        lease_epoch=$(date -d "$lease_expires" +%s 2>/dev/null || echo 0)
        if [ $((lease_epoch + GRACE_PERIOD)) -lt "$now" ]; then
            invalid "Agent $agent_id has status WORKING but lease expired (beyond grace period)"
        fi
    else
        invalid "Agent $agent_id has status WORKING but no lease_expires"
    fi
done < <(yq -r '.agents | to_entries[] | select(.value.status == "WORKING") | "\(.key):\(.value.lease_expires)"' "$STATE" 2>/dev/null)

# --- Anomaly Type Validation ---

for atype in $(yq '.anomalies[].type' "$STATE" 2>/dev/null); do
    if ! echo "$VALID_ANOMALY_TYPES" | grep -qw "$atype"; then
        invalid "unknown anomaly type '$atype'"
    fi
done

# --- Anomaly Details Validation ---

# retry_loop requires: count, error_pattern
retry_malformed=$(count_yq '[.anomalies[] | select(.type == "retry_loop" and (.details.count == null or .details.error_pattern == null))] | length')
[ "$retry_malformed" -gt 0 ] && invalid "retry_loop anomaly missing required details (count, error_pattern)"

# trade_off requires: what, why, debt_created
trade_off_malformed=$(count_yq '[.anomalies[] | select(.type == "trade_off" and (.details.what == null or .details.why == null or .details.debt_created == null))] | length')
[ "$trade_off_malformed" -gt 0 ] && invalid "trade_off anomaly missing required details (what, why, debt_created)"

# external_blocker requires: blocker_service
external_malformed=$(count_yq '[.anomalies[] | select(.type == "external_blocker" and .details.blocker_service == null)] | length')
[ "$external_malformed" -gt 0 ] && invalid "external_blocker anomaly missing required details (blocker_service)"

# assumption_violated requires: assumption, reality
assumption_malformed=$(count_yq '[.anomalies[] | select(.type == "assumption_violated" and (.details.assumption == null or .details.reality == null))] | length')
[ "$assumption_malformed" -gt 0 ] && invalid "assumption_violated anomaly missing required details (assumption, reality)"

# system_ambiguity requires: protocol_section, question
system_malformed=$(count_yq '[.anomalies[] | select(.type == "system_ambiguity" and (.details.protocol_section == null or .details.question == null))] | length')
[ "$system_malformed" -gt 0 ] && invalid "system_ambiguity anomaly missing required details (protocol_section, question)"

# --- Sprint Validation ---

sprint_status=$(yq '.sprint.status // "MISSING"' "$STATE" 2>/dev/null)
if [ "$sprint_status" != "MISSING" ] && [ "$sprint_status" != "null" ]; then
    if ! echo "$VALID_SPRINT_STATUSES" | grep -qw "$sprint_status"; then
        invalid "unknown sprint status '$sprint_status' (valid: $VALID_SPRINT_STATUSES)"
    fi
fi

# Sprint goal_ref must reference existing goal
sprint_goal_ref=$(yq '.sprint.goal_ref // ""' "$STATE" 2>/dev/null)
if [ -n "$sprint_goal_ref" ] && [ "$sprint_goal_ref" != "null" ]; then
    goal_id=$(yq '.goal.id // ""' "$STATE" 2>/dev/null)
    if [ "$sprint_goal_ref" != "$goal_id" ]; then
        invalid "sprint.goal_ref ($sprint_goal_ref) does not match goal.id ($goal_id)"
    fi
fi

# Sprint scope.planned tasks must exist
while read -r planned_task; do
    if [ -n "$planned_task" ] && [ "$planned_task" != "null" ]; then
        task_exists=$(count_yq "[.tasks[] | select(.id == \"$planned_task\")] | length")
        if [ "$task_exists" -eq 0 ]; then
            invalid "sprint.scope.planned references non-existent task '$planned_task'"
        fi
    fi
done < <(yq -r '.sprint.scope.planned[]? // empty' "$STATE" 2>/dev/null)

# Sprint scope.stretch tasks must exist (if present)
while read -r stretch_task; do
    if [ -n "$stretch_task" ] && [ "$stretch_task" != "null" ]; then
        task_exists=$(count_yq "[.tasks[] | select(.id == \"$stretch_task\")] | length")
        if [ "$task_exists" -eq 0 ]; then
            invalid "sprint.scope.stretch references non-existent task '$stretch_task'"
        fi
    fi
done < <(yq -r '.sprint.scope.stretch[]? // empty' "$STATE" 2>/dev/null)

# Sprint timeline.started must be set if sprint exists
sprint_started=$(yq '.sprint.timeline.started // ""' "$STATE" 2>/dev/null)
if [ -z "$sprint_started" ] || [ "$sprint_started" == "null" ]; then
    sprint_exists=$(yq '.sprint // ""' "$STATE" 2>/dev/null)
    if [ -n "$sprint_exists" ] && [ "$sprint_exists" != "null" ]; then
        invalid "sprint.timeline.started is required"
    fi
fi

echo "VALID"
