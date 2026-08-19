# Code Plan: Authoritative Integration Progress Decision

## Intent and evidence

Define one pure, deterministic decision boundary for integration coverage, slice eligibility, repair resolution, global generation readiness, exhaustion, and effective completion. The function reads durable state plus the already-owned pipeline capability, normalized generation budget, and live integration HEAD; it returns facts and requested analysis identities without writing state, creating tasks, reading prompts, or mutating Git.

Success means `TestEvaluateIntegrationProgress` proves every assigned branch of the decision and repeated evaluation of the same state returns byte-for-byte equivalent keys and decisions. A clean lifecycle projection is effective only when its immutable source commit equals the supplied live HEAD and every barrier remains satisfied.

Based on:

- `specs/goals/20260818-sliced-integration-analysis.md`, read in full, especially Slice Integration, Global Integration, Final Closure, Required Properties, and Success Criteria.
- The retained Task 4 contract in `specs/plans/20260818-sliced-integration-analysis/20260819-005612-code-planning-main-1.md` and the replacement ownership graph in `specs/plans/20260818-sliced-integration-analysis/20260819-084641-code-planning-main-1-replan-1.md`.
- The corrected persistence contract at commit `a2920bc0`, which defines the frozen contributing set, tagged coverage records, ordered global generations, immutable analysis source commits, task analysis metadata, mutation receipts, and closure projection.
- The merged configuration and pipeline interfaces in `internal/models/config.go` and `internal/pipeline/config.go` / `resolver.go`: `NormalizeGlobalIntegrationGenerationLimit` and `SlicedIntegrationCapability`.
- ADR-0055, ADR-0059, ADR-0065 as superseded in part by ADR-0077, ADR-0067, and ADR-0112; `INVARIANTS.md` task-state, concurrency, review, integration, scope, governance, process, and Protection Matrix sections; and the Update Policy plus integration-closure and decomposition-risk entries in `specs/architecture/architectural-issues.md`.
- Direct source reads of task ancestry and replacement fields in `internal/models/task.go`, child provenance in `internal/ops/proceed.go`, partial planning handoff in `internal/ops/advance_sprint.go` and `internal/agent/workdetection.go`, and pipeline-derived terminal behavior in `internal/models/sprint.go` and `internal/ops/pipeline_ops.go`.

Load-bearing claims checked against source:

- Child tasks retain their source task in `ParentTasks`, so plan, coding-root, analysis, repair, and escalation ancestry can be reconstructed without prompt or Git reads (`internal/ops/proceed.go`).
- Supersession can branch through `SupersededBy`; replacement satisfaction must therefore walk recursively, reject cycles or missing nodes, and require every replacement leaf to resolve (`internal/models/task.go`, ADR-0065/ADR-0077).
- Partial planning handoff intentionally exposes merged output before the whole planning boundary settles, so that wake signal cannot be reused as coverage settlement (`internal/ops/advance_sprint.go`, `internal/agent/workdetection.go`, ADR-0059).
- Live HEAD is an external observation supplied to the pure evaluator; the decision must not read or mutate Git and must not treat a report commit as the analyzed source commit (goal spec Final Closure, ADR-0112, corrected persistence plan).

Doc Impact: none in this task. Master Task 11 owns product, architecture, protocol, configuration, and issue-registry updates after implementation and end-to-end evidence exist.

Test Impact: add `internal/ops/integration_progress_test.go` with the single required top-level test and deterministic subtests; no integration-layer test is added because master Task 10 owns cross-component lifecycle and race evidence.

## Architecture boundary

```text
models.State + SlicedIntegrationCapability + live integration HEAD
                  |
                  v
        EvaluateIntegrationProgress
          |       |       |       |
       cohort  coverage  global  closure
          |       |       |       |
          +------- IntegrationProgressDecision
                          |
        readers may reconcile or gate elsewhere; this task never mutates
```

`internal/models` remains the sole durable-evidence owner, `internal/pipeline` remains the capability owner, and later reconciliation remains the sole lifecycle/task-creation writer. `internal/ops/integration_progress.go` owns only policy derivation. This separation keeps the decision independently testable and prevents wake, reconciliation, completion, and mutation consumers from re-implementing subtly different predicates.

The evaluator should expose a small input API equivalent to:

```go
func EvaluateIntegrationProgress(
    state *models.State,
    capability pipeline.SlicedIntegrationCapability,
    integrationHEAD string,
) (IntegrationProgressDecision, error)
```

The exact field spelling may follow package conventions, but `IntegrationProgressDecision` must carry these semantic outputs without side effects:

