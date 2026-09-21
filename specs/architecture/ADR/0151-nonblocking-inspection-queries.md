# 151 - Nonblocking Inspection Snapshots and Field Queries

## Status

ACCEPTED — implemented 2026-09-21; backfilled 2026-09-21.

## Context and Problem Statement

Operational `status` and `get` calls competed with supervisor writes and reloaded
locked runtime policy once per task. Inspection — the thing an operator reaches for
when a run is already struggling — added load to the lock that the run needed.

Inspection also disagreed with itself. The TUI, `get-tasks` and dotted field
queries each computed `time_in_status` their own way, and in a live run of 93 tasks
they agreed on 4. The TUI measured from the last history entry of any kind, so a
claim release or a transition recorded on a trigger task reset the displayed
duration by up to 31 hours; the field query read time on the task itself,
reporting zero for a task that had never been claimed. No spec said what the value
meant, so nothing caught the drift.

## User-Confirmed Intent

The user confirms the lock contention was observed, not anticipated. The
`time_in_status` divergence is measured in the commit body against a live run.

## Considered Options

1. **Keep per-task policy reloads and accept the contention** — inspection stays
   correct-by-construction against current state.
2. **Cache inspection results** — serve repeat queries without touching the lock.
3. **Read one uncached atomic snapshot per call and derive everything from it.**

## Decision Outcome

Chose **Option 3**. A call reads one uncached atomic snapshot and derives
transition policy from it, rather than reloading locked runtime policy per task.

The query surface gains dotted task fields, `get-tasks`, and field projections with
consistent nested output and lifecycle redaction.

`models.TimeInStatus` becomes the single definition, built over an explicit event
classification and called from all three surfaces. Every declared task event is
classified as status-moving or not; an unknown event name returns
`classified=false`, so vocabulary drift surfaces instead of silently shifting a
duration.

## Rationale

Option 2 trades contention for staleness, which is the wrong trade for the case
that motivates inspection: an operator diagnosing a live run needs the current
value, and a cached answer that is quietly old is worse than a slow one. A single
atomic snapshot removes the contention without introducing staleness within a call,
and makes every derived value in that call mutually consistent by construction.

Returning `classified=false` for unknown events, rather than defaulting them to
non-moving, follows the same principle as the rest of this record: a wrong duration
that looks plausible cost 31 hours of apparent stall time and went unnoticed
because nothing declared what the number meant.

## Consequences

- Inspection no longer competes with supervisor writes, and policy is resolved once
  per call rather than once per task.
- The TUI, `get-tasks` and field queries report the same `time_in_status`; the
  previously displayed TUI durations were wrong, not merely different.
- A new task event must be classified, or queries report it as unclassified — a
  visible failure rather than a silent duration shift.
- Field projections and lifecycle redaction become part of the query contract; a
  field added to the model is exposed unless redaction says otherwise.

## Evidence and Reconstruction

Sources: commits `e45849438` and `019b4d077`, including the 93-task agreement
figure and the 31-hour reset. The user confirmed on 2026-09-21 that the contention
was observed. Historical alternatives are reconstructed.

---
*Reconstructed from commits `e45849438`..`019b4d077` (2026-09-21).*
