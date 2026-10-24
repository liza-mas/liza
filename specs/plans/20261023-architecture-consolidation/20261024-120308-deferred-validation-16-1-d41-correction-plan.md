# Code Plan: D41-Family Timing Corrections for Agent and Ops Tests

## Source References
Source revision: "53504b7d81d44533d290f4132f7cf4ba2b7d1705"

### Direct References
- "d41": "specs/goals/20261023-architecture-consolidation-decisions.md#D41 — Stabilise two inherited timing-flaky internal/agent tests (run, 2026-10-24)" @ "53504b7d81d44533d290f4132f7cf4ba2b7d1705"
- "d46": "specs/goals/20261023-architecture-consolidation-decisions.md#D46 — Remove the time.Sleep calls merged by A9 oracle rows; the sleep budget stays (run, 2026-10-24)" @ "53504b7d81d44533d290f4132f7cf4ba2b7d1705"
- "d41-task": "specs/plans/20261023-architecture-consolidation/20261024-031400-deferred-validation-2-1-correction-plan.md#Task 1: Make the two D41 deadline tests deterministic" @ "53504b7d81d44533d290f4132f7cf4ba2b7d1705"
- "d41-adjacent": "specs/plans/20261023-architecture-consolidation/20261024-031400-deferred-validation-2-1-correction-plan.md#f. Adjacent findings (recorded, not planned)" @ "53504b7d81d44533d290f4132f7cf4ba2b7d1705"
- "goal-tests": "specs/goals/20261023-architecture-consolidation.md#Test Policy and Sequencing" @ "53504b7d81d44533d290f4132f7cf4ba2b7d1705"
- "goal-proof": "specs/goals/20261023-architecture-consolidation.md#Proof Environment" @ "53504b7d81d44533d290f4132f7cf4ba2b7d1705"
- "guard-tier1": "GUARDRAILS.md#Tier 1 (Hard Constraints)" @ "53504b7d81d44533d290f4132f7cf4ba2b7d1705"

### Obligation Coverage
- "AG-unadmitted" -> "d41", "d41-adjacent"
- "AG-publish" -> "d41", "d41-adjacent"
- "AG-d41-retained" -> "d41", "d41-task"
- "AG-package" -> "d41", "goal-proof"
- "AG-sleep" -> "d46"
- "AG-lint" -> "goal-proof", "guard-tier1"
- "AG-preserve" -> "d41", "goal-tests"
- "AG-red" -> "d41", "d41-task"
- "AW-verdict" -> "d41", "goal-tests"
- "AW-package" -> "goal-proof"
- "AW-sleep" -> "d46"
- "AW-lint" -> "goal-proof", "guard-tier1"
- "AW-preserve" -> "d41", "goal-tests"
- "AW-red" -> "d41"
- "CK-recency" -> "d41", "goal-tests"
- "CK-package" -> "goal-proof"
- "CK-sleep" -> "d46"
- "CK-lint" -> "goal-proof", "guard-tier1"
- "CK-preserve" -> "d41", "goal-tests"
- "CK-red" -> "d41"

## Intent and Authority

Based on: the task JSON, the operator note of 2026-10-24T11:27:21.95006849Z
(on `arm-1-ar-13-cp-3`), D41, D46, the merged D41 plan, the D41 implementation
`deferred-validation-2-1-correction-plan-code-0` (merge `aaf2c5e33`, its
acceptance receipt and round-1 review), the three batch logs, source at
`53504b7d8`, and the probes below.

**Lineage.** This plan follows D41 and its merged implementation. It is not a
correction to `arm-1-ar-13-cp-3`, it supersedes nothing, and it changes no
task's status. The D41 RCA named the two agent tests in its adjacent findings
(d41-adjacent). The D41 receipt mapped `TestExecuteAgentBlocksUnadmittedLateSubmitAfterHandback`
as "unchanged in behavior" (obligation D41-package). The D41 review ran it
unchanged. Batch 15 ran at the D41 merge and failed it there. The
batch 16 bisect blamed a plans-only diff (`800bef47a..e7a3158be`). The
operator note records that `make test` passed on a later tree that contains
that diff. RCA d. covers this attribution.

**Operator constraints (verbatim, binding on every output):** "keep every
assertion, keep maxSleepCallsInTests at 16, and change no production code
unless it is proven to be a real defect." D41's rejected options also bind
all three outputs: no skip, no known-red, and no budget-only widening.

Doc Impact: this plan, its output JSON, and the three acceptance manifests
the children commit. Test Impact: three test files are rewritten in place
(one per child). Each named scenario gains a regression subtest or case
under controlled latency. No production file, guard, test helper package or
oracle file changes. Priority: Must for all three outputs (task priority 1),
with no optional work.

## Root Cause Analysis

Three distinct causes, one per output. All production paths behave as
specified. Each defect is a test whose ordering assumption depends on host
latency.

### a. Evidence

Runtime (checkable artifacts under `.omni-ee/tool-results/`):

- `654716bf…94.txt` (batch 16, `make test`, exit 2): line 120 shows `Agent
  execution timeout (context deadline exceeded) … timeout=1s`. Line 121 is
  `--- FAIL: TestExecuteAgentBlocksUnadmittedLateSubmitAfterHandback`, and
  line 122 is `:230 the session was cancelled before the agent saw its
  submit's outcome`. `internal/ops` passed (line 821).
- `dbd33030…16.txt` (batch 16, `make test-race`): at lines 115–118, `preparing
  the submission` is followed at 10:19:17 by `Execution progress timeout,
  blocking task … timeout=1s`, then by `running canonical validation`. At
  10:19:23 the late block fails with `CODE_TO_REVIEW → BLOCKED`. Lines
  119–120 report `TestExecuteAgentLetsLateSubmitPublishPastSupervisorClocks
  :101 … (marker=false receipt=false …)`. Lines 123–125 show the
  unadmitted-test signature again. Lines 1446–1447 report
  `TestAwaitVerdict_Rejected_NewAttempt (10.58s) :589 Verdict = "TIMEOUT"`.
