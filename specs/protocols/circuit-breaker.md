# Circuit Breaker

## Rationale

Task-level alarms catch local failures. The circuit breaker catches **systemic failures** — patterns that indicate the problem is upstream of task execution.

The circuit breaker is an **observer, not a participant**. It reads signals, detects patterns, and escalates. It never proposes solutions or modifies artifacts.

---

## Identity and Constraints

```yaml
circuit_breaker:
  identity: observer
  permissions:
    read: [.liza/state.yaml (anomalies, planning task review evidence), .liza/log.yaml, sprint.metrics]
    write: [circuit-breaker report, circuit_breaker response/history fields, sprint.status → CHECKPOINT, config.mode → CIRCUIT_BREAKER_TRIPPED on HALT]
    execute: NOTHING
  prohibitions:
    - NEVER propose solutions
    - NEVER modify specs, code, or tasks
    - NEVER call WARNING or CHECKPOINT a trigger
    - NEVER continue execution after a HALT trigger
```

---

## Input: Anomalies and Planning Task Review Evidence

The circuit breaker reads both **anomalies** and **planning task review evidence** from the blackboard. Code Reviewers and Coders populate anomalies:

```yaml
anomalies:
  - timestamp: 2025-01-18T14:32:00Z
    task: task-3
    reporter: code-reviewer-1
    type: retry_loop
    details:
      count: 3
      error_pattern: "serialization failure on nested entity"
      root_cause_hypothesis: "data model doesn't support nesting"

  - timestamp: 2025-01-18T15:10:00Z
    task: task-3
    reporter: coder-1
    type: trade_off
    details:
      what: "flatten entity instead of fixing serializer"
      why: "unblock task within iteration limit"
      debt_created: true

  - timestamp: 2025-01-18T16:45:00Z
    task: task-5
    reporter: coder-2
    type: assumption_violated
    details:
      assumption: "API supports pagination"
      reality: "API returns max 100, no cursor"
      spec_ref: "specs/requirements.md#FR-012"
```

Planning task review evidence is read from planning-task type, status, `review_cycles_total`, and timestamped `rejected` or `review_verdict_rejected` history. This evidence remains durable when a task eventually reaches `MERGED`.

---

## Anomaly Types

