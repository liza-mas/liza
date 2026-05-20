# Code Plan: MergeWorktree Artifact Guard Freshness And Backstop

Status: draft

Task: `architecture-3-code-planning-1`

Architecture reference: `specs/arch-plan/20260520-artifact-ref-protection/20260520-115540-architecture-3.md`

Goal spec: `specs/goals/20260520-artifact-ref-protection.md`

## Source Context

Based on:

- Assigned task `architecture-3-code-planning-1` from `liza get architecture-3-code-planning-1 --json`.
- Goal spec `specs/goals/20260520-artifact-ref-protection.md`, especially FR-001-8, FR-001-12 through FR-001-16, FR-001-28, AC-001-1 through AC-001-10, AC-001-15, AC-001-16, NFR-000-1 through NFR-000-5, and NFR-001-2 through NFR-001-3.
- Architecture plan `specs/arch-plan/20260520-artifact-ref-protection/20260520-115540-architecture-3.md`, Scope 2.
- Prior plan `specs/plans/20260520-artifact-ref-protection/20260520-134347-architecture-3-code-planning-0.md` and output JSON for the candidate-tree validator API.
- CAS hook plan `specs/plans/20260520-artifact-ref-protection/20260520-111957-architecture-2-code-planning-0.md`.
- Codebase reads: `internal/ops/wt_merge.go`, `internal/ops/wt_merge_test.go`, and `internal/statevalidate/validate.go`.
- Guardrails: `GUARDRAILS.md` G1.2 and `INVARIANTS.md` sections 5 and 7 for CAS merge and post-merge artifact validation invariants.

ASSUMPTION: `architecture-3-code-planning-0-coding-1` will expose a guard-facing candidate validator equivalent to `statevalidate.ValidateCandidateArtifactRefs(candidateTreeish, refs, lookup)` and `cas-hook-conflict-retry-coding-2` will expose the optional `performCASMerge` hook parameter. This plan declares both as concrete task dependencies.

Doc Impact: Covered by sibling `architecture-4-code-planning-1`; this implementation plan does not add a documentation child task.

Test Impact: Required. The implementation task must add or update `internal/ops/wt_merge_test.go` tests at the `MergeWorktree`/pre-update guard boundary for the scoped functional requirements and acceptance criteria.

## Objective

Wire the candidate artifact validator into `MergeWorktree` as the real `performCASMerge` pre-update hook. The hook must read blackboard state inside every invocation, validate that snapshot's protected artifact refs against the candidate treeish, confirm first failures with one fresh state snapshot against the same candidate, fail closed when confirmation state cannot be read, and leave the post-merge `ValidateArtifactRefs` rollback backstop in place.

## Current Code Shape

- `MergeWorktree` currently calls `performCASMerge(gitWrapper, integrationRef, expectedCommit, taskID)` before syncing merged files and then runs `statevalidate.ValidateArtifactRefs(currentState, projectRoot)`.
- `performCASMerge` currently owns CAS retry behavior and, per sibling CAS hook work, will accept a nil-safe opaque `func(candidateTreeish string) error` hook.
- `statevalidate.ValidateArtifactRefs` currently validates working-tree or integration-branch artifact references after a ref update; this backstop must remain after successful `performCASMerge`.
- Existing `TestMergeWorktree_RejectsDeletingReferencedArtifact` demonstrates the current rollback-backstop behavior and should be updated or supplemented so pre-update rejection becomes the primary assertion while a separate regression keeps AC-001-15 covered.

## Implementation Guidance

Add a small guard-construction helper in `internal/ops/wt_merge.go`, close to `MergeWorktree`, instead of embedding artifact policy inside `performCASMerge`. A suitable shape is:

```go
func buildArtifactGuardHook(bb *db.Blackboard, projectRoot string, gitWrapper *git.Git) func(candidateTreeish string) error
```

The helper can return a closure over `bb`, `projectRoot`, and the Git wrapper. It should not cache collected refs outside the closure invocation.

Inside each hook invocation:

1. Read the latest state snapshot with `bb.Read()`. If this initial read fails, return a fail-closed error because the protected ref set is unknown.
2. Collect artifact refs from that snapshot using the collector API from `artifact-ref-validator-coding-1`.
3. Validate the collected refs against the provided `candidateTreeish` using the validator from `architecture-3-code-planning-0-coding-1`.
4. Return success immediately if validation passes.
5. If collection or candidate validation fails, preserve that first failure and read state exactly one more time for confirmation.
6. If confirmation state cannot be read, return a composite error that preserves the first failure and reports that state freshness could not be verified.
7. Recollect refs from the confirmation snapshot and revalidate the same `candidateTreeish`.
8. Return success if the confirmation snapshot validates. This covers the case where current state no longer protects the path that failed in the first snapshot.
9. If confirmation collection or validation still fails, return the confirmation failure so diagnostics reflect the freshest available state owner provenance.

Use `errors.Join` or another existing project pattern for composite diagnostics only if `errors.Is`/`errors.As` can still find the underlying candidate failure. Error text must include both the candidate artifact failure and the failed state freshness read for FR-001-16 and AC-001-10.

Pass the constructed hook into `performCASMerge` from `MergeWorktree` once `integrationRef` and `gitWrapper` are available:

```go
artifactGuardHook := buildArtifactGuardHook(bb, projectRoot, gitWrapper)
outcome, err := performCASMerge(gitWrapper, integrationRef, expectedCommit, taskID, artifactGuardHook)
```

Do not move or weaken the existing post-merge block:

```go
currentState, err := bb.Read()
...
if err := statevalidate.ValidateArtifactRefs(currentState, projectRoot); err != nil {
    rollbackMergedCommit(...)
    markIntegrationFailedWithDiagnostic(...)
}
```

That block remains the AC-001-15 backstop for races after hook success and for future artifact-ref gaps outside the candidate guard.

## Planned Coding Tasks

### Task 1: MergeWorktree Artifact Guard Freshness And Backstop

desc: Wire `MergeWorktree` to pass a fresh-state artifact guard hook into `performCASMerge` while retaining the post-merge artifact validation backstop.

done_when: `MergeWorktree` constructs and passes a real artifact guard hook to `performCASMerge`; every hook invocation reads the latest blackboard state, collects protected artifact refs from that snapshot, and validates those refs against the provided candidate treeish before `UpdateRef`; any first collection or candidate validation failure triggers exactly one confirmation state read and revalidation of the same candidate; the hook returns success when the confirmation snapshot validates, fails closed with a composite diagnostic when confirmation state cannot be read, returns freshest-state diagnostics when confirmation validation still fails, and the existing post-merge `statevalidate.ValidateArtifactRefs` rollback backstop remains after successful ref update; `internal/ops/wt_merge_test.go` covers FR-001-8, FR-001-12 through FR-001-16, FR-001-28, AC-001-1 through AC-001-10, AC-001-15, and AC-001-16 at the `MergeWorktree`/pre-update guard boundary.

scope: Modify `internal/ops/wt_merge.go` and `internal/ops/wt_merge_test.go`; consume the candidate validator API from `architecture-3-code-planning-0-coding-1`, the collector-backed artifact refs from `artifact-ref-validator-coding-1`, and the `performCASMerge` hook boundary from `cas-hook-conflict-retry-coding-2`. Out of scope: changing collector normalization or provenance internals, changing candidate tree path mode policy in `internal/statevalidate` or `internal/git`, changing `performCASMerge` hook contract mechanics or CAS retry behavior, adding submit-for-review diagnostics, and removing or weakening post-merge `ValidateArtifactRefs`.

spec_ref: specs/goals/20260520-artifact-ref-protection.md

plan_ref: specs/plans/20260520-artifact-ref-protection/20260520-135305-architecture-3-code-planning-1.md

task_depends_on: ["architecture-3-code-planning-0-coding-1", "cas-hook-conflict-retry-coding-2"]

Implementation notes:

- Keep `performCASMerge` artifact-policy agnostic. The only new value it receives from this task is the opaque hook closure.
- The hook may call a small local helper such as `validateCandidateAgainstCurrentState(bb, projectRoot, gitWrapper, candidateTreeish)` to keep the closure readable.
- Treat collector invalid-ref errors as guard failures eligible for one confirmation read, because the first state snapshot may itself be stale.
- Ensure confirmation validates the same candidate treeish; do not ask `performCASMerge` to recompute candidates from inside the guard.
- Candidate rejection should occur before integration ref advancement. Tests should assert integration HEAD remains the pre-merge SHA without relying on rollback as normal control flow.
- Existing rollback tests should be split or supplemented so AC-001-15 proves post-merge `ValidateArtifactRefs` can still roll back after a successful ref update when state changes after hook success.

