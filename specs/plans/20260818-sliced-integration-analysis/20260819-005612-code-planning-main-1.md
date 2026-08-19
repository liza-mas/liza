# Master Code Plan: Sliced Integration Analysis

## Intent and evidence

Replace the single branch-wide integration pass with bounded per-plan coverage and a bounded global closure loop, while making successful completion true only for clean evidence bound to current integration HEAD.

Success means one authoritative progress decision governs coverage, generation, invalidation, and every completion consumer; every mutable file and inter-task interface has exactly one owner; frozen pipelines fail closed; and each child validation fails when its named behavioral or documentation evidence is absent.

Based on: `specs/goals/20260818-sliced-integration-analysis.md`; ADR-0055, ADR-0059, ADR-0067, ADR-0112; `INVARIANTS.md` §§3, 5-8, 12, 15, and the Protection Matrix; the Update Policy and relevant open issues in `specs/architecture/architectural-issues.md`; Stacklit orientation; Semble lifecycle searches; direct reads of current completion, mutation, pipeline-load, wake, and status paths; and a local proof that the selected Go JSON-event predicate exits 1 for a nonexistent test.

Doc Impact: Task 11 owns the ADR, architecture, protocol, operator, configuration, invariant, and architectural-issue updates.

Test Impact: Tasks 1-9 colocate focused unit tests with behavior changes; Task 10 owns cross-component and controlled-concurrency validation; Task 11 uses fail-closed content assertions plus pre-commit.

## Architecture and boundaries

Durable integration facts live in `internal/models` and are validated by `internal/statevalidate`. Declarative topology remains in `internal/pipeline`. One pure `internal/ops` decision reconstructs the settled contributing cohort, coverage requirements, repair lineage, generation state, and effective completion from durable state, pipeline capability, generation budget, and live integration HEAD. Reconciliation mutates lifecycle state from that decision. Mutation-side invalidation remains at the ADR-0112 lock boundary. Every state-changing and non-mutating completion consumer calls the same decision instead of interpreting terminal states independently.

```text
ledger + config + frozen-pipeline capability
                    |
                    v
       EvaluateIntegrationProgress
          /          |           \
 reconcile      mutation gate    bounded prompts
          \          |           /
           state-changing completion consumers
                         |
           wake/status/supervisor consumers
                         |
                  end-to-end proof
                         |
                    documentation
```

The contributing set freezes once after every pre-integration planning source is terminal, every eligible coding-producing output and transition is consumed, and resulting coding work is terminal. Fewer than two contributing scopes produce no slices. Otherwise one-lineage scopes contribute approval attestations and multi-lineage scopes receive one deterministic slice. Integration-escalation plans remain repair lineage outside the frozen cohort.

Existing frozen `.liza/pipeline.yaml` files retain ADR-0067's no-topology-migration policy. A missing slice capability yields `pipeline_upgrade_required` when slices are required; it never silently skips coverage. Operators must use a fresh workspace or manually update the frozen topology.

The integration ref update is the mutation linearization point. Effective completion compares clean source commit to live HEAD, so a later mutation invalidates success immediately. ADR-0112 lock order remains integration mutation lock then blackboard read lock, with no blackboard write under the integration mutation lock.

## Dependency and ownership graph

```text
Tasks 1 ledger --+
Task 2 budget ----+--> Task 4 decision --> Task 5 mutation
Task 3 pipeline --+          |                  |
                             +-----------------> Task 6 reconcile
                                                    /       \
                                             Task 7 prompts Task 8 progression
                                                                  |
                                                             Task 9 consumers
                                             \                    /
                                              ----> Task 10 E2E
                                                        |
                                                   Task 11 docs
```

Tasks 1-3 are independent. Task 7 and Task 8 may run in parallel after Task 6. No owned file occurs in more than one task.

