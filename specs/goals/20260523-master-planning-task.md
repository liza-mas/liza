# Master Planning Task Pattern

Status: draft

## Goal

Eliminate the chimera risk in planning steps by ensuring every fan-out of
parallel planning tasks is preceded by a single consolidated master task that
defines the general approach and decomposes into specialized tasks.

## Context

When the orchestrator creates multiple parallel planning tasks (epic-planning,
architecture, or code-planning), each starts with only the goal spec and no
shared structural framework. The tasks run independently, and nothing checks
their outputs for consistency before spawning downstream work. Two parallel
planners can make contradictory interface assumptions, overlap in scope, or
diverge on data contracts — producing individually reasonable but collectively
incoherent plans.

The architecture step (introduced in
[20260405-architecture-step](20260405-architecture-step.md)) solved this for the
coding-subpipeline by adding a single consolidation point upstream of
code-planning. But the pattern is incomplete:

1. **Architecture subpipeline itself.** When the orchestrator creates multiple
   architecture tasks for a complex goal (via `functional-spec` or
   `detailed-spec` entry points), those tasks run in parallel with the same
   chimera risk that the architecture step was designed to prevent downstream.

2. **Coding subpipeline without upstream architecture.** The `technical-spec`
   entry point dispatches directly to `code-planning-pair`. The orchestrator can
   create multiple parallel code-planning tasks with no consolidation point.

3. **Epic-spec subpipeline.** The `general-objective` entry point dispatches to
   `epic-planning-pair`. The orchestrator can create multiple parallel
   epic-planning tasks for complex goals.

The root cause is the same in all three cases: the orchestrator's LLM-driven
decomposition into N parallel tasks lacks a structural guarantee that the tasks
share a coherent approach.

Operationally, this manifests as chimera-driven rework: inconsistent designs,
boundaries, and interfaces between parallel tasks are detected late — typically
during review or coding — leading to task superseding, slow convergence, and
high token consumption. The system eventually converges, but through reactive
mitigation (superseding and re-planning) rather than preventive enforcement.

The deeper structural issue is that the orchestrator makes critical
decomposition decisions unilaterally. No other role challenges or validates the
decomposition before it fans out into parallel work. This is the only point in
the pipeline where consequential structural decisions bypass adversarial review
— every other artifact goes through a doer-reviewer loop before downstream
consumption. The master task pattern addresses this by routing decomposition
through adversarial review: a domain expert proposes boundaries, and quorum-2
reviewers challenge them before fan-out occurs.

## Design

### One Rule

**Every planning fan-out must be preceded by a consolidated master task, unless
an upstream pipeline step already provides that consolidation.**

This rule applies uniformly to all three planning steps: epic-planning,
architecture, and code-planning.

For entry-point work, the `INITIAL_PLANNING` orchestrator is responsible for
assessing whether the goal needs exactly one planning task or would otherwise
fan out into multiple parallel planning tasks. Simple planning work enters the
specialized planning pair directly. Fan-out planning work enters the matching
master role-pair.

**Case A — upstream step already provides consolidation (no change):** The
upstream step's output defines the decomposition. One downstream task per
upstream `output[]` entry. This is the current behavior for
`architecture-to-code-plan` (per-subtask transition).

**Case B — no upstream consolidation:** The step splits into two phases within
the subpipeline:

1. A **master task** defines the general approach, boundaries, interface
   contracts, and decomposes the work into specialized scopes via `output[]`.
   The master task has a default review quorum of 2.
2. **Specialized tasks** are created from the master task's `output[]` via a
   per-subtask intra-subpipeline transition. Each specialized task inherits the
   master task's structural framework and focuses on one domain.

Case B applies to entry-point work only when the orchestrator determines that
planning would otherwise fan out. It also applies when a planning step receives
work from a many-to-one transition that consolidates upstream tasks but does not
decompose them.

### Step Split

Case B is implemented as a step split within the subpipeline. The master and
specialized phases use distinct role-pairs, connected by an intra-subpipeline
transition. This reuses existing pipeline machinery — no new transition types
needed.

**Epic-spec subpipeline:**

