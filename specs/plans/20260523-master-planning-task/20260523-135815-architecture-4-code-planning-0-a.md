# Code Plan: Typed Decomposition Manifest Model Surface

## Context

This plan covers the model-only slice of the typed decomposition manifest work from `architecture-4`. The task establishes the persisted schema surface in `internal/models/task.go` and focused model serialization coverage in `internal/models/task_test.go`. Validation behavior, child-task propagation, inspect projections, pipeline topology, prompt templates, documentation, ADRs, and end-to-end tests are out of scope here.

## Task 1: Add DecompositionManifest model persistence and round-trip tests

desc:
Add typed decomposition manifest model persistence on `OutputEntry` and `Task`, with focused JSON/YAML round-trip tests for populated and omitted decomposition metadata.

done_when:
`internal/models/task.go` defines `DecompositionManifest` with JSON/YAML fields `owned_files`, `owned_modules`, `read_only_depends_on`, `read_only_task_depends_on`, `interfaces_owned`, `interfaces_consumed`, and `coverage_notes`; `OutputEntry` and `Task` both persist optional `decomposition` metadata without changing legacy entries; `internal/models/task_test.go` covers JSON and YAML round trips for populated and omitted decomposition metadata on `OutputEntry` and `Task`; `go test ./internal/models` passes.

scope:
Modify only `internal/models/task.go` and focused tests in `internal/models/task_test.go`. Do not add validation behavior, child-task propagation, inspect projections, pipeline topology, prompt templates, docs, ADRs, or end-to-end tests.

spec_ref:
specs/goals/20260523-master-planning-task.md

plan_ref:
specs/plans/20260523-master-planning-task/20260523-135815-architecture-4-code-planning-0-a.md

### Implementation Notes

- Add `DecompositionManifest` near the task/output model declarations so both `Task` and `OutputEntry` can reference it without introducing a new package.
- Use the exact JSON/YAML tags from the goal spec and architecture plan, with `omitempty` on every field.
- Add `Decomposition *DecompositionManifest` to `OutputEntry` and `Task` with `yaml:"decomposition,omitempty" json:"decomposition,omitempty"`.
- Extend or add focused model tests beside the existing `OutputEntry` and `Task` kind round-trip tests.
- Cover populated metadata for both JSON and YAML, and omitted metadata for both JSON and YAML, on both `OutputEntry` and `Task`.
- Confirm legacy entries still decode when `decomposition` is absent.

## Dependency Plan

Task 1 has no sibling dependencies because this plan emits a single coding task. No external task dependency is encoded in this output because the assigned scope is limited to the model surface.

## Shared-File Audit

Only Task 1 modifies `internal/models/task.go` and `internal/models/task_test.go`, so no intra-plan `depends_on` chain is required.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Define `DecompositionManifest` as a concrete Go struct. | Goal spec, "Typed Decomposition Manifest"; Architecture plan, "Task Data Model" | Task 1 | Covered |
| 2 | Persist manifest fields `owned_files`, `owned_modules`, `read_only_depends_on`, `read_only_task_depends_on`, `interfaces_owned`, `interfaces_consumed`, and `coverage_notes` with JSON/YAML tags. | Goal spec, "Typed Decomposition Manifest" code block | Task 1 | Covered |
| 3 | Add optional `decomposition` metadata to `OutputEntry`. | Goal spec, "Typed Decomposition Manifest"; Architecture plan, "Task Data Model" | Task 1 | Covered |
| 4 | Add optional `decomposition` metadata to `Task` for inherited task-level state. | Architecture plan, "Persist Manifest on Child Tasks"; assigned task done_when | Task 1 | Covered |
| 5 | Preserve backward compatibility for legacy output entries and tasks when `decomposition` is omitted. | Goal spec, "Typed Decomposition Manifest"; Architecture plan, "Backward compatibility" | Task 1 | Covered |
| 6 | Add focused JSON round-trip coverage for populated and omitted `decomposition` metadata on `OutputEntry` and `Task`. | Assigned task done_when | Task 1 | Covered |
| 7 | Add focused YAML round-trip coverage for populated and omitted `decomposition` metadata on `OutputEntry` and `Task`. | Assigned task done_when | Task 1 | Covered |
| 8 | Keep validation behavior out of this task. | Assigned task scope; Architecture plan, "Master Output Validator" owned by sibling scope | Task 1 | Covered |
| 9 | Keep child-task propagation, inspect projections, topology, prompts, docs, ADRs, and end-to-end tests out of this task. | Assigned task scope | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this is an internal model-surface slice; end-to-end coverage is explicitly out of scope here. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: docs are explicitly out of scope here. | N/A |

## Validation Plan

- Validate output JSON syntax with `jq`.
- Run `liza set-task-output architecture-4-code-planning-0-a --output /home/tangi/Workspace/liza/.worktrees/architecture-4-code-planning-0-a/specs/plans/20260523-master-planning-task/20260523-135815-architecture-4-code-planning-0-a-output.json --agent-id code-planner-5 --json`.
- Re-read this plan and output JSON to verify character-identical `desc`, `done_when`, `scope`, `spec_ref`, and `plan_ref` values.
- Commit the plan and output JSON.
- Confirm `git -C /home/tangi/Workspace/liza/.worktrees/architecture-4-code-planning-0-a status --short` is clean.
- Submit `HEAD` with `liza submit-for-review architecture-4-code-planning-0-a HEAD --agent-id code-planner-5 --json`.
