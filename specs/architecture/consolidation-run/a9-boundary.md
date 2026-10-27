# Architecture Consolidation Run: Area Boundary Record A9 (Agent runtime)

Evidence of the A9 area-boundary checkpoint (`Area-Boundary Checkpoint Contract`, checks G1–G8), from the arm-1-ar-25
plan, Scope 1, Task 1, task `arm-1-ar-25-cp-0-code-0`. A9 is closed together with its C chain (C6, C1, C2, C3). One
record per boundary; later units never edit it. Beyond this record the unit changes `architectural-issues.md` (the
closures of G6) and the acceptance manifest
(`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-25-cp-0-boundary.json`). It changes no production,
test, guard-data, oracle, baseline, Makefile, ADR or overview file and repairs nothing in A9. `tdd_not_required`:
evidence-only unit that moves no code.

## Observed revision

- Configured integration branch: `global-refactoring` (D23; `baseline.md`).
- **Recorded integration SHA (R1):** `7a44042ba68f09011d6bc92a77ea716c105f029a`
  (`docs(acceptance): map the A9 Windows portability correction obligations to proofs`, the recorded merge commit of
  `d53-d56-a9-windows-portability-plan-code-0`). R1 is the SHA the D52 run of G8 tested. G5 and the A/B benchmark
  observations were made in disposable clones checked out at R1 (or the older commits named there); the other
  observations were made in the task worktree at the submission base below.
- **Task worktree HEAD at the start (2026-10-27T11:40Z):** `dcbfdd044163b8bf214ef5e89f5001c819270014`, equal to
  `git rev-parse global-refactoring`; `git log HEAD..global-refactoring` printed nothing at the start and again at
  2026-10-27T12:01Z, after all measurements. It is R1 plus the plan amendment `arm-1-ar-25-cp-0-amend-1`:
  `git diff --name-only 7a44042ba HEAD` lists exactly
  `specs/plans/.../20261027-112419-arm-1-ar-25-cp-0-amend-1.md` and `...-amend-1-output.json`, and
  `git diff --quiet 7a44042ba HEAD -- . ':!specs/plans'` exited 0. The production, test, guard-data, oracle, Makefile and
  documentation trees checked below are therefore identical to R1, and none of the evidence needed refreshing.
- **Submission base** of this unit: that HEAD plus the evidence commit of this record, the registry edit and the manifest. It is an
  evidence-only revision and is not claimed as CI-tested; the proven SHA is R1 alone. If integration moves
  before submission (rebase), the affected evidence is refreshed at the new SHA and both labels are kept.
- Observation window (UTC): 2026-10-27T11:40Z to 12:10Z. The worktree was clean (`git status --short` empty) before and
  after the G4 suites.
- Starting commit of the run: `96c9ad69aee983ce2b929dd681b6ba43bf920535`. A9 starts at the A8 boundary merge
  `3b9ba5d94921a9875def712f037cfc151b9d3fe5`; A9 diffs below are taken from it.
