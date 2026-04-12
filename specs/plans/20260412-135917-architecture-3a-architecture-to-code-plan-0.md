# Code Plan: Remove MCP Bootstrap Integration Points (IP-1 through IP-6)

**Task:** architecture-3a-architecture-to-code-plan-0
**Architecture ref:** specs/arch-plan/20260412-112517-architecture-3a.md
**Date:** 2026-04-12
**Scope:** Remove 6 code-side MCP bootstrap integration points from non-MCP source files. Delete embedded mcp.json, remove WriteMCPSettings/mergeMCPSettings from embedded.go, remove WriteMCPSettings call from init.go, remove .mcp.json from wt_create.go copy list, remove MCP args from buildCodexArgs in supervisor.go, clean MCP entries from claude-settings.json. Update affected tests.

## Analysis

The 6 integration points (IP-1 through IP-6) span 4 packages and touch 6 source files plus 3 test files. No file is shared between more than one coding task, enabling full parallelism.

### File-to-Integration-Point Mapping

| IP | File | Change | Package |
|----|------|--------|---------|
| IP-1 | `internal/embedded/mcp.json` | Delete | embedded |
| IP-1 | `internal/embedded/embedded.go` | Remove embed, var, 2 functions | embedded |
| IP-2 | `internal/commands/init.go` | Remove WriteMCPSettings call | commands |
| IP-3 | `internal/ops/wt_create.go` | Remove `.mcp.json` from copy list | ops |
| IP-4 | `internal/agent/supervisor.go` | Remove MCP args from buildCodexArgs | agent |
| IP-5 | `internal/embedded/claude-settings.json` | Remove enabledMcpjsonServers | embedded |
| IP-6 | `internal/embedded/claude-settings.json` | Remove 26 mcp__liza__* permissions | embedded |

### Dependency Analysis

- IP-1 (embed directive) requires mcp.json to exist at compile time. Removing the embed directive and deleting the file must happen atomically in the same task.
- IP-2 (init.go) calls `embedded.WriteMCPSettings()`. Removing the function (IP-1) and the call (IP-2) must happen in the same task to avoid compilation failure.
- IP-1 helper `mergeMCPSettings()` is only called by `WriteMCPSettings()`. Removing both together is required.
- IP-5 and IP-6 (claude-settings.json) affect test assertions in `embedded_test.go` and `init_test.go` that check for `mcp__liza__` permissions. These tests must be updated in the same task.
- IP-3 (wt_create.go) and IP-4 (supervisor.go) are fully independent of all other IPs.

### Import Safety

Removing `WriteMCPSettings`, `mergeMCPSettings`, and `mcpSettingsContent` from `embedded.go` does not affect any imports:
- `embed` is still used by `contractsFS`, `skillsFS`, `claudeSettingsContent`, and 6 other embed directives.
- `encoding/json`, `fmt`, `os`, `path/filepath`, `maps` are all used by remaining functions.
- `init.go` still imports `embedded` for `WriteClaudeSettings`, `WriteSupportDoc`, `WriteGuardrails`, `WriteConsoleScript`, `PipelineConfigContent`.

### Parameter Safety (supervisor.go)

After removing MCP args, `buildCodexArgs`'s `projectRoot` parameter becomes unused. Go does not error on unused function parameters (only unused local variables), so this is safe. Removing the parameter would change the signature and ripple to callers and tests — out of scope per the architecture plan.

## Coding Tasks

### Task 1 (CP-1): Remove embedded MCP config and WriteMCPSettings from init path

**Intent:** Remove the MCP config embed, `WriteMCPSettings`/`mergeMCPSettings` functions, and the init.go call that writes `.mcp.json`. Covers IP-1 and IP-2.

**Changes:**

1. Delete `internal/embedded/mcp.json` (8-line file defining the `liza` MCP server entry).
2. In `internal/embedded/embedded.go`:
   - Remove line 37: `//go:embed "mcp.json"`
   - Remove line 38: `var mcpSettingsContent []byte`
   - Remove lines 529-573: `WriteMCPSettings()` function (45 lines)
   - Remove lines 662-687: `mergeMCPSettings()` function (26 lines)
3. In `internal/commands/init.go`:
   - Remove lines 560-565: comment block and `WriteMCPSettings` call with error handling (6 lines)

**Scope:** Modify `internal/embedded/embedded.go`, `internal/commands/init.go`. Delete `internal/embedded/mcp.json`.

