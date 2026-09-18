---
name: spec-review
description: Specification Review Protocol
---

## Purpose

Review technical specifications for inconsistencies, gaps, contradictions, and ambiguities. This skill is for finding specification issues, not proposing design changes.

## Trigger

Use this skill when:
- User asks to review specs, documentation, or technical designs
- User asks to find inconsistencies or gaps in documentation

## Reference-First Planning Artifacts

When the reviewed artifact contains `## Source References`, apply the shared
[Reference-First Authoring contract](../shared/references/reference-first-authoring.md) and ADR-0133.
The assigned carrier plus its declared direct references is the review corpus; do not require a
broad parent or ancestor read when those spans are sufficient. Mechanical path, revision, anchor,
and freshness checks are compositor-owned. The reviewer owns semantic coverage and authority.

Reject a strict planning artifact for any of these contract violations:

1. an inherited requirement, constraint, threshold, or interface has no direct reference;
2. unchanged inherited prose is presented as a local decision;
3. the artifact requires a full-parent read when an assigned anchor is sufficient;
4. a local statement conflicts with its inherited owner;
5. `output[]` is used as a second detailed specification;
6. detailed prose is duplicated only to make Markdown and `output[]` character-identical;
7. a correction accumulates unchanged or resolved history;
8. an assigned obligation maps to no reference, or its mapped anchor span omits that obligation;
9. a priority is silently promoted, demoted, or dropped, Must coverage is missing, or Won't scope
   is included;
10. the next role cannot act from the assigned scope and references without inventing policy or
    waiting on an unallocated prerequisite, or evidence is demanded at the wrong stage; or
11. a consequential guarantee has no owner and evidence.

A finding cannot create a commitment: before blocking, name the binding requirement, exclusion,
or correctness obligation the artifact violates. A demand for a Should/Could feature, a stronger
unspecified guarantee, or Won't scope is at most a non-blocking suggestion; a real Must conflict
is reported with evidence and routed to the human.

Repeated labels and IDs, short task intents, exact action-boundary values, and clearly marked
quotations are permitted. Marker-free legacy artifacts retain the existing review path.

## Inputs

The user should provide:
- **Spec location**: Directory or file paths containing specifications. Default to `specs/` if it exists.
- **Optional focus areas**: Specific concerns to prioritize

If not provided, ask:
```
To review your specifications, I need:
1. Where are the spec files? (e.g., specs/, docs/)
2. Any specific concerns to focus on?
```

## Protocol

### Corpus Size

- **Under 50 files**: Process normally
- **50-100 files**: Warn user that quality may degrade; suggest focusing on specific subsystems
- **Over 100 files**: Require user to specify focus areas or subdirectories; refuse to review entire corpus in single pass

If token limits force truncation mid-analysis, stop and report:
- What was fully reviewed
- What was partially reviewed
- What was not reviewed

### Phase 1: Discovery

1. **Map the specification corpus**
   - List all spec files in the provided location(s)
   - Identify the document hierarchy and relationships
   - Note any README or index files that describe reading order

