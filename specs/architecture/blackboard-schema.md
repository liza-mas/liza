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
  spec_ref: specs/vision.md  # Path to goal specification document
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
    type: coding  # "coding" (coder → code-reviewer) or "planning" (code-planner → code-plan-reviewer). Default: "coding". Superseded by role_pair — see Sub-pipelines spec
    description: "Add retry decorator to UserAPI.get_user()"
    status: MERGED
    priority: 1
    created: 2025-01-17T14:05:00Z
    worktree: null  # cleaned up after merge
    review_commit: a1b2c3d4
    merge_commit: d4e5f6a7
    spec_ref: specs/retry-logic.md  # Path to spec, optionally with #anchor
    done_when: "UserAPI.get_user() retries 3x on 5xx errors with exponential backoff"
    history:
      - { time: "2025-01-17T14:05:00Z", event: "created" }
      - { time: "2025-01-17T14:06:00Z", event: "claimed", agent: "coder-1" }
      - { time: "2025-01-17T14:25:00Z", event: "ready_for_review", commit: "a1b2c3d4" }
      - { time: "2025-01-17T14:28:00Z", event: "approved", agent: "code-reviewer-1" }
      - { time: "2025-01-17T14:29:00Z", event: "merged", extra: { tests_ran: true } }

  - id: task-2
    description: "Add retry decorator to OrderAPI.create_order()"
    status: REJECTED
    priority: 2
    assigned_to: coder-1
    worktree: .worktrees/task-2
    iteration: 3  # Current iteration within this task (Ralph loop count)
    exit42_restart_count: 0   # Consecutive exit-42 restarts without progress (reset on state change)
    review_cycles_current: 2  # Reset to 0 on new attempt
    review_cycles_total: 2    # Never reset (audit trail)
    attempt: 1                # 0=unset, 1=first, 2=second
    review_commit: b2c3d4e5
    spec_ref: specs/retry-logic.md
    done_when: "OrderAPI.create_order() retries only after idempotency key validation"
    rejection_reason: |
      Blockers: 1
      - [blocker] src/api/order.py:47 — Retry applied to non-idempotent POST
        Why it matters: Duplicate orders on network retry
        Suggestion: Add idempotency key validation before retry. See spec section 3.2.

      Concerns: 0

      Overall: Core retry logic is correct but cannot be applied to POST without idempotency.

      Prior Feedback Status:
      - RESOLVED: Missing test coverage (now has unit tests)
      - STILL PRESENT: Idempotency check missing
    created: 2025-01-17T14:05:00Z
    history:
      - { time: "2025-01-17T14:05:00Z", event: "created" }
      - { time: "2025-01-17T14:06:00Z", event: "claimed", agent: "coder-1" }
      - { time: "2025-01-17T14:20:00Z", event: "ready_for_review", commit: "a1b2c3d4" }
      - { time: "2025-01-17T14:25:00Z", event: "rejected", agent: "code-reviewer-1", reason: "Blockers: 2\n- [blocker] Missing tests\n- [blocker] Idempotency check missing" }
      - { time: "2025-01-17T14:26:00Z", event: "reclaimed_after_rejection", agent: "coder-1" }
      - { time: "2025-01-17T14:40:00Z", event: "ready_for_review", commit: "b2c3d4e5" }
      - { time: "2025-01-17T14:45:00Z", event: "rejected", agent: "code-reviewer-1", reason: "Blockers: 1\n- [blocker] Idempotency check still missing\n\nPrior Feedback Status:\n- RESOLVED: Missing tests\n- STILL PRESENT: Idempotency check" }

  - id: task-3
    description: "Add retry decorator to PaymentAPI.charge()"
    status: DRAFT  # Orchestrator still defining — missing done_when keeps it DRAFT
    priority: 3
    worktree: null
    spec_ref: specs/retry-logic.md#payments
    # done_when: TBD — intentionally incomplete to show DRAFT state requirement
    # Orchestrator must define done_when before finalizing to READY
    created: 2025-01-17T15:00:00Z

  - id: task-4
    description: "Original task that was too broad"
    status: SUPERSEDED
    superseded_by: [task-4a, task-4b]
    rescope_reason: "Wrong granularity — split into auth and validation subtasks"
    priority: 2
    created: 2025-01-17T14:10:00Z

  - id: task-5
    description: "Task whose work was completed in a prior sprint"
    status: SUPERSEDED
    # superseded_by is optional — omit when no replacement tasks exist
    rescope_reason: "Auth middleware already merged in sprint-2"
    priority: 1
    created: 2025-01-17T13:00:00Z

  - id: task-4a
    description: "Add auth retry logic"
    status: READY
    supersedes: task-4
    priority: 2
    depends_on: []  # No dependencies — can be claimed immediately
    spec_ref: specs/retry-logic.md#auth
    done_when: "Auth endpoints return 401 on invalid token and retry succeeds after token refresh"
    created: 2025-01-17T14:50:00Z

  - id: task-4b
    description: "Add validation retry logic"
    status: READY
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
    repair_request:
      operation: add-task
      target: architecture-2
      command: "liza add-task --id architecture-2 ... --agent-id orchestrator-1 --json"
      evidence:
        - "coder command failed: command requires role type [orchestrator]"
      validation:
        - "python -m pytest -q tests/backend/test_workflow_contract.py -q"
    failed_by: [coder-1, coder-2]  # Tracks hypothesis exhaustion
    created: 2025-01-17T16:00:00Z

  - id: task-7
    description: "Fix merge conflict in UserAPI retry logic"
    status: IMPLEMENTING
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
    approved_by: null  # Set on approval; used by supervisor to merge only its reviewer's approvals
    spec_ref: specs/api.md#rate-limiting
    done_when: "Public endpoints return 429 with Retry-After header when limit exceeded"
    created: 2025-01-17T16:45:00Z