- `30ca5d6e…64.txt` (batch 15, `make test-race` at the D41 merge): lines
  120–122 show the unadmitted signature. Lines 1625–1627 report
  `TestCheckpointPassedPlansRefusesUnqualifiedState/attempted_after_its_pass
  :575 err = <nil>, want ErrNoPassedPlanCheckpoint`.
- Operator note (reporter-attributed): the deferred runner executes five
  commands concurrently, and the tests carry 0.5–2 s wall-clock budgets.

Source (read this session at `53504b7d8`):

- Agent clocks start before the agent acts. `internal/agent/systemctl.go:377`
  starts `withExecutionDeadline`, `:392` starts the progress watchdog, and
  `:399` runs the agent. `execution_deadline.go:76-86` arms a one-shot real
  timer. If no live marker exists when it fires, it cancels with
  `DeadlineExceeded` (`:83-85`). `progress_watchdog.go:103` counts from the
  watchdog's start, `:139-142` exempts the watchdog only while a marker is
  live, and `:151-157` blocks the task and cancels the session.
- A real submit writes its marker late. `internal/ops/submission_lifecycle.go:96-119`
  takes the shared project lifecycle lock and runs `prepareSubmitForReview`
  (rebase, index refresh) before it writes the marker (`:118`). Each marker is
  its own nonce file (`inflight_submit.go:60`), and any live one counts
  (`:90-106`).
- Test shapes. `inflight_submit_deadline_test.go:216-239` calls
  `lateSubmitSession(…, time.Hour, 500ms, false)` with no held marker; the
  helper uses a 1 s timer (`:186`). `:22-137` holds no marker and relies on
  the submit's own marker against a 1 s progress timeout and a 2 s execution
  timeout (`:87-88`), after 0.5 s idle (`:41`).
- Await. `internal/ops/await_verdict.go:117` reads the initial state, and
  `:171` handles a verdict already present at that read. The watcher is
  registered only at `:222` (`db/watcher.go:41`), after ownership (`:200`).
  The backstop tick (`:231`, `:239-257`) uses the default poll of 10 s
  (`await_policy.go:22-24`, `models/config.go:94`). The deadline (`:220`)
  is armed before the tick. The test synchronizes by sleeping 200 ms
  (`await_verdict_test.go:563`, `testhelpers/utils.go:55-57`) and waits 10 s
  (`:560`).
- Checkpoint recency. The parent captures `now` (`sprint_checkpoint_test.go:549`).
  The case sets the attempt at that time (`:555`). Its subtests are parallel
  (`:567`). `passedPlanState` takes its own clock (`:437`) and records the pass
  one minute before it (`:446-450`). `orchestrator_wake.go:198-211` admits the
  plan when its latest plan event is after the attempt, and
  `sprint_checkpoint.go:52-54` refuses only when no plan qualifies.
  `internal/ops` holds 1619 `t.Parallel()` calls (RP12).

### b. Mechanism

1. **Agent: the session's marker appears after the supervisor's clocks
   fire.** The execution timer and the watchdog start before the mock
   agent runs. Both tests start a real submit whose marker exists only after
   the lifecycle lock and the preparation. Under load, launch latency plus
   0.5 s idle plus preparation can exceed the 1 s budget:
   - In the unadmitted test, the timer fires with no marker, and the context
     ends with plain `DeadlineExceeded` while the submit prepares. The
     submit returns into a dead context, so `liveAtReturn` is false (`:230`).
   - In the publish test, the watchdog reaches 1 s with no marker, logs the
     progress timeout and cancels while the submit prepares. The agent's
     `ctx.Done` branch then sees no marker and no receipt (`:101`). The
     block attempt that follows fails, because the submit published while
     the agent waited for it (batch 16, 10:19:23).

   This is D41's mechanism on the remaining two tests of the file.
2. **Await: lost wakeup in the test's set-up window.** If the test's
   rejecting `Modify` lands after the initial read (`:117`) but before the
   watcher registers (`:222`), no watcher event exists. The designed
   backstop tick is 10 s, which equals the test's 10 s budget, and the
   deadline timer was armed first. `AwaitVerdict` therefore returns
   `TIMEOUT` (10.58 s observed). The 200 ms sleep orders nothing.
3. **Checkpoint: two clocks in one fixture.** The attempt time comes from
   the parent's clock. The pass time comes from the subtest's clock minus
   one minute. A parallel subtest starts only after its parent returns and
   a `-parallel` slot frees up. When it starts more than 60 s after the
   parent's capture, the pass falls after the attempt. The plan then
   qualifies, and `CheckpointPassedPlans` admits it (`err = <nil>`).

**Production defect assessment (none proven).**

- Mechanism 1 is the documented contract. The marker defers clocks only
  while it exists (D-38, `execution_deadline.go:59-69`), and production
  budgets are an execution timeout of minutes and a progress timeout of
  1800 s (`models/config.go:100`), against seconds of preparation.
- Mechanism 2 delays a verdict by at most one poll interval in production
  (`await_verdict.go:239-257` re-reads state), and nothing is lost. It is
  recorded in f.
- Mechanism 3 is a fixture defect only.

No production change is authorized.

### c. Reproduction

All runs were overlay-only (`-overlay`; the worktree was untouched) at
`53504b7d8` under `env -i`. They are listed under Reality Probes.

