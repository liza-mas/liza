# 224 - A5 Ops Review and Acceptance Concerns and Vocabulary Ownership

## Status

ACCEPTED.

## Context

The architecture-consolidation run splits the A5 area of `internal/ops` (review
and acceptance) along concerns until no A5 function or file exceeds the size
guard, and moves its finite control-flow vocabulary and magic values to named
owners without a new configuration field
([goal K7](../../goals/20261023-architecture-consolidation.md#structural-constraints)).
At the starting commit `96c9ad69a` the area was 32 files and 11,351 lines with
18 size offenders (6 files, 12 functions). The worst were
`prepareSubmitForReview` (562 lines, one function holding submission
preconditions, task-locked Git work, acceptance execution and the final
transaction) and `acceptance_evidence.go` (1,105 lines). The decision is indexed
in the [ADR README](README.md).

[ADR-0021](0021-ops-service-layer-for-mutations.md) makes `internal/ops` the
service layer for state mutations and
[ADR-0130](0130-generation-fenced-agent-authority.md) fixes the generation
fence the review operations rely on. This ADR keeps both: it records how A5 is
laid out inside that boundary, which exported surface later areas keep using,
where its vocabulary lives, and the evidence that the area is closable.
[ADR-0222](0222-a3-ops-kernel-concerns-and-vocabulary.md) settled the kernel
layout and the A3 types A5 consumes; [ADR-0220](0220-a1-typed-vocabulary.md)
the typed-vocabulary rules (defined string types, no codec methods, conversion
at boxed-string boundaries, unknown values unchanged);
[ADR-0223](0223-a4-ops-planning-concerns-and-vocabulary.md) is the A4 precedent.
All of them apply here unchanged.

It records the layout approved in
[Scope 1 of the A5 architecture](../../arch-plan/20261023-architecture-consolidation/20261023-232424-arm-1-ar-14.md#scope-1-a4-boundary-and-a5-review-and-acceptance-consolidation)
and delivered by Tasks 2–8 of its code plan. The occurrence-by-occurrence
vocabulary evidence is the
[A5 K7 inventory](../consolidation-run/a5-k7-inventory.md). The measurements
below were taken at coding HEAD `f8547570c` (clean worktree, no commit missing
from the integration branch) against the starting commit.

## Decision

### Concern layout

1. **Review and acceptance stay inside `internal/ops`.** A5 adds no package,
   dependency, framework, configuration field, layer or allowlist entry, S2
   exception or crossing, and keeps every exported entry operation, input and
   result type. Concerns are separated by private, concern-named files and
   functions. `ops` stays application-layer. The A5 guard data (layers,
   allowlist, ratchet, S2) is empty; `TestAreaA5RatchetIsEmpty` holds the
   boundary condition. The 18 starting keys are gone: the longest A5 file is
   `orphaned_review_base.go` at 726 lines (limit 800) and the longest function
   is `runRejectionRCAOperation` at 96 lines (limit 100), over 48 files and
   12,793 lines.

2. **Concern files.** Entry files keep their original name and the exported
   entry; the private files take the separated phases (all `internal/ops/`).

   | Concern | Files | Separated responsibility |
   |---|---|---|
   | K-ACC acceptance evidence and validation readiness | `acceptance_evidence.go`, `acceptance_declaration.go`, `acceptance_allocation_review.go`, `acceptance_reference_reads.go`, `acceptance_execution.go`, `acceptance_creation.go`, `acceptance_claim_block.go`, `acceptance_ownership_fence.go`, `archive_acceptance_receipts.go`, `repair_acceptance_commits.go`, `validation_preflight.go`, `validation_readiness.go` | Declared contract read from the carrier, then reviewed allocation and reference comparison, then bounded blob reads, then receipt preparation and command execution outside the lock, then the final recheck. Readiness preflight splits probing from the readiness record and its alert; refusal block, archive and repair are separate operations |
   | K-SUB submission | `submit_review.go`, `submission_preconditions.go`, `submission_git_work.go`, `submission_finalize.go`, `submission_admission.go`, `submission_lifecycle.go`, `submit_progress.go`, `submit_hold.go`, `inflight_submit.go`, `test_detection.go` | `submit_review.go` is orchestration over one run: snapshot preconditions, then task-locked Git work (candidate, TDD, rebase, carrier checks), then acceptance admission and execution, then the final transaction and result. The rebase-conflict route and preparation record sit with the Git work, the receipt recheck and publication with the finalize step |
   | K-VER verdict recording | `submit_verdict.go`, `verdict_transition.go`, `verdict_integration_evidence.go`, `verdict_failure_anomaly.go`, `pending_verdict.go` (`_unix`, `_windows`), `quarantined_verdict.go`, `rejection_rca.go`, `reaffirm_proof.go` | Input and impact validation, then reviewed-commit check, then one atomic transition (approval, rejection, quorum, rejection-RCA gate, integration evidence), with failure and stale-verdict anomalies projected apart. Outbox replay, quarantine and reconcile, RCA record and resume, and proof reaffirmation remain separate operations |
   | K-AWT await protocols | `await_verdict.go`, `await_verdict_outcome.go`, `await_verdict_finish.go`, `await_resubmission.go`, `review_reservation.go`, `review_reclaim.go`, `await_policy.go` | Ownership acquisition, then bounded polling, then verdict or resubmission classification, then the reclaim transaction or the recovered-verdict result, then ownership release and budget reporting. The two protocols share only the polling policy |
   | K-OWN review ownership and boundary | `claim_reviewer_task.go`, `reviewer_claim_selection.go`, `reviewer_claim_candidate.go`, `update_review_commit.go`, `review_boundary.go`, `reviewed_range.go`, `orphaned_review_base.go`, `clear_stale_review_claims.go`, `repair_review_ownership.go` | Eligibility and diversity selection, then per-candidate request and boundary checks inside the claim transaction, then claim effects and the candidate-failure envelope. Review-commit update, orphaned-base heal, stale-claim clearing and repair keep their own files |

   Sixteen of the 48 files are new private concern files. Each operation keeps
   its phase order (preconditions from a snapshot, Git and command work under
   the task or integration lock, one final transaction that revalidates, then
   projection and cleanup), its error types, first refusal and effect order. A
   helper extracted from a transaction receives explicit state and context and
   never opens a blackboard, task, integration or file lock. Generation fencing,
   request identity and receipt replay stay in the A3 kernel.

3. **C-site anchors.** The C1 lease sites stay single and findable:
   `reclaimReviewLeaseDuration` (`review_reclaim.go`, one use) and the reviewer
   claim lease derived from `ClaimReviewerTaskInput.LeaseDuration`
   (`claim_reviewer_task.go`); K7 named them without changing a value. The C2
   diversity comparisons stay in `reviewer_claim_selection.go`
   (`pickWithApprovalDiversity`, `pickWithFreshDiversity`,
   `filterDoerProviderDiversity`, `isBlockedByDoerDiversity` and its `…At`
   variant); no comparison was added or copied, so C2 edits one concern.

4. **Rejected: an `ops/review` sub-package.** A5 code calls kernel helpers
   (`lifecycleMutation`, `modifyLifecycleState`), A4 helpers
   (`validateBoundedContractCorrection`, `checkPlanningOutputSnapshot`) and A6
   locks (`withIntegrationMutationLock`,
   `withEffectiveIntegrationCompletionLinearization`). A sub-package would
   export those helpers or create import cycles, for no K3 crossing to resolve
   and no size need that private files do not meet.

5. **Rejected: a generic await or lifecycle engine.** `await_verdict` and
   `await_resubmission` differ in ownership, reservation and finish policy and
   are exercised by disjoint oracle rows. A shared engine would hide those
   orderings behind callbacks. The real duplication is the polling policy,
   which already lives once in `await_policy.go` (K6).

### Retained exported surface

6. **The exported A5 surface is unchanged.** `SubmitForReview*`,
   `SubmitVerdict*`, `AwaitVerdict*`, `AwaitResubmission*`, `ClaimReviewerTask`,
   `ReviewerClaimEligible`, `ReviewerSlotAdmits`, `ValidationPreflight` and
   `PrepareValidationPreflightContext`, `ReplayPendingVerdicts`,
   `HealOrphanedReviewBase` with its input, outcome and error types,
   `RecordRejectionRCA*`, `ResumeRejectionRCA*`, `ReconcileVerdict`,
   `ReaffirmProof`, `UpdateReviewCommit`, `ClearStaleReviewClaims`,
   `ArchiveTerminalAcceptanceReceipts`, `RepairAcceptanceCommits` and the
   verdict and claim-class constants keep their names, signatures and values.
   `internal/commands`, `cmd/liza` and `internal/agent` (A7 and A9) are
   unchanged callers; typed vocabulary reaches them by conversion only.

### Vocabulary ownership

The plan numbers the A5 families F1–F12 and applies the rules of the A3 and A4
inventories: adopt the delivered owner (R1), follow the first owner's
classification (R2), name an owner gap in `ops` without editing the closed
owner (R3), name or type a new A5 value in its concern (R4), classify literal
data (R5). The inventory scanned 1,340 literals in the 48 files (146 inside
constant declarations, 1,194 retained and classified with a reason) and left
none unclassified.

7. **Adopted owners (F1, F2, F7, F11).** A5 consumes and never redefines these
   sets: the A1 types `models.TaskEventName`, `models.AnomalyType`,
   `models.RejectionCause`, `models.RecoveryPath`, `models.RestoreMode`,
   `models.AllocationRoute`, `pipeline.ReviewImpact`, `roles.RoleType`; the A3
   `LifecycleSafeAction` and `LifecycleEffects`; the lifecycle outcomes of
   `models.Lifecycle*`; and the **A3-owned review-claim classes**
   (`ReviewClaimClass`, `claim_failure_anomaly.go`), which A5's claim producers
   use and which the plan's `a5-k7` listed as A5-owned. A5 defines no second
   type for any of them. The history `Extra` keys that A4 reads
   (`blocked_class`, `gated_at`, `first_rejection_at`, `causes`,
   `recovery_path`) are the A4 `rejectionRCA*Key` constants in A5's writers.

8. **A5 owns eight finite sets (F3–F6, F8).** Each is a defined type over
   `string` with one constant per value, declared in its concern, with no codec
   or `IsZero` method. The exported result fields keep `string` and take the
   typed constant by conversion, so JSON and persisted bytes are unchanged.

   | Type | Values | Concern |
   |---|---|---|
   | `AwaitVerdictOutcome` | `APPROVED`, `REJECTED`, `NEW_ATTEMPT`, `ALREADY_TRANSITIONED`, `TERMINAL`, `TIMEOUT`, `ABORTED`, `POLL`, `PAUSED` | await-verdict (`await_verdict_outcome.go`) |
   | `AwaitSafeAction` | `revise`, `stop` | await-verdict; not the A3 lifecycle set |
   | `AwaitResubmissionOutcome` | `RESUBMITTED`, `TERMINAL`, `TIMEOUT`, `ABORTED`, `POLL`, `PAUSED` | await-resubmission |
   | `submittedVerdict` | `APPROVED`, `REJECTED` | verdict transition |
   | `validationCode`, `validationResult`, `validationMethod` | 15, 2 and 1 values | validation readiness |
   | `OrphanedBaseHealOutcome` | `not_applicable`, `healed`, `integration_failed` | orphaned-base heal (existing, adopted) |
   | `AcceptanceFault` | `content`, `allocation`, `runtime_input`, `admission_slot`, `batch_timeout` | acceptance evidence (existing, adopted) |

   The three await types and their constants are the only exported
   declarations that gained a type. The configured validation execution policy
   values stay plain strings: `models` keeps them user-configurable.

9. **Owner gaps are named in `ops`, not fixed in the owner (R3).** A closed
   package that never named a value keeps its form (X1); A5 names the value
   once at the consuming concern and the inventory lists the other sites for
   their area (X7): the reconciliation dispositions `refuted` and `superseded`
   (`models.IsVerdictDisposition` switches on literals), the `AGENT_ID` and
   `AGENT_GENERATION` environment suffixes, the reference anchor separator `#`,
   the reclaim-failure marker `transitioned to attempt` (produced by an A3
   refusal, matched by an A5 await, pinned by
   `TestA5AttemptTransitionMarkerIsInTheClaimRefusal`), and the operation names
   `reaffirm-proof`, `reconcile-verdict` and `await-verdict`.

10. **Named values, not types (F9, F10).** Timing, backoff, retry, truncation
    and byte limits, lock labels, file extensions and ID formats became named
    constants in their concern with unchanged values (62 substitution sites,
    24 declarations in Task 8, 120 constants already named). Test-replaceable
    timing variables stay variables, initialised from the named default.

11. **Configured identities and literal data stay dynamic (F12).** Role names,
    role pairs, tool and provider names, configured commands, task, agent,
    finding and request IDs, SHAs, branch names and user text reach the code as
    variables. The test-file naming table of `test_detection.go` is data about
    the user's project and is not promoted to configuration. Same-spelling
    values with another meaning are recorded as false friends (inventory
    table 4), not merged with the typed sets.

12. **AC3 string-boundary rule.** A vocabulary value is converted to `string`
    where it leaves its typed position for a string-kinded consumer: history
    `Extra`, anomaly and error details, log fields, digest material, result
    fields and `map[string]any`. Types name decisions that already exist; they
    add no validation, default or branch. Spellings are pinned by white-box
    tests, among them `TestAwaitVerdictVocabularyKeepsItsValues`,
    `TestAwaitResubmissionVocabularyKeepsItsValues`,
    `TestVerdictNamedValuesKeepTheirValues`,
    `TestValidationReadinessVocabularyKeepsWireSpelling`,
    `TestReviewOwnershipNamedValuesKeepTheirSpelling` and the
    `internal/ops/k7_a5_vocabulary_test.go` set.

### Supplement oracle

13. **Coverage added before the surfaces were touched.** The delivered 40-case
    A5 oracle left several frozen surfaces uncovered. The additive package
    `internal/oracle/a5/supplement/` pins them with 55 cases, committed before
    the production edit each one guards, with goldens generated from the
    starting tree by the regeneration entry point: await-verdict paths (AW,
    11), orphaned-base heal (OH, 3), pending-verdict replay (PV, 2),
    await-resubmission paths (RW, 6), acceptance-declaration refusals (SA, 10),
    submission refusals and the rebase-conflict route (SB, 8) and
    validation-readiness refusals (VR, 15). The package follows the
    `a1/cli`/`a3/control` precedent: manifests are discovered by glob and
    adapters register per file, so later units add files and edit none. The OH2
    golden masks Git's version-dependent rebase transcript (`47e12d912`).

### Shim and exported-API audit

14. **No shim.** Measured by AST (`go/parser`, not text search) over the 32
    starting A5 files and the 48 A5 files at coding HEAD:

    | Check | Start | Head |
    |---|---|---|
    | Type aliases (`type X = Y`) | 0 | 0 |
    | Added alias lines in the `internal/ops` production diff since `96c9ad69a` | n/a | 0 |
    | One-statement forwarders whose arguments equal its parameters | 4 | 4 |

    The four forwarders exist at the starting commit and are unchanged, so none
    was added: `AcceptanceAllocationRef` (now in
    `acceptance_allocation_review.go`; the exported wrapper of the private
    function), `(*acceptanceBatchTimeoutError).Error`
    (`acceptance_execution.go`, an error-wrapping accessor), `ShortCommit`
    (`repair_acceptance_commits.go`) and
    `SubmitVerdictWithAuthorityAndOptions` (`submit_verdict.go`). The A5 Shim
    Register is empty.

15. **Exported surface compared with the starting commit.** Top-level exported
    functions, methods on exported receivers, types, constants and variables,
    printed with `go/printer` and compared as normalised text: 135 declarations
    at the start, 138 at coding HEAD, none removed. The differences are
    exactly:

    | Difference | Cause |
    |---|---|
    | Three added types, `AwaitVerdictOutcome`, `AwaitSafeAction`, `AwaitResubmissionOutcome`, and their 17 constants now carrying them (values unchanged) | F3/F4 typing, `2cf671c92` and `ce35ce28f` |
    | `ReviewerClaimPolicyResolver.ProviderDiversity` takes `pipeline.ReviewImpact` and returns `pipeline.ProviderDiversityMode` instead of `string` | A1 AC4 conformance for V11/V12, `eebdbff8a`; the diversity logic is untouched |
    | `DefaultArchiveLimit` is built from `defaultArchiveMaxTasks` (8) and `defaultArchiveMaxBytes` (4 << 20) | F9 naming, same value |

    No other signature, value or type changed. Both the typing and the
    conformance are X2/K7-documented edits; no A5 declaration was renamed,
    moved across areas or given a forwarder.

### K1 inventory

16. **Method.** The inherited Git and `exec` calls inside A5 transactions were
    traced by static SSA reachability over `internal/ops` and its module
    dependencies (`golang.org/x/tools` v0.29.0, a scratch program outside the
    repository), on the coding-HEAD tree and on an exported copy of
    `96c9ad69a`. Roots are every `func(*models.State) error` an A5 file passes
    to `Modify`, `modifyLifecycleState` or a `lifecycleMutation(...)` result;
    sinks are any `internal/git` function, `internal/subprocess` and the `os/exec`
    `Cmd` run methods. A K1 site is a pair (A5 transaction, inherited first-hop
    call) whose static closure reaches a sink (D33, `k1-ruling`). Dynamic
    calls (interface methods and closure-valued parameters) are not followed
    statically. They were listed in both trees, and the coding-HEAD list adds no
    kind of dynamic call absent from the starting list, apart from SSA register
    names; the plan's source-read classification of them (test hooks, kernel
    request and receipt closures, rejection-RCA mutation callbacks) is the
    one carried over.

17. **Result.** There are 31 A5 transactions in both trees. Nine reach a sink
    through 13 inherited calls, identical per transaction in both trees, and
    K1-E6 is the A9 transaction that calls an A5 method: 14 sites at both,
    **equal to the starting commit's count, not above it**. No refactor added a
    Git or `exec` call to a `Modify`.

    | ID | Transaction at coding HEAD (opener) | Inherited call at coding HEAD | Location at `96c9ad69a` (transaction, call) |
    |---|---|---|---|
    | K1-E1 | `claim_reviewer_task.go:205` `(*reviewerClaim).attempt` (`lifecycleMutation`) | `CheckCurrent`, `claim_reviewer_task.go:302` | `claim_reviewer_task.go:240`, `:436` |
    | K1-E2 | same | `validateReviewBoundaryForAssignment`, `reviewer_claim_candidate.go:64` | `:240`, `claim_reviewer_task.go:379` |
    | K1-E10 | same | `ValidationRetryPending`, `reviewer_claim_selection.go:118` | `:240`, `claim_reviewer_task.go:290` |
    | K1-E3 | `review_reclaim.go:65` `reclaimForReview` (`modifyLifecycleState`) | `CheckCurrent`, `review_reclaim.go:107` | `await_resubmission.go:662`, `:666` |
    | K1-E4 | same | `validateReviewBoundaryForAssignment`, `review_reclaim.go:136` | `:662`, `await_resubmission.go:687` |
    | K1-E5 | `validation_readiness.go:100` `recordReadiness` (`bb.Modify`) | `CheckCurrent`, `validation_readiness.go:101` | `validation_preflight.go:343`, `:344` |
    | K1-E6 | `internal/agent/systemctl.go` provider-start transaction (A9) | `CheckLaunchCurrent`, `systemctl.go:269` → `CheckCurrent`, `validation_preflight.go:122` | `systemctl.go:269` → `validation_preflight.go:125` |
    | K1-E7 | `submission_finalize.go:105` `(*submissionRun).publish` (`modifyLifecycleState`) | `recheckAcceptanceReceipt`, `submission_finalize.go:185` | `submit_review.go:531`, `:573` |
    | K1-E11 | same | `validateBoundedContractCorrection`, `submission_finalize.go:179` | `:531`, `submit_review.go:566` |
    | K1-E8 | `update_review_commit.go:117` `updateReviewCommitLocked` (`bb.Modify`) | `recheckAcceptanceReceipt`, `update_review_commit.go:233` | `update_review_commit.go:152`, `:170` |
    | K1-E9 | `orphaned_review_base.go:527` `(*orphanHeal).publish` (`modifyLifecycleState`) | `recheckAcceptanceReceipt`, `orphaned_review_base.go:538` | `orphaned_review_base.go:517`, `:528` |
    | K1-E12 | `reaffirm_proof.go:59` `ReaffirmProof` (`Modify`) | `observeProofDrift`, `reaffirm_proof.go:71` | `reaffirm_proof.go:54`, `:66` |
    | K1-E13 | `rejection_rca.go:233` `runRejectionRCAOperation` (`modifyLifecycleState`) | `statevalidate.ValidateCandidate`, `rejection_rca.go:267` | `:233`, `:267` |
    | K1-E14 | `submit_verdict.go:476` `(*verdictAttempt).commit` (`modifyLifecycleState`) | `validateIntegrationLifecycleCandidate`, `verdict_transition.go:334` → `ValidateCandidate`, `verdict_integration_evidence.go:87` | `submit_verdict.go:456`, `:666` → `:857` |

    The other 22 A5 transactions reach no sink in either tree. At coding HEAD:
    `acceptance_claim_block.go:157`, `archive_acceptance_receipts.go:86`,
    `await_verdict.go:368`, `await_verdict_finish.go:51,185`,
    `clear_stale_review_claims.go:46`, `orphaned_review_base.go:417,649`,
    `quarantined_verdict.go:155,342`, `repair_acceptance_commits.go:126`,
    `repair_review_ownership.go:31`, `review_reservation.go:115,168,201,248`,
    `submission_git_work.go:214,383`, `validation_preflight.go:423,490`,
    `verdict_failure_anomaly.go:71,126`.

    Each listed call keeps its checks, order and refusals inside its
    transaction (D33). The code marks the sites with `K1-E…` comments where the
    call sits. The inventory is a floor for direct calls, not for every
    transitive helper; a future refactor re-runs the trace and may add a site
    only with evidence that it reaches a sink at `96c9ad69a`.

### References

18. **Chain-document citations resolve.** Every `*.go` citation in
    `INVARIANTS.md`, `specs/architecture/{state-machines,blackboard-schema,supervision-model}.md`,
    `specs/protocols/` and `lessons/` (29 documents) was resolved against the
    tracked files at coding HEAD. A5 files are cited on 34 lines (33 in
    `INVARIANTS.md`, one in `specs/protocols/await-primitives.md`) by 25
    files; all resolve. Of the 495 declarations of the 32 starting A5 files one
    (`awaitResubmissionPolling`) no longer exists, and no document cites it; no
    cited identifier is defined only outside the file its line cites. The
    concern units updated the citations their extractions invalidated, so this
    task corrects no location and edits no chain document.

### Notes for Scope 25 (pre-existing semantic drift, recorded, not rewritten)

19. Two citations resolve to a file that does not hold the behaviour the
    sentence describes. Both are identical at the starting commit (the
    `INVARIANTS.md` text then cited `submit_review.go`/`submit_verdict.go`, which
    held no such check either), and each is the subject of an open registry
    issue that A5 preserves rather than fixes (X9):

    | Location | Observation | Issue |
    |---|---|---|
    | `INVARIANTS.md:245` (§7 clean sync) | Cites `submit_review.go`. Neither it, at either revision, nor any other submission file at coding HEAD runs a worktree-status check. Only the strict acceptance gate refuses a dirty worktree (`checkAcceptanceWorktree`, `acceptance_evidence.go`), so for a submission without an acceptance contract the "working tree must be clean" invariant is documented but not enforced | [INVARIANTS.md §7 Clean Worktree Not Enforced at Submission](../architectural-issues.md#invariantsmd-7-clean-worktree-not-enforced-at-submission) |
    | `INVARIANTS.md:218` (§6 quorum) | Cites `verdict_transition.go`. Neither it nor `submit_verdict.go` evaluates provider diversity at verdict time (it is enforced at reviewer claim and preferred at merge readiness) | [INVARIANTS.md §6 Provider Diversity Not Enforced at Verdict Time](../architectural-issues.md#invariantsmd-6-provider-diversity-not-enforced-at-verdict-time) |

    The vocabulary issue
    ([Control-Flow Vocabulary Bypasses Domain Ownership](../architectural-issues.md#control-flow-vocabulary-bypasses-domain-ownership))
    stays open: later areas still adopt the A5 names at their sites (inventory
    table 7), and A5 issue closures belong to the A6 boundary unit.

### Scope limits

20. **No behaviour or surface change.** Error text, `--json` codes,
    `state.yaml` effects, lock names, alert categories and every oracle golden
    are unchanged. Timestamps stay at their call points. The overview is
    rewritten only at the run's close.

## Consequences

- Every A5 file and function is within the size guard and each concern can be
  read and tested without the others' phases; the cost is 16 more files in one
  package and a longer file list per operation.
- A misspelled await outcome, verdict or readiness code is a compile error for
  typed positions. The types do not check membership: an untyped constant or
  explicit conversion still compiles, so membership stays enforced by existing
  runtime validation.
- A forgotten AC3 conversion compiles but changes how a value is stored or
  hashed. The spelling tests, the fingerprint tests and the oracle goldens are
  the defence.
- The 14 inherited K1 sites remain debt under D33: Git runs inside blackboard
  transactions at those sites until a run whose goal admits a synchronization
  redesign. The count cannot grow without a recorded inheritance proof.
- The exported A5 surface is unchanged for A7 and A9, so later areas adopt the
  typed names under their own boundary, not as part of A5.
- Owner-gap names live in `ops` until their owners are refactored; the same
  spelling then exists in two packages and the inventory lists each site.

## Alternatives Considered

1. **An `ops/review` sub-package:** rejected; see Decision 4.
2. **A generic await or lifecycle engine:** rejected; see Decision 5.
3. **Move the inherited Git revalidation out of the transactions:** rejected;
   it changes the concurrency semantics INVARIANTS §5 protects and is not an
   authorised change (D33).
4. **Retype exported result fields or persisted model fields:** rejected; it
   would change wire and A1 surfaces. Those carriers stay `string` and values
   are converted at the boundary.
5. **Edit `models`, `referencecontract` or `brand` to add the missing
   constants:** rejected for this area; closed owners are not edited by A5
   (X1), and the owner-gap names are recorded for their area.
6. **A second review-claim class type in A5:** rejected; A3 delivered
   `ReviewClaimClass` and the plan forbids a parallel type.
7. **One shared vocabulary file for all A5 sets:** rejected; each set stays
   beside the concern that branches on it, as in ADR-0221 to ADR-0223.
