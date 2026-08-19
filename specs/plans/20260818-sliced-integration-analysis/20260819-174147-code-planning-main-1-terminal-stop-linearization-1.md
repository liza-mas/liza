# Code Plan: Linearize Goal-Complete Stop with Integration HEAD Mutation

## Intent and evidence

Close the operations-layer check-then-stop race left between the merged integration-mutation and state-changing completion-gate plans. Add one authoritative goal-complete stop operation whose authorization, conditional mode change, post-change verification, and mutation-side invalidation form one race-safe protocol while generic operator stop behavior remains unchanged.

Success means `TestLinearizableGoalCompleteStop` deterministically pauses the public goal-complete stop after clean/current-HEAD authorization, advances integration HEAD through the public mutation path before the mode write, releases both operations, and proves stale clean evidence cannot leave the system `STOPPED`; the clean no-race case still stops; a mutation after a completed goal stop reactivates only an automatic goal-complete stop; and every blackboard write occurs after release of the ADR-0112 integration mutation lock.

Based on:

- The full goal specification at `specs/goals/20260818-sliced-integration-analysis.md`, especially Final Closure, Required Properties 13-18, and Success Criteria 7-9.
- The retained master plan at `specs/plans/20260818-sliced-integration-analysis/20260819-005612-code-planning-main-1.md`, replacement master plan at `specs/plans/20260818-sliced-integration-analysis/20260819-084641-code-planning-main-1-replan-1.md`, `MUTATE` plan at `specs/plans/20260818-sliced-integration-analysis/20260819-122954-code-planning-main-1-replan-1-code-planning-1.md`, and `GATE` plan at `specs/plans/20260818-sliced-integration-analysis/20260819-161602-code-planning-main-1-code-planning-7.md`.
- Targeted authoritative state reads for `MUTATE`, `GATE`, the superseded consumer plan, and its replacement planning task.
- ADR-0112; `INVARIANTS.md` §§5 and 7 plus the Protection Matrix; the Update Policy, Open Issues Summary, and `Integration Closure Is Not Revalidated` entry in `specs/architecture/architectural-issues.md`.
- Stacklit orientation for `internal/ops`; Semble discovery attempted for the terminal stop path; and direct reads of `internal/ops/mode_change.go`, its focused tests, the integration mutation lock, and the current `ops.Stop` call sites.

Load-bearing claims:

- **EVIDENCED — remaining race:** the superseded consumer plan was rejected because it performed a fresh decision and then called `ops.Stop`; current `Stop` performs a separate blackboard mode write, so a mutation can linearize between those actions.
- **EVIDENCED — dependency boundary:** concrete task `code-planning-main-1-replan-1-code-planning-1-coding-0` owns the integration-ref update, mutation receipts, and lock-ordered clean-source verification; concrete task `code-planning-main-1-code-planning-7-coding-0` owns the shared effective-completion precondition for state-changing progression.
- **EVIDENCED — lock constraint:** ADR-0112 permits blackboard reads under the integration mutation lock but requires release before every blackboard write. Holding that lock through the stop mode write is not an admissible solution.
- **EVIDENCED — supported mutation audit:** the `MUTATE` contract appends a typed before/after receipt after each successful public integration-ref update and leaves no receipt for no-op or failed CAS attempts. That append boundary is the supported mutation-side place to invalidate an automatic terminal stop after lock release.
- **EVIDENCED — manual-stop separation:** generic `Stop` is also used for operator, TUI, and safety shutdowns. Mutation-side reactivation must therefore recognize only the authoritative automatic goal-complete stop and must not restart a manual or fault-induced `STOPPED` mode.

Doc Impact: only this planning artifact and its structured output. Existing goal-level `DOC` remains responsible for documenting the implemented terminal-stop contract after acceptance evidence exists.

Test Impact: Task 1 adds the required deterministic `TestLinearizableGoalCompleteStop` in `internal/ops`, using channels and bounded failure timeouts rather than sleeps or retry-until-green loops. Existing goal-level `E2E` retains cross-component lifecycle validation.

## Current task routing

