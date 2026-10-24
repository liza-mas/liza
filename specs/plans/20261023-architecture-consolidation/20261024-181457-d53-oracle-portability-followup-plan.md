# Code Plan: D53 Follow-up — Windows A1/A5 Oracle Portability and the Submit-Progress Quiet Check

## Source References
Source revision: "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"

### Direct References
- "d53": "specs/goals/20261023-architecture-consolidation-decisions.md#D53 — Oracle portability for N2: generic fixes everywhere, process-supervision oracles Linux-only (human, 2026-10-24)" @ "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"
- "d31": "specs/goals/20261023-architecture-consolidation-decisions.md#D31 — A1 CLI oracle cases run in a disposable Git project with a git-only PATH (run, 2026-10-23)" @ "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"
- "d41": "specs/goals/20261023-architecture-consolidation-decisions.md#D41 — Stabilise two inherited timing-flaky internal/agent tests (run, 2026-10-24)" @ "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"
- "d46": "specs/goals/20261023-architecture-consolidation-decisions.md#D46 — Remove the time.Sleep calls merged by A9 oracle rows; the sleep budget stays (run, 2026-10-24)" @ "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"
- "d52": "specs/goals/20261023-architecture-consolidation-decisions.md#D52 — G8 boundary CI by `workflow_dispatch` on the pushed integration branch; one PR to `main` later (human, 2026-10-24)" @ "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"
- "d55": "specs/goals/20261023-architecture-consolidation-decisions.md#D55 — E1 revised for new code plans: no per-task `go test -race` (human, 2026-10-24)" @ "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"
- "d24": "specs/goals/20261023-architecture-consolidation-decisions.md#D24 — Provenance of goldens changed by a C task (run, 2026-10-23)" @ "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"
- "goal-frozen": "specs/goals/20261023-architecture-consolidation.md#Frozen Surfaces" @ "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"
- "goal-tests": "specs/goals/20261023-architecture-consolidation.md#Test Policy and Sequencing" @ "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"
- "goal-nfr": "specs/goals/20261023-architecture-consolidation.md#Non-Functional Constraints" @ "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"
- "goal-proof": "specs/goals/20261023-architecture-consolidation.md#Proof Environment" @ "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"
- "env-safe": "GUARDRAILS.md#G1.4: Environment-safe tests and diagnostics" @ "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"
- "boundary": "specs/arch-plan/20261023-architecture-consolidation/20261023-162147-arm-1.md#Area-Boundary Checkpoint Contract" @ "2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16"
- "d53-rules": "specs/plans/20261023-architecture-consolidation/20261024-160626-d53-oracle-portability-plan.md#Common Rules" @ "26de41739a882a8d0ffe9732483193a9fe29361c"
- "timing-rules": "specs/plans/20261023-architecture-consolidation/20261024-150848-deferred-validation-19-timing-followup-plan.md#Common Rules (both outputs)" @ "6b7e75b95c8a898f8bf13fd797653cee8eff803b"

### Obligation Coverage
- "W1-entry" -> "d53", "d31"
- "W1-guard" -> "d53", "d31", "env-safe"
- "W1-cases" -> "d31", "goal-tests"
- "W1-provenance" -> "d24", "goal-proof"
- "W1-lint" -> "goal-proof", "goal-nfr"
- "W1-preserve" -> "goal-tests", "d31", "d53-rules"
- "W1-windows" -> "d52", "d53", "boundary"
- "W5-coverage" -> "d53", "goal-tests"
- "W5-vp2" -> "d53", "goal-tests"
- "W5-signals" -> "d53", "goal-tests"
- "W5-package" -> "goal-proof", "d53"
- "W5-provenance" -> "d24", "goal-proof"
- "W5-lint" -> "goal-proof", "goal-nfr"
- "W5-preserve" -> "goal-tests", "d53-rules"
- "W5-windows" -> "d52", "d53", "boundary"
- "Q-order" -> "d41", "timing-rules"
- "Q-late" -> "d41", "goal-tests"
- "Q-focus" -> "d41", "goal-proof"
- "Q-package" -> "goal-proof", "d41"
- "Q-sleep" -> "d46", "timing-rules"
- "Q-lint" -> "goal-proof", "env-safe"
- "Q-preserve" -> "goal-tests", "d41", "timing-rules"
- "Q-red" -> "d41", "timing-rules"

## Intent and Allocation

Based on: the task JSON; operator note `note-d53-followup.md` (2026-10-24T17:45Z); the saved logs of
CI run 40115874515 at `79e22369a` — Windows rest-1 job 120936918717 (`v-120936918717.log`, 881 lines,
read in full around every FAIL) and Linux job 120936918918 (`v-120936918918.log`, FAIL lines and
command header); reads at `2f8b7a7b0` of `internal/oracle/a1/cli/{helpers,golden,usage}_test.go`,
`cmd/liza/main.go:49-150`, `internal/errors/errors.go:85-113`, `internal/paths/paths.go:207-288`,
`internal/gitenv/gitenv.go:1-140`, `internal/testhelpers/oracle/{env,cli,golden}.go`,
`internal/oracle/a5/{driver,oracle}_test.go`, `internal/oracle/a8/mutation_test.go:25`,
`internal/ops/{submit_progress,submission_lifecycle,admission_slot}.go` (sections),
`internal/ops/acceptance_evidence.go:945-980`, `internal/ops/submit_progress_test.go`,
`internal/ops/wt_create_test.go:1084-1122`, sibling git-PATH fixtures (a2, a3, a7), D24/D31/D41/D46/D52–D56,
the goal sections above, both provider plans, and the Reality Probes below.

