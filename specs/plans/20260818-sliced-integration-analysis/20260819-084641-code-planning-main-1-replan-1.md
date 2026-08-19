# Master Replan: Integration Persistence, HEAD Invalidation, and Analysis Reconciliation

## Intent and evidence

Recreate exactly the three corrected specialized planning contracts corresponding to original master Tasks 1, 5, and 6. Preserve original master Tasks 2-4 and 7-11 as existing external scopes rather than redefining them.

Success means the replacement persistence task is the sole owner of `IntegrationMutationReceipt persistence schema`; the replacement mutation task consumes that schema and `integration lifecycle invariant validation` and proves public `MergeWorktree` rejects invalid lifecycle transitions before persistence; and the replacement reconciliation task consumes `IntegrationAnalysisMetadata persistence schema` and `integration lifecycle invariant validation` and proves public `ReconcileIntegrationAnalyses` and `SubmitVerdict` reject invalid transitions before persistence.

Based on: the full goal spec at `specs/goals/20260818-sliced-integration-analysis.md`; assigned task `code-planning-main-1-replan-1`; corrected master plan and output at commit `a2920bc0` for `specs/plans/20260818-sliced-integration-analysis/20260819-005612-code-planning-main-1.md`; corrected specialized persistence plan at the same commit; ADR-0055, ADR-0058, ADR-0059, ADR-0067, and ADR-0112; `INVARIANTS.md` concurrency, review, integration, and Protection Matrix sections; and the Update Policy plus integration-closure, state-validation-composition, and decomposition-cascade entries in `specs/architecture/architectural-issues.md`.

Doc Impact: this task changes only this planning artifact and its structured output. Retained master Task 11 continues to own product and architecture documentation after implementation evidence exists.

Test Impact: no implementation tests are written by this planning task. Each replacement contract retains its canonical fail-closed downstream validation command; retained master Task 10 continues to own end-to-end coverage.

## Architecture and ownership boundary

The corrected boundary separates durable representation from operations:

```text
Task 1: models + statevalidate
  owns lifecycle, analysis metadata, mutation-receipt schema, transition validation
       |                                      |
       v                                      v
Task 2 (original Task 5): mutation path   Task 3 (original Task 6): reconciliation
  produces/persists receipts                 projects metadata/evidence/verdicts
  public MergeWorktree proof                 public Reconcile/SubmitVerdict proofs
```

No replacement task redefines progress evaluation, configuration, pipeline capability, prompts, completion consumers, end-to-end tests, or documentation. Those remain with the retained master tasks below.

| Retained original task | Existing task ID | Preserved responsibility |
|---|---|---|
| Task 2 | `code-planning-main-1-code-planning-1` | Global generation configuration and normalization |
| Task 3 | `code-planning-main-1-code-planning-2` | Slice topology and frozen-pipeline capability |
| Task 4 | `code-planning-main-1-code-planning-3` | Authoritative pure progress decision |
| Task 7 | `code-planning-main-1-code-planning-6` | Bounded slice/global prompt context |
| Task 8 | `code-planning-main-1-code-planning-7` | State-changing effective-completion gate |
| Task 9 | `code-planning-main-1-code-planning-8` | Wake, supervisor, and status completion consumers |
| Task 10 | `code-planning-main-1-code-planning-9` | End-to-end lifecycle and controlled race evidence |
| Task 11 | `code-planning-main-1-code-planning-10` | Documentation and architectural issue lifecycle |

## Interface contracts

