# 153 - Documentation and ADR Conventions Elicited on a Blank Repository

## Status

ACCEPTED — implemented 2026-09-21; backfilled 2026-09-21.

## Context and Problem Statement

A greenfield run shipped undocumented repositories with every gate green. Neither
`goal-writing` nor its rubrics mentioned documentation at all, and nothing
downstream compensated: no pipeline role writes docs, so documentation reaches the
repository only as a story's acceptance criteria, and those trace back to the goal
document.

CORE's Doc Impact backstop is inert in exactly this case. "None" requires that no
sibling be documented — which, on a blank repository, is true of every story,
forever.

## Commit-Evidenced Intent

The commit names an observed greenfield run whose output was undocumented while
every gate passed.

## Considered Options

1. **Strengthen the Doc Impact backstop** — make "none" harder to claim.
2. **Add a documentation-writing role to the pipeline.**
3. **Elicit the documentation surface and ADR convention at goal time**, and check
   it at the readiness gate.

## Decision Outcome

Chose **Option 3**. When nothing exists on disk, `goal-writing` elicits which
documentation the product ships and who reads it, and whether the project keeps an
ADR record.

The first is framed as a deliverable with an audience rather than a file to create,
so it is attackable in phase 2 and yields acceptance criteria; the second is a
checkbox. Both sit under the ADR-candidate paragraph, so the existing boundary
holds: the human owns the convention, the architect still owns which decisions
become records.

The matching criterion is added to the readiness gate, which is where pass/fail
lives; without it, phase 4 would certify a document whose documentation surface it
never examined. The criterion self-suppresses where a convention already exists on
disk, and the gap reports as a note rather than a blocker.

## Rationale

Option 1 cannot reach this case: the backstop's escape hatch is satisfied by an
empty repository, so tightening the question leaves the greenfield hole open.
Option 2 adds a role to the pipeline for something the goal document can settle
once, and would still need the goal to say what the documentation is for.

Eliciting rather than defaulting keeps the human as the owner of the convention,
consistent with [ADR-0132](0132-human-owned-goal-decisions.md). Reporting as a note
rather than a blocker keeps the gate from refusing goals for projects that
legitimately ship no documentation.

## Consequences

- A greenfield goal that omits documentation is visible before any agent runs.
- Runs against already-documented repositories are unaffected, because the
  criterion self-suppresses when a convention exists on disk.
- The gap is a note, so a goal can still proceed undocumented by choice — the
  decision becomes explicit rather than accidental.
- Documentation still reaches the repository only through story acceptance
  criteria; this record changes where the requirement originates, not who writes
  the docs.

## Evidence and Reconstruction

Source: commit `9606449a3`, which names the greenfield run, the inert backstop and
the placement decision. No separate user intent was supplied; alternatives are
reconstructed from the commit's own reasoning about why the backstop cannot cover
this case.

---
*Reconstructed from commit `9606449a3` (2026-09-21).*
