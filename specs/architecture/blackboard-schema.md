# Blackboard Schema

## Location

`.liza/` in project root.

## Files

| File | Purpose | Write Pattern |
|------|---------|---------------|
| `state.yaml` | Current state | Atomic read-modify-write |
| `log.yaml` | Activity history | Append-only |
| `alerts.log` | Persistent watcher alerts | Append-only |
| `archive/` | Terminal-state tasks older than threshold | Periodic pruning |
| `circuit_breaker_report.md` | CB trigger report | Write-once per trigger |
| `ESCALATION` | Stale checkpoint notification | Overwrite by watcher |

### Sections within state.yaml

| Section | Purpose | Write Pattern |
|---------|---------|---------------|
| `anomalies` | Execution observations | Append by Coders/Code Reviewers |
| `spec_changes` | Spec modification history | Append-only |
| `sprint` | Current sprint state | Atomic update |
| `circuit_breaker` | CB status and history | Atomic update |

---

## Timestamp Format

All timestamps in state.yaml and log.yaml use **ISO 8601 format in UTC** with `Z` suffix:
- Format: `YYYY-MM-DDTHH:MM:SSZ`
- Example: `2025-01-17T14:00:00Z`
- Generate with: `date -u +%Y-%m-%dT%H:%M:%SZ`

---

## state.yaml Schema

