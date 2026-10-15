# ADR-0210: Submit Progress and the Same-Caller Submit Hold

## Status

ACCEPTED — 2026-10-15. Amends ADR-0209 §5 (a hold is taken before pre-admission).

## Context

Doers run `submit-for-review --json`, which discards the logger, so a submit
printed nothing while it queued for admission slots (up to the cap minus the
budget) and then ran its batch. Doers measured their batch budget from the
call, read the silence as a stall, interrupted the submit and blocked; one
frames admission was lost mid-run (D-188). Another doer started a second
submit for a task whose first was still running: it took its own queue place
and, once at the head, a slot, only to time out on the task's worktree lock.
Had the first ended without a receipt meanwhile, it would have rerun the batch
on the same commit.

## Decision

1. **Progress on stderr.** A submit reports what it is doing on stderr —
   queued for an admission class (with the tickets ahead and that the batch
   budget has not started), preparing
   (rebase, index refresh), running the canonical batch (with its budget) —
   once per change and then every 5 minutes, each line with the time since
   the submit started and, when known, the latest end of that state. Lines
   are written directly, not through the logger, so `--json` keeps them;
   stdout keeps the lifecycle result alone. Command output is never echoed.
   The reporter stops before the submit returns. Other admission callers
   (deferred runner, `update-review-commit`, heal) report nothing.
2. **Same-caller submit hold.** Each submit takes an exclusive flock per task,
   agent and generation (`<state>.submit-<hash>`) before pre-admission and
   keeps it until it returns. A submit finding it held queues and prepares
   nothing and returns at once `STATE_CHANGED`/`requery` with effects
   `unknown` — the outcome a duplicate already got once the first had
   recorded its preparation; the hold moves that refusal ahead of the slot
   queue. The running call's result settles the request. Another agent or a
   restarted generation is not held: an abandoned submit must not stall the
   new owner, and the ownership fence and preparation checks still govern
   it. A killed submit's hold frees with its process, so retries proceed; the
   hold is not passed to canonical commands.

## Consequences

- A queued or running submit is distinguishable from a hung one in every
  output mode; whether a harness forwards stderr before the call ends is the
  harness's concern (RTK may buffer).
- A duplicate submit costs no queue place, slot or batch. A doer that keeps
  resubmitting gets instant refusals, so the loop costs nothing shared; the
  prompt says to wait for the running call instead.

## Alternatives Considered

- **Make the duplicate wait for the first, then requery.** Avoids a resubmit
  loop, but contradicts the immediate refusal of a pending duplicate that
  callers already rely on.
- **Wait, then proceed normally.** Replays a completed first call, but reruns
  a full batch on the same commit when the first failed without a receipt.
- **One hold per task for every caller.** Also covers a reassigned task, but
  stalls a restarted session behind its abandoned predecessor.
- **Guard on in-flight markers.** Markers exist only for authenticated
  callers and, without a matching admission class, only after preparation.
- **Stream canonical command output as the heartbeat.** Shows liveness, but
  can expose unmasked secrets and floods the caller's context.
