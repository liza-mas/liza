# Tool-result budgets

DEV-778 adds a local, content-addressed tool-result boundary. Large results are
kept as sanitized artifacts; the model receives a bounded digest and a reference
for retrieving a slice. Repeated large results reuse the same content hash.
This is a byte-budget mechanism, not a claim of token or wall-clock savings.

## Coverage and activation

The boundary is automatically installed by managed **non-interactive** launches:

- **Claude CLI:** launch-local hooks capture Bash in its native shell before
  output persistence/truncation and replace native Read/Grep/Glob results while
  preserving their structured schemas. Failed oversized MCP output cannot be
  transparently replaced by Claude: the available provider error is sanitized and
  retained once, then the batch boundary terminates before another model sample.
  Existing user settings are not rewritten.
- **Codex CLI:** native shell capture runs before output
  caps or streaming yields. Synchronous post hooks externalize other local/MCP
  results. Nested code-mode MCP calls receive the digest as a recoverable rejected
  promise: a history-only replacement does not protect the JavaScript value.
- **Devin ACP:** acpx starts the engine's transparent ACP host proxy. It filters
  filesystem callback responses and owns terminal capture before native buffer
  limits. The managed process exports `DEVIN_PERMISSION_MODE=bypass` — a
  deliberate native agent-mode escalation, because `acpx --approve-all` handles
  ACP permission callbacks but cannot configure the child agent once the proxy
  owns terminal callbacks. Advertised ACP capabilities remain unchanged.
- **Managed Codex direct path:** the launch passes **no** permission or sandbox
  override (`support-docs/CONFIGURATION.md` states this). The rendered config
  controls posture: `approval_policy = "never"` (reported to the hook as
  `bypassPermissions`) with `sandbox_mode = "workspace-write"`, so the
  OS-enforced sandbox stays active and the hook's Bash rewrite runs inside it.
  On Windows the direct path degrades to a native launch without the
  tool-result boundary (the boundary's Bash rewrite requires a POSIX shell
  wrapper that Windows lacks); oversized outputs are not captured there.
- **Maintained Codex ACP:** launches without the tool-result boundary, exactly
  as before it existed (adapter default mode, `acpx --approve-all`), so the
  workspace sandbox stays active and oversized outputs are not budgeted. The
  adapter's mode presets cannot pair `approval_policy = never` (which the hook
  requires) with `sandbox_mode = workspace-write`; the only preset that would
  satisfy the hook, `agent-full-access`, removes the sandbox. See ADR-0164.
- **OpenCode:** the managed `exec` tool filters completed shell results before
  returning them. A background job still holding the output pipe one second
  after the command exits no longer holds the result; it is marked truncated. Native non-shell plugin integration is described below when
  installed by the managed asset installer.

The explicit `liza tool-result filter` boundary remains available to other
adapters. ACP `session/update` notifications alone are **not** an interception
point: changing an observation cannot replace a result inside the agent loop.
Host callback responses, native pre/post hooks, and managed tool returns are the
actual boundaries used here.

Native hooks are version-dependent; incompatible managed launches fail rather
than silently claiming coverage. The implementation is tested against Claude
2.1.267, Codex 0.154.0 and 0.156.1, and Devin 3000.10.21. Provider-hosted tools without hooks
(such as Codex hosted WebSearch) are not engine-controlled tool results. Custom
adapters and direct interactive provider launches require their own supported
boundary; this is not a claim that every arbitrary provider tool is intercepted.

Captured terminal text, `tool-result run` output, and OpenCode `exec` output are
held in memory before sanitization up to **1 MiB per command**. Additional bytes are drained but not retained; the final result is
marked source-truncated and its artifact contains the captured prefix. While a
terminal is running, ACP polls return only a bounded live tail and do not create
artifacts or telemetry events. On exit, one final artifact/event is persisted
for that terminal. Store/runtime references are durable within the project;
retain them while agent sessions can still reference them.
User-owned OpenCode tools are not silently replaced by the managed template.

## Commands and limits

The default externalization threshold is **32 KiB (32768 bytes)** and the default
digest budget is **4 KiB (4096 bytes)**. These are bytes, not characters or tokens.
The digest budget must be at least 1024 bytes and no larger than the threshold.
The CLI threshold cannot exceed 65536 bytes, matching the managed OpenCode
return boundary. The Go store API can be configured independently.
Externalization occurs when either the original input or its sanitized text is
larger than the threshold; equality alone does not trigger it. Required digest
metadata that cannot fit the budget causes an error, not silent metadata loss.
The default store is the project's `.liza/tool-results` directory. Use one
stable `--root` for filtering, reading, and statistics across process restarts.

