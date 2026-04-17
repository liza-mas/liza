# Tech Debt

Deliberate debt with payback triggers. See CORE.md Rule 3 (DoD) for policy.

## ParentTask (singular) field deprecation

**What:** `models.Task.ParentTask *string` coexists with `ParentTasks []string`. `EffectiveParentTasks()` bridges both, and `buildChildTask` writes only `ParentTasks`. But `ParentTask` remains in the struct and is populated by existing YAML state files.

**Why deferred:** Removing it requires migrating all active state files (in-flight sprints across user projects). No correctness risk while `EffectiveParentTasks()` handles both.

**Payback trigger:** When no active state files use `parent_task` (singular) — check with `grep -r "parent_task:" ~/.liza/state.yaml` across deployments. At that point, remove the field from the struct and drop the fallback branch in `EffectiveParentTasks()`.

## Worktree path guard: unverified payload shapes (MultiEdit, NotebookEdit)

**What:** `internal/embedded/hooks/worktree-path-guard.sh` extracts `file_path` from the PreToolUse payload to catch `.worktrees/<id>/<id>/` duplication. For Read/Write/Edit the field name is documented. For MultiEdit and NotebookEdit it is not — as of 2026-04-17, neither tool has its PreToolUse hook schema in the public Claude Code docs.

**Current state:**
- MultiEdit: matcher IS registered in `claude-settings.json`. Best-effort coverage — if MultiEdit sends `file_path`, the hook catches the bug; if not, it silently no-ops (no false deny because the extraction returns empty). Do NOT treat as confirmed protection.
- NotebookEdit: matcher NOT registered. Less common use, and promoting MultiEdit was the lower-risk experiment.

**Why deferred:** shipping claims of coverage we haven't verified would mislead future maintainers. The asymmetry (MultiEdit registered, NotebookEdit not) is intentional — MultiEdit is a higher-probability vector for the target bug given its Edit-like semantics.

**Payback trigger:** Next time MultiEdit or NotebookEdit is invoked during an agent session, capture the raw PreToolUse payload via a temporary debug hook (e.g., `cat >> /tmp/payloads.jsonl`). Promote MultiEdit to VERIFIED or teach the script the real field name; add NotebookEdit matcher once its shape is confirmed.

## Deferred greenfield reproduction for precommit-bootstrap

**What:** The greenfield reproduction procedure defined in `specs/goals/20260417-precommit-bootstrap.md` §Greenfield Reproduction Procedure is deferred. That procedure seeds two synthetic greenfield projects under a scratch `REPRO_ROOT`, runs `liza init` on each, starts two parallel supervisors against currently-unfixed prompts, waits for both cycles to reach a terminal state (up to 5 iterations per project), captures per-cycle artifacts (`state-snapshots/`, `agent-outputs/`, `supervisor.stdout.log`, `worktree-git-logs/`, `prompts/`, `precommit-config-presence.txt`) via `scripts/repro/precommit-bootstrap-greenfield.sh`, sanitizes user-home prefixes and scrubs credential-shaped files, and commits the sanitized tree under `specs/goals/precommit-bootstrap-repro-artifacts/<YYYY-MM-DD>/`. In place of executing that procedure, the four hypothesized failure modes from §Evidence were accepted on inspection and recorded in §Observed Failure Modes with the evidence pointer `hypothesis inspection only (no empirical run)`.

**Why deferred:** The reproduction is operator-scale wall-clock work — two full supervisor cycles running end-to-end against an intentionally broken baseline, each potentially iterating up to five times, plus capture and sanitization — and the current sprint is prioritizing landing the remediation stack (Q2 dedup, Q3 architect-prompt bootstrap entry, `internal/precommit/` context helpers, ADR-0036 amend) over producing baseline evidence of a failure mode the design already remediates. The four hypothesized modes are mechanically motivated by the combination of `.pre-commit-config.yaml` being absent in a greenfield `liza init` tree and `commit_workflow.tmpl:3` unconditionally instructing coders to run pre-commit on every commit — each mode is a direct consequence of that composition rather than a speculative behavior, so accepting them on inspection is sufficient to justify the scoped design without blocking the sprint on wall-clock reproduction work.

**Payback trigger:** Any production bootstrap failure whose observed mode does not match one of the four hypothesized modes enumerated in specs/goals/20260417-precommit-bootstrap.md §Evidence.
