# 137 - Selective Dependency Generation

## Status

ACCEPTED

## Context

ADR-0048 introduced automatic phase-gate dependencies: when a downstream
planning task depends on an upstream one, every child generated from the
downstream task inherits a dependency on **every** child of the upstream.
That is deliberate — the ADR states it as "2a, 2b, 2c auto-inherit depends_on
[1a, 1b, 1c]", with the rationale that "planners don't need to manually
specify cross-phase ordering".

The conservative default is correct when the planner has expressed no intent.
It is costly when the planner knows better. A documentation child that needs
nothing from a ten-child implementation phase waits for all ten. One held
child then holds everything downstream of it, so a single local delay
propagates into a broad stall that adding workers cannot relieve.

The generation site is `computeInheritedDeps` in `internal/ops/proceed.go`.
Only its `per-subtask` arm fans out; `one-to-one` and `many-to-one` already
produce a single edge per upstream.

No existing field can express the needed intent:

- `OutputEntry.depends_on` is sibling output **indexes** within the same
  parent (ADR-0048), resolved through `siblingIDs[idx]`.
- `OutputEntry.task_depends_on` names **existing concrete** task IDs
  (ADR-0058), and `set-task-output` rejects IDs not already in state.

Neither can name a child of an upstream expansion that has not happened yet,
which is the normal case when a planner authors its output.

## Decision

Add an optional `inherit_inputs` field to `OutputEntry`, following ADR-0058's
precedent: a purely additive field that leaves `depends_on` and
`task_depends_on` semantics untouched.

```yaml
inherit_inputs:
  mode: selected          # or "all"
  selections:
    - upstream_task: plan-1
      outputs: [0, 2]     # indexes into plan-1's own output[]
```

**Omitted means the whole-phase barrier.** A nil `inherit_inputs` reproduces
ADR-0048's behavior exactly, so every existing and frozen run keeps its
current semantics with no migration. `mode: all` is the explicit spelling of
the same thing, letting a planner record a deliberate barrier rather than an
omission.

**Selections reference upstream output positions, not task IDs**, because the
upstream's children do not exist at authoring time.

**Validation is split across two stages**, because the information needed is
not available at one point:

| Stage | Checks |
| --- | --- |
| `set-task-output` | mode enum; selections non-empty iff `selected`; `upstream_task` is a dependency of the producing task; the producing role-pair has a per-subtask consumer transition; indexes non-negative and deduplicated |
| `proceed` (generation) | the upstream contributed children to this transition; each index is within the upstream's actual output range |

**Generation fails closed.** An unresolvable or out-of-range selection errors
the transition. It never yields a smaller dependency set: a planner naming a
prerequisite that cannot be resolved is a defect, and silently dropping it
produces a child that runs without an input it declared.

**Selections are direction-validated at generation, not authoring.** Unlike
`task_depends_on` (`set_task_output.go`), there is no authoring-time
`validateDependencyDirection` call, because the child IDs do not exist yet.
Every generated child's final `depends_on` passes that check inside
`proceed.go`. This asymmetry is deliberate, not an oversight.

**Selections are inert on single-edge cardinalities.** For `one-to-one` and
`many-to-one` there is no fan-out to narrow, so the full edge is inherited.
That is a superset, so nothing is dropped, and it is documented behavior
rather than an error — one task may have outgoing transitions of several
cardinalities.

**Replanning retires affected selections.** When an upstream is replanned, any
`inherit_inputs` naming it is rewritten to `mode: all` and the retirement is
recorded. Indexes are deliberately **not** retargeted onto the replacement:
replanning exists to change the upstream's `output[]`, so index `k` no longer
denotes what the planner selected, and carrying it across would silently
reinterpret the selection — worse than losing it, because nothing would
detect it. Degrading to the whole-phase barrier is the safe direction: it is a
superset of any selection, it cannot fail, and it matches omitted intent.

That pass deliberately does **not** reuse the terminal-task filter used by
replan's `DependsOn` retarget loop. `MERGED` is terminal *and* is the status
children are generated from, so a merged producer's `output[]` is live input
to generation, not historical audit data. This refines `INVARIANTS.md` §3.4's
"retired SUPERSEDED and ABANDONED task output remains historical audit data
unless it can still drive crash recovery": merged output is not retired.

## Consequences

**Positive:**

- A planner can express that a child needs only part of an upstream phase,
  removing barriers that hold independent work.
- The conservative default is preserved for every artifact that says nothing,
  so no migration is required and frozen runs are unaffected.
- Edge origins become inspectable: a child's inherited dependencies are either
  the whole upstream phase or an explicitly named subset.

**Limitations accepted:**

- Another additive `OutputEntry` field, on top of `depends_on` and
  `task_depends_on`. Three related fields is a real cost; the alternative was
  overloading one of the existing two, which ADR-0058 already rejected for
  making a field context-dependent.
- A planner can narrow a barrier incorrectly. A syntactically valid selection
  is not proof that the child can run without the outputs it excluded; review
  of dependency intent remains a human and reviewer responsibility.
- Replanning loses selection precision for affected entries, which must be
  re-authored to regain it.
- Degradation does not recover the prerequisite when the producer is terminal:
  its `DependsOn` still names the replanned task, which has no children by
  design, so the barrier is lost either way. That is a pre-existing gap in
  replan's retarget loop, mitigated today by a human warning and tracked in
  `TECH_DEBT.md`; it is not introduced by this decision.

**Extends:** ADR-0048 (Multi-Phase Planning) — the automatic phase gate
becomes the default for omitted intent rather than the only behavior.
ADR-0058 (Output Entry Concrete Task Dependencies) — same additive-field
pattern, preserving `depends_on`'s sibling-index meaning.

---
*Recorded 2026-09-14 for the selective-inputs work package (W6).*
