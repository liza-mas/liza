# Cluster 0131 - Serialize Repository Worktree Metadata Mutations

## Status

ADR generated: [0131](../0131-repository-worktree-mutation-lock.md). Selection and grouping approved on 2026-09-06; user intent supplied on 2026-09-06.

## Commit Set

- `698ad2ed` — fix(git): serialize worktree metadata mutations

Earliest author timestamp: 2026-09-01T03:33:04+02:00. Decision implementation range: 2026-09-01 to 2026-09-01.

## Reconstructed Decision

Prevent concurrent Git worktree operations from colliding on repository-wide metadata.

Repository-scoped file lock covers add/attach/remove and fallback metadata cleanup; claims, indexing and setup remain outside the critical section.

**Rationale provenance:** User-confirmed intent: Serialization was chosen because it seemed more robust. Other explanations remain source-derived or reconstructed.

## Evidence

- 698ad2ed full production diff and internal/git/worktree.go:22 (delegated verified).
- ADR-0112 covers integration ref/index mutation, a distinct shared resource; this record must not imply it previously covered worktree metadata.

Source reads were targeted to the named commit diffs and current sections; they do not establish the human's historical alternatives or acceptance of tradeoffs.

## Related Decisions

ADR-0022, ADR-0061, ADR-0112. Existing IDs stay unchanged; this candidate is numbered chronologically within the post-0097 backfill batch.

## User Context — 2026-09-06

Serialization was chosen because it seemed more robust.

## Remaining Historical Gaps

No specific retry comparison or rationale for the 30-minute timeout was supplied.
