# Prevent Blocked-Task Chains — validation manifest

Goal: `specs/goals/20260913-prevent-blocked-tasks-chains.md` (local, ignored; the rubric is its
§Behavioral scenarios, §counterexamples and §Checkable release rules, pinned by the goal's own
delivery-status header of 2026-09-18). Blackboard:
`specs/adversarial-pairing/20260913-prevent-blocked-tasks-chains-bb.md`.

## Commits

| Package | Commit | Content |
| --- | --- | --- |
| W0 | commit 0 on `prevent-blocked-task-chains` | `internal/prompts/rolebudget` baseline (gate off) |
| W1 | commit 1 | shared authoring rules in `skills/` |
| W2 | commit 2 | planner/reviewer duties in `internal/prompts/templates/blocks` |
| W3 | commit 3 | orchestrator wakes, blocking protocol, delivery report |
| W4 | commit 4 | `skills/liza-operator/SKILL.md` |
| W5a | commit 5 | this directory; `rolebudget/baseline.json` regenerated with `gate: true` |

Base: `main@7ce4b25b377c123cc304d2d69e1ab5a1288d76c2`. Exact commit SHAs are recorded in the
blackboard's audit fields when the packages are cut at `READY_TO_COMMIT`.

## Role variants and context budget

**Pre-change anchor (commit 0, `rolebudget` measurement, `gate: false`).** Rendered bytes = base
prompt + role sections with the reference-context payload blanked; mandatory reads = `skills:` +
`mandatory-docs` + shared references linked from those skills, each file once.

| Variant | Rendered | Base prompt | Mandatory reads |
| --- | --- | --- | --- |
| architect | 22,285 | 9,076 | 17,757 |
| architect (decomposition root) | 24,542 | 9,076 | 17,757 |
| architecture-reviewer | 19,578 | 9,112 | 26,075 |
| architecture-reviewer (decomposition root) | 21,529 | 9,112 | 26,075 |
| code-plan-reviewer | 22,776 | 9,103 | 57,383 |
| code-plan-reviewer (decomposition root) | 20,535 | 9,103 | 57,383 |
| code-planner | 27,210 | 9,085 | 31,308 |
| code-planner (decomposition root) | 21,721 | 9,085 | 31,308 |
| code-reviewer | 23,586 | 9,088 | 65,404 |
| coder | 22,705 | 9,064 | 26,862 |
| epic-plan-reviewer | 21,520 | 9,103 | 26,075 |
| epic-plan-reviewer (decomposition root) | 23,471 | 9,103 | 26,075 |
| epic-planner | 22,710 | 9,085 | 36,241 |
| epic-planner (decomposition root) | 24,967 | 9,085 | 36,241 |
| integration-analyst | 23,861 | 9,106 | 39,733 |
| integration-reviewer | 23,235 | 9,109 | 40,419 |
| orchestrator (wake INITIAL_PLANNING) | 12,210 | 8,955 | 9,111 |
| orchestrator (wake BLOCKED_TASKS) | 15,516 | 8,955 | 9,111 |
| orchestrator (wake HYPOTHESIS_EXHAUSTED) | 11,758 | 8,955 | 9,111 |
| orchestrator (wake IMMEDIATE_DISCOVERY) | 10,343 | 8,955 | 9,111 |
| orchestrator (wake PLANNING_COMPLETE) | 9,702 | 8,955 | 9,111 |
| orchestrator (wake MANY_TO_ONE_READY) | 9,729 | 8,955 | 9,111 |
| orchestrator (wake CODING_COMPLETE) | 9,278 | 8,955 | 9,111 |
| orchestrator (wake SPRINT_COMPLETE) | 9,475 | 8,955 | 9,111 |
| orchestrator (wake HUMAN_NOTE) | 9,918 | 8,955 | 9,111 |
| us-reviewer | 20,732 | 9,082 | 16,964 |
| us-writer | 18,704 | 9,076 | 22,873 |

