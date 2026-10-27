# Architecture Review — Liza

**Date:** 2026-10-27 (pass 23 at `ca8d76a3`, branch `global-refactoring`)
**Health check:** 2026-10-27 (same commit) verified every Phase 3 finding after the architecture-consolidation run: findings the run resolved moved to 2.4, the rest carry a refreshed status, and the measurement tables in 1.4 and 2.6 were remeasured; see 1.7. Pass 23 added independent discovery on top of it; see 1.5. An earlier targeted health check (2026-09-06) refreshed file sizes only.
**Adversarial pass:** 2026-10-27 (same commit), targeted at the code quality assessment's concerns; see 1.8. A second, smell-driven pass followed at `2f38c8fe` (no Go changes since `ca8d76a3`); see 1.9.
**Mode:** Adversarial (after pass 23)
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
  - [1.5 Pass 23 Verification and Gap Analysis](#15-pass-23-verification-and-gap-analysis)
  - [1.6 Adversarial Smell-Driven Gap Hunt](#16-adversarial-smell-driven-gap-hunt)
  - [1.7 Health Check 2026-10-27](#17-health-check-2026-10-27)
  - [1.8 Adversarial Gap Hunt from the Code Quality Assessment](#18-adversarial-gap-hunt-from-the-code-quality-assessment)
  - [1.9 Adversarial Smell-Driven Gap Hunt from the Write Path](#19-adversarial-smell-driven-gap-hunt-from-the-write-path)
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
| Go version | 1.25.5 (`go.mod`) |
| Production Go source | 149,233 lines (2026-10-27; scope in 2.6) |
| Test Go source | 351,367 lines (2026-10-27) |
| Test-to-production ratio | 2.35:1 (2026-10-27) |
| Production functions | 5,839 in tracked non-test Go, 1,983 of them in `internal/ops` (pass 23) |
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
- Prompt construction receives resolved data; the `prompts → ops` import was removed (A8). *(adjusted, pass 23)*
- `internal/embedded/embedded.go` is 128 lines; the run split its artifact families across the package. *(adjusted, pass 23)*
- `prompts.RoleContextData` is one flat 104-field struct for every role (`prompts/role_context.go:263`), 19 fields of it `Integration*` and 5 `Planner*`. The type comment states this is deliberate: irrelevant fields stay zero. *(pass 23, Complexity lens)*
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
- `internal/agent/supervisor.go` is 198 lines and `RunSupervisor` 29 (442 at pass 18); the loop now spans the `supervisor_*.go` files. *(adjusted, pass 23)*
- `RunSupervisor` maintains five in-memory loop-detection trackers whose resets are hand-paired across six scattered, asymmetric call sites. All are process-local, so a restarted supervisor resets its own circuit breakers. *(pass 18, Complexity lens)*
- `internal/agent/prompt.go` carries nine near-identical index-adapter functions and three near-identical type families for SCIP, Stacklit, and functional clusters, differing mainly by field name (`Path` vs `IndexPath`). *(pass 18, Complexity lens)*
- The three `available*IndexRefs` helpers discard discovery errors and return `nil`, so agents can silently lose index references from their prompts with no operator signal. *(pass 18, Complexity lens)*
- Quota and provider-unavailable stop signals duplicate the same path/glob/write/check/clear lifecycle in `quota.go` and `provider_unavailable.go`. *(pass 21, Duplication lens)*
- The fixed agent-lease half was resolved by C1 (see 2.4). Registration still records every supervisor under the named placeholder `terminalPlaceholder = "terminal-1"` (`supervisor_lifetime.go:21`). *(adjusted, pass 23)*

#### Application operations (`internal/ops/`)

**Purpose:** Implements task lifecycle operations used by the CLI and agent runtime.

**Pattern:** Service layer around blackboard transactions, pipeline resolution, Git operations, and history recording.

**Observations:**

- `internal/ops` is one package of 53,699 production lines in 241 files: 35% of production Go, 1,983 functions, 329 exported top-level functions. The run deliberately kept it whole: ADR-0222 through ADR-0225 each reject sub-packages on the grounds that "private concern files give the same separation". *(pass 23, Complexity lens)*
- That separation is not observable in the call graph. Only 52% of the 1,795 cross-file call sites to package-level functions stay within one concern family. The kernel family A3 calls into A4, A5 and A6 66 times. 47 of the 241 files appear in none of the four family ADRs, although `overview.md` says "the ADRs list every file". See 2.3. *(pass 23, Complexity lens)*
- `internal/ops/proceed.go` is 638 lines (1,563 at pass 18); transition expansion is spread over `transition_*.go`. *(adjusted, pass 23)*
- `await_verdict.go` and `await_resubmission.go` retain parallel watcher/timer/polling loop structures.
- `AddTasks` accepts state/log paths while most operations accept a project root.
- `SetTaskOutput` accepts an empty `spec_ref`, while proceeding and validation require one. The CLI also describes it as optional.
- Per-subtask fan-out still assigns priority `1`; one-to-one transitions preserve the parent priority.
- The layer contains both the formal project logger and direct `log.Printf` calls.
- The pass-18 transaction scripts of 200-320 lines are gone; no `ops` function exceeds 100 lines. Their ordering now goes through kernel helpers (`lifecycle_authority.go`, ADR-0222), and the `// Phase N` comment convention survives in 5 files. *(adjusted, pass 23)*
- Of the 141 production functions with seven or more parameters, 102 are in `ops`. The worst are `buildChildTask` and `awaitVerdictPolling` (12 parameters each) and `submitVerdict` (11, eight of them `string`: `submit_verdict.go:352`). 78 `ops` functions take `projectRoot`, `taskID` and `agentID` positionally, and 57 take `taskID`, `agentID` and `authority`. *(pass 23, Complexity lens)*
- Production `ops` code declares 37 distinct package-level mutable `*TestHook` seams (for example `mode_change.go:59-61`, `pipeline_ops.go:51-52`). *(pass 23, Complexity lens)*
- The submission TDD gate recognises test files through a hardcoded nine-language matcher (`test_detection.go:10-130`), and the gate cannot be configured per project. *(pass 23, Complexity lens)*
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
- Only `commands/status_orchestrator.go` still imports `internal/agent`, as a K4 allowlist entry. *(adjusted, pass 23)*
- CLI and TUI resume adapters independently glob and clear both provider stop-signal families after `ops.Resume`; `ops.Resume` itself does not own that recovery semantic. *(pass 21, Duplication lens)*
- `internal/commands/watch.go` is 151 lines and `init.go` 508; watch logic is spread over `watch_*.go`. *(adjusted, pass 23)*
- `commands` still holds a pool-capacity planner and spawner: `repair_agent_pool*.go` with `watch_repair.go`, about 1,900 lines of decision logic over `models.State`. The `capacityDemand` struct alone has 23 fields. It is read-only, so C6 permits it. *(pass 23, Complexity lens)*
- Formatting helpers use the standard library `slices.Sort`; the former hand-written bubble sort is gone.
- TUI panels render from per-panel column types (`view_agents.go`, `view_tasks.go`), and `ops/init_project.go` was deleted by C4. The pass-18 duplication observations are historical; see 2.3 and 2.4. *(adjusted, pass 23)*

#### Repository intelligence and optional tooling

**Locations:** `internal/scipsearch/`, `internal/stacklit/`, `internal/semble/`, `internal/functionalclusters/`, `internal/pairingindex/`, `internal/statehygiene/`, `internal/envgate/`, `internal/projectdetect/`

**Purpose:** Adds code indexing, semantic exploration, functional-cluster analysis, pairing hooks, environment readiness, state hygiene, and project detection.

**Pattern:** Optional adapters and focused application services around the core orchestration flow.

**Observations:**

- SCIP and Stacklit integration is optional rather than a core blackboard dependency.
- Semble and functional-cluster support add architecture-aware discovery without changing blackboard ownership.
- Pairing-index hooks and environment gates expand initialization and session-start behavior.
- SCIP, Stacklit, and functional clusters independently own similar plan/runner/result contracts; `functionalclusters` directly composes Stacklit and SCIP while `pairingindex` enumerates concrete backends. Adding another index backend therefore crosses several package boundaries. *(pass 22, Coupling lens)*
- `internal/scipsearch/scipsearch.go` is 155 lines (1,566 at 2026-09-06); the package is 1,654 lines over 9 files. *(adjusted, pass 23)*
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

The layer model is now documented authoritatively in [overview.md — Go Code Structure](overview.md#go-code-structure) and enforced by `TestK4ImportDirectionGuard` in `make lint`. This section keeps only the review-relevant summary. *(health check 2026-10-27)*

```text
Composition root     cmd/liza ──► any layer
Presentation         commands · tui · interactive · jsonout ──► ops
                     commands ──► agent   (allowlisted: read-only wake projection)
                     tui ──► agent        (allowlisted: snapshot-only pause reason)
Runtime              agent ──► ops ──► domain
                     agent ──► db         (allowlisted: non-mutating handle use only)
Application          ops ──► domain · infrastructure      (sole mutation boundary, C6)
Domain               models · pipeline · roles · taskkind (no upward imports)
Prompts              prompts ◄── resolved data only       (no ops import)
Infrastructure       db · filelock · git · gitenv · providers · providersignal ·
                     agentlaunch · embedded · scipsearch · stacklit · semble · …
Shared leaves        paths · identity · errors · render · log · brand
```

The graph remains acyclic. Of the four direction exceptions recorded at pass 19, `prompts → ops` and `jsonout → ops` were removed; `commands → agent` and `agent → db` remain as allowlist entries with stated rationale, joined by `tui → agent`. Blackboard mutation outside `ops` and `db` now fails `TestC6MutationGuard`. *(health check 2026-10-27)*

The Coupling lens finding still holds: the expensive dependencies are behavioral rather than cyclic. Provider onboarding, stop-state cleanup, semantic state validation and worktree-intelligence refresh each still require coordinated changes across otherwise valid package boundaries. *(pass 22, Coupling lens; reverified 2026-10-27)*

### 1.4 Coverage Checkpoint

#### Large production Go files

Repository-wide ranking at `ca8d76a3` (physical lines, production Go under `cmd/` and `internal/`). No production file exceeds 800 lines; `TestS1SizeGuard` enforces that bound. *(health check 2026-10-27)*

| File | Lines |
|------|------:|
| `internal/db/blackboard.go` | 786 |
| `internal/usage/report.go` | 765 |
| `cmd/liza/cmd_init.go` | 742 |
| `internal/analysis/patterns.go` | 739 |
| `internal/ops/orphaned_review_base.go` | 726 |
| `cmd/liza/cmd_review.go` | 705 |
| `internal/ops/claim_task_strategy.go` | 678 |
| `internal/statevalidate/integration.go` | 672 |
| `internal/commands/setup.go` | 663 |
| `internal/toolresult/redact.go` | 654 |

The ten files that headed this table at 2026-09-06 (`embedded.go` 1,635 through `pairingindex/hooks.go` 1,077) were all decomposed by the consolidation run.

#### Longest production Go functions

Measured with `go/parser` (`FuncDecl` with a body, declaration line to closing brace) at `ca8d76a3`. Only three production functions exceed 100 lines, and none exceeds 153. *(health check 2026-10-27)*

| Function | Location | Lines |
|----------|----------|------:|
| `Catalog` | `internal/toolchain/catalog.go:93` | 153 (the S2 table exception) |
| `reconcile` | `scripts/ci/testshard/reconcile.go:78` | 138 (outside S1 scope) |
| `MeasureRendered` | `internal/prompts/promptbench/measure.go:95` | 132 (excluded from S1) |
| `reconcileIntegrationAnalyses` | `internal/ops/integration_reconcile.go:72` | 100 |
| `releaseTaskClaim` | `internal/ops/agent_exit_release.go:83` | 99 |
| `Analyze` | `internal/ops/analyze.go:79` | 98 |

The pass-18 table (`RunSupervisor` 442 through `Replan` 194) is historical. Its conclusion — that depth, not width, was where decomposition paid — guided the run's ratchet.

#### Complexity S1 does not measure *(pass 23, Complexity lens)*

S1 counts lines only. Pass 23 measured branch count (cyclomatic: 1 + `if`/`for`/`range`/`case`/`&&`/`||`), parameter count and struct width with a `go/ast` scan of tracked production Go. It compared the result with the S1 starting commit `96c9ad69`.

| Measure | Run start | `ca8d76a3` |
|---|---:|---:|
| Functions | 4,683 | 5,583 |
| Longer than 100 lines | 93 | 1 |
| Cyclomatic ≥ 20 | 181 | 101 |
| Seven or more parameters | 117 | 141 |
| Mean cyclomatic | 5.65 | 4.94 |

Test-support packages (`testhelpers`, `testguard`, `promptbench`) and `scripts/` are excluded from both columns.

| Highest current cyclomatic | Location | Cyclomatic | Lines |
|---|---|---:|---:|
| `validatePlanAmendment` | `statevalidate/validate_plan_amendment.go:12` | 70 | 84 |
| `validatePrematureIntegrationRecovery` | `ops/recover_integration.go:161` | 48 | 71 |
| `DeferredValidationLedgerViolations` | `models/deferred_validation.go:472` | 44 | 59 |
| `applyPlanAmendment` | `ops/amend_plan.go:184` | 43 | 85 |
| `retirePlanHandoff` | `ops/plan_check_decision.go:97` | 39 | 76 |

The widest structs are `prompts.RoleContextData` with 104 fields, `models.Task` with 75 (26 of them pointers) and `models.Config` with 50. Test files sit outside S1: 22 exceed 2,000 lines, and `ops/proceed_test.go` alone has 7,781.

#### Checkpoint questions

| Question | Current assessment |
|----------|--------------------|
| What exists that should not? | `plugin/acp` (~450 production lines) is a public package under the module path whose only importers are two `internal/agent` performance tests. `plans/`, `plugin/`, and `scripts/` are tracked but absent from `REPOSITORY.md`, and `plugin/` is also outside this document's measurement scope. `commands.UnblockTaskCommand` still has no production caller. *(pass 20, Coverage lens; `UnmetDependencies` gained a caller at `ops/state_lock_holds.go:81`, health check 2026-10-27)* Current C4 documentation also still models the removed `internal/mcp` subsystem. *(pass 21, Duplication lens)* |
| What is implicit that should be explicit? | Review-submission cleanliness, verdict-time provider diversity, and sprint-summary loss are not aligned across contracts and enforcement. Index-discovery failures in `agent/prompt_intelligence.go` and the role coverage of the prompt template chains are silent at runtime. *(pass 18, Complexity lens)* The coverage contract is implicit throughout: `make coverage` gained `-coverpkg` but CI publishes no profile and no threshold exists *(adjusted, health check 2026-10-27)*. Nothing states what level of coverage the project intends to hold. *(pass 20, Coverage lens)* Worktree-intelligence refresh order and provider-signal cleanup are policies encoded only by repeated call sequences. *(pass 21, Duplication lens)* |
| What was missing from the old walkthrough? | Provider catalogs, toolchains, update support, indexing, Semble, functional clusters, pairing hooks, branding, readiness gates, secret masking, and other post-ADR-0057 packages. |
| What requires cross-file comparison? | Invariants versus operations, prompt queries versus ops ownership, command queries versus agent ownership, worktree path construction, await loops, timestamp handling, identity validation, provider-signal lifecycles, index-refresh sequences, and optional-tool shell/diagnostic helpers. |

### 1.5 Pass 23 Verification and Gap Analysis

**Discovery basis.** Phase 1 ran before the findings were read. Only the header was consulted, and a tag count established the previous lens. Discovery used `go/ast` scans of every tracked production file: function length, cyclomatic count, parameters and struct width. It added a call graph of package-level functions inside `internal/ops`, mapped onto the concern families named in ADR-0222 to ADR-0225, plus direct reads.

**Verification.** HEAD is still `ca8d76a3`, the commit the 2026-10-27 health check verified row by row. Re-running those checks against an unchanged tree would be repetition, not evidence, so the Phase 3 statuses from that check stand, except for the semantic-validation row, which pass-23 measurement recounted.

What the health check had not refreshed was §1.2, §2.1 and the Summary. Pass 23 corrected them: 10 walkthrough observations were adjusted to current sizes or locations, and 2.1 row 8 and the Summary no longer describe the mutation bypass that C6 closed.

**New findings:**
- Ops concern-family ownership is nominal. The four ADRs justify keeping `ops` whole by a separation the call graph does not show, and 47 files have no family.
- The TDD gate's test-file matcher is a hardcoded language list (G1.1).
- Branch density concentrates in the plan-amendment family, whose correction disposition is encoded as membership in three lists.
- 37 mutable test hooks are declared in production `ops`.
- Positional identity clumps and wide flat structs.

**Hypothesis refuted by measurement.** I expected S1 to have displaced complexity from length into parameter lists. It did not. The share of functions with seven or more parameters is 2.5% at both the run start and now. The run cut cyclomatic ≥ 20 functions from 181 to 101, so it reduced branching, not only length (see 2.2).

**Existing findings not rediscovered independently:** the contract gaps, coverage tooling, provider-extension and stop-signal ownership, JSON error typing and documentation drift. The Complexity lens did not look for them. This was an attention shift, not contrary evidence.

**Method limits:** call-graph edges count bare-identifier calls to package-level functions only, so method calls and function values are missing and the edge counts are lower bounds. Family membership uses literal and glob file citations in the four ADRs, excluding the prose `*.go` mention in each ADR's citation rule. Counted against the boundary records and K7 inventories as well, 36 files remain unassigned.

### 1.6 Adversarial Smell-Driven Gap Hunt

Entry smells were provider support crossing the catalog boundary, duplicated provider stop-state recovery, and provider-diversity enforcement drift.

Forced comparisons covered catalog identity versus supervisor identity, quota versus provider-unavailable/audit normalization, and registration versus merge-gate provider semantics. Existing findings were not reverified or removed.

The new gap is a missing canonical provider identity at runtime policy boundaries: the catalog models one, but registration, review diversity, provider-unavailable signals, and audit anomalies operate on raw tool names.

### 1.7 Health Check 2026-10-27

**Scope:** verification only, after the [architecture-consolidation run](consolidation-run/run-close.md) (1,103 commits since the 2026-09-06 check; production Go 76,738 → 149,233 lines). The tree was checked at `ca8d76a3`. Each Phase 3 row was checked against the source, with registry entries consulted for code references the run had already corrected. No independent discovery was performed, so absence of new findings carries no signal.

**Result (Phase 3, 65 rows before):**
- 13 rows resolved by the run or stale, moved to 2.4. They cover the decompositions, duplicate initialization, mutation ownership (C6), the import-direction check (K4), `prompts → ops`, `jsonout → ops`, agent lease (C1), canonical provider identity (C2/C3), merge timestamps (C5), `spec_ref`, and the `specs/plans` ignore.
- 39 rows re-checked at `ca8d76a3`: 16 ✓ verified unchanged, 23 ~ adjusted for new locations or partial progress.
- 13 rows carried without re-measurement:
  - the INVARIANTS §6/§7 and sprint-metric rows, whose registry entries the run close re-confirmed;
  - the §12/§3 drift row (§12 text re-read);
  - the historical pass-20 coverage rows and the location-less filesystem row;
  - five `None` judgement rows.
- No finding reopened.

**Adjustments worth reading first:**
- `cmd/liza` now imports `ops` from 36 production files (pass 19 counted 6). Under the documented composition-root rule this is permitted, so the "optional adapter layer" question has been answered by policy rather than by collapsing `commands`.
- The JSON-envelope guard was not extracted: `ErrAlreadyWritten` is referenced in 37 `cmd/liza` files, and `log.SetOutput(io.Discard)` appears 43 times.
- `exec.ts` now has a runtime test (`internal/embedded/opencode_exec_runtime_test.go`), but it skips without Bun, and no CI workflow installs Bun.
- Prompt template chains grew (`review_instructions.tmpl` 392 → 521 lines, `implementation_phase.tmpl` 240 → 404) without a role fallback.

### 1.8 Adversarial Gap Hunt from the Code Quality Assessment

**Entry:** the concerns of the [2026-10-27 code quality assessment](code_quality_assessment.md), used as leads for adjacent smells. These were redaction tested at about 1:1, `analysis/patterns.go` and `db/blackboard.go` listed by size only, the tooling adapters and the Python utilities. This review and the registry were read only after discovery.

**Forced comparisons:**
- the three secret-masking policies, against each other;
- `db.ReadCached` against the TUI's publication check and `ops/review_reservation.go`;
- circuit-breaker pattern code against `specs/protocols/circuit-breaker.md`;
- Go terminal-archive decoding against `skills/liza-logs/scripts/analyze-state.py`;
- the tool-result event store against the usage record store;
- the Stacklit and functional-clusters adapters.

**New findings (see 2.3):** fragmented secret redaction; mtime-only state cache identity; circuit-breaker pattern vocabulary split across producer, consumer and spec; tool-result telemetry without retention; the terminal-archive format implemented twice.

**Already covered, not re-added:** the adapter clone family and its divergent truncation (Optional-tool boundary primitives). One further divergence: the adapters disagree on keeping a copied artifact out of Git. Stacklit refuses an untracked, unignored `stacklit.json` (`stacklit.go:149-172`), while functional clusters writes a repository exclude (`functionalclusters.go:152-168`).

**Searched without a finding:**
- C6 bypass through Python: the skill scripts only read `state.yaml`, and `blackboard_op.py` mutates the adversarial-pairing Markdown blackboard, not Liza state.
- The unbounded context in `ops/admit.go:86`: this is deliberate, since it wraps the operator's own command.

**Method limits:** existing findings were not reverified (Adversarial mode). The mtime premise was measured on this host (ext4, Linux 6.8): 1,694 of 2,000 back-to-back atomic publications shared an mtime. How often real state writes fall in one tick was not measured.

### 1.9 Adversarial Smell-Driven Gap Hunt from the Write Path

**Entry smells:** semantic validation off the write path, the TDD gate's hardcoded conventions, and filesystem error asymmetry. None had served as an adversarial entry before. Only their 2.3 text was read before discovery; the rest of this review and the registry were read afterwards.

**Forced comparisons:**
- the 13 temp-file-and-rename writers, against each other on file fsync, directory fsync and mode;
- `Blackboard.Write` against `Blackboard.Modify` on gates, lock scope and sequence ownership;
- the documented lock-order chains against the lock identities the code constructs.

**New findings (see 2.3):** state migration publishes outside one lock; lock order is partial and leaves the task-worktree lock unranked; state publication is less durable than the side records it references.

**Adjacent signal added to an existing finding:** `Modify` already runs three in-lock semantic gates (see "Semantic validation sits off the write path").

**Searched without a finding:**
- More G1.1 hardcoding near the TDD gate. `isTestFile` has a single owner; prompt examples name several stacks; Node detection only fills an unset `post_worktree_cmd`.
- Filesystem existence checks at worktree boundaries: `review_boundary.go:110`, `orphaned_review_base.go:245` and `git/worktree.go:457-470` are already tri-state. The one remaining binary check, `deferred_validation_lanes.go:199`, skips lane removal on any `Stat` error.
- Non-idempotent `add-task`: task IDs are supplied by the caller, so a retry is refused as a duplicate, not applied twice.
- The `runtimeinputs` and `sessionvalidation` packages, which this review never names: their masking is already covered by the redaction finding.

**Method limits:**
- Existing findings were not reverified (Adversarial mode).
- Durability impact is reasoned from code, not measured; no crash test was run.
- Lock inversion was not searched for exhaustively. The finding rests on the order having no single owner, not on a demonstrated inversion.

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
| 8 | Who owns data and invariants? | `models` defines data; `db.Blackboard` owns persistence serialization and the lock boundary; `pipeline` defines configurable lifecycle semantics. `ops` is the exclusive mutation owner, which `TestC6MutationGuard` enforces. Ownership of the capabilities *inside* `ops` is a documentation convention without enforcement (2.3). *(adjusted, pass 23)* |
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

#### The size ratchet reduced branching, not only length *(pass 23, Complexity lens)*

A line-count guard invites splitting a long function into several dense ones that pass state through long parameter lists. The run did not do that. Between the S1 starting commit and `ca8d76a3`:
- functions with cyclomatic ≥ 20 fell from 181 to 101;
- mean cyclomatic fell from 5.65 to 4.94;
- the share of functions with seven or more parameters held at 2.5%.

`cmd/liza`, the composition root, peaks at 16. The decomposition removed decision complexity; it did not relocate it.

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

*Health check 2026-10-27:* §6 and §7 remain open registry entries, which the run close left unchanged. §12 still states `TRIPPED→PAUSED` (`INVARIANTS.md:397`). §3 was not re-traced.

**Impact:** Operators and agents can reason from guarantees that are not implemented at the stated boundary.

**Direction:** Choose the intended behavior at each mismatch, then align the invariant and enforcement point.

#### Role extensibility stops at the prompt boundary *(pass 18, Complexity lens)*

**Smell:** Leaky abstraction / capability claim not carried through.

**Signals:**

- `supervisor_lifetime.go:93-95` (formerly `supervisor.go:648-650`) states that the resolver reads role definitions from pipeline YAML, "enabling custom YAML-defined roles without Go code changes".
- Pipeline validation checks `role.Type` against `doer|reviewer|orchestrator` but never checks the role *name*; `agent/registration.go` validates only the `{role}-{number}` ID shape and never calls `roles.IsValid`.
- The closed 13-role registry in `internal/roles` is consulted in exactly one production path, on an already-inferred role.
- `review_instructions.tmpl` and `implementation_phase.tmpl` branch on hardcoded role names with no role fallback; the chains have grown to 521 and 404 lines. *(adjusted, health check 2026-10-27)*

**Impact:** A YAML-defined role validates, resolves, registers, and launches — then receives an empty instructions block. The failure is silent at every layer, and the declarative-lifecycle strength holds for the state machine but not for prompt content.

**Direction:** Decide whether custom roles are supported. If yes, give the templates a fallback and make role coverage checkable at config-load time; if no, validate role names where pipeline config is loaded and correct the comment.

#### Parallel implementations of the same capability *(pass 18, Complexity lens)*

**Smell:** Copy-paste divergence.

**Signals:** The initialization duplicate was removed (C4). TUI panels now render through per-panel column types (`view_agents.go` `agentColumn`, `view_tasks.go` `taskColumn`) rather than one shared renderer. `prompts/role_context.go:200-212` still carries three index-ref types, with adapters in `agent/prompt_intelligence.go`. *(adjusted, health check 2026-10-27)*

**Impact:** Fixes land in one copy. The `gitenv` divergence is the concrete instance: locale and timeout hardening applies to one initialization path and not the other.

**Direction:** The remaining raw Git call (`cmd/liza/launch_resolve.go:127`) is the correctness item; treat render and adapter duplication as lower-priority consolidation.

#### Unobservable index-discovery failure *(pass 18, Complexity lens)*

**Smell:** Silent failure.

**Signals:** `availablePromptScipIndexRefs`, `availablePromptStacklitIndexRefs`, and `availablePromptFunctionalClusterIndexRefs` (`agent/prompt_intelligence.go:96,110,123`) each still return `nil` on error with no logging. *(verified, health check 2026-10-27)*

**Impact:** Agents receive prompts missing their code-index references and behave as if none were configured. Degraded exploration is indistinguishable from an unindexed repository.

**Direction:** Log at warning level and keep the nil return; the degradation itself is acceptable, its invisibility is not.

#### Coverage measurement misreports the packages it is used to judge *(pass 20, Coverage lens)*

**Smell:** Instrument that reads wrong in the direction of complacency.

**Signals:**

- `make coverage` runs `go test -coverprofile=<unique-temporary-profile> ./...` with no `-coverpkg`, so each package is credited only with coverage produced by its own test binary. Routine `make test` intentionally collects no coverage.
- Under that default, `internal/alerts` and `internal/projectdetect` read 0.0% and `internal/errors` 12.7%. Measured with `-coverpkg=./...` they are 89.7%, 92.9%, and 91.1%. Neither package has a test file of its own; both are exercised thoroughly by their callers.
- `models.ValidateDependsOn` reads 22.2% by default and 100% cross-package. Three prior passes recorded "untested entry point" findings that dissolve under correct measurement.
- `.github/workflows/ci.yml` still attempts to upload `coverage.out` with `fail_ci_if_error: false`, but its preceding `make test` step no longer produces that file. The repository has no `codecov.yml` or threshold, and CI wiring for `make coverage` is explicitly deferred.

*Health check 2026-10-27:* `make coverage` now runs `go test -coverpkg=./... -coverprofile=…` (merged `de166d2d`), so the per-package misreporting is fixed locally. The target is still interactive and self-deleting. The Codecov step was removed rather than wired, so CI publishes no coverage at all.

**Impact:** The published number is systematically wrong for exactly the packages this review keeps flagging — thin leaf packages consumed from elsewhere. It under-reports them, which is the harmless direction for the package but the harmful direction for the reviewer: three passes have now spent attention on false gaps, and the same artifact hides the real ones behind noise. Codecov is decorative, not a gate.

**Direction:** Give CI an explicit retained-profile path on the `-coverpkg` basis before any coverage-driven work.

#### The human-readable CLI path is structurally unmeasured *(pass 20, Coverage lens)*

**Smell:** Untested branch of a deliberately duplicated path.

**Signals:**

- All 69 Cobra commands are package-level `var … = &cobra.Command{ RunE: func… }` literals. `go tool cover -func` walks `FuncDecl` only, so every `RunE` body is absent from per-function output — the standard gap-hunting tool cannot see the CLI's behavior. This alone accounts for the gap between `cmd/liza` at 62.4% by block arithmetic and 78.4% by `-func`.
- Ten exported `internal/commands` entry points sit at 0.0%: `AddTasksCommand`, `ProceedCommand`, `SetTaskOutputCommand`, `WriteCheckpointCommand`, `RecoverTaskCommand`, `RepairAgentPoolCommand`, `MarkAgentDegradedCommand`, `ClearAgentDegradedCommand`, `AssessHypothesisExhaustedCommand`, `SetDiscoveryDispositionCommand`, along with seven unexported formatters (`formatTaskSummaryOutput`, `printProceedResult`, and siblings).
- Each is called from exactly one `RunE` body and referenced by zero tests. `UnblockTaskCommand` is called from nowhere at all.
- The `--json` sibling of each command routes to `internal/ops`, which is at 84.3% over 6,816 statements.

*Health check 2026-10-27:* 9 of the 10 named `*Command` functions still have no direct test reference (`RepairAgentPoolCommand` has one), and `UnblockTaskCommand` still has no caller. The oracle now holds 961 human-output golden files, which exercise rendered output black-box for commands whose `ops` entry the run refactored. The structural gap is therefore narrower than measured at pass 20, but no fresh coverage run quantifies it.

**Impact:** The dual-path design documented in §1.3 and §2.5 has one path tested and one path not. Agents consume `--json` and are covered; humans consume the rendered path and are not. Output formatting, error phrasing, and result rendering for a dozen mutating commands can break without any test failing — and the tooling a reviewer would reach for to notice this reports `cmd/liza` 16 points higher than it is.

**Direction:** Either test the render path — table-driven tests over the `*Command` functions are cheap, they already take a project root and return `error` — or collapse it, since the same finding read from the Boundaries lens says these adapters are optional. Delete `UnblockTaskCommand` regardless.

#### Application/query boundary erosion

**Smell:** Dependency inversion and inappropriate intimacy.

**Signals:** `prompts → ops` was removed (A8) and prompts now receive resolved data. `commands → agent` and `tui → agent` remain as allowlisted read-only crossings. Read-only query logic still spans `commands`, `agent`, and `ops`. *(adjusted, health check 2026-10-27)*

**Impact:** Presentation and prompt changes acquire unnecessary transitive dependencies on mutation-oriented application code.

**Direction:** Move stable read models and lifecycle queries toward their owning domain/pipeline package, or introduce a focused query boundary when another concrete consumer justifies it.

#### Semantic validation sits off the write path *(pass 19, Boundaries lens)*

**Smell:** Validation placed where it can be skipped.

**Signals:**

- `internal/statevalidate` is 4,908 LOC. It is imported by 32 `ops` files, 4 `payloadschema` files and `commands/validate.go`; `internal/db` is not among them. Coverage of the write path grew, but it stays opt-in: `ops` calls `statevalidate.ValidateCandidate` at 24 sites against 60 `.Modify(` call sites. *(adjusted, pass 23)*
- `db.marshalStateForWrite` calls `statehygiene.ValidateState` only — payload size and scrubbing, not invariants.
- `PostWriteValidationError` is constructed at two sites. *(health check 2026-10-27: `internal/db` still does not import `statevalidate`)*
- The write boundary already enforces some invariants, one ADR at a time, without `statevalidate`. `Modify` runs three in-lock gates over `models` rules (`blackboard.go:540-631`, called at `:677-689`): anomaly violations (ADR-0166), runtime-input ledger transitions (ADR-0169), and runtime-input/deny name collisions (ADR-0213). The question is therefore no longer whether to validate in the lock. It is whether `db` gates and `statevalidate` remain two homes for invariants, with `Write` bypassing the first. *(Adversarial pass, smell-driven)*

**Impact:** An invalid state can be written and persisted; it is detected later by `liza validate` or not at all. Validation strength depends on which operation was used rather than on what was written.

**Direction:** Classify `statevalidate` checks into always-cheap invariants (candidates for the in-lock gate) and expensive/whole-corpus checks (keep on demand). Placing even a subset behind `Modify` would make the write boundary the enforcement boundary.

#### Ops concern-family ownership is nominal *(pass 23, Complexity lens)*

**Smell:** Orphaned abstraction / unenforced internal boundary in a god package.

**Signals:**

- `internal/ops` holds 53,699 production lines in 241 files, 35% of production Go. ADR-0222 through ADR-0225 each reject sub-packages because "private concern files give the same separation".
- `overview.md` §Permanent component groups says "the ADRs list every file". 47 of the 241 files appear in none of the four ADRs, including:
  - agent registration, health, deletion and lease (`agent_registration.go`, `agent_health.go`, `delete_agent.go`, `lease.go`);
  - state initialization and migration (`init_state.go`, `migrate_state.go`);
  - checkpoints (`write_checkpoint.go`);
  - shared helpers (`helpers.go`, fan-in 24).
- Only 52% of the 1,795 cross-file call sites to package-level `ops` functions stay within one family. The kernel A3 calls into A4 31 times, A6 23 times and A5 12 times. A4 and A5 call each other 44 times in each direction.
- `pipeline_ops.go` is filed under A4 "Sprint coordination" (ADR-0223). It is called from 74 of the other 240 `ops` files, because it holds `loadResolver` (43 callers), `BuildPipelineTransitions` and the integration-completion authorization that A6 owns conceptually.
- Generic helpers live in whichever family first needed them. A3's `claim_task_strategy.go` calls `shortSHA` from A6's `wt_merge.go` six times, and `uniqueSortedStrings` sits in `integration_progress.go`. Copies of both exist elsewhere: `git/merge.go:312`, `updater/versions.go:192`, `statevalidate/validate_deps.go:108`, `ops/replan.go:576`.

**Impact:** The "Where new code goes" rule tells contributors to put an operation "in the concern family owning the capability". For a fifth of the files no family exists, and nothing fails when a family calls across. The equivalence the ADRs rely on to reject sub-packages is asserted, not checked. Without a check it erodes one convenient call at a time, as the kernel calling into merge and integration already shows. A3 to A6 cannot be reviewed or owned independently, which was the run's stated purpose for them.

**Direction:** Do not reopen the sub-package decision; its exported-seam cost argument holds. Either make the claim checkable, or withdraw it:
- *Checkable:* a K4-style archguard test over file families inside `ops`, with a rationale-carrying allowlist seeded from today's edges. Assign the 47 files, and move resolver loading and the generic helpers into the kernel or a leaf.
- *Withdrawn:* state in the overview that `ops` capability groups are review aids, not boundaries.

The first costs one guard plus the file assignments; the second costs one paragraph.

#### TDD gate hardcodes the target project's test conventions *(pass 23, Complexity lens)*

**Smell:** Hardcoded configuration at a stack-agnostic boundary (GUARDRAILS G1.1).

**Signals:**

- `ops/test_detection.go:62-130` `isTestFile` is a cyclomatic-30 matcher over Go, Python, JS/TS, shell, Ruby, Java, Kotlin, C# and Rust naming. `testFileMatcherPatterns` (`:10-31`) is a hand-kept display copy of the same rules.
- `submissionRun.enforceTDD` (`submission_git_work.go:119-148`) refuses a doer's code submission when no changed file matches. The only bypass is `--tdd-not-required`, which the error text reserves for non-behavioural work.
- No configuration field feeds the matcher. Language coverage has grown by code fixes: `66f802f1b fix(tdd): recognize C# test filenames at submission` and `44035e294 fix(admission): recognize Node module test files`.

**Impact:** In a project whose tests follow another convention — PHP `*Test.php`, Swift `*Tests.swift`, Elixir `*_test.exs`, C/C++ `test/*.c` — a correct code submission with tests is refused. The agent then either claims a waiver meant for non-behavioural work or stalls. This fails the G1.1 test: "Would this work for a Python project with no Makefile?" generalises to "would this work for a stack Liza's authors did not list?".

**Direction:** Keep the built-in list as the default, and add a project-level test-file pattern list in `Config` that extends or replaces it. Generate the display patterns from the same table so they cannot drift.

#### Residual complexity outside the size guard *(pass 23, Complexity lens)*

**Smell:** Implicit state machine; long parameter lists; hidden mutable state.

**Signals:**

- Branch density concentrates in plan amendment. `validatePlanAmendment` (cyclomatic 70 in 84 lines), `applyPlanAmendment` (43, 8 parameters) and `retirePlanHandoff` (39) lead the repository. The underlying cause is representation: a correction's disposition is membership in `PlanAmendment.Pending`, `.Applied` and `.Quarantined` (`models/plan_amendment.go:53-59`), so "exactly one disposition" becomes a four-term boolean (`validate_plan_amendment.go:54`). The feature spans 27 files in six packages.
- Of the 141 functions with seven or more parameters, 102 are in `ops`, with identity clumps passed positionally. `submitVerdict` takes eight `string` parameters, so swapping `verdict` and `reason`, or `impact` and `requestReason`, still compiles.
- 37 package-level mutable `*TestHook` variables live in production `ops`. They make interleaving tests of lock and CAS paths possible. They are also process-global state, so a test that sets one cannot safely run in parallel with other tests of the package.
- `RoleContextData` (104 fields) and `models.Task` (75) are flat. Each role or lifecycle phase uses a subset, and a template that references a field its role does not populate renders empty silently, the same failure mode as the role-fallback finding above.

**Impact:** These lower readability and review confidence locally; none is a correctness defect today. S1 cannot see any of them, so they are the next place complexity will accumulate unobserved.

**Direction:** Lowest cost first:
- give plan-amendment corrections an explicit per-correction disposition type at the next schema change to that record;
- introduce small identity structs for the recurring `taskID`/`agentID`/`authority` clump when signatures are next touched;
- leave the hooks and flat structs unless they block a parallelism or role-extension change.

A cyclomatic ceiling in S1 is possible but would be a second ratchet to maintain; propose it only if density grows.

#### Machine-readable error codes derive from prose *(pass 19, Boundaries lens)*

**Smell:** Stringly-typed contract at an integration boundary.

**Signals:**

- `jsonout/classify.go` maps errors to `code` values by substring match on the message: `"not found"`, `"must be"`, `"is required"`, `"validation failed"`, `"race condition"`, `"not IMPLEMENTING"`, and others.
- `internal/ops` contains 319 non-test `fmt.Errorf` calls against 8 typed error types, of which 5 are handled in `classify.go`.
- ~~`jsonout` imports `ops`~~ — removed by the run (A7). The substring rule table remains (`classify.go:10-26`), now after typed `errors.As` checks. *(adjusted, health check 2026-10-27)*

**Impact:** Agents consuming `--json` branch on a code derived from English phrasing. Rewording an error message is a silent contract change; a message that happens to contain "must be" is classified as a validation failure regardless of cause.

**Direction:** Type the errors that agents actually branch on and delete the corresponding string rules. The prose table can remain as a fallback, but it should not be the primary classifier for codes the multi-agent protocol depends on.

#### `INVARIANTS.md` enforcement anchors are sparse *(pass 19, Boundaries lens; narrowed, health check 2026-10-27)*

**Smell:** Convention without a check.

**Signals:** The import-direction half of this finding is resolved: `TestK4ImportDirectionGuard` runs in `make lint` against a three-entry allowlist. `INVARIANTS.md` still carries 7 `**Enforced:**` anchors across 16 sections, and anchors such as `config.go` and `mode_change.go` (`INVARIANTS.md:397`) remain bare filenames.

**Impact:** A moved enforcement point is still detected only by review.

**Direction:** Package-qualify anchors and add them to unanchored sections.

#### `internal/brand` combines link-time globals with a value API *(pass 19, Boundaries lens)*

**Smell:** Dual API over mutable global state.

**Signals:**

- `internal/brand` has the highest fan-in in the repository. *(2026-10-27: the dual shape is unchanged — `brand.go:11-50`; 54 test files assign brand globals)*
- Package-level variables are set at link time via `-ldflags -X` (`Makefile:27-38`) and remain writable at runtime.
- A parallel `Values` struct exposes the same fields through methods, so call sites choose between two idioms for the same data.
- Tests mutate `brand.EnvPrefix` directly rather than constructing a `Values`.

**Impact:** The most widely depended-upon package in the tree is mutable from anywhere. Test mutation of globals is order-dependent, and two API shapes mean white-labeling correctness has to be verified twice.

**Direction:** Pick one. Either keep the link-time globals and delete `Values`, or make the globals a private seed for a single exported `Values` accessor. This is a low-risk consolidation with a wide blast radius, so it is worth doing deliberately rather than opportunistically.

#### Provider support crosses the catalog boundary *(pass 22, Coupling lens)*

**Smell:** Incomplete abstraction / change amplification.

**Signals:** `providers.ActivationAssets` contains provider-named fields; `commands/init.go:193` expands Cursor into Claude, Codex, and Cursor-ACP dependencies, while `commands/setup.go:229` expands it into Claude and Codex only *(adjusted, health check 2026-10-27)*, and maps bash-policy support through provider-specific booleans; `agent/provider_audit.go` and `agent/provider_unavailable.go` separately encode Codex output signatures.

**Impact:** A provider can become catalog-resolvable and launchable without receiving complete initialization or operational-diagnostics support. Full onboarding requires coordinated edits across `providers`, `commands`, `embedded`, and `agent`, and omitted runtime signatures fail silently.

**Direction:** Keep genuinely behavioral failure parsers in code, but let the catalog own declarative dependencies and activation assets. Document one provider-extension checklist rather than claiming the catalog is a plug-in boundary.

#### Provider stop-state recovery has multiple owners *(pass 21, Duplication lens; pass 22, Coupling lens)*

**Smell:** Shotgun surgery / duplicated application policy.

**Signals:** The signal-file primitives moved to `internal/providersignal` (`quota.go`, `provider_unavailable.go`). `commands/resume.go:46-54` and `tui/commands.go:286-288` still each run `ClearExpiredQuotaSignals` and `ClearProviderUnavailableSignals` after resume, and format the outcomes separately. *(adjusted, health check 2026-10-27)*

**Impact:** Adding another provider stop reason or changing cleanup semantics requires coordinated edits across the runtime, CLI, TUI, and their duplicated tests. A caller using `ops.Resume` directly resumes state without performing the cleanup that the user-facing adapters treat as part of resume.

**Direction:** Move the signal-file primitive out of `agent` to a small stable owner and make one application operation return structured cleanup outcomes. Preserve category-specific alert text; do not generalize unrelated provider failure detection.

#### Worktree intelligence refresh is orchestration-by-copy *(pass 21, Duplication lens; pass 22, Coupling lens)*

**Smell:** Shotgun surgery.

**Signals:** Refresh is still sequenced by several owners: claim (`ops/claim_task_worktree.go:59` → `scip_indexing.go`, `stacklit_indexing.go`), a wrapper family in `ops/submit_review.go:117-130`, and lower-level package calls in `agent/worktree_check.go:172-188`. *(adjusted locations, health check 2026-10-27; a run `wont` item)* The paths expose different warning shapes and test seams.

**Impact:** A fourth repository-intelligence tool, ordering change, or warning-policy fix must be propagated across claim, create, recovery, and submission. The current order dependency — functional clusters after SCIP and Stacklit — is carried by repetition rather than a named policy.

**Direction:** Introduce one task-worktree refresh operation returning structured per-tool results, then let each caller choose presentation/logging. Keep submit-time versus provision-time activation explicit if their required tool sets differ.

#### Optional-tool boundary primitives have multiple owners *(pass 21, Duplication lens; pass 22, Coupling lens)*

**Smell:** Copy-paste divergence.

**Signals:** POSIX single-quote escaping exists in five packages; `outputSuffix` in five; bounded diagnostics in three; and SCIP/Semble repeat truthy parsing already represented by `internal/envgate`. The diagnostic implementations have already diverged: SCIP truncates to the byte limit without a marker, while Stacklit and functional clusters append `...(truncated)` after slicing to the limit.

**Impact:** Shell hardening and diagnostic-bound changes have a broad edit surface, and identically named limits do not guarantee identical output bounds.

**Direction:** Consolidate only the proven primitives — POSIX quoting, bounded diagnostics, and truthy parsing — in leaf packages with direct edge-case tests. Leave tool-specific planning and error messages local.

#### Current documentation retains removed or undriven interfaces *(pass 21, Duplication lens; pass 22, Coupling lens)*

**Smell:** Documentation and implementation divergence.

**Signals:** `specs/architecture/c4/c4.md` and its diagrams still model the removed `internal/mcp` subsystem; `overview.md` still calls the supervision document an MCP-responsibility guide; `supervision-model.md` assigns the shared implementation role to `commands` rather than `ops`; generated `CONFIGURATION.md` advertises `LIZA_LOG_LEVEL`, while the agent logger is fixed at `slog.LevelInfo`.

**Impact:** Current architecture and configuration guides direct readers toward components and controls that do not exist.

**Direction:** Regenerate or retire the MCP C4 views, update the overview label, and either implement log-level parsing or remove the environment variable from generated documentation. Historical ADR references should remain unchanged.

#### Repeated control-flow skeletons

**Smell:** Structural duplication.

**Signals:** Await operations share a poll-interval policy (`ops/await_policy.go`), but the verdict and resubmission loops were not re-compared. Pipeline-aware status predicates remain local to watch checks (`commands/watch_checks_*.go`). Worktree paths are now constructed through `paths` except two generated shell scripts in `pairingindex`. *(adjusted, health check 2026-10-27)*

**Impact:** Behavioral fixes can land in one path but miss siblings.

**Direction:** Consolidate shared policy only where one abstraction makes failure behavior clearer than the duplicated code.

#### Configuration and time-policy scatter

**Smell:** Hardcoded configuration.

**Signals:** Watch still has top-level timing constants (`commands/watch.go:18-23`), and timeouts and polling intervals live across packages, now as named constants (K7). The UTC (C5) and lease (C1) halves are resolved. *(adjusted, health check 2026-10-27)*

**Impact:** Operational tuning and temporal reasoning require code-wide knowledge.

**Direction:** Centralize values that operators need to tune; keep truly local implementation constants near their use.

#### Data contract loss and ambiguity

**Smell:** Leaky or unstable contract.

**Signals:** Sprint archival drops metrics other than `TasksDone` (registry: mitigated, open), and fan-out sets child priority to `1` (`ops/transition_children.go:347`). The `spec_ref` disagreement was closed in the registry on 2026-09-20. *(adjusted, health check 2026-10-27)*

**Impact:** Data may be accepted at one boundary and rejected later, or silently lose scheduling/observability information.

**Direction:** Define ownership and preservation rules before changing implementation.

#### Filesystem error and cleanup asymmetry

**Smell:** Inconsistent error semantics.

**Signals:** Some `os.Stat` presence checks distinguish only exists/missing, and some cleanup removal failures are suppressed. *(not re-verified 2026-10-27: the finding names no locations)*

**Impact:** Permission and I/O failures can look like absence, while secondary cleanup failures may be observable only indirectly.

**Direction:** Use tri-state existence handling at correctness boundaries and explicitly document best-effort cleanup boundaries.

#### Secret redaction has three strengths, chosen per call site *(Adversarial pass)*

**Smell:** Copy-paste divergence at a security boundary.

**Signals:** Three policies mask text before it is stored or shown:
- `secretmask.MaskText` replaces the values of environment variables whose names match a suffix list (`secretmask/masker.go:23-45`) and are at least 8 characters long. It is called at 25 `secretmask.New()` sites in 18 files, mostly for text persisted to state. One is integration test output (`ops/merge_failure.go:161`), stored as `task.IntegrationFailure`.
- `ops.acceptanceExecutionMaskWith` (`acceptance_execution.go:216-250`) adds `*_URL`/`*_DSN` values, DSN passwords and declared runtime-input secrets with their URL escapes. It serves acceptance execution and `run-live` only.
- `toolresult.newSanitizer` (`redact.go:64-104`) adds JSON-escaped values and pattern rules: bearer/basic values, credential headers, flags and assignments, URL userinfo, private keys and known token shapes.

**Impact:**
- Suppose integration test output prints a `DATABASE_URL` that carries a password. `run-live` masks it, but a merge failure persists it verbatim into `state.yaml`, because `_URL` is not a secret-key suffix and `secretmask` has no URL rule.
- The strength applied depends on which helper the author picked, and nothing checks that state, history, anomaly details or prompts received the strong one.
- Neither this review nor the registry had a secret-handling finding.

**Direction:**
- Name one redaction policy for persisted and agent-visible text, in a leaf package, with `secretmask` as its environment-value primitive rather than an alternative to it.
- Apply it at the persistence boundaries (task diagnostics, anomaly details, history), so that new fields inherit it.
- The streaming supervisor-log masker may stay value-only if that is documented.

#### State cache identity is mtime alone *(Adversarial pass)*

**Smell:** Leaky abstraction: a timestamp used as a version.

**Signals:**
- `db.ReadCached` (`blackboard.go:339-374`) reuses its parsed state while `state.yaml`'s mtime is unchanged.
- The TUI decides "same publication" with `os.SameFile`, mtime and size (`tui/commands.go:77-80`).
- `ops/review_reservation.go:84-95` re-reads uncached before acting, because "mtime caching can lag a write".
- Writers publish by atomic rename, so every publication is a new file, which is cheaply detectable from the `os.Stat` result `ReadCached` already has.
- On this host, 85% of back-to-back atomic publications shared an mtime (1.8).
- The registry records mtime keying as the [resolution of multi-process cache coherence](architectural-issues.md#cache-coherence-gap-in-multi-process-deployments).

**Impact:** A cross-process write that lands in the same timestamp tick as the read that filled the cache is invisible until the next write. `ReadCached` serves decisions as well as display:
- orchestrator wake verification, which can refuse (`agent/orchestrator_verify.go:35`);
- verdict and resubmission awaits (`ops/await_verdict.go:289`, `await_resubmission.go:331,364`);
- reviewer and merge strategy reads.

An await can stall to its budget on a verdict it missed. Each caller that noticed has added its own fresh-read workaround.

**Direction:** Key the cache on the publication (`os.SameFile` plus mtime and size, as the TUI does). Then drop the local workaround or keep it as defence in depth. Reopen or annotate the registry resolution.

#### Circuit-breaker pattern vocabulary is split across producer, consumer and spec *(Adversarial pass)*

**Smell:** Primitive obsession on persisted control-flow vocabulary.

**Signals:**
- Pattern names and severities are raw strings in `analysis/patterns.go` (`"retry_cluster"`, `"provider_audit_degradation"` at :401 and :435, `"ARCHITECTURE_FLAW"`). The model fields are `string` (`models/history.go:454-455,490-491`).
- `ops/analyze.go:22-24` re-declares `"provider_audit_degradation"` privately, with a comment that analysis "declares no constant for it", and branches on it (:116). Only `blocked_replacement_chain` has a models-owned constant.
- The code and the spec disagree:
  - `retry_cluster` triggers on three retry loops of which two share an error pattern (`patterns.go:275-296`), while the spec requires `count(similar(error_pattern)) >= 3` (`circuit-breaker.md:146-149`);
  - `workaround_pattern` falls back to grouping by `what` (`patterns.go:352-357`), which the spec does not mention;
  - `thresholds_test.go` pins neither case.

**Impact:** Renaming a pattern in `analysis` silently disables the checkpoint-continuation branch in `ops`, and persisted responses keyed on the old name stop matching. Operators reading the spec mis-predict when the breaker fires. This is a residue of the control-flow vocabulary work, which typed categories, cardinalities and claim types but not breaker patterns.

**Direction:**
- Give pattern identity and severity named types and constants in `models`, next to `BlockedReplacementChainPattern`.
- Resolve the `retry_cluster` and fallback disagreements as spec or code, which is the human's ruling.
- Add the missing threshold cases to `thresholds_test.go`.

#### Tool-result telemetry has no retention, and one bad record fails the report *(Adversarial pass)*

**Smell:** Unbounded operation.

**Signals:**
- `toolresult.Store.process` writes one `events/<random>.json` per tool call, plus one artifact per distinct externalized output (`store.go:181-237`).
- Nothing in the package or `commands/cleanup.go` prunes `tool-results/`, and `TECH_DEBT.md` does not mention it.
- `Store.Stats` (`store.go:387-435`) reads every event on each call and returns an error on the first unreadable or invalid file.
- Its sibling, `usage.Load` (`records.go:230-260`), windows its reads by time and counts malformed lines instead of failing.

**Impact:** Long-lived projects accumulate an event file per tool call, and `tool-result stats` slows linearly with them. One truncated event, for example from a crash mid-write, makes the whole report unavailable.

**Direction:**
- Pick one store shape for the two telemetry sources: time-partitioned append files with a tolerant reader, as `usage` has.
- State a retention rule.
- Treat this as runtime data in the user's project, separate from the assessment's repository-artifact retention item (3.5).

#### Terminal-archive format is implemented twice *(Adversarial pass)*

**Smell:** Parallel implementation across languages.

**Signals:**
- `skills/liza-logs/scripts/analyze-state.py:147-192` restores archived terminal tasks independently of `internal/db/terminal_archive.go`. It re-encodes the object path layout, `format_version == 2`, the four envelope fields, the stub field set and the terminal status set.
- Its tests (`test_analyze_state.py:121-170`) build their own archive objects. No fixture produced by Go is shared between the two suites.

**Impact:** A Go change to the envelope version or stub shape passes both suites and breaks the log-analysis skill on real archives.

**Direction:** Have the Python tests read an archive fixture generated by the Go encoder (oracle or `testdata`). Alternatively, have the skill call a `liza` query instead of decoding storage itself.

#### State migration publishes outside one lock *(Adversarial pass, smell-driven)*

**Smell:** Non-atomic read-modify-write on the shared state.

**Signals:**
- `ops.MigrateState` (`migrate_state.go:19-80`) reads with `bb.ReadRaw()` and publishes with `bb.Write(state)`, so it takes two lock acquisitions. `liza migrate` (`cmd_init.go:467-495`) does not check that agents are stopped.
- `Blackboard.Write` (`blackboard.go:632-646`) skips all three in-lock gates that `Modify` runs, and it publishes whatever `MutationSequence` the caller read. `Modify` states that "publication owns this counter". The comment on `Write` justifies skipping only the anomaly gate.
- The safety rests on documentation. `support-docs/SUPPORT.md:198-202` requires `stop` before the one migration case whose state stays writable, and names the lost write. The other recommended cases (`:193`, `:205`, `:1186`) are states that already refuse every write: whole-state hygiene rejects transcript payloads and oversized text, and global validation rejects the stranded lease. No agent can race there.

**Impact:**
- If an operator runs `migrate` while agents run, or a future migration targets a writable state, any claim, verdict or merge record published between the read and the write is silently reverted.
- `MutationSequence` returns to the read value, so later transactions reuse sequence numbers already issued. `ops/state_lock_holds.go` compares against them.
- Today's procedure is safe only because one paragraph says so. The command does not enforce it.

**Direction:** Refuse `migrate` unless the system is stopped. This is the precondition the SUPPORT procedure already relies on, and it makes the doc caveat unnecessary. Alternatively, run migration inside one lock acquisition through an unnormalized `Modify` variant, so that the legacy shapes stay detectable and the gates and the sequence apply.

#### Lock order is documented in fragments, and the task-worktree lock is unranked *(Adversarial pass, smell-driven)*

**Smell:** Implicit cross-process protocol.

**Signals:**
- The code constructs about 20 distinct flock identities: state, project lifecycle, agent lifecycle, work admission, task review, integration completion, integration mutation, task worktree, worktree setup, Git worktree, deferred-validation runner, submit hold, admission slot/queue/ticket, pending-verdict outbox, lifecycle metrics, usage, alerts, log, Semble, pairing-index refresh and repository exclude.
- Order is stated as partial chains. `INVARIANTS.md:192` gives agent lifecycle → work admission, and `:198` gives completion → mutation → blackboard read. `:294` gives project lifecycle → agent lifecycle → task review → integration completion → integration mutation → blackboard read. Fragments also appear in nine ADR and protocol files (0053, 0112, 0113, 0136, 0173, 0225, backfilled 0073, `replacement-transactions.md`, `validation-prerequisites.md`).
- The task-worktree lock (`claimTaskWorktreeLockPath`, `claim_task.go:177`) is constructed inline at 10 sites and appears in no chain. Submission nests it inside an admission slot and the project lifecycle lock (`submission_lifecycle.go:112-120`).
- `filelock` keeps no per-process record of held locks, so nothing checks the order at runtime.

**Impact:**
- An inversion does not hang the system, because every acquisition is bounded by `DefaultLockTimeout`. It appears instead as intermittent lock-timeout failures in the middle of an operation, which look like ordinary contention.
- A new caller has no single list to check its nesting against. Of the most frequently constructed lock, the documents say only "released before" at one site (`INVARIANTS.md:294`).

**Direction:** Give the lock identities one ranked table: in `INVARIANTS.md`, or as rank constants beside the lock names in Go. Wrap the task-worktree lock in a named helper, as the lifecycle and review locks are wrapped. A debug-build rank assertion on a per-process held-lock stack is optional.

#### State publication is less durable than the side records it references *(Adversarial pass, smell-driven)*

**Smell:** Durability policy chosen per call site.

**Signals:**
- 13 writers publish through a temporary file and a rename.
- Directory fsync is done for archive objects (`archiveobject/objects.go:147`, specified at `blackboard-schema.md:817-823`), the task archive (`ops/task_archive.go`) and the pending-verdict outbox, which syncs the directory and its parent (`pending_verdict.go:172-186`).
- `Blackboard.writeStateData` (`blackboard.go:404-444`) fsyncs the file and then renames it, without syncing the directory.
- The shared helper `atomicfile.Write`/`Copy` fsyncs neither. Its users are in-flight submit markers and copied index artifacts.
- Liveness records skip fsync by decision (ADR-0177).
- The archive spec calls Windows ordering best-effort "as for state publication", but nothing states what state publication guarantees on POSIX.

**Impact:**
- A process crash is safe, because the page cache survives it.
- After an OS crash or power loss, the latest state renames can be lost while the archive objects and verdict envelopes they produced survive. The plausible result is a rolled-back transition, for example an integration-branch merge that state records as unmerged.
- Whether recovery reconciles that case is unverified.

**Direction:** State the guarantee. Either add the directory fsync to `writeStateData`, measuring the cost first because it runs under the state lock on every write, or record that state publication is atomic but not durable and why. Give `atomicfile` an explicit durability option before more sites copy the pattern.

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
| Large multi-responsibility modules; `proceed.go`, `RunSupervisor`, other large modules | Resolved by the consolidation run (health check 2026-10-27): no production file >800 lines, three functions >100, all ratchets empty. |
| Duplicate initialization implementations | Resolved: C4 deleted `ops.InitProject`; init Git calls go through `gitenv`. |
| State mutation bypasses `ops` | Resolved: C6 confines blackboard mutation to `ops` and `db`, enforced by `TestC6MutationGuard`. |
| No mechanical import-direction check | Resolved: K4 check in `make lint` with a three-entry rationale-carrying allowlist. |
| `prompts → ops` and `jsonout → ops` dependencies | Resolved: both crossings removed (A8, A7). |
| Agent lease renewal ignores configured duration | Resolved: C1; registry closed at A9. |
| Canonical provider identity is dropped | Resolved: C2/C3 (`agentlaunch.ProviderKeys` feeds diversity and stop signals); registry closed at A9. |
| Merge timestamps in local time | Resolved: C5 persists UTC. |
| `spec_ref` contract conflict | Resolved in the registry on 2026-09-20. |
| Stale `specs/plans` ignore in pre-commit | Stale: `specs/plans/` exists again (consolidation plans), so the jscpd ignore is live. |
| "Three packages have zero test files" (`alerts`, `projectdetect`, `taskkind`) | Adjusted at pass 20: accurate as stated, but not a coverage gap. `alerts` is 89.7% and `projectdetect` 92.9% from their callers; `taskkind` has no executable statements. |
| Untested `statevalidate` and embedded entry points | Stale at pass 20: `statevalidate` 89.6%, `PlanGlobalFiles` 90.9%, `WritePipelineConfig` 100%. The finding was an artifact of same-package measurement. |

**Follow-through gap:** The [non-reproducible coverage basis](architectural-issues.md#coverage-basis-is-not-reproducible-by-repository-tooling) has remained High and verified through passes 20, 21, and 22. Reverification shows continued accuracy, not progress; the canonical registry owner makes later closure explicit.

The two headings below are kept so references from the consolidation plan (`specs/arch-plan/…/20261023-162147-arm-1.md`) still resolve.

#### State mutation authority is distributed, not owned *(pass 19, Boundaries lens)*

Resolved: C6 confines blackboard mutation to `ops` and `db` (see the table above). *(health check 2026-10-27)*

#### Canonical provider identity is lost before policy enforcement *(Adversarial pass)*

Resolved: C2/C3 (see the table above). *(health check 2026-10-27)*

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
| Command/Service | `internal/commands` → `internal/ops` | Separates user-facing parsing from mutations. The layer is optional by policy: `cmd/liza` is the composition root, with 36 production files importing `ops` and 21 importing `commands`. *(adjusted, health check 2026-10-27)* |
| Transactional outbox analogue | task history in blackboard state | Records lifecycle changes with the authoritative state update. |
| Architecture fitness functions | `internal/testguard/archguard` | Size ratchet (S1), import direction (K4) and mutation boundary (C6) as tests run by `make lint`. *(health check 2026-10-27)* |
| Characterization oracle | `internal/oracle` | Frozen black-box goldens over CLI, state, prompt and artifact surfaces with chained provenance (ADR-0219). *(health check 2026-10-27)* |

### 2.6 Test Coverage

The [2026-10-27 code quality assessment](code_quality_assessment.md#repository-metrics-dashboard) counts 351,367 Go test lines (21,315 of them the oracle harness) against 149,233 production lines, a 2.35:1 ratio, with 6,878 test functions. *(health check 2026-10-27)* The 2026-07-24 assessment counted all tracked Go files at 150,683 test lines and 65,509 production lines, a 2.30:1 ratio. The previously quoted 150,414/65,061 total used the narrower `cmd/` + `internal/` scope: the exact difference is `plugin/acp` (269 test and 448 production lines). This dated ratio indicates substantial test investment but is not a statement-coverage percentage.

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
- The tracked test tree contains 16 `time.Sleep(` occurrences outside `internal/testguard/`, at the guard's ceiling. The ceiling rose from 11 to 16, each step ADR-linked. *(remeasured 2026-10-27)*
- `t.Parallel()` appears 2,107 times across 275 tracked Go test files (524 across 51 at 2026-09-06). *(remeasured 2026-10-27)*
- `internal/embedded/opencode-tools/exec.ts` is shipped to users' `.opencode/tools/` directories, and no `package.json`, `tsconfig.json`, type-check, or lint step exists. `opencode_exec_runtime_test.go` now executes it, but it skips without Bun, and no CI workflow installs Bun. *(adjusted, health check 2026-10-27)*
- Integration coverage exists for multi-component task, Git, persistence, and execution flows; no new behavior is being added by this review.

## Phase 3: Recommendations

| Priority | Status | Issue | Rationale | Direction |
|----------|--------|-------|-----------|-----------|
| **High** | ~ adjusted | [CI has no reproducible coverage artifact](architectural-issues.md#coverage-basis-is-not-reproducible-by-repository-tooling) | `make coverage` now uses `-coverpkg=./...`, but it is interactive and self-deleting, and CI publishes no profile after the Codecov step was removed. | Give CI a retained `-coverpkg` profile before any coverage-driven work. *(pass 20 Coverage finding; adjusted, health check 2026-10-27)* |
| **Medium** | ~ adjusted | Human-readable CLI path is untested | 9 of 10 named `*Command` functions have no direct test reference; oracle human-output goldens (961 files) now cover rendered output for refactored commands black-box. | Test the remaining render paths directly, or rely on and extend the oracle explicitly. *(pass 20, Coverage lens; adjusted, health check 2026-10-27)* |
| **Medium** | ~ adjusted | [`exec.ts` runtime test skips in CI](architectural-issues.md#ci-does-not-enforce-cross-language-quality-gates) | A runtime test now exists but skips without Bun, and no workflow installs Bun; there is still no type-check. | Install Bun in one CI job, or state that the file is vendored and unverified. *(pass 20, Coverage lens; adjusted, health check 2026-10-27)* |
| **Medium** | ~ adjusted | Provider support crosses the catalog boundary | Setup dependencies, activation assets, bash-policy integration and runtime failure detection still need provider-specific edits; the Cursor expansion now diverges between `init.go:193` and `setup.go:229`. | Move declarative dependencies/assets into the catalog and document the remaining behavioral extension points. *(pass 22, Coupling lens; adjusted, health check 2026-10-27)* |
| **Medium** | ~ adjusted | Provider stop-signal recovery has multiple owners | Primitives now live in `providersignal`; CLI and TUI resume still each orchestrate and render cleanup. | Give one operation ownership of cleanup and its structured outcome; keep CLI/TUI as adapters. *(pass 21, Duplication lens; adjusted, health check 2026-10-27)* |
| **Medium** | ✓ verified | Worktree intelligence refresh has multiple owners | Claim, review submission and the reviewer worktree check still sequence refresh separately (`claim_task_worktree.go:59`, `submit_review.go:117-130`, `agent/worktree_check.go:172-188`). | Introduce one refresh coordinator with explicit trigger and failure-policy inputs; keep lifecycle decisions at callers. *(pass 21, Duplication lens; reverified 2026-10-27)* |
| **Medium** | ✓ verified | Review-submission cleanliness contract gap | `INVARIANTS.md` promises a clean worktree at a boundary that does not enforce it. | Decide whether to enforce cleanliness in `SubmitForReview` or narrow the invariant. |
| **Medium** | ✓ verified | Verdict-time provider-diversity contract gap | The documented enforcement point and current quorum logic disagree. | Define whether diversity is required, preferred, or claim-time-only, then align contract and code. |
| **Medium** | ~ adjusted | Raw Git call bypasses `gitenv` | One raw call remains, `cmd/liza/launch_resolve.go:127` (`liza launch --cwd`); the five initialization sites were removed by C4. | Route it through `gitenv`, and consider a lint check rejecting `exec.Command("git"` outside `gitenv`. *(pass 18, Complexity lens; adjusted, health check 2026-10-27)* |
| **Medium** | ~ adjusted | Custom roles render empty prompt blocks | Nothing validates role names, and the template chains, now 521 and 404 lines, still have no role fallback. | Add a fallback block or config-load-time role coverage validation, then correct the `supervisor_lifetime.go:93-95` comment. *(pass 18, Complexity lens; adjusted, health check 2026-10-27)* |
| **Medium** | ~ adjusted | Silent index-discovery failure | The three helpers, now in `agent/prompt_intelligence.go:96,110,123`, still swallow errors. | Log the failure; keep the graceful degradation. *(pass 18, Complexity lens; adjusted location, health check 2026-10-27)* |
| **Medium** | ~ adjusted | Semantic validation is off the write path | `internal/db` still does not import `statevalidate`. 32 `ops` files now do, but `ValidateCandidate` runs at 24 sites against 60 `.Modify(` sites, so coverage remains per-operation. | Move the cheap invariant subset inside `Blackboard.Modify`. *(pass 19, Boundaries lens; adjusted, pass 23)* |
| **Medium** | + new | [Ops concern-family ownership is nominal](architectural-issues.md#ops-concern-family-ownership-is-nominal) | 47 of 241 `ops` files are cited by no family ADR, contrary to `overview.md`. 48% of cross-file call sites leave their family or reach an unassigned file, the kernel calls into A4–A6 66 times, and `pipeline_ops.go` (A4, "sprint coordination") is depended on by 74 files. The ADRs' "same separation" argument against sub-packages is unchecked. | Add an intra-`ops` family guard with an allowlist and assign the 47 files, or state that the groups are review aids only. Keep the one-package decision either way. *(pass 23, Complexity lens)* |
| **Medium** | + new | [TDD gate hardcodes target-project test conventions](architectural-issues.md#tdd-gate-hardcodes-target-project-test-conventions) | `ops/test_detection.go` recognises nine languages and has no configuration. `enforceTDD` refuses code submissions without a match, and language support has grown by fix commits. A violation of G1.1. | Add a project-configurable pattern list that defaults to today's table, and derive the display patterns from it. *(pass 23, Complexity lens)* |
| **Medium** | ~ adjusted | `--json` error codes derive from message prose | Typed `errors.As` checks now run first, but the substring rule table (`classify.go:10-26`) still classifies the rest. | Type the errors agents branch on; keep prose rules as fallback only. *(pass 19, Boundaries lens; adjusted, health check 2026-10-27)* |
| **Medium** | ✓ verified | Sprint-summary metric loss | Archival preserves only `TasksDone`. | Specify which metrics must survive a sprint boundary before changing the model. |
| **Medium** | + new | [Secret redaction has three strengths, chosen per call site](architectural-issues.md#secret-redaction-strength-depends-on-call-site) | Persisted state text gets env-value masking only (`secretmask`, 25 sites), while `run-live` adds URL/DSN credentials and the tool-result store adds pattern rules. A credential-bearing `DATABASE_URL` in failing integration output reaches `state.yaml` unmasked (`merge_failure.go:161`). | One named policy for persisted and agent-visible text, applied at the persistence boundaries. *(Adversarial pass)* |
| **Medium** | + new | [State cache identity is mtime alone](architectural-issues.md#state-cache-treats-mtime-as-publication-identity) | `ReadCached` (`blackboard.go:339-374`) ignores a publication in the same mtime tick; it feeds orchestrator verification and verdict awaits. The TUI already keys on `SameFile`+mtime+size, and `review_reservation.go:84` works around the lag locally. | Key the cache on publication identity; annotate the registry's cache-coherence resolution. *(Adversarial pass)* |
| **Medium** | + new | [Circuit-breaker pattern vocabulary is split](architectural-issues.md#control-flow-vocabulary-bypasses-domain-ownership) | Pattern names and severities are raw strings; `ops/analyze.go:24` re-declares an `analysis` literal and branches on it. `retry_cluster` and `workaround_pattern` disagree with `circuit-breaker.md:146-164`. | Typed pattern/severity constants in `models`; a human ruling on spec versus code; threshold tests for both cases. *(Adversarial pass)* |
| **Low** | ✓ verified | System-mode and task-status invariant drift | Sections 3 and 12 no longer describe current ownership or transitions. | Update the invariant text after intended behavior is confirmed. |
| **Low** | ~ adjusted | `commands → agent` and `tui → agent` dependencies | Both remain as rationale-carrying K4 allowlist entries for read-only projections. | Keep allowlisted; revisit if a third consumer needs the same projection. *(adjusted, health check 2026-10-27)* |
| **Low** | ~ adjusted | Await-loop structural duplication | A shared poll-interval policy exists (`ops/await_policy.go`); loop shapes were not re-compared. | Consider a shared wait primitive if it preserves explicit failure semantics. *(adjusted, health check 2026-10-27)* |
| **Low** | ✓ verified | `ApplyYAMLTimeouts` concrete type switch | Still at `strategy_doer.go:44`, called from `supervisor_lifetime.go:125`. | Put the capability on the strategy boundary or isolate projection in a dedicated adapter. *(reverified 2026-10-27)* |
| **Low** | ~ adjusted | Pipeline-aware status triplication | Resolver-parameterized predicates now sit in `commands/watch_checks_*.go`; not re-compared for duplication. | Parameterize only if it improves readability and type safety. *(adjusted, health check 2026-10-27)* |
| **Low** | ~ adjusted | Worktree path helper adoption | `paths` owns construction; only `pairingindex` shell-script generation spells `.worktrees` inline. | Accept for generated scripts unless they need runtime-configurable roots. *(adjusted, health check 2026-10-27)* |
| **Low** | ✓ verified | Dual logging paths | 151 direct `fmt.Fprint*(os.Stderr` sites in `internal/` coexist with the project logger. | Document purpose or route warnings through one structured boundary. *(reverified 2026-10-27)* |
| **Low** | ✓ verified | Identity validation duplication | `agent/registration.go:18-48` re-implements ID-shape checks that `identity.ValidateFormat` owns, and still skips role-name validation. | Reuse the identity boundary; decide whether role-name validation belongs there or at config load. *(reverified 2026-10-27)* |
| **Low** | ~ adjusted | Prompt template blocks at 521 and 404 lines | Both per-role chains grew; templates are outside the S1 size guard. | Split per role or per phase when the next role is added. *(adjusted, health check 2026-10-27)* |
| **Low** | ~ adjusted | `ops` write-path transaction scripts | No `ops` function now exceeds 100 lines, and lifecycle mutation goes through shared kernel helpers (`ops/lifecycle_authority.go`; ADR-0222). Generic lifecycle engines were rejected by ADR. | Accept unless the kernel helpers are bypassed. *(adjusted, health check 2026-10-27)* |
| **Low** | ~ adjusted | TUI panel render duplication | Panels now render from separate column types (`agentColumn`, `taskColumn`); the shared-renderer suggestion was not adopted. | Share one column renderer only if a third panel appears. *(adjusted, health check 2026-10-27)* |
| **Low** | ✓ verified | Index adapter triplication | Three index-ref types remain (`prompts/role_context.go:200-212`), with adapters in `agent/prompt_intelligence.go`. | Collapse to one type and one adapter when a fourth index tool appears. *(reverified 2026-10-27)* |
| **Low** | ✓ verified | Optional-tool boundary primitives have multiple owners | Shell quoting is defined in six packages and `outputSuffix` in five; only Stacklit and functional clusters append a truncation marker. | Consolidate only the primitives whose semantics can be named precisely. *(pass 21, Duplication lens; reverified 2026-10-27)* |
| **Low** | ✓ verified | `plugin/acp` has no production consumer | Still imported only by two `internal/agent` performance tests (plus the archguard check). | Confirm intended status; document it or move it under test scope. *(reverified 2026-10-27)* |
| **Low** | ✓ verified | Undocumented tracked directories | `plans/`, `plugin/`, and `scripts/` are still absent from `REPOSITORY.md`. | Add them or explain the omission. *(reverified 2026-10-27)* |
| **Low** | ✓ verified | Current docs retain removed or undriven interfaces | C4 still models MCP, `overview.md:465` still labels supervision as "supervisor vs MCP tool responsibility", `supervision-model.md:182` assigns shared implementation to `commands`, and `CONFIGURATION.md:1764` advertises `LOG_LEVEL` with no Go consumer. | Correct current-state docs; remove undriven configuration surfaces. *(reverified 2026-10-27)* |
| **Low** | ~ adjusted | `cmd/liza` bypasses `internal/commands` | 36 production `cmd/liza` files import `ops` and 21 import `commands`; the composition-root rule now permits this. | Treat `commands` as optional by policy; collapse thin adapters opportunistically. *(adjusted, health check 2026-10-27)* |
| **Low** | ~ adjusted | JSON-envelope guards repeated in `cmd/liza` | `ErrAlreadyWritten` is referenced in 37 files and `log.SetOutput(io.Discard)` appears 43 times; no wrapper was extracted. | Extract one `withJSONEnvelope` wrapper. *(pass 19, Boundaries lens; adjusted, health check 2026-10-27)* |
| **Low** | ✓ verified | `INVARIANTS.md` enforcement anchors are sparse and ambiguous | 7 anchors across 16 sections; bare filenames remain. | Package-qualify anchors and add them to the unanchored sections. *(reverified 2026-10-27)* |
| **Low** | ✓ verified | `internal/brand` dual API over mutable globals | Globals and `Values` both remain (`brand.go:11-50`); 54 test files assign brand globals. | Pick one shape. *(reverified 2026-10-27)* |
| **Low** | ~ adjusted | Two packages have zero test files | `internal/alerts` now has two test files; `projectdetect` (exercised by callers) and `taskkind` (no statements) still have none. | Leave as is. *(adjusted, health check 2026-10-27)* |
| **Low** | ~ adjusted | [No coverage gate](architectural-issues.md#coverage-basis-is-not-reproducible-by-repository-tooling) | The Codecov step was removed; coverage is neither published nor enforced. | Decide whether coverage is a gate or an observation once CI produces a profile. *(adjusted, health check 2026-10-27)* |
| **Low** | ~ adjusted | [118 zero-coverage functions, 59 exported](coverage-summary-2026-07-25.md#zero-coverage-functions) | This is a retained pass-20 full-profile measurement, not a claim derived from the current ignored `coverage.out`; the structural concentration in CLI and orchestration adapters remains relevant. | Triage the exported half after the default coverage command is corrected. *(pass 20, Coverage lens)* |
| **Low** | ~ adjusted | Exported function with no caller | `commands.UnblockTaskCommand` still has no caller; `UnmetDependencies` gained one (`ops/state_lock_holds.go:81`). | Delete `UnblockTaskCommand`. *(adjusted, health check 2026-10-27)* |
| **Low** | ~ adjusted | `internal/interactive` at 35.8% | The pass-20 full-profile measurement remains the latest complete figure; the small package is on first-run initialization, where failure is user-visible and recovery is manual. | Cover the prompt/response paths when initialization next changes. *(pass 20, Coverage lens)* |
| **Low** | ~ adjusted | Remaining serial test packages | `t.Parallel()` now appears 2,107 times across 275 test files; adoption has broadened substantially. | Continue package-local audits where shared state remains. *(remeasured 2026-10-27)* |
| **Low** | ✓ verified | `.gitignore` embedded allowlist drift | `console.sh` and `support.md` remain allowlisted (`.gitignore:36,39`) but untracked; tracked `claudeignore` is not allowlisted. | Prune and add. *(reverified 2026-10-27)* |
| **Low** | ~ adjusted | Pure domain predicates trapped in `ops` | The `prompts → ops` symptom is gone (prompts receive resolved data); predicate placement inside `ops` was not re-audited. | Relocate pure predicates opportunistically. *(adjusted, health check 2026-10-27)* |
| **Low** | ✓ verified | Inconsistent operation inputs | `AddTasks(statePath, logPath, …)` (`ops/add_tasks_delivery.go:39`) still takes paths. | Standardize when the next signature change occurs. *(reverified 2026-10-27)* |
| **Low** | ✓ verified | Filesystem existence checks | Some sites collapse permission/I/O errors into presence logic. | Use tri-state checks at correctness boundaries. |
| **Low** | ✓ verified | Watch thresholds and timeout scatter | Named watch constants remain at `commands/watch.go:18-23`; the run added no configuration by design (K7). | Externalize only values with a demonstrated tuning need. *(reverified 2026-10-27)* |
| **Low** | ✓ verified | Fan-out priority flattening | Generated children still receive `Priority: 1` (`ops/transition_children.go:347`). | Specify priority inheritance for generated tasks. *(reverified 2026-10-27)* |
| **Low** | ~ adjusted | Direct validation/embedded entry-point test visibility | Pass-20 full-package coverage measured the validation internals as exercised; the remaining concern is direct visibility of composition behavior, not a broad validation coverage gap. | Add a focused composition test only when that path next changes. |
| **Low** | + new | Plan-amendment disposition is implicit | Correction disposition is membership in three lists, so the validator needs cyclomatic 70 and a four-term exclusivity check; the feature spans 27 files in six packages. | Introduce a per-correction disposition type at the next schema change to `PlanAmendment`. *(pass 23, Complexity lens)* |
| **Low** | + new | Positional identity clumps in `ops` signatures | 102 of the 141 functions with ≥ 7 parameters are in `ops`, and `submitVerdict` takes eight `string`s. Transposed identities compile. | Introduce small identity structs when these signatures are next touched; no sweep. *(pass 23, Complexity lens)* |
| **Low** | + new | Mutable test hooks in production `ops` | 37 package-level `*TestHook` variables enable interleaving tests of lock and CAS paths but are process-global. | Accept while they serve concurrency tests; prefer injected seams for new ones. *(pass 23, Complexity lens)* |
| **Low** | + new | Tool-result telemetry has no retention | One event file per tool call, never pruned; `Store.Stats` reads them all and fails on one invalid file, unlike the tolerant, time-windowed `usage.Load`. | One telemetry store shape with a tolerant reader and a stated retention rule. *(Adversarial pass)* |
| **Low** | + new | Terminal-archive format implemented twice | `analyze-state.py:147-192` re-encodes the Go archive layout, version and stub shape; neither suite uses the other's fixtures. | Feed the Python tests a Go-generated archive fixture, or query through the CLI. *(Adversarial pass)* |
| **Low** | + new | State migration publishes outside one lock | `MigrateState` reads and writes in separate lock acquisitions, and `liza migrate` does not check that agents are stopped. `Write` skips `Modify`'s three gates and its sequence ownership. Today only `SUPPORT.md:198-202` keeps the one writable-state case safe. | Refuse `migrate` unless stopped; or migrate inside one lock through an unnormalized `Modify` variant. *(Adversarial pass, smell-driven)* |
| **Low** | + new | Lock order is documented in fragments | About 20 flock identities. The order is spread over three `INVARIANTS.md` chains and nine ADR/protocol files, and the task-worktree lock (10 inline sites) is in no chain. Timeouts turn an inversion into intermittent failures, not hangs. | One ranked table of lock identities; a named task-worktree lock helper. *(Adversarial pass, smell-driven)* |
| **Low** | + new | State publication durability is unspecified | Archive objects and verdict envelopes fsync their directories, but `state.yaml` does not, and the shared `atomicfile` helper fsyncs nothing. After power loss, state can roll back past the side records it references. | State the guarantee: measure and add the directory fsync, or document atomic-but-not-durable. *(Adversarial pass, smell-driven)* |
| **None** | ✓ verified | Pipeline resolver loaded per operation | The current call pattern keeps operations self-contained; overhead is negligible. | Accept. |
| **None** | ✓ verified | Explicit operation input guards | Repeated empty-ID checks are idiomatic and readable. | Do not abstract. |
| **None** | ✓ verified | Explicit history appends | The occurrences carry different event data and are clearer locally. | Do not abstract. |
| **None** | ✓ verified | Small `statevalidate` classifier repetition | Current helpers already share `containsStatus`; further abstraction has little payoff. | Accept. |
| **None** | ✓ verified | Global project logger | Appropriate for a CLI process. | Accept unless concurrent in-process consumers emerge. |

## Summary

Liza's architecture remains appropriate for a local, stack-agnostic multi-agent orchestrator. It keeps inspectable file-backed state, process-safe mutation, isolated Git worktrees, declarative pipelines and data-driven provider launch.

The architecture-consolidation run turned the earlier structural risks into tested properties:
- deep functions are gone, and the run cut branching as well as length (2.2);
- the layer rule and the mutation boundary fail the build when broken;
- canonical provider identity reaches policy.

What remains has one shape: boundaries that are asserted but not checked.
- **Inside `ops`.** A third of production code is owned by concern families that only documents define. A fifth of its files belong to none, and half of its internal calls cross families (pass 23).
- **Contracts and enforcement.** Gaps remain between `INVARIANTS.md` and the enforcement points, and semantic validation is opt-in per operation rather than at the write boundary. The write boundary's own protocol is also implicit: migration publishes outside one lock, lock order has no single owner, and state durability is unstated (Adversarial pass, smell-driven).
- **Stack-agnosticism.** The promise stops at the TDD gate's hardcoded test-file conventions (pass 23).
- **Secret handling and state freshness.** Redaction strength depends on which helper a call site picked, and the shared state cache treats mtime as a version (Adversarial pass).
- **Evidence and policy without closure owners.** CI has no coverage artifact, provider extension still crosses packages, and stop-signal cleanup and refresh sequencing are duplicated.

Each is an incremental ownership correction, mostly a guard or a configuration field, not a rewrite.

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
| Architecture guards and oracle | `internal/testguard/archguard/`, `internal/oracle/` |
| Launch resolution and provider signals | `internal/agentlaunch/`, `internal/providersignal/` |
| Tool results and usage | `internal/toolresult/`, `internal/toolresultacp/`, `internal/usage/` |
| Payload and reference contracts | `internal/payloadschema/`, `internal/referencecontract/` |
| Consolidation run records | `specs/architecture/consolidation-run/` |
| Architecture decisions | `specs/architecture/ADR/` |
| Persistent architecture issues | `specs/architecture/architectural-issues.md` |
