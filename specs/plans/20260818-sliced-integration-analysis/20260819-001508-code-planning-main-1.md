# Master Code Plan: Sliced Integration Analysis

## Intent

Replace the single branch-wide integration pass with bounded per-plan coverage and a bounded global closure loop, while making successful completion a derived fact that is true only for clean evidence bound to the current integration HEAD.

Success means the downstream task graph has one authoritative progress decision, one owner per mutable file and interface, an explicit frozen-pipeline compatibility policy, and ordered ownership of every wake, completion, resume, proceed, and sprint-advance consumer. Validation is the parity, ownership, dependency, spec-matrix, and systemic review recorded below.

Based on: `specs/goals/20260818-sliced-integration-analysis.md`; ADR-0055, ADR-0059, ADR-0067, and ADR-0112; `INVARIANTS.md` §§5-7, §12, and the Protection Matrix; the Update Policy and relevant open issues in `specs/architecture/architectural-issues.md`; Stacklit module orientation; Semble lifecycle searches; and direct reads of the owned source paths named below.

Doc Impact: Task 11 owns the ADR, architecture, protocol, operator, configuration, invariant, and architectural-issue updates.

Test Impact: Tasks 1-9 colocate focused unit tests with each behavior change; Task 10 owns cross-component and controlled-concurrency validation.

## Architectural Approach

The prior design failure mode was distributed truth: terminal-task checks, wake logic, prompt logic, resume, and sprint advance could each decide that work was finished without consulting the same clean-evidence rule. This plan instead introduces a single pure decision boundary and makes every lifecycle consumer use it.

```text
task graph + pipeline + durable ledger + observed integration HEAD
                              |
                              v
                 EvaluateIntegrationProgress
                              |
          +-------------------+-------------------+
          |                   |                   |
          v                   v                   v
   reconcile coverage    gate progression    render bounded context
   and generations       and completion      for slice/global review
          |                   |                   |
          +-------------------+-------------------+
                              |
                              v
        effective_complete := clean_source_commit == current_HEAD
                              && no replacement generation pending
                              && no unresolved/blocking lineage
```

The architecture uses an explicit operations-layer barrier rather than trying to encode the fan-in in declarative transitions. Declarative pipeline configuration continues to own role-pair lifecycle states and finding-to-fix transitions. The operations layer owns the settled-boundary snapshot, lineage reconstruction, coverage fan-in, generation reconciliation, and completion decision because these depend on the whole task graph plus the live integration ref.

### Durable state contract

Task 1 owns typed goal-level integration state and per-analysis task identity.

- `IntegrationLifecycle` records one immutable contributing-set snapshot, per-scope coverage records, ordered global generations, mutation receipts, and an explicit blocked/exhausted reason.
- `IntegrationAnalysisMetadata` identifies `slice` or `global`, a deterministic analysis key, generation number where applicable, originating plan, root coding lineages, descendant commits, source commit, affected paths, and source-snapshot paths.
- A coverage record is a tagged union: `approval_attestation` contains the reviewed coding task, acceptance criteria, reviewed commit, approver, validation, and merge commit; `slice_report` refers to one reviewed slice analysis plus its immutable metadata.
- The clean integration source commit is the analysis task's immutable integration snapshot commit, not the report artifact commit created in the analyst worktree. Reviewer approval attests the report and its declared source boundary.
- Integration escalation tasks are tagged as repair lineage. They can affect later global generations but can never mutate the frozen contributing-set snapshot.
- Interleaved slice changes are reconstructed from task-attributed mutation receipts, not a presumed contiguous commit range: for each descendant merge, union the paths changed from that receipt's before commit to after commit, then read those paths at the slice's immutable source commit. This keeps attribution stable when sibling merges are interleaved.

Task 2 owns `max_global_integration_generations` with deterministic default `3`. Generation 1 is the first global pass; generations 2 and 3 are rescans after mutations or fixes. Zero or negative persisted values normalize to the default for backward compatibility; exhaustion records an explicit blocked outcome.

### Authoritative progress interface

Task 4 owns:

```go
EvaluateIntegrationProgress(state, pipelinePolicy, integrationHEAD) IntegrationProgressDecision
```

The decision is pure and contains:

- whether pre-integration planning is settled;
- whether the immutable contributing-set snapshot may be recorded;
- the reproducible contributing scopes and root coding lineages;
- one bounded coverage requirement per contributing scope;
- missing slice analysis keys;
- unresolved, superseded, abandoned, or blocked repair lineage;
- whether a global generation is ready, pending, clean, stale, or exhausted;
- whether effective integration completion is true;
- one explicit block reason when progress cannot continue.

Planning is settled only after every pre-integration planning source is terminal, every eligible coding-producing output has a recorded consumed transition, and all resulting coding work is terminal. Partial planning handoff never satisfies this boundary. The contributing set is then persisted once. Later code-planning tasks descended from integration repair remain outside it.

For fewer than two contributing scopes, no slice keys are produced. With at least two scopes, a one-lineage scope produces one approval-attestation coverage record and a multi-lineage scope produces exactly one deterministic slice key. Root coding-task identity, not descendant count, defines distinct lineages.

