# Code Plan: Linearize Goal-Complete Stop with Integration HEAD Mutation

## Intent and evidence

Close the operations-layer check-then-stop race left between the merged integration-mutation and state-changing completion-gate plans. Add one authoritative goal-complete stop operation whose authorization, collision-proof durable ownership token, conditional mode change, post-change verification, and mutation-side invalidation form one race-safe protocol while unrelated operator and safety stops remain authoritative.

Success means `TestLinearizableGoalCompleteStop` deterministically proves that only a reserved automatic-stop token bound to the authorized closure and integration source can be invalidated; generic `Stop` cannot manufacture that identity; exact operation-token comparison prevents an older post-check from overwriting a newer automatic stop even when audit timestamps repeat; a public HEAD mutation racing with finalization cannot leave stale success `STOPPED`; and every blackboard write occurs after release of the ADR-0112 integration mutation lock.

Based on:

- The full goal specification at `specs/goals/20260818-sliced-integration-analysis.md`, especially Final Closure, Required Properties 13-18, and Success Criteria 7-9.
- The retained master plan at `specs/plans/20260818-sliced-integration-analysis/20260819-005612-code-planning-main-1.md`, replacement master plan at `specs/plans/20260818-sliced-integration-analysis/20260819-084641-code-planning-main-1-replan-1.md`, `MUTATE` plan at `specs/plans/20260818-sliced-integration-analysis/20260819-122954-code-planning-main-1-replan-1-code-planning-1.md`, and `GATE` plan at `specs/plans/20260818-sliced-integration-analysis/20260819-161602-code-planning-main-1-code-planning-7.md`.
- The prior rejected plan and reviewer closure condition requiring a durable unambiguous ownership token, reserved-identity collision proof, and repeated-timestamp/newer-stop ABA proof.
- ADR-0112; `INVARIANTS.md` §§5 and 7 plus the Protection Matrix; the Update Policy, Open Issues Summary, and `Integration Closure Is Not Revalidated` entry in `specs/architecture/architectural-issues.md`.
- Stacklit orientation, Semble discovery, and direct reads of `internal/ops/mode_change.go`, `internal/models/config.go`, the system-mode transition graph, and current generic `Stop` call sites.

Load-bearing claims:

- **EVIDENCED — remaining race:** a fresh effective-completion decision followed by generic `Stop` is a TOCTOU pair across live integration HEAD and the blackboard mode transaction.
- **EVIDENCED — dependency boundary:** concrete task `code-planning-main-1-replan-1-code-planning-1-coding-0` owns integration-ref updates, mutation receipts, and lock-ordered clean-source verification; concrete task `code-planning-main-1-code-planning-7-coding-0` owns the shared effective-completion precondition.
- **EVIDENCED — lock constraint:** ADR-0112 permits blackboard reads under the integration mutation lock but requires release before every blackboard write. Holding that lock through a mode write is not admissible.
- **EVIDENCED — existing durable carrier:** `Config.ModeChangedBy` is persisted by every mode transition and is available to the mutation receipt transaction. It can safely carry a versioned internal token only if generic `Stop` reserves and rejects that namespace.
- **EVIDENCED — timestamp insufficiency:** `ModeChangedAt` is assigned from `time.Now` as audit metadata and same-mode `STOPPED -> STOPPED` transitions are rejected; it is neither a unique operation ID nor a safe compare-and-set identity.
- **EVIDENCED — manual-stop separation:** generic `Stop` is used for operator, TUI, supervisor safety, and maintenance shutdowns. Invalidation must recognize only a valid reserved automatic token and must compare the entire token before reactivation.

Doc Impact: only this planning artifact and its structured output. Existing goal-level `DOC` remains responsible for documenting the implemented terminal-stop contract after acceptance evidence exists.

Test Impact: Task 1 adds the required deterministic `TestLinearizableGoalCompleteStop` in `internal/ops`, including reserved-identity collision and repeated-timestamp/newer-stop ABA schedules. Existing goal-level `E2E` retains cross-component lifecycle validation.

## Current task routing