- Environment: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` for every command (GUARDRAILS G1.4), plus `GOFLAGS=-count=1`
  for G5 (the Protocol). No push, provider CLI, installation or update ran; no live `.liza/` state was read; output is
  sanitized (no environment values). Host: `Linux 6.8.0-147-generic x86_64`, `nproc` 8, `go1.26.0 linux/amd64`,
  `git version 2.43.0`, shared with other agent sessions. Scratch tooling and logs live under `/tmp/a9tools/`,
  `/tmp/g4-a9/` and `/tmp/g5-a9/`, outside the repository; nothing of it is committed.

### Sequence (B-sequence)

Every A9 and C coding unit and the Windows correction merged before R1; `git merge-base --is-ancestor <merge> 7a44042ba`
exited 0 for each (`omni-ee get <id> --json`, status `MERGED`, recorded merge commit).

| Task | Concern | Merge commit |
|---|---|---|
| `arm-1-ar-9-cp-0-code-0` … `code-4` | A9 oracle kit and rows | `35f43754b930cf3c6d605b08b32b900ae9a188e7`, `ef9219aa9ace25831ca55947eb06be8a511bb624`, `d9e91e3307230606d1f6e44ddbd6a6aaf75b685a`, `83f0b725ddfca1985efe68dda659461056456f29`, `53dbbf2890bac13703314946e4380efe0d53b91f` |
| `arm-1-ar-20-cp-0-code-0` | A8 boundary (precedes A9 coding) | `3b9ba5d94921a9875def712f037cfc151b9d3fe5` |
| `arm-1-ar-20-cp-0-code-1` | `agentlaunch` and `providersignal` extraction | `89428dafc81fa95ccf4fcfaa4f5c61269088bf68` |
| `arm-1-ar-20-cp-0-code-2` | CLI and ACP backend run concerns | `a333b02fe98fa0dbcf736b633fe43407144f3340` |
| `arm-1-ar-20-cp-0-code-3` | supervisor lifetime, turn, session, orchestrator verification | `80b3323f5891f7c0644de58d334ac64d4c381d27` |
| `arm-1-ar-20-cp-0-code-4` | claim, recovery, wait, restart concerns; A9 vocabulary and closure (ADR 0228) | `75d4084d49259947d00093a0927350ebc4bb93cc` |
| `arm-1-ar-21-cp-0-code-0` | C6 registration and exit ownership | `8f10c7d73258cc1fccf6ee55487372f5628050da` |
| `arm-1-ar-21-cp-0-code-1` | C6 worktree recovery, bookkeeping | `5cf35b32626cb6c68fb6330e9eee72263eecba68` |
| `arm-1-ar-21-cp-0-code-2` | C6 presentation writes and the mutation guard | `2f5944f0978258bc28a24009679b8f7787fd15e8` |
| `arm-1-ar-22-cp-0-code-0` | C1 configured lease duration | `8124f434b053bd497133f97f87982d7ce8bf2abb` |
| `arm-1-ar-23-cp-0-code-0` | C2 canonical provider keys | `e63a111b1975471d998416b642e0ead262a83fc5` |
| `arm-1-ar-24-cp-0-code-0` | C3 canonical provider-unavailable signals | `225ed0af8c7734e67e83baadebc16817afed8e02` |
| `arm-1-ar-0-cp-2-d56-c5-provenance-code-0` | D24/D56 correction (C5 change point) | `769f8f2a38d26f394030d8241473e9c130396295` |
| `d53-d56-a9-windows-portability-plan-code-0` | Windows correction of the first red G8 (C6 cache, PB-3 outbox) | `7a44042ba68f09011d6bc92a77ea716c105f029a` (R1) |

## G1 — A9 ratchet empty; S2 exceptions data-like with rationale

Source: `internal/testguard/archguard/area_a9_test.go` read at the submission base (tree equal to R1):

| Field | Value |
|---|---|
| `Ratchet` | `[]RatchetEntry{}` (empty) |
| `S2` | `[]S2Entry{}` (empty): no S2 entry exists, so no category or human ruling is to be checked |
| `Allowlist` | three entries, read in G2 |
| `Layers` | four entries: `internal/agent` (`layerRuntime`), `internal/agentlaunch` (`layerInfrastructure`), `internal/process` (`layerRuntime`), `internal/providersignal` (`layerInfrastructure`) |

The start state was one file key and twelve function keys (`baseline.md`; ADR 0228): `internal/agent/systemctl.go` (861
lines) and the twelve functions in the table below. The ratchet is recorded apart from the C0 result because the guard
tolerates debt: `go test -count=1 -v -run 'TestAreaA9RatchetIsEmpty|TestS1SizeGuard|TestK4ImportDirectionGuard|TestC6MutationGuard'
./internal/testguard/archguard/` (exit 0) printed `--- PASS: TestAreaA9RatchetIsEmpty`, `--- PASS: TestC6MutationGuard`,
`--- PASS: TestK4ImportDirectionGuard` and `--- PASS: TestS1SizeGuard`, then `ok …/archguard 1.516s`.
`TestAreaA9RatchetIsEmpty` fails on any remaining ratchet key or S2 entry.

Independent measurement with `go/parser` (declaration line to closing brace of every `FuncDecl` with a body; physical
lines), over the production files of the four A9 packages `internal/agent`, `internal/process`, `internal/agentlaunch` and
`internal/providersignal` (76 files, 15,713 lines, 642 functions, `_test.go` excluded): 0 files over 800 lines and 0
functions over 100 lines. Excluding the nine A8-owned prompt-assembly files of `internal/agent` (A8 boundary), the A9 share
is 67 files, 12,845 lines, 532 functions, also 0 over either limit. Longest A9 files: `agent/strategy_reviewer.go` 642,
`agentlaunch/launch_resolver.go` 622, `agent/claiming.go` 533; longest A9 functions: `CLIAgent.RunInteractive` 93,
`detectOrchestratorWakeTriggers` 84, `runExecutionProgressWatchdog` 82. The start offenders now measure (limits are 800 and
100):

| Start key | Start | Now (owner file at R1) |
|---|---|---|
| `systemctl.go` (file) | 861 | 57 |
| `ResolveLaunchPlan` | 143 | 56 (`agentlaunch/launch_resolver.go`) |
| `CLIAgent.Run` | 178 | 28 (`agent/cli_agent.go`) |
| `CLIAgent.buildRunCommand` | 148 | 43 (`agent/cli_agent_command.go`) |
| `ACPXAgent.Run` | 141 | 36 (`agent/acpx_agent.go`) |
| `RunSupervisor` | 521 | 29 (`agent/supervisor.go`) |
| `executeAgent` | 207 | 15 (`agent/session_execution.go`) |
| `verifyOrchestratorWakeChanges` | 124 | 40 (`agent/orchestrator_verify.go`) |
| `claimDoerTaskWithOptionalAuthority` | 126 | 17 (`agent/claiming.go`) |
| `handleApprovedMergesWithOptionalAuthority` | 107 | 37 (`agent/claiming_merge.go`) |
| `exit42RestartTracker.Handle` | 111 | 24 (`agent/supervisor_restart.go`) |
| `waitForWorkEventDriven` | 132 | 34 (`agent/waitforwork.go`) |
| `ensureReviewerWorktreeLocked` | 139 | 24 (`agent/worktree_check.go`) |

`supervisor.go` is 198 lines (663 at the starting commit). G1 holds: nothing remains as an S1 offender, so no S2
conversion was needed and no S2 category or human ruling is to be checked.

## G2 — A9 allowlist holds only rationale-carrying entries; the A9-owned crossings are resolved or retained with evidence

`area_a9_test.go` holds three allowlist entries, each with owner and non-empty rationale; `TestK4ImportDirectionGuard`
(inside C0, C1 and C2) fails on a stale or new edge and passed. `crossings` gives A9 three SI-5 crossings, owned by
Scope 20 (`commands → agent`, `tui → agent`) and Scope 21 (`agent → db`). Evidence that each rationale matches the delivered
code at R1 (`git grep` of non-test files; the layer-exempt test-support packages and `cmd/liza` aside):

| Entry | Rationale | Delivered code (R1) |
|---|---|---|
| `internal/commands → internal/agent` (Scope 20) | read-only wake projection, no mutation authority; every launch and provider-signal capability lives in `agentlaunch` and `providersignal` | the only non-test importer of `agent` in `internal/commands` is `status_orchestrator.go`, using `agent.DetectOrchestratorWakeTriggersForProject` (line 29) and `agent.OrchestratorWakeResult` (line 33). At the starting commit `status.go`, `repair_agent_pool.go` and `resume.go` imported it. Retained, with an evidenced read-only rationale |
| `internal/tui → internal/agent` (Scope 20) | snapshot-only pause reason, no waiting, resume or mutation side effect | the only importer in `internal/tui` is `view_agents.go:232`, `agent.RolePauseReason(m.state, …)`. Retained, read-only |
| `internal/agent → internal/db` (Scope 21) | non-mutating use only: snapshot, cached and watch reads; handle construction and acquisition configuration passed to ops; generation-scoped liveness record publication (ADR-0177); every persisted-state mutation in `ops` (C6) | `db.` selectors in non-test `internal/agent`: `db.Blackboard` (84), `db.For` (13), `db.LivenessRecord` (2), `db.WriteLivenessRecord` (1, `heartbeat.go:187`, the liveness record). Blackboard calls on the `bb` receiver: `ReadSnapshot` (23), `ReadCached` (5), `WatchForChanges` (1), `Patient` (1, `registration.go:144`, passed to `ops.UnregisterAgent`), `Read` (3: `claiming.go:447`, `supervisor_block.go:30` and `:72`, plain reads of the same non-mutating family the rationale describes as snapshot reads), and `db.For(…).WithLockRetryContext` (`registration.go:180`, `supervisor_lifetime.go:86`). The regex `\.(Modify|ModifyWithAgentAuthority|Write|UpdateTask|UpdateAgent|ArchiveTerminalTask)\(` over non-test `internal/agent` matches four lines, none a Blackboard: `stdoutLog.Write` (`acpx_agent.go:297`), `alerts.Write` (`checkpoint_summary.go:186`, `index_activation.go:72`) and a writer `Write` (`output.go:89`). `TestC6MutationGuard` passed |

No rationale describes an allowed read or liveness dependency as a mutation exemption: the commands and tui entries
state "no mutation authority"/"no … mutation side effect", and the db entry grants "no write authority". The `Read`
calls are not named in the db rationale's parenthetical list; they are the same non-mutating family, so no entry is stale
or wrong and no edit was made (this unit changes no guard data). The other K3 crossings are owned elsewhere
(`jsonout → ops` by A7, `prompts → ops` by A8) and were resolved at their boundaries. The C6 guard exists at R1: `internal/testguard/archguard/c6_mutation_guard_test.go` defines `TestC6MutationGuard` (line 303), with the
fixture and classification checks in `c6_mutation_check_test.go` and the module loader in `c6_mutation_load_test.go`; it ran
inside C0, C1 and C2 and in the focused run of G1. The A9 ADR read for this check is
`specs/architecture/ADR/0228-a9-runtime-concerns-and-launch-signal-capabilities.md` (D3 residual crossings, "Shim and
exported-API audit", "Closure evidence for the A9 boundary"), with the K7 inventory it carries. The package list of A9 is the four
layer entries of G1; `go list ./internal/agent/... ./internal/process/... ./internal/agentlaunch/...
./internal/providersignal/...` prints exactly those four packages (no sub-package). G2 holds.

## G3 — A9 Shim Register empty; no alias or forwarder added by A9, C6, C1, C2 or C3

The register is empty (arm-1-ar-20 plan, `Shim Register`; ADR 0228, "Shim and exported-API audit", over
`3b9ba5d94..633a39144`). This record extends the audit to every A9 and C coding commit up to R1 by diff inspection, not by
a text search alone: `git diff -U0 3b9ba5d94 7a44042ba -- cmd internal ':!*_test.go' ':!internal/oracle' ':!internal/testguard'
':!internal/testhelpers'` (105 production files, 6,016 insertions, 4,167 deletions), then:

| Check | Result |
|---|---|
| Added `type X = Y` lines in the diff | 0. The three aliases that exist in `internal/agent` at R1 (`CLIExecutionResult`, `SecretMasker`, `referenceContextRepository`) also exist at the starting commit `96c9ad69a` (3 hits) and at `3b9ba5d94`; the run added none and removed none, and none concerns a moved A9 capability |
| Added `var`/`const` of the form `X = pkg.Y` forwarding a moved symbol | 0 relevant. The only matches are `resumeCheckpoint`, `resumeCompletedSprint` and `stopCompletedGoal` (`= ops.AutoResume`/`ops.StopForGoalCompletion`): injectable call variables for the auto-resume decision that already existed in `systemctl.go:72-74` at `3b9ba5d94` (`d63_auto_resume_test.go` overrides `resumeCheckpoint`) and moved whole to `pause_gate.go:57-59`; they are runtime seams to `ops`, not a forwarder for a moved capability |
| Added single-return delegations (13 found by script over the diff, each read) | none forwards a moved capability: `handleApprovedMerges`, `handleApprovedMergesWithAuthority`, `ValidCLIs`, `CheckCLIPrerequisites`, `IsValidCLI` pre-date the run (`claiming.go:405`, `cli.go:14,54,84` at `3b9ba5d94`) and delegate inside their package; `ProviderKeys.Same`, `providerKeysOf`, `PublishInitialState`, `providerEndedSession`, `awaitGate`, `launchGate` and the alert writers are real definitions with their own logic or caller-supplied inputs; `LogProviderUnavailableAlert` formats the alert (ADR 0228) |
| Top-level declarations defined in both `internal/agent` and `internal/agentlaunch`, `internal/providersignal` or `internal/process` | 0 (set intersection over non-test files) |
| Markers (`shim`, `forwarder`, `deprecated`, `compat alias`, `temporary alias`) on added lines | 0 |

The extraction ADR 0228, the C6 evidence files (`c6-ownership-evidence.md`, `c6-bookkeeping-evidence.md`,
`c6-presentation-guard-evidence.md`), and the C1/C2/C3 evidence files each report no shim; this check confirms them over
the union. G3 holds: zero shims added.

## G4 — suite green at the integration revision

All commands run in the task worktree (tree equal to R1 outside `specs/plans`, see Observed revision), canonical form
`env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp <command>`, once each and not retried, sequentially, with no translation of
the canonical commands. The operator note of 2026-10-25T11:20Z tells coders not to pre-run the canonical list before
submitting; here the run is this unit's deliverable (boundary evidence at the integration SHA, plan Task 1 step 4), and
submission re-runs the list on the rebased commit. Host load average (1, 5, 15 min) before C0 `5.57 14.61 12.77`, after C3
`3.18 10.15 11.87`.

| Cmd | Command | Start–End (UTC, 2026-10-27) | Exit | Excerpt |
|---|---|---|---|---|
| C0 | `make lint-archguard` | 11:41:33–11:42:00 | 0 | `ok …/internal/testguard/archguard 26.835s` (the whole guard package, `-count=1`) |
| C1 | `make test` | 11:42:00–11:47:17 | 0 | 86 `ok` lines, 4 `no test files` lines, 0 `FAIL`, 0 `(cached)`; `cmd/liza` 126.479s, `internal/agent` 224.364s, `internal/agentlaunch` 0.039s, `internal/commands` 61.268s, `internal/ops` 225.728s, `internal/oracle/a9` 148.765s, `internal/process` 0.818s, `internal/providersignal` 0.125s, `internal/testguard/archguard` 9.658s; last line `ok …/scripts/ci/testshard 0.538s` |
| C2 | `make lint` | 11:47:17–11:48:23 | 0 | `GOOS=linux go vet ./...`, `GOOS=windows go vet ./...`, `GOOS=darwin go vet ./...`, then `ok …/internal/testguard/archguard 4.106s` (`lint-archguard`) |
| C3 | `make test-e2e` | 11:48:23–11:49:58 | 0 | `go test -race -tags e2e -run '^(TestFullSprintSequence|TestTerminalDependencyRecovery)$' ./internal/integration/ -count=1`: `ok …/internal/integration 80.790s` |

The A9 oracle (`internal/oracle/a9`) ran live inside C1 with every golden unchanged by this unit (it changes no oracle file).
The D53 Linux-only A9 oracle files (a `//go:build linux` constraint, each with a non-Linux counterpart, reason stated per
file; `lifecycle_linux_test.go` quotes D53 in its header) are `internal/oracle/a9/lifecycle_linux_test.go`,
`orchreap_linux_test.go`, `reap_linux_test.go`, `signals_linux_test.go` and `signals_reap_linux_test.go`: the
process-supervision scenarios (procfs, signals, reaping) that D53 scopes to Linux. White-box suites of the A9 packages run
on all three platforms. Focused C-chain proofs, run once after the suites (`go test -count=1 -v -run 'TestC1|TestC2|TestC3'
./internal/ops/ ./internal/agent/ ./internal/commands/ ./internal/providersignal/ ./internal/process/ ./internal/tui/
./internal/analysis/ ./internal/agentlaunch/`, exit 0): `ok` for all eight packages, 155 `--- PASS` lines (subtests
included; 9 `TestC1*`, 33 `TestC2*`, 33 `TestC3*` top-level passes across packages), no `FAIL`. Observation on Git
transcripts (operator note 2026-10-25T23:31Z): this unit freezes no Git output.