Replacement resolution follows canonical supersession chains. A finding is resolved only by merged fix work or a fully completed replacement lineage. Blocked or abandoned work without a completed replacement makes the decision blocked rather than indefinitely pending.

### Pipeline and compatibility boundary

Task 3 adds `slice-integration-pair` as a role-pair specialization reusing the existing integration analyst and reviewer roles with distinct lifecycle state names. It adds the slice finding-to-coding-fix auto-transition while retaining `integration-pair` for global generations.

Existing frozen `.liza/pipeline.yaml` files do not receive role-pair or transition topology migration, preserving ADR-0067. The compatibility policy is deliberate and fail-closed: loading remains non-mutating, capability detection reports that sliced integration is unavailable, and reconciliation records `pipeline_upgrade_required` whenever the frozen cohort requires a slice. A cohort with fewer than two contributing scopes may still use its existing global pair because no slice topology is required. Operator documentation requires a fresh workspace or an explicit manual frozen-pipeline update before a qualifying multi-scope run can continue. Tests must prove a legacy frozen pipeline is neither silently backfilled nor silently allowed to skip required slice coverage.

### Generation and evidence lifecycle

Task 6 owns idempotent reconciliation and verdict projection.

- Slice key: `slice:<cohort-id>:<origin-plan-id>`.
- Global key: `global:<cohort-id>:<generation>:<source-commit>`.
- Reconciliation atomically persists the cohort snapshot or creates only missing deterministic analysis tasks; repeated wake/restart evaluation returns the same keys and creates no duplicate tasks.
- Slice tasks are claimable concurrently because their keys, source snapshots, and owned review surfaces are disjoint.
- A slice verdict projects immutable descendant commits, source commit, affected paths, and source-snapshot paths into its coverage record.
- A global findings verdict waits for its merged repair or completed replacement lineage, then reconciliation requests the next generation if budget remains.
- A global clean verdict is recorded only after verifying its immutable source commit against integration HEAD. It is successful only through the effective-completion decision.
- Exhausted slice controls or global generation budget record an explicit blocked state.

### Linearization and invalidation protocol

Task 5 owns the integration-HEAD mutation boundary. Task 8 owns state-changing completion consumers. Task 9 owns wake, supervisor, prompt-dashboard, and status consumers. This ordered split prevents any completion path from retaining the old terminal-only predicate.

1. Every integration ref mutation runs under ADR-0112's integration mutation lock and returns a receipt containing before/after commits.
2. The ref update is the mutation linearization point. From that instant, `current_HEAD != clean_source_commit` makes effective completion false without waiting for a wake or a later state repair.
3. The mutation lock may take a blackboard read lock, but it is released before the mutation receipt is persisted; no blackboard state write occurs while the integration mutation lock is held.
4. Clean finalization verifies the analysis source commit against HEAD through the same lock boundary, releases the lock, then persists the evidence. A mutation before verification prevents success. A mutation after verification immediately falsifies effective completion through the live HEAD comparison, even if the evidence write completes later.
5. Raw `SprintStatusCompleted` or goal status is never sufficient evidence of successful integration. All completion, archive, proceed, auto-resume, stop, dashboard, and status paths consult the effective decision.
6. `AdvanceSprint` from `CHECKPOINT`, resume from `CHECKPOINT`, resume/archive from `COMPLETED`, manual `Proceed`, auto-resume goal stopping, and sprint-complete wake all reject or defer while effective completion is false or a replacement global generation is pending.
7. A mutation receipt makes the next global generation reproducible and auditable, but correctness already holds at the ref update because all success consumers compare against live HEAD.

### Bounded prompt surfaces

Task 7 replaces the current whole-goal integration context with phase-aware data.

- Slice analyst/reviewer context contains the originating plan and architecture refs, root lineages, descendant task acceptance criteria, attributable commits and paths, ownership/dependency/interface metadata, and commands that read affected paths at the immutable source snapshot.
- Global analyst/reviewer context contains the compact coverage map plus a fresh goal-wide diff at the generation source commit. It explicitly states that coverage records are navigation evidence, not proof of aggregate correctness.
- Reviewer instructions distinguish slice intra-plan composition from global cross-scope, specification, architecture, and merge-readiness judgment.
- Source reads use the task's immutable source commit rather than analyst-report `HEAD`, so interleaved sibling commits cannot distort a slice snapshot.

## Dependency Graph

```text
Task 1 ledger -------+------------------+
Task 2 budget -------+                  |
Task 3 pipeline -----+--> Task 4 resolver+--> Task 5 mutation boundary
                                           \              |
                                            \             v
                                             +-------> Task 6 reconciliation
                                                         /              \
                                                        v                v
                                                Task 7 prompts    Task 8 ops gates
                                                                         |
                                                                         v
                                                               Task 9 wake/status
                                                        \                /
                                                         v              v
                                                           Task 10 E2E
                                                                |
                                                                v
                                                           Task 11 docs
```

Tasks 1-3 are independent. Task 7 and Task 8 can run in parallel after Task 6. Every remaining dependency reflects a consumed interface or required validation order.

## Shared-File and Interface Ownership

