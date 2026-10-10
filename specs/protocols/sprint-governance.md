# Sprint Governance

## Rationale

Sprints in Liza serve a different purpose than in Scrum. Agents don't need sustainable pace or team commitment rituals. Sprints exist for:

1. **Budget gates** — bound calendar time and compute cost
2. **Human checkpoints** — forced review points before drift compounds
3. **Spec evolution windows** — structured opportunities to update requirements
4. **Metrics collection** — data for calibrating future sprints

---

## Sprint Definition

```yaml
sprint:
  id: sprint-1
  goal_ref: goal-1
  scope:  # Note: 'scope' not 'tasks' — see blackboard-schema.md for canonical field names
    planned: [task-1, task-2, task-3, task-4, task-5]
    stretch: [task-6]
  timeline:
    started: 2025-01-17T09:00:00Z
    deadline: 2025-01-19T18:00:00Z
  status: IN_PROGRESS  # IN_PROGRESS, CHECKPOINT, COMPLETED, ABORTED
```

**Sprint ends when ANY of:**
- All planned tasks reach terminal state (MERGED, ABANDONED, SUPERSEDED)
- All non-terminal planned tasks BLOCKED (sprint stalled)
- Calendar deadline reached
- Circuit breaker triggered
- Human requests checkpoint

**Sprint Completion Semantic:**

"Planned tasks" = tasks listed in `sprint.scope.planned[]`. The planned list is updated in two ways:
- **At sprint creation:** initial task list is set
- **By pipeline transitions:** when the orchestrator executes `ExecuteAvailableTransitions` after a planning checkpoint is resumed, children are automatically added to `sprint.scope.planned[]`

Tasks created mid-sprint via orchestrator rescoping (e.g., task-3a and task-3b replacing SUPERSEDED task-3) are **not** automatically added to the planned list.

This means:
- Pipeline-created children (from planning → coding transitions) are tracked in sprint scope and prevent premature sprint completion
- Orchestrator-created replacement tasks are NOT in scope — a sprint can complete while they are still in progress
- Sprint metrics may show more `tasks_done` than originally planned (includes replacements)
- Sprint boundaries are for human planning cadence, not work completion guarantees

If precise work-completion tracking is needed, human should update `sprint.scope.planned[]` when rescoping, or wait for all active tasks (planned + unplanned) to finish before considering sprint complete.

---

## Sprint Scope Sizing

Sprint size is measured in **tasks, not tokens**. Token cost is observed post-hoc for future calibration.

| Project Phase | Recommended Sprint Size |
|---------------|------------------------|
| Bootstrap (first sprint) | 3-5 tasks |
| Steady state | 5-8 tasks |
| Complex/risky work | 3-5 tasks |

**Rationale:** Smaller sprints = more frequent checkpoints = faster course correction.

---

## Checkpoint Protocol

Checkpoints are **mandatory human review points** unless auto-resume is enabled (`config.auto_resume: true`). Hard checkpoints pause all agents until human release. Transition checkpoints (`PLANNING_COMPLETE`, `MANY_TO_ONE_READY`) gate downstream transition creation only: the orchestrator waits for human release, while doer/reviewer roles may continue already-available claimable/reviewable work in the current sprint.

### Checkpoint Triggers

| Trigger | Automatic? | `checkpoint_trigger` | Notes |
|---------|------------|---------------------|-------|
| Planning tasks merged with output | Yes | `PLANNING_COMPLETE` | Human reviews planning output before coding begins (skipped when auto-resume enabled) |
| Many-to-one cohort ready | Yes | `MANY_TO_ONE_READY` | Human reviews fan-in readiness before the consolidated child task is created |
| Sprint tasks complete | Yes | `SPRINT_COMPLETE` | Normal completion |
| Sprint deadline reached | Yes | _(empty)_ | Time box enforced |
| Circuit breaker fired | Yes | _(empty)_ | Systemic issue detected |
| Sprint stalled | Yes | _(empty)_ | All non-terminal planned tasks BLOCKED |
| `liza sprint-checkpoint` | Manual | _(empty)_ | Human-initiated review |

The `checkpoint_trigger` field records _why_ the checkpoint was created. It is set by `liza_sprint_checkpoint` (MCP) or `SprintCheckpoint` (ops) and used by the orchestrator to gate post-resume actions (see Planning Transition Gate below).

### Checkpoint Timeout Behavior

Checkpoints are not auto-cleared. If human does not respond:

| Duration | Watcher Action | Escalation |
|----------|----------------|------------|
| 30 min | `⚠️ CHECKPOINT STALE` | Log only |
| 2 hours | `🚨 CHECKPOINT STUCK` | Log anomaly |
| 8 hours | `🚨 CHECKPOINT ABANDONED?` | Log anomaly, suggest abort |

**External Notification (v1.1 — not implemented in v1):**

Webhook escalation is planned for v1.1. The `config.escalation_webhook` field in state.yaml is reserved but not yet functional.

When implemented, watcher will post to webhook at 2h and 8h thresholds:
```json
// Webhook Payload (POST, Content-Type: application/json)
{
  "event": "checkpoint_stuck",
  "duration_hours": 2,
  "timestamp": "2025-01-17T16:00:00Z",
  "sprint": "sprint-1",
  "checkpoint_since": "2025-01-17T14:00:00Z",
  "tasks_waiting": 3,
  "escalation_file": "CHECKPOINT_STUCK since 2025-01-17T14:00:00Z..."
}
```

**Design Principle (auto-resume OFF, default):**
- Hard checkpoints keep agents paused indefinitely — no automatic resume or abort
- Transition checkpoints hold downstream transition creation; doer/reviewer roles may continue existing claimable/reviewable work
- Escalation is notification only, not action
- Human must explicitly act (`liza resume` or `liza stop`)
- Unattended checkpoints are not errors; they're paused work awaiting decision

**Auto-Resume Mode (`config.auto_resume: true`):**
When enabled, supervisors call `ops.AutoResume` at CHECKPOINT or COMPLETED. Only the orchestrator auto-resumes transition checkpoints (`PLANNING_COMPLETE`, `MANY_TO_ONE_READY`); doers/reviewers continue existing work without consuming the checkpoint. Other checkpoints and sprint completions may be resumed by any role. Each resume logs its executed-transition count, and WARN reports retain partial failures. Use `§BRAND_BINARY_NAME§ pause` for a hard stop (pause is never auto-resumed). Toggle at runtime via TUI `[y] yolo`.

**v1 Assumption: Human Availability**

Liza assumes human will respond to escalations within a reasonable timeframe. If human is unavailable:
- Work pauses indefinitely (safe default)
- No data loss or corruption risk
- Sprint can resume when human returns (state persists in `.liza/`)

This is acceptable for v1 because:
1. Target users are solo/small teams who control their own schedules
2. "Safe pause" is preferable to autonomous decisions requiring human judgment
3. Webhook notifications reduce risk of forgotten checkpoints

**Not Supported (v1):** Automatic timeout-based abort, delegation to backup human, or SLA-based escalation paths. These require organizational context Liza doesn't have.

**Manual Override Path:**
- To resume: `liza resume`
- To abort: `liza stop`

**CHECKPOINT File Format:**
```
2025-01-17T14:00:00Z
```
- **Only the timestamp is required** (ISO 8601 format)
- Watcher uses this for stale detection
- If human creates manually via `touch`, timestamp may be missing — watcher handles this gracefully by using file mtime
- Optional: add human-readable notes after the timestamp line (ignored by tooling)

### Checkpoint Sequence

```
1. HALT
   ├── All agents complete current atomic operation
   ├── Commit any pending changes
   ├── Write state to blackboard
   └── Exit gracefully (code 42)

2. CHECKPOINT file created (automatic or manual)
   └── Supervisors wait (same as PAUSE behavior)

3. HUMAN REVIEW
   ├── Read sprint-summary in blackboard
   ├── Review anomalies section
   ├── Review metrics
   ├── Assess goal alignment
   └── Decide next action

4. HUMAN DECISION
   ├── CONTINUE → Remove CHECKPOINT, agents resume
   ├── ADJUST_SPECS → Update specs/, then CONTINUE
   ├── ADJUST_CONTRACTS → Update contracts/, then CONTINUE
   ├── REPLAN → Set tasks to BLOCKED, planner rescopes
   ├── PIVOT → Major scope change, new sprint
   └── STOP → Create ABORT file

5. DOCUMENT DECISION
   └── Add entry to sprint.retrospective with rationale
```

### Planning Transition Gate

When planning tasks (epic-planner, code-planner) are merged, the orchestrator checkpoints the sprint with `checkpoint_trigger: PLANNING_COMPLETE` instead of immediately creating child tasks. When a many-to-one transition cohort is ready, it checkpoints with `checkpoint_trigger: MANY_TO_ONE_READY`. This gives the human a chance to review planning output or fan-in readiness before downstream work begins (unless auto-resume is enabled, in which case agents resume automatically).

