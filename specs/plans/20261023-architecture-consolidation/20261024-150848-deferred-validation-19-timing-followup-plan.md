# Code Plan: Uncovered Wall-Clock Signatures in the Idle-Checkpoint and Await-Verdict Tests

## Source References
Source revision: "01cf6c30decf85192ab594cf20f656bc6538f91a"

### Direct References
- "goal-tests": "specs/goals/20261023-architecture-consolidation.md#Test Policy and Sequencing" @ "01cf6c30decf85192ab594cf20f656bc6538f91a"
- "goal-proof": "specs/goals/20261023-architecture-consolidation.md#Proof Environment" @ "01cf6c30decf85192ab594cf20f656bc6538f91a"
- "d41": "specs/goals/20261023-architecture-consolidation-decisions.md#D41 — Stabilise two inherited timing-flaky internal/agent tests (run, 2026-10-24)" @ "01cf6c30decf85192ab594cf20f656bc6538f91a"
- "d46": "specs/goals/20261023-architecture-consolidation-decisions.md#D46 — Remove the time.Sleep calls merged by A9 oracle rows; the sleep budget stays (run, 2026-10-24)" @ "01cf6c30decf85192ab594cf20f656bc6538f91a"
- "d51": "specs/goals/20261023-architecture-consolidation-decisions.md#D51 — Started successor `arm-1-ar-10-cp-1-code-1` runs before the D41 follow-up correction; bounded resubmission for the named flakes (run, 2026-10-24)" @ "01cf6c30decf85192ab594cf20f656bc6538f91a"
- "d41-rules": "specs/plans/20261023-architecture-consolidation/20261024-120308-deferred-validation-16-1-d41-correction-plan.md#Common Rules (all outputs)" @ "01cf6c30decf85192ab594cf20f656bc6538f91a"
- "d41-task2": "specs/plans/20261023-architecture-consolidation/20261024-120308-deferred-validation-16-1-d41-correction-plan.md#Task 2: Synchronize the rejected new-attempt await test on watcher registration" @ "01cf6c30decf85192ab594cf20f656bc6538f91a"
- "d41-placement": "specs/plans/20261023-architecture-consolidation/20261024-120308-deferred-validation-16-1-d41-correction-plan.md#Placement" @ "01cf6c30decf85192ab594cf20f656bc6538f91a"
- "a4-proof": "specs/arch-plan/20261023-architecture-consolidation/20261023-205627-arm-1-ar-13.md#Proof and Shared Ownership" @ "01cf6c30decf85192ab594cf20f656bc6538f91a"
- "guard-tier1": "GUARDRAILS.md#Tier 1 (Hard Constraints)" @ "01cf6c30decf85192ab594cf20f656bc6538f91a"

### Obligation Coverage
- "CI-emit" -> "goal-tests", "d41"
- "CI-package" -> "goal-proof", "d41"
- "CI-sleep" -> "d46"
- "CI-lint" -> "goal-proof", "guard-tier1"
- "CI-preserve" -> "goal-tests", "d41", "d41-rules"
- "CI-red" -> "d41", "d41-rules"
- "CI-load" -> "d41", "goal-proof"
- "AT-terminal" -> "goal-tests", "d41", "d41-task2"
- "AT-reclaim" -> "goal-tests", "d41", "d41-task2"
- "AT-package" -> "goal-proof", "d41"
- "AT-sleep" -> "d46"
- "AT-lint" -> "goal-proof", "guard-tier1"
- "AT-preserve" -> "goal-tests", "d41", "d41-rules"
- "AT-red" -> "d41", "d41-rules"
- "AT-load" -> "d41", "goal-proof"

## Intent and Authority

Based on: the task JSON (orchestrator assessment of the operator note of
2026-10-24T14:40:18.918841977Z), the three named logs under
`.omni-ee/tool-results/` (read-only), source at `01cf6c30d`, the D41 plan and its
merged children, D41/D46/D51, the A4 placement rule, live task state read with
`omni-ee get`, and the probes below.

**Lineage.** This plan is a sibling of the D41 family, not a correction to any
of it. It plans only the three signatures the D41 children do not own:

| Signature | Observed | Owner before this plan |
|---|---|---|
| `TestOrchestratorWaitForWork_EmitsForACheckpointCreatedWhileIdle` (`internal/agent/checkpoint_summary_gate_test.go:261`) | batch 19 `make test-race` at `0778c1f71` | none |
| `TestAwaitVerdict_Terminal` (`internal/ops/await_verdict_test.go:647` at `0164cbbc1`, now `:680`) | batch 18 `make test-race` at `0164cbbc1` | none |
| `TestAwaitVerdict_RejectedAutoReclaim_PostWorktreeCmdFailureDegradesAgent` (`:1635` at `0164cbbc1`, now `:1668`) | batch 18 `make test-race` | none |

It does not touch `deferred-validation-16-1-d41-correction-plan-code-2` (merged,
`sprint_checkpoint_test.go`), code-0's in-flight submit tests or code-1's
`TestAwaitVerdict_Rejected_NewAttempt`. code-1's watcher-registration pattern
(`await_verdict_test.go:531-634`, commits `ee2d45df3`, `01cf6c30d`) is reused as
precedent only; that test and its inline wrapper stay byte-identical.