**After W1–W4.** This commit regenerates `baseline.json` from the after-figures with `gate: true`;
the 5% ceiling now applies to every later edit against that floor.

| Variant | Rendered before → after | Δ | Mandatory reads before → after | Δ |
| --- | --- | --- | --- | --- |
| architect | 22,285 → 22,815 | +2.4% | 17,757 → 19,612 | +10.4% **OVER** |
| architect (decomposition root) | 24,542 → 25,285 | +3.0% | 17,757 → 19,612 | +10.4% **OVER** |
| architecture-reviewer | 19,578 → 20,460 | +4.5% | 26,075 → 28,149 | +8.0% **OVER** |
| architecture-reviewer (decomposition root) | 21,529 → 22,602 | +5.0% | 26,075 → 28,149 | +8.0% **OVER** |
| code-plan-reviewer | 22,776 → 23,769 | +4.4% | 57,383 → 59,457 | +3.6% |
| code-plan-reviewer (decomposition root) | 20,535 → 20,726 | +0.9% | 57,383 → 59,457 | +3.6% |
| code-planner | 27,210 → 27,778 | +2.1% | 31,308 → 31,308 | +0.0% |
| code-planner (decomposition root) | 21,721 → 21,934 | +1.0% | 31,308 → 31,308 | +0.0% |
| code-reviewer | 23,586 → 24,067 | +2.0% | 65,404 → 65,404 | +0.0% |
| coder | 22,705 → 23,328 | +2.7% | 26,862 → 26,862 | +0.0% |
| epic-plan-reviewer | 21,520 → 22,263 | +3.5% | 26,075 → 28,149 | +8.0% **OVER** |
| epic-plan-reviewer (decomposition root) | 23,471 → 24,405 | +4.0% | 26,075 → 28,149 | +8.0% **OVER** |
| epic-planner | 22,710 → 22,949 | +1.1% | 36,241 → 38,097 | +5.1% **OVER** |
| epic-planner (decomposition root) | 24,967 → 25,419 | +1.8% | 36,241 → 38,097 | +5.1% **OVER** |
| integration-analyst | 23,861 → 24,484 | +2.6% | 39,733 → 39,733 | +0.0% |
| integration-reviewer | 23,235 → 23,235 | +0.0% | 40,419 → 40,419 | +0.0% |
| orchestrator (wake INITIAL_PLANNING) | 12,210 → 12,210 | +0.0% | 9,111 → 9,111 | +0.0% |
| orchestrator (wake BLOCKED_TASKS) | 15,516 → 18,309 | +18.0% | 9,111 → 9,111 | +0.0% **OVER** |
| orchestrator (wake HYPOTHESIS_EXHAUSTED) | 11,758 → 12,559 | +6.8% | 9,111 → 9,111 | +0.0% **OVER** |
| orchestrator (wake IMMEDIATE_DISCOVERY) | 10,343 → 11,036 | +6.7% | 9,111 → 9,111 | +0.0% **OVER** |
| orchestrator (wake PLANNING_COMPLETE) | 9,702 → 10,655 | +9.8% | 9,111 → 9,111 | +0.0% **OVER** |
| orchestrator (wake MANY_TO_ONE_READY) | 9,729 → 10,682 | +9.8% | 9,111 → 9,111 | +0.0% **OVER** |
| orchestrator (wake CODING_COMPLETE) | 9,278 → 10,381 | +11.9% | 9,111 → 9,111 | +0.0% **OVER** |
| orchestrator (wake SPRINT_COMPLETE) | 9,475 → 10,459 | +10.4% | 9,111 → 9,111 | +0.0% **OVER** |
| orchestrator (wake HUMAN_NOTE) | 9,918 → 9,918 | +0.0% | 9,111 → 9,111 | +0.0% |
| us-reviewer | 20,732 → 21,406 | +3.3% | 16,964 → 19,038 | +12.2% **OVER** |
| us-writer | 18,704 → 18,919 | +1.1% | 22,873 → 24,547 | +7.3% **OVER** |