```

### Sub-pipeline Fields

Tasks support inter-pair transitions via `liza proceed` (manual) or orchestrator PreWork (automatic after planning checkpoint):

```yaml
- id: plan-task-1
  status: CODING_PLAN_APPROVED
  output:                          # Structured subtask definitions from doer role
    - desc: "Implement auth middleware"
      done_when: "Auth middleware rejects invalid tokens"
      scope: "src/middleware/auth.go"
      spec_ref: specs/auth.md
      plan_ref: specs/plans/auth-master-plan.md
      decomposition:
        owned_files: ["src/middleware/auth.go"]
        owned_modules: ["auth middleware"]
        read_only_depends_on: []
        read_only_task_depends_on: []
        interfaces_owned: ["auth middleware contract"]
        interfaces_consumed: []
        coverage_notes: "Owns request authentication boundary."
    - desc: "Add token refresh logic"
      done_when: "Expired tokens trigger refresh flow"
      scope: "src/auth/refresh.go"
      spec_ref: specs/auth.md#refresh
  parent_task: null                # Deprecated: use parent_tasks. Set on child tasks, references parent task ID
  parent_tasks: []                 # Multi-parent linkage (many-to-one transitions). Back-references from child to parent tasks
  transitions_executed:            # Tracks which transitions have been applied
    code-plan-to-coding: true
```

Pipeline topology itself is frozen in `.liza/pipeline.yaml` at `liza init`. Role-pair schema supports `role-pairs.<name>.decomposition-root: true` for master planning pairs. That marker is read-only runtime metadata: it selects master prompt sections, output validation, and INITIAL_PLANNING's specialized-to-master mapping. Existing frozen workspaces are not rewritten when the embedded topology changes; users must re-run `liza init` to receive new role-pairs or transitions.

| Field | Type | Set By | Purpose |
|-------|------|--------|---------|
| `output` | `[]OutputEntry` | Doer agent | Structured subtask definitions for next role pair |
| `arch_ref` | `string` | `liza proceed` | Path to architecture document (repo-relative). Set on child tasks during transition: first hop copies from parent's `output[]` entry, second hop inherits from parent task field. Validated via `checkSpecFileExists` (same pattern as `plan_ref`). |
| `parent_task` | `*string` | `liza proceed` / orchestrator | Back-reference from child to parent task (deprecated: use `parent_tasks`) |
| `parent_tasks` | `[]string` | `liza proceed` / orchestrator | Multi-parent back-references (used by many-to-one transitions; supersedes `parent_task`) |
| `transitions_executed` | `map[string]bool` | `liza proceed` / orchestrator | Idempotency — prevents duplicate transitions. For `many-to-one` transitions, set on **all** cohort members (not just the trigger task) to prevent re-firing from any member |

**OutputEntry fields:**

Required:
- `desc`: Task description for the child task
- `done_when`: Completion criteria
- `scope`: Files/areas affected
- `spec_ref`: Specification reference

Optional:
- `plan_ref` (`string`): Path to the plan artifact (repo-relative). Set by doer via `set-task-output`. Normalized by `NormalizeSpecRef` (worktree prefixes stripped).
- `arch_ref` (`string`): Path to the architecture document (repo-relative). Set by architect via `set-task-output`. Normalized by `NormalizeSpecRef` (worktree prefixes stripped). Propagated to child tasks by `proceed.go` during transitions.
- `epic_ref` (`string`): Path to a concrete epic artifact (repo-relative). Specialized `epic-planning-pair` outputs use this for `us-writing-pair` children; epic master framework refs use `plan_ref`, not `epic_ref`.
- `task_depends_on` (`[]string`): Existing concrete task IDs outside this `output[]`. Set by doer via `set-task-output`; copied to generated child tasks as scheduler-facing `depends_on`.
- `decomposition` (`DecompositionManifest`): Typed decomposition metadata. Required on `output[]` entries produced by `decomposition-root` role-pairs and optional elsewhere.

`task_depends_on` must be legal for every per-subtask transition target that can consume the output. A dependency is illegal when the referenced task's `role_pair` is downstream of the generated child's `role_pair` in the configured transition graph; same-role-pair dependencies are allowed. Supersession chains are checked as dependency paths, so a dependency that resolves through `superseded_by` to a downstream role-pair is also invalid.

**DecompositionManifest fields:**

| Field | Type | Purpose |
|-------|------|---------|
| `owned_files` | `[]string` | Exact files this output entry owns when knowable |
| `owned_modules` | `[]string` | Modules, packages, components, or functional areas owned by this entry |
| `read_only_depends_on` | `[]int` | Sibling `output[]` indexes consumed read-only |
| `read_only_task_depends_on` | `[]string` | Existing concrete task IDs consumed read-only |
| `interfaces_owned` | `[]string` | Named interfaces or contracts this entry defines |
| `interfaces_consumed` | `[]string` | Named interfaces or contracts this entry consumes |
| `coverage_notes` | `string` | Why this entry is bounded and how it contributes to full goal coverage |

`read_only_depends_on` and `read_only_task_depends_on` do not schedule work by themselves. They must be mirrored in scheduler-facing `depends_on` and `task_depends_on`; validation rejects decomposition-root output where the read-only metadata and scheduling dependency fields diverge.

Generated child tasks also persist task-level `decomposition` metadata copied from the source `output[]` entry. Task-level metadata is read-only context for the child and does not change dependency scheduling.

For decomposition-root outputs, `liza set-task-output` requires a role-appropriate framework ref on every entry:

| Master role-pair | Required output ref | Child target |
|------------------|---------------------|--------------|
| `epic-planning-main-pair` | `plan_ref` | `epic-planning-pair` |
| `architecture-main-pair` | `arch_ref` | `architecture-pair` |
| `code-planning-main-pair` | `plan_ref` | `code-planning-pair` |

Task-level inherited refs and output-entry produced refs have different meanings. A specialized child reads task-level `plan_ref` or `arch_ref` as the master framework it must respect, then may emit its own output-entry `plan_ref`, `arch_ref`, or `epic_ref` for downstream children. `architecture-to-code-plan` remains the Case A bypass: specialized `architecture-pair` entries produce `arch_ref` for `code-planning-pair` children and do not route through `code-planning-main-pair`.

Artifact reference fields are scalar repo-relative refs, optionally with a
`#fragment` anchor. The protected artifact fields are goal `spec_ref`; task
`spec_ref`, `epic_ref`, `plan_ref`, and `arch_ref`; and durable `output[]` entry
`spec_ref`, `epic_ref`, `plan_ref`, and `arch_ref`. Output refs become durable
artifact requirements only after their owning task is MERGED, or while that task
is the candidate currently being merged. Non-merged `output[]` remains live
planning/rework context, not a global artifact requirement. Delimiter-joined
multi-refs such as `specs/a.md; specs/b.md` are invalid; use scope text or a
future structured multi-ref field instead. Artifact refs also fail closed when
fragment stripping leaves an empty path, the path traverses outside the
repository, or an absolute ref cannot be safely normalized to a repo-relative
path.

