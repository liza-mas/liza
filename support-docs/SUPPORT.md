# §BRAND_NAME_TITLE§ Support Reference

Troubleshooting reference for §BRAND_NAME_TITLE§ multi-agent executions.
This file is written to `§BRAND_PROJECT_DIRNAME§/SUPPORT.md` during `§BRAND_BINARY_NAME§ init`.

## Diagnostic Commands

```bash
§BRAND_BINARY_NAME§ status                        # Dashboard: goal, sprint, agents, task summary
§BRAND_BINARY_NAME§ get tasks                     # All tasks with current state
§BRAND_BINARY_NAME§ get tasks --format table      # Tabular view
§BRAND_BINARY_NAME§ get agents                    # Registered agents and lease status
§BRAND_BINARY_NAME§ get agents --zombies          # Live §BRAND_BINARY_NAME§ agent supervisors missing from state
§BRAND_BINARY_NAME§ clear-agent-degraded <id>     # Clear role-capacity health after manual recovery
§BRAND_BINARY_NAME§ validate                      # Check blackboard against invariants
§BRAND_BINARY_NAME§ validate --skip-process-checks # Offline/archive validation only
§BRAND_BINARY_NAME§ analyze                       # Circuit breaker pattern detection
```

`§BRAND_BINARY_NAME§ analyze --json` reports the selected `response`
(`WARNING`, `CHECKPOINT`, or `HALT`), provider-evidence `classification`
(`ACKNOWLEDGED_HISTORICAL`, `NEW`, or `CONTINUING`), and `explanation` from the
same `AnalyzeResult` used for human output. `triggered` is true only for `HALT`.

`§BRAND_BINARY_NAME§ status --format json` and `§BRAND_BINARY_NAME§ get agents --format json` include `process_status_source` and `process_status_detail` for agents; status also includes them for phase-handoff blockers. Use these fields when a task appears assigned but the process state is ambiguous. On Linux, procfs identity checks distinguish a matching §BRAND_NAME_TITLE§ supervisor from a dead or PID-reused/mismatched process; when process identity is unavailable, active leases are treated conservatively.
PID-based `process_status` is local to the namespace running the command. In containers, sandboxes, or SSH/host boundary situations, a live host agent can appear as `process_status: stopped` because its PID is not observable from the current namespace. Do not recover from `process_status: stopped` alone in that environment; recent heartbeat, active lease, and growing `§BRAND_PROJECT_DIRNAME§/agent-outputs/` are stronger liveness evidence. Treat contradictory signals as ambiguous and inspect `process_status_source` / `process_status_detail` before recovery.
Agent health is separate from lifecycle status. A degraded agent epoch remains visible in status/get-agents health fields and does not count as repair-agent-pool capacity until it is cleared or a newer successful claim proves capacity. If the agent process exits and unregisters, the health marker stays visible as degraded capacity context for repair/status output.
Verified zombie-agent detection requires procfs and uses process cwd as project-scope evidence. A matching cwd verifies a current-project process, while a different readable cwd excludes a foreign process. When cwd is unreadable, confirmed agent processes are reported as unknown scope: `§BRAND_BINARY_NAME§ validate` remains successful but warns that the scan is partial, and `§BRAND_BINARY_NAME§ get agents --zombies` reports only verified zombies while emitting the same warning. Goal IDs do not promote unknown-scope candidates to zombies when a project root is supplied. Unknown scope means unverified, not unowned; do not stop a process solely on the basis of this warning. On hosts without procfs, `§BRAND_BINARY_NAME§ validate` warns and skips the live-process check, while `§BRAND_BINARY_NAME§ get agents --zombies` reports that scanning is unavailable.

## Recovery Commands

```bash
§BRAND_BINARY_NAME§ recover-task <task-id>        # Release claims + preserve/reattach coherent worktree/branch
§BRAND_BINARY_NAME§ recover-task <task-id> --fresh # Explicitly discard worktree/branch and reset non-blocked task to initial
§BRAND_BINARY_NAME§ recover-agent <agent-id>      # Release claim + remove worktree + delete agent
§BRAND_BINARY_NAME§ release-claim <task-id>       # Granular: release claim only
§BRAND_BINARY_NAME§ clear-stale-review-claims     # Clear all expired review leases
§BRAND_BINARY_NAME§ repair-superseded-dependencies <task-id> --reason <reason> # Repair illegal terminal dependency edges
§BRAND_BINARY_NAME§ delete agent <id>             # Remove agent from state
§BRAND_BINARY_NAME§ delete task <id>              # Remove task from state
```

If a write fails because a legacy `goal.alignment_history[].summary` exceeds
the 4096-byte state text limit, run `§BRAND_BINARY_NAME§ migrate`, then retry.
Migration replaces oversized summaries with a bounded scrub notice while
preserving event metadata and task descriptions. New task creation records
only the task ID in alignment history; full descriptions remain on tasks and
in the activity log.

## System Control

```bash
§BRAND_BINARY_NAME§ pause                         # Pause all agents (sets CHECKPOINT)
§BRAND_BINARY_NAME§ resume                        # Resume or advance sprint (see Sprint Lifecycle)
§BRAND_BINARY_NAME§ stop                          # Abort system
§BRAND_BINARY_NAME§ sprint-checkpoint             # Force checkpoint (halt + summary)
§BRAND_BINARY_NAME§ replan [task-id]              # Invalidate planner output, create new planning task
§BRAND_BINARY_NAME§ proceed <task-id> <transition> # Create child tasks for next role-pair
```

