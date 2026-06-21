# Customizing `AGENT_TOOLS.md`

`~/§BRAND_GLOBAL_DIRNAME§/AGENT_TOOLS.md` is not a sample file to leave untouched. It is the tool
contract agents follow when deciding how to read files, search code, fetch docs,
and fall back when a tool is unavailable.

**If this file does not match your real setup, agents will waste turns, bloat
context, and make worse tool choices by trying tools that are missing, misnamed,
or wrong for the current mode.**

## This Is Critical

Before your first real run:

- Review `~/§BRAND_GLOBAL_DIRNAME§/AGENT_TOOLS.md` against your actual MCP servers, CLI tools, and editor integrations.
- If you use Claude Code, allow the CLI tools and MCP servers you intend to use in `~/.claude/settings.json`; otherwise agents may be routed to tools that Claude is not allowed to call.
- Remove tools and servers you do not have.
- If you have the capability under a different provider or tool name, adapt the
  row to that surface instead of deleting it.
- Adjust precedence so the best available tools are tried first.
- For optional navigation or compression tools, install the CLI surface §BRAND_NAME_TITLE§
  references unless this guide explicitly says otherwise. Do not install or
  enable the tool's MCP server just because the tool offers one.
- Do not accept installer prompts that add generic instructions to `CLAUDE.md`,
  `AGENTS.md`, or other agent contract files. §BRAND_NAME_TITLE§ already provides the
  agent-facing guidance through `~/§BRAND_GLOBAL_DIRNAME§/AGENT_TOOLS.md` and the mode contracts.
- Provide your own file during setup if you already maintain one:
  `§BRAND_BINARY_NAME§ setup --agent-tools ~/my-agent-tools.md`

`§BRAND_BINARY_NAME§ setup` owns this global generic guidance. It should describe routing rules
that are safe for any project and any stack. It should not contain generated
paths for one repository, worktree-specific index locations, Semble target roots,
SCIP index paths, Stacklit index paths, or readiness claims from a past session.

Pairing `§BRAND_BINARY_NAME§ init` owns project-local activation artifacts such as provider
SessionStart hooks, project Git hook plumbing, generated-index cleanliness, SCIP
hook command plans, and Semble safety files. MAS prompts own task/reviewer/root
specific optional-tool metadata. Those mechanisms supply concrete paths or
readiness when they exist; `AGENT_TOOLS.md` should only explain how to use them.

## Expected Tools by Default

The stock `AGENT_TOOLS.md` references these tool surfaces or capabilities. Keep
entries for the ones you actually have, rename equivalent providers to match
your environment, and remove unavailable tools so agents do not waste turns.
You need to install these tools yourself, §BRAND_NAME_TITLE§ won't.

