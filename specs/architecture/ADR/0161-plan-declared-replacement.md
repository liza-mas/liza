# ADR-0161: Plan-Declared Replacement

## Status

ACCEPTED — 2026-09-26. Amends [ADR-0143](0143-transactional-task-replacement.md)
(a second atomic replacement route; executing-consumer refusal) and
[ADR-0159](0159-orchestrator-plan-handoff-disposition.md) (classification).

## Context

`replace-task` made one-to-one replacement atomic, but corrective plans replace
several tasks through their generated children. Plan outputs had no field
naming the task they replace, so the transition that generated the children
could not retire the originals. Retirement was left to a later manual
`supersede-task` per original. When that step failed or was forgotten, the
originals stayed claimable beside their replacements, with their consumers
still pointing at them, and validation saw two unrelated tasks. A run observed
this in every plan-generated correction in two days. It saw 10 hours of
claimable duplicates and work delivered twice. Every original was
supersede-eligible when its replacement was generated.

`replace-task` cannot close this gap: it creates a new task and refuses an
existing ID, so it cannot adopt a generated child. The orchestrator's lineage
policy pointed at it anyway.

Separately, a dependency update that gave an executing task an unmet
dependency was rejected only by full-state validation, after mutation, as
`INVALID_INPUT/correct_input`. Retrying cannot succeed while the task executes.

## Decision

- `output[].supersedes` names one existing task an output replaces; several
  outputs may name one task to split it. The field reuses the task-level
  `supersedes` lineage that `replan` already sets.
- The per-subtask transition that generates the children also sets each
  child's `supersedes` and retires each original with all its children through
  the audited supersession path, which retargets consumers. It runs on a deep
  copy of the state (`db.CloneState`), validates it whole, and adopts it only on
  success. The shared transition pass reports a refusal and continues with
  other transitions, so without the copy a retirement made before a later
  failure would persist.
- Eligibility is `replace-task`'s source rule, judged three times:
  - at `set-task-output`, which refuses impossible targets;
  - by hand-off classification, where a missing or terminal original refuses a
    pass or needs reconciliation, and an in-flight one makes a passed plan wait;
  - under the generation lock.
- Kind deduplication ignores named originals, and so do a replacement's
  inherited phase-gate dependencies: a corrective plan usually depends on the
  plan it corrects. A non-replacing output keeps them, retargeted to the
  replacements. Crash recovery leaves an original already retired by its
  children alone and refuses a live one under the executed marker.
- Every dependency update (`retarget-dependency`, `apply-dependency-repair`,
  `replace-task` consumers) refuses before mutation when it would give an
  executing task an unmet dependency: `ALREADY_TRANSITIONED/stop`,
  `details.prerequisite = consumer_not_executing`.

## Consequences

- A corrective plan's replacement is committed with its children or not at
  all, and is visible in the plan's `transition_executed` event.
- Refusals reach the orchestrator before generation (`PLANNING_COMPLETE`
  reconcile list) and the operator at generation (transition failures).
- A replacing transition costs one reflective state copy and one full-state
  validation. Pre-existing unrelated corruption blocks it, as it blocks
  `replace-task`.
- Ceilings: an original named by a passed plan stays claimable until the next
  transition pass; a replacement stated only in prose remains undetected; a
  generation refusal of a passed plan on the automatic path (pre-existing
  corruption, planner-authored cycle) is only logged. All are recorded in
  `TECH_DEBT.md` with payback triggers.

## Alternatives Considered

- **Detect prose-declared duplicates.** Rejected: the prose is inconsistent
  ("Replaces X." or a lowercase "replaces X" mid-sentence), and originals and
  replacements share no structured scope key.
- **Transaction only, without classification.** Rejected: on the automatic
  path a refusal would become a silently stalled transition.
- **Corrective plans without auto-generation, then `replace-task` per output.**
  Rejected: it keeps one manual step per original, the failure being fixed.

## Evidence

An operator defect register (D13, D26, D53, D62) and that run's `state.yaml`,
read for its three plan-generated corrections: each corrective plan's outputs
declared "Replaces <original>" in prose only, and every original was
supersede-eligible at its plan's `transition_executed` time. Regression tests:
`internal/ops/plan_replacement_test.go`,
`internal/ops/dependency_update_executing_test.go`.
