#!/bin/bash
# Claim a task for a coder agent
# Usage: liza-claim-task.sh <task-id> <agent-id>
#
# All validation happens under lock to prevent TOCTOU races.
# Sets all required CLAIMED fields atomically:
# - status, assigned_to, worktree, base_commit, lease_expires
# Creates worktree and adds history entry.

set -euo pipefail

# --- Arguments ---
readonly TASK_ID="${1:-}"
readonly AGENT_ID="${2:-}"

if [ -z "$TASK_ID" ] || [ -z "$AGENT_ID" ]; then
    echo "Usage: liza-claim-task.sh <task-id> <agent-id>" >&2
    exit 1
fi

# --- Path Setup ---
PROJECT_ROOT=$(git rev-parse --show-toplevel)
readonly PROJECT_ROOT
readonly STATE="$PROJECT_ROOT/.liza/state.yaml"
readonly STATE_LOCK="$STATE.lock"
readonly WORKTREE_DIR=".worktrees/$TASK_ID"

# --- Helper Functions ---

die() {
    echo "ERROR: $*" >&2
    exit 1
}

iso_timestamp() {
    date -u +%Y-%m-%dT%H:%M:%SZ
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

lease_duration=$(yq '.config.lease_duration // 300' "$STATE" 2>/dev/null || echo 300)
lease_expires=$(date -u -d "+${lease_duration} seconds" +%Y-%m-%dT%H:%M:%SZ)
now=$(iso_timestamp)

# --- Phase 1: Validate Under Lock (no state mutation) ---
#
# All checks that depend on mutable state must happen inside the lock.
# Exit codes: 0=can proceed, 1=task not found, 2=not UNCLAIMED, 3=unmet deps, 4=agent busy

validate_result=$(flock -x "$STATE_LOCK" -c "
    # Check task exists
    task_status=\$(yq -r '.tasks[] | select(.id == \"$TASK_ID\") | .status' '$STATE' 2>/dev/null)
    if [ -z \"\$task_status\" ] || [ \"\$task_status\" == 'null' ]; then
        echo 'Task not found'
        exit 1
    fi

    # Check task is UNCLAIMED
    if [ \"\$task_status\" != 'UNCLAIMED' ]; then
        echo \"Task is \$task_status\"
        exit 2
    fi

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

    # Check agent isn't already working on another task
    agent_task=\$(yq -r '.agents.\"$AGENT_ID\".current_task // \"\"' '$STATE' 2>/dev/null)
    if [ -n \"\$agent_task\" ] && [ \"\$agent_task\" != 'null' ]; then
        echo \"Agent busy with \$agent_task\"
        exit 4
    fi

    echo 'OK'
    exit 0
") || validate_exit=$?

validate_exit=${validate_exit:-0}

case $validate_exit in
    0) ;; # Validation passed - proceed to worktree creation
    1) die "Task $TASK_ID not found" ;;
    2) die "Task $TASK_ID is not UNCLAIMED ($validate_result)" ;;
    3) die "Task $TASK_ID has unmet dependencies ($validate_result)" ;;
    4) die "Agent $AGENT_ID is already working ($validate_result)" ;;
    *) die "Validation failed: $validate_result" ;;
esac

# --- Phase 2: Create Worktree (outside lock) ---

branch_name="task/$TASK_ID"
worktree_created=false

if [ -d "$PROJECT_ROOT/$WORKTREE_DIR" ]; then
    echo "WARNING: Worktree $WORKTREE_DIR already exists, reusing"
else
    mkdir -p "$PROJECT_ROOT/.worktrees"
    if ! git worktree add "$PROJECT_ROOT/$WORKTREE_DIR" -b "$branch_name" "$base_commit" 2>/dev/null; then
        if ! git worktree add "$PROJECT_ROOT/$WORKTREE_DIR" "$branch_name" 2>/dev/null; then
            die "Failed to create worktree at $WORKTREE_DIR"
        fi
    fi
    worktree_created=true
fi

# --- Phase 3: Re-validate and Commit Under Lock ---
#
# State may have changed while we created the worktree. Re-check everything
# before committing the CLAIMED update. If re-validation fails, clean up worktree.

commit_result=$(flock -x "$STATE_LOCK" -c "
    # Re-check task is still UNCLAIMED
    task_status=\$(yq -r '.tasks[] | select(.id == \"$TASK_ID\") | .status' '$STATE' 2>/dev/null)
    if [ \"\$task_status\" != 'UNCLAIMED' ]; then
        echo \"Task is now \$task_status\"
        exit 2
    fi

    # Re-check dependencies (could have changed)
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

    # Re-check agent availability
    agent_task=\$(yq -r '.agents.\"$AGENT_ID\".current_task // \"\"' '$STATE' 2>/dev/null)
    if [ -n \"\$agent_task\" ] && [ \"\$agent_task\" != 'null' ]; then
        echo \"Agent now busy with \$agent_task\"
        exit 4
    fi

    # All checks passed - commit state update
    yq -i '
        (.tasks[] | select(.id == \"$TASK_ID\")) |= (
            .status = \"CLAIMED\" |
            .assigned_to = \"$AGENT_ID\" |
            .worktree = \"$WORKTREE_DIR\" |
            .base_commit = \"$base_commit\" |
            .lease_expires = \"$lease_expires\" |
            .iteration = ((.iteration // 0) + 1) |
            .history += [{\"time\": \"$now\", \"event\": \"claimed\", \"agent\": \"$AGENT_ID\"}]
        ) |
        .agents.\"$AGENT_ID\".status = \"WORKING\" |
        .agents.\"$AGENT_ID\".current_task = \"$TASK_ID\" |
        .agents.\"$AGENT_ID\".lease_expires = \"$lease_expires\" |
        .agents.\"$AGENT_ID\".heartbeat = \"$now\"
    ' '$STATE'

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

echo "CLAIMED: $TASK_ID by $AGENT_ID"
echo "  worktree: $WORKTREE_DIR"
echo "  base_commit: $base_commit"
echo "  lease_expires: $lease_expires"
