# A3 K7 occurrence inventory (arm-1-ar-12-cp-4-code-2)

Traversal: the 83 A3 production files of `internal/ops` at the submitted revision (the 69 area files at planning HEAD plus the concern files Scopes 2-5 created), `_test.go` excluded, platform files included. Method: (1) the family probes RP3-RP6 of the plan re-run over the whole list (below); (2) an uncommitted AST scan that lists every string, integer, float and duration literal in those files outside import paths, struct tags and test files, 643 occurrences at the submitted revision; each is either a *named declaration* (adopted: the literal is the single definition of a named value) or a retained *dynamic* occurrence with a category and a file:line list. No value, hash input, lock name, persisted spelling or exported signature other than the two X2 types changed.

## Re-run probes

| Probe | Result at the submitted revision (83 files, 15,168 lines) |
|---|---|
| RP3 families | safe-action literals 4 and effects literals 6, all inside the `LifecycleSafeAction`/`LifecycleEffects` declarations, `assessmentOutcomeUnknown`, `postWorktreeDetectionNone`, `runtimeInputNoBoundInstance` and the `"none"` display placeholder of `write_checkpoint.go:94`; role-pair literals 3, all comments; cardinality 0, impact 0 (I6: no A3 occurrence, re-probed, nothing to adopt); runtime-input codes 4 (the `runtimeInputCode` declarations); `OK`/`TRIGGERED` 2 (`circuitBreakerStatus*` declarations); raw `Event:` 0, raw `Type:` 0; `roles.` 17, `models.TaskEvent` 39, `models.Anomaly` 9; `N * time.Unit` 7, all named constant declarations |
| RP4 | every `const` block, `type X string` declaration and literal comparison reviewed through the AST scan; remaining comparisons use constants, typed values or dynamic identities |
| RP5 | `mode_change.go` has no lifecycle-decision call; no function-typed value or interface carries the retyped parameters |
| RP6 | external users of the retyped exported constants are `internal/agent` (`claim_breaker.go`, `claiming.go`, `strategy_reviewer.go`, tests) and A5 `claim_reviewer_task.go`/`validation_preflight.go`; `claim_breaker.go`, `claiming.go`, `validation_preflight.go` and the tests were conformed by type substitution or `string(...)` conversion, while `strategy_reviewer.go` and `claim_reviewer_task.go` assign or pass the typed constants unchanged and compile as written; no logic change |

## Adopted (value unchanged)

