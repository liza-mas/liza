# Architecture Consolidation Run: Area Boundary Record A1 (State core)

Evidence of the A1 area-boundary checkpoint (`Area-Boundary Checkpoint Contract`, checks G1–G8) from the
arm-1-ar-11 plan, Scope 1, task `arm-1-ar-11-cp-0-code-0`. One record per boundary; later units never edit
it. It changes no production, test, guard-data, oracle or baseline file.

## Observed revision

- Configured integration branch: `omni-ee -C <project root> get config --json`, field
  `IntegrationBranch` = `"global-refactoring"` (observed 2026-10-24T14:52Z); equals the D23 ruling cited by
  `baseline.md`.
- Integration revision under test (`git rev-parse global-refactoring`, also the task worktree `HEAD`; the
  worktree was clean and `git log HEAD..global-refactoring` printed nothing):
  `92d39398e89bfb7616882d35848119057c3fba0a`.
- Every command below ran on that code revision; this record is added on top of it. Run start
  2026-10-24T14:52Z. Starting commit of the run (for A1 diffs):
  `96c9ad69aee983ce2b929dd681b6ba43bf920535`.
- A1 coding units, merged before this unit (`git log --no-merges <start>..HEAD` over the 24 A1 package
  paths): `25b9cf48a` models, `800bef47a` pipeline, `be8a4289a` statevalidate, `cf36edcc8`
  referencecontract, `969d82ad4` db, `eebdbff8a` V1–V5/V11–V12, `91ad8a41e` V6–V10, `367a9b5ca` and
  `2143802bd` named-value adoption (plus the ADR task).
