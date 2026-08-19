# Code Plan: Sliced Integration Contract Documentation

## Intent and evidence

Success means the implemented sliced-integration lifecycle is recorded at four durable boundaries: the architectural decision, the internal lifecycle and invariants, the operator/configuration contract, and the architectural-issue registry. Every documentation task waits for the merged end-to-end acceptance task, and issue closure cites the exact acceptance tests and merge commit that justify its disposition.

Based on: `specs/goals/20260818-sliced-integration-analysis.md`; the authoritative master plan; merged sibling planning outputs; ADR-0055 and ADR-0112; the ADR index; `INVARIANTS.md` sections 5-7, 12, 15, and the Protection Matrix; the Update Policy, Open Issues Summary, `Integration Closure Is Not Revalidated`, and historical resolution sections in `specs/architecture/architectural-issues.md`; current integration sections in `specs/architecture/state-machines.md`, `specs/protocols/task-lifecycle.md`, `support-docs/CONFIGURATION.md`, and `support-docs/USAGE_MULTI_AGENTS.md`; and the declared acceptance contract on `code-planning-main-1-code-planning-9-coding-0`.

Doc Impact: Tasks 1-4 own all eight documentation files assigned to this planning scope.

Test Impact: no production tests are added here. The documentation tasks depend on `code-planning-main-1-code-planning-9-coding-0`, preserve its two named acceptance tests as closure evidence, run fail-closed content assertions, and run pre-commit on only their owned files. TDD is not required for documentation-only changes.

ASSUMPTION: none. The acceptance implementation task exists in current state, and each generated task names it through `task_depends_on`, so no documentation work is claimable before the implementation and acceptance evidence merge.

## Documentation architecture and boundaries

```text
Task 10 merged acceptance evidence
                |
       +--------+---------+
       |        |         |
       v        v         v
   ADR-0113  lifecycle  operator contract
       |
       v
 issue-registry disposition
```

Task 1 records the decision and its relationship to ADR-0055 and ADR-0112. Task 2 documents the internal state, evidence, barrier, generation, exhaustion, and linearization rules without duplicating operator procedures. Task 3 documents the persisted generation setting and the actionable frozen-pipeline upgrade path while preserving white-label brand macros. Task 4 applies the issue registry's lifecycle policy only after reading the merged acceptance task's validation and commit evidence.

The documentation must describe source-verified implementation, not merely repeat the proposal. Each coder first reads the merged acceptance task and current production symbols/interfaces named by its scope. If source or acceptance evidence contradicts this plan, the coder stops and reports the contradiction rather than documenting aspirational behavior.

## Dependency and ownership graph

```text
external: code-planning-main-1-code-planning-9-coding-0 (merged)
          |                    |                    |
          v                    v                    v
       Task 1               Task 2               Task 3
       ADR/index        lifecycle/invariants    operator/config
          |
          v
       Task 4
       issue lifecycle
```

Tasks 1-3 own disjoint files and may run in parallel once the acceptance task merges. Task 4 also waits for Task 1 because its evidence-bearing disposition must trace to the new ADR. No file is owned by more than one task.

## Planned coding tasks

### Task 1 — Record ADR-0113 and index it

Description:

```text
Record the implemented sliced integration and final closure architecture in ADR-0113 and index the decision.
```

Done when:

```text
`specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md` explicitly extends ADR-0055, supersedes ADR-0055's accepted no-rescan limitation, records the implemented coverage barrier, immutable evidence, bounded global-generation and exhaustion model, current-HEAD completion linearization, ADR-0112 lock-order preservation, and legacy frozen-pipeline upgrade boundary; `specs/architecture/ADR/README.md` links the exact ADR-0113 filename.
```

Scope:

```text
Own `specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md` and `specs/architecture/ADR/README.md`. Read the merged acceptance task and implemented lifecycle interfaces before writing; preserve ADR-0055 and ADR-0112 as historical records, and do not edit runtime, protocol, operator, invariant, or issue-registry files.
```

