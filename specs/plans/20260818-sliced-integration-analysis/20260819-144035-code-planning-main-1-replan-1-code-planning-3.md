# Code Plan: Reconcile Integration Coverage Edge States

## Intent and evidence

Repair the shared lifecycle contract at its durable boundary, then re-emit the pure integration progress evaluator against that corrected contract. A settled run with no contributing scopes must remain distinguishable from an unsettled run after persistence, and one original coding root with multiple merged replacement leaves must remain one lineage while retaining approval evidence for every leaf that contributed merged work.

Success means `TestIntegrationLifecycleYAMLRoundTrip` and `TestIntegrationLifecycleValidation` prove the durable schema accepts and preserves a non-nil settled zero-scope cohort plus a bounded multi-attestation coverage record, and `TestEvaluateIntegrationProgress` proves the evaluator emits those representations deterministically without creating extra lineages or omitting merged replacement leaves.

Based on:

- The full goal specification at `specs/goals/20260818-sliced-integration-analysis.md`, especially Slice Integration, Required Properties, Success Criteria, and Out of Scope.
- The merged persistence contract at `specs/plans/20260818-sliced-integration-analysis/20260819-111435-code-planning-main-1-replan-1-code-planning-0.md` and its implementation in `internal/models/integration.go` plus `internal/statevalidate/integration.go`.
- The merged pure-evaluator plan at `specs/plans/20260818-sliced-integration-analysis/20260819-115431-code-planning-main-1-code-planning-3.md` and the superseded implementation on `task/code-planning-main-1-code-planning-3-coding-0`.
- The reviewer reframe recorded on `code-planning-main-1-code-planning-3-coding-0`: the current validator rejects a non-nil empty cohort, and the evaluator rejects a one-root lineage when recursive replacement resolution yields more than one merged leaf.
- ADR-0055, ADR-0059, ADR-0067, and ADR-0112; `INVARIANTS.md` §§3, 5, 6, 7, and the Protection Matrix; and the Update Policy plus integration-closure, cross-pair, single-goal, and decomposition-cascade entries in `specs/architecture/architectural-issues.md`.

Load-bearing claims:

- **EVIDENCED — settlement identity:** `IntegrationLifecycle.ContributingSet == nil` is the existing not-yet-frozen state, while `validateContributingSet` currently rejects `&IntegrationContributingSet{Scopes: []}`. A non-nil empty set is therefore the minimum durable marker that distinguishes settled zero scope from unsettled planning.
- **EVIDENCED — lineage identity:** the goal spec defines distinct lineages by different root coding tasks and includes replacements inside the root lineage. Multiple merged replacement leaves below one root are not multiple lineages.
- **EVIDENCED — evidence completeness:** every merged leaf has its own reviewed task, acceptance criteria, reviewed commit, approver, validation, and merge commit. Selecting one leaf would discard evidence for other merged work; the bounded coverage payload must therefore carry a deterministic set of per-task attestations.
- **EVIDENCED — ownership:** `internal/models` owns the representation, `internal/statevalidate` owns structural and immutable-transition checks, and `internal/ops` owns pure lineage and progress policy. Reconciliation, verdict projection, prompts, integration-ref mutation, wake presentation, and completion consumers remain outside this plan.

Doc Impact: only this plan and its structured task output. Product and architecture documentation remains owned by `code-planning-main-1-code-planning-10` after implementation and end-to-end evidence exist.

Test Impact: Task 1 extends the existing named model/state-validation tests; Task 2 re-emits and extends the existing named pure-evaluator test. TDD is colocated with each behavior change.

## Current task routing

Only Task 1 and Task 2 are outputs of this plan. External aliases identify existing sibling ownership and do not add work to this scope.