## Pipeline Structure

Tasks flow through role-pairs organized in sub-pipelines. The project's frozen pipeline is in `§BRAND_PROJECT_DIRNAME§/pipeline.yaml` — inspect it for actual role-pairs, transitions, and state names.

Transitions with `trigger: manual` are human gates; `trigger: auto` transitions run without one. Use `§BRAND_BINARY_NAME§ status` to see currently available manual transitions.

### Transition Cardinalities

Each transition in `§BRAND_PROJECT_DIRNAME§/pipeline.yaml` has a `cardinality`:

| Cardinality | Behavior |
|-------------|----------|
| `per-subtask` | One child task per `output[]` entry |
| `one-to-one` | Single child task from parent |
| `many-to-one` | All sibling tasks in cohort must reach approved, then one child linking all parents |

### `§BRAND_BINARY_NAME§ proceed`

Creates child tasks based on a completed task's `output[]` and the transition's cardinality. After proceed, run `§BRAND_BINARY_NAME§ resume` to start the next sprint.

The source task may be either at the transition's configured source state or already at `MERGED`.
`MERGED` is treated as satisfying the transition precondition because `§BRAND_BINARY_NAME§ proceed` operates from sprint-terminal source tasks and does not change the source task's status.

```bash
§BRAND_BINARY_NAME§ proceed <task-id> <transition-name>
```

Transition names appear under `transitions:` within each sub-pipeline and under the top-level `pipeline-transitions:` (for cross-subpipeline transitions) in `§BRAND_PROJECT_DIRNAME§/pipeline.yaml`.

## Task State Machines

Every role-pair in `§BRAND_PROJECT_DIRNAME§/pipeline.yaml` defines its own state names under `states:`. The generic flow is:

```
initial → executing → submitted → reviewing → approved (sprint-terminal)
               │ ↑                      ↓
               │ └───── rejected ──────┘
               └──→ BLOCKED
```

Cross-pair states (not pair-specific):
- **BLOCKED** — Cannot proceed; see `blocked_reason` and `blocked_questions`
- **SUPERSEDED** — Replaced by tasks in `superseded_by`, or completed externally with no replacements (terminal)
- **ABANDONED** — Killed by orchestrator (terminal)
- **MERGED** — Merged to integration branch (terminal, coding pair only)
- **INTEGRATION_FAILED** — Merge conflict or test failure (coding pair only)

To find the actual state names for a role-pair, check `role-pairs.<name>.states` in `§BRAND_PROJECT_DIRNAME§/pipeline.yaml`. Some pairs define extra states (e.g. `partially-approved`, `reviewing-2` for quorum review, or `clean` for no-issues-found).

Supervisors automatically block a still-owned executing task when the child provider process makes no observable progress for `config.agent_progress_timeout` seconds. Observable progress is task state movement, worktree HEAD/status movement including untracked files, or provider stdout/stderr output. This prevents a stale `WORKING` agent from holding a task indefinitely when its provider process stalls. The watchdog cancels the provider and waits for it to exit before cleaning the worktree.

## Sprint Lifecycle

```
IN_PROGRESS → CHECKPOINT → COMPLETED → (new sprint) IN_PROGRESS
```

### `§BRAND_BINARY_NAME§ resume` behavior depends on sprint state:

| Sprint State | Condition | Effect |
|--------------|-----------|--------|
| CHECKPOINT | Not all tasks terminal | Back to IN_PROGRESS (resume current sprint) |
| CHECKPOINT | All tasks terminal | Mark COMPLETED |
| COMPLETED | — | Archive sprint, create new one, execute pipeline transitions |

**Two-step advance:** To move from one pipeline phase to the next, run `§BRAND_BINARY_NAME§ resume` twice: first marks COMPLETED, second archives and advances.
Approved transition-source output can make a task sprint-terminal before it is integrated. The second `§BRAND_BINARY_NAME§ resume` refuses to advance/archive until approved planning output is merged; run `§BRAND_BINARY_NAME§ wt-merge <task-id>` first.

### Checkpoint Actions

When a sprint checkpoints (status: CHECKPOINT), it is not auto-cleared.
Transition checkpoints (`PLANNING_COMPLETE`, `MANY_TO_ONE_READY`) and a
circuit-breaker `CHECKPOINT` response gate downstream transition creation;
doer/reviewer agents may continue already-available work in the current sprint
unless system mode is `PAUSED` or `CIRCUIT_BREAKER_TRIPPED`. The human decides:

| Action | Command | When |
|--------|---------|------|
| Accept & resume | `§BRAND_BINARY_NAME§ resume` | Satisfied with planner output or fan-in readiness, continue |
| Amend & replan | Edit plan, commit, `§BRAND_BINARY_NAME§ replan` | Want to change planner output |
| Pipeline transition | `§BRAND_BINARY_NAME§ proceed <task-id> <transition>` | Create child tasks from output or a ready cohort (auto-done by `§BRAND_BINARY_NAME§ resume` in batch) |
| Pause for manual work | (no command) | Make manual changes first |
| Abort | `§BRAND_BINARY_NAME§ stop` | Stop entirely |

### Replanning