Spec ref:

```text
specs/goals/20260818-sliced-integration-analysis.md#documentation-impact
```

External dependency: `code-planning-main-1-code-planning-9-coding-0` supplies merged `TestSlicedIntegrationLifecycle` and `TestSlicedIntegrationFinalizationRace` evidence and transitively gates every implementation provider.

Validation:

```text
rg -q 'ADR-0055|0055' specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md
```

```text
rg -q 'supersed.*no-rescan|no-rescan.*supersed' specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md
```

```text
rg -q '0113-sliced-integration-analysis-and-final-closure' specs/architecture/ADR/README.md
```

```text
pre-commit run --files specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md specs/architecture/ADR/README.md
```

Implementation order: inspect the merged acceptance task and current implementation; write the ADR as an extension/supersession decision rather than rewriting historical ADRs; add the ordered ADR index row; run pre-commit on both owned files before the content assertions.

### Task 2 — Document lifecycle mechanics and invariants

Description:

```text
Document the implemented sliced integration lifecycle, evidence barriers, generation outcomes, and completion invariants.
```

Done when:

```text
`specs/architecture/state-machines.md` distinguishes slice and global analysis phases and describes settled coverage barriers plus persisted coverage evidence; `specs/protocols/task-lifecycle.md` describes attestation-or-slice coverage, repair lineage, repeated global generations, and explicit slice/global exhaustion; `INVARIANTS.md` states idempotent analysis materialization, fail-closed progression, and the linearizable relationship among clean reviewed commit, live integration HEAD, completion, mutation-side invalidation, and ADR-0112 lock ordering.
```

Scope:

```text
Own `specs/architecture/state-machines.md`, `specs/protocols/task-lifecycle.md`, and `INVARIANTS.md`. Verify status names, evidence fields, evaluator outcomes, reconciliation behavior, and mutation ordering against merged source and acceptance evidence; do not edit ADRs, operator/configuration docs, the issue registry, or runtime code.
```

Spec ref:

```text
specs/goals/20260818-sliced-integration-analysis.md#required-properties
```

External dependency: `code-planning-main-1-code-planning-9-coding-0` supplies the merged lifecycle and race evidence.

Validation:

```text
rg -q 'slice.*barrier|coverage.*barrier' specs/architecture/state-machines.md
```

```text
rg -q 'coverage.*evidence|evidence.*coverage' specs/architecture/state-machines.md
```

```text
rg -q 'generation.*exhaust|exhaust.*generation' specs/protocols/task-lifecycle.md
```

```text
rg -q 'lineariz|clean.*commit.*integration HEAD|integration HEAD.*clean.*commit' INVARIANTS.md
```

```text
pre-commit run --files specs/architecture/state-machines.md specs/protocols/task-lifecycle.md INVARIANTS.md
```

Implementation order: inspect the persisted lifecycle, authoritative progress evaluator, reconciliation, completion consumers, and mutation protocol; update the state-machine view; update the operational lifecycle; update the invariant and Protection Matrix entries; run pre-commit before the four content assertions.

### Task 3 — Document operator configuration and upgrade behavior

Description:

```text
Document sliced integration configuration and the operator-facing frozen-pipeline upgrade contract.
```

Done when:

```text
`support-docs/CONFIGURATION.md` documents `max_global_integration_generations` with deterministic default `3`, normalization and exhaustion meaning; `support-docs/USAGE_MULTI_AGENTS.md` contains a `#### Sliced Integration` section describing coverage classification, the global rescan loop, blocked or exhausted outcomes, and that a legacy frozen pipeline or topology requires either a fresh workspace or a manual topology update instead of silently skipping required slices.
```

Scope:

```text
Own `support-docs/CONFIGURATION.md` and `support-docs/USAGE_MULTI_AGENTS.md`. Verify the persisted configuration field, default, capability diagnostics, and operator-visible lifecycle against merged implementation; preserve brand macros and do not edit architecture, protocol, invariant, issue-registry, or runtime files.
```

Spec ref:

```text
specs/goals/20260818-sliced-integration-analysis.md#documentation-impact
```

External dependency: `code-planning-main-1-code-planning-9-coding-0` supplies merged fail-closed frozen-pipeline and generation-exhaustion evidence.

Validation:

```text
rg -q 'max_global_integration_generations.*3' support-docs/CONFIGURATION.md
```

```text
python3 -c 'import pathlib,re; text=pathlib.Path("support-docs/USAGE_MULTI_AGENTS.md").read_text(); match=re.search(r"(?ms)^#### Sliced Integration[ \t]*$\n(?P<body>.*?)(?=^#{1,4}[ \t]|\Z)", text); assert match is not None and re.search(r"legacy frozen (?:pipeline|topology)", match.group("body"), re.I) and re.search(r"fresh workspace", match.group("body"), re.I) and re.search(r"manual topology update", match.group("body"), re.I)'
```

```text
pre-commit run --files support-docs/CONFIGURATION.md support-docs/USAGE_MULTI_AGENTS.md
```

Implementation order: inspect the normalized configuration and pipeline capability result; add the configuration table/reference entry; add the exact sliced-integration operator section under pipeline lifecycle guidance; preserve all brand macros; run pre-commit before the two content assertions.

### Task 4 — Disposition the integration-closure issue

Description:

```text
Disposition the integration-closure architectural issue from merged sliced-integration acceptance evidence.
```

Done when:

```text
`specs/architecture/architectural-issues.md` applies its Update Policy to `Integration Closure Is Not Revalidated`: if the merged acceptance evidence proves the root issue removed, the active entry and Open Issues Summary row move to Completed Fixes, Fixed (Traceability), and Fix Details with ADR-0113, `TestSlicedIntegrationLifecycle`, `TestSlicedIntegrationFinalizationRace`, and the exact implementation merge commit; if evidence leaves a residual risk, the active entry remains synchronized with the summary and carries an explicit `Disposition: Revised` plus the same test and commit traceability. Counts and table of contents remain accurate for the chosen disposition.
```

Scope:

```text
Own `specs/architecture/architectural-issues.md`. Read the merged `code-planning-main-1-code-planning-9-coding-0` task, execute its declared acceptance validation, capture its exact merge commit, and cite ADR-0113; do not claim resolution from task status or prose alone, and do not edit ADR, lifecycle, operator, configuration, invariant, or runtime files.
```

Spec ref:

```text
specs/goals/20260818-sliced-integration-analysis.md#documentation-impact
```

Depends on: Task 1, so the issue disposition can trace to the merged ADR-0113 artifact.

External dependency: `code-planning-main-1-code-planning-9-coding-0` must be MERGED and supplies the exact validation command, named tests, and implementation merge commit.

Validation:

```text
python3 -c 'import pathlib,re; t=pathlib.Path("specs/architecture/architectural-issues.md").read_text(); summary=re.search(r"(?ms)^## Open Issues Summary[ \t]*$\n(?P<body>.*?)(?=^##[ \t]|\Z)",t).group("body"); feedback=re.search(r"(?ms)^## Feedback Loops[ \t]*$\n(?P<body>.*?)(?=^##[ \t]|\Z)",t).group("body"); active=re.search(r"(?ms)^### Integration Closure Is Not Revalidated[ \t]*$\n(?P<body>.*?)(?=^###[ \t]|\Z)",feedback); completed=re.search(r"(?ms)^## Completed Fixes[ \t]*$\n(?P<body>.*?)(?=^##[ \t]|\Z)",t).group("body"); fixed=re.search(r"(?ms)^## Fixed \(Traceability\)[ \t]*$\n(?P<body>.*?)(?=^##[ \t]|\Z)",t).group("body"); details=re.search(r"(?ms)^## Fix Details[ \t]*$\n(?P<body>.*)\Z",t).group("body"); detail=re.search(r"(?ms)^### Integration Closure Is Not Revalidated[ \t]*$\n(?P<body>.*?)(?=^###[ \t]|\Z)",details); row=re.search(r"(?m)^\|[^\n]*Integration Closure Is Not Revalidated[^\n]*\|$",fixed); test=r"TestSlicedIntegration(?:Lifecycle|FinalizationRace)"; commit=r"commit[ \t]+[^0-9a-f\n]*[0-9a-f]{7,40}"; revised=active is not None and "Integration Closure Is Not Revalidated" in summary and re.search(r"(?im)^\*\*(?:Disposition|Status):\*\*[ \t]*Revised\b",active.group("body")) and re.search(test,active.group("body")) and re.search(commit,active.group("body")); resolved=active is None and "Integration Closure Is Not Revalidated" not in summary and "Integration Closure Is Not Revalidated" in completed and row is not None and re.search(test,row.group(0)) and re.search(commit,row.group(0)) and detail is not None and "**Resolution:**" in detail.group("body") and "**Evidence:**" in detail.group("body"); assert revised or resolved'
```

```text
pre-commit run --files specs/architecture/architectural-issues.md
```

Implementation order: query the merged acceptance task; execute its exact declared test command and capture the merge commit; compare the evidence with the active issue; resolve or revise under the Update Policy; synchronize summary/counts, active or completed sections, traceability, and details; run pre-commit before the lifecycle assertion.

## Architecture assessment

The four-way split follows distinct change reasons and ownership boundaries: architectural decision, internal lifecycle contract, operator contract, and evidence-governed registry lifecycle. Splitting further would fragment tightly coupled descriptions within a reader-facing boundary; combining them would create an eight-file task with independent failure modes and prevent safe parallelism. Task 4 alone is ordered after Task 1 because it must cite the ADR; all four are gated directly on the acceptance task so no documentation can precede implementation evidence.

The high-cost error is documenting the proposal instead of the merged behavior. The plan counters it with a source-verification step in every task, exact evidence gating, a fail-closed issue-disposition branch, and content checks that target each assigned obligation. No new architecture decision or issue is introduced by the planning artifacts themselves.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Document the two-level model: bounded local coverage before repeated global closure. | Objective | Tasks 1-3 | Covered |
| 2 | Document approval-attestation evidence for a one-lineage scope without implying persisted reviewer reasoning. | Proposed Model | Tasks 1, 2 | Covered |
| 3 | Document the settled pre-integration boundary and exactly-once contributing-set freeze. | Slice Integration; Required Properties 1-2; Success Criteria 1-2 | Tasks 1, 2 | Covered |
| 4 | Document zero-slice bypass when fewer than two contributing scopes exist. | Slice Integration; Required Property 3; Success Criterion 2 | Tasks 1-3 | Covered |
| 5 | Document bounded coverage for every contributing scope when multiple scopes exist. | Required Property 4; Success Criterion 3 | Tasks 1-3 | Covered |
| 6 | Document one-lineage attestation reuse and no slice creation. | Required Property 5; Success Criterion 3 | Tasks 1, 2 | Covered |
| 7 | Document exactly one slice for each multi-lineage scope with merged work. | Required Property 6; Success Criterion 3 | Tasks 1-3 | Covered |
| 8 | Document that integration-escalation plans remain repair lineage outside the frozen cohort. | Required Property 7; Global Integration | Tasks 1, 2 | Covered |
| 9 | Document lineage attribution for coding, fix, and replacement tasks. | Required Property 8 | Tasks 1, 2 | Covered |
| 10 | Document the bounded slice review surface and immutable analyzed source snapshot. | Required Properties 9-10; Success Criterion 5 | Tasks 1, 2 | Covered |
| 11 | Document that later sibling changes do not reopen completed slices and instead belong to global review. | Slice Integration | Tasks 1, 2 | Covered |
| 12 | Document slice resolution through merged fix/replacement lineage and fail-closed blocked or abandoned findings. | Slice Integration; Required Properties 11, 14 | Tasks 1-3 | Covered |
| 13 | Document every planning, coding, repair, coverage, and slice-resolution barrier before global analysis. | Global Integration; Required Property 11; Success Criterion 4 | Tasks 1-3 | Covered |
| 14 | Document aggregate branch review as independent global judgment, with local coverage as a map rather than proof. | Global Integration; Required Property 12; Success Criterion 6 | Tasks 1-3 | Covered |
| 15 | Document that promoted repairs and later HEAD mutations require another global generation while budget remains. | Global Integration; Required Property 13; Success Criterion 9 | Tasks 1-3 | Covered |
| 16 | Document explicit blocked outcomes for slice exhaustion and explicit exhausted outcomes for the global ceiling. | Final Closure; Required Property 14; Success Criterion 9 | Tasks 1-3 | Covered |
| 17 | Document clean completion bound to an immutable reviewed commit equal to live integration HEAD. | Final Closure; Required Property 15; Success Criterion 7 | Tasks 1, 2 | Covered |
| 18 | Document the single linearizable order between finalization and integration-HEAD mutation. | Final Closure; Required Property 16; Success Criterion 8 | Tasks 1, 2 | Covered |
| 19 | Document mutation-side invalidation of completion tied to a superseded HEAD. | Final Closure; Required Property 17; Success Criterion 8 | Tasks 1, 2 | Covered |
| 20 | Document ADR-0112 lock ordering and the prohibition on blackboard writes while the mutation lock is held. | Final Closure; Required Property 18 | Tasks 1, 2 | Covered |
| 21 | Document `max_global_integration_generations`, its deterministic default `3`, normalization, and exhaustion meaning. | Final Closure; Required Property 19 | Tasks 1, 3 | Covered |
| 22 | Document idempotent wake/restart reconciliation with no duplicate slice or global generations. | Required Property 20; Success Criterion 10 | Tasks 1, 2 | Covered |
| 23 | Preserve stack agnosticism and existing review/merge authorization boundaries in all guidance. | Required Property 21 | Tasks 1-3 | Covered |
| 24 | Document the fail-closed capability result for legacy frozen topology. | Assigned scope; Task 10 acceptance contract | Tasks 1, 3 | Covered |
| 25 | State that legacy frozen pipelines require a fresh workspace or manual topology update. | Assigned scope; Documentation Impact | Tasks 1, 3 | Covered |
| 26 | Add ADR-0113 extending ADR-0055 and superseding its no-rescan limitation. | Documentation Impact 1-2 | Task 1 | Covered |
| 27 | Update state-machine and task-lifecycle documentation. | Documentation Impact 3 | Task 2 | Covered |
| 28 | Update invariant, pipeline, operational, configuration, barrier, generation, and terminal-outcome documentation. | Documentation Impact 4; assigned done-when | Tasks 2, 3 | Covered |
| 29 | Change the integration-closure issue only after implementation and validation evidence exists. | Documentation Impact 5 | Task 4 | Covered |
| 30 | Preserve evidence-bearing issue traceability under the registry Update Policy. | Architectural issue Update Policy; assigned done-when | Task 4 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: upstream `code-planning-main-1-code-planning-9-coding-0` owns and gates the two acceptance tests | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | Tasks 1-4 | Covered |

## Pre-submit audit

- Four atomic documentation tasks own disjoint file sets; only Task 4 depends on Task 1 for ADR traceability.
- Every task has the existing acceptance implementation task in `task_depends_on`; no task can be claimed before Task 10 evidence is merged.
- Task descriptions, done-when clauses, scopes, spec refs, validations, plan ref, sibling dependency, and external dependency are copied character-for-character into `output[]`.
- The issue task must execute the acceptance task's declared validation and use its exact merge commit; task status or prose alone cannot close the issue.
- Each assigned canonical content assertion is owned by exactly one task, and each task runs pre-commit only on its owned files.
- All in-scope functional properties, success criteria, documentation obligations, E2E impact, and documentation impact are mapped with no GAP.
- No implementation file, runtime log artifact, historical ADR, or unrelated architectural issue is writable in this plan.
