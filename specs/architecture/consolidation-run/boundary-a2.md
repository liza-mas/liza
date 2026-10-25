# Architecture Consolidation Run: Area Boundary Record A2 (Platform adapters and distribution)

Evidence of the A2 area-boundary checkpoint (`Area-Boundary Checkpoint Contract`, checks G1–G8) from the
arm-1-ar-12 plan, Scope 1, task `arm-1-ar-12-cp-0-code-0`. One record per boundary; later units never edit
it. It changes no production, test, guard-data, oracle or baseline file. Only `architectural-issues.md` (G6) and
the acceptance manifest are written besides this record.

## Observed revision

- Configured integration branch: `omni-ee -C <project root> get config --json`, field
  `IntegrationBranch` = `"global-refactoring"` (observed 2026-10-25T04:05Z); equals the D23 ruling cited by
  `baseline.md`.
- **Integration revision under test (refresh, 2026-10-25T06:10:43Z):**
  `3ffd032fcb24b9b914c42da29af3d93ed5cc55e5` (`test(oracle): mask every path spelling in the Git shim fidelity
  probes`, the last commit of the A3 portability correction `arm-1-ar-12-cp-2-d49-portability-plan-code-0`).
  `git rev-parse global-refactoring` printed this SHA at 06:10:43Z, when the operator's unblock had rebased the
  task worktree onto it (worktree `HEAD` at that time: `5696979e2`, this record's first commit, on top of it).
  Everything below (G1–G7, except where "b3356c3c6" is named as superseded) refers to this SHA.
  `global-refactoring` has moved on since (first seen at the check after C4, after 06:22:00Z): `f474104d2` adds
  four A3-lane commits (`0ca9db493`, `408bb4c1a`, `f04ab36d9`, `e76d6d748`: `internal/ops` and oracle work). `git diff
  --stat 3ffd032fc global-refactoring` over the 26 A2 package dirs, `internal/testguard` and `Makefile` printed
  nothing, so they do not change the A2 code or guard data; this record deliberately stays pinned to `3ffd032fc`
  (the SHA the maintainer's workflow_dispatch branch must point at, D59).
- **Superseded revision.** The first G1–G7 pass and the first G8 dispatch ran at
  `b3356c3c62742eebd20cfcaa3b36d6c66172c149` (`Merge arm-1-ar-11-cp-4-code-8`, observed 2026-10-25T04:05:47Z).
  Its G8 run (`40162651273`) was **red** and is superseded (see G8). `git diff --stat b3356c3c6 3ffd032fc` over the
  26 A2 package dirs, `internal/testguard/archguard/area_a2_test.go` and `Makefile` printed nothing: the A2 code
  and A2 guard data are byte-identical between the two revisions. The delta (82 files) is the A3 lane (`internal/ops`,
  `internal/oracle/a3/**`, `cmd/liza/lifecycle.go`, `internal/testguard/archguard/area_a3_test.go`, one test in
  `internal/commands`, `INVARIANTS.md`, plans/acceptance/decision records). Source inspections of G1–G3 and G6 hold
  by that identity; C0–C4 and G5 were re-executed at `3ffd032fc` (old results kept only as marked below).
  **Boundary proof versus submission base.** G1–G8 are proved at `3ffd032fc` (D59). The task submission is a
  different, later revision: the task worktree was rebased onto `global-refactoring` at `c7504560c` (which includes
  the A3-lane merges after `3ffd032fc` and decision records), and this task adds three commits on top of it that
  change only this record, `architectural-issues.md` and the acceptance manifest
  (`git diff --stat c7504560c <submission>` lists those three paths). Whole-tree, `git diff --shortstat
  3ffd032fc <submission>` reports 86 files, 2485 insertions, 383 deletions (A3-lane `internal/ops`,
  `internal/agent` and oracle work, specs and acceptance files, plus this task's three paths). Bounded to the A2
  proof surface, `git diff --stat 3ffd032fc <submission> -- <26 A2 package dirs> internal/testguard Makefile`
  prints nothing, so A2 code, all guard data and the Makefile are byte-identical to the checked revision. The
  submission-time canonical validation (receipt at the submission commit) therefore also covers the A3-lane
  changes and supplements, but does not replace, the G4 runs at `3ffd032fc`.
- Starting commit of the run (for A2 diffs): `96c9ad69aee983ce2b929dd681b6ba43bf920535`.
- Inherited barrier (sequence): all 23 dependencies of this task are `MERGED` in `omni-ee get <id> --json`
  (observed 2026-10-25T04:20Z): `deferred-validation-2-1-correction-plan-code-0` (D41 repair), A1 boundary
  `arm-1-ar-11-cp-0-code-0`, the A2 coding units `arm-1-ar-11-cp-1-code-0`, `cp-2-code-0..2`, `cp-3-code-0..3`,
  `cp-4-code-0..9`, and the A3 oracle units `arm-1-ar-3-cp-0-code-0/1`, `cp-1-code-0`. Per D58 the A3/A4
  refactor coding no longer waits on this task; a red result here holds A2 closure and run close, not A3–A7.
- A2 coding commits on the integration branch (`git log --no-merges --oneline 96c9ad69a..HEAD -- <26 A2
  package dirs>`, 20 commits): `20d654f7b` embedded split, `81f255467` pairingindex, `f294d97d6` scipsearch,
  `d70a9425a` semble, `b9f880caa` providers, `f2d76a7a1` updater, `b9e590798` toolresult decomposition,
  `e72328595` scipsearch/envgate types,
  `9f5ebfabc` + `c3d33499b` providers, `2db9ec255` toolchain, `7dca57814` updater channel, `fae21b207`
  toolresult hooks, `db5f7f7d6` toolresultacp, `ba7b79b83` sessionvalidation preflight codes, `b11752a40` +
  `af042c62f` git object types, `143a984f7` and `387ad00b0` residual named values, `905adaa4e` toolresultacp
  test bound; plus `f6ead601c` (catalog S2 guard data), which lies outside the A2 package dirs. Aggregate over the A2 dirs: 83 files, 8722 insertions, 6585 deletions.
- Environment for every measurement: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` (GUARDRAILS G1.4);
  `GOFLAGS=-count=1` is added to the same `env -i` call for C1–C4 and G5 (translation of the canonical form
  so no result is served from the Go test cache; C0 passes `-count=1` itself). The `go test` commands ran with
  `go -C <worktree>` and the make targets with `make -C <worktree>`, translations of the canonical
  "run from the worktree root" form (BASH CONSTRAINTS forbid `cd <path> &&`). No remote push, provider CLI,
  installation or update ran; output below is sanitized (no environment values).
- Operator notice of 2026-10-25T02:00Z (cache trim errors `could not import …` / `cannot open file`, 00:37Z–01:54Z)
  does not apply: the runs at `3ffd032fc` started at 06:10Z or later (the superseded ones at 04:06Z or later) and
  none printed such an error.
- Host: `Linux 6.8.0-147-generic x86_64`, `nproc` 8, `go1.26.0 linux/amd64`; shared host, other agent sessions
  running (load below).

## G1 — A2 ratchet empty; S2 exceptions data-like with rationale

- Source: `internal/testguard/archguard/area_a2_test.go` at `b3356c3c6` (read in full, 45 lines); byte-identical at
  `3ffd032fc` (no diff, see Observed revision). Excerpt (lines 36–44):

  ```go
  Allowlist: []AllowEntry{},
  Ratchet:   []RatchetEntry{},
  S2: []S2Entry{
      {
          Key:       Key{File: "internal/toolchain/catalog.go", Func: "Catalog"},
          Category:  S2Table,
          Rationale: "declarative tool catalog: one literal entry per tool",
      },
  },
  ```

- Empty Ratchet is recorded here independently of C0: the guard permits remaining debt, so the empty literal
  is the evidence that all A2 offenders were removed or converted. The start snapshot's A2 count was 6 files /
  2 functions (`baseline.md`; `s1_snapshot_test.go`: `embedded/embedded.go` 1842, `pairingindex/hooks.go` 1210,
  `providers/catalog.go` 804, `scipsearch/scipsearch.go` 1564, `semble/semble.go` 804, `updater/updater.go`
  1323; `toolchain/catalog.go:Catalog` 153 lines, `toolresult/claude.go:ClaudePostToolResult` 107 lines). All 6
  files and `ClaudePostToolResult` were reduced below threshold by the splits and decomposition (`20d654f7b`,
  `81f255467`, `f294d97d6`, `d70a9425a`, `b9f880caa`, `f2d76a7a1`, `b9e590798`); `Catalog` is the single entry
  converted to S2 (`f6ead601c`).
- S2 inspection (one entry). `Catalog` (`internal/toolchain/catalog.go:93-245`, about 153 lines, over the
  100-line function threshold): category `table`, rationale as above. The body is a single `return []Tool{…}`
  composite literal of tool descriptors plus eight `manualTool(…)` constructor calls; a scan of lines 93–245
  finds no `if`/`for`/`switch`/`select`/`go`/`defer` and the only `return` is the literal. It is data-like under
  goal-success S2 ("tables, registrations, generated code"), the rationale states why, and the file
  (`catalog.go`, 313 lines) is under 800. This is the prospective exception the plan named; it is data-like, so
  no human ruling is required (none exists or is claimed).
- Independent size check: largest non-test files in the 26 A2 package dirs are
  `internal/toolresult/redact.go` 654, `internal/toolchain/install.go` 506, `internal/brandrender/render.go`
  482, `internal/git/worktree.go` 477, `internal/pairingindex/index_script.go` 453 (all < 800);
  `internal/embedded/embedded.go` went from 1842 lines at the start to 128.
- C0 at `3ffd032fc`: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go -C <worktree> test -count=1
  ./internal/testguard/archguard/`, run between 06:10:43Z and 06:13:08Z (start/end not stamped), exit 0:
  `ok  github.com/liza-mas/liza/internal/testguard/archguard  0.124s`. (Superseded run at `b3356c3c6`,
  04:06:08Z–04:06:10Z, exit 0, 0.507s.)
- Result: pass.

## G2 — A2 allowlist data holds only rationale-carrying entries; no SI-5 crossing owned by A2

- Source: same file. `Allowlist: []AllowEntry{}`: no entry exists, so none lacks a rationale and none can be
  stale. Layer entries cover all 26 A2 packages (27 entries with `internal/brandrender/cmd/sync-embedded`), each
  `layerInfrastructure`, matching the Area Partition list (git, gitenv, gitbash, worktreeexclude, subprocess,
  procscan, secretmask, toolresult, toolresultacp, sessionvalidation, precommit, projectdetect, initcheck,
  codexconfig, bash-policy-cli, providers, embedded, brandrender, scipsearch, stacklit, semble,
  functionalclusters, pairingindex, toolchain, updater, termutil).
- Remaining allowlist rationales: none (the A2 allowlist is empty). The only A2 data rationale is the S2 entry
  inspected in G1.
- SI-5 (`20261023-162147-arm-1.md#SI-5 K3 Crossing Resolutions`, pinned `74212192d`) assigns its four
  crossings to Scope 19 (A8), Scope 17 (A7), Scope 20 (A9) and Scope 21 (C6, in A9); none is owned by A2.
  Result: no A2-owned crossing.
- The C0 `TestK4ImportDirectionGuard` pass also proves no unlisted forbidden edge and no stale allowlist entry
  exists (the package passes; C1 and C2 at `3ffd032fc` repeat it: `ok … archguard 0.146s` and `0.149s`). Result: pass.

## G3 — A2 Shim Register empty; no temporary alias or forwarder added

- Register: `20261023-195624-arm-1-ar-11.md#Shim Register` (pinned `cb79e6713`) reads "None."
- Inspection of every A2 code change, `git diff -U0 96c9ad69a HEAD -- <26 A2 package dirs> ':!*_test.go'`, run at
  `b3356c3c6`; the A2 dirs are byte-identical at `3ffd032fc` (empty `git diff --stat` between the two), so the
  `96c9ad69a`-relative diff and every count below are the same at the new revision:
  - added `type X = Y` alias declarations: 0 (`grep -cE '^\+\s*type\s+\w+(\[[^]]*\])?\s+=\s'`, count 0).
  - added lines containing `deprecated|shim|forwarder|compat alias|temporary` (case-insensitive): 5, all the
    word "temporary" in the updater's `replaceExecutable` comment and its four `temporary binary` error
    strings; none declares a shim.
  - added one-line forwarder functions (a function whose whole body is a single `return f(...)`), found by
    scanning every non-test function at `HEAD` and at `96c9ad69a`: 69 at `HEAD`, 68 at the start; exactly one
    name exists only at `HEAD`: `normalizeChannel` (`internal/updater/preferences.go:215`), added by
    `7dca57814`. It is the permissive boundary conversion from raw channel text to the new `Channel` type
    (`Channel(strings.ToLower(strings.TrimSpace(raw)))`), has five callers in the same file, and keeps no old
    name alive; the old string-typed helpers (`validateChannel`, `updateChannel`, `updateChannelFlag`) were
    retyped, not forwarded. Not a shim. The other 68 are historical one-liners that already existed at the
    starting commit (or moved with a file split) and are not compatibility forwarders.
  - exported API of `internal/embedded` (the one A2 package with an explicit "keep the package API stable"
    requirement): the 38 exported `func/type/var/const` declarations at `96c9ad69a` and `HEAD` are identical
    (`diff` of the sorted declaration lists printed nothing).
- Result: pass; A2 coding left no temporary alias or forwarder, and the register was never populated.

## G4 — suite green at the integration revision

All five canonical commands ran at `3ffd032fc` in the task worktree (`git status --short` empty after C4, so the
generators — `make sync-embedded` inside `test`/`lint` — left tracked embedded artifacts unchanged). Load average
(1, 5, 15 min) at each start is shown; C1–C4 ran serially, each under `env -i HOME="$HOME" PATH="$PATH"
TMPDIR=/tmp` (+`GOFLAGS=-count=1` for C1–C4).

| Cmd | Command | Start (UTC) | End (UTC) | Exit | Load at start | Excerpt |
|---|---|---|---|---|---|---|
| C0 | `go test -count=1 ./internal/testguard/archguard/` | before 06:13:08 | (not stamped) | 0 | not captured | `ok … archguard 0.124s` |
| C1 | `make test` | 06:13:08 | 06:17:27 | 0 | 1.75 4.68 5.78 | 83 `ok`, 0 `FAIL`, 0 `(cached)`; 4 `[no test files]`; `ok …/internal/testguard/archguard 0.146s`; last: `ok …/scripts/ci/testshard 0.298s` |
| C2 | `make lint` | 06:17:44 | 06:18:27 | 0 | 11.09 9.58 7.70 | `Embedded artifacts are consistent with masters`; `go vet ./...` for `GOOS=linux`, `windows`, `darwin`; archguard `ok 0.149s` |
| C3 | `make test-e2e` | 06:19:05 | 06:20:35 | 0 | 7.05 9.09 7.71 | `ok …/internal/integration 80.080s` (`TestFullSprintSequence`, `TestTerminalDependencyRecovery`, `-race -tags e2e`) |
| C4 | `go test -race <A2 roots>` | 06:20:44 | 06:22:00 | 0 | 2.19 6.92 7.08 | 25 `ok`, 0 `FAIL`, 0 `(cached)`, 2 `[no test files]` |

Superseded run at `b3356c3c6` (04:06Z–04:17Z): C0–C4 all exit 0 with the same counts (83 `ok` in C1, 25 `ok` in C4).

C4 selection reconciled with the delivered layer data: the 24 package roots plus `./internal/embedded/...`
and `./internal/brandrender/...` expand to 27 packages — the 26 A2 layer entries plus
`internal/brandrender/cmd/sync-embedded` — and the layer data holds the same 27. Results: git 2.872s, gitenv
1.241s, gitbash 1.023s, worktreeexclude 12.919s, subprocess 6.023s, procscan 1.023s, secretmask 1.016s,
toolresult 72.747s, toolresultacp 10.094s, sessionvalidation 2.224s, precommit 1.113s, initcheck 1.323s,
codexconfig 1.011s, bash-policy-cli 1.023s, providers 1.425s, embedded 23.310s, brandrender 8.979s,
scipsearch 7.391s, stacklit 1.290s, semble 1.115s, functionalclusters 1.384s, pairingindex 9.008s, toolchain
1.134s, updater 1.390s, termutil 1.045s; `internal/projectdetect` and `internal/brandrender/cmd/sync-embedded`
have no test files (projectdetect has none at the starting commit either).

Not run locally, by design: windows-tagged A2 tests such as `internal/procscan/kill_windows_test.go`. `make lint`
compiles them under `GOOS=windows go vet ./...`; the Windows CI of G8 executes them. Result: pass. Remote
results are G8's, not G4's.

## G5 — wall time and benchmarks against `baseline.md` (indicative; never blocks)

Protocol repeated from `baseline.md` at `3ffd032fc`: fresh disposable clone outside the worktrees
(`git clone --local --no-checkout <project root> /tmp/a2g5r2/repo`, `git checkout --detach 3ffd032fc…`; clone
`HEAD` printed `3ffd032fcb24b9b914c42da29af3d93ed5cc55e5`). Sync-clean check: `make sync-embedded` exit 0 and clone
`git status --short` empty. Environment `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp GOFLAGS=-count=1`; one
warm-up and one timed `make test`, neither retried; then `timeout 300 go test -run '^$' -bench . -benchmem
./internal/<pkg>` per package (db, commands, agent), each exit 0 with `PASS`. The clone's `git status --short` stayed
empty; the clone is deleted after this record is written (`ls -d /tmp/a2g5r2` then fails).

| Run | Start (UTC) | End (UTC) | Wall s | Exit | Load at start | Output |
|---|---|---|---|---|---|---|
| Warm-up | 2026-10-25T06:22:33Z | 2026-10-25T06:26:14Z | 221 | 0 | 5.49 7.22 7.20 | 83 `ok`, no `FAIL`, no `(cached)` |
| Timed | 2026-10-25T06:26:14Z | 2026-10-25T06:29:34Z | 200 | 0 | 6.28 7.86 7.61 | 83 `ok`, no `FAIL`, no `(cached)` |

Load after the timed run: 4.18 7.55 7.67.

**N4 comparison (+25%).** The baseline holds two numbers that are not interchangeable: the timed run of 523 s,
which was red (exit 2, two `internal/agent` deadline tests failed under load about 30) and load-confounded
(bound +25% = 653.75 s), and a separate green warm-up observation of 444 s (bound 555 s). Timed 200 s is −61.8%
vs 523 s and −55.0% vs 444 s; warm-up 221 s is −57.7% and −50.2%. Both are inside either bound. The 444 s
figure is shown only as a separate observation, never substituted for the 523 s reference. Caveats: load here
(about 4–8 on 8 cores during `make test`) is lower than the baseline's (about 9–30) and the test set is larger now
(83 packages with `ok` vs 66), so the comparison is indicative, not like-for-like. Disposition: N4 holds; no
action. (Superseded run at `b3356c3c6`: warm-up 266 s, timed 277 s, both exit 0.)

**N3 benchmarks.** One protocol run per package (run 1), baseline numbers from `baseline.md` (load about 30).
Missing result: none — all 18 benchmark rows of the baseline (14 db, 2 commands, 2 agent) were captured. The host
load was **not** stable across the benchmark window: 1-min load was 4.18 when the timed run ended (06:29:34Z) and
50.10 at 06:32:11Z (5-min 25.32, 15-min 14.25), i.e. another session's suite started during run 1. A supplementary
second pass (run 2, 06:32:11Z–06:34:29Z, load 45–50 throughout, listed as an extra observation; the protocol forbids
retrying `make test`, not diagnosing a benchmark) was slower on almost every row, which confirms the host, not the
code, drives the ns/op swings. ns/op deltas are run 1 against baseline; B/op and allocs/op are run 1 changes
(0.0 where within ±0.1%).

| Benchmark | ns/op base → run 1 | run 2 ns/op (load 45–50) | B/op | allocs/op |
|---|---|---|---|---|
| db `BlackboardWrite` | 12033415 → 1922396 (−84%) | 7255949 | −0.1% | 0.0% |
| db `BlackboardModify` | 22577288 → 2061810 (−91%) | 10813586 | +0.6% | +0.6% |
| db `BlackboardReadCached` | 44938 → 17488 (−61%) | 221016 | 0.0% | 0.0% |
| db `BlackboardReadUncached` | 1890776 → 178078 (−91%) | 2825952 | −0.1% | 0.0% |
| db `MarshalStateForWriteLarge/repaired=false` | 536001675 → 140478630 (−74%) | 1303324243 | 0.0% | 0.0% |
| db `MarshalStateForWriteLarge/repaired=true` | 1331986515 → 141484380 (−89%) | 1042313616 | 0.0% | 0.0% |
| db `ReadCachedHit` | 4916749 → 1079413 (−78%) | 13045064 | 0.0% | 0.0% |
| db `StateParse` | 112041835 → 29336572 (−74%) | 425531034 | 0.0% | 0.0% |
| db `TerminalArchiveTransaction/inline/cold-instance` | 772451364 → 337729194 (−56%) | 2910056875 | 0.0% | 0.0% |
| db `TerminalArchiveTransaction/inline/warm` | 628528774 → 474023423 (−25%) | 2142948869 | 0.0% | 0.0% |
| db `TerminalArchiveTransaction/archived/cold-instance` | 320733652 → 430031011 (**+34%**) | 1438877286 | 0.0% | 0.0% |
| db `TerminalArchiveTransaction/archived/warm` | 185103984 → 127786512 (−31%) | 346105946 | −5.7% | −10.9% |
| db `TerminalArchiveTransaction/archived-256/cold-instance` | 990744276 → 12765621814 (**+1189%**) | 6774634673 | 0.0% | 0.0% |
| db `TerminalArchiveTransaction/archived-256/warm` | 319924638 → 3294806068 (**+930%**) | 2902365721 | **+61.9%** | **+123.5%** |
| commands `InspectionSnapshotDuringHeartbeat/status` | 9380378 → 87608564 (**+834%**) | 119270379 | **+22.9%** | **+12.1%** |
| commands `InspectionSnapshotDuringHeartbeat/get` | 4376937 → 32782161 (**+649%**) | 27367262 | **+7.1%** | **+5.9%** |
| agent `BenchmarkClaimReviewerTask` | 34512825 → 72314661 (**+110%**) | 25092229 | +0.8% | −0.1% |
| agent `BenchmarkResumeHandoff` | 5936738 → 43153582 (**+627%**) | 26319946 | +1.0% | +0.5% |

**Dispositions** (N3 is Should; none blocks):

- Eleven of eighteen rows have ns/op at or below baseline in run 1 with B/op and allocs/op within ±0.6% or lower.
  Seven are slower (db `archived/cold-instance`, `archived-256/cold-instance`, `archived-256/warm`; commands
  `status` and `get`; agent both). None of them is attributable to A2:
  - **A2 code is unchanged** between `b3356c3c6` (benchmarks 04:18Z–04:28Z at load 5–17: db and agent rows all at
    or below baseline, `ClaimReviewerTask` −86%, `archived-256/cold-instance` 634486239) and `3ffd032fc`: no A2
    package, no `internal/db` and no `internal/agent` file changed (`git diff --stat b3356c3c6 3ffd032fc`; the
    `internal/agent` and `internal/db` trees are absent from its 82-file list). The same code measured
    about two hours earlier ran the seven slower rows 2.5–27 times faster, so the swing is the host's load spike
    (run 1 straddled it; run 2 sat in it), not a regression.
  - db `archived-256/warm` B/op +61.9% and allocs/op +123.5% (also in run 2: 99079360 B/op, 556979 allocs/op) are
    count-per-iteration artefacts: both archived-256 rows ran a single iteration (`b.N` = 1), so the "warm" row
    absorbs first-iteration (cold) allocations that more iterations amortise; at `b3356c3c6` the same row showed
    −1.8% and −3.5%. Slower iterations lower the `b.N` Go chooses, which fits this pattern; no `internal/db` change is involved.
  - commands `InspectionSnapshotDuringHeartbeat`: ns/op is load-driven (heartbeat-writes 5 and 4 vs 28 and 22 at
    the previous run and 6 and 13 at the baseline; max heartbeat latency 381679 and 594041 µs vs about 214000–
    236000 at the baseline). The allocation increases (`status` B/op +22.9%, allocs +12.1%; `get` +7.1% and +5.9%)
    are larger than at `b3356c3c6` (+12.4%/+6.7% and +5.2%/+4.8%) and than at the A1 boundary (+10.5%/+5.6% and
    +5.2%/+4.3%). No A2 package changed between the two measured revisions, and `internal/commands` changed only in
    one test (`replace_task_test.go`) and `cmd/liza/lifecycle.go`. The A3 lane merged `internal/ops` changes
    (`lifecycle_result.go`, `mode_change.go`, `resume_transitions.go`, …) in that interval; whether the snapshot path
    uses them was **not isolated** here. Disposition: not A2 debt; flag to the A3 boundary owner for a re-measure,
    and keep the A7 re-measure (the benchmark lives in `commands`, A7) where a persistent increase would be
    commissioned. The single-run ns/op figures are load-confounded and not evidence of slowdown.
  - agent rows: allocations within +1.0%; ns/op swing is load (run 2: 25 ms and 26 ms vs baseline 34.5 ms and
    5.9 ms).
- `BlackboardModify` B/op +0.6%, allocs +0.6%: within run-to-run variation of a concurrent-write benchmark; no
  action.
- Superseded run at `b3356c3c6`: sixteen of eighteen rows at or below baseline; the two commands rows carried the
  same allocation disposition (+12.4%/+6.7% and +5.2%/+4.8%).

## G6 — architectural issues resolved by A2

Reviewed the Open Issues Summary (68 open, `specs/architecture/architectural-issues.md`), the Structural Debt
section, and every registry line mentioning an A2 package (`grep` for each of the 26 package names: only
`internal/git` and `subprocess` appear, in the Git-routing, init-duplication and PAUSE/ABORT text, which concern
`ops`/`commands`/`cmd/liza` callers, not A2) against the A2 commits listed above.

- **Closed: "Split embedded.go by Artifact Family (1,530 LOC)" (`issue-embedded`).** The dated "1,530 LOC" claim
  is historical (the file was 1842 lines at the starting commit), so closure is judged on the issue's
  Direction, not the size: "Keep the package API stable while splitting implementation by artifact family:
  global corpus, Claude settings, Codex settings and permissions, hooks, project artifacts, and stale-artifact
  cleanup." Evidence at `b3356c3c6`, unchanged at `3ffd032fc` (A2 dirs byte-identical; `make test`, `make lint` and the
  `internal/embedded` race run were re-run green there, G4):
  - `20d654f7b` (`refactor(embedded): split embedded.go into concern files`) replaced `embedded.go` (1842 → 128
    lines) with `global_corpus.go` 226, `claude_settings.go` 287, `codex_permissions.go` 344, `codex_hooks.go`
    250, `toml_edit.go` 246, `project_artifacts.go` 229, `stale_artifacts.go` 53, `write_policy.go` 82,
    `copilot_hooks.go` 82, `opencode_plugin.go` 73; the largest is 344 lines.
  - The exported API is identical (38 declarations, G3 above); `diff --stat 96c9ad69a HEAD -- internal/embedded`
    lists only `.go` files (10), so no embedded data file changed; `make sync-embedded` leaves the tree
    unchanged (G4, G5).
  - `make test` is green (C1, C4 `internal/embedded` ok), and the guard's A2 Ratchet is empty with no S2 entry
    for the package (G1).
  This resolves the whole finding; the registry section is moved to Completed Fixes / Fixed (Traceability) /
  Fix Details and the Open Issues Summary counts drop from 68 to 67 open (35 medium → 34).
- **Stays open: "Control-Flow Vocabulary Bypasses Domain Ownership" (`issue-vocabulary`).** Globally open by the
  plan: A2 adopted its vocabulary (W12 owners `procscan.AgentProcessState`, `bash-policy-cli.Status`,
  `scipsearch`, `semble`, `pairingindex`, `toolchain`, `updater.Channel`, git `ObjectType`) and A1 typed the
  domain values, but raw role strings and transition cardinalities remain in later areas' production code
  (`grep -cE '"(doer|reviewer|orchestrator)"'` over non-test Go files of `internal/commands`, `internal/agent`
  and `internal/ops` matches at `b3356c3c6`; the area adoption sweeps of A3–A9 own them). Partial progress is
  recorded here only; no closure.
- **No other item is wholly resolved by A2.** "Provider catalog onboarding" concerns (registry line 670) still
  require coordinated edits across `providers`, `commands/init`, `embedded` and `agent`; A2 split
  `providers/catalog.go` by concern but did not add the single-owner onboarding the issue asks for. The Git
  routing issue (line 644) concerns `ops`/`cmd/liza` direct `exec.Command("git")` sites (A3/A6/A7).
  Everything else is untouched.

Registry edit (first record commit; the evidence line was extended to name `3ffd032fc` in the refresh): `Split embedded.go …` removed from the Table of Contents entry and Structural
Debt active section and from the Open Issues Summary; added to Completed Fixes (dated 2026-10-25), Fixed
(Traceability) and Fix Details with commit references `20d654f7b`, and the Counts line updated (68 → 67 open;
34 medium).

## G7 — record completeness

This file holds the commands or source reads, the integration SHA (`3ffd032fcb24b9b914c42da29af3d93ed5cc55e5`,
for G1–G6 alike, with the superseded `b3356c3c62742eebd20cfcaa3b36d6c66172c149` marked where its results are kept),
UTC observations, exit codes and excerpts for G1–G6 above, and the G8 section below. Manifest:
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-12-cp-0-boundary.json` (G4 mapped to the
`Makefile` test target with `command_index` 1; every other obligation maps to its declared approved
reference). The boundary proof (G1–G8) is pinned to `3ffd032fc`. The submission commit is a later revision: it
sits on `global-refactoring` at `c7504560c`, which carries A3-lane changes made after `3ffd032fc` (whole-tree diff
86 files, see Observed revision), plus this task's three owned paths. Over the 26 A2 package dirs, `internal/testguard`
and `Makefile` the submission is byte-identical to `3ffd032fc`; outside them it is not. The record is not
re-pinned to the later revision.

## G8 — dispatched CI result (D52/D59): Linux, Windows and macOS

Status: **GREEN** (maintainer ruling, 2026-10-25T07:07:58Z; see "G8 result" below). G1–G7 pass at
`3ffd032fcb24b9b914c42da29af3d93ed5cc55e5`.

- **Superseded, red:** run <https://github.com/liza-mas/liza/actions/runs/40162651273> (workflow_dispatch of
  `ci.yml` on `g8/a2-b3356c3`, headSha `b3356c3c62742eebd20cfcaa3b36d6c66172c149`, completed 2026-10-25T04:51:37Z),
  reported by the operator: green Linux lint and build and Windows ops-1..4 and rest-3; red Linux tests, macOS,
  Windows rest-1 and rest-2. Causes: two A3-lane oracle defects (`internal/oracle/a3/lifecycle`
  `TestOracleUnblockTaskVariants/rebase-on-refusal-conflict/json` golden carrying git-version-dependent stderr; the
  fixture git shim not launchable on Windows) and one Windows wall-clock flake in `internal/agent`
  (`TestExecutionDeadlineMarkerPastItsDeadlineWithLiveProcessIsInterrupted`, passed in the A1 G8 run), none in A2
  code. This run is **not** G8 evidence for this boundary and is kept here, not deleted. The A3 portability
  correction (`de5c259cd`, `7ea9d342d`, `3ffd032fc`) merged afterwards; this refresh re-pins to its last commit.
- **Early signal, not G8:** run <https://github.com/liza-mas/liza/actions/runs/40168040686>, a dispatch on
  `global-refactoring` when its head was `3ffd032fc`. Attempt 1 failed in Windows rest-1
  (`internal/agent` `TestHeartbeatRenewsReviewLease`) and in Linux tests (`internal/oracle/a9`
  `TestOracleA9SignalMatch` i21, "a started process outlived the test"); both are pre-existing intermittent tests
  whose code is unchanged on the branch. Whether failed-jobs reruns of that run can satisfy D52 was escalated to the
  human (D60, below); the run below, not this one, is the G8 evidence.
- **Requested from the maintainer** (D52/D59): a `workflow_dispatch` of `.github/workflows/ci.yml` on a branch
  pointing at exactly `3ffd032fcb24b9b914c42da29af3d93ed5cc55e5` (not the moving `global-refactoring` head), with
  each job's conclusion for Linux, Windows (including the windows-tagged A2 tests such as
  `internal/procscan/kill_windows_test.go`) and macOS (under D52 the dispatch itself runs macOS, so no separate
  main-push follow-up and no pull request).

### G8 result (maintainer, reported by the operator 2026-10-25T07:07:58Z; D52 + D59 + D60)

Verdict: **GREEN.** The human decided D60 ("option 1", decision record commit `c7504560c`); the maintainer
confirmation was given under the standing auto-approve delegation. The run meets every D52/D59/D60 condition: a
`workflow_dispatch` of `.github/workflows/ci.yml` on the pinned ref `g8/a2-3ffd032`, whose tested SHA equals this
record's integration SHA, with every job green after 2 attempts.

- Run: <https://github.com/liza-mas/liza/actions/runs/40169821321>; event `workflow_dispatch`; ref `g8/a2-3ffd032`
  (D59: `global-refactoring` had moved past the boundary SHA); no pull request (D52).
- Tested SHA: `3ffd032fcb24b9b914c42da29af3d93ed5cc55e5` (equals the integration revision of G1–G7).
- Run conclusion: success after attempt 2. Created 2026-10-25T06:38:43Z; last updated 2026-10-25T06:59:34Z.
- Final job conclusions (completedAt, UTC):

  | Job | Conclusion | Completed |
  |---|---|---|
  | Shared CI / Linux tests | success | 06:51:15Z |
  | Shared CI / Linux lint and build | success | 06:40:58Z |
  | Shared CI / macOS compatibility | success | 06:49:08Z |
  | Test (windows) | success | 06:59:33Z |
  | Windows shard ops-1 | success | 06:43:42Z |
  | Windows shard ops-2 | success | 06:42:03Z |
  | Windows shard ops-3 | success | 06:42:26Z |
  | Windows shard ops-4 | success | 06:44:41Z |
  | Windows shard rest-1 | success on attempt 2 | 06:59:05Z |
  | Windows shard rest-2 | success | 06:44:22Z |
  | Windows shard rest-3 | success | 06:46:33Z |
  | Release verified tag | skipped (tag-only job) | |

- macOS follow-up: under D52 the dispatch itself runs macOS (`ci-core.yml`: every non-`pull_request` event), so the
  macOS job above is the follow-up result; owner: the maintainer; status: success. No separate main-push job exists
  or is claimed.
- Attempt 1 failure (D60 evidence): Windows rest-1, `internal/agent` `TestHeartbeatConcurrency`
  (`heartbeat_test.go:404`, "heartbeat not updated", a wall-clock window). `internal/agent/heartbeat.go` and
  `heartbeat_test.go` are unchanged on the branch (the operator's diff of both is empty). Attempt 2 reran only
  rest-1 and it passed. This is recorded as an intermittent timing test, not as an A2 defect; no code was
  changed for it.
- Superseded: run 40162651273 at `b3356c3c6` (red, A3 oracle portability; fixed by `de5c259cd`/`7ea9d342d`/
  `3ffd032fc`), kept above.
- The windows-tagged A2 tests such as `internal/procscan/kill_windows_test.go` ran inside the Windows shards of this
  run; the operator reported shard conclusions only, not per-test output, so no per-test claim is made here.

G1–G8 pass; A2 closes at `3ffd032fcb24b9b914c42da29af3d93ed5cc55e5`. Observed 2026-10-25T07:07:58Z (operator
report); recorded by `arm-1-ar-12-cp-0-code-0`.
