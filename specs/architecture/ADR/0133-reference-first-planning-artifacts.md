# 133 - Reference-First Planning Artifacts

## Status

ACCEPTED — prospective adoption approved 2026-09-10.

## Context

Successive planning stages currently repeat upstream requirements, rationale,
and task detail in Markdown, structured `output[]`, and rendered task prompts.
The duplication consumes context, obscures which stage owns a decision, and
makes legitimate later corrections difficult to distinguish from drift.

Planning artifacts must remain locally actionable, but actionability comes from
declared references plus stage-owned decisions. It must not require copying the
upstream corpus into every descendant.

## Decision

Adopt the shared
[Reference-First Authoring contract](../../../skills/shared/references/reference-first-authoring.md)
prospectively for new planning artifacts and explicit corrections.

New-format semantic Markdown opts into strict validation with exactly one
eligible second-level `Source References` ATX heading. The literal grammar is:

    ## Source References
    Source revision: "<40 lowercase hexadecimal Git object ID>"

    ### Direct References
    - "<reference-id>": "<repo-relative-clean-markdown-path>#<exact-heading-text>"
    - "<reference-id>": "<repo-relative-clean-markdown-path>#<exact-heading-text>" @ "<40 lowercase hexadecimal Git object ID>"

    ### Obligation Coverage
    - "<obligation-id>" -> "<reference-id>"
    - "<obligation-id>" -> "<reference-id>", "<reference-id>"

The section, declaration, and non-empty subsections occur exactly once and in
that order. Every quoted token is an RFC 8259 JSON string. Paths are slash-separated,
repository-relative, cleaned Markdown paths. Fragments are exact, case-sensitive
eligible ATX heading source text, not approximate slugs.

Marker detection and span extraction share a scanner. Eligible headings have
zero to three leading spaces, one to six `#` characters, and a following space
or line end. Backtick and tilde fences suppress headings until a valid
same-character closer of at least the opener length. Indented code, headings inside
fences, blockquotes, and setext headings are ineligible. The default revision
applies unless an adjacent override is present.

The normative decision is:

1. Each information class has one owner according to the goal's authority
   matrix. Downstream artifacts cite inherited authority and own only their
   local decision, refinement, allocation, exception, or explicit correction
   delta.
2. Local sufficiency means the assigned section, its declared direct
   references, the task envelope, and stage-appropriate repository evidence
   are enough to act; it does not mean copied ancestor prose.
3. Markdown owns semantic requirements, rationale, architecture, and detailed
   task contracts. `output[]` owns only the concise orchestration envelope and
   anchored artifact references; parity is semantic/referential, never
   character identity.
4. New semantic artifacts use the exact JSON-quoted, fence-aware
   `Source References` grammar above. At one captured integration HEAD, strict carrier
   discovery is bounded to task scalar refs, complete retained review ranges of
   direct merged parents, and the current task's review range. Marker-free
   scalar fragments remain display-only legacy scope hints. Strict scalar
   fragments select exact eligible ATX heading text; an empty strict scalar
   fragment assigns the whole file. Strict Markdown files discovered inside an
   immutable reviewed range are whole-file local artifacts. Scalar carriers
   load at captured HEAD; parent carriers load at the parent's `ReviewCommit`,
   and where a later merge changed that path the integrated version at captured
   HEAD is adopted instead, so only deletion or a stripped contract at HEAD
   blocks; current-review carriers load at the current `ReviewCommit` and may be
   absent or different at HEAD. Every observation completes its own required
   freshness checks before precedence is applied. Per path, current-review
   content precedes parent content, which precedes scalar content; precedence
   selects rendered local content but never suppresses validation of another
   observation, and never widens its scope: where a strict scalar fragment
   assigned a section of that path, the winning observation renders that
   section and everything it shares with the rest of the file, while the
   section's peers — same heading level, same enclosing heading — render as
   one-line pointers to the carrier's own revision. A parent range that
   rediscovers the assigned artifact therefore stops inlining every sibling
   task's section, and an assignment that does not resolve to exactly one
   eligible heading narrows nothing, because narrowing on a guess would drop
   the design and analysis a task reads outside its own section. Conflicting
   same-class provenance blocks; differing eligible parent blobs are
   defensively rejected even though HEAD adoption makes that state
   unreachable. Declared inherited references are read at their default or
   override revisions and their referenced section must be unchanged at captured
   HEAD; whole-path blob identity is the fast path, and an unrelated edit
   elsewhere in that file does not make the reference stale. A current-review
   carrier may reference paths introduced in its own reviewed range, which
   resolve at the current `ReviewCommit`. Missing, stale, deleted, ambiguous, or
   structurally invalid strict content blocks before launch.
   Acceptance allocation judges the same way: a merged planning parent
   authorizes a child when the *allocation section* its `plan_ref` names is
   identical at the parent's `ReviewCommit` and at integration, not when the
   whole carrier file is byte-identical. An edit elsewhere in the carrier — a
   re-pin in `Source References`, a note appended under another heading — does
   not withdraw an approval it did not touch. Because `Source References` is a
   sibling section outside that span, every reference the reviewed contract
   asserts a proof against (those its `approved_proofs` cite) is additionally
   compared by what it *resolves to* at both revisions, so a re-pin may move a
   reference but never substitute the content a proof claims. References
   backing `obligations` alone are deliberately not compared here: a re-pin
   onto legitimately extended content is indistinguishable from a substitution
   at this boundary, and blocking it strands every child of a merged plan whose
   review boundary no command can move. That drift is only partly covered
   elsewhere: reference freshness at prompt build compares a reference's
   section at its pinned revision against the same path and heading at
   integration HEAD, which catches a pin left behind while its target moved,
   but not a reference repointed at a different target that agrees with HEAD.
   Staleness is caught; substitution against an obligation asserting no proof
   is not. An ambiguous or unresolvable heading refuses rather than
   falling back to whole-file scope. The adopted identity recorded for a child
   is the object id of that section, and every boundary that asks whether the
   reviewed source changed — allocation, candidate submission, reviewer
   assignment — compares the same value, so an unrelated edit cannot strand a
   child at whichever boundary still read the file.

