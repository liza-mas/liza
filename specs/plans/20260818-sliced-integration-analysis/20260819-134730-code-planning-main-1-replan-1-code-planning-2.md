# Code Plan: Reconcile Sliced Integration Analyses

## Intent and evidence

Implement the state-changing half of sliced integration as two ordered, cohesive changes: first make final integration-review approval project immutable lifecycle evidence through public `SubmitVerdict`; then make public `ReconcileIntegrationAnalyses` persist only the cohort, coverage, analysis tasks, planned-scope registration, and blocked/exhausted closure requested by the authoritative `EvaluateIntegrationProgress` decision.

Success means both public writers use one alias-safe pre-persistence boundary that first runs candidate `statevalidate.ValidateState` and then `statevalidate.ValidateIntegrationLifecycleTransition`; malformed new evidence, metadata, or task state and rewrites of prior evidence abort the complete blackboard transaction; and reconciliation creates each deterministic analysis task together with exactly one `Sprint.Scope.Planned` membership across wake, restart, permutation, and concurrent calls.

Based on:

- The full goal spec at `specs/goals/20260818-sliced-integration-analysis.md`.
- The authoritative master replan at `specs/plans/20260818-sliced-integration-analysis/20260819-084641-code-planning-main-1-replan-1.md`.
- The merged persistence and mutation contracts at `specs/plans/20260818-sliced-integration-analysis/20260819-111435-code-planning-main-1-replan-1-code-planning-0.md` and `specs/plans/20260818-sliced-integration-analysis/20260819-122954-code-planning-main-1-replan-1-code-planning-1.md`.
- The merged generation and topology contracts at `specs/plans/20260818-sliced-integration-analysis/20260819-075818-code-planning-main-1-code-planning-1.md` and `specs/plans/20260818-sliced-integration-analysis/20260819-073455-code-planning-main-1-code-planning-2.md`.
- The merged `EvaluateIntegrationProgress` interface contract at `specs/plans/20260818-sliced-integration-analysis/20260819-115431-code-planning-main-1-code-planning-3.md`. Its implementation file is not present at review HEAD `9eb4ea90`, so this plan relies on the merged artifact and task dependency rather than claiming source evidence for pending code.
- Current source at review HEAD `9eb4ea90`: `internal/models/integration.go`, `internal/statevalidate/integration.go`, `internal/statevalidate/validate.go`, `internal/ops/submit_verdict.go`, `internal/ops/proceed.go`, `internal/ops/add_tasks.go`, `internal/db/blackboard.go`, `internal/models/sprint.go`, and `internal/agent/workdetection.go`.
- ADR-0055 and ADR-0112; `INVARIANTS.md` task-state, concurrency, review, integration, sprint, scope, and Protection Matrix sections; and the Update Policy plus integration-closure, state-validation-composition, cross-pair, and decomposition-cascade entries in `specs/architecture/architectural-issues.md`.

Load-bearing claims:

- **EVIDENCED — validation composition:** `Blackboard.Modify` persists any callback-success candidate without structural validation, while `ValidateIntegrationLifecycleTransition` explicitly protects only prior lifecycle prefixes and delegates candidate structure to `ValidateState`. Checked against `internal/db/blackboard.go`, `internal/statevalidate/validate.go`, `internal/statevalidate/integration.go`, and the merged `PERSIST` plan.
- **EVIDENCED — planned-scope visibility:** orchestration completion and integration-task discovery iterate `Sprint.Scope.Planned`, and existing task creators append task and planned membership in the same `Modify` callback with a deduplication guard. Checked against `internal/models/sprint.go`, `internal/agent/workdetection.go`, `internal/ops/add_tasks.go`, and `internal/ops/proceed.go`.
- **EVIDENCED — dependency boundary:** the assigned planning task consumes the merged `PERSIST`, `MUTATE`, `CFG`, `TOPO`, and `PROGRESS` contracts. Task 2 depends on Task 1 so it can consume the shared same-package validation boundary without shared-file ownership.
- **EVIDENCED — provenance semantics:** `ParentTasks` is the canonical multi-parent back-reference and task ancestry is reconstructed through `EffectiveParentTasks`. Slice roots and coverage witnesses therefore form provenance, not claimability; `DependsOn` remains empty because integration clean states are not generic `MERGED` dependencies.