**Operator constraints (binding on both outputs):** keep every assertion and
semantic expected value, keep `maxSleepCallsInTests` at 16 without editing the
guard, and change no production code unless it is proven defective and
separately reviewed. No skip, known-red ruling, budget-only widening, oracle
edit or suppression. Do not retry until green or erase the ops signals.

Doc Impact: this plan, its output JSON and the two acceptance manifests the
children commit. Test Impact: two test files are rewritten in place, one per
child, each named scenario gaining a controlled-latency regression subtest. No
production file, guard, `internal/testhelpers` file or oracle file changes.
Priority: Must for both outputs (task priority 1); no optional work.

## Root Cause Analysis

Two distinct causes, one per output. Neither is a production defect.

### a. Evidence

Runtime (logs, read-only):

- `6c289bc1…dc6.txt` (batch 19 `make test-race`): line 35
  `--- FAIL: TestOrchestratorWaitForWork_EmitsForACheckpointCreatedWhileIdle (0.36s)`,
  line 36 `checkpoint_summary_gate_test.go:261: a checkpoint created while the
  orchestrator waited produced no report`; `FAIL internal/agent 484.838s`
  (line 797). Lines 33-34 and 37-52 log every checkpoint-summary emission of the
  neighbouring gate tests; none belongs to the failing test, and no
  `Failed to claim the checkpoint-summary obligation` line exists
  (`grep -c` = 0).
- `a67e413e…a56a41.txt` (batch 18 `make test-race` at `0164cbbc1`): line 1434
  `--- FAIL: TestAwaitVerdict_RejectedAutoReclaim_PostWorktreeCmdFailureDegradesAgent (10.37s)`,
  line 1435 `await_verdict_test.go:1635: AwaitVerdict() error = nil, want the
  setup failure surfaced`; line 1437 `--- FAIL: TestAwaitVerdict_Terminal (10.46s)`
  with lines 1438-1442: `Verdict = "TIMEOUT", want TERMINAL`, `Reason = ""`,
  `TaskStatus = "CODE_TO_REVIEW", want BLOCKED`, `SafeAction = ""`,
  `Guidance = ""`.
- `3c21d7ab…48acf.txt` (batch 19 `make test`): its only failure is
  `TestExecuteAgentBlocksUnadmittedLateSubmitAfterHandback` (line 121), now
  merged code-0 scope; none of the three tests fails there (RP2).
- Load averages 27-38 on 8 cores: operator-reported. Independently observed
  here: `/proc/loadavg` 35.77 at 15:08Z during this planning session (RP9).

Source (read this session, `01cf6c30d`):

- `checkpoint_summary_gate_test.go:249-262`: a 300 ms `context.WithTimeout`,
  then `WaitForWork(..., 50ms, 200ms)`; `hasWork` false is fatal (:257), the
  report assertion is :260-262. The test discards WaitForWork's error (:253).
- `strategy_orchestrator.go:155-167`: `LoadDetectionContext` runs before
  `waitForWorkEventDriven`; `:187` the check closure is the only emitter.
- `waitforwork.go:84-88`: a done context returns `ctx.Err()` before the first
  `checkWork` (:106); the 200 ms `maxWait` deadline is armed at :90/:130.
- `checkpoint_summary.go:137-172`: emission claims a global slot and the
  obligation under `bb.Modify`, then runs the runner in a goroutine;
  `checkpoint_summary_test.go:60-68` waits for that goroutine at cleanup.
- `await_verdict_test.go:636-704` and `:1615-1690`: `t.Parallel()`,
  `AwaitVerdict(..., 10*time.Second)` in a goroutine, then
  `testhelpers.WaitForAsyncSetup()` (`testhelpers/utils.go:55-57`, a 200 ms
  sleep) before the state write.
- `await_verdict.go:117` initial state read; `:142-156` an already-BLOCKED task
  returns a different reason ("task already …"); `:222` watcher registration
  through `newAwaitVerdictWatcher` (`:60-62`); `:231` abort tick at the
  normalized poll interval; `:284-285` deadline → `VerdictTimeout` with the
  initially read status; `:528-538` the TERMINAL "non-awaitable status" result.
- `await_policy.go:15-35` and `models/config.go:94`: default poll 10 s.

### b. Mechanism

**C1 — idle-checkpoint test (agent).** The test's 300 ms context starts before
`WaitForWork`. Everything up to `waitforwork.go:84` (detection context load,
state read setup, scheduling) consumes that budget. When it exceeds 300 ms, the
pre-check returns `ctx.Err()` and `checkWork` never runs, so
`maybeEmitCheckpointSummary` is never called: `hasWork` is false (the fatal
guard passes) and `called` stays false (:261). Measured time to the first check
is 29-55 ms at idle, with and without `-race` (RP4, RP5), a 5-10x margin that
load of 27-38 on 8 cores plus `-p 2` package parallelism can consume. The
failing duration (0.36 s ≈ 300 ms + fixture setup) fits this path.

Excluded alternatives: a busy emission slot (every emission and opt-out in the
log is accounted for by the five earlier gate tests, each awaited at cleanup,
and no earlier-sorting test file sets an obligation: RP3); a claim failure (no
warning logged); a stop or missing obligation (fixture). The test discards the
error that would have shown `context deadline exceeded`.