| ID | Family | Owner and symbol | Files |
|---|---|---|---|
| I1 | claim type | `roles.ClaimDoer` via `string(...)` at the command argument | repair_doer_ownership.go |
| I2 | role type | `roles.RoleTypeOrchestrator` | resolve_orchestrator.go |
| I3 | role names | existing `roles.Orchestrator`, `roles.Coder`, `roles.CodeReviewer` | state_lock_holds.go, recover_task_snapshot.go, resume_handoff.go |
| I4 | history event | `models.TaskEventBlocked` | claim_task_admission.go |
| I5 | anomaly type | `models.AnomalyTypeAgentDegraded` | agent_health.go |
| I6 | transition vocabularies | none present | - |
| I7, I8, I9 | lifecycle safe action, effects, outcomes | delivered by Task 2 (`LifecycleSafeAction`, `LifecycleEffects`); `models.Lifecycle*` in `lifecycle_metrics.go`, `agent_authority.go` | lifecycle_*.go, agent_authority.go |
| I10 | runtime-input codes | `runtimeInputCode` (4), `runtimeInputNoBoundInstance`, `runtimeInputRunIDBytes`; record outcomes `runtimeInputRecorded/Bound/Unchanged` and conflict kinds `runtimeInputConflict*` | runtime_input_ledger.go, runtime_input_record.go |
| I11 | state-lock-hold reasons | `stateLockHoldReason` (19), `stateLockHoldUnblockPrefix`, `stateLockHoldRequestPrefix`; `string(reason)` at the persisted field, hash material and `mark_blocked`/`assess_blocked`/`unblock` carriers | state_lock_holds.go, mark_blocked_transaction.go, assess_blocked_transaction.go, unblock_transaction.go |
| I12 | assessment outcomes, awaited states | `assessmentOutcome` (6), `awaitedState` (3), `humanNoteForAll`; `hex.EncodedLen(sha256.Size)` | assess_blocked_fingerprint.go, assess_blocked_awaited.go |
| I13 | config-set outcomes | `configSetOutcome` (7), `configConflictExistingValue`, `postWorktreeDetection*` | config.go, admission_limit_config.go, agent_env_deny.go, set_auto_resume.go |
| I15 | lock and namespace labels | `claimTask*Operation`, `admission*LockOperation`, `workAdmissionLockName`, `workAdmissionBarrierSuffix`, `projectLifecycleLockName`, `agentLifecycleLockPrefix`, `recoverTaskLockOperation`, `projectCleanupLockOperation`, `recordLifecycleOutcomeLockOperation` | claim_task.go, admission_slot.go, work_admission.go, project_lifecycle_lock.go, agent_lifecycle_lock.go, recover_task.go, project_cleanup.go, lifecycle_metrics.go |
| I16 | internal operation kinds | `preservedClaimRecovery` (4), `interruptedOperationRebase` | claim_task_strategy.go |
| I17 | timing defaults | `defaultAdmissionLimitRefreshInterval`, `admitWaitDelay`, `defaultWorkAdmissionBarrierTimeout`, `agentExitPollInterval`, `processGroupPollInterval` | admission_slot.go, admit.go, work_admission.go, delete_agent.go, exec_unix.go |
| I18 | thresholds and bounds | `maxBlockedQuestions`, `hypothesisExhaustionFailures`, `models.MaxTaskAttempt`, `preservedClaimEvidenceLimit`, `rebaseSkippedNoteLimit`, `unblockRebaseExcerptLimit` | assess_blocked.go, hypothesis_exhaustion.go, assess_hypothesis_exhausted.go, iteration_limits.go, transition_attempt.go, claim_task_strategy.go, unblock_task.go |
| I19 | sizes and modes | `hex.EncodedLen(sha256.Size)`, `lifecycleSprintIDMaxBytes`, `claimTaskLockKeyBytes`, `agentLifecycleKeyBytes`, `admissionTicketNonceBytes`, `admissionDirMode`, `lifecycleMetricsDirMode` | lifecycle_receipt.go, lifecycle_metrics.go, claim_task.go, agent_lifecycle_lock.go, admission_slot.go |
| I20 | breaker status | `circuitBreakerStatusOK`, `circuitBreakerStatusTriggered` (no models constant exists; unexported ops constants) | mode_change.go |
| I21 | git tree modes | `gitModeRegularFile`, `gitModeExecutableFile` (A2 delivered none) | runtime_input_admission.go |
| I22 | broadcast target, operation names | `humanNoteForAll`; `recoverAgentOperation`, `recoverTaskOperation`, `handoffOperation`, `markBlockedOperation`, `releaseClaimOperation`, `transitionAttemptOperation`, `pauseOperation`, `resumeOperation`, `updateSprintMetricsOperation`, `claimTaskOperation` | recover_agent.go, recover_task.go, handoff.go, mark_blocked.go, release_claim.go, transition_attempt.go, mode_change.go, lifecycle_metrics.go, claim_task.go |
| I23 | already typed | retained, no parallel type | - |
| I24 | review-claim classes | `ReviewClaimClass` (10) on `ReviewClaimCandidateFailure.Class`, `ReviewClaimFailure.Class`, `ReviewerClaimCircuitOpenInput.FailureClass`; X2: `internal/agent` `claim_breaker.go`, `claiming.go` and their tests | claim_failure_anomaly.go |
| I25 | degradation reasons | `AgentDegradedReason` (4) on `InfraClaimErrorClassification.Reason`; `string(...)` at the model `Reason` field | agent_health.go, claim_task.go |
| I26 | key and code names | `reviewClaimCodeGitOperation = operationalCodeGitOperation`, `reviewClaimCodeWorktreeContext`, `integrationStateChangedCode`, `humanNoteSourceOperatorCLI`, `alertCategoryBlocked`, `alertCategoryUnresolvedBlocked`, `notFoundEntityTask/Agent`, `actorOperator/Human/System`, `validationContextChanged` | operational_error.go, claim_failure_anomaly.go, mode_change.go, release_blocked_chain.go, assess_blocked.go, mark_blocked.go, helpers.go, claim_task_assignment.go |

## Classified occurrences

All 643 scanned occurrences, by category. Format: `file: line, line, ...`. The AST scan skips literals that are direct arguments of message-formatting calls; those are message text and dynamic by definition.

### named declaration (adopted) - 226

The literal is the single definition of a named value: constants and typed constants of the Adopted table, plus pre-existing named values (`SubmitSlotWaitFloor`, `projectLifecycleLockTimeout`, `*ConfigKey`, `*ExtraKey`).

