# §BRAND_NAME_TITLE§ - Usage Guide

## §BRAND_NAME_TITLE§

See [DEMO](../docs/DEMO.md) for a full example.

This guide assumes §BRAND_NAME_TITLE§ has already been installed, `§BRAND_BINARY_NAME§ setup` has been run,
`AGENT_TOOLS.md` has been reviewed, and the target project is ready for §BRAND_NAME_TITLE§
initialization. For setup/configuration details, see
[Configuration Reference](CONFIGURATION.md) and
[Customizing AGENT_TOOLS.md](CUSTOMIZING_AGENT_TOOLS.md).

### Key Concepts

**Goal** — A §BRAND_NAME_TITLE§ workspace (`§BRAND_PROJECT_DIRNAME§/`) is bound to a single goal. The goal is defined at `§BRAND_BINARY_NAME§ init` with a description and a spec reference. All tasks, sprints, and agent activity within that workspace serve this goal. To pursue a different goal, stop its agents and run `§BRAND_BINARY_NAME§ cleanup` before re-initializing; full init invokes the same cleanup flow when workspace data remains. One project can only have one active goal at a time.

**Checkpoints** — Hard checkpoints halt execution so a human can review before the system continues. Transition checkpoints for planning output gate only downstream task creation: the orchestrator waits for `§BRAND_BINARY_NAME§ resume`, while doer/reviewer agents may continue already-available work in the current sprint. If you want uninterrupted transition execution, enable auto-resume (`§BRAND_BINARY_NAME§ init --auto-resume` or press `y` in the TUI).

**Worktrees** — Agents don't work directly on your main branch. Each task gets its own [git worktree](https://git-scm.com/docs/git-worktree) (under `.worktrees/task-N/`), giving agents isolated workspaces that can't interfere with each other or with your working copy. Completed work merges into the integration branch, then into main. This means §BRAND_NAME_TITLE§ requires a git repository and only one §BRAND_NAME_TITLE§ context per repository.

**Git boundary** — §BRAND_NAME_TITLE§ manages local git state: task worktrees, task branches, review commits, and the configured integration branch. Publishing commits to GitHub, opening pull requests, choosing remote PR bases, enabling automerge, or reconciling GitHub-side branch state is an operator-owned handoff workflow, not part of §BRAND_NAME_TITLE§'s lifecycle.

### Project Structure

```
~/§BRAND_GLOBAL_DIRNAME§/                               # Created by `§BRAND_BINARY_NAME§ setup`
├── CORE.md                            # Universal rules + mode selection gate
├── PAIRING_MODE.md                    # Human-supervised collaboration
├── MULTI_AGENT_MODE.md                # Peer-supervised §BRAND_NAME_TITLE§ system
├── AGENT_TOOLS.md                     # Agent tool contracts
├── COLLABORATION_CONTINUITY.md        # Session continuity
├── pipeline.yaml                      # Default pipeline config (role-pairs, transitions, entry-points)
└── skills/                            # Skill definitions
    ├── code-review/SKILL.md
    ├── context-engineering/SKILL.md
    ├── debugging/SKILL.md
    ├── §BRAND_BINARY_NAME§-logs/SKILL.md
    └── ...

<project>/
├── GUARDRAILS.md                  # Project-specific constraints (optional)
├── §BRAND_PROJECT_DIRNAME§/
│   ├── state.yaml                 # Current state
│   ├── pipeline.yaml              # Frozen pipeline config (validated at init from --config)
│   ├── log.yaml                   # Activity history
│   └── archive/                   # Terminal-state tasks
└── .worktrees/
    └── task-N/                    # Per-task workspace
```

### Project Guardrails

`GUARDRAILS.md` is an optional file at the project root that defines project-specific constraints for §BRAND_NAME_TITLE§ agents. It uses the same tier system (Tier 0-3) from the core contract:

- **Tier 0 (Inviolable)** — Triggers mandatory halt (RESET) if violated
- **Tier 1 (Hard Constraints)** — Suspended only with explicit waiver
- **Tier 2 (Strong Defaults)** — Best-effort under pressure
- **Tier 3 (Preferences)** — Degraded gracefully

**How it's created:** `§BRAND_BINARY_NAME§ init` writes a template with empty tier sections. You can also create it manually.

**How to use it:** Fill in the tier sections with project-specific rules. Agents read and enforce `GUARDRAILS.md` automatically during their initialization sequence. If the file doesn't exist, agents are governed by the core contract only.

### Quick Start (Target Usage)

Use this section once the global setup is complete.

> **Optional but highly recommended:** enable `scip-search` for
> repository-navigation-heavy MAS runs. `§BRAND_ENV_PREFIX§_ENABLE_SCIP_SEARCH` is the MAS
> activation gate; use repeated `--scip-search <language>` init options when you
> want an explicit allowlist. See [Configuration Reference](CONFIGURATION.md) for
> the canonical details on `config.scip_search`, repeated
> `--scip-search <language>` values, indexer prerequisites, and `§BRAND_PROJECT_DIRNAME§/scip/`
> snapshot index locations.

> **Optional:** enable `stacklit-cli` for low-token repository maps in MAS
> prompts. `§BRAND_ENV_PREFIX§_ENABLE_STACKLIT` is the activation gate. Commit curated
> Stacklit inputs such as `stacklit-insights.json` and `.stacklitrc.json` when
> you use them, and either commit or ignore generated `stacklit.json`. §BRAND_NAME_TITLE§
> refreshes task-local `stacklit.json` files in worktrees for prompt context
> without adding them to task diffs. See [Configuration Reference](CONFIGURATION.md)
> for the runtime contract and non-goals.

> **Optional:** enable Semble for semantic discovery when MAS agents need
> natural-language repository search before exact symbols or modules are known.
> `§BRAND_ENV_PREFIX§_ENABLE_SEMBLE` is the activation gate. Run `§BRAND_BINARY_NAME§ init --spec` with
> Semble installed so §BRAND_NAME_TITLE§ can prewarm the model/cache and perform offline
> validation. For unattended work, set `HF_HUB_OFFLINE=1` in the environment that
> launches §BRAND_NAME_TITLE§ agents after Semble is installed or prewarmed. MAS prompts mention
> Semble only when offline validation succeeds and `.sembleignore` safety rules
> protect runtime, generated, and credential files. See
> [Configuration Reference](CONFIGURATION.md) for setup, offline behavior,
> `.sembleignore` scope, routing, and non-goals.

**1. Initialize Project**

> **Commit your spec file and `.pre-commit-config.yaml` before running `§BRAND_BINARY_NAME§ init`.** Worktrees are created from the configured integration branch, so uncommitted files won't be visible to agents unless you explicitly enable ignored root env-file copying with `--copy-worktree-env-files` or `§BRAND_ENV_PREFIX§_ENABLE_COPY_ENV_FILES=true`.

