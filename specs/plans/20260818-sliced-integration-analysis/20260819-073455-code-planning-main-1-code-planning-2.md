# Code Plan: Sliced Integration Pipeline Capability

## Intent and evidence

Make the frozen pipeline boundary explicit: new workspaces expose a dedicated
slice integration lifecycle beside the existing global integration lifecycle,
while older frozen pipelines remain unchanged and report that sliced coverage
requires an upgrade.

Success means the embedded pipeline has independently resolvable slice and
global integration role-pairs plus finding-to-fix transitions, and loading a
pre-slice frozen pipeline preserves its topology while returning an actionable
`pipeline_upgrade_required` capability result.

Based on: `specs/goals/20260818-sliced-integration-analysis.md` (Objective,
Slice Integration, Required Properties, Success Criteria, and Out of Scope);
the parent plan's Task 3 boundary in
`specs/plans/20260818-sliced-integration-analysis/20260819-005612-code-planning-main-1.md`;
ADR-0035, ADR-0045, ADR-0055, and ADR-0067; `INVARIANTS.md` sections 3, 6-8,
and the Protection Matrix; the relevant open issues in
`specs/architecture/architectural-issues.md`; Stacklit module orientation;
Semble frozen-pipeline search; and direct reads of the pending worktree's
embedded pipeline, load/migration/resolver paths, focused tests, and pipeline
test helper.

ASSUMPTION: none. The parent plan fixes the compatibility policy and sole-owned
interface; this plan only resolves the implementation seam inside that scope.

Doc Impact: this plan artifact only. Operator and architecture documentation is
owned by parent-plan Task 11 and is excluded here.

Test Impact: add the two named behavioral tests and strengthen existing focused
pipeline tests where the new embedded topology changes their expectations.

## Architecture and boundary

```text
embedded pipeline (new workspace) ----> slice + global role-pairs available
                                               |
                                               +--> each approved finding set
                                                    auto-fans to coding-pair

frozen pipeline (existing workspace) --> allowed-operation migration only
                                               |
                                               +--> typed capability result
                                                    pipeline_upgrade_required
```

The existing `integration-pair` remains the global integration pair. Add
`slice-integration-pair` as a specialization that reuses the existing
`integration-analyst` and `integration-reviewer` roles. This avoids new roles
and preserves existing global integration consumers while making slice and
global analysis distinguishable by role-pair and lifecycle state.

`Resolver.SlicedIntegrationCapability` is the sole semantic boundary for
callers. It returns a typed `SlicedIntegrationCapability` result containing:

- an availability boolean;
- a stable outcome code (empty when available,
  `pipeline_upgrade_required` when unavailable); and
- guidance that tells the operator to use a fresh workspace or perform a
  manual frozen-topology update.

Availability is fail-closed. It is true only when the resolver can identify the
slice role-pair and its approved-to-coding auto/per-subtask transition beside
the existing global pair and global finding transition. Missing or partial
slice topology returns the upgrade result; it is not treated as a no-slice
workflow.

`LoadFrozen` continues to apply only `MigrateOperations`. Neither
`applyCompatibilityDefaults` nor operation migration may copy role-pairs,
states, sub-pipeline steps, or transitions from the embedded reference.

## Planned coding tasks

### Task 1 — Expose sliced pipeline capability

Description: Make sliced integration capability explicit for both new embedded pipelines and legacy frozen pipelines.

Done when: `TestSlicedIntegrationPipelineTopology` proves new embedded pipelines expose distinct slice/global role-pairs and finding-to-fix transitions; `TestSlicedIntegrationPipelineLegacyFrozenUpgrade` proves an existing frozen pipeline is not topology-backfilled and reports an actionable `pipeline_upgrade_required` capability result instead of skipping slice coverage.

Scope: Own `internal/embedded/pipeline.yaml`, `internal/pipeline/config.go`, `internal/pipeline/config_test.go`, `internal/pipeline/migrate.go`, `internal/pipeline/migrate_test.go`, `internal/pipeline/resolver.go`, `internal/pipeline/resolver_test.go`, and `internal/testhelpers/pipeline.go`. Reuse existing analyst/reviewer roles, keep distinct lifecycle state names, retain allowed-operation migration, and do not create tasks or edit operator documentation.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#slice-integration`

Depends on: None.

Validation: `go test -json ./internal/pipeline -run '^(TestSlicedIntegrationPipelineLegacyFrozenUpgrade|TestSlicedIntegrationPipelineTopology)$' -count=1 | jq -e -s '([.[] | select(.Action == "pass" and (.Test == "TestSlicedIntegrationPipelineLegacyFrozenUpgrade" or .Test == "TestSlicedIntegrationPipelineTopology")) | .Test] | unique | sort) == ["TestSlicedIntegrationPipelineLegacyFrozenUpgrade","TestSlicedIntegrationPipelineTopology"] and all(.[]; .Action != "fail")'`

Implementation notes:

1. Extend `internal/embedded/pipeline.yaml` with
   `slice-integration-pair`, task slug `sia`, and slice-prefixed initial,
   executing, submitted, reviewing, approved, rejected, and clean states. Keep
   every slice state distinct from the existing `integration-pair` states.
