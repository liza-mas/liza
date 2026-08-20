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
| DRAFT, DRAFT_CODING_PLAN | `assigned_to` must be nil; `worktree` may be set only for claimable continuation from a preserved task branch | spec, code (`validate_task.go`, `claim_task.go`) |
| IMPLEMENTING, CODE_PLANNING | `assigned_to`, `worktree`, `lease_expires`, `base_commit` (unless `integration_fix`) | spec, code |
| READY_FOR_REVIEW, CODING_PLAN_TO_REVIEW | `review_commit` | spec, code |
| REVIEWING, REVIEWING_CODING_PLAN | `reviewing_by`, `review_lease_expires`, `review_commit` | spec, code |
| APPROVED, CODING_PLAN_APPROVED | `review_commit` | spec, code |
| BLOCKED | `blocked_reason`, `blocked_questions` (non-empty); optional complete `repair_request` (`operation`, `target`, `command`, non-empty `evidence`, non-empty `validation`) when a repair request is present | spec, code |
| REJECTED, CODING_PLAN_REJECTED | `rejection_reason` | spec, code |
| SUPERSEDED | `rescope_reason`; `superseded_by` is optional for externally completed work | spec, code |
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
| BLOCKED → READY | Broad transition forbidden; only `unblock-task`, after dependency/worktree/rebase validation, may restore a BLOCKED task to its role-pair initial status |
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

### 3.4 Dependency Direction

Task dependencies cannot point downstream in the configured pipeline topology. A task may depend on work from the same role-pair or an upstream/unrelated role-pair, but must not depend on a task whose `role_pair` is reachable downstream from the dependent task's `role_pair` through configured sub-pipeline transitions or `pipeline-transitions`.

Supersession paths count as dependency paths: if `depends_on: old-task` resolves through `old-task.superseded_by` to a downstream task, the dependency is invalid. `output[].task_depends_on` is validated against every per-subtask outgoing transition target that can consume that output, and explicit writes reject terminal non-MERGED task IDs. Generated child `depends_on` is canonicalized after sibling, concrete `task_depends_on`, and inherited phase-gate dependencies are composed; crash recovery validates the same final child `depends_on` set before patching or appending child tasks. Operational dependency surfaces are canonicalized at mutation and transition boundaries: superseded dependencies are rewritten to legal replacements, cancelled or unreplaced retired dependencies are removed, downstream replacements that are already satisfied are not encoded on children, and illegal pending replacements fail the affected mutation or transition before the dependency rewrite is written. Retired `SUPERSEDED` and `ABANDONED` task output remains historical audit data unless it can still drive crash recovery.

**Protects against:** Earlier pipeline phases waiting on later phases, deadlocked planning tasks, hidden cross-phase blockers.

**Enforced:** code (`pipeline.Resolver` topology helpers, `set_task_output.go`, `proceed.go`, `supersede_task.go`, `validate_deps.go`)

### 3.5 Integration Fix History

Tasks with `integration_fix: true` must have `INTEGRATION_FAILED` event in history.

**Enforced:** code (`validate_task.go`)

### 3.6 Failure Attribution Uniqueness

`failed_by` array cannot have duplicate agent IDs.

**Enforced:** code (`validate_task.go`, `wt_merge.go` via `appendUniqueAgentID`)

### 3.7 Task ID Uniqueness

Every non-empty task ID must identify exactly one task in `state.yaml`.

**Protects against:** Ambiguous task lookup, inconsistent list/get behavior, duplicate claimable work.

**Enforced:** code (`validate.go`)

---