```yaml
epic-spec-subpipeline:
  steps:
    - epic-planning-main-pair    # master task, quorum 2
    - epic-planning-pair         # specialized tasks, from master's output[]
    - us-writing-pair
  transitions:
    - name: epic-decompose
      task-slug: epic-planning
      from: epic-planning-main-pair.approved
      to: epic-planning-pair.initial
      trigger: auto              # quorum approval is the decomposition gate
      cardinality: per-subtask
    - name: epic-to-us
      task-slug: us-writing
      from: epic-planning-pair.approved
      to: us-writing-pair.initial
      trigger: manual
      cardinality: per-subtask
```

**Architecture subpipeline:**

```yaml
architecture-subpipeline:
  steps:
    - architecture-main-pair     # master task, quorum 2
    - architecture-pair          # specialized tasks, from master's output[]
  transitions:
    - name: arch-decompose
      task-slug: architecture
      from: architecture-main-pair.approved
      to: architecture-pair.initial
      trigger: auto              # quorum approval is the decomposition gate
      cardinality: per-subtask
```

**Coding subpipeline:**

```yaml
coding-subpipeline:
  steps:
    - code-planning-main-pair    # master task, quorum 2 (Case B entry)
    - code-planning-pair         # Case A lands here directly; Case B via decompose
    - coding-pair
  transitions:
    - name: code-plan-decompose
      task-slug: code-planning
      from: code-planning-main-pair.approved
      to: code-planning-pair.initial
      trigger: auto              # quorum approval is the decomposition gate
      cardinality: per-subtask
    - name: code-plan-to-coding
      task-slug: coding
      from: code-planning-pair.approved
      to: coding-pair.initial
      trigger: manual
      cardinality: per-subtask
```

### Case A / Case B Routing

The two cases are distinguished by source: upstream pipeline transitions that
already provide decomposition land directly on specialized steps; entry-point
work may choose the specialized or master step based on the orchestrator's
fan-out assessment.

**Case A (upstream step provides consolidation):** The
`architecture-to-code-plan` pipeline-transition targets
`coding-subpipeline.code-planning-pair.initial`, bypassing
`code-planning-main-pair` entirely. `architecture-pair` is now the specialized
(post-decomposition) step within the architecture subpipeline, but its role in
the pipeline-transition is unchanged.

**Case B (no upstream consolidation):** Entry points keep their existing
specialized targets. During `INITIAL_PLANNING`, the orchestrator creates either:

- one specialized task directly when the goal needs exactly one planning task;
- one master task in the matching decomposition-root role-pair when the goal
  would otherwise fan out into multiple parallel planning tasks.

The `us-to-coding` many-to-one transition still targets the architecture master
step because it receives a multi-story cohort and is not driven by the
`INITIAL_PLANNING` complexity heuristic.

For `INITIAL_PLANNING`, simple means the orchestrator can justify one coherent
planning scope: one functional area, no shared file/schema/interface ownership
question, no migration or cross-cutting contract that needs explicit ownership,
and no expected downstream split into independent specialized scopes. Fan-out
means any of the following apply: multiple functional areas need separate
ownership, shared files or interfaces require explicit boundary assignment,
independent downstream workstreams could run in parallel after planning, the
goal would likely produce more than 8 downstream `output[]` entries,
or the orchestrator is uncertain about boundary placement. When uncertain,
default to the master task; simple bypass is only for confidently single-scope
work.

```yaml
entry-points:
  general-objective: epic-spec-subpipeline.epic-planning-pair
  functional-spec: architecture-subpipeline.architecture-pair
  detailed-spec: architecture-subpipeline.architecture-pair
  technical-spec: coding-subpipeline.code-planning-pair

pipeline-transitions:
  - name: us-to-coding
    task-slug: architecture
    from: epic-spec-subpipeline.us-writing-pair.approved
    to: architecture-subpipeline.architecture-main-pair.initial      # was: architecture-pair
    trigger: manual
    cardinality: many-to-one
  - name: architecture-to-code-plan
    task-slug: code-planning
    from: architecture-subpipeline.architecture-pair.approved         # unchanged — specialized arch tasks
    to: coding-subpipeline.code-planning-pair.initial                 # Case A: skip main step
    trigger: manual
    cardinality: per-subtask
```

