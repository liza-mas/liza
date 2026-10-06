# `liza_await_resubmission` MCP Tool

> **Historical implementation plan (superseded).** The MCP surface described below
> was removed. The current `await-resubmission` CLI treats `--timeout-seconds` as the total
> wait allowance (default 1,800 seconds, also a hard ceiling), caps one foreground call
> at 540 seconds for registered Claude Code (100 seconds for other tools), and derives
> the remaining share from task history rather than from a
> value the caller carries between calls: retries re-run the identical command, get
> `POLL` while budget remains without releasing review ownership, and `TIMEOUT` once it
> is exhausted. A system halt returns `PAUSED` with stop/resume guidance. Historical MCP names,
> `MCP_TIMEOUT`, and direct 1,500-second blocking behavior below are retained only as
> implementation provenance.

## Context

Mirror of `liza_await_verdict` for the reviewer side. After issuing a non-terminal
rejection, a reviewer agent currently exits its session, and a new session must be
spawned to re-review the doer's resubmission. This costs ~47s per cold restart and
loses the reviewer's accumulated context about the code under review.

`liza_await_resubmission` keeps the reviewer session alive after a rejection verdict.
The reviewer retains logical ownership of the review (`ReviewingBy` stays set),
transitions to WAITING status, and blocks until the doer resubmits. On resubmission,
the tool atomically reclaims the task for review so the reviewer continues in the
same session. No other reviewer can race on the resubmission.

## Critical Files

| File | Action |
|------|--------|
| `internal/ops/await_resubmission.go` | **Create** — core blocking logic |
| `internal/models/task.go` | **Edit** — add ReviewingBy guard to `isClaimablePipeline` |
| `internal/mcp/handlers_mutation.go` | **Edit** — add MCP handler |
| `internal/mcp/server_registration.go` | **Edit** — register tool |
| `internal/embedded/pipeline.yaml` | **Edit** — add operation to reviewer roles |
| `internal/pipeline/testdata/valid-phase2-full.yaml` | **Edit** — add operation to test fixture |
| `internal/prompts/templates/blocks/reviewer_tools.tmpl` | **Edit** — add tool to prompt listings |
| `internal/prompts/templates/blocks/reviewer_state_transitions.tmpl` | **Edit** — add await flow guidance |
| `internal/embedded/claude-settings.json` | **Edit** — add `mcp__liza__liza_await_resubmission` to `allow` array |
| `internal/ops/clear_stale_review_claims.go` | **Edit** — extend to clear stale ReviewingBy on non-REVIEWING tasks |
| `internal/agent/heartbeat.go` | **Verify** — supervisor heartbeat keeps lease alive during block |

## Architectural Assumptions

- **Heartbeat continuity**: Same assumption as `await_verdict` — the supervisor
  process stays alive while the agent is blocked on the MCP tool, so the supervisor
  heartbeat continues and the agent lease doesn't expire.
- **No submit_for_review changes**: `SubmitForReview` does not touch `ReviewingBy` or
  `ReviewLeaseExpires`. ReviewingBy set by `await_resubmission` persists through the
  doer's resubmission cycle (REJECTED → IMPLEMENTING → SUBMITTED).
- **No submit_verdict changes**: `submit_verdict` clears ReviewingBy and releases the
  agent on every verdict. `await_resubmission` **re-acquires** ReviewingBy immediately
  after, mirroring how `await_verdict` re-acquires CurrentTask after
  `submit_for_review` clears it. The gap between the two sequential MCP calls is
  safe: the task is in REJECTED status, and reviewers only claim
  SUBMITTED/PARTIALLY_APPROVED tasks — no race is possible.
- **ClearStaleReviewClaims extended**: The existing stale claims clearer only operates
  on tasks in REVIEWING/REVIEWING_2 states. `await_resubmission` sets ReviewingBy on
  tasks in REJECTED/IMPLEMENTING/SUBMITTED status — a crash during the await phase
  would leave a stale ReviewingBy that nothing cleans up, permanently blocking
  reviewer claims (via the new `isClaimablePipeline` guard). **Fix**: extend
  `ClearStaleReviewClaims` to also scan for expired `ReviewLeaseExpires` on ANY task
  with `ReviewingBy != nil`, regardless of status. See step 2b.