When a transition checkpoint fires, the human reviews proposed downstream work before child tasks are created. `PLANNING_COMPLETE` means planner `output[]` represents the proposed task breakdown; `MANY_TO_ONE_READY` means a fan-in cohort is ready to create its consolidated child task.

```bash
# Typical replan workflow
# 1. Find planner output files — check the task's output[] in state.yaml
§BRAND_BINARY_NAME§ get tasks                         # identify the planning task
# 2. Edit the planner's output docs (e.g. specs/plan.md, specs/stories/*.md)
vim specs/plan.md                      # amend planner deliverables
# 3. If scope changed, also align upstream docs that fed the planner
#    (e.g. specs/goals/*.md, specs/epic.md) so inputs match outputs
git add -A && git commit -m "amend plan"
# 4. Replan
§BRAND_BINARY_NAME§ replan                            # auto-detects the planning task
§BRAND_BINARY_NAME§ replan <task-id>                  # or specify task ID explicitly
```

Replan invalidates the old task's output (preserved for audit, marked superseded), creates a new planning task with the same role-pair and spec, and returns the sprint to IN_PROGRESS. Multiple replans increment: `<task-id>-replan-1`, `<task-id>-replan-2`, etc.

### Auto-Resume

By default, checkpoints require manual `§BRAND_BINARY_NAME§ resume`. Auto-resume skips these gates:

- At init: `§BRAND_BINARY_NAME§ init --auto-resume "Goal"`
- At runtime: TUI `y` key toggles on/off

When enabled, agents auto-call `§BRAND_BINARY_NAME§ resume` on CHECKPOINT or COMPLETED. Use `§BRAND_BINARY_NAME§ pause` for a hard stop (never auto-resumed).

## Agent Review Cycles

Mutating lifecycle commands return `result.outcome` and one server-selected
`result.safe_action`, including on `ok:false` failures. Follow that field:
`continue` proceeds, `stop` ends task work, `requery` inspects current state
and possible effects, `retry` repeats the original request serially under the
bounded retry policy, and `correct_input` fixes the named invalid fields.
Never derive retry from error text. Preserve the original
`--request-id` + `--expected-transition` pair and payload across retries;
refreshing the token creates a new request and requires reevaluation.
After a returned acceptance refusal, the command retires its own matching
preparation when authority and the task boundary still match. Prior rebase or
test effects remain unknown, and the original request stays stale. Inspect the
task and worktree, repair the evidence, then submit its current immutable SHA
with a fresh request and inspected transition; registration need not change.
Process abandonment and uncertain merge completion retain their preparations
and require inspected recovery before further work.
See [Lifecycle Results](../specs/protocols/lifecycle-results.md) for receipt
expiry, interrupted preparations and best-effort sprint counters.

Both `§BRAND_BINARY_NAME§ await-verdict` and
`§BRAND_BINARY_NAME§ await-resubmission` are foreground calls. Each invocation
waits at most 100 seconds. `--timeout-seconds` is the remaining budget for the
whole review wait, not a new per-call allowance.

- **POLL**: Continue with another foreground invocation of the same command,
  passing the returned `timeout_seconds` as its remaining budget. Do not add
  custom polling or background execution.
- **TIMEOUT**: The total budget is finally exhausted. Safe stop and exit
  normally; do not invoke the command again.
- If the execution harness backgrounds an await call and promises a completion
  notification, safe stop by ending the agent turn. Do not monitor the process
  or start another await call.

### Doer: Submit → Await → Handle

```
§BRAND_BINARY_NAME§ submit-for-review → §BRAND_BINARY_NAME§ await-verdict → handle result
```

Capture the full committed SHA once with `git -C <worktree> rev-parse HEAD`,
submit that literal SHA, and retain it on retry. Wait for submission to finish
with `COMPLETED` or `ALREADY_COMPLETED` and `safe_action=continue` before
awaiting; never run submission and await concurrently. After authorized
handoff/restart, inspect current state and capture the current immutable SHA
for a fresh submission.

- **REJECTED**: Fix issues, resubmit (session stays alive — no cold restart)
- **ALREADY_TRANSITIONED**: Verdict was recovered after the task moved onward; follow `safe_action` (`stop` means exit without more worktree commands, `revise` means you still own it)
- **APPROVED** / **TERMINAL** with `safe_action: stop`: Safe stop and exit normally; do not run more worktree commands because merge cleanup may remove the task worktree
- **NEW_ATTEMPT** / **ABORTED**: Safe stop and exit normally

### Reviewer: Verdict → Await → Re-review

```
§BRAND_BINARY_NAME§ submit-verdict TASK REJECTED --review-commit FULL_SHA --reason FEEDBACK → §BRAND_BINARY_NAME§ await-resubmission → review new changes
```

- **RESUBMITTED**: Review again (session stays alive), then pass the returned full `review_commit` on the next verdict
- **TERMINAL** / **ABORTED**: Safe stop and exit normally

### Quarantined verdicts and conflicting approval

Every authenticated `submit-verdict` now requires `--review-commit FULL_SHA`
(40 or 64 hex characters), bound to the commit actually inspected. Migrate
existing scripts by preserving that reviewed SHA; do not look up a replacement
task boundary after a generation-fence error and pretend it was reviewed.

