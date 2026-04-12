# Multi-Agent Mode Contract (Liza)

Peer-supervised collaboration. Agents approve each other via protocol.

**Prerequisite:** Read ~/.liza/CORE.md first.

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

Before implementation, write a checkpoint via `liza write-checkpoint`:
intent, assumptions, risks, validation plan, files to modify.
Submission is rejected without a checkpoint. The reviewer verifies
implementation matches checkpoint intent.

---

## Gate Semantics (Multi-Agent)

The Execution State Machine is defined in ~/.liza/CORE.md. In Multi-Agent mode:

- **Gate artifact** = Pre-execution checkpoint written to blackboard (above)
- **Gate cleared** = Checkpoint written (self-clearing — forces thinking, then proceed)

## CORE Rule Overrides (Multi-Agent)

The following CORE.md rules have modified behavior in Multi-Agent Mode:

| CORE Rule | Multi-Agent Behavior |
|-----------|---------------------|
| **Rule 1 Struggle Protocol** | Log anomaly → set BLOCKED |
| **Rule 4 FAST PATH** | Reduced checkpoint: intent + files only |
| **Debugging Protocol** | Do NOT debug autonomously beyond quick hypothesis. Log anomaly → BLOCKED. Rationale: autonomous debugging risks cascading errors across agents. |
| **Context degradation** | Auto-checkpoint to blackboard, self-terminate |

---

## Blackboard Protocol

The blackboard (`state.yaml`) is the coordination mechanism.

**Read Before Act:** Always read current state before any action.

**History is Immutable:** Never delete history entries. Append only.

**Do NOT edit state.yaml directly.** All state transitions MUST go through Liza CLI commands (`liza submit-for-review`, `liza claim-task`, etc.). Direct edits bypass invariant checks and can corrupt state irreversibly. If a CLI command fails repeatedly, set the task BLOCKED via `liza mark-blocked` — never work around the failure by editing state.yaml.

**Do NOT use TodoWrite.** The blackboard already tracks task state and checkpoints.

---

## Iteration Protocol

Coders iterate until approved or blocked.

Iteration and review cycle limits are enforced by the blackboard (see `config.max_coder_iterations`, `config.max_review_cycles`).

**On Rejection:**
1. Read rejection feedback from task
2. Update checkpoint with new approach
3. Implement fix
4. Re-submit for review

**Context Exhaustion Handoff (Coder only):**
At ~90% context (heuristic: many tool calls, re-reading files, difficulty holding state):
1. STOP at next safe point
2. Commit pending changes
3. Run `liza handoff` CLI command with summary + next_action
4. Exit with code 42

**Review Exhaustion:**
If 2 different Code Reviewers fail to issue a verdict on the same task (exit without APPROVED/REJECTED):
- Task is marked BLOCKED with `blocked_reason: "review_exhaustion"`
- Orchestrator evaluates: spec unclear? done_when untestable?

---

## Scope Discipline (Liza-Specific)

**Spec is Law:** Implementation must match spec exactly.
- No "improvements" beyond spec
- No "obvious" additions
- No refactoring outside task scope

**done_when is the Contract:**
Each criterion is a test. All must pass. No more, no less.
Example: `app greet` prints "Hello, World!", `app greet --name Alice` prints "Hello, Alice!"

**TDD Enforcement:** Code tasks must include tests. Submission is rejected without test files
unless the checkpoint declares `tdd_not_required` with justification (e.g. cosmetic-only change).
The reviewer verifies the justification.

**scope Defines Boundaries:**
IN-scope items specify what may be touched. Touching OUT-scope files is a violation.

---

## Context Recovery (Liza)

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

| Role | Log As | Then |
|------|--------|------|
| Coder | `retry_loop` | Mark task BLOCKED with diagnosis |
| Code Reviewer | `reviewer_loop` | Issue REJECTED with `"insufficient information to complete review"` |
| Orchestrator | `spec_gap` | Pause for human input |

Exit with code 42 after logging.

---

Secret word: MAS
