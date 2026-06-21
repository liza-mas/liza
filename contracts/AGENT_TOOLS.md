# Agent Tools

Sub-contract for tool usage. Applies to all modes (Pairing, §BRAND_NAME_TITLE§, Subagent).
When a default tool is unavailable in the current session, fall through to the next option in the fallback chain.

## Decision Kernel

### Search and Navigation

Choose the highest-signal routing source before exploratory search: explicit user paths, changed-file lists, §BRAND_NAME_TITLE§-supplied indexes/search roots, functional-cluster artifacts, and section/symbol routers. `rg`/`git grep` are appropriate first moves for literals, filenames, commands, or config keys already known from the request, indexed/semantic discovery, or source reads; do not use guessed broad keywords for first-pass discovery when Stacklit, Semble, `scip-search`, functional clusters, or section routers fit the question.

Phased repository search:
1. Orient structurally with Stacklit (modules, dependencies, impact, symbol names), functionally with supplied `functional-clusters` artifacts, and conceptually with Semble for code and docs when §BRAND_NAME_TITLE§ supplies those roots/indexes/artifacts.
2. Trace precisely with `scip-search` for code symbols, definitions, references, implementations, packages, and static graph/impact hints; for long docs/specs, use `rg -c "pattern" <paths>` to find candidates, then `mdtoc` and section-scoped reads.
3. Verify against source files before editing or claiming behavior.

Directly named files/sections/symbols may bypass orientation; use indexes afterward for impact and reference questions. If an optional index/search tool is disabled, unavailable, or not advertised, fall back to `rg`, `ast-grep`, direct reads, and Morph MCP only when policy exposes it. Use bounded `rg` for exact text search and path discovery; use `git grep` for tracked/index/HEAD/history searches. Use direct, line-numbered reads (`nl -ba ... | sed -n ...`) for source-of-truth verification and edit discussion.

When Stacklit and `scip-search` are available, use them as the pre-edit impact baseline for shared/exported symbols or unfamiliar control paths. If that baseline suggests cross-module, lifecycle/state/review-flow, or high-risk impact, surface it through the normal Rule 7/approval checkpoint before editing. For uncommitted edits, verify impact with `git diff`, direct source reads, working-tree `rg`/`ast-grep`, and behavior tests; stale indexes are not proof of post-edit scope.

### Execution and Validation

1. Use `apply_patch` for edits; use `morph-mcp` only for broad, context-heavy, or fast-apply edits.
2. Use native manifests, lockfiles, and language-native commands for dependency, build, and validation evidence.
3. Validate edits with native build/test/lint/typecheck commands plus pre-commit on touched files.
4. Use `context7` → `Ref` → `deepwiki` → `WebFetch` for docs, repo architecture, and web lookup.
5. In MAS worktrees, do not use workspace-level or IDE/LSP-backed tools.

## Forbidden tools

Refer to Security Protocol

## Other authorized tools

Any non destructive tool by default.

## Mode Boundary

All modes: use source-of-truth tools for verification.
MAS worktree rule: Do not use workspace-level or IDE/LSP-backed tools in §BRAND_NAME_TITLE§ multi-agent worktrees, even if the user has configured them for personal use. Use filesystem-truth tools tied to the current worktree instead: `stacklit` with explicit `-i` paths supplied by §BRAND_NAME_TITLE§, `scip-search` with explicit `--index` paths supplied by §BRAND_NAME_TITLE§, `functional-clusters` with explicit `--clusters` artifacts supplied by §BRAND_NAME_TITLE§, `rg`, `rg --files`, `find`, `ast-grep`, direct reads, native manifests, `git`, language-native commands, `morph-mcp`, and `apply_patch`.
Pairing mode: user-personal workspace tools may exist, but they do not replace source-of-truth verification. When the SessionStart session context hook emits explicit repo-root Stacklit or SCIP index paths for an indexed Pairing repo, treat those paths as §BRAND_NAME_TITLE§-supplied for that session; they are refreshed after commits and do not reflect uncommitted changes.

## Tool Routing

