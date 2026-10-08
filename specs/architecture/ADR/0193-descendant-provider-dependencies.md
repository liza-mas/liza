# 193 - Descendant Provider Dependencies

## Status

ACCEPTED. Extends [ADR-0181](0181-provider-output-dependencies.md); its
stale-declaration exceptions (ADR-0185 to ADR-0190) do not apply to the new
field.

## Context

An output declaration waits on behalf of the one child it generates
(ADR-0181). A ruling that orders *writers* — coding after another plan's coding
output, as with a shared-file writer order — therefore had no faithful place on
an architecture: placed on its code-plan output, the wait holds the code plan
until the other writer merges, serializing planning behind implementation
(D-79). Generated children must carry their parent's declarations verbatim, so
the generated code plan could not shed the over-placed wait, and its parent
could no longer be replanned once the child existed.

## Decision

1. **Field.** Task, OutputEntry and AddTaskInput accept optional
   `descendant_dependencies: [{at_transition, provider_dependencies}]`. The
   inner list uses the ADR-0181 shape and reference rules. An output's entries
   are copied onto its generated child; they are never the child's own
   admission waits.
2. **Application.** When a task carrying them writes its output
   (`set-task-output`), every output entry receives each wait in its
   `provider_dependencies`; a declaration for the same provider and transition
   gains the missing indexes. The reviewed output thus shows the waits, and the
   writers generated from it inherit them by the unchanged verbatim copy.
   Generation and crash recovery refuse a stored output that lacks one.
3. **Scope.** One intermediate plan on a single per-subtask path: a
   task-level declaration needs its role pair to have exactly one per-subtask
   outgoing transition, named by `at_transition`; an output-level one needs
   that of its producer and of its generated child. A declaring or receiving
   output entry cannot set `kind`, since deduplication may skip it for an
   incumbent that never had the wait.
4. **Retirement.** A live descendant declaration — task-level on a
   non-terminal task or on a terminal one whose output is still generation
   input, or on operationally consumable output — holds its provider and
   selected children with no stale exception, and state validation checks it.
   A merged plan's declaration is released, to the ordinary rules of the
   writers carrying it, once those writers are generated. Retirement is
   refused before mutation, naming `<task> descendant_dependencies` or
   `<task> output[i].descendant_dependencies`. Re-author through the owner's
   output, a replan of an unexpanded plan, or `replace-task` with a payload
   carrying the intended declaration. Generation and recovery recheck the
   owner's placement, refusing a stored record that escaped validation before
   any child exists.
5. **Matching.** A generated child matches its source output when it carries
   the output's descendant declarations and its provider declarations, except
   those moved into its descendant declarations. Claim readiness, live-output
   classification, transition completeness and recovery use this one rule;
   recovery never re-imposes a moved wait.
6. **Recovery.** Operator-only `defer-provider-dependency` moves one
   task-level declaration of an unstarted task (initial or BLOCKED, never
   claimed) into its descendant declarations, in place, keeping decomposition,
   `arch_ref` and the reviewed parent output. It refuses a wait whose selected
   children differ in role pair from the tasks it would hold, so a planning
   prerequisite is never loosened.
7. **Diagnosis.** `set-task-output` warns when an output whose generated child
   is a plan declares a provider wait selecting non-planning children.

## Consequences

- Writer-order rulings bind writers; planning proceeds in parallel with the
  provider's implementation while every eventual writer keeps its order.
- Every output of the consuming plan receives the wait, over-serializing
  writers of that plan that do not overlap the provider.
- Cycles through a descendant wait are refused when it becomes an ordinary
  output declaration, not when declared: the grandchildren are not projected.
- Strict holds can delay a provider's retirement until the holder is
  re-authored. A replace-task replacement still drops `decomposition`, as for
  any replacement.

## Alternatives Considered

- Apply the wait at generation without writing it into the plan's output:
  hidden from the plan's reviewer, and a second declaration source for
  readiness, projection and recovery.
- Reinterpret planning-level waits on coding transitions as descendant waits:
  silently changes reviewed semantics; some plans legitimately need merged code.
- Extend the stale exceptions to descendant declarations: stale routing,
  unblock and claim refusals at every site for a first version.
- Recover only through `replace-task`: drops the plan's decomposition.