### Role-Pairs

Three new role-pairs for the master steps. Each reuses the doer and reviewer
roles from its detail-pair counterpart. Prompt differentiation is handled at the
role-pair level (see Prompt Differentiation below).

```yaml
epic-planning-main-pair:
  doer: epic-planner
  reviewer: epic-plan-reviewer
  decomposition-root: true
  review-policy:
    quorum: 2
    provider-diversity: preferred
  states:
    initial: DRAFT_EPIC_PLAN_MAIN
    executing: EPIC_PLANNING_MAIN
    submitted: EPIC_PLAN_MAIN_TO_REVIEW
    reviewing: REVIEWING_EPIC_PLAN_MAIN
    approved: EPIC_PLAN_MAIN_APPROVED
    rejected: EPIC_PLAN_MAIN_REJECTED
    partially-approved: EPIC_PLAN_MAIN_PARTIALLY_APPROVED
    reviewing-2: REVIEWING_EPIC_PLAN_MAIN_2

architecture-main-pair:
  doer: architect
  reviewer: architecture-reviewer
  decomposition-root: true
  review-policy:
    quorum: 2
    provider-diversity: preferred
  states:
    initial: DRAFT_ARCHITECTURE_MAIN
    executing: ARCHITECTING_MAIN
    submitted: ARCHITECTURE_MAIN_TO_REVIEW
    reviewing: REVIEWING_ARCHITECTURE_MAIN
    approved: ARCHITECTURE_MAIN_APPROVED
    rejected: ARCHITECTURE_MAIN_REJECTED
    partially-approved: ARCHITECTURE_MAIN_PARTIALLY_APPROVED
    reviewing-2: REVIEWING_ARCHITECTURE_MAIN_2

code-planning-main-pair:
  doer: code-planner
  reviewer: code-plan-reviewer
  decomposition-root: true
  review-policy:
    quorum: 2
    provider-diversity: preferred
  states:
    initial: DRAFT_CODING_PLAN_MAIN
    executing: CODE_PLANNING_MAIN
    submitted: CODING_PLAN_MAIN_TO_REVIEW
    reviewing: REVIEWING_CODING_PLAN_MAIN
    approved: CODING_PLAN_MAIN_APPROVED
    rejected: CODING_PLAN_MAIN_REJECTED
    partially-approved: CODING_PLAN_MAIN_PARTIALLY_APPROVED
    reviewing-2: REVIEWING_CODING_PLAN_MAIN_2
```

All three pairs require `partially-approved` and `reviewing-2` states for
quorum 2 (enforced by `validateQuorumStates`).

Quorum 2 means two approved reviews, not necessarily two distinct reviewer
agents. The current quorum system counts approvals. Master role-pairs should
set `provider-diversity: preferred` to encourage distinct reviewer providers;
the existing merge gate records when diversity is not achievable and does not
block approval solely because only one reviewer provider is available.

### Prompt Differentiation

The master task's responsibility differs from a specialized task's: general
approach and task decomposition — not detailed specification beyond clarity of
boundaries and interfaces. Both doer and reviewer need distinct instructions.

Use a narrow prompt-selection rule instead of arbitrary role-pair context
overrides: when a task's role-pair has `decomposition-root: true`, prompt
construction appends fixed master sections for that role side.

- `master-decomposition-mandate` (doer): Instructs the agent to define the
  general approach (boundaries, interface contracts, cross-cutting concerns) and
  decompose into specialized scopes via typed `output[]` entries. Explicitly
  scoped: no detailed specification beyond clarity of boundaries and interfaces.
  The master doer must run `systemic-thinking` against its draft decomposition
  before submitting for review.
- `master-decomposition-review` (reviewer): Instructs the reviewer to evaluate
  the decomposition against the Master Output Contract and typed decomposition
  manifest. This narrows review to boundaries, ownership, dependencies,
  interface contracts, and coverage. Each master reviewer must run
  `systemic-thinking` before submitting a verdict.