**Race (D55, RP10).** No local race command is run (D55). The race coverage is the Linux tests job of the D52 run in G8,
whose log (`gh run view 40492973663 --job <Shared CI / Linux tests> --log`, grepped for `ok` lines) shows
`make ci-test-nosync` running `go test -race -timeout 30m -p 2 $(go list ./... | grep -v -e '/internal/integration$'
-e '/cmd/liza$')`, then `go test -race -tags e2e … ./internal/integration/` and `go test -race … ./cmd/liza/`, with `ok` for
all five A9-relevant packages: `internal/agent` 267.109s, `internal/agentlaunch` 1.145s, `internal/oracle/a9` 261.396s,
`internal/process` 1.812s and `internal/providersignal` 1.083s (86 `ok` lines in the job, no `FAIL`). The delivered A9
package list is the four of G1; no sub-package exists and the Linux race step runs every one of them. G4 holds: no red
suite is recorded as green.

## G5 — wall time and benchmarks against `baseline.md` (indicative; never blocks)

Protocol repeated from `baseline.md`: fresh disposable clone outside the worktrees (`/tmp/a9g5clone.a1SOQD/repo`,
`git clone --local --no-checkout <project root>` then `git checkout --detach 7a44042ba…`; clone `HEAD` printed
`7a44042ba68f09011d6bc92a77ea716c105f029a`), sync-clean check (`make sync-embedded` exit 0, `git status --short` empty, 0
bytes), then one warm-up `make test`, one timed `make test` and `timeout 300 go test -run '^$' -bench . -benchmem
./internal/<pkg>` for `./internal/db`, `./internal/commands` and `./internal/agent`, serially, after G4 and with nothing
else running from this unit, under `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp GOFLAGS=-count=1`. No retry. The clone's
`git status --short` was empty afterwards and the clone was deleted (`ls -d /tmp/a9g5clone.*` matches nothing).

