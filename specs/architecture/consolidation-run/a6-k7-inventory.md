# A6 K7 Inventory

Evidence for `arm-1-ar-15-cp-0` Task 7 (`arm-1-ar-15-cp-0-code-6`): the complete K7 occurrence-to-owner
classification of the A6 ops files, the substitutions applied, the owner gaps, the later-area adoption
sites and the reconciliation of the plan's K7 families F1–F9. Source of the rules is the plan section
"K7 Families" and the A3, A4 and A5 inventory precedent (`a4-k7-inventory.md`, `a5-k7-inventory.md`, rules R1–R5).

## Coding HEAD

- Integration HEAD at claim (A1, A3, A4, A5 delivered; A6 Tasks 2–6 merged, the last being `e7cbe3206`): `e7cbe32069b986eac8db07c8ff4d6f3356e1c24a`.
- The tables describe the production tree of the code commit `97d17dab166e3370fe8d0a247b40e6cef1e33cc2`, committed on top of that HEAD; line numbers are that tree's (the inventory and its manifest are added by the commits after it, which edit no Go file). Table 2 is derived from `git diff e7cbe3206 97d17dab1 -- internal/ops/`.
- No oracle file, fixture or golden is added or edited by this task: every substitution keeps its value, so no oracle-first checkpoint is needed.

## Method

1. File list: the 19 `A6` files of the area partition plus the 18 private files Tasks 2–6 created (names and creating task in table 1): 37 production files. The private set is not a glob of file names: it is the set of non-test files `git show --diff-filter=A` reports for the five task commits (`f1c5afd0c` Task 2, `bce406207` Task 3, `bafe7b0d6` Task 4, `18246b22d` Task 5, `e7cbe3206` Task 6), and the generator asserts equality with table 1. `*_test.go` files, the A3–A5 files and `init_project.go` are out of the list.
2. Every string and integer literal of those files was extracted with a `go/parser` scan (import paths and struct tags excluded) with its syntactic context (comparison, `case`, composite field, map key or index, call argument, declaration…): **898** literals at this tree.
3. Each non-message literal was matched by value against every string and integer constant of `ops` and of the A1 owner packages (`models`, `pipeline`, `roles`, `taskkind`, `paths`, `alerts`, `errors`, `payloadschema`, `referencecontract`, `statehygiene`, `brand`); a same-value, same-meaning constant is adopted (R1). Same-value constants of a different meaning are false friends (table 4).
4. A literal that is control-flow vocabulary or a repeated magic value without an owner is named in its concern (R4) or, when the owner is a closed package outside `ops` that names nothing, named in `ops` without editing the owner (R3, table 5). A finite set deciding control flow or persisted as a closed vocabulary is typed (R2).
5. Every remaining literal is classified (R5/F8/F9) with a reason in table 3; the classifier raises on a literal it cannot place, so none is unclassified or deferred.
6. Completeness check: the scan of the committed tree yields 898 rows; each is a row of table 3 (726 retained, grouped by file and reason) or part of a constant or variable declaration (172 rows, table 2 for the ones this task added, table 3b for the pre-existing ones).

Rules: **R1** adopt an existing owner constant; **R2** type a finite set; **R3** name in `ops` a value whose closed owner package names nothing; **R4** name an unowned value in its concern; **R5** classify retained literal data or a dynamic identity with a reason.
Outcome vocabulary: **adopted**, **named** (new constant, value unchanged), **typed** (defined type, one constant per value), **classified** (retained literal data or dynamic identity, with reason).

## Summary

| Outcome | Count |
|---|---|
| Substitution sites in this task (table 2: adopted, named or typed use sites) | 100 |
| Constant or variable declarations added by this task (table 2) | 63 |
| Pre-existing named constants in A6 files (table 3b) | 102 |
| Retained literals classified R5/F8/F9 (table 3, grouped by file and reason) | 726 |
| Literals scanned at this tree | 898 = 726 retained (table 3) + 172 inside declarations of named constants and variables (tables 2 and 3b) |
| Unclassified / deferred | 0 |
| New defined types | 2 (`deferredValidationUnattributedReason`, `worktreeSetupOutcomeStatus`); 3 members added to `deferredValidationSkipReason` and 5 deferred-validation members to `integrationProgressCode` |
| BLOCKED findings against A1 or A3 closure (prospective symbol not delivered) | 0 |

## 1. A6 files

| File | Origin | Lines | Literals scanned | Substitution sites | Declarations added | Named constants in file |
|---|---|---|---|---|---|---|
| `deferred_validation.go` | area partition | 210 | 65 | 0 | 34 | 61 |
| `deferred_validation_attribution.go` | private, Task 2 | 351 | 35 | 13 | 0 | 0 |
| `deferred_validation_lanes.go` | private, Task 2 | 565 | 67 | 12 | 0 | 3 |
| `deferred_validation_ruling.go` | area partition | 322 | 39 | 6 | 0 | 11 |
| `deferred_validation_runner.go` | private, Task 2 | 464 | 23 | 12 | 0 | 0 |
| `deferred_validation_schedule.go` | private, Task 2 | 439 | 48 | 16 | 1 | 1 |
| `functional_clusters_indexing.go` | area partition | 28 | 3 | 0 | 0 | 0 |
| `integration_global_surface.go` | area partition | 233 | 16 | 0 | 0 | 0 |
| `integration_head.go` | area partition | 21 | 4 | 0 | 0 | 1 |
| `integration_mutation_lock.go` | area partition | 81 | 10 | 2 | 2 | 5 |
| `integration_progress.go` | area partition | 641 | 61 | 0 | 1 | 13 |
| `integration_progress_coverage.go` | private, Task 3 | 281 | 28 | 1 | 0 | 0 |
| `integration_progress_global.go` | private, Task 3 | 120 | 13 | 0 | 0 | 0 |
| `integration_progress_lineage.go` | private, Task 3 | 222 | 18 | 1 | 0 | 0 |
| `integration_reconcile.go` | area partition | 639 | 68 | 3 | 1 | 8 |
| `merge_admission.go` | private, Task 5 | 174 | 7 | 1 | 0 | 0 |
| `merge_artifact_guard.go` | private, Task 4 | 95 | 3 | 0 | 0 | 0 |
| `merge_failure.go` | private, Task 5 | 291 | 42 | 2 | 0 | 1 |
| `merge_finalize.go` | private, Task 5 | 229 | 21 | 1 | 0 | 1 |
| `merge_forward.go` | private, Task 5 | 117 | 7 | 0 | 0 | 0 |
| `merge_preconditions.go` | private, Task 5 | 101 | 7 | 3 | 0 | 0 |
| `merge_publication.go` | private, Task 4 | 138 | 18 | 0 | 0 | 2 |
| `merge_ref_mutation.go` | private, Task 4 | 169 | 12 | 0 | 0 | 0 |
| `merge_validation.go` | private, Task 5 | 134 | 9 | 0 | 0 | 1 |
| `merged_task_retirement.go` | private, Task 5 | 33 | 3 | 0 | 0 | 0 |
| `post_worktree_setup.go` | area partition | 495 | 57 | 10 | 12 | 15 |
| `post_worktree_setup_error.go` | area partition | 69 | 6 | 0 | 0 | 1 |
| `reconcile_merged.go` | area partition | 149 | 19 | 1 | 0 | 0 |
| `recover_integration.go` | area partition | 260 | 47 | 0 | 0 | 3 |
| `run_live.go` | area partition | 162 | 36 | 2 | 3 | 6 |
| `scip_indexing.go` | area partition | 37 | 3 | 0 | 0 | 0 |
| `semble_indexing.go` | area partition | 204 | 27 | 5 | 3 | 6 |
| `stacklit_indexing.go` | area partition | 27 | 3 | 0 | 0 | 0 |
| `worktree_provisioning.go` | private, Task 6 | 49 | 0 | 0 | 0 | 0 |
| `wt_create.go` | area partition | 460 | 47 | 7 | 6 | 16 |
| `wt_delete.go` | area partition | 106 | 8 | 1 | 0 | 0 |
| `wt_merge.go` | area partition | 291 | 18 | 1 | 0 | 9 |

## 2. Adopted, named and typed (substitutions of this task)

