# Cluster 0129 - Proportional Circuit-Breaker Responses with Explicit Acknowledgement

## Status

ADR generated: [0129](../0129-proportional-circuit-breaker-responses.md). Selection and grouping approved on 2026-09-06; user intent supplied on 2026-09-06.

## Commit Set

- `0acece1b` — fix(circuit-breaker): preserve proportional active responses

Earliest author timestamp: 2026-08-27T11:19:04+02:00. Decision implementation range: 2026-08-27 to 2026-08-27.

## Reconstructed Decision

Prevent historical provider evidence from repeatedly halting runs, and prevent active safety responses from being silently weakened.

Typed WARNING/CHECKPOINT/HALT responses, exact current registration-health proof for provider HALT, persistent active response and resume-only acknowledgement/release.

**Rationale provenance:** User-confirmed intent: Repeated halts from old evidence drove the circuit-breaker redesign. Other explanations remain source-derived or reconstructed.

## Evidence

- specs/protocols/circuit-breaker.md:166-217 and 397-446 describe acknowledgement, current-registration HALT proof and latched response (delegated verified).
- 0acece1b protocol/production diff.

Source reads were targeted to the named commit diffs and current sections; they do not establish the human's historical alternatives or acceptance of tradeoffs.

## Related Decisions

ADR-0010, ADR-0053, ADR-0085. Existing IDs stay unchanged; this candidate is numbered chronologically within the post-0097 backfill batch.

## User Context — 2026-09-06

Repeated halts from old evidence drove the circuit-breaker redesign.

## Remaining Historical Gaps

The separate historical rationale for choosing checkpoint on uncertain current health was not supplied.
