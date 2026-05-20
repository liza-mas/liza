# Code Plan: Statevalidate Artifact Ref Collector And Provenance

Status: draft

## Source Context

Based on:

- Goal spec: `specs/goals/20260520-artifact-ref-protection.md`, especially FR-001-1 through FR-001-7 and FR-001-11.
- Architecture plan: `specs/arch-plan/20260520-artifact-ref-protection/20260520-025848-architecture-1.md`.
- Task state and rejection feedback for `architecture-1-code-planning-0`.
- Guardrails: `GUARDRAILS.md` G1.2 and ADR index `specs/architecture/ADR/README.md`.

ASSUMPTION: The implementation can add `internal/statevalidate/artifact_refs.go` and `internal/statevalidate/artifact_refs_test.go` without conflicting with existing package organization. This is reversible and local to `internal/statevalidate`.

Doc Impact: N/A - this scope exposes an internal validation API and refactors an existing internal backstop. The goal spec and architecture plan already define the behavior; candidate-tree user-visible behavior is owned by sibling planning tasks.

Test Impact: Required in implementation tasks. Unit tests must cover collector normalization/provenance and `ValidateArtifactRefs` collector consumption; end-to-end candidate-tree rejection belongs to sibling guard and acceptance planning scopes.

## Implementation Strategy

Add a collector API in `internal/statevalidate` that centralizes protected artifact-ref traversal for goal refs, task refs, and output refs. The collector returns one item per owning state field, not one item per unique path, because downstream diagnostics need deterministic owner provenance.

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

Normalization should reuse `paths.SplitRefFile` for fragment stripping, but should not use `paths.NormalizeSpecRef` as the authoritative collector normalizer because `NormalizeSpecRef` intentionally preserves unsafe values for downstream validation. Implement the safe collector normalizer inside `statevalidate` unless a generic helper is strictly smaller and remains stack-agnostic with no state ownership knowledge.

Invalid refs should fail closed during collection with owner-aware diagnostics. Extend the existing `ArtifactRefError` where practical instead of creating an unrelated error family, preserving compatibility with `jsonout.ErrorDetails` and existing callers. Add stable cause strings for empty paths after fragment stripping, traversal outside the repo, and unsafe absolute paths. Existing `multiple_refs_not_supported` and `file_not_found` causes should remain recognizable.

`ValidateArtifactRefs` should call `CollectArtifactRefs` once and perform existence checks over the collected normalized paths only. Raw absolute refs must not be passed to Git pathspecs. The existing post-merge call in `internal/ops/wt_merge.go` remains in place; this plan does not add candidate-tree validation or CAS hook mechanics.

## Planned Coding Tasks

### Task 1: Collector API, Normalization, Provenance, And Invalid-Ref Tests

desc: Add the reusable statevalidate artifact-ref collector API with normalization, deterministic owner provenance, invalid-ref diagnostics, and unit tests.

done_when: `internal/statevalidate` exposes a collector callable from package tests that returns one normalized repo-relative fragment-free `ArtifactRef` per goal/task/output artifact owner; owner metadata includes field name, task ID when applicable, and output index when applicable; collector output is sorted by path and owner metadata; semicolon-joined refs, fragment-only or empty refs after fragment stripping, traversal outside the repo, and unsafe absolute refs fail closed with owner-aware `ArtifactRefError` diagnostics; valid absolute refs under `projectRoot` normalize to repo-relative paths; and focused statevalidate tests assert FR-001-1 through FR-001-4, FR-001-6, FR-001-7, and the invalid-ref portion of FR-001-11.

scope: Create `internal/statevalidate/artifact_refs.go` and `internal/statevalidate/artifact_refs_test.go`; update `internal/statevalidate/validate.go` only as needed to extend `ArtifactRefError` fields, cause constants, formatting, and `SafeDetails`; use existing `internal/paths` helpers where appropriate without changing candidate-tree or CAS merge behavior. Out of scope: modifying `internal/ops/wt_merge.go`, adding Git tree mode checks, changing state lifecycle rules, or wiring a pre-update hook.

spec_ref: specs/goals/20260520-artifact-ref-protection.md

plan_ref: specs/plans/20260520-artifact-ref-protection/20260520-111928-architecture-1-code-planning-0.md

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

