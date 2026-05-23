# Code Plan: Decomposition-Root Set Task Output Validation Repair

## Context

This plan covers the `liza set-task-output` validation slice for the master planning task pattern after the artifact-ref preservation race. The model surface is owned by `architecture-4-code-planning-0-a` and its replacement coding task. Inspect projection work is owned by `architecture-4-code-planning-0-d-repair-0`. This plan only validates decomposition-root output batches at the point where the producer task role-pair and the full `output[]` array are available.

The generated coding task must consume the resolver decomposition-root contract already planned and merged by the topology/resolver tasks. It must reuse the existing artifact-ref scalar validation and dependency-direction validation paths in `internal/ops/set_task_output.go`; it must not create a parallel artifact-ref or topology validator.

This repair also preserves blackboard-referenced plan artifacts already present in this worktree for sibling planning outputs:

- `specs/plans/20260523-master-planning-task/20260523-142631-architecture-4-code-planning-0-b.md`
- `specs/plans/20260523-master-planning-task/20260523-142523-architecture-4-code-planning-0-c.md`

These files are preservation artifacts only. They are not in the generated coding task scope.

## Task 1: Enforce decomposition-root set-task-output validation

desc:
Enforce the decomposition-root output contract in `set-task-output` by requiring typed decomposition metadata and the role-appropriate produced artifact ref on decomposition-root outputs while preserving non-root compatibility.

done_when:
`internal/ops/set_task_output.go` detects whether the producing task role-pair is a decomposition root through the resolver `IsDecompositionRoot` API; decomposition-root output entries require `decomposition` and the role-appropriate produced artifact ref (`plan_ref` for `epic-planning-main-pair`, `arch_ref` for `architecture-main-pair`, `plan_ref` for `code-planning-main-pair`); non-root output entries remain valid without `decomposition`; validation rejects duplicate sibling `decomposition.owned_files`, duplicate sibling `decomposition.interfaces_owned`, empty ownership declarations, catch-all ownership declarations, invalid or self `decomposition.read_only_depends_on`, `decomposition.read_only_depends_on` entries not mirrored in `depends_on`, invalid or missing `decomposition.read_only_task_depends_on` targets, `decomposition.read_only_task_depends_on` entries not mirrored in `task_depends_on`, invalid sibling `depends_on`, and sibling `depends_on` cycles; validation continues to use existing artifact-ref scalar checks for output refs and existing dependency-direction checks for `task_depends_on`; focused tests in `internal/ops/set_task_output_test.go` prove each rejection path plus non-root compatibility; `go test ./internal/ops -run 'TestSetTaskOutput'` passes.

scope:
Modify only `internal/ops/set_task_output.go` and focused tests in `internal/ops/set_task_output_test.go`. Consume `models.DecompositionManifest` from the model-surface task and the resolver decomposition-root API from the merged resolver work. Reuse existing artifact-ref scalar validation, `models.ValidateDependsOn`, `validateTaskDependsOn`, existing task existence checks, and dependency-direction validation. Do not modify `internal/models/task.go`, `internal/ops/proceed.go`, command projections, pipeline topology, prompt templates, docs, ADRs, end-to-end tests, or unrelated ops behavior.

spec_ref:
specs/goals/20260523-master-planning-task.md

plan_ref:
specs/plans/20260523-master-planning-task/20260523-144938-architecture-4-code-planning-0-b-repair-0.md

### Implementation Notes

- Add decomposition-root validation after the producing task has been loaded inside the blackboard lock, because role-pair detection and `task_depends_on` existence checks require current state.
- Use `resolver.IsDecompositionRoot(task.RolePair)` to decide whether the strict root contract applies.
- Keep ordinary output-entry validation in the existing front-loaded loop: required `desc`, `done_when`, `scope`, `kind`, artifact-ref scalar shape, sibling `depends_on` index validation, and task ID shape validation.
- Add a small helper that maps known decomposition-root role-pairs to their required produced output ref:
  - `epic-planning-main-pair` requires `plan_ref`
  - `architecture-main-pair` requires `arch_ref`
  - `code-planning-main-pair` requires `plan_ref`
