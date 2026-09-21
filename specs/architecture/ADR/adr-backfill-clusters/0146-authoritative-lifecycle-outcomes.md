# Cluster 0146 - Authoritative Outcomes for Repeated Lifecycle Actions

## Status

ADR generated: [0146](../0146-authoritative-lifecycle-outcomes.md). Selection approved on 2026-09-21; user intent supplied on 2026-09-21.

## Commit Set

- `fa4964845` — fix(lifecycle): return authoritative outcomes for repeated actions (#155, 156 files)
- `04adbc660` — fix(ops): retire a lifecycle preparation left by a restarted generation
- `aab3c362a` — fix(ops): never renew a lease on an unassigned task
- `42a88c519` — fix(commands): clear a stranded lease during migrate
- `1e2c0398e` — fix(ops): carry the write-state cause to the submitting agent

Earliest author timestamp: 2026-09-12T15:20:35+02:00. Decision implementation range: 2026-09-12 to 2026-09-20.

## Reconstructed Decision

Repeated or stale lifecycle commands return authoritative outcomes derived from bounded durable request receipts and transition identities, instead of a generic failure, while preserving authorization and external-effect fences.

Four corrections complete the same principle — a lifecycle operation must leave repairable state and expose its cause: generation-aware preparation retirement through the agent registry; lease renewal that no-ops and self-heals on unassigned tasks; `migrate` normalizing the ownership tuple where global post-mutation validation would otherwise refuse the repair; and the write-state cause routed to agents through masked bounded `Details` rather than the withheld `Err`.

**Rationale provenance:** User-confirmed: the work originated in a live-run incident, not inspection. Commit bodies supply the specific episodes. Historical alternative evaluations were not supplied.

## Evidence

- Commit bodies for all five commits, read in full.
- Named live-run episodes: four rejected plans permanently unclaimable; a provider quota kill stranding a lease; five tasks `liza validate` could not move; two occurrences on `cpm-1-cp-2-code-4` where a coder reported no exposed cause.
- Grouping note: `aab3c362a`/`42a88c519` repair the ownership tuple rather than the receipt mechanism. They are included because they share the "invalid *and* unrepairable" failure shape; a future split is defensible.

## Related Decisions

ADR-0130 (generation-fenced agent authority) supplies the registration generations that preparation retirement consults. ADR-0125, ADR-0112.

## User Context — 2026-09-21

Live-run incident.

## Remaining Historical Gaps

No alternative-evaluation record; the options in the ADR are reconstructed from the preserved fences. Whether the four corrections were understood at the time as one decision or as independent fixes was not stated.