No `RolePairDef.context-sections` field is introduced. The only new prompt
configuration surface is `decomposition-root: true`.

### Master Output Contract

The master task is the coherence guarantee. Its `output[]` entries must satisfy
the following properties, and reviewers must reject decompositions that violate
them:

1. **Non-overlapping scopes.** Each `output[]` entry owns a distinct domain
   boundary. No two entries may claim the same files, modules, or functional
   areas. Scope must be stated explicitly — "everything else" entries are
   rejected.

2. **Interface ownership.** Every interface between domains must be owned by
   exactly one entry. The master task's artifact defines the interface contracts;
   `output[]` entries reference them, they don't redefine them.

3. **Shared-file ownership.** When multiple domains touch a shared file (e.g., a
   registry, a config schema, a migration), exactly one entry owns the file.
   Other entries declare a `depends_on` ordering dependency and state read-only
   use in their scope. `depends_on` enforces scheduling only; write ownership is
   a prompt/reviewer constraint.

4. **Dependency ordering.** `depends_on` indices must reflect genuine data or
   interface dependencies. Independent domains run in parallel (no artificial
   sequencing). Circular dependencies are rejected — they indicate a boundary
   problem.

5. **Inherited constraints.** Each entry must reference the master task's
   artifact (`arch_ref` for architecture masters, `plan_ref` for epic-planning
   and code-planning masters) as the structural framework. The specialized
   task's scope is bounded by the master's decomposition — it cannot redefine
   boundaries or interface contracts.

6. **Completeness.** The union of all `output[]` entry scopes must cover the
   full goal scope. Gaps between entries are chimera vectors — the master task
   must prove coverage.

**Reviewer acceptance criteria:** Both quorum-2 approval passes must verify
properties 1-6. A decomposition that is internally reasonable but violates any
property is rejected, even if each entry looks correct in isolation. These
criteria are rendered by the `master-decomposition-review` context section.

### Typed Decomposition Manifest

Each master `output[]` entry includes structured decomposition metadata in
addition to `desc`, `done_when`, `scope`, refs, and dependencies:

```yaml
decomposition:
  owned_files: []              # exact files this entry owns, when knowable
  owned_modules: []            # modules/packages/components this entry owns
  read_only_depends_on: []      # sibling output indices used read-only
  read_only_task_depends_on: [] # existing task IDs used read-only
  interfaces_owned: []         # named interfaces/contracts this entry defines
  interfaces_consumed: []      # named interfaces/contracts this entry consumes
  coverage_notes: ""           # why this entry's scope is complete and bounded
```

Implementation defines an exact nested field on output entries:

```go
type DecompositionManifest struct {
    OwnedFiles            []string `yaml:"owned_files,omitempty" json:"owned_files,omitempty"`
    OwnedModules          []string `yaml:"owned_modules,omitempty" json:"owned_modules,omitempty"`
    ReadOnlyDependsOn     []int    `yaml:"read_only_depends_on,omitempty" json:"read_only_depends_on,omitempty"`
    ReadOnlyTaskDependsOn []string `yaml:"read_only_task_depends_on,omitempty" json:"read_only_task_depends_on,omitempty"`
    InterfacesOwned       []string `yaml:"interfaces_owned,omitempty" json:"interfaces_owned,omitempty"`
    InterfacesConsumed    []string `yaml:"interfaces_consumed,omitempty" json:"interfaces_consumed,omitempty"`
    CoverageNotes         string   `yaml:"coverage_notes,omitempty" json:"coverage_notes,omitempty"`
}

type OutputEntry struct {
    Decomposition *DecompositionManifest `yaml:"decomposition,omitempty" json:"decomposition,omitempty"`
}
```

`decomposition` is required for output entries produced by
`decomposition-root` tasks and optional elsewhere.

