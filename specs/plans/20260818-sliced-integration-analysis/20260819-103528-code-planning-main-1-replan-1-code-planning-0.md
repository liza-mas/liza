# Code Plan: Persist Integration Lifecycle Evidence

## Intent and evidence

Persist one typed, self-validating integration evidence ledger at goal scope and immutable analysis identity at task scope. This task owns the durable vocabulary and its structural and transition invariants; dependency-ordered mutation owners may append or project evidence only through that invariant boundary.

Success means `TestIntegrationLifecycleYAMLRoundTrip` proves all assigned evidence survives YAML persistence without conflating the analyzed integration source commit with the analyst report commit, and `TestIntegrationLifecycleValidation` proves malformed, duplicate, reordered, mutable, non-monotonic, or source-less evidence cannot enter valid state.

Based on:

- The full goal spec at `specs/goals/20260818-sliced-integration-analysis.md`, especially Proposed Model, Slice Integration, Global Integration, Final Closure, Required Properties, and Success Criteria.
- The authoritative replacement master plan at `specs/plans/20260818-sliced-integration-analysis/20260819-084641-code-planning-main-1-replan-1.md` and its blackboard output. Task 1 solely owns the lifecycle, analysis-metadata, mutation-receipt, and transition-validation interfaces; replacement Tasks 2 and 3 consume those interfaces.
- The prior accepted specialized design at commit `a2920bc0`, whose review blocker concerned stale authoritative parent output rather than this schema, validator, task decomposition, or compliance matrix. The replacement master output now carries the corrected ownership and consumer contracts.
- ADR-0055, ADR-0059, ADR-0067, and ADR-0112; `INVARIANTS.md` §§1-3, 5-7 and the Protection Matrix; and the Update Policy plus integration-closure, state-validation-composition, cross-pair, and decomposition-cascade issues in `specs/architecture/architectural-issues.md`.
- Current source verification: `models.Goal` is defined in `internal/models/history.go`; `models.Task` is defined in `internal/models/task.go`; and `statevalidate.ValidateState` composes candidate-state validators in `internal/statevalidate/validate.go`.

Load-bearing claims:

- **EVIDENCED — schema ownership:** checked against the assigned task, replacement master output, and owned-file decomposition.
- **EVIDENCED — candidate-state enforcement:** checked against the current `ValidateState` validator chain.
- **EVIDENCED — immutable source identity:** checked against the goal's Final Closure requirements and ADR-0112's mutation ordering.
- **EVIDENCED — downstream enforcement:** checked against replacement master Tasks 2 and 3, which require public mutation/reconciliation paths to consume this transition validator before persistence.

Doc Impact: none in this task. Retained master Task 11 owns documentation after implementation and end-to-end evidence exist.

Test Impact: add `internal/models/integration_test.go` and `internal/statevalidate/integration_test.go`; no separate test task because TDD is colocated with the cohesive schema-and-invariant implementation.

## Architecture and ownership boundary

```text
state.yaml
   |
   +-- goal.integration -----------------> IntegrationLifecycle
   |                                          |-- frozen contributing set
   |                                          |-- tagged coverage records
   |                                          |-- ordered global generations
   |                                          |-- mutation receipts
   |                                          `-- closure projection
   |
   `-- tasks[].integration_analysis ------> IntegrationAnalysisMetadata
                                              |-- deterministic identity
                                              |-- slice/global boundary
                                              `-- immutable source surface

candidate state ---------- ValidateState ----------------> structurally valid
previous + candidate ----- ValidateIntegrationLifecycleTransition --> immutable transition
                                      ^
                                      |
                 replacement Task 2 mutation receipt append
                 replacement Task 3 reconciliation/verdict projection