| Boundary | Sole owner | Consumers |
|---|---|---|
| Typed integration ledger and analysis metadata | Task 1 | Tasks 4-10 |
| Generation ceiling and default | Task 2 | Tasks 4, 6, 11 |
| Slice/global declarative topology and frozen compatibility capability | Task 3 | Tasks 4, 6, 7, 9, 11 |
| `EvaluateIntegrationProgress` | Task 4 | Tasks 5, 6, 8, 9, 10 |
| Integration mutation receipt and lock protocol | Task 5 | Tasks 6, 8, 10 |
| Reconciliation and verdict projection | Task 6 | Tasks 7-10 |
| Analyst/reviewer bounded context | Task 7 | Task 10 |
| Resume, advance, proceed, checkpoint completion gate | Task 8 | Tasks 9-10 |
| Wake, auto-resume stop, dashboard, and status consumers | Task 9 | Task 10 |
| Cross-component and race evidence | Task 10 | Task 11 |
| Durable documentation and issue disposition | Task 11 | Operators and future maintainers |

No owned file appears in more than one task. Read-only consumers declare dependency ordering and must not redefine the consumed interface.

## Planned Coding Tasks

### Task 1 — Persist the integration evidence ledger

Description: Persist typed integration lifecycle evidence for coverage snapshots, analysis identities, verdicts, and closure state.

Done when: `TestIntegrationLifecycleYAMLRoundTrip` preserves the contributing-set snapshot, coverage union, generation records, mutation receipts, and per-task analysis metadata; `TestIntegrationLifecycleValidation` rejects duplicate analysis keys, mutable cohort replacement, malformed evidence, non-monotonic generations, and clean evidence without an immutable source commit.

Scope: Own `internal/models/integration.go`, `internal/models/integration_test.go`, `internal/models/history.go`, `internal/models/task.go`, `internal/statevalidate/integration.go`, `internal/statevalidate/integration_test.go`, and validation wiring in `internal/statevalidate/validate.go`. Define persistence and validation only; do not derive progress, create tasks, mutate Git, or render prompts.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#proposed-model`

Validation: `go test ./internal/models ./internal/statevalidate -run TestIntegrationLifecycle`

Decomposition:

```yaml
owned_files: [internal/models/integration.go, internal/models/integration_test.go, internal/models/history.go, internal/models/task.go, internal/statevalidate/integration.go, internal/statevalidate/integration_test.go, internal/statevalidate/validate.go]
owned_modules: [internal/models, internal/statevalidate]
read_only_depends_on: []
read_only_task_depends_on: []
interfaces_owned: [IntegrationLifecycle persistence schema, IntegrationAnalysisMetadata persistence schema, integration lifecycle invariant validation]
interfaces_consumed: []
coverage_notes: Durable facts distinguish slice evidence, global generations, immutable source commits, mutation receipts, and blocked or exhausted closure.
```

### Task 2 — Configure the global analysis generation ceiling

Description: Add a configurable global integration generation ceiling with deterministic default `3`.

Done when: `TestGlobalIntegrationGenerationLimitDefaults` proves new workspaces persist `max_global_integration_generations: 3`, legacy zero or negative values normalize to `3`, and positive configured values survive initialization and YAML round-trip without stack-specific assumptions.

Scope: Own `internal/models/config.go`, `internal/models/config_test.go`, `internal/ops/init_project.go`, `internal/ops/init_project_test.go`, `internal/commands/init.go`, `internal/commands/init_test.go`, and `internal/testhelpers/fixtures.go`. Define the configuration field, normalization, and initialization defaults only; do not implement generation decisions or documentation.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Validation: `go test ./internal/models ./internal/ops ./internal/commands -run TestGlobalIntegrationGenerationLimit`

Decomposition:

```yaml
owned_files: [internal/models/config.go, internal/models/config_test.go, internal/ops/init_project.go, internal/ops/init_project_test.go, internal/commands/init.go, internal/commands/init_test.go, internal/testhelpers/fixtures.go]
owned_modules: [internal/models, internal/ops, internal/commands]
read_only_depends_on: []
read_only_task_depends_on: []
interfaces_owned: [Config.MaxGlobalIntegrationGenerations, NormalizeGlobalIntegrationGenerationLimit]
interfaces_consumed: []
coverage_notes: The deterministic default is three total global generations; project initialization stays stack-agnostic.
```

### Task 3 — Activate sliced integration in declarative pipeline topology

Description: Add the slice integration role-pair specialization and a fail-closed frozen-pipeline capability policy.

Done when: `TestSlicedIntegrationPipelineTopology` proves new embedded pipelines expose distinct slice/global role-pairs and finding-to-fix transitions; `TestSlicedIntegrationPipelineLegacyFrozenUpgrade` proves an existing frozen pipeline is not topology-backfilled and reports an actionable `pipeline_upgrade_required` capability result instead of skipping slice coverage.

Scope: Own `internal/embedded/pipeline.yaml`, `internal/pipeline/config.go`, `internal/pipeline/config_test.go`, `internal/pipeline/migrate.go`, `internal/pipeline/migrate_test.go`, `internal/pipeline/resolver.go`, `internal/pipeline/resolver_test.go`, and `internal/testhelpers/pipeline.go`. Reuse existing analyst/reviewer roles, keep distinct lifecycle state names, retain allowed-operation migration, and do not create tasks or edit operator documentation.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#slice-integration`

