# ADR-0170: Continuation Is the Default Unblock

## Status

ACCEPTED — 2026-10-01. Amends [ADR-0145](0145-rejection-rca-gate.md) (the
direct-assignment mechanism that avoids the iteration increment) and
[ADR-0080](0080-claimable-rebase-unblock.md) (`--assign-to` as the
non-incrementing fast path).

## Context

`unblock-task` had two restore forms, and each fused two independent choices:

- without `--assign-to`, the task returned to its role-pair initial status and
  the next claim incremented `iteration`;
- with `--assign-to`, it returned straight to the executing status without an
  increment, but only for a registered, idle doer of the right role.

So the only way to resume without consuming an iteration was to name an idle
doer at unblock time. A repair that must not consume an iteration — a
`capability_reroute` or `lifecycle_repair` disposition, or work interrupted by a
quota kill — waited for a doer to be idle. Until then the task stayed `BLOCKED`:
it was not claimable demand for pool repair, and nothing re-woke the
orchestrator when a doer freed. An operator run deadlocked this way until a human
started a doer. The same fusion made prerequisite-bearing `assign`-mode tasks
unrestorable (`--assign-to` is refused with `validation_prerequisites`), and let
a `claimable` product correction skip its increment through `--assign-to`.

Only rejected claims check the iteration limit; fresh and preserved-initial
claims never did. The increment on an unblock therefore never bounded
block/unblock cycles.

## Decision

Each `unblock-task` option states one effect:

| Option | Effect |
|--------|--------|
| (default) | Continuation: the resume consumes no iteration. |
| `--new-iteration` | The resume consumes an iteration. |
| `--assign-to X` | X resumes now, in the executing status; otherwise any doer claims. |

An unassigned continuation sets the task field `continuation`. The next claim
skips its increment only when the field is set, the claim resumes the preserved
worktree (preserved-initial strategy), and `iteration > 0`; the claim always
clears the field and records `continuation: true` in its history entry. Every
attempt-state reset (`clearAttemptState`: fresh recovery, review-commit reset,
new attempt, claim release, retirement) clears it, because the preserved work it
was granted for is gone. An assigned unblock does the claim's accounting itself:
it clears the field and increments only with `--new-iteration`.

The rejection-RCA gate fixes the iteration choice and leaves the doer choice
free: restore mode `assign` (`capability_reroute`, `lifecycle_repair`) refuses
`--new-iteration`; `claimable` (`implementation_correction`, `human_override`)
requires it. The stored mode values are unchanged.

## Consequences

- A non-product resume is ordinary claimable work: pool repair staffs it and any
  doer claims it, so no idle doer or orchestrator re-wake is needed.
- Prerequisite-bearing `assign`-mode tasks restore unassigned; their supervisor
  claims with a fresh target-session preflight.
- A product correction always consumes an iteration, assigned or not.
- Breaking change: a flagless `unblock-task` no longer leads to an increment.
  Callers that want one pass `--new-iteration`.
- Block/unblock loops stay unbounded by the iteration limit, as before; they no
  longer push `iteration` toward the limit the next rejected claim checks.
- ADR-0145's commitment that "assignment, not the advisory exemption field"
  prevents the increment is replaced: the `continuation` field is written only by
  the guarded unblock transaction and consumed atomically by the next claim.
  `iteration_exempt` stays an audit record.
- Re-claims after `release-claim` or a dead owner still count an iteration.

## Alternatives Considered

- **Count blocked repairs as pool demand and re-wake the orchestrator when a doer
  frees.** Treats the symptom; keeps the coupling and the unrestorable
  prerequisite case.
- **A per-task continuation budget.** Would add a loop bound that never existed;
  declined for parity.
- **Rename the stored restore modes.** Clearer names, but needs a state
  migration; the meaning is documented instead.
