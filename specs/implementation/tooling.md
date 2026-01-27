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

### Scripts (`~/.liza/scripts/`)

| Script | Purpose |
|--------|---------|
| `liza-init.sh` | Initialize `.liza/` for new goal |
| `liza-lock.sh` | Atomic read-modify-write operations |
| `liza-validate.sh` | Schema validation |
| `liza-watch.sh` | Alarm monitor daemon |
| `liza-analyze.sh` | Circuit breaker analysis (human-triggered) |
| `liza-checkpoint.sh` | Create checkpoint and generate sprint summary |
| `liza-agent.sh` | Agent supervisor (while-true wrapper) |
| `liza-claim-task.sh` | Claim task with two-phase commit (called by supervisor) |
| `liza-submit-for-review.sh` | Atomically set READY_FOR_REVIEW + review_commit + history |
| `liza-submit-verdict.sh` | Atomically set APPROVED/REJECTED + review fields + history |
| `wt-create.sh` | Create worktree for task |
| `wt-merge.sh` | Merge approved worktree (supervisor-executed after APPROVED) |
| `wt-delete.sh` | Clean up abandoned/merged worktree |
| `update-sprint-metrics.sh` | Recompute sprint.metrics from task state |
| `clear-stale-review-claims.sh` | Clear expired review claims |

**Deployment Note:** All scripts above are fully implemented in `scripts/`. They must be deployed to their runtime location (`~/.liza/scripts/`) before use. Until deployed, cross-script references (e.g., `wt-merge.sh` calling `update-sprint-metrics.sh`) will silently no-op.

### Optional Project Files

| Path | Purpose | Used By |
|------|---------|---------|
| `scripts/integration-test.sh` | Integration test suite | `wt-merge.sh` runs if present after merge |

If `scripts/integration-test.sh` exists, `wt-merge.sh` executes it after successful merge. On failure, merge is rolled back and task marked INTEGRATION_FAILED.

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

### Script Exit Codes

All Liza scripts use a consistent exit code taxonomy:

| Code | Meaning | Recovery |
|------|---------|----------|
| 0 | Success | None needed |
| 1 | Validation error (precondition failed) | Fix input, retry |
| 2 | Lock acquisition failed | Retry with backoff |
| 3 | Git operation failed | Check git state, resolve conflicts |
| 4 | State inconsistency (invariant violation) | Manual inspection required |
| 5 | External dependency failed (yq, git, bc not found) | Install dependencies |

**Per-Script Specifics:**

| Script | Exit 1 | Exit 3 | Exit 4 |
|--------|--------|--------|--------|
| `liza-lock.sh` | Invalid operation | — | Concurrent modification detected |
| `wt-create.sh` | Task not CLAIMED | Worktree creation failed | — |
| `wt-merge.sh` | Task not APPROVED, SHA mismatch | Merge conflict | — |
| `liza-validate.sh` | Schema violation found | — | — |

**Recovery Procedures:**
- **Exit 2 (lock failed):** Another process holds lock. Wait 1-5s, retry up to 3 times.
- **Exit 3 (git failed):** Run `git status` in affected worktree; resolve conflicts or stale state.
- **Exit 4 (inconsistency):** Stop all agents. Human must inspect `.liza/state.yaml` and fix manually.

---

## Agent-Blackboard Interface

### How Agents Execute Blackboard Operations

Agents have shell access via Claude Code's bash tool. Blackboard operations are direct script calls.

**Task Claiming:** The supervisor (`liza-agent.sh`) claims tasks using `liza-claim-task.sh` which implements a two-phase commit pattern to prevent invalid intermediate states:

```
Phase 1: Validate under lock (no state mutation)
  - Verify task exists and is UNCLAIMED
  - Verify dependencies are satisfied (all depends_on tasks MERGED)
  - Verify agent is available

Phase 2: Create worktree (outside lock)
  - Create git worktree at .worktrees/task-N
  - Branch from integration branch or main

Phase 3: Re-validate and commit under lock
  - Re-check all conditions (state may have changed)
  - Set CLAIMED status with all required fields atomically
  - On validation failure: delete worktree and exit

Cleanup: If commit fails, worktree is deleted to maintain consistency
```

This pattern ensures no task is ever in CLAIMED state without a valid worktree.

**State Updates:** Always combine related field updates in a single yq expression using `|=` and `|`:

