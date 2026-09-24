# Tooling

## Deliverables

### Contract Files (`<project>/contracts/`)

Contracts are versioned with the project:

| File | Purpose |
|------|---------|
| `CORE.md` | Universal rules + mode selection gate |
| `PAIRING_MODE.md` | Human-supervised collaboration (extracted from current contract) |
| `MULTI_AGENT_MODE.md` | Agent-supervised Liza system (new) |

### Global Symlink (`~/.liza/`)

| File | Purpose |
|------|---------|
| `CLAUDE.md` | Symlink → `<project>/contracts/CORE.md` |

**Note:** Update symlink when switching projects: `ln -sf /path/to/project/contracts/CORE.md ~/.claude/CLAUDE.md`

### Go CLI (`liza`)

All system mechanics are provided by the `liza` Go binary (assumed in PATH). See [ADR-0012](../architecture/ADR/0012-go-cli-replaces-bash-scripts.md).

| Command | Purpose |
|---------|---------|
| `liza init "goal" --spec spec` | Initialize `.liza/` for new goal |
| `liza add-task --id X ...` | Add task to blackboard (atomic, with validation) |
| `liza validate [state] [--skip-process-checks] [--repair]` | Schema validation plus live zombie-agent detection; `--repair` clears invalid active ownership before validation |
| `liza tui` | Alarm monitor daemon |
| `liza analyze` | Circuit breaker analysis (human-triggered) |
| `liza sprint-checkpoint` | Create checkpoint and generate sprint summary |
| `liza agent <role> --agent-id x [--cli C] [--goal-id G]` | Agent supervisor (`--cli`: claude, codex, gemini, mistral, kimi; `--goal-id` marks the process for diagnostics) |
| `liza claim-task <task> <agent>` | Claim task with two-phase commit (called by supervisor) |
| `liza submit-for-review <task> [commit-ref]` | Resolve `[commit-ref]` in the task worktree (default `HEAD`), validate it matches pre-rebase worktree HEAD, then set READY_FOR_REVIEW + post-rebase `review_commit` + history |
| `liza submit-verdict <task> <V> [--reason "<reason>" \| --reason-file <path\|->]` | Atomically set APPROVED/REJECTED + review fields + history; `--reason-file -` reads a bounded multiline reason from stdin |
| `liza wt-create <task> [--fresh]` | Create worktree for task |
| `liza wt-merge <task>` | Merge approved worktree (supervisor-executed after APPROVED) |
| `liza wt-delete <task>` | Clean up abandoned/merged worktree |
| `liza reconcile-merged <task> --merge-commit <sha>` | Mark an INTEGRATION_FAILED task as MERGED after verifying external completion |
| `liza update-sprint-metrics` | Recompute sprint.metrics from task state |
| `liza clear-stale-review-claims` | Clear expired review claims |
| `liza recover-task <task-id>` | Recover by task ID while preserving recoverable worktree/branch state |
| `liza recover-agent <agent-id>` | Recover by agent ID (release claim + worktree + delete agent) |
| `liza release-claim <task> [--role R]` | Release claim on task or review |
| `liza pause` / `liza resume` | Pause/resume system |
| `liza stop` / `liza start` | Stop/start system |
| `liza status` | Show system status |
| `liza get` | Get blackboard data |
| `liza mark-blocked` | Mark task as blocked |
| `liza assess-blocked` | Reconcile canonical blocker metadata after partial repair, or record a note-only assessment |
| `liza supersede-task` | Supersede a task; no-replacement cleanup requires an operator-provided `--recoverability-command` audit string |
| `liza retarget-dependency` | Replace one direct dependency edge on a non-terminal task through an orchestrator-only validated repair |
| `liza narrow-inherited-dependencies` | Apply `inherit_inputs` to a MERGED producer after generation and remove unselected phase-gate edges from its initial-status children (ADR-0138) |
| `liza repair-superseded-dependencies` | Remove all illegal downstream direct dependencies from one SUPERSEDED task through an orchestrator-only audited repair |
| `liza delete agent\|task` | Delete agent or task entry |

Locking is internal to the binary — no external `flock` wrapper needed.

### Optional Project Files

| Path | Purpose | Used By |
|------|---------|---------|
| `integration-test.sh` | Integration test suite | `liza wt-merge` runs if present after merge |

If `integration-test.sh` exists in the project, `liza wt-merge` executes it after successful merge. On failure, merge is rolled back and task marked INTEGRATION_FAILED.

