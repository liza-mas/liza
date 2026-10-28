# Architecture Consolidation Run: Area Boundary Record A3 (Ops kernel and execution lifecycle)

Evidence of the A3 area-boundary checkpoint (`Area-Boundary Checkpoint Contract`, checks G1–G8) from the
arm-1-ar-13 plan, Scope 1, task `arm-1-ar-13-cp-0-code-0`. One record per boundary; later units never edit
it. It changes no production, test, guard-data, oracle or baseline file. `tdd_not_required`: evidence-only unit
that moves no code.

## Observed revision

- Configured integration branch: `global-refactoring` (D23; `baseline.md`). `omni-ee get config.IntegrationBranch`
  answers `not_found` for that key, so the identity is taken from the task worktree: `git rev-parse
  global-refactoring` and `git rev-parse HEAD` both print the revision below, and `git log
  HEAD..global-refactoring` printed nothing at the start (2026-10-25T06:33Z) and again at the end of G1–G5
  (2026-10-25T07:00Z, `global-refactoring` unchanged).
- Integration revision under test: `98dc9efc270749fb130ae5dbbababecf0bfc35d0`
  (`docs(adr): record A3 ops kernel concern layout and vocabulary ownership`). The task worktree `HEAD` is this
  revision plus the commits that add this record and the acceptance manifest; `git diff --stat 98dc9efc2 HEAD`
  lists only those files.
- Observation window (UTC): 2026-10-25T06:33Z to 07:00Z, task branch `task/arm-1-ar-13-cp-0-code-0`, worktree
  clean before and after every command (`git status --short` empty).
- Starting commit of the run (for A3 diffs): `96c9ad69aee983ce2b929dd681b6ba43bf920535`.
- A3 coding units merged before this unit (`git log --no-merges 96c9ad69a..HEAD -- internal/ops`, 23 commits
  touching `internal/ops`): A1 adoption `91ad8a41e`, `eebdbff8a`, `eb5c205f0` (+ tests `97d719e6f`, `cc330a910`);
  claim `c00efcbb8`; blocked episode `4deb689ae`, `e7f519352` (tests), unblock `907472eac`; ownership release and
  recovery `8563e5070`, `4e7f6f2d1` (test); resume `09d0121af`; typed safe-action/effects `930ad944a`,
  `27103e974`; K7 inventory `408bb4c1a`, `e76d6d748`; ADR `98dc9efc2`; test-timing repairs `a268e0163`,
  `ee2d45df3`, `bd9e29aaf`, `5ab2f9870`; A3 oracle portability correction `de5c259cd` (06:38+01:00 on
  2026-10-25) and `3ffd032fc` (06:59+01:00), test and fixture code under `internal/oracle/a3/lifecycle` only. `ba7b79b83` (sessionvalidation), `e72328595` (scipsearch) and
  `b11752a40` (git) touch one `internal/ops` line each as A2 typing adoption and belong to A2's boundary.
  The A4 oracle (`arm-1-ar-4-cp-0-code-0..2`) is merged.
