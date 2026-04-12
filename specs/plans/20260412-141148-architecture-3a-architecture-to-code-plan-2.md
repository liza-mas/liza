# Code Plan: Delete MCP Server Packages and Obsolete Lesson

**Task:** architecture-3a-architecture-to-code-plan-2
**Date:** 2026-04-12
**Architecture ref:** specs/arch-plan/20260412-112517-architecture-3a.md (Task 2)
**Spec ref:** specs/goals/20260412-cli-native-access-control.md (Section 3)

## Context

This is the final task in the MCP server removal sequence. Sibling tasks have already
removed all code references to MCP from non-MCP source files (Task 0: integration points)
and build tooling (Task 1: Makefile, GoReleaser, install script, gitignore). No production
code outside the deleted directories imports `internal/mcp` or `internal/mcp/protocol`.

This task deletes the now-unreferenced MCP packages and binary entrypoint, plus the
dual-surface lesson that is obsolete once the MCP surface no longer exists.

### Deletion inventory

| Path | Type | Contents |
|------|------|----------|
| `cmd/liza-mcp/` | Directory | `main.go` (binary entrypoint, imports `internal/mcp`) |
| `internal/mcp/` | Directory | 18 `.go` files (server, handlers, middleware, registration, tests) |
| `internal/mcp/protocol/` | Subdirectory | 6 `.go` files (types, stdio transport, errors, test helpers) |
| `internal/mcp/testdata/` | Subdirectory | `fakesleep/` test fixture directory |
| `lessons/agents/cli-mcp-surface-sync.md` | File | Lesson about keeping CLI and MCP surfaces in sync |

### Import safety verification

Only `cmd/liza-mcp/main.go` imports `internal/mcp`. No Go source file outside the
deleted directories imports `github.com/liza-mas/liza/internal/mcp` or
`github.com/liza-mas/liza/internal/mcp/protocol`. All third-party imports from these
packages are internal to the liza module — no `go mod tidy` needed.

## Task CP1: Delete MCP server packages, binary entrypoint, and obsolete lesson

**Single intent:** Remove the MCP server code tree, binary entrypoint, obsolete lesson file, and the dangling GUARDRAILS.md trigger row.

### Step 1: Delete directories and files

Delete the following using `git rm -r`:

1. **`cmd/liza-mcp/`** — MCP binary entrypoint directory (1 file: `main.go`)
2. **`internal/mcp/`** — MCP server package directory (18 `.go` files, `protocol/` subdirectory with 6 `.go` files, `testdata/` subdirectory with `fakesleep/` fixture)
3. **`lessons/agents/cli-mcp-surface-sync.md`** — obsolete dual-surface sync lesson

### Step 2: Remove trigger row from GUARDRAILS.md

In `GUARDRAILS.md`, delete line 49 (the trigger row referencing `cli-mcp-surface-sync.md`):

```
| Modifying CLI commands, flags, validation, or vocabulary that MCP handlers also expose | [cli-mcp-surface-sync.md](lessons/agents/cli-mcp-surface-sync.md)                              |
```

Leave the table header and all other rows intact.

### Step 3: Verify no stale imports remain

Run `grep -r 'github.com/liza-mas/liza/internal/mcp' --include='*.go'` from the project
root and confirm zero matches (all files containing this import were deleted).

### Verification

After all changes:
- `test ! -d cmd/liza-mcp` confirms directory deleted
- `test ! -d internal/mcp` confirms directory deleted
- `test ! -f lessons/agents/cli-mcp-surface-sync.md` confirms file deleted
- `grep 'cli-mcp-surface-sync' GUARDRAILS.md` returns empty (no dangling reference)
- `grep -r 'github.com/liza-mas/liza/internal/mcp' --include='*.go'` returns empty
- `go build ./...` succeeds
- `go test ./...` passes

### TDD justification

No new tests required: this is a pure deletion task. All tests within the deleted
directories are removed with the code. No behavior changes in remaining code — the
sibling tasks already severed all references to the deleted packages.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | `cmd/liza-mcp/` directory does not exist | Task done_when / Arch plan Task 2 | CP1 | Covered |
| 2 | `internal/mcp/` directory does not exist | Task done_when / Arch plan Task 2 | CP1 | Covered |
| 3 | `lessons/agents/cli-mcp-surface-sync.md` does not exist | Task done_when / Arch plan Task 2 | CP1 | Covered |
| 4 | `GUARDRAILS.md` does not reference `cli-mcp-surface-sync.md` | Task done_when / Arch plan Task 2 | CP1 | Covered |
| 5 | `go build ./...` succeeds | Task done_when | CP1 | Covered |
| 6 | `go test ./...` passes | Task done_when | CP1 | Covered |
| 7 | No Go source file outside deleted dirs imports `internal/mcp` or `internal/mcp/protocol` | Task done_when | CP1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: pure code deletion, no new runtime behavior | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: GUARDRAILS.md trigger row removal is in-scope; other doc updates are Phase 4 scope (architecture-4a tasks) | N/A |