- If the resolver reports a decomposition-root role-pair that has no required-ref mapping, fail closed with an error naming the role-pair.
- For decomposition-root entries, require `entry.Decomposition != nil`.
- Treat an ownership declaration as non-empty when at least one of `owned_files`, `owned_modules`, or `interfaces_owned` is present after trimming empty strings.
- Reject catch-all ownership values in ownership fields using normalized, trimmed, case-insensitive comparisons for values such as `*`, `everything`, `everything else`, `all`, `all files`, `all remaining`, and `remaining`.
- Normalize duplicate ownership checks by trimming and comparing case-sensitively for paths and interface names after empty strings are removed; error messages should name the conflicting output indices and field.
- Validate `read_only_depends_on` against the output batch: index in range, not self, and mirrored by the string form in `entry.DependsOn`.
- Validate `read_only_task_depends_on`: target exists in state and is mirrored by `entry.TaskDependsOn`.
- Add cycle detection over the already validated sibling `depends_on` graph. The existing `models.ValidateDependsOn` rejects malformed, out-of-range, and self references; this task adds multi-entry cycle rejection.
- Tests should use the existing `testhelpers.SetupLizaDir`, `testhelpers.CreateValidState`, `testhelpers.BuildTaskByStatus`, and `SetTaskOutput` patterns.
- Add test fixtures for a root role-pair by setting a task role-pair to one of the configured master pairs and status to that role-pair's executing status. Use the existing loaded test pipeline rather than custom topology when possible.
- Keep non-root compatibility explicit with a test proving a non-root output without `decomposition` still succeeds.

## Dependency Plan

Task 1 has no sibling dependencies because this plan emits a single coding task. It has an external task dependency on `architecture-4-code-planning-0-a-coding-0-repair-0` because that active replacement coding task introduces `models.DecompositionManifest`, `models.OutputEntry.Decomposition`, and related model serialization support consumed by this validation task.

No dependency is required on `architecture-4-code-planning-0-d-repair-0-coding-0` because inspect projection is a sibling consumer of the model surface and does not provide behavior needed by `set-task-output` validation.

## Shared-File Audit

Only Task 1 modifies `internal/ops/set_task_output.go` and `internal/ops/set_task_output_test.go`, so no intra-plan `depends_on` chain is required. The plan does not overlap with sibling implementation scopes for model persistence (`internal/models/task.go`), child-task propagation (`internal/ops/proceed.go`), or inspect projection (`internal/commands/inspect_tasks.go`).

## Artifact Preservation

Current active output-summary checks for active tasks returned no populated `output[]` refs at planning time. The worktree still contains the previously repaired sibling preservation artifacts for historical blackboard refs:

- `specs/plans/20260523-master-planning-task/20260523-142631-architecture-4-code-planning-0-b.md`
- `specs/plans/20260523-master-planning-task/20260523-142523-architecture-4-code-planning-0-c.md`

