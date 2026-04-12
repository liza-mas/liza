# Code Plan: Remove MCP References from Documentation, Lessons, Specs, and Project Root

**Task:** architecture-4-architecture-to-code-plan-2
**Date:** 2026-04-12
**Architecture ref:** specs/arch-plan/20260412-125944-architecture-4.md (Category 3 + Category 4)
**Goal spec:** specs/goals/20260412-cli-native-access-control.md (Section 3 + MVP Scope docs bullet)

## Context

After Phases 1-3 remove the MCP server code and Phase 4 Task 0 rewrites prompt templates and Task 1 updates contracts, this task removes all remaining stale Liza MCP references from documentation, lessons, active specs, GUARDRAILS, and project root files. Only **Liza MCP** references (`liza-mcp`, `mcp__liza__*`, `.mcp.json` Liza server entry) are removed. External MCP servers (JetBrains, filesystem, etc.) are unrelated and remain untouched.

## Task 1: Remove MCP references from documentation, root files, GUARDRAILS, and lessons

**Description:** Remove Liza MCP references from 10 non-spec files: `REPOSITORY.md` (remove `liza-mcp` from directory tree line 10, remove `mcp.json` from build config line 129), `CONTRIBUTING.md` (remove `liza-mcp` from build command line 41), `README.md` (remove `liza-mcp` from executables description line 155), `RELEASE.md` (remove `liza-mcp` from release build line 18), `docs/TESTING.md` (remove `internal/mcp/` test target line 36), `docs/DEMO.md` (remove `liza-mcp` from prereqs line 15, remove `.mcp.json` from init output line 134), `GUARDRAILS.md` (remove `cli-mcp-surface-sync.md` trigger row line 49, remove `internal/embedded/mcp.json` from settings-master trigger line 50), `lessons/agents/settings-master-not-derived.md` (remove `mcp.json` from master/derived mapping and references), `docs/CONFIGURATION.md` (remove MCP Server Setup section lines 5-98 except rewrite `.claude/settings.json` description to remove MCP tool references; keep Two-Layer Architecture with updated content), `docs/USAGE_MULTI_AGENTS.md` (remove `liza-mcp` from prereqs line 64, remove Codex MCP regression note line 65, remove `.mcp.json` from init creates list line 134, rewrite "Configuring Claude Code (MCP)" section lines 394-452 to remove `.mcp.json` block and `mcp__liza__*` permissions and duality language, remove MCP reference from interactive diagnosis line 505).

**Done when:** `REPOSITORY.md` contains no `liza-mcp`. `CONTRIBUTING.md` contains no `liza-mcp`. `README.md` contains no `liza-mcp`. `RELEASE.md` contains no `liza-mcp`. `GUARDRAILS.md` does not reference `cli-mcp-surface-sync.md` or `mcp.json`. `docs/CONFIGURATION.md` contains no `liza-mcp`, `mcp__liza__`, or Liza-specific `.mcp.json`. `docs/USAGE_MULTI_AGENTS.md` contains no `liza-mcp` or `mcp__liza__`. `docs/DEMO.md` contains no `liza-mcp`. `docs/TESTING.md` does not reference `internal/mcp/`. `lessons/agents/settings-master-not-derived.md` does not reference `mcp.json`. Validation: `grep -n 'liza-mcp\|mcp__liza__\|internal/mcp/\|cli-mcp-surface-sync\|\.mcp\.json' REPOSITORY.md CONTRIBUTING.md README.md RELEASE.md GUARDRAILS.md docs/CONFIGURATION.md docs/USAGE_MULTI_AGENTS.md docs/DEMO.md docs/TESTING.md lessons/agents/settings-master-not-derived.md` returns empty (after excluding external MCP references that are unrelated to Liza).

**Scope:** Modify `REPOSITORY.md`, `CONTRIBUTING.md`, `README.md`, `RELEASE.md`, `docs/TESTING.md`, `docs/DEMO.md`, `docs/CONFIGURATION.md`, `docs/USAGE_MULTI_AGENTS.md`, `GUARDRAILS.md`, `lessons/agents/settings-master-not-derived.md`.

**Spec ref:** specs/arch-plan/20260412-125944-architecture-4.md, Category 3: Documentation and Lessons

### Per-File Change Details

#### REPOSITORY.md

