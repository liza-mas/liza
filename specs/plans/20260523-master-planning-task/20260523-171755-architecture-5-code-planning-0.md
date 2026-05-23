# Code Plan: Master Planning Acceptance Validation

## Sources

Based on:
- Blackboard task `architecture-5-code-planning-0` read with `liza get architecture-5-code-planning-0 --json`.
- Goal spec `specs/goals/20260523-master-planning-task.md`.
- Architecture reference `specs/arch-plan/20260523-master-planning-task/20260523-110234-architecture-5.md`.
- Dependency output summaries for `architecture-2-a-code-planning-0`, `architecture-2-b-code-planning-0-repair-0`, `architecture-3-b-code-planning-0`, `architecture-4-code-planning-0-a`, `architecture-4-code-planning-0-b-repair-0`, `architecture-4-code-planning-0-c-repair-0`, and `architecture-4-code-planning-0-d-repair-0`.

Success means this plan decomposes the acceptance-validation scope into reviewable coding tasks whose tests collectively prove success criteria 1-8 without implementing production behavior or documentation in this planning task.

I will validate by checking output JSON syntax with `jq`, submitting the exact output entries through `liza set-task-output`, reviewing the worktree diff, running pre-commit on touched artifacts, committing only the plan/output repair, confirming a clean worktree, and running `liza submit-for-review architecture-5-code-planning-0 HEAD --agent-id code-planner-1 --json`.

Doc Impact: N/A for this code-planning task. Documentation updates are owned by sibling task `architecture-5-code-planning-1-repair-0`.

Test Impact: Covered by the planned downstream coding tasks below. This code-planning artifact does not add executable tests directly.

## Task Graph

### Task 1: CLI Entry-Point And Pipeline Validation

desc: Add CLI-level acceptance tests and validation matrix coverage proving the embedded master-planning pipeline validates, all four entry points expose one specialized simple route and one mapped master fan-out route, and INITIAL_PLANNING rendering remains a one-task contract.

done_when: `cmd/liza/json_wiring_test.go` includes a focused `validate --json` regression that initializes from the embedded pipeline and asserts the JSON envelope is successful after the master role-pairs, decomposition-root metadata, auto decompose transitions, and `us-to-coding` retargeting are present; `cmd/liza/cmd_init_test.go` or `cmd/liza/json_wiring_test.go` includes table-driven coverage for `general-objective`, `functional-spec`, `detailed-spec`, and `technical-spec` showing each entry point has exactly one specialized simple target and exactly one mapped master fan-out target; the test renders or inspects the same INITIAL_PLANNING route data used by the CLI path and fails if simple, fan-out, or missing-master examples contain multiple task objects, old multi-task fan-out text, an invented master route, or the wrong role-pair/task type; the validation matrix in this plan maps success criteria 1, 2, and 8 to these tests and records `liza validate --json`, `go test ./cmd/liza -run 'TestJSON_Validate.*MasterPlanning|TestInitDispatch_.*InitialPlanning|TestJSON_.*InitialPlanning'`, and `go test ./internal/prompts/...` as required commands.

scope: Modify only focused CLI tests in `cmd/liza/cmd_init_test.go` and `cmd/liza/json_wiring_test.go`, plus test-local helpers in those files if needed. The tests may call existing prompt or pipeline APIs to inspect route data, but must not modify `internal/prompts`, `internal/pipeline`, embedded pipeline YAML, production init logic, docs, ADRs, ops transition code, or integration test files.

spec_ref: specs/goals/20260523-master-planning-task.md

task_depends_on: `architecture-2-a-code-planning-0-coding-0-repair-0`, `architecture-2-a-code-planning-0-coding-1`, `architecture-2-b-code-planning-0-repair-0-coding-0-repair-0`

### Task 2: Ops Master Decomposition And Case A Validation

desc: Add ops-layer acceptance tests proving approved master planning tasks auto-decompose into specialized children with dependency, artifact, and typed decomposition metadata propagation, while `architecture-to-code-plan` continues to bypass `code-planning-main-pair`.

