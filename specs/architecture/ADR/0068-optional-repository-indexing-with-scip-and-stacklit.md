# 68 - Optional Repository Indexing with SCIP and Stacklit

## Status

ACCEPTED — project-root refresh for orchestrator context superseded by
[ADR-0156](0156-repo-root-index-ownership-by-lifecycle-hooks.md): lifecycle Git
hooks and a post-merge trigger now own repo-root indexes, at
`<root>/<language>.scip`. Task worktree refresh is unchanged.

## Context

Liza agents work in dedicated task worktrees and repeatedly need repository navigation before they can plan, code, review, or validate. Plain `rg` and source reads work, but they consume context, create noisy tool traces, and require the agent to already know what to search for.

Two complementary indexing shapes emerged at roughly the same time:

- SCIP indexes for precise symbol, reference, package, and implementation navigation.
- Stacklit indexes for compact module maps, dependency summaries, workflow hints, and first-pass orientation.

Both needed the same architectural constraints: strict opt-in activation, worktree-local generated artifacts, explicit prompt-supplied paths, snapshot semantics, graceful degradation on indexing failure, and no dependence on IDE state, daemons, global caches, or inferred index locations.

## Decision

Add optional repository indexing with SCIP and Stacklit.

SCIP support is gated by `LIZA_ENABLE_SCIP_SEARCH` plus durable `config.scip_search`. Liza generates `.liza/scip/<language>.scip` indexes for supported Go, TypeScript, and Python worktrees using external language indexers. It refreshes task-local indexes during worktree creation and review submission, refreshes project-root indexes for orchestrator context, and injects explicit `scip-search --index <path>` guidance only for successful indexes.

Stacklit support is gated by `LIZA_ENABLE_STACKLIT`. Liza refreshes `stacklit.json` for orchestrator and task worktree context and injects explicit Stacklit index paths into prompts. Agents are instructed to use `stacklit derive --ai-summary -i <path>` for first-pass orientation, then verify behavior against source files.

Generated indexes must not dirty task worktrees. SCIP task indexes live under the task worktree `.liza/scip/` path and are privately ignored for that linked worktree. Stacklit task indexes are allowed only when `stacklit.json` is tracked or ignored; tracked task-local indexes are marked skip-worktree, ignored indexes remain ignored, and Liza rejects the unsafe middle state where `stacklit.json` is untracked and unignored.

Liza does not install `scip-search`, language indexers, or Stacklit. It does not run daemon/global/cache/watch modes or ask agents to infer index paths. Operators own the external tools and curated Stacklit inputs.

## Consequences

Positive:
- Agents get low-token repository orientation and precise symbol navigation before broad source exploration.
- Stacklit and SCIP form an orient-then-trace workflow.
- Prompt guidance uses explicit index paths, avoiding inferred or global index locations.
- Generated task-local indexes do not dirty task diffs.
- Indexing failures degrade gracefully by omitting failed indexes from prompts.
- Projects that do not want repository indexing keep it disabled by default.

Trade-offs:
- `scip-search`, language indexers, and Stacklit are optional external dependencies.
- Runtime index refresh adds lifecycle latency.
- Index snapshots can become stale after subsequent agent edits.
- Language indexers have uneven capability coverage.
- `stacklit.json` git-state rules add setup burden.
- Operators must manage external tools and curated Stacklit inputs outside Liza.

## Alternatives Considered

1. Use only text search and direct reads.

Rejected because this increases first-pass context cost and guesswork for large or unfamiliar repositories.

2. Use SCIP only.

Rejected because SCIP is strongest for precise symbol navigation after the agent has a likely package, symbol, or file. It does not replace a compact repository map.

3. Use Stacklit only.

Rejected because Stacklit's repository summary does not replace precise symbol/reference/implementation queries.

4. Enable indexing unconditionally.

Rejected because Liza should not require every project to install external indexers or accept generated index files.

5. Depend on IDE, daemon, cache, or global index state.

Rejected because MAS worktrees need filesystem-truth indexes tied to the active worktree, not user-local state from the main checkout.

## Relationship to Prior Decisions

Extends ADR-0030 (Code-Enforced Agent Guardrails) and ADR-0057 (CLI-Native Access Control) by keeping tool availability explicit and prompt-safe. Complements ADR-0074, which later exposes Pairing repo index paths through SessionStart hooks.

---
*Reconstructed from specs/goals/20260517-use-scip-search.md and commits 2e626ba1..ba003785 (2026-05-21 to 2026-05-27)*