- Host load average (1, 5, 15 min, `/proc/loadavg`): before warm-up `1.85 9.05 11.43`; before timed run `5.99 9.33 11.20`;
  after timed run `4.32 7.61 10.22`; after benchmarks `3.60 6.86 9.83`. The baseline ran at about 30 on 8 cores, so the
  comparison is load-confounded in this run's favour.

| Run | Start (UTC) | End (UTC) | Wall s | Exit | Output |
|---|---|---|---|---|---|
| Warm-up | 11:50:33 | 11:54:31 | 238 | 0 | 86 `ok`, no `FAIL`, no `(cached)` |
| Timed | 11:54:31 | 11:57:47 | 196 | 0 | 86 `ok`, no `FAIL`, no `(cached)` |

N4: the baseline observations stay distinct: the timed run was 523 s, red (two `internal/agent` deadline tests) and taken
under load about 30; the green warm-up was 444 s under load about 9 rising to about 30. Neither is a clean comparable
baseline, so N4 is **not claimed met** against a clean baseline. This run's 196 s (timed) and 238 s (warm-up) are below
both: +25% is 653.75 s of the timed baseline and 555 s of the warm-up baseline; 196 s is 62.5% below 523 s and 55.9% below
444 s, and 238 s is 46.4% below 444 s. **Disposition (Should, never blocks):** out of the +25% bound for neither figure,
recorded with the load caveat and the red baseline; a repeat of the baseline on a quiet host would supply a cleaner
comparison. Per-package times, warm-up and timed: `cmd/liza` 125.0 s and 93.1 s, `internal/commands` 54.1 s and 45.8 s,
`internal/ops` 187.9 s and 179.2 s, `internal/agent` 214.0 s and 193.8 s, `internal/oracle/a9` 139.9 s and 134.0 s. The
C6 guard's overhead, unmeasured when planned, is `TestC6MutationGuard` at 1.38 s inside
the 26.8 s archguard package of C0.

