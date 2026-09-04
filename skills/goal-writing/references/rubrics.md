# Elicitation Rubrics

Questions that surface the decisions each entry point requires. Read the section for the chosen altitude before phase 1.

**This file does not judge.** Pass/fail criteria, blockers, and the readiness verdict belong to `check-§BRAND_NAME_LOWER§-input-readiness`. Do not restate them here or paraphrase them in conversation. During phase 4, after cold review and the edits it produces, the document is handed to an independent session that runs that skill and rules.

**Use the questions as seeds, not a script.** Each is written to start from a concrete instance, because an abstract category question often fails to activate domain recall. Replace the bracketed instance with something from the human's own material as soon as you have one. A question that has already been answered is not asked again.

---

## `general-objective`

Sections: Problem Statement · Target Users · Solution Overview · MVP Scope · Explicit Out of Scope · General Specification · Success Criteria · Risks and Assumptions.

Rationale kind: **product** — why this, for whom, why now.

### Problem and users

- Walk me through the last time someone hit this problem. What did they do instead?
- Who exactly is that person? What is their job when this happens?
- What does it cost them today — time, money, errors, abandonment?
- If we ship nothing, what happens in six months?
- Who else touches this that we have not mentioned?

### Domain model

Ask first whether one exists — for a product whose substance is a structure rather than a flow, it usually does, and predates this document.

- Is there already a model of this domain — entities, relations, lifecycle — written down or in your head?
- Walk me through it. What is each thing, and what connects to what?
- What states does an entity move through, and what moves it?
- Which of these are genuinely distinct, and which are the same thing wearing two names?
- What is missing from the model that the problem statement implies?

Then challenge it: an abstraction that does no work, peer items that could grow by one without disturbing anything, a relation nobody can name an instance of.

### Solution direction

- Describe the shape of the answer without naming a technology.
- What are the two or three concepts a user has to learn to use this?
- Walk me through the main flow end to end, as if narrating a screen recording.
- Which decision here, if reversed later, would be expensive?

### Scope

- What is the smallest version that a real user would still choose over their workaround?
- Name something adjacent that a reasonable engineer would assume is included. Is it?
- What are you deliberately not building yet, and what makes that safe to defer?

### Behaviour

For each capability in scope:

- What goes in, what comes out?
- What rule decides between the two outcomes when [instance]?
- What happens when the input is absent, malformed, duplicated, or arrives twice?
- Who is allowed to do this, and what do they see if they are not?
- If a UI is involved: what does the screen show before, during, and after?