Candidate integration validation strips the optional fragment and checks the
repo-relative path against the candidate Git tree before integration ref
advancement. It protects goal refs, task-level refs, already-MERGED tasks'
`output[]` refs, and the merging task's own `output[]` refs. It intentionally
ignores unrelated non-merged task output refs because those artifacts may exist
only in sibling worktrees until those tasks merge. Valid protected artifact refs
must resolve to regular Git files with mode `100644` or `100755`. Missing
paths, directories, submodules/gitlinks, symlinks, and other non-regular Git
object modes are rejected. Diagnostics are deterministic and include the invalid
path plus owner provenance: field name, task ID when the owner is a task, and
output index when the owner is an `output[]` entry. Post-merge merge-scoped
artifact validation still runs after a successful ref update as the rollback
backstop.

Live attempt metadata represents the current actionable projection, not all
audit history. Rejection clears stale review metadata (`review_commit`,
approvals, `merge_commit`, `integration_failure`) but keeps `output[]` for
rework. Doer claim release clears `output[]` and review metadata while
preserving `failed_by`. Fresh-attempt reset paths clear `output[]`, review
metadata, and `failed_by`; retire paths clear review/failure metadata while
keeping terminal context such as `output[]` and `failed_by`. Historical evidence
remains in `history[]` entries where those entries recorded it.

**`arch_ref` Propagation:**

`arch_ref` flows through the pipeline in two hops:

| Hop | Source | Target | Mechanism |
|-----|--------|--------|-----------|
| First | Architect's `output[]` entry `.arch_ref` | Code-planning child task `.arch_ref` | `proceed.go` `buildChildTask` copies `entry.ArchRef` |
| Second | Parent code-planning task `.arch_ref` | Coding child task `.arch_ref` | `proceed.go` `buildOneToOneChild` / `buildChildTask` inherits `parent.ArchRef` as fallback when entry has no `arch_ref` |

Precedence: entry-level `arch_ref` takes priority over parent task `arch_ref`. This allows an output entry to override the inherited architecture document if needed.

