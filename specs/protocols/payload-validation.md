# Payload Validation

## Purpose and authority

Preflight rejects malformed payloads before a lifecycle mutation acquires the
state lock. The same structural validator serves preflight and mutation; success
does not establish authorization, state eligibility, or reference resolution.
This protocol records D1–D4 of the approved
[payload-validation plan](../plans/20260918-fix-gh-issues/20260918-212029-cpm-1-cp-3.md#architecture-decisions)
and [ADR-0142](../architecture/ADR/0142-payload-preflight-validation.md).

The contract is independent of the target project's language, build system and
directory layout. Implementation package names below identify this repository's
components, not requirements imposed on managed projects.

## Canonical objects

The canonical object contains exactly the fields read by structural constraints.
Invocation identity and authorization stay at the mutation boundary. A task ID
belongs in the payload only when a structural constraint reads it:
`mark-blocked` compares `repair_request.target` with `task_id`; the task ID used
only to label a `set-task-output` error is not part of that payload.

The table describes object shape; placeholders such as `OutputEntry` are not
literal JSON values. All four schemas start at version 1.

| Command | Canonical object | Source of keys |
|---|---|---|
| `set-task-output` | `{"output": [OutputEntry, …]}` | Contents of the `--output` file |
| `mark-blocked` | `{"task_id", "reason", "questions": [], "depends_on": [], "repair_request": {}\|null}` | `<task-id>`; `--reason`, `--questions`, `--depends-on`, and `--repair-request-file` or the `--repair-*` fields |
| `submit-for-review` | `{"commit_ref"}` | `[commit-ref]` positional argument |
| `handoff` | `{"summary", "next_action", "succeeded": [], "failed": [], "hypothesis", "key_files": [], "dead_ends": []}` | `<summary>` and `<next-action>`; remaining optional fields mirror `ops.HandoffInput` and have no CLI flags today |

Existing agent-facing mutation syntax is unchanged. `set-task-output` is the
single-file-driven command among these four: its input file contains a bare JSON
array. Preflight and mutation accept the same file representation: preflight lifts
the bare array into `{"output": […]}` and rejects wrapped objects with a diagnostic
directing the caller to pass a bare array. The programmatic
`commands.ValidatePayload(operation, canonicalObject)` entry point still validates
canonical objects directly. Flag-driven payload keys mirror their invocation names,
using underscores for JSON fields such as `next_action` and `commit_ref`.

## Preflight command

In these examples, `<cli>` means the configured executable name:

```text
<cli> validate-payload <operation> --payload <file> [--json]
<cli> validate-payload --list [--json]
```

| Aspect | Contract |
|---|---|
| Input | For a single-file operation, the same bare document the mutation command reads; a wrapped canonical object is rejected. For flag-driven operations, one JSON canonical object. Preflight and mutation use the same structural validator. |
| State access | None: no `state.yaml` read, state lock, history append, Git operation or lifecycle receipt. Validation works outside an initialized project. |
| Authorization | State-free and not RBAC-gated: no role entry, `allowed-operations` row or `validateAllowedOperation` call. It grants no mutation authority. |
| Valid payload | `ok:true`, `outcome=COMPLETED`, `safe_action=continue`, `effects=none`, reported `schema_version`, exit 0. `changed` is absent because there is no task boundary or committed transition. |
| Invalid payload | `ok:false`, `outcome=INVALID_INPUT`, `safe_action=correct_input`, `effects=none`, field diagnostics and a nonzero exit. |
| Unknown operation | `payloadschema.ErrUnknownOperation` becomes `INVALID_INPUT` for the `operation` argument; the diagnostic points to `--list` for discovery. |
| Discovery | `--list` renders `payloadschema.List()` as operation/version rows, sorted by operation; JSON places them in `result.schemas`. Every diagnostic carries `schema_version`. |
| Diagnostics | Follow the [lifecycle result contract](lifecycle-results.md#result-contract): schema version, field path, constraint, value class and safe action. Never echo rejected values. Field diagnostics are JSON-only; text retains outcome and safe action. |
| Telemetry | One outer observation on the `validate-payload` × `{COMPLETED, INVALID_INPUT}` counter rows separates preflight rejections from state conflicts. Follow [counter availability and locking](lifecycle-results.md#coverage-and-observation-counters); unavailable telemetry does not prevent validation, including outside a project. Payloads never become counter keys or retained counter data. |

The no-state/no-lock guarantee applies to validation. Optional diagnostic counter
recording is a separate outer observation using its own counter lock, only when
the required project/sprint identity is available. It must not make validation
depend on a state read. This is not a promise that telemetry always records an
observation.

Callers should preflight complex payloads, correct fields named by structured
diagnostics, and then invoke the mutation command. Passing preflight does not
reserve a task boundary or remove the mutation command's state-dependent checks.

## Validation boundary (normative)

`internal/payloadschema` MUST supply the single structural validator. Both
preflight and the corresponding mutation boundary MUST call its `Validate`
entry point on the canonical object. Mutation validation MUST occur before
`db.For(...)` acquires the state lock. Schema validators MUST take no live state
and perform no state, lock or Git operations.

**Contested boundary (F4), not an exemption:** The operator decision for
`integration-global-2` at `2026-09-21T03:22:42.896045101Z` authorizes a written
finding only. The normative requirements above and the preflight State access
None row remain unchanged. Invocation telemetry currently reads state under an
exclusive lock before mutation validation. The complete preflight CLI reads
state for telemetry after validation but before rendering; its pure schema
validator does not read state. See the
[F4 debt record](../../TECH_DEBT.md#invocation-telemetry-conflicts-with-payload-validation-boundaries-f4)
for verified file/line citations, including output and handoff ordering, and
the preserved report at `72a9b099d57f8a34bf6214abca66e1d00b3f78d5`.

The master Shared Contracts source owner must decide between explicitly
exempting invocation telemetry and moving telemetry after validation consistently
across `mark-blocked`, submission and preflight. Neither candidate is adopted.
Post-validation telemetry alone does not resolve preflight's no-state promise;
moving sprint capture also requires an attribution decision. Payback requires
that authoritative decision, authorized repair and lock-held malformed-payload
regression evidence preserving the chosen sprint semantics.

The independent operator-deferred
[cp-6 assign/preflight RCA limitation](task-lifecycle.md#restore-modes) and
[F1 debt](../../TECH_DEBT.md#rca-assign-restore-conflicts-with-session-preflight-f1)
remain, with AC-161-6/AC-161-8 recovery proof outstanding. Documentation and
supersession resolve neither runtime defect nor supply immutable clean global
integration acceptance.

### Schema-validated rules

Schemas validate required fields, scalar and container types, enumerations,
cardinality, size limits, artifact-reference scalar syntax and command-specific
field relationships within their declared scope. In particular:

- `set-task-output` validates manifest fields and dependency/inheritance shapes.
  Every output entry MUST contain a non-empty `spec_ref`; it is not conditional
  on transition type. Reference syntax is checked without resolving live state.
- Present `decomposition` metadata is validated independently of the role pair:
  ownership declaration fields, duplicate `owned_files` and `interfaces_owned`
  across entries, and `read_only_depends_on` index range. Presence itself is a
  separate mutation-boundary rule below.
- `mark-blocked` validates blocker-question cardinality and the structural
  blocker/repair request, including the target/task-ID relationship.
- `submit-for-review` validates `commit_ref` shape; `handoff` validates its
  required strings and optional structured fields. Git commit existence and
  task eligibility are not structural payload checks.

### Rules retained at mutation

| Rule | Why schema validation cannot decide it in v1 |
|---|---|
| `validateDecompositionRootOutput` requiredness | Needs pipeline resolver `IsDecompositionRoot` / `DecompositionOutputRef` |
| `validateReadOnlyTaskDependsOn`, `task_depends_on` existence/terminality, dependency direction, `validateInheritInputsAgainstState` | Require live `models.State` |
| `validateDependsOnForBlockedTask` and cycle detection | Require live `models.State` |
| `task_depends_on` task-ID syntax | Uses `internal/paths.ValidateTaskID`, outside the schema's direct-import allowlist |
| `spec_ref` / `plan_ref` worktree-prefix normalization | Uses `internal/paths.NormalizeSpecRef`, outside that allowlist |
| Invocation authority, transition eligibility and Git checks | Depend on caller identity, task state or repository state |

The import-boundary test permits only the standard library, `internal/models`
and `internal/statevalidate` in non-test schema files. This is a **direct-import
restriction**, not transitive purity: `statevalidate` itself imports `paths`,
`db`, `git`, `pipeline` and `brand`. Schema code MUST NOT import `internal/paths`
directly or add a `statevalidate` wrapper merely to re-export an excluded helper.
Pure existing validators within the allowlist remain usable.

Consequently, parity is exact for the declared schema checks of these four
commands, not for every possible structural or state-related rejection. A
schema-valid payload can still fail a retained mutation check. System-wide parity
also depends on the separately owned `assess-blocked`, `add-task`, `replace-task`,
`submit-verdict` and RCA schemas and their wiring; this document does not certify
those siblings' implementations.

### Deferred extensions

1. **Decomposition requiredness:** a future v2 canonical object could carry
   `decomposition_root` so preflight can check missing metadata. V1 deliberately
   avoids a new caller-supplied flag that could misidentify the pipeline role;
   malformed metadata already is preflightable. The motivating run-log claim
   concerns malformed manifests, but the plan accepted that claim from the issue
   summary without independently verifying the external logs.
2. **Path helpers:** a future `internal/paths` split or reviewed allowlist change
   could expose pure task-ID syntax and reference-normalization helpers to the
   schema. Until that decision, those checks remain at mutation.

## Schema ownership and versions

Each command owner registers its schema at startup in its own file in
`internal/payloadschema` and wires both boundaries to it. Use the shared
[registry API contract](lifecycle-results.md#payload-schema-registry); this
protocol does not define a second registry API. Discovery reports the active
operation/version pairs.

Versions are stable positive integers. Bump the operation's version on an
**incompatible payload change** (for example a newly required field, rejected
previously accepted shape, or incompatible meaning). A compatible correction or
documentation-only change does not itself require a bump. The proposed
`decomposition_root` requiredness extension is the v2 path, not an undocumented
change to v1.

## Recorded deltas for documentation reconciliation

The documentation-reconciliation child (master output 7) consolidates these
records into its owned registers; it does not invent decisions or change code.

### Required `spec_ref` and stale-help debt

The architectural issue **SetTaskOutput spec_ref Validation Gap** is decided as
**required**: schema/write, state-validation and transition-generation contracts
agree on every entry.

**Debt paid:** commit `43dacebcfd2e61d37456f5104690999202388c95`
(`fix(cli): document required output spec_ref`) corrects the help to require
`desc`, `done_when`, `scope`, and `spec_ref`, and excludes `spec_ref` from the
optional-field list. A new `TestMutationCommandWiring` subtest pins both lists;
it fails against the stale help and passes after the correction. The same
commit removes the stale-help entry from `TECH_DEBT.md`. Help alignment is
resolved; the D4 debt record in ADR-0142 remains historical.

### Allowlist-narrowing debt

Consolidate this second row into `TECH_DEBT.md`:

| Field | Value |
|---|---|
| What | Task-ID syntax and `spec_ref` / `plan_ref` worktree-prefix normalization remain outside schema validation because `internal/paths` is not directly importable. |
| Why deferred | V1 obeys the reviewed direct-import boundary; neither an allowlist expansion nor a pure-helper split is owned by this change. Mutation retains the checks. |
| Payback trigger | A schema needs task-ID syntax or ref normalization. Decide a pure `internal/paths` split or an explicit allowlist revision before extending schema scope. |

### Fifth-operation-register traceability

Record in `architectural-issues.md` that preflight adds a **fifth operation
register/attribute**: the state-free, gated/ungated classification. The existing
four are `IsLifecycleOperation`, the lifecycle metrics matrix, pipeline
`allowed-operations`, and the payload-schema registry. `validate-payload` is
lifecycle-named and counted but deliberately absent from `allowed-operations`;
absence currently carries the ungated classification. This remains a drift risk,
not a new centralized register implemented by this task.

Reconciliation also owns the ADR index entry for ADR-0142 and any resulting
`INVARIANTS.md`, blackboard-schema and spec-mapping updates. The integration
help-alignment fix above paid the separately assigned CLI help debt.
