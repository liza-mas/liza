#!/bin/bash
# Agent supervisor - restarts agent on graceful abort
# Usage: LIZA_AGENT_ID=coder-1 liza-agent.sh coder [initial-task-id]

set -euo pipefail

# --- Configuration ---
readonly RESTART_DELAY=2
readonly CRASH_DELAY=5

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
LIZA_ROOT=$(dirname "$SCRIPT_DIR")
readonly LIZA_ROOT
readonly LIZA_SPECS="$LIZA_ROOT/specs"

# --- Helper Functions ---

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

# --- Prompt Builders ---

# Build base bootstrap prompt (outputs to stdout)
build_base_prompt() {
    local goal_desc
    goal_desc=$(yq -r '.goal.description' "$STATE" 2>/dev/null || echo "See specs/vision.md")
    local role_title
    role_title=$(echo "$ROLE" | tr '-' ' ' | sed 's/\b\w/\u&/g')

    cat << EOF
You are a Liza $ROLE agent. Agent ID: $LIZA_AGENT_ID

=== BOOTSTRAP CONTEXT ===
ROLE: $ROLE
SPECS_LOCATION: $LIZA_SPECS
PROJECT: $PROJECT_ROOT
BLACKBOARD: $STATE
GOAL: $goal_desc

Read these specs before acting:
- Role definition: $LIZA_SPECS/architecture/roles.md (section: $role_title)
- Task lifecycle: $LIZA_SPECS/protocols/task-lifecycle.md
- Blackboard schema: $LIZA_SPECS/architecture/blackboard-schema.md
- State machines: $LIZA_SPECS/architecture/state-machines.md

OPERATIONAL RULES:
- You have FULL read/write access to .liza/ directory - USE IT DIRECTLY
- Do NOT ask for permission textually - just use Edit/Write tools
- The human will see tool permission prompts from Claude Code if needed
- Work autonomously: read specs, execute protocol, write to blackboard
- Exit when your current work unit is complete (task implemented, review done, etc.)

HELPER SCRIPTS:
- ~/.claude/scripts/liza-validate.sh <state.yaml> — Validate blackboard state

FORBIDDEN:
- Do NOT attempt to claim tasks - the supervisor has already claimed your task
- Do NOT manually modify task status to CLAIMED
- Do NOT skip worktrees or "simplify" the protocol
- Do NOT make architecture decisions - follow the spec exactly

FIRST ACTIONS:
1. Read your role definition from roles.md
2. Read the current blackboard state: $STATE
3. Read specs/vision.md in the project for the goal details
4. Execute your role's protocol - write directly to the blackboard
EOF
}

# Build coder task context (outputs to stdout)
build_coder_context() {
    local task_desc
    task_desc=$(get_task_field "$CLAIMED_TASK_ID" "description")
    local task_done_when
    task_done_when=$(get_task_field "$CLAIMED_TASK_ID" "done_when")
    local task_scope
    task_scope=$(get_task_field "$CLAIMED_TASK_ID" "scope")

    cat << EOF

=== ASSIGNED TASK ===
TASK ID: $CLAIMED_TASK_ID
WORKTREE: $PROJECT_ROOT/$CLAIMED_WORKTREE
DESCRIPTION: $task_desc

DONE WHEN:
$task_done_when

SCOPE:
$task_scope

INSTRUCTIONS:
- The task is already CLAIMED for you. Do NOT run liza-claim-task.sh.
- Work ONLY in the worktree directory: cd $PROJECT_ROOT/$CLAIMED_WORKTREE
- TDD (code tasks): Write tests FIRST that verify done_when criteria, then implement until tests pass
- Tests are MANDATORY for code tasks — Code Reviewer will reject code without tests
- Exempt: doc-only, config-only, or spec-only tasks (no code = no tests required)
- Use the clean-code skill at the end of the implementation
- When complete, update task status to READY_FOR_REVIEW in the blackboard
- Add a history entry with timestamp and "submitted_for_review" event
EOF
}