- **Line 10:** Change `├── cmd/                    # Go CLI entry points (liza, liza-mcp)` to `├── cmd/                    # Go CLI entry point (liza)`
- **Line 129:** Change `Config files (`claude-settings.json`, `mcp.json`) and hooks are mastered directly in `internal/embedded/`.` to `Config files (`claude-settings.json`) and hooks are mastered directly in `internal/embedded/`.`

#### CONTRIBUTING.md

- **Line 41:** Change `make build          # Build liza and liza-mcp` to `make build          # Build liza`

#### README.md

- **Line 155:** Change `Liza relies on two executables: `liza` and `liza-mcp`:` to `Liza provides a single executable: `liza`:`
- **Line 156:** Update description to remove "created automatically, no sudo needed" phrasing if it references two binaries (keep the install path info).

#### RELEASE.md

- **Line 18:** Change `1. Build binaries for all supported platforms (liza + liza-mcp)` to `1. Build binaries for all supported platforms (liza)`

#### docs/TESTING.md

- **Line 36:** Delete the line `go test -race ./internal/mcp/      # MCP server (concurrent handlers)`

#### docs/DEMO.md

- **Line 15:** Change `- `liza` and `liza-mcp` Go binaries in PATH (see `make install`)` to `- `liza` Go binary in PATH (see `make install`)`
- **Line 134:** Delete the line `- `.mcp.json` — MCP server configuration (tells Claude Code how to start liza-mcp)`

#### GUARDRAILS.md

- **Line 49:** Delete the entire row `| Modifying CLI commands, flags, validation, or vocabulary that MCP handlers also expose | [cli-mcp-surface-sync.md](lessons/agents/cli-mcp-surface-sync.md)                              |`
- **Line 50:** Change trigger text from `Modifying \`internal/embedded/claude-settings.json\`, \`internal/embedded/mcp.json\`, \`internal/embedded/hooks/\`, or any file with master/derived copies` to `Modifying \`internal/embedded/claude-settings.json\`, \`internal/embedded/hooks/\`, or any file with master/derived copies`

#### lessons/agents/settings-master-not-derived.md

- **Line 4 (keywords):** Remove `MCP tools` from keyword list
- **Line 15:** Change `Similarly: \`internal/embedded/mcp.json\` → \`.mcp.json\`, and \`internal/embedded/hooks/enforce-init.sh\` → \`.claude/hooks/enforce-init.sh\`.` to `Similarly: \`internal/embedded/hooks/enforce-init.sh\` → \`.claude/hooks/enforce-init.sh\`.`
- **Line 31:** Delete the line `- \`internal/embedded/mcp.json\` — master`

#### docs/CONFIGURATION.md

This file requires the most substantial rewrite. Remove all Liza MCP-specific sections while preserving information about `.claude/settings.json` (which still exists, just without MCP tool permissions).

**Remove entirely (lines 5-7, 9-22, 47-98):**
- "## MCP Server Setup" section header and liza-mcp description (lines 5-7)
- `.mcp.json` creation block (lines 9-22)
- "### Troubleshooting MCP" section (lines 47-57) — but keep "State file errors" subsection (lines 59-61) as general troubleshooting
- "### MCP Tools Reference" section (lines 63-88) — entire section
- "### CLI vs MCP" section (lines 89-98) — entire section

**Rewrite (lines 24-44):**
- Rename the section covering `.claude/settings.json` from being under "MCP Server Setup" to a new "## Claude Code Settings" section
- Remove `enabledMcpjsonServers` bullet and `mcp__liza__*` description from "Key elements" (lines 29-30)
- Update "Key elements" to reference `Bash(liza:*)` permission instead of MCP tool permissions
- Keep the Two-Layer Architecture table (lines 36-44), updating the Project layer description from "Liza MCP tools, skills, git/build commands" to "Liza CLI permissions, skills, git/build commands"

**What remains after edit:** Title, new "## Claude Code Settings" section describing `.claude/settings.json`, Two-Layer Architecture, general troubleshooting (State file errors), then "## Configuration Matrix" (line 99 onward, unchanged).

#### docs/USAGE_MULTI_AGENTS.md

**Line 64:** Change `- \`liza\` and \`liza-mcp\` Go binaries in PATH` to `- \`liza\` Go binary in PATH (see \`make install\`)`