- **Agent, stalled preparation (RP7).** The agent holds the project
  lifecycle lock exclusively through `ops.WithProjectLifecycleExclusiveLock`
  for 1.5 s (unadmitted) or 2.5 s (publish), then starts the submit, which
  waits before it can write its marker. Both stalls exceed the clocks,
  because the clocks start before the agent acts. The current shapes fail
  deterministically with the exact signatures: "the session was cancelled
  before the agent saw its submit's outcome" after "Agent execution
  timeout … 1s", and "supervisor cancelled the session before the submit
  published" after "Execution progress timeout … 1s". With a marker held
  from before `executeAgent`, both pass.
- **Await, slow setup (RP5).** The `newAwaitVerdictWatcher` seam is delayed
  by 1 s before the real watcher is built. The current sleep-synchronized
  shape then returns `Verdict = "TIMEOUT", want NEW_ATTEMPT` after 10.29 s.
  Waiting for the watcher instead yields NEW_ATTEMPT (1.47 s).
- **Checkpoint, late fixture (RP5).** The fixture's pass record is moved two
  minutes later, which is the state a subtest builds when it starts two
  minutes after its parent's capture. With the attempt at the parent's
  `now`, the result is `err = <nil>, want ErrNoPassedPlanCheckpoint`. With
  the attempt derived from the fixture's own pass record, the case refuses
  as expected.

The children's done_when encode these states as named regression subtests
(Tasks 1–3).

### d. Verification scope

Checked:

- The code paths above.
- Deterministic reproduction of all four exact failure signatures.
- 20× race runs of every corrected shape under host load 6–10 on 8 CPUs
  (RP8, RP9).
- The sleep guard at HEAD (RP10).
- The test selection of every focused command (RP11).

Not checked:

- A natural rerun of the four tests at HEAD. The batch logs are the natural
  evidence.
- The checkpoint subtest's actual slot wait in batch 15 (unmeasured; Go's
  reported 0.10 s excludes the pause). It is inferred from the fixture
  arithmetic and the reproduction.
- The operator's five-concurrent-command figure (reporter-attributed).

On attribution: batch 16's bisect never reran the failing head (operator
note). The blamed diff touches only plans. A test-only nondeterministic
failure therefore explains both red batches without the blamed merge.

### e. Sufficiency

- **Task 1.** A marker of the session's own submit exists before
  `executeAgent` and stays until the agent has observed the submit's
  return. Both clocks then see a live marker at any launch latency or
  preparation stall. The hand-back starts only when the agent releases the
  marker, which happens after the submit returns, as before. Residual:
  - The held marker, not the real submit's, now carries the deferral. That
    the real submit writes its marker during canonical validation stays
    asserted in `internal/ops` (`budget_exception_test.go:244`,
    `submission_admission_test.go:125`).
  - The upper bounds (`wait ≤ 1.5 s` after the submit returns, and the
    hand-back ≥ 400 ms) stay wall-clock. They measure the 50–100 ms recheck
    plus a 500 ms window and were not observed failing. Changing them would
    alter asserted values.
- **Task 2.** The rejection is written only after the real watcher has
  registered, so an event exists whatever the set-up duration. No residual
  in this test.
- **Task 3.** The attempt time is derived from the fixture's own pass
  record, so ordering no longer depends on scheduling. No residual.

### f. Adjacent findings (recorded, not planned)

- Eight other `testhelpers.WaitForAsyncSetup()` uses in
  `internal/ops/await_verdict_test.go` share mechanism 2. None was observed
  failing, and this task's scope names one test. Owner: operator /
  `internal/ops` test owner.
- In production, a verdict that lands between `await_verdict.go:117` and
  `:222` waits up to one `await_poll_interval` (10 s default). This is
  latency, not loss. Owner: A3 ops-kernel owner, if it is judged worth a
  re-read after watcher registration.
- Deferred-validation bisect attributes nondeterministic failures to
  whichever merge it lands on, and it does not rerun the failing head (D41
  f., repeated in batch 16). Owner: OMNI deferred-validation tooling.

## Common Rules (all outputs)

- Carry the operator constraints verbatim (Intent and Authority).
- Every original assertion and asserted value is kept. The traceability
  table below names each assertion by its original line. Tests are
  rewritten in place and are not renamed. Regression variants are added as
  subtests or table cases of the same test.
- Red first. Add the regression variant to the current shape, run the
  focused command, and record the exact failure lines in the checkpoint.
  Then apply the correction.
- No `time.Sleep`. Injected latency uses `time.After` inside the scenario,
  as the existing `startDelay` does. A wait for an event may carry a
  generous failure ceiling: that ceiling fails the test and is not a wait.
  `internal/testguard/sleep_usage_test.go` is not edited, and
  `maxSleepCallsInTests` stays 16.
- Failure messages report no environment values (G1.4). Proof runs under
  `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` in the task worktree.
- Deferred-validation policy: `make test`, `make test-e2e` and
  `make test-race` are shared regression suites. They are listed once and
  deferred, so admission skips them and the integration batch runs them.
  `make lint` checks the changed files and stays undeferred.

## Assertion Traceability (original → retained)

