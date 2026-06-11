# Configuration Reference

System configuration, tuning parameters, and environment variables.

## Global Setup (`liza setup`)

`liza setup` writes Liza's global support files to `~/.liza/`:

- `CORE.md`, `PAIRING_MODE.md`, `MULTI_AGENT_MODE.md`, and other contract files
- `AGENT_TOOLS.md`
- `COLLABORATION_CONTINUITY.md`
- default `pipeline.yaml`
- `skills/`
- `support-docs/`

Bare `liza setup` is the default global install path. It does not require a
provider flag. Provider flags add provider-specific integrations in the user's
CLI config directories. For Claude, Codex, OpenCode, and Gemini, setup creates
skill symlinks under `~/.claude/skills/`, `~/.codex/skills/`,
`~/.config/opencode/skills/`, or `~/.gemini/skills/` pointing to
`~/.liza/skills/`. Mistral/Vibe also gets its prompt link under
`~/.vibe/prompts/`. Project hooks and runtime provider settings are handled by
`liza init`:

```bash
liza setup --claude
liza setup --codex
liza setup --opencode
liza setup --gemini
liza setup --mistral
```

Use `--force` to refresh existing global files after an upgrade. Use
`--agent-tools <path>` to install a custom `AGENT_TOOLS.md` instead of the
embedded default.

## Project Initialization (`liza init`)

Run `liza init` in each project where Liza should activate the contract. The
interactive wizard walks through mode selection, provider selection, and
project-local setup.

Depending on selected providers and options, `liza init` writes or updates:

- root contract discovery files such as `CLAUDE.md`, `AGENTS.md`, and
  `GEMINI.md`, usually as symlinks to `~/.liza/CORE.md`
- project-local provider hooks and settings, such as `.claude/settings.json` and
  `.codex/` hook configuration
- `.claude/hooks/` and `.codex/hooks/` scripts that enforce session
  initialization, inject project context, guard Git usage, route RTK, and catch
  wrong-worktree paths for providers that support hooks
- `.codex/config.toml` and `.codex/hooks.json` for project-local Codex hook
  activation
- global `~/.codex/config.toml` entries for Codex's project root, project `.git`
  directory, support/cache writable roots, and noninteractive workspace baseline
- global fallback contract symlinks such as `~/.claude/CLAUDE.md` or
  `~/.codex/AGENTS.md` when brownfield repo-root files prevent local symlink
  creation. OpenCode uses `~/.config/opencode/AGENTS.md`.
- `.claudeignore` when absent or explicitly refreshed
- `GUARDRAILS.md` when absent
- `.liza/state.yaml`, `.liza/log.yaml`, and `.liza/pipeline.yaml` for a MAS
  workspace
- the configured integration branch for MAS runs
- optional indexing activation artifacts when `LIZA_ENABLE_STACKLIT`,
  `LIZA_ENABLE_SCIP_SEARCH`, or `LIZA_ENABLE_SEMBLE` is enabled

For brownfield projects that already have their own `CLAUDE.md`, `AGENTS.md`,
or `GEMINI.md`, Liza does not overwrite the repo-root file. It uses the
provider global fallback when possible:

| Repo root file | Global fallback |
|---------------|-----------------|
| `CLAUDE.md` | `~/.claude/CLAUDE.md` |
| `AGENTS.md` (Codex) | `~/.codex/AGENTS.md` |
| `AGENTS.md` (OpenCode) | `~/.config/opencode/AGENTS.md` |
| `GEMINI.md` | `~/.gemini/GEMINI.md` |

If both the repo-root file and the global fallback are non-Liza files, `liza
init` warns and skips that provider activation path. If a Liza symlink already
exists at either location, `liza init` reports it and does not create a
duplicate.

## Claude Code Settings

**`.claude/settings.json`** — project-level permissions for Liza CLI commands, skills, git operations, and build commands.

`liza init` writes this file automatically from the embedded [`claude-settings.json`](../internal/embedded/claude-settings.json). The master defines all Liza CLI permissions, skills, and the full set of bash permissions agents need. **Do not hand-craft a subset** — agents will be blocked on any missing permission.

