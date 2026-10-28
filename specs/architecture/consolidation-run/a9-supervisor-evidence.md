# A9 Supervisor Separation: Preservation Evidence

Evidence record of `arm-1-ar-20-cp-0-code-3` (plan Task 4, step 6). Manifest:
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-20-cp-0-supervisor.json`.

## Provenance of the pre-edit PB run

- Pre-refactor production tree: `d1a0cdc85`. It differs from `a333b02fe` (integration base) only by
  the new test file `internal/agent/a9_supervisor_pb_test.go` (PB-17); the first production edit is
  `831ad892e`.
- Historical run (observed in the coding session, no log kept): the canonical focused command of the
  manifest (index 0) at `d1a0cdc85` printed `ok  github.com/liza-mas/liza/internal/agent  125.276s`.
  `TestA9SupervisorPB3ReplayPrecedesRegistration` did not exist then; it was added with the lifetime
  split (`75eb56dde`).
- Reproduction (not historical evidence), after the first review: a disposable detached worktree at
  `d1a0cdc85` (removed afterwards), `make sync-embedded`, with the final `a9_supervisor_pb_test.go`
  copied in, under `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp`:
  - `go test -count=1 -v -run TestA9 ./internal/agent/`: `TestA9BackendPB1..6`,
    `TestA9VerifyOrchestratorWakeTriggers` and `TestA9SupervisorPB3ReplayPrecedesRegistration` all PASS.
  - the manifest's index-0 focused command: `ok  github.com/liza-mas/liza/internal/agent  125.173s`.
- After the refactor: the same index-0 command printed `ok ... 124.911s` at `9769dad8f`, and the
  submit receipt records canonical commands 0-9 passing at that SHA.

## PB map and assertion preservation

| PB | Tests (all unchanged except the two new ones) |
|---|---|
| 1 | `TestRunSupervisorWithholdsAgentEnvDenyBeforeRegistration`, `TestValidateIdentity` |
| 2 | `TestRunSupervisorPassesResolverDerivedRoleTypeToPauseWait` |
| 3 | `TestA9SupervisorPB3ReplayPrecedesRegistration` (new): an unreadable outbox ends the run with the state file byte-identical, so replay precedes registration |
| 4 | `TestRegisterAgent`, `TestUnregisterAgent`, `TestRunSupervisor_HeartbeatMissingAgentStopsSupervisor`, `TestStartSupervisorHeartbeat_StopWaitsForWorker`, `TestReleaseIfSurplusRetiresSubmittedDoerWithoutClaiming` |
| 5-7 | `TestRunSupervisor_HaltedDoerLaunchResumesSameIteration`, `TestRunSupervisorWaitAdmission`, `TestRunSupervisorInitialTaskSpentByClaim`, `TestRunSupervisorRetiresAtTaskBoundaryUnderBacklog`, `TestRunSupervisorParkedSurplusRetiresInsteadOfClaiming` |
| 8-9 | `TestWaitWhilePausedHardModesBlockTransitionCheckpointRoleException`, `TestProviderLaunchGate_RefusesStartWhilePaused`, `TestRunSupervisor_HeartbeatMissingAgentStopsSupervisor` |
| 10-11 | `TestSupervisor_BuildPromptFailure_BlocksTask`, `TestSupervisor_BuildPromptFailure_NonPrecommit_DoesNotBlock`, `TestSupervisorClaimFailureQuarantinesBrokenBoundary` |
| 12 | `TestSupervisor_Exit0ProviderAuditDegradedContinuesPostExecution`, `TestRunSupervisorCrashBackoffSpansTasks`, `TestAbandonedBackgroundTaskRetry_*`, `supervisor_block_preserves_work_test.go` |
| 13-15 | `TestProviderLaunchGenerationLinearization`, `TestProviderLaunchGate_PauseWaitsForAdmittedStart`, `TestProviderLaunchFenceFailureSemantics`, `TestReviewLaunchRequiresCurrentOwnership`, `TestRunSupervisor_HaltedReviewerLaunchReleasesReview`, `state_lock_retry_test.go` |
| 16 | `TestExecuteAgentInteractiveUsageCapture`, `TestExecuteAgentBlocksTaskAfterProgressTimeout`, `TestExecuteAgentLetsLateSubmitPublishPastSupervisorClocks`, `TestExecuteAgentProgressWatchdogStillBlocksDuringHandback`, `TestOracleA9InteractiveLaunch` |
| 17 | `TestA9VerifyOrchestratorWakeTriggers` (new, 16 cases: each trigger's expectation, warning versus refusal) and the existing `TestVerifyOrchestratorStateChanges_*` |

`git diff a333b02fe HEAD --stat -- '*_test.go'` lists only `a9_supervisor_pb_test.go` (+243, new)
and `area_a9_test.go` (4 ratchet keys removed). No existing test file, oracle file, golden or
PROVENANCE record changed, so every earlier assertion remains in place.

Negative proof: with the pre-registration replay skipped, `TestA9SupervisorPB3` did not pass (the
supervisor ran on and the test did not complete); the replay was restored.

## Moved C1 and C6 site names

- C1 (lease 1800 s, terminal `"terminal-1"`): the single site in the touched files moved from
  `internal/agent/supervisor.go:135` (`RunSupervisor`) to `internal/agent/supervisor_lifetime.go`
  `(*supervisorLifetime).register`; arguments unchanged. Heartbeat and reviewer sites are untouched.
- C6 mutation sites: `ModifyWithAgentAuthority`/`.Modify(` count in `internal/agent` production code
  is 18 before and 18 after; the two sites in the touched family (`supervisor_block.go:151`,
  `supervisor_restart.go:75`) did not move. No closure was added or moved into `ops`. The
  mutation-bearing helpers `RunSupervisor` called, now called from the new files, keep their package
  and bodies: `registerAgentWithAuthority`, `unregisterAgent`, `releaseIfSurplus` (lifetime);
  `blockTaskFromSupervisor`, `blockSpinningTask`, `releaseUnstartedTurn` (turn);
  `resetAgentAfterExit`, `releaseAgentAfterExit`, `handleProviderAuditDegraded`,
  `preserveClaimForBackgroundJob`, `exit42RestartTracker.Handle`, `blockReleasedTaskFromSupervisor`,
  `strategy.PostExecution` (completed turn); `blockTaskFromSupervisor`,
  `endSessionAfterInflightSubmit`, `releaseFailedValidation` (session).

## D33 inherited final revalidation

Git or `exec` inside a `Modify` callback in the touched files: 0 before, 0 after. The launch-gate
revalidation `validation.CheckLaunchCurrent(state)` (lifecycle then work-admission lock, snapshot
read, no `Modify`) moved with `newTaskProviderLaunchGate` from `systemctl.go:269` to
`systemctl.go:47`; `CheckCurrent`/`CheckLaunchCurrent` calls in `internal/agent` production code: 1
before, 1 after. No lock order, check or refusal changed.

## Brand and reference audit

- Added lines in `internal/`, `INVARIANTS.md` and the plan acceptance directory contain no raw
  default-brand identifier outside Go import paths (`git diff d1a0cdc85 HEAD -U0` filtered for
  `liza` outside `github.com/liza-mas/liza`: empty). No presentation text was added.
- Chain-document references: `INVARIANTS.md` section 5 now cites `supervisor_lifetime.go` (supervisor
  write retries and cleanup context), `session_execution.go` alongside `systemctl.go` (launch
  prerequisites) and `pause_gate.go` (transition auto-resume). The halt-mode row keeps `systemctl.go`,
  which still holds the launch gate. `supervision-model.md`, `state-machines.md`,
  `blackboard-schema.md`, `specs/protocols` and `lessons` cite none of the moved files
  (`grep` for `supervisor.go`, `systemctl.go`, `executeAgent`, `RunSupervisor`,
  `verifyOrchestrator`, `waitWhilePaused`).

## Vocabulary

Named in `agent`: `exitCodeGracefulAbort` (42), `restartTrackerFailureDelay` (2 s),
`claimFailureDelay` and `surplusRetryDelay` (5 s), typed `loopFlow` (turn outcome). Adopted:
`roles.RoleTypeOrchestrator/Doer/Reviewer` (pause gate and supervisor), `models.DiscoveryUrgencyImmediate`,
`prompts.IntegrationView*`. The C1 `"terminal-1"` and 1800 arguments stay for C1.