| Task | `spec_ref` | `arch_ref` | `plan_ref` |
|------|-----------|-----------|-----------|
| Architecture | goal spec | — (produces it via `output[]`) | — |
| Code-planning | from `output[]` entry | from architecture task's `output[]` entry (first hop) | — |
| Coding | from `output[]` entry | inherited from parent code-planning task (second hop) | from code-planner's `output[]` entry |

**`parent-tasks-context` prompt section:**

The `parent-tasks-context` template block renders upstream parent task metadata for the architect role. It is registered in the architect's `context-sections` list in pipeline YAML.

| Attribute | Value |
|-----------|-------|
| Name | `parent-tasks-context` |
| Used by | architect |
| Data source | `EffectiveParentTasks()` — resolves `parent_tasks` (or deprecated `parent_task`) to task objects from state |
| Rendered fields | ID, description, done_when, spec_ref, plan_ref for each parent task |
| Behavior | Iterates over parent tasks and renders a metadata block per parent. When the parent list is empty (e.g., detailed-spec entry point with no upstream US tasks), the section produces no output. |
| Scope | Architect consolidation — provides upstream deliverable context so the architect can locate and read relevant documents via standard tools. Does **not** embed file content; renders pointers only. |

**Available transitions:**

| Name | Source Status | Cardinality | Effect |
|------|-------------|-------------|--------|
| `epic-decompose` | `EPIC_PLAN_MAIN_APPROVED` | `per-subtask` | Auto-creates specialized epic-planning tasks from master output |
| `arch-decompose` | `ARCHITECTURE_MAIN_APPROVED` | `per-subtask` | Auto-creates specialized architecture tasks from master output |
| `code-plan-decompose` | `CODING_PLAN_MAIN_APPROVED` | `per-subtask` | Auto-creates specialized code-planning tasks from master output |
| `us-to-coding` | `US_APPROVED` | `many-to-one` | When all cohort siblings reach approved, creates **one** child architecture master task linked to all N parents |
| `architecture-to-code-plan` | `ARCHITECTURE_APPROVED` | `per-subtask` | Creates child code-planning tasks at DRAFT from `output[]` |
| `code-plan-to-coding` | `CODING_PLAN_APPROVED` | `per-subtask` | Creates child coding tasks at DRAFT from `output[]` |

**Transition cardinalities:**
- `per-subtask`: One child task per `output[]` entry. Child ID: `{parent-id}-{transition-name}-{index}`.
- `one-to-one`: One child task from the parent task itself. Child ID: `{parent-id}-{transition-name}`.
- `many-to-one`: All sibling tasks sharing a `parent_task` must reach the `from` status. Creates one child linked to all N parents via `parent_tasks`. Child ID: deterministic from the cohort (transition name + shared parent task ID). `transitions_executed` is set on **all** cohort members for idempotency and crash recovery.

**Child task ID format:** `{parent-id}-{transition-name}-{index}` (deterministic, namespaced by transition for crash recovery). Example: `task-1-code-plan-to-coding-0`.

**Crash recovery:** Re-running `liza proceed` creates only missing children (with inherited deps). Existing children are patched with missing inherited deps. If all children already exist with correct deps, returns error.

**Auto-inherited DependsOn:** When a source task has `depends_on` and the upstream dependency
has already executed the same transition, child tasks inherit those upstream children as
additional `depends_on` entries. Dependency composition order is:
1. Sibling deps from `output[].depends_on` index references, resolved to generated child task IDs
2. Concrete task deps from `output[].task_depends_on`
3. Inherited phase-gate deps from upstream parents' children

Before creating or crash-recovery patching a child task, the final composed `depends_on` set is canonicalized and validated against pipeline direction. Superseded inherited children are rewritten to replacements when the replacements can be legally encoded; downstream replacements that are already MERGED are treated as satisfied and omitted; pending downstream replacements fail the affected transition. The dependency task's `role_pair` must not be reachable downstream from the child task's `role_pair` through sub-pipeline transitions or top-level `pipeline-transitions`.

**`transition_cycle_blocked` history event:** Added by `ExecuteAvailableTransitions` when
circular `depends_on` prevents topological ordering. Semantics:
- Does NOT modify task Status (remains MERGED)
- Does NOT modify `transitions_executed` (no forgery)
- Does NOT satisfy downstream dependencies
- True cycle members are excluded from orchestrator wake detection and planning-complete prompt rendering via `IsPlanningCompleteEligible`
- Tasks transitively downstream of those cycle members are also excluded from `PLANNING_COMPLETE` by the same predicate, but do not get a durable `transition_cycle_blocked` event
- Checkpoint auto-trigger (`sprint_checkpoint.go`) still uses `IsUnconsumedPlanningOutput` today
- Idempotent per (taskID, transitionName, sorted cycle member IDs)
- Cycle members stored in `Extra["cycle_members"]` (sorted task ID list)

### Iteration Field Lifecycle

The `iteration` field tracks coder work cycles on a task:

| Event | `iteration` Value |
|-------|-------------------|
| Task created (DRAFT/READY) | Not set (null) |
| First claim (READY → IMPLEMENTING) | Set to 1 |
| Work iteration complete | Unchanged (work within single claim) |
| Review rejected (REJECTED → IMPLEMENTING, same coder) | Increment by 1 |
| New attempt triggered | Reset to 0 |
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