Now ask the [non-functional constraints](#every-altitude--non-functional-constraints), then close with:

### Success and risk

- What observation, six weeks after launch, would tell you this failed?
- What are you assuming about your users that you have not verified?
- What is the most likely reason this gets built correctly and still does not get used?

---

## `functional-spec` (alias: `detailed-spec`)

Sections: the `general-objective` set, plus resolved functional requirements with acceptance criteria, actors, domain concepts, integration points, constraints, failure modes.

Rationale kind: **product, plus the structural constraints architecture must respect.**

Entry here asserts product decisions are already settled. Verify that before proceeding: if the human is still discovering users or scope, the altitude is wrong and `general-objective` is the honest entry point. Say so rather than eliciting product decisions under a functional-spec heading.

**First, ask what already exists.** At this altitude the product layer is usually settled in an upstream vision or goal document. If one exists, reference it rather than re-deriving it: read it, confirm with the human that it still holds, and record only where this document departs from or sharpens it. Re-asking questions a document on disk already answers turns the session into an interrogation and invites answers that contradict the source.

Three cases, in order: the product layer is written down — inherit it; it is firm in the human's head but unwritten — capture it briefly and move on; it is genuinely unsettled — the altitude is wrong.

### Behaviour already resolved

- State the acceptance criteria for [requirement] as something that could be observed failing.
- Which actor performs this, and does the behaviour differ by actor?
- What are the states this thing moves through, and what moves it?
- What must remain true across the whole flow regardless of path taken?

### Structural constraints

Constraints only. The architect owns the structural design at this altitude — elicit what binds them, never what they should build.

- What existing system does this have to live inside or alongside?
- What must this not change — data, contracts, behaviour others depend on?
- Where must data already come from, or already end up, because something else owns it?
- What must not be observable in a partial state, as a business rule rather than a storage choice?
- Which of these is a genuine constraint, and which is a preference you would trade?

If an answer describes a solution rather than a boundary, record the boundary and leave the solution to the architect.

### Rationale and ADR candidates

- What makes this constraint binding, and who owns it?
- Would a new engineer, six months from now, be able to reconstruct that reasoning from the document?
- Is this decision durable enough to constrain work beyond this goal? If so, flag it as an ADR candidate and name it — do not write the ADR here.

Now ask the [non-functional constraints](#every-altitude--non-functional-constraints), then close with:

### Failure

- What are the error states, and what does the user or caller see in each?
- What happens when the external dependency is slow, down, or wrong?
- What is the recovery path once it has already gone wrong?

---

## `technical-spec`

Sections: the `functional-spec` set, plus components, interfaces, data flow, state and storage, migrations, validation, error handling, integration boundaries, test strategy, rollout.

Rationale kind: **structural** — why these components, boundaries, and interfaces.

Entry here asserts architecture is resolved. Verify it is specified, not aspirational: a named component with no interface is a wish. If architecture is still being discovered, `functional-spec` is the honest entry point.

**First, ask what already exists.** At this altitude the product and functional layers are usually settled in upstream vision, goal, or functional documents. If one exists, reference it rather than re-deriving it: read it, confirm with the human that it still holds, and record only where this document departs from or sharpens it. Re-asking questions a document on disk already answers turns the session into an interrogation and invites answers that contradict the source.

Three cases, in order: the upstream layers are written down — inherit it; it is firm in the human's head but unwritten — capture it briefly and move on; it is genuinely unsettled — the altitude is wrong.

### Structure

- Name the components and what each one owns.
- What crosses each boundary, in what shape?
- Why is the boundary here rather than one level up or down?
- Which component changes when [likely future requirement] arrives?

### State and data

- What is stored, where, and who writes it?
- What migration does this require, and what happens to data mid-migration?
- What is validated, at which boundary, and what happens when validation fails?
- What is idempotent, and what breaks if it runs twice?

### Verification

- How is each piece tested, and what command runs those tests?
- What would a test have to observe to prove this works end to end?
- What is deliberately not covered by tests, and why is that acceptable?

Now ask the [non-functional constraints](#every-altitude--non-functional-constraints), then close with:

### Rollout and risk

- How does this ship — all at once, behind a flag, in stages?
- What is the rollback, and how long does it take?
- What are the security, data-integrity, and performance constraints that bound the implementation?
- Which choices remain open, and are they genuinely local implementation choices rather than architecture?

---

## Every altitude — non-functional constraints

Ask these whatever the entry point, once the altitude's functional and structural questions have settled and before its closing success, failure, and risk questions. They bound the solution rather than choosing it, so they are the human's to state at product altitude as much as at technical altitude. Precision scales with the entry point; presence does not.

- How many users or requests must this carry at launch? In a year?
- What response time would make a user abandon it?
- What availability is expected, and what does an hour of downtime cost?
- What stack may this use, and what is ruled out — by policy, licensing, or what the team can actually operate?
- What regulatory, security, privacy, or data-residency rules bind it?
- What data is sensitive, and who must never see it?
- Who operates this once it ships, and what do they already run?
- What cost ceiling applies — to build, or to run?

If an answer names a structure rather than a bound, what to do depends on who is left to decide it:

- `general-objective`, `functional-spec` — keep the bound and leave the structure to the architect the run is about to reach.
- `technical-spec` — the run skips architecture, so there is no architect downstream. Record the structure as a settled decision with its rationale, for the code planner. If it is not actually settled, that is altitude drift: say so, and `functional-spec` is the honest entry point.
