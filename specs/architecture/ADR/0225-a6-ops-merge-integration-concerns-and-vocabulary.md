# 225 - A6 Ops Merge and Integration Concerns and Vocabulary Ownership

## Status

ACCEPTED.

## Context

The architecture-consolidation run splits the A6 area of `internal/ops` (merge
and integration) along concerns until no A6 function or file exceeds the size
guard, and moves its finite control-flow vocabulary and magic values to named
owners without a new configuration field
([goal K7](../../goals/20261023-architecture-consolidation.md#structural-constraints)).
At the starting commit `96c9ad69a` the area was 19 files and 7,522 lines with
nine size offenders (3 files, 6 functions). The worst were `mergeWorktree` (554
lines, one function holding admission, preconditions, preparation, forward
publication, failure classification, validation, rollback, the final
transaction and post-commit effects), `wt_merge.go` (1,403 lines),
`deferred_validation.go` (1,820 lines) and `integration_progress.go` (1,138
lines). The decision is indexed in the [ADR README](README.md).

[ADR-0021](0021-ops-service-layer-for-mutations.md) makes `internal/ops` the
service layer for state mutations and
[ADR-0130](0130-generation-fenced-agent-authority.md) fixes the generation
fence the merge and integration operations rely on. This ADR keeps both: it
records how A6 is laid out inside that boundary, which exported surface later
areas keep using, where its vocabulary lives, and the evidence that the area is
closable. [ADR-0222](0222-a3-ops-kernel-concerns-and-vocabulary.md) settled the
kernel layout and the A3 types A6 consumes;
[ADR-0220](0220-a1-typed-vocabulary.md) the typed-vocabulary rules (defined
string types, no codec methods, conversion at boxed-string boundaries, unknown
values unchanged);
[ADR-0223](0223-a4-ops-planning-concerns-and-vocabulary.md) and
[ADR-0224](0224-a5-ops-review-acceptance-concerns-and-vocabulary.md) are the A4
and A5 precedents. All of them apply here unchanged.

It records the layout approved in
[Scope 1 of the A6 architecture](../../arch-plan/20261023-architecture-consolidation/20261023-235449-arm-1-ar-15.md#scope-1-a5-boundary-and-a6-merge-and-integration-consolidation)
and delivered by Tasks 2–7 of its code plan. The occurrence-by-occurrence
vocabulary evidence is the
[A6 K7 inventory](../consolidation-run/a6-k7-inventory.md). The measurements
below were taken at coding HEAD `f8d20669d` (clean worktree, no commit missing
from the integration branch) against the starting commit.

## Decision

### Concern layout

1. **Merge and integration stay inside `internal/ops`.** A6 adds no package,
   dependency, framework, configuration field, layer or allowlist entry, S2
   exception or crossing, and keeps every exported entry operation, input,
   result and error type. Concerns are separated by private, concern-named
   files and functions. `ops` stays application-layer. The A6 guard data
   (layers, allowlist, ratchet, S2) is empty; `TestAreaA6RatchetIsEmpty` holds
   the boundary condition. The nine starting keys are gone: the longest A6 file
   is `integration_progress.go` at 641 lines (limit 800) and the longest
   function is `reconcileIntegrationAnalyses` at 100 lines (limit 100, not
   over; it measured 100 at the start and did not grow), over 37 files and
   8,407 lines.

2. **Concern files.** Entry files keep their original name and the exported
   entry; the private files take the separated phases (all `internal/ops/`).

   | Concern | Files | Separated responsibility |
   |---|---|---|
   | K-DV deferred and live validation | `deferred_validation.go`, `deferred_validation_schedule.go`, `deferred_validation_runner.go`, `deferred_validation_lanes.go`, `deferred_validation_attribution.go`, `deferred_validation_ruling.go`, `run_live.go` | Typed vocabulary and shared names, then merge-time deferral recording and the due, gate and baseline queries with the runner spawn, then the runner lifecycle (claim, halt and abort watches, publish), then lane worktrees and bounded probe execution, then failure attribution (carry-forward, known-red, bisect as a search over baseline and step helpers). Operator rulings and live command execution keep their own files |
   | K-INT integration progress and analysis | `integration_progress.go`, `integration_progress_coverage.go`, `integration_progress_global.go`, `integration_progress_lineage.go`, `integration_reconcile.go`, `integration_global_surface.go`, `recover_integration.go` | Pure progress evaluation split into the evaluator and its decision types, slice coverage and attestations, the global stage, and lineage and repair resolution, then reconciliation projection, candidate validation and the guarded transaction; global surface projection; premature-integration recovery |
   | K-MRG merge execution | `wt_merge.go`, `merge_admission.go`, `merge_preconditions.go`, `merge_forward.go`, `merge_publication.go`, `merge_artifact_guard.go`, `merge_ref_mutation.go`, `merge_validation.go`, `merge_failure.go`, `merge_finalize.go`, `merged_task_retirement.go`, `reconcile_merged.go`, `integration_mutation_lock.go`, `integration_head.go` | `wt_merge.go` keeps the exported entries, the result and error types and `mergeWorktree`, an orchestration of one private `mergeInvocation` carrier: admission (request, receipt, interrupted-preparation resume, replay), then preconditions and the preparation transaction, then forward publication under completion linearization and the mutation lock, then attribution, artifact validation, integration tests with own-merge rollback, then the final MERGED transaction and post-commit effects. Publication (guarded CAS, artifact guard, mutation receipt, rollback) and failure recording are separate files; external-merge reconciliation reuses the shared retirement helper |
   | K-WT worktree provisioning | `wt_create.go`, `worktree_provisioning.go`, `wt_delete.go`, `post_worktree_setup.go`, `post_worktree_setup_error.go`, `semble_indexing.go`, `scip_indexing.go`, `stacklit_indexing.go`, `functional_clusters_indexing.go` | `createWorktree` is an orchestration over the precondition read, Git creation outside the lock, the base-commit transaction (worktree removed on write failure) and the shared provisioning sequence; deletion; post-worktree command execution; the index refreshes |

   Eighteen of the 37 files are new private concern files: four created for
   K-DV (`f1c5afd0c`), three for K-INT (`bce406207`), three for the merge
   publication half (`bafe7b0d6`), seven for the merge flow (`18246b22d`) and
   one for K-WT (`e7cbe3206`). The set is the files those five commits added,
   not a name glob, and it reconciles with the inventory's count of 19 + 18.
   Each operation keeps its phase order (preconditions from a snapshot, Git and
   command work outside the state lock, one final transaction that revalidates,
   then projection and cleanup), its error types, first refusal and effect
   order. A helper extracted from a transaction receives explicit state and
   context and never opens a blackboard, task, integration, completion or file
   lock. Lock order stays project lifecycle shared → task review → completion
   linearization → integration mutation; no blackboard write happens while the
   mutation lock is held. Generation fencing, request identity and receipt
   replay stay in the A3 kernel.

   Largest entry functions after the split (declaration line to closing
   brace): `mergeWorktree` 56 (was 554), `createWorktree` 61 (was 120),
   `reconcileMergedWithOptionalAuthority` 70 (was 119),
   `integrationProgressEvaluator.evaluateCoverage` 37 (was 112),
   `deferredValidationRunner.bisect` 27 (was 111) and `performCASMerge` 15
   (was 110).

3. **Preserved findable sites.** The merge's compare-and-swap with its bounded
   retries (`performCASMerge` over `attemptCASMerge`, `maxMergeRetries`) and its
   pre-update artifact guard stay in `merge_publication.go` and
   `merge_artifact_guard.go`; rollback of only the merge's own commit stays in
   `merge_ref_mutation.go`; the final MERGED transaction is `commitMerged` in
   `merge_finalize.go`; the two C5 timestamps stay in one function, below.

### The three K6 helpers

4. **Only three shared helpers were admitted**, each naming the duplication it
   absorbs (K6):

   | Helper | File | Duplication absorbed |
   |---|---|---|
   | `publishGuardedRef` | `merge_publication.go` | The pre-update guard, `update-ref` compare-and-swap and lost-race retry decision, repeated in `performCASMerge`'s fast-forward and true-merge branches. Both branches now call it with the candidate and a branch-specific failure prefix |
   | `retireMergedTask` | `merged_task_retirement.go` | The worktree directory removal, task-branch deletion and predecessor-branch cleanup, repeated with identical warning text in `mergeWorktree` and `reconcileMergedWithOptionalAuthority`. Sprint metrics stay with the callers because their authority differs |
   | `provisionWorktree` | `worktree_provisioning.go` | The eight-step provisioning sequence repeated in `createWorktree`'s reuse and fresh paths: Claude Code config, per-worktree pre-commit hook, env files (when authorised), the fail-closed post-worktree command, the Semble ignore payload, then the SCIP, Stacklit and functional-clusters index refreshes. Warning order and the fail-closed error are unchanged |

   Tests pin each: `TestPublishGuardedRef` and
   `TestPerformCASMergeExhaustsBoundedRetries`,
   `TestRetireMergedTaskRemovesWorktreeBranchAndPredecessor` and
   `TestRetireMergedTaskKeepsWarningTextAndOrder`,
   `TestProvisionWorktreeRunsStepsInOrder` and
   `TestProvisionWorktreePostCmdFailureEndsSequence`.

### Rejected alternatives

5. **Rejected: an `ops/integration` sub-package.** A6 code calls A3 kernel
   helpers (`modifyLifecycleState`, `lifecycleMutation`, `RequireWorkAdmitted`,
   the lifecycle request and receipt functions), A4 helpers
   (`cleanupPredecessorBranches`, `UpdateSprintMetrics`,
   `RecordObligationContentDrift`, the archive operations) and A5 helpers
   (`requireReconciledVerdicts`, `validateIntegrationLifecycleCandidate`), and
   A3–A5 call A6's integration locks. A sub-package would export those helpers
   or create import cycles, for no K3 crossing to resolve (SI-5 assigns A6
   none) and no size need that private files do not meet.

6. **Rejected: a generic lifecycle-step framework** over merge, reconcile and
   recover. Their locks, transactions and failure projections differ: the
   merge holds no state lock across Git and retires its own preparation,
   reconcile projects under the completion lock, and recover validates a
   candidate inside its transaction. A shared engine would hide those
   orderings behind callbacks. The real duplications are the three K6 helpers.

7. **Open: `issue-intel-refresh`.** The worktree intelligence refresh keeps
   several owners: worktree creation (A6), task claim, the periodic health
   check and review submission each orchestrate it, with differing ordering,
   warning text and failure tolerance, and unifying it changes a frozen
   surface. A6 only moved its own worktree-creation copy behind
   `provisionWorktree`, in the same order; the issue stays open (D-2). [Worktree Intelligence Refresh Has Multiple Owners](../architectural-issues.md#worktree-intelligence-refresh-has-multiple-owners)
   remains in the registry. So do the issues A5 left open
   (`issue-clean-worktree`, `issue-verdict-diversity`) and the vocabulary issue
   ([Control-Flow Vocabulary Bypasses Domain Ownership](../architectural-issues.md#control-flow-vocabulary-bypasses-domain-ownership)):
   later areas still adopt the A6 names at their sites (inventory table 7).
   A6 issue closures belong to the A7 boundary unit.

### Retained exported surface

8. **The exported A6 surface is unchanged for A7, A8 and A9** (the 67
   declarations of the audit below). It includes `MergeWorktree`,
   `MergeWorktreeWithAuthority` and `MergeWorktreeWithAuthorityAndOptions`,
   `ReconcileMerged` and `ReconcileMergedWithAuthority`, `CreateWorktree`,
   `DeleteWorktree`, `RecoverIntegration`, `ReconcileIntegrationAnalyses`,
   `EvaluateIntegrationProgress`, `EvaluateLiveIntegrationProgress`,
   `UnsettledPreIntegrationPlanningTasks`, `BuildGlobalIntegrationSurface`,
   `ResolveIntegrationHEAD`, `RunDeferredValidation`,
   `StartDeferredValidationIfDue`, `ReadDeferredValidationStatus`, the five
   ruling operations (`RuleDeferredValidation`, `UnruleDeferredValidation`,
   `RetireDeferredValidation`, `SettleDeferredValidation`,
   `AbortDeferredValidation`), `RunLive`, `IntegrationFailedError`,
   `PostWorktreeSetupError`, `RunPostWorktreeCmd`, `ProvisionClaudeConfig`,
   `ProvisionWorktreeEnvFiles`, `InstallWorktreePreCommitHook`,
   `PrepareSembleWorktreeIgnore` and their input and result types. They keep
   their names, signatures and behaviour. The integration lock helpers that
   A3–A5 call are unexported in-package functions
   (`withIntegrationMutationLock`,
   `withEffectiveIntegrationCompletionLinearization`) and keep their names.
   `internal/commands`, `cmd/liza`, `internal/jsonout` and `internal/agent`
   (A7 and A9) needed no edit: the six production commits of A6 touch only
   `internal/ops` and the A6 guard data. `internal/prompts` is not edited.
   `EvaluateLiveIntegrationProgress`, `IntegrationProgressDecision`,
   `IntegrationProgressReason` and `ReconcileIntegrationAnalysesResult` stay
   exported from `ops` with unchanged fields until Scope 19 moves the types
   `prompts` reads.

### Vocabulary ownership

The plan numbers the A6 families F1–F9 and applies the rules of the A3 to A5
inventories: adopt the delivered owner (R1), follow the first owner's
classification (R2), name an owner gap in `ops` without editing the closed
owner (R3), name or type a new A6 value in its concern (R4), classify literal
data or a dynamic identity (R5). The inventory scanned 898 literals in the 37
files (172 inside constant and variable declarations, 726 retained and
classified with a reason) and left none unclassified or deferred.

9. **Adopted owners (F1, F2).** A6 consumes and never redefines these sets: the
   A1 types `models.TaskEventName`, `models.AnomalyType`, `models.InheritMode`,
   `models.DiscoverySeverity`, `models.DiscoveryUrgency`,
   `models.TransitionCardinality` and `roles.ClaimType`/`roles.RoleType`; the
   A3 `LifecycleSafeAction` and `LifecycleEffects` and the `models.Lifecycle*`
   outcomes; and the A3 and A5 named values `gitModeRegularFile`,
   `gitModeExecutableFile`, `actorOperator`, `notFoundEntityTask`,
   `acceptanceExitUnknown`, `agentIDEnvSuffix` and `agentGenerationEnvSuffix`.
   The scan found no A6 literal equal to a history-event or anomaly value
   outside a declaration. The last four raw safe-action spellings were adopted
   in `merge_failure.go`, `merge_preconditions.go` and `wt_merge.go`. A6
   defines no second type for any of them.

10. **A6 owns nine finite sets (F3–F5).** Each is a defined type over `string`
    with one constant per value, declared in its concern, with no codec or
    `IsZero` method. Exported and persisted fields keep `string` (or their
    existing type) and take the typed constant by conversion, so JSON and
    persisted bytes are unchanged.

    | Type | Constants | Carried as | Concern |
    |---|---|---|---|
    | `IntegrationReason` | 5 (`IntegrationReason…`, exported) | `IntegrationFailedError.Reason` is the one exported field that took the type; history `Reason` and diagnostics convert at the model boundary | merge failure |
    | `integrationProgressCode` | 15 | `IntegrationProgressReason.Code` (string, read by `prompts`) by conversion; ten evaluator codes plus five deferred-validation codes | integration progress |
    | `deferredValidationEndReason` | 6 | batch and runner `Reason` by conversion | deferred-validation runner |
    | `deferredValidationRunOutcome` | 2 | `RunDeferredValidationResult.Outcome` | deferred-validation runner |
    | `deferredValidationSkipReason` | 8 | `RunDeferredValidationResult.Reason` | deferred-validation runner |
    | `deferredValidationUnattributedReason` | 5 | `DeferredValidationCulprit.Reason` by conversion | attribution |
    | `deferredValidationRulingAction` | 5 | ruling result and audit log | rulings |
    | `deferredValidationRulingOutcome` | 6 | ruling result and audit log | rulings |
    | `worktreeSetupOutcomeStatus` | 2 | the private outcome file `<seq> <status> <time>` | post-worktree setup |

    `IntegrationProgressWaitingDeferredValidation` stays a string constant with
    its value, now `string(integrationProgressWaitingDeferred)`, so
    `internal/agent` compares unchanged. Batch status stays model-owned. The
    spellings are pinned by `TestIntegrationReasonValuesAndDiagnosticType`,
    `TestA6TypedSetsKeepWireSpelling`, `TestA6ProgressCodesKeepWireSpelling`,
    `TestA6DeferredValidationVocabularyKeepsWireSpelling`,
    `TestA6WorktreeSetupOutcomeStatusKeepsWireSpelling` (which also drives the
    written bytes) and `TestA6AdoptedOwnerValuesKeepTheirSpelling`.

11. **Owner gaps are named in `ops`, not fixed in the owner (R3).** A closed
    package that never named a value keeps its form (X1). A6 names the value at
    the consuming concern and the inventory lists the other sites for their
    area (X7): the `TASK_ID` environment suffix (`brand` takes the suffix as a
    string and names none), the generated analysis task priority, and the
    reuse of the A3 git-mode constants where `referencecontract` and
    `statevalidate` (A1) repeat the literals. Not named, recorded as owner
    findings: the `… is required` precondition wording repeated by 24 sites in
    23 `ops` files (frozen message text, classified by `--json`), the
    `"windows"` platform literal, the `tool-results` directory name that five
    files rebuild (`paths` exposes no accessor), and the `recovery_hint`
    detail key read by `internal/commands`.

12. **Named values, not types (F6, F7).** Operation names, lock labels
    (`forward `, `rollback `, `restore `), the `merge` index-refresh trigger,
    ref prefixes, the merge commit-message format, the fallback integration
    branch, the short-SHA width, timing, retry, size and mode values, discovery
    keys, ID and once-key formats, lane and pathspec names, branded
    subcommands and file names became named constants in their concern with
    unchanged values and bytes: 100 substitution sites and 63 declarations in
    Task 7, 102 constants already named. Test-replaceable timing variables and
    hooks (`startIndexRefresh`, `startDeferredValidationProcess`,
    `DefaultIntegrationTestTimeout`, `statIntegrationScript`,
    `artifactGuardReadState`, `validateIntegrationLifecycleTransition`) stay
    variables with the same names. The twelve `-1` exit-status sentinels adopt
    `acceptanceExitUnknown`.

13. **Configured identities and literal data stay dynamic (F8, F9).** Role
    pairs are configured (ADR-0045): the built-in names are named once
    (`sliceIntegrationRolePair`, `globalIntegrationRolePair`, `codingRolePair`,
    `codePlanningRolePair`) and `integration_progress_lineage.go` uses the
    named pair. `post_worktree_cmd`, deferred commands, the integration test
    script path and the integration branch are configured values (G1.1). The
    `scip-search:`, `stacklit:` and `functional-clusters:` warning prefixes are
    frozen warning wording of the D-5-excluded functions. Same-spelling values
    with another meaning are recorded as false friends (inventory table 4),
    not merged with the typed sets.

14. **AC3 string-boundary rule.** A vocabulary value is converted to `string`
    where it leaves its typed position for a string-kinded consumer: history
    `Extra`, anomaly and error details, log fields, persisted fields, result
    fields and `map[string]any`. Types name decisions that already exist; they
    add no validation, default or branch. The conversion needed outside the A6
    files is two `string(IntegrationReason…)` initialisers in A5's
    `review_boundary.go` and `submission_git_work.go`.

### Gap records (D-5)

15. **Gap coverage was added before the surfaces were touched.** The delivered
    A6 oracle left four frozen paths a refactor task would change without a
    case. The additive files in `internal/oracle/a6/` pin them, each committed
    before the production edit it guards with goldens written by the
    regeneration entry point from the starting tree:

    | Gap | Case | Commit |
    |---|---|---|
    | Bisect-attributed deferred-validation batch and its status | `TestOracleDeferredValidationBisect` (`run_bisect`, `status_bisected`) | `b53bc8c85` |
    | Slice-coverage wait and first global-analysis projection | `TestOracleIntegrationReconcileGaps` (`coverage_waiting`, `global_projection`) | `e5a4ce8fe` |
    | Merge failure when integration tests fail (own-merge rollback) | `TestOracleWtMergeFailureGaps/tests_failed` | `32b38e7f4` |
    | Post-worktree command failure (`PostWorktreeSetupError`) | `TestOracleWtCreatePostWorktreeGaps/post_worktree_failed` | `4b71fae3e` |

    HEAD mismatch and post-merge state validation are not deterministic
    through the CLI determinism layer (short SHAs in their diagnostics; a
    corrupted artifact ref) and map to white-box tests
    (`TestMergeWorktree_HEADMismatch`,
    `TestMergeWorktree_RetainsPostMergeArtifactValidationBackstop`, the
    `TestMergeWorktree_Rejects*BeforeRefUpdate` artifact-guard tests).
    Publication-failure classes map to `merge_publication_test.go` and the
    existing CAS and guard suites for the same reason.

16. **Gaps recorded, not built (D-5 exclusion).** Each excluded path keeps its
    functions unchanged, its white-box tests, and at most a moved call site
    with arguments and warning order unchanged:

    | Excluded path | Why | State at coding HEAD |
    |---|---|---|
    | Post-merge refresh with an installed index script | `pairingindex.StartRefresh` starts a detached coordinator and only waits in a goroutine of the calling process, so a CLI case would leave the coordinator running after it. The premise holds at coding HEAD (`internal/pairingindex/refresh.go`) | `startIndexRefresh` keeps its name and value; only its call moved, from `wt_merge.go` to `runPostCommitEffects` in `merge_finalize.go`, with the `merge` trigger named `indexRefreshTriggerMerge` and the warning order unchanged |
    | Configured-language scip-search and functional-clusters refresh | External indexers the proof environment excludes | `scip_indexing.go`, `stacklit_indexing.go` and `functional_clusters_indexing.go` are byte-identical to the starting commit (`git diff 96c9ad69a HEAD` is empty); their callers now sit in `provisionWorktree` in the same order |
    | Windows behaviour | Existing tests and area-boundary CI (G8); A6 oracle drivers skip on Windows by decision | Unchanged |

### Shim and exported-API audit

17. **No shim.** Measured by AST (`go/parser`, not text search) over the 19
    starting A6 files and the 37 A6 files at coding HEAD:

    | Check | Start | Head |
    |---|---|---|
    | Type aliases (`type X = Y`) | 0 | 0 |
    | One-statement forwarders whose arguments equal its parameters | 2 | 2 |

    The two forwarders exist at the starting commit and are unchanged, so none
    was added: `(*candidateArtifactGuardError).Error` and
    `(*candidateArtifactGuardUnavailableError).Error`, error-wrapping accessors
    that moved with their types from `wt_merge.go` to
    `merge_artifact_guard.go`. The A6 Shim Register is empty.

18. **Exported surface compared with the starting commit.** Top-level exported
    functions, methods on exported receivers, types, constants and variables of
    the A6 files, printed with `go/printer` and compared as normalised text: 66
    declarations at the start, 67 at coding HEAD, none removed. The differences
    are exactly:

    | Difference | Cause |
    |---|---|
    | One added type, `IntegrationReason` | F3 typing |
    | The five `IntegrationReason…` constants now carry that type (values unchanged) | F3 typing |
    | `IntegrationFailedError.Reason` is `IntegrationReason` instead of `string` | F3 typing; `Error()` text unchanged |
    | `IntegrationProgressWaitingDeferredValidation` is initialised from `string(integrationProgressWaitingDeferred)` | F4 typing; same value, still an untyped string constant |

    No other signature, value or type changed. Both the typing and its callers
    are K7-documented edits; no A6 declaration was renamed, moved across areas
    or given a forwarder.

### Confirmations

19. **C5 timestamps (Scope 16).** The two persisted local-time merge records
    are `Timestamp: time.Now()` (the completion handoff event) and
    `Time: time.Now()` (the MERGED history entry), both in the single function
    `recordMergeCompletion` (`merge_finalize.go`), called from `commitMerged`'s
    final MERGED transaction. At the starting commit they sat at
    `wt_merge.go:1284` and `:1306` in the same transaction. Every other
    timestamp keeps its zone: `recordMergedDeferrals` is still called with
    `time.Now().UTC()` and the failure timestamps in `merge_failure.go` are
    UTC. `TestRecordMergeCompletionAppendsBothMergeRecords` pins the records
    and the oracle case `TestOracleWtMerge/success` pins their values.
    Scope 16 edits one function.

20. **Prompts-read types.** `IntegrationProgressDecision` and
    `IntegrationProgressReason` (`integration_progress.go`) and
    `ReconcileIntegrationAnalysesResult` (`integration_reconcile.go`) have
    byte-identical struct bodies at the starting commit and at coding HEAD
    (same fields, types and tags; compared on the declaration text), and
    `EvaluateLiveIntegrationProgress(state *models.State, projectRoot string)
    (IntegrationProgressDecision, error)` has the same signature. None appears
    in the exported-surface difference above.

### K1 inventory

21. **Method.** The inherited Git and `exec` calls inside A6 transactions were
    traced by static SSA reachability over `internal/ops` and its module
    dependencies (`golang.org/x/tools` v0.29.0, a scratch program outside the
    repository, run on exported copies of both trees so the worktree stayed
    clean), on the coding-HEAD tree and on `git archive 96c9ad69a`. Roots are
    every `func(*models.State) error` an A6 file passes to `Modify`,
    `modifyLifecycleState`, `ModifyWithAgentAuthority` or a `stateMutation`
    value; sinks are any `internal/git` or `internal/subprocess` function and
    the `os/exec` `Cmd` run methods. A K1 site is a pair (A6 transaction,
    inherited first-hop call) whose static closure reaches a sink (D33,
    `k1-ruling`). Dynamic calls (interface methods and closure-valued
    parameters) are not followed statically. They were listed in both trees:
    25 distinct kinds, identical at the start and at coding HEAD once source
    positions and SSA register names are removed.

22. **Result.** There are 17 A6 transactions in both trees. One reaches a sink,
    through one inherited call, in both trees: **1 site at coding HEAD, equal
    to the starting commit's count, not above 1**. No refactor added a Git or
    `exec` call to a `Modify`, and the merge transactions perform no Git: CAS,
    index sync, rollback and receipt-proof reads run outside the state lock.

    | ID | Transaction at coding HEAD (opener) | Inherited call at coding HEAD | Location at `96c9ad69a` (transaction, call) |
    |---|---|---|---|
    | K1-E1 | `recover_integration.go:114` `recoverIntegrationLocked` (`bb.Modify`) | `statevalidate.ValidateCandidate(…, false, …)`, `recover_integration.go:141`, reaching `git.TreePathMode` through `checkArtifactRefFileExists` for an artifact ref absent from the working tree | `recover_integration.go:105`, `:132` |

    The other 16 A6 transactions reach no sink in either tree. Their locations
    follow the moves:

    | Transaction | At `96c9ad69a` | At coding HEAD |
    |---|---|---|
    | runner claim | `deferred_validation.go:798` | `deferred_validation_runner.go:304` |
    | runner publish | `deferred_validation.go:833` | `deferred_validation_runner.go:339` |
    | rule, unrule, retire, settle, abort | `deferred_validation_ruling.go:58,87,120,164,208` | `deferred_validation_ruling.go:81,110,143,187,231` |
    | integration reconciliation | `integration_reconcile.go:140` | `integration_reconcile.go:154` |
    | external-merge reconciliation | `reconcile_merged.go:74` | `reconcile_merged.go:73` |
    | worktree base-commit record | `wt_create.go:125` | `wt_create.go:168` (`recordWorktreeBaseCommit`) |
    | worktree deletion | `wt_delete.go:73` | `wt_delete.go:73` |
    | merge failure record | `wt_merge.go:197` | `merge_failure.go:88` |
    | mutation receipt | `wt_merge.go:352` | `merge_ref_mutation.go:42` |
    | preparation retirement | `wt_merge.go:441` | `merge_admission.go:109` |
    | merge preparation | `wt_merge.go:981` | `merge_preconditions.go:74` |
    | final MERGED | `wt_merge.go:1255` | `merge_finalize.go:62` (`commitMerged`) |

    K1-E1 keeps its checks, order and refusals inside its transaction (D33).
    The inventory is a floor for direct calls, not for every transitive
    helper; a future refactor re-runs the trace and may add a site only with
    evidence that it reaches a sink at `96c9ad69a`.

### References

23. **Chain-document citations resolve.** Every `*.go` citation in
    `INVARIANTS.md`, `specs/architecture/{state-machines,blackboard-schema,supervision-model}.md`,
    `specs/protocols/` and `lessons/` (29 documents) was resolved against the tracked files at coding HEAD. The only
    names that are not a tracked file are an illustrative payload path
    (`src/middleware/auth.go` in `blackboard-schema.md`) and the `*_test.go`
    glob of a lesson. A6 files are cited by 21 `INVARIANTS.md` rows, one
    `blackboard-schema.md` line and one lesson line, naming 26 distinct A6
    files; all resolve, and the behaviour each row describes was read in the
    file it names. All 306 top-level identifiers of the 19 starting A6 files
    still exist in the 37 A6 files, and the three A6 identifiers the chain
    documents mention (`appendUniqueAgentID` in `merge_failure.go`,
    `mergeWorktree` in `wt_merge.go`, `DeleteWorktree` in `wt_delete.go`) are
    defined where cited. The concern units updated the citations their
    extractions invalidated. This task corrects one location pair: the
    `restore` lock step of the merge moved with `restoreCheckout` into
    `merge_finalize.go`, which the two `INVARIANTS.md` rows naming the main-index
    sync/restore lock (§5 and §7) did not list, so both now cite it. No
    meaning changed.

### Notes for Scope 25 and Scope 16

24. No citation of A6 code drifts semantically: each row's described behaviour
    is in a cited file. Two pointers for later scopes:

    | Item | Observation |
    |---|---|
    | Goal text and plan line numbers | The goal and plans give `wt_merge.go:1284,1306` for the C5 sites. They are historical positions in the starting tree; Scope 16 edits `recordMergeCompletion` in `merge_finalize.go` |
    | Owner gaps | The `… is required` precondition wording, the `tool-results` directory name and the `recovery_hint` detail key are repeated across areas (inventory table 5); their owners (A3 helpers, `paths`/`toolresult`, A7 error details) adopt a shared name when refactored |

    A6 issue closures belong to the A7 boundary unit; the registry is not
    edited here.

### Scope limits

25. **No behaviour or surface change.** Error text, `--json` codes,
    `state.yaml` effects, lock names and order, alert categories, warning order
    and every oracle golden are unchanged. The A6 oracle goldens were produced
    from the starting tree and reproduced by the regeneration check. Timestamps
    stay at their call points. The overview is rewritten only at the run's
    close.

## Consequences

- Every A6 file and function is within the size guard and each concern can be
  read and tested without the others' phases; the cost is 18 more files in one
  package and a longer file list per operation.
- A misspelled integration reason, progress code or deferred-validation reason
  is a compile error for typed positions. The types do not check membership: an
  untyped constant or explicit conversion still compiles, so membership stays
  enforced by existing runtime validation.
- A forgotten AC3 conversion compiles but changes how a value is stored or
  compared. The spelling tests and the oracle goldens are the defence.
- The one inherited K1 site (K1-E1) remains debt under D33: Git runs inside the
  `recover-integration` transaction until a run whose goal admits a
  synchronization redesign. The count cannot grow without a recorded
  inheritance proof.
- The exported A6 surface is unchanged for A7, A8 and A9, so later areas adopt
  the typed names under their own boundary, not as part of A6. The `prompts`
  package keeps reading the three integration-progress types until Scope 19.
- Owner-gap names live in `ops` until their owners are refactored; the same
  spelling then exists in two packages and the inventory lists each site.
- Two gap paths remain covered only by white-box tests: the post-merge index
  refresh and the configured-language index refreshes. A change to either must
  add its case first or restate the exclusion.

## Alternatives Considered

1. **An `ops/integration` sub-package:** rejected; see Decision 5.
2. **A generic lifecycle-step framework over merge, reconcile and recover:**
   rejected; see Decision 6.
3. **A refresh coordinator that resolves `issue-intel-refresh`:** rejected for
   this area; it spans A3, A5 and A9 paths and changes frozen warning text and
   failure tolerance (Decision 7).
4. **Move the inherited Git revalidation out of the recovery transaction:**
   rejected; it changes the concurrency semantics INVARIANTS §5 protects and is
   not an authorised change (D33).
5. **Retype exported result fields, persisted batch fields or the progress
   reason fields `prompts` reads:** rejected; it would change wire, A1 and A8
   surfaces. Those carriers stay `string` and values are converted at the
   boundary.
6. **Edit `brand`, `paths`, `referencecontract` or `models` to add the missing
   constants:** rejected for this area; closed owners are not edited by A6 (X1),
   and the owner-gap names are recorded for their area.
7. **Build oracle cases for the post-merge index refresh and the
   configured-language refreshes:** rejected; the first leaves a detached
   coordinator running after the case and the second needs external indexers
   (Decision 16).
8. **Split the 100-line `reconcileIntegrationAnalyses`:** rejected; the size
   guard flags only functions over 100 lines, the function is not an offender,
   and the plan holds it at its measured length.
