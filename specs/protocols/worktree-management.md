# Worktree Management Protocol

## Lifecycle

| Event | Action | Actor |
|-------|--------|-------|
| Task IMPLEMENTING (fresh) | Create worktree via `liza claim-task` | Supervisor |
| Task IMPLEMENTING (reassignment) | Create fresh worktree via `liza claim-task` | Supervisor |
| Task APPROVED | Merge eligible | — |
| Task MERGED | `liza wt-merge task-N` | Supervisor (after Code Reviewer approves) |
| Task BLOCKED | Delete worktree: `liza wt-delete task-N` | Planner |
| Task ABANDONED | Delete worktree: `liza wt-delete task-N` | Planner |
| Task SUPERSEDED (with replacements) | Delete worktree directory (branch preserved for successors): `liza wt-delete task-N` | Planner |
| Task SUPERSEDED (no replacements) | Record recoverability audit command and pre-cleanup branch/worktree snapshot, then delete worktree directory and branch | Planner |
| Task INTEGRATION_FAILED | Worktree retained for conflict resolution | — |

**Note:** Worktree creation is supervisor-only (via `liza claim-task`), not agent-callable. This ensures worktrees exist before agents are spawned.

**Reassignment rule:** When a different coder claims a task after `REJECTED`, the worktree is deleted and recreated fresh. Same coder re-claiming keeps the existing worktree. Rationale: salvaging failed work often costs more than restarting from spec.

**Blocked-task note:** In the current state machine, `BLOCKED` tasks do not transition back to `READY`. They are resolved via `SUPERSEDED` (with or without replacement tasks) or `ABANDONED`; any existing worktree should be cleaned up via `liza wt-delete task-N`.

**Superseded branch preservation:** When a task is superseded with replacement tasks, its worktree directory is removed but its git branch is preserved. Successor tasks may need the branch to access prior artifacts via `git show <branch>:<path>`. The branch is automatically cleaned up when **all** successor tasks listed in `superseded_by` reach terminal status (`MERGED`, `ABANDONED`, or `SUPERSEDED` per `IsTerminal()`). Cleanup is triggered by any successor terminal transition (merge, cancel, or supersede). When a task is superseded without replacements (e.g., work completed externally), the branch is deleted immediately since no successors exist to trigger later cleanup; `supersede-task` requires a single-line `--recoverability-command` audit string and records pre-cleanup branch/worktree status before deleting artifacts. Liza records this command but does not execute it, so do not include secrets.

---

## Naming

`.worktrees/task-{id}/` — one directory per task.

---

## Branch Strategy

```
main
  └── integration  (all approved work merges here)
        ├── .worktrees/task-1/  (branched from integration)
        ├── .worktrees/task-2/
        └── .worktrees/task-3/
```

Merge to main is human-triggered, not part of Liza flow.

---

## Commit Permissions

| Actor | Can Commit To |
|-------|---------------|
| Coder | Task worktree branch only |
| Code Reviewer | None (read-only; approves for merge) |
| Planner | Neither (no code changes) |
| Supervisor | Integration branch (executes merge after APPROVED) |

**Hard rule:** Coders cannot commit to or merge to integration. Only the supervisor can merge, and only after Code Reviewer approval.

---

## Worktree Rules

1. Coder works only in assigned task's worktree
2. Code Reviewer examines same worktree (read-only)
3. No cross-worktree file access
4. No direct commits to integration branch

**Worktree readiness:** When `config.post_worktree_cmd` is set, it runs in the
task worktree before any provider session starts — on initial claim, doer resume
(handoff and restart), review, worktree recovery, and worktree recreation. It
must succeed. A failure fails closed: the doer claim aborts before it is
recorded, a reviewer claim is released back to the task's reviewable status, and
the affected agent is marked degraded so it stops claiming instead of retrying.
Reviewer setup failures do not block the task — blocking would return it to the
doer's executing status and discard review-ready work. The command's output is never captured: a
setup command can print secrets that cannot be masked from this process, so only
the masked command, the worktree path, and the exit status are recorded.
Reproduce a failure by rerunning the command in the named worktree. See
ADR-0117.

