# Code Plan: Master Decomposition Prompt Sections

Status: draft

## Sources Read

- `specs/goals/20260523-master-planning-task.md`
- `specs/arch-plan/20260523-master-planning-task/20260523-105319-architecture-3-b.md`
- `specs/architecture/ADR/README.md`
- `internal/agent/prompt.go`
- `internal/prompts/role_context.go`
- `internal/prompts/builder.go`
- `internal/pipeline/resolver.go`
- `internal/pipeline/config.go`
- `GUARDRAILS.md`

## Implementation Strategy

The implementation should stay as one coding task because the observable behavior is one prompt contract: decomposition-root tasks receive fixed master sections while specialized tasks using the same roles do not. Splitting section selection, data fields, template text, and prompt tests would create serialized shared-file edits without isolating independently useful behavior.

The coder should add master-section selection in `internal/agent/prompt.go` after ordinary role context sections are resolved. The selection must use the active task role-pair, call the existing resolver decomposition-root metadata API, and then append exactly one fixed section for the current role side: `master-decomposition-mandate` for doers and `master-decomposition-review` for reviewers. Resolver or mapping errors for a decomposition-root task must return a prompt construction error rather than rendering incomplete master instructions.

`internal/prompts/role_context.go` should carry prompt-safe master fields such as whether the task is a decomposition root and the required output artifact-ref field. The required ref field is role-pair specific: `plan_ref` for `epic-planning-main-pair`, `arch_ref` for `architecture-main-pair`, and `plan_ref` for `code-planning-main-pair`. Keep the topology decision out of the templates; templates should render the resolved field.

The new mandate and review blocks should be fixed templates under `internal/prompts/templates/blocks/`. They should render the Master Output Contract properties 1-6, typed decomposition metadata expectations, role-appropriate artifact-ref requirements, systemic-thinking obligations, and the doer/reviewer responsibility boundaries. The doer block must require a `Systemic Decomposition Review` artifact section. The reviewer block must require rejection for missing artifact refs, missing typed decomposition metadata, missing systemic-thinking evidence, and violations of properties 1-6.

Focused prompt tests should prove all three master pairs render the correct master instructions and artifact-ref field, all three non-root specialized pairs with the same roles render no master section, role sides do not receive the opposite section, and prompt construction fails closed for a decomposition-root role-pair whose artifact-ref field cannot be determined. Existing prompt packages must still pass with `go test ./internal/agent ./internal/prompts`.

## Task Graph

Task 1 has no sibling dependencies and no concrete `task_depends_on`. The resolver metadata and embedded topology work from `architecture-1-code-planning-0-b` and `architecture-1-code-planning-0-c` is already merged in the current worktree. This task consumes those APIs and topology but does not modify their owned files.

## Coding Tasks

### Task 1: Decomposition-root master prompt sections and prompt tests

**Desc:** Add fixed decomposition-root prompt behavior for master planning tasks: task prompt construction appends `master-decomposition-mandate` for master doers and `master-decomposition-review` for master reviewers based on active task role-pair `decomposition-root: true`, renders the Master Output Contract properties 1-6, typed decomposition metadata expectations, role-appropriate artifact-ref requirements, systemic-thinking obligations, doer/reviewer responsibility boundaries, and focused prompt tests proving specialized tasks using the same roles do not receive master instructions.

**Done when:** Prompts for decomposition-root doers in `epic-planning-main-pair`, `architecture-main-pair`, and `code-planning-main-pair` append `master-decomposition-mandate`, render the Master Output Contract properties 1-6, require a `Systemic Decomposition Review` artifact section, require invoking `systemic-thinking` before `liza set-task-output` or submission, require typed decomposition metadata on each output entry, and render the required output artifact ref as `plan_ref`, `arch_ref`, and `plan_ref` respectively; prompts for decomposition-root reviewers in the same pairs append `master-decomposition-review`, require invoking `systemic-thinking` before verdict, and render rejection criteria for missing artifact refs, missing typed decomposition metadata, missing systemic-thinking evidence, and violations of Master Output Contract properties 1-6; non-root `epic-planning-pair`, `architecture-pair`, and `code-planning-pair` prompts for the same roles render no master section; doer prompts render no `master-decomposition-review` and reviewer prompts render no `master-decomposition-mandate`; prompt construction fails closed for a decomposition-root task whose required output artifact-ref field cannot be determined; focused prompt tests and the existing prompt test packages pass with `go test ./internal/agent ./internal/prompts`.

