# ADR-0162: Seam-Scoped Global Integration Analysis

## Status

ACCEPTED — 2026-09-28. Amends
[ADR-0113](0113-sliced-integration-analysis-and-final-closure.md)
(the scope and review surface of the global analysis).

## Context

The global analysis reviewed `goal.base_commit..<source commit>` of the
integration branch. On a shared, long-lived integration branch that range is
the branch history, not the goal's change set: it also carries other tickets'
merges and merges from the main branch. One observed run diffed 647 files, of
which 208 came from other work. Findings in foreign code turn into fix tasks
outside the goal.

The analyst and reviewer were also told to perform an independent aggregate
review of the whole goal snapshot. Each plan's internals were already reviewed
twice, per task and by a slice analysis or approval attestation. The only thing
no earlier stage sees is the interaction between plans. Re-reviewing everything
duplicates that work, scales with the goal's size, and invites new findings in
approved code. One global analysis ran for hours on full suites and a goal-wide
read before a human stopped it.

## Decision

- **Goal-owned surface.** The global prompt attributes each merged goal task's
  reviewed range (`base_commit..review_commit`) to its contributing plan: the
  merged leaves of the plan's frozen root lineages plus the merged repairs of
  its slice analysis. This is the attribution the slice analysis already uses.
  Commits that reached the integration branch through other work never appear.
- **Seams between plans only.** A seam is a path touched by two or more plans,
  or an interface declared (owned or consumed) by two or more plans. The seam
  identity is always the contributing plan. A single-plan cohort has no
  cross-plan seam. Merged repairs of earlier global generations are listed for
  navigation and suite diagnosis. They never make a path a seam by themselves.
- **Suites at HEAD.** The analyst runs the project's declared suites once over
  the whole branch, because a merge from other work can break goal code.
- **Two finding routes.** A finding either records a disagreement at a seam, or
  a suite failure observed at HEAD that breaks code or tests the goal changed,
  seam or not. Plan-internal code is not audited without an observed suite
  failure. Defects confined to code the goal did not change are observations in
  the report, never fix tasks. The integration reviewer rejects fix tasks
  outside both routes, and its enrichment stays inside the same boundary.
- The surface is computed when the prompt is built, from the frozen cohort and
  immutable reviewed commits, rather than persisted in analysis metadata. Global
  analysis starts only after all coding and repair work is terminal, so the
  analyst and the reviewer see the same surface. A merged goal task without
  reviewed-range attribution fails prompt construction, as it fails slice
  projection.

## Consequences

- Global review effort scales with the cross-plan surface, not with the branch
  history or the goal's size.
- Global analysis no longer creates fix tasks for other work's defects.
- A single-plan cohort with several root lineages gets no composition review:
  the zero-slice rule creates no slice, and the global pass checks no
  intra-plan seam. That gap is accepted under the human rule this decision
  records; closing it would be a separate decision.
- The analysis without integration metadata (no sliced topology) still diffs
  `goal.base_commit..HEAD`; this decision does not change it.

## Alternatives Considered

- **Keep the branch range, add a prose exclusion of foreign work.** Rejected:
  the analyst still receives the misleading surface and must reconstruct
  attribution by hand.
- **Diff the first-parent side of each goal merge.** Rejected: task reviewed
  ranges already exist and are what reviewers approved.
- **Persist the surface in analysis metadata.** Rejected for now: it needs a
  schema, validation, and recovery change, and in-flight global analyses would
  not benefit.
- **Treat root lineages as seam units for a single-plan cohort.** Rejected
  during plan review: it reintroduces intra-plan review, which the human rule
  excludes.

## Evidence

- Operator notes D104 and D105 (2026-09-27), promoted to P1 on 2026-09-28.
- `internal/ops/integration_global_surface.go`, `internal/agent/prompt.go`,
  `internal/prompts/templates/blocks/branch_integration_context.tmpl`,
  `internal/prompts/templates/blocks/review_instructions.tmpl`.
