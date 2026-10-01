# ADR-0171: Cap Agent Blocked-Recovery Replacements

## Status

ACCEPTED — 2026-10-01.

## Context

A blocked-task chain is a lineage where a task blocks, an agent replaces it, the
successor blocks again, and the cycle repeats. The run stays busy and nothing it
was meant to deliver advances. The replacement-lineage policy already told
orchestrators to state what a successor changes before replacing, and to review
the lineage when a successor blocks again. Nothing enforced either rule, and
validating them by hand would cost a large set of manual role exercises for an
occasional failure.

Lineage was also recorded inconsistently. `supersede-task` sets only the
original's `superseded_by`; `replan` sets only the successor's `supersedes`;
plan-declared replacement sets both. A detector reading one direction misses
whole paths.

A detector alone does not hold the cap. Between detection and human analysis an
agent can replace the blocked head, which removes the evidence.

## Decision

1. **Blocked recovery is marked.** Replacing a task that is `BLOCKED` at
   supersession time is a *blocked recovery*. The shared supersession core
   (`supersedeTaskInState`, used by `supersede-task`, `replace-task` and
   plan-declared replacement) records `blocked_recovery`
   (`blocked_reason`, `blocked_at`, `changed`) on the original's `superseded`
   history entry. Superseding without replacements is not a recovery.

2. **A stated change is required.** A blocked recovery needs a non-empty
   `changed` statement: `supersede-task --changed`, the `changed` field of a
   `replace-task` payload, or `changed` on the plan output. `set-task-output`
   requires `changed` on every output that names `supersedes`, whatever the
   original's status, because the original may block before generation. The
   statement is masked and included in the lifecycle request fingerprint. The
   engine records it next to the cause; it does not judge its truth.

3. **Agents are capped.** `models.BlockedRecoveryCapped` is the single rule. A
   `BLOCKED` task whose lineage already contains
   `MaxAgentBlockedRecoveries` (2) blocked recoveries cannot be replaced. The
   lineage is read from both `superseded_by` and `supersedes`, counts only marked
   predecessors, and is guarded against cycles per path. Unmarked and legacy
   entries do not count, so a first block after replans or ordinary
   supersessions is never capped. Supersession, plan-output admission and
   detection all call this rule. A refusal leaves state unchanged; at
   generation it is reported like any other plan-replacement refusal.

4. **Detection reports the capped task.** The `blocked_replacement_chain`
   circuit-breaker pattern (severity `RECOVERY_CONVERGENCE_DEGRADED`, response
   `HALT`) reports the first capped, unreleased task. The cap keeps that task
   `BLOCKED`, so the evidence is still current when a human runs `analyze`.

5. **Release is scoped to one episode.** The response and its history entry
   carry a typed `subject` (`task_id`, `blocked_at`). `resume` resolves it as the
   sole release action. A resolved `blocked_replacement_chain` entry releases
   only a task whose ID and blocked-episode time equal its subject. The episode
   is when the task entered `BLOCKED`, which is its most recent status-transition
   event. That event is `blocked` on every path except a failed
   `recover-task --fresh`, which records `task_recovery_fresh_failed`. One
   release permits one further replacement. The successor's next block, or the
   same task's re-block, is a new episode and is capped again. A missing subject
   never releases, and neither does a task with no recorded episode.

## Consequences

- Repeated blocked recovery now ends in a human decision instead of an
  unbounded chain. The goal's legitimate blockers keep their existing holds.
- Each recovery's cause and claimed change are recorded together, and the halt
  evidence shows them side by side.
- Prompts teach only the atomic replacement paths: `replace-task` for one
  replacement, a corrective plan output for several. They never create
  replacements with `add-tasks` before `supersede-task`, because a refused
  supersession would leave those tasks runnable and unlinked.
- New refusals:
  - An orchestrator still on an old prompt hits a precondition error that names
    the missing `--changed` or the cap and its escalation route.
  - A plan output submitted before this change, with no `changed`, whose
    original is now `BLOCKED`, is refused at generation. The corrective-plan
    route applies.
- A single capped lineage halts the whole run, but only after a human runs
  `analyze`, and `resume` lifts it. Independent capped chains are reported one
  at a time.
- Gaps not covered:
  - Cancelling a task and adding an unlinked one (`cancel-task` + `add-tasks`)
    leaves no lineage, so it escapes the cap. The lineage policy forbids this;
    the engine does not detect it.
  - `resume` itself carries no RBAC check, which is true of every circuit-breaker
    release.

## Alternatives Considered

- **Detection only:** rejected. Replacement before analysis removes the
  evidence.
- **A WARNING response:** rejected. A warning is not an acknowledgement
  boundary, so it would repeat on every analysis.
- **A dedicated human release command:** rejected. `resume` after the halt
  already exists and leaves an audit timestamp.
- **Releasing on any resolved pattern entry:** rejected. One `resume` would
  silently release every capped chain.
- **Exempting plan-declared replacement:** rejected. It is a main automatic
  replacement path.
