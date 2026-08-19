# Code Plan: Sliced Integration Lifecycle Acceptance

## Intent and evidence

Success means the integration test layer proves the complete sliced lifecycle and both externally observable finalization/mutation orders through public operations and real Git refs, with deterministic synchronization and no production-code changes.

Based on: `specs/goals/20260818-sliced-integration-analysis.md`; the authoritative master plan and merged sibling planning outputs; ADR-0055 and ADR-0112; `INVARIANTS.md` §§5-7 and the Protection Matrix; the update policy and relevant integration-closure architectural issues; Stacklit orientation; Semble discovery followed by direct reads of current `internal/integration` workflow and concurrency tests; and the approved plans for lifecycle persistence, progress evaluation, mutation invalidation, reconciliation, prompt projection, progression gates, terminal stop, and completion consumers.

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

The test file may construct valid prerequisite state through `db.Blackboard` and existing test helpers, but every lifecycle outcome under assertion must cross its public boundary: `ReconcileIntegrationAnalyses`, task claim/submission/reviewer claim/verdict operations, declarative fix and escalation transitions, `MergeWorktree`, prompt construction, `Resume`, `AdvanceSprint`, and `StopForGoalCompletion` as applicable. It must never edit `.liza/state.yaml` directly, copy evaluator predicates into helpers, or manufacture lifecycle outcomes that a public operation owns.

Use a fresh temporary repository and frozen pipeline per independent scenario. Read and assert the actual integration ref after every mutation. Re-read durable state through a fresh `db.Blackboard` instance for restart cases. Keep the tests serial because existing initialization helpers change process working directory and the blackboard cache is process-global; call `db.ResetInstances` at fixture boundaries.

No production test hook is added. The finalization test uses goroutines and channel handshakes to force the two public-operation orders. Dependency-owned unit tests retain responsibility for finer internal lock-boundary interleavings. All waits use bounded contexts or timeout selects; no sleeps, polling loops, timing-only assertions, or retry-until-green behavior.

## Deterministic test design

### `TestSlicedIntegrationLifecycle`

Use named subtests and same-file helpers so each acceptance claim is independently diagnosable:

1. `settled_boundary_and_zero_slice_bypass`
   - Build an eligible planning source with an unconsumed coding-producing output/transition and non-terminal resulting coding work. Public reconciliation is a no-op: no cohort, coverage, slice, or global task may appear.
   - Settle each prerequisite in turn and assert the boundary opens only after all are terminal and consumed.
   - Cover both a non-nil empty contributing set and one contributing scope. Reconciliation freezes each boundary exactly once, creates no slice, and requests the first global analysis directly when otherwise ready.

2. `mixed_coverage_two_slices_concurrent_creation_and_restart`
   - Freeze three contributing scopes: one root lineage whose branched replacement leaves carry complete coding-review attestations, plus two separate scopes that each contain at least two distinct merged root lineages. The expected coverage map therefore contains one attestation and two required slice identities.
   - Release concurrent public reconciliation calls from one channel barrier. Assert deterministic keys, exactly one approval coverage record containing every merged leaf attestation, exactly two slice tasks with one task per multi-lineage scope, exactly one `Sprint.Scope.Planned` membership for each identity, and no slice for the one-lineage scope.
   - Re-run reconciliation through a newly constructed blackboard and permuted in-memory task order. Assert the cohort, coverage, both task IDs, analysis metadata, parent provenance, and planned memberships remain byte-equivalent and duplicate-free.
   - Build both slice prompts through the public role strategy and assert each contains only its own originating plan, descendant criteria, attributed commits/paths, decomposition metadata, and immutable source reads. Each prompt must exclude the attestation-only scope, the other slice scope, unrelated goal data, and moving `HEAD` commands.