Doc Impact: only this planning artifact and its structured output. Goal-level documentation remains owned by `code-planning-main-1-code-planning-10` after implementation and end-to-end evidence exist.

Test Impact: Task 1 adds focused public-boundary tests in `internal/ops/submit_verdict_test.go`; Task 2 adds the required aggregate reconciliation test in `internal/ops/integration_reconcile_test.go`. Goal-level end-to-end and controlled-race coverage remains owned by `code-planning-main-1-code-planning-9`.

## Current task routing

`Task 1` and `Task 2` are this plan's outputs. Other aliases are existing concrete tasks and are not work added to this scope.

| Alias | Concrete task ID | Responsibility |
|---|---|---|
| `PERSIST` | `code-planning-main-1-replan-1-code-planning-0` | Lifecycle schema plus candidate structural and before/after transition validation |
| `CFG` | `code-planning-main-1-code-planning-1` | Configurable global-generation ceiling and deterministic default |
| `TOPO` | `code-planning-main-1-code-planning-2` | Slice/global role-pair topology and frozen-pipeline capability |
| `PROGRESS` | `code-planning-main-1-code-planning-3` | Pure cohort, barrier, request, exhaustion, and effective-completion decision |
| `MUTATE` | `code-planning-main-1-replan-1-code-planning-1` | Mutation receipts and clean-source verification under ADR-0112 ordering |
| Task 1 / `PROJECT` | this plan output 0 | Immutable analysis-verdict projection and shared pre-persistence validation boundary |
| Task 2 / `RECONCILE` | this plan output 1 | Atomic idempotent cohort/task/planned-scope/closure reconciliation |
| `CONTEXT` | `code-planning-main-1-code-planning-6` | Bounded slice and independent aggregate prompt context |
| `GATE` | `code-planning-main-1-code-planning-7` | State-changing effective-completion barriers |
| `CONSUMERS` | `code-planning-main-1-code-planning-8` | Wake, supervisor, status, and terminal consumers |
| `E2E` | `code-planning-main-1-code-planning-9` | End-to-end lifecycle, restart, rescan, exhaustion, and race evidence |
| `DOC` | `code-planning-main-1-code-planning-10` | ADR-0113, lifecycle/operator docs, and issue disposition |

The parent planning task already depends on `PERSIST`, `MUTATE`, `CFG`, `TOPO`, and `PROGRESS`; its phase gate supplies their implementation dependencies to both outputs. Task 2 additionally depends on Task 1 because it consumes Task 1's shared validation helper and its aggregate test exercises public verdict projection.

## Architecture and ownership boundary

```text
frozen capability + live integration HEAD + current state
                         |
                         v
             EvaluateIntegrationProgress
                         |
                         v
       ReconcileIntegrationAnalyses (Task 2)
        | freeze cohort + attestations
        | create task + planned membership atomically
        ` project blocked/exhausted closure
                         |
                  analyst/reviewer pair
                         |
                         v
               SubmitVerdict (Task 1)
        | append slice/global verdict evidence
        ` project clean closure from verified source
                         |
                         v
       ValidateState(candidate)
                         |
                         v
       ValidateIntegrationLifecycleTransition(previous, candidate)
                         |
                         v
                    persistence
```

`PERSIST` remains the sole schema and invariant owner. `PROGRESS` remains the sole owner of eligibility, lineage resolution, barriers, next generation, and exhaustion. `TOPO` remains the role-pair capability owner. `MUTATE` remains the integration-ref writer and supplies lock-ordered clean-source verification. This plan owns only translation from those interfaces and reviewed task facts into authorized atomic blackboard mutations.

