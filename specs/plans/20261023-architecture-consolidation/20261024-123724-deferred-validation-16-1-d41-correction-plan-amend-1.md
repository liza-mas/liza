# Plan Correction: Placement of the D41-Family Timing Corrections

## Source References
Source revision: "49595a9d7cf9d52f5348cfce74cb614a7ad7e7bc"

### Direct References
- "original-deps": "specs/plans/20261023-architecture-consolidation/20261024-120308-deferred-validation-16-1-d41-correction-plan.md#Dependencies and Shared Files" @ "49595a9d7cf9d52f5348cfce74cb614a7ad7e7bc"
- "original-task1": "specs/plans/20261023-architecture-consolidation/20261024-120308-deferred-validation-16-1-d41-correction-plan.md#Task 1: Make the unadmitted and publish in-flight submit tests deterministic" @ "49595a9d7cf9d52f5348cfce74cb614a7ad7e7bc"
- "original-rca": "specs/plans/20261023-architecture-consolidation/20261024-120308-deferred-validation-16-1-d41-correction-plan.md#Root Cause Analysis" @ "49595a9d7cf9d52f5348cfce74cb614a7ad7e7bc"
- "a4-proof": "specs/arch-plan/20261023-architecture-consolidation/20261023-205627-arm-1-ar-13.md#Proof and Shared Ownership" @ "49595a9d7cf9d52f5348cfce74cb614a7ad7e7bc"
- "a3-proof": "specs/arch-plan/20261023-architecture-consolidation/20261023-203240-arm-1-ar-12.md#Proof and Shared Ownership" @ "49595a9d7cf9d52f5348cfce74cb614a7ad7e7bc"
- "a2-chain": "specs/arch-plan/20261023-architecture-consolidation/20261023-195624-arm-1-ar-11.md#Output" @ "49595a9d7cf9d52f5348cfce74cb614a7ad7e7bc"
- "cp5-task1": "specs/plans/20261023-architecture-consolidation/20261024-105657-arm-1-ar-13-cp-5.md#Task 1: Separate sprint checkpoint preparation, authorization and locked update" @ "49595a9d7cf9d52f5348cfce74cb614a7ad7e7bc"
- "a5-task5": "specs/plans/20261023-architecture-consolidation/20261024-113541-arm-1-ar-14-cp-0.md#Task 5: Separate await-verdict ownership, polling, classification and finish" @ "49595a9d7cf9d52f5348cfce74cb614a7ad7e7bc"
- "a2-boundary-o3": "specs/plans/20261023-architecture-consolidation/20261024-045004-arm-1-ar-12-cp-0.md#Spec Compliance Matrix" @ "49595a9d7cf9d52f5348cfce74cb614a7ad7e7bc"
- "goal-tests": "specs/goals/20261023-architecture-consolidation.md#Test Policy and Sequencing" @ "49595a9d7cf9d52f5348cfce74cb614a7ad7e7bc"
- "d51": "specs/goals/20261023-architecture-consolidation-decisions.md#D51 — Started successor `arm-1-ar-10-cp-1-code-1` runs before the D41 follow-up correction; bounded resubmission for the named flakes (run, 2026-10-24)" @ "ba13e745b1da3db62e2dc2475fe964bd341660f3"

### Obligation Coverage
- "PL-rule" -> "a4-proof", "a3-proof", "a2-chain"
- "PL-predecessors" -> "a4-proof", "original-task1", "d51"
- "PL-started-ruling" -> "d51", "a4-proof", "a3-proof"
- "PL-successors" -> "a4-proof", "cp5-task1", "a5-task5", "a2-boundary-o3"
- "PL-slot" -> "a4-proof", "a2-chain"
- "PL-cycle" -> "a4-proof"
- "PL-seam" -> "a4-proof", "cp5-task1", "a5-task5"
- "PL-preserve" -> "original-rca", "goal-tests"
- "PL-no-prose-order" -> "original-deps", "a4-proof"

## Correction

Based on: the amendment reason on this task; the current output manifest of
`deferred-validation-16-1-d41-correction-plan` (three slots, equal to the
committed output JSON); the original plan at `49595a9d7`; the A2, A3 and A4
correction placement rules; the cp-5 and A5 Task 5 plan sections;
`internal/models/plan_amendment.go:67-88`; `.omni-ee/pipeline.yaml:608-613`;
operator ruling D51 (`ba13e745b`, reference `d51`); and the probes below.

