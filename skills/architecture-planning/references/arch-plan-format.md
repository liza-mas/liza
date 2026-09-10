# Architecture Plan: <Short Title>

Status: draft/review/approved

## Source References
Source revision: "<40 lowercase hexadecimal Git object ID>"

### Direct References
- "<reference-id>": "<repo-relative-clean-markdown-path>#<exact-heading-text>"
- "<reference-id>": "<repo-relative-clean-markdown-path>#<exact-heading-text>" @ "<40 lowercase hexadecimal Git object ID>"

### Obligation Coverage
- "<obligation-id>" -> "<reference-id>"
- "<obligation-id>" -> "<reference-id>", "<reference-id>"

## Goal

One sentence. The structural vision for implementing the goal spec.

## Context

Architecture-local context and the repository evidence used for new structural decisions. Cite
inherited product behavior and parent decisions through Source References instead of copying them.

### Constraints
New scope-local constraints plus IDs and direct-reference IDs for inherited constraints.

### Assumptions
- **ASM-001**: <architectural assumption> — *Why*: <reasoning> — Confidence: HIGH | MEDIUM | LOW

### Open Questions
- **OQ-001**: <structural question> — *Impact*: <what stays ambiguous for code-planners>

---

## Components

### <Name> (`<path/>`)

**Responsibility:** One sentence — what this component owns.

**Boundaries:**
- Exposes: <what other components can access>
- Depends on: <what this component requires>

**Key decisions:**
- <decision>: <rationale>

---

## Interfaces

### <Component A> → <Component B>

**Contract:** What crosses the boundary, in what form.
**Direction:** Who calls whom; data flow direction.
**Invariants:** What must always be true.

---

## Data Flow

```
Input → Component A → [interface] → Component B → Output
```

Annotate transformations at each stage.

---

## Cross-Cutting Concerns

| Concern | Approach |
|---------|----------|
| Error handling | <how errors propagate across components> |
| Observability | <logging, metrics, tracing> |
| Configuration | <what's configurable, where it lives> |
| Testing | <integration boundaries, what's mockable> |

---

## Decomposition

Each scope becomes a code-planning child task.

### Scope 1: <title>

**Component(s):** <which components>
**Boundary:** <in scope / out of scope>
**Done when:** <falsifiable criterion>
**Depends on:** <scope numbers, if any>

### Scope 2: <title>
...

### Spec Coverage

| Obligation ID | Direct reference ID | Scope |
|---------------|---------------------|-------|
| <FR/feature ID> | <reference ID> | Scope N |
| ... | ... | ... |

Every assigned obligation must map to an anchor whose span contains it and at least one scope.
Unmapped obligations are gaps.

## Output

For each downstream `output[]` entry, record its scope ID, concise intent, boundary, dependencies,
validation observation, and anchored architecture reference. If `desc` exceeds 160 characters or
`done_when`/`scope` exceeds 400 characters, explain why the longer value is executable at dispatch
and has no authoritative home behind a reference.

## Correction Delta *(only when correcting an approved artifact)*

- **Corrected anchors:** <artifact and exact heading references>
- **Replacement decisions:** <new architecture-local decision and rationale>
- **Unchanged contracts:** <direct-reference IDs>
- **Effective superseding reference:** <reference ID and revision>