Test guidance:

- Update or supplement `TestMergeWorktree_RejectsDeletingReferencedArtifact` so true-merge deletion of a protected `arch_ref` is rejected before integration advances.
- Add table coverage for task `plan_ref`, `spec_ref`, `epic_ref`, output `arch_ref`, `plan_ref`, `spec_ref`, `epic_ref`, and `goal.spec_ref` deletion cases.
- Add a fast-forward deletion case that proves the hook validates `expectedCommit` before `UpdateRef`.
- Add a rename case where the old protected path is missing in the candidate tree and rejected.
- Add non-regular replacement cases for directory and symlink at the `MergeWorktree` boundary; submodule/gitlink and unknown-mode classification are owned by the candidate-validator task, so this boundary test can rely on at least one non-regular fixture that is practical in the existing temp repo helpers.
- Add invalid-ref state coverage showing the guard rejects before ref advancement with an actionable collector-backed diagnostic.
- Add a freshness confirmation test where the first snapshot protects a failing path, the confirmation snapshot no longer protects it, and the same candidate validates.
- Add a confirmation-read failure test that proves the returned error contains both the first candidate failure and the state freshness read failure.
- Add a post-merge backstop regression where the pre-update hook passes, state changes before post-merge validation, and `ValidateArtifactRefs` still triggers rollback and `INTEGRATION_FAILED`.
- Assert diagnostics include the invalid path and at least one deterministic owner with task ID and artifact field where applicable.

Validation commands for the coder:

- `go test ./internal/ops -run 'TestMergeWorktree_ArtifactGuard|TestMergeWorktree_RejectsDeletingReferencedArtifact|TestMergeWorktree_PostMergeArtifactBackstop'`
- `go test ./internal/ops ./internal/git ./internal/statevalidate`

## Dependency And Shared-File Audit

| File or package | Task 1 | Dependency required |
|-----------------|--------|---------------------|
| `internal/ops/wt_merge.go` | Builds the artifact guard hook, passes it into `performCASMerge`, and leaves post-merge validation in place. | Existing task dependencies provide the validator API and hook parameter. |
| `internal/ops/wt_merge_test.go` | Adds or updates `MergeWorktree` boundary tests for candidate rejection, freshness confirmation, fail-closed diagnostics, and post-merge backstop retention. | No sibling output shares this plan's files. |
| `internal/statevalidate` | Consumed only through the candidate validator and collector APIs. | `task_depends_on` includes `architecture-3-code-planning-0-coding-1`; transitive dependencies cover collector work. |
| `internal/git` | Consumed through the Git wrapper used by the candidate validator lookup. | `architecture-3-code-planning-0-coding-1` owns validator/Git query mechanics. |

