# Code Plan: D49 Portability Correction — A3 Lifecycle Git Transcript and Windows Git Launch

## Source References

Source revision: "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"

### Direct References

- "d53": "specs/goals/20261023-architecture-consolidation-decisions.md#D53 — Oracle portability for N2: generic fixes everywhere, process-supervision oracles Linux-only (human, 2026-10-24)" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "d55": "specs/goals/20261023-architecture-consolidation-decisions.md#D55 — E1 revised for new code plans: no per-task `go test -race` (human, 2026-10-24)" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "d57": "specs/goals/20261023-architecture-consolidation-decisions.md#D57 — D53 extended: the A1 CLI oracle (D31 git-only PATH) is excluded on Windows (run, 2026-10-24)" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "d58": "specs/goals/20261023-architecture-consolidation-decisions.md#D58 — A2 runs in parallel with the A3–A7 chain (human, 2026-10-24)" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "d59": "specs/goals/20261023-architecture-consolidation-decisions.md#D59 — G8 at a boundary SHA behind the branch head: dispatch on a pinned ref (operator, standing delegation, 2026-10-25)" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "d31": "specs/goals/20261023-architecture-consolidation-decisions.md#D31 — A1 CLI oracle cases run in a disposable Git project with a git-only PATH (run, 2026-10-23)" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "d49-plan": "specs/plans/20261023-architecture-consolidation/20261024-231041-arm-1-ar-12-cp-2-d49-correction.md#Task 1: Separate blocked episode concerns within ops, keeping inherited K1 sites in place" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "goal-frozen": "specs/goals/20261023-architecture-consolidation.md#Frozen Surfaces" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "goal-tests": "specs/goals/20261023-architecture-consolidation.md#Test Policy and Sequencing" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "goal-proof": "specs/goals/20261023-architecture-consolidation.md#Proof Environment" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "a3-oracle-constraints": "specs/arch-plan/20261023-architecture-consolidation/20261023-183344-arm-1-ar-3.md#Constraints" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "a3-proof": "specs/arch-plan/20261023-architecture-consolidation/20261023-203240-arm-1-ar-12.md#Proof and Shared Ownership" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "adr-0219": "specs/architecture/ADR/0219-oracle-harness-and-chained-provenance.md#Decision" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "proof-scope": "internal/testhelpers/oracle/proof-scope.md#Oracle proof scope" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"
- "guard-tier1": "GUARDRAILS.md#Tier 1 (Hard Constraints)" @ "c53aaf99843775e8892f4f5fcbf69fd153c4bdc4"

### Obligation Coverage

- "AC-mask" -> "d53", "goal-tests", "goal-proof", "goal-frozen"
- "AC-launch" -> "d53", "d57", "d31", "d49-plan", "goal-proof"
- "AC-env" -> "goal-proof", "guard-tier1", "d31"
- "AC-regress" -> "goal-tests", "d53"
- "AC-oracle" -> "goal-tests", "goal-proof", "adr-0219", "a3-oracle-constraints"
- "AC-platform" -> "d53", "d49-plan", "d59", "proof-scope", "a3-oracle-constraints"
- "AC-proof" -> "goal-proof", "a3-proof", "d55", "guard-tier1"
- "AC-placement" -> "a3-proof", "d58", "d59"

## Correction Authority and Boundary

Correction (`planning_change.kind = correction`) of merged `arm-1-ar-12-cp-2-d49-correction`,
whose sole coding output `arm-1-ar-12-cp-2-d49-correction-code-0` (merge `1270bdf47`, oracle
commit `41b9c3bb4`) added `TestOracleAssessBlockedVariants` and `TestOracleUnblockTaskVariants`
as portable scenarios (`d49-plan`, platform-scope paragraph). Trigger: operator evidence
2026-10-25T04:53:59Z, G8 run 40162651273 at `b3356c3c6`, items 1-2. Item 3 (the `internal/agent`
Windows timing test) is out of scope by operator instruction. This plan supersedes nothing and
amends no merged carrier; no new architectural decision is made: the edits are harness
portability inside `internal/oracle/a3/lifecycle` under `d53`'s generic-fix ruling and the goal's
determinism-only oracle exception (`goal-tests`, `goal-proof` E5).

Planner output: this plan and its output JSON only. Doc Impact: this plan, the child's acceptance
manifest, and one `proof-scope` row (Task 1). Test Impact: Task 1 regressions R1-R5 below.

