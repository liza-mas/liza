# Code Plan: Reconcile Integration Coverage Edge States

## Intent and evidence

Repair the shared lifecycle contract at its durable boundary, re-emit the pure integration progress evaluator against that corrected contract, and preserve the retained master dependency order while those two replacement providers are created. A settled run with no contributing scopes must remain distinguishable from an unsettled run after persistence, and one original coding root with multiple merged replacement leaves must remain one lineage while retaining approval evidence for every leaf that contributed merged work.

Success means `TestIntegrationLifecycleYAMLRoundTrip` and `TestIntegrationLifecycleValidation` prove the durable schema accepts and preserves a non-nil settled zero-scope cohort plus a bounded multi-attestation coverage record; `TestEvaluateIntegrationProgress` proves the evaluator emits those representations deterministically without creating extra lineages or omitting merged replacement leaves; and every already-created consumer stays unclaimable behind the exact emitted persistence or evaluator child until that provider is merged.

Based on:

- The full goal specification at `specs/goals/20260818-sliced-integration-analysis.md`, especially Slice Integration, Required Properties, Success Criteria, and Out of Scope.
- The retained master plan at `specs/plans/20260818-sliced-integration-analysis/20260819-005612-code-planning-main-1.md` and replacement master plan at `specs/plans/20260818-sliced-integration-analysis/20260819-084641-code-planning-main-1-replan-1.md`.
- The merged persistence contract at `specs/plans/20260818-sliced-integration-analysis/20260819-111435-code-planning-main-1-replan-1-code-planning-0.md` and the superseded evaluator contract at `specs/plans/20260818-sliced-integration-analysis/20260819-115431-code-planning-main-1-code-planning-3.md`.
- Targeted authoritative reads of `GATE`, `MUTATE`, both `RECONCILE` coding tasks, the context coding task, and the active-task summary after the prior rejection.
- ADR-0058 and ADR-0075; `INVARIANTS.md` §§3.3-3.4 and the Protection Matrix; and the integration-closure, cross-pair, well-formed-state, and decomposition-cascade entries in `specs/architecture/architectural-issues.md`.

Load-bearing claims:

- **EVIDENCED — settlement identity:** `IntegrationLifecycle.ContributingSet == nil` is the not-yet-frozen state. A non-nil empty set is the minimum durable marker that distinguishes settled zero scope from unsettled planning.
- **EVIDENCED — lineage identity:** the goal spec defines distinct lineages by different root coding tasks and includes replacements inside the root lineage. Multiple merged replacement leaves below one root are not multiple lineages.
- **EVIDENCED — evidence completeness:** selecting one merged replacement leaf would discard approval evidence for other merged work. Approval coverage therefore needs a deterministic bounded set of per-task attestations.
- **EVIDENCED — provider fan-out:** `INVARIANTS.md` §3.3 makes a draft task claimable when all direct dependencies are merged. A dependency on this planning task is only a temporary placeholder and cannot safely stand in for either emitted coding child after this plan merges.
- **EVIDENCED — current safety barrier:** `GATE` is `BLOCKED`; `MUTATE` depends on `GATE`; both `RECONCILE` coding tasks depend on this planning task and the first also depends on `MUTATE`; later context and completion work is downstream of those barriers. This keeps the consumers unclaimable while the exact children do not yet exist.

Doc Impact: only this plan and its structured task output. Product and architecture documentation remains owned by `code-planning-main-1-code-planning-10` after implementation and end-to-end evidence exist.

Test Impact: Task 1 extends the existing named model/state-validation tests; Task 2 re-emits and extends the existing named pure-evaluator test. TDD is colocated with each behavior change.

## Current task routing

