# Architecture Consolidation Run: Area Boundary Record A1 (State core)

Evidence of the A1 area-boundary checkpoint (`Area-Boundary Checkpoint Contract`, checks G1–G8) from the
arm-1-ar-11 plan, Scope 1, task `arm-1-ar-11-cp-0-code-0`. One record per boundary; later units never edit
it. It changes no production, test, guard-data, oracle or baseline file.

## Observed revision

- Configured integration branch: `omni-ee -C <project root> get config --json`, field
  `IntegrationBranch` = `"global-refactoring"` (observed 2026-10-24T21:21Z); equals the D23 ruling cited by
  `baseline.md`.
- Integration revision under test (`git rev-parse global-refactoring` at 2026-10-24T21:21Z, after the task
  branch was rebased onto it; `git log HEAD..global-refactoring` printed nothing):
  `265a14b3ae563d01175eb68d8a98c3501d905f71` (`fix(oracle-regen): exclude C packages by resolved directory`).
  The task worktree `HEAD` is this revision plus the commits that add this record and the acceptance manifest;
  `git diff --stat 265a14b3a HEAD` lists only those two files.
- Refresh history (earlier evidence is superseded, not relabeled). First set at
  `92d39398e89bfb7616882d35848119057c3fba0a`; refreshed at `79e22369a7beb3aa1a552819a7b5f8c0686a04e7` and
  `2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16` after the D53 oracle-portability and D52 timing repairs; refreshed
  again here at `265a14b3a` after the D53 follow-up (3 outputs), D57, the timing follow-up and the D56 Windows
  fix merged. A red dispatched run at `79e22369a` (operator note 2026-10-24T17:47Z) is why the earlier
  revisions never reached G8 closure.
- What changed since `2f8b7a7b0`: `git diff --name-only 2f8b7a7b0 265a14b3a -- '*.go' Makefile .github` lists 14
  files, all test code or oracle-regeneration tooling (`internal/ops/submit_progress_test.go`,
  `internal/oracle/a1/cli/main_test.go`, `internal/oracle/a5/driver_test.go`,
  `internal/testhelpers/oracle/cmd/regen/**`); plus `internal/testhelpers/oracle/proof-scope.md` and specs. No
  Makefile or workflow change. Because test sets changed, C0–C4 and G5 were rerun at `265a14b3a`, not carried over.
- A1 code and guard data did not change across any refresh: `git diff --stat 79e22369a 265a14b3a` and
  `git diff --stat 2f8b7a7b0 265a14b3a` over the 24 A1 package directories print nothing, and
  `git diff --stat 79e22369a 265a14b3a -- internal/testguard specs/architecture/architectural-issues.md Makefile`
  prints nothing.
- Starting commit of the run (for A1 diffs): `96c9ad69aee983ce2b929dd681b6ba43bf920535`. Refresh started
  2026-10-24T21:12Z.
- A1 coding units, merged before this unit (`git log --no-merges <start>..HEAD` over the 24 A1 package
  paths): `25b9cf48a` models, `800bef47a` pipeline, `be8a4289a` statevalidate, `cf36edcc8`
  referencecontract, `969d82ad4` db, `eebdbff8a` V1–V5/V11–V12, `91ad8a41e` V6–V10, `367a9b5ca` and
  `2143802bd` named-value adoption (plus the ADR task).
- Environment for every measurement: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` (GUARDRAILS G1.4). No
  remote push, provider CLI, installation or update ran; output below is sanitized (no environment values).

## G1 — A1 ratchet empty; S2 exceptions data-like with rationale

- Source: `internal/testguard/archguard/area_a1_test.go` at `265a14b3a` (read in full, 36 lines).
  Excerpt (lines 33–35):

  ```go
  Allowlist: []AllowEntry{},
  Ratchet:   []RatchetEntry{},
  S2:        []S2Entry{},
  ```

- A1 Ratchet: none (paired with the guard run below, since the guard alone permits ratchet debt). A1 S2:
  none, so there is no S2 entry needing a category or rationale. The start snapshot's A1 count was 3 files /
  7 functions (`baseline.md`); all were removed rather than converted to S2.
- Independent size check: largest non-test files in the 24 A1 packages are `internal/db/blackboard.go`
  (786 lines < 800), `internal/usage/report.go` 765, `internal/analysis/patterns.go` 690.
- C0: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 ./internal/testguard/archguard/`,
  2026-10-24T21:12:41Z, exit 0: `ok  github.com/liza-mas/liza/internal/testguard/archguard  0.180s`.