| Exact interface | Sole owner | Consumers in this replan |
|---|---|---|
| `IntegrationLifecycle persistence schema` | Task 1 | Tasks 2 and 3 |
| `IntegrationAnalysisMetadata persistence schema` | Task 1 | Task 3 |
| `IntegrationMutationReceipt persistence schema` | Task 1 | Task 2 |
| `integration lifecycle invariant validation` | Task 1 | Tasks 2 and 3 |
| `integration mutation receipt production and persistence` | Task 2 | — |
| `integration mutation linearization protocol` | Task 2 | Retained Tasks 8, 10, and 11 |
| `clean-source verification under the integration mutation lock` | Task 2 | Task 3 |
| `ReconcileIntegrationAnalyses` | Task 3 | Retained Tasks 7-11 |
| `analysis verdict projection` | Task 3 | — |
| `idempotent analysis task materialization` | Task 3 | — |

## Dependency order

```text
Task 1 ------------------------------+
  |                                  |
  +--> Task 2 --+                    |
       ^         |                    v
Retained Task 4 -+-----------------> Task 3
                                      ^
Retained Tasks 2, 3, and 4 -----------+
```

- Task 1 is independent.
- Task 2 depends on Task 1 and retained original Task 4.
- Task 3 depends on Tasks 1 and 2 plus retained original Tasks 2, 3, and 4.
- Sibling dependencies use `depends_on`; existing concrete task dependencies use `task_depends_on` per ADR-0058.
- Owned files are disjoint across all three replacements, so no additional serialization edge is required.

## Planned specialized tasks

### Task 1 — Replacement for original Task 1: persist integration evidence

Description: Persist typed integration lifecycle evidence for coverage snapshots, analysis identities, verdicts, and closure state.

Done when: `TestIntegrationLifecycleYAMLRoundTrip` preserves the contributing-set snapshot, coverage union, generation records, mutation receipts, and per-task analysis metadata; `TestIntegrationLifecycleValidation` rejects duplicate analysis keys, mutable cohort replacement, malformed evidence, non-monotonic generations, and clean evidence without an immutable source commit.

Scope: Own `internal/models/integration.go`, `internal/models/integration_test.go`, `internal/models/history.go`, `internal/models/task.go`, `internal/statevalidate/integration.go`, `internal/statevalidate/integration_test.go`, and validation wiring in `internal/statevalidate/validate.go`. Define persistence and validation only; do not derive progress, create tasks, mutate Git, or render prompts.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#proposed-model`

Dependencies: none.

Validation: `go test -json ./internal/models ./internal/statevalidate -run '^(TestIntegrationLifecycleYAMLRoundTrip|TestIntegrationLifecycleValidation)$' -count=1 | jq -e -s '([.[] | select(.Action == "pass" and (.Test == "TestIntegrationLifecycleYAMLRoundTrip" or .Test == "TestIntegrationLifecycleValidation")) | .Test] | unique | sort) == ["TestIntegrationLifecycleValidation","TestIntegrationLifecycleYAMLRoundTrip"] and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/models/integration.go, internal/models/integration_test.go, internal/models/history.go, internal/models/task.go, internal/statevalidate/integration.go, internal/statevalidate/integration_test.go, internal/statevalidate/validate.go]`; `owned_modules=[internal/models, internal/statevalidate]`; `read_only_depends_on=[]`; `read_only_task_depends_on=[]`; `interfaces_owned=[IntegrationLifecycle persistence schema, IntegrationAnalysisMetadata persistence schema, IntegrationMutationReceipt persistence schema, integration lifecycle invariant validation]`; `interfaces_consumed=[]`; coverage: durable facts distinguish slice evidence, global generations, immutable source commits, mutation receipts, and blocked or exhausted closure.

### Task 2 — Replacement for original Task 5: linearize HEAD invalidation

Description: Make every integration ref mutation invalidate superseded clean evidence at the mutation linearization point.

Done when: `TestIntegrationMutationLinearization` proves public `MergeWorktree` receipt persistence consumes the shared lifecycle transition validator, names the before/after commits without rewriting prior lifecycle evidence, occurs only after releasing the integration mutation lock, immediately makes old clean evidence ineffective, and clean finalization ordered before or after a racing mutation can never yield effective success for a stale commit.

