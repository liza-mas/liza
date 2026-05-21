# Coding Plan: Task Worktree Creation Indexing

## Scope

Plan coding work for task-worktree SCIP indexing in the claim/create lifecycle only.

In scope:
- `ops.ClaimTask` refreshes task-local SCIP indexes after worktree provisioning and optional `PostWorktreeCmd`, before the supervisor can build the doer prompt from the returned claim.
- `ops.CreateWorktree` refreshes task-local SCIP indexes after Claude config provisioning, pre-commit hook setup, and optional `PostWorktreeCmd` for new and existing healthy worktrees.
- Existing claim/create warning surfaces carry warning-only indexing failures.
- Lifecycle tests cover enabled indexing, disabled activation no-op, graceful indexer failure, clean git status, existing-worktree idempotent refresh, and concurrent task worktree isolation.

Out of scope:
- `submit-for-review` regeneration.
- Reviewer worktree recovery.
- Orchestrator project-root refresh.
- Prompt wording, README, Claude settings, and init-time validation.
- Changes under `.liza/agent-outputs/`.

## Source Basis

- Goal spec: `specs/goals/20260517-use-scip-search.md`
- Architecture: `specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md#task-worktree-creation-indexing-internalopsclaim_taskgo-internalopswt_creatego`
- Prior runtime-service plan: `specs/plans/20260517-use-scip-search/20260521-073929-architecture-2-code-planning-0.md`
- Current integration points read: `internal/ops/claim_task.go`, `internal/ops/claim_task_strategy.go`, `internal/ops/wt_create.go`, `internal/agent/claiming.go`, `internal/scipsearch/scipsearch.go`
- Invariants checked: `INVARIANTS.md` sections 5 and 7, especially three-phase claim, deterministic worktree paths, and clean worktree status.

## Dependency Context

Both tasks depend on the runtime SCIP refresh service from prior work, specifically the completed chain ending in `architecture-2-code-planning-0-coding-2`. That chain owns language selection, refresh execution, available-index discovery, private worktree ignore setup, clean git status mechanics, and concurrent output isolation inside `internal/scipsearch`.

This plan must not reimplement language detection, environment parsing, indexer command planning, private gitdir exclude handling, or prompt rendering. The ops layer should call the runtime service and translate its per-language failures into existing warning channels.

## Task 1: ClaimTask Task-Worktree SCIP Indexing

**desc:** ClaimTask task-worktree SCIP indexing: wire the runtime SCIP indexing service into `ops.ClaimTask` after worktree provisioning and optional `PostWorktreeCmd`, append warning-only indexer failures to `ClaimResult.Warnings`, and test initial doer-claim indexing, disabled activation no-op, failed-indexer graceful claim, clean git status, and concurrent claim isolation. Out of scope: direct `ops.CreateWorktree` wiring, submit-for-review regeneration, orchestrator project-root refresh, prompt wording, README, and Claude settings.

**done_when:** Claim lifecycle tests in `internal/ops` prove `ops.ClaimTask` refreshes enabled detected task worktree indexes under `<worktree>/.liza/scip/` after optional `PostWorktreeCmd` and before supervisor prompt construction would read claim results, disabled activation creates no `.liza/scip/` directory and yields no available task indexes, a failing indexer appends a `scip-search <language>:` warning while the task still transitions to its executing status, generated indexes leave `git status --porcelain` clean, and two concurrent successful claims for different tasks produce distinct absolute index paths with no shared output files.

**scope:** In scope: `internal/ops/claim_task.go`, an ops-local helper for invoking runtime task-worktree refresh and converting per-language failures to existing warning strings, `ClaimResult.Warnings` propagation, and claim lifecycle tests in `internal/ops/claim_task_test.go` using fakeable runtime indexing seams and git-backed temp worktrees. Out of scope: direct `ops.CreateWorktree` wiring, submit-for-review regeneration, reviewer recovery, orchestrator refresh, prompt wording/rendering, README, Claude settings, init-time validation, and `.liza/agent-outputs/`.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md#task-worktree-creation-indexing-internalopsclaim_taskgo-internalopswt_creatego

**depends_on:** []

**task_depends_on:** ["architecture-2-code-planning-0-coding-2"]