**Exact overruns against the 5% per-variant ceiling, for the human's scoped decision** (goal
§Context budget: "report the exact overrun for a scoped budget decision before shipping; never
remove binding rules merely to pass"):

- W1 (mandatory reads): us-reviewer +12.2%, architect +10.4%, architecture-reviewer and
  epic-plan-reviewer +8.0%, us-writer +7.3%, epic-planner +5.1%. Cause: the shared
  `reference-first-authoring.md` grew +1,178 B after ~2.5 KB of compositor-internals prose was
  absorbed (G2.3 cut table on the blackboard); small read sets exceed 5% on that alone.
- W3 (rendered orchestrator wakes): BLOCKED_TASKS +18.0%, CODING_COMPLETE +11.9%,
  SPRINT_COMPLETE +10.4%, PLANNING_COMPLETE and MANY_TO_ONE_READY +9.8%, HYPOTHESIS_EXHAUSTED +6.8%,
  IMMEDIATE_DISCOVERY +6.7%. Cause: the recovery table, non-convergence check and the delivery
  report the goal assigns to exactly these templates, on 320–850 B of instruction over a ~9 KB
  base prompt.
- W4: `skills/liza-operator/SKILL.md` 25,253 → 28,259 B (+11.9%); the operator is not a pipeline
  role, reported as touched-file bytes.