| Site | Family | Rule | Outcome | Constant | Literal | Owner |
|---|---|---|---|---|---|---|
| `deferred_validation.go:40` | F4 | R2 | declaration of typed (string conversion) | `IntegrationProgressWaitingDeferredValidation, integrationProgressWaitingDeferred` | string(integrationProgressWaitingDeferred), "deferred_validation_pending" | A6 deferred_validation.go |
| `deferred_validation.go:45` | F4 | R2 | declaration of typed | `integrationProgressWaitingDeferred` | "deferred_validation_pending" | A6 deferred_validation.go |
| `deferred_validation.go:46` | F4 | R2 | declaration of typed | `integrationProgressBlockedDeferredFailed` | "deferred_validation_failed" | A6 deferred_validation.go |
| `deferred_validation.go:47` | F4 | R2 | declaration of typed | `integrationProgressBlockedDeferredInfra` | "deferred_validation_infra" | A6 deferred_validation.go |
| `deferred_validation.go:48` | F4 | R2 | declaration of typed | `integrationProgressBlockedDeferredUnsettled` | "deferred_validation_unsettled" | A6 deferred_validation.go |
| `deferred_validation.go:49` | F4 | R2 | declaration of typed | `integrationProgressBlockedDeferredAborted` | "deferred_validation_aborted" | A6 deferred_validation.go |
| `deferred_validation.go:63` | F7 | R4 | declaration of named | `deferredValidationDirMode` | 0o755 | A6 deferred_validation.go |
| `deferred_validation.go:64` | F7 | R4 | declaration of named | `deferredValidationLogFileMode` | 0o644 | A6 deferred_validation.go |
| `deferred_validation.go:68` | F6 | R4 | declaration of named | `deferredLaneWorktreeName` | "worktree" | A6 deferred_validation.go |
| `deferred_validation.go:69` | F6 | R4 | declaration of named | `deferredLaneWorktreeFormat` | "worktree-%d" | A6 deferred_validation.go |
| `deferred_validation.go:70` | F6 | R4 | declaration of named | `deferredLaneWorktreeGlob` | "worktree-*" | A6 deferred_validation.go |
| `deferred_validation.go:74` | F6 | R4 | declaration of named | `deferredPathspecMagicPrefix` | ":" | A6 deferred_validation.go |
| `deferred_validation.go:75` | F6 | R4 | declaration of named | `deferredPathspecExcludePrefix` | ":(exclude)" | A6 deferred_validation.go |
| `deferred_validation.go:79` | F6 | R4 | declaration of named | `deferredAlertOnceKeyFormat` | "deferred-validation:%d:%s" | A6 deferred_validation.go |
| `deferred_validation.go:80` | F6 | R4 | declaration of named | `deferredDiscoveryIDFormat` | "deferred-validation-%d-%d" | A6 deferred_validation.go |
| `deferred_validation.go:83` | F7 | R4 | declaration of named | `deferredGitExitNegative` | 1 | A6 deferred_validation.go |
| `deferred_validation.go:86` | F7 | R4 | declaration of named | `deferredValidationStatusBatches` | 5 | A6 deferred_validation.go |
| `deferred_validation.go:90` | F6 | R4 | declaration of named | `deferredValidationSubcommandStatus` | "status" | A6 deferred_validation.go |
| `deferred_validation.go:91` | F6 | R4 | declaration of named | `deferredValidationSubcommandRun` | "run" | A6 deferred_validation.go |
| `deferred_validation.go:92` | F6 | R4 | declaration of named | `deferredValidationFlagForce` | "--force" | A6 deferred_validation.go |
| `deferred_validation.go:97` | F6 | R4 | declaration of named | `deferredDiscoveryKeyBatch` | "batch" | A6 deferred_validation.go |
| `deferred_validation.go:98` | F6 | R4 | declaration of named | `deferredDiscoveryKeyBatchCommit` | "batch_commit" | A6 deferred_validation.go |
| `deferred_validation.go:99` | F6 | R4 | declaration of named | `deferredDiscoveryKeyCommandSHA256` | "command_sha256" | A6 deferred_validation.go |
| `deferred_validation.go:100` | F6 | R4 | declaration of named | `deferredDiscoveryKeyCulpritTask` | "culprit_task" | A6 deferred_validation.go |
| `deferred_validation.go:101` | F6 | R4 | declaration of named | `deferredDiscoveryKeyMergeCommit` | "merge_commit" | A6 deferred_validation.go |
| `deferred_validation.go:111` | F5 | R2 | declaration of typed | `deferredValidationUnattributedNoBaseline` | "no_baseline" | A6 deferred_validation.go |
| `deferred_validation.go:112` | F5 | R2 | declaration of typed | `deferredValidationUnattributedBaselineNotAncestor` | "baseline_not_in_ancestry" | A6 deferred_validation.go |
| `deferred_validation.go:113` | F5 | R2 | declaration of typed | `deferredValidationUnattributedHistoryUnavailable` | "history_unavailable" | A6 deferred_validation.go |
| `deferred_validation.go:114` | F5 | R2 | declaration of typed | `deferredValidationUnattributedFailsAtBaseline` | "fails_at_baseline" | A6 deferred_validation.go |
| `deferred_validation.go:115` | F5 | R2 | declaration of typed | `deferredValidationUnattributedFailsAfterLastMerge` | "fails_after_last_merge" | A6 deferred_validation.go |
| `deferred_validation.go:117` | F5 | R4 | declaration of named | `deferredValidationReasonSetupUnavailableFormat` | "setup_unavailable at %s" | A6 deferred_validation.go |
| `deferred_validation.go:163` | F5 | R2 | declaration of typed | `deferredValidationSkipNothingDeferred` | "nothing_deferred" | A6 deferred_validation.go |
| `deferred_validation.go:164` | F5 | R2 | declaration of typed | `deferredValidationSkipAttemptsExhausted` | "attempts_exhausted" | A6 deferred_validation.go |
| `deferred_validation.go:165` | F5 | R2 | declaration of typed | `deferredValidationSkipNotDue` | "not_due" | A6 deferred_validation.go |
| `deferred_validation_attribution.go:89` | F7 | R1 | adopted | `acceptanceExitUnknown` | -1 (exit status never reported) | acceptance_execution.go (A5) |
| `deferred_validation_attribution.go:117` | F6 | R4 | named | `deferredDiscoveryKeyCommandSHA256, deferredDiscoveryKeyCulpritTask` | "command_sha256", "culprit_task" | A6 deferred_validation_attribution.go |
| `deferred_validation_attribution.go:147` | F7 | R4 | named | `deferredGitExitNegative` | 1 | A6 deferred_validation_attribution.go |
| `deferred_validation_attribution.go:159` | F6 | R4 | named | `deferredPathspecMagicPrefix` | ":" | A6 deferred_validation_attribution.go |
| `deferred_validation_attribution.go:173` | F6 | R4 | named | `deferredPathspecExcludePrefix` | ":(exclude)" | A6 deferred_validation_attribution.go |
| `deferred_validation_attribution.go:200` | F5 | R2 | typed | `deferredValidationUnattributedNoBaseline` | "no_baseline" | A6 deferred_validation_attribution.go |
| `deferred_validation_attribution.go:207` | F5 | R2 | typed | `deferredValidationUnattributedBaselineNotAncestor` | "baseline_not_in_ancestry" | A6 deferred_validation_attribution.go |
| `deferred_validation_attribution.go:214` | F5 | R2 | typed | `deferredValidationUnattributedHistoryUnavailable` | "history_unavailable" | A6 deferred_validation_attribution.go |
| `deferred_validation_attribution.go:270` | F7 | R1 | adopted | `acceptanceExitUnknown` | -1 (exit status never reported) | acceptance_execution.go (A5) |
| `deferred_validation_attribution.go:291` | F5 | R2 | typed | `deferredValidationUnattributedFailsAtBaseline` | "fails_at_baseline" | A6 deferred_validation_attribution.go |
| `deferred_validation_attribution.go:293` | F5 | R4 | named | `deferredValidationReasonSetupUnavailableFormat` | "setup_unavailable at %s" | A6 deferred_validation_attribution.go |
| `deferred_validation_attribution.go:308` | F5 | R4 | named | `deferredValidationReasonSetupUnavailableFormat` | "setup_unavailable at %s" | A6 deferred_validation_attribution.go |
| `deferred_validation_attribution.go:312` | F5 | R2 | typed | `deferredValidationUnattributedFailsAfterLastMerge` | "fails_after_last_merge" | A6 deferred_validation_attribution.go |
| `deferred_validation_lanes.go:68` | F7 | R1 | adopted | `acceptanceExitUnknown` | -1 (exit status never reported) | acceptance_execution.go (A5) |
| `deferred_validation_lanes.go:95` | F7 | R1 | adopted | `acceptanceExitUnknown` | -1 (exit status never reported) | acceptance_execution.go (A5) |
| `deferred_validation_lanes.go:100` | F7 | R1 | adopted | `acceptanceExitUnknown` | -1 (exit status never reported) | acceptance_execution.go (A5) |
| `deferred_validation_lanes.go:144` | F7 | R4 | named | `deferredGitExitNegative` | 1 | A6 deferred_validation_lanes.go |
| `deferred_validation_lanes.go:165` | F6 | R4 | named | `deferredLaneWorktreeName` | "worktree" | A6 deferred_validation_lanes.go |
| `deferred_validation_lanes.go:167` | F6 | R4 | named | `deferredLaneWorktreeFormat` | "worktree-%d" | A6 deferred_validation_lanes.go |
| `deferred_validation_lanes.go:178` | F6 | R4 | named | `deferredLaneWorktreeGlob` | "worktree-*" | A6 deferred_validation_lanes.go |
| `deferred_validation_lanes.go:180` | F7 | R1 | adopted | `acceptanceExitUnknown` | -1 (exit status never reported) | acceptance_execution.go (A5) |
| `deferred_validation_lanes.go:290` | F7 | R1 | adopted | `acceptanceExitUnknown` | -1 (exit status never reported) | acceptance_execution.go (A5) |
| `deferred_validation_lanes.go:297` | F7 | R1 | adopted | `acceptanceExitUnknown` | -1 (exit status never reported) | acceptance_execution.go (A5) |
| `deferred_validation_lanes.go:310` | F7 | R1 | adopted | `acceptanceExitUnknown` | -1 (exit status never reported) | acceptance_execution.go (A5) |
| `deferred_validation_lanes.go:523` | F7 | R1 | adopted | `acceptanceExitUnknown` | -1 (exit status never reported) | acceptance_execution.go (A5) |
| `deferred_validation_ruling.go:71` | F6 | R1 | adopted | `actorOperator` | "operator" | helpers.go (A3) |
| `deferred_validation_ruling.go:175` | F7 | R4 | named | `deferredValidationDirMode` | 0o755 | A6 deferred_validation_ruling.go |
| `deferred_validation_ruling.go:219` | F7 | R4 | named | `deferredValidationDirMode` | 0o755 | A6 deferred_validation_ruling.go |
| `deferred_validation_ruling.go:228` | F5 | R1 | adopted | `rulingActionSettle` | "settle" (branded subcommand) | deferred_validation_ruling.go (A6) |
| `deferred_validation_ruling.go:238` | F5 | R1 | adopted | `rulingActionSettle` | "settle" (branded subcommand) | deferred_validation_ruling.go (A6) |
| `deferred_validation_ruling.go:310` | F7 | R4 | named | `deferredValidationStatusBatches` | 5 | A6 deferred_validation_ruling.go |
| `deferred_validation_runner.go:62` | F7 | R4 | named | `deferredValidationDirMode` | 0o755 | A6 deferred_validation_runner.go |
| `deferred_validation_runner.go:140` | F5 | R1 | adopted | `rulingActionSettle` | "settle" (branded subcommand) | deferred_validation_ruling.go (A6) |
| `deferred_validation_runner.go:317` | F5 | R1 | adopted | `rulingActionSettle` | "settle" (branded subcommand) | deferred_validation_ruling.go (A6) |
| `deferred_validation_runner.go:365` | F6 | R4 | named | `deferredAlertOnceKeyFormat` | "deferred-validation:%d:%s" | A6 deferred_validation_runner.go |
| `deferred_validation_runner.go:385` | F6 | R4 | named | `deferredDiscoveryIDFormat` | "deferred-validation-%d-%d" | A6 deferred_validation_runner.go |
| `deferred_validation_runner.go:394` | F6 | R4 | named | `deferredDiscoveryKeyBatch` | "batch" | A6 deferred_validation_runner.go |
| `deferred_validation_runner.go:395` | F6 | R4 | named | `deferredDiscoveryKeyBatchCommit` | "batch_commit" | A6 deferred_validation_runner.go |
| `deferred_validation_runner.go:396` | F6 | R4 | named | `deferredDiscoveryKeyCommandSHA256` | "command_sha256" | A6 deferred_validation_runner.go |
| `deferred_validation_runner.go:397` | F6 | R4 | named | `deferredDiscoveryKeyCulpritTask` | "culprit_task" | A6 deferred_validation_runner.go |
| `deferred_validation_runner.go:398` | F6 | R4 | named | `deferredDiscoveryKeyMergeCommit` | "merge_commit" | A6 deferred_validation_runner.go |
| `deferred_validation_runner.go:408` | F6 | R4 | named | `deferredDiscoveryKeyCommandSHA256, deferredDiscoveryKeyCulpritTask` | "command_sha256", "culprit_task" | A6 deferred_validation_runner.go |
| `deferred_validation_runner.go:429` | F6 | R4 | named | `deferredValidationSubcommandStatus` | "status" | A6 deferred_validation_runner.go |
| `deferred_validation_schedule.go:126` | F4 | R2 | typed | `integrationProgressBlockedDeferredUnsettled` | "deferred_validation_unsettled" | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:127` | F5 | R1 | adopted | `rulingActionSettle` | "settle" (branded subcommand) | deferred_validation_ruling.go (A6) |
| `deferred_validation_schedule.go:135` | F6 | R4 | named | `deferredValidationSubcommandStatus` | "status" | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:140` | F4 | R2 | typed | `integrationProgressBlockedDeferredFailed` | "deferred_validation_failed" | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:147` | F4 | R2 | typed | `integrationProgressBlockedDeferredAborted` | "deferred_validation_aborted" | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:148` | F6 | R4 | named | `deferredValidationSubcommandRun, deferredValidationFlagForce` | "run", "--force" | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:154` | F4 | R2 | typed | `integrationProgressBlockedDeferredInfra` | "deferred_validation_infra" | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:155` | F6 | R4 | named | `deferredValidationSubcommandRun, deferredValidationFlagForce` | "run", "--force" | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:158` | F4 | R2 | typed | `integrationProgressWaitingDeferred` | "deferred_validation_pending" | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:292` | F5 | R2 | typed | `deferredValidationSkipNothingDeferred` | "nothing_deferred" | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:306` | F5 | R2 | typed | `deferredValidationSkipAttemptsExhausted` | "attempts_exhausted" | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:322` | F5 | R2 | typed | `deferredValidationSkipNotDue` | "not_due" | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:363` | F7 | R4 | named | `deferredValidationDirMode` | 0o755 | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:392` | F7 | R4 | named | `deferredValidationLogFileMode` | 0o644 | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:397` | F6 | R4 | named | `deferredValidationSubcommandRun` | "run" | A6 deferred_validation_schedule.go |
| `deferred_validation_schedule.go:423` | F6 | R3 | declaration of named | `taskIDEnvSuffix` | "TASK_ID" | A6 deferred_validation_schedule.go; `brand` owner gap |
| `deferred_validation_schedule.go:427` | F6 | R1 | adopted | `agentIDEnvSuffix, agentGenerationEnvSuffix, taskIDEnvSuffix` | "AGENT_ID", "AGENT_GENERATION", "TASK_ID" | submission_lifecycle.go (A5); A6 deferred_validation_schedule.go; `brand` owner gap |
| `integration_mutation_lock.go:26` | F6 | R4 | declaration of named | `integrationLockVerifyClean` | "verify clean integration source" | A6 integration_mutation_lock.go |
| `integration_mutation_lock.go:31` | F6 | R4 | declaration of named | `integrationMutationLockName` | "integration-mutation" | A6 integration_mutation_lock.go |
| `integration_mutation_lock.go:38` | F6 | R4 | named | `integrationMutationLockName` | "integration-mutation" | A6 integration_mutation_lock.go |
| `integration_mutation_lock.go:67` | F6 | R4 | named | `integrationLockVerifyClean` | "verify clean integration source" | A6 integration_mutation_lock.go |
| `integration_progress.go:36` | F7 | R4 | declaration of named | `slicedCohortMinScopes` | 2 | A6 integration_progress.go |
| `integration_progress_coverage.go:22` | F7 | R4 | named | `slicedCohortMinScopes` | 2 | A6 integration_progress_coverage.go |
| `integration_progress_lineage.go:97` | F8 | R1 | adopted | `codePlanningRolePair` | "code-planning-pair" | integration_reconcile.go alias of legacyPlanningRolePair (pipeline_ops.go, A4) |
| `integration_reconcile.go:30` | F7 | R4 | declaration of named | `integrationAnalysisTaskPriority` | 1 | A6 integration_reconcile.go |
| `integration_reconcile.go:338` | F7 | R4 | named | `integrationAnalysisTaskPriority` | 1 | A6 integration_reconcile.go |
| `integration_reconcile.go:504` | F6 | R1 | adopted | `gitModeRegularFile, gitModeExecutableFile` | "100644", "100755" | runtime_input_admission.go (A3) |
| `integration_reconcile.go:521` | F7 | R4 | named | `slicedCohortMinScopes` | 2 | A6 integration_reconcile.go |
| `merge_admission.go:117` | F6 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `merge_failure.go:91` | F6 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `merge_failure.go:271` | F2 | R1 | adopted | `LifecycleSafeActionRequery` | "requery" | lifecycle_result.go (A3) |
| `merge_finalize.go:65` | F6 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `merge_preconditions.go:24` | F2 | R1 | adopted | `LifecycleSafeActionStop, LifecycleEffectsNone` | "stop", "none" | lifecycle_result.go (A3) |
| `merge_preconditions.go:82` | F6 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `merge_preconditions.go:85` | F2 | R1 | adopted | `LifecycleSafeActionRequery, LifecycleEffectsNone` | "requery", "none" | lifecycle_result.go (A3) |
| `post_worktree_setup.go:37` | F7 | R4 | declaration of named | `postWorktreeSetupDefaultRetryDelay` | 2*time.Second | A6 post_worktree_setup.go |
| `post_worktree_setup.go:38` | F7 | R4 | declaration of named | `worktreeSetupDefaultLockSlack` | time.Minute | A6 post_worktree_setup.go |
| `post_worktree_setup.go:40` | F6 | R4 | declaration of named | `worktreeSetupOutputExt` | ".log" | A6 post_worktree_setup.go |
| `post_worktree_setup.go:41` | F5 | R2 | declaration of typed | `worktreeSetupOutcomeExt` | ".outcome" | A6 post_worktree_setup.go |
| `post_worktree_setup.go:45` | F6 | R4 | declaration of named | `worktreeSetupLockOperation` | "worktree-setup" | A6 post_worktree_setup.go |
| `post_worktree_setup.go:46` | F6 | R4 | declaration of named | `worktreeSetupLockPurposePrefix` | "worktree-setup-" | A6 post_worktree_setup.go |
| `post_worktree_setup.go:49` | F5 | R2 | declaration of typed | `worktreeSetupOutcomeFields` | 3 | A6 post_worktree_setup.go |
| `post_worktree_setup.go:52` | F7 | R4 | declaration of named | `worktreeSetupDigestBytes` | 8 | A6 post_worktree_setup.go |
| `post_worktree_setup.go:55` | F7 | R4 | declaration of named | `worktreeSetupDirMode` | 0o700 | A6 post_worktree_setup.go |
| `post_worktree_setup.go:56` | F7 | R4 | declaration of named | `worktreeSetupFileMode` | 0o600 | A6 post_worktree_setup.go |
| `post_worktree_setup.go:64` | F5 | R2 | declaration of typed | `worktreeSetupOutcomeOK` | "ok" | A6 post_worktree_setup.go |
| `post_worktree_setup.go:65` | F5 | R2 | declaration of typed | `worktreeSetupOutcomeFailed` | "failed" | A6 post_worktree_setup.go |
| `post_worktree_setup.go:74` | F7 | R4 | named | `postWorktreeSetupDefaultRetryDelay` | 2*time.Second | A6 post_worktree_setup.go |
| `post_worktree_setup.go:78` | F7 | R4 | named | `worktreeSetupDefaultLockSlack` | time.Minute | A6 post_worktree_setup.go |
| `post_worktree_setup.go:128` | F6 | R4 | named | `worktreeSetupLockOperation` | "worktree-setup" | A6 post_worktree_setup.go |
| `post_worktree_setup.go:317` | F7 | R4 | named | `worktreeSetupDigestBytes` | 8 | A6 post_worktree_setup.go |
| `post_worktree_setup.go:319` | F7 | R4 | named | `worktreeSetupDirMode` | 0o700 | A6 post_worktree_setup.go |
| `post_worktree_setup.go:325` | F6 | R4 | named | `worktreeSetupOutputExt` | ".log" | A6 post_worktree_setup.go |
| `post_worktree_setup.go:326` | F5 | R2 | typed | `worktreeSetupOutcomeExt` | ".outcome" | A6 post_worktree_setup.go |
| `post_worktree_setup.go:345` | F6 | R4 | named | `worktreeSetupLockPurposePrefix` | "worktree-setup-" | A6 post_worktree_setup.go |
| `post_worktree_setup.go:387` | F7 | R4 | named | `worktreeSetupFileMode` | 0o600 | A6 post_worktree_setup.go |
| `post_worktree_setup.go:421` | F5 | R2 | typed | `worktreeSetupOutcomeFields` | 3 | A6 post_worktree_setup.go |
| `reconcile_merged.go:76` | F6 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `run_live.go:29` | F6 | R4 | declaration of named | `runLiveShellFlag` | "-c" | A6 run_live.go |
| `run_live.go:30` | F6 | R4 | declaration of named | `runLiveShellWrapperArgs` | 3 | A6 run_live.go |
| `run_live.go:35` | F6 | R4 | declaration of named | `runLiveShells` | []string{"sh", "bash", "zsh", "dash"} | A6 run_live.go |
| `run_live.go:129` | F7 | R1 | adopted | `acceptanceExitUnknown` | -1 (exit status never reported) | acceptance_execution.go (A5) |
| `run_live.go:145` | F6 | R4 | named | `runLiveShellFlag, runLiveShellWrapperArgs, runLiveShells` | "-c", 3, []string{"sh", "bash", "zsh", "dash"} | A6 run_live.go |
| `semble_indexing.go:22` | F7 | R4 | declaration of named | `sembleIgnoreFileMode` | 0o644 | A6 semble_indexing.go |
| `semble_indexing.go:24` | F9 | R4 | declaration of named | `sembleWarningEllipsis` | "..." | A6 semble_indexing.go |
| `semble_indexing.go:27` | F7 | R4 | declaration of named | `sembleLsFilesUnmatchedExit` | 1 | A6 semble_indexing.go |
| `semble_indexing.go:78` | F7 | R4 | named | `sembleLsFilesUnmatchedExit` | 1 | A6 semble_indexing.go |
| `semble_indexing.go:105` | F7 | R4 | named | `sembleIgnoreFileMode` | 0o644 | A6 semble_indexing.go |
| `semble_indexing.go:117` | F7 | R4 | named | `sembleIgnoreFileMode` | 0o644 | A6 semble_indexing.go |
| `semble_indexing.go:195` | F9 | R4 | named | `sembleWarningEllipsis` | "..." | A6 semble_indexing.go |
| `semble_indexing.go:203` | F7 | R1 | adopted | `acceptanceExitUnknown` | -1 (exit status never reported) | acceptance_execution.go (A5) |
| `wt_create.go:27` | F6 | R4 | named | `worktreeHooksDirSuffix` | "-hooks" | A6 wt_create.go |
| `wt_create.go:45` | F6 | R4 | declaration of named | `worktreeCreateOperation` | "worktree-create" | A6 wt_create.go |
| `wt_create.go:48` | F6 | R4 | declaration of named | `worktreeHooksDirSuffix` | "-hooks" | A6 wt_create.go |
| `wt_create.go:52` | F6 | R4 | declaration of named | `worktreeEnvFileName` | ".env" | A6 wt_create.go |
| `wt_create.go:53` | F6 | R4 | declaration of named | `worktreeEnvFilePrefix` | ".env." | A6 wt_create.go |
| `wt_create.go:54` | F6 | R4 | declaration of named | `worktreeEnvFileSuffix` | ".env" | A6 wt_create.go |
| `wt_create.go:55` | F6 | R4 | declaration of named | `worktreeEnvrcFileName` | ".envrc" | A6 wt_create.go |
| `wt_create.go:72` | F6 | R4 | named | `worktreeCreateOperation` | "worktree-create" | A6 wt_create.go |
| `wt_create.go:171` | F6 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `wt_create.go:348` | F6 | R4 | named | `worktreeEnvFileName` | ".env" | A6 wt_create.go |
| `wt_create.go:349` | F6 | R4 | named | `worktreeEnvFilePrefix` | ".env." | A6 wt_create.go |
| `wt_create.go:350` | F6 | R4 | named | `worktreeEnvFileSuffix` | ".env" | A6 wt_create.go |
| `wt_create.go:351` | F6 | R4 | named | `worktreeEnvrcFileName` | ".envrc" | A6 wt_create.go |
| `wt_delete.go:76` | F6 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `wt_merge.go:146` | F2 | R1 | adopted | `LifecycleSafeActionCorrectInput, LifecycleEffectsNone` | "correct_input", "none" | lifecycle_result.go (A3) |

## 3. Classified literals (R5 / F8 / F9)

Rows are grouped by file and reason; the line numbers are the tree's. Messages keep their exact wording because `internal/jsonout/classify.go` classifies untyped errors by substring (goal-frozen "Error message text").

| File | Kind | Reason | Count | Lines |
|---|---|---|---|---|
| `deferred_validation.go` | other literal | `runtime.GOOS` platform identifier compared where the platform cannot support the feature (owner gap, table 5) | 1 | 182 |
| `deferred_validation_attribution.go` | message | runner diagnostic line written to the batch log | 3 | 36, 44, 85 |
| `deferred_validation_attribution.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 16 | 78, 82, 98, 103, 103, 104, 133, 159, 199, 221, 221, 232, 245, 248, 252, 255 |
| `deferred_validation_attribution.go` | Git argv | Git subcommand, option or revision argument (external CLI vocabulary) | 9 | 145, 145, 145, 145, 145, 145, 323, 323, 323 |
| `deferred_validation_attribution.go` | Git argv | Git pathspec selecting the whole tree | 1 | 161 |
| `deferred_validation_attribution.go` | number | zero: emptiness, count or sign test | 1 | 163 |
| `deferred_validation_attribution.go` | number | negative one: bisect lower bound before the first candidate | 1 | 295 |
| `deferred_validation_attribution.go` | number | one: exactly-one or more-than-one test | 1 | 296 |
| `deferred_validation_attribution.go` | number | two: midpoint of the bisect interval | 1 | 297 |
| `deferred_validation_attribution.go` | Git argv | Git revision-range operator `from..to` | 1 | 323 |
| `deferred_validation_attribution.go` | number | zero: initial length of a made slice or map | 1 | 345 |
| `deferred_validation_lanes.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 4 | 82, 116, 290, 297 |
| `deferred_validation_lanes.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 12 | 105, 154, 250, 296, 318, 411, 416, 453, 490, 495, 509, 517 |
| `deferred_validation_lanes.go` | Git argv | executable name run with the project's Git environment | 1 | 133 |
| `deferred_validation_lanes.go` | Git argv | Git subcommand, option or revision argument (external CLI vocabulary) | 23 | 143, 143, 157, 157, 175, 175, 190, 190, 190, 202, 202, 202, 213, 213, 240, 240, 240, 243, 243, 285, 292, 292, 292 |
| `deferred_validation_lanes.go` | Git argv | Git first-parent revision suffix | 1 | 157 |
| `deferred_validation_lanes.go` | number | zero: emptiness, count or sign test | 3 | 164, 381, 467 |
| `deferred_validation_lanes.go` | number | zero: the first lane, which keeps the single-lane worktree path | 1 | 182 |
| `deferred_validation_lanes.go` | number | one: first attempt or generation of a counting loop | 1 | 239 |
| `deferred_validation_lanes.go` | message | runner diagnostic line written to the batch log | 3 | 259, 402, 415 |
| `deferred_validation_lanes.go` | message | composed message, warning, guidance or identifier text | 3 | 272, 274, 473 |
| `deferred_validation_lanes.go` | number | unit conversion: 20 bits per MiB in the output-limit message | 1 | 272 |
| `deferred_validation_lanes.go` | Git argv | Git revision name passed to Git or to the ref helper | 1 | 285 |
| `deferred_validation_lanes.go` | number | zero: initial or reset value | 1 | 299 |
| `deferred_validation_lanes.go` | other literal | Unicode replacement character substituted for invalid UTF-8 | 1 | 376 |
| `deferred_validation_lanes.go` | number | two: halves of the head and tail kept around the omission marker | 2 | 380, 380 |
| `deferred_validation_lanes.go` | other literal | path component of the shared tool-results directory (owner `toolresult`/`paths`; owner gap, table 5) | 1 | 396 |
| `deferred_validation_lanes.go` | number | zero: first element or first argument | 1 | 483 |
| `deferred_validation_lanes.go` | number | one: drop-first slice | 1 | 484 |
| `deferred_validation_lanes.go` | number | one: one goroutine counted by the wait group | 1 | 485 |
| `deferred_validation_lanes.go` | message | fragment of a composed message, warning or truncation marker (frozen wording) | 1 | 546 |
| `deferred_validation_lanes.go` | other literal | line separator of captured text | 1 | 546 |
| `deferred_validation_ruling.go` | message | field label in the validation-violation message for a deferred command | 1 | 59 |
| `deferred_validation_ruling.go` | number | zero: emptiness, count or sign test | 1 | 59 |
| `deferred_validation_ruling.go` | number | zero: first element or first argument | 1 | 60 |
| `deferred_validation_ruling.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 8 | 62, 106, 127, 171, 202, 215, 237, 248 |
| `deferred_validation_ruling.go` | message | precondition or refusal reason text (classified messages are frozen) | 6 | 63, 151, 172, 183, 216, 235 |
| `deferred_validation_ruling.go` | message | log line (free surface, goal-frozen) | 1 | 70 |
| `deferred_validation_ruling.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 5 | 97, 129, 162, 204, 250 |
| `deferred_validation_ruling.go` | number | zero: slice start | 1 | 116 |
| `deferred_validation_ruling.go` | message | composed message, warning, guidance or identifier text | 3 | 141, 228, 238 |
| `deferred_validation_ruling.go` | number | zero: lower clamp of a window start | 1 | 310 |
| `deferred_validation_runner.go` | message | composed message, warning, guidance or identifier text | 7 | 59, 140, 317, 423, 425, 431, 433 |
| `deferred_validation_runner.go` | number | zero: initial or reset value | 1 | 99 |
| `deferred_validation_runner.go` | number | zero: emptiness, count or sign test | 3 | 108, 114, 115 |
| `deferred_validation_runner.go` | number | one: last index, len(x)-1 | 1 | 108 |
| `deferred_validation_runner.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 8 | 206, 270, 301, 314, 378, 422, 430, 453 |
| `deferred_validation_runner.go` | message | runner diagnostic line written to the batch log | 1 | 251 |
| `deferred_validation_runner.go` | message | precondition or refusal reason text (classified messages are frozen) | 1 | 343 |
| `deferred_validation_runner.go` | number | one: next element, sequence number or attempt, x+1 | 1 | 385 |
| `deferred_validation_schedule.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 4 | 35, 38, 272, 390 |
| `deferred_validation_schedule.go` | message | precondition or refusal reason text (classified messages are frozen) | 1 | 38 |
| `deferred_validation_schedule.go` | number | zero: emptiness, count or sign test | 10 | 48, 53, 63, 71, 108, 121, 136, 195, 291, 349 |
| `deferred_validation_schedule.go` | number | zero: initial length of a made slice or map | 2 | 51, 431 |
| `deferred_validation_schedule.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 12 | 70, 124, 164, 204, 234, 239, 248, 310, 314, 317, 320, 352 |
| `deferred_validation_schedule.go` | number | one: last index, len(x)-1 | 2 | 71, 111 |
| `deferred_validation_schedule.go` | number | zero: initial or reset value | 1 | 96 |
| `deferred_validation_schedule.go` | message | composed message, warning, guidance or identifier text | 5 | 127, 135, 137, 148, 155 |
| `deferred_validation_schedule.go` | other literal | list separator in a composed message | 1 | 137 |
| `deferred_validation_schedule.go` | number | one: previous element or generation, x-1 | 1 | 196 |
| `deferred_validation_schedule.go` | Git argv | Git subcommand, option or revision argument (external CLI vocabulary) | 3 | 237, 237, 270 |
| `deferred_validation_schedule.go` | Git argv | Git first-parent revision suffix | 1 | 237 |
| `deferred_validation_schedule.go` | number | zero: no count | 2 | 249, 253 |
| `deferred_validation_schedule.go` | Git argv | Git revision-range operator `from..to` | 1 | 251 |
| `deferred_validation_schedule.go` | other literal | separator of an environment entry `NAME=value` | 1 | 433 |
| `functional_clusters_indexing.go` | F9 warning prefix | frozen warning prefix of an indexing refresh (D-5-excluded function; classified, not edited) | 2 | 17, 25 |
| `functional_clusters_indexing.go` | number | zero: initial length of a made slice or map | 1 | 23 |
| `integration_global_surface.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 6 | 67, 96, 128, 162, 206, 210 |
| `integration_global_surface.go` | number | zero: initial length of a made slice or map | 6 | 81, 100, 178, 191, 201, 224 |
| `integration_global_surface.go` | number | one: first attempt or generation of a counting loop | 1 | 121 |
| `integration_global_surface.go` | number | one: previous element or generation, x-1 | 1 | 147 |
| `integration_global_surface.go` | number | zero: emptiness, count or sign test | 1 | 184 |
| `integration_global_surface.go` | number | two: a seam is shared when at least two plans own it | 1 | 227 |
| `integration_head.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 17, 18 |
| `integration_head.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 1 | 18 |
| `integration_mutation_lock.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 3 | 50, 70, 74 |
| `integration_mutation_lock.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 77 |
| `integration_progress.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 16 | 117, 121, 125, 140, 156, 159, 165, 168, 180, 186, 214, 454, 507, 510, 515, 519 |
| `integration_progress.go` | number | zero: initial length of a made slice or map | 8 | 149, 238, 334, 430, 489, 529, 530, 531 |
| `integration_progress.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 6 | 155, 164, 175, 375, 481, 506 |
| `integration_progress.go` | number | zero: emptiness, count or sign test | 13 | 263, 301, 329, 347, 398, 412, 420, 420, 514, 558, 562, 566, 606 |
| `integration_progress.go` | message | fragment of a composed message, warning or truncation marker (frozen wording) | 1 | 360 |
| `integration_progress.go` | CLI guidance | branded CLI subcommand, flag or placeholder printed in guidance (brand owns the command prefix) | 5 | 361, 361, 361, 361, 361 |
| `integration_progress_coverage.go` | number | one: exactly-one or more-than-one test | 1 | 38 |
| `integration_progress_coverage.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 12 | 62, 103, 161, 178, 212, 217, 224, 229, 252, 270, 274, 278 |
| `integration_progress_coverage.go` | number | zero: first element or first argument | 2 | 76, 178 |
| `integration_progress_coverage.go` | number | zero: initial length of a made slice or map | 4 | 168, 194, 219, 238 |
| `integration_progress_coverage.go` | number | zero: emptiness, count or sign test | 4 | 174, 185, 211, 216 |
| `integration_progress_coverage.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 5 | 187, 251, 251, 251, 251 |
| `integration_progress_global.go` | number | zero: emptiness, count or sign test | 2 | 20, 47 |
| `integration_progress_global.go` | number | one: last index, len(x)-1 | 2 | 21, 48 |
| `integration_progress_global.go` | number | one: next element, sequence number or attempt, x+1 | 1 | 73 |
| `integration_progress_global.go` | message | composed message, warning, guidance or identifier text | 1 | 77 |
| `integration_progress_global.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 81, 104 |
| `integration_progress_global.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 5 | 82, 105, 109, 113, 116 |
| `integration_progress_lineage.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 6 | 30, 34, 125, 141, 167, 171 |
| `integration_progress_lineage.go` | number | zero: emptiness, count or sign test | 6 | 50, 86, 97, 139, 179, 204 |
| `integration_progress_lineage.go` | number | zero: initial length of a made slice or map | 4 | 79, 119, 132, 163 |
| `integration_progress_lineage.go` | number | zero: first element or first argument | 1 | 205 |
| `integration_progress_lineage.go` | number | one: drop-first slice | 1 | 206 |
| `integration_reconcile.go` | message | log line (free surface, goal-frozen) | 1 | 68 |
| `integration_reconcile.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 27 | 83, 87, 98, 102, 124, 174, 178, 212, 284, 312, 317, 324, 327, 400, 423, 427, 455, 484, 488, 493, 518, 533, 540, 546, 550, 559, 616 |
| `integration_reconcile.go` | number | one: first attempt or generation of a counting loop | 1 | 95 |
| `integration_reconcile.go` | number | zero: emptiness, count or sign test | 6 | 197, 202, 203, 290, 323, 539 |
| `integration_reconcile.go` | number | zero: initial length of a made slice or map | 6 | 272, 470, 479, 480, 498, 520 |
| `integration_reconcile.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 7 | 308, 312, 317, 324, 327, 545, 616 |
| `integration_reconcile.go` | number | one: next element, sequence number or attempt, x+1 | 1 | 520 |
| `integration_reconcile.go` | number | zero: first element or first argument | 1 | 542 |
| `integration_reconcile.go` | number | one: previous element or generation, x-1 | 3 | 557, 558, 559 |
| `integration_reconcile.go` | other literal | colon replaced by a dash when an analysis key becomes a task ID | 1 | 606 |
| `integration_reconcile.go` | other literal | dash that replaces the key colon in an analysis task ID | 1 | 606 |
| `integration_reconcile.go` | message | composed message, warning, guidance or identifier text | 4 | 622, 624, 636, 638 |
| `integration_reconcile.go` | message | guidance or done-when text (frozen wording) | 2 | 629, 631 |
| `merge_admission.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 63 |
| `merge_admission.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 4 | 122, 154, 158, 161 |
| `merge_admission.go` | message | log line (free surface, goal-frozen) | 2 | 163, 169 |
| `merge_artifact_guard.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 3 | 76, 87, 88 |
| `merge_failure.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 15 | 45, 58, 113, 157, 160, 187, 247, 247, 248, 275, 275, 276, 286, 286, 287 |
| `merge_failure.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 2 | 94, 226 |
| `merge_failure.go` | number | zero: emptiness, count or sign test | 5 | 96, 117, 175, 194, 200 |
| `merge_failure.go` | number | zero: first element or first argument | 1 | 97 |
| `merge_failure.go` | persisted key | persisted history `Extra` key of the integration-failure diagnostic (single writer; read by `internal/commands` through its own literal) | 1 | 120 |
| `merge_failure.go` | persisted key | persisted map key (integration-failure diagnostic or recovery history `Extra`); different maps, see table 4 | 1 | 153 |
| `merge_failure.go` | persisted key | persisted integration-failure diagnostic key | 7 | 154, 162, 163, 165, 166, 169, 188 |
| `merge_failure.go` | persisted key | persisted integration-failure diagnostic key (also an operational-error detail key in A3-A5 files; owner gap, table 5) | 1 | 155 |
| `merge_failure.go` | persisted key | persisted integration-failure diagnostic key (a different map from the discovery key, table 4) | 1 | 158 |
| `merge_failure.go` | number | one: last index, len(x)-1 | 1 | 201 |
| `merge_failure.go` | message | guidance or done-when text (frozen wording) | 5 | 209, 211, 213, 215, 217 |
| `merge_failure.go` | message | composed message, warning, guidance or identifier text | 1 | 246 |
| `merge_finalize.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 34, 44, 103 |
| `merge_finalize.go` | number | one: exactly-one or more-than-one test | 1 | 36 |
| `merge_finalize.go` | message | composed message, warning, guidance or identifier text | 9 | 51, 158, 167, 175, 179, 186, 192, 210, 213 |
| `merge_finalize.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 2 | 72, 93 |
| `merge_finalize.go` | persisted key | persisted MERGED history `Extra` key (mirrors the `tests_ran` JSON field of the result) | 1 | 126 |
| `merge_finalize.go` | number | zero: emptiness, count or sign test | 1 | 127 |
| `merge_finalize.go` | number | zero: first element or first argument | 2 | 127, 128 |
| `merge_finalize.go` | other literal | list separator in a composed message | 1 | 159 |
| `merge_forward.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 4 | 44, 49, 68, 92 |
| `merge_forward.go` | Git argv | Git revision `HEAD` passed as the restore ref of the rollback helper | 1 | 64 |
| `merge_forward.go` | message | composed message, warning, guidance or identifier text | 1 | 111 |
| `merge_forward.go` | Git argv | Git revision `HEAD` recorded as the ref to restore the main index from | 1 | 115 |
| `merge_preconditions.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 4 | 19, 38, 61, 85 |
| `merge_preconditions.go` | message | composed message, warning, guidance or identifier text | 1 | 24 |
| `merge_preconditions.go` | message | precondition or refusal reason text (classified messages are frozen) | 1 | 28 |
| `merge_preconditions.go` | message | log line (free surface, goal-frozen) | 1 | 56 |
| `merge_publication.go` | number | zero: initial or reset value | 1 | 35 |
| `merge_publication.go` | number | zero: emptiness, count or sign test | 1 | 36 |
| `merge_publication.go` | message | log line (free surface, goal-frozen) | 1 | 37 |
| `merge_publication.go` | number | one: next element, sequence number or attempt, x+1 | 1 | 37 |
| `merge_publication.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 10 | 47, 56, 60, 67, 76, 89, 97, 123, 131, 132 |
| `merge_publication.go` | message | failure wording of the guarded ref publication (frozen error text) | 2 | 79, 99 |
| `merge_ref_mutation.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 80, 126, 144, 166 |
| `merge_ref_mutation.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 5 | 85, 92, 96, 128, 150 |
| `merge_ref_mutation.go` | message | log line (free surface, goal-frozen) | 2 | 114, 124 |
| `merge_ref_mutation.go` | message | composed message, warning, guidance or identifier text | 1 | 168 |
| `merge_validation.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 2 | 36, 42 |
| `merge_validation.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 50, 53 |
| `merge_validation.go` | message | fragment of a composed message, warning or truncation marker (frozen wording) | 1 | 51 |
| `merge_validation.go` | message | log line (free surface, goal-frozen) | 2 | 98, 103 |
| `merge_validation.go` | message | composed message, warning, guidance or identifier text | 1 | 131 |
| `merged_task_retirement.go` | message | composed message, warning, guidance or identifier text | 3 | 20, 24, 27 |
| `post_worktree_setup.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 7 | 137, 157, 242, 275, 278, 320, 452 |
| `post_worktree_setup.go` | number | zero: emptiness, count or sign test | 6 | 144, 175, 293, 369, 425, 469 |
| `post_worktree_setup.go` | number | one: first attempt or generation of a counting loop | 1 | 155 |
| `post_worktree_setup.go` | number | one: exactly-one or more-than-one test | 1 | 156 |
| `post_worktree_setup.go` | message | log line (free surface, goal-frozen) | 4 | 167, 177, 184, 484 |
| `post_worktree_setup.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 174, 469, 480, 492 |
| `post_worktree_setup.go` | number | zero: no count | 4 | 193, 449, 452, 460 |
| `post_worktree_setup.go` | other literal | `runtime.GOOS` platform identifier compared where the platform cannot support the feature (owner gap, table 5) | 1 | 247 |
| `post_worktree_setup.go` | other literal | dash joining the worktree name and its path digest in the private setup file key | 1 | 322 |
| `post_worktree_setup.go` | message | runner diagnostic line written to the batch log | 2 | 364, 366 |
| `post_worktree_setup.go` | number | one: previous element or generation, x-1 | 1 | 369 |
| `post_worktree_setup.go` | other literal | dot of the private-file temp name `<name>.<random>` | 1 | 381 |
| `post_worktree_setup.go` | other literal | temp-file name pattern suffix of the private-file writer | 1 | 381 |
| `post_worktree_setup.go` | number | zero: first element or first argument | 1 | 424 |
| `post_worktree_setup.go` | number | base 10 / 64-bit width of the persisted outcome sequence | 2 | 424, 424 |
| `post_worktree_setup.go` | number | one: second element | 1 | 429 |
| `post_worktree_setup.go` | number | two: third element of a fixed-arity record (shell command argument or outcome time field) | 1 | 436 |
| `post_worktree_setup.go` | number | one: next element, sequence number or attempt, x+1 | 1 | 454 |
| `post_worktree_setup.go` | persisted key | persisted outcome record layout `<seq> <status> <time>` | 1 | 459 |
| `post_worktree_setup_error.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 52, 68 |
| `post_worktree_setup_error.go` | message | composed message, warning, guidance or identifier text | 2 | 53, 55 |
| `post_worktree_setup_error.go` | message | fragment of a composed message, warning or truncation marker (frozen wording) | 1 | 68 |
| `reconcile_merged.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 5 | 37, 40, 43, 46, 137 |
| `reconcile_merged.go` | message | precondition or refusal reason text (classified messages are frozen) | 5 | 38, 41, 44, 47, 131 |
| `reconcile_merged.go` | message | composed message, warning, guidance or identifier text | 4 | 56, 70, 79, 95 |
| `reconcile_merged.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 2 | 61, 84 |
| `reconcile_merged.go` | message | history reason text of an external-merge reconciliation | 1 | 130 |
| `reconcile_merged.go` | persisted key | persisted MERGED history `Extra` key of an external-merge reconciliation | 1 | 135 |
| `reconcile_merged.go` | persisted key | persisted history `Extra` key read by `internal/commands/integration_failure_diagnostics.go` through its own literal (later-area adoption, table 7) | 1 | 138 |
| `recover_integration.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 48, 48, 256 |
| `recover_integration.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 24 | 49, 74, 100, 120, 144, 163, 167, 171, 175, 179, 182, 186, 191, 201, 205, 209, 216, 228, 235, 239, 242, 246, 250, 257 |
| `recover_integration.go` | persisted key | persisted map key (integration-failure diagnostic or recovery history `Extra`); different maps, see table 4 | 1 | 134 |
| `recover_integration.go` | persisted key | persisted recovery history `Extra` key (mirrors `models.IntegrationRecovery.ReportCommit`) | 1 | 134 |
| `recover_integration.go` | persisted key | persisted recovery history `Extra` key (mirrors `models.IntegrationRecovery.PreservationRef`) | 1 | 134 |
| `recover_integration.go` | message | composed message, warning, guidance or identifier text | 2 | 147, 150 |
| `recover_integration.go` | other literal | ASCII zero digit repeated to the commit width: the all-zero object ID | 1 | 158 |
| `recover_integration.go` | number | zero: emptiness, count or sign test | 8 | 166, 166, 166, 174, 174, 174, 181, 227 |
| `recover_integration.go` | number | zero: initial or reset value | 1 | 194 |
| `recover_integration.go` | number | one: exactly-one or more-than-one test | 1 | 208 |
| `recover_integration.go` | other literal | slash joining the worktrees directory name and the task ID, the deterministic worktree path `.worktrees/{taskID}` | 1 | 234 |
| `run_live.go` | number | zero: emptiness, count or sign test | 4 | 60, 64, 84, 106 |
| `run_live.go` | number | zero: first element or first argument | 3 | 60, 115, 145 |
| `run_live.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 5 | 60, 87, 97, 161, 161 |
| `run_live.go` | message | precondition or refusal reason text (classified messages are frozen) | 2 | 61, 124 |
| `run_live.go` | message | composed message, warning, guidance or identifier text | 5 | 68, 81, 85, 88, 127 |
| `run_live.go` | number | one: drop-first slice | 1 | 115 |
| `run_live.go` | message | fragment of a composed message, warning or truncation marker (frozen wording) | 1 | 135 |
| `run_live.go` | other literal | word separator when comparing a command line | 3 | 144, 153, 155 |
| `run_live.go` | number | one: second element | 1 | 145 |
| `run_live.go` | number | two: third element of a fixed-arity record (shell command argument or outcome time field) | 1 | 146 |
| `scip_indexing.go` | F9 warning prefix | frozen warning prefix of an indexing refresh (D-5-excluded function; classified, not edited) | 1 | 23 |
| `scip_indexing.go` | number | zero: initial length of a made slice or map | 1 | 26 |
| `scip_indexing.go` | message | composed message, warning, guidance or identifier text | 1 | 28 |
| `semble_indexing.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 5 | 40, 61, 70, 169, 178 |
| `semble_indexing.go` | message | composed message, warning, guidance or identifier text | 6 | 49, 56, 65, 68, 181, 184 |
| `semble_indexing.go` | number | zero: emptiness, count or sign test | 2 | 58, 111 |
| `semble_indexing.go` | other literal | leading slash anchoring the generated ignore entry at the repository root | 1 | 64 |
| `semble_indexing.go` | Git argv | Git subcommand, option or revision argument (external CLI vocabulary) | 2 | 74, 74 |
| `semble_indexing.go` | number | one: last index, len(x)-1 | 1 | 111 |
| `semble_indexing.go` | number | zero: initial length of a made slice or map | 2 | 132, 167 |
| `semble_indexing.go` | other literal | line separator of captured text | 1 | 168 |
| `semble_indexing.go` | other literal | list separator in a composed message | 1 | 185 |
| `stacklit_indexing.go` | F9 warning prefix | frozen warning prefix of an indexing refresh (D-5-excluded function; classified, not edited) | 2 | 16, 24 |
| `stacklit_indexing.go` | number | zero: initial length of a made slice or map | 1 | 22 |
| `wt_create.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 81 |
| `wt_create.go` | message | precondition or refusal reason text (classified messages are frozen) | 1 | 82 |
| `wt_create.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 3 | 108, 124, 178 |
| `wt_create.go` | message | composed message, warning, guidance or identifier text | 1 | 151 |
| `wt_create.go` | message | fragment of a composed message, warning or truncation marker (frozen wording) | 19 | 203, 222, 241, 245, 258, 262, 267, 273, 277, 286, 292, 294, 385, 391, 398, 404, 409, 419, 424 |
| `wt_create.go` | number | zero: emptiness, count or sign test | 2 | 293, 331 |
| `wt_create.go` | other literal | list separator in a composed message | 1 | 294 |
| `wt_create.go` | Git argv | Git subcommand, option or revision argument (external CLI vocabulary) | 3 | 355, 355, 355 |
| `wt_delete.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 26, 97 |
| `wt_delete.go` | message | precondition or refusal reason text (classified messages are frozen) | 1 | 27 |
| `wt_delete.go` | message | composed message, warning, guidance or identifier text | 4 | 43, 47, 65, 69 |
| `wt_delete.go` | message | error text (classified messages are frozen: `--json` classifies by substring) | 1 | 83 |
| `wt_merge.go` | message | composed message, warning, guidance or identifier text | 3 | 81, 83, 86 |
| `wt_merge.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 157, 160, 287 |
| `wt_merge.go` | message | precondition or refusal reason text (classified messages are frozen) | 2 | 158, 161 |

