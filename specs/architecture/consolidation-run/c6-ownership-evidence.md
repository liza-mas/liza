# C6 Registration and Exit Ownership: Preservation Evidence

Evidence record of `arm-1-ar-21-cp-0-code-0` (plan Task 1). Manifest:
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-21-cp-0-ownership.json`.
Test commands ran under `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp`. The canonical list of the manifest
(`make test`, `make lint`, `make test-e2e`, regen check) is not run by the coding session; the submit receipt
records it.

## Trees

| Name | Commit | Meaning |
|---|---|---|
| start | `75d4084d4` | integration head at claim; production tree before this unit |
| gap rows | `ae2228b1a` | oracle-only commit; its production tree equals `start` (`git diff 75d4084d4 ae2228b1a` over `internal/ops`, `internal/agent`, `internal/commands`, `cmd` is empty) |
| move | `8361eca0f` | the C6 move; sweep and site names refer to it |

## PB map, pre-edit run and after

PB items of plan Task 1 and the existing tests that pin them (all in `internal/agent` unless noted; call-site-only
edits listed below, no assertion changed):

| PB item | Tests |
|---|---|
| Registration collision and takeover | `TestRegisterAgentCollisionIsTyped`, `TestRegisterOrchestratorTakeoverExpired`, `TestRegisterAgentKeepsUnknownActiveLeaseConservative`, `TestRegisterAgentLeaseLessRegistrationOwnedForLivenessWindow` |
| Capacity per role and orchestrator type | `TestRegisterSecondOrchestratorBlocked`, `TestRegisterOrchestratorSingularityAcrossRoleKeys`, `TestRegisterNonOrchestratorMaxInstancesUnaffected`, `TestRegisterDefaultMaxInstances`, `TestRegisterMaxInstancesUsesLeaseFirstCapacity`, `TestRegisterAgentLeaseLessRegistrationOccupiesRoleCapacity`, `TestRegisterAgentNamespaceSafeSingularity`, `TestRegisterSecondOrchestratorBlockedDespiteMisconfiguredMaxInstances` |
| Generation, IDLE row, PID, model | `TestAgentGeneration`, `TestRegisterAgent`, `TestRegisterAgentStoresPID`, `TestRegisterAgentRecordsLaunchModel` |
| In-transaction sweep (new, written before the move) | `TestRegisterAgentSweepsOrphanLivenessRecordInTransaction` |
| Reviewer stale-claim cleanup after the transaction (new, written before the move) | `TestRegisterReviewerClearsStaleReviewClaimsAfterRegistration` |
| Surplus locked recheck, lock-time clock | `pool_surplus_test.go`: `TestReleaseIfSurplus*` (11 tests incl. `LockedRecheckDeclines`, `IgnoresPeerExpiredWhileWaitingForLock`, `ConcurrentRetirementsStopAtCap`, `RefusesStaleAuthority`), `TestRunSupervisorParkedSurplusRetiresInsteadOfClaiming`, `TestRunSupervisorRetiresAtTaskBoundaryUnderBacklog` |
| Patient unregister releasing doer and reviewer claims | `TestUnregisterAgent`, `TestUnregisterAgent_ReleasesTaskSideClaimWhenCurrentTaskMissing`, `state_read_patience_test.go`: `TestUnregisterAgentReleasesDoerClaimUnderTransientLockHold`, `...ReleasesReviewerClaimUnderTransientLockHold`, `...RejectsReplacementRegisteredDuringLockWait` |
| WAITING/HANDOFF exit differences, continuation, witness | `TestResetAgentAfterExit_*` (12 tests), `supervisor_test.go` claim/exit witness cases, `TestReleasedSupervisorBlockActsOnlyOnTheReleasedContinuation` |
| Orchestration status and lease renewal | `TestOrchestratorStatusTransitions`, `TestSetAgentToPlanningStatusNonExistent`, `TestResetAgentToIdle_NotFound` |
| Three block variants, WIP before transaction, stale no-op, release, history | `supervisor_block_ownership_test.go`: `TestSupervisorBlockReleasesAllTaskOwners`; `supervisor_block_preserves_work_test.go`: `TestWatchdogBlockPreservesWorkForUnblockAndReclaim`, `TestReviewerSupervisorBlockLeavesGitStateUntouched`, `TestSupervisorBlockLeavesALifecycleMovedDuringTheSave`, `TestReleasedSupervisorBlockActsOnlyOnTheReleasedContinuation`; `inflight_submit_deadline_test.go`: `TestBlockExecutingTaskFromSupervisorLeavesAMovedTask` |
| Restart count persistence and conditional block | `a9_concerns_pb_test.go`: `TestA9RestartPB5BlockRecordsReasonQuestionsAndHistory`, `TestA9RestartPB5UnownedTaskIsCountedButNeverBlocked`; `supervisor_test.go`: `TestExit42RestartTracker_Blocking`, `TestExit42RestartTracker_BlocksNonCoderRoles` |

| Claim | Command | Tree | Outcome |
|---|---|---|---|
| PB map and new TestA9-style tests green before any production edit | manifest command 0 (`go test -count=1 -run 'TestC6\|Regist\|Surplus\|Unregister\|ReleaseAgent\|ResetAgent\|Orchestrating\|SupervisorBlock\|BlockTask\|BlockExecuting\|BlockReleased\|Exit42\|Restart\|Halt\|PendingMerge' ./internal/agent/`), run in a disposable detached worktree at `75d4084d4` with the two new agent tests copied in | start | `ok github.com/liza-mas/liza/internal/agent 11.741s` |
| Same set after the move | same command over `./internal/ops/ ./internal/agent/` | move | `ok internal/ops 3.122s`, `ok internal/agent 11.722s` |
| New oracle rows on the unchanged production tree | `go test -count=2 -run 'TestOracleA9HaltRelease\|TestOracleA9BlockReleasedStale\|TestOracleA9BlockDoerStale' ./internal/oracle/a9/` in a detached worktree at `75d4084d4` with the oracle commit's files and goldens overlaid | start plus oracle files | `ok 61.390s` |
| New oracle rows after the move | same command | move | `ok 60.965s` |

## Gap record (plan Common Rules, `d40` excepted)

| Gap at `75d4084d4` | Row | Recipe | Frozen observation |
|---|---|---|---|
| Halt-refused start releases the claim | `TestOracleA9HaltRelease` (`i05-halt-release`) | the scenario's `claude` wrapper runs `pause` on the first `--version` probe after coder-1 holds the claim; the probe runs after the claim and before the launch gate | mid-run `launch.yaml`: mode PAUSED, task back in DRAFT_CODE as a continuation, `claim_released` once, coder-1 IDLE with no task, no provider call |
| Crash-loop block after an exit that released no claim | `TestOracleA9BlockReleasedStale` (`i05-block-released-stale`) | crash threshold 1; the provider releases its own claim (`release-claim --force`) and crashes twice | task never BLOCKED, two `doer_claim_released`, third call stops the run |
| Observed-failure block on a task no longer owned | `TestOracleA9BlockDoerStale` (`i05-block-doer-stale`) | spinning threshold 1; the provider releases its claim and prints a failed envelope twice | no BLOCKED; run log shows "Supervisor block skipped: task is not owned by this agent"; without the release the same scenario blocks (checked once, then reverted) |

Goldens come from `regen -mode write-new` at the starting tree in the oracle-only commit; four generation runs
were needed while the recipes were made deterministic (standard-library log lines are dropped from the compared
console text by the new driver, prompt files are spaced by whole seconds as in the I-07 row). No existing oracle
file or golden is edited.

Not reachable black-box: the executing variant (`blockExecutingTaskFromSupervisor`, called only after a session
ends past its execution timeout around its own in-flight submit) needs a live submit marker and a timeout, and
the in-transaction recheck of every variant fires only on a race between the locked read and the transaction. Their
stale outcome (state unchanged) is pinned by `TestBlockExecutingTaskFromSupervisorLeavesAMovedTask`,
`TestSupervisorBlockLeavesALifecycleMovedDuringTheSave` and `TestC6BlockSupervisorTaskStaleObservationsAreNoOps`
(moved token, changed owner, released task owned again). This is a deviation from "a row for each variant" and is
raised for the reviewer.

## Assertion-preservation map

No test was deleted or weakened. Call-site-only edits follow the moved symbols:

| File | Edit |
|---|---|
| `pool_surplus_test.go` | `releaseIfSurplus(` to `ops.RetireSurplusAgent(`, `poolSurplus(` to `ops.PoolSurplus(` |
| `registration_test.go` | `setAgentToOrchestratingStatus(` to `ops.EnterOrchestration(`, `resetAgentToIdle(` to `ops.ResetAgentToIdle(`; two tests added |
| `supervisor_test.go`, `supervisor_block_preserves_work_test.go` | `releasedTaskClaim` to `ops.ReleasedTaskClaim` |
| `generation_fence_test.go` | `supervisor_restart.go` marker `ModifyWithAgentAuthority` to `ops.PersistExit42Restart`; `setAgentToOrchestratingStatus` to `ops.EnterOrchestration` |
| `cmd/liza/agent_generation_e2e_test.go` | `watchdog-block` entry: `transitionSupervisorBlock` / `ops.ModifyWithAgentAuthority` to `applySupervisorBlock` / `ops.BlockSupervisorTask` |

## Moved sites

| Old (`internal/agent`) | New (`internal/ops`) |
|---|---|
| `registerAgentLocked` closure, `occupiedRolePeers` | `RegisterAgent`, `occupiedRolePeers` (`agent_registration.go`) |
| `releaseIfSurplus`, `poolSurplus`, `ownedPendingMergeFingerprint`, `approvedMergePending` | `RetireSurplusAgent`, `PoolSurplus`, `OwnedPendingMergeFingerprint`, `ApprovedMergePending` (`agent_registration.go`) |
| `unregisterAgent` closure, `unregisterInState`, `releaseTaskClaim` | `UnregisterAgent`, `unregisterInState`, `releaseTaskClaim` (`agent_exit_release.go`; `releaseTaskClaim` stays 99 lines) |
| `resetAgentToIdle`, `releaseAgentAfterExit`, `releaseClaimForHalt`, `setAgentToOrchestratingStatus`, `releasedTaskClaim` | `ResetAgentToIdle`, `ReleaseAgentAfterExit`, `ReleaseClaimForHalt`, `EnterOrchestration`, `ReleasedTaskClaim` (`agent_exit_release.go`) |
| `transitionSupervisorBlock`, `supervisorTaskSlot` | `BlockSupervisorTask`, `SupervisorTaskSlot` (`supervisor_block.go`) |
| `persistExit42Restart`, `blockExit42RestartLoop`, `isReviewingStatus` | `PersistExit42Restart`, `blockExit42RestartLoop`, `taskInReviewingStatus` (`exit42_restart.go`) |

The agent keeps `loadPipelineForRelease` (returns `ops.ClaimReleasePipeline`), the patient handle choice for
unregistration, `applySupervisorBlock` (test hook before the call, stale-observation warning after it), the WIP
save before a block, the in-memory exit-42 tracker, and the post-registration stale-review cleanup.
`rg 'ModifyWithAgentAuthority|\.Modify\(' internal/agent/registration.go supervisor_block.go supervisor_restart.go`
finds nothing; the remaining agent uses belong to O7-O15 (Task 2).

## D33 and no-new-behaviour checks

- The four new ops files contain no `exec.`, `gitpkg`, `git.New` or `CheckCurrent` (`rg` exit 1); no inherited
  `CheckCurrent` site is touched, so the count of Git/exec calls inside `Modify` is unchanged. The disk reads
  (`LoadResolverForModels`, `LoadPipelineTransitions`) inside the exit-42 callback are the old ones, in the old order.
- Clock placement is as before: registration, unregister, release, halt release, orchestration read the clock
  before the transaction; surplus retirement and the supervisor block read it under the lock. One difference of a
  few microseconds: `UnregisterAgent` reads its clock after the caller loaded the pipeline instead of before.
- Pre-existing, not changed: a nil `*pipeline.Resolver` converted to `models.PipelineResolver` panics in the
  release helpers (typed nil), so an unreadable pipeline config plus a held claim panics at unregistration;
  `ReleaseClaimForHalt`'s own continuation marking is redundant with `releaseTaskClaim` for an executing task.
  Both are outside C6's no-validation boundary and are reported, not fixed.
- Branding: the added lines contain no raw default-brand literal outside Go import paths and identifiers
  (`SetupLizaDir`, the `lizaerrors` alias); the oracle driver's `liza`-named strings live in the A9 package, whose
  default-brand assertions are permitted.

## Negative checks

Each guard was disabled once and its test went red for the intended reason, then restored: `holds()` forced true
(`TestC6BlockSupervisorTaskStaleObservationsAreNoOps`), collision refusal off (`...RefusesLiveCollision...`),
capacity refusal off (`...EnforcesRoleCapacity...`), `ReleaseClaimForHalt` fence replaced by `bb.Modify`
(`TestC6ReleaseClaimForHaltReleasesOnlyTheNamedHeldClaim`), exit-42 ownership check off
(`...LeavesUnownedAndInactiveTasksUnblocked`), surplus recheck off (`TestC6PoolSurplusAndRetireSurplusAgent`).
