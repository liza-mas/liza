# 131 - Serialize Repository Worktree Metadata Mutations

## Context and Problem Statement

Task worktrees isolate checked-out files, but their registration metadata belongs
to one repository. Concurrent claims could invoke `git worktree add` against
that shared metadata and fail intermittently on macOS. Different task IDs did
not make these operations independent.

Integration serialization already protected another shared resource: integration
ref advancement and shared-index synchronization. That lock did not cover
worktree creation, attachment, or removal. Fixing the metadata race required a
boundary shared by those operations without serializing entire task claims.

## User-Confirmed Intent

On 2026-09-06, the user supplied this historical intent: Serialization was chosen because it seemed more robust.

No specific retry comparison or rationale for the 30-minute timeout was supplied.

## Considered Options

The following are reconstructed comparisons, not documented historical rejections:

1. Retry failed Git commands. This reacts after contention and requires callers
   to distinguish retryable metadata conflicts from other failures.
2. Serialize whole claim and recovery workflows. This would also serialize
   surrounding work that does not mutate shared worktree metadata.
3. Serialize worktree metadata operations at the Git wrapper boundary. This is
   the implemented choice and gives callers one common critical section.

## Decision Outcome

Use a repository-scoped file lock at `.git/worktree-mutation` around the shared
metadata mutations in `internal/git/worktree.go`:

- `CreateWorktree`: the `git worktree add` that creates a task branch and checkout.
- `AttachWorktree`: the `git worktree add` that attaches an existing task branch.
- `RemoveWorktreeDir`: removal of the checkout and its worktree registration,
  including fallback filesystem cleanup.

Removal keeps task-local cleanup within the same critical section. If the
checkout directory has already disappeared, it clears only that task's stale
registration metadata. It does not run a global prune that could interfere with
another worktree being added.

The implementation uses the existing `filelock` abstraction with named operations
and a 30-minute acquisition timeout. The source establishes that value, but the
historical rationale for choosing thirty minutes is not recorded.

The lock covers metadata mutation, not an entire task lifecycle. Surrounding
claim orchestration, setup, and indexing remain outside this critical section.
Preliminary path checks and base-commit lookup also precede it; subsequent task
branch deletion is separate from `RemoveWorktreeDir`.

### Rationale

The repository is the sharing boundary even when each caller works on a different
task. Locking at the Git wrapper makes add, attach, and removal participate in
one protocol while retaining concurrency for work that does not touch their
shared metadata. Cross-process locking matches the independently running
supervisors that create the contention.

### Consequences

- Cooperating worktree operations no longer overlap their shared metadata
  mutations.
- Worktree provisioning can wait behind another metadata operation, but setup
  and indexing do not extend that wait.
- Direct external Git commands do not acquire this application lock; it is not
  a replacement for Git's own locking or a transaction over every repository
  operation.
- The timeout bounds acquisition waiting without explaining or changing the
  existing cleanup fallback behavior.

## Related Decisions

Extends [ADR-0022](0022-concurrency-hardening-singleton-blackboard-and-cas-merges.md).
[ADR-0112](0112-serialize-integration-working-tree-mutations.md) protects integration
ref/index mutation; [ADR-0125](0125-safe-project-reset-lifecycle-lock.md) protects
project cleanup against participating lifecycle work;
[ADR-0130](0130-generation-fenced-agent-authority.md) orders registration and
provider start for one agent. These locks protect different resources.

---
*Reconstructed from commit `698ad2ed` (2026-09-01) and
`internal/git/worktree.go`. ADR selection approved by the user on 2026-09-06;
serialization's robustness motivation was confirmed; historical alternatives
and the timeout rationale remain unconfirmed.*