```bash
# Interactive wizard: walks through setup choices.
§BRAND_BINARY_NAME§ init

# Project initialization only: activates provider contract links/hooks/config.
§BRAND_BINARY_NAME§ init --claude --codex

# MAS run initialization: creates §BRAND_PROJECT_DIRNAME§/state.yaml from a goal/spec.
§BRAND_BINARY_NAME§ init "[Goal description]" --spec [spec_ref]

# Auto-confirm init approval prompts such as config merges and setup suggestions.
§BRAND_BINARY_NAME§ init "[Goal description]" --yes

# spec_ref: Path to goal specification (default: specs/vision.md)
# .pre-commit-config.yaml must exist on the configured integration branch.
# Examples:
#   §BRAND_BINARY_NAME§ init "Implement retry logic"                        # uses specs/vision.md
#   §BRAND_BINARY_NAME§ init "Add auth" --spec specs/auth-feature.md        # uses custom spec
#
# Pipeline config (--config defaults to ~/§BRAND_GLOBAL_DIRNAME§/pipeline.yaml, installed by §BRAND_BINARY_NAME§ setup):
#   §BRAND_BINARY_NAME§ init "Sub-pipelines phase 2" \
#     --post-worktree-cmd "make sync-embedded" \
#     --spec specs/build/2\ -\ Sub-pipelines\ and\ spec\ writing.md
#
# Worktree setup: If package.json is detected and --post-worktree-cmd is not set,
# §BRAND_BINARY_NAME§ init auto-suggests the right install command (npm/yarn/pnpm/bun).
# See CONFIGURATION.md "Worktree Setup" for details.
#
# Integration branch (--branch sets the branch name, default: "integration"):
#   §BRAND_BINARY_NAME§ init "Build auth system" --branch develop
#
# Entry points (--entry-point selects which sub-pipeline to start from):
#   §BRAND_BINARY_NAME§ init "Build auth system" --entry-point general-objective   # full pipeline: epic → US → architecture → code-plan → code
#   §BRAND_BINARY_NAME§ init "Implement from functional spec" --entry-point functional-spec  # architecture → code-plan → code
#   §BRAND_BINARY_NAME§ init "Implement from technical spec" --entry-point technical-spec    # code-plan → code
#   # detailed-spec is a legacy alias for functional-spec.
#   # Add --no-follow-up to execute only the entry-point sub-pipeline.
#   # Simple entry-point work creates one specialized planning task.
#   # Fan-out or uncertain work creates one mapped master planning task first.
#   # Existing frozen §BRAND_PROJECT_DIRNAME§/pipeline.yaml files are not rewritten; manually update them to receive new topology.
#   # If omitted, the orchestrator auto-classifies from the spec content.

# Verify
cat §BRAND_PROJECT_DIRNAME§/state.yaml
```

`§BRAND_BINARY_NAME§ init` creates:
- `§BRAND_PROJECT_DIRNAME§/state.yaml` — Blackboard state
- `§BRAND_PROJECT_DIRNAME§/pipeline.yaml` — Frozen pipeline config (validated copy of the selected `--config`, default: `~/§BRAND_GLOBAL_DIRNAME§/pipeline.yaml`)
- `§BRAND_PROJECT_DIRNAME§/log.yaml` — Activity log
- `.claude/settings.json` — Claude Code project permissions (§BRAND_NAME_TITLE§ CLI, skills, git/build commands)
- provider contract symlinks to `~/§BRAND_GLOBAL_DIRNAME§/CORE.md`: Claude,
  Codex, OpenCode, Gemini, and Qwen prefer their documented global instruction
  paths; Cursor, Kimi, and Devin use repo-root files
- `GUARDRAILS.md` — Project-specific constraints template (if not already present)
- `.bash-policy.yaml` and standalone provider hooks when
  `§BRAND_ENV_PREFIX§_ENABLE_BASH_POLICY=1` is set before init, the
  `bash-policy` CLI is installed, and hook setup succeeds
- Integration branch (default `integration`, configurable via `--branch`) — For merging completed work

Contracts and skills live in `~/§BRAND_GLOBAL_DIRNAME§/` (global, from `§BRAND_BINARY_NAME§ setup`), not in the project.
Operational reference content (blackboard fields, anomaly types, etc.) is inlined directly into agent prompts.

**2. Start Agents**

The TUI (`§BRAND_BINARY_NAME§ tui`) is the primary way to spawn and monitor agents. Press `s` to spawn with the configured default CLI (role names autocomplete from the pipeline config), or `S` to pick a specific CLI.

Alternatively, spawn agents from the CLI: `§BRAND_BINARY_NAME§ agent <role>`. Agent identity defaults to the first `{role}-N` not already registered with a valid lease (e.g., `coder-1`, or `coder-2` if `coder-1` is active). Override with `--agent-id` or the `§BRAND_ENV_PREFIX§_AGENT_ID` environment variable. After resolution, `§BRAND_BINARY_NAME§ agent` exports that ID as `§BRAND_ENV_PREFIX§_AGENT_ID` to the spawned provider CLI, including `-i` interactive sessions, so hooks select Multi-Agent mode rather than Pairing mode.

To launch a whole role set in one WezTerm window, use `§BRAND_BINARY_NAME§ launch wezterm mas`.
It starts `§BRAND_BINARY_NAME§ tui` in the first pane and one `§BRAND_BINARY_NAME§ agent <role>` process per
additional pane:

```bash
§BRAND_BINARY_NAME§ launch wezterm mas --preset technical-spec
§BRAND_BINARY_NAME§ launch wezterm mas --preset functional-spec
§BRAND_BINARY_NAME§ launch wezterm mas --preset general-objective
```

**CMUX support:** §BRAND_NAME_TITLE§ also supports CMUX as an alternative to WezTerm. Use
`§BRAND_BINARY_NAME§ launch cmux mas` with the same flags and presets:

```bash
§BRAND_BINARY_NAME§ launch cmux mas --preset technical-spec
§BRAND_BINARY_NAME§ launch cmux mas --preset functional-spec
§BRAND_BINARY_NAME§ launch cmux mas --preset general-objective
```

Both WezTerm and CMUX launchers support the same flags: `--preset`, `--role`,
`--cli`, `--no-tui`, `--class`/`--workspace`, `--cwd`, and `--dry-run`. CMUX
launchers create a CMUX workspace and use `cmux send` + `cmux send-key enter`
for prompt injection, avoiding TUI slash command issues.

| Launch command | Equivalent panes |
|----------------|------------------|
| `§BRAND_BINARY_NAME§ launch wezterm mas --preset technical-spec` | `§BRAND_BINARY_NAME§ tui`; `§BRAND_BINARY_NAME§ agent orchestrator`; `§BRAND_BINARY_NAME§ agent code-planner`; `§BRAND_BINARY_NAME§ agent code-plan-reviewer`; `§BRAND_BINARY_NAME§ agent coder`; `§BRAND_BINARY_NAME§ agent code-reviewer` |
| `§BRAND_BINARY_NAME§ launch wezterm mas --preset functional-spec` | Everything in `technical-spec`, plus `§BRAND_BINARY_NAME§ agent architect`; `§BRAND_BINARY_NAME§ agent architecture-reviewer` |
| `§BRAND_BINARY_NAME§ launch wezterm mas --preset general-objective` | Everything in `functional-spec`, plus `§BRAND_BINARY_NAME§ agent epic-planner`; `§BRAND_BINARY_NAME§ agent epic-plan-reviewer`; `§BRAND_BINARY_NAME§ agent us-writer`; `§BRAND_BINARY_NAME§ agent us-reviewer` |

Pass `--cli <name>` to force the same backend for every launched role, or repeat
`--role <role>` to launch a custom role set instead of a preset:

