# Liza v0.9.0

The main improvement in v0.9.0 is how work is decomposed throughout planning.
Clearer task boundaries, dependency direction, and handoffs lead to fewer
conflicts, fewer superseded tasks, and work that composes better downstream.
Sliced integration addresses the context pressure of a single integration task
by distributing local analysis across bounded scopes while retaining an
independent global review.

The release also restores native Windows support, adds ACP-backed execution
and a configurable provider catalog, and introduces build-time white-label
branding. Stronger recovery, local toolchain installation, terminal launchers,
and goal-writing guidance support the smoother execution flow.

195 commits since v0.8.0, through `f54c443e`.

---

## Highlights

**Better decomposition throughout planning** -- Improved decomposition across
all planning stages makes autonomous runs smoother: fewer conflicting tasks,
less supersession churn, and better downstream composition. Clearer separation
of decomposition and specialized planning responsibilities, executable dependency
proofs, and stronger handoff and replacement-lineage rules help tasks fit
together before execution begins.

**Sliced integration relieves context pressure** -- A single integration task
previously had to absorb the full analysis workload. Bounded local analyses
distribute that workload, with eligible coding-review attestations reused as
coverage evidence. An independent global review still checks how the complete
branch fits together. Global fixes or later integration-head changes require
fresh global analysis within the configured generation budget, and completion
requires clean evidence for the current head.

**Native Windows support returns** -- Windows amd64 and arm64 release artifacts,
a PowerShell installer, and Windows CI restore native operation without WSL2.
Git for Windows supplies the POSIX shell used by hooks. Path handling, managed
hook installation, and replacement of a running executable during updates now
account for Windows behavior.

**Provider catalog and ACP execution** -- Provider definitions now describe
launch arguments, detection, and contract activation through a validated
catalog. ACP variants are synthesized from shared provider metadata, including
Codex, Cursor, and OpenCode backends. ACP execution streams transcripts and
usage metadata, scopes sessions to tasks, and checks required executables before
launch. Project-local agent tools and profiles support custom launch definitions.

**Recovery preserves ownership and work** -- Registration generations fence
stale agent writes and provider starts. Recovery preserves review evidence,
quorum requirements, leases, and reusable worktree artifacts. Dependency repair
batches validate and commit the complete graph change atomically, and blocked
tasks wake when relevant dependencies or descendants change.

**Build-time white-label branding** -- Central brand values and rendered assets
cover binary names, runtime roots, environment variables, hooks, documentation,
and distribution surfaces. Go module identity remains stable, legacy environment
aliases remain supported, and existing installation roots are not moved
automatically.

**A shorter setup path** -- Local toolchain profiles, WezTerm and CMUX launchers,
shell completion, opt-in update checks, and non-interactive setup confirmations
reduce manual setup. The new goal-writing workflow records human decisions and
uses an independent readiness review before handing a goal to autonomous agents.

---

## Breaking Changes and Upgrade Notes

| Change | Impact and migration |
|--------|----------------------|
| Provider catalog schema v2 | Older binaries cannot refresh the v2 remote catalog. Upgrade the binary before relying on catalog refreshes. |
| Global-first contract activation | Providers with a supported global instruction path prefer it over duplicate repository links. Init verifies global activation before removing managed duplicates and preserves user-owned files. A legacy Kimi `CLAUDE.md` link without recorded ownership may need restoration with `liza init --provider kimi`. |
| User-owned Claude permission mode | `liza init --claude` removes `permissions.defaultMode` from project `.claude/settings.json`. Set interactive preferences in `~/.claude/settings.json`. MAS launches explicitly request `--permission-mode auto`; project `agent_tools` can override launch arguments. Check auto-mode compatibility with the chosen account and model. |
| Pipeline role-type validation | Role pairs now reject incompatible role types. A doer position must reference a role declared as `type: doer`, and a reviewer position must reference `type: reviewer`. |
| TUI task creation removed | The `[a]` add-task flow is gone. Use `liza add-task` or the normal orchestrator workflow. |
| Worktree setup fails closed | A failing configured `post_worktree_cmd` now prevents provider execution instead of merely warning. Fix the command in the retained worktree before recovering the affected agent. Reviewer setup failures release the review claim so the task remains reviewable. |
| Frozen pipeline topology | Existing workspaces retain their frozen pipeline. A cohort that requires slice analysis but lacks its topology reports `pipeline_upgrade_required`; use a fresh workspace or a deliberate manual topology update. Zero- and one-scope cohorts can still use a valid existing global integration pair. |
| Compact generated task IDs | New default pipelines use shorter role-pair and transition slugs. Existing workspaces keep their original IDs and naming behavior. Avoid depending on generated names when scripting task discovery. |
| Codex preference ownership | Generated configuration no longer supplies model, effort, or personality defaults. Existing user values are preserved. |

