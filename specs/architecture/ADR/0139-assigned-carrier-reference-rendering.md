# 139 - Assigned-Carrier Reference Rendering

## Status

ACCEPTED

## Context

ADR-0133 bounds a task's read set to "the assigned section, its declared
direct references, the task envelope, and stage-appropriate repository
evidence", and `roles.md` tells reviewers to "review the assigned carrier and
declared direct-reference spans, not every ancestor by default". The compositor
resolved that read set correctly but rendered more than it: every carrier's
declared direct references were inlined in full, including those of carriers
inherited from further up the lineage — the epic behind an architecture plan,
the architecture plan behind a code plan. Those references were chosen for
the ancestor's author, not for the task.

Measured on one complete run (558 prompts, 136 MB): references declared by
ancestor carriers were 23% of all rendered prompt bytes and 31–32% of coder
and code-reviewer prompts, with 2.1 ancestor carriers and 26 such references
per affected prompt. Prompt bytes are re-read at cache-read price on every
turn, so rendered context size, not fresh input, governs spend.

## Decision

The compositor classifies each carrier as *assigned* or *ancestor*, and the
renderer inlines only the assigned carriers' declared references.

Assigned carriers are the parent-class and review-class carriers, plus the
most specific strict scalar carrier present, in the order `plan_ref` >
`arch_ref` > `epic_ref` > `spec_ref`. Every other scalar carrier is an
ancestor. The scalar clause is load-bearing, not an edge: when the assigned
artifact also arrives through a merged parent's reviewed range, that
observation wins the path and is assigned anyway, but when the parent is not
`MERGED` or has no reviewed range, the scalar ref is the only route to it.

Ancestor carriers are still inlined in full. Their references render as one
line each: a pointer to the carrier that inlines the same reference in full
when one does, otherwise a pointer to the pinned revision with the `git show`
command that reads it. A reference shared by an ancestor and an assigned
carrier is rendered in full exactly once. Validation is unchanged: every
declared reference is still resolved, freshness-checked, and span-extracted
before launch, so a stale or missing ancestor reference still blocks.

`referencecontract.Carrier.ElideRefs` carries the classification. Its zero
value renders every reference in full, so a caller that does not classify
gets the complete context rather than a silently reduced one.

## Consequences

- On the calibrated fixture the rendered prompt drops from 413,308 to
  278,994 bytes (-32.5%), all of it in the ancestor rows; every other section
  is byte-identical.
- A planner that under-declared its own references and relied on transitive
  inclusion now leaves the coder a pointer instead of a section. That is the
  coverage gap ADR-0133 already assigns to reviewers ("anchor spans carry
  every assigned obligation"); the pointer keeps it discoverable.
- Whether agents re-read pointed sections often enough to cancel the saving
  is not known. The next run's cache-read tokens per merged task, against
  the prior run's accounting, is the falsifier.
- Ancestor carrier bodies remain inlined. Section-scoping them is a separate,
  smaller lever and is not decided here.

## Alternatives Considered

- **Render ancestor carriers as pointers too:** rejected; the assigned
  carrier's references into the architecture plan rely on it being inlined,
  and the carrier body is the ancestor's own decision record, which the task
  does need.
- **Section-scope ancestor carriers to the headings the assigned carrier
  references:** deferred; roughly half the lever with more machinery, and
  measurable only after this step lands.
- **Heuristic relevance pruning of references:** rejected; ADR-0133's
  authority model is explicit declaration, and a heuristic would make the
  read set non-deterministic.
