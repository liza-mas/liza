# A5 K7 Inventory

Evidence for `arm-1-ar-14-cp-0` Task 8 (`arm-1-ar-14-cp-0-code-7`): the complete K7 occurrence-to-owner
classification of the A5 ops files, the substitutions applied, the owner gaps, the later-area adoption
sites and the reconciliation of the plan's K7 families F1–F12. Source of truth for the rules is the plan
section "K7 Families" and the A3/A4 inventory precedent (`a3-k7`, `a4-k7-inventory.md`, rules R1–R5).

## Coding HEAD

- Integration HEAD at claim (A1, A3, A4 delivered; A5 Tasks 2–7 merged, the last being `eacedcafb`): `47e12d91220adde2dd28348f24d6c9dfb4900c40`.
- The tables describe the tree after the substitutions of this task, committed on top of that HEAD; line numbers are that tree's. Table 2 is derived from `git diff 47e12d912 -- internal/ops/`.
- No oracle file, fixture or golden is added or edited by this task: every substitution keeps its value, so no oracle-first checkpoint is needed.

## Method

1. File list: the 32 `A5` files of the area partition plus the 16 private concern files Tasks 2–7 created (names and creating task in table 1). `init_project.go` (A7) and the A3, A4 and A6 files are out of the list.
2. Every string and numeric literal of those 48 production files was extracted with a `go/parser` scan (import paths and struct tags excluded) with its syntactic context (comparison, `case`, composite field, map key or index, call argument, declaration…): **1342 literals**, of which 144 sit in `const` declarations (already named) and 1198 elsewhere.
3. Each literal's value was matched against every string and integer constant of `ops` and the constants of the A1 owner packages (`models`, `pipeline`, `roles`, `taskkind`, `paths`, `alerts`, `errors`, `payloadschema`, `referencecontract`, `statehygiene`, `statevalidate`, `analysis`, `git`, `gitenv`, `db`, `log`). A same-value, same-meaning owner constant is adopted (R1); a false friend (same spelling, other meaning) is recorded in table 4.
4. A literal that is control-flow vocabulary or a repeated magic value without an owner is named (R4) or, when the owner is a closed package outside `ops` that names nothing, named in `ops` without editing the owner (R3, table 5). A finite set deciding control flow is typed; the A5 sets were typed by Tasks 2–7 (table 3c).
5. Every remaining literal is classified (R5/F12) with a reason in table 3; the classifier fails on a literal it cannot place, so no literal is left unclassified or deferred.
6. Cross-checks: a quoted-literal probe for the lifecycle safe actions and effects, the review impact levels, the claim and role types and the submitted verdicts finds only constant declarations in the 48 files; every `models.TaskEvent*` and `models.AnomalyType*` use in them is the constant, and the one raw `"submit_verdict_failed"` is an activity-log action label (table 4).

Outcome vocabulary: **adopted** (existing owner constant used), **named** (new constant, value unchanged), **typed** (defined type, one constant per value), **classified** (retained literal data or dynamic identity, with reason).

## Summary

| Outcome | Count |
|---|---|
| Substitution sites in this task (table 2: adopted, named or typed use sites) | 58 |
| Constant declarations added by this task (table 2) | 22 |
| Pre-existing named constants in A5 files (table 3b) | 120 |
| Retained literals classified R5/F12 (table 3, grouped by file and reason) | 1198 |
| Unclassified / deferred | 0 |
| New defined types | 0 (every finite control-flow set was typed by Tasks 2–7; table 3c) |
| BLOCKED findings against A1 or A3 closure (prospective symbol not delivered) | 0 |

## 1. A5 files

