#!/usr/bin/env bash
#
# Scaffolding for the greenfield reproduction procedure defined in
#   specs/goals/20260417-precommit-bootstrap.md (§Greenfield Reproduction Procedure)
# and operationalized by
#   specs/arch-plan/20260417-145405-architecture-3.md (§5.3, §5.4).
#
# The script body may use standard shell features (including $(...) command
# substitution and && chaining). BASH CONSTRAINTS in base_prompt.tmpl govern
# the agent's direct shell invocations, not the contents of a script file the
# agent commits. See arch plan §5.4.
#
# This script never installs anything host-level. It uses only git, mkdir,
# cp, find, date, test, printf, and shell builtins. All writes land under
# $REPRO_ROOT.

set -euo pipefail

usage() {
    cat >&2 <<'USAGE'
Usage: bash scripts/repro/precommit-bootstrap-greenfield.sh <REPRO_ROOT> <cycle> [<phase>]

  <REPRO_ROOT>  Writable absolute path (e.g. /tmp/liza-precommit-repro-YYYYMMDD-NNN)
  <cycle>       Project subdirectory name (e.g. proj, proj-parallel)
  <phase>       setup | snapshot | capture   (default: capture)

Phases:
  setup     Seed REPRO_ROOT/<cycle> with README.md + specs/vision/greenfield.md,
            git init + initial commit, liza init (if liza is on PATH).
  snapshot  Copy REPRO_ROOT/<cycle>/.liza/state.yaml to a timestamped file
            under REPRO_ROOT/observations/<cycle>/state-snapshots/.
  capture   Collect agent outputs, prompts, supervisor log, worktree git logs,
            and the integration-branch presence of .pre-commit-config.yaml
            under REPRO_ROOT/observations/<cycle>/.

See specs/arch-plan/20260417-145405-architecture-3.md §5.4 for responsibilities.
USAGE
    exit 2
}

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
    usage
fi

REPRO_ROOT="$1"
cycle="$2"
phase="${3:-capture}"

