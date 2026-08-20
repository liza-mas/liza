# Configuration Reference

System configuration, tuning parameters, and environment variables.

## Global Setup (`§BRAND_BINARY_NAME§ setup`)

`§BRAND_BINARY_NAME§ setup` writes §BRAND_NAME_TITLE§'s global support files to `~/§BRAND_GLOBAL_DIRNAME§/`:

- `CORE.md`, `PAIRING_MODE.md`, `MULTI_AGENT_MODE.md`, and other contract files
- `AGENT_TOOLS.md`
- `COLLABORATION_CONTINUITY.md`
- default `pipeline.yaml`
- `skills/`
- `support-docs/`

Bare `§BRAND_BINARY_NAME§ setup` is the default global install path. It does not require a
provider flag. Provider flags add provider-specific integrations in the user's
CLI config directories. For Claude, Codex, OpenCode, and Gemini, setup creates
skill symlinks under `~/.claude/skills/`, `~/.codex/skills/`,
`~/.config/opencode/skills/`, or `~/.gemini/skills/` pointing to
`~/§BRAND_GLOBAL_DIRNAME§/skills/`. `--cursor` is a convenience shortcut for the
Claude and Codex global setup Cursor relies on; it does not write Cursor project
rules or MCP configuration. Mistral/Vibe also gets its prompt link under
`~/.vibe/prompts/`. Project hooks, Cursor project activation, and runtime
provider settings are handled by `§BRAND_BINARY_NAME§ init`. Built-in shortcut
flags remain supported; catalog providers use repeatable `--provider <id>`:

```bash
§BRAND_BINARY_NAME§ setup --claude
§BRAND_BINARY_NAME§ setup --codex
§BRAND_BINARY_NAME§ setup --cursor
§BRAND_BINARY_NAME§ setup --opencode
§BRAND_BINARY_NAME§ setup --gemini
§BRAND_BINARY_NAME§ setup --mistral
§BRAND_BINARY_NAME§ setup --provider qwen
```

Use `--force` to refresh existing global files after an upgrade; combine it
with `--yes` to accept overwrite prompts non-interactively. `--yes` also
accepts provider symlink replacement prompts. Use `--agent-tools <path>` to
install a custom `AGENT_TOOLS.md` instead of the embedded default.

## Update Preferences

Interactive update preferences live in `~/§BRAND_GLOBAL_DIRNAME§/update.json`. Explicit
`--check-update` and `--update-channel` flags persist there so future
interactive `§BRAND_BINARY_NAME§` runs reuse the saved choice.

| Key | Values | Purpose |
|-----|--------|---------|
| `check_update` | `true`, `false`, or unset | Enables or disables interactive update checks |
| `channel` | `stable`, `main`, or unset | Selects release updates or main-branch updates |

Command-line flags take precedence over saved preferences and update the file.
`§BRAND_ENV_PREFIX§_UPDATE_CHANNEL` overrides the saved channel for that process. Saved
`check_update` controls whether update checks run when no explicit flag is
provided.

## Project Initialization (`§BRAND_BINARY_NAME§ init`)

Run `§BRAND_BINARY_NAME§ init` in each project where §BRAND_NAME_TITLE§ should activate the contract. The
interactive wizard walks through mode selection, provider selection, and
project-local setup. Add `--yes` when using explicit init arguments to
auto-confirm approval prompts such as provider config merges, template
overwrites, detected `post_worktree_cmd` suggestions, and removal of existing
workspace data before full initialization. Workspace cleanup lists
`§BRAND_PROJECT_DIRNAME§/`, `.worktrees/`, and associated task branches before
deleting them. `--yes` bypasses that confirmation, but never ownership or
live-agent safety checks. Run `§BRAND_BINARY_NAME§ cleanup` to perform the same
cleanup without immediately initializing another goal.

An existing repo contract file does not trigger a conflict prompt when every
affected global-first provider can use its preferred global path. When repo
placement still requires a decision, the wizard asks once per shared repo path,
identifies the affected providers, and offers only destinations that are
currently available to all of them.

Depending on selected providers and options, `§BRAND_BINARY_NAME§ init` writes or updates:

- provider contract discovery links to `~/§BRAND_GLOBAL_DIRNAME§/CORE.md`.
  Providers with documented global instruction files use those paths; Cursor,
  Kimi, and Devin use repo-root files.
- project-local provider hooks and settings, such as `.claude/settings.json`,
  `.codex/` hook configuration, and standalone `bash-policy` provider hooks
- `.claude/hooks/` and `.codex/hooks/` scripts that enforce session
  initialization, inject project context, guard Git usage, route RTK, and catch
  wrong-worktree paths for providers that support hooks
- `.codex/config.toml` and `.codex/hooks.json` for project-local Codex hook
  activation
- global `~/.codex/config.toml` entries for Codex's project root, project `.git`
  directory, support/cache writable roots, and noninteractive workspace baseline