Success means three independent Must coding outputs, one per reported signature, each test-only:
(1) the A1 CLI git-only PATH entry runs Git on Windows, (2) A5's coverage walk and failure-signal
expectations stop assuming `/`, (3) the submit-progress quiet check stops misreading pre-return
stderr as late. Validate this plan by payload preflight, Acceptance Contract/output parity and
independent plan review. Doc Impact: this plan, its output JSON and the three children's manifests;
the boundary record stays with `arm-1-ar-11-cp-0-code-0`; no user or oracle doc describes the
touched helpers (`rg -l 'git-only|requireQuiet|GoldenCoverage' docs specs/architecture internal/testhelpers/oracle/*.md`
hits only `ADR/0083…md:55`, "git-only cleanup" of recovered tasks, unrelated). Test Impact: none for planning; each child owns its regression.

| Task | Intent | Signature | Authority |
|---|---|---|---|
| 1 | Run Git through the A1 CLI git-only PATH on Windows | Windows rest-1 A1 `failed to detect project root` (14 leaf cases) | D53 generic fix; D31 fixture contract |
| 2 | Make A5 golden-coverage and failure-signal path checks separator-independent | Windows rest-1 A5 `driver_test.go:105/110/374/380/390` incl. `state/VP2.state.yaml` | D53 generic fix |
| 3 | Drain the submit-progress stderr capture before the quiet check | Linux `submit_progress_test.go:188` | D41 timing lineage |

All three are **Must** (D53 orders them before the A1 boundary's G8; D41 lineage for Task 3). No
Should/Could work. Provider Ask Inventory: N/A — no shared interface is frozen; every edit is in a
`_test.go` file of the package it serves. No new ruling: the fixes are generic portability (D53) and
test-only determinism (D41); none changes production, a fixture, a golden or an asserted value.

## Root Cause Analysis

### a. Evidence

**F1 — A1 CLI root detection on Windows (reporter + log).**
- Log 120936918717 lines 459–558: `TestOracleAnalysisReport`, 7 `TestOracleUsageReport`, 4
  `TestOracleValidate`, `TestOracleCLIIsolation`, `TestOraclePipelineRBAC` fail; stdout/stderr carry
  `liza: failed to detect project root` (JSON code `project_root`); the RBAC and invalid-validate
  cases fail their "reached refusal" check because the CLI stops earlier; `usage-inverted.json` (exit
  1 expected) passes `requireExit` and fails as `golden mismatch`.
- That text with no `Message`/`CurrentDir` comes only from `ProjectRootError{Operation, Err}`
  (`internal/errors/errors.go:107-108`), built at `cmd/liza/main.go:57` (`paths.GetProjectRoot`
  failed), `:61` (`Getwd`), `:65`/`:69` (`canonicalDir`). A cwd/root mismatch would print "must be
  run from project root" (`:71-77`, `errors.go:101-105`); an explicit root would print
  "--project-root … is not a git repository" (`:113-114`). A1 passes no `--project-root`.
- `GetProjectRoot` → `gitToplevelAndCommonDir` runs `gitenv.Output(dir, "rev-parse",
  "--show-toplevel", "--git-common-dir")` (`paths.go:270-288`), i.e. `exec.CommandContext(ctx,
  "git", …)` resolved through the CLI's `PATH` (`gitenv.go:83`, `:139`). RP-6 shows these are the
  only Git calls the A1 cases make besides one `ls-tree … :(literal)README.md`.
- The CLI's `PATH` is the fixture's git-only directory holding one entry, a symlink named `git.exe`
  on Windows to the test process's `exec.LookPath("git")` result (`helpers_test.go:53-63`, checked at
  `:132-138`); its env is `oracle.ChildEnv` plus the D31 Git pins (`helpers_test.go:64`, `:113`;
  `env.go:42-73` adds USERPROFILE/TMP/TEMP/SYSTEMROOT on Windows).
- The same host Git, invoked by that absolute path with the identical env and directory, succeeded
  on Windows: `newProject` runs `git init` that way (`helpers_test.go:69-75`) and would `t.Fatal`
  "disposable Git initialization failed" otherwise; every failing case got past it to the CLI run.