**Done when:** `go build ./cmd/liza` succeeds. `go test ./internal/embedded/... ./internal/commands/...` passes. `internal/embedded/mcp.json` does not exist. `grep -c 'WriteMCPSettings\|mergeMCPSettings\|mcpSettingsContent' internal/embedded/embedded.go` returns 0. `grep -c 'WriteMCPSettings' internal/commands/init.go` returns 0.

**Spec ref:** Architecture plan IP-1 and IP-2 sections.

### Task 2 (CP-2): Remove MCP entries from claude-settings.json and update permission tests

**Intent:** Remove all Liza MCP permission entries and the `enabledMcpjsonServers` line from the embedded settings, then update tests that assert those entries exist. Covers IP-5 and IP-6.

**Changes:**

1. In `internal/embedded/claude-settings.json`:
   - Remove line 36: `"enabledMcpjsonServers": ["liza"],`
   - Remove lines 44-69: all 26 `mcp__liza__*` permission entries (from `"mcp__liza__liza_get"` through `"mcp__liza__liza_delete_agent"`)
2. In `internal/embedded/embedded_test.go`, function `TestWriteClaudeSettings_NewFile` (lines 818-833):
   - Replace the `mcp__liza__` assertion block with a `Bash(liza:*)` assertion — verify the CLI permission exists instead of MCP tool permissions. The new assertion should search the allow array for the string `"Bash(liza:*)"` and fail if not found.
3. In `internal/commands/init_test.go`, function `TestInitCommand_WritesClaudeSettings` (lines 900-912):
   - Replace the `mcp__liza__` assertion block with a `Bash(liza:*)` assertion — same logic as the embedded_test.go change.

**Scope:** Modify `internal/embedded/claude-settings.json`, `internal/embedded/embedded_test.go`, `internal/commands/init_test.go`.

**Done when:** `go build ./cmd/liza` succeeds. `go test ./internal/embedded/... ./internal/commands/...` passes. `grep -c 'mcp__liza__' internal/embedded/claude-settings.json` returns 0. `grep -c 'enabledMcpjsonServers' internal/embedded/claude-settings.json` returns 0. `grep -c 'mcp__liza__' internal/embedded/embedded_test.go` returns 0. `grep -c 'mcp__liza__' internal/commands/init_test.go` returns 0.

**Spec ref:** Architecture plan IP-5 and IP-6 sections.

### Task 3 (CP-3): Remove .mcp.json from worktree provisioning

**Intent:** Remove `.mcp.json` from the worktree config copy list and update the function documentation. Covers IP-3.

**Changes:**

1. In `internal/ops/wt_create.go`:
   - Remove `".mcp.json",` from the `files` slice in `ProvisionClaudeConfig` (line 128).
   - Update function doc comment (lines 119-122): replace "have MCP access and correct settings" with "have correct settings" (remove MCP reference).
   - Update comment at line 105: replace "so agents in worktrees have MCP access" with "so agents in worktrees have correct settings".
2. In `internal/commands/wt_create_test.go`, function `TestWtCreateCommand_ProvisionClaudeConfig` (starting at line 419):
   - Remove `mcpContent` variable declaration (line 427).
   - Remove the `os.WriteFile` call that creates `.mcp.json` in the test fixture (lines 432-434).
   - Remove the `{".mcp.json", mcpContent, 0644}` entry from the verification table (line 519).
3. In `internal/commands/wt_create_test.go`, function `TestWtCreateCommand_ProvisionClaudeConfig_NoFiles` (starting at line 544):
   - Remove `".mcp.json"` from the absent-file assertion list (line 618). The assertion should only check for `".claude"`.

**Scope:** Modify `internal/ops/wt_create.go`, `internal/commands/wt_create_test.go`.

**Done when:** `go build ./cmd/liza` succeeds. `go test ./internal/ops/... ./internal/commands/...` passes. `grep -c '\.mcp\.json' internal/ops/wt_create.go` returns 0. The `ProvisionClaudeConfig` function doc in `wt_create.go` does not contain "MCP". `grep -c '\.mcp\.json' internal/commands/wt_create_test.go` returns 0.

**Spec ref:** Architecture plan IP-3 section.

### Task 4 (CP-4): Remove MCP args from buildCodexArgs in supervisor.go

**Intent:** Remove MCP server configuration arguments from the Codex launch function. Covers IP-4.

