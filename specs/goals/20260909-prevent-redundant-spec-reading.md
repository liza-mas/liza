# Prevent Redundant Spec Reading

Status: draft
Entry point: `technical-spec`

## Objective

Reduce planning-agent context consumption without losing intent, traceability, or
implementation readiness. Planning artifacts must reference inherited authority
and contain only decisions introduced at their own stage.

The first implementation applies the contract to artifact-producing skills,
rendered planning prompts, consumer read instructions, and their reviewers. A
controlled CAP-001 vertical slice validates the design from the `epm-1` master
decomposition through the ST-004 coder task.

## Evidence and Baseline

- `../omni/.omni-ee/log-analysis.md` reports heavy repeated reading of epics,
  stories, architecture plans, and code plans.
- The integration corpus after `6f47c830` contains a representative CAP-001
  chain whose tracked planning artifacts total 489,020 bytes and 57,061 words.
- Its rendered coder prompt is 33,879 bytes; the marker-delimited region from
  `=== ASSIGNED TASK ===` through the last line before `CODER STATE TRANSITIONS:`
  is 15,816 bytes and 1,719 words. Common coder boilerplate is excluded.
- The CAP-001 code plan alone is 212,558 bytes and 24,236 words.
- Current producer/reviewer instructions combine self-containment requirements,
  broad upstream reads, and character-identical Markdown/`output[]` fields.
- The chain includes later human decisions that legitimately supersede earlier
  artifact text. Without explicit supersession provenance, a reference-only
  consumer cannot distinguish that case from unauthorized downstream drift.
- The code plan accumulates five corrections and repeats ST-004 requirements in
  prose, embedded JSON, adjacent JSON, and the rendered coder task.

The experiment manifest must identify the exact files and source revision used.
Measurements compare the source payload actually supplied by current routing with
the rewritten payload for the same ST-004 outcome; they must not claim savings by
dropping requirements.

## Reference, Don't Restate Contract

### Core rule

Every information item has one authoritative owner. A downstream artifact:

1. cites inherited information by stable ID and anchored artifact reference;
2. states only its local decision, refinement, allocation, or exception; and
3. never silently changes inherited behavior, constraints, or rationale.

Self-contained means locally actionable with declared references, not a copy of
the upstream corpus.

### Authority matrix

| Stage | Owns | Must reference or omit, never restate |
|---|---|---|
| Goal | Problem, users, scope, behavior, global NFRs, success | External evidence |
| Master decomposition | Coverage partition, scope/interface ownership, cross-child ordering | Goal requirements and rationale |
| Specialized epic | Capability-local product interpretation and story boundaries | Goal and master shared constraints |
| User story | Story behavior, ACs, edge cases, story-local exclusions | Epic personas, shared NFRs, and inherited contracts |
| Master architecture | Cross-scope components, interfaces, ownership, dependency direction | Product behavior and AC prose |
| Specialized architecture | Scope-local structure, interfaces, data flow, failure handling | Inherited behavior and shared architecture |
| Code plan | Concrete change units, file ownership, ordering, tests, requirement-to-task allocation | Requirement and architecture prose |
| Coder task | Orchestration envelope and direct executable references | Plan detail, AC text, and architecture rationale |
| Review feedback | Current unresolved findings, severity, evidence, and closure conditions | Unchanged task contracts; resolved/superseded findings stay only in the review ledger |

Only an owner may redefine its information class. A downstream stage that needs a
change must create an explicit correction routed to that owner or mark itself
blocked.

A later human decision may supersede an existing owner artifact. Before downstream
work consumes it, record the decision in that owner's decision record or an explicit
owner correction. Its reference carries an explicit later revision override. The
consumer cites that supersession rather than treating older text as current or
redefining it locally.

### References and source identity

- References are repository-relative `path#anchor` values. Whole-file references
  are allowed only when the whole file is the consumer's assigned scope.
- A heading anchor spans from that heading through the line before the next heading
  of the same or higher level. Sibling sections are outside its scope. When a
  consumer owns obligations across siblings, reference their common parent or list
  every required sibling anchor.
- Every semantic Markdown artifact declares a default source revision. A reference
  from another revision is listed adjacent to an explicit override in the form
  `path#anchor @ <40-hex-git-revision>`. Its adjacent `output[]` projection inherits
  those bindings through its anchored artifact reference; a standalone task envelope
  records `SOURCE REVISION` plus any overrides. No orchestration state field is added.
