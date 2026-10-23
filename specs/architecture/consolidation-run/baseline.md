# Architecture Consolidation Run: Baseline Record

Measured facts of the run's starting commit. Sole writer: the Scope 4 task of the arm-1-ar-0 plan
(`specs/arch-plan/20261023-architecture-consolidation/20261023-172626-arm-1-ar-0.md`, Scope 4).
Boundary gates (G5) read this record and repeat its Protocol.

## Starting commit

`96c9ad69aee983ce2b929dd681b6ba43bf920535`

The disposable clone's `git rev-parse HEAD` after `git checkout --detach` printed exactly this SHA
(clone-HEAD output: `96c9ad69aee983ce2b929dd681b6ba43bf920535`).

## E0 resolution

- Decision: D23, "Integration branch and sync-clean start (run, 2026-10-23)", in
  `specs/goals/20261023-architecture-consolidation-decisions.md`. Human ruling 2026-10-23T16:46Z:
  the run's integration branch is `global-refactoring`; the ruling supersedes the branch name in
  E0 (`refactor/architecture-consolidation`). The E0 hold is satisfied by this ruling.
- Configured branch observed: `omni-ee -C <project root> get config --json`, field
  `IntegrationBranch` = `"global-refactoring"` (observed 2026-10-23T19:35Z).
- Result: the configured branch equals the branch D23 rules.

## Sync-clean check

Run in the clone, at the starting commit, under the Protocol environment (below):

```text
env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp GOFLAGS=-count=1 make -C <clone>/repo sync-embedded
git -C <clone>/repo status --short
```

- `make sync-embedded`: exit code 0, last output line before the make directory notice:
  `Files synced successfully`.
- `git status --short`: exit code 0, output empty (0 bytes). The synced copy leaves the tree
  unchanged (OQ-003 satisfied).

## S1 counts

Per-area offenders at the starting commit, counted from the Scope 1 ratchet data. Cited, not
copied: the entries live in `internal/testguard/archguard/s1_snapshot_test.go` (`startSnapshot`,
immutable) and in the per-area data files `internal/testguard/archguard/area_a1_test.go` to
`area_a9_test.go`, as merged at integration commit `d7cfe16548e1516340904ee0c62cd6c171201adf`
(Merge arm-1-ar-0-cp-0-code-0).

| Area | Offending files | Offending functions |
|---|---|---|
| A1 | 3 | 7 |
| A2 | 6 | 2 |
| A3 | 2 | 13 |
| A4 | 2 | 21 |
| A5 | 6 | 12 |
| A6 | 3 | 6 |
| A7 | 9 | 17 |
| A8 | 1 | 3 |
| A9 | 1 | 12 |
| Total | 33 | 93 |

Counts equal the "S1 files / funcs at start" column of the Area Partition (`arch-areas`) in every
row; the snapshot holds 33 file keys and 93 function keys (126 entries). No mismatch.

## `make test` wall time

### Protocol

1. Clone: `git clone --local --no-checkout <project root> <tmp>/repo`, then
   `git -C <tmp>/repo checkout --detach 96c9ad69aee983ce2b929dd681b6ba43bf920535`, with `<tmp>` a
   fresh directory under `/tmp` (outside `.worktrees/`). Nothing is pushed, fetched into, or
   branched in the project repository. No `make install`. The clone is deleted afterwards.
2. Environment for every measurement command:
   `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp GOFLAGS=-count=1 <command>`
   (no test result is cached).
3. After the sync-clean check: one warm-up `make -C <tmp>/repo test` (must exit 0, proves green),
   then one timed `make -C <tmp>/repo test`; wall time from UTC start/end stamps. Neither run is
   retried.
4. Measurements ran in the clone at the starting commit; nothing in a task worktree.

### Host

- `uname -srm`: `Linux 6.8.0-147-generic x86_64`; `nproc`: `8`;
  CPU `11th Gen Intel(R) Core(TM) i7-1165G7 @ 2.80GHz`.
- `go version`: `go version go1.26.0 linux/amd64`.
- Shared host: other agent sessions were running during the measurement.
- `uptime` load averages (1, 5, 15 min):
  before warm-up (2026-10-23T19:34:36Z) `8.72, 10.01, 12.50`;
  before timed run (2026-10-23T19:42:00Z) `30.24, 27.52, 20.48`;
  after timed run (2026-10-23T19:50:43Z) `30.21, 30.86, 26.06`.
- Disk: about 32 GB free on `/` at the start (the full-disk window of the earlier attempt is
  discarded; this is a re-run from scratch).

### Result

| Run | Start (UTC) | End (UTC) | Wall seconds | Exit code |
|---|---|---|---|---|
| Warm-up | 2026-10-23T19:34:36Z | 2026-10-23T19:42:00Z | 444 | 0 |
| Timed | 2026-10-23T19:42:00Z | 2026-10-23T19:50:43Z | 523 | 2 |