Validation: `go test ./internal/pipeline -run TestSlicedIntegrationPipeline`

Decomposition:

```yaml
owned_files: [internal/embedded/pipeline.yaml, internal/pipeline/config.go, internal/pipeline/config_test.go, internal/pipeline/migrate.go, internal/pipeline/migrate_test.go, internal/pipeline/resolver.go, internal/pipeline/resolver_test.go, internal/testhelpers/pipeline.go]
owned_modules: [internal/embedded, internal/pipeline]
read_only_depends_on: []
read_only_task_depends_on: []
interfaces_owned: [slice-integration-pair lifecycle, slice-integration-to-fix transition, SlicedIntegrationCapability]
interfaces_consumed: []
coverage_notes: New workspaces receive the topology; legacy frozen workspaces fail closed with explicit fresh-or-manual-update guidance per ADR-0067.
```

### Task 4 — Compute one authoritative integration progress decision

Description: Compute a single deterministic integration progress decision from state, pipeline capability, generation budget, and integration HEAD.

Done when: `TestEvaluateIntegrationProgress` proves partial handoff cannot settle coverage, the cohort freezes exactly once, fewer than two scopes create no slices, one-lineage scopes produce attestations, multi-lineage scopes produce one slice key, escalation plans stay repair lineage, replacements resolve recursively, blocked or abandoned findings block, global readiness waits for all barriers, stale clean evidence is ineffective, and exhausted generations block.

Scope: Own `internal/ops/integration_progress.go` and `internal/ops/integration_progress_test.go`. Implement a pure decision API over Task 1-3 interfaces; do not write state, create tasks, read prompts, or mutate Git.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#required-properties`

Validation: `go test ./internal/ops -run TestEvaluateIntegrationProgress`

Decomposition:

```yaml
owned_files: [internal/ops/integration_progress.go, internal/ops/integration_progress_test.go]
owned_modules: [internal/ops]
read_only_depends_on: [0, 1, 2]
read_only_task_depends_on: []
interfaces_owned: [EvaluateIntegrationProgress, IntegrationProgressDecision, deterministic slice and global analysis keys]
interfaces_consumed: [IntegrationLifecycle persistence schema, NormalizeGlobalIntegrationGenerationLimit, SlicedIntegrationCapability]
coverage_notes: One pure decision closes classification drift across coverage, global readiness, exhaustion, and effective completion.
```

### Task 5 — Linearize integration-HEAD mutation invalidation

Description: Make every integration ref mutation invalidate superseded clean evidence at the mutation linearization point.

Done when: `TestIntegrationMutationLinearization` proves a mutation receipt names the before/after commits, the ref update immediately makes old clean evidence ineffective, receipt persistence occurs only after releasing the integration mutation lock, and clean finalization ordered before or after a racing mutation can never yield effective success for a stale commit.

Scope: Own `internal/ops/integration_mutation_lock.go`, `internal/ops/integration_mutation_lock_test.go`, `internal/ops/wt_merge.go`, and `internal/ops/wt_merge_test.go`. Preserve ADR-0112 lock order, CAS merge behavior, rollback behavior, and the prohibition on blackboard writes under the integration mutation lock; do not own sprint progression or generation reconciliation.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Validation: `go test ./internal/ops -run TestIntegrationMutationLinearization`

Decomposition:

```yaml
owned_files: [internal/ops/integration_mutation_lock.go, internal/ops/integration_mutation_lock_test.go, internal/ops/wt_merge.go, internal/ops/wt_merge_test.go]
owned_modules: [internal/ops]
read_only_depends_on: [0, 3]
read_only_task_depends_on: []
interfaces_owned: [IntegrationMutationReceipt, integration mutation linearization protocol, clean-source verification under the integration mutation lock]
interfaces_consumed: [IntegrationLifecycle persistence schema, EvaluateIntegrationProgress]
coverage_notes: Live HEAD mismatch is the immediate invalidator; durable receipts provide audit and next-generation input after the lock is released.
```

### Task 6 — Reconcile analysis generations from durable progress

Description: Reconcile deterministic slice and global analysis tasks from the authoritative progress decision.

Done when: `TestReconcileIntegrationAnalyses` proves cohort snapshotting and missing-task creation are atomic and idempotent across repeated wake or restart calls; slice verdicts project immutable coverage evidence; global findings wait for resolved repair or replacement lineage; clean verdicts bind to the verified source commit; and slice or generation exhaustion records an explicit blocked state.

Scope: Own `internal/ops/integration_reconcile.go`, `internal/ops/integration_reconcile_test.go`, `internal/ops/submit_verdict.go`, and `internal/ops/submit_verdict_test.go`. Create and project analysis lifecycle state through existing authorization boundaries; do not render prompts, mutate the integration ref, or decide sprint completion independently.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#global-integration`

Validation: `go test ./internal/ops -run TestReconcileIntegrationAnalyses`

Decomposition:

```yaml
owned_files: [internal/ops/integration_reconcile.go, internal/ops/integration_reconcile_test.go, internal/ops/submit_verdict.go, internal/ops/submit_verdict_test.go]
owned_modules: [internal/ops]
read_only_depends_on: [0, 1, 2, 3, 4]
read_only_task_depends_on: []
interfaces_owned: [ReconcileIntegrationAnalyses, analysis verdict projection, idempotent analysis task materialization]
interfaces_consumed: [IntegrationLifecycle persistence schema, Config.MaxGlobalIntegrationGenerations, SlicedIntegrationCapability, EvaluateIntegrationProgress, clean-source verification under the integration mutation lock]
coverage_notes: Deterministic keys and atomic reconciliation prevent duplicate slice or global generations after wake and restart recovery.
```

### Task 7 — Render bounded slice and global analysis context

Description: Render phase-aware immutable review context for slice and global integration analyses.

Done when: `TestSliceIntegrationContext` proves slice prompts contain only the originating plan boundary, descendant acceptance criteria, attributable commits and paths, decomposition metadata, and snapshot reads at the source commit; `TestGlobalIntegrationContext` proves global prompts contain the compact coverage map plus an independent aggregate diff and phase-specific reviewer instructions.

Scope: Own `internal/agent/prompt.go`, `internal/agent/prompt_integration_test.go`, `internal/prompts/role_context.go`, `internal/prompts/role_context_integration_test.go`, `internal/prompts/templates/blocks/branch_integration_context.tmpl`, and `internal/prompts/templates/blocks/review_instructions.tmpl`. Consume persisted analysis metadata; do not classify lineages, create tasks, or alter wake decisions.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#slice-integration`

Validation: `go test ./internal/agent ./internal/prompts -run 'Test(SliceIntegrationContext|GlobalIntegrationContext)'`

Decomposition:

```yaml
owned_files: [internal/agent/prompt.go, internal/agent/prompt_integration_test.go, internal/prompts/role_context.go, internal/prompts/role_context_integration_test.go, internal/prompts/templates/blocks/branch_integration_context.tmpl, internal/prompts/templates/blocks/review_instructions.tmpl]
owned_modules: [internal/agent, internal/prompts]
read_only_depends_on: [0, 2, 5]
read_only_task_depends_on: []
interfaces_owned: [slice analysis prompt projection, global analysis prompt projection, phase-aware integration reviewer instructions]
interfaces_consumed: [IntegrationAnalysisMetadata persistence schema, SlicedIntegrationCapability, ReconcileIntegrationAnalyses]
coverage_notes: Slice context is attributable and immutable; global context remains independently goal-wide.
```

### Task 8 — Gate sprint transitions on effective integration completion

Description: Reject every state-changing sprint progression path while effective integration completion is false.

Done when: `TestEffectiveIntegrationCompletionGate` proves checkpoint-to-completed resume, completed-sprint resume/archive, direct advance, and manual proceed all reject stale clean evidence or a pending replacement generation; the same paths succeed only when the authoritative decision is effectively complete; and invalidation followed immediately by resume or advance cannot archive or complete the sprint.

Scope: Own `internal/ops/pipeline_ops.go`, `internal/ops/pipeline_ops_test.go`, `internal/ops/advance_sprint.go`, `internal/ops/advance_sprint_test.go`, `internal/ops/mode_change.go`, `internal/ops/mode_change_test.go`, `internal/ops/proceed.go`, `internal/ops/proceed_test.go`, `internal/ops/sprint_checkpoint.go`, and `internal/ops/sprint_checkpoint_test.go`. Replace terminal-only completion checks with Task 4's decision; do not implement wake presentation, analysis reconciliation, or Git mutation.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Validation: `go test ./internal/ops -run TestEffectiveIntegrationCompletionGate`

Decomposition:

```yaml
owned_files: [internal/ops/pipeline_ops.go, internal/ops/pipeline_ops_test.go, internal/ops/advance_sprint.go, internal/ops/advance_sprint_test.go, internal/ops/mode_change.go, internal/ops/mode_change_test.go, internal/ops/proceed.go, internal/ops/proceed_test.go, internal/ops/sprint_checkpoint.go, internal/ops/sprint_checkpoint_test.go]
owned_modules: [internal/ops]
read_only_depends_on: [3, 4, 5]
read_only_task_depends_on: []
interfaces_owned: ["state-changing effective-completion gate for checkpoint, resume, archive, advance, and proceed"]
interfaces_consumed: [EvaluateIntegrationProgress, integration mutation linearization protocol, ReconcileIntegrationAnalyses]
coverage_notes: This is the sole owner of every state-changing sprint completion and advance consumer, including the previously unguarded COMPLETED-sprint archive path.
```

### Task 9 — Route supervisor wake and status decisions through effective completion

Description: Route every non-mutating completion consumer and supervisor terminal action through the authoritative progress decision.

Done when: `TestEffectiveIntegrationCompletionConsumers` proves wake detection, orchestrator dashboard instructions, post-run assertions, auto-resume goal stopping, and status diagnostics request reconciliation or report blocked/exhausted while effective completion is false; repeated wake evaluation creates no duplicate generation; and `SPRINT_COMPLETE` or goal-complete stop appears only for clean evidence bound to current HEAD.

