# Code Plan: Linearize Integration HEAD Invalidation

## Intent and evidence

Make each successful integration-ref update performed by public `MergeWorktree` the immediate invalidation point for clean evidence tied to the previous HEAD, while recording an append-only typed mutation receipt only after the ADR-0112 integration mutation lock is released.

Success means `TestIntegrationMutationLinearization` proves public `MergeWorktree` appends exact before/after mutation receipts through the shared lifecycle transition validator without changing prior evidence; successful forward and rollback ref updates are recorded, no-op or failed CAS attempts are not; receipt persistence never runs under the integration mutation lock; and both mutation-before-finalization and finalization-before-mutation schedules leave stale clean evidence ineffective against live integration HEAD.

Based on:

- The full goal spec at `specs/goals/20260818-sliced-integration-analysis.md`, especially Final Closure, Required Properties 15-18, and Success Criteria 7-8.
- The replacement master plan at `specs/plans/20260818-sliced-integration-analysis/20260819-084641-code-planning-main-1-replan-1.md` and original ownership graph at `specs/plans/20260818-sliced-integration-analysis/20260819-005612-code-planning-main-1.md`.
- The persistence contract at `specs/plans/20260818-sliced-integration-analysis/20260819-111435-code-planning-main-1-replan-1-code-planning-0.md` and progress contract at `specs/plans/20260818-sliced-integration-analysis/20260819-115431-code-planning-main-1-code-planning-3.md`.
- ADR-0112; `INVARIANTS.md` §§5 and 7 plus the Protection Matrix; the architectural-issue Update Policy and `Integration Closure Is Not Revalidated`.
- Stacklit orientation for `internal/ops`, Semble transition-generation discovery, and direct reads of `integration_mutation_lock.go`, `wt_merge.go`, their focused tests, and inherited phase-gate dependency construction in `proceed.go`.

Load-bearing claims:

- **EVIDENCED — immediate invalidation:** the master plan and `PROGRESS` contract define effective completion as clean source commit equality with supplied live integration HEAD. The Git ref update, not later receipt persistence or wake evaluation, is therefore the invalidation linearization point.
- **EVIDENCED — state-write boundary:** ADR-0112 and current `withIntegrationMutationLock` require lock order `integration mutation lock -> blackboard read lock` and prohibit blackboard writes until the integration mutation lock is released.
- **EVIDENCED — mutation surface:** current `MergeWorktree` advances the integration ref inside its forward lock window and may later rewind it inside `rollbackMergedCommit`; already-merged results and failed CAS attempts do not mutate the ref.
- **EVIDENCED — dependency ordering:** this planning task depends on the `PERSIST` and `PROGRESS` planning tasks. The code-planning-to-coding transition inherits generated children from upstream planning dependencies; the existing concrete persistence child is also named explicitly in output metadata, while the not-yet-materialized progress child is inherited when its upstream transition executes.
- **EVIDENCED — scope boundary:** reconciliation, sprint progression, terminal consumers, cross-component E2E proof, and documentation retain separate owners; this task supplies only the mutation and clean-source verification protocol they consume.

Doc Impact: only this planning artifact and its structured output. `DOC` owns user-facing and architecture documentation after E2E evidence exists.

Test Impact: downstream Task 1 extends the two owned test files with one deterministic top-level `TestIntegrationMutationLinearization`; `E2E` retains the full-system controlled race.

## Current task routing

`Task 1` / `MUTATE` is this plan's sole output. Other aliases are existing goal-level planning scopes, not work added here.

| Alias | Concrete task ID | Responsibility |
|---|---|---|
| `PERSIST` | `code-planning-main-1-replan-1-code-planning-0` | Lifecycle, mutation-receipt schema, and shared lifecycle transition validation |
| `CFG` | `code-planning-main-1-code-planning-1` | Configurable global-generation ceiling and deterministic default |
| `TOPO` | `code-planning-main-1-code-planning-2` | Slice topology and frozen-pipeline capability |
| `PROGRESS` | `code-planning-main-1-code-planning-3` | Pure authoritative progress and effective-completion decision |
| Task 1 / `MUTATE` | `code-planning-main-1-replan-1-code-planning-1` | Integration ref mutation receipts, linearization, and locked clean-source verification |
| `RECONCILE` | `code-planning-main-1-replan-1-code-planning-2` | Idempotent analysis materialization and verdict projection |
| `CONTEXT` | `code-planning-main-1-code-planning-6` | Bounded slice and aggregate global context |
| `GATE` | `code-planning-main-1-code-planning-7` | State-changing effective-completion gates |
| `CONSUMERS` | `code-planning-main-1-code-planning-8` | Wake, supervisor, status, and terminal consumers |
| `E2E` | `code-planning-main-1-code-planning-9` | End-to-end lifecycle and controlled finalization race |
| `DOC` | `code-planning-main-1-code-planning-10` | ADR, lifecycle/operator docs, and architectural-issue disposition |