The plan-review check found three problems with the merged plan's
Dependencies and Shared Files section (original-deps):

- It said no `## Placement` was needed.
- It ordered Task 3 against `arm-1-ar-13-cp-5-code-0` only through a
  commutativity claim and submit-time rebase.

This correction changes two sections of the original plan file, and nothing
else in it:

1. It replaces that section with a writer table and a new `## Placement`
   section. Placement states:
   - the rule;
   - the required-successor test (R1 non-commuting writer; R2 full-suite,
     unfiltered agent/ops package run, or boundary evidence);
   - the predecessor waits, including the started required successor
     `arm-1-ar-10-cp-1-code-1` under D51;
   - the earliest legal slot;
   - six successor reservations;
   - the seam re-weigh;
   - the cycle and unborn-child analysis;
   - the orchestrator order;
   - the residual exposure.
2. It records each task's predecessor waits in its **Depends on** line:
   Task 1 waits on the merged D41 writer and on `arm-1-ar-10-cp-1-code-1`;
   Tasks 2 and 3 wait on `arm-1-ar-10-cp-1-code-1`.

Everything else in the original plan stays byte-identical:

- the RCA;
- the assertion traceability;
- every Acceptance Contract JSON block, obligation, validation array and
  deadline;
- every done_when, scope and desc;
- the D41 lineage and the operator constraints.

### Output

The adjacent output JSON copies the current three-slot manifest. Slots are
not added, reordered or redefined. The only changes are `task_depends_on`
values, which `ValidateAmendmentOutput` allows as a scheduling field
(`plan_amendment.go:76-82`):

- `output[0].task_depends_on = ["deferred-validation-2-1-correction-plan-code-0", "arm-1-ar-10-cp-1-code-1"]`;
- `output[1].task_depends_on = ["arm-1-ar-10-cp-1-code-1"]`;
- `output[2].task_depends_on = ["arm-1-ar-10-cp-1-code-1"]`.

The D51 wait is an existing concrete task outside this output, so it is a
`task_depends_on`, not a provider reservation. Children still read their `plan_ref` sections in
the original plan file, and after apply those sections resolve at this
correction's review commit.

The successor reservations are waits on existing tasks, which this plan
cannot write. The orchestrator applies them with `reserve-provider`, in the
order stated in Placement.

### Reconciliation with prior corrections

This is the first correction: `plan_amendment.corrections` lists only this
task (RP1). The prior merged D41 correction (`deferred-validation-2-1-correction-plan`)
also declared no Placement. Its placement before A3 came from the A2
boundary unit's own wait on its writer (a2-boundary-o3, O3).

This correction follows that precedent, then corrects it:

- The new reservations sit at the A1 boundary, one area earlier. That is the
  first unit whose done_when needs the same full suite (RP6).
- They also cover the two named shared-file writers, which the D41 family
  never ordered.

## Root Cause Analysis

### a. Evidence

- The original plan (original-deps, before this commit) declared "no
  `depends_on`, no `task_depends_on` and no provider wait" and "no
  `## Placement` is needed". It covered cp-5 with "submit-time rebase routes
  the textual conflict".
- `arm-1-ar-13-cp-5-code-0` names colocated `sprint_checkpoint_test.go` in
  its Files (cp-5 plan line 339). Its state is DRAFT_CODE, unassigned, with
  empty history and no provider wait (RP4).
- A4's rule (`20261023-205627-arm-1-ar-13.md:149-153`) requires the
  correction to wait for every started or merged writer, to precede every
  unstarted consumer that requires it, and to take the earliest legal slot.
  The A3 rule (`20261023-203240-arm-1-ar-12.md:163-166`) and the A2 rule
  (`20261023-195624-arm-1-ar-11.md:523-528`) say the same, and A3 qualifies
  "consumer" as one "whose done_when requires the correction".

### b. Mechanism

The plan treated only common-file writers as ordering-relevant, and it
treated their conflict as commuting edits that rebase resolves. That missed
two things:

- cp-5-code-0, and A5 code-4 on `await_verdict_test.go`, are unstarted
  non-commuting writers. Without a declared wait, either one may claim before
  the corrections and edit the uncorrected shape.
- The area-boundary units, and every A3–A5 unit with undeferred `make test`,
  need the full suite green. That is exactly where batches 15 and 16
  observed the failures.

### c. Reproduction

At `49595a9d7`, no recorded edge orders the corrections against these
successors, and their claims are not held by the corrections (RP4, RP5):

