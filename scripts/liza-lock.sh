#!/bin/bash
# Atomic blackboard operation
# Usage: liza-lock.sh <read|write|modify> [args...]
#
# liza-lock.sh read                    # Print current state
# liza-lock.sh write field value       # Set field (yq syntax, value passed via env)
# liza-lock.sh modify <cmd> [args...]  # Run command with lock held (no shell)
#
# Exit codes:
#   0: Success
#   1: Invalid usage
#   2: Lock acquisition failed (timeout)

set -euo pipefail

# --- Path Setup ---
source "$(dirname "$0")/liza-common.sh"
setup_liza_paths
readonly LOCK_TIMEOUT=10

# --- Helper Functions ---

acquire_lock_cmd() {
    if ! flock -x -w "$LOCK_TIMEOUT" "$LOCK" "$@"; then
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
        acquire_lock_cmd env VALUE="$value" yq -i "$field = strenv(VALUE)" "$STATE"
        ;;
    modify)
        shift
        if [ "$#" -lt 1 ]; then
            echo "Usage: liza-lock.sh modify <cmd> [args...]" >&2
            exit 1
        fi
        acquire_lock_cmd "$@"
        ;;
    *)
        echo "Usage: liza-lock.sh <read|write|modify> [args...]" >&2
        exit 1
        ;;
esac