## Architecture boundary

```text
integration ref before=A
        |
        | integration mutation lock
        v
CAS update-ref A -> B  ------------------> live HEAD B invalidates clean source A
        |
        | release integration mutation lock
        v
blackboard Modify: append receipt(A,B)
        |
        +--> ValidateIntegrationLifecycleTransition(previous, candidate)
        `--> preserve prior lifecycle evidence byte-for-byte as an append-only prefix
```

The safety fact and audit fact are deliberately distinct:

- Live HEAD inequality is the immediate safety invalidator consumed by `EvaluateIntegrationProgress`.
- `IntegrationMutationReceipt` is durable audit evidence appended after lock release.

Do not clear or rewrite a clean closure, coverage, generation, analysis metadata, or earlier receipt when HEAD changes. A stale clean projection may remain durable but cannot be effective because all success decisions compare its immutable source commit with live HEAD.

## Mutation protocol

### 1. Capture only successful ref changes

Represent an internal mutation outcome with mutating task ID, before commit, after commit, and whether `update-ref` actually changed the integration ref.

- A successful forward fast-forward or true merge produces `before=preMergeHEAD`, `after=mergeCommit`.
- An already-merged result where before and after are equal produces no receipt.
- A failed or retried CAS attempt produces no receipt; only the final successful ref update does.
- A successful rollback produces `before=mergeCommit`, `after=preMergeHEAD`.
- A rollback skipped because another commit already landed produces no rollback receipt.

Preserve current three-attempt CAS behavior, conflict handling, candidate-artifact guards, shared-index synchronization, and branch restore behavior. Receipt production observes those outcomes; it does not replace merge policy.

### 2. Persist after releasing the mutation lock

After each successful ref update, exit `withIntegrationMutationLock` before any receipt write. Append exactly one `models.IntegrationMutationReceipt` in a blackboard `Modify` transaction. Snapshot the previous lifecycle, append to the candidate, and call `statevalidate.ValidateIntegrationLifecycleTransition(previous, candidate)` before allowing persistence.

The helper must:

- lazily initialize the optional lifecycle when needed without replacing existing evidence;
- retain all previous receipt, coverage, generation, closure, and per-task analysis evidence as required by the shared validator;
- reject a validator error before the candidate state is committed and return that error through public `MergeWorktree`;
- keep the receipt append separate from the later task-to-`MERGED` write so lock ownership and validation order are observable;
- record a ref update even when a later working-tree sync, artifact check, integration test, or restore step fails; adjacent I/O failure must not erase the already-linearized mutation fact.

For rollback, return the mutation outcome independently from restore/sync errors so the caller can append the rollback receipt whenever the CAS rewind succeeded. Preserve existing `IntegrationFailedError` and rollback diagnostics while joining or surfacing receipt-persistence errors without reporting false success.

### 3. Verify clean source under the same lock order

Add a same-package clean-source verification helper for `RECONCILE` and later finalizers. It acquires the integration mutation lock, reads fresh blackboard state only in the permitted lock order, reads live integration HEAD, and evaluates the shared `EvaluateIntegrationProgress` decision or equivalent exact source/HEAD check. It performs no blackboard write while holding the lock and returns the verified result to its caller.

The caller may persist clean projection only after the helper releases the integration mutation lock. If a mutation occurs after verification but before that persistence, live-HEAD evaluation makes the just-written projection ineffective. If a mutation occurs first, locked verification observes the new HEAD and refuses stale success. The protocol therefore has one total order without holding the Git/index lock across a state write.

## Test design

Add one top-level `TestIntegrationMutationLinearization` with deterministic named subtests. Reuse existing temporary-repository and public `MergeWorktree` fixtures; do not replace Git behavior with a mock.

Required proofs:

1. Seed valid lifecycle state with prior coverage, generation, clean closure, and an earlier receipt. Public `MergeWorktree` succeeds and appends one receipt whose task ID and exact before/after SHAs match the actual ref change; deep-compare every earlier lifecycle collection as an unchanged prefix.
2. Observe the shared transition validator from the public receipt path with a narrow restored test seam or an invalid-transition fixture. Force a validator rejection, assert the error escapes `MergeWorktree`, and assert neither the candidate receipt nor later task-merge state is persisted.
3. At the receipt-write boundary, attempt to acquire the integration mutation lock with bounded timeout and require success, proving the blackboard write begins only after release. Keep tests serial and restore every package-level hook with `t.Cleanup`.
4. Force integration-test failure after a successful forward mutation. Assert the receipt chain records forward `A -> B` and successful rollback `B -> A` in order while preserving the existing rollback result and diagnostic behavior. Assert skipped rollback or already-merged no-op paths do not manufacture receipts.
5. With old clean evidence for `A`, pause a public merge after `A -> B` linearizes. A racing clean-source verification ordered after the mutation must observe `B` and reject or return ineffective completion for `A`.
6. Order locked clean-source verification for `A` before the public merge, then let `A -> B` linearize before the clean projection is persisted. Evaluate the resulting state with live `B` and assert effective completion is false even though stale clean projection remains durable.
7. Assert both race schedules complete without sleeps, retry-until-green loops, or timing-only expectations; coordinate with channels and bounded failure timeouts.

The canonical validation must fail when the named test is absent and reject any Go failure event:

`go test -json ./internal/ops -run '^TestIntegrationMutationLinearization$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestIntegrationMutationLinearization") and all(.[]; .Action != "fail")'`

## Planned coding tasks

### Task 1 — Linearize integration HEAD invalidation

Description: Make every integration ref mutation invalidate superseded clean evidence at the mutation linearization point.

Done when: `TestIntegrationMutationLinearization` proves public `MergeWorktree` receipt persistence consumes the shared lifecycle transition validator, names the before/after commits without rewriting prior lifecycle evidence, occurs only after releasing the integration mutation lock, immediately makes old clean evidence ineffective, and clean finalization ordered before or after a racing mutation can never yield effective success for a stale commit.

Scope: Own `internal/ops/integration_mutation_lock.go`, `internal/ops/integration_mutation_lock_test.go`, `internal/ops/wt_merge.go`, and `internal/ops/wt_merge_test.go`. Preserve ADR-0112 lock order, CAS merge behavior, rollback behavior, and the prohibition on blackboard writes under the integration mutation lock; do not own sprint progression or generation reconciliation.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Dependencies: existing concrete persistence task `code-planning-main-1-replan-1-code-planning-0-coding-0`, plus the generated `PROGRESS` child inherited from this planning task's upstream phase-gate dependency when that transition executes. Both interfaces must be present before implementation compiles; do not copy either policy locally.

Validation: `go test -json ./internal/ops -run '^TestIntegrationMutationLinearization$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestIntegrationMutationLinearization") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/integration_mutation_lock.go, internal/ops/integration_mutation_lock_test.go, internal/ops/wt_merge.go, internal/ops/wt_merge_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[]`; `read_only_task_depends_on=[code-planning-main-1-replan-1-code-planning-0-coding-0]`; `interfaces_owned=[integration mutation receipt production and persistence, integration mutation linearization protocol, clean-source verification under the integration mutation lock]`; `interfaces_consumed=[IntegrationLifecycle persistence schema, IntegrationMutationReceipt persistence schema, integration lifecycle invariant validation, EvaluateIntegrationProgress]`; coverage: live HEAD mismatch is the immediate invalidator; public `MergeWorktree` appends validated typed receipts after lock release and preserves prior evidence.

