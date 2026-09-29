# Await Primitives

## Rationale

An agent session is the only place accumulated context lives. Nothing recovers it
after exit: no backend implements provider-side session resume. Supervisor-level
machinery — task affinity, reservations, resume routing — can reduce stranded work,
but it can never restore context.

So `await-verdict` (doer) and `await-resubmission` (reviewer) are the only mechanism
that keeps an agent alive across a review boundary. Everything below follows from that.

Three premises:

1. **Await is the only way to hold a session.** There is no coming back once an agent exits.
2. **Calls are sliced at 100 seconds** because some providers time out tool calls at 120s.
3. **Affinity is attempt-scoped and best-effort.** Both agents should survive a whole
   attempt, but if that breaks it is acceptable for a fresh agent to pick the task up.

Premise 3 is why budget exhaustion ends in a clean exit rather than an escalation.

---

## The two primitives

| | `await-verdict` | `await-resubmission` |
|---|---|---|
| Caller | doer, after `submit-for-review` | reviewer, after a REJECTED verdict |
| Waiting for | a reviewer's verdict | the doer's next submission |
| Awaitable statuses | submitted, reviewing, partially-approved | rejected, executing, submitted |
| Wake outcome | REJECTED → auto-reclaim via `ClaimTask`, continue in session | RESUBMITTED → `reclaimForReview`, re-review in session |
| Terminal outcomes | APPROVED, TERMINAL, NEW_ATTEMPT, ABORTED, ALREADY_TRANSITIONED | TERMINAL, ABORTED |
| Budget exhausted | TIMEOUT → exit | TIMEOUT → exit |

Below the prompt layer both share `awaitWithBudget` (`internal/commands/await_budget.go`),
the same 100s interval constant, the same POLL/TIMEOUT relabelling, and the same
fsnotify-with-polling-fallback event loop. **They must stay symmetric.** The
doer/reviewer divergence that produced the ~5-minute doer wait existed only in prompt
text wrapped around identical code, because no document described the mechanism.

---

## Budget mechanics

`--timeout-seconds` is the **total** wait allowance, not a wait duration and not a
remainder the caller tracks. Every invocation recomputes what is left from state:

```
anchor    = latest submitted_for_review (doer) / rejected (reviewer) entry, this agent
total     = min(--timeout-seconds, DefaultAwaitBudget)   -- ceiling, not caller-raisable
remaining = clamp(total - (now - anchor), 0, total)
interval  = min(remaining, 100s)      -> how long this call actually blocks
remaining - interval > 0  -> relabel TIMEOUT as POLL, report timeout_seconds
remaining - interval == 0 -> TIMEOUT is final
```

Both halves are load-bearing. Anchoring alone is not monotonic: a caller that raises
`--timeout-seconds` by the time elapsed would hold the remainder constant forever. The
ceiling closes that. The invariant it buys is a fixed horizon, not a ratchet: a later
call passing a larger total *can* extend a shorter allowance an earlier call set, but
no sequence of calls can wait past `anchor + DefaultAwaitBudget`. That upper bound is
what makes the loop terminate.

Deriving from the anchor rather than from a value the agent carries between calls is
what makes the bound **enforceable**. An agent that never passes `--timeout-seconds`
still converges on TIMEOUT, because elapsed time advances whatever it sends. Without
this, a non-threading caller resets to the full budget on every call and the loop ends
only at the session ceiling — which surfaces as `Agent execution timeout`, exit 1, and
a retry, i.e. a bounded wait misreported as a hung provider.

`timeout_seconds` is still reported on POLL, but for visibility only. Prompts instruct
agents to re-run the identical command with no arguments to carry over.

On exhaustion the doer's **assignment and lease** are released — and nothing else. The
generic `ReleaseClaim` doer profile also clears `Worktree`, `BaseCommit`, `Iteration`,
`Output` and the submitted-attempt block (`ReviewCommit`, approvals, `MergeCommit`);
applying it here would strip the review boundary from a task still in review, so the
later verdict would fail validation and the reviewer would be handed an empty worktree.
`ReleaseDepartedDoerAssignment` exists for this narrower case. The submitted attempt
outlives the doer that produced it.

Clamps: a missing anchor falls back to the full total, leaving the error to the await's
own precondition checks; an anchor in the future (clock skew) yields the total, never
more.

The budget is **per wait, not per attempt**. REJECTED and RESUBMITTED are not POLL
outcomes and each writes a fresh history entry, so the next wait anchors on that entry
and starts over. Each wait is bounded by one peer pass, not by the attempt.

---

## Ownership

Both primitives acquire ownership on entry and release it on every interval expiry,
re-acquiring on the next call. The inter-call gap is seconds against a 30-minute
task lease, so it is harmless; only the final release matters, and there it is
correct — the agent is leaving.

- **Doer:** `agent.Status = WAITING`, `agent.CurrentTask = taskID`. Does not touch
  `assigned_to` (the reviewer needs it). Heartbeat renews the task lease only while
  `CurrentTask` is set.
