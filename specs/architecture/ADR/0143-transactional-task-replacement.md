# ADR-0143: Transactional Task Replacement

## Status

ACCEPTED — 2026-09-20. Records the approved
[replacement plan](../../plans/20260918-fix-gh-issues/20260918-211707-cpm-1-cp-4.md#transaction-design-and-interface-contract).

## Context

Replacement previously required separate creation, dependency repair and
supersession commands. Between calls, a replacement could be visible with the
wrong dependencies while the source remained actionable. Retries and competing
requests could collide on task IDs without a durable identity for the whole
operation. The approved issue digest also records preserved-source drift:
declaring a commit did not ensure that the successor worktree contained it.
These historical observations are inherited evidence, not newly inspected logs.

Existing lifecycle receipts already bind actor, generation, request identity,
source boundary and payload digest. The source survives supersession, so it can
carry replacement completion without a new task field or state-level index.

## Decision

Provide one orchestrator-only `replace-task` operation. Its canonical file
payload binds source, reason, replacement task, expected/desired consumer
dependencies and optional preserved base. Require both request ID and expected
source transition; use the shared versioned payload-schema registry before
state acquisition. Keep authorization, Git and live dependency checks at the
mutation boundary.

Under the existing project lifecycle, source ownership and blackboard lock
order, compose the creation, dependency-update and supersession cores in one
generation-fenced transaction. Validate the complete candidate before
persistence. Any failure discards the candidate without compensation writes.
Keep cleanup and logging outside the state transaction as best-effort effects.

Record `replacement_committed` and the lifecycle receipt on the source. Reuse
its durable `superseded_by` and receipt source-status projection to reconstruct
replay results. Exact retained replay returns `ALREADY_COMPLETED`, `changed=false`
and fresh server-selected routing; conflicting identity/payload or replacement
ID reuse returns `CONFLICT`/`stop`. Existing receipt users retain their outcomes.

Validate, but do not construct, a declared preserved base. Require a resolvable
commit and an existing healthy canonical successor worktree on its task branch,
with that commit in HEAD's ancestry. Refuse a commit-only declaration because
fresh claim would overwrite it with integration HEAD. Omitting preservation
remains the ordinary integration-based claim path.

The normative payload, outcomes, audit details and limits are in
[Replacement Transactions](../../protocols/replacement-transactions.md).

## Consequences

One operation publishes a coherent lineage and consumer graph. Identical
concurrent requests can produce one committed replacement and receipt-backed
replay; conflicting requests cannot publish a partial second lineage. The
complete-candidate check intentionally refuses unrelated pre-existing state
corruption instead of degrading to `add-task`'s warning posture.

Replay remains bounded by receipt retention and registration-generation
identity. Re-registration can require requery instead of proving the original
completion. The preserved-source drift class closes only for callers declaring
both preserved-base fields; worktree materialization remains separate. Existing
primitives remain available but their multi-command composition does not gain
transactional replacement guarantees. Agent guidance must direct replacement
work to the new operation. No schema migration or new `Task`/`State` field is
introduced; master output 7 owns shared-register and debt reconciliation.

## Alternatives considered

- **State-level idempotency index:** rejected. It would require a new
  `Task`/`State` field and master ownership correction, while the persistent
  source and existing lifecycle receipts already represent completion. This
  decision accepts the documented pruning and generation ceilings; it does not
  promise an unbounded or cross-generation replay index.
- **Sequence existing commands:** rejected for replacement atomicity. Separate
  commits expose intermediate lineages, and compensating cleanup cannot make
  those observations disappear.
- **Accept a commit-only preserved base:** rejected. Without a worktree, claim
  chooses the fresh strategy and silently replaces that base. Validation of an
  already materialized successor is the bounded decision; constructing it is
  outside this operation.

## Evidence

The approved Output 4 and replacement plan supply decision authority. The merged
replacement ops, payload schemas and lifecycle receipt/result helpers were read
to verify transaction ordering, identity, replay, base checks and audit carriers.
This ADR records those decisions without claiming new behavioral-test coverage.
