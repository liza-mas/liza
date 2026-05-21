# Code Plan: Review Submission and Reviewer Recovery Indexing

Task ID: `architecture-2-code-planning-2`

Source artifacts:
- Goal spec: `specs/goals/20260517-use-scip-search.md`
- Architecture reference: `specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md`
- Upstream runtime service plan: `specs/plans/20260517-use-scip-search/20260521-073929-architecture-2-code-planning-0.md`

## Intent

Plan review-boundary lifecycle wiring so `submit-for-review` and recovered reviewer worktrees refresh task-local SCIP indexes from the review candidate, keep indexing failures warning-only, expose only successful indexes to prompt data, and preserve clean task worktrees.

## Source Basis

Based on:
- `specs/goals/20260517-use-scip-search.md`
- `specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md`
- `specs/plans/20260517-use-scip-search/20260521-073929-architecture-2-code-planning-0.md`
- `internal/ops/submit_review.go`
- `cmd/liza/cmd_review.go`
- `internal/commands/submit_review.go`
- `internal/agent/worktree_check.go`
- `internal/agent/strategy_reviewer.go`
- `internal/scipsearch/scipsearch.go`
- `INVARIANTS.md`
- `specs/architecture/ADR/README.md`

No assumptions are required.

## Planned Coding Tasks

### Task 1: Submit-for-Review SCIP Index Regeneration and Warning Surface

**desc:** Submit-for-review SCIP index regeneration and warning surface: wire the runtime indexing service into `ops.SubmitForReview` after successful rebase and review-boundary validation but before the submitted-state transition; add submit result warnings and CLI JSON/non-JSON warning propagation so failed indexers are observable without failing submission; test post-rebase regeneration, warning-only failure, omitted failed-language availability, and clean git status. Out of scope: reviewer recovery refresh, initial task claim indexing, orchestrator refresh, prompt wording, README, and Claude settings.

**done_when:** Submit-for-review tests prove a successful submission refreshes enabled detected task-local SCIP indexes from the post-rebase review candidate under `<worktree>/.liza/scip/` before the submitted-state transition is committed, failed indexers appear in `SubmitForReviewResult.Warnings` and `liza submit-for-review --json` warnings while the task still reaches the submitted status, failed languages are absent from available prompt indexes, and regenerated indexes leave `git status --porcelain` clean.

**scope:** In scope: `internal/ops/submit_review.go` wiring to the runtime scip-search refresh service, submit warning fields/accessors, `cmd/liza/cmd_review.go` JSON warning propagation, `internal/commands/submit_review.go` non-JSON warning output, and focused tests in `internal/ops/submit_review_test.go`, `cmd/liza/cmd_review_test.go`, `cmd/liza/json_wiring_test.go`, or equivalent existing submit-for-review test files. Out of scope: `internal/agent/worktree_check.go`, `internal/agent/strategy_reviewer.go`, task claim/create indexing, orchestrator refresh, prompt wording, README, Claude settings, automatic indexer installation, and `.liza/agent-outputs/`.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md#submit-for-review-regeneration-internalopssubmit_reviewgo-cmdliza-cmd_reviewgo

**task_depends_on:** architecture-2-code-planning-0

### Task 2: Reviewer Worktree Recovery SCIP Refresh

**desc:** Reviewer worktree recovery SCIP refresh: wire the runtime indexing service into recovered reviewer worktrees after branch attach and optional `PostWorktreeCmd`, keep recovery indexing failures warning-only through logs without blocking reviewer claim recovery, and test recovered-worktree refresh, post-worktree ordering, omitted failed-language availability, and clean git status. Out of scope: submit-for-review regeneration, initial task claim indexing, orchestrator refresh, prompt wording, README, and Claude settings.

