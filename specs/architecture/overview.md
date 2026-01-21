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
    │ Planner   │        │  Coder   │        │    Code    │
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
    │ Planner │        │  Coder  │        │  Code   │
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

Planner writes tasks as DRAFT, finalizes to UNCLAIMED. Coders cannot claim half-written tasks.

### Commit SHA Verification

Coder records commit SHA when requesting review. Code Reviewer verifies the SHA before examining work. No reviewing stale state.

### Code Reviewer-Only Merge

Coders commit to their worktree. Only Code Reviewers can merge to the integration branch. Authority is structural, not advisory.

### Hypothesis Exhaustion

If two different coders fail the same task, the task framing is presumed wrong. Planner must rescope—cannot just reassign unchanged.

### Rescoping Audit Trail

When tasks are rescoped, original task becomes SUPERSEDED with explicit reason. New tasks reference what they replace. No silent rewrites.

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

### Global Symlink and Contract Loading

```
~/.claude/
├── CLAUDE.md                      → <project>/contracts/CORE.md (symlink)
└── scripts/                       # Generic Liza tooling
```

**Contract Loading Chain:**
1. Agent reads `~/.claude/CLAUDE.md` (symlink)
2. Symlink resolves to `<project>/contracts/CORE.md`
3. CORE.md contains universal rules and mode selection gate
4. For Liza mode: read MULTI_AGENT_MODE.md

Update symlink when switching projects: `ln -sf /path/to/project/contracts/CORE.md ~/.claude/CLAUDE.md`

### Global Scripts (`~/.claude/scripts/`)

```
~/.claude/
    ├── liza-init.sh               # Initialize blackboard
    ├── liza-lock.sh               # Atomic operations
    ├── liza-validate.sh           # Schema validation
    ├── liza-watch.sh              # Alarm monitor
    ├── liza-checkpoint.sh         # Create checkpoint
    ├── liza-analyze.sh            # Circuit breaker analysis
    ├── liza-agent.sh              # Agent supervisor
    ├── wt-create.sh               # Create worktree
    ├── wt-merge.sh                # Merge (Code Reviewer-only)
    └── wt-delete.sh               # Clean up worktree
```

**Note:** `~/.claude/CLAUDE.md` symlinks to the active project's `contracts/CORE.md`. When switching projects, update the symlink.

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
- [Worktree Management](../protocols/worktree-management.md) — worktree lifecycle
