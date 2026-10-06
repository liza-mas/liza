# 54 - Blocking Await Primitives for Session-Persistent Review Flow

## Context and Problem Statement

The original vision was for agents to live along the entire attempt — not just one submission cycle. In practice, every review rejection triggered a full session exit → supervisor loop → new session spawn → contract initialization, costing ~47s per cycle and destroying accumulated context. A task with 4 rejections across 2 review cycles faced up to 8 full restarts (~6+ minutes overhead), with agents losing all context about prior feedback and their own reasoning.

This was always the intended architecture but had been deferred due to complexity. The observed token waste from cold restarts made it urgent to stop postponing.

## Considered Options

1. **Two complementary blocking MCP tools** — `liza_await_verdict` (doer-side) and `liza_await_resubmission` (reviewer-side) — keeping agent sessions alive across review cycles within an attempt.

No alternatives were considered. Agents living along the entire attempt but not longer is the right lifespan — a trade-off between context reuse and context pressure. The blocking MCP call pattern was consistent with the existing architecture (reusing the `waitForWorkEventDriven` infrastructure).

## Decision Outcome

Chose **Option 1**: two blocking MCP tools that keep doer and reviewer sessions alive across feedback cycles, eliminating cold restarts while preserving crash safety and budget awareness.

### Architecture

**liza_await_verdict (doer-side):**

Doer submits work, then blocks on an MCP call until the reviewer's verdict arrives. On rejection, the tool attempts atomic auto-reclaim (same attempt), allowing the agent to continue in the same session.

```
Coder Session:
  [implement] → submit_for_review() → await_verdict() ← BLOCKS
                                         │
                                         ├─ Budget Gate: pre-check limits → ErrBudgetExhausted (fail fast)
                                         │
                                         └─ Event Loop (fsnotify + 5s polling fallback):
                                              ├─ SUBMITTED/REVIEWING → keep waiting
                                              ├─ PARTIALLY_APPROVED → keep waiting (quorum)
                                              ├─ APPROVED → release ownership, exit
                                              ├─ REJECTED → ClaimTask (auto-reclaim)
                                              │    ├─ Same attempt → continue in session
                                              │    ├─ LimitActionNewAttempt → exit, new coder spawns
                                              │    └─ LimitActionBlocked → exit
                                              ├─ BLOCKED/SUPERSEDED → exit
                                              └─ TIMEOUT/ABORTED → exit
```

**liza_await_resubmission (reviewer-side):**

Reviewer issues rejection, then blocks on an MCP call (retaining `ReviewingBy` ownership) until the doer resubmits. On resubmission, the tool atomically reclaims the task for re-review.

```
Reviewer Session:
  [review] → submit_verdict(REJECTED) → await_resubmission() ← BLOCKS
                                           │
                                           └─ Event Loop (fsnotify + 5s polling fallback):
                                                ├─ REJECTED/IMPLEMENTING → keep waiting
                                                ├─ SUBMITTED → reclaimForReview (atomic)
                                                │    └─ SUBMITTED → REVIEWING, refresh lease
                                                │    └─ Continue in session
                                                ├─ BLOCKED/SUPERSEDED → exit
                                                └─ TIMEOUT/ABORTED → exit
```

**Key design choices:**

| Choice | Rationale |
|--------|-----------|
| Blocking MCP call (not polling) | Consistent with existing `waitForWorkEventDriven` pattern; event-driven via fsnotify with polling fallback |
| 1500s default timeout | Must stay under Claude Code's 30-min MCP_TIMEOUT to avoid interrupted sessions |
| Budget gate (doer only) | Pre-checks iteration/review limits before entering block; prevents 25-min wait only to discover agent can't iterate. Reviewer doesn't need it — limits were already validated during verdict |
| Auto-reclaim via ClaimTask | Reuses existing limit classification logic rather than reimplementing; preserves worktree on same-attempt reclaim |
| Supervisor heartbeat keeps lease alive | Agent session blocks on MCP call but supervisor process remains alive, heartbeat goroutine continues, lease doesn't expire |

