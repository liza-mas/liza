# Code Plan: JSON Output Infrastructure Package

**Task:** architecture-2a-architecture-to-code-plan-0
**Architecture:** specs/arch-plan/20260412-110120-architecture-2a.md (Sections 2.1, 2.2, 3.1–3.4, 3.6, 9.1, 9.2, Scope 0)
**Spec:** specs/goals/20260412-cli-native-access-control.md (Section 2)

## Overview

Create the `internal/jsonout/` package providing error classification, JSON envelope types, and output helpers. Add CLI helpers `addJSONFlag`/`isJSON` and `ErrAlreadyWritten` handling to `cmd/liza/main.go`. This is the foundation layer that Scope 1 (command wiring) depends on.

## Task 1 (CP1): Create `internal/jsonout/` package with error classification, envelope types, and unit tests

**Intent:** Provide a standalone package that maps Go errors to string error codes and writes structured JSON envelopes to an `io.Writer`.

**Files (all new):**
- `internal/jsonout/classify.go` — `ClassifyError(err error) (code string, message string)`
- `internal/jsonout/envelope.go` — `Envelope`, `ErrorDetail`, `ErrAlreadyWritten`, `WriteResult(w io.Writer, result any, warnings []string, err error) error`
- `internal/jsonout/classify_test.go` — Unit tests for error classification
- `internal/jsonout/envelope_test.go` — Unit tests for envelope serialization

**Done when:** Package `internal/jsonout` compiles. `ClassifyError` maps all error types from arch doc Section 3.3: `*errors.NotFoundError` → `not_found`/`"resource not found"`, `*ops.PreconditionError` → `validation`/`err.Reason`, `*ops.PostWriteValidationError` → `validation`/`"validation failed: precondition not met"`, `*ops.IntegrationFailedError` → `validation`/`"integration failed: <reason>"`, `*ops.OperationalError` → `internal`/`err.Message`, lock timeout compound pattern (`"lock"` AND `"timeout"`/`"timed out"`) → `lock_timeout`/`"lock acquisition timed out"`, race condition patterns (`"race condition"`, `"changed concurrently"`) → `race_condition`/`"state changed concurrently, retry"`, validation string patterns (`"not IMPLEMENTING"`, `"not REVIEWING"`, `"not READY_FOR_REVIEW"`, `"not CODE_READY_FOR_REVIEW"`, `"not CODE_APPROVED"`, `"not APPROVED"`, `"must be"`, `"is required"`, `"invalid task ID"`, `"validation failed"`, `"must include"`, `"mandatory"`) → `validation`/`"validation failed"`, not-found string patterns (`"not found"`, `"does not exist"`) → `not_found`/`"resource not found"`, default → `internal`/`"internal error"`. Raw `err.Error()` is never exposed for untyped errors. `WriteResult` writes a success envelope (`{"ok":true,"result":...}`) when err is nil and an error envelope (`{"ok":false,"error":{...}}`) returning `ErrAlreadyWritten` when err is non-nil. Warnings field is present only when non-empty. `TestClassifyError_NotFoundError`, `TestClassifyError_PreconditionError`, `TestClassifyError_PostWriteValidationError`, `TestClassifyError_OperationalError`, `TestClassifyError_IntegrationFailedError`, `TestClassifyError_LockTimeout`, `TestClassifyError_RaceCondition`, `TestClassifyError_ValidationPatterns`, `TestClassifyError_DefaultInternal`, `TestClassifyError_NoRawLeak` all pass in `classify_test.go`. `TestWriteResult_Success`, `TestWriteResult_SuccessWithWarnings`, `TestWriteResult_Error`, `TestWriteResult_NullResult`, `TestWriteResult_SuccessNoWarnings` all pass in `envelope_test.go`. `go test ./internal/jsonout/...` passes.

**Scope:** `internal/jsonout/classify.go`, `internal/jsonout/envelope.go`, `internal/jsonout/classify_test.go`, `internal/jsonout/envelope_test.go`. No modifications to existing files.

**Implementation notes:**
- Extract error classification logic from `internal/mcp/server_protocol.go:197-247` and `stringErrorRules` (lines 154-179), adapting from numeric JSON-RPC codes to string codes per the arch doc Section 3.3 mapping table.
- MCP-specific error types (`NotInitializedError`, `RoleError`, `InputShapeError`) are NOT included — they don't occur in CLI context.
- `Envelope` struct: `OK bool`, `Result any`, `Warnings []string`, `Error *ErrorDetail` with json tags per Section 3.2.
- `WriteResult` calls `ClassifyError` when `err != nil` to populate the error envelope.
- String error rules order: not-found patterns, race condition patterns, validation patterns — matching the MCP's `stringErrorRules` order.
- Lock timeout is a compound match (`"lock"` AND `"timeout"`/`"timed out"`), checked before string rules, matching MCP's `classifyError` structure.

## Task 2 (CP2): Add `addJSONFlag`, `isJSON` helpers and `ErrAlreadyWritten` handling to `cmd/liza/main.go`

**Intent:** Register the `--json` flag helper functions and modify `main()` to suppress duplicate stderr output when a JSON error envelope has already been written.

**Files:**
- `cmd/liza/main.go` (modify) — Add `addJSONFlag(cmd *cobra.Command)`, `isJSON(cmd *cobra.Command) bool`, modify `main()` to check `ErrAlreadyWritten`