plan_ref: specs/plans/20260520-artifact-ref-protection/20260520-111928-architecture-1-code-planning-0.md

depends_on: ["0"]

Implementation notes:

- Introduce a small existence helper that accepts a collected `ArtifactRef` or owner metadata so missing-file diagnostics retain provenance without re-parsing field names.
- Keep `checkSpecFileExists` and existing scalar validation behavior stable for call sites in task validation and task-output mutation commands.
- The integration-branch fallback must call Git with `integrationBranch + ":" + collected.Path`, never with the raw ref.
- Add regression tests showing an absolute in-repo raw value is collected as a relative path and then uses the normalized path for integration-branch fallback.
- Add tests for goal `spec_ref`, task `plan_ref` or `arch_ref`, and `output[].plan_ref` or `output[].arch_ref` missing-file diagnostics including field, task ID, and output index where applicable.

Validation commands for the coder:

- `go test ./internal/statevalidate -run 'TestValidateArtifactRefs|TestCheckSpecFileExists|TestCheckArtifactRefFileExists|TestCollectArtifactRefs'`
- `go test ./internal/ops -run TestMergeWorktree_RejectsDeletingReferencedArtifact`
- `go test ./internal/statevalidate ./internal/jsonout ./internal/ops`

## Dependency And Shared-File Audit

| File or package | Task 1 | Task 2 | Dependency required |
|-----------------|--------|--------|---------------------|
| `internal/statevalidate/validate.go` | Extends `ArtifactRefError` and cause constants | Refactors `ValidateArtifactRefs` and helpers | Yes: Task 2 depends on output[0] |
| `internal/statevalidate` tests | Adds collector-focused tests | Adds validator-consumption tests | Yes: Task 2 depends on output[0] |
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
| 8 | Rejection diagnostics include invalid path and deterministic owner provenance. | Goal spec FR-001-11; NFR-000-3; NFR-001-3 | Task 1, Task 2 | Covered |
| 9 | Collector is the `statevalidate` artifact-ref collection interface used by validation and future merge guards. | Goal spec I-000-1 and Interfaces | Task 1, Task 2 | Covered |
| 10 | Existing post-merge `ValidateArtifactRefs` backstop remains in place. | Goal spec NFR-000-5; FR-001-28; AC-001-15 | Task 2 | Covered |
| 11 | Invalid collector refs must reject merge evaluation with actionable diagnostics once consumed by the future guard. | Goal spec AC-001-8 | Task 1 | Covered |
| 12 | Rejected artifact diagnostics name the invalid path and at least one deterministic owner, including task ID and field where applicable. | Goal spec AC-001-16 | Task 1, Task 2 | Covered |
| 13 | Preserve `performCASMerge` separation from blackboard state and artifact-ref semantics. | Goal spec NFR-000-4; NFR-001-2 | Task 1, Task 2 | Covered |
| 14 | Candidate-tree Git object mode checks, CAS hook mechanics, integration ref advancement, and state freshness retry semantics are out of scope for this code plan. | Assigned task scope; architecture plan Scope 1 boundary | Task 1, Task 2 | Covered |
| 15 | Unit tests cover FR-001-1 through FR-001-7 and FR-001-11. | Assigned done_when; architecture plan Required Test Cases | Task 1, Task 2 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this scope exposes an internal collector and refactors the existing post-merge validator; end-to-end candidate-tree rejection belongs to sibling guard and acceptance planning tasks. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: no user-facing command or configuration is added in this scope; the goal spec and architecture plan already document the new internal interface. | N/A |

## Pre-Submit Self-Check

- Task decomposition: Task 1 owns the reusable collector contract; Task 2 owns the existing validator's consumption of that collector. TDD is colocated with each behavior change.
- Dependency order: Task 2 depends on output[0] because it consumes the collector and may share `internal/statevalidate/validate.go`.
- Output parity: Task 2 dependency is written as `depends_on: ["0"]`, matching the task-output JSON field exactly.
- Scope boundary: No task plans `performCASMerge`, candidate Git tree mode validation, integration ref advancement, state freshness retry semantics, or submit-for-review diagnostics.
- Shared-file audit: `internal/statevalidate/validate.go` and statevalidate tests are shared, so Task 2 depends on output[0].
- Guardrails: The plan preserves the post-merge rollback backstop and keeps artifact semantics out of CAS merge mechanics.
