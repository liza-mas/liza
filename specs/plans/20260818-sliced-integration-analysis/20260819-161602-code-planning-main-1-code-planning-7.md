# Code Plan: Gate Effective Integration Completion Before Sprint Progression

## Intent and evidence

Route every state-changing path that can claim or cross the settled integration boundary through one shared effective-completion precondition. Preserve earlier phase handoffs while the contributing cohort is still unsettled, but reject an explicit `SPRINT_COMPLETE` checkpoint, checkpoint-to-completed resume, completed-sprint archive, direct sprint advance, or manual proceed once authoritative integration progress is not effectively complete.

Success means `TestEffectiveIntegrationCompletionGate` proves every gated public path rejects stale clean evidence and a pending replacement global generation without applying its progression mutation; once the contributing cohort is frozen, those paths succeed only for clean evidence bound to live integration HEAD; phase-handoff checkpoint, resume, advance, and proceed behavior remains available before the cohort freezes; and a public integration mutation followed immediately by resume or advance cannot complete or archive the stale sprint.

Based on:

- The full goal specification at `specs/goals/20260818-sliced-integration-analysis.md`, especially Final Closure, Required Properties 11 and 13-18, and Success Criteria 4 and 7-9.
- The retained master plan at `specs/plans/20260818-sliced-integration-analysis/20260819-005612-code-planning-main-1.md` and replacement master plan at `specs/plans/20260818-sliced-integration-analysis/20260819-084641-code-planning-main-1-replan-1.md`.
- The corrected persistence/progress plan at `specs/plans/20260818-sliced-integration-analysis/20260819-152852-code-planning-main-1-replan-1-code-planning-3.md`, mutation plan at `specs/plans/20260818-sliced-integration-analysis/20260819-122954-code-planning-main-1-replan-1-code-planning-1.md`, and reconciliation plan at `specs/plans/20260818-sliced-integration-analysis/20260819-134730-code-planning-main-1-replan-1-code-planning-2.md`.
- Targeted authoritative task reads of the concrete `PROGRESS`, `MUTATE`, and `RECONCILE` coding providers.
- Stacklit orientation for `internal/ops`, Semble discovery of the resume/archive paths, and direct source reads of the assigned checkpoint, terminal detection, resume, advance, and proceed implementations and their focused tests.
- ADR-0112; `INVARIANTS.md` §§3.3-3.4, 5, 7-8, 12, 15, and the Protection Matrix; and the integration-closure, sprint-completion, manual-transition, and `proceed.go` entries in `specs/architecture/architectural-issues.md`.

Load-bearing claims:

- **EVIDENCED — settled-boundary identity:** the corrected persistence contract makes `IntegrationLifecycle.ContributingSet == nil` mean pre-integration planning is not frozen and a non-nil empty set mean a settled zero-scope cohort. The gate must test that durable distinction, not lifecycle pointer presence or task terminality.
- **EVIDENCED — phase-handoff compatibility:** public `Proceed`, completed-sprint resume, and direct advance also carry planning output into later phases. Blocking them before the cohort freezes would deadlock the existing manual `epic-to-us`, `us-to-coding`, `architecture-to-code-plan`, and `code-plan-to-coding` transitions.
- **EVIDENCED — policy ownership:** `PROGRESS` alone decides effective completion and blocked/exhausted/waiting reasons; `RECONCILE` alone materializes requested analysis work; `MUTATE` alone supplies lock-ordered live-HEAD verification and mutation invalidation. Task 1 composes those interfaces and does not recreate their predicates.
- **EVIDENCED — zero-scope closure:** after reconciliation freezes a non-nil empty cohort, the gate is active. Fewer than two scopes bypass slice analysis only; they do not bypass global clean evidence.
- **EVIDENCED — side-effect boundary:** `SprintCheckpoint` writes a summary before its blackboard mutation, `Resume` and `AdvanceSprint` write sprint archives before advancing state, and `Proceed` creates children inside its state transaction. Effective-completion rejection must occur before those progression-specific file or state changes.
- **EVIDENCED — dependency claimability:** direct dependencies on the concrete `PROGRESS`, `MUTATE`, and `RECONCILE` children keep this implementation unclaimable until all consumed interfaces are merged; `RECONCILE` transitively supplies verdict projection and persistence validation.

Doc Impact: only this planning artifact and its structured task output. Product, protocol, invariant, and architecture documentation remains owned by `code-planning-main-1-code-planning-10` after end-to-end evidence exists.

