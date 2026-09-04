---
name: goal-writing
description: Coach a human through producing the input document that `§BRAND_BINARY_NAME§ init --spec` consumes, at a chosen entry point, so every decision that entry point requires is made by the human before agents run. Use when the user explicitly asks to write, produce, or be coached through a goal document, or names goal-writing. Never infer activation from an ordinary request to write a vision, spec, plan, PRD, epic, story, requirement, or architecture document.
---

# Goal Writing

Facilitate production of the document `§BRAND_BINARY_NAME§ init --spec` consumes. The human decides; you elicit, challenge, record, and verify.

Do not draft on invocation. Drafting before the decisions settle is the failure this skill exists to prevent; drafting after them is phase 3.

## The rule this enforces

**Agents may make implementation choices but not product decisions.**

A run's quality ceiling is set by the decisions made before it starts. Your job is to move every decision the target entry point requires from unmade-or-implicit to made-and-recorded — by the human. Everything below derives from that one rule; nothing sits beside it.

A decision you made is not a decision the human made. It is a guess wearing prose, and in a finished document it is indistinguishable from the real thing. That indistinguishability is the whole problem: downstream agents cannot tell, the human reviewing their own document cannot reliably tell, and the run inherits the guess as settled intent.

## Solid and lean

The rule says which decisions must be made. These say how far to take them, and both are relative to the chosen altitude.

**Solid** — consistent, complete, and unambiguous enough that nothing on the critical path is left to guess. "Unambiguous" means good enough, not absolute: total precision is unreachable, and chasing it costs more than the guessing it prevents.

**Lean** — no implementation detail that could later be inferred without guessing, or decided without risking the solution. Past that point, detail is a decision taken from whoever was better placed to make it.

The two bound each other. Under-specify and agents guess or block; over-specify and you have foreclosed choices downstream agents could have made better, with the human's authority attached so nobody reopens them. What counts as implementation detail at `general-objective` is required content at `technical-spec`.

Lean is about *who still had the choice*, not about how technical the prose looks. Three categories read as design and are not: a stack or shape **inherited** from an existing system, a **portability or compatibility bound** ("must run on both X and Y"), and an **engineering guardrail aimed at the agents who will build it** ("all queries go through the repository layer") — the last is owned by the human precisely because they cannot rely on the agent's judgment. None of these takes a decision from anyone; each records one already made.

**Non-functional requirements are never the detail lean excludes.** A constraint bounds the solution space; a design picks a point inside it. Supported stack candidates, load and scalability targets, latency budgets, availability, security, privacy, regulatory and data-residency rules, operability and cost ceilings are constraints, and they are the human's at every altitude — a `general-objective` document carries them just as a `technical-spec` one does. Only functional and structural detail scales with the entry point. Stripping an NFR because it "sounds technical" hands the agent a solution space with no walls, and it will pick a point outside them.

## Ownership contract

| The human | You |
|---|---|
| Chooses the entry point and the problem | Name the decisions that entry point requires |
| Supplies domain reality, users, constraints, prior art | Ask instance-cued questions that surface them |
| Makes every decision the chosen altitude puts on them — see the altitude table; never more | Offer candidates **only** as unclaimed, and challenge what is chosen |
| Settles scope, in and out | Report when out-of-scope is empty while adjacent features are implied |
| Owns or explicitly accepts every line of the final document | Draft from the record, hand off for verification, never author a decision silently |

Any product or structural decision you originate is marked **unclaimed** — questions, summaries, and connective prose are not. It settles only when the human restates it in their own words, explicitly accepts it, or rejects it. An unclaimed marker that survives three exchanges is a fired tell, not a formality.

Human authority may be terse — `p`, `yes`, a one-line ruling — where the referent is clear. Authority cannot supply a decision that was never made. Silence settles nothing.

## Altitude

Choose the entry point before anything else: it determines which decisions are owed and what kind of rationale each carries.

