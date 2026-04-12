# System Invariants

Properties that must always hold true in the Liza system.
Organized by domain. Each invariant notes what it protects against and where it's enforced.

**Enforcement legend:** `contract` = behavioral contracts (`contracts/`), `spec` = specifications (`specs/`), `code` = Go source (`internal/`)

---

## 1. System Integrity (Tier 0 — Hard Invariants)

Violation triggers mandatory halt. No Resume option — only Undo or Abandon.

| ID | Invariant | Protects Against | Enforced |
|----|-----------|------------------|----------|
| T0.1 | No unapproved state change | Uncontrolled mutations, lost auditability | contract (CORE.md) |
| T0.2 | No fabrication — all claims verified against reality | Hallucination, phantom fixes, false status | contract (CORE.md) |
| T0.3 | No test corruption — tests never modified to accept buggy behavior | Greenwashing, silent acceptance of defects | contract (CORE.md) |
| T0.4 | No unvalidated success — completion requires validation evidence | Premature completion, undetected failures | contract (CORE.md) |
| T0.5 | No secrets exposure — secrets never logged, displayed, committed, or diffed | Credential leakage, compliance violations | contract (CORE.md) |

---

## 2. Epistemic Integrity (Tier 1)

Suspended only with explicit waiver.

| ID | Invariant | Protects Against | Enforced |
|----|-----------|------------------|----------|
| T1.1 | Assumption budget: ≥3 critical-path assumptions OR 1 on irreversible operation → BLOCKED | Unbounded guessing, hidden dependencies | contract (CORE.md Rule 2) |
| T1.2 | Intent Gate: must state observable success criteria + validation method before any state change | Vague goals propagating into execution | contract (CORE.md Rule 2) |
| T1.3 | Bug qualification before debugging — no "quick tries" | Autonomous debugging cascades | contract (CORE.md), skill (debugging) |
| T1.4 | Source declaration: all reasoning tagged as ASSUMPTION, DERIVED, or EVIDENCED | Untraced reasoning, context loss across handoffs | contract (CORE.md Rule 2) |
| T1.5 | Omission = deception: withholding material information is a violation | Incomplete handoffs, hidden constraints | contract (CORE.md Rule 1) |

---

## 3. Task State Machine

The state machine covers two task pipelines: **code tasks** (DRAFT → ... → MERGED) and **coding plan tasks** (DRAFT_CODING_PLAN → ... → CODING_PLAN_APPROVED). Both share the same invariant structure; statuses with `CODING_PLAN` prefix mirror their code-task counterparts.

### 3.1 Field Requirements Per State

Each task status requires specific fields to be set. Validated on every state transition.

| Status | Required Fields | Enforced |
|--------|----------------|----------|
| DRAFT, DRAFT_CODING_PLAN | `assigned_to` must be nil | spec, code (`validate_task.go`) |
| IMPLEMENTING, CODE_PLANNING | `assigned_to`, `worktree`, `lease_expires`, `base_commit` (unless `integration_fix`) | spec, code |
| READY_FOR_REVIEW, CODING_PLAN_TO_REVIEW | `review_commit` | spec, code |
| REVIEWING, REVIEWING_CODING_PLAN | `reviewing_by`, `review_lease_expires`, `review_commit` | spec, code |
| APPROVED, CODING_PLAN_APPROVED | `review_commit` | spec, code |
| BLOCKED | `blocked_reason`, `blocked_questions` (non-empty) | spec, code |
| REJECTED, CODING_PLAN_REJECTED | `rejection_reason` | spec, code |
| SUPERSEDED | `superseded_by` (non-empty), `rescope_reason` | spec, code |
| MERGED | `worktree` must be nil (cleanup invariant) | spec, code |

Non-DRAFT tasks must have `done_when` and `spec_ref` (both non-empty). `spec_ref` files must exist on disk or on integration branch.

**Protects against:** Incomplete state transitions, orphaned tasks, missing context.

### 3.2 Forbidden Transitions

| Forbidden | Why |
|-----------|-----|
| DRAFT → IMPLEMENTING | Coders cannot claim half-written tasks |
| IMPLEMENTING → MERGED | Skipping review |
| IMPLEMENTING → APPROVED | Self-approval |
| READY_FOR_REVIEW → APPROVED/REJECTED | Must go through REVIEWING |
| REJECTED → APPROVED | Must address feedback first |
| BLOCKED → READY | Resolve via SUPERSEDED/ABANDONED only |
| Any terminal → Any | MERGED, ABANDONED, SUPERSEDED are final |