```bash
# IMPORTANT: If yq fails mid-update, state.yaml is inconsistent.
# Always combine related field updates in a single yq expression

# Extend lease
~/.liza/scripts/liza-lock.sh write '.agents.coder-1.lease_expires' '2025-01-17T15:00:00Z'

# Read current state
~/.liza/scripts/liza-lock.sh read

# Request review (MUST be atomic)
$SCRIPT_DIR/liza-submit-for-review.sh task-3 a1b2c3d

# Log spec change (when human updates specs)
~/.liza/scripts/liza-lock.sh modify "
  yq -i '.spec_changes += [{
    \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
    \"spec\": \"specs/retry-logic.md#auth\",
    \"change\": \"Added auth token refresh retry behavior\",
    \"triggered_by\": \"task-4a\"
  }]' .liza/state.yaml
"

# Finalize DRAFT → UNCLAIMED (Planner only, after defining all required fields)
~/.liza/scripts/liza-lock.sh modify "
  yq -i '(.tasks[] | select(.id == \"task-3\" and .status == \"DRAFT\")) |=
    select(.done_when != null and .spec_ref != null) |
    .status = \"UNCLAIMED\"' .liza/state.yaml
"
# Note: The select() ensures task has required fields before finalization
```

### Script Availability

Scripts are divided into agent-callable and supervisor-only:

**Agent-Callable Scripts:**

| Script | Called By | Purpose |
|--------|-----------|---------|
| `liza-lock.sh` | All agents | Atomic blackboard operations |
| `liza-validate.sh` | All agents (optional) | Verify state before/after operations |
| `wt-merge.sh` | Supervisor | Merge after Code Reviewer approves |
| `wt-delete.sh` | Planner | Clean up abandoned tasks |

**Supervisor-Only Scripts:**

| Script | Purpose |
|--------|---------|
| `liza-agent.sh` | Agent lifecycle management (start, restart, backoff) |
| `liza-claim-task.sh` | Two-phase task claiming with worktree creation |
| `wt-create.sh` | Create worktree (called by liza-claim-task.sh) |

### Supervisor-Only Operations

**Terminology clarification:** "Supervisor" refers to the enclosing bash loop within each `liza-agent.sh` instance—not a central singleton process. Each agent role runs in its own terminal with its own supervisor loop:

```
Terminal 1                    Terminal 2                    Terminal 3
┌─────────────────────┐      ┌─────────────────────┐      ┌─────────────────────┐
│ liza-agent.sh       │      │ liza-agent.sh       │      │ liza-agent.sh       │
│ (planner supervisor)│      │ (coder supervisor)  │      │ (reviewer supervisor)│
│                     │      │                     │      │                     │
│  while true:        │      │  while true:        │      │  while true:        │
│    wait_for_work()  │      │    claim_task()     │      │    claim_review()   │
│    claude -p "..."  │      │    claude -p "..."  │      │    claude -p "..."  │
│    handle_exit()    │      │    handle_exit()    │      │    handle_exit()    │
└─────────────────────┘      └─────────────────────┘      └─────────────────────┘
```

When specs say "supervisor claims task before spawning agent," this means the bash loop claims the task before invoking `claude`—all within the same `liza-agent.sh` process. The `claude` call blocks until the session ends.

The supervisor handles:
- Starting/restarting the Claude Code process
- Claiming tasks before spawning Coders (via `liza-claim-task.sh`)
- Assigning reviews before spawning Code Reviewers
- Detecting exit codes
- Respecting PAUSE/ABORT/CHECKPOINT files
- Backoff timing on crashes

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
   ~/.liza/scripts/liza-init.sh "Implement retry logic for all API calls"
   ```

3. **Write/verify specs:**
   - Ensure `specs/` contains requirements for the goal
   - Ensure `REPOSITORY.md` describes project structure
   - Review and approve spec content

4. **Start watcher (optional but recommended):**
   ```bash
   # Dedicated terminal
   ~/.liza/scripts/liza-watch.sh
   ```

### Agent Startup (Human Triggers, Agents Run)

Start agents in separate terminals. Each agent requires a unique `LIZA_AGENT_ID`:

```bash
# Terminal 1: Planner
LIZA_AGENT_ID=planner-1 ~/.liza/scripts/liza-agent.sh planner

# Terminal 2: Coder (after planner has created tasks)
LIZA_AGENT_ID=coder-1 ~/.liza/scripts/liza-agent.sh coder

# Terminal 3: Code Reviewer (after coder starts requesting reviews)
LIZA_AGENT_ID=code-reviewer-1 ~/.liza/scripts/liza-agent.sh code_reviewer
```

See [Agent Identity Protocol](../architecture/roles.md#agent-identity-protocol) for identity validation and collision prevention.

### Startup Order

| Phase | Who Starts | Prerequisites |
|-------|------------|---------------|
| 1. Bootstrap | Human | Project exists, git initialized |
| 2. Planner | Human | Blackboard initialized, specs exist |
| 3. Coder(s) | Human | Planner has finalized tasks (UNCLAIMED) |
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
# - WAKE TRIGGER: INITIAL_PLANNING | BLOCKED_TASKS | INTEGRATION_FAILED | HYPOTHESIS_EXHAUSTED | IMMEDIATE_DISCOVERY
# - SPRINT STATE: total tasks, merged, in_progress, unclaimed, blocked, integration_failed, hypothesis_exhausted, immediate_discoveries
# - INSTRUCTIONS: trigger-specific guidance (varies by wake trigger)
```

See `liza-agent.sh` functions `build_coder_context()`, `build_reviewer_context()`, and `build_planner_context()` for exact formats.

