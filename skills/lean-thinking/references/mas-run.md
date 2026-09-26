# Multi-Agent Run Analysis

Evidence map for a run under `§BRAND_PROJECT_DIRNAME§/`. Read-only. Query large files (`state.yaml`, `alerts.log`, `agent-outputs/`) with `jq`/`yq`/scripts — never read them whole.

## Evidence Sources

| Source | Gives | Caveat |
|--------|-------|--------|
| `state.yaml` `tasks[].history[]` (`event`, `agent`, `time`, `reason`) | Per-task lifecycle: blocks, repairs, reviews, merges | Agent status is a snapshot, not a history |
| `state.yaml` `anomalies[]` | Typed anomalies with timestamps | |
| `agent-sessions.csv` | Every session: start, role, provider, duration | No task id |
| `usage/records-*.jsonl` | Tokens per session, task, role | Check for non-zero values first: zeros with `provenance: unknown` mean missing telemetry, not zero cost |
| `lifecycle-metrics/*.json` `counts.<op>.<outcome>` | Operation outcomes (e.g. `claim-task` `INVALID_INPUT` share) | Covers `observed_since` onward only |
| `alerts.log` | Alert stream | Count distinct messages, not lines |
| `log.yaml` | System events (auto-repair spawns, checkpoints) | |
| `operator-notes.md` | Incidents, interventions, operator-measured costs, fixes | Check cited fix commits against the engine's git history |
| `agent-prompts/`, `agent-outputs/` | Prompt size by role and time; tool use per session | Hint text echoed in a transcript is not an invocation |
| Earlier reports (`context-engineering.md`, `log-analysis.md`, `circuit_breaker_report.md`, …) | Prior findings | Reuse, don't re-derive |

Tooling: the `§BRAND_BINARY_NAME§-logs` skill scripts — `analyze-state.py` (rejection and supersede churn), `analyze-log.py` (per-role session friction).

## Repair Actions

Every action that puts the task graph back on track is friction, even when the block that triggered it was protective. Count them per run and per merged task.

| History event | Repair it records |
|---------------|-------------------|
| `blocked` → `unblocked` | Work stopped and restarted (cause in `blocked.reason`) |
| `superseded`, `replacement_committed` | Task replaced |
| `dependencies_rewritten`, `dependency_repair_applied` | Graph rewired |
| `claim_released`, `task_recovered_fresh`, `reclaimed_after_rejection`, `reassigned_after_rejection` | Ownership reset or rework |
| `integration_failed` | Merge-time repair |
| `orchestrator_assessment` | Orchestrator turn spent assessing (unchanged repeats are Over-processing) |

Also count tasks created as corrections or complements (plan amendments, corrective plans, revisions). Identify them from task descriptions; ID suffixes are a heuristic — mark estimated.

**Metrics:**
- Repair actions per merged task, and per supersede (fan-out)
- Blocked task-hours (block → unblock, supersede, or claim), pauses subtracted
- Share of tasks blocked at least once; share created as corrections; corrections of corrections
- Block causes: classify `blocked.reason` into environment or runtime input, upstream plan or contract gap, engine mechanics, human decision — by reading a sample; keyword clustering misfires on the product's domain vocabulary
- Detection distance: the role that blocked vs the stage where the cause was introduced
- Who repaired: the `agent` field — operator- or human-directed actions may be recorded under the orchestrator; say so

## Pitfalls

- **Pauses:** subtract spans with no sessions from lead and wait times, and ask the owner why. A pause to improve the engine is enabling work, not waiting.
- **Lock files:** `state.yaml*.lock` are flock files — presence does not mean held. The owner JSON records the last acquisition.
- **`claim_released` after the task's own lifecycle ended** (following `merged` or `transition_executed`) is agent-shutdown cleanup, not repair. Classify each event by the one before it.
- **Snapshots vs history:** agent status and sprint summaries are points in time; use task histories and session logs for trends.