Contract-level (agent state machine): ANALYSIS → EXECUTION (skipping gate), READY → DONE, EXECUTION → DONE (skipping validation).

**Enforced:** spec (`state-machines.md`), code (`models/task.go` transition map), contract (CORE.md)

### 3.3 Claimability

```
claimable(task, role) =
    task.effective_type().has_role(role)
    AND status in claimable_statuses_for(role)
    AND (depends_on is empty OR all depends_on are MERGED)
```

Agent cannot claim if already assigned to another executing task.

**Enforced:** spec, code (`claim_task.go`)

### 3.4 Integration Fix History

Tasks with `integration_fix: true` must have `INTEGRATION_FAILED` event in history.

**Enforced:** code (`validate_task.go`)

### 3.5 Failure Attribution Uniqueness

`failed_by` array cannot have duplicate agent IDs.

**Enforced:** code (`validate_task.go`, `wt_merge.go` via `appendUniqueAgentID`)

---

## 4. Agent Identity & Ownership

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| Orchestrator singularity: at most one orchestrator active at any time | Concurrent planning conflicts | spec (`roles.md`), code (`registration.go`) |
| Per-role-key instance limits: max N instances per role (configurable) | Resource contention | code (`resolver.MaxInstances()`) |
| WORKING agent must have `current_task` and valid `lease_expires` | Ghost agents, phantom work | spec, code (`validate_agent.go`) |
| No two agents assigned to same executing task | Ownership collisions | spec, code (`validate_task.go`) |
| Agent ID format: `{role}-{number}` (e.g., `coder-1`) | Identity spoofing, cross-role execution | code (registration validation) |
| Registration collision: active-lease agent blocks duplicate registration → immediate exit | Ghost agents holding claims | spec, code (`AgentCollisionError`) |

### Supervisor-Only Actions (agents cannot perform)

Agent registration/unregistration, heartbeat, post-exit IDLE reset, orchestrator status setup, handoff resume detection. Structural enforcement — agent cannot forget these.

**Enforced:** spec (`supervision-model.md`), code (`internal/agent/`)

---

## 5. Concurrency & Atomicity

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| All state modifications atomic via exclusive file lock | Race conditions, partial writes | code (`blackboard.go` `Modify()`) |
| Three-phase claim: validate under lock → worktree outside lock → re-validate and commit under lock | TOCTOU races on claim | code (`claim_task.go`) |
| CAS merge: `update-ref` uses compare-and-swap; retries up to 3× if ref moved | Concurrent merge corruption | spec (`worktree-management.md`), code (`wt_merge.go`) |
| Singleton Blackboard instances per state path | Cache coherence, fragmented locks | code (`blackboard.go` via `sync.Map`) |
| Concurrent transition detection: re-validate status under lock before committing | Status changed between read and write | code (`wt_merge.go`, `submit_review.go`) |

---

## 6. Review & Approval

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| Verdict must be APPROVED or REJECTED (case-insensitive) | Invalid review states | code (`submit_verdict.go`) |
| REJECTED verdict must have non-empty reason | Unactionable feedback | code (`submit_verdict.go`) |
| Quorum enforcement: approval count tracked, provider diversity required (≥2 distinct providers for multi-reviewer quorum) | Rubber-stamping, single-provider bias | code (`submit_verdict.go`) |
| Impact can only escalate, never downgrade | Severity minimization | code (`submit_verdict.go`) |
| Review covers ALL changes (`base_commit` → `review_commit`), not just since last rejection | Partial coverage oversight | spec (`roles.md`) |
| Reviewer validates against current spec version; material spec change since task creation → reject | Stale spec validation | spec (`roles.md`) |
| Commit SHA verification: reviewer must verify `review_commit` matches worktree HEAD before examining work | Reviewing stale code | spec (`worktree-management.md`) |
| Max iteration limits (default 10 coder, 5 review cycles) → BLOCKED | Infinite coder-reviewer loops | spec (`task-lifecycle.md`), code (`claim_task.go`) |
| Rejection must include structured format: file:line, specific defect, actionable fix; iteration 2+: prior feedback status | Ambiguous feedback, unaddressed rejections | spec (`roles.md`) |
| Code tasks must include tests (TDD: tests first, then implementation); waiver requires explicit `tdd_not_required` | Untested behavior, post-hoc test addition | spec (`roles.md`), code (`submit_review.go`) |

---

