# 117 - Fail-Closed Worktree Readiness

## Status

ACCEPTED — Supersedes the failure-handling decision of ADR-0031. The configuration
mechanism, trust model, and stack-agnostic constraint of ADR-0031 remain in force.
Amended 2026-10-22 (D-74): setup is serialized per worktree and retried once, the
child's output tail is kept in a private file, and a later passing setup clears
the setup degradation it disproves. Amended 2026-10-23 (D-34): each setup call
is bounded by a configurable timeout. See the amendments at the end.

## Context and Problem Statement

ADR-0031 made worktree setup configurable via `post_worktree_cmd` and chose a
permissive failure strategy: "Failures produce warnings, never block task
claiming." The rationale was operational — let agents proceed and analyze logs
in batches.

A run on 2026-08-22 showed what that costs. The repository had no
`post_worktree_cmd` configured, and 45 provider sessions in a single day failed
on the same missing generated input (`pattern contracts/*.md: no matching files
found`) across coder, integration-analyst, code-plan-reviewer, code-planner, and
integration-reviewer roles. Agents spent turns rediscovering and repairing the
same prerequisite.

`daf90df7` addressed the missing-configuration half at init time. This ADR
addresses the second half: when a command *is* configured and fails, a warning in
a log nobody reads is indistinguishable from success. Worse, several provider
entry points never ran the command at all.

## Considered Options

1. **Keep warn-only, improve diagnostics** — the 2026-08-22 run is evidence that
   better warnings do not get read during a run.
2. **Fail closed, block the task on failure** — rejected for reviewers: see
   Decision.
3. **Fail closed, release and degrade** — chosen.

## Decision Outcome

A configured `post_worktree_cmd` must succeed before a provider session starts.

**Doer claim** (`claim_task.go`): the claim aborts in Phase 2, before Phase 3
commits any task or agent state, so no claim exists for a worktree that is not
build-ready. The worktree is left on disk so the failure can be reproduced
against it; a later fresh claim treats it as a stale resource.

**Doer resume** (`claiming.go`): `ResumeHandoff` and `ResumeOwnedTask` return
before `ClaimTask`, so they enforce setup themselves. Enforcement is not placed
in `doerStrategy.PreExecution` because the supervisor logs `PreExecution`
failures and continues; using that hook would not fail closed without changing
the error contract for every strategy, including the orchestrator.

**Reviewer sessions** (`worktree_check.go`, `strategy_reviewer.go`): the command
runs on both the recovery path and the intact-worktree path, because reviewers
build and test and need the checkout the doer had. On failure the reviewer claim
is **released** back to the task's reviewable status and the reviewer agent is
degraded.

Blocking the task was rejected. `unblock-task` restores a BLOCKED task to the
doer's executing status, which would discard completed, review-ready doer work
for an environment fault that left the worktree intact. `blockReviewerTask`
remains reserved for genuinely lost work (worktree and branch both missing).

**Retry is bounded** by classification: the failure is an infrastructure claim
error (`claim_worktree_setup_failed`), so the agent is marked degraded with a
recover hint naming the command and worktree, and the supervisor exits on
`ErrAgentDegraded` before building a prompt. Without this bound, failing closed
would produce a hot claim loop across every candidate task.

Degradation is owned by `ops.ClaimTask` rather than by the supervisor claim
loop, because that loop is not the only production caller: `await-verdict`
auto-reclaims a rejected task through the same function, and a per-caller bound
left that path retrying without one. Resume and reviewer paths bypass
`ClaimTask`, so they degrade explicitly at their own boundaries.

**The diagnostic is the command, the worktree, and the exit status — nothing
from the child process.** (Amended by D-74: the output tail is kept in a private
file whose path, never its content, joins the diagnostic.) `RunPostWorktreeCmd` does not capture the command's
output at all, and `PostWorktreeSetupError` has no field for it.

This is a security decision, not a brevity one. Masking cannot cover the child.
`secretmask` is built from this process's environment and recognizes
credential-shaped key names (`API_KEY`, `*_TOKEN`, `*_SECRET`, `*_PASSWORD`, and
a fixed provider list). Worktree env files are copied in before setup runs, so a
command can source `.env` and print a value this process never saw — and a key
like `DATABASE_URL` matches none of those rules, so a masking-based defense would
pass a connection string containing a password straight through into agent logs.
Since no masking of child output is a guarantee, none is attempted and nothing is
captured. CORE.md's secret-handling rule (T0.5) covers logs, not just durable
state.

The command itself is operator-supplied configuration, so it is masked with the
parent environment and capped head-first at 256 bytes; with a
filesystem-bounded worktree path and a short exit status, the whole rendered
diagnostic is bounded. The operator reproduces the failure by rerunning the named
command in the named worktree, which the recover hint instructs.

**No opt-out.** A per-project "warn only" switch would reintroduce exactly the
silent mode this ADR removes.

### Consequences

**Positive:**
- A setup failure surfaces as a named, actionable state instead of a log line.
- Review progress survives a setup failure; only the reviewer is degraded.
- Every provider entry point is covered, including resumes.

**Limitations accepted:**
- A flaky setup command now idles agents instead of producing unreliable work.
  Deliberate: a stalled run with a named cause is recoverable, output built on a
  broken environment is not.
- A failing worktree is left on disk with no task state referencing it until the
  next claim reclaims it as stale.
- The failing command's output is not shown anywhere. Diagnosing a setup failure
  requires rerunning the command in the worktree. Accepted as the price of a
  guarantee rather than a best-effort filter; it also removes the unbounded
  in-memory buffering that capturing output required.

