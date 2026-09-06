# 129 - Proportional Circuit-Breaker Responses with Explicit Acknowledgement

## Context and Problem Statement

Provider audit degradation is evidence that transcript or rollout persistence
failed; it does not by itself prove that current execution must stop. Historical
provider evidence could repeatedly halt a run after an operator had addressed it.
Conversely, re-analysis or mode changes could weaken an active safety response
before anyone acknowledged it. The system needed both proportional intervention
and durable ownership of the decision to resume.

## User-Confirmed Intent

On 2026-09-06, the user supplied this historical intent: Repeated halts from old evidence drove the circuit-breaker redesign.

The separate historical rationale for choosing checkpoint on uncertain current health was not supplied.

## Considered Options

The following comparisons are reconstructed from the implemented policy, not a
record of historically rejected alternatives:

1. Halt on every qualifying provider-audit pattern. Simple, but treats already
   acknowledged evidence and uncertain current health as execution failure.
2. Clear responses whenever a later analysis finds no matching pattern. Avoids
   stale interruptions, but lets observation replace operator acknowledgement.
3. Classify evidence, select a proportional response, and retain actionable
   responses until explicit acknowledgement. This is the implemented choice.

## Decision Outcome

Represent responses as `WARNING`, `CHECKPOINT`, or `HALT`. Only `HALT` is a
circuit-breaker trigger. A warning records observations without changing mode,
sprint, or active-response state. A checkpoint pauses downstream transition
creation while preserving already-available doer and reviewer work.

Provider evidence is evaluated against the latest resolved actionable response:

| Evidence | Default response |
|----------|------------------|
| Entirely acknowledged historical evidence | `WARNING` |
| Newly qualifying evidence | `CHECKPOINT` |
| Continuing evidence across the acknowledgement boundary | `CHECKPOINT` |

New or continuing evidence becomes `HALT` only with exact current-health proof:
the provider key is non-empty, at least one registration matches it exactly,
and every matching registration has degraded health for the same agent ID,
provider, PID, and registration time. Missing health, aliases, and stale epochs
remain non-halting uncertainty rather than inferred identity.

Analysis recomputes its candidate inside the committing mutation. An active
`HALT` survives every subsequent candidate, including no match. An active
provider checkpoint survives non-halting candidates but can atomically escalate
to `HALT`. Escalation marks the old response as superseded, not acknowledged.

`resume` is the sole release and acknowledgement action. It resolves the matching
history entry and clears the active response and, for a halt, the legacy trigger
fields. The resolved response timestamp bounds subsequent provider evidence;
unchanged evidence cannot immediately interrupt the run again. Generic patterns
retain their existing cleared-trigger watermark for compatibility.

### Rationale

Separating evidence age, current execution health, and operator acknowledgement
prevents both disproportionate retriggering and silent release. Persisted active
responses make repeated analysis report the intervention still in force.

### Consequences

- Audit degradation receives an intervention justified by current evidence.
- Clean re-analysis cannot erase an unacknowledged halt or checkpoint.
- Response classification and history add persisted state and compatibility
  handling alongside the legacy `TRIGGERED` representation.
- Incomplete identity evidence requires operator review at checkpoint; automatic
  recovery is deliberately limited by the available proof.

## Related Decisions

Extends the supervision and auditability concerns of
[ADR-0053](0053-supervisor-resilience-automated-failure-detection.md) and
[ADR-0085](0085-llm-agent-boundary-and-acp-observability.md).
The [circuit-breaker protocol](../../protocols/circuit-breaker.md) specifies the
current response lifecycle.

---
*Reconstructed from commit `0acece1b` (2026-08-27) and the circuit-breaker protocol.
ADR selection and the repeated-halt trigger were confirmed by the user on
2026-09-06; alternatives remain reconstructed.*