Benchmarks: all three packages exited 0 with `PASS` (`internal/db` 42.519s, `internal/commands` 3.184s, `internal/agent`
2.972s). Name-to-owner map: all 18 baseline benchmark rows were captured at their baseline owners (`internal/db` 14,
`internal/commands` 2, `internal/agent` 2); none moved package during A9, and none is "not captured". No benchmark was
added.

| Benchmark | ns/op base → now | B/op, allocs/op |
|---|---|---|
| db `BlackboardWrite` | 12033415 → 1832578 (−84.8%) | same |
| db `BlackboardModify` | 22577288 → 2051942 (−90.9%) | B/op +0.6%, allocs +0.7% |
| db `BlackboardReadCached` | 44938 → 19297 (−57.1%) | same |
| db `BlackboardReadUncached` | 1890776 → 173484 (−90.8%) | same |
| db `MarshalStateForWriteLarge/repaired=false` | 536001675 → 125114035 (−76.7%) | same |
| db `MarshalStateForWriteLarge/repaired=true` | 1331986515 → 125792689 (−90.6%) | same |
| db `ReadCachedHit` | 4916749 → 1099305 (−77.6%) | same |
| db `StateParse` | 112041835 → 27755201 (−75.2%) | same |
| db `TerminalArchiveTransaction/inline/cold-instance` | 772451364 → 287889926 (−62.7%) | same |
| db `TerminalArchiveTransaction/inline/warm` | 628528774 → 264518161 (−57.9%) | same |
| db `TerminalArchiveTransaction/archived/cold-instance` | 320733652 → 149750144 (−53.3%) | same |
| db `TerminalArchiveTransaction/archived/warm` | 185103984 → 25779473 (−86.1%) | B/op −9.9%, allocs −18.9% |
| db `TerminalArchiveTransaction/archived-256/cold-instance` | 990744276 → 634129482 (−36.0%) | same |
| db `TerminalArchiveTransaction/archived-256/warm` | 319924638 → 101470035 (−68.3%) | B/op −1.8%, allocs −3.5% |
| commands `InspectionSnapshotDuringHeartbeat/status` | 9380378 → 7418253 (−20.9%) | B/op +12.3%, allocs +6.6%; heartbeat-writes 6 → 34 |
| commands `InspectionSnapshotDuringHeartbeat/get` | 4376937 → 2494355 (−43.0%) | B/op +5.3%, allocs +4.3%; heartbeat-writes 13 → 31 |
| agent `BenchmarkClaimReviewerTask` | 34512825 → 4517068 (−86.9%) | B/op +2.7%, allocs +1.2% |
| agent `BenchmarkResumeHandoff` | 5936738 → 2223387 (−62.5%) | same |

