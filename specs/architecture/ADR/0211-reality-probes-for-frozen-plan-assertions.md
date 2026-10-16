# ADR-0211: Reality Probes for Frozen Plan Assertions

## Status

ACCEPTED — 2026-10-16. Amends [ADR-0192](0192-admission-checks-child-feasibility-known-consumers.md)
(Decision 1: the existing-source premise check).

## Context

ADR-0192 tells architects and code-planners to check an existing-source premise
before it restricts a child, and the `Child feasibility` review row rejects an
unchecked one. The check leaves no artifact, so a reviewer cannot tell a checked
premise from an assumed one.

In a downstream run (D-209), planners froze exact oracles from specs and
assumptions: response shapes, read keys, refusal text, snapshot table sets and
a validation command. Plan review accepted them, and coders blocked on the
contradictions. A reviewed acceptance cannot change in-task, so each case took
a block, an orchestrator assessment and a human ruling; nine rulings in two
days, mostly on the critical path. One canonical command could not pass by
design: it ran without a flag the project's test setup requires and failed
at once on the integration branch, so one run at planning time would have
shown it.

## Decision

1. **Authors record probes.** The architect and code-planner self-checks
   include a reality-probe duty. Each assertion binding a child to delivered
   behavior (existing route, read, store function, migration, refusal text,
   response shape) and each canonical validation or acceptance command is
   recorded in a `## Reality Probes` table: the integration SHA, the command
   run there and an excerpt of its output. A command need not finish. It must
   show that its selection, flags and setup work as intended. Behavior that a
   child or an unmerged provider builds is marked `PROSPECTIVE`, with its
   producer named. A contradicting probe is fixed in the plan, never left to
   the child.
2. **Execution authority is unchanged.** Authors never run a command that
   needs a runtime input or authority they lack. Runtime-input confinement
   stays as it is: `single_use` values never reach a local run. An author
   cites an existing gate receipt or probes a bounded part of the command.
   Otherwise it marks the assertion `UNPROBED` with the reason and holds the
   affected scope on an allocated investigation or correction
   (`task_depends_on` or `provider_dependencies`), or marks the task blocked.
   A reason or an owner alone is not a hold, so the child never starts on an
   unchecked frozen premise; this keeps ADR-0192's hold semantics. Missing
   access is no evidence against delivered behavior.
3. **Reviewers audit the record.** A `Reality probes` row in the code-plan and
   architecture review checklists rejects:
   - a delivered-behavior assertion or canonical command with no entry and no
     `PROSPECTIVE` mark or held `UNPROBED` mark;
   - delivered behavior labelled `PROSPECTIVE`;
   - an observation that does not support its assertion;
   - a disagreeing re-run of one eligible observed probe.

   When no probe is eligible, the reviewer says so. Reviewers never re-run a
   probe that needs a runtime input.
4. **Placement.** The duty and the row are prompt templates shared by the two
   authors and their two reviewers, the roles that freeze child assertions.
   Epics state needs, not oracles about delivered code, so they carry neither.
   No engine check, state field or review stage is added.

## Consequences

- An unchecked premise now shows as a missing or unsupported table entry,
  which the reviewer can reject and spot-check.
- This is still a mitigation. Rendered-template tests prove only that the
  duty and row render for the intended roles. They cannot prove that agents
  run the probes, or that D-209-class blocks stop.
- Prompt growth: +965 bytes for the architect and code-planner prompts, and
  +562 bytes for the architecture and code-plan reviewer prompts. The
  role-budget baseline was re-anchored; it also absorbed earlier unanchored
  growth in the coder and orchestrator prompts.

## Alternatives Considered

1. **Engine execution of plan validation commands on integration HEAD before
   merge** (D-209 item 3). Deferred. The engine cannot tell a command that
   fails because its child has not written the tests yet from one that fails
   by design without runner knowledge (G1.1). ADR-0136 and ADR-0168 already
   reject inferring meaning from shell commands. A full run would also
   duplicate admission cost. Payback trigger: another D-209-class block from
   a canonical command whose probe a reviewer accepted.
2. **Engine presence check for `## Reality Probes` at plan submission.** Cheap
   and stack-agnostic, but it proves only a heading, and greenfield plans
   would need a vacuous section. This is the next step if the payback trigger
   above fires.
3. **An "oracle drift" block class settled by a bounded non-human owner**
   (D-209 item 4). Deferred. It changes who may amend reviewed acceptance, an
   authority question for review flow. ADR-0206 keeps acceptance unchanged
   under scope exceptions. Payback trigger: the human decides that
   engine-level delegation is wanted.
4. **Strengthen the `Child feasibility` wording only.** Rejected. That keeps
   the guidance-only form that left no artifact.
