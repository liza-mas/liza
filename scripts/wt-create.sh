#!/bin/bash
# Create worktree for task
# Usage: wt-create.sh [--fresh] <task-id>
#   --fresh: Delete existing worktree before creating (for reassignment)

set -euo pipefail

# --- Arguments ---
fresh=false
if [ "${1:-}" = "--fresh" ]; then
    fresh=true
    shift
fi

readonly TASK_ID="$1"

# --- Path Setup ---
PROJECT_ROOT=$(git rev-parse --show-toplevel)
readonly PROJECT_ROOT
readonly STATE="$PROJECT_ROOT/.liza/state.yaml"
readonly STATE_LOCK="$STATE.lock"
readonly WORKTREE_REL=".worktrees/$TASK_ID"
readonly WORKTREE_DIR="$PROJECT_ROOT/$WORKTREE_REL"

# --- Validation ---

status=$(yq ".tasks[] | select(.id == \"$TASK_ID\") | .status" "$STATE")
if [ "$status" != "CLAIMED" ]; then
    echo "Error: Task $TASK_ID is not CLAIMED (status: $status)" >&2
    exit 1
fi

integration_branch=$(yq '.config.integration_branch' "$STATE")

# --- Handle Existing Worktree ---

if [ -d "$WORKTREE_DIR" ]; then
    if [ "$fresh" = true ]; then
        echo "Reassignment: deleting existing worktree"
        git worktree remove --force "$WORKTREE_DIR" 2>/dev/null || rm -rf "$WORKTREE_DIR"
        git branch -D "task/$TASK_ID" 2>/dev/null || true
    else
        echo "Worktree already exists: $WORKTREE_DIR"
        exit 0
    fi
fi

# --- Create Worktree ---

mkdir -p "$PROJECT_ROOT/.worktrees"

# Record base_commit before creating worktree (for drift tracking)
base_commit=$(git rev-parse --short "$integration_branch")

# Create worktree from integration branch
git worktree add "$WORKTREE_DIR" "$integration_branch" -b "task/$TASK_ID"

# Record base_commit (worktree path already set during atomic claim)
flock -x "$STATE_LOCK" yq -i "(.tasks[] | select(.id == \"$TASK_ID\")).base_commit = \"$base_commit\"" "$STATE"

echo "Created worktree: $WORKTREE_DIR"