The change intersects task-state, concurrency, review, integration, and sprint-completion protections. Preserve `db.Blackboard.Modify` as the state-atomicity boundary, existing pipeline-resolved review transitions and quorum, and ADR-0112's integration-mutation-lock-before-blackboard-read order. Neither task writes state while holding the integration mutation lock.

## Shared pre-persistence validation protocol

Task 1 defines a narrow same-package helper consumed by both tasks. It is composition, not a new validator.

1. At entry to each relevant `db.Blackboard.Modify` callback, capture an unaliased before image of `Goal.Integration` and every existing task's `IntegrationAnalysis` metadata. Clone all nested slices and pointers needed by transition comparison; retaining aliases would make rewrite detection vacuous.
2. Build the candidate in the callback. Immediately before callback success, call `statevalidate.ValidateState(candidate, projectRoot, false, io.Discard)` to reject malformed newly appended lifecycle records, metadata, tasks, references, statuses, and sprint scope.
3. Only after candidate validation succeeds, call `statevalidate.ValidateIntegrationLifecycleTransition(previous, candidate)` to reject clearing, replacement, reordering, or rewriting of persisted evidence.
4. Return either error from the callback. The blackboard then persists none of the candidate's status, history, reviewer-release, evidence, cohort, task, or planned-scope changes.

A restored test hook may corrupt the candidate immediately before validation. Task 1 proves both a malformed new evidence append and a prior-prefix rewrite fail through public `SubmitVerdict`; Task 2 proves malformed new metadata/task state and frozen-cohort replacement fail through public reconciliation. Hooks are restored with `t.Cleanup`.

## Task 1 design: project reviewed verdicts

Extend the final-quorum `APPROVED` branch of `SubmitVerdict` only when the reviewed task carries `IntegrationAnalysis` metadata matching its role-pair phase. Ordinary verdict behavior remains unchanged.

1. Derive the immutable analysis verdict from the reviewed artifact: empty `Output` is `clean`; non-empty `Output` is `findings`. Do not parse finding text or re-evaluate progress.
2. Use `IntegrationAnalysisMetadata.SourceCommit` as the analyzed source and `Task.ReviewCommit` as the analyst report commit; never substitute one for the other.
3. A slice approval appends exactly one `IntegrationCoverageRecord` of kind `slice_report`, bound to metadata key, originating plan, exact frozen roots, source commit, report commit, and verdict. Findings remain slice evidence; `PROGRESS` owns repair/replacement resolution.
4. A global approval appends exactly one contiguous `IntegrationGlobalGeneration`. Findings do not create clean closure. Clean may project `IntegrationClosureStatusClean` only after `MUTATE` verifies the metadata source under the integration mutation lock and releases that lock.
5. Run clean-source verification before the verdict `Modify`. A later ref mutation may make the new projection stale, but `PROGRESS` compares immutable source with live HEAD, so do not hold the mutation lock across the state write and do not erase history.
6. Run the shared candidate-plus-transition validation after the complete verdict candidate is built. Reject phase mismatch, duplicate projection, malformed new evidence, or prior evidence rewrite before any part of the verdict transaction persists. Partial approval records no lifecycle evidence.

Preserve approval/rejection history, impact escalation, quorum, reviewer release, clean-vs-approved routing, rejection exhaustion, and existing authorization. If rejection exhaustion blocks an analysis task, leave verdict evidence absent; `PROGRESS` reports the barrier and Task 2 projects explicit closure.

### Task 1 test design

`TestSubmitVerdictIntegrationLifecycleProjection` uses public `SubmitVerdict` and deterministic named subtests:

