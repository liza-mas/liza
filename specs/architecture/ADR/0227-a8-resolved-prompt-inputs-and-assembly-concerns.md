# 227 - A8 Resolved Prompt Inputs and Assembly Concerns

## Status

ACCEPTED.

## Context

The architecture-consolidation run requires `prompts` to receive resolved data
instead of reaching into `ops` (K3, SI-5), the area's size offenders to be
separated by concern (S1, S3) and its finite vocabulary and magic values to be
named (K7), with rendered prompts byte-identical and no behaviour change
([goal](../../goals/20261023-architecture-consolidation.md#structural-constraints)).
At the A8 start (`71d768f46`) the area (`internal/prompts` Go and templates,
`rolebudget`, and the four prompt-assembly files of `internal/agent`) had one
file offender (`agent/prompt.go`, 1,401 lines), three function offenders
(`renderOrchestratorDashboard` 176 lines, `buildTaskRoleContextData` 179,
`buildReferenceContextWithRepository` 223) and one allowlisted crossing,
`prompts` to `ops`. The decision is indexed in the [ADR README](README.md).

[ADR-0016](0016-embedded-template-architecture.md) keeps the embedded template
loader the single template source; [ADR-0045](0045-declarative-role-definitions.md)
keeps role names, role-pair names and context-section identities dynamic;
[ADR-0092](0092-build-time-white-label-branding.md) binds rendered text to brand
indirection; [ADR-0220](0220-a1-typed-vocabulary.md) supplies the typed role
type and the conversion rules (AC3). Per-area guard data follows
[ADR-0218](0218-architecture-guard-package-and-ratchet-data-seam.md); the A8
data file `internal/testguard/archguard/area_a8_test.go` is the evidence. Three
coding units delivered A8: the shared records and the orchestrator resolution
(`492ddace7`, `fd9598786`) and the assembly and closure unit this ADR belongs to.

## Decision

### D1 - Resolution owner and the two entries

The runtime, `internal/agent/orchestrator_dashboard.go`, resolves every read-only
value the orchestrator render needs and passes it to `prompts` as data; `prompts`
formats, executes templates and composes blocks, `models` owns shared domain
records and `ops` keeps queries and mutations. No callback reaches `ops` from a
template or from `prompts`.

There are two entries over one render function. `RenderOrchestratorDashboard`
resolves the input, selects the standalone wake (held-plan suppression and the
blocked-planning-handoff exception, evaluating live integration progress only for
a completion wake) and renders. `renderOrchestratorDashboardForWake` keeps the
supervisor's already revalidated trigger and integration projection and selects
nothing. Both evaluate the integration outcome before entry points resolve, so an
entry-point error (`building wake template data: %w`) never hides it, and a
detection context that cannot load gates nothing in resolution.

### D2 - The resolved record

`prompts.OrchestratorDashboardInput` carries the snapshot and agent ID, whether
the sprint accepts wakes, the sprint-terminal states (nil when the detection
context could not load), the cycle-blocked count, the planned merged planning
tasks in planned order, the ready many-to-one count, the held-plan flag, the
blocked-awaiting-handoff flag, the actionable blocked IDs and actionable
hypothesis-exhausted count (the displayed blocked and hypothesis counts stay raw
and distinct), unseen human notes, the test-file matcher text and the resolved
wake routes (entry-point role pairs, display names, task types and ID prefixes).
`OrchestratorWakeDecision` carries the trigger with its integration projection.
`WakeTrigger` and `IntegrationViewStatus` are `prompts`-owned typed vocabularies
with unchanged string values.

### D3 - Shared result types

`models.IntegrationProgressDecision`, `IntegrationScopeCoverage`,
`IntegrationAnalysisRequest`, `IntegrationProgressReason`,
`ReconcileIntegrationAnalysesResult` and `PlanHandoffClass` with its constants
moved from `ops` to `models` (`492ddace7`) with fields, tags and zero values
unchanged; the classifier, domain and resolver stay in `ops`. Callers migrated in
the same unit, with no alias. `prompts.ProjectEffectiveIntegrationCompletion`
consumes the records. No mutation body or query moved.

### D4 - Matcher data path

`agent.SubmissionTestPatternsText()` joins `ops.TestFileMatcherPatterns()` with
comma-space. It fills `TestFilePatterns` on the orchestrator input, on
`RoleContextData` (task and planning-wake contexts) and, through
`RenderWakeInstructions(trigger, agentID, testFilePatterns)`, on the stateless
wake measurement; templates print `{{.TestFilePatterns}}` and no template function
calls `ops`. The matcher list exists once, in `ops`.

### D5 - Template structure and assembly concerns

The two embedded template sets and their single loader (`templates.go`) are
unchanged. The static orchestrator command text lives in
`templates/orchestrator_commands.tmpl`; dynamic dashboard formatting stays in
Go. Rendered bytes, block names and invocation order are unchanged.

Assembly in `internal/agent` is separated by concern, orchestration staying in
`prompt.go` (321 lines at closure, 1,401 at start):

| Concern | File | Contents |
|---|---|---|
| Prompt orchestration | `prompt.go` | base prompt, role context, orchestrator prompt, `buildTaskRoleContextData` (32 lines), context-section extension |
| Identity, recovery, reviewer, architect and reference-provenance stages | `prompt_task_context.go` | `newTaskContextData` and the ordered `populate…` helpers |
| Repository-intelligence discovery | `prompt_intelligence.go` | index-reference conversion and availability probes |
| Integration evidence | `prompt_integration.go` | `populateIntegrationContext` through `integrationCoverageSummary` |
| Plan position and task-graph digest | `prompt_task_graph.go` | `collectPlanPosition`, `buildRelevantTaskGraph` and its bounding |
| Planner navigation, corrective context | `prompt_planner_context.go`, `prompt_corrective_context.go` | unchanged boundaries |
| Reference provenance | `reference_context.go` | `buildReferenceContextWithRepository` (33 lines) over scalar-carrier collection, parent range collection, review range collection, decomposition elision, assigned-section presentation and legacy filtering |

`buildTaskRoleContextData` keeps its stage order and wrapping: identity and task
values, intelligence discovery, prior rejection, `planner navigation context: %w`,
prior attempt, coder recovery, validation prerequisites, reviewer fields,
integration, declarative fields, `precommit config check: %w`, then
`reference context topology for task %q: %w` and `reference context for task %q:
%w: %w` with `precommit.ErrContextBuild`. The ignored errors (role type, skills,
mandatory docs, direct-allocation consumer lookup) stay ignored. The reference
build captures the integration HEAD once, lazily (`referenceHead`), validates every
carrier before any elision, keeps the parent-range precedence, proof-policy drift
checking and reviewer-attribution refusal, and rendering stays in
`referencecontract`. `TestA8TaskContextStageOrder` and
`TestA8ReferenceHeadCapturedOnce` were green on the pre-edit tree (`2d6a402c1`)
and stay green; one fixture note: a retained commit missing from a reachable
repository is disclosed as advisory absence, so the planner-stage failure of the
precedence case uses an unavailable repository, an error the existing planner
tests already classify as operational.

### D6 - Vocabulary and the K7 inventory

The inventory covers all 16 A8 production Go files: `internal/prompts/{builder,
orchestrator_input,role_context,templates,text,wake}.go`, `rolebudget/rolebudget.go`
and `internal/agent/{prompt,prompt_task_context,prompt_intelligence,
prompt_integration,prompt_task_graph,prompt_planner_context,
prompt_corrective_context,reference_context,orchestrator_dashboard}.go`.
It is reproducible with, from the repository root and a file list of those paths:

```text
rg -n -e '(==|!=)\s*"[^"]+"' -e '"[^"]+"\s*(==|!=)' -e '^\s*case\s+.*"[^"]+"' \
   -e 'strings\.(HasPrefix|HasSuffix|Contains|Cut|Trim[A-Za-z]*|Split[A-Za-z]*|Index[A-Za-z]*)\([^)]*"[^"]+"' \
   -e 'append\([A-Za-z.]+, "[^"]+"\)' -e 'append\(\[\]string\{"[^"]+"' <files>   # string literals
rg -n -e 'TruncateText\([^)]*,\s*[0-9]+' -e '^\s+[A-Za-z]+\s+(=|[A-Za-z.]+ =)\s+[0-9]+' \
   -e '^const [A-Za-z]+ = [0-9]+' -e ':= -?[1-9][0-9]*$' <files>                  # numbers
```

Classes: **T** typed constant in its owning package; **N** named constant;
**O** open string by rule (YAML-defined identity, ADR-0045); **S** structural
(delimiter, empty-string or zero-length test, regular-expression group), not
vocabulary; **M** message or template text, not control flow. Every row is a
`file:line` at the closure HEAD of this unit.

Control-flow string literals (the first search, after the sweep):

| Location | Literal | Class | Owner or reason |
|---|---|---|---|
| `agent/prompt.go:286` | `"task-decomposition"`, `"implementation-phase"` | O | context-section identity, removed for a decomposition-root code planner |
| `agent/prompt.go:289` | `"master-decomposition-mandate"` | O | context-section identity appended for a root doer |
| `agent/prompt.go:293` | `"review-instructions"` | O | context-section identity, removed for a root plan reviewer |
| `agent/prompt.go:296` | `"master-decomposition-review"` | O | context-section identity appended for a root reviewer |
| `agent/prompt_task_graph.go:417` | trim set `" \t\r\n.,;:()[]{}\"'"` | S | punctuation trimming of a candidate path |
| `agent/prompt_task_graph.go:418` | `"*"`, `" "` | S | rejects glob and prose candidates |
| `agent/prompt_task_graph.go:421` | `"/"`, `"."` | S | path-shape heuristic |
| `agent/prompt_planner_context.go:136` | `foundationScopeHeading`, `":"` | N, S | named heading `Scope 0`; `:` delimits a heading suffix |
| `agent/prompt_planner_context.go:185` | `":"` | S | trailing delimiter of an interface token |
| `agent/prompt_planner_context.go:484` | `"latest rejection has no retained commit"` | M | disclosure text stored for rendering |
| `prompts/orchestrator_input.go:85` | `"#"` | S | ref fragment delimiter |
| `prompts/builder.go:384`, `:401` | `"task-corrective-context"`, `"mandatory-docs"` | O | context-section identities prepended to the role's sections |
| `prompts/builder.go:414`, `:420` | `"task-corrective-context"`, `"mandatory-docs"` | O | same identities, when skipped as already rendered |
| `prompts/builder.go:440` | `"REVIEW BOUNDARY MISSING: …"` | M | block text |

Vocabulary adopted or named by this unit (each replaced a literal of the same value):

| Location | Constant | Class | Owner |
|---|---|---|---|
| `agent/prompt.go:158`, `:264`, `:283`; `agent/reference_context.go:91`, `:275`; `agent/prompt_planner_context.go:43`; `agent/prompt_task_context.go:124`, `:160`; `prompts/builder.go:381`, `:403`; `prompts/rolebudget/rolebudget.go:90` | `roles.RoleTypeDoer`, `RoleTypeReviewer`, `RoleTypeOrchestrator` | T | A1 `roles/roles.go`; `RoleContextData.RoleType` stays `string`, converted at that field (AC3) |
| `prompts/builder.go:171`–`:175`, `:184`, `:193`, `:202` | `scipsearch.LanguageGo`, `LanguageTypeScript`, `LanguagePython` | T | `scipsearch/languages.go`; `ScipSearchIndex.Language` stays `string` |
| `prompts/builder.go:163`–`:166` | `scipRankGo`, `scipRankTypeScript`, `scipRankPython`, `scipRankOther` | N | `prompts`; index display order |
| `agent/prompt_planner_context.go:27`, `:28` | `plannerArtifactArchitecture`, `plannerArtifactPlan` (`plannerArtifactKind`) | T | `agent`; converted to `string` in `PlannerArtifactPointer.Kind` |
| `agent/prompt_planner_context.go:34`, `:37` | `foundationScopeHeading`, `generatedFoundationSuffix` | N | `agent` |
| `agent/prompt_task_graph.go:108`–`:113` | `relationDependency`, `Blocked`, `Sibling`, `FileOverlap`, `ArtifactProducer`, `ArtifactRef` (`taskGraphRelation`) | T | `agent`; converted to `string` in `TaskGraphEntry.Relations` |
| `agent/prompt_task_graph.go:99`, `:100` | `taskGraphDescriptionLimit` 96, `taskGraphBlockedReasonLimit` 120 | N | `agent` |
| `agent/prompt_task_graph.go:119`–`:124` | `taskGraphWeightDependency` 100, `Blocked` 90, `FileOverlap` 80, `Sibling` 50, `Default` 30, `ArtifactOnly` 10 | N | `agent`; digest bounding order |
| `agent/prompt_integration.go:17` | `integrationDescriptionLimit` 200 | N | `agent` |
| `agent/prompt_task_context.go:20`, `:22`, `:25` | `parentDescriptionLimit` 500, `priorAttemptNumber` 2, `nullRejectionReason` | N | `agent` |
| `agent/reference_context.go:37`–`:40` | `referenceRankSpec` 0, `Epic` 1, `Arch` 2, `Plan` 3 | N | `agent`; scalar ref specificity |
| `agent/prompt_corrective_context.go:24`–`:26` | `humanNoteTargetAll`, `humanNoteSourceOperatorCLI`, `humanNoteOperationAddNote` | N | owner gap: `ops` keeps these unexported (`humanNoteForAll`, `humanNoteSourceOperatorCLI`, `addHumanNoteOperation`); exporting them edits a closed area, so `agent` mirrors the values and `TestA8NamedVocabularyKeepsValues` pins them |
| `agent/orchestrator_dashboard.go:184` | `entryPointValueSeparator` | N | `agent` |
| `prompts/rolebudget/rolebudget.go:296` | `percent` 100 | N | `rolebudget` |

Already named before this unit and unchanged: `agent/prompt_task_graph.go:93`–`:96`
(`maxTaskGraph…` 8, 3, 16, 8), `agent/prompt_corrective_context.go:14`–`:16`
(`correctiveNoteLimit` 8, `correctiveTextRunes` 4096, `correctiveLabelRunes` 128),
`prompts/orchestrator_input.go:58`, `:60`, `:63`, `:65` (`activeTaskDigestLimit` 12,
`blockerDigestLimit` 220, `hypothesisExhaustionFailures` 2, `integrationRolePair`,
a role-pair name kept as a named open string), `prompts/rolebudget/rolebudget.go:54`
(`Ceiling` 0.05). Role names, role-pair names, task prefixes, context-section
identities and other YAML-defined identities stay open strings; custom-role
behaviour is untouched. `TestA8NamedVocabularyKeepsValues` pins every spelling and
limit above.

### Shim and exported-API audit

`git diff 71d768f46 HEAD` over `internal` and `cmd` adds no `type X = Y` alias
and no forwarding function; the Shim Register stayed empty. The new files hold
whole moved concerns, and the two public orchestrator entries are the D1 entries,
not forwarders to `ops`. `go list -deps ./internal/prompts` lists no `internal/ops`
and no `internal/agent`; `prompts/rolebudget` and `prompts/promptbench` import
`agent` as exempt test support (SI-1). Exported API added by A8:
`agent.RenderOrchestratorDashboard`, `agent.SubmissionTestPatternsText`, the
`prompts` input records and `models` records of D2 and D3; the orchestrator render moved its `ops`
queries out of `prompts` into `agent`.

### Assertion preservation

No existing test file changed in the assembly unit; the reference-context and
master reference tests and the prompt, planner and corrective suites assert
exactly what they did. The unit adds `prompt_a8_assembly_test.go`
(stage order, single HEAD, vocabulary values). The A8 oracle goldens, its
regeneration check and `TestRoleBudget` run unchanged.

### Chain-document references

Searching `INVARIANTS.md`, `state-machines.md`, `blackboard-schema.md`,
`supervision-model.md`, `specs/protocols` and `lessons` for the A8 files and
symbols finds one citation: the INVARIANTS.md section 6 row on strict planning
artifacts names `reference_context.go`, which still holds the one-HEAD
collection, validation, elision and presentation it describes. No location needs
correcting and no pre-existing drift was found.

### Closure evidence for the A8 boundary

Registry item **Prompts Layer Imports Business Logic** (`issue-prompts`) is
resolved by `492ddace7` (shared records moved to `models`), `fd9598786`
(orchestrator rendered from runtime-resolved inputs; matcher text and template
function removed) and the assembly commit of this unit; the A8 allowlist,
ratchet and S2 are empty (`TestAreaA8DataIsEmpty`) and `prompts` imports no
`ops`. Final measures (`go/parser`): `agent/prompt.go` 321 lines; longest A8
function 94 lines (`populateSliceIntegrationContext`); `buildTaskRoleContextData`
32 and `buildReferenceContextWithRepository` 33. The A8 boundary unit owns the
registry closure. Items that stay open:

- **No Query Layer** (`issue-query`): caller resolution was chosen and no query
  package was added, so the other read paths (`commands` to `agent`, the `models`
  diagnostics home) are unchanged.
- **Custom Roles Render Empty Prompt Blocks** (`issue-custom-roles`): template
  role chains and role names are untouched by design (ADR-0045).
- **Control-Flow Vocabulary Bypasses Domain Ownership** (`issue-vocabulary`):
  its scope spans later areas.

## Consequences

- `prompts` is presentation of resolved data: it can be tested without a state
  directory, and a change to planning eligibility or cohort readiness no longer
  edits it.
- Task context stages have one place each; their order is observable and pinned.
  The reference build has one HEAD owner, so a new collection stage cannot capture
  a second snapshot without failing `TestA8ReferenceHeadCapturedOnce`.
- `agent` holds a mirrored spelling of three `ops` human-note constants until a
  later area exports an owner.

## Alternatives Considered

1. **A shared query package** both `ops` and `prompts` import: rejected; the
   runtime already owns the production call and the projection needs only data.
2. **Callbacks into `ops` from `prompts`**: rejected; they hide the dependency.
3. **Fixed-size function slices** instead of concern files: rejected (S3).
4. **Exporting the `ops` human-note constants**: rejected here as an edit of a
   closed area for three strings; recorded as the owner gap above.
