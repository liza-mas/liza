---
name: epic-writing
description: Transform vision documents into structured epics that bound story-writing
---

# Objective

Transform a vision document or high-level product requirement into a structured epic — a markdown
document, git-tracked, treated with the same rigor as code.

**An epic has two audiences: first the teller, then the doer.** The Intent Review (Part 1) lets the
intent owner verify scope and interpretation in minutes. The Execution Contract (Part 2) gives
Story Writers and the Orchestrator everything they need to decompose and implement. Neither section
is optional.

The Execution Contract is the primary input to the **user-story-writing** skill. Each capability
section becomes a Story Writer task: the Story Writer reads the epic's Personas, General
Information, and the capability section — nothing else. Write accordingly. A capability that
requires the Story Writer to re-read the vision to understand what they're building is incomplete.

An epic bounds one cohesive capability area, serving a coherent persona cluster, expected to
decompose into **3–8 user stories** across its capabilities. It lives one level above user stories.
Its job is to answer: *what is being built, for whom, and what is explicitly out of scope* — not
how. It decomposes into capabilities; capabilities decompose into story documents.

# Trigger

Use this skill when:
- Orchestrator creates an epic-writing task (§BRAND_NAME_TITLE§ mode)
- User asks to write an epic for a feature area or product milestone (Pairing mode)
- Story-writing is blocked because scope has not been bounded at the capability level

# Inputs

- **References** to one or more sections of source material (vision doc, product brief,
  strategy doc, OKRs, or prior epics)
- **Output file path**: In §BRAND_NAME_TITLE§ mode, assigned by the Orchestrator on the blackboard. In Pairing
  mode, propose a path under the project's epic directory and confirm with the human before writing.

**Scope discipline (two-tier):**
- **Upfront:** Read task references and scan existing epics in the same domain for consistency.
  Same domain = same parent directory, or epics referenced by the same vision doc section.
  This minimal consistency check is always permitted.
- **Reviewer-driven:** Broader expansion only when the reviewer's feedback identifies specific
  additional material to consult.

In both cases, declare what you read and why in the References section.

# Output Format

Produce a markdown file at the output path.
Use the [Epic format](references/epic-format.md) template.

# Protocol

## 0. Size

Skim the source material for obvious splitting signals before deep parsing. A badly-sized epic fans
out into N Story Writer tasks before anyone notices the framing was off.

**Granularity signals:**

| Signal | Diagnosis |
|--------|-----------|
| Scope spans multiple independent persona clusters or unrelated subsystems | Too broad — split at capability boundary |
| Would produce >8 user stories to cover its scope | Too broad — find a natural seam |
| Completion criteria require outcomes across unrelated subsystems | Too broad — each subsystem likely deserves its own epic |
| Description contains conjunctions joining unrelated capabilities ("auth and notifications and reporting") | Too broad — composite epic smell |
| Entire scope is a single user action with one acceptance criterion | Too narrow — this is a story, not an epic |
| Could be implemented in a single coding task without further decomposition | Too narrow — skip the Story Writer stage |
| Would produce fewer than 2 meaningful user stories | Too narrow — merge with an adjacent epic or promote directly |
| Cannot be delivered or validated independently of another epic | Too coupled — reconsider the boundary |

If a single epic's scope spans multiple independent capability areas, flag this — the epic is too
broad and needs splitting. This is about epic sizing, not about the number of epics a task produces.
When a planner task decomposes a vision document, producing multiple well-sized epics from one
task is normal — apply this skill once per epic boundary.

If sizing is ambiguous, flag before proceeding. A wrong cut here is more expensive to fix than a
brief hold.

## 1. Parse

Read the source material. Before writing anything, identify:

1. **The outcome**: what ships, what changes for the user, how success is measured
2. **The personas**: who benefits and in what context — if the source doesn't name them, infer the
   minimum set from the described actions
3. **The capabilities**: the coarse functional groupings that together constitute the epic
4. **What is explicitly excluded**: the source material almost always implies adjacent scope; name it
5. **What is unresolved**: gaps, contradictions, or decisions the source defers

