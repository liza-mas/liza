# How Liza Delivers Production-Ready Code

## The Evolution of Agent Quality

The industry's approach to making coding agents reliable has evolved through several generations,
each addressing a real limitation — and each insufficient on its own:

**Prompt engineering** came first. Instructions in system prompts: "don't modify tests,"
"follow this workflow," "ask before acting." The problem: agents interpret instructions flexibly.
Under pressure to appear competent, "don't modify tests" loses to the drive to make things green.
Prompts shape intent but can't enforce it.

**Context engineering** addressed a real failure mode: agent quality degrades as context windows fill.
Fresh subagent contexts, spec-driven plans, file-path passing, context budgets — these prevent
the rot that accumulates in long sessions. The problem: a fresh-context agent can still
fabricate, mutate tests, or silently expand scope. Better information doesn't change behavior
under pressure.

**Harness engineering** made codebases legible to agents — structured repos, workflow documents,
CI integration. The problem: a well-structured codebase doesn't prevent an agent from greenwashing
tests or self-approving its own work. The harness wraps the environment, not the agent.

**The [Ralph Wiggum loop](https://github.com/anthropics/claude-code/tree/main/plugins/ralph-wiggum)**
compensated for all of the above with brute force: loop agents until they converge through
persistence. Keep trying until it's right. The problem: iterating on bad framing produces
confidently wrong results. An agent retrying its own work has no external signal
to distinguish "almost there" from "fundamentally wrong approach."

**Role specialization** multiplied agent types — 60+ specialized roles, each with a narrow
prompt and dedicated tools. The problem: specialization fragments capabilities. A "security-coder"
that hits a debugging problem can't load a debugging methodology. Narrow roles create
rigid pipelines where every new need requires a new agent type.

All five assume good-faith execution or that persistence compensates for misbehavior.
They improve what agents know, see, retry, or specialize in — not what they *do* under pressure.
Each is necessary — and none is sufficient.

Liza starts from the opposite assumption: agents will exhibit predictable failure modes unless
specifically constrained. Rather than replacing these approaches, Liza subsumes and hardens them:

- **Prompt engineering → behavioral contract.** Still prompts — but designed as a system:
  55 failure modes mapped to countermeasures, an explicit state machine with forbidden transitions,
  tiered rules that define what never bends. Not instructions agents interpret flexibly,
  but an executable specification.
- **Ralph Wiggum loop → adversarial review.** Still iterative — but with constructive feedback.
  A different agent reviews, rejects with structured comments (file:line, defect, fix),
  and the coder revises against that feedback. Not blind persistence, but informed convergence.
- **Context engineering → short-lived agents with externalized memory.** Each task gets
  a fresh agent — no context rot. Specs, checkpoints, and history survive in durable files
  that new agents read at bootstrap. The context freshness that context engineering achieves
  through orchestrator complexity, Liza gets architecturally.
- **Role specialization → fewer roles with composable skills.** 9 roles, not 60+.
  Capabilities come from 21 skills agents load on demand — a coder hitting a bug loads
  the debugging skill; a reviewer with structural concerns loads the architecture skill.
  Roles define boundaries, skills define capabilities.
- **Harness engineering → a much broader and more diverse harness.** Not just codebase
  structure, but five enforcement layers: behavioral contracts, adversarial architecture,
  compiled Go enforcement, git workspace isolation, and composable quality skills.

---

## Inventory

Every component — from the behavioral contract agents read to the Go code that wraps them —
exists to prevent a specific class of failure.

This document inventories every hardening measure, organized by enforcement layer.
The layers compose into defense-in-depth: no single layer is sufficient, but together they cover
the [55 documented failure modes](../contracts/CONTRACT_FAILURE_MODE_MAP.md) with zero gaps.

```
┌───────────────────────────────────────────────────────────┐
│  Layer 5: Quality Skills & Observability                  │
│  21 composable skills · circuit breaker · anomaly logging │
├───────────────────────────────────────────────────────────┤
│  Layer 4: Git & Workspace Isolation                       │
│  worktrees · merge authority · git-guard hooks · rebase   │
├───────────────────────────────────────────────────────────┤
│  Layer 3: Mechanical Go Enforcement                       │
│  supervisor · MCP middleware · state machine · TDD gates  │
├───────────────────────────────────────────────────────────┤
│  Layer 2: Adversarial Architecture                        │
│  doer/reviewer pairs · role boundaries · verdict protocol │
├───────────────────────────────────────────────────────────┤
│  Layer 1: Behavioral Contract                             │
│  55 failure modes · state machine · tiered rules · hooks  │
└───────────────────────────────────────────────────────────┘
```

**Liza doesn't make agents smarter. It makes them accountable — at every layer.**

---

## Layer 1: Behavioral Contract

The contract is Liza's foundational layer. It operates at the LLM prompt level — shaping agent behavior
before any code or tool runs.

### 55 Failure Modes, 55 Countermeasures

The [Contract Failure Mode Map](../contracts/CONTRACT_FAILURE_MODE_MAP.md) catalogs every known way
coding agents fail, sourced from:

- **MAST taxonomy** (Berkeley, 2025) — 14 multi-agent failure modes from 1,600+ traces
- **LLM behavioral research** — sycophancy, deception, hallucination studies (2024–2025)
- **Code generation studies** — bug introduction, incomplete refactoring, test corruption
- **Instruction following benchmarks** — constraint failures, tool violations
- **Gaming vectors** — intent violation, interpretation narrowing, prompt injection

Each failure mode maps to a specific contract clause. Coverage: 55/55 strong, 0 partial, 0 gaps.

| Category | Modes | Example |
|----------|-------|---------|
| Specification & system design | 5 | Step repetition (17% of MAS failures) → stop trigger: same fix proposed twice |
| Inter-agent misalignment | 6 | Reasoning-action mismatch (14%) → exposed reasoning with tagged assumptions |
| Task verification | 3 | Incomplete verification (7%) → validation must exercise changed behavior |
| Sycophancy | 5 | Softening critical feedback → direct response rule, no cheerleading |
| Deception | 6 | Claiming success without validation → Tier 0 invariant, mandatory halt |
| Hallucination | 4 | Fabricating files/APIs → source validation, never invent |
| Code generation | 9 | Test corruption → Tier 0 invariant (T0.3), test protocol |
| Gaming & exploitation | 6 | "Technically compliant" → anti-gaming clause: outcome matters, not letter |

### Tiered Rule Architecture

Not all rules degrade equally. The contract defines what never bends versus what degrades gracefully
under context pressure:

| Tier | Name | Violation Response | Example Rules |
|------|------|--------------------|---------------|
| **0** | Hard Invariants | Mandatory halt. No Resume — only Undo or Abandon | No fabrication, no test corruption, no unvalidated success, no secrets |
| **1** | Epistemic Integrity | Suspended only with explicit waiver | Assumption budget, intent gate, source declaration |
| **2** | Process Quality | Best-effort under pressure | DoR/DoD completeness, batch validation, DRY gate |
| **3** | Collaboration Quality | Degraded gracefully | Mode discipline, knowledge transfer |

When context degrades (long sessions, large tasks), agents announce which tier they're operating at.
Tier 0 rules survive even severe context degradation.

### Agent State Machine

Agents operate as an explicit state machine with forbidden transitions:

```
IDLE → ANALYSIS → READY → EXECUTION → VALIDATION → DONE
```

**Forbidden transitions** (cannot skip gates):
- ANALYSIS → EXECUTION (skipping approval gate)
- READY → DONE (skipping execution and validation)
- EXECUTION → DONE (skipping validation)

**Stop triggers** halt execution when patterns indicate failure:
- Same fix proposed twice without new rationale
- Evidence contradicts hypothesis
- ≥3 assumptions on critical path
- Tool fails 3× consecutively
- Same rule violated twice in session

### Contract Reading Enforcement

The contract isn't optional. Two complementary mechanisms ensure agents read it:

**Mechanical gate (Claude Code):** A PreToolUse hook (`enforce-init.sh`) blocks all state-modifying
tool calls until the agent has read the mode-specific contract, `AGENT_TOOLS.md`, and `GUARDRAILS.md`.
Session state tracked via `${TMPDIR:-/tmp}/liza-init-gate-${session_id}`. Once cleared, the gate is transparent.

**Canary test (all providers):** Four "secret words" are embedded across four contract files.
The contract instructs agents to display them at session start — a
[Van Halen brown M&M's trick](https://colterreed.com/blog/the-genius-of-banishing-brown-mms/)
that makes compliance (or its absence) immediately visible. This is the primary enforcement
mechanism for providers that don't support hooks.

---

## Layer 2: Adversarial Architecture

Every activity in Liza has a doer and a reviewer. They interact like a PR review —
submission, feedback, verdict, revision — until approval.

### Doer/Reviewer Pairs

Liza has 9 roles organized in 5 pairs across 2 pipeline phases:

| Phase | Doer | Reviewer |
|-------|------|----------|
| Specification | Epic Planner | Epic Plan Reviewer |
| Specification | US Writer | US Reviewer |
| Coding | Code Planner | Code Plan Reviewer |
| Coding | Coder | Code Reviewer |
| Both | Orchestrator | (decomposes, rescopes) |

The adversarial pattern means no agent self-approves. The coder writes code;
a different agent — the code reviewer — examines it, issues a binding verdict, and only then
can the supervisor merge.

### Structural Role Separation

Role boundaries are enforced at three levels:

1. **Contract level**: agents read role-specific prompts defining their boundaries
2. **MCP middleware level**: Go code validates every tool call against the agent's role
3. **Pipeline YAML level**: `allowed-operations` defines what each role can invoke

The result: a coder literally cannot call `liza_submit_verdict` or `liza_wt_merge`.
A reviewer cannot call `liza_write_checkpoint`. The orchestrator cannot claim coding tasks.

### Review Protocol

The review loop enforces structured feedback:

1. **Coder** implements, records commit SHA, submits for review
2. **Reviewer** verifies SHA matches worktree HEAD (no reviewing stale code)
3. **Reviewer** examines all changes from `base_commit` to `review_commit` — not just the latest delta
4. **Verdict**: APPROVED (merge-eligible) or REJECTED (structured feedback: file:line, defect, fix)
5. On rejection: coder addresses feedback, resubmits — iteration 2+ must reference prior feedback status

**Iteration limits** prevent infinite loops: 10 coder iterations, 5 review cycles.
After exhaustion → task BLOCKED, escalated to orchestrator.

### Quorum Review

Not all code changes carry equal risk. The review system supports configurable quorum:

- **Standard tasks**: 1 reviewer sufficient
- **Significant/architectural tasks**: 2+ reviewers required, with `partially_approved` intermediate state
- **Impact escalation**: task impact can be upgraded (standard → significant → architecture),
  raising the reviewer threshold mid-flight

The quorum is enforced mechanically — `SubmitVerdict()` checks approval count against
the task's required quorum before allowing transition to APPROVED.

### Reviewer Diversity

Different LLM providers share blind spots. The reviewer assignment system promotes
provider diversity at two levels:

- **Doer→reviewer diversity**: when `provider-diversity: preferred` is configured for a role-pair,
  the system blocks reviewers that share the coder's LLM provider if a different-provider
  reviewer is registered — even if currently busy. Code written by Claude gets reviewed
  by Gemini (or vice versa) whenever possible.
- **Cross-approval diversity**: for quorum reviews (2+ reviewers), candidate selection prefers
  tasks where the claimer's provider differs from existing approvals, so the second reviewer
  brings a different model's perspective.

Diversity is a soft preference, not a hard gate — if only one provider is available,
reviews still proceed. The mechanism is configurable per impact level, so high-impact
tasks can require diversity while routine tasks skip the overhead.

### Hypothesis Exhaustion

When a task exhausts both attempts (each with independent iteration and review budgets),
the system presumes the task framing is wrong.
The task gets blocked and escalated for rescoping, not retried.

---

## Layer 3: Mechanical Go Enforcement

The Go supervisor and MCP server enforce what agents cannot circumvent — regardless of what
the LLM decides to do.

The design principle: **everything that doesn't require judgment is preferably handled mechanically.**
Agents exercise judgment on *what* to build and *how* to test it. But state transitions, role boundaries,
concurrency, input validation, and secret handling are not judgment calls — they're invariants,
and invariants belong in compiled code, not prompts.

### Blackboard

The YAML blackboard (`state.yaml`) is more than a coordination mechanism — it's the single
source of truth for roles, responsibility, observability, and auditability:

- **Responsibility**: every task records `assigned_to`, `reviewing_by`, `approved_by`,
  `merge_commit`. Who did what is always traceable, not reconstructed from git blame
- **Observability**: humans and the orchestrator read the same state agents write to.
  `liza status`, `liza get tasks`, and the console all render the blackboard directly —
  no separate monitoring layer, no derived views that can drift
- **Auditability**: every state transition appends to the task's `history` array with
  timestamp, agent, event type, and event-specific details. The history is append-only —
  agents cannot rewrite past events
- **Role enforcement**: the blackboard records agent registrations with role and provider,
  enabling the MCP middleware to validate every tool call against the caller's identity

Concurrency guarantees protect this central structure: atomic writes (flock → temp-write →
fsync → rename), diagnostic lock-owner metadata, and a three-phase claim pattern that
prevents TOCTOU races during task assignment.

### Agent Supervisor

Every agent runs wrapped by a Go supervisor that handles registration, heartbeat,
and post-exit cleanup. The supervisor detects restart loops (exit code 42) by comparing
state hashes between restarts — if the agent isn't making progress, the task gets blocked
with exponential backoff rather than retried indefinitely.

Supervisor-only actions (agents cannot perform): registration, heartbeat, post-exit reset.

#### Generation-Fenced Recovery Contract

- **Authority:** Current-generation authority is required at every agent-authenticated lifecycle write. The caller's agent ID and opaque registration generation are compared inside the same `Blackboard.Modify` that performs the write, before any mutation. This fence is independent of effective-operation authorization: it does not change RBAC permissions.
- **Provider launch:** One cross-process per-agent lifecycle lock orders registration against provider start. Registration acquires the lifecycle lock before the blackboard lock. A launch holder reads state, verifies current-generation authority, and reaches only the provider's start/session-creation boundary; no provider backend effect runs inside `Blackboard.Modify`. Built-in providers complete start before wait and wait outside both locks. The compatibility adapter holds the lifecycle lock for the complete blocking legacy call because its interface exposes no narrower start boundary. Lock timeout, state-read failure, generation mismatch, setup failure, or process-start failure releases the lock with no successful start event or state rewrite.
- **Liveness:** Ownership is lease-first, and namespace-unverifiable ownership has effective status `unknown/degraded`. A fresh heartbeat and unexpired lease continue to occupy singularity and role capacity despite a raw dead or mismatched PID observation from another namespace. Registration and watcher diagnostics preserve the registered PID and report a deterministically correlated observer-visible PID, or explicitly state `correlation unavailable`; that process evidence cannot authorize takeover. Expiry or explicit audited removal remains required.
- **Approved merge takeover:** If the final approver exits, a deterministic current-generation reviewer may resume the already-approved merge. Takeover preserves immutable approval actors, quorum, provider-diversity evidence, review commit, and reviewer role. Recovery converges from both Git not advanced and Git already advanced while task state remains approved, without duplicating the integration result or merged history. This does not serialize issue #129.
- **Rejected-task handoff:** Release and reclaim treat `assigned_to, lease_expires, worktree, base_commit, physical task artifact, and reviewer affinity` as one locked tuple. Recovery reuses a healthy worktree, reattaches a valid task branch when its directory is absent, and recreates from integration only when no reusable valid artifact exists. It fails closed without deleting unclassifiable artifacts.

### MCP Tool Middleware

Every MCP tool call passes through role validation and operation checking before reaching
the handler. A coder cannot call `liza_submit_verdict`. A reviewer cannot call
`liza_write_checkpoint`. Violations fail with clear messages naming the agent, role,
and forbidden operation.

### Task State Machine

The task lifecycle is enforced in Go code, not just described in specs.
Every transition validates preconditions — claiming requires READY status, submission
requires a checkpoint and test files, verdicts require SHA match, merging requires approval.
Forbidden transitions are rejected immediately. Terminal states are irreversible.

### TDD Enforcement

Code submission requires test files — mechanically enforced across 8 languages,
not instructional. No test files = automatic rejection, with a waiver path requiring
justification that the reviewer (not the coder) verifies.

Before implementation, agents must write a pre-execution checkpoint: intent, validation plan,
and files-to-modify. This is the non-HITL counterpart of pairing mode's approval gate —
the agent externalizes its plan before acting, making reasoning auditable.

### Lease System

Tasks are assigned with time-bounded leases (default 30 minutes, configurable per role).
Heartbeats refresh the lease. Stale leases are detected and released automatically —
dead agents don't hold tasks indefinitely.

### Compare-and-Swap Merges

Reviewer merges use `git merge-tree` — no working tree checkout, so multiple reviewers
can merge in parallel. Compare-and-swap semantics on `update-ref` prevent race conditions.
Merge conflicts reassign to the coder; integration failures roll back to pre-merge HEAD.

### Secret Masking

Agent output is automatically scrubbed of sensitive environment variable values
(API keys, tokens, passwords) below the agent layer. Agents don't need to actively
redact — the masker enforces the contract's T0.5 (no secrets exposure) mechanically.

### Input & State Validation

A dedicated validation layer enforces structural integrity on all state mutations:
task ID sanitization prevents path traversal, graph traversal catches circular dependencies
before they deadlock, SCC analysis detects planning cycles during pipeline transitions,
and spec reference validation ensures referenced files actually exist.

---

## Layer 4: Git & Workspace Isolation

### Worktree Isolation

Every task gets its own git worktree (`.worktrees/{taskID}`):

- Parallel development without interference
- Deterministic paths (no directory traversal)
- Clean state: worktrees deleted on terminal states (MERGED, ABANDONED, SUPERSEDED)
- New attempt on a task → worktree deleted and recreated fresh (no context contamination from failed approach)

### Merge Authority

Coders cannot commit to or merge to the integration branch. The merge path:

1. Coder commits to task worktree
2. Coder rebases onto integration branch before submission (conflict → abort, restore clean state)
3. Reviewer examines, approves
4. **Supervisor** performs the merge — only after reviewer approval

Merge traceability: every task records `approved_by` and `merge_commit`.

### Git-Guard Hook

A PreToolUse hook (`git-guard.sh`) blocks destructive git operations for Liza agents:

- `git push --force` / `git push -f`
- `git reset --hard`
- `git clean -f`

Active only for Liza agents (detected via `LIZA_AGENT_ID` env var).
Pairing sessions are exempt (human already in the loop).

### Rebase-Before-Review

Before submission, the supervisor ensures the worktree is rebased onto the current integration branch
and the working tree is clean (no staged, unstaged, or untracked files). Conflicts abort
the submission — they're caught here, not during merge.

---

## Layer 5: Quality Skills & Observability

### 21 Composable Skills

Skills encode methodology — not instructions, but structured protocols agents load on demand:

| Skill | Purpose |
|-------|---------|
| **code-review** | Structured review: P0 (security), P1 (correctness), P2 (data integrity), P3 (quality) |
| **testing** | Test protocol: coverage strategy, edge cases, TDD enforcement |
| **debugging** | Structured RCA before attempting fixes — no "quick tries" |
| **white-box-red-testing** | Adversarial testing: find bugs by writing tests that should pass but don't |
| **black-box-red-testing** | External adversarial testing without code access |
| **software-architecture-review** | Structural concerns, dependency analysis, coupling evaluation |
| **clean-code** | Language-specific style enforcement |
| **code-quality-assessment** | Complexity metrics, maintainability scoring, refactoring recommendations |
| **systemic-thinking** | Systemic coherence and risk analysis |
| **detailed-spec-writing** | Transform requirements into precise specifications |
| **epic-writing** | Transform vision into structured epics |
| **user-story-writing** | Transform requirements into user stories |
| **spec-backfill** | Backfill missing specifications, reconcile spec/code drift |
| **code-spec-backfill** | Backfill function-level contracts (docstrings, type annotations) |
| **spec-review** | Specification review protocol |
| **adr-backfill** | Backfill missing Architecture Decision Records from git history |
| **lesson-capture** | Capture operational lessons from mistakes and discoveries |
| **liza-logs** | Analyze agent logs for token optimization and struggle detection |
| **context-engineering** | Analyze agent prompts and outputs for context quality and handoff fit |
| **have-you-considered** | Surface alternatives and different approaches |
| **feynman** | Explain complex ideas clearly (knowledge transfer) |
| **generic-subagent** | Context-efficient delegation to subagents |

Skills are composable — agents aren't locked to their role's skills.
A coder hitting a bug loads the debugging skill. A reviewer with structural concerns
loads the architecture review skill.

### Circuit Breaker

Circuit-breaker detection uses anomalies and planning task review evidence to escalate systemic failures before they cascade:

| Pattern | Threshold | Severity |
|---------|-----------|----------|
| Retry cluster | ≥3 retry loops with similar errors | ARCHITECTURE_FLAW |
| Debt accumulation | ≥3 trade-offs creating tech debt | SCOPE_FLAW |
| Assumption cascade | Same assumption violated across 2+ tasks | SPEC_FLAW |
| Spec gap cluster | Multiple tasks hitting same ambiguity | SPEC_FLAW |
| Workaround pattern | 2+ workarounds with same root cause | ARCHITECTURE_FLAW |
| External service outage | 2+ tasks blocked by same service | EXTERNAL_DEPENDENCY |
| planning_review_churn | four or more planning rejection cycles; `MERGED` tasks remain eligible | PLANNING_CONVERGENCE_DEGRADED |

When triggered: sprint pauses, markdown report generated with evidence, human decision required.
The circuit breaker is observation-only — it never proposes solutions, modifies code,
or continues execution.

### Anomaly Logging

Agents must log anomalies at time of occurrence:

- **Coders**: `retry_loop` (>2 iterations), `trade_off`, `spec_ambiguity`, `external_blocker`, `assumption_violated`
- **Reviewers**: `retry_loop`, `scope_deviation`, `workaround`, `debt_created`, `assumption_violated`, `spec_changed`, `reviewer_loop`

Type-specific detail requirements enforced in code (e.g., `retry_loop` needs `count` + `error_pattern`).

### Orchestrator Wake Triggers

The orchestrator primarily waits on fsnotify change events, with polling fallback
when the watcher fails or errors. It wakes on specific conditions, in priority order:

1. **INITIAL_PLANNING**: no tasks exist yet
2. **BLOCKED_TASKS**: actionable blocked tasks awaiting escalation. When one of them waits on a
   planner's untransitioned output, PLANNING_COMPLETE takes this rank instead (at most one
   transition attempt per planner per sprint), since a BLOCKED_TASKS turn cannot checkpoint
3. **HYPOTHESIS_EXHAUSTED**: 2+ coders failed the same task
4. **IMMEDIATE_DISCOVERY**: new discoveries not yet converted to tasks
5. **HUMAN_NOTE**: operator notes no completed turn has rendered or consumed
6. **PLANNING_COMPLETE**: all planned tasks terminal, and merged planning tasks have unconsumed output
   (pipeline transitions execute via `liza proceed <task> <transition>` for a single task,
   or automatically in batch after checkpoint and human `liza resume`)
7. **SPRINT_COMPLETE**: all planned tasks terminal (no unconsumed planning output remains)

Re-wake loop prevention: if sprint is already CHECKPOINT or COMPLETED, SPRINT_COMPLETE is suppressed.
Two consecutive BLOCKED_TASKS turns that leave the blocker state unchanged raise one
`BLOCKED_TASKS UNRESOLVED` alert.

### Crash Recovery

When things go wrong despite all layers:

- `liza recover-agent <id>`: releases all tasks claimed by dead agent, clears registration
- `liza recover-task <id>`: releases stale task claims while preserving coherent task work by default; `--fresh` is required to discard branch/worktree state, and `BLOCKED` tasks remain blocked until `unblock-task`
- Both produce audit trail entries

### Sprint Governance

- Sprint ends when: all tasks terminal, all non-terminal BLOCKED, deadline reached,
  circuit breaker triggered, or human requests checkpoint
- Hard checkpoints require human response — agents remain paused indefinitely. Transition checkpoints
  require human response before downstream transition creation, while doer/reviewer work already
  available in the sprint may continue.
- System mode transitions enforced: RUNNING↔PAUSED, any→CIRCUIT_BREAKER_TRIPPED

### Specification System as Durable Memory

Agents are stateless — every restart is a new mind. Specs are the durable memory that survives:

- Specs are injected into agent bootstrap prompts — agents read shared understanding, not rediscover it
- The spec-first pipeline (epic planning → US writing → code planning → coding) front-loads understanding
  so agents don't discover requirements by failing tests

Errors caught in specs cost less than errors caught in code. The specification system
is itself a hardening measure — it reduces the surface area for misunderstanding.

---

## How the Layers Compose

No single layer is sufficient. Each addresses a different trust boundary:

| Threat | Layer 1 (Contract) | Layer 2 (Adversarial) | Layer 3 (Go Code) | Layer 4 (Git) | Layer 5 (Skills) |
|--------|---|---|---|---|---|
| Test corruption | T0.3 invariant | Reviewer catches | `HasTestFiles()` gate | — | Testing skill |
| Self-approval | State machine gate | Role separation | MCP middleware | Merge authority | — |
| Hallucinated success | T0.4 invariant | Reviewer verifies | SHA validation | Rebase-before-review | — |
| Scope creep | Rule 6, atomic intent | Reviewer rejects | Scope field on tasks | Worktree isolation | — |
| Infinite retry loops | Stop triggers | Iteration limits | Exit-42 detection | — | Circuit breaker |
| Race conditions | — | — | CAS merges, 3-phase claim | Worktree isolation | — |
| Dead agents | — | — | Lease expiry | — | Crash recovery |
| Systemic failures | — | — | — | — | Circuit breaker |
| Misunderstood requirements | DoR, assumption budget | Reviewer checks spec | — | — | Spec system |
| Code quality drift | — | Reviewer P0–P3 checklist | — | — | Clean code, quality assessment |
| Secret exposure | T0.5 invariant | — | Secret masking | — | — |
| Path traversal | — | — | Task ID validation | Deterministic worktree paths | — |
| Single-reviewer blind spots | — | Quorum review | Quorum enforcement | — | — |
| Shared model blind spots | — | Reviewer diversity | Provider filtering | — | — |
| Circular dependencies | — | — | SCC detection, dep validation | — | — |

The contract shapes behavior. The adversarial architecture prevents conflicts of interest.
The Go code enforces what agents cannot circumvent. Git isolation prevents interference.
Skills and observability catch what slips through.

Together, they leave no gap. The blank cells aren't oversights — they're where one layer is already sufficient.

---

## Leading by Example: Liza's Own Engineering Practices

The measures above travel with Liza to any project. The practices below are how Liza's own
codebase is built — they demonstrate the standard and are encouraged for user projects,
but they are not part of Liza's enforcement machinery.

**One data point worth noting: Liza is written entirely by Liza. Its author has never written a single line of Go.**

### Pre-Commit Pipeline

Liza's repo has 22 pre-commit hooks. The contract requires agents to pass pre-commit
before claiming done — but the specific hooks are project-configured, not Liza-imposed.

| Category | Hooks |
|----------|-------|
| **Go quality** | `go-fmt`, `goimports`, `go-vet`, `staticcheck`, `go-mod-tidy` |
| **Python quality** | `ruff` (lint), `ruff-format`, `mypy`, `debug-statements` |
| **Duplicate detection** | `jscpd` — counters the tendency of coding agents to duplicate code |
| **Commit hygiene** | `commitizen` (conventional commits), `check-merge-conflict` |
| **File format** | `check-yaml`, `check-toml`, `check-json`, `end-of-file-fixer`, `trailing-whitespace`, `forbid-crlf`, `remove-crlf` |
| **Project-specific** | `check-testhelpers`, `check-useless-excludes` |

### Dependency Minimalism

4 direct Go dependencies: fsnotify, flock, cobra, yaml.v3. Radical minimalism reduces
attack surface, transitive risk, and supply chain exposure.

### Testing Culture

- **2.6:1 test-to-production ratio** (~76k test LOC / ~29k production LOC)
- **Race detection** enabled by default in CI
- **Table-driven tests** with `t.Run()` subtests throughout
- **134 specification files** including **53 ADRs** documenting decisions, constraints, and context

### System Invariants

[INVARIANTS.md](../INVARIANTS.md) codifies 80+ properties that must always hold in Liza itself,
organized across 15 domains: system integrity, epistemic integrity, task state machine, agent identity,
concurrency, review & approval, worktree management, scope & discovery, security, git protocol,
mode-specific rules, sprint governance, handoff, anomaly logging, and process invariants.

Each invariant specifies what it protects against and where it's enforced (contract, spec, or code).
Compliance is enforced at runtime via `GUARDRAILS.md` (G1.2), which requires agents to check
the [Protection Matrix](../INVARIANTS.md#cross-reference-protection-matrix) when a change's blast
radius intersects a listed threat category.
