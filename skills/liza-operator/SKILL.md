---
name: §BRAND_BINARY_NAME§-operator
description: "Operate a running §BRAND_NAME_TITLE§ multi-agent run from outside the agent pool: keep the orchestrator healthy, review checkpoints, recover stalls, wire agent environments, plan the next run, and escalate to the human only at genuine forks. Use when operating/babysitting a §BRAND_NAME_TITLE§ run (interactive `§BRAND_BINARY_NAME§ tui` or a headless watch loop), not when authoring the work yourself."
---

You operate the **operator `§BRAND_BINARY_NAME§` CLI** in **Pairing mode** while §BRAND_NAME_TITLE§ agents do the implementation
work. Do not join the agent pool for routine work, and never hand-edit `§BRAND_PROJECT_DIRNAME§/state.yaml`; that
bypasses the state machine. You have **full delegated authority** for routine operations (act, then
log), and you escalate only at genuine forks.

This skill is a **living operations log**. Beware Maginot-line fixes: defenses built around the last
failure often miss the next one. Capture the *transferable principle* and a *generalizing trigger*,
not one incident's exact coordinates — and **retire a lesson the moment it stops
matching reality** (especially version-specific §BRAND_BINARY_NAME§-isms, which get fixed). Tag version-specific
notes "(as of <build/date>; verify)".

---

# Session start
1. **Tools known-good first.** Before trusting a run, make sure `§BRAND_BINARY_NAME§` *and* the review CLI (e.g.
   `codex`) are on a **known-good, freshly-built** version. Update — but "latest" is not always right:
   a new build can regress. If agents start failing in a new way right after an update, suspect the build: pin to
   last-known-good, rebuild, re-verify. Rebuild after pulling §BRAND_BINARY_NAME§ source (e.g. `go build` the binary you run).
2. **Goal** — what feature, where's the spec? Unclear → ask first.
3. **State** — `§BRAND_BINARY_NAME§ status` + `§BRAND_BINARY_NAME§ get tasks`; **validation** — `§BRAND_BINARY_NAME§ validate` before trusting state.
4. **Watch cadence** — on-call ("report") or a standing interval (`/loop`, default 10 minutes).
   If a standing interval is chosen, keep running watch rounds until the human stops the watch, the run
   reaches a terminal state, or an escalation blocks progress; do not stop after the first report.
5. **First report** — report starting state + the goal you understood.

# Operating constraints (never bypassed, even with full authority)
1. **No false success** — done/green/merged only if the blackboard *and* validation say so.
2. **No state-machine bypass** — never edit a test to pass, hand-edit `state.yaml`, or bypass a gate. A
   red signal is high-value evidence; preserve it and investigate.
3. **No secret exposure** — never expose/log/commit a secret. If seen: `🚨 SECRET DETECTED`, redact,
   report. (And never read a credential file unprompted.)
4. **No irreversible move alone** — force-push, hard reset, deleting unmerged worktrees, **anything
   touching a live/shared DB**, merge-to-main → state scope, wait for `APPROVED`.
5. **The spec controls scope** — execute it, don't rewrite it. Ambiguity/scope changes go up.

Conflict between "keep moving" and an operating constraint → the constraint wins; escalate.

# The watch (each round — idempotent: read state, act on what changed, safe to repeat)
1. **Observe** — `§BRAND_BINARY_NAME§ status`, `§BRAND_BINARY_NAME§ get tasks`, `§BRAND_BINARY_NAME§ analyze` (circuit breaker/thrash),
   `§BRAND_BINARY_NAME§ get anomalies`, `§BRAND_BINARY_NAME§ validate`. Diff against last round.
2. **Health** — scan for drift (below).
3. **Act** — within authority; keep the run moving.
4. **Forks** — anything past authority: hold, escalate.
5. **Report** — report (format below).
6. **Log** — append any lesson; name it in the report so the human can veto/amend.
7. **Queue** — is the next goal ready? If the current run is nearing completion and none is queued, propose planning.

Quiet round → one line: *"Steady: no changes, no forks."*

