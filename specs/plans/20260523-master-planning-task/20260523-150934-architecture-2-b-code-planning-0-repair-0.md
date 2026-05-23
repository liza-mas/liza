# Code Plan: INITIAL_PLANNING One-Task Prompt Contract Repair

## Sources

Based on:
- Task `architecture-2-b-code-planning-0-repair-0` from blackboard via `liza get`.
- Goal spec `specs/goals/20260523-master-planning-task.md`.
- Architecture reference `specs/arch-plan/20260523-master-planning-task/20260523-104359-architecture-2-b.md`.
- Upstream wake route-data reference `specs/arch-plan/20260523-master-planning-task/20260523-103617-architecture-2-a.md`.
- Prior code plan `specs/plans/20260523-master-planning-task/20260523-135215-architecture-2-a-code-planning-0.md`.
- Dependency repair plan `specs/plans/20260523-master-planning-task/20260523-145900-architecture-4-code-planning-0-c-repair-0.md`.
- Current prompt files: `internal/prompts/templates/wake_initial_planning.tmpl`, `internal/prompts/wake.go`, focused region of `internal/prompts/builder_test.go`, and resolver API evidence in `internal/pipeline/resolver.go`.

ASSUMPTION: None on the critical path. The current worktree exposes the resolver API planned by `architecture-2-a`, and the generated coding task is ordered after the existing route-rendering coding task that consumes it.

## Architectural Notes

Problem being solved: `INITIAL_PLANNING` still renders the old direct fan-out contract that asks the orchestrator to create multiple specialized planning tasks. The repaired prompt contract must make routing a one-task decision: specialized for confidently simple work, mapped master for fan-out or uncertain work when a fan-out target exists, and a single specialized fallback when no master mapping exists.

Change vectors: route-data construction is owned by the upstream `architecture-2-a` coding tasks; this plan owns only template wording, one-object examples, and focused prompt regressions. The cost of being wrong is medium and reversible because the prompt controls first task creation, but focused prompt tests can catch old multi-task guidance before merge.

Boundary: `internal/prompts/templates/wake_initial_planning.tmpl` owns the prompt contract. Prompt tests may use helpers or fixtures to render the route-data contract, but this task must not implement resolver traversal, pipeline topology, master prompt sections, output-entry validation, artifact propagation, docs, or end-to-end init/status scenarios.

Doc Impact: N/A: documentation updates are explicitly out of scope and covered by `architecture-5-code-planning-1`.

Test Impact: focused prompt tests in `internal/prompts/builder_test.go` or a focused prompt `_test.go` must be added or updated to cover the rendered one-task route contract and old-guidance regression checks.

## Task Graph

This plan emits one coding task because the behavior change is cohesive: prompt wording, JSON examples, and focused prompt tests are one observable contract. Splitting tests from the template would separate TDD from the behavior under test and would create unnecessary shared-file conflicts.

### Task 1: INITIAL_PLANNING one-task prompt contract and tests

desc: Update the INITIAL_PLANNING wake prompt contract and prompt rendering tests so simple goals create exactly one specialized planning task, fan-out or uncertain goals create exactly one mapped master planning task, and old multi-task fan-out guidance is removed.

done_when: `wake_initial_planning.tmpl` no longer instructs INITIAL_PLANNING to create multiple specialized planning tasks; rendered INITIAL_PLANNING instructions state that simple goals create exactly one specialized planning task and fan-out or uncertain goals create exactly one mapped master planning task when a fan-out target exists; task JSON examples are bare JSON arrays with exactly one object for the selected route; explicit entry-point rendering for `general-objective`, `functional-spec`, `detailed-spec`, and `technical-spec` shows the correct specialized simple target and mapped master fan-out target role-pairs/task types; no-entry-point classification rendering exposes the same simple-vs-fan-out routing choices; missing-master rendering falls back to exactly one specialized planning task and does not invent a master role-pair; focused prompt tests fail if old INITIAL_PLANNING multi-task guidance such as "MULTI-TASK PLANNING", "Create up to", same role-pair for all tasks, or two-domain task arrays reappears; `go test ./internal/prompts/...` passes.

scope: `internal/prompts/templates/wake_initial_planning.tmpl`, `internal/prompts/builder_test.go`, and focused prompt test helpers or fixtures needed to render the `architecture-2-a` route data contract. Consume the specialized simple target and mapped master fan-out target fields planned by `architecture-2-a`; do not modify resolver or topology traversal implementation, embedded pipeline master role-pair topology, `RolePairDef` schema changes, master doer/reviewer prompt sections, output-entry decomposition manifest validation, artifact propagation, docs, ADRs, or broad end-to-end `liza init` or `liza status` scenarios.

spec_ref: specs/goals/20260523-master-planning-task.md