- Environment for every measurement: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` (GUARDRAILS G1.4), plus
  `GOFLAGS=-count=1` for C1–C4 and G5 (translation of the canonical form, so no result is served from the Go test
  cache). No remote push, provider CLI, installation or update ran; output below is sanitized (no environment
  values). Host: `Linux 6.8.0-147-generic x86_64`, `nproc` 8, `go1.26.0 linux/amd64`, `git version 2.43.0`.

## G1 — A3 ratchet empty; S2 exceptions data-like with rationale

- Source: `internal/testguard/archguard/area_a3_test.go` at `98dc9efc2` (read in full, 13 lines). Excerpt (lines
  10–12):

  ```go
  Allowlist: []AllowEntry{},
  Ratchet:   []RatchetEntry{},
  S2:        []S2Entry{},
  ```

- A3 Ratchet: none. This is recorded separately from the C0 pass below, since the guard alone permits remaining
  debt. The start snapshot's A3 count was 15 keys (2 files, 13 functions; `baseline.md`); all were removed rather
  than converted to S2 (four keys dropped by `c6aae2d49`, the rest by the claim, blocked, release and resume units; the last edit to the file is `09d0121af`).
- A3 S2: none, so no entry needs a category, data-likeness check or human ruling.
- Independent size check (a throwaway Go AST program under `/tmp`, not in the repository; S1 measurement:
  `FuncDecl` with body, declaration to closing brace; physical lines) over the 83 production files of the A3 list
  plus the 14 files the A3 tasks added: longest function 95 lines
  (`recover_agent.go` `recoverAgentLifecycle`), 578 functions, 0 over 100; largest file
  `claim_task_strategy.go` 678 lines (< 800), then `mode_change.go` 634, `unblock_task.go` 506.
- C0: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 ./internal/testguard/archguard/`,
  2026-10-25T06:34:31Z, exit 0: `ok  github.com/liza-mas/liza/internal/testguard/archguard  0.936s`.
- Result: pass.

## G2 — A3 allowlist holds only rationale-carrying entries; no SI-5 crossing owned by A3

- Source: same file, `Allowlist: []AllowEntry{}`: empty, so no entry lacks a rationale. The `internal/ops`
  application-layer entry is intact (line 8: `{Package: "internal/ops", Layer: layerApplication}`).
  `go list ./internal/ops/...` prints only `github.com/liza-mas/liza/internal/ops`: no kernel or capability
  sub-package exists, so no other layer entry was owed.
- SI-5 (`20261023-162147-arm-1.md#SI-5 K3 Crossing Resolutions`) assigns `prompts → ops` to Scope 19 (A8),
  `jsonout → ops` to Scope 17 (A7), `commands → agent`/`tui → agent` to Scope 20 (A9) and `agent → db` to Scope 21
  (C6, in A9); the baseline allowlist entries live in `area_a7_test.go`, `area_a8_test.go` and `area_a9_test.go`
  with rationales naming those scopes. The plan's `crossings` reference assigns none to A3. Result: no A3-owned
  crossing.
- The C0 pass (`TestK4ImportDirectionGuard` is in that package) also shows no unlisted forbidden edge and no stale
  allowlist entry. Result: pass.

## G3 — A3 Shim Register empty; no alias or forwarder added

- Register: `20261023-203240-arm-1-ar-12.md#Shim Register` reads "Empty"; the pre-existing `LifecycleRequest`
  model alias is not a migration shim. Verified: `type LifecycleRequest = models.LifecycleIdentity` is at
  `internal/ops/lifecycle_receipt.go:30` both at the starting commit `96c9ad69a` and at `98dc9efc2`, introduced by
  `fa4964845` (`fix(lifecycle): return authoritative outcomes for repeated actions`), before this run's A3 work.
- Inspection, `git diff -U0 96c9ad69a 98dc9efc2 -- internal/ops ':!*_test.go'` (9,086 diff lines; net 84 files,
  4,697 insertions, 3,419 deletions, all `internal/ops`, no sub-package):
  - added `type X = Y` alias declarations: 0 (`grep -cE '^\+\s*type\s+\w+(\[[^]]*\])?\s+=\s'`, count 0).
  - added lines with deprecated / shim / forwarder / compat alias / temporary / back-compat / backward markers: 1
    (`grep -ciE`), `lifecycleCounterTempPattern` ("names the temporary file a counter write…"), a temp-file name
    pattern for a counter write, not a migration shim.
  - added one-line forwarder functions (a script matching `func … { return f(…) }` bodies in `HEAD` and at the
    start, comparing names): 161 at the start, 160 at `98dc9efc2`, 0 new names. Every forwarder that exists at
    `98dc9efc2` existed before the run.
  - The three non-ops commits above (`b11752a40`, `e72328595`, `ba7b79b83`) add no `type X = Y` declaration
    (`grep` over their non-test diffs prints nothing).