| Field | Reset on New Attempt | Purpose |
|-------|----------------------|---------|
| `review_cycles_current` | Yes (→ 0) | Limit check — new attempt gets full budget |
| `review_cycles_total` | No | Audit trail — total rejections across all attempts |

**Rationale:** Budget reset is about approach exhaustion, not personnel change. When cap-triggered attempt transition occurs, counters reset so the new attempt starts with full iteration and review budget.

**Limit checks use `review_cycles_current`; retrospectives use `review_cycles_total`.**

### Attempt Field Lifecycle

The `attempt` field tracks the structural lifecycle unit for a task:

| Event | `attempt` Value |
|-------|-----------------|
| Task created (DRAFT/READY) | 0 (unset) |
| First claim | Set to 1 |
| Cap hit (iteration 10 or review_cycles 5), attempt 1 | Set to 2, counters reset, worktree deleted |
| Cap hit, attempt 2 | BLOCKED |

The `attempt` field is a structural lifecycle counter independent of agent identity. Within an attempt, all claims (same or different coder) share the same counter budget.

### Rejection Reason Format

The `rejection_reason` field uses a structured format derived from the code-review skill:

```yaml
rejection_reason: |
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

**Requirements:**
- Blockers and Concerns must reference specific `file:line` locations
- Each issue must include actionable suggestion
- For iteration 2+: Prior Feedback Status section is mandatory

**Rationale:** Structured format enables:
- Coder to address specific locations rather than interpreting prose
- Reviewer to track feedback continuity across iterations
- Watcher to detect oscillation patterns (issue flip-flopping between RESOLVED and STILL PRESENT)

**History tracking:** Rejection events in task history include the full `reason` field for audit trail:
```yaml
history:
  - { time: "...", event: "rejected", agent: "code-reviewer-1", reason: "Blockers: 1\n- [blocker] ..." }
```

### Task Dependencies

The `depends_on` field declares explicit dependencies between tasks:

```yaml
- id: task-auth
  status: READY
  depends_on: []  # No dependencies

- id: task-validation
  status: READY
  depends_on: [task-auth]  # Blocked until task-auth is MERGED