**Scope:** `internal/agent/prompt.go` for task-role-pair based master section selection; `internal/prompts/role_context.go` for master prompt data fields; new `internal/prompts/templates/blocks/master_decomposition_mandate.tmpl` and `internal/prompts/templates/blocks/master_decomposition_review.tmpl`; focused `internal/agent` and `internal/prompts` tests that verify rendering for decomposition-root and non-root tasks. Consume existing resolver metadata APIs and embedded topology from `architecture-1-code-planning-0-b` and `architecture-1-code-planning-0-c`; do not modify `internal/embedded/pipeline.yaml`, `internal/pipeline/config.go`, or `internal/pipeline/resolver.go` except to consume existing APIs; do not implement `OutputEntry.DecompositionManifest`, `set-task-output` validation, parent artifact-ref mutation, documentation, ADRs, INITIAL_PLANNING wake routing, or broad end-to-end scenarios. This architecture output declares no `task_depends_on`; when the downstream code-planner decomposes coding work, any concrete coding dependencies on `architecture-1-code-planning-0-b` or `architecture-1-code-planning-0-c` must be carried by the code-planner's coding output entries if they are still needed.

**Spec ref:** `specs/goals/20260523-master-planning-task.md`

**Plan ref:** `specs/plans/20260523-master-planning-task/20260523-130003-architecture-3-b-code-planning-0.md`

**Depends on:** none

**Task depends on:** none

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Append `master-decomposition-mandate` for decomposition-root doers in `epic-planning-main-pair`, `architecture-main-pair`, and `code-planning-main-pair`. | Assigned done_when; Goal spec Success Criteria 7; Architecture plan Scope 1 | Task 1 | Covered |
| 2 | Append `master-decomposition-review` for decomposition-root reviewers in the same three master pairs. | Assigned done_when; Goal spec Success Criteria 7; Architecture plan Scope 1 | Task 1 | Covered |
| 3 | Select master sections from active task role-pair `decomposition-root: true`, not from role name or outgoing transition inference. | Goal spec Prompt Differentiation; Goal spec Orchestrator Behavior Change; Architecture plan Interfaces | Task 1 | Covered |
| 4 | Do not introduce `RolePairDef.context-sections` or a generic role-pair context override. | Goal spec Prompt Differentiation; Architecture plan Constraints | Task 1 | Covered |
| 5 | Render Master Output Contract property 1, non-overlapping scopes, as doer obligation and reviewer rejection criterion. | Goal spec Master Output Contract 1; Assigned done_when | Task 1 | Covered |
| 6 | Render Master Output Contract property 2, interface ownership, as doer obligation and reviewer rejection criterion. | Goal spec Master Output Contract 2; Assigned done_when | Task 1 | Covered |
| 7 | Render Master Output Contract property 3, shared-file ownership, as doer obligation and reviewer rejection criterion. | Goal spec Master Output Contract 3; Assigned done_when | Task 1 | Covered |
| 8 | Render Master Output Contract property 4, dependency ordering, as doer obligation and reviewer rejection criterion. | Goal spec Master Output Contract 4; Assigned done_when | Task 1 | Covered |
| 9 | Render Master Output Contract property 5, inherited constraints, including the master artifact ref requirement. | Goal spec Master Output Contract 5; Goal spec Artifact Reference Propagation; Assigned done_when | Task 1 | Covered |
| 10 | Render Master Output Contract property 6, completeness, as doer obligation and reviewer rejection criterion. | Goal spec Master Output Contract 6; Assigned done_when | Task 1 | Covered |
| 11 | Master doer prompt requires a `Systemic Decomposition Review` artifact section. | Assigned done_when; Architecture plan Master Decomposition Mandate Block | Task 1 | Covered |
| 12 | Master doer prompt requires invoking `systemic-thinking` after drafting and before `liza set-task-output` or submission. | Assigned done_when; Goal spec Prompt Differentiation; Architecture plan Master Decomposition Mandate Block | Task 1 | Covered |
| 13 | Master reviewer prompt requires invoking `systemic-thinking` before verdict. | Assigned done_when; Goal spec Prompt Differentiation; Architecture plan Master Decomposition Review Block | Task 1 | Covered |
| 14 | Render typed decomposition metadata expectations with the goal-spec manifest field names on each master output entry. | Assigned done_when; Goal spec Typed Decomposition Manifest; Architecture plan Master Decomposition Mandate Block | Task 1 | Covered |
| 15 | Render required master output artifact-ref fields as `plan_ref`, `arch_ref`, and `plan_ref` for epic, architecture, and code-planning masters. | Assigned done_when; Goal spec Artifact Reference Propagation table; Architecture plan Master Prompt Data | Task 1 | Covered |
| 16 | Reviewer prompt rejects missing artifact refs. | Assigned done_when; Goal spec Reviewer acceptance criteria; Architecture plan Master Decomposition Review Block | Task 1 | Covered |
| 17 | Reviewer prompt rejects missing typed decomposition metadata. | Assigned done_when; Goal spec Typed Decomposition Manifest; Architecture plan Master Decomposition Review Block | Task 1 | Covered |
| 18 | Reviewer prompt rejects missing systemic-thinking evidence. | Assigned done_when; Architecture plan Master Decomposition Review Block | Task 1 | Covered |
| 19 | Reviewer prompt rejects violations of Master Output Contract properties 1-6 even if entries look plausible alone. | Assigned done_when; Goal spec Reviewer acceptance criteria | Task 1 | Covered |
| 20 | Non-root `epic-planning-pair`, `architecture-pair`, and `code-planning-pair` prompts using the same roles render no master section. | Assigned done_when; Architecture plan Specialized task flow | Task 1 | Covered |
| 21 | Doer prompts render no `master-decomposition-review` and reviewer prompts render no `master-decomposition-mandate`. | Assigned done_when; Architecture plan Prompt Tests | Task 1 | Covered |
| 22 | Prompt construction fails closed when a decomposition-root task's required output artifact-ref field cannot be determined. | Assigned done_when; Architecture plan Cross-Cutting Concerns | Task 1 | Covered |
| 23 | Focused prompt tests cover decomposition-root and non-root rendering for master and specialized tasks. | Assigned done_when; Architecture plan Prompt Tests | Task 1 | Covered |
| 24 | Existing prompt test packages pass with `go test ./internal/agent ./internal/prompts`. | Assigned done_when | Task 1 | Covered |
| 25 | Consume existing resolver metadata APIs and embedded topology from merged topology tasks without modifying `internal/embedded/pipeline.yaml`, `internal/pipeline/config.go`, or `internal/pipeline/resolver.go` except API consumption. | Assigned scope; Architecture plan Constraints | Task 1 | Covered |
| 26 | Do not implement `OutputEntry.DecompositionManifest`, `set-task-output` validation, parent artifact-ref mutation, documentation, ADRs, INITIAL_PLANNING wake routing, or broad end-to-end scenarios. | Assigned scope; Architecture plan Constraints | Task 1 | Covered |
| 27 | Carry concrete coding dependencies on topology/resolver implementation tasks only if still needed. | Assigned scope | N/A: `architecture-1-code-planning-0-b` and `architecture-1-code-planning-0-c` are merged into this worktree; no concrete task dependency is still needed. | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this scope is fixed prompt construction and focused prompt-package tests; broad end-to-end scenarios are explicitly out of scope and owned by architecture-5 planning. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: documentation and ADR updates are explicitly out of scope for this coding plan and are owned by architecture-5-code-planning outputs. | N/A |