| Exact interface | Sole owner | Consumers |
|---|---|---|
| `IntegrationLifecycle persistence schema`; `IntegrationAnalysisMetadata persistence schema`; `integration lifecycle invariant validation` | Task 1 | Tasks 4-6, 7, 10, 11 as declared below |
| `Config.MaxGlobalIntegrationGenerations`; `NormalizeGlobalIntegrationGenerationLimit` | Task 2 | Tasks 4, 6, 10, 11 |
| `slice-integration-pair lifecycle`; `slice-integration-to-fix transition`; `SlicedIntegrationCapability` | Task 3 | Tasks 4, 6, 7, 10, 11 |
| `EvaluateIntegrationProgress`; `IntegrationProgressDecision`; `deterministic slice and global analysis keys` | Task 4 | Tasks 5, 6, 8-11 |
| `IntegrationMutationReceipt`; `integration mutation linearization protocol`; `clean-source verification under the integration mutation lock` | Task 5 | Tasks 6, 8, 10, 11 |
| `ReconcileIntegrationAnalyses`; `analysis verdict projection`; `idempotent analysis task materialization` | Task 6 | Tasks 7-11 |
| `slice analysis prompt projection`; `global analysis prompt projection`; `phase-aware integration reviewer instructions` | Task 7 | Tasks 10-11 |
| `state-changing effective-completion gate for checkpoint, resume, archive, advance, and proceed` | Task 8 | Tasks 9-11 |
| `effective-completion wake projection`; `supervisor terminal decision`; `integration lifecycle status projection` | Task 9 | Tasks 10-11 |
| `end-to-end sliced integration acceptance evidence`; `controlled finalization race evidence` | Task 10 | Task 11 |
| `ADR-0113`; `sliced integration operator contract`; `architectural issue disposition` | Task 11 | Operators and maintainers |

## Validation contract

Tasks 1-10 use `go test -json` and `jq -e -s`. A validation succeeds only when every named top-level test emits `Action == "pass"` and no event emits `Action == "fail"`; a selector matching no test exits non-zero. Task 11 uses one content assertion per documentation obligation and retains pre-commit as a separate quality gate. These exact commands are duplicated character-for-character in `output[].validation`.

## Planned coding tasks

### Task 1 — Persist integration evidence

Description: Persist typed integration lifecycle evidence for coverage snapshots, analysis identities, verdicts, and closure state.

Done when: `TestIntegrationLifecycleYAMLRoundTrip` preserves the contributing-set snapshot, coverage union, generation records, mutation receipts, and per-task analysis metadata; `TestIntegrationLifecycleValidation` rejects duplicate analysis keys, mutable cohort replacement, malformed evidence, non-monotonic generations, and clean evidence without an immutable source commit.

Scope: Own `internal/models/integration.go`, `internal/models/integration_test.go`, `internal/models/history.go`, `internal/models/task.go`, `internal/statevalidate/integration.go`, `internal/statevalidate/integration_test.go`, and validation wiring in `internal/statevalidate/validate.go`. Define persistence and validation only; do not derive progress, create tasks, mutate Git, or render prompts.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#proposed-model`

Validation: `go test -json ./internal/models ./internal/statevalidate -run '^(TestIntegrationLifecycleYAMLRoundTrip|TestIntegrationLifecycleValidation)$' -count=1 | jq -e -s '([.[] | select(.Action == "pass" and (.Test == "TestIntegrationLifecycleYAMLRoundTrip" or .Test == "TestIntegrationLifecycleValidation")) | .Test] | unique | sort) == ["TestIntegrationLifecycleValidation","TestIntegrationLifecycleYAMLRoundTrip"] and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/models/integration.go, internal/models/integration_test.go, internal/models/history.go, internal/models/task.go, internal/statevalidate/integration.go, internal/statevalidate/integration_test.go, internal/statevalidate/validate.go]`; `owned_modules=[internal/models, internal/statevalidate]`; `read_only_depends_on=[]`; `read_only_task_depends_on=[]`; `interfaces_owned=[IntegrationLifecycle persistence schema, IntegrationAnalysisMetadata persistence schema, integration lifecycle invariant validation]`; `interfaces_consumed=[]`; coverage: durable facts distinguish slice evidence, global generations, immutable source commits, mutation receipts, and blocked or exhausted closure.

### Task 2 — Configure the global generation ceiling

Description: Add a configurable global integration generation ceiling with deterministic default `3`.

Done when: `TestGlobalIntegrationGenerationLimitDefaults` proves new workspaces persist `max_global_integration_generations: 3`, legacy zero or negative values normalize to `3`, and positive configured values survive initialization and YAML round-trip without stack-specific assumptions.

Scope: Own `internal/models/config.go`, `internal/models/config_test.go`, `internal/ops/init_project.go`, `internal/ops/init_project_test.go`, `internal/commands/init.go`, `internal/commands/init_test.go`, and `internal/testhelpers/fixtures.go`. Define the configuration field, normalization, and initialization defaults only; do not implement generation decisions or documentation.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Validation: `go test -json ./internal/models ./internal/ops ./internal/commands -run '^TestGlobalIntegrationGenerationLimitDefaults$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestGlobalIntegrationGenerationLimitDefaults") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/models/config.go, internal/models/config_test.go, internal/ops/init_project.go, internal/ops/init_project_test.go, internal/commands/init.go, internal/commands/init_test.go, internal/testhelpers/fixtures.go]`; `owned_modules=[internal/models, internal/ops, internal/commands]`; `read_only_depends_on=[]`; `read_only_task_depends_on=[]`; `interfaces_owned=[Config.MaxGlobalIntegrationGenerations, NormalizeGlobalIntegrationGenerationLimit]`; `interfaces_consumed=[]`; coverage: deterministic default is three total generations and initialization stays stack-agnostic.