Machine validation runs when `liza set-task-output` receives the full `output[]`
array. That is the point where batch-local checks are possible. `buildChildTask`
assumes entries have already been validated and only copies refs/dependencies
into child tasks. Validation covers manifest shape and local or batch-local
invariants: required artifact refs on master output, valid dependency indices, no
duplicate owned files across siblings, no circular sibling dependencies, no empty
ownership declaration, and no catch-all `"everything else"` ownership. Semantic
review covers what the machine cannot know: whether boundaries are meaningful,
whether interface ownership matches the goal, and whether the union of entries
really covers the goal.

### Orchestrator Behavior Change

The `INITIAL_PLANNING` wake changes from "create 1-5 parallel tasks" to:

- create exactly 1 specialized task for simple planning work;
- create exactly 1 master task when the goal would otherwise fan out.

**Detection mechanism:** The pipeline resolver reads explicit
`decomposition-root: true` metadata and maps each specialized entry role-pair to
the decomposition-root role-pair whose auto `*-decompose` transition targets it.
When the orchestrator's complexity assessment would previously create multiple
parallel planning tasks, the wake template instructs it to create exactly 1 task
in the mapped master role-pair. Do not infer this from outgoing `per-subtask`
transitions — regular specialized planning steps also fan out.

The `INITIAL_PLANNING` orchestrator still classifies the spec and assesses
whether planning needs one task or multiple specialized scopes. It no longer
performs parallel decomposition itself; when fan-out is needed, the domain-expert
agent (epic-planner, architect, or code-planner) does the decomposition. The
assessment belongs only to entry-point initialization; non-entry transitions
follow the configured pipeline target.

### Artifact Reference Propagation

The master task's artifact is the structural framework that specialized tasks
must respect. Each planning step uses the appropriate artifact ref:

| Master step | Specialized step | Propagation field |
|-------------|-----------------|-------------------|
| `epic-planning-main-pair` | `epic-planning-pair` | `plan_ref` |
| `architecture-main-pair` | `architecture-pair` | `arch_ref` |
| `code-planning-main-pair` | `code-planning-pair` | `plan_ref` |

The master task sets the ref on its `output[]` entries. `buildChildTask`
propagates it to specialized child tasks.

`epic-planning-main-pair` deliberately uses `plan_ref`, not `epic_ref`, for the
master framework. The master epic-planning artifact is a decomposition plan that
constrains specialized epic planners; it is not a concrete epic artifact for
story-writing. `epic_ref` remains reserved for specialized epic outputs consumed
by `us-writing-pair` children.

Artifact refs have two distinct scopes:

- **Task-level inherited refs** (`Task.PlanRef`, `Task.ArchRef`, `Task.EpicRef`)
  tell the specialized task which upstream artifact constrains its work.
- **Output-entry produced refs** (`OutputEntry.PlanRef`, `OutputEntry.ArchRef`,
  `OutputEntry.EpicRef`) tell downstream children which artifact this task
  produced for them.

A specialized planner may therefore read an inherited task-level ref and still
emit a different produced ref on its own `output[]`. For example, an
`architecture-pair` task may have `Task.ArchRef` pointing to the master
architecture framework, then emit `output[].arch_ref` pointing to its own
specialized architecture artifact for code-planning children. If a specialized
planner intentionally reuses the master artifact downstream, it must set the
output-entry ref to that same path explicitly.

This does not change downstream `epic-to-us` semantics: specialized
`epic-planning-pair` tasks still emit `epic_ref` for `us-writing-pair` children
when they produce concrete epic artifacts.

**Output-entry ref requirement.** The master task writes its artifact before
calling `liza set-task-output`, then sets the appropriate artifact ref on every
`output[]` entry:

- `epic-planning-main-pair`: `plan_ref`
- `architecture-main-pair`: `arch_ref`
- `code-planning-main-pair`: `plan_ref`

No parent task artifact-ref mutation is required. `buildChildTask` already
copies refs from `OutputEntry` to the generated child task. Parent fallback
remains a compatibility backstop for existing `arch_ref`/`epic_ref` flows, not
the primary mechanism for master artifact propagation.

Specialized tasks inherit the master's artifact as a read-only structural
constraint, not as a starting point to modify.

## Implementation Cost