- global contract symlinks for global-first providers, including active
  `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `XDG_CONFIG_HOME`, and `QWEN_HOME`
  overrides
- `.claudeignore` when absent or explicitly refreshed
- `GUARDRAILS.md` when absent
- `§BRAND_PROJECT_DIRNAME§/state.yaml`, `§BRAND_PROJECT_DIRNAME§/log.yaml`, and `§BRAND_PROJECT_DIRNAME§/pipeline.yaml` for a MAS
  workspace
- the configured integration branch for MAS runs
- optional tool activation artifacts when `§BRAND_ENV_PREFIX§_ENABLE_STACKLIT`,
  `§BRAND_ENV_PREFIX§_ENABLE_SCIP_SEARCH`, or `§BRAND_ENV_PREFIX§_ENABLE_SEMBLE` is enabled
- standalone `.bash-policy.yaml` defaults and selected provider hooks when
  `§BRAND_ENV_PREFIX§_ENABLE_BASH_POLICY` is enabled

### Task ID slugs

Task IDs use compact, configurable segments so complete IDs remain readable in
the TUI. `role-pairs.<name>.task-slug` controls the prefix for initial tasks;
each transition's `task-slug` controls the segment appended to its child task
IDs. Both fields are optional: role pairs fall back to their name without the
conventional `-pair` suffix, while transitions fall back to their name.

The embedded pipeline supplies compact defaults. Because
`§BRAND_PROJECT_DIRNAME§/pipeline.yaml` is frozen during initialization,
existing workspaces retain their original task IDs and naming behavior.

| Role pair | Default slug |
|-----------|--------------|
| `epic-planning-main-pair` | `epm` |
| `epic-planning-pair` | `ep` |
| `us-writing-pair` | `uw` |
| `architecture-main-pair` | `arm` |
| `architecture-pair` | `ar` |
| `code-planning-main-pair` | `cpm` |
| `code-planning-pair` | `cp` |
| `coding-pair` | `code` |
| `integration-pair` | `ia` |

The `integration-to-fix` transition uses `fix`, distinguishing rework from
normal coding tasks. For example, `epic-1-arm-ar-0-cp-0-code-0` identifies a
coding task reached through architecture and code planning.

Provider catalog metadata defines the active contract location:

| Provider | Active contract path |
|----------|----------------------|
| Claude | `${CLAUDE_CONFIG_DIR:-~/.claude}/CLAUDE.md` |
| Codex | `${CODEX_HOME:-~/.codex}/AGENTS.md` |
| OpenCode | `${XDG_CONFIG_HOME:-~/.config}/opencode/AGENTS.md`; relative `XDG_CONFIG_HOME` values are invalid and fall back to `~/.config` |
| Gemini | `~/.gemini/GEMINI.md` |
| Qwen | `${QWEN_HOME:-~/.qwen}/QWEN.md` for unset, absolute, or `~`-based values; `<repo>/QWEN.md` for relative values |
| Cursor | `<repo>/AGENTS.md` |
| Kimi | `<repo>/CLAUDE.md` |
| Devin | Catalog-defined repo path |

The embedded Devin repo filename is rendered from the build-time product name.
Paths declared by downloaded or operator-supplied catalogs remain literal.

Global-first providers set `contract.prefer_global`. Initialization creates or
verifies the active global link before removing any managed repo link. Repo-only
activations are recorded at
`<main-repository-git-dir>/§BRAND_NAME_LOWER§-provider-activations.json`. §BRAND_NAME_TITLE§
resolves the main repository root before locating its Git directory, so a later
init preserves only paths used by providers activated in that repository.
When this metadata is first introduced, existing managed repo links are
attributed directly when their contract path has only one catalog claimant.
Shared paths require corroborating repo-local activation artifacts for the
repo-only provider; otherwise the link is treated as an unowned global-first
fallback and may be deduplicated. Kimi has no repo-local activation artifact, so
a first post-upgrade Claude initialization may remove a legacy `CLAUDE.md` link
whose Kimi ownership was never recorded; rerun
`§BRAND_BINARY_NAME§ init --provider kimi` to restore it. Paths shared by
providers selected together also remain because another provider's global
activation may fail at runtime. If the global path cannot be resolved or
created, or is occupied by a user-owned file, the repo link is retained or
created instead. Reinitializing a provider also retains its previously recorded
repo or local fallback when no authoritative placement succeeds. After the
preferred global link is verified, that prior managed activation is removed only
when no other provider owns it. Repo-only providers never receive an invented global fallback.
Custom providers without `prefer_global` retain managed links at both declared
locations and emit a duplicate warning.

Malformed, semantically invalid, or unsupported-version activation metadata
emits a warning instead of stopping initialization. The command leaves the
untrusted metadata file unchanged and preserves every managed repo link that the
current provider activation could otherwise remove. Delete the
`§BRAND_NAME_LOWER§-provider-activations.json` file from the main repository Git
directory to rebuild trusted metadata on a later run. Read or write permission
errors still stop initialization; restore access to the Git directory and rerun
the command.

§BRAND_NAME_TITLE§ never overwrites user-owned contract files. If both the repo-root
file and a declared global path are user-owned, `§BRAND_BINARY_NAME§ init` warns and
skips that provider activation path.

During the initialization gate, shell fallbacks read one mandatory document per command;
consecutive invalid reads instruct the agent to stop instead of trying path or command variants.

Global setup repairs existing provider contract symlinks at both the active environment-resolved
path and the provider's home-default path when they still target the name-derived global root and
`§BRAND_GLOBAL_DIRNAME§` differs. An unresolvable provider root emits a warning without stopping
setup. Repair preserves absent links, regular files, and unrelated symlinks. Codex writable roots
include both `~/.§BRAND_NAME_LOWER§` and `~/§BRAND_GLOBAL_DIRNAME§` when those directories differ.

## Claude Code Settings

**`.claude/settings.json`** — project-level permissions for §BRAND_NAME_TITLE§ CLI commands, skills, git operations, and build commands.

`§BRAND_BINARY_NAME§ init` writes this file automatically from the embedded [`claude-settings.json`](../internal/embedded/claude-settings.json). The master defines all §BRAND_NAME_TITLE§ CLI permissions, skills, and the full set of bash permissions agents need. **Do not hand-craft a subset** — agents will be blocked on any missing permission.

**Key elements:**
- **`enableAllProjectMcpServers`** — enables any project MCP servers (for non-§BRAND_NAME_TITLE§ tools like filesystem, etc.)
- **`Bash(§BRAND_BINARY_NAME§:*)`** — grants permission for agents to invoke §BRAND_NAME_TITLE§ CLI commands
- **`Skill(...)`** — contract skills from `~/§BRAND_GLOBAL_DIRNAME§/skills/` (installed by `§BRAND_BINARY_NAME§ setup`)
- **No `defaultMode`** — §BRAND_NAME_TITLE§ never writes one here, and removes any it finds when merging. Claude Code v2.1.142 and later deliberately ignore `auto` in `.claude/settings.json` and `.claude/settings.local.json` "so a repository cannot grant itself auto mode" ([permission modes](https://code.claude.com/docs/en/permission-modes)); a value that *is* honored here shadows the mode you set in your own `~/.claude/settings.json`. Set the mode you want for interactive sessions there. MAS agents do not read it: they are launched with `--permission-mode auto` from the provider catalog, so their mode is the same on every machine. Override it per project with an `agent_tools` entry that restates `run_args` and `logged_run_args` (see [Supported CLIs](#supported-clis)).

  Two consequences worth knowing:

  - **Auto mode has account and model requirements.** Per the [documented requirements](https://code.claude.com/docs/en/permission-modes), it needs Opus 4.6+, Sonnet 4.6+, or Fable 5 on the Anthropic API and Claude Platform on AWS, and Sonnet 5, Opus 4.7+, or Fable 5 on Bedrock, Agent Platform, Foundry, and gateway sessions; Haiku, Sonnet 4.5, Opus 4.5, and claude-3 models are unsupported on every provider. Organization administrators can also disable it via `permissions.disableAutoMode` in managed settings, which rejects `--permission-mode auto` at startup. When the model is merely unsupported there is no startup error: an agent run observed on `claude-haiku-4-5-20251001` proceeded normally and then refused its `Write` tool call, asking for permission and exiting non-zero with no file written — the same invocation with `--permission-mode acceptEdits` succeeded. In a MAS run that presents as an agent that works and produces nothing, so check the model first if agents stop making edits.
  - **Auto mode ignores allow rules it treats as classifier-bypassing**, so the `allow` list below no longer fully determines what agents may do.
- **`permissions.additionalDirectories`** — grants access to required non-project directories such as `~/§BRAND_GLOBAL_DIRNAME§` and `/tmp`

### Two-Layer Architecture

Claude Code unions permissions from global and project settings:

| Layer | File | Managed by | Contains |
|-------|------|-----------|----------|
| **Project** | `<project>/.claude/settings.json` | `§BRAND_BINARY_NAME§ init` (automatic) | §BRAND_NAME_TITLE§ CLI permissions, skills, git/build commands |
| **Global** | `~/.claude/settings.json` | Manual (one-time) | Personal MCP tools (IDE, search, etc.), machine-specific permissions |

The project layer is portable (team-shared). The global layer is machine-specific (personal tools and paths). Neither alone is sufficient — both are needed.

For global setup and project activation, use `§BRAND_BINARY_NAME§ setup` and `§BRAND_BINARY_NAME§ init`.

## Codex Project Permissions

**`~/.codex/config.toml`** — global Codex CLI settings.

`§BRAND_BINARY_NAME§ init --codex` manages the Codex settings §BRAND_NAME_TITLE§ needs for unattended
supervisor tasks and pairing mode. It adds or corrects the global Codex
baseline (noninteractive approvals, the auto-review preference,
workspace-write sandboxing, and network access). It does not manage model,
reasoning effort, or personality settings. It also adds the active project root
plus the active project `.git` directory to
`sandbox_workspace_write.writable_roots` so Codex can edit project files and
write Git metadata. It also adds Codex/§BRAND_NAME_TITLE§ support directories and user cache
roots to `writable_roots`. If the file already exists, §BRAND_NAME_TITLE§ prompts before
merging those entries and preserves unrelated settings.

Earlier versions wrote `model`, `model_reasoning_effort`, and `personality`.
Upgrades preserve those values because they may have been changed by the user;
remove them from `~/.codex/config.toml` to return to Codex defaults.

When launching headless MAS agents, §BRAND_NAME_TITLE§ relies on this global Codex
configuration for sandbox mode, approval policy, network access, and writable
roots. It does not pass launch-time permission overrides (`-c
sandbox_mode=...`, `-c approval_policy=...`) or explicit `--add-dir` entries.
Git worktrees write the task index under the main repo metadata path
(`.git/worktrees/<task>/index.lock`), not under the worktree directory itself,
so the active project `.git` directory must be present in `writable_roots`.

Pin MAS Codex agents to a specific package version with this durable project
config key:

```yaml
config:
  codex_package_version: "0.125.0"
```

For a temporary process-local fallback when that config field is unset, set this
before running `§BRAND_BINARY_NAME§ agent`:

```bash
export §BRAND_ENV_PREFIX§_CODEX_VERSION=0.125.0
```

`codex_package_version` or `§BRAND_ENV_PREFIX§_CODEX_VERSION` makes §BRAND_NAME_TITLE§ launch headless
Codex agents through
`npm exec --yes --package @openai/codex@<version> -- codex`.
The state config version takes precedence over the environment fallback.
Interactive `§BRAND_BINARY_NAME§ agent -i` keeps using the installed Codex binary.

The recommended complete setup shape is:

```toml
approval_policy = "never"
approvals_reviewer = "auto_review"

sandbox_mode = "workspace-write"

[permissions.workspace.network]
enabled = true

