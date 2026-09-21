# 140 - Reviewer-Claim Circuit Breaker

## Status

ACCEPTED

## Context

A reviewer supervisor whose claim failed deterministically retried it forever at
a fixed interval. In one observed incident a single "review boundary needs
repair" failure repeated 133 times in about 13.5 minutes, and one supervisor
accumulated 1,798 review-claim errors over its lifetime, with no review ever
starting.

The claim-failure branch of the supervisor loop had no memory: it matched only
agent degradation, slept 5 s and continued. The work check kept reporting work
because a task whose `review_commit` no longer matches its worktree HEAD is
still claimable, and the failing claim wrote no state, so the next iteration saw
exactly the same condition. None of the existing bounds reached it. The per-task
review-claim cooldown keys on *release* history entries, which a claim that never
succeeded never writes. Spinning detection runs only after a successful claim and
keys on a claimed task ID. Agent-pool auto-repair backoff bounds supervisor
*spawning* in the watch process, not a live supervisor's attempts. Generation
fencing stops the loser only when the background heartbeat next writes state.

A further obstacle was reporting: `ops.ClaimReviewerTask` collapsed a set of
per-candidate failures into one prose `PreconditionError`, discarding the typed
errors, so no caller could tell which candidate failed, in which class, or
against which version of that candidate's boundary.

## Decision

The supervisor keys consecutive pre-claim reviewer failures and answers them in
two tiers.

**The failure key** is `role | task_id | failure_class | boundary_version`, where
`boundary_version` digests the candidate's state-visible `status`,
`review_commit`, `base_commit`, `worktree` and history length. This is the
load-bearing choice: too coarse and the key never clears, too fine and it never
opens. Every input is a `state.yaml` field of the candidate, read where the
failure is produced. The documented repair writes `review_commit` and
`base_commit`, so it advances the key; any lifecycle event moves `status` or
history, so unrelated progress also clears it; nothing time-derived enters, so
identical failures against unchanged state collide and the breaker opens. The
out-of-state inputs the boundary validator also reads — worktree HEAD and the
integration merge base — are deliberately excluded, because deriving them would
mean parsing error prose into policy. A failure that names no candidate gets no
key at all.

The registration generation is neither keyed nor recorded. The counters live in
the supervisor process, whose lifetime is bounded by its registration, so a
re-registered agent starts empty by construction; encoding the generation would
give each restart a distinct key and multiply anomalies.

**Classification is decided at the removal site**, while the typed error is still
in hand, and the returned error is wrapped rather than replaced, so `errors.As`
for `*PreconditionError` still yields the same lifecycle outcome and recovery
fields. Re-classifying an already-typed failure returns it unchanged, so a
multi-candidate failure cannot degrade to "no work" on the way to the breaker.
Successful claims also carry the classified candidate failures through the claim
adapter, so claiming a healthy task does not hide failures of earlier candidates.

**The two-tier response:** `review_boundary_repair`, `acceptance_evidence` and
`worktree_context` are deterministic and repair-required, so after three
consecutive identical failures the key opens — the candidate leaves the
reviewer's work check for a 5 min cooldown doubling per
failed re-probe to 30 min, and one durable `reviewer_claim_circuit_open` anomaly
is recorded. Every other non-stopping class takes bounded exponential backoff
keyed by role and class, from the same 5 s the loop used before to a 5 min cap.
Authority loss stops the supervisor; agent degradation keeps its existing exit.
Quarantine is evaluated from in-memory counters and task fields only — no git,
no state lock — so the work check stays cheap.
Claim selection still evaluates and removes a broken candidate when other work
wakes the reviewer. Those failures count even during cooldown, but do not extend
it or write another anomaly until the key reopens. Success clears only the
claimed task's keys (and the role's transient backoff).

**One anomaly per key.** The writer scans for a matching type, task, role, class
and `boundary_version` inside the same locked transaction: on a miss it appends
one record, on a hit it updates `attempts`, `last_failure` and `cooldown_until`
in place and appends nothing. Retry count and duration keep advancing without an
entry per attempt, and concurrent or restarted supervisors do not multiply
records.

**Thresholds and cooldowns are constants**, not configuration. They describe the
supervisor loop, not the user's stack, so there is no per-project variation for a
config field to express; the backoff base equals the previous flat delay, leaving
first-failure behavior unchanged.

## Consequences

When only quarantined work remains, attempts against an unchanged boundary are
bounded by the threshold plus one re-probe per cooldown window, and the
error-level claim log line is emitted once per opened key rather than once per
attempt. A quarantined candidate does not block the role — any other reviewable
task is still claimed. Operators get one record naming the role, task, class,
first and last failure, attempt count, cooldown and the required repair.

Three residual paths survive, each bounded:

1. **A process restart loses the in-memory counters.** A fresh supervisor
   re-attempts up to the threshold before re-quarantining. Anomaly deduplication
   is durable, so each restart costs at most that many attempts and adds no
   record and no alert. The steady state is a park on the existing wait loop, and
   because the cooldown cap is far below the reviewer's max wait, an expiry always
   interrupts the park before the supervisor would exit.
2. **Repairs invisible in `state.yaml`.** A repair that moves only worktree HEAD
   or the integration merge base changes no keyed field and emits no watcher
   event. The cooldown is therefore time-bounded rather than purely change-gated:
   expiry permits exactly one re-probe, which re-runs the real validator and
   clears the key when the repair landed.
3. **Unenumerated failure classes** fall to `unclassified`: backoff, never
   quarantine, never an anomaly. They degrade to the previous behavior with a
   growing delay instead of a constant one.

The doer claim path shares the supervisor branch, so it inherits the backoff tier
and the authority stop for free; no doer-specific quarantine exists. The new
anomaly type carries required-detail validation, mitigating one row of the open
"anomaly detail validation incomplete" issue without closing it.

## Alternatives Considered

**Key on the existing release-history cooldown.** Rejected: that cooldown scans
task history for claim-release entries by the agent, and a claim that never
succeeded never releases. It cannot see a pre-claim failure at all — this is the
root cause, not a tuning gap.

**A state-level breaker record instead of in-memory counters.** Rejected: it
needs a new `State` field owned outside this change, and it buys little. The
durable half that matters for restarts and concurrency — one deduplicated
anomaly per key — is already durable, and per-attempt counters in state would add
a state write per tick to a loop whose problem is excessive activity.

**Reuse the `retry_loop` anomaly type.** Rejected: its required details (`count`,
`error_pattern`) describe a retry cluster inside a task's execution and carry no
role, candidate boundary or recovery, so a pre-claim quarantine recorded as
`retry_loop` would be unactionable and would pollute retry-cluster pattern
detection.

**Configurable threshold and cooldown.** Rejected under constants-not-config: no
per-project variation was found, and a config field would make a property of the
supervisor loop look like a stack-dependent tuning knob.

## Register Deltas Owed to Output 7

This change does not edit the shared registers. Output 7 (`cpm-1-cp-7`) owns:

- `specs/architecture/blackboard-schema.md` — the `reviewer_claim_circuit_open`
  anomaly-type row and its detail fields (`role`, `failure_class`,
  `boundary_version`, `attempts`, `first_failure`, `last_failure`,
  `cooldown_until`, `recovery`, `error`).
- `specs/architecture/ADR/README.md` — the index row for this ADR.
- `specs/architecture/architectural-issues.md` — the traceability line noting
  that one more anomaly type now has detail validation.