- Sibling Windows precedent: `internal/ops/wt_create_test.go:1091-1100` builds its isolated PATH on
  Windows from a forwarding `git.cmd` shim instead of a symlink ("the isolated PATH would hold a git
  that nothing can run"); the ops Windows shards are green in this run (operator note), but the
  shim's Windows execution path is not traced (d).

**F2 — A5 path separators (log + source).**
- Log lines 561–753: `TestA5GoldenCoverage` reports 98 `unexpected golden testdata/golden/…` and
  91 `missing golden …`, the latter including `state/VP2.state.yaml` and `state/VP1.state.yaml`
  (lines 674, 722) while `unexpected golden testdata/golden/state/VP{1,2}.state.yaml` appear at
  659–660. `git ls-files internal/oracle/a5/testdata/golden` lists 98 files including both VP goldens.
- `driver_test.go:89-91`: `filepath.Walk("testdata/golden", …)` then
  `strings.TrimPrefix(path, "testdata/golden"+string(filepath.Separator))`. Walk joins children with
  `filepath.Join`, which on Windows yields `testdata\golden\human\AA1.txt`; the prefix
  `testdata/golden\` never matches, so `ToSlash` keeps the `testdata/golden/` prefix on every file.
- Log lines 754–763: `TestA5FailureSignals` (`driver_test.go:374`, `:380`) and `TestA5JSONStderr`
  (`:390`) receive `testdata\\golden\\human\\…` messages and look for `testdata/golden/human/…`
  (`:373`, `:379`, `:389`). The harness builds the path with `filepath.Join(root, "testdata",
  "golden", kind, filepath.FromSlash(storage))` (`internal/testhelpers/oracle/golden.go:39`) and
  prints it as is (`:106`, `:113`). In-repo precedent for the platform form:
  `internal/oracle/a8/mutation_test.go:25` (`filepath.FromSlash("testdata/golden/prompt/missing.txt")`).

**F3 — submit-progress quiet check (log + source + reproduction).**
- Log 120936918918 line 771–772 (`go test -race -timeout 30m -p 2 …`, line 358):
  `submit_progress_test.go:188: progress written after the submit returned: "submit-for-review
  task-acceptance-submit: running canonical validation, batch budget 30s; 0s since the submit
  started, at most until …Z. Still working: …\n"`.
- Production joins every writer before returning: `SubmitForReviewWithAuthority` defers
  `progress.finish()` (`submission_lifecycle.go:80-81`), which closes `stop` and waits `done`
  (`submit_progress.go:84-90`); the heartbeat goroutine writes only inside `repeat` before `done`
  closes (`:41-55`); every `set` caller runs synchronously on the submit goroutine
  (`submission_lifecycle.go:96`, `admission_slot.go:157-161` via synchronous `acquireAdmissionSlot`,
  `acceptance_evidence.go:959`, `:979`). Each `Fprintln` to `os.Stderr` (`submit_progress.go:79`)
  returns after the bytes are in the pipe.
- The test's capture is asynchronous: `captureStderr` copies the pipe into a buffer on its own
  goroutine (`submit_progress_test.go:40-52`); `requireQuiet` snapshots that buffer (`:162`), sleeps
  `5 * submitHeartbeatInterval` (`:163`) and treats any growth as a late write (`:164-165`). A line
  written before the return but not yet copied when the snapshot is taken is reported as late.
- RP-7/RP-8 reproduce and discriminate this (below).

### b. Mechanism

- **F1.** The CLI resolves `git` in the git-only directory to the `git.exe` symlink and runs it;
  that process fails the `rev-parse` query on Windows, `GetProjectRoot` returns an error, and the
  CLI exits 1 with the generic project-root error before reaching any tested surface. The host
  binary run from its install path works with the same environment and directory. Which Windows
  facility makes the linked copy fail (for example dependent DLL lookup relative to the link's
  directory) is **not established** (d); the fix does not depend on it (e).
- **F2.** `TestA5GoldenCoverage` derives golden names by trimming a mixed-separator prefix that
  never matches a Windows walk path, so all 98 files look unexpected and all 91 inventory goldens
  missing — `state/VP2.state.yaml` is one instance, not a separate lookup or coverage defect. The
  three failure-signal checks compare a slash literal with the harness's OS-native path.
- **F3.** `requireQuiet` races the capture goroutine: a progress line that production wrote before
  `SubmitForReviewWithAuthority` returned can still be in the pipe at the snapshot, and its later
  copy is misattributed. The race detector's scheduling delay (CI runs `-race`) widens the window.

### c. Reproduction

- **F1.** Windows only: the Windows rest-1 job of a D52 dispatched run, or `go test -count=1
  ./internal/oracle/a1/cli/` on a Windows host with Git for Windows. Task 1's
  `TestOracleCLIGitOnlyPath` runs the CLI's exact query through the git-only PATH entry with the
  fixture environment, so on Windows it fails on the symlink entry and passes on the shim. Not
  reproducible on Linux (RP-2: Linux passes before the change).
- **F2.** Windows only, same job. On Linux the walk paths and messages already use `/` (RP-1).
- **F3.** RP-7: unchanged tree, `go test -race -cpu 1 -count=40 -run
  '^TestSubmitProgressRepeatsWhileQueuedAndRunning$' ./internal/ops/` → 2 of 40 fail with the CI
  signature verbatim. Deterministic shape without race (RP-8): write ≥128 KiB to the captured stderr
  (beyond the 64 KiB Linux pipe buffer), then call the current `requireQuiet` → 41 of 50 runs report
  the burst as "progress written after the submit returned". Task 3's regression is that shape.

### d. Verification scope

Checked: the log lines cited; every source line cited at `2f8b7a7b0`; that newProject's absolute-path
`git init` precedes each failing CLI run; that the A1 cases issue only `rev-parse --show-toplevel
--git-common-dir` and `ls-tree --full-tree -z integration -- :(literal)README.md` (RP-6); that the 98
A5 files are tracked; Linux runs of every named test (RP-1..RP-4); the F3 reproduction, the drained
counterfactual and a late-write control in a disposable clone (RP-7, RP-8).
Not checked (no Windows host or dispatch authority in this task): why the symlinked `git.exe` fails
on Windows (unverified); that a `git.cmd` forwarding shim run by the CLI through `exec` works on
Windows (unverified until the D52 run; the ops precedent's shim sits on the PATH that `git commit`
passes to its hook shell, `wt_create_test.go:1128-1132`, and whether Windows ever runs it was not
traced, so it is a house pattern, not proof); what A1 comparisons do on Windows once root detection passes
(masked by F1, never observed); A5 behaviour on Windows beyond these three tests (`TestOracleA5` and
`TestA5Isolation` skip on Windows, `oracle_test.go:98`, `driver_test.go:306`).

### e. Sufficiency

- **F1.** The fix makes the PATH entry execute the host Git at the absolute path the Windows run
  proved working, with the unchanged env, so it removes the only Windows-specific difference between
  the failing and the working invocation without relying on the unverified facility. Residuals,
  named for the boundary: (i) the shim's own Windows behaviour — `cmd.exe` re-parses `%*`, and an
  argument with `^ & | < > % "` would be changed; RP-6 shows none in the A1 Git calls, and any
  mangling would fail a byte-exact golden rather than pass silently; (ii) A1 differences hidden
  behind root detection on Windows. Either surfaces as a new Windows signature in the D52 run and
  keeps G8 red; this plan does not claim them.
- **F2.** Platform-relative names and platform-form expected substrings remove every reported A5
  failure; no other A5 check compares a walk path or harness message with a slash literal outside
  the opt-in, Windows-skipped `sensitivity_test.go` (f).
- **F3.** FIFO markers written through the same `os.Stderr` pipe make the snapshot contain every
  byte written before it, so a pre-return line can no longer be attributed to the quiet window; the
  window still catches any write between the markers. RP-8: drained helper 80/80 green under the
  RP-7 reproduction, burst shape 50/50 green, injected late write caught 5/5. No residual on this
  path; production already guarantees the property under test.

### f. Adjacent findings (recorded, not planned)

- `internal/oracle/a5/sensitivity_test.go:119-148` and `internal/oracle/a1/cli/tamper_test.go:104-129`
  match slash path literals in tool output; both are opt-in and skip on Windows (`:104`, `:90`).
- `internal/oracle/a2/cli/helpers_test.go:135,146` symlinks and runs `bin/git` with no extension;
  Windows shards are reported green, so it is either not reached on Windows or tolerated there —
  unverified, out of scope.
- `internal/oracle/{a3/control,a3/lifecycle,a7/present}` symlink `git` without `.exe` but skip off
  Linux (`normalise_test.go:15`, `env_test.go:20`, `main_test.go:25`).
- `submitProgress.set` does not check `stop`, so a caller setting state after `finish` would write;
  no caller does (a), so this is not a current defect.

## Common Rules

- **Oracle immutability (D53 portability exception, `d53-rules`).** No fixture, golden content,
  golden name or asserted value changes; no `PROVENANCE` edit; regen check is the provenance proof.
  No skip, `//go:build` constraint or `runtime.GOOS` early return is added to any test.
- **Test-only.** No file outside the owned `_test.go` files and each task's manifest. Production
  (`cmd/`, non-test `internal/`), the harness (`internal/testhelpers/oracle/`) and the D56 regen work
  are untouched.
- **Environment (G1.4, E2–E4).** Every command runs under `env -i HOME="$HOME" PATH="$PATH"
  TMPDIR=/tmp`; disposable Git only; new failure messages name variables, files and booleans, never
  environment values or host paths.
- **Timing (`timing-rules`).** No new `time.Sleep`; `internal/testguard/sleep_usage_test.go` is
  unedited and `maxSleepCallsInTests` stays 16; waits for an event may carry a failure ceiling.
- **TDD.** Each task commits its regression test before or with the edit it guards and records the
  red run (or, where Linux cannot fail, the before/after green runs and the CI evidence) in its
  checkpoint.
- **Proof.** Pre-commit on touched files, then the canonical commands serially. `make test` (and for
  Task 3 `make test-e2e`) is listed once and deferred. No race command anywhere (D55); race coverage
  is the Linux race job of the D52 boundary run. `make test-e2e` is not listed for Tasks 1–2: no
  product or integration path changes.

## Assertion Traceability (original → retained)

| Assertion (location at `2f8b7a7b0`) | Retained as | Change |
|---|---|---|
| A1 every CLI case: exit codes, stdout/stderr channel checks, goldens (`usage_test.go`, `validate_test.go`, `analysis_test.go`, `isolation_test.go`, `rbac_test.go`, `golden_test.go`) | unchanged | none |
| A1 `assertEnvironment` allowed/required names (`helpers_test.go:160-197`) and explicit-value check (`:118-131`) | unchanged | none |
| A1 "CLI PATH must contain only Git" (`helpers_test.go:136-138`) | same check, entry name from one platform helper (`git` / `git.cmd`) | Windows entry name only |
| A5 `TestA5GoldenCoverage` exact-set rule, json-stderr exemption (`driver_test.go:79-112`) | unchanged rule; names derived by `filepath.Rel` + `ToSlash` | name derivation only |
| A5 `TestA5FailureSignals` path + "is missing" / "differs at line 1" (`:373`, `:379`) | same substrings, path in `filepath.FromSlash` form | path form only |
| A5 `TestA5JSONStderr` lost-stderr path + "differs at line"; appeared "is missing"; quiet "" (`:389-397`) | same, path in `filepath.FromSlash` form | path form only |
| ops `TestSubmitProgressRepeatsWhileQueuedAndRunning`: ≥2 queued lines, budget-stopped wording, ≥2 running lines, COMPLETED, no output after return (`submit_progress_test.go:172-189`) | unchanged body; `requireQuiet` drains first | helper only |
| ops `TestSubmitProgressWithoutAdmissionClass` (`:193-206`) | unchanged body; same helper | helper only |
| ops `requireQuiet` failure text "progress written after the submit returned: %q" (`:165`) | unchanged text | none |

## Task 1: Run Git through the A1 CLI git-only PATH on Windows

**Priority:** Must (`d53` generic fix; `d31`).
**Direct references:** ["d53", "d31", "d24", "d52", "goal-tests", "goal-proof", "goal-nfr", "env-safe", "boundary", "d53-rules"]

**Owned files:** `internal/oracle/a1/cli/helpers_test.go`; new
`internal/oracle/a1/cli/git_path_test.go`; manifest
`specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-followup-plan-a1-git-path.json`.
**Not edited:** every other A1 file, fixtures, goldens, `gitFixtureEnv`, `assertEnvironment`,
`internal/testhelpers/oracle/`, production.

1. **Guard first.** Add `TestOracleCLIGitOnlyPath` in `git_path_test.go`: build `newProject(t)`;
   resolve `git` inside the git-only directory as the CLI does (`exec.LookPath` of
   `filepath.Join(p.path, "git")`, which applies Windows executable extensions) and require the
   result to lie in `p.path`; run `rev-parse --show-toplevel --git-common-dir` (the CLI's query,
   `paths.go:271`) with `cmd.Dir = p.root` and the same `oracle.ChildEnv` + `gitFixtureEnv` env as
   `run`; require exit 0, two lines, and the `filepath.EvalSymlinks` form of line 1 equal to that of
   `p.root` (the comparison `cmd/liza/main.go:63-71` makes). Failure messages carry booleans and
   names only. Record Linux green; on Windows it is the fixture-level reproduction of F1.
2. **Entry.** Introduce one helper returning the git-only entry's file name (`git` off Windows,
   `git.cmd` on Windows) and use it in `newProject` and in `run`'s single-entry check (replacing the
   two `gitName` blocks, `:57-60`, `:132-135`). Off Windows the entry stays the symlink to the host
   Git, unchanged. On Windows write `git.cmd` containing `@echo off`, CRLF, then the double-quoted
   absolute host Git path followed by ` %*`, CRLF (precedent `internal/ops/wt_create_test.go:1095`);
   fail with a names-only message if that path is not absolute or contains `%` or `"`. Add one
   comment stating the ceiling: `cmd.exe` re-parses `%*`, so an argument containing `^ & | < > % "`
   would be altered; the A1 Git calls carry none (RP-6) and a change would fail a golden.