1. **Decomposition-root prompt behavior.** Add `decomposition-root: true` to
   `RolePairDef` in `pipeline/config.go`. Prompt construction uses this boolean
   to append fixed master doer/reviewer sections when the active task belongs to
   a decomposition-root role-pair. No generic role-pair context-section override
   is introduced.

2. **New fixed master context sections.** Two new sections:
   `master-decomposition-mandate` (doer) and `master-decomposition-review`
   (reviewer). Template content defines the master task's responsibility
   (general approach + typed decomposition via `output[]`) and the reviewer's
   acceptance criteria. Reused across all three master pairs.

3. **New role-pairs in pipeline YAML.** Three new role-pairs
   (`epic-planning-main-pair`, `architecture-main-pair`,
   `code-planning-main-pair`) with quorum 2 states. Roles are reused from
   existing pairs.

4. **New intra-subpipeline transitions.** Three new auto transitions
   (`epic-decompose`, `arch-decompose`, `code-plan-decompose`). Standard
   `per-subtask` transitions within a subpipeline — same mechanism as existing
   cross-step transitions. No new pipeline primitives needed.

5. **Entry routing and pipeline-transition retargeting.** Entry points keep
   their specialized role-pair targets. `INITIAL_PLANNING` maps a specialized
   target to its decomposition-root role-pair only when the goal would otherwise
   fan out. `us-to-coding` retargeted to `architecture-main-pair`.
   `architecture-to-code-plan` source unchanged — `architecture-pair` is now the
   specialized step.

6. **New task states.** Eight new states per main pair (24 total). Since states
   are pipeline-driven, verify no hardcoded state lists need updating.

7. **Orchestrator prompt update.** `wake_initial_planning.tmpl`: keep the
   simple-vs-complex assessment, but change complex handling from "create N
   specialized planning tasks" to "create exactly 1 mapped master task." Simple
   handling continues to create exactly 1 specialized planning task.

8. **Decomposition-root mapping.** Add explicit `decomposition-root: true`
   metadata to `RolePairDef`. Pipeline resolver method: given an entry role-pair,
   return the decomposition-root role-pair whose auto `per-subtask` transition
   targets it, if one exists. Expose this mapping in template data. Do not infer
   master behavior from outgoing `per-subtask` transitions on the entry role-pair;
   regular specialized planning steps also fan out.
   Validation: `decomposition-root: true` is valid only when the role-pair has
   exactly one outgoing intra-subpipeline auto `per-subtask` transition whose
   target is another role-pair in the same subpipeline. Reject configs where the
   marker is set on a terminal step, a non-auto or non-`per-subtask` transition
   source, or a role-pair with multiple outgoing decomposition transitions.

9. **Master output artifact refs.** Update master prompt/review sections so
   every master `output[]` entry must include the appropriate artifact ref from
   the propagation table. Reviewers reject master output that omits it. No
   `buildChildTask` parent `plan_ref` fallback is required for this feature.

10. **Typed decomposition manifest.** Add `OutputEntry.Decomposition
    *DecompositionManifest` with the fields specified above. `set-task-output`
    validation requires it on outputs from `decomposition-root` tasks and rejects
    malformed manifests, duplicate file ownership, empty ownership declarations,
    invalid/circular sibling dependencies, missing required artifact refs on
    master output, and catch-all ownership.

## Success Criteria

1. **Pipeline validation passes.** `liza validate` accepts the updated
   `pipeline.yaml` with all three master role-pairs, intra-subpipeline
   auto-decompose transitions, decomposition-root metadata, and the retargeted
   `us-to-coding` transition.

2. **Entry-point routing.** `general-objective`, `functional-spec`,
   `detailed-spec`, and `technical-spec` entry points create exactly 1
   specialized planning task for simple goals and exactly 1 mapped master task
   for goals that would otherwise fan out. Verified via `liza init --spec` +
   `liza status`.

3. **Intra-subpipeline transition.** After a master task is approved and merged,
   the auto decompose transition creates one specialized task per `output[]`
   entry with correct `depends_on`, artifact refs, typed decomposition metadata,
   and target status.

