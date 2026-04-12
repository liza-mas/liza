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

### Troubleshooting

**State file errors:**
- Verify project initialized: `liza validate`
- Check: `ls -la .liza/state.yaml`

## Configuration Matrix

All configuration lives in `.liza/state.yaml` under the `config` section.

| Parameter | Default | Min | Max | Unit | Purpose |
|-----------|---------|-----|-----|------|---------|
| `max_coder_iterations` | 10 | 1 | 100 | count | Max iterations per coder per task |
| `max_review_cycles` | 5 | 1 | 20 | count | Max review rejection cycles |
| `heartbeat_interval` | 60 | 1 | 300 | seconds | Heartbeat frequency |
| `lease_duration` | 1800 | 300 | 7200 | seconds | Task lease duration |
| `coder_poll_interval` | 30 | 5 | 120 | seconds | Check interval (legacy, now event-driven) |
| `coder_max_wait` | 7200 | 300 | 7200 | seconds | Max idle before agent exits |
| `planner_poll_interval` | 60 | — | — | seconds | Planner polling interval |
| `planner_max_wait` | 7200 | — | — | seconds | Max planner idle before exit |
| `reviewer_poll_interval` | 30 | — | — | seconds | Reviewer polling interval |
| `reviewer_max_wait` | 7200 | — | — | seconds | Max reviewer idle before exit |
| `post_worktree_cmd` | (none) | — | — | shell cmd | Command run after worktree creation (e.g. `npm install`) |

### Agent Execution Timeouts

| Role | Timeout | Rationale |
|------|---------|-----------|
| Code Reviewer | 30 min | Reviews should complete quickly |
| Coder | 2 hours | Implementation takes longer |
| Planner | 4 hours | Complex planning needs time |

When exceeded, supervisor kills CLI, resets agent to IDLE, retries after 5s delay.

**Note:** Planners now respect `planner_max_wait` (default 2 hours). Previously planners ran indefinitely; they now exit after the configured idle timeout, same as coders and reviewers.

## Tuning Guidelines

### Short Tasks (<10 min)
```yaml
config:
  heartbeat_interval: 30
  lease_duration: 900       # 15 min
  coder_max_wait: 600       # 10 min
  max_coder_iterations: 5   # Escalate fast
```

### Long Tasks (30min-2hr)
```yaml
config:
  heartbeat_interval: 60
  lease_duration: 3600      # 1 hour
  coder_max_wait: 7200      # 2 hours
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

### Worktree Setup (`post_worktree_cmd`)

Worktrees are bare checkouts — they lack build artifacts like `node_modules/`, `vendor/`, or compiled outputs. The `post_worktree_cmd` config field specifies a shell command that runs after every worktree creation, ensuring agents have a build-ready workspace.

**Setting it:**

- **At init time:** `liza init "Goal" --post-worktree-cmd "npm install"`
- **Auto-detection:** When `--post-worktree-cmd` is not provided, `liza init` checks for `package.json` in the project root and suggests the appropriate install command based on the lockfile:

  | Lockfile detected | Suggested command |
  |-------------------|-------------------|
  | `pnpm-lock.yaml` | `pnpm install` |
  | `yarn.lock` | `yarn install` |
  | `bun.lockb` / `bun.lock` | `bun install` |
  | `package-lock.json` (or none) | `npm install` |

- **After init:** Add `post_worktree_cmd: "your command"` to the `config` section of `.liza/state.yaml`.

**Behavior:** The command runs via `sh -c` in the worktree directory. It is non-fatal — warnings are emitted but worktree creation succeeds even if the command fails.

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

## Task Lifecycle States

| Status | Claimable | Reviewable | Terminal |
|--------|-----------|------------|----------|
| DRAFT | No | No | No |
| DRAFT_CODE | Yes | No | No |
| IMPLEMENTING_CODE | No | No | No |
| CODE_READY_FOR_REVIEW | No | Yes | No |
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

The `--cli` flag on `liza agent` selects which coding agent to invoke. When omitted, the default is resolved from `config.default_cli` in `state.yaml`, then the `LIZA_DEFAULT_CLI` environment variable, then `claude`. Set the default at init time with `liza init --default-cli <cli>`.

| CLI | Notes |
|-----|-------|
| `claude` | Claude Code (fallback default when no config is set) |
| `codex` | OpenAI Codex CLI |
| `gemini` | Google Gemini CLI |
| `mistral` | Mistral Le Chat CLI |
| `kimi` | Kimi (alias to claude with Kimi-specific env vars) |

## Output Logging

`liza agent` automatically saves a copy of the agent's output to `.liza/agent-outputs/`. Stdout is saved as `{agent-id}-{timestamp}.txt` and stderr as `{agent-id}-{timestamp}.err`. The directory is created automatically if it does not exist. Pass `--no-log` to disable.

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

| Variable | Required | Default | Purpose |
|----------|----------|---------|---------|
| `LIZA_AGENT_ID` | For agent commands | -- | Agent identifier (format: `{role}-{number}`) |
| `LIZA_SPECS` | No | `specs/` | Path to specs directory (relative to project root) |
| `LIZA_LOG_LEVEL` | No | `INFO` | Logging verbosity: DEBUG, INFO, WARN, ERROR |

## Making Configuration Changes

1. `liza pause --reason "config update"`
2. Edit `state.yaml` (or use commands)
3. `liza validate`
4. `liza resume`

**Never edit state.yaml while agents are running** without pausing first.