### Templates (`<project>/templates/`)

| File | Purpose |
|------|---------|
| `vision-template.md` | Template for goal-level vision document |
| `README.md` | Instructions for using templates |

**Note:** ADR template is at `specs/architecture/ADR/TEMPLATE.md` (co-located with ADRs for discoverability).

### Project Runtime (per project)

| Path | Purpose |
|------|---------|
| `.liza/state.yaml` | Goal, tasks, assignments, leases |
| `.liza/log.yaml` | Append-only activity log |
| `.liza/archive/` | Archived terminal-state tasks |
| `.worktrees/` | Git worktrees, one per active task |

### CLI Exit Codes

The `liza` CLI uses a consistent exit code taxonomy:

| Code | Meaning | Recovery |
|------|---------|----------|
| 0 | Success | None needed |
| 1 | Validation error (precondition failed) | Fix input, retry |
| 2 | Lock acquisition failed | Retry with backoff |
| 3 | Git operation failed | Check git state, resolve conflicts |
| 4 | State inconsistency (invariant violation) | Manual inspection required |

**Per-Command Specifics:**

| Command | Exit 1 | Exit 3 | Exit 4 |
|---------|--------|--------|--------|
| `liza wt-create` | Task not in an executing state | Worktree creation failed | — |
| `liza wt-merge` | Task not APPROVED, SHA mismatch | Merge conflict (detected via merge-tree) | — |
| `liza validate` | Schema violation found | — | — |

**Recovery Procedures:**
- **Exit 2 (lock failed):** Another process holds lock. Wait 1-5s, retry up to 3 times.
- **Exit 3 (git failed):** Run `git status` in affected worktree; resolve conflicts or stale state.
- **Exit 4 (inconsistency):** Stop all agents. Human must inspect `.liza/state.yaml` and fix manually.

---

## Agent-Blackboard Interface

### How Agents Execute Blackboard Operations

Agents have shell access via Claude Code's bash tool. Blackboard operations are `liza` CLI calls.

**Task Claiming:** The supervisor (`liza agent`) claims tasks using `liza claim-task` which implements a two-phase commit pattern to prevent invalid intermediate states:

```
Phase 1: Validate under lock (no state mutation)
  - Verify task exists and is READY
  - Verify dependencies are satisfied (all depends_on tasks MERGED)
  - Verify agent is available

Phase 2: Create worktree (outside lock)
  - Create git worktree at .worktrees/task-N
  - Branch from integration branch or main

Phase 3: Re-validate and commit under lock
  - Re-check all conditions (state may have changed)
  - Set IMPLEMENTING status with all required fields atomically
  - On validation failure: delete worktree and exit

Cleanup: If commit fails, worktree is deleted to maintain consistency
```

This pattern ensures no task is ever in IMPLEMENTING state without a valid worktree.

**State Updates:** Agents use dedicated CLI commands for state transitions. The CLI handles locking and validation internally. Agents must never edit `.liza/state.yaml` directly; unsupported mutations are escalated as orchestrator repair requests or blockers.

```bash
# Request review (atomic)
liza submit-for-review task-3 HEAD
# Requires: HEAD resolves to .worktrees/task-3 HEAD (pre-rebase)
# Sets READY_FOR_REVIEW + post-rebase review_commit + history

# Add task (Planner operation)
liza add-task \
  --id task-3 \
  --desc "Add retry decorator to UserAPI.get_user()" \
  --spec specs/retry-logic.md \
  --done "UserAPI.get_user() retries 3x on 5xx errors with exponential backoff" \
  --scope "src/api/user.py, tests/test_user_api.py" \
  --priority 1 \
  --depends "task-1,task-2"

# Read current state
liza get

# System status
liza status
```

### Command Availability

CLI commands are divided into agent-callable and supervisor-only:

**Agent-Callable Commands:**

