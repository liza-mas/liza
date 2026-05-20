# PRD: Candidate-Tree Artifact Reference Protection

Status: draft

## Goal

Prevent Liza integration merges from advancing the integration ref when the
candidate tree removes or invalidates any artifact path referenced by current
`.liza/state.yaml`.

## Context

Liza state can record durable artifact paths in `.liza/state.yaml`, including
goal `spec_ref`, task `spec_ref`, `epic_ref`, `plan_ref`, `arch_ref`, and the
same fields on `output[]` entries. These paths are inputs to downstream agents
and validation.

During task rescoping, cleanup, or merge-hygiene work, the Orchestrator can
remove an artifact that appears unrelated to the immediate task while another
task still references that path in state. `liza validate` correctly fails
afterward, and `wt-merge` currently rolls back after post-merge artifact
validation, but the bad candidate is only rejected after the integration ref has
already advanced and rollback has become normal control flow.

The missing control is an integration-time invariant over the candidate Git tree:
state-referenced artifact paths must survive before the integration ref is
advanced.

## General Information

Applies to: Liza artifact references stored in `.liza/state.yaml` and evaluated
during `wt-merge`.

### References

- Code: `internal/statevalidate/validate.go` - existing `ValidateArtifactRefs`
  traversal and filesystem/integration fallback validation.
- Code: `internal/ops/wt_merge.go` - `performCASMerge`, `MergeWorktree`, and
  post-merge artifact validation.
- Spec: `INVARIANTS.md` - current post-merge artifact-reference rollback
  invariant.
- Spec: `specs/protocols/worktree-management.md` - integration protocol and
  rollback behavior.
- Spec: `specs/architecture/blackboard-schema.md` - artifact-ref field
  definitions and validation semantics.
- Incident report: user-supplied causal chain describing architecture artifacts
  committed to integration, later removed while correcting a contaminated
  task-review diff, while downstream `DRAFT_CODING_PLAN` tasks still referenced
  the deleted `arch_ref` paths.

### Non-Functional Requirements

- NFR-000-1: The guard must run before advancing the integration ref for normal
  merge paths, so rollback remains a backstop rather than the primary protection.
- NFR-000-2: The guard must preserve existing CAS retry safety for concurrent
  merges.
- NFR-000-3: Diagnostics must be deterministic and actionable, naming the
  invalid artifact path and its state owner provenance.
- NFR-000-4: The solution must not make `performCASMerge` depend on blackboard
  state or artifact-ref semantics.
- NFR-000-5: The existing post-merge `ValidateArtifactRefs` backstop must remain
  in place.

### Related External Components

- Component C-000-1 - Git object database and refs, accessed through Liza's Git
  wrapper.
- Component C-000-2 - `.liza/state.yaml`, accessed through the blackboard state
  model.

### Interfaces

- Interface I-000-1 - `statevalidate` artifact-ref collection: returns
  normalized artifact refs with owner provenance for validation and merge guards.
- Interface I-000-2 - `performCASMerge` pre-update hook: accepts an optional
  callback that validates the candidate treeish before `UpdateRef`.

### Out of Scope

- Detecting semantic corruption of artifact contents. Replacing content at the
  same path is out of scope unless the path no longer resolves to a regular
  file.
- Adding artifact content hashes, producer metadata, or versioned artifact refs.
- Making `submit-for-review` the authoritative cross-task protection gate.
- Protecting arbitrary repository files not referenced by Liza state artifact
  fields.
- Allowing symlinks as valid durable artifacts.

### Assumptions

- **ASM-000-1**: Liza artifact refs should resolve to regular Git files with
  mode `100644` or `100755`. Symlinks are rejected even though Git stores them
  as blobs. *Why*: durable state refs should point directly to artifacts, not
  indirections. Confidence: HIGH.
- **ASM-000-2**: `wt-merge` is the authoritative protection point for cross-task
  deletion. *Why*: only candidate integration has the full tree needed to detect
  one task deleting artifacts referenced by another task. Confidence: HIGH.

### Open Questions

- None.

---

## Feature FT-001 - Protect Artifact Refs in Candidate Integration Trees

### References

- Code: `internal/statevalidate/validate.go` - current artifact-ref traversal.
- Code: `internal/ops/wt_merge.go` - CAS merge loop and post-merge validation.
- Spec: `specs/architecture/blackboard-schema.md` - artifact-ref fields and
  current validation semantics.

### Functional Requirements

