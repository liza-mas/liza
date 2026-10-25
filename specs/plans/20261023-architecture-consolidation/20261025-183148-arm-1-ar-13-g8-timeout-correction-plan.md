# Code Plan: A4 G8 Correction — Explicit Test Budget for the macOS Compatibility Lane

## Source References

Source revision: "1e7807b0688691feef5369ff8d06953cfc8c174e"

### Direct References

- "goal-nfr": "specs/goals/20261023-architecture-consolidation.md#Non-Functional Constraints" @ "1e7807b0688691feef5369ff8d06953cfc8c174e"
- "goal-proof": "specs/goals/20261023-architecture-consolidation.md#Proof Environment" @ "1e7807b0688691feef5369ff8d06953cfc8c174e"
- "goal-tests": "specs/goals/20261023-architecture-consolidation.md#Test Policy and Sequencing" @ "1e7807b0688691feef5369ff8d06953cfc8c174e"
- "goal-success": "specs/goals/20261023-architecture-consolidation.md#Success Criteria" @ "1e7807b0688691feef5369ff8d06953cfc8c174e"
- "goal-failure": "specs/goals/20261023-architecture-consolidation.md#Failure Modes" @ "1e7807b0688691feef5369ff8d06953cfc8c174e"
- "d52": "specs/goals/20261023-architecture-consolidation-decisions.md#D52 — G8 boundary CI by `workflow_dispatch` on the pushed integration branch; one PR to `main` later (human, 2026-10-24)" @ "1e7807b0688691feef5369ff8d06953cfc8c174e"
- "d55": "specs/goals/20261023-architecture-consolidation-decisions.md#D55 — E1 revised for new code plans: no per-task `go test -race` (human, 2026-10-24)" @ "1e7807b0688691feef5369ff8d06953cfc8c174e"
- "d59": "specs/goals/20261023-architecture-consolidation-decisions.md#D59 — G8 at a boundary SHA behind the branch head: dispatch on a pinned ref (operator, standing delegation, 2026-10-25)" @ "1e7807b0688691feef5369ff8d06953cfc8c174e"
- "d60": "specs/goals/20261023-architecture-consolidation-decisions.md#D60 — G8 may be green across failed-job reruns of one run when the reruns cover pre-existing intermittent tests (human, 2026-10-25)" @ "1e7807b0688691feef5369ff8d06953cfc8c174e"
- "a4-proof": "specs/arch-plan/20261023-architecture-consolidation/20261023-205627-arm-1-ar-13.md#Proof and Shared Ownership" @ "1e7807b0688691feef5369ff8d06953cfc8c174e"
- "guard-tier1": "GUARDRAILS.md#Tier 1 (Hard Constraints)" @ "1e7807b0688691feef5369ff8d06953cfc8c174e"

### Obligation Coverage

- "AC-budget" -> "goal-failure", "goal-nfr", "d52", "d60"
- "AC-preserve" -> "goal-failure", "goal-tests", "d60", "guard-tier1"
- "AC-lint" -> "goal-proof", "goal-success"
- "AC-disproof" -> "d60", "goal-failure"
- "AC-proof" -> "goal-proof", "goal-success", "d55"
- "AC-placement" -> "a4-proof", "d52", "d59"

## Correction Authority and Boundary

Maintainer-commissioned CI correction for the A4 boundary (`goal-failure`: "CI is red at an area
boundary — … the maintainer opens the fix task"; `d52`: "Red CI keeps the next area held, and the
maintainer commissions the fix (E1 unchanged)"; `d60`: a failure in code changed on the branch is
never rerun away). Trigger: operator evidence and commission 2026-10-25T18:20:03Z, G8 run
40219462697 at `1e7807b06`, item 1. Item 2 (ACP integration timeout) is not corrected here
(Adjacent findings).

