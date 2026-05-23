# 67 - Master Planning Task Pattern

## Status

ACCEPTED

## Context

Parallel planning tasks can produce individually plausible but mutually inconsistent decompositions. Architecture planning already reduced this risk for downstream code planning, but the same risk remained when entry-point work could fan out directly into multiple epic-planning, architecture, or code-planning tasks.

The structural problem was that consequential decomposition could happen before adversarial review. Once parallel planners started from only the same goal spec, interface ownership, shared-file ownership, artifact refs, and dependency ordering were discovered late through rejection or superseding.

ADR-0066 separated the architecture sub-pipeline and clarified entry points. It did not add a reviewed decomposition gate for every planning fan-out.

## Decision

Add a master planning task pattern for planning fan-out:

- Entry points still target specialized planning pairs.
- `INITIAL_PLANNING` creates exactly one first task.
- Simple work creates one specialized planning task.
- Fan-out or uncertain work creates one mapped master task in the corresponding `decomposition-root` role-pair.
- Master role-pairs reuse the same doer and reviewer roles as specialized pairs.
- Master tasks require quorum 2 and use `partially-approved` plus `reviewing-2` states before final approval.
- Approved master outputs auto-decompose into specialized planning children through same-subpipeline `per-subtask` transitions.

The master task owns the general approach, boundaries, interfaces, shared ownership, dependency ordering, required framework artifact refs, and typed decomposition metadata. Specialized children inherit that framework and do not redefine it.

Case A remains unchanged: `architecture-to-code-plan` consumes specialized `architecture-pair` output and targets `code-planning-pair` directly, bypassing `code-planning-main-pair`. Specialized epic outputs continue to use `epic_ref` for `us-writing-pair`; epic master outputs use `plan_ref` as the framework ref.

## Consequences

Positive:
- Planning fan-out now has a reviewed coherence gate before parallel work starts.
- The same roles are reused, so the topology adds role-pairs but not new agent roles.
- Quorum 2 moves review effort to the highest-leverage decomposition point.
- Typed decomposition metadata makes ownership, read-only dependencies, and interface contracts inspectable and propagatable.
- Simple entry-point work avoids the extra master cycle.

Trade-offs:
- Fan-out work pays an additional planning review cycle before specialized planners start.
- Poor master decomposition becomes a high-impact single point of failure, mitigated by quorum 2 and master-specific reviewer criteria.
- Output-entry schema is heavier for decomposition-root tasks.
- Existing frozen `.liza/pipeline.yaml` workspaces are not rewritten when embedded topology changes. Known legacy master role-pairs missing `decomposition-output-ref` are backfilled in memory at load time; adopting new role-pairs or transitions requires manually updating `.liza/pipeline.yaml` or starting a fresh workspace.

## Alternatives Considered

1. Let `INITIAL_PLANNING` keep creating multiple specialized tasks and rely on reviewers to catch inconsistencies.

Rejected because the inconsistency is often cross-task and appears only after multiple branches of work have already spent agent cycles.

2. Add a new decomposition role.

Rejected because epic planners, architects, and code planners already have the domain expertise needed to decompose their phase. The master role-pair specialization changes responsibilities through `decomposition-root: true` without increasing role count.

3. Make every entry-point task go through a master role-pair.

Rejected because simple single-scope work does not need a master gate. The accepted routing keeps the master path for fan-out or uncertainty and uses the specialized path for confidently simple work.

## Relationship to ADR-0066

ADR-0066 remains in force. It extracted architecture into its own sub-pipeline and introduced `functional-spec`, `technical-spec`, and the `detailed-spec` alias. This ADR builds on that topology by adding reviewed master role-pairs ahead of specialized planning pairs when fan-out is needed. It preserves ADR-0066's `architecture-to-code-plan` target as a direct specialized architecture-to-code-planning transition.
