---
name: user-story-writing
description: Transform requirements into user stories for coding tasks
---

# Objective

Transform high-level, fuzzy requirements into precise, well-structured user stories that an Orchestrator can decompose into coding tasks.

**Stories have two audiences: first the intent owner, then the Coder.** The Story Review (Part 1)
lets the intent owner verify the lived user behavior in minutes. The Coder Contract (Part 2) gives
the Coder implementation-ready acceptance criteria. Neither section is optional.

The output is a user story artifact — a markdown document, git-tracked, treated with the same rigor as code.
One story document per task; one cohesive capability per document. A capability is the document's scope.
Individual user stories are its constituent parts — each story maps to one Coder-sized unit of work.

# Trigger

Use this skill when:
- Orchestrator creates a story-writing task (§BRAND_NAME_TITLE§ mode)
- User asks to write user stories for a feature, capability, or requirement (Pairing mode)
- The team's workflow uses user stories rather than formal PRD requirements

# Inputs

Your task provides:
- An **output file path** assigned by the Orchestrator on the blackboard
- **References** to one or more sections of source material (epic, vision doc, prior specs,
  existing code)

When the source is a parent epic, read its Personas, General Information, and the assigned
capability section — nothing else. The epic's personas, NFRs, assumptions, and open questions
apply to all stories in this document. Inherit them; do not contradict them.

**Scope discipline (two-tier):**
- **Upfront:** Read task references and scan existing stories in the same domain for consistency.
  Same domain = same parent directory, or stories referenced by the same source section.
  This minimal consistency check is always permitted.
- **Reviewer-driven:** Broader expansion (new source material, adjacent codepaths, external docs) only when the
  Story Reviewer's feedback identifies specific additional material to consult.

In both cases, declare what you read and why in the References section.

# Output Format

Produce a markdown file at the path specified by the task.
Use the [User Story format](references/user-story-format.md) template.

# Protocol

## 1. Parse

Read the source material. Identify what is said, what is implied, and what is missing. Do not start writing stories
until you can distinguish the three.

Identify the personas. If the source material does not name them, infer the minimum set from the actions described.
A story without a clear persona is a sign that the requirement is not yet understood from the user's perspective.

A useful persona drives design decisions. "User" tells the Coder nothing about context, expectations, or constraints.
Include environment and skill level when they affect how the feature should behave. Compare:
"User: a person managing their todo list" vs. "Terminal User: a developer or power user who manages personal tasks
from the command line and expects standard CLI conventions (flags, non-zero exit codes, concise output)." The second
persona tells the Coder that `--help` should exist and errors should go to stderr.

If the source material contains multiple independent capabilities, flag this to the Orchestrator — it may need to split the task.
Do not silently produce a mega-document.

## 2. Write the Story Review

Write Part 1 before Part 2. The Story Review is a forcing function: if you cannot write a clear
walkthrough of the experience, the stories are not yet understood from the user's perspective.

**Promise (Before/After):** State what the persona cannot do or trust today and what becomes
possible after these stories ship. Plain language, no IDs. A non-technical stakeholder should be
able to read this and say "yes, that's the experience I want."

**Behavior Map:** One row per story. The user-visible change column restates the story as a
plain-language outcome — not the AC list, but its human consequence. The source intent column
traces back to the capability or source material. The main exclusion column names what this story
deliberately leaves out.

**Example Walkthrough:** Write a short narrative (3–6 steps) of a representative user going through
the main happy path. This is not Given/When/Then — it is a plain-language scenario the intent owner
can picture. The walkthrough should exercise the most important story, or the flow that connects
multiple stories. If the walkthrough does not match the intent owner's mental model, the ACs need
revision regardless of how technically complete they are.

**Interpretation Decisions:** Surface judgment calls made during decomposition. When the parent
epic's Intent Review already resolved the interpretation, do not repeat it here — only surface
new story-level decisions (AC boundary choices, edge case handling, decomposition trade-offs).
Omit this section entirely when no new judgment calls arose.

**Review Questions:** Write 3–5 targeted questions the intent owner can answer with a short
response. Focus on behavior and experience: "Is the user expected to see X before Y?", "Should
this edge case be handled now or deferred?", "Is this the right story boundary between ST-001
and ST-002?"

