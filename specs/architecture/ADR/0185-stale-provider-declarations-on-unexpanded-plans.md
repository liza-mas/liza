# 185 - Stale Provider Declarations on Unexpanded Plans

## Status

ACCEPTED. Amends [ADR-0181](0181-provider-output-dependencies.md) (Recovery and
Compatibility) and [ADR-0184](0184-replan-lineage-dependency-repair.md)
(Consequences).

## Context

ADR-0181 refuses to retire or replan a provider while any live declaration names
it, and prescribes consumer-first recovery. A MERGED plan whose hand-off has not
run (unadmitted, or its transition failed) counts as live, so its output
declarations hold the provider. Such a plan has no children, and its reviewed
content must be re-authored once the provider changes anyway, because output
indexes do not transfer to a replacement. The refusal protected nothing the
plan could still use, but it imposed depth-first cascading replans. In
practice, operators had to escalate each one as a human decision, and in the
hand-off path it caused a planning re-wake loop.

The same plan's `depends_on` edge to a replanned upstream already goes stale
lazily: hand-off classification marks it for reconciliation, and a pass is
refused.

## Decision

An **unexpanded plan** is MERGED, has output, has no executed transition marker
and no retired hand-off, and no task names it as a parent.

1. **Retirement.** An unexpanded plan's output declaration that names the
   provider task directly does not hold it, whatever the retirement (replan,
   plan-check replacement, supersession, cancellation). Every other holder is
   unchanged: non-terminal task-level declarations, outputs of plans not yet
   MERGED, plans with a marker or any child, and every selected child slot.
   After a child replan, replan lineage would silently follow the slot.
2. **Retention.** The stale declaration stays unchanged as audit and reauthoring
   evidence. State validation accepts a retired provider only in such a plan's
   output, and only while the plan stays unexpanded.
3. **Classification.** Hand-off classification reports an output declaring a
   missing or retired provider as a reconcile blocker. A passed plan needs
   reconciliation; an unreviewed one cannot pass. A human hold still takes
   precedence and is released only by an operator clear.
4. **Generation.** A per-subtask hand-off refuses an output entry that declares
   a retired provider, before any child exists, on every path (reviewed and
   operator transition passes, manual proceed). The refusal is recorded as a
   hand-off failure, so the plan shows under repair instead of re-waking.
5. **Recovery.** Replanning the stale plan retires its output; the replacement
   declares the provider's replacement. `replan` warns about each plan it
   leaves stale.

## Consequences

- A provider whose consumers have not expanded can be replanned or replaced
  directly. Each stale consumer then needs one replan, the same reauthoring
  cost as before, in any order and without a human fork.
- A stale declaration is never copied to a child. Generated (non-terminal)
  declarations on a retired provider remain invalid.
- A consumer's own task-level declarations are still cloned verbatim by its
  replan. If they name the retired provider, that replan is refused (tracked
  in `TECH_DEBT.md`).

## Alternatives Considered

1. Cascade: replan every unexpanded consumer inside the retiring transaction.
   Rejected: it mints planning work silently, adds side effects to transition
   passes, and removes the orchestrator's hold option.
2. `retarget-provider-declaration` from a provider to its replacement. Rejected:
   indexes are not transferable, and the replacement has no reviewed output at
   retirement time.
3. Also release selected child slots. Deferred: after a child replan, lineage
   resolution hides the staleness, and no observed incident used that shape.
