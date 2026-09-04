---
name: check-§BRAND_NAME_LOWER§-input-readiness
description: Assess whether an input document is ready for a specific §BRAND_NAME_TITLE§ MAS entry point. Use when a user asks whether a goal, functional spec, detailed spec, technical spec, PRD, story bundle, architecture plan, or other source document is solid enough to run through §BRAND_BINARY_NAME§ with `--entry-point general-objective`, `functional-spec`, `detailed-spec`, or `technical-spec`; when deciding which entry point fits a document; or before `§BRAND_BINARY_NAME§ init --spec`.
---

# Objective

Assess whether a document gives §BRAND_NAME_TITLE§ agents enough signal to produce quality artifacts and code for the selected entry point.

Output a readiness report, not a rewrite. The report answers:
- Is the document ready for the requested entry point?
- If not, what would force agents to guess, block, or produce low-quality artifacts?
- Is another entry point a better fit?
- What exact questions or edits would make it ready?

# Inputs

Require both:
- **Document path**: the source document to assess.
- **Target entry point**: one of `general-objective`, `functional-spec`, `detailed-spec`, or `technical-spec`.

If either is missing, ask for it before assessing.

Treat `detailed-spec` as a legacy alias of `functional-spec`.

# Protocol

## 1. Read

Read the full input document before judging it. For long Markdown documents, use heading navigation first, then read every relevant section.

If the document lives in a Git repository, run a read-only status check for the document path. Warn if it is untracked or uncommitted: worktrees are created from the configured integration branch, so uncommitted input may not be visible to agents.

Declare what you read in the report. If token limits prevent full reading, stop and report the partial scope instead of issuing a readiness verdict.

## 2. Classify Entry-Point Fit

Compare the document's altitude to the requested entry point:

| Entry point | Starts at | Document must already contain |
|-------------|-----------|-------------------------------|
| `general-objective` | Epic planning | Product intent: why, users, scope, behavior, success, and product decisions. |
| `functional-spec` / `detailed-spec` | Architecture | Functional behavior already resolved enough for architects to define structure. |
| `technical-spec` | Code planning | Architecture and implementation boundaries already resolved enough for code planners. |

If the document is good but aimed at a different altitude, return `Wrong entry point` rather than `Not ready`.

## 3. Assess Universal Readiness

Check every input document for:

| Criterion | Ready means |
|-----------|-------------|
| Intent | The reader can state what changes and why without relying on external conversation. |
| Scope | In-scope and out-of-scope boundaries are explicit enough to prevent scope absorption. |
| Decisions | Product or design decisions required by the selected entry point are made, not left for agents to infer. |
| Traceability | Major requirements trace to source context, user need, or stated rationale. |
| Testability | Success criteria or acceptance criteria are observable and falsifiable. |
| Edge cases | Important error states, boundaries, and exclusions are named. |
| Dependencies | External systems, prerequisites, sequencing, and constraints are explicit. |
| Ambiguity | Critical-path TBDs, open questions, contradictions, and low-confidence assumptions are surfaced. |
| Non-functional constraints | Load, latency, availability, security, privacy, regulatory, stack, operability, and cost bounds are stated wherever they apply. |
| Leanness | No detail takes a decision a downstream stage still had. Detail recording a decision already made elsewhere is a constraint, not excess. |

Readiness runs in both directions. Too little detail makes agents guess or block; too much takes a decision from the stage better placed to make it, and attaches the author's authority to it so nobody downstream reopens it. Assess both.

Do not demand lower-level detail than the entry point needs. A `general-objective` document should not need API shapes; a `technical-spec` document usually does. Conversely, test detail by asking who still had the choice. Detail that takes a live downstream decision and **contradicts** something else in the document is a blocker; detail that merely exceeds the altitude is a `Note`. Structural vocabulary is not the test and misleads reliably: an inherited stack, a portability bound, and an engineering guardrail aimed at the agents who will build it all read as architecture while deciding nothing that was still open.

Never treat a non-functional constraint as excess detail, at any entry point. A constraint bounds the solution space; a design picks a point inside it. Stack candidates, load and scalability targets, latency, availability, security, privacy, regulatory and cost bounds are constraints the human owns at every altitude — their absence is a gap, not their presence.

## 4. Apply Entry-Point Rubric

### `general-objective`

Ready when §BRAND_NAME_TITLE§ can decompose the source into epics and stories without inventing product intent.

Require:
- Problem statement and motivation.
- Target users or personas with enough context to drive behavior.
- MVP scope and explicit out-of-scope.
- Main capabilities, workflows, business rules, inputs, outputs, and edge cases.
- Product decisions that affect behavior.
- Success criteria and key risks or assumptions.

Blockers:
- Missing target user or value.
- Vague capability descriptions such as "improve UX" or "add auth" with no behavior.
- Critical product choices deferred to agents.
- Out-of-scope absent where adjacent features are implied.

### `functional-spec` / `detailed-spec`

Ready when §BRAND_NAME_TITLE§ can skip epic/story writing and start architecture.

Require:
- Functional requirements or user stories are already bounded and behaviorally complete.
- Personas or actors are clear where behavior depends on them.
- Acceptance criteria, workflows, domain concepts and their relationships, integration points, constraints, and failure modes are explicit. These are domain-level: the entry point starts at architecture, so storage shape, schema, and component structure are the architect's to define and are not required here.
- Product scope is settled; remaining open questions are architectural, not product-level.

