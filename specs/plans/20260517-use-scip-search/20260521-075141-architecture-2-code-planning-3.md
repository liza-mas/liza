# Code Plan: Orchestrator Project-Root SCIP Refresh

Task ID: `architecture-2-code-planning-3`

Source artifacts:
- Goal spec: `specs/goals/20260517-use-scip-search.md`
- Architecture reference: `specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md`
- Prior runtime service plan: `specs/plans/20260517-use-scip-search/20260521-073929-architecture-2-code-planning-0.md`

## Intent

Plan the orchestrator-side lifecycle wiring so every orchestrator wake refreshes project-root SCIP indexes before prompt construction, and plan the prompt-data boundary that exposes only successful absolute project-root index paths without adding prompt wording.

## Source Basis

Based on:
- `specs/goals/20260517-use-scip-search.md`
- `specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md`
- `specs/plans/20260517-use-scip-search/20260521-073929-architecture-2-code-planning-0.md`
- `internal/agent/strategy_orchestrator.go`
- `internal/agent/strategy.go`
- `internal/agent/supervisor.go`
- `internal/agent/prompt.go`
- `internal/prompts/role_context.go`
- `internal/scipsearch/scipsearch.go`
- `INVARIANTS.md`
- `specs/architecture/ADR/README.md`

No assumptions are required.

## Architectural Notes

The orchestrator refresh belongs in `orchestratorStrategy.PreExecution`, which the supervisor calls after a wake has been detected and before it re-reads state for prompt construction. `setAgentToOrchestratingStatus` remains the authoritative status mutation; the SCIP refresh is warning-only side work in the same phase. This preserves the agent identity and ownership invariants in `INVARIANTS.md` section 4 and avoids adding state mutations outside the existing supervisor flow.

Prompt data should query the runtime `internal/scipsearch` available-index boundary with `config.ProjectRoot`, not a task worktree. This task may add role-context data fields or helper extraction needed for later prompt rendering, but it must not add scip-search prompt wording, capability text, README copy, Claude settings changes, task-worktree claim indexing, or submit-for-review regeneration.

## Planned Coding Tasks

### Task 1: Orchestrator PreExecution Project-Root SCIP Refresh

**desc:** Orchestrator PreExecution project-root SCIP refresh: wire the runtime SCIP indexing service into `orchestratorStrategy.PreExecution` after `setAgentToOrchestratingStatus` succeeds so every orchestrator wake refreshes project-root indexes before the supervisor re-reads state and builds the prompt; keep refresh failures warning-only; and test enabled refresh, disabled activation, empty allowlist, graceful indexer failure, and project-root output paths. Out of scope: prompt wording/capability text, README, Claude settings, task worktree claim indexing, submit-for-review regeneration, and task-worktree prompt data.

**done_when:** Orchestrator strategy or supervisor tests prove `PreExecution` invokes project-root SCIP refresh before prompt construction for each orchestrator wake trigger covered by `DetectOrchestratorWakeTriggers`, stores successful indexes under `<project_root>/.liza/scip/`, performs no refresh and creates no index paths when activation is disabled or `config.scip_search` is empty, logs or surfaces indexer failure without preventing orchestrator execution, and never uses a task worktree path for orchestrator index output.

**scope:** In scope: `internal/agent/strategy_orchestrator.go`, focused tests under `internal/agent` for orchestrator `PreExecution` or supervisor wake execution, consumption of the runtime `internal/scipsearch` refresh service planned by `architecture-2-code-planning-0`, warning-only error handling, and verification that refresh targets `config.ProjectRoot`. Out of scope: prompt template wording, `internal/embedded/claude-settings.json`, README/operator docs, task worktree lifecycle wiring, submit-for-review lifecycle wiring, and `.liza/agent-outputs/`.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md#orchestrator-project-root-refresh-internalagentstrategy_orchestratorgo

**depends_on:** none

**task_depends_on:** architecture-2-code-planning-0

### Task 2: Orchestrator Prompt Data Project-Root Index Availability

**desc:** Orchestrator prompt data project-root index availability: extend orchestrator prompt context data so prompt construction can read successful project-root SCIP index paths through the runtime service boundary and carry only language plus absolute index path metadata for later rendering. Out of scope: prompt wording/capability text, base-prompt section rendering, README, Claude settings, task-worktree prompt data, task worktree claim indexing, and submit-for-review regeneration.

**done_when:** Prompt data tests prove orchestrator context construction queries available indexes for `config.ProjectRoot`, includes only successful existing absolute project-root index paths, omits failed or missing language indexes, ignores task worktree `.liza/scip/` paths even when they exist, and leaves prompt wording/template rendering unchanged for this slice.

**scope:** In scope: `internal/agent/prompt.go`, `internal/prompts/role_context.go`, focused tests under `internal/agent` and `internal/prompts` as needed, a role-context data field or helper for successful SCIP index references, and consumption of the runtime `internal/scipsearch` available-index query planned by `architecture-2-code-planning-0`. Out of scope: scip-search command-loop wording, Python capability caveat text, `internal/prompts/templates/base_prompt.tmpl`, `internal/embedded/claude-settings.json`, README/operator docs, task worktree lifecycle wiring, submit-for-review lifecycle wiring, and `.liza/agent-outputs/`.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md#prompt-index-availability-boundary-internalagentpromptgo-internalprompts

**depends_on:** none

**task_depends_on:** architecture-2-code-planning-0

## Dependency Graph