### Task 3 — Activate sliced integration topology

Description: Add the slice integration role-pair specialization and a fail-closed frozen-pipeline capability policy.

Done when: `TestSlicedIntegrationPipelineTopology` proves new embedded pipelines expose distinct slice/global role-pairs and finding-to-fix transitions; `TestSlicedIntegrationPipelineLegacyFrozenUpgrade` proves an existing frozen pipeline is not topology-backfilled and reports an actionable `pipeline_upgrade_required` capability result instead of skipping slice coverage.

Scope: Own `internal/embedded/pipeline.yaml`, `internal/pipeline/config.go`, `internal/pipeline/config_test.go`, `internal/pipeline/migrate.go`, `internal/pipeline/migrate_test.go`, `internal/pipeline/resolver.go`, `internal/pipeline/resolver_test.go`, and `internal/testhelpers/pipeline.go`. Reuse existing analyst/reviewer roles, keep distinct lifecycle state names, retain allowed-operation migration, and do not create tasks or edit operator documentation.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#slice-integration`

Validation: `go test -json ./internal/pipeline -run '^(TestSlicedIntegrationPipelineLegacyFrozenUpgrade|TestSlicedIntegrationPipelineTopology)$' -count=1 | jq -e -s '([.[] | select(.Action == "pass" and (.Test == "TestSlicedIntegrationPipelineLegacyFrozenUpgrade" or .Test == "TestSlicedIntegrationPipelineTopology")) | .Test] | unique | sort) == ["TestSlicedIntegrationPipelineLegacyFrozenUpgrade","TestSlicedIntegrationPipelineTopology"] and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/embedded/pipeline.yaml, internal/pipeline/config.go, internal/pipeline/config_test.go, internal/pipeline/migrate.go, internal/pipeline/migrate_test.go, internal/pipeline/resolver.go, internal/pipeline/resolver_test.go, internal/testhelpers/pipeline.go]`; `owned_modules=[internal/embedded, internal/pipeline]`; `read_only_depends_on=[]`; `read_only_task_depends_on=[]`; `interfaces_owned=[slice-integration-pair lifecycle, slice-integration-to-fix transition, SlicedIntegrationCapability]`; `interfaces_consumed=[]`; coverage: new workspaces receive topology while legacy frozen workspaces fail closed with fresh-or-manual-update guidance.

### Task 4 — Compute authoritative progress

Description: Compute a single deterministic integration progress decision from state, pipeline capability, generation budget, and integration HEAD.

Done when: `TestEvaluateIntegrationProgress` proves partial handoff cannot settle coverage, the cohort freezes exactly once, fewer than two scopes create no slices, one-lineage scopes produce attestations, multi-lineage scopes produce one slice key, escalation plans stay repair lineage, replacements resolve recursively, blocked or abandoned findings block, global readiness waits for all barriers, stale clean evidence is ineffective, and exhausted generations block.

Scope: Own `internal/ops/integration_progress.go` and `internal/ops/integration_progress_test.go`. Implement a pure decision API over Task 1-3 interfaces; do not write state, create tasks, read prompts, or mutate Git.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#required-properties`

Depends on: Tasks 1, 2, 3.

Validation: `go test -json ./internal/ops -run '^TestEvaluateIntegrationProgress$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestEvaluateIntegrationProgress") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/integration_progress.go, internal/ops/integration_progress_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[0,1,2]`; `read_only_task_depends_on=[]`; `interfaces_owned=[EvaluateIntegrationProgress, IntegrationProgressDecision, deterministic slice and global analysis keys]`; `interfaces_consumed=[IntegrationLifecycle persistence schema, NormalizeGlobalIntegrationGenerationLimit, SlicedIntegrationCapability]`; coverage: one pure decision closes classification drift across coverage, readiness, exhaustion, and effective completion.

### Task 5 — Linearize HEAD invalidation

Description: Make every integration ref mutation invalidate superseded clean evidence at the mutation linearization point.

Done when: `TestIntegrationMutationLinearization` proves a mutation receipt names the before/after commits, the ref update immediately makes old clean evidence ineffective, receipt persistence occurs only after releasing the integration mutation lock, and clean finalization ordered before or after a racing mutation can never yield effective success for a stale commit.

