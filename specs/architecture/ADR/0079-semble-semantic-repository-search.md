# 79 - Semble Semantic Repository Search

## Status

ACCEPTED

## Context

ADR-0068 added Stacklit for module/dependency orientation and SCIP for precise symbol navigation. Those tools reduced a large amount of repository-discovery cost, but they did not cover the earliest search shape agents often need: natural-language discovery before the right module, symbol, or filename is known.

Agents still asked broad conceptual questions such as where a workflow is validated, where defaults are resolved, or where a repair policy is specified. Plain `rg` and direct reads are reliable source-of-truth tools, but they are inefficient for broad conceptual discovery and can consume context before the agent has a good search term.

The useful operating pattern is a combination, not a replacement chain: Stacklit first gives agents the repository map, likely owning modules, and dependency boundaries; Semble then answers conceptual questions inside that oriented search space when the right symbol or filename is still unknown. In practice, Stacklit narrows "which part of the system owns task/review flow?" to packages such as `internal/ops`, `internal/models`, and `internal/pipeline`, while Semble surfaces the relevant lifecycle, role, and support documentation for questions phrased in product terms. The agent then verifies both candidate routes with direct source reads.

Semble provided a local semantic chunk-search CLI that fit this missing layer, but it introduced operational constraints: model/cache readiness, possible first-run Hugging Face access, worktree root safety, `.sembleignore` handling, and the need to keep semantic results as candidate pointers rather than proof.

## Decision

Add Semble as an optional semantic repository-search tool for conceptual discovery.

Semble activation is strict opt-in through `LIZA_ENABLE_SEMBLE`. Liza validates the executable and offline readiness before advertising Semble to unattended MAS agents. Init-time prewarm may intentionally populate the model/cache when the operator enables Semble; offline validation uses `HF_HUB_OFFLINE=1`, MAS prompt guidance omits Semble entirely when validation fails, and user/operator installation guidance owns any persistent offline-mode environment setup.

Semble prompt context is scoped to explicit local roots:
- task agents search the task worktree root
- reviewer agents search the reviewer candidate worktree root
- orchestrators search the project root only when root safety checks pass
- Pairing SessionStart may advertise repo-root Semble only when the root has safe `.sembleignore` coverage and offline validation succeeds

Semble is positioned as candidate discovery, not evidence. Agents must still verify behavior with direct source reads before editing, reviewing, or claiming success.

Liza creates or verifies Semble-visible ignore rules so repository searches do not index `.liza/`, `.worktrees/`, generated indexes, runtime artifacts, or credential files. Generated task-worktree `.sembleignore` files are hidden from task diffs using the shared private-exclude helper used for other generated worktree artifacts.

### Decision revision — 2026-10-05 (D-48)

Task and reviewer semantic discovery now uses the safe project-root corpus.
The original per-worktree targets required a cold build on every new task,
while the model-fixture readiness check did not prepare any repository corpus.
Init and the serialized lifecycle refresh coordinator prepare one `all` corpus
offline, including Semble-only installations; orchestrator startup requests
preparation for existing installations. Preparation is bounded to ten minutes
and runs outside state/integration locks. Engine corpus queries share a
cross-process lock separate from refresh coordination, so a busy readiness
probe omits guidance instead of starting a competing build. Preparation waits
for a live probe within its ten-minute budget, then uses the remaining time
for its query. Pairing SessionStart uses the same engine probe via the hidden
`semble-ready` backend; its 40-second provider budget contains the 30-second
query and retains other startup context on optional failure.

Every advertised search/find-related query uses `--content all`: the external
cache is one slot per root with exact content-mode matching. A live actual-root
query within 30 seconds is required before advertisement, across restarts and
partial preparation failures. Its success is point-in-time evidence, not a
guarantee against subsequent source/cache changes or external mode changes.

The project root supplies candidate paths only. Agents resolve each root-relative
candidate in their assigned worktree and read there; added, deleted, and diverged
task files require worktree text/symbol searches. Review evidence and all edits
stay tied to the candidate worktree. Strict opt-in, offline-model behavior,
physical ignore coverage, and optional failure behavior remain required.

This supersedes the worktree-root discovery choice above without changing
worktree isolation for implementation or review. Cold/warm performance still
requires representative measurements; tests establish routing/readiness behavior.

## Consequences

Positive:
- Agents get a semantic discovery tier before they know exact symbols or module names.
- Semble complements Stacklit and SCIP instead of replacing them: Stacklit supplies structural orientation, Semble supplies conceptual routing, and SCIP/direct reads provide precise verification.
- The Stacklit + Semble combination reduces `rg`-only thrash for broad questions by avoiding premature keyword guessing and by steering agents toward both the relevant implementation modules and the relevant docs/specs.
- A representative Pairing-session comparison estimated the combined Stacklit + Semble path at roughly 12-18 focused tool calls, versus 20-35 calls with `rg`-only discovery, with `rg`-only context usage around 1.5x-2.5x higher for the same broad conceptual question.
- MAS prompts do not silently trigger model downloads during unattended work.
- Worktree-specific target roots preserve isolation between task worktrees and the project root.
- Semantic search results remain bounded by source-read verification discipline.

Trade-offs:
- Semble, Python dependencies, and model/cache state are optional external operator responsibilities.
- Offline-readiness checks and init prewarm add setup complexity.
- `.sembleignore` safety is required before project-root or worktree-root search can be advertised.
- Semantic results can be irrelevant or incomplete, so agents must not treat them as proof.

## Alternatives Considered

1. Use only `rg`, `ast-grep`, Stacklit, SCIP, and direct reads.

Rejected because none of those directly answer broad natural-language discovery questions before useful exact terms are known.

2. Use Morph MCP semantic search as the primary path.

Rejected for this milestone because Liza needed local, explicit, worktree-scoped CLI guidance that fits MAS tool restrictions. Morph remains a fallback when Semble is unavailable and current tool policy exposes it.

3. Use Semble MCP or remote Git URL indexing.

Rejected for the first milestone because MCP policy and remote cache semantics were separate concerns. Local path search gives Liza better control over worktree roots and safety checks.

4. Treat Semble as required tooling.

Rejected because Liza is stack-agnostic and optional repository navigation must degrade gracefully.

## Relationship to Prior Decisions

Extends ADR-0068 (Optional Repository Indexing with SCIP and Stacklit) by adding the semantic-discovery layer before module orientation and symbol tracing. Extends ADR-0074 (SessionStart Context Hooks) by allowing Pairing sessions to receive Semble repo-root guidance when safe. Reinforces ADR-0030 (Code-Enforced Agent Guardrails) by keeping tool availability explicit and verification separate from discovery.

---
*Reconstructed from specs/goals/20260601-integrate-semble.md and commits 8716af67..ff8130be (2026-06-01 to 2026-06-02)*
