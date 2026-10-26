# Architecture Consolidation Run: Area Boundary Record A7 (Presentation, with C4)

Evidence of the A7 area-boundary checkpoint (`Area-Boundary Checkpoint Contract`, checks G1–G8) with the C4
check (`B-c4`), from the arm-1-ar-19 plan, Scope 1, task `arm-1-ar-19-cp-0-code-0`. One record per boundary; later
units never edit it. Beyond this record the unit changes `architectural-issues.md` (the three closures of G6) and
the acceptance manifest. It changes no production, test, guard-data, oracle, baseline, Makefile or ADR file.
`tdd_not_required`: evidence-only unit that moves no code.

## Observed revision

- Configured integration branch: `global-refactoring` (D23; `baseline.md`). `omni-ee get config --json` reports
  `IntegrationBranch` = `"global-refactoring"`. `git rev-parse global-refactoring` and `git rev-parse HEAD` of the
  task worktree both print the revision below; `git log HEAD..global-refactoring` printed nothing at the start
  (2026-10-26T15:34Z) and `git rev-parse global-refactoring` still printed R1 at the end of the measurements
  (2026-10-26T15:58Z).
- **Checked revision (R1):** `4cd8d7d7811ea85b5de90484256ab162301f7169`
  (`docs(c4): record the pre-C4 red run and the branding scan for AC-root, AC-env-timeout, AC-timeout-answer,
  AC-branding`, the recorded merge commit of `arm-1-ar-18-cp-0-code-0`). Every observation below was made at R1.
  No other integration revision is recorded.
- Observation window (UTC), task branch `task/arm-1-ar-19-cp-0-code-0`: 2026-10-26T15:34Z to 15:58Z. The worktree
  was clean before and after the suites (`git status --short` empty before the registry edit of G6).
- Starting commit of the run (for A7 diffs): `96c9ad69aee983ce2b929dd681b6ba43bf920535`.
- A7 and C4 coding units, and the A8 oracle, merged before this unit (`omni-ee get <id> --json`, status `MERGED`,
  recorded merge commit) — B-sequence:

| Task | Concern | Merge commit |
|---|---|---|
| `arm-1-ar-17-cp-0-code-0` | A6 boundary with C5 | `40bd4dd8f30887cf5fe07b7a5a04309c06da05dc` |
| `arm-1-ar-17-cp-0-code-1` | K-ERR: envelope classification, `jsonout` drops `ops` | `c6dcfdf8c01f6fd5790810f233045c89958560dc` |
| `arm-1-ar-17-cp-0-code-2` | K-CLI: `cmd/liza` command families | `0150c27a023a3642faff8a2d4a8ce111da635ff7` |
| `arm-1-ar-17-cp-0-code-3` | launch resolution, build, execution, pairing seeding | `7c9291a912c7c95d308cb6bff7cababf93d44af7` |
| `arm-1-ar-17-cp-0-code-4` | K-INIT: MAS init phases | `e11874725b7221a36733ef863557a7aeef6fe85f` |
| `arm-1-ar-17-cp-0-code-5` | K-INIT: pairing init, contract placement, setup | `2c289761fc7ed8da922b9736bd31119d740ded7e` |
| `arm-1-ar-17-cp-0-code-6` | K-INS: inspection | `bd42d1b348598bbddf512341d6dce8507c12f59b` |
| `arm-1-ar-17-cp-0-code-7` | K-POOL: agent-pool repair | `e8da46d69b733e8c5fae8771f53a3b83783317ce` |
| `arm-1-ar-17-cp-0-code-8` | K-WATCH: watch and alert categories | `6e24bb215c5a8ad0b0f7e557a5b709f2d446bc49` |
| `arm-1-ar-17-cp-0-code-9` | K-TUI | `f71ac787f938db5417c24a2b509af0edd16b75b9` |
| `arm-1-ar-17-cp-0-code-10` | K7 inventory | `5b422e0678fa60607852cc29b5c01cf1efacf424` |
| `arm-1-ar-17-cp-0-code-11` | A7 closure evidence and ADR 0226 | `c1d8629bfc7dd821767a98fbdac5f6443ee61db5` |
| `arm-1-ar-18-cp-0-code-0` | C4 (production commit `009d4835a6ab4f0b428a3e97aba13b63297ba9ba`) | `4cd8d7d7811ea85b5de90484256ab162301f7169` |
| `arm-1-ar-8-cp-0-code-0` | A8 prompt oracle (30 goldens) | `59aa84316354b6201b4435746a3d145afea6bd8d` |