Scope: Own `internal/ops/integration_mutation_lock.go`, `internal/ops/integration_mutation_lock_test.go`, `internal/ops/wt_merge.go`, and `internal/ops/wt_merge_test.go`. Preserve ADR-0112 lock order, CAS merge behavior, rollback behavior, and the prohibition on blackboard writes under the integration mutation lock; do not own sprint progression or generation reconciliation.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Depends on: Tasks 1, 4.

Validation: `go test -json ./internal/ops -run '^TestIntegrationMutationLinearization$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestIntegrationMutationLinearization") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/integration_mutation_lock.go, internal/ops/integration_mutation_lock_test.go, internal/ops/wt_merge.go, internal/ops/wt_merge_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[0,3]`; `read_only_task_depends_on=[]`; `interfaces_owned=[IntegrationMutationReceipt, integration mutation linearization protocol, clean-source verification under the integration mutation lock]`; `interfaces_consumed=[IntegrationLifecycle persistence schema, EvaluateIntegrationProgress]`; coverage: live HEAD mismatch is the immediate invalidator and receipts provide audit after lock release.

### Task 6 — Reconcile analysis generations

Description: Reconcile deterministic slice and global analysis tasks from the authoritative progress decision.

Done when: `TestReconcileIntegrationAnalyses` proves cohort snapshotting and missing-task creation are atomic and idempotent across repeated wake or restart calls; slice verdicts project immutable coverage evidence; global findings wait for resolved repair or replacement lineage; clean verdicts bind to the verified source commit; and slice or generation exhaustion records an explicit blocked state.

Scope: Own `internal/ops/integration_reconcile.go`, `internal/ops/integration_reconcile_test.go`, `internal/ops/submit_verdict.go`, and `internal/ops/submit_verdict_test.go`. Create and project analysis lifecycle state through existing authorization boundaries; do not render prompts, mutate the integration ref, or decide sprint completion independently.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#global-integration`

Depends on: Tasks 1, 2, 3, 4, 5.

Validation: `go test -json ./internal/ops -run '^TestReconcileIntegrationAnalyses$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestReconcileIntegrationAnalyses") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/integration_reconcile.go, internal/ops/integration_reconcile_test.go, internal/ops/submit_verdict.go, internal/ops/submit_verdict_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[0,1,2,3,4]`; `read_only_task_depends_on=[]`; `interfaces_owned=[ReconcileIntegrationAnalyses, analysis verdict projection, idempotent analysis task materialization]`; `interfaces_consumed=[IntegrationLifecycle persistence schema, Config.MaxGlobalIntegrationGenerations, SlicedIntegrationCapability, EvaluateIntegrationProgress, clean-source verification under the integration mutation lock]`; coverage: deterministic keys and atomic reconciliation prevent duplicate generations after wake and restart.

### Task 7 — Render bounded analysis context

Description: Render phase-aware immutable review context for slice and global integration analyses.

Done when: `TestSliceIntegrationContext` proves slice prompts contain only the originating plan boundary, descendant acceptance criteria, attributable commits and paths, decomposition metadata, and snapshot reads at the source commit; `TestGlobalIntegrationContext` proves global prompts contain the compact coverage map plus an independent aggregate diff and phase-specific reviewer instructions.

Scope: Own `internal/agent/prompt.go`, `internal/agent/prompt_integration_test.go`, `internal/prompts/role_context.go`, `internal/prompts/role_context_integration_test.go`, `internal/prompts/templates/blocks/branch_integration_context.tmpl`, and `internal/prompts/templates/blocks/review_instructions.tmpl`. Consume persisted analysis metadata; do not classify lineages, create tasks, or alter wake decisions.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#slice-integration`

Depends on: Tasks 1, 3, 6.

Validation: `go test -json ./internal/agent ./internal/prompts -run '^(TestGlobalIntegrationContext|TestSliceIntegrationContext)$' -count=1 | jq -e -s '([.[] | select(.Action == "pass" and (.Test == "TestGlobalIntegrationContext" or .Test == "TestSliceIntegrationContext")) | .Test] | unique | sort) == ["TestGlobalIntegrationContext","TestSliceIntegrationContext"] and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/agent/prompt.go, internal/agent/prompt_integration_test.go, internal/prompts/role_context.go, internal/prompts/role_context_integration_test.go, internal/prompts/templates/blocks/branch_integration_context.tmpl, internal/prompts/templates/blocks/review_instructions.tmpl]`; `owned_modules=[internal/agent, internal/prompts]`; `read_only_depends_on=[0,2,5]`; `read_only_task_depends_on=[]`; `interfaces_owned=[slice analysis prompt projection, global analysis prompt projection, phase-aware integration reviewer instructions]`; `interfaces_consumed=[IntegrationAnalysisMetadata persistence schema, SlicedIntegrationCapability, ReconcileIntegrationAnalyses]`; coverage: slice context is attributable and immutable while global context stays independently goal-wide.

