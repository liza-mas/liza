# Repository Guide

Liza is a disciplined peer-supervised multi-agent coding system. See [README](README.md) for philosophy, approach, and architecture.

This document is a navigation aid: where to find things and why they're organized that way.

## Structure

```
├── cmd/                    # Go CLI entry point (liza)
├── internal/               # Go internal packages (implementation)
├── contracts/              # Behavioral contracts governing agents
├── specs/                  # Specifications (durable agent context)
├── skills/                 # Domain-specific agent skills
├── docs/                   # Public docs, walkthroughs, and compatibility indexes/stubs
├── support-docs/           # Installable operational docs copied to ~/.liza/support-docs/
├── lessons/                # Project-specific operational lessons
├── templates/              # Document templates
│
├── README.md               # Project overview
├── REPOSITORY.md           # This file
├── RELEASE.md              # Release documentation
├── AGENTS.md               # Symlink → ~/.liza/CORE.md (for agents, e.g. Codex)
├── CLAUDE.md               # Symlink → ~/.liza/CORE.md (Claude Code)
├── GEMINI.md               # Symlink → ~/.liza/CORE.md (Gemini)
├── INVARIANTS.md           # System invariants (cross-ref: contracts, specs, code)
├── GETTING_STARTED.md      # First-run installation and setup guide
├── LICENSE                 # Apache 2.0
├── go.mod                  # Go module definition
├── Makefile                # Build system (build, test, lint, install, release)
├── install.sh              # Installation script
├── .goreleaser.yaml        # Release automation
├── .pre-commit-config.yaml # Pre-commit hooks
├── .editorconfig           # Editor formatting
└── .envrc                  # Direnv environment
```

## contracts/

Behavioral contracts that turn agents into trustworthy peers by countering LLM failure modes.

| File | Purpose |
|------|---------|
| `CORE.md` | Universal rules: tier system, state machine, golden rules 1-14, protocols, security |
| `PAIRING_MODE.md` | Human-supervised mode: approval gates, collaboration modes, magic phrases |
| `MULTI_AGENT_MODE.md` | Peer-supervised Liza mode: blackboard protocol, role definitions, circuit breaker |
| `SUBAGENT_MODE.md` | Subagent mode: delegated read-only work, internal ceremony |
| `AGENT_TOOLS.md` | Tool sub-contract (MCP preferences, forbidden tools, codebase exploration) |
| `COLLABORATION_CONTINUITY.md` | Cross-session collaboration patterns (the "letter to future self") |
| `CONTRACT_FAILURE_MODE_MAP.md` | Maps every clause to the failure modes it covers |
| `README.md` | Contract navigation guide |

Deployed by symlinking `CORE.md` to `~/.liza/CORE.md`. Mode contracts and supporting files go under `~/.liza/`.

## specs/

Externalized context that survives agent restarts. Agents read specs before acting — specs prevent rediscovering requirements through failing tests.

```
specs/
├── README.md                           # Reading order and navigation
├── build/                              # Build specifications (system implementation)
│   └── 0 - Vision.md                   # Philosophy, success criteria, foundation spec
├── functional/                         # Functional domain specifications
│   ├── 1 - Liza.md                     # Liza system overview
│   └── 1.1.md - 1.6.md                 # Domain-specific specs
├── architecture/
│   ├── overview.md                     # Components, data flow, directory structure
│   ├── roles.md                        # Orchestrator, Coder, Code Reviewer responsibilities
│   ├── state-machines.md               # Task states, agent states, exit codes
│   ├── blackboard-schema.md            # YAML state structure, locking, operations
│   ├── supervision-model.md            # Supervision architecture
│   ├── architectural-issues.md         # Known architectural issues and trade-offs
│   └── ADR/                            # Architecture Decision Records
├── protocols/
│   ├── task-lifecycle.md               # Claim → iterate → review → merge
│   ├── await-primitives.md             # Session-holding waits, budget mechanics, coupled clocks
│   ├── sprint-governance.md            # Checkpoints, retrospectives, spec evolution
│   ├── circuit-breaker.md              # Systemic failure detection, severity levels
│   ├── worktree-management.md          # Isolated workspaces, merge protocol
│   └── agent-initialization.md         # Agent bootstrap protocol
├── implementation/
│   ├── phases.md                       # Implementation roadmap
│   ├── tooling.md                      # Scripts, agent-blackboard interface
│   ├── validation-checklist.md         # v1 completion criteria
│   └── future.md                       # v1.1+ roadmap
└── _archive/                           # Archived/superseded specs
```

## lessons/

Project-specific operational lessons captured via the `lesson-capture` skill. Prevents recurring mistakes by encoding gotchas, patterns, and hard-won insights.

```
lessons/
├── agents/              # Agent lessons; trigger index lives in GUARDRAILS.md Tier 2
└── humans/              # Lessons for human developers
    └── README.md        # Index: trigger + title for each lesson
```

## skills/

Specialized protocols agents load conditionally. Each contains a single `SKILL.md`.