| Test (file) | Original assertion (line) | Value | Retained in |
|---|---|---|---|
| BlocksUnadmittedLateSubmitAfterHandback (agent) | submit with a failing command errs (:226-228) | `exit 3` after `sleep 1` | both subtests, Task 1 |
| same | session live when the submit returned (:229-231) | — | both subtests |
| same | hand-back ≥ 400 ms (:232-234) | 500 ms window | both subtests |
| same | exit 0 (:235-237) | 0 | both subtests |
| same | BLOCKED "no longer observable", no claim_released (:238, :196-211) | — | both subtests |
| LetsLateSubmitPublishPastSupervisorClocks (agent) | not cancelled before submit (:97-99) | 0.5 s idle | both subtests |
| same | not cancelled before publication (:100-102) | — | both subtests |
| same | submit error nil (:103-105) | — | both subtests |
| same | receipt at review commit (:110-113) | — | both subtests |
| same | last event submitted_for_review (:114-116) | — | both subtests |
| same | no claim_released/blocked event (:117-121) | — | both subtests |
| same | exit 1, nil error (:124-126) | 1 | both subtests |
| same | session end ≤ 1.5 s after submit return (:127-129) | 1500 ms | both subtests |
| same | no in-flight markers left (:130-136) | 0 | both subtests |
| D41 tests (agent, :244-334) | all assertions | unchanged | unchanged; call sites pass the new zero stall |
| AwaitVerdict_Rejected_NewAttempt (ops) | AwaitVerdict error nil (:585-587) | — | both subtests, Task 2 |
| same | Verdict NEW_ATTEMPT (:588-590) | NEW_ATTEMPT | both subtests |
| same | state readable (:593-596) | — | both subtests |
| same | CurrentTask nil (:597-600) | nil | both subtests |
| CheckpointPassedPlansRefusesUnqualifiedState (ops) | refusal precondition (:569-573) | 1 failure | all cases, Task 3 |
| same | ErrNoPassedPlanCheckpoint (:574-576) | the error | all cases incl. the new late case |
| same | state readable (:577-580) | — | all cases |
| same | sprint IN_PROGRESS (:581-583) | IN_PROGRESS | all cases |

Scenario values kept: agent budgets of 1 s / 2 s / 1 h, a 1 s progress
timeout, a 500 ms hand-back, 50 / 100 ms rechecks, and the canonical scripts;
await timeout 10 s, cycles 4→5 of 5, iterations limit 10, reason "Missing
tests", and the `AwaitVerdict` entry point at the default poll; checkpoint
pass one minute before the fixture clock, with the attempt one minute after
the pass.

## Task 1: Make the unadmitted and publish in-flight submit tests deterministic

**Priority:** Must (d41 lineage; task priority 1). **Depends on:**
`task_depends_on: ["deferred-validation-2-1-correction-plan-code-0"]`, the
merged earlier writer of this file (Placement predecessor; already MERGED, so
the wait is satisfied at claim). The earliest input is merged source at
`53504b7d8`, which includes the D41 merge `aaf2c5e33`.
**Files:** `internal/agent/inflight_submit_deadline_test.go` and the declared
manifest. Nothing else: no production `.go`, no `execution_deadline_test.go`,
no `testhelpers`, no guard.

Deliverable (local directions under d41):

- Add a colocated helper that holds `ops.WithProjectLifecycleExclusiveLock`
  in a goroutine for a given duration and returns once the lock is held.
  A submit started meanwhile then waits before writing its marker.
- Extend `lateSubmitSession` with a stall duration (zero means none). When it
  is non-zero, the agent stalls preparation after `startDelay` and then
  submits. The D41 call sites pass zero; this is a mechanical change.
- `TestExecuteAgentBlocksUnadmittedLateSubmitAfterHandback` runs two
  subtests, both with `holdMarker` true and a 500 ms start: "prompt start"
  (no stall) and "stalled preparation" (stall 1.5 s, past the 1 s timer).
  Every assertion and value is kept.
- `TestExecuteAgentLetsLateSubmitPublishPastSupervisorClocks`:
  - Before `executeAgent`, write this session's marker with
    `ops.WriteInflightSubmitMarkerForTest` (deadline now+10 min, removed in
    `t.Cleanup`).
  - The agent releases the marker right after the submit returns, in both
    select branches and before it waits for `ctx.Done()`. The hand-back
    therefore starts as before, and the no-markers assertion still holds.
  - Two subtests: "prompt start" (0.5 s idle) and "stalled preparation"
    (0.5 s idle, then a 2.5 s stall, past both the 1 s progress and the 2 s
    execution clocks).
- Update the doc comments. The held marker stands for the session's submit,
  and the real submit's marker is asserted in `internal/ops`
  (`budget_exception_test.go:244`, `submission_admission_test.go:125`).
