---
name: detailed-spec-writing
description: Transform requirements into precise specifications for coding tasks
---

# Objective

Transform fuzzy requirements into precise, SMARC specifications an Orchestrator can decompose into coding tasks.

Output: a git-tracked markdown document. One spec per task; one cohesive capability per spec.

# Trigger

- Orchestrator creates a spec-writing task (§BRAND_NAME_TITLE§ mode)
- User asks to spec a feature or requirement (Pairing mode)

# Inputs

Your task provides:
- An **output file path**
- **References** to source material (vision doc, prior specs, existing code)

**Scope discipline:**
- **Upfront:** Read task references + scan existing specs in same domain for consistency.
- **Reviewer-driven:** Broader material only when Spec Reviewer feedback identifies gaps.
- Declare what you read and why in References.

# Output Format

[PRD format](references/prd-format.md) template at the task-specified path.

# Protocol

## 1. Parse

Read source material. Distinguish what is **said**, **implied**, and **missing** before writing any requirements.

Multiple independent capabilities → flag to Orchestrator for split. Do not silently produce a mega-spec.

## 2. Write

**SMARC** every functional requirement — Specific, Measurable, Achievable, Relevant, Context-bound
(implementable within a single agent's context budget — if it forces loading many files or deep cross-component
knowledge, decompose further).

Cannot make it SMARC → Open Question, never a vague requirement.

**References:** Each feature traces to source material. No traceability = Scope Absorption.

**Dependencies:** True ordering constraints only. FT-X depends on FT-Y only if FT-X cannot be implemented
or tested without FT-Y.

**Assumptions are first-class outputs.** Surfaced ambiguity is more valuable than papered-over gaps.
Assumptions resolve *behavioral* ambiguity only — if an assumption names a file format, storage engine,
library, or protocol, you've crossed into design territory. State the behavioral need and move on.

**Acceptance Criteria are contracts.** ACs define done. If they don't fully define done, the spec is incomplete.

**Audience:** Write for the implementing engineer. No marketing language, hedging, or filler.

## 3. Self-Review

Before submitting:
- [ ] Every FR/NFR traces to a source reference
- [ ] Added specificity beyond source is traceable to an Assumption, derivation, or Open Question
- [ ] Every AC maps to ≥1 FR; ACs test requirements, they don't create them
- [ ] No LOW-confidence assumptions in requirements (they belong in Assumptions section)
- [ ] Out of Scope covers adjacent concerns the Coder might drift into
- [ ] Spec is self-contained — Coder needs only this spec and referenced files
- [ ] Re-read source — nothing missed, nothing added beyond scope
- [ ] No assumptions/NFRs prescribe implementation choices — only observable behavior
- [ ] No OQ contradicts or duplicates an existing FR

Fix issues before submitting.

# Constraints

- Do not write code — spec only
- Do not modify existing specs unless task explicitly scopes an update
- Do not invent requirements beyond source material and task scope
- Surface contradictions between references as Open Questions

# Anti-Patterns

| Pattern | What goes wrong |
|---------|----------------|
| **Wishful Specification** | Untestable requirements. *"User-friendly"* is not a requirement. |
| **Hidden Coupling** | References other components' behavior without declaring a dependency. |
| **Assumption Burial** | Assumptions embedded in FRs instead of surfaced in Assumptions section. |
| **Scope Absorption** | Source says "support X" → you spec X, Y, and Z. Stick to task scope. |
| **Premature Solutioning** | Specifying *how* instead of *what*. Common leak: assumptions resolving technical ambiguity instead of stating behavioral need." |
| **Context Explosion** | Requirement needs the entire codebase to implement. Decompose until bounded. |

# Integration

| Skill | Relationship |
|-------|-------------|
| **spec-review** | Downstream. Validates specs against completeness/consistency/testability checklist. |
| **spec-backfill** | Complementary. Backfill: specs from code (archaeology). This skill: specs from requirements (forward-looking). Same `specs/` hierarchy. |
| **code-review** | ACs are the contract bridge — Coders implement against them, reviewers validate against them. |

# Mode-Specific Behavior

**Pairing:** Present draft for human review before writing. Human may redirect scope, resolve OQs, or confirm assumptions.
**§BRAND_NAME_TITLE§:** Autonomous within task scope.

| Pairing Prompt | §BRAND_NAME_TITLE§ Behavior |
|----------------|---------------|
| "Multiple capabilities — split?" | BLOCKED with split recommendation |
| "LOW confidence assumption — resolve?" | Surface in Assumptions; human resolves before coding sprint |
| "Adjacent spec may conflict?" | Read adjacent spec, declare in References, note in Context |