- Environment for every measurement: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` (GUARDRAILS G1.4), plus
  `GOFLAGS=-count=1` for G5 (the Protocol). No remote push, provider CLI, installation or update ran; output below
  is sanitized (no environment values). Host: `Linux 6.8.0-147-generic x86_64`, `nproc` 8, `go1.26.0 linux/amd64`,
  `git version 2.43.0`. Shared host with other agent sessions running.
- Two launch errors are disclosed, neither a test result: the first G4 batch (15:36:55Z) and the first G5 script
  (15:48:00Z) passed a multi-word command to `env` as one word and exited 127 in the same second for every command,
  so no test or build ran. Both were relaunched unchanged in intent (G4 at 15:37:11Z, G5 at 15:48:31Z) and the
  figures below are those relaunched runs, each executed once.
- Scratch tooling (the `go/parser` size measure, the A/B benchmark script) lives under `/tmp/a7tools/`, outside the
  repository, with the starting tree exported by `git archive 96c9ad69a` to `/tmp/a7start/` (deleted). Nothing of
  it is committed. The A7 file set is derived mechanically: `git ls-files` over `cmd/liza`, `internal/commands`,
  `internal/tui`, `internal/interactive` and `internal/jsonout`, non-test `.go` files: 179 at R1; the same listing
  at `96c9ad69a` plus `internal/ops/init_project.go` (the area partition's init capability) gives 127.

## G1 — A7 ratchet empty; S2 exceptions data-like with rationale

Source: `internal/testguard/archguard/area_a7_test.go` read at R1:

| Field | Value |
|---|---|
| `Ratchet` | `[]RatchetEntry{}` (empty) |
| `S2` | `[]S2Entry{}` (empty): no S2 entry exists, so no category or human ruling is to be checked |
| `Allowlist` | `[]AllowEntry{}` (empty) |
| `Layers` | five entries, all `layerPresentation`: `cmd/liza`, `internal/commands`, `internal/interactive`, `internal/jsonout`, `internal/tui` |

The start state was 26 keys (9 files, 17 functions; `baseline.md`, `a7-size`), one of them `internal/ops/init_project.go`
`InitProject` (229 lines), which C4 deleted. All are gone. This is recorded apart from the C0 result because the
guard tolerates debt: the delivered closure assertion `TestAreaA7RatchetAllowlistAndS2AreEmpty` (C4's rewrite of
`TestAreaA7RatchetHoldsOnlyInitProject`, as the plan's prospective name allowed) fails on any remaining ratchet key,
allowlist entry or S2 entry. `go test -count=1 -v -run 'TestAreaA7RatchetAllowlistAndS2AreEmpty|TestS1SizeGuard'
./internal/testguard/archguard/` printed `--- PASS: TestAreaA7RatchetAllowlistAndS2AreEmpty` and `--- PASS:
TestS1SizeGuard`; the whole package passed (C0, below). The assertion is present at R1, so it is no longer
prospective.

Independent measurement with `go/parser` (declaration line to closing brace of every `FuncDecl` with a body;
physical lines), over the A7 production files at R1 against the area at `96c9ad69a`:

| | Files | Lines | Files > 800 | Functions > 100 | Longest files | Longest functions |
|---|---|---|---|---|---|---|
| `96c9ad69a` | 127 | 27,907 | 9 | 17 | `watch.go` 2,084, `cmd_task.go` 2,005, `init.go` 1,684, `repair_agent_pool.go` 1,252, `cmd_launch.go` 1,159 | `InitCommandWithConfig` 471, `FindRoleCapacity` 234, `InitProject` 229, `renderTaskPanel` 208, `cmd_task.go` `init` 199 |
| R1 | 179 | 29,918 | 0 | 0 | `cmd_init.go` 741, `cmd_review.go` 705, `setup.go` 663, `cmd_agent.go` 650, `cmd_task_blocked.go` 632 | `FindMissingRolesWithClaimableWork` 86, `formatTaskValue` 84, `NewStyles` 83, `RunInitWizard` 83, `cmd_agent.go` `init` 83 |

Limits are 800 and 100. The start counts equal the area partition (27,907 lines, 9 files, 17 functions). G1 holds:
nothing remains as an S1 offender, so no S2 conversion was needed and no S2 category or human ruling is to be
checked.

## G2 — A7 allowlist holds only rationale-carrying entries; the A7-owned crossing is resolved

`Allowlist` is empty at R1 (table above), so no entry lacks a rationale and no stale entry exists. `crossings`
assigns A7 one SI-5 crossing, `jsonout → ops`: `go list -f '{{join .Imports "\n"}}' ./internal/jsonout` lists, among
module packages, only `internal/errors`, `internal/filelock` and `internal/models`; no `ops` import remains (a
`git grep` of `internal/jsonout` non-test files for an `internal/ops` import prints nothing). The other three
crossings are owned elsewhere: `prompts → ops` by A8 (Scope 19), `commands → agent` and `tui → agent` by A9
(Scope 20), `agent → db` by C6 (Scope 21). A7 added no production package: the five A7 layer entries are the five
packages of the area partition. The K4 import-direction check ran inside `make lint` (`lint-archguard`, `ok …
0.229s`). G2 holds.

## G3 — A7 Shim Register empty; no alias or forwarder added

The register is empty (arm-1-ar-17 plan, `Shim Register`; C4 plans no shim). Input: the A7 ADR's audit
(`specs/architecture/ADR/0226-a7-presentation-concerns-and-classification-contract.md`, "Shim and exported-API
audit", `a7-plan-closure`), verified independently:

| Check | `96c9ad69a` | R1 |
|---|---|---|
| Type aliases (`git grep -n -E '^type [A-Za-z0-9_]+ += '` over `cmd/liza`, `internal/commands`, `internal/tui`, `internal/interactive`, `internal/jsonout`, `internal/errors`, `internal/alerts`; tests excluded) | 4 | 4, the same four |
| Added `type X = Y` lines in `git diff 96c9ad69a HEAD` over `cmd` and `internal` (tests, oracle and testhelpers excluded) | n/a | 0 |
| Added lines in the five A7 packages naming `shim`, `forwarder`, `deprecated`, `temporary` or `compat` | n/a | 1: the comment "Enter confirms with the typed value, for backward compatibility" in the wizard; a behaviour note, not an alias |
| Exported functions added in the A7 packages, `errors` and `alerts` (diff lines `+func [A-Z]…`) | n/a | 20, of which 17 exist at `96c9ad69a` under the same name (moved between files) and three are new vocabulary helpers: `Category.String`, `InspectFormatNames`, `StatusFormatNames` |
| Added alias-shaped variable lines (`^\+var \w+ = [\w.]+$`) in A7 packages | n/a | 1: `var inspectCommand = commands.InspectCommand` at `cmd/liza/cmd_system_inspect.go:21` |

- The four aliases are the baseline ones ADR 0226 names: `AwaitResubmissionOptions` (`await_resubmission.go:36`) and
  `AwaitVerdictOptions` (`await_verdict.go:42`), aliases of `ops` types, and `AlertLevel` and `Alert` (`watch.go:74,81`
  at the start, `watch.go:58,65` now), aliases of `alerts` types, all in `internal/commands`. They exist at the starting
  commit, no A7 step introduced them, and none is scheduled for removal by A7; they are not shims of the Shim Register.
- `inspectCommand` exists at the starting commit at `cmd/liza/cmd_system.go:500` with the same name and value; it is the
  package-variable test seam of `json_wiring_test.go` (assigned at lines 2531 and 2534 at both revisions), moved with
  its command family by `d7321e525`. Not new, no removal owner.
- C4 (`009d4835a`): the production diff adds no alias, forwarder, `var`/`type` alias line or shim marker (`git show
  009d4835a -U0` over non-test files: no `^\+type … = ` and no `^\+var … = ident` line). It deletes
  `internal/ops/init_project.go` and changes `init_integration_branch.go` and `init_mas_inputs.go`.
- Exported-surface changes and caller conformance are those ADR 0226 lists (vocabulary types, constants and
  accessors; `jsonout` unchanged). They were not re-derived here beyond the 20-function check above.

G3 holds: zero shims added.

## C4 — `ops.InitProject` deleted; init Git hardened and proven (B-c4)

- **Merged:** `arm-1-ar-18-cp-0-code-0` status `MERGED`, merge commit `4cd8d7d78` (production commit `009d4835a`, evidence
  commit within the same merge); manifest
  `specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-18-cp-0-c4.json`, read at R1: 16 obligations;
  its P1–P3 mappings name `internal/commands/init_git_root_test.go` at `command_index` 0, AC-ratchet
  `area_a7_test.go`, AC-fixture and AC-delete `internal/integration/await_verdict_test.go`, AC-frozen the install oracle.
- **Removal:** `git grep -n -w -e InitProject -e InitProjectParams -- cmd internal` at R1 prints exactly one line,
  `internal/testguard/archguard/s1_snapshot_test.go:77:  Key{File: "internal/ops/init_project.go", Func: "InitProject"}: 229`
  (the immutable start snapshot), exit 0. `git ls-tree HEAD internal/ops/` lists no `init_project` file;
  `go list ./internal/ops/...` prints one package.
- **Git hardening:** the four init helpers in `internal/commands/init_integration_branch.go` call
  `gitpkg.CombinedOutput(dir, "check-ref-format", "--branch", name)`, `…(dir, "branch", name, "HEAD")`,
  `gitpkg.Output(dir, "rev-parse", "--verify", name)` and `…(dir, "rev-parse", "--verify", "HEAD")`; no non-test
  `exec.Command("git", …)` remains outside `internal/gitenv` except `cmd/liza/launch_resolve.go:127` (`--cwd`; G6).
- **P1–P3 run:** `go test -count=1 -run 'TestInitGitTargetsProjectRoot|TestInitBranchValidationPrecedence|TestInitGitTimeout' -v
  ./internal/commands/`, exit 0 at about 15:36Z, before the G4 batch: `--- PASS` for
  `TestInitGitTargetsProjectRoot_BranchCreatedAtRootHEAD`, `…_ExistingRootRefDetected`,
  `…_BornRootHEADFromUnbornWorktree`, `…_BranchNameValidation`, `TestInitBranchValidationPrecedence_NonRepository` and
  `TestInitGitTimeout_HelpersReportTimeout` (12.57 s); `ok …/internal/commands 13.600s`. The same command is task
  validation index 5 and ran again, as part of the whole package, in the race run of G4.
- **Red before green:** the pre-C4 red run (failing test names, failure lines, 48.7 s) is recorded in the body of
  commit `4cd8d7d78`; it is a recorded observation of the C4 unit, not re-run here.

B-c4 holds: merged, `InitProject` gone from `cmd` and `internal` except the start snapshot, P1–P3 green at R1.

## G4 — suite green at the integration revision

All commands run at R1 in the canonical form `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp <command>`, once each and not
retried, with the task worktree as target (`make -C <worktree>`, `go -C <worktree>`; no translation of the canonical
commands beyond the working-directory flag the bash constraints require). The operator note of 2026-10-25T11:20Z tells
coders not to pre-run the canonical list before submitting; here the run is this unit's deliverable (boundary
evidence at the integration SHA, plan Task 1 step 4), and submission re-runs the list on the rebased commit.

| Cmd | Command | Start–End (UTC) | Exit | Excerpt |
|---|---|---|---|---|
| C0 | `go test -count=1 ./internal/testguard/archguard/` | about 15:35Z | 0 | `ok …/internal/testguard/archguard 0.894s` |
| C5 | `go test -count=1 -run 'TestInitGitTargetsProjectRoot|TestInitBranchValidationPrecedence|TestInitGitTimeout' -v ./internal/commands/` | about 15:36Z | 0 | six tests `--- PASS`; `ok …/internal/commands 13.600s` |
| C1 | `make test` | 15:37:11–15:42:22 | 0 | the last 25 lines are `ok` lines (none `(cached)`, no `FAIL`), ending `ok …/scripts/ci/testshard 0.712s`; `internal/testhelpers/oracle/cmd/regen` 47.661s |
| C2 | `make lint` | 15:42:22–15:43:18 | 0 | `✓ Embedded artifacts are consistent with masters`; `go vet ./...` for `GOOS` linux, windows and darwin; `ok …/internal/testguard/archguard 0.229s` (`lint-archguard`) |
| C3 | `make test-e2e` | 15:43:18–15:45:03 | 0 | `go test -race -tags e2e -run '^(TestFullSprintSequence|TestTerminalDependencyRecovery)$' ./internal/integration/`: `ok …/internal/integration 85.371s` |
| C4 | `go test -race ./cmd/liza/ ./internal/commands/ ./internal/tui/ ./internal/interactive/ ./internal/jsonout/` | 15:45:03–15:47:38 | 0 | `ok` for all five: `cmd/liza` 144.694s, `internal/commands` 78.989s, `internal/tui` 3.872s, `internal/interactive` 1.045s, `internal/jsonout` 1.139s |

Task validation indices 0–5 are C0, C1, C2, C3, C4 and C5. The G4 batch kept only the last 25 lines of each command, so
the package count of C1 is not read from it; the two `make test` runs of G5 at the same revision, run with
`GOFLAGS=-count=1`, printed 84 `ok` lines, 0 `FAIL` and 0 `(cached)` each. The A7 oracles (`internal/oracle/a7/install`,
`internal/oracle/a7/present`) and the A8 oracle ran live inside C1 with every golden unchanged (this unit changes no
oracle file). Observation on Git transcripts (operator note 2026-10-25T23:31Z): this unit freezes no Git output.
G4 holds: no red suite is recorded as green.

## G5 — wall time and benchmarks against `baseline.md` (indicative; never blocks)

Protocol repeated from `baseline.md`: fresh disposable clone outside the worktrees (`/tmp/a7g5clone.eQduGj/repo`:
`git clone --local --no-checkout <project root>` then `git checkout --detach 4cd8d7d78…`; clone `HEAD` printed
`4cd8d7d7811ea85b5de90484256ab162301f7169`), sync-clean check (`make sync-embedded` exit 0, `Files synced successfully`,
`git status --short` empty, 0 bytes), then one warm-up `make test`, one timed `make test`, then `timeout 300 go test -run
'^$' -bench . -benchmem ./internal/<pkg>` under `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp GOFLAGS=-count=1`. No
retry. The clone's `git status --short` was empty afterwards and the clone was deleted (`ls -d /tmp/a7g5clone.*` matches
nothing).

- Host load average (1, 5, 15 min, `/proc/loadavg`): before warm-up `2.18 8.43 12.99`; before timed run `8.00 9.80
  12.70`; after timed run `7.94 9.61 12.11`. The baseline ran at about 30 on 8 cores, so the comparison is
  load-confounded in this run's favour.

| Run | Start (UTC) | End (UTC) | Wall s | Exit | Output |
|---|---|---|---|---|---|
| Warm-up | 15:48:32 | 15:52:31 | 239 | 0 | 84 `ok`, no `FAIL`, no `(cached)` |
| Timed | 15:52:31 | 15:56:02 | 211 | 0 | 84 `ok`, no `FAIL`, no `(cached)` |

N4: the baseline timed run was 523 s, red (two `internal/agent` deadline tests) and taken under load about 30; +25%
is 653.75 s. 211 s is 60% below it and inside the bound; against the green 444 s warm-up (a separate observation) the
timed run is 52% below and the warm-up 46% below. **Disposition:** within bound, recorded with the baseline's load
caveat, since both numbers favour this run. Per-package times, warm-up and timed: `cmd/liza` 118.5 s and 108.5 s,
`internal/commands` 56.9 s and 52.2 s, `internal/ops` 196.6 s and 205.5 s, `internal/agent` 199.2 s and 195.9 s,
`internal/oracle/a7/install` 4.7 s and 4.5 s, `internal/oracle/a7/present` 18.6 s and 19.6 s.

Benchmarks: all three packages exited 0 with `PASS` (`internal/db` 49.767s, `internal/commands` 6.872s,
`internal/agent` 2.992s); all 18 benchmark rows of the baseline were captured, none "not captured".

| Benchmark | ns/op base → now | B/op, allocs/op |
|---|---|---|
| db `BlackboardWrite` | 12033415 → 1983833 (−83.5%) | same |
| db `BlackboardModify` | 22577288 → 3017341 (−86.6%) | B/op +0.6%, allocs +0.7% |
| db `BlackboardReadCached` | 44938 → 28326 (−37.0%) | same |
| db `BlackboardReadUncached` | 1890776 → 694210 (−63.3%) | same |
| db `MarshalStateForWriteLarge/repaired=false` | 536001675 → 163648309 (−69.5%) | same |
| db `MarshalStateForWriteLarge/repaired=true` | 1331986515 → 138442272 (−89.6%) | same |
| db `ReadCachedHit` | 4916749 → 1319009 (−73.2%) | same |
| db `StateParse` | 112041835 → 33483635 (−70.1%) | same |
| db `TerminalArchiveTransaction/inline/cold-instance` | 772451364 → 288126797 (−62.7%) | same |
| db `TerminalArchiveTransaction/inline/warm` | 628528774 → 468642012 (−25.4%) | same |
| db `TerminalArchiveTransaction/archived/cold-instance` | 320733652 → 352349075 (+9.9%) | same |
| db `TerminalArchiveTransaction/archived/warm` | 185103984 → 27025881 (−85.4%) | B/op −9.7%, allocs −18.3% |
| db `TerminalArchiveTransaction/archived-256/cold-instance` | 990744276 → 1206785432 (+21.8%) | same |
| db `TerminalArchiveTransaction/archived-256/warm` | 319924638 → 144290169 (−54.9%) | same |
| commands `InspectionSnapshotDuringHeartbeat/status` | 9380378 → 12333557 (+31.5%) | B/op +13.2%, allocs +7.0%; heartbeat-writes 6 → 19 |
| commands `InspectionSnapshotDuringHeartbeat/get` | 4376937 → 3176409 (−27.4%) | B/op +6.5%, allocs +5.2%; heartbeat-writes 13 → 25 |
| agent `BenchmarkClaimReviewerTask` | 34512825 → 4461157 (−87.1%) | same |
| agent `BenchmarkResumeHandoff` | 5936738 → 2314960 (−61.0%) | same |

"same" means within ±0.5%. **Dispositions** (N3 is Should; none blocks):

- **`InspectionSnapshotDuringHeartbeat/status`**: ns/op +31.5% and B/op +13.2%, allocs +7.0% against the baseline,
  out of the +25% bound. This is the observation the A3 to A6 boundaries handed to A7 (`boundary-a6.md` G5), and the
  `commands` package is what A7 refactored, so it was investigated rather than only recorded. Same-host A/B,
  `go test -run '^$' -bench InspectionSnapshotDuringHeartbeat -benchmem ./internal/commands` in the deleted clone,
  three rounds interleaving `96c9ad69a` and R1 (`make sync-embedded` before each), under the Protocol environment:

  | Round | `status` ns/op, start → R1 | `status` B/op, start → R1 | `get` ns/op, start → R1 | `get` B/op, start → R1 |
  |---|---|---|---|---|
  | 1 | 42974262 → 20870123 | 7216019 → 7223977 | 17663277 → 12975037 | 3554020 → 3582523 |
  | 2 | 34299414 → 18418452 | 7120341 → 7222298 | 7168096 → 3889775 | 3563371 → 3575325 |
  | 3 | 20072446 → 19023097 | 7180102 → 7187009 | 18105146 → 17470628 | 3653580 → 3554514 |

  R1 is not slower than the starting commit on the same host in any round (`status` ns/op 18.4–20.9 ms at R1 against
  20.1–43.0 ms at the start; `get` 3.9–17.5 ms against 7.2–18.1 ms), and B/op and allocs/op are equal within about 1%
  (`status` 7.19–7.22 MB at R1, 7.12–7.22 MB at the start). The +13.2% B/op against `baseline.md` is therefore already
  present at the starting commit: the baseline's 6.33 MB was taken under load about 30 with 6 heartbeat writes in the
  measured window (19 here), and the benchmark overlaps a concurrent heartbeat writer, so work per iteration is not
  like-for-like. **Disposition:** not attributable to A7; no correction commissioned. The figures are the same
  `status` B/op and allocation shape recorded at the A3 to A6 boundaries (+11.9% to +12.0%, +6.3%).
- **`InspectionSnapshotDuringHeartbeat/get`** B/op +6.5%, allocs +5.2%: same cause and same A/B (start 3.55–3.65 MB,
  R1 3.55–3.58 MB); ns/op is 27.4% below the baseline. No action.
- **db `TerminalArchiveTransaction/archived/cold-instance` (+9.9%) and `archived-256/cold-instance` (+21.8%)**: inside
  the +25% bound. They are cold-instance rows measured with one to six iterations, in `internal/db` (A1), a package no
  A7 unit owns or edits; the rows read 141780337 and 549573505 at the A6 boundary, so the spread between boundaries is
  larger than the difference to the baseline. Recorded as measurement noise of single-iteration rows. No action.
- **db `BlackboardModify`** B/op +0.6%, allocs +0.7%: the same figures as the A5 and A6 boundaries, within noise of the
  benchmark's own setup, in `internal/db` (A1). No action.
- **A7 paths.** The baseline set holds no benchmark of `cmd/liza`, `tui`, `interactive`, `jsonout` or the init, watch and
  agent-pool command paths; their tests and oracle cases ran in C1, C4 and the timed run (`cmd/liza` 108.5 s,
  `internal/commands` 52.2 s), so no A7-specific timing is claimed beyond that and the `commands` A/B above.
- The other rows are decreases or unchanged: no action.

## G6 — architectural issues resolved by A7 and C4

Reviewed the Open Issues Summary (66 open at R1: 17 high, 33 medium, 16 low; `specs/architecture/architectural-issues.md`,
counted from the table rows) under `issues-policy` (the file's Update Policy), the ADR 0226 registry note and C4's
closure evidence (`4cd8d7d78`, `arm-1-ar-18-cp-0-c4.json`). The registry was re-grepped for the A7 packages' paths
(`internal/commands/`, `cmd/liza/`, `internal/tui`, `internal/interactive`, `internal/jsonout`, `commands/`, `jsonout/`)
and for A7 file names; the active-section hits are `commands/init.go` and `cmd/liza/cmd_launch.go` in the raw-Git
entry, `status.go`, `repair_agent_pool.go` and `resume.go` in the commands-to-agent entry, `init.go` and `watch.go` in
the three closed entries and the file-size note of the Structural Debt section, and `cmd/liza/main.go` in the
**Evidence** of the already-fixed "Implicit Orchestrator Provenance Default"; other basename hits are ops files
sharing a name with a commands file (`add_tasks.go`, `set_task_output.go`, `proceed.go`, `wt_merge.go` and the like,
cited as `internal/ops/…`) or substrings of ops names (`agent_process_status.go`, `submit_review.go`).

**Closed (3), each wholly resolved by merged work, with the registry synchronised (active section removed, Completed
Fixes, Fixed (Traceability) and Fix Details added, Open Issues Summary and its Counts line updated to 17 high, 30
medium, 16 low, 63 total):**

| Issue | Resolving commits | Evidence at R1 |
|---|---|---|
| Decompose init.go (1,268 LOC) (`issue-init`) | `e11874725` (MAS phases), `2c289761f` (pairing init, contract placement, setup) | `init.go` 1,684 → 516 lines; `InitCommandWithConfig` 471 → 60, `InitPairingCommand` 182 → 42, `createContractSymlinksForProviders` 192 → 36, `SetupCommand` 114 → 28; the ten init and setup files have the longest function at 70 lines and none over 800 lines |
| Decompose watch.go (1,407 LOC) (`issue-watch`) | `6e24bb215` | `watch.go` 2,084 → 151 lines; `RunAutoRepairAgentPool` 133 → 35; eleven watch files, 2,328 lines, the longest function 76 lines |
| Duplicate Initialization Implementations (`issue-dup-init`) | `009d4835a` (C4, merged at `4cd8d7d78`) | `ops.InitProject` deleted with its tests; `git grep` finds `InitProject` only in the start snapshot (C4 section); the init Git helpers run through `gitenv` timed runners in the project root |

Why each is "wholly resolved": the init issue's Direction is to extract each phase into its own function or file,
and the longest init function is 70 lines against a 405-line original; the watch issue's Direction is to separate
observation, diagnosis, repair policy, lifecycle actions and rendering, which the concern files and the 35-line
auto-repair entry do, with the S1 guard green; the duplicate-implementation issue's Direction is a single owner for
initialization with Git on `gitenv`, which C4 delivers by deletion.

**Not closed, with reasons:**

- "Raw Git Calls Bypass gitenv Hardening" (`issue-raw-git`): stays open. Its initialization half is resolved by C4 (the
  four `ops/init_project.go` sites are gone and the `commands` helpers use `gitenv`), recorded in an **Update
  (2026-10-26)** paragraph of the entry. One raw call remains, `exec.Command("git", "-C", path, "rev-parse",
  "--show-toplevel")` at `cmd/liza/launch_resolve.go:127` (formerly `cmd_launch.go:333`), the `--cwd` path the goal
  keeps out of scope (`goal-wont`); the lint guard the Direction suggests does not exist.
- "Commands Layer Imports Agent Runtime" (`issue-commands-agent`): stays open. `status.go`, `repair_agent_pool.go`
  and `resume.go` still import `internal/agent`; Scope 20 (A9) owns the crossing.
- "Control-Flow Vocabulary Bypasses Domain Ownership" (`issue-vocabulary`): stays open. A7 typed and named its own
  vocabulary (ADR 0226, K7 inventory `a7-k7-inventory.md`), but the issue spans every area, and A8, A9 and the later C
  scopes are not yet refactored.
- "Custom Roles Render Empty Prompt Blocks" (`issue-custom-roles`): stays open; prompt content for custom roles is
  outside the run (`goal-wont`) and no A7 or C4 work changes it.
- "Prompts Layer Imports Business Logic" and "No Query Layer": not A7 items; A8 owns the prompts crossing, and A8
  supplies closure evidence for the former to the A9 boundary.
- "Status Vocabulary Partition Between Go Constants and Declarative Pipeline" and the other open entries found by the
  re-grep name no A7 file as their subject: no closure and no edit.

## G7 — record completeness

This file holds the commands or source reads, the integration SHA R1 `4cd8d7d7811ea85b5de90484256ab162301f7169`, the UTC
observations, exits and excerpts for G1–G6 and C4, the G5 dispositions, the G6 reasons and the G8 section below. The
acceptance manifest
(`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-19-cp-0-boundary.json`) is written once G8 is
green (it maps G1–G8 and the boundary obligations; it records no result itself).

## G8 — maintainer CI result (D52, D59, D60): pending

Status: **pending.** Under D52 the maintainer dispatches `.github/workflows/ci.yml` (`workflow_dispatch`) on a pinned ref
carrying exactly R1 `4cd8d7d7811ea85b5de90484256ab162301f7169` (the A6 precedent used `g8/a6-7677139`) and records
the Linux, Windows and macOS results. This unit marks itself BLOCKED with `--human-action` for that dispatch and does
not submit until the result is recorded here: the PR number or run URL, the tested SHA, every job name and conclusion,
the UTC observation, the ruling and the macOS follow-up's owner and status. A red result keeps A8 coding held and the
maintainer opens the fix task; if CI tests different code, the affected checks are re-run at that revision and both are
kept. No job is claimed as passed before it has run.
