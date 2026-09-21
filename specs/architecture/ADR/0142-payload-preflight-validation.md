# ADR-0142: Shared Payload Preflight Validation

## Status

ACCEPTED — 2026-09-20. Records approved decisions D1–D4 in the
[payload-validation plan](../../plans/20260918-fix-gh-issues/20260918-212029-cpm-1-cp-3.md#architecture-decisions).

## Context

Malformed manifests and lifecycle payloads were discovered at mutation time,
causing input-repair cycles and inconsistent diagnostics. Separate preflight
validation would risk accepting payloads that mutation rejects structurally.
The approved issue summary reports representative planning failures; its external
run logs were not independently verified by this documentation task.

There was also an explicit source conflict: output state validation and child
generation required `spec_ref`, while the output write boundary and CLI help
treated it as optional. Deferring failure until a later transition admitted
manifests that durable-state validation could not accept.

## Decision

1. **One structural validator serves both boundaries.** Each command owns a
   versioned schema in `internal/payloadschema`. Preflight and mutation call that
   schema on the same object before the mutation's state lock. Validators take
   no state and do no Git or lock work. Registry ownership and the API remain in
   [Lifecycle Results](../../protocols/lifecycle-results.md#payload-schema-registry).
2. **Canonical objects carry payload, not caller identity.** Include exactly
   the fields read by structural constraints. `mark-blocked.task_id` is included
   because repair-target equality reads it; `agent_id` and identity used only for
   authorization/error labels are excluded. The four command shapes and the
   bare-array lifting rule for `set-task-output` are normative in
   [Payload Validation](../../protocols/payload-validation.md#canonical-objects).
   Existing mutation syntax does not change.
3. **`validate-payload` is state-free and ungated.** It reads no state, acquires
   no state lock, appends no history/receipt and touches no Git. It needs no role
   or `allowed-operations` entry. It lists schema versions and reports bounded,
   secret-safe field diagnostics. Optional outer counter telemetry obeys its
   own availability and leaf-lock contract; it does not grant mutation authority
   or turn validation into a state-dependent operation. Valid preflight returns
   `COMPLETED`/`continue`, `effects=none` and no `changed` field.
4. **Every output entry requires `spec_ref`.** Schema validation rejects its
   absence at write time, aligning the write boundary with state validation and
   transition generation. This resolves the semantic conflict as required,
   rather than relaxing the two stricter validators.

## Consequences

Structural diagnostics and version discovery come from one implementation, so
callers can correct complex payloads before mutation. Diagnostics identify value
classes rather than rejected values. Incompatible payload changes require a
schema-version bump; the [protocol](../../protocols/payload-validation.md#schema-ownership-and-versions)
owns that policy and the command-owner registration workflow.

Parity covers the declared schema checks for `set-task-output`, `mark-blocked`,
`submit-for-review` and `handoff`. It does not certify authorization, current
state, Git existence or every path constraint. Pipeline-dependent decomposition
requiredness, dependency existence/terminality/direction, inheritance against
state and cycle detection remain at mutation. System-wide parity additionally
depends on the separately owned assessment, task creation/replacement, verdict
and RCA schemas and their mutation wiring.

Two extensions remain deferred:

- **`decomposition_root` v2 path:** an explicit canonical-object boolean could
  make missing decomposition metadata preflightable. V1 checks present metadata
  (ownership fields, cross-entry duplicate ownership and read-only dependency
  index ranges), but leaves requiredness to the pipeline resolver. Adding a
  caller-controlled flag now would introduce another value the agent must get
  right without verified evidence that missing blocks caused the cited failures.
- **`internal/paths` access:** task-ID syntax and worktree-prefix normalization
  remain at mutation. The schema import test admits only standard-library,
  `models` and `statevalidate` imports. This is a direct-import restriction, not
  transitive purity: `statevalidate` already imports `paths`, `db`, `git`,
  `pipeline` and `brand`. No wrapper is added merely to re-export an excluded
  helper. A pure-helper split or reviewed allowlist change is future work.

## Alternatives Considered

- Independent preflight validators would duplicate rules and permit divergence.
- Passing all invocation identity to schemas would couple reusable structural
  checks to authorization without need; only structurally consumed identity is
  included.
- RBAC-gating preflight would restrict a state-free check and contradict the
  approved shared contract.
- Making `spec_ref` optional would require weakening existing state and
  generation validation instead of closing the write-now/fail-later path.

## Recorded deltas for documentation reconciliation

These records mirror the [protocol handoff](../../protocols/payload-validation.md#recorded-deltas-for-documentation-reconciliation).
Master output 7 owns register reconciliation and the ADR-0142 index entry, not
the CLI source correction. Historical source coordinates in the following D4
table are reproduced verbatim from the approved plan. Their default-brand path
component is retained only as historical evidence, not an advertised runtime path.

**SetTaskOutput spec_ref Validation Gap:** resolved as **required** on every
entry. The architectural issue can be closed once the stale help line is
corrected; retain that remaining obligation as debt until then.

Consolidate this D4 row verbatim into `TECH_DEBT.md`:

| Field | Value |
|---|---|
| What | `cmd/liza/cmd_task.go:1283` documents `spec_ref` as optional after the schema makes it required |
| Why deferred | `cmd_task.go` is outside every child's `owned_files`; writing it is a master correction (`mp-ownership`) |
| Owner | The **orchestrator** — the only actor in this decomposition that can give `cmd/liza/cmd_task.go` a writer, by master correction or by a new task. No coding child is named, because naming one that cannot write the file would repeat the defect this row exists to fix |
| Payback trigger | The first change to any `cmd/liza/cmd_task.go` help surface, or the master correction that gives the file an owner — whichever comes first |

Consolidate the allowlist-narrowing row into `TECH_DEBT.md`:

| Field | Value |
|---|---|
| What | Task-ID syntax and `spec_ref` / `plan_ref` worktree-prefix normalization remain outside schema validation because `internal/paths` is not directly importable. |
| Why deferred | V1 obeys the reviewed direct-import boundary; neither an allowlist expansion nor a pure-helper split is owned by this change. Mutation retains the checks. |
| Payback trigger | A schema needs task-ID syntax or ref normalization. Decide a pure `internal/paths` split or an explicit allowlist revision before extending schema scope. |

For `architectural-issues.md`, record the **fifth operation register/attribute**:
the state-free, gated/ungated classification introduced by preflight. The four
existing registers are `IsLifecycleOperation`, the metrics matrix, pipeline
`allowed-operations`, and the payload-schema registry. `validate-payload` is
named and counted but deliberately absent from the role allowlist. That absence
currently encodes its classification; this ADR records the drift risk without
claiming a centralized fifth register has been implemented.

## Evidence

Requirement authority is the approved plan's D1–D4 and Task 7, with its pinned
direct references, including the documented `spec_ref` source conflict. The
merged registry documentation supplies the shared API and version rule; the
preflight command implementation supplies the no-task-boundary result and
same-file lifting behavior. These documents record the agreed contract and its
limitations; they do not claim fresh execution of sibling behavioral tests.