**Key elements:**
- **`enableAllProjectMcpServers`** — enables any project MCP servers (for non-Liza tools like filesystem, etc.)
- **`Bash(liza:*)`** — grants permission for agents to invoke Liza CLI commands
- **`Skill(...)`** — contract skills from `~/.liza/skills/` (installed by `liza setup`)
- **`defaultMode: acceptEdits`** — required for headless agent operation
- **`permissions.additionalDirectories`** — grants access to required non-project directories such as `~/.liza` and `/tmp`

### Two-Layer Architecture

Claude Code unions permissions from global and project settings:

| Layer | File | Managed by | Contains |
|-------|------|-----------|----------|
| **Project** | `<project>/.claude/settings.json` | `liza init` (automatic) | Liza CLI permissions, skills, git/build commands |
| **Global** | `~/.claude/settings.json` | Manual (one-time) | Personal MCP tools (IDE, search, etc.), machine-specific permissions |

The project layer is portable (team-shared). The global layer is machine-specific (personal tools and paths). Neither alone is sufficient — both are needed.

For global setup and project activation, use `liza setup` and `liza init`.

## Codex Project Permissions

**`~/.codex/config.toml`** — global Codex CLI settings.

`liza init --codex` manages the Codex settings Liza needs for unattended
supervisor tasks and pairing mode. It adds or corrects the global Codex
baseline (`gpt-5.5`, noninteractive approvals, high reasoning effort,
workspace-write sandboxing, and network access) and adds the active project root
plus the active project `.git` directory to
`sandbox_workspace_write.writable_roots` so Codex can edit project files and
write Git metadata. It also adds Codex/Liza support directories and user cache
roots to `writable_roots`. If the file already exists, Liza prompts before
merging those entries and preserves unrelated settings.

When launching headless MAS agents, Liza relies on this global Codex
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
before running `liza agent`:

```bash
export LIZA_CODEX_VERSION=0.125.0
```

`codex_package_version` or `LIZA_CODEX_VERSION` makes Liza launch headless
Codex agents through
`npm exec --yes --package @openai/codex@<version> -- codex`.
The state config version takes precedence over the environment fallback.
Interactive `liza agent -i` keeps using the installed Codex binary.

The recommended complete setup shape is:

```toml
model = "gpt-5.5"
approval_policy = "never"
sandbox_mode = "workspace-write"
model_reasoning_effort = "high"
personality = "pragmatic"

[permissions.workspace.network]
enabled = true

[sandbox_workspace_write]
network_access = true
exclude_tmpdir_env_var = false
exclude_slash_tmp = false
writable_roots = [
  "/home/<USER>/.codex",
  "/home/<USER>/.liza",
  "/home/<USER>/.npm",
  "/home/<USER>/.pyenv/shims",
  "/home/<USER>/.cache",
  "/tmp",
  "/path/to/project",
  "/path/to/project/.git",
]
```

`liza init --codex` manages the active project entries and preserves unrelated
settings when merging an existing config.

## OpenCode

`liza setup --opencode` installs Liza skill symlinks under
`~/.config/opencode/skills/`.

`liza init --opencode` activates the Liza contract through the shared
`AGENTS.md` discovery file. It does not write Codex hooks or Codex settings. If
the repository already has a non-Liza `AGENTS.md`, Liza uses the OpenCode
fallback symlink at `~/.config/opencode/AGENTS.md`.

Init also installs Liza's managed `.opencode/tools/exec.ts` compatibility tool.
The tool exposes a simple `exec` schema with required `cmd` plus nullable
optional `workdir` and `timeout_ms` fields. OpenCode agents are instructed to
prefer this tool for shell and file operations, omit optional fields when not
needed, avoid repeating successful commands, inspect command results, and move
to the next Liza protocol step. Liza only overwrites this file when its managed
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

Within Liza, `--cli opencode-acp` is the selector for the ACPX-backed runtime
that targets OpenCode. It is not an OpenCode command name.

For Groq-backed OpenCode runs, prefer a stable tool-calling model such as
Llama 3.3 70B over GPT-OSS 120B until Harmony/tool-call behavior is proven
reliable in this path. Always validate the selected model against a real Liza
task before relying on it for unattended work.