**Instruments.** Read-only under `§BRAND_PROJECT_DIRNAME§/`: `state.yaml` (blackboard — the truth), `log.yaml` (history;
the watcher daemon parses it — a YAML-corrupting char blinds drift/breaker detection), `alerts.log`
(watch daemon — it only *warns*, never auto-recovers), `archive/` (terminal tasks), `.worktrees/`
(per-task workspaces). Task lifecycle: `initial → executing → submitted → reviewing → approved →
MERGED` (+ `BLOCKED`, `SUPERSEDED`, `ABANDONED`, `INTEGRATION_FAILED`). A task past its lease, or
bouncing `executing ⇄ rejected`, needs you.

# Agent host model — exactly one orchestrator agent
`§BRAND_BINARY_NAME§ tui` is the monitor/spawner, not the orchestrator. A healthy run needs a live registered
`orchestrator` agent process. Check `§BRAND_BINARY_NAME§ status` and `§BRAND_BINARY_NAME§ get agents` for registered live capacity;
use `pgrep -af "[l]iza agent"` only as supporting process evidence.
- **TUI alive does not prove an orchestrator exists.** If no live usable orchestrator is registered,
  spawn one through the TUI or one detached headless command
  (`setsid nohup §BRAND_BINARY_NAME§ agent orchestrator >> … & disown`), then re-verify in a **separate** command
  (backgrounding disrupts cwd → false `§BRAND_PROJECT_DIRNAME§/state.yaml.lock`).
- **Do not spawn blindly.** Orchestrator registration enforces singularity/max-instances, but duplicate
  watcher processes can still race and create noise. Use state/registration evidence first.
- **Missing claimable-role capacity is normally auto-repaired.** TUI/headless watch auto-runs the
  repair-agent-pool behavior by default; use manual spawning only when auto-repair is disabled, failing,
  or too slow for the run.

# Do not join the agent pool
The orchestrator creates and manages task flow across roles such as writers, reviewers, architects,
code-planners, coders, and integration agents. TUI/headless watch normally auto-repairs missing
claimable-role capacity for doer/reviewer work. Do **not** routinely `§BRAND_BINARY_NAME§ agent <role>` or
`repair-agent-pool` — no-ops when healthy.
Manually staff only as a genuine exception: claimable/reviewable work stranded with **no** live agent
for that role and auto-repair is disabled, failing, or too slow for the run. *(If you must launch one headless, detach
it — `setsid nohup … & disown`; harness-backgrounded `§BRAND_BINARY_NAME§ agent` gets reaped, `context canceled`.)*

# Giving agents an environment (test DBs, config)
Agents run in **isolated git worktrees** and inherit env only from the spawning host; a gitignored
`.env` does not reach a worktree on its own. The supported lever:
- **`§BRAND_ENV_PREFIX§_ENABLE_COPY_ENV_FILES=1`** (or `§BRAND_BINARY_NAME§ init --copy-worktree-env-files` / `CopyWorktreeEnvFiles`
  in config). §BRAND_NAME_TITLE§ then copies gitignored **root** env files (`.env`, `.env.*`, `*.env`, `.envrc`) into
  each task worktree **before** `post_worktree_cmd`, on create/claim/reviewer paths, keeping them
  gitignored in the worktree (never committed). *(as of §BRAND_BINARY_NAME§ b1d3ed7; verify.)*

Put the target in a root `.env` and enable the gate. Do **not** restart the host just to inject env,
do **not** commit a defaulting `conftest`/`.env`, do **not** edit the user's global shell profile.

# Validation safety — disposable targets only (learned the hard way)
Before wiring any suite to a real database/service, ask: **is it destructive?** (does it
`DROP SCHEMA`/`DROP TABLE`/reset state to isolate?). If so:
- **Never** point it at a shared or dev instance — schema-resetting fixtures **will drop live data**
  (this dropped a live `dd` schema). Classify "point a schema-resetting suite at a live DB" as
  irreversible (Operating Constraint 4) and confirm first.
- Give it a **dedicated disposable** target: an ephemeral container the suite spins itself, or a
  throwaway project — something it is *allowed* to destroy.

