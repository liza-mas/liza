# 53 - Supervisor Resilience: Automated Failure Detection and Recovery

## Context and Problem Statement

A change in Codex's security policy caused agents to hit usage limits and exit with non-zero codes. The supervisor treated this as a transient crash and restarted after 5 seconds, creating an infinite claim/release loop that burned 9+ hours in production with no measurable progress.

The existing safety nets were insufficient:
- Exit code 42 (graceful self-abort, ADR-0010) had a restart limit, but generic crashes (non-zero, non-42) did not
- No detection mechanism existed for provider quota-exhaustion patterns
- Supervisors had no way to coordinate when a provider was exhausted — each independently hit the same wall
- Exit 0 (success) could still lead to task re-spinning if immediately reclaimed with no state change
- Human-initiated recovery (ADR-0023) could fix the aftermath but not prevent the loop

## Considered Options

1. **Three-layer in-process detection with provider-scoped coordination** — quota detection via pattern matching, crash-restart tracker with state signatures, spinning tracker for exit-0 loops.

No alternatives were considered. No additional dependencies were needed — the solution uses in-process detection with file-based coordination, consistent with the existing architecture.

## Decision Outcome

Chose **Option 1**: three independent detection layers, each addressing a distinct failure mode with appropriate consequences.

### Architecture

**Design principle:** Different failure causes require different responses. Quota exhaustion is provider-scoped (unregister all agents from that provider). Task-specific spinning is task-scoped (mark the task as blocked). This distinction drives the three-layer design.

**Layer 1 — Provider Quota Detection:**
- Extensible pattern registry: `quotaPatterns` maps providers to known exhaustion signatures
- `DetectQuotaExhaustion()` scans the last 8KB of agent output (tail read for efficiency)
- On detection: writes alert to `alerts.log`, creates signal file `.liza/provider-quota-exhausted-{provider}`
- All supervisors on the same provider check for the signal file at loop top — immediate termination
- `liza resume` clears all quota signal files, allowing restart after quota resets (amended 2026-10-01: expired files only)

**Amended 2026-09-24: the quota signal is time-bounded.** A resume-only clear worked while supervisors were only restarted by a human. Once pool auto-repair respawned agents unattended, a signal that never expired left the provider's roles unstaffed past the provider's announced reset, until an operator intervened (operator defect D67). Detection now records the announced reset: Claude's structured `rate_limit_event.resetsAt` first, then provider reset text. The signal file stores `resets_at` and `expires`. The shared check treats a signal as set only until `expires`: the reset plus one minute, at least five minutes after detection, or detection plus 30 minutes when no reset is announced. After a lift, the first provider turn is the probe. A still-exhausted provider re-raises the signal with its new reset. Resume and file deletion remain the early manual clear. A dedicated provider probe was not added, because a readiness check that consumes no quota is still unsolved.

**Amended 2026-09-25: pool auto-repair restores a missing orchestrator.** Pool demand came only from claimable tasks' role pairs, so an orchestrator that left, whether after a quota termination, a lock timeout, or a clean idle exit, stayed gone while doers and reviewers were re-staffed (operator defect D61). The watcher now treats the orchestrator as required capacity while the goal is IN_PROGRESS and the system is RUNNING. Presence is lease-first, the same `Occupied` predicate registration uses to refuse a second orchestrator. Once the absence outlasts the 60-second grace of the `ORCHESTRATOR MISSING` alert (one shared episode), the existing pool repair spawns one, with its 60-second backoff, its budget of three unregistered starts, and the quota spawn gate above. The grace keeps launchers that start the watcher and the orchestrator together from racing the one-instance limit. Registration remains the cross-process singleton guarantee. Each absence is still announced once, and restarts that register reset the budget, so a registered crash loop is restarted indefinitely, the policy already accepted for other roles; pause or the auto-repair switch stops it.

**Amended 2026-09-28: a quota-terminated doer's work is committed before its claim is released.** The claim release after every exit keeps the task's worktree for continuation, and the next claim blocks a preserved worktree that is dirty. A quota cutoff mid-edit therefore turned a pause into a block that needed a human and a free doer to repair (operator defects D57, D59; 2026-09-27). Before the release, the supervisor now commits the session's tracked and untracked changes on the task branch with `--no-verify`: the project hooks would refuse half-edited files, and the commit attests no quality; later commits, review of the full `base_commit`→`review_commit` range, and integration still gate the content. The commit takes recover-agent's lock order (project lifecycle, agent lifecycle, task claim-worktree), re-checks the caller's generation and that the task is still assigned to it and executing, and refuses a worktree off its task branch or mid-merge, rebase, cherry-pick or revert. A refusal changes nothing and falls back to the dirty block. Provider-unavailable terminations are not covered.

