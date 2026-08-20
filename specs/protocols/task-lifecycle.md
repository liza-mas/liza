# Task Lifecycle Protocol

## Overview

Tasks flow through a lifecycle managed by Planner, Coder, and Code Reviewer roles. Each transition has explicit triggers and validation requirements.

For state diagrams and valid transitions, see [State Machines](../architecture/state-machines.md).

## Task Type and Role Workflow

> **Note:** The `task.type` field and static type registry described below reflect the current
> hardcoded system. The [Sub-pipelines spec](../build/2%20-%20Sub-pipelines and spec writing.md) replaces this
> mechanism with the `role_pair` field, which links tasks to their role-pair in the pipeline
> config for claimability and state resolution. The `type` field may remain as a human-readable
> category but will no longer drive role dispatch.

For pipeline-configured goals, `role_pair` determines which roles participate in a task lifecycle. When `type` is omitted during task creation, Liza derives it from the selected `role_pair`'s doer role. For legacy/non-pipeline data, the `type` field maps to an ordered role workflow via a static registry:

| Type | Workflow | Description |
|------|----------|-------------|
| `coding` | coder → code-reviewer | Standard code implementation with review |
| `planning` | code-planner → code-plan-reviewer | Code plan creation with review. TDD gate exempt. |
| `epic-planning` | epic-planner → epic-plan-reviewer | Epic/spec planning with review. TDD gate exempt. |
| `us-writing` | us-writer → us-reviewer | User-story writing with review. TDD gate exempt. |
| `integration` | integration-analyst → integration-reviewer | Branch-wide integration analysis. TDD gate exempt. Analyst produces fix-task definitions as `output[]`; approved findings auto-transition to coding-pair fix tasks. |
| `architecture` | architect → architecture-reviewer | Architecture planning with review. TDD gate exempt. |

When the `type` field is empty and no `role_pair` is available, it defaults to `"coding"` for backward compatibility. Unknown types are rejected by `liza validate`. New task creation rejects explicit `type` values that conflict with the selected `role_pair`.

The task type is normally derived during task creation from the Planner's selected `role_pair`; `liza add-task --type` is an explicit override only when it matches that role-pair. Pipeline-aware claiming uses `role_pair` for role eligibility.