- Red first. Add both "stalled preparation" subtests to the current
  no-held-marker shapes and capture the two exact signatures: "the session
  was cancelled before the agent saw its submit's outcome", and "supervisor
  cancelled the session before the submit published (marker=false
  receipt=false". Then add the held marker.

**Done when:** each named test runs "prompt start" and "stalled preparation".
The stalled subtests fail on the old shape with the signatures above (in the
checkpoint) and pass afterwards. The four undeferred commands pass. Every row
of the traceability table for the agent tests holds. The diff touches only
the two declared files.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/deferred-validation-16-1-d41-correction-plan-agent-inflight.json",
  "obligations": ["AG-unadmitted", "AG-publish", "AG-d41-retained", "AG-package", "AG-sleep", "AG-lint", "AG-preserve", "AG-red"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=20 -timeout 30m -run '^TestExecuteAgent(BlocksUnadmittedLateSubmitAfterHandback|LetsLateSubmitPublishPastSupervisorClocks|ProgressWatchdogStillBlocksDuringHandback|BlocksSubmitStillRunningWhenGraceEnds)$' ./internal/agent/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 -timeout 30m ./internal/agent/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -run '^TestSleepUsageBudget$' ./internal/testguard/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-e2e",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-race"
  ],
  "timeout_seconds": 5400,
  "deferred": [4, 5, 6],
  "approved_proofs": [
    {"obligation_id": "AG-preserve", "reference_id": "d41", "rationale": "Independent diff review against the Assertion Traceability table confirms that every original assertion and value is kept, that no production, guard or helper-package file changed, that the D41 tests changed only at call sites and that no budget was widened; a green run cannot prove absence of weakening."},
    {"obligation_id": "AG-red", "reference_id": "d41", "rationale": "A passing receipt cannot show the pre-correction failure; the reviewer checks that the checkpoint records the stalled-preparation subtests failing on the old shape with both exact signatures."}
  ]
}
```

## Task 2: Synchronize the rejected new-attempt await test on watcher registration

**Priority:** Must (d41 lineage; task priority 1). **Depends on:** nothing.
The earliest input is merged source; the seam `newAwaitVerdictWatcher`
(`await_verdict.go:60`) and its precedent (`await_verdict_test.go:443-492`)
already exist.
**Files:** `internal/ops/await_verdict_test.go` and the declared manifest.

Deliverable:

- `TestAwaitVerdict_Rejected_NewAttempt` stops calling `t.Parallel()`,
  because it swaps a package seam (as the precedent at `:443` does). It
  wraps `newAwaitVerdictWatcher`: the wrapper builds the original real
  watcher, closes a `ready` channel and is restored on cleanup.
- Replace `testhelpers.WaitForAsyncSetup()` with a wait on `ready`. The wait
  may carry a failure ceiling, as at `:488-492`.
- Two non-parallel subtests: "prompt setup", and "slow setup", in which the
  wrapper waits 1 s or more (`time.After`) before it builds the watcher.
  Both keep the `AwaitVerdict` entry point, the 10 s budget, the default
  poll and all four assertions.
- Red first. Add "slow setup" to the current sleep-synchronized shape and
  capture `Verdict = "TIMEOUT", want NEW_ATTEMPT`. Then switch to the
  `ready` wait.
- Leave the other eight `WaitForAsyncSetup` uses unchanged (RCA f.).

**Done when:** both subtests pass. "Slow setup" failed with `TIMEOUT` on the
old shape (in the checkpoint). The four undeferred commands pass. The
traceability rows hold. Only the two declared files change.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/deferred-validation-16-1-d41-correction-plan-ops-await-verdict.json",
  "obligations": ["AW-verdict", "AW-package", "AW-sleep", "AW-lint", "AW-preserve", "AW-red"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=20 -run '^TestAwaitVerdict_Rejected_NewAttempt$' ./internal/ops/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 -timeout 30m ./internal/ops/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -run '^TestSleepUsageBudget$' ./internal/testguard/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-e2e",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-race"
  ],
  "timeout_seconds": 3600,
  "deferred": [4, 5, 6],
  "approved_proofs": [
    {"obligation_id": "AW-preserve", "reference_id": "d41", "rationale": "Independent diff review against the Assertion Traceability table confirms the four assertions and every scenario value are kept, the entry point and poll interval are unchanged, only t.Parallel and the sleep-based wait were replaced, and no production file changed."},
    {"obligation_id": "AW-red", "reference_id": "d41", "rationale": "A passing receipt cannot show the pre-correction failure; the reviewer checks that the checkpoint records the slow-setup subtest returning TIMEOUT on the old shape."}
  ]
}
```

## Task 3: Derive the checkpoint recency fixture's attempt time from its own pass record

**Priority:** Must (d41 lineage; task priority 1). **Depends on:** nothing.
The earliest input is merged source.
**Files:** `internal/ops/sprint_checkpoint_test.go` (only the case table of
`TestCheckpointPassedPlansRefusesUnqualifiedState`, plus at most one
uniquely named colocated helper) and the declared manifest. `passedPlanState`
is unchanged.

Deliverable:

- The "attempted after its pass" mutate sets `TransitionsAttemptedAt` to the
  fixture's recorded pass time plus one minute
  (`s.Tasks[0].PlanCheck.At.Add(time.Minute)`). That pass time is also the
  time of its `plan_check` history entry. The parent-captured `now` is no
  longer used for this case.
- Add the case "attempted after its pass, fixture built late". Its mutate
  first moves the pass record (`PlanCheck.At` and its `plan_check` history
  entry) two minutes later, which emulates a subtest that starts two minutes
  after its parent, and then applies the same rule. Its assertions are the
  same as the other cases'.
- The other cases keep `now` unchanged.
- Red first. Add the late case with the current rule (attempt at the
  parent's `now`) and capture `err = <nil>, want ErrNoPassedPlanCheckpoint`.
  Then change the rule.

**Done when:** all six cases pass. The late case failed on the old rule (in
the checkpoint). The four undeferred commands pass. The traceability rows
hold. Only the two declared files change.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/deferred-validation-16-1-d41-correction-plan-ops-checkpoint-recency.json",
  "obligations": ["CK-recency", "CK-package", "CK-sleep", "CK-lint", "CK-preserve", "CK-red"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=20 -run '^TestCheckpointPassedPlansRefusesUnqualifiedState$' ./internal/ops/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 -timeout 30m ./internal/ops/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -run '^TestSleepUsageBudget$' ./internal/testguard/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-e2e",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-race"
  ],
  "timeout_seconds": 3600,
  "deferred": [4, 5, 6],
  "approved_proofs": [
    {"obligation_id": "CK-preserve", "reference_id": "d41", "rationale": "Independent diff review against the Assertion Traceability table confirms every case keeps its assertions and expected error, the attempt stays one minute after the pass, other cases and passedPlanState are unchanged, and no production file changed."},
    {"obligation_id": "CK-red", "reference_id": "d41", "rationale": "A passing receipt cannot show the pre-correction failure; the reviewer checks that the checkpoint records the late case returning nil under the old rule."}
  ]
}
```

## Acceptance Manifest Map

| Obligation | Task | Command index | Assertion to map |
|---|---|---|---|
| AG-unadmitted | 1 | 1 | unadmitted test, both subtests |
| AG-publish | 1 | 1 | publish test, both subtests |
| AG-d41-retained | 1 | 1 | the two D41 tests, unchanged assertions |
| AG-package | 1 | 2 | internal/agent under race |
| AG-sleep | 1 | 3 | TestSleepUsageBudget at 16 |
| AG-lint | 1 | 0 | gofmt, vet, archguard |
| AG-preserve, AG-red | 1 | approved (d41) | — |
| AW-verdict | 2 | 1 | NEW_ATTEMPT test, both subtests |
| AW-package | 2 | 2 | internal/ops under race |
| AW-sleep | 2 | 3 | TestSleepUsageBudget at 16 |
| AW-lint | 2 | 0 | gofmt, vet, archguard |
| AW-preserve, AW-red | 2 | approved (d41) | — |
| CK-recency | 3 | 1 | recency test, all cases incl. late |
| CK-package | 3 | 2 | internal/ops under race |
| CK-sleep | 3 | 3 | TestSleepUsageBudget at 16 |
| CK-lint | 3 | 0 | gofmt, vet, archguard |
| CK-preserve, CK-red | 3 | approved (d41) | — |

Deferred indexes 4–6 (`make test`, `make test-e2e`, `make test-race`) map to
no obligation. The integration batch runs them, and they are the commands
that were red in batches 15 and 16. `make test-e2e` compiles neither
package's `_test.go` files (RP11). It stays listed as a regression-only
suite.

## Dependencies and Shared Files

The three outputs edit three different files, so they have no `depends_on`
between them and run in parallel. Output 0 waits for the merged earlier
writer of its file (`task_depends_on`). No output has a provider wait. Their
order against other writers and consumers is set by `## Placement` below,
using declared waits only: no commutativity claim and no submit-time rebase
orders anything. Provider Ask Inventory: N/A, because no interface is frozen.

