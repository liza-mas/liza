# Code Plan: INITIAL_PLANNING Resolver-Backed Routing Data

## Sources

Based on:
- Task `architecture-2-a-code-planning-0` from blackboard via `liza get`.
- Goal spec `specs/goals/20260523-master-planning-task.md`.
- Architecture reference `specs/arch-plan/20260523-master-planning-task/20260523-103617-architecture-2-a.md`.
- Sibling boundary reference `specs/arch-plan/20260523-master-planning-task/20260523-104359-architecture-2-b.md`.
- Existing prompt code in `internal/prompts/wake.go`, `internal/prompts/templates/wake_initial_planning.tmpl`, and focused regions of `internal/prompts/builder_test.go`.
- Resolver API evidence in `internal/pipeline/resolver.go`.

ASSUMPTION: None on the critical path. The resolver API is present in the worktree as `pipeline.Resolver.DecompositionRootForTarget`.

## Architectural Notes

Problem being solved: INITIAL_PLANNING currently renders a direct single-or-many specialized planning task contract. The new behavior needs one route table per entry-point: a specialized simple target and, when configured, a resolver-backed decomposition-root fan-out target.

Change vectors: pipeline topology and resolver validation are stable upstream contracts from `architecture-1`; prompt wording is still evolving in sibling `architecture-2-b-code-planning-0`. This plan keeps topology lookup in `internal/pipeline` and limits prompt-package work to consuming resolved data and rendering it.

Cost of being wrong: medium and reversible. A bad route-data contract can cause incorrect initial planning task creation, but it is covered by focused prompt tests and constrained to `internal/prompts`.

Boundary: `internal/prompts/wake.go` owns prompt data construction; `wake_initial_planning.tmpl` owns rendering; tests assert prompt behavior. The prompt package must not reimplement pipeline traversal or infer master role-pairs from names.

## Task Graph

Task 1 must complete before Task 2 because Task 2 consumes the route-data fields produced by Task 1 and both tasks update prompt tests.

### Task 1: Resolver-Backed Wake Route Data

desc: Add resolver-backed INITIAL_PLANNING route-data construction so each configured entry-point exposes a specialized simple target plus an optional decomposition-root fan-out target, and explicit Goal.EntryPoint resolution reuses the same entry-point data as classification rendering.

done_when: `wakeEntryPointData` or an equivalent prompt data type exposes, for every configured entry-point, simple target role-pair, task type, display name, and task ID prefix plus fan-out target role-pair, task type, display name, task ID prefix, and availability when `pipeline.Resolver.DecompositionRootForTarget` finds a mapping; `buildWakeTemplateData` uses the resolver lookup instead of topology traversal, propagates lookup errors as render errors, treats a clean missing mapping as no fan-out target, and resolves explicit `Goal.EntryPoint` from the same entry-point data used by the classification list; focused tests cover route-data construction for `general-objective`, `functional-spec`, `detailed-spec`, and `technical-spec`, a missing-master fallback fixture with no invented master role-pair, and a resolver-error path; `go test ./internal/prompts/...` passes.

scope: `internal/prompts/wake.go` and focused prompt helper tests in `internal/prompts/builder_test.go` or a new `_test.go` file under `internal/prompts`. Consume `pipeline.Resolver.DecompositionRootForTarget` and existing role/task display helpers; do not modify pipeline YAML/schema/validation, resolver topology implementation beyond API consumption, master prompt sections, output-entry manifest validation, artifact propagation, docs, ADRs, broad end-to-end scenario tests, or unrelated prompt prose.

spec_ref: specs/goals/20260523-master-planning-task.md

Validation: focused route-data tests plus `go test ./internal/prompts/...`.

### Task 2: INITIAL_PLANNING Route Rendering

desc: Render INITIAL_PLANNING from the resolved route data so simple examples target the specialized planning role-pair, fan-out examples target the mapped master role-pair when available, and missing-master rendering falls back to one specialized planning task.

done_when: `wake_initial_planning.tmpl` consumes the resolved simple and fan-out target fields from wake data for both explicit entry-point and classification rendering; rendered simple-goal examples contain exactly one task using the specialized role-pair, task type, display name, and task ID prefix; rendered fan-out examples contain exactly one task using the mapped master role-pair, task type, display name, and task ID prefix when `HasFanOutTarget` is true; rendered missing-master fallback contains exactly one specialized planning task and no invented master role-pair; prompt tests cover explicit `general-objective`, `functional-spec`, `detailed-spec`, and `technical-spec` routes, classification data, mapped master targets, and missing-master fallback; `go test ./internal/prompts/...` passes.

scope: `internal/prompts/templates/wake_initial_planning.tmpl`, `internal/prompts/builder_test.go`, and focused prompt helper tests needed to assert rendered INITIAL_PLANNING output. Use the route data produced by Task 1; do not implement resolver/topology traversal, modify pipeline YAML/schema/validation, change master prompt sections, output-entry manifest validation, artifact propagation, docs, ADRs, broad end-to-end scenario tests, or unrelated prompt prose.

spec_ref: specs/goals/20260523-master-planning-task.md

depends_on: Task 1

Validation: focused prompt rendering tests plus `go test ./internal/prompts/...`.

## Shared-File Audit

