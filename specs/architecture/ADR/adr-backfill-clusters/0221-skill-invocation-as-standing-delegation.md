# Cluster 0221 - Skill Invocation as a Standing Delegation

## Status

ADR generated: [0221](../0221-skill-invocation-as-standing-delegation.md). Selected 2026-10-25.

## Commit Set

- `0f0180404` — feat(operator): make a standing delegation for rulings the default (operator skill, PAIRING_MODE Contract Authority clause, failure-mode map GAM-5/GAM-6, multi-agent usage doc)

Earliest author timestamp: 2026-10-16T17:51:24+02:00. Range: 2026-10-16.

## Reconstructed Decision

Only a human-invoked skill can grant a standing delegation, within its stated bounds; the operator skill makes delegated rulings its default with a hold list, a per-run decision record, and a separate explicit auto-approve mode.

## Evidence

- Commit body read in full; contract diff verified (`git show 0f0180404 -- contracts/PAIRING_MODE.md`); GAM-5/GAM-6 rows read.

## Related Decisions

ADR-0147, ADR-0132, ADR-0154.

## User Context — 2026-10-25

The PAIRING_MODE change is protection against prompt injection. Delegation is better framed once, in a controlled way, than restated per run.