- the existing frozen cohort or the one canonical cohort candidate and whether it must be frozen;
- effective per-scope coverage plus missing slice requests, each with originating plan, sorted root lineage IDs, and deterministic key;
- whether all local barriers permit global analysis, and the next global generation/key/source commit when one is required;
- whether integration is effectively complete for the supplied HEAD;
- a machine-readable blocked/exhausted reason plus stable diagnostic context when progress cannot continue.

Malformed references, ancestry cycles, impossible tagged evidence, or contradictory durable facts return an error and therefore fail closed. Expected workflow states such as unsettled planning, waiting repairs, pipeline upgrade required, blocked findings, and exhausted generations are represented in the decision rather than as errors.

## Deterministic evaluation contract

### 1. Build a canonical graph view

Index tasks by ID once. Derive plan ancestry, root coding lineages, analysis descendants, repair descendants, and escalation descendants from `ParentTask` / `ParentTasks`, with task IDs sorted before emitting any slice, coverage, or diagnostic collection. Never depend on state slice order or Go map iteration.

Replacement resolution follows `SupersededBy` recursively. A superseded node is resolved only when every referenced replacement branch exists and resolves to merged work; a pending replacement keeps the barrier waiting, while a missing/cyclic replacement or an abandoned/blocked leaf produces a stable blocking reason. Do not use direct dependency claimability as a substitute for finding-resolution ancestry.

### 2. Settle and freeze the contributing cohort exactly once

If `Goal.Integration.ContributingSet` already exists, reuse it unchanged and ignore later integration-repair planning descendants for membership. Do not recompute or replace it.

If it is absent, emit no freeze candidate until all pre-integration planning sources are terminal, every eligible coding-producing output and transition from those sources is consumed, and every resulting root coding lineage is terminal through replacement resolution. In particular, a merged plan with early unconsumed output from partial handoff keeps settlement false.

At the first settled evaluation, derive contributing scopes only from pre-integration code-planning scopes with at least one distinct root coding lineage that produced merged work. Exclude code-planning tasks descended from slice/global analyses or their repair/escalation descendants. Canonically sort scopes by plan task ID and roots by root task ID. Repeated evaluation before persistence returns the identical candidate; after persistence the stored cohort is authoritative.

### 3. Classify bounded local coverage

- With fewer than two contributing scopes, request no slice analyses and allow the workflow to approach the global barrier directly.
- With at least two scopes, every scope must have exactly one bounded coverage route.
- A one-root scope projects the existing coding-review approval attestation required by the persistence schema and requests no slice.
- A scope with at least two distinct roots requests exactly one slice analysis with key `slice:<originating-plan-task-id>`.
- Repeated evaluation, task-order permutations, sibling mutations, and restart recovery must not change or duplicate a slice key.
- If any slice is required and `SlicedIntegrationCapability.Available` is false, block with the capability's `Code` and `Guidance`; never silently bypass the slice.
- A persisted slice report is effective only for its matching deterministic key, plan, roots, analysis metadata, and immutable snapshot. A clean slice settles that scope only; it never sets effective global completion.

### 4. Resolve findings and global readiness

Treat findings as resolved only when each repair lineage reaches merged work or every superseded branch recursively reaches merged replacement work. A blocked or abandoned finding without completed replacement work blocks integration. Pending repair or replacement work waits without creating another analysis.

Global analysis is ready only when all of these barriers hold simultaneously:

1. the pre-integration planning boundary is settled and the cohort is frozen or has a canonical freeze candidate;
2. all currently planned coding and integration-repair work is terminal through replacement resolution;
3. every required scope has an approval attestation or a created slice analysis;
4. every created slice is resolved and none is blocked;
5. no existing global findings remain unresolved.

Integration escalation plans and their coding descendants remain repair ancestry: they participate in the current-work barrier and the next global source HEAD, but never join the frozen cohort or create slices.

### 5. Derive global generations and effective completion

Normalize `state.Config.MaxGlobalIntegrationGenerations` with `models.NormalizeGlobalIntegrationGenerationLimit`. Global generation keys are `global:<positive-generation>`; the generation's immutable `SourceCommit` is the supplied non-empty integration HEAD. The next generation is one greater than the highest valid contiguous persisted generation and is requested only when all global barriers are ready and no analysis for that generation already exists.

A clean global generation is effective completion only when its clean lifecycle projection names the same generation/key/source commit and that source commit equals the supplied live integration HEAD. A clean result bound to any other HEAD is stale immediately: effective completion is false, and another generation is requested when budget and barriers allow.

If no current-HEAD clean evidence exists and the normalized generation limit has been reached, return an explicit exhausted/blocked decision and request no further global analysis. Findings, repair merges, or any other HEAD mutation request a later generation only while budget remains.

