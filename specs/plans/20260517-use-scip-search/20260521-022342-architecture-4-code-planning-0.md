# Code Plan: scip-search Operator Documentation

Task ID: `architecture-4-code-planning-0`

Source artifacts:
- Goal spec: `specs/goals/20260517-use-scip-search.md`
- Architecture reference: `specs/arch-plan/20260517-use-scip-search/20260521-021340-architecture-4.md`

## Intent

Plan the operator-facing documentation implementation for `scip-search` as a highly recommended, external, strict opt-in MAS navigation tool without expanding into runtime code, init validation, prompt rendering, Claude settings, automatic installation, or generated runtime log state.

## Planned Coding Tasks

### Task 1: README scip-search Recommended Tool and Setup Pointer

**desc:** README scip-search recommended-tool and setup guidance: update README.md so public first-read documentation lists scip-search as a highly recommended external MAS navigation tool, preserves ast-grep as complementary structural pattern/rewrite search, and points readers to support-docs/CONFIGURATION.md for detailed opt-in setup.

**done_when:** Documentation checks prove README.md contains a Recommended Tools entry for scip-search using highly recommended MAS navigation language, keeps the ast-grep row as complementary structural search/rewrite guidance, states Liza does not install scip-search or language indexers automatically, shows or links the LIZA_ENABLE_SCIP_SEARCH opt-in plus repeated --scip-search <language> setup path, and does not describe scip-search as required for all Liza modes.

**scope:** In scope: README.md recommended-tool table and compact MAS setup guidance for scip-search, including the external-tool warning, ast-grep complement, LIZA_ENABLE_SCIP_SEARCH opt-in pointer, repeated --scip-search <language> example or link, and support-docs/CONFIGURATION.md reference. Out of scope: support-docs content, docs/ stubs, init validation code, runtime indexing code, prompt rendering, Claude settings, automatic installation, and .liza/agent-outputs/.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-021340-architecture-4.md#readme-recommended-tool-positioning-readmemd

**depends_on:** none

### Task 2: Configuration Reference for scip-search Activation and Indexers

**desc:** Configuration reference for scip-search activation and indexers: update support-docs/CONFIGURATION.md so operators have the canonical details for LIZA_ENABLE_SCIP_SEARCH, config.scip_search, repeated --scip-search <language>, supported language indexers, index storage, graceful omission, and explicit non-goals.

**done_when:** Documentation checks prove support-docs/CONFIGURATION.md documents LIZA_ENABLE_SCIP_SEARCH truthy values 1 and true, false values unset, empty, 0, and false, case-insensitive trimmed parsing, strict opt-in behavior, config.scip_search as a durable language allowlist that does not activate MAS indexing by itself, repeated --scip-search <language> setup with supported values go, typescript, and python, auto-detection when no explicit language is supplied and the env gate is truthy, scip-go/scip-typescript/scip-python as separately installed external indexers, task indexes under <worktree>/.liza/scip/, project-root indexes under <project_root>/.liza/scip/, snapshot semantics, graceful runtime omission on indexing failure, and explicit non-goals saying Liza does not build, vendor, auto-install, daemonize, watch, cache, or wrap scip-search or its language indexers; docs/CONFIGURATION.md remains a stub pointing to support-docs/CONFIGURATION.md.

**scope:** In scope: support-docs/CONFIGURATION.md scip-search environment variable, config, setup, indexer prerequisite, index storage, graceful degradation, and non-goal documentation; validation that docs/CONFIGURATION.md remains a pointer stub. Out of scope: editing docs/CONFIGURATION.md unless the stub is accidentally broken by the task, support-docs/CUSTOMIZING_AGENT_TOOLS.md, support-docs/USAGE_MULTI_AGENTS.md, README.md, init/runtime code, prompt rendering, Claude settings, automatic installation, and .liza/agent-outputs/.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-021340-architecture-4.md#configuration-reference-support-docsconfigurationmd

**depends_on:** none

### Task 3: Agent Tool Customization Guidance for scip-search

**desc:** Agent tool customization guidance for scip-search: update support-docs/CUSTOMIZING_AGENT_TOOLS.md so MAS operators understand where scip-search fits alongside rg and ast-grep for worktree-safe navigation when Liza supplies explicit SCIP index paths.

