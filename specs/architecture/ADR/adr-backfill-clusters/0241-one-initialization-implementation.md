# Cluster 0241 - One Initialization Implementation

## Status

ADR generated: [0241](../0241-one-initialization-implementation.md). Selected 2026-11-02.

## Commit Set

- `009d4835a` — refactor(c4): delete ops.InitProject and run init branch Git in the project root
- `4cd8d7d78` — docs(c4): record the pre-C4 red run and the branding scan

Earliest author timestamp: 2026-10-26T16:06:27+01:00. Range: 2026-10-26.

## Reconstructed Decision

The CLI init path is the only implementation; `ops.InitProject` is deleted; init branch Git runs through `gitenv` timed runners in the project root.

## Evidence

- Goal decisions D6, D7, R5; goal C4 row.
- `009d4835a` body read in full; pre-C4 `init_integration_branch.go` read (used `gitenv.Command`: environment, no timeout, no Dir).
- `git grep InitProject 009d4835a^`: only the await-verdict integration fixture and the A7 ratchet referenced it.
- architectural-issues.md resolution entry; run-close.md C4 row.

## Related Decisions

ADR-0021, ADR-0218, ADR-0226, ADR-0242.

## User Context — 2026-11-02

Approved 2026-11-02 without added intent. Reference-path choice derived from 'no production caller' (user accepted sources-only with gaps marked).
