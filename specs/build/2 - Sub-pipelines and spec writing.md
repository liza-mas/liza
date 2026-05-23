# Goal

Status: Done

## Context

> **Supersedes** Vision §"What Liza Doesn't Do" — fixed-roles constraint.
> The Vision's intent (no *dynamic* agent spawning at runtime) is preserved:
> roles are still statically declared in pipeline configuration, not spawned on the fly.
> This spec expands the fixed role set from 3 (Planner, Coder, Code Reviewer)
> to 8 (Epic Planner, Epic Plan Reviewer, US Writer, US Reviewer, Code Planner,
> Code Plan Reviewer, Coder, Code Reviewer) plus an Orchestrator.
> The Vision document should be updated to reflect this expanded role set
> once this spec is implemented.

The single biggest gap in current AI coding: most systems jump straight to code. MetaGPT's strongest contribution is
mandating structured artifacts (PRDs, API designs) before implementation. AgentMesh's paper candidly admits error
propagation from bad plans is their top limitation. Splitting complexity earlier is where the real cost savings come from.

The spec phase runs as its own sprint. The human reviews and approves specs at the sprint boundary before the coding
sprint begins. Specs are the contract between human intent and agent execution — if the specs are good, the coding sprint
can be trusted to run autonomously. If the specs are bad, no amount of intra-sprint guardrails will produce the right
software.

