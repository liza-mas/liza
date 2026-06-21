#!/bin/sh
# __BRAND_NAME_TITLE__ worktree pre-commit hook (two-stage chain).
#
# Stage 1 — __BRAND_NAME_TITLE__ guard (check-commit-allowed):
#   Rejects commits when the task is not in a state that permits coder
#   mutations. Fail-safe: any unexpected exit code (missing subcommand,
#   panic, signal death) falls through to allow — the guard is a guard,
#   not a lock, and a crashing evaluator must never deadlock commits.
#
# Stage 2 — Project pre-commit chain:
#   When <worktree>/.pre-commit-config.yaml is present, runs `pre-commit
#   run` and propagates its real exit code. Fail-closed: a missing
#   `pre-commit` binary in this case is a loud error, not a silent skip
#   (silent skip would re-introduce the gap this chain closes).
#
# Installed by `__BRAND_BINARY_NAME__ wt-create` into each task worktree via:
#   git config extensions.worktreeConfig true
#   git config --worktree core.hooksPath <worktree>/__BRAND_PROJECT_DIRNAME__-hooks
#
# The absolute path to the __BRAND_BINARY_NAME__ binary is baked in at install time. If the
# binary is missing at commit time, the guard step is skipped (belt-and-
# braces with submit-verdict's HEAD check) but the project pre-commit
# chain still runs.

BRAND_BIN="__LIZA_BIN__"
TASK_ID="__TASK_ID__"

if [ ! -x "$BRAND_BIN" ]; then
    BRAND_BIN="$(command -v __BRAND_BINARY_NAME__ 2>/dev/null)"
fi
if [ -n "$BRAND_BIN" ] && [ -x "$BRAND_BIN" ]; then
    "$BRAND_BIN" check-commit-allowed "$TASK_ID"
    ec=$?
    case "$ec" in
        1) exit 1 ;;
        0) ;;  # fall through to project pre-commit
        *) ;;  # fail-safe: unknown exit treated as allow, then chain
    esac
fi

# Stage 2: project pre-commit chain.
WORKTREE="$(git rev-parse --show-toplevel 2>/dev/null)"
if [ -z "$WORKTREE" ] || [ ! -f "$WORKTREE/.pre-commit-config.yaml" ]; then
    exit 0
fi

if ! command -v pre-commit >/dev/null 2>&1; then
    echo "ERROR: .pre-commit-config.yaml present in worktree but pre-commit binary not installed" >&2
    echo "       Install pre-commit: https://pre-commit.com/#install" >&2
    exit 1
fi

cd "$WORKTREE" || exit 1
pre-commit run
exit $?