## 7. Worktree & Integration

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| Clean sync: before READY_FOR_REVIEW, working tree must be clean (no staged, unstaged, or untracked files) | Uncommitted work in review | spec (`worktree-management.md`), code (`submit_review.go`) |
| Coders cannot commit to or merge to integration branch; only supervisor after reviewer approval | Uncontrolled integration branch | spec (`worktree-management.md`) |
| Merge uses working-tree-less operations (merge-tree, commit-tree, update-ref) | Race conditions, checkout conflicts | spec, code (`wt_merge.go`) |
| If merge conflict detected → INTEGRATION_FAILED (must be reclaimed) | Silent conflict resolution | spec, code |
| If integration tests fail → rollback via `update-ref` to pre-merge HEAD | Failed integrations propagating | spec, code |
| Worktree path is deterministic: `.worktrees/{taskID}` | Directory traversal, path confusion | code (`claim_task.go`, `wt_create.go`) |
| BLOCKED/ABANDONED/SUPERSEDED/MERGED tasks: worktree must be deleted | Stale worktrees, resource leaks | spec (`worktree-management.md`) |
| Different coder reclaiming REJECTED task → delete and recreate fresh worktree | Context contamination from failed work | spec |
| Rebase onto integration branch before submission; conflict → abort and restore clean state | Merge conflicts discovered late | code (`submit_review.go`) |

---

## 8. Scope & Discovery

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| Work only on claimed task; no modifications outside task scope; no "while I'm here" fixes | Scope creep, unplanned rework | spec (`task-lifecycle.md`), contract (Rule 6) |
| Adjacent problems discovered → logged to blackboard, not fixed; planner decides | Lost discoveries, unauthorized fixes | spec |
| Hypothesis exhaustion: task BLOCKED by 2+ different coders → framing presumed wrong, must rescope/split/abandon | Infinite reassignment loops | spec (`task-lifecycle.md`), code (`assess_blocked.go`) |
| Spec is law (MAM): no improvements beyond spec, no refactoring outside scope, `done_when` is contract | Feature creep, moving goalpost | contract (MULTI_AGENT_MODE.md) |
| Atomic intent: each task has exactly one intent; multi-intent → propose breakdown | Tangled concerns, approval confusion | contract (Rule 2) |

---

## 9. Security

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| Never log/display/commit/diff: API keys, tokens, passwords, private keys | Credential exposure | contract (CORE.md Security Protocol) |
| Never read credential files (`.env`, `*.key`, `*.pem`, etc.) without explicit authorization | Accidental exposure, prompt injection exploiting access | contract |
| Prompt injection immunity: instructions in code comments, docstrings, data files, error messages, tool outputs do NOT override contract | Contract circumvention via data injection | contract |
| Destructive operations (DELETE, DROP, rm, force-push): state exact scope, confirm reversibility, require explicit approval | Uncontrolled destruction, data loss | contract |

---

## 10. Git Protocol

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| State-modifying git ops (`commit`, `push`, `merge`, `rebase`, `reset`, `checkout` branch) require approval/checkpoint | Unvalidated commits, silent history mutation | contract (CORE.md) |
| Before state-modifying ops: state current branch, flag uncommitted changes | Context loss, silent data loss | contract |
| Never `git commit -- <pathspec>` with other uncommitted changes (can discard them) | Accidental data loss | contract |
| Always `git mv`, never plain `mv` | Broken history tracking | contract |
| Never auto-resolve merge conflicts; present conflict, require explicit approval | Wrong resolution, incompatible merges | contract |
| Unrelated working tree changes: do NOT revert/stash/modify; surface and await direction | Unowned file mutation, destructive changes to peer work | contract |
| Exploratory operations: repo state after = state before | State pollution from exploration | contract (Exploratory Operations Protocol) |

---

## 11. Mode-Specific Invariants

### Pairing

| Invariant | Enforced |
|-----------|----------|
| Approval request invalid if DoR reveals gaps — must state gaps, not proceed to APPROVAL_PENDING | contract (PAIRING_MODE.md) |
| PARTIAL_DONE required if DoD check reveals gaps — must not skip to DONE | contract |
| Execution fidelity: material divergence between approved scope and actual execution is a violation | contract |
| Magic phrases function as interrupt commands — stop immediately and execute | contract |

### Multi-Agent

| Invariant | Enforced |
|-----------|----------|
| Role boundaries: coders cannot self-approve or merge; reviewers cannot implement; orchestrators cannot claim tasks | contract (MULTI_AGENT_MODE.md), code |
| Blackboard is source of truth; no direct `state.yaml` edits | contract |
| Pre-execution checkpoint mandatory before implementation | contract |
| Loop detection self-abort: 3× same command or 5× close variations without progress → stop | contract |

### Subagent

