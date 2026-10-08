# 191 - Replace-Task Successors Carry Selected Child Slots

## Status

ACCEPTED. Amends [ADR-0184](0184-replan-lineage-dependency-repair.md)
(Decision 3), [ADR-0186](0186-stale-selected-child-slots-on-unexpanded-plans.md)
(Decision 1) and [ADR-0187](0187-stale-provider-declarations-on-unstarted-consumers.md)
(Decision 2).

## Context

A selected child slot follows only replan lineage (ADR-0184). `replace-task`
supersedes its source, a permanent retirement, so the slot keeps naming the
retired child. Two costs followed (D-70):

- A consumer could not re-state its typed selection after its selected child
  was replaced: validation refused the retired child, and the only accepted
  re-authoring dropped the typed declaration for a plain edge.
- Each replacement of a selected child blocked the child's unstarted consumers
  for re-authoring (ADR-0187), and their re-authoring blocked their own
  consumers. Runs paid one re-authoring wave per corrected child.

`replace-task` admits only an initial, rejected, BLOCKED or INTEGRATION_FAILED
source, never a MERGED one, so no consumer has consumed the replaced child's
output. A same-pair replacement inherits the source's parents (commit
`1ec3384e3`) and `arch_ref` (D-67): it carries the same provider output slot.

## Decision

1. **Slot following.** A selected child slot follows a same-pair replace-task
   successor exactly as it follows a replan successor: the child is SUPERSEDED
   by that single successor, which has the child's role pair and parents, and
   the child's history records the replacement (`replacement_task_id` on its
   `superseded` entry, or on `replacement_committed` for replacements made
   before this decision). Replans and such replacements chain in any order;
   a missing, ambiguous or cyclic step resolves nothing.
2. **Evidence before retirement checks.** `replace-task` records the successor
   on the `superseded` entry, which the supersession writes before its
   retirement barrier and consumer routing run. The replaced child is then no
   longer named by any declaration: no holder refuses it and no consumer is
   blocked. The consumer waits for the successor to merge.
3. **Scope.** Direct declarations never follow (ADR-0190 Alternative 1).
   supersede-task, cancellation, plan-check replacement and plan-declared
   replacement stay permanent retirements, and `depends_on` lineage
   (ADR-0184 Decisions 1 and 2) stays replan-only.

## Consequences

- Replacing a selected child costs no consumer re-authoring; a consumer can
  re-state its typed selection after the replacement.
- Consumers blocked earlier by such a replacement validate again and can be
  unblocked; nothing unblocks them automatically.
- The retirement barrier protects the successor, as it protects a replan
  successor.
- A replacement that changes the child's scope no longer forces its consumers
  to re-review their selection. The same trade-off ADR-0184 accepted for
  replan: the successor continues the same allocation.

## Alternatives Considered

1. Let a declaration name the successor directly. Rejected: a schema change
   and a planner burden (ADR-0184 Alternative 3), and every replacement still
   blocks its consumers.
2. Follow any supersession by one same-pair, same-parent successor. Rejected:
   supersede-task can name an existing sibling child, which owns another
   output slot.
3. Move `replacement_committed` before the supersession. Rejected: the task
   transition ID and replay key on the last history event.
