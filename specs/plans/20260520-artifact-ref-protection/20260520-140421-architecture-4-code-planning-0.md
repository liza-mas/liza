# Code Plan: Candidate-Tree Artifact Guard Acceptance Matrix

Status: draft

Task: `architecture-4-code-planning-0`

Architecture reference: `specs/arch-plan/20260520-artifact-ref-protection/20260520-121639-architecture-4.md`

Goal spec: `specs/goals/20260520-artifact-ref-protection.md`

## Source Context

Based on:

- Assigned task `architecture-4-code-planning-0` from `liza get architecture-4-code-planning-0 --json`.
- Goal spec `specs/goals/20260520-artifact-ref-protection.md`, read with line numbers.
- Architecture plan `specs/arch-plan/20260520-artifact-ref-protection/20260520-121639-architecture-4.md`, especially the acceptance matrix, boundary regression, and decomposition sections.
- Prior code plans `specs/plans/20260520-artifact-ref-protection/20260520-134347-architecture-3-code-planning-0.md` and `specs/plans/20260520-artifact-ref-protection/20260520-135305-architecture-3-code-planning-1.md`.
- Current task graph reads for production dependencies: `artifact-ref-collector-coding-0`, `artifact-ref-validator-coding-1`, `cas-preupdate-hook-coding-0`, `cas-hook-staleness-coding-1`, `cas-hook-conflict-retry-coding-2`, `architecture-3-code-planning-0-coding-0`, `architecture-3-code-planning-0-coding-1`, and `architecture-3-code-planning-1-coding-0`.
- Codebase reads: `internal/ops/wt_merge.go`, targeted `internal/ops/wt_merge_test.go` sections, `internal/statevalidate/validate.go`, and `internal/git/query.go`.
- Guardrails: `GUARDRAILS.md` G1.2, `INVARIANTS.md` sections 5 and 7, `INVARIANTS.md` Protection Matrix, and `specs/architecture/ADR/README.md`.

ASSUMPTION: The production coding tasks named in `task_depends_on` will expose the collector, candidate validator, Git tree mode query, CAS hook behavior, and `MergeWorktree` artifact guard described by the approved sibling plans. This plan treats those task IDs as explicit implementation dependencies, not as hidden design authority.

Doc Impact: N/A for this task. Documentation edits are out of scope here and are owned by sibling planning task `architecture-4-code-planning-1`.

Test Impact: Required. This plan emits test-only coding tasks that add or update acceptance and regression tests in `internal/ops`, `internal/statevalidate`, and `internal/git`.

## Objective

Close the feature with an acceptance matrix that proves candidate-tree artifact reference protection works at the observable boundaries:

- `MergeWorktree` rejects invalid candidate trees before the integration ref advances.
- `performCASMerge` retains opaque hook and CAS retry semantics.
- Boundary tests prove the collector, candidate validator, Git tree mode query, and diagnostics cover cases that would be too expensive or ambiguous to repeat end to end.
- The post-merge `ValidateArtifactRefs` rollback path remains tested as a backstop after successful ref updates.

## Planning Decisions

### Decision 1: Keep End-To-End Assertions At `MergeWorktree`

Rejected-candidate acceptance tests should call `MergeWorktree`, capture `integration` HEAD before the call, and assert it is byte-for-byte unchanged afterward. A rolled-back ref can have the same final SHA, so these tests must also assert task state and diagnostics indicate pre-update artifact rejection rather than the historical post-merge rollback path.

### Decision 2: Split Ops Tests By Behavioral Risk, Not By Requirement Number

Protected-field/path-mutation cases and freshness/CAS/backstop cases both live in `internal/ops/wt_merge_test.go`, so Task 2 depends on Task 1 to avoid concurrent edits to the same test file. They are separated because the first task exercises artifact owner and candidate path matrices, while the second exercises temporal and CAS behavior.

### Decision 3: Use Boundary Tests For Exhaustive Mode And Diagnostic Matrices

`MergeWorktree` tests should sample invalid object replacements and invalid refs to prove wiring. Exhaustive object-mode, invalid-ref, owner-provenance, normalization, and deterministic diagnostic assertions belong in `internal/git` and `internal/statevalidate` tests after the production boundary tasks land.