- Warm-up: green. 66 `ok` package lines, no `(cached)`, no `FAIL`.
- Timed: **red, exit code 2.** 65 `ok` package lines, no `(cached)`, one failing package
  `internal/agent` (326.670 s) with two failing tests, both in
  `internal/agent/inflight_submit_deadline_test.go`:
  - `TestExecuteAgentBlocksUnadmittedLateSubmitAfterHandback` (line 220):
    "the session was cancelled before the agent saw its submit's outcome"
  - `TestExecuteAgentProgressWatchdogStillBlocksDuringHandback` (line 241): same message.
  Both passed in the warm-up of the same clone. Both rely on short real-time deadlines (the
  failing runs log a 1 s execution timeout), and the timed run executed at load average about 30
  on 8 cores. Not retried, per Protocol.
- Consequence for N4 (`make test` within +25% of its starting time): the timed 523 s run is red and
  was taken under load about 3.5 times the core count, so it is not a clean baseline. The green
  warm-up (444 s) ran at load about 9 rising to about 30. Both numbers are recorded as measured;
  boundary gates (G5) get a recorded disposition for any out-of-bound result (N4 is Should and
  blocks nothing). A repeat on a quiet host would supply a cleaner baseline.

## Benchmarks

Command per package, in the clone after the timed run, under the Protocol environment:
`timeout 300 go test -run '^$' -bench . -benchmem <package>`. No retries, no added waits. The
benchmarks ran at load average about 30 (indicative only, N3). All three packages exited 0 and
printed `PASS`; the clone's `git status --short` stayed empty.

Package `internal/db` (`ok`, 163.137 s):

| Benchmark | Result |
|---|---|
| `BenchmarkBlackboardWrite` | 12033415 ns/op, 205782 B/op, 1171 allocs/op |
| `BenchmarkBlackboardModify` | 22577288 ns/op, 263198 B/op, 1928 allocs/op |
| `BenchmarkBlackboardReadCached` | 44938 ns/op, 4066 B/op, 17 allocs/op |
| `BenchmarkBlackboardReadUncached` | 1890776 ns/op, 55740 B/op, 747 allocs/op |
| `BenchmarkMarshalStateForWriteLarge/repaired=false` | 536001675 ns/op, 8.04 MB/s, 68701068 B/op, 179519 allocs/op |
| `BenchmarkMarshalStateForWriteLarge/repaired=true` | 1331986515 ns/op, 3.23 MB/s, 68811344 B/op, 179540 allocs/op |
| `BenchmarkReadCachedHit` | 4916749 ns/op, 240048 B/op, 1935 allocs/op |
| `BenchmarkStateParse` | 112041835 ns/op, 10700785 B/op, 56356 allocs/op |
| `BenchmarkTerminalArchiveTransaction/inline/cold-instance` | 772451364 ns/op, 3672204 yaml_bytes, 138455332 B/op, 1176162 allocs/op |
| `BenchmarkTerminalArchiveTransaction/inline/warm` | 628528774 ns/op, 3672204 yaml_bytes, 138455373 B/op, 1176162 allocs/op |
| `BenchmarkTerminalArchiveTransaction/archived/cold-instance` | 320733652 ns/op, 55362 yaml_bytes, 77212016 B/op, 646273 allocs/op |
| `BenchmarkTerminalArchiveTransaction/archived/warm` | 185103984 ns/op, 55362 yaml_bytes, 15812984 B/op, 67665 allocs/op |
| `BenchmarkTerminalArchiveTransaction/archived-256/cold-instance` | 990744276 ns/op, 216402 yaml_bytes, 308468540 B/op, 2576422 allocs/op |
| `BenchmarkTerminalArchiveTransaction/archived-256/warm` | 319924638 ns/op, 216402 yaml_bytes, 61202293 B/op, 249153 allocs/op |

Package `internal/commands` (`ok`, 4.577 s):

| Benchmark | Result |
|---|---|
| `BenchmarkInspectionSnapshotDuringHeartbeat/status` | 9380378 ns/op, 6 heartbeat-writes, 214095 max-heartbeat-us, 6328340 B/op, 103681 allocs/op |
| `BenchmarkInspectionSnapshotDuringHeartbeat/get` | 4376937 ns/op, 13 heartbeat-writes, 236106 max-heartbeat-us, 3356618 B/op, 33229 allocs/op |

Package `internal/agent` (`ok`, 4.125 s):

| Benchmark | Result |
|---|---|
| `BenchmarkClaimReviewerTask` | 34512825 ns/op, 920922 B/op, 16886 allocs/op |
| `BenchmarkResumeHandoff` | 5936738 ns/op, 743934 B/op, 14401 allocs/op |

All eleven benchmark functions of the Q11 set were captured; none is "not captured".

## Measurement provenance

- Clone path pattern: `/tmp/cp3base.XXXXXX/repo` (fresh `mktemp -d` directory; deleted after the
  session; `ls -d /tmp/cp3base.*` then matches nothing).
- UTC window of the final session: 2026-10-23T19:34:35Z to 2026-10-23T19:53:37Z. An earlier
  attempt overlapped a full root filesystem; its `make test` wall time was discarded on the
  operator's instruction (note of 2026-10-23T19:33Z) and every measurement here was taken from
  scratch.
- Measurements ran at the starting commit before this record was written, which happened after
  Scope 1 merged (integration commit `d7cfe16548e1516340904ee0c62cd6c171201adf`); the S1 counts
  cite that merged Scope 1 artifact rather than a measurement of this session.
- The CLI name `omni-ee` appears as invoked (historical artifact).
