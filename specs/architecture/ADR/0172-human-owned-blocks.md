# ADR-0172: Human-Owned Blocks Raise AWAITING HUMAN

## Status

ACCEPTED — 2026-10-01.

## Context

A task blocked on something only a human can do (provisioning, credentials,
access, a product decision) raised the same warning-level `BLOCKED` alert as any
other block, then a `STALLED` warning saying "read blocked_reason". The human
was never told a request was waiting. `AWAITING HUMAN` fired only for a paused
run, a checkpoint, or a held plan (ADR-0159). `UNRESOLVED BLOCKED` needs an
orchestrator assessment, which does not exist while the orchestrator is dead.
In one run, a task held on operator provisioning stalled the critical path for
three hours this way.

The block had no structured owner of its unblock action, so no alert could tell
human-owned blocks from agent-owned ones. Runtime-input refusals (ADR-0169)
knew the operator had to act, and still raised only `BLOCKED`.

## Decision

1. **The ask is explicit and episode-scoped.** A blocked episode is human-owned
   when it carries `awaiting_human: "<ask>"`, a non-blank, single-line string in
   `TaskHistoryEntry.Extra`. Producers:
   - `mark-blocked --human-action "<ask>"` writes it on the `blocked` entry;
   - both runtime-input refusal paths (claim and submission) write the operator
     question on the `blocked` entry, with no orchestrator involved; its list of
     refused inputs is cut to whole codes within 1024 bytes plus a count of the
     rest, while the blocked question keeps them all;
   - `assess-blocked --human-action "<ask>"` sets or replaces it on the
     assessment entry, `--clear-human-action` drops it, and an assessment with
     neither carries the current ask forward, like `awaited_tasks`.

2. **One reader.** `models.CurrentAwaitingHuman` reads a BLOCKED task's current
   episode, from its latest status-transition event: the episode's opening
   entry and each later assessment set the ask, and the last one decides. A new
   episode starts with a new `blocked` entry, so no writer has to clear a
   stale ask, and legacy state, which has no key, is not human-owned.

3. **One alert per ask occurrence.** The watch raises a critical
   `AWAITING HUMAN` naming the task and the ask. The occurrence is the first
   entry of the final unbroken run of that ask, identified by its history index
   (history is append-only) and time. The alert's identity and once-ledger key
   are that occurrence, so all watchers and restarts log it once, carrying it
   forward does not repeat it, and clearing or changing it and coming back
   alerts again. `BLOCKED` still fires alongside it with the reason.
   `STALLED` names the human-owned blockers.

4. **The ask is assessment material.** It enters the assessment fingerprint only
   when present, so recorded digests stay comparable, an unchanged carried ask
   is `NO_CHANGE`, and setting, changing or clearing it is a material change.
   The wake reader derives the same ask, so it agrees with the writer.

## Consequences

- An operator sees a request with its action, at the level of a request, even
  with no orchestrator alive.
- Blocks recorded before this change, and free-text human blocks written
  without `--human-action`, raise no `AWAITING HUMAN` until the orchestrator
  reclassifies them. Adoption depends on agent guidance in the blocking
  protocol and the blocked-task wake; the runtime-input case needs none.
- Emission is watch-only: the alert follows the block by up to one watch
  interval, while `BLOCKED` is immediate.
- An ask cleared and set again in the same episode alerts again; that is the
  intended re-arm.

## Alternatives Considered

- **Match "human"/"operator" in reason text.** Rejected: free text yields false
  positives in a critical alert, misses blocks phrased otherwise, and carries
  no precise ask.
- **Treat every BLOCKED task without a live orchestrator as awaiting a
  human.** Rejected: it pages the human for agent-owned blocks and conflates a
  missing orchestrator (its own alert) with a human prerequisite.
- **A Task field.** Rejected: twelve paths open a blocked episode, and each
  would have to clear it, as would unblock, recover and replace. The
  episode-scoped history value cannot leak into a later episode.