**Two-wake model:**

1. **Wake 1:** Orchestrator detects merged planning tasks with unconsumed `output[]` or a ready many-to-one cohort. For `PLANNING_COMPLETE` it first reviews and dispositions each plan in the reviewed hand-off domain (below), then creates the checkpoint with `trigger: PLANNING_COMPLETE` or `MANY_TO_ONE_READY` if anything is admissible → downstream transition creation waits for resume; doer/reviewer roles may continue existing work (or auto-resume advances immediately)
2. **Human reviews** planning output or fan-in readiness in the sprint summary → runs `liza resume` (skipped when auto-resume is enabled)
3. **Resume:** The resume operation runs available transitions (reviewed admission for automatic resume, operator admission for explicit resume) → child tasks created → doers can claim. Orchestrator PreWork handles a remaining resumed transition trigger. Cleanup compares sprint ID/number, checkpoint time, trigger and IN_PROGRESS status under the write lock, so an older pass cannot clear a newer checkpoint.

**Plan hand-off disposition ([ADR-0159](../architecture/ADR/0159-orchestrator-plan-handoff-disposition.md)):**
- Domain: selected `manual` transitions with `per-subtask` or `one-to-one` cardinality out of a planning pair whose task has `output[]`. Auto-only, many-to-one and empty-output sources are outside it and behave as before. A selected gated transition stays pending, and needs its disposition, even after an auto transition from the same plan has run; an unselected exclusive route never creates a hypothetical pending handoff.
- The orchestrator records `plan_check`: `plan-check <id> --pass`, optionally with selected advisory hook/probe notes via `--notes-file`, or `--hold <ask>` for a human action not yet done. Material scheduling/prerequisite corrections use independently reviewed `amend-plan` when existing output identities remain fixed; broader changes use `replan <id> --reason` with authorized consumer retirement. Only an operator `plan-check <id> --clear` releases a hold; pass, amendment begin and replan refuse a held plan. Notes cannot excuse missing producers/provisioning/runtime inputs or alter acceptance, scope, contracts or order.
- Automatic creation (auto-resume, orchestrator and reviewer PreWork) expands an in-domain plan only when it is `passed` and every in-domain planning dependency has transitioned or is itself admissibly passed. An operator resume or `proceed` expands undispositioned plans too, but never a held one.
- Wake classes: undispositioned plans render for review; passed ones render checkpoint-only until they transition (crash recovery without re-review); a passed plan whose upstream was replanned or held renders for reconciliation; held plans do not wake and raise `AWAITING HUMAN`, and keep the sprint open: no sprint- or coding-complete wake while one is held.
- The `PLANNING_COMPLETE` verifier requires every wake-time plan dispositioned and admissible plans checkpointed during the turn; it self-heals a checkpoint only for admissible plans, since a checkpoint cannot expand an undispositioned plan. A plan the turn left undecided is recorded: unchanged, it no longer wakes `PLANNING_COMPLETE`, stays outstanding, keeps the sprint open and is reported by a once-key `PLAN DISPOSITION MISSING` alert and status `DISPOSITION_REQUIRED`; new input (an operator note to it, a plan-check, a changed class, blocker, output or dependency) re-admits it for one turn.
- Initial gated per-subtask output-validation/selective-inheritance failures and plan replacements refused by a pre-existing live provider declaration persist a material-input observation. Unchanged failures do not re-wake planning, auto-select a planning checkpoint or automatically retry; status and a once-key `PLAN HANDOFF FAILED` alert retain the repair evidence. They keep completion/integration unsettled and remain in carry-forward even if another outgoing transition ran. Relevant input repair, including releasing the provider holder, permits retry; an operator retry still validates and reports refusal without duplicating diagnostics. Other errors, cycles and crash recovery keep their existing handling.
- If a separately merged correction replaces an unused original, an operator may run `plan-check ORIGINAL --replaced-by MERGED_CORRECTION`. Both must have output in the same reviewed role-pair; original holds must be cleared first, and delivered children/executed transitions forbid retirement. Retarget/review pending `inherit_inputs` selectors naming the original, and pending same-role-pair plans other than the correction that depend on it, first. The audited `replaced` disposition preserves MERGED and ordinary dependencies, removes only the original hand-off/completion barrier and cannot be cleared or revived. Correction review and expansion continue normally; no automatic correction detection occurs.
- A pending `amend-plan` fences its original on every generation path, including explicit resume/proceed and crash recovery, and keeps completion/integration unsettled. The original remains MERGED; the correction follows ordinary independent review and never expands itself. Apply validates the exact merged correction, stable existing slots (new slots may append), current prospective graph and immutable ancestry, then preserves original attribution/provider identity, clears pending and old pass/notes and retires only the correction's handoff. A later human hold survives. Fresh disposition is required before original handoff. If apply refuses after drift, `--replace-pending CORRECTION --reason TEXT` quarantines an unapplied MERGED or ABANDONED correction and creates fresh review work without releasing the fence or hold. Active/applied corrections refuse; terminal tasks never reopen. See [ADR-0197](../architecture/ADR/0197-reviewed-plan-amendments-and-validation-notes.md).