4. **Case A bypass.** `architecture-to-code-plan` transition creates
   code-planning tasks in `code-planning-pair` (not `code-planning-main-pair`).
   The master step is skipped when architecture is upstream.

5. **Quorum-2 behavior.** Master tasks require 2 approved reviews before
   reaching approved status. First approval transitions to
   `partially-approved`, second to `approved`.

6. **Artifact propagation.** Specialized planning tasks carry the master's
   artifact as `plan_ref` or `arch_ref` according to the propagation table, and
   master output entries missing the required ref are rejected.
   Downstream coding tasks inherit the relevant planning/architecture artifact
   transitively; downstream story-writing keeps the existing `epic_ref` behavior
   from specialized epic outputs.

7. **Prompt differentiation.** Master doers receive the fixed
   `master-decomposition-mandate` section. Master reviewers receive the fixed
   `master-decomposition-review` section with manifest and properties 1-6
   acceptance criteria. Both sections require running `systemic-thinking` on the
   master decomposition.

8. **Orchestrator simplification.** `INITIAL_PLANNING` wake creates exactly 1
   task in both simple and complex cases: specialized for simple, master for
   fan-out.

## Documentation Impact

- `README.md` — update entry-point and pipeline flow descriptions.
- `support-docs/USAGE_MULTI_AGENTS.md` — update pipeline step descriptions,
  entry-point documentation, and any spawn-count guidance.
- `specs/architecture/overview.md` — update subpipeline descriptions.
- `specs/architecture/roles.md` — document master task decomposition
  responsibility.
- `specs/architecture/state-machines.md` — document the new master task states
  and quorum transitions if state names are enumerated there.
- `specs/architecture/blackboard-schema.md` or pipeline config schema docs —
  document `role-pairs.<name>.decomposition-root` and typed decomposition
  manifest fields on output entries.
- `specs/architecture/ADR/` — add an ADR or ADR supersession/update note for the
  master planning task pattern.

## Out of Scope

- **Review quorum mechanism changes.** The quorum system already supports
  quorum > 1 with `partially-approved` and `reviewing-2` states. No changes to
  the review machinery itself.

- **New roles or skills.** The master task reuses existing roles. A
  purpose-built "decomposition" skill may emerge from operational experience but
  is not structural to this design.

- **Migration of existing workspaces.** Pipeline config is frozen at `liza init`
  time. Existing workspaces will not receive the new role-pairs, transitions, or
  entry-point changes. Users must re-initialize (`liza init`) to pick up the new
  pipeline topology. No runtime migration mechanism is introduced.

## Risks

1. **Latency cost.** The master task adds one full plan-review-approve cycle
   before specialized tasks can start when fan-out is needed. Mitigation: simple
   goals bypass the master task and enter the specialized planning pair directly.
   More importantly, the latency cost must be weighed against the current cost
   of chimera-driven rework: multiple superseded tasks, wasted agent cycles, and
   token consumption from inconsistencies detected late. The master task moves
   cost from reactive rework to proactive review — net latency is likely reduced
   for goals where chimeras would otherwise occur.

2. **Master task quality is a single point of failure.** If the master task
   produces a poor decomposition, all specialized tasks inherit the flawed
   framework. Mitigation: quorum 2 on the master task catches decomposition
   issues before fan-out. This is the design intent — invest review effort at
   the consolidation point where it has the highest leverage. The master role-pairs
   also set `provider-diversity: preferred`, which encourages qualitative redundancy
   (different model families/providers) when available. This is best-effort
   metadata, not a hard requirement: approval is not blocked solely because provider
   diversity is unavailable.

3. **Additional review-cycle latency.** Master tasks require two approval events
   before reaching approved status. This does not require two distinct registered
   reviewer agents; the current quorum mechanism counts approvals, not distinct
   reviewers/providers. The operational cost is an extra review claim/verdict
   cycle and potential delay if no reviewer agent is available to claim the
   partially-approved task.

4. **Typed decomposition manifest increases output-entry schema weight.** The
   manifest shifts part of decomposition quality from prompt prose into
   structured fields. Mitigation: only master tasks are required to populate the
   manifest, and validation focuses on shape and local ownership invariants.