## Architecture review

### Discovery

`internal/ops` is the authoritative mutation service used by CLI, supervisor, and other consumers. `performCASMerge` currently owns forward CAS decisions, `rollbackMergedCommit` owns CAS rewind, `withIntegrationMutationLock` serializes ref/index mutation, and the final `bb.Modify` owns task lifecycle completion. The new evidence schema and progress evaluator remain dependency-owned boundaries.

The volatile concern is coordination between Git ref truth and durable audit. The stable constraints are working-tree-less commit construction, CAS, ADR-0112 lock order, append-only lifecycle evidence, and the rule that live HEAD determines whether clean evidence is effective.

### Analysis and recommendation

| Question | Assessment |
|---|---|
| Problem | A later integration ref update must invalidate clean evidence immediately and leave validated audit evidence without deadlocking Git and blackboard locks. |
| Cost of error | High: stale completion could be treated as successful or a rollback could disappear from audit. Ref movement remains recoverable, but false completion is a correctness failure. |
| Failure handling | CAS failures do not produce receipts. Successful ref changes produce outcomes even if adjacent sync/restore fails. Validator rejection aborts state persistence and never converts stale evidence into effective success. |
| Concurrency | Git ref update linearizes under one project lock; clean verification uses the same lock order; blackboard writes occur only after release; live HEAD closes the release-to-write race. |
| Data ownership | `models` owns receipt shape, `statevalidate` owns append-only transitions, `PROGRESS` owns effectiveness, and Task 1 owns mutation outcomes plus authorized receipt persistence. |
| Boundary risk | Clearing stale closure would rewrite history; treating receipt persistence as the safety point would leave a wake-sized race; holding the lock through `bb.Modify` would violate ADR-0112. The protocol forbids all three. |

