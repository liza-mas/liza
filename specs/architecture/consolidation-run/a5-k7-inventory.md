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
6. Completeness check: the scan was run on the committed tree and every row of its output is either a row of table 3 (retained literal) or part of a constant declaration listed in table 2 or 3b; table 3 groups the 1198 rows by file, class and reason, so the line numbers listed there can be checked against the source one by one. Cross-checks: a quoted-literal probe for the lifecycle safe actions and effects, the review impact levels, the claim and role types and the submitted verdicts finds only constant declarations in the 48 files; every `models.TaskEvent*` and `models.AnomalyType*` use in them is the constant, and the one raw `"submit_verdict_failed"` is an activity-log action label (table 4).

Outcome vocabulary: **adopted** (existing owner constant used), **named** (new constant, value unchanged), **typed** (defined type, one constant per value), **classified** (retained literal data or dynamic identity, with reason).

## Summary

| Outcome | Count |
|---|---|
| Substitution sites in this task (table 2: adopted, named or typed use sites) | 58 |
| Constant declarations added by this task (table 2) | 22 |
| Pre-existing named constants in A5 files (table 3b) | 120 |
| Retained literals classified R5/F12 (table 3, grouped by file and reason) | 1198 |
| Literals scanned at this tree | 1342 = 1198 retained (table 3) + 144 inside declarations of the 142 named constants (22 declared by this task in table 2, 120 pre-existing in table 3b) |
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