**C2 — two await tests (ops).** The state write happens 200 ms after the
await goroutine starts, ordered only by a sleep. When the goroutine has done
its initial read (:117) but not yet registered the watcher (:222) at the
write, fsnotify delivers no event. Only the abort tick at the default 10 s poll
can observe it, and the 10 s deadline was armed first (:220, :228), so the
await returns TIMEOUT with the initially read status `CODE_TO_REVIEW` and empty
reason, safe action and guidance. That is all five Terminal lines. For the
auto-reclaim test, the TIMEOUT path returns a nil error, so the setup failure
is never reached (:1668). Both failing durations (10.37 s, 10.46 s) equal the
budget. This is code-1's mechanism (D41 lineage; `ee2d45df3`) in two tests it
did not own. A second, unobserved window closes with the same fix: a write
before the initial read makes Terminal return "task already BLOCKED", which
fails the reason assertion.

**Production assessment.** C1: returning before work on a cancelled context is
the documented contract (`waitforwork.go:80-83`). C2: the abort tick is the
documented cover for the registration gap (`await_verdict.go:231`; the same
design is stated at `waitforwork.go:53-58`); with the 540 s production budget, a
missed event costs at most one poll interval of latency, not a lost verdict.
Neither is proven defective, so no production scope is returned.

### c. Reproduction

Controlled, deterministic and verified in disposable diagnostics (RP4, RP6):

- C1: create the test's 300 ms context, wait 400 ms (`time.After`), then call
  `WaitForWork`. Result: `hasWork=false`, `err=context deadline exceeded`,
  runner never called — the :261 signature. With a cancel-only context and the
  same 400 ms delay the runner is called.
- C2: wrap `newAwaitVerdictWatcher` so it waits 1 s before building the real
  watcher, and keep the 200 ms sleep ordering. Terminal returns
  `Verdict="TIMEOUT"`, `Reason=""`, `TaskStatus="CODE_TO_REVIEW"`,
  `SafeAction=""`, empty guidance at 10.05 s; auto-reclaim returns a nil error
  and records no health at 10.13 s.

Task 1's "late entry" and Task 2's "slow setup" subtests are these
reproductions; their done_when requires each to fail on the old shape first.

### d. Verification scope

Checked: the source paths above; log lines cited; durations; both
reproductions; idle and race timing to the first check; that the two test
files and the related production files are identical between `01cf6c30d` and
integration `92d39398e` (RP1); that `waitWhilePaused` reads state and emits
before it consults its context (`systemctl.go:138-163`), so its three 300 ms
gate tests do not share C1 (partially read; not planned).

Not checked: which pre-check step consumed the time in batch 19 (no capture of
that run exists; the controlled reproduction is independent of where the time
went); whether `make test` without `-race` has ever hit these signatures
(not observed in the cited batch 19 `make test`; the measured margin is similar
without `-race`, so exposure is assumed); the operator's load figures before
15:08Z (operator-attributed).

### e. Sufficiency

C1: with a cancel-only context, the pre-check cannot fire before the first
`checkWork`, which emits synchronously; the wait stays bounded by `maxWait`
(200 ms) and the emission is observed through `waitForCheckpointSummary`. No
wall-clock path to "no report" remains in this test.

C2: the write is issued only after the real watcher has registered, which
happens after the initial read, so both the missed-event and the
write-before-read windows close. The auto-reclaim worktree setup runs inside
the event handler, where the deadline timer is not consulted, so its duration
cannot cause TIMEOUT. Residual: fsnotify event loss after registration falls
back to the 10 s tick, as in code-1's accepted correction.

### f. Adjacent findings (recorded, not planned)

- Six other `WaitForAsyncSetup` orderings in `await_verdict_test.go` share C2's
  shape and are not observed failing: `TestAwaitVerdict_Approved` (:309),
  `_Rejected_SameAttempt` (:386), `_Aborted` (:855), `_PartiallyApproved`
  (:1148, :1158), `_RaceGuard` (:1222).
- code-1's inline wrapper in `TestAwaitVerdict_Rejected_NewAttempt` could adopt
  Task 2's helper: a refactor of code-1 scope, not done here.
- `TestOrchestratorWaitForWork_EmitsForACheckpointCreatedWhileIdle` discards
  WaitForWork's error (:253), which hid `context deadline exceeded` in the
  batch 19 output.
- The production await registration gap costs up to one poll interval of
  verdict latency (C2 production assessment); a latency property, not a defect.

## Common Rules (both outputs)

Inherited from d41-rules unchanged: every original assertion and value kept and
traced; tests rewritten in place, not renamed, regression variants added as
subtests; red first with the exact failure lines in the checkpoint; no
`time.Sleep` (injected latency uses `time.After`, a wait for an event may carry
a failure ceiling); `internal/testguard/sleep_usage_test.go` unedited and
`maxSleepCallsInTests` 16; no environment values in messages (G1.4); proof under
`env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` in the task worktree;
`make test`, `make test-e2e`, `make test-race` listed once and deferred,
`make lint` undeferred.

Added here:

- **Representative load.** The injected latency (400 ms past the old 300 ms
  context; 1 s watcher setup past the old 200 ms sleep) reproduces the load
  effect deterministically in every iteration of the count=20 run. The coder
  also records `cat /proc/loadavg` immediately before and after index 1 in the
  checkpoint. The load reading is an observation, not a gate.
- **No production defect path.** If red-first does not reproduce the named
  signature, or the correction needs a production edit, stop and
  `mark-blocked` with the evidence; do not widen scope.