- admission_limit_config.go: 17
- admission_slot.go: 28, 38, 39, 40, 41, 45, 48, 52, 57, 58, 67
- admit.go: 23, 58
- agent_authority.go: 12
- agent_env_deny.go: 19
- agent_health.go: 21, 22, 23, 24
- agent_lifecycle_lock.go: 61, 64
- agent_process_status.go: 11, 33, 34, 35
- assess_blocked.go: 21, 223, 227, 231
- assess_blocked_awaited.go: 19, 78, 79, 80
- assess_blocked_fingerprint.go: 16, 18, 39, 47, 48, 49, 50, 51, 52
- assess_hypothesis_exhausted.go: 51
- budget_exception.go: 14, 16
- claim_agent.go: 13, 14, 15, 16, 17, 18, 19
- claim_failure_anomaly.go: 26, 27, 28, 29, 30, 35, 36, 37, 38, 39, 46, 249
- claim_task.go: 22, 23, 24, 25, 27, 173, 174
- claim_task_assignment.go: 15
- claim_task_strategy.go: 18, 26, 27, 28, 29, 34, 36
- config.go: 19, 42, 43, 44, 45, 46, 47, 48, 53, 57, 58, 59, 127, 128, 129, 133, 234
- delete_agent.go: 168
- exec_unix.go: 35
- handoff.go: 16
- helpers.go: 14, 15, 20, 21, 22
- hypothesis_exhaustion.go: 14, 16, 19, 20
- lifecycle_metrics.go: 21, 25, 27, 29, 32, 34, 36, 40, 41, 42, 43, 44, 45
- lifecycle_receipt.go: 32
- lifecycle_result.go: 14, 21, 22, 24, 34, 35, 36
- mark_blocked.go: 20, 23
- mode_change.go: 22, 23, 27, 28, 29, 30, 31, 35, 38, 39, 43, 49, 50
- operational_error.go: 7
- project_cleanup.go: 17
- project_lifecycle_lock.go: 12, 16
- recover_agent.go: 18
- recover_task.go: 15, 16
- release_blocked_chain.go: 21, 23
- release_claim.go: 19
- release_claim_descriptor.go: 33, 34, 52, 53
- runtime_input_admission.go: 16, 17, 23
- runtime_input_ledger.go: 27, 28, 29, 30, 35, 287
- runtime_input_record.go: 17, 18, 19, 21, 22, 23
- scope_exception.go: 21, 23
- set_auto_resume.go: 13
- state_lock_holds.go: 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 50, 54
- terminated_doer_wip.go: 15
- transition_attempt.go: 16, 28
- unblock_task.go: 17, 20
- work_admission.go: 64, 68, 71

### message or error text (dynamic) - 176

Frozen user-facing, operator-facing or error text: it is output, never compared; changing it changes behaviour, so it stays text (I27).

- admission_limit_config.go: 36, 52, 68
- admission_slot.go: 329, 331
- admission_timeout_retry.go: 150
- admit.go: 49, 69
- agent_authority.go: 23, 25
- agent_env_deny.go: 38, 51
- agent_health.go: 61, 64, 67, 70, 153, 262, 263, 265, 267, 270
- agent_process_status.go: 52
- assess_blocked.go: 282, 312, 339, 342, 345
- assess_blocked_awaited.go: 32, 97, 99, 105, 168, 194, 198, 203, 207
- assess_blocked_fingerprint.go: 112
- assess_blocked_transaction.go: 104
- assess_hypothesis_exhausted.go: 113, 116
- check_commit_allowed.go: 34, 41, 46, 55, 61, 73
- claim_failure_anomaly.go: 279
- claim_task.go: 79, 82, 274
- claim_task_admission.go: 174, 254, 345, 348
- claim_task_assignment.go: 45, 61
- claim_task_stranded.go: 36, 62, 66
- claim_task_strategy.go: 371, 376, 377, 556, 563, 602, 617
- claim_task_worktree.go: 160, 161, 162
- config.go: 68, 71, 94, 192, 252, 265, 294
- handoff.go: 68
- hypothesis_exhaustion.go: 41
- iteration_limits.go: 96, 102, 108
- lifecycle_metrics.go: 145, 151, 153, 245
- lifecycle_receipt.go: 168, 193, 196, 232, 242, 249, 303
- lifecycle_result.go: 77, 79
- mark_blocked.go: 157
- mark_blocked_transaction.go: 59, 73
- mode_change.go: 85, 351, 358, 412, 579, 608, 627
- project_cleanup.go: 146
- recover_agent.go: 59, 115
- recover_agent_claims.go: 52, 59
- recover_task.go: 99, 123, 222
- recover_task_snapshot.go: 120
- release_blocked_chain.go: 42, 46, 71, 74, 100, 102
- release_claim.go: 53, 82, 99
- repair_doer_ownership.go: 21, 121
- resume_handoff.go: 33
- resume_owned_task.go: 55, 192
- resume_transitions.go: 44
- runtime_input_admission.go: 78, 91, 93, 114, 162, 204, 207, 243, 270
- runtime_input_ledger.go: 345
- runtime_input_record.go: 57, 124
- state_lock_holds.go: 189, 219
- terminated_doer_wip.go: 72, 75, 82, 85, 91, 94, 101, 108, 116
- transition_attempt.go: 89, 188
- unblock_task.go: 291, 298, 310, 320, 341, 440, 452
- unblock_transaction.go: 53, 56, 59, 203, 210, 213, 269
- write_checkpoint.go: 50, 53, 56, 59, 101