case "$REPRO_ROOT" in
    /*) ;;
    *)
        printf 'error: REPRO_ROOT must be an absolute path: %s\n' "$REPRO_ROOT" >&2
        usage
        ;;
esac

if [ -z "$cycle" ]; then
    printf 'error: <cycle> must be non-empty\n' >&2
    usage
fi

case "$phase" in
    setup|snapshot|capture) ;;
    *)
        printf 'error: invalid phase: %s (expected setup|snapshot|capture)\n' "$phase" >&2
        usage
        ;;
esac

project_dir="$REPRO_ROOT/$cycle"
observations_dir="$REPRO_ROOT/observations/$cycle"

phase_setup() {
    mkdir -p "$project_dir/specs/vision"

    readme_path="$project_dir/README.md"
    if [ ! -e "$readme_path" ]; then
        printf '%s\n' "greenfield repro project" > "$readme_path"
    fi

    vision_path="$project_dir/specs/vision/greenfield.md"
    if [ ! -e "$vision_path" ]; then
        {
            printf '%s\n' "# Greenfield Vision"
            printf '\n'
            printf '%s\n' "Add a \`hello\` script that prints 'hi'."
        } > "$vision_path"
    fi

    if [ ! -e "$project_dir/.git" ]; then
        git init "$project_dir" >/dev/null
    fi

    git -C "$project_dir" add README.md specs/vision/greenfield.md

    if ! git -C "$project_dir" diff --cached --quiet; then
        git -C "$project_dir" commit -F - <<'EOF'
initial: README + greenfield vision
EOF
    fi

    if [ ! -e "$project_dir/.liza/state.yaml" ]; then
        if command -v liza >/dev/null 2>&1; then
            liza init "$project_dir"
        else
            printf 'warning: %s\n' "'liza' not on PATH — skipping 'liza init $project_dir'. Install liza separately before running the supervisor." >&2
        fi
    fi

    cat <<'ENVHINT'
# Recommended env-var exports (run in the caller's shell; this script cannot
# propagate env to the parent).
export LIZA_LOG_LEVEL=debug
export LIZA_TEE_AGENT_OUTPUT=1
ENVHINT
}

phase_snapshot() {
    src_state="$project_dir/.liza/state.yaml"
    if [ ! -f "$src_state" ]; then
        printf 'error: source state.yaml absent: %s\n' "$src_state" >&2
        exit 1
    fi
    snap_dir="$observations_dir/state-snapshots"
    mkdir -p "$snap_dir"
    stamp="$(date -u +%Y%m%dT%H%M%SZ)"
    dst="$snap_dir/state-${stamp}.yaml"
    cp "$src_state" "$dst"
    printf 'snapshot: %s\n' "$dst"
}

copy_tree_if_present() {
    label="$1"
    src="$2"
    dst="$3"
    if [ -d "$src" ]; then
        mkdir -p "$dst"
        cp -R "$src/." "$dst/"
    else
        printf 'capture: skipped missing source (%s): %s\n' "$label" "$src" >&2
    fi
}

phase_capture() {
    mkdir -p \
        "$observations_dir/agent-outputs" \
        "$observations_dir/prompts" \
        "$observations_dir/state-snapshots" \
        "$observations_dir/worktree-git-logs"

    copy_tree_if_present "agent-outputs" \
        "$project_dir/.liza/agent-outputs" \
        "$observations_dir/agent-outputs"

    copy_tree_if_present "agent-prompts" \
        "$project_dir/.liza/agent-prompts" \
        "$observations_dir/prompts"

    src_log="$project_dir/supervisor.stdout.log"
    if [ -f "$src_log" ]; then
        cp "$src_log" "$observations_dir/"
    else
        printf 'capture: skipped missing source (supervisor.stdout.log): %s\n' "$src_log" >&2
    fi

    worktrees_dir="$project_dir/.worktrees"
    if [ -d "$worktrees_dir" ]; then
        while IFS= read -r -d '' wt; do
            wt_name="$(basename "$wt")"
            log_out="$observations_dir/worktree-git-logs/${wt_name}.log"
            if [ -e "$wt/.git" ]; then
                git -C "$wt" log --all --oneline --graph > "$log_out" 2>&1 || \
                    printf 'capture: git log failed for worktree: %s\n' "$wt" >&2
            else
                printf 'capture: skipped non-git worktree: %s\n' "$wt" >&2
            fi
        done < <(find "$worktrees_dir" -mindepth 1 -maxdepth 1 -type d -print0)
    else
        printf 'capture: skipped missing source (worktrees): %s\n' "$worktrees_dir" >&2
    fi

    presence_out="$observations_dir/precommit-config-presence.txt"
    if [ -e "$project_dir/.git" ]; then
        git -C "$project_dir" ls-tree HEAD -- .pre-commit-config.yaml > "$presence_out" 2>/dev/null || \
            : > "$presence_out"
    else
        : > "$presence_out"
        printf 'capture: %s is not a git repo; wrote empty %s\n' "$project_dir" "$presence_out" >&2
    fi

    summary="$observations_dir/observations-summary.txt"
    {
        printf 'Observations captured for cycle: %s\n' "$cycle"
        printf 'Repro root: %s\n' "$REPRO_ROOT"
        printf 'Project dir: %s\n' "$project_dir"
        printf 'Observations dir: %s\n' "$observations_dir"
        printf '\n'
        printf 'Artifact subpaths (relative to observations dir):\n'
        printf '  agent-outputs/\n'
        printf '  prompts/\n'
        printf '  state-snapshots/\n'
        printf '  worktree-git-logs/\n'
        printf '  supervisor.stdout.log (if present)\n'
        printf '  precommit-config-presence.txt\n'
        printf '\n'
        printf 'Reminder: the operator populates the goal-spec section\n'
        printf '"Observed Failure Modes - Reproduction Run YYYY-MM-DD" table\n'
        printf 'cells using evidence under %s.\n' "$observations_dir"
    } > "$summary"

    printf 'capture: wrote %s\n' "$summary"
}

case "$phase" in
    setup)    phase_setup    ;;
    snapshot) phase_snapshot ;;
    capture)  phase_capture  ;;
esac