- All other variants within the ceiling (W2's largest: architecture-reviewer root +4.98%).

Doer and reviewer both recommend accepting these exact figures as a recorded per-package decision
with no rule change; the disposition is recorded under the blackboard's Decisions.

## Inputs

`inputs/<case>/` — one packet per case: `request.md` (roles, request), `state.json` (minimal
sanitized state), `receipts.md` (read-only command receipts, or none). Every packet whose roles
include the orchestrator carries a `Wake trigger:` header line naming the instruction template the
runner renders for it (`INITIAL_PLANNING`, `BLOCKED_TASKS`, `HYPOTHESIS_EXHAUSTED`,
`IMMEDIATE_DISCOVERY`, `PLANNING_COMPLETE`, `MANY_TO_ONE_READY`, `CODING_COMPLETE`,
`SPRINT_COMPLETE`, `HUMAN_NOTE`); it is runner metadata, stripped with the rubric row, never shown to
the agent. V12 is `PLANNING_COMPLETE`. 24 required release cases
(V1–V18, V21, V22, C1–C4) plus V19/V20/V23 stubs marked W7 EVIDENCE GATE. The fixture domain is a
generic field-service kiosk product; no observed run's names, devices, timings, counts or task IDs
appear (goal §Observed failure pattern: "must not become … prompt examples that prescribe a stack").

## Protocol trial (V1, V5, V12 against the unmodified baseline)

Purpose per goal §W5 protocol: prove inputs can be supplied, outputs retained, and each expected
behavior scored. Baseline FAIL is useful evidence; this is not a candidate exercise.

- Instructions: rendered role prompts from `main@7ce4b25b` (a temporary detached worktree, a
  throwaway `_test.go` rendering `BuildBasePrompt` + `BuildRoleContext` /
  `buildInstructionsForWakeTrigger` with empty run data; worktree removed afterwards) plus the
  role's `skills:` files at that commit via `git show`.
- Sessions: `claude -p --no-session-persistence --tools "" --max-turns 1 --model claude-sonnet-5`,
  one fresh session per role, on this host; `2.1.258 (Claude Code)`; run 2026-09-18.
- Retained: `observations/protocol-trial/<case>-<role>.prompt.txt` (complete supplied prompt; the
  repository's end-of-file hook appended one trailing newline to four of them after the run) and
  `.output.txt` (complete output). SHA-256 of the retained files:

```
e573a34c248fcd7c2307e457f99f7767c95f8acb9950c7f51f6543801de13f32  V12-orchestrator.output.txt
3c88d9ffcbf869bc9b8bf42c10d5a5cb99e4b019c9e03e1e967324b5b96d1d8c  V12-orchestrator.prompt.txt
7f83e349127df3fbb3ae0db719876821f1b1c28bddeacff19b94b084abfafd82  V1-architect.output.txt
d9d44558d0cb2b390857f72e00832c24a6bc7a2adf84b32ae2f3786c8836f293  V1-architect.prompt.txt
5e274a3e20e5c4c1466cb35ac1f2186306838e9babae18a4735a94579ac2969b  V5-code-planner.output.txt
667054c2aba19be92a41ac030eb49836c6636611d9aea6e4d57fd93c4117386c  V5-code-planner.prompt.txt
a505aa845aeaa025d212124c5cfaceff57a384f3d1b9116efa6846b0b17595e1  V5-orchestrator.output.txt
5a5d0274bb91016f602882c41466cd3b6671fa3afd4af5722bd0438db003c13b  V5-orchestrator.prompt.txt
```

Observations for the scorer (protocol-trial scoring is "evidence complete and scoreable", not
behavior):

- V1 (architect, baseline): output is a bounded architecture decision with explicit assumption
  handling — scoreable against the V1 row.
- V5 (code-planner and orchestrator, baseline): both explain edge origin as default
  `inherit_inputs` inheritance and propose the supported repair — scoreable; note that the
  baseline already contains W6 (`inherit_inputs`), so V5's "prepares a supported repair" clause is
  met at baseline.
- V12 (orchestrator, baseline): **fixture correction needed before the candidate run** — the trial
  rendered the SPRINT_COMPLETE wake, whose premise the packet contradicts; the model refused the
  checkpoint on that ground and produced a watch report anyway. The candidate exercise must render
  the PLANNING_COMPLETE (or BLOCKED_TASKS) wake for this packet. The retained output remains
  scoreable for the report clauses.
- **Session-setup caveat for W5b:** fresh `claude -p` sessions on this host load the user's global
  `~/.claude/CLAUDE.md` pairing contract; the outputs cite its rules. Candidate exercises must run
  with a clean `HOME` (or an equivalent settings isolation) so that only the delivered instructions
  are in scope, and record that setup here.

## W5b (not started here)

The 24 candidate exercises, the independent reviewer's score table with evidence pointers per
clause, and the PASS/FAIL release decision run against a built-and-installed candidate from the
topic branch, in a follow-on blackboard. Until then the instruction release is a candidate, not
released (goal §Rollout).

Session isolation for every candidate exercise (records the setup here when run):

```
CAND_HOME=$(mktemp -d)                       # no ~/.claude, no global contract, no MCP config
mkdir -p $CAND_HOME/.claude
cp ~/.claude/.credentials.json $CAND_HOME/.claude/   # credentials only — no CLAUDE.md, no settings
                                             # (or export ANTHROPIC_API_KEY instead of copying)
make -C <topic-worktree> build               # candidate binary from the topic branch
HOME=$CAND_HOME <topic-worktree>/bin/liza setup   # installs the candidate contracts/skills under $CAND_HOME
HOME=$CAND_HOME claude -p --no-session-persistence --tools "" --max-turns 1 \
  --model <pinned model> < <case>-<role>.prompt.txt > <case>-<role>.output.txt
```
Verify isolation before the first case: `ls $CAND_HOME/.claude` must show the credentials file only,
and a probe session must not cite any rule from the host's global contract.

The prompt file is the rendered candidate role instructions (same rendering route as the trial,
from the topic branch) + the role's `skills:` files as installed under `$CAND_HOME` + the packet
body from `## Request` on; record `liza version`, `claude --version`, the model, and the SHA-256 of
every prompt and output file in this manifest.