plan_ref: specs/plans/20260523-master-planning-task/20260523-150934-architecture-2-b-code-planning-0-repair-0.md

task_depends_on: `architecture-2-a-code-planning-0-coding-1`

Validation: focused prompt rendering tests plus `go test ./internal/prompts/...`.

## Dependency Plan

Task 1 has no sibling `depends_on` because this plan emits one output entry.

Task 1 has an external task dependency on `architecture-2-a-code-planning-0-coding-1` because that task renders INITIAL_PLANNING from resolved route data and depends on the route-data construction repair. This ordering avoids concurrent edits to `wake_initial_planning.tmpl` and `builder_test.go`, and ensures this prompt-contract tightening consumes the finalized simple/fan-out fields instead of redefining them.

## Shared-File Audit

| File | Task 1 | Dependency |
|------|--------|------------|
| `internal/prompts/templates/wake_initial_planning.tmpl` | Owns removal of old multi-task guidance and one-object route examples | External dependency on `architecture-2-a-code-planning-0-coding-1` because that active task also edits this template |
| `internal/prompts/builder_test.go` | Owns focused prompt assertions for one-task routing and old-guidance absence | External dependency on `architecture-2-a-code-planning-0-coding-1` because that active task also edits prompt tests |
| Focused prompt helper or fixture files under `internal/prompts` | May be used only if needed to render the route-data contract | No sibling dependency needed because there is one output entry |

No generated coding task may touch `internal/prompts/wake.go`; route-data construction remains owned by `architecture-2-a-code-planning-0-coding-0-repair-0` and consumed through `architecture-2-a-code-planning-0-coding-1`.

## Artifact Preservation

Active output-summary checks during this repair planning run returned no populated `output[]` artifact refs for active tasks with outputs, including:

- `architecture-5-code-planning-0`
- `architecture-5-code-planning-1`
- `architecture-2-a-code-planning-0-coding-1`
- `architecture-2-a-code-planning-0-coding-0-repair-0`
- `architecture-3-b-code-planning-0-coding-0-repair-0`
- `architecture-4-code-planning-0-a-coding-0-repair-0`
- `architecture-4-code-planning-0-d-repair-0-coding-0`
- `architecture-2-b-code-planning-0-repair-0`

The worktree already contains the historical preservation artifacts needed by sibling repair plans:

- `specs/plans/20260523-master-planning-task/20260523-142523-architecture-4-code-planning-0-c.md`
- `specs/plans/20260523-master-planning-task/20260523-142631-architecture-4-code-planning-0-b.md`