**Runtime setup configuration:** Operators read and write `config.post_worktree_cmd`
with `config get` and `config set`; the general `get` query remains equivalent.
Setting stores the shell command without executing it. An identical value is a
successful no-op; a different existing value requires `--replace` and a non-empty
`--reason`. Compare and write run in one blackboard transaction: `bb.Modify` for
operators, or `ModifyWithAgentAuthority` for identified, capability-authorized agents.
No default role receives the `config-set-post-worktree-cmd` capability. Replacement
flags prevent accidental writes; they do not establish authenticated human identity.

Existing merge-time detection remains unchanged: after a successful merge, it checks
for an unset value again inside the transaction marking the task MERGED. Thus an
operator write committed first wins; if detection commits first, a different explicit
set returns a conflict. Settings apply when subsequent setup operations read them;
already-running setup and provider sessions are not restarted. No task metadata or
planner activation mechanism is involved.

Before setting a command, validate it against committed scaffolding in a fresh checkout.
Generated artifacts must be gitignored or already committed, and a second run must
change nothing observable. Git status must stay clean, including untracked files.
The setter validates representation, not shell safety or project readiness.
Its `config_set` audit is process-log-only (stderr), including outcome and detector
comparison; it adds no blackboard fields or activity-log entries. See the support
reference's “Changing worktree setup during a run” section for operational details.

**Ignored env-file provisioning:** Worktree env-file copying is disabled by
default. When `config.copy_worktree_env_files` is true, worktree setup may copy
only root-level regular files matching `.env`, `.env.*`, `*.env`, or `.envrc`.
The source path must be ignored by Git, the destination path must be made and
verified ignored in the task worktree before copying, and existing destination
files are never overwritten. Failures produce path-only warnings and do not
block worktree creation.

---

## Lease Expiration and Worktree State

When a coder's lease expires:

1. **Task becomes reclaimable** — status stays IMPLEMENTING but lease_expires is in the past
2. **Original coder must self-abort** — if they return after expiry, they exit immediately
3. **Worktree handling depends on who supervisor assigns:**
   - Same coder: worktree preserved (agent returning after brief network issue)
   - Different coder: supervisor deletes and recreates worktree fresh

**Design Rationale:**
- Same coder reclaiming: preserve work (crash recovery)
- Different coder reclaiming: fresh start (salvaging failed work costs more than restarting)
- Handoff notes (if written) provide context regardless of worktree state

---

## Staleness Detection

Before starting work, coder checks if worktree base is stale:

```bash
git fetch origin integration
git merge-base --is-ancestor integration HEAD || echo "STALE"
```

If stale, coder decides based on:

| Condition | Risk | Action |
|-----------|------|--------|
| No conflicts after rebase attempt | Low | Auto-rebase, continue |
| Task touches ≤2 files, no shared modules | Low | Auto-rebase, continue |
| Task touches shared code (utils, models, API) | High | BLOCKED, planner decides |
| Merge conflicts detected | High | BLOCKED, planner decides |
| Integration branch has schema/API changes | High | BLOCKED, planner decides |

**Decision rule:** If in doubt, BLOCKED is safer than silent rebase. Planner can always unblock.

`liza unblock-task <id> --rebase-on <branch>` is an orchestrator repair path for
blocked tasks whose preserved worktree should be rebased before resuming. The
rebase runs outside the blackboard lock, then the final state update revalidates
task status, worktree path, task branch, HEAD, and the resolved target SHA. On
success, `base_commit` becomes the resolved target SHA and history records old
HEAD, new HEAD, target ref, and target SHA. Rebase conflicts are aborted and the
task remains `BLOCKED` with fresh blocked metadata and a repair request; this is
distinct from submit/merge conflicts that move tasks to `INTEGRATION_FAILED`.

When `unblock-task` is run without `--assign-to`, it may restore a repaired task
with valid pending dependencies to the role-pair initial status. The task remains
dependency-held and unclaimable until every direct dependency is `MERGED`;
direct `--assign-to` remains rejected while any dependency is unmet. If
`worktree` remains set, the initial task is a preserved-branch continuation.

