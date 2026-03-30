# `liza_await_verdict` MCP Tool

## Context

In the current Liza multi-agent system, each review rejection triggers a full cold restart: session exit → supervisor loop → new session spawn → contract initialization (~47s overhead per cycle). For doer agents iterating on rejections, this overhead compounds — up to 8 restarts per task (4 rejections × 2 attempts). A blocking MCP tool that keeps the session alive between submit and verdict eliminates this restart cost while preserving the agent's accumulated context about the code.

The tool preserves task ownership across the review cycle. The doer retains logical ownership while awaiting (agent status WAITING, CurrentTask set), and on rejection within the same attempt, the tool atomically reclaims the task so the agent continues in the same session — no cold restart. New attempts always get a fresh doer.

## Critical Files

| File | Action |
|------|--------|
| `internal/ops/await_verdict.go` | **Create** — core blocking logic |
| `internal/mcp/handlers_mutation.go` | **Edit** — add MCP handler |
| `internal/mcp/server_registration.go` | **Edit** — register tool |
| `internal/embedded/pipeline.yaml` | **Edit** — add operation to doer roles |
| `internal/pipeline/testdata/valid-phase2-full.yaml` | **Edit** — add operation to test fixture |
| `internal/prompts/templates/blocks/doer_tools.tmpl` | **Edit** — add tool to prompt listings |
| `internal/prompts/templates/blocks/submission_phase.tmpl` | **Edit** — add await flow guidance |
| `internal/embedded/claude-settings.json` | **Edit** — add `mcp__liza__liza_await_verdict` to `allow` array |
| `internal/agent/heartbeat.go` | **Verify** — supervisor heartbeat keeps lease alive during block |

## Architectural Assumptions

- **Heartbeat continuity**: The supervisor's heartbeat goroutine runs for the supervisor process lifetime, not per-agent-session. While the agent is blocked on the MCP tool, the supervisor process is alive (blocked on `executeAgent()`), so heartbeat continues and the supervisor lease doesn't expire. If heartbeat were changed to require agent-side signaling, this would break.
- **No submit_for_review changes**: The await tool sets agent `CurrentTask` itself on entry. The window between submit (which clears CurrentTask) and await is within a single session — the supervisor is blocked on `executeAgent()` and can't observe the gap. No race exists, so submit_for_review stays unchanged.
- **MCP timeout**: Claude Code's default MCP tool timeout is 30 min (`MCP_TIMEOUT` env var). The tool defaults to 1500s (25 min) to stay within this. For workspaces where reviews routinely exceed 25 min, configure `MCP_TIMEOUT` higher and pass a larger `timeout_seconds`. This is a known limitation, not a design flaw — the alternative (no await) is strictly worse.

## Reuse

- `bb.WatchForChanges()` from `internal/db/watcher.go` — fsnotify setup, debounce, event channel
- `waitForWorkEventDriven` pattern from `internal/agent/waitforwork.go` — event loop with abort ticker, deadline, fallback to polling
- `effectiveCoderIterationLimit` / `effectiveReviewCycleLimit` / `classifyLimitEscalation` from `internal/ops/iteration_limits.go` — budget checks
- `resolver.RejectedStatus()` / `resolver.ApprovedStatus()` etc. from `internal/pipeline/resolver.go` — status resolution per role-pair
- `ops.ClaimTask` from `internal/ops/claim_task.go` — auto-reclaim on rejection (uses `rejectedClaimStrategy`, preserves worktree)
- `operationChecker` from `internal/mcp/middleware.go` — role authorization
- `requireString` / `textResult` from `internal/mcp/handlers_mutation.go` — parameter extraction / response formatting
- `MigrateOperations` from `internal/pipeline/migrate.go` — auto-propagation to frozen workspaces

## Implementation Steps

### 1. `internal/ops/await_verdict.go` — Core blocking logic

```go
func AwaitVerdict(projectRoot, taskID, agentID string, timeout time.Duration) (*AwaitVerdictResult, error)
```

