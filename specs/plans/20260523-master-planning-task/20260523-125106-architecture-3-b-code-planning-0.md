# Code Plan: Decomposition-Root Master Prompt Sections

Status: draft

Task: `architecture-3-b-code-planning-0`

Based on:
- Assigned task `architecture-3-b-code-planning-0` from `liza get architecture-3-b-code-planning-0 --json`.
- Goal spec `specs/goals/20260523-master-planning-task.md`.
- Architecture reference `specs/arch-plan/20260523-master-planning-task/20260523-105319-architecture-3-b.md`.
- Current worktree reads of `internal/agent/prompt.go`, `internal/prompts/role_context.go`, `internal/prompts/builder.go`, `internal/pipeline/resolver.go`, and scoped prompt test file discovery.

## Planning Decision

Emit one coding task. The prompt selector, prompt data, two fixed blocks, and focused prompt tests form one prompt-contract behavior: decomposition-root tasks receive master-only instructions while specialized tasks using the same roles do not. Splitting this would create serial dependencies through shared prompt data and tests without giving independent runnable behavior.

ASSUMPTION: No sibling dependency is required for merged resolver/topology work because `architecture-1-code-planning-0-b` and `architecture-1-code-planning-0-c` are already `MERGED` in the blackboard and the current worktree contains `pipeline.Resolver.IsDecompositionRoot`.

## Task 1: Add Decomposition-Root Master Prompt Sections

**Output desc:** Add fixed decomposition-root prompt behavior for master planning tasks: task prompt construction appends `master-decomposition-mandate` for master doers and `master-decomposition-review` for master reviewers based on active task role-pair `decomposition-root: true`, renders the Master Output Contract properties 1-6, typed decomposition metadata expectations, role-appropriate artifact-ref requirements, systemic-thinking obligations, doer/reviewer responsibility boundaries, and focused prompt tests proving specialized tasks using the same roles do not receive master instructions.

**Output scope:** `internal/agent/prompt.go` for task-role-pair based master section selection; `internal/prompts/role_context.go` for master prompt data fields; new `internal/prompts/templates/blocks/master_decomposition_mandate.tmpl` and `internal/prompts/templates/blocks/master_decomposition_review.tmpl`; focused `internal/agent` and `internal/prompts` tests that verify rendering for decomposition-root and non-root tasks. Consume existing resolver metadata APIs and embedded topology from `architecture-1-code-planning-0-b` and `architecture-1-code-planning-0-c`; do not modify `internal/embedded/pipeline.yaml`, `internal/pipeline/config.go`, or `internal/pipeline/resolver.go` except to consume existing APIs; do not implement `OutputEntry.DecompositionManifest`, `set-task-output` validation, parent artifact-ref mutation, documentation, ADRs, INITIAL_PLANNING wake routing, or broad end-to-end scenarios. This architecture output declares no `task_depends_on`; when the downstream code-planner decomposes coding work, any concrete coding dependencies on `architecture-1-code-planning-0-b` or `architecture-1-code-planning-0-c` must be carried by the code-planner's coding output entries if they are still needed.

**Output done_when:** Prompts for decomposition-root doers in `epic-planning-main-pair`, `architecture-main-pair`, and `code-planning-main-pair` append `master-decomposition-mandate`, render the Master Output Contract properties 1-6, require a `Systemic Decomposition Review` artifact section, require invoking `systemic-thinking` before `liza set-task-output` or submission, require typed decomposition metadata on each output entry, and render the required output artifact ref as `plan_ref`, `arch_ref`, and `plan_ref` respectively; prompts for decomposition-root reviewers in the same pairs append `master-decomposition-review`, require invoking `systemic-thinking` before verdict, and render rejection criteria for missing artifact refs, missing typed decomposition metadata, missing systemic-thinking evidence, and violations of Master Output Contract properties 1-6; non-root `epic-planning-pair`, `architecture-pair`, and `code-planning-pair` prompts for the same roles render no master section; doer prompts render no `master-decomposition-review` and reviewer prompts render no `master-decomposition-mandate`; prompt construction fails closed for a decomposition-root task whose required output artifact-ref field cannot be determined; focused prompt tests and the existing prompt test packages pass with `go test ./internal/agent ./internal/prompts`.

**Output spec_ref:** `specs/goals/20260523-master-planning-task.md`

**Output plan_ref:** `specs/plans/20260523-master-planning-task/20260523-125106-architecture-3-b-code-planning-0.md`

**Depends on:** none.

**Existing task dependencies:** none.

### Implementation Notes

