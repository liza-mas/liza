# Code Plan: Artifact Reference Protection Documentation Updates

Task: `architecture-4-code-planning-1`
Agent: `code-planner-1`
Goal spec: `specs/goals/20260520-artifact-ref-protection.md`
Architecture reference: `specs/arch-plan/20260520-artifact-ref-protection/20260520-121639-architecture-4.md`

## Sources And Scope

Based on:
- Goal spec `specs/goals/20260520-artifact-ref-protection.md`
- Architecture reference `specs/arch-plan/20260520-artifact-ref-protection/20260520-121639-architecture-4.md`
- Prior plans `architecture-3-code-planning-0`, `architecture-3-code-planning-1`, and `architecture-4-code-planning-0`
- Current docs `INVARIANTS.md`, `specs/protocols/worktree-management.md`, and `specs/architecture/blackboard-schema.md`
- Project guardrail G1.2 and ADR index `specs/architecture/ADR/README.md`

This plan is documentation-only. It does not plan production implementation, test implementation, unrelated protocol rewrites, or new standalone documentation.

Doc Impact: `INVARIANTS.md`, `specs/protocols/worktree-management.md`, and `specs/architecture/blackboard-schema.md`.

Test Impact: none for this generated child task because it is documentation-only. Behavior and regression tests are owned by prior implementation and acceptance-test plans.

## Dependency Reasoning

The documentation task depends on the implementation and acceptance coverage tasks so durable docs describe the settled behavior:
- Collector and post-merge validator contracts: `artifact-ref-collector-coding-0`, `artifact-ref-validator-coding-1`
- CAS hook contract and retry behavior: `cas-preupdate-hook-coding-0`, `cas-hook-staleness-coding-1`, `cas-hook-conflict-retry-coding-2`
- Candidate tree Git mode query and validator: `architecture-3-code-planning-0-coding-0`, `architecture-3-code-planning-0-coding-1`
- `MergeWorktree` guard wiring and retained backstop: `architecture-3-code-planning-1-coding-0`
- Acceptance and boundary coverage: `architecture-4-code-planning-0-coding-0`, `architecture-4-code-planning-0-coding-1`, `architecture-4-code-planning-0-coding-2`

No sibling dependency is needed because this plan emits one child task.

## Planned Coding Tasks

### Task 1: Durable Artifact Reference Protection Documentation

desc: Update durable artifact-reference protection documentation.

done_when: `INVARIANTS.md`, `specs/protocols/worktree-management.md`, and `specs/architecture/blackboard-schema.md` describe candidate artifact validation before integration ref advancement, the retained post-merge `ValidateArtifactRefs` rollback backstop, protected goal/task/output artifact fields, scalar repo-relative refs with optional `#fragment` anchors, rejection of missing paths, directories, submodules/gitlinks, symlinks, and other non-regular Git object modes, and deterministic diagnostics that name the invalid path plus owner provenance including field name, task ID when applicable, and output index when applicable.

scope: Modify only `INVARIANTS.md`, `specs/protocols/worktree-management.md`, and `specs/architecture/blackboard-schema.md`. Update existing invariant, integration protocol, validation rules, and artifact-field sections; do not create standalone documentation, change production code, change tests, rewrite unrelated protocols, weaken the retained post-merge `ValidateArtifactRefs` backstop, or redefine artifact-ref semantics beyond the approved candidate-tree protection contract.

spec_ref: specs/goals/20260520-artifact-ref-protection.md

plan_ref: specs/plans/20260520-artifact-ref-protection/20260520-141346-architecture-4-code-planning-1.md

task_depends_on: `artifact-ref-collector-coding-0`, `artifact-ref-validator-coding-1`, `cas-preupdate-hook-coding-0`, `cas-hook-staleness-coding-1`, `cas-hook-conflict-retry-coding-2`, `architecture-3-code-planning-0-coding-0`, `architecture-3-code-planning-0-coding-1`, `architecture-3-code-planning-1-coding-0`, `architecture-4-code-planning-0-coding-0`, `architecture-4-code-planning-0-coding-1`, `architecture-4-code-planning-0-coding-2`

Implementation notes:
- In `INVARIANTS.md`, extend the Worktree & Integration invariant table so it names the pre-update candidate-tree artifact guard as the primary protection and keeps the existing post-merge artifact-reference rollback invariant as a backstop.
- In `specs/protocols/worktree-management.md`, update the Integration Protocol sequence so candidate artifact refs are validated against the candidate treeish before `git update-ref`, while post-merge `ValidateArtifactRefs` remains after successful ref update.
- In `specs/architecture/blackboard-schema.md`, update the artifact-reference field and validation-rule sections to identify the protected goal, task, and output artifact fields; preserve scalar repo-relative ref semantics with optional `#fragment`; and document invalid-ref and object-mode rejection behavior with deterministic owner-aware diagnostics.