| Alias | Concrete task ID | Responsibility |
|---|---|---|
| `PROGRESS` | `code-planning-main-1-replan-1-code-planning-3-coding-1` | Pure authoritative progress and effective-completion decision |
| `MUTATE` | `code-planning-main-1-replan-1-code-planning-1-coding-0` | Integration-ref mutation receipts, immediate HEAD invalidation, and lock-ordered clean-source verification |
| `RECONCILE` | `code-planning-main-1-replan-1-code-planning-2-coding-1` | Idempotent analysis materialization and blocked/exhausted closure projection |
| `GATE` | `code-planning-main-1-code-planning-7-coding-0` | Shared state-changing effective-completion precondition |
| Task 1 / `TERMINAL-STOP` | output 0 | Authoritative automatic goal-complete stop and exact-token mutation-side invalidation |
| `CONSUMERS` | `code-planning-main-1-code-planning-8-replacement-1` | Replacement planning for wake, supervisor, and status consumers; depends on this planning task |
| `E2E` | `code-planning-main-1-code-planning-9` | Goal-level lifecycle and controlled-race integration coverage |
| `DOC` | `code-planning-main-1-code-planning-10` | ADR, lifecycle/operator documentation, and issue disposition |

## Architecture and protocol

```text
StopForGoalCompletion
    |
    +-- GATE reconciliation + effective-completion precondition
    +-- MUTATE locked verification -> closure key K, source A, receipt prefix R
    |                                 (integration lock released)
    +-- fresh crypto operation ID N
    +-- canonical reserved token T = v1(K, A, N)
    +-- conditional blackboard mode write -> STOPPED / ModeChangedBy=T
    +-- MUTATE locked post-check
           | current A -> return STOPPED
           ` stale B   -> conditional RUNNING only if ModeChangedBy is exactly T

MergeWorktree A -> B
    |
    +-- integration mutation lock: update ref only
    |                         (integration lock released)
    `-- receipt blackboard transaction
           +-- append validated receipt(A,B)
           `-- decode current reserved token; reactivate only when
               token.source=A and the transaction still owns that exact token
