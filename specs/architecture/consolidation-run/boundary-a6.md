# Architecture Consolidation Run: Area Boundary Record A6 (Ops merge and integration, with C5)

Evidence of the A6 area-boundary checkpoint (`Area-Boundary Checkpoint Contract`, checks G1–G8) with the C5
check (`B-c5`), from the arm-1-ar-17 plan, Scope 1, task `arm-1-ar-17-cp-0-code-0`. One record per boundary; later
units never edit it. Beyond this record it changes only the acceptance manifest; the G6 outcome is a set of
non-closures, so `architectural-issues.md` is unchanged. It changes no production, test, guard-data, oracle or
baseline file. `tdd_not_required`: evidence-only unit that moves no code.

## Observed revision

- Configured integration branch: `global-refactoring` (D23; `baseline.md`). `omni-ee get config --json` reports
  `IntegrationBranch` = `"global-refactoring"`. `git rev-parse global-refactoring` and `git rev-parse HEAD` of the
  task worktree both print the revision below; `git log HEAD..global-refactoring` printed nothing at the start
  (2026-10-26T06:36Z) and `git rev-parse global-refactoring` still printed R1 at the end of G1–G6 (2026-10-26T06:57Z).
- **Checked revision (R1):** `7677139f664a83d9356029c223c16c0c5e2b6d6c`
  (`test(ops): run the C5 persisted-zone proof on Windows instead of skipping it`, the merge of
  `arm-1-ar-16-cp-0-code-0`). Every observation below was made at R1. No other integration revision is recorded.
- Observation window (UTC), task branch `task/arm-1-ar-17-cp-0-code-0`: 2026-10-26T06:36Z to 06:57Z. The worktree
  was clean before and after the suites (`git status --short` empty).
- Starting commit of the run (for A6 diffs): `96c9ad69aee983ce2b929dd681b6ba43bf920535`.
- A6 coding units and C5 merged before this unit (`omni-ee get <id> --json`, status `MERGED`, recorded merge
  commit): `arm-1-ar-15-cp-0-code-0` (A5 boundary) `01cefeded`; `-code-1` K-DV `59275d541` (refactor `f1c5afd0c`);
  `-code-2` K-INT `bce406207`; `-code-3` merge publication half `bafe7b0d6`; `-code-4` merge flow `18246b22d`;
  `-code-5` K-WT `e7cbe3206`; `-code-6` K7 inventory `f8d20669d` (with `cfa5e1c0c` and `97d17dab1`); `-code-7`
  closure and ADR 0225 `526d7783a`; C5 `arm-1-ar-16-cp-0-code-0` `7677139f6` (production commit `66f6fa871`, golden
  `7e4e9f37d`); and the A6 oracle `arm-1-ar-6-cp-0-code-0` `e994eac21`. The four A6 gap-oracle commits (`b53bc8c85`,
  `e5a4ce8fe`, `32b38e7f4`, `4b71fae3e`) precede the refactor commit each one guards.
- Go production tree since the closure commit `526d7783a` (ADR 0225): `git diff --stat 526d7783a HEAD` lists four
  files: `internal/ops/merge_finalize.go` (C5), `internal/ops/merge_c5_utc_test.go`, the C5 manifest and the oracle
  golden `internal/oracle/a6/testdata/golden/state/WtMerge/success.yaml`. The measurements ADR 0225 took at
  its coding HEAD therefore carry over to R1, and this record re-measured them independently (below) rather than
  citing them.