## 3. Write the Coder Contract

Every user story follows the canonical form:

> **As a** <persona>, **I want to** <action>, **so that** <outcome/value>.

**Before writing each assumption, check:**
- System quality nobody would dispute? → NFR, not assumption.
- Specifies a data type, format, or representation? → Design territory. State the behavioral need only.
- Provides an answer that an Open Question also asks? → Keep the OQ, drop the assumption.
- Another aspect of the same source gap is already an OQ? → The assumption needs a clear
  conventional default to justify different treatment; otherwise, prefer OQ for consistency.

Apply SMARC to every user story — Specific, Measurable, Achievable, Relevant, and:
- **C**ontext-bound — implementable within the context budget of a single agent task; if a story would force the
  Coder to load excessive context (many files, large dependency graphs, deep cross-component knowledge), it must be
  split into smaller stories

If you cannot make a story SMARC, it becomes an Open Question — never a vague story.

**References:** Each story must trace to its source material. A story with no traceability is Scope Absorption.

**Declared dependencies:** Verify they are true ordering constraints. ST-X depends on ST-Y only if ST-X cannot be
implemented or tested without ST-Y existing. Shared concepts do not imply implementation ordering.

**Assumptions are first-class outputs** that resolve behavioral ambiguity, not technical. Well-identified
LOW-confidence assumptions are more valuable than papered-over gaps. If an assumption names a data type, format,
encoding, storage engine, library, or protocol — state the behavioral need only. If it describes a quality nobody
would dispute (persistence, latency, error reporting) — it's an NFR.

**Acceptance criteria are contracts.** They define done. A Coder who satisfies all ACs has completed the story.
If your ACs don't fully define done, your story is incomplete.

**Acceptance criteria are user-observable.** Every AC must describe something the persona can see, do, or experience.
Internal system behavior that has no user-visible effect is not an AC — it may be a technical task or an NFR, but it
is not a story-level acceptance criterion.

**Edge cases are AC variants.** Use the `AC-NNN-Nb` suffix (e.g., AC-001-1b) for edge cases of a parent AC.
Edge cases cover error states, boundary conditions, and unexpected input — not just the happy path.
Each edge case states expected behavior explicitly.

## 4. Self-Review

Before submitting for review, verify:

**Story Review (Part 1):**
- [ ] Promise is readable without implementation context
- [ ] Behavior Map covers every story — no row missing
- [ ] Example Walkthrough exercises the main happy path as a narrative the intent owner can picture
- [ ] Interpretation Decisions surface story-level judgment calls not already resolved by the parent epic
- [ ] Review Questions are specific and answerable — focused on behavior, experience, and scope boundaries

**Coder Contract (Part 2):**
- [ ] Every story traces to a source reference (not invented, as granular as possible)
- [ ] Every story has a clear persona, action, and value statement
- [ ] Persona includes environment or skill-level detail when it affects feature behavior
- [ ] Every AC maps to its parent story and describes user-observable behavior
- [ ] No LOW-confidence assumptions leaked into stories (they belong in Assumptions)
- [ ] Out of Scope is explicit — not just what's excluded, but adjacent concerns the Coder might drift into
- [ ] A Coder reading only this document and the referenced files can implement it (no hidden context dependencies)
- [ ] Re-read the source material — did you miss anything? Did you add anything not in scope?
- [ ] No assumption prescribes implementation (data types, formats, libraries) or states something nobody would dispute (reclassify as NFR)
- [ ] No assumption overlaps with an OQ — if overlap exists, keep the OQ and drop the assumption
- [ ] Every AC traces to stated or implied source behavior — unmentioned scenarios belong in Assumptions or Open Questions, not ACs
- [ ] No OQ contradicts or is resolved by an existing story — if so, either the story is premature or the OQ is unnecessary
- [ ] Error states and boundary conditions are covered — not just the happy path
- [ ] Dependencies are true ordering constraints, not merely shared concepts
- [ ] Sibling ambiguities from the same source gap treated consistently — mixed OQ/assumption requires a clear conventional default

