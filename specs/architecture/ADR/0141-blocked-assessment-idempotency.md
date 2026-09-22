# ADR-0141: Blocked-Assessment Idempotency

## Status

ACCEPTED — records issue #157, amended for outcome-based provider identity.

## Context

Suppressing unchanged blocked-task wakes did not prevent an authorized explicit
`assess-blocked` call from appending equivalent history. Repeated entries enlarged
both durable state and later orchestrator context. Concurrent calls and restarts
require comparison inside the state transaction against a durable baseline.

The [protocol](../../protocols/blocked-assessment-idempotency.md) defines the six
material inputs, their normalization, persistence and result contract. The
decisions below explain why that identity differs from general request replay.

## Decisions and alternatives

**Use the consumer's non-assessment history count as its lifecycle version,
with current status and provider creation identity.** Reject `Lifecycle.Revision`
for assessment identity. [CompleteLifecycleRequest](../../../internal/ops/lifecycle_receipt.go)
advances that revision when an assessment succeeds. Hashing it would therefore
invalidate the digest just recorded and make the next identical call look new.
Excluding assessment events preserves sensitivity to the consumer's other
lifecycle activity without this feedback loop. The count is reconstructible after
restart.

The alternative is the existing `Lifecycle.Revision` cursor, convenient because
it is already durable but unsuitable because it includes assessment completion.
Changing the general revision's semantics instead would alter lifecycle fencing
and receipt behavior beyond this invariant. It remains authoritative for request
boundaries; the fingerprint is a separate content identity, not a substitute.

**Classify provider outcomes instead of hashing routine provider progress.**
The September operator run showed repeated blocked-consumer wakes after provider
claims, submissions and review steps. Raw provider status/history did not answer
whether the consumer had new options. Direct dependencies and descendants now
share pending, satisfied, failed/blocked and missing classes. Replaced nodes
retain supersession edges and each reachable replacement's outcome; task IDs,
creation times and parent/dependency edges preserve structural changes. Both
failure and recovery remain material. The engine owns `MERGED`, `BLOCKED`,
`ABANDONED`, `INTEGRATION_FAILED` and `SUPERSEDED`; every other nonempty status is
pending. Intermediate states belong to the configured pipeline, not a closed Go
enum. Empty statuses remain unknown with history sensitivity. Exhaustive coverage
of Go status declarations forces review when a new engine outcome is added;
architecture and synthetic custom-pipeline sequences guard the open state space.

Ignoring all provider changes would miss merge/failure and new replacement work;
using raw status/history for only descendants would preserve the wasted wakes.
The shared projection avoids both, without changing dependency satisfaction.
Loading the pipeline to enumerate pending states would couple this pure identity
to configuration access. Instead, the explicit engine-outcome boundary supports
any role pair. This fingerprint does not validate pipeline status membership.

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

Only the latest assessment retains `assessment_fingerprint_v2`; a new append
removes obsolete digest/snapshot extras, including retired v1 digests, from older
assessments while preserving their audit prose. Existing blocked assessments
rebaseline once after upgrade: one `BLOCKED_TASKS` turn per affected run. No
new top-level state field or unbounded cursor history is needed. Structural
normalization ignores whitespace, Unicode composition and
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
