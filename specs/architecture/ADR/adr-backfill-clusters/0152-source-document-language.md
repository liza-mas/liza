# Cluster 0152 - Source-Document Language Propagates to Generated Specs

## Status

ADR generated: [0152](../0152-source-document-language.md). Selection approved on 2026-09-21; no separate user intent supplied.

## Commit Set

- `89d978aed` — feat(skills): follow source-document language in generated specs

Earliest author timestamp: 2026-09-21T11:30:09+02:00. Decision implementation range: 2026-09-21.

## Reconstructed Decision

One Constraints bullet in the three spec-authoring skills: write in the language of the assigned source material, keep identifiers verbatim, do not switch language mid-document. Because a story's assigned source is its epic, the choice propagates down the chain without a second rule. Architecture plans stay English.

**Rationale provenance:** Commit-evidenced. The body states the observed run, why a `state.yaml` field would hold a value that does not vary independently, and why skills rather than templates cover both modes from one place.

## Evidence

- `89d978aed` commit body, read in full.
- The body records that every prior mention of "language" in skills, contracts and templates refers to a programming language or a register — so the output language was a model default filling a gap.

## Related Decisions

ADR-0150 (shared authoring rules, the other multi-skill constraint added the same week), ADR-0153.

## User Context — 2026-09-21

None supplied.

## Remaining Historical Gaps

Whether the architecture-plan exception was debated is not recorded. The commit itself flags two open items: spec reviewers are not told to accept a non-English artifact, and the propagation consequence is undocumented for the human writing the goal.