A valid fenced verdict still fails authorization, but its bounded substantive
evidence is retained separately. Diagnostics name the finding ID and direct the
caller to stop without exposing generations or their fingerprints; the stored
record preserves the immutable boundary and provenance. Missing generation,
malformed input,
unknown task, and invalid or oversized reason are administrative failures
without a substantive record. A valid unknown boundary is retained as unmatched
and never creates a merge hold, even if a later task happens to use that SHA.

Inspect the records through `§BRAND_BINARY_NAME§ get quarantined_verdicts --json`.
Explicit `delete task` also removes that task's findings and reconciliation
audit. Export the records first if you need to retain them after deletion.
Applicable unresolved conflicts block approval and every merge route, including
already-advanced Git recovery. Explicit `review_commit_updated` lineage carries
the finding across rebases; a new SHA does not automatically resolve it.

An authorized current-generation orchestrator records the decision:

```bash
§BRAND_BINARY_NAME§ reconcile-verdict TASK FINDING_ID refuted --reason "The named check covers this boundary; validation evidence is in the review" --agent-id ORCHESTRATOR --json
```

| Disposition | Effect |
|-------------|--------|
| `refuted` | Clear the hold with evidence explaining why the finding is incorrect |
| `superseded` | Clear the hold with explanation of the replacement/correction and its boundary |
| `accepted` | Record that the finding stands; accepted rejection still blocks unchanged work and accepted approval creates no quorum |
| `escalated` | Retain the hold and record what further judgment is required |

The actor is the actual registered orchestrator, checked again in the write
transaction. The orchestrator may decide directly or route an escalation to a
human. Use its inherited authority; never copy a registration generation into
an operator shell. There is no `--changed-by` or missing-generation bypass.
Matching frozen orchestrator roles receive the new capability automatically
through `LoadFrozen`'s in-memory operation migration; no manual file edit is
needed. Custom roles still need their configured capability and orchestrator type.

Reconciliation appends actor, time, disposition, and reason without changing
task status, approval actors, or quorum. Identical retries are idempotent.
Evidence ordered before merge prevents a conflicting merge. Evidence arriving
after a completed merge remains available for reconciliation and corrective-work
routing; it does not reopen or roll back the task. A task-review lock timeout
is retryable and does not claim that evidence was saved.

## Agent Log Analysis

Agent logs (`§BRAND_PROJECT_DIRNAME§/agent-outputs/`) are the primary diagnostic tool.

**LLM-assisted** — use `/§BRAND_BINARY_NAME§-logs` in any pairing agent session to cross-correlate logs, diagnose patterns, and propose fixes.

**CLI analyzer** (stdlib Python 3.12+):
```bash
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_BINARY_NAME§-logs/scripts/analyze-log.py §BRAND_PROJECT_DIRNAME§/agent-outputs/*.txt
```

**Browser analyzer** — drag-and-drop visual charts:
```bash
open ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_BINARY_NAME§-logs/tools/§BRAND_BINARY_NAME§-session-analyzer.html   # or xdg-open on Linux
```

## state.yaml

Key task fields:
- `status` — current state
- `assigned_to` — which agent holds the task (doer)
- `reviewing_by` — which agent is reviewing
- `lease_expires` / `review_lease_expires` — when the claim expires
- `base_commit` — review diff base; for submitted/reviewing tasks with a worktree, the merge-base of `review_commit` and the configured integration branch
- `review_commit` — commit submitted for review; must match task worktree HEAD
- `merge_commit` — commit on integration branch after merge
- `iteration` — doer iteration count
- `review_cycles_current` / `review_cycles_total` — rejection count
- `blocked_reason` / `blocked_questions` — why the task is stuck
- `repair_request` — optional complete orchestrator-only repair request captured when the blocker is a state transition the assigned agent cannot perform (`operation`, `target`, `evidence`, `validation`, and either `command` for command-based non-dependency requests or `dependency_updates` for `apply-dependency-repair`)
- `rejection_reason` — reviewer feedback on rejection
- `depends_on` — task IDs that must be directly MERGED before this task is claimable and must not point downstream in the pipeline
- `output[]` — structured output entries (used by `§BRAND_BINARY_NAME§ proceed` to create child tasks)
  - `output[].depends_on` — sibling output indexes resolved during `proceed`
  - `output[].task_depends_on` — existing concrete task IDs copied to generated child tasks
  - `output[].destructive_db` — requires non-empty validation, with every command starting `§BRAND_ENV_PREFIX§_ALLOW_DESTRUCTIVE_DB=1 ` or `env §BRAND_ENV_PREFIX§_ALLOW_DESTRUCTIVE_DB=1 `; copied only to per-subtask children
- `history[]` — timestamped event log per task

Key agent fields:
- `id` — e.g. `coder-1`, `code-reviewer-2`
- `role` — runtime role name
- `status` — STARTING, IDLE, WORKING, REVIEWING, WAITING, HANDOFF
- `lease_expires` — agent registration expiry
- `current_task` — task ID being worked on

### Modifying state.yaml

**Golden rule:** Never edit `state.yaml` directly. Direct edits bypass locking,
role authorization, audit history, and all-or-nothing candidate validation.

Use the dedicated CLI operation for every state mutation. In particular:

