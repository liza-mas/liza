# Code Plan: Rewrite Prompt Templates and Go Code to CLI-Only

**Task:** architecture-4a-architecture-to-code-plan-0
**Date:** 2026-04-12
**Architecture ref:** specs/arch-plan/20260412-124731-architecture-4a.md (Category 1: Prompt Templates)
**Goal spec:** specs/goals/20260412-cli-native-access-control.md

## Context

The prompt system uses an `MCPToolPrefix` mechanism to inject `mcp__liza__` (Claude) or `""` (Codex) before all Liza tool names in templates. After MCP server removal (Phase 3 — merged), all Liza operations are CLI commands via Bash. The `ToolPrefix` mechanism and MCP tool call syntax must be replaced with direct CLI command invocations.

The coupling between Go struct fields (`ToolPrefix`), template function (`toolSearchHint`), and template references (`{{.ToolPrefix}}`, `{{toolSearchHint ...}}`) requires coordinated changes. Templates are `//go:embed`'d — they compile into the binary and are validated at test time.

## Decomposition Rationale

Splitting into two sequential tasks:

1. **CP-1 (Template rewrites):** All 20 `.tmpl` files + `builder_test.go` assertion updates. This ensures templates produce CLI-format output and tests pass with the new output format. Go struct fields remain but become unused.

2. **CP-2 (Go code cleanup):** Remove dead Go infrastructure — `MCPToolPrefix()`, `toolSearchHint()`, `ToolPrefix` struct fields, template function registration, `prompt.go` calls, `builder.go` inline tools rewrite. Tests updated to remove `ToolPrefix` from test data.

**Shared file:** `builder_test.go` is modified by both tasks (CP-1: assertion strings; CP-2: test data and function signatures). The `depends_on` ensures sequential execution.

**Why not a single task:** 27 files in one task makes review expensive. The split keeps each review focused: CP-1 on template content correctness, CP-2 on Go code structural correctness.

## CLI Command Syntax Reference

MCP tool names map to CLI commands via underscore-to-hyphen conversion. Positional args replace JSON params where the CLI already uses them. The `--json` flag is appended for structured output.

| MCP Tool | CLI Command |
|----------|-------------|
| `liza_get` | `liza get tasks`, `liza get tasks/<id>`, `liza get agents` `--json` |
| `liza_status` | `liza status --json` |
| `liza_validate` | `liza validate --json` |
| `liza_submit_for_review` | `liza submit-for-review <task-id> <commit-sha> --agent-id <id> --json` |
| `liza_await_verdict` | `liza await-verdict <task-id> --agent-id <id> --json` |
| `liza_handoff` | `liza handoff <task-id> <summary> <next-action> --agent-id <id> --json` |
| `liza_mark_blocked` | `liza mark-blocked <task-id> --agent-id <id> --reason "..." --questions "..." --json` |
| `liza_write_checkpoint` | `liza write-checkpoint <task-id> --agent-id <id> --intent "..." --validation-plan "..." --files-to-modify "..." --json` |
| `liza_set_task_output` | `liza set-task-output <task-id> --output <path.json> --agent-id <id> --json` |
| `liza_submit_verdict` | `liza submit-verdict <task-id> <APPROVED\|REJECTED> [reason] --agent-id <id> --json` |
| `liza_await_resubmission` | `liza await-resubmission <task-id> --agent-id <id> --json` |
| `liza_add_tasks` | `liza add-tasks --tasks-file <path.json> --agent-id <id> --json` |
| `liza_supersede_task` | `liza supersede-task <task-id> --replacement-ids "..." --reason "..." --agent-id <id> --json` |
| `liza_assess_blocked` | `liza assess-blocked <task-id> --note "..." --agent-id <id> --json` |
| `liza_assess_hypothesis_exhausted` | `liza assess-hypothesis-exhausted <task-id> --note "..." --agent-id <id> --json` |
| `liza_wt_delete` | `liza wt-delete <task-id> --agent-id <id> --json` |
| `liza_sprint_checkpoint` | `liza sprint-checkpoint --agent-id <id> --json` |
| `liza_update_sprint_metrics` | `liza update-sprint-metrics --agent-id <id> --json` |
| `liza_set_discovery_disposition` | `liza set-discovery-disposition <id> <disposition> --json` |