## 4. Agent Identity & Ownership

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| Orchestrator singularity: at most one orchestrator active at any time | Concurrent planning conflicts | spec (`roles.md`), code (`registration.go`) |
| Per-role-key instance limits: max N instances per role (configurable) | Resource contention | code (`resolver.MaxInstances()`) |
| WORKING agent must have `current_task` and valid `lease_expires` | Ghost agents, phantom work | spec, code (`validate_agent.go`) |
| Active doer ownership: tasks in a pipeline executing state must have `assigned_to` pointing to an agent with the exact doer role for the task's `role_pair`, valid owner metadata, and either status `WORKING` with matching `current_task`, status `HANDOFF` with `handoff_pending` and matching `current_task`, or the owned-executing recovery state | Dead doers holding work, cross-role doer claims | spec, code |
| Rejected doer ownership: tasks in a pipeline rejected state with `assigned_to` and an unexpired `lease_expires` can be reclaimed only by that same agent; an expired lease permits reassignment; `assigned_to` without `lease_expires` is corrupted state requiring repair before any reclaim | Ownership collisions, lost rejected work, noisy claim loops | spec, code (`claim_task.go`, `diagnostics.go`) |
| Active review ownership: tasks in a pipeline reviewing state (`ReviewingStatus` or `Reviewing2Status`) must have `reviewing_by` pointing to an agent with the exact reviewer role for the task's `role_pair`, status `REVIEWING`, matching `current_task`, valid review lease, and live-matching or live-unknown reviewer process evidence. Passive reviewer ownership while awaiting resubmission or reclaiming a just-submitted task requires reviewer status `WAITING`, matching `current_task`, an unexpired review lease, and live-matching or live-unknown reviewer process evidence. Missing, unusable, dead, mismatched, or non-observing reviewer process/agent evidence is stale ownership. | Dead reviewers holding review work, cross-role review claims | spec, code |
| No two agents assigned to same executing task | Ownership collisions | spec, code (`validate_task.go`) |
| Agent ID format: `{role}-{number}` (e.g., `coder-1`) | Identity spoofing, cross-role execution | code (registration validation) |
| Registration collision: active-lease agent with live-matching or unknown process identity blocks duplicate registration → immediate exit; dead or mismatched registered PIDs do not count as live capacity | Ghost agents holding claims, PID reuse collisions | spec, code (`AgentCollisionError`, `procscan`) |

### Supervisor-Only Actions (agents cannot perform)

Agent registration/unregistration, heartbeat, post-exit IDLE reset, orchestrator status setup, handoff resume detection. Structural enforcement — agent cannot forget these.

**Enforced:** spec (`supervision-model.md`), code (`internal/agent/`)

---

## 5. Concurrency & Atomicity

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| All state modifications atomic via exclusive file lock | Race conditions, partial writes | code (`blackboard.go` `Modify()`) |
| Three-phase claim: validate ownership/eligibility under lock → worktree outside lock → re-validate ownership/eligibility and commit under lock | TOCTOU races on claim | code (`claim_task.go`) |
| CAS merge: `update-ref` uses compare-and-swap; retries up to 3× if ref moved | Concurrent merge corruption | spec (`worktree-management.md`), code (`wt_merge.go`) |
| Integration ref advancement and every main-index sync/restore run under one project-scoped file lock; blackboard state writes happen after releasing it | Cross-process `index.lock` collisions, lock-order inversion | spec (`worktree-management.md`), code (`wt_merge.go`, `integration_mutation_lock.go`) |
| Sliced-integration analysis materialization is idempotent: a deterministic slice/global analysis key has exactly one matching task and one planned membership across concurrent reconciliation, wake, and restart | Duplicate analysis tasks or generations | code (`integration_reconcile.go`, `workdetection.go`) |
| Completion and integration-ref mutation are linearizable under the completion → mutation → blackboard-read lock order; ADR-0112's mutation → blackboard-read order remains intact, and there is no blackboard state write while the mutation lock is held | Durable completion tied to a stale integration HEAD, deadlock | code (`pipeline_ops.go`, `integration_mutation_lock.go`, `wt_merge.go`, `mode_change.go`) |
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
| Review covers ALL changes (`base_commit` → `review_commit`), not just since last rejection; for submitted/reviewing tasks with a worktree, `review_commit` must match worktree HEAD and `base_commit` must match the effective merge-base of `review_commit` and the configured integration branch; code reviewers run a separate late current-integration drift check before verdict | Partial coverage oversight, stale review ranges | spec (`roles.md`), code (`review_boundary.go`) |
| Reviewer validates against current spec version; material spec change since task creation → reject | Stale spec validation | spec (`roles.md`) |
| Commit SHA verification: reviewer must verify `review_commit` matches worktree HEAD before examining work | Reviewing stale code | spec (`worktree-management.md`) |
| Max iteration limits (default 10 coder, 5 review cycles) → BLOCKED | Infinite coder-reviewer loops | spec (`task-lifecycle.md`), code (`claim_task.go`) |
| Rejection must include structured format: file:line, specific defect, actionable fix; iteration 2+: prior feedback status | Ambiguous feedback, unaddressed rejections | spec (`roles.md`) |
| Code tasks must include tests (TDD: tests first, then implementation); waiver requires explicit `tdd_not_required` | Untested behavior, post-hoc test addition | spec (`roles.md`), code (`submit_review.go`) |
| Local coverage is a bounded navigation record; global integration review independently assesses the aggregate branch and cannot inherit a slice or coding-review verdict as goal-wide approval | Local approval mistaken for aggregate correctness | spec (`task-lifecycle.md`), code (`integration_progress.go`, `integration_reconcile.go`) |

