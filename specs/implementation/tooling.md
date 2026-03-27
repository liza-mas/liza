# Tooling

## Deliverables

### Contract Files (`<project>/contracts/`)

Contracts are versioned with the project:

| File | Purpose |
|------|---------|
| `CORE.md` | Universal rules + mode selection gate |
| `PAIRING_MODE.md` | Human-supervised collaboration (extracted from current contract) |
| `MULTI_AGENT_MODE.md` | Agent-supervised Liza system (new) |

### Global Symlink (`~/.liza/`)

| File | Purpose |
|------|---------|
| `CLAUDE.md` | Symlink → `<project>/contracts/CORE.md` |

**Note:** Update symlink when switching projects: `ln -sf /path/to/project/contracts/CORE.md ~/.claude/CLAUDE.md`

### Go CLI (`liza`)

All system mechanics are provided by the `liza` Go binary (assumed in PATH). See [ADR-0012](../architecture/ADR/0012-go-cli-replaces-bash-scripts.md).

| Command | Purpose |
|---------|---------|
| `liza init "goal" --spec spec` | Initialize `.liza/` for new goal |
| `liza add-task --id X ...` | Add task to blackboard (atomic, with validation) |
| `liza validate [state]` | Schema validation |
| `liza tui` | Alarm monitor daemon |
| `liza analyze` | Circuit breaker analysis (human-triggered) |
| `liza sprint-checkpoint` | Create checkpoint and generate sprint summary |
| `liza agent <role> --agent-id x [--cli C]` | Agent supervisor (`--cli`: claude, codex, gemini, mistral, kimi) |
| `liza claim-task <task> <agent>` | Claim task with two-phase commit (called by supervisor) |
| `liza submit-for-review <task> <sha>` | Validate `<sha>` matches pre-rebase worktree HEAD, then set READY_FOR_REVIEW + post-rebase `review_commit` + history |
| `liza submit-verdict <task> <V> [reason]` | Atomically set APPROVED/REJECTED + review fields + history |
| `liza wt-create <task> [--fresh]` | Create worktree for task |
| `liza wt-merge <task>` | Merge approved worktree (supervisor-executed after APPROVED) |
| `liza wt-delete <task>` | Clean up abandoned/merged worktree |
| `liza update-sprint-metrics` | Recompute sprint.metrics from task state |
| `liza clear-stale-review-claims` | Clear expired review claims |
| `liza recover-task <task-id>` | Recover by task ID (release claims + worktree/branch + agent) |
| `liza recover-agent <agent-id>` | Recover by agent ID (release claim + worktree + delete agent) |
| `liza release-claim <task> [--role R]` | Release claim on task or review |
| `liza pause` / `liza resume` | Pause/resume system |
| `liza stop` / `liza start` | Stop/start system |
| `liza status` | Show system status |
| `liza get` | Get blackboard data |
| `liza mark-blocked` | Mark task as blocked |
| `liza assess-blocked` | Record orchestrator assessment of a blocked task (prevents re-wake) |
| `liza supersede-task` | Supersede a task |
| `liza delete agent\|task` | Delete agent or task entry |

Locking is internal to the binary — no external `flock` wrapper needed.

### Optional Project Files

| Path | Purpose | Used By |
|------|---------|---------|
| `integration-test.sh` | Integration test suite | `liza wt-merge` runs if present after merge |

If `integration-test.sh` exists in the project, `liza wt-merge` executes it after successful merge. On failure, merge is rolled back and task marked INTEGRATION_FAILED.

### Templates (`<project>/templates/`)

| File | Purpose |
|------|---------|
| `vision-template.md` | Template for goal-level vision document |
| `README.md` | Instructions for using templates |

**Note:** ADR template is at `specs/architecture/ADR/TEMPLATE.md` (co-located with ADRs for discoverability).

### Project Runtime (per project)

| Path | Purpose |
|------|---------|
| `.liza/state.yaml` | Goal, tasks, assignments, leases |
| `.liza/log.yaml` | Append-only activity log |
| `.liza/archive/` | Archived terminal-state tasks |
| `.worktrees/` | Git worktrees, one per active task |

### CLI Exit Codes

The `liza` CLI uses a consistent exit code taxonomy:

| Code | Meaning | Recovery |
|------|---------|----------|
| 0 | Success | None needed |
| 1 | Validation error (precondition failed) | Fix input, retry |
| 2 | Lock acquisition failed | Retry with backoff |
| 3 | Git operation failed | Check git state, resolve conflicts |
| 4 | State inconsistency (invariant violation) | Manual inspection required |

**Per-Command Specifics:**

| Command | Exit 1 | Exit 3 | Exit 4 |
|---------|--------|--------|--------|
| `liza wt-create` | Task not in an executing state | Worktree creation failed | — |
| `liza wt-merge` | Task not APPROVED, SHA mismatch | Merge conflict (detected via merge-tree) | — |
| `liza validate` | Schema violation found | — | — |

**Recovery Procedures:**
- **Exit 2 (lock failed):** Another process holds lock. Wait 1-5s, retry up to 3 times.
- **Exit 3 (git failed):** Run `git status` in affected worktree; resolve conflicts or stale state.
- **Exit 4 (inconsistency):** Stop all agents. Human must inspect `.liza/state.yaml` and fix manually.

---

## Agent-Blackboard Interface

### How Agents Execute Blackboard Operations

Agents have shell access via Claude Code's bash tool. Blackboard operations are `liza` CLI calls.

**Task Claiming:** The supervisor (`liza agent`) claims tasks using `liza claim-task` which implements a two-phase commit pattern to prevent invalid intermediate states:

```
Phase 1: Validate under lock (no state mutation)
  - Verify task exists and is READY
  - Verify dependencies are satisfied (all depends_on tasks MERGED)
  - Verify agent is available

Phase 2: Create worktree (outside lock)
  - Create git worktree at .worktrees/task-N
  - Branch from integration branch or main

Phase 3: Re-validate and commit under lock
  - Re-check all conditions (state may have changed)
  - Set IMPLEMENTING status with all required fields atomically
  - On validation failure: delete worktree and exit

Cleanup: If commit fails, worktree is deleted to maintain consistency
```

This pattern ensures no task is ever in IMPLEMENTING state without a valid worktree.

**State Updates:** Agents use dedicated CLI commands for state transitions. The CLI handles locking and validation internally:

```bash
# Request review (atomic)
liza submit-for-review task-3 "$COMMIT"
# Requires: $COMMIT == git -C .worktrees/task-3 rev-parse HEAD (pre-rebase)
# Sets READY_FOR_REVIEW + post-rebase review_commit + history

# Add task (Planner operation)
liza add-task \
  --id task-3 \
  --desc "Add retry decorator to UserAPI.get_user()" \
  --spec specs/retry-logic.md \
  --done "UserAPI.get_user() retries 3x on 5xx errors with exponential backoff" \
  --scope "src/api/user.py, tests/test_user_api.py" \
  --priority 1 \
  --depends "task-1,task-2"

# Read current state
liza get

# System status
liza status
```

### Command Availability

CLI commands are divided into agent-callable and supervisor-only:

**Agent-Callable Commands:**

| Command | Called By | Purpose |
|---------|-----------|---------|
| `liza add-task` | Planner | Add task atomically (validates after write) |
| `liza validate` | All agents (optional) | Verify state before/after operations |
| `liza get` | All agents | Read blackboard data |
| `liza submit-for-review` | Coder | Request review (atomic state transition) |
| `liza submit-verdict` | Code Reviewer | Approve/reject (atomic state transition) |
| `liza mark-blocked` | Coder, Planner | Mark task as blocked |
| `liza assess-blocked` | Orchestrator | Record assessment of blocked task (prevents re-wake loops) |
| `liza wt-merge` | Supervisor | Merge after Code Reviewer approves |
| `liza wt-delete` | Planner | Clean up abandoned tasks |

**Supervisor-Only Commands:**

| Command | Purpose |
|---------|---------|
| `liza agent` | Agent lifecycle management (start, restart, backoff) |
| `liza claim-task` | Two-phase task claiming with worktree creation |
| `liza wt-create` | Create worktree (called internally by `liza claim-task`) |

### Supervisor-Only Operations

**Terminology clarification:** "Supervisor" refers to the Go process loop within each `liza agent` instance—not a central singleton process. Each agent role runs in its own terminal with its own supervisor loop:

