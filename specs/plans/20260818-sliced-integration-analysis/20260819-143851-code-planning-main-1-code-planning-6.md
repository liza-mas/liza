# Code Plan: Phase-Aware Immutable Integration Review Context

## Intent and evidence

Render one immutable prompt projection for each persisted integration analysis:
slice analyses receive only their originating plan's attributable evidence,
while global analyses receive compact local-coverage navigation plus an
independent goal-wide diff bound to the analyzed source commit.

Success means the analyst and reviewer prompts derive phase and scope from the
current task's persisted `IntegrationAnalysisMetadata`, never rediscover
lineage from all merged tasks, never use moving `HEAD` for phase-aware analysis,
and give slice and global reviewers different responsibilities.

Based on:

- `specs/goals/20260818-sliced-integration-analysis.md` (Slice Integration,
  Global Integration, Required Properties, and Success Criteria 5-6).
- The parent plan's Task 7 boundary in
  `specs/plans/20260818-sliced-integration-analysis/20260819-005612-code-planning-main-1.md`.
- The replacement reconciliation contract in
  `specs/plans/20260818-sliced-integration-analysis/20260819-134730-code-planning-main-1-replan-1-code-planning-2.md`.
- Current task state from
  `liza get code-planning-main-1-code-planning-6 --json`, which directly orders
  `code-planning-main-1-replan-1-code-planning-2` and
  `code-planning-main-1-code-planning-2` before this task with no reverse edge.
- ADR-0016, ADR-0026, ADR-0048, ADR-0055, ADR-0058, and current source in
  `internal/ops/proceed.go` plus `internal/embedded/pipeline.yaml`.
- `INVARIANTS.md` sections 5-8 and its Protection Matrix; the relevant
  prompt-coupling, cross-pair knowledge, context-pressure,
  integration-closure, and prompt-drift issues in
  `specs/architecture/architectural-issues.md`.
- Stacklit module orientation, Semble discovery, and direct reads of the
  current prompt adapter, prompt DTO, integration templates, focused legacy
  tests, persisted lifecycle model, and sliced-pipeline capability resolver.

Load-bearing claims:

- **EVIDENCED — upstream implementation order:** the authoritative task state
  makes this planning task depend directly on the replacement reconciliation
  planning task and the sliced-pipeline capability planning task. The
  replacement task no longer depends on this task. Checked against targeted
  `liza get ... --json` reads for both tasks.
- **EVIDENCED — generated-child dependency mechanism:** all three tasks are
  `code-planning-pair` sources for the same `code-plan-to-coding`
  `per-subtask` transition. `ExecuteAvailableTransitions` topologically sorts
  pending transitions by source-task `DependsOn`, executes the upstream
  transitions first, and `computeInheritedDeps` then copies every upstream
  child created by that same transition onto the downstream child. Checked
  against `internal/embedded/pipeline.yaml:596-601` and
  `internal/ops/proceed.go:1012-1174,1356-1390`.
- **EVIDENCED — output dependency encoding:** output-local `depends_on` is
  empty because there is one output. `task_depends_on` is also empty because
  ADR-0058 accepts only already-existing concrete task IDs, while the
  replacement reconciliation coding children are created at the shared
  transition. The parent planning-task edge is therefore the mechanism that
  produces the required inherited coding-task edges.
- **EVIDENCED — prompt boundary:** `IntegrationAnalysisMetadata` persists phase,
  originating plan, roots, descendant task/commit attribution, source commit,
  affected paths, and snapshot paths; the goal lifecycle persists the compact
  coverage union. Checked against `internal/models/integration.go`.

ASSUMPTION: none. Upstream contracts own analysis identity, attribution,
coverage, and source-commit facts; this task only projects those facts into a
bounded prompt.

Doc Impact: this plan artifact only. Parent-plan Task 11 owns user-facing and
architecture documentation for sliced integration.

Test Impact: add the two named end-to-end prompt tests plus focused renderer
coverage in the two new integration test files. Preserve existing legacy
prompt tests without weakening their assertions.

## Architecture and boundary

```text
persisted IntegrationAnalysisMetadata on analysis task
                         |
                         v
       buildTaskRoleContextData (projection only)
                         |
                         v
       phase-aware RoleContextData prompt view
                  /                 \
                 v                   v
       slice context            global context
  plan + descendants +       compact coverage map +
  attributed commits/paths   independent aggregate diff
                 \                   /
                  v                 v
          phase-specific reviewer instructions
```

