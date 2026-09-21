# 147 - Bounded Operator Notes and the Human-Note Wake

## Status

ACCEPTED — implemented 2026-09-13 to 2026-09-14; backfilled 2026-09-21.

## Context and Problem Statement

An operator watching a run had no supported way to deliver recovery input to the
orchestrator. Assessed orchestrators already reacted to targeted human notes, but
nothing wrote them: the only route was hand-editing state, which no command
sanctioned and no provenance recorded.

Adding the write half exposed two further gaps. A note reached the orchestrator
only through a blocked task, because `isTaskActionableSinceAssessment` treats a
note newer than the last assessment as activity on that task. A note for a task
that was not blocked, or addressed to `all`, woke nothing — the orchestrator could
stay idle indefinitely with an operator request sitting in `state.human_notes`, and
no wake verb could force a turn. Separately, a fresh note could wake an assessed
orchestrator without reaching its decision: the CLI had no `human_notes` query, and
the prompt did not require reading the input before another assessment. One
observed run ignored a recovery request for a draft task whose acceptance claim had
failed.

## User-Confirmed Intent

The user hit the idle-orchestrator case directly: a note was recorded and the
orchestrator never turned. The work is a response to observed behavior, not a
reasoned-from-code precaution.

## Considered Options

1. **Route all operator input through the blocked-task path** — keep the single
   existing channel and require the operator to target a blocked task.
2. **Add a wake verb the operator invokes explicitly** — let the operator force a
   turn as a separate action from leaving the note.
3. **Make the note itself a wake trigger, with seen-stamping** — one action for the
   operator, with delivery tracked per note.

## Decision Outcome

Chose **Option 3**, in three parts.

**Write.** A local operator-only file-input CLI appends bounded notes under the
state lock, records provenance, and preserves task status, approval and existing
notes. It rejects identified agents and invalid input, returns compact
acknowledgements that do not echo note contents, and retains committed success if
activity logging fails.

**Wake.** A `HUMAN_NOTE` trigger ranks after `IMMEDIATE_DISCOVERY` and before
`PLANNING_COMPLETE`, in both the supervisor detector and the prompt builder so the
two cannot disagree. A note is unseen until `orchestrator_seen_at` is stamped on
it. The `HUMAN_NOTE` turn renders every unseen note verbatim, with instructions to
carry the request out as written and stop at the first failure.

**Read-before-decide.** The existing `get` command exposes ordered note contents
and bounded provenance, and the prompt requires note inspection before another
reassessment, retaining scope, authority and review gates.

Stamping happens in `PostExecution`, which runs only on exit code 0, so a failed
turn shows the note again rather than dropping it. Only notes that existed when the
prompt was built qualify, `human_notes` being append-only. A `HUMAN_NOTE` turn
stamps everything it rendered; a turn woken by a higher-ranked trigger stamps only
the notes it consumed — those targeting a task, or `all`, that gained an assessment
during the turn — because the blocked-task instructions read `human_notes` before
assessing, and re-rendering those as fresh requests would execute them twice.

Audit notes written by `delete-task`, `recover-task`, `delete-agent` and
`recover-agent` are stamped at creation: they are records, not requests, and those
commands never woke the orchestrator before.

## Rationale

Option 1 preserves the defect the user hit — the channel exists but only for tasks
already in a particular state. Option 2 makes delivery an operator responsibility
that the operator has no way to verify; the note is written, and whether it arrived
is unobservable.

Seen-stamping is what makes a single action safe. Without it, the choice is between
a note that can be dropped (stamp on render) and one that can be executed twice
(never stamp). Tying the stamp to exit code 0 and to consumption by higher-ranked
triggers resolves both directions.

## Consequences

- An operator has one supported verb for recovery input, with provenance and
  bounded size, and does not hand-edit state.
- The detector and the prompt builder must keep their trigger ranking aligned;
  divergence silently changes which notes get stamped.
- Notes recorded before this build are unseen and produce one catch-up wake, which
  may include legacy audit entries.
- Acknowledgements deliberately omit note contents, so an operator confirming what
  was recorded must query rather than read the command output.
- The orchestrator cannot reassess without inspecting notes first, which adds a
  read to every reassessment turn.

## Evidence and Reconstruction

Sources: commits `417c80f1e`, `cd2abc66a`, `1866c29fa`. The user confirmed on
2026-09-21 having hit the idle-orchestrator behavior. The options above are
reconstructed; no historical alternative evaluation was supplied.

---
*Reconstructed from commits `417c80f1e`..`1866c29fa` (2026-09-13 to 2026-09-14).*