This submission must not remove or rewrite those preservation files. The new output entry's `plan_ref` points to this plan file.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Entry-point work creates one specialized task directly when the goal needs exactly one planning task. | Goal spec `Case A / Case B Routing`, lines 177-182 | Task 1 | Covered |
| 2 | Entry-point work creates one master task in the matching decomposition-root role-pair when work would otherwise fan out. | Goal spec `Case A / Case B Routing`, lines 177-182 | Task 1 | Covered |
| 3 | Simple means one coherent planning scope without shared ownership or expected downstream split. | Goal spec `Case A / Case B Routing`, lines 188-198 | Task 1 | Covered |
| 4 | Fan-out includes multiple functional areas, shared boundaries, independent downstream workstreams, likely more than 8 downstream outputs, or uncertainty; uncertainty defaults to the master route. | Goal spec `Case A / Case B Routing`, lines 188-198 | Task 1 | Covered |
| 5 | Explicit entry-points remain specialized targets for `general-objective`, `functional-spec`, `detailed-spec`, and `technical-spec`. | Goal spec entry-point YAML, lines 200-205 | Task 1 | Covered |
| 6 | `INITIAL_PLANNING` wake changes from creating 1-5 parallel tasks to exactly one specialized task for simple work or exactly one master task for fan-out work. | Goal spec `Orchestrator Behavior Change`, lines 404-409 | Task 1 | Covered |
| 7 | The wake template must instruct mapped master routing through resolver-provided decomposition-root data and must not infer master behavior from outgoing `per-subtask` transitions. | Goal spec `Orchestrator Behavior Change`, lines 411-417; architecture-2-b `Interfaces`, lines 115-119 | Task 1 | Covered |
| 8 | The orchestrator still classifies the spec and assesses whether planning needs one task or multiple specialized scopes, but no longer performs parallel decomposition itself. | Goal spec `Orchestrator Behavior Change`, lines 419-424 | Task 1 | Covered |
| 9 | `wake_initial_planning.tmpl` must keep simple-vs-complex assessment but change complex handling from creating N specialized planning tasks to exactly one mapped master task. | Goal spec `Implementation Cost`, lines 516-519 | Task 1 | Covered |
| 10 | Scope is limited to INITIAL_PLANNING template wording, one-object task JSON examples, simple-vs-fan-out heuristic rendering, and prompt tests. | Architecture-2-b `Constraints`, lines 31-40 | Task 1 | Covered |
| 11 | The template consumes the wake routing data contract from `architecture-2-a` and does not duplicate resolver or topology traversal. | Architecture-2-b `Constraints`, lines 33-38; `Interfaces`, lines 115-119 | Task 1 | Covered |
| 12 | Prompt text remains stack-agnostic and does not hardcode project build tooling. | Architecture-2-b `Constraints`, line 40; `Cross-Cutting Concerns`, line 183 | Task 1 | Covered |
| 13 | Simple route rendering uses the specialized simple target role-pair, task type, display name, and ID prefix. | Architecture-2-b `INITIAL_PLANNING Wake Template`, lines 53-66 | Task 1 | Covered |
| 14 | Fan-out and uncertain route rendering uses exactly one mapped master planning task when a fan-out target exists. | Architecture-2-b `INITIAL_PLANNING Wake Template`, lines 61-66; `Simple-vs-Fan-Out Heuristic Text`, lines 76-79 | Task 1 | Covered |
| 15 | Task JSON examples are bare JSON arrays with exactly one object for the selected route, one route-specific task ID, and empty `depends`. | Architecture-2-b `Task JSON Example Contract`, lines 81-93 | Task 1 | Covered |
| 16 | Explicit entry-point rendering covers `general-objective`, `functional-spec`, `detailed-spec`, and `technical-spec` with correct specialized simple and mapped master fan-out role-pairs/task types. | Architecture-2-b `Prompt Rendering Tests`, lines 95-111 | Task 1 | Covered |
| 17 | No-entry-point classification rendering exposes the same simple-vs-fan-out choices for each entry-point. | Architecture-2-b `Classification flow`, lines 154-162 | Task 1 | Covered |
| 18 | Missing-master rendering falls back to exactly one specialized planning task and does not invent a master role-pair. | Architecture-2-b `Missing-master fallback flow`, lines 164-170 | Task 1 | Covered |
| 19 | Prompt tests must reject old multi-task guidance such as `MULTI-TASK PLANNING`, `Create up to`, same role-pair for all tasks, or two-domain task arrays. | Architecture-2-b `Prompt Rendering Tests`, lines 103-111; `Structural Decisions`, lines 195-197 | Task 1 | Covered |
| 20 | Focused prompt tests pass with `go test ./internal/prompts/...`. | Architecture-2-b `Output done_when`, line 221 | Task 1 | Covered |
| 21 | Generated coding task modifies only `wake_initial_planning.tmpl`, `builder_test.go`, and focused prompt helpers or fixtures needed to render the route data contract. | Assigned task scope | Task 1 | Covered |
| 22 | Generated coding task does not modify resolver or topology traversal, embedded topology, `RolePairDef`, master prompt sections, decomposition manifest validation, artifact propagation, docs, ADRs, or broad end-to-end init/status scenarios. | Assigned task scope | Task 1 | Covered |
| 23 | Planning submission preserves artifact refs already present in active blackboard outputs at claim time. | Assigned task done_when and scope | Artifact Preservation | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: broad end-to-end `liza init` and `liza status` scenarios are explicitly out of scope and owned by `architecture-5-code-planning-0`. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: docs and ADRs are explicitly out of scope and owned by `architecture-5-code-planning-1`. | N/A |

## Output Entries

The structured output JSON contains one entry for Task 1. It has `task_depends_on: ["architecture-2-a-code-planning-0-coding-1"]` because the active upstream rendering task shares `wake_initial_planning.tmpl` and prompt tests and is the finalized route-data consumer this task tightens.

## Validation Plan

- Validate output JSON syntax with `jq`.
- Run `liza set-task-output architecture-2-b-code-planning-0-repair-0 --output /home/tangi/Workspace/liza/.worktrees/architecture-2-b-code-planning-0-repair-0/specs/plans/20260523-master-planning-task/20260523-150934-architecture-2-b-code-planning-0-repair-0-output.json --agent-id code-planner-2 --json`.
- Re-read this plan and output JSON to verify character-identical `desc`, `done_when`, `scope`, `spec_ref`, and `plan_ref` values.
- Verify preserved artifact files still exist at `specs/plans/20260523-master-planning-task/20260523-142523-architecture-4-code-planning-0-c.md` and `specs/plans/20260523-master-planning-task/20260523-142631-architecture-4-code-planning-0-b.md`.
- Run pre-commit on the touched plan and output JSON.
- Commit only this plan and its output JSON.
- Confirm `git -C /home/tangi/Workspace/liza/.worktrees/architecture-2-b-code-planning-0-repair-0 status --short` is clean.
- Submit `HEAD` with `liza submit-for-review architecture-2-b-code-planning-0-repair-0 HEAD --agent-id code-planner-2 --json`.