## Root Cause Analysis

### a. Evidence

- CI (reporter-attributed, then read by this planner from the job logs with
  `gh run view 40162651273 -R liza-mas/liza --job <id> --log`):
  - Linux job 121077253917: `case_test.go:149: oracle: golden testdata/golden/state/unblock-task/rebase-on-refusal-conflict.txt (kind state) differs at line 132`; golden and actual share the
    displayed 120-character prefix ending `stderr=rebase conflict: Rebasing (1/1)\rAuto-merging READ`. Runner Git `2.55.0` (log line 65). macOS job 121077253876: same subtest fails; Git `2.56.0`.
    Every other lifecycle scenario passes on both.
  - Windows rest-2 job 121077253709 (log line 454 and following, 102 `--- FAIL` lines): every
    variant of both drivers fails at `case_test.go:71` with
    `start git: exec: "…\001\bin\git": executable file not found in %PATH%`; Git `2.56.0.windows.2`.
- Golden: `testdata/golden/state/unblock-task/rebase-on-refusal-conflict.txt` carries the Git
  rebase transcript twice: line 132 (`repair_request.evidence[0]`, after `stderr=rebase conflict: `)
  and line 157 (`stdout_stderr_excerpt`). No other A3 lifecycle golden contains `hint:`,
  `Auto-merging`, `CONFLICT` or `Rebasing` (RP4). Local Git is `2.43.0`; the focused subtests pass
  locally (RP5), so the golden was produced with Git 2.43.
- Producer path: `internal/git/rebase.go:63-71` returns `RebaseConflictError{Output: <combined Git
  output>}` when the output contains `CONFLICT` or `could not apply`; `Error()` is
  `"rebase conflict: " + Output` (`rebase.go:15-16`). `internal/ops/unblock_task.go:440` takes
  `excerpt := truncateForDiagnostics(cause.Error(), 2000)`, stores it as `stdout_stderr_excerpt`
  (`:448`) and formats evidence `command=… target_ref=… target_sha=… exit_code=1 stderr=<excerpt>`
  (`:473`).
- Harness launch: `env_test.go:39-44` symlinks the host Git as `<root>/bin/git` and sets
  `PATH=<root>/bin` (`:47`); `fixture_test.go:42-48` launches `filepath.Join(r.path, "git")`
  through `environment.command` (`env_test.go:59-75`, `t.Fatalf("start %s: %v")` at `:73`);
  `case_test.go:71` calls `buildFixture`, whose first act is `f.git(t, "init", …)`
  (`fixture_test.go:58`). Go's Windows lookup (`/usr/local/go/src/os/exec/lp_windows.go`, Go 1.26.0):
  a name containing a separator goes to `findExecutable` (`:132-138`), which accepts only `file`
  with an extension or `file+ext` for PATHEXT extensions, default `.com .exe .bat .cmd`
  (`:37-56`, `:112-128`), else `ErrNotFound` = `executable file not found in %PATH%` (`:16`).
- Scope history: every other driver in the package calls `linuxOnly` (`env_test.go:18-23`, OC3
  in `a3-oracle-constraints`); `41b9c3bb4` added the first two drivers without it, as `d49-plan`
  requires. `env_test.go` is unchanged since `9c1d7dd70`; no file in scope changed between
  `b3356c3c6` and `c53aaf998` (RP7).

### b. Mechanism

1. Linux/macOS: the state golden freezes `RebaseConflictError.Output`, which is Git's own
   rebase transcript (progress, merge notice, conflict line, hints, final line). Its wording is
   Git-version dependent. Running the same fixture with Git 2.55/2.56 produces different bytes
   after the common prefix, so `oracle.CheckState` (`case_test.go:149`) fails. Every
   production-owned field of the line (`command=`, `target_ref=`, `target_sha=`, `exit_code=1`,
   `stderr=rebase conflict: `) lies inside the identical prefix; the differing bytes are Git
   output. Line 157 freezes the same output and would fail next.
2. Windows: the harness executes the extensionless link `<root>\bin\git`; Windows lookup rejects
   it before any process starts, so every case fails during fixture setup. The CLI under test
   resolves `git` in the same `PATH` by the same rule (`lookPath` → `findExecutable`), so an
   extensionless entry would also fail there, and the fixture environment lacks the Windows names
   `USERPROFILE`, `TMP`, `TEMP` that `oracle.ChildEnv` supplies (`internal/testhelpers/oracle/env.go:22-50`).

