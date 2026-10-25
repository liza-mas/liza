# Architecture Consolidation Run: Area Boundary Record A2 (Platform adapters and distribution)

Evidence of the A2 area-boundary checkpoint (`Area-Boundary Checkpoint Contract`, checks G1–G8) from the
arm-1-ar-12 plan, Scope 1, task `arm-1-ar-12-cp-0-code-0`. One record per boundary; later units never edit
it. It changes no production, test, guard-data, oracle or baseline file. Only `architectural-issues.md` (G6) and
the acceptance manifest are written besides this record.

## Observed revision

- Configured integration branch: `omni-ee -C <project root> get config --json`, field
  `IntegrationBranch` = `"global-refactoring"` (observed 2026-10-25T04:05Z); equals the D23 ruling cited by
  `baseline.md`.
- Integration revision under test (`git rev-parse global-refactoring` and task worktree `HEAD`, both observed
  2026-10-25T04:05:47Z; `git log HEAD..global-refactoring` printed nothing, again at 04:28:53Z after all runs):
  `b3356c3c62742eebd20cfcaa3b36d6c66172c149` (`Merge arm-1-ar-11-cp-4-code-8`). No rebase was needed. The
  task worktree `HEAD` is this revision plus the commits that add this record, the registry edit and the
  acceptance manifest; those commits are documentation/manifest only (`git diff --stat b3356c3c6 HEAD` lists
  the three owned paths).
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
  does not apply: all runs here started at 04:06Z or later and none printed such an error.
- Host: `Linux 6.8.0-147-generic x86_64`, `nproc` 8, `go1.26.0 linux/amd64`; shared host, other agent sessions
  running (load below).

## G1 — A2 ratchet empty; S2 exceptions data-like with rationale

- Source: `internal/testguard/archguard/area_a2_test.go` at `b3356c3c6` (read in full, 45 lines). Excerpt
  (lines 36–44):

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
- C0: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 ./internal/testguard/archguard/`,
  2026-10-25T04:06:08Z–04:06:10Z, exit 0: `ok  github.com/liza-mas/liza/internal/testguard/archguard  0.507s`.
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
  exists (the package passes; C1 and C2 repeat it: `ok … archguard 1.101s` and `0.199s`). Result: pass.

## G3 — A2 Shim Register empty; no temporary alias or forwarder added

- Register: `20261023-195624-arm-1-ar-11.md#Shim Register` (pinned `cb79e6713`) reads "None."
- Inspection of every A2 code change, `git diff -U0 96c9ad69a HEAD -- <26 A2 package dirs> ':!*_test.go'`:
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

All five canonical commands ran at `b3356c3c6` in the task worktree (`git status --short` empty before and
after, so the generators — `make sync-embedded` inside `test`/`lint` — left tracked embedded artifacts
unchanged). Load average (1, 5, 15 min) at each start is shown; C1–C4 ran serially.

| Cmd | Command | Start (UTC) | End (UTC) | Exit | Load at start | Excerpt |
|---|---|---|---|---|---|---|
| C0 | `go test -count=1 ./internal/testguard/archguard/` | 04:06:08 | 04:06:10 | 0 | 21 (1 min) | `ok … archguard 0.507s` |
| C1 | `make test` (+`GOFLAGS=-count=1`) | 04:06:16 | 04:13:07 | 0 | 21.48 17.97 13.94 | 83 `ok`, 0 `FAIL`, 0 `(cached)`; 4 `[no test files]`; last: `ok …/scripts/ci/testshard 0.768s` |
| C2 | `make lint` (+`GOFLAGS=-count=1`) | 04:13:07 | 04:14:50 | 0 | 17.09 24.94 19.64 | `Embedded artifacts are consistent with masters`; `go vet ./...` for `GOOS=linux`, `windows`, `darwin`; archguard `ok 0.199s` |
| C3 | `make test-e2e` (+`GOFLAGS=-count=1`) | 04:14:50 | 04:16:48 | 0 | 23.28 25.46 20.43 | `ok …/internal/integration 100.989s` (`TestFullSprintSequence`, `TestTerminalDependencyRecovery`, `-race -tags e2e`) |
| C4 | `go test -race <A2 roots>` (+`GOFLAGS=-count=1`) | 04:16:48 | 04:17:47 | 0 | 6.95 18.85 18.63 | 25 `ok`, 0 `FAIL`, 0 `(cached)`, 2 `[no test files]` |

C4 selection reconciled with the delivered layer data: the 24 package roots plus `./internal/embedded/...`
and `./internal/brandrender/...` expand to 27 packages — the 26 A2 layer entries plus
`internal/brandrender/cmd/sync-embedded` — and the layer data holds the same 27. Results: git 2.985s, gitenv
1.224s, gitbash 1.012s, worktreeexclude 12.782s, subprocess 6.020s, procscan 1.027s, secretmask 1.012s,
toolresult 56.416s, toolresultacp 8.082s, sessionvalidation 2.228s, precommit 1.093s, initcheck 1.282s,
codexconfig 1.014s, bash-policy-cli 1.017s, providers 1.308s, embedded 9.366s, brandrender 6.723s,
scipsearch 6.465s, stacklit 1.235s, semble 1.087s, functionalclusters 1.196s, pairingindex 3.593s, toolchain
1.080s, updater 1.147s, termutil 1.012s; `internal/projectdetect` and `internal/brandrender/cmd/sync-embedded`
have no test files (projectdetect has none at the starting commit either).