**done_when:** Reviewer recovery tests prove a missing reviewer worktree recreated from the task branch runs optional `PostWorktreeCmd` before refreshing enabled detected task-local SCIP indexes under `<worktree>/.liza/scip/`, an existing reviewer worktree is not redundantly refreshed by recovery, failed recovery indexers are logged as warnings without blocking `ensureReviewerWorktree`, failed languages are absent from available prompt indexes, and regenerated indexes leave `git status --porcelain` clean.

**scope:** In scope: `internal/agent/worktree_check.go` reviewer recovery wiring to the runtime scip-search refresh service after branch attach and post-worktree setup, warning-only logging for recovery indexing failures, and focused tests in `internal/agent/worktree_check_test.go` or equivalent reviewer recovery tests using git-backed worktrees and fake indexers. Out of scope: `internal/agent/strategy_reviewer.go`, `internal/ops/submit_review.go`, `cmd/liza/cmd_review.go`, task claim/create indexing, orchestrator refresh, prompt wording, README, Claude settings, automatic indexer installation, and `.liza/agent-outputs/`.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md#reviewer-worktree-recovery-indexing-internalagentworktree_checkgo

**task_depends_on:** architecture-2-code-planning-0

## Dependency Graph

Task 1 and Task 2 can run in parallel after `task_depends_on: architecture-2-code-planning-0` is satisfied.

No sibling dependency is required between Task 1 and Task 2 because they modify disjoint production files and disjoint primary test files. Both output entries carry `task_depends_on: ["architecture-2-code-planning-0"]` to encode their upstream runtime scip-search service dependency.

## Shared-File Audit

