# Code Plan: Per-Subtask Child Decomposition Propagation Repair

## Context

This plan covers the per-subtask child creation propagation slice for the master planning task pattern after the artifact-ref preservation race. The model surface is owned by `architecture-4-code-planning-0-a` and its replacement coding task. Decomposition-root `set-task-output` validation is owned by `architecture-4-code-planning-0-b-repair-0`. Inspect projection work is owned by `architecture-4-code-planning-0-d-repair-0`.

This slice only changes the child-task builder path that converts a validated `models.OutputEntry` into generated child `models.Task` records during per-subtask transitions. It consumes `models.DecompositionManifest` after the model-surface task lands and keeps `buildChildTask` as a copier of already-validated state, not a second validation layer.

Active output-summary checks for active tasks returned no populated `output[]` refs at planning time, so there were no active blackboard output artifact refs to add back for this repair. The worktree still contains the previously repaired sibling preservation artifacts for historical blackboard refs:

- `specs/plans/20260523-master-planning-task/20260523-142631-architecture-4-code-planning-0-b.md`
- `specs/plans/20260523-master-planning-task/20260523-142523-architecture-4-code-planning-0-c.md`

These files are preservation artifacts only. They are not in the generated coding task scope.

## Task 1: Propagate decomposition metadata through per-subtask child creation

desc:
Propagate typed decomposition metadata from per-subtask output entries into generated child tasks while preserving existing ref, kind, dependency, and fallback behavior.

done_when:
`internal/ops/proceed.go` copies `models.OutputEntry.Decomposition` into generated child `models.Task.Decomposition` for per-subtask transitions; per-subtask child creation still copies `Kind`, direct `PlanRef`, direct `ArchRef`, direct `EpicRef`, sibling `depends_on` IDs, external `task_depends_on`, and inherited phase-gate dependencies; duplicate dependencies from sibling, external, and inherited sources are removed without changing stable ordering; parent `arch_ref` and `epic_ref` fallback behavior remains covered; parent `plan_ref` fallback is not introduced and a focused test proves output entries without `plan_ref` still create per-subtask children with empty `Task.PlanRef`; focused tests in `internal/ops/proceed_test.go` prove decomposition propagation, ref propagation, dependency merge behavior, inherited phase-gate dependencies, preserved parent `arch_ref`/`epic_ref` fallback, and empty parent `plan_ref` fallback; `go test ./internal/ops -run 'TestProceed'` passes.

scope:
Modify only `internal/ops/proceed.go` and focused tests in `internal/ops/proceed_test.go`. Consume `models.DecompositionManifest`, `models.OutputEntry.Decomposition`, and `models.Task.Decomposition` from the model-surface task. Keep validation responsibility in `set-task-output`; do not add set-task-output validation, command projection changes, pipeline topology, prompt templates, docs, ADRs, end-to-end tests, model schema changes, or unrelated proceed behavior.

spec_ref:
specs/goals/20260523-master-planning-task.md

plan_ref:
specs/plans/20260523-master-planning-task/20260523-145900-architecture-4-code-planning-0-c-repair-0.md

### Implementation Notes

- Add `Decomposition: entry.Decomposition` to the `models.Task` literal returned by `buildChildTask`.
- Do not deep-copy the manifest unless existing model conventions require it after `architecture-4-code-planning-0-a-coding-0-repair-0` lands; the persisted state path serializes tasks immediately, and this task should match the model task/output pointer conventions introduced there.
- Leave artifact-ref behavior unchanged:
  - `Task.PlanRef` comes only from `entry.PlanRef`.
  - `Task.ArchRef` comes from `entry.ArchRef`, falling back to parent `ArchRef` only when the entry is empty.
  - `Task.EpicRef` comes from `entry.EpicRef`, falling back to parent `EpicRef` only when the entry is empty.
- Leave dependency construction unchanged except for focused regression coverage:
  - sibling `depends_on` indices resolve through `siblingIDs`;
  - `entry.TaskDependsOn` appends concrete task IDs;
  - inherited phase-gate dependencies append after entry dependencies;
  - `dedupeStrings` removes duplicates.