```yaml
# .liza/state.yaml

version: 1

goal:
  id: goal-1
  description: "Implement retry logic for all API calls with exponential backoff"
  created: 2025-01-17T14:00:00Z
  status: IN_PROGRESS  # Goal status: IN_PROGRESS, COMPLETED, ABORTED (no CHECKPOINT — goals span sprints)
  alignment_history:  # Append-only — preserves drift trajectory through rescopes
    - timestamp: 2025-01-17T14:00:00Z
      event: initialization
      summary: |
        Initial alignment: 5 API endpoints need retry logic.
        Approach: tenacity library with exponential backoff.
    - timestamp: 2025-01-17T16:30:00Z
      event: rescope_task-4
      summary: |
        Current: Basic retry decorator implemented for 2/5 API endpoints.
        Change: task-4 split into auth/validation subtasks (scope was too broad).
        Remaining: 3 endpoints, exponential backoff config, integration tests.
        Risk: None identified.

tasks:
  - id: task-1
    description: "Add retry decorator to UserAPI.get_user()"
    status: MERGED
    priority: 1
    created: 2025-01-17T14:05:00Z
    worktree: null  # cleaned up after merge
    review_commit: a1b2c3d4
    spec_ref: specs/retry-logic.md  # Path to spec, optionally with #anchor
    done_when: "UserAPI.get_user() retries 3x on 5xx errors with exponential backoff"
    history:
      - { time: "2025-01-17T14:05:00Z", event: "created" }
      - { time: "2025-01-17T14:06:00Z", event: "claimed", agent: "coder-1" }
      - { time: "2025-01-17T14:25:00Z", event: "ready_for_review", commit: "a1b2c3d4" }
      - { time: "2025-01-17T14:28:00Z", event: "approved", agent: "code-reviewer-1" }
      - { time: "2025-01-17T14:29:00Z", event: "merged" }

  - id: task-2
    description: "Add retry decorator to OrderAPI.create_order()"
    status: REJECTED
    priority: 2
    assigned_to: coder-1
    worktree: .worktrees/task-2
    iteration: 3  # Current iteration within this task (Ralph loop count)
    review_cycles_current: 2  # Reset to 0 on coder reassignment
    review_cycles_total: 2    # Never reset (audit trail)
    review_commit: b2c3d4e5
    spec_ref: specs/retry-logic.md
    done_when: "OrderAPI.create_order() retries only after idempotency key validation"
    rejection_reason: "Retry should not apply to non-idempotent POST. Add idempotency check first. See spec section 3.2."
    created: 2025-01-17T14:05:00Z

  - id: task-3
    description: "Add retry decorator to PaymentAPI.charge()"
    status: DRAFT  # Planner still defining — missing done_when keeps it DRAFT
    priority: 3
    worktree: null
    spec_ref: specs/retry-logic.md#payments
    # done_when: TBD — intentionally incomplete to show DRAFT state requirement
    # Planner must define done_when before finalizing to UNCLAIMED
    created: 2025-01-17T15:00:00Z

  - id: task-4
    description: "Original task that was too broad"
    status: SUPERSEDED
    superseded_by: [task-4a, task-4b]
    rescope_reason: "Wrong granularity — split into auth and validation subtasks"
    priority: 2
    created: 2025-01-17T14:10:00Z

  - id: task-4a
    description: "Add auth retry logic"
    status: UNCLAIMED
    supersedes: task-4
    priority: 2
    depends_on: []  # No dependencies — can be claimed immediately
    spec_ref: specs/retry-logic.md#auth
    done_when: "Auth endpoints return 401 on invalid token and retry succeeds after token refresh"
    created: 2025-01-17T14:50:00Z

  - id: task-4b
    description: "Add validation retry logic"
    status: UNCLAIMED
    supersedes: task-4
    priority: 3
    depends_on: [task-4a]  # Blocked until task-4a is MERGED
    spec_ref: specs/retry-logic.md#validation
    done_when: "Validation endpoints retry on transient errors only"
    created: 2025-01-17T14:50:00Z

  - id: task-6
    description: "Add rate limit handling"
    status: BLOCKED
    priority: 3
    assigned_to: null
    worktree: null
    spec_ref: specs/retry-logic.md#rate-limits
    done_when: "429 responses trigger backoff; Retry-After header respected"
    blocked_reason: "Two coders failed — hypothesis exhaustion triggered"
    blocked_questions:
      - "Is the rate limit spec incomplete?"
      - "Should we split into detection vs handling subtasks?"
    failed_by: [coder-1, coder-2]  # Tracks hypothesis exhaustion
    created: 2025-01-17T16:00:00Z

  - id: task-7
    description: "Fix merge conflict in UserAPI retry logic"
    status: CLAIMED
    priority: 1
    assigned_to: coder-1
    worktree: .worktrees/task-7
    base_commit: a1b2c3d4  # Integration HEAD at claim time (drift tracking)
    spec_ref: specs/retry-logic.md
    done_when: "Merge conflict resolved; integration tests pass"
    integration_fix: true  # This task fixes a prior INTEGRATION_FAILED
    handoff_pending: false  # Set true on context exhaustion; cleared when new agent reads handoff
    created: 2025-01-17T16:30:00Z
    history:
      - { time: "2025-01-17T16:20:00Z", event: "integration_failed", task: "task-1-retry" }
      - { time: "2025-01-17T16:30:00Z", event: "created", note: "integration-fix scope" }
      - { time: "2025-01-17T16:31:00Z", event: "claimed", agent: "coder-1" }

  - id: task-5
    description: "Add pagination to list endpoints"
    status: BLOCKED
    priority: 2
    assigned_to: coder-2
    worktree: .worktrees/task-5
    spec_ref: specs/api.md#list-endpoints
    done_when: "List endpoints accept cursor param and return next_cursor in response"
    blocked_reason: "Spec doesn't define behavior for partial failures during pagination"
    blocked_questions:
      - "Should partial results be returned if page 3 of 5 fails?"
      - "Is retry of failed pages in scope?"
    attempted:
      - "Checked specs/api.md — no pagination error handling section"
      - "Searched codebase for existing patterns — none found"
    created: 2025-01-17T15:30:00Z

  - id: task-8
    description: "Add rate limiting to public endpoints"
    status: READY_FOR_REVIEW
    priority: 2
    assigned_to: coder-1
    worktree: .worktrees/task-8
    review_commit: c3d4e5f6
    reviewing_by: code-reviewer-1         # Code Reviewer who claimed this review
    review_lease_expires: 2025-01-17T17:05:00Z  # Code Reviewer lease (same mechanics as coder)
    spec_ref: specs/api.md#rate-limiting
    done_when: "Public endpoints return 429 with Retry-After header when limit exceeded"
    created: 2025-01-17T16:45:00Z
```

