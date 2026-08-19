# Code Plan: Reconcile Sliced Integration Analyses

## Intent and evidence

Implement the state-changing half of sliced integration as two ordered, cohesive changes: first make final integration-review approval project immutable lifecycle evidence through public `SubmitVerdict`; then make public `ReconcileIntegrationAnalyses` persist only the cohort, coverage, analysis tasks, and blocked/exhausted closure requested by the authoritative `EvaluateIntegrationProgress` decision.

Success means verdict projection and reconciliation both execute in atomic blackboard transactions, both call `statevalidate.ValidateIntegrationLifecycleTransition` against an unaliased pre-mutation snapshot before persistence, repeated wake or restart calls cannot duplicate or replace immutable evidence, and no code in this scope re-derives progress policy, renders prompts, advances the integration ref, or declares sprint completion.

Based on: the full goal spec at `specs/goals/20260818-sliced-integration-analysis.md`; the authoritative master replan `specs/plans/20260818-sliced-integration-analysis/20260819-084641-code-planning-main-1-replan-1.md`; the merged persistence plan `20260819-111435-code-planning-main-1-replan-1-code-planning-0.md`; the merged mutation plan `20260819-122954-code-planning-main-1-replan-1-code-planning-1.md`; the generation, topology, and progress plans `20260819-075818-code-planning-main-1-code-planning-1.md`, `20260819-073455-code-planning-main-1-code-planning-2.md`, and `20260819-115431-code-planning-main-1-code-planning-3.md`; pending source in `internal/models/integration.go`, `internal/statevalidate/integration.go`, `internal/ops/submit_verdict.go`, `internal/ops/proceed.go`, `internal/db/blackboard.go`, `internal/git/merge.go`, and `internal/git/query.go`; ADR-0055 and ADR-0112; `INVARIANTS.md` §§5-7 and Protection Matrix; and the Update Policy plus relevant integration-closure and decomposition-cascade entries in `specs/architecture/architectural-issues.md`.

Doc Impact: this planning task changes only this plan and its structured output. Goal-level documentation remains owned by `code-planning-main-1-code-planning-10` after implementation and end-to-end evidence exist.

Test Impact: Task 1 adds focused public-boundary coverage in `internal/ops/submit_verdict_test.go`; Task 2 adds the required aggregate reconciliation test in `internal/ops/integration_reconcile_test.go`. Goal-level end-to-end and controlled-race coverage remains owned by `code-planning-main-1-code-planning-9`.

## Current task routing

`Task 1` and `Task 2` are this plan's outputs. Other aliases are existing concrete tasks and are not work added to this scope.

| Alias | Concrete task ID | Responsibility |
|---|---|---|
| `PERSIST` | `code-planning-main-1-replan-1-code-planning-0` | Lifecycle schema and shared structural/transition validation |
| `CFG` | `code-planning-main-1-code-planning-1` | Configurable global-generation ceiling and deterministic default |
| `TOPO` | `code-planning-main-1-code-planning-2` | Slice/global role-pair topology and frozen-pipeline capability |
| `PROGRESS` | `code-planning-main-1-code-planning-3` | Pure cohort, barrier, request, exhaustion, and effective-completion decision |
| `MUTATE` | `code-planning-main-1-replan-1-code-planning-1` | Mutation receipts and clean-source verification under ADR-0112 ordering |
| Task 1 / `PROJECT` | this plan output 0 | Immutable analysis-verdict projection in `SubmitVerdict` |
| Task 2 / `RECONCILE` | this plan output 1 | Atomic idempotent cohort/task/closure reconciliation |
| `CONTEXT` | `code-planning-main-1-code-planning-6` | Bounded slice and independent aggregate prompt context |
| `GATE` | `code-planning-main-1-code-planning-7` | State-changing effective-completion barriers |
| `CONSUMERS` | `code-planning-main-1-code-planning-8` | Wake, supervisor, status, and terminal consumers |
| `E2E` | `code-planning-main-1-code-planning-9` | End-to-end lifecycle, restart, rescan, exhaustion, and race evidence |
| `DOC` | `code-planning-main-1-code-planning-10` | ADR-0113, lifecycle/operator docs, and issue disposition |

The parent planning task already depends on `PERSIST`, `MUTATE`, `CFG`, `TOPO`, and `PROGRESS`. The code-plan transition propagates their generated coding-task dependencies to both outputs. Task 2 additionally depends on Task 1 because its aggregate public-flow test consumes verdict projection.