Single-Scope architecture direct coding retains this gate and architecture quorum;
`when: coding-allocation` selects flat fully reviewed coding units and the exclusive
`scope-decomposition` route retains ordinary stages. Unmarked output and old frozen
configurations retain their earlier topology. RCA-required work and intermediate
descendant writer waits refuse direct allocation rather than losing their review/ordering.

Explicit `amend-plan --contract` and `replan --preserve-output-identity` extend correction
to expanded architecture while preserving the complete ordered manifest, children,
transition markers, selectors and immutable review attribution. Existing referenced
Scope `#### CONTRACT` subsection alone may change, leaving surrounding metadata/subsections
unchanged; bare architecture refs refuse edits. Unused nonarchitecture planning prose may
preserve identity outside its frozen strict acceptance allocation. Approved resolved
proofs remain unchanged. Pending fences include applicable consumer admission/submission; correction work
remains runnable. Apply adopts under ORIGINAL, retains human holds and refuses incompatible
evidence without dropping the fence. Legacy scheduling amendments and destructive replan
retain their prior semantics ([ADR-0198](../architecture/ADR/0198-frozen-interface-corrections-and-direct-coding-allocation.md)).

Inspection `get tasks`/`get-tasks` defaults to active work, including held/refused/undecided
selected handoffs, pending originals and exact MERGED pending corrections. `--all` provides
history; explicit task/field queries remain complete. This is visibility, not claimability.
Typed `planning_change` records explicit correction/replan trigger and original task;
`get metrics` groups logical creations once by UTC date, kind, trigger and original
architecture classification, including archives, and discloses unknown attribution and
missing creation times. Applying/retrying is not another creation; comparable-run rates
still require subsequent observation.

**Gate correctness:**
- Fresh sprint (trigger empty) → gate does not fire
- Manual checkpoint (trigger empty) → gate does not fire
- Sprint-complete checkpoint (trigger `SPRINT_COMPLETE`) → gate does not fire
- Planning checkpoint not yet resumed (status `CHECKPOINT`) → gate does not fire; doer/reviewer role loops may still process existing work
- A transition pass stamps `sprint.timeline.transitions_attempted_at` at its start under the state lock only when it actually attempts pending planning work, whether called by resume or PreWork. Skipped, held and unchanged failed plans do not stamp; later merges/plan checks stay fresh. The timestamp bounds blocked-triage preference; matching material failure evidence suppresses ordinary planning wakes.
- An actionable BLOCKED task whose dependency or awaited task is a planner with unconsumed output wakes Wake 1 (`PLANNING_COMPLETE`) instead of `BLOCKED_TASKS`, which may not checkpoint. The preference holds only while that planner merged, or was released by a plan-check (hold cleared or plan passed), after `transitions_attempted_at`: once a pass fails to consume it, blocked triage regains its normal priority. Checkpoints that attempt no transition do not count.
- After transitions consumed → `countMergedPlanningTasksWithOutput` returns 0 → idempotent
- Cycle-blocked planning tasks are excluded from orchestrator wake detection and planning-complete rendering, but remain visible for carry-forward, replan, and checkpoint auto-trigger

The supervisor retains the wake selected before index refresh for that invocation.
After refresh it revalidates the selected trigger's predicate against fresh state;
if the work disappeared, ordinary priority selects any remaining work. No work
means no provider launch. The prompt, human-note consumption, verification and
checkpoint self-heal all use the same revalidated decision. Thus a new blocker
during indexing cannot replace a still-eligible planning handoff's instructions.
The next wait starts with ordinary priority, including after a failed transition.

Before launching, the supervisor also rechecks its shared pause/checkpoint and
stop predicates, cancellation, provider availability and quota signals. A cancelled launch
clears the selection and restores idle runtime status before returning to the
existing gates; it is not counted as a provider turn or spin. Revalidation never
resumes a checkpoint or creates downstream tasks. Existing manual and automatic
resume policy remains in charge. An INFO skip log names the selected and fresh
triggers for diagnosing changes during indexing.

