# ADR-0145: Rejection RCA Gate

## Status

ACCEPTED

## Context

Repeated review rejection can mix product defects, unavailable validation
capabilities and lifecycle retries. Eventual approval hides that distinction.
Review budgets bound attempts and `planning_review_churn` detects planning churn,
but neither requires a classified recovery decision across all reviewed task types.

The [status vocabulary partition](../architectural-issues.md#status-vocabulary-partition-between-go-constants-and-declarative-pipeline)
already splits Go constants from declarative pipeline statuses. Adding another
pipeline status would widen that unresolved issue. The existing `BLOCKED` state
already prevents normal claims and submissions, and `unblock-task` owns its
validated restoration. ADR-0051's attempt accounting also means that restoring
through an ordinary claim consumes an iteration even when no product correction
was needed.

## Decision

Reuse `BLOCKED` with typed reason `rejection_rca_required` and a durable
`task.rejection_rca` record. The predicate `RejectionRCAGateOpen()` identifies the
gate from record/disposition presence; consumers do not parse the prose reason.
The typed token also identifies gate events through `blocked_class`.

Evaluate the configurable threshold (default four durable rejections) at the
REJECTED verdict boundary in the same locked transaction that commits the
rejection. Existing review-budget and iteration escalation take precedence.
This boundary blocks further iteration before another normal claim or submission and
serializes concurrent verdicts without adding checks to the claim path. The
durable count survives attempts. After a disposition, a further threshold's worth
of rejections is required to open a new cycle; it replaces the live record while
earlier cycles remain in immutable history.

Separate request provenance from stored record provenance. The
`record-rejection-rca` payload contains only `schema_version`, `summary` and
`contributions` (rejection index, category set and bounded evidence). Gate-seeded
threshold/count/time/commit and operation-derived identity/time/actor cannot be
supplied by the caller. The fingerprint covers only the structurally normalized
request, making equivalent resubmissions idempotent without letting seeded fields
alter identity. Mixed and unrecognized causes remain visible.

The orchestrator's `resume-rejection-rca` request contains only `schema_version`,
`recovery_path` and `rationale`. The operation derives actor, lifecycle revision,
decision time, restore mode and iteration exemption. It records a disposition and
closes the gate, leaving the task `BLOCKED`. `unblock-task` remains the only
restoration operation and enforces the disposition:

| Recovery path | Restore mode | Consequence |
|---------------|--------------|-------------|
| `implementation_correction`, `human_override` | `claimable` | Either unblock form; a subsequent normal claim increments iteration. Override requires rationale. |
| `capability_reroute`, `lifecycle_repair` | `assign` | Require `--assign-to`; direct restoration to executing status avoids the claim increment. |
| `rescope` | `none` | Refuse unblock; supersede instead. |

Capability/lifecycle dispositions reset the current review-cycle count and record
`iteration_exempt`; the durable rejection total stays intact. Assignment, not the
advisory exemption field, prevents the product-iteration increment. Dependency,
worktree and rebase validation continue to apply.

## Consequences

- Product correction, capability rerouting and lifecycle repair become distinct,
  authorized recovery choices with durable evidence before normal iteration resumes.
- The task-state-machine and atomicity invariants are preserved: no new pipeline
  status, one verdict transaction, and restoration only through `unblock-task`.
  Review-budget limits and planning-churn detection remain active backstops.
- History entries for blocking, recording and resuming preserve cumulative causes,
  timing and dispositions after a new cycle replaces the live record. Telemetry
  reads history for cumulative dimensions and the live record for current gating.
- Consumers that only display ordinary blockers still see `BLOCKED`; consumers
  that route recovery must recognize the gate predicate. The broader status
  vocabulary partition remains unresolved.
- Future claim/restore changes must preserve the direct-assignment mechanism;
  `iteration_exempt` alone cannot protect iteration accounting.

The [task lifecycle protocol](../../protocols/task-lifecycle.md#rejection-rca-gate)
defines request bounds, event detail keys and the restoration sequence.

## Alternatives Considered

- **New pipeline status:** rejected because it expands the existing status
  partition and requires new transitions where `BLOCKED` already supplies them.
- **Claim-boundary gate:** rejected because it separates threshold escalation from
  the durable verdict and leaves submission paths needing equivalent guards.
- **Reuse budgets or planning alerts alone:** rejected because neither requires
  classified RCA and disposition for all reviewed task types.
- **Accept the complete stored record as a request:** rejected because gate-seeded
  and operation-derived fields could be spoofed, and identity would depend on data
  absent from the caller's request.
- **Restore all recoveries as claimable:** rejected because capability and
  lifecycle repairs would consume product iterations despite requiring no product
  correction. A new exception in claim accounting is unnecessary when validated
  direct assignment already avoids the increment.