Scope: Own `internal/agent/workdetection.go`, `internal/agent/workdetection_test.go`, `internal/agent/systemctl.go`, `internal/agent/systemctl_test.go`, `internal/prompts/builder.go`, `internal/prompts/builder_test.go`, `internal/prompts/wake.go`, `internal/prompts/templates/wake_coding_complete.tmpl`, `internal/commands/status.go`, and `internal/commands/status_test.go`. Consume Task 8's gate and Task 6's reconciliation; do not recreate progress predicates or mutate the integration ref.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#required-properties`

Validation: `go test ./internal/agent ./internal/prompts ./internal/commands -run TestEffectiveIntegrationCompletionConsumers`

Decomposition:

```yaml
owned_files: [internal/agent/workdetection.go, internal/agent/workdetection_test.go, internal/agent/systemctl.go, internal/agent/systemctl_test.go, internal/prompts/builder.go, internal/prompts/builder_test.go, internal/prompts/wake.go, internal/prompts/templates/wake_coding_complete.tmpl, internal/commands/status.go, internal/commands/status_test.go]
owned_modules: [internal/agent, internal/prompts, internal/commands]
read_only_depends_on: [3, 5, 7]
read_only_task_depends_on: []
interfaces_owned: [effective-completion wake projection, supervisor terminal decision, integration lifecycle status projection]
interfaces_consumed: [EvaluateIntegrationProgress, ReconcileIntegrationAnalyses, "state-changing effective-completion gate for checkpoint, resume, archive, advance, and proceed"]
coverage_notes: All remaining success consumers share the same decision; raw terminal task or sprint status cannot independently signal integration success.
```

### Task 10 — Prove the sliced integration lifecycle end to end

Description: Prove the complete sliced integration lifecycle and finalization race through the integration test layer.

Done when: `TestSlicedIntegrationLifecycle` proves the settled boundary, zero-slice bypass, mixed attestation and slice coverage, concurrent slice creation without duplicates, blocked slice fan-in, global fix rescans, generation exhaustion, restart recovery, frozen-pipeline fail-closed behavior, and invalidation followed immediately by resume or advance; `TestSlicedIntegrationFinalizationRace` proves both mutation-before-finalization and mutation-after-finalization orderings never leave effective success tied to stale HEAD.

Scope: Own `internal/integration/sliced_integration_test.go`. Exercise public operations and real Git refs with controlled synchronization; do not change production behavior, weaken existing assertions, or encode stack-specific validation commands.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#success-criteria`

Validation: `go test ./internal/integration -run TestSlicedIntegration`

Decomposition:

```yaml
owned_files: [internal/integration/sliced_integration_test.go]
owned_modules: [internal/integration]
read_only_depends_on: [0, 1, 2, 3, 4, 5, 6, 7, 8]
read_only_task_depends_on: []
interfaces_owned: [end-to-end sliced integration acceptance evidence, controlled finalization race evidence]
interfaces_consumed: [IntegrationLifecycle persistence schema, Config.MaxGlobalIntegrationGenerations, SlicedIntegrationCapability, EvaluateIntegrationProgress, integration mutation linearization protocol, ReconcileIntegrationAnalyses, slice analysis prompt projection, "state-changing effective-completion gate for checkpoint, resume, archive, advance, and proceed", supervisor terminal decision]
coverage_notes: Cross-component tests exercise every success criterion, especially immediate resume or advance after invalidation.
```

### Task 11 — Document sliced integration and close the architectural record

Description: Document the sliced integration contract and update the architectural issue lifecycle after implementation evidence exists.

Done when: ADR-0113 extends ADR-0055 and supersedes its no-rescan limitation; state-machine, task-lifecycle, invariant, configuration, and multi-agent usage docs describe barriers, evidence, generations, exhaustion, linearization, and the frozen-pipeline upgrade policy; the ADR index is updated; and `integration-closure-is-not-revalidated` is resolved or revised with Task 10 validation evidence and traceability.

Scope: Own `specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md`, `specs/architecture/ADR/README.md`, `specs/architecture/state-machines.md`, `specs/protocols/task-lifecycle.md`, `INVARIANTS.md`, `support-docs/CONFIGURATION.md`, `support-docs/USAGE_MULTI_AGENTS.md`, and `specs/architecture/architectural-issues.md`. Document implemented behavior only; preserve issue traceability and explicitly state that legacy frozen pipelines require a fresh workspace or manual topology update.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#documentation-impact`

Validation: `pre-commit run --files specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md specs/architecture/ADR/README.md specs/architecture/state-machines.md specs/protocols/task-lifecycle.md INVARIANTS.md support-docs/CONFIGURATION.md support-docs/USAGE_MULTI_AGENTS.md specs/architecture/architectural-issues.md`

Decomposition:

```yaml
owned_files: [specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md, specs/architecture/ADR/README.md, specs/architecture/state-machines.md, specs/protocols/task-lifecycle.md, INVARIANTS.md, support-docs/CONFIGURATION.md, support-docs/USAGE_MULTI_AGENTS.md, specs/architecture/architectural-issues.md]
owned_modules: [specs/architecture, specs/protocols, support-docs]
read_only_depends_on: [0, 1, 2, 3, 4, 5, 6, 7, 8, 9]
read_only_task_depends_on: []
interfaces_owned: [ADR-0113, sliced integration operator contract, architectural issue disposition]
interfaces_consumed: [all implementation interfaces and Task 10 acceptance evidence]
coverage_notes: Documentation includes the deliberate fail-closed legacy frozen-pipeline policy and updates issue lifecycle only after validation exists.
```