- final-quorum clean and findings slice approvals append correctly tagged reports with distinct source/report commits; partial approval appends none;
- global findings append one generation without clean closure; global clean binds closure generation/key/source to the verifier-confirmed source;
- existing coverage, generations, mutation receipts, and analysis metadata remain unchanged prefixes;
- role/phase mismatch and duplicate projection fail closed;
- candidate corruption makes a newly appended report structurally invalid, proving `ValidateState` aborts the full transaction before status, approval/history, reviewer release, or evidence persists;
- a separate corruption rewrites prior coverage or metadata, proving transition validation aborts the same full transaction;
- ordinary non-integration approval follows the existing path unchanged.

Use channels and bounded failure timeouts for clean-source ordering seams; never use sleeps or retry-until-green behavior.

Validation: `go test -json ./internal/ops -run '^TestSubmitVerdictIntegrationLifecycleProjection$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestSubmitVerdictIntegrationLifecycleProjection") and all(.[]; .Action != "fail")'`

## Task 2 design: reconcile authoritative requests

Add public `ReconcileIntegrationAnalyses(projectRoot string)` returning whether state changed, created task IDs in deterministic order, and any projected blocked/exhausted reason. It performs no terminal I/O.

Within one `db.Blackboard.Modify` callback:

1. Load the frozen resolver and `SlicedIntegrationCapability`, read integration HEAD, and call `EvaluateIntegrationProgress` against the callback's current state. Treat the decision as authoritative; do not duplicate cohort, lineage, barrier, generation, or exhaustion policy.
2. If requested, freeze the decision's canonical contributing set exactly once and append decision-provided one-lineage approval attestations. Existing cohort and coverage entries are never replaced.
3. Process missing slice/global requests in sorted key order. Task ID is deterministic: `integration-` plus the key with `:` replaced by `-`. An exact existing ID/key/metadata match is idempotent; any ID/key/metadata collision is an error.
4. For every requested task, append a new task to `State.Tasks` only if absent and register that ID in `Sprint.Scope.Planned` in the same callback only if absent. The task and planned membership are one atomic projection. Repeated wake, restart, permutation, and concurrent calls must leave exactly one task and exactly one planned membership per request.
5. Resolve role-pair initial status and task type from the frozen pipeline. Slice uses `slice-integration-pair`; global uses `integration-pair`; both use `TaskTypeIntegration`, priority `1`, the callback timestamp, empty history, immutable metadata, and `DependsOn: nil`. Existing analyst/reviewer claim, review, fix-transition, and merge authorization remain authoritative.
6. Attach immutable metadata at creation. Slice metadata contains decision key, originating plan, exact sorted roots, sorted descendant task/merge-commit attribution, analyzed HEAD, sorted affected paths from descendant reviewed ranges, and the subset present as regular files in that source tree. Global metadata contains key, positive generation, and analyzed HEAD; `CONTEXT` owns aggregate rendering.
7. Preserve source refs needed by `CONTEXT`: slice copies the originating plan's normalized spec/plan/architecture refs; global uses the normalized goal spec and no invented plan/architecture ref.
8. Apply this exact provenance policy, always sorted lexically and deduplicated:
   - slice `ParentTasks` is exactly its frozen root task IDs; the originating plan is already reachable through those roots and is also explicit in metadata;
   - global generation 1 `ParentTasks` contains one witness per frozen scope: the approval attestation's `ReviewedTaskID`, the slice report's `AnalysisTaskID`, or, only on the fewer-than-two-scope no-coverage bypass, that scope's root task IDs;
   - global generation N > 1 uses the same coverage witnesses plus generation N-1's `AnalysisTaskID` before sort/dedup, preserving the rescan chain and its repair lineage.
   These are provenance back-references, not dependency gates; `DependsOn` remains empty because clean integration terminal states are not `MERGED`.
9. Waiting is a no-op. Blocked/exhausted decisions project their exact machine-readable reason as `IntegrationClosureStatusBlocked` or `IntegrationClosureStatusExhausted`; do not invent policy or decide sprint completion.
10. Run the shared candidate-plus-transition validation after every candidate mutation. Git snapshot, pipeline, decision, collision, planned-registration, structural-validation, or transition-validation failure aborts the whole callback, leaving no partial cohort, evidence, task, or planned membership.

