# ADR-0173: Pause Halts Work Admission

## Status

ACCEPTED — 2026-10-01.

## Context

`pause` set `config.mode: PAUSED`, and the supervisor's wait gate was the only
thing that read it. The gate failed open: any state read error was treated as
RUNNING. Nothing after the gate checked the mode again. A supervisor past the
gate, or one whose read failed, claimed work, started a provider turn and
merged while the system was PAUSED (D86: a reviewer claimed and reviewed
during a pause on 2026-09-25). That incident's trigger is inferred, not
logged: the gate then used a locked read with a 10s timeout, the same process
logged lock timeouts, and the swallowed read error is the only path consistent
with the logs. `pause` also returned
before provider starts already in progress had happened, so the operator could
not tell when the system was quiet. A circuit-breaker trip had the same gap.

## Decision

1. **Halt modes refuse admission at commit.** PAUSED and
   CIRCUIT_BREAKER_TRIPPED are halt modes (`SystemMode.HaltsWork`).
   `RequireWorkAdmitted` refuses, with `SystemHaltedError`
   (`ErrSystemHalted`, a precondition naming `resume`), inside the committing
   transaction of every doer and reviewer claim, of each write a claim
   triggers (stranded takeover, attempt rollover, blocked escalation), of
   merge preparation, and of provider start. An earlier check in a claim or
   merge only avoids needless work; the check in the transaction is the one
   that decides.

2. **Provider starts are ordered against the halt.** A provider start holds the
   project's `work-admission` file lock shared across its mode check and
   `start()`. `pause`, and `analyze` when it trips the breaker, commit the mode
   and then take the lock exclusively once, as a barrier. When they return,
   every start admitted before the halt has happened and no later start can be
   admitted. Lock order is agent lifecycle → work admission; no state or
   integration lock is held while waiting on it.

3. **The barrier is bounded and the retry is safe.** The barrier times out
   after 60s with `WorkAdmissionBarrierError`. The mode stays committed. `pause`
   from PAUSED changes nothing and waits on the barrier again, so the operator
   retries the same command.

4. **The supervisor gate fails closed.** A state read error while waiting at
   the pause gate counts as paused.

5. **A refused start releases its claim.** The supervisor records a gate
   refusal itself, because backends report it differently (ACPX masks it as
   exit 1). It releases the claim the turn held: a doer task returns to its
   initial status with `continuation` set, so the next claim resumes the same
   iteration (ADR-0170); a reviewer task returns to its submitted status. Spin,
   crash and runtime-failure trackers are reset, and a halt refusal is not
   counted as a claim failure.

6. **In-flight work finishes.** A running provider turn is not interrupted. A
   merge whose preparation committed before the halt completes. An interrupted
   preparation is left untouched while halted and resumes after `resume`. The
   reviewer merge loop stops its batch at the first halt refusal.

## Consequences

- After `pause` returns, no claim, provider start or merge preparation begins
  until `resume`. Turns already running keep using their claims.
- A reviewer released by a halt gets the existing 60s per-task reviewer
  cooldown on its next claim of the same task. Another reviewer is not
  affected.
- Every supervisor provider start takes one more file lock. It is shared, so
  starts do not wait for each other.
- `pause` may block for as long as an admitted start takes, up to 60s.

## Alternatives Considered

- **Fix only the gate's fail-open read.** This closes the incident's path but
  leaves every later step unchecked. A pause committed between the gate and a
  claim is still missed.
- **Check the mode before each step without a lock.** This narrows the window
  without closing it, and `pause` still cannot say when admitted starts are
  done.
- **Stop agents on pause (exit code 42).** This interrupts running turns and
  loses their work. The documentation described this behavior, but the code
  did not do it.
- **Refuse every merge while halted, prepared ones included.** This leaves a
  prepared merge half applied across the pause, and the recovery path would
  need its own exception.
