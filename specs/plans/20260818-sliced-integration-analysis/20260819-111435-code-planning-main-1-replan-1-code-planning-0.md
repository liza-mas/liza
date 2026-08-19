# Code Plan: Persist Integration Lifecycle Evidence

## Intent and evidence

Persist one typed, self-validating integration evidence ledger at goal scope and immutable analysis identity at task scope. This task owns durable representation plus structural and transition invariants; dependency-ordered mutation owners derive policy, append or project valid evidence, and perform authorized state changes.

Success means `TestIntegrationLifecycleYAMLRoundTrip` proves every assigned evidence family survives YAML persistence without conflating the analyzed integration source commit with the analyst report commit, and `TestIntegrationLifecycleValidation` proves malformed, duplicate, cross-plan, cross-root, reused, reordered, mutable, non-monotonic, or source-less evidence cannot enter valid state.

Based on:

- The full goal spec at `specs/goals/20260818-sliced-integration-analysis.md`, especially Proposed Model, Required Properties, Success Criteria, and Documentation Impact.
- The authoritative replacement master plan at `specs/plans/20260818-sliced-integration-analysis/20260819-084641-code-planning-main-1-replan-1.md` and the current blackboard outputs for `code-planning-main-1-replan-1` and `code-planning-main-1`.
- The rejected predecessor plan at `specs/plans/20260818-sliced-integration-analysis/20260819-105450-code-planning-main-1-replan-1-code-planning-0.md` and its remaining reviewer blocker: the goal-wide compliance map omitted four slice lifecycle requirements and the no-new-role specialization boundary.
- ADR-0055, ADR-0059, ADR-0067, and ADR-0112; `INVARIANTS.md` §§3, 5, 7, 12 and the Protection Matrix; and the Update Policy plus integration-closure, state-validation-composition, cross-pair, and decomposition-cascade entries in `specs/architecture/architectural-issues.md`.

Load-bearing claims:

- **EVIDENCED — schema ownership:** checked against the assigned task and replacement master output; Task 1 solely owns the lifecycle, analysis-metadata, mutation-receipt, and lifecycle-validation interfaces.
- **EVIDENCED — operational ownership:** checked against current master and replacement outputs; progress, mutation, reconciliation, prompts, completion consumers, E2E evidence, and documentation remain concrete sibling scopes listed below.
- **EVIDENCED — slice lineage closure:** checked against Proposed Model lines 79-136 and reviewer feedback; a slice coverage record is valid only when its plan and exact frozen root set agree with the referenced slice-analysis task metadata, and that analysis reference is used once.
- **EVIDENCED — immutable source identity:** checked against Final Closure and ADR-0112; source/report identities remain distinct, while lock ordering and live-HEAD invalidation are outside this task.

Doc Impact: only this planning artifact and its structured output. `DOC` below owns product and architecture documentation after implementation and E2E evidence exist.

Test Impact: downstream Task 1 adds `internal/models/integration_test.go` and `internal/statevalidate/integration_test.go`; TDD remains colocated with the cohesive schema-and-invariant implementation.

## Current task routing

The compliance matrix uses these aliases. `Task 1` is this plan's sole output; every other alias is a current concrete retained or replacement task, not work added to this scope.

| Alias | Concrete task ID | Responsibility |
|---|---|---|
| Task 1 / `PERSIST` | `code-planning-main-1-replan-1-code-planning-0` | Evidence schema, YAML persistence, structural validation, and append-only/immutable transition validation |
| `CFG` | `code-planning-main-1-code-planning-1` | Configurable global-generation ceiling and deterministic default |
| `TOPO` | `code-planning-main-1-code-planning-2` | Slice/global role-pair topology and frozen-pipeline capability |
| `PROGRESS` | `code-planning-main-1-code-planning-3` | Authoritative pure progress decision, cohort classification, barriers, and effective completion |
| `MUTATE` | `code-planning-main-1-replan-1-code-planning-1` | Replacement mutation-side invalidation and ADR-0112 linearization |
| `RECONCILE` | `code-planning-main-1-replan-1-code-planning-2` | Replacement idempotent analysis materialization and verdict projection |
| `CONTEXT` | `code-planning-main-1-code-planning-6` | Bounded slice context and independent aggregate global context |
| `GATE` | `code-planning-main-1-code-planning-7` | State-changing effective-completion barriers |
| `CONSUMERS` | `code-planning-main-1-code-planning-8` | Wake, supervisor, status, and other terminal consumers |
| `E2E` | `code-planning-main-1-code-planning-9` | End-to-end lifecycle, restart, rescan, exhaustion, and controlled race evidence |
| `DOC` | `code-planning-main-1-code-planning-10` | ADR-0113, lifecycle/operator documentation, and architectural-issue disposition |

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
                                              |-- originating plan + exact roots
                                              `-- immutable source surface

candidate state ---------- ValidateState ----------------> structurally valid
previous + candidate ----- ValidateIntegrationLifecycleTransition --> immutable history
                                      ^
                                      |
                         MUTATE and RECONCILE consume
```

