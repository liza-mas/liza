# Configuration Reference

System configuration, tuning parameters, and environment variables.

## Claude Code Settings

**`.claude/settings.json`** — project-level permissions for Liza CLI commands, skills, git operations, and build commands.

`liza init` writes this file automatically from the embedded [`claude-settings.json`](../internal/embedded/claude-settings.json). The master defines all Liza CLI permissions, skills, and the full set of bash permissions agents need. **Do not hand-craft a subset** — agents will be blocked on any missing permission.

**Key elements:**
- **`enableAllProjectMcpServers`** — enables any project MCP servers (for non-Liza tools like JetBrains, filesystem, etc.)
- **`Bash(liza:*)`** — grants permission for agents to invoke Liza CLI commands
- **`Skill(...)`** — contract skills from `~/.liza/skills/` (installed by `liza setup`)
- **`defaultMode: acceptEdits`** — required for headless agent operation

### Two-Layer Architecture

Claude Code unions permissions from global and project settings:

| Layer | File | Managed by | Contains |
|-------|------|-----------|----------|
| **Project** | `<project>/.claude/settings.json` | `liza init` (automatic) | Liza CLI permissions, skills, git/build commands |
| **Global** | `~/.claude/settings.json` | Manual (one-time) | Personal MCP tools (IDE, search, etc.), `additionalDirectories`, `Read(~/.liza/**)` |

The project layer is portable (team-shared). The global layer is machine-specific (personal tools and paths). Neither alone is sufficient — both are needed.

For global settings setup and provider-specific config (Claude, Codex, Gemini), see [Contract Activation](../contracts/contract-activation.md).

## Codex Project Permissions

**`~/.codex/config.toml`** — global Codex CLI settings.

`liza init --codex` manages the Codex permissions Liza needs for unattended
supervisor tasks without breaking pairing mode. It adds or corrects the global
`workspace` permission baseline and adds the active project root plus the active
project `.git` directory to `sandbox_workspace_write.writable_roots` so
interactive Codex can edit project files. It also marks Codex/Liza support
directories and user cache roots readable or writable in
`permissions.workspace.filesystem`: `~/.liza` is readable for contracts and
skills, while Codex state, Go cache, and npm cache roots are writable. If the
file already exists, Liza prompts before merging those entries and preserves
unrelated settings.

When launching headless MAS agents, Liza passes launch-time `workspace`
permission overrides and explicit `--add-dir` entries for both the task worktree
and the project `.git` directory. This is required because Git worktrees write
the task index under the main repo metadata path
(`.git/worktrees/<task>/index.lock`), not under the worktree directory itself.

Codex versions 0.126.0-alpha.17 through 0.132.0 keep linked-worktree metadata
read-only under `workspace-write`. Until upstream fixes this, pin MAS Codex
agents to the last tested working compatibility path with these durable project
config keys:

```yaml
config:
  codex_package_version: "0.125.0"
  codex_legacy_landlock: true
```

For a temporary process-local fallback when those config fields are unset, set
these before running `liza agent`:

```bash
export LIZA_CODEX_VERSION=0.125.0
export LIZA_CODEX_LEGACY_LANDLOCK=1
```

`codex_package_version` or `LIZA_CODEX_VERSION` makes Liza launch headless
Codex agents through
`npm exec --yes --package @openai/codex@<version> -- codex`.
`codex_legacy_landlock: true` or `LIZA_CODEX_LEGACY_LANDLOCK=1` adds
`--enable use_legacy_landlock --sandbox workspace-write`. The state config
version takes precedence over the environment fallback. Interactive
`liza agent -i` keeps using the installed Codex binary.

