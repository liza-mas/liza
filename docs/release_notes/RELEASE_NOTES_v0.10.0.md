# Liza v0.10.0

v0.10.0 is a consolidation release. The production Go code was restructured
area by area under a black-box oracle that proved behaviour unchanged, and
architecture guards in `make lint` now hold the result: layer imports, function
and file size, and blackboard mutation only through named `ops` operations. Six
listed behaviour changes came with it. Each was ruled before the run started
and shipped as its own task.

Around it, planning learned to freeze interfaces before coding and to correct
them under review instead of reopening architecture. Validation cost became
admissible work, with deferred tiers, per-class concurrency limits and visible
progress. Agent pools now size themselves to claimable work, and each role can
run its own CLI and model. Lock contention on the shared state, the main cause
of stalls in long runs, dropped once heartbeats and observation reads moved off
the state lock.

311 commits on `main` since v0.9.1 (1,023 including the consolidation branch
history), through `197364916`.

---

## Highlights

**Architecture consolidation** -- Production Go had grown from 307 to 558 files
in seven weeks, with `internal/ops` reaching 47,200 lines. A nine-area run
reworked it without changing what users or agents observe. `ops` is split into
private concern files for the kernel, planning, review and acceptance, and merge
and integration. Finite control-flow sets became typed vocabularies owned by
the package that speaks them. `prompts` and `jsonout` no longer import `ops`.
Launch resolution and provider signals moved into `internal/agentlaunch` and
`internal/providersignal`. The `internal/testguard/archguard` package enforces
the result in `make lint`: the dependency rule, functions within 100 lines and
files within 800, and the C6 mutation guard. Every oracle golden was generated
from the starting commit, and only the task owning a listed change could alter
one ([ADR-0218](../../specs/architecture/ADR/0218-architecture-guard-package-and-ratchet-data-seam.md)
to [ADR-0230](../../specs/architecture/ADR/0230-canonical-provider-unavailable-signals.md),
[ADR-0241](../../specs/architecture/ADR/0241-one-initialization-implementation.md)
to [ADR-0243](../../specs/architecture/ADR/0243-configured-lease-duration-at-every-lease-site.md)).

**Interfaces freeze before coding** -- Consumers ask for what they need early, and
providers freeze their contracts only after recorded human decisions or a
declared wait. Corrections go through independent review and keep the output
and allocation identities that coding already depends on. Architects and code
planners record a reality probe for each claimed behaviour, place proofs along a
test pyramid inside a proof environment and budget set by the goal, and weigh
each task's fixed cost before splitting work. Shared-file writers are ordered
only when their edits do not commute. **`get tasks` now defaults to active work
-- see Breaking Changes.**

**Validation is admitted work** -- Canonical commands can be deferred to a
goal-level tier that runs in concurrent lanes on periodic integration batches,
carrying passes forward over unchanged scopes. Operators cap how many heavy
commands of one class run at once across submit, review update, heal and the
deferred runner. Submits report queue position and progress on stderr every five
minutes and refuse a duplicate at once. A timed-out batch can be retried once
under an operator policy. Strict tasks declare runtime inputs, and agents
cannot see host variables the operator withholds.

**Agent pools follow demand** -- Pool repair starts one agent per claimable task
that idle agents do not cover, up to each role's `max-instances` (default 3).
Surplus supervisors retire themselves, and in new workspaces idle doers and
reviewers exit after 10 minutes instead of 5 hours. `.liza/models.yaml` selects a CLI and model per
role and binds each quorum review slot to its reviewer entry. pi and GitHub
Copilot CLI join the provider catalog. **Pool caps change -- see Breaking
Changes.**

**Recovery continues instead of restarting** -- A flagless `unblock-task` now
resumes the current iteration, and each flag states one effect. Agents may
replace a blocked lineage at most twice before an operator decides. Human-owned
blocks raise `AWAITING HUMAN`. Supervisor blocks save doer work as a WIP commit
before blocking. Executing claims whose holder died are taken over, and review
bases orphaned by integration rollbacks are healed by rebase. Pause and the
circuit breaker now stop new work inside the committing transaction.

**State contention drops** -- Heartbeats publish per-generation liveness records
beside `state.yaml` instead of rewriting it under the lock. Supervisors, watch,
the TUI and inspection read lock-free snapshots. Terminal tasks' receipts and
archived history leave the live state and the lock hold.

**Context cost: budgets and opt-in system-prompt contracts** -- Tool results
over 32 KiB are stored as sanitized, content-addressed artifacts, and the model
receives a digest of at most 4 KiB. The boundary sits at each provider's native
hook. Planner prompts carry only their assigned Scope sections, with pinned
navigation. `use-system-prompt` puts the contracts in the Claude and Codex
system prompt, where every lane agent shares them as a cacheable prefix. In
A/B runs this cut a small task's cost by about 40 % on Claude.