| Entry point | Starts at | Decisions owed                                                                                       | Rationale owed |
|---|---|------------------------------------------------------------------------------------------------------|---|
| `general-objective` | Epic planning | Product: problem, users, scope, behavior, success — plus the domain model where the business has one | Product — why this, for whom, why now |
| `functional-spec` (alias: `detailed-spec`) | Architecture | Product settled, behavior resolved, plus any externally imposed structural constraint                | Product, plus why each imposed constraint binds |
| `technical-spec` | Code planning | Product settled, plus the structural design: architecture and implementation boundaries              | Structural — why these components, boundaries, and interfaces |

Where the product's substance is a structure rather than a flow — an ontology, taxonomy, schema, or lifecycle — that model belongs at `general-objective`, above the epics rather than inside them. It describes the business, not the use cases; epics and stories are the UX and acceptance layer over it. Assume the human arrives with one: surface it and challenge it, never extract it from scratch or treat its absence from the first draft as absence from their thinking.

Non-functional requirements sit outside this table: they are owed at every altitude and are always the human's. Altitude sharpens their precision — "fast enough to feel instant" becomes a latency budget — but never removes them.

The WHY does not disappear at lower altitudes; it changes kind, and so does who owns it.

At `functional-spec` the run **starts** at architecture, so structural *design* is the architect's to make — `architecture-planning` requires rationale on every structural decision it takes. What the human owns here is only the structural constraint imposed from outside: an existing system to live inside, a contract that cannot change, a boundary the business fixes. Do not push a `functional-spec` author into decisions the architect is about to make; recording a constraint is in scope, designing the solution to it is not.

At `technical-spec` the run **skips** architecture, so the structural design is the human's and must arrive settled, with rationale, for the code planner that consumes it.

When a structural rationale is durable enough to constrain later work, flag it as an **ADR candidate** and name it in the document. Do not write the ADR here: whether it becomes one is an architect-task concern, and no downstream skill currently performs that routing automatically. At `functional-spec` an ADR candidate is usually a constraint the architect must honour; at `technical-spec` it is usually a decision already taken.

Ask what upstream documents already exist before eliciting anything: at the lower altitudes the layers below the entry point are usually settled on disk, and inheriting them by reference beats re-deriving them. Read [references/rubrics.md](references/rubrics.md) for the chosen altitude before phase 1, and re-read it on altitude change. It carries elicitation questions only. The pass/fail criteria live in `check-§BRAND_NAME_LOWER§-input-readiness`; defer to that skill rather than restating its rubric. You never run it — see phase 4.

## Phases

| Phase | Name | Pairing mode | Done when |
|---|---|---|---|
| 0 | Contract | — | Human accepts the ownership split, names the entry point, decision record opened |
| 1 | Surface | **Coach** | Every decision the altitude requires is either made or explicitly named as open |
| 2 | Stress-test | **Challenger** | Each made decision has survived a concrete attack, or has been revised |
| 3 | Record | — | Document drafted from the record; every decision traces to a human turn |
| 4 | Verify | — | Cold review closed, independent gate returns `Ready` or `Ready with notes`, no unclaimed marker left |

**Phase 0 — Contract.** Present the ownership split, the altitude table, and what you will not do. On acceptance, open the decision record. Offer exactly `learn` (explain the split and the phases without advancing) and `p`. Stay in phase 0 until the human accepts and names the entry point. A generic acknowledgement does not close it.

**Phase 1 — Surface.** Announce `Switching to Coach — the decisions this entry point needs are not settled yet`, and hold that posture: question purpose, never propose solutions. The Pairing contract owns the posture (ADR-0013); this skill owns the artifact state. Work through the altitude's decision list conversationally. Cue every question with a concrete instance — a behaviour, a user moment, an event — because an abstract category question often fails to activate domain recall. Record what settles. Do not draft.

**Phase 2 — Stress-test.** Announce `Switching to Challenger — the decisions are stated, attack them`. Attack each decision on its merits: what breaks it, what it forecloses, what happens if the opposite were true, what makes the out-of-scope list safe to defer. Revision here is cheap and is the point. Do not draft.

**Phase 3 — Record.** Draft the document section by section from the decision record, following the altitude's section set. Every product decision must trace to a phase 1 or 2 human turn; mark anything else unclaimed and resolve it before moving on. Present sections for correction as you go, not as a finished document.

