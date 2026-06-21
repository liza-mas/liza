# Make Liza a white label

I want to make Liza a white label product

## Where is the Liza name visible?

RCA lens: not every visible path is an independent naming source. Some files are
only projection points. For example, `~/.claude/CLAUDE.md` and repo-root
`CLAUDE.md` are symlinks to `~/.liza/CORE.md`; the visible root cause is the
canonical `~/.liza` contract root and the contract content it exposes.

Brand-bearing roots:
- the `liza` CLI binary and command namespace; command namespace is covered by binary/help/usage validation
- canonical install/runtime roots: `~/.liza`, `<project-repo>/.liza`
- environment/config namespace: `LIZA_*`
- upstream identity: `liza-mas/liza`, public URLs, release metadata, installation instructions; module/import paths are structural visibility sources, not presentation surfaces by default
- named tools and skills: `/liza-logs`, `liza-logs`, `liza-index.sh`, `liza-index-hook.sh`, `.liza-hooks`
- Mistral/Vibe prompt identity: `~/.vibe/prompts/liza.md`, `system_prompt_id = "liza"`

Canonical content sources:
- contracts and support files under `~/.liza`
- generated support docs such as `.liza/SUPPORT.md`
- prompt templates that render text such as `You are a Liza ... agent`
- docs, instructions, CLI output, warnings, logs, and hook status messages that mention Liza
- repo-internal absolute GitHub links such as
  `github.com/liza-mas/liza/blob/main/...`; these should use relative links
  when the target is in the same repository

Projection points:
- provider discovery files: `~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`, `~/.config/opencode/AGENTS.md`, `~/.gemini/GEMINI.md`
- repo-root discovery files: `<project-repo>/CLAUDE.md`, `<project-repo>/CLAUDE.local.md`, `<project-repo>/AGENTS.md`, `<project-repo>/GEMINI.md`
- provider-local configuration and hooks under `<project-repo>/.claude` and `<project-repo>/.codex`
- OpenCode bridge files such as `<project-repo>/.opencode/tools/exec.ts`

Rendered runtime/project artifacts:
- `.liza/state.yaml`, `.liza/log.yaml`, `.liza/alerts.log`, `.liza/pipeline.yaml`
- `.liza/agent-prompts/` and `.liza/agent-outputs/`
- `.liza/provider-*` signal files and other runtime artifacts whose names include Liza concepts
- `<project-repo>/GUARDRAILS.md` and `<project-repo>/.claudeignore` when installed from Liza templates
- `<project-repo>/.worktrees/<task-id>` and per-worktree `.liza-hooks/pre-commit`
- generated or advertised index artifacts/instructions such as `stacklit.json`, SCIP indexes, and session-start text like `Liza repository indexes detected`

White-label implication: rename or parameterize the brand-bearing roots and
canonical content first, then regenerate projection points and runtime artifacts.
Changing only the symlink locations hides routes, but leaves the target content
and namespace visible.

## Strategy

Use build-time brand inputs with default values:
- `BRAND_NAME_LOWER=liza`
- `BRAND_NAME_UPPER=LIZA`
- `BRAND_NAME_TITLE=Liza`
- `BRAND_REPO=liza-mas/liza`
- `BRAND_BINARY_NAME=${BRAND_NAME_LOWER}`
- `BRAND_GLOBAL_DIRNAME=.${BRAND_NAME_LOWER}`
- `BRAND_PROJECT_DIRNAME=.${BRAND_NAME_LOWER}`
- `BRAND_ENV_PREFIX=${BRAND_NAME_UPPER}`
- `BRAND_MISTRAL_PROMPT_ID=${BRAND_NAME_LOWER}`
- `BRAND_ARCHIVE_PREFIX=${BRAND_BINARY_NAME}`
- `BRAND_RELEASE_REPO=${BRAND_REPO}`
- `BRAND_RELEASE_BASE_URL=https://github.com/${BRAND_RELEASE_REPO}/releases/download`
- `BRAND_CHECKSUM_BASE_URL=${BRAND_RELEASE_BASE_URL}`
- `BRAND_INSTALL_REPO=${BRAND_REPO}`

For embedded docs, contracts, support docs, configs, hooks, and scripts, replace
end-user-visible literals with source macros:
- `§BRAND_NAME_LOWER§`
- `§BRAND_NAME_UPPER§`
- `§BRAND_NAME_TITLE§`
- `§BRAND_REPO§`
- `§BRAND_BINARY_NAME§`
- `§BRAND_GLOBAL_DIRNAME§`
- `§BRAND_PROJECT_DIRNAME§`
- `§BRAND_ENV_PREFIX§`
- `§BRAND_MISTRAL_PROMPT_ID§`
- `§BRAND_ARCHIVE_PREFIX§`
- `§BRAND_RELEASE_REPO§`
- `§BRAND_RELEASE_BASE_URL§`
- `§BRAND_CHECKSUM_BASE_URL§`
- `§BRAND_INSTALL_REPO§`