See [Configuration](../../support-docs/CONFIGURATION.md) for provider placement,
launch overrides, and worktree setup, and
[Sliced Integration](../../support-docs/USAGE_MULTI_AGENTS.md#sliced-integration)
for the integration upgrade boundary.

---

## Features

| Feature | Description |
|---------|-------------|
| Planning decomposition | Improves task boundaries, dependency direction, and handoffs across planning stages, reducing conflicts and supersession churn while improving downstream composition. |
| Sliced integration analysis | Relieves the context pressure of one integration task through bounded local analyses and reused coding-review attestations, followed by independent global review. |
| Native Windows distribution | Restores Windows amd64/arm64 builds, zip archives, PowerShell installation, and platform CI coverage. |
| Provider catalog commands | Adds `providers list`, `providers detect`, and `providers refresh`, with validated remote metadata, caching, and synthesized ACP variants. The catalog's `disabled` field is informational rather than an execution block. |
| Structured agent launches | Adds project-local `agent_tools`, reusable agent profiles, and `agent --explain-launch` to inspect resolved launch arguments without starting a provider. |
| ACP backends | Adds the provider-neutral agent boundary and ACPX execution, including streamed transcripts, lifecycle metadata, and task-scoped sessions. |
| Global-first activation | Resolves provider-specific configuration roots and records repository activations to avoid duplicate contract loading while preserving shared paths. |
| Toolchain management | Adds `toolchain list`, `doctor`, `install`, and `configure`, with lean, balanced, and full profiles plus explicit tool selection. |
| Terminal launchers | Starts MAS and adversarial-pairing sessions in supported WezTerm and CMUX layouts with provider selection and initial prompts. |
| Shell completion | Generates command-aware completion and integrates activation with toolchain configuration. |
| Explicit project selection | Supports project-root selection independently of the current working directory. |
| Update management | Adds opt-in update checks and self-update with persisted preferences, artifact verification, and rollback preparation. |
| Safe project reset | Adds cleanup planning, confirmation, ownership checks, and lifecycle locking shared with resource creation and reinitialization. |
| Optional bash-policy setup | Delegates provider shell policy setup to the standalone bash-policy CLI, including Cursor hooks. |
| Functional-cluster context | Refreshes optional functional-cluster artifacts and advertises them in agent and Pairing context. |
| Multi-root SCIP indexes | Aggregates supported language roots rather than exposing only one root's index. |
| Atomic dependency repairs | Adds structured `mark-blocked --repair-request-file` requests and `apply-dependency-repair` with expected-state checks, graph validation, and auditable receipts. |
| Terminal dependency repair | Adds `repair-superseded-dependencies` to repair illegal downstream edges on already-superseded tasks. |
| Defect-objective RCA | Requires reviewed diagnosis for defect objectives, supports per-output overrides, and separates decomposition-root duties from specialized implementation planning. |
| Executable dependency proofs | Requires planning handoffs to demonstrate dependency readiness and preserve replacement lineage and direction. |
| Destructive validation metadata | Adds `destructive_db` task/output metadata and requires an explicit leading authorization marker on destructive database validation commands. |
| Planning churn detection | Extends circuit-breaker analysis to planning review loops and distinguishes warning, checkpoint, and halt responses with explicit acknowledgement. |
| Goal preparation skills | Adds input-readiness assessment, goal-writing with a companion decision record and independent final review, and an operator skill for supervising runs. |
| Two-sided code review | Defines author responses, bounded corrective scope, and explicit accept/counter/refute/escalate routes for disagreements. |

---

## Fixes

| Fix | Impact |
|-----|--------|
| Generation-fenced lifecycle authority | Rejects stale authenticated lifecycle writes and orders registration against provider startup. Fresh leases retain authority even when sandbox process observations are uncertain. |
| Approved and rejected task recovery | Preserves valid review evidence and recoverable work while allowing eligible current-generation agents to finish stranded work. |
| Integration ref and index locking | Serializes shared integration mutations so concurrent merges cannot corrupt index synchronization or restore stale state. |
| Repository worktree metadata locking | Serializes metadata mutations across processes at the Git wrapper boundary. |
| Current-head completion | Prevents stale clean analysis from authorizing sprint progression or automatic goal completion after integration changes. |
| Planning barrier enforcement | Waits for decomposition and eligible downstream output consumption before freezing integration scope or advancing the sprint. |
| Dependency-held task recovery | Restores eligible blocked work, reconciles stale blocked metadata, handles terminal superseded dependencies, and avoids inheriting replanned upstreams. |
| Quorum-aware pool repair | Provisions reviewer capacity that can actually claim the remaining review slots; permits merge quorum with one provider when diversity is not required. |
| Bounded review waits | Restores bounded await behavior and limits session-preserving review loops instead of allowing indefinite waits. |
| Pipeline-aware status | Aligns per-role readiness, ownership, dependencies, planning merge blockers, and actual claim eligibility across diagnostics. |
| Assignment elapsed time | Measures task age from the current assignment and resets it on unblock rather than carrying obsolete elapsed time. |
| Provider failure classification | Unifies Codex provider failures and distinguishes runtime command failures from no-progress loops. |
| ACP session behavior | Sets Cursor sessions to agent mode, avoids cross-task session reuse, and accounts for no-progress reclaims. |
| Managed hooks | Preserves the managed dispatcher and hook identity, fixes Windows path handling, and avoids repeated hook reinstallation. |
| Initialization readiness | Warns and requests confirmation when no worktree setup command can be selected; adds `--yes` support for setup and init. |
| Review diagnostics | Bounds rejection reasons, surfaces verdict submission failures, repairs missing review boundaries, and clears obsolete integration failure metadata after approval. |
| Supervisor and log diagnostics | Persists lifecycle diagnostics, reports uncertain process scope, surfaces permission friction, and avoids guessing context-window sizes. |
| Pairing indexes and hooks | Preserves usable SCIP indexes after individual root failures and limits Codex SessionStart context to startup. |
| Build and validation tooling | Derives build versions from Git, makes Go cache URLs portable to Git Bash, accelerates routine tests, and removes timing and fixture races. |

---

## Documentation

- Reworks the [README](../../README.md) around accountable agents and autonomous
  software delivery, with clearer explanations of human governance, behavioral
  contracts, adversarial review, and decomposition. Improves mode selection and
  onboarding, refines comparisons, and makes the evidence and limits explicit.
- Adds a central [Getting Started](../../GETTING_STARTED.md) guide and a
  [Toolchain guide](../../support-docs/TOOLCHAIN.md).
- Expands configuration and troubleshooting for ACP, provider activation,
  Windows, recovery authority, and integration closure.
- Records architectural decisions for provider execution, white-label builds,
  toolchain setup, safe cleanup, dependency repair, lifecycle generations, and
  goal readiness in the [ADR index](../../specs/architecture/ADR/README.md).
- Updates architecture issue evidence and code-quality assessments, and removes
  intermediate implementation plans.
- Strengthens adversarial-pairing coordination, worktree path discipline,
  initialization order, minimality guidance, and review convergence.
- Adds a [Contributor License Agreement](../../Liza-CLA.md), scoped to pull
  request submissions.

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
frozen pipeline.