- use `§BRAND_BINARY_NAME§ retarget-dependency <task-id> <old-dep-id> <new-dep-ids> --reason <reason>` for one direct edge on a non-terminal task;
- for multiple active tasks or complete dependency lists, store a command-free `apply-dependency-repair` JSON request with `§BRAND_BINARY_NAME§ mark-blocked --repair-request-file <path>`, then have the orchestrator run `§BRAND_BINARY_NAME§ apply-dependency-repair <blocked-task-id> --reason <reason>`;
- use `§BRAND_BINARY_NAME§ repair-superseded-dependencies <task-id> --reason <reason>` for all illegal downstream direct edges on one `SUPERSEDED` task;
- use `§BRAND_BINARY_NAME§ unblock-task`, `§BRAND_BINARY_NAME§ cancel-task`, `§BRAND_BINARY_NAME§ supersede-task`, `§BRAND_BINARY_NAME§ release-claim`, or `§BRAND_BINARY_NAME§ recover-task` for their declared transitions.

Before any dependency repair, perform semantic verification by re-reading the
affected tasks and their planning/decomposition context. Confirm that a
consumer depends on its provider. If another relationship requires the inverse
edge, name that relationship as an explicit exception rationale in the repair
reason. Structural candidate-state validation cannot supply this semantic
proof.

The dependency request file contains operation `apply-dependency-repair`, the
blocked source task as `target`, unique `dependency_updates` with explicit
`expected_depends_on` and `desired_depends_on` lists, structured evidence, and
validation; it omits `command`. File input is mutually exclusive with the
individual `--repair-*` flags used for command-based non-dependency repairs. Do
not encode dependency repairs as command sequences. If no supported operation
covers another mutation, stop and record an orchestrator-only repair request;
do not work around the state machine. Run `§BRAND_BINARY_NAME§ validate` after
recovery to verify the whole blackboard.

### Changing worktree setup during a run

Read the current value with `§BRAND_BINARY_NAME§ config get config.post_worktree_cmd --json`.
The general query `§BRAND_BINARY_NAME§ get config.post_worktree_cmd --json` is equivalent;
an unset value is `null`. Both config subcommands use the same dotted key.

After scaffolding is committed and merged, validate the proposed command from a fresh
checkout of that commit. It must provide the required build/test environment, run
noninteractively using project-scoped tooling, and succeed on repeated execution.
Generated artifacts must be gitignored or already committed, and a second run must
change nothing observable. Git status must stay clean. Check `git status --porcelain` for
staged, unstaged, and untracked files. Put required ignore rules in the scaffold itself.
Reference environment variables for credentials; do not embed secret values in commands
or reasons.

```bash
§BRAND_BINARY_NAME§ config set config.post_worktree_cmd "make setup" --json
# Explicitly replace a different existing value:
§BRAND_BINARY_NAME§ config set config.post_worktree_cmd "make bootstrap" --replace --reason "Correct project setup" --json
```

Only `config.post_worktree_cmd` is supported. Empty, multiline, NUL-containing,
invalid UTF-8, and whitespace-padded commands are rejected. A repeated identical set
succeeds with outcome `unchanged`; a different existing value returns a nonzero exit
and a JSON `validation` error with `details.conflict: existing_value` unless `--replace`
and a non-empty `--reason` are supplied. Replacement flags guard against mistakes;
they do not authenticate a human caller. The command is stored, not executed or certified
by `config set`. Re-read it afterward to verify the current configuration.

The value comparison and write share one locked transaction. With an agent ID (flag or
environment), the role must explicitly allow `config-set-post-worktree-cmd`, and the
registration generation is checked in that transaction. No default role is granted this
capability. Without an ID, the operator path uses the ordinary locked mutation.

Existing configuration takes precedence over automatic Node detection at merge. If the
operator write commits first, detection leaves it alone. If detection commits first,
a different operator set requires explicit replacement. Existing provider sessions are
not restarted; subsequent setup on claim, resume, review, recovery, or recreation uses
the configuration read by that operation. An already-running setup may have read the
previous value. Configured command failures still fail closed: fix the command or its
environment in the retained worktree, then follow agent recovery guidance. Do not clear
the setting to bypass readiness failures.

**Audit location:** each set attempt passing the domain operation's input validation emits one
greppable `config_set key=config.post_worktree_cmd` process-log record on stderr, also in
JSON mode. It is **not** persisted in `state.yaml` or the activity log; retain stderr if
history is needed. Records include actor, project, outcome (`set`, `replaced`, `unchanged`,
`conflict`, or `failed`), a command SHA-256 fingerprint, and masked, bounded command/reason
excerpts. The detector comparison is observational at invocation time: `match`, `different`,
or `none`. CLI admission failures and in-operation input validation failures (malformed
commands or replacement requests without a non-empty reason) return errors without
emitting these audit records.

Count successful explicit changes (`set`/`replaced`) with `detector=different` or `none`
as candidates for follow-up. Classify missed conventional layouts into the detection
backlog; repeated custom workflows requiring operator intervention provide evidence for
considering planner declarations. Mismatches alone do not establish that need. Log loss,
masking, truncation, and subsequent repository changes limit retrospective classification.

### Known Gotchas