Brand input constraints:
- Reject invalid brand inputs before rendering, building, or packaging. The
  failure must name the invalid variable and reason.
- `BRAND_NAME_LOWER` must match `^[a-z][a-z0-9-]*$`.
- `BRAND_NAME_UPPER` and `BRAND_ENV_PREFIX` must match
  `^[A-Z][A-Z0-9_]*$`.
- `BRAND_NAME_TITLE` must be non-empty printable text with no newline.
- `BRAND_BINARY_NAME`, `BRAND_ARCHIVE_PREFIX`, and
  `BRAND_MISTRAL_PROMPT_ID` must match `^[A-Za-z0-9][A-Za-z0-9._-]*$`.
- `BRAND_GLOBAL_DIRNAME` and `BRAND_PROJECT_DIRNAME` must be single directory
  names: no `/`, `\`, NUL, empty value, `.` or `..`.
- `BRAND_REPO`, `BRAND_RELEASE_REPO`, and `BRAND_INSTALL_REPO` must use
  `owner/repo` form with no URL scheme.
- `BRAND_RELEASE_BASE_URL` and `BRAND_CHECKSUM_BASE_URL` must be absolute
  `https://` URLs with no whitespace.
- Do not shell-escape invalid inputs into generated snippets; fail fast.

Surface matrix:

| Surface | Current default | Branding rule | Compatibility | Validation |
|---------|-----------------|---------------|---------------|------------|
| CLI binary and command examples | `liza` | Use `BRAND_BINARY_NAME`, defaulted from `BRAND_NAME_LOWER` | Old binary name is not assumed present for new installs | Built binary name, help text, usage examples, shell snippets |
| Display/product name | `Liza`, `liza`, `LIZA` | Use the three `BRAND_NAME_*` forms | No compatibility concern; text-only | Rendered docs, prompts, CLI output |
| Global install/runtime root | `~/.liza` | Use `BRAND_GLOBAL_DIRNAME`, default `.${BRAND_NAME_LOWER}` | Existing roots are not moved automatically | Fresh setup in temp HOME creates branded root and discovery symlinks target it |
| Project runtime root | `<repo>/.liza` | Use `BRAND_PROJECT_DIRNAME`, default `.${BRAND_NAME_LOWER}` | Existing project state is not moved automatically | Fresh init creates branded root and provider files point to it |
| Env namespace | `LIZA_*` | Use `BRAND_ENV_PREFIX`; Go helpers compose env var names | Legacy `LIZA_*` names remain indefinite compatibility aliases unless a future breaking spec explicitly removes them | Non-default brand build accepts branded vars and warns on legacy aliases |
| Mistral/Vibe prompt identity | `liza.md`, `system_prompt_id = "liza"` | Use `BRAND_MISTRAL_PROMPT_ID` only when the provider prompt identity rule below is satisfied | Existing prompt files are not overwritten without explicit repair; if verification is unavailable, keep the canonical id allowlisted | Setup writes branded prompt path and provider config, or documents the allowlisted canonical provider id |
| Skills, tools, hooks, and generated helper names | `liza-logs`, `liza-index.sh`, `.liza-hooks` | Rename rendered or installed path/name artifacts, not just file contents; source-only paths may stay structural if they are never copied or advertised | Existing generated helpers are left in place unless repair is requested; legacy generated names may be detected as aliases during migration | Generated skill dirs, tool names, scripts, worktree hooks, and advertised helper paths are scanned |
| Repo display links and docs | `liza-mas/liza` | Use `BRAND_REPO` only for display and public project links | Relative links are preferred for same-repo targets | Markdown link scan finds no same-repo absolute GitHub blob links |
| Go module/import paths | `github.com/liza-mas/liza/...` | Keep as structural identity unless the fork changes module path | Allowlisted as non-presentation code identity | Brand scan allowlist includes `go.mod`, `go.sum`, and import paths |
| Install/update/release URLs | `liza-mas/liza`, `liza-*` archives | Use explicit distribution inputs: `BRAND_RELEASE_REPO`, `BRAND_RELEASE_BASE_URL`, `BRAND_CHECKSUM_BASE_URL`, `BRAND_INSTALL_REPO`; `BRAND_ARCHIVE_PREFIX` is the source of archive names | Canonical upstream URLs may stay allowlisted until white-label releases exist | Installer, GoReleaser config, updater URLs, archive names, checksum URLs |

Go build cannot rewrite arbitrary string literals in Go source. `-ldflags -X`
only sets package-level string variables. For Go code, introduce a central
`internal/brand` package with default values:
- `brand.NameLower`
- `brand.NameUpper`
- `brand.NameTitle`
- `brand.Repo`
- `brand.BinaryName`
- `brand.GlobalDirName`
- `brand.ProjectDirName`
- `brand.EnvPrefix`
- `brand.MistralPromptID`
- `brand.ArchivePrefix`
- `brand.ReleaseRepo`
- `brand.ReleaseBaseURL`
- `brand.ChecksumBaseURL`