Scope: Own `internal/ops/integration_mutation_lock.go`, `internal/ops/integration_mutation_lock_test.go`, `internal/ops/wt_merge.go`, and `internal/ops/wt_merge_test.go`. Preserve ADR-0112 lock order, CAS merge behavior, rollback behavior, and the prohibition on blackboard writes under the integration mutation lock; do not own sprint progression or generation reconciliation.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Dependencies: Task 1 and retained original Task 4 (`code-planning-main-1-code-planning-3`).

Validation: `go test -json ./internal/ops -run '^TestIntegrationMutationLinearization$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestIntegrationMutationLinearization") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/integration_mutation_lock.go, internal/ops/integration_mutation_lock_test.go, internal/ops/wt_merge.go, internal/ops/wt_merge_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[0]`; `read_only_task_depends_on=[code-planning-main-1-code-planning-3]`; `interfaces_owned=[integration mutation receipt production and persistence, integration mutation linearization protocol, clean-source verification under the integration mutation lock]`; `interfaces_consumed=[IntegrationLifecycle persistence schema, IntegrationMutationReceipt persistence schema, integration lifecycle invariant validation, EvaluateIntegrationProgress]`; coverage: live HEAD mismatch is the immediate invalidator; public `MergeWorktree` appends typed receipts only after transition validation and lock release while preserving prior evidence.

### Task 3 — Replacement for original Task 6: reconcile analysis generations

Description: Reconcile deterministic slice and global analysis tasks from the authoritative progress decision.

Done when: `TestReconcileIntegrationAnalyses` proves cohort snapshotting and missing-task creation are atomic and idempotent across repeated wake or restart calls; public `ReconcileIntegrationAnalyses` rejects replacement of a frozen cohort and public `SubmitVerdict` rejects rewriting previously projected coverage by consuming the shared lifecycle transition validator before persistence; slice verdicts project immutable coverage evidence; global findings wait for resolved repair or replacement lineage; clean verdicts bind to the verified source commit; and slice or generation exhaustion records an explicit blocked state.

Scope: Own `internal/ops/integration_reconcile.go`, `internal/ops/integration_reconcile_test.go`, `internal/ops/submit_verdict.go`, and `internal/ops/submit_verdict_test.go`. Create and project analysis lifecycle state through existing authorization boundaries; do not render prompts, mutate the integration ref, or decide sprint completion independently.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#global-integration`

Dependencies: Tasks 1 and 2 plus retained original Tasks 2 (`code-planning-main-1-code-planning-1`), 3 (`code-planning-main-1-code-planning-2`), and 4 (`code-planning-main-1-code-planning-3`).

Validation: `go test -json ./internal/ops -run '^TestReconcileIntegrationAnalyses$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestReconcileIntegrationAnalyses") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/integration_reconcile.go, internal/ops/integration_reconcile_test.go, internal/ops/submit_verdict.go, internal/ops/submit_verdict_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[0,1]`; `read_only_task_depends_on=[code-planning-main-1-code-planning-1, code-planning-main-1-code-planning-2, code-planning-main-1-code-planning-3]`; `interfaces_owned=[ReconcileIntegrationAnalyses, analysis verdict projection, idempotent analysis task materialization]`; `interfaces_consumed=[IntegrationLifecycle persistence schema, IntegrationAnalysisMetadata persistence schema, integration lifecycle invariant validation, Config.MaxGlobalIntegrationGenerations, SlicedIntegrationCapability, EvaluateIntegrationProgress, clean-source verification under the integration mutation lock]`; coverage: deterministic keys and atomic reconciliation prevent duplicate generations after wake and restart, and public reconciliation/verdict paths fail before immutable lifecycle evidence can be replaced or rewritten.

## Systemic Decomposition Review

The systemic lens was applied to schema ownership, transition enforcement, public mutation boundaries, external dependency remapping, retained-scope references, and exact interface-by-interface downstream fan-out.

No systemic issues identified.

## Spec Compliance Matrix

