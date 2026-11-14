# Prevent Bugs at Their Shape, Not Their Instance

Status: draft
Entry point: `functional-spec`

## Objective

Make every liza agent treat a bug fix as unfinished until the shape that made
the bug possible is removed, scheduled with an owner, or knowingly accepted, and
give the project a recurring review that turns those answers into higher-level
invariants and guardrails, kept short. The run starts at architecture: this
document fixes the problem, the bounds and the proof; the architect chooses
where each rule lives.

Objective shape: a **direction** (fewer fixes that repair a recent change)
bounded by **limits** (no contract growth beyond the semantic content added,
G2.2; no new ceremony where an existing one can carry the step).

Companion goals, in run order: this goal first, then
`20261114-bug-prevention-engine.md` (both edit skills and contracts, so they
do not run in parallel, and the engine run then produces prevention records),
then `../omni/specs/goals/20261114-bug-prevention-omni.md`.

## Problem Statement

liza's contract asks for root cause before symptoms, but its process stops at
the defect. A fix is done when the symptom is gone and a regression test pins
it. Nothing asks what made the defect writable, so the next fix is written under
the same frame and often creates the next defect.

### Evidence

Collected on 2026-11-14 from a downstream fork and its launcher repository,
the last 10 days of history: about 80 distinct fix commits.

- **Fixes breed fixes.** At least 12 of the ~80 fixes repair a change merged
  earlier the same month (counted by hand from commit messages). One relocation
  of where run state lives produced six tickets in sequence; one message reads
  "three defects, each hidden by the one before". Two cross-cutting changes
  each needed a portability fix after merge.
- **The same shape twice.** A deadline that also covered work it was not sized
  for was fixed on Nov 4 (test helper) and again on Nov 13 (launcher download).
  A sibling search after the first would have found the second.
- **Prompt rules oscillate.** Skill rules were loosened on Nov 9, then a shared
  rule was restored on Nov 11 ("a skill's stricter rule wins again").
- **Invariants and lessons barely learn.** Two of ~80 fixes touched
  `INVARIANTS.md`; one lesson was added in 30 days. The invariant-check
  guardrail (G1.2) fires on "system state, concurrency, review flow, agent
  lifecycle, or integration", while the fixes clustered in installation and
  upgrade, cross-binary interfaces, session hooks, outcome reporting,
  portability and environment assumptions, so it never fired for them.
- **Rules learnt by rejection.** Over the product repository's whole history,
  about 40 commits only fixed lint the submit gate reported and the local check
  did not, and about 210 only repaired plan grammar the gate refused. Authors
  met the gate's rules when it rejected them.
- **Integration loses work.** Four merges in the same history lost or
  duplicated code.
- **Whole-history baselines.** In the downstream fork, whose fix history is
  91% liza's (840 of 920 distinct fix subjects), 64% of the 822 fixes that
  change existing lines repair code 30 days old or less, by blame on each
  fix's parent (an upper bound; rewrites count as young). In the product
  repository, about 44% of 319 product fixes repair a change merged shortly
  before.
- **Prerequisites learnt at admission.** The same validation prerequisite (a
  compiler a race-enabled test needs) was missing from plans three times;
  each time, admission refused it.

### Why the process stops at the instance

- **Rule 6 vs Rule 11.** Scope discipline ("solve the problem, then stop";
  refactoring "proposed as distinct tasks") puts the structural fix out of
  scope by construction. Rule 11 ends with "propose countermeasures", but no
  gate, artifact or owner checks it.
- **The debugging skill has no prevention step.** It ends at the root cause in
  code.
- **Proposals have no queue.** Deferrals land in `TECH_DEBT.md` (over 90 KB in
  both repositories), where nothing schedules them.
- **Small fixes skip design review.** The architecture protocol triggers on
  planning and structural concerns; a fix looks small even when it introduces
  a new state, path or representation.
- **Concepts are shared before they are defined.** The project's runtime
  directory was named for its owner, not for its one-run lifespan, and nothing
  defined what belonged in it. Writers read the name correctly and put durable
  files there; each new run deleted them. An earlier review comment on one such
  file got a fix for that file only. The replacement directory again carries an
  owner's name, and its resolver's name already disagrees with what it holds.
