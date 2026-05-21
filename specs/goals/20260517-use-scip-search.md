# Use scip-search as Liza's Recommended Repository Index Tool

## Problem Statement

Liza agents work in dedicated task worktrees and repeatedly use text search to
locate symbols, references, implementations, and package structure before they
can act. This burns context, slows work, and produces noisy tool traces for any
agent that needs to inspect code, not only coders and code reviewers.

Liza needs an opt-in, worktree-safe repository index capability that gives
agents fast structural navigation without depending on IDE indexes, stale MCP
state, semantic search, or a daemon tied to the user's main checkout.

## Target Users

- All MAS agents that need to inspect repository code while planning, coding,
  reviewing, integrating, or validating task scope.
- MAS coders that need to find definitions, references, implementations, and
  packages inside their task worktree.
- MAS reviewers that need fast structural navigation while validating submitted
  changes.
- Orchestrator and supervisor flows that need deterministic task-worktree setup
  before spawning agents.
- Operators who need Liza agents to use the same predictable navigation tools
  across providers and worktrees.

## Goal

Make `scip-search` a highly recommended, strict opt-in Liza tool for structural
repository navigation in MAS worktrees.

When `LIZA_ENABLE_SCIP_SEARCH` is set to a truthy value, Liza should generate
one structural index per detected enabled language during task worktree creation
before spawning the assigned agent. Liza should also regenerate indexes on
`submit-for-review` before spawning reviewer agents and refresh project-root
indexes on orchestrator wakeup. Spawned agents should receive explicit index
paths and concise prompt guidance telling them how to query the indexes with
`scip-search`.

`scip-search` is an external tool recommended by Liza; this goal integrates it
into Liza's init-time validation, `state.yaml` config, worktree lifecycle, base
prompt contract, Claude settings, and README. The tool itself is not implemented
inside the Liza repository.

When this goal is used, `scip-search --help` and `scip-search --version` are
expected to be available in the environment where `liza init --spec` runs.

`scip-search` only reads caller-supplied SCIP index files. It does not generate
indexes, search for default indexes, update indexes, cache indexes, watch the
worktree, compile source, type-check source, or parse custom index formats.
Liza is responsible for generating task-local SCIP files with language-specific
indexers before invoking `scip-search`.

## Source Material

- `../scip-search/README.md`
- <https://github.com/liza-mas/scip-search/>

## MVP Scope

- [ ] Document `scip-search` as a highly recommended Liza MAS tool for
      token-efficient repository navigation.
- [ ] During `liza init --spec`, validate `scip-search` availability by running
      `scip-search --help`.
- [ ] Document `LIZA_ENABLE_SCIP_SEARCH` as the strict opt-in MAS activation
      gate. When unset or false, Liza must not generate SCIP indexes or inject
      the `scip-search` prompt section, even when `config.scip_search` exists.
- [ ] Support repeated `liza init --spec --scip-search <language>` options for
      explicitly selecting enabled SCIP languages.
- [ ] If no `--scip-search` option is provided and `LIZA_ENABLE_SCIP_SEARCH` is
      truthy, auto-detect enabled SCIP languages from git-tracked code.
- [ ] Validate `scip-search` and selected language indexers during
      `liza init --spec`.
- [ ] Add the selected `scip_search` language list to the `state.yaml` `config`
      section when `scip-search` validation succeeds.
- [ ] Use `LIZA_ENABLE_SCIP_SEARCH` plus `config.scip_search` to decide whether
      the supervisor indexes detected task-worktree languages and injects the
      `scip-search` prompt section.
- [ ] During task worktree creation, detect enabled languages, run the
      appropriate SCIP indexers, write task-local indexes, then spawn the
      assigned agent.
- [ ] Regenerate task-worktree indexes during `submit-for-review` before
      spawning reviewer agents.
- [ ] Refresh project-root indexes during orchestrator wakeup before injecting
      `scip-search` guidance into orchestrator prompts.
- [ ] Use `scip-go`, `scip-typescript`, and `scip-python` as the first milestone
      indexers for Go, TypeScript, and Python respectively.
- [ ] Store generated indexes under `<worktree>/.liza/scip/` and ensure that
      path is ignored by the task worktree's git status.
- [ ] Store project-root orchestrator indexes under `<project_root>/.liza/scip/`.
- [ ] Pass index paths explicitly into the spawned agent context instead of
      requiring agents to infer index locations from worktree paths.
- [ ] Inject a concise base-prompt section that explains available indexes,
      snapshot semantics, and the supported `scip-search` command loop.