- Compression: [`rtk`](https://github.com/rtk-ai/rtk)
- Structured and Markdown navigation: `mdtoc`,
  [`mdq`](https://github.com/yshavit/mdq),
  [`jq`](https://github.com/jqlang/jq), and
  [`yq`](https://github.com/mikefarah/yq).
- Grep: [`rg`](https://github.com/BurntSushi/ripgrep)
- Code search and rewrite: [`ast-grep`](https://github.com/ast-grep/ast-grep),
  [Semble / `semble`](https://github.com/MinishLab/semble),
  `scip-search`,
  Stacklit / `stacklit`,
  Functional Clusters / `functional-clusters`,
  and [Morph MCP / `morph-mcp`](https://docs.morphllm.com/mcpquickstart).
- Mandatory quality gate:
  [`pre-commit`](https://github.com/pre-commit/pre-commit).
- Documentation, web, and repository lookups:
  [`gh`](https://cli.github.com/),
  [`context7`](https://github.com/upstash/context7),
  [Ref](https://github.com/ref-tools/ref-tools-mcp),
  [DeepWiki](https://deepwiki.com/home),
  [fetch MCP](https://github.com/modelcontextprotocol/servers/tree/main/src/fetch),
  [Perplexity](https://www.perplexity.ai/).
- Session-dependent or provider-specific capabilities:
  [`postgres`](https://www.postgresql.org/), native tree/list or filename search,
  editor/IDE or LSP-backed tools, and provider tool-loading surfaces such as
  `ToolSearch` / `tool_search`.

Activate Stacklit, SCIP Search, Functional Clusters and Semble using:
```bash
export §BRAND_ENV_PREFIX§_ENABLE_STACKLIT=1
export §BRAND_ENV_PREFIX§_ENABLE_SCIP_SEARCH=1
export §BRAND_ENV_PREFIX§_ENABLE_SEMBLE=1
export §BRAND_ENV_PREFIX§_ENABLE_FUNCTIONAL_CLUSTERS=1
```
This should be done before running `§BRAND_BINARY_NAME§ init`.

Scip and Stacklit rely on indexes on every branch (repo root and worktrees) that are updated via git hooks. This slow down the git operations but speed up agents and greatly reduce token consumption.

The `functional-clusters.json` is not refreshed automatically as of today.
You have to do this manually after a major structural change. Script example
(adapt it to the project languages):

```bash
stacklit export-architecture -o stacklit-architecture.json
scip-search graph-export --index go.scip -o go-scip-graph.json
scip-search graph-export --index python.scip -o python-scip-graph.json
scip-search graph-export --index typescript.scip -o typescript-scip-graph.json
functional-clusters build \
  --scip-graph go-scip-graph.json \
  --scip-graph python-scip-graph.json \
  --scip-graph typescript-scip-graph.json \
  --stacklit-architecture stacklit-architecture.json \
  -o functional-clusters.json
```

Semble doesn't rely on an index but on a local LLM model that is automatically downloaded once.
Run `§BRAND_BINARY_NAME§ init --spec` with Semble installed so §BRAND_NAME_TITLE§ can prewarm and
validate the model cache. After installation or prewarm, set `HF_HUB_OFFLINE=1`
in the shell or service environment that launches unattended agents if you want
to prevent model downloads during normal work.

These tools provide navigation candidates, not proof. Agents still validate
against direct source reads. See
[Configuration Reference](CONFIGURATION.md#optional-indexing-activation)
for setup details, safety rules, and non-goals.

## Why It Matters

Agents treat `AGENT_TOOLS.md` as an operational contract:

- Unavailable tools cause repeated failed attempts and fallback churn.
- Suboptimal tools bloat context by injecting more material than the task needs.
- Wrong precedence wastes context and pushes agents toward weaker discovery paths.
- Stale or incompatible indexing tools can return silently wrong answers.
- Project-specific generated paths in global guidance become stale outside the
  session or repository that produced them.

## Multi-Agent Specific Warnings

Some support tools are a poor fit, or outright incompatible, with §BRAND_NAME_TITLE§ multi-agent
mode.

## Optional Index Routing

Stacklit, SCIP Search, Semble, and Functional Clusters are optional. Keep global
guidance conditional:

- Use Semble only when the current session supplies an explicit target root or
  readiness metadata.
- Use `scip-search` only when the current session supplies an explicit
  `--index` path.
- Use Stacklit only when the current session supplies an explicit `-i` index
  path.
- Use Functional Clusters only when the current session supplies an explicit
  `--clusters` artifact path.
- Treat all index and semantic-search results as navigation candidates. Direct
  source reads remain the evidence for edits, reviews, and success claims.

When an optional tool is disabled, unavailable, or not advertised in the current
session, do not require agents to try it first. Route them to direct reads, `rg`
for exact literals and path discovery, `ast-grep` for syntax-shaped search, and
policy-exposed semantic fallback tools only when those tools are available.

### Worktree-Local Semble Search

When §BRAND_NAME_TITLE§ supplies a Semble target root in an agent prompt, use Semble for
natural-language semantic discovery before exact symbols or modules are known.
Examples include "where is this behavior implemented?", "where is this behavior
specified?", and "where is this config/default defined?". Use `--content docs`
for documentation/spec questions and `--content config` for configuration
questions. Semble returns candidate chunks, not proof; always follow with a
direct read or exact source read before editing or claiming behavior.

Do not bake `HF_HUB_OFFLINE=1` into agent-facing Semble command examples here.
Offline mode belongs in the user/operator environment after Semble is installed
or prewarmed; this file should describe routing syntax agents can apply in any
session where §BRAND_NAME_TITLE§ supplies a safe target root.

Semble complements the rest of the worktree-safe stack:

- Use Semble for broad conceptual discovery when it is enabled and offline-ready.
- Use Stacklit for module ownership, dependencies, hot files, and impact maps.
- Use SCIP / `scip-search` for exact symbols, references, callers, and
  implementations, packages, static graph hints, and review/test impact hints
  when §BRAND_NAME_TITLE§ supplies an explicit index path.
- Use Morph MCP semantic/codebase search only as a fallback when Semble is
  unavailable or not offline-ready and the current tool policy exposes Morph MCP.
- Use `rg` for literal strings, exact error messages, config keys, and path
  discovery; do not spray broad common-word conceptual queries through `rg`.
- Use `ast-grep` for syntax-shaped search and rewrite workflows.
- Use direct source reads as the evidence source before edits, reviews, or
  success claims.

Do not infer Semble target roots, search sibling worktrees, run `semble init`,
use Semble remote Git URL indexing from unattended MAS prompts, write one
project's Semble root into global guidance, or treat Semble MCP as part of the
default MAS setup.

When installing Semble for §BRAND_NAME_TITLE§, install the CLI and keep §BRAND_NAME_TITLE§'s own
`AGENT_TOOLS.md` routing as the source of truth. Do not enable a Semble MCP
server by default, and do not accept installer-generated additions to
`CLAUDE.md` or `AGENTS.md`.

### Worktree-Local SCIP Indexes

When §BRAND_NAME_TITLE§ supplies an explicit SCIP index path in an agent prompt, prefer
`scip-search` for indexed repository navigation in that worktree:

- `scip-search symbols --index <path>` for symbol lookup
- `scip-search packages --index <path>` for indexed package discovery
- `scip-search references --index <path>` for references by exact symbol or name
- `scip-search implementations --index <path>` for implementation navigation
  by exact symbol or name when the indexed language exposes implementation rows
- `scip-search callers --index <path>`, `callees`, `graph`, and `impact` for
  static SCIP-derived review hints; verify candidate paths with direct reads

This is the MAS-safe path for symbol/package/reference/implementation/graph questions
because the query is tied to a caller-supplied worktree index instead of an IDE,
LSP, or branch-global index that may describe another checkout.

Keep `rg` as the default for exact text search and path discovery, including
`rg --files`. Keep `ast-grep` as the default for syntax-pattern structural search
and rewrite workflows, such as finding call shapes, function signatures, or
struct literals that an index query cannot express.

Agents should not search for default SCIP indexes, infer index locations from
worktree paths, or rely on `scip-search` daemon, global-index, cache, watch, or
auto-discovery behavior. Treat the explicit `--index <path>` supplied by §BRAND_NAME_TITLE§ as
the authority. If no explicit index path is available, fall back to `rg`,
`ast-grep`, exact reads, and other worktree-safe tools. Do not copy concrete
SCIP paths from one project or worktree into global guidance examples.

### Worktree-Local Stacklit Indexes

When §BRAND_NAME_TITLE§ supplies an explicit Stacklit index path in an agent prompt, use
`stacklit` for low-token repository orientation before opening files:

- `stacklit derive --ai-summary -i <path>` for a compact module/dependency/hints map
- `stacklit find-module <query> -i <path>` to locate likely ownership
- `stacklit get-module <module> -i <path>` for files, exports, type
  definitions, dependencies, and activity
- `stacklit get-dependencies <module> -i <path>` for impact checks
- `stacklit get-hints -i <path>` and `stacklit get-hot-files -i <path>` for
  workflow commands and churn hotspots

This is the MAS-safe path for broad codebase navigation because the query is
tied to a caller-supplied worktree or project-root index. Agents should not
infer index locations, regenerate insights, run `stacklit view`, or mutate
`stacklit-insights.json` / `.stacklitrc.json`. Treat the explicit `-i <path>`
supplied by §BRAND_NAME_TITLE§ as the authority. Do not copy concrete Stacklit paths from one
project or worktree into global guidance examples.

### Functional Cluster Artifacts

When §BRAND_NAME_TITLE§ supplies an explicit functional-clusters artifact path, use
`functional-clusters` for advisory functional capability context:

- `functional-clusters list --clusters <path>` for a compact cluster overview
- `functional-clusters explain --clusters <path> '<exact-member-symbol>'` to inspect why an
  exact artifact member symbol belongs to a cluster. Cluster labels such as
  `internal/taskkind` are not valid `explain` inputs.

This is useful after Stacklit or SCIP has identified candidate modules or
symbols and before deciding which feature boundary or cross-cluster dependency
to inspect. Functional clusters are generated snapshots and may be stale.
Agents should not infer artifact locations, generate SCIP or Stacklit exports,
run `functional-clusters build`, or treat cluster labels/membership as ground
truth. If no explicit artifact path is supplied, fall back to Stacklit,
`scip-search`, `rg`, `ast-grep`, and direct reads.

### Per-Worktree Servers

Language servers are tied to a specific worktree. In multi-agent mode, §BRAND_NAME_TITLE§ may
run many divergent worktrees at once, so duplicating them across the fleet is
expensive and often impractical.

That makes LSP a poor primary default for divergent multi-agent worktrees, even
if it still remains useful as a fallback or as a pairing-mode tool.

Examples:

- LSP servers such as `gopls`, `pyright`, `tsserver`

Prefer Semble for broad conceptual discovery when §BRAND_NAME_TITLE§ supplies an offline-ready
target root; `scip-search` when §BRAND_NAME_TITLE§ supplies an explicit `--index` path for
indexed symbols, packages, references, and implementations; Stacklit when §BRAND_NAME_TITLE§
supplies an explicit `-i` path for module and dependency orientation; Functional
Clusters when §BRAND_NAME_TITLE§ supplies an explicit `--clusters` path for advisory feature
boundary context; `rg` for exact text/path search; `ast-grep` for syntax-pattern
structural search and rewrites; Morph MCP only as the semantic fallback when
Semble is unavailable and policy exposes it; and direct reads for evidence in
multi-agent worktrees.

### IDE-Specific MCP Tools

IDE-specific MCP tools should be used with care on worktrees. The safe subset is
the one that does not rely on a centralized index tied to a single project state.

If an IDE integration answers from an index that is effectively built for one
open worktree, it becomes stale for divergent worktrees and is a poor fit for
multi-agent use in worktrees.

There is another caveat even when path resolution is correct: IDE-specific MCP
tools may lag in detecting changes made externally by an agent. If the agent
edits files outside the IDE's own write path, IDE-backed reads or project-aware
operations can briefly reflect stale state until refresh or reindex catches up.

### Centralized Indexes

Tools that maintain one shared index for one branch become stale when agents work
in multiple divergent worktrees. That means they can return incorrect results
without obvious errors.

Examples:

- code graph and embedding index tools
- SQLite-backed context stores
- branch-global review indexes

### Session Token-Reduction Tools

Interactive-session token compression tools usually add little in §BRAND_NAME_TITLE§ multi-agent
mode because §BRAND_NAME_TITLE§ already reduces context structurally through blackboard-driven
instructions and headless execution.

Exception:

- `RTK` remains most useful because it compresses tool output at the transport layer

Install RTK as the CLI/proxy that §BRAND_NAME_TITLE§'s tool contract references. If the RTK
installer offers to add instructions to `CLAUDE.md`, `AGENTS.md`, or another
agent prompt file, decline or remove them. §BRAND_NAME_TITLE§ owns RTK guidance in
`AGENT_TOOLS.md`; duplicate vendor instructions can conflict with §BRAND_NAME_TITLE§'s
fallback and mode-specific rules.

## Safer Default Direction For Multi-Agent Use

Prefer tools that remain correct across divergent worktrees:

- Semble with the explicit target root supplied by §BRAND_NAME_TITLE§, for semantic discovery
- `scip-search` with explicit `--index` paths supplied by §BRAND_NAME_TITLE§, for indexed
  symbols, packages, references, and implementations
- Stacklit with explicit `-i` paths supplied by §BRAND_NAME_TITLE§, for module and dependency
  orientation
- `rg` and related search tools
- `ast-grep` for syntax-pattern structural search and rewrite workflows
- workspace-aware tools such as `morph-mcp` when policy exposes them
- `glob`
- direct read / exact read tools and narrowly scoped fetch tools

These constraints are less severe in pairing mode, where one human and one agent
typically share a single worktree.

## Suggested Review Prompt

Before your first serious run, it is worth asking an agent to review your
installed `AGENT_TOOLS.md` against this guide and the tools actually available
in your environment.

Use a prompt like this:

```text
Review ~/§BRAND_GLOBAL_DIRNAME§/AGENT_TOOLS.md against ~/§BRAND_GLOBAL_DIRNAME§/support-docs/CUSTOMIZING_AGENT_TOOLS.md and the
tools actually installed and available in this environment.

Goals:
1. Identify tool rows that should be removed because the capability is not available.
2. Identify rows that should be renamed or adapted because the capability exists under a different provider or tool name.
3. Identify precedence or fallback changes that would reduce wasted turns and context bloat.
4. Flag tools that are a poor default for multi-agent worktrees, especially indexed IDE tools, LSP-heavy flows, or centralized indexes.
5. Confirm whether the safer defaults for worktrees are present: rg, ast-grep where applicable, workspace-aware tools such as morph-mcp, glob, and exact file reads.

Instructions:
- Check the real installed tools, not just the file contents.
- Distinguish unavailable capabilities from equivalent capabilities exposed under different names.
- Prefer recommendations that reduce context injection and stale-state risk.
- Do not edit anything yet.

Output:
- A short findings list ordered by impact.
- A proposed diff for ~/§BRAND_GLOBAL_DIRNAME§/AGENT_TOOLS.md.
- A short rationale for each proposed change.
```
