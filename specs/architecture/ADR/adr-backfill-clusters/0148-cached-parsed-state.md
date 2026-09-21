# Cluster 0148 - Cached Parsed State with Per-Call Deep Copies

## Status

ADR generated: [0148](../0148-cached-parsed-state.md). Selection approved on 2026-09-21; no separate user intent supplied.

## Commit Set

- `f5b4da415` — perf(db): cache parsed state and hand out deep copies

Earliest author timestamp: 2026-09-14T12:12:32+02:00. Decision implementation range: 2026-09-14.

## Reconstructed Decision

Cache the parsed, normalized `models.State` keyed by file mtime and return a reflective deep copy per call, preserving `ReadCached`'s contract that callers may mutate the result. Cross-process coherence is unchanged: every call stats the shared path and writes invalidate.

**Rationale provenance:** Commit-evidenced. The body states the starvation mechanism and carries the measurements (1.07 ms cached read vs 30.4 ms reparse on a 2.4 MB fixture; 2.18 MB retained heap).

## Evidence

- `f5b4da415` commit body, read in full, including `TestStateModelShapeIsCloneable` as the machine check for the shallow-copy escape hatch and `BenchmarkStateParse` retained to keep the premise falsifiable.
- ADR-worthiness rests on the contract change to `ReadCached`, not on the performance gain alone.

## Related Decisions

ADR-0022 (concurrency hardening, singleton blackboard) owns the state-access boundary this caches behind.

## User Context — 2026-09-21

None supplied; the user answered "no" when asked whether any Tier A cluster carried an unstated tradeoff.

## Remaining Historical Gaps

No record of whether a read-only contract (pushing copies to callers) was considered. The mtime key's behavior under coarse filesystem timestamp granularity is not discussed in the commit.
