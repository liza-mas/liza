# Code Plan: Remove MCP from Makefile and Supervisor Codex Args

**Task:** architecture-3-architecture-to-code-plan-3
**Arch ref:** specs/arch-plan/20260412-120128-architecture-3.md (Scope 3, Section 2.6-2.7)
**Spec ref:** specs/goals/20260412-cli-native-access-control.md (Section 3)

## Overview

Two independent intents: (1) Remove all `liza-mcp` / `MCP_BINARY_NAME` references from the Makefile so the build system produces only the `liza` binary. (2) Remove MCP server config args from `buildCodexArgs()` in `supervisor.go` so Codex agents launch without MCP server configuration, and update MCP-referencing comments. Tests are colocated with the supervisor change.

## Task 1: Remove MCP binary targets from Makefile

**Intent:** Build system no longer references or builds `liza-mcp`.

**File:** `Makefile`

**Changes (by target):**

1. **Line 5** — Delete `MCP_BINARY_NAME=liza-mcp` variable declaration.
2. **`build` target (lines 31-32)** — Delete the two MCP build echo+build lines. Only `liza` is built.
3. **`clean` target (lines 53-54)** — Delete `rm -f $(MCP_BINARY_NAME)` and `rm -f $(MCP_BINARY_NAME)-*`.
4. **`install` target (line 67)** — Delete the `$(SUDO) install ... $(MCP_BINARY_NAME)` line. Update warning at line 69: remove `/usr/local/bin/liza-mcp` from the rm suggestion (becomes `'sudo rm /usr/local/bin/liza'`).
5. **`build-all` target (lines 113-116)** — Delete all four MCP cross-compile lines.
6. **`release` target (lines 128-133)** — Delete the `@# Build liza-mcp for all platforms` comment and five MCP build lines.
7. **`package` target (lines 148-152)** — Remove `$(MCP_BINARY_NAME)-*` from each tar/zip command so packages contain only the `liza` binary.
8. **`help` target** — Line 159: `"Build liza and liza-mcp binaries"` → `"Build liza binary"`. Line 164: `"Install liza and liza-mcp binaries"` → `"Install liza binary"`. Line 170: `"Build both binaries for multiple platforms"` → `"Build liza for multiple platforms"`.

**Done when:** `grep -cE 'liza-mcp|MCP_BINARY_NAME' Makefile` returns 0. `make build` (in a worktree with `cmd/liza-mcp/` already deleted by Scope 0) builds only the `liza` binary without errors.

**Scope:** Modify `Makefile` only. No other files.

## Task 2: Remove MCP server config from buildCodexArgs and update supervisor comments and tests

**Intent:** Codex agents launch without MCP server configuration args; MCP-referencing comments are updated; test expectations verify no MCP args.

**Files:** `internal/agent/supervisor.go`, `internal/agent/supervisor_test.go`

**Changes in `internal/agent/supervisor.go`:**

1. **Lines 372-375** — Replace the `args` initialization that includes two `-c mcp_servers.*` entries with `var args []string`. The function signature and return type are unchanged.

   Before:
   ```go
   args := []string{
       "-c", fmt.Sprintf("mcp_servers.liza.command=%q", "liza-mcp"),
       "-c", fmt.Sprintf("mcp_servers.liza.args=[%q,%q]", "--project-root", projectRoot),
   }
   ```
   After:
   ```go
   var args []string
   ```

2. **Lines 381-385** — Replace the MCP-specific comment block explaining `destructiveHint` with a simpler comment about `--full-auto`:

   Before:
   ```go
   // Codex exec mode auto-cancels MCP elicitation prompts for tools with
   // destructiveHint=true (the default). Liza MCP tools declare
   // destructiveHint=false, so --full-auto lets them through while keeping
   // the OS-enforced sandbox.
   // Requires Codex >0.118.0: https://github.com/openai/codex/issues/16685
   ```
   After:
   ```go
   // --full-auto enables Codex's auto-approval mode within the OS-enforced sandbox.
   ```

3. **Line 471** — Update working directory comment:

   Before: `// Set working directory to project root so claude can find .mcp.json and .claude/settings.json`
   After: `// Set working directory to project root so claude can find .claude/settings.json`

4. **Line 483** — Update LIZA_AGENT_ID comment:

   Before: `// Ensure LIZA_AGENT_ID is available to child processes (hooks, MCP servers).`
   After: `// Ensure LIZA_AGENT_ID is available to child processes (hooks).`

**Changes in `internal/agent/supervisor_test.go`:**

5. **`TestBuildCodexArgs` (lines 580-611)** — Add negative assertions to both subtests verifying that no MCP server config args appear in the result. Specifically, add to each subtest:
   ```go
   for _, a := range args {
       if strings.Contains(a, "mcp_servers") {
           t.Fatalf("args = %v, did not expect mcp_servers config", args)
       }
   }
   ```
   This requires adding `"strings"` to the test file imports if not already present.

**Done when:** `buildCodexArgs("/tmp/project", "test", true, "")` returns args containing no `mcp_servers` substring. `grep -c 'mcp_servers\|MCP\|\.mcp\.json' internal/agent/supervisor.go` returns 0. `go test -run TestBuildCodexArgs ./internal/agent/...` passes with the new negative assertions.

**Scope:** Modify `internal/agent/supervisor.go` and `internal/agent/supervisor_test.go` only. No other files.

## Dependencies

- Task 1 and Task 2 touch disjoint files — no shared-file dependency. They can run in parallel.
- Both depend on architecture-3-architecture-to-code-plan-0 (Scope 0: MCP code deletion) being merged, which is already MERGED.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Remove `MCP_BINARY_NAME=liza-mcp` variable from Makefile | Arch Scope 3 / Section 2.7 | Task 1 | Covered |
| 2 | Remove MCP build line from `build` target | Arch Scope 3 / Section 2.7 | Task 1 | Covered |
| 3 | Remove MCP build lines from `build-all` target | Arch Scope 3 / Section 2.7 | Task 1 | Covered |
| 4 | Remove MCP build lines from `release` target | Arch Scope 3 / Section 2.7 | Task 1 | Covered |
| 5 | Remove MCP from `package` target tarballs | Arch Scope 3 / Section 2.7 | Task 1 | Covered |
| 6 | Remove `liza-mcp` from install warnings | Arch Scope 3 / Section 2.7 | Task 1 | Covered |
| 7 | Remove MCP references from `help` target | Arch Scope 3 / Section 2.7 | Task 1 | Covered |
| 8 | Remove MCP config args from `buildCodexArgs()` | Arch Scope 3 / Section 2.6, lines 373-374 | Task 2 | Covered |
| 9 | Update `destructiveHint` / MCP elicitation comment | Arch Scope 3 / Section 2.6, lines 381-385 | Task 2 | Covered |
| 10 | Update working directory comment (remove .mcp.json) | Arch Scope 3 / Section 2.6, line 471 | Task 2 | Covered |
| 11 | Update LIZA_AGENT_ID comment (remove MCP servers) | Arch Scope 3 / Section 2.6, line 483 | Task 2 | Covered |
| 12 | Update `buildCodexArgs` test expectations | Arch Scope 3 / Section 4.2 | Task 2 | Covered |
| 13 | `make build` builds only `liza` binary | Done-when criterion | Task 1 | Covered |
| 14 | `go test ./internal/agent/...` passes | Done-when criterion | Task 2 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: internal refactor removing dead code paths — no new user-visible behavior. Existing `go test ./...` and `make build` verify correctness. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: documentation updates are handled by Phase 4 tasks (architecture-4-architecture-to-code-plan-2). | N/A |
