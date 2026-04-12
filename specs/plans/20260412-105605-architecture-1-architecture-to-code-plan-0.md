# Code Plan: RBAC Validation Module

**Task:** architecture-1-architecture-to-code-plan-0
**Architecture:** specs/arch-plan/20260412-085210-architecture-1.md (Section 2.1, 3.1, 3.2, 7.1)
**Spec:** specs/goals/20260412-cli-native-access-control.md (Section 1)

## Overview

Create the RBAC validation module in `cmd/liza/rbac.go` with four functions: two resolver loading helpers and two authorization checkers. Unit tests in `cmd/liza/rbac_test.go` cover all validation paths.

## Task 1: Implement RBAC validation functions and unit tests

**Intent:** Create the RBAC validation module — four functions in `cmd/liza/rbac.go` and comprehensive unit tests in `cmd/liza/rbac_test.go`.

**Scope:** `cmd/liza/rbac.go` (new), `cmd/liza/rbac_test.go` (new). No modifications to existing files.

**Functions to implement (package `main` in `cmd/liza/`):**

### `loadResolverForRBAC(projectRoot string) (*pipeline.Resolver, error)`

Loads the pipeline config via `pipeline.LoadFrozen(projectRoot)` and wraps it in `pipeline.NewResolver(cfg)`. Returns fail-closed error on load failure with message: `cannot authorize operation: failed to load pipeline config: <underlying error>`.

### `loadResolverFromDir(lizaDir string) (*pipeline.Resolver, error)`

Loads the pipeline config via `pipeline.Load(filepath.Join(lizaDir, "pipeline.yaml"))` and wraps it in `pipeline.NewResolver(cfg)`. Returns fail-closed error on load failure with message: `cannot authorize operation: failed to load pipeline config: <underlying error>`. Used by `add-task`/`add-tasks` where project root detection is not available (arch doc Section 2.1.1).

### `validateAllowedOperation(resolver *pipeline.Resolver, agentID, operationName string) error`

1. Calls `identity.ExtractRole(agentID)` to get the role name. On invalid format, wraps with context: `cannot validate operation "<operationName>" for agent "<agentID>": <underlying error>`.
2. Calls `resolver.AllowedOperations(role)`. On unknown role, wraps with context: `cannot validate operation "<operationName>" for agent "<agentID>": <underlying error>`.
3. Checks if `operationName` is in the returned list — returns `nil` if found.
4. On rejection, returns error: `operation "<operationName>" not allowed for role "<role>" (agent <agentID>)`.

### `validateRoleType(resolver *pipeline.Resolver, agentID string, allowedTypes ...string) error`

1. Calls `identity.ExtractRole(agentID)` to get the role name. On invalid format, wraps with context: `cannot validate role type [<allowedTypes joined>] for agent "<agentID>": <underlying error>`.
2. Calls `resolver.RoleType(role)` — on unknown role, wraps with context: `cannot validate role type [<allowedTypes joined>] for agent "<agentID>": <underlying error>`.
3. Checks if the returned type is in `allowedTypes` — returns `nil` if matched.
4. On rejection, returns error: `command requires role type [<allowedTypes joined>] but agent "<agentID>" has type "<actualType>"`.

### Unit tests (`cmd/liza/rbac_test.go`)

Tests use `testhelpers.SetupPipelineConfig(t, tmpDir)` to write embedded pipeline YAML into a temp directory, then load a resolver from it. This provides production-equivalent role definitions (coder=doer, code-reviewer=reviewer, orchestrator=orchestrator, with their allowed-operations lists).

| Test | Function | Category | Assertion |
|---|---|---|---|
| `TestValidateAllowedOperation_HappyPath` | `validateAllowedOperation` | Happy path | `validateAllowedOperation(resolver, "coder-1", "submit-for-review")` returns `nil` |
| `TestValidateAllowedOperation_Rejection` | `validateAllowedOperation` | Rejection | `validateAllowedOperation(resolver, "coder-1", "submit-verdict")` returns error containing `operation "submit-verdict" not allowed for role "coder"` and `agent coder-1` |
| `TestValidateAllowedOperation_InvalidAgentID` | `validateAllowedOperation` | Invalid format | `validateAllowedOperation(resolver, "badformat", "submit-for-review")` returns error containing `cannot validate operation "submit-for-review"`, `agent "badformat"`, and the underlying format error |
| `TestValidateAllowedOperation_UnknownRole` | `validateAllowedOperation` | Unknown role | `validateAllowedOperation(resolver, "nonexistent-1", "submit-for-review")` returns error containing `cannot validate operation "submit-for-review"`, `agent "nonexistent-1"`, and `unknown role` |
| `TestValidateRoleType_HappyPath` | `validateRoleType` | Happy path | `validateRoleType(resolver, "coder-1", "doer")` returns `nil` |
| `TestValidateRoleType_Rejection` | `validateRoleType` | Rejection | `validateRoleType(resolver, "orchestrator-1", "doer")` returns error containing `command requires role type` and `has type "orchestrator"` |
| `TestValidateRoleType_InvalidAgentID` | `validateRoleType` | Invalid format | `validateRoleType(resolver, "badformat", "doer")` returns error containing `cannot validate role type [doer]`, `agent "badformat"`, and the underlying format error |
| `TestValidateRoleType_UnknownRole` | `validateRoleType` | Unknown role | `validateRoleType(resolver, "nonexistent-1", "doer")` returns error containing `cannot validate role type [doer]`, `agent "nonexistent-1"`, and `unknown role` |
| `TestLoadResolverForRBAC_Success` | `loadResolverForRBAC` | Happy path | Returns non-nil `*pipeline.Resolver` and `nil` error when pipeline config exists |
| `TestLoadResolverForRBAC_MissingConfig` | `loadResolverForRBAC` | Fail-closed | Returns error containing `cannot authorize operation` when no pipeline config exists |
| `TestLoadResolverFromDir_Success` | `loadResolverFromDir` | Happy path | Returns non-nil `*pipeline.Resolver` and `nil` error when pipeline.yaml exists in dir |
| `TestLoadResolverFromDir_MissingConfig` | `loadResolverFromDir` | Fail-closed | Returns error containing `cannot authorize operation` when pipeline.yaml does not exist |

