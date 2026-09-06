# Cluster 0123 - Two-Sided Review with Bounded Convergence

## Status

ADR generated: [0123](../0123-two-sided-bounded-review.md). Selection and grouping approved on 2026-09-06; user intent supplied on 2026-09-06.

## Commit Set

- `3f8c4a17` — fix(review): promote faster review convergence
- `ff603810` — feat(review): make code review two-sided and give disagreement a route
- `caf3f364` — fix(review): close gaps the first real reviews exposed
- `1de50e0d` — fix: fix skill reference

Earliest author timestamp: 2026-07-30T01:07:06+02:00. Decision implementation range: 2026-07-30 to 2026-08-09.

## Reconstructed Decision

Stop reviewer-only guidance and expanding corrective passes from producing endless review loops.

Author/reviewer response protocol with concrete-harm contests, four reviewer responses and routed escalation; full critical-defect inspection every round with bounded lower-severity follow-up.

**Rationale provenance:** User-confirmed intent: Real reviews showed doers fixing false-positive findings and non-converging cycles where fixing A broke B and fixing B broke A. Other explanations remain source-derived or reconstructed.

## Evidence

- 3f8c4a17 adds prior-feedback reconciliation and limits new blocking findings after round one.
- skills/code-review/SKILL.md:161-220 defines minimal fixes, contests and carriers; 225-273 defines continuation/independent review and convergence.
- ff603810 and caf3f364 bodies record reviewer-only guidance and feedback from real reviews.

Source reads were targeted to the named commit diffs and current sections; they do not establish the human's historical alternatives or acceptance of tradeoffs.

## Related Decisions

ADR-0001, ADR-0020, ADR-0026. Existing IDs stay unchanged; this candidate is numbered chronologically within the post-0097 backfill batch.

## User Context — 2026-09-06

Real reviews showed doers fixing false-positive findings and non-converging cycles where fixing A broke B and fixing B broke A.

## Remaining Historical Gaps

Whether the July and August changes arose from the same incident was not specified; their grouping remains the approved backfill grouping.