For the authoritative list of anomaly types, see [Blackboard Schema — Anomaly Types](../architecture/blackboard-schema.md#anomaly-types).

Summary of types relevant to circuit breaker patterns:

| Type | Logged By | Pattern Relevance |
|------|-----------|-------------------|
| `retry_loop` | Coder, Code Reviewer | retry_cluster |
| `trade_off` | Coder | debt_accumulation |
| `assumption_violated` | Coder, Code Reviewer | assumption_cascade |
| `spec_ambiguity` | Coder | spec_gap_cluster |
| `scope_deviation` | Code Reviewer | workaround_pattern |
| `workaround` | Code Reviewer | workaround_pattern |
| `debt_created` | Code Reviewer | debt_accumulation |
| `external_blocker` | Coder | external_service_outage (aggregated by `blocker_service`) |
| `provider_audit_degraded` | Supervisor | provider_audit_degradation |
| `reviewer_claim_circuit_open` | Supervisor | (supervisor-local claim quarantine, not CB) |
| `hypothesis_exhaustion` | Planner | (triggers rescope, not CB) |
| `review_budget_exhausted` | Planner | (logged for audit; triggers Planner intervention, not CB) |
| `spec_gap` | Planner | spec_gap_cluster |

### Reviewer Claim Quarantine

A reviewer claim that keeps failing the same deterministic way against unchanged
state is quarantined by the supervisor instead of retried every few seconds. The
failure key is `role | task_id | failure_class | boundary_version`, where
`boundary_version` digests the candidate's state-visible `status`,
`review_commit`, `base_commit`, `worktree` and history length. The class is the
branch that removed the candidate, never parsed error text: `review_boundary_repair`,
`acceptance_evidence` and `worktree_context` are quarantined; `git_operation`,
`integration_failed` and anything unclassified take bounded exponential backoff
(5s doubling to a 5min cap) and are never quarantined; authority loss or agent
degradation stops the supervisor outright.

Three consecutive identical failures open a key. While it is open the candidate
is excluded from the reviewer's work check for a 5min
cooldown that doubles per failed re-probe up to 30min, and every other reviewable
task stays claimable. A key clears when its `boundary_version` changes — the
documented repair writes `review_commit` and `base_commit`, and any lifecycle
event moves `status` or history — or when that task's claim succeeds. Claim
selection still evaluates and removes broken candidates when other work wakes
the reviewer. Their classified failures reach the breaker even when a later
candidate is claimed successfully; that success clears only the claimed task's
keys and the role's transient backoff. Failures during cooldown count in memory
but neither extend the cooldown nor write another anomaly.
When only quarantined work remains, cooldown expiry permits one re-probe without
clearing the key. It re-runs the real boundary
validation and either clears the key or re-quarantines it with a doubled
cooldown. The out-of-state inputs that validation also reads (worktree HEAD, the
integration merge base) are deliberately outside the key, so a repair that moves
only those is recovered by that time-bounded re-probe. Thresholds and cooldowns
are constants of the supervisor loop, not configuration.

`reviewer_claim_circuit_open` records one anomaly per key, however many
supervisors or restarts observe the condition. `role`, `failure_class`,
`boundary_version`, `first_failure`, `recovery` and the bounded masked `error`
are written once; `attempts`, `last_failure` and `cooldown_until` are updated in
place on that same record, so retry count and duration stay observable without an
entry per attempt. The counters live in the supervisor process: a restart
re-attempts up to the threshold before re-quarantining, and adds no anomaly.
Quarantine is supervisor-local — it bounds one claim loop and does not by itself
feed the pattern detection or responses below.

---

## Pattern Detection Rules

```yaml
circuit_breaker_rules:
  window: current_sprint
  patterns:
    - name: retry_cluster
      description: "Same error type recurring across tasks"
      condition: count(type=retry_loop, similar(error_pattern)) >= 3
      severity: ARCHITECTURE_FLAW

    - name: debt_accumulation
      description: "Multiple trade-offs creating debt"
      condition: count(type=trade_off, debt_created=true) >= 3
      severity: SCOPE_FLAW

    - name: assumption_cascade
      description: "Same assumption failing across tasks"
      condition: count(type=assumption_violated, same(assumption)) >= 2
      severity: SPEC_FLAW

    - name: workaround_pattern
      description: "Multiple workarounds for similar issues"
      condition: count(type IN [workaround, trade_off], similar(root_cause)) >= 2
      severity: ARCHITECTURE_FLAW

    - name: spec_gap_cluster
      description: "Multiple tasks hitting spec ambiguity"
      condition: count(type=spec_ambiguity, same(spec_ref)) >= 2
      severity: SPEC_FLAW

    - name: external_service_outage
      description: "Same external service blocking multiple tasks"
      condition: count(type=external_blocker, same(blocker_service)) >= 2
      severity: EXTERNAL_DEPENDENCY
      action: TRIP_MODE  # Set config.mode to CIRCUIT_BREAKER_TRIPPED; human may checkpoint sprint separately

    - name: provider_audit_degradation
      description: "Provider transcript or rollout persistence degraded across agent work"
      condition: count(type=provider_audit_degraded, same(provider), distinct(agent_id) >= 2) OR count(type=provider_audit_degraded, same(provider)) >= 3
      severity: OBSERVABILITY_DEGRADED
      action: PROPORTIONAL_RESPONSE  # WARNING, CHECKPOINT, or HALT from evidence lifecycle and exact current-health proof

    - name: planning_review_churn
      description: "Four or more durable rejection cycles on a planning task; `MERGED` tasks remain eligible"
      condition: task.type=planning AND durable_rejection_count >= 4
      severity: PLANNING_CONVERGENCE_DEGRADED
      action: TRIP_MODE
```

For `planning_review_churn`, a positive `review_cycles_total` is the authoritative durable count; when `review_cycles_total` is zero, count timestamped `rejected` and `review_verdict_rejected` task-history events instead. A count of four or more cycles triggers regardless of current task status; `MERGED` tasks remain eligible.

### Pattern Matching Functions

The pattern conditions use pseudo-functions for matching:

| Function | v1 Implementation | v2 (Future) |
|----------|-------------------|-------------|
| `similar(field)` | **Exact match only** — `§BRAND_BINARY_NAME§ analyze` uses `group_by(.)` | String similarity threshold (Levenshtein ≥ 0.7) |
| `same(field)` | **Exact match** — string equality comparison | Exact match after normalization |
| `count(...)` | Go implementation counts matching entries | Same |

**v1 Limitations:**
- `§BRAND_BINARY_NAME§ analyze` uses **exact string matching** for pattern detection
- Anomalies with `error_pattern: "timeout"` and `error_pattern: "connection timeout"` are counted separately
- Human must review script output and apply judgment for similar-but-not-identical patterns
- Thresholds may need adjustment: exact matching misses related errors, so lower counts may indicate real patterns

**v1 Workflow:**
1. Run `§BRAND_BINARY_NAME§ analyze` — outputs exact-match counts per pattern
2. Human reviews output, anomalies, and qualifying planning-task rejection evidence
3. Human applies judgment: "these 2 exact + 1 similar = pattern"
4. Human reviews the selected response; only `HALT` is a circuit-breaker trigger

**v2 Implementation:** Requires defining similarity thresholds and normalization rules. Defer until v1 proves which patterns are valuable.

### Acknowledgement Watermark

Circuit-breaker detection evaluates durable anomalies and planning task review evidence.
Generic patterns keep the backward-compatible cleared-trigger
watermark: when `status == OK` and `current_trigger == null`, the latest history
entry with `result: TRIGGERED` suppresses generic anomalies at or before its
timestamp. If either hard-trigger field is active, no generic watermark applies.

Provider-audit evaluation instead uses the latest resolved actionable response
(`CHECKPOINT` or `HALT`) as its evidence boundary. The `resume` command resolves the
matching history entry, sets `resolved_at` and `resolution`, and clears
`current_response`; it also clears `current_trigger`/`TRIGGERED` state for a
`HALT`. The response timestamp remains durable, so unchanged evidence cannot
immediately checkpoint or halt again. Qualifying provider evidence is classified:

| Classification | Evidence relative to the resolved boundary | Default response |
|----------------|---------------------------------------------|------------------|
| `ACKNOWLEDGED_HISTORICAL` | Qualifies entirely at or before the boundary, with nothing later | `WARNING` |
| `NEW` | No same-provider acknowledged evidence exists and later evidence independently qualifies | `CHECKPOINT` |
| `CONTINUING` | Same-provider evidence exists on both sides and the combined group qualifies, including new evidence completing a historical sub-threshold group | `CHECKPOINT` |

`WARNING` is observation-only: it writes the report/history result but leaves
mode, sprint, trigger, and active-response state unchanged. `NEW` and
`CONTINUING` are promoted from `CHECKPOINT` to `HALT` only by the exact
current-execution proof below.

For `planning_review_churn`, the task's durable rejection count remains the
threshold evidence, but retrigger eligibility requires a timestamped rejection
strictly after the watermark (the generic watermark above). A counter at or
above four with no later rejection remains suppressed; `MERGED` tasks remain
eligible when later evidence arrives.

### Provider-Audit HALT Proof

`NEW` or `CONTINUING` provider-audit evidence may select `HALT` only when the
state committed by analysis proves all of the following:

1. The qualifying anomaly group has one non-empty provider key.
2. At least one currently registered agent has `Agent.Provider` exactly equal
   to that key.
3. Every exact-matching registration has a degraded `agent_health` entry for
   the same agent ID, provider, PID, and registration time.
4. No exact-matching registration lacks that current epoch-matched degraded
   record.

An alias-only match (for example `codex` evidence with a `codex-acp`
registration), empty or absent identity, missing health, or a mismatched/stale
PID or registration-time epoch is non-halting unknown. Analysis returns
`CHECKPOINT` and explains the failed join; it does not guess canonical identity.

---

## Severity Classification

| Severity | Layer Affected | Remediation Scope |
|----------|---------------|-------------------|
| `VISION_FLAW` | Why we're building | Stop, revisit goal and brief |
| `SCOPE_FLAW` | What we're building (MVP) | Pause, revise PRD/requirements |
| `SPEC_FLAW` | Requirements detail | Pause, update specs |
| `ARCHITECTURE_FLAW` | How we're building | Pause, new ADR, possible refactor |
| `TECH_STACK_FLAW` | Tools/frameworks | Spike needed, possible tech pivot |
| `EXTERNAL_DEPENDENCY` | External services | Halt sprint — external issue, not agent problem; wait or escalate |
| `OBSERVABILITY_DEGRADED` | Provider transcript/rollout auditability | Warning or checkpoint by default; halt only with exact current execution-compromise proof |
| `PLANNING_CONVERGENCE_DEGRADED` | code-planning convergence | pause downstream fan-out and inspect rejection evidence before choosing remediation |

**Note:** `TECH_STACK_FLAW` is reserved for future patterns (e.g., library version conflicts). No current pattern triggers it.

---

## Circuit Breaker Activation

1. **MATCH** — Qualify the pattern and classify provider evidence when applicable.
2. **SELECT** — Choose `WARNING`, `CHECKPOINT`, or `HALT` from the state observed
   inside the committing blackboard mutation.
3. **REPORT** — Write the project circuit-breaker report with response,
   classification, explanation, and evidence.
4. **APPLY**:
   - `WARNING`: observation only; no mode, sprint, trigger, or active-response mutation.
   - `CHECKPOINT`: persist a non-trigger `current_response` as a hard checkpoint,
     keep mode `RUNNING`, keep status `OK` and `current_trigger: null`, and set
     sprint status to `CHECKPOINT`. Downstream transition creation pauses;
     already-available doer/reviewer work may continue.
   - `HALT`: create the hard trigger/response, set status `TRIGGERED` and mode
     `CIRCUIT_BREAKER_TRIPPED`; execution never continues after this trigger.
5. **ACKNOWLEDGE** — After review or remediation, run the `resume` command to
   resolve the active `CHECKPOINT` or `HALT` response and preserve its evidence
   boundary. The `stop` command remains the abort action.

Only `HALT` is a circuit-breaker **trigger**. A matched pattern, `WARNING`, or
`CHECKPOINT` must not be described as triggering the circuit breaker.

---

## Circuit Breaker Report Format

The operation result exposes `AnalyzeResult.Response`,
`AnalyzeResult.Classification`, and `AnalyzeResult.Explanation`; JSON projects
the same typed decision as `response`, `classification`, and `explanation`.
`triggered` is true only for `HALT`; provider responses use the three evidence
classifications above.

````markdown
# Circuit Breaker Report

**Analyzed:** 2025-01-18T17:30:00Z
**Pattern:** provider_audit_degradation
**Severity:** OBSERVABILITY_DEGRADED
**Evidence Class:** `CONTINUING`
**Response:** `CHECKPOINT`
**Explanation:** qualifying provider evidence spans the last acknowledged boundary, but current registered-provider health cannot be proved for the same process epoch

## Evidence
| Agent | Provider | Anomaly | Timestamp |
|-------|----------|---------|-----------|
| coder-1 | codex | provider_audit_degraded | 2025-01-18T17:29:00Z |
| coder-2 | codex | provider_audit_degraded | 2025-01-18T17:29:30Z |

## Response Action

`CHECKPOINT` is a non-trigger hard checkpoint: pause downstream transition
creation, preserve already-available doer/reviewer work, and leave the recovery
decision to the operator. After review or remediation, run
`§BRAND_BINARY_NAME§ resume`.

- `WARNING` is observation-only and does not create an active response.
- `CHECKPOINT` is a non-trigger hard checkpoint with an active response.
- `HALT` is the only circuit-breaker trigger and stops execution until resume.

## Anomalies (trimmed)
1. `provider_audit_degraded` at `2025-01-18T17:29:00Z`
   - task: `task-7`
   - reporter: `coder-1`
   - message_excerpt: `provider transcript persistence failed`

## Anomalies (raw)
```yaml
anomalies:
  - timestamp: 2025-01-18T17:29:00Z
    reporter: coder-1
    type: provider_audit_degraded
    details:
      provider: codex
      agent_id: coder-1
      message: provider transcript persistence failed
  - timestamp: 2025-01-18T17:29:30Z
    reporter: coder-2
    type: provider_audit_degraded
    details:
      provider: codex
      agent_id: coder-2
      message: provider rollout persistence failed
```

## Human Decision Required
- [ ] Acknowledge report
- [ ] Confirm severity assessment
- [ ] Choose and record remediation
- [ ] Release the active response with `§BRAND_BINARY_NAME§ resume`
````

---

## Implementation: v1 vs v2

**v1: Human-triggered analysis**
- Human runs `§BRAND_BINARY_NAME§ analyze` on demand (commonly during checkpoint review)
- Script parses anomalies, applies rules, generates report
- No background daemon

**v2: Continuous monitoring**
- `§BRAND_BINARY_NAME§ tui` extended with pattern detection
- Every match receives the same proportional response. Only `HALT` trips mode;
  `CHECKPOINT` is a non-trigger checkpoint, and `WARNING` is observation-only.

**Recommendation:** Start with v1. Promote to v2 if manual analysis becomes bottleneck.

---

## Blackboard Circuit Breaker Section

```yaml
circuit_breaker:
  last_check: 2025-01-18T17:30:00Z
  status: OK  # OK, TRIGGERED; TRIGGERED is HALT-only
  current_trigger: null  # HALT-only, retained for backward compatibility
  current_response:
    timestamp: 2025-01-18T17:30:00Z
    pattern: provider_audit_degradation
    severity: OBSERVABILITY_DEGRADED
    response: CHECKPOINT
    classification: CONTINUING
    explanation: current provider health is unknown because the registered alias does not exactly match the anomaly provider
    report_file: .liza/circuit_breaker_report.md
  history:
    - timestamp: 2025-01-17T12:00:00Z
      pattern: null
      result: OK
    - timestamp: 2025-01-18T17:30:00Z
      pattern: provider_audit_degradation
      severity: OBSERVABILITY_DEGRADED
      result: CHECKPOINT
      response: CHECKPOINT
      classification: CONTINUING
      explanation: current provider health is unknown because the registered alias does not exactly match the anomaly provider
      resolution: "resumed by human"                 # Added by resume acknowledgement
      resolved_at: 2025-01-18T19:00:00Z
```

### History Entry Fields

| Field | Set By | When | Notes |
|-------|--------|------|-------|
| `timestamp` | Script | On check | ISO 8601 timestamp of check |
| `pattern` | Script | On check | Pattern name (null if OK) |
| `severity` | Script | On a qualifying response | Severity classification |
| `result` | Script | On check | `OK`, `WARNING`, `CHECKPOINT`, or backward-compatible `TRIGGERED` for `HALT` |
| `response` | Script | On a qualifying response | Typed `WARNING`, `CHECKPOINT`, or `HALT`; optional for legacy entries |
| `classification` | Script | On provider-audit response | `ACKNOWLEDGED_HISTORICAL`, `NEW`, or `CONTINUING`; optional for legacy/non-provider entries |
| `explanation` | Script | On a qualifying response | Why the evidence supports the selected action; optional for legacy entries |
| `superseded_by_response` | Analyze | On provider checkpoint escalation | Optional `HALT`-only replacement marker; not an acknowledgement or provider watermark |
| `resolution` | Resume/human | On acknowledgement | Free text describing acknowledgement/corrective action |
| `resolved_at` | Resume/human | On acknowledgement | Timestamp that makes the response an evidence boundary |

`current_response` uses the same timestamp, pattern, severity, response,
classification, explanation, and report-file fields. It exists only for active
`CHECKPOINT` and `HALT` responses. `HALT` additionally populates the legacy
`current_trigger` and `status: TRIGGERED` fields; `CHECKPOINT` does not.
Readers must accept older state without `current_response`, `response`,
`classification`, or `explanation`. Existing `result: TRIGGERED` history remains
a valid cleared-trigger acknowledgement boundary.

### Resolution Workflow

For a `CHECKPOINT`, the `analyze` command records `current_response`, leaves mode
`RUNNING`, keeps status `OK`/`current_trigger: null`, and checkpoints the sprint.
For a `HALT`, it also records `current_trigger`, sets status `TRIGGERED`, and
sets mode `CIRCUIT_BREAKER_TRIPPED`. A `WARNING` has no active response to clear.

Analysis recomputes the candidate inside the committing mutation before
reconciling an active response. Candidate detection and existing pattern priority
remain authoritative.

| Active response | Committed candidate | Result |
|-----------------|---------------------|--------|
| `HALT` of any pattern | No match or any response | Retain the active `HALT` unchanged |
| Provider `CHECKPOINT` | No match or any non-`HALT` candidate | Retain the active `CHECKPOINT` unchanged |
| Provider `CHECKPOINT` | `HALT` | Atomically supersede the checkpoint and create the active `HALT` |

An active `HALT` of any pattern is latched across re-analysis until resume and
wins over no match and every matched candidate. An active provider-audit `HALT`
is latched by this same rule. An active provider checkpoint therefore wins over
no match or any non-`HALT` candidate, but escalates when the committed candidate
is `HALT`. The exact current degraded-epoch proof remains eligible to produce
that provider `HALT`, and a generic `HALT` candidate selected by existing pattern
priority remains eligible at checkpoint; neither is suppressed before
reconciliation.

Escalation gives the former checkpoint exactly
`superseded_by_response: HALT`, with its `resolution` and `resolved_at` absent,
and the same atomic transition creates exactly one matching unresolved `HALT`
history boundary for the new active response. The supersession marker is not an
acknowledgement and does not advance the provider-evidence watermark. Resume
later resolves only the active `HALT`; the superseded checkpoint remains
unresolved.

When an active response wins, repeated analysis projects that response's
pattern, severity, typed response, classification, explanation, and report path
through `AnalyzeResult` and report output rather than returning `OK`. After
review or remediation, `§BRAND_BINARY_NAME§ resume` is the sole release and
acknowledgement action:
it resolves the history entry matching the active response's timestamp, pattern,
and response; records `resolution` and `resolved_at`; clears `current_response`;
and, for `HALT`, clears the trigger and returns mode/status to normal. The
resolved response timestamp becomes the provider-evidence boundary, so unchanged
qualifying evidence subsequently reports `ACKNOWLEDGED_HISTORICAL`/`WARNING`
instead of checkpointing or halting again.

Legacy `TRIGGERED` history remains the generic acknowledgement watermark.
Later `OK` entries do not move that watermark. A clean analysis does not release
a sprint checkpoint or latched halt.

## Related Documents

- [Sprint Governance](sprint-governance.md) — checkpoints, retrospectives
- [Roles](../architecture/roles.md) — logging duties
- [Blackboard Schema](../architecture/blackboard-schema.md) — anomalies section