**done_when:** Documentation checks prove support-docs/CUSTOMIZING_AGENT_TOOLS.md positions scip-search as the preferred MAS tool for indexed symbol, package, reference, and implementation navigation when Liza supplies an explicit --index path, keeps rg for text and path search, keeps ast-grep for syntax-pattern structural search and rewrite workflows, warns agents should not search for default indexes or rely on daemon/global/cache behavior, and docs/CUSTOMIZING_AGENT_TOOLS.md remains a stub pointing to support-docs/CUSTOMIZING_AGENT_TOOLS.md.

**scope:** In scope: support-docs/CUSTOMIZING_AGENT_TOOLS.md multi-agent tool guidance for scip-search, rg, ast-grep, explicit --index paths, worktree-safety, and non-daemon/non-cache expectations; validation that docs/CUSTOMIZING_AGENT_TOOLS.md remains a pointer stub. Out of scope: editing docs/CUSTOMIZING_AGENT_TOOLS.md unless the stub is accidentally broken by the task, support-docs/CONFIGURATION.md, support-docs/USAGE_MULTI_AGENTS.md, README.md, runtime prompt text, Claude settings, tool installation, and .liza/agent-outputs/.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-021340-architecture-4.md#agent-tool-customization-guidance-support-docscustomizing_agent_toolsmd

**depends_on:** none

### Task 4: MAS Usage Pointer for scip-search Setup

**desc:** MAS usage pointer for scip-search setup: update support-docs/USAGE_MULTI_AGENTS.md with a concise optional-but-highly-recommended scip-search setup pointer that sends MAS operators to support-docs/CONFIGURATION.md for the detailed activation and indexer contract.

**done_when:** Documentation checks prove support-docs/USAGE_MULTI_AGENTS.md includes a concise MAS setup pointer that calls scip-search optional but highly recommended for repository-navigation-heavy MAS runs, names LIZA_ENABLE_SCIP_SEARCH as the activation gate, links to support-docs/CONFIGURATION.md for config.scip_search, repeated --scip-search <language>, indexer prerequisites, and .liza/scip/ snapshot details, and does not duplicate the full configuration reference.

**scope:** In scope: support-docs/USAGE_MULTI_AGENTS.md quick-start/setup wording that points operators to the canonical configuration reference for scip-search. Out of scope: support-docs/CONFIGURATION.md, support-docs/CUSTOMIZING_AGENT_TOOLS.md, README.md, docs/ stubs, init/runtime code, prompt rendering, Claude settings, automatic installation, and .liza/agent-outputs/.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-021340-architecture-4.md#mas-usage-setup-notes-support-docusage_multi_agentsmd

**depends_on:** none

## Dependency Graph

All four tasks can run in parallel. Each task owns a distinct documentation file, and cross-links should target stable file paths rather than newly created section anchors when possible.

## Shared-File Audit

