# Liza Support Reference

Troubleshooting reference for Liza multi-agent executions.
This file is written to `.liza/SUPPORT.md` during `liza init`.

## Diagnostic Commands

```bash
liza status                        # Dashboard: goal, sprint, agents, task summary
liza get tasks                     # All tasks with current state
liza get tasks --format table      # Tabular view
liza get agents                    # Registered agents and lease status
liza validate                      # Check blackboard against invariants
liza analyze                       # Circuit breaker pattern detection
```

## Recovery Commands

```bash
liza recover-task <task-id>        # Release claim + remove worktree/branch
liza recover-agent <agent-id>      # Release claim + remove worktree + delete agent
liza release-claim <task-id>       # Granular: release claim only
liza clear-stale-review-claims     # Clear all expired review leases
liza delete agent <id>             # Remove agent from state
liza delete task <id>              # Remove task from state
```

## System Control

```bash
liza pause                         # Pause all agents (sets CHECKPOINT)
liza resume                        # Resume from CHECKPOINT or advance sprint
liza stop                          # Abort system
liza sprint-checkpoint             # Force checkpoint (halt + summary)
liza replan [task-id]              # Invalidate planner output, create new planning task
```

## Task State Machine

### Coding Pair (coder / code-reviewer)

```
DRAFT → READY → IMPLEMENTING → READY_FOR_REVIEW → REVIEWING → APPROVED → MERGED
                     │ ↑                              ↓
                     │ └────────── REJECTED ──────────┘
                     │
                     └──→ BLOCKED → SUPERSEDED | ABANDONED

APPROVED → INTEGRATION_FAILED → IMPLEMENTING (integration-fix)
```

| State | Meaning |
|-------|---------|
| DRAFT | Orchestrator defining task — coders must ignore |
| READY | Available for claim |
| IMPLEMENTING | Coder working (has lease) |
| READY_FOR_REVIEW | Submitted, awaiting reviewer |
| REVIEWING | Reviewer active (has review lease) |
| REJECTED | Reviewer rejected — feedback in `rejection_reason` |
| APPROVED | Reviewer approved — eligible for merge |
| MERGED | Merged to integration branch (terminal) |
| BLOCKED | Cannot proceed — see `blocked_reason` and `blocked_questions` |
| SUPERSEDED | Replaced by tasks in `superseded_by`, or completed externally (terminal) |
| ABANDONED | Killed by orchestrator (terminal) |
| INTEGRATION_FAILED | Merge conflict or test failure |

### Code-Planning Pair (code-planner / code-plan-reviewer)

```
DRAFT_CODING_PLAN → CODE_PLANNING → CODING_PLAN_TO_REVIEW → REVIEWING_CODING_PLAN
                         │ ↑                                        ↓
                         │ └──────── CODING_PLAN_REJECTED ─────────┘
                         │
                         └──→ BLOCKED
                                              CODING_PLAN_APPROVED (sprint-terminal)
```

## Sprint Lifecycle

```
IN_PROGRESS → CHECKPOINT → COMPLETED → (new sprint) IN_PROGRESS
```

**`liza resume` behavior depends on sprint state:**
- **CHECKPOINT, not all tasks terminal**: resumes as IN_PROGRESS
- **CHECKPOINT, all tasks terminal**: marks COMPLETED
- **COMPLETED**: archives sprint, creates new one, executes pipeline transitions

**`liza proceed <task-id> <transition>`**: creates child tasks from a completed task's `output[]` for the next role-pair.

## Reading state.yaml

Use `liza` CLI commands for all state mutations. If a needed operation isn't covered, edit `.liza/state.yaml` directly but: write atomically (write to temp file, then rename), use ISO 8601 UTC timestamps (`YYYY-MM-DDTHH:MM:SSZ`), and run `liza validate` afterward to verify invariants.