**Done when:** `cmd/liza/main.go` compiles with `addJSONFlag(cmd)` registering a `--json` bool flag on the given command, `isJSON(cmd)` returning the flag value, and `main()` exiting with code 1 without printing to stderr when `errors.Is(err, jsonout.ErrAlreadyWritten)` is true. The existing error path (non-ErrAlreadyWritten) remains unchanged: prints `"Error: %v\n"` to stderr and exits 1. `go build ./cmd/liza/` succeeds. `addJSONFlag` follows the existing `addAgentIDFlag` pattern (per-command flag registration, not PersistentFlag on root). Import added: `"github.com/liza-mas/liza/internal/jsonout"` and `"errors"`.

**Scope:** `cmd/liza/main.go` only. No modifications to command files (flag registration on specific commands is Scope 1).

**Depends on:** Task 1 (CP1) — requires `internal/jsonout` package to exist for `ErrAlreadyWritten` import.

**Implementation notes:**
- `addJSONFlag` mirrors `addAgentIDFlag` pattern at line 123: `cmd.Flags().Bool("json", false, "output result as structured JSON")`.
- `isJSON` mirrors `requireAgentID` pattern: `v, _ := cmd.Flags().GetBool("json"); return v`.
- `main()` modification per arch doc Section 3.6: wrap existing error handling with `errors.Is(err, jsonout.ErrAlreadyWritten)` check.

## Dependency Graph

```
CP1 (internal/jsonout/ package)
  └── CP2 (cmd/liza/main.go helpers) — depends on CP1
```

CP1 has no dependencies. CP2 depends on CP1 (import `jsonout.ErrAlreadyWritten`).

No shared files between tasks — CP1 creates new files in `internal/jsonout/`, CP2 modifies `cmd/liza/main.go`.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | `Envelope` struct with `ok`, `result`, `warnings`, `error` fields and json tags | Arch doc Section 3.1, 3.2 | CP1 | Covered |
| 2 | `ErrorDetail` struct with `code` and `message` fields | Arch doc Section 3.2 | CP1 | Covered |
| 3 | `ErrAlreadyWritten` sentinel error | Arch doc Section 3.2 | CP1 | Covered |
| 4 | `WriteResult(w, result, warnings, err)` writes success envelope when err is nil | Arch doc Section 3.2 | CP1 | Covered |
| 5 | `WriteResult` writes error envelope and returns `ErrAlreadyWritten` when err is non-nil | Arch doc Section 3.2 | CP1 | Covered |
| 6 | `ClassifyError` maps `*errors.NotFoundError` → `not_found` | Arch doc Section 3.3 | CP1 | Covered |
| 7 | `ClassifyError` maps `*ops.PreconditionError` → `validation` with `err.Reason` | Arch doc Section 3.3 | CP1 | Covered |
| 8 | `ClassifyError` maps `*ops.PostWriteValidationError` → `validation` | Arch doc Section 3.3 | CP1 | Covered |
| 9 | `ClassifyError` maps `*ops.IntegrationFailedError` → `validation` with reason | Arch doc Section 3.3 | CP1 | Covered |
| 10 | `ClassifyError` maps `*ops.OperationalError` → `internal` with `err.Message` | Arch doc Section 3.3 | CP1 | Covered |
| 11 | `ClassifyError` maps lock timeout compound pattern → `lock_timeout` | Arch doc Section 3.3 | CP1 | Covered |
| 12 | `ClassifyError` maps race condition string patterns → `race_condition` | Arch doc Section 3.3 | CP1 | Covered |
| 13 | `ClassifyError` maps validation string patterns → `validation` | Arch doc Section 3.3 | CP1 | Covered |
| 14 | `ClassifyError` maps not-found string patterns → `not_found` | Arch doc Section 3.3 | CP1 | Covered |
| 15 | `ClassifyError` default → `internal`/`"internal error"` | Arch doc Section 3.3 | CP1 | Covered |
| 16 | Raw `err.Error()` never exposed for untyped errors | Arch doc Section 3.3 (message safety) | CP1 | Covered |
| 17 | MCP-specific types excluded (`NotInitializedError`, `RoleError`, `InputShapeError`) | Arch doc Section 3.3 | CP1 | Covered |
| 18 | `addJSONFlag(cmd)` registers `--json` bool flag per-command | Arch doc Section 3.4 | CP2 | Covered |
| 19 | `isJSON(cmd)` returns `--json` flag value | Arch doc Section 3.4 | CP2 | Covered |
| 20 | `main()` suppresses stderr when `ErrAlreadyWritten` is returned, exits 1 | Arch doc Section 3.6 | CP2 | Covered |
| 21 | Unit tests for error classification (Section 9.1: 10 test cases) | Arch doc Section 9.1 | CP1 | Covered |
| 22 | Unit tests for envelope (Section 9.2: 5 test cases) | Arch doc Section 9.2 | CP1 | Covered |
| 23 | `warnings` field omitted when empty, present when non-empty | Arch doc Section 3.1 | CP1 | Covered |
| 24 | `result` is `null` for void-success | Arch doc Section 3.1, 5.4 | CP1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: infrastructure package with no user-visible behavior; commands are wired in Scope 1 which owns integration tests | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: internal package, no user-facing change until Scope 1 wires commands | N/A |
