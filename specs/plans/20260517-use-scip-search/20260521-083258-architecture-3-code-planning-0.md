# Code Plan: Agent Prompt scip-search Contract and Claude Permission

Task ID: `architecture-3-code-planning-0`

Source artifacts:
- Goal spec: `specs/goals/20260517-use-scip-search.md`
- Architecture reference: `specs/arch-plan/20260517-use-scip-search/20260521-020422-architecture-3.md`
- Prior runtime service plan: `specs/plans/20260517-use-scip-search/20260521-073929-architecture-2-code-planning-0.md`
- Orchestrator project-root prompt-data plan: `specs/plans/20260517-use-scip-search/20260521-075141-architecture-2-code-planning-3.md`

## Intent

Plan the implementation tasks that turn successful runtime `scip-search` index metadata into a concise, capability-aware base prompt section, and add the Claude Bash permission for `scip-search`.

## Source Basis

Based on:
- `specs/goals/20260517-use-scip-search.md`
- `specs/arch-plan/20260517-use-scip-search/20260521-020422-architecture-3.md`
- `specs/plans/20260517-use-scip-search/20260521-073929-architecture-2-code-planning-0.md`
- `specs/plans/20260517-use-scip-search/20260521-075141-architecture-2-code-planning-3.md`
- `internal/agent/prompt.go`
- `internal/agent/supervisor.go`
- `internal/prompts/builder.go`
- `internal/prompts/templates/base_prompt.tmpl`
- `internal/embedded/claude-settings.json`
- `internal/scipsearch/scipsearch.go`
- `internal/models/config.go`
- `GUARDRAILS.md`
- `lessons/agents/worktree-file-path-consistency.md`
- `lessons/agents/worktree-path-construction.md`
- `lessons/agents/settings-master-not-derived.md`
- `lessons/agents/large-test-file-reads.md`
- `specs/architecture/ADR/README.md`

No assumptions are required.

## Architectural Notes

The prompt renderer should accept structured records rather than discovering indexes itself. Runtime index generation and available-index discovery are owned by the architecture-2 implementation chain. This plan consumes the read-only successful-index boundary and renders only the records supplied to the base prompt.

`internal/prompts` should own the role-independent text contract because `base_prompt.tmpl` is shared by doers, reviewers, orchestrators, and planning roles. Agent prompt assembly should only populate `BasePromptConfig` from the correct target-root source. It should not duplicate command wording or reconstruct index paths.

The architecture-2 orchestrator prompt-data task is already responsible for exposing project-root successful index metadata without wording. The prompt plumbing task below depends on that task and should bridge or reuse its output when adding the base prompt data path, rather than adding a second project-root discovery path.

Claude permission work is independent and belongs in the embedded master settings file only. Generated `.claude/settings.json` remains out of scope.

## Planned Coding Tasks

### Task 1: Base Prompt scip-search Rendering Contract

**desc:** Base prompt scip-search rendering contract: extend `internal/prompts` base prompt configuration and template rendering so supplied successful SCIP index prompt records render a conditional, concise `scip-search` section with deterministic Go, TypeScript, and Python ordering, explicit absolute `--index` paths, supported `symbols` and `references` examples, file-open loop guidance, snapshot semantics, and capability-specific implementation wording; render no section when the supplied record list is empty. Out of scope: querying the runtime index service, lifecycle indexing call sites, Claude settings, README/operator documentation, generated `.claude/settings.json`, and wrapper or MCP abstractions for `scip-search`.

**done_when:** Unit tests in `internal/prompts` prove `BuildBasePrompt` renders no `scip-search` section when no successful index records are supplied; renders each supplied Go, TypeScript, and Python absolute index path in every relevant `--index` example without placeholder or inferred paths; includes `scip-search symbols`, `scip-search references --location-only`, and `nl -ba <result-path> | sed -n '<first-line>,<last-line>p'` loop guidance; states indexes are snapshots that will not reflect subsequent agent edits; renders Go implementations guidance, TypeScript implementations guidance caveated as upstream-supported but not locally verified, and Python guidance saying `scip-search implementations` is not supported for Python; and does not render a Python implementations command example.