| Command | Called By | Purpose |
|---------|-----------|---------|
| `liza add-task` | Planner | Add task atomically (scoped-validates the new task; warns if full state remains degraded) |
| `liza validate` | All agents (optional) | Verify state before/after operations |
| `liza get` | All agents | Read blackboard data |
| `liza submit-for-review` | Coder | Request review (atomic state transition) |
| `liza submit-verdict` | Code Reviewer | Approve/reject (atomic state transition) |
| `liza mark-blocked` | All doer roles | Mark task as blocked |
| `liza assess-blocked` | Orchestrator | Reconcile canonical blocker metadata after partial repair, or record a note-only assessment |
| `liza retarget-dependency` | Orchestrator | Replace one direct edge on a non-terminal task and validate the candidate state |
| `liza narrow-inherited-dependencies` | Orchestrator | Persist post-hoc `inherit_inputs` on a producer and narrow its initial-status children, validating the candidate state |
| `liza repair-superseded-dependencies` | Orchestrator | Atomically remove illegal downstream edges from a SUPERSEDED task with full validation and audit history |
| `liza wt-merge` | Supervisor | Merge after Code Reviewer approves |
| `liza wt-delete` | Planner | Clean up abandoned tasks |

**Supervisor-Only Commands:**

| Command | Purpose |
|---------|---------|
| `liza agent` | Agent lifecycle management (start, restart, backoff) |
| `liza claim-task` | Two-phase task claiming with worktree creation |
| `liza wt-create` | Create worktree (called internally by `liza claim-task`) |

### Supervisor-Only Operations

**Terminology clarification:** "Supervisor" refers to the Go process loop within each `liza agent` instance—not a central singleton process. Each agent role runs in its own terminal with its own supervisor loop:

```
Terminal 1                    Terminal 2                    Terminal 3
┌─────────────────────┐      ┌─────────────────────┐      ┌─────────────────────┐
│ liza agent planner  │      │ liza agent coder    │      │ liza agent          │
│ --agent-id planner-1│      │ --agent-id coder-1  │      │ code-reviewer       │
│                     │      │                     │      │ --agent-id cr-1     │
│  while true:        │      │  while true:        │      │  while true:        │
│    wait_for_work()  │      │    claim_task()     │      │    claim_review()   │
│    claude -p "..."  │      │    claude -p "..."  │      │    claude -p "..."  │
│    handle_exit()    │      │    handle_exit()    │      │    handle_exit()    │
└─────────────────────┘      └─────────────────────┘      └─────────────────────┘
```

When specs say "supervisor claims task before spawning agent," this means the Go loop claims the task before invoking `claude`—all within the same `liza agent` process. The `claude` call blocks until the session ends.

The supervisor handles:
- Starting/restarting the Claude Code process
- Claiming tasks before spawning Coders (via `liza claim-task`)
- Assigning reviews before spawning Code Reviewers
- Detecting exit codes
- Respecting system mode (`config.mode: PAUSED`, `STOPPED`) and sprint status (`CHECKPOINT`)
- Backoff timing on crashes

**Signal handling:** The supervisor respects SIGINT/SIGTERM via `signal.NotifyContext`. On signal, the context is cancelled and the supervisor checks `ctx.Err()` at the top of its loop, exiting gracefully. Agent unregistration (`unregisterAgent`) atomically releases any active task claim — tasks return to READY (coder) or READY_FOR_REVIEW (reviewer) — before deleting the agent entry.

Agents do not call supervisor-only scripts or manage their own lifecycle.

---

## Startup Sequence

### Bootstrap (Human, One-Time)

Before any agent starts:

1. **Create vision document:**
   ```bash
   mkdir -p specs
   cp templates/vision-template.md specs/vision.md
   # Edit specs/vision.md with goal context
   ```

2. **Initialize blackboard:**
   ```bash
   cd /path/to/project
   liza init "Implement retry logic for all API calls"
   ```

3. **Write/verify specs:**
   - Ensure `specs/` contains requirements for the goal
   - Ensure `REPOSITORY.md` describes project structure
   - Review and approve spec content

4. **Start watcher (optional but recommended):**
   ```bash
   # Dedicated terminal
   liza tui
   ```

### Agent Startup (Human Triggers, Agents Run)

Start agents in separate terminals. Each agent requires a unique `LIZA_AGENT_ID`:

```bash
# Terminal 1: Planner
liza agent planner --agent-id planner-1

# Terminal 2: Coder (after planner has created tasks)
liza agent coder --agent-id coder-1

# Terminal 3: Code Reviewer (after coder starts requesting reviews)
liza agent code-reviewer --agent-id code-reviewer-1
```

**Multiple agents of the same role are supported.** Run additional agents in separate terminals:

```bash
# Terminal 4: Second coder (processes tasks in parallel)
liza agent coder --agent-id coder-2

# Terminal 5: Second reviewer (processes reviews in parallel)
liza agent code-reviewer --agent-id code-reviewer-2
```