- **`|N` block scalars**: Go writes indentation indicators for multi-line fields (for example `rejection_reason`). YAML serializers can make live state unparseable. Restore a trusted backup or use a supported migration/recovery command; do not hand-edit a live blackboard.
- **Timestamps**: Python's `yaml.dump` can convert `2026-04-14T14:29:31Z` to `2026-04-14 14:29:31+00:00`. Go rejects this. Never round-trip the blackboard through a YAML library.
- **Concurrent writes**: Agents and CLI write concurrently. Only CLI mutations participate in the required lock and validation protocol.
- **Field names**: SUPERSEDED tasks require `rescope_reason` (not `superseded_reason`). Check `§BRAND_BINARY_NAME§ validate` for correct field names.
- **Status constraints**: `§BRAND_BINARY_NAME§ supersede-task` works from BLOCKED, REJECTED, or any pipeline-declared initial state. Without replacements, pass `--recoverability-command "<single-line command>"` to record the operator audit command before branch/worktree cleanup; do not include secrets. Unsupported status changes require escalation, not a direct edit.
- **Dependency edits**: Use `§BRAND_BINARY_NAME§ retarget-dependency <task-id> <old-dep-id> <new-dep-ids> --reason "..."` for one direct edge on a non-terminal task. For multiple active tasks or complete lists, persist a command-free request through `§BRAND_BINARY_NAME§ mark-blocked --repair-request-file <path>` and apply it atomically with `§BRAND_BINARY_NAME§ apply-dependency-repair <blocked-task-id> --reason "..."`. Use `§BRAND_BINARY_NAME§ repair-superseded-dependencies <task-id> --reason "..."` for all illegal downstream direct edges on a `SUPERSEDED` task.
- **Holding a task from review**: Add a `depends_on` on the task that should be reviewed first — the system enforces ordering. Alternatively, set status to the pre-review state.

## Agent Exit Codes

| Code | Meaning | Supervisor action |
|------|---------|-------------------|
| 0 | No more work for this role | Stop supervisor |
| 42 | Graceful abort (context exhaustion, lease lost, pause) | Restart immediately |
| Other | Crash | Restart with backoff |

Exit 42 with `handoff_pending: true` on the task means context exhaustion — the restarted agent reads handoff notes and continues.

## Common Failure Patterns

### Lease defaults

- Lease duration: 30 minutes
- Heartbeat interval: 60 seconds
- If lease expires, task becomes reclaimable

### Stuck task (stale lease)
**Symptom**: Task in executing or reviewing state but agent is gone.
**Diagnosis**: `§BRAND_BINARY_NAME§ get tasks` — check `lease_expires` is in the past (see Lease defaults above).
**Fix**: `§BRAND_BINARY_NAME§ recover-task <task-id>` or `§BRAND_BINARY_NAME§ release-claim <task-id>`.

### Agent crash loop
**Symptom**: Supervisor keeps restarting, agent exits non-zero repeatedly.
**Diagnosis**: Check agent output logs in `§BRAND_PROJECT_DIRNAME§/agent-outputs/` and the bootstrap prompt in `§BRAND_PROJECT_DIRNAME§/agent-prompts/` (what the agent was told to do).
**Fix**: After 5 restarts without progress, supervisor auto-blocks the task. Check `blocked_reason`. May need `§BRAND_BINARY_NAME§ recover-task` then manual investigation.

### BLOCKED task
**Symptom**: Task in BLOCKED state, agents skip it.
**Diagnosis**: Read `blocked_reason`, `blocked_questions`, `depends_on`, and optional `repair_request` in state.yaml. A `BLOCKED` alert is raised when a task blocks; if the orchestrator assesses but cannot resolve it, an `UNRESOLVED BLOCKED` alert is raised.
**Fix**: If the blocker was another task, the blocked task should list it in `depends_on` so the orchestrator wakes when that task changes. If one direct edge is wrong, use `§BRAND_BINARY_NAME§ retarget-dependency <id> <old-dep-id> <new-dep-id[,new-dep-id]> --reason "..."`. For multiple tasks or complete lists, the blocked agent stores the command-free request through `§BRAND_BINARY_NAME§ mark-blocked --repair-request-file <path>` and the orchestrator runs `§BRAND_BINARY_NAME§ apply-dependency-repair <blocked-task-id> --reason "..."`; stale or invalid batches leave every dependency, audit entry, and request unchanged. The task remains BLOCKED until its repair validation passes and it is explicitly unblocked or assessed. Unassigned `§BRAND_BINARY_NAME§ unblock-task <id> --reason "..."` may restore a repaired task with valid pending dependencies to its role-pair initial status, but that task remains dependency-held and unclaimable until every direct dependency is `MERGED`. Adding `--assign-to <doer-agent-id>` is a direct-resume path and remains rejected while any dependency is unmet.
If the task has a preserved worktree and integration moved while it was blocked, use `§BRAND_BINARY_NAME§ unblock-task <id> --rebase-on <integration-branch> --reason "..."`. Tracked worktree changes require `--allow-dirty`, which rebases with Git autostash; untracked files that would be overwritten are refused. Submit/merge conflicts move tasks to `INTEGRATION_FAILED`; unblock-time rebase conflicts remain `BLOCKED` with fresh repair metadata so the preserved worktree can be repaired and unblocked again. Once dependencies merge, claim-time recovery rebases and validates the preserved branch on one captured integration SHA, then uses the completion lock to order the final ref equality check and assignment against cooperating integration movement, without holding the integration mutation lock across the blackboard write.
Supersede/cancel operations rewrite active downstream dependencies first; stale edges to SUPERSEDED or ABANDONED tasks must not remain on active tasks. Supersession also prunes the retiring task's own illegal downstream edges, retains legal historical dependencies, audits removed IDs, and validates the candidate before commit. Otherwise use `§BRAND_BINARY_NAME§ supersede-task <id> [replacements] --reason "..."` to replace with new tasks, `§BRAND_BINARY_NAME§ supersede-task <id> --reason "..." --recoverability-command "§BRAND_BINARY_NAME§ recover-task <id>"` to mark completed externally with no replacements, or `§BRAND_BINARY_NAME§ recover-task <id>` to reset.