### Troubleshooting

**State file errors:**
- Verify project initialized: `liza validate`
- Check: `ls -la .liza/state.yaml`

**Codex `.git` read-only in linked worktrees:**
- Verify `~/.codex/config.toml` includes the project root and project `.git`
  directory in `sandbox_workspace_write.writable_roots`.
- If the failure is version-specific, pin MAS agents with
  `config.codex_package_version` or temporary `LIZA_CODEX_VERSION`.

## Configuration Matrix

All configuration lives in `.liza/state.yaml` under the `config` section.
Supported CLI names are `claude`, `codex`, `codex-acp`, `opencode`,
`opencode-acp`, `gemini`, `mistral`, and `kimi`.

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
| `post_worktree_cmd` | (none) | — | — | shell cmd | Command run after worktree creation (e.g. `npm install`) |
| `copy_worktree_env_files` | false | — | — | boolean | Explicitly authorize copying ignored root env files into task worktrees |
| `auto_checkpoint_summary` | true | — | — | boolean | Auto-runs checkpoint-summary after successful merges and writes `.liza/checkpoint-summary.md` |
| `scip_search` | (none) | — | — | language list | Durable allowlist of SCIP languages Liza may index when `LIZA_ENABLE_SCIP_SEARCH` is truthy |

## Optional Indexing Activation

