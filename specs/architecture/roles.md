# Role Definitions

## Terminology

**Implementation:** Roles are defined declaratively in the pipeline YAML under the `roles` section
(see [Declarative Role Definitions](../build/3%20-%20Declarative%20Role%20Definitions.md)).
Each role entry specifies its `type` (doer, reviewer, orchestrator), display name, timeouts,
context sections, allowed operations, and skills. Role classification and mappings are derived
from the YAML at load time — no hardcoded role constants.

| Canonical Name | YAML Key | Agent ID Prefix | Agent Name Pattern |
|----------------|----------|-----------------|-------------------|
| Orchestrator | `orchestrator` | `orchestrator-` | `orchestrator-1` |
| Epic Planner | `epic-planner` | `epic-planner-` | `epic-planner-1`, `epic-planner-2` |
| Epic Plan Reviewer | `epic-plan-reviewer` | `epic-plan-reviewer-` | `epic-plan-reviewer-1`, `epic-plan-reviewer-2` |
| US Writer | `us-writer` | `us-writer-` | `us-writer-1`, `us-writer-2` |
| US Reviewer | `us-reviewer` | `us-reviewer-` | `us-reviewer-1`, `us-reviewer-2` |
| Architect | `architect` | `architect-` | `architect-1`, `architect-2` |
| Architecture Reviewer | `architecture-reviewer` | `architecture-reviewer-` | `architecture-reviewer-1`, `architecture-reviewer-2` |
| Code Planner | `code-planner` | `code-planner-` | `code-planner-1`, `code-planner-2` |
| Code Plan Reviewer | `code-plan-reviewer` | `code-plan-reviewer-` | `code-plan-reviewer-1`, `code-plan-reviewer-2` |
| Coder | `coder` | `coder-` | `coder-1`, `coder-2` |
| Code Reviewer | `code-reviewer` | `code-reviewer-` | `code-reviewer-1`, `code-reviewer-2` |
| Integration Analyst | `integration-analyst` | `integration-analyst-` | `integration-analyst-1` |
| Integration Reviewer | `integration-reviewer` | `integration-reviewer-` | `integration-reviewer-1` |

**Usage Rules:**
- **Prose/documentation:** Use canonical name ("Code Reviewer validates...")
- **YAML role key:** Use the hyphenated key from the `roles` section (`role: code-reviewer`)
- **Agent IDs:** Use prefix form (`code-reviewer-1`, `coder-2`)
- **Single name form:** The YAML key is the canonical identifier — used in pipeline YAML, task model, agent IDs, and CLI. There is no separate "workflow" form.

**ID Validation Regex:** `^(orchestrator|epic-planner|epic-plan-reviewer|us-writer|us-reviewer|architect|architecture-reviewer|code-planner|code-plan-reviewer|coder|code-reviewer|integration-analyst|integration-reviewer)-[0-9]+$`

## Multiple Agents Per Role

Running multiple agents of the same role is fully supported:

| Role | Multiple Agents Supported | Notes |
|------|--------------------------|-------|
| Epic Planner | Yes | Claims independent epic-planning or epic-planning-main tasks |
| Epic Plan Reviewer | Yes | Claims independent epic plan reviews, including second reviews for master quorum |
| US Writer | Yes | Claims independent user-story tasks |
| US Reviewer | Yes | Claims independent user-story reviews |
| Architect | Yes | Claims independent architecture or architecture-main tasks |
| Architecture Reviewer | Yes | Claims independent architecture reviews, including second reviews for master quorum |
| Code Planner | Yes | Claims independent code-planning or code-planning-main tasks |
| Code Plan Reviewer | Yes | Claims independent code-plan reviews, including second reviews for master quorum |
| Coder | Yes | Each coder claims independent tasks; no coordination needed |
| Code Reviewer | Yes | Reviewers claim independent review tasks; merge safety via working-tree-less `liza wt-merge` |
| Orchestrator | No (`max-instances: 1`) | Singular orchestrator enforced at registration (see [Declarative Role Definitions](../build/3%20-%20Declarative%20Role%20Definitions.md#constraints)) |

**Concurrency Safety:**
- Task claiming: File locking on `state.yaml` ensures atomic claim operations
- Review claiming: Lease-based exclusive access prevents duplicate reviews
- Merging: Working-tree-less merge (`git merge-tree` + `commit-tree` + `update-ref`) enables concurrent merges without corruption

**Example Deployment:**
```bash
# Terminal 1: Coder 1
liza agent coder --agent-id coder-1

# Terminal 2: Coder 2 (concurrent)
liza agent coder --agent-id coder-2

# Terminal 3: Code Reviewer 1
liza agent code-reviewer --agent-id code-reviewer-1

# Terminal 4: Code Reviewer 2 (concurrent)
liza agent code-reviewer --agent-id code-reviewer-2
```

---

## Shared Capabilities

All roles have:
- Read blackboard state (`state.yaml`: tasks, agents, discoveries, config)
- Write to activity log (`log.yaml`)
- Write to anomalies section (see role-specific Logging Duties)

## Shared Constraints

All roles must:
- Raise ambiguity in Liza protocols or role definitions — log as `system_ambiguity` anomaly, escalate to Orchestrator
- Never silently interpret unclear system instructions — ask before proceeding
- Treat protocol gaps the same as spec gaps: explicit escalation, not creative interpretation

**Rationale:** Specs are complex and imperfect. Silent workarounds compound into systemic drift. Orchestrator escalates to human via CHECKPOINT if system-level clarification needed.

## Master Planning Responsibilities

Master planning tasks are role-pair specializations, not new roles. The same doer/reviewer roles are reused by both the master and specialized pairs:

| Master role-pair | Specialized role-pair | Doer | Reviewer |
|------------------|-----------------------|------|----------|
| `epic-planning-main-pair` | `epic-planning-pair` | `epic-planner` | `epic-plan-reviewer` |
| `architecture-main-pair` | `architecture-pair` | `architect` | `architecture-reviewer` |
| `code-planning-main-pair` | `code-planning-pair` | `code-planner` | `code-plan-reviewer` |

When a task's role-pair has `decomposition-root: true`, the doer must define the general approach before specialized work starts: boundaries, interface contracts, shared-file ownership, dependency ordering, required `plan_ref` or `arch_ref`, and one typed `output[]` entry per specialized scope. The master doer must prove coverage of the goal without writing the detailed specialized plan.

The master reviewer validates decomposition coherence rather than implementation detail. Approval requires the Master Output Contract: non-overlapping scopes, exactly-one interface ownership, explicit shared-file ownership, real scheduling dependencies, inherited artifact refs, and complete coverage. `decomposition.read_only_depends_on` and `decomposition.read_only_task_depends_on` are descriptive metadata; the scheduler dependency must also be mirrored in `depends_on` or `task_depends_on`.

Master role-pairs use quorum 2. A first approval moves the task to the configured `partially-approved` state; a second reviewer claims from that state, transitions through `reviewing-2`, and the second approval reaches the configured `approved` state.

### Loop Detection Self-Abort

If you observe yourself running:
- The same command more than 3 times, OR
- Close variations of the same command (same base, different flags/pipes) more than 5 times total

WITHOUT meaningful progress toward your goal, STOP IMMEDIATELY:

1. Log anomaly to blackboard:
   - Coder/Orchestrator: `retry_loop` with command pattern
   - Code Reviewer: `reviewer_loop` with command pattern
2. Take role-appropriate action:
   - Coder: Mark task BLOCKED with diagnosis of what's not working
   - Code Reviewer: Issue REJECTED verdict with `"insufficient information to complete review"` and specific blocker
   - Orchestrator: Log `spec_gap` and pause for human input
3. Exit with code 42

**"Meaningful progress" means:** New information that changes your next action.
Piping the same output through different tools hoping for different results is NOT progress.

**Examples of loops to abort:**
- Running `unittest discover` when tests use pytest (wrong framework)
- Repeating `grep` with different flags on the same empty result
- Re-reading the same file expecting different content

---

## Orchestrator

**Purpose:** Decompose goal into tasks. Monitor for blocked states. Rescope when needed.

**Capabilities:**
- Read specs and docs to understand goal context
- Write goal and tasks to blackboard (two-phase: DRAFT → READY)
- Rescope tasks (split, redefine, kill) with audit trail
- Reassign tasks after hypothesis exhaustion
- Resolve blocked reviews
- Mark tasks SUPERSEDED when rescoping
- Write deferred systemic findings to `specs/architecture/architectural-issues.md` (see below)

**Constraints:**
- Cannot claim Coder or Code Reviewer tasks
- Plan review by human before execution (dedicated Plan Reviewer role planned for v2)
- Must append to `goal.alignment_history` after each rescope (preserves drift trajectory)
- Rescoping must reference original task and state reason
- For hypothesis exhaustion, rescoping must include root cause (what failed and why) in `rescope_reason` and the log entry
- Must ensure specs exist before creating tasks

**Self-Validation Gates (required for each task):**

| Gate | Requirement |
|------|-------------|
| Spec reference | Each task must cite `spec_ref` pointing to relevant spec section |
| Success criteria | Each task must have falsifiable `done_when` statement |
| Scope boundary | Each task must state what is explicitly IN scope (functional area, not file names — file structure is the coder's decision) |
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
- Required: Field must be present (enforced by `liza validate`)
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
- Each code task MUST include tests — Orchestrator does NOT create separate "add tests" tasks
- Coder writes tests FIRST that verify `done_when` criteria, then implements until tests pass
- Code Reviewer REJECTS code submissions without tests covering `done_when`
- Exempt: documentation-only, config-only, or spec-only tasks (no code = no tests required)
- Waiver: code tasks with no behavior change (cosmetic fixes, comment edits) can declare `tdd_not_required` with justification in the checkpoint; Code Reviewer verifies
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
| Immediate discovery | Discovery with `urgency: immediate` not yet converted | Evaluate conversion to task |
| Systemic finding | Discovery with `source: systemic-thinking` not yet converted | Evaluate: create task, defer to ISSUES_FILE, or dismiss |

**Systemic Finding Disposition:**
When processing discoveries with `source: systemic-thinking`:
- **Actionable now:** Create task with `spec_ref` and `done_when`. Set `converted_to_task` on discovery.
- **Deferred:** Write to `specs/architecture/architectural-issues.md` using the persistence format from the systemic-thinking skill. Set `converted_to_task: deferred` on discovery.
- **Dismissed:** Set `converted_to_task: dismissed` on discovery. No further action.

**Multiple Blocked Tasks:**
When multiple tasks are BLOCKED simultaneously:
1. Process sequentially by priority (lowest number first)
2. For same priority: process by created timestamp (oldest first)
3. May rescope multiple tasks in single session if related
4. Each rescope is a separate logged action (no batch rescoping without audit trail)

### Orchestrator Logging Duties

Orchestrator MUST log to anomalies section:

| Event | Log As |
|-------|--------|
| Two coders failed same task | `hypothesis_exhaustion` |
| Spec gap discovered during planning | `spec_gap` |

**Logging happens at time of occurrence, before rescope action.**
Include a one-sentence root cause in the `hypothesis_exhaustion` log entry.

---

## Coder

**Purpose:** Implement tasks. Iterate until Code Reviewer approves.

**Capabilities:**
- Read specs to understand task requirements without asking
- Work on claimable tasks: READY, REJECTED, or INTEGRATION_FAILED (supervisor claims on coder's behalf)
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
The supervisor (`liza agent`) claims tasks on behalf of coders before spawning the agent. This avoids permission prompts in non-interactive mode. The coder receives its assigned task in the bootstrap prompt and should NOT attempt to claim tasks directly.

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
| `blocked_reason` | Yes | What is blocking progress (specific, not vague). Include what approaches were tried before blocking. |
| `blocked_questions` | Yes | 1-3 specific questions that would unblock if answered |

Example:
```yaml
blocked_reason: "Task requires API pagination but spec doesn't define behavior for partial failures"
blocked_questions:
  - "Should partial results be returned if page 3 of 5 fails?"
  - "Is retry of failed pages in scope, or should we fail the whole request?"
  - "What's the timeout budget for pagination across all pages?"
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

### Doer Assignment

The supervisor (`liza agent`) assigns executing tasks to doers before spawning
the child agent process.

The supervisor sets:
- `assigned_to` field on the task
- `lease_expires` timestamp on the task
- Agent status to `WORKING`
- Agent `current_task` and lease

For tasks in a pipeline executing state, doer ownership is a bidirectional
invariant: `assigned_to` must point to an agent whose role is the exact doer
role resolved from the task's `role_pair`, and the owner row must have
registered provider, PID, and lease metadata. A `WORKING` doer agent with
`current_task` must point back to an executing task assigned to that same agent.
During explicit context handoff, `handoff_pending: true` with a `HANDOFF` agent
and matching `current_task` is also valid active ownership.

One recovery state is valid: an executing task assigned to the correct doer
agent may be resumed when that agent row is registered, has live-process
metadata, is `IDLE`, and has empty `current_task` or `current_task` already set
to the same task. This covers supervisor restart and child CLI exit windows
without losing owned work.

`WAITING` with `current_task` is passive wait metadata, not active ownership.
It is valid for a doer only while the assigned task is submitted, reviewing, or
partially approved and the doer is awaiting a verdict. Other doer `WAITING`
task references are stale.

Repair is more conservative for doers than for reviewers. Reviewer repair is a
state-only correction, but doer worktrees may contain unsubmitted work; therefore
`liza validate --repair` refuses to clear invalid doer ownership while the
assigned PID is live and points the operator at explicit recovery tooling.

---

## Code Reviewer

**Purpose:** Verify coder output. Approve or reject with binding verdict.

**Capabilities:**
- Read specs to validate implementation against requirements
- Claim tasks in READY_FOR_REVIEW state (write `reviewing_by`, `review_lease_expires`)
- Read task worktree and verify commit SHA
- Run validation commands
- Approve (triggers merge eligibility — supervisor executes merge)
- Reject with specific, actionable reason
- Write to `discovered` section (systemic findings from review — see systemic-thinking skill)

**Constraints:**
- Cannot modify code in worktree (read-only)
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

The supervisor (`liza agent`) assigns review tasks to Code Reviewers before spawning the agent, similar to Coder task assignment. This avoids permission prompts in non-interactive mode.

The supervisor sets:
- `reviewing_by` field on the task
- `review_lease_expires` timestamp on the task
- Agent status to `REVIEWING`
- Agent `current_task` and lease

For tasks in a pipeline reviewing state, including second-review states, review
ownership is a bidirectional invariant: `reviewing_by` must point to an agent
whose role is the exact reviewer role resolved from the task's `role_pair`, and
that agent must be `REVIEWING` with `current_task` set to the same task.
`reviewing_by` on a non-reviewing task is not active review ownership. It is
valid only as passive wait metadata when a `WAITING` reviewer is awaiting doer
resubmission for a rejected/executing task and `review_lease_expires` is still
in the future; otherwise it is stale or orphaned state to clear through
recovery.

| Condition | Review Claimable? |
|-----------|-------------------|
| `reviewing_by` is null | Yes |
| `reviewing_by` set but `review_lease_expires` in past | Yes (stale claim) |
| `reviewing_by` set and `review_lease_expires` in future | No (active claim) |

**Code Reviewer must extend lease with heartbeats** during long reviews (same 60s interval as coders).

**On verdict submission:** Update task status to APPROVED/REJECTED and clear agent's `current_task`.

### Review Exhaustion

If 2 different Code Reviewers fail to issue a verdict on the same task (exit without APPROVED/REJECTED):

1. Log `review_exhaustion` anomaly with details of what blocked both reviewers
2. Set task BLOCKED with `blocked_reason: "review_exhaustion: 2 reviewers unable to complete review"`
3. Orchestrator evaluates: task spec unclear? done_when untestable? missing context?

**"Failed to issue verdict" means:** Agent exited (crash, timeout, loop detection) without calling `liza submit-verdict`.

**Tracking:** Supervisor tracks review attempts via task history events. Two `review_started` events without corresponding `approved` or `rejected` event triggers this protocol.

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
| Own review stuck in command loop | `reviewer_loop` |
| Verdict attempt races with task leaving review | CLI records `stale_verdict` |

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

**Review Scope on Iteration Cycles:**
Review covers **all changes in the scope/workmanship diff** (`base_commit` → `review_commit`), not just changes since last rejection. Each review is a fresh evaluation of whether the full implementation meets the spec. This catches regressions introduced by fixes and keeps the mental model simple: "does this worktree satisfy the task?"

For code reviews, current integration drift is checked separately immediately before verdict with `integration_branch` → `review_commit`. Scope findings come exclusively from the scope/workmanship diff. If current-integration drift makes approval unsafe or ambiguous while the scope diff is clean, reject with verdict text saying review-range drift / rebase needed instead of logging `scope_deviation`.

**Spec Currency:** Code Reviewer always validates against the **current** spec version (at review time), not the version when task was created. If spec changed materially since task creation:
- Reject if implementation no longer matches current spec
- Log `spec_changed` anomaly with details
- Orchestrator may need to rescope task based on spec delta

**v1 Limitation:** No automated spec_hash tracking. Code Reviewer must manually verify spec currency by checking `spec_changes` section in blackboard.

Code Reviewer does NOT evaluate:
- Style preferences
- Alternative approaches (unless current is defective)
- Scope expansion opportunities

### Binding Verdict Rules

**Rejection must use structured format** (from code-review skill):
```
Blockers: [count]
- [blocker] file:line — Issue description
  Why it matters: [impact]
  Suggestion: [fix]

Concerns: [count]
- [concern] file:line — Issue description

Overall: [1-2 sentence assessment]

Prior Feedback Status:  # Required for iteration 2+
- RESOLVED: [issues from prior rejection now fixed]
- STILL PRESENT: [issues not addressed]
- PARTIAL: [issues partially addressed]
```

**Rejection must include:**
- Specific file and location (`file:line` format)
- Specific defect or missing requirement (reference spec if applicable)
- Actionable fix description (what to change, not just "this is wrong")
- **For iteration 2+:** Prior Feedback Status section comparing to previous rejection

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

## Integration Analyst

**Purpose:** Scan the full branch diff after all coding tasks complete. Produce actionable fix-task definitions for mechanical integration issues.

**Capabilities:**
- Read full branch diff (`goal.base_commit..HEAD`) in assigned worktree
- Read all specs referenced by completed tasks
- Produce fix-task definitions as `output[]` entries (desc, done_when, scope, spec_ref)
- Mark self BLOCKED if issues require human judgment

**Constraints:**
- Does not commit code — submits with worktree HEAD unchanged
- TDD enforcement does not apply (task type `integration`, not `coding`)
- Findings must be mechanical and pattern-matchable: type alignment, serialization completeness, error mapping, API surface coherence, test/code agreement, code organization
- Judgment calls (architecture decisions, design trade-offs) are out of scope — escalate via BLOCKED

**Context:** Receives `branch-integration-context` section with the diff command and completed task list with `done_when` criteria. The analyst executes the diff in its worktree at analysis time — the diff is not stored in the blackboard.

**Clean scan:** When no issues are found, the analyst submits without setting output. The reviewer approves, and the task routes to `INTEGRATION_ANALYSIS_CLEAN` — a terminal state that bypasses per-subtask transition.

---

## Integration Reviewer

**Purpose:** Validate and enrich the integration analyst's findings with systemic analysis.

**Capabilities:**
- Read the same branch diff as the analyst
- Validate that each proposed fix-task is well-formed and correctly scoped
- Enrich findings with systemic concerns: interaction effects across fixes, architectural drift, emergent risks
- Approve or reject with binding verdict (same mechanism as Code Reviewer)

**Constraints:**
- Cannot modify the analyst's output — can only approve or reject
- Must verify each `output[]` entry has valid desc, done_when, scope, spec_ref
- Rejection must cite specific issues with task definitions, not disagree on whether findings are valid

**Context:** Receives `branch-integration-context` (same as analyst) plus `prior-rejection` context for re-reviews.

---

## Role Interaction Summary

| Actor | Can Commit To |
|-------|---------------|
| Coder | Task worktree branch only |
| Code Reviewer | None (read-only; approves for merge) |
| Integration Analyst | None (analysis only — submits with worktree HEAD unchanged) |
| Integration Reviewer | None (read-only; approves or rejects findings) |
| Orchestrator | Neither (no code changes) |
| Supervisor | Integration branch (executes merge after APPROVED) |

**Merge Execution:** The supervisor (`liza agent`) executes `liza wt-merge` after Code Reviewer sets status to APPROVED. This keeps agents permission-free in non-interactive mode while preserving the Code Reviewer approval gate.

## Agent Identity Protocol

Agents do not self-identify. Identity is **assigned by the supervisor** and passed as an environment variable.

### Identity Assignment

```bash
# Supervisor spawns agent with explicit identity
liza agent coder --agent-id coder-1
```

| Env Variable | Required | Format | Example |
|--------------|----------|--------|---------|
| `LIZA_AGENT_ID` | Yes | `{role}-{number}` | `coder-1`, `code-reviewer-2`, `orchestrator-1` |

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

Implemented by `liza agent` (see `internal/agent/registration.go`). The supervisor:
1. Acquires file lock on `state.yaml`
2. Checks if agent ID exists with active (non-expired) lease
3. If active lease found: exits with `COLLISION` error
4. If no conflict: writes agent entry with role, status, lease, heartbeat
5. Releases lock

### Agent Exit and Claim Release

When an agent exits (signal, crash, or graceful shutdown), `unregisterAgent()` atomically releases any active task claim and deletes the agent entry in a single `Modify` transaction. This prevents orphaned claims:

- **Coder exit:** Task transitions from IMPLEMENTING back to READY; `assigned_to` and `lease_expires` are cleared
- **Reviewer exit:** Task transitions from REVIEWING back to READY_FOR_REVIEW; `reviewing_by` and `review_lease_expires` are cleared

The supervisor defers `unregisterAgent` immediately after registration, ensuring cleanup runs regardless of how the agent exits.

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

- [Declarative Role Definitions](../build/3%20-%20Declarative%20Role%20Definitions.md) — roles as first-class YAML objects (type, timeouts, allowed operations, context sections)
- [Agent Initialization](../protocols/agent-initialization.md) — startup sequence from spawn to first action
- [Task Lifecycle](../protocols/task-lifecycle.md) — claim, iterate, review, merge
- [State Machines](state-machines.md) — task and agent state transitions
- [Sprint Governance](../protocols/sprint-governance.md) — checkpoints, retrospectives
