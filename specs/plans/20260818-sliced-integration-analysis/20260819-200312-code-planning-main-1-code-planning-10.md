# Code Plan: Sliced Integration Contract Documentation

## Intent and evidence

Success means the implemented sliced-integration lifecycle is recorded at four durable boundaries: the architectural decision, the internal lifecycle and invariants, the operator/configuration contract, and the architectural-issue registry. Every documentation task waits for the merged end-to-end acceptance task, and issue closure cites both exact acceptance tests and that task's exact merge commit.

Based on: `specs/goals/20260818-sliced-integration-analysis.md`; the authoritative master plan; the current task and prior rejection; ADR-0055 and ADR-0112; the ADR index; `INVARIANTS.md` sections 5-7 and the Protection Matrix; the Update Policy, Table of Contents, Open Issues Summary, active `Integration Closure Is Not Revalidated` entry, and historical resolution sections in `specs/architecture/architectural-issues.md`; current integration sections in `specs/architecture/state-machines.md`, `specs/protocols/task-lifecycle.md`, `support-docs/CONFIGURATION.md`, and `support-docs/USAGE_MULTI_AGENTS.md`; and the declared acceptance contract on `code-planning-main-1-code-planning-9-coding-0`.

Doc Impact: Tasks 1-4 own all eight documentation files assigned to this planning scope.

Test Impact: no production tests are added here. The documentation tasks depend on `code-planning-main-1-code-planning-9-coding-0`; Task 4 reruns its two-test acceptance command; every task runs section-scoped fail-closed content assertions and pre-commit on only its owned files. TDD is not required for documentation-only changes.

ASSUMPTION: none. The acceptance implementation task exists in current state, and every generated task names it through `task_depends_on`, so documentation work is not claimable before implementation and acceptance evidence merge.

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

Task 1 records the architecture and its relationship to ADR-0055 and ADR-0112. Task 2 documents the implemented state, evidence, cohort, barrier, generation, exhaustion, idempotency, and linearization mechanics. Task 3 documents configuration, operator-visible classification and barriers, preserved authorization boundaries, and the frozen-pipeline upgrade path. Task 4 applies the issue registry's lifecycle policy only after rerunning the merged acceptance task's exact tests and resolving its exact `merge_commit` from authoritative state.

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

Tasks 1-3 own disjoint files and may run in parallel once the acceptance task merges. Task 4 also waits for Task 1 because its evidence-bearing disposition must trace to the merged ADR-0113. No file is owned by more than one task.

## Planned coding tasks

### Task 1 — Record ADR-0113 and index it

Description:

```text
Record the implemented sliced integration and final closure architecture in ADR-0113 and index the decision.
```

Done when:

```text
`specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md` explicitly extends ADR-0055 and supersedes its accepted no-rescan limitation; records the implemented two-level model, settled exactly-once contributing cohort, zero-slice bypass, one-lineage approval attestation without persisted reviewer reasoning, exactly one slice for each qualifying multi-lineage scope, bounded immutable coverage evidence, repair lineage outside the frozen cohort, and the rule that later sibling mutations do not reopen slices; records independent global review, bounded rescans and exhaustion, idempotent materialization, clean-current-HEAD completion, mutation-side invalidation, linearizable finalization, ADR-0112 lock ordering with no blackboard write under the mutation lock, and the legacy frozen-pipeline requirement to use a fresh workspace or manual topology update; and `specs/architecture/ADR/README.md` links the exact ADR-0113 filename.
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
python3 -c 'import pathlib,re; t=pathlib.Path("specs/architecture/ADR/0113-sliced-integration-analysis-and-final-closure.md").read_text(); pats=[r"two.level|slice.*global",r"settled.*(?:contributing|cohort)",r"exactly.once",r"fewer.than.two.*(?:no|zero).*slice|zero.slice",r"one.lineage.*approval.attestation",r"(?:not|without).*reviewer.reasoning|reviewer.reasoning.*(?:not|is.not)",r"(?:multi|at.least.two).lineage.*exactly.one.*slice|exactly.one.*slice.*(?:multi|at.least.two).lineage",r"bounded.*(?:coverage|evidence)",r"immutable.*(?:snapshot|commit|evidence)",r"integration.escalation.*repair|repair.lineage.*frozen",r"later.sibling.*(?:not|do.not|does.not).*reopen",r"independent.*global",r"(?:rescan|global.generation)",r"exhaust",r"idempot|duplicate",r"clean.*(?:reviewed|source).*commit",r"integration.HEAD",r"mutation.*invalid",r"lineariz",r"ADR-0112",r"no.blackboard.*write|blackboard.*write.*after.*releas",r"legacy.frozen",r"fresh.workspace",r"manual.topology.update"]; assert all(re.search(p,t,re.I|re.S) for p in pats)'
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
`specs/architecture/state-machines.md` distinguishes slice and global analysis phases and describes the settled coverage barrier, bounded persisted evidence for every contributing scope, blocked fan-in, and explicit slice/global exhaustion; `specs/protocols/task-lifecycle.md` describes exactly-once cohort freeze after planning settles, zero-slice bypass, approval-attestation reuse without persisted reviewer reasoning, exactly one analysis for qualifying multi-lineage scopes, bounded slice review surfaces tied to immutable source snapshots, coding/fix/replacement lineage, integration-escalation repairs outside the cohort, later sibling mutations not reopening slices, merged repair resolution, unresolved blocked or abandoned findings failing closed, and global analysis waiting for settled planning, terminal coding and repair work, complete coverage, and resolved slices; it also describes promoted repairs and later HEAD mutations triggering another bounded global generation, explicit slice/global exhaustion, and duplicate-free wake/restart reconciliation. `INVARIANTS.md` states idempotent analysis materialization, fail-closed progression, independent aggregate global review, and the linearizable relationship among clean reviewed commit, live integration HEAD, completion, mutation-side invalidation, ADR-0112 lock ordering, and no blackboard state write while the mutation lock is held.
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
python3 -c 'import pathlib,re; t=pathlib.Path("specs/architecture/state-machines.md").read_text(); m=re.search(r"(?ms)^### Integration(?:-Pair)? State Machine[^\n]*\n(?P<body>.*?)(?=^### |^## |\Z)",t); assert m is not None; b=m.group("body"); pats=[r"slice",r"global",r"settled",r"coverage.*barrier|barrier.*coverage",r"(?:persisted|immutable|bounded).*evidence|evidence.*(?:persisted|immutable|bounded)",r"blocked",r"slice.*exhaust|exhaust.*slice",r"global.*exhaust|exhaust.*global"]; assert all(re.search(p,b,re.I|re.S) for p in pats)'
```

```text
python3 -c 'import pathlib,re; t=pathlib.Path("specs/protocols/task-lifecycle.md").read_text(); m=re.search(r"(?ms)^## Integration Phase[^\n]*\n(?P<body>.*?)(?=^## |\Z)",t); assert m is not None; b=m.group("body"); pats=[r"planning.*settled|settled.*planning",r"exactly.once.*(?:cohort|contributing)|(?:cohort|contributing).*exactly.once",r"fewer.than.two.*(?:no|zero).*slice|zero.slice",r"one.lineage.*approval.attestation",r"(?:not|without).*reviewer.reasoning|reviewer.reasoning.*(?:not|is.not)",r"(?:multi|at.least.two).lineage.*exactly.one.*slice|exactly.one.*slice.*(?:multi|at.least.two).lineage",r"every.*contributing.*(?:coverage|record)|(?:coverage|record).*every.*contributing",r"bounded.*review.surface",r"immutable.*source.snapshot|source.snapshot.*immutable",r"coding.*fix.*replacement|lineage.*replacement",r"integration.escalation.*repair|repair.*outside.*cohort",r"later.sibling.*(?:not|do.not|does.not).*reopen",r"merged.*(?:fix|repair|replacement)",r"blocked",r"abandoned",r"global.*wait.*planning",r"terminal.*coding",r"terminal.*repair",r"complete.*coverage|coverage.*complete",r"resolved.*slice|slice.*resolved",r"promoted.*repair.*(?:rescan|generation)|(?:rescan|generation).*promoted.*repair",r"later.*(?:HEAD.)?mutation.*(?:rescan|generation)",r"slice.*exhaust|exhaust.*slice",r"global.*exhaust|exhaust.*global",r"wake.*restart.*(?:no|without|duplicate.free).*duplicate|duplicate.free.*wake.*restart"]; assert all(re.search(p,b,re.I|re.S) for p in pats)'
```

