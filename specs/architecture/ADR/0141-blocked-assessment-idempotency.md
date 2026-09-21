# ADR-0141: Blocked-Assessment Idempotency

## Status

ACCEPTED — records the implemented issue #157 decisions.

## Context

Suppressing unchanged blocked-task wakes did not prevent an authorized explicit
`assess-blocked` call from appending equivalent history. Repeated entries enlarged
both durable state and later orchestrator context. Concurrent calls and restarts
require comparison inside the state transaction against a durable baseline.

The [protocol](../../protocols/blocked-assessment-idempotency.md) defines the six
material inputs, their normalization, persistence and result contract. The
decisions below explain why that identity differs from general request replay.

## Decisions and alternatives

**Use non-assessment history counts as lifecycle versions, with current status
and dependency creation identity.** Reject `Lifecycle.Revision` for assessment
identity. [CompleteLifecycleRequest](../../../internal/ops/lifecycle_receipt.go)
advances that revision when an assessment succeeds. Hashing it would therefore
invalidate the digest just recorded and make the next identical call look new.
Excluding assessment events preserves sensitivity to other lifecycle activity
without this feedback loop. The count is reconstructible after restart.

The alternative is the existing `Lifecycle.Revision` cursor, convenient because
it is already durable but unsuitable because it includes assessment completion.
Changing the general revision's semantics instead would alter lifecycle fencing
and receipt behavior beyond this invariant. It remains authoritative for request
boundaries; the fingerprint is a separate content identity, not a substitute.

**Use one shared material-change predicate for writes and blocked-task wakes.**
Both call `BuildAssessmentFingerprint`; the reader uses the latest assessment's
note and current blocker metadata, while the writer can supply new candidate
metadata or disposition. Thus the writer covers every wake-relevant material
change and can also record a newly chosen assessment. Comparison and append are
atomic; equivalent calls return `NO_CHANGE` without history, receipt or alert.

The alternative is two cursors: retain the dependency-descendant wake snapshot
for reads and introduce a separate write fingerprint. That requires two evolving
definitions of meaningful change; they can drift into repeated wakes for a write
that is suppressed, or miss a material change that the writer recognizes.
One builder and one latest-entry digest keep both paths aligned. Missing or
invalid legacy digests fail open once rather than guessing an old identity.

## Consequences

Only the latest assessment retains `assessment_fingerprint_v1`; a new append
removes obsolete digest/snapshot extras from older assessments while preserving
their audit prose. No new top-level state field or unbounded cursor history is
needed. Structural normalization ignores whitespace, Unicode composition and
map order, but deliberately preserves ordered-list changes and does not infer
whether different prose has the same meaning.

The existing outcome matrix observes appends and suppression. A suppressed call
returns its would-be entry's serialized byte size, but durable byte aggregation
is deferred: it needs a reviewed metrics-schema extension, not another history
append that would defeat suppression. The limitation and payback trigger are
tracked in [TECH_DEBT.md](../../../TECH_DEBT.md#blocked-assessment-byte-aggregate).
Suppression does not solve general retention of materially different history.

## Evidence and reconciliation

These decisions are checked against
[the fingerprint builder](../../../internal/ops/assess_blocked_fingerprint.go),
[the transactional writer](../../../internal/ops/assess_blocked.go),
[wake detection](../../../internal/ops/orchestrator_wake.go) and the lifecycle
completion implementation linked above. They record merged behavior without
claiming new behavioral-test execution by this documentation change.

Master output 7 owns the ADR index, invariant/schema registers and spec mapping;
this document supplies their issue #157 decision and protocol reference.