- **Validate preconditions**: task exists, task status is submitted or reviewing (via `resolver.SubmittedStatus` / `resolver.ReviewingStatus` for the task's role-pair), agent was the last submitter (check task history for `submitted_for_review` event by this agent)
- **Acquire ownership**: atomically set agent status = WAITING, CurrentTask = taskID in blackboard. This prevents other doer supervisors from claiming the task if it gets rejected. Does NOT touch `assigned_to` on the task (reviewer needs that).
- **Budget gate**: load config, compute limits with `effectiveCoderIterationLimit` and `effectiveReviewCycleLimit`. Simulate what would happen on rejection: if `classifyLimitEscalation` returns `shouldEscalate=true`, release ownership and return `ErrBudgetExhausted` immediately
- **Watch loop**: `bb.WatchForChanges()` → event-driven select loop:
  - `watcher.Events()` → `bb.ReadCached()` → check if task status left the submitted/reviewing/partially-approved set
  - `abortTicker` (1s) → check ABORT flag via `paths.AbortPath` → release ownership, return `Verdict: "ABORTED"`
  - `deadlineTimer` → release ownership, return `Verdict: "TIMEOUT"`
  - `ctx.Done()` → release ownership, return context error
  - `watcher.Errors()` → fall back to polling (re-read state every 5s)
- **PartiallyApproved**: keep waiting — quorum not yet met, review still in progress
- **Result mapping and ownership**:
  - Approved → release ownership, return `Verdict: "APPROVED"` (agent exits)
  - Rejected → attempt auto-reclaim via `ops.ClaimTask`. ClaimTask internally checks limits via `classifyLimitEscalation` — the tool doesn't need its own same-vs-new-attempt detection:
    - ClaimTask succeeds → same attempt. Return `Verdict: "REJECTED"` with:
      - Rejection reason (from latest history entry)
      - Updated iteration number and remaining budget
      - Inline guidance equivalent to `prior_rejection.tmpl` ("MUST ADDRESS", scope_extensions hint for coders)
      - Agent continues working — no cold restart
    - ClaimTask returns `LimitActionNewAttempt` error → release ownership, return `Verdict: "NEW_ATTEMPT"` (agent exits, fresh doer spawns)
    - ClaimTask returns `LimitActionBlocked` error → release ownership, return `Verdict: "TERMINAL"`
  - Blocked/Superseded/IntegrationFailed → release ownership, return `Verdict: "TERMINAL"`
- **Return**: `AwaitVerdictResult{Verdict, Reason, ReviewerAgent, TaskStatus, Iteration}`

### 2. `internal/mcp/handlers_mutation.go` — MCP handler

```go
func (s *Server) handleAwaitVerdict(params map[string]any) (any, error)
```

- Extract `task_id`, `agent_id` (required), `timeout_seconds` (optional, default 1500)
- **Timeout constraint**: Claude Code MCP timeout is 30 min by default (`MCP_TIMEOUT` env var). Default 1500s (25 min) leaves margin within this limit
- Call `ops.AwaitVerdict(s.projectRoot, taskID, agentID, timeout)`
- Format result: `textResult(fmt.Sprintf("Verdict: %s\nStatus: %s\nReason: %s\nReviewer: %s", ...))`
- On `ErrBudgetExhausted`: return clear error message

### 3. `internal/mcp/server_registration.go` — Register tool

Add to mutation tools `toolDef` slice:
```go
{
    tool: protocol.Tool{
        Name:        "liza_await_verdict",
        Description: "Block until review verdict arrives for a submitted task. Budget-aware: refuses if iteration limit would be exceeded on rejection. Call after liza_submit_for_review.",
        InputSchema: protocol.InputSchema{
            Type: "object",
            Properties: map[string]protocol.Property{
                "task_id":         {Type: "string", Description: "Task ID to await verdict for"},
                "agent_id":        {Type: "string", Description: "Agent ID (for authorization)"},
                "timeout_seconds": {Type: "integer", Description: "Max wait time in seconds (default: 1500, must be < MCP_TIMEOUT)"},
            },
            Required: []string{"task_id", "agent_id"},
        },
    },
    handler:     s.handleAwaitVerdict,
    roleChecker: operationChecker(s.resolver, s.pipelineLoadErr, "liza_await_verdict"),
}
```

### 4. Pipeline config — add `await-verdict` to doer operations

In `internal/embedded/pipeline.yaml` and `internal/pipeline/testdata/valid-phase2-full.yaml`, add `await-verdict` to `allowed-operations` for: coder, code-planner, epic-planner, us-writer.

`MigrateOperations` handles existing frozen workspaces automatically.

### 5. Prompt updates — `internal/prompts/templates/blocks/doer_tools.tmpl`

Add to each doer role's tool listing:
```
- liza_await_verdict — Block until review verdict arrives (call after submit_for_review; budget-aware)
  Tool parameters: {"task_id": "...", "agent_id": "..."}
```

### 6. Prompt updates — submission flow guidance

In `submission_phase.tmpl` (or `implementation_phase.tmpl`), after the submit instruction, add:
```
After successful submission, call liza_await_verdict to wait for the review verdict.
- If REJECTED: task is already reclaimed — read the rejection reason, fix, and resubmit
- If APPROVED, NEW_ATTEMPT, TIMEOUT, or ABORTED: exit normally
- If budget error: exit normally (at iteration limit)
```

### 7. `internal/embedded/claude-settings.json` — Tool permission

Add `"mcp__liza__liza_await_verdict"` to the `permissions.allow` array alongside the other liza tools.

### 8. Tests

- `internal/ops/await_verdict_test.go`:
  - Ownership: on entry, agent CurrentTask set and status WAITING
  - Budget exhausted → `ErrBudgetExhausted`, ownership released
  - Within budget, task becomes rejected (same attempt) → auto-reclaims, returns REJECTED with reason and new iteration
  - Within budget, task becomes rejected (triggers new attempt) → returns NEW_ATTEMPT, ownership released
  - Within budget, task becomes approved → returns APPROVED, ownership released
  - Task becomes blocked → returns TERMINAL, ownership released
  - PartiallyApproved → keeps waiting (quorum not met)
  - Timeout → returns TIMEOUT, ownership released
  - ABORT signal → returns ABORTED, ownership released
  - Wrong task status (not submitted/reviewing) → precondition error
  - Wrong agent → precondition error
  - Race guard: another agent cannot claim REJECTED task while awaiting agent holds ownership

- `internal/integration/await_verdict_test.go` — E2E rejection simulation:
  - Setup: init project, create task, register coder + reviewer agents
  - Coder: claim → implement → commit → submit_for_review
  - Coder: call AwaitVerdict (in goroutine — it blocks)
  - Reviewer: transition to REVIEWING → submit REJECTED verdict with reason
  - Verify: AwaitVerdict returns REJECTED, task auto-reclaimed, iteration incremented
  - Coder: fix → commit → re-submit
  - Reviewer: approve
  - Merge succeeds
  - Pattern: follows `e2e_workflow_test.go` setup + `sprint_and_merge_test.go` rejection flow

- `internal/mcp/handlers_test.go`: parameter validation, role check, successful call

- `internal/pipeline/resolver_test.go`: update `TestAllowedOperations` coder expected list

- `internal/pipeline/config_test.go`: update expected operation count

- `internal/pipeline/migrate_test.go`: verify `await-verdict` migrates to frozen configs

### 9. Specs & docs updates

| File | Update |
|------|--------|
| `specs/build/1.3.1 - Claim-to-Verdict Task Lifecycle.md` | Add await-verdict to the submission lifecycle |
| `specs/build/3 - Declarative Role Definitions.md` | Add `await-verdict` to doer role allowed operations |
| `docs/USAGE_MULTI_AGENTS.md` | Document submit → await → handle verdict workflow |
| `docs/CONFIGURATION.md` | Add tool to MCP tools listing |

### 10. Validation

```bash
make test                    # all existing + new tests pass
go build ./cmd/liza-mcp/     # binary builds with new handler
# Pre-commit on all touched files (automatic via make test)
# Inspect generated prompt for coder role to verify liza_await_verdict appears
```
