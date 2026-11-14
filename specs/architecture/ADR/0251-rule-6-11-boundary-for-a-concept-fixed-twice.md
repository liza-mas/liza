# ADR-0251: Rule 6/11 Boundary for a Concept Fixed Twice

## Status

ACCEPTED — 2026-11-14. Bug prevention discipline, FR4 (a concept fixed twice
stops the process). The record and its query are owned by
[ADR-0250](0250-prevention-record-format-and-vocabulary.md); this ADR records
the check, the hold, the route and the Rule 6/11 boundary built on them. The
rules themselves live in CORE under *Rule 2: Definition of Ready (DoR)*,
*Rule 3: Definition of Done (DoD)*, *Rule 6: Scope Discipline* (Refactoring
Discipline), *Rule 11: Root Cause Analysis (RCA) Before Symptoms* and
*Execution State Machine* (Stop Triggers).

## Context

FR4 asks that when a fix lands on a concept within 14 days of another fix on
it, the next change on that concept is designed before it is coded. The goal's
account of why the process stops at the instance names the tension: Rule 11
says to fix the cause, Rule 6 says to solve the problem and stop, so an agent
that finds the shape recurring has no sanctioned way to remove it, and a
refactor mixed into a fix is the very scope creep Rule 6 forbids. Without a
boundary the two rules are read case by case, and the recurrence is fixed a
third time.

Constraints that shape the decision:

- MC3: no engine-tracked concept state. The only memory is the commit history
  and the prevention record of ADR-0250, readable in any git repository.
- FR4 binds the *next change* whatever its type: a feature or refactor that
  touches the concept is as exposed as a fix, but makes no RCA, so the check
  cannot live in the fix's RCA.
- NFR Cost (Must): the prevention step of a fix adds no tool call.
- The second fix cannot see its own commit and must not be held: an urgent fix
  is not blocked (goal failure modes).
- ADR-0250's I1 stands: the dispositions `removed`, `scheduled` and
  `accepted` keep their meaning, and in multi-agent mode an agent never
  records `accepted`.

## Decision

### D1 — The check, the hold and the route

1. **Terms.** A concept is *fixed twice* when a fix lands on it within
   14 days of another fix on it. The *next change* is the first change — fix,
   feature or refactor — that touches it after that. The 14-day window is
   contract text in CORE's Rule 2, provisional, and recalibrated by the human at
   a retrospective, not configuration and not engine state.
2. **The check runs in CORE Rule 2, for every change, before its code.** In one
   shell call the change runs ADR-0250's concept query from the epoch to now,
   passing every concept it touches (each file it will edit as its path token,
   `path#symbol` for each symbol, every earlier token the boundary had, and the
   invariant IDs its invariant check names), plus the marker list below. For
   each concept, with A and B its two newest listed fixes (B newer), the change
   is *held* on that concept when B is within 14 days after A, B is not marked
   with A, and no commit is marked with B. Each concept is decided on its own.
   The pair is read from the query's SHA and date fields, never from a
   disposition.
3. **The design marker.** A change held on a concept lands with the commit
   trailer `Fixed-Twice-Design: <concept> <full SHA of that concept's newer
   fix>` in its final trailer block, one trailer per held concept. It releases
   that concept's pair only: a marker on X never releases Y, even when one fix
   is the newer fix of both. It is not an ADR-0250 key (no `Prevention-`
   prefix), so the submit gate's validator ignores it and a fix commit may
   carry both. A change that comes from a design carries a marker for each held
   pair it touches that the design covers. A fix within 14 days after a
   design-routed fix not marked with that fix is a new pair: the shape recurred
   after the design.
4. **The hold lasts until a marked change lands.** It does not expire with
   time. The second fix, which cannot see its own commit, lands as any fix and
   records its own evidence-based disposition, never one forced by the check;
   the hold falls on the change after it.
5. **Mode behavior.**
   - *Multi-agent:* the check rides `write-checkpoint`. The agent marks its
     task BLOCKED before code, with a `blocked_reason` starting `fixed-twice:`
     and one `<concept> <older SHA> <newer SHA>` entry per held concept. The
     orchestrator's blocked wake, per entry, looks for an architecture task
     whose description starts `fixed-twice:` and names that `<concept>
     <newer SHA>`; it awaits every open one found, commissions one
     `architecture-pair` task naming the uncovered pairs, awaits it, and
     unblocks naming the designs once all have merged. The resumed agent
     follows them and lands with one marker per held concept.
   - *Pairing:* the approval request states the hold; the Software
     Architecture Protocol runs inline before the change's code, or the human
     waives it (the waiver noted in the commit body); the commit carries one
     marker per held concept.
   - *Subagent:* stops before code and returns the hold with its prevention
     answer to its caller, which applies its own mode.