"same" means within ±0.5%. Every ns/op figure is below the baseline, so no row is out of the +25% bound. **Dispositions**
(N3 is Should; none blocks):

- **agent `BenchmarkClaimReviewerTask`** B/op +2.7% and allocs/op +1.2%: the only row of an A9 package above the baseline
  in memory, and not present at the A8 boundary (same, ±0.5%). It was attributed by a same-host A/B in a disposable clone
  at five commits (`go test -run '^$' -bench ClaimReviewerTask -benchmem ./internal/agent`, one run each, after
  `make ci-prepare`): `3b9ba5d94` (A9 start) 920704 B/op, 16881 allocs; `4bd986841` (C6 end) 921162, 16882; `8124f434b`
  (C1) 921337, 16881; `1dd4045af` (C2) 946539, 17093; `7a44042ba` (R1) 945934, 17091. The A9 refactor, C6 and C1 leave it
  within 0.1%; the step is C2's, which loads the canonical provider keys once per reviewer claim (`reviewerClaim.loadPolicy`,
  `c2-provider-keys-evidence.md`, "Catalog capture placement"). ns/op at the five commits is 4.39–4.64 ms (within host
  noise). **Disposition:** an intended cost of the C2 behaviour change, 2.7% memory on a path whose time is 87% below the
  baseline; no correction commissioned.
- **`InspectionSnapshotDuringHeartbeat/status`** and **`/get`** B/op and allocs/op (+12.3% and +6.6%; +5.3% and +4.3%):
  the observation the A3 to A8 boundaries carried (`boundary-a7.md` G5: same-host A/B of the starting commit against the
  boundary revision showed B/op equal within about 1%, the +12.2% already present at the starting commit, since the benchmark
  overlaps a concurrent heartbeat writer and does 6 against 34 heartbeat writes per window, so work per iteration is not
  like-for-like). `boundary-a8.md` recorded +12.2%/+6.5% and +5.9%/+5.1%; the figures now are the same within noise.
  **Disposition:** not attributable to A9 or the C chain; no correction commissioned, no new A/B run.
- **db `BlackboardModify`** B/op +0.6%, allocs +0.7%: the same figures as the A5 to A8 boundaries, within noise of the
  benchmark's own setup, in `internal/db` (A1). No action.
- **A9 paths.** The baseline set holds no benchmark of the supervisor loop, launch or signal code, and none requiring a real
  provider was added. Supervisor and launch behaviour is covered by the A9 oracle and the PB maps of the supervisor and
  backend units (`a9-supervisor-evidence.md`, `a9-closure-evidence.md`); no A9-specific timing is claimed beyond the test times above.
- The other rows are decreases or unchanged: no action.

## G6 — architectural issues resolved by A9 and C6, C1, C2, C3

Reviewed the Open Issues Summary (62 open at R1 before this unit's edit: 17 high, 30 medium, 15 low;
`specs/architecture/architectural-issues.md`, counted from the table rows) under `issues-policy` (the file's Update
Policy), the A9 ADR 0228 closure evidence and the C chain evidence files (`c1-lease-duration-evidence.md`,
`c2-provider-keys-evidence.md`, `c3-unavailable-signals-evidence.md`, ADRs 0229 and 0230). The active sections were
re-grepped at R1 for `internal/agent`, `supervisor.go`, `registration.go`, `lease_duration`, `terminal-1` and `provider_key`;
the hits that name an A9 subject are the entries closed or classified below.

**Closed (3), wholly resolved by merged work, with the registry synchronised (active section removed, table of contents and
Open Issues Summary rows removed, Completed Fixes, Fixed (Traceability) and Fix Details added, Counts line updated to
17 high, 27 medium, 15 low, 59 total):**