`internal/models` owns durable representation, including `IntegrationMutationReceipt`. `internal/statevalidate` owns candidate-state consistency and before/after immutability. `internal/ops` owns policy, authorization, derivation, task creation, Git mutation, and persistence transactions; it must consume rather than redefine these types and invariants.

This preserves the existing dependency direction: operations and state validation depend on models; models do not import operational policy. Keep the schema concrete and typed. Do not add a generic event map, policy methods to `models`, a second progress evaluator, or a second lifecycle state machine.

## Persistence schema

### Goal-level lifecycle

Add optional `Integration *IntegrationLifecycle` to `models.Goal` with `yaml:"integration,omitempty"`. Legacy state without the field decodes to nil.

Define the durable vocabulary in `internal/models/integration.go`:

| Type | Durable facts |
|---|---|
| `IntegrationLifecycle` | Optional contributing-set snapshot, per-scope coverage union, ordered global generations, mutation receipts, and current closure projection |
| `IntegrationContributingSet` | Settled contributing plan scopes; written once and never replaced or cleared |
| `IntegrationScopeSnapshot` | Originating plan task ID and distinct root coding-lineage IDs |
| `IntegrationCoverageRecord` | Originating plan task ID, kind discriminator, and exactly one approval-attestation or slice-report payload |
| `IntegrationApprovalAttestation` | Reviewed task, acceptance criteria, reviewed commit, approver, validation evidence, and merge commit |
| `IntegrationSliceReport` | Slice analysis task/key, verdict, immutable integration source commit, and analyst report commit |
| `IntegrationGlobalGeneration` | Positive generation, global analysis task/key, verdict, immutable integration source commit, and analyst report commit |
| `IntegrationMutationReceipt` | Mutating task ID and distinct integration-ref before/after commits; `MUTATE` owns production and persistence |
| `IntegrationClosure` | Explicit `clean`, `blocked`, or `exhausted`; clean identity/source fields or blocked/exhausted reason |

Use typed string enums with `IsValid` helpers for coverage kind (`approval_attestation`, `slice_report`), analysis phase (`slice`, `global`), analysis verdict (`clean`, `findings`), and closure status (`clean`, `blocked`, `exhausted`). Absence means not yet recorded.

The coverage payload is a tagged union of two optional pointer payloads. Ordinary YAML handles persistence; structural validation enforces exactly one payload matching the discriminator.

### Per-analysis task metadata

Add optional `IntegrationAnalysis *IntegrationAnalysisMetadata` to `models.Task`, tagged `yaml:"integration_analysis,omitempty" json:"integration_analysis,omitempty"`.

`IntegrationAnalysisMetadata` records deterministic key and phase, global-only generation, slice-only originating plan and exact frozen roots, typed descendant task/commit attribution, immutable analyzed integration source commit, affected paths, and source-snapshot paths. A path may be attributable yet absent from the source-read surface after deletion or rename, so affected paths and snapshot paths remain distinct.

`Task.ReviewCommit` identifies the reviewed analyst artifact. It must never substitute for `IntegrationAnalysisMetadata.SourceCommit` or an evidence record's source commit.

## Validation contract

Create `internal/statevalidate/integration.go` with two public layers:

1. Candidate-only structural validation called by `ValidateState`.
2. `ValidateIntegrationLifecycleTransition(previous, candidate *models.State) error`, the single before/after invariant consumed by every lifecycle mutation owner.

### Structural checks

Accept a nil lifecycle and absent analysis metadata for backward compatibility. When evidence exists, enforce:

- non-empty unique contributing plans; each scope has non-empty unique roots; no root belongs to two scopes;
- unique coverage plans that reference the frozen cohort; partial coverage remains structurally valid because completeness is downstream `PROGRESS` policy;
- coverage discriminator and exactly-one-payload agreement;
- all approval-attestation facts are present;
- each slice report references an existing slice-analysis task whose key and immutable source commit match the report;
- each slice coverage plan equals that task metadata's originating plan, and the metadata root set equals the frozen cohort root set for that plan exactly, independent of ordering;
- one slice-analysis task/key can back exactly one slice coverage record, rejecting cross-plan reuse even when the repeated report fields otherwise match;
- task analysis keys are non-empty and globally unique; source commits are non-empty; slice/global fields match their phase;
- descendant task IDs, commits, affected paths, and snapshot paths are non-empty and duplicate-free;
- global generations are contiguous and strictly increasing from 1 and reference matching global task metadata;
- mutation receipts have a task ID plus distinct non-empty before/after commits;
- clean closure references a clean generation with identical key, generation, and immutable source commit; blocked/exhausted closure has a reason;
- clean slice, global, or closure evidence without an immutable source commit is invalid even when a report commit exists.

These checks close three malformed-reference classes explicitly: coverage for plan A attached to metadata for plan B, metadata roots differing from plan A's frozen roots, and one slice analysis reused by multiple coverage records.

Do not enforce barrier completeness, slice eligibility, generation budget, live-HEAD equality, repair resolution, mutation-side invalidation, aggregate inspection, or effective completion. Those remain with `PROGRESS`, `MUTATE`, `RECONCILE`, `CONTEXT`, `GATE`, and `CONSUMERS`.

### Transition checks

`ValidateIntegrationLifecycleTransition` enforces:

- an existing contributing-set snapshot cannot be cleared or changed;
- existing per-task analysis metadata cannot be cleared or changed;
- coverage records, global generations, and mutation receipts are append-only prefixes: existing entries cannot be removed, reordered, or rewritten;
- structurally valid appends and closure-projection changes remain allowed.

The validator is an enforced interface, not an optional utility:

| Consumer | Lifecycle writes | Required downstream proof |
|---|---|---|
| `MUTATE` | Append typed mutation receipt after releasing the ADR-0112 integration mutation lock | Public `MergeWorktree` validates before persistence, preserves prior evidence, and immediately makes superseded clean evidence ineffective |
| `RECONCILE` | Freeze cohort; attach task metadata; append coverage/generations; project verdict/closure | Public reconciliation/verdict paths reject cohort replacement or prior-evidence rewriting before persistence |

## Test design

### `TestIntegrationLifecycleYAMLRoundTrip`

In `internal/models/integration_test.go`, construct a state with one single-lineage and one multi-lineage scope, both coverage variants, two generations with deliberately distinct source/report commits, multiple task-attributed mutation receipts, clean closure, and full slice/global task metadata. Marshal with `yaml.v3`, unmarshal, and deep-compare lifecycle and task metadata. Add a legacy case proving omitted fields decode to nil.

Assertions must fail if source/report commits are conflated, the slice plan/root boundary is lost, or any assigned evidence family is omitted.

### `TestIntegrationLifecycleValidation`

In `internal/statevalidate/integration_test.go`, build one valid baseline and clone it for deterministic negative subtests. Assert diagnostic substrings and rejection for:

- duplicate task analysis keys;
- replacement or clearing of the frozen cohort through `ValidateIntegrationLifecycleTransition`;
- mutation, removal, or reordering of existing coverage, generation, receipt, or analysis metadata;
- unknown enums, unknown coverage scope, duplicate scope/lineage/path/task values, and both/neither tagged-union payloads;
- missing attestation facts and malformed slice/global task, key, source, or phase references;
- slice coverage plan differing from referenced task metadata originating plan;
- slice task metadata roots differing from the exact frozen roots for its originating plan, including missing, extra, and cross-plan roots;
- one slice analysis task/key reused by multiple slice coverage records;
- zero, gapped, duplicated, or descending generations;
- missing or equal mutation-receipt commits;
- clean slice/global/closure evidence lacking source commit;
- clean closure pointing to findings or mismatching key/generation/source;
- blocked/exhausted closure without reason;
- a valid append-only transition and order-insensitive equality of the same frozen root set.

Exercise structural validation through public `ValidateState`, not only an internal helper. This focused test proves the shared state contract; `MUTATE` and `RECONCILE` prove actual writers cannot bypass it.

## Dependency and change sequence

This specialized plan emits one cohesive coding task. Within that task, the coder should proceed in this dependency order:

