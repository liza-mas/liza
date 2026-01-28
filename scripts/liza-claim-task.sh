#!/bin/bash
# Claim a task for a coder agent
# Usage: liza-claim-task.sh <task-id> <agent-id>
#
# Supports claiming from multiple source states:
# - UNCLAIMED: normal new claim
# - REJECTED: re-claim (same coder preserves worktree, different coder gets fresh)
# - INTEGRATION_FAILED: any coder can claim (worktree preserved for conflict resolution)
#
# All validation happens under lock to prevent TOCTOU races.
# Creates/reuses worktree as appropriate for the transition.

set -euo pipefail

# --- Arguments ---
readonly TASK_ID="${1:-}"
readonly AGENT_ID="${2:-}"

if [ -z "$TASK_ID" ] || [ -z "$AGENT_ID" ]; then
    echo "Usage: liza-claim-task.sh <task-id> <agent-id>" >&2
    exit 1
fi

# --- Path Setup ---
source "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")/liza-common.sh"
PROJECT_ROOT=$(get_project_root)
readonly PROJECT_ROOT
readonly STATE="$PROJECT_ROOT/.liza/state.yaml"
readonly STATE_LOCK="$STATE.lock"
readonly WORKTREE_DIR=".worktrees/$TASK_ID"

# --- Helper Functions ---

die() {
    echo "ERROR: $*" >&2
    exit 1
}

# --- Prepare Values (outside lock - these don't depend on state) ---

integration_branch=$(yq -r '.config.integration_branch // "integration"' "$STATE" 2>/dev/null)

if git rev-parse --verify "$integration_branch" >/dev/null 2>&1; then
    base_commit=$(git rev-parse "$integration_branch")
elif git rev-parse --verify main >/dev/null 2>&1; then
    base_commit=$(git rev-parse main)
elif git rev-parse --verify master >/dev/null 2>&1; then
    base_commit=$(git rev-parse master)
else
    base_commit=$(git rev-parse HEAD)
fi

lease_duration=$(yq '.config.lease_duration // 1800' "$STATE" 2>/dev/null || echo 1800)
lease_expires=$(date -u -d "+${lease_duration} seconds" +%Y-%m-%dT%H:%M:%SZ)
now=$(iso_timestamp)

# --- Phase 1: Validate Under Lock (no state mutation) ---
#
# All checks that depend on mutable state must happen inside the lock.
# Returns: "OK|<status>|<previous_assignee>" on success
# Exit codes: 0=can proceed, 1=task not found, 2=invalid status, 3=unmet deps, 4=agent busy

validate_result=$(flock -x "$STATE_LOCK" -c "
    # Check task exists and get current state
    task_status=\$(yq -r '.tasks[] | select(.id == \"$TASK_ID\") | .status' '$STATE' 2>/dev/null)
    if [ -z \"\$task_status\" ] || [ \"\$task_status\" = 'null' ]; then
        echo 'Task not found'
        exit 1
    fi

    # Get previous assignee (for REJECTED/INTEGRATION_FAILED)
    prev_assignee=\$(yq -r '.tasks[] | select(.id == \"$TASK_ID\") | .assigned_to // \"\"' '$STATE' 2>/dev/null)

    # Check task status is claimable
    case \"\$task_status\" in
        UNCLAIMED)
            # Check dependencies are satisfied
            unmet=\$(yq -r '
                (.tasks[] | select(.id == \"$TASK_ID\") | .depends_on // []) as \$deps |
                if (\$deps | length) == 0 then
                    \"\"
                else
                    [.tasks[] | select(.id as \$id | \$deps | contains([\$id])) | select(.status != \"MERGED\") | .id] | join(\", \")
                end
            ' '$STATE' 2>/dev/null)
            if [ -n \"\$unmet\" ] && [ \"\$unmet\" != 'null' ]; then
                echo \"Unmet dependencies: \$unmet\"
                exit 3
            fi
            ;;
        REJECTED|INTEGRATION_FAILED)
            # These are valid source states - no dependency check needed
            ;;
        *)
            echo \"Task is \$task_status (not UNCLAIMED, REJECTED, or INTEGRATION_FAILED)\"
            exit 2
            ;;
    esac

    # Check agent isn't already working on another task (allow reclaiming same task)
    agent_task=\$(yq -r '.agents.\"$AGENT_ID\".current_task // \"\"' '$STATE' 2>/dev/null)
    if [ -n \"\$agent_task\" ] && [ \"\$agent_task\" != 'null' ] && [ \"\$agent_task\" != '$TASK_ID' ]; then
        echo \"Agent busy with \$agent_task\"
        exit 4
    fi

    # Return status and previous assignee for Phase 2 decisions
    echo \"OK|\$task_status|\$prev_assignee\"
    exit 0
") || validate_exit=$?

validate_exit=${validate_exit:-0}

case $validate_exit in
    0) ;; # Validation passed - proceed to worktree handling
    1) die "Task $TASK_ID not found" ;;
    2) die "Task $TASK_ID: $validate_result" ;;
    3) die "Task $TASK_ID has unmet dependencies ($validate_result)" ;;
    4) die "Agent $AGENT_ID is already working ($validate_result)" ;;
    *) die "Validation failed: $validate_result" ;;
