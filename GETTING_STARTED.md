# Getting Started with Liza

This guide is the first-run path for installing Liza, setting up the global
contract files, activating a project, and choosing Pairing or Multi-Agent mode.

## Requirements

- Unix-like environment. On Windows, use WSL2; native Windows is not supported.
- Git 2.38+ for worktree support.
- A supported coding agent CLI: Claude Code, Codex, OpenCode, Kimi, Mistral,
  or Gemini.
  Claude and Codex are the recommended providers.
- `ripgrep` (`rg`), used by contracts and skills as the default code search
  tool.
- Go 1.25.5+ only when building from source. Pre-built binaries are available
  through `install.sh`.

## Install Liza

Quick install for the latest release on macOS/Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/liza-mas/liza/main/install.sh | bash
```

Install options:

```bash
# Specific version
curl -fsSL https://raw.githubusercontent.com/liza-mas/liza/main/install.sh | VERSION=v1.0.0 bash

# Build from a branch, requires Go and make
curl -fsSL https://raw.githubusercontent.com/liza-mas/liza/main/install.sh | BRANCH=main bash

# Custom install directory
curl -fsSL https://raw.githubusercontent.com/liza-mas/liza/main/install.sh | INSTALL_DIR=~/.local/bin bash
```

From a local clone:

```bash
git clone https://github.com/liza-mas/liza.git
cd liza
make install
```

Verify the binary:

```bash
liza version
```

## Global Setup

Run setup once per machine:

```bash
liza setup
```

Bare `liza setup` writes contracts, skills, support docs, default pipeline
configuration, and the default `AGENT_TOOLS.md` to `~/.liza/`.

Provider flags add provider-specific integrations in the user's CLI config
directories. For Claude, Codex, OpenCode, and Gemini, setup creates skill
symlinks under `~/.claude/skills/`, `~/.codex/skills/`,
`~/.config/opencode/skills/`, or `~/.gemini/skills/` pointing to
`~/.liza/skills/`. Mistral/Vibe also gets its prompt link under
`~/.vibe/prompts/`. Project hooks and runtime provider settings are handled by
`liza init`:

```bash
liza setup --claude --codex
liza setup --claude --codex --opencode --gemini --mistral
```

Use `--force` after an upgrade when you want to refresh existing global files.
Use `--agent-tools` if you maintain your own tool contract:

```bash
liza setup --agent-tools ~/my-agent-tools.md
```

## Customize Agent Tools

**️️️⚠ Do not skip `AGENT_TOOLS.md` customization before a serious run.**

Agents treat `~/.liza/AGENT_TOOLS.md` as their operational tool contract. If it
names tools you do not have, or routes agents to tools that are wrong for your
environment, agents will waste turns, bloat context, and make weaker decisions.

Review the installed file against your actual MCP servers, CLI tools, and editor
integrations. The detailed guide is
[Customizing AGENT_TOOLS.md](support-docs/CUSTOMIZING_AGENT_TOOLS.md).

The default `AGENT_TOOLS.md` references optional tools that Liza does not
install. Before running agents, either install the CLI tools you intend agents
to use or remove/adapt the corresponding guidance. Leaving instructions for
missing tools is not harmless: agents will follow the contract, spend turns on
unavailable commands or MCP servers, and then recover noisily.

If you keep the default SCIP guidance, install `scip-search` plus the language
indexers for the stacks you want Liza to index. Installing `scip-search` alone
is not enough:

```bash
# scip-search
curl -fsSL https://raw.githubusercontent.com/liza-mas/scip-search/main/install.sh | bash
scip-search --version

# Go
go install github.com/scip-code/scip-go/cmd/scip-go@latest

# TypeScript
npm install -g @sourcegraph/scip-typescript