- [ ] Allow `scip-search` Bash invocations in
      `internal/embedded/claude-settings.json`.
- [ ] List `scip-search` in `README.md` as a highly recommended tool that saves
      agent tokens during repository navigation.
- [ ] Preserve `ast-grep` as the complementary tool for pattern-based structural
      search.
- [ ] Ensure indexing is worktree-local and safe for concurrent task worktrees.

## Required Agent Prompt Contract

When `LIZA_ENABLE_SCIP_SEARCH` is truthy, `config.scip_search` contains at least
one language, and at least one index is generated successfully, agents should
receive terse prompt content equivalent to the following for each successful
index:

~~~text
[language].scip indexes this repo [language] code. Search symbols:
```
scip-search symbols --index <path> --name Foo --name Bar
scip-search references --index <path> --symbol '<exact-foo>' --symbol '<exact-bar>' --location-only
nl -ba <result-path> | sed -n '<first-line>,<last-line>p'
```
--name: substring of a symbol
--symbol: exact symbol
scip-search implementations: same syntax as references

That's the full loop for code exploration: three Bash calls typically replacing
5-10 grep/read round-trips.
Be mindful the index will not reflect your changes as you make them.
~~~

For Python indexes, append:

```text
scip-search implementations is not supported for python.
```

For a real prompt, replace `[language].scip` with the explicit absolute index
path supplied by Liza, for example `/path/to/worktree/.liza/scip/go.scip`.

If indexing fails for an enabled language, Liza should still spawn the agent but
omit that language from the prompt. If no index is available, Liza should omit
the `scip-search` prompt section entirely. Indexing failures should remain
observable to operators through logs or command output, but should not consume
agent prompt budget.

If indexing succeeds but the language indexer has limited capability coverage,
Liza should avoid promising unsupported commands for that language. The first
milestone capability baseline is:

| Language | Indexer | Symbols | References | Implementations |
|---|---|---:|---:|---:|
| Go | `scip-go` | yes | yes | yes |
| TypeScript | `scip-typescript` | yes | yes | upstream says yes; local sample not verified |
| Python | `scip-python` | yes | partial | no |

Baseline evidence:

- Go symbols, references, and implementations were verified against this Liza
  repository's `go.scip` index.
- TypeScript symbols and references were verified against `../omni/typescript.scip`.
  TypeScript implementations are claimed by upstream `scip-search` source
  material, but were not locally verified in the available sample.
- Python symbols and sampled references were verified against
  `../omni/python.scip`. Python implementations returned no sampled results.
- Python references remain classified as partial until the limitation is defined
  against `scip-python` behavior rather than inferred from a small sample.

Examples:
```bash
scip-go index --module-root "$(pwd)" --skip-tests --output go.scip
scip-typescript index --cwd ~/Workspace/omni/apps/web/src  --output ~/Workspace/omni/typescript.scip  ~/Workspace/omni/apps/web
scip-python index --cwd ~/Workspace/omni/apps/api --output ~/Workspace/omni/python.scip
```

## Configuration Shape

The initial `state.yaml` config should use a language allowlist. Users may set
it explicitly with repeated `--scip-search <language>` options:

```bash
liza init --spec goal.md --scip-search go --scip-search typescript
```

If no `--scip-search` option is provided and `LIZA_ENABLE_SCIP_SEARCH` is
truthy, Liza should auto-detect supported languages from git-tracked code and
write the detected allowlist:

```yaml
config:
  scip_search:
    - go
    - typescript
    - python
```

Semantics:

- `scip_search` lists languages Liza should attempt to index for MAS task
  worktrees and project-root orchestrator context.
- `LIZA_ENABLE_SCIP_SEARCH` is the strict opt-in activation gate. If the
  environment variable is absent or false, Liza must not generate SCIP indexes
  or inject the `scip-search` prompt section.
- The first Liza integration milestone requires `go`, `typescript`, and
  `python`.
- The supervisor indexes only languages that are both detected in the task
  worktree and present in `config.scip_search`, and only when
  `LIZA_ENABLE_SCIP_SEARCH` is truthy.
- An absent or empty `scip_search` list disables `scip-search` indexing and
  prompt injection.
- Failed indexing for one enabled language omits that language from the prompt;
  it does not block agent spawn.

## Behavioral Decisions

- `LIZA_ENABLE_SCIP_SEARCH` truthy values are `1` and `true`.
- `LIZA_ENABLE_SCIP_SEARCH` false values are unset, empty, `0`, and `false`.
- Environment variable values are case-insensitive after trimming surrounding
  whitespace.
