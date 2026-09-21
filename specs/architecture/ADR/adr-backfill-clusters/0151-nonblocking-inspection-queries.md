# Cluster 0151 - Nonblocking Inspection Snapshots and Field Queries

## Status

ADR generated: [0151](../0151-nonblocking-inspection-queries.md). Selection approved on 2026-09-21; user intent supplied on 2026-09-21.

## Commit Set

- `e45849438` — feat(inspection): add nonblocking snapshots and task field queries
- `019b4d077` — fix(inspection): derive time in status from one shared definition

Earliest author timestamp: 2026-09-21T10:47:19+02:00. Decision implementation range: 2026-09-21.

## Reconstructed Decision

Serve inspection from one uncached atomic snapshot per call, deriving transition policy from it instead of reloading locked runtime policy per task; add dotted task fields, `get-tasks` and field projections with lifecycle redaction; and define `time_in_status` once in `models.TimeInStatus` over an explicit event classification, with `classified=false` for unknown event names.

**Rationale provenance:** User-confirmed: the lock contention was observed. The `time_in_status` divergence is measured in the commit body.

## Evidence

- Commit bodies for both commits, read in full.
- Measured divergence: three surfaces agreed on 4 of 93 tasks in a live run; the TUI reset displayed duration by up to 31 hours; the field query reported zero for never-claimed tasks.

## Related Decisions

ADR-0148 (cached parsed state) addresses the adjacent read-path cost; ADR-0022 owns the state-access boundary.

## User Context — 2026-09-21

Observed.

## Remaining Historical Gaps

No record of whether caching inspection results was considered before choosing the snapshot approach. The redaction policy's origin is not discussed.
