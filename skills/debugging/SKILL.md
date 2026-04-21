---
name: debugging
description: "Systematic bug diagnosis through hypothesis-driven narrowing. Triages with quick-win checks, then escalates through fast-path or full analysis with structured qualification, root-cause gating, and falsification. Use when the user reports a bug, error, exception, or unexpected behavior, or asks to troubleshoot, debug, or fix failing code."
---

Debugging optimizes for certainty, not speed. Always start by ruling out trivial causes, then escalate to Fast Path or Normal Path.

# Quick Wins (30 seconds max)

First check the no brainers:
- Typo / wrong variable name
- Missing import / uninstalled dependency
- Stale cache / needs rebuild
- Wrong branch / uncommitted changes
- Environment variable missing / misconfigured
- Runtime / compiler version mismatch
- Unexpected input shape / encoding / locale

If any of these is the cause → fix directly, no protocol needed.

# Fast Path Debugging

Note: Regular tasks have their own FAST PATH (Rule 4).

**Eligible when ALL true:**
- Bug conditions are obvious and need no report intake.
- Stacktrace/error points directly to fault location (no inference needed)
- Single hypothesis survives first inspection (no competing explanations)
- Fix is self-evident from the bug (seeing the problem IS seeing the solution)
- Localized (single file, single function)
- Deterministic (not flaky)

**Constraint:** No refactor, no cleanup, no opportunistic improvements. Fix the bug and stop.

**Format:**
```
Mode: Debug (Fast)
Bug: [one-line: location + cause]
Fix: [one-line change]
Verify: [test/command] → [expected outcome]
Proceed (P)?
```
The above verification should be successful for the bug to be considered fixed.

**Escalation:** If fix fails on first attempt → undo the fix, then escalate to normal path. No second tries in fast mode.
If new information contradicts the initial hypothesis → abandon Fast Path immediately. Don't bend the hypothesis to fit.

# Normal Path Debugging

## Bug Report Intake

Before qualification, ground the report:
- Where? (local / CI / staging / prod)
- What branch/version?
- Since when? (always / recent / after X)
- Who's affected? (just me / others / unknown)
- What was already tried?

If critical context is missing, ask before proceeding.

## Mode Selection

When debugging detected, ask: `"Debugging in [collaboration mode] because [reason]. Override?"`

**Defaults:** Syntax/type → Autonomous. Logic → User Duck. Architectural → Pairing.

**Escalation:** 2 failed Autonomous → User Duck. 3 stalled iterations → Stop & Document (see Structured Failure Report).

## Bug Qualification

**Convergence Goal:** A reproducible trigger + a traced failure path. Iterate until both are stable.

**Failure Path:** Trace trigger → state → symptom with concrete identifiers (values, line numbers, states).
- Distinguish: Observed (logs/debugger) vs Inferred (code reading)
- Multiple errors: identify primary vs cascade
- Calibration: ❌ "The validation fails" ✓ "score=None at line 47 → ValueError; set by parse_input(line 32) receiving empty string"

**Reproducibility:** Capture in failing test (ideal — this becomes the regression test) or documented manual steps (acceptable).
Reduce to minimal case before analysis — fewer lines reveal more.
If sporadic: narrow conditions or instrument. Do not propose fixes for untriggerable bugs.