- **Invariants drift to bug level.** New rows get written from the fix
  ("cleanup moves the binding, handoffs, tracker artifacts…") rather than from
  the property the fix restores, so the file grows and gets harder to consult.

### Prior art the run inherits

- `contracts/CORE.md` Rule 11 (RCA, minimal RCA, countermeasures), Rule 6
  (scope discipline), Stop Triggers ("same fix proposed twice"), Rule 3 (DoD).
- `skills/debugging/SKILL.md`, `skills/code-review/SKILL.md`,
  `skills/lesson-capture/SKILL.md`.
- `specs/protocols/sprint-governance.md` Retrospective Protocol: data-driven,
  agents supply the data, the human synthesizes.
- Pairing Retrospective Protocol (`contracts/PAIRING_MODE.md`).
- `INVARIANTS.md` Protection Matrix, `GUARDRAILS.md` G1.2, G2.2, G2.3,
  `contracts/CONTRACT_FAILURE_MODE_MAP.md`.
- ADR-0200 and `internal/git/patchid.go` (patch identity preserved when a
  review range is replayed); CORE's Git Protocol ("Merge Conflicts: Never
  auto-resolve").

## Actors

- **Fixer**: the agent writing a fix, in any mode (Pairing, multi-agent coder,
  subagent read-write).
- **Reviewer**: the code reviewer of the fix (multi-agent) or the human
  (Pairing).
- **Human**: owns invariants, guardrails and contract changes; synthesizes the
  retrospective.

## Scope

In scope, mechanism left to the architect:

- A prevention step in the definition of done for bug fixes.
- A countable record of that step in the project's change history.
- Review checks that a change does not open a new shape or lose work.
- A stop trigger when one concept keeps needing fixes.
- A recurring review that feeds invariants and guardrails, at principle level,
  including pruning.
- Defining shared concepts, and naming them for what matters to their writers.
- A default home for invariants in every project liza initializes.
- A skill that identifies structural weaknesses from a project's fix history.
- Running a gate's checks before submission.
- The ADRs that record the discipline's structural decisions.

### Out of scope

- Detection: test matrices, CI coverage, release gates. They are valuable but
  separate.
- Rewriting `INVARIANTS.md` as a whole; this goal sets the altitude rule and
  the review that applies it from now on.