Exact CLI syntax depends on Claude Code version. The contract handles mode selection regardless of invocation method.

---

## Script Specifications

Script implementations are in the [`scripts/`](scripts/) directory:

| Script | Purpose | Source |
|--------|---------|--------|
| `liza-init.sh` | Initialize `.liza/` for new goal | [scripts/liza-init.sh](scripts/liza-init.sh) |
| `liza-lock.sh` | Atomic read-modify-write operations | [scripts/liza-lock.sh](scripts/liza-lock.sh) |
| `liza-validate.sh` | Schema validation | [scripts/liza-validate.sh](scripts/liza-validate.sh) |
| `liza-watch.sh` | Alarm monitor daemon | [scripts/liza-watch.sh](scripts/liza-watch.sh) |
| `liza-analyze.sh` | Circuit breaker analysis (human-triggered) | [scripts/liza-analyze.sh](scripts/liza-analyze.sh) |
| `liza-checkpoint.sh` | Create checkpoint and generate sprint summary | [scripts/liza-checkpoint.sh](scripts/liza-checkpoint.sh) |
| `liza-agent.sh` | Agent supervisor (while-true wrapper) | [scripts/liza-agent.sh](scripts/liza-agent.sh) |
| `liza-claim-task.sh` | Claim task with two-phase commit | [scripts/liza-claim-task.sh](scripts/liza-claim-task.sh) |
| `wt-create.sh` | Create worktree for task | [scripts/wt-create.sh](scripts/wt-create.sh) |
| `wt-merge.sh` | Merge approved worktree (supervisor-executed after APPROVED) | [scripts/wt-merge.sh](scripts/wt-merge.sh) |
| `wt-delete.sh` | Clean up abandoned/merged worktree | [scripts/wt-delete.sh](scripts/wt-delete.sh) |

### Script Usage Summary

**liza-init.sh** — Initialize Liza blackboard for new goal
```bash
liza-init.sh "Goal description"
```

**liza-lock.sh** — Atomic blackboard operations
```bash
liza-lock.sh read                    # Print current state
liza-lock.sh write field value       # Set field (yq syntax)
liza-lock.sh modify "script"         # Run script with lock held
```

**liza-validate.sh** — Validate blackboard state
```bash
liza-validate.sh [state.yaml]
# Returns "VALID" or "INVALID: [issue description]"
```

**liza-watch.sh** — Monitor blackboard and alert
```bash
liza-watch.sh [project_root]
# Runs continuously, alerts on: expired leases, blocked tasks, review loops, etc.
```

**liza-analyze.sh** — Circuit breaker analysis
```bash
liza-analyze.sh [project_root]
# Detects systemic patterns, generates report, creates CHECKPOINT if triggered
```

**liza-checkpoint.sh** — Create checkpoint
```bash
liza-checkpoint.sh [project_root]
# Creates CHECKPOINT file and generates sprint summary
```

**liza-agent.sh** — Agent supervisor
```bash
LIZA_AGENT_ID=coder-1 liza-agent.sh coder [initial-task-id]
# Runs agent in loop, handles exit codes, respects PAUSE/ABORT/CHECKPOINT
```

**wt-create.sh** — Create worktree
```bash
wt-create.sh [--fresh] <task-id>
# Creates .worktrees/<task-id> from integration branch
# --fresh: Delete existing worktree before creating (for reassignment to different coder)
```

**wt-merge.sh** — Merge worktree (supervisor-executed after APPROVED)
```bash
wt-merge.sh <task-id>
# Requires LIZA_AGENT_ID to be a Code Reviewer, task must be APPROVED
```

**wt-delete.sh** — Delete worktree
```bash
wt-delete.sh <task-id>
# Removes worktree and branch for abandoned/superseded tasks
```

---

## Human Override Protocol

Human owns the intent and acts as observer and circuit-breaker, not approver.

### Observation Channels

| Channel | Purpose |
|---------|---------|
| Terminals | Watch agent output in real-time |
| `.liza/state.yaml` | Current assignments and states |
| `.liza/log.yaml` | Activity history (skimmable) |
| `liza-watch.sh` output | Alarms for attention-needed conditions |

### Override Actions

| Action | Mechanism | Effect |
|--------|-----------|--------|
| Kill agent | Ctrl+C / kill | Supervisor restarts; agent re-reads blackboard |
| Pause all | Create `.liza/PAUSE` file | Agents exit gracefully (code 42), supervisors wait |
| Resume | Remove `.liza/PAUSE` file | Supervisors restart agents |
| Force replan | Edit `state.yaml`: set task to BLOCKED with `blocked_reason: "human override"` | Planner escalation triggered |
| Inject task | Edit `state.yaml`: add task (as UNCLAIMED, not DRAFT) | New task available for claim |
| Abort goal | Create `.liza/ABORT` file | All agents terminate, supervisors stop |

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

`liza-watch.sh` monitors and alerts on:

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
