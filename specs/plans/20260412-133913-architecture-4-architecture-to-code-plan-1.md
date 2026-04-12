# Code Plan: Update Contracts and Embedded Support Doc for CLI-Only

**Task:** architecture-4-architecture-to-code-plan-1
**Date:** 2026-04-12
**Architecture ref:** specs/arch-plan/20260412-125944-architecture-4.md (Category 2: Contracts and Embedded Support)
**Goal spec:** specs/goals/20260412-cli-native-access-control.md
**Scope:** contracts/MULTI_AGENT_MODE.md, contracts/contract-activation.md, internal/embedded/support.md

## Context

After MCP server removal (Phase 3), contracts and embedded support still reference "Liza MCP tools", `mcp_servers.liza`, and `liza-mcp`. This plan covers replacing those references with CLI-only language. External MCP references (JetBrains, filesystem, perplexity, etc.) are unrelated and remain untouched.

## Task 1: Remove Liza MCP references from contracts and embedded support doc

**Intent:** Replace all Liza-specific MCP language with CLI-only language in three contract/support files.

**tdd_not_required:** All changes are markdown-only (contracts and documentation). No code, no tests.

### File 1: contracts/MULTI_AGENT_MODE.md

**Line 55** (Pre-Execution Checkpoint):
- Before: `write a checkpoint via \`liza_write_checkpoint\``
- After: `write a checkpoint via \`liza write-checkpoint\``
- Rationale: Consistency with CLI hyphenated naming used elsewhere in the file after this update.

**Line 90** (Blackboard Protocol) — full line rewrite:
- Before: `**Do NOT edit state.yaml directly.** All state transitions MUST go through Liza MCP tools (\`liza_submit_for_review\`, \`liza_claim_task\`, etc.). Direct edits bypass invariant checks and can corrupt state irreversibly. If an MCP tool fails repeatedly, set the task BLOCKED via \`liza_mark_blocked\` — never work around the failure by editing state.yaml.`
- After: `**Do NOT edit state.yaml directly.** All state transitions MUST go through Liza CLI commands (\`liza submit-for-review\`, \`liza claim-task\`, etc.). Direct edits bypass invariant checks and can corrupt state irreversibly. If a CLI command fails repeatedly, set the task BLOCKED via \`liza mark-blocked\` — never work around the failure by editing state.yaml.`

**Line 112** (Context Exhaustion Handoff):
- Before: `3. Use \`liza_handoff\` MCP tool with summary + next_action`
- After: `3. Run \`liza handoff\` CLI command with summary + next_action`

### File 2: contracts/contract-activation.md

**Line 13** (Settings table — Project row):
- Before: `Liza MCP tools, skills, git/build commands`
- After: `Liza CLI permissions, skills, git/build commands`

**Lines 50-53** (Codex `[mcp_servers.liza]` TOML block): Remove entirely. The four lines to remove are:
```toml
[mcp_servers.liza]
type = "stdio"
command = "liza-mcp"
args = ["--project-root", "/home/<USER>/Workspace/liza"]
```

After removal, add a comment or note in the Codex section explaining that Codex agents access Liza via `liza` CLI commands through Bash (no MCP server needed). For example, add after the `[mcp_servers.filesystem]` block:

```
# Codex agents access Liza via `liza` CLI commands through Bash — no MCP server needed.
```

**Preserve:** Lines 14 ("Personal MCP tools"), 46 (`[mcp_servers.filesystem]`), 74-76 (Mistral MCP filesystem) — these are external MCP, not Liza MCP.

### File 3: internal/embedded/support.md

**Line 93** (Reading state.yaml section):
- Before: `Prefer \`liza_*\` MCP tools and CLI commands for all state mutations. If a needed operation isn't covered, edit \`.liza/state.yaml\` directly but: write atomically (write to temp file, then rename), use ISO 8601 UTC timestamps (\`YYYY-MM-DDTHH:MM:SSZ\`), and run \`liza validate\` afterward to verify invariants.`
- After: `Use \`liza\` CLI commands for all state mutations. If a needed operation isn't covered, edit \`.liza/state.yaml\` directly but: write atomically (write to temp file, then rename), use ISO 8601 UTC timestamps (\`YYYY-MM-DDTHH:MM:SSZ\`), and run \`liza validate\` afterward to verify invariants.`

### Validation

```bash
# MULTI_AGENT_MODE.md: no Liza MCP references
grep -i 'Liza MCP tools' contracts/MULTI_AGENT_MODE.md  # expect: empty
grep -i 'MCP tool' contracts/MULTI_AGENT_MODE.md  # expect: empty (no Liza-specific MCP tool mentions)

# contract-activation.md: no mcp_servers.liza or liza-mcp
grep 'mcp_servers.liza' contracts/contract-activation.md  # expect: empty
grep 'liza-mcp' contracts/contract-activation.md  # expect: empty

# support.md: no MCP tools
grep 'MCP tools' internal/embedded/support.md  # expect: empty

# All files are valid markdown (no broken links, unclosed formatting)
# Visual inspection of changed lines for markdown validity
```

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | MULTI_AGENT_MODE.md Blackboard Protocol: "Liza MCP tools" → "Liza CLI commands" with CLI command name format | Arch doc, Contract Update Details, MULTI_AGENT_MODE.md section, line 90 before/after | Task 1 | Covered |
| 2 | MULTI_AGENT_MODE.md Blackboard Protocol: "MCP tool fails" → "CLI command fails" with `liza_mark_blocked` → `liza mark-blocked` | Arch doc, same section (second sentence of line 90) + done_when "no 'MCP tool'" | Task 1 | Covered |
| 3 | MULTI_AGENT_MODE.md Context Exhaustion Handoff: "`liza_handoff` MCP tool" → "`liza handoff` CLI command" | Arch doc, Contract Update Details, Context Exhaustion section, line 112 before/after | Task 1 | Covered |
| 4 | contract-activation.md: Remove `[mcp_servers.liza]` TOML block (lines 50-53) | Arch doc, Contract Update Details, contract-activation.md section | Task 1 | Covered |
| 5 | contract-activation.md: Update settings descriptions ("Liza MCP tools" → CLI-appropriate) | Arch doc, "Settings table (lines 13, 40)" | Task 1 | Covered |
| 6 | support.md: "Prefer `liza_*` MCP tools and CLI commands" → "Use `liza` CLI commands" | Arch doc, internal/embedded/support.md section, line 93 | Task 1 | Covered |
| 7 | All three files are valid markdown | done_when criterion | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: markdown-only changes to contracts and documentation, no executable behavior | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: these files ARE the documentation updates; no further docs needed | N/A |