## Systemic Decomposition Review

[CASCADE]

A clean-evidence token is only protective if every success consumer treats it as authoritative. Leaving even one terminal-only path in wake, resume, completed-sprint archive, proceed, auto-stop, or status creates a bypass through which a later HEAD mutation can still reach durable closure.

Implication: Completion-consumer ownership is a single ordered chain across Tasks 8 and 9, with Task 10 attacking invalidation followed immediately by resume and advance.

[FRAGILITY]

The embedded slice topology and a frozen workspace pipeline can diverge because ADR-0067 deliberately excludes role-pair migration. Without an explicit capability state, the same binary would implement different safety guarantees while presenting one feature identity.

Implication: Task 3 owns a fail-closed capability result and Task 11 owns the fresh-or-manual-update operational contract.

[LOAD-BEARING]

An integration analyst's report commit and the integration source commit are different Git objects. Treating the report's review commit as the analyzed branch commit would make final closure compare unrelated identities and would either reject valid evidence or accept stale evidence through accidental equality assumptions.

Implication: Task 1 models the source commit explicitly, Tasks 5-6 verify and project it, and Task 10 tests the distinction.

[FEEDBACK]

Slice reports and coding-review attestations are heterogeneous evidence feeding one global coverage map. If each consumer interprets them independently, new coverage forms will multiply barrier rules and recreate the distributed-truth problem this change is intended to remove.

Implication: Task 1 owns one tagged coverage union and Task 4 is its sole interpreter for progress.

