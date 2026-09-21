# Cluster 0153 - Documentation and ADR Conventions Elicited on a Blank Repository

## Status

ADR generated: [0153](../0153-greenfield-documentation-conventions.md). Selection approved on 2026-09-21; no separate user intent supplied.

## Commit Set

- `9606449a3` — feat(skills): elicit documentation and ADR conventions on a blank repo

Earliest author timestamp: 2026-09-21T11:36:35+02:00. Decision implementation range: 2026-09-21.

## Reconstructed Decision

`goal-writing` elicits, when nothing exists on disk, which documentation the product ships and who reads it, and whether the project keeps an ADR record — framed as a deliverable with an audience rather than a file to create. The readiness gate gains the matching criterion, self-suppressing where a convention already exists, reporting as a note rather than a blocker.

**Rationale provenance:** Commit-evidenced. The body states the greenfield run, why CORE's Doc Impact backstop is inert on a blank repository, and why the placement preserves the human-owns-convention / architect-owns-records boundary.

## Evidence

- `9606449a3` commit body, read in full.
- The inert-backstop argument: "none" requires no sibling be documented, which on a blank repo is true of every story, forever.

## Related Decisions

ADR-0132 (human-owned goal decisions) supplies the ownership boundary this preserves; ADR-0154 supplies the architect-side consumer for ADR candidates.

## User Context — 2026-09-21

None supplied.

## Remaining Historical Gaps

No record of whether adding a documentation-writing pipeline role was considered. The choice of note over blocker is stated but its discussion is not preserved.