- `--scip-search <language>` accepts only `go`, `typescript`, and `python`.
- Any other `--scip-search` value causes `liza init --spec` to fail with a clear
  unsupported-language error.
- Duplicate `--scip-search` values are de-duplicated before writing
  `config.scip_search`.
- Explicit `--scip-search` values are written to `config.scip_search` even when
  `LIZA_ENABLE_SCIP_SEARCH` is false; MAS indexing remains inactive until
  `LIZA_ENABLE_SCIP_SEARCH` becomes truthy.
- `liza init --spec` validates `scip-search --help` and each selected language
  indexer.
- If a selected language indexer is missing during init, Liza warns and drops
  that language from `config.scip_search`.
- If every selected or auto-detected language is dropped because its indexer is
  missing, `config.scip_search` is absent or empty and no scip-search prompt is
  injected.
- Auto-detection uses `git ls-files`; test files count.
- Go is detected when `git ls-files` contains `go.mod` or any `*.go` file.
- TypeScript is detected when `git ls-files` contains `tsconfig.json`, any
  `*.ts` file, or any `*.tsx` file.
- Python is detected when `git ls-files` contains `pyproject.toml` or any
  `*.py` file.
- Worktree-creation indexing failures degrade gracefully: Liza still spawns the
  assigned agent and omits failed indexes from the prompt.
- `submit-for-review` indexing failures degrade gracefully: Liza still proceeds
  and omits failed indexes from reviewer prompts.
- Orchestrator project-root indexes refresh on every orchestrator wakeup.
- Python prompt guidance mentions that `scip-search implementations` is not
  supported for Python.

## Indexer Requirements

Language indexers are separate tools from `scip-search`. Installing
`scip-search` does not install them.

| Language | Indexer | Install |
|---|---|---|
| Go | `scip-go` | `go install github.com/scip-code/scip-go/cmd/scip-go@latest` |
| TypeScript | `scip-typescript` | `npm install -g @sourcegraph/scip-typescript` |
| Python | `scip-python` | `npm install -g @sourcegraph/scip-python` |

`scip-go` expects a Go module. Liza should run it from the directory containing
`go.mod` or pass `--module-root <worktree-path>`.

## Index Storage

Task-local SCIP indexes should be stored under:

```text
<worktree>/.liza/scip/
```

Expected files for the first milestone:

```text
<worktree>/.liza/scip/go.scip
<worktree>/.liza/scip/typescript.scip
<worktree>/.liza/scip/python.scip
```

Orchestrator SCIP indexes should be stored under:

```text
<project_root>/.liza/scip/
```

Semantics:

- Index files are generated artifacts owned by the task worktree lifecycle.
- Deleting the task worktree deletes its indexes.
- Worktree creation generates the initial indexes before the assigned agent is
  spawned.
- `submit-for-review` regenerates indexes before reviewer agents are spawned.
- Orchestrator wakeup refreshes project-root indexes before injecting
  `scip-search` guidance into orchestrator prompts.
- The supervisor passes absolute index paths to spawned agents; agents do not
  infer index paths from naming conventions.
- The `.liza/scip/` path must not make `git status --porcelain` dirty in the
  task worktree.
- Task-worktree indexes are not written into the repository root, main checkout
  `.liza/`, or a sibling `.worktrees/.indexes/` directory.

## Query Model

| Question | Tool |
|---|---|
| Where is `Supervisor` defined? | `scip-search symbols` |
| What implements `Doer`? | `scip-search implementations` |
| What calls `blackboard.Write`? | `scip-search references` |
| What packages exist? | `scip-search packages` |
| Find all functions returning unwrapped errors | `ast-grep` |
| Find all struct literals missing field `Timeout` | `ast-grep` |
| Find every textual mention of `DisplayName` | `rg` |
| Find fixture or golden files by path | `rg --files` |

## Runtime Contract

- All query commands require `--index <index-path>`.
- `symbols` accepts one or more `--name` values.
- `references` and `implementations` require at least one `--symbol` or
  `--name`; both flags are repeatable and resolved symbols are de-duplicated.
- `--location-only` is valid only for exact `--symbol` queries and cannot be
  combined with `--name`.
- `--json` should be preferred for automated prompt/tooling workflows.
- Successful queries write to stdout, write nothing to stderr, and exit `0`.
- Usage failures exit `2`; index loading failures exit `3`.

## Success Criteria

1. `liza init --spec` runs `scip-search --help` and fails with a clear setup
   error if the command is unavailable or exits unsuccessfully.
