# Blackboard Schema

## Location

`.liza/` in project root.

## Files

| File | Purpose | Write Pattern |
|------|---------|---------------|
| `state.yaml` | Current state | Atomic read-modify-write |
| `log.yaml` | Activity history | Append-only |
| `lifecycle-metrics/` | Fixed operation/outcome counters per sprint, with observation-window metadata; a non-empty subset of the known counts matrix is available with missing cells at zero, and the next recording materializes the full matrix; missing files are unavailable, while existing empty, truncated, malformed, bad-identity or unknown-key files remain unavailable and are never overwritten | Locked atomic replacement per sprint |
| `usage/` | Durable provider-turn usage outside `state.yaml`; day-rolled `records-YYYY-MM-DD.jsonl` and numbered size-rotation parts; a missing directory means unavailable, not zero | Append-only JSONL under a leaf day lock; no retention pruning |
| `alerts.log` | Persistent watcher alerts | Append-only |
| `archive/` | `sprint-N.yaml` sprint records written at sprint advance; `objects/<sha[0:2]>/<sha>.json` immutable task-field objects (see [Archived Task Fields](#archived-task-fields)) | Write-once; never rewritten or pruned |
| `circuit_breaker_report.md` | Latest qualifying circuit-breaker response report | Rewritten by `analyze` for each qualifying response |
| `ESCALATION` | Stale checkpoint notification | Overwrite by watcher |

### Sections within state.yaml

| Section | Purpose | Write Pattern |
|---------|---------|---------------|
| `anomalies` | Execution observations | Append by Coders/Code Reviewers; Supervisor deduplicates reviewer-claim quarantine records and updates retry evidence in place |
| `quarantined_verdicts` | Bounded substantive verdicts from fenced registrations | Evidence-only append/deduplication; authorized append-only reconciliation |
| `proof_reaffirmations` | Authorized approved-proof content transitions | Orchestrator-only append; identical transitions are idempotent |
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

## Provider usage records

The runtime directory's `usage/` is durable telemetry, separate from task state
and sprint counters. Records survive sprint rollover. The supervisor writes one
record per provider turn, outside state locks; the report derives outcomes from
task history at read time. Missing storage is unavailable, never a zero total.

| JSON fields | Meaning |
|-------------|---------|
| `schema_version`, `record_id` | Version 1; SHA-256 identity over agent, supervisor run, session, provider and start time |
| `task_id`, `role`, `agent_id` | Task and runtime role/agent attribution; an empty task is unattributed |
| `supervisor_run_id`, `session_id`, `provider` | Random 128-bit hex identity per supervisor process, provider session (with caller fallback), configured provider name |
| `started_at`, `ended_at` | UTC provider-turn interval |
| `fresh_input_tokens`, `cache_read_tokens`, `cache_write_tokens`, `output_tokens` | Provider-reported token counts |
| `provenance` | `terminal_authoritative`, `partial`, `unknown`, or `conflicting`; conflicts are detected when duplicate record identities disagree at load time |
| `exit_code`, `warm_session` | Turn exit code; `warm_session` is reserved and capture currently leaves it false |

Registration generations and their digests are deliberately absent;
`supervisor_run_id` is independent of registration and confers no authority.
Only authoritative records enter token totals; unavailable, partial, unknown and
conflicting observations remain explicit. See [Usage Attribution](../protocols/usage-attribution.md).

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
    validation:
      - make test
    validation_prerequisites:  # optional; exact command association, names only
      - command: make test
        executables: [make]
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
        Closure condition: create_order() cannot retry without a validated idempotency key
        Possible approach: Validate the key before retry. See spec section 3.2.

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

### Assessment and replacement history

History metadata is `TaskHistoryEntry.Extra`, inlined into the YAML entry.

| Event | Inline fields and retention |
|-------|-----------------------------|
| `orchestrator_assessment` | `assessment_fingerprint_v2`: one 64-character lowercase hex SHA-256 digest; `awaited_tasks`: sorted task IDs the hold waits for (all of them), present when declared with `--awaits` or carried forward from the previous assessment of the same blocked episode. Only the latest assessment retains them; a new assessment removes both, plus the retired `assessment_fingerprint_v1` and `dependency_descendant_wake_snapshot_v1`, from earlier assessments, preserving their notes and other audit fields. Equivalent input appends nothing. |
| `replacement_committed` (source task) | `source_task_id`, `replacement_task_id`, `source_prior_transition_id`, `source_new_transition_id`, `retargeted_consumers`, `request_id`, and `preserved_base_commit` when declared. Identifiers only, never the replacement payload. |
| `acceptance_commits_remapped` (merged parent task) | `replaced` (old-to-new commit identifiers), `integration` (integration commit identifier), and `history_entries` (updated history-entry count). Identifiers and count only, never commit content. |

Replacement replay uses the source's existing lifecycle receipts and
`superseded_by`; it adds no task/state field. See [Blocked-Assessment Idempotency](../protocols/blocked-assessment-idempotency.md)
and [Replacement Transactions](../protocols/replacement-transactions.md).

### Rejection RCA record and history

Optional task field `rejection_rca` holds a `RejectionRCARecord` for the current
high-churn cycle. The verdict gate seeds it while entering `BLOCKED` with
`blocked_reason` prefixed by `rejection_rca_required`. Authority is
`RejectionRCAGateOpen()` (record present and disposition absent), not prose.

| Record fields | Provenance |
|---------------|------------|
| `schema_version`, `threshold`, `rejection_count`, `gated_at`, `gating_commit` | Gate-seeded; callers cannot override stored provenance. RCA requests carry schema version 1, summary and contributions only. |
| `summary`, `contributions[]` | Normalized caller content; each contribution has `rejection_index`, `categories[]`, `evidence[]` |
| `fingerprint`, `recorded_at`, `recorded_by` | Derived when RCA is recorded; empty before recording |
| `disposition` | Caller `recovery_path` and `rationale`, plus derived `restore_mode`, `actor`, `lifecycle_version`, `decided_at`, `iteration_exempt`; absent until resume |

Cause vocabulary is `product_defect`, `capability_failure`, `lifecycle_retry`,
`unknown`. Unrecognized causes remain verbatim and telemetry groups them under
unknown. Recovery paths are a closed enum:

| Recovery path | Restore mode |
|---------------|--------------|
| `implementation_correction`, `human_override` | `claimable`: either unblock form |
| `capability_reroute`, `lifecycle_repair` | `assign`: requires `--assign-to`; resets current review cycles and records `iteration_exempt` |
| `rescope` | `none`: unblock refused; route to supersession |

Resume closes the gate but leaves the task `BLOCKED`. Only `unblock-task` restores
it, subject to dependency/worktree/rebase checks and the mode above. Re-gating
requires another threshold of durable rejections; it replaces the live record,
while earlier cycles remain in history. Inline event fields are:

| Event | Detail keys |
|-------|-------------|
| Gate-appended `blocked` | `blocked_class` = `rejection_rca_required`, `threshold`, `rejection_count`, `gated_at`, `first_rejection_at` (earliest durable rejection) |
| `rejection_rca_recorded` | `fingerprint`, `threshold`, `rejection_count`, `gated_at`, `causes`, `contribution_count`, `recorded_by` |
| `rejection_rca_resumed` | `fingerprint`, `recovery_path`, `restore_mode`, `actor`, `lifecycle_version`, `decided_at`, `iteration_exempt`, `gated_at` |

Event times use RFC3339; `causes` is sorted and distinct, retaining unknown
values. See [Rejection RCA Gate](../protocols/task-lifecycle.md#rejection-rca-gate).

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
      validation:
        - make test
      destructive_db: false
      rca_required: false
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
      validation:
        - env LIZA_ALLOW_DESTRUCTIVE_DB=1 make test
      destructive_db: true
  parent_task: null                # Deprecated: use parent_tasks. Set on child tasks, references parent task ID
  parent_tasks: []                 # Multi-parent linkage (many-to-one transitions). Back-references from child to parent tasks
  transitions_executed:            # Tracks which transitions have been applied
    code-plan-to-coding: true
  plan_check:                      # Orchestrator hand-off disposition of a merged plan (ADR-0159)
    verdict: held                  # passed | held
    ask: "Provision smoke credentials in the agent env files"  # held only: the human action awaited
    by: orchestrator-1
    at: 2026-09-24T09:30:00Z
```

Pipeline topology itself is frozen in `.liza/pipeline.yaml` at `liza init`. Role-pair schema supports `role-pairs.<name>.decomposition-root: true` for master planning pairs, with required `decomposition-output-ref` (`spec_ref`, `epic_ref`, `plan_ref`, or `arch_ref`) naming the framework ref each master output must provide. That marker is read-only runtime metadata: it selects master prompt sections, output validation, and INITIAL_PLANNING's specialized-to-master mapping. Existing frozen workspaces are not rewritten when the embedded topology changes; known legacy master role-pairs missing `decomposition-output-ref` are backfilled in memory at load time, while new role-pairs or transitions require manually updating `.liza/pipeline.yaml` or starting a fresh workspace.

| Field | Type | Set By | Purpose |
|-------|------|--------|---------|
| `output` | `[]OutputEntry` | Doer agent | Structured subtask definitions for next role pair |
| `arch_ref` | `string` | `liza proceed` | Path to architecture document (repo-relative). Set on child tasks during transition: first hop copies from parent's `output[]` entry, second hop inherits from parent task field. Validated via `checkSpecFileExists` (same pattern as `plan_ref`). |
| `rca_required` | `bool` | Orchestrator (`add-tasks`) / `liza proceed` | Specialized objective requires defect diagnosis. Defaults to false. For per-subtask children, an explicit `output[].rca_required` overrides this parent default; omission inherits it. One-to-one children inherit it, many-to-one children OR parent values, and `liza replan` preserves it. When true, the specialized code-planner produces a `## Root Cause Analysis` section and the code-plan-reviewer gates on it. Not the same strength as the `adversarial-pairing` field, which gates a separate analysis phase. |
| `parent_task` | `*string` | `liza proceed` / orchestrator | Back-reference from child to parent task (deprecated: use `parent_tasks`) |
| `parent_tasks` | `[]string` | `liza proceed` / orchestrator | Multi-parent back-references (used by many-to-one transitions; supersedes `parent_task`) |
| `transitions_executed` | `map[string]bool` | `liza proceed` / orchestrator | Idempotency — prevents duplicate transitions. For `many-to-one` transitions, set on **all** cohort members (not just the trigger task) to prevent re-firing from any member |
| `plan_check` | `*PlanCheck` | Orchestrator (`plan-check --pass/--hold`) / operator (`plan-check --clear`) | Disposition of a merged planning task whose manual `per-subtask`/`one-to-one` hand-off has not run. `passed` admits automatic expansion; `held` (with `ask`) blocks every expansion path until an operator clears it. Only on MERGED planning-pair tasks. See [ADR-0159](ADR/0159-orchestrator-plan-handoff-disposition.md) |

`set-task-output --json` returns a write receipt with `task_id`, `output_count`,
and `state_path`. The same transaction appends a `task_output_set` history event
with the agent, timestamp, `previous_output_count`, and `output_count`, including
repeated writes and explicit empty output. A receipt confirms the completed
write; it does not prevent a later lifecycle reset from clearing output. Compare
the receipt with subsequent task history when investigating missing entries.
Failures include operation, phase, task, state path, attempted output count, and
recovery context. Filesystem failures also expose the OS cause without dumping
arbitrary error payloads.

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
- `validation` (`[]string`): Ordered canonical validation commands for the generated child task. Commands are stored exactly as declared, must be single-line, non-empty, and must not have leading or trailing whitespace. Newly declared validation commands must be single-purpose and agent-executable. Forbidden validation command shapes: `cd ... &&`, command substitution/backticks, polling or tail pipelines, and task artifact paths outside the worktree. Existing stored commands that violate this shape remain visible as task-declared executable guidance and are translated by consumers rather than edited in place. This field is distinct from `repair_request.validation`, which records validation for an orchestrator repair request.
- `destructive_db` (`bool`): Optional safety marker for validation commands that may reset, drop, or otherwise destroy DB state. Defaults to false and is inert when omitted. When true, `validation` must be non-empty and every command must start with `LIZA_ALLOW_DESTRUCTIVE_DB=1 ` or `env LIZA_ALLOW_DESTRUCTIVE_DB=1 `. The marker is part of the canonical command and must not be translated away.
- `rca_required` (`*bool`): Per-child RCA classification. Explicit true or false overrides the parent task default during per-subtask construction; omission inherits the parent value. It is mandatory on every output from a decomposition root when any configured output consumer's doer role is `code-planner`.
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

`interfaces_owned` and `interfaces_consumed` are free-form planning annotations,
not authoritative machine relationships. Structural dependency validation checks
task IDs, pipeline direction, terminal-state legality, and cycles, but it does
not infer provider/consumer direction from interface names. Agents must verify
that direction from the decomposition and plan: a consumer may depend on its
provider, while an inverse edge requires another explicit relationship.

Generated child tasks for per-subtask transitions also persist task-level `validation` and `destructive_db` copied from the source `output[]` entry. Synthesized one-to-one and many-to-one transition children do not inherit parent task validation or `destructive_db` because those commands belong to the parent phase.

For decomposition-root outputs, `liza set-task-output` requires the configured `decomposition-output-ref` framework ref on every entry:

| Master role-pair | `decomposition-output-ref` | Child target |
|------------------|----------------------------|--------------|
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
remains in `history[]` entries where those entries recorded it. Integration-fix
claims preserve `output[]` alongside the reused worktree while clearing stale
review/failure metadata; the doer updates output if repair changes the deliverables.

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

**`rca_required` Propagation:**

`rca_required` resolves at child construction. Per-subtask entries can override the
parent default explicitly; omission retains backward-compatible inheritance:

| Constructor | Cardinality | Semantics |
|---|---|---|
| `buildChildTask` | per-subtask | explicit `output[].rca_required` wins; omitted value inherits the parent task's flag |
| `buildOneToOneChild` | one-to-one | inherits the parent task's flag |
| `buildManyToOneChild` | many-to-one | true if **any** cohort member carries it — a fan-in mixing defect and feature parents still consolidates work whose root cause must be established downstream |

`replan.go` copies it onto replacement tasks. The many-to-one path is load-bearing:
`us-to-coding` is many-to-one, so every `general-objective` goal traverses it before
reaching code planning.

For decomposition roots, the mandatory classification rule is derived from pipeline
topology rather than role-pair names: if any output consumer resolves to doer role
`code-planner`, every output must carry non-null `rca_required`. The same validation
runs when output is persisted, during the normal per-subtask transition, and during
crash recovery. This lets mixed feature/defect siblings receive different values while
preventing a task-wide default from leaking across structural boundaries.

One deliberate exclusion remains. The integration-analysis task built in
`integration_reconcile.go` does not carry the flag: those tasks route to
`integration-pair` and `coding-pair`, neither of which renders any RCA text, so the
flag would be inert.

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

### Lifecycle Receipt Metadata

The optional task `lifecycle` field has three members:

| Member | Shape and constraint |
|--------|----------------------|
| `revision` | Monotonic unsigned counter advanced by effective covered mutations; absent metadata starts at zero |
| `receipts` | Completed identities/projections, latest four per operation and at most sixteen per task |
| `preparation` | At most one unresolved request/boundary reservation, separate from receipt pruning |

Each receipt contains `operation`, `actor`, optional `request_id`,
`expected_transition`, `payload_digest`, completion `sequence`,
`transition_id` and a compact `projection`. Generation digests used for
internal authority matching are persistence-only and must be redacted from
inspection and presentation. Preparation retains the request identity and
live `boundary`; it does not assert completion.

Receipt/preparation projections are limited to 1 KiB serialized. No arbitrary
notes, output reports or edge lists are copied into them. Validate known
operations, count/size limits and unique monotonic versions. Replay never
appends metadata or repeats a domain event. Retention is the intersection of
both count bounds, with no guaranteed per-operation slots or time-based expiry.
Keep completed receipts at terminal transitions; expired requests requery/stop
instead of silently mutating again.

Authorized ownership/attempt-ending transactions and current-generation
turnover retire obsolete preparations while advancing revision. Denied/no-op
calls, passive heartbeat renewal and receipt pruning cannot retire a live
preparation. Retirement creates no completion receipt and promises no rollback.

The existing single `submitted_for_review` history event additionally records
original full input SHA and attempt boundary. It can detect expired legacy SHA
requests without claiming exact replay or adding an unbounded receipt list.
Old state/history without this metadata remains valid.

Task inspection and lifecycle results expose a current `transition_id`,
separate from a receipt's original completion identity. The token detects
ownership/history changes, including release/reacquire ABA, without changing
on passive lease renewal. See [Lifecycle Results](../protocols/lifecycle-results.md)
for request-pair identity, replay and preparation semantics.

Sprint outcome counters live outside task state and domain history. Their
optional metrics projection reports availability and `observed_since`.
Missing/corrupt data is unavailable, not zero; a first write starts a new
observation window. Counter snapshots are scoped to sprint identity and cannot
claim lossless coverage across process death or manual deletion.

### Archived Task Fields

A terminal task's `acceptance_receipt` may leave live state to shrink every
state read, parse and write. It moves into an immutable archive object, and the
task keeps a reference:

```yaml
archived:
  - field: acceptance_receipt
    sha256: <lowercase SHA-256 of the object bytes>
    archived_at: 2026-09-24T10:00:00Z
```

- **Eligibility.** The task status is terminal (MERGED, ABANDONED, SUPERSEDED)
  and it carries a live receipt. Terminal statuses have no outgoing
  transitions, and every non-inspection reader of the receipt serves an active
  task. History is never archived: history counts feed transition IDs, wake
  detection and sprint metrics.
- **Object.** `archive/objects/<sha[0:2]>/<sha>.json` is the JSON envelope
  `{format_version: 1, task_id, field, value}` with the receipt verbatim. It is
  named by the SHA-256 of its exact bytes, installed without replacing an
  existing file, and never rewritten, so every state snapshot that references
  it stays restorable. The path is derived from the validated digest, never
  stored.
- **Durability.** Before the reference is published under the state lock, the
  object file and every directory from its prefix directory up to the runtime
  directory are fsynced. This barrier runs on every write, including reuse of
  an existing identical object, so a retry cannot skip a failed barrier. A
  failure leaves at most an unreferenced object that the retry reuses. Windows
  cannot fsync directories; there, crash ordering is best-effort, as for state
  publication.
- **Validation.** At most one reference per field; only on terminal tasks;
  never alongside the live value it replaces; the task keeps
  `acceptance_source`. Validation checks shape only and opens no files.
- **Triggers.** Each newly completed `wt-merge` runs one bounded archive
  transaction afterwards, under the merge caller's authority (the generation
  fence applies) and never on replay; a failure is a merge warning. Operators
  drain a backlog with the configured executable's
  `archive-acceptance-receipts` subcommand. Agent sessions are
  refused. A transaction takes at most 8 tasks and a soft 4 MiB of objects:
  the first object always progresses. With nothing eligible it reads a snapshot
  and takes no lock.
- **Restoration.** `get tasks <id>` and task lists in JSON/YAML, structured
  results, `task.<id>.acceptance_receipt` and `--field acceptance_receipt`
  restore the receipt after verifying digest, task, field and format; the
  `archived` reference stays visible. A missing or corrupt object is an
  explicit error naming task, digest and path, never an absent receipt. Table
  and value output, summaries, other fields and computed queries do no archive
  I/O. Other readers (status, TUI, watch, usage, prompts) show compact state.

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
    Closure condition: [observable state required for approval]
    Possible approach: [advisory — the coder chooses the implementation]

  Concerns: [count]
  - [concern] file:line — Issue description
    Closure condition: [observable state required for approval]

  Overreach: [count]
  - [overreach] file:line — Beyond task scope, or a larger fix than the finding required
    Smaller resolution: [what to revert or split out]

  Overall: [1-2 sentence assessment]

  Prior Feedback Status:  # Required for iteration 2+
  - RESOLVED: [prior issues now fixed]
  - ACCEPTED: [rationale accepted, no code change]
  - PARTIAL: [prior issues partially addressed]
  - STILL PRESENT: [prior issues not addressed]
```

**Requirements:**
- Blockers and Concerns must reference specific `file:line` locations
- Each blocker and concern must include a closure condition; any suggested approach is advisory
- For iteration 2+: Prior Feedback Status section is mandatory
- The complete `rejection_reason` must not exceed 4096 bytes. Store longer raw evidence in the project runtime's `agent-outputs/` directory and include a bounded summary plus an artifact reference.

**Rationale:** Structured format enables:
- Coder to satisfy explicit closure conditions rather than interpreting prose
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
- Active tasks must not depend on terminal non-MERGED tasks. When a task is superseded, active downstream `depends_on` entries are rewritten to its replacements and the retiring task's own illegal downstream dependencies are pruned in the same transaction; legal historical edges remain. When a task is cancelled, active downstream `depends_on` entries pointing at it are removed.
- Explicit `output[].task_depends_on` writes reject terminal non-MERGED task IDs. Operational output and generated child `depends_on` follow the canonical dependency rule before they can mint or patch child tasks: superseded entries are rewritten to legal replacements, cancelled or unreplaced retired entries are removed, downstream replacements that are already MERGED are treated as satisfied and omitted from child dependencies, and illegal pending replacements fail the affected mutation or transition instead of being silently dropped. `SUPERSEDED` and `ABANDONED` task output remains audit history unless crash recovery can still consume it.
- Empty array or missing field means no dependencies — task is immediately claimable
- Coders can only claim tasks where ALL dependencies are satisfied
- Orchestrator sets dependencies during task creation based on logical ordering

Dependency direction remains valid for terminal tasks. To recover legacy
corruption on one `SUPERSEDED` task, the orchestrator runs
`liza repair-superseded-dependencies <task-id> --reason <reason>`. The operation
removes every illegal downstream direct edge in one locked transaction, retains
legal edges and terminal/replacement metadata, records removed and retained IDs
with the reason and caller in `dependencies_rewritten` history, and validates
the full candidate state before commit. Failed repairs do not mutate state;
direct edits to `.liza/state.yaml` are prohibited.

Active dependency repair has two separate paths. `retarget-dependency` changes
one direct edge on a non-terminal task. A repair that changes multiple tasks or
complete dependency lists is stored on its blocked source task as a declarative
request:

```yaml
repair_request:
  operation: apply-dependency-repair
  target: blocked-task
  dependency_updates:
    - task_id: consumer-a
      expected_depends_on: [old-a]
      desired_depends_on: [replacement-a, replacement-b]
    - task_id: consumer-b
      expected_depends_on: []
      desired_depends_on: [replacement-a]
  evidence:
    - "command=retarget-dependency exit_code=1 stderr=repair requires multiple atomic updates"
  validation:
    - "project-specific validation command"
```

The request is command-free: `command` must be absent, update `task_id` values
must be unique, and both dependency lists must be explicit, unique, non-empty
task IDs even when the list itself is `[]`. The owning task is the request
`target`. Agents provide the complete JSON object to
`mark-blocked --repair-request-file <path>`; file input is mutually exclusive
with the individual `--repair-*` flags used by command-based non-dependency
repairs.

The orchestrator runs
`apply-dependency-repair <blocked-task-id> --reason <reason>`. One locked
mutation checks the source request and every expected list, canonicalizes all
desired lists, validates the complete candidate state, appends per-task audit
history, and clears the request only on success. Any stale or invalid update
leaves all dependencies, history, and request data unchanged. The source task
remains `BLOCKED` until its declared validation succeeds and it is explicitly
unblocked.

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

validation_readiness:  # latest safe audit observation per registration/task
  coder-1:
    task-2:
      generation: registration-generation
      task_id: task-2
      commit: task-worktree-sha
      review_commit: submitted-review-sha  # optional metadata, distinct from actual HEAD
      integration_sha: integration-sha
      digest: command-and-prerequisite-digest
      fingerprint: opaque-process-local-hmac
      checked_at: 2025-01-17T14:52:00Z
      method: direct
      result: failed
      code: environment_missing
      command_index: 0  # diagnostic only, not command identity
      check_index: 0
      variable: DATABASE_URL  # never the value

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
    # Absent until an orchestrator turn renders (HUMAN_NOTE) or consumes
    # (assessment of the target) the note; delete/recover audit entries set
    # it at creation. Unmarked notes wake an idle orchestrator.
    orchestrator_seen_at: 2025-01-17T15:30:00Z

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
    transitions_attempted_at: null  # Start of the last planning-transition pass after a resumed transition checkpoint; bounds the blocked-wake planning preference
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

# Outstanding obligation to write the checkpoint steering report. Recorded by
# the checkpoint producers in the same transaction that creates the checkpoint,
# and cleared by the orchestrator when it takes the obligation. Absent when no
# report is owed. It lives here rather than under sprint because a sprint
# rollover replaces sprint wholesale, timeline included.
pending_checkpoint_summary:
  at: 2025-01-18T17:30:00Z
  trigger: SPRINT_COMPLETE  # empty for a circuit-breaker checkpoint

circuit_breaker:
  last_check: 2025-01-18T17:30:00Z
  status: OK  # OK, TRIGGERED
  current_trigger: null  # HALT-only, retained for backward compatibility
  current_response: null # active CHECKPOINT or HALT; WARNING never becomes active
  history:
    - timestamp: 2025-01-17T12:00:00Z
      pattern: null
      result: OK

config:
  max_coder_iterations: 10      # Default for all tasks
  max_review_cycles: 5          # Default for all tasks
  high_churn_rejection_threshold: 4  # Durable rejections per RCA cycle; non-positive uses default 4
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

**Circuit-breaker response and compatibility model:** `WARNING`, `CHECKPOINT`,
and `HALT` are typed responses. Only `HALT` is a trigger: it sets
`status: TRIGGERED`, populates `current_trigger`, and moves mode to
`CIRCUIT_BREAKER_TRIPPED`. `CHECKPOINT` leaves mode `RUNNING`, status `OK`, and
`current_trigger: null`; it stores this active non-trigger hard-checkpoint
response and moves the sprint to `CHECKPOINT`, gating downstream transition
creation while work already available to doer/reviewer agents may continue. `WARNING` is
observation-only and creates no `current_response`.

An active `current_response` contains `timestamp`, `pattern`, `severity`, typed
`response`, provider-evidence `classification`, `explanation`, and
`report_file`. History entries may carry `response`, `classification`, and
`explanation` alongside their existing fields. A history entry may also carry
`superseded_by_response: HALT`, an optional HALT-only replacement marker written
when an active provider-audit `CHECKPOINT` escalates. These additions are
optional for backward compatibility: readers accept legacy state without them,
and existing `result: TRIGGERED` entries remain generic acknowledgement
boundaries.

An active provider-audit response is monotonic until `resume`: `HALT` remains
active against every later analysis, while `CHECKPOINT` remains active for no
match or a non-HALT candidate. A committed `HALT` candidate supersedes the
checkpoint atomically by marking the former boundary with
`superseded_by_response: HALT` and appending exactly one unresolved `HALT`
boundary matching the new `current_response`. The superseded checkpoint keeps
both `resolution` and `resolved_at` absent. Supersession is therefore not
operator acknowledgement and does not establish the provider-evidence
watermark.

Provider-audit classifications are `ACKNOWLEDGED_HISTORICAL` (qualifying
evidence entirely at or before a resolved boundary), `NEW` (later evidence
qualifies without same-provider acknowledged evidence), and `CONTINUING`
(same-provider evidence spans the boundary and qualifies in combination).
The `resume` command resolves the matching active `CHECKPOINT` or `HALT` history
entry, records the paired `resolution`/`resolved_at` acknowledgement fields, and
clears `current_response`; for `HALT` it also clears trigger state. A superseded
checkpoint is never resolved by that operation. The resolved winning response
timestamp remains the provider evidence boundary, so unchanged evidence returns
`ACKNOWLEDGED_HISTORICAL`/`WARNING` rather than checkpointing or halting again.

Provider-audit `NEW` or `CONTINUING` evidence can become `HALT` only when at
least one current registration exactly matches the anomaly provider and every
exact match has degraded health for the same agent ID, provider, PID, and
registration time. Alias-only provider equality, empty or missing identity,
missing health, or a stale/mismatched PID or registration epoch is unknown and
therefore remains `CHECKPOINT`.

The operation result exposes `AnalyzeResult.Response`,
`AnalyzeResult.Classification`, and `AnalyzeResult.Explanation`; JSON projects
them as `response`, `classification`, and `explanation`, in addition to the
legacy `pattern`, `severity`, `evidence`, `triggered`, and `report_path` fields.
`triggered` is true only for `HALT`.

For generic patterns, when `status == OK` and `current_trigger == null`, the
latest history entry with `result: TRIGGERED` remains the acknowledgement
watermark. Later `OK` entries do not move it. If `status == TRIGGERED` or
`current_trigger` is non-null, no generic watermark applies.

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

## Proof Reaffirmations

`proof_reaffirmations` is an optional top-level sequence recorded by the
orchestrator-only `reaffirm-proof` command using current inherited agent authority.
Acceptance still refuses approved-proof reference drift without a matching record.

| Field | Contract |
|-------|----------|
| `parent_task`, `carrier_path`, `heading`, `reference_id`, `reviewed_section`, `current_section` | Identity tuple binding the allocating planning parent, carrier section, proof reference, and exact reviewed-to-current content transition |
| `actor` | Registered orchestrator that authorized the transition |
| `timestamp` | UTC time of the decision |
| `reason` | Nonblank UTF-8 justification, at most 4096 bytes |

Both section identities are derived from the reviewed carrier and integration,
not caller-supplied. `--expected-section` is only a precondition naming the
inspected current identity; a mismatch records nothing. The record authorizes
exactly one content transition, not a standing waiver: a later change refuses
again. It grants no task status change or approval. Repeating the same identity
tuple appends nothing and preserves the original provenance.

## Quarantined Verdict Evidence

`quarantined_verdicts` is an optional top-level sequence; existing state without
it remains valid. Each record contains:

| Field | Contract |
|-------|----------|
| `id` | Deterministic `qv-` plus 64 lowercase hex SHA-256 identity |
| `task_id`, `reviewer_id` | Existing task and submitting reviewer identity |
| `review_commit` | Immutable full 40- or 64-hex SHA, canonicalized lowercase |
| `verdict`, `reason` | APPROVED or REJECTED, sanitized reason at most 4096 UTF-8 bytes; rejection requires nonblank content |
| `timestamp` | First capture time in UTC, unchanged by retries |
| `generation_fingerprints` | Deduplicated SHA-256 fingerprints, never reusable generations |
| `matched` | Fixed at capture: supplied boundary was known in the task's live/history review boundaries |
| `reconciliations` | Append-only entries with `actor`, UTC `timestamp`, `disposition`, and nonblank bounded `reason` |

Judgment identity is task + reviewed commit + reviewer + verdict + canonicalized
sanitized reason. Generation is provenance, not identity: the same judgment
across registrations adds a fingerprint to the existing record. Identical
retries do not append a record or change its timestamp. Validation rejects
malformed records and duplicate identities.

`matched: false` records remain non-gating even if a later task happens to use
their SHA. Matched evidence applies along explicit chronological
`review_commit_updated` lineage without rewriting its original commit.
Unresolved conflicting evidence gates approval and merge. Reconciliation
supports `accepted`, `refuted`, `superseded`, and `escalated`; see
[task lifecycle](../protocols/task-lifecycle.md#quarantined-verdicts).
It cannot mutate task status or quorum. Evidence retention is deliberate debt
with a concrete trigger in [TECH_DEBT.md](../../TECH_DEBT.md#quarantined-verdict-retention).

Explicit task deletion atomically removes its quarantined findings and their
reconciliation audit. Findings for remaining tasks are preserved; orphaned
findings are invalid state.

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
resubmission for a rejected/executing task or reclaiming a just-submitted task
with an unexpired review lease and a live observer. A live observer means the
reviewer agent row exists, has a usable
PID, `AgentProcessStatus(...).IsLiveOrUnknown()` is true, and `current_task`
matches the task ID. Active review ownership additionally requires agent status
`REVIEWING`; passive await-resubmission ownership requires agent status
`WAITING`. Provider CLI exit releases passive reviewer ownership even when the
supervisor PID remains alive; missing, unusable, dead, mismatched, or
non-observing reviewer process/agent evidence makes the claim stale.

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

Verdict submission, reconciliation, and merge also use a cross-process per-task
review lock. Public entries acquire it once; recursive clean-integration
submission stays inside that acquisition. Existing outer lifecycle locks precede
task review → integration completion → integration mutation → blackboard read.
Blackboard writes occur after releasing integration mutation. Administrative
failure does not append a substantive finding.

All writes to `state.yaml` use `flock`:

```bash
flock -x .liza/state.yaml.lock -c 'operation'
```

Lock hold time must be minimal (read, modify, write, release).

Operator inspection (`status`, `get`, `get-tasks`) and observation-only reads
(TUI, `watch`, `validate`, `usage-report`, shell completion, launch `--cli`
validation, lifecycle metrics sprint capture) read one complete published state
through `Blackboard.ReadSnapshot`, without acquiring the state lock or consulting
an mtime cache. A read qualifies only when the state it returns is displayed,
observed, or used as launch-input validation, and any consequent state mutation or
authority decision re-reads under the lock or is guarded by locked registration.
The read itself is lock-free, but the command around it may still take locks. For
example, `watch` and TUI auto-repair take `RepairAgentPool`'s locked read, and
`validate --repair` repairs under lock. Writers publish closed temporary files by atomic
rename; inspection sees either publication, never a partially written state.
The file is closed before YAML decoding. Missing files and malformed YAML remain
errors. In-place external writes are outside this guarantee.

Snapshots may become stale immediately. Status derives runtime transition policy
from its captured state; process/filesystem diagnostics remain separate observations.
Snapshots do not authorize mutations: existing locked reads and locked
read-modify-write revalidation retain their contracts. Windows publication retains
its bounded retry for filesystem handles held during the byte read. State byte
reads also retry transient Windows sharing violations within the same bounded
budget, without acquiring the state lock; other read errors remain immediate.

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
| Quarantine fenced verdict | Fenced reviewer submission | Preserve immutable reviewed SHA and sanitized evidence only; never mutate task/agent lifecycle state |
| Reconcile verdict | Current-generation orchestrator with capability | Task review lock → transactionally validate authority and append justified disposition; no task or quorum mutation |
| Execute merge | Supervisor | After Code Reviewer sets APPROVED → supervisor runs `liza wt-merge` → update state to MERGED |
| Mark blocked | Any | Lock → set state BLOCKED + diagnosis → unlock |
| Rescope task | Orchestrator | Lock → prune the retiring task's illegal downstream edges + set original SUPERSEDED + rewrite active consumers/create replacements + validate candidate → unlock |
| Replace task | Orchestrator | Validate payload and declared preserved base → lock → validate source boundary, identity/ID collisions and dependency expectations → create replacement + retarget consumers + supersede source + append audit → validate full candidate and persist together → unlock; any candidate error persists nothing |
| Record / resume rejection RCA | Orchestrator | Validate through payload-schema registry before lock → lock → merge normalized RCA or record disposition + append event → unlock; equal-fingerprint RCA resubmission is `NO_CHANGE`, and resume leaves status BLOCKED |
| Repair superseded dependencies | Orchestrator | Lock → require SUPERSEDED + remove all illegal downstream direct edges + append audit history + validate full candidate → unlock; append activity log after commit |
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
| `submit_verdict_failed` | CLI | Submit-verdict failed after accepting a verdict attempt; best-effort when the blackboard remains writable |
| `reviewer_claim_circuit_open` | Supervisor | Repeated identical pre-claim reviewer failures against an unchanged task/state boundary crossed the threshold |

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
| `submit_verdict_failed` | `verdict`, `error` | Preserve failed verdict-write cause for operator diagnosis |
| `reviewer_claim_circuit_open` | `role`, `failure_class`, `attempts`, `first_failure`, `last_failure`, `recovery` | Bounded quarantine evidence, one durable record per failure key |

The Supervisor also retains `boundary_version`, bounded masked `error`, and
`cooldown_until` on `reviewer_claim_circuit_open`. Role, task, failure class and
boundary identify one record. Only `attempts`, `last_failure` and
`cooldown_until` advance in place after failed re-probes; boundary and error
remain immutable. Registration generations are not recorded. See
[ADR-0140](ADR/0140-reviewer-claim-circuit-breaker.md).

Anomalies with malformed details will fail validation. This ensures circuit breaker pattern detection has reliable data.
The agent should be very specific about the faced issue so this may be reproduced and investigated.
`submit_verdict_failed` is a best-effort diagnostic for post-operation failures; if anomaly persistence fails, the CLI preserves the original verdict error and records the secondary anomaly-recording failure in the activity log when possible.

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
  - "BLOCKED task repair_request, when present, must have operation, target, evidence, validation, and either `command` for command-based non-dependency requests or `dependency_updates` for `apply-dependency-repair`"
  - "SUPERSEDED task must have rescope_reason (superseded_by is optional)"
  - "MERGED task must not have worktree"
  - "Task type must be a known type (currently: 'coding', 'planning'); empty defaults to 'coding'"
  - "depends_on must reference existing task IDs"
  - "depends_on must not reference a task whose role_pair is downstream of the dependent task's role_pair, including for terminal tasks"
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
  - "Task validation and output entry validation commands must be single-line non-empty strings without leading or trailing whitespace"
  - "Task and output entry destructive_db:true requires non-empty validation, and every validation command must start with LIZA_ALLOW_DESTRUCTIVE_DB=1 or env LIZA_ALLOW_DESTRUCTIVE_DB=1"
  - "Artifact reference fields are scalar repo-relative refs with optional #fragment anchors; semicolon-joined multi-refs are rejected"
  - "Artifact refs fail closed when fragment stripping leaves an empty path, the path traverses outside the repository, or an absolute ref cannot be safely normalized to repo-relative"
  - "Protected artifact fields are goal spec_ref; task spec_ref, epic_ref, plan_ref, arch_ref; and output entry spec_ref, epic_ref, plan_ref, arch_ref"
  - "Candidate-tree artifact validation strips fragments and rejects missing paths, directories, submodules/gitlinks, symlinks, and non-regular object modes; valid paths resolve to Git file modes 100644 or 100755"
  - "Artifact-ref diagnostics include deterministic invalid path and owner provenance: field name, task ID when applicable, and output index when applicable"
  - "Task arch_ref must not contain worktree prefix (.worktrees/) — must be repo-relative"
  - "Task arch_ref must reference an existing file (checked via checkSpecFileExists against project root then integration branch)"
  - "Task output entry arch_ref must not contain worktree prefix (.worktrees/) — must be repo-relative"
  # Note: output entry arch_ref does NOT have file-existence validation (entries are set before merge)
  - "Anomaly type must be one of: retry_loop, trade_off, spec_ambiguity, external_blocker, assumption_violated, scope_deviation, workaround, debt_created, spec_changed, hypothesis_exhaustion, spec_gap, review_budget_exhausted, review_exhaustion, reviewer_loop, stale_verdict, system_ambiguity, provider_audit_degraded, agent_degraded, submit_verdict_failed"
  # Transition invariants (runtime-enforced, not statically validated)
  # These are enforced by agent behavior and atomic operations during state transitions.
  # `liza validate` validates static state invariants; these require history analysis.
  - "IMPLEMENTING task from REJECTED must have new lease_expires (not stale from prior claim)"
  - "READY task must preserve failed_by if previously BLOCKED"
  - "IMPLEMENTING task with integration_fix:true must have lease_expires set"
```

**Enforcement Note:** Static invariants (above the "Transition invariants" comment) are validated by `liza validate`. Transition invariants are runtime constraints enforced by agents performing atomic operations during state transitions — they cannot be verified post-hoc without history event analysis.

## Validation prerequisite evidence

Tasks and `output[]` accept optional `validation_prerequisites` entries with
`command`, `env`, `executables`, and argv-vector `probes`. When declared, every
unique canonical command requires one non-vacuous exact-text association; task
creation, output persistence, child generation and state validation enforce the
same contract. See [limits and execution semantics](../protocols/validation-prerequisites.md).

`validation_readiness` is task-specific audit evidence, separate from global
`agent_health`. Its command/prerequisite digest, registration generation and
actual worktree `commit` identify the observation. Optional `review_commit`
separately captures task review metadata; a changed review reference invalidates
that observation even when the worktree HEAD is unchanged. `passed` never authorizes another claim or
launch without fresh checks. HMAC fingerprints use a private per-process key
and cannot prove environment equality across processes or restarts. Records and
diagnostics exclude raw environment values, probe output and process errors.

## Related Documents

- [State Machines](state-machines.md) — state transitions
- [Task Lifecycle](../protocols/task-lifecycle.md) — operational flow
- [Lifecycle Results](../protocols/lifecycle-results.md) — request identity, receipt retention and safe actions
- [Blocked-Assessment Idempotency](../protocols/blocked-assessment-idempotency.md) — structural fingerprint and no-change contract
- [Payload Validation](../protocols/payload-validation.md) — shared structural preflight and mutation validation
- [Replacement Transactions](../protocols/replacement-transactions.md) — atomic replacement and source audit
- [Usage Attribution](../protocols/usage-attribution.md) — durable provider records and report-time outcomes
- [Tooling](../implementation/tooling.md) — CLI commands for blackboard operations
