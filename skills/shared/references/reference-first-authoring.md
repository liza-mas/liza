# Reference-First Authoring

Use one authoritative owner for each information item. A downstream artifact
cites inherited authority, records only its local decision/refinement/allocation,
and never silently changes inherited behavior, constraints, or rationale.
Local sufficiency means actionable from declared references, not a copy of the
upstream corpus. See
[ADR-0133](../../../specs/architecture/ADR/0133-reference-first-planning-artifacts.md).
Coding-child allocations also follow [Acceptance Evidence](acceptance-evidence.md),
which maps these existing obligation IDs to proofs and execution receipts.

## Ownership

| Stage | Owns | References or omits |
|---|---|---|
| Goal | Problem, users, scope, behavior, global NFRs, success | External evidence |
| Master decomposition | Coverage partition, ownership, interfaces, cross-child ordering | Goal requirements and rationale |
| Specialized epic | Capability-local interpretation and story boundaries | Goal and master shared constraints |
| User story | Story behavior, ACs, edge cases, local exclusions | Epic personas, shared NFRs, inherited contracts |
| Master architecture | Cross-scope components, interfaces, ownership, dependency direction | Product behavior and AC prose |
| Specialized architecture | Scope-local structure, interfaces, data flow, failure handling | Inherited behavior and shared architecture |
| Code plan | Change units, file ownership, ordering, tests, requirement-to-task allocation | Requirement and architecture prose |
| Coder task | Orchestration envelope and executable references | Plan detail, AC text, architecture rationale |
| Review feedback | Unresolved findings, severity, evidence, closure conditions | Unchanged task contracts and resolved history |

Only an owner may redefine its information class. Route a needed change to that
owner or block. A later human decision becomes effective only after an owner
correction or decision record gives it an explicit revision override.

## References and Local Sufficiency

New semantic artifacts opt into strict validation with exactly this section:

    ## Source References
    Source revision: "<40 lowercase hexadecimal Git object ID>"

    ### Direct References
    - "<reference-id>": "<repo-relative-clean-markdown-path>#<exact-heading-text>"
    - "<reference-id>": "<repo-relative-clean-markdown-path>#<exact-heading-text>" @ "<40 lowercase hexadecimal Git object ID>"

    ### Obligation Coverage
    - "<obligation-id>" -> "<reference-id>"
    - "<obligation-id>" -> "<reference-id>", "<reference-id>"

The eligible second-level section, source declaration, and two non-empty
subsections occur once and in that order. Every quoted token is an RFC 8259 JSON
string. IDs are non-empty single-line values. Coverage names only declared
reference IDs. Paths are slash-separated, repository-relative, cleaned Markdown
paths: no absolute path, `.`/`..` segment, backslash, control character, or `#`
inside the path. An adjacent override binds only its line.

The fragment is the exact trimmed eligible ATX heading source text, decoded from
the JSON string and matched case-sensitively. A heading has zero to three leading
spaces, one to six `#` characters, and a following space or line end. A shared
scanner excludes headings inside backtick or tilde fences, indented code,
blockquotes, and setext headings. Zero matches fail missing; multiple matches
fail ambiguous. A heading span ends before the next eligible heading of the same
or higher level.

At one captured integration HEAD, strict carriers come only from task scalar
refs, complete retained review ranges of direct merged parents, and the current
task's review range. Marker-free scalar fragments remain display-only legacy
scope hints. Strict scalar fragments select exact heading text; an empty strict
fragment assigns the whole file. A strict Markdown file discovered inside an
immutable reviewed range is a whole-file local artifact.

Every carrier observation completes its own freshness checks before precedence
selects rendered content. Per path, current-review content precedes parent
content, which precedes scalar content; losing observations still validate.
Conflicting same-class provenance blocks. Differing eligible parent blobs are
defensively rejected at observation reconciliation even though normal
HEAD-identity eligibility makes that state unreachable.

Declared direct references resolve at their default or adjacent override
revision and must have blob identity with the same path at captured HEAD. Read
the selected local carrier once, then its deduplicated direct-reference spans.
Do not recursively read every ancestor. Missing, stale, deleted, ambiguous,
structurally invalid, contradictory, or obligation-incomplete references block;
consumers do not reconstruct authority from memory.

An artifact is locally sufficient when its consumer can act from the assigned
section, direct references, task envelope, and stage-owned repository evidence.
Local sufficiency does not permit copied upstream rationale or acceptance text.
Exact task IDs, paths, commands, schema fragments, protocol examples, and other
action-boundary values may be materialized when their source is labeled.

Strict enforcement is prospective. Updated producer formats emit the strict
section and exact strict subsection fragments; reviewers reject omission.
Marker-free artifacts and scalar fragments keep legacy behavior. Complete
direct-parent reviewed ranges enable strict discovery. An all-absent range on a
pre-existing merged parent remains legacy; partial or inconsistent attribution
fails closed. Reference-first resolution alone adds no state fields or migration;
the linked acceptance-evidence contract adds provenance and execution receipts.

## Markdown and Structured Output

Markdown owns semantic requirements, rationale, architecture, and detailed task
contracts. `output[]` owns the concise orchestration projection: intent, scope,
dependencies, validation commands, and anchored artifact references.

- `desc`: one intent, normally at most 160 characters.
- `done_when`: one falsifiable completion observation, normally at most 400 characters.
- `scope`: owned boundary and principal exclusions, normally at most 400 characters.

A longer field needs a reason recorded in the owning Markdown artifact's
`Output` section: the value must be executable at dispatch and have no
authoritative home behind a reference. Review semantic and referential parity,
not character identity. Do not add structured output where a stage currently
has none.

## Corrections

A correction is a delta. Name the corrected artifact and anchors, replacement
decisions and rationale, unchanged contracts by reference, and the effective
downstream superseding reference. Do not accumulate unchanged or resolved
history. Readers consume only the effective correction chain.

## Review Boundary

Reject:

1. an unreferenced requirement, constraint, threshold, or interface;
2. unchanged inherited prose presented as local authority;
3. a full-parent read when an assigned anchor is sufficient;
4. a semantic conflict with an inherited owner;
5. `output[]` used as a second detailed specification;
6. detailed prose duplicated only to make representations character-identical;
7. a correction that accumulates unchanged history; or
8. a declared read set whose anchor spans omit an assigned obligation.

Repeated labels, IDs, short task intents, exact action-boundary values, and
clearly marked quotations are allowed.
