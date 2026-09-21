# Cluster 0155 - Spec Reconciliation Tested at Review

## Status

ADR generated: [0155](../0155-spec-reconciliation-at-review.md). Selection approved on 2026-09-21; no separate user intent supplied.

## Commit Set

- `d12866d8b` — feat(contracts): reconcile specs with changed behavior, and let review see it

Earliest author timestamp: 2026-09-21T11:59:07+02:00. Decision implementation range: 2026-09-21.

## Reconstructed Decision

Widen the Doc Impact question so either an additive sibling test or an alters-documented-behavior test can falsify "none", and add the missing observer: the review checklist tests the declaration, using the Change Summary the reviewer already receives. Severity stays with the reviewer — "none" is judged defensible, not correct.

**Rationale provenance:** Commit-evidenced. The body frames the failure as mis-scoped tests for three instructions that already existed, names blocker-pinning as the over-correction, and states the scope left untouched.

## Evidence

- `d12866d8b` commit body, read in full.
- The self-confirmation argument: the declaration was made at DoR and confirmed at DoD by the same agent, and neither `code-review` nor `spec-review` compared a diff against existing specs.

## Related Decisions

ADR-0123 (two-sided bounded review) owns the reviewer-obligation surface this extends; ADR-0154 adds the decision-record Doc Impact category to the same declaration.

## User Context — 2026-09-21

None supplied.

## Remaining Historical Gaps

No record of whether the MAS separate-doc-task gate was discussed alongside this change; the commit states only that it remains unaddressed.
