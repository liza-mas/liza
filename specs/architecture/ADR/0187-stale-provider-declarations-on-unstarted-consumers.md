# 187 - Stale Provider Declarations on Unstarted Consumers

## Status

ACCEPTED. Amends [ADR-0185](0185-stale-provider-declarations-on-unexpanded-plans.md)
(Decision 1) and [ADR-0186](0186-stale-selected-child-slots-on-unexpanded-plans.md)
(Decision 1).

## Context

ADR-0185 and ADR-0186 release an unexpanded plan's output declaration from
holding a provider or selected child retired permanently, and keep every
non-terminal task-level declaration holding. In practice a corrective plan's
hand-off could not supersede a selected child because an unclaimed draft code
plan, generated earlier from an expanded architecture plan, declared the
child's parent (D-65). Nobody had started the draft, its declaration could
never resolve again (output indexes do not transfer and only replan creates
lineage), and readiness already refused to claim it. The hold protected nothing
and deadlocked the replacement.

The refusal also named only the first holder, so holders on one chain surfaced
one per fix-and-deploy cycle (D-61, D-64, D-65).

## Decision

1. **Unstarted consumer.** A non-terminal task is unstarted when it is in its
   role pair's initial status or BLOCKED, was never claimed, and has no
   assignee, lease, worktree or pending hand-off.
2. **Retirement.** An unstarted consumer's task-level declaration does not hold
   a provider, or selected child, retired permanently (supersession,
   cancellation, plan-declared or plan-check replacement). A replan of either
   still holds. Started consumers hold as before.
3. **Escalation.** The retiring transaction blocks each such consumer that is
   still in its initial status, naming the declaration and the retired task,
   and asks the orchestrator to replace or cancel it. The BLOCKED_TASKS wake
   delivers it. Unblocking is refused while the declaration is stale, and
   readiness never offers the consumer for claim.
4. **Retention.** The declaration stays unchanged. State validation accepts a
   retired provider or child in an unstarted consumer's task-level
   declarations; bounds, provenance and cycles are still checked. `replan`
   refuses to copy a stale task-level declaration to a replacement.
5. **Diagnostics.** A refused retirement names every live holder.

## Consequences

- A provider whose only consumers are unstarted can be replaced or retired
  directly. Each stale consumer then needs one replacement, which the
  orchestrator is woken for.
- Retirement now blocks a third task. This is an escalation the orchestrator
  must act on anyway, not new work minted silently (ADR-0185 Alternative 1).

## Alternatives Considered

1. A dedicated orchestrator reconcile section and wake for stale consumers.
   Rejected: it duplicates BLOCKED triage and needs its own trigger and
   fingerprinting.
2. Cancel or supersede the stale consumer in the retiring transaction.
   Rejected: it drops the work item without a decision.
3. Retire the consumer first. Rejected: it restores the per-holder cascade
   ADR-0185 removed.