- Result: pass.

## G2 — A1 allowlist empty; no SI-5 crossing owned by A1

- Source: same file, `Allowlist: []AllowEntry{}`: empty, so no entry lacks a rationale. Layer entries cover
  all 24 A1 packages (domain: models, pipeline, referencecontract, roles, taskkind; leaf: brand, errors,
  identity, log, paths, render; infrastructure: the other 13).
- SI-5 (`20261023-162147-arm-1.md#SI-5 K3 Crossing Resolutions`, pinned `74212192d`) assigns its four
  crossings to Scope 19 (A8), Scope 17 (A7), Scope 20 (A9) and Scope 21 (C6, in A9); `arch-crossings` names
  none for A1. Result: no A1-owned crossing.
- The C0 `TestK4ImportDirectionGuard` pass above (the package passes) also proves no unlisted forbidden edge
  and no stale allowlist entry exists. Result: pass.

## G3 — A1 Shim Register empty; no alias or forwarder added

- Register: `20261023-192211-arm-1-ar-10.md#Shim Register` (pinned `2226d2668`) reads "None."
- Inspection, `git diff -U0 96c9ad69a 265a14b3a -- <24 A1 package dirs> ':!*_test.go'`; the numstat over the
  24 directories including tests is 117 files, 7895 insertions, 5100 deletions, identical to the figures at
  `92d39398e` and `79e22369a` (A1 code did not change in the refreshes):
  - added `type X = Y` alias declarations: 0 (`grep -cE '^\+\s*type\s+\w+(\[[^]]*\])?\s+=\s'`, count 0).
  - added lines containing deprecated / shim / forwarder / compat alias / temporary markers: 0
    (`grep -ciE`, count 0).
  - added one-line `return f(...)` functions (candidate forwarders): 21 found by script at the first evidence
    set; the diff is unchanged, so the set is the same. 20 exist at the starting commit with the same
    one-line body (moved by a file split, or a string literal replaced by a named constant, e.g.
    `roleNamesByType("doer")` → `roles.RoleTypeDoer`): `IsSprintTerminal`, `CanPipelineTransition`,
    `IsPipelineSprintTerminal`, `HasRole`, `ValidateValidationCommands`, `ValidateValidationSafety`,
    `Doer/ReviewerRoleNames`, `IsDeclaredState`, `Available{Manual,Auto}Transitions`,
    `OutputConsumerRolePairs[ForOutput]`, the six `statusClassifier` `Is*` methods,
    `collapseRejectionRCAText`. The 21st, `hasInlineTag` (`internal/db/yaml_write.go`, added by `969d82ad4`),
    is a private predicate with two callers in the same file; it keeps no old name and forwards to nothing
    that moved. None preserves a relocated API, so none is a temporary shim.
- Result: pass; no alias or forwarder was added by A1 coding.

## G4 — suite green at the integration revision

All five canonical commands ran at `265a14b3a` in the task worktree (clean before; `git status --short` empty
afterwards), under the `env -i` form. C1–C4 add `GOFLAGS=-count=1` to the same `env -i` command (translation of
the canonical form, so no result is served from the Go test cache). Wall times are shared-host, not
baseline-comparable (see G5 for that).