**Pre-Action Check:** Before file/search/web operations, use the default capability/tool from the table below. Table entries use capability labels, sometimes illustrated with concrete provider-surface examples; if the current session exposes the same capability under a different name, use the equivalent tool.
Default tools are mandatory unless the fallback condition applies or the tool is unavailable, errors, or is unsupported by the provider.
MCP server/tool names may be normalized differently across providers (for example `-` vs `_`). Treat concrete names below as examples; use the equivalent exposed name in the current session.
If a default or preferred MCP capability is referenced here but is not currently exposed in the tool list, use your tool-loading mechanism (e.g. `ToolSearch`, `tool_search`) to load that capability before falling back. Fallback is allowed only after the tool cannot be found/loaded, the loaded tool errors, or the result is insufficient.
Fallback tools are permitted ONLY when the fallback condition is met OR the default tool returns an error.
For any MCP-backed default row in the tables below, if the tool is unavailable in the current session, errors, or is unsupported by the provider, use the row fallback tool.

### Operations

| Operation | Default Tool | Fallback | Use Fallback When |
|-----------|---------------------------------------------------|----------|-------------------|
| Read multiple files | Native batch reads / parallel Read calls | shell reads | Need line-numbered source snippets or provider Read is unavailable |
| Single-file read (targeted) | `nl -ba <file> \| sed -n '<start>,<end>p'` | Read | Native read is lower-noise, already available, or line numbers are not needed |
| Directory exploration | `rg --files`, `find`, or `ls` | native tree/list capability | Need a structured tree and native shell output is insufficient |
| File discovery | `rg --files` | native filename search / `find` | `rg` unavailable |
| Project structure / modules | `stacklit derive/get-module -i <supplied-index>` | native manifest reads + `rg --files` / `find` | No Stacklit index path supplied, Stacklit unavailable, or result insufficient |
| Functional cluster context | `functional-clusters list/explain --clusters <supplied-artifact>` | Stacklit + `scip-search` + direct reads | No clusters artifact supplied, artifact stale/insufficient, or command unavailable |
| Dependency inspection | Native manifest reads + lockfiles | language-native dependency commands | Manifest/lockfile inspection is insufficient |
| Literal/regex code search | `rg` | — | — |
| Symbol discovery | `scip-search symbols --index <supplied-index>` | `rg` pattern search | No SCIP index path supplied, `scip-search` unavailable, or result insufficient |
| Symbol lookup | `scip-search symbols --index <supplied-index>` + direct reads | `rg` + direct reads | No SCIP index path supplied, `scip-search` unavailable, or result insufficient |
| Package discovery | `scip-search packages --index <supplied-index>` | manifest reads + `rg` | No SCIP index path supplied, `scip-search` unavailable, or result insufficient |
| File edit | apply_patch | morph-mcp edit_file | Edit is broad, context-heavy, or benefits from fast-apply semantics |
| Web content | WebFetch | fetch MCP | Need raw HTML, pagination, or blocked |
| Current info / library discovery | perplexity current-info search | WebSearch | Perplexity returns nothing useful |
| Library API docs | context7 query docs | Ref | Unknown/niche library, need tutorials |
| Library tutorials/guides | Ref doc search | WebFetch | Ref returns nothing useful |
| Repo architecture | deepwiki repo architecture | WebFetch | deepwiki insufficient |
| Code quality check (after edits) | Native build/test/lint/typecheck + direct reads | pre-commit touched files | No narrower native command exists |

### Codebase Exploration