---

## Breaking Changes and Upgrade Notes

| Change | Impact and migration |
|--------|----------------------|
| Role agent pools capped | Non-orchestrator roles without their own `max-instances` are capped at 3 live agents (previously unlimited); a fourth manual start is refused at registration. Raise the cap with `config set config.max_instances <n> --replace --reason "<why>"` or a role `max-instances` in the pipeline. New workspaces default doer and reviewer max-wait to 600 s; existing workspaces keep their stored waits. |
| `unblock-task` continues by default | A flagless unblock no longer leads to an iteration increment at the next claim; pass `--new-iteration` when the restore should count. A closed rejection-RCA gate with restore mode `claimable` refuses an unblock without `--new-iteration`. Assign-mode dispositions no longer require `--assign-to`. |
| Claude Bash output no longer captured | `tool-result claude-capture` is removed and the Claude settings overlay registers no `PreToolUse` hook, because rewriting Bash input defeated Claude's native deny rules. Claude Bash output is not budgeted, sanitized or recorded in any permission mode; Read, Grep, Glob and MCP boundaries remain. No migration path ([ADR-0178](../../specs/architecture/ADR/0178-claude-capture-preserves-permissions.md)). |
| Architecture outputs need exact Scope refs | New architecture submissions require an `arch_ref` for every output pointing to an existing exact Scope heading in the committed artifact. Add the fragment and the heading before resubmitting. Existing stored assignments keep their legacy reads. |
| `get tasks` lists active work | `get tasks` and `get-tasks` default to active work. Use `--all` for complete task history; explicit task and field lookups stay complete. |
| Restart all processes together | Heartbeats now write liveness side records ([ADR-0177](../../specs/architecture/ADR/0177-liveness-side-records.md)). After upgrading, restart every supervisor, watch and TUI together rather than mixing versions. |
| `pre-commit` required at init | `init` requires `pre-commit` on `PATH` and records the operator's `--validation-execution local` assertion for each default-resolved CLI ([ADR-0168](../../specs/architecture/ADR/0168-adopt-validation-prerequisites-in-planning.md)). |
| Reviewer diversity compares provider keys | Two approvals from agents launched through `codex` and `codex-acp`, or through custom tools declaring the same `provider_key`, no longer satisfy diversity. A provider-unavailable signal for one tool now stops every tool with the same key. Stored provider values and output are unchanged (C2, C3). |
| `lease_duration` honoured everywhere | Registration, heartbeat, reviewer claims, re-review reclaim and pool repair use the configured `lease_duration` instead of a hard-coded 1800 s. Default configurations see no change (C1). |
| Merge timestamps in UTC | The completion handoff event and the MERGED history entry are now written in UTC. Existing state with local-offset timestamps still loads (C5). |
| Coder and code-reviewer timeout | The embedded pipeline raises the coder and code-reviewer execution timeout to 4 hours. Existing workspaces keep their frozen pipeline. |
| Managed Codex sandbox | Managed Codex launches carry no permission override and keep the workspace-write sandbox. The tool-result hook rewrites Bash only in the configured `bypassPermissions` mode; codex-acp and Windows launch without the boundary instead of escalating ([ADR-0164](../../specs/architecture/ADR/0164-managed-codex-sandbox-posture.md)). |
| CI on pull requests | Pull requests run Linux checks only; macOS and Windows run on `main` pushes, tags and manual dispatch, and releases still wait for both. Forks receive the shared `ci-core.yml` through upstream merges ([ADR-0235](../../specs/architecture/ADR/0235-shared-ci-core-and-ci-gated-releases.md)). |

---

## Features

