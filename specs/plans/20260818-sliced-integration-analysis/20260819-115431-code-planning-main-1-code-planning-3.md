# Code Plan: Authoritative Integration Progress Decision

## Intent and evidence

Define one pure, deterministic decision boundary for integration coverage, slice eligibility, repair resolution, global-generation readiness, exhaustion, and effective completion. The function reads durable state plus the already-owned pipeline capability, normalized generation budget, and live integration HEAD; it returns facts and requested analysis identities without writing state, creating tasks, reading prompts, or mutating Git.

Success means `TestEvaluateIntegrationProgress` proves every assigned branch of the decision and repeated evaluation of the same state returns equivalent ordered keys and decisions. A clean lifecycle projection is effective only when its immutable source commit equals the supplied live HEAD and every barrier remains satisfied.

Based on:

- `specs/goals/20260818-sliced-integration-analysis.md`, read in full, including every Required Property, Success Criterion, constraint, and documentation obligation.
- The retained Task 4 contract and complete goal-level ownership matrix in `specs/plans/20260818-sliced-integration-analysis/20260819-005612-code-planning-main-1.md`.
- The corrected replacement ownership and dependency graph in `specs/plans/20260818-sliced-integration-analysis/20260819-084641-code-planning-main-1-replan-1.md`.
- Live task state for `code-planning-main-1-replan-1-code-planning-0-coding-0`: it is the concrete coding task that implements `IntegrationLifecycle persistence schema`, is currently `IMPLEMENTING_CODE`, and already depends on the merged generation-limit and sliced-pipeline implementation tasks.
- `INVARIANTS.md` §§3.3-3.4: a coding task is not claimable until every direct dependency is `MERGED`, and same-role-pair concrete dependencies are permitted while downstream dependency edges are forbidden.
- Direct source reads of `NormalizeGlobalIntegrationGenerationLimit` in `internal/models/config.go` and `SlicedIntegrationCapability` in `internal/pipeline/config.go` / `resolver.go`.
- ADR-0112's lock order and prohibition on blackboard writes while the integration mutation lock is held, plus the relevant open architecture issues and repository-layer orientation from Stacklit.

Load-bearing claims checked against external state:

- `internal/models/integration.go` is absent at this task worktree's current HEAD, so a planning-task dependency is not sufficient to make the persistence interface available.
- The generated coding task will directly depend on `code-planning-main-1-replan-1-code-planning-0-coding-0`; therefore INVARIANTS §3.3 prevents it from becoming claimable before the concrete persistence implementation merges.
- That provider task already depends on the four merged configuration and pipeline implementation tasks, so the one concrete dependency preserves the original Task 1-3 ordering without adding redundant edges.
- Cross-task prompt, mutation, reconciliation, progression, consumer, controlled-race, and documentation requirements remain owned by their explicit sibling planning tasks; this pure evaluator is credited only for decision-policy portions.

Doc Impact: none in this task. `code-planning-main-1-code-planning-10` owns the ADR, architecture, protocol, configuration, operator, invariant, and issue-registry updates after implementation and end-to-end evidence exist.

Test Impact: add `internal/ops/integration_progress_test.go` with the required top-level test and deterministic subtests. `code-planning-main-1-code-planning-9` owns cross-component lifecycle and controlled-concurrency validation.

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
        later reconciliation/gates consume; this task never mutates
