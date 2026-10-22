# 196 - Plan Replacement Preserves Reservation Placement

## Status

ACCEPTED. Amends [ADR-0161](0161-plan-declared-replacement.md) and
[ADR-0195](0195-provider-reservations-for-inserted-writers.md). Amended
2026-10-22 (D-172).

## Context

D-89: an architecture plan that replaced a reserved code plan was refused by
the permanent-provider-retirement barrier. Its fresh successor already existed
in the candidate, but the reservation still named the original. Generation also
dropped the original's outgoing reservations and output cap, losing its place
between earlier and later writers.

## Decision

Before retiring any plan-declared original, prepare the complete replacement
map in the same validated candidate transaction:

- A unique child must belong to the generating plan, name the original in
  `supersedes`, and have the same role pair. It inherits the original's own
  `provider_reservations` and `max_outputs`, keeping its new plan provenance.
- Retarget every live unsatisfied reservation whose effective provider is a
  mapped original. Resolve existing replan/replace-task aliases first and
  collapse duplicate reservation keys. Include retiring originals and newly
  generated successors: if B reserves A and both are replaced, B and B's
  successor must both reserve A's successor before ordinary retirement and
  reservation-drop checks run.
- Each rewrite records `dependencies_rewritten`, actor `system`, operation
  `plan-declared-replacement`, and `provider_reservation_retargeted` with
  `previous_provider_task`, `provider_task`, and `transition`. The terminal
  original retains the transaction-final reservation; its previous target
  remains in history. Direct edges are not rewritten by this event.
- A split is refused if the original has an outgoing reservation, nonzero cap,
  or a live unsatisfied incoming reservation. Ordinary unplaced splits remain
  supported. An all-work reservation names one provider; splitting it would
  require an explicit fan-in design.

Keep permanent-retirement and typed positional-declaration barriers, blocked
recovery limits, supersession routing, and full candidate validation. Any
failure discards all children, caps, rewrites, history, and retirements together.
Reservations continue to wait for the successor's handoff and **all** generated
work, including outputs authored after transfer.

Completed replay adds no placement or audit effects. Missing-child recovery
inherits placement only for children recreated in that pass; it preserves
surviving children's later authorized changes and terminal originals' history.
Provider-retirement failure fingerprints include a placement-policy
discriminator: pre-fix observations retry once under the new policy; unchanged
new-policy refusals remain suppressed.

## Consequences

Plan replacement preserves an inserted writer's position atomically without
an operator release/re-reserve sequence. No schema or CLI changes are needed.
Typed output indexes do not follow plan replacements, and generic
`ProviderChildSuccessor` lineage is unchanged: a new plan has different parents
and cannot silently inherit an old reviewed slot's meaning.

## Alternatives Considered

- Generalize selected-child lineage to plan replacement: silently transfers
  positional scheduling intent despite different parent provenance.
- Release and recreate placement operationally: exposes an unordered interval
  and loses transaction atomicity.
- Allow placed splits: needs explicit all-successor reservation semantics,
  beyond the unique-successor correction required by D-89.

## Amendment (2026-10-22, D-172)

A correction's output superseded a child C of plan P while a writer reserved
P at C's transition. The transfer above retargets only reservations whose
effective provider is the original, so this one still selected C; full
candidate validation then refused the retired child. That refusal was not a
classified hand-off failure: it recurred at every pass, the plan kept waking
the orchestrator, and only a WARN line named it. `plan-check --pass` had
admitted the plan because it judges the current state.

- **Barrier.** The permanent-retirement barrier also holds an unsatisfied
  reservation that selects the retiring task as a generated child of its
  provider, named `<holder> provider_reservations on <provider> at
  <transition>`. A same-pair `replace-task` still passes: the slot resolves to
  its successor. A plan-declared replacement is refused as a
  `provider_retirement` hand-off failure, persisted and alerted like the
  others.
- **Pass.** `plan-check --pass` runs the barrier's reservation clauses, after
  this ADR's transfer, for each live original a per-subtask hand-off declares,
  and refuses with the holder named. Typed declarations are left to
  generation: an original retired earlier in the same hand-off stops holding,
  which only the ordered transaction sees. It is a snapshot, not a dry-run of
  the transition; generation stays authoritative.
- **Visibility.** A passed plan's gated transition skipped for a failure that
  no observation classifies raises one `PLAN HANDOFF FAILED` alert per task,
  transition and masked error. It keeps retrying.

The orchestrator repairs the placement with `reserve-provider` (ADR-0195 §5).
Carrying such a reservation to the successor automatically stays out of scope:
it would need reservations to follow plan-declared lineage (rejected above) and
an all-work meaning when P has other children (with D-167).
