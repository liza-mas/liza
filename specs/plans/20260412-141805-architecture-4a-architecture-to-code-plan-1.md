# Code Plan: Update Contracts and Embedded Support Doc for CLI-Only

**Task:** architecture-4a-architecture-to-code-plan-1
**Date:** 2026-04-12
**Architecture ref:** specs/arch-plan/20260412-124731-architecture-4a.md (Task 1)
**Spec ref:** specs/goals/20260412-cli-native-access-control.md

## Context

After MCP server removal (Phase 3), agent contracts and the embedded support doc still reference "Liza MCP tools" and MCP-specific configuration. This plan covers replacing those references with CLI-only language across three files.

This is a documentation-only change with no code compilation or test impact.

## CP-1: Update contracts and embedded support doc for CLI-only

**Description:** Replace Liza MCP references with CLI equivalents in `contracts/MULTI_AGENT_MODE.md` (Blackboard Protocol and Context Exhaustion Handoff sections), `contracts/contract-activation.md` (remove `[mcp_servers.liza]` TOML block, update settings descriptions), and `internal/embedded/support.md` (replace MCP tools reference with CLI commands).

**Scope:** Modify `contracts/MULTI_AGENT_MODE.md`, `contracts/contract-activation.md`, `internal/embedded/support.md`.

### Changes

#### `contracts/MULTI_AGENT_MODE.md`

**Blackboard Protocol section (line 90):**
- Before: `All state transitions MUST go through Liza MCP tools (\`liza_submit_for_review\`, \`liza_claim_task\`, etc.).`
- After: `All state transitions MUST go through Liza CLI commands (\`liza submit-for-review\`, \`liza claim-task\`, etc.).`
- Before: `If an MCP tool fails repeatedly, set the task BLOCKED via \`liza_mark_blocked\``
- After: `If a CLI command fails repeatedly, set the task BLOCKED via \`liza mark-blocked\``

**Context Exhaustion Handoff section (line 113):**
- Before: `Use \`liza_handoff\` MCP tool with summary + next_action`
- After: `Run \`liza handoff\` CLI command with summary + next_action`

#### `contracts/contract-activation.md`

**Settings table (line 13):**
- Before: `Liza MCP tools, skills, git/build commands`
- After: `Liza CLI permissions, skills, git/build commands`

**Codex section (lines 50-54):** Remove the `[mcp_servers.liza]` TOML block:
```toml
[mcp_servers.liza]
type = "stdio"
command = "liza-mcp"
args = ["--project-root", "/home/<USER>/Workspace/liza"]
```
Add a comment noting Codex agents access Liza via CLI commands through Bash.

#### `internal/embedded/support.md`

**Line 93:**
- Before: `Prefer \`liza_*\` MCP tools and CLI commands for all state mutations.`
- After: `Use \`liza\` CLI commands for all state mutations.`

### Verification

```bash
# contracts/MULTI_AGENT_MODE.md: no "Liza MCP tools" or "MCP tool" in Liza-specific context
grep -n 'Liza MCP tools\|MCP tool' contracts/MULTI_AGENT_MODE.md
# Expected: empty (no matches)

# Line 90 contains "Liza CLI commands"
grep -n 'Liza CLI commands' contracts/MULTI_AGENT_MODE.md
# Expected: line 90 match

# Line 113 contains "liza handoff" CLI command
grep -n 'liza handoff.*CLI command' contracts/MULTI_AGENT_MODE.md
# Expected: line ~113 match

# contracts/contract-activation.md: no mcp_servers.liza or liza-mcp
grep -n 'mcp_servers.liza\|liza-mcp' contracts/contract-activation.md
# Expected: empty (no matches)

# internal/embedded/support.md: no "MCP tools"
grep -n 'MCP tools' internal/embedded/support.md
# Expected: empty (no matches)
```

**TDD not required:** Documentation-only text changes, no code compilation or behavioral change.

**Done when:** `contracts/MULTI_AGENT_MODE.md` contains no `Liza MCP tools` or `MCP tool` in Liza-specific context (line 90 says `Liza CLI commands`, line ~113 says `liza handoff CLI command`). `contracts/contract-activation.md` contains no `mcp_servers.liza` or `liza-mcp`. `internal/embedded/support.md` contains no `MCP tools`. All three files are valid markdown.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Replace "Liza MCP tools" with "Liza CLI commands" in MULTI_AGENT_MODE.md Blackboard Protocol (line 90) | Arch plan, Contract Update Details, MULTI_AGENT_MODE.md section | CP-1 | Covered |
| 2 | Replace "MCP tool" with "CLI command" in MULTI_AGENT_MODE.md Context Exhaustion Handoff (line 113) | Arch plan, Contract Update Details, MULTI_AGENT_MODE.md section | CP-1 | Covered |
| 3 | Remove `[mcp_servers.liza]` TOML block from contract-activation.md | Arch plan, Contract Update Details, contract-activation.md section | CP-1 | Covered |
| 4 | Update "Liza MCP tools" to "Liza CLI permissions" in contract-activation.md settings table | Arch plan, Contract Update Details, contract-activation.md section | CP-1 | Covered |
| 5 | Replace "Prefer `liza_*` MCP tools and CLI commands" with "Use `liza` CLI commands" in support.md | Arch plan, Category 6: Embedded Support | CP-1 | Covered |
| 6 | All three files are valid markdown | Task done_when | CP-1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: documentation-only text changes, no behavioral change | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: the task itself IS the documentation update | N/A |