```bash
§BRAND_BINARY_NAME§ launch wezterm mas --role orchestrator --role coder --role code-reviewer --cli codex
§BRAND_BINARY_NAME§ launch cmux mas --role orchestrator --role coder --role code-reviewer --cli codex
```

CMUX equivalents provide the same pane layouts as WezTerm but in a CMUX workspace.


Supported CLI names for `--cli`, `--default-cli`, `--default-doer-cli`, and
`--default-reviewer-cli` include the embedded providers `claude`, `codex`,
`codex-acp`, `cursor-acp`, `opencode`, `opencode-acp`, `gemini`, `mistral`, and
`kimi`, plus catalog-backed providers loaded at runtime, such as `qwen`,
`qwen-acp`, `devin`, and `devin-acp`. Use `§BRAND_BINARY_NAME§ providers list`,
`§BRAND_BINARY_NAME§ providers detect`, and `§BRAND_BINARY_NAME§ providers refresh`
to inspect or refresh the provider catalog.

For OpenCode, run both `§BRAND_BINARY_NAME§ setup --opencode` and `§BRAND_BINARY_NAME§ init --opencode`
before spawning agents. Init installs §BRAND_NAME_TITLE§'s managed
`.opencode/tools/exec.ts` compatibility tool, which OpenCode agents should use
for shell and file operations instead of relying on stricter built-in tool
schemas.

The interactive TUI and headless watch automatically repair claimable work that is stuck because no live usable agent is registered for the required role. Successful auto-repair spawns are written to `log.yaml` as informational events and do not raise alerts; failed spawns raise `AUTO REPAIR FAILED`. Agents marked degraded for the current process epoch do not count as usable role capacity, and their health remains visible after unregister as degraded capacity context. Disable auto-repair with `§BRAND_ENV_PREFIX§_AUTO_REPAIR_AGENT_POOL=0` (or `false`/`no`). To repair manually, run `§BRAND_BINARY_NAME§ repair-agent-pool`. Add `--cli <name>` to choose the backend for newly spawned agents, or `--dry-run` to print the exact spawn commands without launching them.

Avoid running multiple headless watchers for the same project. Auto-repair backoff is per watcher process. §BRAND_NAME_TITLE§'s agent registration and `max-instances` guards prevent invalid ownership, but two watchers can briefly observe the same missing-role gap before a newly spawned agent registers.

Roles are organized into four sub-pipelines (specification, architecture, coding, integration). Which agents you need depends on your entry point:

```
Roles:
  orchestrator            - Creates and manages task breakdown

  Specification phase (general-objective entry point):
  epic-planner            - Decomposes vision into epics; in the master pair, defines the epic-level decomposition framework
  epic-plan-reviewer      - Reviews epic decomposition; in the master pair, verifies boundaries, refs, and manifest coverage
  us-writer               - Writes user stories from epics
  us-reviewer             - Reviews user stories

  Architecture phase (general-objective, functional-spec, detailed-spec):
  architect               - Defines component boundaries, interfaces, and structural decisions
                            (receives parent task context from upstream US tasks or goal spec; in the master pair, owns architectural decomposition)
  architecture-reviewer   - Reviews architectural coherence and structural soundness; in the master pair, verifies decomposition coherence

  Coding phase (all entry points):
  code-planner            - Claims and produces coding plans; in the master pair, defines implementation workstream boundaries
  code-plan-reviewer      - Reviews coding plans and submits verdicts; in the master pair, verifies typed decomposition and artifact refs
  coder                   - Claims and implements coding tasks
  code-reviewer           - Reviews coding tasks and submits verdicts

  Integration phase (post-coding, orchestrator-triggered):
  integration-analyst     - Scans full branch diff for cross-task integration issues
  integration-reviewer    - Validates and enriches integration findings
```

**Functional-spec setup (`functional-spec` or legacy `detailed-spec`) — 7 agents:**
Use `§BRAND_BINARY_NAME§ launch wezterm mas --preset functional-spec`, or spawn from the TUI (`s`): orchestrator, architect, architecture-reviewer, code-planner, code-plan-reviewer, coder, code-reviewer.

**Technical-spec setup (`technical-spec`) — 5 agents:**
Use `§BRAND_BINARY_NAME§ launch wezterm mas --preset technical-spec`, or spawn from the TUI (`s`): orchestrator, code-planner, code-plan-reviewer, coder, code-reviewer.

**Full pipeline (general-objective entry point) — 11 agents:**
Use `§BRAND_BINARY_NAME§ launch wezterm mas --preset general-objective`. This launches all functional-spec roles plus: epic-planner, epic-plan-reviewer, us-writer, us-reviewer.

**Integration phase** agents (integration-analyst, integration-reviewer) are spawned by the orchestrator after all coding tasks for a goal complete. They are not needed at startup — spawn them when the orchestrator triggers the integration sub-pipeline.

Each agent command accepts a `--cli` flag to select the coding agent CLI: `claude`, `codex`, `codex-acp`, `cursor-acp`, `opencode`, `opencode-acp`, `gemini`, `mistral`, `kimi`, catalog providers such as `qwen` or `devin`, or any custom tools configured in `§BRAND_PROJECT_DIRNAME§/state.yaml` under `config.agent_tools`. When `--cli` is omitted, §BRAND_NAME_TITLE§ first applies the selected structured profile, then resolves role-specific config (`config.default_doer_cli` for doers and orchestrators, `config.default_reviewer_cli` for reviewers), role-specific env (`§BRAND_ENV_PREFIX§_DEFAULT_DOER_CLI` for doers and orchestrators, `§BRAND_ENV_PREFIX§_DEFAULT_REVIEWER_CLI` for reviewers), `config.default_cli`, `§BRAND_ENV_PREFIX§_DEFAULT_CLI`, then `claude`. Set built-in defaults at init time with `§BRAND_BINARY_NAME§ init --default-cli codex --default-reviewer-cli gemini "..."`, or edit `state.yaml` directly. Use `§BRAND_BINARY_NAME§ agent coder --explain-launch` to inspect the resolved executable and argv before spawning a provider.
`codex-acp`, `cursor-acp`, and catalog-backed ACP providers such as `qwen-acp` and `devin-acp` use §BRAND_NAME_TITLE§'s ACPX runtime. They require the `acpx` executable on the spawned agent's `PATH`; install it with `npm install -g acpx`. Provider-specific commands may also be required: `cursor-acp` needs an authenticated Cursor CLI (`cursor-agent`), and `devin-acp` needs the `devin` executable because ACPX runs it as `--agent "devin acp"`. Use `§BRAND_BINARY_NAME§ init --cursor` for Cursor's project-local contract setup; it includes the Claude and Codex project setup Cursor relies on. Set `§BRAND_ENV_PREFIX§_ENABLE_BASH_POLICY=1` before init when selected providers should receive standalone bash-policy hooks. The runtime CLI name remains `cursor-acp`. §BRAND_NAME_TITLE§ preflights these prerequisites before direct `§BRAND_BINARY_NAME§ agent` execution and before TUI/API agent spawning, so a missing binary fails before the ACP session is started. ACPX providers run with catalog-defined target names, session templates, and prompt argv while preserving §BRAND_NAME_TITLE§'s event parsing, usage capture, session reuse, output logging, and approval behavior. During `acpx prompt`, stdout JSON-RPC and stderr diagnostics are streamed to `§BRAND_PROJECT_DIRNAME§/agent-outputs/`; parsed message chunks are returned to the supervisor and lifecycle/usage metadata is logged. Short ACPX session control calls are not transcript-logged.
In the TUI, `s` spawns with the configured default CLI; `S` prompts for CLI selection.