| file:line | F | Rule | Outcome | Constant | Replaces | Owner |
|---|---|---|---|---|---|---|
| `acceptance_declaration.go:174` | F2 | R4 | named | `reaffirmProofOperation` | "reaffirm-proof" (rendered command) | A5 reaffirm_proof.go |
| `acceptance_execution.go:42` | F9 | R4 | declaration of named | `acceptanceEnvSuffixURL` | "URL" | A5 acceptance_execution.go |
| `acceptance_execution.go:43` | F9 | R4 | declaration of named | `acceptanceEnvSuffixDSN` | "DSN" | A5 acceptance_execution.go |
| `acceptance_execution.go:230` | F9 | R4 | named | `acceptanceEnvSuffixURL, acceptanceEnvSuffixDSN` | "URL", "DSN" | A5 acceptance_execution.go |
| `acceptance_execution.go:231` | F12 | R3 | named | `agentGenerationEnvSuffix (twice)` | "AGENT_GENERATION" (twice) | A5 submission_lifecycle.go; `brand` owner gap |
| `archive_acceptance_receipts.go:23` | F9 | R4 | declaration of named | `archiveAcceptanceReceiptsLockOperation` | "archive-acceptance-receipts" | A5 archive_acceptance_receipts.go |
| `archive_acceptance_receipts.go:58` | F9 | R4 | named | `archiveAcceptanceReceiptsLockOperation` | "archive-acceptance-receipts" | A5 archive_acceptance_receipts.go |
| `await_resubmission.go:177` | F12 | R1 | adopted | `notFoundEntityAgent` | "agent" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `await_verdict.go:147` | F12 | R1 | adopted | `notFoundEntityAgent` | "agent" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `await_verdict.go:371` | F12 | R1 | adopted | `notFoundEntityAgent` | "agent" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `await_verdict_finish.go:18` | F3 | R4 | declaration of named | `awaitVerdictOperation` | "await-verdict" | A5 await_verdict_finish.go |
| `await_verdict_finish.go:188` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `await_verdict_finish.go:203` | F3 | R4 | named | `awaitVerdictOperation` | "await-verdict" | A5 await_verdict_finish.go |
| `await_verdict_outcome.go:336` | F11 | R1 | adopted | `oldReviewCommitExtraKey` | "old_review_commit" | update_review_commit.go (K-OWN) |
| `await_verdict_outcome.go:337` | F11 | R1 | adopted | `newReviewCommitExtraKey` | "new_review_commit" | update_review_commit.go (K-OWN) |
| `inflight_submit.go:28` | F9 | R4 | declaration of named | `inflightSubmitFileExt` | ".json" | A5 inflight_submit.go |
| `inflight_submit.go:67` | F9 | R4 | named | `inflightSubmitFileExt` | ".json" | A5 inflight_submit.go |
| `inflight_submit.go:132` | F9 | R4 | named | `inflightSubmitFileExt` | ".json" | A5 inflight_submit.go |
| `orphaned_review_base.go:48` | F9 | R4 | declaration of named | `orphanedBaseHealCauseLimit` | 500 | A5 orphaned_review_base.go |
| `orphaned_review_base.go:220` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `orphaned_review_base.go:530` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `orphaned_review_base.go:589` | F9 | R4 | named | `orphanedBaseHealCauseLimit` | 500 | A5 orphaned_review_base.go |
| `orphaned_review_base.go:697` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `pending_verdict.go:36` | F9 | R4 | declaration of named | `pendingVerdictFileExt` | ".json" | A5 pending_verdict.go |
| `pending_verdict.go:37` | F9 | R4 | declaration of named | `pendingVerdictFileNameFormat` | "%x.json" | A5 pending_verdict.go |
| `pending_verdict.go:40` | F9 | R4 | declaration of named | `pendingVerdictRequestIDFormat` | "verdict-%x" | A5 pending_verdict.go |
| `pending_verdict.go:42` | F9 | R4 | declaration of named | `publishPendingVerdictLockOperation` | "publish-pending-verdict" | A5 pending_verdict.go |
| `pending_verdict.go:43` | F9 | R4 | declaration of named | `removePendingVerdictLockOperation` | "remove-pending-verdict" | A5 pending_verdict.go |
| `pending_verdict.go:85` | F9 | R4 | named | `pendingVerdictRequestIDFormat` | "verdict-%x" | A5 pending_verdict.go |
| `pending_verdict.go:153` | F9 | R4 | named | `pendingVerdictFileNameFormat` | "%x.json" | A5 pending_verdict.go |
| `pending_verdict.go:155` | F9 | R4 | named | `publishPendingVerdictLockOperation` | "publish-pending-verdict" | A5 pending_verdict.go |
| `pending_verdict.go:231` | F9 | R4 | named | `pendingVerdictFileExt` | ".json" | A5 pending_verdict.go |
| `pending_verdict.go:290` | F9 | R4 | named | `removePendingVerdictLockOperation` | "remove-pending-verdict" | A5 pending_verdict.go |
| `pending_verdict.go:309` | F9 | R4 | named | `pendingVerdictFileNameFormat` | "%x.json" | A5 pending_verdict.go |
| `quarantined_verdict.go:29` | F2 | R4 | declaration of named | `reconcileVerdictOperation` | "reconcile-verdict" | A5 quarantined_verdict.go |
| `quarantined_verdict.go:32` | F9 | R4 | declaration of named | `taskReviewLockKeyFormat` | "task-review-%x" | A5 quarantined_verdict.go |
| `quarantined_verdict.go:33` | F9 | R4 | declaration of named | `quarantinedVerdictIDFormat` | "qv-%x" | A5 quarantined_verdict.go |
| `quarantined_verdict.go:36` | F5 | R3 | declaration of named | `reconcileDispositionRefuted` | "refuted" | A5 quarantined_verdict.go; `models` owner gap |
| `quarantined_verdict.go:37` | F5 | R3 | declaration of named | `reconcileDispositionSuperseded` | "superseded" | A5 quarantined_verdict.go; `models` owner gap |
| `quarantined_verdict.go:50` | F9 | R4 | named | `taskReviewLockKeyFormat` | "task-review-%x" | A5 quarantined_verdict.go |
| `quarantined_verdict.go:79` | F2 | R4 | named | `reconcileVerdictOperation` | "reconcile-verdict" | A5 quarantined_verdict.go |
| `quarantined_verdict.go:136` | F9 | R1 | adopted | `acceptanceMaskedValue` | "***" | acceptance_execution.go (K-ACC) |
| `quarantined_verdict.go:173` | F9 | R4 | named | `quarantinedVerdictIDFormat` | "qv-%x" | A5 quarantined_verdict.go |
| `quarantined_verdict.go:216` | F11 | R1 | adopted | `oldReviewCommitExtraKey` | "old_review_commit" | update_review_commit.go (K-OWN) |
| `quarantined_verdict.go:247` | F11 | R1 | adopted | `oldReviewCommitExtraKey` | "old_review_commit" | update_review_commit.go (K-OWN) |
| `quarantined_verdict.go:248` | F11 | R1 | adopted | `newReviewCommitExtraKey` | "new_review_commit" | update_review_commit.go (K-OWN) |
| `quarantined_verdict.go:295` | F5 | R3 | named | `reconcileDispositionRefuted, reconcileDispositionSuperseded` | "refuted", "superseded" | A5 quarantined_verdict.go; `models` owner gap |
| `quarantined_verdict.go:314` | F2 | R4 | named | `reconcileVerdictOperation` | "reconcile-verdict" | A5 quarantined_verdict.go |
| `quarantined_verdict.go:337` | F2 | R4 | named | `reconcileVerdictOperation` | "reconcile-verdict" | A5 quarantined_verdict.go |
| `quarantined_verdict.go:347` | F2 | R4 | named | `reconcileVerdictOperation` | "reconcile-verdict" | A5 quarantined_verdict.go |
| `reaffirm_proof.go:21` | F2 | R4 | declaration of named | `reaffirmProofOperation` | "reaffirm-proof" | A5 reaffirm_proof.go |
| `reaffirm_proof.go:64` | F2 | R4 | named | `reaffirmProofOperation` | "reaffirm-proof" | A5 reaffirm_proof.go |
| `reaffirm_proof.go:65` | F2 | R4 | named | `reaffirmProofOperation (twice)` | "reaffirm-proof" (twice) | A5 reaffirm_proof.go |
| `rejection_rca.go:242` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `review_reclaim.go:112` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `review_reclaim.go:173` | F12 | R1 | adopted | `notFoundEntityAgent` | "agent" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `review_reservation.go:118` | F12 | R1 | adopted | `notFoundEntityAgent` | "agent" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `review_reservation.go:122` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `review_reservation.go:129` | F2 | R1 | adopted | `claimReviewerTaskOperation` | "claim-reviewer-task" | claim_reviewer_task.go (K-OWN) |
| `reviewer_claim_selection.go:137` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `submission_finalize.go:142` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `submission_git_work.go:217` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `submission_git_work.go:386` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `submission_lifecycle.go:26` | F9 | R4 | declaration of named | `submitFinalizeLockOperation` | "submit-for-review-finalize" | A5 submission_lifecycle.go |
| `submission_lifecycle.go:30` | F12 | R3 | declaration of named | `agentIDEnvSuffix` | "AGENT_ID" | A5 submission_lifecycle.go; `brand` owner gap |
| `submission_lifecycle.go:31` | F12 | R3 | declaration of named | `agentGenerationEnvSuffix` | "AGENT_GENERATION" | A5 submission_lifecycle.go; `brand` owner gap |
| `submission_lifecycle.go:95` | F12 | R3 | named | `agentIDEnvSuffix` | "AGENT_ID" | A5 submission_lifecycle.go; `brand` owner gap |
| `submission_lifecycle.go:139` | F9 | R4 | named | `submitFinalizeLockOperation` | "submit-for-review-finalize" | A5 submission_lifecycle.go |
| `submit_progress.go:12` | F9 | R4 | declaration of named | `defaultSubmitHeartbeatInterval` | 5 * time.Minute (initialiser of the test-mutable `submitHeartbeatInterval`) | A5 submit_progress.go |
| `submit_progress.go:16` | F9 | R4 | named | `defaultSubmitHeartbeatInterval` | 5 * time.Minute | A5 submit_progress.go |
| `submit_verdict.go:285` | F2 | R1 | adopted | `payloadschema.SubmitVerdictOperation` | "submit-verdict" | payloadschema (A1) |
| `submit_verdict.go:305` | F12 | R3 | named | `agentIDEnvSuffix` | "AGENT_ID" | A5 submission_lifecycle.go; `brand` owner gap |
| `update_review_commit.go:221` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `validation_preflight.go:329` | F12 | R3 | named | `agentIDEnvSuffix` | "AGENT_ID" | A5 submission_lifecycle.go; `brand` owner gap |
| `validation_preflight.go:330` | F12 | R3 | named | `agentGenerationEnvSuffix` | "AGENT_GENERATION" | A5 submission_lifecycle.go; `brand` owner gap |
| `validation_preflight.go:493` | F6 | R4 | named | `runtimeInputPreflightOperation` | "preflight" | A5 validation_readiness.go |
| `validation_readiness.go:58` | F6 | R4 | declaration of named | `runtimeInputPreflightOperation` | "preflight" | A5 validation_readiness.go |
| `verdict_failure_anomaly.go:112` | F12 | R1 | adopted | `actorSystem` | "system" | helpers.go (A3) |
| `verdict_failure_anomaly.go:129` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |
| `verdict_transition.go:152` | F12 | R1 | adopted | `notFoundEntityTask` | "task" (errors.NotFoundError.Entity) | helpers.go (A3) |