```text
python3 -c 'import pathlib,re; t=pathlib.Path("INVARIANTS.md").read_text(); pats=[r"idempot|duplicate.*analysis|analysis.*duplicate",r"fail.closed",r"independent.*aggregate|aggregate.*independent",r"clean.*(?:reviewed|source).*commit",r"integration.HEAD",r"completion",r"lineariz",r"mutation.*invalid|invalid.*mutation",r"ADR-0112",r"no.blackboard.*write|blackboard.*write.*after.*releas"]; assert all(re.search(p,t,re.I|re.S) for p in pats)'
```

```text
pre-commit run --files specs/architecture/state-machines.md specs/protocols/task-lifecycle.md INVARIANTS.md
```

Implementation order: inspect the persisted lifecycle, authoritative progress evaluator, reconciliation, completion consumers, and mutation protocol; update the state-machine view; update the operational lifecycle; update invariant and Protection Matrix entries; run pre-commit before the content assertions.

### Task 3 — Document operator configuration and upgrade behavior

Description:

```text
Document sliced integration configuration and the operator-facing frozen-pipeline upgrade contract.
```

Done when:

```text
`support-docs/CONFIGURATION.md` documents `max_global_integration_generations` with deterministic default `3`, non-positive normalization to `3`, positive-value preservation, and exhaustion meaning; `support-docs/USAGE_MULTI_AGENTS.md` contains a `#### Sliced Integration` section describing the settled barrier, zero-slice bypass, one-lineage approval-attestation coverage without persisted reviewer reasoning, exactly one slice for qualifying multi-lineage scopes, bounded coverage for every contributing scope as navigation rather than aggregate proof, independent global rescans, blocked slice and exhausted generation outcomes, stack-agnostic validation, and unchanged reviewer approval and supervisor merge authority; that section also names the fail-closed `pipeline_upgrade_required` result and states that a legacy frozen pipeline or topology requires either a fresh workspace or a manual topology update instead of silently skipping required slices.
```

Scope:

```text
Own `support-docs/CONFIGURATION.md` and `support-docs/USAGE_MULTI_AGENTS.md`. Verify the persisted configuration field, default, capability diagnostics, operator-visible lifecycle, review/merge authorization boundaries, and stack-agnostic behavior against merged implementation; preserve brand macros and do not edit architecture, protocol, invariant, issue-registry, or runtime files.
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
python3 -c 'import pathlib,re; t=pathlib.Path("support-docs/CONFIGURATION.md").read_text(); pats=[r"max_global_integration_generations",r"(?:default|defaults.to).{0,80}3",r"(?:non.positive|zero.or.negative|<=?.?0).{0,80}(?:normalize|default).{0,80}3",r"positive.{0,80}(?:preserv|remain|surviv)",r"exhaust"]; assert all(re.search(p,t,re.I|re.S) for p in pats)'
```

```text
python3 -c 'import pathlib,re; text=pathlib.Path("support-docs/USAGE_MULTI_AGENTS.md").read_text(); match=re.search(r"(?ms)^#### Sliced Integration[ \t]*$\n(?P<body>.*?)(?=^#{1,4}[ \t]|\Z)", text); assert match is not None and re.search(r"legacy frozen (?:pipeline|topology)", match.group("body"), re.I) and re.search(r"fresh workspace", match.group("body"), re.I) and re.search(r"manual topology update", match.group("body"), re.I)'
```

```text
python3 -c 'import pathlib,re; t=pathlib.Path("support-docs/USAGE_MULTI_AGENTS.md").read_text(); m=re.search(r"(?ms)^#### Sliced Integration[ \t]*$\n(?P<body>.*?)(?=^#{1,4}[ \t]|\Z)",t); assert m is not None; b=m.group("body"); pats=[r"planning.*settled|settled.*barrier",r"fewer.than.two.*(?:no|zero).*slice|zero.slice",r"one.lineage.*approval.attestation",r"(?:not|without).*reviewer.reasoning|reviewer.reasoning.*(?:not|is.not)",r"(?:multi|at.least.two).lineage.*exactly.one.*slice|exactly.one.*slice.*(?:multi|at.least.two).lineage",r"every.*contributing.*coverage|coverage.*every.*contributing",r"coverage.*(?:map|navigation).*(?:not|is.not).*proof|(?:not|is.not).*proof.*coverage",r"independent.*global",r"(?:rescan|global.generation)",r"blocked.*slice|slice.*blocked",r"exhaust",r"pipeline_upgrade_required",r"stack.agnostic",r"review.*(?:approval|boundary)",r"(?:supervisor.*merge|merge.*authority)"]; assert all(re.search(p,b,re.I|re.S) for p in pats); assert re.search(r"\b(?:Liza|LIZA|liza)\b",b) is None'
```

```text
pre-commit run --files support-docs/CONFIGURATION.md support-docs/USAGE_MULTI_AGENTS.md
```

Implementation order: inspect the normalized configuration and pipeline capability result; add the configuration reference; add the exact sliced-integration operator section under pipeline lifecycle guidance; preserve brand macros and existing authorization boundaries; run pre-commit before the content assertions.

### Task 4 — Disposition the integration-closure issue

Description:

```text
Disposition the integration-closure architectural issue from merged sliced-integration acceptance evidence.
```

Done when:

```text
`specs/architecture/architectural-issues.md` applies its Update Policy to `Integration Closure Is Not Revalidated` only after `code-planning-main-1-code-planning-9-coding-0` is MERGED and both `TestSlicedIntegrationLifecycle` and `TestSlicedIntegrationFinalizationRace` pass: if that evidence proves the root issue removed, the active entry and Open Issues Summary and Table of Contents links move to Completed Fixes, Fixed (Traceability), and Fix Details, whose traceability row and detail each cite ADR-0113, both exact test names, and the task's exact `merge_commit`; if evidence leaves residual risk, the active entry remains synchronized with the summary and Table of Contents and carries `Disposition: Revised` plus ADR-0113, both exact test names, and the exact `merge_commit`. The high/medium/low and total Open Issues Summary counts equal the actual summary rows for the chosen disposition.
```

Scope:

```text
Own `specs/architecture/architectural-issues.md`. Read the merged `code-planning-main-1-code-planning-9-coding-0` task, execute its declared two-test acceptance validation, resolve its exact `merge_commit` from authoritative task state, and cite ADR-0113; do not claim resolution from task status or prose alone, and do not edit ADR, lifecycle, operator, configuration, invariant, or runtime files.
```

Spec ref:

```text
specs/goals/20260818-sliced-integration-analysis.md#documentation-impact
```

Depends on: Task 1, so the issue disposition can trace to the merged ADR-0113 artifact.

External dependency: `code-planning-main-1-code-planning-9-coding-0` must be MERGED and supplies the exact validation command, named tests, and implementation merge commit.

Validation:

```text
go test -json ./internal/integration -run '^(TestSlicedIntegrationFinalizationRace|TestSlicedIntegrationLifecycle)$' -count=1 | jq -e -s '([.[] | select(.Action == "pass" and (.Test == "TestSlicedIntegrationFinalizationRace" or .Test == "TestSlicedIntegrationLifecycle")) | .Test] | unique | sort) == ["TestSlicedIntegrationFinalizationRace","TestSlicedIntegrationLifecycle"] and all(.[]; .Action != "fail")'
```

```text
python3 -c 'import pathlib,re; t=pathlib.Path("specs/architecture/architectural-issues.md").read_text(); summary=re.search(r"(?ms)^## Open Issues Summary[ \t]*$\n(?P<body>.*?)(?=^##[ \t]|\Z)",t).group("body"); feedback=re.search(r"(?ms)^## Feedback Loops[ \t]*$\n(?P<body>.*?)(?=^##[ \t]|\Z)",t).group("body"); active=re.search(r"(?ms)^### Integration Closure Is Not Revalidated[ \t]*$\n(?P<body>.*?)(?=^###[ \t]|\Z)",feedback); completed=re.search(r"(?ms)^## Completed Fixes[ \t]*$\n(?P<body>.*?)(?=^##[ \t]|\Z)",t).group("body"); fixed=re.search(r"(?ms)^## Fixed \(Traceability\)[ \t]*$\n(?P<body>.*?)(?=^##[ \t]|\Z)",t).group("body"); details=re.search(r"(?ms)^## Fix Details[ \t]*$\n(?P<body>.*)\Z",t).group("body"); detail=re.search(r"(?ms)^### Integration Closure Is Not Revalidated[ \t]*$\n(?P<body>.*?)(?=^###[ \t]|\Z)",details); row=re.search(r"(?m)^\|[^\n]*Integration Closure Is Not Revalidated[^\n]*\|$",fixed); test=r"TestSlicedIntegration(?:Lifecycle|FinalizationRace)"; commit=r"commit[ \t]+[^0-9a-f\n]*[0-9a-f]{7,40}"; revised=active is not None and "Integration Closure Is Not Revalidated" in summary and re.search(r"(?im)^\*\*(?:Disposition|Status):\*\*[ \t]*Revised\b",active.group("body")) and re.search(test,active.group("body")) and re.search(commit,active.group("body")); resolved=active is None and "Integration Closure Is Not Revalidated" not in summary and "Integration Closure Is Not Revalidated" in completed and row is not None and re.search(test,row.group(0)) and re.search(commit,row.group(0)) and detail is not None and "**Resolution:**" in detail.group("body") and "**Evidence:**" in detail.group("body"); assert revised or resolved'
```

```text
python3 -c 'import json,pathlib,re,subprocess; result=json.loads(subprocess.run(["liza","get","code-planning-main-1-code-planning-9-coding-0","--json"],check=True,capture_output=True,text=True).stdout)["result"]; assert result["status"]=="MERGED" and re.fullmatch(r"[0-9a-f]{40}",result["merge_commit"]); commit=result["merge_commit"]; t=pathlib.Path("specs/architecture/architectural-issues.md").read_text(); section=lambda h,n: re.search(rf"(?ms)^{re.escape(h)}[ \t]*$\n(?P<body>.*?)(?=^{re.escape(n)}[ \t]*$|\Z)",t).group("body"); toc=section("## Table of Contents","## Open Issues Summary"); summary=section("## Open Issues Summary","## Structural Load-Bearing Elements"); feedback=section("## Feedback Loops","## Assumptions"); completed=section("## Completed Fixes","## Fixed (Traceability)"); fixed=section("## Fixed (Traceability)","## Fix Details"); details=re.search(r"(?ms)^## Fix Details[ \t]*$\n(?P<body>.*)\Z",t).group("body"); active=re.search(r"(?ms)^### Integration Closure Is Not Revalidated[ \t]*$\n(?P<body>.*?)(?=^### |\Z)",feedback); detail=re.search(r"(?ms)^### Integration Closure Is Not Revalidated[ \t]*$\n(?P<body>.*?)(?=^### |\Z)",details); row=re.search(r"(?m)^\|[^\n]*Integration Closure Is Not Revalidated[^\n]*\|$",fixed); name="Integration Closure Is Not Revalidated"; link="[Integration Closure Is Not Revalidated](#integration-closure-is-not-revalidated)"; evidence=lambda s: all(x in s for x in ("ADR-0113","TestSlicedIntegrationLifecycle","TestSlicedIntegrationFinalizationRace",commit)); revised=active is not None and link in toc and link in summary and re.search(r"(?im)^\*\*Disposition:\*\*[ \t]*Revised\b",active.group("body")) and evidence(active.group("body")); resolved=active is None and link not in toc and link not in summary and link in completed and row is not None and evidence(row.group(0)) and detail is not None and "**Resolution:**" in detail.group("body") and "**Evidence:**" in detail.group("body") and evidence(detail.group("body")); assert revised or resolved'
```

```text
python3 -c 'import collections,pathlib,re; t=pathlib.Path("specs/architecture/architectural-issues.md").read_text(); s=re.search(r"(?ms)^## Open Issues Summary[ \t]*$\n(?P<body>.*?)(?=^## |\Z)",t).group("body"); actual=collections.Counter(re.findall(r"(?m)^\| \*\*(high|medium|low)\*\* \|",s)); m=re.search(r"\*\*Counts:\*\*[ \t]*(\d+) high, (\d+) medium, (\d+) low[^\d]+(\d+) open issues total",s); assert m is not None; stated=tuple(map(int,m.groups())); assert stated==(actual["high"],actual["medium"],actual["low"],sum(actual.values()))'
```

```text
pre-commit run --files specs/architecture/architectural-issues.md
```

Implementation order: query the merged acceptance task; execute its exact declared test command and capture its exact `merge_commit`; compare the evidence with the active issue; resolve or revise under the Update Policy; synchronize summary, Table of Contents, counts, active or completed sections, traceability, and details; run pre-commit before the lifecycle and count assertions.

## Architecture assessment

The four-way split follows distinct change reasons and reader boundaries: architectural decision, internal lifecycle contract, operator contract, and evidence-governed registry lifecycle. Splitting further would fragment tightly coupled descriptions; combining them would create an eight-file task with independent failure modes and prevent safe parallelism. Task 4 alone is ordered after Task 1 because it must cite ADR-0113; all four are gated directly on the acceptance task so no documentation can precede implementation evidence.

The high-cost error is documenting the proposal instead of merged behavior. The plan counters it with source verification, exact evidence gating, a fail-closed issue-disposition branch, and section-scoped assertions that correspond one-for-one with each done-when obligation. The software-architecture-review lens found no new architecture decision or persistable issue introduced by these planning artifacts.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Document the two-level model: bounded local coverage before repeated global closure. | Objective | Task 1 | Covered |
| 2 | Document approval-attestation evidence for a one-lineage scope without implying persisted reviewer reasoning. | Proposed Model | Tasks 1-3 | Covered |
| 3 | Document the settled pre-integration boundary and exactly-once contributing-set freeze. | Slice Integration; Required Properties 1-2; Success Criteria 1-2 | Tasks 1-2 | Covered |
| 4 | Document zero-slice bypass when fewer than two contributing scopes exist. | Slice Integration; Required Property 3; Success Criterion 2 | Tasks 1-3 | Covered |
| 5 | Document bounded coverage for every contributing scope when multiple scopes exist. | Required Property 4; Success Criterion 3 | Tasks 2-3 | Covered |
| 6 | Document one-lineage attestation reuse and no slice creation. | Required Property 5; Success Criterion 3 | Tasks 1-3 | Covered |
| 7 | Document exactly one slice for each multi-lineage scope with merged work. | Required Property 6; Success Criterion 3 | Tasks 1-3 | Covered |
| 8 | Document that integration-escalation plans remain repair lineage outside the frozen cohort. | Required Property 7; Global Integration | Tasks 1-2 | Covered |
| 9 | Document lineage attribution for coding, fix, and replacement tasks. | Required Property 8 | Task 2 | Covered |
| 10 | Document the bounded slice review surface and immutable analyzed source snapshot. | Required Properties 9-10; Success Criterion 5 | Task 2 | Covered |
| 11 | Document that later sibling changes do not reopen completed slices and instead belong to global review. | Slice Integration | Tasks 1-2 | Covered |
| 12 | Document slice resolution through merged fix/replacement lineage and fail-closed blocked or abandoned findings. | Slice Integration; Required Properties 11, 14 | Task 2 | Covered |
| 13 | Document every planning, coding, repair, coverage, and slice-resolution barrier before global analysis. | Global Integration; Required Property 11; Success Criterion 4 | Task 2 | Covered |
| 14 | Document aggregate branch review as independent global judgment, with local coverage as navigation rather than proof. | Global Integration; Required Property 12; Success Criterion 6 | Task 3 | Covered |
| 15 | Document that promoted repairs and later HEAD mutations require another global generation while budget remains. | Global Integration; Required Property 13; Success Criterion 9 | Task 2 | Covered |
| 16 | Document explicit blocked outcomes for slice exhaustion and explicit exhausted outcomes for the global ceiling. | Final Closure; Required Property 14; Success Criterion 9 | Task 3 | Covered |
| 17 | Document clean completion bound to an immutable reviewed commit equal to live integration HEAD. | Final Closure; Required Property 15; Success Criterion 7 | Tasks 1-2 | Covered |
| 18 | Document the single linearizable order between finalization and integration-HEAD mutation. | Final Closure; Required Property 16; Success Criterion 8 | Tasks 1-2 | Covered |
| 19 | Document mutation-side invalidation of completion tied to a superseded HEAD. | Final Closure; Required Property 17; Success Criterion 8 | Tasks 1-2 | Covered |
| 20 | Document ADR-0112 lock ordering and the prohibition on blackboard writes while the mutation lock is held. | Final Closure; Required Property 18 | Tasks 1-2 | Covered |
| 21 | Document `max_global_integration_generations`, deterministic default `3`, normalization, and exhaustion meaning. | Final Closure; Required Property 19 | Task 3 | Covered |
| 22 | Document idempotent wake/restart reconciliation with no duplicate slice or global generations. | Required Property 20; Success Criterion 10 | Task 2 | Covered |
| 23 | Preserve stack agnosticism and existing review/merge authorization boundaries in operator guidance. | Required Property 21 | Task 3 | Covered |
| 24 | Document the fail-closed capability result for legacy frozen topology. | Assigned scope; Task 10 acceptance contract | Task 3 | Covered |
| 25 | State that legacy frozen pipelines require a fresh workspace or manual topology update. | Assigned scope; Documentation Impact | Tasks 1, 3 | Covered |
| 26 | Add ADR-0113 extending ADR-0055 and superseding its no-rescan limitation. | Documentation Impact 1-2 | Task 1 | Covered |
| 27 | Update state-machine and task-lifecycle documentation. | Documentation Impact 3 | Task 2 | Covered |
| 28 | Update invariant, pipeline, operational, configuration, barrier, generation, and terminal-outcome documentation. | Documentation Impact 4; assigned done-when | Tasks 2-3 | Covered |
| 29 | Change the integration-closure issue only after implementation and validation evidence exists. | Documentation Impact 5 | Task 4 | Covered |
| 30 | Preserve evidence-bearing issue traceability, summary counts, and Table of Contents under the registry Update Policy. | Architectural issue Update Policy; assigned done-when | Task 4 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: upstream `code-planning-main-1-code-planning-9-coding-0` owns both acceptance tests; Task 4 reruns them as closure evidence | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | Tasks 1-4 | Covered |

## Pre-submit audit

- Four atomic documentation tasks own disjoint file sets; only Task 4 depends on Task 1 for ADR traceability.
- Every task has the acceptance implementation task in `task_depends_on`; no task can be claimed before Task 10 evidence is merged.
- Task descriptions, done-when clauses, scopes, spec refs, validations, plan ref, sibling dependency, and external dependency are copied character-for-character into `output[]`.
- Every matrix mapping names a task whose done-when and validation explicitly cover that requirement; no matrix-only acceptance criterion remains.
- Task 4 reruns both exact acceptance tests and dynamically compares registry traceability with the exact authoritative `merge_commit`; summary counts and Table of Contents are independently asserted.
- Each assigned canonical content assertion is retained by its owning task, with additional section-scoped assertions for the previously unproved obligations.
- No implementation file, runtime log artifact, historical ADR, or unrelated architectural issue is writable in this plan.
