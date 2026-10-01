# 80 - Claimable Rebase Unblock

## Status

ACCEPTED

Amended by [ADR-0170](0170-continuation-is-the-default-unblock.md): an
unassigned restore continues the current iteration unless `--new-iteration` is
passed, and `--assign-to` only picks the doer.

## Context

Blocked tasks are often blocked because a required artifact is missing. The orchestrator can create a separate task to fill that gap, but once the repair task merges, the original blocked task's preserved worktree may still be based on an older integration branch that does not contain the newly introduced artifact.

Before this decision, `unblock-task` was less useful for that common recovery path. The blocked task needed to see the new artifact before resuming, and direct reassignment with `--assign-to` was not a good default for sandboxed agents. Agents running inside sandboxes can be confused by their inability to inspect process state reliably, so asking them to choose or validate a direct target agent creates avoidable operational friction.

The better recovery shape is: repair the missing artifact, rebase the preserved blocked-task worktree onto the integration branch that now contains it, then return the task to normal claimability.

## Decision

Make `unblock-task` able to rebase preserved blocked-task worktrees and restore repaired tasks to claimable status without requiring a live assignee.

When unblocking a repaired `BLOCKED` task, Liza may:
- validate dependencies and preserved-worktree metadata
- optionally rebase the preserved task worktree onto a chosen branch
- update `base_commit` after a successful unblock-time rebase
- move the task back to its role-pair initial status so any eligible agent can claim it
- keep `--assign-to` as an explicit direct-resume fast path

If the unblock-time rebase conflicts, Liza leaves the task `BLOCKED` with repair metadata instead of moving it to integration failure or pretending the task is claimable.

## Current Policy Note (2026-08-21)

Issue #118 separates restoration to a role-pair initial status from immediate claimability. Unassigned `unblock-task` may restore a repaired task with valid pending dependencies, but the restored task remains dependency-held and unclaimable until every direct dependency is `MERGED`; direct `--assign-to` remains rejected while any dependency is unmet. Once the dependencies merge, preserved-worktree claim uses one captured integration SHA for rebase and ancestry validation. It then holds the completion lock across the final integration-ref equality check and assignment, ordering cooperating integration movement on either side without holding the integration mutation lock across the blackboard write.

## 2026-09-28 Amendment: Claim-Time Adoption of Uncommitted Work

A preserved worktree holding a dead owner's uncommitted edits made every claim
block the task for manual cleanup (operator note D59). Its successor could not
continue the work it was meant to inherit.

Claim now adopts that work as one hook-skipping WIP commit on the task branch
before the captured-integration rebase, and records the commit in the claim
history. Adoption reuses the Git primitives that quota-terminated doers use to
save their work. It is refused when Git is mid-operation, because committing
would record unresolved state as resolved. Refusals and commit failures keep
this ADR's blocked repair flow and delete nothing. Failure states that may remain
(staged work, or a WIP commit with residue) are tracked in `TECH_DEBT.md`.

## 2026-09-30 Amendment: Claim-Time Rebase of Rejected Work

A rejected re-claim, including await-verdict's automatic one, kept its old
base, so rework and any work bound to the exact HEAD ran on stale integration
until submission rebased it (operator note D95). A rejected claim that reuses
its worktree now rebases it onto the integration commit captured for the claim
when integration has advanced from `base_commit`, and records the old HEAD and
target in the claim history.

Unlike the preserved claim above, this rebase is best effort. A conflict, a
refusal before the rebase starts, or tracked uncommitted work keeps the branch
and `base_commit` and records `rebase_skipped`. The doer continues rejected work
it already owns and meets the conflict at its own rebase or at submission, so
blocking a rework loop on it would cost a repair cycle for no safety gain. The
claim fails closed only when Git state after the attempt is unknown, or does
not match the pre-rebase HEAD and branch. There is no integration-ref equality
check at assignment: a later integration commit makes the base less fresh, not
invalid. The rebase target always descends from the old base, so a claim that
fails after rebasing leaves a branch the next claim still validates.

## Consequences

Positive:
- Blocked tasks can see artifacts introduced by repair/fill-gap tasks before resuming.
- Normal scheduler claimability becomes the default recovery path.
- Sandboxed agents do not need to reason about live process state to resume repaired work.
- `--assign-to` remains available for operators or controlled direct-resume flows.
- Rebase conflicts stay attached to the blocked repair flow with actionable metadata.

Trade-offs:
- `unblock-task` now owns more preserved-worktree validation and rebase behavior.
- Rebase conflicts add another blocked-task repair state operators and agents must understand.
- Successful unblock can change a task's base commit, so state and prompt guidance must make that boundary explicit.

## Alternatives Considered

1. Require `--assign-to` for all unblocked work.

Rejected because direct assignment is inconvenient and confusing for sandboxed agents that cannot reliably inspect process state.

2. Make agents manually rebase preserved worktrees before unblocking.

Rejected because the need to rebase is part of the blocked-task repair boundary and should be validated by the operation that restores claimability.

3. Move unblock-time rebase conflicts to integration failure.

Rejected because these conflicts are repair-specific and should remain `BLOCKED` with repair metadata until the blocked task can be safely resumed.

4. Treat dependency repair as sufficient without rebasing.

Rejected because a preserved worktree may not contain the newly merged artifact even after the dependency that created it is satisfied.

## Relationship to Prior Decisions

Extends ADR-0063 (Blocked Task Alerts and Re-Wake) by making repaired blocked tasks resumable through normal claimability. Complements ADR-0075 (Retarget Dependency Repair), which fixes stale dependency edges before unblock. Relates to ADR-0077 (Dependency Edge Canonicalization) because unblock requires direct merged dependencies before restoring claimability.

---
*Reconstructed from commit b3d44346 and user context (2026-06-02)*