If validation reports downstream dependencies on an already-`SUPERSEDED` task, the orchestrator runs `§BRAND_BINARY_NAME§ repair-superseded-dependencies <task-id> --reason <reason>`. The command removes every illegal downstream direct edge in one transaction, retains legal edges and terminal/replacement metadata, records the caller, reason, and removed/retained IDs in task history and the activity log, and validates the full candidate state. Non-`SUPERSEDED`, already-valid, or still-invalid candidates are rejected without mutation. Activity-log failure is returned as a warning after a successful state commit. Never repair the blackboard directly.

### `retarget-dependency` rejects a dependency cycle

When full candidate-state validation finds a cycle, `§BRAND_BINARY_NAME§ retarget-dependency A old-dependency B --reason "..." --json` returns a validation envelope with safe recovery details:

```json
{
  "ok": false,
  "result": null,
  "error": {
    "code": "validation",
    "message": "retarget dependency rejected because the candidate state contains a dependency cycle",
    "details": {
      "operation": "retarget-dependency",
      "task_id": "A",
      "old_dependency": "old-dependency",
      "new_dependencies": ["B"],
      "phase": "candidate-state-validation",
      "cycle_path": ["A", "B", "C", "A"],
      "diagnostic_action": "retarget_dependency_rejected"
    }
  }
}
```

With `--json -v`, stdout contains exactly one JSON envelope and stderr contains only the classified safe message and details; the raw underlying error is not emitted. The ordered string-array `cycle_path` closes the cycle and identifies the edges to repair.

The candidate dependency, repair request, and task history remain unchanged, and no `retarget-dependency` success activity is recorded. The activity log instead records `retarget_dependency_rejected` with masked, bounded rejection evidence. In retry tracking, the supervisor attributes the failure to `retarget-dependency` and uses the envelope's `validation` code.

### Integration failure
**Symptom**: Task in INTEGRATION_FAILED state.
**Diagnosis**: Merge conflict between task worktree and integration branch.
**Fix**: A coder can claim it (`integration_fix: true`). The worktree is preserved for conflict resolution.

### Sprint stuck at CHECKPOINT
**Symptom**: All agents idle, sprint in CHECKPOINT.
**Diagnosis**: `§BRAND_BINARY_NAME§ status` — check checkpoint trigger.
**Fix**: `§BRAND_BINARY_NAME§ resume` to continue, or `§BRAND_BINARY_NAME§ proceed` + `§BRAND_BINARY_NAME§ resume` to advance to next pipeline phase.

### Orphaned worktree
**Symptom**: `.worktrees/task-N/` exists but task is terminal.
**Diagnosis**: `§BRAND_BINARY_NAME§ validate` will flag this.
**Fix**: `§BRAND_BINARY_NAME§ wt-delete <task-id>`.

### Ghost agent
**Symptom**: Agent registered in state.yaml but process is dead or the registered PID now belongs to a different process.
**Diagnosis**: `§BRAND_BINARY_NAME§ get agents` — check `process_status`, `process_status_source`, `process_status_detail`, and lease expiry. Watch alerts report active-lease registered agents whose PID is dead or mismatched; unknown process identity is treated conservatively as still live.
**Fix**: `§BRAND_BINARY_NAME§ recover-agent <agent-id>` or `§BRAND_BINARY_NAME§ delete agent <id>`.

### Zombie agent process
**Symptom**: A live `§BRAND_BINARY_NAME§ agent` process is not registered in `state.yaml` and can collide with registered agents claiming the same work.
**Diagnosis**: `§BRAND_BINARY_NAME§ get agents --zombies` or `§BRAND_BINARY_NAME§ validate`.
**Fix**: Inspect the PID and stop the stale process. `§BRAND_BINARY_NAME§ validate --skip-process-checks` is only for archived/offline state validation where host process state is irrelevant.

### Provider quota exhausted
**Symptom**: All agents using a provider (e.g. Claude) have stopped. System mode is still RUNNING, sprint still IN_PROGRESS. Signal file `§BRAND_PROJECT_DIRNAME§/provider-quota-exhausted-<provider>` exists.
**Diagnosis**: `ls §BRAND_PROJECT_DIRNAME§/provider-quota-exhausted-*` or check `§BRAND_PROJECT_DIRNAME§/alerts.log` for `PROVIDER QUOTA EXHAUSTED`. `PROVIDER QUOTA SPAWN BLOCKED` means a spawn was attempted while the quota signal was still set; delete the flag file or run `§BRAND_BINARY_NAME§ pause` then `§BRAND_BINARY_NAME§ resume` before spawning again.
**Fix**: `§BRAND_BINARY_NAME§ pause` then `§BRAND_BINARY_NAME§ resume` — pause transitions RUNNING → PAUSED, resume clears quota signals and restarts the sprint. Then restart agents. (`§BRAND_BINARY_NAME§ resume` alone fails because the system is still RUNNING, not PAUSED.)

