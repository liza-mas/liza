# 195 - Provider Reservations for Inserted Writers

## Status

ACCEPTED. Extends [ADR-0181](0181-provider-output-dependencies.md) and
[ADR-0193](0193-descendant-provider-dependencies.md).

## Context

A correction that must write a shared file between two writers already in an
ordered chain (D-80: Unit 4 → correction → Unit 5) had no executable placement.
A typed provider declaration selects output indexes, so a writer could only wait
for children its provider had already authored. The correction's later outputs,
or more outputs than the selection named, escaped the order. Generated children
copy their parent's declarations verbatim, so the later writer could not gain
the wait in place, and the only route left was for its planner to discover the
order at claim time.

## Decision

1. **Field.** A task accepts `provider_reservations: [{provider_task,
   transition}]`. It holds the task until its effective provider is `MERGED`
   with the transition executed and every child generated from every output has
   merged. A provider that merges with no output satisfies it. The field is a
   task's own wait, never copied from a parent output. Unknown or
   non-per-subtask transitions, wrong-source providers, missing or retired
   providers, and `kind` output on the provider fail closed.
2. **Lineage.** The effective provider follows replan and same-pair
   `replace-task` successors (ADR-0184, ADR-0191). Permanent retirement (cancel,
   supersede, plan-check replacement) of a provider is refused while a holder's
   reservation is unsatisfied. Replan and `replace-task` of the holder carry the
   reservation; superseding a holder requires every successor to hold the same
   one, checked when the supersession happens, so a later release on the
   successor remains an explicit decision.
3. **Placed-writer invariant.** A provider held by an unsatisfied reservation
   is a placed writer. Every typed selection of its outputs needs its
   `max_outputs` cap and must select exactly `[0..max_outputs)`. Selections are
   live task-level and output-level `provider_dependencies` and
   `descendant_dependencies`. A selection by index can therefore not miss a
   writer the provider authors later. `max_outputs` binds any task, placed or
   not: `set-task-output` refuses more entries, and state validation refuses
   live output above it. Replan clones the cap; same-pair `replace-task`
   inherits it when the payload omits one.
4. **Commissioning.** A single-item `add-tasks` request may carry
   `reserve_successors` and `max_outputs`. The new task's role pair must have
   one per-subtask outgoing transition, and each named successor is reserved
   behind it in the creating transaction. Successors must exist and be
   unstarted: initial, rejected or BLOCKED, with no assignee or lease. A
   multi-item request carrying either field is refused before any item is
   created, because items commit one by one.
5. **Repair.** Orchestrator-only `reserve-provider <task> <provider>
   --transition <name> --reason ...` adds a reservation to an unstarted writer,
   or withdraws it with `--release`, which is a policy decision. Repeating a
   call is a no-op. `--provider-max-outputs` sets or tightens the provider's cap
   in the same transaction and is refused below its current output count. The
   change is recorded as `dependencies_rewritten` history, a cap change on the
   provider with its previous value, and in the activity log.

## Consequences

- A correction can be inserted into a writer chain when it is commissioned, and
  however many writers it later authors, they all land before the reserved
  writer.
- A writer that has started cannot be placed, and that needs a human decision.
- Placing a provider that is already selected by index needs a cap covering
  those selections. A cap cannot rise while such a selection exists.
- A permanently replaced correction needs its successor reserved, or the
  holder's reservation released, before the retirement is accepted.

## Alternatives Considered

- Widen an index selection to future outputs: changes reviewed ADR-0181
  semantics for every existing declaration.
- Rewrite the later writer's inherited declarations: breaks the verbatim
  generated-child match and loses review provenance.
- Leave placement to the later writer's planner: the D-80 failure.