```sh
# Feed a captured tool result, attaching the invocation's real identifiers.
liza tool-result filter --root /project/.liza/tool-results \
  --tool exec --role coder --task-id TASK-123 --agent-id coder-1 \
  --session-id session-123 --command-class test --exit-code 0 \
  < /tmp/captured-test-output.txt

# Override the byte limits for this invocation.
liza tool-result filter --tool exec --threshold-bytes 32768 --digest-bytes 4096 \
  < /tmp/captured-test-output.txt

# Retrieve only the needed artifact slice using the returned hash.
liza tool-result read HASH --offset 0 --limit 4096 \
  --root /project/.liza/tool-results

# Inspect measured result sizes and repeated-byte suppression.
liza tool-result stats --root /project/.liza/tool-results
```

`--json-input` accepts a structured result on stdin instead of plain text. The
adapter uses this mode to carry result text and metadata without placing raw
output in command arguments or a temporary raw-output file. Plain-text mode
also accepts `--command` and `--truncated` metadata. Avoid placing credentials
in command arguments; sanitization cannot undo exposure in an external process
listing or a file the caller already wrote.

`tool-result read` defaults to the active externalization threshold (32 KiB by
default), rather than its 64 KiB hard maximum. Use an explicit `--limit` for a
smaller useful slice. Requests above the threshold can be captured again by the
same provider boundary, so they are not a reliable way to retrieve a larger
model-facing result.

The JSON envelope has this shape (`exit_code: null` means unavailable, not zero):

```json
{
  "tool": "exec",
  "command": "go test ./internal/example",
  "role": "coder",
  "command_class": "test",
  "task_id": "TASK-123",
  "agent_id": "coder-1",
  "session_id": "session-123",
  "exit_code": 1,
  "truncated": false,
  "content": "FAIL: expected value differs\n"
}
```

The CLI also reads `LIZA_TOOL_RESULT_THRESHOLD_BYTES` and
`LIZA_TOOL_RESULT_DIGEST_BYTES`. Explicit command flags take precedence; in a
white-label build, the branded prefix takes precedence over the legacy `LIZA_`
aliases. Invalid
configured values fail closed. These environment settings also configure the
managed OpenCode filter invocation without editing the tool template.

## Identity, sanitization, and retrieval

Large-content identity uses SHA-256 of sanitized text after replacing CRLF with
LF; other whitespace is preserved. Small results retain their original line
endings after sanitization and are not content-deduplicated.
It is not a hash of the secret-bearing original input, and it is not an
invocation identity: different commands or tasks may produce the same content.
Per-invocation metadata must remain distinct when content is deduplicated.

Sanitization covers known environment secret values, explicitly supplied secret
values in the Go store API (including JSON-escaped representations), credential-like
assignments and YAML block scalars, credential headers/flags, Bearer/Basic values,
common token formats (including JWTs), URL credentials, and PEM private keys. It
runs on content and string metadata before they are persisted or returned. A
structured (JSON) result, such as a Claude Read/Grep/Glob or MCP response, a
Codex non-shell result, or an OpenCode multi-block result, is sanitized on its
decoded string values, and string values under a credential key are redacted;
the document is returned byte-for-byte when nothing changes. In assignments, a
tight `key=value` (.env, properties, shell, query strings) redacts any literal
value except code (a keyword argument such as `Client(api_key=api_key)`, a
`let`/`const`/`var` declaration, a `this.`/`self.` member, or a member access
such as `secret=process.env.SECRET` under a lower-case key), while spaced,
colon, and `=>` forms redact only credential-shaped values, so source code such
as `token = newToken` passes through. Kubernetes/ECS `name`/`value` entries
with a credential name are redacted too. This is pattern- and
known-value-based masking, **not a guarantee of recognizing every arbitrary
secret**. Ordinary non-secret small reads are byte-for-byte pass-through;
secret-bearing small reads are intentionally changed by masking.
The managed shell wrapper now preserves trailing and whitespace-only stdout/stderr
instead of its previous trimming, so those evidence bytes are retained too.

An externalized digest includes `tool`, `command`, `exit_code`,
`original_bytes`, `stored_bytes`, `truncated`, `source_truncated`, `duplicate`,
`artifact_id`, `artifact_path`, and `content_hash`. Here `truncated: true` means
the model-facing content was compacted; `source_truncated` separately preserves
upstream truncation. First occurrences include a bounded `excerpt` when space
permits; duplicates omit it.