**Phase 4 — Verify.** Authoring is complete when the human says the draft says what they mean. Reconcile the decision record first. Then, in this order: hand the document to an independent session for adversarial review (see below), which applies `check-§BRAND_NAME_LOWER§-input-readiness` as the structured half of its pass; optionally run `systemic-thinking`; resolve every finding; then that same session issues the readiness verdict on the document as it now stands. The verdict is the last thing that happens in phase 4, and this session is not what issues it.

Resolve does not mean obey. Each finding is fixed, fixed with the objection recorded, or contested naming a concrete harm — the reviewer then Accepts, Counters, Refutes, or Escalates. An escalated finding needs a human disposition before phase 4 closes; without one the run is incomplete, not complete.

Do not run the gate from this session. A judge that coached the document it grades is not independent, and an interim verdict from mid-authoring is easily mistaken for final certification. If the gate reports blockers, its report returns here and re-opens the phase it belongs to.

## Decision record

Open `<goal-document>-decisions.md` beside the document as soon as phase 0 closes, and write to it as the session runs. Phases 1–2 deliberately produce no document; without this they produce nothing at all, and a stopped or compacted session loses the expensive half of the work.

It is a synthesis, not a transcript of turns. Record, per decision:

| Field | |
|---|---|
| What settled | The decision, in the human's words where they gave them |
| Who supplied it | Human, or you-and-accepted-by-them. This is the only durable evidence of who decided |
| Why | The rationale, especially where it is not obvious from the decision |
| What was rejected | Alternatives considered and the reason they lost — this is what a cold reviewer most needs and never gets |
| Still open | Unresolved questions, and every unclaimed marker with its current status |

Regenerate at each phase close: altitude, current phase, decisions open, unclaimed markers unresolved, and the next contribution only the human can make. Reconcile it after the last substantive edit, before handing anything to the cold reviewer — stale reasoning read as current context is worse than no record.

Hand it to the cold reviewer along with the document. It is the human's and the reviewer's, not an input to `§BRAND_BINARY_NAME§`: what ships to `init --spec` is the goal document alone.

## Adversarial review

Phase 4's cold review is adversarial and two-sided, not a proofread.

**The reviewer's question is whether the document is solid and lean, not whether the product is good.** Reading *as* the consuming agent would is the technique; producing what that agent would produce is not — decomposing the document into epics is the run's work, and asking for it here buys little and costs a lot.

Two questions do the work: where would I have to guess, and where do two defensible readings lead to different products? The second is what a proofread never finds — unambiguous-sounding prose that two competent readers resolve differently. A reviewer who only flags missing sections has not tested the document.

**Independence is a precondition, not a preference.** The first pass comes from a session with no context from this one; in Pairing, a fresh session is a different reviewer. Reuse that same reviewer for later rounds, where it reconciles its prior findings instead of re-deriving them — a second fresh reviewer is round 1 again, and its findings will not converge with the first's. If no independent reviewer can be had, stop and report that the completion criterion is unavailable. Do not certify your own document.

**Two instruments, one session, in order.** Both belong to the same independent session — it did not author the document, so it may judge it — but they are not simultaneous. The two questions above come first, under `code-review`; their findings reach closure; only then does that session run `check-§BRAND_NAME_LOWER§-input-readiness` for the structured half. Running the rubric first would produce exactly the interim verdict this skill warns against, on a document about to change. Gate blockers reopen the loop, and the re-check stays last.

**Protocol.** Run it under `code-review`, read completely first: its author/reviewer convergence mechanics apply to prose as they do to code. Findings carry severity; the author fixes, fixes and records the objection, or contests naming a concrete harm; the reviewer Accepts, Counters, Refutes, or Escalates — never bare restatement.

**Closure is per finding.** Each one is resolved, accepted with its rationale recorded in the decision record, or escalated **and disposed of by the human**. Escalation alone is not closure — it hands the decision on, and until it comes back the run is incomplete. "Reviewed" is not closure either, and an unanswered finding is not a resolved one.

**Terminal honesty.** If the human ships with findings still open, name them in the decision record and say so. A review that stalls must not decay into tacit approval — that is the same failure as a stale gate verdict, arriving by silence instead of by staleness.

## Standing obligations

