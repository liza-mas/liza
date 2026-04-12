# Code Plan: Remove Liza MCP References from Documentation, Lessons, and Project Root

**Task:** architecture-4a-architecture-to-code-plan-2
**Date:** 2026-04-12
**Architecture ref:** specs/arch-plan/20260412-124731-architecture-4a.md (Task 2)
**Goal spec:** specs/goals/20260412-cli-native-access-control.md (Section 3 + MVP Scope docs bullet)

## Context

After Phases 1-3 complete (CLI RBAC, `--json` output, MCP server deletion) and Phase 4a CP-0/CP-1 rewrite prompt templates and contracts, documentation and project root files still reference the deleted MCP server. This task removes all stale Liza MCP references from markdown documentation, lessons, `GUARDRAILS.md`, and `REPOSITORY.md`.

### Key Distinction

Only **Liza MCP** references are removed (`liza-mcp`, `mcp__liza__*`, `.mcp.json` Liza server entry, `internal/embedded/mcp.json`). External MCP servers (JetBrains, filesystem, perplexity, etc.) are unrelated and remain untouched.

### Dependency Note

Sibling tasks CP-0 (prompt templates) and CP-1 (contracts + embedded support doc) handle `.tmpl`, `.go`, and contract files. This task handles `.md` documentation, lessons, and project root files only. No shared files between tasks — all three can execute in parallel.

## CP-1: Remove Liza MCP references from documentation, lessons, GUARDRAILS.md, and REPOSITORY.md

**Intent:** Remove all stale Liza MCP references from the 7 markdown files in scope, ensuring no active documentation references the deleted MCP server.

### File-by-File Changes

#### 1. `docs/CONFIGURATION.md`

**Remove entirely:**
- "## MCP Server Setup" section (lines 5-57): intro, `.mcp.json` config, `.claude/settings.json` MCP description, "### Two-Layer Architecture", "### Troubleshooting MCP"
- "### MCP Tools Reference" section (lines 63-87): `liza_await_verdict` and `liza_await_resubmission` tool docs
- "### CLI vs MCP" section (lines 89-97): comparison table

**Replace with** a brief "## Project Settings" section (between the h1 heading and "## Configuration Matrix"):
- State that `liza init` creates `.claude/settings.json` with pre-approved permissions
- Mention the two-layer architecture (project vs global) without MCP-specific content
- Reference `contracts/contract-activation.md` for full setup guide
- No `.mcp.json`, no `mcp__liza__*` tool names, no `enabledMcpjsonServers`

**What remains unchanged:** Everything from "## Configuration Matrix" (line 99) onward.

#### 2. `docs/USAGE_MULTI_AGENTS.md`

**Prerequisites (line 64):**
- Before: `` - `liza` and `liza-mcp` Go binaries in PATH ``
- After: `` - `liza` Go binary in PATH ``

**Codex note (line 65):**
- Before: `` - When using `--cli codex`: Codex CLI > 0.118.0 (0.117.0–0.118.0 have a [regression](...) that cancels MCP tool calls in exec mode; 0.116.0 also works) ``
- After: Remove this line entirely (MCP tool call regression is irrelevant without MCP)

**`liza init` creates list (lines 132-134):**
- Remove `.mcp.json` line: `` - `.mcp.json` — MCP server configuration (tells Claude Code how to start liza-mcp) ``
- Update `.claude/settings.json` description: remove "Liza MCP tools" → "skills, git/build commands"

**Remove entirely:**
- "### Configuring Claude Code (MCP)" section (lines 394-452): MCP server config, `.mcp.json` example, `mcp__liza__*` permission list, CLI vs MCP explanation

**Interactive diagnosis (line 505):**
- Before: "It picks up Liza's MCP tools from `.mcp.json` and can read `.liza/state.yaml`..."
- After: "It can read `.liza/state.yaml`, agent logs, and prompts — everything needed to diagnose issues interactively."

#### 3. `docs/DEMO.md`

**Prerequisites (line 14):**
- Before: `` - `liza` and `liza-mcp` Go binaries in PATH (see `make install`) ``
- After: `` - `liza` Go binary in PATH (see `make install`) ``

**`liza init` creates list (line 134):**
- Remove: `` - `.mcp.json` — MCP server configuration (tells Claude Code how to start liza-mcp) ``

#### 4. `docs/TESTING.md`

**Race Detection section (line 37):**
- Remove: `` go test -race ./internal/mcp/      # MCP server (concurrent handlers) ``

**Test Organization directory listing (lines 66-69):**
- Remove the `mcp/` block:
  ```
  ├── mcp/
  │   ├── server.go / server_test.go
  │   ├── handlers.go / handlers_test.go
  │   └── concurrency_test.go
  ```

#### 5. `REPOSITORY.md`

**Directory structure (line 10):**
- Before: `` ├── cmd/                    # Go CLI entry points (liza, liza-mcp) ``
- After: `` ├── cmd/                    # Go CLI entry point (liza) ``

**Build requirement (line 129):**
- Before: `` Config files (`claude-settings.json`, `mcp.json`) and hooks are mastered directly in `internal/embedded/`. ``
- After: `` Config files (`claude-settings.json`) and hooks are mastered directly in `internal/embedded/`. ``

#### 6. `GUARDRAILS.md`

**G2.1 Lessons table:**

Remove the `cli-mcp-surface-sync.md` trigger row (line 49):
```
| Modifying CLI commands, flags, validation, or vocabulary that MCP handlers also expose | [cli-mcp-surface-sync.md](lessons/agents/cli-mcp-surface-sync.md) |
```
Rationale: `lessons/agents/cli-mcp-surface-sync.md` is deleted by Phase 3 (architecture-3a-architecture-to-code-plan-2). The GUARDRAILS.md row is a broken link.