```
Terminal 1                    Terminal 2                    Terminal 3
┌─────────────────────┐      ┌─────────────────────┐      ┌─────────────────────┐
│ liza agent planner  │      │ liza agent coder    │      │ liza agent          │
│ --agent-id planner-1│      │ --agent-id coder-1  │      │ code-reviewer       │
│                     │      │                     │      │ --agent-id cr-1     │
│  while true:        │      │  while true:        │      │  while true:        │
│    wait_for_work()  │      │    claim_task()     │      │    claim_review()   │
│    claude -p "..."  │      │    claude -p "..."  │      │    claude -p "..."  │
│    handle_exit()    │      │    handle_exit()    │      │    handle_exit()    │
└─────────────────────┘      └─────────────────────┘      └─────────────────────┘
```

When specs say "supervisor claims task before spawning agent," this means the Go loop claims the task before invoking `claude`—all within the same `liza agent` process. The `claude` call blocks until the session ends.

The supervisor handles:
- Starting/restarting the Claude Code process
- Claiming tasks before spawning Coders (via `liza claim-task`)
- Assigning reviews before spawning Code Reviewers
- Detecting exit codes
- Respecting system mode (`config.mode: PAUSED`, `STOPPED`) and sprint status (`CHECKPOINT`)
- Backoff timing on crashes

**Signal handling:** The supervisor respects SIGINT/SIGTERM via `signal.NotifyContext`. On signal, the context is cancelled and the supervisor checks `ctx.Err()` at the top of its loop, exiting gracefully. Agent unregistration (`unregisterAgent`) atomically releases any active task claim — tasks return to READY (coder) or READY_FOR_REVIEW (reviewer) — before deleting the agent entry.

Agents do not call supervisor-only scripts or manage their own lifecycle.

---

## Startup Sequence

### Bootstrap (Human, One-Time)

Before any agent starts:

1. **Create vision document:**
   ```bash
   mkdir -p specs
   cp templates/vision-template.md specs/vision.md
   # Edit specs/vision.md with goal context
   ```

2. **Initialize blackboard:**
   ```bash
   cd /path/to/project
   liza init "Implement retry logic for all API calls"
   ```

3. **Write/verify specs:**
   - Ensure `specs/` contains requirements for the goal
   - Ensure `REPOSITORY.md` describes project structure
   - Review and approve spec content

4. **Start watcher (optional but recommended):**
   ```bash
   # Dedicated terminal
   liza tui
   ```

### Agent Startup (Human Triggers, Agents Run)

Start agents in separate terminals. Each agent requires a unique `LIZA_AGENT_ID`:

```bash
# Terminal 1: Planner
liza agent planner --agent-id planner-1

# Terminal 2: Coder (after planner has created tasks)
liza agent coder --agent-id coder-1

# Terminal 3: Code Reviewer (after coder starts requesting reviews)
liza agent code-reviewer --agent-id code-reviewer-1
```

**Multiple agents of the same role are supported.** Run additional agents in separate terminals:

```bash
# Terminal 4: Second coder (processes tasks in parallel)
liza agent coder --agent-id coder-2

# Terminal 5: Second reviewer (processes reviews in parallel)
liza agent code-reviewer --agent-id code-reviewer-2
```