```

`internal/models` remains the sole durable-evidence owner, `internal/pipeline` remains the capability owner, and later reconciliation remains the sole lifecycle/task-creation writer. `internal/ops/integration_progress.go` owns only policy derivation. This keeps wake, reconciliation, completion, and mutation consumers from reimplementing divergent predicates while preserving their separate mutation authority.

The evaluator should expose a small input API equivalent to:

```go
func EvaluateIntegrationProgress(
    state *models.State,
    capability pipeline.SlicedIntegrationCapability,
    integrationHEAD string,
) (IntegrationProgressDecision, error)
```

The exact field spelling may follow package conventions, but `IntegrationProgressDecision` must carry these semantic outputs without side effects:

- the existing frozen cohort or one canonical cohort candidate and whether it must be frozen;
- effective per-scope coverage plus missing slice requests, each with originating plan, sorted root lineage IDs, and deterministic key;
- whether all local barriers permit global analysis, and the next global generation, key, and source commit when required;
- whether integration is effectively complete for the supplied HEAD;
- a machine-readable blocked or exhausted reason with stable diagnostic context.

Malformed references, ancestry cycles, impossible tagged evidence, or contradictory durable facts return an error and fail closed. Expected workflow states such as unsettled planning, waiting repairs, pipeline upgrade required, blocked findings, and exhausted generations remain explicit decision values.

## Deterministic evaluation contract

### 1. Build one canonical graph view

Index tasks by ID once. Derive plan ancestry, root coding lineages, analysis descendants, repair descendants, and escalation descendants from task provenance. Sort task IDs before emitting any slice, coverage, or diagnostic collection; never depend on state slice order or Go map iteration.

Follow `SupersededBy` recursively. A superseded node resolves only when every referenced replacement branch exists and reaches merged work. Pending replacement waits; missing or cyclic replacement fails closed; blocked or abandoned leaves without completed replacement work block.

### 2. Settle and freeze the contributing cohort exactly once

If a frozen contributing set exists, reuse it unchanged. Otherwise emit no freeze candidate until every pre-integration planning source is terminal, every eligible coding-producing output and transition is consumed, and all resulting coding work is terminal through replacement resolution. Partial planning handoff never satisfies this boundary.

At the first settled evaluation, include only pre-integration code-planning scopes with merged root coding work. Exclude planning tasks descended from integration analyses or their repair/escalation descendants. Canonically sort scopes and roots. Repeated evaluation before persistence returns the same candidate; persisted evidence becomes authoritative.

### 3. Classify bounded local coverage

- Fewer than two contributing scopes request no slices.
- With at least two scopes, each one-root scope projects its coding-review approval attestation and requests no slice.
- Each scope with at least two merged root lineages requests exactly one `slice:<originating-plan-task-id>` identity.
- Missing sliced-pipeline capability blocks only when a slice is required, carrying the capability code and guidance.
- Persisted slice evidence is effective only for the matching key, plan, roots, analysis metadata, and immutable snapshot. Clean slice evidence never implies global completion.

### 4. Resolve findings and gate global analysis

Repairs resolve only through merged work or recursively merged replacement leaves. Blocked or abandoned findings without completed replacements block. Pending repair or replacement work waits without requesting another analysis.

Global analysis is ready only when planning is settled, the cohort is frozen or has its canonical candidate, all planned coding and integration-repair work is terminal, every required scope has coverage, every created slice is resolved and unblocked, and prior global findings are resolved. Escalation plans stay repair ancestry and never join the cohort.

### 5. Derive global generations and effective completion

Normalize the configured limit with `models.NormalizeGlobalIntegrationGenerationLimit`. Keys are `global:<positive-generation>` and the requested generation's immutable source commit is the supplied non-empty integration HEAD.

Clean global evidence is effective only when generation, key, and source commit agree and source commit equals live HEAD. Stale evidence is ineffective immediately and requests another generation only while barriers and budget allow. Reaching the normalized limit without current-HEAD clean evidence returns an explicit exhausted/blocked decision and no request.

### 6. Preserve purity

Do not mutate supplied state, append evidence, set task metadata, create tasks, read pipeline files or prompts, query Git, or depend on time/randomness. Tests deep-compare representative input state before and after evaluation.

## Test design

Add `TestEvaluateIntegrationProgress` as one top-level test with named deterministic subtests and compact fixtures in the owned test file. Assert complete decisions, not only booleans or type shapes.

Required proof pairs:

1. Partial handoff remains unsettled until every planning source, eligible output/transition, and resulting coding lineage settles; only then does one canonical freeze candidate appear.
2. Re-evaluation before and after persistence proves the cohort freezes once and late integration escalation cannot alter it.
3. Zero/one contributing scope yields no slice; two scopes activate bounded coverage.
4. One-lineage scope yields an approval attestation and no slice; multi-lineage scope yields exactly `slice:<plan-id>` once.
5. Missing capability blocks when a slice is required but not on the fewer-than-two-scope bypass.
6. Escalation descendants remain repair lineage and cannot become contributing scopes or slice requests.
7. Multi-level and branched supersession resolves only when every leaf merges; pending waits; missing/cyclic/blocked/abandoned outcomes fail closed or block as appropriate.
8. Missing or unresolved slice, blocked slice finding, active coding/repair work, and unresolved global finding each prevent readiness; satisfying every barrier yields one global request.
9. Repeated evaluation and task-order permutation return the same slice/global keys.
10. Current-HEAD clean evidence completes; the same evidence against another HEAD is ineffective and requests a later generation while budget remains.
11. Non-positive configuration normalizes to the default, positive configuration is honored, and limit exhaustion blocks without another request.
12. Representative evaluations leave input state deeply unchanged.

The canonical validation must fail if the named top-level test is absent and must reject every Go failure event:

`go test -json ./internal/ops -run '^TestEvaluateIntegrationProgress$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestEvaluateIntegrationProgress") and all(.[]; .Action != "fail")'`

## Planned coding tasks

### Task 1 — Compute authoritative integration progress

Description: Compute a single deterministic integration progress decision from state, pipeline capability, generation budget, and integration HEAD.

Done when: `TestEvaluateIntegrationProgress` proves partial handoff cannot settle coverage, the cohort freezes exactly once, fewer than two scopes create no slices, one-lineage scopes produce attestations, multi-lineage scopes produce one slice key, escalation plans stay repair lineage, replacements resolve recursively, blocked or abandoned findings block, global readiness waits for all barriers, stale clean evidence is ineffective, and exhausted generations block.

Scope: Own `internal/ops/integration_progress.go` and `internal/ops/integration_progress_test.go`. Implement a pure decision API over Task 1-3 interfaces; do not write state, create tasks, read prompts, or mutate Git.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#required-properties`

