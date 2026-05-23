# Code Plan: Master Planning Documentation Updates

## Sources

Based on:
- Task `architecture-5-code-planning-1` from blackboard via `liza get`.
- Goal spec `specs/goals/20260523-master-planning-task.md`.

This file preserves the plan artifact referenced by the task output entries so
post-merge state validation can resolve the declared `plan_ref`.

## Task Graph

The documentation work is split by document ownership so each child task has a
small, non-overlapping scope.

### Task 1: User-facing docs

desc: Update user-facing README and multi-agent usage documentation for master
planning entry routing, pipeline flow, startup role guidance, and frozen-workspace
reinitialization.

scope: Update `README.md` and `support-docs/USAGE_MULTI_AGENTS.md` only.
Document one-task INITIAL_PLANNING routing for simple versus fan-out or
uncertain entry-point work, conditional master-to-specialized flow, reused role
guidance, and the need to re-run `liza init` for existing frozen workspaces.

### Task 2: Maintainer architecture docs

desc: Update maintainer architecture docs for master role-pairs, master
decomposition responsibilities, reused roles, and quorum-2 master state behavior.

scope: Update `specs/architecture/overview.md`, `specs/architecture/roles.md`,
and `specs/architecture/state-machines.md` only. Document decomposition-root
role-pairs, master doer and reviewer responsibilities, role reuse, and
partially-approved plus reviewing-2 quorum flow.

### Task 3: Schema and pipeline docs

desc: Update blackboard and pipeline schema documentation for decomposition-root
role-pair metadata, typed decomposition manifests, artifact-ref propagation,
dependency mirroring, and Case A bypass semantics.

scope: Update `specs/architecture/blackboard-schema.md` and
`specs/build/2 - Sub-pipelines and spec writing.md` only. Document
`role-pairs.<name>.decomposition-root`, output-entry and task-level
`decomposition` fields, required master output refs, read-only dependency
metadata, scheduler-facing dependency mirrors, master artifact propagation, Case
A code-planning bypass, and unchanged specialized `epic_ref` behavior.

### Task 4: ADR

desc: Add the master planning task pattern ADR and link it from the ADR index.

scope: Create `specs/architecture/ADR/0067-master-planning-task-pattern.md` and
update `specs/architecture/ADR/README.md` only. Record the master planning task
pattern decision, quorum-2 review, typed decomposition metadata, artifact
propagation, Case A bypass, no-runtime-migration boundary, alternatives,
consequences, and relationship to ADR-0066.

## Validation

- Documentation tasks should validate only their owned files.
- Runtime code, tests, migration tooling, and `.liza/agent-outputs/` are out of
  scope for these documentation tasks.