| File | Earlier writers | Later writers (unstarted) |
|---|---|---|
| `internal/agent/inflight_submit_deadline_test.go` | `deferred-validation-2-1-correction-plan-code-0` (MERGED `aaf2c5e33`) | none named by a plan; any A3–A5 writer descends from a reserved successor |
| `internal/ops/await_verdict_test.go` | `9c685b643` (direct commit, no task) | `arm-1-ar-14-cp-0-code-4` (moves white-box tests with `await_verdict.go`, including the `newAwaitVerdictWatcher` seam Task 2 wraps) |
| `internal/ops/sprint_checkpoint_test.go` | `1508df457` (direct commit, no task) | `arm-1-ar-13-cp-5-code-0` (colocated test of the checkpoint split) |

## Placement

Correction case: the correction placement rule of A4
(`specs/arch-plan/20261023-architecture-consolidation/20261023-205627-arm-1-ar-13.md#Proof and Shared Ownership`),
A3 (`…/20261023-203240-arm-1-ar-12.md`, lines 163–166) and A2
(`…/20261023-195624-arm-1-ar-11.md`, lines 523–528). The three rules agree. A correction waits
for every started or merged writer and precedes every unstarted consumer whose
done_when requires it. It takes the earliest legal slot. The slot lies at the
head of the A2 chain, and the successors span A2 to A5, so all three rules
apply. Correction lineage: `deferred-validation-16-1-d41-correction-plan-amend-1`.

**Required successor test.** A consumer requires these corrections when:

- **R1:** it edits a corrected file, or a definition a correction uses: the
  `TestCheckpointPassedPlansRefusesUnqualifiedState` case table, the
  `newAwaitVerdictWatcher` seam or `lateSubmitSession`; or
- **R2:** its done_when or Acceptance Contract needs one of these to pass:
  - green undeferred `make test`;
  - an undeferred `go test` over `./internal/agent/` or `./internal/ops/`
    with no `-run` filter, which executes the affected tests;
  - area-boundary green evidence recorded at an integration SHA.

  Batches 15 and 16 observed all four failures under load (RCA a.). The
  mechanisms depend on package scheduling and host latency (RCA b.), so any
  unfiltered run of either package can hit them.

A unit whose undeferred commands execute none of the four tests is not a
consumer. For example, `arm-1-ar-10-cp-1-code-2` runs no `internal/agent`
or `internal/ops` command.

**Predecessors (declared waits).**

- `deferred-validation-2-1-correction-plan-code-0` (MERGED) for output 0, as
  `task_depends_on`.
- The two ops files have no task writer: their last writers are direct
  commits (table above). Outputs 1 and 2 have no wait.
- Started tasks: only `arm-1-ar-10-cp-1-code-1` (IMPLEMENTING_CODE,
  claimed 2026-10-24T12:12:50Z).
  - It writes none of the three files, committed or uncommitted (amendment
    RP3), so it is not a predecessor.
  - Its index 1 is undeferred and runs `go test -race` over
    `./internal/agent/` and `./internal/ops/` with no `-run` filter. Under R2
    it is therefore a **started required successor**, which the rules route
    to a human ruling.
  - The ruling is requested through this task's BLOCKED state and is
    recorded here before resubmission. **Ruling: PENDING.**

**Earliest legal slot.** The slot is now. It follows the merged D41 writer
and precedes `arm-1-ar-11-cp-0-code-0`, the first required consumer and the
first slot of the A2 chain. No fixed or merged slot is crossed: every A2–A5
coding unit is DRAFT_CODE, and the six successors are unassigned with empty
history (amendment RP4).
Apart from the started successor above, the corrections may run in parallel
with A1's remaining units, which are not consumers.

**Successor reservations.** The orchestrator reserves each successor below
onto provider `deferred-validation-16-1-d41-correction-plan`, transition
`code-plan-to-coding` (per-subtask), outputs `[0, 1, 2]`:

```json
{"provider_task": "deferred-validation-16-1-d41-correction-plan", "transition": "code-plan-to-coding", "outputs": [0, 1, 2]}
```