## Shared-File Audit

Only Task 1 modifies implementation files. No sibling output task shares files with it, so no `depends_on` chain is required inside this plan.

Potential sprint-level overlaps are scope boundaries, not dependencies:

- INITIAL_PLANNING prompt work is owned by `architecture-2-a-code-planning-0` and `architecture-2-b-code-planning-0`; Task 1 must not edit `wake_initial_planning.tmpl`.
- Typed manifest model, validation, and propagation are owned by `architecture-4-code-planning-0`; Task 1 only renders prompt expectations.
- End-to-end scenarios, docs, and ADRs are owned by architecture-5 code-planning tasks and are out of scope here.

## Validation Plan

For this code-planning artifact:

1. Re-read this plan and the output JSON.
2. Verify the output JSON contains exactly one entry and that `desc`, `done_when`, `scope`, `spec_ref`, and `plan_ref` match Task 1.
3. Validate JSON syntax with `jq`.
4. Run `liza set-task-output architecture-3-b-code-planning-0 --output /home/tangi/Workspace/liza/.worktrees/architecture-3-b-code-planning-0/specs/plans/20260523-master-planning-task/20260523-130003-architecture-3-b-code-planning-0-output.json --agent-id code-planner-2 --json`.
5. Run pre-commit on the two generated plan artifacts.
6. Commit only the two generated artifacts.
7. Confirm `git -C /home/tangi/Workspace/liza/.worktrees/architecture-3-b-code-planning-0 status --short` is clean.
8. Submit `HEAD` with `liza submit-for-review architecture-3-b-code-planning-0 HEAD --agent-id code-planner-2 --json`.