```

`internal/models` owns durable representation, including `IntegrationMutationReceipt`. `internal/statevalidate` owns candidate-state structure and before/after immutability. `internal/ops` remains the policy and persistence layer and must not redefine these schema types.

The boundary preserves the existing stable dependency direction: operations and state validation depend on models; models do not import operational policy. The schema is reversible at the code level but high-impact because multiple downstream tasks consume it. Keep it concrete and typed; do not add a generic event map, policy methods in `models`, or another lifecycle state machine.

## Persistence schema

### Goal-level lifecycle

Add optional `Integration *IntegrationLifecycle` to `models.Goal` with `yaml:"integration,omitempty"`. Legacy state without the field decodes to nil.

Define the durable vocabulary in `internal/models/integration.go`:

| Type | Durable facts |
|---|---|
| `IntegrationLifecycle` | Optional contributing-set snapshot, per-scope coverage union, ordered global generations, mutation receipts, and current closure projection. |
| `IntegrationContributingSet` | Settled contributing plan scopes; written once and never replaced or cleared. |
| `IntegrationScopeSnapshot` | Originating plan task ID and distinct root coding-lineage IDs. |
| `IntegrationCoverageRecord` | Originating plan task ID, kind discriminator, and exactly one approval-attestation or slice-report payload. |
| `IntegrationApprovalAttestation` | Reviewed task, acceptance criteria, reviewed commit, approver, validation evidence, and merge commit. |
| `IntegrationSliceReport` | Slice analysis task/key, verdict, immutable integration source commit, and analyst report commit. |
| `IntegrationGlobalGeneration` | Positive generation, global analysis task/key, verdict, immutable integration source commit, and analyst report commit. |
| `IntegrationMutationReceipt` | Mutating task ID and distinct integration-ref before/after commits. This task owns the durable type; replacement Task 2 owns producing and persisting instances. |
| `IntegrationClosure` | Explicit `clean`, `blocked`, or `exhausted`; clean identity/source fields or blocked/exhausted reason. |

Use typed string enums with `IsValid` helpers for coverage kind (`approval_attestation`, `slice_report`), analysis phase (`slice`, `global`), analysis verdict (`clean`, `findings`), and closure status (`clean`, `blocked`, `exhausted`). Absence means not yet recorded.

The coverage payload is a tagged union of two optional pointer payloads. Ordinary YAML handles persistence; structural validation enforces exactly one payload matching the discriminator.

### Per-analysis task metadata

Add optional `IntegrationAnalysis *IntegrationAnalysisMetadata` to `models.Task`, tagged `yaml:"integration_analysis,omitempty" json:"integration_analysis,omitempty"`.

`IntegrationAnalysisMetadata` records deterministic key and phase, global-only generation, slice-only originating plan and roots, typed descendant task/commit attribution, immutable analyzed integration source commit, affected paths, and source-snapshot paths. A path may be attributable yet absent from the source-read surface after deletion or rename, so affected paths and snapshot paths remain distinct.

`Task.ReviewCommit` identifies the reviewed analyst artifact. It must never substitute for `IntegrationAnalysisMetadata.SourceCommit` or an evidence record's source commit.

## Validation contract

Create `internal/statevalidate/integration.go` with two public layers:

1. Candidate-only structural validation called by `ValidateState`.
2. `ValidateIntegrationLifecycleTransition(previous, candidate *models.State) error`, the single before/after invariant consumed by every lifecycle mutation owner.

### Structural checks

Accept a nil lifecycle and absent analysis metadata for backward compatibility. When evidence exists, enforce:

- non-empty unique contributing plans; each scope has non-empty unique roots; no root belongs to two scopes;
- unique coverage plans that reference the frozen cohort; partial coverage remains structurally valid because completeness is downstream progress policy;
- coverage discriminator and exactly-one-payload agreement;
- all approval-attestation facts are present;
- slice reports reference matching slice-analysis task key and source and contain a valid verdict plus report commit;
- task analysis keys are non-empty and globally unique; source commits are non-empty; slice/global fields match their phase;
- descendant task IDs, commits, affected paths, and snapshot paths are non-empty and duplicate-free;
- global generations are contiguous and strictly increasing from 1 and reference matching global task metadata;
- mutation receipts have a task ID plus distinct non-empty before/after commits;
- clean closure references a clean generation with identical key, generation, and immutable source commit; blocked/exhausted closure has a reason;
- clean slice, global, or closure evidence without an immutable source commit is invalid even when a report commit exists.

Do not enforce barrier completeness, slice eligibility, generation budget, live-HEAD equality, repair resolution, or effective completion. Those remain policy in dependency-ordered progress and reconciliation tasks.

### Transition checks

`ValidateIntegrationLifecycleTransition` enforces:

- an existing contributing-set snapshot cannot be cleared or changed;
- existing per-task analysis metadata cannot be cleared or changed;
- coverage records, global generations, and mutation receipts are append-only prefixes: existing entries cannot be removed, reordered, or rewritten;
- structurally valid appends and closure-projection changes remain allowed.

The validator is an enforced interface, not an optional utility. The authoritative replacement master plan assigns its consumers as follows:

| Replacement master task | Lifecycle writes | Required consumption and public-path proof |
|---|---|---|
| Task 2 — Linearize HEAD invalidation | Append a typed mutation receipt after the ADR-0112 integration lock is released. | Consumes the mutation-receipt schema and lifecycle validator; `TestIntegrationMutationLinearization` exercises public `MergeWorktree`, preserves prior evidence, and proves validation precedes persistence. |
| Task 3 — Reconcile analysis generations | Freeze cohort; append coverage/generations; attach analysis metadata; project verdict/closure. | Consumes analysis metadata and lifecycle validator; `TestReconcileIntegrationAnalyses` exercises public reconciliation/verdict paths and rejects replacement or rewriting before persistence. |

## Test design

### `TestIntegrationLifecycleYAMLRoundTrip`

In `internal/models/integration_test.go`, construct a state with one single-lineage and one multi-lineage scope, both coverage variants, two generations with deliberately distinct source/report commits, multiple task-attributed mutation receipts, clean closure, and full slice/global task metadata. Marshal with `yaml.v3`, unmarshal, and deep-compare lifecycle and task metadata. Add a legacy case proving omitted fields decode to nil.

The assertions must fail if source/report commits are conflated or if any required evidence family is omitted.

### `TestIntegrationLifecycleValidation`

In `internal/statevalidate/integration_test.go`, build one valid baseline and clone it for deterministic negative subtests. Assert diagnostic substrings and rejection for:

- duplicate task analysis keys;
- replacement or clearing of the frozen cohort through `ValidateIntegrationLifecycleTransition`;
- mutation, removal, or reordering of existing coverage, generation, receipt, or analysis metadata;
- unknown enums, unknown coverage scope, duplicate scope/lineage/path/task values, and both/neither tagged-union payloads;
- missing attestation facts and malformed slice/global references;
- zero, gapped, duplicated, or descending generations;
- missing or equal mutation-receipt commits;
- clean slice/global/closure evidence lacking source commit;
- clean closure pointing to findings or mismatching key/generation/source;
- blocked/exhausted closure without reason;
- a valid append-only transition.

Exercise structural validation through public `ValidateState`, not only an internal helper. This focused test proves the shared transition contract; replacement Tasks 2 and 3 prove actual mutation owners cannot bypass it.

## Dependency and change sequence

This specialized plan emits one cohesive coding task. Within that task, the coder should proceed in this dependency order:

1. Add model enums and evidence structs in `internal/models/integration.go`.
2. Wire the optional goal and task fields in `history.go` and `task.go`.
3. Add YAML round-trip and legacy-absence tests in `integration_test.go` under `internal/models`.
4. Implement structural and transition validation in `internal/statevalidate/integration.go` and wire it into `ValidateState`.
5. Add public-composition and transition-invariant tests in `internal/statevalidate/integration_test.go`.

The sequence is internal to one coding task because schema and validator changes share one contract and the tests must evolve atomically with that contract. There is no cross-task shared-file conflict.

## Planned coding tasks

### Task 1 — Persist integration lifecycle evidence

Description: Persist typed integration lifecycle evidence for coverage snapshots, analysis identities, verdicts, and closure state.

Done when: `TestIntegrationLifecycleYAMLRoundTrip` preserves the contributing-set snapshot, coverage union, generation records, mutation receipts, and per-task analysis metadata; `TestIntegrationLifecycleValidation` rejects duplicate analysis keys, mutable cohort replacement, malformed evidence, non-monotonic generations, and clean evidence without an immutable source commit.

Scope: Own `internal/models/integration.go`, `internal/models/integration_test.go`, `internal/models/history.go`, `internal/models/task.go`, `internal/statevalidate/integration.go`, `internal/statevalidate/integration_test.go`, and validation wiring in `internal/statevalidate/validate.go`. Define persistence and validation only; do not derive progress, create tasks, mutate Git, or render prompts.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#proposed-model`