- Environment for every measurement: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` (GUARDRAILS G1.4). No
  remote push, provider CLI, installation or update ran; output below is sanitized (no environment values).

## G1 — A1 ratchet empty; S2 exceptions data-like with rationale

- Source: `internal/testguard/archguard/area_a1_test.go` at `92d39398e` (read in full, 36 lines).
  Excerpt (lines 33–35):

  ```go
  Allowlist: []AllowEntry{},
  Ratchet:   []RatchetEntry{},
  S2:        []S2Entry{},
  ```

- A1 Ratchet: none (paired with the guard run below, since the guard alone permits ratchet debt). A1 S2:
  none, so there is no S2 entry needing a category or rationale. The start snapshot's A1 count was 3 files /
  7 functions (`baseline.md`); all were removed rather than converted to S2.
- Independent size check: largest non-test file in the 24 A1 packages is `internal/db/blackboard.go`
  (786 lines < 800); `internal/usage/report.go` 765, `internal/analysis/patterns.go` 690.
- C0: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 ./internal/testguard/archguard/`,
  2026-10-24T14:53:24Z, exit 0: `ok  github.com/liza-mas/liza/internal/testguard/archguard  0.113s`. A
  `-v` rerun shows `--- PASS: TestS1SizeGuard` and `--- PASS: TestK4ImportDirectionGuard` (plus the
  guard's own unit tests, 41 `--- PASS` lines, no failure).
- Result: pass.

## G2 — A1 allowlist empty; no SI-5 crossing owned by A1

- Source: same file, `Allowlist: []AllowEntry{}`: empty, so no entry lacks a rationale. Layer entries cover
  all 24 A1 packages (domain: models, pipeline, referencecontract, roles, taskkind; leaf: brand, errors,
  identity, log, paths, render; infrastructure: the other 13).
- SI-5 (`20261023-162147-arm-1.md#SI-5 K3 Crossing Resolutions`, pinned `74212192d`) assigns its four
  crossings to Scope 19 (A8), Scope 17 (A7), Scope 20 (A9) and Scope 21 (C6, in A9); `arch-crossings` names
  none for A1. Result: no A1-owned crossing.
- The C0 `TestK4ImportDirectionGuard` pass above also proves no unlisted forbidden edge and no stale
  allowlist entry exists. Result: pass.

## G3 — A1 Shim Register empty; no alias or forwarder added

- Register: `20261023-192211-arm-1-ar-10.md#Shim Register` (pinned `2226d2668`) reads "None."
- Inspection, `git diff -U0 96c9ad69a HEAD -- <24 A1 package dirs> ':!*_test.go'` (117 files changed
  including tests, 7895 insertions, 5100 deletions):
  - added `type X = Y` alias declarations: 0 (`grep -nE '^\+\s*type\s+\w+(\[[^]]*\])?\s+=\s'`, no match).
  - added lines containing deprecated / shim / forwarder / compat alias / temporary markers: 0.
  - added one-line `return f(...)` functions (candidate forwarders): 21 found by script. 20 exist at the
    starting commit with the same one-line body (moved by a file split, or a string literal replaced by a
    named constant, e.g. `roleNamesByType("doer")` → `roles.RoleTypeDoer`): `IsSprintTerminal`,
    `CanPipelineTransition`, `IsPipelineSprintTerminal`, `HasRole`, `ValidateValidationCommands`,
    `ValidateValidationSafety`, `Doer/ReviewerRoleNames`, `IsDeclaredState`,
    `Available{Manual,Auto}Transitions`, `OutputConsumerRolePairs[ForOutput]`, the six `statusClassifier`
    `Is*` methods, `collapseRejectionRCAText`. The 21st, `hasInlineTag` (`internal/db/yaml_write.go`, added
    by `969d82ad4`), is a private predicate with two callers in the same file; it keeps no old name and
    forwards to nothing that moved. None preserves a relocated API, so none is a temporary shim.
- Result: pass; no alias or forwarder was added by A1 coding.

## G4 — suite green at the integration revision

All at `92d39398e` in the task worktree (clean before and after; no tracked file changed), under the `env -i`
form. Wall times are shared-host, not baseline-comparable (see G5 for that).

| Cmd | Command | Start (UTC) | End (UTC) | Exit | Excerpt |
|---|---|---|---|---|---|
| C0 | `go test -count=1 ./internal/testguard/archguard/` | 14:53:24 | 14:53:24 | 0 | `ok .../internal/testguard/archguard 0.113s` |
| C1 | `make test` | 14:53:33 | 14:57:57 | 0 | 83 `ok` lines, 0 `FAIL`, 0 `(cached)`; last: `ok .../scripts/ci/testshard 0.468s` |
| C2 | `make lint` | 14:57:57 | 14:58:47 | 0 | `GOOS=windows go vet ./...`, `GOOS=darwin go vet ./...`, `ok .../internal/testguard/archguard 0.177s` |
| C3 | `make test-e2e` | 14:58:47 | 15:03:40 | 0 | `ok .../internal/integration 279.250s` (`TestFullSprintSequence`, `TestTerminalDependencyRecovery`, `-race -tags e2e`) |
| C4 | `go test -race <24 A1 packages>` | 15:03:40 | 15:05:29 | 0 | 23 `ok`, `internal/taskkind [no test files]` |

C4 packages, all `ok`: models 11.834s, roles 1.187s, pipeline 10.546s, statevalidate 55.545s, statehygiene
1.259s, payloadschema 1.987s, referencecontract 1.712s, runtimeinputs 1.291s, db 65.097s, filelock 2.571s,
archiveobject 1.256s, atomicfile 1.082s, identity 1.104s, errors 1.070s, paths 1.517s, render 1.066s, log
1.527s, alerts 3.646s, usage 3.474s, analysis 1.324s, envgate 1.074s, brand 1.190s, rolemodels 1.194s;
`internal/taskkind` has no test files (as at the start). Result: pass.

## G5 — wall time and benchmarks against `baseline.md` (indicative; never blocks)

Protocol repeated from `baseline.md`: fresh disposable clone outside the worktrees
(`git clone --local --no-checkout <project root>` then `git checkout --detach 92d39398e…`; clone `HEAD`
printed `92d39398e89bfb7616882d35848119057c3fba0a`; clone `git status --short` empty afterwards; clone
deleted after the session), `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp GOFLAGS=-count=1`, one warm-up
`make test`, one timed `make test`, then `timeout 300 go test -run '^$' -bench . -benchmem <pkg>`.

- Host: `Linux 6.8.0-147-generic x86_64`, `nproc` 8, `go1.26.0 linux/amd64`; shared host, other agent sessions
  running. Load average (1, 5, 15 min): before warm-up (15:06:02Z) `33.91 34.06 21.57`; before timed run
  (15:12:40Z) `20.91 25.70 22.52`; after timed run (15:17:54Z) `30.43 27.39 24.10`.

| Run | Start (UTC) | End (UTC) | Wall s | Exit | Output |
|---|---|---|---|---|---|
| Warm-up | 2026-10-24T15:06:03Z | 2026-10-24T15:12:40Z | 397 | 0 | 83 `ok`, no `FAIL`, no `(cached)` |
| Timed | 2026-10-24T15:12:40Z | 2026-10-24T15:17:54Z | 314 | 0 | 83 `ok`, no `FAIL`, no `(cached)` |

**N4 comparison (+25%).** The baseline has two distinct numbers, not interchangeable: the green warm-up of
444 s and the timed run of 523 s, which was red (exit 2, two `internal/agent` deadline tests failed under load
about 30) and is therefore not a clean reference. Against the green 444 s the bound is 555 s; against the red
523 s it would be 653.75 s. Both timed (314 s: −29.3% vs 444 s, −40.0% vs 523 s) and warm-up (397 s: −10.6%
vs 444 s) results are inside either bound. Caveats: load was comparable to the baseline's (about 20–34 on 8
cores) but the test set is larger now (83 packages with `ok` vs 66 at the start), so the comparison is
indicative, not like-for-like. Disposition: N4 holds; no action.