1. Add model enums and evidence structs in `internal/models/integration.go`.
2. Wire the optional goal and task fields in `history.go` and `task.go`.
3. Add YAML round-trip and legacy-absence tests in `internal/models/integration_test.go`.
4. Implement structural and transition validation in `internal/statevalidate/integration.go` and wire it into `ValidateState`.
5. Add public-composition, referential-integrity, and transition-invariant tests in `internal/statevalidate/integration_test.go`.

The sequence is internal to one coding task because schema and validator changes share one contract and their tests must evolve atomically. There is no cross-task shared-file conflict.

## Planned coding tasks

### Task 1 — Persist integration lifecycle evidence

Description: Persist typed integration lifecycle evidence for coverage snapshots, analysis identities, verdicts, and closure state.

Done when: `TestIntegrationLifecycleYAMLRoundTrip` preserves the contributing-set snapshot, coverage union, generation records, mutation receipts, and per-task analysis metadata; `TestIntegrationLifecycleValidation` rejects duplicate analysis keys, mutable cohort replacement, malformed evidence, non-monotonic generations, and clean evidence without an immutable source commit.

Scope: Own `internal/models/integration.go`, `internal/models/integration_test.go`, `internal/models/history.go`, `internal/models/task.go`, `internal/statevalidate/integration.go`, `internal/statevalidate/integration_test.go`, and validation wiring in `internal/statevalidate/validate.go`. Define persistence and validation only; do not derive progress, create tasks, mutate Git, or render prompts.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#proposed-model`

Dependencies: none. This task provides persistence and validation interfaces to dependency-ordered current tasks `MUTATE` and `RECONCILE` and the retained consumers above.

Validation: `go test -json ./internal/models ./internal/statevalidate -run '^(TestIntegrationLifecycleYAMLRoundTrip|TestIntegrationLifecycleValidation)$' -count=1 | jq -e -s '([.[] | select(.Action == "pass" and (.Test == "TestIntegrationLifecycleYAMLRoundTrip" or .Test == "TestIntegrationLifecycleValidation")) | .Test] | unique | sort) == ["TestIntegrationLifecycleValidation","TestIntegrationLifecycleYAMLRoundTrip"] and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/models/integration.go, internal/models/integration_test.go, internal/models/history.go, internal/models/task.go, internal/statevalidate/integration.go, internal/statevalidate/integration_test.go, internal/statevalidate/validate.go]`; `owned_modules=[internal/models, internal/statevalidate]`; `read_only_depends_on=[]`; `read_only_task_depends_on=[]`; `interfaces_owned=[IntegrationLifecycle persistence schema, IntegrationAnalysisMetadata persistence schema, IntegrationMutationReceipt persistence schema, integration lifecycle invariant validation]`; `interfaces_consumed=[]`; coverage: durable facts distinguish slice evidence, global generations, immutable source commits, mutation receipts, and blocked or exhausted closure.

## Architectural assessment

| Question | Assessment |
|---|---|
| Problem | Durable facts must distinguish local coverage, global generations, analyzed sources, mutation audit, and closure while preventing evidence from crossing plan/root boundaries. |
| Change vectors | Evidence grows append-only; model ownership, YAML persistence, and `ValidateState` composition remain stable boundaries. |
| Cost of error | High: cross-plan evidence or source-less clean evidence can make unrelated or stale analysis appear authoritative. The model change is reversible before downstream adoption. |
| Failure behavior | Candidate validation rejects malformed relations; transition validation rejects rewriting prior evidence before persistence. |
| Concurrency | Task 1 stores ordered facts only. ADR-0112 lock order and mutation linearization remain with `MUTATE`; idempotent state mutation remains with `RECONCILE`. |
| Data owner | `internal/models` owns representation; `internal/statevalidate` owns consistency/immutability; `internal/ops` owns authorized policy and mutation. |
| Boundary risk | Policy leakage into models, untyped events, cross-plan reference reuse, or writers bypassing the transition validator. The plan excludes each. |

No new architectural issue is introduced. The plan applies the existing model/state-validation boundary and supplies the enforceable evidence contract needed by the recorded integration-closure issue.

## Spec Compliance Matrix

