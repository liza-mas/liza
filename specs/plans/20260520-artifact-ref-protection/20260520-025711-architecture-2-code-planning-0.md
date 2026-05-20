# Code Plan: CAS Pre-Update Hook Boundary

Status: draft

Task: `architecture-2-code-planning-0`

Architecture reference: `specs/arch-plan/20260520-artifact-ref-protection/20260520-024522-architecture-2.md`

Goal spec: `specs/goals/20260520-artifact-ref-protection.md`

## Objective

Add the narrow `performCASMerge` pre-update hook boundary so future artifact-guard work can validate a candidate tree before integration ref advancement without coupling `performCASMerge` to blackboard state, artifact refs, or artifact validation policy.

## Current Code Shape

- `internal/ops/wt_merge.go` owns `performCASMerge`, its bounded CAS retry loop, and the `MergeWorktree` call site.
- `performCASMerge` currently handles three paths: already-merged no-op, fast-forward `UpdateRef`, and true-merge `MergeTree` plus `CreateCommitFromTree` plus `UpdateRef`.
- `internal/ops/wt_merge_test.go` already has temporary Git repository helpers and a deterministic CAS retry test using `mergeCASRetryTestHook`.
- `MergeWorktree` already keeps post-merge `statevalidate.ValidateArtifactRefs` after successful ref update; this plan must not move or remove that backstop.

## Implementation Guidance

- Change `performCASMerge` to accept an optional hook argument with the shape `func(candidateTreeish string) error`.
- Keep the hook opaque. Do not import `statevalidate`, state models, artifact-ref types, or provenance formatting into `performCASMerge`.
- Keep `MergeWorktree` compiling by passing `nil` until a later artifact-guard hookup task provides the real hook.
- Invoke the hook only after a CAS attempt has computed a candidate treeish that can be advanced, and before the matching `UpdateRef` call.
- Skip the hook on already-merged no-op returns and merge-conflict returns because neither path advances the integration ref.
- For fast-forward attempts, pass `expectedCommit`.
- For true-merge attempts, pass the created candidate merge commit as the treeish before `UpdateRef`; `git ls-tree <mergeCommit> -- <path>` can inspect the candidate tree and this keeps the hook target aligned with the ref target.
- Add a small helper or local branch for hook failures that re-reads `integrationRef`, compares it with the attempt's `preMergeHEAD`, retries if changed, returns the hook error unchanged if unchanged, and returns a composite error preserving both the hook error and the failed staleness read if the re-read fails.
- Keep existing `git.RefConflictError` retry handling after hook success. The next loop attempt must re-read integration HEAD, recompute the candidate, and re-run the hook.

## Planned Coding Tasks

### Task 1: Add `performCASMerge` hook parameter and candidate invocation points

desc: Add the optional `performCASMerge` pre-update hook parameter and invoke it with the correct candidate treeish for no-op, fast-forward, and true-merge paths.

done_when: `performCASMerge` accepts a nil-safe `func(candidateTreeish string) error` hook; `MergeWorktree` passes nil; already-merged no-op returns without calling the hook; fast-forward attempts call the hook with `expectedCommit` before `UpdateRef`; true-merge attempts call the hook with the candidate merge commit before `UpdateRef`; tests in `internal/ops/wt_merge_test.go` assert hook call counts and treeish values for already-merged, fast-forward, and true-merge scenarios.

scope: Modify `internal/ops/wt_merge.go` and `internal/ops/wt_merge_test.go` only. Do not add artifact-ref collection, candidate artifact validation policy, state reads inside the hook boundary, blackboard coupling, or CLI-facing behavior.

spec_ref: specs/goals/20260520-artifact-ref-protection.md

source_requirements: FR-001-17 through FR-001-22 and AC-001-14; architecture plan lines 95-106 and 167-177.

depends_on: none.

Implementation notes:

- Prefer `func performCASMerge(gw *git.Git, integrationRef, expectedCommit, taskID string, preUpdateHook func(string) error) (*casMergeOutcome, error)` over a package-level type unless the implementation reads more clearly with a local alias.
- Update the existing `MergeWorktree` call at `internal/ops/wt_merge.go` to pass `nil`.
- In the fast-forward branch, run the hook immediately before `gw.UpdateRef(integrationRef, expectedCommit, preMergeHEAD)`.
- In the true-merge branch, run the hook immediately after `CreateCommitFromTree` returns `mergeCommit` and before `gw.UpdateRef(integrationRef, mergeCommit, preMergeHEAD)`.
- Add direct package tests against `performCASMerge` where possible to avoid unrelated `MergeWorktree` state transitions. Use the existing temporary Git repo helpers and `internal/git.New(tmpDir)`.
- For the true-merge treeish assertion, have the hook run `git ls-tree <candidateTreeish> -- <base path>` and `git ls-tree <candidateTreeish> -- <task path>` so the test proves the hook receives an inspectable candidate tree before ref advancement.