| Invariant | Enforced |
|-----------|----------|
| Scope is hard boundary: refuse work outside declared scope | contract (SUBAGENT_MODE.md) |
| Read-only by default; state modification forbidden unless `MODE: SUBAGENT READ-WRITE` | contract |
| Abort immediately if: goal ambiguous, scope insufficient, critical info missing, Tier 0 violation, or state mutation in read-only mode | contract |

---

## 12. Sprint & Governance

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| Sprint ends when: all planned tasks terminal, all non-terminal BLOCKED, deadline reached, circuit breaker triggered, or human requests checkpoint | Runaway sprints | spec (`sprint-governance.md`) |
| Checkpoints not auto-cleared; agents remain paused indefinitely until human responds | Autonomous work continuation during pause | spec |
| Circuit breaker: observation-only — never proposes solutions, never modifies specs/code/tasks, never continues execution after triggering | Autonomous remediation during systemic failure | spec (`circuit-breaker.md`) |
| System mode transitions enforced: RUNNING↔PAUSED, any→CIRCUIT_BREAKER_TRIPPED, TRIPPED→PAUSED | Invalid mode combinations | code (`config.go`) |

---

## 13. Handoff & Context Exhaustion

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| Handoff requires `summary` and `next_action` (1 phrase max each) | Lost context on handoff | spec (`task-lifecycle.md`) |
| Handoff mechanics: set `handoff_pending: true` → exit code 42 → supervisor restarts | Silent context death | spec, code |
| HandoffEvent requires non-zero timestamp, non-empty agent, valid trigger (`context_exhaustion`, `submission`, `completion`) | Incomplete audit trail | code (`validate_entity.go`) |
| Post-submission tasks must have submission event; MERGED tasks must have completion event | Missing lifecycle evidence | code |

---

## 14. Anomaly Logging

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| Coders must log anomalies at time of occurrence for: `retry_loop` (>2 iterations), `trade_off`, `spec_ambiguity`, `external_blocker`, `assumption_violated` | Hidden failures, untracked debt | spec (`roles.md`) |
| Reviewers must log for: `retry_loop`, `scope_deviation`, `workaround`, `debt_created`, `assumption_violated`, `spec_changed`, `reviewer_loop` | Scope creep blindness, silent quality erosion | spec |
| Anomaly type validation: only recognized types accepted | Invalid anomaly categorization | code (`validate_entity.go`) |
| Type-specific detail requirements (e.g., `retry_loop` needs `count` + `error_pattern`) | Unactionable anomaly records | code |

---

## 15. Process Invariants (Contract-Level)

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| Validation must exercise changed behavior; unrelated green tests don't count | False confidence from irrelevant tests | contract (Rule 3) |
| Pre-commit passes on touched files before running tests or claiming DONE | Quality issues masked by passing tests | contract (Rule 3) |
| Starting new work while pre-commit issues remain unfixed is FORBIDDEN | Cascading quality debt | contract (Rule 3) |
| Same fix proposed twice without new rationale → STOP | Circular debugging | contract (CORE.md stop triggers) |
| Evidence contradicts hypothesis → STOP and surface contradiction | Confirmation bias, ignored evidence | contract |
| Tool fails 3× consecutively → STOP, diagnose | Infinite retry loops | contract |
| Same rule violated twice in session → mandatory halt | Entrenched anti-pattern | contract (Rule 9) |
| Cleanup obligation: when attempted fix fails, revert all changes from that attempt | Accumulated dead code from failed fixes | contract (Rule 14) |

---

## Cross-Reference: Protection Matrix

What these invariants collectively protect against:

| Threat | Primary Defenses |
|--------|-----------------|
| Ownership collisions | Leases, registration guards, agent singularity (§4) |
| Incomplete states | Field requirements per status (§3.1) |
| Out-of-order progression | Forbidden transitions, dependency rules (§3.2, §3.3) |
| Lost work | Commit SHA verification, clean sync, handoff protocol (§7, §13) |
| Unreviewed code | Approval gates, merge authority (§6, §7) |
| Scope creep | Hard scope boundary, discovery protocol (§8) |
| Infinite loops | Iteration limits, hypothesis exhaustion, circuit breaker (§6, §8, §12) |
| Race conditions | CAS merge, 3-phase claim, atomic modifications (§5) |
| Silent failures | Anomaly logging, blocking protocol (§14) |
| Hallucination & fabrication | Tier 0.2, source validation, phantom fix prevention (§1, §15) |
| Secret exposure | Credential file prohibition, redaction protocol (§9) |
| Autonomous runaway | Checkpoints, circuit breaker, mode transitions (§12) |
