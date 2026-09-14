# 138 - Post-Hoc Inherited Dependency Narrowing

## Status

ACCEPTED

## Context

ADR-0137 lets a plan output declare `inherit_inputs` so its generated child
waits only for named upstream outputs instead of the whole upstream phase.
That intent is consumed once, in `computeInheritedDeps` during `proceed`.
Children that already exist keep the ADR-0048 barrier they were generated
with, and the ADR is explicit that this is a feature: frozen runs need no
migration.

A run that started before ADR-0137, or whose planners expressed no intent,
therefore carries the full barrier on every generated child. Measured on one
such run at the time of this decision: 58 draft coding children carried 2,109
edges, of which 1,201 were unmet cross-phase barrier edges and 65 were unmet
intra-plan edges; 51 children waited on two records-chain tasks they did not
consume. The run was serial where the plans were parallel.

No existing verb narrows that barrier honestly:

- `replan` refuses once children exist (`TransitionsExecuted` must be empty).
- `retarget-dependency` replaces one edge; it cannot remove one, and
  narrowing ~1,200 edges through it would be ~1,200 lock-taking mutations.
- Editing `state.yaml` by hand bypasses the state machine.

## Decision

Add an orchestrator-only metadata repair, `narrow-inherited-dependencies`,
that applies ADR-0137's selection semantics after generation.

**`mode: none`.** Plans record "external prerequisites: none added" for
children whose only cross-phase inputs are already concrete
`task_depends_on` edges. `selected` requires at least one selection, so this
decision adds `mode: none` to ADR-0137's schema: inherit no phase-gate edge.
It carries no selections, names no upstream, and is therefore untouched by
replan's retirement pass.

**Input.** A MERGED producer, the per-subtask transition that generated its
children (auto-detected when unambiguous), and a selections file naming
output indexes with their `inherit_inputs`. Selections use exactly the
ADR-0137 schema and pass `ValidateInheritInputs`.

**Intent is persisted first.** Each selection is written onto the producer's
`output[i].inherit_inputs` before any child is touched, so the recorded intent
is what the rewrite was computed from and a later `proceed`, replan, or audit
reads the same selection.

**Only initial-status children are rewritten.** A child is narrowed only when
its status is its role-pair's initial status and it holds no live doer lease.
Terminal, blocked, claimed, executing, and reviewing children are reported as
`skipped` and left untouched: their scheduler state would race with a
dependency rewrite, and terminal children are audit history.

**Only inherited edges are removed.** The removable set for a child is the
whole-phase inherited set (`computeInheritedDeps(...).all`) minus what its
selection keeps (`forEntry`), in both the generated spelling and the
canonical post-supersession spelling. Sibling edges (`depends_on`), concrete
edges (`task_depends_on`), and manual retargets are never in that set, so
they survive. Selected edges missing from the child are added back: that is
the superset direction.

**It fails closed, as a whole.** An out-of-range or unresolvable selection
aborts the mutation before anything is written — no partial narrowing, no
persisted intent. Every rewritten list is canonicalized and direction-checked,
and the full candidate state is validated before commit, as
`retarget-dependency` does.

**Replanned producers are refused.** Their executed-transition markers are
suppressive metadata, not evidence of children (GH #137); narrowing must
target the replacement planning task.

**Audit.** Each narrowed child records a `dependencies_rewritten` history
entry with the producer, output index, removed and added edges, and the
canonical result; the producer records the authored output indexes; the
activity log records one `narrow-inherited-dependencies` entry. The operation
carries a lifecycle request so an identified retry replays instead of
re-applying.

## Consequences

**Positive:**

- A running run can recover the parallelism ADR-0137 offers without a
  migration or a restart, one plan at a time.
- The narrowing is reproducible from persisted intent and reviewable per
  child in history.

**Limitations accepted:**

- The authoring risk in ADR-0137 applies unchanged: a valid selection is not
  proof the child can run without what it excluded. Reviewing selections
  remains a planner and reviewer responsibility; the verb only guarantees the
  rewrite matches the selection.
- Narrowing removes edges only. It will not restore an edge a selection never
  named, so an earlier over-narrowing is corrected by re-running with a wider
  selection or `mode: all`, not by omission.
- Upstream index drift after authoring is detected only where the generation
  checks detect it (replan, out-of-range). An in-place amendment of a MERGED
  upstream's `output[]` that keeps the same length is not distinguished; that
  amendment path does not exist today.
- Unlocking many children at once raises provider concurrency. Pool caps
  remain the operator's lever.

**Extends:** ADR-0137 (Selective Dependency Generation) — same intent,
applied after generation. ADR-0075 (Retarget Dependency Repair) — same
orchestrator-only, validate-then-commit repair shape, for many edges on many
children of one producer.

---
*Recorded 2026-09-14 for the selective-inputs work package (W6).*