**Changes:**

1. In `internal/agent/supervisor.go`, function `buildCodexArgs` (lines 371-391):
   - Remove lines 372-374: the `args := []string{...}` initializer containing the two `-c mcp_servers.liza.*` args. Replace with `var args []string`.
   - Remove lines 381-385: the comment block about `destructiveHint` and MCP elicitation prompts (no longer relevant without MCP).
   - Keep: `exec` mode args, `--full-auto` flag, `--json` flag.

**Note:** The `projectRoot` parameter becomes unused after this change but is intentionally retained — removing it would change the function signature, affecting callers and tests, which is out of scope.

**Scope:** Modify `internal/agent/supervisor.go`.

**Done when:** `go build ./cmd/liza` succeeds. `go test ./internal/agent/...` passes. `grep -c 'mcp_servers\.liza' internal/agent/supervisor.go` returns 0. `grep -c 'destructiveHint' internal/agent/supervisor.go` returns 0.

**Spec ref:** Architecture plan IP-4 section.

## Task Dependency Graph

```
CP-1 ─┐
CP-2 ─┤  (all independent, no shared files)
CP-3 ─┤
CP-4 ─┘
```

All four tasks can execute in parallel. No task depends on another.

## Shared-File Audit

| File | CP-1 | CP-2 | CP-3 | CP-4 |
|------|------|------|------|------|
| `internal/embedded/mcp.json` | Delete | — | — | — |
| `internal/embedded/embedded.go` | Modify | — | — | — |
| `internal/commands/init.go` | Modify | — | — | — |
| `internal/embedded/claude-settings.json` | — | Modify | — | — |
| `internal/embedded/embedded_test.go` | — | Modify | — | — |
| `internal/commands/init_test.go` | — | Modify | — | — |
| `internal/ops/wt_create.go` | — | — | Modify | — |
| `internal/commands/wt_create_test.go` | — | — | Modify | — |
| `internal/agent/supervisor.go` | — | — | — | Modify |

No file is touched by more than one task. No dependency links needed.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Delete `internal/embedded/mcp.json` | Arch plan IP-1 | CP-1 | Covered |
| 2 | Remove `//go:embed "mcp.json"` and `var mcpSettingsContent` from embedded.go | Arch plan IP-1 | CP-1 | Covered |
| 3 | Remove `WriteMCPSettings()` function from embedded.go | Arch plan IP-1 | CP-1 | Covered |
| 4 | Remove `mergeMCPSettings()` function from embedded.go | Arch plan IP-1 | CP-1 | Covered |
| 5 | Remove `WriteMCPSettings` call from init.go (lines 560-565) | Arch plan IP-2 | CP-1 | Covered |
| 6 | Remove `"enabledMcpjsonServers": ["liza"]` from claude-settings.json | Arch plan IP-5 | CP-2 | Covered |
| 7 | Remove all 26 `mcp__liza__*` permission entries from claude-settings.json | Arch plan IP-6 | CP-2 | Covered |
| 8 | Remove `".mcp.json"` from ProvisionClaudeConfig files list in wt_create.go | Arch plan IP-3 | CP-3 | Covered |
| 9 | Update ProvisionClaudeConfig comment to remove MCP reference | Arch plan IP-3 | CP-3 | Covered |
| 10 | Remove MCP server config args from buildCodexArgs in supervisor.go | Arch plan IP-4 | CP-4 | Covered |
| 11 | Remove destructiveHint comment from supervisor.go | Arch plan IP-4 | CP-4 | Covered |
| 12 | Update embedded_test.go to not assert mcp__liza__ permissions | Done-when criteria | CP-2 | Covered |
| 13 | Update init_test.go to not assert mcp__liza__ permissions | Done-when criteria | CP-2 | Covered |
| 14 | Update wt_create_test.go to remove .mcp.json references | Done-when criteria | CP-3 | Covered |
| 15 | `go build ./cmd/liza` succeeds | Done-when criteria | CP-1, CP-2, CP-3, CP-4 | Covered |
| 16 | `go test` passes for all 4 packages | Done-when criteria | CP-1, CP-2, CP-3, CP-4 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: pure deletion/removal with no new behavior; existing tests verify remaining functionality | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: documentation updates are Phase 4 scope (architecture-4a tasks), explicitly out of scope per architecture plan Phase Boundary section | N/A |