| Cmd | Command | Start (UTC) | End (UTC) | Exit | Excerpt |
|---|---|---|---|---|---|
| C0 | `go test -count=1 ./internal/testguard/archguard/` | 21:12:41 | 21:12:41 | 0 | `ok .../internal/testguard/archguard 0.180s` |
| C1 | `make test` (+`GOFLAGS=-count=1`) | 21:12:44 | 21:16:39 | 0 | 83 `ok` lines, 0 `FAIL`, 0 `(cached)`; last: `ok .../scripts/ci/testshard 0.254s` |
| C2 | `make lint` (+`GOFLAGS=-count=1`) | 21:19:30 | 21:19:32 | 0 | `go vet ./...` for `GOOS=linux`, `windows`, `darwin`; `ok .../internal/testguard/archguard 0.119s` |
| C3 | `make test-e2e` (+`GOFLAGS=-count=1`) | 21:19:32 | 21:20:51 | 0 | `ok .../internal/integration 78.427s` (`TestFullSprintSequence`, `TestTerminalDependencyRecovery`, `-race -tags e2e -count=1`) |
| C4 | `go test -race <24 A1 packages>` (+`GOFLAGS=-count=1`) | 21:20:51 | 21:21:06 | 0 | 23 `ok`, 0 `(cached)`, `internal/taskkind [no test files]` |

C4 packages, all `ok`: models 1.802s, roles 1.018s, pipeline 1.802s, statevalidate 5.902s, statehygiene
1.016s, payloadschema 1.088s, referencecontract 1.043s, runtimeinputs 1.023s, db 12.559s, filelock 2.735s,
archiveobject 1.022s, atomicfile 1.009s, identity 1.009s, errors 1.008s, paths 1.024s, render 1.010s, log
1.723s, alerts 3.622s, usage 2.572s, analysis 1.016s, envgate 1.007s, brand 1.008s, rolemodels 1.011s;
`internal/taskkind` has no test files (as at the start). `make test` and `make lint` print `make sync-embedded`
output (`Files synced successfully`) and leave the tree unchanged.
Result: pass. Remote Windows and macOS results are G8's, not G4's.

## G5 — wall time and benchmarks against `baseline.md` (indicative; never blocks)

Protocol repeated from `baseline.md`: fresh disposable clone outside the worktrees
(`git clone --local --no-checkout <project root>` then `git checkout --detach 265a14b3a…`; clone `HEAD`
printed `265a14b3ae563d01175eb68d8a98c3501d905f71`; clone `git status --short` empty afterwards; clone
deleted, `ls -d /tmp/a1g5base.*` matched nothing), `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp
GOFLAGS=-count=1`, one warm-up `make test`, one timed `make test`, then
`timeout 300 go test -run '^$' -bench . -benchmem ./internal/<pkg>`. Measured at the integration revision itself.

- Host: `Linux 6.8.0-147-generic x86_64`, `nproc` 8, `go1.26.0 linux/amd64`; shared host, other agent sessions
  possibly running. Load average (1, 5, 15 min): before warm-up (21:21:28Z) `1.09 2.87 3.40`; before timed run
  (21:25:21Z) `4.12 5.06 4.33`; after timed run (21:28:30Z) `4.12 5.12 4.55`.

| Run | Start (UTC) | End (UTC) | Wall s | Exit | Output |
|---|---|---|---|---|---|
| Warm-up | 2026-10-24T21:21:28Z | 2026-10-24T21:25:21Z | 233 | 0 | 83 `ok`, no `FAIL`, no `(cached)` |
| Timed | 2026-10-24T21:25:21Z | 2026-10-24T21:28:30Z | 189 | 0 | 83 `ok`, no `FAIL`, no `(cached)` |

**N4 comparison (+25%).** The baseline has two distinct numbers, not interchangeable: the green warm-up of
444 s and the timed run of 523 s, which was red (exit 2, two `internal/agent` deadline tests failed under load
about 30) and is therefore not a clean reference. Against the green 444 s the bound is 555 s; against the red
523 s it would be 653.75 s. Both timed (189 s: −57.4% vs 444 s, −63.9% vs 523 s) and warm-up (233 s: −47.5% vs
444 s, −55.5% vs 523 s) results are inside either bound. Caveats: load here (about 1–5) was well below the
baseline's (about 9–30 on 8 cores) and the test set is larger now (83 packages with `ok` vs 66 at the start), so
the comparison is indicative, not like-for-like, and the margin partly reflects the quieter host. Disposition:
N4 holds; no action.