### 6. Preserve purity

The evaluator must not mutate the supplied state, append lifecycle evidence, set task metadata, create tasks, read pipeline files/prompts, query Git, or depend on time/randomness. The test deep-compares the input state before and after representative evaluations.

## Test design

Add `TestEvaluateIntegrationProgress` as one top-level test with table-driven or clearly named subtests. Use compact fixture builders in the owned test file and assert complete decisions rather than only booleans or type shapes.

Required positive/negative pairs:

1. Partial handoff remains unsettled until every planning source, eligible output/transition, and resulting coding lineage settles; only then is one canonical freeze candidate emitted.
2. Re-evaluation before and after persistence proves the cohort freezes exactly once and late integration-escalation planning cannot alter it.
3. Zero/one contributing scope yields no slice; two scopes activate bounded coverage for every scope.
4. A one-lineage scope yields a complete approval attestation and no slice; a multi-lineage scope yields exactly `slice:<plan-id>` once.
5. Missing slice capability blocks with `pipeline_upgrade_required` when a slice is required but does not block the fewer-than-two-scope bypass.
6. Escalation-plan descendants remain repair lineage and cannot become contributing scopes or slice requests.
7. Multi-level and branched supersession resolves only when every replacement leaf merges; pending waits, while missing/cyclic/blocked/abandoned leaves fail closed or block as appropriate.
8. Missing slice, unresolved slice, blocked slice finding, active coding work, active repair work, and unresolved global finding each prevent global readiness; satisfying all barriers permits exactly one global request.
9. Repeated evaluation and task-order permutation return the same slice/global keys and no duplicate request.
10. Clean evidence for current HEAD sets effective completion; the same evidence against a different supplied HEAD is ineffective and requests the next generation while budget remains.
11. Zero/negative configuration normalizes to the default limit; a positive configured limit is honored; reaching either limit without current clean evidence returns exhausted and no request.
12. Representative evaluations leave the input state deeply unchanged.

The canonical validation must fail if the named top-level test is absent and must reject every Go failure event:

`go test -json ./internal/ops -run '^TestEvaluateIntegrationProgress$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestEvaluateIntegrationProgress") and all(.[]; .Action != "fail")'`

## Planned coding tasks

### Task 1 — Compute authoritative integration progress

Description: Compute a single deterministic integration progress decision from state, pipeline capability, generation budget, and integration HEAD.

Done when: `TestEvaluateIntegrationProgress` proves partial handoff cannot settle coverage, the cohort freezes exactly once, fewer than two scopes create no slices, one-lineage scopes produce attestations, multi-lineage scopes produce one slice key, escalation plans stay repair lineage, replacements resolve recursively, blocked or abandoned findings block, global readiness waits for all barriers, stale clean evidence is ineffective, and exhausted generations block.

Scope: Own `internal/ops/integration_progress.go` and `internal/ops/integration_progress_test.go`. Implement a pure decision API over Task 1-3 interfaces; do not write state, create tasks, read prompts, or mutate Git.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#required-properties`

Dependencies: existing planning contracts `code-planning-main-1-replan-1`, `code-planning-main-1-code-planning-1`, and `code-planning-main-1-code-planning-2`. The generated coding task consumes their declared interfaces; there is no sibling output dependency in this one-task plan.

Validation: `go test -json ./internal/ops -run '^TestEvaluateIntegrationProgress$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestEvaluateIntegrationProgress") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/integration_progress.go, internal/ops/integration_progress_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[]`; `read_only_task_depends_on=[code-planning-main-1-replan-1, code-planning-main-1-code-planning-1, code-planning-main-1-code-planning-2]`; `interfaces_owned=[EvaluateIntegrationProgress, IntegrationProgressDecision, deterministic slice and global analysis keys]`; `interfaces_consumed=[IntegrationLifecycle persistence schema, NormalizeGlobalIntegrationGenerationLimit, SlicedIntegrationCapability]`; coverage: one pure decision closes classification drift across coverage, readiness, exhaustion, and effective completion.

## Architecture review

The stable boundary is durable state and existing pipeline/config interfaces; the volatile policy is how those facts combine into readiness. A pure function is appropriate because later reconciliation, mutation, wake, status, and progression paths all need the same answer while retaining separate mutation authority.

Failure handling is fail-closed: corrupt graph/evidence returns an error, expected blocked or waiting workflow states remain explicit decision values, and no branch can manufacture durable success. Concurrency is observational here; linearization belongs to the caller that supplies HEAD and to later mutation/finalization tasks. The implementation is reversible and localized to two files, but downstream fan-out makes deterministic ordering and exhaustive branch tests load-bearing.