- Any project-specific invariant (each project's own goals produce those).
- Tooling that parses commit history beyond what a shell one-liner or an
  existing command can do; FR9 is a skill that reads history, not a parser.
- A definition checklist for every new concept: the companion goals' lints
  refuse the known shapes at introduction; FR7 covers concepts once shared.

## Functional Requirements

Priorities: **Must** FR1–FR5 (the core loop); **Should** FR6, FR7, FR8, FR9's
light mode, FR10; **Could** FR9's second independent classification.

Trade-offs, decided 2026-11-14, in three tiers:
1. **Above all:** the core loop holds (FR1–FR5: no fix without a prevention
   answer, and a removed shape is refused), and leanness (G2.2: contract and
   engine growth proportional to what is added).
2. Low operating cost per fix and per run: no tool call beyond the fix's own
   analysis and validation, no run blocked on the human.
3. Completeness: FR10 covering every gate check, FR9's full mode, FR8's
   invariant home in every project.

What gives way first: FR9's second classification, then FR10's coverage,
then FR8. The Must set stands alone: FR1–FR5 work without the retrospective,
the skill or the gate command.

FR1 — **No fix without a prevention answer.** A bug fix is done only when it
states what made the bug possible and one disposition:
(a) *removed*: the shape is refused after this change. The answer names the
boundary that now refuses it, a negative test proving the refusal, and the
sibling paths searched with their disposition. An existing protection
qualifies with that evidence and no new code.
(b) *scheduled*: an accountable owner removes it or decides on it: a task or
ticket that removes it, or the human when the decision is theirs.
(c) *accepted*: why removing it is not worth it. Only the human accepts, in
Pairing at the fix. In multi-agent mode an agent cannot accept: it records
*scheduled* with the human as owner, no task needed, and the fix merges; FR6
lists such records.
A recurrence of the shape voids an acceptance.
"Possible" is answered at the level of the system (a missing owner, a duplicate
source of truth, an unchecked assumption, an unsafe default), not by restating
the defect.
A fix commit is an agent's commit of Conventional Commit type `fix`; commits a
human makes, and reverts, need no record.
*Accepted when* the debugging skill and the bug-fix DoD require the answer; the
submit gate refuses an agent's fix commit whose record is missing or
malformed, as a correctable refusal (the author keeps the task), while the
reviewer judges the substance; in Pairing the record is part of the agent's
DoD; tests cover an omitted, a malformed and a squashed record.

FR2 — **The answer is countable.** The record carries the concepts the shape
concerns, each named by its one canonical identifier (an invariant, or the
boundary's identifier in the code; the record-format ADR defines how) rather
than free prose, so FR4 can match them; the disposition, with its owner when
scheduled (a task reference, or the human); and its classification against the
invariants and guardrails: *gap* (none covers it), *unenforced* (covered, not
enforced), *not consulted* (covered, its trigger did not fire) or *none needed*
(a classification, not a disposition: FR1 still requires one). It survives in
the change history in a form a shell command can count without reading prose,
in any project, whatever its stack (G1.1).
*Accepted when* a documented command lists the fixes of a period by concept,
disposition and classification on a repository with sample commits.

FR3 — **Review checks what a change opens or loses.** The reviewer of a fix
checks what new state, path, file, flag or representation it introduces and
who else reads it. The reviewer of a merge, rebase, cherry-pick or conflict
resolution checks that no change of either side was lost or duplicated; CORE's
rule never to auto-resolve conflicts stays. An unanswered one is a finding.
*Accepted when* the code-review skill carries both checks and its pinned test
covers them.

FR4 — **A concept fixed twice stops for design.** When a fix lands on a concept
(FR2) that another fix touched within the last 14 days (decided 2026-11-14,
provisional until the first retrospective, where the human recalibrates it),
the next change on that concept goes through design before code: in
multi-agent mode it is routed to the architect role; in Pairing the
architecture protocol runs inline, and the human may waive it.
Removing the shape then counts as in scope, not as scope creep (Rule 6/11
boundary).
*Accepted when* the stop trigger and the scope boundary are in the contract
and a pinned test covers both.

FR5 — **Invariants stay principle-level.** An invariant states a property of
the system and what it protects against; the bug-specific detail lives in its
enforcement (type, lint, test, guard). Invariants record how they are enforced,
and unenforced ones stay visible.
*Accepted when* the rule is stated where invariants are maintained and the
guardrail on invariant compliance points to it.

FR6 — **Recurring review.** At each sprint retrospective (multi-agent) and on
request in Pairing, agents supply the period's records, by running FR9's light
mode over them; the human decides:
- triggers of the invariant-check guardrail widened to where shapes cluster;
- gaps turned into principle-level invariants or guardrails;
- unenforced invariants scheduled or accepted;
- open *scheduled* dispositions, including decisions an agent left to the
  human: done with proof, still scheduled, or accepted;
- FR4's window recalibrated;
- invariants not cited in the period examined, then retained, merged or
  retired: no citation can mean the invariant works (G2.3: the human decides
  removals);
- lessons captured where a gotcha, not a shape, is the finding.
*Accepted when* the retrospective protocols name this input and output, and a
dry run on a sample history produces the list.

FR7 — **Concepts are defined before they are shared.** A shared concept (a
directory, file, flag, state, record) has a written definition — what it
holds, what it does not, who writes it, how long its content lives — no later
than when a second writer or a second kind of content arrives. Its name states
the property its writers must respect (for storage, the lifespan, not only the
owner); a name that no longer matches what the concept holds is a finding.
This is the earliest prevention point; FR4 is its backstop.
*Accepted when* the code-review skill checks a change that adds a writer or a
kind of content to a shared concept for its definition and the fit of its name,
and its pinned test covers the check.

FR8 — **Every project has a home for its invariants.** FR5 and FR6 need a
place to land in every project, not only in liza's own repository; today the
guardrails template (`internal/embedded/guardrails-template.md`) has no
invariant section and nothing creates an `INVARIANTS.md`.
- `init` and `refresh-project` create an `INVARIANTS.md` skeleton when it is
  missing: the altitude rule of FR5, the `Invariant | Protects against |
  Enforced` table (unenforced allowed, shown as such) and an empty Protection
  Matrix. An existing file is never touched.
- The guardrails template carries a Tier 1 invariant-compliance rule pointing
  to it, with a trigger list the project fills in (stack-agnostic, G1.1).
- An existing `GUARDRAILS.md` is user-owned and never rewritten; when it lacks
  the pointer, `init` and `refresh-project` report an outcome check with the
  text to add.
- `INVARIANTS.md` is read when a rule's trigger fires, never as a mandatory
  initialization read.
*Accepted when* tests cover: a fresh project gets both files; a project with
`GUARDRAILS.md` and no `INVARIANTS.md` gets the skeleton, an unchanged
`GUARDRAILS.md` and the check; an existing `INVARIANTS.md` is byte-identical
after both commands; the initialization gate does not require it.

FR9 — **Weaknesses are identified from the fix history.** A skill analyzes the
fixes of a window, across the repositories a product spans, and returns the
project's structural weaknesses. Records made at fix time (FR2) see one bug;
only a window shows chains, repeated shapes and guardrail triggers drawn too
narrowly, and a project adopting liza has history but no records yet.
- **Gather:** fix and revert commits of the window, cherry-pick pairs counted
  once; read each one's why, not only its subject; FR2 records, when present,
  as a starting point.
- **Classify each fix** on three axes: the condition the tests did not cover
  that let it through (upgrade, second instance, other platform or provider,
  inherited environment, existing user files, slow or failing dependency),
  the structural cause that made it possible, and its lineage (repairs a
  recent change; a shape seen before). Causes are derived from the evidence,
  never from a preset list.
- **Check coverage:** each cause against invariants and guardrails, in FR2's
  vocabulary.
- **Find process causes:** why fixes stayed at the instance (fixes that repair
  recent fixes, the same shape twice, narrow triggers, concepts shared before
  being defined).
- **Return:** weaknesses at principle altitude with evidence and a confidence
  per cause, a draft goal per repository, and process findings.
- **Verify before claiming:** chronology checked before any causal claim;
  hand counts marked as such; claims marked verified or not (CORE Rule 3).
- **Reproducible census:** counts come from a pinned commit with the commands
  that reproduce them.
- **Independent classifications:** in *full* mode, at least two independent
  classifications are reconciled against the source; each divergence is
  resolved or reported. A single classifier does not catch its own errors.
- **Separate populations:** product fixes, corrections made inside a run
  before merge, and test or proof fixes are classified separately, never
  dropped: excluding flaky-test fixes as non-defects hides the cause "a
  green proof that did not exercise its inputs".
- **Direction without records:** it computes the share of fixes whose
  repaired code is 30 days old or less, by blame on each fix's parent, so a
  baseline exists before any FR2 record does.
- **Coverage first in every draft:** each goal it drafts says that a past fix
  establishes a concern, not a current defect, and that an invariant already
  enforced is discharged with proof, not new code.
- **Placement:** a fix found in a downstream fork that also exists upstream is
  attributed upstream, and the goal it drafts states how its changes reach
  upstream: a run there, or commits cherry-picked from the fork.
- **Modes:** *full* (a window of raw history, delegated to subagents, which
  return a classification digest), *light* (one period's FR2 records, for
  FR6).
Thin commit messages lower confidence and send the skill to diffs or linked
tickets; it says which.
*Accepted when* a full run on the fixture history (Proof Environment) finds at
least the repair-of-a-repair chain, the cherry-pick duplicate counted once, the
young-code share computed by blame, and the misleading message caught by its
diff, with evidence; a light run on sample FR2 records produces FR6's list;
pinned tests cover the skill's method.

FR10 — **Authors can run the gate before submitting.** liza provides one
command that runs exactly the checks its submit gate applies — the project's
configured validation commands, their declared prerequisites, and liza's own
validators such as plan and reference grammar — on the author's worktree,
with the gate's configuration and inputs, and returns the gate's verdict.
That command is the local check; comparing it with a project's own tooling,
which liza cannot see (G1.1), is out of scope.
*Accepted when* the command matches the gate's verdict on fixtures that pass
and fail each check, a missing prerequisite included; the planning and coding
prompts point authors to it; it works for any configured commands (G1.1).

## Constraints

- **Conciseness (G2.2):** net growth of `contracts/`, `skills/` and
  `internal/prompts/templates/` stays proportional to the content added; prefer
  rewriting Rule 11, the DoD and existing skill steps over appending.
- **Removal is systemic (G2.3):** no existing rule is cut to make room without
  the human's decision.
- **Reuse existing ceremonies:** the review lives inside the existing
  retrospectives, not in a new one; checks run at existing boundaries.
- **Stack-agnostic (G1.1):** the record of FR2 works in any git repository.
- **Modes:** Pairing (the human approves dispositions), multi-agent (reviewer
  checks them; human owns the retrospective and acceptances), subagent
  (returns the answer to its caller).
- **Where it runs:** in execution-engine; the changes are cherry-picked into
  liza. Commits are brand-neutral (no OMNI literal, G1.3; brand macros on
  rendered surfaces) and separate from any OMNI-specific change.
- **Decision records:** the run writes the ADRs below; they are deliverables,
  not candidates.

### ADRs the run writes

One ADR per decision, following `specs/architecture/ADR/README.md`, or an
amendment where an existing ADR owns the question:

- The record format of FR2 (for example a commit trailer) and its vocabulary.
- The Rule 6/11 boundary of FR4.
- The default invariant home of FR8, as an amendment of ADR-0032.

## Non-Functional Requirements

- **Adoption (Must):** enforced in multi-agent mode, where the submit gate
  refuses an agent's fix commit without a well-formed record (FR1). Not
  measured in Pairing: the record is part of the agent's DoD, and the history
  does not tell an agent's commit from a human's.
- **Cost (Must):** the prevention step adds no tool call beyond those the fix's
  own root-cause analysis and validation already make (Rule 11's minimal RCA
  already searches siblings); the review takes one command to gather its data.
- **Upstream portability (Should):** checked by the human when cherry-picking
  into liza: each commit applies cleanly and liza's `make test` passes. Within
  the run, commits pass the G1.3 scan and stay separate from OMNI-specific
  ones.
- **Direction (Should):** the share of fixes whose repaired code is 30 days
  old or less falls from the baseline FR9 measures on liza's own history;
  reported at each retrospective, not gated.

## Proof Environment

Local only: the repository, temporary git repositories and the existing pinned
contract/skill text tests. No external service or credential. Fixture proofs
run in the normal test suite and add at most one minute to it. A skill's
proof (FR6, FR9) is a recorded agent run of the skill on its fixture, within
one agent session, reviewed by the code reviewer.

- Pinned text tests for every new rule (FR1, FR3, FR4, FR5, FR6, FR7, FR9,
  FR10).
- Fixtures that pass and fail each gate check, a missing prerequisite included
  (FR10).
- A fixture history for FR9: temporary repositories whose commits carry real,
  minimal source changes — an introducing change, its repair, a repair of that
  repair, a cherry-pick duplicate, a proof defect, and one commit whose message
  misleads about its diff — with messages free of downstream product or ticket
  names.
- A temporary repository with sample fix commits proves FR1's submit check
  (omitted, malformed and squashed records) and the FR2 command.
- A dry run of FR6 on sample FR2 records produces at least: a widened trigger,
  a concept fixed twice, and an open scheduled disposition.

## Failure Modes

- Records become boilerplate (*none needed* classifications everywhere).
  FR6's review samples them; a high *none needed* share is itself a finding.
- A *removed* answer covers one caller and leaves siblings writable. FR1
  requires the refusing boundary, a negative test and the sibling search.
- *Scheduled* becomes a queue nobody drains. FR6 lists open dispositions at
  each retrospective.
- Invariants grow faster than they are pruned. FR5, FR6, G2.3.
- The stop trigger blocks urgent fixes. It holds only the next change on the
  concept, and in Pairing the human can waive it.
- The record format drifts between agents. FR1's submit check validates it
  whoever wrote the commit; FR2's sample commits cover format variants.
- Squashed, multi-concept and human commits. A squashed commit keeps its
  record and the submit check reads it; a fix touching two concepts lists
  both; a human's commits and reverts need no record.
- Free-text concepts never match, so FR4 never fires. FR2 names concepts by
  declared identifiers.

## Risks and Assumptions

- ASSUMPTION: the downstream evidence represents liza's own fix history. The
  first retrospective with FR2 data confirms or corrects it.
- RISK: agents classify inconsistently at first; the vocabulary may need a
  revision after the first review.
- RISK: the 14-day window of FR4 is provisional; the first retrospective
  recalibrates it.