## Assertion Traceability (original → retained)

Original lines are at the failing batch SHA; current lines at `01cf6c30d`.

| Test | Original assertion (line) | Value | Retained in |
|---|---|---|---|
| EmitsForACheckpointCreatedWhileIdle (agent) | `hasWork` false, fatal (:257-259, same at `0778c1f71`) | false | both subtests, Task 1 |
| same | runner called: "produced no report" (:260-262; failed at :261) | called | both subtests |
| AwaitVerdict_Terminal (ops) | AwaitVerdict error nil (`0164`:643-645; now :676-678) | nil | both subtests, Task 2 |
| same | Verdict TERMINAL (:646-648; now :679-681) | `VerdictTerminal` | both subtests |
| same | Reason contains "non-awaitable status" (:649-651; now :682-684) | substring | both subtests |
| same | TaskStatus BLOCKED (:652-654; now :685-687) | BLOCKED | both subtests |
| same | SafeAction stop (:655-657; now :688-690) | `SafeActionStop` | both subtests |
| same | Guidance contains "Stop this session" (:658-660; now :691-693) | substring | both subtests |
| same | state readable (:662-665; now :696-699) | — | both subtests |
| same | CurrentTask nil (:666-669; now :700-703) | nil | both subtests |
| RejectedAutoReclaim_PostWorktreeCmdFailureDegradesAgent (ops) | error non-nil (:1634-1636; failed at :1635; now :1667-1669) | non-nil | both subtests, Task 2 |
| same | `errors.Is(err, ErrAgentDegraded)` (:1639-1641; now :1672-1674) | `ErrAgentDegraded` | both subtests |
| same | state readable (:1643-1646; now :1676-1679) | — | both subtests |
| same | AgentHealth entry present (:1647-1650; now :1680-1683) | present | both subtests |
| same | health.Reason (:1651-1653; now :1684-1686) | `AgentDegradedWorktreeSetupFailed` | both subtests |
| same | LastError single line (:1654-1656; now :1687-1689) | no `\n` | both subtests |

Scenario values kept. Agent: trigger `SPRINT_COMPLETE`, auto-resume false,
detector `WakeTriggerNone`, poll 50 ms, `maxWait` 200 ms, the
`orchestratorStrategy.WaitForWork` entry point and the fake runner. Ops: the
`AwaitVerdict` entry point, 10 s budget, default poll; Terminal's
READY_FOR_REVIEW → BLOCKED write with reason "Spec ambiguity" and the WAITING
coder fixture; auto-reclaim's iterations 10, review cycles 5, `PostWorktreeCmd`
`exit 1`, reason "Missing error handling", 30 min lease and
`code-reviewer-1`.

Changed, not asserted: Task 1 replaces the 300 ms context deadline with a
cancel-only context (the cause; the wait stays bounded by the unchanged
`maxWait`); Task 2 removes `t.Parallel()` from the two tests (package seam) and
replaces their 200 ms sleep ordering with the registration wait. No budget is
widened.

## Task 1: Make the idle-orchestrator checkpoint test independent of its context deadline

**Priority:** Must (task priority 1). **Depends on:** pending the Placement
ruling; under the recommended option A,
`task_depends_on: ["arm-1-ar-11-cp-0-code-0"]` (orders only; supplies no
input). The earliest input is merged source at `01cf6c30d`; the seams
`orchestratorWaitForWorkDetector` and `withFakeCheckpointSummaryRunner`
already exist.
**Files:** `internal/agent/checkpoint_summary_gate_test.go` and the declared
manifest. No production `.go`, no other test file, no `testhelpers`, no guard.

Deliverable:

- `TestOrchestratorWaitForWork_EmitsForACheckpointCreatedWhileIdle` runs two
  non-parallel subtests, each with its own fixture, fake runner and detector
  override: "prompt entry" and "late entry". "Late entry" waits 400 ms
  (`time.After`) after creating the context and before calling `WaitForWork`,
  standing in for the load-delayed pre-check.
- The context becomes `context.WithCancel(context.Background())`, cancelled at
  the end of each subtest. `WaitForWork` is still called with 50 ms / 200 ms.
  A failure ceiling (for example, a goroutine with a 30 s `time.After` select
  that fails the test) is allowed but not required.
- Both assertions keep their messages and values. Update the doc comment to
  say the wait is bounded by `maxWait`, not by a context deadline.
- Red first: add "late entry" to the current 300 ms shape and capture
  `a checkpoint created while the orchestrator waited produced no report` for
  that subtest; then switch to the cancel-only context.