The package should expose one package-level string variable per build input
that Go code consumes at runtime, so `-ldflags -X` can override every
independently configurable value. Build/render-only inputs do not need Go
variables, but must still be declared above if they can appear as macros.
`BRAND_INSTALL_REPO` is render/shell-only for installer snippets unless Go code
starts consuming it at runtime; it should not appear in `internal/brand` while
unused by Go.

Provider prompt identity rule: before rebranding `system_prompt_id`, verify the
provider source of truth by either provider documentation or an automated smoke
test in a temporary HOME that proves a prompt file named
`${BRAND_MISTRAL_PROMPT_ID}.md` is resolved from matching local config. If
neither verification path is available, branded Mistral/Vibe prompt ids are out
of scope and the canonical `liza` id remains allowlisted as structural provider
identity.

Then replace end-user-visible Go string literals with `brand.*` values or small
formatting helpers. Keep non-user-facing Go module imports as
`github.com/liza-mas/liza/...`; those are structural package identity, not
presentation.

Extend the existing build ldflags to set the brand package variables from env
or Make defaults. `BINARY_NAME` should default from `BRAND_BINARY_NAME`, so the
filesystem binary, help text, and usage examples are consistent. GoReleaser
archive names should use `BRAND_ARCHIVE_PREFIX`; `BINARY_NAME` must not be a
second archive-name authority.

Macro rendering mechanics:
- Render macro sources during `make sync-embedded`, before `go:embed` captures
  `internal/embedded/`.
- Source roots include `contracts/`, `skills/`, `support-docs/`, and directly
  mastered embedded assets under `internal/embedded/` when they contain macros:
  provider JSON/TOML configs, hook scripts, git hooks, OpenCode bridge files,
  guardrails templates, `.claudeignore`, and pipeline defaults.
- Keep raw macro-bearing sources separate from rendered destinations when a file
  needs to be both human-edited and embedded. Embedded consistency tests should
  compare expected rendered output against `internal/embedded/`, not raw macro
  source bytes.
- Prefer simple token replacement over turning every Markdown file into a Go
  template. Use `.tmpl` / `text/template` only where logic or conditionals are
  needed.
- Macro rendering covers file contents only. Directory names, filenames, hook
  paths, copied skill paths, and generated helper names require an explicit
  rename/copy map in the sync or install step.

Existing-install migration policy:
- New installs use branded binary names, roots, env names, hooks, prompt ids,
  and generated provider files.
- Existing global setup is not moved automatically. A repair or migration
  command must be explicit about what it will move or rewrite.
- Existing project runtime state is not moved automatically. Commands should
  detect legacy project roots, explain the migration state, and avoid silently
  splitting runtime state across old and new roots.
- Legacy env vars remain indefinite compatibility aliases unless a future
  breaking spec explicitly removes them. Branded env vars take precedence when
  both are set, and mixed values produce a warning.
- If both legacy and branded roots exist, prefer the branded root and warn that
  the legacy root is ignored unless the user asks to migrate.
- Destructive moves, overwrites, or symlink retargeting require explicit user
  approval.

Validation strategy:
- Build once with non-default brand values and assert `version`, root `--help`,
  every subcommand `--help`, setup/init validation errors, updater messages, and
  representative hook errors use the branded names.
- Assert rendered embedded assets and generated runtime artifacts contain no
  unresolved `§BRAND_...§` macros and no unknown brand macro names.
- Against the non-default brand build/render output, scan rendered end-user
  assets, generated projection points, generated project artifacts, provider
  hooks/configs, worktree hook artifacts, and the CLI outputs enumerated above
  for raw `Liza`, `LIZA`, `liza`, and `liza-mas/liza` outside an explicit
  allowlist.
- Run setup in a temporary HOME and init in a temporary repository, then scan
  all provider discovery files from the projection-points inventory, including
  `~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`,
  `~/.config/opencode/AGENTS.md`, and `~/.gemini/GEMINI.md`; repo-root
  discovery files; provider-local config; generated support docs; runtime
  directories; prompt files; and worktree hooks.
- Define the allowlist at category level before implementation: Go module path
  and imports, `go.mod`/`go.sum`, license or attribution text, historical
  archived specs/plans, tests intentionally asserting legacy compatibility, and
  release/install/update URLs intentionally pinned to canonical upstream.
- Treat the brand scan as literal and case-sensitive for the listed forms. Fail
  on unresolved known tokens and on any `§BRAND_...§` token not declared above.
- Fail if the macro delimiter `§` appears in macro source content outside a
  declared brand macro token.
- Keep repo-internal GitHub links relative when the target is in this repository.
