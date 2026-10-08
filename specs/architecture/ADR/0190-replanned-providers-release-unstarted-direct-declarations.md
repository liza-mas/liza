# 190 - Replanned Providers Release Unstarted Consumers' Direct Declarations

## Status

ACCEPTED. Amends [ADR-0187](0187-stale-provider-declarations-on-unstarted-consumers.md)
(Decisions 2 and 3).

## Context

ADR-0187 releases an unstarted consumer's task-level declaration only when the
provider, or selected child, is retired permanently; a replan of either still
holds. The hold's reason is replan lineage: a selected-child slot resolves to
the child's replan successor (ADR-0184 Decision 3), so releasing it would hide
the staleness. A direct declaration never follows lineage: output indexes stay
provider-scoped, and a replanned provider is retired for every check that
judges it. After a replan it is exactly as stale as after a permanent
retirement, so the hold protects nothing. Output declarations already release a
direct reference on any retirement (ADR-0185, ADR-0188).

In D-72 a stale plan, whose only repair is its replan, could not be replanned
because three never-claimed drafts declared it directly at task level. Each
would have needed a replacement first, cascading to its own consumers.

## Decision

1. **Release.** An unstarted consumer's task-level declaration does not hold
   the provider it names directly, however retired, replan included. A
   selected child retired by replan still holds, as do started consumers.
2. **Escalation.** A replan blocks each such consumer still in its initial
   status, naming the declaration and the replanned task, exactly as ADR-0187
   Decision 3 does for a permanent retirement. Retention, validation, unblock
   and claim refusals are unchanged.

## Consequences

- A stale plan whose consumers have not started can be replanned directly. Each
  stale consumer then needs one replacement or cancellation, which the
  BLOCKED_TASKS wake delivers.
- Task-level, unexpanded-plan and draft-output declarations now share one
  release rule: a direct reference on any retirement, a selected child only on
  a permanent one.

## Alternatives Considered

1. Let the direct declaration follow replan lineage. Rejected: output indexes
   are identities in one reviewed provider (ADR-0181, ADR-0184).
2. Have the orchestrator replace every consumer before the replan. Rejected: it
   restores the per-holder cascade ADR-0185 removed.