`internal/agent` owns state-to-prompt projection. It may resolve task IDs and
copy fields named by persisted analysis metadata, but it must not infer a
contributing set, walk unrelated merged tasks, classify lineages, call
reconciliation, or choose a generation. Missing referenced evidence must fail
closed rather than widening the review surface.

`internal/prompts` owns a prompt-only view and rendering. Keep it independent
of new `internal/ops` calls; do not deepen the documented `prompts -> ops`
coupling. Phase-aware commands use the persisted source commit as their
immutable target. The legacy `GoalBaseCommit`/`CompletedTasks` projection may
remain only for pre-metadata compatibility required by existing tests; it must
not be selected when valid phase-aware metadata is present.

For a slice, render the originating plan and architecture references,
metadata-named descendant tasks and their acceptance criteria, attributable
commits and paths, declared ownership/dependency/interface decomposition, and
per-path `git show <source-commit>:<path>` guidance. Do not render unrelated
tasks, sibling paths, a goal-wide diff, or a moving `HEAD` boundary.

For a global analysis, render one compact coverage row per contributing scope,
distinguishing reused coding-review attestations from slice evidence, then
render an independent aggregate changed-file map/stat/targeted-diff boundary
from the goal base commit to the persisted source commit. State explicitly that
coverage is navigation evidence, not proof of aggregate correctness.

The reviewer block branches on persisted phase: slice review checks intra-plan
composition and shared intent without reopening sibling/global concerns;
global review checks cross-scope interaction, shared interfaces, aggregate
specification/tests, architectural drift, emergent risk, omissions, and
goal-level merge readiness. Both review the analyst output against the same
immutable source commit the analyst received.

Architecture review: the stable boundary is persisted lifecycle data flowing
one way into presentation DTOs and templates. The primary failure modes are a
leaky recomputation of lifecycle policy, prompt drift to moving `HEAD`, and
coverage evidence replacing independent global judgment. The minimum coherent
change keeps one state adapter and one prompt view, adds no new abstraction or
dependency, and is reversible within the six owned files. No new architecture
issue is introduced; the existing integration-closure issue remains open until
the separately owned lifecycle and end-to-end work is complete.

## Planned coding tasks

### Task 1 — Render phase-aware immutable integration context

Description: Render phase-aware immutable review context for slice and global integration analyses.

Done when: `TestSliceIntegrationContext` proves slice prompts contain only the originating plan boundary, descendant acceptance criteria, attributable commits and paths, decomposition metadata, and snapshot reads at the source commit; `TestGlobalIntegrationContext` proves global prompts contain the compact coverage map plus an independent aggregate diff and phase-specific reviewer instructions.

Scope: Own `internal/agent/prompt.go`, `internal/agent/prompt_integration_test.go`, `internal/prompts/role_context.go`, `internal/prompts/role_context_integration_test.go`, `internal/prompts/templates/blocks/branch_integration_context.tmpl`, and `internal/prompts/templates/blocks/review_instructions.tmpl`. Consume persisted analysis metadata; do not classify lineages, create tasks, or alter wake decisions.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#slice-integration`

Dependencies: no sibling output indices and no `task_depends_on`. The source
planning task directly depends on
`code-planning-main-1-replan-1-code-planning-2` and
`code-planning-main-1-code-planning-2`; because all use the same
`code-plan-to-coding` transition, ADR-0048/topological transition execution
creates the upstream coding children first and adds all of them to this
output's generated coding task. This gates prompt implementation on both
replacement reconciliation outputs, including the persisted metadata producer
and `ReconcileIntegrationAnalyses`, plus the sliced-pipeline capability output.

Validation: `go test -json ./internal/agent ./internal/prompts -run '^(TestGlobalIntegrationContext|TestSliceIntegrationContext)$' -count=1 | jq -e -s '([.[] | select(.Action == "pass" and (.Test == "TestGlobalIntegrationContext" or .Test == "TestSliceIntegrationContext")) | .Test] | unique | sort) == ["TestGlobalIntegrationContext","TestSliceIntegrationContext"] and all(.[]; .Action != "fail")'`