**Done when:** both subtests pass; "late entry" failed on the old shape with the
:261 message (in the checkpoint, with the command and its nonzero result); the
four undeferred commands pass with `/proc/loadavg` recorded around index 1;
every agent row of the traceability table holds; only the two declared files
change.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/deferred-validation-19-timing-followup-plan-agent-idle-checkpoint.json",
  "obligations": ["CI-emit", "CI-package", "CI-sleep", "CI-lint", "CI-preserve", "CI-red", "CI-load"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=20 -run '^TestOrchestratorWaitForWork_EmitsForACheckpointCreatedWhileIdle$' ./internal/agent/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 -timeout 30m ./internal/agent/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -run '^TestSleepUsageBudget$' ./internal/testguard/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-e2e",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-race"
  ],
  "timeout_seconds": 3600,
  "deferred": [4, 5, 6],
  "approved_proofs": [
    {"obligation_id": "CI-preserve", "reference_id": "d41", "rationale": "Independent diff review against the Assertion Traceability table confirms both assertions, their messages and every scenario value are kept, that only the context construction, the subtest split and the doc comment changed, and that no production, guard or helper-package file changed; a green run cannot prove absence of weakening."},
    {"obligation_id": "CI-red", "reference_id": "d41", "rationale": "A passing receipt cannot show the pre-correction failure; the reviewer checks that the checkpoint records the late-entry subtest failing on the 300 ms shape with the exact :261 message."},
    {"obligation_id": "CI-load", "reference_id": "d41", "rationale": "A receipt does not record host load; the reviewer checks that every index 1 iteration ran the injected 400 ms late-entry latency and that the checkpoint records /proc/loadavg before and after index 1."}
  ]
}
```

## Task 2: Synchronize the terminal and rejected auto-reclaim await tests on watcher registration

**Priority:** Must (task priority 1). **Depends on:** pending the Placement
ruling; under option A, `task_depends_on: ["arm-1-ar-11-cp-0-code-0"]`. The
earliest input is merged source: the seam `newAwaitVerdictWatcher`
(`await_verdict.go:60`) and code-1's merged precedent
(`await_verdict_test.go:531-634`).
**Files:** `internal/ops/await_verdict_test.go` and the declared manifest.

Deliverable:

- Add one colocated helper in `await_verdict_test.go` that, given a setup
  delay, swaps `newAwaitVerdictWatcher` for a wrapper that optionally waits
  (`time.After`), builds the original real watcher, closes a ready channel, and
  restores the seam on cleanup. It returns the ready channel. Leave
  `TestAwaitVerdict_Rejected_NewAttempt` and its inline wrapper unchanged.
- `TestAwaitVerdict_Terminal` and
  `TestAwaitVerdict_RejectedAutoReclaim_PostWorktreeCmdFailureDegradesAgent`
  stop calling `t.Parallel()` and each run two non-parallel subtests, "prompt
  setup" and "slow setup" (1 s), each with its own fixture.
- Each subtest registers `t.Cleanup(func() { <-done })` after the helper, so
  the await goroutine ends before the seam is restored; replaces
  `testhelpers.WaitForAsyncSetup()` with a wait on the ready channel under a
  30 s failure ceiling (as at `:590-594`); and keeps every assertion.
- Red first: add "slow setup" to the current sleep-ordered shape and capture
  `Verdict = "TIMEOUT", want TERMINAL` (with the four associated lines) and
  `AwaitVerdict() error = nil, want the setup failure surfaced`; then switch to
  the ready wait.
- Leave the other six `WaitForAsyncSetup` uses unchanged (RCA f.).

**Done when:** all four subtests pass; both "slow setup" subtests failed on the
old shape with the signatures above (in the checkpoint); the four undeferred
commands pass with `/proc/loadavg` recorded around index 1; every ops row of the
traceability table holds; only the two declared files change.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/deferred-validation-19-timing-followup-plan-ops-await-verdict.json",
  "obligations": ["AT-terminal", "AT-reclaim", "AT-package", "AT-sleep", "AT-lint", "AT-preserve", "AT-red", "AT-load"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=20 -timeout 30m -run '^TestAwaitVerdict_(Terminal|RejectedAutoReclaim_PostWorktreeCmdFailureDegradesAgent)$' ./internal/ops/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 -timeout 30m ./internal/ops/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -run '^TestSleepUsageBudget$' ./internal/testguard/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-e2e",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-race"
  ],
  "timeout_seconds": 3600,
  "deferred": [4, 5, 6],
  "approved_proofs": [
    {"obligation_id": "AT-preserve", "reference_id": "d41", "rationale": "Independent diff review against the Assertion Traceability table confirms every terminal-result and degraded-health assertion and every scenario value are kept, that the AwaitVerdict entry point, 10 s budget and default poll are unchanged, that only t.Parallel and the sleep ordering were replaced, and that code-1's test and all production files are unchanged."},
    {"obligation_id": "AT-red", "reference_id": "d41", "rationale": "A passing receipt cannot show the pre-correction failure; the reviewer checks that the checkpoint records both slow-setup subtests failing on the sleep-ordered shape with the TIMEOUT and nil-error signatures."},
    {"obligation_id": "AT-load", "reference_id": "d41", "rationale": "A receipt does not record host load; the reviewer checks that every index 1 iteration ran the injected 1 s watcher-setup latency and that the checkpoint records /proc/loadavg before and after index 1."}
  ]
}
```

## Acceptance Manifest Map

| Obligation | Task | Command index | Assertion to map |
|---|---|---|---|
| CI-emit | 1 | 1 | idle-checkpoint test, prompt entry and late entry |
| CI-package | 1 | 2 | internal/agent under race |
| CI-sleep | 1 | 3 | TestSleepUsageBudget at 16 |
| CI-lint | 1 | 0 | gofmt, vet, archguard on the changed file |
| CI-preserve, CI-red, CI-load | 1 | approved (d41) | — |
| AT-terminal | 2 | 1 | Terminal test, both subtests: TERMINAL result and ownership release |
| AT-reclaim | 2 | 1 | auto-reclaim test, both subtests: ErrAgentDegraded and degraded health |
| AT-package | 2 | 2 | internal/ops under race, other WaitForAsyncSetup tests unchanged |
| AT-sleep | 2 | 3 | TestSleepUsageBudget at 16 |
| AT-lint | 2 | 0 | gofmt, vet, archguard on the changed file |
| AT-preserve, AT-red, AT-load | 2 | approved (d41) | — |

