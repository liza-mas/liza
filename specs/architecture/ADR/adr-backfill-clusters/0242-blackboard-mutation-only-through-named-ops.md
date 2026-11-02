# Cluster 0242 - Blackboard Mutation Only Through Named Ops Operations

## Status

ADR generated: [0242](../0242-blackboard-mutation-only-through-named-ops.md). Selected 2026-11-02.

## Commit Set

- `bc6945b07` — refactor(ops): move registration, exit release, supervisor block and exit-42 persistence into named operations
- `263298679` — refactor(ops): move worktree recovery, clean-terminal, anomaly, orchestrator-turn and checkpoint-summary writes into named operations
- `4bd986841` — refactor(ops): move init publication, migrate and the TUI auto-resume toggle into ops and enforce the C6 boundary in lint
- `fd09e4b3f`, `2f5944f09` — fix(arch): keep/mask environment values in the C6 guard's listing diagnostics
- `f112f85ed` — fix(portability): give the C6 listing test an owned Go cache (Windows)

Earliest author timestamp: 2026-10-27T02:40:55+01:00. Range: 2026-10-27.

## Reconstructed Decision

Every blackboard mutation is a named `ops` operation; `TestC6MutationGuard` in `make lint` rejects mutation outside `ops`/`db`, with no baseline exception.

## Evidence

- Goal decisions D6, D7, R8, N5, D61; goal C6 row.
- All five commit bodies read; guard source header and method table read.
- Allowlist rationale change verified in `4bd986841` diff.
- c6-ownership and c6-bookkeeping evidence headers read.

## Related Decisions

ADR-0011, ADR-0021 (completed), ADR-0218, ADR-0228, ADR-0241.

## User Context — 2026-11-02

Approved 2026-11-02 without added intent. Why go/types rather than grep or a runtime validator is not recorded; marked in the ADR.