Key task fields:
- `status` — current state
- `assigned_to` — which agent holds the task (doer)
- `reviewing_by` — which agent is reviewing
- `lease_expires` / `review_lease_expires` — when the claim expires
- `base_commit` — commit the worktree was created from
- `review_commit` — commit submitted for review
- `merge_commit` — commit on integration branch after merge
- `iteration` — doer iteration count
- `review_cycles_current` / `review_cycles_total` — rejection count
- `blocked_reason` / `blocked_questions` — why the task is stuck
- `rejection_reason` — reviewer feedback on rejection
- `depends_on` — task IDs that must be terminal before this task is claimable
- `output[]` — structured output entries (used by `liza proceed` to create child tasks)
- `history[]` — timestamped event log per task

Key agent fields:
- `id` — e.g. `coder-1`, `code-reviewer-2`
- `role` — runtime role name
- `status` — STARTING, IDLE, WORKING, REVIEWING, WAITING, HANDOFF
- `lease_expires` — agent registration expiry
- `current_task` — task ID being worked on

## Agent Exit Codes

| Code | Meaning | Supervisor action |
|------|---------|-------------------|
| 0 | No more work for this role | Stop supervisor |
| 42 | Graceful abort (context exhaustion, lease lost, pause) | Restart immediately |
| Other | Crash | Restart with backoff |

Exit 42 with `handoff_pending: true` on the task means context exhaustion — the restarted agent reads handoff notes and continues.

## Common Failure Patterns

### Lease defaults

- Lease duration: 30 minutes
- Heartbeat interval: 60 seconds
- If lease expires, task becomes reclaimable

### Timestamp format

All timestamps in state.yaml use ISO 8601 UTC: `YYYY-MM-DDTHH:MM:SSZ`
Generate: `date -u +%Y-%m-%dT%H:%M:%SZ`

### Stuck task (stale lease)
**Symptom**: Task in IMPLEMENTING or REVIEWING but agent is gone.
**Diagnosis**: `liza get tasks` — check `lease_expires` is in the past (see Lease defaults above).
**Fix**: `liza recover-task <task-id>` or `liza release-claim <task-id>`.

### Agent crash loop
**Symptom**: Supervisor keeps restarting, agent exits non-zero repeatedly.
**Diagnosis**: Check agent output logs in `.liza/agent-outputs/` and the bootstrap prompt in `.liza/agent-prompts/` (what the agent was told to do).
**Fix**: After 5 restarts without progress, supervisor auto-blocks the task. Check `blocked_reason`. May need `liza recover-task` then manual investigation.

### BLOCKED task
**Symptom**: Task in BLOCKED state, agents skip it.
**Diagnosis**: Read `blocked_reason` and `blocked_questions` in state.yaml.
**Fix**: Either `liza supersede-task <id> [replacements] --reason "..."` (replace with new tasks or mark completed externally) or resolve the blocker and use `liza recover-task <id>` to reset.

### Integration failure
**Symptom**: Task in INTEGRATION_FAILED state.
**Diagnosis**: Merge conflict between task worktree and integration branch.
**Fix**: A coder can claim it (status allows INTEGRATION_FAILED → IMPLEMENTING with `integration_fix: true`). The worktree is preserved for conflict resolution.

### Sprint stuck at CHECKPOINT
**Symptom**: All agents idle, sprint in CHECKPOINT.
**Diagnosis**: `liza status` — check checkpoint trigger.
**Fix**: `liza resume` to continue, or `liza proceed` + `liza resume` to advance to next pipeline phase.

### Orphaned worktree
**Symptom**: `.worktrees/task-N/` exists but task is terminal.
**Diagnosis**: `liza validate` will flag this.
**Fix**: `liza wt-delete <task-id>`.

### Ghost agent
**Symptom**: Agent registered in state.yaml but process is dead.
**Diagnosis**: `liza get agents` — check lease expiry.
**Fix**: `liza recover-agent <agent-id>` or `liza delete agent <id>`.

## Validation Invariants

`liza validate` checks these (among others):
- IMPLEMENTING tasks must have `assigned_to`, `worktree`, and valid `lease_expires`
- REVIEWING tasks must have `reviewing_by`, `review_lease_expires`, and `review_commit`
- READY_FOR_REVIEW tasks must have `review_commit`
- REJECTED tasks must have `rejection_reason`
- BLOCKED tasks must have `blocked_reason`
- MERGED tasks must not have `worktree`
- No two agents assigned to the same task
- DRAFT tasks cannot have `assigned_to`
