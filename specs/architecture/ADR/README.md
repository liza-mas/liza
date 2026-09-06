# Architecture Decision Records

| ADR | Decision |
|-----|----------|
| [0001 — Leverage Proven Contract for MAS](0001-leverage-proven-contract-for-mas.md) | Port the pairing contract to multi-agent, using peer approval instead of human gates. |
| [0002 — Skills as Lean Prompts](0002-skills-as-lean-prompts.md) | Separate methodologies into pluggable skill modules loaded on demand. |
| [0003 — Blackboard Coordination](0003-blackboard-coordination.md) | File-based blackboard in `.liza/` for agent coordination with atomic operations and audit trail. |
| [0004 — Dual-Mode Contract Architecture](0004-dual-mode-contract-architecture.md) | Split into CORE.md (shared) plus mode-specific annexes with auto-detection. |
| [0005 — Bash Scripts for POC](0005-bash-scripts-for-poc.md) | Use bash for initial orchestration POC; plan to reconsider for production. |
| [0006 — Supervisor-Assigns-Work](0006-supervisor-assigns-work.md) | Pre-assign tasks before spawning agents, eliminating race conditions. |
| [0007 — TDD Enforcement in MAS](0007-tdd-enforcement-in-mas.md) | Mandate test-driven development with tests written first against `done_when` criteria. |
| [0008 — Multi-LLM Provider Support](0008-multi-llm-provider-support.md) | Direct integration of multiple LLM providers with documented compliance levels. |
| [0009 — Canonical Contract Root](0009-canonical-contract-root.md) | Centralize contracts and skills in `~/.liza/` with minimal symlinks to repos. |
| [0010 — Loop Detection Self-Abort](0010-loop-detection-self-abort.md) | Agents self-detect repetitive loops and abort with role-appropriate actions. |
| [0011 — Script-Enforced Agent Status](0011-script-enforced-agent-status.md) | Move status management into scripts that atomically set status alongside task state. |
| [0012 — Go CLI Replaces Bash Scripts](0012-go-cli-replaces-bash-scripts.md) | Replace 18+ bash scripts with a single Go binary (`liza`) using cobra subcommands. |
| [0013 — Coach and Challenger Modes](0013-coach-and-challenger-collaboration-modes.md) | Add Coach (Socratic questioning) and Challenger (adversarial stress-test) collaboration modes. |
| [0014 — Tiered Context Degradation](0014-tiered-context-degradation.md) | Three-tier context management (Full → Working Set → Kernel) with explicit transitions. |
| [0015 — Subagent Mode First-Class](0015-subagent-mode-first-class.md) | Dedicated SUBAGENT_MODE.md with lightweight contract for read-only research agents. |
| [0016 — Embedded Template Architecture](0016-embedded-template-architecture.md) | Embed all agent prompts as Go templates in the binary, separated from logic. |
| [0017 — Release Infrastructure](0017-release-infrastructure.md) | GoReleaser with GitHub Releases and curl-pipe-sh installer for cross-platform distribution. |
| [0018 — Two-Step Deployment](0018-two-step-deployment.md) | Split into `liza setup` (global to `~/.liza/`) and `liza init` (project-local scaffold). |
| [0019 — Task Lifecycle State Machine Evolution](0019-task-lifecycle-state-machine-evolution.md) | Rename states to activity-descriptive names (READY/IMPLEMENTING/REVIEWING). |
| [0020 — Explicit Task Workflow Contract](0020-explicit-task-workflow-contract.md) | Centralize lifecycle rules in a declared transition graph; escalate exhausted loops to BLOCKED. |
| [0021 — Ops Service Layer for Mutations](0021-ops-service-layer-for-mutations.md) | Extract mutation logic into `internal/ops` used by CLI, agents, and MCP handlers. |
| [0022 — Concurrency Hardening](0022-concurrency-hardening-singleton-blackboard-and-cas-merges.md) | Per-path singleton blackboard and working-tree-less CAS merges for safe concurrency. |
| [0023 — Crash Recovery Commands](0023-crash-recovery-commands.md) | `liza recover-agent` and `liza recover-task` for automated single-command recovery. |
| [0024 — Unified Role Constants Package](0024-unified-role-constants-package.md) | `internal/roles` with constants and bidirectional mapping between naming conventions. |
| [0025 — State Validation Extraction](0025-state-validation-extraction.md) | Shared `internal/statevalidate` package accessible by both CLI and MCP handlers. |
| [0026 — Role-Specific Prompt Templates](0026-role-specific-prompt-templates.md) | Per-role templates with only relevant transitions and tools, reducing prompt size by 58%. |
| [0027 — Contract Compression for MAM Context](0027-contract-compression-for-mam-context.md) | Remove pairing-specific content from CORE.md for multi-agent use, reducing context by 9%. |
| [0028 — Multi-Sprint Support](0028-multi-sprint-support.md) | Automatic sprint advancement with archive-before-mutate and lightweight history. |
| [0029 — Agent Log Analysis Tools](0029-agent-log-analysis-tools.md) | Default-on agent output logging (`--no-log` to disable) with Python/HTML analysis tools for token waste and struggle detection. |
| [0030 — Code-Enforced Agent Guardrails](0030-code-enforced-agent-guardrails.md) | Move role boundary, TDD, and checkpoint enforcement from prompts to Go code validation. |
| [0031 — Configurable Post-Worktree Command](0031-configurable-post-worktree-command.md) | Replace hardcoded build setup with configurable `PostWorktreeCmd` for any stack. Failure handling superseded by ADR-0117. |
| [0032 — Project-Specific Guardrails](0032-project-specific-guardrails.md) | GUARDRAILS.md at project root with Tier 0-3 constraints reusing CORE.md enforcement. |
| [0033 — Orchestrator Role Rename](0033-orchestrator-role-rename.md) | Rename "Planner" to "Orchestrator" to clarify coordination responsibilities. |
| [0034 — Spec and Story Writing Skills](0034-spec-and-story-writing-skills.md) | Two reusable skills: detailed-spec-writing (SMARC + PRD) and user-story-writing (SMARC + anti-patterns). |
| [0035 — Declarative Sub-Pipelines](0035-declarative-sub-pipelines.md) | YAML configuration for pipeline structure supporting arbitrary multi-phase workflows. |
| [0036 — Structured Task Output and Scope Extensions](0036-structured-task-output-and-scope-extensions.md) | Structured `output[]` for inter-pipeline data flow and `scope_extensions` for scope negotiation. |
| [0037 — Rebase Conflict Detection](0037-rebase-conflict-detection.md) | Programmatic conflict detection at submission, auto-transitioning to INTEGRATION_FAILED. |
| [0038 — Phase 2 Roles](0038-phase-2-roles.md) | Six new domain-specific roles (code-planner, epic-planner, us-writer, and their reviewers). |
| [0039 — MCP Role-Based Access Control](0039-mcp-role-based-access-control.md) | Role validation on MCP handlers to match CLI access control and prevent privilege escalation. |
| [0040 — Legacy Pipeline Removal](0040-legacy-pipeline-removal.md) | Remove all dual-path code; make pipeline configuration mandatory. |
| [0041 — RoleStrategy Pattern](0041-role-strategy-pattern.md) | `RoleStrategy` interface with category implementations replacing 9-way switch chains. |
| [0042 — Generic Claim-Type Vocabulary](0042-generic-claim-type-vocabulary.md) | Rename `coder`/`code-reviewer` to `doer`/`reviewer` across all layers. |
| [0043 — MCP Middleware and Declarative Registration](0043-mcp-middleware-and-declarative-registration.md) | Middleware chain and declarative `toolDef` metadata eliminating inline boilerplate. |
| [0044 — Task Event Constants](0044-task-event-constants.md) | Centralized `TaskEventName` type with 26 constants replacing scattered string literals. |
| [0045 — Declarative Role Definitions](0045-declarative-role-definitions.md) | Pipeline YAML `roles` section defining role properties declaratively instead of Go constants. |
| [0046 — Review Quorum](0046-review-quorum.md) | Configurable multi-reviewer approval with provider-diversity and impact-based escalation. |
| [0047 — Dual Name Elimination](0047-dual-name-elimination.md) | Unified all constants to hyphenated form with `liza migrate` as safety net. |
| [0048 — Multi-Phase Planning](0048-multi-phase-planning.md) | Multi-phase planning with phase-gate dependency propagation, topo-sorted execution, and planning checkpoints. |
| [0049 — Structured Handoff Events](0049-structured-handoff-events.md) | Per-task append-only HandoffEvent array replacing State.Handoff map, with three lifecycle triggers. |
| [0050 — Brownfield-Safe Initialization](0050-brownfield-safe-initialization.md) | Global fallback symlinks for existing projects and Node.js auto-detection. |
| [0051 — First-Class Attempt Model](0051-first-class-attempt-model.md) | Structural attempt lifecycle replacing identity-based reassignment, with 3-phase transition and sentinel guards. |
| [0052 — Bubbletea TUI](0052-bubbletea-tui.md) | Interactive Bubbletea TUI replacing console.sh and headless monitoring with live dashboard and keyboard commands. |
| [0053 — Supervisor Resilience](0053-supervisor-resilience-automated-failure-detection.md) | Three-layer automated failure detection (quota exhaustion, crash loops, spinning) preventing infinite supervisor restart cycles. |
| [0054 — Blocking Await Primitives](0054-blocking-await-primitives-for-review-flow.md) | Session-persistent blocking await tools (`await_verdict`, `await_resubmission`) eliminating cold restarts across review cycles. |
| [0055 — Integration Sub-Pipeline](0055-integration-sub-pipeline.md) | Automated branch-wide integration analysis with clean terminal states, auto-transitions, and fix-task generation reusing the coding-pair. |
| [0056 — Architecture Step with Many-to-One Transitions](0056-architecture-step-many-to-one-transitions.md) | Architecture consolidation point with new `many-to-one` fan-in cardinality, `arch_ref` propagation, and multi-parent task linkage. |
| [0057 — MCP Server Removal and CLI-Native Access Control](0057-mcp-server-removal-cli-native-access-control.md) | Remove MCP server entirely; move RBAC to CLI `--agent-id` validation, add `--json` structured output. Supersedes ADR-0039, ADR-0043. |
| [0058 — Output Entry Concrete Task Dependencies](0058-output-entry-task-dependencies.md) | Add `task_depends_on` on `OutputEntry` so generated child tasks can depend on existing concrete task IDs while `depends_on` stays sibling-index-only. |
| [0059 — Partial Planning Handoff](0059-partial-planning-handoff.md) | Decouple planning-output readiness from whole-sprint planning completion so ready outputs can generate implementation work mid-sprint. |
| [0060 — Agent Execution Progress Watchdog](0060-agent-execution-progress-watchdog.md) | Detect stalled assigned executions from observable task progress, not heartbeat/process liveness alone. |
| [0061 — Flock-Only Lock Authority](0061-flock-only-lock-authority.md) | Treat kernel `flock` acquisition as authoritative and make PID lock metadata diagnostic-only. |
| [0062 — Ghost Agent Claim Prevention and Ownership Reconciliation](0062-ghost-agent-claim-prevention-and-ownership-reconciliation.md) | Reject corrupt agent identities from claims and enforce task/agent ownership invariants with conservative repair. |
| [0063 — Blocked Task Alerts and Re-Wake](0063-blocked-task-alerts-and-rewake.md) | Make blocked-task transitions visible through canonical alerts and dependency wake diagnostics. |
| [0064 — Review Boundary Recovery](0064-review-boundary-recovery.md) | Treat review commit/worktree HEAD mismatch as a hard boundary failure and recover stale candidates through integration failure. |
| [0065 — Recursive Superseded Dependency Resolution](0065-recursive-superseded-dependency-resolution.md) | Resolve dependencies through supersession chains so replacement work, not superseded tasks, controls downstream claimability. |
| [0066 — Architecture Sub-Pipeline and Spec Entry Points](0066-architecture-subpipeline-entry-points.md) | Extract architecture into its own sub-pipeline and distinguish functional-spec, technical-spec, and legacy detailed-spec entry points. |
| [0067 — Master Planning Task Pattern](0067-master-planning-task-pattern.md) | Add reviewed master planning tasks before fan-out, including topology-driven per-output RCA classification for code-planning decomposition. |
| [0068 — Optional Repository Indexing with SCIP and Stacklit](0068-optional-repository-indexing-with-scip-and-stacklit.md) | Add opt-in SCIP and Stacklit index refresh with explicit prompt paths for worktree-safe repository navigation. |
| [0070 — Active Task Cancellation](0070-active-task-cancellation.md) | Allow invariant-checked cancellation of active tasks before approval while preserving approved-merge boundaries. |
| [0071 — Automatic Checkpoint Summary on Merge](0071-automatic-checkpoint-summary-on-merge.md) | Auto-run checkpoint-summary after successful merges as a best-effort steering context artifact. |
| [0072 — Declared Validation Commands](0072-declared-validation-commands.md) | Store validation commands on tasks and generated outputs so agents validate against explicit executable contracts. |
| [0073 — Adversarial Pairing Blackboard](0073-adversarial-pairing-blackboard.md) | Add a lightweight Markdown blackboard and locked writer for separate doer/reviewer pairing sessions. |
| [0074 — SessionStart Context Hooks](0074-sessionstart-context-hooks.md) | Use provider SessionStart hooks to emit initialization guidance and explicit repo index context before first agent action. |
| [0075 — Retarget Dependency Repair](0075-retarget-dependency-repair.md) | Add a first-class repair command for retargeting stale direct task dependencies without superseding dependent tasks. |
| [0076 — Candidate Artifact Reference Guard](0076-candidate-artifact-reference-guard.md) | Validate protected artifact refs against the candidate Git tree before advancing the integration ref. |
| [0077 — Dependency Edge Canonicalization](0077-dependency-edge-canonicalization.md) | Canonicalize dependency edges at mutation and transition time instead of resolving supersession chains at read time. |
| [0078 — Repairable Review Boundary Metadata](0078-repairable-review-boundary-metadata.md) | Treat stale review commit/base metadata as repairable drift while preserving exact review-boundary validation. |
| [0079 — Semble Semantic Repository Search](0079-semble-semantic-repository-search.md) | Add strict opt-in Semble semantic discovery with offline readiness, target-root safety, and source-read verification boundaries. |
| [0080 — Claimable Rebase Unblock](0080-claimable-rebase-unblock.md) | Let repaired blocked tasks rebase preserved worktrees onto integration and return to claimable status without requiring direct agent assignment. |
| [0081 — Indexing Activation for Setup and Init](0081-indexing-activation-for-setup-and-init.md) | Split optional-index activation between global setup guidance and project-local init artifacts, hooks, and session metadata. |
| [0082 — Worktree Env-File Provisioning](0082-worktree-env-file-provisioning.md) | Add opt-in copying of ignored project-root environment files into Liza worktrees before post-worktree setup. |
| [0083 — Preserve-by-Default recover-task](0083-preserve-by-default-recover-task.md) | Preserve or validate recoverable task work by default; require explicit `--fresh` for destructive reset. |
| [0084 — Destructive DB Validation Marker](0084-destructive-db-validation-marker.md) | Add `destructive_db` metadata requiring a leading break-glass marker on every destructive DB validation command. |
| [0085 — LLMAgent Boundary and ACP Observability](0085-llm-agent-boundary-and-acp-observability.md) | Add a provider-neutral LLMAgent boundary with CLIAgent as the OSS backend and an event stream for ACP trajectory observability. |
| [0086 — Local Support Toolchain](0086-local-support-toolchain.md) | Add profile-based installation, diagnostics, and shell activation for local support tools. |
| [0087 — Terminal-Multiplexer Launchers](0087-terminal-multiplexer-launchers.md) | Launch MAS and adversarial-pairing sessions through supported terminal multiplexers. |
| [0088 — Aggregate SCIP Indexes Across Language Roots](0088-aggregate-scip-indexes-across-language-roots.md) | Aggregate per-root SCIP indexes so multi-root repositories receive usable language indexes. |
| [0089 — Explicit Project-Root Selection](0089-explicit-project-root-selection.md) | Resolve Liza project roots independently of CWD for supported task-worktree execution. |
| [0090 — Opt-In Update Checks and Self-Update](0090-opt-in-update-checks-and-self-update.md) | Add persisted update settings and verified self-update behavior to the CLI. |
| [0091 — Optional Bash-Policy Initialization](0091-optional-bash-policy-initialization.md) | Delegate AST-based provider policy setup to the standalone bash-policy CLI. |
| [0092 — Build-Time White-Label Branding](0092-build-time-white-label-branding.md) | Parameterize end-user branding while preserving Liza's structural Go identity. |
| [0093 — Shell Completion From Command Metadata](0093-shell-completion-from-command-metadata.md) | Add command-aware shell completion with toolchain-managed activation. |
| [0094 — Entry-Point Input-Readiness Assessment](0094-entry-point-input-readiness-assessment.md) | Review whether an input is ready for its selected MAS entry point before initialization. |
| [0095 — Functional-Cluster Index Lifecycle](0095-functional-cluster-index-lifecycle.md) | Refresh optional target-local functional-cluster artifacts alongside Stacklit and SCIP indexes. |
| [0096 — Catalog-Backed Provider Registry](0096-catalog-backed-provider-registry.md) | Move provider behavior from hardcoded branches into declarative catalog metadata. |
| [0097 — Cursor Secondary-Provider Policy Hooks](0097-cursor-secondary-provider-policy-hooks.md) | Configure Cursor's dependent provider setup and policy hooks during initialization. |
| [0111 — Capability-Aware Global-First Contract Activation](0111-capability-aware-global-first-contract-activation.md) | Prefer documented active global instruction paths without deleting the only usable provider contract. |
| [0112 — Serialize Integration Working-Tree Mutations](0112-serialize-integration-working-tree-mutations.md) | Use a project-scoped file lock for integration ref advancement and shared-index sync/restore. |
| [0113 — Sliced Integration Analysis and Final Closure](0113-sliced-integration-analysis-and-final-closure.md) | Add bounded per-scope coverage, independent global rescans, and linearizable current-HEAD closure. |
| [0114 — Terminal Dependency Repair](0114-terminal-dependency-repair.md) | Prune illegal downstream edges during supersession and repair existing SUPERSEDED task metadata through one audited transaction. |
| [0115 — Declarative Atomic Dependency Repairs](0115-declarative-atomic-dependency-repairs.md) | Persist command-free dependency repair requests through `--repair-request-file` and consume them atomically with `apply-dependency-repair`. |
| [0116 — Orchestrator Classifies Defect Objectives](0116-orchestrator-classifies-defect-objectives.md) | Define task-level RCA defaults plus explicit per-output overrides so each specialized code-planning scope gets the right reviewed diagnosis requirement. |
| [0117 — Fail-Closed Worktree Readiness](0117-fail-closed-worktree-readiness.md) | A configured `post_worktree_cmd` must succeed before a provider session starts; failure degrades the agent instead of warning. Supersedes ADR-0031's failure handling. |
| [0118 — Native Windows Support](0118-native-windows-support.md) | Support Windows natively, requiring Git for Windows for POSIX hooks and falling back where symlinks are unavailable. |
| [0121 — Unified Provider Catalog with Synthesized ACP Variants](0121-unified-provider-runtime-catalog.md) | Declare shared provider metadata once and synthesize independently addressable ACP runtime variants. |
| [0123 — Two-Sided Review with Bounded Convergence](0123-two-sided-bounded-review.md) | Define author and reviewer obligations, bound corrective scope, and route unresolved disagreements without narrowing critical-defect inspection. |
| [0124 — User-Owned Provider Preferences and Explicit MAS Launch Policy](0124-user-owned-provider-preferences.md) | Leave personal provider preferences to users and express required MAS permission policy at launch. |
| [0125 — Safe Project Reset Through a Lifecycle Lock](0125-safe-project-reset-lifecycle-lock.md) | Confirm cleanup targets and revalidate under an exclusive lifecycle lock shared with resource-creating operations. |
| [0129 — Proportional Circuit-Breaker Responses with Explicit Acknowledgement](0129-proportional-circuit-breaker-responses.md) | Separate warning, checkpoint, and halt responses and retain actionable interventions until explicit acknowledgement. |
| [0130 — Generation-Fenced Agent Authority and Recovery](0130-generation-fenced-agent-authority.md) | Fence lifecycle writes and provider starts by registration generation while preserving lease and review evidence during recovery. |
| [0131 — Serialize Repository Worktree Metadata Mutations](0131-repository-worktree-mutation-lock.md) | Serialize repository worktree metadata mutations through a cross-process lock at the Git wrapper boundary. |
| [0132 — Human-Owned Goal Decisions with Independent Readiness Review](0132-human-owned-goal-decisions.md) | Elicit human-owned decisions before drafting and require independent final readiness review. |
