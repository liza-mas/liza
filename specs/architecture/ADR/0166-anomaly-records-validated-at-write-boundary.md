# ADR-0166: Anomaly Records Are Validated at the Write Boundary

## Status

ACCEPTED — implemented 2026-09-30. Follow-up named in
[ADR-0165](0165-mutation-validation-refuses-only-introduced-violations.md)'s
residuals.

## Context and Problem Statement

Anomaly type and detail rules were enforced only by `liza validate` and by the
mutation paths that validate their whole candidate (ADR-0165). The ordinary
writers that append anomalies (provider audit, agent health, the reviewer-claim
breaker, obligation drift, pending-merge stalls, stale and failed verdicts) ran
no entity validation. D83's record, a `retry_loop` without `count` and `error_pattern`,
got in that way, and was only noticed when it vetoed unrelated mutations.

Every one of those writers goes through `Blackboard.Modify`, so a check there
covers all of them, including ones added later.

## Considered Options

1. **Validate in each writer.** Each caller checks the record it builds. A new
   writer that forgets the call reopens the gap.
2. **Refuse any invalid anomaly in the written state.** Simple, but a legacy
   invalid record would then veto every write, the D83 failure ADR-0165 removed.
3. **Refuse only the anomaly violations a transaction adds, at `Modify`.**
   ADR-0165's rule, applied to anomalies on every transaction.

## Decision Outcome

**Option 3.**

- **One rule.** `models.AnomalyViolations` owns the recognized types and each
  type's required details. `statevalidate.validateAnomalies` and the write
  boundary both call it, so `validate` and writers cannot disagree. It lives in
  `models` because `db` cannot import `statevalidate`, which imports `db`.
- **Identity.** One violation per unknown type and one per missing required
  detail, owned by the record's index. Anomalies are append-only, so an index is
  a stable owner. A detail is missing when absent, `nil`, or a nil pointer: those
  persist as `null`. Nil or empty slices and maps persist as `[]` or `{}` and
  count as present.
- **Boundary.** `Modify` checks the state `fn` produced before marshalling it.
  If it has anomaly violations, the locked pre-image is decoded and its
  violations are subtracted as a multiset. Any remainder refuses the
  transaction with `anomaly write refused: …` and nothing is written. The valid
  path costs one scan and no decode.
- **Legacy records.** A pre-existing violation never refuses a write, and
  filling one of a record's missing details is a partial repair that passes.
  A second copy of an invalid record adds its own violations and is refused.
  Clearing a required detail of a valid record in place is refused.
- **Fail closed.** If the pre-image cannot be decoded, the write is refused.
- **`Write` is exempt.** Its callers are `init`, which writes no anomalies,
  and `migrate`, which must be able to write states that still hold legacy
  records.

## Consequences

- No `Modify` transaction can add an unknown anomaly type or a record missing
  a required detail, whichever writer builds it.
- Legacy invalid records stay readable, writable around, and repairable one
  detail at a time; `validate` still reports them.
- A writer bug now fails the writer's transaction instead of persisting a
  record that later confuses analysis.
- Residuals:
  - Which details each type requires is unchanged; ten recognized types still
    require none ("Anomaly Detail Validation Incomplete" in
    `architectural-issues.md`).
  - Existing records are not migrated.
  - Other entities written outside the ADR-0165 mutation paths are still not
    validated on write, and reads still assume well-formed fields.
  - Tests and `migrate` can still write invalid anomalies through `Write`.

## Related Decisions and Provenance

Extends [ADR-0165](0165-mutation-validation-refuses-only-introduced-violations.md)
to anomaly writers. Mitigates "Well-Formed Blackboard State" in
`specs/architecture/architectural-issues.md`.

---
*Recorded 2026-09-30.*
