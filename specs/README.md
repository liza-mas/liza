# Liza Specification Index

## Quick Navigation

### Architecture

| Document                                               | Purpose |
|--------------------------------------------------------|---------|
| [C4 Diagrams](architecture/c4/c4.md) | System context, container, and component diagrams |
| [Overview](architecture/overview.md)                   | System components, data flow, directory structure |
| [Roles](architecture/roles.md)                         | Planner, Coder, Code Reviewer responsibilities |
| [State Machines](architecture/state-machines.md)       | Task states, agent states, exit codes |
| [Blackboard Schema](architecture/blackboard-schema.md) | state.yaml structure, locking, operations |
| [Supervision Model](architecture/supervision-model.md) | Action responsibility: supervisor vs agent via CLI commands |
| [Architecture Review](architecture/architecture-review.md) | Architecture review and analysis |
| [Architectural Issues](architecture/architectural-issues.md) | Persistent record of issues from architectural analysis |
| [ADR/](architecture/ADR/)                              | Architecture Decision Records (created as decisions arise) |

### Build (Intent)

| Document                        | Purpose |
|---------------------------------|---------|
| [Vision](<build/1 - Vision.md>) | Why Liza exists, target users, success metrics, risks |
| [1.1 Contract System](<build/1.1 - Contract System.md>) | Epic: enforceable behavioral contract and mode gates |
| [1.1.1 Pairing Safety Gates](<build/1.1.1 - Pairing Safety Gates.md>) | Story: explicit human gate and invariant enforcement in pairing |
| [1.1.2 Multi-Agent Gate Semantics](<build/1.1.2 - Multi-Agent Gate Semantics.md>) | Story: mode-specific gate artifacts and forbidden transition enforcement |
| [1.1.3 Secrets and Recovery Protocols](<build/1.1.3 - Secrets and Recovery Protocols.md>) | Story: explicit secret handling and deterministic violation recovery flow |
| [1.2 Multi-Agent Coordination](<build/1.2 - Multi-Agent Coordination.md>) | Epic: blackboard-driven role coordination and deterministic wake/claim flow |
| [1.2.1 Blackboard Coordination and Agent Wake Flow](<build/1.2.1 - Blackboard Coordination and Agent Wake Flow.md>) | Story: planner wake detection and coder/reviewer claim orchestration via shared state |
| [1.2.2 Stale-Lease Reclamation Policy](<build/1.2.2 - Stale-Lease Reclamation Policy.md>) | Story: deterministic stale-lease detection and safe claim reclamation policy |
| [1.2.3 Coordination Observability Views](<build/1.2.3 - Coordination Observability Views.md>) | Story: operator-facing views for role drift and queue health visibility |
| [1.3 Task Management](<build/1.3 - Task Management.md>) | Epic: claim/review/verdict lifecycle with escalation limits and auditable transitions |
| [1.3.1 Claim-to-Verdict Task Lifecycle](<build/1.3.1 - Claim-to-Verdict Task Lifecycle.md>) | Story: three-phase claim, rebase-based review submission, and verdict enforcement |
| [1.3.2 Integration-Fix Loop Guidance](<build/1.3.2 - Integration-Fix Loop Guidance.md>) | Story: explicit recovery/ownership loop for INTEGRATION_FAILED tasks |
| [1.3.3 Automatic Rescope Prompts](<build/1.3.3 - Automatic Rescope Prompts.md>) | Story: planner prompts when review/iteration limits are exhausted |
| [1.4 Worktree Isolation](<build/1.4 - Worktree Isolation.md>) | Epic: isolated task execution and controlled integration |
| [1.4.1 Task Worktree Provisioning](<build/1.4.1 - Task Worktree Provisioning.md>) | Story: per-task worktree creation, reassignment, and base commit capture |
| [1.4.2 Approved-Commit Integration Constraints](<build/1.4.2 - Approved-Commit Integration Constraints.md>) | Story: integration merges constrained to explicitly approved review commits |
| [1.4.3 Reassignment Recovery Rules](<build/1.4.3 - Reassignment Recovery Rules.md>) | Story: deterministic reuse-vs-recreate behavior for reassigned worktrees |
| [1.5 Circuit Breaker](<build/1.5 - Circuit Breaker.md>) | Epic: systemic anomaly pattern detection and human checkpoint escalation |
| [1.5.1 Pattern-Triggered Checkpoint Escalation](<build/1.5.1 - Pattern-Triggered Checkpoint Escalation.md>) | Story: deterministic pattern detection, report creation, and mode trip behavior |
| [1.5.2 Configurable Pattern Thresholds](<build/1.5.2 - Configurable Pattern Thresholds.md>) | Story: runtime-tunable detection thresholds without code edits |
| [1.5.3 Trend-Based Early Warnings](<build/1.5.3 - Trend-Based Early Warnings.md>) | Story: pre-trigger warning signals based on metric/anomaly trends |
| [1.6 Skills](<build/1.6 - Skills.md>) | Epic: modular domain-specific protocol library |
| [1.6.1 Skill Triggering and Use](<build/1.6.1 - Skill Triggering and Use.md>) | Story: trigger-based skill loading under contract |
| [1.6.2 Skill Packaging and Distribution](<build/1.6.2 - Skill Packaging and Distribution.md>) | Story: consistent packaging/install flow for reproducible skill behavior |
| [1.6.3 Mandatory High-Risk Skill Enforcement](<build/1.6.3 - Mandatory High-Risk Skill Enforcement.md>) | Story: required debugging/testing/review skills for critical workflows |
| [Build Changelog](<build/changelog.md>) | Build hierarchy change history |