### 3b. Pre-existing named constants in A6 files

| Constant | Type | Value | Declared at |
|---|---|---|---|
| `deferredValidationOperation` | `untyped` | `"deferred-validation"` | `deferred_validation.go:24` |
| `deferredValidationMaxRunsPerCall` | `untyped` | `3` | `deferred_validation.go:25` |
| `deferredValidationGitTimeout` | `untyped` | `5*time.Minute` | `deferred_validation.go:26` |
| `deferredValidationDirName` | `untyped` | `"deferred-validation"` | `deferred_validation.go:27` |
| `deferredValidationWaitDelay` | `untyped` | `5*time.Second` | `deferred_validation.go:30` |
| `deferredValidationAlertCategory` | `untyped` | `"DEFERRED VALIDATION FAILED"` | `deferred_validation.go:32` |
| `deferredValidationUnattributedOwner` | `untyped` | `"unattributed"` | `deferred_validation.go:35` |
| `deferredValidationTokenBytes` | `untyped` | `16` | `deferred_validation.go:37` |
| `deferredValidationRunnerLockName` | `untyped` | `"runner"` | `deferred_validation.go:53` |
| `lockLabelDeferredValidationRun` | `untyped` | `"deferred-validation-run"` | `deferred_validation.go:54` |
| `lockLabelDeferredValidationProbe` | `untyped` | `"deferred-validation-probe"` | `deferred_validation.go:55` |
| `lockLabelDeferredValidationSettle` | `untyped` | `"deferred-validation-settle"` | `deferred_validation.go:56` |
| `lockLabelDeferredValidationAbort` | `untyped` | `"deferred-validation-abort"` | `deferred_validation.go:57` |
| `deferredValidationRunnerLogName` | `untyped` | `"runner.log"` | `deferred_validation.go:60` |
| `deferredValidationReasonHalted` | `deferredValidationEndReason` | `"halted"` | `deferred_validation.go:127` |
| `deferredValidationReasonRunnerDied` | `deferredValidationEndReason` | `"runner_died"` | `deferred_validation.go:129` |
| `deferredValidationReasonRunnerSettled` | `deferredValidationEndReason` | `"runner_settled_by_operator"` | `deferred_validation.go:132` |
| `deferredValidationReasonUnstoppable` | `deferredValidationEndReason` | `"descendants_unstoppable"` | `deferred_validation.go:135` |
| `deferredValidationReasonAdmissionSlotError` | `deferredValidationEndReason` | `"admission_slot_error"` | `deferred_validation.go:138` |
| `deferredValidationReasonOperatorAbort` | `deferredValidationEndReason` | `"aborted_by_operator"` | `deferred_validation.go:141` |
| `deferredValidationOutcomeRan` | `deferredValidationRunOutcome` | `"ran"` | `deferred_validation.go:153` |
| `deferredValidationOutcomeSkipped` | `deferredValidationRunOutcome` | `"skipped"` | `deferred_validation.go:154` |
| `deferredValidationSkipRunnerActive` | `deferredValidationSkipReason` | `"runner_active"` | `deferred_validation.go:156` |
| `deferredValidationSkipHalted` | `deferredValidationSkipReason` | `"halted"` | `deferred_validation.go:157` |
| `deferredValidationSkipCovered` | `deferredValidationSkipReason` | `"covered"` | `deferred_validation.go:158` |
| `deferredValidationSkipFailedAtHead` | `deferredValidationSkipReason` | `"failed_at_head"` | `deferred_validation.go:159` |
| `deferredValidationSkipAborted` | `deferredValidationSkipReason` | `"aborted_at_head"` | `deferred_validation.go:160` |
| `deferredValidationSetupAttempts` | `untyped` | `2` | `deferred_validation_lanes.go:219` |
| `deferredStateTextMarker` | `untyped` | `"\n[... %d bytes omitted ...]\n"` | `deferred_validation_lanes.go:365` |
| `deferredStateTranscriptOmitted` | `untyped` | `"output omitted from state: it resembles a provider transcript"` | `deferred_validation_lanes.go:369` |
| `rulingActionRule` | `deferredValidationRulingAction` | `"rule"` | `deferred_validation_ruling.go:37` |
| `rulingActionUnrule` | `deferredValidationRulingAction` | `"unrule"` | `deferred_validation_ruling.go:38` |
| `rulingActionRetire` | `deferredValidationRulingAction` | `"retire"` | `deferred_validation_ruling.go:39` |
| `rulingActionSettle` | `deferredValidationRulingAction` | `"settle"` | `deferred_validation_ruling.go:40` |
| `rulingActionAbort` | `deferredValidationRulingAction` | `"abort"` | `deferred_validation_ruling.go:41` |
| `rulingOutcomeUnchanged` | `deferredValidationRulingOutcome` | `"unchanged"` | `deferred_validation_ruling.go:43` |
| `rulingOutcomeSet` | `deferredValidationRulingOutcome` | `"set"` | `deferred_validation_ruling.go:44` |
| `rulingOutcomeRemoved` | `deferredValidationRulingOutcome` | `"removed"` | `deferred_validation_ruling.go:45` |
| `rulingOutcomeRetired` | `deferredValidationRulingOutcome` | `"retired"` | `deferred_validation_ruling.go:46` |
| `rulingOutcomeSettled` | `deferredValidationRulingOutcome` | `"settled"` | `deferred_validation_ruling.go:47` |
| `rulingOutcomeRequested` | `deferredValidationRulingOutcome` | `"requested"` | `deferred_validation_ruling.go:48` |
| `integrationBranchRefPrefix` | `untyped` | `"refs/heads/"` | `integration_head.go:11` |
| `integrationLockForward` | `untyped` | `"forward "` | `integration_mutation_lock.go:21` |
| `integrationLockRollback` | `untyped` | `"rollback "` | `integration_mutation_lock.go:22` |
| `integrationLockRestore` | `untyped` | `"restore "` | `integration_mutation_lock.go:23` |
| `integrationProgressWaitingPlanning` | `integrationProgressCode` | `"planning_unsettled"` | `integration_progress.go:20` |
| `integrationProgressWaitingSliceCoverage` | `integrationProgressCode` | `"slice_coverage_pending"` | `integration_progress.go:21` |
| `integrationProgressWaitingCoding` | `integrationProgressCode` | `"coding_work_pending"` | `integration_progress.go:22` |
| `integrationProgressWaitingRepairs` | `integrationProgressCode` | `"integration_repairs_pending"` | `integration_progress.go:23` |
| `integrationProgressWaitingGlobalAnalysis` | `integrationProgressCode` | `"global_analysis_pending"` | `integration_progress.go:24` |
| `integrationProgressWaitingClosure` | `integrationProgressCode` | `"closure_projection_pending"` | `integration_progress.go:25` |
| `integrationProgressBlockedRepair` | `integrationProgressCode` | `"integration_repair_blocked"` | `integration_progress.go:27` |
| `integrationProgressBlockedAnalysis` | `integrationProgressCode` | `"integration_analysis_blocked"` | `integration_progress.go:28` |
| `integrationProgressBlockedExhausted` | `integrationProgressCode` | `"global_generations_exhausted"` | `integration_progress.go:29` |
| `integrationProgressBlockedCapability` | `integrationProgressCode` | `pipeline.SlicedIntegrationUpgradeRequired` | `integration_progress.go:30` |
| `sliceAnalysisKeyPrefix` | `untyped` | `"slice:"` | `integration_progress.go:585` |
| `globalAnalysisKeyPrefix` | `untyped` | `"global:"` | `integration_progress.go:586` |
| `sliceIntegrationRolePair` | `untyped` | `"slice-integration-pair"` | `integration_reconcile.go:21` |
| `globalIntegrationRolePair` | `untyped` | `"integration-pair"` | `integration_reconcile.go:22` |
| `codingRolePair` | `untyped` | `baseCommitSnapshotRolePair` | `integration_reconcile.go:23` |
| `codePlanningRolePair` | `untyped` | `legacyPlanningRolePair` | `integration_reconcile.go:24` |
| `maxIntegrationReconcileAttempts` | `untyped` | `3` | `integration_reconcile.go:26` |
| `integrationReconcileLockLabel` | `untyped` | `"reconcile integration analyses"` | `integration_reconcile.go:34` |
| `integrationAnalysisTaskIDPrefix` | `untyped` | `"integration-"` | `integration_reconcile.go:37` |
| `maxIntegrationDiagnosticOutputExcerptBytes` | `untyped` | `4096` | `merge_failure.go:19` |
| `indexRefreshTriggerMerge` | `untyped` | `"merge"` | `merge_finalize.go:16` |
| `maxMergeRetries` | `untyped` | `3` | `merge_publication.go:12` |
| `mergeCommitMessageFormat` | `untyped` | `"Merge %s (task/%s)"` | `merge_publication.go:16` |
| `integrationTestWaitDelay` | `untyped` | `5*time.Second` | `merge_validation.go:19` |
| `postWorktreeSetupAttempts` | `untyped` | `2` | `post_worktree_setup.go:28` |
| `postWorktreeSetupOutputTailBytes` | `untyped` | `64<<10` | `post_worktree_setup.go:30` |
| `postWorktreeSetupWaitDelay` | `untyped` | `10*time.Second` | `post_worktree_setup.go:34` |
| `postWorktreeCmdMaxBytes` | `untyped` | `256` | `post_worktree_setup_error.go:14` |
| `recoverIntegrationOperation` | `untyped` | `"recover-integration"` | `recover_integration.go:20` |
| `integrationRecoveryRefPrefix` | `untyped` | `"refs/integration-recovery/"` | `recover_integration.go:23` |
| `prematureGlobalGeneration` | `untyped` | `1` | `recover_integration.go:25` |
| `runLiveMinTimeoutSeconds` | `untyped` | `1` | `run_live.go:22` |
| `runLiveDefaultTimeoutSeconds` | `untyped` | `600` | `run_live.go:23` |
| `runLiveMaxTimeoutSeconds` | `untyped` | `3600` | `run_live.go:24` |
| `runLiveWaitDelay` | `untyped` | `5*time.Second` | `run_live.go:25` |
| `sembleWorktreeIgnoreFile` | `untyped` | `".sembleignore"` | `semble_indexing.go:18` |
| `maxSemblePreparationWarningBytes` | `untyped` | `512` | `semble_indexing.go:19` |
| `maxSembleMissingPatternsListed` | `untyped` | `8` | `semble_indexing.go:20` |
| `claudeConfigDirName` | `untyped` | `".claude"` | `wt_create.go:33` |
| `claudeSettingsFileName` | `untyped` | `"settings.json"` | `wt_create.go:34` |
| `claudeLocalSettingsName` | `untyped` | `"settings.local.json"` | `wt_create.go:35` |
| `claudeHooksDirName` | `untyped` | `"hooks"` | `wt_create.go:36` |
| `preCommitHookFileName` | `untyped` | `"pre-commit"` | `wt_create.go:37` |
| `worktreeDirMode` | `untyped` | `0o755` | `wt_create.go:38` |
| `worktreeHookFileMode` | `untyped` | `0o755` | `wt_create.go:39` |
| `provisionClaudeConfigWarn` | `untyped` | `"provision-claude-config"` | `wt_create.go:40` |
| `copyWorktreeEnvFilesWarn` | `untyped` | `"copy-worktree-env-files"` | `wt_create.go:41` |
| `installPreCommitHookWarn` | `untyped` | `"install-pre-commit-hook"` | `wt_create.go:42` |
| `integrationTestCommand` | `untyped` | `"scripts/integration-test.sh"` | `wt_merge.go:14` |
| `mergeFallbackIntegrationBranch` | `untyped` | `"main"` | `wt_merge.go:19` |
| `IntegrationReasonHEADMismatch` | `IntegrationReason` | `"worktree HEAD mismatch"` | `wt_merge.go:60` |
| `IntegrationReasonMergeConflict` | `IntegrationReason` | `"merge conflict"` | `wt_merge.go:61` |
| `IntegrationReasonReviewBoundaryMismatch` | `IntegrationReason` | `"review boundary mismatch"` | `wt_merge.go:62` |
| `IntegrationReasonTestsFailed` | `IntegrationReason` | `"integration tests failed"` | `wt_merge.go:63` |
| `IntegrationReasonStateInvalid` | `IntegrationReason` | `"post-merge state validation failed"` | `wt_merge.go:64` |
| `integrationOperationWTMerge` | `untyped` | `"wt-merge"` | `wt_merge.go:68` |
| `shortSHAWidth` | `untyped` | `7` | `wt_merge.go:109` |