| Successor | Test | Need |
|---|---|---|
| `arm-1-ar-11-cp-0-code-0` (A1 boundary) | R2: `make test` at index 1, undeferred; G1–G7 at the integration revision | earliest required consumer; every open A2–A5 coding unit descends from it |
| `arm-1-ar-12-cp-0-code-0` (A2 boundary) | R2: `make test` at index 1; "Failures hold A3"; already waits on the D41 writer for the same full suite | head of every open A3–A5 coding unit |
| `arm-1-ar-13-cp-0-code-0` (A3 boundary) | R2: `make test` at index 1 | boundary evidence |
| `arm-1-ar-14-cp-0-code-0` (A4 boundary) | R2: `make test` at index 1 | boundary evidence |
| `arm-1-ar-13-cp-5-code-0` | R1 on output 2 (`sprint_checkpoint_test.go`); R2: `make test` at index 0, ops race at index 3 | non-commuting writer |
| `arm-1-ar-14-cp-0-code-4` | R1 on output 1 (`await_verdict.go` seam and white-box tests); R2: `make test` at index 0, agent/ops race at index 3 | non-commuting writer |

Each successor selects all three outputs. Its undeferred full suite runs all
four tests, and each mechanism depends on host latency, not on the race flag
(RCA b.).

Every other R2 consumer is ordered through recorded `depends_on` edges, not
through prose. These are the A3–A5 refactor units with undeferred `make test`.
Each of them descends from `arm-1-ar-12-cp-0-code-0` (amendment RP5). A
dependency repair that removes such a path must re-check this section.

**Seam re-weighed with the correction included.**

- `sprint_checkpoint_test.go`: Task 3 runs first, then cp-5-code-0. cp-5
  keeps every passed-plan test and moves white-box assertions with code, so
  it carries the corrected case table.
- `await_verdict_test.go`: Task 2 runs first, then A5 code-4. If code-4 moves
  `newAwaitVerdictWatcher`, it moves the test's ready-wait wrapper with it,
  under the same white-box rule.

One test function's case table and one seam wrapper do not justify an
additive seam. The chain still wins.

**Cycle and unborn-child analysis.**

- The only wait the outputs carry is on `deferred-validation-2-1-correction-plan-code-0`
  (MERGED, no dependencies). The provider is MERGED and has no
  `depends_on`, so the children inherit no upstream phase.
- No reserved successor is an ancestor of the provider, of this amendment or
  of the D41 writer (amendment RP5). Therefore no cycle exists, whether
  through production, inheritance or provider waits.
- The children are unborn. They are generated only after
  `amend-plan --apply` and the manual `code-plan-to-coding` transition.
  Until the provider's transition has executed and children 0–2 have
  merged, every reserved successor holds. This is the intended order.
- Indexes 0–2 exist, and this amendment appends no slot, so the selections
  are stable.

**Orchestrator order.**

1. Review and merge this correction.
2. Recheck each successor. If any has started, stop: a started required
   successor needs a human ruling.
3. `reserve-provider` each successor.
4. `amend-plan deferred-validation-16-1-d41-correction-plan --apply
   deferred-validation-16-1-d41-correction-plan-amend-1`.
5. A human fires `code-plan-to-coding`.

**Unfiltered package-race consumers in A2.** `arm-1-ar-11-cp-4-code-0/6/7/8`
run undeferred, unfiltered agent/ops races, so they are R2 consumers. They
descend from the reserved `arm-1-ar-11-cp-0-code-0` through recorded edges
(amendment RP5), so they are ordered after the corrections and need no added
wait.

**Open item.** The started required successor `arm-1-ar-10-cp-1-code-1`
needs a human ruling (see Predecessors).

## Reality Probes

Integration/worktree snapshot `53504b7d81d44533d290f4132f7cf4ba2b7d1705`,
2026-10-24 UTC, 8 CPUs. The probe files under `/tmp/dv161-probe/` were used
only through `-overlay`, and the worktree stayed clean. Their sha256 values:
`agent_probe_test.go` 310cf07f…e21c, `agent_stall_probe_test.go`
d9240928…344d, `ops_probe_test.go` 48bab504…2591.

| ID | Command / source | Observation |
|---|---|---|
| RP1 | `git rev-parse HEAD`; `git status --short` | 53504b7d8…, clean. |
| RP2 | Batch logs (RCA a.) | Lines cited; agent FAIL lines 794/797/794, ops FAIL 1710 (batches 16-race, 15-race); batch 16 `make test` ops ok (821). |
| RP3 | `omni-ee get human_notes` (11:27:21.95Z) and the D41 code task history | Operator note as quoted. The round-1 review ran the unchanged unadmitted test, and the receipt maps it as unchanged in behavior. |
| RP4 | `go test -race -overlay … -run '^TestProbe(UOldLate\|UHeld\|POldLate\|PHeld)' ./internal/agent/` 11:52Z | A late start before the submit fails before the submit starts (a different line). Held shapes pass. This is why the regression uses a preparation stall rather than a start delay. |
| RP5 | `go test -race -count=1 -overlay … -run '^TestProbe' ./internal/ops/` 11:51Z | `TestProbeAwaitOldSlowSetup` fails 10.29 s: `Verdict = "TIMEOUT", want NEW_ATTEMPT`. Ready-sync passes in 1.47 s. `TestProbeCheckpointOldLate` fails: `err = <nil>, want ErrNoPassedPlanCheckpoint`. Relative rule passes, late and prompt. |
| RP6 | `make -n test-race lint` | `go test -race -timeout 30m -p 2 ./...`; gofmt, three-GOOS vet, archguard. |
| RP7 | `go test -race -count=1 -overlay … -run '^TestProbe(UStall\|PStall)' ./internal/agent/` 11:54Z, load 9.4 | Old shapes: "Agent execution timeout … 1s" then "the session was cancelled before the agent saw its submit's outcome"; "Execution progress timeout … 1s" then "supervisor cancelled the session before the submit published". Held shapes pass, logging "Deferring execution timeout". |
| RP8 | `go test -race -count=20 -overlay … -run '^TestProbe(UHeldPrompt\|UStallHeld\|PHeldPrompt\|PStallHeld)$' ./internal/agent/` 11:54–12:01Z, load 6.7–8.9 | ok 387.4 s, 80 runs. |
| RP9 | `go test -race -count=20 -overlay … -run '^TestProbe(AwaitReadySlowSetup\|CheckpointRelativeLate\|CheckpointRelativePrompt)$' ./internal/ops/` 12:01Z | ok 27.0 s, 60 runs. |
| RP10 | `go test -count=1 -run '^TestSleepUsageBudget$' -v ./internal/testguard/` (index 3) | PASS. No `time.Sleep(` in the three target files (`grep -c` 0 each). |
| RP11 | `go test -list` with the index 1 regexes (agent also with `-race -timeout 30m`, iteration 2); `make -n test-e2e` | Selects the four agent tests and each ops test. e2e = `go test -race -tags e2e -run '^(TestFullSprintSequence\|TestTerminalDependencyRecovery)$' ./internal/integration/`. |
| RP12 | `git grep -c 't.Parallel()' -- 'internal/ops/*_test.go'` | 1619 calls. Package wall times are 359–428 s under race in batches 15/16. |
| RP13 | `models/config.go:94,100` | Default await poll is 10 s; default agent progress timeout is 1800 s. |

