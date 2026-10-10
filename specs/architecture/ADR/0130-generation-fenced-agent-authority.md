# 130 - Generation-Fenced Agent Authority and Recovery

## Context and Problem Statement

A stable agent ID identifies a role instance across registrations, but cannot
distinguish a current supervisor from an older process using the same ID. Stale
supervisors could mutate replacement registrations, launch duplicate providers,
or interfere with ownership handoffs. Approved and rejected work could then
remain stranded despite valid review evidence or recoverable worktree artifacts.

Sandbox execution tends to confuse the operator agent; the user sought a more
robust basis for authority and recovery. PID observations are insufficient across
process namespaces: an observer
can report a registered process as absent while its heartbeat and lease remain
current. Recovery needed registration-specific authority without weakening
review or lease invariants.

## User-Confirmed Intent

On 2026-09-06, the user supplied this historical intent: Sandbox execution tends to confuse the operator agent; generation-fenced authority provides more robust operation in that environment.

No specific incident or intentional-debt classification for nil-authority compatibility paths was supplied.

## Considered Options

These comparisons are reconstructed, not confirmed historical alternatives:

1. Continue relying on agent ID, PID observations, and leases. These describe
   identity and liveness but do not fence an earlier registration's writes.
2. Check registration identity before an operation. A separate check leaves a
   replacement-registration race before mutation or provider start.
3. Carry registration generations and check them at the authoritative mutation
   and launch boundaries. This is the implemented choice.

## Decision Outcome

Mint an opaque random generation for each successful registration. Agent callers
carry `(agent ID, generation)`, and agent-authenticated lifecycle writes compare
both against current state inside the same `Blackboard.Modify` transaction that
performs the mutation. Effective-operation authorization remains independent;
generation ownership grants no additional RBAC permission.

A cross-process lifecycle lock for one agent ID orders registration against
provider start. Registration takes this lock before the blackboard lock. Launch
checks current authority while holding the lifecycle lock and reaches the
provider start/session-creation boundary without running provider effects inside
`Blackboard.Modify`. Built-in providers wait outside both locks. The legacy
compatibility adapter holds the lifecycle lock for its entire blocking call
because that interface exposes no narrower start boundary.

Ownership remains lease-first. A fresh heartbeat and unexpired lease occupy
singularity and role capacity despite namespace-unverifiable PID evidence.
Diagnostics retain the registered PID and report correlated observation or
explicit uncertainty; such observation does not authorize takeover.

Recovery preserves work and authority together:

- A deterministic current-generation reviewer may finish an already-approved
  merge after the final approver exits, preserving approval actors, quorum,
  provider diversity, review commit, and reviewer role.
- Rejected-task release and reclaim treat owner, lease, worktree, base commit,
  physical artifact, and reviewer affinity as one locked tuple. Recovery reuses
  valid artifacts and refuses to delete unclassifiable ones.

### Rationale

A generation distinguishes successive owners of the same ID. Transactional
comparison closes the stale-writer race; launch serialization closes the
separate check-before-start race. Neither requires treating uncertain process
observations as permission to discard live ownership.

### Consequences

- Stale authenticated callers fail before mutating replacement state.
- Recovery can retain reviewed work without impersonating its original approver.
- Token propagation and per-agent locking add lifecycle complexity; the legacy
  adapter holds its lock longer than built-in providers.
- Nil-authority compatibility paths remain in `lifecycle_authority.go` for
  legacy internal callers. This is not universal authentication of every
  internal mutation.

## 2026-09-28 Amendment: Merge Takeover Settles Preparations by Effect

The approved-merge takeover could not finish a merge whose `wt-merge`
preparation a departed approver had left. Resumption required the preparer to
be the requester, and a missing registration does not count as retirement.
Every inheritor therefore requeried forever (operator note D106).

A preparation seen under the task review lock belongs to an ended invocation,
because the merge holds that lock for its whole call. It is therefore settled by
effect, not identity:
- a receipt-proven merge finishes;
- an approved commit absent from integration retires the preparation and merges
  afresh;
- an approved commit reachable without an attributable receipt stays fenced for
  inspection.

Exactly-once still rests on the locked approved-status recheck. The inheriting
reviewer also wakes from its wait when the set of merges it owns changes.

## 2026-10-10 Amendment: Lease-less Registrations Stay Owned for One Liveness Window

Submit, verdicts, release and block clear the agent lease of a live
registration until its next heartbeat renews it. Registration ownership and the
agent-ID allocators read a nil lease as expired. So a pool-repair start inside
that window received the live agent's ID and took it over, and the fenced
supervisor lost its work context (operator note D-17 part B).

Ownership stays lease-first under the shared `models.AgentRegistrationLive`
rule. A recorded heartbeat with an unexpired lease is ownership, as before.
Without a lease, a heartbeat inside the liveness window (heartbeat interval +
grace) is ownership too. Registration collision, singularity and role-capacity
counts, pool-repair headroom, the watcher and both ID allocators apply it.
Process evidence stays diagnostic only. Rejected alternatives:
- release paths keep the agent lease: a recovered dead agent would then hold
  its ID and capacity for up to the full lease;
- a live matching PID wins: that contradicts the namespace rule above.

Cost: a dead agent that a recovery path released stays occupied until its
last heartbeat leaves the window, at most 180 s with the default interval.

## Related Decisions

Extends [ADR-0062](0062-ghost-agent-claim-prevention-and-ownership-reconciliation.md)
and [ADR-0083](0083-preserve-by-default-recover-task.md).
The per-agent lock is distinct from [ADR-0125](0125-safe-project-reset-lifecycle-lock.md)'s
project cleanup lock and [ADR-0131](0131-repository-worktree-mutation-lock.md)'s
repository metadata lock. See the
[supervision contract](../supervision-model.md#generation-fenced-recovery-contract).

---
*Reconstructed from commit `a22c1238` (2026-08-27), supporting fixes `866c903f`
and `586542f1` (2026-08-31), and the supervision contract. ADR selection approved
by the user on 2026-09-06; sandbox/operator robustness was subsequently
confirmed as the motivation. Historical alternatives remain reconstructed.*
