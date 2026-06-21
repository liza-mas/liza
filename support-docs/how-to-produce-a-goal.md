# How to Produce a Goal Document For §BRAND_NAME_TITLE§

A goal document is the input to `§BRAND_BINARY_NAME§ init --spec`. It defines what you want built, why, and what "done" looks like — with enough precision that agents can decompose it into tasks without guessing.

This guide walks you through producing one interactively in Pairing mode.

## Rule of thumb

**Agents may make implementation choices but not product decisions.**

The goal document is where you make every product decision — what to build, for whom, how it behaves, what's out. If a decision is missing from the document, agents will either guess (badly) or block waiting for clarification.

A goal document is not a technical spec — it defines the problem (WHY) and desired behavior (WHAT), not HOW to implement it.

## Prerequisites

- Your coding CLI (claude, codex, ...) installed and configured with the §BRAND_NAME_TITLE§ contract
- A rough idea of what you want to build (even a single sentence is enough to start)

## Process

### 1. Start a Pairing session

Open the coding CLI in a terminal. You'll use two collaboration modes in sequence: **Coach** first, then **Challenger**.

### 2. Coach mode — surface the WHY

Ask the agent to switch to Coach mode:

```
Switching to Coach — I have an idea for a project but the WHY isn't sharp yet.
```

Coach mode is Socratic. The agent will ask questions, not propose solutions. Expect questions like:

- Who is this for? What problem are they hitting today?
- What happens if we don't build this?
- What's the smallest version that would be useful?

Stay in Coach until you can articulate the problem statement, target users, and MVP scope without hesitation. If you find yourself saying "I think" or "maybe" — you're not done yet.

### 3. Challenger mode — stress-test the WHAT

Once the vision feels solid, switch to Challenger:

```
Switching to Challenger — the vision feels ready, attack it.
```

Challenger will try to break your plan:

- What's the strongest argument against this approach?
- What failure mode haven't you considered?
- What's explicitly out of scope, and are you sure it can wait?

Defend or revise. The goal isn't to survive unscathed — it's to surface gaps before agents hit them.

### 4. Write the goal document

Still in the same session, ask the agent to draft the goal document based on the conversation. The document should cover:

- **Problem Statement** — what problem, with evidence
- **Target Users** — who benefits, what they need
- **Solution Overview** — how the system solves the problem at a general level: key concepts, main flows, and the decisions that shape the design. This is not implementation detail — it's a non-ambiguous direction for building.
- **MVP Scope** — what's IN the first deliverable
- **Explicit Out of Scope** — what you're NOT building yet
- **General Specification** — For each capability, describe the expected behavior: inputs, outputs, rules, edge cases. Business logic should be explicit enough that an agent doesn't have to infer how things work. If a UI is involved, include layouts and interaction patterns. Use the pairing agent to produce them interactively.
- **Success Criteria** — how you know you succeeded
- **Risks and Assumptions** — what could go wrong

### 5. Review thoroughly

**It is of paramount importance that YOU review this document with the greatest attention because:**
1. This is the document that captures YOUR INTENT. Any drift would compound.
2. This is a crucial opportunity for you to build your mental model of the system to be built, especially if the implementation is to be performed by autonomous agents.

### 6. Iterate until tight

Continue pairing to fill gaps. A goal document is ready when:

- An engineer unfamiliar with the project could read it and know what to build
- Every section has specifics, not placeholders
- No ambiguous "TBD" items remain on the critical path
- Out-of-scope is explicit enough to prevent creep

### 7. Get reviews from other agents

Open a fresh Pairing session in another pane or window. Ask it to review the
goal document cold — no context from the first session. A good review prompt:

```
Review this goal document as if you were an agent about to decompose it into tasks.
Flag anything ambiguous, missing, or that would force you to guess.
```

§BRAND_NAME_TITLE§'s contract makes the agents accountable. They'd raise concerns if any rather than praising a non-ready document.

Address the feedback and iterate until the reviewer agent approves.

If you have multiple provider subscriptions, it is highly recommended to make different models review the goal spec.

Do a final pass with the systemic-thinking skill:
```bash
/systemic-thinking path/to/your-goal.md
```

### 8. Initialize the project

```bash
§BRAND_BINARY_NAME§ init --spec path/to/your-goal.md
```

§BRAND_NAME_TITLE§'s orchestrator will use this document as the authoritative source for task decomposition.
