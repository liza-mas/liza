# Role Definitions

## Terminology

| Canonical Name | YAML Identifier | Agent ID Prefix | Agent Name Pattern |
|----------------|-----------------|-----------------|-------------------|
| Planner | `planner` | `planner-` | `planner-1`, `planner-2` |
| Coder | `coder` | `coder-` | `coder-1`, `coder-2` |
| Code Reviewer | `code_reviewer` | `code-reviewer-` | `code-reviewer-1`, `code-reviewer-2` |

**Usage Rules:**
- **Prose/documentation:** Use canonical name ("Code Reviewer validates...")
- **YAML role field:** Use identifier (`role: code_reviewer`)
- **Agent IDs:** Use prefix form (`code-reviewer-1`, `coder-2`)
- **Role → ID mapping:** `code_reviewer` maps to `code-reviewer-` prefix

**ID Validation Regex:** `^(coder|code-reviewer|planner)-[0-9]+$`

---

## Shared Capabilities

All roles have:
- Read blackboard state (`state.yaml`: tasks, agents, discoveries, config)
- Write to activity log (`log.yaml`)
- Write to anomalies section (see role-specific Logging Duties)

## Shared Constraints

All roles must:
- Raise ambiguity in Liza protocols or role definitions — log as `system_ambiguity` anomaly, escalate to Planner
- Never silently interpret unclear system instructions — ask before proceeding
- Treat protocol gaps the same as spec gaps: explicit escalation, not creative interpretation

**Rationale:** Specs are complex and imperfect. Silent workarounds compound into systemic drift. Planner escalates to human via CHECKPOINT if system-level clarification needed.

---

## Planner

**Purpose:** Decompose goal into tasks. Monitor for blocked states. Rescope when needed.

**Capabilities:**
- Read specs and docs to understand goal context
- Write goal and tasks to blackboard (two-phase: DRAFT → UNCLAIMED)
- Rescope tasks (split, redefine, kill) with audit trail
- Reassign tasks after hypothesis exhaustion
- Resolve blocked reviews
- Mark tasks SUPERSEDED when rescoping

**Constraints:**
- Cannot claim Coder or Code Reviewer tasks
- Plan review by human before execution (dedicated Plan Reviewer role planned for v2)
- Must append to `goal.alignment_history` after each rescope (preserves drift trajectory)
- Rescoping must reference original task and state reason
- Must ensure specs exist before creating tasks

**Self-Validation Gates (required for each task):**

| Gate | Requirement |
|------|-------------|
| Spec reference | Each task must cite `spec_ref` pointing to relevant spec section |
| Success criteria | Each task must have falsifiable `done_when` statement |
| Scope boundary | Each task must state what is explicitly IN scope |
| Dependency check | If task depends on another, state the dependency |
| TDD inclusion | Code tasks include tests — do NOT create separate "add tests" tasks (exempt: doc/config/spec-only) |

Tasks missing any gate remain DRAFT until completed. This enables:
- Code Reviewer validation against spec
- Auditable task definitions
- Earlier detection of framing errors (before coder burns cycles)

**Field Formats:**

| Field | Format | Example |
|-------|--------|---------|
| `spec_ref` | Path to spec file relative to project root, optionally with `#anchor` | `specs/api.md#pagination` |
| `done_when` | Falsifiable statement describing observable outcome. Must be something that could be proven wrong. | `"GET /users returns 200 with JSON array containing user objects"` |

**`spec_ref` Validation:**
- Required: Field must be present (enforced by `liza-validate.sh`)
- Required: File must exist (enforced by default; skip with `SKIP_SPEC_FILE_CHECK=true`)
- Anchors (`#section`) are not validated (human responsibility to maintain)
- Missing file is an ERROR, not warning — fail fast prevents cascade of blocked tasks at runtime

**`done_when` Guidelines:**
- State the observable behavior, not the implementation approach
- Include specific endpoints, status codes, or data formats where applicable
- Avoid vague terms like "works correctly" or "handles errors properly"
- Good: `"429 responses trigger exponential backoff starting at 1s"`
- Bad: `"Rate limiting is handled appropriately"`

