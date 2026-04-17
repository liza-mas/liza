# 36 - Structured Task Output and Scope Extensions

## Context and Problem Statement

The declarative sub-pipeline architecture (ADR-0035) introduced `per-subtask` cardinality for transitions — one child task per planned item. But there was no mechanism for a planning agent to persist structured task definitions that downstream transitions could consume.

Separately, coders encountered rejection loops when they legitimately needed to modify files outside their declared scope. In real-world development, no plan is 100% accurate — scope negotiation between implementer and reviewer is normal. The system needed a way to formalize this.

## Considered Options

### Task Output
1. **Free-form task descriptions** — the orchestrator creates tasks with prose descriptions. Loses structure; downstream roles can't validate completeness.
2. **Structured `output[]` on tasks** — planning agents persist typed entries that transitions read to create child tasks.

### Scope Extensions
1. **Reject and re-plan** — any out-of-scope file modification triggers rejection. Too rigid; blocks legitimate work.
2. **Scope extensions in checkpoint** — coder declares out-of-scope files with justification; reviewer evaluates.

## Decision Outcome

Chose structured mechanisms for both: `output[]` for inter-pipeline data flow, `scope_extensions` for scope negotiation.

### Architecture

**Task Output:**
```go
type OutputEntry struct {
    Desc     string `yaml:"desc"`
    DoneWhen string `yaml:"done_when"`
    Scope    string `yaml:"scope"`
    SpecRef  string `yaml:"spec_ref"`  // optional
}
```

> **Note:** The four-field schema shown above is the original (2026-03 backfill) form. The struct has since been extended with additive optional fields — see the Revision History at the end of this ADR.

- Stored directly on `Task.Output []OutputEntry`
- Written via `liza_set_task_output` MCP tool
- Validated: task must be in executing status; agent must be assigned; all required fields non-empty
- Overwrites atomically (idempotent)
- Consumed by `liza proceed` with `per-subtask` cardinality — one child task per output entry

**Scope Extensions:**
```go
type ScopeExtensionEntry struct {
    File          string
    Justification string
}
```

- Added to `WriteCheckpointInput.ScopeExtensions`
- Serialized into checkpoint history entry's `Extra["scope_extensions"]`
- Rendered in reviewer template (`{{- if .ScopeExtensions}}` block)
- Reader helper `GetLatestScopeExtensions()` extracts from latest checkpoint

### Rationale

**Output[]** provides the generic data channel that makes `per-subtask` transitions work. Without it, the orchestrator would need to manually create and configure each child task — defeating the purpose of declarative transitions.

**Scope extensions** mirror how real teams work: a developer discovers a legitimate need to touch an adjacent file, documents why, and the reviewer evaluates. Blocking this entirely causes rejection loops; allowing it silently removes reviewer oversight. Explicit declaration with justification is the right middle ground.

### Consequences

**Positive:**
- Planning agents produce structured, validated task definitions
- Transitions consume output[] automatically — no manual orchestrator intervention
- Scope negotiation is explicit and auditable
- Reviewers have context to make informed decisions about out-of-scope changes

**Limitations accepted:**
- ~~Output[] entries must match the expected schema — no extensibility beyond the four fields~~ *(revised — see Revision History)*
- Scope extensions are advisory — reviewer can still reject

## Revision History

### 2026-04-17 — Schema-extensibility policy revised

**Trigger.** The pre-commit bootstrap goal (`specs/goals/20260417-precommit-bootstrap.md`, Q2) required adding a typed marker field `kind` to `OutputEntry` (and propagated to persisted `Task`) as the stable dedup primitive. The schema change itself is specified in the architecture plan for task `architecture-1`: `specs/arch-plan/20260417-141659-architecture-1.md` (§2.1 and §2.2).

**What changed.**

The "Limitations accepted" bullet asserting that `OutputEntry` admits no fields beyond the original four (see the struck-through line in the Consequences section above for the verbatim wording) is rescinded. That constraint was always aspirational: the live `OutputEntry` struct in `internal/models/task.go:262-269` already carries three additive optional fields beyond the original four, and the persisted `Task` struct carries two of them in turn.