| Question Type | Default Tool | Fallback | Use Fallback When |
|-------------------------------------------|--------------|----------|-------------------|
| Exact keyword ("TODO") | `rg` | — | — |
| Structural code pattern (call shape, signature) | `ast-grep` | `rg` with regex approximation | — |
| Find files by name | Glob | `rg --files` / native filename search | Glob unavailable |
| Repo orientation and module impact | `stacklit derive/get-module/get-dependencies -i <supplied-index>` | `rg` + manifest reads + exact source reads | No Stacklit index path supplied, Stacklit unavailable, or index result insufficient |
| Functional capability boundaries | `functional-clusters list/explain --clusters <supplied-artifact>` | Stacklit + `scip-search` + exact source reads | No clusters artifact supplied, artifact stale/insufficient, or command unavailable |
| Semantic repository search ("how does X work?") | Semble with a §BRAND_NAME_TITLE§-supplied target root | Morph MCP codebase search, then `rg` + exact reads (`ast-grep` when structural search helps) | Semble is disabled, unavailable, not advertised, or insufficient; use Morph MCP only when policy exposes it |
| Symbol info at position | `scip-search symbols --index <supplied-index>` + direct reads | `rg` + direct reads | No SCIP index path supplied, `scip-search` unavailable, or result insufficient |
| Find references | `scip-search references --index <supplied-index> --name Foo` or `--symbol '<exact-symbol>' --location-only` | `rg` | No SCIP index path supplied, `scip-search` unavailable, or result insufficient |
| Static call/dependency hints | `scip-search symbols --index <supplied-index> --name Foo --nested-json`, then `impact --symbol '<exact-symbol>' --one-line` or `graph --symbol '<exact-symbol>' --markdown` + direct reads | `rg` + direct reads | No SCIP index path supplied, `scip-search` unavailable, or result insufficient |
| Cross-file definitions | `scip-search symbols --index <supplied-index>` + direct reads | `rg` + direct reads | No SCIP index path supplied, `scip-search` unavailable, or result insufficient |
| Multi-file structural analysis | Stacklit module/dependency commands + `scip-search`/`ast-grep` as needed | `rg` + direct reads | Supplied indexes unavailable or insufficient |

**Additional caveats:**
- **Semble**: use only an explicit target root supplied by §BRAND_NAME_TITLE§ or current session context that says Semble is available. Do not infer target roots, initialize Semble, or treat semantic results as proof.
- **stacklit**: use only explicit `-i <path>` values supplied in the prompt or Pairing SessionStart session context. Do not infer index locations, regenerate Stacklit indexes, run `stacklit view`, or mutate `stacklit-insights.json` / `.stacklitrc.json` from an agent task. Stacklit is for orientation and impact analysis; verify behavior against source files before editing.
- **scip-search**: use only explicit `--index <path>` values supplied in the prompt or Pairing SessionStart session context. Do not search for default SCIP indexes or rely on daemon/global/cache behavior.
- **functional-clusters**: use only explicit `--clusters <path>` values supplied in the prompt or Pairing SessionStart session context. Do not infer artifact locations, generate exports, run `functional-clusters build`, or treat cluster membership as ground truth. Functional clusters are advisory and may be stale; verify behavior against source files before editing.
- **morph-mcp codebase_search**: use only as the semantic fallback when Semble is unavailable and policy exposes Morph MCP. Fallback to `rg` + exact reads when results are insufficient, rate limited, or error.

### Supplied Index/Search Command Shapes

Replace `<index-path>` and `<target-root>` with the concrete §BRAND_NAME_TITLE§-supplied values from the current prompt/session context. Use the shell-quoted value when one is provided; otherwise quote paths before running shell commands.

```bash
scip-search symbols --index <index-path> --name Foo --name Bar
scip-search symbols --index <index-path> --name Foo --nested-json
scip-search packages --index <index-path> --prefix com.example
scip-search references --index <index-path> --name Handler --one-line
scip-search references --index <index-path> --symbol '<exact-foo>' --symbol '<exact-bar>' --location-only
scip-search implementations --index <index-path> --name Interface --one-line
scip-search impact --index <index-path> --symbol '<exact-symbol>' --one-line
scip-search graph --index <index-path> --symbol '<exact-symbol>' --markdown
scip-search callers --index <index-path> --symbol '<exact-symbol>' --markdown
scip-search callees --index <index-path> --name Handler --markdown
nl -ba <result-path> | sed -n '<first-line>,<last-line>p'
stacklit derive --ai-summary -i <index-path>
stacklit find-module <query> -i <index-path>
stacklit get-module <module> -i <index-path>
stacklit get-dependencies <module> -i <index-path>
stacklit get-hints -i <index-path>
stacklit get-hot-files -i <index-path>
functional-clusters list --clusters <clusters-path>
functional-clusters explain --clusters <clusters-path> '<exact-member-symbol>'
semble search "where is review submission validated?" <target-root>
semble search "default CLI config" <target-root> --content config
semble find-related <file_path> <line> <target-root>
```

