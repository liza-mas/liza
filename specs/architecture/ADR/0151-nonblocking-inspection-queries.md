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

## Amendment (2026-09-24 to 2026-10-16): observation reads beyond inspection

Under lock saturation (D69, D100), the snapshot rule extends past `status` and
`get`. A read that feeds only display, observation or launch-input validation takes
a lock-free snapshot. Any mutation or authority decision that follows it re-reads
under the lock or is guarded by locked registration. A locked read releases the
lock before its caller acts, so its only extra guarantee is ordering after a writer
that holds the lock.

- `309594e94` (D69 step 2): the TUI state read, the watch tick, shell completion,
  `usage-report`, `launch --cli` validation, `validate` and the lifecycle-metrics
  sprint capture. The TUI had held the exclusive lock in 4 of 40 samples during the
  2026-09-24 run.
- `32dff6d2d` (D100): 24 supervisor observation, polling and gate reads. With a
  6 MB `state.yaml` these reads had saturated the lock, so submits, claims, merges
  and recoveries timed out.
- Kept locked:
  - `resolverOptionsFromState`, because it feeds the transition policy with no
    later locked revalidation (the gap is recorded in `TECH_DEBT.md`);
  - clean-task worktree cleanup, because its authority check is the only fence in
    front of `DeleteWorktree`;
  - the provider-start gate, which runs under the agent lifecycle lock.
- `69865939c`, `94810936d`: TUI reads are single-flight, and checks run only
  against a completed read's snapshot. Validation judges that same snapshot
  (`ValidateLoadedState`) rather than re-reading by path. A failed read keeps the
  last good snapshot and raises INVALID STATE once per episode.

The selection rule is documented in the snapshot section of
`blackboard-schema.md` and in the `db` package doc. Related: ADR-0177 (heartbeats
evaluated against snapshots) and ADR-0183/ADR-0215 (shrinking what each locked
cycle decodes). Reconstructed from the four commit bodies, read in full, on
2026-10-25; no separate user intent was supplied.

---
*Reconstructed from commits `e45849438`..`019b4d077` (2026-09-21).*