No new abstraction beyond the assigned `EvaluateIntegrationProgress` / `IntegrationProgressDecision` interface is justified. No architecture issue is introduced by the plan; documentation and end-to-end concerns remain with the already-owned master tasks.

## Spec Compliance Matrix

This matrix covers the requirements allocated to the authoritative progress-decision slice. Persistence, reconciliation writes, mutation locking, prompts, completion consumers, end-to-end tests, and documentation remain dependency-ordered sibling responsibilities.

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Partial planning handoff does not open integration coverage. | Required Property 1; Success Criterion 1 | Task 1 | Covered |
| 2 | Settlement waits for every planning source, eligible output/transition, and resulting coding lineage. | Slice Integration; Required Property 2; Success Criterion 1 | Task 1 | Covered |
| 3 | The contributing plan set freezes exactly once and remains reproducible. | Slice Integration; Required Property 2; Success Criterion 2 | Task 1 | Covered |
| 4 | Fewer than two contributing scopes produce no slice analyses. | Required Property 3; Success Criterion 2 | Task 1 | Covered |
| 5 | Multiple contributing scopes each receive one bounded coverage route. | Required Property 4; Success Criterion 3 | Task 1 | Covered |
| 6 | One-lineage scopes reuse approval attestations and produce no slice. | Required Property 5; Success Criterion 3 | Task 1 | Covered |
| 7 | Multi-lineage scopes with merged work produce exactly one deterministic slice key. | Required Property 6; Success Criterion 3 | Task 1 | Covered |
| 8 | Integration-escalation plans remain repair lineage outside the frozen cohort. | Required Property 7; Global Integration | Task 1 | Covered |
| 9 | Coding, fix, replacement, and escalation ancestry is attributed to the correct originating scope or finding. | Required Property 8; Slice Integration | Task 1 | Covered |
| 10 | Slice evidence is accepted only for its bounded plan/roots/key/snapshot and never implies global completion. | Required Properties 9-10; Slice Integration | Task 1 | Covered |
| 11 | Slice findings resolve through merged repair or recursive replacement lineage; blocked or abandoned unresolved findings block. | Slice Integration; Required Property 14 | Task 1 | Covered |
| 12 | Global readiness waits for settled planning, terminal coding/repair work, complete coverage, and resolved slices. | Required Property 11; Success Criterion 4 | Task 1 | Covered |
| 13 | Missing or blocked slice work prevents global readiness. | Required Property 11; Global Integration | Task 1 | Covered |
| 14 | Global findings require another generation only after repair/replacement resolution. | Final Closure; Required Property 13 | Task 1 | Covered |
| 15 | Later HEAD mutation invalidates stale clean evidence and requests another generation while budget remains. | Final Closure; Required Properties 13, 15-17; Success Criteria 7, 9 | Task 1 | Covered |
| 16 | Generation exhaustion produces an explicit blocked/exhausted outcome. | Final Closure; Required Property 14; Success Criterion 9 | Task 1 | Covered |
| 17 | Generation normalization uses the configurable limit and deterministic default. | Required Property 19; Final Closure | Task 1 | Covered |
| 18 | Repeated wake/restart evaluation cannot create duplicate slice or global identities. | Required Property 20; Success Criterion 10 | Task 1 | Covered |
| 19 | Frozen pipelines fail closed through `SlicedIntegrationCapability` when required topology is absent. | Required Property 21; Slice Integration | Task 1 | Covered |
| 20 | The decision remains stack-agnostic and preserves review, merge, persistence, and Git-mutation ownership boundaries. | Required Property 21; Out of Scope | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: master Task 10 (`code-planning-main-1-code-planning-9`) owns public-operation lifecycle and controlled-race evidence; this task supplies focused pure-decision tests. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: master Task 11 (`code-planning-main-1-code-planning-10`) owns documentation after implementation and end-to-end evidence. | N/A |

## Pre-submit audit

- Atomicity: one coding task implements one pure decision and its colocated tests.
- Ownership: only the two assigned `internal/ops` files are writable; persistence, reconciliation, prompts, completion consumers, Git mutation, E2E, and docs remain out of scope.
- Interfaces: the plan consumes only the three assigned interfaces and owns exactly the assigned evaluator, decision, and deterministic keys.
- Determinism: all emitted collections are sorted, keys exclude time/map order, and repeated/permuted evaluation is tested.
- Shared files: one output entry means no intra-plan collision; the external dependencies are explicit and acyclic.
- Validation: the JSON-event predicate requires the named top-level test and rejects every failing Go event.
- Compliance: every progress-decision requirement allocated by the master plan is covered; E2E and docs retain their first-class sibling owners.
