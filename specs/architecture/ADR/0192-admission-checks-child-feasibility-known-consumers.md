# 192 - Admission Checks: Child Feasibility and Known Consumers

## Status

ACCEPTED. Amends [ADR-0150](0150-shared-authoring-vocabulary.md) (W2 handoff
duties).
Extended by [ADR-0198](0198-frozen-interface-corrections-and-direct-coding-allocation.md)
with early epic consumer inventories at the existing author/reviewer checkpoints.

## Context

Planning producers and reviewers check an artifact against its own sources:
coverage, authority, priority, proof stage and dependency meaning (ADR-0150).
No explicit admission duty reconciled its restrictions with all neighbour
obligations already binding, so conflicts surfaced only after the child was
generated and its inputs were part of reviewed ancestry. Repair then needed an
owner amendment plus supersession (D-78). Traced examples:

- An architecture Scope froze "writes only one test file" while every coding
  child must commit an acceptance manifest (acceptance-evidence guide); the
  architect and its reviewer were never told about the manifest.
- A code plan froze "do not rewrite the parser"; code review found a parser
  defect blocking an allocated obligation, and the freeze forced owner routing.
- A code plan bound a coding child to existing test seams that current source
  did not provide; it deferred that premise like an unfinished provider.
- A provider architecture froze a shared interface two days after a consumer
  story stating a need it omitted had merged; the consumer blocked at its own
  architecture review.

## Decision

1. **Child feasibility.** Architect and code-planner self-checks, and the
   shared planning-review rows, require that a child's frozen owned files and
   preserve directives leave room for what its obligations need (tests, each
   acceptance manifest, a correction an allocated obligation requires). A
   premise about existing source is checked before it restricts a child; only
   an unfinished provider's delivery is deferred as readiness.
2. **Consumer asks before freeze.** Epics record a Provider Ask Inventory before
   downstream decomposition: interface ID, consumer epic/ref, source-backed
   required operations, fields and statuses (or explicit none). A shared-interface
   owner obtains the inventory, reconciles coverage against assigned epics and
   inherited obligations, answers or concretely defers every ask, and also searches
   merged specs by interface ID for additional consumers. Unavailable declared asks
   block the affected scope's freeze. A need it cannot settle yet
   is a design prerequisite: an allocated correction or bounded investigation
   plus a hold on the affected scope through existing dependency declarations,
   on the corrected contract rather than an implementation. A gap note on a
   frozen contract does not clear review.
3. **Placement.** Both checks live in the existing producer self-checks, the
   `handoff-review-rows` table, the shared Reference-First Authoring contract
   and spec-review rejection class 10. No review stage, task, phase barrier or
   engine check is added.

## Consequences

- Conflicts visible at admission are refusable by author and reviewer before
  descendant generation.
- This is a mitigation: consumers written after the provider freezes still
   route gaps late; the early epic inventory mitigates this without proving its
   semantic completeness or coordinating new undeclared consumers automatically.
   Latent defects in preserved code
  are still found at code review, and adoption remains reviewer judgment.
  Rendered-template tests prove only that the guidance is present.
- The role-budget baseline was re-anchored: the change adds about 0.7 KB to the
  code-planner prompt, 0.5 KB to planning-reviewer prompts and 0.4 KB to
  Reference-First readers, on top of earlier unanchored growth.

## Alternatives Considered

1. Engine check that each coding child's manifest path lies in its scope.
   Rejected for now: architect and code-planner `scope` is free text; only
   decomposition roots carry typed `owned_files`.
2. A consumer-inventory review stage before every provider freeze. A new global
   phase barrier remains rejected. ADR-0198 adopts an early epic inventory checked
   at existing checkpoints; it does not add an engine stage or infer executable
   dependency edges from interface strings.
3. Accept a recorded gap with an owner as sufficient. Rejected: it admits the
   incompatible contract the examples froze.