2. **Classify documents**
   - **Primary specs**: Markdown/text documents defining the system (review targets)
   - **Reference material**: OpenAPI schemas, JSON schemas, ADRs, code comments (use for cross-validation, don't review independently)

3. **Read all documents completely**
   - Read every spec file before beginning analysis
   - Track key terms, states, field names, and identifiers as you read
   - Note cross-references between documents

### Phase 2: Analysis

Review against these categories. For each issue found, record:
- **Location:** `file.md:123`, `file.md:50-55`, `file.md#section`, or quoted snippet (≤15 words) if line numbers unavailable
- **Type**: One of the types below
- **Severity**: Critical | High | Medium | Low
- **Description**: What's wrong
- **Suggestion**: How to fix the spec (e.g., "define term", "align names", "add missing state"). Not system design changes.

#### Issue Types

| Type | Definition |
|------|------------|
| **Inconsistency** | Same concept defined differently in different places |
| **Gap** | Missing information needed for implementation |
| **Contradiction** | Two statements that cannot both be true |
| **Ambiguity** | Statement that can be interpreted multiple ways |
| **Missing Reference** | Cross-reference to non-existent section or document |
| **Undefined Term** | Term used without definition |

#### Severity Levels

| Severity | Definition |
|----------|------------|
| **Critical** | Blocks implementation or causes runtime failure |
| **High** | Causes significant confusion or likely bugs |
| **Medium** | Reduces spec quality, may cause minor issues |
| **Low** | Nitpick, polish, or style issue |

### Phase 3: Systematic Checks

Apply these checks to the specification corpus:

#### 1. Cross-Document Consistency
- [ ] Same term defined identically everywhere
- [ ] State/status names match across all documents
- [ ] Field names in examples match schema definitions
- [ ] File/script/component names consistent across documents
- [ ] Path formats consistent (relative vs absolute, directory conventions)

#### 2. Completeness
- [ ] Every state has defined entry conditions and exit transitions
- [ ] Every role/actor has defined: purpose, capabilities, constraints
- [ ] Every operation has: trigger, steps, success/failure outcomes
- [ ] Every data type/field has: purpose, format, valid values
- [ ] Cross-references resolve (no broken links)
- [ ] Error conditions documented with recovery paths

#### 3. Logical Consistency
- [ ] No circular dependencies in sequences or phases
- [ ] State transitions are deterministic (no ambiguous branches)
- [ ] Constraints don't contradict capabilities
- [ ] Numeric limits/thresholds align across documents
- [ ] Default values documented and consistent

#### 4. Edge Cases
- [ ] What happens on component/process failure mid-operation?
- [ ] What happens on data corruption?
- [ ] What happens on race conditions (concurrent access)?
- [ ] What happens on resource exhaustion?
- [ ] What happens when humans don't respond?
- [ ] What happens when external dependencies fail?

#### 5. Undefined or Ambiguous Terms
- [ ] All field/property names documented
- [ ] All states/statuses have clear definitions
- [ ] Functions/operations have precise semantics
- [ ] Format specifications are explicit (dates, IDs, paths)
- [ ] "Magic values" or special cases explained

#### 6. Error Handling
- [ ] Each operation specifies possible errors
- [ ] Error propagation paths documented
- [ ] Partial failure handling specified
- [ ] Recovery procedures defined

#### 7. Testability
- [ ] Success criteria are measurable
- [ ] Validation items can be objectively verified
- [ ] Timeouts/thresholds are specific numbers, not vague terms
- [ ] Examples are concrete and verifiable

### Phase 4: Report

- If the same issue appears across multiple files, report once with all locations listed
- Prefer root-cause framing over symptom repetition

Generate report in this format:

```markdown
# Specification Review: [Project/Component Name]

## Summary

- Critical: N
- High: N
- Medium: N
- Low: N

---

## Critical Issues

### [Issue Title]

- **Location:** file.md:123 or file.md#section
- **Type:** [Issue Type]
- **Description:** [What's wrong]
- **Suggestion:** [How to fix]

---

## High Issues

[Same format]

---

## Medium Issues

[Same format]

---

## Low Issues

[Same format]

---

## Recommendations

[Overall observations about spec quality and suggested improvements]
```

## Constraints

- **DO NOT** modify any file - this skill is read-only, producing a report, not changes
- **DO NOT** suggest changes to the system design itself — only identify spec issues
- **DO NOT** skip files within the agreed review scope — read everything before concluding
- **DO** note when something is intentionally deferred (marked "v2", "future", "out of scope")
- **DO** check that deferred items aren't referenced as if they exist in the current version
- **DO** flag when deferral creates inconsistency in the planned version (e.g., v1 spec assumes a v2 feature, or deferral leaves a gap that breaks v1 completeness)
- **DO** distinguish between "spec doesn't say" (gap) and "spec is wrong" (contradiction)

## Examples

### Example Issue: Inconsistency

```markdown
### Agent State Missing from State Machine

- **Location:** roles.md:330 vs state-machines.md:102-108
- **Type:** Inconsistency
- **Description:** roles.md shows agents register with `status: STARTING`, but state-machines.md only defines IDLE, WORKING, WAITING, HANDOFF. STARTING is not a valid state.
- **Suggestion:** Add STARTING to state machine with transition STARTING → IDLE, or change registration to use IDLE.
```

### Example Issue: Gap

```markdown
### Grace Period Duration Undefined

- **Location:** state-machines.md:263
- **Type:** Undefined Term
- **Description:** Validation rule references "grace period" but duration is never defined.
- **Suggestion:** Define explicitly (e.g., "60 seconds") or reference related timing constant.
```

### Example Issue: Contradiction

```markdown
### Backoff Timing Mismatch

- **Location:** design.md:50-55 vs implementation.md:120-125
- **Type:** Contradiction
- **Description:** design.md specifies exponential backoff (10s, 20s, 40s), but implementation.md shows fixed 5s delay.
- **Suggestion:** Align documents — update implementation to match design or document the simplification.
```

## Notes

- For large spec corpuses, organize findings by document or subsystem
- Prioritize Critical and High issues in the summary
- If spec quality is generally good, say so in Recommendations