**TDD Enforcement (code tasks only):**
- Each code task MUST include tests — Planner does NOT create separate "add tests" tasks
- Coder writes tests FIRST that verify `done_when` criteria, then implements until tests pass
- Code Reviewer REJECTS code submissions without tests covering `done_when`
- Exempt: documentation-only, config-only, or spec-only tasks (no code = no tests required)
- Rationale: Coder can't validate their work without tests; separate test tasks break TDD flow

**`done_when` vs Tests:**
- `done_when` is the **acceptance criterion** — what the Code Reviewer validates
- Tests should **exercise the `done_when` scenarios** — but tests passing doesn't automatically satisfy `done_when`
- Code Reviewer validates: (1) tests pass, AND (2) tests actually cover `done_when` behavior
- A task can have passing tests but fail review if tests don't demonstrate the `done_when` outcome

**Iteration Model:**
- Runs until all tasks DONE or ABANDONED
- Wakes on: blocked task, hypothesis exhaustion trigger, integration failure, immediate discovery
- Sleeps otherwise (event-driven, not polling)

**Wake Triggers:**

| Trigger | Condition | Action |
|---------|-----------|--------|
| Blocked task | Task status = BLOCKED | Evaluate rescope options |
| Hypothesis exhaustion | Task has `failed_by` with ≥2 coders | Reassign or rescope |
| Integration failure | Task status = INTEGRATION_FAILED | Create fix task |
| Immediate discovery | Discovery with `urgency: immediate` not yet converted | Evaluate conversion to task |

**Multiple Blocked Tasks:**
When multiple tasks are BLOCKED simultaneously:
1. Process sequentially by priority (lowest number first)
2. For same priority: process by created timestamp (oldest first)
3. May rescope multiple tasks in single session if related
4. Each rescope is a separate logged action (no batch rescoping without audit trail)

### Planner Logging Duties

Planner MUST log to anomalies section:

| Event | Log As |
|-------|--------|
| Two coders failed same task | `hypothesis_exhaustion` |
| Spec gap discovered during planning | `spec_gap` |

**Logging happens at time of occurrence, before rescope action.**

---

## Coder

**Purpose:** Implement tasks. Iterate until Code Reviewer approves.

**Capabilities:**
- Read specs to understand task requirements without asking
- Work on claimable tasks: UNCLAIMED, REJECTED, or INTEGRATION_FAILED (supervisor claims on coder's behalf)
- Create/modify code in task worktree
- Commit to task worktree (not integration branch)
- Request review
- Address rejection feedback
- Mark self BLOCKED with diagnosis and clarifying questions

**Constraints:**
- Work only in assigned worktree
- No modifications outside task scope
- Cannot self-approve
- Cannot merge to integration branch (Code Reviewer-only)
- Cannot commit to integration branch
- Cannot claim under-specified work (triggers BLOCKED, not guessing)

**Task Assignment:**
The supervisor (`liza-agent.sh`) claims tasks on behalf of coders before spawning the agent. This avoids permission prompts in non-interactive mode. The coder receives its assigned task in the bootstrap prompt and should NOT attempt to claim tasks directly.

If multiple coders contend for tasks, the supervisor handles backoff:
1. Log `claim_failed` to activity log
2. Sleep randomized backoff (1-5 seconds)
3. Retry at most 3 times
4. If still failing, exit and let human investigate

**Iteration Model:**
- Ralph-style loop until APPROVED or BLOCKED
- Max iterations configurable per task (default: 10)
- On max iterations without approval → BLOCKED

### Blocking Protocol

When marking a task BLOCKED, coder MUST provide:

| Field | Required | Purpose |
|-------|----------|---------|
| `blocked_reason` | Yes | What is blocking progress (specific, not vague) |
| `blocked_questions` | Yes | 1-3 specific questions that would unblock if answered |
| `attempted` | Recommended | What approaches were tried before blocking |

Example:
```yaml
blocked_reason: "Task requires API pagination but spec doesn't define behavior for partial failures"
blocked_questions:
  - "Should partial results be returned if page 3 of 5 fails?"
  - "Is retry of failed pages in scope, or should we fail the whole request?"
  - "What's the timeout budget for pagination across all pages?"
attempted:
  - "Checked specs/api.md — no pagination section"
  - "Searched codebase for existing pagination patterns — none found"
```

**Do not block for:**
- Questions answerable by reading existing specs (read first)
- Style/approach preferences (make a reasonable choice)
- Missing nice-to-haves (implement core, log to `discovered` section with `severity: low`)

### Coder Logging Duties

Coder MUST log to anomalies section:

| Event | Log As |
|-------|--------|
| > 2 iterations on same error | `retry_loop` |
| Accepting suboptimal solution | `trade_off` |
| Spec doesn't cover encountered case | `spec_ambiguity` |
| External service/API blocking | `external_blocker` |
| Assumption in spec proven false | `assumption_violated` |

**Logging happens at time of occurrence, not end of task.**

---

## Code Reviewer

**Purpose:** Verify coder output. Approve or reject with binding verdict. Merge approved work.

**Capabilities:**
- Read specs to validate implementation against requirements
- Claim tasks in READY_FOR_REVIEW state (write `reviewing_by`, `review_lease_expires`)
- Read task worktree and verify commit SHA
- Run validation commands
- Approve (triggers merge eligibility)
- Reject with specific, actionable reason
- Execute merge to integration branch (Code Reviewer-only privilege)

**Constraints:**
- Cannot modify code in worktree (read-only except for merge)
- Must cite specific criteria or invariant for rejection
- Cannot reject on "vibes" or style preference
- Verdict is final for that review cycle
- Must verify commit SHA matches READY_FOR_REVIEW state
- Rejects work that doesn't match spec (not just work that doesn't pass tests)