---

## Task 1 (CP-1): Rewrite all 20 template files from MCP to CLI syntax

**Description:** Rewrite all 20 template files under `internal/prompts/templates/` from `{{.ToolPrefix}}liza_*` MCP tool call syntax to `liza <command> --json` CLI command syntax. Remove all `{{toolSearchHint ...}}` calls. Remove MCP-specific language from `base_prompt.tmpl` (MCP fallback instructions, MCP discovery block, MCP approval language, "MCP tool calls" references). Replace "MCP tool" emphasis with "CLI command" emphasis in verdict and submission templates. Update `builder_test.go` assertions to match new CLI-format template output.

**Done when:** No `{{.ToolPrefix}}` exists in any `.tmpl` file under `internal/prompts/templates/`. No `{{toolSearchHint` exists in any `.tmpl` file under `internal/prompts/templates/`. Grep for 'MCP tool' (case-insensitive) in `.tmpl` files returns no Liza-MCP-specific matches (external MCP references in base_prompt.tmpl's AGENT_TOOLS.md mention are acceptable since they refer to the user's AGENT_TOOLS contract, not Liza MCP). `go build ./cmd/liza` succeeds. `go test ./internal/prompts/...` passes. All assertion strings in `builder_test.go` that previously matched MCP tool names (`mcp__liza__liza_*`) or MCP-specific language (`"APPROVED: use MCP tools"`, `"MCP tool calls"`, `"MCP TOOL DISCOVERY"`) now match CLI equivalents.

**Scope:** Modify `internal/prompts/templates/base_prompt.tmpl`, `internal/prompts/templates/blocks/doer_tools.tmpl`, `internal/prompts/templates/blocks/reviewer_tools.tmpl`, `internal/prompts/templates/blocks/verdict_submission.tmpl`, `internal/prompts/templates/blocks/submission_phase.tmpl`, `internal/prompts/templates/blocks/blocking_protocol.tmpl`, `internal/prompts/templates/blocks/implementation_phase.tmpl`, `internal/prompts/templates/blocks/review_instructions.tmpl`, `internal/prompts/templates/blocks/reviewer_state_transitions.tmpl`, `internal/prompts/templates/blocks/collective_plan_scoping.tmpl`, `internal/prompts/templates/blocks/integration_fix.tmpl`, `internal/prompts/templates/blocks/prior_rejection.tmpl`, `internal/prompts/templates/wake_many_to_one_ready.tmpl`, `internal/prompts/templates/wake_immediate_discovery.tmpl`, `internal/prompts/templates/wake_blocked_tasks.tmpl`, `internal/prompts/templates/wake_hypothesis_exhausted.tmpl`, `internal/prompts/templates/wake_initial_planning.tmpl`, `internal/prompts/templates/wake_planning_complete.tmpl`, `internal/prompts/templates/wake_sprint_complete.tmpl`, `internal/prompts/templates/wake_coding_complete.tmpl`, `internal/prompts/builder_test.go`.

**Spec ref:** specs/arch-plan/20260412-124731-architecture-4a.md

### Transformation rules (for coder reference)

Per the architecture plan section "Prompt Template Rewrite Strategy" -> Transformation Rules:

