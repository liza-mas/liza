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
| Task SUPERSEDED (no replacements) | Delete worktree directory and branch | Planner |
| Task INTEGRATION_FAILED | Worktree retained for conflict resolution | — |

**Note:** Worktree creation is supervisor-only (via `liza claim-task`), not agent-callable. This ensures worktrees exist before agents are spawned.

**Reassignment rule:** When a different coder claims a task after `REJECTED`, the worktree is deleted and recreated fresh. Same coder re-claiming keeps the existing worktree. Rationale: salvaging failed work often costs more than restarting from spec.

**Blocked-task note:** In the current state machine, `BLOCKED` tasks do not transition back to `READY`. They are resolved via `SUPERSEDED` (with or without replacement tasks) or `ABANDONED`; any existing worktree should be cleaned up via `liza wt-delete task-N`.

**Superseded branch preservation:** When a task is superseded with replacement tasks, its worktree directory is removed but its git branch is preserved. Successor tasks may need the branch to access prior artifacts via `git show <branch>:<path>`. The branch is automatically cleaned up when **all** successor tasks listed in `superseded_by` reach terminal status (`MERGED`, `ABANDONED`, or `SUPERSEDED` per `IsTerminal()`). Cleanup is triggered by any successor terminal transition (merge, cancel, or supersede). When a task is superseded without replacements (e.g., work completed externally), the branch is deleted immediately since no successors exist to trigger later cleanup.

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

---

## Integration Protocol

After APPROVED, **Code Reviewer** executes:

1. Verify `review_commit` matches current HEAD
2. Run `liza wt-merge task-N`
3. Script performs working-tree-less merge:
   - Read integration HEAD without checkout (`git rev-parse refs/heads/integration`)
   - Detect fast-forward (task commit is descendant of integration)
   - For fast-forward: validate candidate artifact refs against the task commit before `git update-ref`
   - For true merge: compute tree via `git merge-tree`, create commit via `git commit-tree`, validate candidate artifact refs against the merge tree or commit tree before `git update-ref`, then update the ref
   - Working tree files are transiently synced for integration test correctness, then restored if checked-out branch differs from integration
4. If conflict: task → INTEGRATION_FAILED, Code Reviewer reports
5. If candidate artifact validation fails: reject before integration ref advancement, task → INTEGRATION_FAILED
6. Validate post-merge blackboard artifact references with `ValidateArtifactRefs` against the synced tree and integration branch. Retired task refs (`SUPERSEDED`, `ABANDONED`) are non-blocking until supersede-time artifact retirement is implemented; active and `MERGED` refs remain protected.
7. If post-merge artifact validation fails: rollback via `git update-ref` to pre-merge HEAD, task → INTEGRATION_FAILED
8. If integration tests fail: rollback via `git update-ref` to pre-merge HEAD, task → INTEGRATION_FAILED
9. On success: working tree restored to checked-out branch HEAD (unless on integration, where no restore needed), task → MERGED, worktree deleted

Candidate artifact validation protects goal `spec_ref`; task `spec_ref`,
`epic_ref`, `plan_ref`, and `arch_ref`; and the same fields on `output[]`
entries. Each protected ref is a scalar repo-relative path with an optional
`#fragment` anchor; validation strips the fragment before checking the Git tree.
The candidate tree must contain the path as a regular Git file mode `100644` or
`100755`. Missing paths, directories, submodules/gitlinks, symlinks, and other
non-regular object modes are rejected. Diagnostics are deterministic and name
the invalid path plus owner provenance: field name, task ID when the owner is a
task, and output index when the owner is an `output[]` entry.

Post-merge `ValidateArtifactRefs` remains a rollback backstop after successful
ref advancement; it is not replaced by the candidate-tree guard.

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

Blackboard records `review_commit` as the resolved worktree HEAD. Code Reviewer verifies this SHA before reviewing.

---

## Commit SHA Verification

Code Reviewer must verify before examining work (implemented by supervisor at review claim time):

```
ACTUAL  = git -C $WORKTREE rev-parse HEAD
EXPECTED = task.review_commit from blackboard

if ACTUAL != EXPECTED:
    ERROR: Worktree modified since review requested
```

## Concurrent Merge Safety

Multiple reviewers can merge approved tasks concurrently without race conditions:

**Before (race-prone):**
```
reviewer A: git checkout integration → git merge task-1  [modifies working tree]
reviewer B: git checkout integration → git merge task-2  [concurrent modification → corruption]
```

**After (working-tree-less):**
```
reviewer A: read HEAD → merge-tree → commit-tree → update-ref  [object operations only]
reviewer B: read HEAD → merge-tree → commit-tree → update-ref  [safe concurrent execution]
```

Git object database operations are inherently safe for concurrent reads. Each `update-ref` uses compare-and-swap (CAS): `git update-ref <ref> <new> <old>`. If the ref moved since it was read (another merge landed), the CAS fails and the merge retries from the new HEAD. This prevents lost updates without requiring external locks.

## Related Documents

- [Task Lifecycle](task-lifecycle.md) — claim, iterate, review
- [Tooling](../implementation/tooling.md) — `liza wt-create`, `liza wt-merge`, `liza wt-delete`
- [Roles](../architecture/roles.md) — commit permissions
