# ADR-0209: Admission Limits for Heavy Canonical Commands

## Status

ACCEPTED — 2026-10-14. Amends ADR-0176 (the in-flight marker covers a slot wait) and ADR-0204 (the deferred runner shares the pool).

## Context

Every canonical batch starts as soon as its submit reaches execution, and the
deferred-validation runner starts its probes independently. Nothing bounded how
many heavy suites ran at once. When suites share a scarce resource (a database,
host CPU), overlapping batches slow each other into their wall-clock budgets.
In one run most budget blocks over two days came from this (D-153). The only
levers were raising budgets, which delays the detection of real hangs by as
much, and lowering `max_instances`, which caps sessions rather than suites.

## Decision

1. **Configuration.** `config.admission_limits` lists operator-defined classes:
   `{class, max_concurrent, match[]}`. A canonical command belongs to a class
   when its text contains any `match` substring. Operators manage it with
   `admission-limit set|remove` (operator-only; replacing a different
   definition needs `--replace --reason`). The engine infers nothing: classes
   apply to tasks already reviewed, with no contract change.
2. **Slots.** Per class, under `<runtime dir>/admission-slots/<class>/`,
   `max_concurrent` exclusive flocks (`slot-<i>`). Waiters queue as flock-held
   tickets in `queue/`, created and pruned under a short queue lock; only the
   earliest live ticket tries the slots (FIFO). A ticket or slot whose holder
   died is free, with no PID evidence. A batch matching several classes takes
   them in class order under one deadline; a refusal releases all it holds.
3. **Wait bound.** A wait plus the execution budget behind it never exceeds the
   acceptance cap (`AcceptanceMaxTimeoutSeconds`): wait ≤ cap − budget. The
   wait never counts against `timeout_seconds`; the batch clock starts once
   the slots are held. `MaxInflightSubmitGrace`, the Claude command ceiling
   and the INVARIANTS session bound are unchanged.
4. **Canonical execution** (`executeAcceptanceReceipt`, used by submit,
   `update-review-commit` and the orphaned-base heal) takes slots after the
   non-executing preconditions and before runtime inputs are acquired, so a
   full class spends no single-use input. A class that stays full is an
   `acceptance.admission_slot` refusal (fault class `admission_slot`): no
   command ran; retry the same commit. The heal treats it as transient.
5. **Submit pre-admission.** Before any lock, preparation or rebase, a submit
   whose task is executing loads its acceptance input read-only and waits for
   its classes, under the ownership fence. Its in-flight marker is written
   first, with window cap (+ grace). A full class returns `RETRYABLE`/`retry`
   with effects `none`: nothing was prepared, so the same commit and request
   ID are retried unchanged (no D-147 trap). A claim lost while waiting
   returns `STATE_CHANGED`/`requery`, or the authority error. Execution then
   uses the same limits snapshot and slots; a class pre-admission could not
   know (the reviewed commands changed before the rebase, or could not be
   loaded) is taken within the same deadline, and only that residual refusal
   is post-effect.
6. **Slots follow commands.** Each command inherits its slot descriptors
   (`ExtraFiles`). If the owner is SIGKILLed, the running command keeps the
   slot until it exits. On every normal return the owner unlocks, which also
   unlocks inherited descriptors, so a lingering descendant cannot keep it.
7. **Deferred runner.** Each probe (including bisect steps) takes its command's
   slots from the same pool, re-queuing behind waiting submits between probes.
   Amended 2026-10-19 (D-223): its wait has no bound; a halt ends it. Git and
   setup steps take no slot.
   An orphaned probe leaves its slots with the survivors, like the runner lock.

## Consequences

- Heavy suites run at most `max_concurrent` at a time per class, in arrival
  order; budgets no longer absorb contention. Unconfigured projects see no
  change (no slot, same marker window).
- A submit budgeted at the cap has no queue headroom: one try. Upgrade trigger:
  admission-slot refusals on large-budget batches; then raise the grace
  (ADR-0176) instead of sharing the cap.
- Per-batch granularity: a batch holds its slot during its light commands too,
  and a submit also during its rebase and index refresh.
- Crash ceiling: a descendant that daemonized under a SIGKILLed owner holds the
  slot until it exits; waiters then refuse at their deadline, naming the slot
  directory where the holder can be found (`fuser`). On Windows descriptors
  are not inherited, so a slot is held only while its owner lives.
- The reviewer-supervisor heal can block for wait + batch, within the cap.
- Lowering `max_concurrent` converges as current holders finish; removing a
  class leaves current waiters waiting for the slots they knew.

## Alternatives Considered

- **Resource tag in the reviewed acceptance contract.** Explicit, but every
  in-flight task would need re-review before the limit applied, and the count
  still lives in configuration. Config matching can coexist with it later.
- **A separate slot-wait cap with a larger grace (~8 h).** Gives cap-budget
  batches queue room, but grows the session bound, the provider command
  ceiling and the agent tool timeout. Deferred behind the trigger above.
- **Waiting after the rebase, inside execution.** Simpler, but a refusal there
  leaves a rebased worktree the same request cannot resubmit (D-147).
- **Unordered polling.** Fewer lines, but the runner re-acquires between bisect
  probes within microseconds and would starve waiting submits.

## Amendment (2026-10-15)