2. `liza init --spec` can also report `scip-search --version` for diagnostics
   when available.
3. Successful `liza init --spec` writes `scip_search: [go, typescript, python]`
   into the `state.yaml` `config` section for the first integration milestone.
4. `LIZA_ENABLE_SCIP_SEARCH` is documented and enforced as the strict opt-in MAS
   activation gate for index generation and prompt injection.
5. `liza init --spec` accepts repeated `--scip-search <language>` options.
6. `liza init --spec --scip-search <language>` rejects unsupported languages and
   de-duplicates repeated supported languages.
7. Explicit `--scip-search` values are written to `config.scip_search` even when
   `LIZA_ENABLE_SCIP_SEARCH` is false.
8. If no `--scip-search` option is provided and `LIZA_ENABLE_SCIP_SEARCH` is
   truthy, `liza init --spec` auto-detects supported languages from git-tracked
   code.
9. `liza init --spec` validates selected language indexers and drops languages
   whose indexers are missing, with a warning.
10. The supervisor uses `config.scip_search` as the language allowlist for
   creating task worktree indexes and injecting the `scip-search` prompt
   section only when `LIZA_ENABLE_SCIP_SEARCH` is truthy.
11. `internal/embedded/claude-settings.json` allows agents to run
   `scip-search`.
12. `README.md` lists `scip-search` as a highly recommended tool for
   token-efficient repository navigation.
13. Task worktree creation detects enabled languages, runs the appropriate SCIP
   indexers, writes task-local indexes, and then spawns the assigned agent.
14. `submit-for-review` regenerates structural indexes before spawning reviewer
   agents.
15. Orchestrator wakeup refreshes project-root indexes before injecting
   `scip-search` guidance into orchestrator prompts.
16. Index files are scoped to the task worktree or project root and do not depend
   on the user's main checkout or IDE state.
17. Generated task indexes are stored under `<worktree>/.liza/scip/` and do not
   make the task worktree dirty.
18. Generated orchestrator indexes are stored under `<project_root>/.liza/scip/`.
19. Agent prompts include explicit index paths and the concise `scip-search`
   invocation loop.
20. Python prompt guidance says `scip-search implementations` is not supported
   for Python.
21. MAS agents are told that indexes are not updated after subsequent agent
   edits.
22. Concurrent task worktrees can each have independent indexes without path
   collisions.
23. If indexing fails for an enabled language, Liza still spawns the agent and
   omits that language from the `scip-search` prompt section.
24. Agent prompts do not promise references or implementations for a language
    where the configured indexer does not provide them.

## Explicit Out of Scope

- Building or vendoring `scip-search` inside the Liza repository.
- Building language-specific SCIP indexers inside Liza.
- Installing language-specific SCIP indexers automatically.
- Teaching `scip-search` to generate, discover, cache, update, or watch indexes.
- Adding a daemon, watch mode, or incremental index updates.
- Adding semantic search, embeddings, or vector storage.
- Adding a UI or graph visualization for indexes.
- Adding an MCP server or wrapper abstraction for `scip-search`.
- Depending on IDE-backed indexes for MAS worktree navigation.
- Continuously updating indexes after every agent edit.
- Storing SCIP index artifacts in tracked source directories.

## Risks and Assumptions

- Assumption: `scip-search` is installed separately and `scip-search --help` /
  `scip-search --version` are available when `liza init --spec` is run.
- Assumption: supported language SCIP indexers are installed separately in the
  agent environment.
- Assumption: MAS operators explicitly set `LIZA_ENABLE_SCIP_SEARCH` when they
  want Liza to generate indexes and inject `scip-search` guidance.
- Assumption: `liza init --spec` can run `git ls-files` in the target project
  when auto-detecting languages.
- Assumption: `ast-grep` remains available for pattern-based structural search.
- Risk: auto-detecting languages from git-tracked code can miss generated or
  untracked code; explicit `--scip-search <language>` options override
  autodetection.
- Risk: generated indexes can become stale during a task; the prompt contract
  must make snapshot semantics explicit.
- Risk: language indexers have different capability coverage, so Liza must not
  promise references or implementations where the indexer cannot provide them.
- Risk: degrading on indexing failure preserves agent availability but may
  reduce navigation quality for the affected language.
- Risk: indexing can add task startup and review-submission latency; this should
  be paid at controlled lifecycle points rather than during arbitrary agent
  edits.
- Risk: path handling must preserve worktree isolation and avoid collisions
  between concurrent tasks.
- Risk: generated index files must be ignored so they do not violate the clean
  worktree invariant before review.
