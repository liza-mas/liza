# Cluster 0132 - Human-Owned Goal Decisions with Independent Readiness Review

## Status

ADR generated: [0132](../0132-human-owned-goal-decisions.md). Selection and grouping approved on 2026-09-06; user intent supplied on 2026-09-06.

## Commit Set

- `73a50ca8` — feat(skills): add goal-writing workflow

Earliest author timestamp: 2026-09-04T17:35:32+02:00. Decision implementation range: 2026-09-04 to 2026-09-04.

## Reconstructed Decision

Prevent AI-authored goals from presenting unmade product or structural decisions as settled human intent.

Entry-point-specific human ownership, elicitation and challenge before drafting, persistent decision provenance, and a cold independent final readiness review.

**Rationale provenance:** User-confirmed intent: Liza MAS is intended to run without human-in-the-loop intervention. That requires a solid goal document; agents helping humans write goals and check readiness support that requirement. Other explanations remain source-derived or reconstructed.

## Evidence

- skills/goal-writing/SKILL.md:14-18,34-46,52-70,74-94,98-128 (delegated verified).
- support-docs/how-to-produce-a-goal.md: Run the goal-writing skill; Review thoroughly; Get reviews from other agents.

Source reads were targeted to the named commit diffs and current sections; they do not establish the human's historical alternatives or acceptance of tradeoffs.

## Related Decisions

ADR-0013, ADR-0034, ADR-0094. Existing IDs stay unchanged; this candidate is numbered chronologically within the post-0097 backfill batch.

## User Context — 2026-09-06

Liza MAS is intended to run without human-in-the-loop intervention. That requires a solid goal document; agents helping humans write goals and check readiness support that requirement.

## Remaining Historical Gaps

The user did not identify separate incidents involving invented decisions or author self-certification.