### c. Reproduction

- R-L: `TestOracleUnblockTaskVariants/rebase-on-refusal-conflict/json` with a Git whose rebase
  transcript differs from 2.43.0's (CI Git 2.55.0/2.56.0). Locally reproducible without another
  Git by applying the state comparison to a transcript variant (R4).
- R-W: any variant of `TestOracleAssessBlockedVariants` or `TestOracleUnblockTaskVariants` on
  Windows: `buildFixture` → `f.git(t, "init", …)` → `exec` of extensionless `bin\git`. Locally
  observable through the launch layout `newEnvironment` produces for `goos == "windows"` (R1).

### d. Verification scope

Checked: the source lines above, the three job logs, the golden lines, Go 1.26 stdlib lookup,
local passes with Git 2.43, Windows compilation of the package (RP6). Not checked:
the exact Git 2.55/2.56 bytes after the 120-character prefix (log truncation; no such Git is
installed locally, and installing one is out of scope); any Windows behaviour after process
launch, because no Windows host is available and dispatch is out of scope; the D57 A1 cause,
which D57 itself records as unproven.

### e. Sufficiency

- Linux/macOS: masking the Git-produced remainder in both occurrences removes every Git-version
  dependent byte from this golden; no other lifecycle golden freezes Git transcript text (RP4).
  The failure becomes impossible for this cause.
- Windows: an executable PATH entry carrying `.exe` removes the observed launch failure for the
  harness and the CLI. Residual, unverified: (1) the A1 symptom D57 recorded with a `git.exe`
  symlink ("failed to detect project root"); the shim (Task 1) runs Git from its installed path,
  which avoids D57's stated plausible cause but not an unknown one; (2) Windows-only divergence
  after launch in the 17 scenarios (path separators or escaping against the path masks, process
  liveness of `<live-pid>`). Native proof is the next G8 (`d59`). If it shows a further Windows
  divergence, the `d49-plan` rule applies: report with evidence; no skip or build constraint.

### f. Adjacent findings (recorded, not planned)

- `proof-scope` lists `a3/lifecycle` as Linux-only through `env_test.go` without the two
  portable drivers merged by `41b9c3bb4`; Task 1 corrects that row because it touches the same
  platform statement.
- Windows rest-1 `TestExecutionDeadlineMarkerPastItsDeadlineWithLiveProcessIsInterrupted`:
  operator classifies it as an intermittent timing flake; no action.

## Task 1: Make the A3 lifecycle portable variants independent of the Git version and launchable on Windows

Priority: Must. One intent: the two portable lifecycle drivers produce the same verdicts on
Linux, macOS and Windows hosts with any supported Git, with no assertion lost.

### Owned files

| File | Change |
|---|---|
| `internal/oracle/a3/lifecycle/env_test.go` | `newEnvironment` builds the environment with `oracle.ChildEnv(oracle.EnvBase{Home, Path, Tmp}, <existing extras in existing order>)`; Git PATH entry chosen by one GOOS-parameterised layout function (below) |
| `internal/oracle/a3/lifecycle/fixture_test.go` | `environment.git` launches the layout's PATH entry instead of the literal `bin/git` |
| `internal/oracle/a3/lifecycle/case_test.go`, `mask_test.go` | Opt-in `lifecycleCase` field (precedent: `maskBoundaries`, `case_test.go:42-45`) applying the Git-transcript normaliser to the state comparison of that case only |
| `internal/oracle/a3/lifecycle/unblock_task_variants_test.go` | Opt-in set on `rebase-on-refusal-conflict` only |
| `internal/oracle/a3/lifecycle/testdata/gitshim/main.go` (new) | Stdlib-only forwarding shim |
| New `internal/oracle/a3/lifecycle/*_test.go` | Regressions R1-R5 |
| `testdata/golden/state/unblock-task/rebase-on-refusal-conflict.txt` | Lines 132 and 157 only (determinism edit) |
| `internal/testhelpers/oracle/proof-scope.md` | `a3/lifecycle` row and "Runs on all three platforms" entry name the two portable drivers |
| `specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-12-cp-2-d49-portability-lifecycle.json` (new) | Acceptance manifest |

