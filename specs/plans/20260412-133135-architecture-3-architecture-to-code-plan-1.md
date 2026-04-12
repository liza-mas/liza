# Code Plan: Remove Embedded MCP Config and Init Integration

**Task:** architecture-3-architecture-to-code-plan-1
**Architecture:** specs/arch-plan/20260412-120128-architecture-3.md (Section 2.2, 2.3, 2.8)
**Spec:** specs/goals/20260412-cli-native-access-control.md (Section 3, integration points table)

## Overview

Remove the embedded MCP config file, the functions that write/merge it, the init call site, and the now-stale test assertion. This eliminates the `liza init` integration point that wrote `.mcp.json` to the project root.

## Task 1: Remove embedded MCP config, WriteMCPSettings functions, init call site, and stale test assertion

**Intent:** Delete `internal/embedded/mcp.json` and remove all code that reads, writes, or merges MCP settings, plus the init-test assertion for `mcp__liza__*` permissions.

**Changes:**

### 1a. Delete `internal/embedded/mcp.json`

Remove the file entirely. This is the embedded MCP server configuration template.

### 1b. Modify `internal/embedded/embedded.go`

Remove these three code blocks:

1. **Embed directive** (lines 37-38):
   ```go
   //go:embed "mcp.json"
   var mcpSettingsContent []byte
   ```

2. **`WriteMCPSettings` function** (lines 529-573): The entire function that writes `.mcp.json` to the project root, including its doc comment.

3. **`mergeMCPSettings` function** (lines 662-687): The helper that merges Liza MCP settings with existing user settings, including its doc comment.

No import changes needed — all imports (`bufio`, `encoding/json`, `maps`, etc.) remain used by other functions (`WriteClaudeSettings`, `mergeSettings`, `mergePermissions`, `mergeHooks`).

### 1c. Modify `internal/commands/init.go`

Remove the `WriteMCPSettings` call block (lines 560-565):
```go
// Write/merge MCP server configuration to .mcp.json
// This is non-fatal - if it fails, just warn
// Note: This may prompt user for input if settings file exists
if err := embedded.WriteMCPSettings(lizaPaths.ProjectRoot(), stdin); err != nil {
    fmt.Fprintf(os.Stderr, "Warning: failed to write .mcp.json: %v\n", err)
}
```

The `embedded` import remains used by other calls in the same function (`WriteClaudeSettings`, `WriteSupportDoc`, etc.).

### 1d. Modify `internal/commands/init_test.go`

Remove the `mcp__liza__*` permission assertion block (lines 900-912):
```go
// Verify key liza MCP tools are in allow array (explicit tool format)
foundLizaMCP := false
for _, perm := range allow {
    permStr := perm.(string)
    // Check for explicit tool format: mcp__liza__liza_add_tasks
    if strings.HasPrefix(permStr, "mcp__liza__") {
        foundLizaMCP = true
        break
    }
}
if !foundLizaMCP {
    t.Errorf("Expected liza MCP tools in allow array (e.g., mcp__liza__liza_add_tasks)")
}
```

Note: This assertion would still pass after this task (since `claude-settings.json` is not modified here — that's Scope 2). However, removing it now prevents a shared-file conflict with Scope 2 when it removes the `mcp__liza__*` entries from `claude-settings.json`.

**Scope:**
- Delete: `internal/embedded/mcp.json`
- Modify: `internal/embedded/embedded.go` (remove embed directive at lines 37-38, `WriteMCPSettings` at lines 529-573, `mergeMCPSettings` at lines 662-687)
- Modify: `internal/commands/init.go` (remove `WriteMCPSettings` call block at lines 560-565)
- Modify: `internal/commands/init_test.go` (remove `mcp__liza__*` assertion at lines 900-912)

**Done when:** `internal/embedded/mcp.json` does not exist. `embedded.go` has no `mcpSettingsContent` variable and no `WriteMCPSettings` function. `init.go` has no `WriteMCPSettings` call. `go build ./...` succeeds. `go test ./internal/embedded/... ./internal/commands/...` passes. Grep for `mcpSettingsContent`, `WriteMCPSettings`, and `mergeMCPSettings` in `internal/embedded/embedded.go` returns no matches. Grep for `WriteMCPSettings` in `internal/commands/init.go` returns no matches. Grep for `mcp__liza__` in `internal/commands/init_test.go` returns no matches.

**Spec ref:** specs/goals/20260412-cli-native-access-control.md

**TDD note:** No new tests needed. This is a pure deletion — existing tests confirm no regressions. The removed test assertion was testing for MCP permissions that are being removed in the broader initiative.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Delete `internal/embedded/mcp.json` | Arch doc Section 2.2: "DELETE this file entirely" | Task 1 (step 1a) | Covered |
| 2 | Remove `mcpSettingsContent` embed directive from `embedded.go` | Arch doc Section 2.2: "Remove `mcpSettingsContent` embed directive (line 37-38)" | Task 1 (step 1b) | Covered |
| 3 | Remove `WriteMCPSettings()` function from `embedded.go` | Arch doc Section 2.2: "`WriteMCPSettings()` function (lines 529-573)" | Task 1 (step 1b) | Covered |
| 4 | Remove `mergeMCPSettings()` helper from `embedded.go` | Arch doc Section 2.2: "and `mergeMCPSettings()` helper (if it exists in the same file)" | Task 1 (step 1b) | Covered |
| 5 | Remove `WriteMCPSettings` call from `init.go` | Arch doc Section 2.3: "Remove the `WriteMCPSettings` call block at lines 560-565" | Task 1 (step 1c) | Covered |
| 6 | Remove `mcp__liza__*` permission assertion from `init_test.go` | Arch doc Section 2.8: "remove the test assertion must be removed or inverted" | Task 1 (step 1d) | Covered |
| 7 | `go build ./...` succeeds after changes | Arch doc Section 2.2: done_when | Task 1 (done_when) | Covered |
| 8 | `go test ./internal/embedded/... ./internal/commands/...` passes | Arch doc Scope 1: done_when | Task 1 (done_when) | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: pure deletion of internal bootstrap code with no new user-visible behavior | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: documentation updates are handled by Phase 4 sibling tasks (architecture-4-architecture-to-code-plan-2) | N/A |

## Dependencies

Task 1 has no internal dependencies (single task). This task's parent (`architecture-3-architecture-to-code-plan-1`) depends on `architecture-3-architecture-to-code-plan-0` (MCP server package deletion), which is already MERGED.

## Shared File Audit

No shared files across tasks (single task plan). Cross-task shared file analysis:
- `internal/commands/init_test.go`: touched only by this task (Scope 1) among the current sprint siblings. Scope 2 does NOT touch this file.
- `internal/embedded/embedded.go`: touched only by this task. Scope 2 touches `claude-settings.json` (different file).
- `internal/commands/init.go`: touched only by this task.
