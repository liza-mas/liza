---
name: generic-subagent
description: Context-efficient delegation to subagents (read-only default, READ-WRITE opt-in)
---

Subagents offload context-expensive work. The caller receives a digest, not the full trace.
Default subagents are read-only (research, analyze, summarize). `READ-WRITE` subagents may modify state within declared scope.

# When to Delegate

See the contract's Subagent Delegation Protocol for authoritative triggers.

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
MODE: SUBAGENT READ-WRITE    ← only when objective requires state changes
GOAL: {{objective}}
CONTEXT: {{what caller already knows — no pre-analysis required}}
SCOPE: {{files, directories, or boundaries}}
```

**Brief principles:**
- Write from existing knowledge only — if investigation is needed to specify the goal, that IS the delegated work
- Vague goals are valid when they reflect genuine uncertainty
- Subagent has the contract — don't repeat rules in the brief

## 3. Dispatch

Use Task tool. Subagent inherits contract but operates in Subagent Mode (no external gates, compressed output). Default is read-only; `READ-WRITE` permits state modification with mandatory Intent Gate per action.

## 3b. Model Selection

The `CLAUDE_CODE_SUBAGENT_MODEL` env var sets the default model.
Override it per-agent when the task doesn't need the default's reasoning:

| Task type | Model | Examples |
|-----------|-------|----------|
| **Mechanical** | `haiku` | grep, glob, file reads, size checks, find-and-list |
| **Analytical** | default | code review, architecture survey, dependency analysis |
| **Judgment-heavy** | `opus` | cross-module impact, ambiguous specs, security review |

Rule of thumb: if the brief could be expressed as a shell pipeline
but delegation is used for context isolation, use `haiku`.

---

# For the Subagent

When receiving a `MODE: SUBAGENT` brief:
1. Review ~/§BRAND_GLOBAL_DIRNAME§/AGENT_TOOLS.md — MCP servers often provide efficient alternatives to manual tool chains
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
- State-modifying operations without `READ-WRITE` marker — default subagents are read-only
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