Not edited: production code; `conventions_test.go` (`TestLifecycleEnvironment` keeps asserting
the Linux layout: git-only symlink, same names); any other driver, fixture or golden;
`internal/testhelpers/oracle/*.go`; `internal/oracle/PROVENANCE` (the package has no C record;
determinism records are required only after the first C record, `adr-0219` item 4); `.github/`.

### Change 1: Git-transcript determinism (AC-mask)

- Normaliser: inside a YAML scalar that begins with the production text
  `stderr=rebase conflict: ` (evidence) or `rebase conflict: ` (`stdout_stderr_excerpt`), replace
  the Git-produced remainder up to the scalar's closing quote with one named token (for example
  `<git-rebase-conflict-output>`). Production-owned text before it stays byte-exact.
- Before masking, the driver asserts on the raw remainder only what production classification
  implies plus the fixture's conflicted path: it contains `CONFLICT` or `could not apply`
  (`rebase.go:69-70`) and names `README` (the `integration-conflict` recipe,
  `fixture_test.go:125-129`). No other Git wording is asserted. Failure messages name the
  missing fact, not Git output.
- Applied only to the opted-in case; every other case and the JSON/human goldens stay
  digit-for-digit. The JSON golden of this case contains no Git transcript (RP4).
- Golden: rewrite with the harness write mode (`ORACLE_WRITE=1`, `golden.go:15,80-86`) for this
  subtest only; the diff is confined to the transcript remainders on lines 132 and 157.

### Change 2: portable Git launch (AC-launch, AC-env)

- Layout function `gitLayout(goos)` (name free) used by `newEnvironment`: Linux and macOS keep
  the symlink named `git` exactly as today; Windows installs `git.exe`, a copy or hard link of
  the shim built once per test binary with `oracle.BuildCLI(t, "./internal/oracle/a3/lifecycle/testdata/gitshim")` (`cli.go:54-77`; builds `.exe` on Windows, Go child env with
  `-buildvcs=false`). No `.cmd`/`.bat` shim (`d57`).
- Shim: reads the absolute host Git path (resolved by the parent with `exec.LookPath("git")`, as
  today) from a sidecar file outside the PATH directory, located relative to `os.Executable()`;
  runs it with `os.Args[1:]`, its own stdin/stdout/stderr, working directory and environment
  unchanged; exits with Git's exit code. It refuses with a fixed message if the sidecar is
  missing or not absolute. It never consults `PATH` and runs no other program.
- PATH directory keeps exactly one entry (`git`, or `git.exe` on Windows); `environment.git`
  launches that entry, so harness and CLI reach Git the same way.
- Environment: `oracle.ChildEnv` gives HOME, PATH, TMPDIR on every platform (unchanged on
  Linux/macOS, so `TestLifecycleEnvironment` holds) and on Windows adds USERPROFILE, TMP, TEMP
  set to the fixture dirs, plus SYSTEMROOT. SYSTEMROOT is the one host value: `os/exec` appends
  it on Windows whatever the caller passes (`/usr/local/go/src/os/exec/exec.go:1311-1328`), and
  ChildEnv names it to keep the set exact (`env.go:22-27`). No other host variable reaches a child.
- No `linuxOnly`, build constraint, `TestMain` guard or Windows skip is added to either driver.

### Regressions (AC-regress)

All assert names/booleans only (`guard-tier1` G1.4). Three evidence classes, each recorded in
the manifest with its executed observation on Linux:

- Defect regressions, red on the pre-correction harness: R1 (trigger: the pre-correction
  Windows layout, extensionless `git`, fed to the R1 assertion) and R4 (trigger: a Git-only
  transcript variant compared without the normaliser). The manifest records the red run
  (test name, failing assertion) against the pre-correction code and the green run after.
- Fidelity and composition controls for the new shim: R2 and R3. The pre-correction harness has
  no shim, so no red run is claimed; their negative controls (a defective shim variant) must be
  detected, and the manifest records that detection.
- Preservation proof: R5. It passes before and after on Linux, where the environment is
  unchanged; the manifest records both green runs. Its Windows names are exercised natively
  only by the next G8 (`d59`); no Linux failure is manufactured.

- R1 layout (all platforms, pure): for `goos == "windows"` the layout's PATH entry carries an
  extension of the default PATHEXT set and is an executable, not a `.cmd`/`.bat`; for linux and
  darwin it is the `git` symlink. The pre-correction layout (`git`, no extension) fails.