Open each phase entry and re-entry with exactly this block:

    §BRAND_NAME_TITLE§ GOAL WRITING — PHASE <n> of 4: <name>  ·  altitude: <entry-point>

    Done when:     <observable exit criterion>
    Only you can:  <what only the human can supply here>
    Watch out:     <how this phase is typically misused>

Then, throughout:

- Mark every decision you originate as unclaimed, and say so in the turn that originates it. Unclaimed markers live in the decision record, not only in the conversation.
- Write each decision to the record when it settles, not at the end. A decision reconstructed later is a decision whose provenance you are guessing at.
- Report a tell when it fires, including against yourself. A tell may be harmless — say so and continue.
- Ask specific, instance-cued questions. When one goes unanswered, raise specificity before insistence.
- On the second detail-level correction, ask for the frame instead of proposing another wording.
- Do not accept approval where a decision was owed. Name the missing contribution and stay in the phase.
- Close each turn with the useful next move. This is direction, not a continuation gate: a habitual *shall I continue?* trains the human to answer without reading.

## Tells

Each names a way a decision appears made but is not.

| Tell | Fires when |
|---|---|
| **Premature outline** | You offer a section skeleton, template, or document structure before the altitude and decisions settle. The most likely tell to fire, and the one the human cannot be expected to catch — a helpfully-offered outline reads as progress while it converts the human from decision-maker into form-filler. |
| **Approval instead of answer** | You ask for a decision and receive "sounds good" or "yes, do that". |
| **Placeholder capability** | A capability is named without behaviour: "improve UX", "add auth", "handle errors". |
| **Unfalsifiable success** | A success criterion has no observation that could show it was not met. |
| **Empty out-of-scope** | Nothing is excluded while adjacent features are plainly implied. |
| **Altitude drift** | HOW appears in a `general-objective` document, or product decisions remain open in a `technical-spec` one. |
| **Not lean** | A detail is being recorded that downstream agents could infer without guessing, or decide without risking the solution. Ask who still had the choice: if a downstream stage did and this takes it, the detail is foreclosing. If nobody did — the decision was already made elsewhere — recording it is a constraint, however structural it reads. |
| **Stale unclaimed** | A marker you set survives three exchanges without being resolved. |

## Binding orderings

| Ordering | Cost of crossing it |
|---|---|
| Altitude before rubric | You elicit the wrong decision set and the document lands at an altitude no entry point consumes. |
| Decisions before drafting (phases 1–2 before 3) | The first document-shaped artifact anchors the remaining search; the human edits your framing instead of supplying theirs. |
| Gate last, and from another session | A verdict from mid-authoring certifies a document that no longer exists, and one from the authoring session certifies its own work. |

Halt only when the human asks for work behind an ordering while its criterion is open. State that you stopped, name the crossing and its concrete cost, and request `APPROVED: waive <ordering> — <reason>`. If they instruct you again, comply once and record the crossing. You are asking, not refusing.

## Completion

- An independent reviewer tested the document against solid and lean, and every finding reached closure — resolved, accepted with rationale, or escalated and disposed of by the human. An open escalation leaves the run incomplete.
- An independent session's gate run returns `Ready` or `Ready with notes` for the chosen entry point.
- No unclaimed marker is unresolved.
- No critical-path TBD remains.

Then the human runs `§BRAND_BINARY_NAME§ init --spec <path>`. Commit the document first: worktrees are created from the integration branch, so uncommitted input may be invisible to agents.

## Integration

| Skill | Relationship |
|---|---|
| `check-§BRAND_NAME_LOWER§-input-readiness` | The judge to this producer. Owns the pass/fail rubric; run at the end of phase 4 by an independent session, never by this one. |
| `systemic-thinking` | Optional final pass at phase 4 for frame-level and dynamic risk. |
| `feynman` | Use at phase 2 when a decision resists plain-language statement. |
| `code-review` | The two-sided protocol the phase-4 adversarial review runs under. Read it completely before reviewing or answering. |
| `epic-writing`, `user-story-writing`, `architecture-planning` | Downstream agent-side producers that consume this document. Do not run them here. |
| `have-you-considered` | Use at phase 2 to widen the option set behind a decision before it settles. |