### 3c. A6-owned typed sets and the tests that pin them

| Type | Constants | Persisted or crossing as | Pinned by |
|---|---|---|---|
| `IntegrationReason` | `IntegrationReason…` | history `Reason`, diagnostics (conversion at the model boundary); exported | TestIntegrationReasonValuesAndDiagnosticType (merge_flow_test.go) |
| `integrationProgressCode` | `integrationProgress…` | `IntegrationProgressReason.Code` (string, read by prompts) by conversion | TestA6TypedSetsKeepWireSpelling, TestA6ProgressCodesKeepWireSpelling |
| `deferredValidationEndReason` | `deferredValidationReason…` | batch and runner `Reason` by conversion | TestA6TypedSetsKeepWireSpelling |
| `deferredValidationRunOutcome` | `deferredValidationOutcome…` | `RunDeferredValidationResult.Outcome` | TestA6TypedSetsKeepWireSpelling |
| `deferredValidationSkipReason` | `deferredValidationSkip…` | `RunDeferredValidationResult.Reason` | TestA6DeferredValidationVocabularyKeepsWireSpelling |
| `deferredValidationRulingAction` | `rulingAction…` | ruling result and audit log | TestA6TypedSetsKeepWireSpelling |
| `deferredValidationRulingOutcome` | `rulingOutcome…` | ruling result and audit log | TestA6TypedSetsKeepWireSpelling |
| `deferredValidationUnattributedReason` | `deferredValidationUnattributed…` | `DeferredValidationCulprit.Reason` by conversion | TestA6DeferredValidationVocabularyKeepsWireSpelling |
| `worktreeSetupOutcomeStatus` | `worktreeSetupOutcome…` | private outcome file `<seq> <status> <time>` | TestA6WorktreeSetupOutcomeStatusKeepsWireSpelling (also the written bytes) |