Once dependencies merge, claim rechecks them before filesystem work, captures
one integration commit, and rebases or validates the preserved HEAD on that
captured integration SHA. The completion lock then encloses the final ref
equality check and assignment, ordering cooperating integration movement before
or after the assignment without holding the integration mutation lock across the
blackboard write. A stale ref, changed HEAD, dirty worktree, or failed rebase
leaves the task unassigned and preserves actionable recovery state.

---

## Drift Tracking

Tasks record `base_commit` (integration HEAD) at claim time. This enables drift visibility:

**At claim:**
```bash
base_commit=$(git rev-parse integration)
# Stored in task.base_commit
```

**At merge** (implemented by `liza wt-merge`):
```bash
current_integration=$(git rev-parse integration)
base_commit=<task.base_commit from blackboard>
drift_commits=$(git rev-list --count $base_commit..$current_integration)
```

**Sprint summary includes drift metrics:**
- Tasks with `drift_commits > 0` at merge indicate accumulated staleness
- High drift correlates with integration failure risk
- "Last task penalty": sequential tasks accumulate drift; later tasks have higher merge conflict probability

**Retrospective signal:** If drift consistently high, consider:
- Smaller sprints
- More frequent integration checkpoints
- Prioritizing tasks that touch shared code early

At `submit-for-review`, the CLI rebases the task worktree onto the current integration head and updates the live review boundary:

```bash
review_commit=$(git -C "$WORKTREE" rev-parse HEAD)
base_commit=$(git merge-base "$review_commit" integration)
# Stored as task.review_commit and task.base_commit
```

For submitted/reviewing tasks with a worktree, this boundary must stay coherent:
- `review_commit` equals the task worktree HEAD
- `base_commit` equals the merge-base of `review_commit` and the configured integration branch

If an external rebase or operator repair changes the worktree after submission, run `liza update-review-commit <task-id>`. The command updates both `review_commit` and `base_commit` and releases any active reviewer claim so review restarts from a coherent boundary.

`update-review-commit` is also the narrow degraded-state repair for a submitted/reviewing task whose `review_commit` is missing while its task worktree still exists. The input state violates the submitted/reviewing boundary invariant; the command repairs it by setting `review_commit` from worktree HEAD, setting `base_commit` from the effective merge-base, and recording the missing old boundary in audit history.

---

## Integration Protocol

After APPROVED, **Code Reviewer** executes:

1. Verify `review_commit` matches current HEAD
2. Run `liza wt-merge task-N`
3. Script constructs the merge commit without a working tree:
   - Read integration HEAD without checkout (`git rev-parse refs/heads/integration`)
   - Detect fast-forward (task commit is descendant of integration)
   - For fast-forward: validate candidate artifact refs against the task commit before `git update-ref`
   - For true merge: compute tree via `git merge-tree`, create commit via `git commit-tree`, validate candidate artifact refs against the merge tree or commit tree before `git update-ref`, then update the ref
   - Integration ref advancement and transient working-tree sync/restore run under a project-scoped file lock; files are restored if the checked-out branch differs from integration
4. If conflict: task → INTEGRATION_FAILED, Code Reviewer reports
5. If candidate artifact validation fails: reject before integration ref advancement, task → INTEGRATION_FAILED
6. Validate post-merge blackboard artifact references with merge-scoped artifact validation against the synced tree and integration branch. Retired task refs (`SUPERSEDED`, `ABANDONED`) are non-blocking; goal refs, task-level refs, the merging task's output refs, and already-MERGED tasks' output refs remain protected. Unrelated in-flight task output refs are ignored because their artifacts may still exist only in sibling worktrees.
7. If post-merge artifact validation fails: rollback via `git update-ref` to pre-merge HEAD, task → INTEGRATION_FAILED
8. If integration tests fail: rollback via `git update-ref` to pre-merge HEAD, task → INTEGRATION_FAILED
9. On success: working tree restored to checked-out branch HEAD (unless on integration, where no restore needed), task → MERGED, worktree deleted