## Architecture and ownership boundary

```text
frozen pipeline capability + live integration HEAD + current state
                              |
                              v
                  EvaluateIntegrationProgress
                              |
                  immutable decision requests
                              |
                              v
              ReconcileIntegrationAnalyses (Task 2)
                 | freeze cohort + attestations
                 | create only missing analysis tasks
                 ` project blocked/exhausted closure
                              |
                        analyst/reviewer pair
                              |
                              v
                    SubmitVerdict (Task 1)
                 | append slice/global verdict evidence
                 | project clean closure only from verified source
                 ` preserve prior evidence
                              |
                              v
          ValidateIntegrationLifecycleTransition -> persistence
```

`PERSIST` remains the sole schema and invariant owner. `PROGRESS` remains the sole owner of eligibility, lineage resolution, barrier, next-generation, and exhaustion policy. `TOPO` remains the role-pair capability owner. `MUTATE` remains the only integration-ref writer and supplies lock-ordered clean-source verification. This plan owns only translation from those decisions and reviewed task facts into one atomic blackboard mutation.

The change intersects the concurrency, review, and integration rows in the Protection Matrix. Preserve `db.Blackboard.Modify` as the state-atomicity boundary, existing pipeline-resolved review transitions and quorum, and ADR-0112's integration-mutation-lock-before-blackboard-read order. Neither task writes blackboard state while holding the integration mutation lock.

## Shared transition-validation protocol

Task 1 adds a narrow same-package helper used by both public writers. At the start of each `db.Blackboard.Modify` callback, capture an unaliased snapshot of the prior `Goal.Integration` value and every existing task's `IntegrationAnalysis` metadata. Build the candidate by copy-on-write or an equivalent deterministic deep copy so later slice or pointer mutation cannot alter the snapshot. Immediately before returning success from the callback, call `statevalidate.ValidateIntegrationLifecycleTransition(previous, candidate)`.

The helper is not a second validator: it only obtains an alias-safe before image and invokes the `PERSIST` interface. Any validation error aborts the callback, so status transitions, reviewer release, lifecycle evidence, cohort creation, and task creation roll back together. A narrow package test hook may corrupt the candidate immediately before validation to prove the public paths cannot bypass the invariant; restore every hook with `t.Cleanup`.

## Task 1 design: project reviewed verdicts

Extend only the final-quorum `APPROVED` branch of `SubmitVerdict`. Ordinary coding/planning verdict behavior remains unchanged. When the reviewed task has `IntegrationAnalysis` metadata and its role pair matches the metadata phase:

1. Derive the immutable analysis verdict from the already-reviewed artifact: empty `Output` is `clean`; non-empty `Output` is `findings`. Do not parse finding text or re-evaluate progress.
2. Use `IntegrationAnalysisMetadata.SourceCommit` as the analyzed source and `Task.ReviewCommit` as the analyst report commit. Never substitute one for the other.
3. For a slice analysis, append exactly one `IntegrationCoverageRecord` of kind `slice_report`, bound to the metadata key, originating plan, exact frozen roots, source commit, report commit, and verdict. Findings remain valid slice evidence; `PROGRESS` determines whether their repair or replacement lineage has resolved.
4. For a global analysis, append exactly one contiguous `IntegrationGlobalGeneration` from metadata. A findings verdict does not claim clean closure. A clean verdict may project `IntegrationClosureStatusClean` only after `MUTATE`'s clean-source verifier has checked the metadata source under the integration mutation lock and released that lock.
5. Call clean-source verification before entering the verdict `Modify` callback. A later ref mutation may make newly persisted clean evidence stale, but `PROGRESS` compares it with live HEAD and therefore makes it ineffective immediately; do not hold the mutation lock across the state write and do not erase stale evidence.
6. Reject phase/role-pair mismatch, missing metadata, duplicate evidence, non-contiguous generation, source mismatch, or any attempted rewrite before persistence. Only project after final quorum; partial approval records no lifecycle evidence.

Keep the existing approval/rejection history, impact escalation, quorum, reviewer release, clean-vs-approved routing, and limit escalation in the same transaction. If rejection exhaustion blocks a slice/global analysis, leave immutable verdict evidence absent; `PROGRESS` reports the blocked barrier and Task 2 projects the explicit lifecycle closure.

### Task 1 test design

Add `TestSubmitVerdictIntegrationLifecycleProjection` with deterministic named subtests using public `SubmitVerdict`:

- final-quorum clean and findings slice approvals append one correctly tagged report with distinct source/report commits, while partial approval appends nothing;
- global findings append one generation without clean closure, and global clean binds closure generation/key/source to the verifier-confirmed source;
- existing coverage/generations/task metadata remain unchanged prefixes;
- role/phase mismatch and duplicate projection fail closed;
- the pre-validation hook attempts to rewrite prior coverage or analysis metadata, the shared validator error escapes, and the full verdict transaction is absent afterward;
- an ordinary non-integration approval follows the existing path unchanged.

Use channels and bounded failure timeouts for any clean-source ordering seam; never use sleeps or retry-until-green behavior.

Validation: `go test -json ./internal/ops -run '^TestSubmitVerdictIntegrationLifecycleProjection$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestSubmitVerdictIntegrationLifecycleProjection") and all(.[]; .Action != "fail")'`

## Task 2 design: reconcile authoritative requests

Add public `ReconcileIntegrationAnalyses(projectRoot string)` with a result that reports whether state changed, created task IDs in deterministic order, and any projected blocked/exhausted reason. It performs no terminal I/O.

Within one `db.Blackboard.Modify` callback:

1. Load the frozen pipeline resolver and `SlicedIntegrationCapability`, read the integration HEAD, and call `EvaluateIntegrationProgress` against the callback's current state. Treat the decision as authoritative; do not duplicate its cohort, lineage, barrier, generation, or exhaustion predicates.
2. If requested, freeze the decision's canonical contributing set exactly once and append its decision-provided one-lineage approval attestations. Existing cohort or coverage entries are never replaced.
3. Materialize every missing slice/global request in sorted key order in the same callback. Use deterministic task IDs derived from the immutable key (`integration-` plus the key with `:` replaced by `-`); an exact existing task is an idempotent no-op and any ID/key/metadata collision is an error.
4. Resolve the role-pair initial status and task type from the frozen pipeline. Slice tasks use `slice-integration-pair`; global tasks use `integration-pair`; both use `TaskTypeIntegration` and existing analyst/reviewer claim, review, fix-transition, and merge authorization paths.
5. Attach immutable `IntegrationAnalysisMetadata` at creation. Slice metadata includes the decision key, originating plan, exact sorted roots, sorted descendant task/merge-commit attribution, analyzed HEAD, sorted affected paths from each descendant's reviewed change range, and the subset of those paths present as regular files in that source tree. Global metadata includes key, positive generation, and analyzed HEAD; `CONTEXT` owns aggregate rendering.
6. Preserve source refs needed by `CONTEXT`: slice tasks inherit the originating plan's normalized spec/plan/architecture references; global tasks use the goal spec. Parent provenance may identify the slice roots or the covered analysis sources, but `DependsOn` remains empty because clean integration terminal states are not `MERGED`; claimability comes from `PROGRESS` issuing a request only after every barrier settles.
7. If the decision is waiting, make no state change. If it is blocked or exhausted, project the exact machine-readable decision reason as `IntegrationClosureStatusBlocked` or `IntegrationClosureStatusExhausted`; do not invent a second reason or decide sprint completion.
8. Run shared transition validation after all candidate mutations and before persistence. Any Git snapshot, pipeline, decision, collision, or validation failure aborts the entire callback, leaving neither a frozen cohort nor a partial task batch.

Repeated wake calls, a new `Blackboard` instance simulating restart, and concurrent callers all converge because decision keys, task IDs, cohort, and append-only evidence are deterministic and the read/decision/write sequence occurs under the blackboard lock.

### Task 2 test design

Add one top-level `TestReconcileIntegrationAnalyses` with named subtests that exercise public `ReconcileIntegrationAnalyses` and, for end-to-end projection within this package boundary, public `SubmitVerdict`:

1. An unsettled planning/output/transition/coding boundary is a no-op. At the settled boundary, one atomic call freezes the canonical cohort, appends one-lineage attestations, and creates every missing multi-lineage slice with exact source metadata.
2. A forced failure after cohort preparation but before validation persists neither cohort nor tasks. A hook that attempts frozen-cohort replacement is rejected by the shared transition validator.
3. Repeating the call, recreating the blackboard object, permuting task order, and invoking concurrent callers produces the same keys/IDs and exactly one task per request.
4. Fewer than two contributing scopes create no slices; missing sliced capability with a required slice projects the decision's blocked reason and creates no analysis task.
5. Slice clean/findings verdicts project immutable coverage. Pending, blocked, abandoned, superseded, and replacement repair lineages are left to `PROGRESS`; global creation waits until the decision reports those lineages resolved and every required slice covered.
6. The first global findings verdict appends generation 1. Reconciliation waits while repair/replacement work is unresolved, then creates exactly generation 2 at the current HEAD; later calls do not duplicate either generation.
7. A clean global verdict uses the verified metadata source/report commits, and a changed live HEAD makes it ineffective and requests the next generation while budget remains.
8. Slice/review exhaustion and normalized global-generation exhaustion project explicit blocked/exhausted closure with no extra task.
9. Public `SubmitVerdict` candidate corruption proves prior projected coverage cannot be rewritten and the rejected transaction leaves status/evidence unchanged.