```

**Semantics:**
- `depends_on` is an array of task IDs that must reach MERGED status before this task can be claimed
- `depends_on` must not point to a downstream pipeline role-pair. Same-role-pair and upstream dependencies are valid.
- Active tasks must not depend on terminal non-MERGED tasks. When a task is superseded, active downstream `depends_on` entries are rewritten to its replacements; when a task is cancelled, active downstream `depends_on` entries pointing at it are removed. Terminal tasks keep historical dependency edges for audit.
- Explicit `output[].task_depends_on` writes reject terminal non-MERGED task IDs. Operational output and generated child `depends_on` follow the canonical dependency rule before they can mint or patch child tasks: superseded entries are rewritten to legal replacements, cancelled or unreplaced retired entries are removed, downstream replacements that are already MERGED are treated as satisfied and omitted from child dependencies, and illegal pending replacements fail the affected mutation or transition instead of being silently dropped. `SUPERSEDED` and `ABANDONED` task output remains audit history unless crash recovery can still consume it.
- Empty array or missing field means no dependencies — task is immediately claimable
- Coders can only claim tasks where ALL dependencies are satisfied
- Orchestrator sets dependencies during task creation based on logical ordering

**Claimability Rule:**
```
claimable = (status in [READY, REJECTED, INTEGRATION_FAILED]) AND (depends_on is empty OR all depends_on tasks are MERGED)
```

- **READY**: Fresh task ready for first attempt
- **REJECTED**: Code review failed; coder can reclaim to address feedback
- **INTEGRATION_FAILED**: Merge failed; coder can reclaim to resolve conflicts

**Why explicit dependencies?**
- Without explicit dependencies, Coders discover blockers at runtime → scattered BLOCKED tasks
- Orchestrator has context to identify dependencies during decomposition
- Explicit dependencies enable parallel work on independent tasks
- Dependencies surface the critical path for human visibility

**Dependency vs BLOCKED:**
- `depends_on`: Known at planning time — task waits automatically
- `BLOCKED`: Discovered at runtime — requires Orchestrator intervention

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

  orchestrator-1:
    role: orchestrator
    status: WAITING
    task: null
    lease_expires: null
    heartbeat: 2025-01-17T14:51:00Z
    terminal: /dev/pts/1

agent_health:
  coder-1:
    state: degraded
    role: coder
    provider: codex
    pid: 12345
    registered_at: 2025-01-17T14:52:00Z
    degraded_at: 2025-01-17T14:53:00Z
    reason: claim_worktree_create_failed
    last_task: task-2
    candidate_tasks: [task-2, task-3]
    last_error: "failed to create worktree: cannot lock ref"
    recover_hint: "restart the agent from a process context that can write project git refs and .worktrees"

discovered:
  - id: disc-1
    by: coder-1
    during: task-2
    source: null  # null or omitted = implementation discovery (default)
    description: "OrderAPI.create_order() has no idempotency key support"
    severity: high
    urgency: deferred  # deferred (default), immediate — immediate wakes Orchestrator
    recommendation: "Add idempotency key parameter before retry logic"
    created: 2025-01-17T14:46:00Z
    converted_to_task: null  # null, task-id, "deferred", or "dismissed"

  - id: disc-2
    by: coder-1
    during: task-3
    source: null
    description: "Auth token refresh needed before retry can succeed"
    severity: critical
    urgency: immediate  # Wakes Orchestrator immediately — blocks current work
    recommendation: "Must add auth refresh to unblock task-3"
    created: 2025-01-17T15:30:00Z
    converted_to_task: task-3a

  - id: disc-3
    by: code-reviewer-1
    during: task-8
    source: systemic-thinking  # Analytical finding from systemic review
    description: "[TENSION] Rate limiting implementation assumes single-instance deployment but spec mentions horizontal scaling"
    severity: high
    urgency: deferred
    recommendation: "Rate limiting strategy will fail under horizontal scaling pressure"
    created: 2025-01-17T17:00:00Z
    converted_to_task: null  # Orchestrator evaluates: task, "deferred" (→ ISSUES_FILE), or "dismissed"

**Discovery Fields:**

| Field | Values | Meaning |
|-------|--------|---------|
| `source` | `null` / omitted | Implementation discovery (default — found during coding) |
| | `systemic-thinking` | Analytical finding from systemic review (typically by Code Reviewer) |
| `severity` | `critical` | Blocks current task; must address before continuing |
| | `high` | Significant issue; should address soon |
| | `medium` | Notable finding; address when convenient |
| | `low` | Nice-to-have; log for future consideration |
| `urgency` | `immediate` | Wakes Orchestrator now (for critical blockers) |
| | `deferred` | Orchestrator reviews at next planning cycle (default) |
| `converted_to_task` | `null` | Not yet evaluated by Orchestrator |
| | `task-N` | Orchestrator created task to address |
| | `deferred` | Orchestrator wrote to ISSUES_FILE — acknowledged, not actionable now |
| | `dismissed` | Orchestrator evaluated and dismissed — no action warranted |

**Usage:** Coders encountering nice-to-haves during implementation log them with `severity: low, urgency: deferred` rather than blocking or scope-creeping. Code Reviewers invoking the systemic-thinking skill log findings with `source: systemic-thinking` (see skill for severity mapping).

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
  checkpoint_trigger: ""  # Why last checkpoint was created: PLANNING_COMPLETE, MANY_TO_ONE_READY, SPRINT_COMPLETE, or empty (manual/other)
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
  lease_duration: 1800          # Seconds (30 minutes)
  coder_poll_interval: 30       # Seconds between work availability checks
  doer_max_wait: 18000          # Max seconds doer supervisors wait for claimable work
  orchestrator_poll_interval: 60 # Seconds between orchestrator work checks
  orchestrator_max_wait: 18000  # Max seconds orchestrator waits for work
  reviewer_poll_interval: 30    # Seconds between reviewer work checks
  reviewer_max_wait: 18000      # Max seconds reviewers wait for work
  exit42_restart_threshold: 5   # Consecutive exit-42 restarts without progress before BLOCKED (default: 5)
  exit42_max_backoff_seconds: 60 # Max backoff delay between exit-42 restarts (default: 60)
  default_cli: claude           # Optional global default agent CLI
  default_doer_cli: codex       # Optional default CLI for doer and orchestrator roles
  default_reviewer_cli: gemini  # Optional default CLI for reviewer roles
  integration_branch: integration
  escalation_webhook: null      # Optional: URL for external notifications
```

**Circuit-breaker acknowledgement watermark:** When
`circuit_breaker.status == OK` and `current_trigger == null`, the latest
history entry with `result: TRIGGERED` is the acknowledgement watermark for
future circuit-breaker checks. `liza analyze` and `liza tui` consider only
anomalies with `timestamp` strictly after that watermark. Later `OK` entries do
not move the watermark. If `status == TRIGGERED` or `current_trigger` is
non-null, no watermark applies.

**Config Scope:**
- Config values are **goal-level defaults** (apply to all tasks in current goal)
- Agent CLI defaults resolve in this order: explicit `--cli`, role-specific config
  (`default_doer_cli` for doers and orchestrators, `default_reviewer_cli` for reviewers),
  role-specific env (`LIZA_DEFAULT_DOER_CLI` / `LIZA_DEFAULT_REVIEWER_CLI`),
  `default_cli`, `LIZA_DEFAULT_CLI`, then `claude`.
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
  agent: orchestrator-1
  action: goal_created
  detail: "Implement retry logic for all API calls with exponential backoff"

- timestamp: 2025-01-17T14:05:00Z
  agent: orchestrator-1
  action: tasks_finalized
  detail: "5 tasks moved from DRAFT to READY"

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
  agent: orchestrator-1
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
- On claim: set `lease_expires` to now + lease_duration (default: 30 minutes)
- Heartbeat extends lease by lease_duration
- Task reclaimable only after lease expires
- If original agent returns after expiry → must self-abort immediately