| Alias | Concrete task ID | Responsibility |
|---|---|---|
| Task 1 / `PERSIST-PATCH` | this plan output 0 | Settled zero-scope representation and bounded plural approval-attestation persistence/validation |
| Task 2 / `PROGRESS` | this plan output 1 | Pure progress evaluation over the corrected schema |
| `CFG` | `code-planning-main-1-code-planning-1` | Configurable global-generation limit and normalization |
| `TOPO` | `code-planning-main-1-code-planning-2` | Slice/global role-pair topology and frozen-pipeline capability |
| `PERSIST-BASE` | `code-planning-main-1-replan-1-code-planning-0-coding-0` | Existing lifecycle schema and invariant baseline |
| `MUTATE` | `code-planning-main-1-replan-1-code-planning-1` | Integration-ref mutation receipts and HEAD invalidation |
| `RECONCILE` | `code-planning-main-1-replan-1-code-planning-2` | Analysis verdict projection and task materialization |
| `CONTEXT` | `code-planning-main-1-code-planning-6` | Bounded slice and independent aggregate global context |
| `GATE` | `code-planning-main-1-code-planning-7` | State-changing effective-completion barriers |
| `CONSUMERS` | `code-planning-main-1-code-planning-8` | Wake, supervisor, status, and terminal consumers |
| `E2E` | `code-planning-main-1-code-planning-9` | End-to-end lifecycle, restart, exhaustion, and race evidence |
| `DOC` | `code-planning-main-1-code-planning-10` | ADR, lifecycle/operator docs, and architecture-issue disposition |

## Architecture and ownership boundary

```text
planning boundary                         one original coding root
       |                                            |
       v                                            v
nil = unsettled                         replacement DAG (one lineage)
non-nil empty = settled zero scope      merged leaf A   merged leaf B
       |                                      |              |
       +---------- IntegrationLifecycle ------+--------------+
                              |
                              v
                 ValidateState + immutable transition
                              |
                              v
                 EvaluateIntegrationProgress (pure)
                              |
             canonical cohort / coverage / requests / closure facts
```

The representation change is a prerequisite, not a workaround inside the evaluator. Task 1 changes the tagged approval payload from one optional attestation to a bounded list of `IntegrationApprovalAttestation` values and permits a non-nil contributing set with zero scopes. Task 2 consumes that interface and emits canonical evidence. This preserves dependency direction: operational policy depends on validated models; models do not depend on task-graph policy.

The change is reversible and localized. No new service, generic event abstraction, role, task state machine, or mutation owner is introduced.

## Durable representation contract

Task 1 updates `IntegrationCoverageRecord` so approval coverage contains `ApprovalAttestations []IntegrationApprovalAttestation`. The existing coverage discriminator remains `approval_attestation`; it identifies the evidence family, while the payload contains one attestation per merged terminal leaf in the single root lineage.

Candidate validation must enforce:

1. `ContributingSet == nil` remains valid and means the settled boundary has not been frozen.
2. A non-nil contributing set with zero scopes is valid and is the durable settled zero-contributing-scope marker.
3. Every non-empty scope still has a non-empty plan ID and at least one unique root, and roots remain exclusive to one scope.
4. Approval coverage has a non-empty attestation list and no slice report; slice coverage has one slice report and no approval attestations.
5. Every attestation retains all six required evidence facts, and reviewed task IDs are non-empty and unique within the coverage record.
6. Coverage for an empty cohort remains invalid because no contributing plan can own it.
7. The transition validator preserves both empty and non-empty frozen cohorts immutably and keeps coverage append-only.

`TestIntegrationLifecycleYAMLRoundTrip` must include a non-nil empty cohort case and a one-root coverage case with at least two distinct attestations. `TestIntegrationLifecycleValidation` must exercise the valid zero-scope marker through public `ValidateState`, reject empty, duplicate, mixed, or malformed attestation payloads, and prove an empty frozen cohort cannot be cleared or replaced.

## Pure progress contract

Task 2 restores the previously reviewed pure evaluator and retains its settled-planning, cohort, slice, repair, global-generation, exhaustion, and effective-completion behavior. The corrective behavior is:

1. When pre-integration planning settles with no contributing plans, return `FreezeContributingSet=true` and a non-nil empty `IntegrationContributingSet`. Do not collapse it back to nil. Request no slices; global readiness continues through the ordinary barriers.
2. Count lineages by original root task IDs only. A replacement descendant never becomes another root, including when one superseded root branches to multiple replacements.
3. Resolve the complete replacement DAG. A one-root scope is settled only when every required replacement branch is settled according to the existing lineage rules.
4. For a one-root scope in a multi-scope cohort, collect every merged terminal leaf, sort by task ID, and project one `IntegrationApprovalAttestation` per leaf. Missing review, acceptance, validation, or merge facts fail closed.
5. Persisted approval coverage is effective only when its unique reviewed-task membership exactly equals the evaluator's canonical merged-leaf set. Missing, extra, duplicate, non-merged, or out-of-lineage attestations fail closed. Returned evidence order is deterministic.
6. Multiple attestations remain one `IntegrationScopeCoverage` entry and never request a slice, because the frozen scope still contains one root task ID.