| File | Origin | Lines | Literals scanned | Substitution sites | Declarations added | Named constants in file |
|---|---|---|---|---|---|---|
| `submit_review.go` | area partition | 168 | 13 | 0 | 0 | 3 |
| `submission_admission.go` | area partition | 156 | 6 | 0 | 0 | 0 |
| `submission_lifecycle.go` | area partition | 275 | 25 | 2 | 3 | 6 |
| `submit_progress.go` | area partition | 93 | 5 | 1 | 1 | 1 |
| `submit_hold.go` | area partition | 41 | 6 | 0 | 0 | 1 |
| `inflight_submit.go` | area partition | 159 | 11 | 2 | 1 | 4 |
| `submit_verdict.go` | area partition | 495 | 48 | 2 | 0 | 1 |
| `pending_verdict.go` | area partition | 322 | 43 | 6 | 5 | 10 |
| `pending_verdict_windows.go` | area partition | 56 | 3 | 0 | 0 | 4 |
| `pending_verdict_unix.go` | area partition | 14 | 0 | 0 | 0 | 0 |
| `quarantined_verdict.go` | area partition | 380 | 46 | 11 | 5 | 5 |
| `await_verdict.go` | area partition | 450 | 16 | 2 | 0 | 0 |
| `await_resubmission.go` | area partition | 502 | 31 | 1 | 0 | 8 |
| `await_policy.go` | area partition | 43 | 10 | 0 | 0 | 1 |
| `update_review_commit.go` | area partition | 318 | 26 | 1 | 0 | 5 |
| `review_boundary.go` | area partition | 213 | 36 | 0 | 0 | 2 |
| `reviewed_range.go` | area partition | 306 | 42 | 0 | 0 | 0 |
| `orphaned_review_base.go` | area partition | 726 | 82 | 4 | 1 | 7 |
| `clear_stale_review_claims.go` | area partition | 250 | 30 | 0 | 0 | 0 |
| `repair_review_ownership.go` | area partition | 127 | 25 | 0 | 0 | 0 |
| `claim_reviewer_task.go` | area partition | 371 | 22 | 0 | 0 | 1 |
| `rejection_rca.go` | area partition | 363 | 27 | 1 | 0 | 0 |
| `reaffirm_proof.go` | area partition | 207 | 35 | 2 | 1 | 1 |
| `acceptance_evidence.go` | area partition | 477 | 91 | 0 | 0 | 6 |
| `acceptance_execution.go` | area partition | 267 | 41 | 2 | 2 | 9 |
| `acceptance_creation.go` | area partition | 153 | 18 | 0 | 0 | 3 |
| `acceptance_claim_block.go` | area partition | 272 | 34 | 0 | 0 | 9 |
| `acceptance_ownership_fence.go` | area partition | 63 | 2 | 0 | 0 | 1 |
| `archive_acceptance_receipts.go` | area partition | 139 | 7 | 1 | 1 | 3 |
| `repair_acceptance_commits.go` | area partition | 399 | 44 | 0 | 0 | 3 |
| `validation_preflight.go` | area partition | 513 | 42 | 3 | 0 | 0 |
| `test_detection.go` | area partition | 163 | 62 | 0 | 0 | 0 |
| `acceptance_allocation_review.go` | concern file (Task 2) | 341 | 26 | 0 | 0 | 0 |
| `acceptance_declaration.go` | concern file (Task 2) | 202 | 23 | 1 | 0 | 0 |
| `acceptance_reference_reads.go` | concern file (Task 2) | 191 | 46 | 0 | 0 | 0 |
| `validation_readiness.go` | concern file (Task 2) | 144 | 29 | 0 | 1 | 22 |
| `submission_finalize.go` | concern file (Task 3) | 230 | 13 | 1 | 0 | 0 |
| `submission_git_work.go` | concern file (Task 3) | 438 | 84 | 2 | 0 | 1 |
| `submission_preconditions.go` | concern file (Task 3) | 161 | 11 | 0 | 0 | 0 |
| `verdict_failure_anomaly.go` | concern file (Task 4) | 167 | 25 | 2 | 0 | 3 |
| `verdict_integration_evidence.go` | concern file (Task 4) | 169 | 11 | 0 | 0 | 0 |
| `verdict_transition.go` | concern file (Task 4) | 518 | 36 | 1 | 0 | 4 |
| `await_verdict_finish.go` | concern file (Task 5) | 227 | 16 | 2 | 1 | 4 |
| `await_verdict_outcome.go` | concern file (Task 5) | 405 | 50 | 2 | 0 | 11 |
| `review_reclaim.go` | concern file (Task 6) | 217 | 7 | 2 | 0 | 1 |
| `review_reservation.go` | concern file (Task 6) | 298 | 4 | 3 | 0 | 1 |
| `reviewer_claim_candidate.go` | concern file (Task 7) | 122 | 4 | 0 | 0 | 0 |
| `reviewer_claim_selection.go` | concern file (Task 7) | 472 | 28 | 1 | 0 | 1 |