**Budget basis (estimate):** `make lint` was not rerun; the cited D41 receipt
ran it in 2 s with a warm cache, and 900 s is allowed for a cold three-GOOS
vet.

- Task 1: index 1 ≈ 30 s per iteration × 20 (RP8 gives 19.4 s for the four
  new subtests, plus about 10 s for the D41 tests) ≈ 600 s, ×1.5 for load
  = 900 s. Index 2 ≈ 450 s (batch logs), allowed 600 s. Index 3 ≈ 70 s with
  build. The sum is about 2470 s; ×2 headroom gives 5400 s.
- Tasks 2 and 3: index 1 ≈ 200 s with build (RP9). Index 2 ≈ 430 s (batch
  logs), allowed 600 s. Index 3 ≈ 70 s. The sum is about 1770 s; ×2 gives
  3600 s.

Deferred commands do not count toward admission. All budgets are under the
14400 s cap.

**Runner envelope.** Go's default test-binary timeout is 10 min (`go help
testflag`), and `timeout_seconds` bounds only the outer batch. The
estimates above leave too little margin under that default:

- Task 1 index 1: RP8 measured 387 s for the new shapes, and the D41
  receipt measured 214 s for the retained tests.
- Package races: up to 452 s (agent) and 428 s (ops) in the batch logs.

Task 1 index 1 and every package race (index 2 of each task) therefore pass
`-timeout 30m`, the same runner bound that `make test-race` uses (RP6). The
worst case still fits each outer envelope: Task 1 is 900 + 1800 + 1800 +
70 = 4570 s against 5400 s, and Tasks 2–3 are 900 + 200 + 1800 + 70 =
2970 s against 3600 s. This aligns the runner with the estimate only: no
`count`, scenario deadline or assertion changes. The short ops focused runs
(≈ 27 s, RP9) and the guard keep the default.

## Spec Compliance Matrix

| # | Requirement (priority) | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Must: RCA or bounded exclusion for the unadmitted late-submit test | task DONE WHEN; d41-adjacent | RCA 1; Task 1 | Covered |
| 2 | Must: RCA or exclusion for the publish-past-clocks test | task DONE WHEN; d41-adjacent | RCA 1; Task 1 | Covered |
| 3 | Must: RCA or exclusion for the rejected new-attempt await test | task DONE WHEN | RCA 2; Task 2 | Covered |
| 4 | Must: RCA or exclusion for passed-plan checkpoint recency | task DONE WHEN | RCA 3; Task 3 | Covered |
| 5 | Must: distinct causes in bounded outputs | task DONE WHEN | Tasks 1–3 | Covered |
| 6 | Must: regression proof exposes the old defect and passes after correction | task DONE WHEN; d41 | Tasks 1–3 (red-first, -red obligations) | Covered |
| 7 | Must: event synchronization or controlled time, not wall-clock budgets | d41 Ruling | Tasks 1–3 | Covered |
| 8 | Must: every assertion and asserted value preserved | operator note; d41; goal-tests | Traceability; -preserve obligations | Covered |
| 9 | Must: maxSleepCallsInTests stays 16; guard unedited | operator note; d46 | -sleep obligations, index 3 | Covered |
| 10 | Must: no production change without a proven defect | operator note; d41 | RCA b. assessment; task scopes | Covered |
| 11 | Must: no skip, known-red, budget-only widening or oracle weakening | d41 Rejected | Common Rules; -preserve | Covered |
| 12 | Must: focused race runs at count ≥ 20 under env -i | task DONE WHEN | index 1 of each task | Covered |
| 13 | Must: package race for internal/agent and internal/ops | task DONE WHEN | Task 1 index 2; Tasks 2–3 index 2 | Covered |
| 14 | Must: make lint, test, test-e2e and test-race under the deferred policy | task DONE WHEN; goal-proof E1 | indexes 0, 4–6 (4–6 deferred) | Covered |
| 15 | Must: acceptance manifest mapping every obligation | task DONE WHEN | Acceptance Manifest Map; three manifests | Covered |
| 16 | Must: reviewable original-to-retained assertion traceability | task DONE WHEN | Assertion Traceability | Covered |
| 17 | Must: explicit D41 lineage; no correction against arm-1-ar-13-cp-3 | task DESCRIPTION | Intent and Authority | Covered |
| 18 | Must: proof in task worktrees under env -i; no env values in messages | task SCOPE; guard-tier1 G1.4; goal-proof E4 | Common Rules | Covered |
| 19 | Must: representative-load proof | task DONE WHEN | stalled/slow/late variants (controlled latency) plus RP8–RP9 under load | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: test-only change with no behavior change; `make test-e2e` stays as a deferred regression | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: no behavior change; this plan is the RCA record | N/A |