- `arm-1-ar-13-cp-5-code-0` becomes claimable as soon as its A3/A4 inherited
  waits merge.
- `arm-1-ar-11-cp-0-code-0` becomes claimable when A1 code-1 and code-2
  merge.

Placement's reservations are what make this state impossible.

### d. Verification scope

Checked:

- the state of every open coding task, and its validation commands with
  their deferred indexes taken from its Acceptance Contract (RP6);
- dependency ancestry (RP5);
- the started task's diff (RP3);
- the D51 ruling and the started task's current state and ancestry (RP10);
- the last writers of the three files (RP2);
- the per-subtask transition (RP7);
- the amendment freeze rule (RP8).

Not checked:

- Future children of the five DRAFT_CODING_PLAN plans (A6 onward). They
  inherit A5 coding through whole-phase inheritance (`a4-proof`), so they
  are ordered after the reserved successors. This is unverified until they
  are planned.
- Whether a future dependency repair keeps the descent paths. Placement
  requires that any such repair re-check it.

### e. Sufficiency

With the predecessor waits and the six reservations, no unstarted required
successor can claim before children 0–2 merge. Every other open A2–A5
consumer, including the unfiltered package-race units
`arm-1-ar-11-cp-4-code-0/6/7/8`, descends from a reserved successor through
recorded edges (RP5).

The one started required successor, `arm-1-ar-10-cp-1-code-1`, cannot be
preceded: its undeferred index 1 runs unfiltered agent/ops races (RP3,
RP6). The rules route that case to a human ruling, and D51 (`d51`) rules
option A: it runs first, and the children wait on it through
`task_depends_on` (RP10). Its exposure to the four flakes before the
corrections merge is accepted by D51, bounded by D51's resubmission rule.
That exposure is a ruled exception, not a gap in this placement.

Residual: if that task blocks under D51's disproof limit, the children
hold with the A1 boundary until the operator applies D51's revert
(option B). No unruled path lets an unstarted required successor claim
first.

### f. Adjacent findings (recorded, not planned)

None new. The original RCA f. stands unchanged.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Must: a reviewed `## Placement` under the correction rule | amendment reason; a4-proof | Placement (all outputs) | Covered |
| 2 | Must: identify all affected chain consumers, auditing earlier A3/A4 canonical ops-test consumers and the cp-5 shared-file writer | amendment reason | Placement (R1/R2 test, successor table, descent rule); RP4–RP6 | Covered |
| 3 | Must: earliest legal insertion slot | amendment reason; a4-proof; a2-chain | Placement (Earliest legal slot) | Covered |
| 4 | Must: predecessor waits on every started or merged writer | amendment reason; a4-proof; d51 | Tasks 1–3 (`task_depends_on`); Placement (Predecessors) | Covered |
| 5 | Must: successor reservations through the configured per-subtask transition | amendment reason | Placement (`code-plan-to-coding`, outputs 0–2); RP7 | Covered |
| 6 | Must: cycle and unborn-child analysis; no dependency on descendants that would prevent producing the correction | amendment reason | Placement (Cycle and unborn-child analysis); RP5 | Covered |
| 7 | Must: no ordering by prose commutativity or rebase | amendment reason | Dependencies and Shared Files; Placement | Covered |
| 8 | Must: three output identities, test-only scopes, assertions, deadlines, acceptance commands and D41 lineage preserved | amendment reason; goal-tests | Tasks 1–3 (frozen slots); RP8 | Covered |
| 9 | Must: reconcile prior merged correction artifacts | amendment reason | Reconciliation with prior corrections | Covered |
| 10 | Must: seam re-weighed with the correction included | a4-proof | Placement (Seam re-weighed) | Covered |
| 11 | Must: no started required successor overtaken; a human ruling if one exists | a4-proof; a3-proof; d51 | Tasks 1–3 (`task_depends_on: arm-1-ar-10-cp-1-code-1`); Placement (Predecessors: Ruling D51; Earliest legal slot; Orchestrator order step 2); RP3, RP10 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: planning correction; children stay test-only (unchanged allocation) | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: only plan artifacts change | N/A |

## Reality Probes

Integration SHA `49595a9d7cf9d52f5348cfce74cb614a7ad7e7bc` (worktree HEAD
equals `global-refactoring`), 2026-10-24 12:00–12:40 UTC.

