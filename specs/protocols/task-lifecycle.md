# Task Lifecycle Protocol

## Overview

Tasks flow through a lifecycle managed by Planner, Coder, and Code Reviewer roles. Each transition has explicit triggers and validation requirements.

For state diagrams and valid transitions, see [State Machines](../architecture/state-machines.md).

## Iteration Protocol

### Ralph-Style Loop

Coder iterates until externally approved:

```
while task.state != APPROVED and iterations < max_iterations:
    extend_lease()
    work on task
    log_anomalies_as_they_occur()  # see roles.md for required types
    if ready:
        ensure_clean_git_status()
        record_commit_sha()
        request_review()
        # Wait model: exit and let supervisor restart
        exit(42)  # supervisor restarts; on restart, re-read blackboard for verdict

# After restart, check verdict:
if task.state == REJECTED:
    read feedback
    iterations++
    # continue loop on next restart

if iterations >= max_iterations and task.state != APPROVED:
    mark_blocked("max iterations reached without approval")
    exit(42)
```

**Wait Model:** Agents do not block waiting for verdicts. After requesting review, the coder exits with code 42. The supervisor restarts the agent, which re-reads the blackboard to discover the verdict. This stateless restart model is fundamental to Liza's design.

**Logging:** Coder MUST log anomalies as they occur (not at end of task). See [Roles](../architecture/roles.md#coder-logging-duties) for required anomaly types.

### Iteration Limits

| Role | Default Max | Rationale |
|------|-------------|-----------|
| Coder | 10 | Enough for complex tasks, bounded |
| Code Reviewer | 1 per review | Review should be decisive |
| Review cycles | 5 | Coder-Code Reviewer loop cap |

### Early Warning Thresholds

`liza-watch.sh` alerts before limits are hit (trajectory visibility):

| Metric | Warning | Cliff | Condition |
|--------|---------|-------|-----------|
| Coder iterations | 8 | 10 | Always |
| Review cycles | 3 | 5 | Always |
| Coder failures | 1 | 2 | Only if review_cycles ≥ 3 |

The third condition avoids noise on isolated recoverable failures — a single coder failure with few review cycles is likely recoverable.

---

## Hypothesis Exhaustion Rule

If same task is BLOCKED by two different coders:

1. Task framing presumed wrong
2. Task cannot be reassigned unchanged
3. Planner must: rescope (→ SUPERSEDED), split, or abandon

This prevents infinite polite failure.

Tracked via:
```yaml
tasks:
  - id: task-3
    failed_by:
      - coder-1  # first failure
      - coder-2  # second failure → hypothesis exhaustion
```

---

## Rescoping Audit Trail

When planner rescopes a blocked task:

1. Original task → `SUPERSEDED`
2. New task(s) created with:
   - `supersedes: [original-task-id]`
   - `rescope_reason: [why — wrong granularity | ambiguity | missing dependency | ...]`
3. Log entry records the rescope

Original task history is preserved. No silent rewrites.

---

## Blocked Escalation

| Condition | Escalation |
|-----------|------------|
| Coder BLOCKED | Planner notified, may rescope |
| Code Reviewer and Coder deadlocked (5 cycles) | Planner intervenes (see Review Deadlock Protocol) |
| Integration failed | Task reclaimable with integration-fix scope |
| Two coders failed same task | Hypothesis exhaustion → mandatory rescope |

### Review Deadlock Protocol

When Coder and Code Reviewer reach `max_review_cycles` (default: 5) without approval:

1. **Task transitions to BLOCKED** with `blocked_reason: "review_deadlock"`
2. **Planner evaluates** the rejection/revision history:

| Planner Assessment | Action |
|--------------------|--------|
| Coder not addressing feedback | Reassign to different coder (preserves worktree) |
| Code Reviewer criteria unclear/shifting | Clarify spec, reset review_cycles, same coder continues |
| Genuine disagreement on approach | Rescope task with clearer constraints |
| Task fundamentally misframed | SUPERSEDED, create replacement task(s) |
| No viable path forward | ABANDONED (requires rationale in log) |

3. **Planner must log** `review_deadlock` anomaly with assessment
4. **Work is NOT discarded** unless Planner explicitly chooses ABANDONED after assessment

**Key invariant:** The Coder-Code Reviewer loop runs to completion (5 cycles) before any intervention. No premature escalation.

### Integration-Fix Protocol

When merge fails (INTEGRATION_FAILED):

1. **Any coder may claim** — not restricted to original coder
2. **Worktree is reused** — contains the conflicting state
3. **Claimer must set** `integration_fix: true` on claim
4. **Scope is limited** — resolve conflict only, no new features
5. **After resolution** — normal review cycle applies

```bash
# Claim integration-fix task
~/.claude/scripts/liza-lock.sh modify "
  yq -i '(.tasks[] | select(.id == \"task-3\" and .status == \"INTEGRATION_FAILED\")) |=
    (.status = \"CLAIMED\" | .assigned_to = \"coder-2\" | .integration_fix = true |
     .lease_expires = \"$(date -u -d '+5 minutes' +%Y-%m-%dT%H:%M:%SZ)\")' .liza/state.yaml
"
```

---

## Scope Discipline

### Task Scope is Hard Boundary

- Work only on claimed task
- No modifications outside task scope, even if "obviously needed"
- No "while I'm here" fixes
- No prerequisite work unless explicitly part of task

### Discovery Protocol

If coder discovers adjacent problem:

1. Do not fix
2. Log to blackboard as potential new task:

```yaml
discovered:
  - id: disc-1
    by: coder-1
    during: task-3
    description: "API client has no timeout, could hang indefinitely"
    severity: high
    recommendation: "New task: add configurable timeout"
```

3. Continue with original task

Planner decides whether to create new task.

---

## Context Exhaustion Handoff

At 90% context capacity:

1. STOP at next safe point (not mid-edit)
2. Commit any pending changes
3. Write structured handoff to blackboard:

```yaml
handoff:
  task: task-3
  agent: coder-1
  context_used: 92%
  timestamp: 2025-01-17T15:00:00Z
  # REQUIRED (1 phrase max each — degraded agent can still produce this)
  summary: "Implementing retry logic, core mechanism done"
  next_action: "Add exponential backoff for 429 responses"
  # OPTIONAL (include if context allows)
  approach: "Using tenacity library. Decorator pattern."
  blockers: "Edge case: API returns 429 with Retry-After"
  files_modified:
    - src/api/client.py
    - tests/test_client.py
  next_steps:
    - Add exponential backoff
    - Handle 429 with Retry-After header
```

### Handoff Field Requirements

| Field | Required | Constraint | Rationale |
|-------|----------|------------|-----------|
| `summary` | Yes | 1 phrase max | What state is the task in? |
| `next_action` | Yes | 1 phrase max | What should replacement do first? |
| `approach` | No | — | How was it being implemented? |
| `blockers` | No | — | What's blocking progress? |
| `files_modified` | No | — | Which files were touched? |
| `next_steps` | No | — | Remaining work items |

**Rationale:** An agent at 90% context is degraded but can still produce two phrases. Required fields bound the minimum viable handoff; optional fields capture richer context when available. This doesn't solve degradation but bounds its impact on handoff quality.

4. Set `handoff_pending: true` on task in blackboard
5. Exit with code 42
6. Supervisor restarts agent process (fresh context)
7. On startup, agent checks task's `handoff_pending` flag:
   - If `true` AND agent ID matches handoff → clear flag, read handoff, resume
   - If `true` AND agent ID differs → this is the replacement agent, read handoff, claim task
   - If `false` → normal startup (context exhaustion was for different reason)

**Note:** "Fresh agent" means fresh LLM context, not necessarily different agent ID. The supervisor restarts the same agent process; whether it's the "same" agent depends on whether handoff was to self or replacement.

### Context Tracking (v1 Limitation)

Claude Code does not expose token usage programmatically. The `context_percent` field in the blackboard is aspirational for v1.

**v1 Approach: Iteration Proxy**

Instead of measuring context, agents use iteration count as proxy for context pressure:

- After N iterations on a single task without resolution → consider handoff
- If response quality degrades noticeably → initiate handoff
- Agent self-reports: "Context feels constrained, initiating handoff"

The 90% trigger becomes heuristic, not measured:
- Many tool calls in sequence
- Repeated re-reading of same files
- Difficulty holding task state

**Handoff remains mandatory behavior.** The trigger is approximate.

---

## Integration-Fix Ownership

See [Worktree Management — Integration-Fix Ownership](worktree-management.md#integration-fix-ownership) for the canonical definition.

---

## Session Initialization

### Human Bootstrap Sequence

Before agents can run, human must initialize the project:

1. **Initialize Liza state:** `liza-init.sh <project-dir>`
   - Creates `.liza/` directory structure
   - Creates empty `state.yaml` with config defaults
   - Creates `log.yaml`

2. **Write specs:** Create `specs/vision.md` and any required specs
   - Planner cannot decompose goal without vision document

3. **Define goal:** Add goal to `state.yaml` (via script or manual edit)
   ```yaml
   goal:
     id: goal-1
     description: "..."
     status: IN_PROGRESS
   ```

4. **Start watcher:** `liza-watch.sh` in separate terminal
   - Monitors for CHECKPOINT, anomalies, circuit breaker triggers

5. **Start agents:** Launch Planner, then Coders/Code Reviewers as needed
   - Each in separate terminal for observation

### Agent Startup Sequence

1. Read `~/.claude/CLAUDE.md` → symlink to `<project>/contracts/CORE.md`
2. CORE.md contains universal rules and mode selection
3. State: `"Mode: Liza [role]"` (Planner/Coder/Code Reviewer)
4. Read `contracts/MULTI_AGENT_MODE.md` (Liza-specific rules)
5. Read project context: `REPOSITORY.md`, `specs/`, relevant docs
6. Read `.liza/state.yaml`
7. Check for PAUSE/ABORT/CHECKPOINT files → if found, exit(42) immediately
8. Read `human_notes` if present
9. **Verify lease if resuming task** — abort if lease lost
10. Read `handoff` notes if present for assigned task
11. Role-specific initialization (below)
12. Announce ready: `"[Role] ready. Reading blackboard."`

### Planner Initialization

1. Read specs to understand goal context
2. If no goal defined: exit(42) — human must define goal via bootstrap sequence
3. If goal defined but no tasks: decompose into tasks (write as DRAFT first)
4. Verify specs exist for tasks; if not, flag for human
5. Finalize DRAFT → UNCLAIMED when plan complete
6. If tasks exist: monitor for blocked/escalation conditions
7. Write initial goal-alignment summary

### Coder Initialization

1. Read specs relevant to available tasks
2. Check for REJECTED task assigned to self → resume
3. Check for handoff notes for any reclaimable task → claim and resume
4. Scan for UNCLAIMED tasks → claim first available (with backoff)
5. If task under-specified (no clear spec) → BLOCKED with clarifying questions (see [Blocking Protocol](../architecture/roles.md#blocking-protocol))
6. If none available: wait or exit
7. After claim: create/enter worktree, begin iteration loop

### Code Reviewer Initialization

1. Scan for READY_FOR_REVIEW tasks
2. If found: claim review, verify commit SHA
3. Read specs relevant to task (using task's `spec_ref`)
4. If none: wait for review requests or exit
5. On claim: examine worktree, validate against spec and `done_when` criteria, run validations, produce verdict
6. On approval: execute merge

## Related Documents

- [Agent Initialization](agent-initialization.md) — startup sequence from spawn to first action
- [State Machines](../architecture/state-machines.md) — state transitions
- [Roles](../architecture/roles.md) — role responsibilities
- [Worktree Management](worktree-management.md) — worktree operations
- [Sprint Governance](sprint-governance.md) — checkpoints, retrospectives