### Task 8 — Gate state-changing sprint progression

Description: Reject every state-changing sprint progression path while effective integration completion is false.

Done when: `TestEffectiveIntegrationCompletionGate` proves checkpoint-to-completed resume, completed-sprint resume/archive, direct advance, and manual proceed all reject stale clean evidence or a pending replacement generation; the same paths succeed only when the authoritative decision is effectively complete; and invalidation followed immediately by resume or advance cannot archive or complete the sprint.

Scope: Own `internal/ops/pipeline_ops.go`, `internal/ops/pipeline_ops_test.go`, `internal/ops/advance_sprint.go`, `internal/ops/advance_sprint_test.go`, `internal/ops/mode_change.go`, `internal/ops/mode_change_test.go`, `internal/ops/proceed.go`, `internal/ops/proceed_test.go`, `internal/ops/sprint_checkpoint.go`, and `internal/ops/sprint_checkpoint_test.go`. Replace terminal-only completion checks with Task 4's decision; do not implement wake presentation, analysis reconciliation, or Git mutation.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Depends on: Tasks 4, 5, 6.

Validation: `go test -json ./internal/ops -run '^TestEffectiveIntegrationCompletionGate$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestEffectiveIntegrationCompletionGate") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/pipeline_ops.go, internal/ops/pipeline_ops_test.go, internal/ops/advance_sprint.go, internal/ops/advance_sprint_test.go, internal/ops/mode_change.go, internal/ops/mode_change_test.go, internal/ops/proceed.go, internal/ops/proceed_test.go, internal/ops/sprint_checkpoint.go, internal/ops/sprint_checkpoint_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[3,4,5]`; `read_only_task_depends_on=[]`; `interfaces_owned=[state-changing effective-completion gate for checkpoint, resume, archive, advance, and proceed]`; `interfaces_consumed=[EvaluateIntegrationProgress, integration mutation linearization protocol, ReconcileIntegrationAnalyses]`; coverage: sole owner of every state-changing completion and advance consumer.

### Task 9 — Route terminal consumers through effective completion

Description: Route every non-mutating completion consumer and supervisor terminal action through the authoritative progress decision.

Done when: `TestEffectiveIntegrationCompletionConsumers` proves wake detection, orchestrator dashboard instructions, post-run assertions, auto-resume goal stopping, and status diagnostics request reconciliation or report blocked/exhausted while effective completion is false; repeated wake evaluation creates no duplicate generation; and `SPRINT_COMPLETE` or goal-complete stop appears only for clean evidence bound to current HEAD.

Scope: Own `internal/agent/workdetection.go`, `internal/agent/workdetection_test.go`, `internal/agent/systemctl.go`, `internal/agent/systemctl_test.go`, `internal/prompts/builder.go`, `internal/prompts/builder_test.go`, `internal/prompts/wake.go`, `internal/prompts/templates/wake_coding_complete.tmpl`, `internal/commands/status.go`, and `internal/commands/status_test.go`. Consume Task 8's gate and Task 6's reconciliation; do not recreate progress predicates or mutate the integration ref.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#required-properties`

Depends on: Tasks 4, 6, 8.

Validation: `go test -json ./internal/agent ./internal/prompts ./internal/commands -run '^TestEffectiveIntegrationCompletionConsumers$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestEffectiveIntegrationCompletionConsumers") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/agent/workdetection.go, internal/agent/workdetection_test.go, internal/agent/systemctl.go, internal/agent/systemctl_test.go, internal/prompts/builder.go, internal/prompts/builder_test.go, internal/prompts/wake.go, internal/prompts/templates/wake_coding_complete.tmpl, internal/commands/status.go, internal/commands/status_test.go]`; `owned_modules=[internal/agent, internal/prompts, internal/commands]`; `read_only_depends_on=[3,5,7]`; `read_only_task_depends_on=[]`; `interfaces_owned=[effective-completion wake projection, supervisor terminal decision, integration lifecycle status projection]`; `interfaces_consumed=[EvaluateIntegrationProgress, ReconcileIntegrationAnalyses, state-changing effective-completion gate for checkpoint, resume, archive, advance, and proceed]`; coverage: all remaining success consumers share the decision.

### Task 10 — Prove the lifecycle end to end

Description: Prove the complete sliced integration lifecycle and finalization race through the integration test layer.

Done when: `TestSlicedIntegrationLifecycle` proves the settled boundary, zero-slice bypass, mixed attestation and slice coverage, concurrent slice creation without duplicates, blocked slice fan-in, global fix rescans, generation exhaustion, restart recovery, frozen-pipeline fail-closed behavior, and invalidation followed immediately by resume or advance; `TestSlicedIntegrationFinalizationRace` proves both mutation-before-finalization and mutation-after-finalization orderings never leave effective success tied to stale HEAD.

