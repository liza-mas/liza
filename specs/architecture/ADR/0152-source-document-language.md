# 152 - Source-Document Language Propagates to Generated Specs

## Status

ACCEPTED — implemented 2026-09-21; backfilled 2026-09-21.

## Context and Problem Statement

A run whose input document was written in another language produced epics and
stories in English. Nothing in the skills, contracts or prompt templates said
anything about the natural language of an artifact — every existing mention of
"language" refers to a programming language or to the plain/technical register — so
the output language was a model default filling a gap, and the resulting mix was
invisible to every gate.

## Commit-Evidenced Intent

The commit records an observed run, not a hypothetical: the input was in another
language and the generated artifacts were not.

## Considered Options

1. **A `state.yaml` configuration field** — let each project declare its artifact
   language.
2. **One Constraints rule in the spec-authoring skills** — derive the language from
   the assigned source material.
3. **A rule in the prompt templates** — set the language at render time.

## Decision Outcome

Chose **Option 2**. The three spec-authoring skills — `detailed-spec-writing`,
`epic-writing`, `user-story-writing` — gain one Constraints bullet: write in the
language of the assigned source material, keep identifiers verbatim, and do not
switch language mid-document.

Because a story's assigned source is its epic, the choice propagates down the chain
from the goal document without a second rule.

Architecture plans stay English: they are read by the Coder, not by the intent
owner.

## Rationale

A config field (Option 1) would hold a value that does not vary independently — the
policy derives from the input document, so the field would either duplicate what
the source already says or contradict it. Placing the rule in the skills rather
than the templates covers Pairing and multi-agent mode from one place, since both
load the same skills.

Deriving from the *assigned* source rather than from the goal document is what
makes one rule sufficient for the whole chain.

## Consequences

- Artifacts follow their source's language without per-project configuration.
- Identifiers stay verbatim, so cross-references survive a non-English document.
- Spec reviewers are still not told to accept a non-English artifact.
- The propagation consequence is undocumented for the human writing the goal
  document: choosing the goal's language silently chooses the language of every
  downstream epic and story.
- Architecture plans remain English by exception, so a chain in another language is
  mixed by design at that one stage.

## Evidence and Reconstruction

Source: commit `89d978aed`, which states the observed run, the chosen placement and
the two consequences left open. No separate user intent was supplied; the options
above are reconstructed from the commit's own comparison of placements.

---
*Reconstructed from commit `89d978aed` (2026-09-21).*