Not run locally, by design: windows-tagged A2 tests such as `internal/procscan/kill_windows_test.go`. `make lint`
compiles them under `GOOS=windows go vet ./...`; the Windows CI of G8 executes them. Result: pass. Remote
results are G8's, not G4's.

## G5 — wall time and benchmarks against `baseline.md` (indicative; never blocks)

Protocol repeated from `baseline.md`: fresh disposable clone outside the worktrees
(`git clone --local --no-checkout <project root> /tmp/a2g5base.XXXXXX/repo`, `git checkout --detach
b3356c3c6…`; clone `HEAD` printed `b3356c3c62742eebd20cfcaa3b36d6c66172c149`). Sync-clean check: `make
sync-embedded` exit 0 and clone `git status --short` empty. Environment
`env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp GOFLAGS=-count=1`; one warm-up and one timed `make test`, neither
retried; then `timeout 300 go test -run '^$' -bench . -benchmem ./internal/<pkg>` per package. The clone's
`git status --short` stayed empty; the clone was deleted (`ls -d /tmp/a2g5base.*` matches nothing).

| Run | Start (UTC) | End (UTC) | Wall s | Exit | Load at start | Output |
|---|---|---|---|---|---|---|
| Warm-up | 2026-10-25T04:18:11Z | 2026-10-25T04:22:37Z | 266 | 0 | 5.52 15.68 17.54 | 83 `ok`, no `FAIL`, no `(cached)` |
| Timed | 2026-10-25T04:22:37Z | 2026-10-25T04:27:14Z | 277 | 0 | 15.12 15.31 17.10 | 83 `ok`, no `FAIL`, no `(cached)` |

Load after the timed run: 9.77 14.64 16.58.

**N4 comparison (+25%).** The baseline holds two numbers that are not interchangeable: the timed run of 523 s,
which was red (exit 2, two `internal/agent` deadline tests failed under load about 30) and load-confounded
(bound +25% = 653.75 s), and a separate green warm-up observation of 444 s (bound 555 s). Timed 277 s is −47.0%
vs 523 s and −37.6% vs 444 s; warm-up 266 s is −49.1% and −40.1%. Both are inside either bound. The 444 s
figure is shown only as a separate observation, never substituted for the 523 s reference. Caveats: load here
(about 5–17 on 8 cores) is lower than the baseline's (about 9–30) but not quiet, and the test set is larger now
(83 packages with `ok` vs 66), so the comparison is indicative, not like-for-like. Disposition: N4 holds; no
action.

**N3 benchmarks.** One run per package, baseline numbers from `baseline.md` (load about 30). Missing result: none
— all 18 benchmark rows of the baseline (14 db, 2 commands, 2 agent) were captured and all three packages exited
0 with `PASS` (`internal/db` 59.119s, `internal/commands` 4.906s, `internal/agent` 3.172s). Deltas below are
ns/op; B/op and allocs/op are shown as changes (0.0 where within ±0.1%).

| Benchmark | ns/op base → now | B/op | allocs/op |
|---|---|---|---|
| db `BlackboardWrite` | 12033415 → 5677267 (−53%) | 0.0% | 0.0% |
| db `BlackboardModify` | 22577288 → 6528676 (−71%) | +0.4% | +0.4% |
| db `BlackboardReadCached` | 44938 → 24172 (−46%) | 0.0% | 0.0% |
| db `BlackboardReadUncached` | 1890776 → 225713 (−88%) | −0.1% | 0.0% |
| db `MarshalStateForWriteLarge/repaired=false` | 536001675 → 130252064 (−76%) | 0.0% | 0.0% |
| db `MarshalStateForWriteLarge/repaired=true` | 1331986515 → 150660211 (−89%) | 0.0% | 0.0% |
| db `ReadCachedHit` | 4916749 → 1378280 (−72%) | 0.0% | 0.0% |
| db `StateParse` | 112041835 → 38458015 (−66%) | 0.0% | 0.0% |
| db `TerminalArchiveTransaction/inline/cold-instance` | 772451364 → 349433419 (−55%) | 0.0% | 0.0% |
| db `TerminalArchiveTransaction/inline/warm` | 628528774 → 355246667 (−43%) | 0.0% | 0.0% |
| db `TerminalArchiveTransaction/archived/cold-instance` | 320733652 → 173038090 (−46%) | 0.0% | 0.0% |
| db `TerminalArchiveTransaction/archived/warm` | 185103984 → 32488329 (−82%) | −9.7% | −18.5% |
| db `TerminalArchiveTransaction/archived-256/cold-instance` | 990744276 → 634486239 (−36%) | 0.0% | 0.0% |
| db `TerminalArchiveTransaction/archived-256/warm` | 319924638 → 121707374 (−62%) | −1.8% | −3.5% |
| commands `InspectionSnapshotDuringHeartbeat/status` | 9380378 → 10780557 (**+15%**) | **+12.4%** | **+6.7%** |
| commands `InspectionSnapshotDuringHeartbeat/get` | 4376937 → 4153097 (−5%) | **+5.2%** | **+4.8%** |
| agent `BenchmarkClaimReviewerTask` | 34512825 → 4745032 (−86%) | 0.0% | 0.0% |
| agent `BenchmarkResumeHandoff` | 5936738 → 2332885 (−61%) | −0.1% | −0.1% |