| Alias | Concrete task ID | Responsibility |
|---|---|---|
| Task 1 / `PERSIST-PATCH` | output 0; expected transition ID `code-planning-main-1-replan-1-code-planning-3-coding-0` | Settled zero-scope representation and bounded plural approval-attestation persistence/validation |
| Task 2 / `PROGRESS` | output 1; expected transition ID `code-planning-main-1-replan-1-code-planning-3-coding-1` | Pure progress evaluation over the corrected schema |
| `MUTATE` | `code-planning-main-1-replan-1-code-planning-1-coding-0` | Integration-ref mutation receipts and HEAD invalidation |
| `VERDICT` | `code-planning-main-1-replan-1-code-planning-2-coding-0` | Verdict projection at the persistence boundary |
| `RECONCILE` | `code-planning-main-1-replan-1-code-planning-2-coding-1` | Analysis task materialization and closure projection |
| `GATE` | `code-planning-main-1-code-planning-7` | State-changing effective-completion barriers; currently `BLOCKED` |
| `CONTEXT-CODE` | `code-planning-main-1-code-planning-6-coding-0` | Bounded slice and global context; transitively waits on `VERDICT` and `RECONCILE` |
| `CONSUMERS` | `code-planning-main-1-code-planning-8` | Wake, supervisor, status, and terminal consumers; downstream of `GATE` |
| `E2E` | `code-planning-main-1-code-planning-9` | End-to-end lifecycle, restart, exhaustion, and race evidence |
| `DOC` | `code-planning-main-1-code-planning-10` | ADR, lifecycle/operator docs, and architecture-issue disposition |

The expected child IDs above are deterministic transition names, not pre-existing task references. ADR-0058 forbids placing them in `output[].task_depends_on` before they exist. The orchestrator must query the emitted children after this plan merges and stop if either actual ID differs.

## Architecture and ownership boundary

```text
nil / non-nil cohort + one-root replacement DAG
                       |
                       v
Task 1: durable lifecycle representation + ValidateState
                       |
                       v
Task 2: pure EvaluateIntegrationProgress decision
                       |
                       v
retained mutation, verdict, reconciliation, gate, context, consumer, E2E, and doc tasks
```

Task 1 owns durable facts. Task 2 consumes those facts and owns pure derivation. Existing downstream tasks consume one or both interfaces only after the provider child is merged. No reconciliation, verdict projection, prompt, mutation, wake, sprint-completion, E2E, or documentation behavior moves into this plan.

The change is reversible and localized. No new service, generic event abstraction, role, task state, or mutation owner is introduced.

## Durable representation contract

Task 1 updates approval coverage to contain `ApprovalAttestations []IntegrationApprovalAttestation` while retaining the `approval_attestation` evidence-family discriminator. Candidate and transition validation must enforce:

1. `ContributingSet == nil` remains valid and means the settled boundary has not been frozen.
2. A non-nil contributing set with zero scopes is valid and is the durable settled zero-contributing-scope marker.
3. Every non-empty scope still has a non-empty plan ID and at least one unique root, and roots remain exclusive to one scope.
4. Approval coverage has a non-empty attestation list and no slice report; slice coverage has one slice report and no approval attestations.
5. Every attestation retains all required approval facts, and reviewed task IDs are non-empty and unique within the coverage record.
6. Coverage for an empty cohort remains invalid because no contributing plan can own it.
7. The transition validator preserves empty and non-empty frozen cohorts immutably and keeps coverage append-only.

`TestIntegrationLifecycleYAMLRoundTrip` includes a non-nil empty cohort and a one-root coverage record with at least two distinct attestations. `TestIntegrationLifecycleValidation` exercises the valid zero-scope marker through public `ValidateState`, rejects empty, duplicate, mixed, or malformed attestation payloads, and proves an empty frozen cohort cannot be cleared or replaced.

## Pure progress contract

Task 2 restores the previously reviewed pure evaluator and corrects these edge states:

1. Settled planning with no contributing plans returns `FreezeContributingSet=true` and a non-nil empty `IntegrationContributingSet`; a later evaluation reuses the persisted marker without refreezing.
2. Lineages are counted by original root IDs. Replacement descendants never become additional roots.
3. The complete replacement DAG is resolved, and a one-root scope settles only when every required replacement branch settles under the existing lineage rules.
4. A one-root scope in a multi-scope cohort yields one sorted attestation per merged terminal leaf; missing approval facts fail closed.
5. Persisted approval coverage is effective only when unique reviewed-task membership exactly equals the canonical merged-leaf set. Missing, extra, duplicate, non-merged, or out-of-lineage evidence fails closed.
6. Multiple attestations remain one `IntegrationScopeCoverage` and never request a slice.

