# Code Plan: Inspect Decomposition Projection Repair

## Context

This plan covers the inspect-command projection slice for typed decomposition metadata from the master planning task pattern. The model surface is owned by `architecture-4-code-planning-0-a` and its generated coding task, so this plan only teaches task inspection projections to expose metadata once `models.Task.Decomposition`, `models.OutputEntry.Decomposition`, and `models.DecompositionManifest` exist.

This repair also preserves the sibling plan artifact refs currently stored in blocked blackboard outputs for `architecture-4-code-planning-0-b` and `architecture-4-code-planning-0-c`. Those refs are required so post-merge state validation does not fail with `candidate_artifact_missing`.

## Task 1: Add decomposition metadata to inspect task JSON projections

desc:
Add decomposition metadata to inspect task JSON projections so full task inspection exposes task-level metadata and output-summary inspection exposes output-entry metadata.

done_when:
`internal/commands/inspect_tasks.go` exposes optional top-level `decomposition` metadata in full task JSON projections by copying `Task.Decomposition` into `taskInfo`; output-summary JSON projections expose optional `output[].decomposition` metadata by copying `OutputEntry.Decomposition` into `outputEntrySummaryInfo`; compact task summaries from `--summary` remain unchanged and do not expose decomposition metadata; focused tests in `internal/commands/inspect_tasks_test.go` prove full task JSON includes `Task.Decomposition`, output-summary JSON includes `output[].decomposition`, and compact summary JSON stays compact; `go test ./internal/commands -run 'TestInspectTasks.*Decomposition|TestInspectTasksOutputSummary'` passes.

scope:
Modify only `internal/commands/inspect_tasks.go` and focused tests in `internal/commands/inspect_tasks_test.go`. Consume `models.DecompositionManifest`, `Task.Decomposition`, and `OutputEntry.Decomposition` from the model-surface task; do not modify model schema, `set-task-output` validation, child-task creation, prompt templates, docs, ADRs, end-to-end tests, or compact summary fields except as required to assert they remain absent.

spec_ref:
specs/goals/20260523-master-planning-task.md

plan_ref:
specs/plans/20260523-master-planning-task/20260523-143720-architecture-4-code-planning-0-d-repair-0.md

### Implementation Notes

- Add `Decomposition *models.DecompositionManifest` to `taskInfo` with `json:"decomposition,omitempty" yaml:"decomposition,omitempty"` and assign it from `task.Decomposition` in `buildTaskInfo`.
- Add `Decomposition *models.DecompositionManifest` to `outputEntrySummaryInfo` with `json:"decomposition,omitempty" yaml:"decomposition,omitempty"` and assign it from `entry.Decomposition` in `buildTaskOutputSummaryInfo`.
- Do not add decomposition metadata to `taskSummaryInfo`; `--summary` remains a compact task envelope and currently exposes only output counts and kinds.
- Extend the existing `TestInspectTasksOutputSummary` fixture to include `OutputEntry.Decomposition` on one output entry and assert the projected nested fields.
- Add focused full task JSON coverage for task-level `Task.Decomposition`.
- Add or extend compact summary JSON coverage to assert `decomposition` is absent from summary task envelopes.
- Keep table/value output behavior unchanged unless JSON/YAML formatters automatically include the new struct fields where the projection already includes full or output-entry details.

## Dependency Plan

Task 1 has no sibling dependencies because this plan emits a single coding task. It has an external task dependency on `architecture-4-code-planning-0-a-coding-0-repair-0` because that active replacement coding task introduces `models.DecompositionManifest`, `Task.Decomposition`, and `OutputEntry.Decomposition`.

## Shared-File Audit

Only Task 1 modifies `internal/commands/inspect_tasks.go` and `internal/commands/inspect_tasks_test.go`, so no intra-plan `depends_on` chain is required. The plan does not overlap with sibling implementation scopes for model persistence (`internal/models/task.go`), set-task-output validation (`internal/ops/set_task_output.go`), or child-task propagation (`internal/ops/proceed.go`).