## Planned Coding Tasks

### Task 1: MergeWorktree Protected Owner And Path Mutation Acceptance

desc: Add MergeWorktree acceptance coverage for protected artifact owners and candidate path mutations.

done_when: `internal/ops/wt_merge_test.go` contains table-driven `MergeWorktree` acceptance tests that cover true-merge deletion of another task's `arch_ref`, task `plan_ref`, `spec_ref`, and `epic_ref`, output `arch_ref`, `plan_ref`, `spec_ref`, and `epic_ref`, `goal.spec_ref` deletion or removal, fast-forward deletion of a protected artifact, rename of a protected artifact without state rewrite, directory and symlink replacement plus at least one practical non-regular replacement, and one invalid-ref rejection; each rejected candidate captures the integration ref before `MergeWorktree`, asserts the ref is unchanged afterward, and asserts deterministic diagnostics name the invalid path plus owner provenance with task ID, artifact field, and output index where applicable.

scope: Modify `internal/ops/wt_merge_test.go` only; reuse existing temp Git repository, state fixture, and `MergeWorktree` helpers or add local test helpers in that file when they reduce duplicated setup for the acceptance matrix. Out of scope: production code, `performCASMerge` hook mechanics, guard freshness and CAS retry tests owned by Task 2, exhaustive `internal/git` or `internal/statevalidate` boundary matrices owned by Task 3, documentation edits, and submit-for-review diagnostics.

spec_ref: specs/goals/20260520-artifact-ref-protection.md

plan_ref: specs/plans/20260520-artifact-ref-protection/20260520-140421-architecture-4-code-planning-0.md

task_depends_on: ["architecture-3-code-planning-1-coding-0"]

Implementation notes:

- Update the existing `TestMergeWorktree_RejectsDeletingReferencedArtifact` coverage so the primary assertion is pre-update rejection, not post-merge rollback.
- Use a helper that can vary owner field, task ID, output index, mutation type, and merge shape while preserving explicit assertions per table row.
- For the rename case, use `git mv` or equivalent in the task worktree and assert the old protected path is missing in the candidate tree.
- For invalid-ref wiring, pick one representative collector-invalid ref; Task 3 owns the exhaustive invalid-ref matrix.
- Do not rely only on final working-tree contents. The acceptance invariant is integration ref non-advancement for rejected candidates.

Validation commands for the coder:

- `go test ./internal/ops -run 'TestMergeWorktree_ArtifactGuardProtectedOwners|TestMergeWorktree_RejectsDeletingReferencedArtifact'`
- `go test ./internal/ops`

### Task 2: Guard Freshness, CAS Retry, No-Op, And Backstop Regression Coverage

desc: Add MergeWorktree and performCASMerge regression coverage for guard freshness, CAS retry, no-op, and post-merge backstop behavior.

done_when: `internal/ops/wt_merge_test.go` contains tests proving candidate validation failure is confirmed against exactly one fresh state snapshot, the hook returns success when the confirmation snapshot no longer protects the failing path and validates the same candidate, confirmation state read failure returns an error preserving both the candidate failure and freshness failure, hook failure after a changed integration HEAD is treated as stale and retried, hook failure with unreadable integration HEAD preserves both hook and staleness-read failures, a hook that passes before an `UpdateRef` CAS conflict is re-run against the recomputed retry candidate, the already-merged no-op path does not invoke the hook, `performCASMerge` remains artifact-policy agnostic by asserting only opaque hook behavior at the CAS boundary, and a successful ref update still runs post-merge `ValidateArtifactRefs` so rollback and `INTEGRATION_FAILED` remain available when full-state artifact validation fails afterward.

scope: Modify `internal/ops/wt_merge_test.go` only; extend existing CAS retry, hook, and rollback tests or add focused tests in that file. Out of scope: production code, protected owner/path mutation matrix owned by Task 1, exhaustive collector/validator/Git boundary tests owned by Task 3, documentation edits, and changing retry limits or rollback semantics.

spec_ref: specs/goals/20260520-artifact-ref-protection.md

plan_ref: specs/plans/20260520-artifact-ref-protection/20260520-140421-architecture-4-code-planning-0.md

