#!/bin/bash
# Atomic blackboard operation
# Usage: liza-lock.sh <read|write|modify> [args...]
#
# liza-lock.sh read                    # Print current state
# liza-lock.sh write field value       # Set field (yq syntax)
# liza-lock.sh modify "command"        # Run command with lock held
#
# Exit codes:
#   0: Success
#   1: Invalid usage
#   2: Lock acquisition failed (timeout)

set -euo pipefail

# --- Path Setup ---
PROJECT_ROOT=$(git rev-parse --show-toplevel)
readonly PROJECT_ROOT
readonly STATE="$PROJECT_ROOT/.liza/state.yaml"
readonly LOCK="$STATE.lock"
readonly LOCK_TIMEOUT=10

# --- Helper Functions ---

acquire_lock() {
    local cmd="$1"
    if ! flock -x -w "$LOCK_TIMEOUT" "$LOCK" -c "$cmd"; then
        echo "Error: Lock acquisition failed (timeout after ${LOCK_TIMEOUT}s)" >&2
        exit 2
    fi
}

# --- Main ---

case "${1:-}" in
    read)
        cat "$STATE"
        ;;
    write)
        field="$2"
        value="$3"
        acquire_lock "yq -i '$field = \"$value\"' '$STATE'"
        ;;
    modify)
        cmd="$2"
        acquire_lock "$cmd"
        ;;
    *)
        echo "Usage: liza-lock.sh <read|write|modify> [args...]" >&2
        exit 1
        ;;
esac
