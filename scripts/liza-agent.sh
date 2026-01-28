#!/bin/bash
# Agent supervisor - restarts agent on graceful abort
# Usage: LIZA_AGENT_ID=coder-1 liza-agent.sh [--cli claude|codex|gemini|mistral] <role> [initial-task-id]

set -euo pipefail

# --- Configuration ---
readonly RESTART_DELAY=2
readonly CRASH_DELAY=5

# --- Argument Parsing ---
CLI="claude"
while [[ $# -gt 0 ]]; do
    case "$1" in
        --cli) CLI="$2"; shift 2 ;;
        *) break ;;
    esac
done

if [[ -z "${1:-}" ]]; then
    echo "Usage: liza-agent.sh [--cli claude|codex|gemini|mistral] <role> [initial-task-id]" >&2
    exit 1
fi

case "$CLI" in
    claude|codex|gemini|mistral) ;;
    *) echo "Error: --cli must be 'claude', 'codex', 'gemini', or 'mistral', got '$CLI'" >&2; exit 1 ;;
esac
readonly CLI

# --- Path Setup ---
# Normalize role: CODE_REVIEWER → code-reviewer, "Code Reviewer" → code-reviewer
ROLE="$1"
ROLE="${ROLE,,}"      # lowercase
ROLE="${ROLE// /-}"   # spaces → hyphens
ROLE="${ROLE//_/-}"   # underscores → hyphens
readonly ROLE
INITIAL_TASK="${2:-}"

PROJECT_ROOT=$(git rev-parse --show-toplevel)
readonly PROJECT_ROOT
readonly LIZA_DIR="$PROJECT_ROOT/.liza"
readonly STATE="$LIZA_DIR/state.yaml"
readonly STATE_LOCK="$STATE.lock"

# Detect Liza specs location from script symlink (scripts/ is sibling to specs/)
SCRIPT_PATH=$(readlink -f "${BASH_SOURCE[0]}")
readonly SCRIPT_PATH
SCRIPT_DIR=$(dirname "$SCRIPT_PATH")
readonly SCRIPT_DIR
LIZA_SPECS=$(dirname "$SCRIPT_DIR")/specs
readonly LIZA_SPECS

# --- Helper Functions ---

source "$SCRIPT_DIR/liza-prompt-builders.sh"

die() {
    echo "ERROR: $*" >&2
    exit 1
}

# Get config value with default fallback
get_config() {
    local key="$1"
    local default="$2"
    yq ".config.$key // $default" "$STATE" 2>/dev/null || echo "$default"
}

# Count tasks matching a yq filter
count_tasks() {
    local filter="$1"
    yq "[.tasks[] | select($filter)] | length" "$STATE" 2>/dev/null || echo 0
}

# Get task field by ID
get_task_field() {
    local task_id="$1"
    local field="$2"
    yq -r ".tasks[] | select(.id == \"$task_id\") | .$field" "$STATE" 2>/dev/null
}

# Execute yq update with file lock
locked_yq() {
    flock -x "$STATE_LOCK" yq -i "$@" "$STATE"
}

# Check for abort signal
check_abort() {
    [ -f "$LIZA_DIR/ABORT" ]
}

# Format ISO timestamp
iso_timestamp() {
    date -u +%Y-%m-%dT%H:%M:%SZ
}

# Format ISO timestamp with offset
iso_timestamp_offset() {
    local offset="$1"
    date -u -d "$offset" +%Y-%m-%dT%H:%M:%SZ
}


# --- Identity Validation ---
# See roles.md#agent-identity-protocol for full specification

validate_agent_identity() {
    if [ -z "${LIZA_AGENT_ID:-}" ]; then
        die "LIZA_AGENT_ID environment variable is required
Usage: LIZA_AGENT_ID=coder-1 liza-agent.sh coder"
    fi

    # Validate format: {role}-{number}
    if ! [[ "$LIZA_AGENT_ID" =~ ^(coder|code-reviewer|planner)-[0-9]+$ ]]; then
        die "Invalid LIZA_AGENT_ID format: $LIZA_AGENT_ID
Expected: {role}-{number} (e.g., coder-1, code-reviewer-2, planner-1)"
    fi

    # Extract role prefix and validate against $ROLE
    local id_role_prefix="${LIZA_AGENT_ID%-[0-9]*}"
    if [ "$id_role_prefix" != "$ROLE" ]; then
        die "LIZA_AGENT_ID role mismatch: $LIZA_AGENT_ID vs role=$ROLE"
    fi
}

