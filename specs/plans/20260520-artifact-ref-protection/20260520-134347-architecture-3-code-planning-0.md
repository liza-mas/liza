# Code Plan: Candidate Tree Artifact Path Validator

Status: draft

## Source Context

Based on:

- Goal spec: `specs/goals/20260520-artifact-ref-protection.md`, especially FR-001-8 through FR-001-11, AC-001-7, AC-001-8, AC-001-16, NFR-001-1, and NFR-001-3.
- Architecture plan: `specs/arch-plan/20260520-artifact-ref-protection/20260520-115540-architecture-3.md`, Scope 1.
- Upstream collector plan and tasks: `artifact-ref-collector-coding-0` and `artifact-ref-validator-coding-1`.
- Codebase reads: `internal/git/query.go`, `internal/git/git.go`, `internal/git/query_test.go`, `internal/statevalidate/validate.go`, and targeted searches in `internal/ops/wt_merge.go`.
- Guardrails: `GUARDRAILS.md` G1.2, `INVARIANTS.md` concurrency and integration sections, and `specs/architecture/ADR/README.md`.

ASSUMPTION: `artifact-ref-validator-coding-1` will leave `internal/statevalidate` with a collector API equivalent to `CollectArtifactRefs(state, projectRoot)` and an `ArtifactRef` value carrying normalized path, raw value, and owner metadata as planned by `architecture-1-code-planning-0`. This is a concrete task dependency, not a hidden implementation guess.

Doc Impact: N/A - this scope adds internal Git/tree validation and statevalidate validator-boundary behavior. User-facing docs for the overall artifact guard are owned by sibling documentation planning.

Test Impact: Required. Implementation tasks must add focused unit tests for the Git tree path query and statevalidate candidate validator diagnostics; e2e merge behavior belongs to sibling guard-wiring and acceptance-matrix planning tasks.

## Implementation Strategy

Split the validator boundary into two implementation tasks:

1. Add a narrow `internal/git` tree path query that reports whether a single repo-relative path is present in a treeish and, when present, returns the raw Git object mode.
2. Add `internal/statevalidate` candidate validation over collected artifact refs using that query, accepting only modes `100644` and `100755` and returning deterministic owner-aware diagnostics for missing, invalid-mode, and collector-invalid refs.

The Git query should stay policy-free. It should not know artifact semantics, owner provenance, blackboard state, or merge lifecycle. A suitable shape is:

```go
func (g *Git) TreePathMode(treeish, path string) (mode string, present bool, err error)
```

The implementation should use `git ls-tree` with `--` before the path, preferably with `--full-tree` and `-z` so paths with spaces remain parseable. Empty output means missing. A non-empty entry yields the leading Git mode string such as `100644`, `100755`, `040000`, `120000`, or `160000`. Unexpected multiple entries or malformed output should return a query error rather than silently choosing one.

The statevalidate validator should own artifact policy. It should expose a guard-facing boundary equivalent to:

```go
type CandidateTreeLookup interface {
	TreePathMode(treeish, path string) (mode string, present bool, err error)
}

func ValidateCandidateArtifactRefs(candidateTreeish string, refs []ArtifactRef, lookup CandidateTreeLookup) error
```

An optional convenience wrapper can collect from an in-memory state snapshot if it stays inside `internal/statevalidate`, but blackboard reads, hook freshness, `performCASMerge`, and post-merge rollback behavior remain out of scope for this plan.

Validation rules:

- Accept modes `100644` and `100755`.
- Reject missing paths with a stable cause such as `candidate_artifact_missing`.
- Reject any present mode other than `100644` or `100755`, including `040000`, `160000`, `120000`, and unknown modes, with a stable cause such as `candidate_artifact_invalid_mode`.
- Treat collector invalid-ref errors as candidate validation failures, preserving the collector cause and owner metadata rather than converting them to missing paths.
- Deduplicate Git lookups by normalized path if practical, but diagnostics must retain all deterministic owners for the failing path or at least the first deterministic owner.
- Return the first invalid path in collector order or sorted path order; do not depend on map iteration.

Diagnostics should be safe and testable. Prefer a dedicated error type or an extension of `ArtifactRefError` that exposes `SafeDetails()` with at least:

- `path`
- `cause`
- `mode` when a tree entry exists
- deterministic owner provenance: field, task ID when applicable, and output index when applicable

Error text should include the invalid path, cause, mode when present, and owner provenance. Exact wording may vary, but tests must assert stable substrings or exact structured details that prove the diagnostic is actionable.

## Planned Coding Tasks

### Task 1: Git Tree Path Mode Query