Deferred indexes 4-6 map to no obligation; the integration batch runs them.
`make test-race` is where all three signatures were observed.

## Ownership, Dependencies and Shared Files

Current writer and proof ownership (`omni-ee get`, 15:00Z):

| Item | Owner | State |
|---|---|---|
| `inflight_submit_deadline_test.go` signatures | `deferred-validation-16-1-d41-correction-plan-code-0` | MERGED at `92d39398e` (was IMPLEMENTING_CODE in the assessment) |
| `TestAwaitVerdict_Rejected_NewAttempt` | `…-code-1` | MERGED (`ee2d45df3`, `01cf6c30d`) |
| `sprint_checkpoint_test.go` recency | `…-code-2` | MERGED |
| the three signatures of this plan | Task 1 (agent), Task 2 (ops) | this plan |

| File | Earlier writers | Later writers (unstarted) |
|---|---|---|
| `internal/agent/checkpoint_summary_gate_test.go` | direct commits (`76562ebaf`, `0f797153c`); no task writer | none named; unborn A9/C6 children (`arm-1-ar-20-cp-0` CODE_PLANNING, `arm-1-ar-21-cp-0` DRAFT_CODING_PLAN) may move `waitforwork.go`/`checkpoint_summary.go` tests |
| `internal/ops/await_verdict_test.go` | `…-code-1` (MERGED) | `arm-1-ar-14-cp-0-code-4` (A5 Task 5, moves white-box tests with `await_verdict.go` and its seam) |

The two outputs edit different files and share no definition, so they have no
`depends_on` between them and run in parallel. Provider Ask Inventory: N/A, no
interface is frozen.

## Placement

Correction case: the A4 correction placement rule (a4-proof; A3 and A2 state
the same rule, as applied in d41-placement). A correction waits for every
started or merged writer, precedes every unstarted consumer that requires it,
and takes the earliest legal slot. **A started required successor needs a human
ruling.** One exists, so this plan is held for that ruling (status below).

**Required successor test** (d41-placement R1/R2, for these three tests): R1 —
it edits `checkpoint_summary_gate_test.go`'s idle test, the ops tests or the
`newAwaitVerdictWatcher` seam; R2 — an undeferred command executes them
(`make test`, or `go test` over `./internal/agent/` or `./internal/ops/` with
no excluding `-run`), or its done_when needs boundary green evidence. Exposure is
not limited to `-race`: the measured margin is similar without it (RCA b., d.).

**Predecessors.** Merged writer `…-code-1` (satisfied). No started writer: the
only started task is `arm-1-ar-11-cp-0-code-0`, whose scope excludes test files.

**Started required successor.** `arm-1-ar-11-cp-0-code-0` (A1 boundary,
IMPLEMENTING_CODE, claimed 14:52:13Z by coder-3) is R2: its C1
`make test` (index 1) is undeferred and runs all three tests, and its G8
maintainer CI runs them on Linux/Windows. D51 covers neither this task nor these
signatures. Options for the human:

- **A (recommended, D51 shape):** the boundary keeps running; both children
  wait on it (`task_depends_on`) and take the slot before the reserved A2
  consumers below. The ruling should state how the boundary treats a C1 or G8
  failure only on these three signatures (for example D51's bounded
  resubmission with names and lines recorded); its plan otherwise blocks on a
  failed G4. Cost: none on the critical path, because every A2 unit already
  waits on the boundary.
- **B:** hand the boundary back until both children merge; its evidence then
  includes the corrections. Cost: its in-progress G1-G7 work.
- **C:** children run now with no wait. Cost: a merge during the boundary's run
  moves its integration revision, which its plan answers by refreshing G1-G7.

**Status: held for the ruling.** This plan is committed in its task worktree
and the task is marked BLOCKED with a human action. The output JSON encodes
option A (`task_depends_on: ["arm-1-ar-11-cp-0-code-0"]`); a B or C ruling drops
that wait. The plan is submitted for review only after the ruling is recorded
here.

**Earliest legal slot under A.** After `arm-1-ar-11-cp-0-code-0` merges and
before the first unstarted R2 consumers of A2. `arm-1-ar-11-cp-1-code-0`
through `cp-3-code-3`, `cp-4-code-1`…`-5` and `-9` defer `make test` and run no
agent/ops command, so they are not consumers and may run in parallel (RP7).
`arm-1-ar-11-cp-4-code-0`, `-6` and `-7` run an undeferred, unfiltered race over
`./internal/agent/` and `./internal/ops/` at index 1. No fixed or merged slot is
crossed: all A2-A5 coding units are DRAFT_CODE.

**Successor reservations (orchestrator, after the ruling).** Provider
`deferred-validation-19-timing-followup-plan`, transition `code-plan-to-coding`
(per-subtask), outputs `[0, 1]`, reserved onto `arm-1-ar-11-cp-4-code-0`,
`arm-1-ar-11-cp-4-code-6` and `arm-1-ar-11-cp-4-code-7`:

```json
{"provider_task": "deferred-validation-19-timing-followup-plan", "transition": "code-plan-to-coding", "outputs": [0, 1]}
```

Every later R1/R2 consumer descends from those three through recorded edges
(RP7): `cp-4-code-8` and the A2 boundary `arm-1-ar-12-cp-0-code-0` list them;
`arm-1-ar-13-cp-0-code-0` lists the A2 boundary; `arm-1-ar-14-cp-0-code-0`
lists A3 units; `arm-1-ar-14-cp-0-code-4` (R1) descends through `code-3`…`code-1`
to `code-0`. A dependency repair that removes such a path must re-check this
section.

**Seam re-weigh.** Task 2 adds one test helper beside code-1's inline wrapper;
A5 code-4 moves both with `await_verdict.go` under the white-box rule. Task 1
changes one test function. Neither justifies an additive seam; the chain wins.

**Cycle and unborn-child analysis.** This planning root has no `depends_on`.
Under A the children wait only on `arm-1-ar-11-cp-0-code-0`, whose `depends_on`
lists A0-A1 and `arm-1-ar-2` units, none a reserved successor or this provider
(RP7), so no cycle exists. The children are unborn until the
`code-plan-to-coding` transition; until they merge, the reserved successors
hold. Unborn A9/C6 children that may later move the agent test descend from
later area chains; their descent from the reserved A2 units is UNVERIFIED and is
the residual: if one moves the test first, submit-time rebase routes the
conflict to that coder, who keeps both subtests under the white-box rule.

**Residual exposure (RP9).** One unfiltered `internal/ops` race at load 36
failed on a test this session could not name. It may be one of Task 2's
signatures or an uncovered one. Task 2's index 2 runs the same command, so an
uncovered ops flake under load would refuse its admission. In that case the
coder records the failing names and lines and stops for routing; that failure
is not Task 2 scope, and retrying until green is not allowed.

**Orchestrator order.** 1. Human ruling (A, B or C). 2. Resume this task: record
the ruling in this section and in `task_depends_on`, then submit for review.
3. After merge, recheck that the three reserved successors are unstarted; if one
has started, stop for a ruling. 4. `reserve-provider` each. 5. Fire
`code-plan-to-coding`.

## Reality Probes

Worktree `01cf6c30decf85192ab594cf20f656bc6538f91a`, clean before and after;
integration head `92d39398e` differs from it only in code-0's two files (RP1).
2026-10-24 UTC, 8 CPUs. Diagnostic test files were created in the worktree,
run, and deleted before any commit (`git status --short` empty after each).

