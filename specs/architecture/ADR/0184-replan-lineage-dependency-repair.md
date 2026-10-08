# 184 - Replan Lineage Dependency Repair

## Status

ACCEPTED. Amends [ADR-0075](0075-retarget-dependency-repair.md),
[ADR-0114](0114-terminal-dependency-repair.md) and
[ADR-0181](0181-provider-output-dependencies.md).

## Context

`replan` retires a MERGED plan and creates a replacement whose `supersedes`
names it. Dependency addressing did not follow that link:

- `replan` retargets edges on non-terminal consumers only. A consumer that is
  MERGED with an unexecuted hand-off keeps its edge to the replanned upstream.
  This is correct, because its content predates the replan and plan-check
  demands reconciliation. When that consumer is replanned in turn, however, its
  replacement copied the stale edge verbatim. The re-authored plan then targets
  the successor in its output while its `depends_on` still names the retired
  upstream. Plan-check refuses it, `retarget-dependency` refuses terminal tasks,
  and only another replan remains, which copies the edge again.
- A `provider_dependencies` selection projects the child ID
  `<provider>-<slug>-<index>`. Once that child is replanned, the projection
  names a retired task, and no declaration can name its replacement.

Admission cannot simply follow the lineage: transition inheritance reads the
parent's `depends_on` and skips replanned upstreams, so a pass over a stale edge
would generate children without the successor's barrier.

## Decision

Replan lineage is followed at three points, through one fail-closed primitive.
`ReplanSuccessor` walks from a replanned task to the unique task whose
`supersedes` names it, with the same role-pair and parents, repeating while
that task is itself replanned. A missing, ambiguous or cyclic step resolves
nothing.

1. **Replan clones resolved edges.** Each dependency of the original that has a
   live (not superseded, abandoned or hand-off-retired) successor is cloned as
   that successor, with a `dependencies_rewritten` entry on the replacement. An
   edge without a live successor is kept.
2. **MERGED-plan lineage repair.** `retarget-dependency` accepts one terminal
   case. The task is a MERGED planning task with output, no executed transition
   and no retired hand-off. The old dependency is replanned, and the new
   dependency list is exactly its MERGED successor. Some output entry names the
   successor (`task_depends_on`, a provider task, or an inheritance selection),
   and no output entry names the old dependency or another replanned task of
   the same lineage. Plan-check state, output and review records are unchanged.
   A refused `plan-check --pass` names this repair when it applies.
3. **Provider child slots follow lineage.** Readiness, validation, projected
   cycles, the retirement barrier and claim priority resolve a replanned
   selected child to its successor. An unresolvable child keeps its projected
   ID and stays retired. Output indexes remain provider-scoped: a replanned
   provider is still refused.

**Amended 2026-10-08: a direct child counts as its parent plan (decision 2).**
A plan authored after its upstream's successor ran its hand-off may name only
the successor's children (operator defect D-77). The repair refused it as
predating the replan, which left a re-authoring cycle as the only route for
reviewed content. An output reference now counts as naming the reference
itself and each of its parents. Naming a direct child of the successor
therefore targets the successor; a child exists only once that hand-off ran.
A reference that is, or is a child of, a task of the retired lineage is
refused, and every parent is checked before acceptance. Hand-off admission is
unchanged (Alternative 1 still applies).

## Consequences

- Stale edges are no longer minted by replan, and existing ones on MERGED plans
  have a reviewed-content-gated repair, so no extra planning cycle is needed.
- Plans whose content predates the upstream replan still require replan.
- The reconcile rule and hand-off admission are unchanged.
- The retirement barrier now protects a replan successor that a live
  declaration resolves to. The policy itself (D-61) is unchanged.

## Alternatives Considered

1. Plan-check follows lineage at admission. Rejected: the stale `depends_on`
   would still feed inheritance and drop the successor's barrier.
2. A new repair command. Rejected: `retarget-dependency` already owns
   authorization, receipts, cycle envelopes and audit for a single edge.
   ADR-0114's refusal of terminal retargets addressed multi-edge direction
   repair on SUPERSEDED tasks, not this lineage-only case.
3. A declaration selector for replanned children. Rejected: a schema change
   and a planner burden for an identity the lineage already fixes.
