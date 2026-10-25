# Architecture Consolidation Run: Area Boundary Record A4 (Ops planning and task graph)

Evidence of the A4 area-boundary checkpoint (`Area-Boundary Checkpoint Contract`, checks G1–G8) from the
arm-1-ar-14 plan, Scope 1, task `arm-1-ar-14-cp-0-code-0`. One record per boundary; later units never edit it.
Beyond this record it changes only `architectural-issues.md` (the justified A4 closure of G6) and the acceptance
manifest. It changes no production, test, guard-data, oracle or baseline file. `tdd_not_required`: evidence-only
unit that moves no code.

## Observed revision

- Configured integration branch: `global-refactoring` (D23; `baseline.md`). `omni-ee get config --json` reports
  `IntegrationBranch` = `"global-refactoring"`. `git rev-parse global-refactoring` and `git rev-parse HEAD` of the
  task worktree both print the revision below, and `git log HEAD..global-refactoring` printed nothing at the start
  (2026-10-25T17:35Z) and again at the end of G1–G6 (2026-10-25T18:00Z, `global-refactoring` unchanged).
- Two integration revisions are recorded; neither relabels the other.
  - **R1, superseded:** `1e7807b0688691feef5369ff8d06953cfc8c174e`
    (`Merge arm-1-ar-13-cp-5-code-2 (task/arm-1-ar-13-cp-5-code-2)`). G1–G6 below were observed there. The
    dispatched G8 run at R1 was red (section G8), so R1 is not the closing revision.
  - **R2, current:** `20fee7197f2548b8e9844154a874700a603b8aee` (`fix(build): fold manifest notes into the
    AC-budget assertion`), after the macOS test-budget correction merged (`47abea4a1`, `20fee7197`). Section
    "Refresh at R2" re-runs G1–G4 and identifies the delta for G5–G6.
  The task worktree `HEAD` is the integration revision of the last observation plus the commits that add this
  record, the registry closure and the acceptance manifest; `git diff --stat 20fee7197 HEAD` lists only those
  files.
- Observation windows (UTC), task branch `task/arm-1-ar-14-cp-0-code-0`: R1 2026-10-25T17:35Z to 18:00Z (the
  worktree was clean before every command until the G6 registry edit, `git status --short` empty); R2
  2026-10-25T19:04Z to 19:17Z.
- Starting commit of the run (for A4 diffs): `96c9ad69aee983ce2b929dd681b6ba43bf920535`.
- A4 coding units merged before this unit: the task `depends_on` list (arm-1-ar-13-cp-0 through cp-5, 21 tasks) is
  all MERGED. Commits touching `internal/ops` since the A3 boundary record (`98dc9efc2`), by concern:
  transition entry, child generation, cohort, recovery `1faa264af`, `5ebceec54`; add-tasks `f75bcf0be`;
  set-task-output `59f0d42d0`; amend-plan `19d4de3d3`; replan `dc54110b7`; plan-check `84379a195`;
  obligation drift `9f3347970`; direct-edge repair `95d82d82d`; provider-wait vocabulary `27ebce221`;
  apply-dependency-repair `030abc1b7`; narrow-inherited-dependencies `e60ea3e2e`; archive vocabulary `15f52511f`;
  replace-task `1212e9ad7`; supersede-task and cancel-task `80eb1d8a5`; delete-task `b71741092`; sprint checkpoint
  `4d15b2df7`; K7 vocabulary `9600d2f0f`, `5163454ba`; K7 inventory `faaf87cca`, `202d531fc`; ADR 0223 `a1bab0dea`,
  `d22895738`; closure-readiness evidence `31f06f5df`. The matching oracle additions are in the same range
  (`git log 98dc9efc2..1e7807b06 -- internal/oracle/a4`). No Go file changed after `202d531fc`
  (`git diff --stat 202d531fc 1e7807b06 -- internal ':!*_test.go'` prints nothing; the last three commits are
  documentation).
