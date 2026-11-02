# 243 - Configured Lease Duration at Every Lease Site

## Status

ACCEPTED — implemented 2026-10-27 (behaviour change C1 of the architecture
consolidation); backfilled 2026-11-02.

## Context and Problem Statement

`lease_duration` in the project configuration (default
`models.DefaultLeaseDurationSeconds`, 1800 s) was meant to set how long an agent
or review lease lasts. Five sites ignored it and used a hard-coded 1800 s or
30 min:

| Site | Before |
|---|---|
| Agent registration (`supervisor_lifetime.go`, `register`) | literal `1800` |
| Supervisor heartbeat (`HeartbeatConfig.LeaseDuration`) | field omitted, so `DefaultLeaseDuration` |
| Reviewer claim (`strategy_reviewer.go`) | literal `1800` |
| Re-review reclaim (`review_reclaim.go`) | const `reclaimReviewLeaseDuration = 30 * time.Minute` |
| Agent-pool repair projection (`repair_agent_pool_start.go`) | `models.DefaultLeaseDurationSeconds` |

A project that set `lease_duration` got a mix of configured and default leases.
Only non-default configurations were affected.

The consolidation goal froze behaviour except for a closed list of forced
changes (decision D6). Its rejected Option A, a pure refactor, would have baked
known warts like this one into the clean structure as if they were deliberate
design. D7 listed this change as C1, and R12 extended it to the re-review
reclaim and the pool-repair default. Earlier area records (ADR-0220, 0224, 0226,
0228) kept these sites single and findable for C1 and did not change their
values.

## Considered Options

1. **Keep the literals** and document that `lease_duration` covers only some
   leases.
2. **Honour `lease_duration` at every lease site through one resolver** (D7, R12).

## Decision Outcome

Chose **Option 2**.

- `ops.LeaseDurationSeconds(cfg models.Config) int` (`internal/ops/lease.go`)
  returns `lease_duration`, or the model default when the value is not
  positive. Callers convert to `time.Duration` at the call that takes a
  duration.
- Registration resolves the duration once from its startup state snapshot and
  passes the same value to the heartbeat. The reviewer strategy, the reclaim
  transaction and the pool-repair planner resolve it from state they already
  read.
- The terminal placeholder becomes the named constant `terminalPlaceholder`.
  Its stored value is still `"terminal-1"`.
- No configuration field is added. Heartbeat publication, authority checks,
  lock placement and error paths are unchanged. `NewHeartbeat` keeps its
  default for standalone construction without a duration.

With `lease_duration: 600`, a new agent's lease expires 600 s after
registration, each heartbeat extends it by 600 s, and reviewer claims,
re-review reclaims and pool repair all use 600 s. With the default
configuration, every site stays at 1800 s.

### Rationale

A configuration value that only some sites honour is a defect, not a design.
Fixing it inside the consolidation run, as its own task, kept the behaviour
change visible to the oracle instead of hiding it in a refactor (D6). One pure
resolver gives every site the same rule and the same fallback.

### Consequences

**Positive:**
- `lease_duration` means what its name says for every agent and review lease.
- Default-configured projects see no change.

**Limitations accepted:**
- A project that had set `lease_duration` without knowing the literals ignored
  it now gets shorter or longer leases at the five sites, which can change how
  fast stale agents and claims are reclaimed.
- The value is read from state at each site, so a configuration change takes
  effect at the next read of each site, not all at once. *Derived from the
  per-site resolution; the sources do not discuss this.*

## Related Decisions

- [ADR-0062](0062-ghost-agent-claim-prevention-and-ownership-reconciliation.md)
  — lease-based ownership that these durations govern.
- [ADR-0220](0220-a1-typed-vocabulary.md),
  [ADR-0224](0224-a5-ops-review-acceptance-concerns-and-vocabulary.md),
  [ADR-0226](0226-a7-presentation-concerns-and-classification-contract.md),
  [ADR-0228](0228-a9-runtime-concerns-and-launch-signal-capabilities.md) — the
  area records that left the C1 sites unchanged for this task.

## Evidence and Reconstruction

Sources: goal decisions D6, D7 and R12
(`specs/goals/20261023-architecture-consolidation-decisions.md`), C1 in the goal
document, the commit body of `8124f434b`, and
`specs/architecture/consolidation-run/c1-lease-duration-evidence.md`. No user
input beyond the approval of the record (2026-11-02). The backfill flagged this
change as close to a bug fix; it is recorded because it was ruled as one of the
six forced behaviour changes.

---
*Reconstructed from commit `8124f434b` (2026-10-27)*