**Iteration Model:**
- Invoked on-demand when tasks enter READY_FOR_REVIEW
- Single pass per review (not looping)
- Max review cycles per task before escalation (default: 5)

### Review Assignment

The supervisor (`liza-agent.sh`) assigns review tasks to Code Reviewers before spawning the agent, similar to Coder task assignment. This avoids permission prompts in non-interactive mode.

The supervisor sets:
- `reviewing_by` field on the task
- `review_lease_expires` timestamp on the task
- Agent status to `REVIEWING`
- Agent `current_task` and lease

| Condition | Review Claimable? |
|-----------|-------------------|
| `reviewing_by` is null | Yes |
| `reviewing_by` set but `review_lease_expires` in past | Yes (stale claim) |
| `reviewing_by` set and `review_lease_expires` in future | No (active claim) |

**Code Reviewer must extend lease with heartbeats** during long reviews (same 60s interval as coders).

**On verdict submission:** Update task status to APPROVED/REJECTED and clear agent's `current_task`.

### Code Reviewer Logging Duties

Code Reviewer MUST log to anomalies section:

| Observation | Log As |
|-------------|--------|
| Coder retry loop visible in history | `retry_loop` (check `anomalies` section for task-id first) |
| Implementation differs from task spec | `scope_deviation` |
| Workaround or shortcut taken | `workaround` |
| Technical debt introduced | `debt_created` |
| Spec assumption contradicted by code | `assumption_violated` |
| Spec changed since task creation | `spec_changed` |

Code Reviewer MUST include in rejection:
- Spec reference (if applicable)
- Whether anomaly logged

### Review Scope

Code Reviewer evaluates:
- Does implementation match task definition?
- Does implementation match spec?
- Do tests validate the specified behavior?
- Are there obvious defects?
- Does commit SHA match READY_FOR_REVIEW state?

**Spec Currency:** Code Reviewer always validates against the **current** spec version (at review time), not the version when task was created. If spec changed materially since task creation:
- Reject if implementation no longer matches current spec
- Log `spec_changed` anomaly with details
- Planner may need to rescope task based on spec delta

**v1 Limitation:** No automated spec_hash tracking. Code Reviewer must manually verify spec currency by checking `spec_changes` section in blackboard.

Code Reviewer does NOT evaluate:
- Style preferences
- Alternative approaches (unless current is defective)
- Scope expansion opportunities

### Binding Verdict Rules