**Line 65:** Delete the Codex MCP regression note: `- When using \`--cli codex\`: Codex CLI > 0.118.0 (0.117.0–0.118.0 have a [regression](...) that cancels MCP tool calls in exec mode; 0.116.0 also works)`

**Lines 132-134:** In the `liza init` creates list:
- Change `- \`.claude/settings.json\` — Claude Code project permissions (Liza MCP tools, skills, git/build commands)` to `- \`.claude/settings.json\` — Claude Code project permissions (Liza CLI, skills, git/build commands)`
- Delete `- \`.mcp.json\` — MCP server configuration (tells Claude Code how to start liza-mcp)`

**Lines 394-452:** Rewrite "### Configuring Claude Code (MCP)" section:
- Rename to "### Configuring Claude Code"
- Remove the `.mcp.json` JSON block and description (lines 398-407)
- Update `.claude/settings.json` description (line 410): Remove "(MCP tools shown in full, other categories truncated)"
- Remove `"enabledMcpjsonServers": ["liza"],` from JSON example (line 414)
- Replace all 24 `mcp__liza__*` permission entries (lines 419-442) with `"Bash(liza:*)"` permission entry
- Update the description below (line 448) to remove "Liza MCP tools" from what the template pre-approves
- Rewrite line 450 duality paragraph: Replace "Both CLI commands (e.g., `liza add-task`) and MCP tools (e.g., `liza_add_tasks`) operate on the same `.liza/state.yaml` file. Claude Code agents use MCP tools for better error handling; the CLI is for manual use. `liza-mcp` starts gracefully even without `.liza/` — only `liza_version` works; all other tools return `NotInitializedError`." with "CLI commands (e.g., `liza add-task`) operate on `.liza/state.yaml` with proper locking. Agents use CLI commands via Bash with `--json` for structured output."
- Rewrite line 452: Replace "The templates are embedded into the binary. `liza init` writes the active copies to `.claude/settings.json` and `.mcp.json` in the project directory." with "The settings template is embedded into the binary. `liza init` writes the active copy to `.claude/settings.json` in the project directory."

**Line 505:** Replace "It picks up Liza's MCP tools from `.mcp.json` and can read `.liza/state.yaml`, agent logs, and prompts" with "It can read `.liza/state.yaml`, agent logs, and prompts"

## Task 2: Rewrite active specifications from MCP to CLI-only architecture

**Description:** Update 4 active specification files to replace Liza MCP references with CLI equivalents: `specs/architecture/supervision-model.md` (rewrite "MCP tools" to "CLI commands" throughout, rename "MCP Fallback" to "CLI Fallback", convert MCP tool underscore names to CLI hyphen commands, remove `liza://` MCP resources and replace with CLI equivalents, update architecture diagram), `specs/functional/1 - Liza.md` (remove MCP server row from Key Integrations table, remove "MCP companion server" from MVP scope, update success criterion from "CLI commands and MCP tools" to "CLI commands"), `specs/functional/1.1 - Contract System.md` (rewrite "MCP-first tool selection" to "Tool selection" on line 19), `specs/README.md` (change "via MCP tools" to "via CLI commands" on line 14).

**Done when:** `specs/architecture/supervision-model.md` contains no "MCP tool" or "MCP fallback" in Liza-specific context (external MCP references acceptable). `specs/functional/1 - Liza.md` contains no `liza-mcp` or "MCP server" as Liza component (line 28 row removed, line 40 rewritten, line 60 rewritten). `specs/functional/1.1 - Contract System.md` line 19 says "Tool selection" or "Tool Sub-Contract" without "MCP-first". `specs/README.md` line 14 says "via CLI commands" not "via MCP tools". Validation: `grep -in 'MCP tool\|MCP fallback\|mcp__liza__\|liza-mcp\|MCP server.*liza\|MCP-first\|via MCP tools' specs/architecture/supervision-model.md "specs/functional/1 - Liza.md" "specs/functional/1.1 - Contract System.md" specs/README.md` returns empty.

**Scope:** Modify `specs/architecture/supervision-model.md`, `specs/functional/1 - Liza.md`, `specs/functional/1.1 - Contract System.md`, `specs/README.md`.

**Spec ref:** specs/arch-plan/20260412-125944-architecture-4.md, Category 4: Active Specifications

### Per-File Change Details

#### specs/architecture/supervision-model.md

