# Code Plan: Deterministic In-Flight Submit Deadline Tests (D41)

## Source References
Source revision: "40776dd02fbc4f579c03438599df147483690c08"

### Direct References
- "d41": "specs/goals/20261023-architecture-consolidation-decisions.md#D41 — Stabilise two inherited timing-flaky internal/agent tests (run, 2026-10-24)" @ "40776dd02fbc4f579c03438599df147483690c08"
- "goal-tests": "specs/goals/20261023-architecture-consolidation.md#Test Policy and Sequencing" @ "40776dd02fbc4f579c03438599df147483690c08"
- "goal-proof": "specs/goals/20261023-architecture-consolidation.md#Proof Environment" @ "40776dd02fbc4f579c03438599df147483690c08"
- "guard-tier1": "GUARDRAILS.md#Tier 1 (Hard Constraints)" @ "40776dd02fbc4f579c03438599df147483690c08"
- "a1-task": "specs/plans/20261023-architecture-consolidation/20261023-200740-arm-1-ar-1-cp-1.md#Task 1: A1 persistence oracle package" @ "40776dd02fbc4f579c03438599df147483690c08"

### Obligation Coverage
- "D41-watchdog" -> "d41"
- "D41-grace" -> "d41"
- "D41-package" -> "d41"
- "D41-preserve" -> "d41", "goal-tests"
- "A1-race" -> "a1-task"
- "A1-regen" -> "a1-task", "goal-proof"
- "A1-tamper" -> "a1-task"
- "A1-goldens" -> "a1-task", "goal-tests"
- "E-lint" -> "goal-proof", "guard-tier1"
- "E-suite" -> "goal-proof", "guard-tier1"

## Intent and Authority

Based on: the D41 ruling and both operator inputs recorded on this task,
the task JSON, current worktree source at 40776dd02, the original A1 task
acceptance, and the captures in Reality Probes. Success means one bounded
test-only child makes the two D41 tests deterministic and carries the
task's retained full-suite and A1 non-regression proof. Doc Impact: this
plan and its output JSON only. Test Impact: the child rewrites two existing
white-box tests in place and adds a late-start regression subtest to each.

⚠️ Source conflict, resolved by the human: the task SCOPE still bounds the
correction to "the original A1 persistence ownership", while D41 and the
operator input of 2026-10-24T02:52Z withdraw the A1 attribution and
repurpose this task to the two `internal/agent` tests. The later human
ruling governs. The task's DONE WHEN A1 and full-suite items are kept as
undeferred non-regression commands, so both sources are satisfied. No
production, oracle, harness or other-area file is allocated.

Priority: Must (D41 ruling; task priority 1). One output, no optional work.

## Root Cause Analysis

### a. Evidence

Source (read this session at 40776dd02):

- `internal/agent/systemctl.go:377` starts `withExecutionDeadline` before
  the watchdogs and before `agent.Run` (`:392`, `:399`). The mock agent's
  `OnExecute` runs inside `Run` (`supervisor_test.go:83-84`). Everything
  between, including the synchronous state read in
  `review_execution.go:139` and `ops.LoadResolverForModels` in
  `progress_watchdog.go:52`, consumes the execution budget.
- `internal/agent/execution_deadline.go:76-86`: a real `time.NewTimer` fires
  once. If `inflight(time.Now())` finds no valid marker at that moment, it
  cancels with plain `context.DeadlineExceeded`. A later marker cannot
  revive the session. No clock or timer seam exists. The only seams are the
  package variables `executionDeadlineRecheckInterval`,
  `maxInflightSubmitGrace` and `submitHandbackWindow` (`:15-24`).
- `internal/ops/submission_lifecycle.go:97-119`: a real submit writes its
  marker only after `prepareSubmitForReview` (rebase, index refresh).
  `inflight_submit.go:90-106`: deferral needs a marker whose deadline is in
  the future. "Running" counts any live-process marker.
- `internal/agent/systemctl.go:458-468`: a plain `DeadlineExceeded` cause
  takes the "Agent execution timeout" path and returns exit 1. Only the
  in-flight causes reach `endSessionAfterInflightSubmit` (block, exit 0).
- `inflight_submit_deadline_test.go:143-184,234-247`: test 1 (watchdog). Its
  timer is 1 s. The agent idles 0.5 s, then the real submit prepares and
  writes its marker. `:251-292`: test 2 (grace). Its timer is 200 ms, and
  the agent writes the marker as its first action.

Runtime:

