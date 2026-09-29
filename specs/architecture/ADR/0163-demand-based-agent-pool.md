# ADR-0163: Demand-Based Agent Pool

## Status

ACCEPTED — 2026-09-29. Closes GitHub issue #169.

## Context

Pool auto-repair started at most one agent per role, and only when the role
had no live usable registration. One busy or idle agent therefore masked any
backlog: in an observed run one architect took three to four claimable tasks
serially, with claims waiting up to 7.8 minutes, until the operator started
more by hand. The operator skill read as if auto-repair scaled roles out.

The opposite also held. Idle doers and reviewers waited up to five hours for
work. Each idle supervisor renews its heartbeat under the state lock every
minute and re-reads the whole state file on every state write, so idle agents
added lock contention and parse cost that grows with the file size.

Registration already capped live agents per role, but only the orchestrator
had a cap; `max-instances` 0 meant unlimited.

## Decision

The pool size of every non-orchestrator role follows its claimable work,
within a cap. There is no minimum pool: the orchestrator keeps its floor of
one through orchestrator auto-repair while a goal runs, and other roles may
drop to zero.

- **Cap.** A role's effective `max-instances` is its own pipeline value, else
  `config.max_instances`, else 3. Registration enforces it with the existing
  lease-first occupancy test, so a leased registration whose process died still
  holds its slot. The orchestrator stays hard-coded at 1. The project default is
  read from state at the two consumers (registration and pool repair) instead of
  being threaded into every resolver construction.
- **Scale-up.** For each role, pool repair counts ready tasks that idle agents
  do not cover and starts that many, bounded by the cap minus occupied
  registrations. An agent covers demand only if it holds no task and passes claim
  admission (provider, live process). Reviewer coverage is matched per task
  against the existing claim filters. Reviewers are started under an explicit
  ID: the lowest free `<role>-N`, skipping IDs of starts still registering,
  that the production claim filters accept for the task when projected as a
  reviewer registered with the spawn CLI as provider, in an isolated state
  projection. Auto-assignment would take the first free ID, which is a prior
  approver's once it exited, and could never supply the quorum's next
  approval. Tasks no such reviewer could claim are reported as unservable
  rather than staffed. A task with a known validation
  failure for a usable agent of the role is not demand, for doers and reviewers
  alike.
- **In-flight starts.** A started supervisor signals readiness before it
  registers. The watcher tracks each process it started until the process
  registers or is seen to exit (dead, or its PID names another program),
  independently of current demand, and counts it as covering demand meanwhile.
  An unobservable process is treated as alive. One still unregistered after
  five minutes counts once toward the failed-start suppression but keeps its
  reservation. The failed-start count is consecutive and resets on a
  registration. This bookkeeping is watcher-local; after a watcher restart,
  registration's cap is the only guard, as before.
- **Scale-down.** Agents leave on their own: an idle supervisor exits when its
  wait deadline passes with no claimable work, as it already did. The default
  doer and reviewer wait drops from five hours to ten minutes; the
  orchestrator's stays at five hours. The watcher never terminates agents.
- **Settings.** `init --max-instances` sets the project default; after init,
  `config set` accepts `config.max_instances`, `config.doer_max_wait` and
  `config.reviewer_max_wait` under the existing compare/replace rule for runtime
  configuration. These keys are operator-only. A changed wait applies to
  supervisors started afterwards.

## Consequences

- Fan-out stages run in parallel up to the cap without operator staffing.
- Idle roles stop heartbeating and re-reading state after ten minutes.
- Scale-up needs a running TUI or headless watch with auto-repair enabled.
  Without one, a role that idled out stays empty until an operator starts an
  agent; with five-hour waits this rarely happened, with ten minutes it is
  routine.
- Non-orchestrator roles go from unlimited to 3 live agents by default; a
  fourth manual start is refused at registration until the cap is raised.
- A role whose work arrives in bursts pays supervisor start-up more often.
- Greedy reviewer matching can overestimate demand for unusual eligibility
  patterns; the overshoot is bounded by the cap, and extra agents leave after
  their wait.
- Existing workspaces keep their stored waits (often five hours) until changed
  with `config set`.

## Alternatives Considered

- **Watcher terminates idle agents.** Rejected: a kill decided on one snapshot
  races a claim made before the signal lands, orphaning the claim — the class of
  failure fixed repeatedly in claim takeover and quota-termination paths.
- **A minimum pool size per role.** Rejected: idle agents are the cost being
  removed, and supervisor start-up is cheap compared with a task.
- **Default cap as a resolver option.** Rejected: resolvers are built in many
  places without state, so the default would silently not apply to some of
  them.
- **Keep counting starts per spawn for suppression.** Rejected: under sustained
  demand with successful starts, the role would be suppressed as failing.
- **Global agent cap.** Deferred until a run shows the per-role cap is not
  enough.
