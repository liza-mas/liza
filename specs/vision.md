# Liza Vision

## What

An agent-supervised multi-agent coding system that makes AI agents accountable engineering collaborators, not merely autonomous but unreliable assistants.

Liza combines four ideas:
- **Behavioral contracts** for per-agent discipline—Tier 0 invariants are never violated
- **Specification system** for durable context—agents are stateless, specs persist understanding across restarts
- **Blackboard coordination** for visible state—all coordination happens through a shared file humans can observe
- **Externally validated completion** with Ralph-like loops—Coders cannot self-certify; Code Reviewers issue binding verdicts

## Why

Single-agent coding works until it doesn't. The agent marks their task complete when it isn't. It "fixes" things you didn't ask for. It claims tests pass when they don't (or have been silently greenwashed). At best human review catches these failures—but human review doesn't scale.

Multi-agent systems promise coordination, but most inherit the same failure modes and add new ones: agents approve each other's mistakes, drift collectively from the goal, or converge confidently on broken solutions.

> Systems that optimize for immediate output generate muda—defects, rework, and correction loops. By optimizing for trust, quality, and auditability, Liza eliminates these wasted cycles—and should reach completion sooner, not later.
>
> The behavioral contract (developed over 6 months of pairing) proved it: Quality is the fastest path to real completion.

This contract made approval gates boring—violations disappeared, requests got fulfilled as expected. Yet these gates are load-bearing and cannot be removed.

Liza is what comes next: delegating approval to acting-reviewing agent pair who operate under the same contract,
thus enabling production-quality vibe coding.

## For Whom

**Primary users:**
- Solo developers or small teams with Claude Code experience
- Users comfortable with terminal-based workflows
- Projects where quality, auditability and overall speed matter

**Use cases:**
- Medium-complexity features requiring multiple coordinated changes
- Refactoring tasks where consistency matters
- Projects where human bandwidth is the bottleneck, not agent capability

**Not for (v1):**
- Teams without existing Claude Code familiarity
- Real-time collaborative editing scenarios
- Projects requiring IDE integration
- Domains where requirements emerge through implementation (Liza v1 assumes specs exist before coding; frequent spec gaps cause excessive pauses)

## Core Principles

- **Work may be discarded to preserve clarity and momentum** — Salvaging flawed work often costs more than rewriting from spec. When code carries the scars of multiple failed iterations, starting fresh produces cleaner results faster than negotiating with accumulated debt. **Discard is a Planner decision, only after exhausting defined limits** (5 review cycles, 2 coder failures, so 10 iterations). No premature abandonment.
- **Corrections leave trails** — Every rescope, rejection, and spec change is logged with rationale. The activity log (`log.yaml`) is append-only for audit; state (`state.yaml`) uses atomic read-modify-write. No silent rewrites, no "it was always like this." Future agents (and humans) can reconstruct why.
- **Bounded failure is preferred over prolonged negotiation** — Five review cycles, then escalate. Two coders fail, then rescope. Ten iterations, then block. Hard limits prevent polite infinite loops where agents keep trying without progress.
- **Every restart is a new mind with old artifacts, not continuity** — Agents don't remember previous sessions. They read specs, blackboard, and handoff notes fresh. Design for amnesia: if it's not written down, it doesn't exist for the next agent.

### Cost Gradient

```
Thought → Words → Specs → Code → Tests → Docs → Commits
◄─────────────── cheaper ─────────────────────────►
```

Errors caught in specs cost less than errors caught in code. The spec system front-loads understanding so agents don't discover requirements by failing tests.

## Success Criteria

Liza succeeds when:

1. **Quality maintained** — Work produced under Liza passes the same quality bar as human-supervised pairing
2. **Human time reduced** — Human acts as observer/circuit-breaker, not approval bottleneck
3. **Failures visible** — When things go wrong, the blackboard and logs make it obvious what happened
4. **Recovery tractable** — Human can pause, inspect, redirect, or abort at any point
5. **Context survives restarts** — Agent replacement doesn't lose semantic understanding

Quantitative signals (collect during v1 usage):
- Review cycle count per task (target: ≤3 on average)
- Hypothesis exhaustion rate (target: <10% of tasks)
- Human intervention frequency (target: <1 per sprint)
- Time from task creation to merge (baseline needed)

## MVP Scope (v1)

**In scope:**
- Single goal, single [sprint](protocols/sprint-governance.md) at a time
- One Planner, one Coder, one Code Reviewer (the 3 in parallel but a single instance per role)
- Terminal-based observation
- YAML blackboard with file locking
- Shell script tooling
- Human-triggered circuit breaker analysis
- Checkpoint-based retrospectives

## Explicit Out of Scope (v1)

| Feature | Rationale |
|---------|-----------|
| IDE integration | Terminal workflow sufficient; IDE adds complexity |
| Web dashboard | `tail -f` and `watch` sufficient for observation |
| Multi-repo coordination | Single repo focus for v1 |
| Parallel coders | Validate sequential first |
| Real-time circuit breaker | Human-triggered analysis sufficient |
| Token budget tracking | API doesn't expose; calendar time proxy |
| SQLite backend | YAML sufficient at v1 scale |
| MCP server integration | Nice-to-have, not essential |

## Risks and Assumptions

### Assumptions

| Assumption | Impact if Wrong |
|------------|-----------------|
| Claude Code CLI supports mode-based prompting | Need workaround for agent invocation |
| Agents can reliably call shell scripts | Core mechanism broken |
| YAML + flock sufficient for coordination | Race conditions, corruption |
| Exit code 42 triggers restart reliably | Supervision model fails |
| Agents will log anomalies honestly | Circuit breaker ineffective; mitigated by anti-gaming clause in CORE.md |
| Specs substantially complete before work | System pauses frequently for spec updates; defeats throughput in emergent-requirements domains |
| Planner interprets failures correctly | Single semantic interpreter; bias propagates to all task framings. Human is appeal mechanism via CHECKPOINT |

### Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Contract discipline degrades under Liza mode | Medium | High | Extensive testing, circuit breaker |
| Code Reviewer rubber-stamps coder work | Medium | High | Review verdict approval rate monitoring (>95% over ≥5 review verdicts triggers warning) |
| Context exhaustion causes knowledge loss | Medium | Medium | Structured handoff, spec-first design |
| Blackboard corruption from concurrent access | Low | High | flock, validation on every read |
| Human forgets to check CHECKPOINT | Medium | Medium | Desktop notifications, alert log |
| Spec changes while task in progress | Low | Medium | Code Reviewer validates against current spec; v2: spec_hash tracking |

## Related Documents

- [Architecture Overview](architecture/overview.md) — components, data flow, spec artifacts
- [Roles](architecture/roles.md) — Planner, Coder, Code Reviewer responsibilities
- [Task Lifecycle](protocols/task-lifecycle.md) — claim, iterate, review, merge flow
- [Sprint Governance](protocols/sprint-governance.md) — checkpoints, retrospectives
- [Circuit Breaker](protocols/circuit-breaker.md) — systemic failure detection
- [Implementation Phases](implementation/phases.md) — build roadmap
- [Validation Checklist](implementation/validation-checklist.md) — v1 completion criteria

---

*Note: This document is the canonical vision specification. README.md provides an external-facing summary.*