## Shared-File Audit

| File | Task 1 | Dependency required |
|------|--------|---------------------|
| `INVARIANTS.md` | Updates Worktree & Integration artifact guard invariants | No sibling task writes this file |
| `specs/protocols/worktree-management.md` | Updates Integration Protocol sequence and rollback wording | No sibling task writes this file |
| `specs/architecture/blackboard-schema.md` | Updates artifact field semantics and validation rules | No sibling task writes this file |

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Durable docs must reflect that candidate trees removing or invalidating state-referenced artifact paths cannot advance the integration ref. | Goal spec lines 7-9 and 25-27 | Task 1 | Covered |
| 2 | Durable docs must identify the protected artifact fields: goal `spec_ref`, task `spec_ref`, `epic_ref`, `plan_ref`, `arch_ref`, and the same fields on `output[]` entries. | Goal spec lines 13-16; AC-001-1 through AC-001-5 lines 199-214 | Task 1 | Covered |
| 3 | Durable docs must preserve the existing post-merge `ValidateArtifactRefs` rollback behavior as a backstop, not the primary guard. | Goal spec lines 20-23, 53-54, 61-62; FR-001-13 lines 144-145; FR-001-28 lines 182-183; AC-001-15 lines 247-250 | Task 1 | Covered |
| 4 | Durable docs must state that candidate-tree validation runs before integration ref advancement for protected refs. | FR-001-8 lines 133-134; NFR-000-1 lines 53-54; Interface I-000-2 lines 75-76 | Task 1 | Covered |
| 5 | Durable docs must describe scalar repo-relative artifact refs with optional `#fragment` anchors and fragment stripping for validation. | FR-001-2 lines 118-119; existing blackboard schema artifact-ref section | Task 1 | Covered |
| 6 | Durable docs must describe invalid artifact refs as fail-closed, including semicolon-joined refs, empty paths after fragment stripping, traversal outside the repository, and unsafe absolute refs. | FR-001-6 lines 126-129; FR-001-7 lines 130-132; AC-001-8 lines 223-226 | Task 1 | Covered |
| 7 | Durable docs must state that valid candidate artifact paths resolve to regular Git file modes `100644` or `100755`. | FR-001-9 lines 135-136; ASM-000-1 lines 91-94; ASM-001-1 lines 280-282 | Task 1 | Covered |
| 8 | Durable docs must state that missing paths, directories, submodules/gitlinks, symlinks, and other non-regular Git object modes are rejected. | FR-001-10 lines 137-139; AC-001-7 lines 220-222; out-of-scope symlink allowance lines 78-87 | Task 1 | Covered |
| 9 | Durable docs must describe deterministic diagnostics that name invalid path and owner provenance. | NFR-000-3 lines 57-58; FR-001-3 lines 120-121; FR-001-11 lines 140-141; AC-001-16 lines 251-253 | Task 1 | Covered |
| 10 | Durable docs must not imply `performCASMerge` owns blackboard state or artifact-ref policy; artifact semantics remain in the guard/validator layer. | FR-001-19 lines 160-163; NFR-000-4 lines 59-60; NFR-001-2 lines 192-193 | Task 1 | Covered |
| 11 | Durable docs must not make `submit-for-review` the authoritative cross-task protection gate. | Out of scope lines 83-85; FR-001-29 lines 184-185 | Task 1 | Covered |
| 12 | Documentation updates must be placed in existing durable docs rather than a new standalone document. | Architecture-4 Documentation Impact Mapper and Interface sections | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this plan is documentation-only; e2e and boundary coverage are owned by merged plan `architecture-4-code-planning-0`. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | Task 1 | Covered |

## Pre-Submit Self-Check

- Task decomposition: one child task, one intent, documentation-only.
- Output parity: `desc`, `done_when`, `scope`, `spec_ref`, and `plan_ref` must be copied verbatim into the output JSON.
- Shared files: no sibling output entries share files because only one child task is emitted.
- Cross-reference consistency: Task 1 explicitly owns every referenced doc responsibility.
- Scope boundary: production code, tests, new standalone docs, unrelated protocol rewrites, and semantic changes outside the approved contract are out of scope.