**Replan timing and reason:** `liza replan` works at `CHECKPOINT` (it then resumes the sprint) or `IN_PROGRESS` (sprint status and trigger untouched), as long as the task has no children. `--reason` is appended to the replacement's description for its planner and plan reviewer.

**Replan with multi-phase:** When replanning a task that is part of a phase chain, `liza replan`
requires explicit task ID (auto-detect may find multiple candidates). The new task inherits
`depends_on` (cloned). Non-terminal downstream tasks' `depends_on` are retargeted from old→new
task ID (order-preserving dedupe). Terminal downstream tasks trigger a warning.

### Checkpoint Review Checklist

```markdown
## Sprint N Checkpoint Review

### Metrics
- [ ] Tasks completed: ___ / ___ planned
- [ ] Tasks blocked: ___
- [ ] Tasks abandoned: ___
- [ ] Calendar time used: ___ / ___ allocated
- [ ] Anomalies logged: ___
- [ ] Trade-offs accepted: ___

### Anomaly Patterns
- [ ] Reviewed anomalies section
- [ ] No systemic patterns detected
- [ ] OR: Pattern identified → action: ___

### Goal Alignment
- [ ] Current state matches original intent
- [ ] OR: Drift identified → action: ___

### Spec Health
- [ ] Specs still accurate
- [ ] OR: Spec gaps found → update needed: ___

### Decision
- [ ] CONTINUE as-is
- [ ] CONTINUE with adjustments: ___
- [ ] REPLAN required
- [ ] STOP
```

---

## Retrospective Protocol

Retrospectives are **data-driven**, not feeling-based. The blackboard provides the data.

**Owner:** Human produces the retrospective, using data from blackboard (log.yaml, anomalies, metrics). Agents provide raw data; human synthesizes patterns and actions.

**Write Mechanism:** Human edits `.liza/state.yaml` directly to populate the `sprint.retrospective` field. Use any text editor to paste the retrospective YAML structure into the sprint section.

### Retrospective Timing

| Event | Retrospective? |
|-------|---------------|
| Sprint checkpoint | Mini-retro (metrics + patterns) |
| Goal completion | Full retro |
| Circuit breaker | Incident retro |
| Human request | Ad-hoc retro |

### Retrospective Inputs

| Source | Data |
|--------|------|
| `log.yaml` | State transitions, timing |
| `anomalies` section | Retries, trade-offs, blocked reasons |
| `sprint.metrics` | Counts, durations |
| `discovered` section | Adjacent problems found |

### Retrospective Output

```yaml
retrospective:
  timestamp: 2025-01-19T18:30:00Z
  metrics:
    tasks_planned: 5
    tasks_completed: 4
    tasks_blocked: 1
    total_iterations: 47
    review_cycles: 12
    calendar_days: 2
  patterns_identified:
    - pattern: "serialization failures"
      occurrences: 3
      tasks: [task-2, task-3, task-5]
      root_cause: "nested entity handling not in architecture"
      action: "ADR required"
  spec_gaps:
    - gap: "FR-012 assumes flat entities"
      discovered_in: task-3
      action: "Update spec with nesting requirements"
  contract_observations:
    - observation: "Retry limit 3 too low for flaky API"
      action: "Consider raising to 5 for API tasks"
  actions:
    - id: action-1
      type: ADR
      description: "Document nested entity serialization decision"
      owner: human
    - id: action-2
      type: SPEC_UPDATE
      description: "Clarify entity nesting in requirements.md"
      owner: human
  notes: |
    First sprint. Calibration data collected.
    5 tasks in 2 days is sustainable.
    API flakiness higher than expected.
```

---

## Spec Evolution Protocol

Specs are **living documents** but changes must be controlled and audited.

### When Specs Change

| Trigger | Process |
|---------|---------|
| Checkpoint reveals gap | Human updates during checkpoint |
| Circuit breaker (spec-level) | Mandatory update before resume |
| Discovered item escalated | Planner flags, human decides |
| Assumption invalidated | Block task, update spec, then resume |

### Spec Change Process

```
1. IDENTIFY gap or error in spec
2. PAUSE affected work (tasks → BLOCKED with "spec update pending")
3. UPDATE spec (human edits, add changelog, commit)
4. LOG change in activity log (log.yaml: action=spec_updated)
5. ASSESS impact (which tasks affected? rescope needed?)
6. RESUME (unblock tasks, agents re-read specs on restart)
```

