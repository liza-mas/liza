# Code Plan: D53/D56 Correction — A9 G8 Windows Failures (C6 Listing Cache, PB-3 Outbox Hang)

## Source References
Source revision: "225ed0af8c7734e67e83baadebc16817afed8e02"

### Direct References
- "d53": "specs/goals/20261023-architecture-consolidation-decisions.md#D53 — Oracle portability for N2: generic fixes everywhere, process-supervision oracles Linux-only (human, 2026-10-24)" @ "225ed0af8c7734e67e83baadebc16817afed8e02"
- "d52": "specs/goals/20261023-architecture-consolidation-decisions.md#D52 — G8 boundary CI by `workflow_dispatch` on the pushed integration branch; one PR to `main` later (human, 2026-10-24)" @ "225ed0af8c7734e67e83baadebc16817afed8e02"
- "d59": "specs/goals/20261023-architecture-consolidation-decisions.md#D59 — G8 at a boundary SHA behind the branch head: dispatch on a pinned ref (operator, standing delegation, 2026-10-25)" @ "225ed0af8c7734e67e83baadebc16817afed8e02"
- "d60": "specs/goals/20261023-architecture-consolidation-decisions.md#D60 — G8 may be green across failed-job reruns of one run when the reruns cover pre-existing intermittent tests (human, 2026-10-25)" @ "225ed0af8c7734e67e83baadebc16817afed8e02"
- "d55": "specs/goals/20261023-architecture-consolidation-decisions.md#D55 — E1 revised for new code plans: no per-task `go test -race` (human, 2026-10-24)" @ "225ed0af8c7734e67e83baadebc16817afed8e02"
- "d24": "specs/goals/20261023-architecture-consolidation-decisions.md#D24 — Provenance of goldens changed by a C task (run, 2026-10-23)" @ "225ed0af8c7734e67e83baadebc16817afed8e02"
- "d56": "specs/goals/20261023-architecture-consolidation-decisions.md#D56 — D24 extended: C tasks may flip in-driver assertions and delete goldens; regen must reproduce both (run, 2026-10-24)" @ "225ed0af8c7734e67e83baadebc16817afed8e02"
- "goal-frozen": "specs/goals/20261023-architecture-consolidation.md#Frozen Surfaces" @ "225ed0af8c7734e67e83baadebc16817afed8e02"
- "goal-k1": "specs/goals/20261023-architecture-consolidation.md#Structural Constraints" @ "225ed0af8c7734e67e83baadebc16817afed8e02"
- "goal-n2": "specs/goals/20261023-architecture-consolidation.md#Non-Functional Constraints" @ "225ed0af8c7734e67e83baadebc16817afed8e02"
- "goal-tests": "specs/goals/20261023-architecture-consolidation.md#Test Policy and Sequencing" @ "225ed0af8c7734e67e83baadebc16817afed8e02"
- "goal-proof": "specs/goals/20261023-architecture-consolidation.md#Proof Environment" @ "225ed0af8c7734e67e83baadebc16817afed8e02"
- "env-safe": "GUARDRAILS.md#G1.4: Environment-safe tests and diagnostics" @ "225ed0af8c7734e67e83baadebc16817afed8e02"

### Obligation Coverage
- "W9-c6-cache" -> "d53", "goal-k1", "env-safe"
- "W9-c6-regression" -> "goal-tests", "d60", "env-safe"
- "W9-pb3-fix" -> "d53", "goal-frozen", "goal-n2"
- "W9-pb3-regression" -> "goal-tests", "d60"
- "W9-preserve" -> "goal-tests", "goal-frozen", "d53", "d24", "d56"
- "W9-windows" -> "d52", "d59", "d60", "goal-n2"
- "W9-proof" -> "goal-proof", "env-safe", "d55"

## Correction Authority and Boundary