No A6 type duplicates an A1 or A3 set: events and anomaly types (V6, V7) are the delivered `models.TaskEventName` and `models.AnomalyType`, safe actions and effects are `LifecycleSafeAction` and `LifecycleEffects` of `lifecycle_result.go`, outcomes are `models.Lifecycle*`.
## 4. False friends: same spelling, different meaning

Each pair below was matched by the value scan and deliberately **not** merged: the constants name different things, and a shared constant would couple them.

| Spelling | Meanings | Disposition |
|---|---|---|
| `"deferred-validation"` | `deferredValidationOperation` (the branded command and lock operation) and `deferredValidationDirName` (the runner directory under the project state directory) | Both stay named; neither derives from the other |
| `"halted"` | `deferredValidationReasonHalted` (batch end reason, persisted in the batch) and `deferredValidationSkipHalted` (run skip reason, returned in the JSON result) | Two typed sets keep their own constant |
| `"worktree"` | `deferredLaneWorktreeName` (lane directory), the Git `worktree` subcommand in the lane git calls, and `replacePreservedFieldWorktree` (A4: the replace-task field) | Lane name named; the Git subcommand is argv vocabulary; the A4 field is not adopted |
| `"--force"` | `deferredValidationFlagForce` (deferred-validation run flag in guidance and spawn) and Git's `--force` in `worktree remove` and `checkout` | Flag named; the Git options stay argv vocabulary |
| `"settle"` | `rulingActionSettle` (the ruling action, whose branded subcommand has the same spelling) | Adopted in the five guidance sites (R1); same meaning |
| `"operator"` | `actorOperator` (the actor recorded by operator-only operations) and `models.AcceptanceDeferralOperator` (the origin of an acceptance deferral) | `actorOperator` adopted at the audit line; the models constant names a different attribute |
| `"merge_commit"` | `deferredDiscoveryKeyMergeCommit` (discovery `Extra`) and the integration-failure diagnostic key | Different maps; the diagnostic key stays classified (table 3) |
| `"operation"` | the integration-failure diagnostic key and the recovery history `Extra` key | Different maps; both classified |
| `"clean"` | the Git `clean` subcommand and `models.IntegrationAnalysisVerdictClean` / `IntegrationClosureStatusClean` / `StateCategoryClean` | Git argv vocabulary; not adopted |
| `":"` | a pathspec-magic prefix (`deferredPathspecMagicPrefix`) and the separator the analysis key replaces in a task ID | The first is named; the second is composition text of `integrationAnalysisTaskID` |
| `"status"` | `deferredValidationSubcommandStatus` (branded subcommand) and Git `status --short` | Subcommand named; Git argv vocabulary stays |
| `"ok" / "failed"` | `worktreeSetupOutcomeStatus` (the outcome file status word) and unrelated status words of other sets | Typed with its own constants |
| `"task"` | `notFoundEntityTask` (an `errors.NotFoundError.Entity` name) and task-ID words in messages | Entity adopted at seven sites (R1); messages stay frozen text |