**N3 benchmarks.** Single run per package, load as above; baseline numbers from `baseline.md` (load about 30).
Deltas are ns/op; B/op and allocs/op are shown where they differ by more than 0.5%.

| Benchmark | ns/op base → now | B/op, allocs/op |
|---|---|---|
| db `BlackboardWrite` | 12033415 → 5735192 (−52%) | same |
| db `BlackboardModify` | 22577288 → 7730668 (−66%) | same (+0.3%) |
| db `BlackboardReadCached` | 44938 → 53560 (+19%) | same |
| db `BlackboardReadUncached` | 1890776 → 1202305 (−36%) | same |
| db `MarshalStateForWriteLarge/repaired=false` | 536001675 → 533921465 (−0%) | same |
| db `MarshalStateForWriteLarge/repaired=true` | 1331986515 → 563071474 (−58%) | same |
| db `ReadCachedHit` | 4916749 → 5239869 (+7%) | same |
| db `StateParse` | 112041835 → 144941892 (+29%) | B/op +26 bytes, allocs identical (56356) |
| db `TerminalArchiveTransaction/inline/cold-instance` | 772451364 → 1319584172 (+71%) | same |
| db `TerminalArchiveTransaction/inline/warm` | 628528774 → 842485180 (+34%) | same |
| db `TerminalArchiveTransaction/archived/cold-instance` | 320733652 → 268073013 (−16%) | same |
| db `TerminalArchiveTransaction/archived/warm` | 185103984 → 39953480 (−78%) | B/op −9.0%, allocs −17.1% |
| db `TerminalArchiveTransaction/archived-256/cold-instance` | 990744276 → 1010659662 (+2%) | same |
| db `TerminalArchiveTransaction/archived-256/warm` | 319924638 → 213664919 (−33%) | B/op +1.3%, allocs +2.5% |
| commands `InspectionSnapshotDuringHeartbeat/status` | 9380378 → 17011393 (+81%) | B/op +12.9%, allocs +6.9%; heartbeat-writes 6 → 15 |
| commands `InspectionSnapshotDuringHeartbeat/get` | 4376937 → 4201576 (−4%) | B/op +5.9%, allocs +5.2%; heartbeat-writes 13 → 22 |
| agent `BenchmarkClaimReviewerTask` | 34512825 → 7742282 (−78%) | same |
| agent `BenchmarkResumeHandoff` | 5936738 → 3994478 (−33%) | same |

All three packages exited 0 with `PASS` (`internal/db` 69.937s, `internal/commands` 4.390s,
`internal/agent` 3.768s); all 18 benchmark rows of the baseline were captured, none "not captured".

**Dispositions** (N3 is Should; none blocks):

