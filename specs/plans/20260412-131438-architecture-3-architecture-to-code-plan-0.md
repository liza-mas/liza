# Code Plan: Delete MCP Server Package and Binary

**Task:** architecture-3-architecture-to-code-plan-0
**Architecture:** specs/arch-plan/20260412-120128-architecture-3.md (Scope 0)
**Spec:** specs/goals/20260412-cli-native-access-control.md (Section 3)

## Summary

Delete the entire MCP server codebase: the binary entrypoint (`cmd/liza-mcp/`) and the server package (`internal/mcp/` including `protocol/` and `testdata/`). This is a pure deletion task — no files outside these two directory trees are modified. No other package imports `internal/mcp` or `internal/mcp/protocol` except `cmd/liza-mcp/main.go`, which is itself deleted.

## Pre-conditions

- Worktree is clean (no uncommitted changes)
- `go build ./...` succeeds before deletion (baseline)

## Task 1: Delete MCP server package and binary entrypoint

**Intent:** Remove the MCP server binary entrypoint and the entire MCP server package (handlers, middleware, protocol, tests, testdata) in a single atomic deletion.

**Rationale for single task:** All files being deleted are in two self-contained directory trees (`cmd/liza-mcp/` and `internal/mcp/`). No file outside these trees is modified. The only import dependency is `cmd/liza-mcp/main.go` importing `internal/mcp`, and both are deleted together. Splitting into two tasks would create an intermediate state where one directory exists without its consumer/provider, adding complexity with no benefit.

**Files deleted:**
- `cmd/liza-mcp/main.go` (binary entrypoint, 53 lines)
- `internal/mcp/handlers_readonly.go`
- `internal/mcp/handlers_helpers.go`
- `internal/mcp/handlers_complex.go`
- `internal/mcp/handlers_await_resubmission_test.go`
- `internal/mcp/handlers_await_verdict_test.go`
- `internal/mcp/middleware.go`
- `internal/mcp/concurrency_test.go`
- `internal/mcp/handlers_test.go`
- `internal/mcp/handlers_mutation.go`
- `internal/mcp/handlers_helpers_test.go`
- `internal/mcp/server.go`
- `internal/mcp/schema_consistency_test.go`
- `internal/mcp/protocol/testing.go`
- `internal/mcp/protocol/stdio.go`
- `internal/mcp/protocol/types_test.go`
- `internal/mcp/protocol/types.go`
- `internal/mcp/protocol/errors.go`
- `internal/mcp/protocol/stdio_test.go`
- `internal/mcp/middleware_test.go`
- `internal/mcp/testdata/fakesleep/main.go`
- `internal/mcp/server_test.go`
- `internal/mcp/server_protocol.go`
- `internal/mcp/server_run_test.go`
- `internal/mcp/server_registration.go`
- `internal/mcp/server_dispatch_test.go`

**Directories removed (empty after file deletion):**
- `cmd/liza-mcp/`
- `internal/mcp/protocol/`
- `internal/mcp/testdata/fakesleep/`
- `internal/mcp/testdata/`
- `internal/mcp/`

**Deletion order:**
1. `git rm -r cmd/liza-mcp/` — removes binary entrypoint (only consumer of `internal/mcp`)
2. `git rm -r internal/mcp/` — removes server package, protocol, testdata

**Done when:**
- `cmd/liza-mcp/` directory does not exist (verified: `test ! -d cmd/liza-mcp/`)
- `internal/mcp/` directory does not exist (verified: `test ! -d internal/mcp/`)
- `go build ./...` succeeds with no import errors referencing `internal/mcp` or `internal/mcp/protocol`
- `go test ./...` passes with no test references to deleted packages
- Commit contains only deletions within `cmd/liza-mcp/` and `internal/mcp/` — no other files modified

**Scope boundary:** No files outside `cmd/liza-mcp/` and `internal/mcp/` are touched. Embedded config updates (mcp.json, claude-settings.json), init.go changes, wt_create.go changes, Makefile changes, and supervisor.go changes are all owned by sibling tasks (architecture-3-architecture-to-code-plan-1 through architecture-3-architecture-to-code-plan-3).

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Delete `cmd/liza-mcp/` directory | Arch Scope 0, Goal spec Section 3 ("Remove MCP binary entrypoint") | Task 1 | Covered |
| 2 | Delete `internal/mcp/` directory including `protocol/` and `testdata/` | Arch Scope 0, Goal spec Section 3 ("Remove internal/mcp/") | Task 1 | Covered |
| 3 | `go build ./...` succeeds after deletion | Arch Scope 0 done_when | Task 1 | Covered |
| 4 | `go test ./...` passes after deletion | Arch Scope 0 done_when | Task 1 | Covered |
| 5 | No modifications to files outside deleted directories | Arch Scope 0 scope boundary, task SCOPE field | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: pure deletion of self-contained package with no behavior change to remaining code; existing e2e tests exercise the ops layer which is untouched | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: documentation updates are owned by sibling tasks architecture-4-architecture-to-code-plan-2 and architecture-4a-architecture-to-code-plan-2 | N/A |
