# Cluster 0233 - Bounds, Not Benchmarks

## Status

ADR generated: [0233](../0233-bounds-not-benchmarks.md). Selected 2026-10-25.

## Commit Set

- `c1619f04f` — feat(skills): bound requirements from above and challenge overkill (D96, D97)
- `5704e5c83` — feat(skills): make unnamed assurance machinery a human decision (D97)

Earliest author timestamp: 2026-09-27T00:01:05+02:00. Range: 2026-09-27.

## Reconstructed Decision

Least costly reading that satisfies the source; unnamed guarantees and assurance machinery are the human's to add via an Open Question. The second commit supersedes the first's author-judged necessity.

## Evidence

- Both commit bodies read in full (D96, D97, SERA OP-010, DEV-239 ~61k lines); current text in `skills/shared/references/reference-first-authoring.md`.

## Related Decisions

ADR-0150, ADR-0132, ADR-0149.

## User Context — 2026-10-25

Human-decides is the intended outcome, not interim. Rationale: benchmarks tend to be overkill in common situations.
