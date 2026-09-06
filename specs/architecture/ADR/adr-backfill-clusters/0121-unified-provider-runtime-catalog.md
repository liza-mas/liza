# Cluster 0121 - Unified Provider Catalog with Synthesized ACP Variants

## Status

ADR generated: [0121](../0121-unified-provider-runtime-catalog.md). Selection and grouping approved on 2026-09-06; user intent supplied on 2026-09-06.

## Commit Set

- `e592a40d` — feat(providers): unified provider catalog with ACP synthesis and disabled flag (#100)

Earliest author timestamp: 2026-07-22T18:55:20+02:00. Decision implementation range: 2026-07-22 to 2026-07-22.

## Reconstructed Decision

Avoid separate provider entries duplicating setup and detection metadata across CLI and ACP runtimes.

Declare runtime plus optional acp_runtime once per provider, synthesize <id>-acp entries, list both variants but detect only declared providers.

**Rationale provenance:** User-confirmed intent: Make adding a new provider more declarative. Other explanations remain source-derived or reconstructed.

## Evidence

- internal/providers/catalog.go:59-72 declares shared provider metadata and ACPRuntime; 240-254 synthesizes and rejects ID collisions; 279-292 defines inherited setup/detection.
- internal/providers/catalog.go:430-448 separates full resolvable listing from base-only detection.
- ADR-0096 describes declarative provider metadata but not one identity with synthesized runtime variants.

Source reads were targeted to the named commit diffs and current sections; they do not establish the human's historical alternatives or acceptance of tradeoffs.

## Related Decisions

ADR-0085, ADR-0096. Existing IDs stay unchanged; this candidate is numbered chronologically within the post-0097 backfill batch.

## User Context — 2026-09-06

Make adding a new provider more declarative.

## Remaining Historical Gaps

Historical alternative evaluations were not supplied.