## 5. Owner gaps (R3) and owner findings

| Value | Gap | Disposition |
|---|---|---|
| `"AGENT_ID"`, `"AGENT_GENERATION"`, `"TASK_ID"` suffixes | `brand.EnvName` and `LegacyEnvName` take the suffix as a string and name none | A6 reuses the A5-named `agentIDEnvSuffix` and `agentGenerationEnvSuffix` and names `taskIDEnvSuffix` beside its single use; `brand` is not edited (R3) |
| `"task ID is required"` (and the other `… is required` refusals) | The wording is repeated by 24 `PreconditionError` sites in 23 `ops` files (A3–A6); no shared helper exists | Classified as frozen message text in A6 (table 3). Owner: A3 helpers; not edited here (`--json` classifies by this text) |
| `"windows"` (`runtime.GOOS` comparisons) | Go names no platform constant; `admit.go` (A3) carries the same literal | Two A6 sites classified as platform identifiers; not named locally to avoid a third copy of a shared literal |
| `"tool-results"` directory name | Five production files across `cmd/liza`, `internal/agent` and `ops` rebuild `<project>/<dir>/tool-results`; `paths` exposes no accessor | Classified in A6 (table 3). Owner: `paths`/`toolresult`; the other areas adopt it when they are refactored |
| `"100644"`, `"100755"` | `gitModeRegularFile` and `gitModeExecutableFile` (A3 `runtime_input_admission.go`) exist; `referencecontract/resolve.go` and `statevalidate/candidate_artifact_refs.go` (A1) repeat the literals | A6 adopts the A3 constants. The two A1 literals are outside A6 and untouched |
| `Priority: 1` of a generated task | `models` names no default or top priority; `transition_children.go` (A4) writes the same literal for generated children | A6 names `integrationAnalysisTaskPriority` for its analysis tasks; the A4 literal is outside A6 and untouched |
| `"recovery_hint"` key | Used as an operational-error detail key in A3–A5 files and read by `internal/commands` | The A6 diagnostic key is classified; unification belongs to the area owning the error details |