- Operator capture at aaac7a4a1 (reporter-attributed, D41 and the operator
  input): one of two `go test -race -count=1 -v ./internal/agent/` runs
  failed, at `:241` ("session was cancelled before the agent saw its
  submit's outcome") and at `:286` ("executeAgent = (1, <nil>), want exit
  0").
- Planner baseline RP2: test 1 failed 3/20 at `:241`. Each failing iteration
  logs "Agent execution timeout (context deadline exceeded)" and no
  "Deferring execution timeout" line.
- Planner fault injection RP4: delaying the agent's first action past the
  timer reproduces both exact signatures deterministically.

### b. Mechanism

Both tests need the session's in-flight marker to exist when the supervisor's
real execution timer fires. That timer starts before the agent runs. Test 1
leaves a margin of 1 s − (launch latency + 0.5 s idle + submit preparation).
Test 2 leaves 200 ms − launch latency. When a loaded host misses the margin,
`execution_deadline.go:83-85` sees no marker and cancels with
`DeadlineExceeded`. In test 1 the context ends while the submit prepares,
so `liveAtReturn` is false (`:241`). In test 2 `executeAgent` takes the
plain-timeout path and returns `(1, nil)` (`:286`). Production behaves as
specified: a session without an observable marker at its timeout is a
plain timeout. The defect is the tests' wall-clock ordering assumption.

### c. Reproduction

- Natural: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -race -count=20 -run '^(TestExecuteAgentProgressWatchdogStillBlocksDuringHandback|TestExecuteAgentBlocksSubmitStillRunningWhenGraceEnds)$' ./internal/agent/`
  on a host at load ≥10 (RP2: 3/20 red, test 1).
- Deterministic: delay the agent's first `OnExecute` action past the
  execution timeout: 600 ms for test 1's shape, 300 ms for test 2's.
  The current shapes then fail with the exact `:241` and `:286` signatures
  (RP4). The child's "late start" subtests encode this state.

### d. Verification scope

Checked: the code paths above, a natural reproduction of test 1, a
deterministic reproduction of both, and a 20× race run of the proposed
held-marker shape under load 12–17 (RP5). The full package passed once
(RP6). Not checked: a natural reproduction of test 2. It passed 20/20
pinned to one CPU (RP3), so its natural occurrence is reporter-attributed.
The repeated "docs-only" bisect blame in batches 2 and 8 is consistent with
a nondeterministic package failure. Neither batch's original log was
re-read.

### e. Sufficiency

The child writes the marker before `executeAgent`. The marker therefore
exists before the timer starts, and the timer observes it at any launch
latency. The marker is released or expires only after events that the
scenario orders after the fire:

- Test 1 releases its held marker after the real submit returns. That
  submit's canonical `sleep 1` starts after the timer, so the release
  cannot precede the fire. The watchdog is exempt while the marker is valid
  (`progress_watchdog.go:139`).
- Test 2 holds a 1 h marker and bounds the deferral with
  `maxInflightSubmitGrace` = 500 ms. That is the same 0.5 s deferral as
  before, measured from the fire.

Residual: test 2 keeps its unchanged `elapsed > 3s` upper bound, which
includes supervisor set-up. Under extreme load that bound can still fail.
It was not observed failing, and changing it would alter an asserted value
(D41).

### f. Adjacent findings (recorded, not planned)

- `TestExecuteAgentBlocksUnadmittedLateSubmitAfterHandback` and
  `TestExecuteAgentLetsLateSubmitPublishPastSupervisorClocks` have the same
  latent ordering (the real submit's marker must precede a 1 s / 2 s timer).
  Neither was observed failing. D41 bounds this task to two tests. Owner:
  operator / `internal/agent` test owner.
- The deferred-validation bisect assigns a nondeterministic failure to
  whichever merge it lands on (docs-only merges 2f3374a6d, be0c4d4ca).
  Owner: OMNI deferred-validation tooling.

## Task 1: Make the two D41 deadline tests deterministic

**Priority:** Must (d41). **Depends on:** nothing. The file has been
unchanged since the starting commit. No other planned or started task names
it, and later A9 refactors carry the merged version.
**Files:** `internal/agent/inflight_submit_deadline_test.go` and the
declared manifest. Nothing else: no production `.go`, no
`execution_deadline_test.go`, no `testhelpers`, no oracle path.

**Deliverable (local directions under d41):**

- Test 1 (`TestExecuteAgentProgressWatchdogStillBlocksDuringHandback`):
  before `executeAgent`, write this session's marker with
  `ops.WriteInflightSubmitMarkerForTest` (deadline now+10 min, `t.Cleanup`
  removal). The agent runs the real submit. After the submit returns, it
  records `liveAtReturn` and then removes the held marker, which starts the
  1 h hand-back. Keep the canonical command, the 1 s / 2 s / 1 h budgets and
  the 50 ms recheck. Update the stale "preparation before the marker"
  comment.
- Test 2 (`TestExecuteAgentBlocksSubmitStillRunningWhenGraceEnds`): before
  `executeAgent`, shorten `maxInflightSubmitGrace` to 500 ms (restore on
  cleanup) and write a marker with deadline now+1 h that is held for the
  whole session. The agent only waits for `ctx.Done()`. The docstring states
  that the grace cap bounds the deferral. The marker-past-own-deadline
  semantics stay asserted by the unchanged
  `TestExecutionDeadlineMarkerPastItsDeadlineWithLiveProcessIsInterrupted`
  (`execution_deadline_test.go:237`).
- Each test runs its scenario as two subtests: "prompt start" with no delay,
  and "late start". In "late start", the agent's first action is delayed
  past the execution timeout (≥1.5 s for test 1, ≥1 s for test 2). Both
  subtests carry every existing assertion with unchanged values.
- Red first: add the "late start" subtests against the current scenario
  shape. Capture the failure output, which must show the `:241` and `:286`
  signatures. Record the command and an excerpt in the checkpoint. Then
  apply the held-marker change to make them pass.
- If `lateSubmitSession` is extended, `TestExecuteAgentBlocksUnadmittedLateSubmitAfterHandback`
  keeps its current behavior: no held marker and no delay. The other tests
  in the file stay untouched.
- Failure messages report no environment values (G1.4).

**Completion:** the two tests pass 20× under race and normal agent load, and
so does the full package. The late-start subtests fail before the change and
pass after it. Every prior assertion and value is retained. The diff touches
only the two declared files.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/deferred-validation-2-1-correction-plan-agent-deadline-tests.json",
  "obligations": [
    "D41-watchdog",
    "D41-grace",
    "D41-package",
    "D41-preserve",
    "A1-race",
    "A1-regen",
    "A1-tamper",
    "A1-goldens",
    "E-lint",
    "E-suite"
  ],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=20 -run '^(TestExecuteAgentProgressWatchdogStillBlocksDuringHandback|TestExecuteAgentBlocksSubmitStillRunningWhenGraceEnds)$' ./internal/agent/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 ./internal/agent/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 ./internal/oracle/a1/persist/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a1/persist/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp ORACLE_VERIFY_TAMPER=1 go test -count=1 -timeout 25m -run '^TestOraclePersistenceTamper$' ./internal/oracle/a1/persist",
    "git diff --exit-code global-refactoring -- internal/oracle/a1/persist",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test"
  ],
  "timeout_seconds": 5400,
  "approved_proofs": [
    {
      "obligation_id": "D41-preserve",
      "reference_id": "d41",
      "rationale": "Independent diff review confirms that no assertion or asserted value was weakened or removed, that no production file changed, that the other tests in the file kept their behavior, and that the moved marker-deadline semantics remain asserted by the unchanged unit test; a green run cannot prove absence of weakening."
    }
  ]
}
```

### Manifest and Retained Proof

Manifest mapping: D41-watchdog and D41-grace name both subtests of their
test at index 1. D41-package maps to index 2. A1-race, A1-regen, A1-tamper
and A1-goldens map to indexes 3–6. E-lint maps to 0 and E-suite to 7.
D41-preserve maps by `approved_reference_id` only. Lint is rerun because
the change is Go. `make test-e2e` is retained from batch 2 (passed): it
builds only tagged `internal/integration` tests, and a package-`agent`
`_test.go` file is not compiled into that binary (RP9).

## Dependencies and Shared Files

One output, no `depends_on`, no `task_depends_on` and no provider wait.
The earliest input is already merged source. No other output or active task
edits `inflight_submit_deadline_test.go`. A1 typed-vocabulary tasks keep
`models.TaskEvent*` comparisons compiling unchanged, so those edits commute.
The manifest path is unique. There is no ordered shared-file chain, so no
`## Placement` is needed. Provider Ask Inventory: N/A (no shared interface).

## Reality Probes

Worktree/integration snapshot `40776dd02fbc4f579c03438599df147483690c08`,
2026-10-24 UTC, 8 CPUs. Logs live under `/tmp` and are evidence for this
review only.

| ID | Command / source | Observation |
|---|---|---|
| RP1 | `git rev-parse HEAD`; `git status --short` | 40776dd02…, clean. |
| RP2 | Natural reproduction command (RCA c), 02:59–03:01Z, load 10.5 | exit 1, 100.6 s. 3× `--- FAIL: TestExecuteAgentProgressWatchdogStillBlocksDuringHandback` at `:241`, each with "Agent execution timeout (context deadline exceeded) … timeout=1s" and no deferral line. Log sha256 42a9994e…3312. An earlier identical run also exited 1. |
| RP3 | `taskset -c 0 go test -race -count=20 -run '^TestExecuteAgentBlocksSubmitStillRunningWhenGraceEnds$' ./internal/agent/` | ok, 25.7 s. Test 2 not reproduced naturally; the operator capture stands as reporter evidence. |
| RP4 | `go test -race -count=1 -overlay /tmp/dv21-probe/overlay.json -run '^TestProbe' -v ./internal/agent/` (probe file sha256 fc58fa32…2841, overlay only, worktree untouched) | Current shapes with injected latency: "live=false exit=1" (600 ms) and "executeAgent = (1, <nil>), want exit 0 after a supervisor block" (300 ms). Held-marker shapes pass at latency 0 and 1.5 s / 1 s. |
| RP5 | Same overlay, `-count=20 -run '^TestProbe.*HeldMarker$'`, 03:06–03:10Z, load 12–17 | ok, 256.3 s. 40 runs of each held shape passed. |
| RP6 | `env -i … go test -race -count=1 ./internal/agent/` (index 2), 03:13–03:20Z, load ~20 | ok, 365.7 s (wall 366 s). |
| RP7 | Indexes 3–5 at HEAD | ok 1.16 s; regen check prints `ok … H1` for all nine `testdata/golden/state/*.txt`; tamper ok 56.8 s. |
| RP8 | Index 6 at HEAD | exit 0. `config.integration_branch` = `global-refactoring`. |
| RP9 | `make -n test lint test-e2e` | `go test ./...`; sync-embedded, three-GOOS vet, archguard; e2e = `go test -race -tags e2e -run '^(TestFullSprintSequence\|TestTerminalDependencyRecovery)$' ./internal/integration/`. |
| RP10 | `specs/architecture/consolidation-run/baseline.md` §`make test` wall time | Recorded baseline 523 s (cited, not rerun). |

**Budget basis (estimate):** lint 900 s, index 1 ≈ 300 s (RP5 plus the two
"prompt start" subtests), index 2 ≈ 400 s (RP6), indexes 3–6 ≈ 120 s
(RP7), and `make test` ≈ 1100 s (RP10 ×2 for load). That sums to about
2800 s; ×~2 headroom gives 5400 s. No stated bound is exceeded (cap 14400 s).

## Spec Compliance Matrix

| # | Requirement (priority) | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Must: test 1 deterministic through event ordering instead of wall-clock budgets | d41 Ruling | Task 1 | Covered |
| 2 | Must: test 2 deterministic through event ordering instead of wall-clock budgets | d41 Ruling | Task 1 | Covered |
| 3 | Must: no weakened assertion | d41 Ruling | Task 1 (D41-preserve) | Covered |
| 4 | Must: no production code change | d41 Ruling | Task 1 (scope, D41-preserve) | Covered |
| 5 | Must: `-race -count=20` on the two tests passes under agent load | d41 Proof | Task 1 (index 1) | Covered |
| 6 | Must: full package passes | d41 Proof | Task 1 (index 2) | Covered |
| 7 | Must: no skip, no known-red, no budget-widening-only fix | d41 Rejected | Task 1 | Covered |
| 8 | Must: white-box assertions remain asserted somewhere | goal-tests | Task 1 (unit test kept, D41-preserve) | Covered |
| 9 | Must: regression tests fail before and pass after the correction | task DONE WHEN | Task 1 (late-start subtests, red first) | Covered |
| 10 | Must: exact deferred `make test` passes in a clean task worktree | task DONE WHEN; goal-proof E1 | Task 1 (index 7) | Covered |
| 11 | Must: A1 focused race, regen check and tamper pass | task DONE WHEN; a1-task | Task 1 (indexes 3–5) | Covered |
| 12 | Must: nine A1 goldens and asserted values unchanged | task DONE WHEN; goal-tests | Task 1 (index 6, scope) | Covered |
| 13 | Must: lint rerun where affected; e2e retained | task DONE WHEN | Task 1 (index 0); e2e retained (RP9) | Covered |
| 14 | Must: an unreproduced failure is reported as unresolved | task DONE WHEN | RCA d (test 2 natural occurrence reporter-attributed) | Covered |
| 15 | Must: env -i isolation, no env values in messages | guard-tier1 G1.4; goal-proof E4 | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: test-only change, no behavior; `make test-e2e` evidence retained | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: no behavior change; this plan is the RCA record | N/A |
