# §BRAND_NAME_TITLE§ Toolchain

`§BRAND_BINARY_NAME§ toolchain` installs and verifies optional local tools used
by §BRAND_NAME_TITLE§ agents and project activation. It does not install
secrets, provider accounts, or MCP credentials.

## Default Install

The default profile is `balanced`:

```bash
§BRAND_BINARY_NAME§ toolchain install --profile balanced --yes
§BRAND_BINARY_NAME§ toolchain configure --profile balanced --write-shell-profile
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
- `bash-policy`

Unchecked by default:

- `functional-clusters` (selected by the `full` profile)
- MCP/provider capabilities such as filesystem, context7, Ref, fetch,
  Perplexity, DeepWiki, Morph, and postgres.

## Select Tools

Use `--include` and `--exclude` for repeatable explicit choices:

```bash
§BRAND_BINARY_NAME§ toolchain install --profile lean --include semble --exclude scip-python --yes
§BRAND_BINARY_NAME§ toolchain install --profile full --dry-run
```

In an interactive terminal, `§BRAND_BINARY_NAME§ toolchain install` opens a checklist unless
`--yes` or `--dry-run` is supplied. In non-interactive environments, pass
`--yes` or `--dry-run`.

## Check Tools

```bash
§BRAND_BINARY_NAME§ toolchain list
§BRAND_BINARY_NAME§ toolchain doctor
§BRAND_BINARY_NAME§ toolchain doctor --tool stacklit
§BRAND_BINARY_NAME§ toolchain doctor --profile full --json
```

`doctor` reports MCP/provider integrations as manual capabilities. Configure
those in the active agent provider or MCP host; §BRAND_NAME_TITLE§ will not write credentials
or provider-specific secrets.

## Configure Activation

```bash
§BRAND_BINARY_NAME§ toolchain configure --profile balanced
source ~/§BRAND_GLOBAL_DIRNAME§/toolchain/env.sh
```

The generated env file adds the selected install directory to `PATH`, exports
selected `§BRAND_ENV_PREFIX§_ENABLE_*` gates, and registers `§BRAND_BINARY_NAME§ completion`
for interactive Bash and Zsh shells before `§BRAND_BINARY_NAME§ init` or agent runtime.
After writing the file, `configure` prints the exact `source .../env.sh`
command to run in the current shell so the selected gates affect subsequent
commands.
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

The `balanced` and `full` profiles install `bash-policy` and export
`§BRAND_ENV_PREFIX§_ENABLE_BASH_POLICY=1`. Source that env file before
`§BRAND_BINARY_NAME§ init` when the project should receive `.bash-policy.yaml`
and standalone bash-policy provider hooks. For the full usage lifecycle, see
your bash-policy distribution's configuration guide.

## Project Activation

After the machine-local toolchain is installed and configured, run normal
project activation:

```bash
source ~/§BRAND_GLOBAL_DIRNAME§/toolchain/env.sh
§BRAND_BINARY_NAME§ init --claude --codex
```

Use `--cursor` when the project should activate Cursor's project-local setup;
it includes the Claude and Codex project setup Cursor relies on.
When `§BRAND_ENV_PREFIX§_ENABLE_BASH_POLICY=1` is set before init, selected
provider hook initialization and activation are delegated to `bash-policy`. If
`bash-policy` is not on `PATH` or setup fails, init warns and continues.

Or use `configure --project` to invoke pairing activation for provider contract
files and optional indexing hooks:

```bash
§BRAND_BINARY_NAME§ toolchain configure --profile balanced --project . --agents claude,codex
```

Project-local activation still belongs to `§BRAND_BINARY_NAME§ init`: Stacklit hooks, SCIP hook
plans, `.sembleignore`, provider symlinks, bash-policy policy files, delegated
provider hooks, and OpenCode exec tools are written there. If
`§BRAND_ENV_PREFIX§_ENABLE_BASH_POLICY` is truthy but the standalone
`bash-policy` CLI is unavailable or setup fails, `§BRAND_BINARY_NAME§ init`
warns and continues; `.bash-policy.yaml` is written before those CLI calls. Global
`AGENT_TOOLS.md` remains generic and must not contain stale project-specific
index paths.

## Install Fallbacks

Most tools are installed through package managers, upstream install scripts,
`go install`, npm, or `uv tool install`. Some upstream install scripts can be
temporarily unavailable even when the source builds cleanly. For `mdtoc`,
`scip-search`, `functional-clusters`, and `bash-policy`,
`§BRAND_BINARY_NAME§ toolchain install` falls back to cloning the official
source repository and running `go install ./cmd/<tool>` into the selected
install directory.

The source fallback requires `git` and `go` on `PATH`.

## Windows

The toolchain installs and activates natively; the differences are in how each
tool arrives.

**Package managers.** `winget`, `scoop` and `choco` are tried before the Unix
managers. They identify packages by publisher-qualified IDs, so a tool is only
installed through them when the catalog states the identifier — otherwise the
step is skipped with the command to run by hand, rather than installing whatever
else answers to the same name. Today that affects `mdq` (`cargo install mdq`)
and `pre-commit` (`uv tool install pre-commit`).

**Install scripts.** Several upstream scripts accept Linux and macOS only. Where
a source fallback exists it takes over automatically, so `mdtoc`, `scip-search`,
`functional-clusters` and `bash-policy` still install — given `git` and `go` on
`PATH`. `rtk` has no source fallback, so it is installed from the native Windows
archive its publisher ships.

**Binary names.** An install script that builds with `go build -o <dir>/<name>`
produces a file with no extension, which Windows cannot resolve through
`PATHEXT`: the install would report success and every later invocation, including
`§BRAND_BINARY_NAME§ toolchain doctor`, would report the tool missing. Installs
into the managed directory are renamed to `<name>.exe`, and any tool that is
neither in that directory nor on `PATH` afterwards is reported as failed.

**Activation.** PowerShell reads none of the POSIX profiles, so
`§BRAND_BINARY_NAME§ toolchain configure` writes `env.ps1` beside `env.sh` and
sources it from `$PROFILE.CurrentUserAllHosts`. Git Bash sessions keep using
`env.sh`; both are wired in the same run.

**Known gaps.** Two tools install on Windows without being usable there, and
nothing here works around either. `bash-policy` does not build: its file locking
calls `syscall.Flock` with no build tags. `scip-python` builds a regular
expression from the platform path separator while its module loads, so a
backslash leaves the pattern unterminated and every invocation — `--version`
included — dies before it starts; `doctor` reports it as failed, and Python
projects have no SCIP index on this platform.