Prospective A1/A3 symbols: none was undelivered. The A1 vocabulary (`models.TaskEventName`, `AnomalyType`, `InheritMode`, `DiscoverySeverity`, `DiscoveryUrgency`, `TransitionCardinality`, `roles.ClaimType`, `roles.RoleType`) and the A3 lifecycle vocabulary (`LifecycleSafeAction`, `LifecycleEffects`, `models.Lifecycle*`) are present at the coding HEAD, so there is no BLOCKED finding against either area's closure.

## 6. Conformance sites edited outside A6 files

No production file outside the A6 list changes, so no X2 caller edit exists: `git diff --name-only e7cbe3206 97d17dab1` names only `internal/ops/*.go` files, `internal/oracle/**` is untouched, and `internal/prompts`, `internal/commands`, `cmd/liza`, `internal/jsonout` and `internal/agent` are not edited.

White-box tests edited (mechanical `string(...)` conversions where an unexported progress code became typed; what each asserts is unchanged):

| Test file | Edit |
|---|---|
| `deferred_validation_abort_test.go` | comparison of `IntegrationProgressReason.Code` with a deferred-validation progress code converts the typed constant with `string(...)` |
| `deferred_validation_carry_test.go` | comparison of `IntegrationProgressReason.Code` with a deferred-validation progress code converts the typed constant with `string(...)` |
| `deferred_validation_fence_test.go` | comparison of `IntegrationProgressReason.Code` with a deferred-validation progress code converts the typed constant with `string(...)` |
| `deferred_validation_knownred_test.go` | comparison of `IntegrationProgressReason.Code` with a deferred-validation progress code converts the typed constant with `string(...)` |
| `deferred_validation_test.go` | comparison of `IntegrationProgressReason.Code` with a deferred-validation progress code converts the typed constant with `string(...)` |
| `integration_progress_test.go` | comparison of `IntegrationProgressReason.Code` with a deferred-validation progress code converts the typed constant with `string(...)` |
| `k7_a6_vocabulary_test.go` | new: pins every A6 typed set and every name this task adopted or added |

