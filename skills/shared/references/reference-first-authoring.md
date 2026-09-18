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

## Priority, Commitments and Proof Stage

Each scoped requirement or output carries an effective MoSCoW label and source
anchor. Must and Won't bind: Must requires inclusion, Won't requires exclusion
this run. Should and Could may be deferred with a recorded disposition (prefer
Should when both are ready and costs do not dictate otherwise); deferral never
waives correctness, safety, or acceptance obligations of included work.
Splitting or merging keeps constituent priorities; a shared prerequisite a Must
needs stays necessary without promoting optional features that share it. Never
relabel scope: a Must that needs a Should/Could or Won't capability is a source
conflict for the human. A missing label is neither Must nor optional — resolve
it at the source; frozen runs keep their obligations until a reviewed scope
decision.

Where it affects scope, observable behavior, feasibility, ordering, or
acceptance, distinguish a human requirement, a necessary implication, a design
choice, and an unresolved assumption. A consequential unresolved commitment is
recorded once, in the owning artifact: source anchor or decision, claim, owner,
evidence needed, affected outputs, settling stage. Downstream cites it. A
consequential API or platform assumption is checked against the implementation,
primary documentation, or a targeted probe — a test of an invented adapter
proves nothing about the real one.

Priority, readiness, and proof stage are separate properties:

| Proof kind | Treatment |
|---|---|
| Design prerequisite | Resolve before dependent architecture, or allocate a bounded investigation with an explicit hold. |
| Implementation prerequisite | Affected work waits for the delivered provider or harness; unrelated work proceeds. |
| Final acceptance evidence | Named owner, prerequisites, explicit final hold; not a pre-coding gate without a concrete dependency reason. |

An unavailable acceptance target neither blocks all implementation nor waives
the test. A human-mandated proof stage or guarantee changes only by the human's
decision; a known incompatibility is escalated, not postponed.

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

The fragment is the exact trimmed ATX heading text, matched case-sensitively;
headings inside code fences do not count, and an ambiguous or missing heading
blocks. A strict scalar ref with an empty fragment assigns the whole file. The
compositor resolves declared references at their revision, checks
them against integration HEAD, and renders the assigned carrier with its
deduplicated reference spans (ADR-0133 has the discovery and precedence rules).
Read that rendered context once; do not recursively read every ancestor.
Missing, stale, ambiguous, structurally invalid, contradictory, or
obligation-incomplete references block; consumers do not reconstruct authority
from memory.

An artifact is locally sufficient when its consumer can act from the assigned
section, direct references, task envelope, and stage-owned repository evidence.
Local sufficiency does not permit copied upstream rationale or acceptance text.
Exact task IDs, paths, commands, schema fragments, protocol examples, and other
action-boundary values may be materialized when their source is labeled.

Strict enforcement is prospective: updated producer formats emit the section;
reviewers reject omission; marker-free artifacts keep legacy behavior.

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
7. a correction that accumulates unchanged history;
8. a declared read set whose anchor spans omit an assigned obligation;
9. a priority silently promoted, demoted, or dropped, missing Must coverage,
   or Won't scope included;
10. a handoff the next role cannot act on without inventing policy,
    reconstructing stale authority, or waiting on an unallocated prerequisite,
    or evidence demanded at the wrong stage; or
11. a consequential guarantee with no owner and evidence — an unsupported
    assumption presented as established.

A finding cannot create a commitment: before blocking, name the binding
obligation or real defect the artifact violates; a demand for a Should/Could
feature, a stronger unspecified guarantee, or Won't scope is at most a
non-blocking suggestion. Inspect what each dependency supplies and its inherited
consequences, and the strongest boundary cases, in the first review. Repeated
labels, IDs, short task intents, exact action-boundary values, and clearly
marked quotations are allowed.