Update `settings-master-not-derived.md` trigger (line 50):
- Before: `` | Modifying `internal/embedded/claude-settings.json`, `internal/embedded/mcp.json`, `internal/embedded/hooks/`, or any file with master/derived copies | ``
- After: `` | Modifying `internal/embedded/claude-settings.json`, `internal/embedded/hooks/`, or any file with master/derived copies | ``

#### 7. `lessons/agents/settings-master-not-derived.md`

**Frontmatter keywords (line 7):**
- Before: `keywords: [claude-settings.json, .claude/settings.json, embedded, liza init, permissions, MCP tools, hooks]`
- After: `keywords: [claude-settings.json, .claude/settings.json, embedded, liza init, permissions, hooks]`

**Context section (line 15):**
- Before: `` Similarly: `internal/embedded/mcp.json` → `.mcp.json`, and `internal/embedded/hooks/enforce-init.sh` → `.claude/hooks/enforce-init.sh`. ``
- After: `` Similarly: `internal/embedded/hooks/enforce-init.sh` → `.claude/hooks/enforce-init.sh`. ``

**References section (line 31):**
- Remove: `` - `internal/embedded/mcp.json` — master ``

### Verification

After all edits, the coder runs these checks:

1. `grep -n 'liza-mcp' docs/CONFIGURATION.md docs/USAGE_MULTI_AGENTS.md docs/DEMO.md docs/TESTING.md REPOSITORY.md GUARDRAILS.md lessons/agents/settings-master-not-derived.md` → empty
2. `grep -n 'mcp__liza__' docs/CONFIGURATION.md docs/USAGE_MULTI_AGENTS.md docs/DEMO.md docs/TESTING.md REPOSITORY.md GUARDRAILS.md lessons/agents/settings-master-not-derived.md` → empty
3. `grep -n '\.mcp\.json' docs/CONFIGURATION.md docs/USAGE_MULTI_AGENTS.md docs/DEMO.md` → empty (no Liza-specific .mcp.json references)
4. `grep -n 'internal/mcp/' docs/TESTING.md` → empty
5. `grep -n 'mcp\.json' GUARDRAILS.md lessons/agents/settings-master-not-derived.md` → empty
6. Stale-reference scan on `*.md` files in this task's scope:
   ```bash
   grep -rn 'liza-mcp\|mcp__liza__\|\.mcp\.json.*liza\|WriteMCP\|mergeMCP\|mcpSettings' \
     --include='*.md' \
     docs/CONFIGURATION.md docs/USAGE_MULTI_AGENTS.md docs/DEMO.md docs/TESTING.md \
     REPOSITORY.md GUARDRAILS.md lessons/agents/settings-master-not-derived.md
   ```
   → empty

**Note:** The full global stale-reference scan across `*.md *.tmpl *.go` (from the architecture plan) is only valid after all Phase 4a tasks (CP-0, CP-1, CP-2) merge to the integration branch. This task's coder verifies the `*.md` subset; `*.tmpl` and `*.go` are owned by sibling tasks CP-0 and CP-1.

### Scope

Modify: `docs/CONFIGURATION.md`, `docs/USAGE_MULTI_AGENTS.md`, `docs/DEMO.md`, `docs/TESTING.md`, `REPOSITORY.md`, `GUARDRAILS.md`, `lessons/agents/settings-master-not-derived.md`.

No Go code, no tests, no compilation. TDD not required: cosmetic-only documentation change.

### Dependencies

None. This task can execute in parallel with CP-0 and CP-1 (no shared files).

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Remove MCP sections from docs/CONFIGURATION.md (MCP Server Setup, Troubleshooting MCP, MCP Tools Reference, CLI vs MCP) | Arch plan § "Documentation Update Details" / "docs/CONFIGURATION.md" | CP-1 | Covered |
| 2 | Remove liza-mcp and mcp__liza__ from docs/USAGE_MULTI_AGENTS.md | Arch plan § "Documentation Update Details" / "docs/USAGE_MULTI_AGENTS.md" | CP-1 | Covered |
| 3 | Remove liza-mcp from docs/DEMO.md | Arch plan § "Documentation Update Details" / "docs/DEMO.md" | CP-1 | Covered |
| 4 | Remove internal/mcp/ test reference from docs/TESTING.md | Arch plan § "Documentation Update Details" / "docs/TESTING.md" | CP-1 | Covered |
| 5 | Remove liza-mcp from REPOSITORY.md directory tree | Arch plan § "Documentation Update Details" / "REPOSITORY.md" | CP-1 | Covered |
| 6 | Remove mcp.json from REPOSITORY.md build requirement | Arch plan § "Documentation Update Details" / "REPOSITORY.md" | CP-1 | Covered |
| 7 | Remove cli-mcp-surface-sync.md trigger row from GUARDRAILS.md | Arch plan § "Contract Update Details" / "GUARDRAILS.md" | CP-1 | Covered |
| 8 | Remove mcp.json from settings-master-not-derived trigger in GUARDRAILS.md | Arch plan § "Contract Update Details" / "GUARDRAILS.md" + Task done_when | CP-1 | Covered |
| 9 | Remove mcp.json references from lessons/agents/settings-master-not-derived.md | Arch plan § "Documentation Update Details" / "lessons/agents/settings-master-not-derived.md" | CP-1 | Covered |
| 10 | Stale-reference scan on *.md files returns empty | Task done_when (global scan) | CP-1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: documentation-only change, no behavioral code, no testable behavior | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | CP-1 (this task IS the documentation update) | Covered |
