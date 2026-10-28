# Code Quality Assessment and Refactoring Roadmap

* Date: 2026-10-27 (commit `ca8d76a3`, branch `global-refactoring`)
* Previous: 2026-09-06 (commit `73a50ca8`) — 1,103 commits since
* Author: Claude Opus 5.5
* Mode: Reassessment

## Document Role

This is a dated measurement and prioritization snapshot, not a certification of runtime correctness. [The architectural issue registry](architectural-issues.md#open-issues-summary) remains the sole lifecycle authority; this report proposes, it does not close, registry entries.

The assessed commit is the head of the architecture-consolidation integration branch ([goal](../goals/20261023-architecture-consolidation.md)), 699 commits ahead of `main`. The [run-close record](consolidation-run/run-close.md) labels the run complete: all nine areas are closed with G1–G8 evidence, and the merge to `main` is a pending human pull request. Ratings describe that branch head, not `main`.

Code metrics were measured at `dcbfdd04`. The four later commits (`6eb9f9fa` to `ca8d76a3`: A9 boundary, overview rewrite, registry reconciliation, close record) change only `.md` and `.json` files, so every code figure below holds at `ca8d76a3`; documentation figures were re-measured there.

Based on: tracked trees of `73a50ca8`, `96c9ad69` (consolidation start) and `dcbfdd04`, extracted with `git archive`; Go AST function spans; manifests; workflow, Makefile and pre-commit recipes; the archguard data files; targeted source reads; the consolidation goal, its plans and ADR-0218–0227; the registry; and the A9 boundary and run-close records. Executed by this assessment: `go test -count=1 ./internal/testguard/...` (157 passed, 3 packages). Recorded by the run, not re-run here: `make lint-archguard`, `make test`, `make lint` and `make test-e2e` exit 0 at `7a44042ba` (A9 G4), and CI run 40492973663 green on Linux, Windows and macOS (A9 G8); the run close shows that production, test and guard trees are unchanged since. Not executed: coverage, vulnerability audit, upstream-version comparison.

## Repository Metrics Dashboard

Definitions are unchanged from the previous report and reproduce its 2026-09-06 figures exactly: physical lines; tracked files; generated embedded corpus copies and `testdata/` excluded; all platform-specific Go files included; `internal/testhelpers/` non-test files reported separately. Function counts count declarations.

The consolidation start column separates feature growth (409 commits, 09-06 → 10-23) from the consolidation itself (690 commits, 10-23 → HEAD).

| Metric | 2026-09-06 `73a50ca8` | 2026-10-23 `96c9ad69` | Current `ca8d76a3` |
|--------|--------------:|--------:|-------:|
| Go production LOC / files | 76,738 / 312 | 137,963 / 569 | 149,233 / 792 |
| Go test LOC / files | 188,254 / 308 | 304,875 / 788 | 351,367 / 1,182 |
| … of which oracle harness (`internal/oracle`) | — | — | 21,315 / 240 |
| Go test-support LOC / files | 1,610 / 14 | 1,943 / 18 | 4,107 / 34 |
| Go test-to-production ratio | 2.45:1 | 2.21:1 | 2.35:1 |
| Go test / benchmark / fuzz functions | 3,452 / 6 / 0 | 5,940 / 11 / 0 | 6,878 / 11 / 0 |
| Oracle golden fixtures (`internal/oracle/**/testdata`) | — | — | 3,522 files / 274,817 lines |
| Production Go files >300 / >500 / >800 LOC | 82 / 43 / 20 | 143 / 69 / 33 | 160 / 41 / 0 |
| Production Go functions >100 / >150 / >200 LOC | 64 / 24 / 9 | 95 / 44 / 23 | 3 / 1 / 0 |
| Function length median / p90 / p99 | — | 11 / 44 / 143 | 12 / 40 / 82 |
| Python production LOC / files | 5,438 / 8 | 5,792 / 10 | 5,792 / 10 |
| Python test LOC / files / functions | 3,669 / 7 / 99 | 4,218 / 9 / 133 | 4,218 / 9 / 133 |
| TypeScript/JS LOC / files | 132 / 1 | — | 437 / 3 |
| Shell LOC / files | 1,909 / 10 | — | 1,957 / 10 |
| Specification Markdown, incl. archive | 230 / 28,604 | — | 498 / 101,766 |
| … of which `specs/plans/` + `specs/arch-plan/` | — | — | 102 / 43,719 |
| Numbered ADRs under `specs/architecture/ADR/` | 104 | 197 | 210 |
| Markdown under docs/ / support-docs/ / contracts/ | 7,772 / 4,658 / 5,483 | — | 8,843 / 6,467 / 5,505 |
| `TECH_DEBT.md` lines | 219 | — | 1,423 |
| Direct / indirect Go requirements | 14 / 26 | — | 15 / 25 (`x/text` promoted) |
| Configured pre-commit hook entries | 22 | — | 22 |

Production LOC nearly doubled in seven weeks; most of that is feature growth before the consolidation began (+80%). The consolidation added 8% LOC and 223 files while removing every file >800 and all but three functions >100. Of the three, `toolchain.Catalog` (153, `catalog.go:93`) is the run's only S2 exception (table); `testshard.reconcile` (138, `scripts/ci/`) and `promptbench.MeasureRendered` (132) lie outside S1 scope.

### Largest Production Go Files

| LOC | File |
|----:|------|
| 786 | `internal/db/blackboard.go` |
| 765 | `internal/usage/report.go` |
| 742 | `cmd/liza/cmd_init.go` |
| 739 | `internal/analysis/patterns.go` |
| 726 | `internal/ops/orphaned_review_base.go` |
| 705 | `cmd/liza/cmd_review.go` |
| 678 | `internal/ops/claim_task_strategy.go` |
| 672 | `internal/statevalidate/integration.go` |
| 663 | `internal/commands/setup.go` |
| 654 | `internal/toolresult/redact.go` |

Six files sit at 700–800 LOC and 14 at 600–699. Function lengths do not bunch below the cap (21 at 91–100 versus 42 at 80–89), and no production file uses a mechanical-split name (`_part`, `_2`, `_misc`, `_rest`); the one `_helpers` file is `commands/format_helpers.go`. These are weak but useful signals that S1 was met by separating concerns rather than by squeezing (S3).

### Package Concentration

| Package | Prod LOC / files | Test LOC | Ratio |
|---------|-----------------:|---------:|------:|
| `internal/ops` | 53,699 / 241 | 113,678 | 2.12:1 |
| `internal/commands` | 14,807 / 103 | 42,306 | 2.86:1 |
| `internal/agent` | 13,353 / 63 | 46,864 | 3.51:1 |
| `cmd/liza` | 11,192 / 56 | 19,680 | 1.76:1 |
| `internal/models` | 7,886 / 47 | 9,519 | 1.21:1 |

`ops` alone is 36% of production Go and remains one package.

### Dependencies and Code Hygiene

[go.mod](../../go.mod) declares 15 direct and 25 indirect requirements; the only change is `golang.org/x/text` becoming direct. [pyproject.toml](../../pyproject.toml) is unchanged. No upstream-freshness or advisory check was performed.

Production Go (787 tracked files under `cmd/`, `internal/`, `scripts/`): no TODO/FIXME/HACK markers (one `XXX` match is a `mktemp` template), no `nolint`/`nosec`, no `interface{}`. Seven `os.Exit` calls, all in `main` functions. Sixteen `panic` calls: flag/schema registration guards (`cmd_agent.go`, `payloadschema/registry.go` ×4), embedded-catalog load guards, a completion invariant, a deliberate re-panic, a randomness-unavailable guard, two tooling-only sites (`promptbench`, `scripts/ci/testshard`), and `ops/assess_blocked_fingerprint.go:112`, reachable only if a task timestamp is outside JSON's year range. `any` appears in 156 files, concentrated in JSON/settings boundary code (`embedded/claude_settings.go`, `toolresult/claude_*`), where dynamic shapes are inherent.

Category, cardinality and claim-type comparisons are now typed (`pipeline/config.go:332,338`; ADR-0220–0227). Remaining raw-string dispatch is concentrated in two places described below: provider identities across eight packages, and role-pair names in the sliced-integration capability probe.

## Executive Summary

Between the two assessments Liza grew 80% in seven weeks of feature work. It then ran a goal-driven, oracle-protected consolidation that met its size criterion and made the structure enforceable. Every production Go file is now ≤800 LOC; three functions exceed 100 lines, one an approved table. The layer rule (K3) and the blackboard mutation boundary (C6) are checked in `make lint` by `internal/testguard/archguard`. A 291-test black-box oracle with chained provenance (ADR-0219) pins CLI, state, prompt and artifact surfaces. CI now runs the full Go suite under race on Linux, plus a reconciled seven-shard native Windows suite. Most previous P1 recommendations are complete, and the rewritten overview now tells the next run's agents where new code belongs. What remains is enforcement breadth rather than structure: CI still runs none of the local static and Python quality hooks, and produces no coverage.

### Key Strengths

- **Structure is guarded, not just improved:** a ratchet-list size guard (9 empty area ratchets, one S2 table exception), an import-direction check with three rationale-carrying allowlist entries, and a C6 mutation guard. They run in `make lint` and `./...`, so regression is a test failure rather than a review judgement.
- **Behaviour preservation was made measurable:** the oracle harness (21.3k test LOC, 274.8k fixture lines) separates frozen-surface tests from white-box tests that move with code. The only allowed golden edits are the goal's three narrow exceptions.
- **Concern separation over mechanical splitting:** `proceed.go` 1,563→638, supervisor split into eight lifecycle files, prompt assembly into seven context files, `embedded.go` into eleven artifact-family files (max 344). Rejected alternatives (ops sub-packages, generic lifecycle engines) are recorded in ADRs 0222–0225.
- **Platform and race evidence in CI:** full `-race` over units, integration and `cmd/liza`; Windows shards whose `go test -json` streams are reconciled to prove every listed test ran once (`ci.yml:254`); actions pinned by SHA in the shared workflow.
- **Decision record discipline at scale:** 106 ADRs added; all 149 Go file references in `INVARIANTS.md` resolve to existing files at HEAD (file level; line anchors not checked).

### Areas for Improvement

- **CI still enforces only part of the local policy:** staticcheck, goimports, ruff, mypy and jscpd run only in pre-commit (staticcheck/goimports `@latest`, jscpd via unpinned `npx`). CI Python tests cover only `skills/liza-logs`. The Codecov step was removed, so CI now produces no coverage signal.
- **Provider support still crosses the catalog boundary:** about twenty raw provider comparisons in `agent`, `agentlaunch`, `commands`, `providers`, `providersignal` and `cmd/liza`. The Cursor dependency expansion is written twice and already diverges: `commands/init.go:193` adds `cursor-acp`, `setup.go:229` does not.
- **`ops` concern boundaries are documentary:** 53.7k LOC in one package. ADR-0225 records two-way helper coupling between the A3–A6 concern families. Nothing but file naming stops new code from deepening it.
- **Overview drift outside the rewritten section:** the new Go Code Structure section of `overview.md` (lines 170–334) matches the tree. The untouched System Components diagram and provider note (lines 3–43) still show three roles and four providers, against 13 roles and nine providers configured.
- **Sustained growth of auxiliary corpora:** `TECH_DEBT.md` grew 219→1,423 lines; run plans add 43.7k lines under `specs/`; oracle fixtures add 275k lines. Each is justified locally, but none has a stated retention or pruning rule.

## Overall Grade: A- (Excellent with concerns)

**Raised from B+.** The B+ deduction was for "significant responsibility concentration and incomplete automated enforcement across maintained languages". The first half is substantially paid: initialization duplication is gone (C4); the proceed, init, watch, embedded and supervisor concentrations are decomposed; and size and layering are now executable constraints. The second half is unchanged and is the deduction from A: CI does not run the static, duplication or Python quality gates that the local policy declares, and there is no coverage signal. The secondary deduction is that `ops` concern boundaries and provider extension depend on discipline rather than mechanism.

**Basis.** The grade rates the integration branch head after run close; the merge to `main` is pending. This assessment ran only the guard package. Full-suite evidence is the run's recorded A9 G4 and G8 results at `7a44042ba`, which apply to `ca8d76a3` because production, test and guard trees are unchanged since.

**Strongest case against A-:** production code doubled in seven weeks, and the consolidation's own metrics are proxies; cohesion is checked by review, not measured. The answer is that the guards now stop the specific regression that produced the B+ (unbounded files and functions). The ADRs record why each partition was chosen, so later agents have both a constraint and a rationale to follow.

## Detailed Subsystem Analysis

### Domain State, Pipeline, Persistence, and Validation ★★★★☆ *(unchanged)*

**Evidence:** models 7,886 / 9,519 test LOC; pipeline 1,958 / 5,719; db 2,246 / 6,502; statevalidate 4,908 / 8,182. New boundary packages: `payloadschema` (1,671), `referencecontract` (2,073), `runtimeinputs` (426), `archiveobject` (156).

**Strengths:** Typed category/cardinality ownership closed the previous vocabulary concern. K3 holds for the domain layer with no allowlist entry. Payload schemas register through a validating registry (`payloadschema/registry.go:42–54`).

**Concerns:**
- `models/task_status.go:13–19` still binds generic names to code-pipeline values (`TaskStatusReady = "DRAFT_CODE"`), the registry's high [status vocabulary partition](architectural-issues.md#status-vocabulary-partition-between-go-constants-and-declarative-pipeline).
- `pipeline/resolver_sliced.go:31–41,62,73` recognizes the sliced-integration capability by matching eight YAML role-pair, role and transition names. A renamed or customized topology degrades to `SlicedIntegrationUpgradeRequired` with explicit guidance, so the failure is visible rather than silent. It is still a user-configurable identity (ADR-0045) hardcoded in the domain layer; see [sliced-integration name fingerprint](architectural-issues.md#sliced-integration-capability-detected-by-role-pair-names). *(new, reassessment 2026-10-27)*
- Models' test ratio (1.21:1) is the lowest of the large packages. `db/blackboard.go` (786) is the largest production file and closest to the cap.

### Lifecycle Operations and Worktree Integration ★★★★☆ *(raised from ★★★☆☆)*

**Evidence:** `ops` 53,699 / 113,678 test LOC (2.12:1) in 241 files; largest 726 (`orphaned_review_base.go`); `proceed.go` 1,563→638. File prefixes cluster by capability (claim 10, merge 9, recover 8, integration 8, acceptance 8).

**Strengths:** The previous review-cost concern (382/358/318/265-line verdict, merge, submission and claim functions) is resolved: no `ops` function exceeds 100 lines. C6 confines production blackboard mutation to `ops` and `db`, enforced by `archguard/c6_mutation_guard_test.go`. Rejected generic lifecycle engines (ADR-0222 D4, ADR-0225 D6) keep transaction shape explicit per operation.

**Concerns:**
- One package holds 36% of production Go. ADR-0222 D3, 0223 D3 and 0225 D5 reject sub-packages because concerns call each other's private helpers in both directions (A6 calls A3–A5; A3–A5 call A6's integration locks). The decision is sound for this run, but the concern partition now has no mechanical guard; drift shows up only in review. *(new, reassessment 2026-10-27)*
- Index refresh still has three owners: claim (`claim_task_worktree.go:59` → `scip_indexing.go`, `stacklit_indexing.go`), submission (`submit_review.go:117–130`), and reviewer worktree checks (`agent/worktree_check.go:172–188`); see [refresh ownership](architectural-issues.md#worktree-intelligence-refresh-has-multiple-owners).

### Agent Supervision, Prompts, and Providers ★★★★☆ *(raised from ★★★☆☆)*

**Evidence:** agent 13,353 / 46,864 (3.51:1); prompts 2,555 / 8,047; new `agentlaunch` (1,166), `providersignal` (914), `rolemodels` (204), `sessionvalidation` (347).

**Strengths:**
- The supervisor's restart, turn, block, lifetime and runtime-failure policies have separate files.
- `RoleStrategy` (`strategy.go:15`) has three implementations that absorb real per-role variation (K6).
- Prompt context construction is split by source, and `prompts` no longer imports `ops` (A8).
- C2/C3 landed: `agentlaunch.ProviderKeys` feeds `ops.ApprovalsHaveProviderDiversity` (`claiming_merge.go:56,120`).

**Concerns:** Provider-specific behaviour is still dispatched on raw IDs outside the catalog. Examples: `cli_agent_command.go:149`, `session_preflight.go:66`, `tool_result_acp.go:17`, `cli_agent.go:203`, `checkpoint_summary.go:378`, `acpx_agent.go:157–159`, `providers/await.go:16`, `providersignal/quota.go:71`, `quota_reset.go:41,54`. This verifies the registry's [provider catalog boundary](architectural-issues.md#provider-support-crosses-the-catalog-boundary) issue at a larger footprint than recorded.

### Commands and CLI Wiring ★★★★☆ *(raised from ★★★☆☆)*

**Evidence:** commands 14,807 / 42,306 (2.86:1); CLI 11,192 / 19,680 (1.76:1). The `watch*.go` family totals 2,328 LOC across files; the largest command files are `cmd_init.go` 742 and `cmd_review.go` 705.

**Strengths:** C4 deleted `ops.InitProject`, so initialization has one implementation, with `gitenv` timeouts and explicit project-root targeting. `jsonout` no longer imports `ops` (A7 allowlist empty).

**Concerns:**
- Provider expansion and the non-Claude instruction check are implemented twice and already diverge: `init.go:193` vs `setup.go:229` (only init adds `cursor-acp`), and `init_mas_state.go:155` vs `setup.go:391`. *(new, reassessment 2026-10-27)*
- Inspect output dispatch still repeats format switches in five production files (13 sites, densest in `inspect_task_render.go`).
- One raw `exec.Command("git", …)` remains at `cmd/liza/launch_resolve.go:127`, bypassing `gitenv`.
- `commands→agent` and `tui→agent` remain allowlisted K3 crossings with rationale.

### Terminal UI and Interactive Flows ★★★★☆ *(unchanged)*

**Evidence:** TUI 3,020 / 5,675 (1.88:1); largest file `commands.go` 412; interactive 356 / 215.

**Strengths:** `view.go` (867) and `update.go` are decomposed by panel and message family. The `tui/commands.go:327` mutation moved into `ops` (C6).

**Concerns:** None material; interactive test volume remains low, but no specific untested behaviour was identified.

### Tooling Integrations ★★★☆☆ *(unchanged)*

**Evidence:** scipsearch 1,654 / 2,170 in nine files; semble 1,011 / 1,156; pairingindex 1,751 / 2,284; new `toolresult` 1,642 / 1,725, `toolresultacp` 683 / 1,047, `usage` 1,435 / 1,375.

**Strengths:** `scipsearch.go` (1,567) is split; tool-specific execution remains behind dedicated packages. Semble's command plans carry override-only environments (`semble.go:57–64,186`), which keeps G1.4 assertions safe.

**Concerns:** The new tool-result and usage packages test at about 1:1, below the core runtime's 2–3.5:1. `usage/report.go` (765) and `toolresult/redact.go` (654) are among the ten largest files. Redaction is security-relevant and deserves the higher protection. Refresh ownership is as above.

### Installation, Embedding, Branding, Updates, and Toolchain ★★★★☆ *(raised from ★★★☆☆)*

**Evidence:** embedded 2,000 / 6,296 in eleven files (max `codex_permissions.go` 344); updater 1,402 / 2,247 in six; toolchain 1,611 / 1,661.

**Strengths:** The artifact-family split is complete (A2). Artifact byte-identity is now protected by the A2 oracle (`internal/oracle/a2/artifact`), closing the previous report's gap: `consistency_test.go` proves sync, not preservation.

**Concerns:** `toolchain.Catalog` (153 lines) is the only S2 exception; it is a table and is listed with a ruling. `TECH_DEBT.md` still records Windows stdio-handle and toolchain limitations.

### Python Skill Utilities ★★★☆☆ *(unchanged)*

**Evidence:** ten production files / 5,792 LOC; nine test files / 4,218 (0.73:1). Largest: `analyze-log.py` 2,158, `context-corpus-index.py` 968, `blackboard_op.py` 661, `blackboard_state.py` 650.

**Strengths:** New `liza-operator` scripts arrived with tests (`test_critical_paths.py`, `test_watch_until.py`).

**Concerns:** CI runs only `skills/liza-logs/scripts` (`Makefile` `test-python-nosync`). The adversarial-pairing and liza-operator tests are not executed in CI. `context-corpus-index.py` and `discover_targets.py` have no tests. Four modules exceed 500 lines, against Python's ~300-line norm.

### Testing and Quality Infrastructure ★★★★☆ *(unchanged rating, materially stronger)*

**Evidence:** 6,878 Go test functions; oracle 291 test functions across A1–A9 with `PROVENANCE`; `internal/testguard/archguard` 3,583 LOC of guard tests; guards executed (157 passed).

**Strengths:**
- Oracle tests use `ORACLE_WRITE`/tamper switches and chained provenance (ADR-0219), so golden regeneration is explicit and auditable.
- White-box tests may move; frozen-surface tests may not.
- The size guard keys offenders by file and function, which prevents renaming out of the ratchet.

**Concerns:**
- Still zero Go fuzz declarations.
- The sleep ceiling, documented as a ratchet that "should only decrease", rose 11→16 (`sleep_usage_test.go:13–20`, each increment ADR-linked).
- Oracle fixtures (274.8k lines) have no stated retention policy for after the run.
- Coverage remains unmeasured; `make coverage` gained `-coverpkg=./...` but is interactive and self-deleting.

### Pre-Commit and CI Pipeline ★★★☆☆ *(unchanged rating)*

**Evidence:** [ci.yml](../../.github/workflows/ci.yml), [ci-core.yml](../../.github/workflows/ci-core.yml), [Makefile](../../Makefile), [.pre-commit-config.yaml](../../.pre-commit-config.yaml).

**Strengths:**
- The previous "full race suite not in CI" finding is resolved: `ci-test-go-nosync` runs `-race` over units and integration, and the `cmd/liza` lane runs under race too.
- `lint-nosync` runs gofmt, three-OS `go vet` and archguard.
- The reusable `ci-core.yml` gives forks extension points without divergence.
- Generated-artifact drift fails CI.

**Concerns:**
- staticcheck, goimports, ruff, mypy and jscpd remain local-only.
- The Codecov step was removed, so CI now has no coverage artifact; the [non-reproducible coverage basis](architectural-issues.md#coverage-basis-is-not-reproducible-by-repository-tooling) is unchanged.
- `ci.yml:171–174` still claims Linux and macOS cover the Python pre-commit hooks; no executable step does.
- `ci.yml:167` uses `actions/setup-node@v7` by tag while `ci-core.yml` pins by SHA.

### Documentation and Specifications ★★★★☆ *(unchanged from 2026-09-06; briefly ★★★☆☆ at `dcbfdd04` before run close)*

**Evidence:** 498 spec files / 101.8k LOC; 210 ADRs; `overview.md` rewritten at `b901b381`; [run-close record](consolidation-run/run-close.md); registry at 59 open issues (17 high, 27 medium, 15 low).

**Strengths:**
- `overview.md` now documents the layer map, import rules with allowlist discipline, the composition root, the C6 mutation boundary, where each kind of new code goes, the permanent guard groups, and the S2 list. Its S2 table (`toolchain.Catalog`, 153 lines) and three retained crossings match this assessment's measurement.
- The registry was reconciled against the merged tree. Nine issues closed during the run with fixing commits and proof, including supervisor decomposition, agent lease (C1) and canonical provider identity (C2/C3). Partial fixes record their residual, for example raw `"orchestrator"` strings in `internal/agent` under control-flow vocabulary.
- The run-close record keeps out-of-scope items (`wont`) linked to their open entries instead of implying closure.
- ADR-0218–0228 record each partition and its rejected alternatives. `INVARIANTS.md`'s 149 Go file references resolve to existing files.

**Concerns:**
- The System Components diagram and provider note (`overview.md:3–43`) were not part of the rewrite and still show three roles and four providers. Readers now meet a stale summary immediately above an accurate structure section.
- Run artifacts share `specs/` with durable specs: plans and arch-plans are 43% of spec LOC; see [spec corpus lifecycle](architectural-issues.md#spec-corpus-lacks-lifecycle-management).

## Previous Finding Disposition

"Verified" means checked against current source or executable configuration, not proven by a passing runtime test.

| Previous finding | Disposition | Current evidence |
|------------------|-------------|------------------|
| 1.1 Proceed decomposition | ✓ Resolved | 1,563→638 LOC; registry closed at A4 |
| 1.2 Init decomposition and duplicate initialization | ✓ Resolved | C4 deleted `ops.InitProject`; registry closed at A7 |
| 1.3 Supervisor decomposition | ✓ Resolved | Eight `supervisor*.go` files; no function >100; registry closed at A9 |
| 1.4 Watch decomposition | ✓ Resolved | `watch*.go` family; registry closed at A7 |
| 1.5 Embedded artifact split | ✓ Resolved | Eleven files, max 344; registry closed at A2 |
| 1.6 Multi-language CI quality contract | ~ Partly addressed | Full race in CI; static and Python gates still local-only; coverage upload removed |
| 1.7 Control-flow vocabulary | ~ Substantially resolved | Categories, cardinalities and claim types typed (ADR-0220–0228); registry open for the raw `"orchestrator"` residual; role identities stay dynamic |
| 2.1 Tool refresh ownership | ✓ Still relevant | Three owners across `ops` claim, `ops` submission, `agent` reviewer check |
| 2.2 Python test/structure parity | ✓ Still relevant | Two utilities untested; CI covers one of three tested skills |
| 2.3 Coverage trend enforcement | ✓ Still relevant, regressed | No CI coverage artifact at all |
| 2.4 Architecture overview refresh | ~ Partly resolved | Go Code Structure rewritten (`b901b381`); System Components still three roles / four providers |
| 2.5 Inspect output dispatch | ✓ Still relevant | 13 format-switch sites in five files |
| 2.6 Dependency compatibility | Unverified | Pins unchanged; no external check |
| 2.7 TUI partitioning | ✓ Resolved | Largest TUI file 412 |
| 3.1 Fuzz tests | ✓ Still relevant | Zero fuzz declarations |
| 3.2 Spec/overview drift automation | ✓ Still relevant | Overview drift persists |
| 3.3 Release binary size | ✓ Still absent | Not in inspected release configuration |

## Prioritized Refactoring Roadmap

Numbers preserve the previous report's numbering because the registry links to `#16-…`, `#17-…` and `#22-…`. Completed items keep a one-line record; new items take the next free number in their priority band. Items 1.8, 2.8, 2.9, 3.4 and 3.5 are new in this reassessment; their headings carry no status suffix, so registry anchors stay stable. Numbers do not imply execution order.

### P1: High Impact / Low Risk

#### 1.1–1.5 Decompositions and Artifact Split — completed
`proceed.go`, `init.go` and duplicate initialization, `supervisor.go`, `watch.go` and `embedded.go` were decomposed by the consolidation run; see the disposition table.

#### 1.6 Enforce the Multi-Language Quality Contract in CI
- **Registry:** [Cross-language CI gates](architectural-issues.md#ci-does-not-enforce-cross-language-quality-gates).
- **What:**
  - Add pinned staticcheck and goimports verification to `lint-nosync`.
  - Add ruff, mypy, and the adversarial-pairing and liza-operator tests to `test-python-nosync`.
  - Pin jscpd and `actions/setup-node` (`ci.yml:167`).
  - Correct the `ci.yml:171–174` comment.
- **Risk:** Low runtime risk; existing violations may need separately scoped fixes.
- **Impact:** Merge checks match the policy agents are told to satisfy. The full race suite, the previous report's other half of this item, is now in CI.
- **Depends on:** None.

#### 1.7 Own Control-Flow Vocabulary
- **Status:** Substantially completed by the consolidation run.
- **Registry:** [Vocabulary ownership](architectural-issues.md#control-flow-vocabulary-bypasses-domain-ownership).
- **What remains:** The raw `"orchestrator"` role value in `internal/agent` recorded by the registry. Keep YAML-defined role identities dynamic (ADR-0045).
- **Risk:** Low.

#### 1.8 Make the Catalog Own Provider Dependencies
- **What:** Replace the duplicated Cursor expansion (`init.go:193`, `setup.go:229`) and `hasNonClaude` checks (`init_mas_state.go:155`, `setup.go:391`) with catalog-declared dependencies and setup-instruction flags, read by both commands. Keep behavioural parsers in code.
- **Risk:** Low — both outputs are pinned by the A7 oracle. The existing `cursor-acp` divergence must be kept or ruled on, not silently unified.
- **Impact:** Removes a live divergence; first step of the [provider catalog boundary](architectural-issues.md#provider-support-crosses-the-catalog-boundary) direction.
- **Depends on:** A human ruling on whether `setup` should include `cursor-acp`.

### P2: Medium Impact / Medium Risk

#### 2.1 Consolidate Repository-Intelligence Refresh Ownership
- **Registry:** [Worktree intelligence refresh](architectural-issues.md#worktree-intelligence-refresh-has-multiple-owners) (a run `wont` item).
- **What:** One `ops` owner taking trigger, enablement and failure-tolerance inputs, called by claim (`claim_task_worktree.go:59`), submission (`submit_review.go:117–130`) and the reviewer check (`agent/worktree_check.go:172–188`).
- **Risk:** Medium — callers differ in failure tolerance.
- **Depends on:** Tests pinning each caller's policy.

#### 2.2 Restore Python Utility Test and Structure Parity
- **Registry:** [Python quality parity](architectural-issues.md#python-skill-utilities-lack-quality-parity).
- **What:** Tests for `context-corpus-index.py` and `discover_targets.py`; separate parsing, aggregation and rendering in `analyze-log.py` (2,158) when extending it.
- **Risk:** Medium around CLI and serialized output.
- **Depends on:** 1.6 for CI execution; test authoring can start independently.

#### 2.3 Restore a Reproducible Coverage Artifact
- **Registry:** [Coverage basis](architectural-issues.md#coverage-basis-is-not-reproducible-by-repository-tooling).
- **What:** A non-interactive coverage target on the existing `-coverpkg=./...` basis that CI publishes. Choose a non-regression policy only after a baseline exists.
- **Risk:** Medium — CI time, and thresholds can reward gaming.
- **Impact:** CI currently has no coverage artifact at all, since the Codecov step was removed.
- **Depends on:** 1.6.

#### 2.4 Finish the Architecture Overview
- **What:** Bring the System Components diagram and provider note (`overview.md:3–43`) in line with the configured 13 roles and nine providers, or label them as a simplified illustration.
- **Risk:** Low.
- **Depends on:** Canonical pipeline and catalog data.

#### 2.5 Centralize Inspect Output Dispatch (carried)
- **What:** Share dispatch across the 13 format-switch sites where behaviour is equivalent; preserve list/scalar restrictions.
- **Risk:** Medium — CLI compatibility.
- **Depends on:** Oracle CLI goldens.

#### 2.6 Review Direct Dependency Compatibility (carried)
- **What:** Recheck upstream releases and advisories before changing pins.
- **Risk:** Medium for actual updates.
- **Depends on:** Fresh external evidence.

#### 2.7 Partition TUI Growth — completed
Largest TUI file is 412 LOC.

#### 2.8 Declare the Sliced-Integration Capability Instead of Fingerprinting Names
- **Registry:** [Sliced-integration name fingerprint](architectural-issues.md#sliced-integration-capability-detected-by-role-pair-names).
- **What:** Replace the name match in `resolver_sliced.go:31–41,62,73` with a capability declared in pipeline YAML (or derived from role types and transition shape), preserving the upgrade-required guidance.
- **Risk:** Medium — affects existing workspaces' capability evaluation; needs a migration or default.
- **Impact:** Custom topologies can opt in; domain code stops hardcoding YAML identities.
- **Depends on:** An ADR, since the pipeline schema changes.

#### 2.9 Raise Protection of Tool-Result Redaction and Usage Reporting
- **What:** Behavioural tests for `toolresult/redact.go` (654) against representative secrets and formats; split `usage/report.go` (765) by aggregation vs rendering when next extended.
- **Risk:** Medium.
- **Impact:** Brings a security-relevant path, now tested at about 1:1, up to core-runtime protection.

### P3: Strategic / Long-Term

#### 3.1 Add Targeted Fuzz Tests (carried)
- **What:** YAML pipeline/state parsing and graph inputs.
- **Risk:** Medium harness investment.
- **Depends on:** Explicit invariants and bounded inputs.

#### 3.2 Automate Spec and Overview Drift Signals (carried)
- **What:** Validate enumerable role/provider facts in `overview.md` against canonical configuration.
- **Risk:** Medium ownership cost.
- **Impact:** The same drift has now survived three assessments.
- **Depends on:** 2.4.

#### 3.3 Track Release Binary Size (carried)
- **What:** Record comparable release sizes before setting a budget.
- **Risk:** Low.

#### 3.4 Guard `ops` Concern Families Without Sub-Packages
- **What:** If review shows cross-family helper calls growing, add an archguard-style check over `ops` file families (which families may call which private helpers), using the existing group file lists. Respect ADR-0222/0223/0225 rather than reopening sub-packaging.
- **Risk:** Medium — the call-graph analysis must stay cheap and deterministic.
- **Depends on:** Evidence of drift; not justified by size alone.

#### 3.5 Retention Policy for Run Artifacts
- **What:** Decide how long oracle fixtures (274.8k lines), `specs/plans/` and `specs/arch-plan/` (43.7k) stay in the durable tree after the merge, and how `TECH_DEBT.md` (1,423 lines) is pruned.
- **Risk:** Low.
- **Impact:** Keeps agent search and context budgets for durable material.
- **Depends on:** The merge to `main`; ties to the registry's spec-corpus lifecycle issue.

## Summary

The consolidation turned the previous report's structural recommendations into executable constraints, under an oracle that makes behaviour preservation checkable. That is why the grade rises to A-. The remaining gap is breadth of enforcement: the static, duplication and Python gates agents are asked to satisfy locally do not run at merge, and no coverage evidence exists. The two design-level residues, provider extension and `ops` concern boundaries, need mechanisms rather than more splitting. With the run closed and the structure documented, the next investment is merge-time enforcement (1.6, 2.3), ahead of further decomposition.

**Overall Rating: A- (raised from B+; the deduction from A is for CI enforcing only part of the local quality policy, with no coverage signal, and for provider and `ops` concern boundaries that rely on discipline rather than mechanism)**

## Validation and Reproduction Notes

- Trees: `git archive 73a50ca8 | tar -x`, likewise `96c9ad69` and `HEAD`. The 2026-09-06 column reproduces the previous report exactly (312 / 76,738 prod; 308 / 188,254 test; 14 / 1,610 support; 3,452 tests; 43 files >500; 24 functions >150), confirming unchanged definitions.
- Function spans: `go/parser` `FuncDecl` with a body, declaration line to closing brace, excluding `_test.go`, `testdata/` and `internal/testhelpers/`. This scope is wider than S1's (it includes `scripts/` and `promptbench`).
- Guards: `go test -count=1 ./internal/testguard/...` → 157 passed, 3 packages. Ratchet state read from `internal/testguard/archguard/area_a*_test.go`.
- Revision ranges: `73a50ca8..96c9ad69` = 409; `96c9ad69..dcbfdd04` = 690; `73a50ca8..ca8d76a3` = 1,103; `main..ca8d76a3` = 699. `git diff --name-only dcbfdd04..ca8d76a3` lists only `.md` and `.json` files.
- Inbound anchors: `git grep 'code_quality_assessment.md#'` finds four (registry `#16-…`, `#17-…`, `#22-…`; architecture review `#repository-metrics-dashboard`); all resolve in this version.
- Workflow recipes establish configured checks, not successful remote runs.
- The working-tree change to `stacklit-insights.json` and the untracked `decision-data-model-operator-notes.md` and `dev760-tooling-defect-inventory.md` were excluded and left untouched.