| Alias | Concrete task ID | Responsibility |
|---|---|---|
| `PROGRESS` | `code-planning-main-1-replan-1-code-planning-3-coding-1` | Pure authoritative progress and effective-completion decision |
| `MUTATE` | `code-planning-main-1-replan-1-code-planning-1-coding-0` | Integration-ref mutation receipts, immediate HEAD invalidation, and lock-ordered clean-source verification |
| `RECONCILE` | `code-planning-main-1-replan-1-code-planning-2-coding-1` | Idempotent analysis materialization and blocked/exhausted closure projection |
| `GATE` | `code-planning-main-1-code-planning-7-coding-0` | Shared state-changing effective-completion precondition |
| Task 1 / `TERMINAL-STOP` | output 0 | Authoritative automatic goal-complete stop plus mutation-side mode invalidation |
| `CONSUMERS` | `code-planning-main-1-code-planning-8-replacement-1` | Replacement planning for wake, supervisor, and status consumers; depends on this planning task |
| `E2E` | `code-planning-main-1-code-planning-9` | Goal-level lifecycle and controlled-race integration coverage |
| `DOC` | `code-planning-main-1-code-planning-10` | ADR, lifecycle/operator documentation, and issue disposition |

## Architecture and protocol

```text
GoalCompleteStop
    |
    +-- GATE reconciliation + effective-completion precondition
    +-- MUTATE lock-ordered verification -> authorization token for clean source A
    |                                      (integration lock released)
    +-- conditional blackboard mode write -> STOPPED / automatic-goal marker
    +-- MUTATE lock-ordered post-check
           | current A -> return STOPPED
           ` stale B   -> conditional blackboard reactivation -> RUNNING

MergeWorktree A -> B
    |
    +-- integration mutation lock: update ref only
    |                         (integration lock released)
    `-- receipt blackboard transaction
           +-- append validated A -> B receipt
           `-- if mode is the automatic goal-complete STOPPED state for A,
               reactivate RUNNING in the same transaction