desc: Add a policy-free Git tree path mode query for candidate treeish validation.

done_when: `internal/git` exposes a query such as `TreePathMode(treeish, path)` that runs a Git tree lookup for one repo-relative path, distinguishes missing paths from present entries, returns the raw Git mode for present entries, uses `--` before the path, and focused git tests prove mode detection for regular files `100644`, executable files `100755`, directories `040000`, symlinks `120000`, submodules or gitlinks `160000` where feasible, missing paths, and paths containing spaces.

scope: Modify `internal/git/query.go` and `internal/git/query_test.go`; reuse existing Git command helpers and test repository helpers. Out of scope: artifact-ref owner diagnostics, statevalidate policy, collector integration, `internal/ops/wt_merge.go`, `performCASMerge`, and blackboard state reads.

spec_ref: specs/goals/20260520-artifact-ref-protection.md

plan_ref: specs/plans/20260520-artifact-ref-protection/20260520-134347-architecture-3-code-planning-0.md

depends_on: none

Implementation notes:

- Use a machine-parseable `ls-tree` form, preferably `git ls-tree --full-tree -z <treeish> -- <path>`.
- Empty output is a successful missing result, not an error.
- Parse only the mode field before the first space; malformed non-empty output is an error.
- Do not classify modes in `internal/git`; return Git's raw mode string.
- For a submodule/gitlink test, prefer `git update-index --add --cacheinfo 160000,<commit>,<path>` against a valid local commit SHA to avoid network-dependent submodule setup.

Validation commands for the coder:

- `go test ./internal/git -run 'TestTreePathMode|TestCalculateDrift|TestGetWorktreeHEAD'`
- `go test ./internal/git`

### Task 2: Candidate Artifact Ref Validator And Diagnostics

desc: Validate collected artifact refs against candidate Git tree modes with deterministic owner-aware diagnostics.

done_when: `internal/statevalidate` exposes a candidate-tree artifact validator that consumes collected `ArtifactRef` values and a candidate tree lookup, accepts only modes `100644` and `100755`, rejects missing paths, directories, submodules/gitlinks, symlinks with mode `120000`, and any other non-regular mode, propagates collector invalid-ref failures as candidate validation failures, and statevalidate tests assert deterministic diagnostics naming the invalid path, cause, mode when present, and owner provenance for FR-001-9 through FR-001-11 plus the path/mode classification and diagnostic portions of FR-001-8, AC-001-7, AC-001-8, and AC-001-16.

scope: Add a candidate validator in `internal/statevalidate` and focused tests such as `internal/statevalidate/candidate_artifact_refs_test.go`; adjust `internal/statevalidate/validate.go` or artifact-ref error helpers only as needed for stable candidate diagnostics; consume the Git query shape from Task 1 through a small interface or function dependency. Out of scope: modifying `internal/ops/wt_merge.go`, wiring the validator into `MergeWorktree`, reading latest state per hook invocation, second-snapshot confirmation, CAS retry behavior, post-merge rollback behavior, and changing collector normalization internals owned by `artifact-ref-validator-coding-1`.

spec_ref: specs/goals/20260520-artifact-ref-protection.md

plan_ref: specs/plans/20260520-artifact-ref-protection/20260520-134347-architecture-3-code-planning-0.md

depends_on: ["0"]

task_depends_on: ["artifact-ref-validator-coding-1"]

Implementation notes:

- Keep the validator deterministic by sorting or by relying only on collector-sorted refs; any map used for lookup deduplication must not control diagnostic order.
- A regular file is exactly mode `100644` or `100755`. Do not accept symlink blobs with mode `120000`.
- Include all owners for a failing duplicate path when practical. At minimum, include the first deterministic owner and do not hide duplicate owners in structured details if the collector exposes them naturally.
- Add fake lookup tests for unknown modes that are difficult to create through Git, so the policy rejects future non-regular modes without requiring bespoke repository fixtures.
- Add a test where collection fails for an invalid ref and the guard-facing validation wrapper returns an actionable invalid-ref candidate failure instead of attempting a Git lookup.
- Do not remove or weaken existing `ValidateArtifactRefs` tests; this task adds candidate-tree validation alongside the post-merge backstop path.

Validation commands for the coder:

- `go test ./internal/statevalidate -run 'TestValidateCandidateArtifactRefs|TestCandidateArtifactRef'`
- `go test ./internal/git ./internal/statevalidate`

## Dependency And Shared-File Audit