- FR-001-1: Liza must expose a reusable artifact-ref collector from
  `statevalidate`.
- FR-001-2: The collector must return normalized repo-relative paths with any
  `#fragment` stripped.
- FR-001-3: The collector must return owner provenance for each ref, including
  field name, owning task ID when applicable, and output index when applicable.
- FR-001-4: The collector output must be deterministic, sorted by path and owner
  metadata.
- FR-001-5: `ValidateArtifactRefs` must use the collector instead of duplicating
  artifact-ref traversal.
- FR-001-6: The collector must fail closed for invalid artifact refs that cannot
  be checked safely against a candidate Git tree, including semicolon-joined
  refs, empty paths after stripping `#fragment`, path traversal outside the
  repository, and absolute paths that cannot be converted to repo-relative paths.
- FR-001-7: Existing valid absolute refs, if supported by current validation,
  must be normalized to repo-relative paths before candidate-tree checks or
  rejected with actionable diagnostics.
- FR-001-8: Liza must validate every protected artifact path against the
  candidate integration treeish before advancing the integration ref.
- FR-001-9: Candidate validation must require each protected path to exist as a
  regular Git file with mode `100644` or `100755`.
- FR-001-10: Candidate validation must reject missing paths, directories,
  submodules, symlinks with mode `120000`, and any other non-regular Git object
  mode.
- FR-001-11: Rejection diagnostics must include the invalid path and deterministic
  owner provenance.
- FR-001-12: The artifact guard must read the latest available state for each
  hook invocation and collect protected refs from that state snapshot.
- FR-001-13: State changes that occur after a hook passes remain covered by
  post-merge `ValidateArtifactRefs`.
- FR-001-14: If candidate-tree validation fails, the artifact guard must re-read
  state, recollect protected refs, and revalidate the same candidate tree before
  returning an artifact error.
- FR-001-15: If the second state snapshot no longer references the previously
  failing path and the candidate validates against the second snapshot, the hook
  must return success for that invocation.
- FR-001-16: If state cannot be re-read for failure confirmation, the hook must
  fail closed with an error that preserves the candidate-tree validation failure
  and reports that state freshness could not be verified.
- FR-001-17: `performCASMerge` must accept an optional narrow pre-update hook
  with signature equivalent to `func(candidateTreeish string) error`.
- FR-001-18: The pre-update hook must run inside each CAS attempt after that
  attempt computes the candidate treeish and before that attempt calls
  `UpdateRef`.
- FR-001-19: `performCASMerge` must treat the hook as opaque and own CAS retry
  semantics. The artifact-guard hook may close over or load protected artifact
  refs; `performCASMerge` must not know about blackboard state or artifact-ref
  semantics.
- FR-001-20: The pre-update hook must be skipped for the already-merged no-op
  path.
- FR-001-21: For fast-forward merges, the pre-update hook must validate
  `expectedCommit` before `UpdateRef`.
- FR-001-22: For true merges, the pre-update hook must validate the candidate
  merge tree or merge commit tree before `UpdateRef`.
- FR-001-23: If the hook fails, `performCASMerge` must re-read the integration
  ref before returning the hook error.
- FR-001-24: If the integration ref changed after the candidate was computed,
  `performCASMerge` must discard the hook error and retry the CAS loop.
- FR-001-25: If the integration ref is unchanged after hook failure,
  `performCASMerge` must return the hook error.
- FR-001-26: If the integration ref cannot be re-read after hook failure,
  `performCASMerge` must return an error that preserves the hook failure and
  reports that staleness could not be verified.
- FR-001-27: If the hook passes but `UpdateRef` fails with a CAS conflict,
  `performCASMerge` must retry and re-run the hook against the recomputed
  candidate.
- FR-001-28: Existing post-merge `ValidateArtifactRefs` validation must remain
  after a successful ref update.
- FR-001-29: `submit-for-review` checks, if added, must be treated as optional
  diagnostics and not as the authoritative cross-task guard.

### Non-Functional Requirements

- NFR-001-1: Candidate checks should use `git ls-tree <treeish> -- <path>` or an
  equivalent Git wrapper operation that can distinguish missing paths from
  non-regular object modes.
- NFR-001-2: `performCASMerge` must not know about blackboard state,
  artifact-ref provenance, or artifact validation policy.
- NFR-001-3: Error messages must be stable enough for tests to assert without
  relying on map iteration order.

### Acceptance Criteria