| Skill | Trigger |
|-------|---------|
| `check-liza-input-readiness/` | Assessing whether an input document is ready for a chosen Liza MAS entry point |
| `debugging/` | Before any debugging (mandatory) |
| `testing/` | When writing or analyzing tests (mandatory) |
| `code-review/` | When reviewing PRs or pending changes, or answering review feedback |
| `adversarial-pairing/` | Coordinating doer/reviewer pairing sessions through a Markdown blackboard |
| `architecture-planning/` | Defining architecture for a change (architect role) |
| `software-architecture-review/` | Implementation planning, structural concerns |
| `clean-code/` | Pre-commit refactoring (Python-focused) |
| `spec-review/` | Specification validation |
| `spec-backfill/` | Extracting feature-level specifications from existing code |
| `code-spec-backfill/` | Backfilling function-level contracts (docstrings, type annotations) |
| `generic-subagent/` | Delegating read-only work to subagents |
| `goal-writing/` | Coaching a human through the input document for `liza init --spec` |
| `systemic-thinking/` | Systemic coherence and risk analysis |
| `black-box-red-testing/` | Hypothesis-driven red tests that expose real bugs |
| `white-box-red-testing/` | Target-driven red tests with classified findings |
| `adr-backfill/` | Extracting ADRs from git history |
| `feynman/` | Explaining complex ideas simply |
| `lesson-capture/` | Capturing project-specific operational lessons |
| `context-engineering/` | Analyzing agent prompts and outputs for context quality, bloat, and handoff fit |

Skills execute within contract constraints — contract gates are non-negotiable, skill steps operate within them.

## Go CLI (`liza`)

All Liza system mechanics are provided by the `liza` Go binary (assumed in PATH). See [ADR-0012](specs/architecture/ADR/0012-go-cli-replaces-bash-scripts.md).

**Build requirement:** The Go binary embeds contracts, skills, and config files via `//go:embed`. Contracts and skills are copied from the repo root by `make sync-embedded` (a prerequisite of `make build`, `make test-fast`, `make test`, `make test-race`, and `make coverage`). Config files (`claude-settings.json`) and hooks are mastered directly in `internal/embedded/`. Use `make test` for the required routine full-suite check instead of bare `go test ./...`, then run `make test-race` once during final pre-commit/merge validation. Without the sync step, the `internal/embedded` package fails to compile.

Key command groups:

**Initialization & validation subcommands:** `init`, `cleanup`, `validate`

**Agent supervision:** `liza agent`, `liza tui`, `liza analyze`, `liza sprint-checkpoint`, `liza handoff`, `liza delete`

**Task management:** `liza add-task`, `liza claim-task`, `liza mark-blocked`, `liza retarget-dependency`, `liza repair-superseded-dependencies`, `liza supersede-task`, `liza cancel-task`

**Review & merge:** `liza submit-for-review`, `liza submit-verdict`, `liza release-claim`, `liza clear-stale-review-claims`

**Worktree management:** `liza wt-create`, `liza wt-merge`, `liza wt-delete`

**System control:** `liza pause`, `liza resume`, `liza stop`, `liza start`, `liza status`, `liza get`, `liza version`

**Toolchain setup:** `liza toolchain list`, `liza toolchain doctor`, `liza toolchain install`, `liza toolchain configure`

**Metrics:** `liza update-sprint-metrics`

Locking is internal to the binary — no external `flock` wrapper needed.

## docs/

Public documentation, walkthroughs, and compatibility stubs for moved support docs.

| File                    | Purpose |
|-------------------------|---------|
| `DEMO.md`               | Full end-to-end walkthrough |
| `TESTING.md`            | Testing documentation |
| `PERFORMANCE.md`        | Performance documentation |
| `RECIPES.md`            | Usage recipes |
| `liza-hardened-mas.md`  | Hardening measures inventory (defense-in-depth layers) |
| `release_notes/`        | Version changelogs |
| `demo-benchmark/`       | Multi-agent demo traces and comparisons |
| `agent-testimony/`      | Agent session transcripts and observations |

## support-docs/

Installable operational documentation written to `~/.liza/support-docs/` by
`liza setup`.

| File | Purpose |
|------|---------|
| `USAGE_MULTI_AGENTS.md` | Quick start guide (Liza multi-agent) |
| `USAGE_PAIRING.md` | Pairing mode guide (human-agent collaboration) |
| `ADVERSARIAL_PAIRING.md` | Doer/reviewer Pairing-mode blackboard workflow |
| `CONFIGURATION.md` | Configuration documentation, including setup/init effects |
| `TOOLCHAIN.md` | Optional local CLI toolchain install, doctor, and activation guide |
| `TROUBLESHOOTING.md` | Common issues and fixes |
| `CUSTOMIZING_AGENT_TOOLS.md` | How to adapt `AGENT_TOOLS.md` to the local tool surface |
| `how-to-produce-a-goal.md` | How to write a good goal doc for `liza init --spec` |
| `SUPPORT.md` | Installed support reference copied into project-local `.liza/` |

## templates/

Document templates for bootstrapping new artifacts.

| File | Purpose |
|------|---------|
| `vision-template.md` | Goal-level vision document (produces `specs/build/0 - Vision.md`) |
| `README.md` | Template usage guide and triggers |

ADR template lives at `specs/architecture/ADR/TEMPLATE.md`.

## Reading Order

For newcomers:

1. `README.md` — What Liza is and why
2. `GETTING_STARTED.md` — Install and first-run setup
3. `support-docs/USAGE_PAIRING.md` — Pairing mode: what you get and how to use it
4. `specs/build/0 - Vision.md` — Design philosophy and success criteria
5. `specs/architecture/overview.md` — System components and data flow
6. `contracts/CORE.md` — The behavioral contract
7. `support-docs/USAGE_MULTI_AGENTS.md` — How to run the multi-agent system
