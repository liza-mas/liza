# Bug Prevention Discipline — Run Decision Record

Companion to the goal document `20261114-bug-prevention-discipline.md`.
Records rulings made during the multi-agent run. Human and reviewer record; not an input to `init --spec`.

## Delegation in force

- **Standing delegation for rulings** (omni-ee-operator default): the operator settles a ruling a
  human teammate in its seat would, and escalates the listed exceptions.
- **Auto-approval** active from 2026-11-14T11:59Z on the human's words "auto-approve until explicit
  turn off". It ends only at an explicit turn-off. The exception list is decided on the operator's
  recommendation; still held for the human: irreversible or destructive moves (merge to main, live
  data), Tier 0, credential reads, agent or TUI restarts, permission-layer denials.
- **Recording** (human, 2026-11-14T13:12Z, "do both"): each ruling is also recorded in the OMNI
  Decision Map as a Decision (maker: the operator, declared) and as a call for decision for the
  human to approve.

## Rulings

### R1 — Clear the hold on plan `arm-1-ar-4-cp-0`; the whole-oracle red comes from the environment, not the baseline
- **Decision:** clear the plan-check hold. The child tasks' declared validation (`env -i HOME="$HOME"
  PATH="$PATH" TMPDIR=/tmp go test -count=1 ./internal/oracle/...`) and the cleanup assertion stay
  unchanged.
- **Attribution:** operator, auto-approved (2026-11-14T13:26Z). Relayed as a human note on
  `arm-1-ar-4-cp-0` (13:26:12Z) after `plan-check --clear` (verdict none, class needs_review).
- **Decision Map:** call for decision `d4308425-d5e6-4e04-9fff-380f087a0cea` (open, owner: the
  human). The Decision entry was refused, `409 ai_provider_unavailable` (`default_provider_missing`):
  the workspace has no default AI provider. Retry once one is configured.
- **Kind:** check-only. No delivery change, and no check removed or weakened.
- **Intent relied on:** the hold's own release condition: "provide a reviewed repair or a demonstrated
  environment correction for the baseline failure in TestOracleA9Restart42, then successfully run
  [the command] … Preserve the cleanup assertion and canonical whole-oracle acceptance command; do
  not skip or weaken them." Goal, Proof Environment: "Fixture proofs run in the normal test suite".
- **Root cause (checked against source):** `internal/oracle/a9/signals_test.go:39-66` reaps the
  scenario's process tree through a child subreaper and fails when a started process survives
  `signalReap`. `TestOracleA9Restart42` (`internal/oracle/a9/lifecycle_test.go:94`) runs through
  `isolated(t)`. The orchestrator's two red runs (2.47 s, 2.49 s) ran inside its codex agent session,
  through `omni-ee tool-result run --tool Bash --command 'rtk env -i … go test …'`
  (`.omni-ee/agent-outputs/orchestrator-1-20261114-131048.txt:112`). DERIVED, not traced: an extra
  process layer from the wrapper or sandbox changes subreaper and reap behavior. The test code is
  unchanged from the merged baseline.
- **Probe results:** at integration HEAD `0dc17f431`, in a temporary detached worktree after
  `make sync-embedded`, from a plain host shell: the isolated `-run 'TestOracleA9Restart42$'
  ./internal/oracle/a9/` passed (6.7 s). The exact whole command passed too, every package `ok`
  (`a9` 88.4 s), 13:21:43–13:24:16Z.
- **Rejected alternatives:** a correction task for the a9 cleanup (outside both children's scope, and
  not shown to be a code defect); weakening or skipping the cleanup assertion (forbidden by the hold
  and by T0.3); keeping the hold (blocks Scope 4 implementation with no repair to wait for).
- **Risk and revert:** if a child's whole-oracle run fails only at this cleanup inside an agent
  session, that is a recurrence of this environment cause (operator defect D-5) and goes to the
  operator; it is not a child defect. Revert path: the human re-holds the plan
  (`plan-check` hold) or orders the a9 cleanup investigated as its own task.
- **Human:** approved (15:38Z). The Decision Map call d4308425 stays open: closing it needs a recorded Decision, which the workspace refuses without an AI provider (D-6).

### R2 — Raise the run-wide agent cap from 9 to 10
- **Decision:** `config.max_agents` 9 → 10, so one more doer can start while reviewers and
  waiting doers hold their slots. Per-role caps (`max_instances` 3) unchanged.
- **Attribution:** operator, auto-approved (2026-11-14T14:11Z).
- **Kind:** operational (provider load). No delivery change, no check changed.
- **Intent relied on:** the operator's objective "keep the run progressing smoothly", and the
  friction trigger "the third occurrence of one … environment cause" (RUN CAP alerts 13:10:42Z
  and 14:10:04Z, and the 13:59Z hold of `arm-1-ar-6`).
- **Evidence:** at 14:10Z, `repair-agent-pool --dry-run`: coder "held by the run-wide cap
  (max_agents 9: 8 of 8 non-orchestrator slot(s) occupied)" with `arm-1-ar-1-cp-0-code-0` (FR1
  submit gate) claimable. All 8 slots held working or review-waiting agents, none idle. No
  provider quota signal files, and 0 rate-limit or quota mentions in the supervisor logs.
- **Rejected alternatives:** lowering `doer_max_wait`/`reviewer_max_wait` (no idle agents at
  14:10Z, so it frees nothing); waiting (the slot frees only at a coder's task boundary, while the
  FR1 gate code sits on the critical path to Scope 7).
- **Decision Map:** call for decision `939c6cfd-83cb-4c66-8cb1-e939263120b8` (open, owner: the human).
- **Revert path:** `omni-ee config set config.max_agents 9 --replace --reason "..."`; extra agents drain at their next
  boundary.
- **Human:** approved (15:38Z). The Decision Map call 939c6cfd stays open: closing it needs a recorded Decision, which the workspace refuses without an AI provider (D-6).

### R3 — `arm-1-ar-4-cp-0-code-1` splits `historyfixture.Cases()` in place to restore the size guard
- **Decision:** grant `arm-1-ar-4-cp-0-code-1` a bounded scope exception for
  `internal/historyfixture/cases.go`: split the 109-line `Cases()` into helpers of at most 100
  lines, with the same data, order and behavior. No S2 or ratchet row is added.
- **Attribution:** orchestrator (`unblocked` 2026-11-14T14:40:46Z, narrower in-task fixture
  exception). Operator verified and concurs (14:41Z); the operator's own unblock was refused
  ALREADY_TRANSITIONED, so no operator mutation.
- **Kind:** check-only repair of integration. The size guard is kept, not loosened.
- **Intent relied on:** GUARDRAILS G2 defaults and the archguard S1 rule ("reduce it, or add an
  S2 entry"). The doer's offer in its block: "Bounded fix I can do: split the 109-line Cases() …
  no data or behavior change".
- **Root cause (checked):** merged `arm-1-ar-5-cp-0-code-1` (commit `8cbfce103`) added `Cases()`
  at 109 lines. Its validation did not include the archguard command. Reproduced at integration
  HEAD `b3c5e37c6` in a temporary worktree: `TestS1SizeGuard` FAIL, `s1_guard_test.go:250`.
- **Rejected alternatives:** an S2/ratchet row (forbidden by the doer's exception terms and
  loosens the guard); a correction task in the `arm-1-ar-5` lineage (one plan, one review and a
  coding task to split one function, while integration stays red for every task that runs
  archguard).
- **Revert path:** revert the split commit; the guard then fails again.

### R4 — Accept the reviewer-context growth from FR1/FR3 and make role-budget regeneration an owned step
- **Decision:** the measured growth in mandatory reads is accepted as FR1/FR3 content:
  code-reviewer 65 734 → 74 623 B (+13.5%) and integration-analyst 40 913 → 49 802 B (+21.7%),
  from the code-review skill's link to `skills/shared/references/prevention-record.md`
  (merged `3f38988df`). The remedy for the `TestRoleBudget` red is a deliberate
  `ROLEBUDGET_UPDATE=1` regeneration, committed by `arm-1-ar-2-cp-0`'s rules child (U1) after its
  own contract edits, with the per-variant byte table in the commit body. Every later task in this
  run that grows a role's rendered bytes or mandatory reads regenerates the same way, in the same
  commit (as G2.5 for oracle goldens). No binding rule is trimmed to pass.
- **Attribution:** operator, auto-approved (2026-11-14T14:56Z).
- **Kind:** check process (baseline update through the test's designed path). The 5% gate stays
  on for all future growth.
- **Intent relied on:** `rolebudget_test.go:33-37` ("Regenerate deliberately with
  ROLEBUDGET_UPDATE=1"; failure text "Report the exact overrun for a scoped decision; do not remove
  binding rules to pass"). Goal limit "no contract growth beyond the semantic content added
  (G2.2)": the growth is the reviewer's FR1 substance check reading the record semantics, which
  Scope 3 ASM-001 says the reviewer needs.
- **Probe:** at integration `0ed2fde77`, temporary worktree, `make sync-embedded`,
  `env -i … go test -count=1 -run 'TestRoleBudget$' ./internal/prompts/rolebudget/` → FAIL with
  exactly the two rows above. No other variant is over.
- **Rejected alternatives:** splitting the record document so reviewers link a short semantics
  file (touches merged Scope 0 and Scope 3 work and the FR1 gate's installed-document pointer;
  cost out of proportion for ~9 KB); a reviewed amendment that designs a new budget mechanism
  (the orchestrator's open route: it would end in this same regeneration, after a plan, a review
  and a coding task); waiving the gate (forbidden).
- **Relayed:** human notes on `arm-1-ar-2-cp-0-amend-1` and `arm-1-ar-7-amend-1` (1075 B each).
- **Decision Map:** call for decision `a11ef1ca-742e-4204-a6ba-f1ebe41dda48` (open, owner: the human).
- **Revert path:** a later trim task that shrinks the linked document or the link, followed by a
  regeneration.
- **Human:** approved (15:23Z). The Decision Map call stays open: closing it needs a recorded Decision, which the workspace refuses without an AI provider (D-6). The human can also reverse R4 and order that trim now.

### R5 — Re-pin `adr-index` in the merged Scope 2 code plan after Scope 4's ADR-0032 row amendment
- **Decision:** in `specs/plans/20261114-bug-prevention-discipline/20261114-141728-arm-1-ar-2-cp-0.md`,
  re-pin the direct reference `adr-index` from `2768c0e8b` to `e2fa4c4d4` (operator commit on the
  integration branch, as `.omni-ee/SUPPORT.md` prescribes for "approved proof of the task's own
  allocation, pin left behind"). No other line changes.
- **Attribution:** operator, delegated (2026-11-14T15:15Z).
- **Kind:** check carrier only. The obligation `S2.U2.INDEX-ROW` (row 0251 directly after 0250)
  is unchanged.
- **Cause (checked):** `arm-1-ar-2-cp-0-code-1` BLOCKED 15:14:24Z, "direct reference "adr-index"
  is stale at integration HEAD". Between the pin and HEAD the section changed in one commit,
  `0679124c9` (Scope 4, FR8): the ADR-0032 row gained "Amended 2026-11-14: default invariant
  home…", which `arch-files` allocates to Scope 4. That is an extension and does not affect the
  0251 row.
- **Follow-up:** the orchestrator runs `reaffirm-proof arm-1-ar-2-cp-0-code-1 adr-index` if
  acceptance refuses the moved proof, then `unblock-task`.
- **Correction (15:23Z):** that follow-up was wrong. `reaffirm-proof` refuses this task ("no merged planning parent that allocates it", after the applied amendment `arm-1-ar-2-cp-0-amend-1`). The re-pin alone cleared the staleness; the operator ran `unblock-task` directly. If acceptance later refuses the moved proof, the task blocks naming R5 and the operator takes it.
- **Revert path:** restore the old pin; the task blocks again.

### R6 — Authorize a reviewed amendment of `arm-1-ar-2-cp-0` that only accepts the `adr-index` re-pin
- **WITHDRAWN (2026-11-14T15:26Z):** the route does not exist. `amend-plan` refuses a planning original that has children ("amendment requires an unused MERGED planning original without children or executed transitions"). The task stays BLOCKED; the repair is escalated to the human.
- **Decision:** the orchestrator opens `amend-plan arm-1-ar-2-cp-0` whose only change is
  accepting the `adr-index` pin at `53f084838`. After review: `amend-plan --apply`, verify claim
  acceptance for `arm-1-ar-2-cp-0-code-1`, then unblock it.
- **Attribution:** operator, auto-approved (2026-11-14T15:26Z). The orchestrator asked for a human
  action (AWAITING HUMAN 15:25:54Z).
- **Kind:** check carrier, reviewed. No delivery change.
- **Cause (checked):** `internal/ops/acceptance_declaration.go:152-181` requires every reference of
  the reviewed contract to resolve to reviewed content. R5's unreviewed re-pin moved
  `adr-index` (an obligation reference) to Scope 4's amended ADR-0032 row, and `reaffirm-proof`
  covers approved proofs only (refused 15:20Z). A reviewed amendment is the remaining
  supported path.
- **Rejected alternatives:** reverting R5 (back to the stale-reference prompt block); superseding
  code-1 (a heavier route with the same review need); a waiver (none exists, and it would weaken
  provenance).
- **Revert path:** withdraw the pending amendment (`amend-plan --withdraw-pending`); code-1 stays
  blocked.
