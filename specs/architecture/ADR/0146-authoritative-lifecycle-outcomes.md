# 146 - Authoritative Outcomes for Repeated Lifecycle Actions

## Status

ACCEPTED — implemented 2026-09-12 through 2026-09-20; backfilled 2026-09-21.

## Context and Problem Statement

A lifecycle command that was retried, or that arrived after its state had moved,
produced a generic failure. The agent that issued it learned that something went
wrong but not what, and could not tell a duplicate of its own successful request
from a genuine conflict. Recovery therefore had no evidence to act on: an agent
could not distinguish "already done" from "refused", so the safe response was to
block and wait for an orchestrator assessment.

Three further failure shapes shared that root. A preparation marker was treated as
obsolete only when its boundary moved or the *same* actor presented a newer
generation, so a preparation left by a restarted agent blocked every other agent
forever. The ownership tuple `assigned_to` / `lease_expires` could be written half
set — a rejection renewing the lease of a doer that was already gone — and because
`ops` mutations validate globally after mutating, one invalid task refused every
subsequent command, including the ones that would repair it. And a submission that
failed at write-state attached its cause to `Err`, which is deliberately withheld
from agents, so the channel that exists for recovery carried nothing.

## User-Confirmed Intent

The work originated in a live run, not in inspection. The user confirms the
motivating failures were observed during operation; the commit bodies name the
specific episodes — four rejected plans left permanently unclaimable, a provider
quota kill that stranded a lease, five tasks that `liza validate` could not move,
and two occurrences on `cpm-1-cp-2-code-4` where a coder correctly refused to
blind-retry and reported that no underlying cause was exposed.

## Considered Options

1. **Leave repeated actions as generic failures** — keep the existing refusal and
   let the orchestrator assess every ambiguous retry.
2. **Make the commands idempotent by re-executing** — treat a repeat as a fresh
   action and let the effect land twice where it is harmless.
3. **Return an authoritative outcome from a durable receipt** — record the request
   and its transition identity, and answer a repeat from that record without
   re-running the external effect.

## Decision Outcome

Chose **Option 3**. Lifecycle operations gain bounded durable request receipts,
transition identities, recovery outcomes and separate invocation metrics, while
preserving the existing authorization and external-effect fences. A repeated or
stale action returns what actually happened, not a generic error.

Four corrections completed the same principle — that a lifecycle operation must
leave repairable state and expose its cause:

- **Preparation retirement** consults the agent registry for the preparing actor's
  authenticated current generation. A generation the registry has replaced is
  retired for any caller. Absence from the registry is *not* retirement, because
  process abandonment retains its marker until inspected recovery, and a call
  without a generation cannot infer process death. The pre-mutation check and the
  completion guard share one predicate, so a metadata-only operation cannot pass
  the check, mutate, and then roll back at completion.
- **Lease renewal** no-ops on an unassigned task and clears a lease already
  dangling there, so the ownership tuple holds at the one place every caller
  shares and existing bad state self-heals on the next renewal.
- **`migrate`** normalizes the same tuple across all tasks in one pass, because
  `Blackboard.Write` persists without validating and is therefore the only path
  that can repair records that global post-mutation validation would otherwise
  refuse.
- **Write-state failures** carry the cause to the agent through `Details`, bounded
  to 512 bytes and secret-masked, rather than discarding it with `Err`.

Only known-failed preparations retire, so a corrected request can proceed without
retiring a preparation whose fate is unknown.

## Rationale

Re-execution (Option 2) was not available: these operations have external effects
whose fences exist precisely because repeating them is not harmless. Option 1 was
the status quo whose cost the live run measured — each ambiguous retry spent a
block, an orchestrator assessment and an unblock.

The receipt approach puts the evidence where the recovering agent already looks.
It also fixes the asymmetry that made the other three bugs expensive: each one
left state that was simultaneously invalid and unrepairable, because the guard
that rejected it also rejected its repair.

## Consequences

- Agents recovering from a retry receive an authoritative outcome and can act
  without an orchestrator round trip.
- Registration authority stays out of diagnostics, and protected contracts bind to
  transition identity rather than to actor identity alone.
- The registry becomes a dependency of preparation retirement. It is threaded
  explicitly, so a caller that must not consult it says so; the two such sites
  state why.
- Half-set ownership tuples self-heal, and pre-existing ones are repaired by
  `migrate` rather than requiring manual state surgery.
- The 512-byte masked `Details` channel is now load-bearing for recovery; future
  changes that route causes through `Err` alone reintroduce the original defect.

## Evidence and Reconstruction

Sources: commits `fa4964845` (#155), `04adbc660`, `aab3c362a`, `42a88c519`,
`1e2c0398e`, and the lifecycle results protocol they document. The user confirmed
on 2026-09-21 that a live-run incident drove the work. Historical alternative
evaluations were not supplied; the options above are reconstructed from the commit
bodies and the shape of the fences they preserve.

---
*Reconstructed from commits `fa4964845`..`1e2c0398e` (2026-09-12 to 2026-09-20).*