```

Task 1 composes dependency-owned policy and mutation facts. It does not add another progress evaluator, generation rule, analysis creator, Git merge policy, consumer presentation path, or durable model field.

### Durable automatic-stop ownership token

Use `Config.ModeChangedBy` as the existing durable carrier for a canonical versioned token in a reserved namespace such as `system:goal-complete:v1:<payload>`. The payload must encode the authorized clean global closure identity/key, immutable source commit, and a fresh 128-bit operation ID from `crypto/rand` using an unambiguous encoding such as base64url-encoded canonical JSON. `ModeChangedAt` remains audit metadata only and is never part of ownership comparison.

The token contract is:

1. The ordinary public `Stop` boundary rejects every caller-supplied `changedBy` in the entire reserved `system:goal-complete:` namespace before any mode transition. This prevents a generic caller from manufacturing a token, including a syntactically valid token copied from state.
2. `StopForGoalCompletion` alone constructs the versioned token after clean/current-HEAD authorization. Entropy failure returns fail-closed before any mode write. A narrow restored operation-ID generator seam may make the concurrency test deterministic.
3. Internal decoding accepts only the exact supported version and complete canonical payload. Malformed or unknown reserved values are never treated as automatic-stop authority and are never reactivated.
4. Post-check and mutation-side reactivation compare the entire persisted token byte-for-byte. Closure/source fields decide whether the superseded mutation applies; the fresh operation ID distinguishes repeated closure attempts and defeats ABA even when `ModeChangedAt` repeats.
5. Reactivation replaces the automatic token with the ordinary mutation/start audit actor only in the same blackboard transaction that proved exact-token ownership. A later generic or automatic stop with a different token remains `STOPPED`.

This is deliberately not a new lifecycle schema. The operation token is mode ownership metadata, while clean closure and mutation receipts remain dependency-owned immutable integration evidence.

### Authoritative terminal-stop operation

Add one public operations entry point, `StopForGoalCompletion`, for supervisor goal closure. Keep generic `Stop` behavior unchanged except for rejection of the reserved internal actor namespace. The new operation must:

1. Invoke the `GATE` precondition so reconciliation is projected before terminal authorization and waiting, stale, blocked, exhausted, malformed, or unavailable progress fails closed.
2. Obtain a `MUTATE` lock-ordered authorization snapshot containing clean closure identity, immutable source commit, and observed mutation-receipt prefix for live integration HEAD; release the integration mutation lock before continuing.
3. Generate the fresh operation ID and canonical reserved token. In one blackboard transaction, reject if closure identity, source, or receipt prefix differs from the authorization snapshot; validate the ordinary transition to `STOPPED`; write the token to `ModeChangedBy`; and write `ModeChangedAt` only as audit metadata.
4. Re-run dependency-owned lock-ordered verification. If the source is stale, conditionally restore `RUNNING` only when mode is still `STOPPED` and `ModeChangedBy` exactly equals this operation's token, then return a precondition failure. Never overwrite a later operator stop or a newer automatic stop.
5. Return success only after the post-check confirms that the token's authorized source still equals live integration HEAD.

### Mutation-side invalidation

Extend the dependency-owned receipt persistence boundary after `MergeWorktree` successfully changes the integration ref. In the same blackboard transaction that appends and validates the mutation receipt, decode the current mode token and reactivate `RUNNING` only when all of these hold:

- mode is `STOPPED`;
- the value is a valid supported reserved automatic token;
- the token's source commit equals the receipt's superseded `before` commit;
- the exact token read remains the value being conditionally replaced.

Generic operator, TUI, maintenance, circuit-breaker, safety, malformed-reserved, and newer automatic stops remain `STOPPED`.

This covers every relative ordering after authorization:

- Receipt persists before the stop transaction: the receipt-prefix/source compare rejects and no stop is written.
- Stop transaction wins before receipt persistence: the receipt transaction recognizes and reactivates that exact token.
- Ref update occurs while receipt persistence is pending: the post-check observes live HEAD and conditionally reactivates its exact token.
- Mutation begins after a successful automatic stop returns: its post-lock receipt transaction invalidates that token without waiting for a later wake.
- An older post-check resumes after a newer stop: exact operation-token mismatch preserves the newer `STOPPED` state, even if audit timestamps are equal.

All mode and receipt writes occur after the integration mutation lock is released. The protocol preserves lock order `integration mutation lock -> blackboard read`, existing CAS/rollback/sync/restore behavior, lifecycle-transition validation, and unrelated mode-transition authority.

## Deterministic TDD proof

Add one top-level `TestLinearizableGoalCompleteStop` with named subtests and public-operation fixtures. Write the failing test before implementation.

Required proofs:

1. Seed a frozen cohort, resolved barriers, and clean global closure whose immutable source is integration HEAD `A`. `StopForGoalCompletion` succeeds, writes a valid reserved token bound to that closure/source, and leaves mode `STOPPED` while fresh authoritative evaluation remains effectively complete for `A`.
2. Pause the stop after lock-ordered authorization for `A` and before its blackboard mode transaction. Run public `MergeWorktree` to move integration HEAD to `B`, confirm the ref update before releasing the stop, then assert the clean source remains `A`, live HEAD is `B`, the receipt is present, the stop fails closed or is invalidated, and final mode is `RUNNING`.
3. Exercise both blackboard orderings after ref update: receipt append before the mode transaction rejects the conditional stop; mode transaction before receipt append is reactivated by the mutation transaction.
4. Start from a completed automatic goal stop at `A`, then mutate to `B`. Assert mutation completion reactivates `RUNNING` without an intervening wake or consumer call.
5. Call generic `Stop` with an ordinary operator identity and perform the same mutation. Assert it remains `STOPPED`.
6. Call generic `Stop` with the reserved prefix and with a fully well-formed automatic token value. Assert both calls return a precondition error before changing mode, proving generic callers cannot collide with internal ownership.
7. Prove the ABA/newer-stop schedule: hold stop operation 1 after it writes token `T1`; mutate `A -> B` so it is reactivated; project clean closure for `B`; complete stop operation 2 with distinct token `T2`; force both mode writes to share the same `ModeChangedAt`; then release operation 1's stale post-check and assert it cannot replace `T2` or change the newer `STOPPED` state.
8. Force operation-ID generation failure and assert no mode write occurs. Use deterministic distinct IDs in concurrency cases so the assertion proves token identity rather than probability.
9. At every goal-stop and mutation-side mode-write seam, attempt bounded acquisition of the integration mutation lock and require success, proving no blackboard write occurs while that lock is held.
10. Use channels and bounded failure timeouts only. No sleeps, timing-only assertions, mocked progress predicates, duplicated evaluator rules, or retry-until-green loops.

Canonical validation:

`go test -json ./internal/ops -run '^TestLinearizableGoalCompleteStop$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestLinearizableGoalCompleteStop") and all(.[]; .Action != "fail")'`

## Planned coding tasks

### Task 1 — Linearize automatic goal-complete stop

Description: Make the authoritative goal-complete stop linearizable with integration HEAD mutation.

Done when: `TestLinearizableGoalCompleteStop` proves `StopForGoalCompletion` leaves the system `STOPPED` only under a valid reserved operation token bound to clean evidence for current integration HEAD; generic `Stop` rejects the reserved namespace and cannot manufacture automatic-stop ownership; public HEAD mutation before or after the mode write cannot leave stale success `STOPPED`; exact token comparison preserves generic and newer automatic stops under reserved-identity collision, repeated-timestamp, and ABA schedules; and every blackboard write occurs after release of the ADR-0112 integration mutation lock.

Scope: Own the terminal-stop composition, reserved automatic-stop token codec and generic-Stop namespace guard, and mutation-side exact-token invalidation in `internal/ops/pipeline_ops.go`, `internal/ops/pipeline_ops_test.go`, `internal/ops/mode_change.go`, `internal/ops/mode_change_test.go`, `internal/ops/wt_merge.go`, and `internal/ops/wt_merge_test.go`. Reuse the dependency-owned effective-completion precondition, clean-source verifier, mutation receipt, and lifecycle transition validator; preserve CAS merge, rollback, sync/restore, ordinary operator/safety stop authority, and all non-reserved mode behavior. Do not change consumer wake/status presentation, integration generation policy, pipeline topology, durable model schemas, documentation, or code outside `internal/ops`.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Dependencies: concrete `MUTATE` task `code-planning-main-1-replan-1-code-planning-1-coding-0` and concrete `GATE` task `code-planning-main-1-code-planning-7-coding-0`.

Validation: `go test -json ./internal/ops -run '^TestLinearizableGoalCompleteStop$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestLinearizableGoalCompleteStop") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/pipeline_ops.go, internal/ops/pipeline_ops_test.go, internal/ops/mode_change.go, internal/ops/mode_change_test.go, internal/ops/wt_merge.go, internal/ops/wt_merge_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[]`; `read_only_task_depends_on=[code-planning-main-1-replan-1-code-planning-1-coding-0,code-planning-main-1-code-planning-7-coding-0]`; `interfaces_owned=[StopForGoalCompletion, reserved automatic-stop token codec and namespace guard, mutation-side exact-token invalidation]`; `interfaces_consumed=[state-changing effective-completion gate for checkpoint, resume, archive, advance, and proceed, integration mutation linearization protocol, clean-source verification under the integration mutation lock, IntegrationMutationReceipt persistence schema, integration lifecycle invariant validation]`; coverage: one operations protocol closes the authorization-to-mode-write race without weakening unrelated stop authority or ADR-0112.