| Issue | Resolving commits | Evidence at R1 |
|---|---|---|
| Decompose supervisor.go (1,129 LOC) | `75eb56dde216a9dc0d3ce4491b9ecf63097cd5f3`, `5289fc4b190539db4849451d6ea11250391d9393`, `831ad892ebb929eaaf3f746ece8b2da13b7a7b51`, `e8fca74ee03c918b77bb1720036ec5d71d00a0d7`; ADR 0228 in `f915f01ab49bf61921b82c78def9ee3792e485d2` | G1: `supervisor.go` 198 lines (663 at the start), `RunSupervisor` 29 (521), `systemctl.go` 57 (861), all twelve function offenders under 100 lines, A9 ratchet and S2 empty. The entry's concerns have their own files: restart tracking (`supervisor_restart.go`), runtime-failure tracking (`supervisor_runtime_failure.go`), lease behaviour (`supervisor_lifetime.go`, resolved once, C1), lifetime, turn and completed-turn phases, session execution; the `CLIAgent` boundary is unchanged |
| Agent Lease Renewal Ignores Configured Lease Duration | `8124f434b053bd497133f97f87982d7ce8bf2abb` (C1) | `git grep -n 'LeaseDurationSeconds(' -- '*.go' ':!*_test.go'` finds the resolver (`ops/lease.go:13`) and its four callers (`supervisor_lifetime.go:141`, `strategy_reviewer.go:382`, `ops/review_reclaim.go:74`, `commands/repair_agent_pool_start.go:159`); the heartbeat takes the value registration used; no `1800` literal or `30 * time.Minute` reclaim constant remains at those sites (`heartbeat.go` keeps `DefaultLeaseDuration` for standalone construction). `TestC1*` (9 top-level passes in the focused run of G4) cover registration, heartbeat for owned task and active review, reviewer claim, reclaim (early and reservation-held) and pool-repair rows with 600 s configured and 1800 s default |
| Canonical Provider Identity Lost Before Policy Enforcement | `1dd4045af1722607e1c4390d639b8d779afbc892`, `e63a111b1975471d998416b642e0ead262a83fc5` (C2); `f751d845c3d48d4a01d330cfd80c37ba41b30116`, `cbce741a8771a2021e19d34fbfa1d469a320b0a3`, `ecf80111a4742dc39ff25a3b6108e72a793afe8d`, `225ed0af8c7734e67e83baadebc16817afed8e02` (C3) | the issue's three symptoms are all closed: approval diversity (reviewer-claim filtering and the merge gate compare `agentlaunch.ProviderKeys` keys; `git grep -n HasProviderDiversity -- '*.go' ':!*_test.go'` finds only the raw `models.Task` utility, its comment in `ops/provider_diversity.go`), provider-unavailable stop signals (`TestC3RaiseCodexStopsCodexACPAndBack`, `TestC3ProviderUnavailableRaiseStopsAliasSupervisorBothWays`, `TestC3SpawnRefusesByCanonicalKey`) and provider-audit anomaly grouping (`TestC2ProviderAuditGroupsAliasesAsOneProvider`); 33 `TestC2*` and 33 `TestC3*` top-level passes in the focused run of G4 |

Why "wholly resolved", and the distinctions the plan requires:

- **Supervisor decomposition:** the entry's Issue is a mixed 1,129-line file and an oversized `RunSupervisor`; its Direction
  (restart tracking and spinning policy in focused files, `RunSupervisor` around named phases, `CLIAgent` boundary kept) is
  delivered. The size alone was not taken as proof (a smaller file is not cohesion): the concerns above each have an
  owning file and `RunSupervisor` is an ordered coordinator (ADR 0228, D4). The earlier #156 traceability line
  (reviewer-claim breaker in `claim_breaker.go`) is unchanged.
- **Lease versus terminal identity (C1):** the issue's defect is that the configured lease was consumed by neither
  registration nor heartbeat; it is closed at all five sites. The entry's Direction also suggested deriving the terminal
  identity from the launch context. C1's acceptance criterion keeps the stored terminal value `"terminal-1"`, now the named
  constant `terminalPlaceholder` (`supervisor_lifetime.go:21`), pinned by `TestC1TerminalPlaceholderKeepsStoredValue`. The
  closure therefore does **not** claim the terminal identity derived: that clause of the Direction is superseded by the goal
  and the Fix Details entry says so.
- **Canonical-provider policy versus hard diversity at verdict time (C2/C3):** C2 changes which keys the existing checks
  compare (claim filtering, the merge gate, anomaly grouping) and C3 the stop-signal matching, derived at evaluation time;
  stored `agents[].provider` values, quota policy and tool-name lookups are unchanged, and nothing canonicalizes stored values
  or changes quota. "INVARIANTS.md §6 Provider Diversity Not Enforced at Verdict Time" is about refusing a verdict for
  missing diversity, which no C scope adds: it **stays open**. The entry's Direction (carry canonical identity through
  `SupervisorConfig`) is replaced by C2's policy-time derivation, recorded in Fix Details.

**Not closed, with reasons:**

- "Commands Layer Imports Agent Runtime" (`issue-crossing`): stays open. Of the three importing command files, `status.go`
  keeps a dependency (the wake-trigger diagnosis query, now in `status_orchestrator.go`) and `repair_agent_pool.go` and
  `resume.go` no longer import `agent` (G2); `tui` keeps one pure query. Both remain as rationale-carrying allowlist
  entries, but the compile-time dependency for wake-trigger diagnosis the entry describes still exists and its first
  Future option (move the query to `models/diagnostics.go`) was not taken; "No Query Layer" is coupled and open. A partial
  fix stays open with that residual named.
- "Provider Stop-Signal Recovery Has Multiple Owners": stays open (named explicitly by the plan). The signal primitives
  moved to `providersignal` and C3 changed matching, but CLI resume and TUI resume still detect, clear, select the target
  state and report on their own (`TestC3ResumeCommandListsAndClearsRecordsByHandle`,
  `TestC3ResumeSystemCmdClearsEveryRecordByHandle` test both paths separately); one owning operation is a later change.
- "INVARIANTS.md §6 Provider Diversity Not Enforced at Verdict Time": stays open (above).
- "Control-Flow Vocabulary Bypasses Domain Ownership" (`issue-vocabulary`): stays open. A9 typed and named its own
  vocabulary (ADR 0228, D6, K7 table) but the issue spans every area and the C scopes did not retire the unexported `ops`
  constants it names.
- "Restart/Lease Churn Under Load": stays open (accepted v1 limitation); C1 makes the lease configurable but does not change
  restart or churn behaviour.
- "Provider Support Crosses the Catalog Boundary": stays open; the Codex audit and availability signatures moved to
  `providersignal` but the catalog data still does not own the provider-extension contract.