For CLI-backed runtimes, agent output is automatically streamed to `§BRAND_PROJECT_DIRNAME§/agent-outputs/` while the CLI runs (stdout as `.txt`, stderr as `.err`). Pass `--no-log` to disable. Persisted files are automatically masked — secret values from environment variables (API keys, tokens, passwords) are replaced with `***`. Live terminal output remains unmasked. Logging is automatically disabled in `-i` (interactive) mode. Raw provider transcripts and `item.completed` payloads stay in these logs; `state.yaml` stores only bounded summaries and references back to log evidence.
See [Analyzing Agent Logs](#analyzing-agent-logs) for analysis tools.

Multiple agents of the same role can run in parallel (IDs auto-increment):
```bash
§BRAND_BINARY_NAME§ agent coder              # auto-assigns coder-1
§BRAND_BINARY_NAME§ agent coder              # auto-assigns coder-2
§BRAND_BINARY_NAME§ agent coder --agent-id coder-5   # explicit ID
```

**3. Observe and control**

`§BRAND_BINARY_NAME§ tui` shows live system state — agents, tasks, alerts, sprint metrics. Keyboard shortcuts: `s` spawn (default cli), `S` spawn (pick cli), `p` pause, `r` resume, `a` add task, `c` checkpoint, `y` yolo (toggle auto-resume), `Q` stop.

From the CLI:
```bash
# Pause all agents
§BRAND_BINARY_NAME§ pause

# Resume
§BRAND_BINARY_NAME§ resume

# Abort
§BRAND_BINARY_NAME§ stop

# Checkpoint (halt + generate summary)
§BRAND_BINARY_NAME§ sprint-checkpoint

# Activity log
cat §BRAND_PROJECT_DIRNAME§/log.yaml
```

**Signal handling:** Agents cleanly exit on `Ctrl+C` (SIGINT) or `kill` (SIGTERM). On exit, the agent unregisters and atomically releases any active task claim — the task returns to its initial state (doer, e.g. DRAFT_CODE) or submitted state (reviewer, e.g. CODE_TO_REVIEW; legacy configs may use CODE_READY_FOR_REVIEW) — so no orphaned claims are left behind.

**4. Review Results**
```bash
# Integration branch
git log integration --oneline
```

### Running Multiple Sprints

When all tasks in a sprint reach sprint-terminal state, `§BRAND_BINARY_NAME§ resume` marks the sprint COMPLETED. Running `§BRAND_BINARY_NAME§ resume` a second time archives the completed sprint, creates a new IN_PROGRESS sprint, and executes available pipeline transitions — creating child tasks for the next role-pair. Approved transition-source output may be sprint-terminal before it is integrated; if so, merge it first with `§BRAND_BINARY_NAME§ wt-merge <task-id>` before the second resume can advance.

#### Auto-Resume (Yolo Mode)

By default, checkpoints and sprint completions require manual `§BRAND_BINARY_NAME§ resume`. Enable auto-resume to skip these gates and keep the system rolling:

- **At init time:** `§BRAND_BINARY_NAME§ init --auto-resume "Goal"`
- **At runtime:** Press `y` in the TUI to toggle (shows "Auto-resume: ON/OFF" on the status line)

When auto-resume is enabled, agents automatically call `§BRAND_BINARY_NAME§ resume` when they detect CHECKPOINT or COMPLETED sprint status. Use `p` (pause) for a hard stop — pause is never auto-resumed.

To start a completely fresh goal, stop the agents, clean the existing
workspace, and re-initialize:

```bash
§BRAND_BINARY_NAME§ cleanup
§BRAND_BINARY_NAME§ init "<new goal>" --spec <spec_ref>
```

Full init calls the same cleanup flow when existing workspace data is present,
so the separate cleanup command may be omitted when re-initializing immediately.
Cleanup lists and confirms removal of runtime state, task worktrees,
uncommitted worktree files, and associated task branches.

### Sprint Lifecycle & Human Gates

§BRAND_NAME_TITLE§ runs in sprints. Each sprint executes one role-pair (doer + reviewer) from the pipeline.
Human checkpoints gate transitions between pairs (unless auto-resume is enabled).

#### Pipeline & Entry Points

The pipeline defines which role-pairs execute and how tasks flow between them:

```
general-objective entry point (full pipeline):
  simple: epic-planning-pair → us-writing-pair → architecture-main-pair → architecture-pair → code-planning-pair → coding-pair
  fan-out or uncertain: epic-planning-main-pair → epic-planning-pair → us-writing-pair → architecture-main-pair → architecture-pair → code-planning-pair → coding-pair

functional-spec entry point (architecture pipeline):
  simple: architecture-pair → code-planning-pair → coding-pair
  fan-out or uncertain: architecture-main-pair → architecture-pair → code-planning-pair → coding-pair

technical-spec entry point (coding pipeline):
  simple: code-planning-pair → coding-pair
  fan-out or uncertain: code-planning-main-pair → code-planning-pair → coding-pair

detailed-spec entry point:
  legacy alias for functional-spec

integration sub-pipeline (post-coding, orchestrator-triggered):
  slice-integration-pair (when required) → integration-pair → coding-pair (fix tasks)
```

The configured entry points still name the specialized planning pairs. During `INITIAL_PLANNING`, §BRAND_NAME_TITLE§ resolves the mapped `decomposition-root` role-pair and creates exactly one first task: the specialized task for simple work, or the master task when the work would otherwise fan out. The master task's quorum-approved `output[]` entries create the specialized children.

Each transition between pairs is a **human gate** (unless auto-resume is enabled): the sprint completes, the human reviews, then runs `§BRAND_BINARY_NAME§ proceed <task-id> <transition>` followed by `§BRAND_BINARY_NAME§ resume`. With auto-resume, these transitions happen automatically.

The intra-subpipeline master-to-specialized transitions (`epic-decompose`, `arch-decompose`, `code-plan-decompose`) are `trigger: auto` and run after the master task reaches its quorum-approved state. `architecture-to-code-plan` is still Case A: specialized `architecture-pair` output goes directly to `code-planning-pair` children and does not create a `code-planning-main-pair` task. Specialized epic outputs still use `epic_ref` for `us-writing-pair`; the epic master framework uses `plan_ref`.

Use `§BRAND_BINARY_NAME§ init --no-follow-up` to suppress top-level `pipeline-transitions`. The selected entry-point sub-pipeline still runs normally, but §BRAND_NAME_TITLE§ will not show, auto-execute, or allow manual `§BRAND_BINARY_NAME§ proceed` for cross-sub-pipeline follow-up transitions.

The `integration-to-fix` transition is an exception — it uses `trigger: auto`, meaning fix tasks are created automatically when the integration reviewer approves findings, without a human gate.

#### Sliced Integration

Integration coverage opens only after planning reaches the settled barrier:
every pre-integration planning source is terminal, every eligible coding output
and transition has been consumed, and the resulting coding work is terminal.
The contributing scope set is then frozen once. If fewer than two contributing
scopes exist, the workflow persists no coverage records, creates zero slice
analyses, and proceeds directly through an existing valid global
`integration-pair`.

Only with multiple contributing scopes does every contributing scope supply
bounded coverage. A one-lineage scope reuses its coding-review approval
attestation; the attestation records the reviewed task and acceptance criteria,
reviewed and merge commits, approver, and validation, without persisted reviewer
reasoning. A qualifying multi-lineage scope—one with at least two distinct coding
lineages that produced merged work—receives exactly one slice analysis. This
coverage map is navigation evidence, not proof of aggregate correctness.

Global analysis begins only after all required coverage and slice repairs are
resolved, then performs an independent global review of the current aggregate
branch. Global fixes or later integration-HEAD mutations cause the next global
generation to rescan the branch independently while budget remains. A blocked
slice blocks global integration; exhausting the configured global generation
limit returns `global_generations_exhausted` instead of successful completion.

Validation remains stack-agnostic: analysts use the affected paths and declared
project validation rather than an assumed language, framework, or build tool.
Review approval boundaries are unchanged: the integration reviewer still
approves analysis findings or clean results, and the supervisor retains merge
authority.

Frozen sliced-integration capability fails closed only when the frozen cohort
requires slice topology. If such a qualifying cohort's frozen pipeline lacks
the required topology, it returns `pipeline_upgrade_required`; the operator
must create a fresh workspace or perform a manual topology update instead of
silently skipping required slices. A zero- or one-scope cohort needs neither
coverage records nor sliced-integration capability, so incomplete unused slice
topology does not block its direct-global bypass when the existing global
`integration-pair` is valid.

#### Sprint Phases

```
┌─────────────────────────────────────────────────────────┐
│                                                         │
│  1. Orchestrator creates task for current pair          │
│  2. Doer claims task, does work, populates output[]     │
│  3. Reviewer approves → task merges                     │
│  4. All tasks done → SPRINT_COMPLETE                    │
│  5. Sprint checkpoints → HUMAN GATE (or auto-resumed)   │
│                                                         │
│  Human reviews results, then (manual mode):             │
│    §BRAND_BINARY_NAME§ resume                       (complete sprint)  │
│    §BRAND_BINARY_NAME§ resume                     (start next sprint)  │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

Transitions create child tasks from the parent's `output[]` entries (per-subtask cardinality), from the parent task itself (one-to-one cardinality), or from multiple parent tasks in a cohort (many-to-one cardinality). Available transitions are defined in `§BRAND_PROJECT_DIRNAME§/pipeline.yaml`.

#### What Humans Do at Checkpoints

When a sprint checkpoints (status: CHECKPOINT), behavior depends on the trigger. Hard checkpoints
pause all agents. Transition checkpoints (`PLANNING_COMPLETE`, `MANY_TO_ONE_READY`) pause
orchestrator transition execution only; doer/reviewer agents may continue already-available
claimable/reviewable work in the current sprint. The human reviews what agents produced and decides
what to do next.

**Start with a summary.** Run `/checkpoint-summary` in any pairing agent session to get
a prioritized digest of agent decisions, open points, and risks — what needs your input,
what needs confirmation, and what's just informational. This is faster than reading every
artifact yourself and surfaces unflagged decisions agents baked in without marking.

| Action | Command | When                                                                                                 |
|--------|---------|------------------------------------------------------------------------------------------------------|
| Accept & resume | `§BRAND_BINARY_NAME§ resume` | Satisfied with planner output or fan-in readiness, continue the sprint, start next sprint            |
| Amend & replan | Edit plan file, commit, then `§BRAND_BINARY_NAME§ replan` | Want to change a planner's output before proceeding                                                  |
| Pipeline transition | `§BRAND_BINARY_NAME§ proceed <task-id> <transition>` | Create child tasks for the next role-pair from output or a ready cohort. Automatically done in batch by `§BRAND_BINARY_NAME§ resume` |
| Pause for manual work | (no command) | Want to make manual changes before continuing                                                        |
| Abort | `§BRAND_BINARY_NAME§ stop` | Want to stop entirely                                                                                |

**`§BRAND_BINARY_NAME§ proceed`** creates child tasks from a completed task's `output[]` entries based on the pipeline transition's cardinality (`per-subtask`: one child per output entry, `one-to-one`: single child from parent, `many-to-one`: all sibling tasks in a cohort must reach approved status, then one child is created linked to all parents — used by the `us-to-coding` transition to fan N approved user stories into one architecture task). Per-subtask children copy `output[].validation` and `output[].destructive_db`; one-to-one and many-to-one children do not inherit parent task validation metadata. Use `§BRAND_BINARY_NAME§ status` to see available transitions for tasks at terminal states. Transition checkpoints run this in batch during `§BRAND_BINARY_NAME§ resume`; manual use is for explicit one-off transition execution.

For `per-subtask` output, `depends_on` names sibling output indexes (`"0"` means `output[0]`). Use `task_depends_on` when the generated child must depend on existing concrete task IDs outside the current `output[]`. Concrete dependencies must follow pipeline direction: a generated child cannot depend on a task whose role-pair is downstream from the child's role-pair, including through `superseded_by` resolution paths.

If task or `output[]` validation may reset/drop DB state, set `destructive_db: true`. Validation must be non-empty, and every command must start with `§BRAND_ENV_PREFIX§_ALLOW_DESTRUCTIVE_DB=1 ` or `env §BRAND_ENV_PREFIX§_ALLOW_DESTRUCTIVE_DB=1 `. The marker is part of the canonical command and should only target disposable DB state.

Master planning output entries also carry `decomposition` metadata. `read_only_depends_on` and `read_only_task_depends_on` describe read-only use only; scheduling still comes from mirrored `depends_on` and `task_depends_on` entries. Master outputs must include the inherited framework ref configured by the role-pair's `decomposition-output-ref`: `plan_ref` for `epic-planning-main-pair`, `arch_ref` for `architecture-main-pair`, and `plan_ref` for `code-planning-main-pair`.

#### Replanning at Checkpoint

When a transition checkpoint fires, the human reviews the proposed downstream work before child tasks are created. `PLANNING_COMPLETE` means planner `output[]` entries represent the proposed task breakdown; `MANY_TO_ONE_READY` means a fan-in cohort is ready to create its consolidated child task. The human may:

1. **Accept the transition** — run `§BRAND_BINARY_NAME§ resume` to continue
2. **Amend the plan** — edit the plan markdown file, commit, then run `§BRAND_BINARY_NAME§ replan`

`§BRAND_BINARY_NAME§ replan` invalidates the old planning task's output and creates a new planning task with the same role-pair and spec. The sprint returns to IN_PROGRESS and the planner agent picks up the new task, re-reads the amended plan, and regenerates `output[]`.

```bash
# Typical replan workflow
vim specs/plan.md                      # edit the plan
git add specs/plan.md && git commit -m "amend plan"
§BRAND_BINARY_NAME§ replan                            # auto-detects the planning task
# or, if multiple planning tasks exist:
§BRAND_BINARY_NAME§ replan code-planning-1            # specify task ID explicitly
```

The old task's output is preserved for audit (not cleared), just marked as superseded. Multiple replans increment the counter: `code-planning-1-replan-1`, `code-planning-1-replan-2`, etc.

#### Sprint Status Flow

```
IN_PROGRESS → CHECKPOINT ──→ COMPLETED ──→ (new sprint) IN_PROGRESS
                  │  ↑            ↑              ↑
                  │  │            │              └── §BRAND_BINARY_NAME§ resume (2nd: archive & advance)
                  │  │            └── §BRAND_BINARY_NAME§ resume (1st: all tasks terminal → mark COMPLETED)
                  │  └── orchestrator calls §BRAND_NAME_LOWER§_sprint_checkpoint
                  │
                  ├── §BRAND_BINARY_NAME§ resume  (mid-sprint: not all terminal → back to IN_PROGRESS)
                  └── §BRAND_BINARY_NAME§ replan  (amend plan → new planning task → back to IN_PROGRESS)
```

**`§BRAND_BINARY_NAME§ resume` has two behaviors depending on sprint state:**
- **At CHECKPOINT** (not all tasks terminal): resumes the current sprint as IN_PROGRESS
- **At CHECKPOINT** (all tasks terminal): marks sprint COMPLETED. Run `§BRAND_BINARY_NAME§ resume` a second time to archive the sprint, create a new one, and execute available pipeline transitions. Approved transition-source output must be merged before this second resume can advance

### CLI Commands

The `§BRAND_BINARY_NAME§` binary provides all system operations. Key commands:

Use `§BRAND_BINARY_NAME§ -C <project-root> ...` to select a §BRAND_NAME_TITLE§ project root when invoking state commands from a task worktree or any directory inside it. `-C` does not change the process working directory for unrelated relative file arguments; pass absolute paths or run from the intended directory for those.

| Command | Purpose                                                                                                              |
|---------|----------------------------------------------------------------------------------------------------------------------|
| **Setup & Init** |                                                                                                                      |
| `§BRAND_BINARY_NAME§ setup [--yes]` | One-time global setup of contracts, skills and support docs to `~/§BRAND_GLOBAL_DIRNAME§/`; `--yes` auto-confirms approval prompts |
| `§BRAND_BINARY_NAME§ init <goal> [--spec <spec_ref>] [--branch <name>] [--yes]` | Initialize `§BRAND_PROJECT_DIRNAME§/` directory with blackboard; `--yes` auto-confirms init approval prompts |
| `§BRAND_BINARY_NAME§ cleanup [--yes]` | Remove runtime state, owned task worktrees, and associated task branches after checking for live agents and confirming exact targets |
| **Agents & Monitoring** |                                                                                                                      |
| `§BRAND_BINARY_NAME§ agent <role> [--agent-id <id>]` | Agent supervisor (start, restart, backoff loop; ID auto-assigned if omitted)                                         |
| `§BRAND_BINARY_NAME§ launch wezterm mas --preset <name>` | Launch `§BRAND_BINARY_NAME§ tui` plus a MAS role preset in one WezTerm window                                                     |
| `§BRAND_BINARY_NAME§ repair-agent-pool [--cli <name>] [--dry-run]` | Spawn one agent for each claimable-work role that has no live usable agent                                         |
| `§BRAND_BINARY_NAME§ tui` | Live TUI: spawn agents, monitor state, manage system                                                                 |
| `§BRAND_BINARY_NAME§ status` | Show system and task status at a glance                                                                              |
| `§BRAND_BINARY_NAME§ mark-agent-degraded <agent-id>` / `§BRAND_BINARY_NAME§ clear-agent-degraded <agent-id>` | Record or clear role-capacity health for an agent epoch                                                            |
| **System Control** |                                                                                                                      |
| `§BRAND_BINARY_NAME§ pause` / `§BRAND_BINARY_NAME§ resume` | Pause/resume system (resume also advances CHECKPOINT → COMPLETED → new sprint)                                       |
| `§BRAND_BINARY_NAME§ replan [task-id]` | Amend a planner's output at CHECKPOINT (invalidate old task, create new planning task)                               |
| `§BRAND_BINARY_NAME§ stop` / `§BRAND_BINARY_NAME§ start` | Stop/start system                                                                                                    |
| `§BRAND_BINARY_NAME§ sprint-checkpoint` | Create a checkpoint (halt + summary)                                                                                 |
| **Task Operations** |                                                                                                                      |
| `§BRAND_BINARY_NAME§ add-task` | Add a new task to the state. The new task is scoped-validated before persistence; if unrelated existing state corruption keeps full validation degraded, the command succeeds with a warning. |
| `§BRAND_BINARY_NAME§ add-tasks --tasks-file <path>` | Add multiple tasks independently from JSON. Valid items can persist even while unrelated state remains degraded; each successful item carries a warning when the post-add full validation still fails. |
| `§BRAND_BINARY_NAME§ claim-task <task-id> <agent-id>` | Atomically claim a task for a doer agent (creates worktree, updates state)                                           |
| `§BRAND_BINARY_NAME§ submit-for-review <task-id> [commit-ref]` | Submit a task for review (doer agents; defaults to worktree `HEAD`)                                                  |
| `§BRAND_BINARY_NAME§ submit-verdict <task-id> <APPROVED\|REJECTED> [--reason "<reason>"]` | Submit a review verdict (reviewer agents; `--reason` required for REJECTED)                                          |
| `§BRAND_BINARY_NAME§ mark-blocked <task-id>` | Mark a task as BLOCKED with reason/questions; optional `--depends-on` records blocking task IDs for scheduling and orchestrator re-wake; optional `--repair-*` flags request orchestrator-only state repair |
| `§BRAND_BINARY_NAME§ assess-blocked <task-id>` | Record orchestrator assessment of a BLOCKED task and raise an `UNRESOLVED BLOCKED` alert when it cannot be resolved now |
| `§BRAND_BINARY_NAME§ retarget-dependency <task-id> <old-dep-id> <new-dep-ids> --reason "..."` | Orchestrator-only repair for one non-terminal task's direct `depends_on` edge. Replaces the old edge with one or more existing task IDs, canonicalizes dependencies, validates the full candidate state, and leaves task status unchanged. |
| `§BRAND_BINARY_NAME§ unblock-task <task-id> --reason "..." [--assign-to <agent-id>] [--rebase-on <branch>] [--allow-dirty]` | Restore a repaired BLOCKED task. Without `--assign-to`, the task becomes claimable again; with `--assign-to`, it directly resumes for that doer. `--rebase-on` rebases a preserved task worktree before unblocking, updates `base_commit`, and leaves conflicts BLOCKED with repair metadata. Fails while any `depends_on` target is not directly MERGED. |
| `§BRAND_BINARY_NAME§ assess-hypothesis-exhausted <task-id>` | Record orchestrator assessment of a hypothesis-exhausted task (2+ coders failed)                                     |
| `§BRAND_BINARY_NAME§ cancel-task <task-id> "reason"` | Cancel a non-approved, non-terminal task, including active/submitted/reviewing work, by transitioning it to ABANDONED with audit trail. Releases §BRAND_NAME_TITLE§ state claims and removes the task worktree/branch best-effort; it does not kill a live provider process. |
| `§BRAND_BINARY_NAME§ handoff <task-id> <summary> <next-action>` | Context-exhaustion handoff for a doer agent's claimed task                                                           |
| `§BRAND_BINARY_NAME§ supersede-task <task-id> [replacements] --reason "..."` | Mark a task as SUPERSEDED. When no replacements are provided, also requires `--recoverability-command "<single-line command>"`; §BRAND_NAME_TITLE§ records this audit command and a pre-cleanup branch/worktree snapshot, but does not execute it. Do not include secrets. |
| `§BRAND_BINARY_NAME§ proceed <task-id> <transition>` | Execute inter-pair pipeline transition (e.g., code-plan-to-coding)                                                   |
| **Worktree Management** |                                                                                                                      |
| `§BRAND_BINARY_NAME§ wt-create <task-id>` | Create a worktree for an executing task                                                                              |
| `§BRAND_BINARY_NAME§ wt-merge <task-id>` | Merge an approved task into the integration branch (reviewer agents)                                                 |
| `§BRAND_BINARY_NAME§ wt-delete <task-id>` | Delete a worktree for a completed/abandoned task                                                                     |
| **Recovery** |                                                                                                                      |
| `§BRAND_BINARY_NAME§ recover-task <task-id>` | Recover by task ID while preserving coherent worktree/branch state by default; use `--fresh` to discard intentionally |
| `§BRAND_BINARY_NAME§ recover-agent <agent-id>` | Recover by agent ID (release claim + remove worktree + delete agent)                                                 |
| `§BRAND_BINARY_NAME§ release-claim <task-id> [--role R]` | Release claim on a task (manual, granular recovery)                                                                  |
| `§BRAND_BINARY_NAME§ delete agent <id>` / `§BRAND_BINARY_NAME§ delete task <id>` | Delete an agent or task from state                                                                                   |
| **Analysis** |                                                                                                                      |
| `§BRAND_BINARY_NAME§ validate` | Validate blackboard state against schema invariants                                                                  |
| `§BRAND_BINARY_NAME§ analyze` | Run circuit breaker pattern detection                                                                                |
| `§BRAND_BINARY_NAME§ update-sprint-metrics` | Recompute sprint metrics from current state                                                                          |
| `§BRAND_BINARY_NAME§ clear-stale-review-claims` | Clear expired review leases                                                                                          |
| `§BRAND_BINARY_NAME§ get <query>` | Query state data (tasks, agents, etc.)                                                                               |

When passing a shell variable to `--reason`, always quote it:
`--reason "$reason"`. If an unquoted empty variable causes `--reason` to
consume the next token, §BRAND_NAME_TITLE§ rejects the value when that token is
a flag registered on the command. This guard intentionally does not reject
unknown flag-shaped prose, and a literal one-token reason matching a registered
flag or shorthand (for example `-C`) must be rewritten as descriptive prose.

**Important:** The supervisor claims tasks *before* starting the agent CLI. This avoids interactive permission prompts in non-interactive mode. Agents receive their assigned task in the bootstrap prompt and should NOT call claim commands directly.

See [Architecture Overview](../specs/architecture/overview.md) for detailed component descriptions.

### Configuring Claude Code

`§BRAND_BINARY_NAME§ init` creates the Claude Code configuration automatically:

**`.claude/settings.json`** — Permissions for Claude Code agents (§BRAND_NAME_TITLE§ CLI permissions shown, hooks, MCP servers).

The full template also pre-approves skills (code-review, testing, debugging, etc.), git read/write commands, build tools, shell utilities, and web access (WebFetch, WebSearch, LSP). See `internal/embedded/claude-settings.json` for the complete list.

> ️⚠️ To use Claude Code with your Claude subscription, make sure the ANTHROPIC_API_KEY environment variable is not set by default on a new shell start ([Claude support](https://support.claude.com/en/articles/12304248-managing-api-key-environment-variables-in-claude-code), not specific to §BRAND_NAME_TITLE§).

**Claude environment overrides:** Create a `claude.env` file at your project root
to inject environment variables into Claude CLI agent processes. The supervisor
reads this file automatically if it exists. Format: `KEY=VALUE`, one per line
(comments with `#`). See https://code.claude.com/docs/en/env-vars.

```bash
# claude.env — example
# Mitigate recent token usage spike, https://x.com/kunchenguid/status/2043511416448307378
CLAUDE_CODE_EFFORT_LEVEL=high
CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING=1
CLAUDE_CODE_DISABLE_AUTO_MEMORY=1
CLAUDE_CODE_DISABLE_1M_CONTEXT=0
CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=30
CLAUDE_CODE_SUBAGENT_MODEL=sonnet
§BRAND_ENV_PREFIX§_DISABLE_CLAUDE_SUBAGENTS=1
```

Rationale:
- High is probably the optimal reasoning effort - thoughtfulness without excessive token consumption.
- The new adaptive thinking contributed to the degradation of Opus performance since March 2026.
- Auto-memory is investment without return because Claude Code never considers it in practice.
- 1M context is too much - Claude's performance degrades much before hitting a fraction of it: either disable it or set an aggressive autocompact threshold.
- Use Sonnet for subagents in pairing.
- Subagents are useful in general to save context on the master session, but §BRAND_NAME_TITLE§'s contract is heavy, cannot be disabled for subagents, and §BRAND_NAME_TITLE§'s sessions are task bounded.

> **⚠️ Dev ecosystem tools must be allowed.** Agents run non-interactively — they cannot
> answer permission prompts. Any tool not listed in `permissions.allow` will silently fail
> or stall the agent. The default template pre-configures common ecosystems:
>
> | Ecosystem | Pre-configured tools |
> |-----------|---------------------|
> | Python | `uv`, `ruff`, `pytest`, `mypy`, `pip`, `pre-commit` |
> | Go | `go`, `make` |
> | Rust | `cargo`, `rustfmt`, `clippy-driver` |
> | Node.js | `node`, `npm`, `npx`, `yarn`, `pnpm`, `bun`, `eslint`, `prettier`, `tsc` |
>
> If your project uses tools not in this list, add them to `.claude/settings.json` before
> spawning agents. Run `/§BRAND_BINARY_NAME§-logs` after your first sprint to catch any remaining
> permission denials. Run `/context-engineering` when logs point to prompt bloat,
> missing context, or poor handoffs.

CLI commands (e.g., `§BRAND_BINARY_NAME§ add-task`) operate on `§BRAND_PROJECT_DIRNAME§/state.yaml` with proper locking. Agents use CLI commands via Bash with `--json` for structured output.

The settings template is embedded into the binary. `§BRAND_BINARY_NAME§ init` writes the active copy to `.claude/settings.json` in the project directory.

### Analyzing Agent Logs

Agent logs are your primary diagnostic tool for understanding what agents actually did and where they got stuck. **Use `/§BRAND_BINARY_NAME§-logs` early and often** — it cross-correlates logs across agents, surfaces patterns that slow down the execution and increase token usage, and proposes actionable fixes. Use `/context-engineering` when the likely cause is prompt payload shape, context bloat, missing or duplicated context, cacheability, or weak handoff fit.

#### Identifying Frictions

Log analysis serves different purposes depending on where you are in your §BRAND_NAME_TITLE§ journey:

**New users — misconfiguration detection.** Most early failures come from setup issues, not agent logic. Common culprits: incomplete `AGENT_TOOLS.md` (missing tool permissions, wrong MCP server names), missing `GUARDRAILS.md` constraints, incorrect `--post-worktree-cmd`, or stale `~/§BRAND_GLOBAL_DIRNAME§/` files after an upgrade. Run `/§BRAND_BINARY_NAME§-logs` after your first sprint — it will flag permission denials, tool failures, and initialization errors that point straight to the misconfiguration.

**Seasoned users — regression and drift detection.** Once your setup is stable, log analysis shifts to catching new frictions: provider CLI updates that change output formats or break flags, context budget regressions from prompt growth, new tool failure patterns, or behavioral drift after contract changes. Run `/§BRAND_BINARY_NAME§-logs` when a previously-smooth pipeline starts producing unexpected checkpoints, rejections, or BLOCKED tasks.

In both cases, the pattern is the same: run the analysis, read the friction report, fix the root cause, re-run. Logs are cheap; debugging blind is expensive. When `/§BRAND_BINARY_NAME§-logs` shows token pressure, repeated broad searches, or handoff/rejection patterns, follow with `/context-engineering` to inspect the paired `§BRAND_PROJECT_DIRNAME§/agent-prompts/` and `§BRAND_PROJECT_DIRNAME§/agent-outputs/` evidence.

#### Log Format

Logs are captured by default (disable with `--no-log`) as incrementally written NDJSON files (one JSON object per line) from `claude --verbose --output-format stream-json`. Two formats exist depending on the agent role:

| Format | First event | Seen in | Token detail |
|--------|-------------|---------|--------------|
| **Rich** | `type: system` | Orchestrator | Per-API-call breakdown (input, cache, output) |
| **Sparse** | `type: thread.started` | All doer and reviewer roles | Aggregate only (`turn.completed`) |

Both analysis tools auto-detect the format.

**LLM-assisted analysis** — use a coding agent to cross-correlate logs, diagnose patterns and propose fixes:

```
/§BRAND_BINARY_NAME§-logs
```

This works with any coding agent (Claude Code, Codex, etc.) in pairing mode. The agent runs the analyzer, reads the reports, correlates errors across agents, and suggests actionable fixes.

For prompt/context-specific diagnosis, run:

```
/context-engineering
```

That skill pairs prompts and outputs by role and timestamp, then audits context quality, cacheability, load-on-demand opportunities, tool-output pressure, and cross-agent handoff fit. Its corpus indexer supports both Claude rich stream-json logs and Codex sparse `item.completed` logs.

**CLI analyzer** (`~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_BINARY_NAME§-logs/scripts/analyze-log.py`) — stdlib-only Python 3.12+, for batch/CI use:

```bash
# Single file
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_BINARY_NAME§-logs/scripts/analyze-log.py §BRAND_PROJECT_DIRNAME§/agent-outputs/orchestrator-1-*.txt

# Multiple files
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_BINARY_NAME§-logs/scripts/analyze-log.py §BRAND_PROJECT_DIRNAME§/agent-outputs/*.txt
```

Report sections: session header, token summary (fresh/cached/output, cache hit rate), content breakdown by type (chars, estimated tokens, share %), top 10 items by size, tool call frequency. Rich format adds per-turn context growth and cost breakdown.

**Browser analyzer** (`§BRAND_BINARY_NAME§-session-analyzer.html`) — drag-and-drop, visual charts:

```bash
open ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_BINARY_NAME§-logs/tools/§BRAND_BINARY_NAME§-session-analyzer.html   # or xdg-open on Linux
```

Drop one or more log files. Produces the same analysis with bar charts for content breakdown and context growth.

**Raw inspection** with `jq` (no dependencies):

```bash
# Sparse format: extract items
jq -c 'select(.item) | .item | {type, text, command, tool, usage}
  | with_entries(select(.value != null))' §BRAND_PROJECT_DIRNAME§/agent-outputs/coder-1-*.txt

# Rich format: extract token usage per API call
jq -c 'select(.type == "assistant") | {id: .message.id, usage: .message.usage}' \
  §BRAND_PROJECT_DIRNAME§/agent-outputs/orchestrator-1-*.txt
```

**Interactive diagnosis** — open a regular coding agent session (`claude`, `codex`, etc.) in the project directory. It can read `§BRAND_PROJECT_DIRNAME§/state.yaml`, agent logs, and prompts — everything needed to diagnose issues interactively. The `/§BRAND_BINARY_NAME§-logs` and `/context-engineering` skills work this way.

### Submit, Await Verdict, Handle Result

Doer agents (coders, planners, writers) use a blocking workflow for review cycles:

1. `§BRAND_BINARY_NAME§ submit-for-review <task-id> --agent-id <agent-id> --json` — submit completed work
2. `§BRAND_BINARY_NAME§ await-verdict <task-id> --agent-id <agent-id> --json` — wait for the reviewer within the remaining overall budget; one foreground call lasts at most 100 seconds
3. Handle verdict:
  - **POLL**: Call `await-verdict` again in the foreground with the smaller returned `timeout_seconds`
  - **REJECTED**: Fix issues, resubmit (session stays alive — no cold restart)
  - **TIMEOUT**: The overall budget is exhausted; stop waiting and exit normally
  - **APPROVED** / **NEW_ATTEMPT** / **ABORTED**: Exit normally

`--timeout-seconds` is the remaining overall budget, not the duration of one blocking call. This reduces per-rejection overhead from ~47s (cold restart) to near-zero. The call is also iteration-budget-aware — it refuses if the iteration limit would be exceeded on rejection.

### Review, Reject, Await Resubmission

Reviewer agents use a blocking workflow after non-terminal rejections:

1. `§BRAND_BINARY_NAME§ submit-verdict <task-id> REJECTED --agent-id <agent-id> --reason <feedback> --json` — issue the rejection
2. `§BRAND_BINARY_NAME§ await-resubmission <task-id> --agent-id <agent-id> --json` — wait for the doer within the remaining overall budget; one foreground call lasts at most 100 seconds
3. Handle result:
  - **POLL**: Call `await-resubmission` again in the foreground with the smaller returned `timeout_seconds`
  - **RESUBMITTED**: Review the new changes (session stays alive — no cold restart)
  - **TIMEOUT**: The overall budget is exhausted; stop waiting and exit normally
  - **TERMINAL** / **ABORTED**: Exit normally

`--timeout-seconds` has the same remaining-budget meaning as it does for `await-verdict`. This reduces per-rejection overhead from ~47s (cold restart) to near-zero for reviewers. Do not call after terminal rejections (`EscalatedToBlocked` or `NewAttemptTriggered`).

### Differences from Pairing Mode

| Aspect | Pairing Mode | Multi-Agent Mode |
|--------|--------------|------------------|
| Approval | Human approves | Peer agent approves |
| Gates | Approval request → wait | Pre-execution checkpoint → proceed |
| Communication | Conversation | Blackboard |
| Iteration | Human feedback | Reviewer agent feedback |
| Debugging | Debugging skill | Log anomaly, BLOCKED |
| Magic Phrases | Active | Not applicable |
| Session Init | Greet user | Silent execution |

### Supervisor Circuit Breaker

The supervisor automatically handles these conditions (transparent to agents):

| Condition | Action |
|-----------|--------|
| Agent crash loop (3× in 5min) | Supervisor stops the agent |
| Blackboard validation fails | All agents pause |
| Submit/merge integration branch conflict | Task set to INTEGRATION_FAILED |
| Unblock-time `--rebase-on` conflict | Task remains BLOCKED with fresh repair metadata |
| Circuit-breaker pattern detected in anomalies | Set mode to `CIRCUIT_BREAKER_TRIPPED`, create sprint `CHECKPOINT`, write reports |