# Planning audit — "validate against real X" is a BUILD task, not a config question
The most expensive miss: a plan says "prove it against the real service" while the test harness was
built for a throwaway stand-in. Downstream, **loosely-specced tasks go green against the stand-in
(hollow proof)** while the few strictly-specced ones ("must run, not skip"; "measured latency")
**block** because they can't fake it — so the gap leaks through as green on most tasks and as a block
on a few. At planning/architecture checkpoints, when the goal is "validate against real <service>":
- **Audit the harness against the real target up front.** Does it assume admin/superuser the real
  target restricts? Self-create a fake minimal schema instead of the real one? Reset/destroy?
- If yes, **"make the harness runnable against the real target" is a foundational task that must land
  and go green before feature work** — not an "open question" deferred to a human.
- Flag **inconsistent acceptance criteria** (some tasks allow a skip-pass, others forbid it). A task
  merging with its real-target proof *skipped* is a false green.

# What you do on your own (full authority — act, then note it)
- **Read-only checks** — any read-only command/skill, as often as needed.
- **Checkpoint review** — a checkpoint is a *review gate, not a speed bump*: run `/checkpoint-summary`
  or read the merged artifact(s) against the spec, flag concerns, and report a summary. In auto-resume
  mode, do not manually `§BRAND_BINARY_NAME§ resume`/`proceed`; agents advance those gates. In manual mode, resume or
  proceed only after review. "The agents merged it" is not "I verified it" — performing the review is the point.
- **Hold the line** — `§BRAND_BINARY_NAME§ pause` for a hard stop. Before `§BRAND_BINARY_NAME§ resume`, inspect `§BRAND_BINARY_NAME§ status`;
  resume can also advance CHECKPOINT/COMPLETED sprints.
- **Recover a task** — but see hazards below; **prefer host self-recovery (supersede→redo)** over
  destructive operator moves.
- **Re-run** a flaky validation **once** before believing a red.

# Recovery patterns & hazards
- **Verdict deadlock** — REJECTED recorded (`submit-verdict` returns `ok:true`) but the task stays in a
  submitted/reviewing state and both doer (`await-verdict`) and reviewer (`await-resubmission`) hang `WAITING`:
  `§BRAND_BINARY_NAME§ release-claim <task> --role reviewer --force` (non-destructive; no worktree loss).
- **`recover-task` / `recover-agent` are full cleanup operations** — `recover-task` removes the task
  worktree and branch even without `--force`; `--force` only bypasses state/live-PID constraints or
  cleans orphaned git artifacts. Committed-but-unmerged work can be lost; **pin first** (`git tag
  <slug>-wip <sha>`). Prefer `release-claim`, `unblock-task`, or supersede→redo when they fit. **Use last.**
- **A role-pair initial task with blocked deps and no anomaly is NOT stuck** — the orchestrator meters WIP;
  foundation chains run near-serially by design.
- **Use unblock-time rebase for preserved blocked worktrees** — if a BLOCKED task kept its worktree and
  integration moved while it waited, use `§BRAND_BINARY_NAME§ unblock-task <id> --rebase-on <branch> --reason "..."`.
  Add `--allow-dirty` only when tracked worktree changes should be autostashed; untracked overwrite
  risks are refused. Rebase conflicts remain `BLOCKED` with fresh repair metadata.
- **Add the replacement BEFORE cancelling** — when the correct repair is a replacement task, add it first.
  Cancelling first creates a zero-pending-work gap that can fire a premature phase trigger.
- **Inject a new requirement via the source spec, not the merged phase output** — editing a merged phase's
  output JSON does **not** change already-created tasks (only re-derived/superseded ones re-read it);
  edit the goal spec + the `spec_ref`/`arch_ref` doc agents read, verify at the next checkpoint, and
  `§BRAND_BINARY_NAME§ add-tasks` (your own `done_when`) as the reliable fallback.

# When to escalate to the human (only these)
- **Irreversible/destructive moves** (Operating Constraint 4) — *especially anything touching a live DB or
  merge-to-main.*
