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
- `liza resume` clears all quota signal files, allowing restart after quota resets

**Amended 2026-09-24: the quota signal is time-bounded.** A resume-only clear worked while supervisors were only restarted by a human. Once pool auto-repair respawned agents unattended, a signal that never expired left the provider's roles unstaffed past the provider's announced reset, until an operator intervened (operator defect D67). Detection now records the announced reset: Claude's structured `rate_limit_event.resetsAt` first, then provider reset text. The signal file stores `resets_at` and `expires`. The shared check treats a signal as set only until `expires`: the reset plus one minute, at least five minutes after detection, or detection plus 30 minutes when no reset is announced. After a lift, the first provider turn is the probe. A still-exhausted provider re-raises the signal with its new reset. Resume and file deletion remain the early manual clear. A dedicated provider probe was not added, because a readiness check that consumes no quota is still unsolved.

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
  │   ├─ Quota pattern match → signal file + alert + terminate
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