| File/Area | Task(s) | Dependency |
|---|---|---|
| `README.md` | Task 1 | None |
| `support-docs/CONFIGURATION.md` | Task 2 | None |
| `support-docs/CUSTOMIZING_AGENT_TOOLS.md` | Task 3 | None |
| `support-docs/USAGE_MULTI_AGENTS.md` | Task 4 | None |
| `docs/CONFIGURATION.md` | Task 2 validates stub only | None; no planned edit |
| `docs/CUSTOMIZING_AGENT_TOOLS.md` | Task 3 validates stub only | None; no planned edit |
| `.liza/agent-outputs/` | No task | Out of scope |

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Document `scip-search` as a highly recommended Liza MAS tool for token-efficient repository navigation. | MVP Scope lines 61-62; Success Criteria lines 345-346 | Task 1 | Covered |
| 2 | Document `LIZA_ENABLE_SCIP_SEARCH` as the strict opt-in MAS activation gate, including that unset or false disables indexing and prompt injection even when `config.scip_search` exists. | MVP Scope lines 65-67; Configuration Shape lines 195-197; Success Criteria lines 328-329 | Task 1, Task 2, Task 4 | Covered |
| 3 | Document repeated `liza init --spec --scip-search <language>` setup for explicit enabled-language selection. | MVP Scope lines 68-69; Configuration Shape lines 173-177; Success Criteria line 330 | Task 1, Task 2, Task 4 | Covered |
| 4 | Document auto-detection behavior when no `--scip-search` option is supplied and `LIZA_ENABLE_SCIP_SEARCH` is truthy. | MVP Scope lines 70-72; Configuration Shape lines 179-181; Success Criteria lines 335-337 | Task 2 | Covered |
| 5 | Document `config.scip_search` as the language allowlist used with the env gate for MAS task-worktree and project-root indexing. | Configuration Shape lines 187-204; Success Criteria lines 340-342 | Task 2, Task 4 | Covered |
| 6 | Document supported first-milestone languages and external language indexer responsibility for Go, TypeScript, and Python. | MVP Scope lines 86-87; Indexer Requirements lines 245-252 | Task 2, Task 4 | Covered |
| 7 | Preserve `ast-grep` as the complementary tool for pattern-based structural search while positioning `scip-search` for indexed symbol/reference/package navigation. | MVP Scope lines 97-100; Query Model lines 303-306 | Task 1, Task 3 | Covered |
| 8 | Document generated task indexes under `<worktree>/.liza/scip/` and generated project-root indexes under `<project_root>/.liza/scip/`. | MVP Scope lines 88-90; Index Storage lines 262-276; Success Criteria lines 355-357 | Task 2, Task 4 | Covered |
| 9 | Document snapshot semantics and generated-index ownership so operators know task indexes are not live-updated after agent edits. | Index Storage lines 281-287; Success Criteria line 365; Risks lines 399-400 | Task 2, Task 4 | Covered |
| 10 | Document graceful failure behavior: runtime indexing failures omit failed indexes from prompts without blocking agent or reviewer spawn. | Required Agent Prompt Contract lines 128-131; Behavioral Decisions lines 235-238; Success Criteria lines 367-368 | Task 2 | Covered |
| 11 | Keep documentation clear that Liza does not build, vendor, auto-install, daemonize, watch, cache, or wrap `scip-search` or its language indexers. | Goal lines 42-51; Explicit Out of Scope lines 371-380 | Task 1, Task 2, Task 3 | Covered |
| 12 | Keep canonical operator guidance in `support-docs/` and leave `docs/CONFIGURATION.md` and `docs/CUSTOMIZING_AGENT_TOOLS.md` as stubs pointing to canonical content. | Architecture Constraints lines 35-36; Documentation Consistency Boundary lines 118-129 | Task 2, Task 3 | Covered |
| 13 | Add a concise `support-docs/USAGE_MULTI_AGENTS.md` pointer for MAS setup readers without duplicating the full configuration reference. | Architecture MAS Usage Setup Notes lines 106-115 | Task 4 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: documentation-only plan; implementation tasks should use focused documentation content checks rather than product e2e tests. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | Task 1, Task 2, Task 3, Task 4 | Covered |

## Validation Plan

Code tasks should validate with targeted documentation checks, for example:

- `rg -n 'scip-search|highly recommended|ast-grep|LIZA_ENABLE_SCIP_SEARCH|--scip-search' README.md`
- `rg -n 'LIZA_ENABLE_SCIP_SEARCH|config.scip_search|scip-go|scip-typescript|scip-python|<worktree>/.liza/scip|<project_root>/.liza/scip|build|vendor|auto-install|daemon|watch|cache|wrap' support-docs/CONFIGURATION.md`
- `rg -n 'scip-search|--index|rg|ast-grep|daemon|cache|default indexes' support-docs/CUSTOMIZING_AGENT_TOOLS.md`
- `rg -n 'scip-search|LIZA_ENABLE_SCIP_SEARCH|CONFIGURATION.md' support-docs/USAGE_MULTI_AGENTS.md`
- `rg -n 'support-docs/CONFIGURATION.md' docs/CONFIGURATION.md`
- `rg -n 'support-docs/CUSTOMIZING_AGENT_TOOLS.md' docs/CUSTOMIZING_AGENT_TOOLS.md`

Each documentation task should run pre-commit on its touched files. If support-doc embedding checks require generated copies to match and generated copies are materialized, use the repository's existing sync mechanism instead of hand-editing generated content.

## Pre-Submit Self-Check

- Task decomposition: four single-file documentation tasks, each with one observable documentation intent.
- Shared-file audit: no file is edited by more than one task.
- Scope boundaries: no init validation code, runtime indexing code, prompt rendering, Claude settings, automatic installation, docs stub content edits, or `.liza/agent-outputs/` changes are planned.
- Cross-references: each task heading owns the responsibilities referenced in the matrix and shared-file audit.
