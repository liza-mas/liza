# ADR-0174: Contention-Safe Agent Recovery

## Status

ACCEPTED — 2026-10-04.

## Context

A retryable state-lock timeout could terminate a healthy supervisor during
metadata/reset writes, lose a reached review verdict, or strand an attempt
rollover after its Git artifacts had been deleted. Interrupted doer claims
also consumed another iteration on reclaim. Forced recovery treated a failed
state read as if the task were absent, permitting destructive Git-only cleanup.

Existing snapshot inspection, reviewer candidate quarantine, preparation
retirement and generation-fenced lifecycle receipts remain the shared
boundaries. Increasing every timeout would still lose findings at process exit
and would delay ordinary command failures.

## Decision

1. Supervisor blackboards opt into cancellable acquisition retries: 100 ms
   backoff doubling to five seconds. Only acquisition timeouts retry; callbacks
   execute at most once. Completed-turn bookkeeping and unregistration use
   independent 60-second cleanup contexts. Runtime-input name discovery uses an
   atomic inspection snapshot;
   launch authority and prerequisites are still checked at their committing
   boundaries. A timeout proven to precede provider start is a coordination
   outcome, releasing the unstarted claim without crash/spin accounting.

2. Authenticated reached verdicts use a private, atomically published local
   envelope before submission. It retains the original opaque generation,
   immutable reviewed SHA, expected transition, request ID and payload digest;
   prose is sanitized. Supervisors drain envelopes before registration and
   after provider turns, retrying acquisition timeouts. Existing receipts settle
   exact replays; generation replacement routes findings to quarantine.
   Cancellation preserves unsettled envelopes. Review lease renewal requires
   the matching active provider session, including passive waiting during
   that session, rather than merely a living supervisor.

3. Attempt rollover finalizes state before removing its branch/worktree, while
   holding the existing task worktree lock through cleanup. Failed finalization
   retains the sentinel and Git artifacts for explicit recovery. Forced task
   recovery requires a successful state read; only proven absence permits
   Git-only cleanup.

4. Fresh worktrees use the captured immutable integration SHA, and persisted
   `base_commit` reflects their actual HEAD. An invalid preserved base blocks
   the candidate with an explicit repair action rather than looping or deleting
   work. If integration history removed the old base, a clean worktree whose
   HEAD equals that base has no task commits and advances to the captured
   integration SHA; a worktree with task commits requires repair. Interrupted
   executing claims and stranded takeovers carry the current
   positive iteration as a continuation. Other preserved claims enforce the
   effective cap; continuation never waives the review budget. Watcher warnings
   use those same caps.

5. Agent-pool autorepair suppression expires after five minutes. Restart
   attempts still respect pending starts, role capacity and provider checks;
   renewed failures begin a fresh suppression episode.

## Consequences

The state schema and lifecycle authority model stay intact. Supervisor writes
can wait through sustained contention until cancellation, while ordinary CLI
operations retain bounded acquisition. Verdict envelopes survive process exit
and never silently become requests at a new boundary. Interrupted rollover
requires explicit repair, with its evidence preserved.

Pinned integration equality and fresh prerequisite checks remain mandatory.
Selective revalidation is unsafe without declared validation dependencies;
unrelated-looking paths may still affect a command. Whole terminal-record
archival and splitting hot agent metadata remain the measured follow-up in
[Tech Debt](../../../TECH_DEBT.md#terminal-task-history-stays-in-live-state),
because history positions and lifecycle identities currently depend on live
records.

## Alternatives Considered

- Longer global lock timeouts: delay commands without making process-exit
  recovery durable.
- Replay complete mutation callbacks: can duplicate effects or apply stale
  authority; retry only acquisition or exact lifecycle requests instead.
- Remove Git artifacts before finalization: loses the only recoverable task
  evidence if the state write fails.
- Loosen integration equality or split state immediately: the former weakens
  validation, and the latter needs a separate migration of logical history and
  cross-record transactions.