- **Reviewer:** additionally sets `task.ReviewingBy` and `task.ReviewLeaseExpires`,
  which is what stops a second reviewer claiming a task someone is actively awaiting.
  This acquisition is the wait's *reservation*, bounded by the task-history length at
  that moment. It holds while those fields and the agent's WAITING `current_task`
  still match and no later claim superseded it: another reviewer holding the task,
  or a `review_claim_released`, or a `claimed` or `claim_released` by this agent,
  recorded since. A wait outliving its cancelled provider turn therefore cannot
  touch a later claim by the same registration, even one whose fields match
  exactly. Losing the reservation ends the wait with a final `ABORTED` ("review
  ownership lost") and no mutation; callers must not retry. Only a terminal outcome
  tolerates a released agent (`mark-blocked` releases it itself): it is lost only to
  a later claim. The early-resubmission path acquires no reservation before reclaim.

A doer entering `await-verdict` first passes a budget gate: if iteration or
review-cycle limits are already at capacity, it returns `ErrBudgetExhausted`
immediately rather than waiting for a verdict it could not act on. The reviewer needs
no equivalent — limits were validated when the verdict was submitted.

---

## The six clocks

These are coupled. Changing one requires rechecking the others.

| Clock | Default | Defined in | Bounds |
|---|---|---|---|
| Call interval | 100s | `maxAwaitInterval`, `internal/commands/await_budget.go` | one foreground call |
| Wait budget | 1800s | `--timeout-seconds`, `cmd/liza/cmd_review.go` | one wait, i.e. one peer pass |
| Session ceiling | 2h | `timeouts.execution` per role, `pipeline.yaml` | one provider session, i.e. one whole attempt |
| Task lease | 1800s | `DefaultLeaseDurationSeconds` | doer ownership of a rejected task |
| Review lease | interval + 5m | `reviewOwnershipLeaseMargin` | reviewer exclusivity while awaiting |
| Progress timeout | 1800s | `DefaultAgentProgressTimeoutSec` | silent stretch within an *executing* task |

Relationships that must hold:

- **Call interval < provider tool-call limit.** 100s against a ~120s limit; asserted by
  `TestMaxAwaitIntervalStaysBelowForegroundTransportLimit`.
- **Wait budget ≥ longest expected peer pass.** A first review runs ~15m, corrective
  passes 5-10m; 1800s covers the worst case with margin.
- **Session ceiling ≥ rounds × (own pass + wait).** With `DefaultMaxReviewCycles = 5`,
  a first doer pass of ~25m and corrective passes of 5-10m, a full attempt runs ~100
  minutes. 2h covers it. Doer and reviewer ceilings must be equal, or the shorter side
  loses affinity first. A ceiling breach is worse than a clean TIMEOUT: the context is
  cancelled, it logs `Agent execution timeout` and returns exit 1, which reads as a
  hung provider rather than an expired wait.
- **Task lease renewal depends on `CurrentTask`.** Once an agent moves to another task,
  the previous task's lease stops renewing and lapses within the lease duration.
- **The progress watchdog does not cover awaiting agents.** It disengages once the task
  leaves executing status, so the session ceiling is the only bound on a wedged await.

---

## Loop detection

`await-*` POLL retries are exempt from the Loop Detection Self-Abort rule in
`MULTI_AGENT_MODE.md`. Each retry returns a smaller remaining budget and the loop
terminates at TIMEOUT — bounded waiting, not step repetition. The exemption is narrow
and applies to these two commands only.

The exemption is unconditional because termination does not depend on the agent
behaving correctly. The remaining budget is derived from state, so the retry sequence
is monotonic regardless of what the agent passes. Were the budget agent-tracked, an
unconditional exemption would license an unbounded loop.

---

## Exit and claim release

A session never resumes, so exit is always terminal for the agent's hold on a task.
Both primitives release everything they own on the way out:

- **Reviewer:** `releaseReviewOwnership` clears `ReviewingBy` and `ReviewLeaseExpires`
  on every interval expiry, final or not, while the wait's reservation holds.
- **Doer:** ownership (`agent.CurrentTask`) is cleared per expiry, and on final budget
  exhaustion the *assignment* — `assigned_to` and `lease_expires` — is released too, via
  `ReleaseDepartedDoerAssignment`. Status, worktree, commits and the submitted attempt
  are untouched, so a submitted task stays reviewable. The release is a no-op unless the
  departing agent still holds the assignment.

Without that release the task would stay pinned to a departed agent until the lease
lapsed — up to the lease duration of stall on a task nobody is working.

The same reasoning removed the WAITING-doer preservation branch from
`resetAgentAfterExit`: it held a claim open for a session that could never return.

## Rationale for `ResumeSession` removal

`LLMAgentRunRequest.ResumeSession` was declared, assigned an empty string at the only
call site, and never read by any backend — a write-only field that a test mock echoed
back, making session resume look supported. It was removed rather than guarded so the
tree does not imply a capability that does not exist. `SessionID` (set to the task ID)
remains and is passed through unchanged.