Authority: operator human_note `2026-10-27T09:57:05.704405108Z` (for=all): A9 G8 is red in run
40480433464 (`ci.yml`, `workflow_dispatch`, ref `g8/a9-225ed0a`, tested SHA
`225ed0af8c7734e67e83baadebc16817afed8e02`); both failures are in code changed on this branch, so `d60`
forbids rerunning them away; it commissions ONE bounded Windows-portability correction (a)–(c),
depended on by `arm-1-ar-25-cp-0`'s expansion. `d52`: red CI holds the area and the maintainer
commissions the fix. `d53`: white-box suites keep running on all three platforms, and a cross-platform
difference not caused by a Linux process facility is fixed as a defect. Both tests are white-box
(`internal/testguard/archguard`, `internal/agent`), so no build constraint, skip or Linux-only scope is
available; neither cause is a Linux process facility.

This plan allocates one coding task. It touches no oracle driver, fixture, golden or `PROVENANCE`
(`d24`/`d56` provenance untouched), no frozen surface on Linux or macOS, and leaves the
`arm-1-ar-25-cp-0` plan, its state and its final-plan hold to their owners.

## Root Cause Analysis

### a. Evidence

Runtime (observed this session from run 40480433464, jobs and artifacts read with `gh`, RP-1–RP-3):
- Jobs not green: `Test (windows, rest-2)` 122109075644, `Test (windows, rest-1)` 122109075699 and the
  `Test (windows)` aggregate; everything else green or skipped (RP-1).
- rest-2 `test.json`: `TestC6HostGoflagsDoNotReachTheListing` fails in 0.16 s at
  `c6_mutation_check_test.go:311` ("withheld GOFLAGS still reached go list: go list (GOOS=linux) exit
  status 1: build …"; full text in the operator note: "build cache is required, but could not be located:
  GOCACHE is not defined and %LocalAppData% is not defined"). Every other `TestC6*` passes on Windows,
  including `TestC6MutationGuard` (84.7 s, real host environment) and
  `TestC6ListingFailureNeverEchoesInheritedEnvironmentValues` (RP-2).
- rest-1 `test.json`: `panic: test timed out after 30m0s` with only
  `TestA9SupervisorPB3ReplayPrecedesRegistration (29m41s)` running. Goroutine dump: the test goroutine
  is in `RunSupervisor` (`supervisor.go:117`, the iterate loop) → `supervisorLifetime.iterate`
  (`supervisor_turn.go:55`) → `awaitWork` (`:127`) → `doerStrategy.WaitForWork`
  (`strategy_doer.go:71`) → `waitForWorkEventDriven` (`waitforwork.go:102`) → `watchForWork` select
  (`waitforwork.go:128`); the supervisor heartbeat (`heartbeat.go:128`), the state watcher and the
  fsnotify reader are alive. Toolchain `C:/hostedtoolcache/windows/go/1.25.5` (RP-3).

Code (read at `225ed0af8`):
- `internal/testguard/archguard/c6_mutation_check_test.go:245-254` `c6SyntheticEnviron` is `HOME`,
  `PATH` (host), `TMPDIR=/tmp` and three synthetic markers; `:304-313` the failing test passes
  `c6SyntheticEnviron()[:3:3]` plus a hostile `GOFLAGS` to `c6List`.
- `c6_mutation_load_test.go:95-99` allowlist (`GOCACHE`, `LOCALAPPDATA`, `TMP`, `TEMP` included);
  `:107-127` `c6ChildEnv` keeps only allowlisted entries of the given environ and adds
  `GOOS/GOARCH/CGO_ENABLED`; `:134-148` `c6List` runs `go list -e -export -deps …` with exactly that
  environment and masks retained values in its failure text; `:200` the production guard path
  (`c6Load`) passes `os.Environ()`.
- `internal/agent/a9_supervisor_pb_test.go:214-243` (PB-3): writes a regular file where the outbox
  directory `.liza/pending-verdicts` belongs, runs `RunSupervisor` with `context.Background()` and
  asserts the error contains "replay reached verdicts before registration" and the state bytes are
  unchanged.
- `internal/agent/supervisor.go:97-117`: `newSupervisorLifetime` → `selectStrategy` → `register` →
  `start` → iterate loop. `supervisor_lifetime.go:128-129`: `selectStrategy` returns
  "replay reached verdicts before registration: %w" only if `ops.ReplayPendingVerdicts` errors.
- `internal/ops/pending_verdict.go:219-226`: `ReplayPendingVerdicts` lists the outbox with
  `os.ReadDir`; `ErrNotExist` → `nil`; any other error → "read private verdict outbox: %w"; an empty
  listing → `nil`. Same listing at the starting commit `96c9ad69a` (`pending_verdict.go:198`), so the
  divergence predates the run; the PB-3 test that exposes it is new on this branch (`d1a0cdc85`,
  `75eb56dde`).
- Go 1.25.5 (CI toolchain; `go.mod` `go 1.25.5`), read from `golang/go@go1.25.5`:
  `src/os/file_windows.go:127-129` `openDirNolog` = `openFileNolog(name, O_RDONLY, 0)` — no
  directory check, and `syscall.Open` adds `FILE_FLAG_BACKUP_SEMANTICS`, so a regular file opens;
  `src/os/dir_windows.go:122-153` (`File.readdir`) then calls `GetFileInformationByHandleEx` with a
  directory class and returns no error when that call yields `ERROR_FILE_NOT_FOUND` (the
  empty-directory case, `:134-146`). On Linux/macOS `openDir` opens with `O_DIRECTORY` and fails with
  `open <dir>: not a directory` (`ENOTDIR`, RP-6).

### b. Mechanism

1. **C6.** Go locates its build cache from `GOCACHE`, else from the OS user cache directory
   (`%LocalAppData%` on Windows; `$XDG_CACHE_HOME` or `$HOME/.cache` on Linux). The test's constructed
   environ holds neither `GOCACHE` nor `LOCALAPPDATA`; on Linux the inherited `HOME` hides the gap, on
   Windows nothing does, so `go list -export` exits 1 before it reaches the `GOFLAGS` property under
   test. The guard's own path (`c6Load`, host environ with the CI `GOCACHE`) is unaffected (green on
   Windows). Defect of the test's environment construction, not of `c6ChildEnv`.
