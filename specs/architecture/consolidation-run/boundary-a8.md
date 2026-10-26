# Architecture Consolidation Run: Area Boundary Record A8 (Prompts)

Evidence of the A8 area-boundary checkpoint (`Area-Boundary Checkpoint Contract`, checks G1–G8), from the arm-1-ar-20
plan, Scope 1, Task 1, task `arm-1-ar-20-cp-0-code-0`. One record per boundary; later units never edit it. Beyond this
record the unit changes `architectural-issues.md` (the one closure of G6) and the acceptance manifest. It changes no
production, test, guard-data, oracle, baseline, Makefile or ADR file. `tdd_not_required`: evidence-only unit that moves
no code.

## Observed revision

- Configured integration branch: `global-refactoring` (D23; `baseline.md`). `omni-ee get config --json` reports
  `IntegrationBranch` = `"global-refactoring"`. `git rev-parse global-refactoring` and `git rev-parse HEAD` of the task
  worktree both print the revision below; `git log HEAD..global-refactoring` printed nothing at the start
  (2026-10-26T18:31Z) and `git rev-parse global-refactoring` still printed R1 at 2026-10-26T18:53Z, after all
  measurements.
- **Checked revision (R1):** `73d9f9ac82491471c45b99b835d57921d89903f5`
  (`refactor(agent): name the remaining A8 policy thresholds and extend the K7 inventory`, the recorded merge commit of
  `arm-1-ar-19-cp-0-code-2`). Every observation below was made at R1. No other integration revision is recorded.
- Observation window (UTC), task branch `task/arm-1-ar-20-cp-0-code-0`: 2026-10-26T18:31Z to 18:54Z. The worktree was
  clean (`git status --short` empty) before and after the suites; the only change is the registry edit of G6 and this
  record.
- Starting commit of the run: `96c9ad69aee983ce2b929dd681b6ba43bf920535`. A8 starts after the A7 boundary merge
  `48f601820ec77dd7189e756af331136bb1c6ade2`; A8 diffs below are taken from it.
- A8 coding units and the A9 oracle units merged before this unit (`omni-ee get <id> --json`, status `MERGED`, recorded
  merge commit) — B-sequence:

| Task | Concern | Merge commit |
|---|---|---|
| `arm-1-ar-19-cp-0-code-0` | A7 boundary with C4 (precedes A8 coding) | `48f601820ec77dd7189e756af331136bb1c6ade2` |
| `arm-1-ar-19-cp-0-code-1` | resolved orchestrator inputs, shared records to `models`, `prompts` drops `ops` | `10408d4a3f8d7af8293bb5330cbc2192ea808942` |
| `arm-1-ar-19-cp-0-code-2` | task and reference assembly by concern, A8 K7 inventory, A8 guard data emptied (ADR 0227) | `73d9f9ac82491471c45b99b835d57921d89903f5` |
| `arm-1-ar-8-cp-0-code-0` | A8 prompt oracle | `59aa84316354b6201b4435746a3d145afea6bd8d` |
| `arm-1-ar-9-cp-0-code-0` | A9 oracle kit and supervisor lifecycle rows | `35f43754b930cf3c6d605b08b32b900ae9a188e7` |
| `arm-1-ar-9-cp-0-code-1` | A9 provider-output signal rows | `ef9219aa9ace25831ca55947eb06be8a511bb624` |
| `arm-1-ar-9-cp-0-code-2` | A9 reviewer and heartbeat rows | `d9e91e3307230606d1f6e44ddbd6a6aaf75b685a` |
| `arm-1-ar-9-cp-0-code-3` | A9 orchestrator rows | `83f0b725ddfca1985efe68dda659461056456f29` |
| `arm-1-ar-9-cp-0-code-4` | A9 launch and spawn-gate rows | `53dbbf2890bac13703314946e4380efe0d53b91f` |

