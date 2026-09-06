# Cluster 0130 - Generation-Fenced Agent Authority and Recovery

## Status

ADR generated: [0130](../0130-generation-fenced-agent-authority.md). Selection and grouping approved on 2026-09-06; user intent supplied on 2026-09-06.

## Commit Set

- `a22c1238` — fix(lifecycle): fence claim and recovery generations

Supporting fixes: `866c903f`, `586542f1`.

Earliest author timestamp: 2026-08-27T11:21:13+02:00. Decision implementation range: 2026-08-27 to 2026-08-31.

## Reconstructed Decision

Prevent stale same-ID supervisors from mutating replacement registrations or launching duplicate providers and stranding reviewed work.

Opaque registration tokens checked transactionally on agent-authenticated writes, per-agent registration/start lock, lease-first uncertain ownership, approval-preserving takeover and tuple-based rejected recovery.

**Rationale provenance:** User-confirmed intent: Sandbox execution tends to confuse the operator agent; generation-fenced authority provides more robust operation in that environment. Other explanations remain source-derived or reconstructed.

## Evidence

- specs/architecture/supervision-model.md:101-107 consolidates the recovery authority contract (delegated verified).
- internal/models/agent.go:60; internal/ops/agent_authority.go:47,66; internal/ops/agent_lifecycle_lock.go:28 (delegated verified).
- internal/ops/lifecycle_authority.go retains nil-authority compatibility for legacy internal callers; do not claim universal authentication.

Source reads were targeted to the named commit diffs and current sections; they do not establish the human's historical alternatives or acceptance of tradeoffs.

## Related Decisions

ADR-0046, ADR-0051, ADR-0062, ADR-0083, ADR-0085, ADR-0112. Existing IDs stay unchanged; this candidate is numbered chronologically within the post-0097 backfill batch.

## User Context — 2026-09-06

Sandbox execution tends to confuse the operator agent; generation-fenced authority provides more robust operation in that environment.

## Remaining Historical Gaps

No specific incident or intentional-debt classification for nil-authority compatibility paths was supplied.