depends_on: ["0"]

task_depends_on: ["cas-preupdate-hook-coding-0", "cas-hook-staleness-coding-1", "cas-hook-conflict-retry-coding-2", "architecture-3-code-planning-1-coding-0"]

Implementation notes:

- Build on `TestMergeWorktree_CASRetryDeterministic` or its post-production successor for CAS conflict assertions.
- Prefer hook spies that record candidate treeish values and call order; do not inspect artifact error types inside `performCASMerge` tests.
- The freshness-confirmation tests should operate through the guard hook as wired by `MergeWorktree` unless the implementation exposes a package-local helper specifically for this behavior.
- The post-merge backstop regression should force the pre-update hook to pass, then make `ValidateArtifactRefs` fail after successful ref update; assert rollback path and `INTEGRATION_FAILED` diagnostic are still reachable.

Validation commands for the coder:

- `go test ./internal/ops -run 'TestMergeWorktree_ArtifactGuardFreshness|TestPerformCASMerge_PreUpdateHook|TestMergeWorktree_PostMergeArtifactBackstop|TestMergeWorktree_CASRetryDeterministic'`
- `go test ./internal/ops`

### Task 3: Boundary Artifact Ref Matrix And Deterministic Diagnostics Coverage

desc: Add boundary coverage audit tests for artifact-ref owner provenance, invalid refs, Git object modes, and deterministic diagnostics.

done_when: `internal/statevalidate` and `internal/git` tests contain a requirement-mapped boundary matrix proving collector output covers goal, task, and output artifact owners with normalized repo-relative fragment-free paths and deterministic owner ordering; invalid refs fail closed for semicolon-joined refs, empty paths after fragment stripping, traversal outside the repository, and unsafe absolute refs while valid absolute refs under `projectRoot` normalize or are rejected with actionable diagnostics as implemented; Git tree path mode tests distinguish missing paths from present regular files `100644`, executable files `100755`, directories `040000`, submodules/gitlinks `160000` where feasible, symlinks `120000`, and unknown non-regular modes; candidate validator tests accept only `100644` and `100755`, reject every missing or non-regular mode, propagate collector invalid-ref failures, and assert deterministic diagnostics include invalid path, cause, mode when present, and owner provenance without relying on map iteration order.

scope: Modify or add tests under `internal/statevalidate` and `internal/git`, including files such as `internal/statevalidate/artifact_refs_test.go`, `internal/statevalidate/candidate_artifact_refs_test.go`, `internal/statevalidate/validate_specref_test.go`, and `internal/git/query_test.go` as needed. Out of scope: production code, `internal/ops/wt_merge_test.go`, `MergeWorktree` hook wiring, CAS retry behavior, post-merge rollback behavior, documentation edits, and changing artifact-ref semantics beyond asserting the approved implementation contracts.

spec_ref: specs/goals/20260520-artifact-ref-protection.md

plan_ref: specs/plans/20260520-artifact-ref-protection/20260520-140421-architecture-4-code-planning-0.md

task_depends_on: ["artifact-ref-collector-coding-0", "artifact-ref-validator-coding-1", "architecture-3-code-planning-0-coding-0", "architecture-3-code-planning-0-coding-1"]

Implementation notes:

- Reuse the production test files created by dependency tasks when they exist; add missing matrix rows instead of duplicating equivalent assertions.
- For submodules/gitlinks, prefer the lightweight `git update-index --cacheinfo 160000,<commit>,<path>` fixture already planned by `architecture-3-code-planning-0`.
- For unknown modes that are impractical to create through Git, use the candidate validator's fake lookup boundary.
- Diagnostics assertions should prefer structured safe details when available, with stable string substrings only for human-readable context.

Validation commands for the coder:

- `go test ./internal/statevalidate -run 'TestCollectArtifactRefs|TestValidateCandidateArtifactRefs|TestCandidateArtifactRef|TestValidateArtifactRefs'`
- `go test ./internal/git -run 'TestTreePathMode|TestCalculateDrift|TestGetWorktreeHEAD'`
- `go test ./internal/git ./internal/statevalidate`

## Dependency And Shared-File Audit