- Environment for every measurement: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` (GUARDRAILS G1.4), plus
  `GOFLAGS=-count=1` for G5 (the Protocol). No remote push, provider CLI, installation or update ran; output below
  is sanitized (no environment values). Host: `Linux 6.8.0-147-generic x86_64`, `nproc` 8, `go1.26.0 linux/amd64`,
  `git version 2.43.0`. Shared host with other agent sessions running.
- Scratch tooling (AST scans, SSA trace, size measure) lives under `/tmp/a6tools/` and `/tmp/a5tools/`, outside the
  repository, with the starting tree exported by `git archive 96c9ad69a` to `/tmp/a6start/`. Nothing of it is
  committed. The A6 file set is derived mechanically: the 19 files of the area partition, plus the 18 files added
  by the five A6 refactor commits (`git diff-tree --name-status` of `f1c5afd0c`, `bce406207`, `bafe7b0d6`,
  `18246b22d`, `e7cbe3206`; `git log --diff-filter=A 96c9ad69a..HEAD` over `internal/ops` adds
  `verdict_integration_evidence.go`, an A5 file, which is excluded): 37 files, equal to ADR 0225's 19 + 18.

## G1 — A6 ratchet empty; S2 exceptions data-like with rationale

Source: `internal/testguard/archguard/area_a6_test.go` read at R1:

| Field | Value |
|---|---|
| `Ratchet` | `[]RatchetEntry{}` (empty) |
| `S2` | `[]S2Entry{}` (empty): no S2 entry exists, so no category or human ruling is to be checked |
| `Layers` | `[]LayerEntry{}` (empty) |
| `Allowlist` | `[]AllowEntry{}` (empty) |

The start state was 9 keys (3 files, 6 functions; `baseline.md`, `a6-size`). All are gone. This is recorded apart
from the C0 result because the guard tolerates debt: `TestAreaA6RatchetIsEmpty` fails on any remaining ratchet key
and on an S2 entry lacking ruling or rationale. `go test -count=1 -v -run TestAreaA6RatchetIsEmpty
./internal/testguard/archguard/` printed `--- PASS: TestAreaA6RatchetIsEmpty`; the whole package passed (C0,
below). The test is present at R1 (introduced by `526d7783a`), so it is no longer prospective.

Independent measurement with `go/parser` (declaration line to closing brace of every `FuncDecl` with a body;
physical lines), over the 37 A6 production files at R1 against the 19 at `96c9ad69a`:

| | Files | Lines | Files > 800 | Functions > 100 | Longest files | Longest functions |
|---|---|---|---|---|---|---|
| `96c9ad69a` | 19 | 7,522 | 3 | 6 | `deferred_validation.go` 1,820, `wt_merge.go` 1,403, `integration_progress.go` 1,138 | `mergeWorktree` 554, `createWorktree` 120, `reconcileMergedWithOptionalAuthority` 119, `evaluateCoverage` 112, `deferredValidationRunner.bisect` 111, `performCASMerge` 110 |
| R1 | 37 | 8,407 | 0 | 0 | `integration_progress.go` 641, `integration_reconcile.go` 639, `deferred_validation_lanes.go` 565 | `reconcileIntegrationAnalyses` 100 (not over; it was 100 at the start and did not grow), `runDeferredValidation` 97, `deferredValidationRunner.execute` 95 |

Limits are 800 and 100. The start counts equal the 3 files and 6 functions of the area partition (the scratch
tool prints the five longest functions; `performCASMerge` was measured separately: 110 lines at
`wt_merge.go:673-782` at the start, 15 at `merge_publication.go:34` at R1). G1 holds: nothing remains as an S1
offender, so no S2 conversion was needed and no S2 category or human ruling is to be checked.

## G2 — A6 allowlist holds only rationale-carrying entries; no SI-5 crossing owned by A6

`Allowlist` and `Layers` are empty at R1 (table above), so no entry lacks a rationale and no stale entry exists.
The four SI-5 crossings are owned elsewhere (`crossings`, SI-5): `prompts → ops` by A8 (Scope 19),
`jsonout → ops` by A7 (Scope 17), `commands → agent` and `tui → agent` by A9 (Scope 20), `agent → db` by C6
(Scope 21, A9 data). None is assigned to A6. A6 added no production package: `go list ./internal/ops/...` prints
the single package `github.com/liza-mas/liza/internal/ops`. The K4 import-direction check ran inside `make lint`
(`lint-archguard`, `ok … 0.126s`). G2 holds.

## G3 — A6 Shim Register empty; no alias or forwarder added

The register is empty (arm-1-ar-15 plan, `Shim Register`). Input: ADR 0225 §17–18 (the closure audit,
`a6-closure`), verified independently with a scratch `go/parser` program (AST, not text search) over the 19 A6
files of the starting tree and the 37 at R1, plus a diff scan of the 37 files:

| Check | `96c9ad69a` (19 files) | R1 (37 files) |
|---|---|---|
| Type aliases (`type X = Y`, `TypeSpec.Assign` valid) | 0 | 0 |
| One-statement forwarders whose arguments equal the function's parameters | 0 | 0 |
| Added alias lines in the diff of the 37 files since `96c9ad69a` (`^\+type \w+ = `, `^\+var \w+ = \w+$`, `^\+func \w+ = `) | n/a | 1 (explained below) |
| Added lines naming `shim`, `forwarder`, `deprecated`, `temporary` or `compat alias` | n/a | 0 |
| Exported declarations of the A6 files (functions, methods on exported receivers, types, consts, vars) | 66 | 67 |
| File status since `96c9ad69a` | 19 | 18 `A`, 14 `M`, 5 unchanged (`post_worktree_setup_error`, `integration_global_surface`, `scip_indexing`, `stacklit_indexing`, `functional_clusters_indexing`), 0 deleted or renamed |

- The one added alias-shaped line is `var validateIntegrationLifecycleTransition =
  statevalidate.ValidateIntegrationLifecycleTransition` (`merge_ref_mutation.go:18`). The same variable, name and
  value, exists at the starting commit at `wt_merge.go:70`; it is the package-variable test seam the K1 trace plan
  names, which moved whole with the receipt code. It is not new and has no removal owner.
- ADR 0225 counts two forwarders, `(*candidateArtifactGuardError).Error` and
  `(*candidateArtifactGuardUnavailableError).Error` (`return e.err.Error()`). The argument-equals-parameter shape
  of this scan does not match them (error-wrapping accessors with no parameter). They exist at both revisions,
  moved with their types from `wt_merge.go:601-603`, `:616-618` to `merge_artifact_guard.go:44-46`, `:59-61`, and
  were not added.
- Seven one-statement functions are new in the A6 files (`sliceReportResolved`, `fillDetectedPostWorktreeCmd`,
  `newRequest`, `preparationRetirable`, `result`, `rollbackOwnMerge`, `createOnlyOldValue`). Each is a private
  method or helper of a new concern carrying its own logic (a verdict switch, a request constructor binding the
  carrier's fields, a boolean over carrier state, a result projection, a string repeat); none re-exports a function
  under a retained old name.
- Exported surface, printed with `go/printer` and compared as normalised text (sorted) between the two trees:
  nothing removed; the differences are exactly (matching ADR 0225 §18): the added type `IntegrationReason`; the five
  `IntegrationReason…` constants now carrying it, values unchanged; `IntegrationFailedError.Reason` typed
  `IntegrationReason` instead of `string`; and `IntegrationProgressWaitingDeferredValidation` initialised from
  `string(integrationProgressWaitingDeferred)` with the same value.
- Caller conformance (X2) for the typing sits in two A5 files only, as conversions: `review_boundary.go` and
  `submission_git_work.go` (`reason := string(IntegrationReason…)`, commit `18246b22d`).

G3 holds: zero shims added.

## K1 — in-transaction Git sites (inherited exception, D33)

Method: the `a6-k1-trace` method (plan `20261024-122154-arm-1-ar-15-cp-0.md`, K1 Transaction Trace; RP5). The scratch
SSA program of the A5 boundary record (`/tmp/a5tools/k1tool`, `golang.org/x/tools` v0.29.0 `go/packages` and
`go/ssa`, taken from the local module cache because the run is offline) was run with the A6 file list: it builds
SSA over `./internal/...` and `./cmd/...`, finds the roots (every `func(*models.State) error` passed in an A6 file
to `Blackboard.Modify`, `modifyLifecycleState` or the function returned by `lifecycleMutation(...)`), follows
static callees and closures, and treats any `internal/git` or `internal/subprocess` function and the `os/exec`
`Cmd` run methods as sinks. A K1 site is a pair (A6 transaction, inherited first-hop call) whose static closure
reaches a sink. It ran on the `git archive` of `96c9ad69a` with its 19 A6 files and on R1 with its 37. No
`DYNAMIC-ROOT` was printed in either tree.

Result: **17 A6 transactions in both trees; 1 reaches a sink in both; 1 site at the starting commit and 1 at R1,
not above the starting count.** The inherited first-hop call is identical in both trees.

| ID | Transaction at R1 (file:line, opener) | Inherited call at R1 (file:line) | Location at `96c9ad69a` (transaction, call) |
|---|---|---|---|
| K1-E1 | `recover_integration.go:114` `recoverIntegrationLocked`, `bb.Modify` | `statevalidate.ValidateCandidate(…, false, …)`, `recover_integration.go:141`, then `validate_candidate.go:32` → `validateCandidate` | `recover_integration.go:105`, `:132` |

The trace printed the call chain as far as `validateCandidate` (it prints ops frames to depth three and then
stops inside `statevalidate`); the remaining chain to `git.TreePathMode` through `checkArtifactRefFileExists` is
carried from the plan's trace table, which names it. The sink identity does not enter the counting rule.

The other 16 transactions reach no sink in either tree. Locations (start → R1):

| Transaction | At `96c9ad69a` | At R1 |
|---|---|---|
| runner claim, publish | `deferred_validation.go:798`, `:833` | `deferred_validation_runner.go:304`, `:339` |
| rule, unrule, retire, settle, abort | `deferred_validation_ruling.go:58,87,120,164,208` | `deferred_validation_ruling.go:81,110,143,187,231` |
| integration reconciliation | `integration_reconcile.go:140` | `integration_reconcile.go:154` |
| external-merge reconciliation | `reconcile_merged.go:74` | `reconcile_merged.go:73` |
| worktree base-commit record | `wt_create.go:125` | `wt_create.go:168` (`recordWorktreeBaseCommit`) |
| worktree deletion | `wt_delete.go:73` | `wt_delete.go:73` |
| merge failure record | `wt_merge.go:197` | `merge_failure.go:88` |
| mutation receipt | `wt_merge.go:352` | `merge_ref_mutation.go:42` |
| preparation retirement | `wt_merge.go:441` | `merge_admission.go:109` |
| merge preparation | `wt_merge.go:981` | `merge_preconditions.go:74` |
| final MERGED | `wt_merge.go:1255` | `merge_finalize.go:62` (`commitMerged`) |

No transaction outside K1-E1 reaches a sink, no inherited pair was added, and the merge transactions perform no
Git (CAS, sync, rollback and receipt-proof reads run outside the state lock). K1 holds: the count does not exceed
1. The result reproduces the table of ADR 0225 §22, but this record's figures come from its own run. Limits: the
trace follows static edges only (a Git call behind an interface or a closure-valued parameter is not seen, in
either tree); the plan classified the dynamic calls by source read (25 distinct kinds, identical in both trees,
ADR 0225 §21) and this record does not re-enumerate them.

## C5 — UTC merge timestamps merged and proven (B-c5)

- Merged: `arm-1-ar-16-cp-0-code-0` status `MERGED`, recorded merge commit `7677139f6` (R1). Its manifest
  `specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-16-cp-0-c5.json` names the resolved function
  and file: `recordMergeCompletion` in `internal/ops/merge_finalize.go`, called by `mergeInvocation.commitMerged`
  inside the final MERGED transaction, with the tests in `internal/ops/merge_c5_utc_test.go`.
- Production diff of the C5 work (`git diff 526d7783a HEAD -- 'internal/**' ':!*_test.go' ':!internal/oracle'`): one
  file, `merge_finalize.go`, exactly `time.Now()` → `time.Now().UTC()` at the completion handoff `Timestamp:` and
  the MERGED history `Time:`, plus the comment sentence ("local-time records" → "UTC records"). Nothing else in
  production code changed; `recordMergedDeferrals` keeps `time.Now().UTC()` and the failure timestamps are
  unchanged.
- Oracle delta: one golden, `internal/oracle/a6/testdata/golden/state/WtMerge/success.yaml`, two lines
  (`timestamp: <TIME:+02:00>` → `<TIME:Z>` and `time: <TIME:+02:00>` → `<TIME:Z>`), the reviewed C5 exception
  that asserts exactly the two values (`goal-tests`; commit `7e4e9f37d`). Between the A6 oracle's delivery
  (`e994eac21`) and R1 that is the only modified oracle file; the other 22 are additions (gap cases and goldens).
- Run at R1 (C5, validation index 5): `go test -count=1 -run '^TestC5' -v ./internal/ops/` exit 0, 2026-10-26T06:37:21Z
  to 06:37:31Z: `TestC5MergeTimestampsAreUTCInMemory`, `TestC5MergeTimestampsPersistWithZUnderNonUTCZone` and
  `TestC5LocalOffsetMergeTimestampsLoadAndSurviveWrites` (`/live`, `/archived`) each `--- PASS`; `ok …/internal/ops`.
  The oracle case `TestOracleWtMerge/success` ran live inside C1 and passed with the `<TIME:Z>` golden.

C5 holds: merged, production diff limited to the two clock readings, `^TestC5` green at R1.

## G4 — suite green at the integration revision

All commands run from the task worktree at R1 in the canonical form
`env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp <command>`, once each and not retried, from one script outside the
repository with the worktree as working directory (no translation of the canonical commands; the order differs from
the validation list only in that `^TestC5` and `make lint` ran before `make test`). None was served from the Go
test cache (0 `(cached)` lines in `make test`; the race line has no `(cached)`).

| Cmd | Command | Start–End (UTC) | Exit | Excerpt |
|---|---|---|---|---|
| C0 | `go test -count=1 ./internal/testguard/archguard/` | 06:37:20–06:37:21 | 0 | `ok …/internal/testguard/archguard 0.112s` |
| C5 | `go test -count=1 -run '^TestC5' -v ./internal/ops/` | 06:37:21–06:37:31 | 0 | three tests and two subtests `--- PASS`; `ok …/internal/ops 0.118s` |
| C2 | `make lint` | 06:37:31–06:38:23 | 0 | `go vet ./...` for `GOOS` linux, windows and darwin, then `ok …/internal/testguard/archguard 0.126s` (`lint-archguard`) |
| C1 | `make test` | 06:38:23–06:41:58 | 0 | 84 `ok` lines, 0 `FAIL`, 0 `(cached)`; `internal/ops` 178.077s, `internal/agent` 185.731s, `cmd/liza` 100.914s, `internal/commands` 33.353s, `internal/oracle/a6` 9.570s, `internal/oracle/a7/present` 4.093s, `internal/oracle/a7/install` 3.550s |
| C3 | `make test-e2e` | 06:41:58–06:43:29 | 0 | `ok …/internal/integration 78.948s` (`-race -tags e2e`, `TestFullSprintSequence`, `TestTerminalDependencyRecovery`) |
| C4 | `go test -race ./internal/ops/...` | 06:43:29–06:48:13 | 0 | `ok …/internal/ops 243.752s` (the only package under `./internal/ops/...`) |

Task validation indices 0–5 are C0, C1, C2, C3, C4 and C5. The task's canonical list is re-run by the submission on
the clean rebased commit, which is the validation of record for the manifest; the figures above are this record's
observation at the pinned revision. The A6 oracle and the other delivered oracles ran live inside C1 with every
golden unchanged except the C5 exception recorded above. Observation on Git transcripts (operator note
2026-10-25T23:31Z): no A6 golden freezes Git's own text. A search of `internal/oracle/a6/testdata` for
`Rebasing (`, `Could not apply`, `hint:`, `Auto-merging` and `CONFLICT (` printed nothing; the only file of the
package that names them is `determinism_test.go`, whose `gitTextRE` masks those lines before comparison. G4 holds:
no red suite is recorded as green.

## G5 — wall time and benchmarks against `baseline.md` (indicative; never blocks)

Protocol repeated from `baseline.md`: fresh disposable clone outside the worktrees (`/tmp/a6g5clone.4m5HDM/repo`:
`git clone --local --no-checkout <project root>` then `git checkout --detach 7677139f…`; clone `HEAD` printed
`7677139f664a83d9356029c223c16c0c5e2b6d6c`), sync-clean check (`make sync-embedded` exit 0, `git status --short`
empty, 0 bytes), then one warm-up `make test`, one timed `make test`, then `timeout 300 go test -run '^$' -bench .
-benchmem ./internal/<pkg>` under `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp GOFLAGS=-count=1`. No retry. The
clone's `git status --short` was empty afterwards and the clone was deleted (`ls -d /tmp/a6g5clone.*` matches
nothing).

- Host load average (1, 5, 15 min, `/proc/loadavg`): before warm-up `2.49 3.12 3.10`; before timed run `3.89 5.18
  4.11`; after timed run `3.13 4.89 4.25`. The baseline ran at about 30 on 8 cores, so the comparison is
  load-confounded in this run's favour.

| Run | Start (UTC) | End (UTC) | Wall s | Exit | Output |
|---|---|---|---|---|---|
| Warm-up | 06:49:00 | 06:52:32 | 212 | 0 | 84 `ok`, no `FAIL`, no `(cached)` |
| Timed | 06:52:32 | 06:55:37 | 185 | 0 | 84 `ok`, no `FAIL`, no `(cached)` |

N4: the baseline timed run was 523 s, red (two `internal/agent` deadline tests) and taken under load about 30;
+25% is 653.75 s. 185 s is 65% below it and inside the bound; against the green 444 s warm-up (a separate
observation) the timed run is 58% below and the warm-up 52% below. **Disposition:** within bound, recorded with the
baseline's load caveat, since both numbers favour this run. `internal/ops` took 176.3 s and 178.5 s in the two runs.

Benchmarks: all three packages exited 0 with `PASS` (`internal/db` 43.243s, `internal/commands` 2.963s,
`internal/agent` 3.076s); all 18 benchmark rows of the baseline were captured, none "not captured".

| Benchmark | ns/op base → now | B/op, allocs/op |
|---|---|---|
| db `BlackboardWrite` | 12033415 → 1834718 (−84.8%) | same |
| db `BlackboardModify` | 22577288 → 2001254 (−91.1%) | B/op +0.6%, allocs +0.6% |
| db `BlackboardReadCached` | 44938 → 17725 (−60.6%) | same |
| db `BlackboardReadUncached` | 1890776 → 179343 (−90.5%) | same |
| db `MarshalStateForWriteLarge/repaired=false` | 536001675 → 120755970 (−77.5%) | same |
| db `MarshalStateForWriteLarge/repaired=true` | 1331986515 → 120594282 (−90.9%) | same |
| db `ReadCachedHit` | 4916749 → 1027189 (−79.1%) | same |
| db `StateParse` | 112041835 → 26603057 (−76.3%) | same |
| db `TerminalArchiveTransaction/inline/cold-instance` | 772451364 → 243289099 (−68.5%) | same |
| db `TerminalArchiveTransaction/inline/warm` | 628528774 → 242790575 (−61.4%) | same |
| db `TerminalArchiveTransaction/archived/cold-instance` | 320733652 → 141780337 (−55.8%) | same |
| db `TerminalArchiveTransaction/archived/warm` | 185103984 → 24605996 (−86.7%) | B/op −10.0%, allocs −18.9% |
| db `TerminalArchiveTransaction/archived-256/cold-instance` | 990744276 → 549573505 (−44.5%) | same |
| db `TerminalArchiveTransaction/archived-256/warm` | 319924638 → 88013267 (−72.5%) | B/op −2.9%, allocs −5.9% |
| commands `InspectionSnapshotDuringHeartbeat/status` | 9380378 → 6275069 (−33.1%) | B/op +12.0%, allocs +6.3%; heartbeat-writes 6 → 36 |
| commands `InspectionSnapshotDuringHeartbeat/get` | 4376937 → 2251329 (−48.6%) | B/op +5.2%, allocs +4.5%; heartbeat-writes 13 → 32 |
| agent `BenchmarkClaimReviewerTask` | 34512825 → 4370965 (−87.3%) | same (B/op +0.0%, allocs −0.0%) |
| agent `BenchmarkResumeHandoff` | 5936738 → 2107673 (−64.5%) | same (B/op −0.1%, allocs −0.1%) |

"same" means within ±0.5%. **Dispositions** (N3 is Should; none blocks):

- **A6 paths.** The baseline set holds no benchmark of the A6 merge, worktree, integration-progress or
  deferred-validation operations; the Go tests and oracle cases of those paths ran in C1, C4 and the timed run
  (`internal/ops` 176–178 s per `make test`, against 178.8 s at the A5 boundary), so no A6-specific timing is
  claimed beyond that.
- **No ns/op slowdown.** Every one of the 18 rows is below its baseline (−33.1% to −91.1%); none approaches the 25%
  bound. The size of the decreases reflects the load at the baseline (about 30 against under 6 here), not a
  speed-up of the code, and is not claimed as one.
- **`InspectionSnapshotDuringHeartbeat/status` and `/get` per-iteration allocations** (B/op +12.0% and +5.2%,
  allocs +6.3% and +4.5%): out-of-bound as allocation, not attributable to A6. The figures equal those recorded at
  the A3, A4 and A5 boundaries (`boundary-a5.md` G5: +11.9% / +6.3% and +5.4% / +4.6%), measured before any A6
  refactor merged; they did not grow with A6. The benchmark overlaps a concurrent heartbeat writer, so work per
  iteration is not like-for-like (heartbeat-writes per measured window 6 → 36 and 13 → 32). It lives in
  `commands` (A7). Kept as the open indicative observation handed from A3, A4 and A5 to the A7 boundary: A7's
  refactor of `commands` is where a persistent increase is investigated and commissioned.
- **db `BlackboardModify`** B/op +0.6%, allocs +0.6%: within noise of the benchmark's own setup, in `internal/db`
  (A1), which sits below `ops` and imports none of it; A6 changes `internal/ops` only. It was +0.6% / +0.7% at the
  A5 boundary. No action.
- The other allocation deltas are decreases or zero: no action.

## G6 — architectural issues resolved by A6

Reviewed the Open Issues Summary (66 open at R1: 17 high, 33 medium, 16 low; `specs/architecture/architectural-issues.md`,
counted from the table rows) and re-grepped the whole registry (active, Completed Fixes, Fixed, Fix Details) for
the 37 A6 file names (`\b<name>\.go`) and for the A6 operation and concern names (`DeferredValidation`,
`MergeWorktree`, `wt_merge`, `IntegrationProgress`, `reconcile_merged`, `RecoverIntegration`, `CreateWorktree`,
`DeleteWorktree`, `PostWorktree`, `RunLive`, `IntegrationFailed`, `integration reconcil`, `merge path`), under
`issues-policy` (the file's Update Policy). The only file-name hit is `wt_merge.go` in the **Evidence** line of
the already-fixed "Supervisor Contention" entry (Fix Details); it is historical evidence for a closed item, not an
active finding. The concern names match only already-fixed entries ("Integration Closure Is Not Revalidated",
"Unbounded Integration Test Execution") and no active item. No active item describes an A6 decomposition (RP11
found none at the plan), so no A6 size or structure item exists to close.

**Closed: none.** No active item has its whole stated scope resolved by merged A6 work. Each non-closure and its
reason:

- "Worktree Intelligence Refresh Has Multiple Owners" (`issue-intel-refresh`): stays open (`a6-context` D-2). A6 kept
  its indexer files byte-identical (`scip_indexing.go`, `stacklit_indexing.go`, `functional_clusters_indexing.go`;
  `git diff 96c9ad69a HEAD` is empty for them) and only moved their callers into `provisionWorktree` in the same
  order. The coordinator the item asks for would unify refresh sequences owned also by task claim (A3), the
  periodic worktree health check (A9) and review submission (A5), whose ordering, enablement checks, warning text
  and failure tolerance differ; unifying them is a frozen-surface change (`goal-wont`, A6 plan D-2).
- "Control-Flow Vocabulary Bypasses Domain Ownership" (`issue-vocabulary`): stays open. A6 typed and named its own
  vocabulary (ADR 0225 §9–14: `IntegrationReason`, the integration-progress reason set, deferred-validation end
  reasons and ruling actions, named values; the K7 inventory `a6-k7-inventory.md` classifies every occurrence), but
  the issue spans every area. Raw role-type, cardinality and phase-category comparisons remain in the production
  files of A2, A7, A8 and A9, and A7 to A9 are not yet refactored. The whole stated scope is not resolved.
- "Merge Execution Authority Split": stays open. It is a documentation tension between the protocol docs (Code
  Reviewer merges on approval) and the role and supervision architecture (the supervisor executes the merge); A6
  restructured the merge code without changing who executes it or any protocol text (`goal-wont`; protocol docs
  are outside this unit's scope).
- "Duplicate Initialization Implementations", "Decompose init.go" and "Decompose watch.go": open, A7 items; their
  owners are the A7 refactor units and C4, and `issue-init` and `issue-watch` closure evidence is recorded by the
  A7 closure unit for the A8 boundary.

No registry edit is made: `architectural-issues.md` is untouched by this task, so the Open Issues Summary count
(17 high, 33 medium, 16 low; 66) stands. The Update Policy's synchronisation duties (Completed Fixes, Fixed
(Traceability), Fix Details, Summary) therefore do not arise.

## G7 — record completeness

This file holds the commands or source reads, the integration SHA R1 `7677139f664a83d9356029c223c16c0c5e2b6d6c`,
the UTC observations, exits and excerpts for G1–G6, K1 and C5, the G5 dispositions, the G6 reasons and the G8
section below. The acceptance manifest
(`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-17-cp-0-boundary.json`) is written once G8 is
green (it maps G1–G8 and the boundary obligations; it records no result itself).

## G8 — dispatched CI result (D52, D59, D60): green

Status: **green on the first attempt.** Under D52 (replacing the per-boundary pull request) the maintainer dispatched
`.github/workflows/ci.yml` (`workflow_dispatch`) on the pinned ref `g8/a6-7677139` under D59. No agent pushed,
opened a pull request or ran a remote write; the maintainer's operator notes of 2026-10-26T07:00Z (run dispatched,
result pending, task held BLOCKED) and 2026-10-26T07:11Z (result, maintainer confirmation under the standing
auto-approve delegation) are the authority, and the run was read back independently with the read-only
`gh run view 40283146712 --repo liza-mas/liza --json conclusion,event,headSha,headBranch,attempt,createdAt,jobs`
at 2026-10-26T07:13Z.

- Run: https://github.com/liza-mas/liza/actions/runs/40283146712
- Event `workflow_dispatch`; ref `g8/a6-7677139`; created 2026-10-26T07:00:23Z; attempt 1; conclusion `success`.
- **Proven SHA (tested SHA, equal to R1):** `7677139f664a83d9356029c223c16c0c5e2b6d6c`. D52/D59 conditions hold: a
  dispatch of `ci.yml` on the pinned ref, tested SHA equal to this record's integration SHA, every job green in one
  attempt. No D60 rerun was needed or counted.
- Jobs (conclusion, completedAt UTC 2026-10-26):

| Job | Conclusion | Completed |
|---|---|---|
| Shared CI / Linux tests | success | 07:10:47Z |
| Shared CI / Linux lint and build | success | 07:02:50Z |
| Shared CI / macOS compatibility | success | 07:10:32Z |
| Test (windows) | success | 07:08:10Z |
| Test (windows, ops-1) | success | 07:05:31Z |
| Test (windows, ops-2) | success | 07:04:22Z |
| Test (windows, ops-3) | success | 07:04:28Z |
| Test (windows, ops-4) | success | 07:05:07Z |
| Test (windows, rest-1) | success | 07:07:49Z |
| Test (windows, rest-2) | success | 07:03:45Z |
| Test (windows, rest-3) | success | 07:07:09Z |
| Release verified tag | skipped (tag-only job; not run, not claimed as passed) | 07:10:47Z |

- **macOS follow-up:** satisfied inside the same run, not by a later `main` push. D52 runs Linux, Windows and macOS
  in one dispatch, and `Shared CI / macOS compatibility` concluded `success` (07:10:32Z). Owner: the maintainer
  (dispatched the run). Status: closed, green. No separate `main`-push job is awaited and none is claimed as run.
- C5 on all three platforms: the same run exercised the C5 persisted-zone proof on Windows (`7677139f6`, which made
  that proof run instead of skip) as well as Linux and macOS, all green.
- **Ruling:** G8 passes. The tested code is R1, the code every other check of this record observed, so no check is
  re-run at another revision and no second revision is recorded. A6 closes; A7 coding is not held.
- **Submission base (kept separate from the proven SHA):** this record is committed on the task branch
  `task/arm-1-ar-17-cp-0-code-0`. At the read-back above, `git rev-parse global-refactoring` printed
  `7677139f664a83d9356029c223c16c0c5e2b6d6c`, equal to R1, so no rebase onto a different integration head was
  pending. The submission commit and its post-rebase review commit differ from R1 only by this record, the G8
  update and the acceptance manifest (documentation and JSON; no Go file). The proof of A6 and C5 is R1 and the
  run above; the submission's post-rebase validation is separate evidence recorded by the lifecycle.
