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
- Merging constructs commits without a working tree; a project-scoped file lock serializes integration ref advancement and transient main-index sync/restore

See [Role Definitions](roles.md) for supported agent combinations.

## Design Principle

The supervisor (Go process wrapping the agent CLI) **guarantees** infrastructure actions that agents might forget or do partially. CLI commands provide agent-initiated workflow actions and manual fallback paths for supervisor actions. No action that was supervisor-guaranteed has been delegated to agents.

This continues the principle from [ADR-0006](ADR/0006-supervisor-assigns-work.md) (supervisor assigns work) and [ADR-0011](ADR/0011-script-enforced-agent-status.md) (structural enforcement over behavioral compliance).

## Responsibility Matrix

### Supervisor-Only (agent has no access)

| Action | When | Why Supervisor-Only |
|--------|------|---------------------|
| Agent registration | Startup | Identity + collision detection before agent exists |
| Agent unregistration | Exit (deferred) | Cleanup must happen even on crash. Claim release and row removal wait up to the patient lock-acquisition budget (60s), because the exit is often a lock timeout; an exit that skips the deferral (SIGKILL, OOM) or a longer lock queue leaves the claim until lease expiry |
| Heartbeat | Background goroutine | Agent can't maintain its own liveness signal |
| Post-exit reset to IDLE | After CLI exits | Agent is gone — can't update own status |
| Orchestrator status setup | Before orchestrator launch | Sets WORKING atomically before agent sees blackboard |
| Handoff resume detection | Before fresh claim | Supervisor checks for `handoff_pending` tasks to resume |
| Owned executing recovery | Before fresh claim | Supervisor resumes tasks already assigned to the same doer after child CLI exit/restart |
| Execution progress watchdog | During child CLI execution | Detects stalled provider execution even while heartbeat stays fresh |

### Supervisor-Guaranteed + CLI Fallback

These actions are **automatically triggered by the supervisor loop**. The CLI command exists as a manual/administrative path but is not required for normal operation.

| Action | Supervisor Trigger | CLI Command | Shared Code |
|--------|-------------------|-------------|-------------|
| Coder task claim | Before launch (`claimCoderTask`) | `liza claim-task` | `commands.ClaimTaskCommand` |
| Reviewer task claim | Before launch (`claimReviewerTask`) | *(none)* | *(inline in supervisor)* |
| Worktree merge | Reviewer loop (`handleApprovedMerges`) | `liza wt-merge` | `commands.WtMergeCommand` |
| Stale review clearing | Reviewer startup (`registerAgent`) | `liza clear-stale-review-claims` | `commands.ClearStaleReviewClaimsCommand` |
| Terminal receipt archival | After each newly completed merge (`mergeWorktree`) | `archive-acceptance-receipts` subcommand of the configured executable (operator-only) | `ops.ArchiveTerminalAcceptanceReceipts` |

**Why CLI fallback exists:** Orchestrators or humans may need to trigger these manually (e.g., merge a task approved outside the normal reviewer flow, or clear a stale claim without restarting).

Doer supervisors check work in this order: explicit handoff resume, owned executing recovery, then fresh claim. Owned executing recovery is not a handoff: it applies when an executing task is already assigned to the supervisor's agent ID, `handoff_pending` is false, the registered agent role matches the task role-pair's doer role, and the agent is either idle or already points at that task. Before spawning a replacement child CLI, the supervisor validates the existing worktree; missing or unhealthy worktrees transition the task to `BLOCKED` with a diagnostic instead of entering a restart loop.

During a child CLI run, doer supervisors run an execution progress watchdog for the owned executing task. The watchdog treats task-state changes, worktree HEAD/status changes including untracked files, and provider stdout/stderr writes as progress. It polls at `agent_progress_timeout / 4`, capped at 15 seconds. If no progress occurs before `config.agent_progress_timeout`, the supervisor cancels the child process, waits for it to exit, transitions the still-owned executing task to `BLOCKED` with a diagnostic, and runs blocked-task worktree cleanup. Heartbeat and lease renewal alone are not progress.

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

## Generation-Fenced Recovery Contract

