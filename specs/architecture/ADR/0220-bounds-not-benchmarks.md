# 220 - Bounds, Not Benchmarks: Unnamed Assurance Is the Human's Decision

## Status

ACCEPTED — implemented 2026-09-27; backfilled 2026-10-25.

## Context and Problem Statement

Runs resolved open conditions to their strictest reading and grew machinery
nobody asked for:

- An approximate load NFR became a zero-think-time saturation benchmark (D96).
- A 30-minute outage bound grew release gates, canaries and receipts (D97).
- A 48-hour offline expiry grew clock-tamper resistance (SERA OP-010).
- DEV-239 accumulated about 61k lines of recovery, canary, receipt and evidence
  frameworks.

The existing "do not invent" rules did not fire, because each elaboration still
traced to a source line. The defect was not invention but escalation: a bound was
read as a target to be proven at maximum strength.

## User-Confirmed Intent

The user confirms that moving the decision to the human was the intended outcome,
not an interim step, and gives the reason: benchmarks tend to be overkill in
common situations.

## Considered Options

1. **Rely on the existing "do not invent" rules** — the status quo, which every
   example above passed.
2. **Let the author keep unnamed machinery once it names an outcome that fails
   without it** — the first version of the rule (`c1619f04f`).
3. **Make machinery the source does not name the human's to add.**

## Decision Outcome

Chose **Option 3**, superseding Option 2 the same day.

The shared reference-first authoring rule "Bounds, not benchmarks" states:

- A source bounds the solution; it need not specify it. Refining inside a clear
  bound belongs to later stages; moving or tightening it belongs to the human.
- Take the least costly reading that satisfies the source's words and framing,
  never the strictest.
- A guarantee, threat model or supporting machinery (recovery, telemetry,
  canaries, evidence frameworks) the source does not name is the human's to add,
  even with a citation. Leave it out, or raise an Open Question that states the
  outcome it protects and its build and proof cost. Only a recorded human decision
  puts it in scope.
- Once the required outcome is demonstrated, further assurance is optional unless
  a concrete defect or binding obligation requires it.

The rule reaches each authoring stage:

| Stage | Change |
|---|---|
| spec-review | Class 9 rejects bounds tightened or obligations added past their source |
| architecture-planning, user-story-writing | Refine inside inherited NFRs, never tighten them; stories state acceptance criteria and never originate NFRs |
| check-input-readiness | NFRs need clear bounds, not full detail; a Proportionality criterion separates needs from wants, puts a ceiling on required evidence, and blocks on overkill only when it materially drives cost |
| goal-writing | Challenge wants and overkill targets with the human |
| code-planner | The same rule inline, with BLOCKED as the route to the human |

## Rationale

Option 2 left the author as the judge of necessity. Any added machinery can be
given an outcome that "fails without it", so the rule filtered nothing, and the
code-planner never read the shared contract, so plans were not covered at all.
Putting the decision with the human removes the judgement the failures came from,
rather than asking for a better one.

Taking the least costly reading is the default because, as the user puts it,
benchmarks tend to be overkill in common situations. When a strict reading
is right, the human can say so at the cost of one Open Question. A wrong strict
reading cost DEV-239 tens of thousands of lines.

## Consequences

- Authors stop growing assurance frameworks from approximate bounds; required
  outcomes are proven once, at the bound.
- More Open Questions and BLOCKED routes reach the human. That is the intended
  cost of the decision.
- A guarantee the human would have wanted but did not name is left out unless
  someone raises it, so omission risk moves from build cost to an unasked question.
- Spec review can now reject a tightened bound as a defect rather than accept it
  as diligence.
- The added lines count against the per-role instruction budget (ADR-0149); the
  code-planner baseline grew by 229 bytes.

## Related Decisions

ADR-0150 (owner of the shared authoring reference this rule joins), ADR-0132
(human-owned goal decisions), ADR-0149 (instruction budget).

## Evidence and Reconstruction

Sources: `c1619f04f` and `5704e5c83`, read in full, with the cited runs (D96,
D97, client demo OP-010, DEV-239), and the current text in
`skills/shared/references/reference-first-authoring.md`. The user confirmed the
intended outcome and its rationale on 2026-10-25.

---
*Reconstructed from commits `c1619f04f`..`5704e5c83` (2026-09-27).*
