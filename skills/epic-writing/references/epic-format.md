# Epic EP-NNN: <Short Descriptive Title>

Status: draft/review/approved/superseded

---

# Part 1 — Intent Review

*For the intent owner. Verify that this epic captures what you meant before agents decompose it.*

## Promise

**Before this epic:** <what the product cannot do today, in plain language>

**After this epic:** <what becomes possible, in plain language>

## Capability Map

| Capability | Human-facing outcome | Source intent captured | Main exclusion |
|------------|---------------------|-----------------------|----------------|
| CAP-001 — <name> | <what the user gains> | <source section/problem ID> | <what this deliberately leaves out> |
| ... | | | |

## Interpretation Decisions

Non-obvious inferences made while translating source material into this epic.
Omit when the epic is a straightforward decomposition with no judgment calls.

| Source signal | Epic interpretation | Confidence | Verify? |
|---------------|---------------------|------------|---------|
| <quote or paraphrase from source> | <how this epic interprets it> | HIGH / MEDIUM / LOW | Yes / No |
| ... | | | |

## Review Questions

Targeted questions only the intent owner can answer. Not technical unknowns — those belong in
Open Questions within the execution contract.

- [ ] <question about intent, scope boundary, or ownership>
- [ ] ...

---

# Part 2 — Execution Contract

*For Story Writers and the Orchestrator. Operational detail for decomposition and implementation.*

## Source References
Source revision: "<40 lowercase hexadecimal Git object ID>"

### Direct References
- "<reference-id>": "<repo-relative-clean-markdown-path>#<exact-heading-text>"
- "<reference-id>": "<repo-relative-clean-markdown-path>#<exact-heading-text>" @ "<40 lowercase hexadecimal Git object ID>"

### Obligation Coverage
- "<obligation-id>" -> "<reference-id>"
- "<obligation-id>" -> "<reference-id>", "<reference-id>"

## Goal
One sentence. What this epic achieves when fully delivered. Measurable at the product level.

## Context
Why this matters now. How it fits within the product vision. What problem it solves for which persona.
Keep it brief — the Orchestrator needs orientation, not a lecture.

## Personas
- **<inherited persona ID>**: <direct-reference ID; add only epic-local applicability or exclusion>
- ...

## General Information

Applies to: the entire epic scope.

### Epic Dependencies
- <dependency ID and direct-reference ID; state only the epic-local ordering reason>
- ...

### Completion Criteria
The falsifiable condition that closes this epic. When all story ACs pass, this must be satisfied.
Observable outcome, not a direction.

<one or two sentences stating the condition — e.g., "Users can create, edit, and delete tasks
without data loss across sessions. Operators can monitor task volume and error rates in real time."
If you cannot write this yet, surface it as OQ-000-N.>

### Non-Functional Requirements
- <inherited NFR obligation ID> -> <direct-reference ID>
- <new epic-local NFR ID>: <requirement and why this stage owns it>
- ...

### Related External Components
Only epic-local component applicability or exclusions; cite inherited definitions:
- Component C-NNN -> <direct-reference ID>: <epic-local role or exclusion>
- ...

### Interfaces *(include only when this epic defines component boundaries)*
Only epic-local interface use or newly owned boundary decisions; cite inherited definitions:
- I-NNN-NNN -> <direct-reference ID>: <epic-local use or new boundary decision>
- ...

### Out of Scope
Explicit list of what this epic does NOT cover. Adjacent capabilities the Story Writer must not absorb.

### Assumptions
Items where the vision material was ambiguous and you made a judgment call.
- **ASM-000-1**: <what you assumed> — *Why*: <reasoning> — Confidence: HIGH | MEDIUM | LOW

LOW confidence assumptions are blocking: the human must resolve them before story-writing begins.

### Open Questions
Questions that cannot be resolved by assumption. Must be answered before Story Writers begin work.
- **OQ-000-1**: <question> — *Impact if unresolved*: <what remains unbounded or contradictory>

---

## Capability CAP-001 - <capability name>

One sentence: what this capability delivers and for whom.

### Inherited Obligations
- <obligation ID> -> <direct-reference ID>
- ...

### Description
Two to four sentences max. Describe the user-facing behavior — what the persona can do that they
could not do before. Do not describe implementation. Do not write stories.

### Story Documents
The set of story documents this capability decomposes into. One story document per cohesive unit of
work a Story Writer can own.

| Story Doc | Title | Priority | Notes |
|-----------|-------|----------|-------|
| <path — assigned by Orchestrator or agreed with human> | <short title> | P1 / P2 / P3 | <dependency note or blank> |
| ... | | | |

### Depends on:
- Capability CAP-NNN - <name>: <why — what from that capability is required here>
- ...

### Out of Scope
What this capability explicitly excludes. Prevents Story Writer scope absorption.

### Assumptions
- **ASM-001-1**: <what you assumed> — *Why*: <reasoning> — Confidence: HIGH | MEDIUM | LOW

### Open Questions
- **OQ-001-1**: <question> — *Impact if unresolved*: <what the Story Writer cannot bound>

---

## Capability CAP-002 - <capability name>
...

### Depends on:
- Capability CAP-001 - <name>: <why>
- ...

## Output

For each downstream `output[]` entry, record its capability ID, short intent, scope,
dependencies, validation observation, and anchored artifact reference. If `desc` exceeds 160
characters or `done_when`/`scope` exceeds 400 characters, explain why the longer value is
executable at dispatch and has no authoritative home behind a reference.

## Correction Delta *(only when correcting an approved artifact)*

- **Corrected anchors:** <artifact and exact heading references>
- **Replacement decisions:** <new local decision and rationale>
- **Unchanged contracts:** <direct-reference IDs>
- **Effective superseding reference:** <reference ID and revision>