2. **PB-3.** On Windows `os.ReadDir` of the regular-file outbox reports no error that
   `ReplayPendingVerdicts` treats as a failure (an empty listing or `ErrNotExist`), so replay returns
   `nil`, the supervisor registers and waits for work under `context.Background()` with no claimable
   task: an unbounded wait, killed by the 30 m package timeout. The stack proves the `nil` outcome; which
   Win32 code Windows returned is not observed and the fix does not depend on it. Not a timing
   problem: no deadline makes the refusal appear. Production defect: on Windows a corrupt
   (non-directory) outbox is silently treated as empty, contradicting PB-3 ("a replay that cannot proceed
   ends the run with the registration never written"), which Linux and macOS honour.

### c. Reproduction

- C6, Linux analog (observed, RP-4): at `225ed0af8`, `env -i PATH="$PATH" TMPDIR=/tmp
  GOCACHE="$HOME/.cache/go-build" go test -count=1 -run '^TestC6HostGoflagsDoNotReachTheListing$'
  ./internal/testguard/archguard` fails with "build cache is required, but could not be located: GOCACHE
  is not defined and neither $XDG_CACHE_HOME nor $HOME are defined" — the Linux form of the Windows
  signature; with `HOME` it passes. A probe child (`env -i` with only `PATH`, a fresh owned `GOCACHE`,
  owned `TMP`/`TEMP`, `TMPDIR`, no `HOME`) lists the same two-file module, exit 0, 32 packages, 4.0 s
  cold (RP-5).
- PB-3: not reproducible on Linux (Linux `ReadDir` refuses, RP-6); the Windows reproduction is the
  captured run (RP-3).

### d. Verification scope

Checked: (a) reads; RP-1–RP-8. Not checked: native Windows execution of either fix (no Windows host;
route in Task 1 `W9-windows`); the Win32 error code behind the Windows listing (not needed, b.2).

### e. Sufficiency

C6: an explicit, test-owned `GOCACHE` plus Windows temp names removes the only missing input; RP-4/RP-5
show the child needs nothing host-derived beyond the pre-existing `PATH`. PB-3: refusing a non-directory
outbox by its file type, before listing, gives every OS the Linux outcome; replay then errors, so
registration never runs and the test returns in milliseconds. Residual, named: a regression of the PB-3
guard is visible only on Windows (Linux already refuses through `ReadDir`); the Task 1 ops test makes
that a fast Windows failure instead of a 30 m hang.

### f. Adjacent findings (recorded, not planned)

- `RunSupervisor` in PB-3 runs under `context.Background()`, so any future pre-registration regression
  hangs instead of failing. Bounding it is a test-hardening change outside this correction (scope OUT:
  deadline-only fixes); not planned.
- `savePendingVerdict` (`pending_verdict.go:149-150`) uses `MkdirAll`, which fails on a non-directory
  on every OS; no change.

## Corrected Mechanism

- **F1 C6 owned cache.** `TestC6HostGoflagsDoNotReachTheListing` passes `c6List` an environ explicitly
  built from `c6SyntheticEnviron()[:3:3]`, the hostile `GOFLAGS` marker, and three test-owned entries:
  `GOCACHE` = a `t.TempDir()` directory, `TMP` and `TEMP` = another `t.TempDir()` directory. No value is
  read from the host for them (no `os.Getenv`/`os.Environ` of `GOCACHE`, `LOCALAPPDATA`,
  `XDG_CACHE_HOME`, `TMP`, `TEMP`, `USERPROFILE`). The assertion (`err == nil`, message unchanged)
  stays. `c6EnvNames`, `c6ChildEnv`, `c6List`, `c6Sanitize`, `c6Load` and the other `TestC6*` tests
  are unchanged; the helper building the three entries may live in `c6_mutation_check_test.go`.
- **F2 PB-3 outbox type.** In `ReplayPendingVerdicts`, an outbox path that exists and is not a directory
  is refused before it is listed, on every OS, with the error the Linux/macOS listing already returns:
  `read private verdict outbox: open <dir>: not a directory`, where `errors.Is(err, syscall.ENOTDIR)`
  holds. Every other outcome is unchanged on every OS: absent outbox (including a dangling path) →
  `nil`; a stat failure other than the type check falls through to the existing listing and its error;
  listing errors, entry order, per-entry handling, retries and timing. No `runtime.GOOS` branch, no new
  exported identifier, no seam, no edit to `supervisor*.go` or the PB-3 test. Only
  `internal/ops/pending_verdict.go` changes in production; any new message literal is classified for K7
  as error-text data or named in that file.

Preserved (`W9-preserve`): `a9_supervisor_pb_test.go` byte-identical; no existing assertion, expected
string, fixture, golden, oracle driver or `PROVENANCE` line changes; no build constraint, skip, `GOOS`
branch or timeout change; no new `time.Sleep(` in any `*_test.go` (`TestSleepUsageBudget` stays at its
current count); A5 replay oracle (`internal/oracle/a5/supplement`) green.

## Placement

Rule: operator ruling `2026-10-27T09:57:05Z` ("depended on by arm-1-ar-25-cp-0's expansion") under
`d52` (red G8 holds the area; the maintainer commissions the fix).

- **Predecessors** (every writer of the owned files, all merged into `225ed0af8`, RP-7): C6 guard and
  its test `4bd986841`, `fd09e4b3f`, `2f5944f09`; outbox `46dd2d937`, `e35f003c4`, `c551553f5`. No
  started writer: the only active tasks are `arm-1-ar-25-cp-0` (MERGED plan, expansion held) and this
  planning task.
- **Successors:** `arm-1-ar-25-cp-0`'s expansion, through its separately reviewed dependency-only
  amendment (scope), then the operator's G8 re-dispatch at the new integration SHA. This output declares
  no edge toward them and does not depend on them.
- **Slot:** immediately on current integration `225ed0af8`.

## Dependencies and Shared-File Ownership

| Output | Waits on | Earliest sufficient artifact | Priority |
|---|---|---|---|
| 0 (Task 1) | none: `inherit_inputs` `{"mode": "none"}` — it needs no output of `d53-oracle-portability-followup-plan` or `arm-1-ar-0-cp-2-d56` (all their children MERGED) | the merged C6 guard and PB-3 test already in `225ed0af8` | Must |

| File or definition | Writers | Ordering |
|---|---|---|
| `internal/testguard/archguard/c6_mutation_check_test.go` | Task 1 only | Sole writer |
| `internal/ops/pending_verdict.go`, `internal/ops/pending_verdict_test.go` | Task 1 only | Sole writer |
| acceptance manifest | Task 1 only | Sole writer |

## Task 1: Give the C6 listing child a test-owned Go cache and refuse a non-directory verdict outbox on every OS

Priority Must. Single intent: both A9 G8 Windows signatures of run 40480433464 stop, by F1 and F2, with
every existing assertion unchanged.

**Owned files:** `internal/testguard/archguard/c6_mutation_check_test.go` (F1, T1–T2),
`internal/ops/pending_verdict.go` (F2), `internal/ops/pending_verdict_test.go` (T3),
`specs/plans/20261023-architecture-consolidation/acceptance/d53-d56-a9-windows-portability.json`.

**Tests** (TDD; record each red run in the manifest: command, pre-edit SHA, failing excerpt).
- **T1 C6 red/green.** Command 1 (no `HOME`) at the pre-edit SHA: red with the RP-4 cache signature;
  green after F1. Command 0 green after F1.
- **T2 C6 construction (names and booleans only, G1.4).** For the F1 environ, `c6ChildEnv(…, "windows")`
  keeps `GOCACHE`, `TMP` and `TEMP` whose values are test-owned directories, and carries no `GOFLAGS`,
  `LOCALAPPDATA` or `XDG_CACHE_HOME`; a failure names the variable, never a value.
- **T3 ops refusal** (test name starts with `TestReplayPendingVerdicts`, so command 2 selects it). A regular file at the outbox path → `ReplayPendingVerdicts` returns an error with
  `errors.Is(err, syscall.ENOTDIR)` and exactly `read private verdict outbox: open <dir>: not a
  directory`, without waiting, and leaves the file intact; an absent outbox → `nil` (add only if no
  existing test covers it). On Linux T3 passes before and after F2 (the pre-edit run is recorded as the
  text baseline); on Windows it fails before F2.
- **T4 unchanged.** PB-3 (command 2) and every existing `TestPendingVerdict*` pass unchanged; A5 replay
  oracle (command 3); Windows build of the three packages (command 4, compile-level only); sleep budget
  (command 5).

**Out:** `supervisor*.go`, `waitforwork.go`, the PB-3 test, `c6_mutation_load_test.go`, any
`internal/oracle/**` file, Makefile, `.github/**`, ADRs, `arm-1-ar-25-cp-0` artifacts or state, live
state, push, dispatch, PR.

Mappings: `W9-c6-cache` → T2 and T1 (commands 0–1); `W9-c6-regression` → T1 (command 1);
`W9-pb3-fix` → T3 (command 2); `W9-pb3-regression` → T3 and PB-3 (command 2); `W9-preserve`,
`W9-windows`, `W9-proof` → approved proofs.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/d53-d56-a9-windows-portability.json",
  "obligations": ["W9-c6-cache", "W9-c6-regression", "W9-pb3-fix", "W9-pb3-regression", "W9-preserve", "W9-windows", "W9-proof"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/testguard/archguard",
    "env -i PATH=\"$PATH\" TMPDIR=/tmp GOCACHE=\"$HOME/.cache/go-build\" go test -count=1 -run '^TestC6HostGoflagsDoNotReachTheListing$' ./internal/testguard/archguard",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -run '^(TestA9SupervisorPB3ReplayPrecedesRegistration|TestPendingVerdict.*|TestReplayPendingVerdicts.*)$' ./internal/agent ./internal/ops",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/oracle/a5/supplement",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp GOOS=windows go vet ./internal/ops ./internal/agent ./internal/testguard/archguard",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -run '^TestSleepUsageBudget$' ./internal/testguard",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-e2e"
  ],
  "timeout_seconds": 2400,
  "deferred": [7, 8],
  "approved_proofs": [
    {"obligation_id": "W9-preserve", "reference_id": "goal-tests", "rationale": "Review the diff against 225ed0af8: only F1 in c6_mutation_check_test.go, F2 in pending_verdict.go, the new tests and the manifest; a9_supervisor_pb_test.go byte-identical; no existing assertion, expected string, fixture, golden, oracle driver or PROVENANCE line edited (the new T2/T3 tests are additions); no build constraint, skip, GOOS branch, timeout or time.Sleep added; Linux/macOS error text of the outbox listing unchanged."},
    {"obligation_id": "W9-windows", "reference_id": "d52", "rationale": "Agents have no Windows host and E0 forbids pushes, so native Windows proof cannot run in the task. Existing route: after merge the operator dispatches ci.yml per D52/D59 at the new integration SHA. Required evidence, recorded by the boundary owner, not the child: run URL, headSha equal to that SHA, Linux and Windows job conclusions, and pass events in the Windows shard test.json for TestC6HostGoflagsDoNotReachTheListing, TestA9SupervisorPB3ReplayPrecedesRegistration and the T3 ops test. Neither test may be rerun away (D60). Review checks the manifest claims no Windows pass; command 4 is compile-level only."},
    {"obligation_id": "W9-proof", "reference_id": "goal-proof", "rationale": "Review that every command ran under env -i in disposable state, recorded with the tested SHA and outcome (red runs at the pre-edit SHA), that no test message prints an environment value (G1.4), and that no race command is listed (D55)."}
  ]
}
```

## Reality Probes

At `225ed0af8c7734e67e83baadebc16817afed8e02`, 2026-10-27T10:00Z–10:25Z; Go commands under `env -i`
as shown; disposable directories under `/tmp` removed or left outside the repo.

| ID | Assertion / command | Observation |
|---|---|---|
| RP-1 | `gh run view 40480433464 --json jobs` | non-green: `Test (windows, rest-2)` 122109075644, `Test (windows, rest-1)` 122109075699, `Test (windows)` failure; `Release verified tag` skipped |
| RP-2 | `gh run download … -n windows-test-rest-2`; `test.json` `TestC6*` events | fail only `TestC6HostGoflagsDoNotReachTheListing` (0.16 s, `:311`, "go list (GOOS=linux) exit status 1: build …"); `TestC6MutationGuard` pass 84.69 s; the other 13 `TestC6*` pass |
| RP-3 | `gh run download … -n windows-test-rest-1`; PB-3 output in `test.json` | `panic: test timed out after 30m0s`; stack `RunSupervisor` `supervisor.go:117` → `iterate` → `awaitWork` → `WaitForWork` → `watchForWork` `waitforwork.go:128`; `go/1.25.5` |
| RP-4 | command 1 at HEAD; same with `HOME="$HOME"` | FAIL `:311` "… GOCACHE is not defined and neither $XDG_CACHE_HOME nor $HOME are defined"; with HOME `ok` 0.094 s |
| RP-5 | `env -i PATH=… GOCACHE=<fresh> TMP=… TEMP=… TMPDIR=/tmp GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go list -e -export -deps -buildvcs=false -json ./cmd/... ./internal/...` on the test's two-file module | exit 0, 32 packages, 4.0 s cold; without `GOCACHE`/`HOME`: the RP-4 cache error |
| RP-6 | Go 1.26.0 program: `os.ReadDir` of a regular file named `pending-verdicts` (Linux) | error; `errors.Is ENOTDIR` true, `ErrNotExist` false; `op=open err=not a directory`. `golang/go@go1.25.5` `src/os/dir_windows.go` identical to local 1.26.0; `file_windows.go:127-129` `openDirNolog` = `openFileNolog(name, O_RDONLY, 0)` |
| RP-7 | `git log` of owned files; `omni-ee get tasks --active --summary --json` | writers listed in Placement, all ancestors of HEAD; active: `arm-1-ar-25-cp-0` MERGED and this task |
| RP-8 | commands 0, 2–6 at HEAD | 0: `ok` 48.7 s; 2: `ok` agent 0.11 s, ops 0.78 s (39 s with build); 3: `ok` 17.8 s; 4: exit 0, 23.8 s; 5: `ok` 0.13 s; 6: exit 0, 70.8 s; worktree clean after. 7–8 deferred, not run (`make test` ≈ 8–10 min per `d54` evidence). F1/F2/T2/T3 are PROSPECTIVE (Task 1) |
| RP-9 | prerequisites of commands 6–8: `Makefile:283` (`lint-nosync` runs `gofmt`, `find`), `:261` (`check-testhelpers` runs `grep`), `:202` (`test-e2e-nosync` runs `go test -race`, needing a C compiler); `sh -c 'command -v gofmt'`; `cc --version` | `/usr/local/go/bin/gofmt`; `cc (Ubuntu 13.3.0…)`; declared in `validation_prerequisites` (iteration 2) |

**Budget.** Undeferred commands measured ≈ 4 min (RP-4, RP-8) plus F1's cold child cache ≈ 5 s; with the
deferred `make test`/`make test-e2e` counted for a full local run ≈ 20 min. 2400 s keeps headroom for
load.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | C6 listing child gets an explicit, test-owned, Windows-sufficient Go cache location | operator note (a); task done_when (a) | Task 1 (F1; T1, T2) | Covered |
| 2 | Hostile-GOFLAGS isolation preserved; no host cache/environment value inherited; no value displayed | done_when (a); `env-safe`; `goal-k1` | Task 1 (F1; T2; `W9-proof`) | Covered |
| 3 | Source-grounded RCA of the PB-3 Windows hang | operator note (b); done_when (b) | RCA a–c (RP-3, RP-6) | Covered |
| 4 | PB-3 hang fixed; replay-before-registration assertions unchanged in strength | done_when (b); `d53` | Task 1 (F2; T3, T4) | Covered |
| 5 | No skip, scoping, suppression, D60 rerun or timeout-only acceptance | done_when (b); `d53`; `d60` | Task 1 (F1/F2 bounds; `W9-preserve`) | Covered |
| 6 | Meaningful regression proof | done_when (c); `goal-tests` | Task 1 (T1 red/green, T3) | Covered |
| 7 | Both named tests executed on Linux and on Windows with commands, SHA and results; Windows via the existing route, no fabricated pass | done_when (c); `d52`; `d59` | Task 1 (commands 0–2; `W9-windows`) | Covered |
| 8 | Sanitized env proof; make lint/test/test-e2e under deferred policy; no race (D55) | done_when (c); `goal-proof`; `d55` | Task 1 (Acceptance Contract) | Covered |
| 9 | Oracle fixtures/goldens/asserted semantics and D24/D56 provenance preserved | done_when; `d24`; `d56`; `goal-tests` | Task 1 (Out; `W9-preserve`) | Covered |
| 10 | Sleep budget preserved | done_when | Task 1 (command 5; `W9-preserve`) | Covered |
| 11 | Production edit bounded; Linux/macOS behaviour and frozen surfaces unchanged | scope; `goal-frozen` | Task 1 (F2) | Covered |
| 12 | Exactly one coding output; lands before `arm-1-ar-25-cp-0` expansion; no dependency on it | done_when; scope | Placement; Output | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: no new user-visible behaviour on Linux/macOS; Windows refusal of a corrupt outbox matches the existing PB-3 behaviour; `make test-e2e` runs deferred for regression | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | Task 1 (acceptance manifest); this plan's RCA. N/A for user docs: `rg -l "pending-verdicts|verdict outbox" docs specs README.md` finds only `specs/architecture/supervision-model.md` §Reached Verdict Recovery ("replay before supervisor registration"), which F2 makes true on Windows and does not alter | Covered |

## Output

One entry: output[0] = Task 1, no `depends_on`, `task_depends_on` or provider wait (Dependencies
table). Its validation and prerequisites equal the Acceptance Contract. `desc`, `done_when` and `scope`
are short projections of Task 1; this plan stays authoritative.
