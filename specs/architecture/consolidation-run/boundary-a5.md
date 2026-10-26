# Architecture Consolidation Run: Area Boundary Record A5 (Ops review and acceptance)

Evidence of the A5 area-boundary checkpoint (`Area-Boundary Checkpoint Contract`, checks G1–G8) from the
arm-1-ar-15 plan, Scope 1, task `arm-1-ar-15-cp-0-code-0`. One record per boundary; later units never edit it.
Beyond this record it changes only the acceptance manifest; the G6 outcome is a set of non-closures, so
`architectural-issues.md` is unchanged. It changes no production, test, guard-data, oracle or baseline file.
`tdd_not_required`: evidence-only unit that moves no code.

## Observed revision

- Configured integration branch: `global-refactoring` (D23; `baseline.md`). `omni-ee get config --json` reports
  `IntegrationBranch` = `"global-refactoring"`. `git rev-parse global-refactoring` and `git rev-parse HEAD` of the
  task worktree both print the revision below, and `git log HEAD..global-refactoring` printed nothing at the start
  (2026-10-26T00:59Z) and at the end of G1–G6 (2026-10-26T01:20Z).
- **Checked revision (R1):** `9ce883e8a9af3f59fc3b6547aa63c9fc144e0bfe`
  (`docs(arch): record A5 closure evidence and the A5 concern-layout ADR`, the merge of
  `arm-1-ar-14-cp-0-code-8`). Every observation below was made at R1. No other integration revision is recorded.
- Observation window (UTC), task branch `task/arm-1-ar-15-cp-0-code-0`: 2026-10-26T00:59Z to 01:20Z. The worktree
  was clean before every command (`git status --short` empty before and after the suites).