Scope: Own `internal/integration/sliced_integration_test.go`. Exercise public operations and real Git refs with controlled synchronization; do not change production behavior, weaken existing assertions, or encode stack-specific validation commands.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#success-criteria`

Depends on: Tasks 1-9.

Validation: `go test -json ./internal/integration -run '^(TestSlicedIntegrationFinalizationRace|TestSlicedIntegrationLifecycle)$' -count=1 | jq -e -s '([.[] | select(.Action == "pass" and (.Test == "TestSlicedIntegrationFinalizationRace" or .Test == "TestSlicedIntegrationLifecycle")) | .Test] | unique | sort) == ["TestSlicedIntegrationFinalizationRace","TestSlicedIntegrationLifecycle"] and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/integration/sliced_integration_test.go]`; `owned_modules=[internal/integration]`; `read_only_depends_on=[0,1,2,3,4,5,6,7,8]`; `read_only_task_depends_on=[]`; `interfaces_owned=[end-to-end sliced integration acceptance evidence, controlled finalization race evidence]`; `interfaces_consumed=[IntegrationLifecycle persistence schema, Config.MaxGlobalIntegrationGenerations, SlicedIntegrationCapability, EvaluateIntegrationProgress, integration mutation linearization protocol, ReconcileIntegrationAnalyses, slice analysis prompt projection, state-changing effective-completion gate for checkpoint, resume, archive, advance, and proceed, supervisor terminal decision]`; coverage: cross-component tests exercise every success criterion, especially immediate resume or advance after invalidation.

### Task 11 — Document the contract and close the record

Description: Document the sliced integration contract and update the architectural issue lifecycle after implementation evidence exists.

Done when: ADR-0113 extends ADR-0055 and supersedes its no-rescan limitation; state-machine, task-lifecycle, invariant, configuration, and multi-agent usage docs describe barriers, evidence, generations, exhaustion, linearization, and the frozen-pipeline upgrade policy; the ADR index is updated; and `integration-closure-is-not-revalidated` is resolved or revised with Task 10 validation evidence and traceability.

Scope: Own `specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md`, `specs/architecture/ADR/README.md`, `specs/architecture/state-machines.md`, `specs/protocols/task-lifecycle.md`, `INVARIANTS.md`, `support-docs/CONFIGURATION.md`, `support-docs/USAGE_MULTI_AGENTS.md`, and `specs/architecture/architectural-issues.md`. Document implemented behavior only; preserve issue traceability and explicitly state that legacy frozen pipelines require a fresh workspace or manual topology update.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#documentation-impact`

Depends on: Tasks 1-10.

Validation:

- `rg -q 'ADR-0055|0055' specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md`
- `rg -q 'supersed.*no-rescan|no-rescan.*supersed' specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md`
- `rg -q '0113-sliced-integration-analysis-and-final-closure' specs/architecture/ADR/README.md`
- `rg -q 'slice.*barrier|coverage.*barrier' specs/architecture/state-machines.md`
- `rg -q 'coverage.*evidence|evidence.*coverage' specs/architecture/state-machines.md`
- `rg -q 'generation.*exhaust|exhaust.*generation' specs/protocols/task-lifecycle.md`
- `rg -q 'lineariz|clean.*commit.*integration HEAD|integration HEAD.*clean.*commit' INVARIANTS.md`
- `rg -q 'max_global_integration_generations.*3' support-docs/CONFIGURATION.md`
- `python3 -c 'import pathlib,re; text=pathlib.Path("support-docs/USAGE_MULTI_AGENTS.md").read_text(); match=re.search(r"(?ms)^#### Sliced Integration[ \t]*$\n(?P<body>.*?)(?=^#{1,4}[ \t]|\Z)", text); assert match is not None and re.search(r"legacy frozen (?:pipeline|topology)", match.group("body"), re.I) and re.search(r"fresh workspace", match.group("body"), re.I) and re.search(r"manual topology update", match.group("body"), re.I)'`
- `rg -U -q 'Integration Closure Is Not Revalidated(?s).*TestSlicedIntegration(Lifecycle|FinalizationRace)(?s).*commit [0-9a-f]{7,40}' specs/architecture/architectural-issues.md`
- `pre-commit run --files specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md specs/architecture/ADR/README.md specs/architecture/state-machines.md specs/protocols/task-lifecycle.md INVARIANTS.md support-docs/CONFIGURATION.md support-docs/USAGE_MULTI_AGENTS.md specs/architecture/architectural-issues.md`