- `de5c259cd` adds `internal/oracle/a3/lifecycle/testdata/gitshim/main.go`, a stdlib-only test-fixture executable
  installed as `git.exe` on Windows so `os/exec` can launch it. It is a fixture helper under `testdata/`, not an
  alias or forwarder of relocated production API, so it is not a G3 shim.
- Result: pass; no alias or forwarder was added by A3 coding.

## G4 — suite green at the integration revision

All five canonical commands ran at `98dc9efc2` in the task worktree. Wall times are shared-host, not
baseline-comparable (see G5 for that). `make test` and `make lint` print `make sync-embedded` output (`Files
synced successfully`) and leave the tree unchanged (`git status --short` empty after each command).

| Cmd | Command | Start (UTC) | End (UTC) | Exit | Excerpt |
|---|---|---|---|---|---|
| C0 | `go test -count=1 ./internal/testguard/archguard/` | 06:34:31 | 06:34:35 | 0 | `ok .../internal/testguard/archguard 0.936s` |
| C1 | `make test` (+`GOFLAGS=-count=1`) | 06:34:39 | 06:41:26 | 0 | 83 `ok` lines, 0 `FAIL`, 0 `(cached)`; `internal/ops` 265.468s, `internal/oracle/a3/control` 18.502s, `internal/oracle/a3/lifecycle` 58.298s |
| C2 | `make lint` (+`GOFLAGS=-count=1`) | 06:41:41 | 06:42:38 | 0 | `go vet ./...` for `GOOS=linux`, `windows`, `darwin`; `ok .../internal/testguard/archguard 0.286s` |
| C3 | `make test-e2e` (+`GOFLAGS=-count=1`) | 06:42:41 | 06:44:19 | 0 | `ok .../internal/integration 80.708s` (`-race -tags e2e`) |
| C4 | `go test -race ./internal/ops/...` (+`GOFLAGS=-count=1`) | 06:44:22 | 06:51:04 | 0 | `ok .../internal/ops 382.717s` |

C4 selection reconciled with the delivered layer data: `./internal/ops/...` resolves to the single package
`internal/ops` (A3's only package root; no kernel sub-package was created). X2 caller packages (`commands`,
`cmd/liza`, `agent`) are not A3 packages; their tests ran in C1. The windows-tagged A3 files
(`exec_windows.go`, `shell_runner_windows.go`, `delete_agent_process_windows.go`) are only vetted locally (C2,
`GOOS=windows go vet ./...`); their tests run only in G8.
Result: pass at the local host (git 2.43.0). Remote Windows and macOS results are G8's, not G4's. The A3 oracle
`TestOracleUnblockTaskVariants` golden depends on the Git version (see G8 for the operator-reported consequence).

## G5 — wall time and benchmarks against `baseline.md` (indicative; never blocks)

Protocol repeated from `baseline.md`: fresh disposable clone outside the worktrees
(`/tmp/a3g5base.lQKGpZ/repo`: `git clone --local --no-checkout <project root>` then `git checkout --detach
98dc9efc…`; clone `HEAD` printed `98dc9efc270749fb130ae5dbbababecf0bfc35d0`), sync-clean check
(`make sync-embedded` exit 0, `git status --short` empty), then one warm-up `make test`, one timed `make test`,
then `timeout 300 go test -run '^$' -bench . -benchmem ./internal/<pkg>` under `env -i HOME="$HOME" PATH="$PATH"
TMPDIR=/tmp GOFLAGS=-count=1`. No retry. The clone's `git status --short` was empty afterwards and the clone was
deleted (`ls -d /tmp/a3g5base.*` matches nothing).

- Host: shared; other agent sessions were running. Load average (1, 5, 15 min): before warm-up (06:51:31Z)
  `7.78 13.35 17.13`; before timed run (06:55:28Z) `5.93 12.73 16.37`; after timed run (06:58:38Z)
  `3.36 8.96 14.27`.