| File/Area | Task 1 | Task 2 | Ordering |
|---|---|---|---|
| `internal/scipsearch` runtime service | consume refresh and available-index APIs | consume refresh and available-index APIs | `task_depends_on: ["architecture-2-code-planning-0"]` |
| `internal/ops/submit_review.go` | call refresh after rebase/review-boundary validation and before state transition; add result warnings | none | none |
| `cmd/liza/cmd_review.go` | pass submit warnings to JSON envelope | none | none |
| `internal/commands/submit_review.go` | print non-JSON submit warnings | none | none |
| `internal/ops/submit_review_test.go` and submit CLI tests | submit regeneration, warning, available-index, clean-status coverage | none | none |
| `internal/agent/worktree_check.go` | none | refresh recovered reviewer worktree after branch attach and post-worktree setup | none |
| `internal/agent/worktree_check_test.go` | none | recovery refresh, warning-only failure, existing-worktree no-op, available-index, clean-status coverage | none |
| `internal/agent/strategy_reviewer.go` | out of scope | out of scope; existing claim flow calls `ensureReviewerWorktree` before prompt construction | no task may edit it in this plan |
| `.liza/agent-outputs/` | no task | no task | out of scope |

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | `submit-for-review` regenerates task-worktree indexes after successful rebase and review-boundary validation. | Architecture: Submit-for-Review Regeneration key decisions; Success Criteria 14 | Task 1 | Covered |
| 2 | Submit regeneration occurs before the task is made reviewable and before reviewer agents can spawn. | Architecture: Submit-for-Review Regeneration responsibility and data flow | Task 1 | Covered |
| 3 | Submit regeneration captures the final post-rebase review candidate, not the pre-rebase worktree state. | Architecture: Submit-for-Review Regeneration key decisions; assigned done_when | Task 1 | Covered |
| 4 | Submit indexing failures are warning-only and do not block the submitted-state transition. | Behavioral Decisions; Architecture error handling concern; assigned done_when | Task 1 | Covered |
| 5 | Submit warnings are observable through command output or JSON response. | Goal Required Agent Prompt Contract failure observability; Architecture Submit-for-Review Regeneration boundaries | Task 1 | Covered |
| 6 | Submit-generated indexes remain under `<worktree>/.liza/scip/`. | Index Storage; Architecture Runtime SCIP Indexing Service; assigned done_when | Task 1 | Covered |
| 7 | Submit-regenerated indexes do not dirty `git status --porcelain`. | Index Storage; Success Criteria 17; assigned done_when | Task 1 | Covered |
| 8 | Failed submit index languages are absent from available prompt indexes. | Required Agent Prompt Contract; Success Criteria 23; assigned done_when | Task 1 | Covered |
| 9 | Reviewer recovery refreshes indexes only when a missing reviewer worktree is reattached from the task branch. | Architecture: Reviewer Worktree Recovery Indexing key decisions | Task 2 | Covered |
| 10 | Reviewer recovery runs optional `PostWorktreeCmd` before refreshing SCIP indexes. | Architecture: Reviewer Worktree Recovery Indexing key decisions | Task 2 | Covered |
| 11 | Existing reviewer worktrees rely on submit-for-review as the authoritative refresh point and are not redundantly refreshed by recovery. | Architecture: Reviewer Worktree Recovery Indexing key decisions | Task 2 | Covered |
| 12 | Recovery indexing failures are warning-only and do not block recoverable reviewer worktrees. | Behavioral Decisions; Architecture Reviewer Worktree Recovery Indexing key decisions; assigned done_when | Task 2 | Covered |
| 13 | Recovery-generated indexes remain under `<worktree>/.liza/scip/`. | Index Storage; Architecture Reviewer Worktree Recovery Indexing; assigned done_when | Task 2 | Covered |
| 14 | Recovery-regenerated indexes do not dirty `git status --porcelain`. | Index Storage; Success Criteria 17; assigned done_when | Task 2 | Covered |
| 15 | Failed recovery index languages are absent from available prompt indexes. | Required Agent Prompt Contract; Success Criteria 23; assigned done_when | Task 2 | Covered |
| 16 | Review submission and recovery wiring remain stack-agnostic and avoid Liza-specific project commands. | GUARDRAILS.md G1.1; Architecture Constraints | Task 1, Task 2 | Covered |
| 17 | Review submission and recovery preserve state/review-flow invariants by keeping existing blocking behavior for rebase, review-boundary, branch, worktree-health, and state-transition failures. | INVARIANTS.md §§5, 6, 7, 8, 10; Architecture Constraints | Task 1, Task 2 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | Task 1 covers submit-for-review through ops and CLI JSON/non-JSON surfaces; Task 2 covers reviewer recovery through the reviewer recovery lifecycle before prompt construction. | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: operator documentation is covered by merged `architecture-4-code-planning-0`; this assigned scope explicitly excludes README, prompt wording, and Claude settings. | N/A |

## Validation Plan

Each coding task should validate its own behavioral surface with focused Go tests:

- Task 1: `go test ./internal/ops -run 'TestSubmitForReview_.*Scip|TestSubmitForReview_.*Index'`, plus the relevant submit CLI package test such as `go test ./cmd/liza -run 'Test.*SubmitForReview.*(Scip|Warning|JSON)'`.
- Task 2: `go test ./internal/agent -run 'TestEnsureReviewerWorktree_.*(Scip|Index|Recovery)'`.

The final submit/recovery implementation should also run the full touched packages (`go test ./internal/ops ./internal/agent ./cmd/liza`) and pre-commit on touched files. Per the Liza worktree build lesson, use `make test` if broad Go validation trips embedded-asset consistency failures.

## Pre-Submit Self-Check

- Task decomposition: two coding tasks, each with one observable lifecycle intent.
- Output dependency parity: no sibling `depends_on` is needed because Task 1 and Task 2 do not share production files; both output entries include `task_depends_on: ["architecture-2-code-planning-0"]`.
- Shared-file audit: every file appearing in both task considerations is consume-only or explicitly owned by one task.
- Scope boundaries: no task claim/create indexing, orchestrator refresh, prompt wording, README, Claude settings, automatic tool installation, or `.liza/agent-outputs/` changes are planned.
- Cross-references: every responsibility named in the compliance matrix is owned by a task heading above.
