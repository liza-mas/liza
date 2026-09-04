# How to Produce a Goal Document For §BRAND_NAME_TITLE§

A goal document is the input to `§BRAND_BINARY_NAME§ init --spec`. It defines what you want built, why, and what "done" looks like — with enough precision that agents can decompose it into tasks without guessing.

This guide walks you through producing one interactively in Pairing mode.

## Rule of thumb

**Agents may make implementation choices but not product decisions.**

The goal document is where you make every product decision — what to build, for whom, how it behaves, what's out. If a decision is missing from the document, agents will either guess (badly) or block waiting for clarification.

At `general-objective` altitude a goal document is not a technical spec — it defines the problem (WHY) and desired behavior (WHAT), not HOW to implement it. Lower entry points deliberately carry more: see step 1.

## Prerequisites

- Your coding CLI (claude, codex, ...) installed and configured with the §BRAND_NAME_TITLE§ contract
- A rough idea of what you want to build (even a single sentence is enough to start)

## Process

### 1. Run the goal-writing skill

Open the coding CLI in a terminal and invoke:

```
/goal-writing
```

The skill runs four phases. It switches to **Coach** to surface the decisions your chosen entry point requires — Socratic, questioning purpose rather than proposing solutions — then to **Challenger** to stress-test them, then drafts from what settled, then hands off for verification. It will not make your product decisions for you; anything it originates is marked unclaimed until you accept or reject it.

You pick the entry point up front. Use `general-objective` for a goal at product altitude. For lower-level inputs, use `functional-spec` when architecture still needs to be produced, or `technical-spec` when the architecture is already specified and §BRAND_BINARY_NAME§ should start at code planning.

### 2. What the document covers

At `general-objective` altitude:

- **Problem Statement** — what problem, with evidence
- **Target Users** — who benefits, what they need
- **Solution Overview** — how the system solves the problem at a general level: key concepts, main flows, and the decisions that shape the design. This is not implementation detail — it's a non-ambiguous direction for building.
- **MVP Scope** — what's IN the first deliverable
- **Explicit Out of Scope** — what you're NOT building yet
- **General Specification** — For each capability, describe the expected behavior: inputs, outputs, rules, edge cases. Business logic should be explicit enough that an agent doesn't have to infer how things work. If a UI is involved, include layouts and interaction patterns.
- **Success Criteria** — how you know you succeeded
- **Risks and Assumptions** — what could go wrong

Lower altitudes add resolved requirements, acceptance criteria, and — at `technical-spec` — components, interfaces, migrations, and test strategy.

### 3. Review thoroughly

**It is of paramount importance that YOU review this document with the greatest attention because:**
1. This is the document that captures YOUR INTENT. Any drift would compound.
2. This is a crucial opportunity for you to build your mental model of the system to be built, especially if the implementation is to be performed by autonomous agents.

A goal document is ready when it is **solid** and **lean** at its altitude.

**Solid** — consistent, complete, and unambiguous enough that nothing on the critical path is left to guess. "Unambiguous" means good enough, not absolute.

**Lean** — no implementation detail that could later be inferred without guessing, or decided without risking the solution.

What counts as implementation detail depends on the entry point you chose: excluded at `general-objective`, required at `technical-spec` — where "required" means the structural decisions, not every local implementation choice, which stays the coder's.

In practice:

- An engineer unfamiliar with the project could read it and know what to build
- Every section has specifics, not placeholders
- No ambiguous "TBD" items remain on the critical path
- Out-of-scope is explicit enough to prevent creep
- Nothing in it is a choice you took only because you could

### 4. Get reviews from other agents

Open a fresh Pairing session in another pane or window. Ask it to review the
goal document cold — no context from the first session. A good review prompt:

```
Review this goal document as if you were an agent about to decompose it into tasks.
Flag anything ambiguous, missing, or that would force you to guess.
```

§BRAND_NAME_TITLE§'s contract makes the agents accountable. They'd raise concerns if any rather than praising a non-ready document.

Address the feedback and iterate until the reviewer agent approves.

If you have multiple provider subscriptions, it is highly recommended to make different models review the goal spec.

Optionally add a systemic pass, and address whatever it raises:

```
/systemic-thinking path/to/your-goal.md
```

The readiness gate goes **last**, after every edit those reviews produce — a verdict on a document you have since changed certifies nothing. The skill deliberately does not run it: a judge that coached the document is not independent. Run it from a fresh session:

```
/check-§BRAND_NAME_LOWER§-input-readiness path/to/your-goal.md <entry-point>
```

### 5. Initialize the project

```bash
§BRAND_BINARY_NAME§ init --spec path/to/your-goal.md
```

§BRAND_NAME_TITLE§'s orchestrator will use this document as the authoritative source for task decomposition.