The evaluator remains side-effect free: it does not mutate input state, persist evidence, create tasks, read prompts, query Git, or depend on time or map iteration.

### Required `TestEvaluateIntegrationProgress` proof

Retain the prior named test's existing branch coverage and add explicit cases that would fail the incompatible implementation:

- a settled state with no contributing plans emits a non-nil empty freeze candidate, no slice request, and deterministic global progress rather than an invalid or absent settlement marker;
- persisting that empty cohort and evaluating again reuses it without refreezing;
- one root superseded by two merged replacement leaves remains one frozen root, produces no slice, and emits exactly two attestations containing both leaf task IDs and their distinct approval facts;
- permuting replacement declaration, task order, or persisted attestation order produces the same canonical decision;
- persisted evidence missing either merged leaf, adding a non-leaf, duplicating a leaf, or referencing a non-merged/out-of-lineage task fails closed;
- all existing cases for partial handoff, one-scope bypass, mixed one-/multi-lineage coverage, capability blocking, escalation exclusion, recursive replacement resolution, repair blocking, global barriers, current/stale clean evidence, generation normalization/exhaustion, and input immutability remain covered.

## Dependency and change sequence

```text
Task 1: persistence representation + validation
                 |
                 v
Task 2: pure progress evaluator + deterministic lineage evidence
```

Task 2 depends on Task 1 because it compiles against and semantically consumes the plural attestation payload and the durable empty-cohort marker. The tasks share no writable files. Existing merged configuration and topology implementations remain read-only prerequisites already present on the integration branch.

Within each task, write the new failing subtests first, implement the minimum contract change, run pre-commit on touched files, then run the declared named validation command.

## Planned coding tasks

### Task 1 — Make all settled local-coverage states durable

Description: Make settled integration cohorts and bounded one-lineage approval evidence durably representable.

Done when: `TestIntegrationLifecycleYAMLRoundTrip` preserves a non-nil settled contributing set with zero scopes and a one-root coverage record containing attestations for multiple reviewed replacement leaves; `TestIntegrationLifecycleValidation` accepts the zero-scope marker, rejects empty, duplicate, mixed, or malformed approval-attestation sets, and preserves either frozen form immutably through transition validation.

Scope: Own `internal/models/integration.go`, `internal/models/integration_test.go`, `internal/statevalidate/integration.go`, and `internal/statevalidate/integration_test.go`. Update only the lifecycle representation, candidate validation, transition-validation tests, and YAML round-trip tests needed for settled zero scopes and plural one-lineage attestations; do not derive progress, create tasks, project verdicts, mutate Git, render prompts, or change unrelated lifecycle fields.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#required-properties`

Dependencies: none.

Validation: `go test -json ./internal/models ./internal/statevalidate -run '^(TestIntegrationLifecycleYAMLRoundTrip|TestIntegrationLifecycleValidation)$' -count=1`

### Task 2 — Re-emit authoritative integration progress

Description: Compute authoritative integration progress with complete bounded evidence for every merged leaf of one root lineage.

Done when: `TestEvaluateIntegrationProgress` proves a settled zero-contributing-scope boundary emits one persistable non-nil empty cohort, a single root with branched merged replacement leaves remains one lineage and emits one deterministic approval-attestation set containing every leaf, persisted sets are accepted only when their reviewed-task membership is exact, and all existing planning, slice, repair, global-generation, exhaustion, purity, and determinism cases remain covered.

Scope: Own `internal/ops/integration_progress.go` and `internal/ops/integration_progress_test.go`. Re-emit the pure decision API against Task 1's lifecycle schema, including deterministic replacement-leaf attestation sets; do not write state, create tasks, project verdicts, mutate Git, render prompts, gate sprint completion, or modify reconciliation consumers.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#required-properties`