| Run | Start (UTC) | End (UTC) | Wall s | Exit | Output |
|---|---|---|---|---|---|
| Warm-up | 2026-10-25T06:51:31Z | 2026-10-25T06:55:28Z | 237 | 0 | 83 `ok`, no `FAIL`, no `(cached)` |
| Timed | 2026-10-25T06:55:28Z | 2026-10-25T06:58:38Z | 190 | 0 | 83 `ok`, no `FAIL`, no `(cached)` |

**N4 comparison (+25%).** The baseline has two distinct numbers, not interchangeable: the green warm-up of 444 s
(bound 555 s) and the timed run of 523 s, which was red (exit 2, two `internal/agent` deadline tests failed at
load about 30) and load-confounded (bound 653.75 s). Timed 190 s is −57.2% vs 444 s and −63.7% vs 523 s; warm-up
237 s is −46.6% vs 444 s and −54.7% vs 523 s. Both are inside either bound. Caveats: load here (about 3–8) was
well below the baseline's (about 9–30 on 8 cores), and the test set is larger now (83 packages with `ok` vs 66 at
the start), so the comparison is indicative, not like-for-like, and the margin partly reflects the quieter host.
Disposition: N4 holds; no action.

**N3 benchmarks.** Single run per package at the lower load above; baseline numbers from `baseline.md` (load
about 30). Deltas are ns/op; B/op and allocs/op are shown where they differ by more than 0.5%.

| Benchmark | ns/op base → now | B/op, allocs/op |
|---|---|---|
| db `BlackboardWrite` | 12033415 → 6688953 (−44%) | same |
| db `BlackboardModify` | 22577288 → 11395245 (−50%) | same |
| db `BlackboardReadCached` | 44938 → 17319 (−61%) | same |
| db `BlackboardReadUncached` | 1890776 → 176023 (−91%) | same |
| db `MarshalStateForWriteLarge/repaired=false` | 536001675 → 118532729 (−78%) | same |
| db `MarshalStateForWriteLarge/repaired=true` | 1331986515 → 118505589 (−91%) | same |
| db `ReadCachedHit` | 4916749 → 1050175 (−79%) | same |
| db `StateParse` | 112041835 → 26338844 (−76%) | same |
| db `TerminalArchiveTransaction/inline/cold-instance` | 772451364 → 242511194 (−69%) | same |
| db `TerminalArchiveTransaction/inline/warm` | 628528774 → 239901384 (−62%) | same |
| db `TerminalArchiveTransaction/archived/cold-instance` | 320733652 → 154309204 (−52%) | same |
| db `TerminalArchiveTransaction/archived/warm` | 185103984 → 44976854 (−76%) | B/op −8.8%, allocs −16.8% |
| db `TerminalArchiveTransaction/archived-256/cold-instance` | 990744276 → 545474450 (−45%) | same |
| db `TerminalArchiveTransaction/archived-256/warm` | 319924638 → 85468788 (−73%) | B/op −3.4%, allocs −6.8% |
| commands `InspectionSnapshotDuringHeartbeat/status` | 9380378 → 6329817 (−33%) | B/op +12.5%, allocs +6.6%; heartbeat-writes 6 → 37 |
| commands `InspectionSnapshotDuringHeartbeat/get` | 4376937 → 2244339 (−49%) | B/op +5.0%, allocs +4.3%; heartbeat-writes 13 → 36 |
| agent `BenchmarkClaimReviewerTask` | 34512825 → 4420551 (−87%) | same (B/op +0.0%, allocs +0.0%) |
| agent `BenchmarkResumeHandoff` | 5936738 → 2116794 (−64%) | same (B/op −0.1%, allocs −0.1%) |

All three packages exited 0 with `PASS` (`internal/db` 51.317s, `internal/commands` 3.198s, `internal/agent`
3.105s); all 18 benchmark rows of the baseline were captured, none "not captured".

