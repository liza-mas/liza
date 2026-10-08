# 194 - RCA Evidence-Unavailable Route

## Status

ACCEPTED. Amends [ADR-0116](0116-orchestrator-classifies-defect-objectives.md)
(code-planner RCA exit and code-plan-reviewer RCA gates).

## Context

ADR-0116 defines what a valid root cause looks like for an `rca_required`
code-planning task, but gives the planner only one exit when the cause cannot
be established from evidence: mark BLOCKED rather than plan against a
hypothesis. It does not say why the evidence is missing or who can produce it.
Three situations collapsed into that one block:

- evidence obtainable by reading now, which the planner gathers itself;
- evidence obtainable only by executing or instrumenting the failing path,
  which the planner may not do (its planning boundary forbids implementation
  edits);
- evidence lost or not producible by agents, where only a human can choose
  between authorizing capture and accepting a defensive repair without
  attribution.

In D-81 a harness-repair planner blocked twice because the historical backend
behind a seal failure had never been captured. The orchestrator's blocked
routing had no matching row, so it recorded ten unchanged note-only
assessments over about 25 hours. No capture task was created and no human ask
was raised. The hold ended only when a human, prompted out of band, waived
attribution; the planner then produced an approved defensive plan within
minutes. Generic carriers existed (prerequisite tasks, dependency edges,
`assess-blocked --awaits`, `--human-action`). What was missing was the rule
that selects one of them for this case.

## Decision

1. **Planner.** When reads cannot establish the cause, the BLOCKED reason names
   the minimum missing observations and why reads cannot supply them, then
   chooses one of two routes. A capture an agent can run within authorized
   scope is named with its target and bound (runs or time). Otherwise
   (evidence lost, capture outside its authority, or a bound already
   exhausted), it raises `--human-action` to authorize capture or to waive
   attribution for a defensive repair. Planning a repair against a hypothesis
   remains forbidden.
2. **Waiver form.** Under a recorded human attribution waiver, the RCA cites
   the waiver and states the waived cause as not established. For that cause,
   mechanism and reproduction apply to its observed symptom path, and
   sufficiency names what the repair leaves open. The waiver covers only the
   missing attribution: causes established by evidence in the same plan (in
   D-81, the verified failed-template cache retention) stay established and
   pass every normal RCA gate. The code-plan-reviewer's "Unestablished cause"
   gate rejects a repair of an unestablished cause that cites no waiver, and a
   waiver plan that attributes the waived cause.
3. **Orchestrator.** A named in-scope capture becomes a separately reviewed
   capture task with `done_when` "the named observations recorded, or its bound
   exhausted with what was observed". It holds the owner as for any unfinished
   work: a dependency edge only where pipeline direction allows it and it is
   acyclic, otherwise `assess-blocked --awaits`. A coding-pair capture is
   downstream of code planning, so it uses `--awaits`. When the capture merges
   with the observations, the same owner is unblocked and judges the cause
   itself. Lost evidence, capture outside authority, failed capture or an
   exhausted bound raise `--human-action` instead. An evidence-unavailable block
   is never re-assessed unchanged.
4. **Unchanged.** The planner keeps its original repair obligation; a capture
   never replaces that deliverable. Commissioning and classification
   (ADR-0116) are unchanged, because the RCA owner, not the commissioner,
   discovers whether evidence is obtainable. Dependency-direction enforcement
   has no exception. No phase, role, task type or engine check is added.

## Consequences

- Each unestablished cause now has an owner: the orchestrator for in-scope
  capture, the human for authorization or waiver. A recorded waiver is a
  reviewable RCA form that keeps the waived cause explicitly unknown without
  discarding causes the evidence does establish.
- This is a mitigation. It cannot recover evidence that was never captured,
  and whether agents adopt the route is next-run evidence. Rendered-template
  tests prove only that the guidance is present and conditional.
- Prompt growth: the orchestrator BLOCKED_TASKS wake grows by 606 bytes, and
  the conditional RCA blocks of the code-planner and code-plan-reviewer grow by
  about 0.8 KB and 0.34 KB, only when `rca_required` is true.

## Alternatives Considered

1. A separate analysis role pair, the strongest form named in ADR-0116.
   Rejected again: the incident needed a route to evidence or a decision, not
   another review stage; once a waiver existed, the existing pair converged.
2. Engine enforcement (for example, refusing note-only assessments of RCA
   blocks). Rejected: evidence availability is a judgment recorded in free
   text, and the human-ask and hold carriers already exist.
3. An availability check at commissioning. Rejected: the commissioner cannot
   know availability before the RCA owner has investigated; in the incident it
   knew only that the cause was not yet established.
4. The planner allocates the capture as its own output. Rejected: its repair
   cannot be planned before the evidence exists, and waiting on its own
   downstream child would be a backward dependency.