### Iteration Field Lifecycle

The `iteration` field tracks coder work cycles on a task:

| Event | `iteration` Value |
|-------|-------------------|
| Task created (DRAFT/UNCLAIMED) | Not set (null) |
| First claim (UNCLAIMED → CLAIMED) | Set to 1 |
| Work iteration complete | Unchanged (work within single claim) |
| Review rejected (REJECTED → CLAIMED, same coder) | Increment by 1 |
| Task reassigned (different coder) | Reset to 1 |
| Task reaches terminal state | Preserved (audit trail) |

**Semantics:**
- `iteration` counts **claim cycles**, not internal work loops
- A coder may make multiple commits within one iteration
- Incrementing happens when the coder re-claims after rejection
- The field supports limit enforcement (`max_iterations`) and trajectory tracking

**Relationship to `review_cycles_current`:**
- `iteration`: How many times the coder has worked on this task
- `review_cycles_current`: How many times the coder has been rejected

These can differ: a coder might submit multiple reviews in one iteration (if they split work), or iterate multiple times before requesting review.

### Review Cycles Split

Tasks track two review cycle counters:

| Field | Reset on Reassign | Purpose |
|-------|-------------------|---------|
| `review_cycles_current` | Yes (→ 0) | Limit check — new coder gets full budget |
| `review_cycles_total` | No | Audit trail — total rejections across all coders |

**Rationale:** Difficult tasks requiring coder reassignment should not penalize the replacement coder with inherited review budget. A task reassigned after 3 rejections would otherwise leave only 2 cycles before deadlock, creating a self-fulfilling prophecy that reassigned tasks require rescoping.

**Limit checks use `review_cycles_current`; retrospectives use `review_cycles_total`.**

### Task Dependencies

The `depends_on` field declares explicit dependencies between tasks:

```yaml
- id: task-auth
  status: UNCLAIMED
  depends_on: []  # No dependencies

- id: task-validation
  status: UNCLAIMED
  depends_on: [task-auth]  # Blocked until task-auth is MERGED
```

**Semantics:**
- `depends_on` is an array of task IDs that must reach MERGED status before this task can be claimed
- Empty array or missing field means no dependencies — task is immediately claimable
- Coders can only claim tasks where ALL dependencies are satisfied
- Planner sets dependencies during task creation based on logical ordering

**Claimability Rule:**
```
claimable = (status == UNCLAIMED) AND (depends_on is empty OR all depends_on tasks are MERGED)
```

**Why explicit dependencies?**
- Without explicit dependencies, Coders discover blockers at runtime → scattered BLOCKED tasks
- Planner has context to identify dependencies during decomposition
- Explicit dependencies enable parallel work on independent tasks
- Dependencies surface the critical path for human visibility

**Dependency vs BLOCKED:**
- `depends_on`: Known at planning time — task waits automatically
- `BLOCKED`: Discovered at runtime — requires Planner intervention