- **Line 3 (title description):** Change `Who does what — supervisor vs agent via MCP tools.` to `Who does what — supervisor vs agent via CLI commands.`
- **Line 32:** Change `MCP tools provide agent-initiated workflow actions and manual fallback paths for supervisor actions.` to `CLI commands provide agent-initiated workflow actions and manual fallback paths for supervisor actions.`
- **Lines 49-60 (Supervisor-Guaranteed section):**
  - Rename `### Supervisor-Guaranteed + MCP Fallback` to `### Supervisor-Guaranteed + CLI Fallback`
  - Change `The MCP tool exists as a manual/administrative path` to `The CLI command exists as a manual/administrative path`
  - Rename column `MCP Tool` to `CLI Command` in the table
  - Convert tool names: `liza_claim_task` → `liza claim-task`, `liza_wt_merge` → `liza wt-merge`, `liza_clear_stale_review_claims` → `liza clear-stale-review-claims`
  - Change `Why MCP fallback exists:` to `Why CLI fallback exists:`
- **Lines 62-75 (Agent-Initiated section):**
  - Rename `### Agent-Initiated (via MCP tools)` to `### Agent-Initiated (via CLI commands)`
  - Rename column `MCP Tool` to `CLI Command` in the table
  - Convert all underscore tool names to hyphenated CLI commands: `liza_submit_for_review` → `liza submit-for-review`, `liza_submit_verdict` → `liza submit-verdict`, `liza_handoff` → `liza handoff`, `liza_mark_blocked` → `liza mark-blocked`, `liza_add_tasks` → `liza add-tasks`, `liza_supersede_task` → `liza supersede-task`, `liza_release_claim` → `liza release-claim`
- **Lines 76-84 (Administrative section):**
  - Rename `### Administrative (MCP tools, not part of normal flow)` to `### Administrative (CLI commands, not part of normal flow)`
  - Rename column `MCP Tool` to `CLI Command`
  - Convert tool names: `liza_wt_create` → `liza wt-create`, `liza_wt_delete` → `liza wt-delete`, `liza_delete_agent` → `liza delete agent`, `liza_update_sprint_metrics` → `liza update-sprint-metrics`, `liza_analyze` → `liza analyze`
- **Lines 86-96 (Read-Only section):**
  - Rename `### Read-Only (MCP tools + resources)` to `### Read-Only (CLI commands)`
  - Rename column `Tool/Resource` to `CLI Command`
  - Convert tool names: `liza_get` → `liza get`, `liza_status` → `liza status`, `liza_validate` → `liza validate`, `liza_version` → `liza version`
  - Delete the 3 MCP resource rows: `liza://state`, `liza://tasks`, `liza://agents`
- **Line 110:** Change `calls MCP tools:` to `runs CLI commands:`
- **Line 111-114:** Convert MCP tool names to CLI commands in the diagram: `submit-for-review`, `submit-verdict`, `mark-blocked`, `handoff`
- **Line 122:** Change `Both supervisor and MCP handlers call the same \`commands.*\` functions` to `Both supervisor and CLI commands call the same \`commands.*\` functions`

#### specs/functional/1 - Liza.md

- **Line 28:** Delete the MCP server row from the Key Integrations table: `| MCP server (\`liza-mcp\`) | Tool/resource surface for agent actions and supervisor fallback operations |`
- **Line 40:** Change `Go CLI tooling (\`liza\`) with MCP companion server (\`liza-mcp\`)` to `Go CLI tooling (\`liza\`)`
- **Line 60:** Change `Agents can reliably invoke Liza through CLI commands and MCP tools` to `Agents can reliably invoke Liza through CLI commands`

#### specs/functional/1.1 - Contract System.md

- **Line 19:** Change `1.1.7 — [Tool Sub-Contract](../../contracts/AGENT_TOOLS.md) — MCP-first tool selection and operation-specific defaults` to `1.1.7 — [Tool Sub-Contract](../../contracts/AGENT_TOOLS.md) — Tool selection and operation-specific defaults`

#### specs/README.md

- **Line 14:** Change `| [Supervision Model](architecture/supervision-model.md) | Action responsibility: supervisor vs agent via MCP tools |` to `| [Supervision Model](architecture/supervision-model.md) | Action responsibility: supervisor vs agent via CLI commands |`

## Dependency Graph

```
Task 1 ──┐
         ├──> (no dependencies between tasks — can execute in parallel)
Task 2 ──┘
```