`Task 1`, `Task 2`, and `Task 3` below are this plan's three outputs. `Retained Task N` names the unchanged original master scope and its concrete task ID from the table above.

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Bounded per-scope coverage precedes repeated global closure analysis. | Objective | Retained Tasks 3-4 and 7; Task 3 | Covered |
| 2 | Single-lineage coverage records task, criteria, reviewed commit, approver, validation, and merge commit. | Proposed Model | Task 1; Retained Task 4; Task 3 | Covered |
| 3 | Contributing scopes and distinct root coding lineages are reproducible. | Slice Integration | Task 1; Retained Task 4 | Covered |
| 4 | Planning settles only after coding-producing sources, outputs, transitions, and resulting coding work settle. | Slice Integration | Retained Tasks 4 and 9; Task 3 | Covered |
| 5 | Partial planning handoff cannot open coverage. | Required Properties | Retained Tasks 4 and 10 | Covered |
| 6 | The contributing set freezes exactly once after the settled boundary. | Required Properties | Task 1; Retained Tasks 4 and 10; Task 3 | Covered |
| 7 | Fewer than two contributing scopes produce no slice. | Required Properties | Retained Tasks 4 and 10; Task 3 | Covered |
| 8 | Multiple contributing scopes each yield bounded coverage. | Required Properties | Task 1; Retained Tasks 4 and 10; Task 3 | Covered |
| 9 | One-lineage scopes reuse approval attestations without a slice. | Required Properties | Task 1; Retained Tasks 4 and 10; Task 3 | Covered |
| 10 | Multi-lineage scopes with merged work produce exactly one slice. | Required Properties | Retained Tasks 3, 4, and 10; Task 3 | Covered |
| 11 | Escalation plans remain repair lineage outside the contributing set. | Required Properties | Task 1; Retained Tasks 4 and 10; Task 3 | Covered |
| 12 | Lineage attributes coding, fixes, and replacements to a slice. | Required Properties | Task 1; Retained Task 4; Task 3 | Covered |
| 13 | Slice context is bounded to plan, descendants, commits, paths, metadata, and source snapshot. | Slice Integration | Task 1; Task 3; Retained Tasks 7 and 10 | Covered |
| 14 | Slice findings reuse integration review and coding-fix lifecycle. | Slice Integration | Retained Tasks 3 and 10; Task 3 | Covered |
| 15 | Later sibling mutations do not reopen a completed slice. | Slice Integration | Task 1; Retained Tasks 4 and 10 | Covered |
| 16 | Slice resolution follows merged fix/replacement lineage; unresolved terminal work blocks. | Slice Integration | Retained Tasks 4 and 10; Task 3 | Covered |
| 17 | Clean slice evidence cannot imply whole-goal completion. | Slice Integration | Task 1; Retained Tasks 4, 8, and 9 | Covered |
| 18 | Global analysis waits for all planning, coding, repair, coverage, and resolution barriers. | Global Integration | Retained Tasks 4, 8, 9, and 10; Task 3 | Covered |
| 19 | A blocked slice prevents global analysis. | Global Integration | Retained Tasks 4 and 10; Task 3 | Covered |
| 20 | Global context uses coverage navigation but independently inspects the aggregate branch. | Global Integration | Task 1; Retained Tasks 7 and 10 | Covered |
| 21 | Promoted repairs remain repair lineage visible to the next global generation. | Global Integration | Task 1; Retained Tasks 4 and 10; Task 3 | Covered |
| 22 | Global findings require another pass after resolved repair/replacement work. | Final Closure | Retained Tasks 4 and 10; Task 3 | Covered |
| 23 | Completion requires clean evidence bound to current integration HEAD. | Final Closure | Task 1; Task 2; Retained Tasks 4, 8, 9, and 10 | Covered |
| 24 | Completion and mutation have one linearizable order without relying on later wake. | Final Closure | Task 2; Retained Tasks 4, 8, 9, and 10 | Covered |
| 25 | The integration mutation path owns invalidation. | Final Closure | Task 2; Retained Task 10 | Covered |
| 26 | Finalization preserves ADR-0112 lock order and no state write under mutation lock. | Final Closure | Tasks 2 and 3; Retained Task 10 | Covered |
| 27 | HEAD mismatch invalidates evidence and requires another generation. | Final Closure | Tasks 2 and 3; Retained Tasks 4, 9, and 10 | Covered |
| 28 | Global generation bound is configurable with deterministic default and explicit exhaustion. | Final Closure | Task 1; Task 3; Retained Tasks 2, 4, and 10 | Covered |
| 29 | Slice exhaustion or unresolved terminal outcomes block before global analysis. | Final Closure | Retained Tasks 4 and 10; Task 3 | Covered |
| 30 | Wake/restart recovery cannot duplicate slice or global analyses. | Required Properties | Retained Tasks 4, 9, and 10; Task 3 | Covered |
| 31 | Workflow remains stack-agnostic and preserves review/merge authorization. | Required Properties | Tasks 2 and 3; Retained Tasks 2, 3, and 10 | Covered |
| 32 | No coverage begins while any planning/output/transition/coding prerequisite is unsettled. | Success Criterion 1 | Retained Tasks 4 and 10; Task 3 | Covered |
| 33 | Cohort classification and mixed coverage are reproducible. | Success Criteria 2-3 | Task 1; Retained Tasks 4 and 10; Task 3 | Covered |
| 34 | Global analysis is unclaimable behind every local barrier. | Success Criterion 4 | Retained Tasks 4, 9, and 10; Task 3 | Covered |
| 35 | Slice surfaces are immutable and global review remains independent. | Success Criteria 5-6 | Task 1; Retained Tasks 7 and 10 | Covered |
| 36 | Finalization is clean/current-HEAD under both race orders. | Success Criteria 7-8 | Task 2; Retained Tasks 4, 8, 9, and 10 | Covered |
| 37 | Later mutations rescan within budget and block after exhaustion. | Success Criterion 9 | Task 2; Task 3; Retained Tasks 2, 4, and 10 | Covered |
| 38 | Repeated wake/restart evaluation remains duplicate-free. | Success Criterion 10 | Retained Tasks 4, 9, and 10; Task 3 | Covered |
| 39 | No new roles; specialization uses role-pairs. | Out of Scope | Retained Task 3 | Covered |
| 40 | ADR-0113 extends ADR-0055 and supersedes no-rescan. | Documentation Impact | Retained Task 11 | Covered |
| 41 | State-machine and task-lifecycle docs are updated. | Documentation Impact | Retained Task 11 | Covered |
| 42 | Pipeline, operations, configuration, and terminal-outcome docs are updated. | Documentation Impact | Retained Task 11 | Covered |
| 43 | Integration-closure issue changes only after validation evidence exists. | Documentation Impact | Retained Tasks 10-11 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | Retained Task 10 (`code-planning-main-1-code-planning-9`) | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | Retained Task 11 (`code-planning-main-1-code-planning-10`) | Covered |

## Pre-submit audit

- Exactly three planned specialized tasks correspond to original master Tasks 1, 5, and 6 in output order.
- Task 1 solely owns `IntegrationMutationReceipt persistence schema`; Task 2 consumes it and the lifecycle validator; Task 3 consumes analysis metadata and the lifecycle validator.
- Public `MergeWorktree`, `ReconcileIntegrationAnalyses`, and `SubmitVerdict` rejection proofs are present in the corrected done-when contracts.
- Replacement owned-file sets are disjoint. Existing task dependencies are explicit and sibling dependencies are acyclic.
- Retained master Tasks 2-4 and 7-11 are referenced without changing their contracts.
- All scoped requirements and the full goal-level compliance map have no GAP; E2E and documentation remain first-class retained tasks.