Actual prior `OutputEntry` schema-field additions (chronological):

| Field added | Introducing ADR | Extends link declared? |
|-------------|-----------------|------------------------|
| `DependsOn []string` | ADR-0048 (Multi-Phase Planning) | Yes — ADR-0048 line 71: `**Extends:** ADR-0036 ... DependsOn in OutputEntry.` |
| `PlanRef string` | *none* — added via commit `ef80d629` ("feat(ops,prompts): add plan_ref propagation", 2026-03-17) | No ADR link declared; retroactively acknowledged here. |
| `ArchRef string` | ADR-0056 (Architecture Step) | Yes — ADR-0056 line 116: `**Extends:** ADR-0036 ... arch_ref on output[].` |

The `kind` field (see architecture-1's arch plan §2.1 for the exact struct tag and placement, and §2.2 for the propagated field on `Task`) is the fourth such additive extension, shipping with the pre-commit bootstrap goal.

Distinct category — *channel reuse, not schema growth*: ADR-0055 (Integration Sub-Pipeline) reuses `output[]` to create fix-tasks and declares `**Extends:** ADR-0036 ... output[] drives fix-task creation.` (ADR-0055 line 94), but it adds no new field to `OutputEntry` or `Task`. It is not counted among the schema-extension precedents above; it is a channel-reuse precedent that this policy does not govern directly.

This revision formalizes the additive-field practice as policy (see Current Policy below), retroactively acknowledges `PlanRef` as a prior extension that never received its own ADR link, and records the `Kind` addition under the same policy.

**Current policy — schema extension of `OutputEntry` and `Task`.**

Additive optional fields on `OutputEntry` and the propagated persisted `Task` are permitted without superseding this ADR, subject to all of the following:

1. **Additive only.** New fields MUST be optional with a zero-value default that is behaviorally inert (`omitempty` on YAML and JSON tags; empty string or empty slice is the default).
2. **Backward-compatible on load.** State files written before the field existed must decode without error into a zero-valued field. No migration pass may be required.
3. **No required-field semantics.** An empty value MUST NOT cause `validateOutputEntry` (or any successor validator) to reject the entry. Enforcement of the new field's semantics is the responsibility of the consuming code paths, not the generic struct validator.
4. **Provenance via `Extends:` link.** The introducing ADR MUST declare `**Extends:** ADR-0036 (Structured Task Output) — <field name> on OutputEntry.` (or equivalent wording) in its Consequences section, so the cross-reference graph stays navigable without in-place edits here.
5. **No breaking removals.** Removing a field that has ever shipped requires a superseding ADR (this ADR remains the authoritative record of the additive policy). This revision does not remove any existing field.

**Historical record.** The original wording of the rescinded bullet remains struck through (not deleted) in the Consequences section above, so readers of this document can see exactly what constraint was revised.

**Non-changes.** Every other claim in this ADR remains in force: the `output[]` channel, `liza_set_task_output` write path, atomic overwrite semantics, per-subtask cardinality consumption, the `ScopeExtensionEntry` model, the checkpoint serialization path, the reviewer template block, and the `GetLatestScopeExtensions()` helper. `scope_extensions` policy is unchanged in every respect.

**Cross-references.**
- Goal: `specs/goals/20260417-precommit-bootstrap.md#q2-idempotency--plan-time-dedup-execution-time`
- Schema delta (code-side): `specs/arch-plan/20260417-141659-architecture-1.md` §2.1 (`OutputEntry`), §2.2 (`Task`), §2.3 (propagation), §2.4 (replan propagation)
- Prior extensions: ADR-0048, ADR-0055, ADR-0056

---
*Reconstructed from commits 45150db, 8e75dee, df10fe7 (2026-03-05 to 2026-03-06); revised 2026-04-17 (pre-commit bootstrap goal — see Revision History).*
