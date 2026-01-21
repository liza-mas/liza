# Agent Initialization Protocol

## Overview

When `liza-agent.sh` spawns an agent, the agent must bootstrap itself from prompt to productive work. This document specifies that sequence.

---

## Invocation

Supervisor invokes Claude with:
```bash
LIZA_AGENT_ID="coder-1" claude "Mode: Liza coder"
# or with specific task:
LIZA_AGENT_ID="coder-1" claude "Mode: Liza coder" "Resume task: task-3"
```

Agent receives:
- **Environment variable:** `LIZA_AGENT_ID` (e.g., `coder-1`)
- **Initial prompt:** `"Mode: Liza {role}"` with optional task directive

---

## Contract Loading Chain

```
~/.claude/CLAUDE.md (symlink)
        │
        ▼
<project>/contracts/CORE.md
        │
        ├── Tier 0 invariants, shared rules
        │
        └── "Liza" in prompt? → read MULTI_AGENT_MODE.md
                │
                ▼
        MULTI_AGENT_MODE.md instructs:
        1. Extract role from prompt ("coder", "planner", "code-reviewer")
        2. Read identity from $LIZA_AGENT_ID
        3. Read specs/architecture/roles.md#{your-role}
        4. Follow Agent Startup Procedure below
```

---

## Agent Startup Procedure

### Phase 1: Identity Verification

```
1. Read $LIZA_AGENT_ID from environment
2. Read .liza/state.yaml
3. Verify own entry exists in agents section
4. Verify lease_expires is in future (supervisor registered us)
5. If verification fails → exit with error (supervisor handles)
```

**Agent Entry Structure (set by supervisor before agent spawn):**
```yaml
agents:
  coder-1:
    role: coder
    status: IDLE              # STARTING → IDLE by supervisor
    lease_expires: "..."      # now + 5 minutes
    heartbeat: "..."          # now
    terminal: "/dev/pts/0"    # or "unknown"
    iterations_total: 0       # Cumulative across agent restarts
    context_percent: 0        # Current context usage estimate
```

### Phase 2: Context Loading

```
1. Read specs/vision.md (goal context)
2. Read specs/architecture/roles.md#{your-role} (capabilities, constraints)
3. Read relevant protocol docs based on role:
   - Planner: task-lifecycle.md, circuit-breaker.md
   - Coder: task-lifecycle.md, worktree-management.md
   - Code Reviewer: task-lifecycle.md, worktree-management.md
```

### Phase 3: State Assessment

```
1. Parse .liza/state.yaml for:
   - Goal status and description
   - Task statuses (DRAFT, UNCLAIMED, CLAIMED, READY_FOR_REVIEW, etc.)
   - Other agents' states
   - Any PAUSE/CHECKPOINT signals
2. Check for handoff notes relevant to reclaimable tasks
3. Check for discoveries with urgency: immediate (Planner only)
```

### Phase 4: First Action Decision

Role-specific decision tree for what to do first.

---

## Role-Specific Startup

### Planner Startup

```
IF "Resume task:" in prompt:
    → Error: Planner doesn't claim tasks

IF goal.status == "PLANNING":
    → Continue decomposition (DRAFT → UNCLAIMED)

IF any task BLOCKED or INTEGRATION_FAILED:
    → Evaluate rescope (highest priority)

IF discoveries with urgency: immediate exist:
    → Convert to task or dismiss with rationale

IF all tasks in terminal state (MERGED, ABANDONED):
    → Assess goal completion, exit or create checkpoint

ELSE:
    → Monitor mode: wait for triggers, extend lease periodically
```

### Coder Startup