**scope:** In scope: `internal/prompts/builder.go`, `internal/prompts/templates/base_prompt.tmpl`, focused `internal/prompts` tests, prompt-data structs or helpers needed to render language, absolute index path, and capability wording, deterministic Go/TypeScript/Python render ordering, and omission behavior for an empty record list. Out of scope: `internal/agent/prompt.go` runtime index lookup, lifecycle indexing call sites, init-time validation, `internal/embedded/claude-settings.json`, README/operator documentation, generated `.claude/settings.json`, and `.liza/agent-outputs/`.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-020422-architecture-3.md#base-prompt-scip-search-section-internalpromptsbuildergo-internalpromptstemplatesbase_prompttmpl

### Task 2: Agent Prompt scip-search Data Plumbing

**desc:** Agent prompt scip-search data plumbing: populate `prompts.BasePromptConfig` with successful available SCIP index records while building task-based and orchestrator prompts, using task worktree target roots for task roles and project root records already exposed by the runtime/orchestrator prompt-data boundary for orchestrators, degrading to an empty list when no successful indexes are available. Out of scope: prompt wording/template rendering, runtime index generation, lifecycle indexing call sites, Claude settings, README/operator documentation, generated `.claude/settings.json`, and wrapper or MCP abstractions for `scip-search`.

**done_when:** Agent prompt construction tests prove task-based prompt assembly queries or consumes available indexes for the resolved absolute task worktree path, orchestrator prompt assembly uses successful absolute project-root index records from the existing runtime/orchestrator prompt-data boundary, failed or missing language indexes are absent from `BasePromptConfig`, empty available-index results produce a prompt with no `scip-search` section, supplied successful index records reach the base prompt renderer with language, absolute path, and capability metadata intact, and unexpected available-index errors still return through the existing prompt-build error path.

**scope:** In scope: `internal/agent/prompt.go`, focused `internal/agent` tests, any minimal adapter between `internal/scipsearch` available-index records and `prompts.BasePromptConfig`, consumption of the runtime available-index query planned by `architecture-2-code-planning-0-coding-1`, and reuse or bridging of orchestrator project-root prompt data from `architecture-2-code-planning-3-coding-1`. Out of scope: `internal/prompts/templates/base_prompt.tmpl` wording changes beyond consuming Task 1 fields, runtime index generation, task-worktree creation wiring, submit-for-review regeneration, orchestrator `PreExecution` refresh wiring, `internal/embedded/claude-settings.json`, README/operator documentation, generated `.claude/settings.json`, and `.liza/agent-outputs/`.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-020422-architecture-3.md#agent-prompt-context-assembly-internalagentpromptgo

**depends_on:** Task 1

**task_depends_on:** architecture-2-code-planning-0-coding-1, architecture-2-code-planning-3-coding-1

### Task 3: Claude Settings scip-search Bash Permission

**desc:** Claude settings scip-search Bash permission: update the embedded master Claude settings so `permissions.allow` contains `Bash(scip-search:*)` while preserving existing `rg` and `ast-grep` permissions and leaving generated `.claude/settings.json` untouched.

**done_when:** Tests in `internal/embedded` prove `internal/embedded/claude-settings.json` is valid JSON, `permissions.allow` contains exactly one `Bash(scip-search:*)` entry, existing `Bash(rg:*)` and `Bash(ast-grep:*)` permissions remain present, and generated or merged Claude settings inherit the embedded `scip-search` permission without modifying a checked-in `.claude/settings.json`.

**scope:** In scope: `internal/embedded/claude-settings.json` and focused `internal/embedded` tests for JSON validity and permission presence. Out of scope: generated `.claude/settings.json`, hook scripts, README/operator documentation, prompt wording, runtime index generation, lifecycle indexing call sites, and `.liza/agent-outputs/`.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-020422-architecture-3.md#claude-settings-permission-internalembeddedclaude-settingsjson

## Dependency Graph

Task 1 and Task 3 can run in parallel.

Task 2 depends on Task 1 because it must populate the `BasePromptConfig` field shape and renderer contract introduced there. Task 2 also depends on existing concrete tasks `architecture-2-code-planning-0-coding-1` and `architecture-2-code-planning-3-coding-1` so it consumes the runtime available-index and orchestrator project-root prompt-data boundaries instead of duplicating them.