- R2 shim fidelity (host): build the shim, install it as the fixture PATH Git, compare with
  direct host Git: argv round-trip via `git rev-parse --sq-quote` over arguments with spaces,
  quotes, backslashes and an empty argument; stdin (`git hash-object --stdin`); non-zero exit and
  stderr (`git rev-parse --verify` of a missing ref); working directory (`--show-toplevel`);
  environment (`git var GIT_COMMITTER_IDENT` shows the fixture identity and date). Negative
  controls: a shim variant dropping an argument or the exit code is detected.
- R3 scenario equivalence (host): `rebase-on-refusal-conflict` and one assess case run with the
  shim layout forced on the host and match the committed goldens in compare mode. The forcing
  seam is per case or per fixture, never a package-level mutable switch.
- R4 normaliser (pure): the committed 2.43 lines mask to the token form; Git-only variants
  (changed hint wording, no `Rebasing (1/1)\r` progress, an extra or missing final
  `Could not apply` line) mask identically; negative controls: any change to `command=`,
  `target_ref=`, `target_sha=`, `exit_code=`, the `rebase conflict: ` prefix, or removing
  `CONFLICT`/`could not apply` or `README` from the raw remainder, is detected; a case without
  the opt-in keeps its raw text. Without the normaliser the variants differ.
- R5 environment (all platforms): names equal ChildEnv's policy names for `runtime.GOOS` plus the
  existing extras and caller names; HOME/USERPROFILE and TMPDIR/TMP/TEMP equal the fixture
  dirs (booleans); the PATH directory holds exactly one entry.

### Oracle exception and provenance (AC-oracle)

The golden edit is a determinism-only edit (`goal-tests`, `goal-proof` E5): it removes
host-Git-version bytes and changes no production-owned asserted value; R4 and the retained
classification facts keep the semantic assertion. Regeneration: the harness write mode for the
subtest, then `regen -mode check -pkg ./internal/oracle/a3/lifecycle` (H1 reproduction against
starting-tree production with the working drivers, `regen/compare.go:15-115`). The manifest
records the golden diff, CI run 40162651273 lines (variation evidence) and the check output.

### Platform statement (AC-platform)

Both drivers stay portable (`d49-plan`). `proof-scope` is updated: the `a3/lifecycle`
Linux-only row names its two exceptions, and "Runs on all three platforms" lists them. Native
Windows/macOS proof is the next G8 (`d59`), outside this task.

**Done when:** on Linux with Git 2.43, commands 0-4 below pass; the two drivers carry no
platform scoping; R1-R5 pass; R1 and R4 have manifest-recorded red runs on the pre-correction
harness, R2/R3 negative controls are detected, and R5 is green before and after; the golden
diff touches only the transcript remainders of lines 132 and 157; no production, other driver,
fixture, golden, PROVENANCE or harness code file changes.

### Canonical validation and manifest allocation

Run from the coding worktree root, serially; no `-race` (`d55`).

- 0: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 ./internal/oracle/a3/lifecycle/`
- 1: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make test`
- 2: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make lint`
- 3: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make test-e2e`
- 4: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a3/lifecycle`

Mappings: AC-mask → R4 and the rebase conflict subtest (0); AC-launch → R1, R2, R3 (0) and
Windows vet in lint (2); AC-env → R5 and `TestLifecycleEnvironment` (0); AC-regress → R1-R5 (0);
AC-oracle → regen check (4); AC-proof → (1), (2), (3). `make lint` includes `GOOS=windows go vet ./...` (RP8).

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-12-cp-2-d49-portability-lifecycle.json",
  "obligations": ["AC-mask", "AC-launch", "AC-env", "AC-regress", "AC-oracle", "AC-platform", "AC-proof", "AC-placement"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/oracle/a3/lifecycle/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-e2e",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a3/lifecycle"
  ],
  "timeout_seconds": 7200,
  "approved_proofs": [
    {"obligation_id": "AC-oracle", "reference_id": "goal-tests", "rationale": "The reviewer verifies the determinism-only exception: the golden diff is confined to the Git-produced remainders of lines 132 and 157, production-owned text is unchanged, and the before/after evidence and regeneration output are in the manifest."},
    {"obligation_id": "AC-platform", "reference_id": "d59", "rationale": "Native Windows and macOS execution is G8 evidence at the next boundary run; no agent host runs Windows. Review checks that neither driver gained a skip, build constraint or TestMain guard and that proof-scope matches the source."},
    {"obligation_id": "AC-placement", "reference_id": "a3-proof", "rationale": "Waits and the absence of any A2 or A2-boundary edge are graph facts inspected in the generated task, not test outcomes."}
  ]
}
```