The canonical validation must observe the exact top-level pass and reject every Go failure event:

Validation: `go test -json ./internal/ops -run '^TestReconcileIntegrationAnalyses$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestReconcileIntegrationAnalyses") and all(.[]; .Action != "fail")'`

## Planned coding tasks

### Task 1 — Project immutable integration verdict evidence

Description: Project approved slice and global analysis verdicts into immutable lifecycle evidence at `SubmitVerdict`'s atomic persistence boundary.

Done when: `TestSubmitVerdictIntegrationLifecycleProjection` proves final-quorum slice and global approvals append immutable evidence with distinct analyzed-source and analyst-report commits; partial approval and ordinary non-integration verdicts preserve existing behavior; global clean projection consumes lock-ordered clean-source verification; and public `SubmitVerdict` invokes the shared lifecycle transition validator so duplicate projection or attempted rewrite fails before any verdict state persists.

Scope: Own `internal/ops/submit_verdict.go` and `internal/ops/submit_verdict_test.go`. Add the alias-safe lifecycle transition-validation call and verdict projection inside the existing atomic review boundary; preserve quorum, history, impact, rejection escalation, reviewer release, pipeline-resolved statuses, and authorization. Do not create analysis tasks, derive progress, render context, mutate the integration ref, or decide sprint completion.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Dependencies: inherited `PERSIST`, `MUTATE`, `CFG`, `TOPO`, and `PROGRESS` implementation dependencies from the parent planning graph.

Validation: `go test -json ./internal/ops -run '^TestSubmitVerdictIntegrationLifecycleProjection$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestSubmitVerdictIntegrationLifecycleProjection") and all(.[]; .Action != "fail")'`

### Task 2 — Reconcile deterministic analysis lifecycle work

Description: Materialize exactly the missing slice and global analysis work and closure projection requested by `EvaluateIntegrationProgress`.

Done when: `TestReconcileIntegrationAnalyses` proves cohort freezing, one-lineage attestations, immutable slice source snapshots, and missing-task creation are one atomic idempotent transaction across repeated wake, restart, task-order permutation, and concurrent calls; public reconciliation rejects replacement of a frozen cohort through the shared lifecycle transition validator; global creation waits for resolved repair or replacement lineage; later current-HEAD generations are unique and bounded; slice/capability/review or generation exhaustion records an explicit blocked/exhausted closure; and the aggregate flow proves public `SubmitVerdict` cannot rewrite projected coverage.

Scope: Own `internal/ops/integration_reconcile.go` and `internal/ops/integration_reconcile_test.go`. Consume frozen-pipeline capability, authoritative progress decisions, persisted lifecycle types, shared transition validation, and existing Git read helpers to create initial slice/global analysis tasks and closure projection only. Do not modify verdict implementation, pipeline topology, progress policy, prompts, completion consumers, the integration ref, or sprint completion.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#global-integration`

Dependencies: Task 1 plus inherited `PERSIST`, `MUTATE`, `CFG`, `TOPO`, and `PROGRESS` implementation dependencies from the parent planning graph.

Validation: `go test -json ./internal/ops -run '^TestReconcileIntegrationAnalyses$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestReconcileIntegrationAnalyses") and all(.[]; .Action != "fail")'`

## Architecture review

### Discovery

```text
models/statevalidate -> pure progress decision -> reconciliation writer
          |                                         |
          +---------------- verdict writer <--------+
                                   |
                         existing review/fix pipeline
```

The stable boundaries are typed durable facts, shared transition validation, frozen pipeline semantics, and the pure progress decision. The volatile work is translating a request into task fields and translating a reviewed artifact into evidence. Current `SubmitVerdict` already owns one atomic review transaction; `db.Blackboard.Modify` already supplies cross-process serialization; `proceed.go` demonstrates pipeline-resolved task construction; and Git exposes exact changed-path/tree-presence reads. No new store, role, pipeline, generic event abstraction, or alternate lifecycle machine is required.