## Architecture assessment

| Question | Assessment |
|---|---|
| Problem | A correct decision followed by generic `Stop` is a TOCTOU pair; a caller-controlled actor plus wall-clock timestamp is not collision-safe ownership. |
| Change vectors | Integration policy and Git mutation remain dependency-owned; only automatic terminal-stop composition, reserved token ownership, and mutation-side invalidation change. |
| Cost of error | High: stale success can halt the supervisor, while an ambiguous invalidator can override an operator or newer automatic shutdown. |
| Failure handling | Authorization, entropy, decode, compare, and post-check failures are fail-closed; reactivation changes only the exact valid token it owns. |
| Concurrency | Ref updates linearize under the integration mutation lock; blackboard writes happen after release; a random operation ID and exact-token compare cover repeated timestamps and ABA. |
| Data ownership | `PROGRESS` owns truth, `RECONCILE` owns requested work, `MUTATE` owns ref changes/receipts, `GATE` owns the shared precondition, and Task 1 owns mode ownership. |
| Boundary | Generic `Stop` keeps ordinary behavior but cannot enter the reserved internal namespace. Only `StopForGoalCompletion` creates a valid automatic token. |
| Reversibility | The change is localized to dependency-ordered `internal/ops` files and reuses existing durable audit storage without schema or topology changes. |

