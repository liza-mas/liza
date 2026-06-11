# Liza Toolchain

`liza toolchain` installs and verifies optional local tools that reduce context
usage and improve repository navigation. It does not install secrets, provider
accounts, or MCP credentials.

## Default Install

The default profile is `balanced`:

```bash
liza toolchain install --profile balanced --yes
liza toolchain configure --profile balanced --write-shell-profile
```

Selected by default:

- `rtk`
- `stacklit`
- `scip-search`
- `scip-go`, `scip-typescript`, `scip-python`
- `semble`
- `rg`, `ast-grep`
- `mdtoc`, `mdq`
- `jq`, `yq`
- `gh`
- `pre-commit`

Unchecked by default:

- `functional-clusters`
- MCP/provider capabilities such as filesystem, context7, Ref, fetch,
  Perplexity, DeepWiki, Morph, and postgres.

## Select Tools

Use `--include` and `--exclude` for repeatable explicit choices:

```bash
liza toolchain install --profile lean --include semble --exclude scip-python --yes
liza toolchain install --profile full --dry-run
```

In an interactive terminal, `liza toolchain install` opens a checklist unless
`--yes` or `--dry-run` is supplied. In non-interactive environments, pass
`--yes` or `--dry-run`.

## Check Tools

```bash
liza toolchain list
liza toolchain doctor
liza toolchain doctor --tool stacklit
liza toolchain doctor --profile full --json
```

`doctor` reports MCP/provider integrations as manual capabilities. Configure
those in the active agent provider or MCP host; Liza will not write credentials
or provider-specific secrets.

## Configure Activation

```bash
liza toolchain configure --profile balanced
source ~/.liza/toolchain/env.sh
```

The generated env file adds the selected install directory to `PATH` and exports
selected `LIZA_ENABLE_*` gates before `liza init` or agent runtime.
Installers that build or download binaries directly place them in that directory.
OS package managers still use their normal system prefixes. npm-backed tools use
the parent prefix of an install directory ending in `/bin`; `uv tool` installs use
the selected directory as their tool binary directory.

With `--write-shell-profile`, `configure` adds the env source line to the
current shell's startup files: `.zshrc` for Zsh, `.bashrc` and `.profile` for
Bash, and `.profile` for unknown shells.

`configure` also supports `--agent-tools auto|skip|force`:

- `auto` writes the embedded `AGENT_TOOLS.md` only when missing.
- `skip` leaves `AGENT_TOOLS.md` untouched.
- `force` backs up the existing file to `AGENT_TOOLS.md.bak` before writing the
  embedded default.

Semble offline mode is not asserted automatically. After Semble has been
prewarmed and offline validation succeeds, operators may add:

```bash
export HF_HUB_OFFLINE=1
```

## Project Activation

After the machine-local toolchain is installed and configured, run normal
project activation:

```bash
source ~/.liza/toolchain/env.sh
liza init --claude --codex
```

Or use `configure --project` to invoke pairing activation for provider contract
files and optional indexing hooks:

```bash
liza toolchain configure --profile balanced --project . --agents claude,codex
```

Project-local activation still belongs to `liza init`: Stacklit hooks, SCIP hook
plans, `.sembleignore`, provider symlinks, and OpenCode exec tools are written
there. Global `AGENT_TOOLS.md` remains generic and must not contain stale
project-specific index paths.

## Install Fallbacks

Most tools are installed through package managers, upstream install scripts,
`go install`, npm, or `uv tool install`. Some upstream install scripts can be
temporarily unavailable even when the source builds cleanly. For `mdtoc` and
`scip-search`, `liza toolchain install` falls back to cloning the official
source repository and running `go install ./cmd/<tool>` into the selected
install directory.

The source fallback requires `git` and `go` on `PATH`.