### Provider unavailable
**Symptom**: Agents for a provider stop before doing useful work, often after startup/session errors such as Codex failing to access `~/.codex/sessions`. Signal file `§BRAND_PROJECT_DIRNAME§/provider-unavailable-<provider>` exists.
**Diagnosis**: `ls §BRAND_PROJECT_DIRNAME§/provider-unavailable-*` or check `§BRAND_PROJECT_DIRNAME§/alerts.log` for `PROVIDER UNAVAILABLE`. `PROVIDER UNAVAILABLE SPAWN BLOCKED` means a spawn was attempted while the provider-unavailable signal was still set. Also inspect `§BRAND_PROJECT_DIRNAME§/agent-outputs/*.err` for provider startup errors.
**Fix**: Repair the provider environment first (for Codex, ensure the agent process can access `~/.codex/sessions`), then run `§BRAND_BINARY_NAME§ pause` and `§BRAND_BINARY_NAME§ resume` to clear provider-unavailable signals before restarting agents.

### Provider audit degraded
**Symptom**: Agent work may complete, but `§BRAND_PROJECT_DIRNAME§/agent-outputs/*.err` or `§BRAND_PROJECT_DIRNAME§/alerts.log` contains `PROVIDER AUDIT DEGRADED`, for example Codex `failed to record rollout items: thread ... not found`.
**Impact**: Treat task state and explicit task outputs as the source of truth. The provider transcript or rollout audit trail may be incomplete for the affected session.
**Diagnosis**: Upgrade or retest the provider CLI first. Then inspect `§BRAND_PROJECT_DIRNAME§/agent-outputs/*.err`, `§BRAND_PROJECT_DIRNAME§/alerts.log`, task state, and blackboard outputs before relying on the session transcript.
**Response**: A single occurrence does not stop workers. A qualifying
same-provider group is classified as `ACKNOWLEDGED_HISTORICAL`, `NEW`, or
`CONTINUING`. Historical-only evidence is `WARNING` and takes no
mode/sprint/trigger/active-response action. New or continuing evidence is a
non-trigger hard `CHECKPOINT` by default: mode remains `RUNNING`, downstream
transition creation pauses, and already-available sprint work may continue.

`HALT` is permitted only when at least one current registration exactly matches
the anomaly provider and every exact match has a degraded health record for the
same agent ID, provider, PID, and registration time. An alias-only match (such
as `codex` evidence versus a `codex-acp` registration), empty/missing identity,
missing health, or a stale/mismatched PID or registration epoch is non-halting
unknown and remains `CHECKPOINT`. `OBSERVABILITY_DEGRADED` therefore does not by
itself trip system mode.

**Recovery**: Review the report, then run exactly `§BRAND_BINARY_NAME§ resume`.
Resume resolves the active `CHECKPOINT` or `HALT` response, records its history
boundary, and clears it; a `HALT` acknowledgement also clears the trigger.
Unchanged qualifying evidence subsequently reports
`ACKNOWLEDGED_HISTORICAL`/`WARNING` rather than checkpointing or halting again.
Raw provider events are not stored in `state.yaml`; inspect
`§BRAND_PROJECT_DIRNAME§/agent-outputs/` for full transcript evidence. If an
older state contains raw provider JSON in anomaly messages, run
`§BRAND_BINARY_NAME§ migrate` to scrub those fields while keeping the anomaly
record. Legacy circuit-breaker state without `current_response`, `response`,
`classification`, or `explanation` remains readable; cleared legacy
`TRIGGERED` history still acts as an acknowledgement boundary.

### Circuit breaker

`§BRAND_BINARY_NAME§ analyze` detects systemic patterns and selects a response:

| Condition | Action |
|-----------|--------|
| Agent crash loop (3× in 5min) | Supervisor stops the agent |
| Blackboard validation fails | All agents pause |
| Submit/merge integration branch conflict | Task set to INTEGRATION_FAILED |
| Unblock-time `--rebase-on` conflict | Task remains BLOCKED with fresh repair metadata |
| Historical provider-audit evidence (`ACKNOWLEDGED_HISTORICAL`) | `WARNING`; report only, no state action |
| New/continuing provider-audit evidence with unknown or non-degraded current health | Non-trigger `CHECKPOINT`; mode stays RUNNING and report is written |
| Provider-audit evidence with exact all-degraded current provider/agent-ID/PID/registration-time proof | `HALT`; circuit breaker triggers and mode becomes CIRCUIT_BREAKER_TRIPPED |
| Other qualifying systemic pattern | `HALT`; circuit breaker triggers and report is written |

## Validation Invariants

`§BRAND_BINARY_NAME§ validate` checks these (among others):
- Tasks in executing states must have `assigned_to`, `worktree`, and valid `lease_expires`
- Tasks in reviewing states must have `reviewing_by`, `review_lease_expires`, and `review_commit`
- Tasks in submitted states must have `review_commit`
- Tasks in rejected states must have `rejection_reason`
- BLOCKED tasks must have `blocked_reason` and `blocked_questions`
- Dependency direction applies to every task, including terminal tasks; dependencies cannot point to downstream role-pairs
- MERGED tasks must not have `worktree`
- No two agents assigned to the same task
- Tasks in initial/draft states cannot have `assigned_to`
