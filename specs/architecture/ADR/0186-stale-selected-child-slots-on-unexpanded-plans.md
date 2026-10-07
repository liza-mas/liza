# 186 - Stale Selected Child Slots on Unexpanded Plans

## Status

ACCEPTED. Amends [ADR-0185](0185-stale-provider-declarations-on-unexpanded-plans.md)
(Decision 1 and Alternative 3).

## Context

ADR-0185 releases an unexpanded plan's output declaration that names a
provider directly, but keeps every selected child slot holding: after a child
replan, lineage resolution would follow the slot to the successor and hide the
staleness. In practice a corrective plan's hand-off could not supersede a
selected child because an unexpanded consumer declared the child's parent
(D-64). Recovery again needed a cascading consumer replan or a human.

Only `replan` creates lineage: it alone sets the `replanned` marker that
`ReplanSuccessor` follows, and it accepts only a MERGED task whose hand-off is
not retired. A child that is superseded, cancelled or plan-check-replaced is
retired permanently and can never gain a successor, so its slot keeps
projecting to a retired task.

## Decision

1. **Retirement.** An unexpanded plan's output declaration does not hold a
   selected child that is retired permanently (plan-declared replacement,
   supersession, cancellation, plan-check replacement). A child retired by
   replan still holds. Every other holder is unchanged.
2. **Detection.** Validation, hand-off classification and generation treat a
   retired selected child like a retired provider: state validation accepts it
   only in an unexpanded plan's output (provenance, bounds and cycle checks
   still apply), classification reports it as a reconcile blocker naming the
   child, and every generation path refuses the entry as a hand-off failure.
3. **Recovery.** As ADR-0185: replanning the stale plan re-authors its output.

## Consequences

- A selected child can be retired permanently under an unexpanded consumer.
  Each stale consumer then needs one replan, in any order.
- Retirement now depends on how a child is retired: cancelling or superseding
  it is accepted where replanning it is refused, because only the replan would
  hide the staleness.

## Alternatives Considered

1. Also release slots under replan and flag a lineage-resolved slot as stale.
   Rejected: it breaks ADR-0184 slot-following for declarations authored after
   the child replan, and telling them apart needs per-declaration authoring
   evidence.
2. Keep the hold and replan the consumer first. Rejected: it restores the
   cascade ADR-0185 removed.