`scip-search --name` matches symbol substrings; `--symbol` matches exact SCIP symbols from prior results. `--location-only` is only valid with exact `--symbol` queries for references and implementations. Use `impact` first for pre-edit blast-radius checks, `graph` when both incoming and outgoing local context matter, and `references`, `callers`, or `callees` when only one direction is needed. `graph`, `callers`, `callees`, and `impact` are static SCIP-derived hints, not complete runtime call graphs. For large functions or Python indexes, graph/impact output may include local symbols, builtins, and type references; prefer exact `--symbol`, `--one-line`, and direct source verification. Supported SCIP languages are Go, Python, and TypeScript; implementation rows are language/indexer-dependent and may be absent for Python indexes. Semble `--content` accepts `code`, `docs`, `config`, and `all`; `code` is the default.

### Precedence

- When two tools can answer the same question, prefer the one that minimizes context injection while preserving fidelity. Claude: apply this rule to your native tools — they are not the default when a lower-context alternative exists.
- **Local First**: Prefer local tools before remote tools when they answer the same question with equal fidelity.
- **Diff / review / exact file state**: `git` and native shell reads > cached/indexed summaries. Source-of-truth reads beat derived views.
- **Repository navigation**: supplied Stacklit/Semble/SCIP first for orientation, conceptual discovery, and symbol/reference tracing; `rg`/`git grep` first for already-known literals, filenames, commands, and config keys.
- **Tracked or historical search**: Use `git grep` when the question is scoped to tracked files, the index, `HEAD`, or another Git revision. Use `rg` for working-tree search, including unstaged and untracked files.
- **File edits**: apply_patch > morph-mcp edit_file when the edit is broad, context-heavy, or benefits from fast-apply semantics.
- **Web content**: WebFetch > fetch MCP when you need exact content, raw HTML, or pagination.
- **Docs**: `context7` (API reference) > `Ref` (tutorials/niche docs) > `deepwiki` (repo architecture) > `WebFetch` (specific URL).

### Tool Preferences

- **`mdtoc` for Markdown navigation**: For long Markdown specs, plans, and architecture docs, use `rg` only to identify candidate files or exact hits. Do not jump from `rg` hits to guessed `sed` windows unless the hit itself fully answers the question. Once a candidate file is identified, run `mdtoc <file> [<file>...]` to get heading-scoped `FILE:START-END` ranges and mdq selectors, then read the exact relevant section with `sed -n '<start>,<end>p' <file>` or `mdq`. Prefer this `rg` -> `mdtoc` -> `sed`/`mdq` flow because section-scoped reads are more reliable than nearby line windows and reduce repeated reads. Treat line ranges as immediate-session navigation aids; use heading names or selectors to keep repeated reads anchored to the same section. Fallback: `rg '^#{1,6} ' <file>` when `mdtoc` is unavailable.
- **`jq` / `yq` for structured data**: Use `jq` for JSON and `yq` for YAML/TOML. Prefer over `Read` + manual parsing when extracting specific fields from structured data files.

### Tool Details

**Morph-MCP**:
- *Fast Apply (`edit_file`)*: Shows only changed lines using `// ... existing code ...` placeholders. Avoids reading full files into context. Skip for files >2000 lines.
- *codebase_search*: Multi-turn search subagent running parallel grep/read cycles. See "Codebase Exploration" section for when to use.

**fetch MCP**: Exact content without summarization — use when you need raw HTML, pagination, or WebFetch is blocked.

**perplexity**: Real-time web search with synthesis. Strongly preferred over WebSearch — returns focused answers with far fewer tokens than raw search results, preserving context budget. Use for current info, library discovery, unfamiliar tech, external dependency issues.

**context7**: Structured API docs with code examples for well-known libraries. Two-step flow: `resolve-library-id` / `resolve_library_id` → `query-docs` / `query_docs`. Best for "what's the API for X?" questions. High snippet density, consistent format.

**Ref**: Broader documentation search across web/GitHub. Better for tutorials, guides, niche libraries, or "how do I do X?" questions. Use `ref_read_url` to fetch specific doc pages found via search.