| File or package | Task 1 | Task 2 | Task 3 | Dependency required |
|-----------------|--------|--------|--------|---------------------|
| `internal/ops/wt_merge_test.go` | Adds protected owner/path mutation acceptance tests | Adds freshness, CAS, no-op, and backstop tests | No planned writes | Yes: Task 2 depends on output[0] because both write the same file |
| `internal/statevalidate` tests | No planned writes | No planned writes | Adds collector, candidate validator, invalid-ref, owner, and diagnostic matrix tests | No sibling dependency |
| `internal/git/query_test.go` | No planned writes | No planned writes | Adds or completes Git tree mode matrix tests | No sibling dependency |

Task 1 depends on `architecture-3-code-planning-1-coding-0` because it tests the wired `MergeWorktree` guard. Task 2 depends on Task 1 for shared-file ordering and on the CAS and guard production tasks. Task 3 depends on collector, post-merge validator, Git query, and candidate validator production tasks. No task modifies production code.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| FR-001-1 | Expose reusable `statevalidate` artifact-ref collector. | Goal spec lines 116-117 | Task 3 | Covered |
| FR-001-2 | Collector returns normalized repo-relative paths with fragments stripped. | Goal spec lines 118-119 | Task 3 | Covered |
| FR-001-3 | Collector returns owner provenance with field, task ID, and output index where applicable. | Goal spec lines 120-121 | Task 3 | Covered |
| FR-001-4 | Collector output is deterministic, sorted by path and owner metadata. | Goal spec lines 122-123 | Task 3 | Covered |
| FR-001-5 | `ValidateArtifactRefs` uses collector instead of duplicate traversal. | Goal spec lines 124-125 | Task 3 | Covered |
| FR-001-6 | Collector fails closed for unsafe invalid refs. | Goal spec lines 126-129 | Task 3 | Covered |
| FR-001-7 | Existing valid absolute refs normalize to repo-relative paths or are rejected with actionable diagnostics. | Goal spec lines 130-132 | Task 3 | Covered |
| FR-001-8 | Validate every protected artifact path against candidate treeish before advancing integration ref. | Goal spec lines 133-134 | Task 1, Task 2 | Covered |
| FR-001-9 | Candidate validation requires regular Git file mode `100644` or `100755`. | Goal spec lines 135-136 | Task 3 | Covered |
| FR-001-10 | Candidate validation rejects missing paths and non-regular Git object modes. | Goal spec lines 137-139 | Task 1, Task 3 | Covered |
| FR-001-11 | Rejection diagnostics include invalid path and deterministic owner provenance. | Goal spec lines 140-141 | Task 1, Task 3 | Covered |
| FR-001-12 | Artifact guard reads latest available state for each hook invocation. | Goal spec lines 142-143 | Task 2 | Covered |
| FR-001-13 | State changes after hook pass remain covered by post-merge `ValidateArtifactRefs`. | Goal spec lines 144-145 | Task 2 | Covered |
| FR-001-14 | First candidate-tree failure triggers state re-read, recollect, and same-candidate revalidation. | Goal spec lines 146-148 | Task 2 | Covered |
| FR-001-15 | Second snapshot no longer protecting failing path allows hook success. | Goal spec lines 149-151 | Task 2 | Covered |
| FR-001-16 | State re-read failure after candidate failure fails closed with composite diagnostic. | Goal spec lines 152-154 | Task 2 | Covered |
| FR-001-17 | `performCASMerge` accepts optional narrow pre-update hook. | Goal spec lines 155-156 | Task 2 | Covered |
| FR-001-18 | Pre-update hook runs inside each CAS attempt after candidate treeish computation and before `UpdateRef`. | Goal spec lines 157-159 | Task 2 | Covered |
| FR-001-19 | `performCASMerge` treats hook as opaque and owns CAS retry semantics. | Goal spec lines 160-163 | Task 2 | Covered |
| FR-001-20 | Pre-update hook is skipped for already-merged no-op path. | Goal spec lines 164-165 | Task 2 | Covered |
| FR-001-21 | Fast-forward merges validate `expectedCommit` before `UpdateRef`. | Goal spec lines 166-167 | Task 1, Task 2 | Covered |
| FR-001-22 | True merges validate candidate merge tree or merge commit tree before `UpdateRef`. | Goal spec lines 168-169 | Task 1, Task 2 | Covered |
| FR-001-23 | Hook failure causes `performCASMerge` to re-read integration ref before returning hook error. | Goal spec lines 170-171 | Task 2 | Covered |
| FR-001-24 | Changed integration ref after hook failure discards stale hook error and retries. | Goal spec lines 172-173 | Task 2 | Covered |
| FR-001-25 | Unchanged integration ref after hook failure returns hook error. | Goal spec lines 174-175 | Task 2 | Covered |
| FR-001-26 | Integration ref re-read failure after hook failure preserves hook failure and reports staleness check failure. | Goal spec lines 176-178 | Task 2 | Covered |
| FR-001-27 | Hook pass followed by `UpdateRef` CAS conflict retries and re-runs hook against recomputed candidate. | Goal spec lines 179-181 | Task 2 | Covered |
| FR-001-28 | Existing post-merge `ValidateArtifactRefs` validation remains after successful ref update. | Goal spec lines 182-183 | Task 2 | Covered |
| FR-001-29 | `submit-for-review` checks, if added, remain optional diagnostics rather than authoritative guard. | Goal spec lines 184-185 | Task 1, Task 2 | Covered |
| NFR-000-1 | Guard runs before integration ref advancement for normal merge paths. | Goal spec lines 53-54 | Task 1, Task 2 | Covered |
| NFR-000-2 | Preserve existing CAS retry safety for concurrent merges. | Goal spec lines 55-56 | Task 2 | Covered |
| NFR-000-3 | Diagnostics are deterministic and actionable with path and owner provenance. | Goal spec lines 57-58 | Task 1, Task 3 | Covered |
| NFR-000-4 | `performCASMerge` does not depend on blackboard state or artifact-ref semantics. | Goal spec lines 59-60 | Task 2 | Covered |
| NFR-000-5 | Existing post-merge `ValidateArtifactRefs` backstop remains in place. | Goal spec lines 61-62 | Task 2 | Covered |
| NFR-001-1 | Candidate checks use Git tree mode inspection that distinguishes missing from non-regular modes. | Goal spec lines 189-191 | Task 3 | Covered |
| NFR-001-2 | `performCASMerge` does not know blackboard state, artifact provenance, or validation policy. | Goal spec lines 192-193 | Task 2 | Covered |
| NFR-001-3 | Error messages are stable enough for tests without map iteration dependence. | Goal spec lines 194-195 | Task 1, Task 3 | Covered |
| AC-001-1 | True merge deleting another task's `arch_ref` is rejected before integration ref advances. | Goal spec lines 199-201 | Task 1 | Covered |
| AC-001-2 | True merge deleting task `plan_ref`, `spec_ref`, or `epic_ref` is rejected before ref advancement. | Goal spec lines 202-204 | Task 1 | Covered |
| AC-001-3 | True merge deleting output `arch_ref`, `plan_ref`, `spec_ref`, or `epic_ref` is rejected before ref advancement. | Goal spec lines 205-208 | Task 1 | Covered |
| AC-001-4 | Fast-forward candidate deleting a referenced artifact is rejected before ref advancement. | Goal spec lines 209-211 | Task 1 | Covered |
| AC-001-5 | Candidate deleting, renaming, or removing `goal.spec_ref` is rejected before ref advancement. | Goal spec lines 212-214 | Task 1 | Covered |
| AC-001-6 | Rename without state rewrite treats old protected path as missing and rejects candidate. | Goal spec lines 215-219 | Task 1 | Covered |
| AC-001-7 | Replacement by directory, submodule, symlink, or other non-regular object is rejected. | Goal spec lines 220-222 | Task 1, Task 3 | Covered |
| AC-001-8 | Invalid artifact ref that cannot be safely checked rejects merge with actionable diagnostic. | Goal spec lines 223-226 | Task 1, Task 3 | Covered |
| AC-001-9 | First failing snapshot followed by clean second snapshot returns hook success. | Goal spec lines 227-230 | Task 2 | Covered |
| AC-001-10 | Candidate failure plus state reread failure preserves both failures. | Goal spec lines 231-234 | Task 2 | Covered |
| AC-001-11 | Hook failure with changed integration HEAD retries instead of returning stale hook error. | Goal spec lines 235-237 | Task 2 | Covered |
| AC-001-12 | Hook failure with unreadable integration HEAD preserves hook and staleness failures. | Goal spec lines 238-240 | Task 2 | Covered |
| AC-001-13 | Hook pass followed by `UpdateRef` CAS conflict retries and re-runs hook on recomputed candidate. | Goal spec lines 241-243 | Task 2 | Covered |
| AC-001-14 | Already-merged no-op path returns without running hook. | Goal spec lines 244-246 | Task 2 | Covered |
| AC-001-15 | Post-merge `ValidateArtifactRefs` still runs and can mark task `INTEGRATION_FAILED` with rollback. | Goal spec lines 247-250 | Task 2 | Covered |
| AC-001-16 | Candidate rejection diagnostic names invalid path and deterministic owner with task ID and field where applicable. | Goal spec lines 251-253 | Task 1, Task 3 | Covered |
| C-000-1 | Coverage exercises Git object database and refs through Liza Git wrapper boundaries. | Goal spec lines 64-67 | Task 2, Task 3 | Covered |
| C-000-2 | Coverage exercises `.liza/state.yaml` model ownership through state fixtures. | Goal spec lines 68-69 | Task 1, Task 2, Task 3 | Covered |
| I-000-1 | Collector interface returns normalized refs with owner provenance. | Goal spec lines 73-74 | Task 3 | Covered |
| I-000-2 | `performCASMerge` pre-update hook validates candidate treeish before `UpdateRef`. | Goal spec lines 75-76 | Task 2 | Covered |
| ASM-000-1 | Durable artifact refs must resolve to regular Git files `100644` or `100755`; symlinks are rejected. | Goal spec lines 91-94 | Task 1, Task 3 | Covered |
| ASM-000-2 | `wt-merge` is authoritative protection point for cross-task deletion. | Goal spec lines 95-97 | Task 1 | Covered |
| ASM-001-1 | Regular file mode check is sufficient for path-liveness protection. | Goal spec lines 280-282 | Task 3 | Covered |
| ARCH-C1 | Acceptance coverage must exercise observable `wt-merge` and validator boundaries, not hidden implementation details. | Architecture-4 lines 36-38 | Task 1, Task 2, Task 3 | Covered |
| ARCH-C2 | `performCASMerge` remains artifact-policy agnostic. | Architecture-4 lines 37-39 | Task 2 | Covered |
| ARCH-C3 | Existing post-merge `ValidateArtifactRefs` backstop remains documented and tested. | Architecture-4 lines 39-40 | Task 2 | Covered |
| ARCH-C4 | G1.2 integration, rollback, state-validation, and CAS concurrency invariants are preserved. | Architecture-4 lines 41-42; INVARIANTS lines 134-142 and 163-172 | Task 1, Task 2, Task 3 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | Task 1, Task 2 | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: documentation edits are out of scope for this acceptance-test plan and are owned by sibling `architecture-4-code-planning-1`. | N/A |

## Pre-Submit Self-Check

- Task decomposition: Task 1 owns owner/path mutation acceptance, Task 2 owns temporal CAS/backstop regressions, and Task 3 owns boundary matrices. Tests are colocated with each behavior boundary.
- Dependency order: Task 2 depends on output[0] because both modify `internal/ops/wt_merge_test.go`. Task 3 has no sibling dependency because it writes different packages.
- Existing task dependencies: output tasks depend on the production coding tasks whose contracts they validate.
- Output parity: Task headings map to output entries in order: Task 1 is output[0], Task 2 is output[1], Task 3 is output[2].
- Shared-file audit: all same-file sibling writes are ordered; no un-ordered shared test files remain.
- Scope boundary: no task plans production implementation, documentation edits, artifact-ref semantic changes, or replacement of sibling implementation plans.
- Guardrails: G1.2 is addressed by tests that preserve CAS retry, integration-ref non-advancement, rollback backstop, state validation, and deterministic diagnostics.