**N3 benchmarks.** Single run per package at the lower load above; baseline numbers from `baseline.md` (load
about 30). Deltas are ns/op; B/op and allocs/op are shown where they differ by more than 0.5%.

| Benchmark | ns/op base → now | B/op, allocs/op |
|---|---|---|
| db `BlackboardWrite` | 12033415 → 1936154 (−84%) | same |
| db `BlackboardModify` | 22577288 → 6072958 (−73%) | B/op +0.6%, allocs +0.7% |
| db `BlackboardReadCached` | 44938 → 16804 (−63%) | same |
| db `BlackboardReadUncached` | 1890776 → 185509 (−90%) | same |
| db `MarshalStateForWriteLarge/repaired=false` | 536001675 → 116606126 (−78%) | same |
| db `MarshalStateForWriteLarge/repaired=true` | 1331986515 → 126106716 (−91%) | same |
| db `ReadCachedHit` | 4916749 → 1033445 (−79%) | same |
| db `StateParse` | 112041835 → 26041320 (−77%) | same |
| db `TerminalArchiveTransaction/inline/cold-instance` | 772451364 → 237370730 (−69%) | same |
| db `TerminalArchiveTransaction/inline/warm` | 628528774 → 250143648 (−60%) | same |
| db `TerminalArchiveTransaction/archived/cold-instance` | 320733652 → 144836279 (−55%) | same |
| db `TerminalArchiveTransaction/archived/warm` | 185103984 → 25291002 (−86%) | B/op −9.9%, allocs −18.9% |
| db `TerminalArchiveTransaction/archived-256/cold-instance` | 990744276 → 545866586 (−45%) | same |
| db `TerminalArchiveTransaction/archived-256/warm` | 319924638 → 88145860 (−72%) | B/op −2.9%, allocs −5.9% |
| commands `InspectionSnapshotDuringHeartbeat/status` | 9380378 → 7004601 (−25%) | B/op +10.5%, allocs +5.6%; heartbeat-writes 6 → 27 |
| commands `InspectionSnapshotDuringHeartbeat/get` | 4376937 → 2689265 (−39%) | B/op +5.2%, allocs +4.3%; heartbeat-writes 13 → 34 |
| agent `BenchmarkClaimReviewerTask` | 34512825 → 4392177 (−87%) | same |
| agent `BenchmarkResumeHandoff` | 5936738 → 2235682 (−62%) | same |

All three packages exited 0 with `PASS` (`internal/db` 46.183s, `internal/commands` 4.374s,
`internal/agent` 3.091s); all 18 benchmark rows of the baseline were captured, none "not captured".

**Dispositions** (N3 is Should; none blocks):

- Every ns/op row is at or below its baseline; there is no slowdown to dispose of. The improvements are
  recorded as measured and not claimed as caused by A1: the baseline ran at load about 30 and this run at about
  1–5, so most of the gain is host load.
- `InspectionSnapshotDuringHeartbeat/status` B/op +10.5% and allocs +5.6%, and `/get` B/op +5.2% and allocs
  +4.3%: out-of-bound as per-iteration allocation, but not attributable to A1 from this evidence. The benchmark
  overlaps a concurrent heartbeat writer, and heartbeat-writes per measured window rose (6 → 27, 13 → 34), so
  work per iteration is not like-for-like with the baseline; the benchmark lives in `commands` (A7), where A1
  touched only three caller lines (`init.go`, `inspect_anomalies.go`, `repair_agent_pool.go`; 5 lines changed in
  total). Recorded as an open indicative observation: re-measure at the A7 boundary (G5), where a persistent
  increase would be commissioned.
- `BlackboardModify` B/op +0.6% and allocs +0.7%: within the run-to-run variation of a concurrent-write
  benchmark; no action.

