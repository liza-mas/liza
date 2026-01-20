#!/bin/bash
# Merge approved task worktree to integration (Code Reviewer-only)
# Usage: wt-merge.sh <task-id>

set -euo pipefail

# --- Arguments ---
readonly TASK_ID="$1"

# --- Path Setup ---
PROJECT_ROOT=$(git rev-parse --show-toplevel)
readonly PROJECT_ROOT
readonly STATE="$PROJECT_ROOT/.liza/state.yaml"
readonly STATE_LOCK="$STATE.lock"
readonly LOG="$PROJECT_ROOT/.liza/log.yaml"

# --- Helper Functions ---

die() {
    local code=1
    if [[ "$1" =~ ^[0-9]+$ ]]; then
        code="$1"
        shift
    fi
    echo "Error: $*" >&2
    exit "$code"
}

iso_timestamp() {
    date -u +%Y-%m-%dT%H:%M:%SZ
}

get_task_field() {
    local field="$1"
    yq ".tasks[] | select(.id == \"$TASK_ID\") | .$field" "$STATE"
}

locked_yq() {
    flock -x "$STATE_LOCK" yq -i "$1" "$STATE"
}

# --- Load Task Data ---

worktree_rel=$(get_task_field "worktree")
integration_branch=$(yq '.config.integration_branch' "$STATE")
review_commit=$(get_task_field "review_commit")

if [ -z "$worktree_rel" ] || [ "$worktree_rel" == "null" ]; then
    die "No worktree for task $TASK_ID"
fi

worktree_dir="$PROJECT_ROOT/$worktree_rel"

# --- Validate Caller ---

if [ -z "${LIZA_AGENT_ID:-}" ]; then
    die "LIZA_AGENT_ID not set. Merge requires Code Reviewer identity."
fi
if ! [[ "$LIZA_AGENT_ID" =~ ^code-reviewer-[0-9]+$ ]]; then
    die "Merge requires Code Reviewer. Caller is $LIZA_AGENT_ID"
fi

# --- Validate Task Status ---

status=$(get_task_field "status")
if [ "$status" != "APPROVED" ]; then
    die "Task $TASK_ID is not APPROVED (status: $status)"
fi

# --- Verify Commit SHA ---

actual_commit=$(git -C "$worktree_dir" rev-parse HEAD | cut -c1-7)
if [ "$actual_commit" != "$review_commit" ]; then
    die "Worktree HEAD ($actual_commit) != review_commit ($review_commit)
Worktree may have been modified after review."
fi

# --- Calculate Drift ---

base_commit=$(get_task_field "base_commit")
drift_commits=0
if [ -n "$base_commit" ] && [ "$base_commit" != "null" ]; then
    git fetch origin "$integration_branch" 2>/dev/null || true
    drift_commits=$(git rev-list --count "$base_commit".."$integration_branch" 2>/dev/null || echo 0)
    if [ "$drift_commits" -gt 0 ]; then
        echo "Note: Integration branch has $drift_commits commit(s) since task base — drift detected"
    fi
fi

# --- Attempt Merge ---

cd "$PROJECT_ROOT"
git checkout "$integration_branch"

timestamp=$(iso_timestamp)

if git merge --ff-only "task/$TASK_ID" 2>/dev/null; then
    merge_type="fast-forward"
elif git merge --no-edit "task/$TASK_ID"; then
    merge_type="merge commit"
else
    # Conflict
    git merge --abort
    locked_yq "(.tasks[] | select(.id == \"$TASK_ID\")).status = \"INTEGRATION_FAILED\""

    cat >> "$LOG" << EOF
- timestamp: $timestamp
  agent: $LIZA_AGENT_ID
  action: integration_failed
  task: $TASK_ID
  detail: "Merge conflict"
EOF

    die 3 "Merge conflict. Task marked INTEGRATION_FAILED."
fi

# --- Run Integration Tests ---

if [ -f "$PROJECT_ROOT/scripts/integration-test.sh" ]; then
    test_output_file="$PROJECT_ROOT/.liza/integration-test-$TASK_ID-$(date +%s).log"
    if ! "$PROJECT_ROOT/scripts/integration-test.sh" 2>&1 | tee "$test_output_file"; then
        failed_sha=$(git rev-parse HEAD | cut -c1-7)

        git reset --hard HEAD~1
        locked_yq "(.tasks[] | select(.id == \"$TASK_ID\")).status = \"INTEGRATION_FAILED\""

        cat >> "$LOG" << EOF
- timestamp: $timestamp
  agent: $LIZA_AGENT_ID
  action: integration_failed
  task: $TASK_ID
  failed_sha: $failed_sha
  test_output: $test_output_file
  detail: "Integration tests failed — see $test_output_file for diagnostics"
EOF

        echo "Error: Integration tests failed. Task marked INTEGRATION_FAILED."
        echo "Test output saved to: $test_output_file"
        echo "Failed merge was at commit: $failed_sha (now reset)"
        exit 1
    else
        rm -f "$test_output_file"
    fi
fi

# --- Success: Clean Up ---

git worktree remove "$worktree_dir"
git branch -d "task/$TASK_ID"

# Update both fields atomically to prevent observing MERGED with non-null worktree
locked_yq "(.tasks[] | select(.id == \"$TASK_ID\")) |= (.status = \"MERGED\" | .worktree = null)"

# Log with drift info
cat >> "$LOG" << EOF
- timestamp: $timestamp
  agent: $LIZA_AGENT_ID
  action: merged
  task: $TASK_ID
  drift_commits: $drift_commits
  detail: "$merge_type merge to $integration_branch"
EOF

# Update sprint metrics
"$PROJECT_ROOT/scripts/update-sprint-metrics.sh" "$PROJECT_ROOT" 2>/dev/null || true

echo "Merged $TASK_ID to $integration_branch ($merge_type)"
