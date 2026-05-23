# Code Plan: Master Planning Documentation and ADR

Status: draft

## Goal

Plan the documentation-only coding work that updates user-facing docs, maintainer architecture docs, schema references, pipeline configuration docs, and the ADR index for the master planning task pattern without creating invalid dependencies on downstream coding tasks.

## Based On

- Goal spec: `specs/goals/20260523-master-planning-task.md`
- Architecture plans:
  - `specs/arch-plan/20260523-master-planning-task/20260523-024555-architecture-1.md`
  - `specs/arch-plan/20260523-master-planning-task/20260523-103617-architecture-2-a.md`
  - `specs/arch-plan/20260523-master-planning-task/20260523-104359-architecture-2-b.md`
  - `specs/arch-plan/20260523-master-planning-task/20260523-105319-architecture-3-b.md`
  - `specs/arch-plan/20260523-master-planning-task/20260523-102450-architecture-4.md`
  - `specs/arch-plan/20260523-master-planning-task/20260523-110234-architecture-5.md`
- Prior code plans and output summaries:
  - `architecture-2-a-code-planning-0`
  - `architecture-2-b-code-planning-0-repair-0`
  - `architecture-3-b-code-planning-0`
  - `architecture-4-code-planning-0-a`
  - `architecture-4-code-planning-0-b-repair-0`
  - `architecture-4-code-planning-0-c-repair-0`
  - `architecture-4-code-planning-0-d-repair-0`
  - `architecture-5-code-planning-0`
- Current documentation surfaces inspected with `rg` and targeted reads:
  - `README.md`
  - `support-docs/USAGE_MULTI_AGENTS.md`
  - `specs/architecture/overview.md`
  - `specs/architecture/roles.md`
  - `specs/architecture/state-machines.md`
  - `specs/architecture/blackboard-schema.md`
  - `specs/build/2 - Sub-pipelines and spec writing.md`
  - `specs/architecture/ADR/README.md`
  - `specs/architecture/ADR/0066-architecture-subpipeline-entry-points.md`

## Architectural Notes

The documentation change is a single cohesive surface: it explains one accepted topology and metadata pattern across user, maintainer, schema, and ADR documents. Splitting by document would create parallel edits to shared terminology and likely inconsistent wording. One documentation task keeps the prose, diagrams, schema examples, and ADR relationship reviewable together.

The task intentionally declares no `task_depends_on`. It consumes already-merged planning artifacts and does not need to wait on draft or implementing coding tasks to update documentation. This avoids invalid dependency edges to downstream coding work while still letting the coder cite the finalized goal and architecture plans.

## Planned Tasks

### Task 1: Master planning documentation and ADR update

**desc:** Update user, maintainer, schema, and ADR documentation for the master planning task pattern, including master role-pairs, one-task INITIAL_PLANNING routing, master decomposition responsibilities, quorum-2 states, typed decomposition metadata, artifact propagation, Case A bypass, and the explicit no-runtime-migration boundary for existing frozen workspaces.

**scope:** Update `README.md`, `support-docs/USAGE_MULTI_AGENTS.md`, `specs/architecture/overview.md`, `specs/architecture/roles.md`, `specs/architecture/state-machines.md`, `specs/architecture/blackboard-schema.md`, `specs/build/2 - Sub-pipelines and spec writing.md` or the closest pipeline config schema doc section, `specs/architecture/ADR/README.md`, and a new ADR such as `specs/architecture/ADR/0067-master-planning-task-pattern.md`. Document master role-pairs, one-task INITIAL_PLANNING routing, simple versus fan-out behavior, master task decomposition responsibility, quorum-2 master states, typed decomposition metadata, artifact-ref propagation, Case A bypass, and the explicit non-migration boundary for existing frozen workspaces. Consume the goal spec and architecture plans; do not implement code, tests, migration tooling, or changes under `.liza/agent-outputs/`.

**done_when:** User-facing docs explain that simple entry-point work creates one specialized planning task while fan-out or uncertain work creates one mapped master task; pipeline diagrams or prose show master role-pairs before specialized epic, architecture, and code-planning pairs without implying master tasks always run; role docs describe master doer/reviewer responsibilities and that the same roles are reused by master and specialized pairs; state-machine docs list or describe the new master states, quorum-2 partially-approved and reviewing-2 flow, and claimability impact; blackboard/schema docs document `role-pairs.<name>.decomposition-root`, output-entry and task-level `decomposition` metadata fields, required master `plan_ref` or `arch_ref` output refs, read-only dependency metadata, and scheduling dependency mirroring; docs preserve Case A `architecture-to-code-plan` bypass and downstream `epic_ref` behavior; docs state existing frozen `.liza/pipeline.yaml` workspaces are not migrated and users must re-run `liza init` to receive the new topology; a new ADR records the master planning task pattern decision, consequences, alternatives, and relationship to ADR-0066, and `ADR/README.md` links it.