- In `internal/agent/prompt.go`, after resolving normal context sections with `resolver.ContextSections(config.Role)`, check the active task role-pair with `resolver.IsDecompositionRoot(task.RolePair)`. Resolver lookup errors must return prompt-build errors.
- Append exactly one master section based on role side: doers append `master-decomposition-mandate`, reviewers append `master-decomposition-review`, and non-root tasks append neither.
- Populate prompt-safe master data in `internal/prompts/role_context.go`, such as `IsDecompositionRootTask`, `MasterOutputRefField`, and `MasterOutputRefPurpose`.
- Resolve the required output artifact-ref field from the decomposition-root role-pair: `epic-planning-main-pair` -> `plan_ref`, `architecture-main-pair` -> `arch_ref`, and `code-planning-main-pair` -> `plan_ref`.
- Fail closed when a role-pair is marked decomposition-root but the required output artifact-ref field cannot be determined. This should be covered by a focused unit test with a synthetic decomposition-root role-pair not in the mapping.
- Keep `BuildRoleContext` as a generic block renderer. It should receive section names and data; it should not inspect pipeline topology.
- Template content must render a visible block marker/name, the Master Output Contract properties 1-6, typed manifest field names, the required artifact-ref field via prompt data, the `Systemic Decomposition Review` artifact-section requirement, and systemic-thinking obligations.
- Tests should include table coverage for the three master pairs and three specialized pairs across doer/reviewer roles. Prefer existing prompt test helpers in `internal/agent/prompt_test.go` for full prompt construction and focused `internal/prompts` block tests where direct block rendering is clearer.
- Validation command for the coder: `go test ./internal/agent ./internal/prompts`. If stale embedded assets cause unrelated Go failures, follow the worktree lesson and run `make -C /home/tangi/Workspace/liza/.worktrees/architecture-3-b-code-planning-0 sync-embedded` before retrying.

## Shared-File Audit

Only Task 1 is emitted, so no sibling task shares files with another planned task.

Potential overlap with other sprint tasks is explicitly bounded:
- `architecture-2-a-code-planning-0` and `architecture-2-b-code-planning-0` own INITIAL_PLANNING routing and wake prompt behavior; Task 1 must not touch `internal/prompts/templates/wake_initial_planning.tmpl`.
- `architecture-4-code-planning-0` owns `OutputEntry.DecompositionManifest`, `set-task-output` validation, child task propagation, and related model/ops changes; Task 1 may render manifest expectations but must not implement schema or validation.
- `architecture-5-code-planning-0` and `architecture-5-code-planning-1` own broad end-to-end scenarios and documentation/ADR updates.

## Validation Plan