- Environment for every measurement: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` (GUARDRAILS G1.4), plus
  `GOFLAGS=-count=1` for G5 (the Protocol). No remote push, provider CLI, installation or update ran; output below is
  sanitized (no environment values). Host: `Linux 6.8.0-147-generic x86_64`, `nproc` 8, `go1.26.0 linux/amd64`,
  `git version 2.43.0`. Shared host with other agent sessions running.
- Scratch tooling (the `go/parser` size measure, the batch scripts and logs) lives under `/tmp/a8tools/` and
  `/tmp/g4-a8/`, outside the repository; nothing of it is committed.

## G1 — A8 ratchet empty; S2 exceptions data-like with rationale

Source: `internal/testguard/archguard/area_a8_test.go` read at R1:

| Field | Value |
|---|---|
| `Ratchet` | `[]RatchetEntry{}` (empty) |
| `S2` | `[]S2Entry{}` (empty): no S2 entry exists, so no category or human ruling is to be checked |
| `Allowlist` | `[]AllowEntry{}` (empty) |
| `Layers` | two entries: `internal/prompts` (`layerPrompts`) and `internal/prompts/rolebudget` (`layerTestSupport`) |

The start state was four keys (1 file, 3 functions; `baseline.md`, `a8-size`): `internal/agent/prompt.go` (1,401
lines), `buildTaskRoleContextData` (179), `buildReferenceContextWithRepository` (223) and
`internal/prompts/builder.go` `renderOrchestratorDashboard` (176). All are gone. This is recorded apart from the C0 result
because the guard tolerates debt: the delivered closure assertion `TestAreaA8DataIsEmpty` (the plan's prospective name)
fails on any remaining ratchet key, allowlist entry or S2 entry. `go test -count=1 -v -run
'TestAreaA8DataIsEmpty|TestS1SizeGuard|TestK4ImportDirectionGuard' ./internal/testguard/archguard/` printed `--- PASS:
TestAreaA8DataIsEmpty`, `--- PASS: TestK4ImportDirectionGuard` and `--- PASS: TestS1SizeGuard`, then `ok …/archguard
0.119s`. The assertion is present at R1, so it is no longer prospective.

Independent measurement with `go/parser` (declaration line to closing brace of every `FuncDecl` with a body; physical
lines) over the A8 production files at R1: `internal/prompts/*.go` and `internal/prompts/rolebudget/rolebudget.go`
(7 files, `promptbench` excluded) plus the A8 files of `internal/agent` (`prompt.go`, `prompt_planner_context.go`,
`reference_context.go`, `prompt_corrective_context.go` and the five files the A8 units added:
`orchestrator_dashboard.go`, `prompt_integration.go`, `prompt_intelligence.go`, `prompt_task_context.go`,
`prompt_task_graph.go`): 16 files, 4,642 lines, 160 functions, 0 files over 800, 0 functions over 100. Longest file
`prompt_planner_context.go` 554 lines; longest function `populateSliceIntegrationContext` (`prompt_integration.go`) 94
lines. The four start keys now measure: `agent/prompt.go` 321 lines (1,401), `buildTaskRoleContextData` 32 (179),
`buildReferenceContextWithRepository` 33 (223), `renderOrchestratorDashboard` 26 (176); `prompts/builder.go` is 444 lines
(581 at the start). The figures equal those ADR 0227 reports. Limits are 800 and 100. G1 holds: nothing remains as an S1
offender, so no S2 conversion was needed and no S2 category or human ruling is to be checked.

## G2 — A8 allowlist holds only rationale-carrying entries; the A8-owned crossing is resolved

`Allowlist` is empty at R1 (table above), so no entry lacks a rationale and no stale entry exists. `crossings` assigns A8
one SI-5 crossing, `prompts → ops`:

- `git grep -n 'internal/ops' -- internal/prompts ':!*_test.go'`: no output, exit 1.
- `go list -deps ./internal/prompts | grep -c 'internal/ops'`: `0`.

The other three crossings are owned elsewhere: `jsonout → ops` by A7 (resolved at the A7 boundary), `commands → agent` and
`tui → agent` by A9 (Scope 20), `agent → db` by C6 (Scope 21). A8 added no production package: its two layer entries are
the two packages of the area partition (the new `internal/agent` and `internal/models` files belong to existing
packages). The K4 import-direction check ran inside `make lint` (`lint-archguard`, `ok …/internal/testguard/archguard
0.133s`). G2 holds.

## G3 — A8 Shim Register empty; no alias or forwarder added

The register is empty (arm-1-ar-19 plan, `Shim Register`). Input: the A8 ADR's audit
(`specs/architecture/ADR/0227-a8-resolved-prompt-inputs-and-assembly-concerns.md`, "Shim and exported-API audit"; it
diffs from `71d768f46`, the oracle-only commit), verified independently over the wider range from the A7 boundary merge:

| Check | Result at R1 |
|---|---|
| Added `type X = Y` lines in `git diff 48f601820 HEAD -U0 -- cmd internal ':!*_test.go'` | 0 |
| Declarations of the six moved records (`IntegrationProgressDecision`, `IntegrationScopeCoverage`, `IntegrationAnalysisRequest`, `IntegrationProgressReason`, `ReconcileIntegrationAnalysesResult`, `PlanHandoffClass`) left in `internal/ops` (`git grep -nE '^(type|func|const|var)\s+<Name>\b'`) | 0 each; they live in `internal/models` (`integration_progress.go`, `plan_handoff_class.go`) |
| `testFilePatterns` template function | gone from `internal/prompts/templates.go` and `builder.go` (0 hits); the matcher text is the `TestFilePatterns` field and the `testFilePatterns string` parameter of `RenderWakeInstructions` |
| Files added by the A8 range in non-test `cmd` and `internal` (excluding oracle) | 9: `agent/orchestrator_dashboard.go`, `agent/prompt_integration.go`, `agent/prompt_intelligence.go`, `agent/prompt_task_context.go`, `agent/prompt_task_graph.go`, `models/integration_progress.go`, `models/plan_handoff_class.go`, `prompts/orchestrator_input.go`, `prompts/templates/orchestrator_commands.tmpl`; each holds a whole moved concern or a new record, read in G1/G2 |
| Forwarders | `agent.RenderOrchestratorDashboard` and `renderOrchestratorDashboard*` are the two D1 entries that resolve inputs and call `prompts`; they do not forward to `ops` |

The diff over 39 production files adds 2,405 and removes 1,960 lines; none is an alias, forwarder or shim marker. G3 holds:
zero shims added.

## G4 — suite green at the integration revision

All commands run at R1 in the canonical form `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp <command>`, once each and not
retried, sequentially from the task worktree (`make test` and the rest ran with the worktree as working directory; no
translation of the canonical commands). The operator note of 2026-10-25T11:20Z tells coders not to pre-run the canonical
list before submitting; here the run is this unit's deliverable (boundary evidence at the integration SHA, plan Task 1
step 4), and submission re-runs the list on the rebased commit.

| Cmd | Command | Start–End (UTC) | Exit | Excerpt |
|---|---|---|---|---|
| C0 | `go test -count=1 ./internal/testguard/archguard/` | 18:32:12–18:32:13 | 0 | `ok …/internal/testguard/archguard 0.121s` |
| C1 | `make test` | 18:32:13–18:36:32 | 0 | 84 `ok` lines, 0 `FAIL`, 0 `(cached)`; `internal/agent` 196.624s, `internal/ops` 197.853s, `internal/oracle/a8` 5.499s, `internal/prompts` 0.439s, `internal/prompts/promptbench` 1.055s, `internal/prompts/rolebudget` 0.158s, `internal/testguard/archguard` 0.203s; last line `ok …/scripts/ci/testshard 0.153s` |
| C2 | `make lint` | 18:36:32–18:37:20 | 0 | `go vet ./...` for `GOOS` linux, windows and darwin; `ok …/internal/testguard/archguard 0.133s` (`lint-archguard`) |
| C3 | `make test-e2e` | 18:37:20–18:38:55 | 0 | `go test -race -tags e2e -run '^(TestFullSprintSequence|TestTerminalDependencyRecovery)$' ./internal/integration/`: `ok …/internal/integration 80.055s` |
| C4 | `go test -race -timeout 30m -p 2 ./internal/prompts/... ./internal/agent/` | 18:38:55–18:43:02 | 0 | `ok` for `internal/prompts` 2.450s, `internal/prompts/promptbench` 7.399s, `internal/prompts/rolebudget` 1.368s, `internal/agent` 231.138s |
| C5 | `go test -count=1 -run '^TestRoleBudget$' ./internal/prompts/rolebudget/` | 18:43:02 | 0 | `ok …/internal/prompts/rolebudget 0.052s` (the test file defines `TestRoleBudget`; the baseline is read, not rewritten) |
| C6 | `go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a8/...` | 18:43:02–18:43:10 | 0 | 41 lines `ok internal/oracle/a8/testdata/golden/prompt/<name>.txt H1` (T01…, `stateless_*`, `branded_*`), no line of another kind |

Task validation indices 0–6 are C0, C1, C2, C3, C4, C5 and C6; the plan's G4 mapping to the Makefile `test` target names C0,
C2, C3, C4, C5 and C6 as further requirements, all met above. The A8 oracle (`internal/oracle/a8`) ran live inside C1
with every golden unchanged (this unit changes no oracle file), and C6 reproduced the goldens from the starting
baseline. Both the warm-up and the timed `make test` of G5 at the same revision are further green runs of C1. Observation on
Git transcripts (operator note 2026-10-25T23:31Z): this unit freezes no Git output. G4 holds: no red suite is recorded
as green.

## G5 — wall time and benchmarks against `baseline.md` (indicative; never blocks)

Protocol repeated from `baseline.md`: fresh disposable clone outside the worktrees (`/tmp/a8g5clone.Z5QJjI/repo`:
`git clone --local --no-checkout <project root>` then `git checkout --detach 73d9f9ac8…`; clone `HEAD` printed
`73d9f9ac82491471c45b99b835d57921d89903f5`), sync-clean check (`make sync-embedded` exit 0, `Files synced
successfully`, `git status --short` empty, 0 bytes), then one warm-up `make test`, one timed `make test`, then
`timeout 300 go test -run '^$' -bench . -benchmem ./internal/<pkg>` under `env -i HOME="$HOME" PATH="$PATH"
TMPDIR=/tmp GOFLAGS=-count=1`. No retry. The clone's `git status --short` was empty afterwards and the clone was deleted
(`ls -d /tmp/a8g5clone.*` matches nothing).

- Host load average (1, 5, 15 min, `/proc/loadavg`): before warm-up `1.15 2.54 3.32`; before timed run `6.57 6.11 4.78`;
  after timed run `3.70 6.05 5.10`. The baseline ran at about 30 on 8 cores, so the comparison is load-confounded in this
  run's favour.

| Run | Start (UTC) | End (UTC) | Wall s | Exit | Output |
|---|---|---|---|---|---|
| Warm-up | 18:44:04 | 18:47:49 | 225 | 0 | 84 `ok`, no `FAIL`, no `(cached)` |
| Timed | 18:47:49 | 18:50:55 | 186 | 0 | 84 `ok`, no `FAIL`, no `(cached)` |

N4: the baseline timed run was 523 s, red (two `internal/agent` deadline tests) and taken under load about 30; +25% is
653.75 s. 186 s is 64% below it and inside the bound; against the green 444 s warm-up (a separate observation) the timed
run is 58% below and the warm-up 49% below. **Disposition:** within bound, recorded with the baseline's load caveat, since
both numbers favour this run. Per-package times, warm-up and timed: `cmd/liza` 111.9 s and 93.6 s, `internal/commands`
51.7 s and 43.4 s, `internal/ops` 185.7 s and 175.3 s, `internal/agent` 194.6 s and 184.6 s, `internal/oracle/a8` 6.7 s and
4.9 s.

Benchmarks: all three packages exited 0 with `PASS` (`internal/db` 42.409s, `internal/commands` 3.460s, `internal/agent`
3.044s); all 18 benchmark rows of the baseline were captured, none "not captured".

| Benchmark | ns/op base → now | B/op, allocs/op |
|---|---|---|
| db `BlackboardWrite` | 12033415 → 1829999 (−84.8%) | same |
| db `BlackboardModify` | 22577288 → 2026609 (−91.0%) | B/op +0.6%, allocs +0.7% |
| db `BlackboardReadCached` | 44938 → 17735 (−60.5%) | same |
| db `BlackboardReadUncached` | 1890776 → 181540 (−90.4%) | same |
| db `MarshalStateForWriteLarge/repaired=false` | 536001675 → 118822149 (−77.8%) | same |
| db `MarshalStateForWriteLarge/repaired=true` | 1331986515 → 121787798 (−90.9%) | same |
| db `ReadCachedHit` | 4916749 → 1064249 (−78.4%) | same |
| db `StateParse` | 112041835 → 26586157 (−76.3%) | same |
| db `TerminalArchiveTransaction/inline/cold-instance` | 772451364 → 249391945 (−67.7%) | same |
| db `TerminalArchiveTransaction/inline/warm` | 628528774 → 246549288 (−60.8%) | same |
| db `TerminalArchiveTransaction/archived/cold-instance` | 320733652 → 144407315 (−55.0%) | same |
| db `TerminalArchiveTransaction/archived/warm` | 185103984 → 24098143 (−87.0%) | B/op −9.9%, allocs −18.9% |
| db `TerminalArchiveTransaction/archived-256/cold-instance` | 990744276 → 642335585 (−35.2%) | same |
| db `TerminalArchiveTransaction/archived-256/warm` | 319924638 → 91139922 (−71.5%) | B/op −3.0%, allocs −5.9% |
| commands `InspectionSnapshotDuringHeartbeat/status` | 9380378 → 8177921 (−12.8%) | B/op +12.2%, allocs +6.5%; heartbeat-writes 6 → 36 |
| commands `InspectionSnapshotDuringHeartbeat/get` | 4376937 → 2744715 (−37.3%) | B/op +5.9%, allocs +5.1%; heartbeat-writes 13 → 29 |
| agent `BenchmarkClaimReviewerTask` | 34512825 → 4393013 (−87.3%) | same |
| agent `BenchmarkResumeHandoff` | 5936738 → 2123520 (−64.2%) | same |

"same" means within ±0.5%. Every ns/op figure is below the baseline, so no row is out of the +25% bound. **Dispositions**
(N3 is Should; none blocks):

- **`InspectionSnapshotDuringHeartbeat/status`** and **`/get`** B/op and allocs/op (+12.2% and +6.5%; +5.9% and +5.1%):
  the only rows above the baseline, in a package A8 does not touch (`internal/commands`, A7). It is the observation the
  A3 to A7 boundaries carried (`boundary-a7.md` G5: same-host A/B of the starting commit against the boundary revision
  showed B/op equal within about 1%, the +12.2% already present at the starting commit, since the benchmark overlaps a
  concurrent heartbeat writer and does 6 against 36 heartbeat writes per window, so work per iteration is not
  like-for-like). A8 changes neither `commands`, `db` nor the heartbeat path (the A8 range touches `internal/prompts`,
  `internal/agent` prompt files and `internal/models` records). **Disposition:** not attributable to A8; no correction
  commissioned, no new A/B run.
- **db `BlackboardModify`** B/op +0.6%, allocs +0.7%: the same figures as the A5 to A7 boundaries, within noise of the
  benchmark's own setup, in `internal/db` (A1). No action.
- **A8 paths.** The baseline set holds no benchmark of `internal/prompts` or the prompt-assembly functions; the agent
  benchmarks cover claiming and handoff only. Prompt rendering is covered by the A8 oracle and role-budget gate (C1, C5,
  C6) and by `promptbench` in the race run; no A8-specific timing is claimed beyond the `internal/prompts` and
  `internal/oracle/a8` test times above.
- The other rows are decreases or unchanged: no action.

## G6 — architectural issues resolved by A8

Reviewed the Open Issues Summary (63 open at R1 before this unit's edit: 17 high, 30 medium, 16 low;
`specs/architecture/architectural-issues.md`, counted from the table rows) under `issues-policy` (the file's Update
Policy) and the ADR 0227 closure evidence ("Closure evidence for the A8 boundary"). The active sections (everything above
**Completed Fixes**) were re-grepped at R1 for the 19 A8 production file names (`internal/prompts/*.go`, `rolebudget.go`
and the agent files above), for `internal/prompts` and for `internal/agent/prompt`/`reference_context`: the hits are the
**Issue** text of "Prompts Layer Imports Business Logic" (closed below), the `promptbench` mention in the
reference-first-contract context entry, the template paths `internal/prompts/templates/blocks/*.tmpl` in "Custom Roles
Render Empty Prompt Blocks" (template content, untouched by A8 role chains) and
`internal/agent/prompt.go:252,265,278,143-145,355-357` in "Index Discovery Failures Are Unobservable".

**Closed (1), wholly resolved by merged work, with the registry synchronised (active section removed, table of contents
and Open Issues Summary row removed, Completed Fixes, Fixed (Traceability) and Fix Details added, Counts line updated to
17 high, 30 medium, 15 low, 62 total):**

| Issue | Resolving commits | Evidence at R1 |
|---|---|---|
| Prompts Layer Imports Business Logic (`issue-prompts`) | `492ddace7` (shared records to `models`), `fd9598786` (orchestrator rendered from runtime-resolved inputs; matcher text and template function removed; allowlist entry removed), `bf33dc001` (assembly concerns; A8 guard data emptied) | G2: `git grep` of non-test `internal/prompts` for `internal/ops` prints nothing and `go list -deps ./internal/prompts` names no `internal/ops`; G1: `TestAreaA8DataIsEmpty` passes |

Why "wholly resolved": the issue's stated scope is the `prompts → ops` dependency, whose six named queries
(`LoadDetectionContext`, `ManyToOneTransitionInfo`, `IsPlanningPair`, `IsTransitionCycleBlocked`,
`CountReadyManyToOneCohorts`, `IsPlanningCompleteEligible`) are now resolved in `internal/agent`
(`orchestrator_dashboard.go`) and handed to `prompts` as data — the first Future option, caller resolution. The queries
remain in `ops` as read-only functions. Its Implication (prompt assembly coupled to ops, `prompts` hard to test in
isolation) no longer holds: `prompts` has no `ops` import and the A8 import-direction data carries no allowlist entry.

**Not closed, with reasons:**

- "No Query Layer" (`issue-query`): stays open. Caller resolution was chosen and no query package or `models`
  diagnostics migration exists; the `commands` and `agent` read paths are unchanged (ADR 0227).
- "Custom Roles Render Empty Prompt Blocks" (`issue-custom-roles`): stays open; custom-role prompt behaviour is outside
  the run (`goal-wont`) and the template role chains are untouched by A8 (ADR-0045).
- "Control-Flow Vocabulary Bypasses Domain Ownership" (`issue-vocabulary`): stays open. A8 typed and named its own wake,
  integration-view and threshold vocabulary (ADR 0227, K7 occurrence table), but the issue spans every area, and A9 and
  the later C scopes are not yet refactored.
- "Index Discovery Failures Are Unobservable": stays open. A8 moved `availablePromptScipIndexRefs`,
  `availablePromptStacklitIndexRefs` and `availablePromptFunctionalClusterIndexRefs` to
  `internal/agent/prompt_intelligence.go` and kept their behaviour: each still discards the discovery error and returns
  `nil` (read at R1), so the defect — invisibility — is unchanged. The entry's cited locations (`prompt.go:252, :265,
  :278`, `:143-145`, `:355-357`) are stale after the move; this is recorded drift, left uncorrected because this unit
  edits the registry for closures only.
- "Commands Layer Imports Agent Runtime" and "Provider Stop-Signal Recovery Has Multiple Owners": A9 items (Scope 20);
  no A9 issue is touched. "Decompose supervisor.go (1,129 LOC)" likewise belongs to A9.
- The other open entries name no A8 file as their subject: no closure and no edit.

## G7 — record completeness

This file holds the commands or source reads, the integration SHA R1 `73d9f9ac82491471c45b99b835d57921d89903f5`, the UTC
observations, exits and excerpts for G1–G6, the G5 dispositions, the G6 reasons and the G8 section below. G1–G7 are
committed with G8 pending, as `d52` and the plan require. The acceptance manifest
(`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-20-cp-0-boundary.json`) is written once G8 is
green (it maps G1–G8 and the boundary obligations; it records no result itself).

## G8 — dispatched CI result (D52): pending

Status: **pending.** Under D52 the maintainer dispatches `.github/workflows/ci.yml` (`workflow_dispatch`) on a pushed ref
whose tested SHA equals R1 (`73d9f9ac82491471c45b99b835d57921d89903f5`). Green Linux, Windows and macOS jobs are required.
No agent pushes, opens a pull request or runs a remote write. The run URL, tested SHA, each job's name and conclusion and
the UTC observation are recorded here on authorized resume; G8 is not claimed before the jobs complete.