- Add focused tests near the existing per-subtask propagation and dependency tests rather than creating a new integration-style fixture.
- Tests should use the existing `setupPipelineProceedTest`, `testhelpers.CreateValidState`, `testhelpers.BuildTaskByStatus`, `Proceed`, `proceedInner`, and `db.New(...).Read()` patterns already used in `internal/ops/proceed_test.go`.
- For the no parent `plan_ref` fallback regression, create a parent task with `PlanRef` populated and output entries with empty `PlanRef`; after `code-plan-to-coding`, assert generated children have empty `PlanRef`.
- For dependency duplicate removal, construct an output entry whose sibling dependency and external `task_depends_on` overlap with inherited phase-gate dependencies, then assert the child `DependsOn` contains each ID once in first-seen order.

## Dependency Plan

Task 1 has no sibling dependencies because this plan emits a single coding task.

Task 1 has an external task dependency on `architecture-4-code-planning-0-a-coding-0-repair-0` because that active replacement coding task introduces `models.DecompositionManifest`, `models.OutputEntry.Decomposition`, and `models.Task.Decomposition`, which are compile-time prerequisites for this propagation change.

No dependency is required on `architecture-4-code-planning-0-d-repair-0-coding-0` because inspect projection consumes the same model surface but does not provide behavior needed by `proceed.go`.

No dependency is required on the `set-task-output` validation coding slice from `architecture-4-code-planning-0-b-repair-0` because this task does not call or depend on the stricter validator implementation. `buildChildTask` continues to assume persisted output entries are already valid.

## Shared-File Audit

Only Task 1 modifies `internal/ops/proceed.go` and `internal/ops/proceed_test.go`, so no intra-plan `depends_on` chain is required.

The generated coding task shares `internal/ops/proceed_test.go` with sibling `architecture-5-code-planning-0`, but `architecture-5-code-planning-0` already depends on this planning task in the sprint graph. This plan therefore does not add a direct output dependency on that sibling.

## Artifact Preservation

Current active output-summary checks for active tasks returned no populated `output[]` refs at planning time:

- `architecture-5-code-planning-0`
- `architecture-5-code-planning-1`
- `architecture-2-a-code-planning-0-coding-1`
- `architecture-3-b-code-planning-0-coding-0-repair-0`
- `architecture-4-code-planning-0-c-repair-0`
- `architecture-2-b-code-planning-0-repair-0`
- `architecture-4-code-planning-0-a-coding-0-repair-0`
- `architecture-2-a-code-planning-0-coding-0-repair-0`
- `architecture-4-code-planning-0-d-repair-0-coding-0`

The worktree still contains the previously repaired sibling preservation artifacts:

- `specs/plans/20260523-master-planning-task/20260523-142631-architecture-4-code-planning-0-b.md`
- `specs/plans/20260523-master-planning-task/20260523-142523-architecture-4-code-planning-0-c.md`

