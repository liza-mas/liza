# 160 - Doer Acceptance-Refusal Escalation

## Status

ACCEPTED

## Context

A coding task whose acceptance allocation cannot match its reviewed carrier —
a slug where the carrier has an ATX heading, or `validation` that differs from
the reviewed commands — is refused by every claim. Claim refusal writes no
state, and claimability is judged from status and dependencies alone, so the
task stayed claimable: idle doers retried it about every 7 s for hours (3,674
refusals on one task, 16,395 across a run), each refusal taking a locked state
read, and nothing woke the orchestrator, the only actor that can change
orchestrator-authored acceptance fields. The refusal also told the doer to
"correct the evidence and submit, or run `update-review-commit`", neither of
which applies to an unclaimed task.

ADR-0140 bounded the same loop for reviewer claims. Its Consequences said the
doer claim path "inherits the backoff tier"; it did not. The backoff lives in
`claimFailureObserver`, which only the reviewer strategy implements, so a doer
kept the flat 5 s retry. Quarantine and an anomaly would not have been enough
anyway: that anomaly has no wake consumer.

## Decision

**Classify at the refusing site.** `AcceptanceEvidenceError` carries a fault
class decided where acceptance refuses. *Content* covers refusals that are a
pure function of bytes and state read successfully: a missing or malformed
contract, validation that differs from the reviewed commands, validation
safety, missing strict Source References, an unresolvable pinned heading,
two parents allocating the task, and an adopted source that was removed or
downgraded. *Allocation* covers the parent-allocation, drifted-proof and
adopted-ancestry refusals, whose helpers read a failed Git call as "not
allocated", so repetition can be observed but determinism cannot. Every other
site — a failed Git read of the carrier or a pinned reference — stays
unclassified.

**A claim refusal records what it saw.** At claim time the error is marked
claim-stage with the allocation reference, the integration commit validated
against, and an observation digest over the task record passed to validation,
the whole record of each effective parent (or a missing marker), and the proof
reaffirmations recorded against those parents. Whole records keep the digest
conservative: any repair, including a reaffirmation that touches no task field,
changes it. The claim-stage message names the allocation and tells the reader
the orchestrator must correct it (`replace-task`) or repair its integration-side
cause. Submission, review and resubmission refusals keep the
`update-review-commit` advice. `claim-task` still mutates nothing on refusal.

**The doer supervisor escalates; the claim command does not.** A doer strategy
escalates a content refusal on first observation and an allocation refusal on
the third consecutive identical observation (same digest and integration
commit; a different observation, any other claim failure for the task, or a
successful claim of it restarts the count). Unclassified refusals keep the
existing retry. The allocation escalation is retry exhaustion, not a
determinism claim, and its reason says it may be a persistent repository read
failure.

**Escalation is a guarded BLOCKED transition.** `BlockAcceptanceRefusedTask`
takes the integration-completion linearization, compares the integration ref
with the observed commit under the integration-mutation lock, releases that
lock, then in a generation-fenced state transaction requires the task to be in
its role-pair initial or rejected status with an unchanged observation digest.
Any mismatch is a no-op. The BLOCKED record carries `blocked_reason`
(`acceptance_evidence_invalid:` or `acceptance_claim_refused_repeatedly:`, then
the field, allocation and reason), one question naming `replace-task` and
`unblock-task`, cleared ownership, preserved `acceptance_source`, worktree and
base commit, and a history entry with `claim_refusal`, `fault_class` and the
integration commit. It wakes the orchestrator through the existing BLOCKED
trigger; no new wake type or anomaly exists.

**Inspection shows the allocation inputs.** `get tasks <id>` projects `plan_ref`
and the effective `parent_tasks`, which acceptance judges before any claim has
adopted `acceptance_source`.

The threshold is a constant for the same reason as ADR-0140's: it describes the
supervisor loop, not the user's stack.

## Consequences

A content fault now costs one refusal and one orchestrator turn instead of an
unbounded retry loop; an allocation fault costs three refusals. Operators see
the fault in `blocked_reason` and the inputs in task inspection.

Residual paths, each bounded or unchanged:

1. **Unclassified refusals** (a failed Git read) keep the flat retry. A
   persistent read failure loops as before; it is an infrastructure fault this
   decision does not claim to detect.
2. **Restarts** lose the in-memory allocation count; a fresh supervisor
   re-observes up to three times.
3. **Stale observations** are dropped, never blocked: a repair, a moved
   integration ref, a new owner or a lost registration between refusal and
   escalation leaves the task as it is, and the next claim re-validates.
4. **CLI claims** (`claim-task` run by a person) still only report the
   refusal.

## Alternatives Considered

**Extend the ADR-0140 breaker to doers.** Rejected: quarantine bounds the retry
but wakes nobody, and the per-process counters multiply with idle doers.

**Block inside `claim-task`.** Rejected: it turns a refusal into a mutation for
every caller and breaks the refusal-writes-nothing contract the acceptance tests
pin.

**Make allocation refusals deterministic by un-collapsing the parent helpers.**
Deferred: the helpers are shared with `reaffirm-proof`, and three observations
already bound the loop.

**Reject invalid acceptance at task creation (D52).** Complementary, not a
substitute: it cannot catch integration drift after creation or an adopted
source that later disappears.
