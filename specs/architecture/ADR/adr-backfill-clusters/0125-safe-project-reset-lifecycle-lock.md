# Cluster 0125 - Safe Project Reset Through a Lifecycle Lock

## Status

ADR generated: [0125](../0125-safe-project-reset-lifecycle-lock.md). Selection and grouping approved on 2026-09-06; user intent supplied on 2026-09-06.

## Commit Set

- `35fda601` — feat(cleanup): add safe project reset

Earliest author timestamp: 2026-08-10T16:05:47+02:00. Decision implementation range: 2026-08-10 to 2026-08-10.

## Reconstructed Decision

Allow cleanup and reinitialization without orphaning live agents or worktree resources.

Plan exact owned targets, confirm, acquire exclusive lifecycle lock, revalidate the plan and delete; registration/provisioning/recovery take shared locks.

**Rationale provenance:** User-confirmed intent: Cleaning up a previous run was a painful manual task for users, especially worktrees. Leaving ordinary blackboard writes outside the lifecycle lock was a deliberate tradeoff. Other explanations remain source-derived or reconstructed.

## Evidence

- internal/ops/project_lifecycle_lock.go:14-18 scopes shared lifecycle effects and intentionally excludes ordinary blackboard writes.
- internal/ops/project_lifecycle_lock.go:35-42 exposes exclusive cleanup locking.
- internal/ops/project_cleanup.go:131 contains the plan/exclusive-lock/revalidation execution boundary (verified by delegated source read).

Source reads were targeted to the named commit diffs and current sections; they do not establish the human's historical alternatives or acceptance of tradeoffs.

## Related Decisions

ADR-0018, ADR-0022, ADR-0083, ADR-0112. Existing IDs stay unchanged; this candidate is numbered chronologically within the post-0097 backfill batch.

## User Context — 2026-09-06

Cleaning up a previous run was a painful manual task for users, especially worktrees. Leaving ordinary blackboard writes outside the lifecycle lock was a deliberate tradeoff.

## Remaining Historical Gaps

A specific cleanup race incident and historical alternative evaluations were not supplied.