This matrix covers the complete goal, not only Task 1. Task 1 is credited only for the representation or invariant portion it owns; operational ownership is mapped through the concrete aliases in **Current task routing**.

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| O1 | Bounded per-scope coverage precedes global analysis, which repeats after fixes until clean or explicitly blocked. | Objective | `PERSIST` (coverage/generation representation); `PROGRESS`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| PM1 | A one-lineage approval attestation records task, acceptance criteria, reviewed commit, approver, validation, and merge commit. | Proposed Model, lines 49-53 | `PERSIST` (attestation representation/invariants); `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SI1 | Contributing plans and distinct root coding lineages have reproducible identities. | Slice Integration, lines 81-87 | `PERSIST` (plan/root identity); `PROGRESS`; `E2E` | Covered |
| SI2 | Planning settles only after every coding-producing source, output, transition, and resulting coding lineage settles. | Slice Integration, lines 87-94 | `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| SI3 | Slice findings reuse the existing integration-reviewer and coding-pair fix lifecycle. | Slice Integration, lines 118-121 | `TOPO`; `RECONCILE`; `E2E` | Covered |
| SI4 | Slice analyses may run concurrently, and later sibling mutations do not reopen a completed slice. | Slice Integration, lines 123-126 | `PERSIST` (immutable slice identity/snapshot only); `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SI5 | Slice resolution follows merged fix or completed replacement lineage; blocked or abandoned unresolved work blocks integration. | Slice Integration, lines 128-132 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SI6 | Clean slice evidence is slice-local and cannot imply whole-goal completion. | Slice Integration, lines 134-135 | `PERSIST` (slice/global phase and closure separation only); `PROGRESS`; `GATE`; `CONSUMERS` | Covered |
| OOS1 | No new agent roles are introduced; existing role-pair specialization is the mechanism. | Out of Scope, line 294 | `TOPO` | Covered |
| RP1 | Partial planning handoff does not open integration coverage. | Required Properties, bullet 1 | `PROGRESS`; `E2E` | Covered |
| RP2 | The contributing set is evaluated exactly once only after all planning/output/transition/coding prerequisites settle. | Required Properties, bullet 2 | `PERSIST` (immutable snapshot invariant); `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| RP3 | Fewer than two contributing scopes produce no slice analyses. | Required Properties, bullet 3 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| RP4 | Multiple contributing scopes each contribute bounded local coverage. | Required Properties, bullet 4 | `PERSIST` (coverage union); `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| RP5 | A one-lineage scope reuses its approval attestation and produces no slice analysis. | Required Properties, bullet 5 | `PERSIST` (attestation variant); `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| RP6 | A scope with at least two merged root lineages produces exactly one slice analysis. | Required Properties, bullet 6 | `TOPO`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| RP7 | Integration-escalation plans remain repair lineage outside the contributing set and do not create slices. | Required Properties, bullet 7 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| RP8 | Task lineage identifies coding, fix, and replacement tasks belonging to each slice. | Required Properties, bullet 8 | `PERSIST` (plan/root/descendant attribution and referential integrity); `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| RP9 | Each slice receives a bounded surface attributable to its originating plan rather than the whole goal. | Required Properties, bullet 9 | `PERSIST` (bounded metadata); `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| RP10 | Each slice verdict records analyzed descendant changes and immutable source snapshot. | Required Properties, bullet 10 | `PERSIST` (source, descendants, commits, paths); `RECONCILE`; `E2E` | Covered |
| RP11 | Global analysis waits for all planning, coding, repair, required-slice, and slice-resolution barriers; unresolved or blocked slice work prevents it. | Required Properties, bullet 11 | `PROGRESS`; `RECONCILE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| RP12 | Global analysis independently inspects the aggregate branch. | Required Properties, bullet 12 | `CONTEXT`; `E2E` | Covered |
| RP13 | Global fixes and later integration-HEAD mutations trigger another scan while budget remains. | Required Properties, bullet 13 | `CFG`; `PROGRESS`; `MUTATE`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| RP14 | Slice or global-generation exhaustion produces an explicit blocked outcome. | Required Properties, bullet 14 | `PERSIST` (blocked/exhausted representation); `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| RP15 | Clean completion is tied to an immutable reviewed commit. | Required Properties, bullet 15 | `PERSIST` (clean source identity); `PROGRESS`; `MUTATE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| RP16 | Completion state, clean reviewed commit, and integration HEAD have one linearizable relationship. | Required Properties, bullet 16 | `PROGRESS`; `MUTATE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| RP17 | The integration-HEAD mutation path owns invalidation of completion tied to a superseded HEAD. | Required Properties, bullet 17 | `MUTATE`; `E2E` | Covered |
| RP18 | Finalization preserves ADR-0112 lock ordering and no blackboard write occurs while holding the mutation lock. | Required Properties, bullet 18 | `MUTATE`; `RECONCILE`; `E2E` | Covered |
| RP19 | The global generation limit is configurable with a deterministic default. | Required Properties, bullet 19 | `CFG`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| RP20 | Wake evaluation and restart recovery cannot create duplicate slice or global analyses. | Required Properties, bullet 20 | `PERSIST` (unique analysis/reference invariants); `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| RP21 | The workflow remains stack-agnostic and preserves review and merge authorization boundaries. | Required Properties, bullet 21 | `CFG`; `TOPO`; `MUTATE`; `RECONCILE`; `E2E` | Covered |
| SC1 | Coverage does not begin while a planning source/output/transition or resulting coding task remains unsettled. | Success Criterion 1 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SC2 | The contributing set and attestation-vs-slice classification are reproducible, with no slices below two scopes. | Success Criterion 2 | `PERSIST` (cohort/coverage representation); `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SC3 | Every scope in a multi-scope cohort has the correct bounded attestation or exactly one slice analysis. | Success Criterion 3 | `PERSIST` (tagged coverage and slice referential integrity); `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SC4 | Global analysis is unclaimable while any local barrier is unsettled, missing, unresolved, or blocked. | Success Criterion 4 | `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| SC5 | Every slice records a bounded surface and immutable snapshot. | Success Criterion 5 | `PERSIST` (metadata/source invariants); `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| SC6 | Global analysis independently reviews the aggregate after local coverage resolves. | Success Criterion 6 | `PROGRESS`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| SC7 | Successful integration linearizes only when clean source commit equals integration HEAD. | Success Criterion 7 | `PERSIST` (clean source identity); `PROGRESS`; `MUTATE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| SC8 | Controlled concurrency proves both mutation/finalization orders cannot leave durable stale success. | Success Criterion 8 | `MUTATE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| SC9 | Later mutations rescan within budget and block explicitly after exhaustion. | Success Criterion 9 | `CFG`; `PROGRESS`; `MUTATE`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| SC10 | Repeated wake and restart evaluation remains duplicate-free. | Success Criterion 10 | `PERSIST` (duplicate rejection); `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| DOC1 | ADR-0113 extends ADR-0055 with sliced analysis and final closure. | Documentation Impact, bullet 1 | `DOC` | Covered |
| DOC2 | ADR-0113 supersedes ADR-0055's accepted no-rescan limitation. | Documentation Impact, bullet 2 | `DOC` | Covered |
| DOC3 | State-machine and task-lifecycle documentation is updated. | Documentation Impact, bullet 3 | `DOC` | Covered |
| DOC4 | Pipeline and operational documentation covers barriers, generations, and terminal outcomes. | Documentation Impact, bullet 4 | `DOC` | Covered |
| DOC5 | The integration-closure architectural issue is resolved or revised only after implementation and validation evidence exists. | Documentation Impact, bullet 5 | `E2E`; `DOC` | Covered |
| E2E | End-to-end test coverage for the new behavior | Cross-cutting | `E2E` (`code-planning-main-1-code-planning-9`) | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | `DOC` (`code-planning-main-1-code-planning-10`) | Covered |

