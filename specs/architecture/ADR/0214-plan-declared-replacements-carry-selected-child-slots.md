# 214 - Plan-Declared Replacements Carry Selected Child Slots

## Status

ACCEPTED. Amends [ADR-0191](0191-replace-task-successors-carry-selected-child-slots.md)
(Decision 3) and [ADR-0196](0196-plan-replacement-reservation-placement.md)
(Consequences, Alternative 1).

## Context

A selected child slot follows replan and same-pair `replace-task` successors
(ADR-0184, ADR-0191), both minted under the provider. A correcting plan whose
output `supersedes` a selected child generates its successor under the plan
instead, so the slot kept naming the retired child (D-167). An unstarted
consumer was blocked for re-authoring (ADR-0187); a started one held the child,
so the correction's own hand-off was refused; a reservation on the provider
selected the retired child and left the candidate invalid (D-172). Every
repair in the run was the same hand-written re-authoring of the consumer onto
the correction's child, "provider need identical", and it recurred at each
correction in the lineage (B-402).

ADR-0196 rejected generalizing slot lineage to plan replacement because the
successor's different parent would "silently inherit an old reviewed slot's
meaning". The inheritance is not silent: the correction is reviewed, and its
output names the child it replaces.

## Decision

1. **Slot following.** A selected child slot follows a plan-declared
   replacement when it is unique and provable: the child is SUPERSEDED by
   exactly one successor that names it in `supersedes`, has its role pair and
   a single parent plan other than the child's parents, and is the child that
   plan generated, at an executed transition, from its one output superseding
   the child. Replans, same-pair replace-tasks and such replacements chain in
   any order; a missing, ambiguous, split or cyclic step resolves nothing.
2. **Provenance.** A selected child under another parent is accepted only as
   the slot's verified successor, never on its own.
3. **Barrier.** The hand-off evaluates the retirement barrier as if the
   original were already retired into its unique successor, as replace-task's
   supersession entry arranges (ADR-0191 Decision 2), so a consumer selecting
   the original no longer holds it. Provider-retirement failure fingerprints
   carry a lineage-policy discriminator: pre-fix refusals retry once.
   `plan-check --pass` (ADR-0196, D-172 amendment) judges only a split
   original: a unique successor strands no placement.
4. **Scope.** Only selected child slots follow, including reservations, which
   resolve through them. Direct declarations never follow (ADR-0190), and
   reservation provider aliasing and validation-note attribution keep
   following replan and replace-task only. Splits, supersede-task,
   cancellation and plan-check replacement stay permanent retirements.

## Consequences

- A correction of a selected child costs no consumer re-authoring and no
  operator re-point; the consumer waits for the correction's child.
- A reservation behind the provider waits for the correction's child: D-172's
  stranded holder no longer arises for a unique successor.
- Consumers blocked earlier by such a correction validate again; nothing
  unblocks them automatically.
- Readiness is unchanged: the provider must be MERGED, not held, with its
  transition executed, and the slot is satisfied when the successor merges.
- A correction that narrows the child's scope no longer forces its consumers
  to re-review their selection: the trade-off ADR-0184 and ADR-0191 accepted.
  Following adds a consumer-to-successor edge, so a successor that depends on
  its consumer is refused as a cycle.

## Alternatives Considered

1. An opt-in output field declaring the slot carried. Rejected: a schema and
   planner-prompt change, and every correction that omits it still blocks.
2. Rewrite consumer declarations to the correction's output at hand-off, as
   operators did. Rejected: a generated consumer's declarations must equal its
   parent's reviewed output.
3. Widen the generic successor walk. Rejected: reservation provider aliasing
   and validation-note attribution would then credit the successor to the
   original's provider output.
