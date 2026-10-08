# 189 - Stale Provider Refusals Stay Routed to the Orchestrator

## Status

ACCEPTED. Amends [ADR-0185](0185-stale-provider-declarations-on-unexpanded-plans.md)
(Decision 4) and [ADR-0159](0159-orchestrator-plan-handoff-disposition.md)
(Consequences: equal inputs suppress planning wakes).

## Context

ADR-0185 and ADR-0186 classify an unexpanded plan whose output declares a
missing or retired provider, or a selected child retired permanently, as stale:
a pass is refused, a passed plan needs reconciliation, and the repair is the
plan's replan. Generation refuses the stale entry on every path and records the
refusal as a hand-off failure "so the plan shows under repair instead of
re-waking". A current failure makes the plan ineligible for
`PLANNING_COMPLETE`, which removes it from the wake count, the hand-off prompt
lists and the verifier.

Reviewed admission never attempts a stale plan, so the failure is recorded only
when generation runs anyway: an operator resume or `proceed`, which admits
every unheld plan, or an earlier transition in the same pass that retires the
provider after admission. In the D-71 run an operator resume refused two stale
plans; both then vanished from every orchestrator prompt and stranded until an
operator relayed notes. The failure's input fingerprint holds no provider
state, so nothing re-admits the plan short of the replan nobody was asked for.

## Decision

1. **Routing.** A pending, reviewed-hand-off plan whose output declares a stale
   provider stays `PLANNING_COMPLETE`-eligible while a matching failure is
   recorded. The classifier routes it as before: a passed plan is listed for
   reconciliation, an unreviewed one for review with the blocker that refuses
   its pass. The verifier requires a replan or a hold.
2. **Failure unchanged.** The refusal is still recorded with its fingerprint.
   It suppresses automatic retries, stays repair-visible in `status` and in the
   `PLAN HANDOFF FAILED` alert, and keeps completion and integration unsettled.
   `status` lists the plan as failed, not as ready.
3. **Other refusals unchanged.** A refusal whose plan declares no stale
   provider still suppresses the wake until its inputs change.

## Consequences

- A stale consumer refused under operator admission is replanned by the
  orchestrator without a human or operator note.
- If the replan is itself refused (for example by an unstarted consumer's
  task-level declaration, D-72), the orchestrator holds the plan, which raises
  an `AWAITING HUMAN` alert instead of a silent strand.
- The wake re-fires on unchanged inputs until the plan is replanned or held,
  the same contract as any plan needing reconciliation.
- The alert text still offers input repair and plan-check replacement; it does
  not name the replan.

## Alternatives Considered

1. Skip stale plans in operator admission. Rejected as the fix: it misses the
   same-pass retirement path and failures already recorded in existing state.
   It would only spare a pointless attempt.
2. Route every reconcile class despite a recorded failure. Rejected for now:
   an upstream replan already changes a selective-inheritance fingerprint, and
   a refusal on a terminal `supersedes` original has not been observed.
3. Stop recording the stale refusal. Rejected: operator retries would lose
   their evidence and duplicate suppression.