No shared files between tasks. All 14 files are partitioned:
- Task 1: REPOSITORY.md, CONTRIBUTING.md, README.md, RELEASE.md, docs/TESTING.md, docs/DEMO.md, docs/CONFIGURATION.md, docs/USAGE_MULTI_AGENTS.md, GUARDRAILS.md, lessons/agents/settings-master-not-derived.md
- Task 2: specs/architecture/supervision-model.md, specs/functional/1 - Liza.md, specs/functional/1.1 - Contract System.md, specs/README.md

## Global Stale-Reference Scan

The parent task's done_when includes a global stale-reference scan across `*.md`, `*.tmpl`, `*.go` (excluding historical directories). Since `*.tmpl` and `*.go` files are in sibling task architecture-4-architecture-to-code-plan-0's scope, this scan can only fully pass after all sibling tasks are also merged to the integration branch.

Each coding task should validate its own files with targeted greps (see per-task done_when). The global scan serves as post-merge integration validation.

Scan command (for reference):
```bash
grep -rn 'liza-mcp\|mcp__liza__\|liza_mcp\|\.mcp\.json.*liza\|WriteMCP\|mergeMCP\|mcpSettings' \
  --include='*.md' --include='*.tmpl' --include='*.go' \
  | grep -v 'arch-plan/' | grep -v 'ADR/' | grep -v 'release_notes/' | grep -v '_archive/' \
  | grep -v '_test.go' | grep -v 'architecture-review.md' | grep -v 'code_quality_assessment.md' \
  | grep -v 'specs/goals/' | grep -v 'architectural-issues.md' | grep -v 'mas-survey.md' \
  | grep -v 'specs/plans/'
```

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Remove Liza MCP sections from docs/CONFIGURATION.md | Arch plan Category 3, docs/CONFIGURATION.md row | Task 1 | Covered |
| 2 | Remove liza-mcp and mcp__liza__ from docs/USAGE_MULTI_AGENTS.md | Arch plan Category 3, docs/USAGE_MULTI_AGENTS.md row | Task 1 | Covered |
| 3 | Remove liza-mcp from docs/DEMO.md prereqs and .mcp.json reference | Arch plan Category 3, docs/DEMO.md row | Task 1 | Covered |
| 4 | Remove internal/mcp/ test target from docs/TESTING.md | Arch plan Category 3, docs/TESTING.md row | Task 1 | Covered |
| 5 | Remove liza-mcp from REPOSITORY.md directory tree and mcp.json from build config | Arch plan Category 3, REPOSITORY.md row | Task 1 | Covered |
| 6 | Remove cli-mcp-surface-sync.md row and mcp.json from GUARDRAILS.md | Arch plan Category 3, GUARDRAILS.md row | Task 1 | Covered |
| 7 | Remove mcp.json references from lessons/agents/settings-master-not-derived.md | Arch plan Category 3, lessons row | Task 1 | Covered |
| 8 | Remove liza-mcp from CONTRIBUTING.md build command (line 41) | Arch plan Category 3, CONTRIBUTING.md row | Task 1 | Covered |
| 9 | Remove liza-mcp from README.md executables (line 155) | Arch plan Category 3, README.md row | Task 1 | Covered |
| 10 | Remove liza-mcp from RELEASE.md release build (line 18) | Arch plan Category 3, RELEASE.md row | Task 1 | Covered |
| 11 | Rewrite supervision-model.md: MCP tool → CLI command, remove MCP resources/fallback | Arch plan Category 4, supervision-model.md row | Task 2 | Covered |
| 12 | Update 1 - Liza.md: remove MCP server component, rewrite to CLI-only | Arch plan Category 4, 1 - Liza.md row | Task 2 | Covered |
| 13 | Update 1.1 - Contract System.md: remove "MCP-first" language | Arch plan Category 4, 1.1 - Contract System.md row | Task 2 | Covered |
| 14 | Update specs/README.md: "via MCP tools" → "via CLI commands" | Arch plan Category 4, specs/README.md row | Task 2 | Covered |
| 15 | Global stale-reference scan returns empty | Arch plan Verification Strategy, global scan | Task 1 + Task 2 (per-file) + post-merge (global) | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: pure documentation changes, no behavior change; no e2e tests applicable | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | Task 1 + Task 2 (this IS the documentation update task) | Covered |
