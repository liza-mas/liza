# ADR-0176: Execution Timeout Defers to the Agent's Own In-Flight Submit

## Status

ACCEPTED — 2026-10-05.

Amended 2026-10-06 (D-56): the acceptance batch maximum is 14400 s
(`referencecontract.AcceptanceMaxTimeoutSeconds`), because real-database suites
under multi-agent load exceed one hour. `MaxInflightSubmitGrace` derives from it,
so the 3600 s batch and 70 minute cap below now read 14400 s and 4 h 10 min.

Amended 2026-10-11 (D-90): the provider harness has its own per-command
lifetime, which this decision did not cover. Claude Code moves a foreground
command to the background at its timeout (600 s) and stops it 30 minutes later,
and stops an explicit background command at its requested timeout, capped by
`BASH_MAX_TIMEOUT_MS` (2 h by default). Submits whose batch ran past about
40 minutes were killed mid-batch; their preparations stayed (effects unknown),
so same-generation retries were refused until an owner-ending operation.
Claude sessions now launch with `BASH_MAX_TIMEOUT_MS` raised to
`MaxInflightSubmitGrace` unless already higher, leaving the default timeout of
ordinary commands unchanged. `AGENT_TOOLS.md` tells agents to start
`submit-for-review` with `run_in_background: true` and that timeout. A
runtime-input declaration of the variable still strips it (ADR-0169).
Preparation retention after a kill is unchanged.

Amended 2026-10-14 ([ADR-0209](0209-admission-limits.md)): a submit that must
wait for an admission slot writes its marker before waiting, with window
`AcceptanceMaxTimeoutSeconds` + 10 minutes: the wait plus the batch never exceed
the acceptance cap, so the grace bound is unchanged.

Amended 2026-10-22 (D-174): cancelling as soon as the marker ended killed the
session before the agent could act on a non-admitted outcome; the claim was
released as `agent interrupted` and the next claim repeated the batch blind.
Liveness is now decided by a probe that ignores the marker deadline (marker
present for the task and generation, process alive). Once the submit is no
longer running, the session gets a fixed 10-minute hand-back, never extended by
later markers, so it ends no later than timeout + 4 h 10 min + 10 min. If the
session then ends with the task still executing under the agent, the supervisor
blocks it (fenced on generation, doer claim and transition): "admission
interrupted by session deadline" when a submit was still running at the end of
the grace or of the hand-back, "no longer observable … admission outcome
unknown" otherwise, since no typed outcome is recorded on the task. An admitted
or moved task is left alone and logged as a normal end. The progress watchdog
keeps its own clock during the hand-back; if it fires first it blocks the task
with its own reason. No path releases such a task silently.

Amended 2026-10-22 ([ADR-0209](0209-admission-limits.md), D-178): a submit's
admission-slot wait is never under a 2 h floor, so `MaxInflightSubmitGrace` is
`AcceptanceMaxTimeoutSeconds` + 2 h + 10 minutes (6 h 10 min). The session
then ends no later than timeout + 6 h 10 min + 10 min, and Claude sessions
launch with `BASH_MAX_TIMEOUT_MS` at 22200000.

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
