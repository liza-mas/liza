# ADR-0176: Execution Timeout Defers to the Agent's Own In-Flight Submit

## Status

ACCEPTED — 2026-10-05.

## Context

`submit-for-review` runs a strict task's canonical acceptance commands
synchronously, inside the doer's tool call. A batch may take up to 3600 s. The
doer session's execution timeout (2 h for coders) counts from session start, and
the execution progress watchdog (default 30 min) sees nothing from a quiet
command. A submit started late in a session therefore raced both clocks. When it
lost, the supervisor exited and released the claim ("agent interrupted"). The
next claim then redid the whole batch, and could lose the race again.

The submit also survived the kill. The supervisor terminates the provider CLI's
process group, but Claude Code runs each Bash command in its own session. The
orphan finished its validation, contended for shared fixtures, and then failed
the final ownership check (operator incidents I-123 and I-144, defect D-38).

## Decision

1. **Fence.** During submit, canonical execution polls a lock-free state
   snapshot (every 15 s). It stops the command's process group as soon as the
   task is no longer executing, assigned to the submitter, and held under that
   submitter's current registration. Unreadable snapshots never stop it. The
   generation-fenced final transaction still decides admission.
2. **Marker.** After preparation, an agent-authenticated submit writes
   `<project runtime dir>/inflight-submit/<agent-id>.<nonce>.json`. The marker
   holds the task, the submit's PID, a digest of the registration generation,
   its start time, and a deadline fixed at write time: the batch timeout plus
   10 minutes. The submit removes its own marker on every return path. Because
   the name is unique per invocation, a returning submit can never remove the
   marker of a replacement registration using the same agent ID.
3. **Deferral.** The supervisor honors a marker only while it names the
   session's task and generation, its process is alive, and its deadline is
   ahead. In that case:
   - the progress watchdog counts the submit as progress;
   - when the execution timeout elapses, cancellation waits until the marker
     ends or reaches its deadline as first observed;
   - the wait never exceeds timeout + 70 minutes, and a later marker never
     extends it;
   - expiry is still reported as an execution timeout.

## Consequences

- A submit that starts before the timeout and fits its contract's batch timeout
  publishes before the session ends. The worst-case doer session grows from
  `ExecutionTimeout` to `ExecutionTimeout + 70 min`.
- A forged or stale marker buys at most that cap, and a dead submit process
  buys nothing.
- A released claim stops its orphaned validation within one poll interval. The
  result is `STATE_CHANGED`, or `STALE_CALLER` when the registration is gone.
- Validation time is still charged to the session and holds the coder slot.
  Provider quota termination still loses an in-flight submit. Asynchronous,
  task-owned validation remains the structural fix (TECH_DEBT).
- Canonical execution still runs under the task's ownership lock, so
  `release-claim`, cancel and supersede wait for the batch. The fence acts only
  on direct state ownership changes, such as supervisor exit or lease recovery.
- If the submit process itself is SIGKILLed, its command group is not stopped.

## Alternatives Considered

- **Fence only.** Stops the orphan, but the first late submit still loses its
  claim and the validation is repeated.
- **Asynchronous, task-owned validation.** Submit records the candidate and the
  engine validates outside the session. This removes the race for timeouts and
  quota stops alike, but needs a new task status, a runner and failure routing.
  Deferred.
- **Reuse evidence by commit SHA and command digest.** Rarely applies, because
  submit rebases onto a moving integration branch. It also does not protect a
  first validation.
- **Refuse a submit that cannot finish within the remaining budget.** Fails
  closed without progress, and the next session must reorient before
  submitting.