```text
Task 1 -> Task 2
Task 3
architecture-2-code-planning-0-coding-1 -> Task 2
architecture-2-code-planning-3-coding-1 -> Task 2
```

## Shared-File Audit

| File/Area | Task(s) | Dependency |
|---|---|---|
| `internal/prompts/builder.go` | Task 1 owns base prompt config fields and renderer helpers; Task 2 may only consume the fields from `internal/agent/prompt.go` | Task 2 depends on Task 1 |
| `internal/prompts/templates/base_prompt.tmpl` | Task 1 owns all prompt text and conditional rendering | None |
| `internal/prompts/builder_test.go` or equivalent prompt tests | Task 1 owns renderer/golden-style checks | None |
| `internal/agent/prompt.go` | Task 2 owns target-root available-index plumbing | Depends on `architecture-2-code-planning-3-coding-1` because that sibling also edits prompt data |
| `internal/agent/prompt_test.go` or equivalent agent tests | Task 2 owns prompt assembly integration checks | Depends on `architecture-2-code-planning-3-coding-1` for shared package state |
| `internal/scipsearch` | Task 2 consumes available-index APIs only; no runtime indexing behavior is owned here | Depends on `architecture-2-code-planning-0-coding-1` |
| `internal/embedded/claude-settings.json` | Task 3 owns the settings permission edit | None |
| `internal/embedded/embedded_test.go` or equivalent settings tests | Task 3 owns settings permission regression checks | None |
| README/operator docs and support docs | No task | Out of scope; covered by merged `architecture-4-code-planning-0` |
| `.claude/settings.json` | No task | Out of scope; derived file must not be edited |
| `.liza/agent-outputs/` | No task | Out of scope |

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | The prompt layer consumes successful available-index metadata instead of generating indexes or inferring paths. | Architecture-3 Context and Runtime Index Availability Source | Task 2 | Covered |
| 2 | Structured scip-search prompt data is added to base prompt construction rather than role-specific prompt blocks. | Architecture-3 Base Prompt scip-search Section key decisions | Task 1, Task 2 | Covered |
| 3 | If zero successful indexes are available, the base prompt omits the entire `scip-search` section. | Goal Required Agent Prompt Contract; Architecture-3 Constraints; assigned done_when (a) | Task 1, Task 2 | Covered |
| 4 | Rendered guidance includes each explicit absolute index path in `--index` examples and does not ask agents to infer index locations. | Goal MVP Scope; Goal Index Storage; Architecture-3 Interfaces; assigned done_when (b) | Task 1 | Covered |
| 5 | The command loop includes `scip-search symbols --index <absolute-path> --name ...`. | Goal Required Agent Prompt Contract; Goal Runtime Contract; assigned done_when (c) | Task 1 | Covered |
| 6 | The command loop includes `scip-search references --index <absolute-path> --symbol ... --location-only`. | Goal Required Agent Prompt Contract; Goal Runtime Contract; assigned done_when (c) | Task 1 | Covered |
| 7 | The command loop includes the file-open follow-up `nl -ba <result-path> | sed -n '<first-line>,<last-line>p'`. | Goal Required Agent Prompt Contract; Architecture-3 Base Prompt key decisions; assigned done_when (c) | Task 1 | Covered |
| 8 | Implementation lookup guidance appears only where language capability wording permits it. | Goal Required Agent Prompt Contract capability baseline; Success Criteria 24; assigned done_when (c) | Task 1 | Covered |
| 9 | Go guidance states symbols, references, and implementations are available. | Goal capability baseline; Architecture-3 Language Capability Wording | Task 1 | Covered |
| 10 | TypeScript guidance states symbols and references are available, and implementations are upstream-supported but not locally verified. | Goal Baseline evidence; Architecture-3 Language Capability Wording; assigned done_when (e) | Task 1 | Covered |
| 11 | Python guidance states symbols are available, references may be incomplete, and `scip-search implementations` is not supported for Python. | Goal Required Agent Prompt Contract; Goal capability baseline; Success Criteria 20; assigned done_when (d) | Task 1 | Covered |
| 12 | Python prompt output does not render a Python implementations command example. | Goal Required Agent Prompt Contract; assigned done_when (d) | Task 1 | Covered |
| 13 | Prompt text states indexes are snapshots and will not reflect subsequent agent edits. | Goal Required Agent Prompt Contract; Success Criteria 21; assigned done_when (f) | Task 1 | Covered |
| 14 | Rendered indexes use deterministic milestone language order: Go, TypeScript, Python. | Architecture-3 Base Prompt scip-search Section key decisions | Task 1 | Covered |
| 15 | Task-based prompt assembly uses the resolved absolute task worktree path when reading available indexes. | Architecture-3 Agent Prompt Context Assembly; Architecture-3 Task-based agent flow | Task 2 | Covered |
| 16 | Orchestrator prompt assembly uses project-root successful index records and does not use task worktree index paths. | Architecture-3 Agent Prompt Context Assembly; Architecture-3 Orchestrator flow; architecture-2-code-planning-3 Task 2 | Task 2 | Covered |
| 17 | Missing or failed language indexes are omitted from prompt data without adding prompt warnings or failure diagnostics. | Goal Required Agent Prompt Contract; Success Criteria 23; Architecture-3 Runtime Index Availability Source | Task 2 | Covered |
| 18 | Unexpected prompt-context errors that are not absence of indexes still propagate through the existing prompt-build error path. | Architecture-3 Agent Prompt Context Assembly key decisions | Task 2 | Covered |
| 19 | `internal/embedded/claude-settings.json` allows agents to run `scip-search`. | Goal MVP Scope; Success Criteria 11; Architecture-3 Claude Settings Permission; assigned done_when (g) | Task 3 | Covered |
| 20 | The settings edit updates the embedded master file, not generated `.claude/settings.json`. | Architecture-3 Constraints; settings-master-not-derived lesson | Task 3 | Covered |
| 21 | Existing `rg` and `ast-grep` permissions remain because `scip-search` complements them. | Architecture-3 Claude Settings Permission key decisions; Goal MVP Scope | Task 3 | Covered |
| 22 | The implementation avoids init-time validation, language allowlist persistence, SCIP index generation, lifecycle indexing wiring, README/operator docs, generated `.claude/settings.json`, and wrapper/MCP abstractions. | Assigned SCOPE; Goal Explicit Out of Scope | Task 1, Task 2, Task 3 | Covered |
| 23 | The implementation remains stack-agnostic and avoids Liza-specific target-project commands. | GUARDRAILS.md G1.1; Architecture-3 Cross-Cutting Concerns | Task 1, Task 2, Task 3 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | Task 2: prompt-construction integration tests exercise the spawned-agent prompt path without requiring lifecycle index generation, which is out of scope. | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: operator documentation is covered by merged `architecture-4-code-planning-0`, and this assigned scope explicitly excludes README/operator documentation. | N/A |

