# ADR-0205: Surplus Supervisors Self-Release

## Status

ACCEPTED — 2026-10-13. Amends [ADR-0163](0163-demand-based-agent-pool.md)
(Scale-down). Amended by [ADR-0246](0246-run-wide-agent-cap.md): an agent of a
role with the most agents is also surplus when the run is over
`config.max_agents`, or when a reserved role is waiting for a slot and the
agent has acted since it registered.

## Context

ADR-0163 made `max-instances` an admission cap: registration refuses an agent
beyond it and pool repair starts none beyond it. Nothing acts on agents already
registered when the cap drops. After `config set config.max_instances 2` with
three coders, all three keep running. An idle one leaves only when its wait
deadline passes with no claimable work. Under a steady backlog none ever goes
without work, so the role stays over its cap for as long as there is work,
which is when lowering the cap matters.

ADR-0163 rejected having the watcher terminate idle agents: a kill decided on
one snapshot races a claim made before the signal lands.

## Decision

A doer or reviewer supervisor whose role is over its effective `max-instances`
and that holds no in-flight work retires itself. The watcher still never
terminates agents.

- **Where.** The work wait checks a read-only surplus predicate on every state
  read, before it checks for work. Every task boundary passes through that
  wait, so a surplus agent retires instead of claiming even when work is
  waiting. Parked agents see a cap change on the state write that makes it.
- **Surplus.** The role is not an orchestrator, and the agent's other live
  registrations, counted with registration's lease-first occupancy, already
  fill the effective cap (role value, else `config.max_instances`, else 3).
- **In-flight work pins the agent.** A current task; an executing or handoff
  doer assignment; rejected rework under an unexpired lease, which only this
  agent may reclaim; any review claim; a pending approved merge it owns.
  Attribution kept on submitted, approved or terminal tasks does not pin.
- **Retirement.** One generation-fenced transaction re-evaluates the predicate
  on the locked state and, if it still holds, applies the same release-and-
  remove mutation as an idle exit: remaining assignments are released, task
  status, worktree, review commit and history kept, and the row removed.
  Transactions are serial, so each decision sees earlier removals and
  concurrent retirements cannot take a role below its cap. When the locked
  recheck disagrees with the snapshot (a peer left, the cap rose), nothing
  changes and the supervisor retries after a short delay.
- **Unresolvable pipeline or role** fails closed: the agent stays.

## Consequences

- Lowering `config.max_instances` converges at the next task boundary of each
  surplus agent, under load as well as idle. Raising it is unchanged:
  pool repair scales up within the new cap.
- A surplus agent leaves while work is waiting; its peers take that work.
- An agent whose dormant in-review task is released at retirement loses the
  doer affinity for that task, as it does today after an idle exit.
- Occupancy is lease-first, so a dead registration whose lease has not expired
  still holds a slot, and a live agent can retire in its place until it
  expires.
- Agents held at a PAUSE gate retire only after resume, when they reach the
  wait.

## Alternatives Considered

- **Retire only when the wait finds no work.** Rejected: under a backlog the
  pool never shrinks.
- **Watcher terminates surplus agents.** Rejected for the reason in ADR-0163:
  the kill races a claim. Self-release decides inside the agent's own loop,
  where it has no claim in flight, and re-checks under the state lock.
- **Treat every retained assignment as in-flight.** Rejected: submission and
  merge keep `assigned_to`, so any agent that ever submitted would pin capacity
  until its next claim.
