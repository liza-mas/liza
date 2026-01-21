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

**Planning Context:** The supervisor (`liza-agent.sh`) provides planners with computed sprint metrics and context-specific instructions. The planner receives its context in the bootstrap prompt and should follow the wake trigger's instructions.

```
1. Extract planning context from bootstrap prompt (=== PLANNING CONTEXT === section):
   - WAKE TRIGGER: reason planner was spawned
   - SPRINT STATE: computed metrics (total, merged, blocked, in_progress, etc.)
   - INSTRUCTIONS: trigger-specific guidance

2. Wake triggers and their meanings:
   - INITIAL_PLANNING: No tasks exist, decompose goal into tasks
   - BLOCKED_TASKS: Tasks are blocked, analyze blockers and resolve
   - INTEGRATION_FAILED: Merge/test failures, diagnose and plan fix
   - HYPOTHESIS_EXHAUSTED: Multiple coders failed same task, re-evaluate approach
   - IMMEDIATE_DISCOVERY: Urgent discoveries need triage

3. Follow instructions for current wake trigger

4. General decision tree (when no specific trigger):
   IF goal.status == "PLANNING":
       → Continue decomposition (DRAFT → UNCLAIMED)

   IF all tasks in terminal state (MERGED, ABANDONED):
       → Assess goal completion, exit or create checkpoint

   ELSE:
       → Monitor mode: wait for triggers, extend lease periodically
```

**Note:** Planners don't claim tasks. The supervisor spawns planners when planning attention is needed (blocked tasks, discoveries, etc.).

### Coder Startup

**Task Assignment:** The supervisor (`liza-agent.sh`) claims tasks and creates worktrees BEFORE spawning the agent. This avoids permission prompts in non-interactive mode. The coder receives its assigned task in the bootstrap prompt and should NOT attempt to claim tasks directly.

```
1. Extract task from bootstrap prompt (=== ASSIGNED TASK === section):
   - TASK ID: task identifier
   - WORKTREE: absolute path to worktree directory
   - DESCRIPTION, DONE WHEN, SCOPE: task details
   - INSTRUCTIONS: role-specific guidance

2. Verify assignment:
   - Read task from state.yaml
   - Verify status is CLAIMED
   - Verify assigned_to matches $LIZA_AGENT_ID
   - Verify worktree directory exists

3. IF verification fails:
   → Log error to handoff
   → Exit (supervisor will investigate)

4. Read task's spec_ref document

5. Begin implementation loop (see task-lifecycle.md)
```

**Note:** The supervisor handles task selection, claiming, and worktree creation using a two-phase commit pattern that prevents invalid intermediate states. See roles.md for task assignment details.

### Code Reviewer Startup

**Review Assignment:** The supervisor (`liza-agent.sh`) assigns review tasks to Code Reviewers BEFORE spawning the agent, similar to Coder task assignment. This avoids permission prompts in non-interactive mode.

```
1. Extract review task from bootstrap prompt (=== REVIEW TASK === section):
   - TASK ID: task identifier
   - WORKTREE: absolute path to worktree directory
   - COMMIT TO REVIEW: SHA to verify
   - AUTHOR: original coder's agent ID
   - DESCRIPTION, DONE WHEN: task details
   - INSTRUCTIONS: review-specific guidance

2. Verify assignment:
   - Read task from state.yaml
   - Verify status is READY_FOR_REVIEW
   - Verify reviewing_by matches $LIZA_AGENT_ID

3. IF verification fails:
   → Log error to handoff
   → Exit (supervisor will investigate)

4. Verify commit SHA:
   - Read review_commit from task
   - Verify worktree HEAD matches
   - If mismatch: log error, exit

5. Read task's spec_ref document

6. Begin review (see task-lifecycle.md#code-reviewer-protocol)
```

**Note:** The supervisor handles review task selection and claiming. See roles.md for review assignment details.

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

## Sequence Diagram (Coder)

```
Supervisor                    Agent                         Blackboard
    │                           │                               │
    │  find claimable task      │                               │
    │──────────────────────────────────────────────────────────>│
    │                           │                               │
    │  liza-claim-task.sh       │                               │
    │  (two-phase commit)       │                               │
    │──────────────────────────────────────────────────────────>│
    │                           │                               │
    │  spawn claude with        │                               │
    │  task info in prompt      │                               │
    │────────────────────────>  │                               │
    │                           │                               │
    │                           │  read CLAUDE.md → CORE.md     │
    │                           │  read MULTI_AGENT_MODE.md     │
    │                           │                               │
    │                           │  extract task from prompt     │
    │                           │  (TASK ID, WORKTREE)          │
    │                           │                               │
    │                           │  verify assignment            │
    │                           │<──────────────────────────────│
    │                           │                               │
    │                           │  read spec_ref                │
    │                           │                               │
    │                           │  [begin implementation]       │
    │                           │                               │
```

**Key difference from self-claiming:** The supervisor claims the task and creates the worktree BEFORE spawning the agent. The agent receives pre-claimed task info in its bootstrap prompt.

---

## Related Documents

- [Roles](../architecture/roles.md) — role capabilities and constraints
- [Task Lifecycle](task-lifecycle.md) — claim, iterate, review flow
- [Worktree Management](worktree-management.md) — worktree creation on claim
- [State Machines](../architecture/state-machines.md) — valid state transitions
- [Tooling](../implementation/tooling.md) — liza-agent.sh specification
