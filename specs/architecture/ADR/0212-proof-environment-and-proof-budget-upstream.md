# ADR-0212: Proof Environment and Proof Budget Are Decided Upstream

## Status

ACCEPTED — 2026-10-16. Decision 5 (engine-supplied execution envelope) is a
follow-up. Complements [ADR-0136](0136-validation-session-prerequisites.md)
(what a session must have), [ADR-0204](0204-deferred-validation-tier.md) (where a
shared suite runs) and [ADR-0211](0211-reality-probes-for-frozen-plan-assertions.md)
(that a canonical command works). This ADR covers where proof runs and what it
costs.

## Context

A run lost most of its non-product effort to the local execution environment. An
operator digest of the run journal found nine friction themes. None was decided
before the run; each was settled mid-run, often one task at a time:

- **Where proof runs.** The goal said "Supabase persistence" and "LLM work is
  mocked", but never that acceptance needs a real disposable Postgres, which local
  stack is off limits, or who provisions it. The database URL reached agents only
  through an operator wrapper script in `/tmp`. Tests inherited a live API key.
  External samples needed the human's credentials, and sanitization gaps
  followed.
- **What proof costs.** Real-database suites took 30 to 180 minutes. They hit, in
  turn, the 3600 s admission cap, the 30-minute background-job limit, the 2 h and
  5 h session timeouts and the 250-minute command limit. Code plans set
  `timeout_seconds` to 600 or 1800 with no measurement, which led to standing
  budget exceptions. Overlapping database admissions and the deferred runner
  competed for slots. Together these were the largest themes, with about 380
  journal keyword hits, inflated by repeated watch rounds.

Goal writing elicited functional and structural decisions by altitude, plus
non-functional bounds, but no environment. Architecture placed each proof at its
pipeline stage, but never weighed its runtime or shared-resource use against the
execution limits. Planners did not know those limits, so they guessed.

## Decision

1. **The proof environment is a third goal dimension, scaled by altitude**
   (`goal-writing`). It sits beside the functional and structural dimensions and
   follows the same lean test (who still had the choice?):
   - `general-objective` owes the direction: for each dependency, whether proof
     runs against the real thing, a disposable copy, a recording or a mock; what
     proof must never touch; what only the human supplies (provisioning,
     credentials, recorded evidence); and the proof time budget.
   - `functional-spec` adds the environment constraints the architect must honour.
   - `technical-spec` adds the settled environment design, with variable names but
     never values. Because this entry point skips architecture, the design includes
     the proof topology of decision 3.

   Detail is owed where agents cannot act on it, or where the choice changes what
   is proven. It forecloses where an agent could set it up safely in its sandbox.
   An existing stack or environment guide is cited, not restated. A new tell,
   "Environment by assumption", and a rubric section carry the elicitation,
   including when human-supplied inputs must exist and what agents can do on the
   host. The goal owns the proof environment in the reference-first ownership
   table, so architecture inherits it like the global NFRs.
2. **Readiness checks it** (`check-liza-input-readiness`). A universal "Proof
   environment" row, at the entry point's precision. Its absence is a blocker
   wherever the success criteria need something agents cannot supply, or where
   the choice changes what is proven. A missing time budget is a ceiling, and so
   a note.
3. **Architecture budgets proof** (`architecture-planning`). Design step 6, "Proof
   topology", honours the inherited proof environment and treats what only a human
   supplies as a prerequisite. It records for each proof on a shared or slow
   resource three things:
   - whether it is isolated or shared, and what serializes it;
   - its expected runtime with a basis (measured, or an estimate labelled as one);
   - whether it runs per task, at gates, or both.

   It checks each against the execution envelope the task supplies. A misfit, or a
   missing envelope, is an Open Question with its cost. This adds a self-review
   item and the "Unbudgeted Proof" anti-pattern. A master architecture also checks
   the sum across its scopes, which no specialized scope can see.
4. **Code plans ground their budgets** (`acceptance-evidence.md`, code-plan review
   template). `timeout_seconds` comes from a measured run of the same commands
   plus load headroom, or from a labelled estimate. The basis is recorded on the
   Reality Probes entries of the validation commands (ADR-0211), summed for the
   batch, never inside the Acceptance Contract section. The budget is checked
   against the supplied envelope; without one, against the architecture's proof
   topology or the goal's time budget. Where no bound is stated, the basis alone
   suffices; shared or slow proofs are bounded upstream (decisions 1 and 3). A
   budget beyond a stated bound makes the code planner mark the task BLOCKED.
   Code-plan review rejects a missing basis or a budget beyond a stated bound.
5. **The engine supplies the execution envelope** (follow-up, not implemented
   here). Architect and code-planner prompts should carry, as facts:
   - the command limit, the admission cap and the session timeout;
   - the shared slot classes and `max_instances`;
   - measured durations of existing canonical suites, from admission and deferred
     validation records.

   Until then, decision 3 raises an Open Question whenever the task supplies no
   envelope. Decision 4 checks against any upstream bound, and blocks only when a
   stated bound is exceeded.

## Consequences

- An environment or budget gap surfaces at goal readiness or architecture review,
  where one decision settles it, instead of at coding as blocks, timeouts and
  budget exceptions settled per task.
- Decisions 1 to 4 are prompt and skill text. A rendered-template test
  (`acceptance_evidence_test.go`) shows only that the budget rule renders for the
  code-plan reviewer. It cannot show that authors weigh proof cost, or that these
  frictions stop.
- Without decision 5, planners still lack the numbers. Architecture then raises
  Open Questions, and code planning checks against the goal's time budget or the
  architecture's proof topology where they exist. That is better than a silent
  guess, but it costs a human answer per architecture plan.
- Prompt growth: +217 bytes for the code-plan reviewer. The peer-scope baseline was
  re-anchored. The skill changes do not enter rendered prompts.
- Some of the cost stays outside the pipeline's reach: engine hard limits (removed
  only by engine change) and host load from outside the run.

## Alternatives Considered

1. **Put all environment detail in the goal.** Rejected. Tools, versions, setup
   commands and harness conventions are the same for every goal in a project.
   Repeating them breaks the lean rule and lets copies drift. They belong in a
   project environment guide that goals cite.
2. **Leave environment to project setup and the engine only.** Rejected. Which
   dependencies are real or mocked, what proof must never touch, and who supplies
   credentials and recordings are human decisions agents cannot make. Without them
   the run guesses or blocks.
3. **Engine-only budgets** (raise caps, let admission fail fast). Not sufficient.
   Raising caps fixed individual limits, but the per-task or gate decision and slot
   design remained unmade until they hurt.
4. **Engine envelope first, skills later.** Rejected as an ordering. The skill text
   is cheap, and it makes the missing envelope visible as Open Questions, which is
   the evidence that decision 5 is needed.
