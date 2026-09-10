# User Stories: <Short Descriptive Title>

Status: draft/review/approved/superseded

---

# Part 1 — Story Review

*For the intent owner. Verify the user-visible behavior before coding starts.*

## Promise

**Before these stories:** <what the persona cannot do or cannot trust today>

**After these stories:** <what the persona can do, decide, recover from, or rely on>

## Behavior Map

| Story | User-visible change | Source intent captured | Main exclusion |
|-------|---------------------|-----------------------|----------------|
| ST-001 — <name> | <plain-language outcome> | <source section / capability point> | <not covered here> |
| ... | | | |

## Example Walkthrough

A representative user would:
1. <do or observe X>
2. <receive or see Y>
3. <end in state Z>

Narrative, not Given/When/Then. Exercises the main happy path so the intent owner can picture the
experience being specified.

## Interpretation Decisions

Non-obvious inferences made while decomposing the capability into stories.
Omit when all interpretation was resolved at the epic level and no new judgment calls arose.

| Source signal | Story interpretation | Confidence | Verify? |
|---------------|---------------------|------------|---------|
| <source wording or capability claim> | <how the story interprets it> | HIGH / MEDIUM / LOW | Yes / No |
| ... | | | |

## Review Questions

Targeted questions only the intent owner can answer. Not technical unknowns — those belong in
Open Questions within the coder contract.

- [ ] <question about behavior, scope boundary, or AC judgment call>
- [ ] ...

---

# Part 2 — Coder Contract

*For the Coder. Implementation-ready acceptance criteria and context.*

## Source References
Source revision: "<40 lowercase hexadecimal Git object ID>"

### Direct References
- "<reference-id>": "<repo-relative-clean-markdown-path>#<exact-heading-text>"
- "<reference-id>": "<repo-relative-clean-markdown-path>#<exact-heading-text>" @ "<40 lowercase hexadecimal Git object ID>"

### Obligation Coverage
- "<obligation-id>" -> "<reference-id>"
- "<obligation-id>" -> "<reference-id>", "<reference-id>"

## Goal
One sentence. What this set of stories achieves when implemented. Measurable.

## Parent Epic
<path to epic document — capability CAP-NNN, or "none" if written without an epic>

## Context
Story-local orientation and interpretation decisions only. Cite inherited product rationale through
Source References instead of copying it.

## Personas
- **<inherited persona ID>** -> <direct-reference ID>: <story-local applicability or exclusion>
- ...

## General Information

Applies to: the entire scope (all stories).

### Non-Functional Requirements
- <inherited NFR ID> -> <direct-reference ID>
- <new story-local NFR ID>: <requirement and why this story owns it>
- ...

### Related External Components
Only story-local applicability or exclusions; cite inherited definitions:
- Component C-002 -> <direct-reference ID>: <story-local role or exclusion>
- ...

### Interfaces *(include only when this document defines component boundaries)*

Only story-local use or newly owned boundary decisions; cite inherited definitions:
- I-002-001 -> <direct-reference ID>: <story-local use or new boundary decision>
- ...

### Out of Scope
Explicit list of what this document does NOT cover. Adjacent concerns the Coder must not drift into.

### Assumptions
Items where the source material was ambiguous and you made a judgment call. Each assumption is:
- **ASM-000-1**: <what you assumed> — *Why*: <reasoning> — Confidence: HIGH | MEDIUM | LOW
- ...

LOW confidence assumptions are blocking: the human must resolve them before stories move to coding.

### Open Questions
Questions you cannot resolve by assumption. These MUST be answered by a human before stories are coded.
- **OQ-000-1**: <question> — *Impact if unresolved*: <what breaks or stays ambiguous>
- ...

---

## Story ST-001 - <story name>

### Inherited Obligations
- <obligation ID> -> <direct-reference ID>
- ...

### User Story
**As a** <persona>, **I want to** <action>, **so that** <outcome/value>.

### Acceptance Criteria
- AC-001-1: Given <context>, when <action>, then <outcome>
- AC-001-1b: edge case of AC-001-1
- AC-001-2: ...

### Depends on:
Run time coupling:
- Interface I-002-001 - <interface name>
- ...

### Out of Scope
Explicit list of what this story does NOT cover. Adjacent concerns the Coder must not drift into.

### Assumptions
Items where the source material was ambiguous and you made a judgment call. Each assumption is:
- **ASM-001-1**: <what you assumed> — *Why*: <reasoning> — Confidence: HIGH | MEDIUM | LOW

LOW confidence assumptions are blocking: the human must resolve them before this story moves to coding.

### Open Questions
Questions you cannot resolve by assumption. These MUST be answered by a human before this story is coded.
- **OQ-001-1**: <question> — *Impact if unresolved*: <what breaks or stays ambiguous>

---

## Story ST-002 - <story name>
...

### Depends on:
Implementation ordering:
- Story ST-001 - <story name>
...

---

## Correction Delta *(only when correcting an approved artifact)*

- **Corrected anchors:** <artifact and exact heading references>
- **Replacement decisions:** <new story-local decision and rationale>
- **Unchanged contracts:** <direct-reference IDs>
- **Effective superseding reference:** <reference ID and revision>