| File | Task 1 | Task 2 | Dependency |
|------|--------|--------|------------|
| `internal/prompts/wake.go` | Owns route-data construction | Reads generated fields only | Task 2 depends on Task 1 |
| `internal/prompts/templates/wake_initial_planning.tmpl` | Out of scope | Owns rendering | Task 2 depends on Task 1 |
| `internal/prompts/builder_test.go` or focused prompt `_test.go` | Owns route-data tests | Owns render tests | Task 2 depends on Task 1 because prompt test files are shared |

Sibling overlap: `architecture-2-b-code-planning-0` is expected to consume this finalized route-data contract and further tighten the prompt contract, including removal of old multi-task guidance. It already declares `task_depends_on: ["architecture-2-a-code-planning-0"]` in the sibling architecture plan.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Every planning fan-out is preceded by a consolidated master task unless an upstream step already provides consolidation. | Goal spec `Design / One Rule`, lines 60-69 | Task 1, Task 2 for entry-point prompt routing; existing `architecture-1-code-planning-0-*` for topology | Covered |
| 2 | INITIAL_PLANNING creates exactly one specialized planning task for simple entry-point work. | Goal spec `Case A / Case B Routing`, lines 178-181; `Orchestrator Behavior Change`, lines 406-409 | Task 1, Task 2 | Covered |
| 3 | INITIAL_PLANNING creates exactly one matching decomposition-root master task when entry-point work would otherwise fan out. | Goal spec `Case A / Case B Routing`, lines 178-181; `Orchestrator Behavior Change`, lines 406-414 | Task 1, Task 2 | Covered |
| 4 | Entry-points remain configured as specialized targets and master routing is selected only during INITIAL_PLANNING. | Goal spec `Case A / Case B Routing`, lines 163-181; entry-point YAML lines 202-205 | Task 1, Task 2; existing `architecture-1-code-planning-0-c` preserves YAML targets | Covered |
| 5 | Simple-vs-fan-out criteria are retained, and uncertainty defaults to the master route. | Goal spec `Case A / Case B Routing`, lines 188-197 | Task 2; sibling `architecture-2-b-code-planning-0` tightens prompt wording | Covered |
| 6 | Master routing uses resolver data from explicit `decomposition-root: true` metadata, not generic per-subtask inference. | Goal spec `Orchestrator Behavior Change`, lines 411-417; architecture-2-a `Constraints`, lines 31-35 | Task 1 | Covered |
| 7 | Wake data exposes, for each entry-point, specialized simple target role-pair/task-type/display-name/task-ID-prefix. | Architecture-2-a `Wake Route Data Contract`, lines 73-84; `Output done_when`, line 233 | Task 1 | Covered |
| 8 | Wake data exposes optional resolver-mapped master fan-out target role-pair/task-type/display-name/task-ID-prefix when one exists. | Architecture-2-a `Wake Data Builder`, lines 61-65; `Output done_when`, line 233 | Task 1 | Covered |
| 9 | Explicit `Goal.EntryPoint` rendering uses the same resolved entry-point data as classification rendering. | Architecture-2-a `Wake Route Data Contract`, lines 89-90; `Output done_when`, line 233 | Task 1, Task 2 | Covered |
| 10 | Simple-goal task examples target the specialized role-pair. | Architecture-2-a `INITIAL_PLANNING Template Consumer`, lines 99-102; `Output done_when`, line 233 | Task 2 | Covered |
| 11 | Fan-out task examples target the mapped master role-pair when one exists. | Architecture-2-a `INITIAL_PLANNING Template Consumer`, lines 99-102; `Output done_when`, line 233 | Task 2 | Covered |
| 12 | Missing master mappings fall back to exactly one specialized planning task and do not invent a master role-pair. | Architecture-2-a `Missing-master fallback flow`, lines 176-182; `Structural Decisions`, lines 207-213 | Task 1, Task 2 | Covered |
| 13 | Resolver errors while building wake data surface as render errors. | Architecture-2-a `Resolver Topology API`, lines 105-116; `Cross-Cutting Concerns`, line 190 | Task 1 | Covered |
| 14 | Focused prompt tests cover explicit `general-objective`, `functional-spec`, `detailed-spec`, and `technical-spec` routes. | Architecture-2-a `Prompt Tests`, lines 118-128; `Output done_when`, line 233 | Task 1, Task 2 | Covered |
| 15 | Focused prompt tests cover classification data, mapped master targets, and missing-master fallback. | Architecture-2-a `Prompt Tests`, lines 123-128; `Output done_when`, line 233 | Task 1, Task 2 | Covered |
| 16 | `go test ./internal/prompts/...` passes. | Architecture-2-a `Output done_when`, line 233 | Task 1, Task 2 | Covered |
| 17 | Pipeline YAML/schema/validation and resolver topology implementation remain out of this scope except API consumption. | Assigned scope; architecture-2-a `Boundary`, line 223 | Task 1, Task 2 enforce exclusions | Covered |
| 18 | Master prompt sections, output-entry manifest validation, artifact propagation, docs, ADRs, broad end-to-end scenario tests, and unrelated prompt prose remain out of this scope. | Assigned scope; architecture-2-a `Boundary`, line 223 | Task 1, Task 2 enforce exclusions; sibling architecture tasks own those areas | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this task owns focused prompt tests only; broad end-to-end scenarios are explicitly out of scope and owned by `architecture-5-code-planning-0`. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: docs and ADRs are explicitly out of scope and owned by `architecture-5-code-planning-1`. | N/A |

## Output Entries

The structured output JSON contains one entry for Task 1 and one entry for Task 2. Task 2 has `depends_on: ["0"]` because it consumes fields from Task 1 and shares prompt test files.
