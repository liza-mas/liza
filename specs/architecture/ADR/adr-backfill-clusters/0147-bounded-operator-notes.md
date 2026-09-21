# Cluster 0147 - Bounded Operator Notes and the Human-Note Wake

## Status

ADR generated: [0147](../0147-bounded-operator-notes.md). Selection approved on 2026-09-21; user intent supplied on 2026-09-21.

## Commit Set

- `417c80f1e` — feat(ops): add bounded operator notes for assessed blockers
- `cd2abc66a` — fix(orchestrator): read operator notes before reassessment
- `1866c29fa` — feat(agent): wake an idle orchestrator on unseen operator notes

Earliest author timestamp: 2026-09-13T18:29:39+02:00. Decision implementation range: 2026-09-13 to 2026-09-14.

## Reconstructed Decision

Give the operator one supported verb for recovery input and make the note itself the delivery mechanism: a bounded, provenance-recorded append under the state lock; a `HUMAN_NOTE` wake trigger ranked between `IMMEDIATE_DISCOVERY` and `PLANNING_COMPLETE` in both detector and prompt builder; and a requirement to inspect notes before another reassessment.

Seen-stamping in `PostExecution` (exit code 0 only) makes single-action delivery safe in both directions — a failed turn re-renders the note, and a higher-ranked turn stamps only what it consumed.

**Rationale provenance:** User-confirmed: the user hit the idle-orchestrator case. Commit bodies supply the mechanism reasoning and the ignored-recovery-request run.

## Evidence

- Commit bodies for all three commits, read in full.
- `1866c29fa` states the `isTaskActionableSinceAssessment` behavior that limited delivery to blocked tasks, and the double-execution hazard that motivates selective stamping.
- `cd2abc66a` names an observed run that ignored a recovery request for a draft task whose acceptance claim failed.

## Related Decisions

ADR-0150 (cause-based recovery, which the operator skill consumes). Audit-note stamping interacts with the delete/recover commands.

## User Context — 2026-09-21

I hit it.

## Remaining Historical Gaps

Whether an explicit wake verb was considered and rejected, or simply not raised, was not supplied. The ranking position between `IMMEDIATE_DISCOVERY` and `PLANNING_COMPLETE` has no recorded justification beyond the code.