```

Task 1 composes dependency-owned policy and mutation facts; it does not add another progress evaluator, generation rule, analysis creator, Git merge policy, or consumer presentation path.

### Authoritative terminal-stop operation

Add one public operations entry point, `StopForGoalCompletion`, for supervisor goal closure. Keep generic `Stop` unchanged for operator, TUI, maintenance, and safety shutdowns. The new operation must:

1. Invoke the `GATE` precondition so reconciliation is projected before terminal authorization and waiting, stale, blocked, exhausted, malformed, or unavailable progress fails closed.
2. Obtain a `MUTATE` lock-ordered authorization token that identifies the clean closure generation/key, immutable source commit, and observed mutation-receipt prefix for live integration HEAD. The verifier releases the integration mutation lock before returning.
3. In one blackboard transaction, reject if the closure identity/source or receipt prefix differs from the authorization token, validate the ordinary transition to `STOPPED`, set `Config.ModeChangedBy` to a package-private reserved automatic actor such as `system:goal-complete`, and retain the exact `ModeChangedAt` written by this call as its compare identity. This uses the existing mode-change audit fields and introduces no second lifecycle schema; generic `Stop` continues to persist its caller-supplied actor.
4. Re-run the dependency-owned lock-ordered verification after the mode transaction. If the source is now stale, conditionally restore `RUNNING` only when mode is still `STOPPED` and both `ModeChangedBy` and `ModeChangedAt` match this exact goal-complete stop, then return a precondition failure. Never overwrite a later operator mode change.
5. Return success only after the post-check confirms that the stopped authorization source still equals live integration HEAD.

The authorization token is an operations-private compare token, not new durable integration evidence. It may snapshot existing immutable closure identity and the append-only receipt prefix, but it must not duplicate policy from `EvaluateIntegrationProgress`.

### Mutation-side invalidation

Extend the dependency-owned receipt persistence boundary after `MergeWorktree` successfully changes the integration ref. In the same blackboard transaction that appends and validates the mutation receipt, detect an automatic goal-complete `STOPPED` mode authorized for the receipt's superseded `before` commit and reactivate `RUNNING`. Do not reactivate generic operator, TUI, maintenance, circuit-breaker, or safety stops.

This covers every relative ordering after authorization:

- Receipt persists before the stop transaction: the authorization compare fails and no stop is written.
- Stop transaction wins before receipt persistence: the receipt transaction reactivates it.
- Ref update occurs while receipt persistence is pending: the stop post-check observes live HEAD and conditionally reactivates it.
- Mutation begins after a successful stop returns: its post-lock receipt transaction invalidates that automatic stop without waiting for a later wake.

All mode and receipt writes occur after the integration mutation lock is released. The protocol preserves lock order `integration mutation lock -> blackboard read`, introduces no blackboard write under that lock, and retains existing CAS, rollback, sync/restore, lifecycle-transition validation, and generic mode-transition behavior.

## Deterministic TDD proof

Add one top-level `TestLinearizableGoalCompleteStop` with named subtests and public-operation fixtures. Write the failing test before implementation.

Required proofs:

1. Seed a frozen cohort, resolved barriers, and clean global closure whose immutable source is integration HEAD `A`. `StopForGoalCompletion` succeeds and leaves the mode `STOPPED` only while a fresh authoritative evaluation remains effectively complete for `A`.
2. Pause the stop through a narrow restored test seam after lock-ordered authorization for `A` and before its blackboard mode transaction. Run public `MergeWorktree` to move integration HEAD to `B`, confirm the ref update before releasing the stop, then let both operations finish. Assert the clean source remains `A`, live HEAD is `B`, the mutation receipt is present, the stop returns a precondition failure or is invalidated, and final mode is `RUNNING`, never durable stale `STOPPED`.
3. Exercise both blackboard orderings after the ref update: receipt append before the mode transaction rejects the conditional stop; mode transaction before receipt append is reactivated by the mutation transaction.
4. Start from a successful automatic goal-complete stop at `A`, then mutate to `B`. Assert mutation completion reactivates `RUNNING` without an intervening wake or consumer call.
5. Repeat the same mutation from a generic `Stop` created by an operator identity. Assert it remains `STOPPED`, proving the invalidator cannot override unrelated shutdown authority.
6. At every goal-stop and mutation-side mode-write seam, attempt bounded acquisition of the integration mutation lock and require success, proving no blackboard write occurs while that lock is held.
7. Use channels and bounded failure timeouts only. No sleeps, timing-only assertions, mocked progress predicates, duplicated evaluator rules, or retry-until-green loops.

Canonical validation:

`go test -json ./internal/ops -run '^TestLinearizableGoalCompleteStop$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestLinearizableGoalCompleteStop") and all(.[]; .Action != "fail")'`

## Planned coding tasks

### Task 1 — Linearize automatic goal-complete stop

Description: Make the authoritative goal-complete stop linearizable with integration HEAD mutation.

Done when: `TestLinearizableGoalCompleteStop` proves `StopForGoalCompletion` leaves the system `STOPPED` only for clean evidence bound to current integration HEAD; a public HEAD mutation deterministically ordered between stop authorization and mode change cannot leave stale success `STOPPED`; mutation after a completed automatic stop reactivates `RUNNING` without a later wake; generic operator stops remain `STOPPED`; and every blackboard write occurs after release of the ADR-0112 integration mutation lock.

Scope: Own the terminal-stop composition and mutation-side mode invalidation in `internal/ops/pipeline_ops.go`, `internal/ops/pipeline_ops_test.go`, `internal/ops/mode_change.go`, `internal/ops/mode_change_test.go`, `internal/ops/wt_merge.go`, and `internal/ops/wt_merge_test.go`. Reuse the dependency-owned effective-completion precondition, clean-source verifier, mutation receipt, and lifecycle transition validator; preserve generic `Stop`, CAS merge, rollback, sync/restore, and manual or safety shutdown behavior. Do not change consumer wake/status presentation, integration generation policy, pipeline topology, durable model schemas, documentation, or code outside `internal/ops`.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Dependencies: concrete `MUTATE` task `code-planning-main-1-replan-1-code-planning-1-coding-0` and concrete `GATE` task `code-planning-main-1-code-planning-7-coding-0`.

Validation: `go test -json ./internal/ops -run '^TestLinearizableGoalCompleteStop$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestLinearizableGoalCompleteStop") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/pipeline_ops.go, internal/ops/pipeline_ops_test.go, internal/ops/mode_change.go, internal/ops/mode_change_test.go, internal/ops/wt_merge.go, internal/ops/wt_merge_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[]`; `read_only_task_depends_on=[code-planning-main-1-replan-1-code-planning-1-coding-0,code-planning-main-1-code-planning-7-coding-0]`; `interfaces_owned=[StopForGoalCompletion, goal-complete stop authorization token, mutation-side automatic-stop invalidation]`; `interfaces_consumed=[state-changing effective-completion gate for checkpoint, resume, archive, advance, and proceed, integration mutation linearization protocol, clean-source verification under the integration mutation lock, IntegrationMutationReceipt persistence schema, integration lifecycle invariant validation]`; coverage: one operations protocol closes the authorization-to-mode-write race without weakening manual stop authority or ADR-0112.