| Feature | Description |
|---------|-------------|
| Demand-based agent pool | Starts agents per uncovered claimable task up to each role's cap, reports reviewer work no reviewer could claim as `AUTO REPAIR UNSERVABLE`, and lets surplus supervisors retire themselves when `max_instances` drops. |
| Per-role model selection | `.liza/models.yaml`, seeded by `init` flags, selects CLI and model per role; agents and approvals record the model; quorum slots bind to reviewer list entries. See [provider models](../provider-models.md). |
| pi and GitHub Copilot CLI providers | Adds a pi backend with init-gate enforcement and model selection, and a Copilot CLI provider pointed at the instructions file Copilot actually reads. |
| Opt-in system-prompt contracts | `use-system-prompt` builds the contracts into `.liza/system-prompts/` for Claude lanes and Codex outside Windows; `--revert` rolls back. The default stays forced reads. |
| Provider-native tool-result budgets | Stores oversized results as content-addressed artifacts with a bounded digest, at one boundary per provider. See [tool-result budget](../tool-result-budget.md). |
| Interface freeze and reviewed corrections | Freezes provider contracts before coding, adopts bounded contract and identity-preserving corrections under independent review, and lets one reviewed Scope allocate coding units directly. |
| Reviewed plan amendments | `amend-plan` adopts bounded scheduling, prerequisite and appended-slot corrections under the original provider identity. |
| Plan governance | `plan-check` gates automatic expansion of manual hand-offs, holds plans for an operator, and retires unused hand-offs by ruling with `--retire`. |
| Provider dependencies and reservations | Typed provider, transition and output selections retain unborn prerequisites; descendant dependencies defer waits to the writers; reservations hold inserted writers until every child the provider generates has merged. |
| Plan-declared replacement | A plan output names the task it supersedes; generation retires it and retargets consumers atomically. |
| Planning economics | Reality probes for frozen assertions, proof environment and budget decided upstream, proofs along a test pyramid, per-task fixed cost, and shared-file writer ordering only when edits do not commute. |
| Admission checks | Planner self-checks and planning review require room for each child's obligations and check known consumers before freezing. |
| Scope-assigned planner context | Architect and code-planner doers receive only their assigned Scope sections with pinned navigation; full validation is unchanged. |
| Deferred validation tier | Defers canonical commands named by the contract or an operator ruling to a goal ledger run in concurrent lanes on periodic batches, skips known-red commands, and lets an operator abort a batch. |
| Admission limits and progress | `admission-limit` caps concurrent heavy commands per class; submits report queue position and progress, refuse duplicates, and may retry a timed-out batch once under `config.admission_timeout_retry`. |
| Runtime inputs and withheld variables | Strict tasks declare runtime inputs with an HMAC-keyed consumption ledger; `config.agent_env_deny` withholds named host variables from every process supervisors start. |
| Validation preflight | Catches missing validation tooling before coding work and raises a deduplicated `VALIDATION PREFLIGHT FAILED` alert. |
| Blocked-assessment wait-for sets | A blocked assessment names the unfinished tasks it waits for, with a write-time deadlock guard; an awaited set wakes once when all merge or any fails. |
| Bounded recovery | Caps agent blocked-recovery replacements per lineage; `release-blocked-chain` lets an operator release a capped lineage without pausing; unblock can grant in-task scope exceptions and human-ruled admission budget exceptions. |
| Rework-first claims | Doer claims keep explicit priority, then prefer owned rejections, other rework and active dependency demand; rejected work is rebased onto current integration at re-claim. |
| Terminal record archival | Opt-in immutable terminal records keep complete history and output out of live state. |
| Index ownership by hooks | Lifecycle Git hooks own repo-root indexes, a successful merge triggers a detached refresh, task worktrees copy the artifacts instead of indexing, and documentation-only commits skip refresh. |
| Dead-end stage commissioning | `add-tasks` admits a parentless task on a many-to-one stage only with the delivery task that consumes it. |
| Seam-scoped integration analysis | Global analysis reviews the goal's own ranges and seams shared by two or more plans; defects in untouched code become observations. |
| Operator tooling | Shows task dependency trees and critical paths, runs orchestrator recovery commands, repairs an unstarted task's empty `arch_ref`, makes a standing delegation the default for rulings, keeps a friction ledger, and lets a watch leg replay alerts written between legs. |
| TUI and inspection | Shows time on task in the agent panel, cycles a task-panel filter with `[f]`, and lists the agent provider in the agents table. |
| Skills | Adds `lean-thinking` for waste and friction inventory, bounds requirements from above and makes unnamed assurance machinery a human decision, and sizes goal-writing to the stakes. |

---

## Fixes