This plan emits one coding task, so there are no sibling output merge-conflict dependencies. The output task depends on `architecture-3-code-planning-0-coding-1` for candidate validation and `cas-hook-conflict-retry-coding-2` for the final `performCASMerge` hook contract.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Validate every protected artifact path against the candidate integration treeish before advancing the integration ref. | Goal spec FR-001-8; NFR-000-1 | Task 1 | Covered |
| 2 | Read the latest available state for each artifact guard hook invocation and collect protected refs from that snapshot. | Goal spec FR-001-12 | Task 1 | Covered |
| 3 | Keep state changes after a hook pass covered by post-merge `ValidateArtifactRefs`. | Goal spec FR-001-13; NFR-000-5 | Task 1 | Covered |
| 4 | Re-read state, recollect refs, and revalidate the same candidate tree after a first guard failure. | Goal spec FR-001-14 | Task 1 | Covered |
| 5 | Return success when the confirmation snapshot no longer protects the failing path and validates against the same candidate. | Goal spec FR-001-15 | Task 1 | Covered |
| 6 | Fail closed with a composite diagnostic when confirmation state cannot be re-read. | Goal spec FR-001-16 | Task 1 | Covered |
| 7 | Retain existing post-merge `ValidateArtifactRefs` validation after successful ref update. | Goal spec FR-001-28; NFR-000-5 | Task 1 | Covered |
| 8 | Keep `performCASMerge` free of blackboard state, artifact refs, and artifact policy. | Goal spec FR-001-19; NFR-000-4; NFR-001-2 | Task 1 | Covered |
| 9 | Preserve existing CAS retry safety by using the sibling hook boundary instead of moving retry ownership into the artifact guard. | Goal spec NFR-000-2; architecture-3 Constraints | Task 1 | Covered |
| 10 | Keep rejection diagnostics deterministic and actionable, including path and owner provenance. | Goal spec NFR-000-3; NFR-001-3 | Task 1 | Covered |
| 11 | Reject true-merge deletion of another task's `arch_ref` before ref advancement. | Goal spec AC-001-1 | Task 1 | Covered |
| 12 | Reject true-merge deletion of task `plan_ref`, `spec_ref`, or `epic_ref` before ref advancement. | Goal spec AC-001-2 | Task 1 | Covered |
| 13 | Reject true-merge deletion of `output[].arch_ref`, `output[].plan_ref`, `output[].spec_ref`, or `output[].epic_ref` before ref advancement. | Goal spec AC-001-3 | Task 1 | Covered |
| 14 | Reject fast-forward deletion of a protected artifact before ref advancement. | Goal spec AC-001-4 | Task 1 | Covered |
| 15 | Reject deletion of `goal.spec_ref` before ref advancement. | Goal spec AC-001-5 | Task 1 | Covered |
| 16 | Treat rename without state rewrite as old protected path missing and reject it. | Goal spec AC-001-6 | Task 1 | Covered |
| 17 | Reject directory, symlink, and practical non-regular replacement cases at the `MergeWorktree` boundary. | Goal spec AC-001-7 | Task 1 | Covered |
| 18 | Reject invalid artifact refs that cannot be safely checked against the candidate tree with an actionable diagnostic. | Goal spec AC-001-8 | Task 1 | Covered |
| 19 | Return success for a first failing candidate when the second snapshot no longer protects the failing path and validates. | Goal spec AC-001-9 | Task 1 | Covered |
| 20 | Preserve candidate failure and failed state freshness check when confirmation state cannot be re-read. | Goal spec AC-001-10 | Task 1 | Covered |
| 21 | Keep post-merge `ValidateArtifactRefs` able to roll back and mark `INTEGRATION_FAILED` after a successful ref update. | Goal spec AC-001-15 | Task 1 | Covered |
| 22 | Candidate artifact rejection diagnostics name the invalid path and at least one deterministic owner, including task ID and artifact field where applicable. | Goal spec AC-001-16 | Task 1 | Covered |
| 23 | Initial state read failure in the guard fails closed because protected refs cannot be established. | Architecture-3 "Artifact Guard Hook -> Blackboard State" invariant | Task 1 | Covered |
| 24 | Hook success is scoped to one invocation; no refs are cached across CAS attempts. | Architecture-3 Constraints and Decision 3 | Task 1 | Covered |
| 25 | Collector normalization/provenance internals, Git tree mode policy, CAS hook mechanics, and submit-for-review diagnostics remain outside this scope. | Assigned task scope and architecture-3 Scope 2 boundary | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | Covered by sibling `architecture-4-code-planning-0`; this plan covers the implementation-boundary tests assigned to Scope 2. | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | Covered by sibling `architecture-4-code-planning-1`; this plan does not add user-facing docs. | Covered |

## Pre-Submit Self-Check

- Task decomposition: one output task with one behavior intent, wiring the real `MergeWorktree` artifact guard. Tests are colocated with the behavior change.
- Dependency order: the output task depends on `architecture-3-code-planning-0-coding-1` for the candidate validator API and `cas-hook-conflict-retry-coding-2` for the finalized CAS hook boundary.
- Output parity: Task 1 maps to output[0]. The structured output JSON copies `desc`, `done_when`, `scope`, `spec_ref`, `plan_ref`, and `task_depends_on` verbatim from the task section.
- Shared-file audit: only one output task is emitted, so no sibling output can concurrently modify `internal/ops/wt_merge.go` or `internal/ops/wt_merge_test.go` from this plan.
- Scope boundary: no task plans collector normalization/provenance internals, Git tree path mode policy, `performCASMerge` hook mechanics, CAS retry behavior, submit-for-review diagnostics, or removal of post-merge artifact validation.
- Guardrails: G1.2 is addressed by preserving CAS ownership, state mutation boundaries, and the rollback artifact-validation invariant.