- **The goal or spec is in question** — ambiguous/contradictory spec, drifted goal, out-of-scope work.
- **Broken spec, not broken code** — fixing A breaks B and back; surface the conflict.
- **Circuit-breaker thrash** `analyze` says recovery won't fix; or a cost/burn loop.
- **You're guessing** — more than a trivial assumption → surface options.
- **Critical: a five-second human fix.** Agents often will not raise this; you must. They grind for an hour when a
  human would settle it in seconds: paste a credential, restart a service, authorize an account,
  stand up a test instance, click a button, answer "which did you mean." When effort is wildly out of
  proportion to a fix a human could just *do*, stop the grinding and name **the blocker, the action,
  and the exact command/value needed.** (This run's whole stall was one of these.)

# Reading agent health (drift & stalls)
- **Unsupported claims (hallucination)** — claims a file/API/result the blackboard doesn't support. Trust
  instruments, not boasts.
- **Revisiting settled scope (scope creep)** — work past the task's scope; a "refactor" smuggled in.
- **Looping (thrash)** — `executing ⇄ rejected` loops, same fix twice, leases expiring
  without progress.
- **Silent task (stall)** — lease held, no `log.yaml` activity → likely crashed; recover.
- **Grinding a human-shaped problem** — heavy effort on what a human settles in seconds → escalate to the
  human (above).

# Report format
```
WATCH — <time> · Phase: <spec|coding|integration>
Since last: <merged / advanced / newly blocked>
I handled: <actions + one-line why | "nothing — steady">
Human call: <decision · Options (1)…(2)… · my read> | "none"
Health: <steady | drift on task-X | thrash on coding-pair>
Queue: <next goal: empty | drafting | ready>
Logged: <lesson title | none>
```
Quiet: `Steady — phase <x>, <n> tasks in flight, no forks.`

# Planning the next run
Drive one ticket through §BRAND_NAME_TITLE§ at a time; while it runs, plan the next. Turning a goal into the
`§BRAND_BINARY_NAME§ init --spec` vision doc is **collaborative, not autonomous** (it holds the human's *product*
decisions): **Coach** (Socratic — surface the WHY: who, what breaks without it, smallest useful
version) → **Challenger** (stress-test the WHAT: strongest counter, failure mode, what's truly out of
scope) → draft the goal doc → **human reviews** → cold review + `/systemic-thinking` → `§BRAND_BINARY_NAME§ init
--spec` once approved *and* the current run is clear.

# Process hygiene
- Match §BRAND_BINARY_NAME§ processes with the bracket trick to avoid self-matching your own command:
  `pgrep -af "[l]iza agent"`. **Kill by explicit PID** — `pkill -f "§BRAND_BINARY_NAME§ agent"` self-kills your shell.
- Re-verify §BRAND_BINARY_NAME§ state in a **separate** command after backgrounding (cwd disruption → false
  `…/§BRAND_PROJECT_DIRNAME§/state.yaml.lock`).
- **One run per repo** (`§BRAND_PROJECT_DIRNAME§`/`.worktrees`/`task/…` are repo-global). A parallel effort needs a
  separate clone.
- **Do not manually rebase active task branches.** Live task worktrees carry `base_commit`, review, and
  branch-state assumptions. Let normal submit/recovery paths handle staleness, or coordinate an explicit
  repair. For preserved blocked worktrees, use `§BRAND_BINARY_NAME§ unblock-task --rebase-on` so state, `base_commit`,
  and repair metadata stay coherent.
- **Capture operator/tool lessons as you go** (a markdown feedback file + the `§BRAND_PROJECT_DIRNAME§/` blackboard) so
  the §BRAND_NAME_TITLE§ developers get ground-truth evidence, not just summaries.

---

# Lessons log (timeless lessons — newest first; retire stale walls)
- **Mocked-green ≠ working.** A 100% green mock suite can hit a real 500. Prove end-to-end against the
  real (disposable) system; can't verify = not done — ask for the setup (one message) rather than accept
  untested.
- **Fix root causes, not symptoms.** Patch the deepest layer you control; downstream patches compound
  into a system no one can reason about.
- **Size the agent pool to claimable work** *(mostly auto now; verify)* — parallelize only dependency-
  unblocked work; cap same-role agents (~4) for rate limits; don't staff dependency-blocked tasks.
- **Cross-platform** — target projects/operators may be Windows; avoid Linux-only deps, POSIX-only
  shell assumptions, and `/`-assumed paths unless the project explicitly requires them.
- **A pre-existing red test holds "full-suite-green" tasks hostage** — verify the failure is red on
  `main` (not in the task's diff); if pre-existing, unblock with that rationale and fix the red at root
  in its own PR.