See [State Machines — Type-Aware Claimability](../architecture/state-machines.md#type-aware-claimability) for the formal claimability rule.

### Multi-Phase Planning

For complex goals (>3 functional areas or >~8 estimated output entries), the orchestrator
creates multiple sequential planning tasks instead of one. Each phase covers a non-overlapping
scope boundary of the goal. Tasks use the same role-pair and are chained with sequential
`depends_on` (phase-2 depends on phase-1, phase-3 on phase-2, etc.).

Planners in later phases see a PHASE CONSISTENCY RULE requiring them to mark BLOCKED
(via `liza_mark_blocked`) if their plan cannot reconcile with prior phases' plans.
This blocking path is orthogonal to attempt exhaustion — it does not increment the task's attempt counter.

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
| Coder | 10 per attempt | Enough for complex tasks, bounded |
| Code Reviewer | 1 per review | Review should be decisive |
| Review cycles | 5 per attempt | Coder-Code Reviewer loop cap |

### Early Warning Thresholds

`liza tui` alerts before limits are hit (trajectory visibility):

| Metric | Warning | Cliff | Condition |
|--------|---------|-------|-----------|
| Coder iterations | 8 | 10 | Always |
| Review cycles | 3 | 5 | Always |
| Attempt | — | 2 | Warning at attempt 2 start |

### Attempt Transitions

Tasks have at most two attempts. Each attempt is a structural lifecycle unit
with independent iteration and review cycle budgets.

| Trigger | Attempt 1 Result | Attempt 2 Result |
|---------|------------------|------------------|
| Iteration cap (10) reached | New attempt: Attempt=2, counters reset, worktree deleted | BLOCKED |
| Review cycle cap (5) reached | New attempt: Attempt=2, counters reset, worktree deleted | BLOCKED |

Attempt transitions are independent of agent identity. Within an attempt, all
claims share the same counter budget regardless of which coder is assigned.

**Orthogonal to hypothesis exhaustion:** `failed_by` tracks integration failures
(two different coders BLOCKED on the same task). Cap-triggered attempt transitions
do not write to `failed_by`.

**Orthogonal to phase-consistency blocking:** Multi-phase planning allows later
planning phases to BLOCK immediately via PHASE CONSISTENCY RULE. That path is a
spec conflict / planning-scope escalation, not attempt exhaustion, and does not
increment `attempt` or trigger attempt transitions.

---

## Hypothesis Exhaustion Rule

If same task is BLOCKED by two different coders:

1. Task framing presumed wrong
2. Task cannot be reassigned unchanged
3. Planner must: rescope (→ SUPERSEDED), split, or abandon
4. Planner must identify and record root cause before rescoping; include it in `rescope_reason` and the log entry.

This prevents infinite polite failure.

**Note:** Hypothesis exhaustion tracks integration failures via `failed_by`, orthogonal to cap-triggered attempt transitions. Cap-triggered paths (iteration or review cycle limits) do not write to `failed_by`.

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
   - `rescope_reason: [root cause + rationale — ambiguity | missing dependency | architecture mismatch | invalid assumption | ...]`
3. Log entry records the rescope
4. Log entry includes a one-sentence root cause (what failed and why).

Original task history is preserved. No silent rewrites.

---

## Blocked Escalation

| Condition | Escalation |
|-----------|------------|
| Coder BLOCKED | Planner notified, may rescope |
| Code Reviewer and Coder deadlocked (5 cycles) | Planner intervenes (see Review Deadlock Protocol) |
| Integration failed | Task reclaimable with integration-fix scope |
| Review boundary metadata stale | Operator/supervisor runs `liza update-review-commit <task-id>`; task remains submitted/reviewable |
| Two coders failed same task | Hypothesis exhaustion → mandatory rescope |

### Review Budget Exhaustion Protocol

When Coder and Code Reviewer reach `max_review_cycles` (default: 5) without approval:

1. **Task transitions to BLOCKED** with `blocked_reason: "review_budget_exhausted"`
2. **Planner evaluates** the rejection/revision history:

| Planner Assessment | Action |
|--------------------|--------|
| Coder not addressing feedback | Reassign to different coder (preserves worktree) |
| Code Reviewer criteria unclear/shifting | Clarify spec, reset review_cycles, same coder continues |
| Genuine disagreement on approach | Rescope task with clearer constraints |
| Task fundamentally misframed | SUPERSEDED, create replacement task(s) |
| No viable path forward | ABANDONED (requires rationale in log) |

3. **Planner must log** `review_budget_exhausted` anomaly with assessment
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
liza claim-task task-3 coder-2
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

1. **Write goal spec:** Create a goal specification document
   - Default location: `specs/vision.md` (copy from `templates/vision-template.md`)
   - Alternative: Use a custom path for feature-specific goals
   - Fill in goal context and success criteria
   - Planner cannot decompose goal without this document

2. **Initialize Liza state:** `liza init "Goal description" --spec [spec_ref]`
   - `spec_ref` defaults to `specs/vision.md` if not provided
   - Requires the spec file to exist at the specified path
   - Creates `.liza/` directory structure
   - Creates `state.yaml` with goal (including `spec_ref`) and sprint initialized
   - Creates `log.yaml`

3. **Start watcher:** `liza tui` in separate terminal
   - Monitors for CHECKPOINT, anomalies, circuit breaker triggers

4. **Start agents:** Launch Planner, then Coders/Code Reviewers as needed
   - Each in separate terminal for observation

### Agent Startup Sequence

1. Read `~/.liza/CORE.md` → symlink to `<project>/contracts/CORE.md`
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
5. Finalize DRAFT → READY when plan complete
6. If tasks exist: monitor for blocked/escalation conditions
7. Write initial goal-alignment summary

### Coder Initialization

**Note:** The supervisor (`liza agent`) claims tasks and creates worktrees BEFORE spawning the coder. The coder receives its assigned task in the bootstrap prompt.

1. Extract task ID and worktree path from bootstrap prompt
2. Verify assignment in state.yaml (status IMPLEMENTING, assigned_to matches self)
3. Read the **full task entry** from blackboard (all fields: description, done_when, validation, destructive_db, scope, iteration, rejection_reason, etc.)
4. Read specs relevant to task (using task's `spec_ref`)
5. **For iteration 2+:** Read and address prior `rejection_reason` (extracted into prompt by supervisor)
6. If task under-specified (no clear spec) → BLOCKED with clarifying questions (see [Blocking Protocol](../architecture/roles.md#blocking-protocol))
7. Enter worktree, begin iteration loop

### Code Reviewer Initialization

**Note:** The supervisor (`liza agent`) assigns review tasks BEFORE spawning the reviewer. The reviewer receives its assigned task in the bootstrap prompt.

1. Extract review task ID and worktree path from bootstrap prompt
2. Verify assignment in state.yaml (status READY_FOR_REVIEW, reviewing_by matches self)
3. Verify commit SHA matches worktree HEAD
4. Read the **full task entry** from blackboard (all fields including `validation`, `destructive_db`, and prior `rejection_reason` for iteration 2+)
5. Read specs relevant to task (using task's `spec_ref`)
6. Examine worktree, validate against spec and `done_when` criteria, run task-declared `validation` commands when present, preserve any destructive DB marker exactly, and produce verdict
7. **For iteration 2+:** Compare current submission against prior rejection — report which issues are RESOLVED, STILL PRESENT, or PARTIAL
8. On approval: execute merge

## Integration Phase

Integration reverses planning's fan-out: it first establishes bounded local
coverage, then independently reviews the aggregate branch in one or more global
generations. `goal.base_commit` remains the stable goal-wide diff base, while
each analysis also carries immutable `integration_analysis` metadata.

### Lifecycle

1. **Settle and freeze planning exactly once.** Planning is settled only after
   every pre-integration planning source is terminal, each eligible
   coding-producing output and transition is consumed, and the resulting coding
   work is terminal. Partial planning handoff does not open this boundary. The
   first settled evaluation persists `integration.contributing_set`; subsequent
   evaluations validate and reuse that frozen cohort rather than recomputing it.
   A contributing scope is a pre-integration `code-planning-pair` with at least
   one distinct root coding lineage that produced merged work.
2. **Apply the zero-slice rule.** Fewer than two contributing scopes means no
   coverage records, zero slice analyses, and direct global analysis through an
   existing valid `integration-pair`. Only with multiple scopes does every
   contributing scope require one coverage record. A one-lineage scope reuses
   approval attestation evidence for every merged leaf: reviewed task and
   acceptance criteria, reviewed commit, approver, validation, and merge commit.
   It does not persist reviewer reasoning and creates no slice. A scope with at
   least two distinct root lineages is a multi-lineage scope and receives
   exactly one slice analysis, identified by the deterministic key
   `slice:<plan-task-id>`.
3. **Review the bounded slice surface.** Slice metadata records the originating
   plan, root task IDs, merged descendant changes, affected paths, immutable
   source commit, and source snapshot paths that exist at that commit. This is a
   bounded review surface for intra-plan composition, not the entire goal.
   Coding, fix, and replacement lineage determines which merged leaves and
   repairs belong to the slice. Later sibling mutations do not reopen completed
   slices; their cross-scope effects belong to global review.
4. **Resolve findings fail closed.** Empty `output[]` records verdict `clean`.
   Findings record verdict `findings`, and the automatic phase transition creates
   standard reviewed coding fixes. A repair is resolved only by a merged fix or
   merged replacement lineage; superseded work waits for its replacement leaves.
   Unresolved blocked or abandoned findings fail closed and block fan-in. Slice
   task or review exhaustion likewise becomes explicit blocked closure rather
   than silently satisfying coverage.
5. **Run independent global analysis.** Global analysis waits for planning to be
   settled and all coding and integration repair work to be terminal. When the
   frozen cohort has multiple scopes, it additionally waits for complete
   coverage for every contributing scope and every created slice to be resolved;
   a zero- or one-scope cohort has no coverage records to await. Local records,
   when present, are navigation evidence, not proof of aggregate correctness.
   The `integration-pair` independently checks the current goal-wide integration
   HEAD for cross-scope interaction, shared-interface, test/specification,
   architectural, and merge-readiness failures.
6. **Repeat bounded global generations.** Global metadata uses deterministic
   keys `global:<generation>` and binds each verdict and report commit to an
   immutable source commit. Promoted integration-escalation repairs remain
   repair work outside the frozen cohort: they create no new slice and, after
   merging, trigger another global generation. Any later integration HEAD
   mutation also triggers another global generation while budget remains.
   Slice exhaustion is explicitly blocked; global generation exhaustion is
   explicitly `exhausted`. Neither is successful completion.
7. **Complete only on current clean evidence.** An empty global output records a
   clean generation, but clean closure is projected only when the reviewed
   source commit equals live integration HEAD. Sprint progression and automatic
   goal stop re-evaluate that authoritative completion decision rather than
   deriving success from terminal task counts.

### Persisted Evidence

`goal.integration` is an append-only lifecycle ledger plus a current closure
projection:

- `contributing_set.scopes[]` freezes each `plan_task_id` and its root task IDs;
- `coverage[]` remains empty for a frozen cohort with fewer than two scopes;
  when the cohort has at least two scopes, it is a tagged union of
  `approval_attestations[]` or one `slice_report` for every contributing scope;
- slice reports and `global_generations[]` record analysis task ID, analysis
  key, verdict (`clean` or `findings`), immutable `source_commit`, and reviewed
  `report_commit`;
- `mutation_receipts[]` records task ID plus before/after integration commits;
- `closure` projects `clean`, `blocked`, or `exhausted`, with clean closure also
  naming its generation, analysis key, and source commit.

Approval attestations intentionally contain approval facts, not persisted
reviewer reasoning. Slice analysis task metadata holds the more detailed bounded
surface (`descendant_changes`, `affected_paths`, and `source_snapshot_paths`).

### Auto-Transitions

`slice-integration-to-fix` and `integration-to-fix` have `trigger: auto` and
create one `coding-pair` child per approved output entry. They fan out from the
analysis `APPROVED` state, not `MERGED`, because analysts do not merge code.
Large findings may be promoted through `code-planning-pair`, but that plan and
its coding descendants retain the originating analysis as an ancestor and are
therefore integration repairs outside the frozen contributing cohort.

### Reconciliation and Recovery

The pure progress evaluator returns waiting, blocked, exhausted, slice-request,
global-request, or complete outcomes without mutating state. Reconciliation
projects those outcomes atomically. Analysis keys map to deterministic task IDs;
an existing task must match its phase, metadata, parents, initial status, and
planned membership. Consequently wake and restart reconciliation creates no
duplicate analyses: exactly one writer creates each requested slice or global
analysis and every later invocation is a no-op or fails on an evidence
collision. Authoritative wake projection reports reconciliation work,
waiting, blocked, exhausted, unavailable, or complete without inventing tasks
from terminal-count heuristics.

### Goal BaseCommit

`goal.base_commit` is snapshotted when the first coding-pair children are
created (from any pipeline transition). It records the integration branch HEAD
at that point, giving global analysis the stable lower bound for the goal-wide
diff. Per-slice immutable source snapshots are recorded separately.

---

## Related Documents

- [Agent Initialization](agent-initialization.md) — startup sequence from spawn to first action
- [State Machines](../architecture/state-machines.md) — state transitions
- [Roles](../architecture/roles.md) — role responsibilities
- [Worktree Management](worktree-management.md) — worktree operations
- [Sprint Governance](sprint-governance.md) — checkpoints, retrospectives