5. Corrections are deltas naming corrected anchors, replacement decisions and
   rationale, unchanged inherited refs, and the effective superseding ref.
   Rendered retry context carries only unresolved effective findings and
   corrections.
6. Enforcement is prospective. Marker-free legacy artifacts and scalar
   fragments retain existing behavior; updated producers must emit the marker
   and exact strict subsection fragments, and reviewers reject omission.
   Complete direct-parent reviewed ranges enable strict carrier discovery
   without recursive ancestry reads. An all-absent reviewed range on
   pre-existing merged state retains the legacy path; partially populated or
   inconsistent attribution fails closed. No orchestration-state migration or
   new state field is introduced.
7. Rejected: independently complete copied artifacts, character-identical
   Markdown/JSON, token ceilings alone, approximate heading slugs, silent
   worktree fallback, and big-bang corpus migration.

The authority matrix is:

| Stage | Owns | References or omits |
|---|---|---|
| Goal | Problem, users, scope, behavior, global NFRs, success | External evidence |
| Master decomposition | Coverage partition, ownership, interfaces, cross-child ordering | Goal requirements and rationale |
| Specialized epic | Capability-local interpretation and story boundaries | Goal and master shared constraints |
| User story | Story behavior, ACs, edge cases, local exclusions | Epic personas, shared NFRs, inherited contracts |
| Master architecture | Cross-scope components, interfaces, ownership, dependency direction | Product behavior and AC prose |
| Specialized architecture | Scope-local structure, interfaces, data flow, failure handling | Inherited behavior and shared architecture |
| Code plan | Change units, file ownership, ordering, tests, requirement-to-task allocation | Requirement and architecture prose |
| Coder task | Orchestration envelope and direct executable references | Plan detail, AC text, architecture rationale |
| Review feedback | Current findings, severity, evidence, closure conditions | Unchanged contracts and resolved history |

## Consequences

- Downstream context contains local decisions and explicit authority instead
  of repeated ancestor prose.
- Coverage lists and reviewers must check that anchor spans carry every assigned
  obligation; path resolution alone is insufficient.
- Exact action-boundary values may still be materialized when their source is
  labeled and the value is needed for execution.
- Existing artifacts and state references remain readable. No bulk corpus
  rewrite or state-schema migration is required; a legacy baseline can receive
  a reference-first delta correction.
- The policy reduces behavioral prompt pressure but does not structurally
  enforce semantic coverage. That remains an open architectural concern.

## Alternatives Considered

- **Independently complete artifacts:** rejected because completeness by copying
  multiplies context and authority ambiguity.
- **Character-identical Markdown and `output[]`:** rejected because the two
  representations have different owners and purposes.
- **Token ceilings alone:** rejected because limits do not preserve authority,
  traceability, or semantic coverage.
- **Big-bang corpus migration:** rejected because prospective compatibility and
  delta corrections provide a safer incremental path.