### Task 2: Retry stale hook failures and preserve staleness-read failures

desc: Add hook-failure staleness handling so `performCASMerge` retries stale hook errors and preserves hook plus staleness-read errors when freshness cannot be checked.

done_when: When the pre-update hook returns an error, `performCASMerge` re-reads `integrationRef`; if the ref changed from the attempt's `preMergeHEAD`, it discards that stale hook error and retries; if the ref is unchanged, it returns the original hook error; if the ref cannot be re-read, it returns an error that preserves the original hook error and reports the staleness read failure; tests assert unchanged-head return, changed-head retry, and unreadable-head composite error behavior.

scope: Modify `internal/ops/wt_merge.go` and `internal/ops/wt_merge_test.go` only. Do not inspect hook error types for artifact semantics and do not add artifact-guard state freshness logic.

spec_ref: specs/goals/20260520-artifact-ref-protection.md

source_requirements: FR-001-23 through FR-001-26, AC-001-11, and AC-001-12; architecture plan lines 107-118 and 179-183.

depends_on: Task 1.

Implementation notes:

- Keep the stale-hook branch generic: compare the fresh `GetCommitSHA(integrationRef)` result with the attempt's `preMergeHEAD`.
- If the fresh HEAD differs, `continue` the bounded CAS loop. This retry must count against `maxMergeRetries`.
- If the fresh HEAD matches, return the hook error without wrapping that would break `errors.Is` checks for a sentinel hook error.
- If `GetCommitSHA(integrationRef)` fails, return a context error that preserves both errors. `errors.Join` plus contextual wrapping is acceptable if `errors.Is(returned, hookSentinel)` still succeeds and the returned message identifies the failed staleness read.
- For the stale-hook retry test, have the hook advance `integrationRef` to a competing commit and then return a sentinel error on the first attempt; assert the merge eventually succeeds and final integration contains both the competing commit and the task commit.
- For the unreadable-head test, have the hook make `integrationRef` unreadable, such as deleting the ref in the temporary repository, before returning a sentinel error; assert the returned error preserves the sentinel and mentions the failed re-read.

### Task 3: Preserve CAS-conflict retry and re-run the hook after successful hook validation

desc: Preserve existing CAS conflict retry semantics so a hook that passes before a conflicting `UpdateRef` is re-run against the recomputed candidate on the retry.

done_when: If the pre-update hook succeeds but `UpdateRef` returns `git.RefConflictError`, `performCASMerge` retries from a fresh integration HEAD and calls the hook again with the recomputed candidate treeish; tests extend the deterministic CAS conflict scenario to assert at least two hook invocations, distinct retry candidates when applicable, and final integration ancestry for both the competing commit and task commit.

scope: Modify `internal/ops/wt_merge.go` and `internal/ops/wt_merge_test.go` only. Do not change `git.UpdateRef`, retry limits, merge conflict handling, or post-merge artifact validation.

spec_ref: specs/goals/20260520-artifact-ref-protection.md

source_requirements: FR-001-27 and AC-001-13; architecture plan lines 185-189 and 214-218.

depends_on: Task 2.

Implementation notes:

- Reuse or adapt `TestMergeWorktree_CASRetryDeterministic` rather than adding a parallel test helper that simulates the same conflict differently.
- Keep `mergeCASRetryTestHook` as a test-only way to force `UpdateRef` CAS conflict after the attempt's `preMergeHEAD` is read. The new pre-update hook should be a separate closure passed to `performCASMerge`.
- Assert the pre-update hook observes one candidate per CAS attempt. For fast-forward first attempts and true-merge retry attempts, assert the candidates match the expected shape rather than only asserting a count.
- Do not weaken existing assertions that final integration contains the competing commit and the task commit.

## Dependency Graph

```text
Task 1 -> Task 2 -> Task 3
```

The tasks intentionally serialize because all three modify `internal/ops/wt_merge.go` and `internal/ops/wt_merge_test.go`.

## Validation Plan For Implementers

- Run focused tests for each task after its red/green cycle, using `go test ./internal/ops -run '<new test name>|TestMergeWorktree_CASRetryDeterministic'`.
- Run `go test ./internal/ops ./internal/git ./internal/statevalidate` after the full task chain is implemented.
- Run the repository pre-commit hook on touched files before submission.
- If a worktree Go test fails due to stale embedded assets, follow `lessons/agents/worktree-build-prerequisites.md` and run `make -C <worktree> sync-embedded` before retrying.

## Shared-File Audit