1. **Tool prefix removal:** Delete `{{.ToolPrefix}}` from all template files. Delete all `{{toolSearchHint .ToolPrefix "..."}}` calls.
2. **MCP tool call -> CLI command:** Convert `{"param": "value"}` JSON syntax to CLI `--flag <value>` syntax. Map tool names: underscore to hyphen (`liza_submit_for_review` -> `liza submit-for-review`). See CLI Command Syntax Reference table above.
3. **`--json` flag:** Add `--json` to all CLI commands in templates (agents parse structured JSON output).
4. **MCP fallback removal in `base_prompt.tmpl`:** Delete lines 22-29 ("If the MCP server itself is unavailable, fall back to the CLI:" block — CLI is now the only path, so current CLI fallback commands become the primary instructions). Delete line 46 ("All liza_* operations are MCP tool calls"). Delete lines 47-49 ("MCP TOOL DISCOVERY" block). Change line 10 from `"APPROVED: use MCP tools with escalated permissions"` to appropriate CLI-only language. Change line 19 from `"For MODIFYING state: use role-specific MCP tools ONLY"` to CLI-only. Change line 21 from MCP tool failure to CLI failure language. Change line 60 from `"through MCP tools"` to `"through CLI commands"`.
5. **"MCP tool" language -> "CLI command":** In verdict_submission.tmpl, change "is an MCP tool — call it as a tool" to "you MUST execute this CLI command in this session". In wake templates, replace "execute MCP tools immediately" with "run CLI commands immediately".
6. **Reviewer verdict emphasis (reviewer_tools.tmpl):** Change `"liza_submit_verdict — Submit review verdict (MCP tool call, NOT a CLI command)"` and the `"Do NOT use go run, liza submit-verdict..."` warning to `"liza submit-verdict — Submit review verdict"` with emphasis on "you MUST run this CLI command".
7. **Query tools in base_prompt.tmpl:** Replace `{{.ToolPrefix}}liza_get`, `{{.ToolPrefix}}liza_status`, `{{.ToolPrefix}}liza_validate` with direct CLI equivalents: `liza get --json`, `liza status --json`, `liza validate --json`.

### Execution sequence

1. Rewrite `base_prompt.tmpl` first — it's the foundation all agents see.
2. Rewrite block templates (`blocks/*.tmpl`) — these are role-specific tool definitions and instructions.
3. Rewrite wake templates (`wake_*.tmpl`) — these are orchestrator wake instructions.
4. Update `builder_test.go` assertion strings to match new template output.
5. Run `go build ./cmd/liza` and `go test ./internal/prompts/...` to verify.

---

## Task 2 (CP-2): Remove Go ToolPrefix infrastructure and update remaining tests

**Description:** Delete `MCPToolPrefix()` function and `toolSearchHint()` function from `internal/prompts/role_context.go`. Remove `ToolPrefix` field from `RoleContextData` struct in `role_context.go`. Remove `ToolPrefix` field from `BasePromptConfig` struct in `builder.go`. Remove `toolPrefix` parameter from `RenderOrchestratorDashboard()` function signature in `builder.go`. Rewrite orchestrator inline tool definitions in `builder.go` (lines 120-166) from MCP `toolPrefix`-based format to direct CLI command format with `--json`. Remove `ToolPrefix` field from `wakeTemplateData` and `wakePlanningCompleteData` structs in `wake.go`. Remove `toolPrefix` parameter from `buildInstructionsForWakeTrigger()` and all wake template data construction in `wake.go`. Remove `"toolSearchHint": toolSearchHint` registration from `funcMap` in `templates.go`. Remove all `MCPToolPrefix()` calls and `ToolPrefix` field assignments from `internal/agent/prompt.go` (lines 27, 78, 79, 97, 126). Update `builder_test.go`: remove `ToolPrefix: "mcp__liza__"` from all test config structs, remove `toolPrefix` argument from all `RenderOrchestratorDashboard()` calls, update orchestrator dashboard inline tool assertions from MCP format to CLI format. Delete `TestMCPToolPrefix` and `TestToolSearchHint` functions from `role_context_test.go`.

**Done when:** No `ToolPrefix`, `MCPToolPrefix`, or `toolSearchHint` symbols exist in `internal/prompts/*.go` or `internal/agent/prompt.go`. No `mcp__liza__` exists in `internal/prompts/builder_test.go`. No `TestMCPToolPrefix` or `TestToolSearchHint` exists in `internal/prompts/role_context_test.go`. `go build ./cmd/liza` succeeds. `go test ./internal/prompts/... ./internal/agent/...` passes.