[sandbox_workspace_write]
network_access = true
exclude_tmpdir_env_var = false
exclude_slash_tmp = false
writable_roots = [
  "/home/<USER>/.codex",
  "/home/<USER>/§BRAND_PROJECT_DIRNAME§",
  "/home/<USER>/.npm",
  "/home/<USER>/.pyenv/shims",
  "/home/<USER>/.cache",
  "/tmp",
  "/path/to/project",
  "/path/to/project/.git",
]
```

`§BRAND_BINARY_NAME§ init --codex` manages the active project entries and preserves unrelated
settings when merging an existing config.

## OpenCode

`§BRAND_BINARY_NAME§ setup --opencode` installs §BRAND_NAME_TITLE§ skill symlinks under
`~/.config/opencode/skills/`.

`§BRAND_BINARY_NAME§ init --opencode` activates the §BRAND_NAME_TITLE§ contract through the shared
`AGENTS.md` discovery file. It does not write Codex hooks or Codex settings. If
the repository already has a non-§BRAND_NAME_TITLE§ `AGENTS.md`, §BRAND_NAME_TITLE§ uses the OpenCode
fallback symlink at `~/.config/opencode/AGENTS.md`.

Init also installs §BRAND_NAME_TITLE§'s managed `.opencode/tools/exec.ts` compatibility tool.
The tool exposes a simple `exec` schema with required `cmd` plus nullable
optional `workdir` and `timeout_ms` fields. OpenCode agents are instructed to
prefer this tool for shell and file operations, omit optional fields when not
needed, avoid repeating successful commands, inspect command results, and move
to the next §BRAND_NAME_TITLE§ protocol step. §BRAND_NAME_TITLE§ only overwrites this file when its managed
header is present; user-owned OpenCode files are preserved.

Headless MAS runs with `--cli opencode` execute:

```bash
opencode run "<prompt>" --dangerously-skip-permissions
```

Logged runs add `--format json`. OpenCode has
[native ACP support](https://opencode.ai/docs/acp/). If another ACP-capable
tool needs an OpenCode agent server, configure it to run the `opencode acp`
command, not an `opencode-acp` executable:

```json
{
  "agent_servers": {
    "OpenCode": {
      "command": "opencode",
      "args": ["acp"]
    }
  }
}
```

Within §BRAND_NAME_TITLE§, `--cli opencode-acp` is the selector for the ACPX-backed runtime
that targets OpenCode. It is not an OpenCode command name.

For Groq-backed OpenCode runs, prefer a stable tool-calling model such as
Llama 3.3 70B over GPT-OSS 120B until Harmony/tool-call behavior is proven
reliable in this path. Always validate the selected model against a real §BRAND_NAME_TITLE§
task before relying on it for unattended work.

### Troubleshooting

**State file errors:**
- Verify project initialized: `§BRAND_BINARY_NAME§ validate`
- Check: `ls -la §BRAND_PROJECT_DIRNAME§/state.yaml`

**Codex `.git` read-only in linked worktrees:**
- Verify `~/.codex/config.toml` includes the project root and project `.git`
  directory in `sandbox_workspace_write.writable_roots`.
- If the failure is version-specific, pin MAS agents with
  `config.codex_package_version` or temporary `§BRAND_ENV_PREFIX§_CODEX_VERSION`.

## Configuration Matrix

All configuration lives in `§BRAND_PROJECT_DIRNAME§/state.yaml` under the `config` section.
The embedded fallback CLI names are `claude`, `codex`, `codex-acp`,
`cursor-acp`, `opencode`, `opencode-acp`, `gemini`, `mistral`, and `kimi`.
Additional catalog entries such as `qwen`, `qwen-acp`, `devin`, and `devin-acp`
are loaded at runtime from the provider catalog.

| Parameter | Default | Min | Max | Unit | Purpose |
|-----------|---------|-----|-----|------|---------|
| `max_coder_iterations` | 10 | 1 | 100 | count | Max iterations per coder per task |
| `max_review_cycles` | 5 | 1 | 20 | count | Max review rejection cycles |
| `max_global_integration_generations` | 3 | 1 | — | count | Max aggregate integration scans before exhaustion |
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
| `default_profile` | (none) | — | — | profile name | Global default structured launch profile |
| `default_doer_profile` | (none) | — | — | profile name | Default launch profile for doers and orchestrators |
| `default_reviewer_profile` | (none) | — | — | profile name | Default launch profile for reviewers |
| `agent_tools` | built-ins | — | — | map | Custom or overridden CLI launch definitions |
| `agent_profiles` | (none) | — | — | map | Named launch profiles that select a CLI and template variables |
| `codex_package_version` | (none) | — | — | npm package version | Pins headless Codex agents to `@openai/codex@<version>` |
| `post_worktree_cmd` | (none) | — | — | shell cmd | Command run after worktree creation (e.g. `npm install`) |
| `copy_worktree_env_files` | false | — | — | boolean | Explicitly authorize copying ignored root env files into task worktrees |
| `auto_checkpoint_summary` | true | — | — | boolean | Auto-runs checkpoint-summary after successful merges and writes `§BRAND_PROJECT_DIRNAME§/checkpoint-summary.md` |
| `scip_search` | (none) | — | — | language list | Durable allowlist of SCIP languages §BRAND_NAME_TITLE§ may index when `§BRAND_ENV_PREFIX§_ENABLE_SCIP_SEARCH` is truthy |

`max_global_integration_generations` has a deterministic default of `3`.
Absent or non-positive values normalize to the default `3`; positive values are
preserved. Each value bounds the number of independent aggregate scans. If the
current integration HEAD still lacks clean evidence after that many global
generations, integration is blocked with a generation-exhausted result rather
than reported as complete.

## Optional Tool Activation

Stacklit,
SCIP Search,
[Semble](https://github.com/MinishLab/semble/), and
Functional Clusters are optional navigation aids.
They are external tools; `§BRAND_BINARY_NAME§ toolchain` can install and verify
the local CLIs it manages, while §BRAND_NAME_TITLE§ activates prompt/index
guidance only when the configured gates and runtime checks pass.
Set the corresponding `§BRAND_ENV_PREFIX§_ENABLE_*` environment variable before running the
`§BRAND_BINARY_NAME§ setup` or `§BRAND_BINARY_NAME§ init` command that should activate that tool.
§BRAND_NAME_TITLE§ separates their activation across setup, pairing init, and MAS runtime:

- `§BRAND_BINARY_NAME§ toolchain` can install and doctor the local CLI prerequisites and write
  `~/§BRAND_GLOBAL_DIRNAME§/toolchain/env.sh` with selected `§BRAND_ENV_PREFIX§_ENABLE_*` gates. It does not
  install provider credentials or MCP connectors.
- `§BRAND_BINARY_NAME§ setup` owns global generic guidance in `~/§BRAND_GLOBAL_DIRNAME§/AGENT_TOOLS.md`. That
  guidance explains how agents should route to optional tools only when a session
  supplies explicit paths or readiness metadata. It must stay generic and must
  not contain project-specific generated paths, readiness state, or claims that
  optional tools are installed.
- Pairing `§BRAND_BINARY_NAME§ init` owns project-local activation artifacts. When an optional
  indexing environment gate is truthy during pairing init, §BRAND_NAME_TITLE§ installs or
  verifies the project-local hooks, generated-artifact cleanliness, SCIP command
  plans, and Semble safety files needed by pairing SessionStart context.
- MAS runtime owns per-agent prompt activation. MAS prompts include Stacklit,
  SCIP Search, or Semble sections only from target-specific runtime metadata for
  the current project root, task worktree, or reviewer worktree.

Disabled or unavailable optional tools degrade by omission. §BRAND_NAME_TITLE§ omits the
unavailable prompt or SessionStart section and agents fall back to direct source
reads, `rg` for exact literals and path discovery, `ast-grep` for syntax-shaped
search, and any semantic fallback tool exposed by the active tool policy.

When Stacklit or SCIP pairing activation is enabled, `§BRAND_BINARY_NAME§ init` installs a
managed `§BRAND_BINARY_NAME§-index.sh` entrypoint in Git's effective hooks directory, respecting
`core.hooksPath`. Managed wrappers invoke it from `post-commit`,
`post-checkout`, `post-merge`, and `post-rewrite` so repo-root Pairing indexes
stay fresh after normal local history changes. The wrappers skip task worktrees
and file-only checkout events. This Git hook plumbing is separate from
standalone `bash-policy` provider hooks.

### Stacklit (`§BRAND_ENV_PREFIX§_ENABLE_STACKLIT`)

`stacklit-cli` is an optional external repository-navigation tool. It is strict
opt-in.

`§BRAND_ENV_PREFIX§_ENABLE_STACKLIT` is process-local activation, not durable project state.
Values are trimmed and compared case-insensitively:

| Value | Meaning |
|-------|---------|
| `1`, `true` | Enable Stacklit activation for the current init/runtime process |
| unset, empty, `0`, `false` | Keep Stacklit disabled for the current init/runtime process |

Repository-level Stacklit inputs are operator-owned. Commit curated Stacklit
configuration when used:

```text
<project_root>/.stacklitrc.json
```

`stacklit.json` is generated runtime context. `stacklit-insights.json` may be a
curated input, but pairing hook refresh can also create or update it through
`stacklit init-insights`. Projects may either commit these files for a shared
baseline snapshot or ignore/protect them and regenerate them locally.

§BRAND_NAME_TITLE§ does not create or mutate `.stacklitrc.json`. When Stacklit input files
exist, `stacklit generate-json` consumes them naturally while refreshing
`stacklit.json`.

Pairing init behavior:

- Truthy `§BRAND_ENV_PREFIX§_ENABLE_STACKLIT` during pairing `§BRAND_BINARY_NAME§ init` installs or verifies
  project-local `§BRAND_BINARY_NAME§-index.sh` Git hook plumbing that refreshes the repo-root
  `stacklit.json` at safe lifecycle points.
- Pairing init also keeps generated Stacklit artifacts out of accidental task
  diffs by requiring them to be intentionally tracked, ignored, privately
  excluded, or otherwise protected by the generated project-local setup.
- Automatic pairing lifecycle refresh uses `stacklit diff` to skip no-op refresh,
  runs `stacklit generate-json -o stacklit.json --parse-workers 3`, runs
  `stacklit init-insights`, regenerates `stacklit.json` when insights change, and
  does not run `stacklit ai-summary`. Manual project-local refresh may include
  AI-summary only when the generated hook script is invoked with its explicit AI
  argument.
- Pairing init never writes repo-specific Stacklit paths into
  `~/§BRAND_GLOBAL_DIRNAME§/AGENT_TOOLS.md`.

MAS runtime behavior:

At runtime, §BRAND_NAME_TITLE§ runs `stacklit generate-json -o stacklit.json` at controlled
lifecycle points when `§BRAND_ENV_PREFIX§_ENABLE_STACKLIT` is truthy:

- Orchestrator refreshes `<project_root>/stacklit.json`.
- Task worktree creation, reviewer worktree recovery, and submit-for-review
  refresh `<worktree>/stacklit.json`.

Task-local `stacklit.json` is generated for prompt context only. §BRAND_NAME_TITLE§ requires
`stacklit.json` to be either tracked or ignored before task-local generation.
When it is tracked, §BRAND_NAME_TITLE§ marks only that task worktree copy as skip-worktree
before regenerating it. When it is ignored, the generated task-local file remains
ignored. §BRAND_NAME_TITLE§ rejects the unsafe middle state where `stacklit.json` is neither
tracked nor ignored, preserving the clean task-review invariant.

Stacklit generation failures degrade gracefully at runtime. §BRAND_NAME_TITLE§ still spawns
the agent and omits Stacklit prompt guidance when no task-local or project-root
`stacklit.json` is available. If a previously generated project-root index is
available after a failed root refresh, prompts may still include it as an
available repository snapshot; agents are instructed to verify behavior against
source files before editing.

Pairing SessionStart and MAS prompts advertise Stacklit only when they have an
explicit current-session index path. Agents must not infer index locations from
global guidance.

Explicit non-goals: §BRAND_NAME_TITLE§ does not install `stacklit-cli`, run `stacklit view`,
generate `stacklit.html`, or curate Stacklit insights. Operators install
Stacklit, commit their curated Stacklit inputs, and choose whether to commit or
ignore generated indexes and insights.

### Semble (`§BRAND_ENV_PREFIX§_ENABLE_SEMBLE`)

Semble is an optional external semantic repository search tool. It is strict
opt-in.

`§BRAND_ENV_PREFIX§_ENABLE_SEMBLE` is process-local activation, not durable project state.
Values are trimmed and compared case-insensitively:

| Value | Meaning |
|-------|---------|
| `1`, `true` | Enable Semble activation for the current init/runtime process when Semble is installed and offline-ready |
| unset, empty, `0`, `false` | Keep Semble disabled for the current init/runtime process |

Pairing init behavior:

- Truthy `§BRAND_ENV_PREFIX§_ENABLE_SEMBLE` during pairing `§BRAND_BINARY_NAME§ init` ensures the project root
  has a physical `.sembleignore` safety file before pairing SessionStart
  advertises Semble.
- The pairing `.sembleignore` excludes §BRAND_NAME_TITLE§ runtime state, generated indexes, and
  common credential patterns. Projects with sensitive source-adjacent files
  should add their own project-specific patterns before enabling Semble.
- Pairing SessionStart advertises Semble only when the current project root has
  the required safety artifact and Semble is available for that session.
- Pairing init never writes repo-specific Semble target roots into
  `~/§BRAND_GLOBAL_DIRNAME§/AGENT_TOOLS.md`.

MAS runtime behavior:

§BRAND_NAME_TITLE§ validates Semble and injects MAS Semble prompt guidance only when
`§BRAND_ENV_PREFIX§_ENABLE_SEMBLE` is truthy, the `semble` CLI is present, offline validation
succeeds, and the target root is safe to index.

During `§BRAND_BINARY_NAME§ init --spec`, a truthy `§BRAND_ENV_PREFIX§_ENABLE_SEMBLE` lets §BRAND_NAME_TITLE§ perform a
controlled init-time prewarm when `semble` is available. The prewarm uses a
temporary one-file fixture outside the project/worktree and may contact
Hugging Face or the local model cache path to populate the Semble/model2vec/Hugging
Face cache. After prewarming, §BRAND_NAME_TITLE§ performs offline validation with
`HF_HUB_OFFLINE=1` against the same fixture shape.

After installing or prewarming Semble, operators should set `HF_HUB_OFFLINE=1`
in the shell or service environment that launches §BRAND_NAME_TITLE§ agents when they want to
guarantee unattended work cannot trigger model downloads. Agent prompt and
SessionStart command examples remain plain `semble ...` commands; offline mode is
an operator/runtime environment choice, not agent routing syntax.

The implementation-owned Semble validation timeout constant applies to both
prewarm and offline validation and defaults to 30 seconds. Semble diagnostics
are bounded diagnostics: §BRAND_NAME_TITLE§ reports concise operator-visible cases such as a
missing executable, model unavailable offline, or Semble execution failure
without dumping raw command output, cache contents, or file contents. Semble
failures are non-blocking for MAS agent spawn; §BRAND_NAME_TITLE§ omits Semble prompt guidance
when Semble is disabled, unavailable, or not offline-ready.

Semble prompt guidance is scoped to explicit local roots. Task agents search
their task worktree root; reviewer agents search the reviewer worktree root; an
orchestrator may search the project root only when runtime and worktree
exclusions make that root safe. §BRAND_NAME_TITLE§ should prefer local paths over remote Git
URL indexing for MAS.

Semble reads physical `.sembleignore` files while walking directories.
`.sembleignore` is directory-scoped, not global, so a task worktree needs its
own file before Semble is advertised there. Git private excludes hide generated
worktree `.sembleignore` files from task diffs, but Git excludes do not replace
the physical `.sembleignore` that Semble reads.

Default generated `.sembleignore` entries exclude §BRAND_NAME_TITLE§ runtime state, generated
indexes, and common credential files:

```gitignore
§BRAND_PROJECT_DIRNAME§/
.worktrees/
stacklit.json
*.scip
.env
.env.*
*.env
credentials.*
secrets.*
*secret*.*
*.pem
*.key
*.p12
*.pfx
*.jks
*_rsa
*_dsa
*_ecdsa
*_ed25519
*.keystore
*.truststore
config/secrets/
**/secrets/
serviceAccountKey.json
*-credentials.json
```

Projects with sensitive source-adjacent files should add project-specific
ignore patterns before enabling Semble.

Explicit non-goals: §BRAND_NAME_TITLE§ does not vendor Semble, implement semantic search
inside §BRAND_NAME_TITLE§, automatically install Semble or Python dependencies, automatically
install or download models outside the controlled init prewarm, run `semble init`,
use Semble MCP in this milestone, rely on remote Git URL indexing for MAS,
make Semble required, replace Stacklit, SCIP, `rg`, `ast-grep`, or direct
source reads, or guarantee semantic result relevance. Semble results are
candidates only; source reads remain the evidence.

### SCIP Search (`config.scip_search`)

`scip-search` is an optional external repository-navigation tool. It is strict
opt-in.

`§BRAND_ENV_PREFIX§_ENABLE_SCIP_SEARCH` is process-local activation, not durable project
state. Values are trimmed and compared case-insensitively:

| Value | Meaning |
|-------|---------|
| `1`, `true` | Enable SCIP activation for the current init/runtime process |
| unset, empty, `0`, `false` | Keep SCIP disabled for the current init/runtime process |

`config.scip_search` is the durable language allowlist written under
`§BRAND_PROJECT_DIRNAME§/state.yaml` by `§BRAND_BINARY_NAME§ init --spec`. It does not activate MAS indexing by
itself; it only limits which detected languages §BRAND_NAME_TITLE§ may index after the
environment gate is truthy.

Use repeated `--scip-search <language>` options during init to set an explicit
allowlist:

```bash
§BRAND_ENV_PREFIX§_ENABLE_SCIP_SEARCH=1 §BRAND_BINARY_NAME§ init --spec goal.md --scip-search go --scip-search typescript --scip-search python
```

Supported `--scip-search <language>` values are exactly `go`, `typescript`, and
`python`. During MAS `§BRAND_BINARY_NAME§ init --spec`, when no explicit `--scip-search` value
is supplied and `§BRAND_ENV_PREFIX§_ENABLE_SCIP_SEARCH` is truthy, §BRAND_NAME_TITLE§ auto-detects supported
languages from git-tracked code and writes the detected allowlist to
`config.scip_search`.

Pairing init behavior:

- Truthy `§BRAND_ENV_PREFIX§_ENABLE_SCIP_SEARCH` during pairing `§BRAND_BINARY_NAME§ init` asks §BRAND_NAME_TITLE§ to
  autodetect repo-specific SCIP indexing roots and install or verify
  project-local `§BRAND_BINARY_NAME§-index.sh` Git hook plumbing for supported languages.
- Repeated `--scip-search <language>` flags restrict which languages pairing init
  considers, but they are not root or working-directory selections.
- Repeated `--scip-search-plan <language>=<values>` flags provide explicit
  pairing hook roots when monorepo autodetection is not the desired plan:
  `go=<module-root>`, `typescript=<cwd>,<project-root>`, or
  `python=<cwd>[,<target-only>]`. Values may be repo-relative or absolute paths
  under the repo root. Repeating the same language adds multiple input roots to
  that language's aggregate index. Overrides replace autodiscovery for that
  language. These overrides are used only for project-local pairing hook
  generation; full workspace init rejects the flag, and the values are not
  persisted to MAS `config.scip_search`. If `--scip-search <language>` is also
  supplied, every override language must be in that allowlist.
- The generated project-local hook writes one final `<language>.scip` file per
  language by running each per-root indexer into a temporary SCIP file and then
  calling `scip-search aggregate-index --project-root <repo>`. This aggregation
  runs even for a single root so final result paths are repo-root relative.
- Pairing lifecycle refresh skips a language when its generated aggregate index
  exists and is newer than all relevant source roots. If any source root is
  newer, §BRAND_NAME_TITLE§ rebuilds that language's temporary indexes and aggregate output.

MAS runtime behavior:

§BRAND_NAME_TITLE§ generates MAS SCIP indexes and injects `scip-search` prompt guidance only
when `§BRAND_ENV_PREFIX§_ENABLE_SCIP_SEARCH` is truthy and `config.scip_search` contains at
least one supported language for the current target root.

`scip-search` and the language indexers are separate external prerequisites.
Installing `scip-search` does not install the indexers:

| Language | Indexer |
|----------|---------|
| `go` | `scip-go` |
| `typescript` | `scip-typescript` |
| `python` | `scip-python` |

Generated task indexes live under the task worktree:

```text
<worktree>/§BRAND_PROJECT_DIRNAME§/scip/
```

Project-root orchestrator indexes live under:

```text
<project_root>/§BRAND_PROJECT_DIRNAME§/scip/
```

Indexes are snapshots generated at controlled lifecycle points. They reflect the
source tree when §BRAND_NAME_TITLE§ created or refreshed them, not later edits made by an
agent during the same task.

Each generated runtime language index is also an aggregate index. §BRAND_NAME_TITLE§ runs one
or more language indexers into temporary SCIP files, then writes the final
`<language>.scip` with `scip-search aggregate-index --project-root <target>`.
Final document paths are relative to the task worktree or project root advertised
to the agent.

Indexing failures degrade gracefully at runtime. If one enabled language fails
to index, §BRAND_NAME_TITLE§ still spawns the agent and omits that failed language from the
`scip-search` prompt guidance. If no index is available, §BRAND_NAME_TITLE§ omits the
`scip-search` prompt section entirely.

Pairing SessionStart and MAS prompts advertise SCIP only when they have explicit
current-session index paths. Agents must not search for default SCIP indexes or
infer paths from global guidance.

Explicit non-goals: §BRAND_NAME_TITLE§ does not build, vendor, auto-install, daemonize, watch,
cache, or wrap `scip-search` or its language indexers. Operators install and
maintain `scip-search`, `scip-go`, `scip-typescript`, and `scip-python`
separately.

### Functional Clusters (`§BRAND_ENV_PREFIX§_ENABLE_FUNCTIONAL_CLUSTERS`)

`functional-clusters` is an optional external repository-analysis tool. It is
strict opt-in and consumes §BRAND_NAME_TITLE§-generated artifacts when Stacklit and SCIP
prerequisites are also active.

`§BRAND_ENV_PREFIX§_ENABLE_FUNCTIONAL_CLUSTERS` is process-local activation, not durable
project state. Values are trimmed and compared case-insensitively:

| Value | Meaning |
|-------|---------|
| `1`, `true` | Enable Functional Clusters artifact generation, refresh, and prompt guidance when prerequisites are available |
| unset, empty, `0`, `false` | Keep Functional Clusters disabled for the current process |

The standard artifact path is target-local:

```text
<project_root>/functional-clusters.json
<worktree_root>/functional-clusters.json
```

Refresh requires all of these to be true for the current process:

- `§BRAND_ENV_PREFIX§_ENABLE_FUNCTIONAL_CLUSTERS` is truthy.
- `§BRAND_ENV_PREFIX§_ENABLE_STACKLIT` is truthy and `stacklit.json` is available for the target.
- `§BRAND_ENV_PREFIX§_ENABLE_SCIP_SEARCH` is truthy and `config.scip_search` allows at least one detected SCIP language.

§BRAND_NAME_TITLE§ refreshes Functional Clusters after Stacklit and SCIP. The build uses
temporary exports in this order:

```bash
stacklit export-architecture -i stacklit.json -o <tmp>/stacklit-architecture.json
scip-search graph-export --index <language>.scip -o <tmp>/<language>-scip-graph.json
functional-clusters build --scip-graph <tmp>/<language>-scip-graph.json --stacklit-architecture <tmp>/stacklit-architecture.json -o functional-clusters.json
```

Pairing SessionStart and MAS prompts advertise Functional Clusters only when
`§BRAND_ENV_PREFIX§_ENABLE_FUNCTIONAL_CLUSTERS` is truthy and the target-local
`functional-clusters.json` exists. They supply the explicit artifact path for:

```bash
functional-clusters list --clusters <project_root>/functional-clusters.json
functional-clusters explain --clusters <project_root>/functional-clusters.json '<exact-member-symbol>'
```

The artifact is advisory and may be stale. Agents must verify behavior against
source files before editing or claiming success.

§BRAND_BINARY_NAME§ toolchain can install `functional-clusters`; §BRAND_NAME_TITLE§ does
not infer alternate artifact locations, expose temporary Stacklit architecture or
SCIP graph exports to agents, or make cluster membership authoritative.

### Bash Policy (`§BRAND_ENV_PREFIX§_ENABLE_BASH_POLICY`)

`bash-policy` is an optional standalone CLI that installs provider-aware bash
command policy hooks. `§BRAND_BINARY_NAME§ init` runs `bash-policy init`, then
`bash-policy activation on`, only when `§BRAND_ENV_PREFIX§_ENABLE_BASH_POLICY`
is truthy. Selected providers are delegated to the standalone CLI;
`§BRAND_BINARY_NAME§ toolchain` selects `bash-policy` in the `balanced` and
`full` profiles. §BRAND_NAME_TITLE§ prints a warning and continues when the
executable is missing or either command fails.
§BRAND_NAME_TITLE§ does not vendor or implement the policy engine.

`§BRAND_ENV_PREFIX§_ENABLE_BASH_POLICY` is process-local activation, not durable project state.
Values are trimmed and compared case-insensitively:

| Value | Meaning |
|-------|---------|
| `1`, `true` | Ask `§BRAND_BINARY_NAME§ init` to write `.bash-policy.yaml`, run `bash-policy init`, and run `bash-policy activation on` for selected providers |
| unset, empty, `0`, `false` | Skip all `bash-policy init` and activation calls for the current init process |

When enabled, `§BRAND_BINARY_NAME§ init` runs:

```bash
bash-policy init --provider <selected_provider> --policy-artifact-root <project_root>
bash-policy activation on --provider <selected_provider> --policy-artifact-root <project_root>
```

Pairing init derives the provider from selected agents. Full workspace init
includes Claude by default and adds selected provider flags. If the executable
is missing or either command fails,
`§BRAND_BINARY_NAME§ init` prints a warning and continues with the rest of
initialization.

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

When enabled, agents automatically resume the system at CHECKPOINT and COMPLETED sprint states instead of blocking for manual `§BRAND_BINARY_NAME§ resume`. Defaults to `false`.

- **At init time:** `§BRAND_BINARY_NAME§ init --auto-resume "Goal"`
- **At runtime:** Press `y` in the TUI to toggle

Use `§BRAND_BINARY_NAME§ pause` (or `p` in TUI) for a hard stop — pause is never auto-resumed.

### No Follow-Up (`no_follow_up`)

When enabled, §BRAND_NAME_TITLE§ suppresses top-level `pipeline-transitions` (cross-sub-pipeline transitions). Subpipeline-local transitions still run normally, but cross-sub-pipeline follow-up transitions are not shown in status, auto-executed, or allowed through `§BRAND_BINARY_NAME§ proceed`. Defaults to `false`.

- **At init time:** `§BRAND_BINARY_NAME§ init --no-follow-up "Goal"`
- **State config:** `no_follow_up: true`

### Worktree Setup (`post_worktree_cmd`)

Worktrees are bare checkouts — they lack build artifacts like `node_modules/`, `vendor/`, or compiled outputs. The `post_worktree_cmd` config field specifies a shell command that runs after every worktree creation, ensuring agents have a build-ready workspace.

**Setting it:**

- **At init time:** `§BRAND_BINARY_NAME§ init "Goal" --post-worktree-cmd "npm install"`
- **Auto-detection:** When `--post-worktree-cmd` is not provided, `§BRAND_BINARY_NAME§ init` checks for `package.json` in the project root (and immediate subdirectories for monorepo layouts) and suggests the appropriate install command based on the lockfile. For a single subdirectory (e.g. `web/`), it suggests `cd web && npm install`. For multiple subdirectories, it prompts for manual configuration:

  | Lockfile detected | Suggested command |
  |-------------------|-------------------|
  | `pnpm-lock.yaml` | `pnpm install` |
  | `yarn.lock` | `yarn install` |
  | `bun.lockb` / `bun.lock` | `bun install` |
  | `package-lock.json` (or none) | `npm install` |

- **After merge:** If `post_worktree_cmd` is still unset and a successful merge introduces an unambiguous Node project layout on the integration branch, `§BRAND_BINARY_NAME§ wt-merge` auto-populates the same detected command. Ambiguous layouts still require manual configuration.
- **After init:** Add `post_worktree_cmd: "your command"` to the `config` section of `§BRAND_PROJECT_DIRNAME§/state.yaml`.

**Behavior:** The command runs via `sh -c` in the worktree directory. It is non-fatal — warnings are emitted but worktree creation succeeds even if the command fails.

**Ignored env files:** Worktrees are created from committed files, so untracked `.env` files are not present by default. Set `copy_worktree_env_files: true`, initialize with `§BRAND_BINARY_NAME§ init "Goal" --copy-worktree-env-files`, or set `§BRAND_ENV_PREFIX§_ENABLE_COPY_ENV_FILES=true` during init to explicitly authorize §BRAND_NAME_TITLE§ to copy root-level env files before `post_worktree_cmd` runs. Eligible files are regular files only and must match exactly one of these root-level patterns: `.env`, `.env.*`, `*.env`, `.envrc`. The `*.env` pattern includes names such as `secrets.env` when they are ignored. §BRAND_NAME_TITLE§ verifies the source is ignored, configures the task worktree private exclude, verifies the destination path is ignored, and then copies only when the destination is missing. Unsafe cases are warning-only and path-only; contents are not logged.

`.envrc` is included because direnv setup is commonly required for build/test commands, but it is shell configuration. Enabling this option authorizes copying local shell environment setup as well as env values. Custom names such as `.flaskenv` are not copied in this v1 behavior.

**Coder fallback:** If a coding worktree still lacks declared project dependencies during validation, coder prompts pre-authorize rerunning the configured project-scoped bootstrap command. Coders first query `§BRAND_BINARY_NAME§ get config.post_worktree_cmd --json`; when configured, they may run that command from the task worktree. If absent, they may use only the repo's existing lockfile-preserving install workflow. They must not edit manifests or lockfiles, install global/system tools, or add/upgrade/remove dependencies unless the task scope explicitly requires it.

## System Modes

| Mode | Agents | Heartbeats | Set by |
|------|--------|------------|--------|
| `RUNNING` | Work normally | Yes | `§BRAND_BINARY_NAME§ resume` / `§BRAND_BINARY_NAME§ start` |
| `PAUSED` | Block, don't claim | Yes | `§BRAND_BINARY_NAME§ pause` |
| `STOPPED` | Exit cleanly | Stop | `§BRAND_BINARY_NAME§ stop` |
| `CIRCUIT_BREAKER_TRIPPED` | Halt | Yes | `§BRAND_BINARY_NAME§ analyze` or `§BRAND_BINARY_NAME§ tui` (auto on pattern trigger) |

**PAUSED**: Agents stay alive, resume instantly. Use for manual edits.
**STOPPED**: Agents exit. Must restart manually. Use for end of session.

```
RUNNING <-> PAUSED (§BRAND_BINARY_NAME§ pause / §BRAND_BINARY_NAME§ resume)
RUNNING -> STOPPED (§BRAND_BINARY_NAME§ stop)
STOPPED -> RUNNING (§BRAND_BINARY_NAME§ start, then restart agents)
CIRCUIT_BREAKER_TRIPPED -> RUNNING (§BRAND_BINARY_NAME§ resume, after fixing root cause)
```

When `§BRAND_BINARY_NAME§ tui` triggers the circuit breaker, it also sets `sprint.status` to `CHECKPOINT`.

`§BRAND_BINARY_NAME§ tui` also auto-checkpoints when all non-terminal planned tasks are BLOCKED (sprint stalled), since no agent can make further progress without human intervention.

## Status Diagnostics

`tasks.claimable` and `tasks.reviewable` are aggregate task-readiness counts
across all configured doer and reviewer roles, respectively. Their role-level
breakdowns are `tasks.claimable_by_role` and `tasks.reviewable_by_role`; each is
a deterministic, role-sorted list of `role` and `count` entries, including
configured roles whose count is zero. Each aggregate equals the sum of its
role-level entries. Rejected doer work reserved by an unexpired ownership lease
is not available to the role queue, although its current owner can reclaim it
directly. Unowned work and work whose lease expired are ready again; an assigned
task without `lease_expires` fails closed until repaired. Missing-role repair
uses the same readiness boundary, so it does not start an agent for reserved or
malformed ownership.

The explicit legacy fields preserve the former built-in-role counts:
`tasks.legacy_coder_claimable` reports coder work and
`tasks.legacy_code_reviewer_reviewable` reports code-reviewer work. These fields
and the legacy dashboard/work-queue lines are always present. Their historical
lifecycle-level semantics do not account for rejected-task ownership and can
therefore include work reserved by another agent. Use the aggregate and
role-level fields for pipeline-wide scheduling decisions.

Agent capacity is reported separately under `agent_capacity`: `live` counts
current registrations, `free` counts live, non-degraded agents in `IDLE`, and
`degraded` counts current degraded capacity. `agent_capacity.by_role` provides
the same live/free/degraded values in deterministic role order. These capacity
values describe agents, not task readiness, and never change the claimable or
reviewable task counts.

For a completed sprint with approved but unmerged planning output, the
phase-handoff diagnostic uses `phase_handoff.merge_required`. It lists every
blocking task as a `task_id` and an `action`; run the required
`§BRAND_BINARY_NAME§ wt-merge <task-id>` operator action for each entry.
This merge prerequisite is separate from wake-trigger reporting in
`orchestrator_state.trigger`, so an idle wake trigger does not clear or conceal
the handoff blocker.

Status is read-only: it reports these diagnostics but performs no task or
sprint transition and does not merge planning output.

## Checkpoint Summary

After a successful merge, §BRAND_NAME_TITLE§ auto-invokes the configured default CLI with the
`checkpoint-summary` skill and writes the latest report to
`§BRAND_PROJECT_DIRNAME§/checkpoint-summary.md`. The operation is best-effort: merge success does
not depend on the report being created. Set `auto_checkpoint_summary: false` in
`§BRAND_PROJECT_DIRNAME§/state.yaml` to disable it.

The summary emitter snapshots git status paths and filesystem metadata before
and after the CLI run. Changes outside `§BRAND_PROJECT_DIRNAME§/checkpoint-summary.md` are logged
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

The `--cli` flag on `§BRAND_BINARY_NAME§ agent` and `§BRAND_BINARY_NAME§ repair-agent-pool` selects which coding agent to invoke. When omitted, §BRAND_NAME_TITLE§ first applies the selected structured profile, then falls back to role-specific config (`config.default_doer_cli` for doers and orchestrators, `config.default_reviewer_cli` for reviewers), role-specific env (`§BRAND_ENV_PREFIX§_DEFAULT_DOER_CLI` for doers and orchestrators, `§BRAND_ENV_PREFIX§_DEFAULT_REVIEWER_CLI` for reviewers), `config.default_cli`, `§BRAND_ENV_PREFIX§_DEFAULT_CLI`, then `claude`. Set built-in defaults at init time with `§BRAND_BINARY_NAME§ init --default-cli <cli>`, `§BRAND_BINARY_NAME§ init --default-doer-cli <cli>`, or `§BRAND_BINARY_NAME§ init --default-reviewer-cli <cli>`.

Use `agent_tools` for project-local custom launch definitions. §BRAND_NAME_TITLE§ executes these as structured argv through `exec.Command`; values are not shell-split. `prompt_transport` is `stdin`, `arg`, or `file`. `contract_key` may reuse a known setup provider such as `codex`, or use `none` when §BRAND_NAME_TITLE§ should not suggest a setup command for the custom tool.

```yaml
config:
  default_doer_profile: careful
  agent_profiles:
    careful:
      cli: cursor
      vars:
        model: gpt-5
  agent_tools:
    cursor:
      executable: cursor-agent
      prompt_transport: file
      run_args: ["--cwd", "{{projectRoot}}", "--prompt-file", "{{promptFile}}", "--model", "{{profile.model}}"]
      contract_key: none