Implemented in `internal/ops/post_worktree_setup_error.go`,
`internal/ops/wt_create.go`, `internal/ops/claim_task.go`,
`internal/ops/agent_health.go`, `internal/agent/claiming.go`,
`internal/agent/worktree_check.go`, `internal/agent/strategy_reviewer.go`.

## Amendment — 2026-10-22 (D-74): serialized, retried setup with private output

**Context.** In one run, claim-time setup (`bun install`) failed transiently with
exit 1 more than a dozen times across several days. Each time the next claim
passed, and an operator rerun reported no changes. The diagnostic said only
`exit status 1`, so the cause could not be identified. The failures clustered on
claim races of one task worktree, which is consistent with concurrent setups in a
single worktree; this was never verified. When another agent finished the task,
the degraded record stayed until an operator cleared it, and twice that left a
role at zero capacity.

**Decisions** (all in `RunPostWorktreeCmd`, the single entry point for every
claim, resume, review, recovery and recreate path):

1. **Serialized per worktree.** A cross-process file lock, keyed by the worktree's
   resolved path, covers the whole setup. A waiter that does not acquire it within
   30 minutes fails closed with a setup error.
2. **Retried once.** A failed run is retried once after 2 s, inside the same lock
   hold. That makes at most two runs per call, and a run that fails both attempts
   degrades the agent as before.
3. **Private output file.** The last 64 KiB of each attempt's combined
   stdout/stderr goes only to `<runtime>/worktree-setup/<base>-<digest>.log`
   (file 0600, directory 0700). The file is rewritten whenever any attempt fails,
   including one the retry absorbs. The error, the degraded record, its anomaly,
   and the recover hint carry only the file's path. The output never reaches
   logs, state, anomalies, alerts, or error text. The security rationale above
   still holds for those surfaces. The file adds no exposure beyond the operator
   rerunning the command, but one risk is accepted: a process that can read the
   project runtime directory can read the file, just as it can read a copied
   `.env`. The human chose this option over a masked tail in state (the ADR-0204
   approach) and over no capture.
4. **Outcome record and reconciliation.** Under the lock, each run writes the
   worktree's latest outcome: `<seq> ok|failed <time>`, where `seq` increases by
   one per recorded run.
   - **Skipping a stale degrade.** A failure skips its degrade only when its own
     run recorded a seq and a run with a higher seq has since recorded `ok`. This
     check is made under the state lock, inside the degrade mutation.
   - **No seq, no skip.** A failure that recorded no seq always degrades. That
     covers a lock timeout, an unreadable record, and a record write that failed.
   - **Clearing on success.** After a passing run, while its `ok` is still the
     latest outcome, every `claim_worktree_setup_failed` record for that task
     degraded before the pass is cleared. The clear uses the caller's authority,
     so it is generation-fenced; operator and CLI paths pass none. A degrade
     published after the pass is judged by the skip rule instead.

**Consequences.** A hung holder can stall a second claim of the same worktree for
up to 30 minutes before it fails closed. A persistent failure takes one extra run
and 2 s longer to degrade. Output and outcome files accumulate, one pair per
worktree that ever ran setup, and are not removed.
Deferred-validation lanes keep their own runner (ADR-0204).

Implemented in `internal/ops/post_worktree_setup.go`, `post_worktree_setup_error.go`,
`agent_health.go`, `claim_task.go`, and `internal/agent/claiming.go`.

## Amendment — 2026-10-23 (D-34): bounded setup

**Context.** Claim-time setup ran with no deadline. In one run a host's broken
IPv6 left `bun install`'s registry fetch in TCP SYN-SENT for about 12 minutes
on every fresh worktree. The setup never exited, so the fail-closed path above
(which acts on a failed exit) never ran. The claiming agents looked idle and
healthy, with live heartbeats and no alert, anomaly or degraded mark, and fresh
claims stopped across the run.

**Decisions.**

1. **Configurable bound.** `config.post_worktree_cmd_timeout_seconds` bounds
   each `RunPostWorktreeCmd` call. A non-positive or absent value means 1800 s,
   the bound deferred validation already applies to the same command. The
   budget starts once the call holds the worktree's setup lock and covers both
   attempts, the retry pause included. A setup that exceeds it returns the
   setup error `timed out after <bound>` and takes the existing fail-closed
   path. A timed-out setup is not retried.
2. **Waiting for the lock.** A waiter now waits for the holder's bound plus a
   1-minute slack, instead of a fixed 30 minutes.
3. **Process cleanup.** Each attempt runs in its own process group. On Unix the
   deadline kills the whole group, even after the shell has exited, so a
   descendant that holds the output pipes cannot outlive the bound. The group of
   a failed or timed-out attempt is emptied before a retry starts and before the
   lock is released. If it cannot be emptied, the setup fails with no retry. A
   passing attempt is not reaped, so a setup that detaches a process with its
   output redirected keeps its earlier behavior.
4. **Windows limitation.** Windows can kill the tree only while its root process
   is still alive. A descendant that outlives its shell there is not stopped
   (TECH_DEBT).

**Consequences.** A setup that legitimately takes longer than the bound now
degrades the agent. Operators raise the key for slow cold setups, or lower it
where a hang is the likely failure. The 1800 s default bounds an indefinite
hang but would not have caught the 12-minute one. The setup child no longer
shares the caller's process group, so a terminal interrupt of the caller does
not reach it directly. Every other command run with `configProcessGroupKill`
already makes this trade-off.

Implemented in `internal/ops/post_worktree_setup.go` and
`internal/models/config.go`, with callers passing
`models.EffectivePostWorktreeCmdTimeout`.