**Scope:** Modify `internal/prompts/role_context.go`, `internal/prompts/builder.go`, `internal/prompts/wake.go`, `internal/prompts/templates.go`, `internal/agent/prompt.go`, `internal/prompts/builder_test.go`, `internal/prompts/role_context_test.go`.

**Spec ref:** specs/arch-plan/20260412-124731-architecture-4a.md

### Detailed changes per file

**`internal/prompts/role_context.go`:**
- Delete `MCPToolPrefix()` function (lines 10-19)
- Delete `toolSearchHint()` function (lines 21-33)
- Remove `ToolPrefix string` field from `RoleContextData` struct (line 120) and its comment

**`internal/prompts/builder.go`:**
- Remove `ToolPrefix string` field from `BasePromptConfig` struct (line 22) and its comment
- Remove `toolPrefix` parameter from `RenderOrchestratorDashboard()` signature (line 42)
- Remove `orchToolHint` variable (line 120) and the `toolSearchHint` call
- Rewrite the orchestrator inline tool section (lines 121-166) from `fmt.Sprintf` with `toolPrefix` to direct CLI command strings. Each tool becomes a CLI command with `--json`:
  - `%sliza_add_tasks` -> `liza add-tasks --tasks-file <path.json> --agent-id "%s" --json`
  - `%sliza_supersede_task` -> `liza supersede-task <task-id> --replacement-ids "..." --reason "..." --agent-id "%s" --json`
  - `%sliza_assess_blocked` -> `liza assess-blocked <task-id> --note "..." --agent-id "%s" --json`
  - `%sliza_wt_delete` -> `liza wt-delete <task-id> --agent-id "%s" --json`
  - `%sliza_sprint_checkpoint` -> `liza sprint-checkpoint --agent-id "%s" --json`
  - `%sliza_update_sprint_metrics` -> `liza update-sprint-metrics --agent-id "%s" --json`
  - `%sliza_add_tasks` (in TASK CREATION ORDER) -> `liza add-tasks`
  - Change "On MCP tool errors" to "On CLI command errors"
- Remove `orchToolHint` from the ORCHESTRATOR COMMANDS header
- Update `buildInstructionsForWakeTrigger` call (line 89) to remove `toolPrefix` arg

**`internal/prompts/wake.go`:**
- Remove `ToolPrefix string` field from `wakeTemplateData` struct (line 30) and its comment
- Remove `ToolPrefix string` field from `wakePlanningCompleteData` struct (line 43) and its comment
- In `buildInstructionsForWakeTrigger()` (line 182): remove `toolPrefix` parameter, remove `ToolPrefix: toolPrefix` from `agentData` construction (line 183), remove `ToolPrefix: toolPrefix` from INITIAL_PLANNING case (line 187-188), remove `ToolPrefix: toolPrefix` from PLANNING_COMPLETE case (line 197-198)

**`internal/prompts/templates.go`:**
- Remove `"toolSearchHint": toolSearchHint,` from `funcMap` (line 26)

**`internal/agent/prompt.go`:**
- In `baseConfigFrom()` (line 17): remove `ToolPrefix: prompts.MCPToolPrefix(config.CLIName)` (line 27)
- In `buildOrchestratorPromptContext()` (line 67): remove `toolPrefix := prompts.MCPToolPrefix(config.CLIName)` (line 78), update `RenderOrchestratorDashboard` call to remove `toolPrefix` arg (line 79), remove `ToolPrefix: toolPrefix` from `RoleContextData` (line 97)
- In `buildTaskRoleContextData()` (line 116): remove `ToolPrefix: prompts.MCPToolPrefix(config.CLIName)` (line 126)
- Clean up unused import of `prompts` if `MCPToolPrefix` was the only usage (verify — other `prompts.*` calls exist so import stays)

