# 123 - Two-Sided Review with Bounded Convergence

## Context and Problem Statement

Review guidance originally addressed reviewers, while an author's response to
rejection was effectively "read, fix, resubmit." The August implementation
records non-convergent loops: each corrective pass answered a finding while
creating the surface for the next one. July changes had already introduced
finding reconciliation and bounds on later-round blockers.

Real reviews also showed doers fixing false-positive findings and entering
cycles where fixing A broke B and fixing B broke A. Authors lacked a defined
way to challenge a remedy that caused greater harm. Simply repeating a finding could keep the loop alive, and correctness
alone could approve a change whose accumulated complexity outweighed its value.

## User-Confirmed Intent

On 2026-09-06, the user supplied this historical intent: Real reviews showed doers fixing false-positive findings and non-converging cycles where fixing A broke B and fixing B broke A.

Whether the July and August changes arose from the same incident was not specified; their grouping remains the approved backfill grouping.

## Considered Options

These comparisons are reconstructed:

1. Retain reviewer-only guidance and require authors to implement each remedy.
2. Limit review effort by narrowing inspection in later rounds.
3. Define both sides of the exchange, bound corrective scope, and route unresolved
   disagreement to the authority that can change the task.

## Decision Outcome

Choose **Option 3**. The code-review skill governs both raising findings and
answering them. Contracts retain ownership of state transitions and permissions;
the skill defines response vocabulary and its communication channels.

| Event | Required response |
|-------|-------------------|
| Finding received | Minimal fix, fix with recorded objection, or concrete-harm contest |
| Contest received | Accept, Counter, Refute, or Escalate; never bare restatement |
| No consensus | Human Decision Request in Pairing; doer `mark-blocked` for orchestrator rescope in MAS |
| Reviewer recommends reframing | Address the decision itself rather than silently starting another corrective pass |

Every round inspects the complete change set for P0-P2 defects. After the first
round, only unresolved prior findings and new P0-P2 defects may block on that
basis; new P3-P6 observations go to follow-up. Undeclared corrective growth and
harmful vestigial code remain findings. An independent reviewer starts its own
first round rather than inheriting another reviewer's approval.

Corrective work stays within the findings and relevant tests unless growth is
declared and justified. Reviews reconcile prior findings and track both remaining
findings and file-count changes. Accumulated work receives a vestigial and
proportionality sweep from round three or after a design change.

Approval assesses net value both as submitted and with findings resolved. A
fixable defect merits correction; marginal or negative value even after fixes
requires reframing and an explicit decision.

## Rationale

Bound the change set without narrowing inspection. A contest forces both sides
to expose evidence, while escalation gives a broken premise somewhere to go.
Separating submitted value from resolved value prevents ordinary repairable
defects from becoming arguments for abandonment. Real reviews prompted that
distinction and the explicit receiver obligation for Decision Requests.

## Consequences

- Authors can challenge harmful remedies without ignoring review feedback.
- Full critical-defect inspection remains mandatory while lower-severity novelty
  cannot indefinitely expand a continuing review.
- Follow-up observations must be recorded rather than lost.
- Convergence depends on protocol compliance; this does not add a new Go verdict
  state or grant reviewers authority to block tasks directly.

## Related Decisions and Provenance

Extends peer supervision in [ADR-0001](0001-leverage-proven-contract-for-mas.md)
and works within [ADR-0020](0020-explicit-task-workflow-contract.md). The user
approved grouping the July and August changes on 2026-09-06; the motivating review failures were subsequently confirmed above. Evidence is the commit bodies, CORE
Rule 12, and the code-review skill's author, transition, and re-review sections.

---
*Reconstructed from commits 3f8c4a17, ff603810, caf3f364, and 1de50e0d (2026-07-30 to 2026-08-09).*
