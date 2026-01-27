---
name: generic-subagent
description: Context-efficient read-only delegation to subagents
---

Subagents offload context-expensive **read-only** work. The caller receives a digest, not the full trace.
Subagents cannot modify state — they research, analyze, and summarize.

# When to Delegate

See contract § Subagent Delegation Protocol for authoritative triggers.

**Summary:**

| Trigger | Threshold |
|---------|-----------|
| **Uncertain scope** | Assess with cheap ops first → convert to defined |
| **Content to read** | >250KB of files to READ (not search scope) |
| **Processing depth** | >2 intermediate tool calls whose outputs aren't needed in final delivery |

**Clarification on 250KB:**
- Applies to `Read` operations (full content enters context)
- Does NOT apply to `Grep` (only matches enter context)
- Typical sequence: search (cheap) → identify matches → measure size of files to read → apply threshold

## Keep Inline When

- Task requires user interaction mid-execution
- Result interpretation needs full conversation history
- Content to read ≤250KB and processing depth ≤2 steps

# Delegation Protocol

## 1. Assess Scope (if uncertain)

```bash
# Check total size of files to READ (not search)
stat --printf="%s\n" src/api/*.py | awk '{sum+=$1} END {print sum}'
```

## 2. Generate Brief

```
MODE: SUBAGENT
GOAL: {{objective}}
CONTEXT: {{what caller already knows — no pre-analysis required}}
SCOPE: {{files, directories, or boundaries}}
```

**Brief principles:**
- Write from existing knowledge only — if investigation is needed to specify the goal, that IS the delegated work
- Vague goals are valid when they reflect genuine uncertainty
- Subagent has the contract — don't repeat rules in the brief

## 3. Dispatch

Use Task tool. Subagent inherits contract but operates in Subagent Mode (read-only, no gates, compressed output).

---

# For the Subagent

When receiving a `MODE: SUBAGENT` brief:
1. Review ~/.liza/AGENT_TOOLS.md — MCP servers often provide efficient alternatives to manual tool chains
2. Work within scope, abort if insufficient
3. Return structured output (see below)

---

## 4. Review Output

```
RESULT: [success | partial | blocked | failed]
SUMMARY: [what was found/analyzed]
CONCERNS: [issues for caller review]
BLOCKERS: [if not success — what prevented completion]
DETAILED RESPONSE: [findings]
```

**Review:** Verify result addresses goal intent. Address concerns before proceeding.

## 5. Integrate

- **Success**: Use digest, continue main task
- **Partial**: Complete inline or re-delegate with narrower scope
- **Blocked/Failed**: Surface to user or try alternative approach

# Common Delegation Patterns

| Pattern | Goal Template |
|---------|---------------|
| Find definition | "Find where {{symbol}} is defined and its signature" |
| Analyze dependencies | "List what {{module}} imports and what imports it" |
| Explore area | "Understand how {{area}} works" (vague is OK) |
| Search and summarize | "Find all {{pattern}} and summarize their purposes" |
| Architecture survey | "Identify the main components and their relationships in {{area}}" |

# Anti-Patterns

**Don't delegate:**
- State-modifying operations (subagents are read-only)
- Decisions requiring user input
- The debugging hypothesis loop (requires iterative testing with full context)

**Exception — debugging research IS delegatable:**
- "Find all call sites of function X"
- "Find working analogues for this pattern"
- "Check if error handling exists for case Y"

These support Bug Qualification / Pattern Analysis phases without owning the hypothesis loop.

# Parallel Delegation

Independent research tasks can run simultaneously:
1. Identify independent subtasks
2. Generate briefs for each
3. Dispatch in parallel (multiple Task tool calls)
4. Await all results
5. Integrate digests

**Dependency awareness:** If task B depends on task A's output, dispatch sequentially.
