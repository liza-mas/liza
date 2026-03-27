# Architecture Overview

## System Components

```
┌─────────────────────────────────────────────────────────────┐
│                         Human                               │
│   (leads specs, observes terminals, reads blackboard,       │
│               kills agents, pauses system)                  │
└─────────────────────────────────────────────────────────────┘
                              │
          ┌───────────────────┼───────────────────┐
          ▼                   ▼                   ▼
    ┌───────────┐        ┌──────────┐        ┌────────────┐
    │ Orchestrator   │        │  Coder   │        │    Code    │
    │           │        │          │        │  Reviewer  │
    │ Decomposes│        │ Claims   │        │            │
    │ goal into │        │ tasks,   │        │ Examines   │
    │ tasks,    │        │ iterates │        │ work,      │
    │ rescopes  │        │ until    │        │ approves   │
    │ on failure│        │ approved │        │ or rejects,│
    │           │        │  review  │        │ merges     │
    └─────┬─────┘        └────┬─────┘        └─────┬──────┘
          │                   │                    │
          └───────────────────┴────────────────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │   .liza/        │
                    │   state.yaml    │  ← blackboard
                    │   log.yaml      │  ← activity history
                    └─────────────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  .worktrees/    │
                    │  task-1/        │  ← isolated workspaces
                    │  task-2/        │
                    └─────────────────┘
```

**LLM Providers:** Liza supports multiple providers (Claude, Codex, Mistral, Gemini). The behavioral contract is LLM-agnostic in principle. However, not all providers can fully comply — Claude and Codex achieve full compliance; Gemini and Mistral achieve partial compliance. See ADR-0008.

## Data Flow

```
Human writes/approves specs (with support from spec-review and systemic-thinking skills)
         │
         ▼
┌─────────────────────────────────────────────────────┐
│                    specs/                           │
│  requirements.md, architecture.md, ADR              │
└─────────────────────────────────────────────────────┘
         │
         ├──────────────────┬──────────────────┐
         ▼                  ▼                  ▼
    ┌─────────┐        ┌─────────┐        ┌─────────┐
    │ Orchestrator │        │  Coder  │        │  Code   │
    │         │        │         │        │Reviewer │
    │ Reads   │        │ Reads   │        │ Reads   │
    │ specs → │        │ specs → │        │ specs → │
    │ decom-  │        │ under-  │        │ vali-   │
    │ poses   │        │ stands  │        │ dates   │
    │ goal    │        │ task    │        │ against │
    └─────────┘        └─────────┘        └─────────┘
```

## Spec Artifacts

| Artifact | Purpose | Survives |
|----------|---------|----------|
| `specs/` | Requirements, constraints, ADRs | Sessions, restarts, agent replacement |
| `docs/` | Usage, architecture, domain knowledge | Sessions, restarts, agent replacement |
| `REPOSITORY.md` | Project purpose, structure, conventions | Everything |
| Handoff notes (`.liza/`) | Task-specific context for replacement agent | Task lifetime |
| Goal-alignment summary | Current state vs original intent | Goal lifetime |
| `specs/vision.md` | Goal context: why, for whom, MVP scope, out of scope | Goal lifetime |
| `specs/architecture/ADR/ADR-NNN.md` | Architecture Decision Records | Project lifetime |
| `specs/CHANGELOG.md` | Aggregated spec change history | Project lifetime |

### specs/CHANGELOG.md Format

```markdown
# Spec Changelog

## [Goal-2] — User Authentication
| Date | Spec | Change | Triggered By |
|------|------|--------|--------------|
| 2025-01-20 | auth.md | Added OAuth2 flow | task-12 |
| 2025-01-19 | auth.md | Initial version | goal creation |

## [Goal-1] — API Retry Logic
| Date | Spec | Change | Triggered By |
|------|------|--------|--------------|
| 2025-01-18 | retry-logic.md#auth | Added token refresh | task-4a |
| 2025-01-17 | retry-logic.md | Initial version | goal creation |
```

**Note:** Runtime spec changes are logged in `state.yaml` (`spec_changes` section). CHANGELOG.md is the human-maintained, persistent summary aggregated at sprint boundaries.

### Why Specs Are Load-Bearing

The design philosophy states: *"Every restart is a new mind with old artifacts."*

