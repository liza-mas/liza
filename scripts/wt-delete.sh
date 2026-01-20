#!/bin/bash
# Delete abandoned/superseded/blocked task worktree
# Usage: wt-delete.sh <task-id>
#
# SAFETY: Only allows deletion for BLOCKED, ABANDONED, SUPERSEDED tasks
# to prevent accidental destruction of in-progress work.

set -euo pipefail

# --- Arguments ---
readonly TASK_ID="$1"

# --- Path Setup ---
PROJECT_ROOT=$(git rev-parse --show-toplevel)
readonly PROJECT_ROOT
readonly STATE="$PROJECT_ROOT/.liza/state.yaml"
readonly STATE_LOCK="$STATE.lock"

# --- Helper Functions ---

die() {
    echo "Error: $*" >&2
    exit 1
}

# --- Validation ---

status=$(yq ".tasks[] | select(.id == \"$TASK_ID\") | .status" "$STATE")
worktree_rel=$(yq ".tasks[] | select(.id == \"$TASK_ID\") | .worktree" "$STATE")

if [ -z "$status" ] || [ "$status" == "null" ]; then
    die "Task $TASK_ID not found"
fi

# Only allow deletion for safe statuses
case "$status" in
    BLOCKED|ABANDONED|SUPERSEDED)
        # Safe to delete
        ;;
    MERGED)
        echo "Warning: Task $TASK_ID is MERGED — worktree should already be cleaned"
        ;;
    *)
        die "Cannot delete worktree for task $TASK_ID (status: $status)
Deletion only allowed for: BLOCKED, ABANDONED, SUPERSEDED
If task is CLAIMED, the Coder may be actively working in this worktree."
        ;;
esac

if [ -z "$worktree_rel" ] || [ "$worktree_rel" == "null" ]; then
    echo "No worktree for task $TASK_ID"
    exit 0
fi

# --- Delete Worktree ---

worktree_abs="$PROJECT_ROOT/$worktree_rel"

git worktree remove "$worktree_abs" --force
git branch -D "task/$TASK_ID" 2>/dev/null || true

# Update blackboard
flock -x "$STATE_LOCK" yq -i "(.tasks[] | select(.id == \"$TASK_ID\")).worktree = null" "$STATE"

echo "Deleted worktree for $TASK_ID (was $status)"
