# CLIAgent vs ACPXAgent Feature Comparison

Reference for understanding feature parity between the two `LLMAgent` backends.
Based on the `llm-agent-boundary` branch (PR #83, ADR-0085).

## Feature Matrix

| Capability | CLIAgent | ACPXAgent | Notes |
|---|---|---|---|
| **Backend dispatch** | claude, codex, gemini, mistral, kimi | Current Liza integration supports codex via acpx | Implementation gap — ACP is not structurally single-backend |
| **Agent output logs** | Streams to `.liza/agent-outputs/*.txt`, `*.err` | Streams `acpx prompt` stdout JSON-RPC to `.txt` and stderr diagnostics to `.err` | ACP logs raw prompt JSON-RPC output for transcript fidelity |
| **Output ingestion** | Streams stdout/stderr into `.liza/agent-outputs/`, event writers, and progress signals | Streams `acpx prompt` stdout/stderr into `.liza/agent-outputs/`, message events, and progress signals | ACP stdout is parsed incrementally for message chunks and usage |
| **Progress watchdog** | Feeds `progressCh` on every stdout/stderr write | Feeds `progressCh` on streamed ACPX stdout/stderr activity | Watchdog still also polls worktree + state |
| **Interactive mode** | Full support | Returns error | Expected — documented |
| **Secret masking** | Creates masker only when `outputsDir != ""` | Always creates masker | ACP is stricter — masks even in no-log mode |
| **Provider env file** | Loads `claude.env`, feeds entries to masker | No env file loading | Low impact — ACP doesn't use Claude's env file |
| **Codex version pinning** | `LIZA_CODEX_VERSION` / `config.codex_package_version` | No ACPX adapter/package pinning | Implementation gap if ACP becomes production-critical |
| **Claude subagent disabling** | `LIZA_DISABLE_CLAUDE_SUBAGENTS` → `--disallowedTools Task` | N/A | Provider-specific, not applicable |
| **Token usage reporting** | Parses the final usage events of Claude (`result`, with per-model `modelUsage` and cost), Codex (`turn.completed`) and OpenCode (`step_finish`) from their JSON output; other CLIs report none | Parses `inputTokens`/`outputTokens`/`cachedReadTokens`/`cachedWriteTokens`/`thoughtTokens` from acpx JSON-RPC | CLI is richer for Claude (models, cost, 1h cache split) |
| **Warm session management** | None | Local `seen` map + `acpx sessions show` for cross-restart detection | ACP advantage |
| **Event semantics** | `output_chunk` (raw bytes from stdout/stderr) | `agent_message_chunk` (parsed ACP messages) + `usage` | Different by design |
| **Trajectory/tool/thought events** | Not available from CLI stdout | Not parsed yet, though event enum supports them | Implementation gap in ACP event handling |

## Production Event Sink

Both backends emit events to `supervisorLLMAgentEventSink` (`systemctl.go`), which:

- Logs `started` and `completed` to the Liza structured logger
- Logs `usage` with token breakdowns when a backend provides structured usage
- Drops content events (`output_chunk`, `agent_message_chunk`) — "avoids duplicating content events" since CLI output already streams to agent-output log files

This means ACP's richer event types (trajectory, tool calls, thoughts) would
currently hit the `default` no-op case even if parsed. The event infrastructure
is wired but the sink is minimal.

## Opportunities

### Prompt caching and bootstrap efficiency

ACP creates a path to reduce repeated prompt/bootstrap cost, but `codex-acp`
does not make prompt caching a first-class Liza feature by itself.

What exists today:

- Liza can address an ACP-backed agent by stable session name.
- ACPX can continue work in a provider session when that provider supports
  session reuse.
- `LLMAgentUsage` can carry cached-token counters reported by ACPX.

What would need further development:

- Split Liza agent prompts into a deterministic static prefix and a small dynamic
  task payload.
- Preload contracts, tool policy, and stable repository orientation into
  long-lived ACP sessions instead of passing them through every run prompt.
- Define which per-run values are safe dynamic parameters, such as task id,
  blackboard slice, role, worktree path, user request, and validation target.
- Keep those dynamic values after the cacheable prefix so provider-side prompt
  caches can see identical leading content across runs.
- Track ACP backend capabilities for session resume, load, and any future
  session-fork/template semantics.
- If the backend supports forkable or resumable template sessions, create
  role/worktree-specific base sessions with contracts and tool policy already
  loaded, then spawn task runs from those bases.
- Add usage telemetry that distinguishes prompt tokens, cached read/write
  tokens, and unknown usage so operators can validate whether the optimization is
  actually working.

Until that work exists, `codex-acp` should be treated as a warm-session adapter
and observability path, not as a guaranteed prompt-caching mechanism.

## Implementation Gaps (PR #83)

### Rich ACP event handling (medium)

`LLMAgentEventKind` already has ACP-style event kinds for message chunks,
thought chunks, tool-call updates, and usage. The current `ACPXAgent` parser only
extracts agent message chunks and usage from ACPX JSON-RPC output, and the
production sink logs only lifecycle and structured usage metadata.

This is an implementation gap in Liza's ACP event handling, not a protocol
limitation. ACP providers can emit richer `session/update` notifications than
the current adapter consumes.

Closing the gap would require Liza to:

- parse ACP thought, tool-call, command, and trajectory update variants emitted by
  ACPX providers
- map provider-specific payload details into stable `LLMAgentEvent` payloads
- decide which events belong in structured logs, output transcripts, dashboards,
  or future OTLP export
- apply masking and retention rules before persisting or displaying any provider
  content
- add compatibility tests with recorded ACPX JSON-RPC streams from each supported
  provider

### ACP provider coverage (medium)

The current Liza integration exposes only `codex-acp` as a supported ACP-backed
runtime. This is an implementation gap, not a structural ACP limitation:
`ACPXAgent` has generic name-mapping logic for ACPX agent names, but Liza's
public CLI registry only accepts `codex-acp`, and contract setup maps that name
to Codex's `AGENTS.md` convention.

Adding another ACP provider would require explicit Liza wiring:

- add a supported backend name, such as `gemini-acp` or `acpx-gemini`
- map that backend to the correct contract/setup file conventions
- verify the provider's `acpx` command shape for session ensure/show/prompt
- verify JSON-RPC output and usage fields match `ACPXAgent` parsing assumptions
- add tests and support-doc entries for the new backend

### ACPX dependency and adapter version control (low)

CLI-backed Codex can be version-pinned through `LIZA_CODEX_VERSION` and
`config.codex_package_version`. The current ACP path preflights that `acpx` is
available on `PATH` before direct agent execution or spawned agent startup, and
the error includes the `npm install -g acpx` install hint. It does not pin or
validate the ACPX package version or the wrapped ACP adapter package.

This is an implementation gap if `codex-acp` becomes a production-critical
runtime. It is not a structural ACP limitation, but it affects reproducibility:
different machines may run different ACPX or `@agentclientprotocol/codex-acp`
versions with different JSON-RPC behavior.

Closing the gap would require Liza to:

- define configuration fields for ACPX and provider-adapter versions
- check the installed ACPX version during init or agent startup
- fail clearly when the installed version is missing or outside the supported
  range
- document installation/update commands for pinned ACPX adapters
- include version details in ACP run metadata and logs

### Session control-call transcripts (low)

The prompt subprocess now streams to `.liza/agent-outputs/`, but ACPX session
control calls (`sessions show` and `sessions ensure`) still use the short
buffered `runACPX` path. Their diagnostics are returned through the run error and
masked before crossing the `LLMAgent` boundary, but they are not written as
separate transcript files.

This is an implementation gap in startup/session diagnostics, not prompt
execution. Closing it would require either routing session control calls through
the same transcript writer with metadata distinguishing control calls from prompt
runs, or adding explicit support-doc language that only prompt subprocesses are
transcript-logged.