- Before dispatch, the prompt compositor compares each referenced blob at its
  declared revision with the same path at the current authoritative branch HEAD.
  A changed/deleted blob or missing anchor blocks as stale. The producer regenerates
  against current HEAD or binds an explicit later supersession revision, after
  which the comparison repeats.
- Consumers resolve pinned content with `git show <revision>:<path>` and select the
  declared anchor. They may read the worktree copy only when `git hash-object
  <path>` equals `git rev-parse <revision>:<path>`; current worktree content never
  silently substitutes for a differing pinned reference.
- A reference names a stable requirement, AC, interface, decision, or task ID when
  one exists. Prose summaries do not replace those IDs.
- Consumers read the assigned leaf section and its declared direct references.
  They do not recursively read every ancestor unless a direct reference identifies
  an unresolved dependency there.
- A valid read set must also be obligation-complete: every assigned behavior,
  boundary, dependency, and proof maps to at least one anchor whose span contains
  it. Each effective review finding maps to that anchored carrier or to its inline
  finding and closure condition. Resolving an anchor does not prove semantic
  coverage.
- If an anchor is missing, stale, contradictory, or insufficient, the consumer
  blocks or requests correction; it does not reconstruct authority from memory.

### Local sufficiency

An artifact is locally sufficient when its consumer can perform the stage's work
from:

- the assigned artifact section;
- its explicit direct references;
- the current task envelope; and
- relevant repository evidence for decisions owned by that stage.

Local sufficiency does not permit copied upstream rationale or acceptance text.
Exact executable values may be materialized at an action boundary—task IDs, paths,
commands, schema fragments, or protocol examples—when the artifact labels their
source and a reviewer can verify parity.

### Markdown and structured output

Markdown is canonical for semantic requirements, rationale, architecture, and
detailed task contracts. Structured `output[]` is canonical for orchestration:
concise intent, scope boundary, dependencies, validation commands, and anchored
artifact references.

`desc`, `done_when`, and `scope` are task-envelope fields, not containers for the
full task specification:

- `desc`: one intent, normally at most 160 characters;
- `done_when`: one falsifiable completion observation, normally at most 400
  characters; and
- `scope`: owned boundary and principal exclusions, normally at most 400
  characters.

Longer fields require a recorded reason that the information is executable at the
dispatch boundary and has no authoritative home behind a reference. Review checks
referential and semantic parity, not character identity with Markdown prose.
Record the exception in the owning Markdown artifact's `Output` section, naming
the field and rationale; no envelope or state-schema field is added.

Stages that currently emit no structured child-task projection do not add one.
In particular, Story Writer Markdown remains represented by the dispatch entry
created by its parent epic; adding an adjacent duplicate JSON artifact is forbidden.

### Corrections

A correction is a delta artifact. It declares:

- the exact artifact and anchors corrected;
- replacement decisions and their rationale;
- unchanged contracts inherited by reference; and
- the effective downstream reference that supersedes the old one.

It must not append a history of prior corrections or reproduce the unchanged
baseline. Rendered task context includes only the effective correction chain.

## Components and Interfaces

### Shared authoring contract

Create one compact reference document under `skills/shared/references/` containing
the normative rules above. Artifact-producing and reviewing skills link to it and
add only stage-specific ownership rules. This centralizes semantics without adding
the full contract to every rendered prompt. `skills/shared/` is an intentional
non-skill asset subtree: it has no `SKILL.md`, and embed/brand-render consistency
tests must prove it is copied and remains reachable from installed skills.

### Architectural decision record

Before changing producer or reviewer contracts, create an ADR under
`specs/architecture/ADR/` using the next available number. It must record:

- the authority matrix and local-sufficiency definition;
- Markdown versus `output[]` ownership;
- anchored references and source-revision semantics;
- correction-delta behavior;
- prospective compatibility for existing artifacts; and
- the rejected alternatives: independently complete artifacts,
  character-identical representation parity, token ceilings alone, and a
  big-bang corpus migration.

The shared authoring contract and prompt/skill changes must reference the ADR
rather than restating its rationale.

### Producer skills and formats

Revise `epic-writing`, `user-story-writing`, and `architecture-planning` plus their
format references. Replace self-containment language that requires copied upstream
content with local-sufficiency language, explicit authority/reference sections,
and stage-local decision requirements.

### Prompt composition

Planning-role prompt blocks render a short stage-specific authority clause. Update
master decomposition, capability scoping, implementation-phase, base-prompt read
routing, and correction instructions. Do not append the new clause while retaining
contradictory broad-read or verbatim-parity mandates.