```yaml
agents:
  coder-1:
    role: coder
    status: WORKING
    current_task: task-2
    lease_expires: 2025-01-17T14:57:00Z
    heartbeat: 2025-01-17T14:52:00Z
    terminal: /dev/pts/2  # For human observation: which terminal window is this agent?
    iterations_total: 47  # Total iterations across all tasks this session (agent-level aggregate)
    context_percent: 34  # v1: heuristic estimate, not measured — see task-lifecycle.md#context-tracking

  code-reviewer-1:
    role: code_reviewer
    status: IDLE
    current_task: null
    lease_expires: null
    heartbeat: 2025-01-17T14:50:00Z
    terminal: /dev/pts/3

  planner-1:
    role: planner
    status: WAITING
    task: null
    lease_expires: null
    heartbeat: 2025-01-17T14:51:00Z
    terminal: /dev/pts/1

discovered:
  - id: disc-1
    by: coder-1
    during: task-2
    description: "OrderAPI.create_order() has no idempotency key support"
    severity: high
    urgency: deferred  # deferred (default), immediate — immediate wakes Planner
    recommendation: "Add idempotency key parameter before retry logic"
    created: 2025-01-17T14:46:00Z
    converted_to_task: null  # or task-id if planner created task

  - id: disc-2
    by: coder-1
    during: task-3
    description: "Auth token refresh needed before retry can succeed"
    severity: critical
    urgency: immediate  # Wakes Planner immediately — blocks current work
    recommendation: "Must add auth refresh to unblock task-3"
    created: 2025-01-17T15:30:00Z
    converted_to_task: task-3a

**Discovery Fields:**

| Field | Values | Meaning |
|-------|--------|---------|
| `severity` | `critical` | Blocks current task; must address before continuing |
| | `high` | Significant issue; should address soon |
| | `medium` | Notable finding; address when convenient |
| | `low` | Nice-to-have; log for future consideration |
| `urgency` | `immediate` | Wakes Planner now (for critical blockers) |
| | `deferred` | Planner reviews at next planning cycle (default) |

**Usage:** Coders encountering nice-to-haves during implementation log them with `severity: low, urgency: deferred` rather than blocking or scope-creeping.

handoff:
  task-5:
    agent: coder-2
    context_used: 91%  # v1: heuristic estimate
    timestamp: 2025-01-17T15:10:00Z
    # Required fields (1 phrase max each)
    summary: "Retry decorator 80% complete"
    next_action: "Parse Retry-After header from 429 responses"
    # Optional fields (include if context allows)
    approach: "tenacity library with exponential backoff"
    blockers: "Need to handle Retry-After header"
    files_modified: [src/api/client.py]
    next_steps: ["Parse Retry-After", "Add integration test"]

human_notes:
  - timestamp: 2025-01-17T15:00:00Z
    message: "Consider using existing retry util in src/utils/retry.py"
    for: task-2

spec_changes:  # Append-only log of spec modifications
  - timestamp: 2025-01-17T14:00:00Z
    spec: specs/retry-logic.md
    change: "Initial version"
    triggered_by: "goal creation"
  - timestamp: 2025-01-18T16:00:00Z
    spec: specs/retry-logic.md#auth
    change: "Added auth token refresh retry behavior"
    triggered_by: task-4a

anomalies:
  - timestamp: 2025-01-18T14:32:00Z
    task: task-3
    reporter: code-reviewer-1
    type: retry_loop
    details:
      count: 3
      error_pattern: "serialization failure on nested entity"
      root_cause_hypothesis: "data model doesn't support nesting"

  - timestamp: 2025-01-18T15:10:00Z
    task: task-3
    reporter: coder-1
    type: trade_off
    details:
      what: "flatten entity instead of fixing serializer"
      why: "unblock task within iteration limit"
      debt_created: true

  - timestamp: 2025-01-18T15:45:00Z
    task: task-5
    reporter: coder-2
    type: external_blocker
    details:
      blocker_service: "payment-gateway-api"  # Required for aggregation
      error: "Connection refused"
      impact: "Cannot test payment flow"

sprint:
  id: sprint-1
  goal_ref: goal-1
  scope:
    planned: [task-1, task-2, task-3, task-4, task-5]
    stretch: [task-6]
  timeline:
    started: 2025-01-17T09:00:00Z
    deadline: 2025-01-19T18:00:00Z
    checkpoint_at: null
    ended: null
  status: IN_PROGRESS  # Sprint status: IN_PROGRESS, CHECKPOINT, COMPLETED, ABORTED (differs from goal status)
  metrics:
    tasks_done: 2
    tasks_in_progress: 1
    tasks_blocked: 1
    iterations_total: 23  # Sprint-level sum across all agents
    review_cycles_total: 6
    # Review quality metrics (rubber-stamping detection)
    review_verdict_approvals: 4     # Count of approved events
    review_verdict_rejections: 1    # Count of rejected events
    review_verdict_count: 5         # approvals + rejections
    review_verdict_approval_rate_percent: 80  # approvals / (approvals + rejections) * 100
    task_submitted_for_review_count: 5      # Count of ready_for_review events
    task_outcome_approval_rate_percent: 80  # approvals / submitted_for_review * 100
  retrospective: null

circuit_breaker:
  last_check: 2025-01-18T17:30:00Z
  status: OK  # OK, TRIGGERED
  current_trigger: null
  history:
    - timestamp: 2025-01-17T12:00:00Z
      pattern: null
      result: OK

config:
  max_coder_iterations: 10      # Default for all tasks
  max_review_cycles: 5          # Default for all tasks
  heartbeat_interval: 60        # Seconds
  lease_duration: 300           # Seconds (5 minutes)
  coder_poll_interval: 30       # Seconds between work availability checks
  coder_max_wait: 300           # Max seconds to wait for claimable work
  integration_branch: integration
  escalation_webhook: null      # Optional: URL for external notifications
```