2. Keep `integration-pair` as the global pair. Add
   `slice-integration-to-fix` from `slice-integration-pair.approved` to
   `coding-pair.initial`, with `trigger: auto`,
   `cardinality: per-subtask`, and the existing fix task slug. Retain the
   existing `integration-to-fix` transition for global findings.
3. Define the typed `SlicedIntegrationCapability` result in
   `internal/pipeline/config.go` and implement
   `Resolver.SlicedIntegrationCapability` in
   `internal/pipeline/resolver.go`. Check the complete required topology, not
   merely one map key, so partially updated frozen files also fail closed.
4. Preserve the narrow migration contract in `internal/pipeline/migrate.go`:
   role `allowed-operations` may be additive, but topology is never copied from
   the embedded reference into a frozen config.
5. Add or extend a byte-content pipeline setup helper in
   `internal/testhelpers/pipeline.go` so tests can install an explicit legacy
   frozen pipeline without deriving it from the newly embedded topology.
6. Write `TestSlicedIntegrationPipelineTopology` against the embedded config.
   Assert role reuse, distinct lifecycle states, both clean terminals, both
   approved-to-fix transitions, transition targets/triggers/cardinalities, and
   an available capability result. Update existing embedded task-slug and
   resolver expectations only where the added slice topology changes them.
7. Write `TestSlicedIntegrationPipelineLegacyFrozenUpgrade` from the existing
   pre-slice integration fixture. Assert `LoadFrozen` retains the legacy
   `integration-pair`, does not add the slice pair, slice transition, or
   sub-pipeline step, still performs an intentionally exercised
   allowed-operation migration, and returns the exact upgrade code plus both
   fresh-workspace and manual-update guidance.

TDD order: add both named tests first and observe their behavioral failures;
then add the minimum topology, capability, and helper changes needed to make
them pass. Existing assertions must not be weakened to accept missing slice
coverage.

## Ownership and dependency audit

Task 1 owns every file in this specialized scope, so there are no shared-file
edges or sibling dependency indices. It produces the parent-plan interface
`SlicedIntegrationCapability` for later progress/reconciliation tasks but does
not consume any unmerged sibling interface.

Out of scope: contributing-set derivation, task creation, integration evidence,
generation budgeting, prompt rendering, completion gating, end-to-end tests,
and operator/architecture documentation. Those remain with parent-plan Tasks
1-2 and 4-11.

## Risks and validation focus

- A presence-only capability check could certify partial topology. The named
  legacy test must remove or omit the slice pair/transition and assert the
  fail-closed result.
- Reusing global status strings for the slice pair would violate config state
  uniqueness and erase phase identity. The topology test compares the complete
  state sets.
- General topology migration would silently change existing workspace safety
  semantics. The legacy test compares loaded role-pairs, transitions, and steps
  while separately proving allowed-operation migration still occurs.
- A passing `go test -run` with no matching tests is a false green. The
  canonical JSON-event predicate requires both exact top-level pass events and
  rejects every fail event.

## Spec Compliance Matrix

This matrix covers every goal requirement allocated to this specialized
pipeline task by the reviewed parent plan. Requirements assigned to sibling
tasks are intentionally not re-planned here.

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | A multi-lineage contributing scope can use a dedicated slice integration pair. | Slice Integration: "receives exactly one slice analysis" | Task 1 | Covered |
| 2 | Slice findings continue through the existing integration reviewer and coding-pair fix lifecycle. | Slice Integration: findings lifecycle | Task 1 | Covered |
| 3 | Slice and global analyses remain distinguishable without introducing new agent roles. | Out of Scope: no new roles; parent Task 3 scope | Task 1 | Covered |
| 4 | New embedded pipelines declare distinct slice/global lifecycles and finding-to-fix transitions. | Parent-plan Task 3 done-when | Task 1 | Covered |
| 5 | Existing frozen pipeline topology is not rewritten when embedded topology changes. | ADR-0067, Consequences | Task 1 | Covered |
| 6 | A frozen pipeline missing sliced topology fails closed with `pipeline_upgrade_required`. | Parent-plan architecture and Task 3 done-when | Task 1 | Covered |
| 7 | Upgrade guidance identifies both a fresh workspace and manual topology update. | Parent-plan frozen-pipeline policy | Task 1 | Covered |
| 8 | Existing additive allowed-operation migration remains intact. | Parent-plan Task 3 scope; `LoadFrozen` contract | Task 1 | Covered |
| 9 | Workflow stays stack-agnostic and preserves review/merge authorization boundaries. | Required Properties | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: parent-plan Task 10 owns the sliced lifecycle integration test and consumes this capability | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: parent-plan Task 11 owns the frozen-pipeline operator and architecture documentation | N/A |

## Pre-submit self-check

- One cohesive coding task owns the availability contract's positive and
  negative sides; splitting them would duplicate `config.go`, `resolver.go`,
  and their tests and would serialize two changes to one interface.
- The task's done-when is falsifiable through two exact behavioral tests.
- The output entry is one-to-one with Task 1 and copies description, done-when,
  scope, spec ref, and validation verbatim.
- No shared-file dependency or external concrete-task dependency is required.
- E2E and documentation are explicitly owned by parent-plan Tasks 10 and 11.
