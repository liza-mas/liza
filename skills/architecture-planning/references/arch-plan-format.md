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

Each scope becomes a specialized-architecture or code-planning child, according to the
transition. Every Scope declares its complete read set on one line outside code fences:
`**Direct references:** ["product-id", "shared-architecture-id"]`. IDs name entries in
`Source References`; `[]` explicitly selects none. Include every inherited product obligation
and shared decision, constraint, interface, or hold needed by this scope. Shared sections may
use same-file direct references, pinned to a Git revision where that content already exists;
never infer this read set from prose or the coverage table.

### Scope 1: <title>

**Direct references:** ["<product-reference-id>", "<shared-reference-id>"]
**Component(s):** <which components>
**Boundary:** <in scope / out of scope>
**Done when:** <falsifiable criterion>
**Depends on:** <scope numbers, if any>

#### CONTRACT

Scope-local interface prose: provider answers to the early Provider Ask Inventory,
operations, fields, statuses, direction and invariants. Cite inherited authority.
This exact subsection is the bounded writable region for future reviewed contract
corrections; do not place allocations, acceptance declarations, proof mappings or
executable output metadata inside it. Those stay outside this subsection and frozen.

### Scope 2: <title>
**Direct references:** ["<reference-id>"]
... Repeat the exact nested `#### CONTRACT` subsection within each Scope.

### Spec Coverage

| Obligation ID | Direct reference ID | Scope |
|---------------|---------------------|-------|
| <FR/feature ID> | <reference ID> | Scope N |
| ... | ... | ... |

Every assigned obligation must map to an anchor whose span contains it and at least one scope.
Unmapped obligations are gaps.

## Output

For each downstream `output[]` entry, record its scope ID, concise intent, boundary, dependencies,
validation observation, and `arch_ref` as `<doc>.md#<exact Scope heading>`. Both master and
specialized architects must assign an existing unique Scope heading to every output; bare paths
are refused on new submissions. The child sees only that Scope and its selected direct references;
other sections and inherited carriers remain pinned navigation pointers. If `desc` exceeds 160 characters or
`done_when`/`scope` exceeds 400 characters, explain why the longer value is executable at dispatch
and has no authoritative home behind a reference.

## Correction Delta *(only when correcting an approved artifact)*

- **Corrected anchors:** <artifact and exact heading references>
- **Replacement decisions:** <new architecture-local decision and rationale>
- **Unchanged contracts:** <direct-reference IDs>
- **Effective superseding reference:** <reference ID and revision>
