# Cluster 0128 - Separate Routine, Race and Coverage Validation

## Status

Not selected for the eight-record backfill approved on 2026-09-06. Analysis retained for possible future use; no ADR generated.

## Commit Set

- `b112476e` — perf(test): accelerate routine suite validation

Earliest author timestamp: 2026-08-24T09:12:07+02:00. Decision implementation range: 2026-08-24 to 2026-08-24.

## Reconstructed Decision

Reduce routine full-suite validation cost without removing the final race-validation obligation.

Keep make test full-suite but uninstrumented; separate test-fast, final test-race, and coverage profiles.

**Rationale provenance:** Measured rationale exists in docs; ADR-worthiness for repository development policy needs selection.

## Evidence

- b112476e Makefile diff; docs/TESTING.md:9-44; docs/PERFORMANCE.md:142 (delegated verified).
- TECH_DEBT.md:63 records deferred CI race wiring.

Source reads were targeted to the named commit diffs and current sections; they do not establish the human's historical alternatives or acceptance of tradeoffs.

## Related Decisions

ADR-0007, ADR-0017. Existing IDs stay unchanged; this candidate is numbered chronologically within the post-0097 backfill batch.

## Human Context Needed

- Should this development validation policy become an ADR or remain in testing/performance docs?
- Confirm routine passes no longer proving race freedom and deferred CI race wiring were accepted tradeoffs.
- Correct the reconstructed trigger/rationale and name any rejected alternatives, accepted limitations, or additional related decisions.