This submission must not remove or rewrite those files. The new output entry's `plan_ref` points to this plan file.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | `decomposition` is required for output entries produced by decomposition-root tasks. | Goal spec, "Typed Decomposition Manifest" | Task 1 | Covered |
| 2 | `decomposition` remains optional for non-root output entries. | Goal spec, "Typed Decomposition Manifest" | Task 1 | Covered |
| 3 | Machine validation runs in `liza set-task-output` because the full `output[]` batch is available there. | Goal spec, "Typed Decomposition Manifest" | Task 1 | Covered |
| 4 | Decomposition-root outputs require the master task's appropriate produced artifact ref. | Goal spec, "Artifact Reference Propagation", "Output-entry ref requirement" | Task 1 | Covered |
| 5 | `epic-planning-main-pair` output entries require `plan_ref`. | Goal spec, "Artifact Reference Propagation" table | Task 1 | Covered |
| 6 | `architecture-main-pair` output entries require `arch_ref`. | Goal spec, "Artifact Reference Propagation" table | Task 1 | Covered |
| 7 | `code-planning-main-pair` output entries require `plan_ref`. | Goal spec, "Artifact Reference Propagation" table | Task 1 | Covered |
| 8 | Duplicate sibling owned file declarations are rejected. | Goal spec, "Typed Decomposition Manifest" validation paragraph | Task 1 | Covered |
| 9 | Duplicate sibling interface ownership is rejected. | Goal spec, "Master Output Contract", "Interface ownership"; architecture-4 plan, "Master Output Validator" | Task 1 | Covered |
| 10 | Empty ownership declarations are rejected. | Goal spec, "Typed Decomposition Manifest" validation paragraph | Task 1 | Covered |
| 11 | Catch-all ownership declarations such as "everything else" are rejected. | Goal spec, "Master Output Contract", "Non-overlapping scopes"; "Typed Decomposition Manifest" validation paragraph | Task 1 | Covered |
| 12 | Invalid sibling dependency indices are rejected. | Goal spec, "Typed Decomposition Manifest" validation paragraph | Task 1 | Covered |
| 13 | Sibling dependency cycles are rejected. | Goal spec, "Master Output Contract", "Dependency ordering"; "Typed Decomposition Manifest" validation paragraph | Task 1 | Covered |
| 14 | `read_only_depends_on` entries are valid sibling indices, not self references, and mirrored in `depends_on`. | Architecture-4 plan, "Mirror Read-Only Manifest Dependencies into Scheduler Dependencies"; assigned task done_when | Task 1 | Covered |
| 15 | `read_only_task_depends_on` entries are valid existing task IDs and mirrored in `task_depends_on`. | Architecture-4 plan, "Master Output Validator"; assigned task done_when | Task 1 | Covered |
| 16 | Existing artifact-ref scalar validation remains the authority for output refs. | Assigned task scope; architecture-4 plan, "Artifact Reference Validation" | Task 1 | Covered |
| 17 | Existing dependency-direction validation remains the authority for `task_depends_on`. | Assigned task scope; INVARIANTS.md, "Dependency Direction" | Task 1 | Covered |
| 18 | The generated coding task modifies only `internal/ops/set_task_output.go` and focused tests in `internal/ops/set_task_output_test.go`. | Assigned task scope | Task 1 | Covered |
| 19 | The generated coding task does not modify model schema, child-task creation, command projections, topology, prompts, docs, ADRs, or end-to-end tests. | Assigned task scope | Task 1 | Covered |
| 20 | Focused tests prove rejection paths and non-root compatibility. | Assigned task done_when | Task 1 | Covered |
| 21 | `go test ./internal/ops -run 'TestSetTaskOutput'` passes for the generated coding task. | Assigned task done_when | Task 1 | Covered |
| 22 | Planning submission preserves artifact refs already present in active blackboard outputs when this task is claimed. | Assigned task done_when and scope | Artifact Preservation | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this is an internal ops validation slice; end-to-end acceptance coverage is owned by `architecture-5-code-planning-0` per the sprint graph and is explicitly out of scope here. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: documentation updates are owned by `architecture-5-code-planning-1` per the sprint graph and are explicitly out of scope here. | N/A |

## Validation Plan

- Validate output JSON syntax with `jq`.
- Run `liza set-task-output architecture-4-code-planning-0-b-repair-0 --output /home/tangi/Workspace/liza/.worktrees/architecture-4-code-planning-0-b-repair-0/specs/plans/20260523-master-planning-task/20260523-144938-architecture-4-code-planning-0-b-repair-0-output.json --agent-id code-planner-2 --json`.
- Re-read this plan and output JSON to verify character-identical `desc`, `done_when`, `scope`, `spec_ref`, and `plan_ref` values.
- Verify preserved artifact files still exist at `specs/plans/20260523-master-planning-task/20260523-142631-architecture-4-code-planning-0-b.md` and `specs/plans/20260523-master-planning-task/20260523-142523-architecture-4-code-planning-0-c.md`.
- Commit only this plan and its output JSON.
- Confirm `git -C /home/tangi/Workspace/liza/.worktrees/architecture-4-code-planning-0-b-repair-0 status --short` is clean.
- Submit `HEAD` with `liza submit-for-review architecture-4-code-planning-0-b-repair-0 HEAD --agent-id code-planner-2 --json`.