## Pre-submit audit

- Atomicity: one coding task owns the cohesive schema-plus-invariant boundary and colocated tests.
- Rejection closure: the full goal matrix explicitly maps the Proposed Model's slice lifecycle semantics, every Required Property, every Success Criterion, the no-new-role specialization boundary, E2E impact, and DOC impact to current concrete responsibilities; Task 1 is never credited beyond representation or invariant enforcement for barriers, lifecycle reuse, concurrent execution, repair resolution, aggregate inspection, rescans, mutation linearization, ADR-0112 enforcement, task creation, completion consumers, or recovery idempotency.
- Referential integrity: plan equality, exact frozen-root equality, and single-use slice analysis references are explicit structural checks with deterministic negative tests.
- Ownership: Task 1 solely owns `IntegrationMutationReceipt persistence schema`; `MUTATE` owns receipt production/persistence and Git linearization.
- Enforcement: `MUTATE` and `RECONCILE` depend on this task, consume `integration lifecycle invariant validation`, and name public-operation rejection evidence in the replacement master plan.
- Scope: only this specialized plan and structured output change; no downstream implementation file is touched.
- Shared files: one output entry means no intra-plan collision; replacement and retained tasks remain external dependency-ordered scopes.
- Validation: the JSON-event predicate requires both named top-level tests and rejects any failing Go event.
- Cross-references: each alias is bound once to a concrete current task ID and only credited with its declared master/replacement responsibility.