### Analysis and recommendation

The primary risks are non-idempotent task materialization, aliased before/after snapshots that make transition validation vacuous, and a lock-order inversion during clean projection. The two-task order localizes those risks: Task 1 establishes one validated verdict boundary and reusable snapshot discipline; Task 2 consumes it while keeping reconciliation policy-free. The cost of being wrong is durable duplicate or stale success, so public-boundary negative tests and atomic rollback are load-bearing. Keeping `DependsOn` empty on analysis tasks avoids coupling claimability to clean terminal states that the generic dependency resolver does not treat as `MERGED`.

No new architectural issue is introduced by the plan. The existing integration-closure issue remains open until `E2E` validation exists, as required by its lifecycle policy.

## Spec Compliance Matrix

This matrix covers the complete goal. Task 1 and Task 2 are credited only for this plan's projection/reconciliation boundary; retained aliases identify external owners without expanding this scope.

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Bounded per-scope coverage precedes repeated global closure analysis. | Objective | `PROGRESS`; Task 1; Task 2; `CONTEXT`; `E2E` | Covered |
| 2 | Single-lineage coverage records task, criteria, reviewed commit, approver, validation, and merge commit. | Proposed Model | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 3 | Contributing scopes and distinct root coding lineages are reproducible. | Slice Integration | `PROGRESS`; Task 2; `E2E` | Covered |
| 4 | Planning settles only after coding-producing sources, outputs, transitions, and resulting coding work settle. | Slice Integration | `PROGRESS`; Task 2; `CONSUMERS`; `E2E` | Covered |
| 5 | Partial planning handoff cannot open coverage. | Required Properties, bullet 1 | `PROGRESS`; Task 2; `E2E` | Covered |
| 6 | The contributing set freezes exactly once after the settled boundary. | Required Properties, bullet 2 | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 7 | Fewer than two contributing scopes produce no slice. | Required Properties, bullet 3 | `PROGRESS`; Task 2; `E2E` | Covered |
| 8 | Multiple contributing scopes each yield bounded coverage. | Required Properties, bullet 4 | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 9 | One-lineage scopes reuse approval attestations without a slice. | Required Properties, bullet 5 | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 10 | Multi-lineage scopes with merged work produce exactly one slice. | Required Properties, bullet 6 | `TOPO`; `PROGRESS`; Task 2; `E2E` | Covered |
| 11 | Escalation plans remain repair lineage outside the contributing set. | Required Properties, bullet 7 | `PROGRESS`; Task 2; `E2E` | Covered |
| 12 | Lineage attributes coding, fixes, and replacements to a slice. | Required Properties, bullet 8 | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 13 | Slice context is bounded to plan, descendants, commits, paths, metadata, and source snapshot. | Required Properties, bullet 9 | `PERSIST`; Task 2; `CONTEXT`; `E2E` | Covered |
| 14 | Slice verdicts persist analyzed descendant changes and immutable source snapshot. | Required Properties, bullet 10 | `PERSIST`; Task 1; Task 2; `E2E` | Covered |
| 15 | Global analysis waits for planning, coding, repair, required-slice, and resolution barriers. | Required Properties, bullet 11 | `PROGRESS`; Task 2; `GATE`; `CONSUMERS`; `E2E` | Covered |
| 16 | Global analysis independently inspects the aggregate branch. | Required Properties, bullet 12 | Task 2; `CONTEXT`; `E2E` | Covered |
| 17 | Global fixes and later integration-HEAD mutations trigger another scan while budget remains. | Required Properties, bullet 13 | `CFG`; `PROGRESS`; `MUTATE`; Task 1; Task 2; `CONSUMERS`; `E2E` | Covered |
| 18 | Slice or global-generation exhaustion produces an explicit blocked outcome. | Required Properties, bullet 14 | `PERSIST`; `PROGRESS`; Task 2; `CONSUMERS`; `E2E` | Covered |
| 19 | Clean completion is tied to an immutable reviewed commit. | Required Properties, bullet 15 | `PERSIST`; `PROGRESS`; `MUTATE`; Task 1; `E2E` | Covered |
| 20 | Completion state, clean reviewed commit, and integration HEAD have one linearizable relationship. | Required Properties, bullet 16 | `PROGRESS`; `MUTATE`; Task 1; `GATE`; `CONSUMERS`; `E2E` | Covered |
| 21 | The integration-HEAD mutation path owns invalidation. | Required Properties, bullet 17 | `MUTATE`; `E2E` | Covered |
| 22 | Finalization preserves ADR-0112 lock order. | Required Properties, bullet 18 | `MUTATE`; Task 1; Task 2; `E2E` | Covered |
| 23 | The global generation limit is configurable with a deterministic default. | Required Properties, bullet 19 | `CFG`; `PROGRESS`; Task 2; `E2E` | Covered |
| 24 | Wake/restart recovery cannot duplicate slice or global analyses. | Required Properties, bullet 20 | `PERSIST`; `PROGRESS`; Task 2; `CONSUMERS`; `E2E` | Covered |
| 25 | Workflow remains stack-agnostic and preserves review/merge authorization. | Required Properties, bullet 21 | `CFG`; `TOPO`; Task 1; Task 2; `E2E` | Covered |
| 26 | No coverage begins while any planning/output/transition/coding prerequisite is unsettled. | Success Criterion 1 | `PROGRESS`; Task 2; `E2E` | Covered |
| 27 | Cohort classification is reproducible with no slices below two contributing scopes. | Success Criterion 2 | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 28 | Every multi-scope cohort member has an attestation or exactly one required slice. | Success Criterion 3 | `PERSIST`; `PROGRESS`; Task 2; `E2E` | Covered |
| 29 | Global analysis is unclaimable behind every local barrier. | Success Criterion 4 | `PROGRESS`; Task 2; `CONSUMERS`; `E2E` | Covered |
| 30 | Every slice records a bounded surface and immutable snapshot. | Success Criterion 5 | `PERSIST`; Task 1; Task 2; `CONTEXT`; `E2E` | Covered |
| 31 | Global analysis independently reviews the aggregate after local coverage resolves. | Success Criterion 6 | `PROGRESS`; Task 2; `CONTEXT`; `E2E` | Covered |
| 32 | Successful integration linearizes only when clean source equals integration HEAD. | Success Criterion 7 | `PERSIST`; `PROGRESS`; `MUTATE`; Task 1; `GATE`; `CONSUMERS`; `E2E` | Covered |
| 33 | Controlled concurrency proves both mutation/finalization orders reject durable stale success. | Success Criterion 8 | `MUTATE`; Task 1; `GATE`; `CONSUMERS`; `E2E` | Covered |
| 34 | Later mutations rescan within budget and block explicitly after exhaustion. | Success Criterion 9 | `CFG`; `PROGRESS`; `MUTATE`; Task 1; Task 2; `CONSUMERS`; `E2E` | Covered |
| 35 | Repeated wake and restart evaluation remains duplicate-free. | Success Criterion 10 | `PROGRESS`; Task 2; `CONSUMERS`; `E2E` | Covered |
| 36 | No master-planning change, fix-review replacement, global-analysis removal, stack-specific default, or new role is introduced. | Out of Scope | `TOPO`; Task 1; Task 2 | Covered |
| 37 | ADR-0113 extends ADR-0055 with sliced analysis and final closure. | Documentation Impact, bullet 1 | `DOC` | Covered |
| 38 | ADR-0113 supersedes ADR-0055's no-rescan limitation. | Documentation Impact, bullet 2 | `DOC` | Covered |
| 39 | State-machine and task-lifecycle documentation is updated. | Documentation Impact, bullet 3 | `DOC` | Covered |
| 40 | Pipeline, operational, and configuration documentation covers barriers, generations, and terminal outcomes. | Documentation Impact, bullet 4 | `DOC` | Covered |
| 41 | Integration-closure issue changes only after implementation and validation evidence exists. | Documentation Impact, bullet 5 | `E2E`; `DOC` | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | `E2E` (`code-planning-main-1-code-planning-9`): separate retained task | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | `DOC` (`code-planning-main-1-code-planning-10`): separate retained task | Covered |

## Pre-submit audit

- Task 1 and Task 2 each have one observable behavior intent with colocated tests; Task 2 depends on Task 1 for the aggregate public flow.
- Owned files are disjoint; no shared-file dependency is missing.
- Every lifecycle write uses the shared validator; no schema, progress, capability, or mutation policy is re-owned here.
- Task 1 preserves existing review authorization and Task 2 creates tasks only in pipeline-resolved initial states.
- Validation commands are single-purpose, worktree-relative, exact-pass checks with all failure events rejected.
- The complete goal matrix has no GAP; end-to-end and documentation remain explicit retained tasks.