- Slower rows `BlackboardReadCached` +19%, `ReadCachedHit` +7%, `StateParse` +29%,
  `TerminalArchiveTransaction/inline/*` +34%/+71%: accepted as measurement noise, not an A1 regression. B/op
  and allocs/op are deterministic and within ±0.01% of the baseline for these rows (the same work is
  done per iteration), the inline rows ran 1–2 iterations at load 20–34 on 8 cores, and other rows
  of the same package improved 33–78% in the same run. A quiet-host repeat at run close (Scope 25) is the
  re-check trigger if a slowdown persists.
- `InspectionSnapshotDuringHeartbeat/status` +81% ns/op, B/op +12.9%, allocs +6.9%, and `/get` allocs +5.2%:
  not attributable to A1 from this evidence. The benchmark overlaps a concurrent heartbeat writer, and the
  heartbeat-writes per measured window rose (6 → 15, 13 → 22), so work per iteration is not like-for-like
  with the baseline; the benchmark lives in `commands` (A7), where A1 touched only three caller lines
  (`init.go`, `inspect_anomalies.go`, `repair_agent_pool.go`; 5 lines changed in total). Recorded as an
  open indicative observation: re-measure at the A7 boundary (G5), where a persistent slowdown would be
  commissioned.
- Improvements are recorded as measured and not claimed as caused by A1.

## G6 — architectural issues resolved by A1

Reviewed the Open Issues Summary (68 open, `specs/architecture/architectural-issues.md`) and the candidate
sections against the A1 commits listed above. No item is wholly resolved by A1, so
`architectural-issues.md` is unchanged (no registry edit, no Completed Fixes / Fixed (Traceability) entry).

- `issue-vocab` ("Control-Flow Vocabulary Bypasses Domain Ownership"): stays open. A1 typed its own
  vocabularies, but raw `"doer"`/`"reviewer"`/`"orchestrator"` comparisons remain in other areas'
  production code (`rg -c` matches: `internal/commands/watch.go` 8, `internal/agent/registration.go` 7,
  `supervisor_block.go` 6, `prompt.go` 5, `internal/ops/resolve_orchestrator.go` 4).
  It closes when every area has adopted its vocabulary (Scope 25), as the plan said.
- `issue-status` ("Status Vocabulary Partition Between Go Constants and Declarative Pipeline"): stays open.
  `internal/models/task_status.go:13,15,19` still binds `TaskStatusReady = "DRAFT_CODE"`,
  `TaskStatusReadyForReview = "CODE_TO_REVIEW"`, `TaskStatusApproved = "CODE_APPROVED"`, and
  `internal/pipeline/resolver_status.go:39,48,57` still converts with an unvalidated
  `models.TaskStatus(s.Initial)`; the partition is untouched, as the A1 plan states.
- "State Validation Composition Gap" (the one A1-package item that could close): stays open. It asks for a
  focused composition test for `ValidateStateFile`/`ValidateAnomalies`; `rg 'func Test\w*(ValidateStateFile|ValidateAnomalies)' internal/statevalidate`
  matches nothing at `92d39398e` (and nothing at the starting commit); A1's statevalidate change was a
  mechanical split.
- "Sprint Metrics Lossy at Sprint Boundary" and "Blackboard Growth Without Pruning" (models/db related,
  already "mitigated, open"): A1's changes were structural splits and typing with byte-preserving behavior
  (C1/C3 green, frozen oracles unchanged), so no behavior fix closes them.

## G7 — record completeness

This file holds the commands or source reads, the integration SHA (`92d39398e89bfb7616882d35848119057c3fba0a`)
and excerpts for G1–G6 above, and the pending G8 below. Manifest:
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-11-cp-0-boundary.json` (G4 mapped to
the `Makefile` test target with `command_index` 1; every other obligation maps to its approved reference).

## G8 — maintainer Linux/Windows CI result

Status: **pending**. The maintainer is asked to open the `global-refactoring`→`main` pull request and record
the Linux and Windows CI results for the tested integration revision `92d39398e89bfb7616882d35848119057c3fba0a`.
macOS is the subsequent `main`-push check (goal-proof); it has not run and is not claimed here. To be appended
on resume: PR URL/number, tested full SHA, check names, URLs and results, UTC observation.