3. `two_slice_fan_in_and_global_boundary_restart`
   - Start from the three-scope fixture before reconciliation. The two absent required slice identities prevent any global task; reconciliation materializes both slices but still no global work because both are unresolved.
   - Drive slice A through public analyst claim, checkpoint, submission, reviewer claim, and a clean verdict. Assert slice A alone cannot open global analysis while slice B remains unresolved.
   - Approve findings for slice B, materialize its normal coding-fix lineage through the declarative transition, and block or abandon that fix without a completed replacement. Reconciliation must create no global task and must project the dependency-owned blocked reason rather than wait indefinitely.
   - Complete the replacement lineage through public review and merge operations, resolve slice B, and release concurrent reconciliation calls at the now-open global boundary. Assert exactly one generation-1 global identity, one task, and one planned membership.
   - Construct a fresh blackboard and repeat reconciliation at the same boundary. Assert the same global key, task metadata, parent provenance, and planned membership are retained byte-for-byte with no duplicate global generation.

4. `frozen_cohort_completed_slice_stability_and_slice_exhaustion`
   - Drive an integration finding into the normal fix lifecycle, escalate the non-trivial repair through the declared code-planning transition, and complete its coding descendants through public review and merge operations. Assert the promoted plan and descendants remain integration repair lineage, the frozen contributing cohort and coverage requirements are unchanged, and no new or replacement slice identity is created for the escalation plan.
   - After both original slices are complete, merge a later approved sibling change through public `MergeWorktree`. Assert both completed slice verdicts, analyzed descendant commits, source snapshots, keys, and planned memberships remain closed and unchanged; the changed live HEAD is instead visible to the next global analysis.
   - In a separate fixture with deliberately small task/review iteration limits, drive a slice finding's repair lineage to public iteration exhaustion. Reconciliation must record an explicit blocked slice-stage outcome and create no global analysis.
   - Repeat the multi-scope input against a valid legacy frozen pipeline lacking sliced-integration capability. Reconciliation creates neither slice nor global work and projects `pipeline_upgrade_required`; it must not silently take the zero-slice path.

5. `global_fix_rescan_generation_idempotency_and_exhaustion`
   - After mixed local coverage resolves, release concurrent reconciliation calls for generation 1 and assert exactly one deterministic global task/planned membership whose prompt contains the compact coverage map plus an independent aggregate source boundary.
   - Repeat reconciliation through a fresh blackboard before verdict and assert the same generation-1 identity and single membership survive restart.
   - Approve generation 1 with findings, create and merge its fix through the public fix lifecycle, then release concurrent reconciliation calls at the new HEAD. Assert exactly one generation-2 identity, task, and planned membership with the new live HEAD, coverage witnesses, and generation-1 provenance.
   - Repeat through another fresh blackboard at the generation-2 boundary and assert byte-equivalent identity/metadata and no duplicate global generation.
   - With `MaxGlobalIntegrationGenerations` fixed at the small explicit value, resolve generation 2 with findings and another real ref mutation. Reconciliation must project `exhausted`, preserve earlier generations, and create no generation beyond the normalized ceiling.

6. `immediate_invalidation_blocks_resume_and_advance`
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

