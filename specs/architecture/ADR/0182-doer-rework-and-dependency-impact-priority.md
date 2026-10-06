# 182 - Doer Rework and Dependency Impact Priority

## Status

ACCEPTED — 2026-10-06.

## Context

Doer candidate selection previously shuffled the whole highest explicit priority
tier. With uniform priorities, fresh leaves could repeatedly win over rejected
work and providers gating other tasks. Handoff and owned-executing recovery do
not cover rejected work: rejection needs a normal claim transition and iteration.
Releasing dormant assignments when claiming new work avoids stranded ownership,
but supplies no rework or provider precedence.

## Decision

Keep admission and mutations in the existing claim path. Order its eligible
candidates lexicographically:

1. Lowest explicit numerical priority; retain only that tier.
2. Owned pipeline rejection, other pipeline rejection or integration-failed
   rework, then fresh work.
3. Descending unique active transitive dependent count.
4. Random order among exact ties.

Keep every top-tier candidate for normal failure fallback. Explicit priority
remains authoritative even over lower-priority owned rejection. Recovery of
handoffs and owned executing tasks continues before this selection.

The scheduler builds reverse unmet-prerequisite adjacency once per selection.
Ordinary dependencies use the shared resolver's effective replacement blockers.
Typed provider sets must pass the shared readiness interpretation; valid pending
declarations expand to the unfinished producer and all selected active children,
including children beyond the first blocker reported by readiness. An invalid
typed declaration set contributes no typed demand. Actual child dependencies
remain authoritative after materialization.

Count only materialized operationally active consumers. Terminal/clean historical
tasks and hypothetical unborn outputs add no demand. A real consumer awaiting an
unmerged typed provider promotes that producer even before children exist.
Reachability deduplicates diamond paths and excludes the candidate itself;
iterative visited sets bound traversal on corrupt cycles. No graph or priority
mutation, persistent cache, duration estimate or new task field is introduced.

## Consequences

- Equal-priority rework and gating providers no longer compete uniformly with
  fresh leaves; normal reclaim preserves rejection iteration and audit evidence.
- Broad active fan-out may outrank a long narrow chain. This is dependency-demand
  precedence, not elapsed-time critical-path optimality. Outputs not yet backed
  by active consumer tasks are intentionally outside the estimate.
- Rework can delay equal-priority fresh work. Existing iteration/review controls
  bound rework cycles; explicit priorities retain operator/author authority.
- Preferred ranks can attract concurrent claimers. Exact ties remain shuffled,
  while existing three-phase claims, authority checks and fallback handle races.
- Graph walks cost at most `O(C * (V + E))` for `C` candidates. Shared prerequisite
  interpretation also performs existing state lookups; changing that cost is
  separate from this scheduling policy.

## Alternatives Considered

- Manual priority assignment or a new priority-update command requires continued
  intervention when defaults are uniform; it does not repair automatic selection.
- Treating rejection as owned executing recovery bypasses its status transition,
  iteration consumption and rejection-specific worktree/history handling.
- A projected duration-based scheduler would require metadata and wider lifecycle
  changes. Active prerequisite demand addresses the observed failures with the
  existing state model and shared interpretation.

## Validation

Real-claim regressions first captured fresh-leaf selection over valid owned
rejection and a ready provider. Rank/graph coverage checks numeric priority,
rework classes, replacement lineage, all selected provider children, active-only
demand, diamond deduplication, cycles and preserved candidate membership. Existing
claim tests retain lease, role, dependency, recovery and fallback boundaries.
