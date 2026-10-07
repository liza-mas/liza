# 71 - Automatic Checkpoint Summary on Merge

## Status

ACCEPTED

## Context

Checkpoint summaries are one of the most valuable features for keeping users able to steer as Liza becomes more autonomous. As agents make more decisions between human interventions, relying only on logs leaves too much intent and context behind. Logs record activity; they do not reliably preserve why agents chose a path or what decisions were made along the way.

Capturing events and decisions from the blackboard is more solid than reconstructing them later through log archaeology. Manual checkpoint-summary usage was the first step. Automatically producing a fresh summary after successful merges is the next step in making decision capture a built-in steering aid.

## Decision

When the sprint reaches a checkpoint, Liza automatically invokes the configured agent CLI with the `checkpoint-summary` skill and writes the latest report to `.liza/checkpoint-summary.md`.

The operation is best-effort:
- nothing depends on summary generation
- failures are logged rather than rolled back
- `auto_checkpoint_summary: false` disables the behavior
- the subprocess may only mutate `.liza/checkpoint-summary.md` (instructed by the skill, no longer checked after the run: see the 2026-09-30 amendment)

**Amended 2026-09-21 — the trigger moved from every merge to the checkpoint boundary.** The original decision named merges as "the natural steering point after merged progress". Run evidence contradicted that: a merge is a task-level event, a checkpoint is the steering point. Emitting per merge produced one CLI subprocess per merged task — 86 in one observed sprint — each spawned inside the reviewer's merge loop, each re-reading a state file that had grown to 2.69 MB. It also produced the concurrent-writer race recorded as OP-006, because several reviewers could merge at once and write the same report.

The checkpoint producers record a durable obligation — `state.pending_checkpoint_summary` — in the same transaction that creates the checkpoint. The orchestrator claims that obligation (clearing it under the lock) and then writes the report. Only the orchestrator emits, so a checkpoint costs one CLI invocation rather than one per role, and claim-then-emit bounds it to one attempt: a failing CLI cannot make every later poll retry.

It is observed from the two points where the orchestrator reliably reads fresh state — the supervisor pause gate, which covers a supervisor started or parked at a checkpoint and runs before auto-resume, and the orchestrator's own work-detection poll, which covers a checkpoint created while it is already idle.

Three narrower designs were tried and each was reachable only some of the time, which is why the obligation is durable and lives outside `sprint`:

- Emitting after an orchestrator turn missed every checkpoint created while the orchestrator was idle, because the pause gate blocks it before execution, and missed a supervisor restarted at a checkpoint.
- Requiring the sprint to still be at `CHECKPOINT` lost the report whenever another role's auto-resume moved the sprint on first; auto-resume is role-generic, and ordering calls inside one process does not order separate supervisors.
- Keying on `Sprint.Timeline.CheckpointAt` lost it again when a terminal checkpoint was carried through `COMPLETED` into a new sprint: `applySprintAdvance` replaces `Sprint` wholesale and its new timeline keeps only `Started`.

`applyCircuitBreakerResponse` also now stamps `CheckpointAt` like the other producer, since a breaker checkpoint previously carried no identity distinguishing it from the one before. It still leaves `CheckpointTrigger` alone, which may carry a transition the orchestrator has not executed yet.

This amendment supersedes alternative 2 below ("keep checkpoint summaries manual"), which remains rejected: the report is still automatic, just at a coarser and more meaningful boundary. It also removes the motivation for building report isolation and revision binding, since concurrent writers are no longer structurally possible.

**Amended 2026-09-30 — the post-run mutation guard is removed.** The runner used to compare `git status` and per-path size and modification time before and after the subprocess, and to fail on any path other than the report that appeared, changed or disappeared. It cannot tell which process wrote a path, and the run lasts up to five minutes while supervisors, agents, Git hooks and the operator keep writing to the project root by design. In checkouts where the runtime directory is not ignored, any runtime write during the run tripped it, and the checkpoint reported the summary as failed ("not written") although the report had been written. A committed file rewritten by a hook tripped it even where the runtime directory was ignored. The guard only detected after the fact; it never prevented or reverted a write, and it was already blind to the runtime directory wherever that directory is ignored. It was removed rather than narrowed, knowing that a summary CLI which edits project files is no longer reported. The report-only rule remains an instruction in the `checkpoint-summary` skill. Launch failure, non-zero exit, timeout and a missing or empty report are still reported.

**Amended 2026-10-07 — the summary runs in the background on a bounded scope (#171).** The orchestrator ran the CLI in its own loop, bounded by five minutes, and told it to read every artifact the run had referenced. As runs grew, the summary failed at every checkpoint: in one run 16 of 16, 13 by timeout and 3 by exiting without writing the report, after the session delegated the reading to subagents, scheduled a wakeup and ended its turn. In the DEV-766 run it timed out at every checkpoint. Each attempt held the orchestrator for the full five minutes, and operator notes waited behind it. The claimed obligation now runs in a background goroutine of the orchestrator supervisor, one at a time; a checkpoint raised meanwhile keeps its obligation for the next poll. The run uses the supervisor's context, so a stopping supervisor kills the CLI, and the exit drain waits for a running summary. Checkpoint producers record `since`, the previous checkpoint, in the obligation; the prompt names the tasks merged in `(since, at]`, newest first and at most 25, and limits reading to their own artifacts. It also requires the report within one turn, without subagents, wakeups or background work, and Claude runs it with `--disallowedTools Task`. A missing or empty report is reported with the output tail, like any other failure. A full review of the whole run stays a manual skill run.

The summary is intentionally generated from blackboard state rather than from raw logs.

## Consequences

Positive:
- Humans get fresh steering context after merge progress without remembering to run a manual summary.
- Decisions and agent intent are captured closer to the source of truth.
- The report lives under `.liza`, avoiding user-owned project docs.
- Summary failures do not block successful merges.
- The mechanism creates a foundation for richer decision capture later.

Trade-offs:
- Summary generation adds a subprocess dependency to the merge loop.
- The report is only as good as the blackboard and skill interpretation.
- Best-effort behavior means a missing summary does not halt the system.
- Mutation guarding is required to prevent the subprocess from changing project files. (Superseded 2026-09-30: nothing enforces the report-only rule; the skill instruction is the only control.)

## Alternatives Considered

1. Rely on logs.

Rejected because logs lose intent and decision context.

2. Keep checkpoint summaries manual.

Rejected because the feature is most useful when it appears at the natural steering point after merged progress.

3. Make summaries part of merge success.

Rejected because preserving user steering context is valuable, but a summary failure should not roll back or block already-successful merge work.

4. Keep the per-merge trigger and add report isolation and revision binding.

Rejected by the 2026-09-21 amendment: moving the trigger to the checkpoint boundary removes the concurrency the isolation work would have managed, at a fraction of the cost.

## Relationship to Prior Decisions

Extends ADR-0049 (Structured Handoff Events) by treating structured blackboard data as durable context for human steering. Complements ADR-0055 (Integration Sub-Pipeline) and later decision-capture work; automatic checkpoint summaries are a step toward richer built-in decision capture.

---
*Reconstructed from commit cdc2981e and user context (2026-05-26)*
