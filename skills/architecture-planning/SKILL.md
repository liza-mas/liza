---
name: architecture-planning
description: Define component boundaries, interfaces, and structural decisions for a change
---

# Objective

Consolidate approved upstream deliverables into a cohesive architectural plan that downstream
code-planners can implement independently.

Output: a git-tracked architecture document (`specs/arch-plan/<goal-slug>/<timestamp>-<task-id>.md`) plus
structured `output[]` entries, one per code-planning scope. Each output entry becomes a
code-planning child task — the code-planner reads the architecture document and its scope entry,
the goal spec, and referenced material. Write accordingly: the arch doc should be self-contained
for structural decisions, leaving behavioral detail to the spec.

The architect bridges *what* (spec) and *how* (code plan). Spec says what to build; the architect
says where each piece goes and how pieces connect. Code-planners turn each scope into
implementation steps.

# Trigger

- Orchestrator creates an architecture task (§BRAND_NAME_TITLE§ mode)
- User asks to plan architecture for a feature or change (Pairing mode)
- Implementation planning requires structural decisions before code-planning

# Inputs

Your task provides:
- A **goal spec** (`spec_ref`) — what must be built
- **Parent task context** — upstream deliverables being consolidated (many-to-one fan-in)
- An **output mechanism** — `set-task-output` for each code-planning scope

**Scope discipline:**
- **Upfront:** Read goal spec + parent task deliverables + explore relevant codebase areas
- **On demand:** Broader exploration only when structural questions require it
- Declare what you read and why in References

# Output Format

[Architecture Plan format](references/arch-plan-format.md) at the task-specified path.

# Protocol

## 1. Consolidate

Read and synthesize all inputs before making any structural decisions.

1. Read the goal spec — the authoritative source for *what* must be built
2. Read all parent task deliverables (descriptions, outputs, referenced specs/plans)
3. Synthesize scope: what is the full extent of what must be built?
4. Identify:
   - **Overlaps** — parent deliverables that touch the same area
   - **Gaps** — scope in the goal spec not covered by any parent deliverable
   - **Conflicts** — parent deliverables that make incompatible assumptions

If conflicts are unresolvable from the spec, surface as Open Questions.

## 2. Survey

Map the existing architecture before proposing changes.

1. Explore the codebase: existing components, patterns, boundaries
2. Identify extension points, constraints, and conventions to follow
3. Note existing components affected by this change

Do not over-explore. Enough understanding for sound structural decisions, not a full architecture
review. For deeper analysis of a specific area, use `software-architecture-review` in
Code Review Supplement mode.

## 3. Design

Define structural decisions. Each decision must have a rationale.

1. **Component boundaries** — new or modified components, what each is responsible for
2. **Interface contracts** — how components communicate; what data crosses boundaries, in what form
3. **Data flow** — trace the primary data paths end-to-end
4. **Cross-cutting concerns** — error handling, observability, configuration, security
5. **Structural decisions** — explicit choices with rationale

**Decision altitude:**

| Too low (code-planner territory) | Right level | Too high (spec territory) |
|----------------------------------|-------------|---------------------------|
| "Function X takes param Y: int" | "Component A exposes a query interface that B calls" | "The system should be reliable" |
| "Use a map[string]Task" | "Tasks are indexed by ID for O(1) lookup" | "Store tasks" |
| "Error wraps with fmt.Errorf" | "Errors propagate up with context; no silent swallowing" | "Handle errors" |

If a decision reads like code, it's too low. If it reads like a requirement, it's too high.

## 4. Decompose

Map the architecture to code-planning scopes. Each scope becomes one code-planner task.
Scopes correspond to domains or sub-domains — not to implementation steps. Sequencing
work within a scope is code-planner territory.

1. **Identify scopes** — natural boundaries from the design
2. **Scope independence** — each code-planner should work without waiting for another
3. **Scope completeness** — each scope is implementable and testable on its own
4. **Ordering constraints** — `depends_on` only when scope B cannot be implemented without A's output
5. **Shared-file audit** — overlapping file access needs `depends_on` or explicit interface contracts

