# Implementation Phases

**Timeline:** 13 days (phases 10-12 can run in parallel, effective ~11 days)

## Phase 1: Contract Restructuring (Day 1)

1. Create `contracts/` directory in project repo
2. Extract CORE.md from current contract
3. Extract PAIRING_MODE.md from current contract
4. Create symlink `~/.claude/CLAUDE.md -> <project>/contracts/CORE.md`
6. Test: verify Claude Code loads and prompts for mode selection

---

## Phase 2: Multi-Agent Contract (Day 2)

1. Write MULTI_AGENT_MODE.md
2. Include philosophy statement and spec discipline in preamble
3. Review for consistency with CORE.md
4. Test: verify mode selection loads correct contract

---

## Phase 3: Blackboard Schema & Validation (Day 3)

1. Write `liza-init.sh` (creates initial state.yaml structure)
2. Write `liza-validate.sh` (enforces schema via invariant checks)
3. Test: initialize blackboard, validate schema
4. Test: invalid states rejected

**Note:** Schema enforcement is procedural via `liza-validate.sh`, not declarative via a schema file. The blackboard-schema.md document defines the canonical structure; the validation script enforces invariants.

---

## Phase 4: Locking & Lease Model (Day 4)

1. Write `liza-lock.sh`
2. Test concurrent access with flock
3. Implement lease extension routine
4. Test lease expiration detection
5. Test claim backoff behavior

---

## Phase 5: Worktree Management (Day 5)

1. Write `wt-create.sh`
2. Write `wt-merge.sh` (with commit SHA verification)
3. Write `wt-delete.sh`
4. Test full lifecycle: create → work → merge
5. Test conflict scenario → INTEGRATION_FAILED
6. Test Code Reviewer-only merge enforcement

---

## Phase 6: Agent Supervision (Day 6)

1. Write `liza-agent.sh`
2. Test graceful abort (exit 42) → restart
3. Test crash → restart with backoff
4. Test PAUSE file → wait
5. Test ABORT file → stop
6. Test lease verification on restart

---

## Phase 7: Watcher & Alarms (Day 7)

1. Write `liza-watch.sh`
2. Test alarm conditions:
   - Lease expired
   - Blocked task
   - Review loop
   - Integration failure
   - Hypothesis exhaustion
   - Stall detection
   - Invalid state
3. Configure desktop notifications (optional)

---

## Phase 8: Integration Testing (Day 8-9)

1. Manual walkthrough: Planner → Coder → Code Reviewer → Merge
2. Test DRAFT → UNCLAIMED flow
3. Test rescoping with SUPERSEDED state
4. Test spec-driven workflow:
   - Planner verifies specs exist
   - Coder reads specs before work
   - Code Reviewer validates against spec
5. Test failure scenarios:
   - Coder hits max iterations
   - Code Reviewer rejects repeatedly
   - Integration conflict
   - Hypothesis exhaustion (two coders fail)
   - Under-specified task → BLOCKED
6. Test human override:
   - PAUSE/resume
   - Force replan via BLOCKED
   - Abort
   - Human notes
7. Test context exhaustion handoff

---

## Phase 9: Documentation (Day 10)

1. Bootstrap guide (human startup sequence):
   - liza-init.sh usage
   - Writing specs/vision.md
   - Starting watcher
   - Launching agents in order
2. Usage guide for each role
3. Troubleshooting guide
4. Example session transcript
5. Quick reference card

---

## Phase 10: Sprint Governance (Day 11)

1. Update blackboard schema with sprint section
2. Update `liza-init.sh` to initialize sprint
3. Write `liza-checkpoint.sh`:
   - Creates CHECKPOINT file
   - Generates sprint summary
   - Waits for human release
4. Update `liza-watch.sh` to detect sprint deadline
5. Update supervisor to respect CHECKPOINT
6. Write retrospective template generator
7. Test checkpoint flow: trigger → halt → review → release

---

## Phase 11: Circuit Breaker (Day 12)

1. Update blackboard schema with anomalies section
2. Update agent contracts with logging duties
3. Write `liza-analyze.sh`:
   - Parse anomalies section
   - Apply pattern rules
   - Generate report if triggered
4. Test pattern detection:
   - Inject retry_cluster pattern → verify detection
   - Inject debt_accumulation → verify detection
   - Inject assumption_cascade → verify detection
5. Test report generation
6. Test CHECKPOINT trigger from analyzer
7. Document severity classification for human review

---

## Phase 12: Spec Evolution (Day 13)

1. Create vision.md template
2. Create ADR template
3. Update `liza-init.sh` to require vision.md:
   - Check `specs/vision.md` exists before initializing
   - If missing: exit with error "vision.md required — copy from templates/vision-template.md"
   - Planner also checks on startup; exits with same message if missing
4. Add spec_changes section to blackboard
5. Update planner contract: verify vision exists
6. Update coder contract: log spec_ambiguity
7. Update Code Reviewer contract: log assumption_violated
8. Test spec change flow:
   - Checkpoint reveals gap
   - Human updates spec
   - Change logged
   - Tasks unblocked
9. Test ADR creation trigger from circuit breaker

---

## Dependencies

```
Phase 1 (Contract Restructuring)
    │
    ▼
Phase 2 (Multi-Agent Contract)
    │
    ├─────────────────────┐
    ▼                     ▼
Phase 3 (Blackboard)   Phase 5 (Worktrees)
    │                     │
    ▼                     │
Phase 4 (Locking)         │
    │                     │
    └─────────┬───────────┘
              ▼
       Phase 6 (Supervision)
              │
              ▼
       Phase 7 (Watcher)
              │
              ▼
       Phase 8 (Integration Testing)
              │
              ▼
       Phase 9 (Documentation)
              │
    ┌─────────┼─────────┐
    ▼         ▼         ▼
Phase 10   Phase 11   Phase 12
(Sprint)   (Circuit)  (Spec Evo)
```

Phases 3-4 and 5 can run in parallel. Phases 10-12 can run in parallel after Phase 9.

## Related Documents

- [Tooling](tooling.md) — script specifications
- [Validation Checklist](validation-checklist.md) — v1 completion criteria
