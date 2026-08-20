# Architectural Issues

Persistent record of issues identified by architectural analysis skills.

**Last verified:** 2026-08-20 against the pending worktree.

**Skills that contribute here:**
- `systemic-thinking` — Systemic coherence and risk analysis
- `software-architecture-review` — Code-level architectural patterns and smells

## Update Policy

1. Keep unresolved concerns in their thematic sections (load-bearing, tensions, smells, etc.).
2. When an issue is fixed, record it in `Completed Fixes` and `Fixed (Traceability)` with commit references.
3. Do not delete resolved issues from this document without preserving traceability metadata.
4. If a resolved issue is removed from an active section, add/update its `Fixed (Traceability)` entry in the same change.
5. `Fix Details` keeps the long-form rationale; `Fixed (Traceability)` is the canonical index for historical closure.
6. Keep the **Open Issues Summary** table in sync when adding, resolving, or re-prioritizing issues.
7. This document is the sole lifecycle authority for architectural findings. Reviews and assessments may summarize or prioritize a finding, but they must link its anchor here; open/resolved status changes here.

## Table of Contents

- [Update Policy](#update-policy)
- [Open Issues Summary](#open-issues-summary)
- [Structural Load-Bearing Elements](#structural-load-bearing-elements)
  - [Mode Selection Trigger Coupled to Prompt Lexeme](#mode-selection-trigger-coupled-to-prompt-lexeme)
- [Systemic Tensions](#systemic-tensions)
  - [Role-Boundary Severity Drift](#role-boundary-severity-drift)
  - [Merge Execution Authority Split](#merge-execution-authority-split)
  - [Sprint Completion Signal Diverges from Active Scope](#sprint-completion-signal-diverges-from-active-scope)
  - [Prompts Layer Imports Business Logic](#prompts-layer-imports-business-logic)
  - [Commands Layer Imports Agent Runtime](#commands-layer-imports-agent-runtime)
  - [Cross-Pair Knowledge Required by Single-Pair Reviewers](#cross-pair-knowledge-required-by-single-pair-reviewers)
  - [Status Vocabulary Partition Between Go Constants and Declarative Pipeline](#status-vocabulary-partition-between-go-constants-and-declarative-pipeline)
- [Feedback Loops](#feedback-loops)
  - [Contract Complexity vs Context Pressure](#contract-complexity-vs-context-pressure)
  - [Systemic Discovery Channel Has Two Loss Modes](#systemic-discovery-channel-has-two-loss-modes)
  - [Coverage Basis Is Not Reproducible by Repository Tooling](#coverage-basis-is-not-reproducible-by-repository-tooling)
  - [Issue Registry Resolution Drift](#issue-registry-resolution-drift)
  - [Contract-Driven Safety vs Structural Enforcement Asymptote](#contract-driven-safety-vs-structural-enforcement-asymptote)
- [Assumptions](#assumptions)
  - [Spec Maturity Dependency](#spec-maturity-dependency)
  - [Well-Formed Blackboard State](#well-formed-blackboard-state)
  - [Single-Goal Data Model Constrains Applicability](#single-goal-data-model-constrains-applicability)
- [Stress Points](#stress-points)
  - [Filesystem/Git I/O Contention](#filesystemgit-io-contention)
  - [Manual Sprint Transitions as Scaling Bottleneck](#manual-sprint-transitions-as-scaling-bottleneck)
- [Fragility](#fragility)
  - [Cross-Script State Mutation](#cross-script-state-mutation)
  - [File-Based Spec References Without Version Anchors](#file-based-spec-references-without-version-anchors)
  - [State Validation Composition Gap](#state-validation-composition-gap)
  - [SetTaskOutput spec_ref Validation Gap](#settaskoutput-spec_ref-validation-gap)
  - [INVARIANTS.md §7 Clean Worktree Not Enforced at Submission](#invariantsmd-7-clean-worktree-not-enforced-at-submission)
  - [INVARIANTS.md §6 Provider Diversity Not Enforced at Verdict Time](#invariantsmd-6-provider-diversity-not-enforced-at-verdict-time)
  - [Custom Roles Render Empty Prompt Blocks](#custom-roles-render-empty-prompt-blocks)
  - [Raw Git Calls Bypass gitenv Hardening](#raw-git-calls-bypass-gitenv-hardening)
  - [Agent Lease Renewal Ignores Configured Lease Duration](#agent-lease-renewal-ignores-configured-lease-duration)
  - [Provider Support Crosses the Catalog Boundary](#provider-support-crosses-the-catalog-boundary)
  - [Canonical Provider Identity Lost Before Policy Enforcement](#canonical-provider-identity-lost-before-policy-enforcement)
- [Blind Spots](#blind-spots)
  - [Contract Effectiveness Self-Certification](#contract-effectiveness-self-certification)
  - [Initialization Completion Unverifiable](#initialization-completion-unverifiable)
  - [Circuit Breaker Depends on Participant Reporting](#circuit-breaker-depends-on-participant-reporting)
  - [No Source Type for Pre-Implementation Spec Findings](#no-source-type-for-pre-implementation-spec-findings)
  - [Prompt-Build-to-Execution State Drift](#prompt-build-to-execution-state-drift)
  - [No Feedback Signal for Specification Quality](#no-feedback-signal-for-specification-quality)
  - [No Reverse Data Channel in Inter-Pair Transitions](#no-reverse-data-channel-in-inter-pair-transitions)
  - [Retrospective Findings Don't Feed Forward to Next Sprint](#retrospective-findings-dont-feed-forward-to-next-sprint)
  - [Sprint Metrics Lossy at Sprint Boundary](#sprint-metrics-lossy-at-sprint-boundary)
  - [Index Discovery Failures Are Unobservable](#index-discovery-failures-are-unobservable)
  - [Destructive-DB Break-Glass Issued and Consumed by the Same Actor Class](#destructive-db-break-glass-issued-and-consumed-by-the-same-actor-class)
- [Trajectory](#trajectory)
  - [Blackboard Growth Without Pruning](#blackboard-growth-without-pruning)
  - [Role Addition Accelerates Contract Complexity Pressure](#role-addition-accelerates-contract-complexity-pressure)
  - [Anomaly Detail Validation Incomplete](#anomaly-detail-validation-incomplete)
  - [Spec Corpus Lacks Lifecycle Management](#spec-corpus-lacks-lifecycle-management)
  - [Metrics Collection Without Query Interface](#metrics-collection-without-query-interface)
  - [No Query Layer](#no-query-layer)
  - [Ownership Recovery Bound to Process-Shaped Liveness Evidence](#ownership-recovery-bound-to-process-shaped-liveness-evidence)
- [Cascades](#cascades)
  - [Sub-Pipeline Expansion Multiplies Every Existing Issue](#sub-pipeline-expansion-multiplies-every-existing-issue)
  - [Fan-Out Amplifies Decomposition Errors Across Pipeline Stages](#fan-out-amplifies-decomposition-errors-across-pipeline-stages)
- [Accepted v1 Limitations](#accepted-v1-limitations)
  - [Orchestrator as Single Semantic Interpreter](#orchestrator-as-single-semantic-interpreter)
  - [Supervisor as Single Correctness Gate](#supervisor-as-single-correctness-gate)
  - [Spec Completeness vs Reality](#spec-completeness-vs-reality)
  - [Code Reviewer Structural Accountability Gap](#code-reviewer-structural-accountability-gap)
  - [Restart/Lease Churn Under Load](#restartlease-churn-under-load)
  - [Human Availability as Bottleneck](#human-availability-as-bottleneck)
  - [Dual Contract Delivery Paths](#dual-contract-delivery-paths)
  - [Kill Switch Granularity](#kill-switch-granularity)
- [Structural Debt](#structural-debt)
  - [Decompose proceed.go (1,500 LOC)](#decompose-proceedgo-1500-loc)
  - [Decompose init.go (1,268 LOC)](#decompose-initgo-1268-loc)
  - [Duplicate Initialization Implementations](#duplicate-initialization-implementations)
  - [Provider Stop-Signal Recovery Has Multiple Owners](#provider-stop-signal-recovery-has-multiple-owners)
  - [Worktree Intelligence Refresh Has Multiple Owners](#worktree-intelligence-refresh-has-multiple-owners)
  - [Decompose supervisor.go (1,129 LOC)](#decompose-supervisorgo-1129-loc)
  - [Decompose watch.go (1,407 LOC)](#decompose-watchgo-1407-loc)
  - [Split embedded.go by Artifact Family (1,530 LOC)](#split-embeddedgo-by-artifact-family-1530-loc)
  - [CI Does Not Enforce Cross-Language Quality Gates](#ci-does-not-enforce-cross-language-quality-gates)
  - [Control-Flow Vocabulary Bypasses Domain Ownership](#control-flow-vocabulary-bypasses-domain-ownership)
  - [Python Skill Utilities Lack Quality Parity](#python-skill-utilities-lack-quality-parity)
- [Completed Fixes](#completed-fixes)
- [Fixed (Traceability)](#fixed-traceability)
- [Fix Details](#fix-details)

## Open Issues Summary

| Priority | Category | Issue |
|----------|----------|-------|
| **high** | LOAD-BEARING | [Mode Selection Trigger Coupled to Prompt Lexeme](#mode-selection-trigger-coupled-to-prompt-lexeme) |
| **high** | LOAD-BEARING | [Orchestrator as Single Semantic Interpreter](#orchestrator-as-single-semantic-interpreter) |
| **high** | LOAD-BEARING | [Supervisor as Single Correctness Gate](#supervisor-as-single-correctness-gate) |
| **high** | TENSION | [Role-Boundary Severity Drift](#role-boundary-severity-drift) |
| **high** | TENSION | [Code Reviewer Structural Accountability Gap](#code-reviewer-structural-accountability-gap) |
| **high** | FEEDBACK | [Contract Complexity vs Context Pressure](#contract-complexity-vs-context-pressure) |
| **high** | FEEDBACK | [Systemic Discovery Channel Has Two Loss Modes](#systemic-discovery-channel-has-two-loss-modes) |
| **high** | FEEDBACK | [Coverage Basis Is Not Reproducible by Repository Tooling](#coverage-basis-is-not-reproducible-by-repository-tooling) |
| **high** | FRAGILITY | [Dual Contract Delivery Paths](#dual-contract-delivery-paths) |
| **high** | FEEDBACK | [Contract-Driven Safety vs Structural Enforcement Asymptote](#contract-driven-safety-vs-structural-enforcement-asymptote) |
| **high** | CASCADE | [Sub-Pipeline Expansion Multiplies Every Existing Issue](#sub-pipeline-expansion-multiplies-every-existing-issue) |
| **high** | BLIND SPOT | [No Feedback Signal for Specification Quality](#no-feedback-signal-for-specification-quality) |
| **high** | BLIND SPOT | [No Reverse Data Channel in Inter-Pair Transitions](#no-reverse-data-channel-in-inter-pair-transitions) |
| **high** | CASCADE | [Fan-Out Amplifies Decomposition Errors Across Pipeline Stages](#fan-out-amplifies-decomposition-errors-across-pipeline-stages) |
| **high** | TENSION | [Cross-Pair Knowledge Required by Single-Pair Reviewers](#cross-pair-knowledge-required-by-single-pair-reviewers) |
| **high** | TENSION | [Status Vocabulary Partition Between Go Constants and Declarative Pipeline](#status-vocabulary-partition-between-go-constants-and-declarative-pipeline) |
| **high** | BLIND SPOT | [Destructive-DB Break-Glass Issued and Consumed by the Same Actor Class](#destructive-db-break-glass-issued-and-consumed-by-the-same-actor-class) |
| **medium** | BLIND SPOT | [Retrospective Findings Don't Feed Forward to Next Sprint](#retrospective-findings-dont-feed-forward-to-next-sprint) |
| **medium** | STRESS POINT | [Manual Sprint Transitions as Scaling Bottleneck](#manual-sprint-transitions-as-scaling-bottleneck) |
| **medium** | STRESS POINT | [Filesystem/Git I/O Contention](#filesystemgit-io-contention) |
| **medium** | TENSION | [Merge Execution Authority Split](#merge-execution-authority-split) |
| **medium** | TENSION | [Sprint Completion Signal Diverges from Active Scope](#sprint-completion-signal-diverges-from-active-scope) |
| **medium** | TENSION | [Spec Completeness vs Reality](#spec-completeness-vs-reality) |
| **medium** | FEEDBACK | [Issue Registry Resolution Drift](#issue-registry-resolution-drift) |
| **medium** | FEEDBACK | [Restart/Lease Churn Under Load](#restartlease-churn-under-load) |
| **medium** | ASSUMPTION | [Well-Formed Blackboard State](#well-formed-blackboard-state) |
| **medium** | ASSUMPTION | [Human Availability as Bottleneck](#human-availability-as-bottleneck) |
| **medium** | TRAJECTORY | [Ownership Recovery Bound to Process-Shaped Liveness Evidence](#ownership-recovery-bound-to-process-shaped-liveness-evidence) |
| **medium** | FRAGILITY | [Cross-Script State Mutation](#cross-script-state-mutation) |
| **medium** | FRAGILITY | [File-Based Spec References Without Version Anchors](#file-based-spec-references-without-version-anchors) |
| **medium** | FRAGILITY | [SetTaskOutput spec_ref Validation Gap](#settaskoutput-spec_ref-validation-gap) |
| **medium** | FRAGILITY | [INVARIANTS.md §7 Clean Worktree Not Enforced at Submission](#invariantsmd-7-clean-worktree-not-enforced-at-submission) |
| **medium** | FRAGILITY | [INVARIANTS.md §6 Provider Diversity Not Enforced at Verdict Time](#invariantsmd-6-provider-diversity-not-enforced-at-verdict-time) |
| **medium** | FRAGILITY | [Custom Roles Render Empty Prompt Blocks](#custom-roles-render-empty-prompt-blocks) |
| **medium** | FRAGILITY | [Raw Git Calls Bypass gitenv Hardening](#raw-git-calls-bypass-gitenv-hardening) |
| **medium** | FRAGILITY | [Agent Lease Renewal Ignores Configured Lease Duration](#agent-lease-renewal-ignores-configured-lease-duration) |
| **medium** | FRAGILITY | [Provider Support Crosses the Catalog Boundary](#provider-support-crosses-the-catalog-boundary) |
| **medium** | FRAGILITY | [Canonical Provider Identity Lost Before Policy Enforcement](#canonical-provider-identity-lost-before-policy-enforcement) |
| **medium** | BLIND SPOT | [Contract Effectiveness Self-Certification](#contract-effectiveness-self-certification) |
| **medium** | BLIND SPOT | [Initialization Completion Unverifiable](#initialization-completion-unverifiable) |
| **medium** | BLIND SPOT | [Circuit Breaker Depends on Participant Reporting](#circuit-breaker-depends-on-participant-reporting) |
| **medium** | BLIND SPOT | [Sprint Metrics Lossy at Sprint Boundary](#sprint-metrics-lossy-at-sprint-boundary) |
| **low** | ASSUMPTION | [Spec Maturity Dependency](#spec-maturity-dependency) |
| **low** | ASSUMPTION | [Single-Goal Data Model Constrains Applicability](#single-goal-data-model-constrains-applicability) |
| **low** | BLIND SPOT | [No Source Type for Pre-Implementation Spec Findings](#no-source-type-for-pre-implementation-spec-findings) |
| **low** | BLIND SPOT | [Prompt-Build-to-Execution State Drift](#prompt-build-to-execution-state-drift) |
| **low** | BLIND SPOT | [Index Discovery Failures Are Unobservable](#index-discovery-failures-are-unobservable) |
| **low** | TRAJECTORY | [Blackboard Growth Without Pruning](#blackboard-growth-without-pruning) |
| **low** | TRAJECTORY | [Role Addition Accelerates Contract Complexity Pressure](#role-addition-accelerates-contract-complexity-pressure) |
| **low** | TRAJECTORY | [Anomaly Detail Validation Incomplete](#anomaly-detail-validation-incomplete) |
| **low** | TRAJECTORY | [Spec Corpus Lacks Lifecycle Management](#spec-corpus-lacks-lifecycle-management) |
| **low** | TRAJECTORY | [Metrics Collection Without Query Interface](#metrics-collection-without-query-interface) |
| **low** | TENSION | [Prompts Layer Imports Business Logic](#prompts-layer-imports-business-logic) |
| **low** | TENSION | [Commands Layer Imports Agent Runtime](#commands-layer-imports-agent-runtime) |
| **low** | TRAJECTORY | [No Query Layer](#no-query-layer) |
| **low** | ACCEPTED v1 | [Kill Switch Granularity](#kill-switch-granularity) |
| **low** | FRAGILITY | [State Validation Composition Gap](#state-validation-composition-gap) |
| **medium** | STRUCTURAL DEBT | [Decompose proceed.go (1,500 LOC)](#decompose-proceedgo-1500-loc) |
| **medium** | STRUCTURAL DEBT | [Decompose init.go (1,268 LOC)](#decompose-initgo-1268-loc) |
| **medium** | STRUCTURAL DEBT | [Decompose supervisor.go (1,129 LOC)](#decompose-supervisorgo-1129-loc) |
| **medium** | STRUCTURAL DEBT | [Duplicate Initialization Implementations](#duplicate-initialization-implementations) |
| **medium** | STRUCTURAL DEBT | [Provider Stop-Signal Recovery Has Multiple Owners](#provider-stop-signal-recovery-has-multiple-owners) |
| **medium** | STRUCTURAL DEBT | [Worktree Intelligence Refresh Has Multiple Owners](#worktree-intelligence-refresh-has-multiple-owners) |
| **medium** | STRUCTURAL DEBT | [Decompose watch.go (1,407 LOC)](#decompose-watchgo-1407-loc) |
| **medium** | STRUCTURAL DEBT | [Split embedded.go by Artifact Family (1,530 LOC)](#split-embeddedgo-by-artifact-family-1530-loc) |
| **medium** | CROSS-CUTTING | [CI Does Not Enforce Cross-Language Quality Gates](#ci-does-not-enforce-cross-language-quality-gates) |
| **medium** | CROSS-CUTTING | [Control-Flow Vocabulary Bypasses Domain Ownership](#control-flow-vocabulary-bypasses-domain-ownership) |
| **medium** | SUBSYSTEM CONCERN | [Python Skill Utilities Lack Quality Parity](#python-skill-utilities-lack-quality-parity) |

**Counts:** 17 high, 36 medium, 15 low — 68 open issues total. *(2026-08-20: ADR-0113 and the merged sliced-integration acceptance tests resolved the integration-closure issue. The 2026-07-25 review follow-up added the non-reproducible coverage basis; the Adversarial architecture pass added the canonical-provider-identity concern; a systemic-thinking pass over `specs/architecture/` added the status-vocabulary partition, the destructive-DB break-glass authority, and the process-shaped liveness evidence behind ownership recovery — the last reframed from ASSUMPTION to TRAJECTORY after ADR-0085:68 was found to record the exclusion deliberately. A MAS-focused systemic pass added the discovery-channel, integration-closure, and circuit-breaker sensing issues, and extended Dual Contract Delivery Paths to cover the project pipeline control plane.)*

---

## Structural Load-Bearing Elements

Single points of failure with no redundancy or validation mechanism.

### Mode Selection Trigger Coupled to Prompt Lexeme

**Skill:** systemic-thinking
**Category:** LOAD-BEARING

**Issue:** Mode selection in CORE depends on detecting specific bootstrap wording (`"You are a Liza ... agent"` for Liza mode, `MODE: SUBAGENT` for subagent mode). The prompt template (`base_prompt.tmpl`) currently generates matching wording (`"You are a Liza {{.Role}} agent"`), so there is no active mismatch. However, because gate semantics and approval behavior branch entirely on this lexical detection, the coupling is load-bearing despite being outside the blackboard/state machine. A template edit, prompt builder refactor, or alternative CLI integration that changes the wording would silently change mode without any structural guard.

**Implication:** Mode detection correctness depends on convention alignment between two independently maintained artifacts (CORE.md detection table and prompt templates). No compile-time or runtime check validates this alignment.

**Current mitigation:** Prompt template output currently matches CORE.md detection patterns. Builder tests (`builder_test.go`) assert the expected prefix, providing regression coverage.

**Future options:**
- Add explicit mode declaration outside free-text prompt (e.g., structured field/environment variable)
- Add startup self-check that fails fast when expected mode and detected mode diverge
- Record detected mode in blackboard state for runtime observability

---

## Systemic Tensions

Design contradictions that create structural friction.

### Role-Boundary Severity Drift

**Skill:** systemic-thinking
**Category:** TENSION

**Issue:** Vision-level contract text classifies role-boundary violations as Tier 0 (contract termination), while the active Multi-Agent mode contract classifies the same class of violations as Tier 1. The same behavioral breach therefore has two incompatible severities across authoritative artifacts.

**Implication:** Violation handling semantics become document-dependent, so recovery behavior can vary by which artifact an agent or operator treats as canonical.

**Current mitigation:** None structural; conflict is resolved ad hoc by whichever document is consulted first.

**Future options:**
- Align role-boundary severity to a single tier across all mode and vision artifacts
- Add consistency checks in contract maintenance workflow for severity-classified rules
- Publish one canonical severity table referenced by all contracts

### Merge Execution Authority Split

**Skill:** systemic-thinking
**Category:** TENSION

**Issue:** Task/worktree protocols state that Code Reviewer executes merge on approval, while role/supervision architecture defines Code Reviewer as read-only and supervisor as the merge executor. Merge authority and operational responsibility are split across documents.

**Implication:** Integration ownership is ambiguous, weakening accountability and making post-incident reconstruction of merge authority less reliable.

**Current mitigation:** Runtime flow appears supervisor-driven in architecture documents, so practical execution tends to converge despite specification drift.

**Future options:**
- Normalize all protocol docs to one merge authority model
- Record merge executor identity explicitly in task history for auditability
- Add validation/docs linting to flag authority contradictions across artifacts

### Sprint Completion Signal Diverges from Active Scope

**Skill:** systemic-thinking
**Category:** TENSION

**Issue:** Sprint governance allows sprint completion when only the original planned task list is terminal, while replacement tasks created by rescoping may still be active. The completion signal is therefore cadence-based rather than work-closure-based.

**Implication:** A sprint can report completion while unresolved implementation risk remains in flight under replacement tasks.

**Current mitigation:** Pipeline-created children (from `ExecuteAvailableTransitions`, executed by orchestrator PreWork after planning checkpoint is resumed) are automatically added to `sprint.scope.planned[]`, preventing premature sprint completion when planning tasks transition to coding tasks. Orchestrator-created replacement tasks (rescoping) are still NOT automatically added — humans must manually update scope or wait for all active tasks. Sprint governance protocol (`sprint-governance.md`) documents this as expected behavior.

**Remaining gap:** Orchestrator rescoping (SUPERSEDED → replacement tasks) does not auto-update scope. This is intentional — rescoping is a human-guided decision, and auto-inclusion could mask scope creep.

**Future options:**
- Add an alternate completion criterion based on all active (planned + replacement) tasks
- Separate cadence checkpoint status from true work-closure status

### Prompts Layer Imports Business Logic

**Skill:** software-architecture-review
**Category:** TENSION
**Coupled with:** [No Query Layer](#no-query-layer)

**Issue:** `internal/prompts` imports `internal/ops` for pipeline and lifecycle queries used during prompt assembly: `LoadDetectionContext`, `ManyToOneTransitionInfo`, `IsPlanningPair`, `IsTransitionCycleBlocked`, `CountReadyManyToOneCohorts`, and `IsPlanningCompleteEligible`. This creates a `prompts → ops` dependency — the prompt-building layer reaches into the business-logic layer to derive context instead of receiving already-resolved data from its caller.

**Implication:** Prompt assembly is coupled to ops implementation details. Changes to planning eligibility, transition-cycle detection, or cohort readiness can require coordinated changes in both packages, and `prompts` is harder to test in isolation.

**Current mitigation:** These dependencies are read-only queries, so they create no mutation-path risk.

**Future options:**
- Have the caller (agent) resolve these values and pass them into prompt builder as parameters
- Extract the query functions to a shared query package that both `ops` and `prompts` can import
- Accept the coupling and document `prompts → ops` as an intentional read-path shortcut

### Commands Layer Imports Agent Runtime

**Skill:** software-architecture-review
**Category:** TENSION
**Coupled with:** [No Query Layer](#no-query-layer)

**Issue:** Three production command files — `status.go`, `repair_agent_pool.go`, and `resume.go` — import `internal/agent`. The CLI presentation/control layer therefore depends directly on runtime implementation for wake-trigger diagnosis and agent-pool lifecycle behavior.

**Implication:** The CLI layer has a compile-time dependency on agent internals. Runtime changes can ripple into command implementations, and query/control responsibilities do not have a stable boundary.

**Current mitigation:** The status dependency is a pure query; the repair and resume dependencies are explicit lifecycle operations rather than hidden mutations.

**Future options:**
- Move `DetectOrchestratorWakeTriggers` to `models/diagnostics.go` (already partially serves as query home)
- Define an application/service boundary for agent-pool repair and resume operations
- Accept and document `commands` as the runtime control adapter

### Cross-Pair Knowledge Required by Single-Pair Reviewers

**Skill:** systemic-thinking
**Category:** TENSION
**Related:** [No Feedback Signal for Specification Quality](#no-feedback-signal-for-specification-quality), [No Reverse Data Channel in Inter-Pair Transitions](#no-reverse-data-channel-in-inter-pair-transitions)

**Issue:** Reviewers validate both the doer's artifact and its `output[]` decomposition into downstream tasks. Artifact review is within-domain ("is this plan sound?"). Decomposition review is cross-domain ("will these entries make good inputs for the next pair?"). The Epic Plan Reviewer must understand what makes a good US Writer input; the Code Plan Reviewer must understand what makes a good Coder input. This knowledge remains embedded in reviewer prompt checklists, and the reviewer is the final quality gate before a transition fans out child tasks.

**Implication:** The reviewer's decomposition judgment is the single most consequential quality gate in the pipeline — it controls fan-out scope and shape — yet it operates on cross-pair knowledge that the role's structural position doesn't naturally provide.

**Current mitigation:** The deployed reviewer prompt templates contain decomposition-specific checklists and canonical validation rules. These improve consistency but do not provide a reverse quality signal from downstream execution.

**Future options:**
- Downstream pair signals decomposition quality back to the reviewer pair (structural feedback, not just anomaly logging)
- Decomposition validation as a separate gate from artifact review (two verdicts per submission)
- Iteration cap calibration per role-pair to bound the cost of bad decomposition before detection

### Status Vocabulary Partition Between Go Constants and Declarative Pipeline

**Skill:** systemic-thinking
**Category:** TENSION
**Related:** [Sub-Pipeline Expansion Multiplies Every Existing Issue](#sub-pipeline-expansion-multiplies-every-existing-issue), [Sprint Metrics Lossy at Sprint Boundary](#sprint-metrics-lossy-at-sprint-boundary), [Sprint Completion Signal Diverges from Active Scope](#sprint-completion-signal-diverges-from-active-scope)

**Issue:** The role vocabulary was deliberately unified — ADR-0024 introduced dual role names as acknowledged debt, ADR-0045 made roles derived from YAML with no hardcoded constants, ADR-0047 paid the debt off and deleted the conversion machinery. The status vocabulary never received that treatment and now carries the same duality in a worse form. `internal/models/task.go:104-127` binds generic Go identifiers to code-pipeline-specific values: `TaskStatusReady = "DRAFT_CODE"`, `TaskStatusReadyForReview = "CODE_TO_REVIEW"`, `TaskStatusApproved = "CODE_APPROVED"`. Meanwhile `internal/embedded/pipeline.yaml` carries 59 distinct status literals across all role-pairs, and `internal/pipeline/resolver.go:63-99` converts them with a raw `models.TaskStatus(s.Initial)` — no validation against the constant set. Two paths therefore coexist for the same question: resolver lookups that follow the declarative pipeline, and constant comparisons that silently mean "the coding pipeline only." `internal/ops/release_claim.go:41-54` contains both. The consequence is already visible in generically-named surfaces: `internal/models/sprint.go:118-149` computes velocity, in-progress, and blocked counts against code-only constants, and `internal/ops/sprint_checkpoint.go:147-157` renders a table labeled MERGED/APPROVED/IMPLEMENTING/REJECTED that counts code-only values — so a task sitting in `ARCHITECTURE_APPROVED`, `CODING_PLAN_TO_REVIEW`, or `US_TO_REVIEW` is neither in-progress nor done in the system's own reporting. The architecture states the pipeline is declarative; the status layer is a partial hardcoded mirror of it.

**Implication:** Each new sub-pipeline extends the region of the state space that generically-named Go surfaces cannot see, so the declarative pipeline's reach shrinks relative to its stated scope rather than growing.

**Current mitigation:** Resolver-based accessors (`InitialStatus`, `ExecutingStatus`, `SubmittedStatus`, `ReviewingStatus`, `ApprovedStatus`) exist and are used on the claim/release path; `ComputeSprintMetricsWithTerminalStates` and `AllPlannedTasksTerminalWith` accept pipeline-supplied terminal states. The partition is partially bridged, not closed.

**Future options:**
- Derive the status constant set from the pipeline YAML at load time, as ADR-0045 did for roles
- Validate YAML status literals against a known set at config-validation time so unknown statuses fail loudly
- Rename code-pipeline constants to match their values, making the remaining hardcoded scope legible at every call site

---

## Feedback Loops

Self-reinforcing patterns that can amplify failures.

### Contract Complexity vs Context Pressure

**Skill:** systemic-thinking
**Category:** FEEDBACK

**Issue:** The contract is the mechanism that suppresses agent failure modes. It competes for the same resource agents need to do work: context tokens. The current baseline is 778 lines of CORE.md, 200 lines of MULTI_AGENT_MODE.md, and 252 lines of AGENT_TOOLS.md — 1,230 lines before repository orientation, role-specific skills, blackboard state, or task specifications. The tier architecture and kernel appendix address degradation after it happens, but each new clause still consumes context that makes the remaining rules harder to retain.

**Implication:** The contract will hit a ceiling where adding another clause to prevent failure mode N+1 degrades compliance with clauses 1 through N, and no tier architecture can compensate because the contract must be loaded before tiers can be evaluated.

**Future options:**
- Contract compression (semantic deduplication, remove examples that models don't need)
- Conditional loading (only load clauses relevant to current role/task type)
- Structural enforcement replacing behavioral rules (move more rules into Go code, reducing contract size)
- Measure contract-to-work ratio across sessions to detect the ceiling empirically

### Systemic Discovery Channel Has Two Loss Modes

**Skill:** systemic-thinking
**Category:** FEEDBACK
**Related:** [Issue Registry Resolution Drift](#issue-registry-resolution-drift), [Cross-Script State Mutation](#cross-script-state-mutation)

**Issue:** The role contract declares every unresolved `systemic-thinking` discovery an Orchestrator wake trigger, while runtime detection wakes only for `urgency: immediate`. The skill assigns `deferred` urgency by default, so the normal systemic finding does not activate its declared consumer. When an immediate discovery is processed, the wake prompt permits `set-discovery-disposition <id> deferred` without first writing the issue registry, although the blackboard schema defines `deferred` as confirmation that the Orchestrator wrote to `ISSUES_FILE`. The disposition operation mutates only blackboard state.

**Implication:** MAS architectural risks can remain permanently untriaged or appear durably acknowledged when no durable record exists.

**Current mitigation:** Immediate discoveries do wake the Orchestrator, disposition values are validated, and the Git-tracked registry can be audited manually. These controls do not connect default systemic discoveries to a consumer or bind registry persistence to disposition.

**Future options:**
- Add an unresolved-systemic-discovery wake condition distinct from immediate implementation discoveries
- Make deferred disposition and registry persistence one recoverable operation
- Represent evaluation and persistence as separate states so incomplete disposition is observable

### Coverage Basis Is Not Reproducible by Repository Tooling

**Skill:** software-architecture-review
**Category:** FEEDBACK
**Related:** [Issue Registry Resolution Drift](#issue-registry-resolution-drift)

**Issue:** The repository's `make test` target generates `coverage.out` without `-coverpkg=./...`, while the architecture review's 80.7% result came from a separate cross-package run whose raw profiles and deduplication implementation were not retained. The profile path is gitignored, so the exact basis cannot be regenerated from repository tooling.

**Implication:** Coverage claims can look current after their evidence has disappeared, and repeated reviews must trust prose rather than execute a repository command to verify the same measurement.

**Direction:** Make the default coverage command produce the cross-package basis, retain a deterministic summarization method, and replace the dated historical summary with a freshly generated artifact.

### Issue Registry Resolution Drift

**Skill:** systemic-thinking
**Category:** FEEDBACK

**Issue:** The architectural issues registry is treated as the durable source of resolved-vs-open architectural risk, but its own resolution claims can diverge from live internal behavior. A prior example was the `submit-for-review` `commit_sha` item: the registry treated it as resolved while internal CLI/ops surfaces still required caller-provided SHA. That creates a reinforcing loop where planning and review work trusts the registry, then inherits stale assumptions, then perpetuates stale status.

**Implication:** Architectural debt tracking becomes self-invalidating: "resolved" no longer means the risk is absent in current runtime surfaces.

**Current mitigation:** Manual source verification during reviews can detect the mismatch, but only when a reviewer re-audits internals.

**Update:** The `submit-for-review` example was addressed on 2026-04-25 by allowing worktree-local commit refs (`HEAD`) and defaulting omitted refs to `HEAD`. The meta-risk remains: registry resolution claims need validation artifacts.

**Future options:**
- Add automated checks that verify each "resolved" entry against current code contracts
- Require a validation artifact (test/doc/assertion) link for every resolved architectural issue
- Add a `REGRESSED` status class to avoid binary resolved/unresolved drift

### Contract-Driven Safety vs Structural Enforcement Asymptote

**Skill:** systemic-thinking
**Category:** FEEDBACK
**Related:** [Contract Complexity vs Context Pressure](#contract-complexity-vs-context-pressure)

**Issue:** The architecture has two enforcement strategies operating simultaneously. The 778-line CORE.md suppresses agent failure modes through behavioral rules loaded into context. The Go binary (`liza`) enforces structural invariants through code (supervisor-assigned work, lease management, state-machine validation, and the `ops` layer). ADR-0011 and ADR-0030 explicitly chose structural enforcement over behavioral compliance. Yet the most critical safety properties — no fabrication, no test corruption, no unapproved state change — still require semantic judgment and cannot be fully reduced to runtime checks. Each new structural enforcement removes a mechanical rule from the contract, leaving a higher concentration of the hard-to-enforce behavioral residue.

**Implication:** The contract will asymptotically approach a core of pure behavioral rules that cannot be structurally enforced, and that residue is precisely the set the system was designed to solve.

**Current mitigation:** The tier architecture (Tier 0 hard invariants vs Tier 2-3 best-effort) explicitly acknowledges that some rules are non-negotiable and others degrade gracefully — but doesn't address the dynamic where the non-negotiable set becomes the entire contract.

**Future options:**
- Accept the asymptote explicitly: document which rules are permanently behavioral and invest in detection rather than prevention (e.g., post-hoc audit of fabrication)
- Structural proxies for semantic properties (e.g., require validation command output in commit metadata to structurally enforce "no unvalidated success")
- Adversarial testing to measure behavioral rule compliance under context pressure empirically

---

## Assumptions

Implicit dependencies that constrain system behavior.

### Spec Maturity Dependency

**Skill:** systemic-thinking
**Category:** ASSUMPTION

**Issue:** "Specs substantially complete before work" ties throughput to spec maturity and creates dependency on continuous human availability for spec evolution.

**Implication:** When specs incomplete or human constrained, throughput collapses rather than degrading gracefully.

**Addressed by:** BLOCKED resolution via `human_notes`.

### Well-Formed Blackboard State

**Skill:** systemic-thinking
**Category:** ASSUMPTION

**Issue:** CLI commands assume blackboard fields (current_task, review_lease_expires, integration_branch) are present and well-formed. Limited defensive handling for corrupted or partial state.

**Implication:** Single malformed entry can cascade into systemic stop conditions across all roles.

**Current mitigation:** `liza validate` checks invariants.

**Future options:**
- Schema validation on every state read
- Auto-repair for common corruption patterns
- Quarantine malformed entries rather than fail-stop

### Single-Goal Data Model Constrains Applicability

**Skill:** systemic-thinking
**Category:** ASSUMPTION

**Issue:** The active blackboard schema has exactly one `goal` section, one `sprint` section, and a flat `tasks` array. Completed sprints are preserved in full archive files and summarized in `sprint_history`, so the system now has historical memory. The remaining structural limit is one active goal and sprint at a time: concurrent goals, a visible multi-sprint backlog, and hierarchical task relationships cannot be represented in active state. Historical summaries also retain only a small subset of sprint metrics.

**Implication:** Liza remains structurally a single-goal-at-a-time system. Teams that need concurrent objectives or portfolio planning require a schema and query-model change rather than a configuration switch.

**Future options:**
- Document as explicit v1 limitation in vision and deployment docs
- Extend sprint history with planning-relevant metrics
- Goal array with per-goal task filtering
- Backlog section separate from active sprint scope

---

## Stress Points

Bottlenecks that emerge under load.

### Filesystem/Git I/O Contention

**Skill:** systemic-thinking
**Category:** STRESS POINT

**Issue:** Worktree creation, review assignment, and integration operations still funnel through one repository and filesystem. The former concurrent-merge corruption risk has been addressed: integration now uses working-tree-less `merge-tree`/`commit-tree` construction and compare-and-swap `update-ref` with bounded retries. The remaining concern is shared-resource pressure — concurrent worktree creation, object-database access, hooks, and retrying branch updates can saturate I/O or exhaust the bounded retry budget under load.

**Implication:** The primary failure mode is now throughput degradation or a recoverable integration failure, not silent branch corruption. It becomes relevant as concurrent agent and worktree counts grow.

**Current mitigation:** Agents operate in separate worktrees; integration updates are atomic and retry on concurrent ref changes.

**Future options:**
- Worktree pool pre-creation
- Instrument repository I/O and CAS retry exhaustion before adding serialization
- Queue only the operations shown by measurements to contend

### Manual Sprint Transitions as Scaling Bottleneck

**Skill:** systemic-thinking
**Category:** STRESS POINT
**Related:** [Human Availability as Bottleneck](#human-availability-as-bottleneck)

**Issue:** Manual/checkpoint transitions remain at trust boundaries in the deployed multi-stage topology. The configured pipeline contains 9 role-pairs, and a feature can traverse several reviewed stages before coding and integration. Human checkpoints are therefore structural rather than exceptional, so throughput is partly capped by response time at each manual boundary regardless of available agent capacity.

**Implication:** The system's throughput ceiling shifts from "agent capacity" toward "human gate frequency × pipeline depth" — adding more agents does not help when the bottleneck is response time at configured manual boundaries.

**Current mitigation:** The Planning Transition Gate reduces this from a two-step flow to a single checkpoint: the orchestrator checkpoints with `checkpoint_trigger: PLANNING_COMPLETE`, the human reviews and resumes, and child tasks are created automatically by the orchestrator's PreWork. This eliminates the separate `liza proceed` step for pipeline-configured transitions. Human review at sprint boundaries remains the trust mechanism.

**Future options:**
- Allow configurable auto-proceed for low-risk transitions (e.g., code-plan → coding when plan approval rate is high)
- Batch multiple pipeline transitions into single human review gate
- Async notification with timeout-based auto-proceed for non-critical pipelines

---

## Fragility

Partial failure modes with unclear recovery.

### Cross-Script State Mutation

**Skill:** systemic-thinking
**Category:** FRAGILITY

**Issue:** State mutation distributed across CLI commands (`liza claim-task`, `liza wt-merge`, `liza clear-stale-review-claims`) with shared transactional boundary via the Go binary's internal locking. Cross-command assumptions about state shape are type-checked at compile time.

**Implication:** Partial failure in any command can leave blackboard logically consistent but operationally stuck.

**Future options:**
- State machine validation after each operation
- Transaction log for rollback capability
- Centralized state mutation through single entry point

### File-Based Spec References Without Version Anchors

**Skill:** systemic-thinking
**Category:** FRAGILITY

**Issue:** The `spec_ref` field in tasks and goal uses file paths (e.g., `specs/retry-logic.md`, optionally with `#section` anchors). The anchors refer to headings within the file, not to versions of the file. Git tracks file history, but `spec_ref` contains no commit SHA, no version identifier, and no content hash. When a task cites `specs/api.md#pagination`, it references whatever content currently exists at that heading.

**Implication:** Spec drift during task execution is undetectable. A PRD produced by a spec-authoring agent and consumed by the Orchestrator can change between when the Orchestrator decomposes it and when the Coder implements the resulting tasks. The blackboard's `spec_changes` log tracks that changes occurred, not which tasks were affected by which changes.

**Current mitigation:** Code Reviewer validates against "current spec version" and logs `spec_changed` anomaly if material changes detected.

**Future options:**
- Include commit SHA or content hash in `spec_ref`
- Track `spec_version` at task creation and warn on divergence
- Generate spec snapshots when tasks are created

### State Validation Composition Gap

**Skill:** software-architecture-review
**Category:** FRAGILITY
**Status:** Low-visibility concern

**Issue:** `statevalidate` has 82 direct tests across task, agent, dependency, role, sprint, pipeline, spec-reference, and artifact-reference validation. Pass 20's separate full `-coverpkg` run measured the package at 89.6%, confirming that the inner validators are exercised. The remaining observable gap is narrower: there are no focused tests named for `ValidateStateFile` or `ValidateAnomalies`, so the public composition behavior is less visible than the validation units it coordinates.

**Implication:** A composition-only regression could be harder to localize, but this is not evidence of a broad validation coverage gap and does not warrant blanket coverage work.

**Direction:** Add one focused composition test when the validation entry point next changes, covering representative aggregation behavior. Keep package-wide percentage claims tied to a fresh complete profile rather than the unrelated ignored local `coverage.out`.

### SetTaskOutput spec_ref Validation Gap

**Skill:** software-architecture-review
**Category:** FRAGILITY
**Status:** Unresolved source conflict
**Related:** [`one-to-one` Transition Child Field Generation Unspecified](#one-to-one-transition-child-field-generation-unspecified)

**Issue:** The `set-task-output` CLI help declares `spec_ref` optional, and `ops/set_task_output.go` validates `desc`, `done_when`, and `scope` without generally requiring `spec_ref`. By contrast, `statevalidate` output validation and `ops/proceed.go:validateOutputEntry` require every output entry to have a non-empty `spec_ref`. The write boundary and its public contract therefore disagree with state and transition validation.

**Implication:** The same output is accepted by `SetTaskOutput` but rejected when the state or a downstream transition is validated. Operators cannot tell whether omitting `spec_ref` is supported, and the delayed failure creates a "write now, fail later" path.

**Direction:** Unresolved. Decide whether `spec_ref` is universally required or transition-specific. If required, enforce it in `SetTaskOutput` and update CLI help. If optional, relax state and transition validation for the applicable output types. Align tests and documentation with the chosen contract.

### INVARIANTS.md §7 Clean Worktree Not Enforced at Submission

**Skill:** software-architecture-review
**Category:** FRAGILITY
**Related:** [Contract-Driven Safety vs Structural Enforcement Asymptote](#contract-driven-safety-vs-structural-enforcement-asymptote)

**Issue:** INVARIANTS.md §7 states: "Clean sync: before READY_FOR_REVIEW, working tree must be clean (no staged, unstaged, or untracked files)." The `submit_review.go` submission path validates commit SHA match, TDD enforcement, pre-execution checkpoint, rebase onto integration, and re-validates status/assignment under lock — but performs no `git status` or working tree cleanliness check. The invariant is documented but not enforced.

**Implication:** A coder can submit work with uncommitted changes in the worktree. The reviewer sees only the committed code (via `review_commit`), but the coder may have relied on uncommitted files during local testing. If the coder's tests passed partly due to uncommitted state, the reviewer approves code that may fail on integration. This is a concrete instance of the structural enforcement asymptote — the invariant document promises a protection the code doesn't deliver.

**Direction:** Add a clean worktree check (`git status --porcelain` in the worktree) to `submit_review.go` before the rebase step. This matches the invariant's intent and catches a real failure mode. Alternatively, if clean worktree enforcement is intentionally deferred (e.g., to allow configuration files), narrow the invariant in INVARIANTS.md and the worktree-management spec.

### INVARIANTS.md §6 Provider Diversity Not Enforced at Verdict Time

**Skill:** software-architecture-review
**Category:** FRAGILITY
**Related:** [Contract-Driven Safety vs Structural Enforcement Asymptote](#contract-driven-safety-vs-structural-enforcement-asymptote)

**Issue:** INVARIANTS.md §6 states: "Quorum enforcement: approval count tracked, provider diversity required (≥2 distinct providers for multi-reviewer quorum)" and attributes enforcement to verdict submission. `SubmitVerdict` evaluates quorum count but does not evaluate provider diversity.

Provider diversity is actually enforced at two different points:
1. Reviewer claim filtering in `internal/agent/claiming.go` prevents a reviewer with the same provider as the doer from claiming when a diverse provider is available.
2. Merge-readiness evaluation in the same runtime path applies the configured preferred-diversity policy before integration.

Neither matches the invariant's description of hard enforcement during verdict submission.

**Implication:** If two reviewers from the same provider approve a task, the quorum count is met and the task transitions to APPROVED. The diversity check at merge-readiness uses "preferred" semantics (can be overridden if no diverse reviewers are available). The invariant document implies stronger enforcement than exists. This creates a false confidence surface for security-sensitive review workflows where provider diversity is a compliance requirement.

**Direction:** Either (a) add diversity evaluation to `submit_verdict.go` — when `ProviderDiversity` is configured and quorum > 1, verify `HasProviderDiversity()` before transitioning to APPROVED, or (b) update INVARIANTS.md §6 to accurately describe the current enforcement model: diversity is ensured at claim-time (doer-reviewer) and preferred at merge-time (reviewer-reviewer), not enforced at verdict time.

### Custom Roles Render Empty Prompt Blocks

**Skill:** software-architecture-review
**Category:** FRAGILITY
**Related:** [Mode Selection Trigger Coupled to Prompt Lexeme](#mode-selection-trigger-coupled-to-prompt-lexeme), [Contract-Driven Safety vs Structural Enforcement Asymptote](#contract-driven-safety-vs-structural-enforcement-asymptote)

**Issue:** `internal/agent/supervisor.go:648-650` documents that the resolver reads role definitions from pipeline YAML, "enabling custom YAML-defined roles without Go code changes". No layer validates the role *name*. Pipeline `validate` (`internal/pipeline/config.go:214-221`) checks only that `role.Type` is one of `doer|reviewer|orchestrator`. `Resolver.RoleType` looks the name up in the YAML map itself, so any declared role resolves. `agent/registration.go:18-43` validates the `{role}-{number}` ID shape and ID/config agreement, but never calls `roles.IsValid`. The closed 13-role registry in `internal/roles` is consulted in exactly one production path (`ops/claim_reviewer_task.go:61`), on an already-inferred role. Prompt content, however, is selected by hardcoded `if/else if` chains over role names in `internal/prompts/templates/blocks/review_instructions.tmpl` (392 lines, six roles) and `implementation_phase.tmpl` (240 lines, five roles). Neither chain has an `else` branch.

**Implication:** A YAML-defined role passes config validation, resolves a strategy, registers an agent, and launches successfully — then receives an empty instruction block. Every layer reports success; the agent simply has no role instructions. The declarative-lifecycle capability is real for the state machine and absent for prompt content, and the gap is silent at the boundary where it matters. This is the same failure shape as the mode-selection lexeme coupling: two independently maintained artifacts aligned only by convention, with no structural guard.

**Direction:** Decide whether custom roles are a supported capability. If yes, give both template chains an `else` fallback and validate role coverage where pipeline config is loaded, so an uncovered role fails at config time rather than at prompt render. If no, validate role names against `roles.IsValid` at pipeline load and correct the supervisor comment to describe the actual boundary.

### Raw Git Calls Bypass gitenv Hardening

**Skill:** software-architecture-review
**Category:** FRAGILITY

**Issue:** `internal/gitenv` exists specifically to force `LC_ALL=C` on Git subprocesses and to bound them (`DefaultCommandTimeout`, `DefaultCommandWaitDelay`). Sixteen files route Git through it. `internal/ops/init_project.go` and `cmd/liza/cmd_launch.go` call `exec.Command("git", ...)` directly at five sites. The divergence is visible in a duplicated pair: `commands/init.go:1170` `validateBranchName` uses `gitpkg.Command`, while `ops/init_project.go:300` `validateBranch` performs the same `check-ref-format` check with a raw `exec.Command`.

**Implication:** Git invocations on the unhardened path inherit the operator's locale, so any output parsing is locale-dependent, and they are unbounded — a hung Git process has no timeout. Both sites are in initialization, which runs before any supervisor or watchdog exists to detect the hang. The hardening is real but not structurally enforced, so each new Git call site is a coin flip.

**Direction:** Route all Git invocation through `gitenv`. Consider a pre-commit or lint guard rejecting `exec.Command("git"` outside `internal/gitenv`, which converts a convention into a check.

### Agent Lease Renewal Ignores Configured Lease Duration

**Skill:** software-architecture-review
**Category:** FRAGILITY
**Related:** [Restart/Lease Churn Under Load](#restartlease-churn-under-load)

**Issue:** `RunSupervisor` registers the agent with terminal identity `"terminal-1"` and a literal 1,800-second lease. It then constructs heartbeat configuration without a lease duration, causing the heartbeat path to apply its own 1,800-second default. The configuration schema says heartbeats extend the configured lease, but neither runtime path consumes that configured value.

**Implication:** A deployment that changes the agent lease duration still registers and renews agents on the duplicated default. A shorter configured lease leaves agent ownership active longer than intended; a longer configured lease allows earlier expiry than intended. The duplicated literal also hides the mismatch because registration and renewal agree with each other while disagreeing with configuration.

**Direction:** Build one runtime identity/lease context from configuration and pass it to both registration and heartbeat setup. Derive terminal identity from the actual launch context rather than a fixed placeholder.

### Provider Support Crosses the Catalog Boundary

**Skill:** software-architecture-review
**Category:** FRAGILITY
**Related:** [Provider Stop-Signal Recovery Has Multiple Owners](#provider-stop-signal-recovery-has-multiple-owners), [Agent Lease Renewal Ignores Configured Lease Duration](#agent-lease-renewal-ignores-configured-lease-duration)

**Issue:** The provider catalog owns detection and launch profiles but not the complete provider-extension contract. `ActivationAssets` exposes provider-named fields; initialization expands Cursor into Claude, Codex, and Cursor-ACP dependencies and maps bash-policy support through provider-specific booleans; Codex audit and availability signatures live separately in `internal/agent`.

**Implication:** Adding a catalog entry can make a provider resolvable and launchable without complete setup or operational-diagnostics support. Full onboarding requires coordinated edits across `providers`, `commands/init`, `embedded`, and `agent`, and omissions are discovered only at runtime.

**Direction:** Keep behavioral output parsers in code, but move declarative dependencies and activation assets into catalog-owned data. Maintain one explicit provider-extension checklist; do not introduce a generalized plug-in framework until another implementation proves the need.

### Canonical Provider Identity Lost Before Policy Enforcement

**Skill:** software-architecture-review
**Category:** FRAGILITY
**Related:** [INVARIANTS.md §6 Provider Diversity Not Enforced at Verdict Time](#invariantsmd-6-provider-diversity-not-enforced-at-verdict-time), [Provider Support Crosses the Catalog Boundary](#provider-support-crosses-the-catalog-boundary), [Provider Stop-Signal Recovery Has Multiple Owners](#provider-stop-signal-recovery-has-multiple-owners)

**Issue:** The provider catalog populates `AgentToolConfig.ProviderKey`, and `ResolveLaunchPlan` copies it into `LaunchPlan.ProviderKey`, but production code does not consume that launch-plan field. `SupervisorConfig` carries only the raw `CLIName`; `RunSupervisor` registers that value as `Agent.Provider`, and approval diversity compares the resulting strings. Quota signals canonicalize aliases such as `codex-acp` to `codex`, while provider-unavailable signals and audit anomalies use raw CLI names.

**Implication:** `codex` and `codex-acp` can falsely satisfy provider diversity despite sharing a backend, while a provider-unavailable signal for one alias does not stop supervisors using another. Custom tools sharing a `provider_key` are similarly fragmented across review, stop-state, and anomaly policy.

**Direction:** Carry canonical provider identity alongside tool identity through `SupervisorConfig`. Use canonical identity for registration, review policy, stop signals, and anomaly grouping; retain tool identity for launching and tool-specific diagnostics. Add alias-focused tests across diversity and both signal paths.

---

## Blind Spots

Unacknowledged forces or gaps the system doesn't model.

### Contract Effectiveness Self-Certification

**Skill:** systemic-thinking
**Category:** BLIND SPOT

**Issue:** The contract's failure mode coverage is self-certified. The failure mode map (`CONTRACT_FAILURE_MODE_MAP.md`) claims 55/55 "Strong" coverage with 0 Partial and 0 Gap. This assessment is produced by the same process that writes the contract — there is no independent validation that clauses actually suppress the failure modes they claim to cover. The map still describes "Contract v3 (882 lines)" and includes line references beyond the current 778-line CORE.md. The maintenance protocol ("check which failure modes the affected clause covers") is itself a behavioral rule. There is no test suite, simulation, or adversarial probe demonstrating that the 55 claims hold under context pressure, novel model versions, or multi-agent interaction.

**Implication:** The 55/55 coverage claim provides confidence without evidence — the map may be accurate, or it may be a snapshot of aspirational intent that has drifted from reality as the contract evolved.

**Future options:**
- Adversarial testing: deliberately trigger each failure mode and verify the contract suppresses it
- Automated line-number maintenance (extract clause IDs instead of line numbers)
- Periodic red-team exercises using the failure mode map as a checklist

### Initialization Completion Unverifiable

**Skill:** systemic-thinking
**Category:** BLIND SPOT

**Issue:** The contract requires a complex initialization sequence: mode detection → read mode contract → read project files → build 6 mental models → role-specific initialization. Completion of this sequence is entirely self-reported. There is no structural verification that an agent actually read what it was supposed to read, built the models it was supposed to build, or internalized the constraints. In multi-agent mode, the supervisor verifies agent registration (identity, lease) but not contract compliance. An agent that skips initialization or partially completes it enters the same state machine as a fully initialized agent. The "compaction checkpoint" and "working set" mechanisms handle mid-session degradation but assume initialization was complete — if it wasn't, the agent starts in a degraded state without any detection signal.

**Implication:** Contract compliance depends on a bootstrap sequence that cannot be verified from outside the agent — a model that partially follows initialization instructions produces no observable difference from one that follows them completely, until a violation occurs.

**Future options:**
- Initialization checklist emitted as structured output (supervisor verifies before accepting agent as ready)
- Canary questions: supervisor tests agent's knowledge of key contract clauses before allowing work
- Reduce initialization surface by embedding more rules in supervisor-enforced structural mechanisms

### Circuit Breaker Depends on Participant Reporting

**Skill:** systemic-thinking
**Category:** BLIND SPOT

**Issue:** The architecture describes the circuit breaker as a systemic observer with access to anomalies, logs, and sprint metrics, but current pattern detection consumes blackboard anomalies only. Its task-semantic inputs are behavioral logging duties assigned to Coders and Code Reviewers; supervisor-reported provider degradation is the main independent exception. Detection therefore assumes affected agents continue reporting failures and classify equivalent failures consistently.

**Implication:** Correlated failures that suppress, misclassify, or normalize anomaly reporting are invisible to the mechanism intended to detect systemic failure.

**Current mitigation:** Multiple role classes can report anomalies, anomaly payloads are validated, and operational monitoring observes several direct health signals. Those direct signals are not combined with the circuit-breaker pattern model.

**Future options:**
- Derive corroborating signals from task history, retries, verdict cycles, and sprint metrics
- Detect expected-but-missing anomaly reports after observable failure events
- Treat degraded or absent reporting as a circuit-breaker input

### No Source Type for Pre-Implementation Spec Findings

**Skill:** systemic-thinking
**Category:** BLIND SPOT

**Issue:** The `Discovery` model has no typed `Source` field. YAML keys such as `source` are absorbed by its inline `Extra` map, while the blackboard schema documents only `null` and `systemic-thinking`. The deployed spec-authoring roles can therefore log pre-implementation findings, but there is no canonical source value for them and no validation of the taxonomy.

**Implication:** Specification-quality issues can be recorded only through an undocumented ad hoc key/value convention. Consumers cannot reliably distinguish implementation discovery, architectural analysis, and spec-production findings.

**Current mitigation:** `by` and `during` identify the reporting agent and task, and `Extra` preserves ad hoc source metadata.

**Future options:**
- Add `spec-authoring` or `prd-validation` as valid `source` values
- Add `urgency: blocks_spec` to distinguish spec-blockers from implementation-blockers
- Track spec-production work separately from implementation tasks

### Prompt-Build-to-Execution State Drift

**Skill:** systemic-thinking
**Category:** BLIND SPOT

**Issue:** `RunSupervisor` builds and saves the prompt from its pre-execution state snapshot before starting the agent subprocess. The subprocess may then read a newer state. There is no version marker tying the saved prompt to the state snapshot from which it was constructed.

**Implication:** When debugging failures, the saved prompt may not represent the actual state the agent operated on. Agents can read live state through CLI JSON commands and the blackboard during execution, so the prompt is initial context rather than runtime truth and the behavioral impact remains low.

**Current mitigation:** Agents use current CLI/blackboard reads for state-dependent decisions. Prompts are timestamped and saved for debugging.

**Future options:**
- Include state version/checksum in prompt header for comparison
- Snapshot state.yaml at prompt build time alongside the prompt
- Add prompt-state consistency verification to post-execution diagnostics

### Index Discovery Failures Are Unobservable

**Skill:** software-architecture-review
**Category:** BLIND SPOT

**Issue:** `availablePromptScipIndexRefs`, `availablePromptStacklitIndexRefs`, and `availablePromptFunctionalClusterIndexRefs` (`internal/agent/prompt.go:252, :265, :278`) each discard the discovery error and return `nil`. The callers at `prompt.go:143-145` and `:355-357` cannot distinguish "no indexes configured" from "index discovery failed".

**Implication:** Agents receive prompts silently missing their SCIP, Stacklit, or functional-cluster index references and proceed as if the repository were unindexed. Exploration quality degrades — agents fall back to broad grep over targeted symbol lookup — with no error, no log line, and no operator signal. Because the degradation is invisible, it is indistinguishable from a repository that was simply never indexed, so it will not be investigated.

**Direction:** Log at warning level and keep the `nil` return. The graceful degradation is correct; only its invisibility is the defect.

### Destructive-DB Break-Glass Issued and Consumed by the Same Actor Class

**Skill:** systemic-thinking
**Category:** BLIND SPOT
**Related:** [Self-Reported Validation](#self-reported-validation), [Contract-Driven Safety vs Structural Enforcement Asymptote](#contract-driven-safety-vs-structural-enforcement-asymptote)

**Issue:** ADR-0072 made `validation[]` a canonical task field executed by both doer and reviewer — which is how the "Self-Reported Validation" issue was closed. ADR-0084 then layered a break-glass on top: `destructive_db: true` unlocks commands that would otherwise be rejected, provided every one is prefixed `LIZA_ALLOW_DESTRUCTIVE_DB=1 `. The ADR is explicit that "the marker is a human/operator safety contract, not proof that the selected DB target is disposable." But no human sits on that contract in normal operation. `internal/ops/add_tasks.go:67` (reached via the orchestrator's `liza add-task`) and `internal/ops/set_task_output.go:44` (reached via the planning doer's `liza set-task-output`) both accept `destructive_db` and the accompanying commands from an agent. The same actor class that authors the destructive command also sets the flag that authorizes it, and a downstream doer or reviewer executes it inside a worktree that ADR-0082 may have provisioned with real credential-bearing env files copied from the project root. Three independently reasonable ADRs compose into a path where an agent writes, authorizes, and has another agent execute a destructive database command against whatever the environment actually points at.

**Implication:** The safety property ADR-0084 claims — human authorization for destructive operations — is not structurally present in the path the system uses by default.

**Current mitigation:** `ValidateValidationSafety` enforces the prefix on every command when the marker is set, so the marker cannot be set without the commands visibly declaring themselves destructive. Worktree env-file provisioning (ADR-0082) is opt-in. Both make the situation legible in the blackboard; neither introduces an approver.

**Future options:**
- Require the marker to originate from project configuration or an operator-set flag rather than from task-writing CLI input
- Gate execution of marked commands on an explicit human acknowledgement recorded in the blackboard
- Separate the authoring authority from the authorizing authority so no single role can do both

### No Feedback Signal for Specification Quality

**Skill:** systemic-thinking
**Category:** BLIND SPOT
**Related:** [Spec Completeness vs Reality](#spec-completeness-vs-reality), [No Source Type for Pre-Implementation Spec Findings](#no-source-type-for-pre-implementation-spec-findings)

**Issue:** The system has rich feedback loops for code quality: reviewers reject work, rejection metrics are tracked, iteration limits catch loops, and circuit breakers detect patterns. Spec-writing roles and their reviewers are now deployed, with decomposition checklists at approval time. What is still missing is an outcome signal that connects an approved specification to downstream implementation results. There is no metric, anomaly type, or circuit-breaker pattern for "approved specs that later produce blocked tasks," and no attribution from a coder's spec failure back to the spec reviewer verdict.

**Implication:** With spec-writing pipelines deployed, the spec reviewer's accuracy is a poorly measured and consequential quality dimension — a bad spec can amplify through every downstream role without an outcome signal before coders burn cycles.

**Current mitigation:** The `spec_changed` anomaly type lets Code Reviewer note spec drift, but this tracks change, not quality. Human checkpoint review provides periodic correction.

**Future options:**
- Track spec provenance on tasks: which spec version was approved, which role approved it, and whether downstream tasks succeeded or blocked
- Add `spec_quality_gap` anomaly type for Coders to log "approved spec was insufficient for implementation"
- Compute spec reviewer accuracy metric: ratio of downstream task success to spec approvals
- Circuit breaker pattern for repeated spec-caused blocks across tasks from the same approved spec

### No Reverse Data Channel in Inter-Pair Transitions

**Skill:** systemic-thinking
**Category:** BLIND SPOT
**Related:** [No Feedback Signal for Specification Quality](#no-feedback-signal-for-specification-quality), [Cross-Pair Knowledge Required by Single-Pair Reviewers](#cross-pair-knowledge-required-by-single-pair-reviewers)

**Issue:** The deployed `output[]` mechanism is the structured forward path between role pairs: a doer writes decomposition entries, a reviewer validates them, and transitions create child tasks. When a downstream pair discovers that scope, completion criteria, validation, or dependencies were wrong, the available path is BLOCKED → orchestrator rescope. Discoveries and anomalies are visible to the orchestrator but are not routed back to the upstream pair or attached as outcome feedback to the originating output entry.

**Implication:** Decomposition quality is a learned skill that cannot improve within the system because the feedback loop is severed at the pair boundary — the entity that could learn (the upstream pair) never sees the signal.

**Current mitigation:** Human checkpoint review provides periodic observation of decomposition quality. Retrospective protocol identifies patterns but actions are human-directed, not agent-routed.

**Future options:**
- Add structured reverse-channel entries on child tasks (e.g., `parent_feedback: {quality: good|poor, reason: ...}`) populated by downstream doer on completion or blocking
- Route `spec_quality_gap` anomalies to the upstream pair's reviewer, not just the orchestrator
- Compute upstream pair accuracy metric: ratio of downstream child task success to upstream `output[]` entries approved

### Retrospective Findings Don't Feed Forward to Next Sprint

**Skill:** systemic-thinking
**Category:** BLIND SPOT
**Related:** [No Feedback Signal for Specification Quality](#no-feedback-signal-for-specification-quality)

**Issue:** Sprint retrospective data — identified patterns, spec gaps, root causes, corrective actions — is archived with the completed sprint to `.liza/archive/sprint-N.yaml`. Agents in sprint N+1 read `state.yaml`, which contains only lightweight `sprint_history[]` summaries (id, number, status, dates, tasks_done). The detailed retrospective that should inform the next sprint's task creation, scope calibration, and risk awareness is structurally inaccessible to agents unless they explicitly read archive files — which no initialization protocol requires. The human is expected to translate retrospective actions into spec updates, but the *patterns* (iteration count distributions, anomaly frequencies, decomposition quality signals) are lost to agent context.

**Implication:** Each sprint starts with the same naive optimism as the first — agents cannot calibrate based on prior sprint experience because the retrospective data exits the agent-readable surface at archival time.

**Current mitigation:** Human translates retrospective actions into spec updates or task adjustments manually. `spec_changes` in state.yaml provides some continuity, but this tracks changes, not the reasoning behind them.

**Future options:**
- Include a `lessons` summary field in `sprint_history[]` entries (top 3 patterns from retrospective, machine-readable)
- Add agent initialization step to read the most recent archived sprint's retrospective
- Carry forward unresolved retrospective actions as structured items in state.yaml rather than archiving them with the sprint

### Sprint Metrics Lossy at Sprint Boundary

**Skill:** software-architecture-review
**Category:** BLIND SPOT
**Related:** [Retrospective Findings Don't Feed Forward to Next Sprint](#retrospective-findings-dont-feed-forward-to-next-sprint), [Metrics Collection Without Query Interface](#metrics-collection-without-query-interface)

**Issue:** `applySprintAdvance` reduces the current 11-field `SprintMetrics` struct to `TasksDone` in `SprintSummary`. The active history loses in-progress/blocked counts, iteration and review-cycle totals, verdict counts and approval rate, submission count, and task-outcome approval rate. These remain available only in the archived sprint YAML.

The orchestrator context renders each `SprintSummary` as status plus tasks done. Planning for sprint N+1 therefore cannot use prior block, iteration, review, or submission metrics without reading the archive files, which no prompt template requires.

This is distinct from "Retrospective Findings Don't Feed Forward" (which covers qualitative retrospective findings) and "Metrics Collection Without Query Interface" (which covers the absence of a unified query layer). This issue is specifically about the quantitative metrics data being structurally discarded at the sprint boundary.

**Implication:** The system collects detailed sprint performance metrics during execution, then discards all but one at the sprint boundary. Scope calibration decisions (how many tasks to plan, iteration cap tuning, risk assessment) lack quantitative foundation from prior sprints.

**Current mitigation:** Full sprint data is archived to disk. Humans can inspect archives manually. The `SprintSummary` struct was intentionally kept lightweight per ADR-0028.

**Future options:**
- Extend `SprintSummary` with a small set of decision-relevant metrics (e.g., `AvgReviewIterations`, `ReviewApprovalRate`, `TasksBlockedCount`)
- Add a `MetricsSummary` sub-struct to `SprintSummary` carrying the top 4-5 planning-relevant fields
- Have orchestrator templates reference the previous sprint's archive file (deterministic path: `.liza/archive/sprint-N.yaml`)

---

## Trajectory

Long-term concerns about system evolution.

### Blackboard Growth Without Pruning

**Skill:** systemic-thinking
**Category:** TRAJECTORY

**Issue:** Completed sprint structures are archived and summarized in `sprint_history`, but active `state.yaml` retains the flat task collection and append-oriented discoveries, anomalies, history, and audit data. There is no retention trigger for moving old task/log detail out of the active blackboard.

**Implication:** Sprint archival bounds some active sprint data but does not bound the primary state file. Long-lived goals can still increase read, validation, and prompt-context costs.

**Current mitigation:** Full sprint records are archived to `.liza/archive/`, and current state retains lightweight sprint summaries.

**Future options:**
- Archive or compact terminal tasks and associated audit entries after a retention window
- Prune history older than N days
- Split blackboard by concern (tasks, agents, anomalies)

### Role Addition Accelerates Contract Complexity Pressure

**Skill:** systemic-thinking
**Category:** TRAJECTORY
**Related:** [Contract Complexity vs Context Pressure](#contract-complexity-vs-context-pressure)

**Issue:** The deployed topology now contains 13 roles and 9 role-pairs. Each additional role still requires coordinated changes across pipeline configuration, role documentation, prompts/context sections, allowed operations, task-type workflow registration, and tests. The baseline contract corpus is already 1,230 lines before role-specific inputs, so role growth continues to consume the same context budget needed for task work.

**Implication:** Topology expansion has moved from a future concern to an active maintenance multiplier. The tier architecture handles mid-session degradation, but initialization and cross-artifact consistency costs grow with each role.

**Current mitigation:** Declarative pipeline configuration centralizes much of the topology, and consistency tests cover rendered embedded artifacts.

**Future options:**
- Conditional contract loading (only load role-relevant sections)
- Structural enforcement replacing behavioral rules (more logic in Go, less in contract)
- Measure contract-to-work ratio empirically before adding roles

### Anomaly Detail Validation Incomplete

**Skill:** code-review
**Category:** FRAGILITY

**Issue:** The model accepts 19 anomaly types. `internal/statevalidate/validate_entity.go` enforces type-specific detail fields for 9 of them; the other 10, including `reviewer_loop` and `review_exhaustion`, receive no type-specific detail validation even where specs or prompts expect structured fields.

**Implication:** Agents can write structurally valid but informationally empty anomalies. Circuit breaker pattern detection and retrospective analysis degrade when detail fields are missing.

**Future options:**
- Add cases for the 10 unvalidated types in `internal/statevalidate/validate_entity.go`
- Generate validation from a single type→fields declaration (eliminate spec/code/template as three separate lists)

### Spec Corpus Lacks Lifecycle Management

**Skill:** systemic-thinking
**Category:** TRAJECTORY

**Issue:** The spec-first design requires specifications before implementation, blocks work on missing specs, and logs spec changes to the blackboard. The corpus still has no automated lifecycle management. Commit `ffe89080` manually removed 226 intermediate architecture and code-planning artifacts (150 Markdown and 76 JSON files) from `specs/arch-plan/` and `specs/plans/`, reducing immediate corpus pressure. However, the generation paths still create Git-tracked artifacts, and no policy identifies when a completed run's artifacts may be retired. Durable specifications likewise have no status for superseded or archived content and no staleness detection when their referent code changes.

**Implication:** Without a repeatable retention trigger, intermediate artifacts will accumulate again and humans must periodically distinguish disposable run output from durable product specifications. Long-lived projects still incur maintenance and agent-context costs proportional to corpus age.

**Current mitigation:** Manual pruning can remove completed-run artifacts while Git history preserves the audit trail.

**Future options:**
- Retire intermediate planning artifacts automatically or by checklist when a goal closes
- Spec status field (active, superseded, archived) with archival workflow
- Staleness detection: flag specs not referenced by any task in N sprints
- Hierarchical spec organization with summary documents to reduce agent read cost

### Metrics Collection Without Query Interface

**Skill:** systemic-thinking
**Category:** TRAJECTORY

**Issue:** The system collects file-lock timing, sprint metrics, and diagnostic data, but there is no unified query layer or historical store across those sources. Operators can inspect current sprint metrics through CLI status/JSON commands, but questions such as agent performance over time or current lock contention still require archive inspection or custom code.

**Implication:** Operational visibility requires ad hoc tooling or direct state.yaml inspection. The investment in metrics instrumentation doesn't translate to operational insight because the data is fragmented and inaccessible through standard interfaces.

**Current mitigation:** Current metrics are accessible through `liza inspect metrics` and `liza status`.

**Future options:**
- Unified query interface aggregating all metric sources
- Time-series storage for historical metric analysis
- Dashboard generation from collected metrics

### No Query Layer

**Skill:** systemic-thinking
**Category:** TRAJECTORY
**Coupled with:** [Prompts Layer Imports Business Logic](#prompts-layer-imports-business-logic), [Commands Layer Imports Agent Runtime](#commands-layer-imports-agent-runtime)

**Issue:** The system has a clear mutation layer (`ops`) but no cohesive query layer. Read operations are distributed across `models`, `db`, `commands`, `agent`, and `ops`. `models/diagnostics.go` provides a partial query home, yet prompt assembly still imports lifecycle queries from `ops` and commands still import agent runtime functions. Each consumer builds its own view of state rather than depending on a stable structured query boundary.

**Implication:** As dashboards, diagnostics, and prompt context evolve, query logic can be duplicated or exposed through package seams whose primary responsibility is mutation or runtime control.

**Future options:**
- Extract query functions to `ops` or a new `queries` package returning structured data (each presentation layer formats independently)
- Promote `models/diagnostics.go` as the canonical query home and migrate state queries from `commands` and `agent`
- Accept `commands` as the shared query+formatting layer and document or rename to reflect its dual role

### Ownership Recovery Bound to Process-Shaped Liveness Evidence

**Skill:** systemic-thinking
**Category:** TRAJECTORY
**Related:** [Restart/Lease Churn Under Load](#restartlease-churn-under-load), [Agent Lease Renewal Ignores Configured Lease Duration](#agent-lease-renewal-ignores-configured-lease-duration)

**Issue:** Every ownership reconciliation in the architecture resolves to one form of evidence: a PID plus `AgentProcessStatus(...).IsLiveOrUnknown()`. It gates registration collision detection, active and passive review ownership (`roles.md:408`), stale-claim clearing, owned-executing recovery, and blocked-task repair (`state-machines.md`, `blackboard-schema.md:907,911`, `INVARIANTS.md:135`). This binds all recovery to OS process identity. ADR-0085 already carries the other half of the picture: `ACPXAgent` exposes `SessionID`/`WarmUsage`, and warm sessions survive across process restarts (`acp-vs-cli.md`: local `seen` map plus `acpx sessions show`), so conversational state can outlive the process that hosted it. This is **not an unnoticed gap** — ADR-0085:68 records the exclusion deliberately: `WarmUsage` "is best-effort operational metadata … It must not drive correctness-sensitive task transitions." The blackboard schema models no session identity accordingly. The decision is coherent today because every provider backend is a locally-spawned process whose liveness the OS can answer for. It is a decision with an expiry condition, and the condition is not recorded anywhere.

**Implication:** The exclusion of session state from correctness paths is sound only while providers remain locally-spawned processes; a remote, daemonized, or pooled backend would make PID liveness answer a different question than the one recovery is asking, with no signal that the recorded decision's premise has lapsed.

**Current mitigation:** `IsLiveOrUnknown` fails safe — an unknown process status is treated as live, so reconciliation prefers leaving a claim in place over stealing it. Combined with lease expiry this bounds the damage. ADR-0085's boundary keeps session metadata out of correctness paths, so the two liveness notions cannot currently be confused.

**Future options:**
- Record the premise as an explicit constraint on the `LLMAgent` boundary: backends must be process-addressable for liveness, so a non-conforming backend fails at review rather than silently
- Define a provider-supplied liveness predicate on the boundary (ADR-0085), leaving PID probing as the CLI backend's implementation of it
- Revisit ADR-0085:68 if and when a non-process-shaped backend is proposed, rather than treating the exclusion as permanent

---

## Cascades

Failure propagation and compound interaction effects.

### Sub-Pipeline Expansion Multiplies Every Existing Issue

**Skill:** systemic-thinking
**Category:** CASCADE
**Related:** [Contract Complexity vs Context Pressure](#contract-complexity-vs-context-pressure), [Role Addition Accelerates Contract Complexity Pressure](#role-addition-accelerates-contract-complexity-pressure), [Filesystem/Git I/O Contention](#filesystemgit-io-contention), [Restart/Lease Churn Under Load](#restartlease-churn-under-load)

**Issue:** Sub-pipeline expansion is deployed: 13 roles operate across 9 role-pairs, including epic planning, user-story writing, architecture, code planning, coding, and integration. Concerns that scale with topology now compound in production: contract/context pressure, cross-pair decomposition quality, supervisor process count, restart/lease churn, and filesystem/Git load. The 53 open issues are tracked individually, but their interaction under full topology remains largely unmeasured.

**Implication:** Risks that were tolerable in a smaller topology can amplify one another as task fan-out and agent count increase; the aggregate risk profile is not captured by individual issue severity alone.

**Current mitigation:** Declarative role-pair configuration, generic role strategies, progress watchdogs, atomic blackboard writes, and CAS integration merges prevent several earlier scaling failure modes. Individual concerns remain documented here.

**Future options:**
- Model interaction effects between high-priority issues
- Establish performance baselines at the current full topology
- Prioritize structural fixes for concerns that compound before adding more role-pairs

### Fan-Out Amplifies Decomposition Errors Across Pipeline Stages

**Skill:** systemic-thinking
**Category:** CASCADE
**Related:** [Sub-Pipeline Expansion Multiplies Every Existing Issue](#sub-pipeline-expansion-multiplies-every-existing-issue), [Cross-Pair Knowledge Required by Single-Pair Reviewers](#cross-pair-knowledge-required-by-single-pair-reviewers), [No Reverse Data Channel in Inter-Pair Transitions](#no-reverse-data-channel-in-inter-pair-transitions)

**Issue:** Each `per-subtask` transition multiplies the task count. In the full pipeline, 1 goal produces N epics, each epic produces M user stories, each US produces P code plans, each plan produces Q coding tasks. A decomposition error at stage K propagates as a multiplicative factor across all downstream stages. Sprint serialization means each stage runs to completion before the next begins — the first signal that an epic was wrongly framed arrives from the coding sprint, 3 sprint boundaries later, having generated potentially dozens of mis-scoped tasks that each consumed agent time. The human reviews at each sprint boundary, but reviews a *completed* sprint — the error is visible only in retrospect, after the fan-out has occurred. Unlike "Sub-Pipeline Expansion Multiplies Every Existing Issue" (which concerns *system-level* issues scaling with role count), this concerns *domain-level* errors (wrong decomposition) amplifying through the fan-out topology of per-subtask transitions.

**Implication:** The cost of decomposition errors grows multiplicatively with pipeline depth while detection remains constant (single reviewer at each stage), creating a structural risk gradient that steepens as the pipeline lengthens.

**Current mitigation:** Sprint-boundary human review provides a checkpoint between stages. Deployed reviewer templates include decomposition quality gates, and the sub-pipelines spec acknowledges `output[]` quality as harder than artifact review.

**Future options:**
- Early sampling: downstream pair processes one `output[]` entry as a pilot before the full fan-out is committed
- Decomposition cost estimator: flag `output[]` entries that would produce >N downstream tasks for human review before `liza proceed`
- Cross-sprint error attribution: when downstream tasks block, trace back to the upstream `output[]` entry and accumulate error counts per upstream pair

---

## Accepted v1 Limitations

### Kill Switch Granularity

**Skill:** systemic-thinking

**Issue:** System-wide PAUSE/ABORT switches affect all agents. `cancel-task` now provides task-scoped cancellation and releases task/reviewer state, but it does not immediately terminate a currently running provider subprocess.

**Why accept:** Hard process termination requires supervisor/process ownership coordination and risks interrupting cleanup. The residual window is bounded by the running command or supervisor lifecycle.

**Current mitigation:** `liza cancel-task` prevents further task progression and the supervisor observes the cancelled state.

**Future option:** Supervisor-mediated cancellation signal that terminates the active provider process and then performs normal cleanup.

### Orchestrator as Single Semantic Interpreter

**Skill:** systemic-thinking
**Category:** LOAD-BEARING

**Issue:** The Orchestrator carries the cross-system semantic burden: it selects task topology, interprets failure signals, resolves blocked reviews, converts discoveries to tasks, and maintains goal alignment. Architecture, planning, writing, implementation, and review roles perform substantive reasoning within assigned scopes, but no peer role independently validates the Orchestrator's coordination and rescoping decisions.

**Implication:** Orchestrator drift compounds silently across all tasks until human checkpoint reveals accumulated misalignment. Correction costs scale with drift duration.

**Current mitigation:** Human checkpoints provide periodic correction opportunities.

**Future options:**
- Orchestrator self-review before finalizing task decomposition
- Second Orchestrator instance for cross-validation on critical decisions
- Automated coherence checks against vision.md

### Supervisor as Single Correctness Gate

**Skill:** systemic-thinking
**Category:** LOAD-BEARING

**Issue:** System depends on the shared supervisor implementation (`liza agent`) performing correct pre-claim/assignment for every role. Supervisors run per agent, but all instances execute the same control path; a systematic bug in that path can prevent tasks from progressing or allow agents outside protocol.

**Implication:** A shared supervisor bug or configuration error can affect every role simultaneously even though process instances are distributed.

**Current mitigation:** Supervisor is implemented in the `liza` Go binary with type-safe error handling. `liza validate` catches invalid states.

**Future options:**
- Supervisor health check endpoint
- Redundant supervisor with leader election
- Agent self-validation of claim state on startup

### Spec Completeness vs Reality

**Skill:** systemic-thinking
**Category:** TENSION

**Issue:** The vision positions durable specifications as the mechanism for context survival. Deployed architecture, epic-planning, user-story, and code-planning pipelines can elaborate an input document, but the selected entry point still requires enough domain intent to make downstream criteria testable. Missing product decisions eventually produce blocked tasks or spec-gap escalation that only the human can resolve.

**Implication:** The system handles specification production better than v1 did, but throughput still collapses rather than degrades when requirements must be discovered through implementation or an unavailable domain owner.

**Current mitigation:** Multiple specification-producing entry points, input-readiness checks, and BLOCKED resolution through `human_notes`; Orchestrator reads human notes on wake.

**Future options:**
- Spike mode for spec discovery
- Orchestrator-assisted spec drafting from coder discoveries
- Graceful degradation when specs incomplete (proceed with explicit assumptions)

### Code Reviewer Structural Accountability Gap

**Skill:** systemic-thinking
**Category:** TENSION

**Issue:** The Code Reviewer has binding approval/rejection authority but no structural accountability for verdict quality. The contract specifies detection of reviewer dysfunction in two modes: rubber-stamping (>95% approval-rate metric, `MULTI_AGENT_MODE.md`) and abandonment (review exhaustion — two reviewers exit without verdict). These remain contract-specified rather than supervisor-computed signals. The system cannot directly detect incorrect verdicts with plausible reasoning. A reviewer that rejects valid work forces implement-review cycles before Orchestrator evaluation, while a flawed approval may be caught by canonical validation or downstream integration review without being attributed back to the original verdict. Coders can plead a contested finding and, absent consensus, mark the task BLOCKED for Orchestrator rescope (`MULTI_AGENT_MODE.md`, Iteration Protocol), but verdict quality itself remains unmeasured.

**Implication:** Code review quality is the least observable dimension of system health, yet it gates all task completion — the system optimizes for reviewer throughput signals while reviewer accuracy remains unmeasured.

**Current mitigation:** Task-declared canonical validation commands are rendered to reviewers, reviewer contracts require re-execution, and the deployed Integration Analyst/Integration Reviewer pair checks cross-task integration. These controls catch some bad approvals but do not measure reviewer accuracy, and the bounded contest path is not a first-class persisted appeal.

**Future options:**
- Reviewer accuracy metric (compare rejected items against final merged state)
- First-class persisted appeal (objection recorded as state rather than prose, triggering Orchestrator evaluation before 5 cycles)
- Attribute integration-validation failures back to the approving review cycle

### Restart/Lease Churn Under Load

**Skill:** systemic-thinking
**Category:** FEEDBACK

**Issue:** Protocol restarts agents on exit 42 and uses leases/heartbeats for coordination. Under load or long-running operations, lease pressure and restart frequency can amplify each other. The restart loop is assumed stabilizing but can become self-sustaining when work exceeds lease windows.

**Implication:** Under stress, system enters churn state—progress stops but resource usage and log noise increase.

**Current mitigation:** Grace periods on lease checks, bounded exit-42 restart tracking with increasing backoff, execution progress watchdogs, and spinning/successful-no-progress detection that blocks the affected task.

**Future options:**
- Adaptive lease duration based on task complexity
- Telemetry for restart/backoff and lease-expiry correlation

### Human Availability as Bottleneck

**Skill:** systemic-thinking
**Category:** ASSUMPTION

**Issue:** Human is circuit breaker, final domain authority, checkpoint reviewer, and resolution authority for deadlocks. Sprint governance states hard checkpoints pause agents indefinitely awaiting human action, and manual transition checkpoints still require human approval before downstream work proceeds. The "solo developers, small teams" deployment context is load-bearing, not merely scope-limiting.

If human attention becomes bottleneck (competing priorities, vacation, scaling), system has no degradation path. All escalation paths terminate at same person with no delegation.

**Implication:** Human availability constrains throughput more than agent capacity, inverting goal of reducing human bandwidth as bottleneck.

**Future options:**
- Timeout with automatic abort after N hours without human response
- Delegation mechanism for escalation routing
- Async human review queue with SLA tracking

### Dual Contract Delivery Paths

**Skill:** systemic-thinking
**Category:** FRAGILITY

**Issue:** Contract content exists as repository masters, binary-embedded setup artifacts, and installed copies under `~/.liza/`. Development symlinks can resolve to repository masters immediately, while installed copies change only after `liza setup --force`; the embedded corpus changes only when the binary is rebuilt. The project pipeline adds a fourth version regime: `.liza/pipeline.yaml` freezes topology at initialization, while `LoadFrozen` injects missing allowed operations and selected compatibility metadata from the current binary without migrating topology or task slugs. The pipeline schema carries no version identifying the runtime semantics with which it is compatible. A workspace can therefore combine historical topology, current operation capabilities, current Go behavior, and independently installed contracts.

**Implication:** Control-plane drift is silent — agents may operate under different behavioral rules and orchestration topology than the operator believes are active, with no explicit compatibility boundary.

**Partial mitigation (P1.4, commits `47e5597`, `bab9a78`):** `TestArtifactConsistency` in `internal/embedded/consistency_test.go` compares the rendered repo master corpus against embedded copies and is wired into `make lint` via `make check-embedded`. Pipeline loading validates the frozen document structurally and backfills selected compatibility fields. These checks do not establish semantic compatibility across pipeline, runtime, and installed contract versions.

**Remaining gaps:**
- Installed copies (`~/.liza/`) can still drift from both repo and embedded versions
- No runtime compatibility check between binary version and installed contract version
- `state.yaml`'s `version: 1` field remains inert
- No version or semantic hash links the frozen pipeline to its originating runtime
- Partial in-memory migration creates a hybrid control plane rather than a declared supported version

**Future options:**
- Content hash in contract files, verified at agent startup
- `liza validate` checks embedded vs installed contract consistency
- Single delivery path (eliminate duplication, choose symlinks or embedding)
- Version the pipeline schema and declare runtime compatibility ranges
- Report or migrate semantic differences between frozen and embedded pipeline configuration

---

## Structural Debt

### Decompose proceed.go (1,500 LOC)

**Skill:** code-quality-assessment
**Category:** RECOMMENDATION (P1 — reassessed 2026-07-24)
**Assessment:** [2026-07-24 code quality assessment](code_quality_assessment.md#11-decompose-internalopsproceedgo)

**Issue:** `internal/ops/proceed.go` reached 1,500 LOC, up from 1,200 at the 2026-04-13 assessment and 816 at the 2026-03-24 assessment. It spans transition execution, many-to-one cohort management, phase-gate dependency propagation, topo-sorted execution with SCC cycle detection, crash recovery, and child task construction. `ExecuteAvailableTransitions` is 218 LOC and `recoverCrashedTransition` is 183 LOC.

**Implication:** Navigability, reviewability, and testability degrade as feature work continues to interleave otherwise distinct lifecycle responsibilities. Two consecutive reassessments show that the debt is compounding rather than remaining stable.

**Direction:** Extract by responsibility cluster: transition execution, cohort management, child construction, graph algorithms, crash recovery, and the available-transitions engine. Each cluster has clear boundaries already. After structural decomposition, evaluate design-level improvements (P3.4).

### Decompose init.go (1,268 LOC)

**Skill:** code-quality-assessment
**Category:** RECOMMENDATION (P1 — reassessed 2026-07-24)
**Assessment:** [2026-07-24 code quality assessment](code_quality_assessment.md#12-decompose-internalcommandsinitgo)

**Issue:** `internal/commands/init.go` reached 1,268 LOC, up from 854 at the 2026-04-13 assessment. `InitCommandWithConfig` grew from 315 to 405 LOC. Sequential phases including project detection, artifact setup, configuration generation, and interactive prompting remain concentrated in the same command implementation.

**Implication:** Brownfield init evolution is constrained by the monolithic function. Adding detection, migration, or interactive behavior requires understanding a 405 LOC control flow and increases regression scope.

**Direction:** Extract each phase into its own function or file. Phases are sequential with clear data boundaries.

### Duplicate Initialization Implementations

**Skill:** software-architecture-review
**Category:** STRUCTURAL DEBT
**Related:** [Decompose init.go (1,268 LOC)](#decompose-initgo-1268-loc), [Raw Git Calls Bypass gitenv Hardening](#raw-git-calls-bypass-gitenv-hardening)

**Issue:** `internal/commands/init.go` (1,268 LOC) and `internal/ops/init_project.go` are parallel initialization implementations carrying near-verbatim duplicated helpers. They have already diverged: the `commands` path routes Git through `internal/gitenv`, the `ops` path calls `exec.Command("git", ...)` directly. `InitCommandWithConfig` (405 LOC) and `InitProject` (222 LOC) are the second and sixth longest functions in the repository.

**Implication:** Initialization fixes land in one implementation and miss the other, and the existing divergence is already a hardening gap rather than a cosmetic one. This is distinct from the file-size debt tracked in *Decompose init.go*: decomposing either file does not converge the two implementations, and decomposing both independently would double the duplicated surface.

**Direction:** Establish a single owner for initialization mechanics before decomposing either file, otherwise decomposition entrenches the duplication. Converging the Git call sites on `gitenv` is the smallest useful first step and closes the known divergence.

### Provider Stop-Signal Recovery Has Multiple Owners

**Skill:** software-architecture-review
**Category:** STRUCTURAL DEBT
**Related:** [Commands Layer Imports Agent Runtime](#commands-layer-imports-agent-runtime)

**Issue:** Quota exhaustion and provider-unavailable stop states are implemented as parallel agent-runtime helpers, then interpreted and cleared independently by CLI resume and TUI resume. The two presentation paths duplicate signal detection, cleanup, target-state selection, and user-facing reporting, with corresponding duplicated tests. Policy is therefore spread across `internal/agent`, `internal/commands`, and `internal/tui`.

**Implication:** Adding a provider stop reason or changing recovery semantics requires coordinated edits across runtime and both interaction surfaces. Each surface can remain locally tested while disagreeing about which signals to clear or which agent state to restore.

**Direction:** Give one operation ownership of stop-signal inspection, recovery-policy evaluation, cleanup, and the resulting state mutation. Return a typed recovery result that CLI and TUI render independently; keep provider-specific signal creation in the agent runtime.

### Worktree Intelligence Refresh Has Multiple Owners

**Skill:** software-architecture-review
**Category:** STRUCTURAL DEBT
**Related:** [Duplicate Initialization Implementations](#duplicate-initialization-implementations)

**Issue:** Worktree creation, task claim, periodic worktree health checking, and review submission each orchestrate SCIP, Stacklit, and functional-clusters refreshes. The sequences differ slightly in ordering, enablement checks, warning text, and failure tolerance. Some paths call tool packages directly while review submission adds local wrapper functions around the same operations.

**Implication:** Repository-intelligence freshness and failure policy depend on which lifecycle path triggered the refresh. Adding a tool or correcting refresh behavior requires finding every orchestration copy, and partial updates can produce lifecycle-specific stale indexes without a single failing boundary.

**Direction:** Introduce one refresh coordinator with explicit inputs for trigger, enabled tools, and failure policy. Keep lifecycle-specific decisions at callers and tool-specific execution in the existing adapters; do not collapse distinct tool semantics into a generic command runner.

### Decompose supervisor.go (1,129 LOC)

**Skill:** code-quality-assessment
**Category:** RECOMMENDATION (P1 — reassessed 2026-07-24)
**Assessment:** [2026-07-24 code quality assessment](code_quality_assessment.md#13-decompose-internalagentsupervisorgo)

**Issue:** `internal/agent/supervisor.go` reached 1,129 LOC, up from 831 at the 2026-04-13 assessment. It continues to mix restart tracking, spinning detection, CLI execution, lease behavior, and the supervisor loop. `RunSupervisor` grew from 287 to 442 LOC.

**Implication:** Policy, process execution, and recovery behavior now compete for attention in the runtime's central loop. The larger function makes lifecycle changes harder to review against concurrency and lease invariants.

**Direction:** Extract restart tracking and spinning policy into focused files, and move `DefaultCLIExecutor` to a dedicated implementation. Then simplify `RunSupervisor` around a small set of named lifecycle phases.

### Decompose watch.go (1,407 LOC)

**Skill:** code-quality-assessment
**Category:** RECOMMENDATION (P1 — escalated 2026-07-24)
**Assessment:** [2026-07-24 code quality assessment](code_quality_assessment.md#14-decompose-internalcommandswatchgo)

**Issue:** `internal/commands/watch.go` grew from 846 LOC at the 2026-04-13 assessment to 1,407 LOC. It combines state observation, anomaly diagnosis, automated repair, lifecycle decisions, and human-facing reporting in one command implementation.

**Implication:** Monitoring is becoming a parallel orchestration surface. A change to repair policy or output formatting requires navigating unrelated concerns, and command-level tests must cover an increasingly broad state space.

**Direction:** Separate observation, diagnosis, repair policy, lifecycle actions, and rendering while preserving the command's public behavior and existing tests.

### Split embedded.go by Artifact Family (1,530 LOC)

**Skill:** code-quality-assessment
**Category:** RECOMMENDATION (P1 — new 2026-07-24)
**Assessment:** [2026-07-24 code quality assessment](code_quality_assessment.md#15-split-internalembeddedembeddedgo-by-artifact-family)

**Issue:** `internal/embedded/embedded.go` is 1,530 LOC and combines global corpus writing, Claude JSON merging, Codex TOML and permission handling, hook installation, stale MCP cleanup, pipeline/guardrail installation, and project artifact generation.

**Implication:** These responsibilities operate on different schemas and change for different reasons. Co-location increases the blast radius of provider-specific changes and makes artifact installation behavior harder to review.

**Direction:** Keep the package API stable while splitting implementation by artifact family: global corpus, Claude settings, Codex settings and permissions, hooks, project artifacts, and stale-artifact cleanup.

### CI Does Not Enforce Cross-Language Quality Gates

**Skill:** code-quality-assessment
**Category:** CROSS-CUTTING
**Assessment:** [2026-07-24 code quality assessment](code_quality_assessment.md#16-enforce-the-multi-language-quality-contract-in-ci)

**Issue:** CI runs `make lint`, `make test`, `make test-e2e`, and `make build`, but `make lint` does not run the full 22-hook pre-commit policy. CI therefore omits staticcheck, goimports verification, duplicate detection, ruff, mypy, and pytest. Python now accounts for 5,114 production LOC and 2,196 test LOC. The pre-commit configuration also resolves goimports and staticcheck through `@latest`.

**Implication:** The repository advertises a broader quality contract than merge protection enforces. Python regressions and several Go/static-analysis failures can merge successfully, while local results depend on mutable tool versions.

**Direction:** Add a shared, reproducible CI quality target that installs the locked Python development environment and runs pytest, ruff, and mypy alongside the intended Go and cross-language checks. Pin Go lint-tool versions.

### Control-Flow Vocabulary Bypasses Domain Ownership

**Skill:** code-quality-assessment
**Category:** CROSS-CUTTING
**Assessment:** [2026-07-24 code quality assessment](code_quality_assessment.md#17-own-control-flow-vocabulary)

**Issue:** Role values (`"doer"`, `"reviewer"`, `"orchestrator"`), transition cardinalities (`"per-subtask"`, `"one-to-one"`, `"many-to-one"`), and phase categories participate in control flow across command, agent, operation, and pipeline packages as raw strings. This occurs despite partial typed ownership in `internal/roles` and `internal/pipeline`.

**Implication:** Cross-package behavior is coupled through spelling rather than compiler-checked vocabulary. Typos and renames can create runtime-only failures, and the valid value set is harder to discover.

**Direction:** Define the vocabulary as typed constants at its owning domain boundary and replace raw comparisons incrementally. Keep user-supplied identities and configuration values dynamic rather than disguising them as constants.

### Python Skill Utilities Lack Quality Parity

**Skill:** code-quality-assessment
**Category:** SUBSYSTEM CONCERN
**Assessment:** [2026-07-24 code quality assessment](code_quality_assessment.md#22-restore-python-utility-test-and-structure-parity)

**Issue:** Python skill utilities total 5,114 production LOC and 2,196 test LOC (0.43:1). `analyze-log.py` is 1,924 LOC, `context-corpus-index.py` is 968 LOC, and two blackboard modules exceed 600 LOC. The context-engineering and white-box-red-testing Python packages have no tests, and no Python checks run in CI.

**Implication:** A material production surface has weaker regression protection and substantially larger modules than the repository's Go conventions. CLI and serialized-output changes can break skill workflows without blocking a merge.

**Direction:** First enforce existing Python tests and tooling in CI. Add behavioral tests for the untested skill packages, then split the largest scripts around parsing, analysis/state operations, and rendering boundaries while preserving their CLI contracts.

---

## Completed Fixes

- **2026-03-11:** [MCP Admin Handler Authorization Gap](#mcp-admin-handler-authorization-gap) and [Unbounded Integration Test Execution](#unbounded-integration-test-execution).
- **2026-04-13:** [MCP Cross-Layer Read Dependency](#mcp-cross-layer-read-dependency).
- **2026-07-25:** [Role Pair Field as Single Point of Configuration Truth](#role-pair-field-as-single-point-of-configuration-truth), [Task Type Registry Only Supports Coding Workflows](#task-type-registry-only-supports-coding-workflows), [Task Type Registry is Partial Abstraction](#task-type-registry-is-partial-abstraction), [Orchestrator Role Dissolution Without Replacement](#orchestrator-role-dissolution-without-replacement), [Supervisor Wait-Claim-Spawn Loop](#supervisor-wait-claim-spawn-loop), [Implicit Orchestrator Provenance Default](#implicit-orchestrator-provenance-default), [Orchestrator State Change Verification is Non-Binding](#orchestrator-state-change-verification-is-non-binding), [`one-to-one` Transition Child Field Generation Unspecified](#one-to-one-transition-child-field-generation-unspecified), [Cache Coherence Gap in Multi-Process Deployments](#cache-coherence-gap-in-multi-process-deployments), [Bootstrap Artifact Path Drift](#bootstrap-artifact-path-drift), [Review Lease Orphaning Without Automatic Reclamation](#review-lease-orphaning-without-automatic-reclamation), [Self-Reported Validation](#self-reported-validation), [Hypothesis Exhaustion Without Root Cause](#hypothesis-exhaustion-without-root-cause), and [Supervisor Contention](#supervisor-contention).
- **2026-08-20:** [Integration Closure Is Not Revalidated](#integration-closure-is-not-revalidated).

---

## Fixed (Traceability)

| Issue | Category | Verified |
|-------|----------|----------|
| [MCP Admin Handler Authorization Gap](#mcp-admin-handler-authorization-gap) | FRAGILITY | 2026-07-25 — superseded by removal of the complete MCP surface (`90c132d5`) |
| [Unbounded Integration Test Execution](#unbounded-integration-test-execution) | STRESS POINT | 2026-03-11 — `exec.CommandContext` with `DefaultIntegrationTestTimeout` (10m) + process group kill |
| [MCP Cross-Layer Read Dependency](#mcp-cross-layer-read-dependency) | TENSION | 2026-04-13 — MCP server removed (`90c132d5`, ADR-0057); single consumer (CLI) eliminates cross-layer concern |
| [Role Pair Field as Single Point of Configuration Truth](#role-pair-field-as-single-point-of-configuration-truth) | LOAD-BEARING | 2026-07-25 — pipeline load and task creation validate role-pair references (`581d377f`, `a944248b`) |
| [Task Type Registry Only Supports Coding Workflows](#task-type-registry-only-supports-coding-workflows) | TENSION | 2026-07-25 — six task workflows cover all deployed doer/reviewer pairs (`eb2d271c`, `6de0aba7`) |
| [Task Type Registry is Partial Abstraction](#task-type-registry-is-partial-abstraction) | TRAJECTORY | 2026-07-25 — claimability derives roles and lifecycle states from pipeline role-pairs (`581d377f`) |
| [Orchestrator Role Dissolution Without Replacement](#orchestrator-role-dissolution-without-replacement) | TENSION | 2026-07-25 — roles and pipeline define a unique system-level orchestrator (`540026e8`) |
| [Supervisor Wait-Claim-Spawn Loop](#supervisor-wait-claim-spawn-loop) | FEEDBACK | 2026-07-25 — watchdog and no-progress/spinning guards block non-advancing work (`2bb71dad`, `491d2079`) |
| [Implicit Orchestrator Provenance Default](#implicit-orchestrator-provenance-default) | ASSUMPTION | 2026-07-25 — task creation requires a resolved registered identity (`40f407c2`) |
| [Orchestrator State Change Verification is Non-Binding](#orchestrator-state-change-verification-is-non-binding) | ASSUMPTION | 2026-07-25 — orchestrator spinning detection stops repeated no-progress wakes (`491d2079`) |
| [`one-to-one` Transition Child Field Generation Unspecified](#one-to-one-transition-child-field-generation-unspecified) | ASSUMPTION | 2026-07-25 — `buildOneToOneChild` implements deterministic child construction (`16385d96`) |
| [Cache Coherence Gap in Multi-Process Deployments](#cache-coherence-gap-in-multi-process-deployments) | STRESS POINT | 2026-07-25 — every cached read stats the shared state path before reuse |
| [Bootstrap Artifact Path Drift](#bootstrap-artifact-path-drift) | FRAGILITY | 2026-07-25 — `docs/USAGE.md` exists and active references use the canonical Vision path (`2533585f`) |
| [Review Lease Orphaning Without Automatic Reclamation](#review-lease-orphaning-without-automatic-reclamation) | FRAGILITY | 2026-07-25 — reviewer startup and wait paths reclaim expired claims (`cb6f12d9`, `ebc3f5bd`) |
| [Self-Reported Validation](#self-reported-validation) | ACCEPTED v1 | 2026-07-25 — reviewers must execute task-declared canonical validation (`1e88541f`, `03e2d19f`) |
| [Hypothesis Exhaustion Without Root Cause](#hypothesis-exhaustion-without-root-cause) | FEEDBACK | 2026-07-25 — rescoping requires root cause in role contract and audit log (`99216a33`) |
| [Supervisor Contention](#supervisor-contention) | STRESS POINT | 2026-07-25 — supervisors are per-agent; blackboard writes lock and integration updates use CAS |
| [Integration Closure Is Not Revalidated](#integration-closure-is-not-revalidated) | FEEDBACK | 2026-08-20 — ADR-0113; `TestSlicedIntegrationLifecycle` and `TestSlicedIntegrationFinalizationRace`; merge commit `84159a1eb0593e2e4eb5288e9ed6c65ae4776a4d` |

---

## Fix Details

### MCP Admin Handler Authorization Gap

**Resolution:** The historical handler-level authorization gap no longer has a runtime surface because MCP was removed in `90c132d5` under ADR-0057. Agents use the CLI JSON interface.

**Evidence:** No MCP server or admin-handler registration remains in the current application.

### Unbounded Integration Test Execution

**Resolution:** Integration commands execute with `DefaultIntegrationTestTimeout` (10 minutes), `exec.CommandContext`, a separate process group, and bounded shutdown behavior.

**Evidence:** The current integration execution path applies the timeout and terminates the command's process group.

### MCP Cross-Layer Read Dependency

**Resolution:** Removing MCP in `90c132d5` removed the `mcp → commands` read dependency. The CLI is the remaining presentation consumer; the broader absence of a query layer remains tracked separately.

**Evidence:** ADR-0057 and the current package graph contain no MCP server package.

### Role Pair Field as Single Point of Configuration Truth

**Resolution:** The role-pair remains authoritative, but it is no longer an unvalidated string boundary. Pipeline loading uses strict YAML fields and validates roles, role-pairs, unique states, sub-pipeline membership, and transitions. `AddTask` rejects missing or unknown role-pairs and task-type mismatches.

**Evidence:** `internal/pipeline/config.go` and `internal/ops/add_tasks.go`; commits `581d377f` and `a944248b`.

### Task Type Registry Only Supports Coding Workflows

**Resolution:** `taskWorkflows` now registers coding, planning, epic-planning, user-story writing, integration, and architecture workflows. Pre-implementation and integration work use the same task system as coding.

**Evidence:** `internal/models/task.go`; commits `eb2d271c` and `6de0aba7`.

### Task Type Registry is Partial Abstraction

**Resolution:** `Task.IsClaimable` no longer hardcodes a task-type/status switch. It resolves the doer, reviewer, and lifecycle states through the task's pipeline role-pair.

**Evidence:** `internal/models/task.go`; commit `581d377f`.

### Orchestrator Role Dissolution Without Replacement

**Resolution:** The Orchestrator is explicitly the system-level coordination role rather than a code-planning role. The role contract assigns rescoping, blocked-review resolution, discovery disposition, and goal alignment; pipeline configuration declares one orchestrator outside the doer/reviewer role-pairs.

**Evidence:** `specs/architecture/roles.md` and `internal/embedded/pipeline.yaml`; commit `540026e8`.

### Supervisor Wait-Claim-Spawn Loop

**Resolution:** Execution progress watchdogs and successful-no-progress/spinning trackers now detect repeated runs that do not advance task or worktree state and block the affected task instead of cycling indefinitely.

**Evidence:** `internal/agent/progress_watchdog.go`, `internal/agent/supervisor.go`, and their tests; commits `2bb71dad` and `491d2079`.

### Implicit Orchestrator Provenance Default

**Resolution:** The synthetic `orchestrator-1` default was removed. CLI commands resolve an explicit flag/environment identity or the registered orchestrator from state, while `ops.AddTask` rejects an empty orchestrator ID.

**Evidence:** `cmd/liza/main.go` and `internal/ops/add_tasks.go`; commit `40f407c2`.

### Orchestrator State Change Verification is Non-Binding

**Resolution:** Warning-only post-run verification is backed by an orchestrator-specific spinning tracker. Repeated wake cycles with the same progress signature are stopped rather than allowed to consume resources indefinitely.

**Evidence:** `internal/agent/systemctl.go`, `internal/agent/supervisor.go`, and associated spinning tests; commit `491d2079`.

### `one-to-one` Transition Child Field Generation Unspecified

**Resolution:** `buildOneToOneChild` deterministically constructs the child from the transition definition and parent task, preserving applicable references, priority, dependencies, and lifecycle metadata.

**Evidence:** `internal/ops/proceed.go` and one-to-one transition tests; commit `16385d96`.

### Cache Coherence Gap in Multi-Process Deployments

**Resolution:** `ReadCached()` performs `os.Stat` on the shared `state.yaml` path for every call and reloads bytes when the file modification time changes. Independent processes therefore observe atomic writes without process-to-process invalidation messages.

**Evidence:** `internal/db/blackboard.go` and cache invalidation/external-modification tests.

### Bootstrap Artifact Path Drift

**Resolution:** The compatibility entry point `docs/USAGE.md` exists, and active repository references use `specs/build/1 - Vision.md`. Remaining references to the former usage arrangement are confined to archived historical contracts.

**Evidence:** Commit `2533585f` and current repository path/reference search.

### Review Lease Orphaning Without Automatic Reclamation

**Resolution:** Reviewer registration clears expired claims, and the reviewer strategy repeats that reclamation before waiting for work. Graceful unregister also releases active review claims. A new reviewer demand therefore repairs a crashed review lease automatically.

**Evidence:** `internal/agent/registration.go`, `internal/agent/strategy_reviewer.go`, and `internal/ops/clear_stale_review_claims.go`; commits `cb6f12d9` and `ebc3f5bd`.

### Self-Reported Validation

**Resolution:** Reviewer contracts now require running validation commands. Tasks can carry canonical `validation[]` commands, and the same executable guidance is rendered to doers and reviewers.

**Evidence:** `specs/architecture/roles.md` and prompt validation blocks; commits `1e88541f` and `03e2d19f`.

### Hypothesis Exhaustion Without Root Cause

**Resolution:** Orchestrator rescoping after hypothesis exhaustion must include what failed and why in both `rescope_reason` and the audit log.

**Evidence:** `specs/architecture/roles.md`; commit `99216a33`.

### Supervisor Contention

**Resolution:** The original issue assumed one central supervisor process. The deployed model runs a supervisor per agent. Shared blackboard mutations are serialized with file locking, while concurrent integration updates use working-tree-less compare-and-swap merges with bounded retries.

**Evidence:** `internal/agent/supervisor.go`, `internal/db/blackboard.go`, and `internal/ops/wt_merge.go`.

### Integration Closure Is Not Revalidated

**Resolution:** ADR-0113 supersedes the accepted no-rescan limitation with independent, bounded global rescans after integration repairs and ties successful completion to clean evidence for the current integration HEAD. Its linearizable finalization and mutation-side invalidation prevent a concurrent HEAD change from leaving stale success.

**Evidence:** `TestSlicedIntegrationLifecycle` and `TestSlicedIntegrationFinalizationRace` both passed for the merged acceptance task at merge commit `84159a1eb0593e2e4eb5288e9ed6c65ae4776a4d`, proving repair rescans, generation exhaustion, current-HEAD closure, and both finalization race orderings.