Test Impact: Task 1 adds the required aggregate top-level test in the owned `internal/ops` test surface and adjusts only affected path-specific fixtures/assertions. Goal-level end-to-end and controlled-concurrency proof remains owned by `code-planning-main-1-code-planning-9`.

## Current task routing

| Alias | Concrete task ID | Responsibility |
|---|---|---|
| `CFG` | `code-planning-main-1-code-planning-1` | Configurable global-generation ceiling and deterministic default |
| `TOPO` | `code-planning-main-1-code-planning-2` | Slice/global role-pair topology and frozen-pipeline capability |
| `PERSIST-PATCH` | `code-planning-main-1-replan-1-code-planning-3-coding-0` | Durable nil/non-nil cohort identity and bounded approval evidence |
| `PROGRESS` | `code-planning-main-1-replan-1-code-planning-3-coding-1` | Pure authoritative integration progress and effective-completion decision |
| `MUTATE` | `code-planning-main-1-replan-1-code-planning-1-coding-0` | Integration-ref invalidation and lock-ordered clean-source verification |
| `PROJECT` | `code-planning-main-1-replan-1-code-planning-2-coding-0` | Immutable verdict projection and shared lifecycle write validation |
| `RECONCILE` | `code-planning-main-1-replan-1-code-planning-2-coding-1` | Idempotent decision projection, analysis creation, and blocked/exhausted closure |
| Task 1 / `GATE` | output 0 | Shared state-changing checkpoint, resume/archive, advance, and proceed precondition |
| `CONTEXT` | `code-planning-main-1-code-planning-6-coding-0` | Bounded slice and independent aggregate analysis context |
| `CONSUMERS` | `code-planning-main-1-code-planning-8` | Wake, supervisor, status, and non-mutating terminal consumers |
| `E2E` | `code-planning-main-1-code-planning-9` | End-to-end lifecycle, restart, exhaustion, and controlled race evidence |
| `DOC` | `code-planning-main-1-code-planning-10` | ADR, lifecycle/operator documentation, and issue disposition |

## Architecture and ownership boundary

```text
public checkpoint / resume / advance / proceed
                         |
                         v
             ReconcileIntegrationAnalyses
              (project requested work first)
                         |
                         v
       MUTATE lock-ordered fresh progress verification
                         |
                         v
          EvaluateIntegrationProgress decision
                /                       \
 cohort still nil                 cohort non-nil
 phase handoff may continue        require effective complete
 explicit completion rejects       otherwise reject fail-closed
                \                       /
                         v
       existing path-specific state/file mutation
```

Task 1 owns only composition at the progression boundary. `PERSIST-PATCH` owns the settled marker, `PROGRESS` owns policy, `MUTATE` owns the integration lock and live HEAD observation, and `RECONCILE` owns lifecycle/task writes. No schema, generation rule, Git mutation, analysis task, prompt, wake/status presentation, or documentation behavior moves into this task.

The shared precondition is intentionally narrow:

1. Reconcile authoritative progress before finality is evaluated. If reconciliation requests or observes a replacement global generation, that work becomes durable before the progression attempt is rejected.
2. Obtain a fresh decision through `MUTATE`'s lock-ordered clean-source verification protocol. Do not read live HEAD independently, hold the integration mutation lock across a blackboard write, or write state from the verifier.
3. If the contributing set remains nil, permit ordinary phase-handoff progression but reject an explicit `SPRINT_COMPLETE` claim because the pre-integration boundary is unsettled.
4. If the contributing set is non-nil, including the settled empty cohort, require the decision's effective-completion result. Waiting, stale, blocked, exhausted, malformed, or pending-generation decisions reject with a `PreconditionError` carrying the dependency-owned stable reason/context.
5. Execute the existing path mutation only after the precondition passes. Re-check every existing status, terminality, transition, and crash-recovery condition in its current atomic boundary; the new gate supplements those checks and does not replace them.
6. Propagate reconciliation, capability, Git, evaluator, or verification errors fail-closed. Never translate an unavailable decision into terminal-only fallback behavior.

This ordering preserves ADR-0112: integration mutation lock, then blackboard read, release the integration lock, then any later blackboard write. Task 1 adds no lock and never clears stale evidence; a later mutation remains immediately ineffective through `PROGRESS` and `MUTATE`.

## Path integration