```

Preview the resolved command without launching a provider:

```bash
§BRAND_BINARY_NAME§ agent coder --explain-launch
```

Headless watch automatically runs the repair-agent-pool behavior when a task is immediately claimable but no live agent is registered for the required role. This is enabled by default. Set `§BRAND_ENV_PREFIX§_AUTO_REPAIR_AGENT_POOL=0`, `false`, or `no` to disable it. Unset or empty values enable it; other invalid non-empty values also leave it enabled and emit a warning.

| CLI | Notes |
|-----|-------|
| `claude` | Claude Code (fallback default when no config is set) |
| `codex` | OpenAI Codex CLI |
| `codex-acp` | OpenAI Codex through ACPX. Requires the `acpx` executable on the spawned agent's `PATH`; install it with `npm install -g acpx`. §BRAND_NAME_TITLE§ preflights this prerequisite before direct `§BRAND_BINARY_NAME§ agent` execution and before TUI/API agent spawning. `codex-acp` reuses Codex `AGENTS.md` contract setup and runs ACPX with non-interactive auto-approval inside §BRAND_NAME_TITLE§ task worktrees. During `acpx prompt`, streams stdout JSON-RPC and stderr diagnostics to `§BRAND_PROJECT_DIRNAME§/agent-outputs/`, returns parsed message chunks to the supervisor, and logs lifecycle/usage metadata. Short ACPX session control calls are not transcript-logged. |
| `cursor` | Cursor CLI (`cursor-agent`). Use `§BRAND_BINARY_NAME§ setup --provider cursor` for global skills and `§BRAND_BINARY_NAME§ init --provider cursor` for contract setup. Set `§BRAND_ENV_PREFIX§_ENABLE_BASH_POLICY=1` before init when selected providers should receive standalone bash-policy hooks. |
| `cursor-acp` | Cursor through ACPX. Requires `acpx` on `PATH` and an authenticated Cursor CLI (`cursor-agent`). Reuses the shared `AGENTS.md` contract setup and selects the ACPX Cursor target; it is not a Cursor executable name. Use `§BRAND_BINARY_NAME§ init --cursor` for contract setup; it includes the Claude and Codex project setup Cursor relies on. Set `§BRAND_ENV_PREFIX§_ENABLE_BASH_POLICY=1` before init when selected providers should receive standalone bash-policy hooks. |
| `opencode` | OpenCode CLI through `opencode run`. Requires `§BRAND_BINARY_NAME§ setup --opencode` and `§BRAND_BINARY_NAME§ init --opencode` for contract and skill activation. Logged runs add JSON output. |
| `opencode-acp` | OpenCode through ACPX. Requires `acpx` on `PATH`, reuses OpenCode `AGENTS.md` contract setup, and selects the ACPX OpenCode target; it is not an OpenCode executable name. |
| `gemini` | Google Gemini CLI. Marked `disabled: true` in the catalog — informational only, the provider remains detectable and resolvable. |
| `mistral` | Mistral Le Chat CLI. Marked `disabled: true` in the catalog — informational only, the provider remains detectable and resolvable. |
| `kimi` | Kimi (alias to claude with Kimi-specific env vars) |
| `qwen` | Qwen CLI from the remote provider catalog. Use `§BRAND_BINARY_NAME§ setup --provider qwen` and `§BRAND_BINARY_NAME§ init --provider qwen` for contract and skill activation. |
| `qwen-acp` | Qwen through ACPX from the remote provider catalog. Requires `acpx` on `PATH`, reuses Qwen's `QWEN.md` contract setup, and uses catalog-defined ACPX session and prompt argv. |
| `devin` | Devin CLI from the remote provider catalog. Use `§BRAND_BINARY_NAME§ setup --provider devin` for global skills and `§BRAND_BINARY_NAME§ init --provider devin` to link §BRAND_NAME_TITLE§'s contract at the catalog-defined repo path. |
| `devin-acp` | Devin through ACPX from the remote provider catalog. Requires both `acpx` and `devin` on `PATH`; ACPX is invoked with `--agent "devin acp"` because Devin's ACP server is the `devin acp` command, not a standalone executable. Reuses Devin's catalog-defined contract setup. |

## Provider Catalog

§BRAND_NAME_TITLE§ loads provider definitions through a cache at
`~/§BRAND_GLOBAL_DIRNAME§/cache/provider-catalog.yaml` with metadata in
`~/§BRAND_GLOBAL_DIRNAME§/cache/provider-catalog.meta.json`. The default source
is the project raw provider catalog. Override it with
`§BRAND_ENV_PREFIX§_PROVIDER_CATALOG_URL`. Cache freshness defaults to one hour
and can be changed with `§BRAND_ENV_PREFIX§_PROVIDER_CATALOG_TTL`; network
timeout can be changed with `§BRAND_ENV_PREFIX§_PROVIDER_CATALOG_TIMEOUT`.

Use `§BRAND_BINARY_NAME§ providers list`, `§BRAND_BINARY_NAME§ providers detect`,
and `§BRAND_BINARY_NAME§ providers refresh` to inspect and refresh the catalog.
`providers list` outputs four tab-separated columns: provider ID, display name,
backend (`cli` or `acpx`), and disabled (boolean). Synthesized ACP variants
(e.g. `codex-acp`, `cursor-acp`) appear in the list alongside their base
providers. Remote YAML is accepted only after HTTPS fetch (localhost is allowed
for tests) and strict schema validation. Catalog entries describe structured
argv, env-file paths, contract links, and setup assets, not arbitrary shell
scripts. Path fields must stay relative to their intended project or home roots,
and executable names must be bare command names.

**ACP synthesis:** Providers with an `acp_runtime` block automatically
synthesize a virtual `<id>-acp` provider at catalog validation time. The
synthesized provider inherits setup and detection from the base provider and
uses the `acp_runtime` as its runtime. This means `Resolve("codex-acp")` works
without a separate catalog entry. The `disabled` field is informational: it
marks providers that are not yet fully supported, but disabled providers remain
detectable, resolvable, and available for setup.

The provider catalog is a launch trust boundary. Catalog-defined argv can include
provider permission flags such as ACPX `--approve-all` or OpenCode
`--dangerously-skip-permissions`; operators who override
`§BRAND_ENV_PREFIX§_PROVIDER_CATALOG_URL` should treat that source as trusted code
configuration. Explicit `§BRAND_BINARY_NAME§ providers refresh` fails if the
remote catalog cannot be fetched or validated, except for a verified `304 Not
Modified` response that reuses the existing cache and refreshes its metadata.

## Output Logging

`§BRAND_BINARY_NAME§ agent` automatically streams the agent's output to `§BRAND_PROJECT_DIRNAME§/agent-outputs/` while the CLI runs. Stdout is written as `{agent-id}-{timestamp}.txt` and stderr as `{agent-id}-{timestamp}.err`. The directory is created automatically if it does not exist. Pass `--no-log` to disable.

**Secret masking:** Persisted log files are automatically sanitized — environment variable values whose names match sensitive patterns (e.g. `*_API_KEY`, `*_TOKEN`, `*_SECRET`, `*_PASSWORD`, provider-specific keys) are replaced with `***`. Live terminal output is **not** masked, so operators see full output during the session. Values shorter than 8 characters are excluded to avoid false positives.

```bash
§BRAND_BINARY_NAME§ agent coder                  # logging enabled (default)
§BRAND_BINARY_NAME§ agent coder --no-log         # logging disabled
```

Logging is automatically disabled in `-i` (interactive) mode.

## Agent Identity

Agent identity is auto-assigned by default (`coder-1`, or `coder-2` if `coder-1` is active). Override with:

1. **CLI flag**: `§BRAND_BINARY_NAME§ agent coder --agent-id coder-5`
2. **Environment variable**: `export §BRAND_ENV_PREFIX§_AGENT_ID=coder-5`

The `--agent-id` flag takes precedence over `§BRAND_ENV_PREFIX§_AGENT_ID`.
After resolution, `§BRAND_BINARY_NAME§ agent` exports the resolved value as `§BRAND_ENV_PREFIX§_AGENT_ID` to the spawned provider CLI, including `-i` interactive sessions. This environment variable is how SessionStart and guard hooks distinguish §BRAND_NAME_TITLE§ MAS sessions from Pairing sessions. Project env overlays such as `claude.env` cannot override the resolved agent ID.

**Agent ID format**: `{role}-{number}` — e.g. `coder-1`, `code-reviewer-1`, `planner-1`.

**System commands** (`pause`, `stop`, `start`, `resume`, `release-claim`) use `--changed-by` for audit trail (defaults to `human`).

## Environment Variables

Environment variables are process-local fallbacks or identity inputs. Durable
project configuration belongs in `§BRAND_PROJECT_DIRNAME§/state.yaml`.

| Variable | Required | Default | Purpose |
|----------|----------|---------|---------|
| `§BRAND_ENV_PREFIX§_AGENT_ID` | For agent commands | -- | Agent identifier input (format: `{role}-{number}`). `§BRAND_BINARY_NAME§ agent` also exports the resolved ID to spawned provider CLIs so hooks select MAS mode. |
| `§BRAND_ENV_PREFIX§_DISABLE_CLAUDE_SUBAGENTS` | No | unset | Set to `1` to launch Claude Code agents with `--disallowedTools Task`, disabling Claude subagent delegation. Use only when intentionally waiving Claude subagent delegation; agents may be unable to satisfy contract delegation triggers while this is set. |
| `§BRAND_ENV_PREFIX§_ENABLE_BASH_POLICY` | No | unset | Strict opt-in activation gate for standalone bash-policy init. Truthy values write `.bash-policy.yaml`, run `bash-policy init`, and run `bash-policy activation on` for selected providers when the CLI is installed. Full workspace init includes Claude by default and adds selected provider flags. Missing or failing bash-policy setup is warning-only. Unset, empty, `0`, and `false` skip all bash-policy init and activation calls. Values are trimmed and parsed case-insensitively. |
| `§BRAND_ENV_PREFIX§_ENABLE_SCIP_SEARCH` | No | unset | Strict opt-in activation gate for SCIP. In pairing init, truthy values enable project-local hook planning and installation for detected or selected languages. In MAS, truthy values enable indexing and `scip-search` prompt guidance only when `config.scip_search` also allows a detected language. Unset, empty, `0`, and `false` disable it. Values are trimmed and parsed case-insensitively. |
| `§BRAND_ENV_PREFIX§_ENABLE_SEMBLE` | No | unset | Strict opt-in activation gate for Semble. In pairing init, truthy values enable project-root `.sembleignore` safety setup before SessionStart advertisement. In MAS, truthy values enable prewarm/offline validation and prompt guidance only when Semble is installed, offline-ready, and safe for the target root. Unset, empty, `0`, and `false` disable it. Values are trimmed and parsed case-insensitively. |
| `§BRAND_ENV_PREFIX§_ENABLE_STACKLIT` | No | unset | Strict opt-in activation gate for Stacklit. In pairing init, truthy values enable project-local hook setup for repo-root `stacklit.json` refresh. In MAS, truthy values enable target-local `stacklit.json` refresh and prompt guidance when an index is available. Unset, empty, `0`, and `false` disable it. Values are trimmed and parsed case-insensitively. |
| `§BRAND_ENV_PREFIX§_ENABLE_FUNCTIONAL_CLUSTERS` | No | unset | Strict opt-in activation gate for Functional Clusters. Truthy values enable target-local `functional-clusters.json` refresh and prompt guidance when Stacklit and SCIP prerequisites are also active and an artifact is available. Unset, empty, `0`, and `false` disable it. Values are trimmed and parsed case-insensitively. |
| `§BRAND_ENV_PREFIX§_ALLOW_DESTRUCTIVE_DB` | Per marked validation command | unset | Break-glass marker for task or `output[]` validation declared with `destructive_db: true`. It is not a global configuration switch: every destructive DB validation command must start with `§BRAND_ENV_PREFIX§_ALLOW_DESTRUCTIVE_DB=1 ` or `env §BRAND_ENV_PREFIX§_ALLOW_DESTRUCTIVE_DB=1 `. |
| `§BRAND_ENV_PREFIX§_PROVIDER_CATALOG_URL` | No | GitHub raw catalog | Provider catalog source URL. Must be HTTPS except localhost test URLs. |
| `§BRAND_ENV_PREFIX§_PROVIDER_CATALOG_TTL` | No | `1h` | Cache freshness duration for `~/§BRAND_GLOBAL_DIRNAME§/cache/provider-catalog.yaml`. |
| `§BRAND_ENV_PREFIX§_PROVIDER_CATALOG_TIMEOUT` | No | `1500ms` | Network timeout for provider catalog refresh attempts. |
| `§BRAND_ENV_PREFIX§_CODEX_VERSION` | No | unset | Process-local fallback for `config.codex_package_version` when launching headless Codex agents |
| `§BRAND_ENV_PREFIX§_SPECS` | No | `specs/` | Path to specs directory (relative to project root) |
| `§BRAND_ENV_PREFIX§_LOG_LEVEL` | No | `INFO` | Logging verbosity: DEBUG, INFO, WARN, ERROR |

## Making Configuration Changes

1. `§BRAND_BINARY_NAME§ pause --reason "config update"`
2. Use a §BRAND_NAME_TITLE§ command for the config change when one exists. Manual state
   changes are support operations, not the normal config path.
3. `§BRAND_BINARY_NAME§ validate`
4. `§BRAND_BINARY_NAME§ resume`

**Never change state while agents are running** without pausing first.

Do not change a role-pair or transition `task-slug` after related tasks have
been created. Recovery derives deterministic child IDs from the frozen slug;
changing it mid-run can make existing children appear missing.