Agents' own runs of a limited command took no slot, so `max_concurrent` bounded
only the engine's share of the load (D-191). `admit [--max-wait <s>] --
<command>` now runs a local command through the same pool: its words, joined
with spaces, are matched like a canonical command; it waits up to `--max-wait`
(default 3600 s, at most the acceptance cap) for a slot of every matching
class, and a command matching none runs at once.

- **Priority.** Ticket names lead with a priority digit (`0` canonical, `1`
  local), so a local waiter queues behind every canonical waiter of a class.
  Priority is per class, not a global schedule across held classes. An older
  binary's tickets sort between the two. A running command is never preempted.
- **Workload.** The command runs in its own process group holding the slot
  descriptors; interrupts are forwarded to the group. After the leader exits,
  the group is reaped as in the deferred runner: slots are released once it is
  gone, else abandoned to the survivors and `admit` fails naming the slot
  directories. A descendant that left the group is not seen (shared ceiling).
  On Windows slots are released when `admit` ends.
- **Carrier.** Task prompts name the match substrings and the wrapper when
  limits exist; with runtime inputs, `admit` wraps `run-live`, never the
  reverse. Using it remains agent compliance (TECH_DEBT: mechanical enforcement).
  Upgrade trigger for a separate local cap: canonical waits dominated by local
  holders.

## Amendment (2026-10-19, D-223)

The wait bound (§3) protects a submit's in-flight grace; the deferred runner is
a detached process no grace bounds, yet it applied the same arithmetic, so a
probe budgeted at the cap got no wait and a full class aborted its whole batch.
A deferred probe now waits for its slots with no deadline, cancelled only when
its batch ends or a halt or stop is observed while waiting (ADR-0204 amendment
2026-10-19). Canonical submits, `update-review-commit`, the heal and `admit`
keep their bounds.

## Amendment (2026-10-22, D-178)

The upgrade trigger in Consequences fired: submits budgeted at the cap were
refused after 0 s, each retry waited 0 s again, and their doers blocked until
an operator unblocked them. Queue room shrank as the budget grew, so the
heaviest suites, the ones a class exists for, queued least. A submit now waits
`max(2 h, cap − budget)`: wait plus batch stay within cap + 2 h, and
`MaxInflightSubmitGrace` grows by the same 2 h (ADR-0176 amendment), so the
marker, the session deferral and the Claude command ceiling still cover both.
Only submits get the floor: `update-review-commit` and the orphaned-base heal
run under no in-flight grace and keep cap − budget. A class that stays full
for the whole wait still refuses as `RETRYABLE`; a retry queues again behind
the waiters already there. Priority order among canonical waiters and queued
waiters re-reading a raised limit (D-180) are unchanged.

## Amendment (2026-10-22, D-180)

A waiter fixed `max_concurrent` when it queued, so raising a limit admitted no
one already waiting: the head kept probing the old slot range while newer
waiters, queued behind it, probed nothing, and the new slots stayed idle until
every earlier waiter was served. Once the head waiter has found every slot
held, it now re-reads its class's `max_concurrent` on its next poll and then
about every 10 s, without the state lock and decoding only
`config.admission_limits`; uncontended acquisitions and non-head waiters read
nothing. A raise reaches the queue within about that interval, as a polling
bound, not a guarantee. A lowered limit applies to the head's next probe; a
removed class, an invalid entry or an unreadable state keeps the last known
limit, so removal still leaves current waiters waiting for the slots they
knew. A waiter keeps the class it matched, deadlines and queue order are
unchanged, and lowering still converges as current holders finish.

## Amendment (2026-10-22, D-218)

The upgrade trigger in the 2026-10-15 amendment fired. Local `admit` runs
(2h45 and about 2 h) held db-suite slots while a deferred batch waited, and
since nothing is preempted the batch could not start until they ended. Deferred
probes also shared the canonical priority, so a long probe queued ahead of a
submit delayed it, even though the submit has a bounded wait and the probe has
none.

- **Three tiers.** The ticket's leading digit is now its tier: `0` canonical
  (submit, `update-review-commit`, the heal), `1` deferred probe, `2` local.
  Each class serves them in that order, FIFO within a tier, and priority stays
  per class.
- **Local cap.** A local run never takes `slot-<max_concurrent − 1>`, so at
  most `max_concurrent − 1` local runs hold a class at once and canonical and
  deferred work always keeps a slot. A local waiter at the head with only that
  slot free does not block anyone: a later canonical or deferred ticket sorts
  ahead of it and takes the slot. The cap follows a limit re-read under D-180.
- **Ceiling (human decision).** A class with `max_concurrent = 1` keeps no
  reservation, so `admit` can still run there. Making `admit` refuse such a
  class was rejected because agents would run the command unslotted. On such
  a class a running local command can still delay canonical work until it
  exits.
- **Mixed binaries.** A pre-D-191 ticket (no digit) now sorts after canonical
  and ahead of deferred. A binary from D-191 until this amendment writes its
  probes as `0`, so they rank as canonical, and its `admit` as `1`, which ties
  with new probes in time order. Older binaries try every slot, so the local cap holds only once older
  admission-capable processes, including a detached deferred runner, have
  drained or been upgraded. There is no compatibility code.
- **Not changed:** non-preemption; probes may still hold every slot of a class
  and delay a submit until one finishes. Upgrade trigger: submits refused
  `RETRYABLE` while probes held the class.
