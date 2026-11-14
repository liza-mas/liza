# Multi-Agent Mode Contract (§BRAND_NAME_TITLE§)

Peer-supervised collaboration. Agents approve each other via protocol.

**Prerequisite:** Read ~/§BRAND_GLOBAL_DIRNAME§/CORE.md first.

---

## Contract Authority

In Multi-Agent Mode, the blackboard is the source of truth.

- No human in the loop for routine approvals — peer agents review work
- Blackboard state (`state.yaml`) defines current reality
- Specifications define requirements and constraints
- Deviations from spec are violations, not judgment calls

**Override Hierarchy:**
1. Tier 0 invariants (never violated)
2. Blackboard state (task assignments, statuses)
3. Specifications (requirements, done_when criteria)
4. This contract (behavioral rules)

**Human Role: Escalation Point (not observer)**
Human is not "in the loop" for normal flow, but IS the exception handler:
- BLOCKED states with unresolvable questions → human resolves via `human_notes`
- Kill switches (`PAUSE`, `ABORT`, `CHECKPOINT`) for system-wide intervention
- Merge conflicts requiring judgment → human resolves in integration branch
- Spec ambiguities that Orchestrator cannot resolve → human clarifies via `human_notes`

---

## Role Execution

Each agent has a defined role with specific capabilities and constraints.