## Validation Plan

Each coding task should validate its own behavioral surface with focused Go tests:

- Task 1: `go test ./internal/prompts -run 'Test.*Scip.*(Prompt|Base|Index|Capability)'`
- Task 2: `go test ./internal/agent ./internal/prompts -run 'Test.*Scip.*(Prompt|Context|Index|Base)'`
- Task 3: `go test ./internal/embedded -run 'Test.*Scip.*(Settings|Permission)|Test.*ClaudeSettings.*'`

The final task in any dependency chain should also run package-level tests for touched packages and pre-commit on touched files. If broad Go validation is needed in this worktree, follow `lessons/agents/worktree-build-prerequisites.md`.

## Pre-Submit Self-Check

- Task decomposition: three coding tasks, each with one observable implementation intent.
- Shared-file audit: Task 2 depends on Task 1 for shared prompt config shape; Task 3 is independent.
- Dependency consistency: Task 2 waits for the concrete runtime available-index and orchestrator project-root prompt-data tasks instead of duplicating those responsibilities.
- Scope boundaries: no init validation, language allowlist persistence, SCIP index generation, lifecycle indexing call-site wiring, README/operator docs, generated `.claude/settings.json`, wrapper/MCP abstraction, or `.liza/agent-outputs/` changes are planned.
- Cross-references: every responsibility named in the compliance matrix is owned by Task 1, Task 2, Task 3, or explicitly marked out of scope/N/A.