## Artifact Preservation

The following blackboard-referenced sibling plan refs were missing from both this worktree and `integration`, so this repair preserves them as blackboard-derived artifacts:

- `specs/plans/20260523-master-planning-task/20260523-142631-architecture-4-code-planning-0-b.md`
- `specs/plans/20260523-master-planning-task/20260523-142523-architecture-4-code-planning-0-c.md`

These files are not new scope for Task 1. They exist to satisfy post-merge state validation for already-recorded sibling `output[].plan_ref` values.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Typed decomposition metadata is a structured manifest associated with output entries and tasks. | Goal spec, "Typed Decomposition Manifest" | Task 1 | Covered |
| 2 | Full task JSON projections include optional task-level `decomposition` metadata. | Assigned task done_when | Task 1 | Covered |
| 3 | Output-summary JSON projections include optional `output[].decomposition` metadata. | Assigned task done_when | Task 1 | Covered |
| 4 | Existing compact summaries remain compact unless they already expose output-entry details. | Assigned task done_when | Task 1 | Covered |
| 5 | Focused inspect command tests prove full task JSON exposes `Task.Decomposition`. | Assigned task done_when | Task 1 | Covered |
| 6 | Focused inspect command tests prove output-summary JSON exposes `output[].decomposition`. | Assigned task done_when | Task 1 | Covered |
| 7 | The generated coding task modifies only `internal/commands/inspect_tasks.go` and focused tests in `internal/commands/inspect_tasks_test.go`. | Assigned task scope | Task 1 | Covered |
| 8 | The generated coding task consumes `DecompositionManifest` and does not modify model schema. | Assigned task scope; Goal spec, "Typed Decomposition Manifest" | Task 1 | Covered |
| 9 | Keep set-task-output validation out of scope. | Assigned task scope | Task 1 | Covered |
| 10 | Keep child-task creation out of scope. | Assigned task scope | Task 1 | Covered |
| 11 | Keep prompt templates, docs, ADRs, and end-to-end tests out of scope for this coding slice. | Assigned task scope | Task 1 | Covered |
| 12 | Preserve sibling plan artifacts referenced by blackboard outputs for `architecture-4-code-planning-0-b` and `architecture-4-code-planning-0-c` so post-merge state validation does not fail with `candidate_artifact_missing`. | Assigned task done_when and scope | Artifact Preservation | Covered |
| 13 | The focused validation command `go test ./internal/commands -run 'TestInspectTasks.*Decomposition|TestInspectTasksOutputSummary'` passes. | Assigned task done_when | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this is an internal CLI projection slice; end-to-end coverage is owned by `architecture-5-code-planning-0` per the sprint graph and is explicitly out of scope here. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: documentation updates are owned by `architecture-5-code-planning-1` per the sprint graph and are explicitly out of scope here. | N/A |

## Validation Plan

- Validate output JSON syntax with `jq`.
- Run `liza set-task-output architecture-4-code-planning-0-d-repair-0 --output /home/tangi/Workspace/liza/.worktrees/architecture-4-code-planning-0-d-repair-0/specs/plans/20260523-master-planning-task/20260523-143720-architecture-4-code-planning-0-d-repair-0-output.json --agent-id code-planner-2 --json`.
- Re-read this plan and output JSON to verify character-identical `desc`, `done_when`, `scope`, `spec_ref`, and `plan_ref` values.
- Verify the sibling preservation artifact files exist at the exact `plan_ref` paths from the current blackboard output summaries for `architecture-4-code-planning-0-b` and `architecture-4-code-planning-0-c`.
- Commit only this plan, its output JSON, and the two sibling preservation artifacts.
- Confirm `git -C /home/tangi/Workspace/liza/.worktrees/architecture-4-code-planning-0-d-repair-0 status --short` is clean.
- Submit `HEAD` with `liza submit-for-review architecture-4-code-planning-0-d-repair-0 HEAD --agent-id code-planner-2 --json`.
