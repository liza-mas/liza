# Customizing `AGENT_TOOLS.md`

`~/.liza/AGENT_TOOLS.md` is not a sample file to leave untouched. It is the tool
contract agents follow when deciding how to read files, search code, fetch docs,
and fall back when a tool is unavailable.

If this file does not match your real setup, agents will waste turns, bloat
context, and make worse tool choices by trying tools that are missing, misnamed,
or wrong for the current mode.

## This Is Critical

Before your first real run:

- Review `~/.liza/AGENT_TOOLS.md` against your actual MCP servers, CLI tools, and editor integrations.
- If you use Claude Code, allow the CLI tools and MCP servers you intend to use in `~/.claude/settings.json`; otherwise agents may be routed to tools that Claude is not allowed to call.
- Remove tools and servers you do not have.
- If you have the capability under a different provider or tool name, adapt the
  row to that surface instead of deleting it.
- Adjust precedence so the best available tools are tried first.
- Provide your own file during setup if you already maintain one:
  `liza setup --agent-tools ~/my-agent-tools.md`

## Why It Matters

Agents treat `AGENT_TOOLS.md` as an operational contract:

- Unavailable tools cause repeated failed attempts and fallback churn.
- Suboptimal tools bloat context by injecting more material than the task needs.
- Wrong precedence wastes context and pushes agents toward weaker discovery paths.
- Stale or incompatible indexing tools can return silently wrong answers.

## Multi-Agent Specific Warnings

Some support tools are a poor fit, or outright incompatible, with Liza multi-agent
mode.

### Worktree-Local SCIP Indexes

When Liza supplies an explicit SCIP index path in an agent prompt, prefer
`scip-search` for indexed repository navigation in that worktree:

- `scip-search symbols --index <path>` for symbol lookup
- `scip-search packages --index <path>` for indexed package discovery
- `scip-search references --index <path>` for callers and references
- `scip-search implementations --index <path>` for implementation navigation
  when the indexed language supports it

This is the MAS-safe path for symbol/package/reference/implementation questions
because the query is tied to a caller-supplied worktree index instead of an IDE,
LSP, or branch-global index that may describe another checkout.

Keep `rg` as the default for exact text search and path discovery, including
`rg --files`. Keep `ast-grep` as the default for syntax-pattern structural search
and rewrite workflows, such as finding call shapes, function signatures, or
struct literals that an index query cannot express.

Agents should not search for default SCIP indexes, infer index locations from
worktree paths, or rely on `scip-search` daemon, global-index, cache, watch, or
auto-discovery behavior. Treat the explicit `--index <path>` supplied by Liza as
the authority. If no explicit index path is available, fall back to `rg`,
`ast-grep`, exact reads, and other worktree-safe tools.

### Per-Worktree Servers

Language servers are tied to a specific worktree. In multi-agent mode, Liza may
run many divergent worktrees at once, so duplicating them across the fleet is
expensive and often impractical.

That makes LSP a poor primary default for divergent multi-agent worktrees, even
if it still remains useful as a fallback or as a pairing-mode tool.

Examples:

- LSP servers such as `gopls`, `pyright`, `tsserver`

Prefer `scip-search` when Liza supplies an explicit `--index` path for indexed
symbols, packages, references, and implementations; `rg` for exact text/path
search; `ast-grep` for syntax-pattern structural search and rewrites; and
workspace-aware tools such as `morph-mcp` or Task(Explore) for broader semantic
or cross-file analysis in multi-agent worktrees.

### IDE-Specific MCP Tools

IDE-specific MCP tools should be used with care on worktrees. The safe subset is
the one that does not rely on a centralized index tied to a single project state.

If an IDE integration answers from an index that is effectively built for one
open worktree, it becomes stale for divergent worktrees and is a poor fit for
multi-agent use. In multi-agent mode, indexed IDE tools should generally fall
back in worktrees rather than be treated as the default.

There is another caveat even when path resolution is correct: IDE-specific MCP
tools may lag in detecting changes made externally by an agent. If the agent
edits files outside the IDE's own write path, IDE-backed reads or project-aware
operations can briefly reflect stale state until refresh or reindex catches up.

Direct-file IDE tools are a separate case. For example, a JetBrains `read_file`
style tool is generally safer because it reads by path rather than through an
index. Liza's prompt contract already steers shell and git operations toward the
worktree explicitly, but IDE-specific MCP tools still need care when they derive
state from the IDE's project context rather than the current filesystem state.

### Centralized Indexes

Tools that maintain one shared index for one branch become stale when agents work
in multiple divergent worktrees. That means they can return incorrect results
without obvious errors.

Examples:

- code graph and embedding index tools
- SQLite-backed context stores
- branch-global review indexes

### Session Token-Reduction Tools

Interactive-session token compression tools usually add little in Liza multi-agent
mode because Liza already reduces context structurally through blackboard-driven
instructions and headless execution.

Exception:

- `RTK` remains useful for Claude Code because it compresses tool output at the transport layer

## Safer Default Direction For Multi-Agent Use

Prefer tools that remain correct across divergent worktrees:

- `scip-search` with explicit `--index` paths supplied by Liza, for indexed
  symbols, packages, references, and implementations
- `rg` and related search tools
- `ast-grep` for syntax-pattern structural search and rewrite workflows
- workspace-aware tools such as `morph-mcp`
- `glob`
- exact file reads and narrowly scoped fetch tools

These constraints are less severe in pairing mode, where one human and one agent
typically share a single worktree.

## Suggested Review Prompt

Before your first serious run, it is worth asking an agent to review your
installed `AGENT_TOOLS.md` against this guide and the tools actually available
in your environment.

Use a prompt like this:

```text
Review ~/.liza/AGENT_TOOLS.md against ~/.liza/support-docs/CUSTOMIZING_AGENT_TOOLS.md and the
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
- A proposed diff for ~/.liza/AGENT_TOOLS.md.
- A short rationale for each proposed change.
```
