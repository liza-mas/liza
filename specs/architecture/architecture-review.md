# Architecture Review — Liza

**Date:** 2026-07-25
**Targeted health check:** 2026-09-06 refreshed file sizes and test-structure counts; historical function and coverage measurements remain dated evidence. No issue was closed.
**Mode:** Adversarial (after pass 22)
**Reviewer:** software-architecture-review skill

---

## Update Policy

1. This document is a current-state architecture snapshot, not a cumulative pass log.
2. Git history preserves prior reviews and resolved findings.
3. `Phase 3: Recommendations` contains only concerns verified against the current tree.
4. [`architectural-issues.md`](architectural-issues.md#open-issues-summary) is the sole lifecycle authority for architectural findings. This review verifies and prioritizes current evidence; open/resolved status changes in the registry.
5. File-size measurements are physical Go source lines from `cmd/` and `internal/`, including comments and blank lines. Aggregate repository LOC uses all tracked Go files and links its dated source. Both exclude non-Go artifacts such as prompt templates, so file-size tables alone under-report complexity — see 1.4.
6. Coverage percentages are reported only from a complete, representative coverage run. Pass 20 used `go test -coverpkg=./... ./...` with blocks deduplicated by block key across test binaries, but the raw profiles and deduplication implementation were not retained. Those figures are historical evidence, not a reproducible current baseline — see 2.6.
7. The Summary synthesizes current risks rather than retaining a paragraph-per-pass history; Git preserves pass evolution.

## Table of Contents

- [Phase 1: Discovery](#phase-1-discovery)
  - [1.1 Overview](#11-overview)
  - [1.2 Component Walkthrough](#12-component-walkthrough)
  - [1.3 Dependency Map](#13-dependency-map)
  - [1.4 Coverage Checkpoint](#14-coverage-checkpoint)
  - [1.5 Pass 22 Verification and Gap Analysis](#15-pass-22-verification-and-gap-analysis)
  - [1.6 Adversarial Smell-Driven Gap Hunt](#16-adversarial-smell-driven-gap-hunt)
- [Phase 2: Analysis](#phase-2-analysis)
  - [2.1 Analysis Framework](#21-analysis-framework)
  - [2.2 Strengths](#22-strengths)
  - [2.3 Current Smells and Tensions](#23-current-smells-and-tensions)
  - [2.4 Resolved Since the Prior Review](#24-resolved-since-the-prior-review)
  - [2.5 Patterns](#25-patterns)
  - [2.6 Test Coverage](#26-test-coverage)
- [Phase 3: Recommendations](#phase-3-recommendations)
- [Summary](#summary)
- [Appendix: File Reference](#appendix-file-reference)

## Phase 1: Discovery

### 1.1 Overview

Liza is a stack-agnostic, peer-supervised multi-agent orchestration system implemented in Go. It coordinates planning, execution, review, and integration through a YAML blackboard, declarative pipeline configuration, per-task Git worktrees, and supervisor-managed external coding agents.

```text
Requirements  →  Initialization  →  Task graph  →  Agent execution  →  Review  →  Integration
     ↓                 ↓                ↓               ↓                ↓             ↓
 specs/docs       pipeline config    state.yaml      worktrees       verdicts      target branch
```

The primary runtime is the `liza` CLI. Cobra commands and the Bubble Tea TUI call application operations; those operations coordinate the domain model, blackboard persistence, Git worktrees, pipeline rules, provider profiles, and agent processes.

**Dated size baseline**

| Measure | Current value |
|---------|---------------|
| Go version | 1.25.5 |
| Production Go source | 65,509 lines (2026-07-24) |
| Test Go source | 150,683 lines (2026-07-24) |
| Test-to-production ratio | 2.30:1 (2026-07-24) |
| Primary binary | `cmd/liza/` |
| State authority | `.liza/state.yaml` through `internal/db.Blackboard` |

The former MCP server and `liza-mcp` binary are no longer part of the architecture (ADR-0057).

### 1.2 Component Walkthrough

#### Domain model (`internal/models/`, `internal/roles/`, `internal/taskkind/`)

**Purpose:** Defines task, agent, sprint, history, lease, configuration, and system-mode concepts.

**Pattern:** State-machine-oriented domain model with pipeline-aware status interpretation.

**Observations:**

- `models.State` is the serialized blackboard aggregate.
- Task lifecycle vocabulary is split between stable model types and declarative pipeline resolution.
- Canonical role naming now lives in `internal/roles`; the former bidirectional runtime/workflow maps are gone.
- Three pipeline-aware status helpers in `models/task.go` retain the same structural shape.
- `SprintSummary` preserves only `TasksDone`, so other sprint metrics are lost when a sprint is archived.

#### Persistence and validation (`internal/db/`, `internal/filelock/`, `internal/statevalidate/`)

**Purpose:** Provides process-safe blackboard access and state validation.

**Pattern:** Repository-style `Blackboard` with `flock` authority, read-modify-write closures, and atomic replacement.

**Observations:**

- Writes use a temporary file, synchronization, and rename.
- `Modify` keeps state mutation within the lock boundary.
- Filesystem watching observes the containing directory so atomic renames are visible.
- Validation is extensive, but direct tests for the top-level state-file entry points remain less visible than tests for inner validators.
- `internal/statevalidate` measures 89.6% statement coverage over 924 statements; the earlier suspicion that its entry points were untested was a measurement artifact, not a gap. *(pass 20, Coverage lens)*

#### Pipeline and prompt construction (`internal/pipeline/`, `internal/prompts/`, `internal/embedded/`)

**Purpose:** Loads frozen pipeline configuration, resolves lifecycle behavior, builds agent prompts, and installs bundled assets.

**Pattern:** Declarative pipeline plus builder-oriented prompt assembly.

**Observations:**

- Pipeline configuration is loaded per operation, preserving self-contained command behavior.
- Prompt construction still imports application logic from `internal/ops` for several read-only pipeline and planning queries.
- `internal/embedded/embedded.go` is now 1,635 lines and contains multiple artifact families.
- Direct named tests were not found for `PlanGlobalFiles`, `WritePipelineConfig`, and `WriteGuardrails`, but all three are reached indirectly: 90.9%, 100%, and 66.7% statement coverage respectively. Absence of a named test is not absence of coverage. *(pass 20, Coverage lens)*
- Two prompt template blocks dominate the template tree: `blocks/review_instructions.tmpl` (392 lines, 26.7 KB) and `blocks/implementation_phase.tmpl` (240 lines, 20.8 KB), together about 36% of all template bytes and larger than `CORE.md`. *(pass 18, Complexity lens)*
- Both are hardcoded `if/else if` chains over role names with no `else` fallback, so an unrecognized role renders an empty instruction block. *(pass 18, Complexity lens)*

#### Agent runtime and external tools (`internal/agent/`, `internal/providers/`, `internal/toolchain/`, `internal/codexconfig/`)

**Purpose:** Claims work, builds execution plans, launches external agents, monitors progress, and maps configured providers to tool invocations.

**Pattern:** Strategy-based roles, `LLMAgent`/`CLIAgent` boundaries, provider catalog, and data-driven launch profiles.

**Observations:**

- Interfaces include `RoleStrategy`, `LLMAgent`, `CLIExecutor`, state watchers, command runners, and candidate-tree lookups.
- Provider launch behavior is primarily catalog/profile driven; the old monolithic five-tool executor switch is no longer an accurate description.
- A few provider-specific policies remain intentionally explicit, including Claude subagent disabling and the Codex wrapper path.
- The catalog is a launch/profile boundary, not a complete provider-extension boundary: activation assets are provider-named, initialization expands Cursor into Claude/Codex dependencies, and operational failure detectors remain Codex-specific outside the catalog. *(pass 22, Coupling lens)*
- The catalog carries canonical `ProviderKey` metadata into `LaunchPlan`, but no production consumer reads it; supervisor registration persists the raw CLI name instead. *(Adversarial pass)*
- `ApplyYAMLTimeouts` still switches over concrete strategy types instead of using the strategy interface.
- `internal/agent/supervisor.go` is 1,131 lines and remains a high-change orchestration surface. The pass-18 measurement found concentrated complexity: `RunSupervisor` alone was 442 lines, then the longest function in the repository. *(pass 18, Complexity lens)*
- `RunSupervisor` maintains five in-memory loop-detection trackers whose resets are hand-paired across six scattered, asymmetric call sites. All are process-local, so a restarted supervisor resets its own circuit breakers. *(pass 18, Complexity lens)*
- `internal/agent/prompt.go` carries nine near-identical index-adapter functions and three near-identical type families for SCIP, Stacklit, and functional clusters, differing mainly by field name (`Path` vs `IndexPath`). *(pass 18, Complexity lens)*
- The three `available*IndexRefs` helpers discard discovery errors and return `nil`, so agents can silently lose index references from their prompts with no operator signal. *(pass 18, Complexity lens)*
- Quota and provider-unavailable stop signals duplicate the same path/glob/write/check/clear lifecycle in `quota.go` and `provider_unavailable.go`. *(pass 21, Duplication lens)*
- Supervisor registration and heartbeat renewal use a fixed 1,800-second agent lease even when `state.Config.LeaseDuration` differs; registration also records every supervisor as `"terminal-1"`. *(pass 21, Duplication lens)*

#### Application operations (`internal/ops/`)

**Purpose:** Implements task lifecycle operations used by the CLI and agent runtime.

**Pattern:** Service layer around blackboard transactions, pipeline resolution, Git operations, and history recording.

**Observations:**

- `internal/ops/proceed.go` is 1,563 lines and combines graph traversal, recovery, child construction, and dependency propagation.
- `await_verdict.go` and `await_resubmission.go` retain parallel watcher/timer/polling loop structures.
- `AddTasks` accepts state/log paths while most operations accept a project root.
- `SetTaskOutput` accepts an empty `spec_ref`, while proceeding and validation require one. The CLI also describes it as optional.
- Per-subtask fan-out still assigns priority `1`; one-to-one transitions preserve the parent priority.
- The layer contains both the formal project logger and direct `log.Printf` calls.
- Five of the ten longest functions in the repository are `ops` write-path transaction scripts of 200-320 lines (`MergeWorktree`, `SubmitForReview`, `SubmitVerdict`, `ExecuteAvailableTransitions`, `completeClaimTaskAfterValidation`). *(pass 18, Complexity lens)*
- Their read/validate → external effects → mutate-under-lock ordering is expressed as a `// Phase N` comment convention present in only 9 of 73 production files, not as a shared transaction helper. *(pass 18, Complexity lens)*
- Worktree intelligence refresh is assembled independently in claim, create, recovery, and review-submission paths. `wt_create.go` repeats the same four-call sequence in both its existing- and new-worktree branches. *(pass 21, Duplication lens)*

#### Git and worktrees (`internal/git/`, `internal/gitenv/`, `internal/worktreeexclude/`)

**Purpose:** Creates, inspects, rebases, merges, excludes, and removes task worktrees.

**Pattern:** Infrastructure adapters around Git commands and worktree naming.

**Observations:**

- Integration-test execution is bounded by `DefaultIntegrationTestTimeout`, `exec.CommandContext`, process-group termination, and `WaitDelay`.
- Worktree-relative path construction is still duplicated at three inline call sites in addition to the shared helper.
- Some cleanup removals remain best-effort and intentionally do not propagate secondary failures.
- `wt_merge.go` still mixes UTC timestamps with two local `time.Now()` calls.

#### CLI and terminal UI (`cmd/liza/`, `internal/commands/`, `internal/tui/`, `internal/interactive/`)

**Purpose:** Exposes command-line, interactive initialization, and live terminal views.

**Pattern:** Cobra command adapters plus Bubble Tea model/update/view.

**Observations:**

- Commands usually delegate mutations to `internal/ops`.
- `supervision-model.md` still calls `commands` the shared implementation layer used by supervisors and CLI commands, contrary to ADR-0021 and the current `ops`-centered dependency shape. *(pass 22, Coupling lens)*
- `commands/status.go`, `repair_agent_pool.go`, and `resume.go` import `internal/agent`, so the presentation/application boundary is not one-directional.
- CLI and TUI resume adapters independently glob and clear both provider stop-signal families after `ops.Resume`; `ops.Resume` itself does not own that recovery semantic. *(pass 21, Duplication lens)*
- `internal/commands/watch.go` is 1,407 lines and holds twelve top-level timing/retry constants in code.
- `internal/commands/init.go` is 1,418 lines.
- Formatting helpers use the standard library `slices.Sort`; the former hand-written bubble sort is gone.
- `internal/tui/view.go` renders agent and task panels through two structurally duplicated routines (`renderAgentPanel`, 140 lines; `renderTaskPanel`, 204 lines), each declaring its own local `column` type with incompatible closure signatures. *(pass 18, Complexity lens)*
- Both routines contain a verbatim-duplicated stringly-typed `c.header == "STATUS"` ANSI-padding special case. *(pass 18, Complexity lens)*
- `internal/commands/init.go` and `internal/ops/init_project.go` are parallel initialization implementations with near-verbatim duplicated helpers; `InitCommandWithConfig` is 405 lines, the second-longest function in the repository. *(pass 18, Complexity lens)*
- The two paths have diverged on Git invocation: `commands/init.go` uses `internal/gitenv`, while `ops/init_project.go` calls `exec.Command("git", ...)` directly, bypassing the `LC_ALL=C` and timeout guarantees `gitenv` exists to provide. Five raw-Git sites remain across two files versus sixteen files using `gitenv`. *(pass 18, Complexity lens)*

#### Repository intelligence and optional tooling

**Locations:** `internal/scipsearch/`, `internal/stacklit/`, `internal/semble/`, `internal/functionalclusters/`, `internal/pairingindex/`, `internal/statehygiene/`, `internal/envgate/`, `internal/projectdetect/`

**Purpose:** Adds code indexing, semantic exploration, functional-cluster analysis, pairing hooks, environment readiness, state hygiene, and project detection.

**Pattern:** Optional adapters and focused application services around the core orchestration flow.

**Observations:**

- SCIP and Stacklit integration is optional rather than a core blackboard dependency.
- Semble and functional-cluster support add architecture-aware discovery without changing blackboard ownership.
- Pairing-index hooks and environment gates expand initialization and session-start behavior.
- SCIP, Stacklit, and functional clusters independently own similar plan/runner/result contracts; `functionalclusters` directly composes Stacklit and SCIP while `pairingindex` enumerates concrete backends. Adding another index backend therefore crosses several package boundaries. *(pass 22, Coupling lens)*
- `internal/scipsearch/scipsearch.go` is currently the largest production Go file at 1,566 lines.
- POSIX shell quoting has five local implementations; `outputSuffix` has five; bounded failure diagnostics have three with different truncation semantics; and SCIP/Semble duplicate truthy parsing already owned by `internal/envgate`. *(pass 21, Duplication lens)*

#### Operational support packages

**Locations:** `internal/updater/`, `internal/alerts/`, `internal/procscan/`, `internal/secretmask/`, `internal/brand/`, `internal/brandrender/`, `internal/bash-policy-cli/`, `internal/precommit/`, `internal/initcheck/`, `internal/testguard/`

**Purpose:** Provides self-update, alerts, process inspection, secret masking, white-label rendering, bash-policy integration, initialization checks, and quality gates.

**Observations:**

- These packages represent deliberate product expansion since the prior review.
- Provider catalogs, toolchain selection, branding, shell completion, readiness checks, and functional-cluster workflows are backed by ADRs 0085–0097.
- They should be treated as first-class architecture, not omitted as incidental utilities.

#### Shared leaf utilities

**Locations:** `internal/paths/`, `internal/identity/`, `internal/errors/`, `internal/jsonout/`, `internal/render/`, `internal/log/`, `internal/process/`, `internal/termutil/`, `internal/analysis/`

**Purpose:** Centralizes naming, identity parsing, errors, output formats, rendering, logging, process handling, terminal behavior, and pattern analysis.

**Observations:**

- The leaf-package strategy keeps common vocabulary out of larger application packages.
- `agent/registration.go` still duplicates identity validation already owned by `internal/identity`.
- The global project logger remains proportionate to a single-process CLI.

### 1.3 Dependency Map

```text
Presentation / entry points
  cmd/liza ──► commands ──► ops
       │  │       │          │
       │  └───────┼──► ops   │        50 direct sites bypass commands
       │          └──► agent │        known boundary crossing
       └──────► tui          ▼
Agent runtime ─────────────► ops ◄──── prompts
      │         │             │           known inversion
      │         └──► db ──────┤        13 .Modify sites bypass ops
      ├──► providers          ├──► pipeline
      ├──► toolchain          ├──► db ──► filelock
      └──► prompts            ├──► git
                              └──► models

Stable vocabulary / leaf services
  roles · taskkind · paths · identity · errors · render · log

Presentation with business dependency
  jsonout ──► ops · filelock

Optional repository intelligence
  scipsearch · stacklit · semble · functionalclusters · pairingindex
```

The dependency graph remains acyclic. *(pass 19, Boundaries lens)* Four direction exceptions are now visible rather than two: `prompts → ops`, `commands → agent`, `agent → db` (state mutation reaching persistence without passing through `ops`), and `jsonout → ops` (a presentation package importing the application layer to type-assert its errors). Read-only query logic is also distributed across `commands`, `agent`, and `ops`, which increases transitive coupling.

The Coupling lens finds that the expensive dependencies are behavioral rather than cyclic: provider onboarding, stop-state recovery, semantic state validation, and worktree-intelligence refresh each require coordinated changes across otherwise valid package boundaries. *(pass 22, Coupling lens)*

### 1.4 Coverage Checkpoint

#### Large production Go files

Files retained from the prior review, remeasured on 2026-09-06 (not a new repository-wide ranking).

| File | Lines |
|------|------:|
| `internal/embedded/embedded.go` | 1,635 |
| `internal/scipsearch/scipsearch.go` | 1,567 |
| `internal/ops/proceed.go` | 1,563 |
| `cmd/liza/cmd_task.go` | 1,495 |
| `internal/commands/init.go` | 1,418 |
| `internal/commands/watch.go` | 1,407 |
| `internal/updater/updater.go` | 1,323 |
| `cmd/liza/cmd_launch.go` | 1,159 |
| `internal/agent/supervisor.go` | 1,131 |
| `internal/pairingindex/hooks.go` | 1,077 |

#### Longest production Go functions *(pass 18, Complexity lens)*

| Function | Location | Lines |
|----------|----------|------:|
| `RunSupervisor` | `internal/agent/supervisor.go:637` | 442 |
| `InitCommandWithConfig` | `internal/commands/init.go:763` | 405 |
| `MergeWorktree` | `internal/ops/wt_merge.go:491` | 316 |
| `SubmitForReview` | `internal/ops/submit_review.go:43` | 315 |
| `SubmitVerdict` | `internal/ops/submit_verdict.go:93` | 302 |
| `InitProject` | `internal/ops/init_project.go:49` | 222 |
| `ExecuteAvailableTransitions` | `internal/ops/proceed.go:987` | 218 |
| `completeClaimTaskAfterValidation` | `internal/ops/claim_task.go:246` | 209 |
| `renderTaskPanel` | `internal/tui/view.go:402` | 204 |
| `Replan` | `internal/ops/replan.go:35` | 194 |

The historical pass-18 function scan found that file size and function depth did not correlate. Its broad modules decomposed well — `scipsearch.go` (1,566 lines) has no function above 63 lines, `pairingindex/hooks.go` above 76, `commands/watch.go` above 80, and `embedded.go` is an accessor family. Concentrated complexity sits instead in `supervisor.go`, `commands/init.go`, and the `ops` write path.

#### Checkpoint questions

| Question | Current assessment |
|----------|--------------------|
| What exists that should not? | `plugin/acp` (~450 production lines) is a public package under the module path whose only importers are two `internal/agent` performance tests. `plans/`, `plugin/`, and `scripts/` are tracked but absent from `REPOSITORY.md`, and `plugin/` is also outside this document's measurement scope. Coverage measurement adds two exported functions with no caller at all — `commands.UnblockTaskCommand` and `models.(*DependencyResolver).UnmetDependencies`. *(pass 20, Coverage lens)* Current C4 documentation also still models the removed `internal/mcp` subsystem. *(pass 21, Duplication lens)* |
| What is implicit that should be explicit? | Review-submission cleanliness, verdict-time provider diversity, `spec_ref` requirements, and sprint-summary loss are not aligned across contracts and enforcement. Index-discovery failures in `agent/prompt.go` and the role coverage of the prompt template chains are silent at runtime. *(pass 18, Complexity lens)* The coverage contract is implicit throughout: no `-coverpkg`, no `codecov.yml`, no threshold, `fail_ci_if_error: false`. Nothing states what level of coverage the project intends to hold. *(pass 20, Coverage lens)* Worktree-intelligence refresh order and provider-signal cleanup are policies encoded only by repeated call sequences. *(pass 21, Duplication lens)* |
| What was missing from the old walkthrough? | Provider catalogs, toolchains, update support, indexing, Semble, functional clusters, pairing hooks, branding, readiness gates, secret masking, and other post-ADR-0057 packages. |
| What requires cross-file comparison? | Invariants versus operations, prompt queries versus ops ownership, command queries versus agent ownership, worktree path construction, await loops, timestamp handling, identity validation, provider-signal lifecycles, index-refresh sequences, and optional-tool shell/diagnostic helpers. |

### 1.5 Pass 22 Verification and Gap Analysis

Every recommendation in Phase 3 now carries `✓ verified`, `~ adjusted`, or `+ new`; that status column is the per-finding verification record required by the enrichment protocol.

All 64 findings inherited from pass 21 were rechecked against the unchanged production tree. The five pass-21 `+ new` rows are now `✓ verified`, producing 56 verified and 8 adjusted existing findings; none is stale enough to remove.

**New finding from the pass:**

- The provider catalog does not form the complete provider-extension boundary. Full support still spans catalog schema, initialization dependencies and assets, embedded writers, bash-policy mapping, and runtime output detectors.

**Coupling refinements to existing findings:**

- Direct `agent → db` mutation and off-write-path semantic validation are two sides of the same ownership problem: callers must know both persistence mechanics and which invariant subset to invoke.
- Provider stop-state cleanup is coupled to presentation adapters, so calling `ops.Resume` directly does not perform the recovery semantics exposed by CLI and TUI.
- Worktree refresh and optional-tool contracts make repository-intelligence extension a cross-package change rather than an adapter-local change.

**Existing findings not rediscovered independently:** coverage instrumentation, sprint-summary loss, JSON error typing, several low-priority data-contract observations, and most test-structure findings. This was an attention shift rather than contrary evidence; each was rechecked after the merge. Pass-20 coverage figures remain historical evidence because no fresh complete coverage run was made.

**Method limitation:** Phase 1 did not satisfy the strict contamination-free discovery guarantee. An exact repository search surfaced three existing-review snippets before the formal merge phase; the user explicitly waived that requirement and authorized continuation. Pass-22 conclusions are source-verified but are not claimed as fully unanchored discovery.

### 1.6 Adversarial Smell-Driven Gap Hunt

Entry smells were provider support crossing the catalog boundary, duplicated provider stop-state recovery, and provider-diversity enforcement drift.

Forced comparisons covered catalog identity versus supervisor identity, quota versus provider-unavailable/audit normalization, and registration versus merge-gate provider semantics. Existing findings were not reverified or removed.

The new gap is a missing canonical provider identity at runtime policy boundaries: the catalog models one, but registration, review diversity, provider-unavailable signals, and audit anomalies operate on raw tool names.

## Phase 2: Analysis

### 2.1 Analysis Framework

| # | Question | Assessment |
|---|----------|------------|
| 1 | What problem is being solved? | Reliable orchestration of multiple external coding agents through a local, inspectable coordination model. |
| 2 | What are the change vectors? | Provider/tool support, pipeline shapes, task transitions, repository intelligence, initialization, and operational recovery change frequently. Blackboard durability and Git isolation should remain stable. |
| 3 | What are the constraints? | Stack-agnostic target projects, local filesystem/Git authority, multiple processes, human-readable state, and heterogeneous external CLIs. |
| 4 | What is the cost of being wrong? | High for task state, worktree, integration, and recovery operations; lower for presentation and optional discovery tooling. |
| 5 | How are failures handled? | Typed operational errors, blackboard transaction boundaries, history, watchdogs, alerts, and recovery commands. Some cleanup failures remain best-effort. |
| 6 | What is the expected lifespan? | Long-lived product code with active subsystem expansion. |
| 7 | What is the concurrency model? | Multiple OS processes coordinate through `flock`, leases, atomic state writes, filesystem watching, and Git worktrees. |
| 8 | Who owns data and invariants? | `models` defines data; `db.Blackboard` owns persistence serialization and the lock boundary; `pipeline` defines configurable lifecycle semantics. `ops` is the *intended* mutation owner but not the exclusive one — `agent` and `commands` also write state directly. *(adjusted, pass 19)* |
| 9 | Where are the boundaries? | CLI/TUI, agent runtime, operations, domain/pipeline, persistence/Git, providers/tooling, and optional repository intelligence. |
| 10 | What are the runtime constraints? | Local filesystems, Git behavior, terminal processes, external CLI compatibility, bounded waits, and recoverability after interruption. |

### 2.2 Strengths

#### Durable, inspectable coordination

The YAML blackboard and per-task worktrees make coordination observable and recoverable without introducing a remote service. File locking, atomic replacement, leases, and history are appropriate safeguards for that constraint.

#### Declarative lifecycle expansion

Pipeline configuration, role pairs, task kinds, and transition definitions allow new workflows without embedding every lifecycle in the domain model. Recent architecture, readiness, and functional-cluster pipelines demonstrate that the abstraction is carrying real variation.

#### Provider and execution boundaries have matured

The provider catalog, toolchain configuration, launch plans, and `LLMAgent` boundary replace earlier tool-specific branching with data-driven configuration while preserving explicit escape hatches where CLIs genuinely differ.

Pass 22 narrows this strength: runtime launch is substantially data-driven, while setup dependencies, activation assets, and operational failure detection still expose provider identity across packages.

#### Lock scope is respected in practice

`gitenv` warns that callers should avoid invoking Git while holding state locks. A scan of every `.Modify(func` closure in the tree found no Git or `exec` invocation inside the lock boundary, and the largest `ops` write paths sequence Git work explicitly outside it. The discipline is real, not merely documented. *(pass 18, Complexity lens)*

#### Safety mechanisms cover the expensive paths

Merge integration tests are bounded, worktree operations have dedicated infrastructure, state validation is broad, state mutations occur under the blackboard lock, and watchdog/alert/recovery facilities address stalled or interrupted execution.

#### Strong test investment

The repository contains more than twice as much Go test source as production source. Large behavioral suites cover operations, prompts, initialization, watchers, embedded artifacts, supervisor behavior, configuration, and blackboard persistence. Measured correctly, this converts: 80.7% statement coverage repository-wide, with the domain and persistence core — `models` 90.5%, `pipeline` 90.6%, `statevalidate` 89.6%, `db` 86.5%, `filelock` 95.2% — above the application layer. The investment is concentrated where the cost of being wrong is highest, which is the right shape. *(pass 20, Coverage lens)*

#### Quality gates are mechanical, not aspirational *(pass 20, Coverage lens)*

The repository enforces test-suite properties in the suite itself. `internal/testguard` ratchets `t.Parallel()` as a floor and `time.Sleep` as a ceiling; `check-testhelpers` fails the build if production code imports the test-helper package; `check-embedded` asserts embedded artifacts match their sources. These are the same class of mechanism §2.3 recommends for import direction — the habit exists and is already load-bearing.

### 2.3 Current Smells and Tensions

#### Contract enforcement gaps

**Smell:** Documentation and enforcement divergence.

**Signals:**

- `INVARIANTS.md` §7 promises a clean worktree before review, but `SubmitForReview` does not perform a porcelain-status cleanliness check.
- `INVARIANTS.md` §6 describes provider diversity at verdict submission, but `SubmitVerdict` checks quorum only; diversity is handled during claiming and merge readiness.
- `INVARIANTS.md` §12 omits valid `STOPPED` transitions and describes `TRIPPED → PAUSED`, while resume behavior returns `TRIPPED` to `RUNNING`.
- `INVARIANTS.md` §3 uses legacy status vocabulary and attributes transition ownership to an obsolete location.

**Impact:** Operators and agents can reason from guarantees that are not implemented at the stated boundary.

**Direction:** Choose the intended behavior at each mismatch, then align the invariant and enforcement point.

#### Large multi-responsibility modules

**Smell:** God module / divergent change.

**Signals:** `proceed.go`, `embedded.go`, `watch.go`, `init.go`, `supervisor.go`, and `scipsearch.go` are all above 1,100 lines and combine multiple change reasons.

**Refinement *(pass 18, Complexity lens)*:** Function-level measurement separates these. `scipsearch.go`, `embedded.go`, `watch.go`, and `pairingindex/hooks.go` are wide rather than deep — many small functions, no member above 80 lines — and are largely review-scope concerns. `supervisor.go`, `commands/init.go`, and the `ops` write path carry genuine single-function complexity of 200-442 lines and are where decomposition pays.

**Impact:** Review scope, merge-conflict probability, and regression surface grow as independent capabilities evolve. Where a single function exceeds 300 lines, that surface is also untestable in parts.

**Direction:** Extract only along demonstrated responsibility boundaries; avoid interface layers with a single speculative implementation. Prioritize by longest function, not by file size.

#### Role extensibility stops at the prompt boundary *(pass 18, Complexity lens)*

**Smell:** Leaky abstraction / capability claim not carried through.

**Signals:**

- `supervisor.go:648-650` states that the resolver reads role definitions from pipeline YAML, "enabling custom YAML-defined roles without Go code changes".
- Pipeline validation checks `role.Type` against `doer|reviewer|orchestrator` but never checks the role *name*; `agent/registration.go` validates only the `{role}-{number}` ID shape and never calls `roles.IsValid`.
- The closed 13-role registry in `internal/roles` is consulted in exactly one production path, on an already-inferred role.
- `review_instructions.tmpl` and `implementation_phase.tmpl` branch on hardcoded role names with no `else`.

**Impact:** A YAML-defined role validates, resolves, registers, and launches — then receives an empty instructions block. The failure is silent at every layer, and the declarative-lifecycle strength holds for the state machine but not for prompt content.

**Direction:** Decide whether custom roles are supported. If yes, give the templates a fallback and make role coverage checkable at config-load time; if no, validate role names where pipeline config is loaded and correct the comment.

#### Parallel implementations of the same capability *(pass 18, Complexity lens)*

**Smell:** Copy-paste divergence.

**Signals:** `commands/init.go` and `ops/init_project.go` duplicate initialization helpers and have already diverged on Git invocation; `tui/view.go` duplicates panel table rendering including a verbatim ANSI-padding special case; `agent/prompt.go` triples index adapters and types across three tools.

**Impact:** Fixes land in one copy. The `gitenv` divergence is the concrete instance: locale and timeout hardening applies to one initialization path and not the other.

**Direction:** Converge the Git call sites on `gitenv` first — that one is a correctness gap, not a tidiness preference. Treat the render and adapter duplication as lower-priority consolidation.

#### Unobservable index-discovery failure *(pass 18, Complexity lens)*

**Smell:** Silent failure.

**Signals:** `availablePromptScipIndexRefs`, `availablePromptStacklitIndexRefs`, and `availablePromptFunctionalClusterIndexRefs` each return `nil` on error with no logging.

**Impact:** Agents receive prompts missing their code-index references and behave as if none were configured. Degraded exploration is indistinguishable from an unindexed repository.

**Direction:** Log at warning level and keep the nil return; the degradation itself is acceptable, its invisibility is not.

#### Coverage measurement misreports the packages it is used to judge *(pass 20, Coverage lens)*

**Smell:** Instrument that reads wrong in the direction of complacency.

**Signals:**

- `make coverage` runs `go test -coverprofile=<unique-temporary-profile> ./...` with no `-coverpkg`, so each package is credited only with coverage produced by its own test binary. Routine `make test` intentionally collects no coverage.
- Under that default, `internal/alerts` and `internal/projectdetect` read 0.0% and `internal/errors` 12.7%. Measured with `-coverpkg=./...` they are 89.7%, 92.9%, and 91.1%. Neither package has a test file of its own; both are exercised thoroughly by their callers.
- `models.ValidateDependsOn` reads 22.2% by default and 100% cross-package. Three prior passes recorded "untested entry point" findings that dissolve under correct measurement.
- `.github/workflows/ci.yml` still attempts to upload `coverage.out` with `fail_ci_if_error: false`, but its preceding `make test` step no longer produces that file. The repository has no `codecov.yml` or threshold, and CI wiring for `make coverage` is explicitly deferred.

**Impact:** The published number is systematically wrong for exactly the packages this review keeps flagging — thin leaf packages consumed from elsewhere. It under-reports them, which is the harmless direction for the package but the harmful direction for the reviewer: three passes have now spent attention on false gaps, and the same artifact hides the real ones behind noise. Codecov is decorative, not a gate.

**Direction:** Add `-coverpkg=./...` to the `coverage` target and give CI an explicit retained-profile path. That change affects what every future coverage claim in this document means, so it should land before any coverage-driven work.

#### The human-readable CLI path is structurally unmeasured *(pass 20, Coverage lens)*

**Smell:** Untested branch of a deliberately duplicated path.

**Signals:**

- All 69 Cobra commands are package-level `var … = &cobra.Command{ RunE: func… }` literals. `go tool cover -func` walks `FuncDecl` only, so every `RunE` body is absent from per-function output — the standard gap-hunting tool cannot see the CLI's behavior. This alone accounts for the gap between `cmd/liza` at 62.4% by block arithmetic and 78.4% by `-func`.
- Ten exported `internal/commands` entry points sit at 0.0%: `AddTasksCommand`, `ProceedCommand`, `SetTaskOutputCommand`, `WriteCheckpointCommand`, `RecoverTaskCommand`, `RepairAgentPoolCommand`, `MarkAgentDegradedCommand`, `ClearAgentDegradedCommand`, `AssessHypothesisExhaustedCommand`, `SetDiscoveryDispositionCommand`, along with seven unexported formatters (`formatTaskSummaryOutput`, `printProceedResult`, and siblings).
- Each is called from exactly one `RunE` body and referenced by zero tests. `UnblockTaskCommand` is called from nowhere at all.
- The `--json` sibling of each command routes to `internal/ops`, which is at 84.3% over 6,816 statements.

**Impact:** The dual-path design documented in §1.3 and §2.5 has one path tested and one path not. Agents consume `--json` and are covered; humans consume the rendered path and are not. Output formatting, error phrasing, and result rendering for a dozen mutating commands can break without any test failing — and the tooling a reviewer would reach for to notice this reports `cmd/liza` 16 points higher than it is.

**Direction:** Either test the render path — table-driven tests over the `*Command` functions are cheap, they already take a project root and return `error` — or collapse it, since the same finding read from the Boundaries lens says these adapters are optional. Delete `UnblockTaskCommand` regardless.

#### Application/query boundary erosion

**Smell:** Dependency inversion and inappropriate intimacy.

**Signals:** Prompt construction imports `ops`; commands import agent runtime; read-only query logic spans all three layers.

**Impact:** Presentation and prompt changes acquire unnecessary transitive dependencies on mutation-oriented application code.

**Direction:** Move stable read models and lifecycle queries toward their owning domain/pipeline package, or introduce a focused query boundary when another concrete consumer justifies it.

#### State mutation authority is distributed, not owned *(pass 19, Boundaries lens)*

**Smell:** Leaking encapsulation / bypassed authority boundary.

**Signals:**

- `internal/agent` calls `bb.Modify` at 13 non-test sites: `claiming.go:452`, `heartbeat.go:82`, `provider_audit.go:80`, `registration.go` (×5), `strategy_orchestrator.go:80`, `supervisor.go:103,323`, `worktree_check.go:146,169`.
- `commands/init.go:1113` and `commands/migrate.go:54` call `bb.Write` directly.
- `worktree_check.go:165-204` documents the bypass in a comment: it forces `t.Status = TaskStatusBlocked` "bypassing normal transition validation" because "no valid transition exists from REVIEWING".
- `claiming.go:449-470` reimplements the merge path's non-git finalization (agent release + handoff event) outside `ops`.

**Impact:** Transition validation, history conventions, and post-write checks are enforced only on the `ops` path. The state machine is advisory for any caller that opens its own `Modify` scope, and duplicated finalization can drift from the authoritative implementation.

**Direction:** Decide whether `ops` is the mutation boundary. If yes, give `ops` an explicit forced-transition operation for the unrecoverable-worktree case rather than reaching past it, and route the `agent` write sites through named operations. If no, drop the claim from Q8 and document the second write path.

#### Semantic validation sits off the write path *(pass 19, Boundaries lens)*

**Smell:** Validation placed where it can be skipped.

**Signals:**

- `internal/statevalidate` is 2,148 LOC and is imported by six non-test files: `commands/validate.go` and five `ops` write paths (`add_tasks`, `replan`, `retarget_dependency`, `set_task_output`, `wt_merge`). `internal/db` is not among them.
- `db.marshalStateForWrite` calls `statehygiene.ValidateState` only — payload size and scrubbing, not invariants.
- `PostWriteValidationError` is constructed at exactly one site, `ops/replan.go:224`.

**Impact:** An invalid state can be written and persisted; it is detected later by `liza validate` or not at all. Validation strength depends on which operation was used rather than on what was written.

**Direction:** Classify `statevalidate` checks into always-cheap invariants (candidates for the in-lock gate) and expensive/whole-corpus checks (keep on demand). Placing even a subset behind `Modify` would make the write boundary the enforcement boundary.

#### Machine-readable error codes derive from prose *(pass 19, Boundaries lens)*

**Smell:** Stringly-typed contract at an integration boundary.

**Signals:**

- `jsonout/classify.go` maps errors to `code` values by substring match on the message: `"not found"`, `"must be"`, `"is required"`, `"validation failed"`, `"race condition"`, `"not IMPLEMENTING"`, and others.
- `internal/ops` contains 319 non-test `fmt.Errorf` calls against 8 typed error types, of which 5 are handled in `classify.go`.
- `jsonout` imports `ops` to perform those type assertions, making a presentation package depend on the application layer.

**Impact:** Agents consuming `--json` branch on a code derived from English phrasing. Rewording an error message is a silent contract change; a message that happens to contain "must be" is classified as a validation failure regardless of cause.

**Direction:** Type the errors that agents actually branch on and delete the corresponding string rules. The prose table can remain as a fallback, but it should not be the primary classifier for codes the multi-agent protocol depends on.

#### Boundaries are documented but not mechanically enforced *(pass 19, Boundaries lens)*

**Smell:** Convention without a check.

**Signals:**

- Pre-commit and `make lint` already run `check-testhelpers` and `check-embedded` (`TestArtifactConsistency`) — the repository has the capability and the habit.
- No equivalent check exists for import direction. `prompts → ops`, `commands → agent`, `agent → db`, and `jsonout → ops` are all documented in this review and none of them fails a build.
- `INVARIANTS.md` carries 7 `**Enforced:**` anchors across 15 sections, and those anchors use bare filenames (`validate.go`, `claim_task.go`, `proceed.go`) that each resolve to two packages.

**Impact:** Every layer violation in this document was found by review, not by CI, and each has survived multiple passes. Documented invariants drift because nothing binds the document to a code location precisely enough to break when the location moves.

**Direction:** Add an import-direction check to `make lint` covering the four known crossings as an allowlist — new violations fail, existing ones are visible and countable. Separately, make `INVARIANTS.md` anchors package-qualified so a moved enforcement point is detectable.

#### `internal/brand` combines link-time globals with a value API *(pass 19, Boundaries lens)*

**Smell:** Dual API over mutable global state.

**Signals:**

- `internal/brand` has the highest fan-in in the repository (28 packages).
- Package-level variables are set at link time via `-ldflags -X` (`Makefile:27-38`) and remain writable at runtime.
- A parallel `Values` struct exposes the same fields through methods, so call sites choose between two idioms for the same data.
- Tests mutate `brand.EnvPrefix` directly rather than constructing a `Values`.

**Impact:** The most widely depended-upon package in the tree is mutable from anywhere. Test mutation of globals is order-dependent, and two API shapes mean white-labeling correctness has to be verified twice.

**Direction:** Pick one. Either keep the link-time globals and delete `Values`, or make the globals a private seed for a single exported `Values` accessor. This is a low-risk consolidation with a wide blast radius, so it is worth doing deliberately rather than opportunistically.

#### Provider support crosses the catalog boundary *(pass 22, Coupling lens)*

**Smell:** Incomplete abstraction / change amplification.

**Signals:** `providers.ActivationAssets` contains provider-named fields; `commands/init.go` expands Cursor into Claude, Codex, and Cursor-ACP dependencies and maps bash-policy support through provider-specific booleans; `agent/provider_audit.go` and `agent/provider_unavailable.go` separately encode Codex output signatures.

**Impact:** A provider can become catalog-resolvable and launchable without receiving complete initialization or operational-diagnostics support. Full onboarding requires coordinated edits across `providers`, `commands`, `embedded`, and `agent`, and omitted runtime signatures fail silently.

**Direction:** Keep genuinely behavioral failure parsers in code, but let the catalog own declarative dependencies and activation assets. Document one provider-extension checklist rather than claiming the catalog is a plug-in boundary.

#### Canonical provider identity is lost before policy enforcement *(Adversarial pass)*

**Smell:** Primitive obsession / leaky abstraction.

**Signals:**

- The catalog populates `AgentToolConfig.ProviderKey`, and `ResolveLaunchPlan` copies it into `LaunchPlan.ProviderKey`, but production code never reads that field.
- `RunSupervisor` receives only the raw `CLIName`; registration stores it as `Agent.Provider`, which approvals and the merge diversity gate compare as a string.
- Quota handling canonicalizes `codex-acp` to `codex`, while provider-unavailable and audit-degradation handling compare and address raw CLI names.
- The existing quota tests explicitly cover ACP canonicalization; the sibling provider-unavailable and audit tests do not.

**Impact:** `codex` and `codex-acp` can count as different providers despite sharing one provider backend, falsely satisfying preferred diversity. Conversely, a provider-unavailable signal for `codex` does not stop a `codex-acp` supervisor. Custom tools declaring the same `provider_key` are similarly fragmented across review, stop-state, and anomaly policy.

**Direction:** Carry both tool identity and canonical provider identity through `SupervisorConfig`. Use the canonical provider for registration, approvals, stop signals, and anomaly grouping; retain the tool name for launch behavior and tool-specific diagnostics.

#### Provider stop-state recovery has multiple owners *(pass 21, Duplication lens; pass 22, Coupling lens)*

**Smell:** Shotgun surgery / duplicated application policy.

**Signals:** `agent/quota.go` and `agent/provider_unavailable.go` independently implement the same signal-file path, glob, provider extraction, write, check, and clear lifecycle. `commands/resume.go` and `tui/commands.go` then independently glob and clear both families after calling `ops.Resume`; both silently ignore glob errors and separately format clear failures.

**Impact:** Adding another provider stop reason or changing cleanup semantics requires coordinated edits across the runtime, CLI, TUI, and their duplicated tests. A caller using `ops.Resume` directly resumes state without performing the cleanup that the user-facing adapters treat as part of resume.

**Direction:** Move the signal-file primitive out of `agent` to a small stable owner and make one application operation return structured cleanup outcomes. Preserve category-specific alert text; do not generalize unrelated provider failure detection.

#### Worktree intelligence refresh is orchestration-by-copy *(pass 21, Duplication lens; pass 22, Coupling lens)*

**Smell:** Shotgun surgery.

**Signals:** Semble preparation plus SCIP, Stacklit, and functional-cluster refresh is sequenced in `ops/claim_task.go`, twice in `ops/wt_create.go`, through a second wrapper family in `ops/submit_review.go`, and through lower-level package calls in `agent/worktree_check.go`. The paths expose different warning shapes and test seams.

**Impact:** A fourth repository-intelligence tool, ordering change, or warning-policy fix must be propagated across claim, create, recovery, and submission. The current order dependency — functional clusters after SCIP and Stacklit — is carried by repetition rather than a named policy.

**Direction:** Introduce one task-worktree refresh operation returning structured per-tool results, then let each caller choose presentation/logging. Keep submit-time versus provision-time activation explicit if their required tool sets differ.

#### Optional-tool boundary primitives have multiple owners *(pass 21, Duplication lens; pass 22, Coupling lens)*

**Smell:** Copy-paste divergence.

**Signals:** POSIX single-quote escaping exists in five packages; `outputSuffix` in five; bounded diagnostics in three; and SCIP/Semble repeat truthy parsing already represented by `internal/envgate`. The diagnostic implementations have already diverged: SCIP truncates to the byte limit without a marker, while Stacklit and functional clusters append `...(truncated)` after slicing to the limit.

**Impact:** Shell hardening and diagnostic-bound changes have a broad edit surface, and identically named limits do not guarantee identical output bounds.

**Direction:** Consolidate only the proven primitives — POSIX quoting, bounded diagnostics, and truthy parsing — in leaf packages with direct edge-case tests. Leave tool-specific planning and error messages local.

#### Supervisor registration bypasses configured runtime metadata *(pass 21, Duplication lens)*

**Smell:** Hardcoded configuration.

**Signals:** `RunSupervisor` passes `"terminal-1"` and `1800` directly to `registerAgent`. `NewHeartbeat` receives no `LeaseDuration`, so it falls back to the same default rather than `state.Config.LeaseDuration`, even though the schema says heartbeat extends leases by the configured duration. The fixed terminal string is stored as human-observation metadata.

**Impact:** Task leases follow configuration while agent registration and renewal do not. Non-default lease settings therefore produce two liveness policies, and every concurrent supervisor reports the same terminal identity.

**Direction:** Resolve agent lease duration from state once and pass it to registration and heartbeat. Either capture a real terminal identifier or remove the field from current operational claims.

#### Current documentation retains removed or undriven interfaces *(pass 21, Duplication lens; pass 22, Coupling lens)*

**Smell:** Documentation and implementation divergence.

**Signals:** `specs/architecture/c4/c4.md` and its diagrams still model the removed `internal/mcp` subsystem; `overview.md` still calls the supervision document an MCP-responsibility guide; `supervision-model.md` assigns the shared implementation role to `commands` rather than `ops`; generated `CONFIGURATION.md` advertises `LIZA_LOG_LEVEL`, while the agent logger is fixed at `slog.LevelInfo`.

**Impact:** Current architecture and configuration guides direct readers toward components and controls that do not exist.

**Direction:** Regenerate or retire the MCP C4 views, update the overview label, and either implement log-level parsing or remove the environment variable from generated documentation. Historical ADR references should remain unchanged.

#### Repeated control-flow skeletons

**Smell:** Structural duplication.

**Signals:** Await operations repeat watcher/timer/polling orchestration; pipeline-aware status helpers repeat resolver-shaped logic; worktree paths are constructed both through a helper and inline.

**Impact:** Behavioral fixes can land in one path but miss siblings.

**Direction:** Consolidate shared policy only where one abstraction makes failure behavior clearer than the duplicated code.

#### Configuration and time-policy scatter

**Smell:** Hardcoded configuration.

**Signals:** Watch has twelve top-level timing/retry constants, timeouts and polling intervals live across packages, `wt_merge.go` mixes UTC and local timestamps, and supervisor agent leases bypass `Config.LeaseDuration`.

**Impact:** Operational tuning and temporal reasoning require code-wide knowledge.

**Direction:** Centralize values that operators need to tune; keep truly local implementation constants near their use.

#### Data contract loss and ambiguity

**Smell:** Leaky or unstable contract.

**Signals:** `SetTaskOutput` disagrees with validation/proceeding about `spec_ref`; sprint archival drops metrics other than `TasksDone`; fan-out overwrites subtask priority.

**Impact:** Data may be accepted at one boundary and rejected later, or silently lose scheduling/observability information.

**Direction:** Define ownership and preservation rules before changing implementation.

#### Filesystem error and cleanup asymmetry

**Smell:** Inconsistent error semantics.

**Signals:** Some `os.Stat` presence checks distinguish only exists/missing, and some cleanup removal failures are suppressed.

**Impact:** Permission and I/O failures can look like absence, while secondary cleanup failures may be observable only indirectly.

**Direction:** Use tri-state existence handling at correctness boundaries and explicitly document best-effort cleanup boundaries.

### 2.4 Resolved Since the Prior Review

| Prior finding | Current status |
|---------------|----------------|
| Unbounded integration-test execution | Resolved: merge tests use a configured timeout, command context, process-group termination, and `WaitDelay`. |
| Bidirectional role-map synchronization | Resolved: canonical role names and normalization live in `internal/roles`. |
| Five-tool concrete CLI executor switch | Resolved/reframed: provider catalog and launch profiles own most tool variation. |
| No interface-based seams | Stale: the runtime now exposes multiple focused interfaces, including `LLMAgent`, `CLIExecutor`, `RoleStrategy`, watchers, runners, and lookups. |
| Broken sprint-governance Vision link | Resolved: the spec links the existing build Vision document. |
| Hand-written bubble sort | Resolved: formatting uses `slices.Sort`. |
| Hardcoded child task type | Resolved: transition definitions supply the child task type. |
| Different-coder rejected-worktree recreation | Resolved by contract alignment: rejected worktrees are preserved after lease-expiry reassignment. |
| Untested role Phase 2 query functions | Stale: those functions no longer exist. |
| MCP runtime architecture findings | Obsolete: MCP was removed under ADR-0057. Current C4/overview references are a separate documentation-drift finding. *(adjusted, pass 21)* |
| `format.go` file references | Stale path: current helpers live in `format_helpers.go`. |
| "`coverage.out` is a narrow or partial run" | Adjusted again on 2026-08-24: `make coverage` now uses and removes a unique temporary profile, so there is no canonical local `coverage.out`. Pass 20's figures came from a separate complete `-coverpkg` run and remain historical evidence. |
| "Three packages have zero test files" (`alerts`, `projectdetect`, `taskkind`) | Adjusted at pass 20: accurate as stated, but not a coverage gap. `alerts` is 89.7% and `projectdetect` 92.9% from their callers; `taskkind` has no executable statements. |
| Untested `statevalidate` and embedded entry points | Stale at pass 20: `statevalidate` 89.6%, `PlanGlobalFiles` 90.9%, `WritePipelineConfig` 100%. The finding was an artifact of same-package measurement. |

**Follow-through gap:** The [non-reproducible coverage basis](architectural-issues.md#coverage-basis-is-not-reproducible-by-repository-tooling) has remained High and verified through passes 20, 21, and 22. Reverification shows continued accuracy, not progress; the canonical registry owner makes later closure explicit.

### 2.5 Patterns

| Pattern | Where Used | Purpose |
|---------|------------|---------|
| Repository | `internal/db.Blackboard` | Serializes process-safe access to shared state. |
| Unit of Work | `Blackboard.Modify` callbacks | Keeps mutation and payload-hygiene validation inside one lock scope; semantic invariant validation is outside it. *(adjusted, pass 19)* |
| State | `models`, `pipeline`, `ops` | Governs task and system-mode transitions. |
| Strategy | `internal/agent` role strategies | Varies behavior by role without duplicating the supervisor. |
| Adapter | providers, toolchain, Git, SCIP, Stacklit | Isolates external CLI and repository-tool behavior. |
| Catalog/Profile | `internal/providers` | Makes provider launch configuration data-driven. |
| Builder | `internal/prompts` | Composes role- and pipeline-specific prompts. |
| Observer | blackboard watcher and TUI | Reacts to atomic state-file changes. |
| Command/Service | `internal/commands` → `internal/ops` | Separates user-facing parsing from mutations, but the layer is optional: tests excluded, package-qualified call expressions in production `cmd/liza` yield 31 calls to `ops` and 58 to `commands` (89 total). The count excludes type references and struct literals. *(adjusted, pass 19; re-counted 2026-07-25)* |
| Transactional outbox analogue | task history in blackboard state | Records lifecycle changes with the authoritative state update. |

### 2.6 Test Coverage

The [2026-07-24 code quality assessment](code_quality_assessment.md#repository-metrics-dashboard) counted all tracked Go files at 150,683 test lines and 65,509 production lines, a 2.30:1 ratio. The previously quoted 150,414/65,061 total used the narrower `cmd/` + `internal/` scope: the exact difference is `plugin/acp` (269 test and 448 production lines). This dated ratio indicates substantial test investment but is not a statement-coverage percentage.

**Historical measurement basis *(pass 20, Coverage lens)*.** A separate complete `go test -coverpkg=./... ./...` run produced whole-module profiles for approximately 48 test binaries, then deduplicated blocks by block key. The raw profiles and deduplication implementation were not retained. The committed [2026-07-25 coverage summary](coverage-summary-2026-07-25.md) preserves the reviewed aggregate, per-package bands, and zero-function counts, but the figures remain historical until repository tooling regenerates the same basis. `make coverage` still omits `-coverpkg` and cleans its temporary profile — see §2.3.

The retained summary records **80.7%** over 26,178 statements by profile-block arithmetic and **82.6%** by `go tool cover -func`. The latter omits function literals outside a `FuncDecl`, including all 69 Cobra `RunE` bodies, so block arithmetic is the more complete historical total.

**Coverage by package (`-coverpkg` basis, statements in parentheses):**

| Band | Packages |
|------|----------|
| ≥90% | `identity` 100 (48), `roles` 100 (5), `envgate` 96.0, `jsonout` 96.3 (81), `filelock` 95.2 (126), `analysis` 94.9, `secretmask` 94.6, `projectdetect` 92.9, `termutil` 91.7, `semble` 91.3, `errors` 91.1, `pipeline` 90.6 (663), `models` 90.5 (641) |
| 80–90% | `alerts` 89.7, `statevalidate` 89.6 (924), `plugin/acp` 89.2, `paths` 88.5, `procscan` 88.3, `scipsearch` 87.9 (742), `render` 87.7, `codexconfig` 87.5, `brand` 86.6, `db` 86.5, `prompts` 85.6, `ops` 84.3 (6,816), `gitenv` 84.2, `embedded` 84.1 (731), `precommit` 83.3, `providers` 83.1, `testhelpers` 82.5, `tui` 82.0 (972), `git` 82.0, `toolchain` 80.1, `process` 80.0 |
| 70–80% | `commands` 79.7 (3,343), `initcheck` 78.9, `agent` 78.8 (3,162), `pairingindex` 78.0, `statehygiene` 75.2, `worktreeexclude` 74.0, `stacklit` 73.5, `updater` 71.9 (629) |
| <70% | `log` 69.0, `brandrender` 64.9, `functionalclusters` 64.1, **`cmd/liza` 62.4 (2,631)**, `interactive` 35.8 (67), `brandrender/cmd/sync-embedded` 0.0 (6) |

`internal/taskkind` does not appear because it contains no executable statements.

**Current structural observations:**

- The shape is healthy: the domain, pipeline, validation, and persistence core sits at 86–96%, and the two weakest substantial packages are the CLI wiring layer (`cmd/liza`, 62.4%) and the human-readable command adapters inside `internal/commands`. Depth of coverage tracks cost of being wrong.
- 118 of 2,314 measured functions are at 0.0%, 59 of them exported. They cluster in `commands` (19), `ops` (17), `cmd/liza` (15), `updater` (10), and `agent` (10). This count excludes the 69 `RunE` bodies, which the measuring tool cannot see at all.
- `internal/interactive` at 35.8% is small (67 statements) but sits on the first-run initialization path, where failure is user-visible and recovery is manual.
- `git.ResetHard` is the only destructive Git helper at 0.0%; `MergeTree`, `CreateCommitFromTree`, and `AttachWorktree` are all covered.
- The tracked test tree contains ten `time.Sleep(` occurrences outside `internal/testguard/` and 524 `t.Parallel()` text occurrences across all tracked Go test files. `internal/testguard` retains the 11-sleep ceiling and a minimum of 514 parallel occurrences; this is a floor, not an exact count. *(remeasured 2026-09-06)*
- The 524 parallel occurrences span 51 of 308 tracked Go test files and are concentrated in the audited `internal/ops` set plus sliced-integration lifecycle branches. The remaining large packages still need package-local shared-state audits before broader adoption. *(remeasured 2026-09-06)*
- `internal/embedded/opencode-tools/exec.ts` is shipped to users' `.opencode/tools/` directories, and no `package.json`, `tsconfig.json`, type-check, or lint step exists anywhere in the repository or CI. Its only assertions are content-identity checks in `embedded_test.go`, which verify that the bytes were copied, not that they run.
- Integration coverage exists for multi-component task, Git, persistence, and execution flows; no new behavior is being added by this review.

## Phase 3: Recommendations

| Priority | Status | Issue | Rationale | Direction |
|----------|--------|-------|-----------|-----------|
| **High** | ✓ verified | [`make coverage` has no `-coverpkg`](architectural-issues.md#coverage-basis-is-not-reproducible-by-repository-tooling) | Packages are credited only with their own tests, so the standard profile understates caller-driven coverage and cannot regenerate the pass-20 basis. | Add `-coverpkg=./...` and retain a deterministic summary before any coverage-driven work. *(pass 20 Coverage finding verified in passes 21–22; command updated 2026-08-24)* |
| **Medium** | ~ adjusted | Human-readable CLI path is untested | Pass 20 found 12 exported `*Command` entry points and 7 formatters at 0.0%; no fresh complete coverage run was made in pass 21, while the structural gap remains. | Add table-driven tests over the `*Command` functions, or collapse the adapter layer — the Boundaries lens reaches the same place. *(pass 20, Coverage lens)* |
| **Medium** | ✓ verified | `exec.ts` ships without a toolchain | A TypeScript file installed into users' `.opencode/tools/` has no type-check, lint, or test anywhere in the repo or CI; only byte-identity assertions. | Add a type-check step, or state explicitly that the file is vendored and unverified. *(pass 20, Coverage lens)* |
| **Medium** | + new | Provider support crosses the catalog boundary | Provider launch is catalog-driven, but setup dependencies, activation assets, bash-policy integration, and runtime failure detection still require provider-specific edits across four packages. | Move declarative dependencies/assets into the catalog and document the remaining behavioral extension points. *(pass 22, Coupling lens)* |
| **Medium** | + new | Canonical provider identity is dropped before runtime policy | Catalog-generated `ProviderKey` is unused while raw CLI names drive diversity, stop signals, and anomalies. `codex` and `codex-acp` can be treated as distinct providers, and unavailable signals do not cross aliases. | Propagate canonical `ProviderKey` through the supervisor and keep tool identity separate. *(Adversarial pass)* |
| **Medium** | ✓ verified | Provider stop-signal recovery has multiple owners | Quota and provider-unavailable handling are encoded separately in agent helpers, CLI resume, and TUI resume, including duplicated cleanup and tests. | Give one operation ownership of signal inspection, policy evaluation, cleanup, and resume-state mutation; keep CLI/TUI as adapters. *(pass 21, Duplication lens; reverified pass 22)* |
| **Medium** | ✓ verified | Worktree intelligence refresh has multiple owners | Create, claim, health-check, and review submission repeat slightly different SCIP/Stacklit/functional-clusters refresh sequences and warning policy. | Introduce one refresh coordinator with explicit trigger and failure-policy inputs; keep lifecycle decisions at callers. *(pass 21, Duplication lens; reverified pass 22)* |
| **Medium** | ✓ verified | Review-submission cleanliness contract gap | `INVARIANTS.md` promises a clean worktree at a boundary that does not enforce it. | Decide whether to enforce cleanliness in `SubmitForReview` or narrow the invariant. |
| **Medium** | ✓ verified | Verdict-time provider-diversity contract gap | The documented enforcement point and current quorum logic disagree. | Define whether diversity is required, preferred, or claim-time-only, then align contract and code. |
| **Medium** | ✓ verified | [`ops/proceed.go` at 1,563 lines](architectural-issues.md#decompose-proceedgo-1500-loc) | Graph, recovery, construction, and propagation logic change for different reasons. | Split along existing responsibility boundaries. |
| **Medium** | ✓ verified | `RunSupervisor` at 442 lines | The longest function in the repository interleaves setup, signal handling, strategy dispatch, exit-code routing, and five loop detectors. | Extract the exit-code dispatch and the loop-detection block; the trackers are a cohesive unit with hand-paired resets. *(pass 18, Complexity lens)* |
| **Medium** | ✓ verified | Agent lease renewal ignores configured duration | Supervisor registration and heartbeat each rely on a literal/default 1,800-second lease, while runtime configuration says heartbeats extend the configured lease. | Pass the configured lease and terminal identity through one registration/heartbeat runtime context. *(pass 21, Duplication lens; reverified pass 22)* |
| **Medium** | ✓ verified | Raw Git calls bypass `gitenv` | `ops/init_project.go` and `cmd/liza/cmd_launch.go` invoke `git` directly at five sites, losing `LC_ALL=C` and timeout bounds that sixteen other files get. | Route all Git invocation through `gitenv`. *(pass 18, Complexity lens)* |
| **Medium** | ✓ verified | Custom roles render empty prompt blocks | Nothing validates role names, and both large template chains lack an `else` branch. | Add a fallback block or config-load-time role coverage validation, then correct the supervisor comment. *(pass 18, Complexity lens)* |
| **Medium** | ✓ verified | Duplicate initialization implementations | `commands/init.go` and `ops/init_project.go` maintain near-verbatim helpers that have already diverged. | Establish one owner for initialization mechanics. *(pass 18, Complexity lens)* |
| **Medium** | ~ adjusted | Other large orchestration/artifact modules | `embedded.go`, `watch.go`, and `scipsearch.go` remain large but decompose well internally; `init.go` and `supervisor.go` do not. | Decompose the latter two by longest function; treat the former three as review-scope only. *(adjusted, pass 18; reverified pass 21)* |
| **Medium** | ✓ verified | Silent index-discovery failure | Three `agent/prompt.go` helpers swallow errors, so agents lose index references invisibly. | Log the failure; keep the graceful degradation. *(pass 18, Complexity lens)* |
| **Medium** | ✓ verified | State mutation bypasses `ops` at 15 sites | `agent` (13 `.Modify`) and `commands` (2 `bb.Write`) write state without transition validation; `worktree_check.go` forces `BLOCKED` by comment-documented design. | Add an explicit forced-transition operation to `ops` and route agent writes through named operations, or stop claiming `ops` owns mutations. *(pass 19, Boundaries lens)* |
| **Medium** | ✓ verified | Semantic validation is off the write path | `statevalidate` is not imported by `db`; the in-lock gate is `statehygiene` only, and `PostWriteValidationError` remains narrowly used. | Move the cheap invariant subset inside `Blackboard.Modify`. *(pass 19, Boundaries lens)* |
| **Medium** | ✓ verified | `--json` error codes derive from message prose | `fmt.Errorf` remains dominant while `classify.go` substring-matches phrases such as `"must be"`, `"is required"`, and `"not found"`. Rewording can silently reclassify. | Type the errors agents branch on; keep prose rules as fallback only. *(pass 19, Boundaries lens)* |
| **Medium** | ✓ verified | No mechanical import-direction check | Documented crossings remain buildable; existing repository checks prove that an allowlist-based boundary check is feasible. | Add an allowlist-based import check to `make lint`. *(pass 19, Boundaries lens)* |
| **Medium** | ✓ verified | `spec_ref` contract conflict | CLI and mutation accept absence; validation and proceeding reject it. | Specify the lifecycle requirement and make every boundary consistent. |
| **Medium** | ✓ verified | Sprint-summary metric loss | Archival preserves only `TasksDone`. | Specify which metrics must survive a sprint boundary before changing the model. |
| **Low** | ✓ verified | System-mode and task-status invariant drift | Sections 3 and 12 no longer describe current ownership or transitions. | Update the invariant text after intended behavior is confirmed. |
| **Low** | ✓ verified | `prompts → ops` dependency | Prompt assembly imports mutation-layer queries. | Relocate stable query policy toward pipeline/domain ownership. |
| **Low** | ✓ verified | `commands → agent` dependency | Command adapters still import runtime orchestration, including provider-signal inspection. | Move read-only runtime inspection behind a narrower owner. |
| **Low** | ✓ verified | Await-loop structural duplication | Verdict and resubmission waits share timer/watcher/polling shape. | Consider a shared wait primitive if it preserves explicit failure semantics. |
| **Low** | ✓ verified | `ApplyYAMLTimeouts` concrete type switch | Timeout projection bypasses the role strategy abstraction. | Put the capability on the strategy boundary or isolate projection in a dedicated adapter. |
| **Low** | ✓ verified | Pipeline-aware status triplication | Three helpers differ primarily by resolver method. | Parameterize only if it improves readability and type safety. |
| **Low** | ✓ verified | Worktree path helper adoption | Three production call sites still construct the relative path inline. | Use one naming authority. |
| **Low** | ✓ verified | Dual logging paths | Direct stderr logging and the project logger coexist without a concise contract. | Document purpose or route warnings through one structured boundary. |
| **Low** | ✓ verified | Identity validation duplication | Registration repeats checks owned by `internal/identity`, and validates the ID shape without validating the role name. | Reuse the identity boundary; decide whether role-name validation belongs there or at config load. *(adjusted, pass 18)* |
| **Low** | ✓ verified | Prompt template blocks at 392 and 240 lines | Two files hold about 36% of all template bytes as hardcoded per-role chains. | Split per role or per phase when the next role is added. *(pass 18, Complexity lens)* |
| **Low** | ✓ verified | `ops` write-path transaction scripts | Five functions repeat a read/validate → external effects → mutate ordering carried mainly by phase comments. | Consider a shared transaction shape only if it makes the lock and effect boundaries harder to get wrong. *(pass 18, Complexity lens)* |
| **Low** | ✓ verified | TUI panel render duplication | `renderAgentPanel` and `renderTaskPanel` duplicate column layout and an ANSI-padding special case verbatim. | Share one column renderer with a width-aware value type. *(pass 18, Complexity lens)* |
| **Low** | ✓ verified | Index adapter triplication | Nine adapters and three type families differ mainly by field name. | Collapse to one type and one adapter when a fourth index tool appears. *(pass 18, Complexity lens)* |
| **Low** | ✓ verified | Optional-tool boundary primitives have multiple owners | SCIP, Stacklit, functional-clusters, pairing-index, toolchain, and Semble repeat shell quoting, bounded diagnostics, output suffixes, and environment-gate parsing with small semantic differences. | Consolidate only the primitives whose semantics can be named precisely; do not hide tool-specific policy behind a generic helper. *(pass 21, Duplication lens; reverified pass 22)* |
| **Low** | ✓ verified | `plugin/acp` has no production consumer | A public package of ~450 lines is imported only by two performance tests, and `plugin/` is outside both `REPOSITORY.md` and this document's measurement scope. | Confirm intended status; document it or move it under test scope. *(pass 18, Complexity lens)* |
| **Low** | ✓ verified | Undocumented tracked directories | `plans/`, `plugin/`, and `scripts/` are absent from `REPOSITORY.md`. | Add them or explain the omission. *(pass 18, Complexity lens)* |
| **Low** | ✓ verified | Current docs retain removed or undriven interfaces | The C4 model and overview still describe the removed MCP boundary, `supervision-model.md` assigns shared implementation to `commands`, and generated configuration advertises an undriven log-level setting. | Regenerate or correct current-state docs and remove configuration surfaces that have no runtime consumer. *(pass 21, Duplication lens; adjusted and reverified pass 22)* |
| **Low** | ✓ verified | `jsonout` imports `ops` | Presentation depends on the application layer to type-assert errors; §1.3 previously misfiled it as a leaf service. | Move the ops-error contract to a shared error package, or accept and document the crossing. *(pass 19, Boundaries lens)* |
| **Low** | ~ adjusted | `cmd/liza` bypasses `internal/commands` | Direct `ops` imports remain in six production command files while `internal/commands` is imported by seven; the adapter layer remains optional. | Decide whether `commands` is mandatory; if not, collapse the thin adapters. *(pass 19, Boundaries lens; counts rechecked pass 21)* |
| **Low** | ✓ verified | 32 verbatim JSON-envelope guards in `cmd/liza` | Each copy repeats the `ErrAlreadyWritten` deferred write and mutates the global logger via `log.SetOutput(io.Discard)`. | Extract one `withJSONEnvelope` wrapper. *(pass 19, Boundaries lens)* |
| **Low** | ✓ verified | `INVARIANTS.md` enforcement anchors are sparse and ambiguous | Anchors remain sparse, and bare filenames can resolve to multiple packages. | Package-qualify anchors and add them to the unanchored sections. *(pass 19, Boundaries lens)* |
| **Low** | ✓ verified | `internal/brand` dual API over mutable globals | The high-fan-in package exposes link-time globals *and* a parallel `Values` struct; tests mutate globals directly. | Pick one shape. *(pass 19, Boundaries lens)* |
| **Low** | ~ adjusted | Three packages have zero test files | `internal/alerts`, `internal/projectdetect`, and `internal/taskkind` still have no direct test files; pass 20 showed the first two are exercised by callers and the third has no executable statements. | Leave as is; add tests only if those packages acquire independent behavior. *(adjusted, pass 20; structural state rechecked pass 21)* |
| **Low** | ✓ verified | [No coverage gate](architectural-issues.md#coverage-basis-is-not-reproducible-by-repository-tooling) | Codecov uploads with `fail_ci_if_error: false`, and no repository threshold exists. Coverage is published but never enforced. | Decide whether coverage is a gate or an observation, and configure accordingly. Meaningful only after `-coverpkg` lands. *(pass 20, Coverage lens)* |
| **Low** | ~ adjusted | [118 zero-coverage functions, 59 exported](coverage-summary-2026-07-25.md#zero-coverage-functions) | This is a retained pass-20 full-profile measurement, not a claim derived from the current ignored `coverage.out`; the structural concentration in CLI and orchestration adapters remains relevant. | Triage the exported half after the default coverage command is corrected. *(pass 20, Coverage lens)* |
| **Low** | ✓ verified | Two exported functions with no caller | `commands.UnblockTaskCommand` and `models.(*DependencyResolver).UnmetDependencies` still have no production caller. | Delete both. *(pass 20, Coverage lens)* |
| **Low** | ~ adjusted | `internal/interactive` at 35.8% | The pass-20 full-profile measurement remains the latest complete figure; the small package is on first-run initialization, where failure is user-visible and recovery is manual. | Cover the prompt/response paths when initialization next changes. *(pass 20, Coverage lens)* |
| **Low** | ~ adjusted | Remaining serial test packages | There are 524 `t.Parallel()` text occurrences across 51 of 308 tracked Go test files, with a guard enforcing a minimum of 514, but adoption is concentrated in `internal/ops` and sliced integration. | Audit shared state package by package before extending parallelism to `internal/commands`, `internal/agent`, or `cmd/liza`. *(remeasured 2026-09-06)* |
| **Low** | ✓ verified | Stale `specs/plans` reference in tooling | `.pre-commit-config.yaml` still ignores `**/specs/plans/**` after the directory was deleted. | Prune the obsolete ignore independently. *(pass 20, Coverage lens)* |
| **Low** | ✓ verified | `.gitignore` embedded allowlist drift | `console.sh` and `support.md` are allowlisted but absent; `claudeignore` is tracked but not allowlisted. | Prune and add. *(pass 19, Boundaries lens)* |
| **Low** | ✓ verified | Pure domain predicates trapped in `ops` | Domain predicates inside `ops` still force `prompts → ops`, while infrastructure probes also live in the same package. | Relocate pure predicates to `models`/`pipeline` and infrastructure probes to `process`. *(pass 19, Boundaries lens)* |
| **Low** | ✓ verified | Inconsistent operation inputs | `AddTasks` accepts paths while most operations accept a project root. | Standardize when the next signature change occurs. |
| **Low** | ✓ verified | Filesystem existence checks | Some sites collapse permission/I/O errors into presence logic. | Use tri-state checks at correctness boundaries. |
| **Low** | ✓ verified | Timestamp inconsistency | Two merge timestamps use local time while surrounding state uses UTC. | Standardize persisted timestamps on UTC. |
| **Low** | ✓ verified | Watch thresholds and timeout scatter | Operational values span several packages and multiple top-level watch timing/retry constants. | Externalize only values with a demonstrated tuning need. |
| **Low** | ✓ verified | Fan-out priority flattening | Many-to-one child tasks receive priority `1`. | Specify priority inheritance for generated tasks. |
| **Low** | ~ adjusted | Direct validation/embedded entry-point test visibility | Pass-20 full-package coverage measured the validation internals as exercised; the remaining concern is direct visibility of composition behavior, not a broad validation coverage gap. | Add a focused composition test only when that path next changes. |
| **None** | ✓ verified | Pipeline resolver loaded per operation | The current call pattern keeps operations self-contained; overhead is negligible. | Accept. |
| **None** | ✓ verified | Explicit operation input guards | Repeated empty-ID checks are idiomatic and readable. | Do not abstract. |
| **None** | ✓ verified | Explicit history appends | The occurrences carry different event data and are clearer locally. | Do not abstract. |
| **None** | ✓ verified | Small `statevalidate` classifier repetition | Current helpers already share `containsStatus`; further abstraction has little payoff. | Accept. |
| **None** | ✓ verified | Global project logger | Appropriate for a CLI process. | Accept unless concurrent in-process consumers emerge. |

## Summary

Liza’s architecture remains appropriate for a local, stack-agnostic multi-agent orchestrator: inspectable file-backed state, process-safe mutation, isolated Git worktrees, declarative pipelines, and increasingly data-driven provider execution. The system has expanded substantially since the prior review, particularly in repository intelligence, provider/toolchain support, initialization, readiness, updating, branding, and architecture workflows. The largest current risks are not missing foundations but drift between contracts and enforcement, a handful of very large multi-responsibility modules, and read/query logic crossing intended layer boundaries. Those concerns merit incremental correction; they do not justify a broad rewrite.

Passes 18–22 and the adversarial follow-up converge on three risks. First, complexity is concentrated in a few deep orchestration functions rather than every large file. Second, intended boundaries are not enforcement boundaries: mutation and validation ownership can be bypassed, and 31 of 89 direct `cmd/liza` package calls reach `ops` without `commands`. Third, evidence and policy lack closure owners: the retained coverage baseline is not reproducible by repository tooling, and provider identity, stop-state recovery, and worktree-intelligence refresh cross otherwise valid package boundaries. Later passes mostly strengthened these themes rather than opening new architectural fronts, supporting incremental ownership corrections rather than a rewrite.

## Appendix: File Reference

| Component | Location |
|-----------|----------|
| CLI entry point | `cmd/liza/` |
| Domain model | `internal/models/` |
| Roles and task kinds | `internal/roles/`, `internal/taskkind/` |
| Blackboard persistence | `internal/db/`, `internal/filelock/` |
| State validation and hygiene | `internal/statevalidate/`, `internal/statehygiene/` |
| Application operations | `internal/ops/` |
| CLI commands | `internal/commands/` |
| Terminal UI and interaction | `internal/tui/`, `internal/interactive/`, `internal/termutil/` |
| Agent runtime | `internal/agent/` |
| Provider and tool configuration | `internal/providers/`, `internal/toolchain/`, `internal/codexconfig/` |
| Pipeline configuration | `internal/pipeline/` |
| Prompt generation | `internal/prompts/` |
| Embedded assets | `internal/embedded/` |
| Git and worktrees | `internal/git/`, `internal/gitenv/`, `internal/worktreeexclude/` |
| Repository intelligence | `internal/scipsearch/`, `internal/stacklit/`, `internal/semble/` |
| Functional clusters and pairing hooks | `internal/functionalclusters/`, `internal/pairingindex/` |
| Project/environment detection | `internal/projectdetect/`, `internal/envgate/`, `internal/initcheck/` |
| Updating and alerts | `internal/updater/`, `internal/alerts/` |
| Process and secret safety | `internal/process/`, `internal/procscan/`, `internal/secretmask/` |
| Branding and rendering | `internal/brand/`, `internal/brandrender/`, `internal/render/` |
| Paths, identity, and errors | `internal/paths/`, `internal/identity/`, `internal/errors/` |
| Output and logging | `internal/jsonout/`, `internal/log/` |
| Analysis and quality gates | `internal/analysis/`, `internal/precommit/`, `internal/testguard/` |
| Bash policy integration | `internal/bash-policy-cli/` |
| Test helpers and integration suites | `internal/testhelpers/`, `internal/integration/` |
| Architecture decisions | `specs/architecture/ADR/` |
| Persistent architecture issues | `specs/architecture/architectural-issues.md` |
