# Supervision Model: Action Responsibility

Who does what — supervisor vs agent via CLI commands.

## Multiple Agents Per Role

The supervision model supports running multiple agents of the same role concurrently. Each agent operates with its own supervisor loop and claims work independently:

```
Terminal 1: coder-1          Terminal 2: coder-2          Terminal 3: code-reviewer-1
┌─────────────────┐          ┌─────────────────┐          ┌───────────────────┐
│ liza agent coder│          │ liza agent coder│          │ liza agent        │
│ --agent-id      │          │ --agent-id      │          │ code-reviewer     │
│   coder-1       │          │   coder-2       │          │ --agent-id        │
│                 │          │                 │          │   code-reviewer-1 │
│  while true:    │          │  while true:    │          │  while true:      │
│    claim_task() │          │    claim_task() │          │    claim_review() │
│    spawn()      │          │    spawn()      │          │    spawn()        │
│    handle_exit()│          │    handle_exit()│          │    handle_exit()  │
└─────────────────┘          └─────────────────┘          └───────────────────┘
```

**Concurrency is safe because:**
- Task claiming uses atomic file locking (`flock` on `state.yaml`)
- Review claiming uses lease-based exclusive access
- Merging uses working-tree-less git operations (no working tree conflicts)

See [Role Definitions](roles.md) for supported agent combinations.

## Design Principle

The supervisor (Go process wrapping the agent CLI) **guarantees** infrastructure actions that agents might forget or do partially. CLI commands provide agent-initiated workflow actions and manual fallback paths for supervisor actions. No action that was supervisor-guaranteed has been delegated to agents.

This continues the principle from [ADR-0006](ADR/0006-supervisor-assigns-work.md) (supervisor assigns work) and [ADR-0011](ADR/0011-script-enforced-agent-status.md) (structural enforcement over behavioral compliance).

## Responsibility Matrix

### Supervisor-Only (agent has no access)

| Action | When | Why Supervisor-Only |
|--------|------|---------------------|
| Agent registration | Startup | Identity + collision detection before agent exists |
| Agent unregistration | Exit (deferred) | Cleanup must happen even on crash |
| Heartbeat | Background goroutine | Agent can't maintain its own liveness signal |
| Post-exit reset to IDLE | After CLI exits | Agent is gone — can't update own status |
| Orchestrator status setup | Before orchestrator launch | Sets WORKING atomically before agent sees blackboard |
| Handoff resume detection | Before fresh claim | Supervisor checks for `handoff_pending` tasks to resume |

### Supervisor-Guaranteed + CLI Fallback

These actions are **automatically triggered by the supervisor loop**. The CLI command exists as a manual/administrative path but is not required for normal operation.

| Action | Supervisor Trigger | CLI Command | Shared Code |
|--------|-------------------|-------------|-------------|
| Coder task claim | Before launch (`claimCoderTask`) | `liza claim-task` | `commands.ClaimTaskCommand` |
| Reviewer task claim | Before launch (`claimReviewerTask`) | *(none)* | *(inline in supervisor)* |
| Worktree merge | Reviewer loop (`handleApprovedMerges`) | `liza wt-merge` | `commands.WtMergeCommand` |
| Stale review clearing | Reviewer startup (`registerAgent`) | `liza clear-stale-review-claims` | `commands.ClearStaleReviewClaimsCommand` |

**Why CLI fallback exists:** Orchestrators or humans may need to trigger these manually (e.g., merge a task approved outside the normal reviewer flow, or clear a stale claim without restarting).

### Agent-Initiated (via CLI commands)

These are workflow actions that only the agent can trigger — they represent the agent's work output.

| Action | CLI Command | State Transition |
|--------|-------------|------------------|
| Submit work for review | `liza submit-for-review` | task: IMPLEMENTING -> READY_FOR_REVIEW, agent: WORKING -> WAITING |
| Submit review verdict | `liza submit-verdict` | task: -> APPROVED or -> IMPLEMENTING (rejection), agent: REVIEWING -> IDLE |
| Initiate handoff | `liza handoff` | task: sets `handoff_pending`, agent: WORKING -> HANDOFF |
| Mark task blocked | `liza mark-blocked` | task: -> BLOCKED |
| Add task(s) | `liza add-tasks` | Creates new task(s) (orchestrator) |
| Supersede task | `liza supersede-task` | task: -> SUPERSEDED (orchestrator) |
| Release claim | `liza release-claim` | task: -> READY, agent: -> IDLE |

### Administrative (CLI commands, not part of normal flow)

| Action | CLI Command | Use Case |
|--------|-------------|----------|
| Create worktree | `liza wt-create` | Re-create worktree for a claimed task (e.g., `--fresh` after reassignment) |
| Delete worktree | `liza wt-delete` | Manual cleanup |
| Delete agent | `liza delete agent` | Remove stale agent entry |
| Update sprint metrics | `liza update-sprint-metrics` | Recompute metrics on demand |
| Circuit breaker analysis | `liza analyze` | Trigger analysis manually |

### Read-Only (CLI commands)

| CLI Command | Purpose |
|-------------|---------|
| `liza get` | Query blackboard (tasks, agents, logs, config) |
| `liza status` | System status summary |
| `liza validate` | State consistency check |
| `liza version` | Version info |

## Architecture

```
Supervisor (Go)                          Agent (LLM CLI)
═══════════════                          ═══════════════
register agent
start heartbeat goroutine
claim task / detect handoff
build bootstrap prompt
spawn CLI ──────────────────────────────▶ receives pre-claimed work
  │                                        │
  │ heartbeat ticks (background)           │ does work in worktree
  │                                        │ runs CLI commands:
  │                                        │   liza submit-for-review
  │                                        │   liza submit-verdict
  │                                        │   liza mark-blocked
  │                                        │   liza handoff
  │                                        │
CLI exits ◀─────────────────────────────── agent completes/aborts
reset agent status
handle approved merges (reviewer)
loop: wait for work → claim → spawn
```

The `commands` package is the shared implementation layer. Both supervisor and CLI commands call the same `commands.*` functions, ensuring identical logic regardless of caller.

## Related

- [ADR-0006](ADR/0006-supervisor-assigns-work.md) — Supervisor-assigns-work model
- [ADR-0011](ADR/0011-script-enforced-agent-status.md) — Structural enforcement of status transitions
- [ADR-0012](ADR/0012-go-cli-replaces-bash-scripts.md) — Go CLI replaces bash scripts
- [State Machines](state-machines.md) — Task and agent state transitions