3. **Unchanged contract.** `newProject` still runs `git init` through the host path; the CLI env,
   pinned Git variables and every assertion stay as they are; the git-only directory holds exactly
   one entry and no provider.

**Done when:** `TestOracleCLIGitOnlyPath` passes on Linux and drives the CLI's own Git query through
the git-only PATH entry; on Windows the entry is a single `git.cmd` forwarding to the absolute host
Git and off Windows the symlink is unchanged; `TestOracleUsageReport` (7), `TestOracleValidate` (4),
`TestOracleAnalysisReport`, `TestOracleCLIIsolation`, `TestOraclePipelineRBAC` and the whole package
pass on Linux with no golden, fixture, env-policy or assertion change; regen check reports every A1
CLI golden `ok … H1`; the D52 Windows rest-1 job is the Windows acceptance (W1-windows).

Mappings: W1-entry → `internal/oracle/a1/cli/helpers_test.go`, `TestOracleCLIGitOnlyPath` resolves
the platform entry inside the git-only directory, index 0; W1-guard →
`internal/oracle/a1/cli/git_path_test.go`, `TestOracleCLIGitOnlyPath` exit 0 and root equality,
index 0; W1-cases → `internal/oracle/a1/cli/usage_test.go`, the five named tests, index 0;
W1-provenance → `internal/testhelpers/oracle/cmd/regen/main.go`, check mode exits 0 with every A1 CLI
golden H1, index 2; W1-lint → `Makefile`, `lint` (gofmt, vet for linux/windows/darwin), index 3;
W1-preserve and W1-windows use their `approved_reference_id`.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-followup-plan-a1-git-path.json",
  "obligations": ["W1-entry", "W1-guard", "W1-cases", "W1-provenance", "W1-lint", "W1-preserve", "W1-windows"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -v -run '^(TestOracleCLIGitOnlyPath|TestOracleUsageReport|TestOracleValidate|TestOracleAnalysisReport|TestOracleCLIIsolation|TestOraclePipelineRBAC)$' ./internal/oracle/a1/cli/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/oracle/a1/cli/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a1/cli/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test"
  ],
  "timeout_seconds": 1800,
  "deferred": [4],
  "approved_proofs": [
    {"obligation_id": "W1-preserve", "reference_id": "goal-tests", "rationale": "Review the diff against the Assertion Traceability rows: only the Windows entry form, the shared entry-name helper and the new guard test change; no golden, fixture, gitFixtureEnv, assertEnvironment rule or CLI assertion changes; the git-only directory still holds exactly one provider-free entry and no skip or build constraint is added."},
    {"obligation_id": "W1-windows", "reference_id": "d52", "rationale": "Linux cannot fail on F1, so a passing receipt cannot show the Windows correction; the checkpoint records Linux green before and after plus the CI job 120936918717 red evidence, and Windows acceptance is the rest-1 job of the D52 dispatched run that the boundary task awaits."}
  ]
}
```

## Task 2: Make A5 golden-coverage and failure-signal path checks separator-independent

**Priority:** Must (`d53` generic fix).
**Direct references:** ["d53", "d24", "d52", "goal-tests", "goal-proof", "goal-nfr", "boundary", "d53-rules"]

**Owned files:** `internal/oracle/a5/driver_test.go`; manifest
`specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-followup-plan-a5-paths.json`.
**Not edited:** `oracle_test.go` (inventory), `sensitivity_test.go`, every other A5 file, goldens,
fixtures, the harness, production.

1. **Coverage names.** In `TestA5GoldenCoverage` derive each name with
   `filepath.Rel(filepath.FromSlash("testdata/golden"), path)` and `filepath.ToSlash`; a `Rel` error
   fails the walk. Keep the walk root, the exact-set rule and the json-stderr exemption. This
   restores coverage lookup for all 91 inventory goldens, `state/VP2.state.yaml` and
   `state/VP1.state.yaml` included.
2. **Signal paths.** In `TestA5FailureSignals` and `TestA5JSONStderr` build the three expected path
   substrings with `filepath.FromSlash` (precedent `internal/oracle/a8/mutation_test.go:25`); keep
   "is missing", "differs at line 1", "differs at line" and the appeared/quiet checks verbatim.
3. **Evidence.** Linux runs before and after are green (Linux cannot fail on F2); `make lint` vets
   the package for windows and darwin; the checkpoint cites log 120936918717 lines 561–763 as the
   red evidence and confirms by `git ls-files` that the inventory and the 98 tracked goldens are
   unchanged.

**Done when:** `TestA5GoldenCoverage` names goldens relative to `testdata/golden` with `/` on every OS
and finds exactly the inventory set including `state/VP2.state.yaml`; `TestA5FailureSignals` and
`TestA5JSONStderr` expect the harness's platform-form golden paths with unchanged wording checks;
`TestA5Inventory` and the package pass on Linux; regen check reports all 98 A5 goldens `ok … H1`;
no golden, fixture, inventory or asserted value changes; Windows acceptance is the D52 rest-1 job.

Mappings: W5-coverage → `internal/oracle/a5/driver_test.go`, `TestA5GoldenCoverage`, index 0; W5-vp2
→ `internal/oracle/a5/driver_test.go`, `TestA5GoldenCoverage` counts `state/VP2.state.yaml` as an
expected, present golden, index 0; W5-signals → `internal/oracle/a5/driver_test.go`,
`TestA5FailureSignals` and `TestA5JSONStderr`, index 0; W5-package →
`internal/oracle/a5/oracle_test.go`, package `./internal/oracle/a5/...` passes, index 1;
W5-provenance → `internal/testhelpers/oracle/cmd/regen/main.go`, check mode exits 0 with every A5
golden H1, index 2; W5-lint → `Makefile`, `lint`, index 3; W5-preserve and W5-windows use their
`approved_reference_id`.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-followup-plan-a5-paths.json",
  "obligations": ["W5-coverage", "W5-vp2", "W5-signals", "W5-package", "W5-provenance", "W5-lint", "W5-preserve", "W5-windows"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -v -run '^(TestA5Inventory|TestA5GoldenCoverage|TestA5FailureSignals|TestA5JSONStderr)$' ./internal/oracle/a5/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/oracle/a5/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a5/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test"
  ],
  "timeout_seconds": 1800,
  "deferred": [4],
  "approved_proofs": [
    {"obligation_id": "W5-preserve", "reference_id": "goal-tests", "rationale": "Review the diff against the Assertion Traceability rows: only name derivation and the expected path form change in driver_test.go; the exact-set rule, the json-stderr exemption, every wording check, the inventory, goldens and fixtures are unchanged, and no skip or build constraint is added."},
    {"obligation_id": "W5-windows", "reference_id": "d52", "rationale": "Linux paths already use '/', so a passing receipt cannot show the Windows correction; the checkpoint cites CI job 120936918717 lines 561-763 as red evidence, and Windows acceptance is the rest-1 job of the D52 dispatched run that the boundary task awaits."}
  ]
}
```