No residual systemic gap remains in the decomposition after applying these four rejection tests. The self-challenge was whether splitting completion consumers across Tasks 8 and 9 reintroduced distributed truth; it does not because Task 4 owns the only predicate, Task 8 owns every state-changing consumer, Task 9 is ordered after Task 8 and owns every remaining consumer, and Task 10 validates the combined boundary.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Bounded per-scope coverage precedes repeated global closure analysis. | Objective, lines 5-15 | Tasks 3, 4, 6, 7 | Covered |
| 2 | Single-lineage coverage is a coding-review attestation with task, criteria, reviewed commit, approver, validation, and merge commit. | Proposed Model, lines 49-56 | Tasks 1, 4, 6 | Covered |
| 3 | A contributing scope is a pre-integration code plan with merged coding lineage; root coding tasks define distinct lineages. | Slice Integration, lines 81-85 | Tasks 1, 4 | Covered |
| 4 | Planning settles only after all coding-producing planning sources are terminal and their eligible outputs and transitions are consumed. | Slice Integration, lines 87-94 | Tasks 4, 6, 9 | Covered |
| 5 | Partial planning handoff cannot open integration coverage. | Required Properties, line 206 | Tasks 4, 10 | Covered |
| 6 | The contributing set is evaluated and frozen exactly once after planning and resulting coding work settle. | Required Properties, lines 207-210 | Tasks 1, 4, 6, 10 | Covered |
| 7 | Fewer than two contributing scopes produce no slice analysis. | Required Properties, line 211 | Tasks 4, 6, 10 | Covered |
| 8 | Multiple contributing scopes each produce one bounded coverage record. | Required Properties, lines 212-213 | Tasks 1, 4, 6, 10 | Covered |
| 9 | One-lineage scopes reuse approval attestations and produce no slice analysis. | Required Properties, lines 214-215 | Tasks 1, 4, 6, 10 | Covered |
| 10 | Multi-lineage scopes with merged work produce exactly one slice analysis. | Required Properties, lines 216-217 | Tasks 3, 4, 6, 10 | Covered |
| 11 | Integration-escalation plans remain repair lineage outside the contributing set and slice creation. | Required Properties, lines 218-219 | Tasks 1, 4, 6, 10 | Covered |
| 12 | Task lineage attributes coding, fixes, and replacements to each slice. | Required Properties, line 220 | Tasks 1, 4, 6 | Covered |
| 13 | Slice context is bounded to its plan, descendants, commits, paths, decomposition metadata, and source snapshot. | Slice Integration, lines 110-126 | Tasks 1, 6, 7, 10 | Covered |
| 14 | Slice findings reuse the integration reviewer and coding-pair fix lifecycle. | Slice Integration, lines 118-121 | Tasks 3, 6, 10 | Covered |
| 15 | Later sibling mutations do not reopen a completed slice. | Slice Integration, lines 123-126 | Tasks 1, 4, 10 | Covered |
| 16 | Slice resolution follows merged fixes or completed replacement lineage; unresolved terminal outcomes block. | Slice Integration, lines 128-132 | Tasks 4, 6, 10 | Covered |
| 17 | Clean slice evidence cannot imply whole-goal completion. | Slice Integration, lines 134-135 | Tasks 1, 4, 8, 9 | Covered |
| 18 | Global analysis waits for settled planning, terminal planned coding and repair work, complete required slice coverage, and resolved slices. | Global Integration, lines 139-143 | Tasks 4, 6, 8, 9, 10 | Covered |
| 19 | A blocked slice prevents global analysis. | Global Integration, line 143 | Tasks 4, 6, 10 | Covered |
| 20 | Global context uses bounded coverage records while independently inspecting the aggregate branch. | Global Integration, lines 145-160 | Tasks 1, 7, 10 | Covered |
| 21 | Promoted integration repairs remain repair lineage and are visible to the next global generation. | Global Integration, lines 162-164 | Tasks 1, 4, 6, 10 | Covered |
| 22 | Global findings require another global pass after resolved repair or replacement work. | Final Closure, lines 168-170 | Tasks 4, 6, 10 | Covered |
| 23 | Integration completes only for clean evidence bound to current integration HEAD. | Final Closure, lines 172-173 | Tasks 1, 4, 5, 8, 9, 10 | Covered |
| 24 | Completion and HEAD mutation have one linearizable order; correctness does not wait for a wake. | Final Closure, lines 175-180 | Tasks 4, 5, 8, 9, 10 | Covered |
| 25 | The integration-HEAD mutation path owns invalidation at its logical linearization point. | Final Closure, lines 182-184 | Tasks 5, 10 | Covered |
| 26 | Finalization preserves ADR-0112 lock order and forbids blackboard writes under the integration mutation lock. | Final Closure, lines 186-188 | Tasks 5, 6, 10 | Covered |
| 27 | HEAD mismatch invalidates evidence and requires another global analysis. | Final Closure, lines 190-192 | Tasks 4, 5, 6, 9, 10 | Covered |
| 28 | Global generations have configurable bound and deterministic default; exhaustion blocks explicitly. | Final Closure, lines 194-198 | Tasks 1, 2, 4, 6, 10 | Covered |
| 29 | Slice exhaustion or unresolved terminal outcomes block before global analysis. | Final Closure, lines 200-202 | Tasks 4, 6, 10 | Covered |
| 30 | Wake evaluation and restart recovery create no duplicate slice or global analysis. | Required Properties, lines 242-243 | Tasks 4, 6, 9, 10 | Covered |
| 31 | Workflow stays stack-agnostic and preserves review and merge authorization. | Required Properties, lines 244-245 | Tasks 2, 3, 5, 6, 10 | Covered |
| 32 | Success criterion 1: no coverage while any planning/output/transition/coding prerequisite remains unsettled. | Success Criteria 1, lines 251-253 | Tasks 4, 6, 10 | Covered |
| 33 | Success criteria 2-3: reproducible cohort classification and complete mixed coverage. | Success Criteria 2-3, lines 254-259 | Tasks 1, 4, 6, 10 | Covered |
| 34 | Success criterion 4: global analysis is unclaimable behind every local barrier. | Success Criteria 4, lines 260-262 | Tasks 4, 6, 9, 10 | Covered |
| 35 | Success criteria 5-6: immutable slice surfaces plus independent aggregate review. | Success Criteria 5-6, lines 263-266 | Tasks 1, 7, 10 | Covered |
| 36 | Success criteria 7-8: clean-current-HEAD finalization and both race orderings. | Success Criteria 7-8, lines 267-273 | Tasks 4, 5, 8, 9, 10 | Covered |
| 37 | Success criterion 9: later mutations rescan within budget and block after exhaustion. | Success Criteria 9, lines 274-275 | Tasks 2, 4, 5, 6, 10 | Covered |
| 38 | Success criterion 10: repeated wake and restart recovery remain duplicate-free. | Success Criteria 10, lines 276-277 | Tasks 4, 6, 9, 10 | Covered |
| 39 | No new agent roles; role-pair specialization is used. | Out of Scope, lines 288-294 | Task 3 | Covered |
| 40 | Add an ADR extending ADR-0055 and superseding its no-rescan limitation. | Documentation Impact, lines 298-301 | Task 11 | Covered |
| 41 | Update state machine and task lifecycle documentation. | Documentation Impact, lines 302-303 | Task 11 | Covered |
| 42 | Update pipeline, operations, configuration, and terminal-outcome documentation. | Documentation Impact, lines 304-305 | Task 11 | Covered |
| 43 | Resolve or revise the integration-closure issue only after validation evidence exists. | Documentation Impact, lines 306-308 | Tasks 10, 11 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | Task 10 | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | Task 11 | Covered |

## Pre-Submit Audit

- Atomicity: each task owns one observable contract; behavior and unit tests are colocated.
- Output completeness: eleven planned tasks map to eleven output entries in the same order.
- Shared files: no `owned_files` value appears in two tasks.
- Dependencies: all read-only interface consumption has a matching `depends_on`; the graph is acyclic.
- Completion-consumer closure: Task 8 owns every state-changing completion/advance path; Task 9 is ordered after Task 8 and owns every remaining wake, supervisor, dashboard, and status path.
- Compatibility closure: Task 3 explicitly owns and tests the legacy frozen-pipeline policy; Task 11 documents it.
- Cross-references: each interface named as consumed is explicitly owned by the cited predecessor.
- Spec coverage: all functional requirements, constraints, acceptance criteria, E2E impact, and documentation impact are covered with no GAP.