One cohesive coding task is appropriate: forward/rollback outcomes, receipt persistence, locked verification, and the named concurrency test share the same four files and one linearization invariant. Splitting them would create unavoidable shared-file serialization without an independent behavior boundary.

No new architecture issue is introduced. The plan directly addresses the existing integration-closure issue; only `DOC` may revise its lifecycle status after `E2E` supplies complete evidence.

## Spec Compliance Matrix

This matrix covers the complete goal. Task 1 is credited only for mutation-side invalidation, receipt audit, and locked clean-source verification; all other requirements retain their concrete owners from **Current task routing**.

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| O1 | Bounded per-scope coverage precedes repeated global closure analysis. | Objective | `PROGRESS`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| PM1 | One-lineage approval coverage records task, criteria, reviewed commit, approver, validation, and merge commit. | Proposed Model | `PERSIST`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SI1 | Contributing plans and distinct root coding lineages are reproducible. | Slice Integration | `PERSIST`; `PROGRESS`; `E2E` | Covered |
| SI2 | Planning settles only after all coding-producing sources, outputs, transitions, and resulting coding work settle. | Slice Integration | `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| SI3 | Fewer than two contributing scopes bypass slice analyses. | Slice Integration; Required Property 3 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SI4 | Multiple contributing scopes each contribute bounded local coverage. | Slice Integration; Required Property 4 | `PERSIST`; `PROGRESS`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| SI5 | One-lineage scopes reuse approval attestations and receive no slice. | Slice Integration; Required Property 5 | `PERSIST`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SI6 | Multi-lineage scopes with merged work receive exactly one slice. | Slice Integration; Required Property 6 | `TOPO`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SI7 | Integration-escalation plans remain repair lineage outside the contributing set. | Slice Integration; Required Property 7 | `PERSIST`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SI8 | Task lineage attributes coding, fixes, and replacements to slices. | Required Property 8 | `PERSIST`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SI9 | Slice context is bounded to its originating plan, descendants, commits, paths, metadata, and snapshot. | Slice Integration; Required Properties 9-10 | `PERSIST`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| SI10 | Slice findings reuse integration-reviewer and coding-pair repair lifecycle. | Slice Integration | `TOPO`; `RECONCILE`; `E2E` | Covered |
| SI11 | Later sibling changes do not reopen completed slices. | Slice Integration | `PERSIST`; `PROGRESS`; `E2E` | Covered |
| SI12 | Slice resolution follows merged repair/replacement lineage; unresolved terminal work blocks. | Slice Integration | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SI13 | Clean slice evidence cannot imply whole-goal completion. | Slice Integration | `PERSIST`; `PROGRESS`; `GATE`; `CONSUMERS` | Covered |
| GI1 | Global analysis waits for settled planning, terminal coding/repair, complete required coverage, and resolved slices. | Global Integration; Required Property 11 | `PROGRESS`; `RECONCILE`; `GATE`; `CONSUMERS`; `E2E` | Covered |
| GI2 | A blocked slice prevents global analysis. | Global Integration | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| GI3 | Global context uses coverage navigation but independently inspects the aggregate branch. | Global Integration; Required Property 12 | `CONTEXT`; `E2E` | Covered |
| GI4 | Promoted repairs remain repair lineage visible to a later global analysis. | Global Integration | `PERSIST`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| FC1 | Global findings require another global pass after repair or replacement work resolves. | Final Closure | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| FC2 | Completion requires clean global evidence bound to current integration HEAD. | Final Closure; Required Property 15 | `PERSIST`; `PROGRESS`; Task 1; `GATE`; `CONSUMERS`; `E2E` | Covered |
| FC3 | Completion and HEAD mutation have one linearizable order without later-wake correctness. | Final Closure; Required Property 16 | `PROGRESS`; Task 1; `GATE`; `CONSUMERS`; `E2E` | Covered |
| FC4 | The integration-HEAD mutation path owns stale-completion invalidation. | Final Closure; Required Property 17 | Task 1; `E2E` | Covered |
| FC5 | Finalization preserves ADR-0112 lock order and performs no blackboard write under the mutation lock. | Final Closure; Required Property 18 | Task 1; `RECONCILE`; `E2E` | Covered |
| FC6 | Any live HEAD/source mismatch invalidates evidence and requires another generation. | Final Closure | `PROGRESS`; Task 1; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| FC7 | Global mutations rescan while budget remains and exhaust explicitly at the bound. | Final Closure; Required Properties 13-14, 19 | `CFG`; `PROGRESS`; Task 1; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| FC8 | Slice exhaustion or unresolved terminal outcomes block before global analysis. | Final Closure | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| RP1 | Partial planning handoff does not open integration coverage. | Required Property 1 | `PROGRESS`; `E2E` | Covered |
| RP2 | The contributing set freezes exactly once after all planning/output/transition/coding prerequisites settle. | Required Property 2 | `PERSIST`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| RP20 | Wake and restart recovery cannot duplicate slice or global analyses. | Required Property 20 | `PERSIST`; `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| RP21 | Workflow remains stack-agnostic and preserves review and merge authorization boundaries. | Required Property 21 | `CFG`; `TOPO`; Task 1; `RECONCILE`; `E2E` | Covered |
| SC1 | Coverage remains closed while any planning source, output, transition, or resulting coding work is unsettled. | Success Criterion 1 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SC2 | Cohort classification and zero-slice bypass are reproducible. | Success Criterion 2 | `PERSIST`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SC3 | Every scope in a multi-scope cohort has the correct attestation or exactly one slice. | Success Criterion 3 | `PERSIST`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| SC4 | No global analysis is claimable behind any local barrier. | Success Criterion 4 | `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| SC5 | Every slice records a bounded surface and immutable snapshot. | Success Criterion 5 | `PERSIST`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| SC6 | Global analysis independently reviews the aggregate after local coverage resolves. | Success Criterion 6 | `PROGRESS`; `RECONCILE`; `CONTEXT`; `E2E` | Covered |
| SC7 | Successful integration linearizes only when clean source commit equals live integration HEAD. | Success Criterion 7 | `PROGRESS`; Task 1; `GATE`; `CONSUMERS`; `E2E` | Covered |
| SC8 | Controlled concurrency proves both finalization/mutation orders never leave effective stale success. | Success Criterion 8 | Task 1; `E2E` | Covered |
| SC9 | Later mutations reanalyze within budget and block after exhaustion. | Success Criterion 9 | `CFG`; `PROGRESS`; Task 1; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| SC10 | Repeated wake and restart evaluation remains duplicate-free. | Success Criterion 10 | `PERSIST`; `PROGRESS`; `RECONCILE`; `CONSUMERS`; `E2E` | Covered |
| OOS1 | No master-planning responsibility or decomposition rule changes. | Out of Scope | All tasks retain declared boundaries | Covered |
| OOS2 | Coder/reviewer validation of integration fixes remains intact. | Out of Scope | `TOPO`; `RECONCILE`; `E2E` | Covered |
| OOS3 | Global integration remains present and no new role is introduced. | Out of Scope | `TOPO`; `E2E` | Covered |
| OOS4 | No stack-specific validation command is introduced. | Out of Scope | `CFG`; Task 1; `E2E` | Covered |
| DOC1 | ADR extends ADR-0055 and supersedes the no-rescan limitation. | Documentation Impact 1-2 | `DOC` | Covered |
| DOC2 | State-machine and task-lifecycle docs describe the new lifecycle. | Documentation Impact 3 | `DOC` | Covered |
| DOC3 | Pipeline and operational docs cover barriers, generations, and terminal outcomes. | Documentation Impact 4 | `DOC` | Covered |
| DOC4 | Integration-closure issue changes only after implementation and validation evidence exists. | Documentation Impact 5 | `E2E`; `DOC` | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | `E2E` (`code-planning-main-1-code-planning-9`) | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | `DOC` (`code-planning-main-1-code-planning-10`) | Covered |

## Pre-submit audit

- Atomicity: one coding task owns one observable invariant and colocated tests across the four assigned files.
- Dependency ordering: the explicit existing persistence child and inherited `PROGRESS` phase-gate child provide all consumed interfaces before coding claimability; no future task ID is fabricated in `task_depends_on`.
- Mutation completeness: forward and successful rollback ref changes produce receipts; no-op, conflict, and failed CAS attempts do not.
- Lock discipline: ref/index work stays under ADR-0112; state reads follow the permitted order; every state write happens after lock release.
- Validation: public receipt persistence calls the dependency-owned transition validator before commit and the canonical JSON predicate requires the named top-level test.
- Shared files: one output entry means no intra-plan file collision or dependency edge is needed.
- Cross-references: every alias maps to one current concrete planning task and is credited only with its declared scope.
- Compliance: every goal requirement, constraint, acceptance criterion, E2E impact, and documentation impact is covered with no GAP.