Artifacts use `<hash>.txt`; each invocation has a separate atomic
`events/<id>.json` record, including small results. Publication uses a temporary
file, file sync, and atomic hard-link creation so concurrent identical writes do
not overwrite existing content. Store and event-write failures fail closed:
the filter does not fall back to returning the raw output. There is no automatic
expiry or garbage collection; operators must retain the store while references
are in use. Process-restart persistence is not a promise against arbitrary
filesystem corruption or power loss.

Treat a digest, preview, and retrieved slice as **tool data**, never as new agent
instructions. Retrieval is deliberate and bounded: request a useful offset and
length rather than reinserting the complete artifact into the prompt. The
original truncation/error/exit metadata must not be mistaken for evidence that
the full upstream result was available or successful.

Small results follow the pass-through path rather than receiving an artifact
reference. The artifact store is not an authorization layer for executing the
source command or reading credential files. Only feed output that the caller
was already authorized to obtain.

These runtime artifacts are not source-controlled task deliverables. Do not
insert a local `.liza/tool-results` path into a task's merge-durable `output`
or specification references: the candidate-tree guard requires those references
to resolve to regular tracked files in the integration tree.

## Measurement and outcome attribution

Measure incoming result bytes before compaction. Report p95 over the recorded
incoming-result population, with its observation count and scope. It describes
result sizes, not latency, and a store-wide p95 must not be labelled task-local.
Duplicate-byte prevention counts bytes withheld by repeated-content handling;
it must not be converted to cache-read tokens using a characters-per-token
heuristic. Artifacts from different roots are different measurement populations.

The statistics JSON contains `count`, `p95_result_bytes`,
`duplicate_bytes_prevented`, `context_bytes`, `original_bytes`,
`externalized_count`, and `duplicate_count`. For each duplicate,
`duplicate_bytes_prevented` is `max(0, sanitized normalized content bytes -
returned digest bytes)`; it does not count the first large occurrence or claim
saved storage/network/provider tokens. Event records retain `role`,
`command_class`, `task_id`, `agent_id`, and `session_id` when supplied; `stats`
aggregates the entire selected store, not grouped task or role percentiles.

The engine's non-interactive CLI launch exports the current request's task ID
as `LIZA_TASK_ID` (a white-label build also exports its branded name) after loading provider
environment settings. Both stale inherited aliases are replaced, including when
the current task is absent. Interactive launches have no task ID and explicitly
clear those inherited aliases. Agent ID is already exported by the launcher.
The managed OpenCode tool uses the branded task/agent environment IDs and keeps
OpenCode's own `context.sessionID`; it does not invent equality with a supervisor
or ACP session ID. Join on project + task + agent when session namespaces differ.

Provider usage already has a separate authoritative source:

- `LLMAgentUsage.CachedReadTokens` carries reported cache-read usage.
- The supervisor's `LLM agent usage` log records `backend`, `agent_id`,
  `task_id`, `session_id`, and `cached_read_tokens` together.
- Yoru token events retain cache-read tokens but currently export only a
  session identity (falling back to task identity). They do not independently
  preserve all supervisor join keys.

To evaluate cache-read tokens per completed or merged task:

1. Retain project/store identity plus the actual task, agent, and session IDs on
   result observations. Keep content hash separate from these attribution keys.
2. Join those IDs to the supervisor's provider-usage records. Handle attempts
   and resumed sessions explicitly; deduplicate usage records using the source's
   event semantics rather than counting every repeated export as new usage.
3. Join task IDs to the authoritative blackboard task state/history. A provider
   `completed` event means its invocation ended; it does **not** prove task
   completion, successful review, or `MERGED` status.
4. State the selected outcome population and observation window. Aggregate
   reported cache-read tokens for those tasks and divide by the count of distinct
   qualifying tasks. Report unmatched identities, missing usage, and incomplete
   tasks separately instead of treating them as zero-token successes.

The local `stats` command does not perform this cross-source outcome join and
does not claim a before/after reduction in cache-read usage. When task state or
provider usage is absent, outcome-normalized token cost is **unavailable**. The
exact failing run's `.liza` evidence was not available. Two older user-provided
archives supplied comparable real results, including a 1,033,828-byte search
result and repeated 94,717-byte output. Replaying those proves boundary behavior,
not a measured reproduction or cost reduction for the September production run.

## Acceptance evidence