### Functional (Current State)

| Document                                        | Purpose |
|-------------------------------------------------|---------|
| [Product Description](<functional/1 - Liza.md>) | What Liza is, domains, key integrations, scope |

### Protocols

| Document | Purpose |
|----------|---------|
| [Task Lifecycle](protocols/task-lifecycle.md) | Claim, iterate, review, merge flow |
| [Sprint Governance](protocols/sprint-governance.md) | Checkpoints, retrospectives, spec evolution |
| [Circuit Breaker](protocols/circuit-breaker.md) | Systemic failure detection, severity classification |
| [Worktree Management](protocols/worktree-management.md) | Isolated workspaces, merge protocol |
| [Agent Initialization](protocols/agent-initialization.md) | Agent bootstrap sequence from prompt to productive work |

### Implementation

| Document | Purpose |
|----------|---------|
| [Tooling](implementation/tooling.md) | Scripts, agent-blackboard interface, startup sequence |
| [Phases](implementation/phases.md) | Implementation roadmap (13 phases) |
| [Validation Checklist](implementation/validation-checklist.md) | v1 completion criteria |
| [Future](implementation/future.md) | v1.1 roadmap, deferred items, technical debt |

---

## Reading Order

**For understanding the system:**
1. [Vision](<build/1 - Vision.md>) — philosophy and rationale
2. [Product Description](<functional/1 - Liza.md>) — what Liza is today
3. [Architecture Overview](architecture/overview.md) — components and flow
4. [Roles](architecture/roles.md) — who does what
5. [Task Lifecycle](protocols/task-lifecycle.md) — how work flows

**For implementation:**
1. [Blackboard Schema](architecture/blackboard-schema.md) — data structures
2. [Tooling](implementation/tooling.md) — scripts and interfaces
3. [Phases](implementation/phases.md) — build order

**For operations:**
1. [Sprint Governance](protocols/sprint-governance.md) — checkpoints and retrospectives
2. [Circuit Breaker](protocols/circuit-breaker.md) — failure detection
3. [Validation Checklist](implementation/validation-checklist.md) — completeness check

---

## Key Concepts

### Four Pillars

1. **Behavioral contracts** — Tier 0 invariants turn agents into accountable peers
2. **Externally validated completion** — Coders cannot self-certify; Code Reviewers approve
3. **Specification system** — specs persist understanding across agent restarts
4. **Blackboard coordination** — all state visible through shared file

### Design Philosophy

See [Vision](<build/1 - Vision.md>) for the design philosophy and cost gradient.

---

## Related Documents

- [README.md](../README.md) — project overview
- [contracts/](../contracts/) — behavioral contracts (LOADER, CORE, modes)

---

## Document Status

| Category | Documents | Status |
|----------|-----------|--------|
| Build | 26 (Vision + 6 epics + 18 stories + changelog) | Partial backfill |
| Functional | 7 (0.md + 1.1–1.6) | Complete |
| Architecture | 6 + ADR/ | Complete |
| Protocols | 5 | Complete |
| Implementation | 4 | Complete |
| Contracts | 8 | Complete |

---

## Maintenance Notes

### Agent Prompt Templates

Operational reference content (blackboard fields, state machine, anomaly types, etc.) is inlined directly into agent prompts via Go templates in `internal/prompts/templates/`. The key templates are:

| Template | Content |
|----------|---------|
| `shared_reference.tmpl` | Shared operational content (state machine, blackboard fields, anomaly types, lease model, exit codes) |
| `planner_context.tmpl` | Planner logging duties, self-validation gates, field format guidelines |
| `coder_context.tmpl` | Coder logging duties, blocking protocol |
| `reviewer_context.tmpl` | Reviewer logging duties, review scope, approval semantics |

**Sync requirement:** When updating these specs, check if the prompt templates need corresponding updates:

| Spec | Templates affected |
|------|-------------------|
| `roles.md` | Role-specific templates (planner/coder/reviewer_context.tmpl) |
| `blackboard-schema.md` | `shared_reference.tmpl` field tables |
| `state-machines.md` | `shared_reference.tmpl` state transitions |
| `task-lifecycle.md` | `shared_reference.tmpl` + role templates |

The specs contain rationale and design context; the templates contain only what agents need to act.

**Trade-off:** Templates are compiled into the binary. Updating operational content requires `make build` → reinstall → restart agents. This higher deployment friction was accepted to eliminate the failure mode where agents skip reading a deployed reference file.