This plan supersedes nothing, amends no merged carrier and makes no architectural decision. The
A4 boundary task `arm-1-ar-14-cp-0-code-0` stays valid and held; its scope excludes `Makefile`, so
no scope exception or unblock/rebase of it can carry this edit. `a4-proof` says each A4 concern
leaves "goal, contracts, skills, Makefile and overview" unedited; that bounds A4 Scopes 1-6. This
correction is outside those scopes, opened under `goal-failure`/`d52`, and edits no A4 concern file.
`guard-tier1` G1.1: `Makefile` is this repository's own CI build, not runtime behaviour in user
projects.

Planner output: this plan and its output JSON only. Doc Impact (child): none — `Makefile:361-364`
help text and `.github/workflows/README.md:11-14` name the targets, not their flags (RP10).
Test Impact (child): none; non-behavioural build configuration, `tdd_not_required` (Task 1).

## Root Cause Analysis

### a. Evidence

- CI, read by this planner from the attempt-1 macOS job log
  (`gh api repos/liza-mas/liza/actions/jobs/121247667497/logs`, job "Shared CI / macOS
  compatibility", 18:04:04Z-18:17:52Z, Go `go1.25.5 darwin/arm64`, log line 199):
  - line 368-378: `make ci-test-compat-nosync` with `GOFLAGS: -count=1` expands to
    `go test $(go list ./... | grep -v …)` and `go test -race -tags e2e -timeout 30m -count=1 ./internal/integration/`, run concurrently.
  - line 2935-2939: `panic: test timed out after 10m0s` / `running tests:` lists exactly three
    subtests, each `(0s)`: `TestRejectionRCAGateBackstopsRemainActive/planning_review_churn_…`,
    `…/review_budget_escalates_at_its_cap_after_a_resume`,
    `TestRejectionRCAGateEndToEnd/coding_task_product_defect_->_implementation_correction`.
  - line 8752: `FAIL github.com/liza-mas/liza/internal/ops 600.094s`; every other unit package
    reports `ok` (lines 379-404, 8753-8794).
  - line 1568/1849/2118: `test-integration-nosync` fails on
    `TestCodexACPProviderUnavailableLeavesUnrelatedFailureOnCrashPath` (`provider_unavailable_acp_test.go:220: … context deadline exceeded`, 20.12s), package 556.262s; make stops the
    `ci-test-slow-nosync` chain, so `./cmd/liza/` did not run in this attempt.
- Duration trend, read by this planner (`internal/ops` line of each job log, attempt 1): macOS
  446.773s (A1, run 40132329230), 500.995s (A2, run 40162651273), >600s (A4); Linux
  331.799s, 344.256s, 353.390s (A4 job `Shared CI / Linux tests`). Operator-attributed: macOS
  481s (A2) and 488s (A3) from runs not read here.
- Test growth: top-level `func Test…` in `internal/ops/*_test.go`: 2000 at `origin/main`
  (`6a01d55d3`), 2098 at `1e7807b06`; 45 branch commits touch `internal/ops` (RP12).
- Source: `Makefile:142-143` `ci-test-compat-nosync` runs `-j2 test-units-nosync ci-test-slow-nosync`;
  `Makefile:154-155` `go test ./cmd/liza/` and `Makefile:157-158` `go test $(CI_UNIT_PACKAGES)` carry
  no `-timeout`; `Makefile:152,161,164` (race and integration recipes) carry `-timeout 30m`.
  `ci-core.yml:228-230` runs this target in the macOS job (`timeout-minutes: 30`, `:176`); no
  other workflow or target invokes the two recipes (RP10). Both recipes came from `main`
  (`8e117e1fa`, "ci: share and optimize workflow execution (#99)").
- `go help testflag` (Go 1.26.0 locally): `-timeout d … The default is 10 minutes (10m).`

### b. Mechanism

`test-units-nosync` runs `go test` over every package except `internal/integration` and
`cmd/liza` with no `-timeout`, so each package test binary panics at Go's 10-minute default. On
macOS, all `internal/ops` tests run inside one test binary, beside the race-instrumented
integration package (`-j2`). Its run time crossed 600 s at A4 as the branch added ops tests (2000 →
2098) on a slower runner. The alarm fired while three short subtests were running, so no single
test was hung. The Linux lane runs the same package under `-timeout 30m` (`Makefile:161`) and
passed in 353 s.

### c. Reproduction

The defect is a missing flag in a recipe; its observable state is the recipe expansion.
At `1e7807b06`: `make -n test-units-nosync` prints `go test $(go list …)` and
`make -n ci-test-slow-nosync` prints `go test ./cmd/liza/`, so validation commands 1 and 2 of
Task 1 exit 1 (RP5). The child records that red run, then the green run after its edit. The
runtime symptom (a package binary past 10 minutes on the macOS runner) is reproducible only on CI;
the next G8 observes it (`d59`).

### d. Verification scope

Checked: the attempt-1 macOS and Linux A4 job logs, A1/A2 job logs for ops durations, the Makefile
recipes, `ci-core.yml` macOS job, the Go default, local dry-run expansion before and after a scratch
edit (RP5, RP6), `make lint` at `1e7807b06` (RP7). Not checked: per-test durations inside
`internal/ops` on macOS (the lane runs without `-v`); A3's macOS duration (operator-attributed);
the in-progress attempt 2 of run 40219462697, which is informational only and cannot count as
G8 under `d60`.

### e. Sufficiency

With `-timeout 30m`, the `internal/ops` binary gets three times the 600 s it exceeded
before Go aborts it; the failure observed here becomes impossible below 30 minutes.
Residuals:
1. The macOS job's `timeout-minutes: 30` (`ci-core.yml:176`) bounds the whole lane; the A4 job ran
   about 14 minutes. Growth past that cap needs a workflow change (out of scope).
2. The ACP integration timeout (item 2) is independent of this budget and is not fixed here.
3. A test that does hang would now take 30 minutes to surface instead of 10, still with Go's
   `running tests:` report. The disproof route below keeps this from being misread as budget.

### f. Adjacent findings (recorded, not planned)

- `TestCodexACPProviderUnavailableLeavesUnrelatedFailureOnCrashPath`: its file has no commit on
  `origin/main..1e7807b06` (RP13). Whether its exercised production code changed on the branch is
  not checked here; `d60` classification belongs to the maintainer at the next G8.
- The macOS job cap (`ci-core.yml:176`) is now the binding lane limit (residual 1).

### Disproof route (AC-disproof)

If a later timeout panic in either recipe lists, under `running tests:`, a test whose elapsed time
is minutes rather than seconds, the budget is not the cause. That test is reported as a hung or
regressed test with the job log line; under `d60` it holds the area if the test or its code changed
on the branch. The budget is not raised again. The A4 evidence above (three `(0s)` entries) is the
disproof check for this correction; the manifest cites it.

## Task 1: Give the macOS compatibility lane's non-race go test runs an explicit 30-minute budget

Priority: Must. One intent: the two non-race `go test` recipes run by `ci-test-compat-nosync`
use the same 30-minute per-binary budget as the race and integration recipes.

### Owned files

| File | Change |
|---|---|
| `Makefile` | `:155` `go test ./cmd/liza/` → `go test -timeout 30m ./cmd/liza/`; `:158` `go test $(CI_UNIT_PACKAGES)` → `go test -timeout 30m $(CI_UNIT_PACKAGES)`. Flag placed after `go test`, as in `:152`/`:161` |
| `specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-13-g8-timeout-correction.json` (new) | Acceptance manifest |

Not edited: any other `Makefile` line (`CI_UNIT_PACKAGES`, `-j2`, prerequisites, race, integration,
`test-nosync`, help text); test source, assertions, fixtures, goldens; `.github/`; production Go;
boundary evidence; installed binaries.

`tdd_not_required` (checkpoint): non-behavioural build configuration. No Go test can observe a
`Makefile` recipe flag, and test source is out of scope. Commands 1 and 2 are the falsifiable check:
red before the edit, green after.

### Obligations

- AC-budget: both recipes carry `-timeout 30m` (commands 1, 2; command 0 shows both in the
  lane expansion).
- AC-preserve: the diff is exactly the two flag insertions plus the manifest; package list,
  `-j2`, prerequisites and every other target are unchanged. Reviewer inspects the diff and the
  manifest-recorded command 0 output.
- AC-lint: `make lint` passes (command 3).
- AC-disproof: the manifest cites the disproof route and the A4 evidence (job 121247667497, log
  lines 2935-2939). Reviewer-verified.
- AC-proof: E1 decision below; no `-race` (`d55`).
- AC-placement: graph facts below.

### E1 decision (AC-proof)

`make test` (`go test ./...`, RP8) is kept as a deferred regression for per-merge N1/S7 cover; it
does not run the edited recipes. `make test-e2e` is not listed: its only Go command is the
`-race -tags e2e` run of two integration tests (RP8), which does not use the edited recipes, and a
`Makefile` parse failure already fails commands 0-3. No package is touched, so no scoped race
applies (`d55`).

**Done when:** at `1e7807b06` commands 1 and 2 exit 1 (red recorded in the manifest); after the edit,
commands 0-3 pass; command 0 prints `go test -timeout 30m $(go list ./... | grep -v -e '/internal/integration$' -e '/cmd/liza$')` and `go test -timeout 30m ./cmd/liza/` with the integration
line unchanged; `git diff` against the base touches only `Makefile:155,158` and the new manifest.
No implementation delivery claims G8.

### Canonical validation and manifest allocation

Run from the coding worktree root, serially.

- 0: `make -n ci-test-compat-nosync`
- 1: `sh -c "make -n test-units-nosync | grep -E '^go test -timeout 30m '"`
- 2: `sh -c "make -n ci-test-slow-nosync | grep -x -F 'go test -timeout 30m ./cmd/liza/'"`
- 3: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make lint`
- 4 (deferred): `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make test`

Mappings: AC-budget → command 1 (units recipe) and command 2 (cmd/liza recipe), file `Makefile`;
AC-lint → 3; AC-preserve, AC-disproof, AC-placement → approved proofs. The manifest maps
each ID once; AC-budget maps to command 1 and names command 2 in its assertion text.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-13-g8-timeout-correction.json",
  "obligations": ["AC-budget", "AC-preserve", "AC-lint", "AC-disproof", "AC-proof", "AC-placement"],
  "validation": [
    "make -n ci-test-compat-nosync",
    "sh -c \"make -n test-units-nosync | grep -E '^go test -timeout 30m '\"",
    "sh -c \"make -n ci-test-slow-nosync | grep -x -F 'go test -timeout 30m ./cmd/liza/'\"",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test"
  ],
  "deferred": [4],
  "timeout_seconds": 1800,
  "approved_proofs": [
    {"obligation_id": "AC-preserve", "reference_id": "goal-failure", "rationale": "The fix task changes only the failing budget: the reviewer verifies the diff is the two flag insertions plus the manifest and that the recorded command 0 output keeps the package list, -j2 and integration line unchanged."},
    {"obligation_id": "AC-disproof", "reference_id": "d60", "rationale": "Whether a timeout is budget or a hung changed test is a CI-log fact: the reviewer checks the manifest cites job 121247667497 lines 2935-2939 (three running subtests at 0s) and the plan's disproof route."},
    {"obligation_id": "AC-proof", "reference_id": "d55", "rationale": "The E1 selection is a plan decision: no race command, make test deferred as regression, make test-e2e omitted because it does not run the edited recipes."},
    {"obligation_id": "AC-placement", "reference_id": "a4-proof", "rationale": "Waits and the absence of any edge from or to the held A4 boundary are graph facts inspected in the generated task, not test outcomes."}
  ]
}
```

## Dependencies and Shared-File Ownership

| Output | Waits on | Earliest sufficient artifact | Priority |
|---|---|---|---|
| 0 (Task 1) | `inherit_inputs` selected: `arm-1-ar-13-cp-5` output 2 (= `arm-1-ar-13-cp-5-code-2`, MERGED as `1e7807b06`) | the A4 boundary tree G8 tested; the edited recipes predate the run (`8e117e1fa` on `main`) | Must |

Output 2 is the last A4 merge, so the selection inherits nothing unmerged and no barrier wider than
the tested SHA. No `depends_on` (single output), no `task_depends_on`, no provider wait; no
Should/Could target is reachable.

| File or definition | Writers | Ordering |
|---|---|---|
| `Makefile` `test-units-nosync`, `ci-test-slow-nosync` recipes | `8e117e1fa` (main) → Task 1 | Only open writer: every active non-draft task is this plan or the held boundary, whose scope excludes `Makefile`; every `arm-1-ar-14` … `arm-1-ar-25` plan output naming `Makefile` lists it as Out (RP11) |
| Acceptance manifest | Task 1 | Unique name |

## Placement

Rule: `a4-proof` "Correction placement rule".

- **Predecessors:** no started writer of the edited recipes; the merged A4 chain is satisfied through
  the selected output.
- **Successors:** the only consumer needing this correction is the started, BLOCKED A4 boundary
  `arm-1-ar-14-cp-0-code-0`. The operator commission of 2026-10-25T18:20:03Z (`d52` maintainer role)
  is the ruling for that started successor: the boundary stays held; after this child merges, the
  operator releases it to rebase, refresh G1-G7 at the new SHA and request a new G8 (`d59`). Its
  existing blocked reason already records that hold, so no graph edge is added and no existing A5
  wait changes.
- **Slot:** earliest, on current integration (`1e7807b06` or later).

## Reality Probes

At `1e7807b0688691feef5369ff8d06953cfc8c174e`, 2026-10-25T18:22Z-18:32Z UTC, Linux, Go 1.26.0.

| ID | Assertion / command | Observation |
|---|---|---|
| RP1 | `gh run view 40219462697 -R liza-mas/liza --json jobs,headSha,attempt`; `gh api …/runs/40219462697/attempts/1/jobs` | headSha `1e7807b06…`; attempt 2 in progress (operator rerun); attempt-1 macOS job 121247667497 `failure`, Linux and Windows `success` |
| RP2 | `gh api …/jobs/121247667497/logs` lines 199, 368-378, 1568-2118, 2935-2939, 8752 | As in RCA a |
| RP3 | `internal/ops` line in attempt-1 macOS/Linux job logs of runs 40132329230, 40162651273, 40219462697 | macOS 446.773s, 500.995s, >600s; Linux 331.799s, 344.256s, 353.390s |
| RP4 | `nl -ba Makefile` 125-175, 355-368 | Recipes and help text as cited |
| RP5 | `make -n ci-test-compat-nosync`; `make -n test-units-nosync`; `make -n ci-test-slow-nosync`; commands 1 and 2 | Expansion without `-timeout` on both recipes; commands 1 and 2 exit 1 |
| RP6 | Scratch copy outside the repo with the two Task 1 edits (anchor-asserted script): `make -n ci-test-compat-nosync`, commands 1 and 2 | Diff exactly lines 155, 158; expansion shows both flags, integration line unchanged; commands 1 and 2 exit 0 |
| RP7 | `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make lint` | Pass, wall 67.48s; worktree clean afterwards |
| RP8 | `make -n lint`, `make -n test`, `make -n test-e2e` | lint: sync-embedded, testhelpers check, consistency test, gofmt, three-OS `go vet`, archguard; test: `go test ./...`; test-e2e: `go test -race -tags e2e -run '^(TestFullSprintSequence\|TestTerminalDependencyRecovery)$' ./internal/integration/ -count=1` |
| RP9 | `go help testflag` | `-timeout d` default 10m |
| RP10 | `nl -ba .github/workflows/ci-core.yml` 172-240; `grep -rn` for the three target names; `.github/workflows/README.md:1-19` | macOS job runs `make ci-test-compat-nosync`, `timeout-minutes: 30`; no other invoker; docs name targets only |
| RP11 | `omni-ee get tasks --active --summary --json`; `omni-ee get arm-1-ar-14-cp-0-code-0 --json`; `jq` over `*-output.json` scopes naming `Makefile` | Non-draft active tasks: this plan, the BLOCKED boundary (scope "Out: … Makefile"); ar-14/15/17/19/21/25 plans list `Makefile` as Out |
| RP12 | `git grep -h -E '^func Test[A-Z_]' <rev> -- 'internal/ops/*_test.go' \| wc -l`; `git log --oneline origin/main..HEAD -- internal/ops \| wc -l` | 2000 → 2098; 45 commits |
| RP13 | `git log --oneline origin/main..HEAD -- internal/integration/provider_unavailable_acp_test.go` | No commit |
| RP14 | `git log -L154,161:Makefile`; `git merge-base --is-ancestor 8e117e1fa origin/main` | Recipes from `8e117e1fa`, on `main` |
| RP15 | `omni-ee get arm-1-ar-13-cp-5 --output-summary --json` | Output index 2 = Task 3 (`arm-1-ar-13-cp-5-code-2`), merged as `1e7807b06` |

PROSPECTIVE (Task 1): the two flags and the manifest. Native macOS behaviour is UNPROBED (no macOS
host; dispatch out of scope); it is held on the new G8 the boundary already awaits (`d59`).

**Budget (measured plus estimate):** commands 0-2 under 1 s each (RP5/RP6); `make lint` 67.5 s
measured (RP7). 1800 s also covers deferred `make test` (8-10 min per D54's evidence, an estimate
for this host) should a batch apply the same bound, with load headroom.

## Spec Compliance Matrix

| # | Requirement (effective priority) | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Must: red boundary CI is fixed by a maintainer-commissioned task before the next area starts | goal-failure; d52 | Task 1 | Covered |
| 2 | Must: Linux, Windows and macOS CI green at the boundary (budget cause removed) | goal-nfr N2 | Task 1 (AC-budget) | Covered |
| 3 | Must: a failure in changed code is fixed, not rerun away; hung test disproved, not masked | d60 | RCA; Task 1 (AC-disproof) | Covered |
| 4 | Must: no test, assertion, package list, parallelism or other target changes | goal-tests; goal-failure | Task 1 (AC-preserve) | Covered |
| 5 | Must: E1 proof without per-task race; `make lint` passes | goal-proof E1; d55 | Task 1 (AC-lint, AC-proof) | Covered |
| 6 | Must: N1 integration branch stays green per merge | goal-nfr N1; goal-success S7 | Task 1 (deferred `make test`) | Covered |
| 7 | Must: correction placement; boundary held; G8 re-requested on a new SHA | a4-proof; d59 | Placement | Covered |
| 8 | Must: no runtime hardcoding of project tooling | guard-tier1 G1.1 | Correction Authority | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: CI build configuration, no product behaviour change | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: help text and workflow README name targets, not flags (RP10) | N/A |

## Output

One entry, output[0] = Task 1: `inherit_inputs` selects `arm-1-ar-13-cp-5` output 2; `plan_ref` the
Task 1 heading; `spec_ref` `goal-nfr`; validation and prerequisites equal the Acceptance Contract.
`desc`, `done_when` and `scope` are short projections of Task 1.