## Task 3: Drain the submit-progress stderr capture before the quiet check

**Priority:** Must (`d41` timing lineage).
**Direct references:** ["d41", "d46", "goal-tests", "goal-proof", "env-safe", "timing-rules"]

**Owned files:** `internal/ops/submit_progress_test.go`; manifest
`specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-followup-plan-ops-submit-progress.json`.
**Not edited:** `internal/ops/submit_progress.go` and all production, the two
`TestSubmitProgress*` test bodies, `TestDuplicateSubmitIsRefusedWhileTheFirstIsQueuedOrRunning`,
`internal/testguard/`.

1. **Red first.** Add `TestRequireQuietAttributesPreReturnWrites`: `shortSubmitHeartbeat`, capture,
   write one line of at least 128 KiB to `os.Stderr` synchronously, then `requireQuiet`. Run it with
   `-count=20` against the current helper and record the failures ("progress written after the
   submit returned" naming the burst) in the checkpoint (RP-8: 41/50).
2. **Drain.** Rework `requireQuiet` (and the helpers it needs, widened to `testing.TB`) so it writes
   a unique marker to `os.Stderr` — the same pipe production writes to — waits until the capture
   contains it, takes the settled text there, runs the quiet window, writes and awaits a second
   marker, and fails with the unchanged message if anything lies between the two markers. The
   window is the existing `time.Sleep(5 * submitHeartbeatInterval)`, moved into a window argument
   so the file's `time.Sleep(` count stays 1; marker waits reuse the ticker-and-ceiling pattern of
   `waitForProgress`.
3. **Late writes still fail.** Add `TestRequireQuietReportsWriteAfterReturn`: a window that writes a
   line to `os.Stderr` must make the helper fail; observe it through a recording `testing.TB` (Fatalf
   records and `runtime.Goexit`s on its own goroutine, as `internal/oracle/a5/driver_test.go:341-363`)
   and assert the recorded message contains "progress written after the submit returned" and the
   line.
4. **Unchanged tests.** `TestSubmitProgressRepeatsWhileQueuedAndRunning` and
   `TestSubmitProgressWithoutAdmissionClass` keep their bodies, counts, wording checks and call to
   `requireQuiet`.

**Done when:** the burst regression failed on the old helper (recorded) and passes 50 times with the
drained helper; the late-write regression proves a write inside the quiet window still fails with the
original message; `TestSubmitProgressRepeatsWhileQueuedAndRunning` still requires ≥2 queued lines with
the stopped-budget wording, ≥2 running lines, COMPLETED and no output after return, and passes 50
times with its sibling; the ops package passes; `TestSleepUsageBudget` passes with the budget at 16
and no new `time.Sleep(`; production is unchanged.

Mappings: Q-order → `internal/ops/submit_progress_test.go`,
`TestRequireQuietAttributesPreReturnWrites`, index 0; Q-late → `internal/ops/submit_progress_test.go`,
`TestRequireQuietReportsWriteAfterReturn`, index 0; Q-focus → `internal/ops/submit_progress_test.go`,
`TestSubmitProgressRepeatsWhileQueuedAndRunning` and `TestSubmitProgressWithoutAdmissionClass` ×50,
index 0; Q-package → `internal/ops/submit_progress_test.go`, package `./internal/ops/` passes, index 1;
Q-sleep → `internal/testguard/sleep_usage_test.go`, `TestSleepUsageBudget`, index 2; Q-lint →
`Makefile`, `lint`, index 3; Q-preserve and Q-red use their `approved_reference_id`.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-followup-plan-ops-submit-progress.json",
  "obligations": ["Q-order", "Q-late", "Q-focus", "Q-package", "Q-sleep", "Q-lint", "Q-preserve", "Q-red"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=50 -run '^(TestSubmitProgressRepeatsWhileQueuedAndRunning|TestSubmitProgressWithoutAdmissionClass|TestRequireQuietAttributesPreReturnWrites|TestRequireQuietReportsWriteAfterReturn)$' ./internal/ops/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -timeout 30m ./internal/ops/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -run '^TestSleepUsageBudget$' ./internal/testguard/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-e2e"
  ],
  "timeout_seconds": 3600,
  "deferred": [4, 5],
  "approved_proofs": [
    {"obligation_id": "Q-preserve", "reference_id": "d41", "rationale": "Review the diff against the Assertion Traceability rows: both TestSubmitProgress bodies, their counts and wording, the requireQuiet failure text and every production file are unchanged; only the helper's snapshot is drained through markers and no deadline is widened or failure suppressed."},
    {"obligation_id": "Q-red", "reference_id": "timing-rules", "rationale": "A passing receipt cannot show the pre-correction failure; the reviewer checks that the checkpoint records TestRequireQuietAttributesPreReturnWrites failing on the old helper with the 'progress written after the submit returned' signature before the drain was added."}
  ]
}
```

## Dependencies and Shared-File Ownership

| Output | Waits on | Earliest sufficient artifact | Priority |
|---|---|---|---|
| 0 (Task 1) | none | current integration `2f8b7a7b0` (D31 fixture, D53 children merged) | Must |
| 1 (Task 2) | none | current integration (D53 golden storage names merged) | Must |
| 2 (Task 3) | none | current integration (D-188 reporter and its tests merged) | Must |

No `depends_on`, `task_depends_on`, `provider_dependencies` or `inherit_inputs`: both provider plans
and all their children are merged into `2f8b7a7b0` (RP-9), and no output needs another's result.

| File or definition | Writers | Ordering |
|---|---|---|
| `internal/oracle/a1/cli/helpers_test.go`, new `git_path_test.go` | Task 1 only | Sole writer |
| `internal/oracle/a5/driver_test.go` | Task 2 only | Sole writer; `arm-1-ar-14-cp-0-code-1` adds a new `internal/oracle/a5/supplement` package (new files) — no common file |
| `internal/ops/submit_progress_test.go` | Task 3; later `arm-1-ar-14-cp-0-code-2` (K-SUB white-box tests, DRAFT_CODE) | Commute: Task 3 edits only the helpers `requireQuiet`/`waitForProgress` and adds two test names; code-2's structural refactor of `submit_progress.go` claims none of these names and must keep what they assert (goal Test Policy); a moved file rebases onto Task 3 |
| Acceptance manifests | one task each | Distinct paths |

## Placement

Rule: `d53` Order — corrections land before the A1 boundary's G8; the boundary then rebases on
`global-refactoring`, reruns G1–G7 at the corrected integration SHA and holds for one D52 dispatched
run at that exact SHA with Linux, Windows and macOS green. Task 3 joins under the same rule, as
`d53` placed the timing follow-up before that rerun. Correction lineage: this plan (new; supersedes
nothing).

- **Predecessors:** none started. Every writer of the owned files is merged into `2f8b7a7b0`
  (A1 CLI oracle, A5 oracle, D-188 progress, D53 children); RP-9 finds no IMPLEMENTING or REVIEWING
  coding task. No wait is declared.
- **Successors:** `arm-1-ar-11-cp-0-code-0` (BLOCKED A1 boundary). The orchestrator holds it on
  every child of this plan through its supported provider reservation; this plan adds no edge to or
  from it, so the correction never depends on its consumer. Neither the old G1–G7 evidence nor red
  run 40115874515 closes G8. Later unstarted writers (`arm-1-ar-14-cp-0-code-1`, `-code-2`) keep
  their admission default (table above).
- **Independent:** `arm-1-ar-0-cp-2-d56` (D56 harness correction, REVIEWING_CODING_PLAN) owns
  `internal/testhelpers/oracle/cmd/regen`; no file overlap and no edge either way. The merged D53
  and timing outputs are not repeated.
- **Slot:** immediately, on current integration, before the boundary rerun.

## Reality Probes

Run at `2f8b7a7b0df3dacfd57dc02cffa0af3a2d64ed16` (worktree HEAD), 2026-10-24T17:51Z–18:14Z, under
`env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp`; RP-7/RP-8 edits only in a disposable clone
`/tmp/d53f-diag` (after `make sync-embedded`), since removed.

| ID | Assertion / command | Observation |
|---|---|---|
| RP-1 | `go test -count=1 -v -run '^(TestA5GoldenCoverage\|TestA5FailureSignals\|TestA5JSONStderr)$' ./internal/oracle/a5/` | 3 PASS, 0.003 s: Linux cannot fail on F2 |
| RP-2 | `go test -count=1 ./internal/oracle/a1/cli/ ./internal/oracle/a5/`; same A1 selection as Task 1 C0 without the new test | `ok` a1/cli 4.2 s, a5 7.6 s (7.8 s wall); 16 PASS lines (5 tests + 11 subtests) |
| RP-3 | `go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a1/cli/...`; `… -pkg ./internal/oracle/a5/...` | 21 `ok` (9 s); 98 `ok` (14 s); no other line |
| RP-4 | `go test -count=20 -run '^(TestSubmitProgressRepeatsWhileQueuedAndRunning\|TestSubmitProgressWithoutAdmissionClass)$' ./internal/ops/`; `go test -count=1 -run '^TestSleepUsageBudget$' -v ./internal/testguard/`; `go test -count=1 -timeout 30m ./internal/ops/` | `ok` 9.9 s (20 s wall, loadavg 2.15); budget PASS; ops `ok` 184.5 s (3 min 5 s wall) |
| RP-5 | `make lint` | gofmt, vet linux/windows/darwin (`Makefile:300-303`), archguard `ok`; 60 s |
| RP-6 | logging `git` wrapper first on the test PATH, then the ten A1 CLI tests | CLI Git calls: 21× `rev-parse --show-toplevel --git-common-dir`, 2× `ls-tree --full-tree -z integration -- :(literal)README.md`; the rest are the fixture's `git init` |
| RP-7 | unchanged tree: `go test -race -cpu 1 -count=40 -run '^TestSubmitProgressRepeatsWhileQueuedAndRunning$' ./internal/ops/`; same with `-cpu 1 -count=100` and no race | race: 2/40 FAIL with the CI line verbatim (`…running canonical validation, batch budget 30s; 0s since…`); no race: 100/100 pass (diagnostic only; no canonical race command, D55) |
| RP-8 | clone: drained `requireQuiet` prototype, `-race -cpu 1 -count=80` of the same test; plus an injected `time.AfterFunc(20ms)` write before `requireQuiet`, `-count=5`; burst probe (128 KiB + `requireQuiet`) on old helper `-count=50`, then on drained helper `-count=50` | 80/80 pass; injected write caught 5/5 with "late-injected"; burst old 41/50 FAIL, drained 50/50 pass |
| RP-9 | `omni-ee get tasks --all --json` filtered (full read: writer audit across 91 unlisted DRAFT_CODE tasks) | non-merged: 91 DRAFT_CODE, 2 CODE_PLANNING, 1 REVIEWING_CODING_PLAN (`arm-1-ar-0-cp-2-d56`), 2 BLOCKED (`arm-1-ar-11-cp-0-code-0`, `arm-1-ar-25-cp-0`); none started; only `arm-1-ar-14-cp-0-code-1`/`-code-2` scopes reach an owned area |

**Budget.** Task 1 ≈ focused 6 s + package 5 s + regen 9 s + lint 60 s ≈ 1.5 min; Task 2 ≈ 1 s + 8 s +
14 s + 60 s ≈ 1.5 min; Task 3 ≈ focused 50 × (≈0.9 s) ≈ 50 s + ops package 185 s + guard 1 s + lint
60 s ≈ 5 min. Deferred commands are excluded. Budgets 1800 s, 1800 s and 3600 s leave more than ten
times headroom for load.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | All three reported signatures commissioned in ≤3 cohesive coding outputs | task done_when | Tasks 1–3 | Covered |
| 2 | Each output has source-grounded RCA and falsifiable acceptance | task done_when; RCA rule | RCA F1–F3; Tasks 1–3 | Covered |
| 3 | A1: D31 disposable Git project resolves on Windows | task done_when; `d53` generic fixes | Task 1 (W1-entry, W1-guard, W1-windows) | Covered |
| 4 | A1: Git-only, provider-free PATH and pinned fixture env kept | `d31`; task done_when | Task 1 (W1-preserve) | Covered |
| 5 | A1: CLI surface coverage and every assertion/golden unchanged | `d31`; `goal-tests` | Task 1 (W1-cases, W1-provenance, W1-preserve) | Covered |
| 6 | A5: both platform-specific diagnostic path comparisons fixed | task done_when; `d53` | Task 2 (W5-signals) | Covered |
| 7 | A5: VP2 state-golden coverage/path lookup fixed | task done_when | Task 2 (W5-coverage, W5-vp2; RCA F2 shows the shared cause) | Covered |
| 8 | A5: scenario coverage, fixtures, golden bytes, asserted values preserved; no skip/constraint | task done_when; `d53` | Task 2 (W5-preserve, W5-provenance) | Covered |
| 9 | ops: deterministic via event synchronization; test-only; no deadline widening, suppression or production change | task done_when; `d41` | Task 3 (Q-order, Q-preserve) | Covered |
| 10 | ops: still verifies repeats while queued/running and no write after return | task done_when | Task 3 (Q-focus, Q-late) | Covered |
| 11 | ops: regression proof for the reported ordering | task done_when | Task 3 (Q-order, Q-red) | Covered |
| 12 | Each output declares TDD/regression proof, sanitized focused repetitions/package proof, applicable lint/test/test-e2e under deferral policy | task done_when; `goal-proof`; `env-safe` | Tasks 1–3 (Acceptance Contracts) | Covered |
| 13 | No per-task race command (D55); sleep budget 16 kept | `d55`; `d46` | Tasks 1–3; Task 3 (Q-sleep) | Covered |
| 14 | Outputs merge before the A1 boundary closes G8; boundary rebases, reruns G1–G7, awaits one D52 run at that SHA green on three OSes; old evidence and run 40115874515 do not close G8 | `d53` Order; `d52`; `boundary` | Placement | Covered |
| 15 | No dependency on the boundary consumer; no backward edge | task done_when | Placement; Dependencies | Covered |
| 16 | D24/E5 regeneration and traceability preserved | `d24`; `goal-proof` E5 | Tasks 1–2 (regen check), Task 3 (no oracle file) | Covered |
| 17 | Any genuine need to change frozen production behaviour reported unresolved | task done_when; `goal-frozen` | None found (RCA a–e: all causes are in tests); Common Rules forbid production edits | Covered |
| 18 | No duplication of delivered D53/timing outputs; no interference with D56 | task scope | Placement (Independent) | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: no product behaviour changes; test-only edits (Task 3 keeps the deferred `make test-e2e` of the timing lineage) | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: no user-visible or documented behaviour changes; this plan and the manifests are the records | N/A |

## Output

Three entries, output[i] = Task i+1, none with a dependency. Each `plan_ref` anchors that task's
section, whose Acceptance Contract matches the entry's ordered validation and prerequisites. `desc`,
`done_when` and `scope` are short projections of the task sections. This session writes only the two
planning artifacts.