Dependencies: none. This task provides persistence and validation interfaces to the dependency-ordered replacement master Tasks 2 and 3 and retained downstream consumers.

Validation: `go test -json ./internal/models ./internal/statevalidate -run '^(TestIntegrationLifecycleYAMLRoundTrip|TestIntegrationLifecycleValidation)$' -count=1 | jq -e -s '([.[] | select(.Action == "pass" and (.Test == "TestIntegrationLifecycleYAMLRoundTrip" or .Test == "TestIntegrationLifecycleValidation")) | .Test] | unique | sort) == ["TestIntegrationLifecycleValidation","TestIntegrationLifecycleYAMLRoundTrip"] and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/models/integration.go, internal/models/integration_test.go, internal/models/history.go, internal/models/task.go, internal/statevalidate/integration.go, internal/statevalidate/integration_test.go, internal/statevalidate/validate.go]`; `owned_modules=[internal/models, internal/statevalidate]`; `read_only_depends_on=[]`; `read_only_task_depends_on=[]`; `interfaces_owned=[IntegrationLifecycle persistence schema, IntegrationAnalysisMetadata persistence schema, IntegrationMutationReceipt persistence schema, integration lifecycle invariant validation]`; `interfaces_consumed=[]`; coverage: durable facts distinguish slice evidence, global generations, immutable source commits, mutation receipts, and blocked or exhausted closure.

