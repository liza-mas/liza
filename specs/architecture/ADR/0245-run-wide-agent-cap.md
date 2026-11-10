# ADR-0245: Run-Wide Agent Cap with Slot Reservations

## Status

ACCEPTED — 2026-11-10. Closes GitHub issue #212. Amends
[ADR-0163](0163-demand-based-agent-pool.md) (scale-up is bounded by the run as
well as the role) and [ADR-0205](0205-surplus-supervisors-self-release.md) (a
second surplus condition).

## Context

Liza capped live agents per role (`max-instances`, ADR-0163) but not per run.
An operator with a machine-wide limit, such as 9 agents on a shared host, had
to derive a per-role cap from the number of roles active at once and redo it at
each phase change. A cap that fit one phase overshot or starved another.

A plain run-wide cap starves roles. Pool repair starts agents only for
uncovered demand, and agents leave only when idle or over their role's cap.
Under a backlog a doer that finishes a task claims the next one, so a role that
filled the run never frees a slot. A role whose work appears later, such as
review behind a coding fan-out or the merge that unblocks dependent tasks,
waits until the backlog drains and the doers idle out.

## Decision

1. **Configuration.** `config.max_agents` (default 9, minimum 3) counts every
   live agent, orchestrator included. Three is the orchestrator plus a doer
   and a reviewer: a review loop keeps both alive at once, each waiting on the
   other (ADR-0054). `init --max-agents` sets it, and
   `config set config.max_agents` changes it (operator-only, `--replace
   --reason`). One slot is always held for the orchestrator: other roles share
   `B = max_agents − 1`, alongside their per-role caps. Occupancy is lease-first,
   as for the per-role cap.
2. **Reservations.** When a role has actionable work and no occupied
   registration, pool auto-repair records a reservation for it in state
   (`pool_reservations`: role, `selected_at`, `until`).
   - **Demand.** The demand counted is repair's own (claimable and reviewable
     work, and ownerless approved merges), excluding unservable, provider-blocked
     and suppressed work. It is taken before pending starts and the run bound
     are applied, so a full run still yields candidates.
   - **Order and limit.** New reservations are appended in wait-age order: the
     earliest time any of the role's tasks became ready, approximated as the
     later of the task's last history entry and its dependencies' last entries.
     Ties go by role name. At most `B` are held.
   - **Lifetime.** A reservation is kept while its role has no occupied
     registration and either still has demand or has a pending start. It is
     refreshed at half-life of a 5-minute TTL. It is dropped on registration,
     when demand and pending starts are gone, or when the role is suppressed.
     Readers ignore an expired entry, so a dead watcher cannot hold slots.
   - **Writer.** The watcher is the only writer, and writes only when the
     ordered list or an expiry changes. This is the watcher's first state write.
     It touches no agent or task.
3. **Admission.** Registration refuses a non-orchestrator when `O ≥ B`, or
   when its role is not reserved and `O + |R| ≥ B`. Here `O` is occupied
   non-orchestrator registrations and `R` is the unexpired reservations of
   roles with none. The orchestrator keeps only its singleton check.
4. **Retirement.** At a task boundary, with no in-flight work (ADR-0205's
   pinning), an agent of a role with the most occupied registrations (ties all
   count) is surplus in either case:
   - **Over the cap.** `O > B`, which is how a lowered `max_agents` converges.
   - **Handoff.** `O + |R| > B` and the agent has a task history entry
     attributed to it since it registered (a claim, submission, verdict or
     merge). This keeps a newly admitted agent from handing its slot straight
     back before doing any work.

   The locked recheck in `RetireSurplusAgent` stops retirements exactly at the
   limit.
   - **Waits yield.** A doer blocked in `await-verdict` holds its task
     (`CurrentTask`) and a reviewer blocked in `await-resubmission` holds its
     review claim, so neither ever reaches a boundary while it waits. Each wait
     checks the same run-wide condition (`RunCapYieldDue`) on every state read
     and, when it holds, ends with `ABORTED` (`safe_action: stop`). That
     releases what it holds, like any other non-poll finish, and leaves the
     submitted work or the rejection on the task. The agent exits its session,
     and its supervisor retires at the next wait. A verdict or resubmission
     that has already arrived is handled first. Without this, a run whose
     slots are all held by waiting doers never frees one for the reviewer they
     wait on.
5. **Allocation.** `assessRepairDemand` bounds every repair entry point
   (watcher, manual `repair-agent-pool`, dry run) after pending starts and the
   role filter.
   - Each reservation holds one slot. The i-th is funded when `O + P_u + i ≤ B`,
     where `P_u` counts pending starts that do not fill a reservation.
   - A funded reservation with no pending start gets one start.
   - Other starts, including further starts of a reserved role, take
     `B − O − P_u − |R|` in wait-age order.
   - The dry-run result carries `run_budget`.
   - The watcher stamps backoff only on roles that started or failed to start,
     so a role held by the budget starts on the tick its slot frees.
6. **Observability.** One `RUN CAP` warning per reservation that needs another
   role to give up a slot, naming the donor roles. Registration refusals name
   the cap and the reserved roles.

## Consequences

- Live agents never exceed `max_agents` at registration. Lowering it converges
  at task boundaries, as a lowered `max_instances` does.
- A starved role's wait has three parts. The first is the current task of an
  agent in a donor role, because nothing is preempted. The second is the
  reservations ordered ahead of it. The third is the watcher tick and supervisor
  start-up. Only most-occupied roles donate, so a smaller role reaching a
  boundary while every donor is busy does not donate. This trades latency for
  less churn.
- At small caps, roles take turns at task granularity, and each turn costs a
  supervisor start. A wait that yields gives up the session ADR-0054 keeps
  alive across review cycles: a rejection is reworked, or a resubmission
  reviewed, by a fresh session without the earlier context. This happens only
  while the run is full and another role waits.
- Several waits can see the same reservation write and yield together. The
  locked recheck retires only as many as the budget needs; the others keep
  their slots, and their review cycle continues in a fresh session. That costs
  extra session restarts, never a slot.
- An agent whose reserved work vanishes before it acts cannot donate. It leaves
  through the normal idle exit.
- Starvation relief needs a running watcher with auto-repair. Without one,
  nothing would start the starved role anyway, and the cap is still enforced at
  registration.
- Existing workspaces get the cap of 9 on upgrade. With the per-role default of
  3, it binds only when three non-orchestrator roles are full at once.
- After a watcher restart, pending-start tracking is lost, as before.
  Registration's caps remain the limit.

## Alternatives Considered

- **Each reader derives starvation from state.** Rejected. Repair's exclusions
  (unservable reviewer work, blocked providers, failed-start suppression,
  pending starts) live in the watcher or in `commands`. A false positive holds a
  slot nothing can fill, which stalls a run at a small `max_agents`. Recomputing
  demand on every waiting agent's state read is costly.
- **A floor of one agent per role.** Rejected. When every occupied role is at
  one, the waiting role never gets a slot.
- **Preempt busy agents.** Rejected for the reason in ADR-0163: a kill races a
  claim. Retirement stays inside the agent's own loop at a boundary.
- **A general fair-share scheduler.** Not needed: reservations plus the
  existing self-release solve the issue.
