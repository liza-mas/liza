# Code Plan: Wire RBAC into State-Mutating CLI Commands

**Task:** architecture-1-code-plan-1a
**Architecture:** specs/arch-plan/20260412-085210-architecture-1.md (Sections 2, 4, 5, 7.2, 8)
**Spec:** specs/goals/20260412-cli-native-access-control.md (Section 1)
**Predecessor plan:** specs/plans/20260412-105605-architecture-1-architecture-to-code-plan-0.md

## Overview

Wire the RBAC validation module (created by predecessor plan) into all 16 state-mutating CLI commands listed in the arch doc RBAC Coverage Matrix (Section 5). Normalize wt-merge's inline RBAC to use the shared helpers. Add integration tests covering RBAC rejection for each identity pattern.

**Prerequisite:** All tasks in this plan assume `cmd/liza/rbac.go` exists with `loadResolverForRBAC`, `loadResolverFromDir`, `validateAllowedOperation`, and `validateRoleType` (delivered by predecessor plan's coding task). Tasks run after that coding task is merged.

## Task 1 (CP1): Wire RBAC into cmd_review.go commands

**Intent:** Add RBAC validation calls to all 5 state-mutating commands in cmd_review.go. All use the `--agent-id` flag pattern with `loadResolverForRBAC(projectRoot)` and `validateAllowedOperation`.

**Scope:** `cmd/liza/cmd_review.go`

**Changes per command (all follow arch doc Section 4.2 Pattern 2):**

Each command's `RunE` gains two lines after `requireProjectRoot()` and before the `commands.*Command()` call:

| Command | Operation Name |
|---|---|
| `submit-for-review` (line 44) | `"submit-for-review"` |
| `handoff` (line 79) | `"handoff"` |
| `submit-verdict` (line 134) | `"submit-verdict"` |
| `await-verdict` (line 216) | `"await-verdict"` |
| `await-resubmission` (line 253) | `"await-resubmission"` |

Pattern for each (example: submit-for-review, inserted between line 43 and 44):
```go
resolver, err := loadResolverForRBAC(projectRoot)
if err != nil { return err }
if err := validateAllowedOperation(resolver, agentID, "submit-for-review"); err != nil {
    return err
}
```

No new imports needed — `loadResolverForRBAC` and `validateAllowedOperation` are in the same package (`main`).

**Done when:** All 5 commands in cmd_review.go (submit-for-review, handoff, submit-verdict, await-verdict, await-resubmission) have `loadResolverForRBAC(projectRoot)` + `validateAllowedOperation(resolver, agentID, "<command-name>")` calls inserted after identity and project root resolution, before the handler call. `go build ./cmd/liza/` succeeds. `go test ./cmd/liza/ -run TestMutationCommandWiring -count=1` passes (existing tests use valid roles so RBAC permits them).

## Task 2 (CP2): Normalize wt-merge RBAC in cmd_worktree.go

**Intent:** Replace wt-merge's inline RBAC logic (cmd_worktree.go lines 99-119) with the shared `loadResolverForRBAC` + `validateRoleType` helpers, removing duplicated identity/pipeline/resolver code.

**Scope:** `cmd/liza/cmd_worktree.go`

**Current inline RBAC (lines 99-119):**
```go
role, err := identity.ExtractRole(agentID)
if err != nil { return err }

projectRoot, err := requireProjectRoot()
if err != nil { return err }

cfg, cfgErr := pipeline.LoadFrozen(projectRoot)
if cfgErr != nil { return fmt.Errorf("load pipeline config: %w", cfgErr) }
resolver := pipeline.NewResolver(cfg)
roleType, rtErr := resolver.RoleType(role)
if rtErr != nil { return fmt.Errorf("unknown role %q: %w", role, rtErr) }
if roleType != "reviewer" {
    return fmt.Errorf("wt-merge requires a reviewer role (got: %s)", role)
}
```

**Replacement (per arch doc Section 4.2 Pattern 4):**
```go
projectRoot, err := requireProjectRoot()
if err != nil { return err }

resolver, err := loadResolverForRBAC(projectRoot)
if err != nil { return err }
if err := validateRoleType(resolver, agentID, "reviewer"); err != nil {
    return err
}
```

Note: `requireProjectRoot()` moves before RBAC (was after `identity.ExtractRole`). Error messages change from the inline format to the standardized RBAC module format (e.g., `command requires role type [reviewer] but agent "coder-1" has type "doer"`). This is intentional — the arch doc Section 3.2 defines the canonical error contract.

**Import cleanup:** Remove `"fmt"`, `"github.com/liza-mas/liza/internal/identity"`, and `"github.com/liza-mas/liza/internal/pipeline"` — all are only used by the inline RBAC block being replaced.

**Done when:** wt-merge's RunE in cmd_worktree.go uses `loadResolverForRBAC(projectRoot)` + `validateRoleType(resolver, agentID, "reviewer")` instead of inline identity extraction, pipeline loading, and role-type checking. Imports `fmt`, `identity`, and `pipeline` are removed from cmd_worktree.go. `go build ./cmd/liza/` succeeds. `go test ./cmd/liza/ -run TestMutationCommandWiring/wt-merge -count=1` passes (existing test uses code-reviewer-3 which has reviewer type, so RBAC permits it). The existing test's error assertion (`"task must be in an approved state to merge"`) still holds because the RBAC check passes before the handler's status check.

## Task 3 (CP3): Wire RBAC into cmd_task.go commands

**Intent:** Add RBAC validation calls to all 10 state-mutating commands in cmd_task.go, using the appropriate resolver loading and validation pattern for each identity source.

**Scope:** `cmd/liza/cmd_task.go`

**Changes per command:**

### Pattern 1: Positional identity + loadResolverForRBAC (arch doc Section 4.2 Pattern 1)

| Command | Check | Insertion point |
|---|---|---|
| `claim-task` | `validateRoleType(resolver, agentID, "doer")` | After `requireProjectRoot()` (line 40), before `commands.ClaimTaskCommand` (line 42) |

### Pattern 2: --agent-id flag + loadResolverForRBAC (arch doc Section 4.2 Pattern 2)

| Command | Operation Name | Insertion point |
|---|---|---|
| `mark-blocked` | `"mark-blocked"` | After `requireProjectRoot()` (line 224), before `commands.MarkBlockedCommand` (line 226) |
| `write-checkpoint` | `"write-checkpoint"` | After `requireProjectRoot()` (line 380), before `commands.WriteCheckpointCommand` (line 412) |
| `set-task-output` | `"set-task-output"` | After `requireProjectRoot()` (line 452), before `commands.SetTaskOutputCommand` (line 470) |

### Pattern 3: Auto-resolved orchestrator + loadResolverForRBAC (arch doc Section 4.2 Pattern 3)

| Command | Check | Insertion point |
|---|---|---|
| `supersede-task` | `validateAllowedOperation(resolver, agentID, "supersede-task")` | After `requireProjectRoot()` (line 178), before `commands.SupersedeTaskCommand` (line 181) |
| `cancel-task` | `validateAllowedOperation(resolver, agentID, "cancel-task")` | After `requireProjectRoot()` (line 328), before `commands.CancelTaskCommand` (line 330) |
| `assess-blocked` | `validateRoleType(resolver, orchestratorID, "orchestrator")` | After `requireProjectRoot()` (line 257), before `commands.AssessBlockedCommand` (line 260) |
| `assess-hypothesis-exhausted` | `validateRoleType(resolver, orchestratorID, "orchestrator")` | After `requireProjectRoot()` (line 291), before `commands.AssessHypothesisExhaustedCommand` (line 295) |

Note: `supersede-task` and `cancel-task` use variable name `agentID` (from `resolveOrchestratorID`). `assess-blocked` and `assess-hypothesis-exhausted` use `orchestratorID` (also from `resolveOrchestratorID`). The variable name differs per command but the resolution function is the same.

### Pattern 3a: Auto-resolved orchestrator + loadResolverFromDir (arch doc Section 4.2 Pattern 3a)

| Command | Check | Insertion point |
|---|---|---|
| `add-task` | `validateRoleType(resolver, orchestratorID, "orchestrator")` | After `orchestratorID` resolution (line 131-134), before `commands.AddTaskCommand` (line 136). Uses `loadResolverFromDir(filepath.Dir(statePath))` where `statePath` is resolved at lines 67-77. |
| `add-tasks` | `validateAllowedOperation(resolver, orchestratorID, "add-tasks")` | After `orchestratorID` resolution (line 512-514) and `statePath` definition (line 517), before `commands.AddTasksCommand` (line 520). Uses `loadResolverFromDir(filepath.Dir(statePath))`. |

No new imports needed — all functions are in the same package. `filepath` is already imported.

**Done when:** All 10 commands in cmd_task.go have RBAC validation calls. Commands using `requireProjectRoot()` (claim-task, supersede-task, cancel-task, mark-blocked, assess-blocked, assess-hypothesis-exhausted, write-checkpoint, set-task-output) use `loadResolverForRBAC(projectRoot)`. Commands without project root dependency (add-task, add-tasks) use `loadResolverFromDir(filepath.Dir(statePath))`. Each command uses the correct validation function per the RBAC Coverage Matrix (arch doc Section 5): `validateAllowedOperation` for operation-based checks, `validateRoleType` for type-based checks. `go build ./cmd/liza/` succeeds. `go test ./cmd/liza/ -run TestMutationCommandWiring -count=1` passes (existing tests use valid roles).

## Task 4 (CP4): RBAC rejection integration tests

**Intent:** Add integration tests to mutation_wiring_test.go that verify RBAC rejection for each identity pattern: positional arg, --agent-id flag, and auto-resolved orchestrator via env-var.

**Scope:** `cmd/liza/mutation_wiring_test.go`

**Depends on:** CP1, CP2, CP3 (all RBAC wiring must be in place for rejection tests to exercise the new code paths).

**Tests to add (within `TestMutationCommandWiring`):**

### Test 1: `claim-task rejects orchestrator agent` (positional pattern)

Setup: task in DRAFT_CODE status. Execute: `claim-task <task-id> orchestrator-1`. Assert: error returned, error string contains `command requires role type [doer]` and `orchestrator-1`. This exercises the positional identity pattern where the agent ID is the second positional arg.

### Test 2: `submit-verdict rejects coder agent` (--agent-id flag pattern)

Setup: task in REVIEWING status. Execute: `submit-verdict <task-id> APPROVED --agent-id coder-1`. Assert: error returned, error string contains `operation "submit-verdict" not allowed for role "coder"` and `coder-1`. This exercises the --agent-id flag pattern where a coder attempts a reviewer-only operation.

### Test 3: `add-task rejects non-orchestrator via env-var` (auto-resolved env-var pattern)

Setup: standard test project (pipeline config present). Set `LIZA_AGENT_ID=coder-1` via `t.Setenv`, do NOT pass `--agent-id` flag. Execute: `add-task --id new-task --desc "test" --spec "s" --done "d" --scope "sc"`. Assert: error returned, error string contains `command requires role type [orchestrator]` and `coder-1`. This exercises the `resolveOrchestratorID` env-var early-return path (main.go:62-68): `identity.Resolve` returns `coder-1` from env, `resolveOrchestratorID` returns immediately without state read, then `validateRoleType` rejects because coder is not orchestrator type.

### Implementation notes

All tests use `setupMutationTestProject` and `executeRootCommand` (existing helpers). Error assertions use `strings.Contains` on `err.Error()` to check for specific RBAC error substrings defined by the error contract (arch doc Section 3.2). Tests that set env vars use `t.Setenv` for automatic cleanup.

**Done when:** `TestMutationCommandWiring` in `cmd/liza/mutation_wiring_test.go` includes subtests for all 3 identity patterns: (1) positional — `claim-task orchestrator-1` returns role-type error containing `command requires role type [doer]`, (2) --agent-id flag — `submit-verdict --agent-id coder-1` returns allowed-operation error containing `operation "submit-verdict" not allowed for role "coder"`, (3) auto-resolved env-var — `LIZA_AGENT_ID=coder-1` with `add-task` (no --agent-id flag) returns role-type error containing `command requires role type [orchestrator]`. `go test ./cmd/liza/ -run TestMutationCommandWiring -count=1` passes with all new subtests green.

## Dependency Graph

```
CP1 (cmd_review.go) ──┐
CP2 (cmd_worktree.go) ─┼── CP4 (mutation_wiring_test.go)
CP3 (cmd_task.go) ─────┘
```

CP1, CP2, CP3 are independent (different files, no shared modifications) and can run in parallel. CP4 depends on all three.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | All 16 commands in RBAC Coverage Matrix with "State-mutating with RBAC" have validation calls | done_when, arch doc Section 5 | CP1 (5), CP2 (1), CP3 (10) | Covered |
| 2 | Commands that call requireProjectRoot() use loadResolverForRBAC(projectRoot) | done_when, arch doc Section 2.1/6.1 | CP1, CP2, CP3 | Covered |
| 3 | add-task and add-tasks use loadResolverFromDir(filepath.Dir(statePath)) to avoid new project-root dependency | done_when, arch doc Section 2.1.1 | CP3 | Covered |
| 4 | wt-merge inline RBAC (cmd_worktree.go:99-119) replaced by loadResolverForRBAC + validateRoleType | done_when, arch doc Section 4.2 Pattern 4 | CP2 | Covered |
| 5 | Integration test: positional pattern — claim-task T1 orchestrator-1 returns role-type error | done_when | CP4 (Test 1) | Covered |
| 6 | Integration test: --agent-id flag pattern — submit-verdict T1 APPROVED --agent-id coder-1 returns allowed-operation error | done_when | CP4 (Test 2) | Covered |
| 7 | Integration test: auto-resolved env-var pattern — LIZA_AGENT_ID=coder-1 on add-task triggers role-type rejection | done_when | CP4 (Test 3) | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | CP4 | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: RBAC wiring is internal CLI validation with no user-facing syntax changes. Documentation and contract updates are owned by sibling task architecture-4a (Phase 4). | N/A |