## Architectural assessment

| Question | Assessment |
|---|---|
| What problem is being solved? | Durable evidence must distinguish local coverage, global generations, immutable analyzed sources, mutation audit, and closure without letting downstream writers reinterpret those facts. |
| What changes and what remains stable? | Evidence grows through append-only lifecycle mutations; model ownership, YAML persistence, and `ValidateState` composition remain stable boundaries. |
| Cost of being wrong | High: mutable cohort or source-less clean evidence can make stale integration completion appear valid. The change remains reversible before downstream adoption. |
| Failure behavior | Candidate-state validation rejects malformed evidence; transition validation rejects rewriting prior evidence before persistence. |
| Concurrency model | This task stores ordered facts only. ADR-0112 lock order and mutation linearization remain owned by replacement Task 2. |
| Data and invariant owner | `internal/models` owns representation; `internal/statevalidate` owns structure and immutability; `internal/ops` owns authorized mutations. |
| Boundary risks | Policy leakage into models, a generic untyped event log, or lifecycle writers bypassing the transition validator. The plan excludes all three. |

No new architecture issue is introduced by this plan. It applies the existing model/state-validation boundary and supplies the missing enforceable evidence contract for the already-recorded integration-closure issue.

## Spec Compliance Matrix

This matrix covers every goal requirement allocated to the persistence-and-validation slice. Progress derivation, task creation, Git mutation, prompt rendering, completion consumers, end-to-end tests, and documentation remain dependency-ordered sibling responsibilities in the authoritative replacement master plan.

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Persist the settled contributing plan set and distinct root coding lineages as one reproducible snapshot. | Slice Integration; Required Properties 1-2, 8 | Task 1 | Covered |
| 2 | Make the contributing set immutable once evaluated; escalation work cannot replace it. | Slice Integration; Required Properties 2, 7 | Task 1 | Covered |
| 3 | Represent every contributing scope through one uniform bounded coverage map. | Slice Integration; Required Property 4 | Task 1 | Covered |
| 4 | Represent one-lineage coverage with task, criteria, reviewed commit, approver, validation, and merge commit. | Proposed Model; Required Property 5 | Task 1 | Covered |
| 5 | Represent multi-lineage coverage as a slice report without implying goal completion. | Slice Integration; Required Properties 6, 17 | Task 1 | Covered |
| 6 | Attribute roots, descendant tasks, commits, and paths to an originating plan. | Slice Integration; Required Properties 8-10 | Task 1 | Covered |
| 7 | Bind each analysis and verdict to an immutable source commit and bounded snapshot paths. | Slice Integration; Required Property 10; Success Criterion 5 | Task 1 | Covered |
| 8 | Keep approval and slice evidence as an unambiguous tagged union. | Global Integration coverage record | Task 1 | Covered |
| 9 | Persist ordered global generations and verdicts. | Global Integration; Final Closure; Required Property 13 | Task 1 | Covered |
| 10 | Persist explicit blocked or exhausted closure. | Final Closure; Required Property 14 | Task 1 | Covered |
| 11 | Persist clean completion against the immutable reviewed source commit. | Final Closure; Required Property 15; Success Criterion 7 | Task 1 | Covered |
| 12 | Keep analyzed source commit distinct from analyst report commit. | Proposed Model; Final Closure | Task 1 | Covered |
| 13 | Persist typed task-attributed before/after mutation receipts with one schema owner. | Final Closure invalidation ownership | Task 1 | Covered |
| 14 | Give slice/global analyses deterministic unique durable identities. | Required Property 20; Success Criterion 10 | Task 1 | Covered |
| 15 | Reject duplicate analysis keys and malformed or inconsistent evidence. | Assigned Done When | Task 1 | Covered |
| 16 | Reject non-monotonic global generations. | Assigned Done When; bounded generation cycle | Task 1 | Covered |
| 17 | Reject clean evidence without immutable source commit. | Assigned Done When; Final Closure | Task 1 | Covered |
| 18 | Preserve lifecycle and task metadata across YAML while legacy absent fields remain compatible. | Assigned Done When; persisted-state contract | Task 1 | Covered |
| 19 | Make cohort and append-only evidence invariants enforceable at every lifecycle persistence owner. | Required Properties 2, 16-17, 20; Success Criteria 7-10 | Task 1; replacement master Tasks 2-3 | Covered |
| 20 | Remain stack-agnostic and preserve review/merge authorization. | Required Property 21; Out of Scope | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: retained master Task 10 owns cross-component lifecycle and race evidence; this internal schema task supplies the validated state contract it consumes. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: retained master Task 11 documents implemented behavior only after Task 10 evidence. | N/A |

## Pre-submit audit

- Atomicity: one coding task owns the cohesive schema-plus-invariant boundary and colocated tests.
- Ownership: Task 1 solely owns `IntegrationMutationReceipt persistence schema`; replacement master Task 2 owns receipt production/persistence and the Git linearization protocol.
- Enforcement: replacement master Tasks 2 and 3 both depend on this task, consume `integration lifecycle invariant validation`, and name public-operation rejection evidence.
- Scope: only the specialized plan and structured output are changed; no implementation file is touched.
- Shared files: one specialized output entry means no intra-plan collision; the replacement master tasks have disjoint owned-file sets and explicit dependency order.
- Validation: the JSON-event predicate requires both named top-level tests and rejects any failing Go event.
- Cross-references: every replacement/retained task reference above matches the responsibility declared in the authoritative replacement master plan.
- Compliance: all requirements allocated to this specialized scope are Covered; E2E and documentation remain explicit retained responsibilities.