The evaluator remains side-effect free and deterministic. `TestEvaluateIntegrationProgress` retains the existing planning, slice, repair, global-generation, exhaustion, purity, and determinism cases while adding the zero-scope and branched replacement proofs.

## Dependency and transition-time handoff

```text
Task 1: PERSIST-PATCH
          |
          v
Task 2: PROGRESS
       /    |       \
 MUTATE  RECONCILE  GATE
    |       |         |
    +-------+---------+--> later context, consumers, E2E, docs
```

Task 2 depends on Task 1 because it compiles against and semantically consumes the plural attestation payload and durable empty-cohort marker. The tasks share no writable files.

### Required post-emission dependency repair

ADR-0075 makes dependency retargeting an orchestrator-only, invariant-checked metadata repair. After this plan merges and both children exist, the orchestrator must perform the following handoff before any blocked downstream work is resumed:

1. Query output 0 and output 1 and confirm their actual IDs are `code-planning-main-1-replan-1-code-planning-3-coding-0` and `code-planning-main-1-replan-1-code-planning-3-coding-1`.
2. Retarget `MUTATE` from the temporary `GATE` barrier to `PROGRESS`. This restores retained master Task 5's dependency on Task 4 and removes the temporary downstream-shaped edge.
3. Retarget `VERDICT` from this planning task to `PERSIST-PATCH`.
4. Retarget `RECONCILE` from this planning task to `PROGRESS`; its existing dependency on `VERDICT` preserves persistence/projection ordering.
5. Keep `GATE` blocked through the retarget validation. When it is subsequently unblocked and re-claimed, its emitted coding child must use `output[].task_depends_on` to name `PROGRESS`, `MUTATE`, and `RECONCILE`; the planning task itself must not depend on a downstream coding child.
6. Run `liza validate --json`, re-read the three retargeted coding tasks plus `GATE`, and verify the exact dependencies before unblocking `GATE` or allowing coding to resume.

The corresponding orchestrator commands are:

```text
liza -C /home/tangi/Workspace/liza retarget-dependency code-planning-main-1-replan-1-code-planning-1-coding-0 code-planning-main-1-code-planning-7 code-planning-main-1-replan-1-code-planning-3-coding-1 --reason "Restore mutation consumer to the repaired progress provider" --json
liza -C /home/tangi/Workspace/liza retarget-dependency code-planning-main-1-replan-1-code-planning-2-coding-0 code-planning-main-1-replan-1-code-planning-3 code-planning-main-1-replan-1-code-planning-3-coding-0 --reason "Bind verdict projection to the repaired persistence provider" --json
liza -C /home/tangi/Workspace/liza retarget-dependency code-planning-main-1-replan-1-code-planning-2-coding-1 code-planning-main-1-replan-1-code-planning-3 code-planning-main-1-replan-1-code-planning-3-coding-1 --reason "Bind reconciliation to the repaired progress provider" --json
```

Safety invariant: until step 6 passes, `GATE` remains `BLOCKED`. The current graph keeps `MUTATE` behind `GATE`, keeps `VERDICT` and `RECONCILE` behind this unmerged planning task plus `MUTATE`, and keeps later context/consumer work behind those nodes. After retargeting, §3.3 keeps each existing coding consumer unclaimable until its exact provider child, or a dependency transitively containing that provider, is `MERGED`. `GATE` may then be re-planned, but its coding output remains unclaimable through the required concrete `task_depends_on` edges. If any current dependency differs from the queried precondition, stop and route the repair back to the orchestrator rather than deleting or guessing an edge.

Within each emitted task, write the new failing subtests first, implement the minimum contract change, run pre-commit on touched files, then run the declared named validation command.

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

The stable boundary is the lifecycle schema plus `ValidateState`; the volatile boundary is policy deriving a frozen cohort and coverage from task provenance. The rejected implementation exposed two boundary mismatches: zero-scope settlement could not survive persistence, and a one-root replacement DAG was compressed to one leaf or mistaken for multiple lineages. The prior review exposed a third boundary mismatch at fan-out: a planning-task placeholder could satisfy §3.3 without either repaired provider being merged.