[Stacklit](https://github.com/liza-mas/stacklit-cli),
[SCIP Search](https://github.com/liza-mas/scip-search/),
[Semble](https://github.com/MinishLab/semble/), and
[Functional Clusters](https://github.com/liza-mas/functional-clusters) are optional navigation aids.
They are external tools that users install and maintain separately; Liza only
activates prompt/index guidance when the configured gates and runtime checks pass.
Set the corresponding `LIZA_ENABLE_*` environment variable before running the
`liza setup` or `liza init` command that should activate that tool.
Liza separates their activation across setup, pairing init, and MAS runtime:

- `liza toolchain` can install and doctor the local CLI prerequisites and write
  `~/.liza/toolchain/env.sh` with selected `LIZA_ENABLE_*` gates. It does not
  install provider credentials or MCP connectors.
- `liza setup` owns global generic guidance in `~/.liza/AGENT_TOOLS.md`. That
  guidance explains how agents should route to optional tools only when a session
  supplies explicit paths or readiness metadata. It must stay generic and must
  not contain project-specific generated paths, readiness state, or claims that
  optional tools are installed.
- Pairing `liza init` owns project-local activation artifacts. When an optional
  indexing environment gate is truthy during pairing init, Liza installs or
  verifies the project-local hooks, generated-artifact cleanliness, SCIP command
  plans, and Semble safety files needed by pairing SessionStart context.
- MAS runtime owns per-agent prompt activation. MAS prompts include Stacklit,
  SCIP Search, or Semble sections only from target-specific runtime metadata for
  the current project root, task worktree, or reviewer worktree.

Disabled or unavailable optional tools degrade by omission. Liza omits the
unavailable prompt or SessionStart section and agents fall back to direct source
reads, `rg` for exact literals and path discovery, `ast-grep` for syntax-shaped
search, and any semantic fallback tool exposed by the active tool policy.

When Stacklit or SCIP pairing activation is enabled, `liza init` installs a
managed `liza-index.sh` entrypoint in Git's effective hooks directory, respecting
`core.hooksPath`. Managed wrappers invoke it from `post-commit`,
`post-checkout`, `post-merge`, and `post-rewrite` so repo-root Pairing indexes
stay fresh after normal local history changes. The wrappers skip task worktrees
and file-only checkout events. This Git hook plumbing is separate from
Claude/Codex provider hooks.

### Stacklit (`LIZA_ENABLE_STACKLIT`)

`stacklit-cli` is an optional external repository-navigation tool. It is strict
opt-in.

`LIZA_ENABLE_STACKLIT` is process-local activation, not durable project state.
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

Liza does not create or mutate `.stacklitrc.json`. When Stacklit input files
exist, `stacklit generate-json` consumes them naturally while refreshing
`stacklit.json`.

Pairing init behavior:

- Truthy `LIZA_ENABLE_STACKLIT` during pairing `liza init` installs or verifies
  project-local `liza-index.sh` Git hook plumbing that refreshes the repo-root
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
  `~/.liza/AGENT_TOOLS.md`.

MAS runtime behavior:

At runtime, Liza runs `stacklit generate-json -o stacklit.json` at controlled
lifecycle points when `LIZA_ENABLE_STACKLIT` is truthy:

- Orchestrator refreshes `<project_root>/stacklit.json`.
- Task worktree creation, reviewer worktree recovery, and submit-for-review
  refresh `<worktree>/stacklit.json`.

Task-local `stacklit.json` is generated for prompt context only. Liza requires
`stacklit.json` to be either tracked or ignored before task-local generation.
When it is tracked, Liza marks only that task worktree copy as skip-worktree
before regenerating it. When it is ignored, the generated task-local file remains
ignored. Liza rejects the unsafe middle state where `stacklit.json` is neither
tracked nor ignored, preserving the clean task-review invariant.

Stacklit generation failures degrade gracefully at runtime. Liza still spawns
the agent and omits Stacklit prompt guidance when no task-local or project-root
`stacklit.json` is available. If a previously generated project-root index is
available after a failed root refresh, prompts may still include it as an
available repository snapshot; agents are instructed to verify behavior against
source files before editing.

Pairing SessionStart and MAS prompts advertise Stacklit only when they have an
explicit current-session index path. Agents must not infer index locations from
global guidance.

Explicit non-goals: Liza does not install `stacklit-cli`, run `stacklit view`,
generate `stacklit.html`, or curate Stacklit insights. Operators install
Stacklit, commit their curated Stacklit inputs, and choose whether to commit or
ignore generated indexes and insights.

### Semble (`LIZA_ENABLE_SEMBLE`)

Semble is an optional external semantic repository search tool. It is strict
opt-in.

`LIZA_ENABLE_SEMBLE` is process-local activation, not durable project state.
Values are trimmed and compared case-insensitively:

| Value | Meaning |
|-------|---------|
| `1`, `true` | Enable Semble activation for the current init/runtime process when Semble is installed and offline-ready |
| unset, empty, `0`, `false` | Keep Semble disabled for the current init/runtime process |

Pairing init behavior:

- Truthy `LIZA_ENABLE_SEMBLE` during pairing `liza init` ensures the project root
  has a physical `.sembleignore` safety file before pairing SessionStart
  advertises Semble.
- The pairing `.sembleignore` excludes Liza runtime state, generated indexes, and
  common credential patterns. Projects with sensitive source-adjacent files
  should add their own project-specific patterns before enabling Semble.
- Pairing SessionStart advertises Semble only when the current project root has
  the required safety artifact and Semble is available for that session.
- Pairing init never writes repo-specific Semble target roots into
  `~/.liza/AGENT_TOOLS.md`.

MAS runtime behavior:

Liza validates Semble and injects MAS Semble prompt guidance only when
`LIZA_ENABLE_SEMBLE` is truthy, the `semble` CLI is present, offline validation
succeeds, and the target root is safe to index.

During `liza init --spec`, a truthy `LIZA_ENABLE_SEMBLE` lets Liza perform a
controlled init-time prewarm when `semble` is available. The prewarm uses a
temporary one-file fixture outside the project/worktree and may contact
Hugging Face or the local model cache path to populate the Semble/model2vec/Hugging
Face cache. After prewarming, Liza performs offline validation with
`HF_HUB_OFFLINE=1` against the same fixture shape.

After installing or prewarming Semble, operators should set `HF_HUB_OFFLINE=1`
in the shell or service environment that launches Liza agents when they want to
guarantee unattended work cannot trigger model downloads. Agent prompt and
SessionStart command examples remain plain `semble ...` commands; offline mode is
an operator/runtime environment choice, not agent routing syntax.

The implementation-owned Semble validation timeout constant applies to both
prewarm and offline validation and defaults to 30 seconds. Semble diagnostics
are bounded diagnostics: Liza reports concise operator-visible cases such as a
missing executable, model unavailable offline, or Semble execution failure
without dumping raw command output, cache contents, or file contents. Semble
failures are non-blocking for MAS agent spawn; Liza omits Semble prompt guidance
when Semble is disabled, unavailable, or not offline-ready.

Semble prompt guidance is scoped to explicit local roots. Task agents search
their task worktree root; reviewer agents search the reviewer worktree root; an
orchestrator may search the project root only when runtime and worktree
exclusions make that root safe. Liza should prefer local paths over remote Git
URL indexing for MAS.

Semble reads physical `.sembleignore` files while walking directories.
`.sembleignore` is directory-scoped, not global, so a task worktree needs its
own file before Semble is advertised there. Git private excludes hide generated
worktree `.sembleignore` files from task diffs, but Git excludes do not replace
the physical `.sembleignore` that Semble reads.

Default generated `.sembleignore` entries exclude Liza runtime state, generated
indexes, and common credential files:

```gitignore
.liza/
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

Explicit non-goals: Liza does not vendor Semble, implement semantic search
inside Liza, automatically install Semble or Python dependencies, automatically
install or download models outside the controlled init prewarm, run `semble init`,
use Semble MCP in this milestone, rely on remote Git URL indexing for MAS,
make Semble required, replace Stacklit, SCIP, `rg`, `ast-grep`, or direct
source reads, or guarantee semantic result relevance. Semble results are
candidates only; source reads remain the evidence.

### SCIP Search (`config.scip_search`)

`scip-search` is an optional external repository-navigation tool. It is strict
opt-in.

`LIZA_ENABLE_SCIP_SEARCH` is process-local activation, not durable project
state. Values are trimmed and compared case-insensitively:

| Value | Meaning |
|-------|---------|
| `1`, `true` | Enable SCIP activation for the current init/runtime process |
| unset, empty, `0`, `false` | Keep SCIP disabled for the current init/runtime process |

`config.scip_search` is the durable language allowlist written under
`.liza/state.yaml` by `liza init --spec`. It does not activate MAS indexing by
itself; it only limits which detected languages Liza may index after the
environment gate is truthy.

Use repeated `--scip-search <language>` options during init to set an explicit
allowlist:

```bash
LIZA_ENABLE_SCIP_SEARCH=1 liza init --spec goal.md --scip-search go --scip-search typescript --scip-search python
```

Supported `--scip-search <language>` values are exactly `go`, `typescript`, and
`python`. During MAS `liza init --spec`, when no explicit `--scip-search` value
is supplied and `LIZA_ENABLE_SCIP_SEARCH` is truthy, Liza auto-detects supported
languages from git-tracked code and writes the detected allowlist to
`config.scip_search`.

Pairing init behavior:

- Truthy `LIZA_ENABLE_SCIP_SEARCH` during pairing `liza init` asks Liza to
  autodetect a repo-specific SCIP indexing plan and install or verify
  project-local `liza-index.sh` Git hook plumbing for confident languages.
  Ambiguous auto-detected languages are skipped with warnings so optional SCIP
  indexing does not block pairing setup.
- Repeated `--scip-search <language>` flags restrict which languages pairing init
  considers, but they are not root or working-directory selections. Pairing init
  still needs one confident root per explicitly enabled language.
- Repeated `--scip-search-plan <language>=<values>` flags provide explicit
  pairing hook roots when monorepo autodetection would otherwise be ambiguous:
  `go=<module-root>`, `typescript=<cwd>,<project-root>`, or
  `python=<cwd>[,<target-only>]`. Values may be repo-relative or absolute paths
  under the repo root. These overrides are used only for project-local pairing
  hook generation; full workspace init rejects the flag, and the values are not
  persisted to MAS `config.scip_search`. If `--scip-search <language>` is also
  supplied, every override language must be in that allowlist.
- If pairing init finds multiple plausible roots for an explicitly enabled
  language and cannot choose confidently, it reports an ambiguity diagnostic
  instead of writing guessed hook commands.
- If pairing init finds exactly one confident root per non-skipped language, the
  generated project-local hook contains concrete repo-specific indexer commands.
  Those concrete commands belong in the project hook, not in global setup
  guidance.
- Pairing lifecycle refresh skips a SCIP indexer when its generated index exists
  and is newer than the relevant source files. It runs the indexer when the index
  is missing or stale.

MAS runtime behavior:

Liza generates MAS SCIP indexes and injects `scip-search` prompt guidance only
when `LIZA_ENABLE_SCIP_SEARCH` is truthy and `config.scip_search` contains at
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

Pairing SessionStart and MAS prompts advertise SCIP only when they have explicit
current-session index paths. Agents must not search for default SCIP indexes or
infer paths from global guidance.

Explicit non-goals: Liza does not build, vendor, auto-install, daemonize, watch,
cache, or wrap `scip-search` or its language indexers. Operators install and
maintain `scip-search`, `scip-go`, `scip-typescript`, and `scip-python`
separately.

### Functional Clusters (`LIZA_ENABLE_FUNCTIONAL_CLUSTERS`)

`functional-clusters` is an optional external repository-analysis tool. It is
strict opt-in and consumes operator-generated artifacts; Liza does not generate
or refresh them.

`LIZA_ENABLE_FUNCTIONAL_CLUSTERS` is process-local activation, not durable
project state. Values are trimmed and compared case-insensitively:

| Value | Meaning |
|-------|---------|
| `1`, `true` | Enable Pairing SessionStart guidance when the repo-root artifact exists |
| unset, empty, `0`, `false` | Keep Functional Clusters disabled for the current process |

The standard artifact path is:

```text
<project_root>/functional-clusters.json
```

Pairing SessionStart advertises Functional Clusters only when
`LIZA_ENABLE_FUNCTIONAL_CLUSTERS` is truthy and
`<project_root>/functional-clusters.json` exists. It supplies the explicit
artifact path for:

```bash
functional-clusters list --clusters <project_root>/functional-clusters.json
functional-clusters explain --clusters <project_root>/functional-clusters.json '<exact-member-symbol>'
```

The artifact is advisory and may be stale. Agents must verify behavior against
source files before editing or claiming success.

Explicit non-goals: Liza does not install `functional-clusters`, run
`functional-clusters build`, generate SCIP graph exports, generate Stacklit
architecture exports, wire Functional Clusters into Git hooks, auto-refresh
`functional-clusters.json`, infer alternate artifact locations, or inject MAS
prompt guidance for this milestone.

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

**Ignored env files:** Worktrees are created from committed files, so untracked `.env` files are not present by default. Set `copy_worktree_env_files: true`, initialize with `liza init "Goal" --copy-worktree-env-files`, or set `LIZA_ENABLE_COPY_ENV_FILES=true` during init to explicitly authorize Liza to copy root-level env files before `post_worktree_cmd` runs. Eligible files are regular files only and must match exactly one of these root-level patterns: `.env`, `.env.*`, `*.env`, `.envrc`. The `*.env` pattern includes names such as `secrets.env` when they are ignored. Liza verifies the source is ignored, configures the task worktree private exclude, verifies the destination path is ignored, and then copies only when the destination is missing. Unsafe cases are warning-only and path-only; contents are not logged.

`.envrc` is included because direnv setup is commonly required for build/test commands, but it is shell configuration. Enabling this option authorizes copying local shell environment setup as well as env values. Custom names such as `.flaskenv` are not copied in this v1 behavior.

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
| `codex-acp` | OpenAI Codex through ACPX. Requires the `acpx` executable on the spawned agent's `PATH`; install it with `npm install -g acpx`. Liza preflights this prerequisite before direct `liza agent` execution and before TUI/API agent spawning. `codex-acp` reuses Codex `AGENTS.md` contract setup and runs ACPX with non-interactive auto-approval inside Liza task worktrees. During `acpx prompt`, streams stdout JSON-RPC and stderr diagnostics to `.liza/agent-outputs/`, returns parsed message chunks to the supervisor, and logs lifecycle/usage metadata. Short ACPX session control calls are not transcript-logged. |
| `opencode` | OpenCode CLI through `opencode run`. Requires `liza setup --opencode` and `liza init --opencode` for contract and skill activation. Logged runs add JSON output. |
| `opencode-acp` | OpenCode through ACPX. Requires `acpx` on `PATH`, reuses OpenCode `AGENTS.md` contract setup, and selects the ACPX OpenCode target; it is not an OpenCode executable name. |
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
After resolution, `liza agent` exports the resolved value as `LIZA_AGENT_ID` to the spawned provider CLI, including `-i` interactive sessions. This environment variable is how SessionStart and guard hooks distinguish Liza MAS sessions from Pairing sessions. Project env overlays such as `claude.env` cannot override the resolved agent ID.

**Agent ID format**: `{role}-{number}` — e.g. `coder-1`, `code-reviewer-1`, `planner-1`.

**System commands** (`pause`, `stop`, `start`, `resume`, `release-claim`) use `--changed-by` for audit trail (defaults to `human`).

## Environment Variables

Environment variables are process-local fallbacks or identity inputs. Durable
project configuration belongs in `.liza/state.yaml`.

| Variable | Required | Default | Purpose |
|----------|----------|---------|---------|
| `LIZA_AGENT_ID` | For agent commands | -- | Agent identifier input (format: `{role}-{number}`). `liza agent` also exports the resolved ID to spawned provider CLIs so hooks select MAS mode. |
| `LIZA_DISABLE_CLAUDE_SUBAGENTS` | No | unset | Set to `1` to launch Claude Code agents with `--disallowedTools Task`, disabling Claude subagent delegation. Use only when intentionally waiving Claude subagent delegation; agents may be unable to satisfy contract delegation triggers while this is set. |
| `LIZA_ENABLE_SCIP_SEARCH` | No | unset | Strict opt-in activation gate for SCIP. In pairing init, truthy values enable project-local hook planning and installation for detected or selected languages. In MAS, truthy values enable indexing and `scip-search` prompt guidance only when `config.scip_search` also allows a detected language. Unset, empty, `0`, and `false` disable it. Values are trimmed and parsed case-insensitively. |
| `LIZA_ENABLE_SEMBLE` | No | unset | Strict opt-in activation gate for Semble. In pairing init, truthy values enable project-root `.sembleignore` safety setup before SessionStart advertisement. In MAS, truthy values enable prewarm/offline validation and prompt guidance only when Semble is installed, offline-ready, and safe for the target root. Unset, empty, `0`, and `false` disable it. Values are trimmed and parsed case-insensitively. |
| `LIZA_ENABLE_STACKLIT` | No | unset | Strict opt-in activation gate for Stacklit. In pairing init, truthy values enable project-local hook setup for repo-root `stacklit.json` refresh. In MAS, truthy values enable target-local `stacklit.json` refresh and prompt guidance when an index is available. Unset, empty, `0`, and `false` disable it. Values are trimmed and parsed case-insensitively. |
| `LIZA_ENABLE_FUNCTIONAL_CLUSTERS` | No | unset | Strict opt-in activation gate for Functional Clusters. In Pairing SessionStart, truthy values enable guidance only when repo-root `functional-clusters.json` exists. Liza does not generate, refresh, or hook this artifact. Unset, empty, `0`, and `false` disable it. Values are trimmed and parsed case-insensitively. |
| `LIZA_ALLOW_DESTRUCTIVE_DB` | Per marked validation command | unset | Break-glass marker for task or `output[]` validation declared with `destructive_db: true`. It is not a global configuration switch: every destructive DB validation command must start with `LIZA_ALLOW_DESTRUCTIVE_DB=1 ` or `env LIZA_ALLOW_DESTRUCTIVE_DB=1 `. |
| `LIZA_CODEX_VERSION` | No | unset | Process-local fallback for `config.codex_package_version` when launching headless Codex agents |
| `LIZA_SPECS` | No | `specs/` | Path to specs directory (relative to project root) |
| `LIZA_LOG_LEVEL` | No | `INFO` | Logging verbosity: DEBUG, INFO, WARN, ERROR |

## Making Configuration Changes

1. `liza pause --reason "config update"`
2. Use a Liza command for the config change when one exists. Manual state
   changes are support operations, not the normal config path.
3. `liza validate`
4. `liza resume`

**Never change state while agents are running** without pausing first.
