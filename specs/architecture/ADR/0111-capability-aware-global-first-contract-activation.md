# 111 - Capability-Aware Global-First Contract Activation

## Context and Problem Statement

Liza historically created repo-level instruction links first and used provider
global paths only as brownfield fallbacks. Providers that load both locations
could therefore receive the same `~/.liza/CORE.md` contract twice. A Codex-only
`prefer_global` policy addressed that duplication, but exposed two broader
problems:

- a configured provider home such as `CODEX_HOME` can differ from `$HOME`, so
  deleting the repo link before resolving the active global path can remove the
  only contract the provider loads;
- several catalog fallbacks were assumed rather than documented provider
  instruction paths, making a blanket global-first policy unsafe.

The provider catalog is the authoritative setup-policy boundary established by
ADR-0096, so active-path selection must be represented there rather than in
provider-specific init branches.

## Considered Options

1. **Prefer global for every provider.** Uniform, but unsafe where no documented
   file-based global instruction path exists.
2. **Keep repo-first for every provider.** Reliable locally, but duplicates the
   contract for providers that load both repo and global instruction files.
3. **Capability-aware global-first policy.** Prefer global only when the catalog
   declares a documented global instruction path and enough metadata to resolve
   its active environment-specific location.

## Decision Outcome

Choose **Option 3**.

Catalog schema version 2 describes global contract resolution with:

- `global_fallback`: the default path beneath the user's home directory;
- `global_fallback_env`: an optional environment variable containing the active
  provider configuration root;
- `global_fallback_env_suffix`: the contract path beneath that root;
- `global_fallback_env_expand_home`: whether a provider accepts a literal `~`
  prefix in its configuration root;
- `prefer_global`: whether the resolved global path is authoritative.

Built-in global-first providers are Claude, Codex, OpenCode, Gemini, and Qwen.
Claude respects `CLAUDE_CONFIG_DIR`, Codex respects `CODEX_HOME`, and OpenCode
respects absolute `XDG_CONFIG_HOME` values; relative values are invalid under the
XDG base-directory specification and fall back to `$HOME/.config`. Qwen uses its
global contract for unset, absolute, and `~`-based `QWEN_HOME` values. Relative
`QWEN_HOME` is CWD-dependent, so Liza retains the repo activation instead. Cursor,
Kimi, and Devin remain repo-only
because the catalog does not declare a supported file-based global instruction path for them.
Mistral retains its native prompt configuration path.

Initialization follows a safety-ordered transition:

1. Resolve the active global path from catalog metadata and the process
   environment.
2. Create or verify the managed global symlink.
3. Only after step 2 succeeds, remove a redundant managed repo symlink.
4. If resolution, creation, or verification fails, retain or create the repo
   activation instead.
5. If ordinary repo/global placement remains unavailable, use the catalog's
   local fallback when supported. Privately exclude managed local fallback
   files, including existing correct links.

Placement reports an error when any selected file-based provider has no active
supported link. Both init paths check this result before deploying new hooks or
settings; failed placement leaves prior activation metadata and recorded links
intact. An explicit skip is successful only with an already-active contract.
Startup context names the canonical global CORE path, and the init gate requires
its read before clearing, independently of provider instruction discovery.

User-owned files and unrelated symlinks are never overwritten. A custom provider
that declares both locations without `prefer_global` retains both managed links
and receives the existing duplicate warning.

Legacy version-1 catalogs are accepted. Built-ins using a still-supported
embedded default global path receive missing version-2 placement metadata at
resolution time; explicit operator paths and preferences remain authoritative.
When version 2 removes a built-in provider's unsupported global path, that
capability removal supersedes stale version-1 global metadata so repo activation
remains usable.

Publishing schema version 2 is intentionally incompatible with pre-v2 binaries
whose strict decoder does not recognize the new fields. Normal initialization in
those binaries falls back to their embedded catalog and preserves its prior
behavior, but `liza providers refresh` returns the decode error because forced
refresh does not fall back. Those released binaries also stop receiving remote
provider updates from the version-2 catalog until upgraded; this is a durable
catalog compatibility break, not a one-request fallback.

## Rationale

Global-first activation removes duplicate contract loading without sacrificing
availability. Resolving environment-specific roots before deletion makes the
operation reflect the path the provider actually reads, not merely Liza's `$HOME`
default. Keeping the policy declarative maintains one source of truth for
embedded, published, and future catalog-backed providers.

The policy is capability-aware rather than universal: absence of a documented
global path is treated as a repo-only contract, not an invitation to invent one.
Provider hooks improve initialization enforcement for providers that support
them, but hook availability is not assumed for every provider and is not the
basis for deleting an instruction link.

## Consequences

**Positive:**

- Supported providers receive one authoritative contract link by default.
- Custom configuration roots such as `CODEX_HOME` remain active and safe.
- User files are preserved, including at preferred global paths.
- Published and embedded provider metadata express the same setup policy.
- Repo-only and custom non-prefer providers retain explicit regression coverage.
- Interactive conflict decisions are grouped by repo path and limited to
  destinations currently available to every affected provider. A free preferred
  global path therefore avoids an irrelevant prompt for an occupied repo file.
- The embedded Devin repo filename is rendered from the build-time product name;
  externally supplied catalog paths remain literal operator data.

**Trade-offs:**

- Catalog schema version 2 requires compatibility migration for version-1
  built-ins and an upgrade for older binaries to resume remote catalog updates.
- Initialization records active repo-only provider paths in an atomic file under
  the main repository Git directory. Initialization is serial per repository;
  atomic replacement protects against partial writes, not
  concurrent read-modify-write cycles. Later invocations preserve recorded paths,
  including a prior local fallback when the current placement attempt remains
  unresolved. Once a preferred global path is verified, its prior managed repo
  or local activation is removed only when no other provider owns that path.
  Existing managed links
  that predate this metadata are attributed to a compatible repo-only provider
  when the path has a single catalog claimant. Shared paths require corroborating
  repo-local activation artifacts, avoiding permanent attribution based only on
  catalog membership. Paths shared by providers selected together also remain
  because another provider's global activation can fail at runtime. Malformed,
  semantically invalid, or unsupported-version state degrades to preserve-all
  behavior for the current activation and is left unchanged for recovery. State
  file access and write errors remain fatal.

## Supersedes

- ADR-0009's repo-first activation rationale; its canonical `~/.liza` root
  decision remains active.
- ADR-0050's repo-first/global-fallback ordering; its brownfield no-overwrite
  invariant remains active.