### Task 2 test design

`TestReconcileIntegrationAnalyses` exercises public reconciliation and, for package-boundary aggregate projection, public `SubmitVerdict`:

1. An unsettled planning/output/transition/coding boundary is a no-op. At settlement, one atomic call freezes the canonical cohort, appends attestations, creates every missing slice, and gives every created ID exactly one `Sprint.Scope.Planned` membership.
2. A forced structural failure after cohort/task/planned preparation persists none of them. Candidate corruption of new analysis metadata or task state is rejected by `ValidateState`; frozen-cohort replacement is rejected by transition validation.
3. Repeated call, new `Blackboard` instance, task-order permutation, and concurrent callers yield the same ordered keys/IDs, exact metadata, exact `ParentTasks`, exactly one task, and exactly one planned membership per request.
4. Fewer than two scopes create no slices; missing sliced capability when a slice is required projects the decision's blocked reason and creates no analysis task.
5. Slice clean/findings verdicts project immutable coverage. Global creation waits while pending, blocked, abandoned, superseded, repair, or replacement lineage remains unresolved and begins only when `PROGRESS` reports all barriers resolved.
6. Global generation 1 uses the exact coverage-witness parent set. After findings resolve, generation 2 uses those witnesses plus generation 1's analysis task, has the current HEAD, and is unique and bounded.
7. Clean global verdict uses verified source/report commits; changed live HEAD makes it ineffective and requests the next generation while budget remains.
8. Slice/review exhaustion and normalized global-generation exhaustion project explicit blocked/exhausted closure with no extra task.
9. Public `SubmitVerdict` cannot rewrite projected coverage, and every rejected structural or transition candidate leaves both state collections and sprint planned scope unchanged.

The canonical validation must observe the exact top-level pass and reject every Go failure event:

Validation: `go test -json ./internal/ops -run '^TestReconcileIntegrationAnalyses$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestReconcileIntegrationAnalyses") and all(.[]; .Action != "fail")'`

## Planned coding tasks

### Task 1 — Project immutable integration verdict evidence

Description: Project approved slice and global analysis verdicts into immutable lifecycle evidence at `SubmitVerdict`'s atomic persistence boundary.

Done when: `TestSubmitVerdictIntegrationLifecycleProjection` proves final-quorum slice and global approvals append immutable evidence with distinct analyzed-source and analyst-report commits; partial approval and ordinary non-integration verdicts preserve existing behavior; global clean projection consumes lock-ordered clean-source verification; and public `SubmitVerdict` composes candidate `ValidateState` with shared lifecycle transition validation against an alias-safe before image so malformed newly appended evidence, duplicate projection, or attempted prior-evidence rewrite aborts the whole verdict transaction before status, history, reviewer release, or evidence persists.

Scope: Own `internal/ops/submit_verdict.go` and `internal/ops/submit_verdict_test.go`. Define the same-package pre-persistence helper that runs candidate structural validation followed by lifecycle transition validation, and use it for integration-analysis verdict projection inside the existing atomic review boundary; preserve quorum, history, impact, rejection escalation, reviewer release, pipeline-resolved statuses, and authorization. Do not create analysis tasks, derive progress, render context, mutate the integration ref, or decide sprint completion.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Dependencies: inherited `PERSIST`, `MUTATE`, `CFG`, `TOPO`, and `PROGRESS` implementation dependencies from the parent planning graph.

Validation: `go test -json ./internal/ops -run '^TestSubmitVerdictIntegrationLifecycleProjection$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestSubmitVerdictIntegrationLifecycleProjection") and all(.[]; .Action != "fail")'`

### Task 2 — Reconcile deterministic analysis lifecycle work

Description: Materialize exactly the missing slice and global analysis work and closure projection requested by `EvaluateIntegrationProgress`.