**spec_ref:** `specs/goals/20260523-master-planning-task.md`

**plan_ref:** `specs/plans/20260523-master-planning-task/20260523-172921-architecture-5-code-planning-1-repair-0.md`

**depends_on:** none

**task_depends_on:** none

**TDD:** N/A. This is a documentation-only task. The coder should use pre-commit plus content checks rather than code tests.

**Suggested validation:**

- `rg -n "master|decomposition-root|INITIAL_PLANNING|architecture-to-code-plan|pipeline.yaml|plan_ref|arch_ref|epic_ref" README.md support-docs/USAGE_MULTI_AGENTS.md specs/architecture/overview.md specs/architecture/roles.md specs/architecture/state-machines.md specs/architecture/blackboard-schema.md 'specs/build/2 - Sub-pipelines and spec writing.md' specs/architecture/ADR/README.md specs/architecture/ADR/0067-master-planning-task-pattern.md`
- `pre-commit run --files README.md support-docs/USAGE_MULTI_AGENTS.md specs/architecture/overview.md specs/architecture/roles.md specs/architecture/state-machines.md specs/architecture/blackboard-schema.md 'specs/build/2 - Sub-pipelines and spec writing.md' specs/architecture/ADR/README.md specs/architecture/ADR/0067-master-planning-task-pattern.md`

## Shared-File Audit

This plan emits one output entry, so there are no sibling shared-file hazards. The task owns all listed documentation and ADR files for the master planning pattern update. It must not edit implementation code, tests, migration tooling, or `.liza/agent-outputs/`.

The prior acceptance plan `architecture-5-code-planning-0` owns executable validation files and does not share documentation files with this plan. The domain implementation plans own Go code, prompt templates, tests, and pipeline fixtures, not the documentation files listed here.

## Dependency Audit

No sibling `depends_on` is required because there is only one output entry.