| Path | Gate point | Preserved behavior | Rejected progression side effects |
|---|---|---|---|
| `SprintCheckpoint` | Before summary-file generation when `trigger == SPRINT_COMPLETE` | Manual hard checkpoints and planning/many-to-one transition checkpoints remain available | No summary overwrite, checkpoint timestamp, trigger, or status change |
| `Resume` from ordinary `CHECKPOINT` | Only when the atomic resume branch would mark the sprint `COMPLETED`; transition checkpoints and non-terminal mid-sprint resume remain unchanged | Pre-integration phase completion remains available while the cohort is nil | Sprint remains `CHECKPOINT`; no terminal trigger clearing |
| `Resume` from `COMPLETED` | Before archive planning or archive-file write | Pre-integration completed sprint may still advance to carry newly created downstream tasks while the cohort is nil | No archive file, sprint history, new sprint, or post-advance transition execution |
| `AdvanceSprint` | Before archive planning or archive-file write | Existing pre-integration carry-forward of unconsumed planning output remains available while the cohort is nil | No archive file, sprint history, or new sprint |
| `Proceed` | After transition resolution but before its blackboard mutation | Manual planning transitions remain available while the cohort is nil | No transition marker, child task, dependency rewrite, or sprint-scope append |

For `Resume`, perform a read-only preflight to determine whether the current branch can claim completion, then retain a fail-closed guard inside the existing `Modify`: if concurrent state changes would make the callback enter a completion branch without a successful precondition, abort instead of completing. Do not move archive or transition mutation outside their current atomic state boundary.

## Test design

Add one top-level `TestEffectiveIntegrationCompletionGate` with named, table-driven subtests. Keep fixtures in the owned test package and use real temporary Git refs for live-HEAD assertions. Use the public operations under test and the public mutation path for immediate-invalidation coverage; do not mock `EvaluateIntegrationProgress`, rewrite decision rules in test helpers, use sleeps, or retry until green.

Required proofs:

1. For `SprintCheckpoint(SPRINT_COMPLETE)`, ordinary checkpoint-to-`COMPLETED` `Resume`, `Resume` from `COMPLETED`, direct `AdvanceSprint`, and manual `Proceed`, test both stale clean evidence and a decision that requests or observes the next global generation. Every call returns a precondition failure and leaves its path-specific progression fields and files unchanged. Reconciliation-created lifecycle work is allowed and asserted separately from forbidden progression side effects.
2. Repeat the same path matrix with a frozen cohort, all barriers resolved, and clean global evidence whose immutable source equals live integration HEAD. Each public operation reaches its existing success result.
3. Prove the settled zero-scope marker activates the gate: a non-nil empty cohort with no current-HEAD global clean evidence rejects, while nil cohort state retains pre-integration phase handoff behavior.
4. Preserve planning compatibility with focused cases for a transition checkpoint resume, pre-integration completed-sprint carry-forward, direct advance carrying unconsumed planning output, and manual `Proceed`; none requires global clean evidence before the cohort freezes.
5. Create current clean evidence for integration HEAD `A`, perform a successful public integration mutation to `B`, and immediately call ordinary checkpoint completion `Resume` and direct `AdvanceSprint` without an intervening wake. Both reject, leave the sprint uncompleted/unarchived, and observe `B` rather than stale source `A`.
6. Force reconciliation, clean-source verification, and evaluator error paths through narrow restored seams or malformed fixtures. Assert fail-closed errors and no progression-specific state/file mutation.
7. Assert rejection diagnostics preserve dependency-owned blocked/exhausted/pending context and never substitute a terminal-task fallback.

The canonical validation must observe the exact top-level pass and reject every Go failure event:

`go test -json ./internal/ops -run '^TestEffectiveIntegrationCompletionGate$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestEffectiveIntegrationCompletionGate") and all(.[]; .Action != "fail")'`

## Planned coding tasks

### Task 1 — Gate state-changing sprint progression

Description: Reject every state-changing sprint progression path while effective integration completion is false.

Done when: `TestEffectiveIntegrationCompletionGate` proves `SPRINT_COMPLETE` checkpoint, checkpoint-to-completed resume, completed-sprint resume/archive, direct advance, and manual proceed reject stale clean evidence or a pending replacement generation without applying progression-specific state or file mutations; once the contributing set is frozen, those paths succeed only when the authoritative decision is effectively complete; pre-integration phase handoffs remain available while the contributing set is nil; a settled non-nil empty cohort cannot bypass global closure; and a successful integration mutation followed immediately by resume or advance cannot archive or complete the stale sprint.