## 2. Adopted and named (substitutions of this task)

One row per changed line. A row marked "declaration of named" is the constant's declaration; every other row is a use site that replaced the literal shown. F is the plan's K7 family.



## 3. Classified literals (R5 / F12)

Each row lists every line of one file carrying literals of one class and reason. Classes: **J18** detail, history, payload or hash-material keys (field names; the persisted ones are written here and no A5 decision reads them back by spelling — the keys A5 does read back are named in table 2 or pre-exist in table 3b); **J20** message, format, report or label text, rendered command names, diagnostic field paths and activity-log labels; **J21** emptiness, arity, sign and unit identities and string-operation delimiters; **J22** the data table of test-file naming conventions of other ecosystems (G1.1: it is data about the user's project, not Liza vocabulary, and a configuration field is out of scope). Role names, role pairs, tool and provider names, IDs and user text never appear as literals (F12 below).



### 3b. Pre-existing named constants in A5 files

Named by earlier scopes, by the A5 concern tasks or by A3 inside the A5 files; retained, adopted nowhere twice, no parallel definition. Constants this task declared are in table 2.



### 3c. A5-owned typed sets

| Type | File | Values | Wire pin (white-box test asserting each constant equals its wire value) |
|---|---|---|---|
| `AwaitVerdictOutcome` | `await_verdict_outcome.go` | 9: APPROVED, REJECTED, NEW_ATTEMPT, ALREADY_TRANSITIONED, TERMINAL, TIMEOUT, ABORTED, POLL, PAUSED | `TestAwaitVerdictVocabularyKeepsItsValues`, `TestAwaitVerdictResultKeepsStringFields` |
| `AwaitSafeAction` | `await_verdict_outcome.go` | 2: revise, stop | same tests |
| `AwaitResubmissionOutcome` | `await_resubmission.go` | 6: RESUBMITTED, TERMINAL, TIMEOUT, ABORTED, POLL, PAUSED | `TestAwaitResubmissionVocabularyKeepsItsValues`, `TestAwaitResubmissionResultKeepsStringFields` |
| `submittedVerdict` | `verdict_transition.go` | 2: APPROVED, REJECTED | `TestVerdictNamedValuesKeepTheirValues` |
| `validationCode` | `validation_readiness.go` | 15 | `TestValidationReadinessVocabularyKeepsWireSpelling` |
| `validationResult`, `validationMethod` | `validation_readiness.go` | 2 and 1 | `TestValidationReadinessVocabularyKeepsWireSpelling` |
| `OrphanedBaseHealOutcome` | `orphaned_review_base.go` | 3: not_applicable, healed, integration_failed | `TestReviewOwnershipNamedValuesKeepTheirSpelling` |
| `AcceptanceFault` | `acceptance_evidence.go` | 5: content, allocation, runtime_input, admission_slot, batch_timeout | `TestA5AcceptanceFaultKeepsWireSpelling` (added by this task; no test pinned the spelling before) |

Adopted, not redefined: `ReviewClaimClass` (A3 I24, `claim_failure_anomaly.go`), `LifecycleSafeAction` and `LifecycleEffects` (A3), the A1 types `models.TaskEventName`, `models.AnomalyType`, `models.AllocationRoute`, `pipeline.ReviewImpact`, `roles.RoleType`. No A5 type has a constant of the same set as an A1 or A3 type. `AwaitSafeAction` (`revise`, `stop`) is the await protocol's own pair and is not the A3 lifecycle set (`continue`, `stop`, `requery`, `retry`, `correct_input`): see table 4.

The values this task named are pinned by `TestA5NamedValuesKeepTheirSpelling`, `TestA5ReconcileDispositionsAreOwnerAccepted` and `TestA5ComposedTextKeepsItsBytes` in `internal/ops/k7_a5_vocabulary_test.go`.

## 4. False friends: same spelling, different meaning

| Occurrence | Looks like | Why it is not that vocabulary |
|---|---|---|
| `Action: "submit_verdict_failed"` (`verdict_failure_anomaly.go`) | `models.AnomalyTypeSubmitVerdictFailed` | activity-log action label; log lines are a free surface (goal-frozen), kept apart from anomaly types |
| `Phase: "rebase"` and the other phase labels of `submission_git_work.go` | `interruptedOperationRebase` (`claim_task_strategy.go`) | diagnostic phase of a classified operational error, not the interrupted-operation kind a claim branches on |
| `currentAgent := "none"` (`submission_preconditions.go`) | `LifecycleEffectsNone`, `models.InheritModeNone` | message text for an unassigned task |
| `brand.Command("config", "set")` (`await_policy.go`) | `configSetSet` | rendered operator command arguments, not a config-set outcome |
| `brand.Command("unblock-task" …)`, `("replace-task" …)`, `("update-review-commit" …)`, `("mark-blocked" …)`, `("resume")`, `("write-checkpoint" …)`, `("repair-agent-pool" …)`, `("get" …)` | `integrationOperationUnblockTask`, `replaceTaskOperation`, `updateReviewCommitOperation`, `markBlockedOperation`, `resumeOperation`, `configSetSet` | rendered operator command names in guidance text; command identities are not closed into a type (A3 I22, A4 table 3) |
| `AwaitSafeAction` `stop` | `LifecycleSafeActionStop` | the await pair `revise`/`stop` is a different set read by the await callers; the lifecycle set stays A3's |
| `verdictRequestPayload` keys `verdict`, `reason`, `impact`, `review_commit` | `verdictImpactExtraKey` | payload-digest material keys; their spelling is the stored request digest, apart from the history `Extra` key |
| `"impact"` detail key in `verdict_failure_anomaly.go` | `verdictImpactExtraKey` | anomaly detail key, apart from the history `Extra` key |
| `"integration"` and `"replaced"` keys in `repair_acceptance_commits.go` | `models.TaskTypeIntegration` | history detail keys, named there as `remap*ExtraKey` |
| `"reconcile-verdict"` in `internal/embedded/pipeline.yaml` | `reconcileVerdictOperation` | the pipeline file lists configured capabilities; the constant is the spelling ops compares them with |
| `> 3` / `<= 3` ellipsis test in `submission_git_work.go` | `AcceptanceAllocationRefusalThreshold`, `maxBlockedQuestions` | `len("...")`: unrelated count of the same value |
| `1024` in `acceptance_reference_reads.go` | `runtimeInputHumanAskLimit`, `verdictFailureAnomalyErrorLimit` | KiB unit conversion in a size message |
| `delay*2` in `pending_verdict.go` | `deferredValidationSetupAttempts` | the retry backoff doubles; the constant is an unrelated attempt count |

## 5. Owner gaps (R3) and owner findings

| Value | Owner | Finding | A5 treatment |
|---|---|---|---|
| reconciliation dispositions `accepted`, `refuted`, `superseded`, `escalated` | `models.IsVerdictDisposition` | the owner switches on four literals and names none | `reconcileDispositionRefuted`, `reconcileDispositionSuperseded` in `quarantined_verdict.go` for the two that release a hold; no `models` edit (X1); `TestA5ReconcileDispositionsAreOwnerAccepted` ties both to the owner's validation |
| environment suffixes `AGENT_ID`, `AGENT_GENERATION` | `brand.EnvName` / `brand.LookupEnv` take a free suffix and name no identity variable | owner gap | `agentIDEnvSuffix`, `agentGenerationEnvSuffix` in `submission_lifecycle.go`; other-area sites in table 7 |
| `refs/heads/` branch namespace | `git` names none | owner gap, repeated in A6 and A2 | classified in `orphaned_review_base.go` (table 3); the A6 and A2 sites are listed in table 7 so their owners name it once |
| `reaffirm-proof`, `reconcile-verdict`, `await-verdict` operation names | `models.IsLifecycleOperation` and the pipeline capability list spell them; no constant | owner gap | named in the A5 concern that raises them (table 2) |
| actor `system` in an activity-log entry | `actorSystem` (A3 `helpers.go`) | delivered | adopted (table 2) |
| history `Extra` keys written by A5 and read by A4 (`blocked_class`, `gated_at`, `first_rejection_at`, `causes`, `recovery_path`) | A4 `rejectionRCA*Key` | delivered (F11) | already adopted by `rejection_rca.go` and `verdict_transition.go`; probe: no raw spelling of those five remains in the A5 files |

No V-set or A3 symbol the plan promised is missing: `models.TaskEventName`, `models.AnomalyType`, `pipeline.ReviewImpact`, `roles.ClaimType`, `roles.RoleType`, `LifecycleSafeAction`, `LifecycleEffects` and `ReviewClaimClass` are delivered and used; no BLOCKED finding against A1 or A3 closure.

## 6. Conformance sites edited outside A5 files

None. No exported A5 name, signature, value or type changed, so no X2 caller in `internal/commands`, `cmd/liza`, `internal/agent`, `internal/tui` or another area's ops file needed a conversion, and no INVARIANTS, architecture-spec, protocol or lesson reference is invalidated (no symbol was renamed or moved).

## 7. Later-area adoption sites (X7: adopted when those areas are refactored)

| Area | Site | Literal | A5 name |
|---|---|---|---|
| A3 | `internal/ops/handoff.go:80` | `AGENT_ID` | `agentIDEnvSuffix` |
| A6 | `internal/ops/deferred_validation.go:509` | `AGENT_ID`, `AGENT_GENERATION` | `agentIDEnvSuffix`, `agentGenerationEnvSuffix` |
| A1 | `internal/identity/resolver.go:32,45` | `AGENT_ID` | `agentIDEnvSuffix` (or an owner constant in `brand`) |
| A7 | `internal/commands/stop_guard.go:40`, `start.go:20,21` | `AGENT_ID` | `agentIDEnvSuffix` |
| A7 | `cmd/liza/cmd_repair_arch_ref.go:35`, `cmd_tool_result_run.go:32`, `cmd_provision.go:46`, `cmd_amend_plan.go:55` | `AGENT_ID` | `agentIDEnvSuffix` |
| A7 | `cmd/liza/main.go:30` | `AGENT_GENERATION` | already declares its own `agentGenerationEnvSuffix` (package `main`); one owner constant in `brand` would serve all |
| A9 | `internal/agent/cli_agent.go:642-645` | `AGENT_ID`, `AGENT_GENERATION` | `agentIDEnvSuffix`, `agentGenerationEnvSuffix` |
| A6 | `internal/ops/wt_merge.go:481,977`, `internal/ops/integration_head.go:16` | `refs/heads/` | one Git branch-ref constant |
| A2 | `internal/git/git.go:77`, `internal/git/worktree.go:198` | `refs/heads/` | the owner of that constant |
| A1 | `internal/models/quarantined_verdict.go:45` | `accepted`, `refuted`, `superseded`, `escalated` | disposition constants (owner gap) |
| A7 | `cmd/liza/cmd_reconcile_verdict.go:85` | `accepted`, `refuted`, `superseded`, `escalated` | disposition constants (owner gap) |
| A7 | `cmd/liza/cmd_review.go:654`, `internal/commands/submit_verdict.go:42` | `APPROVED`, `REJECTED` | `submittedVerdict` is unexported; the exported `AwaitVerdictOutcome` constants `VerdictApproved`/`VerdictRejected` are the same wire values for a completion list |
| A3 | `internal/ops/lifecycle_metrics.go:41` | `claim-reviewer-task` | `claimReviewerTaskOperation` (diagnostic-counter key list, A3 I-row classification; listed, not edited) |
| A1 | `internal/models/lifecycle.go:199`, `internal/statevalidate/validate_lifecycle.go:141` | `claim-reviewer-task` | `claimReviewerTaskOperation` (owner list and validator) |
| A9 | `internal/agent/acceptance_refusal.go:85` | `fault_class` | log attribute key (log lines are free); `faultClassExtraKey` names the persisted detail key and is read by no A9 code |

## 8. Plan K7 families F1–F12

| Family | Evidence at this HEAD | Result |
|---|---|---|
| F1 history events, anomaly types, rejection causes, recovery/restore modes, impact, diversity | the A5 files use the delivered `models.TaskEvent*`, `models.AnomalyType*`, `models.RejectionCause`, `models.RecoveryPath` and `models.RestoreMode` constants and `pipeline.ReviewImpact*` (probe in Method 6); the only raw anomaly spelling is the log action label of table 4 | delivered, no residual |
| F2 lifecycle safe actions, effects, outcomes | no quoted safe-action or effects literal outside declarations in the 48 files (probe in Method 6); outcomes come from `models.Lifecycle*`; the operation identities passed to the lifecycle constructors are named (table 2) | delivered, operation identities completed here |
| F3 await-verdict outcomes and safe actions | `AwaitVerdictOutcome`, `AwaitSafeAction` (table 3c) | delivered (Task 5) |
| F4 await-resubmission outcomes | `AwaitResubmissionOutcome` (table 3c) | delivered (Task 6) |
| F5 submitted-verdict input set | `submittedVerdict` (table 3c); the quarantine compare uses it (`quarantined_verdict.go` `submittedVerdict(finding.Verdict) == submittedVerdictRejected`) | delivered (Task 4); disposition owner gap named here |
| F6 validation-readiness result, method, code set | `validationResult`, `validationMethod`, `validationCode` (table 3c); the execution policy values stay configured strings named `validationExecutionLocal`/`validationExecutionArtifactOnly` (table 3b: `models` keeps the configured value a plain string, user-configurable, so not closed into a type) | delivered (Task 2) |
| F7 review-claim classes | `ReviewClaimClass` (A3 I24) adopted by `claim_reviewer_task.go` and the reviewer-claim files; no parallel type | delivered (Task 7) |
| F8 `AcceptanceFault`, `OrphanedBaseHealOutcome` | kept and adopted (table 3c); `AcceptanceFault` gains its spelling pin | delivered |
| F9 timing, backoff, retry, truncation, byte limits | pre-existing in table 3b; this task named the 500-byte heal cause limit, the submit heartbeat default (the test-mutable `submitHeartbeatInterval` stays a variable, initialised from it), the connection-string suffixes, the lock labels and the file extensions and ID formats (table 2) | complete |
| F10 `reclaimReviewLeaseDuration`; reviewer-claim lease from `LeaseDuration` | `reclaimReviewLeaseDuration` keeps its name, value and single use (`review_reclaim.go:60`); the claim lease still derives from `LeaseDuration` in `claimReviewerTask`; both untouched | preserved for C1 |
| F11 history `Extra` keys of A3/A4 inventories for A5 writers | A4 `rejectionRCA*Key` adopted by the writers; A5's own `old/new_review_commit` keys adopted by their readers in `await_verdict_outcome.go` and `quarantined_verdict.go` (table 2); the keys no A5 or A4 decision reads back stay classified (table 3) | complete |
| F12 role names, role pairs, tool and provider names, configured commands, IDs, user text | dynamic, see section 9 | classified |

K1: this task adds no Git or `exec` call and moves no code: every edit replaces a literal with a constant of the same value, so the nine inherited exception sites K1-E1–E9 and their order are untouched.

## 9. Dynamic identities (F12)

Role names, role pairs, tool and provider names, configured commands, task, agent, finding and request IDs, commit SHAs, branch names and user text never appear as literals in the 48 files: they reach the code as variables (`rolePair`, `input.AgentID`, `taskID`, `findingID`, `reviewCommit`, `integrationBranch`, `reason`). The only literal that names an agent identity is `actorSystem` (A3 `helpers.go`), adopted. The hard-coded test-file naming conventions of `test_detection.go` (table 3, J22) are data about the user's project, matched by file name, and stay literal: promoting them to configuration is a new configuration field, which this run forbids. No configuration field is added.