| File or package | Task 1 | Task 2 | Dependency required |
|-----------------|--------|--------|---------------------|
| `internal/git/query.go` | Adds the tree path mode query | Consumes through interface only | Yes: Task 2 depends on output[0] for the query contract |
| `internal/git/query_test.go` | Adds Git mode fixture tests | No planned writes | No |
| `internal/statevalidate` candidate validator files | No planned writes | Adds validator and tests | No |
| `internal/statevalidate/validate.go` or artifact error helpers | No planned writes | May extend diagnostics only | No |
| `internal/ops/wt_merge.go` | Out of scope | Out of scope | No |

Task 2 depends on output[0] because it needs the Git query contract. Task 2 also depends on `artifact-ref-validator-coding-1` because it consumes the collector-backed `ArtifactRef` API and owner diagnostics. No two tasks in this plan write the same file.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Candidate validation uses a Git tree query rather than filesystem state. | Goal spec FR-001-8; NFR-001-1; architecture-3 Scope 1 | Task 1, Task 2 | Covered |
| 2 | Candidate validation requires protected paths to exist as regular Git files with mode `100644` or `100755`. | Goal spec FR-001-9; ASM-000-1; ASM-001-1 | Task 2 | Covered |
| 3 | Candidate validation rejects missing paths. | Goal spec FR-001-10; architecture-3 Scope 1 done_when | Task 1, Task 2 | Covered |
| 4 | Candidate validation rejects directories. | Goal spec FR-001-10; AC-001-7 | Task 1, Task 2 | Covered |
| 5 | Candidate validation rejects submodules/gitlinks. | Goal spec FR-001-10; AC-001-7 | Task 1, Task 2 | Covered |
| 6 | Candidate validation rejects symlinks with mode `120000`. | Goal spec FR-001-10; AC-001-7; goal spec Out of Scope symlink note | Task 1, Task 2 | Covered |
| 7 | Candidate validation rejects any other non-regular or unknown mode. | Goal spec FR-001-10; AC-001-7 | Task 2 | Covered |
| 8 | Rejection diagnostics include the invalid path and deterministic owner provenance. | Goal spec FR-001-11; AC-001-16; NFR-000-3; NFR-001-3 | Task 2 | Covered |
| 9 | Rejection diagnostics include the cause and mode when the candidate path exists with an invalid mode. | Architecture-3 Scope 1 done_when; architecture-3 Cross-Cutting Concerns | Task 2 | Covered |
| 10 | Collector invalid-ref failures are propagated as candidate validation failures with actionable diagnostics. | Goal spec FR-001-6; AC-001-8; assigned done_when | Task 2 | Covered |
| 11 | Git path query distinguishes missing paths from present non-regular modes. | Goal spec NFR-001-1; architecture-3 Git Tree Path Query component | Task 1 | Covered |
| 12 | Artifact policy stays out of the Git query layer. | Goal spec NFR-000-4; NFR-001-2; architecture-3 Structural Decision 1 | Task 1, Task 2 | Covered |
| 13 | Validator-boundary tests cover FR-001-9 through FR-001-11. | Assigned done_when; architecture-3 Required Test Cases | Task 1, Task 2 | Covered |
| 14 | Validator-boundary tests cover path/mode classification and diagnostic portions supporting FR-001-8, AC-001-7, AC-001-8, and AC-001-16. | Assigned done_when | Task 1, Task 2 | Covered |
| 15 | `MergeWorktree` hook wiring, latest-state reads, second-snapshot confirmation, CAS retry behavior, and post-merge rollback behavior are not planned in this scope. | Assigned scope out-of-scope list; architecture-3 Decomposition Scope 1 boundary | Task 1, Task 2 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this scope is an internal validator boundary; end-to-end merge rejection is owned by sibling guard-wiring and acceptance-matrix planning tasks. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: this scope adds an internal validator boundary; overall user-visible artifact guard documentation is owned by sibling documentation planning. | N/A |

## Pre-Submit Self-Check

- Task decomposition: Task 1 owns policy-free Git tree lookup; Task 2 owns artifact validation policy and diagnostics. TDD is colocated with each behavior change.
- Dependency order: Task 2 depends on output[0] because it consumes the Git query. Task 2 also depends on existing task `artifact-ref-validator-coding-1` because it consumes collector-backed refs.
- Output parity: Task headings map to output entries in order: Task 1 is output[0], Task 2 is output[1].
- Shared-file audit: No planned shared write files across sibling outputs. The dependency is semantic, not a merge-conflict workaround.
- Scope boundary: No task plans `MergeWorktree` hook wiring, state freshness reads, second-snapshot confirmation, CAS retry handling, post-merge rollback behavior, or submit-for-review diagnostics.
- Guardrails: The plan preserves `performCASMerge` separation from artifact policy and keeps project-specific commands out of runtime behavior.