1. Re-read this plan and the output JSON and verify `desc`, `done_when`, `scope`, `spec_ref`, and `plan_ref` match exactly.
2. Run `jq . specs/plans/20260523-master-planning-task/20260523-125106-architecture-3-b-code-planning-0-output.json`.
3. Run `liza set-task-output architecture-3-b-code-planning-0 --output /home/tangi/Workspace/liza/.worktrees/architecture-3-b-code-planning-0/specs/plans/20260523-master-planning-task/20260523-125106-architecture-3-b-code-planning-0-output.json --agent-id code-planner-2 --json`.
4. Run pre-commit on the two touched plan artifacts.
5. Commit only the two plan artifacts.
6. Confirm `git -C /home/tangi/Workspace/liza/.worktrees/architecture-3-b-code-planning-0 status --short` is clean.
7. Submit `HEAD` with `liza submit-for-review architecture-3-b-code-planning-0 HEAD --agent-id code-planner-2 --json`.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Planning fan-out uses master tasks that define general approach and decompose into specialized scopes. | Goal spec `Design / One Rule` and `Case B` | Task 1 | Covered |
| 2 | Prompt construction appends fixed master sections only when the active task role-pair has `decomposition-root: true`. | Goal spec `Prompt Differentiation`; architecture `Master Section Selector` | Task 1 | Covered |
| 3 | Master section selection is role-pair based, not role based, so reused specialized roles keep ordinary prompts. | Architecture `Constraints` and `Decision 5` | Task 1 | Covered |
| 4 | No `RolePairDef.context-sections` or generic role-pair context override is introduced. | Goal spec `Prompt Differentiation`; architecture `Decision 2` | Task 1 | Covered |
| 5 | Doer prompts for `epic-planning-main-pair`, `architecture-main-pair`, and `code-planning-main-pair` append `master-decomposition-mandate`. | Assigned `DONE WHEN`; architecture `Master Section Selector` | Task 1 | Covered |
| 6 | Reviewer prompts for the same three master pairs append `master-decomposition-review`. | Assigned `DONE WHEN`; architecture `Master Section Selector` | Task 1 | Covered |
| 7 | Doer prompts never render `master-decomposition-review`; reviewer prompts never render `master-decomposition-mandate`. | Assigned `DONE WHEN`; architecture `Prompt Tests` | Task 1 | Covered |
| 8 | Non-root `epic-planning-pair`, `architecture-pair`, and `code-planning-pair` prompts for reused roles render no master section. | Assigned `DONE WHEN`; architecture `Prompt Tests` | Task 1 | Covered |
| 9 | Master doer block defines responsibility for general approach, boundaries, interface contracts, shared-file ownership, dependency ordering, cross-cutting concerns, and coverage proof. | Architecture `Master Decomposition Mandate Block` | Task 1 | Covered |
| 10 | Master doer block states master doers must not produce detailed specialized plans beyond boundaries/interfaces or implement unrelated pipeline/schema/validation behavior. | Architecture `Master Decomposition Mandate Block` | Task 1 | Covered |
| 11 | Master reviewer block narrows review to decomposition coherence: boundaries, ownership, dependencies, interface contracts, artifact refs, typed manifest evidence, and spec coverage. | Architecture `Master Decomposition Review Block` | Task 1 | Covered |
| 12 | Master Output Contract property 1, non-overlapping scopes, is rendered as doer obligation and reviewer rejection criterion. | Goal spec `Master Output Contract`; assigned `DONE WHEN` | Task 1 | Covered |
| 13 | Master Output Contract property 2, interface ownership, is rendered as doer obligation and reviewer rejection criterion. | Goal spec `Master Output Contract`; assigned `DONE WHEN` | Task 1 | Covered |
| 14 | Master Output Contract property 3, shared-file ownership, is rendered as doer obligation and reviewer rejection criterion. | Goal spec `Master Output Contract`; assigned `DONE WHEN` | Task 1 | Covered |
| 15 | Master Output Contract property 4, dependency ordering, is rendered as doer obligation and reviewer rejection criterion. | Goal spec `Master Output Contract`; assigned `DONE WHEN` | Task 1 | Covered |
| 16 | Master Output Contract property 5, inherited constraints through the master artifact ref, is rendered as doer obligation and reviewer rejection criterion. | Goal spec `Master Output Contract`; assigned `DONE WHEN` | Task 1 | Covered |
| 17 | Master Output Contract property 6, completeness, is rendered as doer obligation and reviewer rejection criterion. | Goal spec `Master Output Contract`; assigned `DONE WHEN` | Task 1 | Covered |
| 18 | Typed decomposition manifest expectations are rendered for each output entry, including owned files/modules, read-only dependencies, interfaces owned/consumed, and coverage notes. | Goal spec `Typed Decomposition Manifest`; architecture `Master Decomposition Mandate Block` | Task 1 | Covered |
| 19 | The required master output artifact-ref field renders as `plan_ref` for epic masters, `arch_ref` for architecture masters, and `plan_ref` for code-planning masters. | Goal spec `Artifact Reference Propagation`; assigned `DONE WHEN` | Task 1 | Covered |
| 20 | Prompt construction fails closed for a decomposition-root task whose required output artifact-ref field cannot be determined. | Assigned `DONE WHEN`; architecture `Master Prompt Data` and `Cross-Cutting Concerns` | Task 1 | Covered |
| 21 | Master doers must invoke `systemic-thinking` after drafting decomposition and before `liza set-task-output` or submission. | Goal spec `Prompt Differentiation`; architecture `Master Decomposition Mandate Block` | Task 1 | Covered |
| 22 | Master artifacts must contain a `Systemic Decomposition Review` section with either no-issue evidence or systemic-thinking findings. | Assigned `DONE WHEN`; architecture `Decision 4` | Task 1 | Covered |
| 23 | Master reviewers must invoke `systemic-thinking` before verdict. | Goal spec `Prompt Differentiation`; assigned `DONE WHEN` | Task 1 | Covered |
| 24 | Reviewers reject missing artifact refs, missing typed decomposition metadata, missing systemic-thinking evidence, and violations of Master Output Contract properties 1-6. | Assigned `DONE WHEN`; architecture `Master Decomposition Review Block` | Task 1 | Covered |
| 25 | Prompt implementation consumes existing resolver metadata APIs and embedded topology from merged architecture-1 implementation tasks. | Assigned `SCOPE`; architecture `References` | Task 1 | Covered |
| 26 | `internal/embedded/pipeline.yaml`, `internal/pipeline/config.go`, and `internal/pipeline/resolver.go` are not modified except consuming existing APIs. | Assigned `SCOPE`; architecture `Constraints` | Task 1 | Covered |
| 27 | `OutputEntry.DecompositionManifest`, `set-task-output` validation, and parent artifact-ref mutation are not implemented here. | Assigned `SCOPE`; architecture `Constraints` | Task 1 | Covered |
| 28 | Documentation, ADRs, INITIAL_PLANNING wake routing, and broad end-to-end scenarios are not implemented here. | Assigned `SCOPE`; architecture `Constraints` | Task 1 | Covered |
| 29 | Focused prompt tests and existing prompt test packages pass with `go test ./internal/agent ./internal/prompts`. | Assigned `DONE WHEN`; architecture `Prompt Tests` | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this scope is internal prompt construction with focused prompt package tests; broad end-to-end scenarios are explicitly out of scope and owned by architecture-5-code-planning-0. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: documentation, ADRs, and user-facing docs are explicitly out of scope here and owned by architecture-5-code-planning-1. | N/A |