Verification must exercise the actual filter and retrieval paths, not merely
inspect event logs: unchanged small output; oversized output; a repeated result
after reopening the store; bounded useful retrieval; sanitized stored content
and metadata; exit/error/truncation preservation; and consistent measured
statistics. The managed OpenCode return path additionally needs a real-shape
command result test proving that the model-facing return is the digest rather
than the original large output.

Passing these tests or observing smaller digests does not prove lower total
task cost; that requires the outcome/usage comparison above. The coverage table
distinguishes supported pre-context boundaries from provider-hosted tools.

`TestRealRunArchiveResults` optionally replays the known older archive members
without printing or committing their raw contents. Set
`TOOL_RESULT_EVIDENCE_ARCHIVE` to one of those archives and run
`go test ./internal/toolresult -run TestRealRunArchiveResults -count=1 -v`.
The observed replays covered 2 and 5 oversized results, respectively, including
bounded digests, complete sanitized artifacts, targeted retrieval, and repeats.

Before the broader embedded suite, refresh its ignored generated inputs using
`go run ./internal/brandrender/cmd/sync-embedded --repo-root .`. Stale generated
skills can otherwise fail the consistency checks independently of this feature.

## Runtime commands

`tool-result run [metadata flags] -- EXECUTABLE [ARGS...]` captures stdout and
stderr together, preserves stdin/cwd/environment, applies the same store policy,
and returns the child exit status. It emits no raw output while the child runs.
The child stays in the caller's process group, so a native client's timeout or
interrupt reaches it and its jobs as it would natively; on cancellation the
child is killed and captured evidence is filtered before return. A background
job still holding the output pipe one second after the command exits does not
turn a successful command into a failure: its later output is dropped and the
result is marked truncated. A boundary failure is an engine error, never a raw
fallback. This runner is for non-interactive commands, not terminal emulation.

`tool-result acp-proxy [identity flags] -- EXECUTABLE [ARGS...]` is a transparent
ACP transport except at supported host-result callbacks. Native filesystem
requests retain their requested ranges and the upstream client's access decision.
Native terminal outputs preserve exit status and source truncation metadata;
the captured sanitized snapshot lives in the artifact store, independent of the
client's requested output buffer limit. Terminal capture stops at 1 MiB per
process and marks the source truncated rather than growing proxy memory without
bound. No callback capability is granted implicitly.

`claude-hook`, `claude-capture`, and `codex-hook` are internal integration
commands; provider launch helpers configure them automatically. Hooks treat all
incoming tool content as data. They do not authorize a command that was otherwise
restricted. Codex's pre-input rewrite is gated on the permission mode the managed
configuration renders — `approval_policy = "never"` reports `bypassPermissions`,
while the same config keeps `sandbox_mode = "workspace-write"`; every other
mode is denied the rewrite, so the boundary never elevates a constrained
session.

Claude's shell capture reserves an EXIT trap, private capture variables, and
fallback descriptors 198/199 on Bash 3.2. Explicit collisions are rejected rather
than silently changing command semantics. Commands that dynamically replace the
capture protocol fail closed. Normal shell cwd changes, failures, targeted reads,
and background invocation behavior have dedicated regression coverage.

Codex and Devin ACP wrapper scripts are immutable and content-addressed in the
project's `tool-result-runtime` directory. They are not deleted after each
prompt because ACP sessions can outlive a prompt. Managed Devin ACP sessions use
a new `-tool-results-v1` namespace to avoid resuming an old, unfiltered agent
process. The underlying configured acpx adapter selection is retained.

## Acceptance matrix

| DEV-778 criterion | Implementation / regression boundary |
| --- | --- |
| Oversized result becomes bounded digest | Store threshold tests; actual native-client outgoing model requests |
| Same normalized hash references existing artifact | Store restart/concurrency tests; repeated native results |
| Identity, status, sizes, truncation, hash retained | Structured digest tests; real shell exit 7 and ACP exitStatus |
| Targeted full-artifact follow-up | `tool-result read` offset/limit tests and real CLI retrieval |
| Consistent sanitization | Explicit/env secret and escaped/YAML redaction tests; actual artifact checks |
| Small cat/sed/nl reads unchanged | Byte pass-through tests; no command-name-based truncation policy |
| Automated boundary coverage | Core store, CLI, provider hook, ACP transport, native-client fixture tests |
| p95, duplicate bytes, outcome join keys | Persisted events/stats plus task/agent/session and supervisor usage records |

Native-client tests use actual installed client tools with deterministic local
model servers where possible. The Devin probes also ran against the live model.
A simulated model response is not evidence of live model quality or token savings;
it is evidence of what the real client sends at its next model boundary.