### details, history or payload map key (dynamic) - 178

Field names of anomaly details, history extras, `SafeDetails`, result payloads and handoff payloads: schema keys consumed by other areas and persisted data, not a decision vocabulary (I26/I27).

- admission_limit_config.go: 53
- agent_authority.go: 33, 34, 35, 36
- agent_env_deny.go: 52
- agent_health.go: 124, 125, 126, 127, 128, 129, 130, 131, 132
- assess_blocked.go: 328
- assess_blocked_fingerprint.go: 80, 81, 82, 83, 85, 86, 87, 88, 90, 91, 92, 93, 100, 101, 106, 192, 194
- assess_blocked_transaction.go: 182, 183
- budget_exception.go: 42, 58, 59
- claim_failure_anomaly.go: 162, 290, 291, 292, 301, 302, 303, 304, 305, 306, 307, 308, 309, 330, 331, 332
- claim_task_assignment.go: 222
- claim_task_strategy.go: 416, 417, 439, 442, 445, 502, 504
- config.go: 95, 208, 266
- handoff.go: 228, 230, 233, 234, 235, 236
- lifecycle_metrics.go: 250
- lifecycle_result.go: 99, 100, 105, 106, 107, 119, 122
- operational_error.go: 44, 45
- recover_task_fresh.go: 85, 86, 156, 157
- release_blocked_chain.go: 81, 82, 83, 84, 85
- runtime_input_ledger.go: 304, 307, 321, 322, 323, 324, 325, 326
- runtime_input_record.go: 105, 115, 125
- scope_exception.go: 100, 101, 102, 120, 121, 123
- unblock_task.go: 87, 88, 89, 90, 91, 445, 446, 447, 448, 449, 450, 451, 452, 489
- unblock_transaction.go: 278, 284, 285, 286, 287, 288, 292, 293, 294, 295, 296, 324, 351, 368
- work_admission.go: 48
- write_checkpoint.go: 105, 106, 107, 110, 113, 116, 119, 125, 126, 129, 160, 175, 190, 207, 226, 227, 230, 231

### cross-area command, flag or env name (dynamic) - 15

Command, flag and environment-suffix names (`brand.Command`, `brand.EnvName`, `sh -c`): command identity, not closed into a type (I22/I27).

- handoff.go: 80
- recover_task_snapshot.go: 118
- repair_doer_ownership.go: 67, 68
- runtime_input_admission.go: 255
- runtime_input_ledger.go: 334
- shell_runner_unix.go: 16
- shell_runner_windows.go: 23

### field path or field name carried in a diagnostic (dynamic) - 17

Evidence field paths, snapshot field names and payload paths reported to the caller as diagnostic data (I27).

- admission_slot.go: 113
- admission_timeout_retry.go: 150
- assess_blocked_fingerprint.go: 99
- lifecycle_result.go: 116, 117
- mark_blocked.go: 351, 355
- recover_task_snapshot.go: 64, 67, 70, 73, 76, 79, 82
- runtime_input_admission.go: 281, 303
- runtime_input_ledger.go: 345

### single-use phase, action or hint label (dynamic) - 9

A label with exactly one producer and no comparison anywhere: no repeated identity to name (I27).

- repair_doer_ownership.go: 91
- resume_handoff.go: 91
- unblock_task.go: 213, 215, 309, 319, 340, 446, 472

### git, path and platform text matcher (dynamic) - 16

Git error-text matchers, path-traversal and whitespace guards, status-line parsing and `rev-parse` suffix: platform/protocol text, compared in place and never a vocabulary (I27).

- agent_health.go: 268, 271
- budget_exception.go: 34
- claim_task_strategy.go: 228
- config.go: 67, 293
- project_cleanup.go: 90
- scope_exception.go: 74
- unblock_task.go: 498, 499, 500

### encoding delimiter or format verb (dynamic) - 4

Hash/encoding delimiters (`\x1f`, `\x00`, `\x01`) are part of hashed bytes and must not move; `%v` is a format verb.

- admission_limit_config.go: 91
- claim_failure_anomaly.go: 242, 244
- terminated_doer_wip.go: 68

### display placeholder (dynamic) - 2

Display placeholders (`<nil>`, the checkpoint `none` agent label) printed in messages; not control flow.

- transition_attempt.go: 256
- write_checkpoint.go: 94

## Result

Every occurrence is adopted or dynamic; nothing is deferred. The vocabulary spellings, the thresholds and the two hash inputs are pinned by `internal/ops/k7_vocabulary_test.go` and `internal/ops/k7_digest_test.go`.
