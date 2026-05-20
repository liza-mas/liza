# Code Plan: Statevalidate Artifact Ref Collector And Provenance

Status: draft

## Source Context

Based on:

- Goal spec: `specs/goals/20260520-artifact-ref-protection.md`
- Architecture plan: `specs/arch-plan/20260520-artifact-ref-protection/20260520-025848-architecture-1.md`
- Current code reads: `internal/statevalidate/validate.go`, relevant regions of `internal/statevalidate/validate_task.go`, `internal/statevalidate/validate_specref_test.go`, `internal/statevalidate/validate_task_test.go`, `internal/paths/normalize.go`, `internal/models/state.go`, `internal/models/task.go`, `internal/ops/wt_merge.go`, `internal/ops/add_tasks.go`, and `internal/ops/set_task_output.go`
- Guardrails: `GUARDRAILS.md`, `INVARIANTS.md` worktree and integration matrix rows

ASSUMPTION: The implementation can add a new `internal/statevalidate/artifact_refs.go` and `internal/statevalidate/artifact_refs_test.go` without conflicting with existing package organization. This is reversible and local to the collector package.

Doc Impact: N/A - this is an internal validation API and implementation plan; the goal and architecture specs already define the behavior.

Test Impact: Required in implementation tasks. Unit tests must cover collector normalization/provenance and `ValidateArtifactRefs` collector consumption; no separate e2e task belongs to this scope.

## Implementation Strategy

Add a collector API in `internal/statevalidate` that centralizes protected artifact-ref traversal for goal refs, task refs, and output refs. The collector should return one item per owning state field, not one item per unique path, because downstream diagnostics need deterministic owner provenance.

Recommended public shapes:

```go
type ArtifactRefOwner struct {
	Field       string
	TaskID      string
	OutputIndex *int
}

type ArtifactRef struct {
	Path     string
	RawValue string
	Owner    ArtifactRefOwner
}

func CollectArtifactRefs(state *models.State, projectRoot string) ([]ArtifactRef, error)
```

The exact names can vary, but the implementation must preserve these semantics:

- `ArtifactRef.Path` is always repo-relative, slash-separated, fragment-free, non-empty, and safe as a Git tree path.
- `ArtifactRef.RawValue` preserves the original scalar value for diagnostics.
- `ArtifactRefOwner` contains the field name, task ID when the owner is a task or task output, and output index when the owner is an output entry.
- Goal-level owner provenance has no task ID and no output index.
- Output sorting is deterministic by normalized path, then task ID, then output-index presence/value, then field name.

Normalization should reuse `paths.SplitRefFile` for fragment stripping, but should not use `paths.NormalizeSpecRef` as the authoritative collector normalizer because `NormalizeSpecRef` intentionally preserves unsafe values for downstream validation. Implement the safe collector normalizer inside `statevalidate` or add a generic helper only if it remains stack-agnostic and has no state ownership knowledge.

Invalid refs should fail closed during collection with owner-aware diagnostics. Extend the existing `ArtifactRefError` where practical instead of creating an unrelated error family, preserving compatibility with `jsonout.ErrorDetails` and existing callers. Add stable cause strings for empty paths after fragment stripping, traversal outside the repo, and unsafe absolute paths. Existing `multiple_refs_not_supported` and `file_not_found` causes should remain recognizable.

`ValidateArtifactRefs` should call `CollectArtifactRefs` once and perform existence checks over the collected normalized paths only. Raw absolute refs must not be passed to Git pathspecs. The existing post-merge call in `internal/ops/wt_merge.go` remains in place; this plan does not add candidate-tree validation or CAS hook mechanics.

## Planned Coding Tasks

### Task 1: Collector API, Normalization, Provenance, And Invalid-Ref Tests

desc: Add the reusable statevalidate artifact-ref collector API with normalization, deterministic owner provenance, invalid-ref diagnostics, and unit tests.

done_when: `internal/statevalidate` exposes a collector callable from package tests that returns one normalized repo-relative fragment-free `ArtifactRef` per goal/task/output artifact owner; owner metadata includes field name, task ID when applicable, and output index when applicable; collector output is sorted by path and owner metadata; semicolon-joined refs, fragment-only or empty refs after fragment stripping, traversal outside the repo, and unsafe absolute refs fail closed with owner-aware `ArtifactRefError` diagnostics; valid absolute refs under `projectRoot` normalize to repo-relative paths; and focused statevalidate tests assert FR-001-1 through FR-001-4, FR-001-6, FR-001-7, and the invalid-ref portion of FR-001-11.