## 7. Later-area adoption sites (X7: adopted when those areas are refactored)

| Site | Spelling | A6 owner constant | Area |
|---|---|---|---|
| `internal/commands/integration_failure_diagnostics.go` (reads `"recovery_hint"` and `"pr_url"`) | diagnostic and history `Extra` keys written by A6 | A6 keeps these keys as classified literals (table 3); A7 adopts a shared constant if A6 later names them | A7 |
| `internal/commands/wt_merge.go`, `internal/jsonout/classify.go` (switch on `ops.IntegrationReason…`) | `IntegrationReason` values | `IntegrationReason…` (exported, unchanged) | A7 (already by conversion, Task 5) |
| `internal/agent/strategy_orchestrator_deferred_validation_test.go`, `internal/agent/workdetection.go` | `ops.IntegrationProgressWaitingDeferredValidation`; `"integration-pair"` | `IntegrationProgressWaitingDeferredValidation` stays a string constant with its value; the role-pair literal is dynamic (F8) | A9 |
| `internal/agent/{tool_result_acp,tool_result_claude,tool_result_codex}.go`, `cmd/liza/cmd_tool_result.go` | `"tool-results"` | none (owner gap, table 5) | A7/A9 |

## 8. Plan K7 families F1–F9

| Family | Reconciliation at the coding HEAD |
|---|---|
| F1 history events, anomaly types (V6, V7) | Delivered (`models.TaskEventName`, `AnomalyType`). The scan found 0 A6 literals equal to any of their 73 values outside a declaration: nothing left to adopt; `models.TaskEvent…` and the anomaly constants are used by name throughout. |
| F2 lifecycle outcomes, safe actions, effect classes | Delivered in `lifecycle_result.go` and `models`. The plan's count ("`requery` ×4, `correct_input` ×3, `continue` ×3, `stop` ×1") described the starting commit; Tasks 2–6 had already adopted all but four sites. This task adopts the last four raw spellings: `merge_failure.go` (`requery`), `merge_preconditions.go` (`stop`/`none`, `requery`/`none`) and `wt_merge.go` (`correct_input`/`none`). No second type exists. |
| F3 `IntegrationReason…` (5 constants) | Typed by Task 5 (`IntegrationReason`; `IntegrationFailedError.Reason` takes it); pinned by `TestIntegrationReasonValuesAndDiagnosticType`. A7 callers convert (X2, Task 5). |
| F4 progress wait/block reasons (15 names) | `integrationProgressCode` holds the evaluator's ten codes (Task 3); this task moves the five deferred-validation ones into it. The exported `IntegrationProgressWaitingDeferredValidation` stays a string constant with its value (it is `string(integrationProgressWaitingDeferred)`), so `internal/agent` compares unchanged. `IntegrationProgressDecision`, `IntegrationProgressReason`, `ReconcileIntegrationAnalysesResult` and `EvaluateLiveIntegrationProgress` are unchanged: no declaration line of them is in the diff. |
| F5 batch end reasons; ruling actions and outcomes | Typed by Task 2 (`deferredValidationEndReason`, `…RulingAction`, `…RulingOutcome`, `…RunOutcome`, `…SkipReason`). This task adds the three no-batch skip reasons and the unattributed-culprit reasons to typed sets, and `worktreeSetupOutcomeStatus`; persisted batch fields keep their string type. |
| F6 operation names, lock labels, refresh trigger, alert category, ref prefixes, commit message format, default branch literal, short-SHA width | Named by Tasks 2–6 for the merge and integration concerns; this task names the remaining ones (table 2): mutation-lock name and verify label, worktree create/setup operations, setup file extensions and lock purpose, hooks directory suffix, env-file patterns, lane worktree names, pathspec magic, discovery keys and ID/once-key formats, branded subcommands and flag. |
| F7 timing, retry and size values | Named by Tasks 2–6 (`deferredValidation…`, `runLive…`, `postWorktreeSetup…`, `maxIntegrationReconcileAttempts`, `maxSemble…`, `DefaultIntegrationTestTimeout` and the other variables kept replaceable). This task adopts `acceptanceExitUnknown` (A5) for the twelve `-1` exit-status sentinels of the deferred-validation executor, run-live and the semble Git exit helper, and names the status window, file modes, digest width, outcome record arity, shell-wrapper arity, the cohort minimum, the analysis task priority, the two quiet-Git exit statuses and the two setup defaults (`2s` retry delay, `1m` lock slack) that seeded atomic test knobs. Test-replaceable variables stay variables. |
| F8 role and tool names, configured commands | Dynamic or already named: the built-in role pairs (`sliceIntegrationRolePair`, `globalIntegrationRolePair`, `codingRolePair`, `codePlanningRolePair`) are named and `integration_progress_lineage.go` now uses `codePlanningRolePair` instead of the literal; `post_worktree_cmd`, the deferred commands and the integration test script path (`integrationTestCommand`) are configured values (ADR-0045, K7). Table 9. |
| F9 warning prefixes of the indexing files | Classified literal text (table 3, family F9): `scip-search:`, `stacklit:` and `functional-clusters:` prefixes are frozen warning wording; the indexing functions are D-5-excluded and are not edited. `sembleWarningEllipsis` names the one truncation marker of the semble warning bound (the function composes it by call-site arithmetic only). |

Proof of "no behaviour change": `git diff --stat e7cbe3206 97d17dab1 -- internal/oracle` is empty (no golden, fixture or oracle file edited); the added lines contain no Git or `exec` call (the one changed `exec.Command` line replaces the `"run"` literal of the existing detached runner spawn, which starts under the work-admission lock and not in a `Modify`); the exported surface differs by the one retyped initialiser above; every named value is pinned against the literal it replaced by `k7_a6_vocabulary_test.go`.

## 9. Dynamic identities (F8)

| Identity | Where | Why it stays dynamic or named |
|---|---|---|
| built-in role pairs `slice-integration-pair`, `integration-pair`, `coding-pair`, `code-planning-pair` | `integration_reconcile.go`, `integration_progress_lineage.go` | Role pairs are configured through the pipeline (ADR-0045); the built-in names are named once here and aliased from A4 (`legacyPlanningRolePair`, `baseCommitSnapshotRolePair`) |
| `post_worktree_cmd` | `post_worktree_setup.go`, `merge_finalize.go`, `wt_create.go` | User-configured command (G1.1); never a constant |
| deferred commands and their rulings | `deferred_validation*.go` | User-configured commands and operator text; masked and bounded, never closed |
| `scripts/integration-test.sh` | `wt_merge.go` (`integrationTestCommand`) | The conventional default path, named once; the project may replace it |
| integration branch name | `wt_merge.go` (`mergeFallbackIntegrationBranch`), `integration_head.go` | Read from `config.IntegrationBranch`; the named fallback applies only when the configuration is empty |
| agent IDs, task IDs, plan IDs, worktree paths, commits | all files | Runtime identities, not vocabulary |