**Ownership model:**
- Doer: `Agent.CurrentTask` prevents other supervisors from claiming the task while doer awaits
- Reviewer: `Task.ReviewingBy` prevents second reviewer from claiming while first awaits resubmission
- Both use time-based lease expiry for crash recovery

**Crash safety:**
- Extended `ClearStaleReviewClaims` to detect orphaned `ReviewingBy` on non-reviewing tasks (reviewer crash during await)
- Defense-in-depth guard in `isClaimablePipeline`: reviewer claiming requires `ReviewingBy == nil` or expired lease

**Registered in pipeline.yaml:** `await-verdict` added to all 4 doer roles, `await-resubmission` to all 4 reviewer roles. Role-based access control via `operationChecker` middleware.

### Rationale

The attempt lifecycle (ADR-0051) established that agents should own their attempt. The missing piece was session persistence across rejection cycles — without it, agents paid cold-start costs on every rejection and lost all accumulated context. Blocking MCP calls are the natural fit: they keep the session alive without requiring new infrastructure, reuse the existing event-watching system, and the timeout constraint (25 min < 30 min MCP limit) is acceptable for review cycles.

### Consequences

**Positive:**
- Eliminates ~47s cold restart per rejection within same attempt
- Preserves agent context across review cycles (no session exit/re-init)
- Budget-safe: doer-side fail-fast on limit exhaustion prevents wasted blocks
- Crash-safe: orphaned ReviewingBy cleanup + defense-in-depth guard
- Both doer and reviewer benefit — symmetric session persistence

**Limitations accepted:**
- 1500s (25 min) default timeout — workspaces with longer reviews need manual `MCP_TIMEOUT` configuration
- 5s polling latency if fsnotify watcher fails (robust fallback, not primary path)

**Restores:** Original vision of attempt-scoped agent sessions. **Extends:** ADR-0051 (First-Class Attempt Model) — completes the attempt lifecycle with session persistence.

## Amendment (2026-08-23)

The `MCP_TIMEOUT` constraint above no longer applies — ADR-0057 removed the MCP
server, and both awaits are now plain foreground CLI calls. Each invocation is
capped at 100s (provider tool-call limits) and carries no argument between calls:
the remaining budget is derived from the submission or rejection recorded in task
history, bounded by a fixed `DefaultAwaitBudget` horizon.

Consequent changes: default budget 1500s → 1800s; reviewer session ceiling
30m → 2h so a reviewer can span a full attempt as a doer already could; the
doer-side three-call prompt cap removed, which had bounded doer waiting at ~5
minutes and abandoned the task on expiry.

See [Await Primitives](../../protocols/await-primitives.md) for the current mechanism.

## Amendment (2026-10-05): foreground intervals and retained reviewer waits

D-42 exposed unnecessary provider turns at the uniform 100s cap and review
ownership churn that invalidated overlapping resubmission preparation (D-53).
Both commands now use the same exact-tool policy: registered `claude` gets a
540s interval with explicit Bash timeout 600000ms; other tools retain 100s.
Backend/contract aliases do not inherit a harness limit. Codex yielding a session
ID is handled by waiting on the same process with `write_stdin`.

The 1800s history-anchored ceiling remains. A resubmission interval expiry returns
POLL before cleanup, retaining its live, generation-fenced reservation; retry
refreshes the lease without advancing lifecycle or discarding preparation. Final
expiry, cancellation, stop, terminal state or reservation loss ends the hold.

Halt modes return PAUSED with stop/resume guidance. Departed ownership is released
narrowly while submitted work remains intact; resubmission reclaim refuses halt
inside its transaction, including pause during preflight. No provider session
resume capability or lifecycle-validation relaxation is introduced.

---
*Reconstructed from commits 3f49344..ce6469a (2026-03-30 to 2026-03-31)*
