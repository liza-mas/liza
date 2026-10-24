# Architecture Consolidation Run: Area Boundary Record A1 (State core)

Evidence of the A1 area-boundary checkpoint (`Area-Boundary Checkpoint Contract`, checks G1–G8) from the
arm-1-ar-11 plan, Scope 1, task `arm-1-ar-11-cp-0-code-0`. One record per boundary; later units never edit
it. It changes no production, test, guard-data, oracle or baseline file.

## Observed revision

- Configured integration branch: `omni-ee -C <project root> get config --json`, field
  `IntegrationBranch` = `"global-refactoring"` (observed 2026-10-24T17:38Z); equals the D23 ruling cited by
  `baseline.md`.
- Integration revision under test (`git rev-parse global-refactoring` at 2026-10-24T17:41Z, after the task
  branch was rebased onto it; `git log HEAD..global-refactoring` printed nothing):
  `2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16`. The task worktree `HEAD` is this revision plus the one commit that
  adds this record and the acceptance manifest; no other file differs.
- Refresh history (the earlier evidence is superseded, not relabeled). A first evidence set was taken at
  `92d39398e89bfb7616882d35848119057c3fba0a`; the D53 oracle-portability and D52 timing-follow-up repairs then
  merged (36 commits, up to `79e22369a7beb3aa1a552819a7b5f8c0686a04e7`), and this record was regenerated:
  C0–C4 and G5 ran at `79e22369a`, and `global-refactoring` then advanced to `2f8b7a7b0` by one commit,
  `docs(goals): record D56 …` (`git diff --stat 79e22369a 2f8b7a7b0`: one file,
  `specs/goals/20261023-architecture-consolidation-decisions.md`, 24 insertions, no Go, test, data or workflow
  file). C0–C4 were therefore rerun at `2f8b7a7b0`; G5 stays at `79e22369a`, whose code is byte-identical
  (the only delta is that decisions document).
- A1 code did not change across the refresh: `git diff --stat 92d39398e 79e22369a` over the 24 A1 package
  directories lists only two added files under `internal/testguard/` (`ci_checkout_history_test.go`,
  `tracked_paths_test.go`, D53 guards); nothing under the A1 packages.
- Run start 2026-10-24T17:21Z (first refreshed run); starting commit of the run (for A1 diffs):
  `96c9ad69aee983ce2b929dd681b6ba43bf920535`.
- A1 coding units, merged before this unit (`git log --no-merges <start>..HEAD` over the 24 A1 package
  paths): `25b9cf48a` models, `800bef47a` pipeline, `be8a4289a` statevalidate, `cf36edcc8`
  referencecontract, `969d82ad4` db, `eebdbff8a` V1–V5/V11–V12, `91ad8a41e` V6–V10, `367a9b5ca` and
  `2143802bd` named-value adoption (plus the ADR task).
