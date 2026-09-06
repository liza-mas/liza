# Code Quality Assessment and Refactoring Roadmap

* Date: 2026-09-06 (commit `73a50ca8`)
* Previous: 2026-07-24 (commit `ffe89080`) — 79 commits since
* Author: Codex GPT 6 Astra
* Mode: Reassessment

## Document Role

This is a dated measurement and prioritization snapshot, not a certification of runtime correctness. [The architectural issue registry](architectural-issues.md#open-issues-summary) remains the sole lifecycle authority. Its historical LOC-based anchors are deliberately preserved below. All current observations and dispositions in this report are from the **2026-09-06 reassessment**; the registry itself is unchanged.

Based on: tracked source at the assessed commit, Go AST function spans, manifests, workflow and Makefile recipes, targeted source/test inspection, and the registry. Source inspection was shared with two read-only assessment agents. No application test suite, coverage run, vulnerability audit, or external latest-version check was performed for this documentation update. Ratings describe observed maintainability and regression-protection mechanisms, not measured branch coverage or a verified green build.

## Repository Metrics Dashboard

LOC means physical lines, including comments and blanks. Counts use tracked files, exclude generated embedded corpus copies, and include all platform-specific Go files regardless of the host build selection. Go tests are every `*_test.go`; non-test files under `internal/testhelpers/` are reported separately from production. Python tests use `test_*.py` / test-directory classification. Function counts count declarations, not table cases or executed tests.

The July Go and documentation baselines below were recomputed from `ffe89080` using these definitions. The previous report counted 1,027 lines of test helpers as production and used broader or inconsistent documentation groupings. Those differences are measurement corrections, not code removal.

| Metric | July baseline | Current | Change |
|--------|--------------:|--------:|-------:|
| Go production LOC / files | 64,482 / 277 | 76,738 / 312 | +12,256 (+19%), +35 files |
| Go test LOC / files | 150,683 / 246 | 188,254 / 308 | +37,571 (+25%), +62 files |
| Go test-support LOC / files | 1,027 / 7 | 1,610 / 14 | +583 / +7 |
| Go test-to-production ratio | 2.34:1 | 2.45:1 | +0.11 |
| Go test functions | 2,998 | 3,452 | +454 |
| Go benchmark / fuzz functions | 6 / 0 | 6 / 0 | Unchanged |
| Python production LOC / files | 5,114 / 8 | 5,438 / 8 | +324 |
| Python test LOC / files | 2,196 / 6 | 3,669 / 7 | +1,473 |
| Python test ratio / functions | 0.43:1 / 65 | 0.67:1 / 99 | Improved |
| TypeScript LOC / files | 132 / 1 | 132 / 1 | Unchanged |
| Shell LOC / files | 1,822 / 10 | 1,909 / 10 | +87 |
| Production Go files >500 LOC | 35 | 43 | +8 |
| Production Go functions >150 LOC | 21 reported | 24 measured | +3 |
| Specifications, including archive | 219 / 26,696 LOC | 230 / 28,604 LOC | +11 files |
| Numbered ADR files directly under specs/architecture/ADR/ | 96 | 104 | +8 |
| Markdown under docs/ | 39 / 7,690 LOC | 39 / 7,772 LOC | +82 LOC |
| Markdown under support-docs/ | 9 / 3,904 LOC | 9 / 4,658 LOC | +754 LOC |
| Markdown under contracts/ | 11 / 5,404 LOC | 11 / 5,483 LOC | +79 LOC |
| Skills / lesson Markdown files | 28 / 7 | 29 / 10 | +1 / +3 |
| Direct Go dependencies | 13 | 14 | +1 |
| Python runtime / development dependencies | 3 / 5 | 3 / 5 | Unchanged |
| Configured pre-commit hook entries | 22 | 22 | Unchanged |

Specification totals exclude this assessment. Outside `specs/_archive/`, there are 228 specification Markdown files and 28,081 LOC. The ADR row counts only numbered Markdown files directly under `specs/architecture/ADR/`; it excludes 40 tracked numbered files under `ADR/adr-backfill-clusters/` (144 recursively). ADR count is a file inventory, not a claim that every historical decision is still active. Go test volume is evidence of investment, not proof that all important paths are exercised.

### Largest Production Go Files

| LOC | File |
|----:|------|
| 1,635 | `internal/embedded/embedded.go` |
| 1,567 | `internal/scipsearch/scipsearch.go` |
| 1,563 | `internal/ops/proceed.go` |
| 1,495 | `cmd/liza/cmd_task.go` |
| 1,418 | `internal/commands/init.go` |
| 1,407 | `internal/commands/watch.go` |
| 1,323 | `internal/updater/updater.go` |
| 1,232 | `internal/agent/prompt.go` |
| 1,159 | `cmd/liza/cmd_launch.go` |
| 1,131 | `internal/agent/supervisor.go` |
| 1,077 | `internal/pairingindex/hooks.go` |
| 1,037 | `internal/ops/integration_progress.go` |
| 974 | `internal/pipeline/resolver.go` |
| 956 | `internal/ops/wt_merge.go` |
| 922 | `internal/ops/claim_task.go` |
| 904 | `internal/commands/status.go` |
| 892 | `internal/models/task.go` |
| 867 | `internal/tui/view.go` |
| 848 | `internal/ops/await_verdict.go` |
| 802 | `internal/commands/inspect_tasks.go` |

Eighty-two production Go files exceed 300 LOC; 43 exceed 500. These are investigation thresholds, not automatic god-module findings. Declarative command registration and cohesive evaluators warrant different remedies from files combining unrelated schemas and lifecycle responsibilities.

### Longest Production Go Functions

Measured with Go's `go/parser` and `go/token`: declaration start through closing brace, inclusive, excluding preceding comments and nested function literals as separate entries. Twenty-four declarations exceed 150 lines.

| LOC | Function | Location |
|----:|----------|----------|
| 421 | `InitCommandWithConfig` | `internal/commands/init.go:857` |
| 417 | `RunSupervisor` | `internal/agent/supervisor.go:664` |
| 382 | `submitVerdict` | `internal/ops/submit_verdict.go:117` |
| 358 | `mergeWorktree` | `internal/ops/wt_merge.go:599` |
| 318 | `submitForReview` | `internal/ops/submit_review.go:54` |
| 265 | `completeClaimTaskAfterValidation` | `internal/ops/claim_task.go:317` |
| 227 | `InitProject` | `internal/ops/init_project.go:50` |
| 218 | `ExecuteAvailableTransitions` | `internal/ops/proceed.go:1028` |
| 204 | `renderTaskPanel` | `internal/tui/view.go:405` |
| 195 | `Replan` | `internal/ops/replan.go:35` |

The earlier report placed `InitProject` in the commands package; the measured implementation is in ops. Several lifecycle entry points now delegate to unexported implementations, whose names are used here.

### Dependencies and Code Hygiene

[go.mod](../../go.mod) declares 14 direct and 26 indirect requirements, plus a coverage-conversion tool directive. [pyproject.toml](../../pyproject.toml) declares three runtime and five development dependencies. `go list -m all` successfully resolved the selected module graph. The counts remain modest for this feature surface; they are not a dependency-security assessment.

The pinned `x/mod` v0.22.0 and `go-runewidth` v0.0.16 remain unchanged. The previous report's upstream-version comparisons were not refreshed and are not carried forward as current evidence.

Tracked production scans found no Go/Python TODO, FIXME, or HACK markers, no Go `nolint`/`nosec` suppression directives, and no empty `interface{}` spelling. Three Python `type: ignore` annotations remain around platform/typing boundaries in adversarial-pairing. Four `os.Exit` calls are confined to command entry points.

There are now seven production Go `panic()` calls: three flag-registration guards in `cmd/liza/cmd_agent.go`, one completion invariant in `completion_values.go`, two embedded-catalog guards in `internal/providers/catalog.go`, and one deliberate re-panic in `cmd/liza/supervisor_logs.go`. The count alone does not establish a defect; the re-panic is distinct from configuration invariants. Raw claim/category/cardinality strings remain a maintainability concern described below.

## Executive Summary

The orchestrator retains substantial test investment, explicit domain and persistence boundaries, and a large design record. Since July, production Go grew 19% while test LOC grew 25%. Python log analysis gained CLI-level regression tests and CI execution; native Windows gained a dedicated Go CI job. The supervisor loop and TUI update code also became smaller.

Those improvements have not removed the main maintenance constraints. Large production Go files rose from 35 to 43, initialization and artifact installation remain concentrated, and prompt assembly grew substantially. CI still enforces only part of the local quality policy. The next investment should combine completion of existing CI coverage with focused ownership fixes and decomposition, rather than a blanket file-splitting campaign.

### Key Strengths

- **Regression-protection investment:** 188,254 Go test LOC and 3,452 test declarations; Python test LOC grew 67%. Sampled integration and log-CLI tests exercise behavior beyond helper calls.
- **Explicit integration evaluation:** `EvaluateIntegrationProgress` takes state, capability, and integration HEAD as inputs; Git/config reads remain in its live wrapper.
- **Concrete follow-through:** log-analyzer CI and Windows Go jobs exist; `RunSupervisor` shrank 442→417 lines and TUI `update.go` shrank 742→551.
- **Traceability and restrained dependencies:** 104 numbered ADRs and 14 direct Go requirements support a broad runtime/tooling surface without a large direct dependency list.

### Areas for Improvement

- **Ownership and concentration:** parallel initialization implementations already use different Git command boundaries; independent file splits would preserve that divergence.
- **Lifecycle review cost:** long verdict, merge, claim, and transition functions demand substantial context to assess state invariants.
- **Partial CI enforcement:** adversarial-pairing tests and Python lint/type checks remain absent; the full Go race suite and usable coverage upload are still not wired into CI.
- **Uneven Python protection:** context-engineering and white-box-red-testing have no tracked tests; four Python modules still exceed 500 lines.
- **Documentation and vocabulary drift:** the current overview still understates role/provider scope, and finite control-flow categories are compared through raw strings across packages.

## Overall Grade: B+ (Good)

**Retained, not downgraded again.** The deduction from A- remains for significant responsibility concentration and incomplete automated enforcement across maintained languages. The strongest case for A- is the growing behavioral test suite, broader platform checks, and partial structural progress. It does not yet outweigh the concrete initialization duplication, growing installation/prompt surfaces, and remaining CI gaps.

This judgment follows the subsystem evidence below. File size, test ratios, and documentation volume are supporting signals rather than standalone quality scores.

## Detailed Subsystem Analysis

### Domain State, Pipeline, Persistence, and Validation ★★★★☆

**Evidence:** `internal/models` has 2,635 production / 5,257 test LOC; pipeline 1,677 / 5,196; database 725 / 3,002; state validation 2,740 / 4,750. These remain distinct package boundaries.

**Strengths:** Declarative pipeline resolution, domain models, database operations, and state validation retain separate owners and substantial tests.

**Concerns:** `pipeline/resolver.go` is 974 LOC and `models/task.go` 892. They are concentrated but more cohesive than initialization or artifact installation. Raw category comparisons in `pipeline/config.go:267–275` remain part of the [vocabulary ownership concern](architectural-issues.md#control-flow-vocabulary-bypasses-domain-ownership).

### Lifecycle Operations and Worktree Integration ★★★☆☆

**Evidence:** `internal/ops` has 21,873 production / 54,070 test LOC (2.47:1). `proceed.go` grew 1,500→1,563 lines; its transition, cohort, recovery, topology/SCC, scheduler, and child-construction functions remain together.

**Strengths:** The new 1,037-line `integration_progress.go` separates pure evaluation at line 74 from live Git/config acquisition at line 88. `internal/integration/sliced_integration_test.go:34` tests lifecycle barriers and zero-slice behavior through real reconciliation. This concentration is not by itself evidence that the evaluator needs a new abstraction.

**Concerns:** Verdict, merge, submission, and claim-completion implementations are 382, 358, 318, and 265 lines. The [transition decomposition](architectural-issues.md#decompose-proceedgo-1500-loc) and [refresh ownership](architectural-issues.md#worktree-intelligence-refresh-has-multiple-owners) concerns remain relevant. Refactoring must preserve transaction, ordering, and failure-policy boundaries.

### Agent Supervision, Prompts, and Providers ★★★☆☆

**Evidence:** Agent package 8,870 production / 25,995 test LOC (2.93:1); prompts 1,239 / 5,721. `supervisor.go` is 1,131 LOC; `prompt.go` grew 956→1,232.

**Strengths:** `RunSupervisor` shrank to 417 lines. The execution interface and compatibility adapter already reside in `internal/agent/llm_agent.go:123–178`; the prior advice to move the default executor out of the supervisor is stale.

**Concerns:** Restart/crash/spin policy, heartbeat behavior, and the main loop remain concentrated. Prompt adapter plumbing at `prompt.go:169–307` and integration-context construction at lines 446, 476, 571, and 633 give the file multiple change drivers. Consolidate finite claim/category vocabulary without turning user-configurable role identities into a closed enumeration.

### Commands and CLI Wiring ★★★☆☆

**Evidence:** Commands 9,795 production / 31,926 test LOC (3.26:1); CLI 6,773 / 11,419. `init.go` grew 1,268→1,418; `watch.go` stayed at 1,407.

**Strengths:** Much of the large Cobra surface is registration rather than dense behavioral code. Stable watch size counters a blanket claim of worsening everywhere.

**Concerns:** Initialization's 421-line main command coexists with another `InitProject` implementation in ops. `commands/init.go:1281` uses `gitpkg.Command`, while `ops/init_project.go:307` uses raw `exec.Command("git", ...)`. Branch creation also differs in repository targeting: the ops implementation passes `-C projectRoot` (`ops/init_project.go:318–328`), while the commands implementation relies on the current directory. This verifies the registry's [duplicate initialization finding](architectural-issues.md#duplicate-initialization-implementations). Watch still mixes observation, repair, and presentation. Inspect handlers repeat output switches, for example `inspect_agents.go:207–245`, with meaningful list/scalar differences that any consolidation must retain.

### Terminal UI and Interactive Flows ★★★★☆

**Evidence:** TUI 2,425 production / 4,458 test LOC (1.84:1); interactive 345 / 205 (0.59:1). `view.go` is 867 lines; `update.go` is 551, down from 742.

**Strengths:** The Model-Update-View shape remains recognizable; the reduction in update code is material progress.

**Concerns:** `renderTaskPanel` remains 204 lines and `Update` 166. Interactive test volume is lower than the core runtime's, but ratio alone does not demonstrate a missing behavior test. Keep panel/message-family extraction opportunistic rather than treating TUI as an urgent decomposition target.

### Tooling Integrations ★★★☆☆

**Evidence:** `scipsearch.go` is nearly unchanged at 1,567 lines; `pairingindex/hooks.go` grew 1,053→1,077. SCIP has 1,579 production / 1,927 test LOC; Semble 764 / 831.

**Strengths:** Tool-specific execution remains behind dedicated packages. `languageIndexPlans` at `scipsearch.go:611` already dispatches to separate Go, TypeScript, and Python planning functions.

**Concerns:** Configuration, refresh, discovery, planning, and command execution remain concentrated in SCIP. The existing three-case dispatch does not justify a new strategy interface by itself. More consequential is the [multiple-owner refresh sequence](architectural-issues.md#worktree-intelligence-refresh-has-multiple-owners) across claim, submission, and runtime worktree checks. Distinct trigger and failure semantics must remain explicit.

### Installation, Embedding, Branding, Updates, and Toolchain ★★★☆☆

**Evidence:** `embedded.go` grew 1,530→1,635 LOC; updater 1,259→1,323. Embedded tests total 5,057 LOC and updater tests 2,060.

**Strengths:** Embedded assets and consistency checks provide inspectable installation inputs. Updater and toolchain logic retain dedicated packages.

**Concerns:** Corpus writing, Claude JSON, Codex permissions/hooks, and project artifacts still share `embedded.go` (entry points around lines 146, 391, 454, 516). These schemas have different change drivers: the [artifact-family split](architectural-issues.md#split-embeddedgo-by-artifact-family-1530-loc) remains justified. Native Windows coverage is new, but [TECH_DEBT.md](../../TECH_DEBT.md) records remaining stdio-handle and toolchain limitations; a configured CI job does not establish complete platform parity.

### Python Skill Utilities ★★★☆☆

**Evidence:** Eight production files / 5,438 LOC and seven test files / 3,669 LOC (0.67:1). Largest modules: log analyzer 2,163, corpus indexer 968, blackboard operations 661, blackboard state 650.

**Strengths:** Improved from ★★☆☆☆. Log-analysis CLI tests invoke the scripts and assert rendered token totals, usage provenance, deduplication, and friction evidence (`test_log_cli_e2e.py:275–291`). CI now executes the log-analysis test directory using a frozen dependency environment.

**Concerns:** Context-engineering and white-box-red-testing still have no tracked Python tests; adversarial-pairing has three test files but its tests are outside CI. The [quality-parity finding](architectural-issues.md#python-skill-utilities-lack-quality-parity) is partially addressed, not closed. The report's former “no Python in CI” wording is stale.

### Testing and Quality Infrastructure ★★★★☆

**Evidence:** [Makefile](../../Makefile), `internal/testguard/{parallel_usage,sleep_usage}_test.go`, and the tracked test inventory.

**Strengths:** Routine, race, E2E, and coverage targets remain distinct. There are 22 files under `internal/integration/`, totaling 9,644 LOC. Only two test files carry the `e2e` build tag; integration-package file count should not be labeled an E2E count.

**Concerns:** No Go fuzz declarations exist. Parallel-use enforcement is a floor of 514 literal occurrences, not an exact count; the guard-equivalent current scan finds 521 in 50 files, excluding the guard itself. Ten actual `time.Sleep` calls remain against a ceiling of eleven. These guards count byte strings, not AST calls or runtime scheduling. Coverage percentages and complete test-suite health were not measured in this pass.

### Pre-Commit and CI Pipeline ★★★☆☆

**Evidence:** [.github/workflows/ci.yml](../../.github/workflows/ci.yml), [release.yml](../../.github/workflows/release.yml), [Makefile](../../Makefile), and [.pre-commit-config.yaml](../../.pre-commit-config.yaml).

**Strengths:** Linux/macOS run lint, routine Go tests, log-analyzer pytest, selected race-enabled E2E tests, and builds. Windows now runs native Go vet, tests, and build. The local policy still has 22 hook entries.

**Concerns:** `make lint` checks embedding/test-helper boundaries, fmt, and vet; it does not run staticcheck, goimports verification, jscpd, ruff, or mypy. CI's Python test command only names `skills/liza-logs/scripts`. The Windows workflow comment claiming Linux/macOS cover Python pre-commit hooks is not supported by the executable steps.

CI still does not run the full `make test-race` target or generate the fixed `coverage.out` expected by its non-blocking Codecov upload. [Existing debt](../../TECH_DEBT.md) records this wiring gap. The race-enabled E2E subset does not replace the full race suite. Local goimports/staticcheck use `@latest`, and duplicate detection invokes unpinned `npx jscpd`.

### Documentation and Specifications ★★★★☆

**Evidence:** Tracked corpus inventory, [overview.md](overview.md), [pipeline.yaml](../../internal/embedded/pipeline.yaml), [provider-catalog.yaml](../../provider-catalog.yaml), and the ADR index.

**Strengths:** The specification and support-document corpus continues to grow, with 104 numbered ADRs preserving historical decisions. Registry ownership distinguishes assessment snapshots from issue lifecycle decisions.

**Concerns:** The overview still depicts three roles and lists four providers; canonical configuration contains 13 roles and nine providers. The registry's CI/Python entries retain July measurements and blanket “no Python checks” wording. Read them with this snapshot's partial-progress qualification; this update does not silently change their lifecycle status.

## Previous Finding Disposition

“Verified” means checked against current source or executable configuration, not proven by a passing runtime test.

**Follow-up owner: assessment author.** In the next registry-maintenance change, correct the stale Python-CI wording and measurements in [cross-language CI gates](architectural-issues.md#ci-does-not-enforce-cross-language-quality-gates) and [Python quality parity](architectural-issues.md#python-skill-utilities-lack-quality-parity), preserving their remaining open concerns; the withdrawn vestigial-Python advice below is historical assessment advice, not a separate registry entry.

| Previous finding | Disposition | Current evidence |
|------------------|-------------|------------------|
| Proceed decomposition | ✓ Still relevant; grew | 1,500→1,563 LOC; mixed transition responsibilities remain |
| Init decomposition | ~ Adjusted; ownership first | 1,268→1,418; parallel implementations and Git-boundary divergence |
| Supervisor decomposition | ~ Partial progress | Loop 442→417; execution adapter separate; policy concentration remains |
| Watch decomposition | ✓ Still relevant; stable | 1,407 LOC, mixed observation/repair/rendering |
| Embedded artifact split | ✓ Still relevant; grew | 1,530→1,635; independent schemas remain co-located |
| CI has no Python execution | ✗ Stale wording | Log-analyzer pytest now runs; broader parity still missing |
| Python quality parity | ~ Improved, incomplete | Ratio 0.43→0.67; new CLI tests; two utilities still lack tests |
| Raw control-flow vocabulary | ✓ Still relevant | Category/cardinality comparisons bypass existing ownership |
| SCIP strategy prescription | ~ Narrowed | Existing planner functions already separate language variants |
| Inspect output duplication | ✓ Still relevant | Repeated format switches; preserve list/scalar semantics |
| TUI concentration | ~ Improved | update.go 742→551; view remains 867 |
| Missing process/gitenv tests | ✓ Earlier resolution remains | Both packages have tracked tests |
| Interactive coverage concern | ~ Evidence limited | Ratio improved to 0.59; no coverage measurement |
| Coverage enforcement / full CI race run | ✓ Still relevant | No profile producer for upload; only selected E2E runs use race |
| Fuzz testing | ✓ Still relevant | Zero Go fuzz declarations |
| Architecture overview drift | ✓ Still relevant | Three/four documented roles/providers versus 13/nine configured |
| Dependency-version freshness | Unverified | Pins unchanged; no current upstream comparison or vulnerability scan |
| Release binary-size budget | ✓ Still absent from inspected release configuration | Packaging/checksums do not enforce size trends |
| Remove vestigial Python tooling | ✗ Remains withdrawn | Active scripts, tests, and CI execution contradict that characterization |

## Prioritized Refactoring Roadmap

Numbers preserve incoming registry links; they do not imply execution order. Start with CI parity and artifact-family separation. Before splitting initialization mechanics, settle their shared owner. Structural relocation is lower risk than changing state or recovery semantics.

### P1: High Impact / Low Risk

#### 1.1 Decompose `internal/ops/proceed.go`

- **Registry:** [Transition decomposition](architectural-issues.md#decompose-proceedgo-1500-loc).
- **What:** Separate graph algorithms, cohort construction, recovery, and scheduling along existing function boundaries.
- **Risk / impact:** Low for relocation only; reduces review context. Changes to transaction/order semantics require a separate, higher-risk change.
- **Depends on:** Existing lifecycle and integration contracts remaining intact.

#### 1.2 Decompose `internal/commands/init.go`

- **Registry:** [Init decomposition](architectural-issues.md#decompose-initgo-1268-loc) and [duplicate initialization](architectural-issues.md#duplicate-initialization-implementations).
- **What:** Establish one owner for shared initialization mechanics before splitting the command into configuration, detection, and artifact phases.
- **Risk / impact:** Owner convergence is medium risk and precedes the low-risk relocation. It prevents fixes diverging across two implementations.
- **Depends on:** Explicit equivalence tests for the two entry paths; do not independently decompose both copies.

#### 1.3 Decompose `internal/agent/supervisor.go`

- **Registry:** [Supervisor decomposition](architectural-issues.md#decompose-supervisorgo-1129-loc).
- **What:** Separate restart/spin policy from heartbeat and loop orchestration. Preserve the existing LLMAgent boundary; remove no working adapter solely to satisfy old advice.
- **Risk / impact:** Low for cohesive relocation, medium for concurrency-policy changes; narrows runtime review context.
- **Depends on:** Preserved process, lease, and restart behavior.

#### 1.4 Decompose `internal/commands/watch.go`

- **Registry:** [Watch decomposition](architectural-issues.md#decompose-watchgo-1407-loc).
- **What:** Separate observation, repair decisions, and rendering without changing recovery policy.
- **Risk / impact:** Low for relocation; isolates output changes from orchestration.
- **Depends on:** Existing command output and repair behavior.

#### 1.5 Split `internal/embedded/embedded.go` by Artifact Family

- **Registry:** [Artifact-family split](architectural-issues.md#split-embeddedgo-by-artifact-family-1530-loc).
- **What:** Separate corpus, Claude settings, Codex settings/hooks, and project artifact implementations within the existing package.
- **Risk / impact:** Low with unchanged APIs; localizes independent schema changes.
- **Depends on:** Artifact consistency and alternate-brand rendering checks.

#### 1.6 Enforce the Multi-Language Quality Contract in CI

- **Registry:** [Cross-language CI gates](architectural-issues.md#ci-does-not-enforce-cross-language-quality-gates).
- **What:** Extend the existing frozen Python job to adversarial-pairing tests; enforce ruff/mypy and intended Go/static/duplication checks with pinned tools. Wire the full race suite and an explicit coverage artifact producer.
- **Risk / impact:** Low runtime risk; existing check failures may require separately scoped fixes. Aligns merge checks with declared local policy.
- **Depends on:** Explicit CI coverage output handling; the interactive, self-cleaning local coverage target is not an upload contract.

#### 1.7 Own Control-Flow Vocabulary

- **Registry:** [Vocabulary ownership](architectural-issues.md#control-flow-vocabulary-bypasses-domain-ownership).
- **What:** Reuse or define constants for finite claim types, categories, and cardinalities. Preserve YAML-owned, configurable role identities as required by ADR-0045.
- **Risk / impact:** Low when values stay unchanged; reduces cross-package string coupling.
- **Depends on:** Existing domain ownership, not a new closed role-name enum.

### P2: Medium Impact / Medium Risk

#### 2.1 Consolidate Tool Refresh Ownership

- **Registry:** [Worktree intelligence refresh](architectural-issues.md#worktree-intelligence-refresh-has-multiple-owners).
- **What:** Give refresh orchestration one owner with explicit trigger/enablement/failure inputs. Keep tool-specific planners and execution separate; extract existing SCIP planner functions only where that improves cohesion.
- **Risk / impact:** Medium because lifecycle failure tolerance differs; prevents inconsistent updates across callers.
- **Depends on:** Tests preserving each caller's enablement and failure policy. A new planner interface is not a prerequisite.

#### 2.2 Restore Python Utility Test and Structure Parity

- **Registry:** [Python quality parity](architectural-issues.md#python-skill-utilities-lack-quality-parity).
- **What:** Add behavioral tests for corpus indexing and target discovery; separate analyzer parsing, aggregation, and rendering when extending those boundaries.
- **Risk / impact:** Medium around CLI/serialized output; protects currently untested maintained utilities.
- **Depends on:** CI execution of the added tests. Test authoring can proceed independently of recommendation 1.6.

#### 2.3 Add Coverage Trend Enforcement

- **What:** First restore reproducible coverage generation/upload, then select an evidence-based non-regression policy.
- **Risk / impact:** Medium; arbitrary thresholds can reward gaming. Turns missing evidence into a useful regression signal.
- **Depends on:** Recommendation 1.6's coverage artifact contract, not completion of unrelated Python refactoring.

#### 2.4 Refresh the Current Architecture Overview

- **What:** Reconcile the overview with canonical role/provider inventories and distinguish simplified diagrams from exhaustive current architecture.
- **Risk / impact:** Low; improves onboarding accuracy.
- **Depends on:** Canonical pipeline/catalog data; keep historical ADRs historical.

#### 2.5 Centralize Inspect Output Dispatch

- **What:** Share repeated dispatch only where output behavior is equivalent; preserve list/scalar restrictions and errors.
- **Risk / impact:** Medium around CLI compatibility; removes repeated formatting ceremony.
- **Depends on:** Output contract tests.

#### 2.6 Review Direct Dependency Compatibility

- **What:** Recheck upstream releases and advisories before proposing changes to the unchanged pins.
- **Risk / impact:** Medium for actual updates; terminal width and version ordering need targeted validation.
- **Depends on:** Fresh external evidence. No urgent upgrade is justified by this pass alone.

#### 2.7 Partition TUI Growth by Panel and Message Family

- **What:** Extract cohesive panel/message families when extending them; retain the existing framework flow.
- **Risk / impact:** Medium if generalized prematurely; lower priority after the measured update-file reduction.
- **Depends on:** A concrete extension/review burden, not crossing a LOC threshold alone.

### P3: Strategic / Long-Term

#### 3.1 Add Targeted Fuzz Tests

- **What:** Start with deterministic YAML and graph/state-input boundaries.
- **Risk / impact:** Medium harness investment; exercises combinations absent from enumerated examples.
- **Depends on:** Explicit invariants and stable, bounded test inputs.

#### 3.2 Automate Spec and Overview Drift Signals

- **What:** Validate enumerable role/provider facts from their canonical configurations.
- **Risk / impact:** Medium ownership cost; reduces recurring inventory drift.
- **Depends on:** Corrected current overview and explicit generated/manual boundaries.

#### 3.3 Track Release Binary Size

- **What:** Record comparable release sizes before setting an alert budget.
- **Risk / impact:** Low; makes embedded-corpus/dependency growth visible.
- **Depends on:** A reproducible platform/build baseline. Lower priority than correctness and CI gaps.

## Validation and Reproduction Notes

- Inventory: `git ls-files -z`; historical baseline: `git ls-tree -r --name-only ffe89080` and `git cat-file --batch`. Count physical lines with the classification rules above.
- Revision range: `git rev-list --count ffe89080..HEAD` returned 79; assessed HEAD is `73a50ca8`.
- Function size: parse tracked production Go files with `go/parser`; select `ast.FuncDecl` with a body and compute `End.Line - Pos.Line + 1`.
- Manifests and `go list -m all` establish declared/selected dependencies; no upstream freshness inference is made.
- Workflow recipes establish configured checks, not successful remote runs. Sampled test source establishes assertions, not current test outcomes.
- The unrelated working-tree change in `stacklit-insights.json` was excluded from assessment evidence and left untouched.
