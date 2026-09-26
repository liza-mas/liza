# 159 - Orchestrator Plan Hand-off Disposition

## Status

ACCEPTED — implemented 2026-09-24. Narrows automatic transition creation from
a merged plan; operator resume and `proceed` keep their authority except over a
human hold.

Amended by [ADR-0161](0161-plan-declared-replacement.md): the classifier also
weighs the originals a plan's outputs `supersede`.

## Context and Problem Statement

A merged plan's `output[]` becomes coding tasks after a `PLANNING_COMPLETE`
checkpoint is resumed. The checkpoint is labelled "for human review", but under
`auto_resume` any supervisor's pause gate resumes it within seconds, and the
resume itself runs the transitions. The orchestrator's `PLANNING_COMPLETE` turn
only ran `update-sprint-metrics` and `sprint-checkpoint`; its self-validation
gates covered only tasks it added itself. The plan-to-coding hand-off therefore
had no owner beyond the code-plan reviewer.

In an operator run on 2026-09-24, two merged plans crossed it with defects a
pre-checkpoint check would have caught: validation declared through hooks whose
file filters matched none of the listed paths, and tests confined to a module
the submission admission does not recognize. Plans also scoped runtime
provisioning out, so each live-validation child blocked on work only a human
could do.

The orchestrator could not route a defect either: `replan`, the only operation
that stops a merged plan's children without creating them, required the sprint
to be at `CHECKPOINT`, which auto-resume closes before the orchestrator can use
it, and it carried no reason for the next planner.

## Considered Options

1. **Prompt-only review** before the checkpoint.
2. **Deterministic engine dry-run** of plan executability before the checkpoint.
3. **Orchestrator disposition in state**, enforced by the transition executor.

## Decision Outcome

Chose **Option 3**.

- **Domain.** A transition needs a disposition iff it is `manual`, its
  cardinality is `per-subtask` or `one-to-one`, and its source is a planning
  pair whose task has `output[]`. Auto-only, many-to-one and empty-output
  sources keep their behavior. The disposition gates the transition, not the
  task: an auto transition from the same plan running first leaves the
  gated one pending.
- **Disposition.** `plan_check` on the merged task: `passed`, or `held` with the
  human action awaited (`ask`). The task ID is the plan's identity: output is
  fixed after merge and every correction goes through `replan`, which mints a
  new ID without a disposition.
- **Commands.** `plan-check <id> --pass | --hold <ask>` (a role configured
  with the orchestrator type, bound to its registration generation) and
  `plan-check <id> --clear` (operator only, refused from an agent session).
  A defect the planner can fix goes through `replan <id> --reason`, now
  allowed at `IN_PROGRESS` as well as `CHECKPOINT` (the no-children
  precondition under the lock is what makes it safe). The reason is appended
  to the replacement's description.
- **Sticky hold.** Pass, a different hold and replan are refused on a held
  plan; only an operator clear releases it. Hold replays with the same ask; a
  pass may be tightened to a hold before transition.
- **Classification.** One classifier (`needs_review`, `passed`,
  `waiting_upstream`, `needs_reconciliation`, `held`, `out_of_domain`) feeds
  wake rendering, the `PLANNING_COMPLETE` verifier, plan-check admission and
  transition admission. A plan is admissible only when every in-domain
  planning dependency has transitioned or is itself admissibly passed; a
  replanned or held upstream makes a passed consumer `needs_reconciliation`
  (replan or hold it), an unreviewed one makes it wait. Blockers carry the
  upstream chain. A missing or terminal `supersedes` original is a blocker
  that refuses a pass and makes a passed plan `needs_reconciliation`; an
  original still in flight makes a passed plan wait (ADR-0161).
- **Admission.** Automatic paths (auto-resume, orchestrator and reviewer
  PreWork) expand an in-domain plan only when it is classified `passed`, judged
  under the lock that creates the children. An operator resume or `proceed`
  expands undispositioned plans as before, but never a held one.
- **Wake and recovery.** Held plans do not wake `PLANNING_COMPLETE`; they raise
  one `AWAITING HUMAN` alert naming the ask and the clear command, and keep
  the sprint open: while one is held, no sprint- or coding-complete wake
  fires, so checkpoint and auto-resume cannot cycle on it. Passed plans
  still wake it until they transition, rendered as checkpoint-only, so a crash
  between a pass and the checkpoint recovers without re-review.
- **Verifier.** A `PLANNING_COMPLETE` turn passes when every plan eligible at
  wake time is dispositioned (by its wake-time class) and admissible plans were
  checkpointed during the turn. The self-heal checkpoint on failure is now
  harmless: it cannot expand an undispositioned plan.

## Rationale

Option 1 cannot bind the three paths that create children (resume, orchestrator
PreWork, reviewer auto pass), all of which scan every merged task under the
lock: a self-heal checkpoint or a plan merged mid-review would be expanded
without review. Option 2 would have to interpret hook configurations and
producer semantics, which vary per project (GUARDRAILS G1.1); the checks that
could be mechanical (test-file admission, acceptance headings) belong at
add/claim time, not here. Option 3 leaves judgment in the prompt and puts the
resulting decision where every creation path already looks.

The operator exception keeps `CHECKPOINT` meaning what it did for a human who
resumes it: resuming is the review. A hold is different — it records that a
human action is still missing — so no path overrides it.

## Consequences

- Architecture and epic plans with a manual per-subtask hand-off are also
  dispositioned; the review checks for coding children pass trivially there.
- If no orchestrator dispositions a plan, automatic expansion stalls for it;
  the `PLANNING_COMPLETE` wake re-fires, and an operator resume or `proceed`
  still expands it.
- A passed plan whose transition keeps failing re-wakes `PLANNING_COMPLETE`
  as checkpoint-only work, as an unconsumed plan did before.
- State decoding is not strict, so an older binary keeps `plan_check` in the
  task's inline extras but ignores it: it expands every plan, held ones
  included.

## Relationship to Prior Decisions

Complements the code-plan reviewer's executability checks (commit
`849f292a7`, the reviewer's duty) with the orchestrator's hand-off duty. Uses
the transition checkpoint model of the sprint governance protocol unchanged:
checkpoints still gate creation; this decision only narrows what automatic
resumption may create.

## Evidence and Reconstruction

Operator notes D73 (with D52, D63, D65, D68, D74, J67); adversarial-pairing
blackboard `specs/adversarial-pairing/20260924-D73-check-merged-plan.md`, plan
revisions 1–4 and review findings R1–R7.

---