validate_agent_identity

# --- Registration with Collision Prevention ---
register_agent() {
    local now
    now=$(iso_timestamp)
    local lease_seconds
    lease_seconds=$(get_config lease_duration 1800)
    local lease
    lease=$(iso_timestamp_offset "+${lease_seconds} seconds")
    local terminal
    terminal=$(tty 2>/dev/null || echo unknown)

    flock -x "$STATE_LOCK" -c "
        existing_lease=\$(yq -r '.agents.\"$LIZA_AGENT_ID\".lease_expires // \"\"' '$STATE')
        if [ -n \"\$existing_lease\" ] && [ \"\$existing_lease\" != 'null' ]; then
            if [ \$(date -d \"\$existing_lease\" +%s 2>/dev/null || echo 0) -gt \$(date +%s) ]; then
                echo 'COLLISION: $LIZA_AGENT_ID already registered until' \$existing_lease >&2
                exit 1
            fi
        fi
        yq -i '.agents.\"$LIZA_AGENT_ID\" = {
            \"role\": \"$ROLE\",
            \"status\": \"STARTING\",
            \"lease_expires\": \"$lease\",
            \"heartbeat\": \"$now\",
            \"terminal\": \"$terminal\",
            \"iterations_total\": 0,
            \"context_percent\": 0
        }' '$STATE'
    "
}

# Unregister agent from blackboard on exit
unregister_agent() {
    echo "Unregistering agent: $LIZA_AGENT_ID"
    flock -x "$STATE_LOCK" -c "
        yq -i 'del(.agents.\"$LIZA_AGENT_ID\")' '$STATE'
    " 2>/dev/null || true
}

# Trap to ensure cleanup on any exit (including Ctrl+C)
trap unregister_agent EXIT

register_agent || die "Failed to register agent $LIZA_AGENT_ID (collision?)"
echo "Registered agent: $LIZA_AGENT_ID"

# Transition STARTING → IDLE (per state-machines.md:168-170)
locked_yq ".agents.\"$LIZA_AGENT_ID\".status = \"IDLE\""

# Code Reviewer startup: clear stale review claims from crashed reviewers
if [ "$ROLE" = "code-reviewer" ]; then
    "$SCRIPT_DIR/clear-stale-review-claims.sh" "$PROJECT_ROOT" 2>/dev/null || true
fi

# --- Work Availability Check (Role-Specific) ---

# Print polling status message
log_polling() {
    local msg="$1"
    local poll_interval="$2"
    local waited="$3"
    local max_wait="$4"
    echo "$msg Polling in ${poll_interval}s (waited ${waited}s/${max_wait}s)..."
}

# Count claimable tasks (UNCLAIMED, REJECTED, or INTEGRATION_FAILED with all dependencies satisfied)
# Uses array subtraction: (depends_on - merged_ids) gives unmet deps; length == 0 means all satisfied
count_claimable_tasks() {
    yq -r '
        (.tasks | map(select(.status == "MERGED") | .id)) as $merged |
        [.tasks[] | select(
            (.status == "UNCLAIMED" or .status == "REJECTED" or .status == "INTEGRATION_FAILED") and
            (((.depends_on // []) - $merged) | length == 0)
        )] | length
    ' "$STATE" 2>/dev/null || echo 0
}

# Coder: Returns 0 if claimable tasks exist or work pending, 1 if system idle
wait_for_coder_work() {
    local poll_interval
    poll_interval=$(get_config coder_poll_interval 30)
    local max_wait
    max_wait=$(get_config coder_max_wait 1800)
    local waited=0

    while [ "$waited" -lt "$max_wait" ]; do
        check_abort && return 1

        local claimable
        claimable=$(count_claimable_tasks)
        local unclaimed
        unclaimed=$(count_tasks '.status == "UNCLAIMED"')
        local waiting_on_deps=$((unclaimed - claimable))
        local draft
        draft=$(count_tasks '.status == "DRAFT"')
        local in_progress
        in_progress=$(count_tasks '.status == "CLAIMED" or .status == "READY_FOR_REVIEW" or .status == "REJECTED"')

        if [ "$claimable" -gt 0 ]; then
            echo "Found $claimable claimable task(s)."
            return 0
        fi

        if [ "$waiting_on_deps" -gt 0 ]; then
            log_polling "No claimable tasks. $waiting_on_deps waiting on dependencies, $in_progress in progress." "$poll_interval" "$waited" "$max_wait"
        elif [ "$draft" -gt 0 ] || [ "$in_progress" -gt 0 ]; then
            log_polling "No claimable tasks. DRAFT: $draft, In progress: $in_progress." "$poll_interval" "$waited" "$max_wait"
        else
            log_polling "No tasks yet. Waiting for Planner..." "$poll_interval" "$waited" "$max_wait"
        fi

        sleep "$poll_interval"
        waited=$((waited + poll_interval))
    done

    echo "Max wait (${max_wait}s) exceeded. Consider checking Planner status."
    return 1
}

# Planner: Returns 0 if wake triggers exist, 1 if system idle
# Wake triggers per roles.md:100-110: BLOCKED, hypothesis exhaustion, INTEGRATION_FAILED, immediate discovery
# Special case: empty tasks array = initial planning needed
wait_for_planner_work() {
    local poll_interval
    poll_interval=$(get_config planner_poll_interval 60)
    local max_wait
    max_wait=$(get_config planner_max_wait 600)  # 10 min default
    local waited=0

    while [ "$waited" -lt "$max_wait" ]; do
        check_abort && return 1

        # Check if this is initial planning (no tasks exist yet)
        local total_tasks
        total_tasks=$(yq '.tasks | length' "$STATE" 2>/dev/null || echo 0)
        if [ "$total_tasks" -eq 0 ]; then
            echo "No tasks exist. Initial planning needed."
            return 0
        fi

        # Check wake triggers
        local blocked
        blocked=$(count_tasks '.status == "BLOCKED"')
        local integration_failed
        integration_failed=$(count_tasks '.status == "INTEGRATION_FAILED"')
        local hypothesis_exhausted
        hypothesis_exhausted=$(yq '[.tasks[] | select(.failed_by != null and (.failed_by | length) >= 2)] | length' "$STATE" 2>/dev/null || echo 0)
        local immediate_discovery
        immediate_discovery=$(yq '[.discovered[] | select(.urgency == "immediate" and .converted_to_task == null)] | length' "$STATE" 2>/dev/null || echo 0)

        local total_triggers=$((blocked + integration_failed + hypothesis_exhausted + immediate_discovery))

        if [ "$total_triggers" -gt 0 ]; then
            echo "Planner wake triggers: BLOCKED=$blocked, INTEGRATION_FAILED=$integration_failed, HYPOTHESIS_EXHAUSTED=$hypothesis_exhausted, IMMEDIATE=$immediate_discovery"
            return 0
        fi

        # Check sprint completion: all planned tasks are terminal (MERGED, ABANDONED, SUPERSEDED)
        # Sprint can complete even if unplanned tasks are still in progress
        local planned_count planned_terminal sprint_status sprint_complete
        planned_count=$(yq '.sprint.scope.planned | length' "$STATE" 2>/dev/null || echo 0)
        planned_terminal=$(yq '
            (.sprint.scope.planned // []) as $planned |
            [.tasks[] | select(.id as $id | $planned | contains([$id])) | select(.status == "MERGED" or .status == "ABANDONED" or .status == "SUPERSEDED")] | length
        ' "$STATE" 2>/dev/null || echo 0)
        sprint_status=$(yq '.sprint.status // ""' "$STATE" 2>/dev/null)
        sprint_complete=false

        if [ "$planned_count" -gt 0 ] && [ "$planned_terminal" -eq "$planned_count" ]; then
            sprint_complete=true
            # Only update sprint status on transition (not already COMPLETED)
            if [ "$sprint_status" != "COMPLETED" ]; then
                # Display sprint progress
                local in_progress planned_merged
                in_progress=$(count_tasks '.status == "CLAIMED"')
                planned_merged=$(yq '
                    (.sprint.scope.planned // []) as $planned |
                    [.tasks[] | select(.id as $id | $planned | contains([$id])) | select(.status == "MERGED")] | length
                ' "$STATE" 2>/dev/null || echo 0)
                echo ""
                echo "Sprint Progress:"
                echo "  Planned tasks: $planned_count"
                echo "  Merged: $planned_merged"
                echo "  Abandoned/Superseded: $((planned_terminal - planned_merged))"
                if [ "$in_progress" -gt 0 ]; then
                    echo "  Unplanned tasks still in progress: $in_progress"
                fi
                echo ""
                echo "All $planned_count planned task(s) complete. Sprint done."
                # Update sprint status only (goal completion is separate)
                local now
                now=$(iso_timestamp)
                flock -x "$STATE_LOCK" -c "
                    yq -i '.sprint.status = \"COMPLETED\" | .sprint.timeline.ended = \"$now\"' '$STATE'
                "
            fi
        fi

        # Check if tasks are still in progress
        local active
        active=$(count_tasks '.status == "CLAIMED" or .status == "READY_FOR_REVIEW" or .status == "APPROVED" or .status == "UNCLAIMED" or .status == "DRAFT"')

        if [ "$active" -gt 0 ]; then
            if [ "$sprint_complete" = true ]; then
                echo "Sprint complete, but $active unplanned task(s) still active."
            fi
            log_polling "No wake triggers, but $active active task(s)." "$poll_interval" "$waited" "$max_wait"
            sleep "$poll_interval"
            waited=$((waited + poll_interval))
            continue
        fi

        # All tasks terminal — goal complete
        local now
        now=$(iso_timestamp)
        flock -x "$STATE_LOCK" -c "
            yq -i '.goal.status = \"COMPLETED\"' '$STATE'
        "
        echo "No active tasks. Goal complete."
        return 1
    done

    echo "Max wait (${max_wait}s) exceeded. Planner exiting to allow restart."
    return 1
}

# Code Reviewer: Returns 0 if READY_FOR_REVIEW tasks exist, 1 if system idle
wait_for_reviewer_work() {
    local poll_interval
    poll_interval=$(get_config reviewer_poll_interval 30)
    local max_wait
    max_wait=$(get_config reviewer_max_wait 1800)
    local waited=0

    while [ "$waited" -lt "$max_wait" ]; do
        check_abort && return 1

        local ready
        ready=$(count_tasks '.status == "READY_FOR_REVIEW"')
        local in_progress
        in_progress=$(count_tasks '.status == "CLAIMED"')

        if [ "$ready" -gt 0 ]; then
            echo "Found $ready task(s) ready for review."
            return 0
        fi

        # Note: APPROVED tasks with merge_commit are already done - they should be MERGED
        # Only count APPROVED tasks without merge_commit as needing work
        local approved_pending
        approved_pending=$(count_tasks '.status == "APPROVED" and .merge_commit == null')
        if [ "$approved_pending" -gt 0 ]; then
            echo "Found $approved_pending APPROVED task(s) awaiting merge."
            return 0
        fi

        # Count total tasks and completed tasks
        local total_tasks
        total_tasks=$(yq '.tasks | length' "$STATE" 2>/dev/null || echo 0)
        local completed
        completed=$(count_tasks '.status == "APPROVED" and .merge_commit != null')

        if [ "$in_progress" -gt 0 ]; then
            log_polling "No reviewable tasks. $in_progress task(s) in progress." "$poll_interval" "$waited" "$max_wait"
        elif [ "$total_tasks" -eq 0 ]; then
            log_polling "No tasks yet. Waiting for Planner..." "$poll_interval" "$waited" "$max_wait"
        elif [ "$completed" -eq "$total_tasks" ]; then
            echo "All $total_tasks task(s) completed and merged. No more work."
            return 1
        else
            log_polling "No reviewable tasks. Waiting for Coder..." "$poll_interval" "$waited" "$max_wait"
        fi

        sleep "$poll_interval"
        waited=$((waited + poll_interval))
    done

    echo "Max wait (${max_wait}s) exceeded."
    return 1
}

# Dispatch to role-specific wait function
wait_for_work() {
    case "$ROLE" in
        coder) wait_for_coder_work ;;
        code-reviewer) wait_for_reviewer_work ;;
        planner) wait_for_planner_work ;;
        *) echo "Unknown role: $ROLE"; return 1 ;;
    esac
}

# Supervisor: Handle merge for APPROVED tasks
# Called after Code Reviewer agent completes - supervisor runs merge (not agent)
# This avoids permission prompts in non-interactive agent mode
handle_approved_merges() {
    local task_id
    task_id=$(yq -r '[.tasks[] | select(.status == "APPROVED" and .approved_by == "'"$LIZA_AGENT_ID"'")] | .[0].id // ""' "$STATE" 2>/dev/null)

    while [ -n "$task_id" ] && [ "$task_id" != "null" ]; do
        echo "Supervisor: Merging APPROVED task $task_id..."
        if "$SCRIPT_DIR/wt-merge.sh" "$task_id"; then
            echo "Supervisor: Merged $task_id successfully."
        else
            echo "ERROR: Supervisor failed to merge $task_id. Manual intervention needed."
            return 1
        fi
        # Check for more APPROVED tasks (by this reviewer)
        task_id=$(yq -r '[.tasks[] | select(.status == "APPROVED" and .approved_by == "'"$LIZA_AGENT_ID"'")] | .[0].id // ""' "$STATE" 2>/dev/null)
    done
}

# Find highest-priority task by status
find_task_by_status() {
    local status="$1"
    yq -r "[.tasks[] | select(.status == \"$status\")] | sort_by(.priority) | .[0].id // \"\"" "$STATE" 2>/dev/null
}

# Find highest-priority reviewable task (READY_FOR_REVIEW with no active review lease)
# A task is reviewable if: status == READY_FOR_REVIEW AND one of:
#   - reviewing_by is null (no one assigned)
#   - reviewing_by is set AND review_lease_expires is set AND expired (stale claim)
# Invalid state (reviewing_by set but review_lease_expires missing) is NOT reviewable - fail fast
# Note: Uses shell interpolation for $now because yq doesn't support --arg like jq
find_reviewable_task() {
    local now
    now=$(iso_timestamp)
    yq -r '
        [.tasks[] | select(
            .status == "READY_FOR_REVIEW" and
            (
                # Case 1: No one assigned
                ((.reviewing_by // null) == null) or
                # Case 2: Someone assigned with valid expired lease
                ((.reviewing_by // null) != null and (.review_lease_expires // null) != null and .review_lease_expires < "'"$now"'")
            )
        )] |
        sort_by(.priority) |
        .[0].id // ""
    ' "$STATE" 2>/dev/null
}

# Find highest-priority claimable task (UNCLAIMED, REJECTED, or INTEGRATION_FAILED)
# A task is claimable if: status in (UNCLAIMED, REJECTED, INTEGRATION_FAILED) AND deps satisfied
find_claimable_task() {
    yq -r '
        # Get list of MERGED task IDs for dependency checking
        (.tasks | map(select(.status == "MERGED") | .id)) as $merged |
        # Filter to claimable tasks where all depends_on are in $merged
        [.tasks[] | select(
            (.status == "UNCLAIMED" or .status == "REJECTED" or .status == "INTEGRATION_FAILED") and
            (((.depends_on // []) - $merged) | length == 0)
        )] |
        sort_by(.priority) |
        .[0].id // ""
    ' "$STATE" 2>/dev/null
}

# Coder: Claim highest-priority claimable task (UNCLAIMED, REJECTED, or INTEGRATION_FAILED)
# Sets CLAIMED_TASK_ID and CLAIMED_WORKTREE on success
# Returns 0 on success, 1 on failure
claim_coder_task() {
    local task_id
    task_id=$(find_claimable_task)

    if [ -z "$task_id" ] || [ "$task_id" = "null" ]; then
        echo "ERROR: No claimable tasks (UNCLAIMED with dependencies satisfied)"
        return 1
    fi

    echo "Claiming task $task_id for $LIZA_AGENT_ID..."

    if ! "$SCRIPT_DIR/liza-claim-task.sh" "$task_id" "$LIZA_AGENT_ID"; then
        echo "ERROR: Failed to claim task $task_id"
        return 1
    fi

    # Export for use in prompt
    CLAIMED_TASK_ID="$task_id"
    CLAIMED_WORKTREE=$(get_task_field "$task_id" "worktree")

    return 0
}

# Code Reviewer: Claim highest-priority reviewable task (no active review lease)
# Sets REVIEW_TASK_ID, REVIEW_WORKTREE, REVIEW_COMMIT on success
# Returns 0 on success, 1 on failure
claim_reviewer_task() {
    local task_id
    task_id=$(find_reviewable_task)

    if [ -z "$task_id" ] || [ "$task_id" = "null" ]; then
        # Normal condition: task was claimed by another reviewer between wait and claim
        return 1
    fi

    echo "Claiming task $task_id for review by $LIZA_AGENT_ID..."

    # Update state to mark reviewer is reviewing this task
    local now
    now=$(iso_timestamp)
    local lease_seconds
    lease_seconds=$(get_config lease_duration 1800)
    local lease
    lease=$(iso_timestamp_offset "+${lease_seconds} seconds")

    locked_yq "
        (.tasks[] | select(.id == \"$task_id\")).reviewing_by = \"$LIZA_AGENT_ID\" |
        (.tasks[] | select(.id == \"$task_id\")).review_lease_expires = \"$lease\" |
        .agents.\"$LIZA_AGENT_ID\".status = \"REVIEWING\" |
        .agents.\"$LIZA_AGENT_ID\".current_task = \"$task_id\" |
        .agents.\"$LIZA_AGENT_ID\".lease_expires = \"$lease\" |
        .agents.\"$LIZA_AGENT_ID\".heartbeat = \"$now\"
    "

    # Export for use in prompt
    REVIEW_TASK_ID="$task_id"
    REVIEW_WORKTREE=$(get_task_field "$task_id" "worktree")
    REVIEW_COMMIT=$(get_task_field "$task_id" "review_commit")

    echo "REVIEWING: $task_id by $LIZA_AGENT_ID"
    echo "  worktree: $REVIEW_WORKTREE"
    echo "  commit: $REVIEW_COMMIT"

    return 0
}

# --- Main Loop ---

while true; do
    # Check for ABORT
    if check_abort; then
        echo "ABORT file detected. Supervisor exiting."
        exit 0
    fi

    # Check for PAUSE or CHECKPOINT
    while [ -f "$LIZA_DIR/PAUSE" ] || [ -f "$LIZA_DIR/CHECKPOINT" ]; do
        echo "PAUSED/CHECKPOINT. Waiting..."
        sleep 5
    done

    # Supervisor handles merge for APPROVED tasks (avoids agent permission prompts)
    if [ "$ROLE" = "code-reviewer" ]; then
        approved_pending=$(count_tasks ".status == \"APPROVED\" and .approved_by == \"$LIZA_AGENT_ID\" and .merge_commit == null")
        if [ "$approved_pending" -gt 0 ]; then
            handle_approved_merges
        fi
    fi

    # Wait for work before starting agent (saves API calls)
    if ! wait_for_work; then
        echo "No work available or pending. Supervisor exiting."
        exit 0
    fi

    # For coders: claim a task before starting the agent
    CLAIMED_TASK_ID=""
    CLAIMED_WORKTREE=""
    if [ "$ROLE" = "coder" ]; then
        if ! claim_coder_task; then
            echo "Failed to claim task. Retrying in ${CRASH_DELAY}s..."
            sleep "$CRASH_DELAY"
            continue
        fi
    fi

    # For code-reviewers: claim a review task before starting the agent
    REVIEW_TASK_ID=""
    REVIEW_WORKTREE=""
    REVIEW_COMMIT=""
    if [ "$ROLE" = "code-reviewer" ]; then
        if ! claim_reviewer_task; then
            # No task to claim (race condition or none available) - go back to waiting
            continue
        fi
    fi

    # Build bootstrap prompt
    PROMPT_FILE=$(mktemp)
    build_base_prompt > "$PROMPT_FILE"

    # Add role-specific context
    if [ "$ROLE" = "coder" ] && [ -n "$CLAIMED_TASK_ID" ]; then
        build_coder_context >> "$PROMPT_FILE"
    fi
    if [ "$ROLE" = "code-reviewer" ] && [ -n "$REVIEW_TASK_ID" ]; then
        build_reviewer_context >> "$PROMPT_FILE"
    fi
    if [ "$ROLE" = "planner" ]; then
        build_planner_context >> "$PROMPT_FILE"
    fi
    if [ -n "$INITIAL_TASK" ]; then
        echo -e "\nRESUME: Task $INITIAL_TASK" >> "$PROMPT_FILE"
    fi

    echo "Starting $ROLE agent ($LIZA_AGENT_ID) via $CLI..."
    # Run agent CLI with prompt from file, then clean up
    # Add liza specs directory to allowed paths
    set +e
    prompt_dir="$LIZA_DIR/agent-prompts"
    mkdir -p "$prompt_dir"
    prompt_ts=$(iso_timestamp | tr ':' '-')
    prompt_log="$prompt_dir/${LIZA_AGENT_ID}-${prompt_ts}.txt"
    cp "$PROMPT_FILE" "$prompt_log"
    echo "Prompt saved: $prompt_log"

    # Start background heartbeat to extend lease while agent runs
    HEARTBEAT_INTERVAL=$(get_config heartbeat_interval 60)
    LEASE_DURATION=$(get_config lease_duration 1800)
    (
        while true; do
            sleep "$HEARTBEAT_INTERVAL"
            now=$(iso_timestamp)
            new_lease=$(iso_timestamp_offset "+${LEASE_DURATION} seconds")
            locked_yq "
                .agents.\"$LIZA_AGENT_ID\".heartbeat = \"$now\" |
                .agents.\"$LIZA_AGENT_ID\".lease_expires = \"$new_lease\"
            " 2>/dev/null || true
        done
    ) &
    HEARTBEAT_PID=$!

    case "$CLI" in
        claude)
            LIZA_AGENT_ID="$LIZA_AGENT_ID" claude -p "$(cat "$PROMPT_FILE")"
            ;;
        codex)
            LIZA_AGENT_ID="$LIZA_AGENT_ID" codex exec "$(cat "$PROMPT_FILE")"
            ;;
        gemini)
            LIZA_AGENT_ID="$LIZA_AGENT_ID" gemini -p "$(cat "$PROMPT_FILE")"
            ;;
        mistral)
            LIZA_AGENT_ID="$LIZA_AGENT_ID" vibe -p "$(cat "$PROMPT_FILE")"
            ;;
    esac
    EXIT_CODE=$?

    # Stop heartbeat background process
    kill "$HEARTBEAT_PID" 2>/dev/null; wait "$HEARTBEAT_PID" 2>/dev/null || true

    rm -f "$PROMPT_FILE"
    set -e

    # Clear initial task after first run
    INITIAL_TASK=""

    case $EXIT_CODE in
        0)
            # Agent completed normally
            echo "Agent completed."
            echo "Checking for more work..."
            ;;
        42)
            echo "Agent aborted gracefully (code 42). Restarting in ${RESTART_DELAY}s..."
            sleep "$RESTART_DELAY"
            ;;
        *)
            echo "Agent crashed (code $EXIT_CODE). Restarting in ${CRASH_DELAY}s..."
            sleep "$CRASH_DELAY"
            ;;
    esac
done