- Environment for every measurement: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` (GUARDRAILS G1.4), plus
  `GOFLAGS=-count=1` for G5 (translation of the Protocol; C0–C4 ran in the canonical form, below). No remote push,
  provider CLI, installation or update ran; output below is sanitized (no environment values). Host: `Linux
  6.8.0-147-generic x86_64`, `nproc` 8, `go1.26.0 linux/amd64`, `git version 2.43.0`. Shared host with other agent
  sessions running.

## G1 — A4 ratchet empty; S2 exceptions data-like with rationale

Source: `internal/testguard/archguard/area_a4_test.go` read at `1e7807b06`:

| Field | Value |
|---|---|
| `Ratchet` | `[]RatchetEntry{}` (empty) |
| `S2` | `[]S2Entry{}` (empty): no S2 entry exists, so no category or human ruling is to be checked |
| `Layers` | `[]LayerEntry{}` (empty) |
| `Allowlist` | `[]AllowEntry{}` (empty) |

The start state was 23 keys (2 files, 21 functions; `baseline.md`, `a4-size`). All are gone. This is recorded
apart from the C0 result because the guard tolerates debt: `TestAreaA4RatchetIsEmpty` fails on any remaining
ratchet key and on an S2 entry lacking ruling or rationale, and passes. Independent measurement with `go/parser`
(declaration line to closing brace; physical lines) over the 60 A4 production files (the 41 of the area
partition plus the 19 concern files the A4 scopes created, `a4-closure-readiness.md`): longest file `proceed.go`
638 lines (start 2,052), next `transition_pass.go` 621; longest function `validatePlanningOutputAcceptance` 97
lines, then `Analyze` 95 and `RepairArchRef` 93 (limits 800 and 100). `proceed.go` functions: `proceedManyToOneInner`
53, `proceedInner` 36, `Proceed` 30, `ExecuteTransitionsReportWith` 28, `ExecuteAvailableTransitions` 10. G1 holds: nothing
remains as an S1 offender, so no S2 conversion was needed.

## G2 — A4 allowlist holds only rationale-carrying entries; no SI-5 crossing owned by A4

`Allowlist` and `Layers` are empty at `1e7807b06` (table above), so no entry lacks a rationale and no stale entry
exists. The four SI-5 crossings are owned elsewhere (`crossings`, SI-5): `prompts → ops` by A8 (Scope 19),
`jsonout → ops` by A7 (Scope 17), `commands → agent` and `tui → agent` by A9 (Scope 20), `agent → db` by C6
(Scope 21, A9 data). None is assigned to A4. A4 added no production package: `internal/ops` is its only package
root and `go list ./internal/ops/...` lists that one package. G2 holds.

## G3 — A4 Shim Register empty; no alias or forwarder added

The register is empty (arm-1-ar-13 plan, `Shim Register`). Inputs: `a4-closure-readiness.md` §3 (prospective for
this task's purposes), verified independently:

- AST scan (`go/parser`, not text search) of the 41 partition files at the starting commit and of the 60 A4 files at
  `1e7807b06`: type aliases (`type X = Y`) 0 → 0; one-statement forwarders whose arguments equal the function's
  parameters, 3 → 3, identical names at both revisions: `collectNonTerminalByKind` and `resolvePerSubtaskSiblings`
  (`proceed.go`), `syncArchiveFile` (`task_archive.go`). They exist at the starting commit and are pre-existing
  compatibility API, not shims introduced by A4. The readiness record counts a fourth, `(*handoffInputError).Error`,
  a method with an error-wrapping accessor outside this scan's function-only shape; it is also unchanged.
- Text check: no added line matching `^\+type \w+ = ` in `git diff 96c9ad69a 1e7807b06 -- 'internal/ops/*.go'
  ':!internal/ops/*_test.go'`.
- Exported surface: 181 declarations at the start and at `202d531fc`, none removed, added or changed (readiness
  record §4, a `go/printer` comparison not re-run here; the Go tree is unchanged since `202d531fc`).

G3 holds: zero shims added.

## G4 — suite green at the integration revision

All commands run from the task worktree at `1e7807b06` in the canonical form
`env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp <command>`; each was run once and not retried.

| Cmd | Command | Start (UTC) | End (UTC) | Exit | Excerpt |
|---|---|---|---|---|---|
| C0 | `go test -count=1 ./internal/testguard/archguard/` | 17:35:43 | 17:35:44 | 0 | `ok .../internal/testguard/archguard 0.125s`; `-run TestAreaA4RatchetIsEmpty -v`: `--- PASS` |
| C1 | `make test` | 17:35:47 | 17:40:05 | 0 | 83 `ok` lines, 0 `FAIL`, 0 `(cached)`; `internal/ops` 186.764s, `internal/oracle/a4` 47.379s, `internal/agent` 197.901s, `internal/commands` 40.002s, `cmd/liza` 110.756s |
| C2 | `make lint` | 17:40:21 | 17:41:11 | 0 | `go vet ./...` for the three target OS, then `ok .../internal/testguard/archguard 0.137s` (`lint-archguard`) |
| C3 | `make test-e2e` | 17:41:11 | 17:42:46 | 0 | `ok .../internal/integration 80.922s` (`-race -tags e2e`, `TestFullSprintSequence`, `TestTerminalDependencyRecovery`) |
| C4 | `go test -race ./internal/ops/...` | 17:42:46 | 17:49:14 | 0 | `ok .../internal/ops 326.696s` (the only package under `./internal/ops/...`) |

Task validation index 3 (`make test-e2e`) and index 4 are C3 and C4. The task's canonical list is re-run by the
submission on the clean rebased commit, which is the validation of record for the manifest; the figures above are
this record's observation at the pinned revision. G4 holds: no red suite is recorded as green.

## G5 — wall time and benchmarks against `baseline.md` (indicative; never blocks)

Protocol repeated from `baseline.md`: fresh disposable clone outside the worktrees
(`/tmp/a4g5clone.mqrVG4/repo`: `git clone --local --no-checkout <project root>` then `git checkout --detach
1e7807b0…`; clone `HEAD` printed `1e7807b0688691feef5369ff8d06953cfc8c174e`), sync-clean check (`make
sync-embedded` exit 0, `git status --short` empty, 0 bytes), then one warm-up `make test`, one timed `make test`,
then `timeout 300 go test -run '^$' -bench . -benchmem ./internal/<pkg>` under `env -i HOME="$HOME" PATH="$PATH"
TMPDIR=/tmp GOFLAGS=-count=1`. No retry. The clone's `git status --short` was empty afterwards and the clone was
deleted (`ls -d /tmp/a4g5clone.*` matches nothing).

- Host load average (1, 5, 15 min): before warm-up (17:49:46Z) `3.88 4.33 7.72`; before timed run (17:53:58Z)
  `5.63 6.60 8.05`; after timed run (17:57:23Z) `6.22 6.90 7.93`. The baseline ran at about 30 on 8 cores, so
  the comparison is load-confounded in this run's favour.

| Run | Start (UTC) | End (UTC) | Wall s | Exit | Output |
|---|---|---|---|---|---|
| Warm-up | 17:49:46 | 17:53:58 | 252 | 0 | 83 `ok`, no `FAIL` |
| Timed | 17:53:58 | 17:57:23 | 205 | 0 | 83 `ok`, no `FAIL`, no `(cached)` |

N4: the baseline timed run was 523 s, red (two `internal/agent` deadline tests) and taken under load about 30;
+25% is 653.75 s. 205 s is 61% below it and inside the bound; against the green 444 s warm-up (a separate
observation) the timed run is 54% below. **Disposition:** within bound; recorded with the baseline's load
caveat, since both numbers favour this run.

Benchmarks: all three packages exited 0 with `PASS` (`internal/db` 72.067s, `internal/commands` 3.767s,
`internal/agent` 3.811s); all 18 benchmark rows of the baseline were captured, none "not captured".

| Benchmark | ns/op base → now | B/op, allocs/op |
|---|---|---|
| db `BlackboardWrite` | 12033415 → 2367782 (−80%) | same |
| db `BlackboardModify` | 22577288 → 2588617 (−89%) | B/op +0.5%, allocs +0.6% |
| db `BlackboardReadCached` | 44938 → 37537 (−16%) | same |
| db `BlackboardReadUncached` | 1890776 → 258241 (−86%) | same |
| db `MarshalStateForWriteLarge/repaired=false` | 536001675 → 276543085 (−48%) | same |
| db `MarshalStateForWriteLarge/repaired=true` | 1331986515 → 267320917 (−80%) | same |
| db `ReadCachedHit` | 4916749 → 2247579 (−54%) | same |
| db `StateParse` | 112041835 → 61465793 (−45%) | same |
| db `TerminalArchiveTransaction/inline/cold-instance` | 772451364 → 504783886 (−35%) | same |
| db `TerminalArchiveTransaction/inline/warm` | 628528774 → 518824018 (−17%) | same |
| db `TerminalArchiveTransaction/archived/cold-instance` | 320733652 → 323586982 (+0.9%) | same |
| db `TerminalArchiveTransaction/archived/warm` | 185103984 → 62384165 (−66%) | B/op −8.2%, allocs −15.6% |
| db `TerminalArchiveTransaction/archived-256/cold-instance` | 990744276 → 989109652 (−0.2%) | same |
| db `TerminalArchiveTransaction/archived-256/warm` | 319924638 → 197133501 (−38%) | B/op +2.9%, allocs +5.9% |
| commands `InspectionSnapshotDuringHeartbeat/status` | 9380378 → 8954935 (−4.5%) | B/op +12.5%, allocs +6.7%; heartbeat-writes 6 → 24 |
| commands `InspectionSnapshotDuringHeartbeat/get` | 4376937 → 2969905 (−32%) | B/op +6.4%, allocs +5.3%; heartbeat-writes 13 → 26 |
| agent `BenchmarkClaimReviewerTask` | 34512825 → 9191352 (−73%) | same (B/op −0.1%, allocs +0.0%) |
| agent `BenchmarkResumeHandoff` | 5936738 → 5212268 (−12%) | same (B/op −0.0%, allocs +0.0%) |

"same" means within ±0.5%. **Dispositions** (N3 is Should; none blocks):

- **A4 paths.** A4 has no benchmark of its own. The two baseline benchmarks nearest A4 code are the `status` and
  `get` inspection snapshots, which reach `pipeline_ops.go` and `plan_handoff.go` (A4). Their change since the
  starting commit is naming constants and a typed cardinality comparison (`git diff 96c9ad69a 1e7807b06 --
  internal/ops/pipeline_ops.go internal/ops/plan_handoff.go`: 31 insertions, 14 deletions, no control-flow or
  allocation change).
- **No ns/op slowdown.** One row is above its baseline: `archived/cold-instance` +0.9% at 3 iterations, within
  the noise of a three-iteration measurement and about 21 percentage points below the 25% bound. Every other row is
  at or below baseline, with the load caveat above.
- **`InspectionSnapshotDuringHeartbeat/status` and `/get` per-iteration allocations** (B/op +12.5% and +6.4%,
  allocs +6.7% and +5.3%): out-of-bound as allocation, not attributable to A4. The figures equal those recorded at
  the A3 boundary (`boundary-a3.md` G5: +12.5% / +6.6% and +5.0% / +4.3%), where the same rise was already present
  before any A4 refactor merged, and the A1 boundary recorded the same direction. The benchmark overlaps a
  concurrent heartbeat writer, so work per iteration is not like-for-like (heartbeat-writes per measured window
  6 → 24 and 13 → 26). The benchmark lives in `commands` (A7). Kept as the open indicative observation the A3
  record handed to the A7 boundary: re-measure there; a persistent increase is commissioned then.
- **db `archived-256/warm`** B/op +2.9%, allocs +5.9%: out-of-bound as allocation, not attributable to A4. The
  benchmark exercises `internal/db` only (A1), which sits below `ops` in the layer map and imports none of it; A4
  changes `internal/ops` only. It is a warm-path figure at 6 iterations, and the same row was −3.4% / −6.8% in the A3
  record, so the sign changes between runs. Recorded as an open indicative observation for the A1/A7 boundary
  re-measurement; no action here.
- `archived/warm` and the other allocation deltas are decreases or zero: no action.

## G6 — architectural issues resolved by A4

Reviewed the Open Issues Summary (67 open at `1e7807b06`, `specs/architecture/architectural-issues.md`) and the
candidate sections against the A4 commits listed above, under `issues-policy` (the file's Update Policy).

**Closed: "Decompose proceed.go (1,500 LOC)" (`issue-proceed`).** Its stated scope is a decomposition of
`internal/ops/proceed.go` by responsibility cluster. Wholly resolved by `1faa264af` and `5ebceec54` (merged by
`4e55d734a`), within S1:

| Cluster named by the Direction | Where it lives at `1e7807b06` |
|---|---|
| Transition execution and the available-transitions entries | `proceed.go` (638 lines): `Proceed`, `proceedInner`, `proceedManyToOneInner`, `ExecuteAvailableTransitions`, `ExecuteTransitionsReportWith` |
| Child construction | `transition_children.go` (429) |
| Cohort management | `transition_cohort.go` (222) |
| Phase-gate dependency propagation | `transition_inherited_deps.go` (191) |
| Graph algorithms (topological sort, SCC cycle detection) and the pass engine | `transition_pass.go` (621): `topoSortPending` 89 lines, `findSCCs` |
| Crash recovery | `transition_recovery.go` (304): `recoverCrashedTransition` 15 lines (183 when the issue was written) |

The issue's measured symptoms are gone: file 2,052 lines at the starting commit (1,500 in the title) → 638;
`ExecuteAvailableTransitions` 218 → 10 lines; `recoverCrashedTransition` 183 → 15; no function over 100 lines and
no file over 800 in A4 (G1). The Direction's last sentence, evaluating design-level improvements (P3.4), is a
recommendation about later work and was not an A4 deliverable; the Fix Details entry says so. Registry edits made
in the same change, per the Update Policy: the active section, its Table of Contents line and its Open Issues
Summary row removed; a Completed Fixes entry, a Fixed (Traceability) row and a Fix Details entry added; counts
changed from 17 high / 34 medium / 16 low (67) to 17 / 33 / 16 (66), the same count as before the edit less this one
medium item.

**Non-closures (each stays open, reason recorded):**

- "Control-Flow Vocabulary Bypasses Domain Ownership" (`issue-vocabulary`): stays open. A4 typed and named its own
  vocabulary (`27ebce221` provider waits, `15f52511f` archive, `9600d2f0f` and `5163454ba` K7 families J1–J22),
  but the issue spans every area. Raw role-type comparisons remain in other areas at `1e7807b06`, for example
  `internal/commands/watch.go`, `internal/agent/registration.go`, `cmd/liza/cmd_launch.go` (A7, A9) and A5 files
  in `internal/ops` (`quarantined_verdict.go`, `reaffirm_proof.go`, `submit_review.go`). The whole stated scope is
  not resolved.
- "Status Vocabulary Partition Between Go Constants and Declarative Pipeline": stays open. It cites A4 files
  (`sprint_checkpoint.go`) but describes `internal/models/task_status.go` binding generic constants to
  code-pipeline values and `internal/pipeline` converting back. A4 changed no partition behavior (outside C1–C6),
  and the constants are unchanged.
- "Single-Goal Data Model Constrains Applicability": stays open. It cites `proceed.go` only as the place that
  populates `ParentTask` and `ParentTasks`; the single-goal schema is unchanged and outside C1–C6.
- "Destructive-DB Break-Glass Issued and Consumed by the Same Actor Class": stays open. It concerns validation
  command safety and who may set the break-glass marker. It cites `add_tasks.go` and `set_task_output.go` as task
  writing paths that accept the marker; A4 refactored them without behavior change (`f75bcf0be`, `59f0d42d0`), so
  the authority gap it describes is untouched.
- "INVARIANTS.md §7 Clean Worktree Not Enforced at Submission" and "INVARIANTS.md §6 Provider Diversity Not
  Enforced at Verdict Time": stay open. They are A5 behaviours (`submit_review.go`, `submit_verdict.go`),
  preserved by the A5 refactor (`goal-wont`, X9); A5 closures belong to the A6 boundary.
- No other open item names an A4 file: a search of the registry for the 41 partition file names
  (`\b<name>\.go`) matches only the three items above, the closed `proceed.go` item, and three already-fixed
  entries (`add_tasks.go`, `proceed.go` evidence lines). "Status Vocabulary…", "Single-Goal Data Model…" and
  "Destructive-DB Break-Glass…" are the three items the plan names as describing behaviour outside C1–C6.

## Refresh at R2 (`20fee7197f2548b8e9844154a874700a603b8aee`)

Reason: the dispatched G8 run at R1 was red on macOS (section G8). The operator-commissioned correction merged
(`47abea4a1`, `20fee7197`) and released this task on 2026-10-25T19:01Z to refresh G1–G7. `git rev-parse
global-refactoring` printed R2 and `git log HEAD..global-refactoring` printed nothing at 19:04Z and 19:17Z.

**Delta R1 → R2** (`git diff --name-only 1e7807b06 20fee7197`): `Makefile`, the correction plan and its output
manifest, and its acceptance manifest under `specs/plans/`. `git diff --stat 1e7807b06 20fee7197 -- internal cmd`
prints nothing: no Go file, test, oracle, guard-data or embedded file changed. The `Makefile` change is two lines
(`go test` → `go test -timeout 30m`) in `test-units-nosync` and `ci-test-slow-nosync`, the targets of the macOS
compat lane (`make -n ci-test-compat-nosync` shows `-timeout 30m` on both). `make test` runs `go test ./...` and
`make lint`, `make test-e2e` and the benchmarks use none of the changed lines.

**G1–G3.** Unchanged by construction and re-read: `internal/testguard/archguard/area_a4_test.go` at R2 still
holds `Layers`, `Allowlist`, `Ratchet` and `S2` as empty slices (lines 9–12); the Go tree is byte-identical to R1,
so the S1 measurements, the SI-5 assignment, the AST shim scan and the exported-surface comparison of G1–G3 apply
unchanged. `TestAreaA4RatchetIsEmpty` passed in C0 below at R2.

**G4 at R2.** `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp <command>` from the task worktree. The first pass
ran the canonical forms; Go's test cache served 69 of 83 packages in C1 and all of C4, which is valid for
byte-identical inputs but is not a fresh run. C1 and C4 were therefore re-run once with `GOFLAGS=-count=1`
(translation of the canonical command; same commands otherwise). Neither pass was retried after a failure; there
was none.

| Cmd | Command | Start–End (UTC) | Exit | Excerpt |
|---|---|---|---|---|
| C0 | `go test -count=1 ./internal/testguard/archguard/` | 19:04:27–19:04:27 | 0 | `ok .../internal/testguard/archguard 0.111s` |
| C1 | `make test` (first pass) | 19:04:27–19:07:22 | 0 | 83 `ok`, 0 `FAIL`, 69 `(cached)` |
| C1 | `make test`, `GOFLAGS=-count=1` | 19:09:29–19:12:31 | 0 | 83 `ok`, 0 `FAIL`, 0 `(cached)`; `internal/ops` 254.434s, `internal/oracle/a4` 34.374s, `internal/agent` 179.445s, `internal/commands` 27.929s, `cmd/liza` 84.946s |
| C2 | `make lint` | 19:07:22–19:07:25 | 0 | `ok .../internal/embedded 0.014s`, `ok .../internal/testguard/archguard 0.117s` (`lint-archguard`) |
| C3 | `make test-e2e` | 19:07:25–19:08:44 | 0 | `ok .../internal/integration 78.545s` (`-race -tags e2e -count=1`) |
| C4 | `go test -race ./internal/ops/...` (first pass) | 19:08:44–19:09:03 | 0 | `ok .../internal/ops (cached)` |
| C4 | `go test -race ./internal/ops/...`, `GOFLAGS=-count=1` | 19:12:31–19:16:46 | 0 | `ok .../internal/ops 169.415s` (the only package under `./internal/ops/...`) |

G4 holds at R2: no red suite is recorded as green. The task's canonical list is re-run again by the submission on
the clean rebased commit, the validation of record for the manifest.

**G5 at R2: carried forward from R1, not re-measured.** The delta identifies no Go change, so the `make test`
wall time and the benchmarks measure the same code and the same recipes. The protocol clone at R1 stays the
measurement of record (205 s timed `make test` against 523 s and +25% = 653.75 s; all 18 benchmark rows
captured; dispositions in G5 above). As a separate, non-protocol observation the uncached C1 above took 182 s in
the task worktree; the 1-minute load average read at 19:16Z, after C4, was 5.56. The two open indicative allocation observations handed to the
A1/A7 boundary re-measurement are unchanged. Never blocks.

**G6 at R2.** `specs/architecture/architectural-issues.md` is untouched by the delta (the delta lists no such
file). The closure of "Decompose proceed.go (1,500 LOC)" and the recorded non-closures in G6 stand at R2.

## G7 — record completeness

This file holds the commands or source reads, both integration SHAs (R1 `1e7807b0688691feef5369ff8d06953cfc8c174e`
for G1–G6 above, R2 `20fee7197f2548b8e9844154a874700a603b8aee` for the refresh), the UTC observations, exits and
excerpts for G1–G6, and the G8 section below. The closing revision is R2. The acceptance manifest
(`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-14-cp-0-boundary.json`) is written once G8 is
green.

## G8 — dispatched CI result (D52, D59, D60): pending at R2

Status: **pending** for R2 `20fee7197f2548b8e9844154a874700a603b8aee`. Under D52 (replacing the per-boundary pull
request), the operator dispatches `.github/workflows/ci.yml` (`workflow_dispatch`) at R2 (on `global-refactoring`
if the head still equals it, otherwise on a pinned ref under D59), applies D60 to any rerun of a pre-existing
intermittent test and cites every attempt, and the maintainer confirms that the Linux, Windows and macOS jobs of
that one run are green. D52 runs macOS in the same dispatched run, so no separate `main`-push macOS follow-up is
expected; if the maintainer instead reports one, its owner and status are recorded here and it is never claimed as
passed before it runs. No pull request to `main` is opened or claimed, and no agent pushes. To be appended on
resume: run URL, tested full SHA (must equal R2, else the run is not G8 evidence), each job's name and conclusion,
every attempt, UTC observation, the ruling.

### History: run 40219462697 at R1 (red, superseded)

Operator evidence, 2026-10-25T18:04Z and 18:20Z. Run
`https://github.com/liza-mas/liza/actions/runs/40219462697`, `workflow_dispatch` on the pinned ref `g8/a4-1e7807b`,
`headSha` `1e7807b0688691feef5369ff8d06953cfc8c174e` (= R1; `global-refactoring` was pushed at the same SHA). This
run is **not** G8 evidence for the closing revision and is not deleted from the record.

