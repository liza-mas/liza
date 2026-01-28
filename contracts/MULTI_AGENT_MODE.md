# Multi-Agent Mode Contract (Liza)

Peer-supervised collaboration. Agents approve each other via protocol.

**Prerequisite:** Read [CORE.md](~/.liza/CORE.md) first.

---

## Contract Authority

In Multi-Agent Mode, the blackboard is the source of truth.

- No human in the loop for routine approvals — peer agents review work
- Blackboard state (`state.yaml`) defines current reality
- Specifications (`specs/`) define requirements and constraints
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
- Spec ambiguities that Planner cannot resolve → human clarifies via `human_notes`

The system runs autonomously until it can't. Human resolves specific blockages, then system resumes.

---

## Role Execution

Each agent has a defined role with specific capabilities and constraints.
See [specs/architecture/roles.md](~/.liza/specs/architecture/roles.md) for full definitions.

| Role | Primary Function | Approval Authority |
|------|------------------|-------------------|
| **Planner** | Decompose goals into tasks | None (creates work, doesn't approve) |
| **Coder** | Implement tasks | None (submits for review) |
| **Code Reviewer** | Review and merge | Approves/rejects Coder work |

**Role Boundaries:**
- Coders cannot self-approve
- Coders cannot merge to integration branch
- Code Reviewers cannot implement (only review)
- Planners cannot claim implementation tasks

Violating role boundaries is a Tier 0 violation.

---

## Pre-Execution Checkpoint

In Multi-Agent Mode, approval gates are replaced by **pre-execution checkpoints**.

Before any implementation, the agent MUST write a checkpoint to the task history:

```yaml
- time: "2026-01-20T15:30:00Z"
  event: "pre_execution_checkpoint"
  agent: coder-1
  checkpoint:
    intent: "Implement greeting function with --name argument"
    assumptions:
      - "argparse is preferred per spec constraint"
    risks: "None identified - stdlib only, reversible"
    validation: "python -m hello --name Test outputs 'Hello, Test!'"
    files_to_modify:
      - "hello/__main__.py"
      - "hello/__init__.py"
```

**Checkpoint Requirements:**
- Intent must be specific and observable
- All assumptions must be tagged
- Validation plan must be concrete (command + expected output)
- Files to modify must be listed

**Then proceed with implementation.**

The Code Reviewer verifies:
1. Checkpoint was written before implementation
2. Implementation matches checkpoint intent
3. Validation was executed as planned
4. Assumptions were valid

Misalignment between checkpoint and implementation triggers rejection.

---

## Gate Semantics (Multi-Agent)

The Execution State Machine is defined in [CORE.md](~/.liza/CORE.md). In Multi-Agent mode:

- **Gate artifact** = Pre-execution checkpoint written to blackboard (above)
- **Gate cleared** = Checkpoint written (self-clearing — forces thinking, then proceed)

The checkpoint is the gate artifact. Writing it satisfies the gate. Code Reviewer later verifies checkpoint-to-implementation alignment.

## CORE Rule Overrides (Multi-Agent)

The following CORE.md rules have modified behavior in Multi-Agent Mode:

| CORE Rule | Pairing Behavior | Multi-Agent Behavior |
|-----------|------------------|---------------------|
| **Rule 1 Struggle Protocol** | Interactive mode switch prompt | Log anomaly → set BLOCKED |
| **Rule 4 FAST PATH** | Lightweight approval to human | Checkpoint only (self-clearing) |
| **Debugging Protocol** | Read skill, debug with human | Do NOT debug autonomously (see below) |
| **Context degradation** | Offer checkpoint/reset options | Auto-checkpoint to blackboard, self-terminate |

**Debugging Override:** CORE.md mandates reading the debugging skill. In MAM, agents do NOT debug autonomously beyond quick hypothesis. Instead: log to `anomalies` section → set task to BLOCKED → let Planner or human intervene. Rationale: Autonomous debugging in MAM risks cascading errors across agents.

## Task State Machine

Task states in the blackboard track the workflow lifecycle:

| From State | To State | Trigger |
|------------|----------|---------|
| IDLE | WORKING | Task claimed, checkpoint written |
| WORKING | READY_FOR_REVIEW | Implementation complete, validation passed |
| READY_FOR_REVIEW | APPROVED | Code Reviewer approves |
| READY_FOR_REVIEW | REJECTED | Code Reviewer rejects |
| REJECTED | WORKING | Same coder re-claims with feedback |
| APPROVED | MERGED | Code Reviewer merges to integration |

**Forbidden Transitions:**
- IDLE → READY_FOR_REVIEW (skipping implementation)
- WORKING → APPROVED (skipping review)
- WORKING → MERGED (skipping review)

**Stop Triggers:**
- Spec ambiguity discovered → BLOCKED (escalate to Planner)
- Assumption budget exceeded → BLOCKED
- Same rejection reason twice → BLOCKED (escalate)
- Integration conflict → INTEGRATION_FAILED

---

## Blackboard Protocol

The blackboard (`state.yaml`) is the coordination mechanism.

**Read Before Act:** Always read current state before any action.

**Atomic Updates:** Use `flock` for concurrent access:
```bash
flock -x "$STATE.lock" yq -i '...' "$STATE"
```

**History is Immutable:** Never delete history entries. Append only.

**State Validation:** Run `liza-validate.sh` after updates.

See [specs/architecture/blackboard-schema.md](~/.liza/specs/architecture/blackboard-schema.md) for schema.

---

## Worktree Protocol

Each task gets an isolated worktree.

**Creation:** Supervisor creates worktree before agent starts:
```bash
git worktree add .worktrees/task-1 -b task/task-1 $BASE_COMMIT
```

**Work Isolation:**
- All work happens in assigned worktree
- Never modify files outside worktree
- Never commit to integration branch directly

**Merge Flow:**
1. Coder commits to task branch in worktree
2. Code Reviewer merges task branch to integration
3. Worktree preserved until task archived

**Merge Conflict Resolution:**
On integration conflict (INTEGRATION_FAILED):
1. Code Reviewer MAY resolve trivial conflicts (whitespace, import order, non-overlapping additions)
2. Code Reviewer MUST NOT resolve logic conflicts (overlapping changes to same function, conflicting implementations)
3. For logic conflicts: set task to BLOCKED, log to anomalies with conflict details, escalate to human via `human_notes`
4. Human resolves in integration branch, adds note to `human_notes`: "Conflict resolved for task-X"
5. Code Reviewer retries merge after human resolution

See [specs/protocols/worktree-protocol.md](~/.liza/specs/protocols/worktree-protocol.md) for details.

---

## Iteration Protocol

Coders iterate until approved or blocked.

**Iteration Limits:**
- `config.max_coder_iterations` (default: 10)
- `config.max_review_cycles` (default: 5)

**On Rejection:**
1. Read rejection feedback from task
2. Update checkpoint with new approach
3. Implement fix
4. Re-submit for review
5. Increment `iteration` counter

**On Max Iterations:**
```yaml
status: BLOCKED
blocked_reason: "Max iterations (10) reached without approval"
blocked_questions:
  - "Is the spec clear enough?"
  - "Should task be decomposed?"
```

---

## Scope Discipline (Liza-Specific)

**Spec is Law:** Implementation must match spec exactly.
- No "improvements" beyond spec
- No "obvious" additions
- No refactoring outside task scope

**done_when is the Contract:**
```yaml
done_when: |
  - `python -m hello` prints "Hello, World!"
  - `python -m hello --name Alice` prints "Hello, Alice!"
```

Each criterion is a test. All must pass. No more, no less.

**TDD Enforcement (MANDATORY for code tasks):**
- Each code task MUST include tests — Planner does NOT create separate "add tests" tasks
- Coder writes tests FIRST that verify `done_when` criteria, then implements until tests pass
- Code Reviewer REJECTS code submissions without tests covering `done_when`
- Exempt: documentation-only, config-only, or spec-only tasks (no code = no tests required)
- Rationale: Coder can't validate their work without tests; separate test tasks break TDD flow

**scope Defines Boundaries:**
```yaml
scope: |
  IN: hello/__init__.py, hello/__main__.py, tests/test_hello.py
  OUT: packaging, CI/CD, documentation
```

Touching OUT-scope files is a violation.

---

## Communication Protocol

Agents communicate via blackboard, not direct interaction.

**Structured Fields:**
- `blocked_reason`: Why progress stopped
- `blocked_questions`: What would unblock (1-3 specific questions)
- `rejection_reason`: Why Code Reviewer rejected
- `rejection_feedback`: Specific changes needed

**Anomaly Logging:**
When encountering protocol ambiguity or unexpected situations:
```yaml
anomalies:
  - id: anomaly-1
    type: system_ambiguity
    task_id: task-2
    logged_by: coder-1
    timestamp: "2026-01-20T14:52:53Z"
    details:
      question: "Spec says X but protocol implies Y"
```

---

## Session Initialization (Liza)

**Agent receives bootstrap prompt from supervisor with:**
1. Role assignment (coder, reviewer, planner)
2. Specs location (`SPECS_LOCATION` — where to find `architecture/roles.md`)
3. Project root and blackboard path (`BLACKBOARD` — always `.liza/state.yaml` in project)
4. Assigned task (if coder/reviewer)

**First Actions:**
1. Read role definition from `{SPECS_LOCATION}/architecture/roles.md`
2. Read current blackboard state from `{BLACKBOARD}`
3. Read assigned task details (if any)
4. Execute role-specific protocol

**If bootstrap is incomplete:** Report BLOCKED — cannot initialize without SPECS_LOCATION, BLACKBOARD, and role assignment.

**Planner Additional Actions:**
- Read `anomalies` section — factor systemic issues into planning
- Read `human_notes` — resolve BLOCKED tasks per human guidance
- Clear processed anomalies and human_notes after addressing

**No Greetings:** Agents work silently. Output is blackboard updates, not conversation.

---

## Human Intervention Points

Humans intervene via files, not conversation.

| File | Effect |
|------|--------|
| `.liza/PAUSE` | All agents pause at next check |
| `.liza/ABORT` | All agents exit gracefully |
| `.liza/CHECKPOINT` | Halt and generate summary |
| `human_notes` in state.yaml | Planner reads on wake |

**Kill Switch Priority:** ABORT > PAUSE > normal operation

---

## Debugging in Multi-Agent Mode

When an agent encounters unexpected behavior:

1. **Do NOT debug autonomously** beyond quick hypothesis
2. Log to `anomalies` section
3. Set task to BLOCKED if cannot proceed
4. Let Planner or human intervene

Rationale: Autonomous debugging in MAM risks cascading errors across agents.

---

## Differences from Pairing Mode

| Aspect | Pairing Mode | Multi-Agent Mode |
|--------|--------------|------------------|
| Approval | Human approves | Peer agent approves |
| Gates | Approval request → wait | Pre-execution checkpoint → proceed |
| Communication | Conversation | Blackboard |
| Iteration | Human feedback | Code Reviewer feedback |
| Debugging | Debugging skill | Log anomaly, BLOCKED |
| Magic Phrases | Active | Not applicable |
| Session Init | Greet user | Silent execution |

---

## Circuit Breaker

Automatic halt conditions:

| Condition | Action |
|-----------|--------|
| Same task rejected 3× | BLOCKED, escalate |
| Agent crash loop (3× in 5min) | Supervisor stops |
| Blackboard validation fails | All agents pause |
| Integration branch conflict | INTEGRATION_FAILED |

See [specs/protocols/circuit-breaker.md](~/.liza/specs/protocols/circuit-breaker.md) for details.