### Review boundary

Epic, story, architecture, and code-plan reviewers reject:

- an unreferenced requirement, constraint, threshold, or interface;
- unchanged inherited prose presented as local authority;
- full-parent reads where an assigned anchor is sufficient;
- semantic conflict with an inherited owner;
- `output[]` used as a second detailed specification;
- detailed prose duplicated merely to make representations character-identical;
- a correction that accumulates unchanged history; or
- a declared read set whose anchor spans omit an assigned consumer obligation.

Static contract/prompt tests—not artifact reviewers—reject instructions requiring
character-identical representation parity.

Repeated labels, IDs, short task intents, exact action-boundary values, and clearly
marked quotations are not violations.

### Reader routing

Architects, code planners, coders, and reviewers receive:

1. their task envelope;
2. the primary anchored artifact reference;
3. direct dependency/correction references; and
4. repository exploration authority appropriate to their stage; and
5. for retry tasks, the current effective review findings and closure conditions.

Review feedback is included inline when no durable anchored carrier exists;
otherwise the task cites that carrier. It contains unresolved required findings
only, never superseded review history.

Before dispatch, the producing stage records a compact obligation-to-reference
coverage list beside its direct references, using stable requirement, boundary,
dependency, proof, or finding IDs where available. Inline findings name their
inline carrier. No orchestration-state field is added. The reviewer rejects
uncovered obligations or anchors whose spans do not contain the mapped obligation.
The prompt compositor mechanically checks revision, path, anchor existence, and
span extraction; it does not infer semantic completeness.

Generic instructions to read the full goal plus every intermediate artifact are
removed where the primary artifact already delegates exact authority.

## Data Flow

```text
authoritative owner
  -> anchored reference + local downstream delta
  -> concise output[] dispatch projection
  -> next stage reads leaf + direct refs
  -> reviewer checks authority, coverage, and contradiction
  -> coder receives task envelope + plan/story anchors + effective corrections
     + unresolved effective review findings
```

Failures are explicit: unresolved reference, conflicting owners, or missing local
context blocks the task. The pipeline must not compensate by inventing a summary.

## Validation

### Static and render tests

- Prompt-render tests assert required stage ownership clauses and forbid superseded
  broad-read, copied-content, and character-identical-parity wording.
- Producer/reviewer pairs are tested together so removing a coverage backstop from
  one side cannot silently weaken the other.
- Prompt-compositor tests prove heading-span extraction and reject unresolved,
  stale, or mechanically invalid coverage references; semantic coverage remains a
  producer/reviewer gate.
- Existing topology, task-state, dependency, artifact-reference, and white-label
  invariants remain unchanged.
- Embed/brand-render tests cover the non-skill `skills/shared/` reference subtree.
- Compare total bytes across touched contracts, skills, formats, and prompt
  templates. Growth must not exceed new semantic content; prefer net reduction.

### Artifact acceptance

For representative generated artifacts:

- every local statement classifies as owned decision, reference, or permitted
  action-boundary materialization;
- every inherited requirement maps to an existing authoritative ID/reference;
- every consumer obligation appears in the producer's coverage list and lies within
  its mapped anchor span or effective inline-review carrier;
- no normalized parent/child prose run of 40 or more words is duplicated outside
  code, commands, schemas, or marked quotations;
- all references and anchors resolve at the recorded revision; and
- a consumer simulation completes without an undeclared ancestor read.

## Controlled CAP-001 Experiment

Use `/tmp/prsr/`, preserving source-relative paths. Rewrite the actual vertical
slice from:

1. `20260907-230845-epm-1.md` and `epm-1.output.json` entry 0;
2. `20260907-234617-epm-1-ep-0.md` and its output;
3. `cap-001-2.md`, retaining ST-004 and reference-only sibling boundaries;
4. CAP-001 master and specialized architecture plus their outputs;
5. the selected-ownership correction and its output;
6. the CAP-001 code plan and output entry 3, reduced to ST-004; and
7. the complete ST-004 task-variable region composed into the rendered coder
   prompt, including effective review feedback but excluding common boilerplate.

Lateral CAP-002/CAP-003 inputs remain anchored references, not copied branches.
The fixture includes a manifest mapping each rewritten artifact to its source and
records source/rewrite bytes and words.

The experiment passes only if:

1. all ST-004 ACs, selected-identity forms, commitment evidence, locking/race
   behavior, zero-write proof, atomic-source behavior, file ownership, exclusions,
   and validation commands remain traceable;