esac

# Parse validation result
IFS='|' read -r _ task_status prev_assignee <<< "$validate_result"

# --- Phase 2: Handle Worktree (outside lock) ---
#
# Worktree handling depends on source state and assignee:
# - UNCLAIMED: create new worktree
# - REJECTED + same coder: preserve existing worktree
# - REJECTED + different coder: delete existing, create fresh
# - INTEGRATION_FAILED: preserve existing worktree (need to see failed merge state)

branch_name="task/$TASK_ID"
worktree_created=false
worktree_deleted=false

case "$task_status" in
    UNCLAIMED)
        # New claim - create worktree
        if [ -d "$PROJECT_ROOT/$WORKTREE_DIR" ]; then
            echo "WARNING: Worktree $WORKTREE_DIR already exists for UNCLAIMED task, recreating"
            git worktree remove "$PROJECT_ROOT/$WORKTREE_DIR" --force 2>/dev/null || true
            git branch -D "$branch_name" 2>/dev/null || true
        fi
        mkdir -p "$PROJECT_ROOT/.worktrees"
        if ! git worktree add "$PROJECT_ROOT/$WORKTREE_DIR" -b "$branch_name" "$base_commit" 2>/dev/null; then
            if ! git worktree add "$PROJECT_ROOT/$WORKTREE_DIR" "$branch_name" 2>/dev/null; then
                die "Failed to create worktree at $WORKTREE_DIR"
            fi
        fi
        worktree_created=true
        ;;

    REJECTED)
        if [ "$prev_assignee" = "$AGENT_ID" ]; then
            # Same coder re-claiming - preserve worktree
            echo "Same coder re-claiming REJECTED task, preserving worktree"
            if [ ! -d "$PROJECT_ROOT/$WORKTREE_DIR" ]; then
                die "Worktree $WORKTREE_DIR missing for REJECTED task (same coder)"
            fi
        else
            # Different coder - delete and recreate fresh worktree
            echo "Different coder claiming REJECTED task, recreating worktree"
            if [ -d "$PROJECT_ROOT/$WORKTREE_DIR" ]; then
                git worktree remove "$PROJECT_ROOT/$WORKTREE_DIR" --force 2>/dev/null || true
                git branch -D "$branch_name" 2>/dev/null || true
                worktree_deleted=true
            fi
            mkdir -p "$PROJECT_ROOT/.worktrees"
            if ! git worktree add "$PROJECT_ROOT/$WORKTREE_DIR" -b "$branch_name" "$base_commit" 2>/dev/null; then
                if ! git worktree add "$PROJECT_ROOT/$WORKTREE_DIR" "$branch_name" 2>/dev/null; then
                    die "Failed to create worktree at $WORKTREE_DIR"
                fi
            fi
            worktree_created=true
        fi
        ;;

    INTEGRATION_FAILED)
        # Preserve worktree for conflict resolution
        echo "Claiming INTEGRATION_FAILED task, preserving worktree for conflict resolution"
        if [ ! -d "$PROJECT_ROOT/$WORKTREE_DIR" ]; then
            die "Worktree $WORKTREE_DIR missing for INTEGRATION_FAILED task"
        fi
        ;;
