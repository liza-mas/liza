---
title: "RTK-buffered long foreground validation"
trigger: "Before backgrounding any validation, or when a long RTK-wrapped foreground validation returns a session ID but no incremental output"
keywords: [RTK, make test, write_stdin, session_id, buffered output]
date: 2026-08-21
---

## Context

Repository-wide validation such as `rtk make test-race`, `rtk make coverage`,
or a repeated race/shuffle package run can run for several minutes. RTK may
buffer successful package output while the execution tool exposes the
still-running process through a session ID.

## Failure Mode

Repeated empty foreground waits can look like a stalled command even while tests
are progressing. Interrupting the session solely because no output was emitted
turns a healthy validation run into an incomplete one and discards its final exit
status.

Backgrounding the validation and ending the turn is abandonment, not delegation.
The supervisor treats `end_turn` as session-complete: it releases the claim, and the
uncommitted worktree blocks the task on `preserved worktree is dirty`. The next agent
re-runs the same validation from scratch, and each cycle consumes one iteration of the
task's finite budget.

## Solution

Treat waits on one execution session as observation of the original foreground
command, not as validation retries. Continue waiting within the tool's documented
foreground mechanism, provide periodic status updates when required, and interrupt
only when there is independent evidence of a stall or a configured timeout expires.
Do not launch a duplicate validation command to obtain visible output, and never end a
turn with a validation still running. A repo-wide race or coverage run exceeds the 600s
foreground cap and the tool will force it to the background on its own; when that happens,
stay in the same turn and poll the job with TaskOutput until it exits, then commit and
submit. The completion notice never arrives, because the supervisor ends the session at
end_turn.

Account for repetition when choosing the timeout. For example, `go test
-race -count=10` runs ten copies inside one package process; the default
ten-minute package timeout may expire even when each individual iteration is
healthy. Measure one iteration, then set an explicit bounded timeout above the
expected aggregate duration.

## References

- [Project guardrails](../../GUARDRAILS.md)
- [Worktree build prerequisites](worktree-build-prerequisites.md)