| File | Tasks | Dependency |
|------|-------|------------|
| `internal/ops/wt_merge.go` | Task 1, Task 2, Task 3 | Task 2 depends on Task 1; Task 3 depends on Task 2 |
| `internal/ops/wt_merge_test.go` | Task 1, Task 2, Task 3 | Task 2 depends on Task 1; Task 3 depends on Task 2 |

## Spec Compliance Matrix

Rows cover the requirements in this code-planning task's assigned scope. Artifact-ref collection, candidate artifact validation policy, state freshness inside the artifact guard, blackboard coupling, and end-to-end guard hookup are sibling-task scopes and are intentionally excluded.

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Guard must preserve existing CAS retry safety for concurrent merges. | NFR-000-2, goal spec lines 55-56 | Task 2, Task 3 | Covered |
| 2 | `performCASMerge` must not depend on blackboard state or artifact-ref semantics. | NFR-000-4, goal spec lines 59-60 | Task 1, Task 2 | Covered |
| 3 | Existing post-merge `ValidateArtifactRefs` backstop must remain in place. | NFR-000-5, goal spec lines 61-62 | Task 1, Task 3 | Covered |
| 4 | Expose a `performCASMerge` pre-update hook accepting an optional candidate treeish callback. | Interface I-000-2, goal spec lines 75-76 | Task 1 | Covered |
| 5 | `performCASMerge` must accept an optional narrow pre-update hook with signature equivalent to `func(candidateTreeish string) error`. | FR-001-17, goal spec lines 155-156 | Task 1 | Covered |
| 6 | The hook must run inside each CAS attempt after candidate computation and before `UpdateRef`. | FR-001-18, goal spec lines 157-159 | Task 1, Task 3 | Covered |
| 7 | `performCASMerge` must treat the hook as opaque and own CAS retry semantics. | FR-001-19, goal spec lines 160-163 | Task 1, Task 2, Task 3 | Covered |
| 8 | The hook must be skipped for the already-merged no-op path. | FR-001-20, goal spec lines 164-165 | Task 1 | Covered |
| 9 | Fast-forward merges must validate `expectedCommit` before `UpdateRef`. | FR-001-21, goal spec lines 166-167 | Task 1 | Covered |
| 10 | True merges must validate the candidate merge treeish before `UpdateRef`. | FR-001-22, goal spec lines 168-169 | Task 1 | Covered |
| 11 | Hook failure must trigger an integration ref re-read before returning the hook error. | FR-001-23, goal spec lines 170-171 | Task 2 | Covered |
| 12 | If integration HEAD changed after candidate computation, discard stale hook error and retry. | FR-001-24, goal spec lines 172-173 | Task 2 | Covered |
| 13 | If integration HEAD is unchanged after hook failure, return the hook error. | FR-001-25, goal spec lines 174-175 | Task 2 | Covered |
| 14 | If integration HEAD cannot be re-read after hook failure, preserve hook failure and staleness-read failure. | FR-001-26, goal spec lines 176-178 | Task 2 | Covered |
| 15 | If hook passes but `UpdateRef` has a CAS conflict, retry and re-run the hook against the recomputed candidate. | FR-001-27, goal spec lines 179-181 | Task 3 | Covered |
| 16 | `performCASMerge` must not know about blackboard state, artifact-ref provenance, or artifact validation policy. | NFR-001-2, goal spec lines 192-193 | Task 1, Task 2 | Covered |
| 17 | Error messages must be stable enough for tests to assert without relying on map iteration order. | NFR-001-3, goal spec lines 194-195 | Task 2 | Covered |
| 18 | Given hook failure with changed integration HEAD, retry instead of returning stale hook error. | AC-001-11, goal spec lines 235-237 | Task 2 | Covered |
| 19 | Given hook failure and failed integration HEAD re-read, return an error preserving both failures. | AC-001-12, goal spec lines 238-240 | Task 2 | Covered |
| 20 | Given hook success but `UpdateRef` CAS conflict, retry and re-run the hook against the recomputed candidate. | AC-001-13, goal spec lines 241-243 | Task 3 | Covered |
| 21 | Given `expectedCommit` is already merged, return without running the hook. | AC-001-14, goal spec lines 244-246 | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this task adds an internal unexported CAS hook boundary that is not CLI-reachable until the sibling artifact-guard hookup task wires a real hook into `MergeWorktree`; package-level Git integration tests cover the scoped behavior. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: the changed boundary is internal and already specified by the goal spec plus architecture plan; no user-facing command, config, or workflow changes in this scope. | N/A |

## Output Entries

The structured output file next to this plan contains one entry for each planned coding task. The `desc`, `done_when`, `scope`, and `spec_ref` fields are copied verbatim from the task sections above.