Dependency: existing concrete task `code-planning-main-1-replan-1-code-planning-0-coding-0`. This is the actual persistence implementation, not its merged planning contract. The generated coding task cannot become claimable until this provider is `MERGED`; its transitive merged dependencies supply `NormalizeGlobalIntegrationGenerationLimit` and `SlicedIntegrationCapability`.

Validation: `go test -json ./internal/ops -run '^TestEvaluateIntegrationProgress$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestEvaluateIntegrationProgress") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/integration_progress.go, internal/ops/integration_progress_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[]`; `read_only_task_depends_on=[code-planning-main-1-replan-1-code-planning-0-coding-0]`; `interfaces_owned=[EvaluateIntegrationProgress, IntegrationProgressDecision, deterministic slice and global analysis keys]`; `interfaces_consumed=[IntegrationLifecycle persistence schema, NormalizeGlobalIntegrationGenerationLimit, SlicedIntegrationCapability]`; coverage: one pure decision closes classification drift across coverage, readiness, exhaustion, and effective completion.

## Architecture review

### Discovery

The stable inputs are durable state, normalized configuration, frozen-pipeline capability, and externally observed integration HEAD. The volatile concern is the policy combining those facts into coverage, readiness, generation, and completion. Persistence and validation live in `internal/models` / `internal/statevalidate`; topology in `internal/pipeline`; pure policy in `internal/ops`; mutation, reconciliation, prompts, completion consumers, E2E, and docs retain distinct owners.

The main dependency risk is claimability before a consumed interface exists. It is closed by a direct same-role-pair dependency on the concrete persistence coding task rather than a planning artifact. The main coverage risk is misattributing cross-component correctness to a pure function; the matrix below maps immutable prompt surfaces, aggregate inspection, mutation invalidation, race ordering, ADR-0112, reconciliation, consumers, and docs to their real owners.

### Analysis and recommendation

A pure policy function is appropriate because several later consumers need the same decision while retaining distinct write authority. Corrupt durable evidence returns errors; expected waiting, blocked, upgrade, and exhaustion outcomes remain typed decisions. Concurrency is observed through supplied HEAD and durable evidence, but linearization and mutation-side invalidation remain outside this task.

The change is reversible and localized to two files. Do not introduce an additional service, storage abstraction, or prompt dependency. No new architecture issue is introduced by this plan; existing integration-closure risk is addressed across the already-owned task graph and may be revised or resolved only by the documentation owner after E2E evidence exists.

## Spec Compliance Matrix