**`internal/prompts/builder_test.go`:**
- Remove `ToolPrefix: "mcp__liza__"` from all `BasePromptConfig` and `RoleContextData` test data structs
- Update `RenderOrchestratorDashboard()` calls to remove the `"mcp__liza__"` argument (change from 4 args to 3)
- Update orchestrator dashboard test assertions: replace MCP-format inline tool names with CLI-format, remove `"(resolve AFTER initialization:"` assertion
- Update `buildInstructionsForWakeTrigger()` calls to remove the `"mcp__liza__"` argument

**`internal/prompts/role_context_test.go`:**
- Delete `TestMCPToolPrefix` function (lines 433-449)
- Delete `TestToolSearchHint` function (lines 451-466)

### Execution sequence

1. Remove functions from `role_context.go` (MCPToolPrefix, toolSearchHint)
2. Remove fields and params from `builder.go`, rewrite inline tools
3. Remove fields and threading from `wake.go`
4. Remove template function registration from `templates.go`
5. Remove calls and field assignments from `prompt.go`
6. Update `builder_test.go` (remove ToolPrefix from test data, update signatures, update dashboard assertions)
7. Delete test functions from `role_context_test.go`
8. Run `go build ./cmd/liza` and `go test ./internal/prompts/... ./internal/agent/...` to verify.

---

## Shared-File Audit

| File | CP-1 | CP-2 |
|------|------|------|
| `internal/prompts/templates/base_prompt.tmpl` | Modify | -- |
| `internal/prompts/templates/blocks/doer_tools.tmpl` | Modify | -- |
| `internal/prompts/templates/blocks/reviewer_tools.tmpl` | Modify | -- |
| `internal/prompts/templates/blocks/verdict_submission.tmpl` | Modify | -- |
| `internal/prompts/templates/blocks/submission_phase.tmpl` | Modify | -- |
| `internal/prompts/templates/blocks/blocking_protocol.tmpl` | Modify | -- |
| `internal/prompts/templates/blocks/implementation_phase.tmpl` | Modify | -- |
| `internal/prompts/templates/blocks/review_instructions.tmpl` | Modify | -- |
| `internal/prompts/templates/blocks/reviewer_state_transitions.tmpl` | Modify | -- |
| `internal/prompts/templates/blocks/collective_plan_scoping.tmpl` | Modify | -- |
| `internal/prompts/templates/blocks/integration_fix.tmpl` | Modify | -- |
| `internal/prompts/templates/blocks/prior_rejection.tmpl` | Modify | -- |
| `internal/prompts/templates/wake_many_to_one_ready.tmpl` | Modify | -- |
| `internal/prompts/templates/wake_immediate_discovery.tmpl` | Modify | -- |
| `internal/prompts/templates/wake_blocked_tasks.tmpl` | Modify | -- |
| `internal/prompts/templates/wake_hypothesis_exhausted.tmpl` | Modify | -- |
| `internal/prompts/templates/wake_initial_planning.tmpl` | Modify | -- |
| `internal/prompts/templates/wake_planning_complete.tmpl` | Modify | -- |
| `internal/prompts/templates/wake_sprint_complete.tmpl` | Modify | -- |
| `internal/prompts/templates/wake_coding_complete.tmpl` | Modify | -- |
| `internal/prompts/builder_test.go` | Modify | Modify |
| `internal/prompts/role_context.go` | -- | Modify |
| `internal/prompts/builder.go` | -- | Modify |
| `internal/prompts/wake.go` | -- | Modify |
| `internal/prompts/templates.go` | -- | Modify |
| `internal/agent/prompt.go` | -- | Modify |
| `internal/prompts/role_context_test.go` | -- | Modify |