Candidate artifact validation protects goal `spec_ref`; task `spec_ref`,
`epic_ref`, `plan_ref`, and `arch_ref`; and merge-durable output refs. Output
refs are merge-durable for the task being merged and for already-MERGED tasks.
Each protected ref is a scalar repo-relative path with an optional `#fragment`
anchor; validation strips the fragment before checking the Git tree. The
candidate tree must contain the path as a regular Git file mode `100644` or
`100755`. Missing paths, directories, submodules/gitlinks, symlinks, and other
non-regular object modes are rejected. Invalid artifact refs fail closed,
including semicolon-joined refs, empty paths after stripping `#fragment`, paths
that traverse outside the repository, and absolute refs that cannot be safely
normalized to repo-relative paths. Diagnostics are deterministic and name the
invalid path plus owner provenance: field name, task ID when the owner is a
task, and output index when the owner is an `output[]` entry.

Post-merge merge-scoped artifact validation remains a rollback backstop after
successful ref advancement; it is not replaced by the candidate-tree guard.

---

## Integration-Fix Ownership

When task is INTEGRATION_FAILED:

1. Task becomes claimable by any coder
2. Claim scope is explicitly "resolve conflicts / fix integration"
3. Original implementation is not re-reviewed
4. Only the conflict resolution is reviewed
5. Mark in blackboard: `integration_fix: true`

This prevents planner paralysis on merge conflicts.

---

## Clean Sync Invariant

Before setting READY_FOR_REVIEW, coder must ensure:

```bash
# Working tree clean
[ -z "$(git -C $WORKTREE status --porcelain)" ] || abort "Uncommitted changes"

# Submit HEAD for Code Reviewer; the CLI resolves HEAD inside the task worktree
liza submit-for-review "$TASK_ID" HEAD --agent-id "$AGENT_ID"
```

**Definition of "clean":**
- No staged changes (index matches HEAD)
- No unstaged changes (working tree matches index)
- No untracked files (except .gitignored)
- `git status --porcelain` returns empty string
- Submodule state is not checked (out of scope for v1)

Blackboard records `review_commit` as the resolved worktree HEAD and `base_commit` as the effective review base. Code Reviewer verifies this boundary before reviewing.

---

## Commit SHA Verification

Code Reviewer must verify before examining work (implemented by supervisor at review claim time):

```
ACTUAL  = git -C $WORKTREE rev-parse HEAD
EXPECTED = task.review_commit from blackboard
EXPECTED_BASE = git merge-base $EXPECTED integration

if ACTUAL != EXPECTED:
    ERROR: Worktree modified since review requested; run liza update-review-commit <task-id>
if task.base_commit != EXPECTED_BASE:
    ERROR: Review base is stale; run liza update-review-commit <task-id>
```

## Concurrent Merge Safety

Multiple reviewers can merge approved tasks concurrently without lost ref updates or shared-index collisions:

**Before (race-prone):**
```
reviewer A: git checkout integration → git merge task-1  [modifies working tree]
reviewer B: git checkout integration → git merge task-2  [concurrent modification → corruption]
```

**After (object-only construction plus serialized index mutation):**
```
reviewer A: lock → read HEAD → merge-tree → commit-tree → update-ref → sync → unlock
reviewer B: wait → lock → read HEAD → merge-tree → commit-tree → update-ref → sync → unlock
```

Git object construction is safe without a checkout. A project-scoped file lock serializes integration ref advancement and every operation that mutates the shared main index, including forward sync, rollback sync/restore, and success restore. Each `update-ref` still uses compare-and-swap (CAS): `git update-ref <ref> <new> <old>`. CAS protects against ref movement by external Git writers and retries from the new HEAD rather than losing an update.

## Related Documents

- [Task Lifecycle](task-lifecycle.md) — claim, iterate, review
- [Tooling](../implementation/tooling.md) — `liza wt-create`, `liza wt-merge`, `liza wt-delete`
- [Roles](../architecture/roles.md) — commit permissions