**Dispositions** (N3 is Should; none blocks):

- **A3 claim/resume paths.** `BenchmarkClaimReviewerTask` 34.5 ms → 4.42 ms and `BenchmarkResumeHandoff` 5.94 ms →
  2.12 ms (B/op and allocs/op unchanged within 0.1%): no slowdown. The gain is recorded as measured, not claimed
  as caused by A3, because the baseline ran at load about 30 and this run at about 3–8. The unchanged allocation
  counts show the claim and resume refactors (`c00efcbb8`, `09d0121af`) did not add per-iteration allocations.
- Every other ns/op row is at or below its baseline; there is no slowdown to dispose of, with the same
  load caveat.
- `InspectionSnapshotDuringHeartbeat/status` B/op +12.5%, allocs +6.6%, and `/get` B/op +5.0%, allocs +4.3%:
  out-of-bound as per-iteration allocation, not attributable to A3. The same direction was recorded at the A1
  boundary (+10.5%/+5.6% and +5.2%/+4.3%). The benchmark overlaps a concurrent heartbeat writer, so work per
  iteration is not like-for-like (heartbeat-writes per measured window rose 6 → 37 and 13 → 36). The benchmark
  lives in `commands` (A7). The `status` path calls `ops.LoadResolverForModels`, `LoadDetectionContext` and
  `LoadPhaseHandoffDetectionContext` (`pipeline_ops.go`, A4) and `EvaluateLiveIntegrationProgress`
  (`integration_progress.go`, A6); `git diff --stat 96c9ad69a 98dc9efc2` over `pipeline_ops.go`,
  `integration_progress.go`, `plan_handoff.go`, `status.go` and `inspect.go` prints nothing, and `internal/commands`
  production files changed by 3 files, 9 lines in total (`init.go`, `inspect_anomalies.go`,
  `repair_agent_pool.go`) outside the benchmark's path. Recorded as an open indicative observation: re-measure at
  the A7 boundary (G5), where a persistent increase would be commissioned.
- `TerminalArchiveTransaction` and `BlackboardModify` allocation deltas are decreases or zero: no action.

## G6 — architectural issues resolved by A3

Reviewed the Open Issues Summary (68 open, `specs/architecture/architectural-issues.md`, unchanged at
`98dc9efc2`) and the candidate sections against the A3 commits listed above. No item is wholly resolved by A3, so
`architectural-issues.md` is unchanged (no registry edit, no Completed Fixes / Fixed (Traceability) entry; owned
file untouched).

- "Control-Flow Vocabulary Bypasses Domain Ownership" (`issue-vocabulary`): stays open. A3 typed its own
  lifecycle safe-action, effect and control-sentinel vocabulary (`930ad944a`, `27103e974`, `408bb4c1a`,
  `e76d6d748`), but the issue spans later areas: raw `"doer"`/`"reviewer"`/`"orchestrator"` comparisons remain in
  production code at `98dc9efc2` (`git grep -cE '"(doer|reviewer|orchestrator)"'`: `cmd/liza/cmd_launch.go` 11,
  `internal/commands/watch.go` 8, `internal/agent/registration.go` 7, `supervisor_block.go` 6, `prompt.go` 5, plus
  five `internal/ops` files — `resolve_orchestrator.go` 3 (A3), `plan_check.go`, `quarantined_verdict.go`,
  `reaffirm_proof.go`, `submit_review.go` 1 each — of other areas). Its whole stated scope is not resolved.
- "Status Vocabulary Partition Between Go Constants and Declarative Pipeline": stays open. `internal/models/
  task_status.go` still binds `TaskStatusReady = "DRAFT_CODE"`, `TaskStatusReadyForReview = "CODE_TO_REVIEW"`,
  `TaskStatusApproved = "CODE_APPROVED"`, and `internal/pipeline/resolver_status.go:39,48,57` still converts with
  `models.TaskStatus(s.Initial)` / `s.Executing` / `s.Submitted`. A3 cites files but changes no partition
  behavior (outside C1–C6).