- Starting commit of the run (for A5 diffs): `96c9ad69aee983ce2b929dd681b6ba43bf920535`.
- A5 coding units merged before this unit: the task `depends_on` list is all MERGED (`omni-ee get <id> --json`,
  `status`): `arm-1-ar-14-cp-0-code-0` (A4 boundary) `4bb343a10`, `-code-1` K-ACC `973909797`, `-code-2` K-SUB
  `67b1426f7`, `-code-3` K-VER `e35f003c4`, `-code-4` K-AWT doer side `2cf671c92`, `-code-5` K-AWT reviewer side
  `ce35ce28f`, `-code-6` K-OWN `47e12d912` (the task's recorded merge commit; its refactor commit is `eacedcafb`),
  `-code-7` K7 inventory `f8547570c` (with `c551553f5`), `-code-8` closure and ADR 0224 `9ce883e8a`, and the A6
  oracle `arm-1-ar-6-cp-0-code-0` `e994eac21`. The A5 supplement oracle commits (`bf2c5b5bf` … `47e12d912`,
  `git log 20fee7197..HEAD -- internal/oracle/a5`) precede the refactor commit each one guards.
- Go production tree since the coding HEAD recorded by ADR 0224 (`f8547570c`): `git diff --stat f8547570c HEAD`
  lists only `internal/testguard/archguard/area_a5_test.go`, ADR 0224, the ADR README and the closure manifest of
  `arm-1-ar-14-cp-0-code-8`. No other Go file changed, so the measurements ADR 0224 took at `f8547570c` apply to R1,
  and this record re-measured them independently (below) rather than citing them.
- Environment for every measurement: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` (GUARDRAILS G1.4), plus
  `GOFLAGS=-count=1` for G5 (translation of the Protocol; C0–C4 ran in the canonical form, below). No remote push,
  provider CLI, installation or update ran; output below is sanitized (no environment values). Host: `Linux
  6.8.0-147-generic x86_64`, `nproc` 8, `go1.26.0 linux/amd64`, `git version 2.43.0`. Shared host with other agent
  sessions running.
- Scratch tooling (AST scans, SSA trace, size measure) lives under `/tmp/a5tools/`, outside the repository, with the
  starting tree exported by `git archive 96c9ad69a` to `/tmp/a5start/`. Nothing of it is committed.

## G1 — A5 ratchet empty; S2 exceptions data-like with rationale

Source: `internal/testguard/archguard/area_a5_test.go` read at R1:

| Field | Value |
|---|---|
| `Ratchet` | `[]RatchetEntry{}` (empty) |
| `S2` | `[]S2Entry{}` (empty): no S2 entry exists, so no category or human ruling is to be checked |
| `Layers` | `[]LayerEntry{}` (empty) |
| `Allowlist` | `[]AllowEntry{}` (empty) |

The start state was 18 keys (6 files, 12 functions; `baseline.md`, `a5-size`). All are gone. This is recorded
apart from the C0 result because the guard tolerates debt: `TestAreaA5RatchetIsEmpty` fails on any remaining ratchet
key and on an S2 entry lacking ruling or rationale. `go test -count=1 -v -run 'TestAreaA5|TestArea'
./internal/testguard/archguard/` printed `--- PASS: TestAreaA5RatchetIsEmpty` (and the A4 counterpart); the whole
package passed (`ok … 0.115s`). The test is present at R1 (introduced by `9ce883e8a`), so it is no longer
prospective.

Independent measurement with `go/parser` (declaration line to closing brace of every `FuncDecl` with a body;
physical lines), over the 48 A5 production files at R1 (the 32 of the area partition plus the 16 concern files the
A5 scopes created, ADR 0224 §2) against the 32 at `96c9ad69a`:

| | Files | Lines | Files > 800 | Functions > 100 | Longest file | Longest functions |
|---|---|---|---|---|---|---|
| `96c9ad69a` | 32 | 11,351 | 6 | 12 | `acceptance_evidence.go` 1,105 | `prepareSubmitForReview` 562, `submitVerdict` 405, `claimReviewerTask` 383 |
| R1 | 48 | 12,793 | 0 | 0 | `orphaned_review_base.go` 726, then `verdict_transition.go` 518 | `runRejectionRCAOperation` 96, `executeAcceptanceCommandsWith` 94, `submitForReviewLifecycle` 93 |

Limits are 800 and 100. The start counts equal the 6 files and 12 functions of the area partition. G1 holds:
nothing remains as an S1 offender, so no S2 conversion was needed and no S2 category or human ruling is to be
checked.

## G2 — A5 allowlist holds only rationale-carrying entries; no SI-5 crossing owned by A5

`Allowlist` and `Layers` are empty at R1 (table above), so no entry lacks a rationale and no stale entry exists.
The four SI-5 crossings are owned elsewhere (`crossings`, SI-5): `prompts → ops` by A8 (Scope 19),
`jsonout → ops` by A7 (Scope 17), `commands → agent` and `tui → agent` by A9 (Scope 20), `agent → db` by C6
(Scope 21, A9 data). None is assigned to A5. A5 added no production package: `go list ./internal/ops/...` prints the
single package `github.com/liza-mas/liza/internal/ops`. The K4 import-direction check ran inside `make lint`
(`lint-archguard`, `ok … 0.126s`). G2 holds.

## G3 — A5 Shim Register empty; no alias or forwarder added

The register is empty (arm-1-ar-14 plan, `Shim Register`). Input: ADR 0224 §14–15 (the closure audit,
`a5-closure`), verified independently with a scratch `go/parser` program (AST, not text search) over the 32 A5 files
of the starting tree and the 48 at R1:

| Check | `96c9ad69a` (32 files) | R1 (48 files) |
|---|---|---|
| Type aliases (`type X = Y`, `TypeSpec.Assign` valid) | 0 | 0 |
| One-statement forwarders whose arguments equal the function's parameters | 3 | 3 |
| Added alias lines in the `internal/ops` production diff since `96c9ad69a` (`git diff 96c9ad69a HEAD -- 'internal/ops/*.go' ':!internal/ops/*_test.go'`, `^\+type \w+ = `) | n/a | 0 |
| Exported declarations of the A5 files (functions, methods on exported receivers, types, consts, vars) | 135 | 138 |

The three forwarders exist at the starting commit with identical names and are unchanged, so none was added:
`AcceptanceAllocationRef` (now in `acceptance_allocation_review.go`, formerly `acceptance_evidence.go`; the
exported wrapper of the private function), `ShortCommit` (`repair_acceptance_commits.go`) and
`SubmitVerdictWithAuthorityAndOptions` (`submit_verdict.go`). ADR 0224 counts a fourth,
`(*acceptanceBatchTimeoutError).Error` (`acceptance_execution.go`, `return e.err.Error()`), an error-wrapping
accessor that the argument-equals-parameter shape of this scan does not match; it is present and unchanged at both
revisions (`acceptance_execution.go:155` at the start, `:174` at R1).

Exported surface, printed with `go/printer` and compared as normalised text (sorted) between the two trees: nothing
removed; the differences are exactly (matching ADR 0224 §15):

- the three added types `AwaitVerdictOutcome`, `AwaitSafeAction`, `AwaitResubmissionOutcome`, and their 17
  constants (`Verdict*`, `Resubmission*`, `SafeAction*`) now carrying them, values unchanged;
- `ReviewerClaimPolicyResolver.ProviderDiversity(string, string) (string, error)` →
  `ProviderDiversity(string, pipeline.ReviewImpact) (pipeline.ProviderDiversityMode, error)` (A1 conformance, the
  C2 site's logic untouched);
- `DefaultArchiveLimit` built from the named constants `defaultArchiveMaxTasks` and `defaultArchiveMaxBytes`, same
  value (`MaxTasks: 8`, `MaxBytes: 4 << 20`).

G3 holds: zero shims added. The 16 declared concern files all appear as additions (`git diff --name-status
96c9ad69a HEAD`, status `A`), and no A5 file was deleted or renamed.

## K1 — in-transaction Git sites (inherited exceptions, D33)

Method: the `a5-k1-trace` method (plan `20261024-113541-arm-1-ar-14-cp-0.md`, K1 Transaction Trace; RP19). A scratch
program outside the repository loads `./internal/...` and `./cmd/...` with `golang.org/x/tools` v0.29.0
(`go/packages`, `go/ssa`; its own `x/sync` v0.15.0 and `x/mod v0.22.0`, taken from the local module cache because
the run is offline; they are not analysed code), builds SSA, and finds the roots: every
`func(*models.State) error` passed in an A5 file to `Blackboard.Modify`, `modifyLifecycleState` or the function
returned by `lifecycleMutation(...)` (a bound method value is unwrapped to its method). Edges are static callees
and closures. Sinks are any `internal/git` function, `internal/subprocess` and the `os/exec` `Cmd` run methods.
A K1 site is a pair (A5 transaction, inherited first-hop call) whose static closure reaches a sink; a private step
the transaction calls is followed (up to three levels printed) to the inherited call, as the counting rule
permits. Dynamic calls (interface methods, closure-valued parameters other than the transaction callback) are
not followed; the plan classified them by source read and this record carries that classification. Run on the
`git archive` of `96c9ad69a` with its 32 A5 files and on R1 with its 48.

Result: **31 A5 transactions in both trees; 9 reach a sink in both; 13 inherited calls in both, plus K1-E6 in
the A9 transaction: 14 sites at the starting commit and 14 at R1, not above the starting count.** The inherited
first-hop calls per transaction are identical in both trees.

| ID | Transaction at R1 (file:line, opener) | Inherited call at R1 (file:line) | Location at `96c9ad69a` (transaction, call) |
|---|---|---|---|
| K1-E1 | `claim_reviewer_task.go:205` `(*reviewerClaim).attempt`, `lifecycleMutation` | `CheckCurrent`, `claim_reviewer_task.go:302` | `claim_reviewer_task.go:240`, `:436` |
| K1-E2 | same | `validateReviewBoundaryForAssignment`, `reviewer_claim_candidate.go:64` | `:240`, `claim_reviewer_task.go:379` |
| K1-E10 | same | `ValidationRetryPending`, `reviewer_claim_selection.go:118` | `:240`, `claim_reviewer_task.go:290` |
| K1-E3 | `review_reclaim.go:65` `reclaimForReview`, `modifyLifecycleState` | `CheckCurrent`, `review_reclaim.go:107` | `await_resubmission.go:662`, `:666` |
| K1-E4 | same | `validateReviewBoundaryForAssignment`, `review_reclaim.go:136` | `:662`, `await_resubmission.go:687` |
| K1-E5 | `validation_readiness.go:100` `recordReadiness`, `bb.Modify` | `CheckCurrent`, `validation_readiness.go:101` | `validation_preflight.go:343`, `:344` |
| K1-E6 | `internal/agent/systemctl.go` provider-start transaction (A9) | `CheckLaunchCurrent`, `systemctl.go:269` → `CheckCurrent` | `systemctl.go:269` (same line) → `validation_preflight.go:125` |
| K1-E7 | `submission_finalize.go:105` `(*submissionRun).publish`, `modifyLifecycleState` | `recheckAcceptanceReceipt`, `submission_finalize.go:185` | `submit_review.go:531`, `:573` |
| K1-E11 | same | `validateBoundedContractCorrection`, `submission_finalize.go:179` | `:531`, `submit_review.go:566` |
| K1-E8 | `update_review_commit.go:117` `updateReviewCommitLocked`, `bb.Modify` | `recheckAcceptanceReceipt`, `update_review_commit.go:233` | `update_review_commit.go:152`, `:170` |
| K1-E9 | `orphaned_review_base.go:527` `(*orphanHeal).publish`, `modifyLifecycleState` | `recheckAcceptanceReceipt`, `orphaned_review_base.go:538` | `orphaned_review_base.go:517`, `:528` |
| K1-E12 | `reaffirm_proof.go:59` `ReaffirmProof`, `Modify` | `observeProofDrift`, `reaffirm_proof.go:71` | `reaffirm_proof.go:54`, `:66` |
| K1-E13 | `rejection_rca.go:233` `runRejectionRCAOperation`, `modifyLifecycleState` | `statevalidate.ValidateCandidate`, `rejection_rca.go:267` | `:233`, `:267` |
| K1-E14 | `submit_verdict.go:476` `(*verdictAttempt).commit`, `modifyLifecycleState` | `validateIntegrationLifecycleCandidate`, `verdict_transition.go:334` (reached from `apply`, `verdict_transition.go:177`) | `submit_verdict.go:456`, `:666` |

Sinks the trace reaches through these calls: `GetCommitSHA` (`CheckCurrent`, `recheckAcceptanceReceipt`,
`ValidationRetryPending`), `ResolveCommit` (`observeProofDrift`) and the `statevalidate.ValidateCandidate` →
`git.TreePathMode` path were seen in the printed chains; `GetMergeBase` (`validateReviewBoundaryForAssignment`) and
`DiffFiles` (`validateBoundedContractCorrection`) are carried from the plan's trace table, which names them, since
the printed chains stopped at the A5 private helpers. The sink identity does not enter the counting rule.

The other 22 transactions reach no sink in either tree. At R1: `acceptance_claim_block.go:157`,
`archive_acceptance_receipts.go:86`, `await_verdict.go:368`, `await_verdict_finish.go:51,185`,
`clear_stale_review_claims.go:46`, `orphaned_review_base.go:417,649`, `quarantined_verdict.go:155,342`,
`repair_acceptance_commits.go:126`, `repair_review_ownership.go:31`, `review_reservation.go:115,168,201,248`,
`submission_git_work.go:214,383`, `validation_preflight.go:423,490`, `verdict_failure_anomaly.go:71,126`. At
the starting commit the 22 are `acceptance_claim_block.go:141`, `archive_acceptance_receipts.go:78`,
`await_resubmission.go:198,442,495,531`, `await_verdict.go:344,360,871`, `clear_stale_review_claims.go:34`,
`orphaned_review_base.go:407,639`, `quarantined_verdict.go:142,329`, `repair_acceptance_commits.go:119`,
`repair_review_ownership.go:31`, `submit_review.go:360,752`, `submit_verdict.go:972,1032`,
`validation_preflight.go:453,520`.

No transaction outside the table reaches a sink, no inherited pair was added, and the two trees show the same
first-hop calls per transaction. K1 holds: the count does not exceed 14. The result reproduces the table of ADR 0224
§17 (which used the same method), but this record's figures come from its own run. Limits: the trace follows static
edges only (a Git call behind an interface or a closure-valued parameter is not seen, in either tree), and it
counts pairs per the rule, not every transitive helper.

## G4 — suite green at the integration revision

All commands run from the task worktree at R1 in the canonical form
`env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp <command>`; each was run once and not retried. The three
`make` commands and the race command ran from one script (outside the repository) as
`make -C <worktree> <target>` and `go -C <worktree> test …`, the translation of the canonical commands without
`cd`. None was served from the Go test cache (0 `(cached)` lines in `make test`; the race line has no
`(cached)`).

| Cmd | Command | Start–End (UTC) | Exit | Excerpt |
|---|---|---|---|---|
| C0 | `go test -count=1 ./internal/testguard/archguard/` | ≈00:59–01:00 | 0 | `ok .../internal/testguard/archguard 0.115s`; `-run 'TestAreaA5|TestArea' -v`: `TestAreaA5RatchetIsEmpty` `--- PASS` |
| C1 | `make test` | 01:00:08–01:04:02 | 0 | 84 `ok` lines, 0 `FAIL`, 0 `(cached)`; `internal/ops` 178.766s, `internal/agent` 189.990s, `cmd/liza` 104.168s, `internal/commands` 36.779s, `internal/oracle/a5` 9.062s, `internal/oracle/a5/supplement` 8.983s, `internal/oracle/a6` 4.306s |
| C2 | `make lint` | 01:04:02–01:04:47 | 0 | `go vet ./...` for `GOOS` linux, windows and darwin, then `ok .../internal/testguard/archguard 0.126s` (`lint-archguard`) |
| C3 | `make test-e2e` | 01:04:47–01:06:20 | 0 | `ok .../internal/integration 79.170s` (`-race -tags e2e`, `TestFullSprintSequence`, `TestTerminalDependencyRecovery`) |
| C4 | `go test -race ./internal/ops/...` | 01:06:20–01:11:02 | 0 | `ok .../internal/ops 241.584s` (the only package under `./internal/ops/...`) |

Task validation indices 0–4 are C0–C4. The task's canonical list is re-run by the submission on the clean rebased
commit, which is the validation of record for the manifest; the figures above are this record's observation at the
pinned revision. The A5 oracle (`internal/oracle/a5`, `internal/oracle/a5/supplement`) and the A6 oracle ran live
inside C1 with every golden unchanged. Observation on Git transcripts (operator note 2026-10-25T23:31Z): no
A5 golden under `internal/oracle/a5` freezes Git's rebase transcript; the OH2 golden carries the placeholder
`<git-rebase-transcript>` (`47e12d912`, masker `transcript_mask_test.go`), and the `recovery_hint` lines in the RW4
and SB6 goldens are the engine's own text. G4 holds: no red suite is recorded as green.

## G5 — wall time and benchmarks against `baseline.md` (indicative; never blocks)

Protocol repeated from `baseline.md`: fresh disposable clone outside the worktrees
(`/tmp/a5g5clone.MsOipq/repo`: `git clone --local --no-checkout <project root>` then `git checkout --detach
9ce883e8…`; clone `HEAD` printed `9ce883e8a9af3f59fc3b6547aa63c9fc144e0bfe`), sync-clean check (`make
sync-embedded` exit 0, `git status --short` empty, 0 bytes), then one warm-up `make test`, one timed `make test`,
then `timeout 300 go test -run '^$' -bench . -benchmem ./internal/<pkg>` under `env -i HOME="$HOME" PATH="$PATH"
TMPDIR=/tmp GOFLAGS=-count=1`. No retry. The clone's `git status --short` was empty afterwards and the clone was
deleted (`ls -d /tmp/a5g5clone.*` matches nothing).

- Host load average (1, 5, 15 min, `/proc/loadavg`): before warm-up (01:11:44Z) `2.83 3.04 3.30`; before timed run
  (01:15:19Z) `4.55 5.73 4.52`; after timed run (01:18:24Z) `2.35 4.92 4.50`. The baseline ran at about 30 on 8
  cores, so the comparison is load-confounded in this run's favour.

| Run | Start (UTC) | End (UTC) | Wall s | Exit | Output |
|---|---|---|---|---|---|
| Warm-up | 01:11:44 | 01:15:19 | 215 | 0 | 84 `ok`, no `FAIL`, no `(cached)` |
| Timed | 01:15:19 | 01:18:24 | 185 | 0 | 84 `ok`, no `FAIL`, no `(cached)` |

N4: the baseline timed run was 523 s, red (two `internal/agent` deadline tests) and taken under load about 30;
+25% is 653.75 s. 185 s is 65% below it and inside the bound; against the green 444 s warm-up (a separate
observation) the timed run is 58% below and the warm-up 52% below. **Disposition:** within bound, recorded with
the baseline's load caveat, since both numbers favour this run. `internal/ops` took 178.0 s and 181.3 s in the
two runs.

Benchmarks: all three packages exited 0 with `PASS` (`internal/db` 40.308s, `internal/commands` 3.306s,
`internal/agent` 3.290s); all 18 benchmark rows of the baseline were captured, none "not captured".

| Benchmark | ns/op base → now | B/op, allocs/op |
|---|---|---|
| db `BlackboardWrite` | 12033415 → 1801521 (−85.0%) | same |
| db `BlackboardModify` | 22577288 → 1998916 (−91.1%) | B/op +0.6%, allocs +0.7% |
| db `BlackboardReadCached` | 44938 → 17208 (−61.7%) | same |
| db `BlackboardReadUncached` | 1890776 → 174284 (−90.8%) | same |
| db `MarshalStateForWriteLarge/repaired=false` | 536001675 → 117612123 (−78.1%) | same |
| db `MarshalStateForWriteLarge/repaired=true` | 1331986515 → 121608267 (−90.9%) | same |
| db `ReadCachedHit` | 4916749 → 1054594 (−78.6%) | same |
| db `StateParse` | 112041835 → 26510096 (−76.3%) | same |
| db `TerminalArchiveTransaction/inline/cold-instance` | 772451364 → 238825612 (−69.1%) | same |
| db `TerminalArchiveTransaction/inline/warm` | 628528774 → 244733018 (−61.1%) | same |
| db `TerminalArchiveTransaction/archived/cold-instance` | 320733652 → 144958570 (−54.8%) | same |
| db `TerminalArchiveTransaction/archived/warm` | 185103984 → 25318549 (−86.3%) | B/op −9.9%, allocs −18.8% |
| db `TerminalArchiveTransaction/archived-256/cold-instance` | 990744276 → 545363011 (−45.0%) | same |
| db `TerminalArchiveTransaction/archived-256/warm` | 319924638 → 86856978 (−72.9%) | B/op −2.9%, allocs −5.9% |
| commands `InspectionSnapshotDuringHeartbeat/status` | 9380378 → 6608756 (−29.5%) | B/op +11.9%, allocs +6.3%; heartbeat-writes 6 → 39 |
| commands `InspectionSnapshotDuringHeartbeat/get` | 4376937 → 2245593 (−48.7%) | B/op +5.4%, allocs +4.6%; heartbeat-writes 13 → 38 |
| agent `BenchmarkClaimReviewerTask` | 34512825 → 4308413 (−87.5%) | same (B/op +0.0%, allocs −0.0%) |
| agent `BenchmarkResumeHandoff` | 5936738 → 2655009 (−55.3%) | same (B/op −0.1%, allocs −0.1%) |

"same" means within ±0.5%. **Dispositions** (N3 is Should; none blocks):

- **A5 path.** `BenchmarkClaimReviewerTask` calls the A5 reviewer claim (K-OWN): −87.5% ns/op, B/op and allocs
  unchanged within 0.1%, so the refactor added no per-iteration allocation there. Other A5 operations (submit,
  verdict, await, acceptance) have no benchmark of their own in the baseline set; the Go tests of those paths ran
  in C1 and the timed run.
- **No ns/op slowdown.** Every one of the 18 rows is below its baseline (−29.5% to −91.1%); none approaches the
  25% bound. The size of the decreases reflects the load at the baseline (about 30 against under 6 here), not a
  speed-up of the code, and is not claimed as one.
- **`InspectionSnapshotDuringHeartbeat/status` and `/get` per-iteration allocations** (B/op +11.9% and +5.4%,
  allocs +6.3% and +4.6%): out-of-bound as allocation, not attributable to A5. The figures equal those recorded at
  the A3 boundary (`boundary-a3.md` G5: +12.5% / +6.6% and +5.0% / +4.3%) and the A4 boundary (`boundary-a4.md` G5:
  +12.5% / +6.4% and +6.7% / +5.3%), measured before any A5 refactor merged; they did not grow with A5. The
  benchmark overlaps a concurrent heartbeat writer, so work per iteration is not like-for-like (heartbeat-writes
  per measured window 6 → 39 and 13 → 38). The benchmark lives in `commands` (A7). Kept as the open indicative
  observation the A3 and A4 records handed to the A7 boundary: re-measure there; a persistent increase is
  commissioned then.
- **db `BlackboardModify`** B/op +0.6%, allocs +0.7%: within noise of the benchmark's own setup, in `internal/db`
  (A1), which sits below `ops` and imports none of it; A5 changes `internal/ops` only. It was +0.5% / +0.6% at the
  A4 boundary. No action.
- The other allocation deltas are decreases or zero: no action.

## G6 — architectural issues resolved by A5

Reviewed the Open Issues Summary (66 open at R1, `specs/architecture/architectural-issues.md`) and re-grepped the
whole registry (active, Completed Fixes, Fixed, Fix Details) for the 48 A5 file names (`\b<name>\.go`) and for
the A5 operation names (`await_verdict`, `await_resubmission`, `ClaimReviewerTask`, `SubmitForReview`,
`SubmitVerdict`, `prepareSubmitForReview`, `acceptance_evidence`), under `issues-policy` (the file's Update
Policy). The file names match only these lines: `submit_review.go` (the "INVARIANTS.md §7 Clean Worktree" item,
Issue and Direction), `submit_verdict.go` (the "INVARIANTS.md §6 Provider Diversity" item, Direction),
`clear_stale_review_claims.go` (the "Ownership Recovery Bound to Process-Shaped Liveness Evidence" item and the
already-fixed "Review Lease Orphaning Without Automatic Reclamation" Evidence line) and `claim_reviewer_task.go`
(the "Custom Roles Render Empty Prompt Blocks" item). The operation names match only the diversity item. No active
item describes an A5 decomposition, so no A5 size or structure item exists to close.

**Closed: none.** No active item has its whole stated scope resolved by merged A5 work. Each non-closure and its
reason:

- "INVARIANTS.md §7 Clean Worktree Not Enforced at Submission" (`issue-clean-worktree`): stays open. The issue
  is that `submit_review.go` performs no `git status` cleanliness check. A5 preserved that behaviour (D-5 of the
  plan; `goal-wont`, X9: adding the check is a behaviour change outside C1–C6), and ADR 0224 §19 records that no
  submission file at R1 runs the check (only the strict acceptance gate `checkAcceptanceWorktree` refuses a dirty
  worktree). The stated gap is unchanged. Re-verified here: no A5 file at R1 contains a `porcelain` status call
  (`grep -n 'porcelain\|"status"'` over the 48 files printed nothing).
- "INVARIANTS.md §6 Provider Diversity Not Enforced at Verdict Time" (`issue-verdict-diversity`): stays open for
  the same reason. `submit_verdict.go` and `verdict_transition.go` still evaluate quorum count only; diversity
  remains enforced at claim time and preferred at merge time. A5 moved the C2 comparisons into one file
  (`reviewer_claim_selection.go`) without changing them; C2 changes provider keys, not this enforcement point.
- "Control-Flow Vocabulary Bypasses Domain Ownership" (`issue-vocabulary`): stays open. A5 typed and named its
  own vocabulary (ADR 0224 §7–10: the three await types, `submittedVerdict`, the validation sets, named values; the
  K7 inventory scanned 1,340 literals in the 48 files and left none unclassified), and the A5 files contain no
  raw role-type literal (`"doer"`, `"reviewer"`, `"orchestrator"`) at R1. The issue spans every area, and raw
  role-type comparisons remain in other areas' production files at R1: `internal/agent/registration.go` (7),
  `internal/commands/watch.go` (6), `internal/agent/prompt.go` (4), `internal/agent/cli.go` (4),
  `internal/agent/systemctl.go` (3), `internal/agent/strategy.go` (3), `internal/rolemodels/rolemodels.go` (2),
  `internal/prompts/builder.go` (2) and others (A2, A7, A8, A9; `grep -rnE '(==|!=) "(doer|reviewer|orchestrator)"
  |case "…"' cmd internal`, excluding tests). The whole stated scope is not resolved.
- "Ownership Recovery Bound to Process-Shaped Liveness Evidence": stays open. It cites `clear_stale_review_claims.go`
  as one place where lease-first recovery already applies; that behaviour is unchanged by A5 and the item's
  residual concern (a non-process-shaped backend) is outside the run.
- "Custom Roles Render Empty Prompt Blocks": stays open. It cites `ops/claim_reviewer_task.go:61` as the one
  production path that consults the closed role registry (`roles.IsValid`, now `claim_reviewer_task.go:148`,
  `:173` at the starting commit, so the item's line number was already stale before A5; recorded as drift, not
  edited here). Custom-role prompt behaviour is explicitly Won't for this run (`goal-wont`), and A5 changed no
  role-name handling (configured role names stay dynamic, ADR 0224 §11).
- "Worktree Intelligence Refresh Has Multiple Owners" (`issue-intel-refresh`): stays open. Review submission
  still carries its own wrappers around the SCIP, Stacklit and functional-clusters refreshes
  (`submit_review.go:33-35`, `refreshSubmitReviewScipIndexes`), and the claim, worktree-creation and health-check
  owners lie in A3, A6 and A9. Consolidating them changes warning text and failure tolerance, a frozen-surface
  change (`goal-wont`; A6 plan D-2).

No registry edit is made: `architectural-issues.md` is untouched by this task, so the Open Issues Summary count
(17 high, 33 medium, 16 low; 66) stands. The Update Policy's synchronisation duties (Completed Fixes, Fixed
(Traceability), Fix Details, Summary) therefore do not arise.

## G7 — record completeness

This file holds the commands or source reads, the integration SHA R1 `9ce883e8a9af3f59fc3b6547aa63c9fc144e0bfe`,
the UTC observations, exits and excerpts for G1–G6 and K1, the G5 dispositions, the G6 reasons and the G8 section
below. The acceptance manifest
(`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-15-cp-0-boundary.json`) is written once
G8 is green (it maps G1–G8 and the four boundary obligations; it records no result itself).

## G8 — dispatched CI result (D52, D59, D60): pending

Status: **pending the maintainer.** Under D52 (replacing the per-boundary pull request) the operator dispatches
`.github/workflows/ci.yml` (`workflow_dispatch`) on a pinned ref under D59, with `git rev-parse global-refactoring`
as the tested SHA; D52 runs Linux, Windows and macOS in the same run, so no separate `main`-push macOS follow-up
is owed. G8 is green only when every job passes in one run on the SHA recorded above, or on the documented
refresh SHA if `global-refactoring` has moved by then (G1–G7 are then re-run at that SHA and both are kept). No
agent pushes, opens a pull request or runs a remote command; this task claims no CI result. This section is
completed on resume with the run URL, event and ref, tested SHA, per-job results, the UTC observation and the
ruling.