## G6 — architectural issues resolved by A1

Reviewed the Open Issues Summary (68 open, `specs/architecture/architectural-issues.md`, unchanged between
`92d39398e` and `265a14b3a`: `git diff --stat` empty) and the candidate sections against the A1 commits listed
above. No item is wholly resolved by A1, so `architectural-issues.md` is unchanged (no registry edit, no
Completed Fixes / Fixed (Traceability) entry).

- `issue-vocab` ("Control-Flow Vocabulary Bypasses Domain Ownership"): stays open. A1 typed its own
  vocabularies, but raw `"doer"`/`"reviewer"`/`"orchestrator"` comparisons remain in other areas'
  production code (`grep -cE '"(doer|reviewer|orchestrator)"'` matches at `265a14b3a`:
  `internal/commands/watch.go` 8, `internal/agent/registration.go` 7, `supervisor_block.go` 6, `prompt.go` 5,
  `internal/ops/resolve_orchestrator.go` 4). It closes when every area has adopted its vocabulary (Scope 25),
  as the plan said.
- `issue-status` ("Status Vocabulary Partition Between Go Constants and Declarative Pipeline"): stays open.
  `internal/models/task_status.go:13,15,19` still binds `TaskStatusReady = "DRAFT_CODE"`,
  `TaskStatusReadyForReview = "CODE_TO_REVIEW"`, `TaskStatusApproved = "CODE_APPROVED"`, and
  `internal/pipeline/resolver_status.go:39,48,57` still converts with an unvalidated
  `models.TaskStatus(s.Initial)` (and `s.Executing`, `s.Submitted`); the partition is untouched, as the A1
  plan states.
- "State Validation Composition Gap" (the one A1-package item that could close): stays open. It asks for a
  focused composition test for `ValidateStateFile`/`ValidateAnomalies`;
  `git grep -nE 'func Test\w*(ValidateStateFile|ValidateAnomalies)' 265a14b3a -- internal/statevalidate`
  matches nothing (and nothing at the starting commit); A1's statevalidate change was a mechanical split.
- "Sprint Metrics Lossy at Sprint Boundary" and "Blackboard Growth Without Pruning" (models/db related,
  already "mitigated, open"): A1's changes were structural splits and typing with byte-preserving behavior
  (C1/C3 green, frozen oracles unchanged), so no behavior fix closes them.

## G7 — record completeness

This file holds the commands or source reads, the integration SHA (`265a14b3ae563d01175eb68d8a98c3501d905f71`,
for G1–G6 alike) and excerpts for G1–G6 above, and the pending G8 below. Manifest:
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-11-cp-0-boundary.json` (G4 mapped to
the `Makefile` test target with `command_index` 1; every other obligation maps to its approved reference).

## G8 — dispatched CI result (D52): Linux, Windows and macOS

Status: **pending**. Under D52 (replacing the per-boundary pull request; `1e3b803b7`, with D53 `6ef124d3e`), the
operator dispatches `.github/workflows/ci.yml` (`workflow_dispatch`) on the pushed `global-refactoring` branch at
the integration revision `265a14b3ae563d01175eb68d8a98c3501d905f71`, and the maintainer confirms that the
Linux, Windows and macOS jobs of that one run are all green. No pull request to `main` is opened or claimed.
Not evidence for this revision: the signal run `https://github.com/liza-mas/liza/actions/runs/40105658735` (at
`1e3b803b7`, red), the run `https://github.com/liza-mas/liza/actions/runs/40115874515` (at `79e22369a`, red:
Windows rest-1, Linux ops `TestSubmitProgressRepeatsWhileQueuedAndRunning`), and the verification run
`https://github.com/liza-mas/liza/actions/runs/40132329230` (at `265a14b3a`, in progress when this record was
written, not dispatched as G8 evidence). To be appended on resume: run URL, tested full SHA (must equal the
revision above), each job's name and conclusion, UTC observation. Red CI keeps the next area held.