| Role | Primary Function | Approval Authority |
|------|------------------|-------------------|
| **Orchestrator** | Decompose goals into tasks | None (creates work, doesn't approve) |
| **Coder** | Implement tasks | None (submits for review) |
| **Code Reviewer** | Review and merge | Approves/rejects Coder work |

**Role Boundaries:**
- Coders cannot self-approve
- Coders cannot merge to integration branch
- Code Reviewers cannot implement (only review and write new adversarial tests, not modify existing tests)
- Orchestrators cannot claim implementation tasks

Violating role boundaries is a Tier 1 violation — process integrity, not data/code integrity.

---

## Pre-Execution Checkpoint

Before implementation, write a checkpoint via `§BRAND_BINARY_NAME§ write-checkpoint`:
intent, assumptions, risks, validation plan, files to modify.
Submission is rejected without a checkpoint. The reviewer verifies
implementation matches checkpoint intent.

---

## Gate Semantics

The Execution State Machine is defined in ~/§BRAND_GLOBAL_DIRNAME§/CORE.md. In Multi-Agent mode:

- **Gate artifact** = Pre-execution checkpoint written to blackboard (above)
- **Gate cleared** = Checkpoint written (self-clearing — forces thinking, then proceed)

## CORE Rule Overrides

The following CORE.md rules have modified behavior in Multi-Agent Mode:

| CORE Rule | Multi-Agent Behavior |
|-----------|---------------------|
| **Rule 1 Struggle Protocol** | Log anomaly → set BLOCKED |
| **Rule 4 FAST PATH** | Reduced checkpoint: intent + files only |
| **Debugging Protocol** | Do NOT debug autonomously beyond quick hypothesis. Log anomaly → BLOCKED. Rationale: autonomous debugging risks cascading errors across agents. |
| **Context degradation** | Auto-checkpoint to blackboard, self-terminate |
| **Rule 11 prevention answer** | Never `accepted` (only the human accepts); where only the human could decide, record `scheduled` with owner `human`. `removed` and task-owned `scheduled` stand |
| **Fixed-twice stop trigger** | The Rule 2 check rides `write-checkpoint`; a held change marks BLOCKED before code, `blocked_reason` starting `fixed-twice:` with one `<concept> <older SHA> <newer SHA>` entry per held concept. The orchestrator routes it to architect designs covering each; resumed, it follows them |

---

## Blackboard Protocol

The blackboard (`state.yaml`) is the coordination mechanism.

**Read Before Act:** Always read current state before any action.

**History is Immutable:** Never delete history entries. Append only.

**Do NOT edit state.yaml directly.** All state transitions MUST go through §BRAND_NAME_TITLE§ CLI commands (`§BRAND_BINARY_NAME§ submit-for-review`, `§BRAND_BINARY_NAME§ claim-task`, etc.). Direct edits bypass invariant checks and can corrupt state irreversibly. If a CLI command fails repeatedly, stop retrying and use the role-specific repeated-failure recovery in Circuit Breaker; never substitute an undeclared mutation or edit state.yaml.

**Do NOT use TodoWrite.** The blackboard already tracks task state and checkpoints.

---

## Output Discipline

No human reads agent text output. Every output token is waste. Speak like caveman — less.

Drop: narration ("Good.", "Now let me...", "Let me check..."), transition announcements
("State: ANALYSIS → READY"), completion summaries, self-review exposition,
filler (all of it — no human to reassure). Thinking block for reasoning, not text.

Record analysis in checkpoint/verdict payload, not text output.

Not: "Good. Worktree verified. Task is IMPLEMENTING_CODE. Now let me read the implementation plan and architecture doc."
Yes: *(nothing — just call the tools)*

Not: "All validation passes: pytest 2 passed, pre-commit hooks all passed, python -m hello prints Hello World. Now commit and invoke clean-code skill."
Yes: *(nothing — tool output already shows this)*

Text output only when: logging anomaly, recording a decision that won't fit in a checkpoint field, or diagnosing an error for the log.

---

## Iteration Protocol

Coders iterate until approved or blocked.

Iteration and review cycle limits are enforced by the blackboard (see `config.max_coder_iterations`, `config.max_review_cycles`).

**On Rejection:**
1. Read rejection feedback from task
2. Update checkpoint with new approach
3. Implement fix
4. Re-submit for review

If the rejection reason declares a reframe or an unresolved contest rather than a
fix to make, do not implement: mark the task BLOCKED with the reviewer's rationale
in `blocked_reason`, and let the Orchestrator rescope.

**Contested Finding:**
The reviewer holds verdict authority. A doer that judges a required fix more harmful
than the finding may plead once, naming the concrete harm — the behavior that breaks,
the invariant violated, the cost incurred. Complexity alone is not a harm. Response
vocabulary and carriers are defined in the `code-review` Transition Reference. The
reviewer does not raise a Decision Request here; the doer carries the declaration to
the human. If no consensus follows, the doer marks the task
BLOCKED with the harm in `blocked_reason` and the disagreement in
`blocked_questions`; the Orchestrator rescopes. Signal strength tracks need, not
insistence — deference is not a reason to weaken it. Applies to every doer role.

**Context Exhaustion Handoff (Coder only):**
At ~90% context (heuristic: many tool calls, re-reading files, difficulty holding state):
1. STOP at next safe point
2. Commit pending changes
3. Run `§BRAND_BINARY_NAME§ handoff` CLI command with summary + next_action
4. Exit with code 42

**Review Exhaustion:**
If 2 different Code Reviewers fail to issue a verdict on the same task (exit without APPROVED/REJECTED):
- Task is marked BLOCKED with `blocked_reason: "review_exhaustion"`
- Orchestrator evaluates: spec unclear? done_when untestable?

---

## Scope Discipline (§BRAND_NAME_TITLE§-Specific)

**Spec is Law:** Implementation must match spec exactly.
- No "improvements" beyond spec
- No "obvious" additions
- No refactoring outside task scope

**done_when is the Contract:**
Each criterion is a test. All must pass. No more, no less.
Example: `app greet` prints "Hello, World!", `app greet --name Alice` prints "Hello, Alice!"

**TDD Enforcement:** Code tasks must include tests. Submission is rejected without test files
unless the checkpoint declares `tdd_not_required` with justification (e.g. non-behavioral
documentation/config/spec-only or cosmetic change).
The reviewer verifies the justification.
If a later checkpoint is written before submission, repeat the waiver if it still applies.

**scope Defines Boundaries:**
IN-scope items specify what may be touched. Touching OUT-scope files is a violation.

---

## Context Recovery

When transitioning to Working Set tier (see CORE.md Context Management), re-read:

**MAM-specific re-read list:**
- Pre-Execution Checkpoint format (this file, "Pre-Execution Checkpoint")
- Current role's constraints from your role section in the agent prompt
- Active task from blackboard (re-read `state.yaml`)

Combined with CORE.md universal items (Tier 0-1 rules, state machine, current task intent).

---

## Circuit Breaker

Automatic halt conditions:

**Loop Detection Self-Abort:**
If an agent observes itself running:
- The same command more than 3 times, OR
- Close variations (same base, different flags/pipes) more than 5 times total

WITHOUT meaningful progress → **STOP IMMEDIATELY**

"Meaningful progress" = new information that changes next action. Piping same output through different tools is NOT progress.

Exempt: `§BRAND_BINARY_NAME§ await-verdict` and `§BRAND_BINARY_NAME§ await-resubmission` POLL retries. Each returns a smaller remaining budget and the loop ends at TIMEOUT — bounded waiting, not repetition.

These recovery paths apply to all doer and reviewer roles. Every repeated-failure record MUST include the exact failing command and observed error.

| Role | Log As | Then |
|------|--------|------|
| Doer (all doer roles) | `retry_loop` | Execute `§BRAND_BINARY_NAME§ mark-blocked` with the failure evidence |
| Reviewer (all reviewer roles) | `reviewer_loop` | Execute `§BRAND_BINARY_NAME§ submit-verdict ... REJECTED` with the failure evidence |
| Orchestrator | `spec_gap` | Pause for human input |

**NON-EXECUTABLE REFERENCE:** `§BRAND_BINARY_NAME§ mark-blocked` is doer-only and MUST NOT be executed by reviewers.

Exit with code 42 after logging.

---

Secret word: MAS