scope: Create `internal/statevalidate/artifact_refs.go` and `internal/statevalidate/artifact_refs_test.go`; update `internal/statevalidate/validate.go` only as needed to extend `ArtifactRefError` fields, cause constants, formatting, and `SafeDetails`; use existing `internal/paths` helpers where appropriate without changing candidate-tree or CAS merge behavior. Out of scope: modifying `internal/ops/wt_merge.go`, adding Git tree mode checks, changing state lifecycle rules, or wiring a pre-update hook.

spec_ref: specs/goals/20260520-artifact-ref-protection.md

plan_ref: specs/plans/20260520-artifact-ref-protection/20260520-081606-architecture-1-code-planning-0.md

depends_on: none

Implementation notes:

- Treat empty artifact fields as absent, not invalid.
- Treat `#fragment` with no file portion as invalid with a stable empty-path cause.
- Reject any cleaned relative path that is `.`, starts with `..`, or escapes repo containment.
- Reject cross-platform absolute forms outside `projectRoot`, including Windows drive-letter paths even when running on Unix.
- Convert in-repo absolute paths to repo-relative slash-separated paths before sorting and returning.
- Include owner information in both error text and safe details. `SafeDetails` should include `field`, `value`, `cause`, and, when present, `path`, `task_id`, and `output_index`.
- Prefer explicit table tests with exact expected slices and exact cause assertions. Avoid shape-only assertions.

Validation commands for the coder:

- `go test ./internal/statevalidate -run 'TestCollectArtifactRefs|TestArtifactRef'`
- `go test ./internal/jsonout -run TestErrorDetails_UnwrapsValidationError`

### Task 2: ValidateArtifactRefs Collector Consumption And Diagnostics

desc: Refactor ValidateArtifactRefs to consume the artifact-ref collector and preserve collector-backed post-merge diagnostics.

done_when: `ValidateArtifactRefs(state, projectRoot)` calls the collector as the single traversal source, checks each collected normalized path against the working tree and integration-branch fallback, passes only repo-relative normalized paths to Git fallback checks, preserves the existing nil-state error and post-merge backstop call shape, returns deterministic owner-aware diagnostics for missing artifacts, and statevalidate tests prove FR-001-5 plus the missing-artifact portion of FR-001-11 for goal, task, and output refs.

scope: Modify `internal/statevalidate/validate.go` and statevalidate tests such as `internal/statevalidate/validate_specref_test.go`; add or adjust helper functions only inside `internal/statevalidate`; preserve existing `checkSpecFileExists`, `checkArtifactRefFileExists`, and `ValidateArtifactRefScalar` compatibility for current callers unless a narrower internal wrapper is required. Out of scope: changing `internal/ops/wt_merge.go`, changing `performCASMerge`, adding candidate-tree `git ls-tree` validation, changing task-output mutation commands, or altering unrelated state invariants.

spec_ref: specs/goals/20260520-artifact-ref-protection.md

plan_ref: specs/plans/20260520-artifact-ref-protection/20260520-081606-architecture-1-code-planning-0.md

depends_on: Task 1

Implementation notes:

- Introduce a small existence helper that accepts a collected `ArtifactRef` or owner metadata so missing-file diagnostics retain provenance without re-parsing field names.
- Keep `checkSpecFileExists` and existing scalar validation behavior stable for call sites in `validateTaskInvariants`, `validateTaskOutput`, `add_tasks`, and `set_task_output`.
- The integration-branch fallback must call Git with `integrationBranch + ":" + collected.Path`, never with the raw ref.
- Add regression tests showing an absolute in-repo raw value is collected as a relative path and then uses the normalized path for integration-branch fallback.
- Add tests for goal `spec_ref`, task `plan_ref` or `arch_ref`, and `output[].plan_ref`/`output[].arch_ref` missing-file diagnostics including field, task ID, and output index where applicable.

Validation commands for the coder:

- `go test ./internal/statevalidate -run 'TestValidateArtifactRefs|TestCheckSpecFileExists|TestCheckArtifactRefFileExists|TestCollectArtifactRefs'`
- `go test ./internal/ops -run TestMergeWorktree_RejectsDeletingReferencedArtifact`
- `go test ./internal/statevalidate ./internal/jsonout ./internal/ops`

## Dependency And Shared-File Audit

| File or package | Task 1 | Task 2 | Dependency required |
|-----------------|--------|--------|---------------------|
| `internal/statevalidate/validate.go` | Extends `ArtifactRefError` and causes | Refactors `ValidateArtifactRefs` and helpers | Yes: Task 2 depends on Task 1 |
| `internal/statevalidate` tests | Adds collector-focused tests | Adds validator-consumption tests | Yes: Task 2 depends on Task 1 |
| `internal/paths` | Read/reuse only unless helper extraction is strictly needed | No planned writes | No |
| `internal/ops/wt_merge.go` | Out of scope | Out of scope | No |

No planned tasks can run in parallel because both implementation tasks may touch `internal/statevalidate/validate.go`.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Expose a reusable artifact-ref collector from `statevalidate`. | Goal spec FR-001-1 | Task 1 | Covered |
| 2 | Collector returns normalized repo-relative paths with any `#fragment` stripped. | Goal spec FR-001-2 | Task 1 | Covered |
| 3 | Collector returns owner provenance including field name, task ID when applicable, and output index when applicable. | Goal spec FR-001-3 | Task 1 | Covered |
| 4 | Collector output is deterministic, sorted by path and owner metadata. | Goal spec FR-001-4 | Task 1 | Covered |
| 5 | `ValidateArtifactRefs` uses the collector instead of duplicating artifact-ref traversal. | Goal spec FR-001-5 | Task 2 | Covered |
| 6 | Collector fails closed for semicolon-joined refs, empty paths after fragment stripping, traversal outside the repo, and unsafe absolute paths. | Goal spec FR-001-6 | Task 1 | Covered |
| 7 | Valid absolute in-repo refs normalize to repo-relative paths, while unsupported absolute refs are rejected with actionable diagnostics. | Goal spec FR-001-7 | Task 1, Task 2 | Covered |
| 8 | Rejection diagnostics include invalid path and deterministic owner provenance. | Goal spec FR-001-11 | Task 1, Task 2 | Covered |
| 9 | Diagnostics are deterministic and actionable, naming invalid artifact path and owner provenance. | Goal spec NFR-000-3 | Task 1, Task 2 | Covered |
| 10 | Existing post-merge `ValidateArtifactRefs` backstop remains in place. | Goal spec NFR-000-5 and FR-001-28 as relevant to this collector scope | Task 2 | Covered |
| 11 | Collector is the `statevalidate` artifact-ref collection interface used by validation and future merge guards. | Goal spec Interface I-000-1 and Feature Interfaces | Task 1, Task 2 | Covered |
| 12 | Preserve `performCASMerge` separation from blackboard state and artifact-ref semantics. | Goal spec NFR-000-4 and NFR-001-2 | Task 1, Task 2 | Covered |
| 13 | Candidate-tree Git object mode checks, CAS hook mechanics, integration ref advancement, and state freshness retry semantics are out of scope for this code plan. | Assigned task scope and architecture plan boundary | Task 1, Task 2 | Covered |
| 14 | Unit tests cover FR-001-1 through FR-001-7 and FR-001-11. | Assigned done_when and architecture plan Validation And Test Strategy | Task 1, Task 2 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this scope exposes an internal collector and refactors the existing post-merge validator; end-to-end candidate-tree rejection belongs to sibling guard and acceptance planning tasks. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: no user-facing command or configuration is added in this scope; the goal spec and architecture plan already document the new internal interface. | N/A |

## Pre-Submit Self-Check

- Task decomposition: Task 1 owns the reusable collector contract; Task 2 owns the existing validator's consumption of that collector. TDD is colocated with each behavior change.
- Dependency order: Task 2 depends on output[0] because it consumes the collector and may share `internal/statevalidate/validate.go`.
- Scope boundary: No task plans `performCASMerge`, candidate Git tree mode validation, integration ref advancement, state freshness retry semantics, or submit-for-review diagnostics.
- Guardrails: The plan preserves the post-merge rollback backstop and keeps artifact semantics out of CAS merge mechanics.
