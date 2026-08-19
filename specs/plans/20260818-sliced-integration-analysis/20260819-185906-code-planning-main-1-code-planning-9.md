# Code Plan: Sliced Integration Lifecycle Acceptance

## Intent and evidence

Success means the integration test layer proves the complete sliced lifecycle and both externally observable finalization/mutation orders through public operations and real Git refs, with deterministic synchronization and no production-code changes.

Based on: `specs/goals/20260818-sliced-integration-analysis.md`; the authoritative master plan and merged sibling planning outputs; ADR-0055 and ADR-0112; `INVARIANTS.md` §§5-7 and the Protection Matrix; the update policy and relevant integration-closure architectural issues; current `internal/integration` workflow and concurrency tests; and the approved plans for lifecycle persistence, progress evaluation, mutation invalidation, reconciliation, prompt projection, progression gates, terminal stop, and completion consumers.

Doc Impact: this plan and its structured output only. Product documentation remains owned by `code-planning-main-1-code-planning-10` after this acceptance evidence exists.

Test Impact: Task 1 creates `internal/integration/sliced_integration_test.go` with the two required top-level tests. Existing assertions and production files remain unchanged.

ASSUMPTION: none. The generated coding task is gated on the final prompt-context and completion-consumer implementation children, which transitively include every other required provider.

## Architecture and test boundary

```text
valid state fixtures + frozen pipeline + real integration ref
                         |
                         v
             public reconciliation / review / merge
                  /                         \
       bounded slice lifecycle        bounded global loop
                  \                         /
                         v
             fresh authoritative evaluation
                         |
             resume / advance / terminal stop
```

The test file may construct valid prerequisite state through `db.Blackboard` and existing test helpers, but every behavior under test must cross its public boundary: `ReconcileIntegrationAnalyses`, task claim/submission/reviewer claim/verdict operations, declarative fix transitions, `MergeWorktree`, prompt construction, `Resume`, `AdvanceSprint`, and `StopForGoalCompletion` as applicable. It must never edit `.liza/state.yaml` directly, copy evaluator predicates into helpers, or manufacture lifecycle outcomes that a public operation owns.

Use a fresh temporary repository and frozen pipeline per independent scenario. Read and assert the actual integration ref after every mutation. Re-read durable state through a fresh `db.Blackboard` instance for restart cases. Keep the tests serial because existing initialization helpers change process working directory and the blackboard cache is process-global; call `db.ResetInstances` at fixture boundaries.

No production test hook is added. The finalization test uses goroutines and channel handshakes to force the two public-operation orders. Dependency-owned unit tests retain responsibility for finer internal lock-boundary interleavings. All waits use bounded contexts or timeout selects; no sleeps, polling loops, timing-only assertions, or retry-until-green behavior.

## Deterministic test design

### `TestSlicedIntegrationLifecycle`

Use named subtests and same-file helpers so each acceptance claim is independently diagnosable:

1. `settled_boundary_and_zero_slice_bypass`
   - Build an eligible planning source with an unconsumed coding-producing output/transition and non-terminal resulting coding work. Public reconciliation is a no-op: no cohort, coverage, slice, or global task may appear.
   - Settle each prerequisite in turn and assert the boundary opens only after all are terminal and consumed.
   - Cover both a non-nil empty contributing set and one contributing scope. Reconciliation freezes each boundary exactly once, creates no slice, and requests the first global analysis directly when otherwise ready.

2. `mixed_coverage_concurrent_creation_and_restart`
   - Freeze two contributing scopes: one root lineage whose branched replacement leaves carry complete coding-review attestations, and one scope with at least two distinct merged root lineages.
   - Release concurrent public reconciliation calls from one channel barrier. Assert deterministic keys, exactly one approval coverage record containing every merged leaf attestation, exactly one slice task for the multi-lineage scope, exactly one `Sprint.Scope.Planned` membership, and no slice for the one-lineage scope.
   - Re-run reconciliation through a newly constructed blackboard and permuted in-memory task order. Assert the cohort, coverage, task ID, analysis metadata, parent provenance, and planned membership remain byte-equivalent and duplicate-free.
   - Build the slice prompt through the public role strategy and assert it contains only its originating plan, descendant criteria, attributed commits/paths, decomposition metadata, and immutable source reads; unrelated scope data and moving `HEAD` commands must be absent.