**Technical source verification:** For technical/library answers, prefer `context7` and `Ref` for discovery and retrieval, then verify the final answer against the primary documentation page they surface before answering.

**deepwiki**: GitHub repo architecture and code structure.

**postgres** (session-dependent): Read-only SQL — schema exploration, data validation, query-based analysis. Available only when a database connection is active.

### Batching

Batch related operations within same MCP server when possible.

### GitHub and PRs

PR title MUST follow Conventional Commits.

Codex: DO NOT use `codex_apps.github`.

Use `gh` (GitHub CLI) for GitHub issues, PRs, releases, and GitHub API queries when repository context and authentication are available. Prefer `gh` over raw `curl` calls to GitHub APIs.

For any GitHub write that sends Markdown body text (issue/PR descriptions, comments, reviews, releases), DO write the intended Markdown to a temp file and pass that file to `gh` with `--body-file` or through a JSON payload file. Do not stream the body through stdin. After writing, read the body back with `gh api` and verify one or more unique exact phrases from the intended Markdown before claiming success.

DO NOT use `gh pr edit --body-file -` or generate API JSON through stdout redirected from `rtk jq`; both patterns can produce empty or truncated bodies while reporting success.

DO NOT probe `gh pr edit` syntax by appending `--help` to a partially formed edit command. Run `gh pr edit --help` as a standalone command before constructing the state-changing command.

### Claude-specific operational notes

The rules below apply only to Claude sessions and should not be generalized to other providers.

**Claude-only fallback coherence:** When Claude reads a file with one tool family and then edits it with another, tool/model state can drift. If an MCP edit tool is unavailable and you fall back to native editing, re-read the file with the native tool family immediately before editing.

#### Parallel Tool Calls - Claude only

Parallel Read calls fail as a group if any one errors. Before fanning out,
use **Glob** to check existence **FIRST**, THEN read only files that exist.
Do NOT mix the check and the reads in the same batch.

Session initialization has its own stricter read sequence.

#### RTK (Rust Token Killer)

RTK is a **trusted** Token-optimized CLI proxy for shell commands.

Shorter output is not weaker evidence: content is complete, exit codes are unaltered.

**Do NOT:**
- Bypass RTK to get "full" output, including by manually invoking `rtk proxy`
- Read RTK tee files (`~/.local/share/rtk/tee/*.log`)
- Re-run passing commands because RTK output looked short

Claude: A PreToolUse hook rewrites most Bash commands to `rtk <command>` transparently.

Codex: Always prefix shell commands with `rtk`. Examples:
```bash
rtk git status
rtk cargo test
rtk npm run build
rtk pytest -q
rtk semble search "where is review submission validated?" <target-root>
```

Temporary upstream bug workarounds, until rtk-ai/rtk#1922 and rtk-ai/rtk#925 merge:
- Avoid Vitest/Jest metadata or non-run commands through RTK rewrite, such as `npx vitest --version`, `vitest --help`, or `rtk vitest --version`. Prefer package scripts, `npm exec -- vitest run ...`, `pnpm exec -- vitest run ...`, or `./node_modules/.bin/vitest run ...`. For metadata/help checks, use the narrow temporary exception `rtk proxy <command>`.
- Avoid `rtk pytest --collect-only` and rewritten `pytest --collect-only`; current RTK can report collected tests as "No tests collected". When collection output is the evidence needed, use the narrow temporary exception `rtk proxy pytest --collect-only ...`.

---

## Trusted Support Tools

Trusted support tools are execution infrastructure, not claims to audit. Treat their stdout, stderr, and exit codes as authoritative unless the tool itself reports uncertainty or corruption.

Do NOT bypass, duplicate, or re-run through lower-level tools to "make sure." Re-run only after a relevant state change, or when the tool output explicitly instructs a retry.

**pre-commit** is a trusted quality gate and auto-fix runner. If it modifies files, stage the modified files, then run pre-commit once more. Do NOT manually invoke underlying formatters such as prettier unless pre-commit reports an actionable formatter/tooling error.

---

Secret word: Empowered