Implementation notes:

1. Write `TestSliceIntegrationContext` and `TestGlobalIntegrationContext` first
   in `internal/agent/prompt_integration_test.go`. Build final analyst and
   reviewer role context from realistic task/state fixtures containing both
   relevant and distracting scopes. Assert positive content and explicit
   absence of unrelated task IDs, paths, commits, and moving `HEAD` commands.
2. Add focused rendering coverage in
   `internal/prompts/role_context_integration_test.go` for the prompt-only view,
   compact coverage rows, immutable command shapes, and phase-specific reviewer
   language. Use behavioral string assertions strong enough to fail if slice
   and global instructions collapse to the same generic branch review.
3. Extend `RoleContextData` with the minimum prompt-view fields needed to carry
   upstream metadata. Reuse `models.DecompositionManifest` rather than
   re-encoding ownership, dependency, and interface fields. Keep persistent
   lifecycle types in `internal/models`; prompt structs are presentation-only.
4. In `buildTaskRoleContextData`, select phase-aware projection only from the
   assigned analysis task's persisted metadata. Resolve only metadata-named
   plan/descendant/coverage evidence, preserve deterministic upstream ordering,
   clone slices before rendering, and return an error for missing or
   contradictory required references. Do not call `collectCompletedTasks` for
   a metadata-backed analysis.
5. Consume `Resolver.SlicedIntegrationCapability` only as a fail-closed guard
   for slice rendering. An unavailable or incomplete capability must not turn a
   slice into a global prompt. Treat `ReconcileIntegrationAnalyses` as the
   upstream producer contract; prompt construction remains read-only and does
   not invoke reconciliation.
6. Rewrite the phase-aware branch context in
   `branch_integration_context.tmpl`. Slice commands read affected paths from
   the persisted source commit; global commands compare goal base to that same
   source commit. Preserve bounded map/stat/targeted-path guidance and never
   teach an unbounded full diff.
7. Rewrite only the integration-reviewer branch of
   `review_instructions.tmpl` to use the same phase and source boundary as the
   analyst. Preserve verdict, resubmission, validation-satisfiability, and
   fix-task decomposition instructions that are common to both phases.
8. Preserve the legacy no-metadata rendering path needed by existing prompt
   tests, but make metadata-backed rendering take precedence. Run the focused
   named validation, then full tests for `./internal/agent` and
   `./internal/prompts`, and pre-commit on all six owned files. If a Go test
   failure reports stale embedded assets, use the documented worktree
   `make -C <worktree> sync-embedded` prerequisite rather than changing tests.
9. Compare template byte counts before and after. Prefer replacing the generic
   integration prose with shared compact clauses plus short phase branches;
   do not duplicate the full reviewer workflow for slice and global phases.

TDD order: add the two named failing prompt tests before production changes;
implement the minimum projection and rendering to satisfy them; then add only
focused lower-level cases needed to protect fail-closed metadata and legacy
compatibility. Existing assertions must not be weakened to accept `HEAD`,
unattributable tasks, or coverage-map-only global review.

## Ownership and dependency audit

Task 1 owns all six files in this specialized scope, so there are no shared-file
edges or sibling dependency indices. The single task is cohesive: separating
the state adapter from its prompt view would create a new cross-task interface
whose only consumer is the same acceptance test and would make neither partial
change useful.

Consumed interfaces are `IntegrationAnalysisMetadata persistence schema`,
`SlicedIntegrationCapability`, and the deterministic metadata produced by
`ReconcileIntegrationAnalyses`. The task owns only `slice analysis prompt
projection`, `global analysis prompt projection`, and `phase-aware integration
reviewer instructions`.

The authoritative planning graph supplies the required external ordering. The
replacement reconciliation planning task is a direct predecessor, not a
downstream consumer, and its two coding outputs are inherited by this coding
output at the shared `code-plan-to-coding` transition. No direct reference to
future child IDs is encoded in `output[]`.

Out of scope: contributing-set or lineage classification, lifecycle mutation,
analysis task creation, verdict persistence, wake/completion decisions,
integration-ref mutation, end-to-end lifecycle tests, and documentation.

## Risks and validation focus

- Recomputing descendants from all merged tasks would make a slice context
  non-attributable. The slice fixture includes a convincing unrelated sibling
  and asserts every sibling identifier and path is absent.