**Validation:** `go test ./internal/ops -run 'TestClaimTask_.*Scip|TestClaimTask_.*Index|TestClaimTask_.*Concurrent'` plus `git -C <worktree> status --porcelain` assertions inside the new tests.

## Task 2: CreateWorktree Task-Worktree SCIP Indexing

**desc:** CreateWorktree task-worktree SCIP indexing: wire the runtime SCIP indexing helper into `ops.CreateWorktree` after Claude config provisioning, pre-commit hook setup, and optional `PostWorktreeCmd` for both new and existing healthy worktrees, preserving warning-only failures and idempotent refresh behavior. Out of scope: `ops.ClaimTask` wiring, submit-for-review regeneration, orchestrator project-root refresh, prompt wording, README, and Claude settings.

**done_when:** CreateWorktree lifecycle tests in `internal/ops` prove `ops.CreateWorktree` refreshes enabled detected task worktree indexes under `<worktree>/.liza/scip/` after Claude config provisioning, pre-commit hook setup, and optional `PostWorktreeCmd` for newly created and already-existing healthy worktrees; repeated calls refresh idempotently without duplicate ignore entries or stale path reuse; disabled activation creates no `.liza/scip/` directory and yields no available task indexes; a failing indexer appends a `scip-search <language>:` warning while creation returns success; generated indexes leave `git status --porcelain` clean; and two concurrently created task worktrees receive independent absolute index files with no path collision.

**scope:** In scope: `internal/ops/wt_create.go`, reuse of the ops-local runtime indexing helper from Task 1, `CreateWorktreeResult.Warnings` propagation, and direct worktree lifecycle tests in `internal/ops/wt_create_test.go` covering new worktrees, existing healthy worktrees, warning-only failures, disabled activation, clean status, and concurrent create isolation. Out of scope: `ops.ClaimTask` wiring, submit-for-review regeneration, reviewer recovery, orchestrator refresh, prompt wording/rendering, README, Claude settings, init-time validation, and `.liza/agent-outputs/`.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md#task-worktree-creation-indexing-internalopsclaim_taskgo-internalopswt_creatego

**depends_on:** ["0"]

**task_depends_on:** ["architecture-2-code-planning-0-coding-2"]

**Validation:** `go test ./internal/ops -run 'TestCreateWorktree_.*Scip|TestCreateWorktree_.*Index|TestCreateWorktree_.*Existing|TestCreateWorktree_.*Concurrent'` plus `git -C <worktree> status --porcelain` assertions inside the new tests.

## Dependency Graph

Task 1 -> Task 2

The sequence is intentional because Task 2 reuses the ops-local refresh/warning helper introduced by Task 1. Both tasks also wait for `architecture-2-code-planning-0-coding-2`, which finalizes the runtime service and task-worktree ignore behavior that claim/create wiring should consume rather than duplicate.

## Shared-File Audit

