# 180 - Master-Decomposition Reference Pointers

## Status

ACCEPTED

Amends [ADR-0139](0139-assigned-carrier-reference-rendering.md).

## Context

A decomposition root receives every strict carrier in its direct parents'
reviewed ranges. An epic range can contain all child stories. Treating each
story as an assigned carrier also inlines every story's declared references,
even though those sections are second-hop context for the master task. D-45
identified master prompts approaching one megabyte, dominated by this fan-out.
Deduplication cannot eliminate distinct external sections.

## Decision

The compositor uses the pipeline's existing `decomposition-root` role-pair
metadata for both doers and reviewers, including custom role pairs. After
complete discovery and validation, it marks parent-class carriers' declared
references for the existing `ElideRefs` pointer rendering, except on the path
of the most-specific strict scalar assignment (`plan_ref` > `arch_ref` >
`epic_ref` > `spec_ref`). The exception follows the path through higher-precedence
parent observations. Current-review carriers retain full declared references.

Every parent carrier body remains available, including its Source References.
An omitted reference retains its path, exact heading, revision and shell-safe
`git show` command, or points to its full emission elsewhere in the context.
References declared by the master submission remain full spans, including a
reference also declared by an elided child; existing once-only emission applies.
Ordinary tasks keep ADR-0139's classification. An explicit Scope read set may
subsequently narrow doer presentation under
[ADR-0179](0179-scope-assigned-planner-context.md).

This is presentation only: no observations or reference resolutions are
skipped. Freshness, proof checks, refusal of invalid pins, carrier provenance,
and drift disclosures remain unchanged. No state migration, recursive lookup,
path/role-name heuristic, new configuration field or byte cap is introduced.

## Consequences

- Prompt size no longer scales with full child-only second-hop spans, while
  carrier bodies and explicitly declared master authority remain reachable.
- A master requiring an external span must declare it instead of relying on
  implicit child-story fan-out. Pointer reads remain a discovery backstop.
- Full-compositor regression tests measure a synthetic high-fan-out prompt
  against ordinary full-parent rendering. Offline byte savings do not establish
  live token/session costs or missing-context rejection rates; those require
  subsequent operational measurement.
- Carrier bodies and required master spans can still exceed a size benchmark.
  A benchmark never authorizes removing binding authority.

## Alternatives Considered

- **Global byte budget:** needs an independent policy for required authority;
  truncation is not needed to correct the demonstrated classification fault.
- **Story paths or built-in role names:** would miss custom pipeline topology.
- **Pointer-only child carriers:** would also remove the stories the master
  is assigned to partition. Keep their bodies and elide only second-hop spans.