The minimum sound correction is an explicit non-nil empty cohort, a plural attestation payload, and a narrow ADR-0075 dependency handoff after the provider children exist. Adding another abstraction or moving downstream behavior into this scope would enlarge the correction without improving the invariant.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| O1 | Bounded local coverage precedes repeated global analysis until clean or explicitly blocked. | Objective | Task 1; Task 2; external `RECONCILE`, `CONTEXT-CODE`, `E2E` | Covered |
| PM1 | A one-lineage approval attestation records reviewed task, criteria, reviewed commit, approver, validation, and merge commit. | Proposed Model, opening paragraphs | Task 1; Task 2; external `VERDICT`, `E2E` | Covered |
| SI1 | Contributing scopes are pre-integration code-planning tasks with merged root coding lineages, and distinct roots define distinct lineages. | Slice Integration, paragraphs 1-2 | Task 1; Task 2 | Covered |
| SI2 | Planning settles only after every coding-producing source, eligible output/transition, and resulting coding lineage settles. | Slice Integration, paragraphs 2-3 | Task 2; external `RECONCILE`, `CONSUMERS`, `E2E` | Covered |
| SI3 | The contributing set is evaluated exactly once at the settled boundary. | Slice Integration, paragraph 3 | Task 1; Task 2; external `RECONCILE`, `E2E` | Covered |
| SI4 | Fewer than two contributing scopes bypass slice analyses. | Slice Integration, paragraph 4 | Task 1; Task 2; external `RECONCILE`, `E2E` | Covered |
| SI5 | In a multi-scope cohort, one-root scopes contribute approval evidence and multi-root scopes receive exactly one slice. | Slice Integration, paragraph 4 | Task 1; Task 2; external topology, `RECONCILE`, `E2E` | Covered |
| SI6 | Integration-escalation plans remain repair lineage outside the contributing set. | Slice Integration, paragraph 5 | Task 2; external `RECONCILE`, `E2E` | Covered |
| SI7 | Slice context is bounded to its plan, descendants, commits, paths, metadata, and source snapshot. | Slice Integration, analyst-input list | external persistence base, `VERDICT`, `RECONCILE`, `CONTEXT-CODE`, `E2E` | Covered |
| SI8 | Slice findings reuse the existing integration-reviewer and coding-pair repair lifecycle. | Slice Integration, analyst responsibility | external topology, `RECONCILE`, `E2E` | Covered |
| SI9 | Completed slices remain immutable and later sibling effects belong to global analysis. | Slice Integration, concurrency paragraph | Task 2; external persistence base, `CONTEXT-CODE`, `E2E` | Covered |
| SI10 | Findings resolve through merged fixes or complete replacement lineages; unresolved terminal work blocks. | Slice Integration, resolution paragraph | Task 2; external `RECONCILE`, `E2E` | Covered |
| SI11 | Clean slice evidence is local and cannot imply goal completion. | Slice Integration, final paragraph | Task 1; Task 2; external `GATE`, `CONSUMERS` | Covered |
| GI1 | Global analysis waits for settled planning, terminal work, complete required coverage, and resolved slices. | Global Integration, paragraph 1 | Task 2; external `RECONCILE`, `GATE`, `CONSUMERS`, `E2E` | Covered |
| GI2 | Global analysis independently inspects the aggregate branch using bounded coverage as navigation, not proof. | Global Integration, paragraphs 2-3 | external `CONTEXT-CODE`, `E2E` | Covered |
| GI3 | Promoted integration repairs remain repair lineage and are visible to the next global analysis. | Global Integration, final paragraph | Task 2; external `RECONCILE`, `E2E` | Covered |
| FC1 | Global findings require another analysis after merged repair/replacement work, and unresolved findings block. | Final Closure, paragraph 1 | Task 2; external `RECONCILE`, `E2E` | Covered |
| FC2 | Clean completion is bound to current integration HEAD with one linearizable completion/mutation order. | Final Closure, paragraphs 2-5 | Task 2; external `MUTATE`, `GATE`, `CONSUMERS`, `E2E` | Covered |
| FC3 | The integration-ref mutation path owns invalidation and preserves ADR-0112 lock ordering. | Final Closure, paragraphs 4-5 | external `MUTATE`, `E2E` | Covered |
| FC4 | Global scans are generation-bounded with deterministic configuration and explicit exhaustion. | Final Closure, paragraphs 7-8 | Task 2; external configuration, `RECONCILE`, `E2E` | Covered |
| RP1 | Partial planning handoff does not open the coverage boundary. | Required Property 1 | Task 2; external `E2E` | Covered |
| RP2 | The contributing set freezes once only after all planning/output/transition/coding prerequisites settle. | Required Property 2 | Task 1; Task 2; external `RECONCILE`, `E2E` | Covered |
| RP3 | Fewer than two contributing scopes produce no slices. | Required Property 3 | Task 1; Task 2; external `RECONCILE`, `E2E` | Covered |
| RP4 | Every scope in a multi-scope cohort contributes bounded local coverage. | Required Property 4 | Task 1; Task 2; external `RECONCILE`, `E2E` | Covered |
| RP5 | A one-lineage scope reuses complete coding-review approval evidence and produces no slice. | Required Property 5 | Task 1; Task 2; external `RECONCILE`, `E2E` | Covered |
| RP6 | A scope with at least two distinct merged root lineages produces exactly one slice. | Required Property 6 | Task 2; external topology, `RECONCILE`, `E2E` | Covered |
| RP7 | Integration-escalation plans remain repair work and create no slices. | Required Property 7 | Task 2; external `RECONCILE`, `E2E` | Covered |
| RP8 | Task lineage attributes coding, fix, and replacement tasks to their slice. | Required Property 8 | Task 2; external persistence base, `RECONCILE`, `E2E` | Covered |
| RP9 | Each slice receives a bounded surface attributable to its originating plan. | Required Property 9 | external persistence base, `VERDICT`, `RECONCILE`, `CONTEXT-CODE`, `E2E` | Covered |
| RP10 | Each slice verdict records descendant changes and immutable source snapshot. | Required Property 10 | external persistence base, `VERDICT`, `RECONCILE`, `CONTEXT-CODE`, `E2E` | Covered |
| RP11 | Missing, unresolved, or blocked local work prevents global analysis. | Required Property 11 | Task 2; external `RECONCILE`, `GATE`, `CONSUMERS`, `E2E` | Covered |
| RP12 | Global analysis independently inspects the aggregate branch. | Required Property 12 | external `CONTEXT-CODE`, `E2E` | Covered |
| RP13 | Global fixes and later integration-HEAD mutations trigger another scan within budget. | Required Property 13 | Task 2; external `MUTATE`, `RECONCILE`, `CONSUMERS`, `E2E` | Covered |
| RP14 | Slice or global exhaustion produces an explicit blocked outcome. | Required Property 14 | Task 2; external `RECONCILE`, `CONSUMERS`, `E2E` | Covered |
| RP15 | Clean completion is tied to an immutable reviewed commit. | Required Property 15 | Task 2; external persistence base, `MUTATE`, `GATE`, `CONSUMERS`, `E2E` | Covered |
| RP16 | Completion, clean source, and integration HEAD remain linearizable under mutation. | Required Property 16 | Task 2; external `MUTATE`, `GATE`, `CONSUMERS`, `E2E` | Covered |
| RP17 | Integration-HEAD mutation owns invalidation of superseded clean completion. | Required Property 17 | external `MUTATE`, `E2E` | Covered |
| RP18 | Finalization preserves ADR-0112 lock ordering. | Required Property 18 | external `MUTATE`, `VERDICT`, `RECONCILE`, `E2E` | Covered |
| RP19 | The generation limit is configurable with a deterministic default. | Required Property 19 | Task 2; external configuration, `RECONCILE`, `E2E` | Covered |
| RP20 | Wake and restart recovery create no duplicate slice or global analyses. | Required Property 20 | Task 2; external persistence base, `RECONCILE`, `CONSUMERS`, `E2E` | Covered |
| RP21 | The workflow stays stack-agnostic and preserves review/merge authorization. | Required Property 21 | Task 1; Task 2; external topology, `MUTATE`, `RECONCILE`, `E2E` | Covered |
| SC1 | Coverage cannot begin while any planning/output/transition/coding prerequisite is unsettled. | Success Criterion 1 | Task 2; external `RECONCILE`, `E2E` | Covered |
| SC2 | Cohort and attestation-vs-slice classification are reproducible, including zero-scope settlement. | Success Criterion 2 | Task 1; Task 2; external `RECONCILE`, `E2E` | Covered |
| SC3 | Every multi-scope cohort member has complete bounded approval evidence or exactly one slice. | Success Criterion 3 | Task 1; Task 2; external `RECONCILE`, `E2E` | Covered |
| SC4 | Global analysis is unclaimable behind any unsettled, missing, unresolved, or blocked local barrier. | Success Criterion 4 | Task 2; external `RECONCILE`, `CONSUMERS`, `E2E` | Covered |
| SC5 | Every slice records a bounded surface and immutable snapshot. | Success Criterion 5 | external persistence base, `VERDICT`, `RECONCILE`, `CONTEXT-CODE`, `E2E` | Covered |
| SC6 | Global analysis independently reviews the aggregate after local resolution. | Success Criterion 6 | external `RECONCILE`, `CONTEXT-CODE`, `E2E` | Covered |
| SC7 | Successful integration linearizes only when clean source equals integration HEAD. | Success Criterion 7 | Task 2; external `MUTATE`, `GATE`, `CONSUMERS`, `E2E` | Covered |
| SC8 | Controlled concurrency proves both mutation/finalization orders cannot leave stale success. | Success Criterion 8 | external `MUTATE`, `GATE`, `CONSUMERS`, `E2E` | Covered |
| SC9 | Later mutations rescan within budget and block after exhaustion. | Success Criterion 9 | Task 2; external configuration, `MUTATE`, `RECONCILE`, `CONSUMERS`, `E2E` | Covered |
| SC10 | Repeated wake and restart evaluation remains duplicate-free. | Success Criterion 10 | Task 2; external `RECONCILE`, `CONSUMERS`, `E2E` | Covered |
| C1 | Do not change master planning, coder review, global analysis existence, stack-specific defaults, or role inventory. | Out of Scope | Task 1; Task 2; external topology | Covered |
| DOC1 | Add an ADR extending ADR-0055 and superseding its no-rescan limitation. | Documentation Impact 1-2 | external `DOC` | Covered |
| DOC2 | Update state-machine and task-lifecycle documentation. | Documentation Impact 3 | external `DOC` | Covered |
| DOC3 | Update pipeline and operational documentation for barriers, generations, and outcomes. | Documentation Impact 4 | external `DOC` | Covered |
| DOC4 | Change the integration-closure issue only after implementation and validation evidence exists. | Documentation Impact 5 | external `E2E`, `DOC` | Covered |
| E2E | e2e test coverage for the new behavior | Cross-cutting | `code-planning-main-1-code-planning-9` | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | `code-planning-main-1-code-planning-10` | Covered |

## Pre-submit audit

- Atomicity: Task 1 has one persistence-contract intent; Task 2 has one pure-policy intent. Tests remain colocated with each behavior change.
- Dependency order: Task 2 depends on output 0. The orchestrator handoff binds every pre-existing coding consumer to output 0 or output 1 and requires `GATE`'s future coding output to name the concrete providers before downstream coding resumes.
- Shared-file audit: the two emitted tasks have disjoint writable files; no additional sibling serialization edge is required.
- Claimability audit: before emission, the current `BLOCKED` gate is the durable safety barrier; after retargeting, exact provider-child dependencies are the barrier under `INVARIANTS.md` §3.3.
- Scope: only planning artifacts change here. Downstream implementation remains confined to the six files declared by the two tasks.
- Validation: each task declares one single-purpose canonical command from the assigned task contract.
- Cross-references: every external alias is bound in Current task routing and credited only for its retained responsibility.
- Impact: E2E and documentation remain first-class external tasks; neither is duplicated or silently dropped.
