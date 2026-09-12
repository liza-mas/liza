# 135 - Quarantined Verdict Evidence and Reconciliation

Date: 2026-09-12

## Context and Problem Statement

Issue #153 exposed an information-loss boundary in generation fencing. A
replaced reviewer's substantive rejection was refused before its findings were
persisted; a later approval of the same commit could then merge without
reconciling the disagreement. The generation fence correctly denied lifecycle
authority, but the review evidence needed a separate durable carrier.

## Decision

Extend [ADR-0130](0130-generation-fenced-agent-authority.md) with one narrow
evidence-only exception. A valid generation-fenced authenticated verdict may
append or deduplicate a typed `quarantined_verdicts` record. It still returns an
authority error and cannot mutate tasks, agents, claims, history, or quorum.
Administrative failures without valid substantive content do not create records.

Authenticated submission requires `--review-commit FULL_SHA` (40 or 64 hex
characters), bound to the commit the reviewer actually inspected. The immutable
supplied SHA is never inferred from replacement task state. Known boundaries
are captured as matched; valid unknown boundaries are retained as permanently
unmatched, non-gating evidence. Unknown tasks and malformed inputs fail.

Record identity hashes task, commit, reviewer, verdict, and canonicalized
sanitized reason. Registration fingerprints are deduplicated provenance, not
part of judgment identity. Reasons are bounded to 4096 UTF-8 bytes and mask known
secrets plus caller and registered generations. Diagnostics use SHA-256
fingerprints; the live registration credential remains only in its existing
authentication field.

Approval and every merge entry check applicable unresolved conflicting evidence.
Explicit chronological `review_commit_updated` history carries a matched
finding from A to B without changing its original SHA or silently superseding
it. Unrelated resubmission is distinct; ambiguous relevant lineage fails closed.
Current authorized rejection remains possible, and quarantined approval cannot
supply quorum or force acceptance.

`reconcile-verdict` requires an actual current-generation registered orchestrator
with the operation capability, revalidated in the append transaction. There is
no nil-authority, human-ID, or `--changed-by` bypass. Each decision records actor,
time, disposition, and reason. Refuted/superseded resolve a hold; accepted
rejection continues to block unchanged work; escalated retains the hold. Later
decisions preserve the earlier audit. Human judgment routes through the
legitimate orchestrator's inherited authority without copying credentials.

The default embedded orchestrator gains this capability. `LoadFrozen` already
merges missing operations from the embedded role of the same name in memory,
so existing matching frozen roles receive it without rewriting their file.

## Ordering and Lock Ownership

Public verdict, merge, and reconciliation entries acquire a per-task file lock
once and hold it through finalization. Private clean-global approval recursion
does not reacquire it. Non-Git focused callers retain the existing root-file
fallback pattern.

Where lifecycle locks are already held, ordering is project lifecycle → agent
lifecycle → task review → integration completion → integration mutation →
blackboard read. These review operations never acquire the outer lifecycle
locks. Blackboard writes happen only after releasing integration mutation,
preserving [ADR-0112](0112-serialize-integration-working-tree-mutations.md).

Evidence ordered before merge is visible to its barrier, including the
already-ancestor recovery path. Merge ordered first may finish; later evidence
is retained with operator guidance but cannot reopen or roll back terminal
state. Lock timeout is an explicit retryable failure with no evidence-save
claim. Long integration tests may delay same-task submissions.

## Alternatives and Consequences

An anomaly-shaped carrier would require untyped validation and reconciliation
indexing. A separate ledger would add a second persistence and synchronization
boundary. Typed evidence in the same locked state preserves atomic deduplication
and a single source for the merge barrier.

This deliberately requires existing CLI scripts to pass their reviewed SHA.
Obsolete reviewers can cause justified review delays, bounded by immutable
provenance and explicit reconciliation. Evidence remains active-state debt:
[retention triggers](../../../TECH_DEBT.md#quarantined-verdict-retention) require
safe archival before 1,000 unique findings or 1 MiB of serialized evidence.
Automatic eviction is deferred because it could erase unresolved holds.

Explicit task deletion removes that task's quarantined findings and their
reconciliation audit in the same state transaction. Evidence for other tasks
is preserved. This follows task deletion's existing reference cleanup and
prevents orphaned findings from invalidating the remaining run.

This partially supersedes ADR-0130's blanket no-write consequence for stale
authenticated callers only for this evidence carrier; all lifecycle authority
and recovery guarantees remain.