The main risk is producing a broad test that passes on manufactured state. The plan bounds that risk by allowing direct state construction only for valid prerequisites, requiring public operations for every lifecycle outcome under assertion, checking real SHAs and durable state after each boundary, and using negative assertions that would fail if a stale terminal-count, moving-`HEAD` fallback, one-slice pseudo-fan-in, or duplicate global materialization remained. No new architecture issue is introduced by this test-only change.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Coverage cannot begin until planning sources, coding-producing outputs/transitions, and resulting coding work are settled. | Success Criterion 1; Required Properties 1-2 | Task 1 | Covered |
| 2 | The contributing set freezes exactly once at a reproducible settled boundary. | Slice Integration; Required Property 2; Success Criterion 2 | Task 1 | Covered |
| 3 | Fewer than two contributing scopes create no slices and proceed directly to global analysis when otherwise ready. | Slice Integration; Required Property 3; Success Criterion 2 | Task 1 | Covered |
| 4 | Multiple scopes each receive bounded coverage, including one approval-attestation scope and two independently required slice scopes. | Required Properties 4-6; Success Criterion 3 | Task 1 | Covered |
| 5 | A one-lineage scope reuses complete coding-review approval attestations without creating a slice. | Proposed Model; Required Property 5 | Task 1 | Covered |
| 6 | Each of two multi-lineage scopes produces exactly one deterministic slice, including under concurrent and restarted reconciliation. | Slice Integration; Required Property 6; Success Criteria 2-3, 10 | Task 1 | Covered |
| 7 | Integration-escalation code plans and descendants remain repair lineage outside the frozen cohort and create no slices. | Slice Integration; Global Integration; Required Property 7 | Task 1 | Covered |
| 8 | Task lineage, bounded prompts, attributable changes, and immutable source snapshots remain specific to each originating slice. | Slice Integration; Required Properties 8-10; Success Criterion 5 | Task 1 | Covered |
| 9 | Later sibling mutation does not reopen or replace completed slices; cross-scope effects move to the next global analysis. | Slice Integration; Required Property 10 | Task 1 | Covered |
| 10 | One clean slice cannot open global analysis while a second required slice is missing, unresolved, or blocked; resolving both creates exactly one global task. | Global Integration; Required Property 11; Success Criterion 4 | Task 1 | Covered |
| 11 | Slice findings resolve only through merged fix or replacement lineage, and unresolved terminal work blocks fan-in. | Slice Integration; Required Property 11 | Task 1 | Covered |
| 12 | Exhausting slice task/review work records an explicit blocked outcome before global analysis. | Final Closure; Required Property 14 | Task 1 | Covered |
| 13 | Global analysis independently reviews the aggregate branch using local coverage only as navigation. | Global Integration; Required Property 12; Success Criterion 6 | Task 1 | Covered |
| 14 | Global findings, promoted repairs, fixes, and later HEAD mutations cause a new scan while budget remains. | Global Integration; Final Closure; Required Property 13; Success Criterion 9 | Task 1 | Covered |
| 15 | Global generations honor the configured deterministic ceiling and expose an explicit exhausted outcome. | Final Closure; Required Properties 14, 19; Success Criterion 9 | Task 1 | Covered |
| 16 | Clean completion is bound to an immutable source equal to live integration HEAD. | Final Closure; Required Property 15; Success Criterion 7 | Task 1 | Covered |
| 17 | Both finalization/mutation orders preserve one linearizable order and prevent effective success for stale HEAD. | Final Closure; Required Properties 16-18; Success Criterion 8 | Task 1 | Covered |
| 18 | The mutation path invalidates stale completion immediately, before wake or later reconciliation. | Final Closure; Required Property 17; Success Criteria 7-9 | Task 1 | Covered |
| 19 | Public finalization and merge complete in both forced orders without deadlock, and receipt/state ordering preserves ADR-0112's lock order and post-lock state-write boundary. | Final Closure; Required Property 18; ADR-0112 | Task 1 | Covered |
| 20 | Repeated concurrent reconciliation and fresh-blackboard restart recovery cannot duplicate either slice or global generations, before or after a fix advances HEAD. | Required Property 20; Success Criterion 10 | Task 1 | Covered |
| 21 | The workflow remains stack-agnostic and preserves existing review and merge authorization boundaries. | Required Property 21 | Task 1 | Covered |
| 22 | A legacy frozen pipeline without sliced capability fails closed instead of silently skipping required coverage. | Assigned done-when; master-plan frozen-pipeline boundary | Task 1 | Covered |
| 23 | Invalidation followed immediately by resume or advance cannot archive or complete from stale clean evidence. | Assigned done-when; Final Closure | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | Task 1 | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: separately owned by `code-planning-main-1-code-planning-10` after Task 1 evidence | N/A |

## Pre-submit audit

- One cohesive coding task owns one new integration-test file and both required named tests.
- The task depends on the two final external provider leaves; no shared writable file exists with another task in this plan.
- Description, done-when, scope, spec ref, validation, plan ref, and dependency IDs are copied verbatim into `output[0]`.
- The three-scope fixture contains two independently required slices; fan-in proves missing, unresolved, blocked, and fully resolved states.
- Global materialization is exercised concurrently and through fresh-blackboard restart both before verdict and after a fix advances HEAD.
- Integration escalation, completed-slice stability after sibling mutation, and slice-stage exhaustion each have an explicit public-operation scenario and matrix row.
- Every assigned lifecycle, finalization, concurrency, restart, capability, and immediate-progression criterion has a matrix row; there is no GAP.
- The validation predicate requires both exact top-level pass events and rejects every Go failure event.