See [Agent Identity Protocol](../architecture/roles.md#agent-identity-protocol) for identity validation and collision prevention.

### Startup Order

| Phase | Who Starts | Prerequisites |
|-------|------------|---------------|
| 1. Bootstrap | Human | Project exists, git initialized |
| 2. Planner | Human | Blackboard initialized, specs exist |
| 3. Coder(s) | Human | Planner has finalized tasks (READY) |
| 4. Code Reviewer | Human | Coder has requested review (READY_FOR_REVIEW) |

Agents can be started earlier—they'll wait/exit if no work available.

### Agent Session Start

When supervisor starts Claude Code, the agent:

1. Reads `CLAUDE.md` → `CORE.md` (contract)
2. Sees mode selection prompt
3. States: `"Mode: Liza [Role]"`
4. Follows initialization sequence from session initialization

The supervisor passes context via the initial prompt, including structured task assignment sections:

```bash
# Coder prompt includes "=== ASSIGNED TASK ===" section with:
# - TASK ID, WORKTREE (absolute path), DESCRIPTION, DONE WHEN, SCOPE, INSTRUCTIONS

# Code Reviewer prompt includes "=== REVIEW TASK ===" section with:
# - TASK ID, WORKTREE, COMMIT TO REVIEW, AUTHOR, DESCRIPTION, DONE WHEN, INSTRUCTIONS

# Planner prompt includes "=== PLANNING CONTEXT ===" section with:
# - WAKE TRIGGER: INITIAL_PLANNING | BLOCKED_TASKS | HYPOTHESIS_EXHAUSTED | IMMEDIATE_DISCOVERY | HUMAN_NOTE
# - SPRINT STATE: total tasks, merged, in_progress, unclaimed, blocked, integration_failed, hypothesis_exhausted, immediate_discoveries
# - INSTRUCTIONS: trigger-specific guidance (varies by wake trigger)
```

See `liza agent` source (Go) for exact prompt-building logic per role.

Exact CLI syntax depends on Claude Code version. The contract handles mode selection regardless of invocation method.

---

## CLI Command Reference

All commands are subcommands of the `liza` binary. Run `liza help` or `liza <command> --help` for full usage.

### Key Commands

**liza init** — Initialize blackboard for new goal
```bash
liza init "Goal description" --spec specs/vision.md
```

**liza add-task** — Add task to blackboard (Planner)
```bash
liza add-task --id TASK_ID --desc DESCRIPTION --spec SPEC_REF \
  --done DONE_WHEN --scope SCOPE [--priority N] [--depends "task-a,task-b"]
# Atomically adds task, updates sprint.scope.planned and goal.alignment_history,
# validates the new task, and warns if unrelated state corruption keeps full
# validation degraded.
```

Task creation records `Added task <id>` in alignment history. The complete
description remains on the task and in the activity log, so description length
does not consume the alignment summary's 4096-byte budget. Batch creation uses
the same behavior for each task.

**liza validate** — Validate blackboard state
```bash
liza validate [state.yaml]
liza validate --skip-process-checks   # Offline/archive validation only
liza validate --repair                # Clear invalid active ownership, then validate
# Returns "VALID" or exits non-zero with the issue description
```

By default, `liza validate` is a live-system assertion: after schema checks pass,
it scans local processes for `liza agent` supervisors that belong to the current
goal/project but whose PID is absent from `state.yaml`. Use
`--skip-process-checks` only when validating an archived state file or running in
an environment where host process state is intentionally irrelevant. Live process
scanning currently requires Linux procfs; on hosts without procfs, validation
emits a warning and skips the live-process check.

`--repair` is intentionally narrow: it clears invalid active review ownership
and invalid or dead active doer ownership, then re-runs validation. Doer repair
requires process checks because a live doer may still be writing in its worktree:
if the assigned PID is live, repair refuses with recovery commands. When
`--skip-process-checks` is set, doer repair is skipped and reviewer repair
remains state-only.

Doer repair leaves the physical worktree on disk for operator inspection. A
later fresh claim of that task may remove stale worktree resources before
recreating them.

**liza tui** — Monitor blackboard and alert
```bash
liza tui
# Runs continuously, alerts on: expired leases, blocked tasks, review loops, etc.
```

**liza analyze** — Circuit breaker analysis
```bash
liza analyze
# Detects systemic patterns, generates report, sets sprint.status: CHECKPOINT if triggered
```

**liza sprint-checkpoint** — Create checkpoint
```bash
liza sprint-checkpoint
# Sets sprint.status: CHECKPOINT and generates sprint summary
```

**liza agent** — Agent supervisor
```bash
liza agent coder --agent-id coder-1
# Runs agent in loop, handles exit codes, respects config.mode and sprint.status
```

Agents spawned by Liza include `--goal-id` in their command line so live-process
diagnostics can display the goal marker. When a project root is supplied,
process cwd is authoritative: a matching cwd verifies a current-project
process, a different readable cwd verifies a foreign process, and unreadable
cwd leaves scope unknown. Goal ID cannot override unreadable or conflicting cwd
evidence. When no project root is supplied, goal matching remains the fallback
scope.

**liza claim-task** — Claim task (supervisor-only)
```bash
liza claim-task task-3 coder-1
# Two-phase commit: validate → create worktree → re-validate and commit
```

**liza submit-for-review** — Request review (Coder)
```bash
liza submit-for-review task-3 HEAD
# Requires: HEAD resolves to .worktrees/task-3 HEAD (pre-rebase)
# Sets READY_FOR_REVIEW + post-rebase review_commit + history
```
With `--json`, missing-test TDD failures include `error.details` with
`base_ref`, `head_ref`, changed files considered, matched test files, and matcher
patterns. Non-conflict rebase failures include bounded `error.details` with the
git command, rebase refs, stdout/stderr excerpt, and recovery hint.
Submit-time integration failures also persist `task.integration_failure` and an
`integration_failed` history diagnostic with the operation, reason, and recovery
hint.

**liza submit-verdict** — Submit review verdict (Code Reviewer)
```bash
liza submit-verdict task-3 APPROVED
liza submit-verdict task-3 REJECTED --reason "Missing error handling for 429 responses"
liza submit-verdict task-3 REJECTED --reason-file - <<'VERDICT'
# Blockers
Missing error handling for 429 responses.
VERDICT
```

**liza wt-create** — Create worktree (supervisor-only)
```bash
liza wt-create task-3 [--fresh]
# Creates .worktrees/task-3 from integration branch
# --fresh: Delete existing worktree before creating (for reassignment to different coder)
```

**liza wt-merge** — Merge worktree (supervisor-executed after APPROVED)
```bash
liza wt-merge task-3
# Task must be APPROVED
# Performs working-tree-less merge using git merge-tree + commit-tree + update-ref
# Working tree files transiently synced for integration tests, then restored
# Multiple reviewers can merge concurrently without race conditions
```
Merge conflicts persist a structured diagnostic with the operation, reason, and
recovery hint.
Tasks moved to `INTEGRATION_FAILED` persist the same diagnostic on
`task.integration_failure` and the `integration_failed` history entry.
For older `INTEGRATION_FAILED` tasks without structured diagnostics, `liza get
tasks --json` and watch alerts synthesize a conservative recovery diagnostic
from the status and worktree metadata.

**liza reconcile-merged** — Reconcile externally completed integration failures
```bash
liza reconcile-merged task-3 --merge-commit abc123 --pr-url https://github.com/org/repo/pull/17 --reason "PR merged externally"
# Task must be INTEGRATION_FAILED
# merge-commit must resolve locally
# Clears stale worktree/claim metadata and records a MERGED history event
```

**liza wt-delete** — Delete worktree
```bash
liza wt-delete task-3
# Removes worktree and branch for abandoned/superseded tasks
```

**liza supersede-task** — Supersede a task
```bash
liza supersede-task task-3 task-4,task-5 --reason "Split into replacements"
liza supersede-task task-3 --reason "Work already merged" --recoverability-command "liza recover-task task-3"
```

Before committing the transition, supersession removes the retiring task's own
illegal downstream direct dependencies, records their IDs in the `superseded`
history event, rewrites active consumers to the declared replacements, and
validates the candidate state. Legal historical dependencies are retained. A
validation failure rolls back the transition and every dependency rewrite.

When no replacements are provided, supersession is the destructive cleanup path:
no successor will preserve the old task branch. The command therefore requires a
single-line `--recoverability-command` audit string. Liza records the string and
a pre-cleanup snapshot in the `superseded` history entry, but does not execute
the command. Do not include secrets; known environment secret values are masked
before persistence.

```yaml
history:
  - event: superseded
    recoverability_command: liza recover-task task-3
    pre_supersession:
      status: BLOCKED
      branch: task/task-3
      branch_exists: true
      branch_head: <commit-sha>
      worktree: .worktrees/task-3
      worktree_path: <absolute-path>
      worktree_exists: true
      worktree_head: <commit-sha>
      worktree_status: ""
      base_commit: <commit-sha>
```

In Go, these audit fields live in `TaskHistoryEntry.Extra`; the state YAML
serializes that map inline.

**liza repair-superseded-dependencies** — Repair terminal dependency metadata
```bash
liza repair-superseded-dependencies <task-id> --reason <reason>
```

This orchestrator-only command accepts one `SUPERSEDED` task with illegal
downstream direct dependencies. In one locked transaction it removes all
illegal edges, retains legal dependencies and all other terminal/replacement
metadata, appends `dependencies_rewritten` history with the reason, caller, and
removed/retained IDs, then validates the full candidate state before commit.
Non-`SUPERSEDED` or already-valid targets and candidates that remain invalid are
rejected without mutation. The activity log is appended after commit; a log
failure is returned as a warning. Never repair this metadata by editing
`.liza/state.yaml` directly.

**liza recover-task** — Recover by task ID
```bash
liza recover-task task-1                    # Release claims + preserve/reattach coherent worktree/branch + recover agent
liza recover-task task-1 --fresh            # Explicitly discard worktree/branch and create a fresh worktree from integration
liza recover-task task-1 --fresh --force    # Required when a claimant PID is alive and destructive reset is intentional
liza recover-task task-1 --force            # Also cleans git artifacts when task is not in state
# Idempotent. Refuses if claiming agent's PID is alive unless --force is set.
```

Default recovery preserves work when it can prove the substrate is coherent:
the task branch must exist, any worktree must be healthy and clean, and submitted
or reviewing candidates must have `review_commit == worktree HEAD`. If only the
branch exists, recovery reattaches a worktree and performs the same checks. Dirty
worktrees fail closed because ambiguous unsubmitted work should not be
redispatched.

`--fresh` is the explicit destructive path. It removes the task worktree/branch,
creates a fresh worktree from the integration branch, clears active claim and
review metadata, and records `task_recovered_fresh`.

Before deleting artifacts, `--fresh` revalidates the task state and integration
branch. If artifact cleanup fails, recovery returns an error and leaves task
state unchanged because the old worktree/branch may still exist. If fresh
worktree creation still fails after successful artifact cleanup, it must commit
a truthful repair state instead of leaving stale pointers: status becomes
`BLOCKED`, claims are cleared, worktree/base/review metadata is cleared, and
`blocked_reason` records the failed fresh creation. `unblock-task` remains the
only guarded path out of `BLOCKED`; pending direct dependencies may still hold
the restored task.

`--fresh` postconditions by status:

| Current status | Allowed | Postcondition |
| --- | --- | --- |
| Initial status (`DRAFT_CODE` / role-pair equivalent) | Yes | Status remains initial; attempt/review/claim metadata cleared; fresh worktree recorded. |
| Executing (`IMPLEMENTING_CODE` / role-pair equivalent) | Yes | Status resets to initial; claim and attempt metadata cleared; fresh worktree recorded. |
| Submitted/reviewing/approved/partial-review statuses | Yes | Status resets to initial; `review_commit`, approvals, merge and output metadata cleared; fresh worktree recorded. |
| `CODE_REJECTED` / role-pair equivalent | Yes | Status resets to initial; rejection and attempt metadata cleared; fresh worktree recorded. |
| `INTEGRATION_FAILED` | Yes | Status resets to initial; repair and integration-failure metadata cleared; fresh worktree recorded. |
| `BLOCKED` | Yes | Status remains `BLOCKED`; claim/review substrate is repaired, but `blocked_reason` and `blocked_questions` are preserved. Use `liza unblock-task` to leave `BLOCKED`; valid pending dependencies may still keep the restored task dependency-held. |
| Terminal statuses (`MERGED`, `ABANDONED`, `SUPERSEDED`) | No | Rejected. |

For tasks absent from state, only `--force` performs git-only cleanup of orphaned
task worktree/branch artifacts; it does not create or mutate blackboard state.

**liza recover-agent** — Recover by agent ID
```bash
liza recover-agent coder-1                  # Release claim + remove worktree + delete agent
liza recover-agent coder-1 --cli claude     # Same + respawn agent
liza recover-agent coder-1 --force          # Override PID liveness check
# Auto-detects role. Idempotent (no error if agent already gone).
```

**liza pause / liza resume** — Pause/resume system
```bash
liza pause    # Sets config.mode: PAUSED — agents exit gracefully
liza resume   # Sets config.mode: RUNNING — supervisors restart agents
```

**liza stop / liza start** — Stop/start system
```bash
liza stop     # Sets config.mode: STOPPED — all agents terminate
liza start    # Sets config.mode: RUNNING — supervisors restart agents
```

**liza status** — Show system status
```bash
liza status   # Summary of goal, sprint, agents, tasks
```

**liza get** — Read blackboard data
```bash
liza get      # Print current state
```

For a BLOCKED task, `liza get <task-id> --json` is the canonical current view
of `depends_on`, `blocked_reason`, `blocked_questions`, and `repair_request`.
Task history is append-only audit evidence, not a second current view.

**liza assess-blocked** — Reconcile canonical blocker metadata (Orchestrator)

After a partial repair, replace the current reason and one to three questions
atomically:

```bash
liza assess-blocked <task-id> --reason "<current blocker>" \
  --question "<current question>" [--question "<another current question>"] \
  --agent-id <orchestrator-id> --json
```

Without replacement input, structured assessment clears the previous repair
request. A command-style replacement is all-or-nothing: provide
`--repair-operation`, `--repair-target`, `--repair-command`, one or more
`--repair-evidence` values, and one or more `--repair-validation` values. A
command-free declarative `apply-dependency-repair` replacement, including
`dependency_updates`, instead uses a complete JSON file through
`--repair-request-file <path>`; it cannot be combined with individual
`--repair-*` fields. The file and field paths are mutually exclusive.

`liza assess-blocked <task-id> --note "<assessment>"` remains note-only,
history-only compatibility when the canonical metadata is already current.
Either form accepts `--awaits <id>[,<id>]` to name all the existing unfinished
tasks the hold waits for: it then wakes once they have all merged or one fails,
instead of on generated descendants or the covered dependencies' partial
progress, while other dependency, blocker, human-note and the task's own changes
still wake it. An assessment without `--awaits` keeps the set minus merged tasks
within the same blocked episode; `--clear-awaits` drops it. See
[Blocked-Assessment Idempotency](../protocols/blocked-assessment-idempotency.md#awaited-set).
After a full repair, use
`liza unblock-task <task-id> --reason "<verified resolution>"`; this guarded
transition clears canonical blocker metadata. It returns the task to its
role-pair initial status, but claimability still depends on direct dependencies:
valid pending dependencies keep it dependency-held and unclaimable until every direct dependency is `MERGED`.

**liza handoff** — Record a context-exhaustion handoff through the CLI
```bash
liza handoff <task-id> <summary> <next-action>
```

The command records the handoff without requiring or permitting a direct
`state.yaml` edit.

---

## Human Override Protocol

Human owns the intent and acts as observer and circuit-breaker, not approver.

### Observation Channels

| Channel | Purpose |
|---------|---------|
| Terminals | Watch agent output in real-time |
| `.liza/state.yaml` | Current assignments and states |
| `.liza/log.yaml` | Activity history (skimmable) |
| `liza tui` output | Alarms for attention-needed conditions |

### Override Actions

| Action | Mechanism | Effect |
|--------|-----------|--------|
| Kill agent | Ctrl+C / kill | Agent releases task claims on exit; supervisor restarts and re-reads blackboard |
| Pause all | `liza pause` | Sets `config.mode: PAUSED`; agents exit gracefully (code 42), supervisors wait |
| Resume | `liza resume` | Sets `config.mode: RUNNING`; supervisors restart agents |
| Force replan | `liza mark-blocked <task> --reason "human override"` | Planner escalation triggered |
| Inject task | `liza add-task --id X ...` (as READY) | New task available for claim |
| Abort goal | `liza stop` | Sets `config.mode: STOPPED`; all agents terminate, supervisors stop |

### Human Communication

Human can leave notes in blackboard:

```yaml
human_notes:
  - timestamp: 2025-01-17T15:00:00Z
    message: "Task-3 approach looks wrong. Consider existing retry util in src/utils/retry.py"
    for: task-3
```

Agents must read `human_notes` relevant to their task before starting/resuming work.

---

## Alarm Conditions

`liza tui` monitors and alerts on:

| Condition | Threshold | Alert |
|-----------|-----------|-------|
| Expired coder lease | lease_expires in past | `⚠️ LEASE EXPIRED: {agent} on {task}` |
| Expired review lease | review_lease_expires in past | `⚠️ REVIEW LEASE EXPIRED: {code_reviewer} on {task} — review can be reclaimed` |
| Running task without live process | Executing or reviewing task owner PID is missing or stopped | `🚨 DEAD AGENT PROCESS: {task} — status {status} has {owner_kind} {agent} but no live process (pid {pid})` |
| Invalid active owner row | Executing or reviewing task owner row has the wrong role, status, or current_task; or an active agent row points at a task that does not point back | `🚨 INVALID AGENT OWNERSHIP: {task} — status {status} has {owner_kind} {agent} with invalid agent row: {reason}` |
| Live supervisor missing from state | `liza validate` or `liza get agents --zombies` finds a current-goal `liza agent` PID absent from `state.yaml` | `zombie liza agent process detected: pid {pid} role {role}` |
| Orchestrator missing | Goal IN_PROGRESS, mode RUNNING, and no orchestrator-type agent holding effective ownership (fresh lease with a heartbeat) for 60s; once per absence episode per running watcher | `🚨 ORCHESTRATOR MISSING: no live {role} agent while goal is IN_PROGRESS (absent since {time}[; last row: {agent} {ownership}]) … start one with {agent <role> command}` |
| Task blocked | Any | `⚠️ BLOCKED: {task} — {reason}` |
| Orphaned rejected | Role pair's rejected status, assignee not WORKING, verdict older than 2 min (a missing verdict time grants no grace) | `🚨 ORPHANED REJECTED: {task} — assigned to {agent} but agent is {status} (no rework 2m+ after verdict)` |
| Same task reassigned | 2nd coder | `⚠️ REASSIGNED: {task} — hypothesis exhaustion risk` |
| Review cycle count | ≥5 (cliff) | `🚨 REVIEW LOOP: {task} — {count} cycles (at cliff)` |
| Integration failure | Any | `🚨 INTEGRATION FAILED: {task}` |
| Hypothesis exhaustion | 2 coders failed | `🚨 HYPOTHESIS EXHAUSTION: {task} — requires rescope` |
| Approaching limits | 8/10 iter, 3/5 review | `⚠️ APPROACHING LIMIT: {task} — {metric}` |
| Goal stalled | No task history entry for 30, 60, 120, 240… min (restarts at 30 after progress) | `⚠️ STALLED: no task progress for {minutes} minutes` |
| Stale draft | DRAFT >30min | `⚠️ STALE DRAFT: {task} — created {age}min ago (Planner crash?)` |
| Immediate discovery | urgency=immediate, not converted | `🚨 IMMEDIATE DISCOVERY: {id} — {desc} (Planner should wake)` |
| Blackboard invalid | Validation fails | `🚨 INVALID STATE: {error}` |
| Checkpoint stale | >30min/2h/8h | `⚠️/🚨 CHECKPOINT STALE/STUCK: waiting for human` |
| PAUSE stale | >30min/2h | `⚠️/🚨 STALE PAUSE/FORGOTTEN: PAUSE file exists for {age}min` |

### Alert Output

Alerts write to:
- stderr (visible in watch terminal)
- `.liza/alerts.log` (persistent)

Each condition is written once per episode, not per check: a repeat is suppressed while the condition stays active, and a condition that resolves and recurs alerts again. The episode is the condition's identity — usually the message; for LEASE EXPIRED the lease, for HYPOTHESIS EXHAUSTION the failed-by set, for STALLED the stall and its escalation step, for ORPHANED REJECTED the rejection. INVALID STATE reports only the first validation error, so its alerts clear only on a clean validation. BLOCKED is written by both `mark-blocked` and the watcher; they share `alerts.log.once`, a ledger of blocked-episode keys (task, blocked history time, message), so one episode gives one line whichever writes first.

Optional: desktop notification via `notify-send` if available.

## Related Documents

- [Blackboard Schema](../architecture/blackboard-schema.md) — state.yaml structure
- [State Machines](../architecture/state-machines.md) — exit codes, state transitions
- [Phases](phases.md) — implementation sequence
