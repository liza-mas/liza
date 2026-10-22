# ADR-0216: Admission Timeout Retry Policy

## Status

ACCEPTED — 2026-10-23. Amends [ADR-0207](0207-admission-budget-exceptions.md)
(its rejected "config-wide ruling" alternative, for one bounded retry).
Related: [ADR-0209](0209-admission-limits.md),
[ADR-0176](0176-execution-timeout-defers-to-inflight-submit.md),
[ADR-0169](0169-runtime-input-provisioning.md).

## Context

When a submit's canonical batch reaches its budget, admission reports an
evidence refusal like any other. The only budget raise is ADR-0207's per-task
human ruling on a BLOCKED task, so each timeout costs a block, a ruling, an
unblock and a re-claim that loses the coder's context. On one run eight such
timeouts recovered either at a larger budget (standing rulings: twice the
budget, capped at 7200 s) or unchanged once outside load ended; none needed a
code change (D-165). A coder resubmitting is no remedy: a submit started in the
session's hand-back is cut off at its end (D-174), so the retry has to run
inside the submit whose marker already holds the session.

## Decision

1. **Policy.** `config.admission_timeout_retry: {budget_factor,
   max_timeout_seconds, ruling}` in state.yaml, operator-set. Absent means
   none. `budget_factor` is 1..14400 (1 is an unchanged rerun);
   `max_timeout_seconds` is 0..14400 (0 = the engine cap); `ruling` follows the
   `--budget-ruling` rules and names the standing human decision. An invalid
   policy is ignored, and a timeout under it says which field is invalid.
2. **Scope.** `submit-for-review` of a coding task only. Update-review-commit,
   the orphaned-base heal and the deferred runner never retry. A task declaring
   a `single_use` runtime input never retries, since the gate spent it.
3. **Retry.** When the batch reaches its own deadline (not a cancelled caller,
   an output overflow or a failing command), the whole batch re-runs once,
   from its first command, in the same submit, slots and runtime-input grant,
   provided the worktree is still clean. Commands must tolerate replay as for
   any resubmission: the engine checks Git cleanliness only, not external state.
4. **Budget.** R = factor × B (B the effective batch budget), lowered to
   `max_timeout_seconds`, the cap, and the submit envelope: slot wait (when a
   class matches), B and R stay within the cap plus the submit wait floor that
   `MaxInflightSubmitGrace` already covers. If R < B there is no retry. Every
   report states R and the bound that lowered it.
5. **Marker binding.** The in-flight marker covers wait + B + R from its first
   publication. Pre-admission records the B and R it published; preparation
   refuses any other as `STATE_CHANGED`/`requery` before any command runs. The
   policy is read from pre-admission's snapshot.
6. **Record.** A passing retry sets the receipt's `timeout_retry` (timed-out
   index, B, R, configured value, clipping bound, ruling), validated for shape
   only and shown to the code reviewer. Other receipt fields keep reviewed
   values.
7. **Exhaustion.** A timeout the policy settles without passing (the retry
   timed out, or no retry was eligible) is refused as `batch_timeout`, and the
   submit then blocks the task for a budget ruling. The block is fenced to the
   transition the submit's own retirement left, so a task released, unblocked
   or reclaimed meanwhile is not blocked.

## Consequences

- A contention timeout costs one longer submit instead of a block, a ruling,
  an unblock and a context-free re-claim; the doer never decides it.
- The retrying task keeps its slots, so queued tasks wait longer.
- A larger budget can change time-sensitive outcomes; the operator accepts that
  by setting the policy, and the receipt names its ruling.
- Budgets above cap − floor (7200 s with a class) get no retry; at 7200 s the
  retry runs at the same budget.
- Without a policy, behavior is unchanged.

## Alternatives

- **Fail, record an exception and let the doer resubmit.** Rejected: past the
  session timeout the resubmit dies at the hand-back's end.
- **Widen `MaxInflightSubmitGrace` to fit any k × B.** Rejected: it widens the
  session and Claude command ceilings of every run.
- **Resume from the timed-out command, reusing earlier passes.** Deferred:
  commands may depend on state their predecessors set up (TECH_DEBT "Evidence
  reuse").