# Python
npm install -g @sourcegraph/scip-python
```

Enable SCIP before project init when you want Liza to generate indexes:

```bash
export LIZA_ENABLE_SCIP_SEARCH=1
liza init --spec goal.md --scip-search go --scip-search typescript --scip-search python
```

Omit `--scip-search ...` to let Liza autodetect supported languages from
tracked files. In monorepos, start with explicit languages and see the
[Configuration Reference](support-docs/CONFIGURATION.md#scip-search-configscip_search)
for `--scip-search-plan`.

## Initialize a Project

Run `liza init` in each repository where you want Liza contract activation:

```bash
liza init
```

The interactive wizard walks through mode selection, provider selection, and
project-local activation. You can also pass provider flags explicitly:

```bash
liza init --claude --codex
liza init --opencode
```

`liza init` creates project-local contract discovery files, hooks/settings for
selected providers, and Liza project state when a goal is supplied.

For Claude, this writes project-local `.claude/settings.json` and `.claude/hooks/`.
For Codex, it writes project-local `.codex/` hooks and updates global
`~/.codex/config.toml` with the project and `.git` writable roots. Brownfield
fallbacks may also create global contract discovery symlinks such as
`~/.claude/CLAUDE.md` or `~/.codex/AGENTS.md`. For OpenCode, it creates the
shared `AGENTS.md` contract symlink without Codex hooks or settings; brownfield
fallback uses `~/.config/opencode/AGENTS.md`.

For brownfield repositories that already have `CLAUDE.md`, `AGENTS.md`, or
`GEMINI.md`, Liza does not overwrite them. It uses the provider global fallback
when possible, or warns if both the repo file and fallback are unavailable.

## Manual Provider Notes

Claude and Codex are the recommended providers. OpenCode is supported through
`opencode run` for headless agents; ACP server integrations should invoke
OpenCode as `opencode acp`. `liza setup` and `liza init` handle the normal Liza
activation paths. OpenCode support
requires both `liza setup --opencode` and `liza init --opencode`; init also
installs Liza's managed `.opencode/tools/exec.ts` compatibility tool so
OpenCode agents have a simple shell/file-operation tool with tolerant optional
arguments.

When using Groq-backed OpenCode models, prefer a stable tool-calling model such
as Llama 3.3 70B over GPT-OSS 120B until Harmony/tool-call behavior is proven
reliable in this path. Verify the selected model against your real Liza flow.

Gemini and Mistral have weaker instruction-following compatibility than Claude
and Codex. They are not recommended as primary Liza providers. When using them,
verify activation by starting the provider and prompting explicitly that it must
follow the contract.

For Gemini, add `~/.liza` to `~/.gemini/settings.json`:

```json
{
  "context": {
    "includeDirectories": [
      "~/.liza"
    ]
  }
}
```

Mistral through the Vibe CLI may require a filesystem MCP server in
`~/.vibe/config.toml` so it can read `~/.liza` and your workspace:

```toml
[[mcp_servers]]
name = "filesystem"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/home/<USER>/.vibe", "/home/<USER>/Workspace", "/home/<USER>/.liza"]
```

Kimi can be used through Claude CLI once Claude setup is in place. Create a
local `kimi` wrapper adapted to your credential storage and model settings:

```bash
cat > ~/.local/bin/kimi <<'EOF'
#!/bin/bash
source ~/.llm-credentials
ANTHROPIC_BASE_URL=https://api.kimi.com/coding/ ANTHROPIC_API_KEY=$KIMI_API_KEY ANTHROPIC_MODEL='kimi-k2.5' claude "$@"
EOF
chmod +x ~/.local/bin/kimi
```

Then run `kimi`. It uses Claude's config automatically.

For the detailed files and settings that `liza setup` and `liza init` write, see
[Configuration Reference](support-docs/CONFIGURATION.md).

## Choose a Mode

**Pairing mode** is the fastest way to experience the contract. Start your CLI
session in the initialized project and greet the agent. The session should select
Pairing mode automatically, read the contract files, and show the hello
protocol.

Use [Pairing Usage](support-docs/USAGE_PAIRING.md) for the first task cycle,
collaboration modes, approval gates, steering tools, and safety model.

**Adversarial Pairing** adds reviewer agents to Pairing mode without launching
the full autonomous system. Use it for higher-stakes work that benefits from a
doer/reviewer blackboard loop. See
[Adversarial Pairing](support-docs/ADVERSARIAL_PAIRING.md).

**Multi-Agent mode** runs an autonomous spec-to-code pipeline. Initialize it
with a goal/spec:

```bash
liza init "Project goal" --spec specs/vision.md
```

Then run:

```bash
liza tui
```

Read [Multi-Agent Usage](support-docs/USAGE_MULTI_AGENTS.md) before running a
multi-agent pipeline. Liza is a complex system, and the usage guide explains
roles, checkpoints, worktrees, TUI controls, and review flow.

## Common First Commands

```bash
liza setup --claude --codex
liza setup --opencode
liza setup --agent-tools ~/my-agent-tools.md
liza init --claude --codex
liza init --opencode
liza init "Project goal" --spec specs/vision.md
liza tui
liza validate
liza status
```

## Next Reading

- [Pairing Usage](support-docs/USAGE_PAIRING.md)
- [Adversarial Pairing](support-docs/ADVERSARIAL_PAIRING.md)
- [Multi-Agent Usage](support-docs/USAGE_MULTI_AGENTS.md)
- [Configuration Reference](support-docs/CONFIGURATION.md)
- [Troubleshooting](support-docs/TROUBLESHOOTING.md)
- [Recipes](docs/RECIPES.md)
- [Demo](docs/DEMO.md)
