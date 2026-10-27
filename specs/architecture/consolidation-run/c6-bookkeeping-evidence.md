# C6 Runtime Task Bookkeeping: Preservation Evidence

Evidence record of `arm-1-ar-21-cp-0-code-1` (plan Task 2). Manifest:
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-21-cp-0-bookkeeping.json`.
Test commands ran under `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp`. The canonical list of the manifest
(`make test`, `make lint`, `make test-e2e`, regen check, archguard) is not pre-run by the coding session beyond
the focused runs below; the submit receipt records it.

## Trees

| Name | Commit | Meaning |
|---|---|---|
| start | `8f10c7d73` | integration head at claim; production tree before this unit |
| gap row | `58b5c6cf7` | oracle-only commit; its production tree equals `start` (`git diff 8f10c7d73 58b5c6cf7` lists only `internal/oracle/a9`) |
| move | `263298679` | the C6 move O7-O15; `git diff 58b5c6cf7 263298679 --stat -- internal/oracle` is empty |

The pre-edit runs used a disposable detached worktree at `58b5c6cf7` after `make sync-embedded` (removed afterwards).

## PB map, pre-edit run and after

| PB item | Tests |
|---|---|
| Recovery history under-lock time, warning-only failure, branch-gone/prior-recovery forced block, reviewer release | `worktree_check_test.go`: `TestEnsureReviewerWorktree_*` (incl. `MissingRecoverable`, `MissingAlreadyRecovered`, `MissingBranchGone`, `..._PostWorktreeCmdFailureDoesNotBlockTask`); `state_snapshot_read_test.go`: `TestReviewerWorktreeSetupReadsStateWhileLockHeld`; `generation_fence_test.go`: `TestSupervisorOwnedMutationGenerationFence` (stale `reviewer worktree blocking`, provider audit, exit-42); oracle `TestOracleA9ReviewerWorktree` |
| Clean-terminal release of a matching agent and completion handoff | `state_snapshot_read_test.go`: `TestCleanTaskCleanupRejectsReplacementPublishedDuringLockWait`; oracle `TestOracleA9AuditAndCleanup` (`i18-cleanup`, completion handoff in the golden) |
| Both anomaly records, reporter, caller policies | `provider_audit_test.go`: `TestHandleProviderAuditDegraded_CanonicalizesProviderAliasInAnomaly`; `supervisor_test.go`: `TestSupervisor_Exit0ProviderAuditDegradedContinuesPostExecution`; `strategy_reviewer_merge_wake_test.go`: `TestReviewerPreWork_PendingMergeStallRecordsAnomaly`; oracle `TestOracleA9AuditAndCleanup` (`i17-degraded`), `TestOracleA9PendingMergeStall` |
| Resumed-checkpoint clear only when the locked sprint matches, even after transition failures | `strategy_test.go`: `TestOrchestratorPreWork_*`; oracle `TestOracleA9OrchestratorPlans` (`i13-cleared`, `i13-kept`, `i13-guard`) |
| Note stamping for HUMAN_NOTE | `strategy_orchestrator_human_note_test.go`: `TestOrchestratorHumanNote_*`; oracle `TestOracleA9OrchestratorTurn` (`i12-success`, `i12-refused`) |
| Note stamping for assessed targets (gap) | `TestOracleA9OrchestratorNotes/i12-assessed` (new); agent-level `TestOrchestratorHumanNote_ConsumedByAssessmentIsNotRedelivered`, `..._AssessmentOfOtherTaskDoesNotConsume` |
| Undecided plans, alerts after commit | `plan_handoff_undecided_test.go`: `TestUndecided*`; `plan_handoff_test.go`: `TestPlanningCompleteUndecidedTurnCreatesNoChildren`, `TestMixedUndecidedTurnStillCheckpointsPassedPlan`; oracle `TestOracleA9OrchestratorPlans` (`i14-*`) |
| Summary claim scope (`Since`, newest-first `TaskIDs`, `Omitted`, limit), atomic clear, unfenced | `checkpoint_summary_gate_test.go`: `TestMaybeEmitCheckpointSummary_*`, `TestWaitWhilePaused_*`, `TestOrchestratorWaitForWork_EmitsForACheckpointCreatedWhileIdle`, `TestRunSupervisor_EmitsPendingCheckpointSummaryBeforeFinalShutdown`; `checkpoint_summary_test.go`: `TestEmitCheckpointSummary_*`, `TestBuildCheckpointSummary*`; oracle `TestOracleA9CheckpointSummary` |

PB selection regex (`go test -count=1 -v -run '<regex>'`):

```text
^(TestEnsureReviewerWorktree.*|TestReviewerWorktreeSetupReadsStateWhileLockHeld|TestCleanTaskCleanupRejectsReplacementPublishedDuringLockWait|TestSupervisorOwnedMutationGenerationFence|TestSupervisorDispatchGenerationFence|TestHandleProviderAuditDegraded_CanonicalizesProviderAliasInAnomaly|TestSupervisor_Exit0ProviderAuditDegradedContinuesPostExecution|TestReviewerPreWork_PendingMergeStallRecordsAnomaly|TestOrchestratorPreWork_.*|TestOrchestratorHumanNote_.*|TestUndecidedPlanningTurnDoesNotReWake|TestUndecidedStaleProviderTurnDoesNotReWake|TestUndecidedPlanKeepsSprintOpen|TestUndecidedPlanReadmittedByNewInput|TestPlanningCompleteUndecidedTurnCreatesNoChildren|TestMixedUndecidedTurnStillCheckpointsPassedPlan|TestMaybeEmitCheckpointSummary_.*|TestWaitWhilePaused_.*|TestOrchestratorWaitForWork_EmitsForACheckpointCreatedWhileIdle|TestRunSupervisor_EmitsPendingCheckpointSummaryBeforeFinalShutdown|TestEmitCheckpointSummary_.*|TestBuildCheckpointSummary.*)$
```

| Claim | Command | Tree | Outcome |
|---|---|---|---|
| Complete PB selection green before any production edit | the regex over `./internal/agent/` in the disposable worktree | `58b5c6cf7` (unchanged production tree) | `ok internal/agent 11.265s`; 69 top-level `--- PASS`, 0 `--- FAIL` |
| Same selection after the move | the regex over `./internal/agent/ ./internal/ops/` (the three scope-builder tests now run in ops) | working tree equal to `263298679` | 69 top-level `--- PASS` (same names, `comm` of the sorted pass lists is empty), 0 `--- FAIL`; `ok` for both packages |
| Oracle rows for O7-O15 before any production edit | `go test -count=1 -run 'TestOracleA9(ReviewerWorktree\|AuditAndCleanup\|PendingMergeStall\|OrchestratorTurn\|OrchestratorPlans\|CheckpointSummary\|OrchestratorNotes)$' ./internal/oracle/a9/` | `58b5c6cf7` | `ok 53.419s` |
| Same rows after the move | same command | `263298679` tree | `ok 52.581s` |
| New ops state-effect tests | `go test -count=1 -v -run 'TestC6(Record\|Force\|Finalize\|Supervisor\|Clear\|Consume\|Claim)\|TestBuildCheckpointSummaryScope' ./internal/ops/` | `263298679` tree | 28 `--- PASS`, 0 `--- FAIL` |
| Archguard | `go test -count=1 ./internal/testguard/archguard/` | `263298679` tree | `ok` |

## Gap record

| Gap at `8f10c7d73` | Row | Recipe | Frozen observation |
|---|---|---|---|
| Assessed-target note consumption on a non-HUMAN_NOTE trigger | `TestOracleA9OrchestratorNotes/i12-assessed` | BLOCKED_TASKS wake (task-1 blocked), three unseen notes (for task-1, for task-2, for all); the claude stub runs `assess-blocked task-1 --note oracle-assessment` then `stop` | one `orchestrator_assessment` entry; exactly two `orchestrator_seen_at` stamps (the task-1 note and the for-all note); the task-2 note unseen |

Goldens come from `go run ./internal/testhelpers/oracle/cmd/regen -mode write-new -pkg ./internal/oracle/a9/...`
at the starting tree, in oracle-only commit `58b5c6cf7` (a first trial run in write mode through the test binary
was deleted before regeneration). The recipe is deterministic: `-count=2` passed. Every other O7-O15 effect already
has an oracle row (table above); no other gap was found.

## Assertion-preservation map

No test was deleted or weakened.

| Old | New |
|---|---|
| `agent/checkpoint_summary_test.go` `TestBuildCheckpointSummaryScope_{MergesSinceThePreviousCheckpoint,FallsBackToTheSprintStart,CapsTheTaskList}` and helper `mergedTask` | `ops/checkpoint_summary_claim_test.go`, same names and assertions, calling `buildCheckpointSummaryScope` |
| `checkpointSummaryScope{` in `emitCheckpointSummary`/prompt tests | `ops.CheckpointSummaryScope{` (call site only) |
| `generation_fence_test.go` `blockReviewerTask(bb, "task-1", stale, ...)` | `ops.ForceBlockReviewerTask(bb, stale, "task-1", ...)`; source markers now name the ops operations the agent files call (plus `strategy_reviewer.go`), and the direct-`.Modify(` bar is unchanged |
| `ProviderAuditDegradedAnomalyType`, `pendingMergeStallAnomalyType` in agent tests | `models.AnomalyTypeProviderAuditDegraded`, `models.AnomalyTypePendingMergeStalled` (same string values) |

## Moved sites

| Old (`internal/agent`) | New (`internal/ops`) |
|---|---|
| `recordReviewerWorktreeRecovery` closure (`worktree_check.go`) | `RecordWorktreeRecovered` (`reviewer_worktree_records.go`) |
| `blockReviewerTask` | `ForceBlockReviewerTask` (`reviewer_worktree_records.go`) |
| `handleCleanTaskCleanup` finalization closure (`claiming.go`) | `FinalizeCleanTerminalTask` (`clean_terminal_finalize.go`) |
| `handleProviderAuditDegraded` anomaly closure | `RecordProviderAuditDegraded` (`supervisor_anomalies.go`) |
| `recordPendingMergeStall` closure | `RecordPendingMergeStall` (`supervisor_anomalies.go`) |
| orchestrator `PreWork` trigger clear | `ClearResumedTransitionCheckpoint` (`orchestrator_turn_records.go`) |
| orchestrator `PostExecution` note consumption | `ConsumeRenderedHumanNotes` (`orchestrator_turn_records.go`) |
| `recordUndecidedPlans` closure | `RecordUndecidedPlans`, `UndecidedPlanRecord` (`orchestrator_turn_records.go`) |
| `maybeEmitCheckpointSummary` claim, `checkpointSummaryScope`, `buildCheckpointSummaryScope`, `checkpointSummaryScopeLimit` | `ClaimCheckpointSummary`, `CheckpointSummaryClaim`, `CheckpointSummaryScope`, `buildCheckpointSummaryScope`, `checkpointSummaryScopeLimit` (`checkpoint_summary_claim.go`) |

The agent keeps filesystem recovery, setup and index refresh, the clean-terminal locked read, authority check and Git
deletion, detection, alerts and the `(true, err)` and warning policies, the `bb == nil` guard of the audit path, the
orchestrator transition handling before the clear, the `clear checkpoint trigger` error wrap, logs and alert writes
after the undecided transaction, the role gate, in-flight slot and background emission of the summary.
`rg 'ModifyWithAgentAuthority|\.Modify\(' internal/agent` over non-test files finds nothing.

## D33 and no-new-behaviour checks

- The five new ops files contain no `exec.`, `gitpkg`, `git.New` or `CheckCurrent` (`rg` exit 1); no inherited
  `CheckCurrent` site is touched, so the count of Git/exec calls inside `Modify` is unchanged. Git deletion and
  filesystem recovery stay before their request, outside any transaction.
- Clock placement is as before: every moved clock read stays under the lock (`time.Now().UTC()`, and local
  `time.Now()` for the clean-terminal handoff event); `RecordUndecidedPlans` takes the caller's pre-transaction `now`.
- One log-timing difference (logs are free): the "Marked operator notes as seen" line is written after the commit
  instead of inside the callback, so it no longer appears when the commit fails.
- Added nothing: no validation, authority check, error prefix, history entry, anomaly field or lock. The
  summary claim stays unfenced (`bb.Modify`); forced block keeps no transition validation.
- Branding: the added lines contain no raw default-brand literal outside Go import paths (`rg` scan of the diff
  and the new files).

## References re-grep

`INVARIANTS.md` line 388 cited `strategy_orchestrator.go` for the resumed-checkpoint comparison inside its write
callback; the callback now lives in `orchestrator_turn_records.go`, which was added to that row (location only).
`INVARIANTS.md` line 253 (`worktree_check.go`, `claiming.go`: post-worktree setup) still holds. Historical ADRs
(0117, 0228), the A9 closure evidence, `architecture-review.md`, the decision record and oracle driver comments that
name the old functions are records and stay unchanged; `protocols/`, `lessons/` and the other architecture documents
name none of the moved sites.

## Negative checks

Each guard was disabled once and its test went red for the intended reason, then restored: `ReleaseAgent` removed
(`TestC6ForceBlockReviewerTaskBlocksOutsideTheTransitionTable`), the `CurrentTask == taskID` match dropped
(`...KeepsAnAgentWorkingElsewhere`), the same-checkpoint comparison forced true
(`...KeepsAReplacedOrHardCheckpoint`, first failing subtest `different checkpoint time`), the assessment filter
forced to consume all and then without the for-all case (`...AssessmentConsumesItsTargetAndAll`, 3 then 1 stamped),
the obligation clear removed (`...ClearsTheObligation`), the `agent_id` detail removed
(`TestC6RecordPendingMergeStallAppendsTheAnomalyFields`, refused by the ADR-0166 write boundary).