done_when: `internal/ops/proceed_test.go` includes table-driven coverage for `epic-planning-main-pair`, `architecture-main-pair`, and `code-planning-main-pair` approved or merged master tasks where `ExecuteAvailableTransitions` or `Proceed` fires the auto decompose transition and creates exactly one specialized child per `output[]` entry; each generated child assertion verifies role-pair, initial status, `depends_on` sibling ID resolution, external `task_depends_on`, copied `decomposition` metadata, copied `kind`, copied `plan_ref` or `arch_ref` from the master output entry according to the propagation table, and preserved parent/task relationships; the same file includes a Case A regression proving an `architecture-pair` source executing `architecture-to-code-plan` creates only `code-planning-pair` children and no `code-planning-main-pair` task; downstream ref assertions prove coding children inherit relevant `plan_ref` or `arch_ref` behavior and story-writing children still consume specialized epic `epic_ref`; the validation matrix maps success criteria 3, 4, and 6 to these tests and records `go test ./internal/ops -run 'TestProceed.*MasterPlanning|TestExecuteAvailableTransitions.*MasterPlanning|TestProceed.*ArchitectureToCodePlan'` as a required command.

scope: Modify only `internal/ops/proceed_test.go`. Reuse existing proceed helpers, fixture pipeline setup, `models.OutputEntry`, and `models.DecompositionManifest`; do not modify `internal/ops/proceed.go`, `internal/ops/set_task_output.go`, models, command projections, prompt templates, embedded pipeline YAML, docs, ADRs, CLI tests, or integration tests.

spec_ref: specs/goals/20260523-master-planning-task.md

task_depends_on: `architecture-4-code-planning-0-a-coding-0-repair-0`, `architecture-4-code-planning-0-b-repair-0-coding-1`, `architecture-4-code-planning-0-c-repair-0-coding-1`

### Task 3: Integration Quorum, Prompt, And Full Sprint Validation

desc: Add integration acceptance tests proving master planning review quorum, prompt differentiation, and full sprint transition behavior compose with master decomposition without regressing specialized prompt or downstream artifact behavior.

done_when: `internal/integration/e2e_workflow_test.go` includes a master planning lifecycle test that creates a decomposition-root task, sets valid typed master outputs, submits it, drives two approved verdicts, asserts the first approval reaches the configured partially-approved state and the second reaches the approved state, merges or resumes as appropriate, and verifies the auto decompose transition creates specialized children; the test captures or renders master doer and reviewer prompts and asserts `master-decomposition-mandate`, `master-decomposition-review`, Master Output Contract properties 1-6, role-appropriate artifact refs, and systemic-thinking obligations appear only for decomposition-root tasks, while specialized tasks using the same doer/reviewer roles do not contain those sections; `internal/integration/full_sprint_test.go` is updated so the full sprint flow reflects the new `us-to-coding` architecture master step, quorum-2 master review behavior, auto decomposition into specialized architecture tasks, Case A `architecture-to-code-plan` bypass into `code-planning-pair`, and unchanged downstream `epic_ref` and `arch_ref` propagation; the validation matrix maps success criteria 5, 6, and 7 to these tests and records `go test ./internal/integration -run 'TestMasterPlanning|TestFullSprintSequence'` as a required command.

scope: Modify only `internal/integration/e2e_workflow_test.go` and `internal/integration/full_sprint_test.go`. Reuse existing integration setup, supervisor mock, lifecycle helpers, command/ops APIs, and prompt capture patterns; do not modify production agent, prompt, pipeline, ops, CLI, model, docs, ADR, or lower-level unit test files.

spec_ref: specs/goals/20260523-master-planning-task.md

task_depends_on: `architecture-2-b-code-planning-0-repair-0-coding-0-repair-0`, `architecture-3-b-code-planning-0-coding-0-repair-1`, `architecture-4-code-planning-0-b-repair-0-coding-1`, `architecture-4-code-planning-0-c-repair-0-coding-1`

## Dependency And Shared-File Audit

The three tasks touch disjoint file sets:

| Task | Files |
|------|-------|
| Task 1 | `cmd/liza/cmd_init_test.go`, `cmd/liza/json_wiring_test.go` |
| Task 2 | `internal/ops/proceed_test.go` |
| Task 3 | `internal/integration/e2e_workflow_test.go`, `internal/integration/full_sprint_test.go` |

No sibling `depends_on` chain is required because no file is shared between planned tasks. Each task declares `task_depends_on` for the concrete upstream implementation slices it needs before executable acceptance tests can pass.

