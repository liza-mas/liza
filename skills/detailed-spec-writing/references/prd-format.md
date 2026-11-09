# PRD: <Short Descriptive Title>

Status: draft/review/approved/superseded

## Goal
One sentence. What this specification achieves when implemented. Measurable.

## Context
Why this matters. How it fits in the broader system. Dependencies on other specs or existing components. Keep it brief — the Coder needs orientation, not a lecture.

## General information

Applies to: the entire scope (all requirements).

### References
- <ref-type>: <path or link> — <section/line range if applicable>
- ...

### Non-Functional Requirements
- NFR-000-1: <requirement> — architectural constraints, performance, security, observability, technology mandates, compatibility requirements, etc.

### Related External Components
Summary of all the external components referenced by this PRD:
- Component C-002 - <component name>

### Interfaces *(include only when this spec defines component boundaries)*

Summary of all the external interfaces referenced by this PRD:
- I-002-001 - <interface name> (Interface 001 of Component C-002): <protocol/contract description>
- ...

### Out of Scope
Explicit list of what this spec does NOT cover. Adjacent concerns the Coder must not drift into.

### Assumptions
Items where the source material was ambiguous and you made a judgment call. Each assumption is:
- **ASM-000-1**: <what you assumed> — *Why*: <reasoning> — ⚠️ Confidence: HIGH | MEDIUM | LOW
- ...

LOW confidence assumptions are blocking: the human must resolve them before the parts of this spec they affect move to coding.

### Open Questions
Questions you cannot resolve by assumption. Each MUST be answered by a human before the parts of this spec its impact names are coded.
- **OQ-000-1**: <question> — *Impact if unresolved*: <what breaks or stays ambiguous>
- ...

---

## Feature FT-001 - <feature name>

### References
- <ref-type>: <path or link> — <section/line range if applicable>
- ...

### Functional Requirements
- FR-001-1: <requirement> — each requirement is atomic, testable, unambiguous
- FR-001-1b: edge case of FR-001-1
- FR-001-2: ...

### Non-Functional Requirements
- NFR-001-1: <requirement> — architectural constraints, performance, security, observability, technology mandates, compatibility requirements, etc.
- NFR-001-2: ...

### Acceptance Criteria
- AC-001-1: Given <context>, when <action>, then <outcome>
- AC-001-2: ...

### Depends on:
Run time coupling:
- Interface I-002-001 - <interface name>
- ...

### Interfaces *(include only when this spec defines component boundaries)*
- Interface with <component>: <protocol/contract description>
- ...

### Out of Scope
Explicit list of what this spec does NOT cover. Adjacent concerns the Coder must not drift into.

### Assumptions
Items where the source material was ambiguous and you made a judgment call. Each assumption is:
- **ASM-001-1**: <what you assumed> — *Why*: <reasoning> — ⚠️ Confidence: HIGH | MEDIUM | LOW

LOW confidence assumptions are blocking: the human must resolve them before this spec moves to coding.

### Open Questions
Questions you cannot resolve by assumption. These MUST be answered by a human before this spec is coded.
- **OQ-001-1**: <question> — *Impact if unresolved*: <what breaks or stays ambiguous>

---

## Feature FT-002 - <feature name>
...

### Depends on:
Implementation ordering:
- Feature FT-001 - <feature name>
...