- **Quorum safety**: Only one reviewer at a time can await resubmission for a given
  task. `ReviewingBy` is a single `*string` — structurally enforces single-reviewer
  ownership. In quorum flows, reviews are sequential: if Reviewer1 approves (→
  PARTIALLY_APPROVED) and Reviewer2 rejects (→ REJECTED, clears all approvals), only
  Reviewer2 would call `await_resubmission`. No double-await conflict is possible.
- **Historical MCP timeout (superseded)**: The original MCP tool defaulted to 1500s
  (25 min) within Claude Code's 30-minute `MCP_TIMEOUT`. This is not the current CLI
  invocation contract; see the supersession note above.
- **Budget gate not needed**: If the task is in REJECTED status, `submit_verdict`
  did NOT escalate (NewAttempt or Blocked), which means the doer CAN iterate. The
  doer's `await_verdict` budget gate checks the same limits and won't trigger either
  (both evaluate `classifyLimitEscalation` with the same post-rejection values). The
  precondition "task in rejected status" implicitly validates non-finality.

## Prerequisite: ReviewingBy guard in isClaimablePipeline

Currently, `isClaimablePipeline` for the reviewer role checks task status (SUBMITTED
or PARTIALLY_APPROVED) and ReviewCommit, but does not check ReviewingBy. This is safe
today because ReviewingBy is always nil when a task reaches these claimable statuses.

With `await_resubmission`, ReviewingBy is set while the task transitions through
REJECTED → IMPLEMENTING → SUBMITTED. When the task reaches SUBMITTED, another
reviewer could claim it via `ClaimReviewerTask` because `isClaimablePipeline` doesn't
check ReviewingBy.

**Fix** (`internal/models/task.go`, `isClaimablePipeline`, reviewer case):

```go
case reviewerRole:
    if t.ReviewCommit == nil {
        return false
    }
    if t.ReviewingBy != nil {
        return false  // another reviewer is awaiting resubmission
    }
    // ... existing status checks unchanged
```

This is defense-in-depth — existing flows are unaffected because ReviewingBy is
always nil for claimable tasks in the current system. For quorum flows, it's correct:
the awaiting reviewer reclaims first, reviews, then clears ReviewingBy via
`submit_verdict`, allowing the second reviewer to claim.

## Reuse

- `bb.WatchForChanges()` from `internal/db/watcher.go` — fsnotify setup, debounce
- `waitForWorkEventDriven` pattern from `internal/agent/waitforwork.go` — event loop
  with abort ticker, deadline, fallback to polling
- `resolver` status methods from `internal/pipeline/resolver.go` — role-pair status
  resolution
- `BuildPipelineTransitions` from `internal/ops/pipeline_ops.go` — transition map for
  reclaim
- `operationChecker` from `internal/mcp/middleware.go` — role authorization
- `requireString` / `textResult` from `internal/mcp/handlers_mutation.go` — parameter
  extraction / response formatting
- `MigrateOperations` from `internal/pipeline/migrate.go` — auto-propagation to
  frozen workspaces
- `readTaskState` from `internal/ops/helpers.go` — shared helper

## Implementation Steps

### 1. `internal/models/task.go` — ReviewingBy guard

Add `t.ReviewingBy != nil → return false` to `isClaimablePipeline` reviewer case,
before the status checks.

### 2. `internal/ops/clear_stale_review_claims.go` — Crash recovery

Extend `ClearStaleReviewClaims` to also clear stale ReviewingBy on tasks that are
NOT in a reviewing state. Currently `detectReviewingState` gates the entire scan —
tasks in REJECTED/IMPLEMENTING/SUBMITTED with a stale ReviewingBy are invisible.

