# 155 - Spec Reconciliation Tested at Review

## Status

ACCEPTED — implemented 2026-09-21; backfilled 2026-09-21.

## Context and Problem Statement

Specs went stale unless someone asked for them by hand, even though three
instructions already covered them: the Doc Impact category maps behavior to specs,
the Spec & TODO trigger asks for a spec before implementation, and MAS task
decomposition makes doc updates a first-class separate task. The instructions were
not missing; their tests were scoped wrong.

The Doc Impact escape hatch asked an additive question only — whether a documented
sibling implies this feature needs the same treatment. It never asked whether an
existing document describes the behavior the change *alters*, so a change to
documented behavior could pass as "none" with the spec left contradicting the code.

That gap had no observer either. The declaration was self-declared at DoR and
self-confirmed by the same agent at DoD, and neither `code-review` nor `spec-review`
compared a diff against existing specs.

## Commit-Evidenced Intent

The commit frames the problem as mis-scoped tests for instructions that already
existed, and identifies the missing observer rather than a missing rule.

## Considered Options

1. **Add a new rule requiring spec updates** — a fourth instruction over the three
   already present.
2. **Widen the Doc Impact question and add the missing observer at review.**
3. **Pin documentation lag to blocker severity** — make a stale spec always reject
   the change.

## Decision Outcome

Chose **Option 2**, in two parts.

The Doc Impact declaration asks both questions — whether a documented sibling
implies the same treatment, *and* whether an existing document describes the
behavior this change alters — and either one falsifies "none".

The review checklist gains the test of that declaration, beside the
operational-surface check that stops short of specs. The Change Summary already
hands the reviewer the declared doc impact and whether each item appears in the
diff, so the missing piece was the instruction to test it. The code-reviewer role
loads this skill, so one line binds both Pairing and multi-agent mode.

Severity stays with the reviewer rather than being pinned to blocker. The reviewer
judges whether "none" is *defensible*, not whether it is correct.

## Rationale

Option 1 adds an instruction where three already exist; the observed failure was
not an agent unaware of the rule but an agent passing a test that asked the wrong
question.

Option 3 is the over-correction the commit names explicitly: every documentation
lag rejecting a change would make the reviewer the bottleneck for edits that are
otherwise correct. Judging defensibility catches careless declarations while
leaving a diligent-looking miss to ordinary reviewer judgment.

Raising the author's standard without adding an observer would have repeated the
original defect in a new place — the declaration would still be self-confirmed by
the agent that made it.

## Consequences

- A change to documented behavior can no longer pass Doc Impact as "none" without
  the reviewer having a stated basis to disagree.
- Reviewers acquire a spec-comparison duty, so review of behavior changes now
  includes reading what the specs currently claim.
- Severity is reviewer judgment, so enforcement strength varies by reviewer; this
  is deliberate, and it means the mechanism catches carelessness rather than
  guaranteeing spec freshness.
- MAS task decomposition still gates its separate doc task on user-visible
  behavior, which this record does not address.

## Evidence and Reconstruction

Source: commit `d12866d8b`, which states the mis-scoped test, the missing observer,
the rejected blocker-severity option and the scope it leaves untouched. No separate
user intent was supplied; alternatives are reconstructed from the commit's own
reasoning.

---
*Reconstructed from commit `d12866d8b` (2026-09-21).*