```
IF "Resume task:" in prompt:
    → Verify task exists and is claimable
    → Skip to claim step for that specific task

1. Scan for claimable tasks:
   - UNCLAIMED tasks (priority order)
   - CLAIMED tasks with expired lease (reclaimable)
   - REJECTED tasks assigned to self (continue iteration)

2. IF no claimable tasks:
   → Check for DRAFT tasks (Planner may finalize soon)
   → If DRAFT exists: wait briefly, re-scan
   → If no DRAFT: exit normally (supervisor handles)

3. Select task:
   - Prefer tasks with lower priority number
   - Prefer tasks without failed_by history
   - Prefer tasks matching own previous work (if REJECTED)

4. Claim task:
   a. Acquire lock on state.yaml
   b. Verify task still claimable (race check)
   c. Set assigned_to: $LIZA_AGENT_ID
   d. Set lease_expires: now + 5 minutes
   e. Set status: CLAIMED (if was UNCLAIMED)
   f. Set worktree: .worktrees/task-N (expected path)
   g. Release lock
   h. On failure: backoff 1-5s, retry up to 3x
   Note: All fields (c-f) MUST be set atomically in single yq command.
   See tooling.md for canonical example.

5. Setup worktree (wt-create.sh records base_commit at branch time):
   - Fresh claim: wt-create.sh task-N
   - Reassignment (different coder): wt-create.sh --fresh task-N
   - Same coder continuing: verify worktree exists

6. Read task's spec_ref document

7. Begin implementation loop (see task-lifecycle.md)
```

### Code Reviewer Startup

```
IF "Resume task:" in prompt:
    → Verify task is READY_FOR_REVIEW
    → Skip to review claim step

1. Scan for reviewable tasks:
   - READY_FOR_REVIEW without reviewing_by
   - READY_FOR_REVIEW with expired review_lease_expires

2. IF no reviewable tasks:
   → Exit normally (supervisor restarts when work appears)

3. Claim review:
   a. Acquire lock on state.yaml
   b. Verify task still reviewable (race check)
   c. Set reviewing_by: $LIZA_AGENT_ID
   d. Set review_lease_expires: now + 10 minutes
   e. Release lock
   f. On failure: backoff, retry

4. Verify commit SHA:
   - Read review_commit from task
   - Verify worktree HEAD matches
   - If mismatch: release claim, log error, try next task

5. Read task's spec_ref document

6. Begin review (see task-lifecycle.md#code-reviewer-protocol)
```

---

## Lease Maintenance

During operation, agents must maintain their lease:

```
Every 60 seconds (or before long operations):
1. Acquire lock
2. Update agents.{id}.heartbeat: now
3. Update agents.{id}.lease_expires: now + 5 minutes
4. Release lock
```

Before long operations (test suites, large builds):
```
1. Extend lease proactively: now + 15 minutes
2. Log: "Extended lease for long operation: {description}"
```

---

## Graceful Exit

When agent decides to exit:

```
IF work incomplete but context exhausted:
    1. Write handoff notes to .liza/state.yaml handoff section
    2. Exit with code 42 (graceful abort, supervisor restarts fresh agent)

IF work complete:
    1. Update task status appropriately
    2. Clear own lease (optional, expires anyway)
    3. Exit with code 0

IF error/violation detected:
    1. Log to anomalies section
    2. Exit with code 1 (supervisor restarts with delay)
```

---

## Sequence Diagram

```
Supervisor                    Agent                         Blackboard
    │                           │                               │
    │  register agent           │                               │
    │──────────────────────────────────────────────────────────>│
    │                           │                               │
    │  claude "Mode: Liza coder"│                               │
    │────────────────────────>  │                               │
    │                           │                               │
    │                           │  read CLAUDE.md → CORE.md     │
    │                           │  read MULTI_AGENT_MODE.md     │
    │                           │  read roles.md#coder          │
    │                           │                               │
    │                           │  read state.yaml              │
    │                           │<──────────────────────────────│
    │                           │                               │
    │                           │  verify identity              │
    │                           │                               │
    │                           │  scan for UNCLAIMED tasks     │
    │                           │                               │
    │                           │  claim task-3                 │
    │                           │──────────────────────────────>│
    │                           │                               │
    │                           │  wt-create.sh task-3          │
    │                           │                               │
    │                           │  read spec_ref                │
    │                           │                               │
    │                           │  [begin implementation]       │
    │                           │                               │
```

---

## Related Documents

- [Roles](../architecture/roles.md) — role capabilities and constraints
- [Task Lifecycle](task-lifecycle.md) — claim, iterate, review flow
- [Worktree Management](worktree-management.md) — worktree creation on claim
- [State Machines](../architecture/state-machines.md) — valid state transitions
- [Tooling](../implementation/tooling.md) — liza-agent.sh specification