Add a second pass (or extend the existing loop) that checks all tasks with
`ReviewingBy != nil` and expired `ReviewLeaseExpires`, regardless of status:

```go
// After the existing reviewing-state scan:
// Clear orphaned ReviewingBy on non-reviewing tasks (crash recovery for await_resubmission).
if task.ReviewingBy != nil && match == nil {
    // Not in a reviewing state, but ReviewingBy is set — check lease.
    if task.ReviewLeaseExpires == nil || !task.ReviewLeaseExpires.After(now) {
        staleReviewer := *task.ReviewingBy
        task.ReviewingBy = nil
        task.ReviewLeaseExpires = nil
        // Release the reviewer agent if still assigned.
        if a, ok := state.Agents[staleReviewer]; ok {
            if a.CurrentTask != nil && *a.CurrentTask == task.ID {
                state.ReleaseAgent(staleReviewer)
            }
        }
        // Log...
        cleared++
    }
}
```

This does NOT revert task status (unlike the existing path which transitions
REVIEWING → SUBMITTED). The task stays in its current status — only the orphaned
ReviewingBy ownership is cleared, unblocking future reviewer claims.

### 3. `internal/ops/await_resubmission.go` — Core blocking logic

```go
// Resubmission verdict constants.
const (
    ResubmissionResubmitted = "RESUBMITTED"
    ResubmissionTerminal    = "TERMINAL"
    ResubmissionTimeout     = "TIMEOUT"
    ResubmissionAborted     = "ABORTED"
)

type AwaitResubmissionResult struct {
    Verdict      string            // One of the Resubmission* constants
    TaskStatus   models.TaskStatus // Final observed task status
    Reason       string            // Terminal explanation (empty on RESUBMITTED)
    ReviewCommit string            // New commit SHA to review (on RESUBMITTED)
    ReviewCycle  int               // Current review cycle count
}

func AwaitResubmission(ctx context.Context, projectRoot, taskID, agentID string, timeout time.Duration) (*AwaitResubmissionResult, error)
```

**Validate preconditions**:
- Task exists, has a role-pair
- Task status is rejected OR already submitted (via `resolver.RejectedStatus` /
  `resolver.SubmittedStatus`). Accepting submitted handles the fast-doer edge case:
  if the doer resubmits before the reviewer calls await_resubmission, skip the wait
  loop and immediately reclaim (see "early resubmission" below).
- Agent was the last rejecting reviewer (scan history in reverse for most recent
  `TaskEventRejected` by this agent)

**Acquire review ownership** — atomically within `bb.Modify`:
- `task.ReviewingBy = &agentID`
- `task.ReviewLeaseExpires = now + timeout + 5min` (margin beyond await deadline)
- `agent.Status = AgentStatusWaiting`
- `agent.CurrentTask = &taskID`

This differs from `acquireAwaitOwnership` (await_verdict) which only sets agent
fields. Here we set both agent AND task fields because we're preserving reviewer
ownership at the task level.