### Spec Changelog Format

```markdown
# Retry Logic Specification

## Changelog
| Date | Change | Triggered By |
|------|--------|--------------|
| 2025-01-19 | Added nested entity handling | task-3 blocked |
| 2025-01-17 | Initial version | goal creation |
```

---

## Multi-Sprint Lifecycle

Liza supports multiple sprints within a single goal. When a sprint completes and the human resumes, a new sprint is automatically created.

### Sprint Advance Trigger

On `liza resume` from CHECKPOINT, if all planned tasks are terminal:
1. Current sprint is archived to `.liza/archive/sprint-N.yaml`
2. A lightweight `SprintSummary` is recorded in `state.sprint_history`
3. A new sprint is created with `Number = previous + 1`
4. Non-terminal tasks (e.g., READY, IMPLEMENTING, BLOCKED) are carried into the new sprint's `scope.planned`
5. The planner then detects work to do (carried tasks or INITIAL_PLANNING if none)

If resumed from CHECKPOINT but planned tasks are NOT all terminal (mid-sprint manual checkpoint), the same sprint continues as IN_PROGRESS.

### Sprint Archive

Full sprint data (scope, metrics, retrospective) is archived to:
```
.liza/archive/sprint-N.yaml
```

State.yaml keeps only `sprint_history[]` — lightweight summaries for quick reference:
```yaml
sprint_history:
  - id: sprint-1
    number: 1
    status: COMPLETED
    started: 2025-01-17T09:00:00Z
    ended: 2025-01-19T14:00:00Z
    tasks_done: 4
```

### Task Carry-Forward

Non-terminal tasks automatically carry into the new sprint's planned scope. This includes:
- READY, IMPLEMENTING, READY_FOR_REVIEW, REVIEWING, REJECTED, BLOCKED, DRAFT, INTEGRATION_FAILED

Terminal tasks (MERGED, ABANDONED, SUPERSEDED) are generally NOT carried forward, with one exception:

**Planning tasks with unconsumed output:** MERGED tasks that belong to a planning role-pair (e.g., `code-planning-pair`), have non-empty `output[]`, and have no `transitions_executed` are carried forward. These tasks have planning output that the orchestrator has not yet expanded into child tasks via the Planning Transition Gate (see above). Without carry-forward, the new sprint would have an empty planned scope and the orchestrator would idle indefinitely.

---

## Blackboard Sprint Section

```yaml
sprint:
  id: sprint-1
  number: 1
  goal_ref: goal-1
  scope:
    planned: [task-1, task-2, task-3, task-4, task-5]
    stretch: [task-6]
  timeline:
    started: 2025-01-17T09:00:00Z
    deadline: 2025-01-19T18:00:00Z
    checkpoint_at: null
    ended: null
  status: IN_PROGRESS
  checkpoint_trigger: ""
  metrics:
    tasks_done: 2
    tasks_in_progress: 1
    tasks_blocked: 1
    iterations_total: 23
    review_cycles_total: 6
  retrospective: null
```

### Metrics Definitions

| Metric | Definition |
|--------|------------|
| `tasks_done` | Count of tasks with status IN (MERGED, ABANDONED, SUPERSEDED) |
| `tasks_in_progress` | Count of tasks with status IN (IMPLEMENTING, READY_FOR_REVIEW, REJECTED) |
| `tasks_blocked` | Count of tasks with status = BLOCKED |
| `review_verdict_approvals` | Count of `approved` events across task histories |
| `review_verdict_rejections` | Count of `rejected` events across task histories |
| `review_verdict_count` | `review_verdict_approvals + review_verdict_rejections` |
| `review_verdict_approval_rate_percent` | `review_verdict_approvals / review_verdict_count * 100` |
| `task_submitted_for_review_count` | Count of `ready_for_review` events across task histories |
| `task_outcome_approval_rate_percent` | `review_verdict_approvals / task_submitted_for_review_count * 100` |

For sprint state transitions, see [State Machines — Sprint State Machine](../architecture/state-machines.md#sprint-state-machine).

## Related Documents

- [Circuit Breaker](circuit-breaker.md) — systemic failure detection
- [Task Lifecycle](task-lifecycle.md) — individual task flow
- [Vision](../build/1%20-%20Vision.md) — design philosophy
- [ADR Template](../architecture/ADR/TEMPLATE.md) — Architecture Decision Records format