# Build code-reviewer task context (outputs to stdout)
build_reviewer_context() {
    local task_desc
    task_desc=$(get_task_field "$REVIEW_TASK_ID" "description")
    local task_done_when
    task_done_when=$(get_task_field "$REVIEW_TASK_ID" "done_when")
    local task_assigned_to
    task_assigned_to=$(get_task_field "$REVIEW_TASK_ID" "assigned_to")

    cat << EOF

=== REVIEW TASK ===
TASK ID: $REVIEW_TASK_ID
WORKTREE: $PROJECT_ROOT/$REVIEW_WORKTREE
COMMIT TO REVIEW: $REVIEW_COMMIT
AUTHOR: $task_assigned_to
DESCRIPTION: $task_desc

DONE WHEN:
$task_done_when

INSTRUCTIONS:
- The task is already assigned to you for review.
- Check the commit: git -C $PROJECT_ROOT/$REVIEW_WORKTREE show $REVIEW_COMMIT. If not, fail fast to REJECTED.
- Review the code in the worktree $PROJECT_ROOT/$REVIEW_WORKTREE using the code-review skill
- If change touches specs/, introduces new abstractions, adds state/lifecycle, or spans 3+ modules: also apply systemic-thinking skill
- TDD ENFORCEMENT (code tasks): REJECT if tests are missing or don't cover done_when criteria
- Exempt: doc-only, config-only, or spec-only tasks (no code = no tests required)
- Verify the done_when criteria are met AND tests exercise those criteria (for code tasks)
- If APPROVED: set task status to APPROVED, then run: $SCRIPT_DIR/wt-merge.sh $REVIEW_TASK_ID (this merges and sets status to MERGED)
- If REJECTED: set task status to REJECTED, add rejection_reason field, add history entry
- Always update your agent status to IDLE when done
EOF
}

# Build planner context (outputs to stdout)
build_planner_context() {
    # Compute sprint state
    local total_tasks
    total_tasks=$(yq '.tasks | length' "$STATE" 2>/dev/null || echo 0)
    local merged
    merged=$(count_tasks '.status == "MERGED"')
    local blocked
    blocked=$(count_tasks '.status == "BLOCKED"')
    local integration_failed
    integration_failed=$(count_tasks '.status == "INTEGRATION_FAILED"')
    local in_progress
    in_progress=$(count_tasks '.status == "CLAIMED" or .status == "READY_FOR_REVIEW" or .status == "APPROVED"')
    local unclaimed
    unclaimed=$(count_tasks '.status == "UNCLAIMED"')
    local hypothesis_exhausted
    hypothesis_exhausted=$(yq '[.tasks[] | select(.failed_by != null and (.failed_by | length) >= 2)] | length' "$STATE" 2>/dev/null || echo 0)
    local immediate_discoveries
    immediate_discoveries=$(yq '[.discovered[] | select(.urgency == "immediate" and .converted_to_task == null)] | length' "$STATE" 2>/dev/null || echo 0)

    # Determine wake trigger
    local wake_trigger="UNKNOWN"
    if [ "$total_tasks" -eq 0 ]; then
        wake_trigger="INITIAL_PLANNING"
    elif [ "$blocked" -gt 0 ]; then
        wake_trigger="BLOCKED_TASKS"
    elif [ "$integration_failed" -gt 0 ]; then
        wake_trigger="INTEGRATION_FAILED"
    elif [ "$hypothesis_exhausted" -gt 0 ]; then
        wake_trigger="HYPOTHESIS_EXHAUSTED"
    elif [ "$immediate_discoveries" -gt 0 ]; then
        wake_trigger="IMMEDIATE_DISCOVERY"
    fi

    cat << EOF

=== PLANNING CONTEXT ===
WAKE TRIGGER: $wake_trigger

SPRINT STATE:
- Total tasks: $total_tasks
- Merged: $merged
- In progress: $in_progress
- Unclaimed: $unclaimed
- Blocked: $blocked
- Integration failed: $integration_failed
- Hypothesis exhausted: $hypothesis_exhausted
- Immediate discoveries: $immediate_discoveries

INSTRUCTIONS:
EOF

    # Context-specific instructions based on wake trigger
    case "$wake_trigger" in
        INITIAL_PLANNING)
            cat << 'EOF'
This is initial planning. Decompose the goal into tasks:

1. Read specs/vision.md thoroughly — understand the goal, constraints, success criteria

2. Identify the minimal set of tasks that achieve the goal