Scope: Own `internal/ops/pipeline_ops.go`, `internal/ops/pipeline_ops_test.go`, `internal/ops/advance_sprint.go`, `internal/ops/advance_sprint_test.go`, `internal/ops/mode_change.go`, `internal/ops/mode_change_test.go`, `internal/ops/proceed.go`, `internal/ops/proceed_test.go`, `internal/ops/sprint_checkpoint.go`, and `internal/ops/sprint_checkpoint_test.go`. Add one shared precondition that composes `ReconcileIntegrationAnalyses`, the integration mutation linearization protocol, and `EvaluateIntegrationProgress`; wire it only to completion-capable branches while preserving pre-integration transition handoffs and existing atomic path checks. Do not derive integration policy, create analysis tasks independently, render wake/status output, mutate the integration ref, change pipeline topology, or refactor the owned operations.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Dependencies: concrete `PROGRESS` task `code-planning-main-1-replan-1-code-planning-3-coding-1`, concrete `MUTATE` task `code-planning-main-1-replan-1-code-planning-1-coding-0`, and concrete `RECONCILE` task `code-planning-main-1-replan-1-code-planning-2-coding-1`.

Validation: `go test -json ./internal/ops -run '^TestEffectiveIntegrationCompletionGate$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestEffectiveIntegrationCompletionGate") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/pipeline_ops.go, internal/ops/pipeline_ops_test.go, internal/ops/advance_sprint.go, internal/ops/advance_sprint_test.go, internal/ops/mode_change.go, internal/ops/mode_change_test.go, internal/ops/proceed.go, internal/ops/proceed_test.go, internal/ops/sprint_checkpoint.go, internal/ops/sprint_checkpoint_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[]`; `read_only_task_depends_on=[code-planning-main-1-replan-1-code-planning-3-coding-1,code-planning-main-1-replan-1-code-planning-1-coding-0,code-planning-main-1-replan-1-code-planning-2-coding-1]`; `interfaces_owned=[state-changing effective-completion gate for checkpoint, resume, archive, advance, and proceed]`; `interfaces_consumed=[EvaluateIntegrationProgress, integration mutation linearization protocol, ReconcileIntegrationAnalyses]`; coverage: one shared precondition protects every progression mutation after the settled cohort boundary without blocking earlier phase handoffs.

## Architecture assessment

### Discovery

`pipeline_ops.go` already hosts shared pipeline-aware terminal detection, while each public progression operation owns its own mutation boundary. `SprintCheckpoint` writes a report before updating checkpoint state; `AdvanceSprint` and completed-sprint `Resume` write an archive before applying sprint history/new-sprint state; `Resume` combines mode and sprint mutation in one blackboard transaction; `Proceed` resolves pipeline policy and creates children in one blackboard transaction. The dependency tasks introduce the durable settled marker, pure progress decision, lock-ordered HEAD verifier, and idempotent reconciliation writer.

The stable boundaries are public operation authorization, blackboard atomicity, ADR-0112 lock order, and dependency-owned integration policy. The volatile concern is which operation branches represent pre-integration phase handoff versus settled integration finality.

### Analysis and recommendation

| Question | Assessment |
|---|---|
| Problem | Terminal task state can currently complete, archive, advance, or proceed even when clean integration evidence is stale or replacement analysis is pending. |
| Cost of error | High: an archive or completed sprint is durable success state and can launch downstream work despite ineffective integration evidence. |
| Failure handling | Reconciliation and verification errors reject before progression side effects; expected waiting, blocked, exhaustion, stale, and pending-generation states use the authoritative decision's diagnostics. |
| Concurrency | `MUTATE` owns integration-lock/live-HEAD order; existing operations retain blackboard atomicity. The gate adds no inverse lock order and does not hold the mutation lock across state writes. |
| Data ownership | Persistence owns settled identity, progress owns truth, reconciliation owns projection, mutation owns invalidation, and Task 1 owns only the progression precondition. |
| Boundary risk | Treating lifecycle pointer presence as settlement blocks partial handoff; treating nil/empty cohorts alike lets zero-scope settlement bypass closure; checking after archive/report writes leaves rejected side effects. The plan excludes all three. |
| Structural trade-off | `proceed.go` is documented structural debt, but extracting it here would mix refactoring with a correctness gate. A small public-entry precondition is the minimum reversible change. |