- Linux tests, Linux lint and build, and all Windows jobs: green.
- **Attempt 1, macOS: red.** `internal/ops` hit `panic: test timed out after 10m0s` (600.094 s): the package
  outgrew Go's default per-binary timeout, because the compat lane's `test-units-nosync` and `ci-test-slow-nosync`
  carried no `-timeout` (macOS `internal/ops`: 447 s at the A1 G8, 481 s at A2, 488 s at A3, over 600 s here).
  `internal/ops` is changed code on the branch, so D60 does not permit a rerun to count. Also
  `internal/integration` `TestCodexACPProviderUnavailableLeavesUnrelatedFailureOnCrashPath` timed out at 20 s
  (`provider_unavailable_acp_test.go:220`); its test file is unchanged on the branch, a candidate intermittent
  timing failure under D60.
- **Attempt 2, macOS: failed, informational only.** The operator reran the failed macOS job; it failed on an
  intermittent A9 golden (`i14-decided`, T3/T4 numbering). It cannot count as G8 under D60 for the `internal/ops`
  timeout.
- Correction: `arm-1-ar-13-g8-timeout-correction-plan-code-0` merged as `47abea4a1` and `20fee7197` (explicit `-timeout
  30m` on the two compat-lane targets, no test, assertion or package-list change). R2 is the integration revision
  after it. Red CI keeps A5 coding held until a green G8 at R2.

If CI tests different code, G1–G7 are re-run at that revision and both are kept (documentation-only deltas
identified by diff); this record is not relabeled. Red CI keeps A5 coding held and the maintainer opens the fix
task.