**Config Scope:**
- Config values are **goal-level defaults** (apply to all tasks in current goal)
- **Per-task overrides** (v1): Tasks can override `max_coder_iterations` and `max_review_cycles`:
  ```yaml
  - id: task-5
    max_iterations: 15  # Override default 10 for this complex task
  ```
- If task field is absent, config default applies
- Other config values (`heartbeat_interval`, `lease_duration`) are not per-task overridable

---

## log.yaml Schema

```yaml
# .liza/log.yaml
# Append-only activity log

- timestamp: 2025-01-17T14:00:00Z
  agent: planner-1
  action: goal_created
  detail: "Implement retry logic for all API calls with exponential backoff"

- timestamp: 2025-01-17T14:05:00Z
  agent: planner-1
  action: tasks_finalized
  detail: "5 tasks moved from DRAFT to UNCLAIMED"

- timestamp: 2025-01-17T14:06:00Z
  agent: coder-1
  action: claimed
  task: task-1
  detail: "Add retry decorator to UserAPI.get_user()"

- timestamp: 2025-01-17T14:06:05Z
  agent: coder-1
  action: claim_failed
  task: task-1
  detail: "Lost race, backing off"

- timestamp: 2025-01-17T14:25:00Z
  agent: coder-1
  action: ready_for_review
  task: task-1
  detail: "Iteration 2, commit a1b2c3d4"

- timestamp: 2025-01-17T14:28:00Z
  agent: code-reviewer-1
  action: approved
  task: task-1
  detail: "Implementation correct per spec, tests comprehensive"

- timestamp: 2025-01-17T14:29:00Z
  agent: code-reviewer-1
  action: merged
  task: task-1
  detail: "Fast-forward merge to integration"

- timestamp: 2025-01-17T14:50:00Z
  agent: planner-1
  action: rescoped
  task: task-4
  detail: "SUPERSEDED → task-4a, task-4b (wrong granularity)"
```

One-line `detail` is mandatory. Human must be able to skim.

---

## Lease Model

Agents hold **leases**, not just heartbeats. Lease = "I own this task until time X."

```yaml
agents:
  coder-1:
    role: coder
    current_task: task-3
    lease_expires: 2025-01-17T14:35:00Z
    heartbeat: 2025-01-17T14:32:00Z
    terminal: /dev/pts/2
```

**Lease rules:**
- On claim: set `lease_expires` to now + lease_duration (default: 5 minutes)
- Heartbeat extends lease by lease_duration
- Task reclaimable only after lease expires
- If original agent returns after expiry → must self-abort immediately

**Lease and Review States:**
- Coder lease (`lease_expires`) governs CLAIMED state only
- When task transitions to READY_FOR_REVIEW, the coder's lease becomes inactive
- Supervisor assigns review by setting `reviewing_by` and `review_lease_expires` before spawning Code Reviewer
- If Code Reviewer crashes, review lease expires and supervisor can assign to another Code Reviewer
- Task in APPROVED or REJECTED has no active lease requirement
- If review is REJECTED, supervisor re-claims for the original coder (acquiring a new lease) to resume work