If self-review reveals issues, fix before submitting.

# Constraints

- **DO** write Part 1 for the intent owner (plain language, verification surface) and Part 2 for
  the Coder (precise, technical, implementation-ready).
- **DO** check existing stories in the same domain for consistency — contradictions between story documents are costly to discover at implementation time.
- **DO** surface inconsistencies as an Open Question if references contradict each other.
- **DO NOT** write code — stories only.
- **DO NOT** modify existing story documents unless the task explicitly scopes an update.

# Anti-Patterns

- **Persona Laundering**: "As a user, I want the system to use JSON storage" — persona is a fig leaf on a technical task.
- **Giant Story**: Can't be implemented in a single focused session? Split it.
- **Wishful Story**: No testable ACs. "As a user, I want the app to be fast" is not a story.
- **Hidden Coupling**: References other components' behavior without declaring the dependency.
- **Assumption Burial**: Assumptions embedded in stories instead of surfaced in the Assumptions section.
- **Scope Absorption**: Source says "support X." You write stories for X, Y, and Z. Stick to what the task scopes.
- **Premature Solutioning**: ACs or assumptions that prescribe implementation. "Given the JSON file exists" is solutioning; "Given tasks were previously saved" is behavioral. "Priority is a numeric value where lower = higher" is solutioning; "Priorities have a defined sort order" is behavioral.
- **Generic Persona**: "As a User, I want to..." where "User" could be any noun. A persona should tell the Coder about context, skill level, or environment that affects behavior.
- **Valueless Story**: Hand-waving the "so that" clause. If you can't articulate the value, the story may not be needed.
- **AC Wall**: Acceptance criteria are technically complete but there is no plain-language
  walkthrough, making it hard for the intent owner to verify the experience being specified.

# ID Conventions

All IDs in the user story format use structured prefixes:

| Prefix | Meaning | Example |
|--------|---------|---------|
| C-NNN | External component | C-002 |
| I-NNN-NNN | Interface (composite: I-{component}-{interface}) | I-002-001 |
| ST-NNN | User story | ST-001 |
| AC-NNN-N | Acceptance criterion (scoped to story) | AC-001-1 |
| AC-NNN-Nb | Edge case acceptance criterion | AC-001-1b |
| NFR-NNN-N | Non-functional requirement | NFR-000-1 |
| ASM-NNN-N | Assumption (000 = general, NNN = story) | ASM-000-1 |
| OQ-NNN-N | Open question | OQ-001-1 |

Do not invent new prefixes. If something doesn't fit these categories, it likely belongs in Context or is a sign the story needs rethinking.

# Integration

| Skill | Relationship |
|-------|-------------|
| **epic-writing** | Upstream provider. When the source is an epic, each capability section is a Story Writer task brief. The Story Writer reads the epic's Personas, General Information, and the assigned capability section — nothing else. |
| **detailed-spec-writing** | Complementary. Stories capture user intent; PRDs capture system requirements. Stories can feed into PRDs for implementation detail. |
| **spec-review** | Downstream. Validates stories against completeness/consistency/testability checklist. |
| **spec-backfill** | Complementary. Backfill: specs from code (archaeology). This skill: stories from requirements (forward-looking). |
| **code-review** | ACs are the contract bridge — Coders implement against them, reviewers validate against them. |

# Mode-Specific Behavior

**Pairing mode:** All interactive prompts apply. Present the draft stories for human review before writing the file. Human may redirect scope, resolve Open Questions inline, or confirm assumptions.

**§BRAND_NAME_TITLE§ mode:** Story Writer operates autonomously within task scope.

| Pairing Prompt | §BRAND_NAME_TITLE§ Behavior |
|----------------|---------------|
| "Source material contains multiple capabilities — split?" | Flag to Orchestrator via BLOCKED with split recommendation |
| "This assumption is LOW confidence — resolve?" | Surface in Assumptions section; Human resolves at the end of the sprint, before coding starts in the next sprint. |
| "Adjacent story doc may conflict — check?" | Read adjacent document, declare in References, note in Context |
| "Cannot identify a clear persona for this requirement" | Surface as Open Question — a requirement without a persona may not be a user story |