## Validation Matrix

| Success Criterion | Required Evidence | Planned Task(s) | Commands |
|-------------------|-------------------|-----------------|----------|
| 1. Pipeline validation passes | Embedded pipeline with master role-pairs, decomposition-root metadata, auto decompose transitions, and retargeted `us-to-coding` validates through the CLI JSON surface. | Task 1 | `liza validate --json`; `go test ./cmd/liza -run 'TestJSON_Validate.*MasterPlanning'` |
| 2. Entry-point routing | `general-objective`, `functional-spec`, `detailed-spec`, and `technical-spec` each expose exactly one specialized simple target and exactly one mapped master fan-out target. | Task 1 | `go test ./cmd/liza -run 'TestInitDispatch_.*InitialPlanning|TestJSON_.*InitialPlanning'` |
| 3. Intra-subpipeline transition | Approved master outputs auto-decompose into exactly one specialized child per `output[]` entry with dependency, ref, typed decomposition, and status assertions. | Task 2, Task 3 | `go test ./internal/ops -run 'TestProceed.*MasterPlanning|TestExecuteAvailableTransitions.*MasterPlanning'`; `go test ./internal/integration -run 'TestMasterPlanning'` |
| 4. Case A bypass | `architecture-to-code-plan` from specialized `architecture-pair` creates `code-planning-pair` children and never creates `code-planning-main-pair`. | Task 2, Task 3 | `go test ./internal/ops -run 'TestProceed.*ArchitectureToCodePlan'`; `go test ./internal/integration -run 'TestFullSprintSequence'` |
| 5. Quorum-2 behavior | First approved verdict reaches the configured partially-approved state; second approved verdict reaches the approved state. | Task 3 | `go test ./internal/integration -run 'TestMasterPlanning'` |
| 6. Artifact propagation | Specialized planning children inherit master `plan_ref` or `arch_ref`; downstream coding/story-writing ref behavior remains unchanged; missing required master refs are covered by upstream set-task-output validation dependencies. | Task 2, Task 3 | `go test ./internal/ops -run 'TestProceed.*MasterPlanning|TestExecuteAvailableTransitions.*MasterPlanning'`; `go test ./internal/integration -run 'TestMasterPlanning|TestFullSprintSequence'` |
| 7. Prompt differentiation | Master doer/reviewer prompts contain fixed master sections, Master Output Contract properties 1-6, artifact-ref expectations, and systemic-thinking obligations; specialized tasks using the same roles do not contain those sections. | Task 3 | `go test ./internal/integration -run 'TestMasterPlanning'`; `go test ./internal/prompts/...` |
| 8. Orchestrator simplification | INITIAL_PLANNING no longer renders or instructs multi-task fan-out; simple, fan-out, uncertain, and missing-master examples are one-task contracts. | Task 1 | `go test ./cmd/liza -run 'TestInitDispatch_.*InitialPlanning|TestJSON_.*InitialPlanning'`; `go test ./internal/prompts/...` |

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Every planning fan-out is preceded by a consolidated master task unless an upstream pipeline step already provides consolidation. | Goal spec "Design / One Rule" | Task 1, Task 2, Task 3 | Covered |
| 2 | Entry-point work creates exactly one specialized task for simple planning work. | Goal spec "Case A / Case B Routing" and "Success Criteria / 2" | Task 1 | Covered |
| 3 | Entry-point work creates exactly one mapped master task for fan-out or uncertain planning work. | Goal spec "Case A / Case B Routing", "Orchestrator Behavior Change", and "Success Criteria / 2, 8" | Task 1 | Covered |
| 4 | `general-objective`, `functional-spec`, `detailed-spec`, and `technical-spec` entry points are all covered. | Goal spec "Success Criteria / 2" | Task 1 | Covered |
| 5 | `liza validate` accepts updated pipeline topology with master role-pairs, auto-decompose transitions, decomposition-root metadata, and retargeted `us-to-coding`. | Goal spec "Success Criteria / 1" | Task 1 | Covered |
| 6 | Approved master tasks auto-decompose into one specialized task per `output[]` entry. | Goal spec "Success Criteria / 3" | Task 2, Task 3 | Covered |
| 7 | Auto-decomposed children preserve correct `depends_on`, artifact refs, typed decomposition metadata, and target statuses. | Goal spec "Success Criteria / 3" and "Typed Decomposition Manifest" | Task 2, Task 3 | Covered |
| 8 | `architecture-to-code-plan` remains Case A and lands directly on `code-planning-pair`, not `code-planning-main-pair`. | Goal spec "Case A / Case B Routing" and "Success Criteria / 4" | Task 2, Task 3 | Covered |
| 9 | Master role-pairs require quorum 2, including partially-approved state after first approval and approved state after second approval. | Goal spec "Role-Pairs" and "Success Criteria / 5" | Task 3 | Covered |
| 10 | Specialized planning tasks inherit the master's `plan_ref` or `arch_ref` according to the propagation table. | Goal spec "Artifact Reference Propagation" and "Success Criteria / 6" | Task 2, Task 3 | Covered |
| 11 | Downstream coding tasks inherit relevant planning or architecture artifacts transitively, and downstream story-writing preserves specialized epic `epic_ref` behavior. | Goal spec "Artifact Reference Propagation" and "Success Criteria / 6" | Task 2, Task 3 | Covered |
| 12 | Master doer prompts include `master-decomposition-mandate`; master reviewer prompts include `master-decomposition-review`. | Goal spec "Prompt Differentiation" and "Success Criteria / 7" | Task 3 | Covered |
| 13 | Master prompt sections include Master Output Contract properties 1-6, artifact-ref expectations, typed decomposition metadata expectations, and systemic-thinking obligations. | Goal spec "Prompt Differentiation", "Master Output Contract", and "Success Criteria / 7" | Task 3 | Covered |
| 14 | Specialized tasks using the same roles do not receive master prompt sections. | Goal spec "Prompt Differentiation" and "Success Criteria / 7" | Task 3 | Covered |
| 15 | INITIAL_PLANNING no longer renders or produces multi-task fan-out. | Goal spec "Orchestrator Behavior Change" and "Success Criteria / 8" | Task 1 | Covered |
| 16 | Acceptance validation documents exact commands, including targeted `go test` package commands and `liza validate --json`, and those commands exit 0. | Assigned task done_when | Task 1, Task 2, Task 3 | Covered |
| 17 | Scope is limited to existing tests in `internal/integration/e2e_workflow_test.go`, `internal/integration/full_sprint_test.go`, `internal/ops/proceed_test.go`, and focused CLI tests in `cmd/liza/cmd_init_test.go` or `cmd/liza/json_wiring_test.go` as needed. | Assigned task scope | Task 1, Task 2, Task 3 | Covered |
| 18 | Do not implement docs, ADRs, new migration logic, or additional domain unit tests already owned by sibling scopes. | Assigned task scope | Task 1, Task 2, Task 3 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | Task 3 covers integration lifecycle/full-sprint e2e behavior; Task 1 and Task 2 cover CLI and ops acceptance layers | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: documentation and ADR work is explicitly out of scope here and owned by sibling task `architecture-5-code-planning-1-repair-0` | N/A |

## Commands For Downstream Validation

The coding tasks must document and run these commands, with command names adjusted only if the final test names differ while preserving package and behavior coverage:

- `liza validate --json`
- `go test ./cmd/liza -run 'TestJSON_Validate.*MasterPlanning|TestInitDispatch_.*InitialPlanning|TestJSON_.*InitialPlanning'`
- `go test ./internal/ops -run 'TestProceed.*MasterPlanning|TestExecuteAvailableTransitions.*MasterPlanning|TestProceed.*ArchitectureToCodePlan'`
- `go test ./internal/integration -run 'TestMasterPlanning|TestFullSprintSequence'`
- `go test ./internal/prompts/...`

## Pre-Submit Self-Check

- Each planned task has one intent and a disjoint write scope.
- Each `done_when` is falsifiable and tied to specific test files, assertions, and commands.
- The spec compliance matrix maps success criteria 1-8 and assigned-scope constraints to planned tasks.
- Output entries are one-to-one with Task 1 through Task 3.
- Shared-file audit finds no shared files between tasks, so no sibling `depends_on` entries are required.
- The stale preservation artifact `20260523-153530-architecture-5-code-planning-0.md` is intentionally removed so the submitted diff contains one current plan artifact for this task.