## 3. Classified literals (R5 / F12)

Each row lists every line of one file carrying literals of one class and reason. Classes: **J18** detail, history, payload or hash-material keys (field names; the persisted ones are written here and no A5 decision reads them back by spelling — the keys A5 does read back are named in table 2 or pre-exist in table 3b); **J20** message, format, report or label text, rendered command names, diagnostic field paths and activity-log labels; **J21** emptiness, arity, sign and unit identities and string-operation delimiters; **J22** the data table of test-file naming conventions of other ecosystems (G1.1: it is data about the user's project, not Liza vocabulary, and a configuration field is out of scope). Role names, role pairs, tool and provider names, IDs and user text never appear as literals (F12 below).

| File | Class | Rule | Outcome | Reason | Lines |
|---|---|---|---|---|---|
| `acceptance_allocation_review.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 286,286,309 |
| `acceptance_allocation_review.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 95,98,100,119,143,143,144,200,207,245,250,254,261,277,317,332,336,340 |
| `acceptance_allocation_review.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 31,164,168,168,239 |
| `acceptance_claim_block.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 142,144,149,208,214,219,220,249,265,271 |
| `acceptance_claim_block.go` | J20 | R5 | classified | rendered operator command or flag name in guidance text (command identities are not closed into a type, A3 I22) | 209,214,220,250 |
| `acceptance_claim_block.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 213,245,261,263 |
| `acceptance_claim_block.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 77,105,121,222,222,231 |
| `acceptance_claim_block.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 262 |
| `acceptance_creation.go` | J20 | R5 | classified | diagnostic field or source label named in a refusal (A3 I27: diagnostic fields stay dynamic) | 54,56,108,110 |
| `acceptance_creation.go` | J20 | R5 | classified | diagnostic field path named in an acceptance refusal (A3 I27: diagnostic fields stay dynamic) | 123 |
| `acceptance_creation.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 98,116,117,118 |
| `acceptance_creation.go` | J20 | R5 | classified | rendered operator command or flag name in guidance text (command identities are not closed into a type, A3 I22) | 119 |
| `acceptance_creation.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 88 |
| `acceptance_creation.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 55,89,101,149 |
| `acceptance_declaration.go` | J20 | R5 | classified | diagnostic field path named in an acceptance refusal (A3 I27: diagnostic fields stay dynamic) | 38,124 |
| `acceptance_declaration.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 68,78,83,89,92,117,133,168,174,176,198 |
| `acceptance_declaration.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 72,131,131 |
| `acceptance_declaration.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 48,66,83,135,137,162,173 |
| `acceptance_evidence.go` | J20 | R5 | classified | diagnostic field or source label named in a refusal (A3 I27: diagnostic fields stay dynamic) | 215 |
| `acceptance_evidence.go` | J20 | R5 | classified | diagnostic field path named in an acceptance refusal (A3 I27: diagnostic fields stay dynamic) | 174,256,262,266,273,281,291,295,338,365,378,385,400,411,419,429,448,454,458,469 |
| `acceptance_evidence.go` | J20 | R5 | classified | diagnostic field path suffix named in an acceptance refusal (A3 I27) | 281 |
| `acceptance_evidence.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 78,80,84,86,91,94,96,98,103,107,227,256,262,281,291,295,331,338,341,344,349,351,354,363,363,378,385,400,411,419,429,448,454,458,469 |
| `acceptance_evidence.go` | J20 | R5 | classified | rendered operator command or flag name in guidance text (command identities are not closed into a type, A3 I22) | 78,81,84,86,94,94,94,94,94,96,107 |
| `acceptance_evidence.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 185,253,467 |
| `acceptance_evidence.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 97,170,226,276,294,348,406,406 |
| `acceptance_evidence.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 214,340,447,447,460,468 |
| `acceptance_execution.go` | J20 | R5 | classified | diagnostic field path named in an acceptance refusal (A3 I27: diagnostic fields stay dynamic) | 89 |
| `acceptance_execution.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 46,92,99,121,136,140,151,154,157,204 |
| `acceptance_execution.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 115,205,225,246,255 |
| `acceptance_execution.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 156,220,226,239,245 |
| `acceptance_execution.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 57,91,92,105,198,243,244,259,259 |
| `acceptance_ownership_fence.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 43 |
| `acceptance_reference_reads.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 24,42,47,49,85,153,171,174,177,181,184,188 |
| `acceptance_reference_reads.go` | J21 | R5 | classified | bytes per KiB: unit conversion in a size message | 184 |
| `acceptance_reference_reads.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 19,19,42,47,102,102 |
| `acceptance_reference_reads.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 21,24,52,72,99,104,114,153,153,171,171,174,174,176,177,177,181,181,184,184,188,188 |
| `acceptance_reference_reads.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 128,128,128,128,180 |
| `archive_acceptance_receipts.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 71,72,105 |
| `await_policy.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 18 |
| `await_policy.go` | J20 | R5 | classified | rendered operator command or flag name in guidance text (command identities are not closed into a type, A3 I22) | 19,19 |
| `await_policy.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 16,17,22,33,33,39 |
| `await_resubmission.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 156,159,196,202,206,242,286,340,385,389,393,401,415,420,467 |
| `await_resubmission.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 155,158,201,368 |
| `await_resubmission.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 408,408,473,473 |
| `await_verdict.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 125,128,180,184,257,323,327,341,355,360 |
| `await_verdict.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 124,127,179,438 |
| `await_verdict.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 348,348 |
| `await_verdict_finish.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 79,181,203 |
| `await_verdict_finish.go` | J20 | R5 | classified | rendered operator command or flag name in guidance text (command identities are not closed into a type, A3 I22) | 79 |
| `await_verdict_finish.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 163,169,177,190 |
| `await_verdict_finish.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 109,110,143,147 |
| `await_verdict_outcome.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 42,76,150,175,182,195,205,206,210,212,370,373,381,390,396,402 |
| `await_verdict_outcome.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 208 |
| `await_verdict_outcome.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 209,226,229,254,273,305,308,312,318,322,338,338,338,341,369,382,394 |
| `await_verdict_outcome.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 219,219,246,246,293 |
| `claim_reviewer_task.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 55,61,104,160,165,279,362,368 |
| `claim_reviewer_task.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 54,60,60,103,135,145,278,291,362,368 |
| `claim_reviewer_task.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 111,259,290 |
| `clear_stale_review_claims.go` | J20 | R5 | classified | activity-log action label (log lines are a free surface, goal-frozen) | 136 |
| `clear_stale_review_claims.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 40,65,91,93,109,110,141,151,154,160,163,167,170,175,180,183,191,229,243 |
| `clear_stale_review_claims.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 86,102,148,188,193,217 |
| `clear_stale_review_claims.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 40,43,65,162 |
| `inflight_submit.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 82,87 |
| `inflight_submit.go` | J21 | R5 | classified | directory entry names a marker agent ID must not be | 50,50,53 |
| `inflight_submit.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 50,51 |
| `inflight_submit.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 112 |
| `orphaned_review_base.go` | J20 | R5 | classified | Git branch ref namespace: A2 names no constant; A6 repeats it (listed for A6) | 612 |
| `orphaned_review_base.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 43,83,95,141,226,243,246,257,267,269,271,281,288,295,300,327,343,351,361,388,391,407,426,431,452,460,465,494,497,503,548,589,605,613,623,630,637,647,660,665,691,708 |
| `orphaned_review_base.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 386 |
| `orphaned_review_base.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 140,140,140,140,181,235,274,278,280,285,292,298,302,313,331,333,350,393,407,448,456,477,502,506,556,622 |
| `orphaned_review_base.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 270,294,360,387,636 |
| `pending_verdict.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 75,91,97,106,109,147,182,225,242,251,261,307,310,314,319 |
| `pending_verdict.go` | J20 | R5 | classified | temporary-file name pattern, single use | 186 |
| `pending_verdict.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 62,83,140,147,151,182,184,188,194,208,316,316 |
| `pending_verdict.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 114,115,275,302 |
| `quarantined_verdict.go` | J18 | R5 | classified | detail, history, payload or hash-material key: field name; no A5 decision reads it back by spelling from persisted state | 93,94,95 |
| `quarantined_verdict.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 26,48,64,79,81,84,104,110,120,162,166,197,200,287,314,322,335,348,351,373 |
| `quarantined_verdict.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 58 |
| `quarantined_verdict.go` | J21 | R5 | classified | directory entry names a marker agent ID must not be | 58 |
| `quarantined_verdict.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 47,135,158,233,255,321,333 |
| `quarantined_verdict.go` | J21 | R5 | classified | truncation ellipsis | 141,145 |
| `quarantined_verdict.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 268,269,270,293,294,362,363 |
| `reaffirm_proof.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 48,51,65,65,69,78,82,122,136,140,150,155,159,164,167,184,188,199,204 |
| `reaffirm_proof.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 47,50,76,121,121,184,184,188,188,198,199,199,203,204,204 |
| `rejection_rca.go` | J18 | R5 | classified | detail, history, payload or hash-material key: field name; no A5 decision reads it back by spelling from persisted state | 115,120,121,170,172,173,174,175,176 |
| `rejection_rca.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 44,101,211,214,217,281,297,315,317,319 |
| `rejection_rca.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 210,213,318 |
| `rejection_rca.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 163,295,298,298,327 |
| `repair_acceptance_commits.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 71,77,82,133,143,160,226,239,249,253,260,263,333,334 |
| `repair_acceptance_commits.go` | J20 | R5 | classified | rendered operator command or flag name in guidance text (command identities are not closed into a type, A3 I22) | 160 |
| `repair_acceptance_commits.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 330,333 |
| `repair_acceptance_commits.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 70,210,214,241,262,276,281,374,375,385,386,389,390,396 |
| `repair_acceptance_commits.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 89,106,110,191,203,309,322,327,356,358 |
| `repair_review_ownership.go` | J20 | R5 | classified | activity-log action label (log lines are a free surface, goal-frozen) | 72 |
| `repair_review_ownership.go` | J20 | R5 | classified | default audit reason text | 17 |
| `repair_review_ownership.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 25,68,76,86,94,97,103,108,111,114,117,120,123 |
| `repair_review_ownership.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 16,44,48,60,93,126 |
| `repair_review_ownership.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 25,28,86,122 |
| `review_boundary.go` | J18 | R5 | classified | detail, history, payload or hash-material key: field name; no A5 decision reads it back by spelling from persisted state | 91,92,93,94,95,124,125,126,139,140,141,195,196,197,198,209 |
| `review_boundary.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 48,53,66,80,89,95,115,122,126,137,141,149,150,156,157,163,164,198 |
| `review_reclaim.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 48,156,216 |
| `review_reclaim.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 80 |
| `review_reclaim.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 77,118 |
| `review_reservation.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 98,103,129 |
| `reviewed_range.go` | J20 | R5 | classified | diagnostic field or source label named in a refusal (A3 I27: diagnostic fields stay dynamic) | 182,186 |
| `reviewed_range.go` | J20 | R5 | classified | diagnostic field path named in an acceptance refusal (A3 I27: diagnostic fields stay dynamic) | 193 |
| `reviewed_range.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 53,56,64,149,164,168,173,217,226,230,235,287,296 |
| `reviewed_range.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 55,95,154,181,198,287,287,292,292,292,293,293,295,295,295,296,296,303 |
| `reviewed_range.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 26,66,80,96,104,106,232,233 |
| `reviewer_claim_candidate.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 115 |
| `reviewer_claim_candidate.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 116 |
| `reviewer_claim_candidate.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 109,112 |
| `reviewer_claim_selection.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 74,126,145,166,194,196,197,197,201 |
| `reviewer_claim_selection.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 108,264,307,386,436 |
| `reviewer_claim_selection.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 94,125,205,215,235,251,264,277,307,320,361,361,468 |
| `submission_admission.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 107,142 |
| `submission_admission.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 53,77,135,153 |
| `submission_finalize.go` | J18 | R5 | classified | detail, history, payload or hash-material key: field name; no A5 decision reads it back by spelling from persisted state | 116,117,126,127 |
| `submission_finalize.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 54,92,114,127,158,162,175 |
| `submission_finalize.go` | J20 | R5 | classified | phase label of a classified operational error (diagnostic text; nothing branches on its spelling) | 113 |
| `submission_finalize.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 174 |
| `submission_git_work.go` | J18 | R5 | classified | detail, history, payload or hash-material key: field name; no A5 decision reads it back by spelling from persisted state | 45,46,47,74,75,76,135,136,137,162,163,164,165,182,183,184,283,284,285,303,304,305,346,347,348,349,350,351,352,357,358,402,403,404,412 |
| `submission_git_work.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 33,43,47,56,58,72,76,84,87,133,137,144,160,165,180,184,220,256,267,281,285,301,305,325,337,351,404,425 |
| `submission_git_work.go` | J20 | R5 | classified | phase label of a classified operational error (diagnostic text; nothing branches on its spelling) | 42,71,132,159,179,300 |
| `submission_git_work.go` | J20 | R5 | classified | phase or operation label of a classified operational error (diagnostic text) | 266,280 |
| `submission_git_work.go` | J20 | R5 | classified | query path prefix rendered in guidance text | 281 |
| `submission_git_work.go` | J20 | R5 | classified | rendered operator command or flag name in guidance text (command identities are not closed into a type, A3 I22) | 281 |
| `submission_git_work.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 357 |
| `submission_git_work.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 55,89,142,324,334 |
| `submission_git_work.go` | J21 | R5 | classified | length of the three-dot ellipsis a truncation reserves | 368,371 |
| `submission_git_work.go` | J21 | R5 | classified | truncation ellipsis | 371 |
| `submission_git_work.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 142 |
| `submission_lifecycle.go` | J18 | R5 | classified | detail, history, payload or hash-material key: field name; no A5 decision reads it back by spelling from persisted state | 235 |
| `submission_lifecycle.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 89,92,95,105,113,170,177,178,179,193,241,262 |
| `submission_lifecycle.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 88,91,94,169,192,239 |
| `submission_preconditions.go` | J20 | R5 | classified | message text for an unassigned task; not LifecycleEffectsNone | 156 |
| `submission_preconditions.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 71,107,111,115,129,141,146,160 |
| `submission_preconditions.go` | J20 | R5 | classified | rendered operator command or flag name in guidance text (command identities are not closed into a type, A3 I22) | 146 |
| `submission_preconditions.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 106 |
| `submit_hold.go` | J20 | R5 | classified | file-name suffix of the submit hold, single use | 24 |
| `submit_hold.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 39 |
| `submit_hold.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 23,23 |
| `submit_hold.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 19 |
| `submit_progress.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 78,80,82 |
| `submit_progress.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 75 |
| `submit_review.go` | J18 | R5 | classified | detail, history, payload or hash-material key: field name; no A5 decision reads it back by spelling from persisted state | 52 |
| `submit_review.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 69,124,136,149,157 |
| `submit_review.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 65,70,70,155 |
| `submit_verdict.go` | J18 | R5 | classified | detail, history, payload or hash-material key: field name; no A5 decision reads it back by spelling from persisted state | 257,257,257,257 |
| `submit_verdict.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 178,227,244,282,299,302,305,309,313,317,368,439,447,451,454,463,481 |
| `submit_verdict.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 65,92,109,135,171,171,174,174,178,178,195,203,298,301,304,312,420 |
| `submit_verdict.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 44,45,46,77,80,80,277,283,283 |
| `test_detection.go` | J18 | R5 | classified | detail key of the test-detection evidence record: field name, written here and not read back by spelling | 52,53,54,55,56 |
| `test_detection.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 140,162 |
| `test_detection.go` | J22 | R5 | classified | test-file naming convention of other ecosystems: configurable-by-ecosystem data table, not Liza vocabulary (G1.1) | 11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,76,76,76,76,76,76 |
| `test_detection.go` | J22 | R5 | classified | test-file naming convention of other ecosystems: matcher fragment of the data table (G1.1) | 66,71,71,71,77,77,82,82,89,90,91,91,97,97,102,102,103,103,108,108,109,109,114,114,119,122,124,124,158 |
| `update_review_commit.go` | J20 | R5 | classified | display of an absent commit pointer in message text | 258,262 |
| `update_review_commit.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 51,118,135,139,142,152,166,168,174,177,201,231,267,316 |
| `update_review_commit.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 50,53,134,158,301 |
| `validation_preflight.go` | J20 | R5 | classified | Git revision name passed to Git (git vocabulary; A2 names no constant) | 100,182,306 |
| `validation_preflight.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 52,54,473 |
| `validation_preflight.go` | J21 | R5 | classified | delimiter, cutset, replacement or hex-format selector of a string operation | 76,76,76,76,76,76,76,76,76,76,76,198,243,473 |
| `validation_preflight.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 74,113,140,176,191,270,273,274,301,325,331,398,398,451,507 |
| `validation_preflight.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 167,230,379,382,392,483,487 |
| `validation_readiness.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 130,132,138,142 |
| `validation_readiness.go` | J20 | R5 | classified | rendered operator command or flag name in guidance text (command identities are not closed into a type, A3 I22) | 139,139 |
| `validation_readiness.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 131 |
| `verdict_failure_anomaly.go` | J18 | R5 | classified | detail, history, payload or hash-material key: field name; no A5 decision reads it back by spelling from persisted state | 78,79,143,144,147,150,153 |
| `verdict_failure_anomaly.go` | J20 | R5 | classified | activity-log action label (log lines are a free surface); not models.AnomalyTypeSubmitVerdictFailed | 60 |
| `verdict_failure_anomaly.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 51,53,56,107 |
| `verdict_failure_anomaly.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 48,48,52,55,93,111,118,146,149 |
| `verdict_failure_anomaly.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 103 |
| `verdict_integration_evidence.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 22,25,33,58,77,88,92 |
| `verdict_integration_evidence.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 39,104,107,110 |
| `verdict_transition.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 61,68,71,76,81,84,104,117,131,138,213,235,248,253,491,497,498,511 |
| `verdict_transition.go` | J21 | R5 | classified | empty-string test or return: absence of an optional value | 70,113,226,233,252,273,273,290,300,350,517 |
| `verdict_transition.go` | J21 | R5 | classified | zero, one or two: sign, emptiness, arity, index or doubling identity | 57,133,300 |

### 3b. Pre-existing named constants in A5 files

Named by earlier scopes, by the A5 concern tasks or by A3 inside the A5 files; retained, adopted nowhere twice, no parallel definition. Constants this task declared are in table 2.

| File | Constants |
|---|---|
| `submit_review.go` | `integrationOperationSubmitForReview`, `operationalCodeStateWrite`, `submitReviewCauseLimit` |
| `submission_lifecycle.go` | `submissionSHA1HexLength`, `submissionSHA256HexLength`, `submitTimeoutRequestIDLength` |
| `submit_hold.go` | `submitHoldDigestBytes` |
| `inflight_submit.go` | `submitGraceMargin`, `inflightSubmitNonceBytes`, `MaxInflightSubmitGrace` |
| `submit_verdict.go` | `verdictImpactExtraKey` |
| `pending_verdict.go` | `maxPendingVerdictBytes`, `pendingVerdictDirName`, `pendingVerdictDirMode`, `pendingVerdictRetryInitialDelay`, `pendingVerdictRetryMaxDelay` |
| `pending_verdict_windows.go` | `pendingVerdictRetryBudget`, `pendingVerdictRetryInterval`, `errAccessDenied`, `errSharingViolation` |
| `await_resubmission.go` | `ResubmissionResubmitted`, `ResubmissionTerminal`, `ResubmissionTimeout`, `ResubmissionAborted`, `ResubmissionPoll`, `ResubmissionPaused`, `awaitResubmissionOperation`, `awaitResubmissionReclaimOperation` |
| `await_policy.go` | `AwaitPollIntervalConfigKey` |
| `update_review_commit.go` | `updateReviewCommitOperation`, `oldReviewCommitExtraKey`, `newReviewCommitExtraKey`, `oldBaseCommitExtraKey`, `newBaseCommitExtraKey` |
| `review_boundary.go` | `reviewBoundaryOperationAssignment`, `reviewBoundaryPhase` |
| `orphaned_review_base.go` | `healOrphanedReviewBaseOperation`, `orphanedBaseHealExtraKey`, `preparedByExtraKey`, `OrphanedBaseHealNotApplicable`, `OrphanedBaseHealed`, `OrphanedBaseHealIntegrationFailed` |
| `claim_reviewer_task.go` | `claimReviewerTaskOperation` |
| `acceptance_evidence.go` | `AcceptanceFaultContent`, `AcceptanceFaultAllocation`, `AcceptanceFaultRuntimeInput`, `AcceptanceFaultAdmissionSlot`, `AcceptanceFaultBatchTimeout`, `acceptanceReceiptVersion` |
| `acceptance_execution.go` | `acceptanceOutputLimit`, `acceptanceFailureExcerptLimit`, `defaultAcceptanceTimeoutSeconds`, `acceptanceCommandWaitDelay`, `acceptanceExitUnknown`, `acceptanceMaskedValue`, `acceptanceSecretEntryName` |
| `acceptance_creation.go` | `acceptanceValidationMismatchReason`, `acceptanceDiagnosticSchemaVersion`, `acceptanceCreationCommitLength` |
| `acceptance_claim_block.go` | `AcceptanceAllocationRefusalThreshold`, `acceptanceBlockedReasonLimit`, `acceptanceReasonContentInvalid`, `acceptanceReasonAllocationRepeated`, `claimRefusalExtraKey`, `claimRefusalAcceptanceValue`, `faultClassExtraKey`, `integrationCommitExtraKey`, `runtimeInputHumanAskLimit` |
| `acceptance_ownership_fence.go` | `defaultAcceptanceOwnershipPollInterval` |
| `archive_acceptance_receipts.go` | `defaultArchiveMaxTasks`, `defaultArchiveMaxBytes` |
| `repair_acceptance_commits.go` | `remapReplacedExtraKey`, `remapIntegrationExtraKey`, `remapHistoryEntriesExtraKey` |
| `validation_readiness.go` | `validationCodeRegistrationRequired`, `validationCodeWorktreeUnavailable`, `validationCodePipelineUnavailable`, `validationCodeIntegrationUnavailable`, `validationCodeConfigurationChanged`, `validationCodeCurrentSessionRequired`, `validationCodeRetryPending`, `validationCodeArtifactUnsupported`, `validationCodeExecutionPolicyRequired`, `validationCodeContextChanged`, `validationCodeOwnershipChanged`, `validationCodeCommitChanged`, `validationCodeIntegrationChanged`, `validationCodeCandidateChanged`, `validationCodeExecutableOwnershipRequired`, `validationResultPassed`, `validationResultFailed`, `validationMethodDirect`, `validationExecutionLocal`, `validationExecutionArtifactOnly`, `ValidationPreflightFailedCategory` |
| `submission_git_work.go` | `submitRebaseDiagnosticLimit` |
| `verdict_failure_anomaly.go` | `verdictFailureErrorLimit`, `verdictFailureStackLimit`, `verdictFailureAnomalyErrorLimit` |
| `verdict_transition.go` | `submittedVerdictApproved`, `submittedVerdictRejected`, `rejectionRCAThresholdKey`, `rejectionRCARejectionCountKey` |
| `await_verdict_finish.go` | `awaitBudgetExhaustedReason`, `awaitUnfencedSubmissionIndex`, `DefaultAwaitBudget` |
| `await_verdict_outcome.go` | `VerdictApproved`, `VerdictRejected`, `VerdictNewAttempt`, `VerdictAlreadyTransitioned`, `VerdictTerminal`, `VerdictTimeout`, `VerdictAborted`, `VerdictPoll`, `VerdictPaused`, `SafeActionRevise`, `SafeActionStop` |
| `review_reclaim.go` | `reclaimReviewLeaseDuration` |
| `review_reservation.go` | `reviewOwnershipLeaseMargin` |
| `reviewer_claim_selection.go` | `defaultReviewClaimCooldown` |

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