Done when: `TestReconcileIntegrationAnalyses` proves cohort freezing, one-lineage attestations, immutable slice snapshots, task insertion, and deduplicated `Sprint.Scope.Planned` registration are one atomic idempotent transaction across repeated wake, restart, task-order permutation, and concurrent calls; created slice and global tasks have the exact deterministic `ParentTasks` policy; public reconciliation composes candidate `ValidateState` with shared lifecycle transition validation so malformed newly appended metadata/task state or frozen-cohort replacement aborts the entire transaction; global creation waits for resolved repair or replacement lineage; later current-HEAD generations are unique and bounded; and slice/capability/review or generation exhaustion records explicit blocked/exhausted closure.

Scope: Own `internal/ops/integration_reconcile.go` and `internal/ops/integration_reconcile_test.go`. Consume frozen-pipeline capability, authoritative progress decisions, persisted lifecycle types, the Task 1 pre-persistence validation helper, and existing Git read helpers to create initial slice/global analysis tasks, register each created task exactly once in `Sprint.Scope.Planned`, and project closure only. Do not modify verdict implementation, pipeline topology, progress policy, prompts, completion consumers, the integration ref, or sprint completion.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#global-integration`

Dependencies: Task 1 plus inherited `PERSIST`, `MUTATE`, `CFG`, `TOPO`, and `PROGRESS` implementation dependencies from the parent planning graph.

Validation: `go test -json ./internal/ops -run '^TestReconcileIntegrationAnalyses$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestReconcileIntegrationAnalyses") and all(.[]; .Action != "fail")'`

## Architecture review

### Discovery

`internal/models` owns typed lifecycle facts; `internal/statevalidate` separately owns candidate structure and immutable transitions; `internal/ops` owns authorized mutation; `Blackboard.Modify` owns serialization but does not validate callback candidates. Sprint completion and integration-task detection use `Sprint.Scope.Planned`, so task insertion without planned registration is an orchestration-invisible state. `SubmitVerdict` already supplies one atomic review transaction; existing task creators establish the task-plus-planned projection pattern.

### Analysis

| Question | Assessment |
|---|---|
| Problem | Translate pure progress decisions and final review approvals into durable, visible, immutable lifecycle state. |
| Stable boundaries | Typed persistence, frozen pipeline resolution, public review authorization, blackboard atomicity, and ADR-0112 lock order. |
| Change vectors | Analysis task materialization and verdict-to-evidence projection; progress policy and rendering remain external. |
| Cost of error | High: invalid evidence can certify stale work; an unplanned task can be invisible to completion and repeatedly recreated. |
| Failure handling | Candidate structural or transition failure aborts the full callback. Expected waiting/blocking/exhaustion remains a `PROGRESS` decision. |
| Concurrency | Deterministic IDs plus one locked read/decision/write callback provide idempotency; clean verification follows mutation-lock then read, never state write under that lock. |
| Data ownership | `PERSIST` validates facts, `PROGRESS` decides, this plan projects, `CONTEXT` renders, and later gates/consumers decide completion. |
| Boundary risk | Duplicating policy, aliasing the before image, omitting planned registration, or treating provenance as dependency claimability. Each is explicitly excluded. |

Two ordered coding tasks remain the minimum coherent split: verdict projection owns the shared validation seam in its assigned file; reconciliation consumes that seam and owns disjoint files. Splitting validation from verdict projection would create another shared-file task without an independent behavior. No new architecture issue is introduced; the existing integration-closure issue remains open until `E2E` validation exists.

## Spec Compliance Matrix