Decomposition: `owned_files=[specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md, specs/architecture/ADR/README.md, specs/architecture/state-machines.md, specs/protocols/task-lifecycle.md, INVARIANTS.md, support-docs/CONFIGURATION.md, support-docs/USAGE_MULTI_AGENTS.md, specs/architecture/architectural-issues.md]`; `owned_modules=[specs/architecture, specs/protocols, support-docs]`; `read_only_depends_on=[0,1,2,3,4,5,6,7,8,9]`; `read_only_task_depends_on=[]`; `interfaces_owned=[ADR-0113, sliced integration operator contract, architectural issue disposition]`; `interfaces_consumed=[IntegrationLifecycle persistence schema, Config.MaxGlobalIntegrationGenerations, SlicedIntegrationCapability, EvaluateIntegrationProgress, integration mutation linearization protocol, ReconcileIntegrationAnalyses, slice analysis prompt projection, global analysis prompt projection, state-changing effective-completion gate for checkpoint, resume, archive, advance, and proceed, integration lifecycle status projection, end-to-end sliced integration acceptance evidence, controlled finalization race evidence]`; coverage: documents only implemented contracts and disposes the issue after exact acceptance evidence exists.

## Systemic Decomposition Review

[CASCADE]

A clean-evidence token protects closure only if every success consumer uses it. A terminal-only wake, resume, completed-sprint archive, proceed, auto-stop, or status path would let a later HEAD mutation reach durable closure.

Implication: Tasks 8 and 9 form one ordered completion-consumer chain, and Task 10 attacks invalidation followed immediately by resume and advance.

[FRAGILITY]

Frozen topology and embedded topology deliberately diverge under ADR-0067. Without an explicit capability result, one product version would silently offer different safety guarantees per workspace.

Implication: Task 3 owns fail-closed capability detection and Task 11 consumes that exact interface for the upgrade contract.

[LOAD-BEARING]

The analyst report commit and analyzed integration source commit are distinct Git objects. Collapsing them would make closure compare unrelated identities.

Implication: Task 1 persists source identity, Tasks 5-6 verify/project it, and Task 10 tests both finalization orders.

[FRAGILITY]

A successful `go test -run` process is not evidence that its named acceptance test exists. With eleven downstream tasks, that false-green shape would replicate across the full fan-out.

Implication: Tasks 1-10 require exact passing JSON test events and reject any failing event; Task 11 bounds the frozen-pipeline assertion to a `#### Sliced Integration` section and requires both fresh-workspace and manual-topology-update alternatives.

