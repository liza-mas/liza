# 157 - Blocked-Assessment Wait-For Set

## Status

ACCEPTED — implemented 2026-09-23. Amends ADR-0141: when an assessment declares
an awaited set, that set replaces the `descendants` fingerprint input. Amended by
ADR-0158: the set is one all-of wait that covers its members' dependency
records and carries forward within a BLOCKED episode.

## Context and Problem Statement

ADR-0141 made the `descendants` input part of the blocked-assessment
fingerprint, so new or failed generated work under a task's dependencies wakes
the orchestrator. In a September operator run, a BLOCKED plan waited for one
specific generated coding task. The plan's merged dependency had generated
several coding tasks, and each merge moved a descendant from pending to
satisfied. That changed the digest and woke the orchestrator, which had nothing
to do until the one task the plan needed merged.

Expressing the wait as a dependency edge was not possible: a task may not
depend on a task whose role pair is downstream of its own
(`IsRolePairDownstream`, enforced by state validation and `unblock-task`), and a
plan is upstream of coding. The wait therefore lived only in prose, which the
fingerprint cannot read.

## Considered Options

1. **Allow backward edges for holds.**
2. **Hash only whether any descendant is still pending**, instead of each
   descendant's outcome record.
3. **Record a wait-for set on the assessment** and, when present, fingerprint
   those tasks instead of the descendants.

For deadlock protection, since the set is not an edge:

- **(a)** Add awaited sets to the shared dependency cycle check run by every
  graph-editing operation.
- **(b)** Reject a deadlocking set when the assessment is written, and have the
  wake reader ignore a set that has since become circular.

## Decision Outcome

Chose **Option 3** with **(b)**.

- **Declaration.** `assess-blocked --awaits <id>[,<id>]`, in the note or the
  reconcile form. IDs are trimmed and stored as a sorted set; repeats merge.
- **Fingerprint.** A non-empty set replaces `descendants` with `awaited`: the
  sorted IDs plus the existing outcome-record projection over each ID's
  replacement path. The other five inputs are unchanged, so direct dependency
  outcomes, human notes and the task's own changes still wake it. Without a
  set the material is byte-identical to ADR-0141's.
- **Persistence.** `awaited_tasks` lives on the latest assessment only, pruned
  from earlier entries with the digest. An assessment without `--awaits`
  clears it. The IDs are part of lifecycle request identity, omitted when empty.
- **Write validation.** After replay and status checks, before the no-change
  comparison: no self-wait, every ID exists, each has pending work on its
  replacement path, and the wait does not lead back to the task. The deadlock
  search follows effective dependencies through supersession, stops at merged,
  abandoned and superseded work, and follows awaited sets only of tasks that are
  BLOCKED now. A rejection names the cycle or the settled IDs.
- **Reader fail-open.** The wake reader ignores a malformed set or one that now
  leads back to the task; the digest then differs and the task wakes.

### Accepted limits

- Write-time protection is best-effort: a later graph edit can close a cycle.
  The reader's fail-open bounds the cost to a wake, but that wake carries no
  reason. The prompt tells the orchestrator to repeat the same `--awaits` after
  an unexplained wake, which is rejected with the cycle.
- A task that is unblocked and re-blocked before being reassessed still carries
  its previous episode's set. Its own status change wakes it, and the next
  assessment replaces the set, but until then the deadlock search follows the
  stale set and can reject another task's valid `--awaits`. Following a set
  only when its assessment postdates the task's latest move into BLOCKED would
  close this; it was deferred as needing an unblock, a re-block and a crossing
  wait inside one assessment gap.

## Rationale

Option 1 inverts scheduling: a plan depending on coding work would make the
plan unclaimable until code it is meant to shape had merged, and every
direction check would need an exception. Option 2 loses identity: two different
sets, or one failure among still-pending tasks, hash the same while any work is
pending, so a changed wait would be suppressed as `NO_CHANGE`. Option 3 keeps
per-task outcome records, so settling or failing any awaited task still wakes
the task, and scopes the change to tasks that declare a set.

The set belongs on the assessment rather than the task: it is the
orchestrator's disposition, with the same lifecycle as the digest, reconsidered
at each assessment and pruned with it. No new task field or schema migration is
needed.

(a) would put a non-edge into the invariant every graph operation checks,
widening each one's failure surface for a hint that only suppresses wakes. (b)
keeps the check in one writer, and the fail-open reader keeps a missed cycle from
turning into a permanent silence.

## Consequences

- A blocked task waiting on named later-stage work wakes once per awaited
  task's settle or fail, plus the usual dependency, human-note and self changes,
  instead of once per generated descendant.
- The `BLOCKED_TASKS` wake prompt routes a wait on existing unfinished work to
  a legal edge when direction and acyclicity allow, to `--awaits` when direction
  forbids the edge, and to breaking the cycle otherwise.
- Existing digests remain valid; no rebaseline wake follows the upgrade.

## Relationship to Prior Decisions

Amends ADR-0141 (Blocked-Assessment Idempotency): the six-input fingerprint and
single shared read/write predicate stand, with `awaited` substituting for
`descendants` when declared. The writer's comparison still covers every reader
change; separately, awaited-set validation makes a candidate ineligible when it
keeps a set whose work has settled or that has become circular.

## Evidence and Reconstruction

Commits `cd1fb1802` (awaited set, validation, fingerprint and reader) and
`8958cd2ff` (wake prompt). The maintainer chose Option 3 before plan review and
(b) after two review rounds; the re-block limit was accepted in code review.

---
*Recorded from the implementing commits (2026-09-23).*