Considered alternatives:

1. Check progress then call generic `Stop`: rejected because it is the demonstrated race.
2. Hold the integration mutation lock through the mode write: rejected because ADR-0112 forbids blackboard writes under that lock.
3. Compare caller actor plus `ModeChangedAt`: rejected because actors are caller-controlled and wall-clock timestamps can collide or repeat.
4. Add durable model fields for stop ownership: not selected because a reserved, versioned, exact token in the existing durable carrier expresses closure, source, and operation identity safely with a smaller operations-only change.
5. Reserve the namespace, bind closure/source, and use a fresh cryptographic operation ID: selected because generic callers cannot manufacture ownership and old operations cannot overwrite newer stops.

One coding task remains appropriate. Token creation/validation, terminal composition, mutation-side invalidation, and controlled race proof are one invariant across the same dependency-ordered operations files; splitting them would create an unsafe half-protocol and unavoidable shared-file serialization.

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
| 25 | Successful integration and automatic terminal stop require clean source equality with live HEAD and unambiguous internal ownership. | Success Criterion 7 | `PROGRESS`; `MUTATE`; `GATE`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 26 | Controlled concurrency proves mutation-before-finalization, mutation-after-finalization, collision, and ABA schedules cannot leave or overwrite stale success. | Success Criterion 8; reviewer closure | `MUTATE`; Task 1; `E2E` | Covered |
| 27 | Later mutations reanalyze within budget and block explicitly after exhaustion. | Success Criterion 9 | `PROGRESS`; `MUTATE`; `RECONCILE`; Task 1; `CONSUMERS`; `E2E` | Covered |
| 28 | Repeated wake and restart evaluation remains duplicate-free. | Success Criterion 10 | `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| 29 | No master-planning change, fix-review replacement, global-analysis removal, stack-specific default, or new role is introduced. | Out of Scope | All owners retain declared boundaries | Covered |
| 30 | ADR, state-machine, task-lifecycle, pipeline, operational, and issue-lifecycle documentation changes remain evidence-ordered. | Documentation Impact | `DOC` after `E2E` | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | `E2E` (`code-planning-main-1-code-planning-9`); operations race is colocated in Task 1 | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | `DOC` (`code-planning-main-1-code-planning-10`); no duplicate documentation task in this repair scope | Covered |

## Pre-submit audit

- Atomicity: one output owns one observable terminal-stop invariant and its TDD proof.
- Reviewer closure: token ownership is reserved against generic callers, bound to closure/source plus a fresh operation ID, and compared without timestamp uniqueness; collision and newer-stop ABA cases are mandatory test rows.
- Dependency order: output 0 names concrete coding providers `MUTATE` and `GATE`; it cannot become claimable until both are merged.
- Shared-file audit: one output owns all six downstream files, so there is no intra-plan collision; external overlap is serialized by concrete dependencies.
- Ownership: Task 1 composes dependency-owned policy, verifier, receipt, and validator interfaces without duplicating them or changing durable schemas.
- Lock discipline: every planned blackboard write occurs after release of the integration mutation lock; unrelated stop authority is preserved.
- Validation: the canonical JSON predicate requires the named top-level test and rejects every Go failure event.
- Cross-references: every alias is bound in Current task routing and credited only for its declared responsibility.
- Compliance: every goal requirement, constraint, success criterion, E2E impact, and documentation impact is covered with no GAP.