One coding task is appropriate. The five paths share one invariant and one named behavioral matrix; splitting them would duplicate the gate or serialize multiple children across the same shared tests without an independent intent.

No new architectural issue is introduced. Task 1 directly contributes to the existing `integration-closure-is-not-revalidated` correction; only `DOC` may revise or resolve that issue after `E2E` evidence exists.

## Spec Compliance Matrix

This matrix covers the complete goal. Task 1 is credited only for the state-changing progression boundary; retained aliases identify external owners without expanding this task's scope.

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Bounded per-scope coverage precedes repeated global closure analysis. | Objective | `TOPO`; `PROGRESS`; `PROJECT`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| 2 | Single-lineage coverage records task, criteria, reviewed commit, approver, validation, and merge commit. | Proposed Model | `PERSIST-PATCH`; `PROGRESS`; `PROJECT`; `RECONCILE`; `E2E` | Covered |
| 3 | Contributing scopes and distinct root coding lineages are reproducible. | Slice Integration | `PERSIST-PATCH`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 4 | Planning settles only after coding-producing sources, outputs, transitions, and resulting coding work settle. | Slice Integration | `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| 5 | Partial planning handoff cannot open coverage. | Required Property 1 | `PROGRESS`; `RECONCILE`; Task 1; `E2E` | Covered |
| 6 | The contributing set freezes exactly once after the settled boundary. | Required Property 2 | `PERSIST-PATCH`; `PROGRESS`; `RECONCILE`; Task 1; `E2E` | Covered |
| 7 | Fewer than two contributing scopes produce no slices. | Required Property 3 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 8 | Multiple contributing scopes each yield bounded local coverage. | Required Property 4 | `PERSIST-PATCH`; `PROGRESS`; `PROJECT`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| 9 | One-lineage scopes reuse approval attestations without a slice. | Required Property 5 | `PERSIST-PATCH`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 10 | Multi-lineage scopes with merged work produce exactly one slice. | Required Property 6 | `TOPO`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 11 | Escalation plans remain repair lineage outside the contributing set and create no slice. | Required Property 7 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 12 | Lineage attributes coding, fixes, and replacements to a slice. | Required Property 8 | `PERSIST-PATCH`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 13 | Each slice receives a bounded surface attributable to its originating plan. | Required Property 9 | `PROJECT`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| 14 | Each slice verdict records descendant changes and immutable source snapshot. | Required Property 10 | `PERSIST-PATCH`; `PROJECT`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| 15 | Slice findings reuse the existing integration-reviewer and coding-pair repair lifecycle. | Slice Integration | `TOPO`; `PROJECT`; `RECONCILE`; `E2E` | Covered |
| 16 | Completed slice evidence remains local and later sibling effects belong to global analysis. | Slice Integration | `PERSIST-PATCH`; `PROGRESS`; `PROJECT`; `E2E` | Covered |
| 17 | Slice resolution follows merged repair/replacement lineage; unresolved terminal work blocks. | Slice Integration | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 18 | Clean slice evidence cannot imply whole-goal completion. | Slice Integration | `PERSIST-PATCH`; `PROGRESS`; Task 1; `CONSUMERS` | Covered |
| 19 | Global analysis waits for settled planning, terminal work, complete required coverage, and resolved slices. | Global Integration; Required Property 11 | `PROGRESS`; `RECONCILE`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 20 | A blocked slice prevents global analysis and successful progression. | Global Integration | `PROGRESS`; `RECONCILE`; Task 1; `E2E` | Covered |
| 21 | Global analysis independently inspects the aggregate branch from bounded coverage navigation. | Global Integration; Required Property 12 | `CONTEXT`; `E2E` | Covered |
| 22 | Promoted repairs remain repair lineage visible to the next global analysis. | Global Integration | `PERSIST-PATCH`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 23 | Global findings require another pass after repair or replacement resolution. | Final Closure | `PROGRESS`; `PROJECT`; `RECONCILE`; Task 1; `E2E` | Covered |
| 24 | Global fixes and later HEAD mutations trigger another scan while budget remains. | Required Property 13 | `PROGRESS`; `MUTATE`; `PROJECT`; `RECONCILE`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 25 | Slice or generation exhaustion produces an explicit blocked outcome. | Required Property 14 | `PROGRESS`; `RECONCILE`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 26 | Clean completion is tied to an immutable reviewed commit. | Required Property 15 | `PERSIST-PATCH`; `PROGRESS`; `MUTATE`; `PROJECT`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 27 | Completion state, clean reviewed commit, and integration HEAD have one linearizable relationship. | Required Property 16; Success Criteria 7-8 | `PROGRESS`; `MUTATE`; `PROJECT`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 28 | The integration-HEAD mutation path owns stale-completion invalidation. | Required Property 17 | `MUTATE`; Task 1; `E2E` | Covered |
| 29 | Finalization preserves ADR-0112 lock order and writes no state under the mutation lock. | Required Property 18 | `MUTATE`; `PROJECT`; `RECONCILE`; Task 1; `E2E` | Covered |
| 30 | The global generation limit is configurable with deterministic default. | Required Property 19 | `CFG`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 31 | Wake and restart recovery cannot duplicate slice or global analyses. | Required Property 20 | `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| 32 | Workflow remains stack-agnostic and preserves review/merge authorization. | Required Property 21 | `CFG`; `TOPO`; `MUTATE`; `PROJECT`; `RECONCILE`; Task 1; `E2E` | Covered |
| 33 | Coverage cannot begin while any planning/output/transition/coding prerequisite is unsettled. | Success Criterion 1 | `PROGRESS`; `RECONCILE`; Task 1; `E2E` | Covered |
| 34 | Cohort classification and zero-slice bypass are reproducible. | Success Criterion 2 | `PERSIST-PATCH`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 35 | Every multi-scope cohort member has an attestation or exactly one required slice. | Success Criterion 3 | `PERSIST-PATCH`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 36 | No global analysis or terminal progression is available behind a local barrier. | Success Criterion 4 | `PROGRESS`; `RECONCILE`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 37 | Every slice records a bounded surface and immutable snapshot. | Success Criterion 5 | `PERSIST-PATCH`; `PROJECT`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| 38 | Global analysis independently reviews the aggregate after local resolution. | Success Criterion 6 | `PROGRESS`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| 39 | Successful integration linearizes only when clean source equals live integration HEAD. | Success Criterion 7 | `PROGRESS`; `MUTATE`; `PROJECT`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 40 | Controlled concurrency proves both mutation/finalization orders cannot leave stale success. | Success Criterion 8 | `MUTATE`; Task 1; `E2E` | Covered |
| 41 | Later mutations reanalyze within budget and block after exhaustion. | Success Criterion 9 | `PROGRESS`; `MUTATE`; `RECONCILE`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 42 | Repeated wake and restart evaluation remains duplicate-free. | Success Criterion 10 | `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| 43 | No master-planning change, fix-review replacement, global-analysis removal, stack-specific default, or new role is introduced. | Out of Scope | `CFG`; `TOPO`; all owners retain declared boundaries | Covered |
| 44 | ADR extends ADR-0055 and supersedes its no-rescan limitation. | Documentation Impact 1-2 | `DOC` | Covered |
| 45 | State-machine and task-lifecycle documentation describes the new lifecycle. | Documentation Impact 3 | `DOC` | Covered |
| 46 | Pipeline and operational documentation covers barriers, generations, and outcomes. | Documentation Impact 4 | `DOC` | Covered |
| 47 | The integration-closure issue changes only after implementation and validation evidence exists. | Documentation Impact 5 | `E2E`; `DOC` | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | `E2E` (`code-planning-main-1-code-planning-9`) | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | `DOC` (`code-planning-main-1-code-planning-10`) | Covered |

## Pre-submit audit

- Atomicity: one coding task owns one observable progression invariant and its colocated aggregate/path tests.
- Dependency order: output 0 names the concrete `PROGRESS`, `MUTATE`, and `RECONCILE` coding providers required by the corrected post-emission handoff.
- Shared-file audit: one output entry owns all ten assigned files, so no intra-plan collision or sibling dependency edge exists.
- Policy boundary: nil/non-nil cohort identity, effective completion, reconciliation, and HEAD verification remain dependency-owned; Task 1 only composes them.
- Compatibility: transition checkpoints and pre-integration manual/carry-forward flows remain explicit positive cases instead of being silently blocked.
- Side effects: every rejection occurs before path-specific summary/archive/task/sprint mutation; reconciliation-created analysis work is distinguished from forbidden progression work.
- Validation: the canonical JSON predicate requires the named top-level test and rejects every Go failure event.
- Cross-references: every alias is bound in Current task routing and credited only for its retained responsibility.
- Compliance: every goal requirement, constraint, success criterion, E2E impact, and documentation impact is covered with no GAP.
