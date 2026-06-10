# Liza: Hardened Multi-Agent Coding

> Because *"it worked in the demo"* is not what on-call engineers are looking for.

The full **[hardening inventory](docs/liza-hardened-mas.md)** to push to production with peace of mind.

![Liza's TUI](docs/img/liza-tui.png)

**[Demo video](https://drive.google.com/drive/folders/1Iea-nNxAazBHeLXL7IElXnG5r1i1E-Ha?usp=sharing)** (45min).

[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/liza-mas/liza)

## Table of Contents

- [What is Liza?](#what-is-liza)
- [How Liza Compares](#how-liza-compares)
- [Getting Started](#getting-started)
- [Architecture](#architecture)
- [Status](#status)
- [Naming](#naming)
- [License](#license)

## What is Liza?

Liza is simultaneously a **Pairing** and **Multi-Agent System** (MAS)
optimized for **doing things right on the first pass** — with the auditability to prove it.
Liza bets on time-to-quality and durable codebase maintainability through automated reviews and documentation
(e.g. the [ADR Backfill](skills/adr-backfill) skill).

Liza's behavioral contract — used by both modes — **makes models more thoughtful**:
> *"I want to wash my car. The car wash is 100 meters away. Should I walk or drive?"<br>*
>
> Sonnet 4.6: *"Walk. Driving 100 meters to a car wash defeats the purpose — you'd barely get the car dirty enough to justify the trip, and parking/maneuvering takes longer than the walk itself."*
>
> Sonnet 4.6 **with Liza's contract**: *"Drive. You're already going to a car wash — arriving dirty is the point."*

Liza is a **frontier Multi-Agent System**:
> Soufiane Keli (Executive Director, IBM) maps AI engineering maturity across 5 levels,
> from autocomplete (L1) to software factory (L5, still theoretical). He places Liza at L4 – Collaborative Agent Networks:
> <br>
> *"Multiple specialized agents work together on design, code, testing, and deployment. Humans orchestrate. This is typically
> what's happening with BMAD, BEADS, and LIZA. Very few organizations have genuinely reached this level in 2026."*

### Main characteristics:

- **Behavior, Posture, Know-How** — three layers that make coding agents useful:
  - **Behavior**: A [behavioral contract](contracts/) enforces governance intrinsically — not through external scaffolding as *Harness Engineering* does. Optional project [guardrails](GUARDRAILS.md) extend the contract with project-specific constraints.
  - **Posture**: Original pairing postures (User Duck, Socratic Coach, Challenger, etc.)
  - **Know-How**: composable [skills](skills/) encode methodology
  - *[Full analysis](https://medium.com/@tangi.vass/behavior-posture-know-how-the-three-layers-that-make-ai-agents-useful-d485388442eb)*
- **Autonomous Spec-driven Coding System:**
  - From **general goal** to code and tests, with multi-stage decomposition into intermediate artifacts (epics, US, implementation plans)
    that are AI generated but human reviewed.
  - Automatic task decomposition based on complexity with dependency management for parallel execution. Many-to-one transitions consolidate sibling tasks (e.g. N user stories → 1 architecture task).
  - Multi-sprints: agents are fully autonomous within a sprint, user steers between sprints via Liza CLI - review of produced artifacts, continuous improvement, and steering of the next sprint
  - A TUI (`liza tui`) displays live system state and lets you spawn agents, pause/resume, add tasks, and trigger checkpoints.
- **Adversarial architecture:**
  - One Orchestrator role + 12 others across four pipeline phases.
  - Every activity is dual — a doer and a reviewer: epic planning, epic writing, US writing, code planning, coding - everything.
  - They interact like on a PR review — submission, feedback comments, verdict, revised submission, etc. — until approval.
- **Hybrid hardened architecture:**
  - LLM agents wrapped by code-enforced supervisors and working on isolated git worktrees.
  - The supervisor does the **deterministic code-enforced actions** (worktree management, merges, TDD enforcement, etc),
    leaving the **judgment to the agent**. Strict task state machine with 43+ validation rules.
  - Agents communicate and act through Liza's **CLI**.
  - 35k LOC of Go (+92k of tests). Liza is not a prompt collection.
  - Agent logs and prompts recording for automatic analysis and continuous improvements (token optimization, tool usage analysis, context quality, ...). The `/liza-logs` skill cross-correlates logs across agents to identify frictions — from misconfiguration in early setups to regressions from provider CLI updates in mature ones. The `/context-engineering` skill audits prompt payload shape, context bloat, cacheability, and handoff fit.
- **Multi-model:**
  - Liza wraps provider **CLIs**, not their APIs. This means your existing subscription (Claude Max, ChatGPT Pro, etc.) works — no API keys or per-token billing required — and your personal setup is used.
  - BYOM: Claude Code, Codex CLI, OpenCode, Kimi, Mistral, Gemini. [Not all are made equal though](docs/demo-benchmark).
- **Structured workflow:**
  - Defined as a composable and customizable YAML pipeline with declarative sub-pipelines (e.g. specification, coding).
  - Coordination is performed via an auditable YAML **blackboard** that acts as both the Kanban board of the agents with full historized state details and the support for PR-like comments made by the reviewer agents.
  - Agents don't discover work — they receive pre-claimed tasks in bootstrap prompt. Eliminates race conditions and cognitive overhead.
- **Resilience:**
  - Circuit breaker: pattern detection (loops, repeated failures) triggers automatic sprint checkpoint
  - Crash recovery: `recover-agent` and `recover-task` commands for idempotent cleanup after hard crashes
  - Context handoff: agents hand off with structured notes when approaching context limits

See the complete [vision](<specs/build/1 - Vision.md>) and [genesis](docs/how-liza-grew-up.md) of Liza.

### What it looks like in practice

Without the contract, an agent that hits a problem it can't solve has two options: admit failure or fake progress. Its training overwhelmingly favors the second. **Faking progress feels collaborative** — *look, I'm trying things!*

So it spirals. Random changes dressed up as hypotheses. Each iteration more elaborate, more confident, more wrong. You watch the diff grow and wonder if any of this is moving toward a solution. If you're clever, you end up reverting.

Under the contract, there's a third option: **say "I'm stuck" and mean it.** The contract makes that safe — no penalty for uncertainty, no pressure to perform progress. And the Approval Request mechanism forces agents to write down their reasoning before acting. *"I'll try random things until something works"* is hard to write in a structured plan. Surface the reasoning, and the reasoning improves — no better model required.

The shift is visible in tone too. Agents under the contract stop sounding like enthusiastic, consensus-seeking assistants. They become more like senior peers — direct style, actual opinions, willing to push back.

This won't self-correct. Sycophancy drives engagement — that's what gets optimized. Acting fast with little thinking controls inference costs. Model providers optimize for adoption and cost efficiency, not engineering reliability.

Ten months of pairing under this contract, and the vigilance tax dropped to near zero. I can mostly focus on the architecture and more specifically build up a MAS upon the contract.

Here is a [demo video](https://drive.google.com/drive/folders/1Iea-nNxAazBHeLXL7IElXnG5r1i1E-Ha?usp=sharing) of an implementation of a basic Todo CLI
using Liza in Multi-agent mode - spec-driven with intermediate epic and User Story creation, fully autonomous agents within sprints, human reviews between sprints.

## How Liza Compares

### MAS Architecture

The multi-agent coding space splits into six categories:

- **Orchestration frameworks** (CrewAI, LangGraph, AutoGen) — general-purpose multi-agent building blocks; none address behavioral trust in software engineering.
- **Company simulators** (MetaGPT, ChatDev) — SOP-based pipelines mimicking software teams; trust assumed through process compliance.
- **Scheduler/runners** (Symphony, Paperclip) — work dispatch and workspace isolation above coding agents; trust delegated to whatever happens inside each session.
- **Context-engineered systems** (GSD) — thin orchestrators spawn fresh subagents for every operation to prevent "context rot"; trust derives from context freshness plus spec-driven process, not behavioral enforcement.
- **Methodology / workflow frameworks** (BMAD-METHOD) — multi-phase agile methodology installed into AI IDEs (Claude Code, Cursor, Codex, Copilot); trust via structured process and context engineering, not mechanical enforcement.
- **Behavioral enforcement** (Liza) — deterministic supervisors enforce state transitions, role boundaries, and merge authority mechanically; agents handle judgment under a behavioral contract addressing 55+ failure modes.

| | Liza | BMAD | CrewAI | Ruflo | Symphony | Paperclip |
|---|---|---|---|---|---|---|
| **Trust approach** | Behavioral contract (55+ failure modes) | Prompt-level three-layer adversarial review (advisory) | Post-hoc output validation | Track-record based (Q-learning) | Implementation-dependent | Budget/approval governance |
| **Review loop** | Adversarial doer/reviewer pairs | 3 parallel reviewers (Blind Hunter / Edge Case / Acceptance) | Optional manager mode | None | None | None |
| **Role enforcement** | Code-enforced (Go supervisor) | Prompt-level (6 named personas) | Prompt suggestion | Claude hooks (provider-specific) | None (single-agent) | Org chart hierarchy |
| **Failure handling** | Structural prevention + escalation | `bmad-correct-course` + readiness gate (PASS/CONCERNS/FAIL) | Retry on output failure | Pattern matching from past successes | Implementation-dependent | Budget auto-pause |

**Where Liza leads** — no competitor offers any of these:
- Failure mode catalog (55+) with mechanical countermeasures
- Adversarial doer/reviewer pairs on every task
- Code-enforced role boundaries (Go supervisor, not prompt suggestions)
- Provider compliance matrix tested empirically across 5 providers
- Multi-sprint continuity, crash recovery, context pressure management

**Where others lead:**
- **Ecosystem**: CrewAI (45k stars, production v1.9.0, enterprise product), MetaGPT (64k stars), and BMAD (~45.2k stars, Discord, 5-language docs, corporate sponsorship) have far larger communities
- **Upstream planning**: BMAD covers brainstorming, market research, PRFAQ, PRD interviews, and UX design — breadth Liza's lighter goal-document entry point doesn't match
- **Cost tracking**: Paperclip ships per-agent/task/project budgets today; Liza's is planned
- **Flexibility**: CrewAI works for any domain; Liza is software-engineering-only

### Spec-Driven Process

Spec-driven development is becoming the standard approach for AI coding. Most tools differ in *what altitude* they expect the input at and *who owns product decisions*.

| | Liza                                              | BMAD | Spec Kit | OpenSpec | Kiro | GSD |
|---|---------------------------------------------------|---|---|---|---|---|
| **Input level** | High-level goal (problem, users, behavior, scope) | Full lifecycle (brainstorming → PRFAQ → PRD → Architecture → Stories) | High-level goal → agent-generated spec | Detailed delta-specs on existing system | Interactive 3-doc generation | Detailed spec required |
| **Who decides what to build** | Human via pairing (Coach/Challenger modes)        | Human via conversational PM-agent interview | Agent generates, human approves | Human (spec pre-decided) | Agent drives, human confirms | Human (pre-written) |
| **Decomposition** | Orchestrator decomposes into adversarial tasks    | Phase workflows produce artifacts (PRD → Architecture → Epics → Stories) | Agent decomposes spec into tasks | Slash commands structure tasks | Agent decomposes from spec | Planner sizes to context budget |
| **Review** | Doer/reviewer pairs with quorum                   | Three parallel reviewers at code stage (prompt-level, advisory) | None | Advisory (verify warns, doesn't block) | None (single-agent) | Checker + verifier (not adversarial) |

Most tools either expect the detailed spec already done (OpenSpec, GSD) or have the agent write it (Spec Kit, Kiro, MetaGPT). BMAD spans the broadest altitude range — from brainstorming and PRFAQ at the top through stories and code review at the bottom — but relies on the PM agent interviewing the human conversationally across every workflow. Liza treats goal-setting as a synchronous human-agent collaboration where the human makes product decisions and the agent helps surface gaps — then enforces those decisions mechanically during autonomous pipeline execution.

The positioning question is not "who starts highest" but "what's the minimum human input that reliably produces working code." BMAD answers with iterative PM-agent interviews; Liza answers with one front-loaded goal doc, then mechanical pipeline execution. A ~200-line goal document describing the "Diagnosis Design" method has been sufficient to produce a complete three-tier application (FastAPI backend, Go CLI, React web UI) in a single Liza run, with human intervention limited to answering questions (checkpoint-summary skill) between goal and merged code; the supporting run artifacts are in a non-public Diagnosis Design repo.

**Rule of thumb: agents may make implementation choices but not product decisions.** The [goal document](support-docs/how-to-produce-a-goal.md) is where every product decision lives. The goal-setting phase uses pairing (Coach mode for surfacing WHY, Challenger mode for stress-testing WHAT) because this phase has the highest decision density — every ambiguity resolved here prevents wrong turns downstream.

[Full competitive survey →](specs/architecture/mas-survey.md)

---

## Getting Started

Start with [GETTING_STARTED.md](GETTING_STARTED.md) for the installation and
setup path: install the `liza` binary, run `liza setup`, customize
`AGENT_TOOLS.md`, initialize a project with `liza init`, and choose Pairing or
Multi-Agent mode.

Mode-specific guides:

- **Pairing**: [Pairing Usage](support-docs/USAGE_PAIRING.md) — human-agent collaboration under contract
- **Adversarial Pairing**: [Adversarial Pairing](support-docs/ADVERSARIAL_PAIRING.md) — one doer plus reviewer sessions through a shared Markdown blackboard
- **Multi-Agent (Liza)**: [Multi-Agent Usage](support-docs/USAGE_MULTI_AGENTS.md), then try the [Demo](docs/DEMO.md)
- **Reference**: [Configuration](support-docs/CONFIGURATION.md) · [Recipes](docs/RECIPES.md) · [Troubleshooting](support-docs/TROUBLESHOOTING.md)

### Recommended Tools

Liza optimizes cost-to-quality, not cost-to-lets-cross-fingers. These tools reduce token usage without sacrificing output quality:

| Tool | What it does | Impact |
|------|-------------|--------|
| [RTK](https://github.com/rtk-ai/rtk) | CLI proxy that compresses tool output (git, go, pytest, ...) — ~90% token savings on command results | Fewer tokens per tool call, more budget for reasoning |
| [stacklit-cli](https://github.com/liza-mas/stacklit-cli) | Compact codebase index — modules, dependencies, hot files, workflow hints | Low-token repo map before targeted reads; surfaces symbol names that scip-search can trace precisely |
| [Semble](https://github.com/MinishLab/semble/) | Optional semantic discovery and semantic repository search for natural-language code, docs, and config questions | Finds candidate chunks before exact symbols are known; direct source reads still provide evidence |
| [scip-search](https://github.com/liza-mas/scip-search/) | Precise SCIP navigation — symbols, references, implementations, packages, and static graph/impact hints | Saves agent tokens on symbol and dependency lookups in worktrees; pairs with Stacklit for orient-then-trace workflows |
| [functional-clusters](https://github.com/liza-mas/functional-clusters) | Advisory functional capability clusters from SCIP graph exports and Stacklit architecture exports | Helps agents inspect likely feature boundaries and cross-cluster dependencies; source reads remain evidence |
| [ast-grep](https://ast-grep.github.io/) | Complementary AST-aware structural pattern search/rewrite — matches code structure, not text | Finds patterns indexes cannot express (function signatures, call shapes, nested expressions) |
| [mdtoc](https://github.com/liza-mas/mdtoc) | Highly recommended for MAS Markdown navigation: prints per-file section line ranges and `mdq` selectors | Saves agent tokens by mapping long specs/plans before reading only the relevant section |
| [MorphLLM MCP](https://www.morphllm.com/) (WarpGrep) | Fast Apply edits via `// ... existing code ...` placeholders + semantic codebase search | Avoids reading full files into context for edits |
| [jq](https://jqlang.github.io/jq/) / [yq](https://github.com/mikefarah/yq) | Query and extract fields from JSON / YAML / TOML | Avoids reading full structured data files into context |
| [GitHub CLI](https://cli.github.com/) | GitHub issues, PRs, releases, and API access from the shell | Avoids raw API calls and keeps GitHub workflows authenticated and structured |
| [filesystem MCP](https://github.com/anthropics/anthropic-quickstarts/tree/main/mcp-filesystem) | Bulk file operations — multi-file reads, recursive directory trees, file metadata | Batch reads in one call instead of sequential Read tool calls |
| [perplexity](https://github.com/perplexityai/modelcontextprotocol/) | Current-info web search with synthesis | Lower-context discovery for external libraries, unfamiliar tech, and current information |
| [context7](https://context7.com/docs/resources/all-clients) | Structured API reference lookup with examples | High-signal library/API docs with consistent formatting |
| [Ref](https://ref.tools/) | Broad documentation and guide search | Better coverage for tutorials, niche libraries, and how-to material |
| [fetch MCP](https://github.com/modelcontextprotocol/servers/tree/main/src/fetch) | Exact web page retrieval | Raw HTML, pagination, and precise page content without summarization |
| [deepwiki](https://docs.devin.ai/work-with-devin/deepwiki-mcp) | Repository architecture and code-structure exploration | Fast high-level orientation on unfamiliar repositories |
| [postgres](https://github.com/modelcontextprotocol/servers#using-an-mcp-client) | Read-only SQL exploration and validation | Direct schema and data inspection when a database MCP is available |
| [claude-usage](https://github.com/phuryn/claude-usage) | Tracks Claude subscription usage with cost breakdown | Visibility into where tokens go — essential for optimizing agent configurations |

These tools are referenced in the default `~/.liza/AGENT_TOOLS.md`; see
[Customizing AGENT_TOOLS.md](support-docs/CUSTOMIZING_AGENT_TOOLS.md).
Liza does not install them, so install the ones you intend to use, and remove or replace the others in `AGENT_TOOLS.md` to match your environment.

**`.claudeignore`** — Claude Code reads all files on disk, including git-tracked ones it doesn't need. Add a `.claudeignore` at your project root (same syntax as `.gitignore`) to keep irrelevant content out of the context budget. Liza ships one by default; review and adapt it to your project. Common candidates:

- **Untracked local files**: `claude.env`, `.mcp.json`, build caches, backup directories
- **Tracked but useless to Claude**: lock files (`package-lock.json`, `go.sum`), generated changelogs, historical SQL migrations
- **Large test fixtures**: JSON/CSV data files committed for tests
- **Generated documentation**: auto-generated `docs/` that duplicates what Claude can infer from source
- **Git submodules**: tracked but no reason for Claude to explore external dependencies

---

## Architecture

![Liza](docs/img/liza-illustration.png)

Most spec-driven multi-agent systems are LLM-all-the-way-down: agents coordinating agents, with compliance dependent on
prompt adherence and artifact-based workflows.

Liza is a hybrid system:
- The agents are the popular coding agent CLIs.
- The workflow is declarative but relies on a code-enforced state machine
- The supervisors that wrap every agent and the validation rules are also deterministic Go code.
  This means critical invariants — state transitions, role boundaries, merge authority, TDD gates — are enforced
  mechanically, not by asking a LLM to please follow rules.
  Liza's mechanical layer cannot fabricate, cannot skip gates, cannot interpret rules flexibly.
- The LLM side is equally differentiated. Liza agents operate under a behavioral contract: 55+ documented
  LLM failure modes each mapped to a specific countermeasure, an explicit state machine
  with forbidden transitions, and tiered rules that define what degrades gracefully
  versus what never bends.

Reliability is built into every component.

```mermaid
graph TB
    H["User"] -->|commands| CLI["Go CLI · <i>liza</i>"]
    AP["Doer / Reviewer LLM Agent Pairs · <small>judgment layer</small>"]
    CLI -->|spawns| S["Supervisor · <small>deterministic Go</small>"]

    CLI --> BB["YAML Blackboard<br><small>state.yaml</small>"]
    CLI --> WT["Git Worktrees<br><small>isolated workspaces</small>"]

    S -->|wraps| AP
    PL["YAML Pipeline & Roles"] --> |specializes| S
    S --> PB
    BC["Behavioral Contract"] -->|harness| AP
    PB["Prompt Builder"] -->|bootstrap prompt| AP
    SK["Skills"] -->|empowers| AP
    SP["Specs"] <-->|drives / produces| AP
    AP -->|calls| CLI

    style CLI fill:#4a90d9,stroke:#2c5ea0,color:#fff
    style S fill:#4a90d9,stroke:#2c5ea0,color:#fff
    style AP fill:#e8833a,stroke:#c0652a,color:#fff
    style PB fill:#5bb87d,stroke:#3d8a5a,color:#fff
    style BC fill:#5bb87d,stroke:#3d8a5a,color:#fff
    style SK fill:#5bb87d,stroke:#3d8a5a,color:#fff
    style SP fill:#5bb87d,stroke:#3d8a5a,color:#fff
    style BB fill:#b0b8c4,stroke:#8a929e,color:#333
    style WT fill:#b0b8c4,stroke:#8a929e,color:#333
    style PL fill:#b0b8c4,stroke:#8a929e,color:#333
```

Roles aren't composable, Skills are: agents aren't constrained regarding their capabilities by a rigid "Act as a..." prompt
and may use any skill they consider relevant to adapt to the situation.

**Liza has the built-in capability to do things right on the first pass.**

Liza has 13 roles organized in four pipeline phases:
- **Specification phase**: orchestrator, epic-planner, epic-plan-reviewer, us-writer, us-reviewer
- **Architecture phase**: orchestrator, architect, architecture-reviewer
- **Coding phase**: orchestrator, code-planner, code-plan-reviewer, coder, code-reviewer
- **Integration phase**: integration-analyst, integration-reviewer, coder, code-reviewer

Master planning role-pairs do not add roles. They reuse the same doer and reviewer roles with `decomposition-root: true` when planning would otherwise fan out.

```
┌─────────────────────────────────────────────────────────────┐
│                         Human                               │
│   (leads specs, observes terminals, reads blackboard,       │
│               kills agents, pauses system)                  │
└─────────────────────────────────────────────────────────────┘
                              │
    ┌─────────── Specification Phase ──────────┐
    │                                          │
    │  Orchestrator (decomposes & rescopes)    │
    │  Epic Planner ←→ Epic Plan Reviewer      │
    │  (master pair first only for fan-out)    │
    │  US Writer    ←→ US Reviewer             │
    │                                          │
    └──────────────────┬───────────────────────┘
                       │ liza proceed (us-to-coding, many-to-one)
    ┌─────────── Architecture Phase ───────────┐
    │                                          │
    │  Orchestrator (decomposes & rescopes)    │
    │  Architect    ←→ Architecture Reviewer   │
    │  (master pair first only for fan-out)    │
    │                                          │
    └──────────────────┬───────────────────────┘
                       │ liza proceed (architecture-to-code-plan)
    ┌──────────── Coding Phase ────────────────┐
    │                                          │
    │  Code Planner ←→ Code Plan Reviewer      │
    │  (master pair first only for fan-out)    │
    │  Coder        ←→ Code Reviewer           │
    │                                          │
    └──────────────────┬───────────────────────┘
                       │ all coding tasks merged
    ┌──────────── Integration Phase ───────────┐
    │                                          │
    │  Integration Analyst ←→ Integration Rev. │
    │  (findings → fix tasks in coding-pair)   │
    │                                          │
    └──────────────────┬───────────────────────┘
                       │
                       ▼
              ┌─────────────────┐
              │   .liza/        │
              │   state.yaml    │  ← blackboard
              │   log.yaml      │  ← activity history
              │   alerts.log    │  ← watch daemon output
              │   archive/      │  ← terminal-state tasks
              └─────────────────┘
                       │
                       ▼
              ┌─────────────────┐
              │  .worktrees/    │
              │  task-1/        │  ← isolated workspaces
              │  task-2/        │
              └─────────────────┘
```

See [Architecture](specs/architecture) and [C4 Diagrams](specs/architecture/c4/c4.md).

### Task Lifecycle

Each role pair follows the same intra-pair flow (concrete state names are role-pair-specific, e.g. `DRAFT_CODE`, `IMPLEMENTING_CODE`):

```
initial → executing → submitted → reviewing → approved → MERGED
             │ ↑                      ↓           │
             │ └────── rejected ──────┘           │
             │                                     ↓
             ├──> BLOCKED               INTEGRATION_FAILED
             │    ├──> SUPERSEDED
             │    └──> ABANDONED
             │
             └──> initial (release claim)
```

Inter-pair transitions (`liza proceed`) create downstream tasks between sprints. Case A remains direct: `architecture-to-code-plan` starts `code-planning-pair` children from specialized architecture outputs and bypasses `code-planning-main-pair`.

```
  Spec phase                                  Architecture phase                    Coding phase

  Epic Master ─auto─► Epic Planner      Arch Master ─auto─► Architect          Code Plan Master ─auto─► Code Planner
      ▲ fan-out only      │ epic-to-us       ▲ fan-out only    │ arch-to-code      ▲ fan-out only       │ code-plan-to-code
      │                   ▼                  │                 └──────────────►    │                    ▼
  simple entry ─────► Epic Planner      simple entry ─────► Architect          simple entry ─────► Code Planner
                           │ us-to-coding (many-to-one)                                             Coder
                           ▼                                                                        │ all tasks merged
                      Architecture phase                                                            ▼
                                                                                               Integration Analyst (auto)
```

Example of a task on the blackboard:
```yaml
    - id: code-planning-1-code-3
      type: coding
      role_pair: coding-pair
      description: Role infrastructure recognizes the 4 new roles with correct runtime/workflow mapping.
      status: MERGED
      priority: 1
      assigned_to: coder-2
      base_commit: e7625ed69318836dd495b22855df3a8b91fe32b5
      iteration: 1
      review_commit: 9d9254b893af477fc34f48063169634d200fa332
      approved_by: code-reviewer-1
      merge_commit: 2fa6399223262df6a87c6b1354dfc882b73114c5
      lease_expires: 2026-03-06T01:47:22.075108537Z
      spec_ref: specs/plans/sub-pipelines-phase2.md
      done_when: ToWorkflow("epic-planner") returns "epic_planner" (and all 4 pairs); IsValidRuntime("us-writer") returns true; AllRuntime() returns 9 roles; Tests pass
      scope: internal/roles/roles.go, internal/roles/roles_test.go, internal/models/state.go
      created: 2026-03-06T01:17:00.99638669Z
      history:
        - time: 2026-03-06T01:17:22.075108537Z
          event: claimed
          agent: coder-2
        - time: 2026-03-06T01:19:30.131578505Z
          event: pre_execution_checkpoint
          agent: coder-2
          files_to_modify:
            - internal/roles/roles.go
            - internal/roles/roles_test.go
            - internal/models/state.go
          intent: Add 4 new role constants (epic-planner, epic-plan-reviewer, us-writer, us-reviewer) with runtime↔workflow mapping, update AllRuntime()/AllWorkflow() to return 9 roles, and add Role* aliases in models/state.go.
          validation_plan: 'Run `go test ./internal/roles/ ./internal/models/` in worktree. Verify: ToWorkflow("epic-planner")→"epic_planner" for all 4 new roles, IsValidRuntime("us-writer")→true, AllRuntime() returns 9 roles.'
        - time: 2026-03-06T01:22:05.371651393Z
          event: submitted_for_review
          agent: coder-2
        - time: 2026-03-06T01:24:30.366073081Z
          event: approved
          agent: code-reviewer-1
        - time: 2026-03-06T03:06:35.560908548+01:00
          event: merged
          agent: code-reviewer-1
          commit: 2fa6399223262df6a87c6b1354dfc882b73114c5
          tests_ran: false
```

---

## Status

See [Release Notes](docs/release_notes/) for version history and [RELEASE.md](RELEASE.md) for maintainer release workflow.

**Where Liza works today:**
- **Pairing mode** is battle-tested — agents write **~99% of production code** under human supervision
- **Multi-agent mode** produces solid specs and code through the full goal-to-merge pipeline with 13 roles across 3 phases — starting from release v0.4.0, all major Liza changes are implemented using this mode

Liza is a collaborative agent network (L4 AI maturity) but its architecture has been designed to support a software factory (L5) where humans focus on strategy and product vision. Still a long way to go.

**Implemented roles:**
- Orchestrator (decomposes goal into planning tasks)
- Epic Planner / Epic Plan Reviewer
- US Writer / US Reviewer
- Architect / Architecture Reviewer
- Code Planner / Code Plan Reviewer
- Coder / Code Reviewer
- Integration Analyst / Integration Reviewer

**Planned role pairs:**
- Sprint Analyzer role — analyze agent logs at sprint boundaries, capitalize on patterns via lesson-capture
- Security Auditor / Security Audit Reviewer — review the security of the code

**Roadmap:**
- Context handoff as blackboard event — structured positive/negative findings on every task completion
- Deterministic pre/post hooks at role transitions — mechanical checks before spawning agents and before their handoff
- Orchestrator-routed model selection — assign tasks to models based on estimated complexity

### Provider Compatibility

The contract is a capability test. It requires meta-cognitive machinery—the ability to parse instructions as executable specifications, observe state, pause at gates.

| Provider | Classification                          | Notes |
|----------|-----------------------------------------|-------|
| Claude Opus 4.x | Fully compatible | Reference provider |
| GPT-5.x-Codex | Fully compatible | Equally capable |
| Kimi 2.5 | Compatible but poor on real-world tasks | Responsive to tooling feedback |
| Mistral Devstral-2 | Partial | Requires explicit activation and supervision |
| Gemini 2.5 Flash | Incompatible | Architectural limitation—no prompt-level fix |

See [Model Capability Assessment](docs/demo-benchmark/wrap-up.md) for detailed analysis.

## Naming

**Liza** combines two references:

**Lisa Simpson**—the disciplined, systematic counterpoint to Ralph Wiggum. The [Ralph Wiggum technique](https://github.com/anthropics/claude-code/tree/main/plugins/ralph-wiggum) loops agents until they converge through persistence. Lisa makes sure the work is actually right.

**ELIZA**—the 1966 chatbot that demonstrated structured dialogue patterns. Liza is about structured collaboration patterns: explicit states, binding verdicts, auditable transitions.

Liza doesn't make agents smarter. It makes them accountable.

## License

Apache 2.0

## Acknowledgments

The behavioral contract draws on research into LLM failure modes, sycophancy patterns, and code generation failures. The multi-agent design incorporates ideas from:

- **[SpecKit](https://github.com/github/spec-kit)** — Project specification
- **[BMAD Method](https://github.com/bmad-code-org/BMAD-METHOD)** — Role templates and workflow patterns
- **Classical blackboard architecture** — Shared state coordination
- **[Ralph Wiggum technique](https://github.com/anthropics/claude-code/tree/main/plugins/ralph-wiggum)** — Iteration until convergence, validated by an adversarial agent instead of mechanical check or self-declaration
- Stephen Oberther (**[liza-go](https://github.com/smo921/liza-go)**) — Shell to Go CLI migration
- **[CrewAI](https://github.com/crewAIInc/crewAI)'s composable guardrails concept** — Reduced to Liza's convention-over-code pattern.
