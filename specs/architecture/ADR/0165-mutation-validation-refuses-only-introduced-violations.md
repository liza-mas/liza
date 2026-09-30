# ADR-0165: Mutation Validation Refuses Only Introduced Violations

## Status

ACCEPTED — implemented 2026-09-30 (D84). The partial-repair policy (option 2
below, rather than option 3) was chosen by the maintainer on 2026-09-30 after a
pros/cons comparison.

## Context and Problem Statement

About fifteen mutation paths in `internal/ops` (rejected-task reclaim,
supersede, retarget, dependency repairs, assess-blocked, replace-task, plan
replacement, release-claim, rejection RCA, narrow-inherited-dependencies,
integration recovery and the integration verdict) validated the whole candidate
state before persisting it, and refused on any error. The validator stopped at
the first violation.

D83 showed the cost. One `retry_loop` anomaly written by an older reviewer
without its required details, and attached to no task, refused every rejected-task
reclaim run-wide, along with supersede, retarget and assess-blocked. The D83 fix
migrated that one record shape. The weakness stayed: any invalid persisted
record, whatever wrote it, vetoed every mutation, including ones that never
touched it. And because validation stopped at the first violation, an old
record also hid what the mutation itself got wrong: a retarget that closed a
cycle was reported as the unrelated old record.

## Considered Options

1. **Status quo: whole-state fail-first.** One bad record stops rework and
   orchestrator graph repair until a code fix and `migrate` land.
2. **Refuse only the violations a mutation adds.** Validate the candidate in
   full, subtract the violations the pre-mutation state already had, refuse on
   the remainder, and report the rest as pre-existing.
3. **Option 2, but also refuse when a record the mutation changed is still
   invalid.** Stricter, but a record with two independent defects could only
   be repaired by fixing both in one write; a partial repair that adds nothing
   would be refused, and an operator fixing one field at a time would be stuck.
4. **Quarantine malformed records.** Moves records out of the live state
   automatically; a larger change with its own data-loss risks, still open in
   `architectural-issues.md` ("Well-Formed Blackboard State").

## Decision Outcome

**Option 2.** `statevalidate.ValidateCandidate(candidate, baseline, …)`
replaces whole-state `ValidateState` on the mutation paths.

- **Collect, don't stop.** Validators report every violation, across
  validators, across records and within one record. Grouped requirements are
  split into independent violations (one per missing anomaly detail, per
  missing status field, per unmet dependency), so adding one missing field is a
  repair, not a new violation. `liza validate` stays strict and now lists all
  violations.
- **Identity.** A violation's identity is its owner and its constraint. It
  defaults to the message. Where the message names incidental context that a
  legitimate mutation changes, such as the task's current status, a duplicate's
  index, or the other members of a conflict, an explicit identity leaves it
  out. A task moving between two statuses that share a requirement therefore
  keeps one violation instead of trading an old one for a new one. Nested
  entries are owned by a stable key:
  - an ID;
  - a lifecycle receipt's sequence;
  - an index into an append-only or immutable list: coverage, attestations,
    generations, contributing scopes, receipt commands.
  The same defect on two entries is therefore two violations.
- **Relations.** A duplicate assignment is one violation per conflicting pair.
  A dependency cycle is one violation per edge that lies on a cycle, so a new
  cycle cannot hide behind an old one through the same tasks.
- **Shared helpers.** The validation-command, validation-prerequisite and
  doer-ownership helpers in `models` expose collecting variants. Their
  single-error forms return the first element, so other callers are unchanged.
- **Multiset comparison.** Identities are counted: a second occurrence of an
  existing violation is new. A violation the baseline had and the candidate
  lacks is repaired and not mentioned.
- **Baseline.** Inside `Blackboard.Modify`, `ReadSnapshot` returns the locked
  pre-image. It is loaded only when the candidate has violations, so the valid
  path costs one validation, as before. If it cannot be loaded or validated,
  every candidate violation is returned (fail closed).
- **One clock.** Both passes use one timestamp, so a lease expiring between
  them cannot make an unchanged violation look new.
- **Reporting.** Pre-existing violations go to stderr as
  `WARNING: pre-existing state violation, not caused by this change: …`.
- **Replay.** The integration-recovery replay writes nothing, so it checks only
  its own recovery receipt (`ValidatePrematureRecovery`), not the whole state.

## Consequences

- An invalid record no longer stops unrelated rework, graph repair or
  verdicts. It stays visible: `liza validate` fails on it, and each mutation
  warns about it.
- A mutation's own error is reported even behind an older one.
- A record can be repaired one field at a time.
- A mutation can leave a pre-existing violation in place indefinitely; only
  `validate` and the warnings surface it.
- Residuals, unchanged by this decision:
  - State hygiene is enforced on every write, so a hygiene violation still
    refuses any write.
  - `ValidateIntegrationLifecycleTransition` stays fail-first, because it
    compares two lifecycle snapshots of one transition, not the whole state.
  - Checks that validate only the task being added or changed
    (`ValidateAddedTask`) are unaffected.
  - `add-task` never refused on whole-state failure; it reports
    "state remains degraded" in its result warnings, with whole-state
    semantics, and keeps doing so.
  - Stored rejection-RCA text fields keep the request validator's documented
    one-diagnostic-per-field contract, shared with the record and resume
    request boundaries. The supported writer validates its payload with that
    same validator before the transaction, so no supported mutation can add a
    defect that hides behind an old one in these fields. A hand-edited record
    can still carry a second defect in one field that stays unreported until
    the first is fixed.
  - Ordinary writes that bypass these mutation paths still do not run entity
    validation, which is how D83's record got in. Anomaly writers are now
    covered by [ADR-0166](0166-anomaly-records-validated-at-write-boundary.md),
    which applies this decision's introduced-violation rule to anomalies in
    every `Modify` transaction.

## Related Decisions and Provenance

Qualifies [ADR-0114](0114-terminal-dependency-repair.md), whose rejected
option 2 reasons from "full-state validation remains fail-closed": the full
candidate is still validated, but only introduced violations refuse it.
Mitigates "Well-Formed Blackboard State" in
`specs/architecture/architectural-issues.md`. D83 fix: commit 7827888c5.

---
*Recorded 2026-09-30.*