3. `slice_fan_in_and_frozen_pipeline_fail_closed`
   - Drive a reconciled slice through public analyst claim, checkpoint, submission, reviewer claim, and verdict operations. A clean verdict makes global analysis eligible only after every required slice is resolved.
   - In a separate scenario, approve findings, materialize the normal coding-fix lineage through the declarative transition, and block or abandon it without a completed replacement. Reconciliation must create no global task and must project the dependency-owned blocked reason rather than wait indefinitely.
   - Repeat the multi-scope input against a valid legacy frozen pipeline lacking sliced-integration capability. Reconciliation creates neither slice nor global work and projects `pipeline_upgrade_required`; it must not silently take the zero-slice path.

4. `global_fix_rescan_and_generation_exhaustion`
   - After mixed local coverage resolves, reconcile generation 1 and assert its prompt contains the compact coverage map plus an independent aggregate source boundary.
   - Approve generation 1 with findings, create and merge its fix through the public fix lifecycle, then reconcile. Assert generation 2 has a new deterministic key, the new live HEAD, coverage witnesses plus generation 1 provenance, and exactly one task/planned membership.
   - With `MaxGlobalIntegrationGenerations` fixed at a small explicit value, resolve generation 2 with findings and another real ref mutation. Reconciliation must project `exhausted`, preserve earlier generations, and create no generation beyond the normalized ceiling.

5. `immediate_invalidation_blocks_resume_and_advance`
   - Begin from clean global evidence bound to integration HEAD `A`. Merge an approved task through public `MergeWorktree` to real HEAD `B`.
   - Without wake, reconciliation, or a later observation step, invoke public checkpoint completion `Resume` and direct `AdvanceSprint` in isolated fixtures. Both must reject stale source `A`, preserve the uncompleted/unarchived sprint state and files, and expose requested next-generation or exhaustion context for `B`.

Shared lifecycle helpers may reduce repetitive valid setup, analysis submission, and ref creation, but assertions remain scenario-local. Helpers must use task IDs and expected SHA values supplied by the scenario, stage explicit test files, and preserve review/merge authorization instead of directly assigning successful terminal states.

### `TestSlicedIntegrationFinalizationRace`

Prepare one clean global analysis at source `A` and one approved mutation worktree whose public merge advances the real integration ref to `B`. Start both public operations in goroutines and use channel handshakes to force these orders without timing assumptions:

1. `mutation_before_finalization`: complete `MergeWorktree(A -> B)` before releasing the clean global verdict/finalization call. Finalization for source `A` must fail closed or remain ineffective; a fresh authoritative evaluation must not report integration complete, and `StopForGoalCompletion` must not leave a successful automatic stop for `A`.
2. `mutation_after_finalization`: complete the clean global verdict and authorized goal-complete stop for `A` before releasing `MergeWorktree(A -> B)`. After the mutation returns, durable evidence may retain source `A`, but live HEAD is `B`, authoritative completion is false, the next generation is requested or exhausted, and mutation-side invalidation leaves no effective stale success or stale automatic stop.

For both orders, assert goroutine results, exact `A`/`B` refs and receipt order, closure source, fresh progress decision, final mode/automatic-stop ownership, and unchanged prior evidence. A channel close is the only release signal; every result receive has a bounded timeout.

## Planned coding tasks

### Task 1 — Prove sliced integration acceptance

Description:

```text
Prove the complete sliced integration lifecycle and finalization race through the integration test layer.
```

Done when:

```text
`TestSlicedIntegrationLifecycle` proves the settled boundary, zero-slice bypass, mixed attestation and slice coverage, concurrent slice creation without duplicates, blocked slice fan-in, global fix rescans, generation exhaustion, restart recovery, frozen-pipeline fail-closed behavior, and invalidation followed immediately by resume or advance; `TestSlicedIntegrationFinalizationRace` proves both mutation-before-finalization and mutation-after-finalization orderings never leave effective success tied to stale HEAD.
```

Scope:

```text
Own `internal/integration/sliced_integration_test.go`. Exercise public operations and real Git refs with controlled synchronization; do not change production behavior, weaken existing assertions, or encode stack-specific validation commands.
```

Spec ref:

```text
specs/goals/20260818-sliced-integration-analysis.md#success-criteria
```

Dependencies:

- `code-planning-main-1-code-planning-6-coding-0` provides immutable slice/global prompt projection.
- `code-planning-main-1-code-planning-8-replacement-1-coding-2` provides the final completion-consumer chain and transitively gates progress, persistence, configuration, pipeline capability, mutation, reconciliation, progression, and terminal-stop providers.

Validation:

```text
go test -json ./internal/integration -run '^(TestSlicedIntegrationFinalizationRace|TestSlicedIntegrationLifecycle)$' -count=1 | jq -e -s '([.[] | select(.Action == "pass" and (.Test == "TestSlicedIntegrationFinalizationRace" or .Test == "TestSlicedIntegrationLifecycle")) | .Test] | unique | sort) == ["TestSlicedIntegrationFinalizationRace","TestSlicedIntegrationLifecycle"] and all(.[]; .Action != "fail")'
```

Implementation order: add the two named red tests and minimum same-file fixtures; make lifecycle scenarios pass through public operations; add deterministic channel-ordered finalization cases; run pre-commit on the new file; run the named validation; then run the full `./internal/integration` package. Do not weaken existing integration tests or move helpers into production code.

## Architecture assessment

The integration package is the correct boundary because the evidence composes persisted state, frozen pipeline resolution, prompt projection, review transitions, real Git ref mutation, progression gates, and terminal behavior. One downstream task is intentionally cohesive: both named tests own one new file, share the same expensive repository fixtures, and jointly prove the master plan's single end-to-end acceptance interface. Splitting them would create shared-file serialization without reducing implementation risk.

The main risk is producing a broad test that passes on manufactured state. The plan bounds that risk by allowing direct state construction only for prerequisites, requiring public operations for every lifecycle outcome under assertion, checking real SHAs and durable state after each boundary, and using negative assertions that would fail if a stale terminal-count or moving-`HEAD` fallback remained. No new architecture issue is introduced by this test-only change.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Coverage cannot begin until planning sources, coding-producing outputs/transitions, and resulting coding work are settled. | Success Criterion 1; Required Properties 1-2 | Task 1 | Covered |
| 2 | The contributing set is reproducible and fewer than two scopes create no slices. | Success Criterion 2; Required Properties 2-3 | Task 1 | Covered |
| 3 | Multiple scopes receive mixed one-lineage approval evidence and exactly one slice for each multi-lineage scope. | Success Criterion 3; Required Properties 4-7 | Task 1 | Covered |
| 4 | Lineage and immutable bounded slice snapshots remain attributable to the originating plan. | Required Properties 8-10; Success Criterion 5 | Task 1 | Covered |
| 5 | Global analysis waits for all local, coding, repair, and slice barriers; blocked slice work prevents fan-in. | Required Property 11; Success Criterion 4 | Task 1 | Covered |
| 6 | Global review uses coverage as navigation while independently inspecting the aggregate branch. | Required Property 12; Success Criterion 6 | Task 1 | Covered |
| 7 | Global fixes and later HEAD mutations cause a new generation while budget remains. | Required Property 13; Success Criterion 9 | Task 1 | Covered |
| 8 | Slice or global exhaustion projects an explicit blocked/exhausted outcome. | Required Property 14; Final Closure | Task 1 | Covered |
| 9 | Clean completion is bound to an immutable source equal to live integration HEAD. | Required Property 15; Success Criterion 7 | Task 1 | Covered |
| 10 | Both finalization/mutation orders prevent durable effective success for stale HEAD. | Required Properties 16-18; Success Criterion 8 | Task 1 | Covered |
| 11 | The configurable deterministic generation ceiling is honored by the end-to-end loop. | Required Property 19; Success Criterion 9 | Task 1 | Covered |
| 12 | Repeated concurrent wake/reconcile and restart recovery create no duplicate slice or global analysis. | Required Property 20; Success Criterion 10 | Task 1 | Covered |
| 13 | The workflow remains stack-agnostic and preserves review and merge authority. | Required Property 21; Out of Scope | Task 1 | Covered |
| 14 | Frozen pipelines fail closed when sliced capability is required. | Assigned done-when; master-plan architecture boundary | Task 1 | Covered |
| 15 | A public mutation followed immediately by resume or advance cannot consume stale clean evidence. | Assigned done-when; Final Closure | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | Task 1 | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: separately owned by `code-planning-main-1-code-planning-10` after Task 1 evidence | N/A |

## Pre-submit audit

- One cohesive coding task owns one new integration-test file and both required named tests.
- The task depends on the two final external provider leaves; no shared writable file exists with another task in this plan.
- Description, done-when, scope, spec ref, validation, plan ref, and dependency IDs are copied verbatim into `output[0]`.
- Every assigned lifecycle, finalization, concurrency, restart, capability, and immediate-progression criterion has a matrix row; there is no GAP.
- The validation predicate requires both exact top-level pass events and rejects every Go failure event.