**Rejection must include:**
- Specific file and location
- Specific defect or missing requirement (reference spec if applicable)
- Actionable fix description (what to change, not just "this is wrong")

**Coder must address the specific feedback:**
- Cannot reinterpret rejection
- Cannot work around rejection
- Cannot negotiate via code comments

**Approval means:**
- Implementation matches task requirements
- Implementation matches spec
- Tests validate behavior
- No obvious defects found
- Clear to merge

**Approval does NOT mean:**
- Code is perfect
- No improvements possible
- Ready for production (that's integration + human review of main)

---

## Role Interaction Summary

| Actor | Can Commit To |
|-------|---------------|
| Coder | Task worktree branch only |
| Code Reviewer | Integration branch (via merge only) |
| Planner | Neither (no code changes) |

## Agent Identity Protocol

Agents do not self-identify. Identity is **assigned by the supervisor** and passed as an environment variable.

### Identity Assignment

```bash
# Supervisor spawns agent with explicit identity
LIZA_AGENT_ID=coder-1 liza-agent.sh coder
```

| Env Variable | Required | Format | Example |
|--------------|----------|--------|---------|
| `LIZA_AGENT_ID` | Yes | `{role}-{number}` | `coder-1`, `code-reviewer-2`, `planner-1` |

**Rationale:** Prevents identity collision when multiple agents spawn simultaneously. Agent cannot choose its own name — supervisor controls the namespace.

### Registration Protocol

When an agent starts, it must register in the blackboard before doing any work:

```yaml
# Agent registration attempt
agents:
  coder-1:
    role: coder
    status: STARTING
    lease_expires: 2025-01-17T14:05:00Z  # Short lease during startup
    heartbeat: 2025-01-17T14:00:00Z
    terminal: /dev/pts/2
```

**Registration succeeds if:**
- Agent ID does not exist in `agents` section, OR
- Agent ID exists but `lease_expires` is in the past (stale agent)

**Registration fails if:**
- Agent ID exists AND `lease_expires` is in the future (active agent)

### Collision Prevention

```bash
register_agent() {
    local agent_id="$LIZA_AGENT_ID"
    local now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    local lease=$(date -u -d "+5 minutes" +%Y-%m-%dT%H:%M:%SZ)

    # Check for active agent with same ID
    local existing_lease=$(yq -r ".agents.\"$agent_id\".lease_expires // \"\"" "$STATE")
    if [ -n "$existing_lease" ] && [ "$existing_lease" != "null" ]; then
        if [ "$(date -d "$existing_lease" +%s)" -gt "$(date +%s)" ]; then
            echo "ERROR: Agent $agent_id already registered with active lease until $existing_lease"
            exit 1
        fi
    fi

    # Register (within lock)
    yq -i ".agents.\"$agent_id\" = {
        \"role\": \"$LIZA_ROLE\",
        \"status\": \"STARTING\",
        \"lease_expires\": \"$lease\",
        \"heartbeat\": \"$now\",
        \"terminal\": \"$(tty)\"
    }" "$STATE"
}
```

### Identity Validation

Agent MUST validate identity on startup:

| Check | Failure Action |
|-------|----------------|
| `LIZA_AGENT_ID` unset | Exit with error |
| `LIZA_AGENT_ID` format invalid | Exit with error |
| Role in ID doesn't match `$1` | Exit with error |
| Registration collision | Exit with error |

**Invalid format examples:**
- `coder` (missing number)
- `coder1` (missing hyphen)
- `my-coder-1` (invalid role prefix)

### Supervisor Responsibilities

The supervisor (human or orchestration script) MUST:
1. Assign unique agent IDs before spawning
2. Track which IDs are in use
3. Reclaim IDs after agent termination (lease expires or explicit cleanup)

**v1 Implementation:** Human manually assigns IDs. Future versions may automate ID allocation.

---

## Related Documents

- [Agent Initialization](../protocols/agent-initialization.md) — startup sequence from spawn to first action
- [Task Lifecycle](../protocols/task-lifecycle.md) — claim, iterate, review, merge
- [State Machines](state-machines.md) — task and agent state transitions
- [Sprint Governance](../protocols/sprint-governance.md) — checkpoints, retrospectives
