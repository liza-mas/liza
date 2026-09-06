# 132 - Human-Owned Goal Decisions with Independent Readiness Review

## Status

ACCEPTED — implemented 2026-09-04; backfill selected 2026-09-06.

## Context and Problem Statement

Liza MAS is intended to run without human-in-the-loop intervention, so a solid
goal document is a prerequisite. Agents help humans establish that input through
goal writing and readiness checks.

A polished input document can hide decisions the human never made. If an agent
fills gaps while drafting, downstream agents inherit those guesses as settled
intent. Conversely, demanding unnecessary design detail takes choices away from
the architecture or implementation agents best placed to make them.

The entry-point readiness assessment in
[ADR-0094](0094-entry-point-input-readiness-assessment.md) checks an input, but
does not itself establish who made its decisions. Goal production needs a
separate ownership and elicitation workflow, followed by independent assessment.

## User-Confirmed Intent

On 2026-09-06, the user supplied this historical intent: Liza MAS is intended to run without human-in-the-loop intervention. That requires a solid goal document; agents helping humans write goals and check readiness support that requirement.

The user did not identify separate incidents involving invented decisions or author self-certification.

## Considered Options

The workflow explicitly rules out premature drafting and author self-certification:

1. **Draft immediately and ask the human to review.** Agent-originated decisions
   can become indistinguishable from human intent in the resulting prose.
2. **Elicit decisions, then let the authoring session certify readiness.** Better
   provenance, but the judge shares the assumptions it helped put in the document.
3. **Elicit and challenge human decisions, then use a cold readiness reviewer.**
   Separates decision ownership, document production, and final certification.

These comparisons follow the skill's stated rationale; no additional historical
shortlist was provided during backfill.

## Decision Outcome

Choose **Option 3**. The goal-writing skill organizes production around the
selected pipeline entry point:

| Entry point | Human-owned decisions |
|-------------|-----------------------|
| `general-objective` | Product, users, scope, success and the business domain model |
| `functional-spec` / `detailed-spec` | Resolved product behavior and externally imposed structural constraints |
| `technical-spec` | Product plus structural design, boundaries and their rationale |

Non-functional requirements remain human-owned at every entry point. Architecture
agents retain structural design choices when execution begins at architecture.

```text
Agree ownership → elicit → challenge → draft from the decision record
                                             ↓
                    cold adversarial review → resolve findings → readiness verdict
```

Coach and Challenger modes from
[ADR-0013](0013-coach-and-challenger-collaboration-modes.md) precede drafting.
An agent-originated decision remains unclaimed until the human explicitly
accepts or rejects it. A persistent companion decision record captures ownership,
rationale, rejected alternatives and unresolved questions throughout the process.

The authoring session cannot issue the final readiness verdict. A fresh session
reviews the document and decision record, resolves findings with the author,
then applies the readiness gate to the resulting document. Reuse that reviewer
for follow-up rounds; an escalated finding still requires human disposition.
Only the goal document is passed to `init --spec`; the decision record supplies
context for the human and reviewer.

## Rationale

Make provenance visible before fluent prose obscures it. Entry-point-specific
ownership keeps the document sufficiently settled without prematurely prescribing
downstream implementation. A cold reviewer can challenge assumptions the producer
has already normalized, while a final gate avoids certifying an intermediate draft.

## Consequences

- Decisions and their rationale survive pauses before a draft exists.
- The human spends more time settling intent before execution starts.
- Independent review is a completion requirement; unavailable review cannot be
  replaced by author self-certification.
- Independence does not remove the need for per-finding closure, using the
  two-sided exchange in [ADR-0123](0123-two-sided-bounded-review.md).
- Durable structural rationale can be flagged as an ADR candidate, but this
  workflow does not automatically generate or route that ADR downstream.

## Evidence and Reconstruction

Sources: [goal-writing](../../../skills/goal-writing/SKILL.md) and
[goal-production guidance](../../../support-docs/how-to-produce-a-goal.md).
The user approved this record and confirmed the unattended-MAS motivation on
2026-09-06. Specific incidents and historical alternative evaluations remain
unconfirmed.

---
*Reconstructed from commit 73a50ca8 (2026-09-04).*