- C6 has no registry entry of its own and closes none: "State Validation Composition Gap" and the validation-on-write
  concern are `wont` items (no validation added), and the C6 work moved mutation without adding any. "Merge Execution
  Authority Split" and "Cross-Script State Mutation" concern documents and CLI commands, not the agent-to-Blackboard
  boundary C6 enforces.
- Every `wont` item stays open: validation on the write path, worktree-intelligence refresh policy (including "Worktree
  Intelligence Refresh Has Multiple Owners"), `--json` error codes, custom roles ("Custom Roles Render Empty Prompt
  Blocks"), fan-out priority, sprint-metric preservation ("Sprint Metrics Lossy at Sprint Boundary") and Git hardening of
  `liza launch --cwd` ("Raw Git Calls Bypass gitenv Hardening"). "Index Discovery Failures Are Unobservable" is an A8
  item whose behaviour A9 did not touch. The other open entries name no A9 or C subject.

The next owner of the registry is the documentation unit (`arm-1-ar-25-cp-0-code-1`), which reconciles the remaining
entries after this one.

## G7 — record completeness

This file holds the commands or source reads, the integration SHA R1 `7a44042ba68f09011d6bc92a77ea716c105f029a`, the UTC
observations, exits and excerpts for G1–G6, the G5 dispositions, the G6 reasons and the G8 section below. G1–G7 are
committed with the result of the maintainer-confirmed run recorded in G8. The acceptance manifest maps all eleven
obligations (G1–G8, B-failure, B-sequence, B-environment) and records no result itself.

**B-failure.** No G1–G4 check failed, so no `mark-blocked` for A9 closure debt was needed. This unit repaired nothing: the
files it changes are `specs/architecture/consolidation-run/a9-boundary.md`,
`specs/architecture/architectural-issues.md` and
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-25-cp-0-boundary.json` (`git diff --name-only
dcbfdd044 HEAD` at the submission base), with no production, test, guard-data, oracle, baseline, Makefile, ADR or overview
change. The red G8 run `40480433464` at `225ed0af8` (Windows `rest-2` and `rest-1`; D52, D60) held A9, and the operator
commissioned the correction `d53-d56-a9-windows-portability-plan` (merged at R1), which this unit consumes unchanged.

**B-environment.** Isolation, no push, provider CLI, install or update, sanitized output (no environment values), the
disposable clones (`/tmp/a9g5clone.a1SOQD` for G5, and one clone for the `ClaimReviewerTask` A/B, both deleted and
verified absent) and the scratch locations are as listed in Observed revision and G5.

## G8 — dispatched CI result (D52): green

Status: **green on attempt 1**. Under D52 and D59 the maintainer dispatched `.github/workflows/ci.yml`
(`workflow_dispatch`) on the pinned ref `g8/a9-7a44042` whose tested SHA equals R1. The result was supplied as maintainer
confirmation under the standing delegation recorded in this task's advisory validation notes and in the plan amendment
`arm-1-ar-25-cp-0-amend-1` (RP-A3); the agent pushed nothing and read the run back with `gh run view 40492973663 --json …`
at 2026-10-27T12:04Z (headSha, ref, event, conclusion, status and attempt re-read), which agrees with the confirmation in every field below. D60 (rerun of a red run) was not used.

- Run: https://github.com/liza-mas/liza/actions/runs/40492973663
- Event: `workflow_dispatch`; ref `g8/a9-7a44042`; **tested SHA `7a44042ba68f09011d6bc92a77ea716c105f029a`, equal to R1.**
- Run conclusion `success`, attempt 1; created 2026-10-27T11:01:18Z, completed 2026-10-27T11:18:09Z.

| Job | Conclusion | Completed (UTC) |
|---|---|---|
| Shared CI / Linux tests (race and integration, D55) | success | 11:18:08Z |
| Shared CI / Linux lint and build | success | 11:03:48Z |
| Shared CI / macOS compatibility | success | 11:14:52Z |
| Test (windows) | success | 11:10:38Z |
| Test (windows, ops-1) | success | 11:06:51Z |
| Test (windows, ops-2) | success | 11:04:42Z |
| Test (windows, ops-3) | success | 11:05:30Z |
| Test (windows, ops-4) | success | 11:06:53Z |
| Test (windows, rest-1) | success | 11:08:42Z |
| Test (windows, rest-2) | success | 11:08:29Z |
| Test (windows, rest-3) | success | 11:10:16Z |
| Release verified tag | skipped (tag-only job) | 11:18:08Z |

Linux, Windows and macOS are all green at R1 in one run, so G8 holds. The earlier red run `40480433464` (tested SHA
`225ed0af8c7734e67e83baadebc16817afed8e02`, 2026-10-27T09:23Z–09:55Z: Windows `rest-2` C6 listing without a Go cache
location, Windows `rest-1` hang in the PB-3 verdict outbox test) is superseded and is not G8 evidence; its two failures are
the ones `f112f85ed` (`fix(portability): give the C6 listing test a owned Go cache and refuse a non-directory verdict
outbox on every OS`) corrected. R1 is the proven SHA; the submission base of this unit (the task HEAD `dcbfdd044` plus this
unit's evidence commit) differs from R1 only by the plan amendment files and this unit's own documentation and is not
claimed as CI-tested. No A9 finding needed a correction: the G6 closures recorded above are the only registry change.