esac

# --- Phase 3: Re-validate and Commit Under Lock ---
#
# State may have changed while we handled the worktree. Re-check everything
# before committing the CLAIMED update. If re-validation fails, clean up worktree.

commit_result=$(flock -x "$STATE_LOCK" -c "
    # Re-check task status matches expected
    current_status=\$(yq -r '.tasks[] | select(.id == \"$TASK_ID\") | .status' '$STATE' 2>/dev/null)
    if [ \"\$current_status\" != '$task_status' ]; then
        echo \"Task status changed from $task_status to \$current_status\"
        exit 2
    fi

    # For UNCLAIMED: re-check dependencies
    if [ '$task_status' = 'UNCLAIMED' ]; then
        unmet=\$(yq -r '
            (.tasks[] | select(.id == \"$TASK_ID\") | .depends_on // []) as \$deps |
            if (\$deps | length) == 0 then
                \"\"
            else
                [.tasks[] | select(.id as \$id | \$deps | contains([\$id])) | select(.status != \"MERGED\") | .id] | join(\", \")
            end
        ' '$STATE' 2>/dev/null)
        if [ -n \"\$unmet\" ] && [ \"\$unmet\" != 'null' ]; then
            echo \"Dependencies changed: \$unmet\"
            exit 3
        fi
    fi

    # Re-check agent availability (allow reclaiming same task)
    agent_task=\$(yq -r '.agents.\"$AGENT_ID\".current_task // \"\"' '$STATE' 2>/dev/null)
    if [ -n \"\$agent_task\" ] && [ \"\$agent_task\" != 'null' ] && [ \"\$agent_task\" != '$TASK_ID' ]; then
        echo \"Agent now busy with \$agent_task\"
        exit 4
    fi

    # Build event description
    event='claimed'
    if [ '$task_status' = 'REJECTED' ]; then
        if [ '$prev_assignee' = '$AGENT_ID' ]; then
            event='reclaimed_after_rejection'
        else
            event='reassigned_after_rejection'
        fi
    elif [ '$task_status' = 'INTEGRATION_FAILED' ]; then
        event='claimed_for_integration_fix'
    fi

    # All checks passed - commit state update
    # Different updates based on source state
    if [ '$task_status' = 'INTEGRATION_FAILED' ]; then
        # Set integration_fix flag
        yq -i '
            (.tasks[] | select(.id == \"$TASK_ID\")) |= (
                .status = \"CLAIMED\" |
                .assigned_to = \"$AGENT_ID\" |
                .lease_expires = \"$lease_expires\" |
                .integration_fix = true |
                .iteration = ((.iteration // 0) + 1) |
                .history += [{\"time\": \"$now\", \"event\": \"'\$event'\", \"agent\": \"$AGENT_ID\"}]
            ) |
            .agents.\"$AGENT_ID\".status = \"WORKING\" |
            .agents.\"$AGENT_ID\".current_task = \"$TASK_ID\" |
            .agents.\"$AGENT_ID\".lease_expires = \"$lease_expires\" |
            .agents.\"$AGENT_ID\".heartbeat = \"$now\"
        ' '$STATE'
    elif [ '$task_status' = 'REJECTED' ] && [ '$prev_assignee' != '$AGENT_ID' ]; then
        # Different coder: reset review_cycles_current, update base_commit
        yq -i '
            (.tasks[] | select(.id == \"$TASK_ID\")) |= (
                .status = \"CLAIMED\" |
                .assigned_to = \"$AGENT_ID\" |
                .worktree = \"$WORKTREE_DIR\" |
                .base_commit = \"$base_commit\" |
                .lease_expires = \"$lease_expires\" |
                .review_cycles_current = 0 |
                .iteration = ((.iteration // 0) + 1) |
                .history += [{\"time\": \"$now\", \"event\": \"'\$event'\", \"agent\": \"$AGENT_ID\", \"previous_assignee\": \"$prev_assignee\"}]
            ) |
            .agents.\"$AGENT_ID\".status = \"WORKING\" |
            .agents.\"$AGENT_ID\".current_task = \"$TASK_ID\" |
            .agents.\"$AGENT_ID\".lease_expires = \"$lease_expires\" |
            .agents.\"$AGENT_ID\".heartbeat = \"$now\"
        ' '$STATE'
    elif [ '$task_status' = 'REJECTED' ] && [ '$prev_assignee' = '$AGENT_ID' ]; then
        # REJECTED same coder: preserve base_commit (worktree preserved, drift metrics unchanged)
        yq -i '
            (.tasks[] | select(.id == \"$TASK_ID\")) |= (
                .status = \"CLAIMED\" |
                .lease_expires = \"$lease_expires\" |
                .iteration = ((.iteration // 0) + 1) |
                .history += [{\"time\": \"$now\", \"event\": \"'\$event'\", \"agent\": \"$AGENT_ID\"}]
            ) |
            .agents.\"$AGENT_ID\".status = \"WORKING\" |
            .agents.\"$AGENT_ID\".current_task = \"$TASK_ID\" |
            .agents.\"$AGENT_ID\".lease_expires = \"$lease_expires\" |
            .agents.\"$AGENT_ID\".heartbeat = \"$now\"
        ' '$STATE'
    else
        # UNCLAIMED: new claim with fresh worktree and base_commit
        yq -i '
            (.tasks[] | select(.id == \"$TASK_ID\")) |= (
                .status = \"CLAIMED\" |
                .assigned_to = \"$AGENT_ID\" |
                .worktree = \"$WORKTREE_DIR\" |
                .base_commit = \"$base_commit\" |
                .lease_expires = \"$lease_expires\" |
                .iteration = ((.iteration // 0) + 1) |
                .history += [{\"time\": \"$now\", \"event\": \"'\$event'\", \"agent\": \"$AGENT_ID\"}]
            ) |
            .agents.\"$AGENT_ID\".status = \"WORKING\" |
            .agents.\"$AGENT_ID\".current_task = \"$TASK_ID\" |
            .agents.\"$AGENT_ID\".lease_expires = \"$lease_expires\" |
            .agents.\"$AGENT_ID\".heartbeat = \"$now\"
        ' '$STATE'
    fi

    echo 'OK'
    exit 0
") || commit_exit=$?

commit_exit=${commit_exit:-0}

# --- Cleanup on Commit Failure ---

if [ "$commit_exit" -ne 0 ]; then
    if [ "$worktree_created" = true ]; then
        echo "Cleaning up worktree after failed commit..."
        git worktree remove "$PROJECT_ROOT/$WORKTREE_DIR" --force 2>/dev/null || true
        git branch -D "$branch_name" 2>/dev/null || true
    fi
    case $commit_exit in
        2) die "Race condition: task $TASK_ID status changed ($commit_result)" ;;
        3) die "Race condition: task $TASK_ID dependencies changed ($commit_result)" ;;
        4) die "Race condition: agent $AGENT_ID became busy ($commit_result)" ;;
        *) die "Commit failed: $commit_result" ;;
    esac
fi

echo "CLAIMED: $TASK_ID by $AGENT_ID (from $task_status)"
echo "  worktree: $WORKTREE_DIR"
echo "  base_commit: $base_commit"
echo "  lease_expires: $lease_expires"
if [ "$task_status" = "INTEGRATION_FAILED" ]; then
    echo "  integration_fix: true"
fi
if [ "$task_status" = "REJECTED" ] && [ "$prev_assignee" != "$AGENT_ID" ]; then
    echo "  previous_assignee: $prev_assignee (worktree recreated fresh)"
fi
