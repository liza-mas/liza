# 154 - Decision Records in the Universal Contract

## Status

ACCEPTED — implemented 2026-09-21; backfilled 2026-09-21.

## Context and Problem Statement

Nothing instructed an agent to read or write an architecture decision record. CORE,
both mode contracts and `pipeline.yaml` mentioned ADRs zero times, and so did
`architecture-planning` — the one role whose job is making structural decisions.
The only rule was GUARDRAILS G2.4, which is project-specific, so every other
project running this system got no ADR behavior at all. This repository's records
exist because a human wrote them in pairing sessions.

CORE cannot carry the practice by naming a path: G1.1 forbids assuming a project's
layout.

## Commit-Evidenced Intent

The commit states the gap directly, including the zero-mention count and the
observation that the existing records are a product of human pairing rather than of
any instruction.

## Considered Options

1. **Leave it to project guardrails** — each project adds its own G2.4.
2. **Name the ADR directory in CORE** — the simple version, blocked by G1.1.
3. **Split the practice: CORE states the artifact class and when it applies;
   `architecture-planning` owns the write half.**

## Decision Outcome

Chose **Option 3**.

CORE states the artifact class and when it applies, always qualified by "where the
project keeps one". Decision records join specs, docs and lessons as durable
memory, and a structural decision that constrains later work becomes a Doc Impact
category. This reuses the DoR/DoD gates rather than adding a rule or a tier entry,
and it *declares impact* instead of instructing creation — which keeps routine
choices from each becoming a record.

`architecture-planning` takes the write half, as the only role with the altitude to
judge durability. Survey now reads prior decisions rather than only code; a new
constraint routes a pattern deviation — whose "(document it)" previously resolved
to the plan — and any ADR candidate named upstream into the record. That supplies
the consumer `goal-writing` declares missing when it flags a candidate it is
forbidden to write. Phrasing reuses `goal-writing`'s "durable enough to constrain
later work", so the two skills read as one rule.

G2.4 keeps its trigger, which covers reviewing sessions that never load
`architecture-planning`, and keeps its architectural-issues item. Only the clause
CORE now states is trimmed, leaving the path binding that CORE structurally cannot
hold.

## Rationale

Option 1 is the status quo, and its cost is that the practice does not travel: a
stack-agnostic orchestrator whose decision-record behavior lives in one project's
guardrails has no such behavior for anyone else.

Option 2 is what a reader expects and what G1.1 forbids — "Would this work for a
Python project with no Makefile?" applies equally to a project with no
`specs/architecture/ADR/`. The split resolves the tension by putting the
path-independent half in CORE and the path-dependent half where a project's
conventions are already visible.

Declaring impact rather than instructing creation is the load-bearing choice. An
instruction to write a record makes every decision a candidate; a Doc Impact
category makes the author say whether this one constrains later work, which is the
question that separates a record from a commit message.

## Consequences

- Projects other than this one now get decision-record behavior from the universal
  contract.
- `architecture-planning` reads prior decisions during survey, so a plan that
  contradicts a recorded decision is visible at planning time rather than at
  review.
- The chain `goal-writing` flags is closed: a candidate named upstream has a role
  that can write it.
- CORE gains no new rule or tier entry; the behavior rides the existing DoR/DoD
  gates. A change to those gates now also changes decision-record behavior.
- G2.4 is narrowed but retained, so review sessions that never load
  `architecture-planning` keep their trigger.

## Evidence and Reconstruction

Source: commit `a91a9fb59`, which states the gap, the G1.1 constraint that rules
out naming a path, and the reasoning for declaring impact over instructing
creation. No separate user intent was supplied; alternatives are reconstructed from
the commit's own account of what CORE structurally cannot do.

---
*Reconstructed from commit `a91a9fb59` (2026-09-21).*