| ID | Command / source | Observation |
|---|---|---|
| RP1 | `git diff --stat 01cf6c30d 92d39398e`; same with the two test files and `checkpoint_summary.go`, `strategy_orchestrator.go`, `waitforwork.go`, `await_verdict.go` | only `inflight_submit_deadline_test.go` and code-0's manifest; the six named files identical |
| RP2 | the three logs (RCA a.) | lines as cited; batch 18 `FAIL internal/ops 388.623s`, `FAIL internal/agent 447.782s`; batch 19 race `FAIL internal/agent 484.838s`, `ok internal/ops 603.295s`; batch 19 `make test` FAIL only at line 121 (code-0's test), `FAIL internal/agent 329.796s` (line 794) |
| RP3 | `grep -ln PendingCheckpointSummary internal/agent/*_test.go`; batch 19 log lines 33-52 | only the gate, `checkpoint_summary_test.go` and `claiming_checkpoint_test.go` set obligations, all sorting after or within the gate file; every emission accounted for |
| RP4 | disposable diagnostic, `go test -race -count=2 -v -run '^TestZZDiagCP2$' ./internal/agent/` 14:55Z | 300 ms ctx, prompt: `called=true firstCheck=55ms loadDetection=22ms`; 300 ms ctx + 400 ms late entry: `err=context deadline exceeded called=false`; cancel-only + 400 ms: `called=true` |
| RP5 | disposable diagnostic, `go test -count=5 -v -run '^TestZZDiagCP2FirstCheck$' ./internal/agent/` (no race) | first check 29-54 ms |
| RP6 | disposable diagnostic, `go test -race -count=1 -v -run '^TestZZDiagCP2(Terminal|AutoReclaim)$' ./internal/ops/` (1 s watcher-setup wrapper, 200 ms sleep) | Terminal `verdict="TIMEOUT" reason="" status="CODE_TO_REVIEW" safe="" guidanceEmpty=true elapsed=10.05s`; auto-reclaim `errNil=true verdict="TIMEOUT" healthRecorded=false elapsed=10.13s` |
| RP7 | `omni-ee get <id> --json` for the D41 children, `arm-1-ar-11-cp-0-code-0`, A2 units, A2-A5 boundaries, `arm-1-ar-14-cp-0-code-1…4`, `arm-1-ar-20/21-cp-0`; each A2 plan's Acceptance Contract `deferred` | statuses and edges as cited in Placement; A2 boundary validation has no `deferred`; `cp-4-code-0/6/7` index 1 lists `./internal/agent/ ./internal/ops/`, deferred `[3, 4]` |
| RP8 | `go test -race -count=3 -v -run '^TestOrchestratorWaitForWork_EmitsForACheckpointCreatedWhileIdle$' ./internal/agent/`; `go test -race -count=2 -v -run '^TestAwaitVerdict_(Terminal|RejectedAutoReclaim_PostWorktreeCmdFailureDegradesAgent)$' ./internal/ops/` (index 1 selections, lower count), load ≈ 1.5 | agent PASS 0.28-0.31 s each; Terminal PASS 0.30 s, auto-reclaim PASS 2.43-2.47 s; selections match the named tests only |
| RP9 | index 2 of both tasks, 15:05-15:21Z: `go test -race -count=1 -timeout 30m ./internal/ops/` then `./internal/agent/`; rerun of ops 15:24-15:28Z with full output kept | ops at load 36.3: `FAIL internal/ops 646.141s`; **failing test not attributed**: the probe kept only `tail -3`, so the names were lost (probe defect, not evidence either way). agent at load 27.5: `ok 304.570s`. ops rerun at load 9.6: `ok 256.369s`. See Residual exposure. |
| RP10 | index 3: `go test -count=1 -run '^TestSleepUsageBudget$' -v ./internal/testguard/` | PASS 0.22 s; `maxSleepCallsInTests = 16` (`sleep_usage_test.go:20`) |
| RP11 | index 0 and deferred targets: `make -n lint-nosync test-nosync test-e2e-nosync test-race-nosync` (the public targets add `ci-prepare` sync) | gofmt; `go vet ./...` for linux, windows, darwin; archguard; `go test ./...`; `go test -race -tags e2e -run '^(TestFullSprintSequence\|TestTerminalDependencyRecovery)$' ./internal/integration/ -count=1`; `go test -race -timeout 30m -p 2 ./...` |

**Budget basis (estimate from RP2, RP8 and the D41 plan's measured lint).**
Task 1: index 1 ≈ 20 × 0.8 s (two subtests, the late one 0.4 s longer) + build
≈ 90 s, ×3 for load ≈ 270 s; index 2 ≈ 485 s (batch 19), allowed 1800 s by
`-timeout 30m`; index 3 ≈ 70 s with build; lint ≤ 900 s cold. Worst case
900 + 600 + 1800 + 70 = 3370 s < 3600 s. Task 2: index 1 ≈ 20 × (0.3 + 1.3 +
2.5 + 3.5) s + build ≈ 220 s, ×3 for load ≈ 660 s, bounded by `-timeout 30m`;
index 2 ≈ 390-605 s (batch logs), allowed 1800 s; worst case
900 + 660 + 1800 + 70 = 3430 s < 3600 s. Deferred commands do not count toward
admission.

## Spec Compliance Matrix

| # | Requirement (priority) | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Must: source-grounded RCA and controlled regression for the idle-checkpoint signature | task DONE WHEN | RCA C1; Task 1 | Covered |
| 2 | Must: RCA and regression for `TestAwaitVerdict_Terminal` | task DONE WHEN | RCA C2; Task 2 | Covered |
| 3 | Must: RCA and regression for the rejected auto-reclaim degraded-health test | task DONE WHEN | RCA C2; Task 2 | Covered |
| 4 | Must: distinct causes in bounded outputs | task DONE WHEN | Task 1 (C1), Task 2 (C2) | Covered |
| 5 | Must: regression fails for the observed reason before correction | task DONE WHEN; d41-rules | Tasks 1-2 red first; CI-red, AT-red | Covered |
| 6 | Must: original-to-retained traceability incl. every terminal-result and degraded-health assertion | task DONE WHEN | Assertion Traceability | Covered |
| 7 | Must: obligations mapped into acceptance contracts | task DONE WHEN | Acceptance Contracts; Manifest Map | Covered |
| 8 | Must: every assertion and semantic expected value kept | operator constraint; goal-tests | Traceability; CI-preserve, AT-preserve | Covered |
| 9 | Must: sleep budget 16, guard unedited | operator constraint; d46 | CI-sleep, AT-sleep (index 3) | Covered |
| 10 | Must: focused race count=20 under representative load, sanitized env | task DONE WHEN; d41; guard-tier1 G1.4 | index 1; CI-load, AT-load; Common Rules | Covered |
| 11 | Must: whole affected-package race | task DONE WHEN | index 2 (agent; ops) | Covered |
| 12 | Must: make lint/test/test-e2e/test-race under deferred policy | task DONE WHEN; goal-proof E1 | indexes 0, 4-6 (4-6 deferred) | Covered |
| 13 | Must: production unchanged; defect evidence returned, not implemented | task DONE WHEN | RCA b. production assessment; scopes; Common Rules | Covered |
| 14 | Must: no skip, known-red, budget-only widening, oracle edit, suppression | task SCOPE OUT; d41 | Common Rules; Traceability "Changed, not asserted" | Covered |
| 15 | Must: no correction of code-2, no overlap with code-0 or code-1 scope | task DONE WHEN | Intent and Authority; Task 2 leaves code-1's test unchanged | Covered |
| 16 | Must: writer/proof ownership and legal placement documented before emission | task DONE WHEN; a4-proof | Ownership table; Placement (held for ruling) | Covered |
| 17 | Must: proof only in task worktrees under env -i | task SCOPE; goal-proof E4 | validation commands; Common Rules | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: test-only change with no behavior change; `make test-e2e` stays a deferred regression | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: no behavior change; this plan is the RCA record | N/A |