6. **The commissioned design completes on a reviewed design.** Its done
   condition is a reviewed design for the shape on those concepts, merged, that
   allocates the shape's removal, or the decision on it with its owner, to its
   code plan. Implementation and its proofs belong to the coding descendants
   the architecture-to-code-plan transition generates. Held changes wait only
   for that merge, never for coding or for the human.
7. **The lookup is by concept and newer SHA.** Both the held change's reason
   and the design's description carry the `<concept> <newer SHA>` pair; a
   design covers exactly the pairs its description names, so every held change
   on that pair, including one arriving after the design merged and before a
   marked change lands, finds the design and never reuses it for another
   concept. A change held on several pairs waits for a design covering each.
   No record owner is consulted, so human-owned records need no resolution.

### D2 — Where Rule 6 ends and Rule 11 begins

Rule 6 keeps refactoring as a distinct task. Under the fixed-twice stop
trigger the design-routed change *is* that distinct task, so removing the
shape — the refusing boundary, its negative test and the sibling paths with
their dispositions — is its own intent, not scope creep. Before the trigger, a
fix may refuse the shape at the root cause's shared boundary (Rule 11 already
prefers it); removal beyond that boundary is scheduled, not done in the fix.
Rule 11 owns the prevention answer; Rule 6 owns scope. The boundary sentence
lives in Rule 6's Refactoring Discipline line only, naming the fixed-twice stop
trigger and counting *removing the shape* as in scope under it; no other site
restates it.

### D3 — The check rides a call every change already makes

The check's two commands run in the same shell invocation as an existing
call, after that call's command:

| Change | Call it rides | Why that call always exists |
|---|---|---|
| Any multi-agent change, FAST PATH included | `write-checkpoint` | Submission is rejected without a checkpoint; FAST PATH keeps a reduced one |
| A fix in Pairing or subagent mode | Minimal RCA's sibling search, run as one shell search | Rule 11 requires inspecting sibling paths before patching a code bug |
| A non-fix change in Pairing or subagent mode | Its first shell read or search of a file it will edit | Reading before editing is Rule 5 |

The prevention step of a fix therefore adds no tool call in any mode or path.
The last row adds one call when that read is not a shell call; it is not a
fix's prevention step, so NFR Cost does not bind it, and the goal ranks the
core loop (FR4) above that per-run cost.

## Consequences

- No engine state: the hold is derived from commit history and the marker, so
  it works in any git repository (G1.1) and survives restarts.
- ADR-0250 is untouched: the check reads fix SHAs and dates, never a
  disposition, so the second fix's record stays the fixer's answer, and
  `removed` and task-owned `scheduled` stay standing dispositions. In
  multi-agent mode the agent never accepts; where acceptance would be needed
  it records `scheduled` with owner `human`.
- Cost is one orchestrator turn per held change, normally only the next change
  on the pair. Output added to the ridden call is one line per recorded fix on
  the change's concepts and one per marked commit — usually none.
- The hold lasts until a change marked with the pair lands; an unrelated
  change that does not touch the concept is not held.
- Residuals, each owned. A human's commit carries no record or marker, so a
  human fix is invisible to the check and a human next change leaves the pair
  held for the next agent change (conservative; in Pairing the human waives).
  An agent that skips the check or the marker breaks the contract, and the
  reviewer judges it as FR1's substance. Renames a bridge did not record are
  ADR-0250's accepted misses.
- The 14-day window is provisional; the first retrospective with record data
  recalibrates it by editing the contract text.
- The added call for a non-fix change in Pairing or subagent mode, when it
  occurs, is a per-run cost ranked below FR4 by the goal.

## Alternatives Considered

- **Start the design at the second fix and count it as the next change's
  design**: a non-fix change could land first, and starting a design is no
  evidence the next change passed it.
- **Detect only inside the fix's RCA**: non-fix changes make no RCA, so the
  next change on the concept would escape.
- **Force the second fix to `scheduled` and read the hold from dispositions**:
  narrows ADR-0250's I1 and misstates a real removal.
- **Look the design up by record owner or by date**: the owner is absent on
  `removed`, and chronology is inverted.
- **A design task that must deliver the coding outcome**: it cannot complete
  before its own descendants exist.
- **A fixed lookback for the hold**: a next change after the lookback would
  slip through.
- **An agent-written discovery**: no agent-writable discovery command exists.
- **Hold the second fix**: blocks urgent fixes.
- **Engine-tracked concept state**: forbidden by MC3.
- **In-code hold markers**: stack-specific comments, and invariant concepts
  have no single file.