**Decomposition heuristics:**
- A scope requiring >5-8 files to understand is probably too broad
- A scope producing <2-3 meaningful changes is too narrow — merge it with a sibling
- If you can't write a falsifiable `done_when`, the scope isn't well-bounded
- Prefer scopes aligned with existing package/module boundaries
- If two scopes share one domain and differ only in implementation order, they're one scope
- A domain must have independent user-visible purpose from the spec's perspective. Build config, packaging, module stubs, and boilerplate that exist solely to support a feature are part of that feature's scope — not a separate domain. "Scaffolding then implementation" is an implementation-step split disguised as a domain split.

## 5. Self-Review

Before submitting:
- [ ] Every component boundary has a clear responsibility statement
- [ ] Every interface contract specifies what crosses the boundary
- [ ] Data flow traces cover primary paths — no orphaned components
- [ ] Structural decisions have explicit rationale
- [ ] Decomposition covers the full goal spec scope
- [ ] Scopes map to domains, not implementation steps within a domain
- [ ] Each scope has a falsifiable `done_when`
- [ ] `depends_on` captures true ordering constraints only
- [ ] Shared-file conflicts identified and resolved
- [ ] Architecture document is self-contained for code-planners
- [ ] No decisions prescribe implementation detail below interface level
- [ ] Cross-cutting concerns addressed for all components
- [ ] Re-read goal spec — nothing from spec left unassigned

Fix issues before submitting.

# Constraints

- Do not write code — architecture plan only
- Do not prescribe implementation detail below the interface level
- Do not modify existing specs unless task explicitly scopes an update
- Do not invent requirements beyond the goal spec
- Surface contradictions between parent deliverables as Open Questions
- Respect existing architectural patterns unless there is a concrete reason to deviate (document it)

# Anti-Patterns

| Pattern | What goes wrong |
|---------|----------------|
| **Architecture Astronautics** | Over-abstraction for hypothetical flexibility. If an interface has one implementation and no planned second, it's a cost not an investment. |
| **Scope Inflation** | Components or interfaces not required by the spec. The plan should be minimal — just enough structure. |
| **Interface Speculation** | Defining detailed contracts before understanding data flow. Trace data first; interfaces emerge from what needs to cross boundaries. |
| **Monolithic Scope** | One giant scope spanning unrelated domain boundaries. Decompose when a scope covers multiple domains. |
| **Task-Level Decomposition** | Splitting within a domain by implementation steps (e.g., "scaffolding" then "logic", or "core" then "wiring"). Build config and stubs that serve one feature are not a separate domain. Implementation sequencing is code-planner territory; the architect decomposes by domain. |
| **False Independence** | Scopes sharing state or files without `depends_on`. Shared-file audit catches file overlap; also audit for shared-state overlap. |
| **Invisible Decisions** | Structural choices without rationale. Every boundary should answer "why here?" |
| **Cathedral Planning** | Pursuing perfect upfront design. Define boundaries and interfaces; implementation detail crystallizes during code-planning. |
| **Pattern Imposition** | Forcing design patterns onto the problem. Name the problem first; pattern is the solution, not the starting point. |
| **Boundary Amnesia** | Components without interface contracts. Every boundary needs a contract. |

# Integration

| Skill | Relationship |
|-------|-------------|
| **software-architecture-review** | Complementary. This skill *defines*; that skill *evaluates*. |
| **detailed-spec-writing** | Upstream. Goal spec is the primary input. |
| **systemic-thinking** | Used by Architecture Reviewer to evaluate the plan. |

# Mode-Specific Behavior

**Pairing:** Present the architecture plan for human review before writing.

**§BRAND_NAME_TITLE§:** Autonomous within task scope. Plan submitted for review; Architecture Reviewer validates.

| Pairing Prompt | §BRAND_NAME_TITLE§ Behavior |
|----------------|---------------|
| "Parent deliverables conflict — resolve?" | Surface in Open Questions; document both options |
| "Scope too broad — split?" | Decompose further; document split rationale |
| "Existing pattern X — follow or deviate?" | State rationale; reviewer evaluates |
| "Cross-cutting concern Y unaddressed" | Must be addressed before submission |