3. Analyze task dependencies:
   - Which tasks produce artifacts others need? (APIs, schemas, utilities)
   - Which tasks modify shared code that others will build on?
   - Can tasks run in parallel, or must they be sequential?
   - Draw the dependency graph mentally before writing tasks

4. For each task, define:
   - id: short kebab-case identifier (e.g., "add-auth-middleware")
   - description: what to build (1-2 sentences)
   - done_when: observable completion criteria (testable, specific)
   - scope: files/modules to touch, what's in/out of scope
   - priority: 1 (highest) to 5 (lowest)
   - depends_on: [task-ids] that must be MERGED before this task can be claimed
   - spec_ref: path to relevant spec section

5. TDD ENFORCEMENT (MANDATORY for code tasks):
   - Each code task MUST include its own tests — do NOT create separate "add tests" tasks
   - done_when criteria must be verifiable by tests the coder writes
   - Code Reviewer will reject code tasks without tests covering done_when
   - Exempt: documentation-only, config-only, or spec-only tasks (no code = no tests)
   - Rationale: Coder can't validate their work without tests; separate test tasks break TDD

6. Dependency guidelines:
   - depends_on: [] for tasks with no prerequisites (can start immediately)
   - depends_on: [task-a] for tasks that need task-a's output
   - Avoid long chains — prefer wide parallelism over deep sequences
   - If A depends on B depends on C, consider if A really needs C directly

7. Prefer small, independent tasks over large coupled ones
   - Each task = implementation + tests (not separate tasks)
   - A task is "small" if one coder can complete it in one session

8. Write tasks to blackboard with status: UNCLAIMED
EOF
            ;;
        BLOCKED_TASKS)
            cat << 'EOF'
Tasks are BLOCKED. Analyze and resolve:
1. Read blocked tasks from blackboard — understand blocker_reason
2. Determine if blocker is:
   - Missing dependency → create prerequisite task
   - Spec ambiguity → clarify spec, unblock task
   - External dependency → document, possibly supersede task
   - Wrong approach → supersede task, create alternative
3. Update blocked tasks: either unblock (status → UNCLAIMED) or supersede
4. Log decisions in task history
EOF
            ;;
        INTEGRATION_FAILED)
            cat << 'EOF'
Integration failed. Diagnose and plan fix:
1. Read INTEGRATION_FAILED tasks — check test output logs
2. Determine failure cause:
   - Merge conflict → task may need rebase, reassign
   - Test failure → create fix task or reassign original
   - Environment issue → document, create setup task
3. Either reassign task (status → UNCLAIMED) or create follow-up task
4. Consider if failure reveals spec gap — update specs if needed
EOF
            ;;
        HYPOTHESIS_EXHAUSTED)
            cat << 'EOF'
Multiple coders failed on same task. Re-evaluate:
1. Read task history — understand what was tried and why it failed
2. Determine if:
   - Task is impossible as specified → revise or supersede
   - Missing context/docs → add to task description
   - Needs different approach → update scope/guidance
   - Spec is wrong → fix spec first
3. Either update task and reassign, or supersede with new approach
4. Consider breaking into smaller tasks if too complex
EOF
            ;;
        IMMEDIATE_DISCOVERY)
            cat << 'EOF'
Urgent discoveries need attention:
1. Read discovered[] entries with urgency=immediate
2. For each, decide:
   - Convert to task → create task, set converted_to_task field
   - Defer → change urgency to "deferred" with rationale
   - Reject → document why in discovered entry
3. Prioritize new tasks appropriately (may be high priority)
4. Check if discoveries invalidate existing tasks
EOF
            ;;
    esac
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
    max_wait=$(get_config coder_max_wait 300)
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
    max_wait=$(get_config reviewer_max_wait 300)
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

    echo "Starting $ROLE agent ($LIZA_AGENT_ID)..."
    # Run Claude Code with prompt from file, then clean up
    # Add liza specs directory to allowed paths
    set +e
    LIZA_AGENT_ID="$LIZA_AGENT_ID" claude --add-dir "$LIZA_ROOT" -p "$(cat "$PROMPT_FILE")"
    EXIT_CODE=$?
    rm -f "$PROMPT_FILE"
    set -e

    # Clear initial task after first run
    INITIAL_TASK=""

    case $EXIT_CODE in
        0)
            # Agent completed normally — loop will check for work at top
            echo "Agent completed. Checking for more work..."
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
