# ADR-0207: Admission Budget Exceptions

## Status

ACCEPTED — 2026-10-13. Extends the unblock carrier of
[ADR-0206](0206-in-task-scope-exceptions.md) to the canonical admission batch
budget of [ADR-0134](0134-acceptance-evidence-admission.md).
Amended by [ADR-0216](0216-admission-timeout-retry-policy.md): an operator
policy may re-run a submit's timed-out batch once at a raised budget.
Related: [ADR-0176](0176-execution-timeout-defers-to-inflight-submit.md),
[ADR-0204](0204-deferred-validation-tier.md).

## Context

Strict admission runs a coding task's canonical commands within one batch
budget, the reviewed contract's `timeout_seconds` (default 600, cap 14400).
When a suite outgrows that budget, admission times out and the only way to
change the value is a reviewed carrier change: a code plan, a plan review, a
hand-off and a supersession or adoption, often blocking dependents. On one day,
six admissions timed out on the same database suite and eight tasks needed
corrections that changed nothing but `timeout_seconds`; one of them headed 41
consumers (D-145).

## Decision

1. **Carrier.** `unblock-task <id> --budget-exception <seconds> --budget-ruling
   <decision-id>` (orchestrator or operator) records
   `budget_exception: {timeout_seconds, ruling}` on the `unblocked` history
   entry. Both flags are required together; `timeout_seconds` is 1..14400 (the
   engine cap); the ruling is a nonblank single line of at most 256 bytes. Only
   tasks whose doer role is `coder` accept it. It joins the lifecycle request
   identity, so exact replay is a no-op. Every other unblock gate applies.
2. **Admission reads it.** The admission batch budget is the larger of the
   reviewed `timeout_seconds` and the largest recorded exception. It governs
   the batch timeout and the in-flight submit marker that holds the doer
   session open. Commands, order, manifest, proofs, deferrals and the reviewed
   contract stay as reviewed: receipts and `deferred[].timeout_seconds` keep
   the reviewed value. An exception never lowers the budget; exceptions
   accumulate for the task's life.
3. **Authority.** The ruling names the human decision that authorizes the
   raise. The engine records it as given and does not resolve it against a
   decision ledger; the orchestrator asks the human (`assess-blocked
   --human-action`) when no ruling exists. The blocked-task wake routes a
   timeout-only admission failure here instead of to a correction plan.

## Consequences

- A timeout-only budget correction costs one unblock and a resubmission
  instead of a plan, a review and a hand-off.
- A larger budget can change the outcome of time-sensitive commands; this is a
  bounded execution-budget exception, not a behavior-neutral one. Acceptance
  is otherwise unchanged and the reviewer still sees the executed receipt.
- The deferred-validation runner keeps the reviewed budget for deferred
  commands.
- Only a BLOCKED task can receive an exception, so each task pays its first
  timeout. A pre-emptive grant for open tasks sharing the failing command is
  not provided.
- Exceptions are auditable from task history; an older binary ignores the
  field and applies the reviewed budget.

## Alternatives

- **Raise deferral budgets too.** Rejected: it changes receipt deferral
  identity and could strand a submitted receipt.
- **Config-wide per-command budget ruling.** Rejected: it bypasses the
  per-task human ruling and the coding-task gate.
- **Reviewed amendment of the contract.** Rejected: it keeps the planner and
  review cycle this decision removes.
