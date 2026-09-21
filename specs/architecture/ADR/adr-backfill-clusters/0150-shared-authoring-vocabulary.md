# Cluster 0150 - Shared Authoring Vocabulary: Priority, Commitments, Proof Stage

## Status

ADR generated: [0150](../0150-shared-authoring-vocabulary.md). Selection approved on 2026-09-21 as a single record by user decision; release rationale not supplied.

## Commit Set

- `933fd019c` — feat(skills): W1 shared authoring rules — MoSCoW, commitments, proof stage
- `e3bb91492` — feat(prompts): W2 planner and reviewer handoff duties
- `cee7f87db` — feat(prompts): W3 orchestrator cause-based recovery and delivery reporting
- `38e75d3ab` — feat(skills): W4 operator convergence watch and cause-based recovery
- `20ccc2858` — test(validation): W5a packets, protocol trial, budget gate
- `22d17afc2` — merge: prevent blocked-task chains — instruction release W0–W5a

Earliest author timestamp: 2026-09-18T09:38:21+02:00. Decision implementation range: 2026-09-18.

## Reconstructed Decision

Define the priority/commitment/proof-stage vocabulary once, at one owner (`skills/shared/references/reference-first-authoring.md`), have six skills point at it with one paragraph each, and correct every conflicting instruction in its original location against that definition — reviewer checklists (W2), orchestrator wake templates (W3), and the operator skill (W4).

Cause-based recovery replaces supersession-by-default; closure evidence becomes consumer progress past the old failure point rather than a new task ID.

**Rationale provenance:** Commit-evidenced for the mechanism and the goal mapping. The release-timing decision (D4: merge before W5b, with 24 candidate exercises unrun) is recorded in the merge as a human decision; the user did not supply its rationale when asked.

## Evidence

- Commit bodies for all six commits, read in full.
- Source goal blackboard verified present: `specs/adversarial-pairing/20260913-prevent-blocked-tasks-chains-bb.md`; W5b continues on `...-w5b-bb.md`.
- `933fd019c` records the mandatory-read overruns against the commit-0 anchor: us-reviewer +12.2%, architect +10.4%, architecture- and epic-plan-reviewer +8.0%, us-writer +7.3%, epic-planner +5.1%.
- Grouping note: at least three separable decisions (authoring vocabulary, handoff duties, cause-based recovery) are recorded as one, matching the release and the user's instruction.

## Related Decisions

ADR-0149 (the budget instrument this release is measured against), ADR-0147 (operator notes, consumed by the operator skill), ADR-0123 (two-sided bounded review), ADR-0133.

## User Context — 2026-09-21

One record [grouping decision]. Release rationale: "IDK".

## Remaining Historical Gaps

Why the release shipped before W5b's 24 candidate exercises is unconfirmed — recorded as an open gap, not reconstructed. No alternative-evaluation record for placing the vocabulary at a single owner versus per-skill definitions.
