# 150 - Shared Authoring Vocabulary: Priority, Commitments, Proof Stage

## Status

ACCEPTED — released 2026-09-18 as a candidate release; backfilled 2026-09-21.

## Context and Problem Statement

Producers turned unspecified mechanisms, assumptions and optional scope into
mandatory guarantees, and no shared contract said otherwise. Reviews approved
individual artifacts without checking that the next role could act from them, that
priorities survived the stage boundary, or that a guarantee had an owner. The
blocked-tasks wake told the orchestrator to supersede on spec ambiguity and on a
wrong approach, the blocking protocol told doers to add any blocking task as an
edge, and no final wake reported delivery apart from activity. The operator skill
carried three instructions that conflicted with the others, plus a factual error
against the CLI.

Together these produced the failure the goal was named for: chains of blocked
tasks, where superseding a task created a new one that blocked on the same
unresolved cause.

## Commit-Evidenced Intent

The release addresses one goal, `prevent-blocked-tasks-chains`, whose improvements
map to the packages below. The vocabulary belongs at one owner — the stated reason
for putting priority, commitment and proof-stage rules in a shared reference rather
than in each skill.

## Considered Options

1. **Fix each instruction where it is wrong** — correct the operator skill, the
   wake templates and the reviewer checklists independently.
2. **Define the vocabulary once and point every consumer at it**, then correct each
   instruction in its original location against that definition.
3. **Move the rules into code enforcement** — validate priority and commitment
   properties rather than instructing them.

## Decision Outcome

Chose **Option 2**, released as packages W0–W5a.

**W1 — shared authoring rules.** `reference-first-authoring.md` gains "Priority,
Commitments and Proof Stage": Must and Won't bind; Should and Could defer with a
recorded disposition, preferring Should; split or merge keeps priorities; no
autonomous relabel; a missing label is neither Must nor optional. It adds the
consequential-commitment record — source anchor, claim, owner, evidence, affected
outputs, settling stage — written once at its owner, and the proof-stage table with
the rule that an unavailable acceptance target neither blocks all implementation
nor waives the test. Its Review Boundary gains three items (priority drift or Won't
inclusion; unusable handoff or wrong-stage evidence; an unowned consequential
guarantee) and the conduct rule that a finding cannot create a commitment. Six
skills point at that section with one paragraph each: `goal-writing`,
`check-liza-input-readiness`, `epic-writing`, `user-story-writing`,
`architecture-planning` and `spec-review`.

**W2 — handoff duties.** Planning reviewer checklists gain Handoff usable, Priority
propagation, Consequential commitment and Proof stage rows; architecture adds
Minimal Must mechanism; code-plan adds Dependency meaning — what each `depends_on`,
`task_depends_on` and `inherit_inputs` supplies, and the case of an inherited
barrier holding a child that needs nothing from that upstream. "A finding cannot
create a commitment" precedes the review phase for the four planning reviewers; the
code reviewer receives it as an instruction naming an action it can take — label a
contract defect with its owning artifact and role, reject if delivered behavior is
wrong, comment if it is still correct — so the coder can block for source-owner
routing.

**W3 — cause-based recovery.** The three supersession defaults in
`wake_blocked_tasks.tmpl` become a recovery table keyed by cause: runtime or stale
ownership; wrong or excessive dependency; corrected inputs; product ambiguity
(assess with the minimal decision, route the answer to the source owner, not
supersession by default); unsupported assumption; an earlier stage awaiting a
later-stage correction (a hold, never a backward edge); immutable ancestry; removed
Won't scope. `replacement_lineage_policy.tmpl` carries "supersession is not the
default" into the blocked, hypothesis-exhausted and immediate-discovery wakes.
Closure evidence is the consumer being eligible or past the old failure point — not
a new task ID.

**W4 — operator skill.** Six sections rewritten in place: the watch names the
delivery objective and blocking chain; planning audit is governance, not a second
review; recovery is by cause, with closure defined as consumer progress past the
old failure point; `recover-task` preserves by default and discards only with
`--fresh`, `recover-agent` is full cleanup, and neither unblocks —
`TROUBLESHOOTING.md` now owns flags and version behavior.

**W0/W5a — measurement and validation.** The context-budget instrument is recorded
separately ([ADR-0149](0149-per-role-instruction-budget.md)). W5a adds versioned
scenario inputs, a retained protocol trial with hashes, and flips the budget gate
to `true`.

**Amendment (2026-09-26) — critical-path duties.** W2's Dependency meaning was
satisfiable by naming a provider *implementation* as the consumed artifact, and
only missing edges were ever rejected, so a run serialized behind whole-phase
barriers, implementation-bound clients, a fixture bundled with its proof, and
architecture-chosen shared-file writer chains. Every edge now names the earliest
artifact that suffices: code planner and Dependency meaning; master property 4
(contract or implementation); architecture Dependency ordering (superfluous
edges). The architect's prompt and the new architecture Critical path row cover
contract-first clients, fixture/proof separation and per-scope splits of a shared
file. `architecture-planning` gains False Dependence. Contract-first keeps the
no-fake-provider guard through one hold point: a verification task that depends
on client and provider, which every consumer of the client's behavior depends on.
The [ADR-0048](0048-multi-phase-planning.md) `inherit_inputs: all` default and
critical-path visibility are deferred in `TECH_DEBT.md`.

## Rationale

Option 1 is what produced the conflicting instructions: each location was correct
against its own reading, and there was no shared definition to be wrong against.
Naming one owner for the vocabulary makes drift detectable, which is why the rules
live in `reference-first-authoring.md` and the consumers carry one paragraph each
rather than a copy.

Option 3 is not available for most of this content. Priority coherence and
commitment ownership are properties of prose written by agents; code can validate
that a label exists, not that a Must set is coherent. Where enforcement was
possible it was used — `TestReferenceFirstProducerReviewerPairs` requires the
spec-review rejection classes to be spelled out.

Correcting each instruction at its original location, rather than adding a
correcting layer, is what the goal required and keeps the conflicting-instruction
class from recurring.

## Consequences

- Priority, commitment ownership and proof stage have one definition; a consumer
  that drifts from it is visibly out of step with a named source.
- Reviewers test the handoff, not only the artifact, which adds rows to four
  planning checklists and lengthens those prompts.
- Superseding stops being the orchestrator's default response to a blocked task,
  which is the mechanism intended to break blocked-task chains.
- Mandatory-read growth exceeded the 5% ceiling for six role variants — up to
  +12.2% for `us-reviewer`. The overruns were reported for a scoped human decision;
  no binding rule was removed to fit.
- Three separable decisions (authoring vocabulary, handoff duties, cause-based
  recovery) are recorded here as one, matching how they were released. A later
  change to one of them will need to state which part it revises.

## Evidence and Reconstruction

Sources: commits `933fd019c` (W1), `e3bb91492` (W2), `cee7f87db` (W3), `38e75d3ab`
(W4), `20ccc2858` (W5a) and the merge `22d17afc2`, together with the blackboard
`specs/adversarial-pairing/20260913-prevent-blocked-tasks-chains-bb.md`.

**Unconfirmed:** the merge records human decision D4 — release before W5b — with
W5b's 24 candidate exercises still unrun. The user did not supply the rationale for
that timing when asked on 2026-09-21; it remains an open gap rather than a
reconstructed rationale. Historical alternatives are likewise reconstructed.

---
*Reconstructed from commits `933fd019c`..`22d17afc2` (2026-09-18).*