2. no requirement or p95 threshold is invented;
3. all JSON parses and all internal references/anchors resolve;
4. a cold leaf-context audit finds ST-004 actionable without a broad ancestor read;
5. aggregate artifact bytes fall by at least 50%; and
6. the task-variable rendered context bytes—assigned task, collective scoping, and
   effective prior-review feedback, excluding common coder boilerplate—fall by at
   least 50%.

The fixture is experimental and disposable. Preserve measurements and conclusions
in this goal or a follow-up implementation record; do not promote `/tmp/prsr/` into
a production test dependency.

### Experiment result

Against tracked sources at revision
`1691031b9c9c58ac814b4b3e63ff430beaec6dfe` and the runtime prompt captured at
that workspace HEAD, the corrected fixture measured:

| Payload | Source | Rewrite | Reduction |
|---|---:|---:|---:|
| Planning artifacts | 489,020 bytes / 57,061 words | 27,257 bytes / 2,731 words | 94.43% bytes / 95.21% words |
| Task-variable rendered context | 15,816 bytes / 1,719 words | 4,954 bytes / 533 words | 68.68% bytes / 68.99% words |

JSON parsing, task-envelope budgets, repository-relative references, source
anchors, and the ST-004 semantic checklist pass the author audit. A prose-only
cross-file scan finds no duplicated run of 40 words. Raw overlaps consist of
permitted task-envelope, reference-path, owned-file, and validation materialization
at action boundaries. No latency percentile was introduced.

The measurements validate the quantitative hypothesis for this slice. They combine
precise read routing with leaner prose and therefore are not a pure compression
benchmark. Independent review exposed missing review feedback, executable
identifiers, and two valid-but-incomplete anchor spans; all are restored. The same
reviewer's reconciliation and final readiness verdict remain pending, so this author
audit does not certify its own correction.

## Rollout and Compatibility

- Create and approve the ADR before landing contract-enforcement changes.
- Land producer and matching reviewer changes atomically by pipeline stage.
- Apply the contract prospectively to new artifacts and explicit corrections.
- Existing artifacts and state references remain readable; no bulk rewrite or
  state-schema migration is required.
- A correction to a legacy artifact may use the delta form while referencing the
  legacy baseline.
- Validate one end-to-end planning run before making the new wording universal.
- Roll back by reverting the prompt/skill changes; generated reference-based
  artifacts remain ordinary Markdown and JSON.

## Risks

- **Loss hidden by shorter prose:** coverage and leaf-context audits fail the
  change even when byte targets pass.
- **Reference chasing replaces duplication:** direct stage read sets and anchored
  refs bound traversal.
- **Mixed old/new artifacts:** prospective compatibility and explicit corrections
  avoid a big-bang migration.
- **Brittle length limits:** field budgets are review defaults, not reasons to omit
  executable context.
- **Reviewer overreach:** exceptions preserve exact values needed at the action
  boundary.
- **Contract duplication:** one shared semantic reference plus local stage clauses
  avoids copying the entire policy into every skill and prompt.

## Out of Scope

- Rewriting existing project specifications in bulk.
- Changing pipeline topology, task states, review quorum, or artifact-ref storage.
- Automatic semantic summarization or retrieval infrastructure.
- Treating token count alone as correctness evidence.
- Adding structured output to stages that do not need child-task dispatch.
- Changing CAP-001 product or implementation requirements.
- Retaining `/tmp/prsr/` as a repository fixture.

## Acceptance Criteria

1. The shared contract and every affected producer/reviewer agree on the authority
   matrix, local sufficiency, correction deltas, and representation split.
2. Rendered prompts contain no instruction pair that simultaneously requires full
   upstream copying and broad rereading.
3. Code-plan Markdown and `output[]` are checked for referential/semantic parity,
   not character-identical detailed prose.
4. Consumers receive explicit, obligation-complete minimal read sets and block on
   unresolved authority or an anchor-span coverage gap.
5. New downstream artifacts cannot introduce or change inherited NFRs without an
   explicit owner correction.
6. Existing artifacts remain consumable without migration.
7. The controlled CAP-001 experiment meets all six pass conditions above and
   records its evidence.
8. Touched-file pre-commit, prompt-render tests, and the relevant pipeline tests
   pass with no ignored pre-existing failure.
9. An approved ADR records the authority model, rationale, compatibility policy,
   and rejected alternatives before prompt/skill enforcement lands.