| # | Claim / command | Observed excerpt and significance |
|---|---|---|
| RP1 | `omni-ee get deferred-validation-16-1-d41-correction-plan --json`; `jq -S` diff of `.result.output` against the committed output JSON | MERGED (`49595a9d7`), no depends_on. Three slots, identical to the committed JSON (`SAME`). `plan_amendment.corrections`/`pending` = this task only. No `-code-N` child exists, so the children are unborn. |
| RP2 | `git log --first-parent --since=2026-10-22 -- <three files>`; `git log -1` per file | `aaf2c5e33` Merge deferred-validation-2-1-correction-plan-code-0 (inflight). `await_verdict_test.go` last written by `9c685b643` and `sprint_checkpoint_test.go` by `1508df457`, both direct commits with no task. |
| RP3 | `git -C .worktrees/arm-1-ar-10-cp-1-code-1 diff --name-only global-refactoring...HEAD -- <three files>`; same without range; `status --short`; its `validation[1]` | Both diffs empty; status lists only `internal/ops/rejection_rca_test.go`. The only started task writes none of the files. Its plan defers only indexes `[3, 4]`. Index 1, `go test -race -timeout 30m -p 2 -count=1 … ./internal/agent/ ./internal/ops/ …` with no `-run` filter, stays required, so the task is a started required successor. |
| RP4 | `omni-ee get <id> --json` for the six successors; `omni-ee get tasks --all --json` statuses | Each successor is DRAFT_CODE, unassigned, with history 0 and provider_dependencies 0. Open coding units: 58 DRAFT_CODE, plus 1 IMPLEMENTING_CODE (`arm-1-ar-10-cp-1-code-1`). |
| RP5 | Ancestry script over `depends_on` ∪ `task_depends_on` of all tasks | Units not descending from `arm-1-ar-11-cp-0-code-0`: only `arm-1-ar-10-cp-1-code-1` and `-code-2`. Units not descending from `arm-1-ar-12-cp-0-code-0`: only A1 and A2 units. The provider and the D41 writer have no dependencies, so no successor is their ancestor. |
| RP6 | Audit script: each open unit's `validation[]`, minus the Acceptance Contract `deferred` indexes from its `plan_ref` section | Undeferred `make test`: the A1–A4 boundary units, and every A3–A5 refactor unit except `12-cp-4-code-3` and `13-cp-5-code-2/-3`, which defer it. A2 refactor units and A1 units defer `make test`. Undeferred focused agent/ops races: A1 code-1 and A2 `11-cp-4-code-0/6/7/8`. File mentions: cp-5 Task 1 names `sprint_checkpoint_test.go`, and A5 Task 5 owns `await_verdict.go` and its white-box tests (lines 333, 756-762). |
| RP7 | `.omni-ee/pipeline.yaml:608-613` | `code-plan-to-coding`, `from: code-planning-pair.approved`, `trigger: manual`, `cardinality: per-subtask`. |
| RP8 | `plan_amendment.go:67-88`; `jq -S` comparison of the correction JSON without `task_depends_on` against the current manifest (rerun 2026-10-24T12:57Z after the D51 waits were added) | The freeze nils only dependency, inheritance, validation, prerequisite and runtime-input fields. The comparison returned `FROZEN-EQUAL`, so slot identities are preserved. |
| RP9 | `omni-ee validate-payload set-task-output --payload <correction output JSON> --json` | `ok: true`, `COMPLETED`, no diagnostics. Rerun 2026-10-24T12:57Z with the D51 waits: `ok: true`, `COMPLETED`, `continue`, no diagnostics. |
| RP10 | At `ba13e745b` (D51 commit, worktree rebased onto it), 2026-10-24T12:55Z: `omni-ee get <id> --json` for `arm-1-ar-10-cp-1-code-1`, `-code-2`, `-code-3` and the six successors; D51 section of the decisions file | `arm-1-ar-10-cp-1-code-1` IMPLEMENTING_CODE (coder-1); its `depends_on` lists only `arm-1-ar-0`…`arm-1-ar-10` units, no `task_depends_on`, no provider wait. `-code-2` DRAFT_CODE; `-code-3` MERGED. `arm-1-ar-11-cp-0-code-0` `depends_on` includes `arm-1-ar-10-cp-1-code-1` and `-code-2`. The six successors are still DRAFT_CODE, unassigned, with empty history and no provider wait. D51 rules option A and directs the children to wait on `arm-1-ar-10-cp-1-code-1`. |