Dependencies: Task 1.

Validation: `go test -json ./internal/ops -run '^TestEvaluateIntegrationProgress$' -count=1`

## Architecture assessment

### Discovery

The stable boundary is the goal-scoped lifecycle schema plus `ValidateState`; the volatile boundary is the policy that derives a frozen cohort and per-scope coverage from task provenance. The rejected implementation demonstrated two boundary mismatches: it produced a valid semantic zero-scope state that persistence rejected, and it compressed a replacement DAG into one evidence item even though multiple reviewed leaves contributed work.

No component is missing from the scoped walkthrough. Model serialization, candidate validation, transition immutability, replacement traversal, decision projection, and named unit tests are covered. Reconciliation and global consumers are intentionally excluded and consume the corrected interface in their existing scopes.

### Analysis and recommendation

| Question | Assessment |
|---|---|
| Problem | Two valid lifecycle states cannot cross the current persistence/policy boundary without loss or rejection. |
| Change vectors | Cohort cardinality and replacement DAG shape vary; ownership of durable facts and pure policy remains stable. |
| Cost of error | High: nil/empty collapse reopens a settled boundary after restart, while single-leaf selection hides merged reviewed work. |
| Failure behavior | Structural malformed payloads fail in `ValidateState`; contradictory lineage evidence fails in the evaluator. |
| Concurrency | Both tasks are pure or candidate-state validation only; ADR-0112 mutation linearization remains outside scope. |
| Data owner | Models own facts, statevalidate owns admissibility/immutability, ops owns derivation. |
| Boundary | Task 1 serializes facts; Task 2 consumes them without write authority. |
| Runtime constraints | Deterministic sorting and bounded task-graph traversal; no external I/O or stack-specific behavior. |

The minimum sound design is an explicit non-nil empty cohort plus a plural attestation payload. Treating empty as absent would lose settlement identity; choosing a canonical leaf would lose evidence; turning replacement leaves into roots would violate the lineage definition and incorrectly create slices. No new architectural issue is introduced.

## Spec Compliance Matrix