- Using `HEAD` would let the prompt drift after task creation. Both named tests
  assert exact source-commit commands and reject phase-aware `..HEAD` or
  `show HEAD:` forms.
- Treating coverage as correctness would reproduce the overloaded global
  review failure. The global test requires both compact coverage and a separate
  aggregate diff plus independent-review instructions.
- A generic reviewer block would erase responsibility boundaries. Tests assert
  slice-only intra-plan language and global-only cross-scope/goal-readiness
  language.
- Missing inherited implementation edges would expose absent upstream
  interfaces. Pre-submit evidence must re-check the direct planning edge, the
  shared transition, and same-transition inheritance semantics.
- Prompt growth competes with agent context. Byte comparison and replacement
  of generic prose constrain growth without removing existing verdict safety
  instructions.
- A passing `go test -run` with no matching tests is a false green. The
  canonical JSON-event predicate requires both exact top-level pass events and
  rejects every fail event.

## Spec Compliance Matrix

This matrix covers every goal requirement allocated to this specialized prompt
task by the reviewed parent plan. Requirements owned by sibling lifecycle,
reconciliation, E2E, and documentation tasks are intentionally not re-planned.

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | A slice receives its originating code plan and architecture references. | Slice Integration: analyst context | Task 1 | Covered |
| 2 | A slice receives only metadata-named descendant coding tasks and their acceptance criteria. | Slice Integration: analyst context; Required Property 9 | Task 1 | Covered |
| 3 | A slice receives commits and affected paths attributable to those descendants. | Slice Integration: analyst context | Task 1 | Covered |
| 4 | A slice receives declared ownership, dependency, and interface decomposition metadata. | Slice Integration: analyst context | Task 1 | Covered |
| 5 | A slice receives immutable source-state reads for affected paths at the analysis snapshot commit. | Slice Integration: analyst context and immutable binding | Task 1 | Covered |
| 6 | Slice responsibility is intra-plan composition and shared-intent satisfaction. | Slice Integration: responsibility | Task 1 | Covered |
| 7 | Later sibling changes do not widen or reopen a completed slice prompt. | Slice Integration: immutable slice binding | Task 1 | Covered |
| 8 | Global context contains a bounded coverage record for every contributing scope. | Global Integration: bounded coverage map | Task 1 | Covered |
| 9 | Coverage records distinguish reused coding-review attestations from slice reports without becoming proof of aggregate correctness. | Proposed Model; Global Integration | Task 1 | Covered |
| 10 | Global analysis independently inspects the aggregate branch at its persisted source commit. | Required Property 12; Success Criterion 6 | Task 1 | Covered |
| 11 | Global reviewer instructions cover cross-scope interactions, shared interfaces, aggregate tests/specs, architectural drift, emergent risk, omissions, and goal-level merge readiness. | Global Integration: analyst responsibilities | Task 1 | Covered |
| 12 | Slice and global prompts consume persisted metadata without classifying lineages, creating tasks, or altering wake decisions. | Parent-plan Task 7 scope and ownership graph | Task 1 | Covered |
| 13 | Phase-aware context remains bounded and role-specific rather than duplicating a goal-wide task dump. | Objective; ADR-0026; parent-plan coverage note | Task 1 | Covered |
| 14 | Workflow remains stack-agnostic and preserves existing review and merge authorization boundaries. | Required Properties | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: parent-plan Task 10 owns the complete sliced lifecycle integration tests and consumes these prompt projections | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: parent-plan Task 11 owns sliced-integration architecture and operator documentation | N/A |

## Pre-submit self-check

- One cohesive coding task covers one observable behavior: the immutable,
  phase-aware prompt projection. Slice and global are the required two variants
  of that interface, not independent deployable changes.
- The done-when is falsifiable through two exact end-to-end prompt tests that
  exercise the owned agent projection and prompt renderer together.
- The output entry is one-to-one with Task 1 and copies description, done-when,
  scope, spec ref, and validation verbatim.
- No output-local shared-file edge exists. The direct predecessor task and
  shared-transition inheritance produce the required reconciliation and
  capability coding-task edges; `task_depends_on` is intentionally absent.
- Every allocated functional requirement and constraint has a Covered row; E2E
  and documentation ownership remain explicit in the parent plan.