No `task_depends_on` is declared. The documentation task is allowed to proceed from the merged goal and architecture planning artifacts. It should not depend on draft or implementing downstream coding tasks, because that would risk invalid dependency-direction edges and would contradict this repair task's scope.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Every planning fan-out is preceded by a consolidated master task unless upstream consolidation already exists. | Goal spec `Design / One Rule` | Task 1 | Covered |
| 2 | Entry-point simple work creates exactly one specialized planning task; fan-out or uncertain work creates exactly one mapped master task. | Goal spec `Case A / Case B Routing`; `Orchestrator Behavior Change`; Success criteria 2 and 8 | Task 1 | Covered |
| 3 | Pipeline documentation shows master role-pairs before specialized epic, architecture, and code-planning pairs without implying master tasks always run for simple work. | Goal spec `Step Split`; Architecture 5 Scope 2 done_when | Task 1 | Covered |
| 4 | Documentation preserves Case A `architecture-to-code-plan` bypass to `code-planning-pair` and the unchanged specialized architecture source. | Goal spec `Case A / Case B Routing`; Success criterion 4 | Task 1 | Covered |
| 5 | Documentation preserves downstream specialized epic output `epic_ref` behavior for `us-writing-pair`. | Goal spec `Artifact Reference Propagation`; Success criterion 6 | Task 1 | Covered |
| 6 | Role docs describe master doer responsibility for general approach, boundaries, interfaces, shared ownership, dependency ordering, typed `output[]`, and coverage proof. | Goal spec `Prompt Differentiation`; `Master Output Contract`; Architecture 3-b | Task 1 | Covered |
| 7 | Role docs describe master reviewer responsibility for decomposition coherence, manifest obligations, artifact refs, systemic-thinking evidence, and Master Output Contract properties 1-6. | Goal spec `Prompt Differentiation`; `Reviewer acceptance criteria`; Architecture 3-b | Task 1 | Covered |
| 8 | Role docs state the same doer/reviewer roles are reused by master and specialized pairs, with master behavior selected by `decomposition-root: true`. | Goal spec `Role-Pairs`; `Prompt Differentiation`; Architecture 1 and 3-b | Task 1 | Covered |
| 9 | State-machine docs list or describe the three master role-pair state sets, including `partially-approved` and `reviewing-2` states. | Goal spec `Role-Pairs`; Success criterion 5 | Task 1 | Covered |
| 10 | State-machine docs explain quorum-2 flow: first approval reaches partially approved, second approval reaches approved, and second-review states affect reviewer claimability. | Goal spec `Role-Pairs`; Success criterion 5; Architecture 5 Scope 2 | Task 1 | Covered |
| 11 | Schema docs document `role-pairs.<name>.decomposition-root` and that it is valid only for a role-pair with an outgoing same-subpipeline auto per-subtask decompose transition. | Goal spec `Typed Decomposition Manifest`; Architecture 1 | Task 1 | Covered |
| 12 | Schema docs document output-entry and task-level `decomposition` metadata fields: `owned_files`, `owned_modules`, `read_only_depends_on`, `read_only_task_depends_on`, `interfaces_owned`, `interfaces_consumed`, and `coverage_notes`. | Goal spec `Typed Decomposition Manifest`; Architecture 4 | Task 1 | Covered |
| 13 | Schema docs document read-only dependency metadata as descriptive and require mirroring to scheduler dependencies through `depends_on` and `task_depends_on`. | Goal spec `Master Output Contract`; Architecture 4 | Task 1 | Covered |
| 14 | Schema docs document required master artifact refs: `plan_ref` for epic-planning and code-planning masters, `arch_ref` for architecture masters. | Goal spec `Artifact Reference Propagation`; Architecture 4 | Task 1 | Covered |
| 15 | Schema docs distinguish task-level inherited refs from output-entry produced refs. | Goal spec `Artifact Reference Propagation` | Task 1 | Covered |
| 16 | Documentation states existing frozen `.liza/pipeline.yaml` workspaces are not migrated and users must re-run `liza init` to receive the new topology. | Goal spec `Out of Scope / Migration of existing workspaces`; Architecture 5 Scope 2 | Task 1 | Covered |
| 17 | Pipeline config documentation updates the declarative pipeline schema/prose for master role-pairs, auto decompose transitions, entry points, and frozen pipeline behavior. | Goal spec `Step Split`; Documentation Impact; Architecture 5 Scope 2 | Task 1 | Covered |
| 18 | User-facing docs update entry-point and pipeline-flow descriptions. | Goal spec `Documentation Impact`; Architecture 5 Scope 2 | Task 1 | Covered |
| 19 | `support-docs/USAGE_MULTI_AGENTS.md` updates pipeline step descriptions, entry-point documentation, and spawn-count guidance. | Goal spec `Documentation Impact`; Architecture 5 Scope 2 | Task 1 | Covered |
| 20 | `specs/architecture/overview.md` updates subpipeline descriptions. | Goal spec `Documentation Impact`; Architecture 5 Scope 2 | Task 1 | Covered |
| 21 | `specs/architecture/roles.md` updates master decomposition responsibilities. | Goal spec `Documentation Impact`; Architecture 5 Scope 2 | Task 1 | Covered |
| 22 | `specs/architecture/state-machines.md` updates master task states and quorum transitions. | Goal spec `Documentation Impact`; Architecture 5 Scope 2 | Task 1 | Covered |
| 23 | `specs/architecture/blackboard-schema.md` or pipeline schema docs update `decomposition-root` and typed manifest fields. | Goal spec `Documentation Impact`; Architecture 5 Scope 2 | Task 1 | Covered |
| 24 | A new ADR records the master planning task pattern decision, consequences, alternatives, and relationship to ADR-0066. | Goal spec `Documentation Impact`; Architecture 5 Scope 2 | Task 1 | Covered |
| 25 | `specs/architecture/ADR/README.md` links the new ADR. | Architecture 5 Scope 2 done_when | Task 1 | Covered |
| 26 | The documentation task does not implement code, tests, migration tooling, or changes under `.liza/agent-outputs/`. | Assigned scope | Task 1 | Covered |
| 27 | The documentation plan avoids invalid downstream coding dependencies. | Assigned description; Sibling consistency rule | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this repair task is documentation-only; executable acceptance coverage is already owned by `architecture-5-code-planning-0` and its downstream coding outputs. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | Task 1 | Covered |

## Output Parity

The output JSON must contain one entry corresponding to Task 1. Its `desc`, `scope`, `done_when`, `spec_ref`, and `plan_ref` fields must be character-identical to the Task 1 fields above.

## Validation Plan

- Validate the output JSON with `jq`.
- Re-read this plan and the output JSON to verify field parity.
- Run `liza set-task-output architecture-5-code-planning-1-repair-0 --output specs/plans/20260523-master-planning-task/20260523-172921-architecture-5-code-planning-1-repair-0-output.json --agent-id code-planner-2 --json`.
- Run pre-commit on the plan and output JSON files.
- Commit the plan and output JSON.
- Confirm `git -C /home/tangi/Workspace/liza/.worktrees/architecture-5-code-planning-1-repair-0 status --short` is clean.
- Submit `HEAD` with `liza submit-for-review architecture-5-code-planning-1-repair-0 HEAD --agent-id code-planner-2 --json`.