The matrix covers every functional requirement, constraint, and acceptance criterion in the goal spec. Task 1 and Task 2 receive credit only for the persistence and pure-progress portions assigned here; external aliases retain all other ownership.

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| O1 | Bounded local coverage precedes repeated global analysis until clean or explicitly blocked. | Objective | Task 1; Task 2; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| PM1 | A one-lineage approval attestation records reviewed task, criteria, reviewed commit, approver, validation, and merge commit. | Proposed Model, opening paragraphs | Task 1; Task 2; `RECONCILE`; `E2E` | Covered |
| SI1 | Contributing scopes are pre-integration code-planning tasks with merged root coding lineages, and distinct roots define distinct lineages. | Slice Integration, paragraphs 1-2 | Task 1; Task 2 | Covered |
| SI2 | Planning settles only after every coding-producing source, eligible output/transition, and resulting coding lineage settles. | Slice Integration, paragraphs 2-3 | Task 2; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| SI3 | The contributing set is evaluated exactly once at the settled boundary. | Slice Integration, paragraph 3 | Task 1; Task 2; `RECONCILE`; `E2E` | Covered |
| SI4 | Fewer than two contributing scopes bypass slice analyses. | Slice Integration, paragraph 4 | Task 1; Task 2; `RECONCILE`; `E2E` | Covered |
| SI5 | In a multi-scope cohort, one-root scopes contribute approval evidence and multi-root scopes receive exactly one slice. | Slice Integration, paragraph 4 | Task 1; Task 2; `TOPO`; `RECONCILE`; `E2E` | Covered |
| SI6 | Integration-escalation plans remain repair lineage outside the contributing set. | Slice Integration, paragraph 5 | Task 2; `RECONCILE`; `E2E` | Covered |
| SI7 | Slice context is bounded to its plan, descendants, commits, paths, metadata, and source snapshot. | Slice Integration, analyst-input list | `PERSIST-BASE`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| SI8 | Slice findings reuse the existing integration-reviewer and coding-pair repair lifecycle. | Slice Integration, paragraph after analyst responsibility | `TOPO`; `RECONCILE`; `E2E` | Covered |
| SI9 | Completed slices remain immutable and later sibling effects belong to global analysis. | Slice Integration, concurrency paragraph | `PERSIST-BASE`; Task 2; `CONTEXT`; `E2E` | Covered |
| SI10 | Findings resolve through merged fixes or complete replacement lineages; unresolved terminal work blocks. | Slice Integration, resolution paragraph | Task 2; `RECONCILE`; `E2E` | Covered |
| SI11 | Clean slice evidence is local and cannot imply goal completion. | Slice Integration, final paragraph | `PERSIST-BASE`; Task 2; `GATE`; `CONSUMERS` | Covered |
| GI1 | Global analysis waits for settled planning, terminal coding/repair work, complete required coverage, and resolved slices. | Global Integration, paragraph 1 | Task 2; `RECONCILE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| GI2 | Global analysis independently inspects the aggregate branch using bounded coverage as navigation, not proof. | Global Integration, paragraphs 2-3 | `CONTEXT`; `E2E` | Covered |
| GI3 | Promoted integration repairs remain repair lineage and are visible to the next global analysis. | Global Integration, final paragraph | Task 2; `RECONCILE`; `E2E` | Covered |
| FC1 | Global findings require another analysis after merged repair/replacement work, and unresolved findings block. | Final Closure, paragraph 1 | Task 2; `RECONCILE`; `E2E` | Covered |
| FC2 | Clean completion is bound to current integration HEAD with one linearizable completion/mutation order. | Final Closure, paragraphs 2-5 | Task 2; `MUTATE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| FC3 | The integration-ref mutation path owns invalidation and preserves ADR-0112 lock ordering. | Final Closure, paragraphs 4-5 | `MUTATE`; `E2E` | Covered |
| FC4 | Global scans are generation-bounded with deterministic configuration and explicit exhaustion. | Final Closure, paragraphs 7-8 | `CFG`; Task 2; `RECONCILE`; `E2E` | Covered |
| RP1 | Partial planning handoff does not open the coverage boundary. | Required Property 1 | Task 2; `E2E` | Covered |
| RP2 | The contributing set freezes once only after all planning/output/transition/coding prerequisites settle. | Required Property 2 | Task 1; Task 2; `RECONCILE`; `E2E` | Covered |
| RP3 | Fewer than two contributing scopes produce no slices. | Required Property 3 | Task 1; Task 2; `RECONCILE`; `E2E` | Covered |
| RP4 | Every scope in a multi-scope cohort contributes bounded local coverage. | Required Property 4 | Task 1; Task 2; `RECONCILE`; `E2E` | Covered |
| RP5 | A one-lineage scope reuses complete coding-review approval evidence and produces no slice. | Required Property 5 | Task 1; Task 2; `RECONCILE`; `E2E` | Covered |
| RP6 | A scope with at least two distinct merged root lineages produces exactly one slice. | Required Property 6 | Task 2; `TOPO`; `RECONCILE`; `E2E` | Covered |
| RP7 | Integration-escalation plans remain repair work and create no slices. | Required Property 7 | Task 2; `RECONCILE`; `E2E` | Covered |
| RP8 | Task lineage attributes coding, fix, and replacement tasks to their slice. | Required Property 8 | `PERSIST-BASE`; Task 2; `RECONCILE`; `E2E` | Covered |
| RP9 | Each slice receives a bounded surface attributable to its originating plan. | Required Property 9 | `PERSIST-BASE`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| RP10 | Each slice verdict records descendant changes and immutable source snapshot. | Required Property 10 | `PERSIST-BASE`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| RP11 | Missing, unresolved, or blocked local work prevents global analysis. | Required Property 11 | Task 2; `RECONCILE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| RP12 | Global analysis independently inspects the aggregate branch. | Required Property 12 | `CONTEXT`; `E2E` | Covered |
| RP13 | Global fixes and later integration-HEAD mutations trigger another scan within budget. | Required Property 13 | Task 2; `MUTATE`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| RP14 | Slice or global exhaustion produces an explicit blocked outcome. | Required Property 14 | Task 2; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| RP15 | Clean completion is tied to an immutable reviewed commit. | Required Property 15 | `PERSIST-BASE`; Task 2; `MUTATE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| RP16 | Completion, clean source, and integration HEAD remain linearizable under mutation. | Required Property 16 | Task 2; `MUTATE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| RP17 | Integration-HEAD mutation owns invalidation of superseded clean completion. | Required Property 17 | `MUTATE`; `E2E` | Covered |
| RP18 | Finalization preserves ADR-0112 lock ordering. | Required Property 18 | `MUTATE`; `RECONCILE`; `E2E` | Covered |
| RP19 | The generation limit is configurable with a deterministic default. | Required Property 19 | `CFG`; Task 2; `RECONCILE`; `E2E` | Covered |
| RP20 | Wake and restart recovery create no duplicate slice or global analyses. | Required Property 20 | `PERSIST-BASE`; Task 2; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| RP21 | The workflow stays stack-agnostic and preserves review/merge authorization. | Required Property 21 | `CFG`; `TOPO`; `MUTATE`; `RECONCILE`; `E2E` | Covered |
| SC1 | Coverage cannot begin while any planning/output/transition/coding prerequisite is unsettled. | Success Criterion 1 | Task 2; `RECONCILE`; `E2E` | Covered |
| SC2 | Cohort and attestation-vs-slice classification are reproducible, including zero-scope settlement. | Success Criterion 2 | Task 1; Task 2; `RECONCILE`; `E2E` | Covered |
| SC3 | Every multi-scope cohort member has complete bounded approval evidence or exactly one slice. | Success Criterion 3 | Task 1; Task 2; `RECONCILE`; `E2E` | Covered |
| SC4 | Global analysis is unclaimable behind any unsettled, missing, unresolved, or blocked local barrier. | Success Criterion 4 | Task 2; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| SC5 | Every slice records a bounded surface and immutable snapshot. | Success Criterion 5 | `PERSIST-BASE`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| SC6 | Global analysis independently reviews the aggregate after local resolution. | Success Criterion 6 | `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| SC7 | Successful integration linearizes only when clean source equals integration HEAD. | Success Criterion 7 | Task 2; `MUTATE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| SC8 | Controlled concurrency proves both mutation/finalization orders cannot leave stale success. | Success Criterion 8 | `MUTATE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| SC9 | Later mutations rescan within budget and block after exhaustion. | Success Criterion 9 | `CFG`; Task 2; `MUTATE`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| SC10 | Repeated wake and restart evaluation remains duplicate-free. | Success Criterion 10 | Task 2; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| C1 | Do not change master-planning responsibilities, coder review, global analysis existence, or stack-specific validation defaults; introduce no new roles. | Out of Scope | Task 1; Task 2; `TOPO` | Covered |
| DOC1 | Add an ADR extending ADR-0055 and superseding its no-rescan limitation. | Documentation Impact 1-2 | `DOC` | Covered |
| DOC2 | Update state-machine and task-lifecycle documentation. | Documentation Impact 3 | `DOC` | Covered |
| DOC3 | Update pipeline and operational documentation for barriers, generations, and outcomes. | Documentation Impact 4 | `DOC` | Covered |
| DOC4 | Change the integration-closure issue only after implementation and validation evidence exists. | Documentation Impact 5 | `E2E`; `DOC` | Covered |
| E2E | e2e test coverage for the new behavior | Cross-cutting | `E2E` (`code-planning-main-1-code-planning-9`) | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | `DOC` (`code-planning-main-1-code-planning-10`) | Covered |

## Pre-submit audit

- Atomicity: Task 1 has one persistence-contract intent; Task 2 has one pure-policy intent. Tests remain colocated with each behavior change.
- Dependency order: Task 2 depends on output 0 and cannot compile or become claimable against the old singular schema.
- Shared-file audit: the two tasks have disjoint writable files; no additional serialization edge is required.
- Evidence integrity: Task 1 preserves every per-leaf approval fact; Task 2 requires exact leaf membership and deterministic ordering.
- Scope: only planning artifacts change here. Downstream implementation remains confined to the six files declared by the two tasks.
- Validation: each task declares one single-purpose canonical command from the assigned task contract.
- Cross-references: every external alias is bound in Current task routing and is credited only for its retained responsibility.
- Impact: E2E and documentation remain first-class external tasks; neither is duplicated or silently dropped.