## Dependencies and Shared-File Ownership

| Output | Waits on | Earliest sufficient artifact | Priority |
|---|---|---|---|
| 0 (Task 1) | `inherit_inputs` selected: `arm-1-ar-12-cp-2-d49-correction` output 0 (= `arm-1-ar-12-cp-2-d49-correction-code-0`, MERGED) | the merged defective drivers, fixture recipe and golden this task corrects | Must |

The lifecycle harness producers `arm-1-ar-3-cp-0-code-0/-1` are MERGED and upstream of that
child (RP9). No `task_depends_on`, no `depends_on` (single output). No wait on
`arm-1-ar-12-cp-0-code-0` or any A2 task (`d58`); no Should/Could target is reachable.

| File or definition | Writers | Ordering |
|---|---|---|
| `internal/oracle/a3/lifecycle/**` | `arm-1-ar-3-cp-0-code-*`, `arm-1-ar-12-cp-2-d49-correction-code-0` (MERGED) → Task 1 | Task 1 is the only open writer (RP9) |
| `internal/testhelpers/oracle/proof-scope.md` | MERGED D53/D57 correction children → Task 1 | Only open writer; one row and one list entry |
| Acceptance manifest | Task 1 | Unique name |

`arm-1-ar-12-cp-4-code-2` (IMPLEMENTING_CODE) owns A3 production files, colocated tests and an
additive `a3/control` driver; `-code-3` (DRAFT_CODE) owns one new ADR. Neither declares a
lifecycle oracle or `proof-scope` path, so the edits are disjoint and commute. Task 1 edits no
ratchet literal or shared reference document of the A3 chain (`a3-proof`).

## Placement

Rule: `a3-proof` "Correction placement".

- **Predecessors:** the merged writers above, satisfied through the selected inherited output.
  No started writer of the owned files exists.
- **Successors:** the only consumer whose completion needs this correction is the started,
  BLOCKED A2 boundary `arm-1-ar-12-cp-0-code-0`. The operator ruling of 2026-10-25T04:53:59Z
  already settles that slot: the boundary stays held, then the operator releases it to rebase,
  rerun G1-G7 at a new SHA and request G8 (`d59`). It is not a graph edge (`d58`); no unstarted
  A3 writer needs the correction.
- **Slot:** earliest, on current integration (`c53aaf998` or later), parallel to the A3 chain.

## Reality Probes

At `c53aaf99843775e8892f4f5fcbf69fd153c4bdc4`, 2026-10-25T05:00Z–05:12Z UTC; commands under
`env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` where Go runs.

