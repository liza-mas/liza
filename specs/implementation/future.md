# Future Improvements

## Deferred for v2

| Feature | Description | Rationale for Deferral |
|---------|-------------|------------------------|
| **Integration rollback** | Mechanism to revert merged work from integration branch | v1 is forward-only by design; errors discovered after merge require manual git surgery outside Liza protocols. Decision needed: add ROLLBACK checkpoint action with blackboard audit trail, or document as out-of-scope. |
| **Parallel coders** | Multiple coders working simultaneously | Adds complexity; validate single-coder first |
| **Subagent support** | Within-terminal delegation in multi-agent context | Existing subagent mode works; integration can wait |
| **Plan Reviewer role** | Dedicated agent to validate task decomposition, spec references, and success criteria before coders claim | Human review sufficient for v1; Planner self-validation gates reduce risk |
| **Second Code Reviewer escalation** | Different Code Reviewer after N rejections | Single Code Reviewer in v1 |
| **Merger role separation** | Separate merge authority from review authority (Code Reviewer judges, Merger/Human executes) | v1 trusts Code Reviewer discipline; review verdict approval-rate monitoring detects rubber-stamping. Revisit if warnings persist. |
| **Model heterogeneity** | Different models for different roles | Operational complexity; test with homogeneous first |
| **SQLite backend** | Replace YAML with transactional store | YAML sufficient for small-scale; upgrade when stable |
| **Continuous integration shadow** | Rebase/test on every intermediate state | Compute-heavy; event-based sufficient for v1 |
| **Mutation testing agent** | Adversarial test generation | High-value but complex; add when basic flow is solid |
| **Web dashboard** | Visual blackboard state | Terminal observation sufficient for v1 |
| **MCP integration** | Blackboard as MCP server | Nice-to-have, not essential |
| **Blackboard archival** | Auto-archive terminal tasks | Won't hit scale in v1 |
| **Real-time circuit breaker** | Continuous pattern monitoring | v1 human-triggered sufficient |
| **Token budget tracking** | Per-sprint token limits | API doesn't expose; calendar time sufficient |
| **Multi-sprint planning** | Release/milestone level planning | One sprint at a time for v1 |
| **Automated spec update suggestions** | Agent proposes spec changes | Human-only spec changes for v1 |
| **Spec Writer role** | Dedicated agent with `spec-writing` skill to author specs from vision | Human writes specs for v1; role adds value when spec volume increases |
| **Spec Reviewer / Systemic Reviewer roles** | `spec-review` skill for consistency, `systemic-thinking` for tensions. Workflow: spec-review → fix → systemic-thinker → decide. Verdict schema: BLOCKED/PASSED/ADVISORY with typed findings (TENSION, LOAD_BEARING) containing summary + implication. Scope levels: goal \| epic \| milestone. | Human review sufficient for v1; formalize when spec complexity warrants |

---

## v1.1 Roadmap

Items promoted from "Ideas for Exploration" for near-term implementation:

| Feature | Rationale |
|---------|-----------|
| **Metrics collection** | Essential for sprint calibration — design schema in v1, automate collection in v1.1 |
| **Checkpoint snapshots** | Enables rollback on bad checkpoint decision |
| **Learning loop** | Retrospective data enables prompt/contract tuning |

---

## Technical Debt Accepted

| Item | Impact | Trigger for Payback |
|------|--------|---------------------|
| YAML-based blackboard | Performance at scale | >50 concurrent tasks |
| Shell-based tooling | Limited error handling | First major scripting bug |
| Manual integration branch | Human must trigger main merge | When automation is safe |
| Single watcher instance | No redundancy | Watcher crashes unnoticed |
| No formal state machine | Relies on contract discipline | Repeated invalid transitions |

---

## Ideas for Exploration

| Idea | Notes |
|------|-------|
| **Adversarial Code Reviewer** | Code Reviewer with explicit mandate to find faults |
| **Plan falsification agent** | "How would this plan fail?" before execution |
| **Goal decomposition templates** | Pre-built task patterns for common goals |
| **Deterministic replay** | Snapshot environments for debugging |
| **Human intervention triggers** | Extend circuit breaker with human-facing signals: Quality (test coverage drop, lint errors, complexity spike), Impact (API signature change, new dependency, schema migration, secrets), Uncertainty (agent expresses doubt, equivalent alternatives), Coordination (merge conflict, story dependency blocked). Formalize as intervention gates or circuit breaker extensions. See [chat reference](https://claude.ai/chat/79c1df73-6b63-46ba-85bc-57289f801453). |
| **Contract trade-offs format** | Explicit "X Over Y" format for design decisions (from SpecKit). Makes value hierarchy visible. |
| **Automated compliance check** | Agent-based contract/spec compliance analysis — maps to peer review but could be continuous |
| **Contract semantic versioning** | MAJOR/MINOR tracking for contract evolution. Helps track when breaking changes occur. |
| **SpecKit/BMAD deep dive** | In-depth review for process/agents/skills worth stealing. SpecKit Constitution principles (modularity, observability, simplicity, boundaries), "Articles > Clauses > Anti-patterns" hierarchy. See [chat reference](https://claude.ai/chat/fcb8c786-3002-45b8-9d71-8512df9f7184). |

## Related Documents

- [Validation Checklist](validation-checklist.md) — v1 completion criteria
- [Phases](phases.md) — current implementation sequence
