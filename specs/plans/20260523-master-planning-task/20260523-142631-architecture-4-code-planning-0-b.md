# Preserved Plan Artifact: architecture-4-code-planning-0-b

This preservation artifact exists because the current blackboard output for `architecture-4-code-planning-0-b` references this `plan_ref`, but the referenced file is absent from both the repair worktree and `integration`.

Source: `liza get architecture-4-code-planning-0-b --json` during `architecture-4-code-planning-0-d-repair-0`.

## Task 1: Add decomposition-root output validation

desc:
Add decomposition-root `liza set-task-output` validation for typed decomposition manifests, required master artifact refs, sibling ownership invariants, read-only dependency mirroring, sibling dependency cycles, and non-root compatibility.

done_when:
`internal/ops/set_task_output.go` uses the pipeline resolver decomposition-root contract to detect decomposition-root producer role-pairs; every decomposition-root output requires `decomposition` plus the role-appropriate produced artifact ref (`plan_ref` for `epic-planning-main-pair`, `arch_ref` for `architecture-main-pair`, and `plan_ref` for `code-planning-main-pair`); decomposition-root role-pairs with no known required ref fail closed; non-root outputs remain valid without `decomposition`; validation rejects duplicate sibling `owned_files`, duplicate sibling `interfaces_owned`, empty ownership declarations, catch-all ownership strings, invalid or self `read_only_depends_on`, `read_only_depends_on` not mirrored in `depends_on`, invalid or missing `read_only_task_depends_on` targets, `read_only_task_depends_on` not mirrored in `task_depends_on`, invalid sibling `depends_on`, and circular sibling `depends_on`; focused tests in `internal/ops/set_task_output_test.go` prove each rejection and the non-root compatibility path; `go test ./internal/ops -run 'TestSetTaskOutput'` passes.

scope:
Modify only `internal/ops/set_task_output.go` and focused tests in `internal/ops/set_task_output_test.go`. Reuse the existing artifact-ref scalar validation, dependency-direction validation, `models.DecompositionManifest`, and resolver decomposition-root API from `architecture-1-code-planning-0-a`, `architecture-1-code-planning-0-b`, and `architecture-4-code-planning-0-a-coding-0`; do not modify `internal/models/task.go`, `internal/ops/proceed.go`, command projections, pipeline topology, prompt templates, docs, ADRs, or end-to-end tests.

spec_ref:
specs/goals/20260523-master-planning-task.md

plan_ref:
specs/plans/20260523-master-planning-task/20260523-142631-architecture-4-code-planning-0-b.md

task_depends_on:
- architecture-4-code-planning-0-a-coding-0