- **Authority:** Current-generation authority is required at every agent-authenticated lifecycle write. The caller's agent ID and opaque registration generation are compared inside the same `Blackboard.Modify` that performs the write, before any mutation. This fence is independent of effective-operation authorization: it does not change RBAC permissions.
- **Provider launch:** One cross-process per-agent lifecycle lock orders registration against provider start. Registration acquires the lifecycle lock before the blackboard lock. A launch holder reads state, verifies current-generation authority, and reaches only the provider's start/session-creation boundary; no provider backend effect runs inside `Blackboard.Modify`. Built-in providers complete start before wait and wait outside both locks. The compatibility adapter holds the lifecycle lock for the complete blocking legacy call because its interface exposes no narrower start boundary. Lock timeout, state-read failure, generation mismatch, setup failure, or process-start failure releases the lock with no successful start event or state rewrite.
- **Liveness:** Ownership is lease-first, and namespace-unverifiable ownership has effective status `unknown/degraded`. A fresh heartbeat and unexpired lease continue to occupy singularity and role capacity despite a raw dead or mismatched PID observation from another namespace. Registration and watcher diagnostics preserve the registered PID and report a deterministically correlated observer-visible PID, or explicitly state `correlation unavailable`; that process evidence cannot authorize takeover. Expiry or explicit audited removal remains required.
- **Review retention:** Cleanup first checks the task/reviewer tuple: exact pipeline reviewer role, usable PID, matching `current_task`, appropriate `REVIEWING`/`WAITING` status, and current review lease. A recorded heartbeat and unexpired registration lease preserve a coherent active or passive claim despite raw dead/mismatched PID evidence. Without that registration evidence, the existing live-matching/live-unknown process fallback remains. Missing/expired review leases and inconsistent owner metadata remain stale. A crashed supervisor can therefore reserve review until lease expiry (up to the default 30 minutes); this trade-off was explicitly accepted on 2026-09-22. See the [ADR-0062 amendment](ADR/0062-ghost-agent-claim-prevention-and-ownership-reconciliation.md#2026-09-22-amendment-lease-first-review-ownership).
- **Review execution:** Headless reviewer turns require current registration authority, registration lease/heartbeat evidence, and matching active review ownership at launch under the lifecycle lock. During execution, the supervisor observes current authority and the task/reviewer tuple at the normalized heartbeat interval (default 60 seconds), without raw PID checks. A successful read proving ownership loss cancels the provider; transient read failures are logged and retried. Observation is bounded by that interval when reads succeed, not atomic with state writes. This turn's own new approved/rejected history permits its final response and legitimate `WAITING` transition; old or foreign verdicts cannot authorize continuation, and active re-review resets the prior verdict allowance. A lost claim at launch or during execution is a coordination outcome: after provider exit (if started), the supervisor uses the generation-fenced reset and waits one configured role poll interval before checking for more work, without crash/spin accounting or replacing another owner. A healthy reviewer registration stays available; lost registration authority still ends the supervisor.
- **Headless cancellation:** CLI and ACPX setup/prompt commands use the subprocess layer's owned process-group/tree termination (SIGKILL on Unix) and five-second `WaitDelay`. ACPX also closes its manually drained output readers after that cancellation delay. Normal completion drains output normally. Detached or remote provider processes are outside the termination guarantee; local output-pipe waiting remains bounded. Interactive execution retains its existing terminal semantics. Execution timeout remains the outer bound for a legitimate reviewer turn.
- **Approved merge takeover:** If the final approver exits, a deterministic current-generation reviewer may resume the already-approved merge. Takeover preserves immutable approval actors, quorum, provider-diversity evidence, review commit, and reviewer role. Recovery converges from both Git not advanced and Git already advanced while task state remains approved, without duplicating the integration result or merged history. This does not serialize issue #129.
- **Rejected-task handoff:** Release and reclaim treat `assigned_to, lease_expires, worktree, base_commit, physical task artifact, and reviewer affinity` as one locked tuple. Recovery reuses a healthy worktree, reattaches a valid task branch when its directory is absent, and recreates from integration only when no reusable valid artifact exists. It fails closed without deleting unclassifiable artifacts.

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