**Early resubmission** — if task is already in submitted status at entry (doer was
faster than the reviewer's next MCP call), skip the watch loop entirely and jump
straight to the reclaim logic below. Same happy-path result, zero wait.

**Watch loop** — same structure as `await_verdict`:
- `watcher.Events()` → `bb.ReadCached()` → check if task status left the awaiting set
- `abortTicker` (1s) → check system mode → release ownership, return `ABORTED`
- `deadlineTimer` → release ownership, return `TIMEOUT`
- `ctx.Done()` → release ownership, return context error
- `watcher.Errors()` → fall back to polling (re-read state every 5s)

**Resubmission detection** — `checkResubmissionStatus()`:
Define positively what triggers exit from the wait loop:
- `submitted` status (via `resolver.SubmittedStatus`) → resubmission detected
- `approved`, `blocked`, `superseded`, cancelled, integration_failed → terminal
- All other statuses (rejected, executing, initial) → keep waiting

Note: `partiallyApproved` is NOT in the detection set. Rejection clears all approvals,
so the doer always resubmits to SUBMITTED. PARTIALLY_APPROVED is unreachable through
the await_resubmission flow — it requires a prior approval that the rejection erased.

**Result mapping — resubmission detected (SUBMITTED)** — atomically within `bb.Modify`:
1. Transition task from SUBMITTED → REVIEWING (via `task.TransitionWith`)
2. Set fresh `ReviewLeaseExpires = now + 30min`
3. `agent.Status = AgentStatusReviewing`
4. `agent.CurrentTask = &taskID`
5. Read `task.ReviewCommit` for the new commit SHA
6. Return `Verdict: RESUBMITTED` with ReviewCommit and ReviewCyclesCurrent

**Result mapping — terminal states**:
- Release ownership: clear `task.ReviewingBy`, `task.ReviewLeaseExpires`,
  `agent.CurrentTask`
- Return `Verdict: TERMINAL` with reason

**Result mapping — timeout/abort**:
- Same release as terminal
- Return `Verdict: TIMEOUT` or `ABORTED`

**Ownership release** — `releaseReviewOwnership()`:
```go
func releaseReviewOwnership(bb *db.Blackboard, agentID, taskID string) error {
    return bb.Modify(func(s *models.State) error {
        if agent, ok := s.Agents[agentID]; ok {
            agent.CurrentTask = nil
            s.Agents[agentID] = agent
        }
        task := s.FindTask(taskID)
        if task != nil {
            task.ReviewingBy = nil
            task.ReviewLeaseExpires = nil
        }
        return nil
    })
}
```

Agent status is intentionally left unchanged — same pattern as `releaseOwnership` in
`await_verdict`. The supervisor's `resetAgentAfterExit` handles agent status
transitions when the CLI session ends (which follows immediately after TIMEOUT,
ABORTED, or TERMINAL results per prompt guidance "exit normally").

### 4. `internal/mcp/handlers_mutation.go` — MCP handler

```go
func (s *Server) handleAwaitResubmission(params map[string]any) (any, error)
```

- Extract `task_id`, `agent_id` (required), `timeout_seconds` (optional, default 1500)
- Call `ops.AwaitResubmission(ctx, s.projectRoot, taskID, agentID, timeout)`
- Format result with inline guidance on RESUBMITTED:
  `"New submission received. Review the changes at commit %s. Review cycle %d."`

### 5. `internal/mcp/server_registration.go` — Register tool

```go
{
    tool: protocol.Tool{
        Name:        "liza_await_resubmission",
        Description: "Block until doer resubmits after a rejection. Call after " +
            "liza_submit_verdict with REJECTED. Preserves review ownership.",
        InputSchema: protocol.InputSchema{
            Type: "object",
            Properties: map[string]protocol.Property{
                "task_id":         {Type: "string", Description: "Task ID to await resubmission for"},
                "agent_id":        {Type: "string", Description: "Agent ID (for authorization)"},
                "timeout_seconds": {Type: "integer", Description: "Max wait time in seconds (default: 1500, must be < MCP_TIMEOUT)"},
            },
            Required: []string{"task_id", "agent_id"},
        },
    },
    handler:     s.handleAwaitResubmission,
    roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_await_resubmission"),
}
```

### 6. Pipeline config — add `await-resubmission` to reviewer operations

In `internal/embedded/pipeline.yaml` and `internal/pipeline/testdata/valid-phase2-full.yaml`,
add `await-resubmission` to `allowed-operations` for: code-reviewer, code-plan-reviewer,
epic-plan-reviewer, us-reviewer.

`MigrateOperations` handles existing frozen workspaces automatically.

### 7. Prompt updates — `internal/prompts/templates/blocks/reviewer_tools.tmpl`

Add to each reviewer role's tool listing:
```
- liza_await_resubmission — Block until doer resubmits after rejection (call after
  submit_verdict REJECTED when verdict was non-terminal)
  Tool parameters: {"task_id": "...", "agent_id": "..."}
```

### 8. Prompt updates — reviewer state transitions

In `reviewer_state_transitions.tmpl`, add after the REJECTED transition:
```
After REJECTED verdict (non-terminal):
  Call liza_await_resubmission to wait for the doer's resubmission.
  - If RESUBMITTED: task is reclaimed for review — review the new changes
  - If TERMINAL, TIMEOUT, or ABORTED: exit normally
  Do NOT call if verdict result shows EscalatedToBlocked or NewAttemptTriggered.
```

### 9. `internal/embedded/claude-settings.json` — Tool permission

Add `"mcp__liza__liza_await_resubmission"` to the `permissions.allow` array.

### 10. Tests

- `internal/ops/await_resubmission_test.go`:
  - Ownership: on entry, task.ReviewingBy set, agent status WAITING
  - Wrong task status (not rejected) → precondition error
  - Wrong agent (not the rejecting reviewer) → precondition error
  - Task becomes submitted (doer resubmits) → reclaims review, returns RESUBMITTED
    with ReviewCommit and ReviewCycle
  - Task becomes blocked → returns TERMINAL, ownership released
  - Task becomes superseded → returns TERMINAL, ownership released
  - Task approved via other path → returns TERMINAL, ownership released
  - Task disappears → returns TERMINAL, ownership released
  - Timeout → returns TIMEOUT, ownership released
  - ABORT signal → returns ABORTED, ownership released
  - Early resubmission: task already SUBMITTED at entry → immediate reclaim, no wait
  - Race guard: another reviewer cannot claim while ReviewingBy is set
  - ReviewLeaseExpires set correctly on entry and on reclaim

- `internal/models/task_test.go`:
  - `isClaimablePipeline`: reviewer cannot claim task with ReviewingBy set
  - Existing tests pass: ReviewingBy is nil in all existing claimable scenarios

- `internal/integration/await_resubmission_test.go` — E2E rejection + resubmission:
  - Setup: init project, create task, register coder + reviewer agents
  - Coder: claim → implement → commit → submit_for_review
  - Reviewer: claim → review → submit_verdict(REJECTED)
  - Reviewer: call AwaitResubmission (in goroutine — it blocks)
  - Coder: await_verdict detects rejection → auto-reclaims → fixes → resubmits
  - Verify: AwaitResubmission returns RESUBMITTED, task in REVIEWING,
    ReviewCommit updated, ReviewCycle correct
  - Reviewer: reviews new changes → approve
  - Merge succeeds

- `internal/ops/clear_stale_review_claims_test.go`:
  - Stale ReviewingBy on REJECTED task (expired lease) → cleared, task stays REJECTED
  - Stale ReviewingBy on SUBMITTED task (expired lease) → cleared, task stays SUBMITTED
  - Active ReviewingBy on REJECTED task (valid lease) → NOT cleared
  - Existing tests pass: no behavioral change for REVIEWING/REVIEWING_2 tasks

- `internal/mcp/handlers_test.go`: parameter validation, role check, successful call
- `internal/pipeline/resolver_test.go`: update `TestAllowedOperations` reviewer list
- `internal/pipeline/config_test.go`: update expected operation count
- `internal/pipeline/migrate_test.go`: verify `await-resubmission` migrates to frozen configs

### 11. Specs & docs updates

| File | Update |
|------|--------|
| `specs/build/1.3.1 - Claim-to-Verdict Task Lifecycle.md` | Add await-resubmission to the review lifecycle |
| `specs/build/3 - Declarative Role Definitions.md` | Add `await-resubmission` to reviewer role allowed operations |
| `docs/USAGE_MULTI_AGENTS.md` | Document reject → await → re-review workflow |
| `docs/CONFIGURATION.md` | Add tool to MCP tools listing |

### 12. Validation

```bash
make test                    # all existing + new tests pass
go build ./cmd/liza-mcp/     # binary builds with new handler
# Pre-commit on all touched files
# Inspect generated prompt for reviewer roles to verify liza_await_resubmission appears
```