## Architecture assessment

| Question | Assessment |
|---|---|
| Problem | A correct decision followed by generic `Stop` is a TOCTOU pair across live Git HEAD and blackboard mode state. |
| Change vectors | Integration progress policy and Git mutation remain dependency-owned; only automatic terminal-stop composition and mutation-side mode invalidation change. |
| Cost of error | High: stale success can halt the supervisor before required reanalysis, or an over-broad invalidator can override an operator shutdown. |
| Failure handling | Authorization, compare, and post-check failures are fail-closed; conditional reactivation changes only the exact automatic stop identity it owns. |
| Concurrency | Ref updates linearize under the integration mutation lock; blackboard writes happen only after release; compare tokens plus mutation-side invalidation cover both blackboard orderings. |
| Data ownership | `PROGRESS` owns truth, `RECONCILE` owns requested work, `MUTATE` owns ref changes/receipts, `GATE` owns the shared precondition, and Task 1 owns only terminal-stop composition. |
| Boundary | Generic `Stop` remains an unconditional authorized shutdown. Only `StopForGoalCompletion` participates in clean-evidence policy and automatic reactivation. |
| Reversibility | The change is localized to dependency-ordered `internal/ops` files and introduces no schema, topology, role, or consumer presentation change. |

Considered alternatives:

1. Check progress then call generic `Stop`: rejected because it is the demonstrated race.
2. Hold the integration mutation lock through the mode write: rejected because ADR-0112 forbids blackboard writes under that lock.
3. Clear or rewrite clean evidence on mutation: rejected because evidence is immutable audit and live HEAD mismatch already owns effectiveness.
4. Use compare/post-check plus mutation-side invalidation for a distinct automatic stop: selected because it preserves the lock order, manual stop authority, and one supported mutation owner.

One coding task is appropriate. The terminal operation, receipt-bound invalidation, and controlled race are one invariant and must share the same dependency-ordered operations files; splitting them would create an unsafe half-protocol and unavoidable shared-file serialization.

No new architectural issue is introduced. Task 1 is a scoped correction for the existing `integration-closure-is-not-revalidated` issue; only `DOC` may revise or resolve that issue after goal-level implementation and validation evidence exists.

## Spec Compliance Matrix