**Amended 2026-10-01: quota detection no longer misses on the quote style or an exit 0.** The breaker above never fired for two observed failures (operator defect D57). Codex prints its usage limit with a typographic apostrophe (`You’ve hit your usage limit`), which the ASCII patterns did not match. On 2026-09-22 no signal was written, the orchestrator re-woke and failed on every wake, and the in-flight task blocked as dirty. Separately, the CLI backend passed an exit-0 session through unchanged, and the supervisor classifies quota only on a failed session, so quota text printed before exit 0 raised nothing. `providerDiagnosticLines` now maps typographic single and double quotes to ASCII on every line it returns. This covers the quota, provider-unavailable and audit detectors, whose patterns stay ASCII. The CLI backend now reports an exit-0 session whose diagnostics match a quota pattern as exit 1, with a completed event flagged `quota`, the rule the ACPX backend already applied. The existing failure path then writes the signal, the alert and the WIP commit, and terminates. Exit 42, the agent's graceful abort, is unchanged. The diagnostic filter still ignores quota text quoted in assistant messages or tool output.

**Amended 2026-10-01: exhaustion knowledge survives resume, and pool repair reports what it cannot start.** On 2026-09-22 pool auto-repair started a Codex coder while Codex was exhausted for a week (operator defect D58). Beyond the missed detection fixed by the amendment above, resume deleted every quota signal, so a detected block would have been erased at the next pause/resume anyway. Resume removes only expired quota signals and prints each one it keeps, so recovering one provider (an account switch) no longer lifts another's block; the early manual clear is deleting that provider's file, which the spawn-blocked alert names. Pool repair sets aside work whose configured start CLI is quota-blocked before it reserves agent IDs or counts headroom, per reviewer list item, and starts nothing for it. The watcher raises one `NO USABLE PROVIDER` alert per blocked role, item, CLI and expiry, reconciled against the whole pool on every tick, and suppresses `MISSING ROLE` for those roles. Fallback to another provider was not added: ADR-0167 keeps quota fallback separate, so the alert names the models.yaml change as the way to staff the role elsewhere. Provider-unavailable signals keep their resume clear.

**Layer 2 — Crash-Restart Tracker:**
- In-memory counter per task tracking consecutive non-zero, non-42 exits
- Maintains a task state signature (JSON snapshot); resets counter when state changes (progress detected)
- Blocks task after configurable threshold (default: 5) without progress
- Config field: `crash_restart_threshold`

**Layer 3 — Spinning Tracker:**
- In-memory counter tracking any re-execution of the same task regardless of exit code
- Same signature-based progress detection as Layer 2
- Blocks task after configurable threshold (default: 10) without progress
- Config field: `spinning_restart_threshold`

**Exit-42 broadening:** Previously only applied to coders; now applies to reviewers and orchestrators. Added `REVIEWING → BLOCKED` transition.

```
Supervisor Loop (per iteration):
  ├─ Check quota signal file → if present, terminate
  ├─ Claim task
  │   └─ Spinning tracker: if same task re-executed N times without progress → block task
  ├─ Execute agent
  ├─ Read output tail (8KB)
  │   ├─ Quota pattern match → WIP-commit owned worktree → release → signal file + alert + terminate
  │   ├─ Exit 42 → existing backoff + limit (now all roles)
  │   └─ Non-zero exit → crash tracker
  │       └─ If N consecutive crashes without progress → block task
  └─ Continue
```

### Rationale

The 9-hour incident demonstrated that supervisors need proactive failure detection, not just reactive recovery. Quota exhaustion is provider-scoped because all agents on the same provider will hit the same wall — signal files provide simple, atomic, race-free cross-supervisor coordination. Task-specific failures (crash loops, spinning) are task-scoped because the problem is localized — blocking the task lets other tasks proceed. In-memory tracking is acceptable because there is no external mechanism restarting supervisors automatically; a supervisor restart naturally resets the counters.

### Consequences

**Positive:**
- Prevents infinite restart loops — the 9-hour incident cannot recur
- Provider-scoped quota coordination stops all agents from hitting the same wall
- Task-scoped blocking isolates failures without affecting unrelated tasks
- State signature progress detection avoids false positives (resets on any meaningful state change)
- Extensible pattern registry — new providers added with one-line entry

**Limitations accepted:**
- In-memory tracking resets on supervisor restart — acceptable given no automatic supervisor restarts exist
- Quota patterns are string matching — new provider error formats require registry updates
- Thresholds (5 crash, 10 spinning) are heuristic — may need tuning based on experience

**Extends:** ADR-0010 (Loop Detection Self-Abort) — adds supervisor-side detection complementing agent-side self-abort. ADR-0023 (Crash Recovery) — adds prevention layer before human-initiated recovery.

---
*Reconstructed from commits e2fd453..b5f9c3d (2026-03-28)*
