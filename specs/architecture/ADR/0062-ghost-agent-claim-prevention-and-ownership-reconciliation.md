# 62 - Ghost Agent Claim Prevention and Ownership Reconciliation

## Context and Problem Statement

Liza could accumulate ghost agent rows: no role, no PID, or otherwise corrupt identity records that still had valid leases. These rows could hold work, reclaim work after recovery, or block real agents from attaching. Task-side ownership fields (`assigned_to`, `reviewing_by`, waiting claims) could also outlive the agent rows that originally owned them.

This was the root cause behind execution-progress ambiguity: an agent swarm can only be supervised if task ownership and agent identity are consistent. A CLI can work without its supervisor, and a supervisor can exist without a live CLI. Heartbeat alone cannot define ownership.

The observed effect was a system with apparent idle capacity but no useful progress: work was held by invalid agents or stale task fields, not by active workers.

## Considered Options

1. **Continue repairing ghost rows manually** with `recover-agent` and `release-claim` as separate operator actions.
2. **Prevent corrupt identities from claiming and validate active ownership invariants** across task state and agent rows.

## Decision Outcome

Chose **Option 2**: harden agent identity at claim time and make active ownership a validated, repairable invariant.

### Architecture

**Claim identity hardening:**
- Task claim operations reject missing or corrupt agent identities.
- Heartbeat reports of missing agent rows cause supervisors to stop instead of recreating zero-value rows.
- Recovery/unregister paths release task-side claims even when agent metadata is already missing.

**Active ownership invariants:**
- Review ownership is valid only when task state is a reviewing state, the reviewer role matches the task role pair, and the reviewer agent points back to that task.
- Doer ownership is valid only for executing states and valid handoff/sentinel cases.
- Invalid ownership is surfaced by validation and watch alerts.

**Repair paths:**
- `validate --repair` can clear invalid review ownership.
- Conservative doer repair skips live doer processes, refuses unsafe cleanup, leaves worktrees for inspection, and clears only safe stale claims.
- Late or stale reviewer verdicts can be preserved as anomalies rather than disappearing.

### Rationale

This addresses the swarm-consistency root cause. Liza must not treat a lease or heartbeat as sufficient proof of valid ownership when the agent identity is corrupt or the task points at a stale owner.

The repair path is intentionally conservative. Recovering doer agents can destroy task worktrees, so Liza must distinguish "dead metadata" from "bad metadata with active output growth."

### Consequences

**Positive:**
- Invalid agent rows no longer silently claim or reclaim work.
- Validation can detect task/agent ownership drift instead of leaving it to operators.
- Repair commands can reconcile task-side and agent-side state in safer, targeted ways.
- Status and watch output become more useful for swarm health.

**Limitations accepted:**
- Ownership validation adds cross-entity constraints to state validation.
- Some stale claims require conservative refusal rather than automatic cleanup.
- Output growth can indicate active work despite broken metadata, so repair cannot blindly delete all no-PID/no-role rows.

**Extends:** ADR-0053 (Supervisor Resilience) - swarm health now includes identity and ownership invariants. ADR-0060 (Agent Execution Progress Watchdog) - fixes a root cause behind unreliable progress/liveness interpretation.

---
*Reconstructed from commits 5a7495a1..86ad55e0, GitHub issue #73, and liza-run-issues.md (2026-05-18 to 2026-05-19)*

## 2026-09-22 Amendment: Lease-First Review Ownership

The user explicitly approved lease-first review ownership on 2026-09-22,
accepting up to the current default 30-minute lease duration for automatic
review recovery after a supervisor crash. This extends
[ADR-0130](0130-generation-fenced-agent-authority.md)'s lease-first registration
authority to review-claim retention; registration occupancy is unchanged.

Cleanup still requires the exact reviewer role, matching `current_task`, a
usable PID, appropriate `REVIEWING`/`WAITING` status, and a current review lease.
For that coherent tuple, a nonzero heartbeat and unexpired registration lease
preserve ownership despite namespace-relative dead/mismatched PID evidence.
The registration lease bounds freshness; the nonzero heartbeat establishes
that the registration has initialized heartbeat evidence.
Missing/expired review leases or inconsistent owner metadata remain stale.
Without current registration lease/heartbeat evidence, cleanup retains the
existing live-matching/live-unknown process fallback. A registration lease's
expiry alone therefore does not universally establish stale review ownership.

Headless reviewer execution separately checks current registration authority
and review ownership at launch and at the normalized heartbeat interval
(default 60 seconds). Proven loss cancels the provider turn; transient observer
read failures retry. This turn's own new verdict permits final response and
`WAITING`, while active re-review resets the older verdict allowance. Provider
cancellation terminates the owned local process group/tree and bounds local
output-pipe waiting, without promising termination of detached or remote
processes. See the [supervision contract](../supervision-model.md#generation-fenced-recovery-contract)
for cancellation and observation limits.

This amendment preserves the original requirement that heartbeat alone cannot
repair corrupt ownership. It adds no schema fields or dependencies.