**Dispositions** (N3 is Should; none blocks):

- Sixteen of eighteen rows have ns/op at or below baseline with B/op and allocs/op within ±0.5% or lower, so there
  is no slowdown to dispose of. The improvements are recorded as measured and not claimed as caused by A2: the
  baseline ran at load about 30 and this run at about 5–17.
- `InspectionSnapshotDuringHeartbeat/status`: ns/op +15%, B/op +12.4%, allocs +6.7%; `/get`: ns/op −5%, B/op
  +5.2%, allocs +4.8%. These are the same allocation increases the A1 record disclosed (`boundary-a1.md`: status
  B/op +10.5%, allocs +5.6%; get +5.2%, +4.3%), so they were already present at the A1 boundary and moved by
  about 2 points since. A2 did not cause them: the benchmark exercises `internal/commands` and `internal/db`,
  and A2's changes to those packages are test files plus two call-site edits (`internal/commands/init.go`, 4
  insertions and 4 deletions after the A1 boundary, `git diff --stat 265a14b3a HEAD`; none of `internal/db`). The benchmark
  overlaps a concurrent heartbeat writer (heartbeat-writes 28 and 22 here vs 6 and 13 at the baseline), so work
  per iteration is not like-for-like with the baseline. Disposition, unchanged from A1: the benchmark lives in
  `commands` (A7); re-measure at the A7 boundary, where a persistent increase would be commissioned. The
  `/status` ns/op +15% is a load-confounded single run (baseline at load about 30, this run 5–17 with a
  concurrent heartbeat writer) and is likewise deferred to A7.
- `BlackboardModify` B/op +0.4%, allocs +0.4%: within run-to-run variation of a concurrent-write benchmark; no
  action.

## G6 — architectural issues resolved by A2

Reviewed the Open Issues Summary (68 open, `specs/architecture/architectural-issues.md`), the Structural Debt
section, and every registry line mentioning an A2 package (`grep` for each of the 26 package names: only
`internal/git` and `subprocess` appear, in the Git-routing, init-duplication and PAUSE/ABORT text, which concern
`ops`/`commands`/`cmd/liza` callers, not A2) against the A2 commits listed above.

- **Closed: "Split embedded.go by Artifact Family (1,530 LOC)" (`issue-embedded`).** The dated "1,530 LOC" claim
  is historical (the file was 1842 lines at the starting commit), so closure is judged on the issue's
  Direction, not the size: "Keep the package API stable while splitting implementation by artifact family:
  global corpus, Claude settings, Codex settings and permissions, hooks, project artifacts, and stale-artifact
  cleanup." Evidence at `b3356c3c6`:
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

Registry edit (this commit): `Split embedded.go …` removed from the Table of Contents entry and Structural
Debt active section and from the Open Issues Summary; added to Completed Fixes (dated 2026-10-25), Fixed
(Traceability) and Fix Details with commit references `20d654f7b`, and the Counts line updated (68 → 67 open;
34 medium).

## G7 — record completeness

This file holds the commands or source reads, the integration SHA (`b3356c3c62742eebd20cfcaa3b36d6c66172c149`,
for G1–G6 alike), UTC observations, exit codes and excerpts for G1–G6 above, and the G8 section below. Manifest:
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-12-cp-0-boundary.json` (G4 mapped to the
`Makefile` test target with `command_index` 1; every other obligation maps to its declared approved
reference). The code revision did not change between the G1–G5 runs and the submission commit: only this record,
the registry and the manifest differ.

## G8 — dispatched CI result (D52): Linux, Windows and macOS

Status: **pending (held for the maintainer)**. G1–G7 above pass at `b3356c3c62742eebd20cfcaa3b36d6c66172c149`.
Under D52 the operator may push `global-refactoring` and dispatch `.github/workflows/ci.yml`
(`workflow_dispatch`) on it; G8 evidence is that run's URL, tested SHA (must equal the revision above) and
each job's conclusion, confirmed by the maintainer, covering Linux, Windows (including the windows-tagged A2
tests such as `internal/procscan/kill_windows_test.go`) and macOS. No pull request is opened or claimed, and
this record does not claim G8 before the run's jobs complete. Red CI holds A2 closure and the run close; the
maintainer commissions the fix task. To be appended on resume: run URL, tested full SHA, each job's name and
conclusion, UTC observation, maintainer ruling. If integration moves to different code before the run, the
affected G1–G7 are refreshed at that revision and the old/new distinction is kept here.