External task IDs name current goal-level owners. `Task 1` is this plan's only output. The matrix deliberately separates policy derivation from persistence, task creation, prompt rendering, mutation linearization, completion consumers, controlled-race proof, and documentation.

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Partial planning handoff does not open integration coverage. | Required Property, lines 206-210; Success Criterion 1 | Task 1; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9` | Covered |
| 2 | The contributing plan set is evaluated once only after planning sources, eligible outputs/transitions, and resulting coding work settle. | Required Property, lines 207-210 | Task 1; `code-planning-main-1-replan-1-code-planning-0`; `code-planning-main-1-replan-1-code-planning-2` | Covered |
| 3 | Fewer than two contributing scopes produce no slices. | Required Property, line 211; Success Criterion 2 | Task 1; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9` | Covered |
| 4 | Multiple contributing scopes each contribute a bounded local coverage record. | Required Property, lines 212-213; Success Criterion 3 | Task 1; `code-planning-main-1-replan-1-code-planning-0`; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-6` | Covered |
| 5 | One-lineage scopes reuse coding-review approval attestations and produce no slice. | Required Property, lines 214-215 | Task 1; `code-planning-main-1-replan-1-code-planning-0`; `code-planning-main-1-replan-1-code-planning-2` | Covered |
| 6 | Multi-lineage scopes with merged work produce exactly one slice analysis. | Required Property, lines 216-217 | Task 1; `code-planning-main-1-code-planning-2`; `code-planning-main-1-replan-1-code-planning-2` | Covered |
| 7 | Integration-escalation plans remain repair work outside the contributing set and create no slices. | Required Property, lines 218-219 | Task 1; `code-planning-main-1-replan-1-code-planning-0`; `code-planning-main-1-replan-1-code-planning-2` | Covered |
| 8 | Task lineage identifies coding and fix tasks belonging to each slice. | Required Property, line 220 | Task 1; `code-planning-main-1-replan-1-code-planning-0`; `code-planning-main-1-replan-1-code-planning-2` | Covered |
| 9 | Each slice receives a bounded review surface attributable to its originating plan. | Required Property, lines 221-222; Success Criterion 5 | `code-planning-main-1-code-planning-6`; `code-planning-main-1-replan-1-code-planning-0`; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9` | Covered |
| 10 | Each slice verdict records descendant changes and the immutable source snapshot analyzed. | Required Property, lines 223-224; Success Criterion 5 | `code-planning-main-1-replan-1-code-planning-0`; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-6`; `code-planning-main-1-code-planning-9` | Covered |
| 11 | Global analysis waits for terminal coding/repair work, settled planning, complete required coverage, and resolved slices; missing/unresolved work blocks. | Required Property, lines 225-228; Success Criterion 4 | Task 1; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-7`; `code-planning-main-1-code-planning-8`; `code-planning-main-1-code-planning-9` | Covered |
| 12 | Global analysis independently inspects the aggregate branch. | Required Property, line 229; Success Criterion 6 | `code-planning-main-1-code-planning-6`; `code-planning-main-1-code-planning-9` | Covered |
| 13 | Global fixes and later integration-HEAD mutations trigger another scan while budget remains. | Required Property, lines 230-231 | Task 1; `code-planning-main-1-replan-1-code-planning-1`; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9` | Covered |
| 14 | Slice exhaustion and global-generation exhaustion produce explicit blocked outcomes. | Required Property, lines 232-233 | Task 1; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9` | Covered |
| 15 | Clean completion is tied to an immutable reviewed commit. | Required Property, line 234 | Task 1; `code-planning-main-1-replan-1-code-planning-0`; `code-planning-main-1-replan-1-code-planning-1`; `code-planning-main-1-code-planning-7`; `code-planning-main-1-code-planning-8` | Covered |
| 16 | Completion state, clean reviewed commit, and integration HEAD remain linearizable under concurrent mutation. | Required Property, lines 235-237; Success Criteria 7-8 | `code-planning-main-1-replan-1-code-planning-1`; `code-planning-main-1-code-planning-7`; `code-planning-main-1-code-planning-8`; `code-planning-main-1-code-planning-9` | Covered |
| 17 | The integration-HEAD mutation path owns invalidation of completion tied to a superseded HEAD. | Required Property, lines 238-239 | `code-planning-main-1-replan-1-code-planning-1`; `code-planning-main-1-code-planning-9` | Covered |
| 18 | Finalization preserves ADR-0112 lock ordering and performs no blackboard write under the mutation lock. | Required Property, line 240; Final Closure, lines 175-188 | `code-planning-main-1-replan-1-code-planning-1`; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9`; `code-planning-main-1-code-planning-10` | Covered |
| 19 | The global generation limit is configurable with a deterministic default. | Required Property, line 241 | Task 1; `code-planning-main-1-code-planning-1`; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9` | Covered |
| 20 | Wake evaluation and restart recovery create no duplicate slice or global analyses. | Required Property, lines 242-243; Success Criterion 10 | Task 1; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-8`; `code-planning-main-1-code-planning-9` | Covered |
| 21 | Workflow remains stack-agnostic and preserves review and merge authorization boundaries. | Required Property, lines 244-245; Out of Scope | `code-planning-main-1-code-planning-1`; `code-planning-main-1-code-planning-2`; `code-planning-main-1-replan-1-code-planning-1`; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9` | Covered |
| 22 | No coverage begins while a planning source, output, transition, or resulting coding task remains unsettled. | Success Criterion 1, lines 251-253 | Task 1; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9` | Covered |
| 23 | Cohort classification, zero-slice bypass, attestations, and one-slice classification are reproducible. | Success Criteria 2-3, lines 254-259 | Task 1; `code-planning-main-1-replan-1-code-planning-0`; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9` | Covered |
| 24 | No global analysis becomes claimable while any local barrier is unsettled, missing, unresolved, or blocked. | Success Criterion 4, lines 260-262 | Task 1; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-8`; `code-planning-main-1-code-planning-9` | Covered |
| 25 | Every slice analysis records bounded context and an immutable snapshot. | Success Criterion 5, line 263 | `code-planning-main-1-replan-1-code-planning-0`; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-6`; `code-planning-main-1-code-planning-9` | Covered |
| 26 | Global analysis independently reviews the aggregate after local coverage and slice resolution. | Success Criterion 6, lines 264-266 | `code-planning-main-1-code-planning-6`; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9` | Covered |
| 27 | Successful integration linearizes only when clean reviewed commit equals integration HEAD. | Success Criterion 7, lines 267-269 | Task 1; `code-planning-main-1-replan-1-code-planning-1`; `code-planning-main-1-code-planning-7`; `code-planning-main-1-code-planning-8`; `code-planning-main-1-code-planning-9` | Covered |
| 28 | Controlled concurrency proves both mutation/finalization orders never leave stale durable success. | Success Criterion 8, lines 270-273 | `code-planning-main-1-replan-1-code-planning-1`; `code-planning-main-1-code-planning-9` | Covered |
| 29 | Later mutations reanalyze within budget and block after exhaustion. | Success Criterion 9, lines 274-275 | Task 1; `code-planning-main-1-code-planning-1`; `code-planning-main-1-replan-1-code-planning-1`; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9` | Covered |
| 30 | Repeated wake and restart recovery remain duplicate-free. | Success Criterion 10, lines 276-277 | Task 1; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-8`; `code-planning-main-1-code-planning-9` | Covered |
| 31 | Slice findings continue through existing integration-reviewer and coding-pair fix lifecycle. | Slice Integration | `code-planning-main-1-code-planning-2`; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9` | Covered |
| 32 | Completed slice evidence remains slice-local; later sibling changes are assessed globally rather than reopening the slice. | Slice Integration | Task 1; `code-planning-main-1-replan-1-code-planning-0`; `code-planning-main-1-code-planning-9` | Covered |
| 33 | Global repairs promoted to code planning remain repair lineage visible to the next global analysis. | Global Integration | Task 1; `code-planning-main-1-replan-1-code-planning-0`; `code-planning-main-1-replan-1-code-planning-2`; `code-planning-main-1-code-planning-9` | Covered |
| 34 | The topology retains global integration and introduces no new agent role. | Out of Scope, lines 290-294 | `code-planning-main-1-code-planning-2` | Covered |
| 35 | No stack-specific validation command is introduced. | Out of Scope, line 293 | Task 1; `code-planning-main-1-code-planning-1`; `code-planning-main-1-code-planning-9` | Covered |
| 36 | ADR-0113 extends ADR-0055 and supersedes the no-rescan limitation. | Documentation Impact, lines 300-301 | `code-planning-main-1-code-planning-10` | Covered |
| 37 | State-machine and task-lifecycle documentation describes the new lifecycle. | Documentation Impact, lines 302-303 | `code-planning-main-1-code-planning-10` | Covered |
| 38 | Pipeline and operational documentation covers barriers, generations, and terminal outcomes. | Documentation Impact, lines 304-305 | `code-planning-main-1-code-planning-10` | Covered |
| 39 | The integration-closure architecture issue changes only after implementation and validation evidence exists. | Documentation Impact, lines 306-308 | `code-planning-main-1-code-planning-9`; `code-planning-main-1-code-planning-10` | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | `code-planning-main-1-code-planning-9` | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | `code-planning-main-1-code-planning-10` | Covered |

## Pre-submit audit

- Atomicity: one coding task implements one pure decision and its colocated tests.
- Provider ordering: output `task_depends_on` names the concrete persistence coding task, not a planning task; claimability waits for its merge.
- Ownership: only the two assigned `internal/ops` files are writable downstream; persistence, reconciliation, prompts, progression, consumers, Git mutation, E2E, and docs remain external.
- Determinism: emitted collections are sorted, keys exclude time and map order, and repeated/permuted evaluation is tested.
- Shared files: one output entry means no intra-plan collision.
- Validation: the JSON-event predicate requires the named top-level test and rejects every failing Go event.
- Compliance: every Required Property and Success Criterion is mapped to its actual current task owner; prompt, aggregate-branch, invalidation, race, and ADR-0112 obligations are not credited to the pure evaluator.