Task 1 and Task 2 can run in parallel after the runtime SCIP indexing service implementation generated from `architecture-2-code-planning-0` is available. They do not share planned production files: Task 1 owns orchestrator `PreExecution` wiring; Task 2 owns prompt-data plumbing.

Architecture-3 prompt-contract work is outside this plan. It should consume the prompt-data field planned by Task 2 when it adds user-visible scip-search prompt text.

## Shared-File Audit

| File/Area | Task 1 | Task 2 | Dependency |
|---|---|---|---|
| `internal/agent/strategy_orchestrator.go` | Adds project-root refresh in `PreExecution` | No planned edit | None |
| `internal/agent/prompt.go` | No planned edit | Adds or extracts orchestrator context data population for project-root available indexes | None |
| `internal/prompts/role_context.go` | No planned edit | Adds SCIP index reference data field or equivalent prompt-data type | None |
| `internal/scipsearch` | Consumes refresh API from `architecture-2-code-planning-0` | Consumes available-index API from `architecture-2-code-planning-0` | External dependency on `architecture-2-code-planning-0` |
| `internal/agent/*_test.go` | Adds focused orchestrator refresh lifecycle tests | May add prompt data tests if they belong in agent package | No shared production file |
| `internal/prompts/*_test.go` | No planned edit | May add role-context data tests if needed | None |
| `internal/prompts/templates/` | No task | No task | Out of scope |
| `.liza/agent-outputs/` | No task | No task | Out of scope |

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Orchestrator wakeup refreshes project-root indexes before prompt construction. | Goal spec Success Criteria 15; Architecture Orchestrator Project-Root Refresh | Task 1 | Covered |
| 2 | Refresh runs on every orchestrator wake path, including blocked-task, discovery, sprint-complete, planning-complete, and many-to-one wakes. | Architecture Orchestrator Project-Root Refresh key decisions | Task 1 | Covered |
| 3 | Successful orchestrator indexes are stored under `<project_root>/.liza/scip/`. | Goal spec MVP Scope; Index Storage; Success Criteria 18 | Task 1 | Covered |
| 4 | Disabled activation is a no-op for orchestrator project-root refresh. | Goal spec Configuration Shape; Behavioral Decisions; assigned done_when | Task 1 | Covered |
| 5 | Empty `config.scip_search` is a no-op for orchestrator project-root refresh. | Goal spec Configuration Shape semantics; assigned done_when | Task 1 | Covered |
| 6 | Indexer failures do not prevent orchestrator execution. | Goal spec Required Agent Prompt Contract; Behavioral Decisions; Success Criteria 23; Architecture Cross-Cutting Concerns | Task 1 | Covered |
| 7 | Orchestrator indexes use the project root and do not depend on task worktree index paths. | Goal spec Success Criteria 16 and 18; Architecture Orchestrator wake data flow; assigned done_when | Task 1, Task 2 | Covered |
| 8 | Prompt data construction can read successful project-root index paths through the service boundary. | Architecture Prompt Index Availability Boundary; assigned scope | Task 2 | Covered |
| 9 | Prompt data includes only existing successful absolute index paths and excludes failed language diagnostics. | Goal spec Required Agent Prompt Contract; Architecture Prompt Index Availability Boundary; assigned done_when | Task 2 | Covered |
| 10 | Prompt wording, command guidance, Python caveat text, README, Claude settings, task worktree claim indexing, and submit-for-review regeneration remain out of scope. | Assigned SCOPE; Architecture Prompt Index Availability Boundary | Task 1, Task 2 | Covered |
| 11 | Runtime code remains stack-agnostic and avoids Liza-specific project commands. | GUARDRAILS.md G1.1; Architecture Constraints | Task 1, Task 2 | Covered |
| 12 | Agent lifecycle and state invariants remain preserved while adding refresh behavior. | GUARDRAILS.md G1.2; INVARIANTS.md sections 4, 5, and 14 | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this is internal supervisor/prompt-data wiring; focused orchestrator strategy/supervisor tests and prompt-data tests exercise the changed behavior without adding a user-facing CLI flow. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: operator documentation is covered by merged `architecture-4-code-planning-0`; this task explicitly excludes README, prompt wording, and Claude settings. | N/A |

## Validation Plan

Each coding task should validate its behavioral surface with focused Go tests:

- Task 1: `go test ./internal/agent -run 'Test.*Orchestrator.*Scip|Test.*PreExecution.*Scip'`
- Task 2: `go test ./internal/agent ./internal/prompts -run 'Test.*Scip.*(Prompt|Context|Index)'`

The final implementation should also run the package-level tests covering touched packages and pre-commit on touched files. Per the worktree build lesson, use the repository test wrapper where broad Go validation is needed, and run `make -C /home/tangi/Workspace/liza/.worktrees/architecture-2-code-planning-3 sync-embedded` only if stale embedded assets cause a build failure.

## Pre-Submit Self-Check

- Task decomposition: two coding tasks, each with one observable implementation intent.
- Shared-file audit: no planned production file is edited by both tasks, so no sibling `depends_on` chain is required.
- Dependency consistency: both tasks depend on the runtime SCIP indexing service plan from `architecture-2-code-planning-0` and do not duplicate that service's responsibilities.
- Scope boundaries: no prompt wording, capability text, README, Claude settings, task worktree claim indexing, submit-for-review regeneration, automatic installation, or `.liza/agent-outputs/` changes are planned.
- Cross-references: every responsibility named in the compliance matrix is owned by Task 1, Task 2, or explicitly marked out of scope.