### Dependencies

**Imports for `rbac.go`:**
- `fmt`
- `path/filepath`
- `github.com/liza-mas/liza/internal/identity`
- `github.com/liza-mas/liza/internal/pipeline`

**Imports for `rbac_test.go`:**
- `testing`
- `strings`
- `github.com/liza-mas/liza/internal/testhelpers`

**Existing APIs used (no modifications needed):**
- `pipeline.LoadFrozen(projectRoot string) (*PipelineConfig, error)` — `internal/pipeline/config.go:128`
- `pipeline.Load(path string) (*PipelineConfig, error)` — `internal/pipeline/config.go:162`
- `pipeline.NewResolver(config *PipelineConfig) *Resolver` — `internal/pipeline/resolver.go:18`
- `pipeline.Resolver.AllowedOperations(name string) ([]string, error)` — `internal/pipeline/resolver.go:160`
- `pipeline.Resolver.RoleType(name string) (string, error)` — `internal/pipeline/resolver.go:131`
- `identity.ExtractRole(agentID string) (string, error)` — `internal/identity/resolver.go:113`
- `testhelpers.SetupPipelineConfig(t *testing.T, tmpDir string)` — `internal/testhelpers/pipeline.go:11`

**Done when:** `loadResolverForRBAC`, `loadResolverFromDir`, `validateAllowedOperation`, and `validateRoleType` exist in `cmd/liza/rbac.go`. Validation functions accept `*pipeline.Resolver` (not project root). `loadResolverForRBAC` loads via `pipeline.LoadFrozen(projectRoot)`. `loadResolverFromDir` loads via `pipeline.Load(filepath.Join(lizaDir, "pipeline.yaml"))` for commands that operate without project root detection. Resolver load failures produce fail-closed errors. All error paths (rejection, invalid format, unknown role) return actionable error messages that include both the command context (operation name or allowed types) and the agent identity (agent ID and role where available) — underlying errors from identity.ExtractRole and resolver methods are wrapped, not returned raw. Unit tests cover happy path, rejection, invalid format, and unknown role cases, asserting wrapped context in every error path. All tests pass: `go test ./cmd/liza/ -run TestValidateAllowed -count=1` and `go test ./cmd/liza/ -run TestValidateRoleType -count=1` and `go test ./cmd/liza/ -run TestLoadResolver -count=1` exit 0.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | `loadResolverForRBAC` loads via `pipeline.LoadFrozen(projectRoot)` | done_when, arch doc Section 2.1/6.1 | Task 1 | Covered |
| 2 | `loadResolverFromDir` loads via `pipeline.Load(filepath.Join(lizaDir, "pipeline.yaml"))` | done_when, arch doc Section 2.1.1/6.1 | Task 1 | Covered |
| 3 | `validateAllowedOperation` accepts `*pipeline.Resolver`, checks operation in allowed-operations list | done_when, arch doc Section 3.1 | Task 1 | Covered |
| 4 | `validateRoleType` accepts `*pipeline.Resolver`, checks role type against allowed types | done_when, arch doc Section 3.1 | Task 1 | Covered |
| 5 | Resolver load failures produce fail-closed errors | done_when, arch doc Section 3.2/6.2 | Task 1 | Covered |
| 6 | All error paths return actionable messages with command context and agent identity; underlying errors are wrapped, not raw | done_when, arch doc Section 3.2 | Task 1 | Covered |
| 7 | Unit tests: happy path | done_when, arch doc Section 7.1 | Task 1 | Covered |
| 8 | Unit tests: rejection | done_when, arch doc Section 7.1 | Task 1 | Covered |
| 9 | Unit tests: invalid format | done_when, arch doc Section 7.1 | Task 1 | Covered |
| 10 | Unit tests: unknown role | done_when, arch doc Section 7.1 | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: internal module with no user-visible behavior; wiring into commands is sibling task architecture-1-architecture-to-code-plan-1 | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: internal module with no user-visible change; no CLI syntax or behavior changes | N/A |