**Lease and Review States:**
- Coder lease (`lease_expires`) governs IMPLEMENTING state only
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
| `approved_by` | Agent ID of Code Reviewer who approved the task (null until approved) |
| `merge_commit` | Integration branch commit SHA created by merge (null until merged) |

Code Reviewer lease prevents two Code Reviewers examining same task simultaneously and enables recovery from Code Reviewer crash.

When a task is in the pipeline reviewing state or reviewing-2 state,
`reviewing_by` is active ownership only if it matches the agent-side row: the
agent must have the exact reviewer role resolved from the task's `role_pair`,
status `REVIEWING`, `current_task` equal to the task ID, and a valid review
lease. A `reviewing_by` value on non-reviewing states is stale/orphaned state,
not an active claim, except while a `WAITING` reviewer is passively awaiting
resubmission for a rejected/executing task with an unexpired review lease.

**Heartbeat interval:** 60 seconds
**Lease duration:** 1800 seconds (30 minutes)
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
| Claim review | Supervisor | Lock → verify READY_FOR_REVIEW → set REVIEWING + write reviewing_by + review_lease_expires → unlock |
| Extend review lease | Code Reviewer | Lock → update review_lease_expires → unlock |
| Submit verdict | Code Reviewer | Lock → verify REVIEWING + commit SHA matches + reviewing_by matches self → set APPROVED/REJECTED + reason + set approved_by on approval + clear review lease → unlock |
| Execute merge | Supervisor | After Code Reviewer sets APPROVED → supervisor runs `liza wt-merge` → update state to MERGED |
| Mark blocked | Any | Lock → set state BLOCKED + diagnosis → unlock |
| Rescope task | Orchestrator | Lock → set original SUPERSEDED → create new task(s) with reference → unlock |
| Finalize draft | Orchestrator | Lock → change DRAFT to READY → unlock |
| Log activity | Any | Append to log.yaml (no lock needed, append-only) |

---

## Clean Sync Invariant

Before setting READY_FOR_REVIEW, coder must ensure working tree is clean:

```bash
[ -z "$(git -C $WORKTREE status --porcelain)" ] || abort "Uncommitted changes"
liza submit-for-review "$TASK_ID" HEAD --agent-id "$AGENT_ID"
```

Blackboard records `review_commit` as the resolved worktree HEAD. Code Reviewer verifies this SHA before reviewing.

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
| `hypothesis_exhaustion` | Orchestrator | Two coders failed same task, rescope required |
| `spec_gap` | Orchestrator | Missing spec discovered during planning/rescope |
| `review_budget_exhausted` | Orchestrator | Coder-Code Reviewer reached max cycles without approval |
| `review_exhaustion` | Orchestrator | Two reviewers failed to issue verdict on same task |
| `reviewer_loop` | Code Reviewer | Reviewer stuck in command loop, self-aborted |
| `stale_verdict` | CLI | Reviewer attempted verdict after task already left review |
| `system_ambiguity` | Any role | Liza protocol or role definition unclear, escalated to Orchestrator |
| `provider_audit_degraded` | Supervisor | Provider ran but transcript/rollout persistence is suspect |
| `agent_degraded` | Supervisor / CLI | Agent epoch cannot provide effective role capacity |

**Required Details Fields (validated by `liza validate`):**

| Type | Required Fields | Purpose |
|------|-----------------|---------|
| `retry_loop` | `count`, `error_pattern` | Pattern detection via `similar(error_pattern)` |
| `trade_off` | `what`, `why`, `debt_created` | Debt accumulation counting |
| `external_blocker` | `blocker_service` | Aggregation by service for circuit breaker |
| `assumption_violated` | `assumption`, `reality` | Assumption cascade detection |
| `reviewer_loop` | `count`, `command_pattern` | Reviewer self-abort on repetitive commands |
| `review_exhaustion` | `reviewers_failed`, `common_blocker` | Two reviewers failed to complete review |
| `stale_verdict` | `attempted_verdict`, `current_status` | Preserve reviewer findings lost to review-transition race |
| `system_ambiguity` | `protocol_section`, `question` | Track Liza system gaps for human clarification |
| `provider_audit_degraded` | `provider`, `agent_id`, `message` | Aggregate provider audit degradation across agents |
| `agent_degraded` | `agent_id`, `role`, `reason`, `last_error` | Preserve claim-capacity degradation evidence |

Anomalies with malformed details will fail validation. This ensures circuit breaker pattern detection has reliable data.
The agent should be very specific about the faced issue so this may be reproduced and investigated.

State is not a transcript store. Raw provider events, command output, and full
`item.completed` payloads belong under `.liza/agent-outputs/`; `state.yaml`
keeps bounded orchestration facts only. Human-readable state text fields such
as anomaly `details.message`, handoff summaries, notes, excerpts, and command
summaries are capped at 4096 bytes, and transcript-shaped payloads are rejected
regardless of size. If an anomaly needs raw evidence recovery, store a bounded
summary plus a structured reference:

```yaml
type: provider_audit_degraded
details:
  provider: codex
  agent_id: orchestrator-1
  impact: provider transcript or rollout persistence may be incomplete
  message: provider audit degraded; inspect .liza/agent-outputs and alerts for transcript evidence
  log_ref:
    output_file: .liza/agent-outputs/orchestrator-1-20260515-101530.txt
    event_id: item_abc123        # optional when available
    byte_offset: 18422           # optional when available
    hash: sha256:...             # optional when available
```

Legacy states containing raw transcript payloads in anomaly messages should be
repaired with `liza migrate`, which preserves the anomaly and routing details
while replacing the raw message with a bounded summary and scrub metadata.

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
  - "IMPLEMENTING task must have assigned_to"
  - "IMPLEMENTING task must have worktree"
  - "IMPLEMENTING task worktree path must exist (catches partial claim failures)"
  - "IMPLEMENTING task must have valid lease_expires"
  - "IMPLEMENTING task must have base_commit (except integration_fix tasks which reuse existing worktree)"
  - "READY_FOR_REVIEW task must have review_commit"
  - "REVIEWING task must have reviewing_by"
  - "REVIEWING task must have review_lease_expires"
  - "REVIEWING task must have review_commit"
  - "REJECTED task must have rejection_reason"
  - "BLOCKED task must have blocked_reason and blocked_questions"
  - "BLOCKED task repair_request, when present, must have operation, target, command, evidence, and validation"
  - "SUPERSEDED task must have rescope_reason (superseded_by is optional)"
  - "MERGED task must not have worktree"
  - "Task type must be a known type (currently: 'coding', 'planning'); empty defaults to 'coding'"
  - "depends_on must reference existing task IDs"
  - "depends_on must not reference a task whose role_pair is downstream of the dependent task's role_pair"
  - "depends_on must not create circular dependencies"
  - "Non-terminal tasks must not depend on terminal non-MERGED tasks"
  - "IMPLEMENTING task must have all depends_on tasks directly MERGED"
  - "Agent WORKING must have task"
  - "Agent WORKING should have lease_expires in future (warning if expired beyond grace period of 60s — may indicate long-running operation)"
  - "No two agents assigned to same task"
  - "Task with integration_fix:true must have prior INTEGRATION_FAILED in history"
  - "Task failed_by list must contain unique agent IDs"
  - "Task parent_task/parent_tasks must reference existing task IDs"
  - "Task output entries must have all required fields (desc, done_when, scope, spec_ref)"
  - "Artifact reference fields are scalar repo-relative refs with optional #fragment anchors; semicolon-joined multi-refs are rejected"
  - "Artifact refs fail closed when fragment stripping leaves an empty path, the path traverses outside the repository, or an absolute ref cannot be safely normalized to repo-relative"
  - "Protected artifact fields are goal spec_ref; task spec_ref, epic_ref, plan_ref, arch_ref; and output entry spec_ref, epic_ref, plan_ref, arch_ref"
  - "Candidate-tree artifact validation strips fragments and rejects missing paths, directories, submodules/gitlinks, symlinks, and non-regular object modes; valid paths resolve to Git file modes 100644 or 100755"
  - "Artifact-ref diagnostics include deterministic invalid path and owner provenance: field name, task ID when applicable, and output index when applicable"
  - "Task arch_ref must not contain worktree prefix (.worktrees/) — must be repo-relative"
  - "Task arch_ref must reference an existing file (checked via checkSpecFileExists against project root then integration branch)"
  - "Task output entry arch_ref must not contain worktree prefix (.worktrees/) — must be repo-relative"
  # Note: output entry arch_ref does NOT have file-existence validation (entries are set before merge)
  - "Anomaly type must be one of: retry_loop, trade_off, spec_ambiguity, external_blocker, assumption_violated, scope_deviation, workaround, debt_created, spec_changed, hypothesis_exhaustion, spec_gap, review_budget_exhausted, review_exhaustion, reviewer_loop, stale_verdict, system_ambiguity, provider_audit_degraded, agent_degraded"
  # Transition invariants (runtime-enforced, not statically validated)
  # These are enforced by agent behavior and atomic operations during state transitions.
  # `liza validate` validates static state invariants; these require history analysis.
  - "IMPLEMENTING task from REJECTED must have new lease_expires (not stale from prior claim)"
  - "READY task must preserve failed_by if previously BLOCKED"
  - "IMPLEMENTING task with integration_fix:true must have lease_expires set"
```

**Enforcement Note:** Static invariants (above the "Transition invariants" comment) are validated by `liza validate`. Transition invariants are runtime constraints enforced by agents performing atomic operations during state transitions — they cannot be verified post-hoc without history event analysis.

## Related Documents

- [State Machines](state-machines.md) — state transitions
- [Task Lifecycle](../protocols/task-lifecycle.md) — operational flow
- [Tooling](../implementation/tooling.md) — CLI commands for blackboard operations
