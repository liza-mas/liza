# Tooling

## Deliverables

### Contract Files (`<project>/contracts/`)

Contracts are versioned with the project:

| File | Purpose |
|------|---------|
| `CORE.md` | Universal rules + mode selection gate |
| `PAIRING_MODE.md` | Human-supervised collaboration (extracted from current contract) |
| `MULTI_AGENT_MODE.md` | Agent-supervised Liza system (new) |

### Global Symlink (`~/.claude/`)

| File | Purpose |
|------|---------|
| `CLAUDE.md` | Symlink → `<project>/contracts/CORE.md` |

**Note:** Update symlink when switching projects: `ln -sf /path/to/project/contracts/CORE.md ~/.claude/CLAUDE.md`

### Scripts (`~/.claude/scripts/`)

| Script | Purpose |
|--------|---------|
| `liza-init.sh` | Initialize `.liza/` for new goal |
| `liza-lock.sh` | Atomic read-modify-write operations |
| `liza-validate.sh` | Schema validation |
| `liza-watch.sh` | Alarm monitor daemon |
| `liza-analyze.sh` | Circuit breaker analysis (human-triggered) |
| `liza-checkpoint.sh` | Create checkpoint and generate sprint summary |
| `liza-agent.sh` | Agent supervisor (while-true wrapper) |
| `wt-create.sh` | Create worktree for task |
| `wt-merge.sh` | Merge approved worktree (Code Reviewer-only) |
| `wt-delete.sh` | Clean up abandoned/merged worktree |
| `update-sprint-metrics.sh` | Recompute sprint.metrics from task state |
| `clear-stale-review-claims.sh` | Clear expired review claims |

**Deployment Note:** All scripts above are fully implemented in `scripts/`. They must be deployed to their runtime location (`~/.claude/scripts/`) before use. Until deployed, cross-script references (e.g., `wt-merge.sh` calling `update-sprint-metrics.sh`) will silently no-op.

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

Agents have shell access via Claude Code's bash tool. Blackboard operations are direct script calls:

```bash
# Claim a task (MUST be atomic - single yq command with all updates)
# Note: base_commit is set by wt-create.sh at worktree branch time, not during claim
~/.claude/scripts/liza-lock.sh modify "
  yq -i '(.tasks[] | select(.id == \"task-3\" and .status == \"UNCLAIMED\")) |=
    (.status = \"CLAIMED\" | .assigned_to = \"coder-1\" | .lease_expires = \"$(date -u -d '+5 minutes' +%Y-%m-%dT%H:%M:%SZ)\" | .worktree = \".worktrees/task-3\")' .liza/state.yaml
"
# IMPORTANT: If yq fails mid-update, state.yaml is inconsistent.
# Always combine related field updates in a single yq expression using |= and |
# After claim, call wt-create.sh which sets base_commit

# Extend lease
~/.claude/scripts/liza-lock.sh write '.agents.coder-1.lease_expires' '2025-01-17T15:00:00Z'

# Read current state
~/.claude/scripts/liza-lock.sh read

# Request review
~/.claude/scripts/liza-lock.sh modify "
  yq -i '(.tasks[] | select(.id == \"task-3\")).status = \"READY_FOR_REVIEW\"' .liza/state.yaml
  yq -i '(.tasks[] | select(.id == \"task-3\")).review_commit = \"a1b2c3d\"' .liza/state.yaml
"

# Log activity
echo "- timestamp: $(date -u +%Y-%m-%dT%H:%M:%SZ)
  agent: coder-1
  action: ready_for_review
  task: task-3
  detail: \"iteration 2, commit a1b2c3d\"" >> .liza/log.yaml

# Log spec change (when human updates specs)
~/.claude/scripts/liza-lock.sh modify "
  yq -i '.spec_changes += [{
    \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
    \"spec\": \"specs/retry-logic.md#auth\",
    \"change\": \"Added auth token refresh retry behavior\",
    \"triggered_by\": \"task-4a\"
  }]' .liza/state.yaml
"

# Finalize DRAFT → UNCLAIMED (Planner only, after defining all required fields)
~/.claude/scripts/liza-lock.sh modify "
  yq -i '(.tasks[] | select(.id == \"task-3\" and .status == \"DRAFT\")) |=
    select(.done_when != null and .spec_ref != null) |
    .status = \"UNCLAIMED\"' .liza/state.yaml
"
# Note: The select() ensures task has required fields before finalization
```

### Script Availability

All scripts in `~/.claude/scripts/` are agent-callable, not supervisor-only:

| Script | Called By | Purpose |
|--------|-----------|---------|
| `liza-lock.sh` | All agents | Atomic blackboard operations |
| `liza-validate.sh` | All agents (optional) | Verify state before/after operations |
| `wt-create.sh` | Coder | Create worktree on claim |
| `wt-merge.sh` | Code Reviewer | Merge after approval |
| `wt-delete.sh` | Planner | Clean up abandoned tasks |

### Supervisor-Only Operations

The supervisor (`liza-agent.sh`) handles:
- Starting/restarting the Claude Code process
- Detecting exit codes
- Respecting PAUSE/ABORT/CHECKPOINT files
- Backoff timing on crashes

Agents do not call `liza-agent.sh` or manage their own lifecycle.

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
   ~/.claude/scripts/liza-init.sh "Implement retry logic for all API calls"
   ```

3. **Write/verify specs:**
   - Ensure `specs/` contains requirements for the goal
   - Ensure `REPOSITORY.md` describes project structure
   - Review and approve spec content

4. **Start watcher (optional but recommended):**
   ```bash
   # Dedicated terminal
   ~/.claude/scripts/liza-watch.sh
   ```

### Agent Startup (Human Triggers, Agents Run)

Start agents in separate terminals. Each agent requires a unique `LIZA_AGENT_ID`:

```bash
# Terminal 1: Planner
LIZA_AGENT_ID=planner-1 ~/.claude/scripts/liza-agent.sh planner

# Terminal 2: Coder (after planner has created tasks)
LIZA_AGENT_ID=coder-1 ~/.claude/scripts/liza-agent.sh coder

# Terminal 3: Code Reviewer (after coder starts requesting reviews)
LIZA_AGENT_ID=code-reviewer-1 ~/.claude/scripts/liza-agent.sh code_reviewer
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

The supervisor passes context via the initial prompt:

```bash
claude --print "Mode: Liza Coder. Project root: $(pwd). Read blackboard and specs."
```

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
| `wt-create.sh` | Create worktree for task | [scripts/wt-create.sh](scripts/wt-create.sh) |
| `wt-merge.sh` | Merge approved worktree (Code Reviewer-only) | [scripts/wt-merge.sh](scripts/wt-merge.sh) |
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

**wt-merge.sh** — Merge worktree (Code Reviewer-only)
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
