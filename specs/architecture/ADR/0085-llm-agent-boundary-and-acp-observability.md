# ADR-0085: LLMAgent Boundary and ACP Observability

## Status

Accepted

## Context

Liza currently coordinates agents through supervisor-assigned tasks, file-backed
state, worktrees, and deterministic task transitions. Agent providers are
launched through CLIs such as Claude, Codex, Gemini, Kimi, or Vibe. This keeps
the orchestration provider-agnostic and avoids making agents negotiate with
each other.

ACP/A2A-style agent protocols were evaluated for two different purposes:

- agent-to-agent coordination
- provider-neutral observability of agent reasoning, tool calls, and streaming
  trajectory events

The coordination use case does not match Liza. Liza intentionally avoids
peer-to-peer agent negotiation; the supervisor and blackboard are the source of
truth. The observability use case is different: ACP trajectory metadata can
expose real-time reasoning deltas, internal tool calls, and structured spans
that are hard to recover reliably by tailing provider-specific CLI logs.

## Decision

Introduce `LLMAgent` as the semantic execution boundary and `CLIAgent` as the
default OSS implementation. `CLIAgent` preserves the current CLI subprocess
behavior. Liza can also provide explicit opt-in ACP-backed implementations,
such as `ACPXAgent`, without changing supervisor execution semantics.

The boundary includes `LLMAgentEventSink`, a provider-neutral event stream for
agent observability. `CLIAgent` emits process lifecycle and stdout/stderr chunk
events. An ACP adapter can emit richer trajectory, delta, and tool-call events
through the same sink.

For migration to the existing ACP implementation, the boundary now includes:

- session hints (`TaskID`, `SessionID`, `ResumeSession`, `WarmSession`) in
  `LLMAgentRunRequest`
- session result metadata (`SessionID`, `WarmUsage`) in `LLMAgentRunResult`
- usage accounting (`InputTokens`, `OutputTokens`, `CachedReadTokens`,
  `CachedWriteTokens`) in `LLMAgentUsage`
- ACP-style event kinds (`agent_message_chunk`, `agent_thought_chunk`,
  `tool_call_update`, `usage`) in `LLMAgentEventKind`

The PR keeps ACP out of the default runtime path and does not change Liza's
coordination model. ACP-backed execution is explicit opt-in provider plumbing,
not a replacement for the supervisor or blackboard.

The ACPX implementations use catalog-defined launch arguments such as
`acpx --approve-all` intentionally because Liza agents already run
non-interactively inside supervised task worktrees. This matches the automation
posture of existing CLI-backed agents, but it is a trust boundary: the provider
catalog now carries structured launch argv, and ACPX plus the wrapped provider
remain responsible for their own sandboxing and permission semantics.
Catalog-backed ACP providers are therefore opt-in and should not become the
default runtime dependency without a separate review of the ACPX permission
model and catalog distribution path.

ACPX commands execute with `--cwd` set to the project root, while session names
are scoped to the Liza task identity so later turns within the same task can
reuse provider session state without colliding across tasks. `WarmUsage` is
best-effort operational metadata: the adapter checks both process-local seen
state and persisted ACPX session existence, but a provider may still reload,
recreate, or globally resolve a session internally. It must not drive
correctness-sensitive task transitions.

ACP also opens a future prompt-caching and bootstrap-efficiency path, but this is
not built in by this ADR. What is built in today is a stable session boundary:
Liza can address an ACP-backed agent by session name and can ask the provider to
continue work in that session. This may let an ACP provider reuse already-loaded
conversation and tool context, depending on that provider's session semantics.
It does not automatically create reusable base sessions, fork preloaded agent
templates, or guarantee model-side prompt-cache hits. Cache-oriented follow-up
work is tracked as an opportunity in `specs/architecture/acp-vs-cli.md`.

Event streams may include partial message or usage events before a terminal
`completed` event that reports an error. Consumers should treat `completed` as
the run outcome and earlier events as partial observability, not proof of
success.

The supervisor attaches a conservative event sink for production runs. It records
lifecycle and structured usage metadata, but intentionally does not duplicate
provider content chunks; content stays in the normal provider output stream and
agent output files where existing masking and retention rules apply.

## Consequences

- OSS CLI support remains the default and remains provider-agnostic.
- Legacy `CLIExecutor` names are retained as compatibility wrappers.
- Liza gains an explicit place for ACP trajectory events without binding OSS to
  ACP in the default execution path.
- ACP is framed as an observability and warm-session execution adapter
  opportunity, not a replacement for Liza's blackboard, worktree, or supervisor
  model.
- Real-time dashboards or OTLP export can be added later by consuming
  `LLMAgentEventSink` events.
- CLI-backed agents do not emit usage events when no structured token accounting
  is available; absence of a usage event means "unknown", not zero tokens.
