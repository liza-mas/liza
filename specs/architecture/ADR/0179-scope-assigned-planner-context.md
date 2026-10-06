# 179 - Scope-Assigned Planner Context

## Status

ACCEPTED

## Context

Architecture outputs naming a bare document assign all its Scope sections and
references to each specialized architect and code-planner. Parent reviewed-range
precedence preserves the assignment, but ADR-0139 still inlines shared carrier
bodies and every assigned carrier reference. Large prompts also omit provider
architecture/plan pointers and the generated coding-task IDs a held unit binds to.

The author knows the assigned Scope and its required authority. The compositor
cannot derive that read set reliably from prose, coverage tables, interface
names, or task suffixes. Narrow presentation must preserve complete validation
and navigation to inherited authority.

## Decision

This amends the presentation rules of
[ADR-0133](0133-reference-first-planning-artifacts.md) and
[ADR-0139](0139-assigned-carrier-reference-rendering.md).

### Explicit Scope read set

Both master and specialized architects emit each `output[].arch_ref` as
`<doc>.md#<exact Scope heading>`. New architecture submissions require a
nonempty fragment resolving to one eligible ATX heading; expansion propagates
that validated reference without inventing a heading from an output index.
Each Scope declares its complete required context once, outside code fences:

    **Direct references:** ["product-id", "shared-architecture-id"]

The RFC 8259 JSON array names distinct IDs in the carrier's existing
`Source References` / `Direct References`; `[]` is an explicit empty read set.
Include every needed inherited product obligation and shared decision,
constraint, interface, or hold. Same-file shared sections use exact references
at pins where the path and section content already exist. The declaration
does not create a new authority source or replace Obligation Coverage review.
Malformed, duplicate, or unknown declarations are correctable submission errors.

Only architect and code-planner **doers** opt into narrowed presentation, when
the most-specific strict assignment resolves to one section with a valid
declaration and scalar assignments do not conflict. The winning observation
renders only that section and its selected direct-reference spans. Peer/shared
sections and other inherited carrier bodies render as pinned pointers; selected
references into those bodies still render their exact spans in full. A pointer
cannot satisfy containment for an inlined reference. Unselected reference IDs
may share a carrier's pinned Source References navigation command; individual
drift and unresolved disclosures remain visible.

All observations and declarations are discovered, resolved, freshness-checked,
drift-checked, and proof-checked before presentation selection. Elision cannot
relax any launch or submission refusal. Other roles retain ADR-0139 rendering.
Bare refs, omitted declarations, and conflicting assignments retain existing
presentation. Merged malformed or unknown declarations remain discoverable and
disclosed rather than silently selecting an empty read set. No heuristic
relevance filtering, recursive reference traversal, or new state schema is added.

### Provider and rework navigation

Both planner doers receive compact provider pointers immediately, including on
legacy tasks. Providers come from explicit dependencies, read-only task
dependencies, consumed-interface owners, and all sibling Scope 0 tasks. A
code-planner also uses its architecture parent's sibling cohort. Explicit
Scope 0 fragments identify foundation scopes; the generated output-index-0
sibling is a labeled foundation candidate for legacy cohorts, without claiming
a Scope heading. Exact interface names take precedence; a leading-ID match is
usable only when unique. Missing or ambiguous ownership is visible, never guessed.
These pointers report state; they create no dependency or provider commitment.

Every provider exposes both architecture and plan artifacts, including planning
descendants. Each merged artifact is pinned to its own producing task's reviewed
commit with a shell-safe read command; unmerged or unattributed artifacts are
explicitly unavailable or unpinned. Group every generated coding-task ID/status
and exact plan heading under its producing plan. Resolve a held Unit through
these stored refs, never by constructing an ID: Unit N commonly maps to
`code-(N-1)`, and nonnumeric labels also work. Stable dependency order, sorted
discovered candidates, deduplication, and cycle-safe planning/coding lineage
traversal retain all mappings without provider or child-count truncation.
Missing outputs or generated units remain explicit. Duplicate collective graph
rows may cross-reference these mappings; unrelated graph facts remain available.

The closest merged same-role-pair sibling, otherwise the most recent merged
same-role-pair artifact (deterministic task-ID ties), supplies a pinned **format
precedent only**, with no dependency or authority implied. On iteration >=2,
Markdown `file:line` citations in the latest retained rejected history entry
route to deduplicated exact sections at that rejected commit, with line bounds
and read commands. Start with these cited sections; the full rejection and
document-wide obligations remain authoritative. Unresolvable navigation is
disclosed rather than fabricated.

### Adoption and measurement

A rebuilt binary does not manufacture declarations or fragments for approved
legacy artifacts. Bare/no-declaration assignments retain their safe fallback
while receiving provider, precedent, and rework pointers. To adopt narrowed
context for an existing task:

1. Commission a normal reviewed correction to its owning architecture, adding
   complete per-Scope declarations and exact output fragments.
2. Merge the correction through the normal review boundary.
3. Recreate or retarget child assignments through authorized lifecycle
   operations. Do not edit live state or approved artifacts outside that process.

An already anchored assignment adopts reviewed declaration changes at captured
integration HEAD through ADR-0133's existing rule. Adoption does not change
proof, acceptance, freshness, or dependency contracts.

Measure complete architect and code-planner prompts, separating fixed prompt,
assigned section, selected references, navigation, and provider mappings. Roughly
60 KB is a benchmark: preserve required authority and mappings, and report any
measured excess. A corrected-artifact replay must be labeled separately from a
legacy baseline; it is not automatic migration or evidence of live token/turn
savings. Subsequent live-run measurements are needed for those claims.

## Consequences

- Assigned Scope context can omit unrelated bulk while keeping exact authority
  and provider/unit bindings reachable without task-state lookup loops.
- Semantic completeness remains the producer's and reviewer's responsibility;
  under-declared shared constraints remain a risk, with pinned navigation as a
  discovery backstop rather than an excuse to omit required context.
- Compatibility remains prospective. Existing tasks require explicit reviewed
  adoption; no live-state migration or provider commitment is manufactured.
- Complete prompt sizes and live reading behavior determine the savings;
  a byte benchmark never justifies dropping a binding obligation.

## Alternatives Considered

- **Infer relevance from prose or coverage:** rejected; cannot establish an
  explicit, reproducible read set or safely retain shared constraints.
- **Truncate pointers or mappings to meet a ceiling:** rejected; can hide the
  held unit or provider contract the consumer needs.
- **Rewrite live assignments automatically:** rejected; bypasses reviewed
  authority and lifecycle ownership.
- **Keep all shared and ancestor bodies inline:** retained as compatibility
  fallback; explicit declarations permit the narrower safe projection.
