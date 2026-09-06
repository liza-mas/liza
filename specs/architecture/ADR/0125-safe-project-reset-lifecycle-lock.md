# 125 - Safe Project Reset Through a Lifecycle Lock

## Status

ACCEPTED — implemented 2026-08-10; backfill selected 2026-09-06.

## Context and Problem Statement

Cleaning up a previous run was a painful manual task for users, especially its
worktrees. Project cleanup and repeated initialization need to discard runtime
state and task worktrees. Deleting directories alone can leave registered Git worktrees
behind or race an agent registration, worktree creation, or recovery operation.
Those operations create resources that can outlive the blackboard state being
deleted. Confirmation based on an earlier directory listing is insufficient if
the targets change before execution.

The reset path therefore needs both an inspectable scope and coordination with
resource creation. This extends the setup/init boundary in
[ADR-0018](0018-two-step-deployment.md) without changing the preserve-by-default
task recovery policy in [ADR-0083](0083-preserve-by-default-recover-task.md).

## User-Confirmed Intent

On 2026-09-06, the user supplied this historical intent: Cleaning up a previous run was a painful manual task for users, especially worktrees. Leaving ordinary blackboard writes outside the lifecycle lock was a deliberate tradeoff.

A specific cleanup race incident and historical alternative evaluations were not supplied.

## Considered Options

The source establishes the implemented choice. These alternatives are
reconstructed comparisons, not a confirmed historical shortlist:

1. **Delete discovered directories immediately.** Simple, but neither protects
   concurrent lifecycle operations nor verifies ownership of registered worktrees.
2. **Serialize every blackboard mutation with reset.** Broad protection, but
   unnecessarily couples ordinary state updates to resource lifecycle management.
3. **Confirm a target plan, then revalidate under an exclusive lifecycle lock.**
   Coordinates precisely the operations whose external resources could be orphaned.

## Decision Outcome

Choose **Option 3**, implemented by the shared project cleanup service used by
cleanup and reinitialization.

```text
Read-only target plan → confirmation → exclusive lifecycle lock
                                         ↓
                               replan and compare targets
                                         ↓
                            remove worktrees, then directories
```

The plan identifies the project runtime and task-worktree directories, owned
registered task worktrees and branches, and live or uncertain agent registrations.
Worktrees outside the managed directory are excluded; registered worktrees
inside it must match the expected task-directory and task-branch conventions.

Execution acquires the project lifecycle lock, recomputes the plan, refuses live
or uncertain agents, and rejects changed targets. Registration, provisioning and
recovery take a shared lock; cleanup takes the exclusive lock. Shared holders
may run concurrently. Ordinary blackboard writes deliberately remain outside
this lock because reset is allowed to discard that state.

## Rationale

Ownership checks prevent reset from treating every Git worktree as disposable.
Revalidation binds execution to the confirmed target set. The shared/exclusive
lock closes the race between revalidation and cooperating resource-creating
operations without serializing normal agent work.

## Consequences

- Operators can inspect reset targets before authorizing deletion.
- Cooperating lifecycle operations cannot create orphaned resources during reset.
- Reset still destroys contents within its approved directories; target equality
  is not a file-content snapshot or backup.
- Unknown ownership requires intervention rather than optimistic deletion.
- Filesystem/Git deletion is sequential, not an all-or-nothing transaction; a
  failure can occur after earlier targets were removed.

The lifecycle lock protects reset against resource creation. It is distinct from
the integration ref/index lock in
[ADR-0112](0112-serialize-integration-working-tree-mutations.md) and the later
worktree metadata lock in [ADR-0131](0131-repository-worktree-mutation-lock.md).

## Evidence and Reconstruction

Verified against [project cleanup](../../../internal/ops/project_cleanup.go)
and [lifecycle locking](../../../internal/ops/project_lifecycle_lock.go).
Selection and the intent above were confirmed on 2026-09-06; a specific race
incident and historical alternative evaluations were not supplied.

---
*Reconstructed from commit 35fda601 (2026-08-10).*
