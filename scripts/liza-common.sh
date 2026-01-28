#!/bin/bash
# Common functions for Liza scripts
# Source this file: source "$(dirname "$0")/liza-common.sh"

# Get the main project root, even when called from inside a worktree
# Usage: PROJECT_ROOT=$(get_project_root)
get_project_root() {
    local toplevel git_common_dir
    toplevel=$(git rev-parse --show-toplevel 2>/dev/null)
    git_common_dir=$(realpath "$(git rev-parse --git-common-dir 2>/dev/null)")

    # In a worktree, .git is a file; git-common-dir points to main repo's .git
    # Main repo: git-common-dir == toplevel/.git
    # Worktree:  git-common-dir == <main>/.git (parent of toplevel)
    if [[ "$git_common_dir" != "$toplevel/.git" ]]; then
        # We're in a worktree - common dir is <main>/.git
        dirname "$git_common_dir"
    else
        echo "$toplevel"
    fi
}

# Get standard Liza paths
# Sets: PROJECT_ROOT, LIZA_DIR, STATE, LOG, LOCK
setup_liza_paths() {
    PROJECT_ROOT=$(get_project_root)
    readonly PROJECT_ROOT
    readonly LIZA_DIR="$PROJECT_ROOT/.liza"
    readonly STATE="$LIZA_DIR/state.yaml"
    readonly LOG="$LIZA_DIR/log.yaml"
    readonly LOCK="$STATE.lock"
}

# ISO timestamp in UTC
iso_timestamp() {
    date -u +%Y-%m-%dT%H:%M:%SZ
}

# ISO timestamp with offset
# Usage: iso_timestamp_offset "+60 seconds"
iso_timestamp_offset() {
    date -u -d "$1" +%Y-%m-%dT%H:%M:%SZ
}