This matrix covers the complete goal. Task 1 and Task 2 are credited only for this plan's projection/reconciliation boundary; retained aliases identify external owners without expanding scope.

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Bounded per-scope coverage precedes repeated global closure analysis. | Objective | `PROGRESS`; Task 1; Task 2; `CONTEXT`; `E2E` | Covered |
| 2 | Single-lineage coverage records task, criteria, reviewed commit, approver, validation, and merge commit. | Proposed Model | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 3 | Contributing scopes and distinct root coding lineages are reproducible. | Slice Integration | `PROGRESS`; Task 2; `E2E` | Covered |
| 4 | Planning settles only after coding-producing sources, outputs, transitions, and resulting coding work settle. | Slice Integration | `PROGRESS`; Task 2; `CONSUMERS`; `E2E` | Covered |
| 5 | Partial planning handoff cannot open coverage. | Required Property 1 | `PROGRESS`; Task 2; `E2E` | Covered |
| 6 | The contributing set freezes exactly once after the settled boundary. | Required Property 2 | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 7 | Fewer than two contributing scopes produce no slice. | Required Property 3 | `PROGRESS`; Task 2; `E2E` | Covered |
| 8 | Multiple contributing scopes each yield bounded coverage. | Required Property 4 | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 9 | One-lineage scopes reuse approval attestations without a slice. | Required Property 5 | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 10 | Multi-lineage scopes with merged work produce exactly one slice. | Required Property 6 | `TOPO`; `PROGRESS`; Task 2; `E2E` | Covered |
| 11 | Escalation plans remain repair lineage outside the contributing set and create no slice. | Required Property 7 | `PROGRESS`; Task 2; `E2E` | Covered |
| 12 | Lineage attributes coding, fixes, and replacements to a slice. | Required Property 8 | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 13 | Slice context is bounded to plan, descendants, commits, paths, metadata, and source snapshot. | Required Property 9; Slice Integration | `PERSIST`; Task 2; `CONTEXT`; `E2E` | Covered |
| 14 | Slice verdicts persist analyzed descendant changes and immutable source snapshot. | Required Property 10 | `PERSIST`; Task 1; Task 2; `E2E` | Covered |
| 15 | Slice findings reuse integration-reviewer and coding-pair repair lifecycle. | Slice Integration | `TOPO`; Task 1; Task 2; `E2E` | Covered |
| 16 | Later sibling mutations do not reopen completed slices. | Slice Integration | `PERSIST`; `PROGRESS`; Task 1; `E2E` | Covered |
| 17 | Slice resolution follows merged repair/replacement lineage; unresolved terminal work blocks. | Slice Integration | `PROGRESS`; Task 2; `E2E` | Covered |
| 18 | Clean slice evidence remains slice-local and cannot imply goal completion. | Slice Integration | `PERSIST`; `PROGRESS`; Task 1; `GATE`; `CONSUMERS` | Covered |
| 19 | Global analysis waits for planning, coding, repair, required-slice, and resolution barriers; blocked slices prevent it. | Required Property 11; Global Integration | `PROGRESS`; Task 2; `GATE`; `CONSUMERS`; `E2E` | Covered |
| 20 | Global analysis independently inspects the aggregate branch from bounded coverage navigation. | Required Property 12; Global Integration | Task 2; `CONTEXT`; `E2E` | Covered |
| 21 | Promoted repairs remain repair lineage visible to the next global analysis. | Global Integration | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 22 | Global findings require another pass after repair or replacement resolution. | Final Closure | `PROGRESS`; Task 1; Task 2; `E2E` | Covered |
| 23 | Global fixes and later integration-HEAD mutations trigger another scan while budget remains. | Required Property 13 | `CFG`; `PROGRESS`; `MUTATE`; Task 1; Task 2; `CONSUMERS`; `E2E` | Covered |
| 24 | Slice or global-generation exhaustion produces an explicit blocked outcome. | Required Property 14 | `PERSIST`; `PROGRESS`; Task 2; `CONSUMERS`; `E2E` | Covered |
| 25 | Clean completion is tied to an immutable reviewed commit. | Required Property 15 | `PERSIST`; `PROGRESS`; `MUTATE`; Task 1; `E2E` | Covered |
| 26 | Completion state, clean reviewed commit, and integration HEAD have one linearizable relationship. | Required Property 16; Success Criteria 7-8 | `PROGRESS`; `MUTATE`; Task 1; `GATE`; `CONSUMERS`; `E2E` | Covered |
| 27 | The integration-HEAD mutation path owns stale-completion invalidation. | Required Property 17 | `MUTATE`; `E2E` | Covered |
| 28 | Finalization preserves ADR-0112 lock order and performs no blackboard write under the mutation lock. | Required Property 18 | `MUTATE`; Task 1; Task 2; `E2E` | Covered |
| 29 | The global generation limit is configurable with a deterministic default. | Required Property 19 | `CFG`; `PROGRESS`; Task 2; `E2E` | Covered |
| 30 | Wake/restart recovery cannot duplicate slice or global analyses. | Required Property 20; Success Criterion 10 | `PERSIST`; `PROGRESS`; Task 2; `CONSUMERS`; `E2E` | Covered |
| 31 | Workflow remains stack-agnostic and preserves review/merge authorization. | Required Property 21 | `CFG`; `TOPO`; Task 1; Task 2; `E2E` | Covered |
| 32 | No coverage begins while any planning/output/transition/coding prerequisite is unsettled. | Success Criterion 1 | `PROGRESS`; Task 2; `E2E` | Covered |
| 33 | Cohort classification is reproducible with no slices below two contributing scopes. | Success Criterion 2 | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 34 | Every multi-scope cohort member has an attestation or exactly one required slice. | Success Criterion 3 | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 35 | Global analysis is unclaimable behind every local barrier. | Success Criterion 4 | `PROGRESS`; Task 2; `CONSUMERS`; `E2E` | Covered |
| 36 | Every slice records a bounded surface and immutable snapshot. | Success Criterion 5 | `PERSIST`; Task 1; Task 2; `CONTEXT`; `E2E` | Covered |
| 37 | Global analysis independently reviews the aggregate after local coverage resolves. | Success Criterion 6 | `PROGRESS`; Task 2; `CONTEXT`; `E2E` | Covered |
| 38 | Later mutations rescan within budget and block explicitly after exhaustion. | Success Criterion 9 | `CFG`; `PROGRESS`; `MUTATE`; Task 1; Task 2; `CONSUMERS`; `E2E` | Covered |
| 39 | No master-planning change, fix-review replacement, global-analysis removal, stack-specific default, or new role is introduced. | Out of Scope | `TOPO`; Task 1; Task 2 | Covered |
| 40 | ADR-0113 extends ADR-0055 and supersedes its no-rescan limitation. | Documentation Impact 1-2 | `DOC` | Covered |
| 41 | State-machine, task-lifecycle, pipeline, operational, configuration, and terminal-outcome docs are updated. | Documentation Impact 3-4 | `DOC` | Covered |
| 42 | Integration-closure issue changes only after implementation and validation evidence exists. | Documentation Impact 5 | `E2E`; `DOC` | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | `E2E` (`code-planning-main-1-code-planning-9`): separate retained task | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | `DOC` (`code-planning-main-1-code-planning-10`): separate retained task | Covered |

## Pre-submit audit

- Task 1 and Task 2 each have one observable behavior intent with colocated tests; Task 2 depends on Task 1 for the shared helper and aggregate public flow.
- Candidate `ValidateState` and before/after transition validation are both required at each public writer, with separate negative tests and whole-transaction rollback assertions.
- Every created task and its deduplicated `Sprint.Scope.Planned` membership are one atomic transaction, and restart/concurrency tests assert exact-once membership.
- Slice and global `ParentTasks` are exact, ordered, and justified; `DependsOn` remains intentionally empty.
- Owned files are disjoint and the dependency chain is acyclic; no shared-file edge is missing.
- Every lifecycle write consumes dependency-owned validation; no schema, progress, capability, mutation, prompt, or completion policy is re-owned.
- Validation commands are single-purpose, worktree-relative, exact-pass checks that reject all Go failure events.
- The complete goal matrix has no GAP; end-to-end and documentation remain explicit retained tasks.