- "Ownership Recovery Bound to Process-Shaped Liveness Evidence": stays open. It describes the PID-liveness premise
  at `LLMAgent`/ownership boundaries, which A3's mechanical separation of release and recovery concerns
  (`8563e5070`) preserved (the A3 oracle goldens did not change in that commit; the only golden edit after the oracle freeze is lines 132 and 157 of `rebase-on-refusal-conflict.txt` in `de5c259cd`, the portability correction); the Future options (provider-supplied
  liveness predicate, ADR-0085) are untouched.
- "Raw Git Calls Bypass gitenv Hardening": stays open. Its five raw sites are in `internal/ops/init_project.go`
  and `cmd/liza/cmd_launch.go` (A7), not A3; A3 kept the inherited K1 Git sites in place by design (D49).
- "Agent Lease Renewal Ignores Configured Lease Duration" and "Restart/Lease Churn Under Load": stay open; their
  subject is the supervisor registration/heartbeat path (A9, C1), not changed by A3.
- "Decompose proceed.go (1,500 LOC)" is A4's (`issue-proceed`) and belongs to the next boundary.

## G7 — record completeness

This file holds the commands or source reads, the integration SHA (`98dc9efc270749fb130ae5dbbababecf0bfc35d0`,
for G1–G6 alike) and excerpts for G1–G6 above, and the G8 section below. The acceptance manifest
(`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-13-cp-0-boundary.json`) is written once G8 is
green (it is, per the result below).

## G8 — dispatched CI result (D52, D59): pending

Status: **green** (result appended below; the pre-result text of this section is kept as written). Under D52 (replacing the per-boundary pull request), the operator
dispatches `.github/workflows/ci.yml` (`workflow_dispatch`) at the integration revision
`98dc9efc270749fb130ae5dbbababecf0bfc35d0` (on `global-refactoring` if the head still equals it, otherwise on a
pinned `g8/a3-98dc9ef` ref under D59), and the maintainer confirms that the Linux, Windows and macOS jobs of that
one run are green. No pull request to `main` is opened or claimed, and no agent pushes. To be appended on resume:
run URL, tested full SHA (must equal the revision above, else the run is not G8 evidence), each job's name and
conclusion, UTC observation.

Known failures recorded before dispatch (operator evidence, note of 2026-10-25T04:58Z, A2 G8 run
`https://github.com/liza-mas/liza/actions/runs/40162651273` at `b3356c3c6`): two A3-lane oracle defects that merged
before the A2 boundary SHA were red on remote runners.

1. Linux and macOS: `internal/oracle/a3/lifecycle` `TestOracleUnblockTaskVariants/rebase-on-refusal-conflict/json`;
   the frozen `testdata/golden/state/unblock-task/rebase-on-refusal-conflict.txt` line 132 stored Git-version-
   dependent `stderr=rebase conflict: Rebasing (1/1)\rAuto-merging READ…`.
2. Windows rest-2: `TestOracleAssessBlockedVariants/*` and `TestOracleUnblockTaskVariants/*` failed at
   `case_test.go:71` (`start git: exec: "…\001\bin\git": executable file not found in %PATH%`); the lifecycle
   fixture's Git shim was not launchable on Windows (D57-class).

Both corrections are already in this record's revision: `de5c259cd` (a per-case normaliser for the
Git-produced rebase evidence, golden lines 132 and 157 only, and a GOOS-parameterised layout with a stdlib
`git.exe` forwarding shim for Windows) and `3ffd032fc` (path-spelling masking in the shim fidelity probes).
They are verified locally only: C1 above passed `internal/oracle/a3/lifecycle` (58.298s) at Git 2.43.0 on Linux,
which is not evidence for other Git versions, macOS or Windows. Only the G8 run proves them, so a red G8 on these
tests would be a remaining A3 defect, not a regression of this record.