This submission must not remove or rewrite those files. The new output entry's `plan_ref` points to this plan file.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Child specialized tasks receive typed decomposition metadata as task-level state. | Goal spec, "Typed Decomposition Manifest"; architecture-4 plan, "Persist Manifest on Child Tasks" | Task 1 | Covered |
| 2 | `buildChildTask` copies `OutputEntry.Decomposition` to `Task.Decomposition`. | Architecture-4 plan, "Child Task Builder"; assigned task done_when | Task 1 | Covered |
| 3 | Per-subtask child creation copies output-entry `Kind`. | Assigned task done_when; architecture-4 plan, "OutputEntry -> Child Task" | Task 1 | Covered |
| 4 | Per-subtask child creation copies direct `PlanRef` from the output entry. | Goal spec, "Artifact Reference Propagation"; assigned task done_when | Task 1 | Covered |
| 5 | Per-subtask child creation copies direct `ArchRef` from the output entry. | Goal spec, "Artifact Reference Propagation"; assigned task done_when | Task 1 | Covered |
| 6 | Per-subtask child creation copies direct `EpicRef` from the output entry. | Goal spec, "Artifact Reference Propagation"; assigned task done_when | Task 1 | Covered |
| 7 | Sibling `depends_on` indices resolve to generated sibling task IDs. | Goal spec, "Master Output Contract", "Dependency ordering"; assigned task done_when | Task 1 | Covered |
| 8 | External `task_depends_on` concrete task IDs are copied to generated child dependencies. | ADR-0058; architecture-4 plan, "Dependency flow"; assigned task done_when | Task 1 | Covered |
| 9 | Inherited phase-gate dependencies are copied to generated children. | ADR-0048; assigned task done_when | Task 1 | Covered |
| 10 | Duplicate dependencies from sibling, external, and inherited sources are removed. | Assigned task done_when; existing `proceed.go` dependency merge behavior | Task 1 | Covered |
| 11 | Parent `arch_ref` fallback remains preserved for compatibility. | Goal spec, "Output-entry ref requirement"; assigned task done_when | Task 1 | Covered |
| 12 | Parent `epic_ref` fallback remains preserved for compatibility. | Goal spec, "Output-entry ref requirement"; assigned task done_when | Task 1 | Covered |
| 13 | Parent `plan_ref` fallback is not introduced. | Goal spec, "Output-entry ref requirement"; assigned task done_when | Task 1 | Covered |
| 14 | `buildChildTask` remains a copier and does not add semantic validation for manifest shape, refs, ownership, or dependencies. | Goal spec, "Typed Decomposition Manifest"; architecture-4 plan, "Structural Decisions" | Task 1 | Covered |
| 15 | Focused tests in `internal/ops/proceed_test.go` prove decomposition propagation, ref propagation, dependency merge behavior, inherited phase-gate dependencies, fallback preservation, and empty parent `plan_ref` fallback. | Assigned task done_when | Task 1 | Covered |
| 16 | The generated coding task modifies only `internal/ops/proceed.go` and focused tests in `internal/ops/proceed_test.go`. | Assigned task scope | Task 1 | Covered |
| 17 | The generated coding task does not add set-task-output validation, command projection changes, pipeline topology, prompt templates, docs, ADRs, end-to-end tests, or model schema changes. | Assigned task scope | Task 1 | Covered |
| 18 | The generated coding task consumes `DecompositionManifest` from the model-surface task. | Assigned task scope | Task 1 | Covered |
| 19 | `go test ./internal/ops -run 'TestProceed'` passes for the generated coding task. | Assigned task done_when | Task 1 | Covered |
| 20 | Planning submission preserves every artifact ref already present in active blackboard outputs when this task is claimed. | Assigned task done_when and scope | Artifact Preservation | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this is an internal per-subtask propagation slice; end-to-end acceptance coverage is owned by `architecture-5-code-planning-0` per the sprint graph and is explicitly out of scope here. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: documentation updates are owned by `architecture-5-code-planning-1` per the sprint graph and are explicitly out of scope here. | N/A |

## Validation Plan

- Validate output JSON syntax with `jq`.
- Run `liza set-task-output architecture-4-code-planning-0-c-repair-0 --output /home/tangi/Workspace/liza/.worktrees/architecture-4-code-planning-0-c-repair-0/specs/plans/20260523-master-planning-task/20260523-145900-architecture-4-code-planning-0-c-repair-0-output.json --agent-id code-planner-3 --json`.
- Re-read this plan and output JSON to verify character-identical `desc`, `done_when`, `scope`, `spec_ref`, and `plan_ref` values.
- Verify preserved artifact files still exist at `specs/plans/20260523-master-planning-task/20260523-142631-architecture-4-code-planning-0-b.md` and `specs/plans/20260523-master-planning-task/20260523-142523-architecture-4-code-planning-0-c.md`.
- Commit only this plan and its output JSON.
- Confirm `git -C /home/tangi/Workspace/liza/.worktrees/architecture-4-code-planning-0-c-repair-0 status --short` is clean.
- Submit `HEAD` with `liza submit-for-review architecture-4-code-planning-0-c-repair-0 HEAD --agent-id code-planner-3 --json`.