`builder_test.go` is modified by both tasks. CP-2 depends on CP-1 (required by shared-file rule and by semantic correctness: templates must stop referencing `{{.ToolPrefix}}` before the field is removed from Go structs).

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Delete MCPToolPrefix() and toolSearchHint() from role_context.go | Arch plan Category 1 table, role_context.go row | CP-2 | Covered |
| 2 | Remove ToolPrefix field from RoleContextData | Arch plan Template Data Structure Changes | CP-2 | Covered |
| 3 | Remove ToolPrefix from BasePromptConfig | Arch plan Template Data Structure Changes | CP-2 | Covered |
| 4 | Remove ToolPrefix from wakeTemplateData and wakePlanningCompleteData | Arch plan Template Data Structure Changes | CP-2 | Covered |
| 5 | Remove toolSearchHint template function registration from templates.go | Arch plan Transformation Rules item 3 | CP-2 | Covered |
| 6 | Remove toolPrefix param from RenderOrchestratorDashboard | Arch plan Template Data Structure Changes | CP-2 | Covered |
| 7 | Remove MCPToolPrefix() calls from prompt.go (lines 27, 78, 97, 126) | Arch plan Callers to Update | CP-2 | Covered |
| 8 | Rewrite orchestrator inline tools in builder.go to CLI format | Arch plan Transformation Rules item 7 | CP-2 | Covered |
| 9 | Remove {{.ToolPrefix}} from all .tmpl files | Arch plan Transformation Rules item 1 | CP-1 | Covered |
| 10 | Remove {{toolSearchHint ...}} from all .tmpl files | Arch plan Transformation Rules item 3 | CP-1 | Covered |
| 11 | Convert MCP tool call JSON syntax to CLI flag syntax in templates | Arch plan Transformation Rules item 2 | CP-1 | Covered |
| 12 | Add --json flag to CLI commands in templates | Arch plan Transformation Rules item 4 | CP-1 | Covered |
| 13 | Remove MCP fallback instructions from base_prompt.tmpl | Arch plan Transformation Rules item 5 | CP-1 | Covered |
| 14 | Remove MCP discovery block from base_prompt.tmpl | Arch plan Transformation Rules item 5 | CP-1 | Covered |
| 15 | Remove MCP approval language from base_prompt.tmpl | Arch plan Transformation Rules item 5 | CP-1 | Covered |
| 16 | Replace "MCP tool" language with "CLI command" in templates | Arch plan Transformation Rules item 6 | CP-1 | Covered |
| 17 | Rewrite reviewer verdict emphasis | Arch plan Transformation Rules item 8 | CP-1 | Covered |
| 18 | Update builder_test.go: remove mcp__liza__ assertion strings | Arch plan Per-Task Verification, Task 0 | CP-1, CP-2 | Covered |
| 19 | Delete TestMCPToolPrefix from role_context_test.go | Arch plan Category 1 table, role_context_test.go row | CP-2 | Covered |
| 20 | Delete TestToolSearchHint from role_context_test.go | Arch plan Category 1 table, role_context_test.go row | CP-2 | Covered |
| 21 | go build ./cmd/liza succeeds | Task done_when | CP-1, CP-2 | Covered |
| 22 | go test ./internal/prompts/... ./internal/agent/... passes | Task done_when | CP-1, CP-2 | Covered |
| 23 | No ToolPrefix, MCPToolPrefix, or toolSearchHint in internal/prompts/*.go or internal/agent/prompt.go | Task done_when | CP-2 | Covered |
| 24 | No {{.ToolPrefix}} in .tmpl files | Task done_when | CP-1 | Covered |
| 25 | No mcp__liza__ in builder_test.go | Task done_when | CP-1, CP-2 | Covered |
| 26 | No TestMCPToolPrefix or TestToolSearchHint in role_context_test.go | Task done_when | CP-2 | Covered |
| 27 | Grep 'MCP tool' (case-insensitive) in .tmpl returns no Liza-MCP matches | Task done_when | CP-1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: internal refactor of prompt generation, no user-visible behavior change; existing integration tests exercise prompt rendering via go test | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: documentation updates are handled by sibling task architecture-4a-architecture-to-code-plan-2 | N/A |