**Counterfactual Check:** What similar inputs/states do NOT trigger the bug?
- Identify the boundary: "fails with empty string but not whitespace" reveals more than "fails with empty string"
- Vary one dimension at a time to isolate the trigger
- Document both positive (triggers) and negative (doesn't trigger) cases

**Temporal Bug Patterns:** If timing-related, identify the category:
- Race condition: multiple actors, shared state, non-deterministic outcome
- Async ordering: operations complete in unexpected sequence
- Cache staleness: read returns outdated value
- Eventual consistency: state not yet propagated

Instrumentation for temporal bugs:
- Add timestamps/sequence IDs to logs
- Inject artificial delays to widen race windows
- Force cache misses to isolate caching effects
- Check replica lag / propagation status

These bugs may resist minimal reproduction — document the timing conditions even if not fully deterministic.

**Qualification Summary:**
```
Bug Profile: [regression|novel] [deterministic|flaky] [reproducible|sporadic] [tested|untested]
  Optional: [technical|logical] [observable|opaque] [localized|distributed] [stateless|stateful]
If regression: consider git bisect (known-good SHA, fast test command; moves HEAD — approval required).
Signal: [symptom] vs [expected] — Observability: [stacktrace|logs|metrics|user report|silent]
Impact: [data loss | security exposure | user-visible | silent corruption | degraded performance | cosmetic]
Reproduction: [steps] — Feedback loop: [fast <10s | slow | manual]
Failure Path: [traced path, observed vs inferred]
Missing Evidence: [none | logs: ... | access: ... | repro: ... | spec: ... | metrics: ... | permissions: ...]
User context needed? [N / Y: specific question]
Proceed to analysis? (Y/N)
```

## Pattern Analysis

Before narrowing, establish a reference point:
1. Find working analogues in the codebase (similar functionality that works)
2. Study relevant portions — focus on the code path that differs from your bug
3. Catalog distinctions between working and broken versions
4. Note dependencies and environmental differences

If no analogue exists, note explicitly — you're debugging novel code without a known-good reference.

Skip this phase when: the bug is in isolated logic with no comparable pattern, or the failure path already points to a clear contract violation.

## Analysis (Divide and Conquer)

Debugging is a narrowing search, not guess-and-check.

**Observability check:** Before narrowing, ensure you can observe the relevant state transitions. If not, add instrumentation (logging, tracing, metrics) before forming hypotheses. Debugging blind wastes cycles.

Loop until atomic:
1. State the problem space — check for biases:
   - Anchoring: Am I overweighting my first hypothesis?
   - Stacktrace trust: This shows where it failed, not necessarily where the bug is
   - "It worked before": Maybe the bug was always there, just not triggered
   - Correlation ≠ cause: Are these events actually related, or just coincident?
2. List 2-3 hypotheses if possible. Pick one with rationale, favoring bisection over single-point tests
3. Implement verification (typically a test) — ask: "What evidence would falsify this hypothesis fastest?" Add instrumentation if needed to observe the outcome.
4. Narrow to surviving subspace. If ambiguous, try different bisection point.
   - **Exit condition:** If narrowing reveals expected behavior → False Positive path: document why, assess doc/UX need, close as "Not a Bug"

**Analysis Summary:** Before Root Cause Identification, summarize the narrowing chain.

## Root Cause Gate

**Where → Why:** Pull the string until you reach an actionable flaw — something that should have been different and can be corrected.

Before proposing fix: what evidence would prove this root cause wrong?

```
Root Cause Gate:
Category: [wrong assumption | missing guard | violated contract | design gap | external dependency | timing/race | data corruption | observability gap | other: ...]
Root Cause: [one-line actionable flaw]
Chain: [root cause] → [symptoms] → [impacts]
Falsifiable by: [evidence that would prove this wrong]
Confidence: [high: would bet on it | medium: best hypothesis | low: educated guess]
Proceed to Resolution (P)?
```

## Resolution

1. **Verify:** Re-run repro to confirm bug still exists
2. **Fix:** Minimum scope, root cause not symptom. Compact Approval request.
   - If bug traced to external dependency: present options (workaround / upstream report / version change) before proceeding.
3. **Protect:** Repro test → regression suite (TDD workflow):
  - If manual repro only: write failing test now
  - Verify test fails for the right reason
  - Implement fix → verify test passes
  - Run related tests for regressions
  - Search codebase for similar patterns
  - Post-fix validation: check adjacent code paths, inverse conditions, and previously working edge cases
4. **Confidence Gate:** Explain why this fix (not something else) addresses the root cause stated in the Gate.
  - If explanation doesn't hold → Low confidence regardless of test results.
  - High → proceed to close
  - Medium → require additional validation (extended soak, peer review, or monitoring period)
  - Low → do not close; document as "mitigated, not resolved", record in `TECH_DEBT.md`, schedule follow-up
5. **Close:** Summary with lessons learned

**Post-Resolution Reflection:**
- Why did this bug escape earlier detection? (missing test, untested edge case, insufficient logging, unclear contract)
- What signal could have caught it sooner? (test, assertion, metric, log pattern)
- Is this a one-off or a pattern worth addressing systemically?

**Stop-on-Repeat:** Same fix twice → explain why it will work this time.

**Struggle Report (3 stalled attempts):**
```
Attempted: [approach] — failed because [reason]
Dead Ends: [do not revisit]
Evidence Gathered: [what we now know that we didn't before]
Remaining Hypotheses: [untested]
Blockers: [what prevents progress]
Next Steps: [escalation or alternative]
```