- AC-001-1: Given a true merge candidate deletes a file referenced by another
  task's `arch_ref`, when `wt-merge` evaluates the candidate, then the merge is
  rejected before the integration ref advances.
- AC-001-2: Given a true merge candidate deletes a file referenced by task
  `plan_ref`, `spec_ref`, or `epic_ref`, when `wt-merge` evaluates the
  candidate, then the merge is rejected before the integration ref advances.
- AC-001-3: Given a true merge candidate deletes a file referenced by
  `output[].arch_ref`, `output[].plan_ref`, `output[].spec_ref`, or
  `output[].epic_ref`, when `wt-merge` evaluates the candidate, then the merge
  is rejected before the integration ref advances.
- AC-001-4: Given a fast-forward candidate would delete a referenced artifact,
  when `wt-merge` evaluates the candidate, then the fast-forward is rejected
  before the integration ref advances.
- AC-001-5: Given a candidate deletes, renames, or otherwise removes the file
  referenced by `goal.spec_ref`, when `wt-merge` evaluates the candidate, then
  the merge is rejected before the integration ref advances.
- AC-001-6: Given a candidate uses `git mv` or equivalent rename behavior on a
  referenced artifact without rewriting the state ref, when `wt-merge` evaluates
  the candidate, then the old referenced path is treated as missing and the
  merge is rejected. This is a rename-specific regression case for Git diff/tree
  behavior, not a separate semantic rule from deletion.
- AC-001-7: Given a candidate replaces a referenced artifact path with a
  directory, submodule, symlink, or other non-regular object, when `wt-merge`
  evaluates the candidate, then the merge is rejected.
- AC-001-8: Given the collector encounters an invalid artifact ref that cannot
  be safely checked against a candidate tree, when `wt-merge` evaluates the
  candidate, then the merge is rejected with an actionable invalid-ref
  diagnostic.
- AC-001-9: Given candidate-tree validation fails for refs from the first state
  snapshot but a second state snapshot no longer protects the failing path and
  validates against the same candidate, when the hook confirms state freshness,
  then the hook returns success for that invocation.
- AC-001-10: Given candidate-tree validation fails and state cannot be re-read
  for failure confirmation, when the hook handles the failure, then it returns
  an error preserving the candidate-tree failure and the failed state freshness
  check.
- AC-001-11: Given the pre-update hook fails and integration HEAD has changed
  since the candidate was computed, when `performCASMerge` handles the hook
  failure, then it retries instead of returning the stale hook error.
- AC-001-12: Given the pre-update hook fails and integration HEAD cannot be
  re-read, when `performCASMerge` handles the hook failure, then it returns an
  error preserving the hook failure and the failed staleness check.
- AC-001-13: Given the pre-update hook passes but `UpdateRef` returns a CAS
  conflict, when `performCASMerge` handles the conflict, then it retries and
  re-runs the hook against the recomputed candidate.
- AC-001-14: Given `expectedCommit` is already merged into integration, when
  `performCASMerge` detects the no-op path, then it returns without running the
  hook.
- AC-001-15: Given a ref update succeeds, when `MergeWorktree` continues after
  `performCASMerge`, then post-merge `ValidateArtifactRefs` still runs and can
  still mark the task `INTEGRATION_FAILED` with rollback if full-state artifact
  validation fails.
- AC-001-16: Given candidate artifact validation rejects a path, when the error
  is reported, then the diagnostic names the invalid path and at least one
  deterministic owner, including task ID and artifact field where applicable.

### Depends On

Implementation ordering:

- FR-001-1 through FR-001-7 before candidate-tree guard diagnostics, so merge
  errors can report artifact provenance.
- FR-001-17 before FR-001-18 through FR-001-27, so the CAS loop has a hook point
  to run the guard.

### Interfaces

- `statevalidate.CollectArtifactRefs(state)` or equivalent: returns normalized
  artifact refs with deterministic owner provenance.
- `performCASMerge` optional pre-update hook: validates a candidate treeish and
  returns an error when the candidate violates protected artifact refs.

### Out of Scope

- Artifact content hashing or semantic artifact validation.
- Submission-time cross-task guarantees.
- State schema changes beyond any types needed to expose collected refs and
  provenance in code.

### Assumptions

- **ASM-001-1**: A regular file mode check is sufficient for path-liveness
  protection. *Why*: this feature protects path disappearance and invalid object
  replacement, not artifact meaning. Confidence: HIGH.

### Open Questions

- None.