Submitted, reviewing, partially-approved, and approved tasks must not carry `integration_failure`; that diagnostic belongs to active integration recovery, not live review/approval state. Approval and rejection clear stale integration-attempt metadata (`merge_commit`, integration-failure diagnostics) while preserving the review boundary needed by the next step. Rejected tasks also clear stale live review metadata (`review_commit`, approvals) while preserving `output[]` as rework context. Doer claim release clears `output[]` and live review metadata while preserving `failed_by` for hypothesis exhaustion. Fresh-attempt reset paths (task recovery reset, new attempt) clear `output[]`, live review metadata, and `failed_by` so the next claim starts from the initial projection. Retired tasks (`SUPERSEDED`, `ABANDONED`) clear live review/failure metadata while preserving terminal audit context such as `output[]` and `failed_by`.

Integration-fix claims clear active `output[]`, `review_commit`, approvals, `merge_commit`, and structured integration-failure diagnostics from the failed approved attempt before the doer resumes, while preserving `failed_by` for hypothesis exhaustion. Global artifact validation protects only already-MERGED task output refs. Merge artifact validation additionally protects the merging task's output refs, but ignores unrelated non-merged output refs whose artifacts may still exist only in sibling worktrees.

---

## 7. Worktree & Integration

| Invariant | Protects Against | Enforced |
|-----------|------------------|----------|
| Clean sync: before READY_FOR_REVIEW, working tree must be clean (no staged, unstaged, or untracked files) | Uncommitted work in review | spec (`worktree-management.md`), code (`submit_review.go`) |
| Coders cannot commit to or merge to integration branch; only supervisor after reviewer approval | Uncontrolled integration branch | spec (`worktree-management.md`) |
| Merge constructs commits without a working tree (`merge-tree`, `commit-tree`); transient main-index sync/restore is serialized by the integration mutation lock | Race conditions, checkout conflicts, cross-process index collisions | spec, code (`wt_merge.go`, `integration_mutation_lock.go`) |
| If submit/merge conflict detected → INTEGRATION_FAILED (must be reclaimed); unblock-time `--rebase-on` conflicts remain BLOCKED with repair metadata | Silent conflict resolution, wrong recovery path | spec, code |
| Candidate-tree artifact guard validates protected blackboard artifact refs before `update-ref`; invalid candidates do not advance integration | Broken durable artifact refs propagating through normal merge control flow | spec (`worktree-management.md`), code (`wt_merge.go`, `validate.go`) |
| If integration tests fail → rollback via `update-ref` to pre-merge HEAD | Failed integrations propagating | spec, code |
| If post-merge `ValidateArtifactRefs` fails → rollback via `update-ref` to pre-merge HEAD | Backstop for broken blackboard artifact refs after ref advancement | spec, code (`wt_merge.go`, `validate.go`) |
| Worktree path is deterministic: `.worktrees/{taskID}` | Directory traversal, path confusion | code (`claim_task.go`, `wt_create.go`) |
| ABANDONED/SUPERSEDED/MERGED tasks: worktree must be deleted; BLOCKED worktrees may be preserved only for explicit repair/unblock workflows | Stale worktrees, resource leaks, lost repair work | spec (`worktree-management.md`) |
| Rejected-task reclaim preserves the task worktree/branch for same-owner reclaim and post-expiry reassignment; missing or corrupt worktree state is recreated from integration | Lost rejected work, inconsistent reassignment semantics | spec, code (`claim_task.go`) |
| Initial-status task with `worktree` set means claimable continuation from preserved branch; claim validates path, task branch, HEAD, and `base_commit`, and fails rather than deleting invalid preserved work | Lost blocked-task work, stale worktree misclassification | spec, code (`claim_task.go`) |
| Rebase onto integration branch before submission; conflict → abort and restore clean state | Merge conflicts discovered late | code (`submit_review.go`) |
| Integration progression fails closed until planning is settled, required coverage is complete, every slice is resolved, and coding plus repair work is terminal; blocked/abandoned repairs, slice exhaustion, unavailable topology, and global generation exhaustion cannot satisfy completion | Premature global analysis or successful completion with missing evidence | code (`integration_progress.go`, `pipeline_ops.go`) |
| Clean integration completion names an immutable reviewed source commit and is effective only while it equals live integration HEAD; a later ref mutation appends a receipt and mutation-side invalidation makes any goal-complete stop tied to the superseded commit non-successful | Stale clean evidence surviving a branch mutation | code (`submit_verdict.go`, `wt_merge.go`, `mode_change.go`) |

