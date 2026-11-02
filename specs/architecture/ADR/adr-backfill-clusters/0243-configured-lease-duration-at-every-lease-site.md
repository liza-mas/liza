# Cluster 0243 - Configured Lease Duration at Every Lease Site

## Status

ADR generated: [0243](../0243-configured-lease-duration-at-every-lease-site.md). Selected 2026-11-02.

## Commit Set

- `8124f434b` — feat(ops): honour configured lease_duration at the five agent and review lease sites

Earliest author timestamp: 2026-10-27T05:17:40+01:00. Range: 2026-10-27.

## Reconstructed Decision

`ops.LeaseDurationSeconds` resolves `lease_duration` at registration, heartbeat, reviewer claim, re-review reclaim and pool repair.

## Evidence

- Goal decisions D6, D7, R12; goal C1 row.
- `8124f434b` body read; c1-lease-duration-evidence.md site table read.

## Related Decisions

ADR-0062, ADR-0220, ADR-0224, ADR-0226, ADR-0228.

## User Context — 2026-11-02

Approved 2026-11-02 although flagged low confidence (close to a bug fix); recorded as a ruled forced behaviour change.
