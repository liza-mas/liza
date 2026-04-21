---
name: spec-review
description: "Reviews technical specifications for inconsistencies, gaps, contradictions, ambiguities, undefined terms, and missing references. Produces a severity-ranked report with file-level locations and fix suggestions — without proposing design changes. Use when the user asks to review specs, audit documentation, find inconsistencies, or validate technical designs."
---

## Purpose

Find specification issues, not propose design changes.

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
- [ ] Terms, state/status names, field names, file/component names, and path formats consistent across all documents
- [ ] Examples match schema definitions

#### 2. Completeness
- [ ] Every state, role, operation, and data type fully defined (entry/exit conditions, capabilities, constraints, triggers, outcomes, valid values)
- [ ] Cross-references resolve; error conditions have recovery paths

#### 3. Logical Consistency
- [ ] No circular dependencies; state transitions deterministic
- [ ] Constraints don't contradict capabilities; numeric limits align across documents

#### 4. Edge Cases
- [ ] Failure mid-operation, data corruption, race conditions, resource exhaustion, unresponsive humans, and external dependency failures all addressed

#### 5. Undefined or Ambiguous Terms
- [ ] All fields, states, operations, and formats explicitly defined; magic values explained

#### 6. Error Handling
- [ ] Each operation specifies possible errors, propagation paths, partial failure handling, and recovery

#### 7. Testability
- [ ] Success criteria measurable; thresholds are specific numbers; examples are concrete and verifiable

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
