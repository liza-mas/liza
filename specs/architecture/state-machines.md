# State Machines

## Task State Machine

| State | Description | Valid Transitions |
|-------|-------------|-------------------|
| DRAFT | Task being defined by planner | → UNCLAIMED |
| UNCLAIMED | Task ready, no agent assigned | → CLAIMED |
| CLAIMED | Coder assigned, work in progress | → READY_FOR_REVIEW, BLOCKED |
| READY_FOR_REVIEW | Coder done, awaiting Code Reviewer | → APPROVED, REJECTED |
| REJECTED | Code Reviewer rejected, feedback provided | → CLAIMED (supervisor re-claims for same coder) |
| APPROVED | Code Reviewer approved, merge eligible | → MERGED, INTEGRATION_FAILED |
| MERGED | Successfully merged to integration | Terminal |
| BLOCKED | Cannot proceed, awaiting escalation | → UNCLAIMED (rescoped), SUPERSEDED, ABANDONED |
| SUPERSEDED | Replaced by rescoped task(s) | Terminal |
| ABANDONED | Planner killed task | Terminal |
| INTEGRATION_FAILED | Merge conflict or integration test failure | → CLAIMED (integration-fix scope) |

### Task State Diagram

```
     ┌──────────────────────────────────────────┐
     │              DRAFT                       │
     │  (Planner writing, coders must ignore)   │
     └──────────────────┬───────────────────────┘
                        │ finalize
                        ▼
     ┌──────────────────────────────────────────┐
     │             UNCLAIMED                    │
     │       (Available for claim)              │
     └──────────────────┬───────────────────────┘
                        │ claim
                        ▼
     ┌──────────────────────────────────────────┐
     │              CLAIMED                     │
     │       (Coder working)                    │
     └─────────┬────────────────────┬───────────┘
               │                    │
     request_review             blocked
               │                    │
               ▼                    ▼
     ┌─────────────────┐    ┌──────────────┐
     │ READY_FOR_REVIEW│    │   BLOCKED    │
     └────────┬────────┘    └──────┬───────┘
              │                    │
       ┌──────┴──────┐      ┌──────┴──────┐
       │             │      │             │
    approve       reject  rescope     abandon
       │             │      │             │
       ▼             ▼      ▼             ▼
┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐
│ APPROVED │  │ REJECTED │  │SUPERSEDED│  │ABANDONED │
└────┬─────┘  └────┬─────┘  └──────────┘  └──────────┘
     │             │              Terminal      Terminal
     │             │
     │      resume (CLAIMED)
     │
     ├─────────────┐
     │             │
   merge      conflict
     │             │
     ▼             ▼
┌──────────┐  ┌───────────────────┐
│  MERGED  │  │ INTEGRATION_FAILED│
└──────────┘  └─────────┬─────────┘
   Terminal             │
                   claim (integration-fix)
                        │
                        ▼
                    CLAIMED
```

### Forbidden Transitions

- DRAFT → CLAIMED (coders cannot claim drafts)
- CLAIMED → MERGED (skipping review)
- CLAIMED → APPROVED (self-approval)
- REJECTED → APPROVED (without addressing feedback)
- Any terminal state → Any other state (MERGED, ABANDONED, SUPERSEDED are final)

### Transition Requirements

| Transition | Must Set | Must Preserve | Notes |
|------------|----------|---------------|-------|
| REJECTED → CLAIMED (same coder) | `lease_expires` (new) | `worktree`, `review_cycles_current`, `review_cycles_total` | Supervisor re-claims for same coder |
| REJECTED → CLAIMED (different coder) | `lease_expires`, `assigned_to`, `review_cycles_current: 0` | `review_cycles_total` | Worktree reset: delete old, create fresh |
| INTEGRATION_FAILED → CLAIMED | `lease_expires`, `integration_fix: true` | `worktree` | Any coder may claim; keeps worktree for conflict resolution |
| BLOCKED → UNCLAIMED | — | `failed_by` | Preserves failure history; worktree deleted |
| UNCLAIMED → CLAIMED (reassignment) | `lease_expires`, `assigned_to`, `review_cycles_current: 0` | `failed_by`, `review_cycles_total` | Fresh worktree created |
| Any → MERGED | — | — | Must clear `worktree` (cleanup) |

**Review Cycles on Reassignment:**
- `review_cycles_current` resets to 0 when `assigned_to` changes — new coder gets full budget
- `review_cycles_total` never resets — preserves audit trail for retrospectives
- Limit checks (deadlock detection) use `review_cycles_current`

**Worktree on Reassignment:**
- Same coder continuing: worktree preserved (iterating on own work)
- Different coder taking over: worktree deleted and recreated fresh
- Rationale: salvaging failed work often costs more than restarting from spec (see vision.md)
- Exception: INTEGRATION_FAILED keeps worktree (conflict resolution requires seeing the failed merge state)

**Validation Note:** `liza-validate.sh` should verify that CLAIMED tasks always have valid `lease_expires`, regardless of prior state.

---

## Review Lease Lifecycle