| ID | Assertion / command | Observation |
|---|---|---|
| RP1 | `gh run view 40162651273 -R liza-mas/liza --json jobs`; `--job <id> --log` for 121077253917, 121077253876, 121077253709 | Failures as in RCA a; Git 2.55.0 (Linux), 2.56.0 (macOS), 2.56.0.windows.2 (Windows); macOS and Linux fail only `rebase-on-refusal-conflict/json` |
| RP2 | `nl -ba /usr/local/go/src/os/exec/lp_windows.go`, `exec.go:1311-1328` | Extension rule and SYSTEMROOT append as cited |
| RP3 | `nl -ba` `env_test.go`, `fixture_test.go`, `case_test.go`, `unblock_task_variants_test.go`, `blocked_episode_variants_test.go`, `conventions_test.go:19-55`; `grep -n linuxOnly` | Launch path and env as cited; all other drivers call `linuxOnly`; `TestLifecycleEnvironment` asserts Linux layout under `linuxOnly` |
| RP4 | `grep -rn -e CONFLICT -e Auto-merging -e 'hint:' -e Rebasing` over the lifecycle goldens | Only the state golden, lines 132 and 157; JSON/human goldens of the case: 0 hits |
| RP5 | `go test -count=1 -run 'TestOracleUnblockTaskVariants/rebase-on-refusal-conflict\|TestOracleAssessBlockedVariants' ./internal/oracle/a3/lifecycle/` | `ok … 6.573s` with Git 2.43.0 |
| RP6 | `GOOS=windows go vet ./internal/oracle/a3/lifecycle/` | Exit 0 |
| RP7 | `git diff --stat b3356c3c6 HEAD -- internal/oracle/a3/lifecycle internal/testhelpers/oracle/env.go internal/git/rebase.go`; `git merge-base --is-ancestor 1270bdf47 b3356c3c6` | Empty diff; merge is an ancestor of the boundary SHA; `env_test.go` last changed in `9c1d7dd70` |
| RP8 | `make -n test lint test-e2e`; `go run ./internal/testhelpers/oracle/cmd/regen --help` | `go test ./...`, three-OS `go vet` incl. `GOOS=windows`, archguard, e2e `-race` selection; regen flags `-mode` check/write-new, `-pkg`, `-repo` |
| RP9 | `omni-ee get <id> --json` for `arm-1-ar-3-cp-0-code-0/-1`, `arm-1-ar-12-cp-2-d49-correction-code-0`, `arm-1-ar-12-cp-4-code-2/-3`, `arm-1-ar-12-cp-0-code-0` | MERGED ×3; IMPLEMENTING_CODE and DRAFT_CODE with scopes as stated; A2 boundary BLOCKED |
| RP10 | Scratch module outside the repo: `GOOS=windows GOFLAGS=-buildvcs=false go build -o git.exe ./testdata/shim` | Exit 0: an explicitly named `testdata` main package builds for Windows (deleted after) |
| RP11 | `grep -n WriteEnv internal/testhelpers/oracle/golden.go`; `cat internal/oracle/PROVENANCE` | `ORACLE_WRITE`; ledger is the single `start` line (no C record) |

PROSPECTIVE (Task 1): the normaliser, layout function, shim and R1-R5. Windows behaviour after
launch is UNPROBED (no Windows host; dispatch out of scope); it is held on the next G8 (`d59`),
which the A2 boundary already awaits by operator ruling.

**Budget (estimate):** 7200 s: focused package 120, `make test` 1800, lint 600, e2e 600, regen
check 1200, headroom.

## Spec Compliance Matrix

| # | Requirement (effective priority) | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Must: fix the cross-platform golden difference as a determinism issue, not by scoping | d53 generic rulings | Task 1 (Change 1) | Covered |
| 2 | Must: oracle edits only under the determinism-only exception, reviewer-verified, with provenance | goal-tests; goal-proof E5; adr-0219 | Task 1 (AC-oracle) | Covered |
| 3 | Must: frozen surfaces and production behaviour unchanged | goal-frozen | Task 1 (Not edited) | Covered |
| 4 | Must: the portable drivers run on all three platforms; no skip or build constraint | d53 Linux scope; d49-plan platform paragraph | Task 1 (Change 2, AC-platform) | Covered |
| 5 | Must: no `.cmd` shim or launch-capture cycle; A1 exclusion not extended | d57 | Task 1 (Change 2) | Covered |
| 6 | Must: disposable Git, git-only provider-free PATH, pinned Git identity | d31; goal-proof E2 | Task 1 (Change 2, R5) | Covered |
| 7 | Must: environment-safe tests, env -i proof, names-only diagnostics | guard-tier1 G1.4; goal-proof E4 | Task 1 (R1-R5, validation) | Covered |
| 8 | Must: Linux-only scope of other lifecycle drivers unchanged | a3-oracle-constraints OC3 | Task 1 (Not edited) | Covered |
| 9 | Must: E1 proof without per-task race | goal-proof E1; a3-proof; d55 | Task 1 (validation) | Covered |
| 10 | Must: correction placement; no A2 edge; G8 at pinned ref by the A2 boundary | a3-proof; d58; d59 | Dependencies; Placement | Covered |
| 11 | Must: proof-scope matches source | proof-scope | Task 1 (AC-platform) | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: test-harness portability, no product behaviour change; command 3 keeps the e2e regression | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | Task 1: `proof-scope` row; no user-visible behaviour change | Covered |

## Output

One entry, output[0] = Task 1: `inherit_inputs` selects `arm-1-ar-12-cp-2-d49-correction`
output 0; `plan_ref` the Task 1 heading; `spec_ref` `d53`; validation and prerequisites equal the
Acceptance Contract. `desc`, `done_when` and `scope` are short projections of Task 1 (intent,
completion observation, owned boundary); all mechanism and regression detail stays here.