See [Agent Identity Protocol](../architecture/roles.md#agent-identity-protocol) for identity validation and collision prevention.

### Startup Order

| Phase | Who Starts | Prerequisites |
|-------|------------|---------------|
| 1. Bootstrap | Human | Project exists, git initialized |
| 2. Planner | Human | Blackboard initialized, specs exist |
| 3. Coder(s) | Human | Planner has finalized tasks (READY) |
| 4. Code Reviewer | Human | Coder has requested review (READY_FOR_REVIEW) |

Agents can be started earlier—they'll wait/exit if no work available.

### Agent Session Start

When supervisor starts Claude Code, the agent:

1. Reads `CLAUDE.md` → `CORE.md` (contract)
2. Sees mode selection prompt
3. States: `"Mode: Liza [Role]"`
4. Follows initialization sequence from session initialization

The supervisor passes context via the initial prompt, including structured task assignment sections:

```bash
# Coder prompt includes "=== ASSIGNED TASK ===" section with:
# - TASK ID, WORKTREE (absolute path), DESCRIPTION, DONE WHEN, SCOPE, INSTRUCTIONS

# Code Reviewer prompt includes "=== REVIEW TASK ===" section with:
# - TASK ID, WORKTREE, COMMIT TO REVIEW, AUTHOR, DESCRIPTION, DONE WHEN, INSTRUCTIONS

# Planner prompt includes "=== PLANNING CONTEXT ===" section with:
# - WAKE TRIGGER: INITIAL_PLANNING | BLOCKED_TASKS | HYPOTHESIS_EXHAUSTED | IMMEDIATE_DISCOVERY
# - SPRINT STATE: total tasks, merged, in_progress, unclaimed, blocked, integration_failed, hypothesis_exhausted, immediate_discoveries
# - INSTRUCTIONS: trigger-specific guidance (varies by wake trigger)
```

See `liza agent` source (Go) for exact prompt-building logic per role.

Exact CLI syntax depends on Claude Code version. The contract handles mode selection regardless of invocation method.

---

## CLI Command Reference

All commands are subcommands of the `liza` binary. Run `liza help` or `liza <command> --help` for full usage.

### Key Commands

**liza init** — Initialize blackboard for new goal
```bash
liza init "Goal description" --spec specs/vision.md
```

**liza add-task** — Add task to blackboard (Planner)
```bash
liza add-task --id TASK_ID --desc DESCRIPTION --spec SPEC_REF \
  --done DONE_WHEN --scope SCOPE [--priority N] [--depends "task-a,task-b"]
# Atomically adds task, updates sprint.scope.planned and goal.alignment_history, validates
```

**liza validate** — Validate blackboard state
```bash
liza validate [state.yaml]
# Returns "VALID" or "INVALID: [issue description]"
```

**liza tui** — Monitor blackboard and alert
```bash
liza tui
# Runs continuously, alerts on: expired leases, blocked tasks, review loops, etc.
```

**liza analyze** — Circuit breaker analysis
```bash
liza analyze
# Detects systemic patterns, generates report, sets sprint.status: CHECKPOINT if triggered
```

**liza sprint-checkpoint** — Create checkpoint
```bash
liza sprint-checkpoint
# Sets sprint.status: CHECKPOINT and generates sprint summary
```

**liza agent** — Agent supervisor
```bash
liza agent coder --agent-id coder-1
# Runs agent in loop, handles exit codes, respects config.mode and sprint.status
```

**liza claim-task** — Claim task (supervisor-only)
```bash
liza claim-task task-3 coder-1
# Two-phase commit: validate → create worktree → re-validate and commit
```

**liza submit-for-review** — Request review (Coder)
```bash
liza submit-for-review task-3 "$COMMIT"
# Requires: $COMMIT == git -C .worktrees/task-3 rev-parse HEAD (pre-rebase)
# Sets READY_FOR_REVIEW + post-rebase review_commit + history
```

**liza submit-verdict** — Submit review verdict (Code Reviewer)
```bash
liza submit-verdict task-3 APPROVED
liza submit-verdict task-3 REJECTED "Missing error handling for 429 responses"
```

**liza wt-create** — Create worktree (supervisor-only)
```bash
liza wt-create task-3 [--fresh]
# Creates .worktrees/task-3 from integration branch
# --fresh: Delete existing worktree before creating (for reassignment to different coder)
```

**liza wt-merge** — Merge worktree (supervisor-executed after APPROVED)
```bash
liza wt-merge task-3
# Task must be APPROVED
# Performs working-tree-less merge using git merge-tree + commit-tree + update-ref
# Working tree files transiently synced for integration tests, then restored
# Multiple reviewers can merge concurrently without race conditions
```

**liza wt-delete** — Delete worktree
```bash
liza wt-delete task-3
# Removes worktree and branch for abandoned/superseded tasks
```

**liza recover-task** — Recover by task ID
```bash
liza recover-task task-1                    # Release claims + remove worktree/branch + recover agent
liza recover-task task-1 --force            # Also works when task is not in state (git-only cleanup)
# Idempotent. Refuses if claiming agent's PID is alive (use --force to override).
```

**liza recover-agent** — Recover by agent ID
```bash
liza recover-agent coder-1                  # Release claim + remove worktree + delete agent
liza recover-agent coder-1 --cli claude     # Same + respawn agent
liza recover-agent coder-1 --force          # Override PID liveness check
# Auto-detects role. Idempotent (no error if agent already gone).
```

**liza pause / liza resume** — Pause/resume system
```bash
liza pause    # Sets config.mode: PAUSED — agents exit gracefully
liza resume   # Sets config.mode: RUNNING — supervisors restart agents
```

**liza stop / liza start** — Stop/start system
```bash
liza stop     # Sets config.mode: STOPPED — all agents terminate
liza start    # Sets config.mode: RUNNING — supervisors restart agents
```

**liza status** — Show system status
```bash
liza status   # Summary of goal, sprint, agents, tasks
```

**liza get** — Read blackboard data
```bash
liza get      # Print current state
```

> **Note:** `liza handoff` command is pending Go implementation. The data model exists (`HandoffNote` struct, `handoff_pending` field) but no CLI command wraps it yet. Agents write handoff notes directly to state.yaml for now.

---

## Human Override Protocol

Human owns the intent and acts as observer and circuit-breaker, not approver.

### Observation Channels

| Channel | Purpose |
|---------|---------|
| Terminals | Watch agent output in real-time |
| `.liza/state.yaml` | Current assignments and states |
| `.liza/log.yaml` | Activity history (skimmable) |
| `liza tui` output | Alarms for attention-needed conditions |

### Override Actions

| Action | Mechanism | Effect |
|--------|-----------|--------|
| Kill agent | Ctrl+C / kill | Agent releases task claims on exit; supervisor restarts and re-reads blackboard |
| Pause all | `liza pause` | Sets `config.mode: PAUSED`; agents exit gracefully (code 42), supervisors wait |
| Resume | `liza resume` | Sets `config.mode: RUNNING`; supervisors restart agents |
| Force replan | `liza mark-blocked <task> --reason "human override"` | Planner escalation triggered |
| Inject task | `liza add-task --id X ...` (as READY) | New task available for claim |
| Abort goal | `liza stop` | Sets `config.mode: STOPPED`; all agents terminate, supervisors stop |

### Human Communication

Human can leave notes in blackboard:

```yaml
human_notes:
  - timestamp: 2025-01-17T15:00:00Z
    message: "Task-3 approach looks wrong. Consider existing retry util in src/utils/retry.py"
    for: task-3
```

Agents must read `human_notes` relevant to their task before starting/resuming work.

---

## Alarm Conditions

`liza tui` monitors and alerts on:

| Condition | Threshold | Alert |
|-----------|-----------|-------|
| Expired coder lease | lease_expires in past | `⚠️ LEASE EXPIRED: {agent} on {task}` |
| Expired review lease | review_lease_expires in past | `⚠️ REVIEW LEASE EXPIRED: {code_reviewer} on {task} — review can be reclaimed` |
| Task blocked | Any | `⚠️ BLOCKED: {task} — {reason}` |
| Orphaned rejected | REJECTED task, assignee not WORKING (30s grace) | `🚨 ORPHANED REJECTED: {task} — assigned to {agent} but agent is {status}` |
| Same task reassigned | 2nd coder | `⚠️ REASSIGNED: {task} — hypothesis exhaustion risk` |
| Review cycle count | ≥5 (cliff) | `🚨 REVIEW LOOP: {task} — {count} cycles (at cliff)` |
| Integration failure | Any | `🚨 INTEGRATION FAILED: {task}` |
| Hypothesis exhaustion | 2 coders failed | `🚨 HYPOTHESIS EXHAUSTION: {task} — requires rescope` |
| Approaching limits | 8/10 iter, 3/5 review | `⚠️ APPROACHING LIMIT: {task} — {metric}` |
| Goal stalled | No state change >30min | `⚠️ STALLED: no progress for {duration}` |
| Stale draft | DRAFT >30min | `⚠️ STALE DRAFT: {task} — created {age}min ago (Planner crash?)` |
| Immediate discovery | urgency=immediate, not converted | `🚨 IMMEDIATE DISCOVERY: {id} — {desc} (Planner should wake)` |
| Blackboard invalid | Validation fails | `🚨 INVALID STATE: {error}` |
| Checkpoint stale | >30min/2h/8h | `⚠️/🚨 CHECKPOINT STALE/STUCK: waiting for human` |
| PAUSE stale | >30min/2h | `⚠️/🚨 STALE PAUSE/FORGOTTEN: PAUSE file exists for {age}min` |

### Alert Output

Alerts write to:
- stderr (visible in watch terminal)
- `.liza/alerts.log` (persistent)

Optional: desktop notification via `notify-send` if available.

## Related Documents

- [Blackboard Schema](../architecture/blackboard-schema.md) — state.yaml structure
- [State Machines](../architecture/state-machines.md) — exit codes, state transitions
- [Phases](phases.md) — implementation sequence