**Code Reviewer Lease Fields (READY_FOR_REVIEW only):**

| Field | Purpose |
|-------|---------|
| `reviewing_by` | Agent ID of Code Reviewer currently examining (null if unclaimed) |
| `review_lease_expires` | Code Reviewer lease expiry timestamp (same mechanics as coder lease) |

Code Reviewer lease prevents two Code Reviewers examining same task simultaneously and enables recovery from Code Reviewer crash.

**Heartbeat interval:** 60 seconds
**Lease duration:** 300 seconds (5 minutes)
**Stale threshold:** lease_expires in the past

This resolves "slow but alive" ambiguity cleanly.

**v1 Limitation — Long Operations:**

The lease model assumes agents can interleave heartbeats with work. Some operations are atomic and cannot yield:
- Test suites running >5 minutes
- Large git operations (rebase, merge with conflicts)
- Complex refactors requiring sustained context

If an agent runs a 6-minute test suite without heartbeating, its lease expires mid-operation. Another agent may reclaim the task, creating a race.

**Mitigations for v1:**
1. **Pre-operation lease extension:** Before starting known-long operations, heartbeat immediately to maximize remaining time
2. **Task-level long_operation flag:** Mark tasks that require extended lease (human configures `lease_duration_override`)
3. **Watcher grace period:** Watcher delays reclaim alerts by 60s after lease expiry (allows in-flight operations to complete)

**v2 Solution:** Background heartbeat thread in Claude Code integration, or operation-aware lease that extends automatically during tool execution.

---

## Locking

All writes to `state.yaml` use `flock`:

```bash
flock -x .liza/state.yaml.lock -c 'operation'
```

Lock hold time must be minimal (read, modify, write, release).

Reads do not require lock (eventual consistency acceptable for reads).

---

## Operations

| Operation | Actor | Procedure |
|-----------|-------|-----------|
| Claim task | Supervisor | Two-phase: validate under lock → create worktree → re-validate and commit under lock (see tooling.md) |
| Extend lease | Any | Lock → update heartbeat + lease_expires → unlock |
| Request review | Coder | Lock → verify clean git status → write commit SHA + set READY_FOR_REVIEW atomically → unlock |
| Claim review | Supervisor | Lock → verify READY_FOR_REVIEW + no active review lease → write reviewing_by + review_lease_expires → unlock |
| Extend review lease | Code Reviewer | Lock → update review_lease_expires → unlock |
| Submit verdict | Code Reviewer | Lock → verify commit SHA matches + reviewing_by matches self → set APPROVED/REJECTED + reason + clear review lease → unlock |
| Execute merge | Code Reviewer | Only after APPROVED → run wt-merge.sh → update state to MERGED |
| Mark blocked | Any | Lock → set state BLOCKED + diagnosis → unlock |
| Rescope task | Planner | Lock → set original SUPERSEDED → create new task(s) with reference → unlock |
| Finalize draft | Planner | Lock → change DRAFT to UNCLAIMED → unlock |
| Log activity | Any | Append to log.yaml (no lock needed, append-only) |

---

## Clean Sync Invariant

Before setting READY_FOR_REVIEW, coder must ensure working tree is clean:

```bash
[ -z "$(git -C $WORKTREE status --porcelain)" ] || abort "Uncommitted changes"
COMMIT_SHA=$(git -C $WORKTREE rev-parse HEAD)
```

Blackboard records `review_commit: $COMMIT_SHA`. Code Reviewer verifies this SHA before reviewing.