| Fix | Impact |
|-----|--------|
| State lock contention | Supervisor state reads wait up to 60 s under lock saturation, and supervisors stay alive through contention; locked reads decode after release; observation, supervisor and TUI reads use lock-free snapshots; an unchanged state file is not reparsed. |
| Mutation validation | Refuses only violations a transaction introduces, so one old bad record no longer blocks unrelated work; anomaly records are validated at the write boundary. |
| Submission reference checks | Submit refuses what the reviewer prompt or claim would refuse: unresolvable strict fragments, pins naming no commit, and unintegrated direct reference pins, with the carrier cause exposed. |
| Review and verdicts | Retains leased claims, keeps a reviewer's turn alive after its verdict, retires its own verdict on release, fences await-resubmission to its reservation, persists reached verdicts for fenced replay, and retries Windows collisions on pending verdict envelopes. |
| Orphaned review bases | Heals a review base that left the integration branch by replaying only the task range. |
| Claims and doer work | Takes over executing claims whose holder died, adopts uncommitted work in a preserved worktree, commits quota-terminated work before release, keeps doer work when the supervisor blocks a task, and releases dormant assignments when claiming new work. |
| Provider limits and sessions | Expires a quota spawn block at the announced reset, recognises Claude limits from the API status and typographic quotes, stops on an expired Claude login instead of counting crashes, keeps exhausted providers out of auto-repair, and backs off crash restarts per supervisor. |
| Background jobs | Keeps Claude agent turns open while their background jobs run and alerts the operator when a session abandons one. |
| Late and in-flight submits | Lets in-flight validation finish and stops it on claim loss, lets Claude submits outlive the harness command cap, and hands the session back after a late submit. |
| Stale provider declarations | Unexpanded plans, unstarted consumers and draft output no longer hold retired or replanned providers and selected children; replan lineage, same-pair replacement and plan-declared successors carry dependencies and child slots. |
| Orchestrator wakes | Prevents unjustified wakes, prefers the planning hand-off, scopes the `BLOCKED_TASKS` wake to actionable tasks, stops repeated completion wakes, and checkpoints passed plans at the end of any turn. |
| Merges | Retires a merge preparation its own rollback rewound, keeps merges approved when the artifact guard cannot read state, lets an inheriting reviewer settle an orphaned merge, and staffs approved merges with no owner. |
| Watch and alerts | Alerts when the run is parked awaiting a human or no orchestrator holds ownership, writes each condition once per episode, expires auto-repair suppression after transient failures, and no longer reports a 0/0 run as complete. |
| Worktree setup | Serializes and retries setup with a configurable timeout, keeps its output private, copies env files despite a user `excludesFile`, and reports Git stderr on exclude failures. |
| Deferred validation and admission | Bounds runner state text, keeps a finished step across a halt, counts due-ness from the latest batch, serves submits before deferred probes, gives budgeted submits a queue floor, and lets waiters see a changed class limit. |
| Init and setup | Fails when provider contracts cannot activate, excludes the runtime directory from Git status, stops deploying maintainer-only docs, offers to refresh drifted hook entries, and no longer requires a CORE read: the init hook lists unread documents by absolute path ([ADR-0244](../../specs/architecture/ADR/0244-init-sequence-defined-by-core-enforced-by-hook.md)). |
| Initialization Git | Init branch Git runs through timed `gitenv` runners in the project root, so a hung call times out (C4). |
| Indexing | Publishes index artifacts atomically, reads SCIP indexes from the hooks' layout, clears orphaned Semble indexes, pins Semble, and aggregates target-only Python indexes under their real root. |
| Windows | Fixes the native build, install `PATH` and test isolation, passes OpenCode prompts via stdin past the argv limit, and cleans up admission-slot tickets. |
| Build and CI | Shares one CI core with forks, gates releases on the CI-verified tagged SHA, shards Linux tests into five jobs, and checks asset-capturing oracle goldens at commit. |

---

## Documentation

- Adds [provider models](../provider-models.md), the
  [tool-result budget](../tool-result-budget.md) guide and a
  [CI workflow README](../../.github/workflows/README.md).
- Makes run skills and first-run guidance discoverable, and clarifies first-run
  setup and operator guidance.
- Grows the operator skill with long-run recovery lessons, per-round block and
  supersession registers, durable operator fixes and merge waits encoded as
  dependencies.
- Requires zoned timestamps, searching tests before renaming symbols or
  messages, and keeping host environments out of test output in the contracts
  and guardrails.
- Warns that RTK's Claude hook and Codex shell rewrites bypass native command
  rules, and corrects the agent execution timeout tables.
- Documents the target structure in the "Go Code Structure" section of the
  [architecture overview](../../specs/architecture/overview.md) and closes the
  consolidated issues in the
  [architectural issues registry](../../specs/architecture/architectural-issues.md).
- Records this cycle's decisions in the
  [ADR index](../../specs/architecture/ADR/README.md), now at ADR-0244
  (0237 to 0240 are unassigned).

---

## Installation

**Quick install (macOS/Linux, latest published release):**

```bash
curl -fsSL https://raw.githubusercontent.com/liza-mas/liza/main/install.sh | bash
```

**Windows (PowerShell, latest published release):**

```powershell
irm https://raw.githubusercontent.com/liza-mas/liza/main/install.ps1 | iex
```

Windows requires Git for Windows, with its `bash.exe` ahead of the WSL launcher
on `PATH`. Contract symlinks require Developer Mode or an elevated shell;
managed Git hooks can use wrapper scripts when symlinks are unavailable.

**From source:**

```bash
make install
```

To refresh installed contracts, skills, and support docs after upgrading, run
`liza setup --force` with the provider flags used by your installation. This
overwrites selected managed global files; retain custom tool guidance through
`--agent-tools`. Updating global assets does not replace an existing workspace's
frozen pipeline. Restart all supervisors, watch and the TUI together after the
upgrade.