Task 1 is credited only for the terminal-stop and mutation/finalization boundary. Existing aliases identify retained owners and do not expand this plan's implementation scope.

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Bounded per-scope coverage precedes repeated global closure analysis. | Objective | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 2 | A one-lineage scope contributes bounded coding-review approval evidence. | Proposed Model | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 3 | Contributing scopes and distinct root coding lineages are reproducible only after planning settles. | Slice Integration | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 4 | Fewer than two scopes produce no slices; multi-scope cohorts use attestations or exactly one required slice per scope. | Slice Integration; Required Properties 3-6 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 5 | Integration-escalation plans remain repair lineage outside the contributing set. | Slice Integration; Required Property 7 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 6 | Task lineage attributes coding, fix, and replacement tasks to slices. | Required Property 8 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 7 | Slice surfaces and verdict snapshots are bounded and immutable. | Required Properties 9-10 | `RECONCILE`; `E2E` | Covered |
| 8 | Missing, unresolved, exhausted, or blocked slice work prevents global analysis. | Global Integration; Required Properties 11 and 14 | `PROGRESS`; `RECONCILE`; `GATE`; `E2E` | Covered |
| 9 | Global analysis independently inspects the aggregate branch. | Global Integration; Required Property 12 | `E2E` | Covered |
| 10 | Global findings and later repair mutations require another global pass. | Final Closure; Required Property 13 | `PROGRESS`; `MUTATE`; `RECONCILE`; `GATE`; Task 1; `E2E` | Covered |
| 11 | Clean completion is tied to an immutable reviewed commit equal to current integration HEAD. | Final Closure; Required Property 15 | `PROGRESS`; `MUTATE`; `GATE`; Task 1; `E2E` | Covered |
| 12 | Completion and integration-HEAD mutation have one linearizable order without correctness depending on a later wake. | Final Closure; Required Property 16 | `MUTATE`; `GATE`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 13 | The integration-HEAD mutation path owns invalidation of completion tied to a superseded HEAD. | Final Closure; Required Property 17 | `MUTATE`; Task 1; `E2E` | Covered |
| 14 | Finalization preserves ADR-0112 lock order and performs no blackboard write under the mutation lock. | Final Closure; Required Property 18 | `MUTATE`; `GATE`; Task 1; `E2E` | Covered |
| 15 | HEAD/source mismatch invalidates evidence and requires another generation. | Final Closure | `PROGRESS`; `MUTATE`; `RECONCILE`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 16 | The generation limit is configurable with deterministic default and explicit exhaustion. | Final Closure; Required Properties 13-14 and 19 | `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| 17 | Partial handoff does not open coverage and the cohort freezes exactly once after all prerequisites settle. | Required Properties 1-2 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 18 | Wake and restart recovery cannot duplicate slice or global analyses. | Required Property 20 | `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| 19 | The workflow remains stack-agnostic and preserves review and merge authorization. | Required Property 21 | `MUTATE`; `GATE`; Task 1; `E2E` | Covered |
| 20 | Coverage cannot begin while any planning source, output, transition, or resulting coding work is unsettled. | Success Criterion 1 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 21 | Cohort classification, zero-slice bypass, and bounded coverage are reproducible. | Success Criteria 2-3 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 22 | No global analysis or terminal progression is available behind a local barrier. | Success Criterion 4 | `PROGRESS`; `RECONCILE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| 23 | Slice evidence records a bounded surface and immutable snapshot. | Success Criterion 5 | `RECONCILE`; `E2E` | Covered |
| 24 | Global analysis independently reviews the aggregate after local resolution. | Success Criterion 6 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 25 | Successful integration and automatic terminal stop require clean source equality with live HEAD. | Success Criterion 7 | `PROGRESS`; `MUTATE`; `GATE`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 26 | Controlled concurrency proves mutation-before-finalization and mutation-after-finalization cannot leave stale success. | Success Criterion 8 | `MUTATE`; Task 1; `E2E` | Covered |
| 27 | Later mutations reanalyze within budget and block explicitly after exhaustion. | Success Criterion 9 | `PROGRESS`; `MUTATE`; `RECONCILE`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 28 | Repeated wake and restart evaluation remains duplicate-free. | Success Criterion 10 | `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| 29 | No master-planning change, fix-review replacement, global-analysis removal, stack-specific default, or new role is introduced. | Out of Scope | All owners retain declared boundaries | Covered |
| 30 | ADR, state-machine, task-lifecycle, pipeline, operational, and issue-lifecycle documentation changes remain evidence-ordered. | Documentation Impact | `DOC` after `E2E` | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | `E2E` (`code-planning-main-1-code-planning-9`); operations race is colocated in Task 1 | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | `DOC` (`code-planning-main-1-code-planning-10`); no duplicate documentation task in this repair scope | Covered |

## Pre-submit audit

- Atomicity: one output owns one observable terminal-stop invariant and its TDD proof.
- Dependency order: output 0 names both concrete coding providers `MUTATE` and `GATE`; it cannot become claimable until both are merged.
- Shared-file audit: one output owns all six downstream files, so there is no intra-plan collision; external overlap is serialized by the concrete dependencies.
- Ownership: Task 1 composes dependency-owned policy, verifier, receipt, and validator interfaces without duplicating them or changing durable schemas.
- Lock discipline: every planned blackboard write occurs after release of the integration mutation lock; manual stop authority is preserved.
- Validation: the canonical JSON predicate requires the named top-level test and rejects every Go failure event.
- Cross-references: every alias is bound in Current task routing and credited only for its declared responsibility.
- Compliance: every goal requirement, constraint, success criterion, E2E impact, and documentation impact is covered with no GAP.
