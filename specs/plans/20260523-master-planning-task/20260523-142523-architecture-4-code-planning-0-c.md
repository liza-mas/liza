# Preserved Plan Artifact: architecture-4-code-planning-0-c

This preservation artifact exists because the current blackboard output for `architecture-4-code-planning-0-c` references this `plan_ref`, but the referenced file is absent from both the repair worktree and `integration`.

Source: `liza get architecture-4-code-planning-0-c --json` during `architecture-4-code-planning-0-d-repair-0`.

## Task 1: Propagate decomposition metadata into child tasks

desc:
Propagate per-subtask output metadata into generated child tasks by copying `OutputEntry.Decomposition` alongside existing kind, artifact ref, and dependency fields.

done_when:
`internal/ops/proceed.go` copies `OutputEntry.Decomposition` into generated child `Task.Decomposition`; per-subtask child creation still copies `Kind`, `PlanRef`, `ArchRef`, `EpicRef`, sibling `depends_on` IDs, external `task_depends_on`, and inherited phase-gate dependencies with duplicate removal; existing parent `arch_ref` and `epic_ref` fallback behavior remains covered; no parent `plan_ref` fallback is introduced; focused tests in `internal/ops/proceed_test.go` prove decomposition propagation, ref propagation, dependency merge behavior, inherited phase-gate dependencies, preserved parent `arch_ref`/`epic_ref` fallback, and empty parent `plan_ref` fallback; `go test ./internal/ops -run 'TestProceed'` passes.

scope:
Modify only `internal/ops/proceed.go` and focused tests in `internal/ops/proceed_test.go`. Consume `DecompositionManifest`; do not add set-task-output validation, command projection changes, pipeline topology, prompt templates, docs, ADRs, or end-to-end tests.

spec_ref:
specs/goals/20260523-master-planning-task.md

plan_ref:
specs/plans/20260523-master-planning-task/20260523-142523-architecture-4-code-planning-0-c.md

task_depends_on:
- architecture-4-code-planning-0-a-coding-0
