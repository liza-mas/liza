# Validation Checklist

Before declaring v1 complete:

## Spec Readiness (Pre-Implementation Gate)

Before implementation begins, spec review must reach verdict:

| Verdict | Meaning | Criteria |
|---------|---------|----------|
| **APPROVED** | Ready for implementation | No Critical, no High |
| **CONDITIONAL** | Proceed with tracked items | No Critical; High items have explicit acceptance rationale |
| **BLOCKED** | Cannot proceed | Critical exists or High affects foundational assumptions |

### Stopping Criteria

Review concludes when ANY of:
1. Verdict is APPROVED
2. Two consecutive passes find no new Critical/High
3. Human declares "good enough" with explicit acceptance

### Issue Handling

| Severity | Handling |
|----------|----------|
| Critical | Must fix before implementation |
| High | Must fix or explicitly accept with rationale |
| Medium | Fix if touched, otherwise track |
| Low | Document only |

**Current verdict:** ____________

---

## Contract

- [ ] CORE.md loads and enforces Tier 0 invariants
- [ ] PAIRING_MODE.md preserves all existing behaviors
- [ ] MULTI_AGENT_MODE.md is self-consistent
- [ ] Philosophy statement present in preamble
- [ ] Spec discipline section present
- [ ] Mode selection gate works
- [ ] Cross-mode operations blocked

---

## Blackboard

- [ ] Schema validates correctly
- [ ] DRAFT state respected (coders ignore)
- [ ] Locking prevents concurrent write corruption
- [ ] Lease model works (extend, expire, reclaim)
- [ ] Claim backoff prevents hot spinning
- [ ] Log append works under concurrent access
- [ ] SUPERSEDED state tracks rescoping correctly
- [ ] spec_ref field supported in tasks

---

## Worktrees

- [ ] Worktree created from correct branch
- [ ] Coder isolated to worktree
- [ ] Coder cannot commit to integration
- [ ] Code Reviewer can read worktree
- [ ] Code Reviewer-only merge enforced
- [ ] Commit SHA verified before merge
- [ ] Clean git status required for READY_FOR_REVIEW
- [ ] Merge succeeds (fast-forward and merge commit)
- [ ] Conflict detected and marked INTEGRATION_FAILED
- [ ] Integration-fix claimable after failure

---

## Roles

- [ ] Planner reads specs before decomposition
- [ ] Planner can decompose goal (DRAFT → UNCLAIMED)
- [ ] Planner verifies specs exist for tasks
- [ ] Planner can rescope with audit trail
- [ ] Coder reads specs before work
- [ ] Coder can claim with backoff
- [ ] Coder marks BLOCKED with reason and clarifying questions for under-specified tasks
- [ ] Coder can iterate and request review
- [ ] Code Reviewer reads specs before review
- [ ] Code Reviewer validates against spec
- [ ] Code Reviewer can approve/reject
- [ ] Code Reviewer can execute merge
- [ ] Rejection reason references spec where applicable
- [ ] Blocked escalation reaches planner
- [ ] Hypothesis exhaustion triggers rescope

---

## Supervision

- [ ] Agent supervisor restarts on exit 42
- [ ] Agent supervisor respects PAUSE
- [ ] Agent supervisor respects CHECKPOINT
- [ ] Agent supervisor stops on ABORT
- [ ] Restarted agent reads specs fresh
- [ ] Restarted agent verifies lease
- [ ] Restarted agent reads handoff notes
- [ ] Context exhaustion triggers graceful handoff
- [ ] Agent can call liza-lock.sh via bash
- [ ] Agent can call wt-create.sh, wt-merge.sh
- [ ] Restart-to-check model works for review waiting
- [ ] Supervisor backoff prevents hot spinning on waiting

---

## Bootstrap

- [ ] liza-init.sh creates valid blackboard
- [ ] Specs exist before planner starts
- [ ] Planner can decompose without human interaction
- [ ] Startup order documented and tested
- [ ] Agent invocation syntax works with current Claude Code
- [ ] Agent reads contract chain (CORE.md → MULTI_AGENT_MODE.md)
- [ ] Agent identifies role from prompt
- [ ] Agent verifies identity from $LIZA_AGENT_ID
- [ ] Agent follows role-specific startup procedure

---

## Human Override

- [ ] PAUSE halts all agents
- [ ] Resume continues correctly
- [ ] Force replan triggers planner
- [ ] Abort terminates gracefully
- [ ] Human notes visible to agents
- [ ] Injected tasks claimable

---

## Alarms

- [ ] Lease expired detected
- [ ] Blocked task surfaced
- [ ] Review loop detected
- [ ] Integration failure alerted
- [ ] Hypothesis exhaustion alerted
- [ ] Stall detected
- [ ] Invalid state detected

---

## Sprint Governance

- [ ] Sprint section in blackboard schema
- [ ] Sprint initialized by liza-init.sh
- [ ] CHECKPOINT file halts all agents
- [ ] Supervisors wait on CHECKPOINT (like PAUSE)
- [ ] Sprint summary generated at checkpoint
- [ ] Retrospective template available
- [ ] Checkpoint release resumes agents
- [ ] Sprint deadline detected by watcher
- [ ] Sprint metrics collected correctly
- [ ] liza-checkpoint.sh creates CHECKPOINT and generates summary

---

## Circuit Breaker

- [ ] Anomalies section in blackboard schema
- [ ] Coders log retry_loop anomalies
- [ ] Coders log trade_off anomalies
- [ ] Code Reviewers log scope_deviation anomalies
- [ ] liza-analyze.sh parses anomalies
- [ ] Pattern rules detect retry_cluster
- [ ] Pattern rules detect debt_accumulation
- [ ] Pattern rules detect assumption_cascade
- [ ] Report generated on trigger
- [ ] CHECKPOINT created on trigger
- [ ] Severity classification correct
- [ ] History recorded after resolution

---

## Spec Evolution

- [ ] vision.md template exists
- [ ] ADR template exists
- [ ] Planner blocks without vision.md
- [ ] spec_changes logged in blackboard
- [ ] Spec changelog format documented
- [ ] ADR triggers documented
- [ ] Spec update flow tested
- [ ] Tasks reread specs after update

---

## Anomaly Logging

- [ ] Coder logs retry_loop (> 2 iterations same error)
- [ ] Coder logs trade_off (suboptimal accepted)
- [ ] Coder logs spec_ambiguity (interpretation required)
- [ ] Coder logs external_blocker (external dependency)
- [ ] Coder logs assumption_violated (spec assumption false)
- [ ] Code Reviewer logs retry_loop (if not already logged by coder)
- [ ] Code Reviewer logs scope_deviation (work differs from spec)
- [ ] Code Reviewer logs workaround (bypass detected)
- [ ] Code Reviewer logs debt_created (technical debt)
- [ ] Code Reviewer logs assumption_violated (spec assumption contradicted)
- [ ] Planner logs hypothesis_exhaustion (two coders failed same task)
- [ ] Planner logs spec_gap (missing spec discovered)
- [ ] Logging happens at time of occurrence

## Related Documents

- [Phases](phases.md) — implementation sequence
- [Future](future.md) — deferred items and roadmap