No additional systemic issue remains after checking completion-consumer closure, frozen-pipeline divergence, Git identity separation, interface single ownership, and fail-closed validation.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Bounded per-scope coverage precedes repeated global closure analysis. | Objective | Tasks 3, 4, 6, 7 | Covered |
| 2 | Single-lineage coverage records task, criteria, reviewed commit, approver, validation, and merge commit. | Proposed Model | Tasks 1, 4, 6 | Covered |
| 3 | Contributing scopes and distinct root coding lineages are reproducible. | Slice Integration | Tasks 1, 4 | Covered |
| 4 | Planning settles only after coding-producing sources, outputs, transitions, and resulting coding work settle. | Slice Integration | Tasks 4, 6, 9 | Covered |
| 5 | Partial planning handoff cannot open coverage. | Required Properties | Tasks 4, 10 | Covered |
| 6 | The contributing set freezes exactly once after the settled boundary. | Required Properties | Tasks 1, 4, 6, 10 | Covered |
| 7 | Fewer than two contributing scopes produce no slice. | Required Properties | Tasks 4, 6, 10 | Covered |
| 8 | Multiple contributing scopes each yield bounded coverage. | Required Properties | Tasks 1, 4, 6, 10 | Covered |
| 9 | One-lineage scopes reuse approval attestations without a slice. | Required Properties | Tasks 1, 4, 6, 10 | Covered |
| 10 | Multi-lineage scopes with merged work produce exactly one slice. | Required Properties | Tasks 3, 4, 6, 10 | Covered |
| 11 | Escalation plans remain repair lineage outside the contributing set. | Required Properties | Tasks 1, 4, 6, 10 | Covered |
| 12 | Lineage attributes coding, fixes, and replacements to a slice. | Required Properties | Tasks 1, 4, 6 | Covered |
| 13 | Slice context is bounded to plan, descendants, commits, paths, metadata, and source snapshot. | Slice Integration | Tasks 1, 6, 7, 10 | Covered |
| 14 | Slice findings reuse integration review and coding-fix lifecycle. | Slice Integration | Tasks 3, 6, 10 | Covered |
| 15 | Later sibling mutations do not reopen a completed slice. | Slice Integration | Tasks 1, 4, 10 | Covered |
| 16 | Slice resolution follows merged fix/replacement lineage; unresolved terminal work blocks. | Slice Integration | Tasks 4, 6, 10 | Covered |
| 17 | Clean slice evidence cannot imply whole-goal completion. | Slice Integration | Tasks 1, 4, 8, 9 | Covered |
| 18 | Global analysis waits for all planning, coding, repair, coverage, and resolution barriers. | Global Integration | Tasks 4, 6, 8, 9, 10 | Covered |
| 19 | A blocked slice prevents global analysis. | Global Integration | Tasks 4, 6, 10 | Covered |
| 20 | Global context uses coverage navigation but independently inspects the aggregate branch. | Global Integration | Tasks 1, 7, 10 | Covered |
| 21 | Promoted repairs remain repair lineage visible to the next global generation. | Global Integration | Tasks 1, 4, 6, 10 | Covered |
| 22 | Global findings require another pass after resolved repair/replacement work. | Final Closure | Tasks 4, 6, 10 | Covered |
| 23 | Completion requires clean evidence bound to current integration HEAD. | Final Closure | Tasks 1, 4, 5, 8, 9, 10 | Covered |
| 24 | Completion and mutation have one linearizable order without relying on later wake. | Final Closure | Tasks 4, 5, 8, 9, 10 | Covered |
| 25 | The integration mutation path owns invalidation. | Final Closure | Tasks 5, 10 | Covered |
| 26 | Finalization preserves ADR-0112 lock order and no state write under mutation lock. | Final Closure | Tasks 5, 6, 10 | Covered |
| 27 | HEAD mismatch invalidates evidence and requires another generation. | Final Closure | Tasks 4, 5, 6, 9, 10 | Covered |
| 28 | Global generation bound is configurable with deterministic default and explicit exhaustion. | Final Closure | Tasks 1, 2, 4, 6, 10 | Covered |
| 29 | Slice exhaustion or unresolved terminal outcomes block before global analysis. | Final Closure | Tasks 4, 6, 10 | Covered |
| 30 | Wake/restart recovery cannot duplicate slice or global analyses. | Required Properties | Tasks 4, 6, 9, 10 | Covered |
| 31 | Workflow remains stack-agnostic and preserves review/merge authorization. | Required Properties | Tasks 2, 3, 5, 6, 10 | Covered |
| 32 | No coverage begins while any planning/output/transition/coding prerequisite is unsettled. | Success Criterion 1 | Tasks 4, 6, 10 | Covered |
| 33 | Cohort classification and mixed coverage are reproducible. | Success Criteria 2-3 | Tasks 1, 4, 6, 10 | Covered |
| 34 | Global analysis is unclaimable behind every local barrier. | Success Criterion 4 | Tasks 4, 6, 9, 10 | Covered |
| 35 | Slice surfaces are immutable and global review remains independent. | Success Criteria 5-6 | Tasks 1, 7, 10 | Covered |
| 36 | Finalization is clean/current-HEAD under both race orders. | Success Criteria 7-8 | Tasks 4, 5, 8, 9, 10 | Covered |
| 37 | Later mutations rescan within budget and block after exhaustion. | Success Criterion 9 | Tasks 2, 4, 5, 6, 10 | Covered |
| 38 | Repeated wake/restart evaluation remains duplicate-free. | Success Criterion 10 | Tasks 4, 6, 9, 10 | Covered |
| 39 | No new roles; specialization uses role-pairs. | Out of Scope | Task 3 | Covered |
| 40 | ADR-0113 extends ADR-0055 and supersedes no-rescan. | Documentation Impact | Task 11 | Covered |
| 41 | State-machine and task-lifecycle docs are updated. | Documentation Impact | Task 11 | Covered |
| 42 | Pipeline, operations, configuration, and terminal-outcome docs are updated. | Documentation Impact | Task 11 | Covered |
| 43 | Integration-closure issue changes only after validation evidence exists. | Documentation Impact | Tasks 10, 11 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | Task 10 | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | Task 11 | Covered |

## Pre-submit audit

- Eleven atomic task entries match eleven `output[]` entries in order.
- Every `owned_files` path has one owner; readers are dependency-ordered.
- Every consumed interface above is named exactly as an interface owned by one predecessor; Task 11 has no catch-all consumption.
- Dependency indices are acyclic and match `read_only_depends_on`.
- Tasks 1-10 reject absent named tests; Task 11 asserts each documentation obligation and then runs pre-commit.
- All functional requirements, constraints, success criteria, E2E impact, and documentation impact are covered with no GAP.