Those artifacts are:
1. **Code** — what was built
2. **Blackboard** — coordination state (who's doing what)
3. **Specs** — semantic state (what we're building and why)

Without specs, a restarted agent sees code but not intent. It sees tasks but not requirements. It can continue mechanically but not intelligently.

## Key Mechanisms

### Leases, Not Just Heartbeats

Agents hold time-bounded leases on tasks. A stale agent's task becomes reclaimable only after the lease expires. No ambiguity about ownership.

### DRAFT Tasks

Orchestrator writes tasks as DRAFT, finalizes to READY. Coders cannot claim half-written tasks.

### Commit SHA Verification

Coder records commit SHA when requesting review. Code Reviewer verifies the SHA before examining work. No reviewing stale state.

### Approval-Gated Merge

Coders commit to their worktree. Only the supervisor can merge to the integration branch, and only after Code Reviewer approval. Authority is structural, not advisory.

### Hypothesis Exhaustion

If two different coders fail the same task, the task framing is presumed wrong. Orchestrator must rescope—cannot just reassign unchanged.

### Rescoping Audit Trail

When tasks are rescoped, original task becomes SUPERSEDED with explicit reason. New tasks reference what they replace. No silent rewrites.

### Supervisor-Assigns-Work

Agents don't discover and claim their own work. Each agent's supervisor (the Go process wrapping the agent CLI) claims the task BEFORE spawning the agent. The agent receives pre-claimed work in its bootstrap prompt. This eliminates race conditions and simplifies agent logic. See ADR-0006 and [Supervision Model](supervision-model.md) for the full responsibility matrix.

### TDD Enforcement

TDD is mandatory for all code tasks in MAS. Tests must be written first against `done_when` criteria, encoding spec intent before implementation exists. This prevents the failure mode where tests validate what code does rather than what the spec requires. See ADR-0007.

## Directory Structure

### Project Repository

```
<project>/
├── contracts/                     # Versioned with project
│   ├── CORE.md                    # Universal rules + mode selection gate
│   ├── PAIRING_MODE.md            # Human-supervised collaboration
│   └── MULTI_AGENT_MODE.md        # Agent-supervised Liza system
├── templates/
│   ├── vision-template.md         # Goal-level vision template
│   └── README.md
├── specs/                         # Project specifications
└── .liza/                         # Runtime state (see below)
```

### Global Contract Root and Loading

```
~/.liza/               # Canonical contract root (symlinks to project)
├── CORE.md              → <project>/contracts/CORE.md
├── PAIRING_MODE.md      → <project>/contracts/PAIRING_MODE.md
├── MULTI_AGENT_MODE.md  → <project>/contracts/MULTI_AGENT_MODE.md
├── AGENT_TOOLS.md       → <project>/contracts/AGENT_TOOLS.md
├── skills/              → <project>/skills/
└── specs/               → <project>/specs/
```

**Contract Loading Chain:**

User-level prompts (`~/.claude/CLAUDE.md`) are NOT reliably read by Claude Code. Repo-level prompts are systematically read on session start.

Therefore, each project creates repo-level symlinks:
```
<REPO_ROOT>/CLAUDE.md → ~/.liza/CORE.md   # Reliable loading
<REPO_ROOT>/AGENTS.md → ~/.liza/CORE.md   # Alternative entry point
<REPO_ROOT>/GEMINI.md → ~/.liza/CORE.md   # Provider-specific entry
```

1. Agent reads `<REPO_ROOT>/CLAUDE.md` (repo-level, systematic)
2. Symlink resolves to `~/.liza/CORE.md` → `<project>/contracts/CORE.md`
3. CORE.md contains universal rules and mode selection gate
4. For Liza mode: read `~/.liza/MULTI_AGENT_MODE.md`. For Pairing mode: read `~/.liza/PAIRING_MODE.md`.

Refer to `contracts/contract-activation.md` for activating Liza in a user project. See ADR-0009 for rationale.

### Go CLI (`liza`)

All system mechanics are provided by the `liza` Go binary (assumed in PATH). See [ADR-0012](ADR/0012-go-cli-replaces-bash-scripts.md).

| Command | Purpose |
|---------|---------|
| `liza init "goal" --spec spec` | Initialize blackboard |
| `liza validate [state]` | Schema validation |
| `liza tui` | Alarm monitor |
| `liza sprint-checkpoint` | Create checkpoint |
| `liza analyze` | Circuit breaker analysis |
| `liza agent <role> --agent-id x` | Agent supervisor |
| `liza add-task --id X ...` | Add task to backlog |
| `liza claim-task <task> <agent>` | Atomically claim task for agent |
| `liza submit-for-review <task> <sha>` | Submit work for review |
| `liza submit-verdict <task> <V> [reason]` | Record review verdict |
| `liza release-claim <task> [--role R]` | Release claim on task or review |
| `liza clear-stale-review-claims` | Clean up abandoned reviews |
| `liza update-sprint-metrics` | Sprint statistics |
| `liza wt-create <task> [--fresh]` | Create worktree |
| `liza wt-merge <task>` | Merge (supervisor-executed after APPROVED) |
| `liza wt-delete <task>` | Clean up worktree |
| `liza pause` / `liza resume` | Pause/resume system |
| `liza stop` / `liza start` | Stop/start system |
| `liza status` | Show system status |
| `liza get` | Get blackboard data |

Locking is internal to the binary — no external `flock` wrapper needed.

### Project Runtime

```
<project>/
├── .liza/
│   ├── state.yaml                 # Current state
│   ├── log.yaml                   # Activity history
│   └── archive/                   # Terminal-state tasks
└── .worktrees/
    └── task-N/                    # Per-task workspace
```

## Branch Strategy

```
main
  └── integration  (all approved work merges here)
        ├── .worktrees/task-1/  (branched from integration)
        ├── .worktrees/task-2/
        └── .worktrees/task-3/
```

Merge to main is human-triggered, not part of Liza flow.

## Related Documents

- [Roles](roles.md) — detailed role responsibilities
- [State Machines](state-machines.md) — task and agent states
- [Blackboard Schema](blackboard-schema.md) — state.yaml structure
- [Supervision Model](supervision-model.md) — supervisor vs MCP tool responsibility
- [Worktree Management](../protocols/worktree-management.md) — worktree lifecycle