| File/Area | Task(s) | Dependency |
|---|---|---|
| `internal/ops/claim_task.go` | Task 1 | none |
| `internal/ops/claim_task_test.go` | Task 1 | none |
| `internal/ops/wt_create.go` | Task 2 | Task 2 depends on Task 1 for the shared helper |
| `internal/ops/wt_create_test.go` | Task 2 | Task 2 depends on Task 1 for the shared helper |
| `internal/ops/scip_indexing.go` or equivalent helper location | Task 1 creates; Task 2 reuses | Task 2 depends on Task 1 |
| `internal/scipsearch/` | No planned edits | Prior runtime-service tasks own this package |
| `internal/agent/`, `internal/prompts/`, `cmd/liza/` | No planned edits | Prompt rendering, review submission, and orchestrator work are out of scope |
| `.liza/agent-outputs/` | No planned edits | Runtime log state, forbidden |

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Task worktree creation detects enabled languages, runs SCIP indexers, writes task-local indexes, then spawns the assigned agent. | Goal Success Criteria 13; Architecture Scope 2 | Task 1, Task 2 | Covered |
| 2 | Runtime behavior remains strictly gated by `LIZA_ENABLE_SCIP_SEARCH` plus non-empty `config.scip_search`; disabled activation must not create indexes or prompt-available refs. | Goal Configuration Shape; Goal Success Criteria 10; Architecture Constraints | Task 1, Task 2 | Covered |
| 3 | `ops.ClaimTask` refreshes after worktree provisioning and optional `PostWorktreeCmd`, before the doer prompt would be built. | Architecture Task Worktree Creation Indexing key decisions | Task 1 | Covered |
| 4 | `ops.CreateWorktree` refreshes after Claude config provisioning, pre-commit hook setup, and optional `PostWorktreeCmd` for newly created worktrees. | Architecture Task Worktree Creation Indexing key decisions | Task 2 | Covered |
| 5 | `ops.CreateWorktree` refreshes existing healthy worktrees idempotently. | Assigned done_when; Architecture direct create path | Task 2 | Covered |
| 6 | Successful task indexes are stored under `<worktree>/.liza/scip/` and are discoverable only as explicit successful absolute index paths. | Goal Index Storage; Required Agent Prompt Contract; Architecture Prompt Index Availability Boundary | Task 1, Task 2 | Covered |
| 7 | Indexer failure for an enabled language is warning-only: claim/create still succeeds, failed languages are omitted from available indexes, and diagnostics do not become prompt content. | Goal Behavioral Decisions; Goal Success Criteria 23; Architecture Task Worktree Creation Indexing | Task 1, Task 2 | Covered |
| 8 | Generated task indexes do not dirty `git status --porcelain`. | Goal Index Storage; Goal Success Criteria 17; Architecture Constraints | Task 1, Task 2 | Covered |
| 9 | Concurrent task worktrees receive independent index paths without path collisions or shared output files. | Goal Success Criteria 22; Architecture Concurrent task isolation | Task 1, Task 2 | Covered |
| 10 | The ops lifecycle wiring uses the runtime indexing service instead of duplicating language detection, env parsing, command mapping, or git ignore logic. | Prior plan `architecture-2-code-planning-0`; Architecture Runtime SCIP Indexing Service boundary | Task 1, Task 2 | Covered |
| 11 | Liza remains stack-agnostic and must not hardcode Liza-specific build commands or assume a Makefile/Go module outside the indexer contracts. | `GUARDRAILS.md` G1.1; Architecture Constraints | Task 1, Task 2 | Covered |
| 12 | Claim/create warning propagation uses existing result warning channels. | Assigned scope; Architecture Task Worktree Creation Indexing exposes | Task 1, Task 2 | Covered |
| 13 | Submit-for-review regeneration, reviewer recovery, orchestrator refresh, prompt wording, README, and Claude settings remain out of scope. | Assigned scope; Architecture Scope 3 and Scope 4 boundaries | Task 1, Task 2 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | Task 1, Task 2: ops lifecycle tests exercise the public claim/create lifecycle with git-backed worktrees; no separate CLI e2e task is planned because prompt wording and CLI surfaces are out of scope. | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: operator documentation is covered by merged `architecture-4-code-planning-0`, and this task explicitly excludes README, prompt wording, and Claude settings. | N/A |

## Validation Plan

Each coding task should validate its own lifecycle surface with focused Go tests:

- Task 1: `go test ./internal/ops -run 'TestClaimTask_.*Scip|TestClaimTask_.*Index|TestClaimTask_.*Concurrent'`
- Task 2: `go test ./internal/ops -run 'TestCreateWorktree_.*Scip|TestCreateWorktree_.*Index|TestCreateWorktree_.*Existing|TestCreateWorktree_.*Concurrent'`

The final task should also run:

- `go test ./internal/ops`
- `go test ./internal/scipsearch`
- pre-commit on touched files

## Pre-Submit Self-Check

- Task decomposition: two implementation tasks, each with one observable lifecycle intent.
- Shared-file audit: the shared ops helper forces Task 2 to depend on Task 1.
- Dependency audit: both tasks depend on `architecture-2-code-planning-0-coding-2` so lifecycle wiring consumes the runtime service after task-worktree refresh and ignore behavior exist.
- Scope boundaries: no submit-for-review, reviewer recovery, orchestrator refresh, prompt wording, README, Claude settings, init validation, or `.liza/agent-outputs/` work is planned.
- Invariant audit: planned changes preserve three-phase claim state mutation by keeping indexing warning-only and using deterministic `.worktrees/{taskID}` paths; tests must prove clean worktree status and concurrent worktree isolation.