The finalization linearization point establishes one relationship among the
clean reviewed commit, live integration HEAD, and completion: all three agree or
completion fails. A mutation ordered before finalization prevents clean closure
for the stale source. A mutation ordered after finalization preserves the
immutable prior evidence but appends its before/after receipt, invalidates the
goal-complete stop on the mutation side, and requires another bounded global
generation. This ordering extends ADR-0112 without reversing it: the completion
lock encloses the mutation lock and any blackboard read, the mutation lock is
released before receipt or completion state is written, and no blackboard state
write occurs under the mutation lock.

The candidate-tree artifact guard protects goal `spec_ref`; task `spec_ref`,
`epic_ref`, `plan_ref`, and `arch_ref`; and merge-durable output refs. Output
refs are merge-durable for the task being merged and for already-MERGED tasks;
unrelated in-flight task output refs are not protected by this merge because
their artifacts may exist only in sibling worktrees. Protected refs are scalar
repo-relative paths with optional `#fragment` anchors and must resolve in the
candidate tree to regular Git file modes `100644` or `100755`. Missing paths,
directories, submodules/gitlinks, symlinks, and other non-regular object modes
are rejected. Invalid artifact refs fail closed, including semicolon-joined
refs, empty paths after stripping `#fragment`, paths that traverse outside the
repository, and absolute refs that cannot be safely normalized to repo-relative
paths. Diagnostics deterministically name the invalid path plus owner
provenance: field name, task ID when applicable, and output index when
applicable.

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
| Hard checkpoints are not auto-cleared; agents remain paused indefinitely until human responds. Transition checkpoints gate downstream transition creation, while doer/reviewer work already available in the sprint may continue. | Autonomous downstream continuation during gated transition; runaway manual pauses | spec |
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
| Duplicate or stale integration analysis | Deterministic analysis identities, idempotent reconciliation, immutable coverage and generation evidence (§5, §7) |
| Premature or stale integration completion | Fail-closed coverage/repair barriers, independent aggregate review, clean-current-HEAD linearization, mutation-side invalidation (§6, §7) |
| Silent failures | Anomaly logging, blocking protocol (§14) |
| Hallucination & fabrication | Tier 0.2, source validation, phantom fix prevention (§1, §15) |
| Secret exposure | Credential file prohibition, redaction protocol (§9) |
| Autonomous runaway | Checkpoints, circuit breaker, mode transitions (§12) |