Tasks in READY_FOR_REVIEW state have an independent lease for the Code Reviewer:

| Condition | Review Claimable? |
|-----------|-------------------|
| `reviewing_by` is null | Yes |
| `reviewing_by` set, `review_lease_expires` in past | Yes (stale claim) |
| `reviewing_by` set, `review_lease_expires` in future | No (active claim) |

```
READY_FOR_REVIEW
    │
    ├── reviewing_by: null ──► Supervisor assigns review (sets reviewing_by, review_lease_expires)
    │                              │
    │                              ▼
    │                         reviewing_by: code-reviewer-1
    │                         review_lease_expires: future
    │                              │
    │                    ┌─────────┴─────────┐
    │                    │                   │
    │               verdict              lease expires
    │                    │                   │
    │                    ▼                   ▼
    │            APPROVED/REJECTED      reviewing_by: null
    │            (lease cleared)        (reclaimable)
    │
    └── Supervisor may reassign if lease expired
```

**Key invariants:**
- Supervisor claims review before spawning Code Reviewer (prevents duplicate reviews)
- Code Reviewer must extend lease with heartbeats during long reviews
- On verdict: clear `reviewing_by` and `review_lease_expires`
- Stale review lease allows supervisor to reassign (Code Reviewer crash recovery)

See [Blackboard Schema — Lease Model](blackboard-schema.md#lease-model) for field details.

---

## Agent State Machine

| State | Description | Valid Transitions |
|-------|-------------|-------------------|
| STARTING | Agent registered, initializing | → IDLE |
| IDLE | No task assigned | → WORKING (Coder), REVIEWING (Code Reviewer) |
| WORKING | Coder actively implementing task | → WAITING, IDLE, HANDOFF |
| REVIEWING | Code Reviewer actively reviewing task | → IDLE (verdict done), HANDOFF |
| WAITING | Coder waiting for review result or escalation | → WORKING (continue after feedback), IDLE (task done) |
| HANDOFF | Context exhaustion, preparing handoff notes | → (agent terminates, supervisor restarts fresh) |

**Role-specific states:**
- WORKING and WAITING: Coder only
- REVIEWING: Code Reviewer only
- STARTING, IDLE, HANDOFF: All roles

### Agent State Diagram

```
┌───────────┐
│ STARTING  │
│           │
└─────┬─────┘
      │ registration complete
      ▼
┌───────────┐
│   IDLE    │◄──────────────────────────────┐
│           │                               │
└─────┬─────┘                               │
      │                                     │
      ├── assigned task (Coder)             │
      │         ▼                           │
      │   ┌───────────┐                     │
      │   │  WORKING  │─────────────────────┤
      │   │  (Coder)  │    task done/       │
      │   └─────┬─────┘    lost lease       │
      │         │                           │
      │         │ submitted for review      │
      │         ▼                           │
      │   ┌───────────┐      REJECTED       │
      │   │  WAITING  │◄────────────────────┤
      │   │  (Coder)  │                     │
      │   └─────┬─────┘                     │
      │         │                           │
      │         ├── APPROVED ───────────────┤
      │         │                           │
      │         │ context exhaustion        │
      │         ▼                           │
      │   ┌───────────┐                     │
      │   │  HANDOFF  │─────────────────────┘
      │   │           │    (terminated)
      │   └───────────┘
      │
      └── assigned review (Code Reviewer)
                ▼
          ┌───────────┐
          │ REVIEWING │─────────────────────┐
          │(Reviewer) │    verdict done/    │
          └─────┬─────┘    lost lease       │
                │                           │
                │ context exhaustion        │
                ▼                           │
          ┌───────────┐                     │
          │  HANDOFF  │─────────────────────┘
          │           │    (terminated)
          └───────────┘
```

**Registration Failure:** If agent registration fails (ID collision with active lease), the agent process exits immediately with error—it never enters the state machine. See [Roles — Agent Identity Protocol](roles.md#agent-identity-protocol).

---

## Goal State Machine

| State | Description | Valid Transitions |
|-------|-------------|-------------------|
| IN_PROGRESS | Goal active, sprints ongoing | → COMPLETED, ABORTED |
| COMPLETED | All planned work done | Terminal |
| ABORTED | Human stopped goal | Terminal |

Goals span sprints. Unlike sprints, goals have no CHECKPOINT state — checkpoint is a sprint-level concern.

---

## Sprint State Machine

| State | Description | Valid Transitions |
|-------|-------------|-------------------|
| IN_PROGRESS | Sprint active, work ongoing | → CHECKPOINT, COMPLETED, ABORTED |
| CHECKPOINT | Mandatory human review | → IN_PROGRESS (continue), ABORTED |
| COMPLETED | All planned tasks terminal | Terminal |
| ABORTED | Human or circuit breaker stopped | Terminal |

### Sprint Transition Triggers

| Trigger | Target State | Notes |
|---------|--------------|-------|
| All planned tasks terminal | COMPLETED | Normal completion |
| Calendar deadline reached | CHECKPOINT | Human decides continue/abort |
| Human creates `.liza/CHECKPOINT` | CHECKPOINT | Manual review request |
| Circuit breaker triggered | ABORTED | Systemic issue detected |
| Human creates `.liza/ABORT` | ABORTED | Manual termination |

**From CHECKPOINT:**
- Human removes CHECKPOINT file → IN_PROGRESS (continue)
- Human creates ABORT file → ABORTED (stop)

---

## Exit Codes

| Code | Meaning | Supervisor Action |
|------|---------|-------------------|
| 0 | Role-complete (see below) | Stop supervisor |
| 42 | Graceful abort (context exhaustion, lease lost, invariant) | Restart immediately |
| Other | Crash | Restart with backoff |

### Exit Code 0 Semantics

Exit 0 signals "this agent type has no more work to do." Supervisor should stop (not restart).

| Role | Exit 0 When |
|------|-------------|
| Planner | All tasks in terminal state, no blocked tasks, goal complete |
| Coder | No UNCLAIMED tasks AND no REJECTED tasks assigned to this agent |
| Code Reviewer | No READY_FOR_REVIEW tasks |

**Note:** Exit 0 does NOT mean "task done" (use state transitions for that). It means "role complete for this goal/sprint."

### DRAFT Task Waiting

When a Coder or Code Reviewer finds no work and would exit 0, the supervisor first checks for DRAFT tasks:

```
if agent_exit_code == 0 and count(DRAFT tasks) > 0:
    log("Agent found no work, but N DRAFT task(s) exist. Waiting for Planner...")
    sleep(coder_poll_interval)  # Configurable, default 30s
    restart_agent()  # Re-check after Planner may have finalized
```

**Rationale:** DRAFT tasks will become UNCLAIMED when the Planner finalizes them. Rather than stopping the supervisor (exit 0 behavior), we wait briefly and re-check. This avoids a race where:
1. Coder sees no UNCLAIMED tasks → exit 0
2. Planner finalizes DRAFT → UNCLAIMED
3. No Coder running to claim the newly-available task

The delay is configurable via `config.coder_poll_interval` (default 30s) — long enough for Planner to finalize without busy-waiting, short enough for reasonable responsiveness.

### Supervisor Backoff Timing

| Scenario | Delay | Rationale |
|----------|-------|-----------|
| Exit 42 (graceful abort) | 2s | Brief pause before restart |
| Crash (any exit code except 0, 42) | 5s | Recovery delay for unexpected failures |
| Exit 0 with DRAFT tasks | `coder_poll_interval` (30s) | Wait for Planner to finalize, then re-check |

**v1 Simplification:** No exponential backoff or state-change detection. Simple fixed delays are sufficient for v1 scale. Exponential backoff for persistent failures deferred to v2.

### Graceful Abort Triggers (Exit 42)

Agent should exit with code 42 when:
- Context reaches 90% capacity (set `handoff_pending: true` first)
- Lease expired while working
- Tier 0 invariant violated
- PAUSE file detected
- ABORT file detected
- CHECKPOINT file detected

### Exit 42 with Handoff

When exit 42 is triggered by context exhaustion:

1. Agent sets `handoff_pending: true` on task
2. Agent writes handoff notes to blackboard
3. Agent exits with code 42
4. Supervisor restarts agent process (fresh LLM context)
5. On startup, restarted agent checks `handoff_pending`:
   - `true` → read handoff, clear flag, resume work
   - `false` → normal startup (exit was for pause/checkpoint, not context)

**Distinction:** Exit 42 always restarts the agent process. The `handoff_pending` flag tells the restarted agent whether to read handoff notes or proceed normally.

---

## Validation Rules

```yaml
task_states:
  valid:
    - DRAFT
    - UNCLAIMED
    - CLAIMED
    - READY_FOR_REVIEW
    - REJECTED
    - APPROVED
    - MERGED
    - BLOCKED
    - ABANDONED
    - SUPERSEDED
    - INTEGRATION_FAILED
  terminal:
    - MERGED
    - ABANDONED
    - SUPERSEDED

agent_states:
  - STARTING
  - IDLE
  - WORKING
  - REVIEWING  # Code Reviewer only
  - WAITING
  - HANDOFF

invariants:
  - "DRAFT task cannot have assigned_to"
  - "CLAIMED task must have assigned_to"
  - "CLAIMED task must have worktree"
  - "CLAIMED task must have valid lease_expires"
  - "READY_FOR_REVIEW task must have review_commit"
  - "REJECTED task must have rejection_reason"
  - "BLOCKED task must have blocked_reason"
  - "SUPERSEDED task must have superseded_by and rescope_reason"
  - "MERGED task must not have worktree"
  - "Agent WORKING must have task"
  - "Agent WORKING must have lease_expires in future (or within grace period of 60s)"
  - "No two agents assigned to same task"
  - "Task with integration_fix:true must have prior INTEGRATION_FAILED in history"
```

## Related Documents

- [Task Lifecycle](../protocols/task-lifecycle.md) — operational flow
- [Blackboard Schema](blackboard-schema.md) — state.yaml structure
- [Roles](roles.md) — who can trigger which transitions