Do not start writing until you can answer all five. If you cannot answer one, it becomes an Open
Question — not a blank you fill with imagination.

## 2. Decompose into Capabilities

A capability is a user-facing behavior the team can build, test, and hand off to a Story Writer as
a bounded unit. It is not a technical module, a sprint, or a story. It is a slice of product
behavior.

Each capability must pass the self-containment test defined in the Objective: a Story Writer
handed the Personas, General Information, and this capability section can begin writing without
reading the vision source.

**Capability heuristics:**
- A capability named after a technical concern ("database layer", "API integration") is
  probably not a capability — it's a task. Find the user-facing behavior it enables and name that.
- Capabilities have a natural partial order: identify true ordering constraints (CAP-B cannot be
  built without CAP-A's output) vs. merely convenient ones. Do not over-constrain the plan.

**Story document planning:**
For each capability, list the story documents it decomposes into. A story document covers one
cohesive unit of work a Story Writer can own independently. You are naming documents, not writing
stories — the Story Writer does that. Each entry needs only a short title and any known dependency
notes.

## 3. Write the Intent Review

Write Part 1 before Part 2. The Intent Review is a forcing function: if you cannot write a clear
Before/After promise, the epic scope is not yet understood.

**Promise (Before/After):** State what the product cannot do today and what becomes possible after
this epic ships. Plain language, no IDs, no jargon. A non-technical stakeholder should be able to
read this and say "yes, that's what I want" or "no, you misunderstood."

**Capability Map:** One row per capability. The human-facing outcome column restates the capability
as a user benefit — not the capability description (which is written for Story Writers), but its
plain-language consequence. The source intent column traces back to the vision material. The main
exclusion column names the most important thing this capability deliberately leaves out.

**Interpretation Decisions:** Surface every non-obvious inference from source to epic. The test: if
a reasonable reader of the source material might interpret a signal differently, it belongs here.
Omit this section only when the epic is a straightforward decomposition with no judgment calls.
Mark "Verify? Yes" when the interpretation is consequential — meaning a wrong call here changes
what gets built.

**Review Questions:** Write 3–5 targeted questions the intent owner can answer with a short
response. These are not open-ended feedback prompts ("does this look right?") — they are specific
verification points ("Is X intentionally out of scope, or should it move into this epic?", "Did
we correctly interpret Y as Z?", "Is this the right ownership boundary between EP-NNN and EP-MMM?").

## 4. Write the Execution Contract

**Completion Criteria** replace vague success metrics. They are the falsifiable condition that
closes the epic: when all story ACs pass, the completion criteria must be satisfied. Write them
as observable outcomes, not directions. "Users can create, edit, and delete tasks without data
loss" is a completion criterion. "Improve task management" is not. If you cannot write falsifiable
completion criteria, the epic scope is not yet understood — surface it as an Open Question.

**Personas** at the epic level describe who benefits from the whole epic. Capabilities may
narrow to specific sub-personas — note this in the capability description where relevant. A useful
persona includes environment and skill level when they affect product decisions. Compare:
"Operator: a person running the platform" vs. "Ops Engineer: a backend engineer on call who
monitors the platform via terminal and alerts and needs concise, machine-parseable output." The
second persona constrains every capability under it and tells the Story Writer what conventions
to assume.

**Out of Scope** is not optional. It protects Story Writers from scope absorption and the team from
scope creep. Name adjacent capabilities the source implies but this epic deliberately excludes.
If it's not named, Story Writers will assume it's included.

**Assumptions** at the epic level are strategic: they resolve ambiguity in the vision material
that would otherwise leave Story Writers unable to bound their own work. They are NOT technical
decisions. An assumption that names a data format, protocol, or library is premature solutioning —
state the behavioral constraint only.

**Confidence discipline:**
- HIGH: The source material strongly implies this; any reasonable reader would agree.
- MEDIUM: Reasonable inference, but the source is silent or oblique.
- LOW: You had to guess. This is blocking — a human must resolve it before story-writing begins.

**Open Questions** at the epic level are product-level decisions only: personas, scope boundaries,
completion criteria, or capability ordering. Technical open questions belong in story documents,
not here.

## 5. Self-Review

Before submitting for review, verify:

**Intent Review (Part 1):**
- [ ] Before/After promise is readable by a non-technical stakeholder
- [ ] Capability Map covers every capability — no row missing
- [ ] Capability Map exclusions match the Out of Scope sections in Part 2
- [ ] Every consequential interpretation appears in the Interpretation Decisions ledger
- [ ] Review Questions are specific and answerable — not open-ended feedback prompts
- [ ] Review Questions cover: scope boundaries, key interpretations, and ownership boundaries with adjacent epics

**Execution Contract (Part 2):**
- [ ] Completion criteria are falsifiable — all story ACs passing satisfies them
- [ ] Epic decomposes into 3–8 user stories (use story document count as a proxy)
- [ ] Every capability traces to a source reference — none invented
- [ ] Every capability passes the Story Writer self-containment test
- [ ] Every capability has a clear user-facing description (not technical framing)
- [ ] Personas include environment or skill-level detail where it affects product behavior
- [ ] Out of Scope names adjacent capabilities the source implies but this epic excludes
- [ ] No capability bleeds into story-level detail (data types, API shapes, UI copy)
- [ ] No assumption prescribes implementation — behavioral constraints only
- [ ] LOW-confidence assumptions are flagged as blocking
- [ ] No assumption overlaps with an Open Question — if overlap exists, keep the OQ
- [ ] Capability ordering constraints are true dependencies, not merely convenient ones
- [ ] Story document titles are scoped correctly — a Story Writer can own each one independently
- [ ] Re-read the source material — every source section maps to a capability (no gaps), and nothing was added beyond scope
- [ ] Sibling ambiguities from the same source gap treated consistently across capabilities

If self-review reveals issues, fix before submitting.

# Constraints

- **DO** write Part 1 for the intent owner (plain language, verification surface) and Part 2 for
  the Orchestrator and Story Writers (precise, bounded, operational).
- **DO** check existing epics in the same domain for consistency — contradictions between epic
  documents surface late and are expensive.
- **DO** surface contradictions between source references as an Open Question.
- **DO NOT** write user stories — epics only. Story content belongs in story documents.
- **DO NOT** modify existing epic documents unless the task explicitly scopes an update.
- **DO NOT** name technical solutions in capability descriptions. Name user-facing behavior.

# Anti-Patterns

- **Vision Laundering**: Capabilities named after technical layers ("Auth Service", "Data Pipeline")
  instead of user-facing behavior ("User can authenticate via SSO", "Operator can monitor ingestion").
- **Giant Epic**: Covers multiple independent persona clusters or unrelated subsystems. Flag to
  Orchestrator and split. The conjunction smell: "auth and notifications and reporting" in a single
  epic description is a reliable signal.
- **Dwarf Epic**: A single user action with one acceptance criterion — this is a story, not an epic.
  If it would produce fewer than 2 meaningful user stories, merge or promote directly.
- **Storified Epic**: Capabilities written with Given/When/Then or at story granularity.
  An epic describes what; stories describe done.
- **Unfalsifiable Completion Criteria**: "users have a better experience." Not falsifiable.
  Write a condition that is either true or false when story ACs are checked.
- **Missing Out of Scope**: Adjacent capabilities left unnamed. Story Writers will absorb them.
- **Assumption Burial**: Strategic assumptions embedded in capability descriptions instead of
  surfaced in the Assumptions section.
- **Scope Absorption**: Vision says "support X." You write capabilities for X, Y, and Z.
- **Premature Solutioning**: Capability descriptions that name an API, framework, data format,
  or storage engine. State the behavioral need only.
- **Generic Persona**: "As a User…" — a persona at the epic level must orient Story Writers.
  If "User" could be anyone, it tells Story Writers nothing about constraints or expectations.
- **False Dependencies**: CAP-B depends on CAP-A only because they share a concept, not because
  CAP-B's implementation requires CAP-A's output. Over-constraining the plan delays delivery.
- **Opaque Capability**: The Story Writer needs to re-read the vision to understand what to build.
  Every capability must be self-contained enough that it can be handed to a Story Writer as a
  complete task brief.
- **Buried Interpretation**: A consequential judgment call (scope boundary, signal interpretation,
  ownership split) is embedded in capability prose instead of surfaced in the Interpretation
  Decisions ledger. The intent owner will miss it during review.

# ID Conventions

All IDs use structured prefixes:

| Prefix | Meaning | Example |
|--------|---------|---------|
| EP-NNN | Epic | EP-003 |
| CAP-NNN | Capability (scoped to epic) | CAP-001 |
| C-NNN | External component | C-002 |
| I-NNN-NNN | Interface (composite: I-{component}-{interface}) | I-002-001 |
| NFR-000-N | Non-functional requirement | NFR-000-1 |
| ASM-NNN-N | Assumption (000 = general, NNN = capability) | ASM-000-1 |
| OQ-NNN-N | Open question | OQ-001-1 |

Do not invent new prefixes. If something doesn't fit these categories, it likely belongs in Context
or is a sign the epic needs rethinking.

# Altitude Discipline

The hardest judgment in epic-writing is staying at the right altitude. Use this table as a check:

| Too low (story territory) | Right level (epic territory) | Too high (vision territory) |
|---------------------------|------------------------------|-----------------------------|
| AC: "Given a valid token, when…" | "User can authenticate without re-entering credentials" | "Improve onboarding" |
| "The endpoint returns 200" | "Operator can monitor request health in real time" | "Better observability" |
| "Use PostgreSQL for storage" | "Tasks persist across sessions" | "Reliable data" |
| "Validate email format on blur" | "User receives actionable feedback on invalid input" | "Good UX" |

If a capability description reads like a story, it's too low. Rewrite it as a user-facing behavior.
If it reads like a goal in an OKR, it's too high. Narrow it to something a Story Writer can bound.

# Integration

| Skill | Relationship |
|-------|-------------|
| **user-story-writing** | Direct downstream consumer. Each capability section is a Story Writer task brief. The Story Writer reads the Personas, General Information, and the capability section — nothing else. Unresolved epic assumptions and Open Questions block story-writing from starting. |
| **detailed-spec-writing** | Complementary. Epics capture product intent; PRDs capture system requirements. An approved epic can seed a PRD for a capability. |
| **spec-review** | Downstream. Validates epics against completeness/consistency/testability. |

# Mode-Specific Behavior

**Pairing mode:** All interactive prompts apply. Present the draft epic for human review before
writing the file. Human may redirect scope, resolve Open Questions inline, or confirm assumptions.

**§BRAND_NAME_TITLE§ mode:** Epic Writer operates autonomously within task scope. When a planner task
decomposes a vision document, the planner applies this skill once per epic boundary it identifies.
Producing multiple epic artifacts from a single vision source is normal pipeline behavior — the
"flag for splitting" guidance in Size applies to individual epics that are too broad, not to the
planner's decomposition of a vision into multiple epics.

| Pairing Prompt | §BRAND_NAME_TITLE§ Behavior |
|----------------|---------------|
| "Source material spans multiple capability areas — split?" | Applies to a single epic that's too broad. When decomposing a vision into multiple epics, multiple capability areas across epics is expected — flag only if a single epic spans unrelated areas |
| "Epic would produce >8 stories — where to split?" | Find the natural persona or subsystem seam; flag split rationale in Context |
| "Epic would produce <2 stories — merge or promote?" | Flag to Orchestrator via BLOCKED with merge/promote recommendation |
| "This assumption is LOW confidence — resolve?" | Surface in Assumptions section; blocks story-writing until resolved by human |
| "Adjacent epic may conflict — check?" | Read adjacent document, declare in References, note in Context |
| "Cannot identify a clear persona for this epic" | Surface as Open Question — an epic without a persona cannot produce well-bounded stories |
| "Cannot write falsifiable completion criteria" | Surface as Open Question — scope is not yet understood |