The spec phase also creates the right surface for [structured artifact templates](../../misc/Liza-roadmap.md#structured-artifact-templates-between-roles):
the US Writer produces user stories using a standard template from the user-story-writing skill, which the Coder consumes
directly as implementation guidance.

## Objective

The objective is to add a specification pipeline before the existing pipeline.

## Approach

Existing pipeline:
**Planner → Coder → Code Reviewer**

Enhanced coding pipeline (phase 1):
**Code Planner (new role, absorbs planning from existing Planner) → Code Plan Reviewer → Coder → Code Reviewer**
This enhancement systematizes the adversarial principle to the planning phase.

Specification pipeline (pahse 2):
**Epic Planner → Epic Plan Reviewer → US Writer → US Reviewer**

Three patterns appear:
- role pairs (doer → reviewer):
  - epic planning pair = Epic Planner → Epic Plan Reviewer
  - us writing pair = US Writer → US Reviewer
  - code planning pair = Code Planner → Code Plan Reviewer
  - coding pair = Coder → Code Reviewer
- generic sub-pipelines:
  - epic sub-pipeline = epic planning pair → us writing pair
  - implementation sub-pipeline = code planning pair → coding pair
- composable pipelines:
  - sprint pipeline = epic sub-pipeline → implementation sub-pipeline

**Intra-pair flow (fixed for all role-pairs):**
`initial → executing → submitted → reviewing → approved|rejected`, with `rejected` looping
back to `initial` (task stays assigned to same doer — supervisor reclaims automatically,
iteration counter increments, reviewer feedback preserved as task attributes).
A task at `initial` with `iteration_count > 0` is a revision, not fresh work — tools and
human operators use `iteration_count` to distinguish, not the state name.

**YAML semantics:** The YAML only declares the state names, not the transitions between them.
State names are descriptive labels (e.g., IMPLEMENTING_CODE, CODE_PLANNING) chosen for human
readability, not mechanical prefixing. The config loader treats them as opaque strings.

**Cross-cutting meta-states:** BLOCKED, ABANDONED, SUPERSEDED, and INTEGRATION_FAILED can
overlay any role-pair — they are not part of the role-pair YAML and remain hardcoded in the
state machine.

**Constraint:** Role-pair names must be globally unique across the pipeline config. A role-pair cannot appear in more than one sub-pipeline. This ensures the task's `role_pair` field is sufficient to resolve which sub-pipeline and transitions apply — no additional `sub_pipeline` field is needed on the task model.

To be noted that role pairs may have a terminal state that doesn't match the initial state of the next role pair in the pipeline.
In such a case, e.g. `CODING_PLAN_APPROVED → DRAFT_CODE`, only the user may execute the transition manually using `liza proceed <task-id> <transition-name>`. Both arguments are required — there is no auto-detection when multiple manual transitions are possible. The task's `role_pair` field scopes which transitions are valid; transition names must be globally unique across all sub-pipelines (since `liza proceed` resolves transitions by name without sub-pipeline context). `liza status` shows available transitions for tasks at manual-transition states.

---

## Plan

Phase 1 prepares Phase 2 (the adding of a new US Writing Sub-pipeline) by:
- splitting the existing responsibilities of the Planner into three new agents - the Orchestrator and the Code Planner / Code Plan Reviewer
- making the existing pipeline declarative rather than hardcoded.

### Phase 1: configurable pipeline
1. ✅ Rename the existing Planner to Orchestrator.

2. ✅ Extract the planning responsibility from the (now-renamed) Orchestrator into a new Code Planner role.
   The Orchestrator's only remaining responsibility is creating a task for the Code Planner.

3. ✅ Add a Code Plan Reviewer as the reviewer of the output of the Code Planner
   This implies modifying the transitions from:
   ```
   DRAFT → READY
   ```
   to:
   ```
   DRAFT_CODING_PLAN → CODE_PLANNING → CODING_PLAN_TO_REVIEW → REVIEWING_CODING_PLAN → CODING_PLAN_APPROVED
     ↑                                                             ↓
     └───────────────────── CODING_PLAN_REJECTED ──────────────────┘
   ```
   After approval, the reviewer's supervisor merges the worktree branch to integration
   (CODING_PLAN_APPROVED → MERGED), same as for the coding pair. MERGED is therefore the
   actual sprint-terminal state, and `liza proceed` operates on MERGED tasks.

   **Implementation note:** The code-planning pair states and transitions are hardcoded in
   `models/state.go` alongside the legacy coding-pair transitions. This dual representation
   (hardcoded + YAML-driven) ensures backward compatibility: legacy goals (no pipeline config)
   use the hardcoded transitions, while pipeline-configured goals use the resolver. Both paths
   coexist and are exercised by different code paths at runtime.

4. ✅ Implement a `liza proceed` command to do the `MERGED → DRAFT_CODE` transition.
   The next sprint may then be started using `liza resume`.

   **Task lifecycle semantics for `liza proceed`:**

   `liza proceed` never changes the source task's status. The source task stays at its
   sprint-terminal state (e.g., MERGED) as a permanent record.
   The only field written back to the source task is `transitions_executed` (bookkeeping
   for idempotency — see below). This is not a state change.

   | Cardinality | Behavior |
   |-------------|----------|
   | `per-subtask` | Creates one child task per entry in `output[]`, each at the target pair's `initial` state. Child tasks link back via `parent_task: <source-id>`. |
   | `one-to-one` | Creates one child task at the target pair's `initial` state. The child's spec is the source task itself (linked via `parent_task`), no `output[]` needed. |

   The source task's sprint-terminal status is preserved for metrics and audit trail.
   Child tasks are scoped into the next sprint via `liza resume`.

   **Idempotency:** `liza proceed` records the executed transition in a map on the source
   task (e.g., `transitions_executed: { code-plan-to-coding: true }`). Repeated calls for
   the same transition are rejected. This is a map, not a scalar — a task with multiple
   outbound transitions (rare but allowed) can track each independently. Auto transitions
   use the same guard.

   **Phase-gate inheritance:** When a source task has `depends_on` pointing to upstream tasks
   that have already executed the same transition, child tasks automatically inherit those
   upstream children as dependencies. This enforces phase-gate ordering: phase-2 children
   cannot be claimed until all phase-1 children are MERGED.

   **Recovery:** Child task IDs are deterministic and namespaced by transition:
   `<parent-id>-<transition-name>-<subtask-index>` for `per-subtask` (index from `output[]`
   order), `<parent-id>-<transition-name>` for `one-to-one`. The operation sequence is:
   (1) write transition key to `transitions_executed`, (2) create child tasks one by one.
   On crash recovery, the supervisor compares expected child IDs (derived from `output[]`
   length or cardinality) against existing children — only missing children are created.
   Existing children created before crash are patched with any missing inherited deps.
   This handles both zero-child and partial-creation crashes. Not an atomic transaction;
   idempotent per child.

   **Execution ordering:** `ExecuteAvailableTransitions` topologically sorts pending
   transitions by task `depends_on` before execution (Kahn's algorithm, stable tie-breaking
   by original array order). Upstream transitions fire first, guaranteeing upstream children
   exist before downstream `computeInheritedDeps` runs. Circular dependencies produce a
   `transition_cycle_blocked` history event and are skipped.

   **Undo:** If `liza proceed` was executed prematurely, recovery is: ABANDON child tasks,
   then remove the transition key from `transitions_executed` on the source task via
   `liza edit-task` (manual blackboard edit). A dedicated `liza undo-proceed` is deferred.

   **Batch:** `liza proceed` operates on one task at a time. When N tasks need the same
   transition (e.g., N approved US tasks → N Code Planner tasks), the human runs it N times.
   A `liza proceed --all <transition-name>` convenience is deferred.

5. ✅ Make `liza init` take a `--config` yaml file parameter. This yaml file will define the pipeline to implement.
   At init time, `liza init --config pipeline.yaml "Project goal" --spec vision.md` freezes both the
   pipeline config (into `.liza/pipeline.yaml`) and the goal/spec reference. The Orchestrator reads
   the frozen pipeline to determine the entry-point, then reads the spec to classify and dispatch.
   An optional `--entry-point <name>` parameter bypasses LLM classification (e.g.,
   `liza init --config pipeline.yaml --entry-point detailed-spec "Goal" --spec spec.md`).
   Whether selected by the Orchestrator or by the human, the resolved entry-point is recorded
   in the blackboard (`goal.entry_point: <name>`) for auditability.
   All runtime lookups (supervisor, CLI, MCP tools) read from `.liza/pipeline.yaml`, never the original
   source file. The config is immutable for the lifetime of the goal — changing the pipeline requires
   a new `liza init`. The blackboard records `pipeline_version: 2` at init time so tools can
   distinguish pipeline-configured goals from legacy hardcoded goals without relying on goal history.

   An additional `--post-worktree-cmd` flag allows specifying a shell command to run after
   worktree creation (e.g., dependency installation). This replaces the previously hardcoded
   `syncEmbedded` call, making worktree post-creation configurable per project.
   So:
   ```yaml
   pipeline:
     agent-roles:
       code-planner: "Code Planner"
       code-plan-reviewer: "Code Plan Reviewer"
       coder: "Coder"
       code-reviewer: "Code Reviewer"

     role-pairs:
       code-planning-pair:
           doer: code-planner
           reviewer: code-plan-reviewer
           states:
             initial: DRAFT_CODING_PLAN
             executing: CODE_PLANNING
             submitted: CODING_PLAN_TO_REVIEW
             reviewing: REVIEWING_CODING_PLAN
             approved: CODING_PLAN_APPROVED
             rejected: CODING_PLAN_REJECTED

       coding-pair:
           doer: coder
           reviewer: code-reviewer
           states:
             initial: DRAFT_CODE
             executing: IMPLEMENTING_CODE
             submitted: CODE_TO_REVIEW
             reviewing: REVIEWING_CODE
             approved: CODE_APPROVED
             rejected: CODE_REJECTED

     sub-pipelines:
       coding-subpipeline:
           steps:
             - code-planning-pair
             - coding-pair
           transitions:
             - name: code-plan-to-coding
               from: code-planning-pair.approved
               to: coding-pair.initial
               trigger: manual  # human validates implementation plan before coding begins
               cardinality: per-subtask  # supervisor creates one downstream task per entry in output[]

     entry-points:
       detailed-spec: coding-subpipeline.code-planning-pair
   ```

   **Trigger modes:**
   - `manual`: requires the human to run `liza proceed <task-id> <transition-name>`. Used for quality gates where human judgment is needed before the next pipeline stage begins (e.g. reviewing a plan before committing to implementation).
   - `auto`: the supervisor executes the transition automatically when the source task reaches the configured `from` state and transition-specific validation passes. Master planning uses `auto` for intra-subpipeline decomposition after quorum approval. Preconditions beyond state match: for `per-subtask`, `output[]` must be non-empty and validated; for `one-to-one`, parent artifact must exist. Planning Transition Gate behavior still applies to manual phase gates.

6. ✅ Make the Task state machine configured via the yaml file.

   **Implementation note:** The hardcoded state machine is retained alongside the YAML-driven
   one for backward compatibility. Every ops function (claim, submit, verdict, checkpoint,
   release) checks for a pipeline resolver first (`if resolver != nil && task.RolePair != ""`);
   if present, it resolves states from the config. Otherwise, it falls back to the hardcoded
   transitions. The `TransitionWith()` method on Task accepts a custom transition map from the
   resolver, while the existing `Transition()` uses the hardcoded map. Cross-cutting transitions
   (to/from BLOCKED, ABANDONED, SUPERSEDED) are merged into the pipeline transition map by
   `BuildPipelineTransitions()` in `pipeline_ops.go`.

   **State name migration:** The coding-pair YAML uses new state names (e.g., DRAFT_CODE instead
   of DRAFT, CODE_TO_REVIEW instead of READY_FOR_REVIEW). This is a breaking change for
   existing blackboard state. Phase 1 applies to **new goals only** (`liza init`). Existing
   in-progress goals continue using the hardcoded state machine until completed. No migration
   of active blackboard state is needed.

7. ✅ Extend the task model with `role_pair`, `parent_task`, `output`, and `transitions_executed` attributes.
   The blackboard schema docs (`blackboard-schema.md`, `state-machines.md`) have been updated
   to reflect these fields.

   When a doer's work produces downstream tasks (1:N transitions), the doer writes the subtask definitions
   into `output[]` as part of its deliverable. The reviewer validates both the artifact and the `output[]`
   decomposition. The Orchestrator's supervisor then mechanically creates downstream tasks from the approved `output[]`.

   ```yaml
   # Task model extension
   tasks:
     - id: task-1
       role_pair: code-planning-pair    # links task to its role-pair in pipeline config
       status: CODING_PLAN_APPROVED
       output:                          # structured subtask definitions
         - desc: "Implement user authentication endpoint"
           done_when: "POST /auth/login returns 200 with JWT token"
           scope: "auth module, JWT signing"
           spec_ref: "specs/auth.md#login"
         - desc: "Implement session refresh endpoint"
           done_when: "POST /auth/refresh returns 200 with new JWT"
           scope: "auth module, token refresh"
           spec_ref: "specs/auth.md#refresh"
   ```

   The `role_pair` field links the task to its role-pair in the pipeline config. The supervisor
   uses it to resolve state transitions (e.g., `claim_task` → `role-pair.executing`). Set
   mechanically when the task is created — by the Orchestrator for initial tasks, by the
   supervisor for downstream tasks created from `output[]`. `liza validate` must reject
   tasks whose `role_pair` is absent or not present in the frozen pipeline config.

   **Relationship with `task.type`:** For pipeline-configured goals, `role_pair` is the
   mechanism for claimability and state resolution — the role-pair config defines which roles
   participate and which states are claimable. For legacy goals (no pipeline config), `task.type`
   continues to drive claimability via `EffectiveType()` (defaults to "coding" when empty).
   Both fields coexist; `role_pair` takes precedence when present.

   **Responsibilities:**
   - **Doer** writes `output[]` with full task specs (`desc`, `done_when`, `scope`, `spec_ref`) — same self-validation
     gates as the current Planner (Rule 2 / DoR)
   - **Reviewer** validates `output[]` as part of the review — catches underspecified or poorly scoped subtasks
     before they reach the next pair
   - **Orchestrator's supervisor** reads approved `output[]` and creates one downstream task per entry — purely mechanical,
     no LLM judgment involved in task creation

   **Canonical code-plan artifact path:** Code-planners MUST save their plan document to
   `specs/plans/<YYYYMMDD-HHMMSS>-<task-id>.md` (timestamp from `date -u +'%Y%m%d-%H%M%S'`
   at plan creation time). The timestamp ensures uniqueness across attempts.
   Code Plan Reviewers reject plans at non-canonical paths.

   **Path convention for `spec_ref`:** All `spec_ref` values in `output[]` (and on tasks generally) MUST
   use **repo-relative paths** (e.g. `specs/plans/auth-module.md`), never worktree-prefixed paths
   (e.g. ~~`.worktrees/code-planning-1/specs/plans/auth-module.md`~~). Doers work inside worktrees,
   but the worktree content merges to the integration branch — the repo-relative path is the
   stable reference that survives the merge. Reviewers should reject `spec_ref` values containing
   `.worktrees/` prefixes. `liza validate` enforces this constraint.

   **Cardinality semantics:**
   - `per-subtask`: create one downstream task per entry in `output[]`. Each entry provides `desc`, `done_when`, `scope`, `spec_ref`.
   - `one-to-one`: creates one child task. The parent task is the child's input, not a template — the child's `desc`, `done_when`, `scope` describe the *next phase's work* (e.g., "produce a coding plan for [parent US]"), not a copy of the parent's fields. `parent_task` links back. `spec_ref` points to the parent's artifact. No `output[]` needed. The `liza proceed` command (or supervisor for auto) generates the child's fields from the transition definition.

8. ✅ Make the Orchestrator's supervisor create the downstream tasks from `output[]` on the configured transitions.

   **Implementation note:** Downstream task creation uses two paths:
   - **Manual transitions** (`trigger: manual`): Human invokes `liza proceed` to create child tasks.
   - **Automatic transitions** (pipeline-configured goals): The orchestrator's supervisor
     creates a checkpoint with `checkpoint_trigger: PLANNING_COMPLETE` when planning tasks
     are merged. After the human reviews and resumes, the orchestrator's PreWork executes
     `ExecuteAvailableTransitions` to create child tasks. This ensures human review of
     planning output before coding begins.

   In both cases, the supervisor handles intra-pair transitions (claim → execute → submit →
   review → approve/reject). For pipeline-configured goals, transition definitions are
   resolved from the frozen config via the resolver; for legacy goals, a hardcoded transition
   map is used as fallback.

---

### Phase 2: US Writing Sub-pipeline

1. Add the new role pairs:
  - Epic Planner: creates Epic tasks, one per Epic, (and updates them based on the Epic Plan Reviewer feedback) from the vision document
    Epic Plan Reviewer: reviews the submitted Epic tasks
  - US Writer: consumes a ready Epic task and creates US tasks (and updates them based on the US Reviewer feedback)
    US Reviewer: reviews the submitted US tasks

  The two reviewers can reject specs that are untestable, ambiguous, or scope-creeping, just as the Code Reviewer rejects
  bad code. The contract applies to specs, not just code.

  **Epic Planner decomposition guidance:**

  The Epic Planner decomposes a vision document into epics. Each epic becomes a US Writer
  task via the `epic-to-us` transition (`per-subtask` cardinality). Getting epic granularity
  wrong propagates as task topology — a bad decomposition approved by the reviewer fans out
  into N US Writer tasks before anyone notices the framing was off.

  Granularity heuristics for right-sized epics:

  | Signal | Diagnosis |
  |--------|-----------|
  | Epic spans multiple independent capabilities (different persona clusters, unrelated subsystems) | Too broad — split by capability boundary |
  | Epic would produce >8 user stories to cover its scope | Too broad — find a natural seam to split |
  | done_when requires outcomes across unrelated subsystems | Too broad — each subsystem likely deserves its own epic |
  | Epic description contains conjunctions joining unrelated capabilities ("auth and notifications and reporting") | Too broad — same smell as composite coding tasks |
  | Epic is a single user action with one acceptance criterion | Too narrow — this is a user story, not an epic |
  | Epic can be implemented in a single coding task without further decomposition | Too narrow — skip the US Writer stage |
  | Epic would produce <2 meaningful user stories | Too narrow — merge with an adjacent epic or promote the stories directly |

  A right-sized epic:
  - Represents one cohesive capability area (e.g., "user authentication", "task import/export")
  - Serves a coherent persona cluster — the US Writer shouldn't need to context-switch between
    unrelated user types within a single epic
  - Is expected to decompose into 3–8 user stories
  - Can be delivered and validated independently of other epics (minimal cross-epic dependencies)
  - Has a done_when that is falsifiable at the epic level (all stories passing their ACs satisfies
    the epic's done_when)

  The Epic Planner's prompt template must include these heuristics (parallel to the Code
  Planner's `TASK DECOMPOSITION PRINCIPLE` block added in Phase 1).

  **Epic Plan Reviewer checklist:**

  The Epic Plan Reviewer gates approval on decomposition quality, not just artifact quality.
  The prompt template must include a review checklist (parallel to the Code Plan Reviewer's):

  | Gate | Reject if |
  |------|-----------|
  | Cohesive capability | Epic spans multiple unrelated capabilities — reject with split suggestion |
  | Right-sized scope | Epic would produce >8 or <2 user stories — reject with merge/split suggestion |
  | Falsifiable done_when | done_when is vague, untestable, or requires outcomes across unrelated subsystems |
  | Persona coherence | Epic mixes unrelated persona clusters — US Writer would need to context-switch |
  | Independence | Epic has unnecessary coupling to other epics — verify cross-epic dependencies are true ordering constraints |
  | Vision coverage | Epics collectively miss or exceed vision scope — reject with gap/scope-absorption note |

  **US Writer and US Reviewer guidance:**

  The US Writer uses the existing `user-story-writing` skill, which already defines SMARC
  criteria, persona quality gates, acceptance criteria standards, and self-review checklists.
  The US Writer's prompt template must reference this skill. The US Reviewer uses the existing
  `spec-review` skill, supplemented by the user-story-specific anti-patterns from the
  `user-story-writing` skill (Persona Laundering, Giant Story, Wishful Story, etc.).

2. Change the responsibility of the Orchestrator
   On system start, it still takes a document as an input (currently the "vision" document) but depending on its content, it will:
   - create a task for the Code Planner, as per phase 1, if the document is already and without doubt a detailed spec (structured requirements or list of US).
   - create a task for the Epic Planner otherwise (default when uncertain — skipping the spec phase is riskier than redundant refinement).

   Later on, approved US tasks transition to Code Planner tasks via `liza proceed` (manual `us-to-coding` transition).

   This behavior is configurable in the yaml config:
   ```yaml
   entry-points:
     general-objective: epic-spec-subpipeline.epic-planning-pair
     detailed-spec: coding-subpipeline.code-planning-pair
   ```

3. Add the specification sub pipeline.
   The full pipeline YAML replaces the Phase 1 config (which only defined the coding sub-pipeline):
   ```yaml
   pipeline:
     agent-roles:
       epic-planner: "Epic Planner"
       epic-plan-reviewer: "Epic Plan Reviewer"
       us-writer: "US Writer"
       us-reviewer: "US Reviewer"
       code-planner: "Code Planner"
       code-plan-reviewer: "Code Plan Reviewer"
       coder: "Coder"
       code-reviewer: "Code Reviewer"

     role-pairs:
       epic-planning-pair:
         doer: epic-planner
         reviewer: epic-plan-reviewer
         states:
           initial: DRAFT_EPIC_PLAN
           executing: EPIC_PLANNING
           submitted: EPIC_PLAN_TO_REVIEW
           reviewing: REVIEWING_EPIC_PLAN
           approved: EPIC_PLAN_APPROVED
           rejected: EPIC_PLAN_REJECTED

       us-writing-pair:
         doer: us-writer
         reviewer: us-reviewer
         states:
           initial: DRAFT_US
           executing: WRITING_US
           submitted: US_TO_REVIEW
           reviewing: REVIEWING_US
           approved: US_APPROVED
           rejected: US_REJECTED

       code-planning-pair:
         doer: code-planner
         reviewer: code-plan-reviewer
         states:
           initial: DRAFT_CODING_PLAN
           executing: CODE_PLANNING
           submitted: CODING_PLAN_TO_REVIEW
           reviewing: REVIEWING_CODING_PLAN
           approved: CODING_PLAN_APPROVED
           rejected: CODING_PLAN_REJECTED

       coding-pair:
         doer: coder
         reviewer: code-reviewer
         states:
           initial: DRAFT_CODE
           executing: IMPLEMENTING_CODE
           submitted: CODE_TO_REVIEW
           reviewing: REVIEWING_CODE
           approved: CODE_APPROVED
           rejected: CODE_REJECTED

     sub-pipelines:
       epic-spec-subpipeline:
         steps:
           - epic-planning-pair
           - us-writing-pair
         transitions:
           - name: epic-to-us
             from: epic-planning-pair.approved
             to: us-writing-pair.initial
             trigger: manual  # human validates epic decomposition
             cardinality: per-subtask  # supervisor creates one downstream task per entry in output[]

       coding-subpipeline:
         steps:
           - code-planning-pair
           - coding-pair
         transitions:
           - name: code-plan-to-coding
             from: code-planning-pair.approved
             to: coding-pair.initial
             trigger: manual  # human validates implementation plan before coding begins
             cardinality: per-subtask  # supervisor creates one downstream task per entry in output[]

     pipeline-transitions:
       - name: us-to-coding
         from: epic-spec-subpipeline.us-writing-pair.approved
         to: coding-subpipeline.code-planning-pair.initial
         trigger: manual
         cardinality: one-to-one  # approved task itself is the input, no output[] needed

     # Orchestrator is implicit — mandatory, unique, not part of any pipeline.
     # It reads entry-points below to classify input and dispatch to the right sub-pipeline.
     entry-points:
       general-objective: epic-spec-subpipeline.epic-planning-pair
       detailed-spec: coding-subpipeline.code-planning-pair
   ```
   The Orchestrator is an agent but doesn't appear in the config file because it is mandatory, unique and doesn't belong to any pipeline.
   It reads the entry-points and the input material, then dispatches to the appropriate sub-pipeline, creating the initial task for its planner.

### Master planning role-pairs

Planning fan-out is represented as a master role-pair followed by the specialized role-pair inside the same sub-pipeline. The master and specialized pairs reuse the same roles; `decomposition-root: true` selects the master prompt behavior and output validation.

```yaml
role-pairs:
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

  architecture-pair:
    doer: architect
    reviewer: architecture-reviewer
    states:
      initial: DRAFT_ARCHITECTURE
      executing: ARCHITECTING
      submitted: ARCHITECTURE_TO_REVIEW
      reviewing: REVIEWING_ARCHITECTURE
      approved: ARCHITECTURE_APPROVED
      rejected: ARCHITECTURE_REJECTED

sub-pipelines:
  architecture-subpipeline:
    steps:
      - architecture-main-pair
      - architecture-pair
    transitions:
      - name: arch-decompose
        task-slug: architecture
        from: architecture-main-pair.approved
        to: architecture-pair.initial
        trigger: auto
        cardinality: per-subtask
```

`decomposition-root: true` is valid only when the role-pair has exactly one outgoing same-subpipeline `trigger: auto`, `cardinality: per-subtask` transition to another role-pair. It is invalid on terminal steps, non-auto transitions, non-`per-subtask` transitions, or multiple outgoing decomposition transitions.

`INITIAL_PLANNING` uses entry points as specialized targets, then resolves the mapped master role-pair from the `decomposition-root` transition. It creates exactly one task: the specialized target for simple work, or the mapped master target when the goal would otherwise fan out or when boundary placement is uncertain. Existing frozen `.liza/pipeline.yaml` files are not migrated; users must run a new `liza init` to receive master role-pairs, auto-decompose transitions, and updated entry-point routing.

`architecture-to-code-plan` remains the Case A bypass. It targets `coding-subpipeline.code-planning-pair.initial` from specialized `architecture-pair.approved` output and does not route through `code-planning-main-pair`.

Master output entries must carry typed decomposition metadata and the framework artifact ref consumed by generated specialized children:

```yaml
output:
  - desc: "Plan authentication implementation"
    done_when: "Auth implementation plan is reviewed and decomposes coding work"
    scope: "auth module"
    spec_ref: specs/auth.md
    plan_ref: specs/plans/auth-master-plan.md
    task_depends_on: ["architecture-1"]
    decomposition:
      owned_files: ["internal/auth/service.go"]
      owned_modules: ["auth"]
      read_only_depends_on: []
      read_only_task_depends_on: ["architecture-1"]
      interfaces_owned: ["AuthService"]
      interfaces_consumed: ["UserRepository"]
      coverage_notes: "Owns authentication behavior while consuming user lookup read-only."
```

`epic-planning-main-pair` and `code-planning-main-pair` require `plan_ref`; `architecture-main-pair` requires `arch_ref`. `read_only_depends_on` and `read_only_task_depends_on` document read-only consumption and must be mirrored in scheduler-facing `depends_on` and `task_depends_on`. Specialized epic outputs keep the existing downstream `epic_ref` behavior for `us-writing-pair`; the epic master framework uses `plan_ref`.

---

## Sprint Governance Impact

Sub-pipelines introduce role-pair-specific terminal states. Today, sprint completion
checks only `{MERGED, ABANDONED, SUPERSEDED}`. With configurable pipelines, this
remains unchanged: every role-pair's supervisor merges the worktree branch after
approval, so MERGED is the universal sprint-terminal for all role-pairs.

Sprint scope is inferred from the tasks in `sprint.scope.planned[]`. Each task carries
a `role_pair` field — the sprint completion predicate checks that each task has reached
MERGED (or ABANDONED/SUPERSEDED).

**Sprint homogeneity:** Sprints are not restricted to a single role-pair — the predicate
handles mixed-role-pair sprints by checking each task against its own role-pair's terminal.
In practice, sprints tend to be homogeneous because pipeline transitions create downstream tasks
for the next sprint, so each sprint naturally contains tasks from one pipeline stage. But
the machinery doesn't enforce this — a sprint with both code-planning and coding tasks
completes when all tasks reach MERGED. Pipeline transitions happen between sprints (for
automatic transitions, after the human resumes a `PLANNING_COMPLETE` checkpoint; for manual
transitions, via `liza proceed`).

**Sprint-terminal states by role-pair:**

| Role-pair | Sprint-terminal state | Notes |
|-----------|-----------------------|-------|
| all role-pairs | MERGED | Supervisor merges worktree after `approved` |

Plus ABANDONED and SUPERSEDED for all role-pairs.

All doers work in worktrees and commit their artifacts (code, plans, specs, stories)
to git. The supervisor merges the worktree branch to integration after the reviewer
approves — this is the same mechanical step for every role-pair. MERGED is therefore
not part of the YAML config (it's a cross-cutting post-approval action, like BLOCKED
or ABANDONED) but it is the universal sprint-terminal state.

**Implication:** The sprint completion predicate (`AllPlannedTasksTerminal`) remains
simple: terminal states are `{MERGED, ABANDONED, SUPERSEDED}` for all role-pairs.
No pipeline-aware derivation needed.

**Pipeline transitions and sprint boundaries:**

*Automatic transitions (pipeline-configured goals):*
- The orchestrator detects merged planning tasks with unconsumed `output[]` and checkpoints
  with `checkpoint_trigger: PLANNING_COMPLETE`.
- Human reviews planning output, then runs `liza resume`.
- Orchestrator PreWork detects the trigger + IN_PROGRESS status → executes
  `ExecuteAvailableTransitions` → child tasks created in the current sprint's scope.
- Sequence: planning tasks merged → checkpoint → human reviews → `liza resume` → child tasks created.

*Manual transitions (`liza proceed`):*
- `liza proceed <task-id> <transition-name>` executes a manual inter-pair transition
  (e.g., MERGED → DRAFT_CODE via `code-plan-to-coding`).
- It operates **between sprints** — the preceding sprint must be COMPLETED. CHECKPOINT
  is not sufficient (may have non-terminal tasks from the current sprint still in progress).
  The sprint must also not be STOPPED; `liza proceed` fails fast on STOPPED state.
- `liza resume` then starts the next sprint with the transitioned tasks in scope.
- Sequence: sprint completes → human reviews → `liza proceed` → `liza resume`.

**Sprint state machine impact:** The current sprint state machine treats COMPLETED as terminal
(no transitions out). Sub-pipelines require `liza resume` to work from COMPLETED — not to
reopen the completed sprint, but to archive it and create a new sprint with child tasks. This
is the same semantics as `liza resume` from CHECKPOINT when all tasks are terminal (see
`state-machines.md` §Sprint State Machine). The sprint state machine must be extended:
`COMPLETED` → (via `liza resume`) archives sprint, creates new sprint (IN_PROGRESS).

### Supervisor Model Changes

The declarative pipeline config generalizes the supervisor loop: the supervisor reads
role-pairs to determine doer vs reviewer behavior for task claiming, state transitions,
and exit handling. No role-specific supervisor code is needed beyond what the config
provides.

### MCP Tools / CLI Commands Compatibility

Existing tools (`claim_task`, `submit_for_review`, `submit_verdict`, `wt_merge`) are
semantically role-agnostic — they don't need to be replaced. What changes is how they
resolve target states: from hardcoded values to pipeline config lookups.

| Tool | Current hardcoded state | Generic: resolves from config |
|------|------------------------|-------------------------------|
| `claim_task` | → IMPLEMENTING | → role-pair.`executing` |
| `submit_for_review` | → READY_FOR_REVIEW | → role-pair.`submitted` |
| `submit_verdict(APPROVED)` | → APPROVED | → role-pair.`approved` |
| `submit_verdict(REJECTED)` | → REJECTED | → role-pair.`rejected` |
| `wt_merge` | precondition: APPROVED ∨ CODING_PLAN_APPROVED | precondition: role-pair.`approved` via resolver (legacy fallback preserved) |

The agent calls the same tool regardless of role. The supervisor resolves the target
state from the task's role-pair in the pipeline config (e.g., `submit_for_review` on a
Code Planner task resolves to CODING_PLAN_TO_REVIEW).

**Implementation note on `wt_merge` pipeline-awareness:** While `wt_merge` always produces
MERGED as output status (universal for all role-pairs), its precondition and internal
re-validation checks must be pipeline-aware. The approved status check (`isApprovedStatus`)
uses the pipeline resolver for pipeline tasks and falls back to legacy statuses
(APPROVED, CODING_PLAN_APPROVED) for legacy tasks. This also applies to
`markIntegrationFailed` (re-validates approved state under lock) and `handleApprovedMerges`/
`hasPendingMerges` in the supervisor (detect mergeable tasks). The `PipelineResolver`
interface includes `ApprovedStatus(rolePair)` for this purpose.

**Implemented in Phase 1:**
- Bootstrap prompt templates for Code Planner and Code Plan Reviewer roles
- `liza status` shows available manual transitions for tasks at sprint-terminal states
- `AvailableTransitions()` filters to manual-only, excludes already-executed transitions
- `release_claim` is pipeline-aware with role authorization
- `wt_merge` precondition and `handleApprovedMerges`/`hasPendingMerges` are pipeline-aware
- `logTaskSubmissionIfCompleted` uses pipeline resolver for submitted/executing status checks

**Deferred to follow-up spec:**
- Bootstrap prompt templates for Phase 2 roles, including:
  - Epic Planner: decomposition guidance with granularity heuristics (defined in Phase 2 §1)
  - Epic Plan Reviewer: review checklist with decomposition gates (defined in Phase 2 §1)
  - US Writer: integration with `user-story-writing` skill
  - US Reviewer: integration with `spec-review` + `user-story-writing` anti-patterns
- Orchestrator supervisor loop: wake conditions, `auto` transition polling, entry-point dispatch

---

## Known System Properties

Structural trade-offs inherent to the design, acknowledged here so implementers don't
rediscover them as bugs.

- **Partial declarativity.** The YAML config governs state naming and happy-path routing.
  Entry-point dispatch (Orchestrator) and cross-cutting meta-states (BLOCKED, MERGED,
  etc.) remain in code. This is intentional — full
  declarativity would require a DSL, which trades one complexity for another. The boundary
  is: YAML owns role-pair flow, code owns cross-cutting states and orchestration.

- **Sprint lock-step.** Sprint boundaries force all tasks to complete before transitions
  execute. For automatic transitions, this means the `PLANNING_COMPLETE` checkpoint only
  fires when all planning tasks are merged. This serializes independent tasks but is the
  price of human judgment at boundaries — allowing partial transitions mid-sprint would
  undermine the "complete → review → proceed" model.

- **output[] decomposition quality.** Reviewers validate both the doer's artifact and its
  `output[]` decomposition. Decomposition review (are these the right subtasks?) is harder
  than artifact review (is this plan sound?). A bad decomposition approved by the reviewer
  propagates as task topology, not as a single correctable artifact. The reviewer skill for
  spec pairs should address decomposition criteria — this is a skill concern, not a spec
  constraint.

- **Iteration cap calibration.** The rejection loop's iteration cap (currently 10) was
  calibrated for code iteration. Spec-producing role-pairs (Epic Planner, US Writer) may
  need lower caps — 3-4 cycles without convergence on a spec likely signals a framing
  problem, not incremental progress. Making `iteration_cap` configurable per role-pair is
  an implementation concern.

- **Single-intent decomposition.** The Code Planner prompt enforces single-intent task
  decomposition: each `output[]` entry must have exactly one intent (Atomic Intent from
  CORE.md Rule 2). This is enforced at the prompt level, not the schema level — the
  reviewer catches violations that slip through.

---

## Out of Scope

Perspective provided just to verify the future-proofness of the config model.

### Future insertion of an architecture phase

epic-spec-subpipeline → architecture-subpipeline → coding-subpipeline

with: N US tasks → 1 Architecture task → N Code Planner tasks (one per original US)

```yaml
architecture-pair:
  doer: architect
  reviewer: architecture-reviewer
  states:
    initial: DRAFT_ARCHITECTURE
    executing: ARCHITECTING
    submitted: ARCHITECTURE_TO_REVIEW
    reviewing: REVIEWING_ARCHITECTURE
    approved: ARCHITECTURE_APPROVED
    rejected: ARCHITECTURE_REJECTED
# ... (rest of role-pairs and sub-pipelines as above)
pipeline-transitions:
  - name: us-to-architecture
    from: epic-spec-subpipeline.us-writing-pair.approved
    to: architecture-subpipeline.architecture-pair.initial
    trigger: manual
    cardinality: collect  # all approved US tasks become context on the target. Hypothetical — would require new cardinality mode

  - name: architecture-to-coding
    from: architecture-subpipeline.architecture-pair.approved
    to: coding-subpipeline.code-planning-pair.initial
    trigger: manual
    cardinality: per-context-task  # fan out from the architecture task's context. Hypothetical — would require new cardinality mode
    context:
      - architecture-subpipeline.architecture-pair.approved  # arch doc attached to each
```

The task model would also need an optional arch_ref attribute (in addition to the existing spec_ref).
It would be set when fanning out from the architecture task's context.