This is not the full Codex baseline. Users still own broader settings such as
interactive `approval_policy` and MCP server configuration. See
[Contract Activation](../contracts/contract-activation.md#codex) for the
recommended complete setup.

### Troubleshooting

**State file errors:**
- Verify project initialized: `liza validate`
- Check: `ls -la .liza/state.yaml`

**Codex `.git` read-only in linked worktrees:**
- Use `config.codex_package_version: "0.125.0"` with
  `config.codex_legacy_landlock: true` for MAS agents that must stage or commit
  from linked worktrees.
- If you need a temporary process-local test before changing durable project
  config, use
  `LIZA_CODEX_VERSION=0.125.0` with `LIZA_CODEX_LEGACY_LANDLOCK=1`.
- Treat this as a temporary local workaround. `use_legacy_landlock` is a
  deprecated Codex compatibility feature.

## Configuration Matrix

All configuration lives in `.liza/state.yaml` under the `config` section.

| Parameter | Default | Min | Max | Unit | Purpose |
|-----------|---------|-----|-----|------|---------|
| `max_coder_iterations` | 10 | 1 | 100 | count | Max iterations per coder per task |
| `max_review_cycles` | 5 | 1 | 20 | count | Max review rejection cycles |
| `heartbeat_interval` | 60 | 1 | 300 | seconds | Heartbeat frequency |
| `lease_duration` | 1800 | 300 | 7200 | seconds | Task lease duration |
| `coder_poll_interval` | 30 | 5 | 120 | seconds | Check interval (legacy, now event-driven) |
| `doer_max_wait` | 18000 | 300 | — | seconds | Max idle before doer-role supervisors exit |
| `orchestrator_poll_interval` | 60 | — | — | seconds | Orchestrator polling interval |
| `orchestrator_max_wait` | 18000 | — | — | seconds | Max orchestrator idle before exit |
| `reviewer_poll_interval` | 30 | — | — | seconds | Reviewer polling interval |
| `reviewer_max_wait` | 18000 | — | — | seconds | Max reviewer idle before exit |
| `agent_progress_timeout` | 1800 | — | — | seconds | Max active execution time without state, worktree, or provider-output progress before blocking the task |
| `default_cli` | (none) | — | — | CLI name | Global default coding agent CLI |
| `default_doer_cli` | (none) | — | — | CLI name | Default coding agent CLI for doers and orchestrators |
| `default_reviewer_cli` | (none) | — | — | CLI name | Default coding agent CLI for reviewers |
| `codex_package_version` | (none) | — | — | npm package version | Pins headless Codex agents to `@openai/codex@<version>` |
| `codex_legacy_landlock` | false | — | — | boolean | Adds Codex `use_legacy_landlock` and `workspace-write` flags for headless MAS agents |
| `post_worktree_cmd` | (none) | — | — | shell cmd | Command run after worktree creation (e.g. `npm install`) |
| `auto_checkpoint_summary` | true | — | — | boolean | Auto-runs checkpoint-summary after successful merges and writes `.liza/checkpoint-summary.md` |
| `scip_search` | (none) | — | — | language list | Durable allowlist of SCIP languages Liza may index when `LIZA_ENABLE_SCIP_SEARCH` is truthy |

### SCIP Search (`config.scip_search`)

`scip-search` is an optional external repository-navigation tool for MAS
worktrees. It is strict opt-in: Liza generates SCIP indexes and injects
`scip-search` prompt guidance only when `LIZA_ENABLE_SCIP_SEARCH` is truthy and
`config.scip_search` contains at least one supported language.

`LIZA_ENABLE_SCIP_SEARCH` is process-local activation, not durable project
state. Values are trimmed and compared case-insensitively:

| Value | Meaning |
|-------|---------|
| `1`, `true` | Enable MAS SCIP indexing and prompt guidance when `config.scip_search` allows a detected language |
| unset, empty, `0`, `false` | Disable MAS SCIP indexing and prompt guidance, even when `config.scip_search` exists |

`config.scip_search` is the durable language allowlist written under
`.liza/state.yaml` by `liza init`. It does not activate MAS indexing by itself;
it only limits which detected languages Liza may index after the environment
gate is truthy.

Use repeated `--scip-search <language>` options during init to set an explicit
allowlist:

```bash
LIZA_ENABLE_SCIP_SEARCH=1 liza init --spec goal.md --scip-search go --scip-search typescript --scip-search python
```

Supported `--scip-search <language>` values are exactly `go`, `typescript`, and
`python`. When no explicit `--scip-search` value is supplied and
`LIZA_ENABLE_SCIP_SEARCH` is truthy, Liza auto-detects supported languages from
git-tracked code and writes the detected allowlist to `config.scip_search`.

`scip-search` and the language indexers are separate external prerequisites.
Installing `scip-search` does not install the indexers:

| Language | Indexer |
|----------|---------|
| `go` | `scip-go` |
| `typescript` | `scip-typescript` |
| `python` | `scip-python` |

Generated task indexes live under the task worktree:

```text
<worktree>/.liza/scip/
```

Project-root orchestrator indexes live under:

```text
<project_root>/.liza/scip/
```

Indexes are snapshots generated at controlled lifecycle points. They reflect the
source tree when Liza created or refreshed them, not later edits made by an
agent during the same task.

Indexing failures degrade gracefully at runtime. If one enabled language fails
to index, Liza still spawns the agent and omits that failed language from the
`scip-search` prompt guidance. If no index is available, Liza omits the
`scip-search` prompt section entirely.

Explicit non-goals: Liza does not build, vendor, auto-install, daemonize, watch,
cache, or wrap `scip-search` or its language indexers. Operators install and
maintain `scip-search`, `scip-go`, `scip-typescript`, and `scip-python`
separately.

### Agent Execution Timeouts

| Role | Timeout | Rationale |
|------|---------|-----------|
| Code Reviewer | 30 min | Reviews should complete quickly |
| Coder | 2 hours | Implementation takes longer |
| Planner | 4 hours | Complex planning needs time |

When exceeded, supervisor kills CLI, resets agent to IDLE, retries after 5s delay.

During active task execution, the supervisor also watches for observable progress. Progress means a meaningful task-state change, worktree HEAD/status change including untracked files, or provider stdout/stderr activity. If no progress is observed for `agent_progress_timeout`, the supervisor cancels the provider process, waits for it to exit, transitions the still-owned executing task to `BLOCKED`, cleans its worktree, and records the diagnostic in `blocked_reason`. The watchdog polls at `agent_progress_timeout / 4`, capped at 15 seconds.

**Note:** Doer roles now use `doer_max_wait`; legacy `coder_max_wait` is still accepted for existing state files but new projects should use `doer_max_wait`.

## Tuning Guidelines

### Short Tasks (<10 min)
```yaml
config:
  heartbeat_interval: 30
  lease_duration: 900       # 15 min
  doer_max_wait: 600       # 10 min
  max_coder_iterations: 5   # Escalate fast
```

### Long Tasks (30min-5hr)
```yaml
config:
  heartbeat_interval: 60
  lease_duration: 3600      # 1 hour
  doer_max_wait: 18000     # 5 hours
  max_coder_iterations: 15  # More iterations
```

### Network Filesystems (NFS, SMB)
```yaml
config:
  heartbeat_interval: 90    # Less frequent writes
  lease_duration: 2700      # 45 min
  # fsnotify may not work -- agents fall back to polling
```

### Fast Feedback
```yaml
config:
  max_coder_iterations: 5   # Escalate faster
  max_review_cycles: 3      # Fewer rejection cycles
  heartbeat_interval: 30    # Faster crash detection
```

### Auto-Resume (`auto_resume`)

When enabled, agents automatically resume the system at CHECKPOINT and COMPLETED sprint states instead of blocking for manual `liza resume`. Defaults to `false`.

- **At init time:** `liza init --auto-resume "Goal"`
- **At runtime:** Press `y` in the TUI to toggle

Use `liza pause` (or `p` in TUI) for a hard stop — pause is never auto-resumed.

### No Follow-Up (`no_follow_up`)

When enabled, Liza suppresses top-level `pipeline-transitions` (cross-sub-pipeline transitions). Subpipeline-local transitions still run normally, but cross-sub-pipeline follow-up transitions are not shown in status, auto-executed, or allowed through `liza proceed`. Defaults to `false`.

- **At init time:** `liza init --no-follow-up "Goal"`
- **State config:** `no_follow_up: true`

### Worktree Setup (`post_worktree_cmd`)

Worktrees are bare checkouts — they lack build artifacts like `node_modules/`, `vendor/`, or compiled outputs. The `post_worktree_cmd` config field specifies a shell command that runs after every worktree creation, ensuring agents have a build-ready workspace.

**Setting it:**

- **At init time:** `liza init "Goal" --post-worktree-cmd "npm install"`
- **Auto-detection:** When `--post-worktree-cmd` is not provided, `liza init` checks for `package.json` in the project root (and immediate subdirectories for monorepo layouts) and suggests the appropriate install command based on the lockfile. For a single subdirectory (e.g. `web/`), it suggests `cd web && npm install`. For multiple subdirectories, it prompts for manual configuration:

  | Lockfile detected | Suggested command |
  |-------------------|-------------------|
  | `pnpm-lock.yaml` | `pnpm install` |
  | `yarn.lock` | `yarn install` |
  | `bun.lockb` / `bun.lock` | `bun install` |
  | `package-lock.json` (or none) | `npm install` |

- **After merge:** If `post_worktree_cmd` is still unset and a successful merge introduces an unambiguous Node project layout on the integration branch, `liza wt-merge` auto-populates the same detected command. Ambiguous layouts still require manual configuration.
- **After init:** Add `post_worktree_cmd: "your command"` to the `config` section of `.liza/state.yaml`.

**Behavior:** The command runs via `sh -c` in the worktree directory. It is non-fatal — warnings are emitted but worktree creation succeeds even if the command fails.

**Coder fallback:** If a coding worktree still lacks declared project dependencies during validation, coder prompts pre-authorize rerunning the configured project-scoped bootstrap command. Coders first query `liza get config.post_worktree_cmd --json`; when configured, they may run that command from the task worktree. If absent, they may use only the repo's existing lockfile-preserving install workflow. They must not edit manifests or lockfiles, install global/system tools, or add/upgrade/remove dependencies unless the task scope explicitly requires it.

## System Modes

| Mode | Agents | Heartbeats | Set by |
|------|--------|------------|--------|
| `RUNNING` | Work normally | Yes | `liza resume` / `liza start` |
| `PAUSED` | Block, don't claim | Yes | `liza pause` |
| `STOPPED` | Exit cleanly | Stop | `liza stop` |
| `CIRCUIT_BREAKER_TRIPPED` | Halt | Yes | `liza analyze` or `liza tui` (auto on pattern trigger) |

**PAUSED**: Agents stay alive, resume instantly. Use for manual edits.
**STOPPED**: Agents exit. Must restart manually. Use for end of session.

```
RUNNING <-> PAUSED (liza pause / liza resume)
RUNNING -> STOPPED (liza stop)
STOPPED -> RUNNING (liza start, then restart agents)
CIRCUIT_BREAKER_TRIPPED -> RUNNING (liza resume, after fixing root cause)
```

When `liza tui` triggers the circuit breaker, it also sets `sprint.status` to `CHECKPOINT`.

`liza tui` also auto-checkpoints when all non-terminal planned tasks are BLOCKED (sprint stalled), since no agent can make further progress without human intervention.

## Checkpoint Summary

After a successful merge, Liza auto-invokes the configured default CLI with the
`checkpoint-summary` skill and writes the latest report to
`.liza/checkpoint-summary.md`. The operation is best-effort: merge success does
not depend on the report being created. Set `auto_checkpoint_summary: false` in
`.liza/state.yaml` to disable it.

The summary emitter snapshots git status paths and filesystem metadata before
and after the CLI run. Changes outside `.liza/checkpoint-summary.md` are logged
as an auto-summary failure and do not block the completed merge.

## Task Lifecycle States

| Status | Claimable | Reviewable | Terminal |
|--------|-----------|------------|----------|
| DRAFT | No | No | No |
| DRAFT_CODE | Yes | No | No |
| IMPLEMENTING_CODE | No | No | No |
| CODE_TO_REVIEW | No | Yes | No |
| CODE_REJECTED | Yes | No | No |
| CODE_APPROVED | No | No | No |
| MERGED | No | No | **Yes** |
| BLOCKED | No | No | No |
| ABANDONED | No | No | **Yes** |
| SUPERSEDED | No | No | **Yes** |
| INTEGRATION_FAILED | Yes | No | No |
| | | | |
| **Integration-pair** | | | |
| DRAFT_INTEGRATION_ANALYSIS | Yes | No | No |
| ANALYZING_INTEGRATION | No | No | No |
| INTEGRATION_ANALYSIS_TO_REVIEW | No | Yes | No |
| REVIEWING_INTEGRATION_ANALYSIS | No | No | No |
| INTEGRATION_ANALYSIS_APPROVED | No | No | No |
| INTEGRATION_ANALYSIS_REJECTED | Yes | No | No |
| INTEGRATION_ANALYSIS_CLEAN | No | No | **Yes** |

> **Note:** Status names are pipeline-specific. The tables above show `coding-pair` and `integration-pair` states.
> Other role-pairs use their own names (e.g. `DRAFT_EPIC_PLAN`, `DRAFT_US`).
> See `pipeline.yaml` for the full list.

## Supported CLIs

The `--cli` flag on `liza agent` and `liza repair-agent-pool` selects which coding agent to invoke. When omitted, the default is resolved from role-specific config (`config.default_doer_cli` for doers and orchestrators, `config.default_reviewer_cli` for reviewers), then role-specific env (`LIZA_DEFAULT_DOER_CLI` for doers and orchestrators, `LIZA_DEFAULT_REVIEWER_CLI` for reviewers), then `config.default_cli`, then `LIZA_DEFAULT_CLI`, then `claude`. Set defaults at init time with `liza init --default-cli <cli>`, `liza init --default-doer-cli <cli>`, or `liza init --default-reviewer-cli <cli>`.

Headless watch automatically runs the repair-agent-pool behavior when a task is immediately claimable but no live agent is registered for the required role. This is enabled by default. Set `LIZA_AUTO_REPAIR_AGENT_POOL=0`, `false`, or `no` to disable it. Unset or empty values enable it; other invalid non-empty values also leave it enabled and emit a warning.

| CLI | Notes |
|-----|-------|
| `claude` | Claude Code (fallback default when no config is set) |
| `codex` | OpenAI Codex CLI |
| `gemini` | Google Gemini CLI |
| `mistral` | Mistral Le Chat CLI |
| `kimi` | Kimi (alias to claude with Kimi-specific env vars) |

## Output Logging

`liza agent` automatically streams the agent's output to `.liza/agent-outputs/` while the CLI runs. Stdout is written as `{agent-id}-{timestamp}.txt` and stderr as `{agent-id}-{timestamp}.err`. The directory is created automatically if it does not exist. Pass `--no-log` to disable.

**Secret masking:** Persisted log files are automatically sanitized — environment variable values whose names match sensitive patterns (e.g. `*_API_KEY`, `*_TOKEN`, `*_SECRET`, `*_PASSWORD`, provider-specific keys) are replaced with `***`. Live terminal output is **not** masked, so operators see full output during the session. Values shorter than 8 characters are excluded to avoid false positives.

```bash
liza agent coder                  # logging enabled (default)
liza agent coder --no-log         # logging disabled
```

Logging is automatically disabled in `-i` (interactive) mode.

## Agent Identity

Agent identity is auto-assigned by default (`coder-1`, or `coder-2` if `coder-1` is active). Override with:

1. **CLI flag**: `liza agent coder --agent-id coder-5`
2. **Environment variable**: `export LIZA_AGENT_ID=coder-5`

The `--agent-id` flag takes precedence over `LIZA_AGENT_ID`.

**Agent ID format**: `{role}-{number}` — e.g. `coder-1`, `code-reviewer-1`, `planner-1`.

**System commands** (`pause`, `stop`, `start`, `resume`, `release-claim`) use `--changed-by` for audit trail (defaults to `human`).

## Environment Variables

Environment variables are process-local fallbacks or identity inputs. Durable
project configuration belongs in `.liza/state.yaml`.

| Variable | Required | Default | Purpose |
|----------|----------|---------|---------|
| `LIZA_AGENT_ID` | For agent commands | -- | Agent identifier (format: `{role}-{number}`) |
| `LIZA_DISABLE_CLAUDE_SUBAGENTS` | No | unset | Set to `1` to launch Claude Code agents with `--disallowedTools Task`, disabling Claude subagent delegation. Use only when intentionally waiving Claude subagent delegation; agents may be unable to satisfy contract delegation triggers while this is set. |
| `LIZA_ENABLE_SCIP_SEARCH` | No | unset | Strict opt-in MAS activation gate for SCIP indexing and `scip-search` prompt guidance. Truthy values are `1` and `true`; unset, empty, `0`, and `false` disable it. Values are trimmed and parsed case-insensitively. |
| `LIZA_CODEX_VERSION` | No | unset | Process-local fallback for `config.codex_package_version` when launching headless Codex agents |
| `LIZA_CODEX_LEGACY_LANDLOCK` | No | unset | Process-local fallback enabling legacy Landlock for headless Codex agents when `config.codex_legacy_landlock` is false |
| `LIZA_SPECS` | No | `specs/` | Path to specs directory (relative to project root) |
| `LIZA_LOG_LEVEL` | No | `INFO` | Logging verbosity: DEBUG, INFO, WARN, ERROR |

## Making Configuration Changes

1. `liza pause --reason "config update"`
2. Use a Liza command for the config change when one exists. Manual state
   changes are support operations, not the normal config path.
3. `liza validate`
4. `liza resume`

**Never change state while agents are running** without pausing first.