For detailed definition including edge cases (submodules, untracked files), see [Worktree Management — Clean Sync Invariant](../protocols/worktree-management.md#clean-sync-invariant).

---

## Validation Rules

### Anomaly Types

| Type | Logged By | When to Log |
|------|-----------|-------------|
| `retry_loop` | Coder, Code Reviewer | Same error pattern across >2 iterations |
| `trade_off` | Coder | Accepted suboptimal solution to unblock progress |
| `spec_ambiguity` | Coder | Spec doesn't cover encountered case, judgment call made |
| `external_blocker` | Coder | External service/API blocking progress |
| `assumption_violated` | Coder, Code Reviewer | Spec assumption proven false by implementation |
| `scope_deviation` | Code Reviewer | Implementation differs from task spec |
| `workaround` | Code Reviewer | Shortcut taken instead of proper fix |
| `debt_created` | Code Reviewer | Technical debt introduced |
| `spec_changed` | Code Reviewer | Spec changed since task creation |
| `hypothesis_exhaustion` | Planner | Two coders failed same task, rescope required |
| `spec_gap` | Planner | Missing spec discovered during planning/rescope |
| `review_deadlock` | Planner | Coder-Code Reviewer reached max cycles without approval |
| `system_ambiguity` | Any role | Liza protocol or role definition unclear, escalated to Planner |

**Required Details Fields (validated by liza-validate.sh):**

| Type | Required Fields | Purpose |
|------|-----------------|---------|
| `retry_loop` | `count`, `error_pattern` | Pattern detection via `similar(error_pattern)` |
| `trade_off` | `what`, `why`, `debt_created` | Debt accumulation counting |
| `external_blocker` | `blocker_service` | Aggregation by service for circuit breaker |
| `assumption_violated` | `assumption`, `reality` | Assumption cascade detection |
| `system_ambiguity` | `protocol_section`, `question` | Track Liza system gaps for human clarification |

Anomalies with malformed details will fail validation. This ensures circuit breaker pattern detection has reliable data.

```yaml
required_fields:
  state:
    - version
    - goal
    - tasks
    - agents
    - config

invariants:
  - "DRAFT task cannot have assigned_to"
  - "Non-DRAFT task (except SUPERSEDED, ABANDONED) must have done_when"
  - "Non-DRAFT task (except SUPERSEDED, ABANDONED) must have spec_ref"
  - "CLAIMED task must have assigned_to"
  - "CLAIMED task must have worktree"
  - "CLAIMED task worktree path must exist (catches partial claim failures)"
  - "CLAIMED task must have valid lease_expires"
  - "CLAIMED task must have base_commit (except integration_fix tasks which reuse existing worktree)"
  - "READY_FOR_REVIEW task must have review_commit"
  - "REJECTED task must have rejection_reason"
  - "BLOCKED task must have blocked_reason and blocked_questions"
  - "SUPERSEDED task must have superseded_by and rescope_reason"
  - "MERGED task must not have worktree"
  - "depends_on must reference existing task IDs"
  - "depends_on must not create circular dependencies"
  - "CLAIMED task must have all depends_on tasks in MERGED status"
  - "Agent WORKING must have task"
  - "Agent WORKING must have lease_expires in future (or within grace period of 60s)"
  - "No two agents assigned to same task"
  - "Task with integration_fix:true must have prior INTEGRATION_FAILED in history"
  - "Task failed_by list must contain unique agent IDs"
  - "Anomaly type must be one of: retry_loop, trade_off, spec_ambiguity, external_blocker, assumption_violated, scope_deviation, workaround, debt_created, spec_changed, hypothesis_exhaustion, spec_gap, review_deadlock, system_ambiguity"
  # Transition invariants (runtime-enforced, not statically validated)
  # These are enforced by agent behavior and atomic operations during state transitions.
  # liza-validate.sh validates static state invariants; these require history analysis.
  - "CLAIMED task from REJECTED must have new lease_expires (not stale from prior claim)"
  - "UNCLAIMED task must preserve failed_by if previously BLOCKED"
  - "CLAIMED task with integration_fix:true must have lease_expires set"
```

**Enforcement Note:** Static invariants (above the "Transition invariants" comment) are validated by `liza-validate.sh`. Transition invariants are runtime constraints enforced by agents performing atomic operations during state transitions — they cannot be verified post-hoc without history event analysis.

## Related Documents

- [State Machines](state-machines.md) — state transitions
- [Task Lifecycle](../protocols/task-lifecycle.md) — operational flow
- [Tooling](../implementation/tooling.md) — scripts for blackboard operations