- Environment for every measurement: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` (GUARDRAILS G1.4). No
  remote push, provider CLI, installation or update ran; output below is sanitized (no environment values).

## G1 — A1 ratchet empty; S2 exceptions data-like with rationale

- Source: `internal/testguard/archguard/area_a1_test.go` at `2f8b7a7b0` (read in full, 36 lines).
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
  2026-10-24T17:38:37Z, exit 0: `ok  github.com/liza-mas/liza/internal/testguard/archguard  0.288s` (also
  exit 0 at `79e22369a`, 17:21:44Z, 0.177s).
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
- Inspection, `git diff -U0 96c9ad69a 79e22369a -- <24 A1 package dirs> ':!*_test.go'` (117 files changed
  including tests, 7895 insertions, 5100 deletions; identical to the figures at `92d39398e`, as A1 code did not
  change in the refresh; the diff is the same at `2f8b7a7b0`, whose delta is one document):
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

Final set at `2f8b7a7b0` in the task worktree (clean before; no tracked file changed by the runs), under the
`env -i` form. Wall times are shared-host, not baseline-comparable (see G5 for that). Because C1 and C4 of the
first pass at `2f8b7a7b0` were served wholly from the Go test cache (`83` and `23` `(cached)` lines; inputs
unchanged since `79e22369a`), they were rerun uncached with `GOFLAGS=-count=1` added to the same `env -i`
command (translation of the canonical form; rows C1u/C4u). Results at `79e22369a` (all uncached, canonical
form) are listed for the refresh trail.

| Cmd | Command | Start (UTC) | End (UTC) | Exit | Excerpt |
|---|---|---|---|---|---|
| C0 | `go test -count=1 ./internal/testguard/archguard/` | 17:38:37 | 17:38:38 | 0 | `ok .../internal/testguard/archguard 0.288s` |
| C1u | `make test` (+`GOFLAGS=-count=1`) | 17:41:06 | 17:44:43 | 0 | 83 `ok` lines, 0 `FAIL`, 0 `(cached)`; last: `ok .../scripts/ci/testshard 0.720s` |
| C2 | `make lint` | 17:39:20 | 17:39:23 | 0 | `go vet ./...` for `GOOS=linux`, `windows`, `darwin`; `ok .../internal/testguard/archguard 0.172s` |
| C3 | `make test-e2e` | 17:39:23 | 17:40:44 | 0 | `ok .../internal/integration 80.397s` (`TestFullSprintSequence`, `TestTerminalDependencyRecovery`, `-race -tags e2e -count=1`) |
| C4u | `go test -race <24 A1 packages>` (+`GOFLAGS=-count=1`) | 17:44:43 | 17:45:03 | 0 | 23 `ok`, 0 `(cached)`, `internal/taskkind [no test files]` |

First pass at `2f8b7a7b0` for C1/C4 (17:38:38–17:39:20 and 17:40:44, exit 0, fully `(cached)`) is not counted
as evidence. At `79e22369a`: C0 17:21:44 exit 0; C1 17:21:45–17:26:01 exit 0 (83 `ok`, 0 `(cached)`); C2
17:26:01–17:26:58 exit 0; C3 17:26:58–17:28:43 exit 0 (`ok .../internal/integration 87.973s`); C4
17:28:43–17:29:08 exit 0 (23 `ok`).

C4u packages at `2f8b7a7b0`, all `ok`: models 3.445s, roles
1.031s, pipeline 3.264s, statevalidate 12.930s, statehygiene 1.063s, payloadschema 1.207s, referencecontract
1.129s, runtimeinputs 1.058s, db 18.011s, filelock 2.388s, archiveobject 1.057s, atomicfile 1.038s, identity
1.041s, errors 1.015s, paths 1.056s, render 1.020s, log 1.346s, alerts 3.538s, usage 2.638s, analysis 1.041s,
envgate 1.018s, brand 1.036s, rolemodels 1.024s; `internal/taskkind` has no test files (as at the start).
Result: pass.

## G5 — wall time and benchmarks against `baseline.md` (indicative; never blocks)

Protocol repeated from `baseline.md`: fresh disposable clone outside the worktrees
(`git clone --local --no-checkout <project root>` then `git checkout --detach 79e22369a…`; clone `HEAD`
printed `79e22369a7beb3aa1a552819a7b5f8c0686a04e7`; clone `git status --short` empty afterwards; clone
deleted after the session, `ls -d /tmp/a1g5base.*` matched nothing), `env -i HOME="$HOME" PATH="$PATH"
TMPDIR=/tmp GOFLAGS=-count=1`, one warm-up `make test`, one timed `make test`, then
`timeout 300 go test -run '^$' -bench . -benchmem ./internal/<pkg>`. Measured at `79e22369a`, whose code equals
the integration revision `2f8b7a7b0` (one documentation file apart); not remeasured at the latter.

- Host: `Linux 6.8.0-147-generic x86_64`, `nproc` 8, `go1.26.0 linux/amd64`; shared host, other agent sessions
  possibly running. Load average (1, 5, 15 min): before warm-up (17:29:27Z) `3.79 5.97 8.04`; before timed run
  (17:33:23Z) `6.14 6.79 8.03`; after timed run (17:36:56Z) `6.55 9.22 8.98`.

| Run | Start (UTC) | End (UTC) | Wall s | Exit | Output |
|---|---|---|---|---|---|
| Warm-up | 2026-10-24T17:29:27Z | 2026-10-24T17:33:23Z | 236 | 0 | 83 `ok`, no `FAIL`, no `(cached)` |
| Timed | 2026-10-24T17:33:23Z | 2026-10-24T17:36:56Z | 213 | 0 | 83 `ok`, no `FAIL`, no `(cached)` |

**N4 comparison (+25%).** The baseline has two distinct numbers, not interchangeable: the green warm-up of
444 s and the timed run of 523 s, which was red (exit 2, two `internal/agent` deadline tests failed under load
about 30) and is therefore not a clean reference. Against the green 444 s the bound is 555 s; against the red
523 s it would be 653.75 s. Both timed (213 s: −52.0% vs 444 s, −59.3% vs 523 s) and warm-up (236 s: −46.8% vs
444 s, −54.9% vs 523 s) results are inside either bound. Caveats: load here (about 4–9) was well below the
baseline's (about 9–30 on 8 cores) and the test set is larger now (83 packages with `ok` vs 66 at the start), so
the comparison is indicative, not like-for-like, and the margin partly reflects the quieter host. Disposition:
N4 holds; no action.

**N3 benchmarks.** Single run per package at the lower load above; baseline numbers from `baseline.md` (load
about 30). Deltas are ns/op; B/op and allocs/op are shown where they differ by more than 0.5%.

| Benchmark | ns/op base → now | B/op, allocs/op |
|---|---|---|
| db `BlackboardWrite` | 12033415 → 1881376 (−84%) | same |
| db `BlackboardModify` | 22577288 → 2042489 (−91%) | B/op +0.6%, allocs +0.6% |
| db `BlackboardReadCached` | 44938 → 18450 (−59%) | same |
| db `BlackboardReadUncached` | 1890776 → 205684 (−89%) | same |
| db `MarshalStateForWriteLarge/repaired=false` | 536001675 → 132728454 (−75%) | same |
| db `MarshalStateForWriteLarge/repaired=true` | 1331986515 → 149194723 (−89%) | same |
| db `ReadCachedHit` | 4916749 → 1097955 (−78%) | same |
| db `StateParse` | 112041835 → 32184304 (−71%) | same |
| db `TerminalArchiveTransaction/inline/cold-instance` | 772451364 → 261004706 (−66%) | same |
| db `TerminalArchiveTransaction/inline/warm` | 628528774 → 277715635 (−56%) | same |
| db `TerminalArchiveTransaction/archived/cold-instance` | 320733652 → 166486121 (−48%) | same |
| db `TerminalArchiveTransaction/archived/warm` | 185103984 → 28394675 (−85%) | B/op −9.9%, allocs −18.8% |
| db `TerminalArchiveTransaction/archived-256/cold-instance` | 990744276 → 650534574 (−34%) | same |
| db `TerminalArchiveTransaction/archived-256/warm` | 319924638 → 118949919 (−63%) | B/op −3.0%, allocs −5.9% |
| commands `InspectionSnapshotDuringHeartbeat/status` | 9380378 → 7808910 (−17%) | B/op +11.5%, allocs +6.1%; heartbeat-writes 6 → 32 |
| commands `InspectionSnapshotDuringHeartbeat/get` | 4376937 → 3136079 (−28%) | B/op +6.0%, allocs +5.1%; heartbeat-writes 13 → 34 |
| agent `BenchmarkClaimReviewerTask` | 34512825 → 10021547 (−71%) | same |
| agent `BenchmarkResumeHandoff` | 5936738 → 3820155 (−36%) | same |

All three packages exited 0 with `PASS` (`internal/db` 51.486s, `internal/commands` 3.606s,
`internal/agent` 3.908s); all 18 benchmark rows of the baseline were captured, none "not captured".

**Dispositions** (N3 is Should; none blocks):

- Every ns/op row is at or below its baseline; there is no slowdown to dispose of. The improvements are
  recorded as measured and not claimed as caused by A1: the baseline ran at load about 30 and this run at about
  4–9, so most of the gain is host load.
- `InspectionSnapshotDuringHeartbeat/status` B/op +11.5% and allocs +6.1%, and `/get` B/op +6.0% and allocs
  +5.1%: out-of-bound as per-iteration allocation, but not attributable to A1 from this evidence. The benchmark
  overlaps a concurrent heartbeat writer, and heartbeat-writes per measured window rose (6 → 32, 13 → 34), so
  work per iteration is not like-for-like with the baseline; the benchmark lives in `commands` (A7), where A1
  touched only three caller lines (`init.go`, `inspect_anomalies.go`, `repair_agent_pool.go`; 5 lines changed in
  total). Recorded as an open indicative observation: re-measure at the A7 boundary (G5), where a persistent
  increase would be commissioned.
- `BlackboardModify` B/op +0.6% and allocs +0.6%: within the run-to-run variation of a concurrent-write
  benchmark; no action.

## G6 — architectural issues resolved by A1

Reviewed the Open Issues Summary (68 open, `specs/architecture/architectural-issues.md`, unchanged between
`92d39398e` and `79e22369a`: `git diff --stat` empty) and the candidate sections against the A1 commits listed
above. No item is wholly resolved by A1, so `architectural-issues.md` is unchanged (no registry edit, no
Completed Fixes / Fixed (Traceability) entry).

- `issue-vocab` ("Control-Flow Vocabulary Bypasses Domain Ownership"): stays open. A1 typed its own
  vocabularies, but raw `"doer"`/`"reviewer"`/`"orchestrator"` comparisons remain in other areas'
  production code (`grep -cE '"(doer|reviewer|orchestrator)"'` matches at `2f8b7a7b0`:
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
  `grep -rnE 'func Test\w*(ValidateStateFile|ValidateAnomalies)' internal/statevalidate` matches nothing at
  `2f8b7a7b0` (and nothing at the starting commit); A1's statevalidate change was a mechanical split.
- "Sprint Metrics Lossy at Sprint Boundary" and "Blackboard Growth Without Pruning" (models/db related,
  already "mitigated, open"): A1's changes were structural splits and typing with byte-preserving behavior
  (C1/C3 green, frozen oracles unchanged), so no behavior fix closes them.

## G7 — record completeness

This file holds the commands or source reads, the integration SHA (`2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16`;
G5 at the code-identical `79e22369a7beb3aa1a552819a7b5f8c0686a04e7`) and excerpts for G1–G6 above, and the
pending G8 below. Manifest:
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-11-cp-0-boundary.json` (G4 mapped to
the `Makefile` test target with `command_index` 1; every other obligation maps to its approved reference).

## G8 — dispatched CI result (D52): Linux, Windows and macOS

Status: **pending**. Under D52 (replacing the per-boundary pull request; `1e3b803b7`, with D53 `6ef124d3e`), the
operator dispatches `.github/workflows/ci.yml` (`workflow_dispatch`) on the pushed `global-refactoring` branch at
the integration revision `2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16`, and the maintainer confirms that the
Linux, Windows and macOS jobs of that one run are all green. No pull request to `main` is opened or claimed. The
earlier signal run `https://github.com/liza-mas/liza/actions/runs/40105658735` (at `1e3b803b7`, red) is not
evidence for this revision. To be appended on resume: run URL, tested full SHA (must equal the revision above),
each job's name and conclusion, UTC observation. Red CI keeps the next area held.