If CI tests different code, G1–G7 are re-run at that revision and the old/new distinction is kept here
(documentation-only deltas identified by diff); this record is not relabeled. The `internal/agent` `TestExecutionDeadlineMarkerPastItsDeadlineWithLiveProcessIsInterrupted`
wall-clock flake on Windows rest-1 is A9 code, not A3, and is the operator's to track.

### Result (appended 2026-10-25T07:20Z)

Maintainer confirmation under the standing auto-approve delegation (operator note, 2026-10-25T07:16Z, D52 + D59):
**green on the first attempt**. The run is a D52/D59 dispatch whose tested revision equals this record's integration
revision, so no D60 rerun of failed jobs was needed. No pull request to `main` was opened or is claimed, and no
agent pushed.

- Run URL: `https://github.com/liza-mas/liza/actions/runs/40171327023`
- Event and ref: `workflow_dispatch` of `.github/workflows/ci.yml` on the pinned ref `g8/a3-98dc9ef`
  (`global-refactoring` was pushed at the same SHA).
- Tested SHA: `98dc9efc270749fb130ae5dbbababecf0bfc35d0`, equal to the integration revision of G1–G6. The only
  commits between it and this task's `HEAD` are `c7504560c` (decision record D60, documentation) and the commit that
  added this record (`git diff --stat 98dc9efc2 HEAD`: `boundary-a3.md` and the decisions file only).
- Run conclusion: `success`, attempt 1. Created 2026-10-25T07:03:09Z.

| Job | Conclusion | Completed (UTC) |
|---|---|---|
| Shared CI / Linux tests | success | 07:15:54Z |
| Shared CI / Linux lint and build | success | 07:05:01Z |
| Shared CI / macOS compatibility | success | 07:14:27Z |
| Test (windows) | success | 07:10:49Z |
| Windows shard ops-1 | success | 07:07:48Z |
| Windows shard ops-2 | success | 07:07:10Z |
| Windows shard ops-3 | success | 07:07:06Z |
| Windows shard ops-4 | success | 07:08:57Z |
| Windows shard rest-1 | success | 07:09:49Z |
| Windows shard rest-2 | success | 07:09:21Z |
| Windows shard rest-3 | success | 07:10:30Z |
| Release verified tag | skipped (tag-only job) | — |

Linux, Windows and macOS all passed in this one run (D52 replaces the separate `main`-push macOS follow-up, so the
macOS result is recorded here and no follow-up is open). The result is the maintainer's relayed observation,
recorded as supplied; this task made no provider CLI call and ran no remote command.

The run also proves on all three platforms the two A3 oracle portability corrections listed above (`de5c259cd`,
`3ffd032fc`), which were verified only locally on Linux when G1–G7 were recorded. The Windows `internal/agent`
wall-clock test that failed in the A2 run did not recur.

### Oracle platform-proof scope (D53)

Green jobs do not show that every oracle scenario ran on every platform. The committed inventory is
[`internal/testhelpers/oracle/proof-scope.md`](../../../internal/testhelpers/oracle/proof-scope.md) (read at this
task's `HEAD`; authority D53, D52). As it applies to A3 in the run above:

- **`a3/control`:** all tests are Linux-only (`linuxOnly`, `t.Skip` off Linux: process liveness, POSIX
  permissions). They ran on Linux; on Windows and macOS they were skipped, so the green jobs there prove only that
  the package compiles (non-Linux twins carry no proof).
- **`a3/lifecycle`:** Linux-only through `linuxOnly` in `env_test.go` (process and timezone proof), except
  `TestOracleUnblockTaskVariants`, `TestOracleAssessBlockedVariants` and the portability regressions
  (`portability_test.go`, `transcript_test.go`), which have no skip or build constraint and ran on all three
  platforms.
- **White-box coverage** in `internal/ops` and elsewhere carries no such restriction and ran on all three.

The CI result, tested SHA and job conclusions recorded above are unchanged by this paragraph.