Blockers:
- Mostly vision-level content better suited to `general-objective`.
- Requirements lack acceptance criteria or observable behavior.
- Multiple unrelated capabilities are bundled without decomposition.
- Product decisions remain open on the critical path.

### `technical-spec`

Ready when §BRAND_NAME_TITLE§ can skip architecture and start code planning.

Require:
- Components, interfaces, data flow, state/storage implications, migrations, validation, error handling, and integration boundaries are specified where relevant.
- Implementation work can be split into bounded coding tasks.
- Test strategy and validation commands or scenarios are known.
- Security, data integrity, performance, and rollout constraints are explicit where applicable.
- Remaining choices are local implementation choices, not architecture or product decisions.

Blockers:
- Architecture is absent or only aspirational.
- Interface, state, migration, or validation behavior is underspecified.
- Tests or validation strategy are missing.
- The document requires agents to discover major design decisions from scratch.

## 5. Report

Use this format:

```markdown
# §BRAND_NAME_TITLE§ Input Readiness: [document]

## Verdict

**[Ready | Ready with notes | Not ready | Wrong entry point]** for `[entry-point]`.

One-paragraph rationale.

## Entry-Point Fit

- Requested: `[entry-point]`
- Best fit: `[entry-point]`
- Reason: [altitude and pipeline fit]

## Readiness Matrix

| Area | Status | Evidence | Gap |
|------|--------|----------|-----|
| Intent | Pass / Note / Blocker | [location] | [gap or "-"] |

## Blockers

### [Title]

- **Location:** [file:line, heading, or short excerpt]
- **Why it blocks §BRAND_NAME_TITLE§:** [specific agent guess/block/failure mode]
- **Fix:** [question to answer or document section to add]

## Notes

- [Non-blocking risks or improvements]

## Questions To Resolve

1. [Targeted question]

## Next Action

[Initialize with requested entry point | Initialize at `[best-fit entry point]` instead | Revise via `goal-writing`, then re-check]
```

Rules:
- Route the next action by verdict:
  - `Ready` / `Ready with notes` — initialize at the requested entry point. Notes travel with the document; they do not send it back.
  - `Wrong entry point` — the document may already be sound at another altitude. Name the best fit and leave the choice with the human: initialize there, or revise via `goal-writing` to meet the entry point they asked for. Do not pick for them.
  - `Not ready` — hand this report to `goal-writing`, the producer for every entry point; it re-opens the phase the blockers belong to.
- Never resolve blockers yourself, in any verdict. A judge that rewrites the document can no longer assess it.
- Use `Blocker` only for issues that would make the selected entry point unsafe or low-quality.
- Use `Note` for improvements that would help but do not force guessing.
- Cite evidence from the input document; do not rely on memory or intent.
- If no blockers exist, still name the most plausible way the run could fail.

## 6. Re-Check

A document routed to `goal-writing` and returned arrives here again. A re-check is not a fresh assessment:

- Reconcile every prior blocker explicitly as **Resolved**, **Partially addressed**, or **Still present**. Do not re-derive them from scratch, and do not restate a resolved one.
- Any critical-path gap present in the document may block, whenever it is found. A gap missed in round 1 does not become acceptable by having been missed; unlike a code review, which is bounded by a diff, this gate is bounded by nothing smaller than the whole document, and letting an unready document through is the failure it exists to prevent.
- Anti-churn applies to the non-blocking half only: do not accumulate fresh `Note`s round after round on a document that is converging, and do not re-raise a resolved finding.
- Assess the document as it now stands, not the delta. The document is what converges — the finding set follows it down as gaps close, and may still grow when the document has a gap nobody had reached yet.
- Open the report with `Round N — blockers X→Y`.

If blockers are not falling while the document grows, say so and stop: that is a document whose problem is its framing or its entry point, and another round will not find it.

# Constraints

- Never modify the input document. Assessment is read-only (ADR-0094), and the exception matters most exactly when it is invoked: a session that rewrites the document has become its author and can no longer certify it.
- If a rewrite is requested, end the assessment, hand this report to `goal-writing`, and leave the next gate to a session that did not write the change.
- Do not produce implementation plans, epics, stories, or architecture while assessing readiness.
- Do not paper over missing decisions with assumptions. Missing critical decisions are readiness blockers.
- Do not require details owned by downstream pipeline stages.
- Do not call a document ready because it is polished prose; readiness means the selected entry point can operate without hidden context.

# Integration

| Skill | Relationship |
|-------|--------------|
| `goal-writing` | Upstream producer for every entry point. Receives `Not ready`, and `Wrong entry point` only when the human chooses to revise rather than switch altitude; `Ready` and `Ready with notes` go to initialization, not back. Independence runs both ways: it must not run this skill on its own output, and this skill must not fix what it flags. |
| `spec-review` | Deeper corpus quality review. Use after or instead of this skill when the user wants a full consistency audit. |
| `checkpoint-summary` | Downstream checkpoint digest after agents have already produced artifacts. |
| `epic-writing` | Downstream producer for `general-objective` inputs. |
| `user-story-writing` | Downstream producer after epic planning. |
| `detailed-spec-writing` | Complementary producer for requirements that need to become implementation-ready specs. |
