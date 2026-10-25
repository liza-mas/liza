# 219 - Provider-Native Tool-Result Budgets

## Status

ACCEPTED — implemented 2026-09-24 (DEV-778); backfilled 2026-10-25. Narrowed by
ADR-0164 (Codex sandbox posture) and ADR-0178 (Claude Bash left uncaptured).

## Context and Problem Statement

Agent tool results entered the model's context whole. A test log, a large file
read or an MCP response could flood the context window. Nothing bounded, stored
or deduplicated them, and nothing kept credentials in that output away from the
model or the transcript.

The engine launches four kinds of agent sessions — Claude CLI, direct Codex CLI,
Devin over ACP and OpenCode — and each exposes a different interception point.
None of them has one in common.

## User-Confirmed Intent

The user confirms the trigger was context flooding. The mechanism was built in a
downstream fork first, and was then ported upstream.

## Considered Options

1. **Instruct agents to bound their own output** — prompt rules on piping,
   `head` and targeted reads.
2. **Rely on each provider's native output caps** — accept per-provider truncation
   as the budget.
3. **A local, content-addressed result boundary installed at each provider's
   native interception point.**

Options 1 and 2 are reconstructed; the user did not supply the alternatives that
were weighed.

## Decision Outcome

Chose **Option 3**. Results larger than a threshold are persisted as sanitized,
content-addressed artifacts. The model receives a bounded digest with a reference
for retrieving a slice.

### Boundary per provider

| Provider | Boundary |
|---|---|
| Claude CLI | Launch-local hooks replace native Read/Grep/Glob results with their structured schemas preserved; a batch guard stops before another model sample on a guarded result. Bash is not captured (ADR-0178). |
| Codex CLI | PreToolUse rewrite of `Bash` into a managed runner, plus post hooks for other results; sandbox posture per ADR-0164 |
| Devin ACP | The engine's transparent ACP host proxy filters filesystem callbacks and owns terminal capture before native buffer limits |
| OpenCode | Managed exec tool and plugin |

### Budget and identity

- Default threshold 32 KiB and digest 4 KiB, in bytes, configurable by flag or
  environment. The CLI threshold is capped at 64 KiB to match the OpenCode return
  boundary; invalid values fail closed.
- The digest carries exit status, truncation, deduplication and invocation
  metadata. Metadata that does not fit is an error, not silently dropped.
- Identity is SHA-256 of the sanitized, LF-normalized text: a content identity,
  not an invocation identity. Repeated results reuse the artifact.
- Sanitization runs before anything is persisted or returned. It works on decoded
  string values for structured results and redacts credential assignments,
  headers, flags, URL credentials, known token formats and PEM keys, while leaving
  ordinary code, calls and member accesses intact.
- Runner and OpenCode exec capture is bounded at 1 MiB per command.
- `tool-result read HASH --offset --limit` retrieves a slice; `tool-result stats`
  reports sizes and suppressed bytes.

The mechanism is a byte budget. It makes no claim of token or wall-clock savings.

## Rationale

Context flooding is a property of the result, not of the agent's intent, so a
mechanism has to sit where the result is produced. Prompt rules (Option 1) depend
on every agent anticipating output size before running a command. Native caps
(Option 2) truncate without a way back to the rest and without sanitization. A
store with a digest keeps the model's view bounded while the full result stays
retrievable.

Content addressing makes repeated large outputs, such as the same failing suite,
cost one artifact. Sanitizing before storage keeps the store from becoming a new
place secrets accumulate.

## Consequences

- Large results no longer enter context whole on covered paths, and the
  remainder is retrievable by hash.
- Four provider-specific boundaries must be maintained. Each relies on hook or
  proxy behavior the provider can change.
- Coverage is uneven by design: after ADR-0178, Claude Bash results receive no
  budgeting, sanitization or deduplication, to keep native permission rules
  evaluating the original command.
- Codex's input rewrite can bypass native rules for the original command (see
  `docs/tool-result-budget.md`); ADR-0164 records the sandbox posture chosen
  around it.
- Devin ACP runs in its bypass permission mode, a deliberate escalation, because
  the proxy owning terminal callbacks prevents configuring the child agent
  otherwise.
- Sanitization is pattern-based. It cannot undo exposure in a process listing or
  in a file the caller already wrote.
- A guard that stops sessions on results it should pass is a new failure class;
  `5049f50a0` fixed one such case on native results.

## Related Decisions

ADR-0164 and ADR-0178 (sub-decisions on Codex and Claude posture), ADR-0121
(provider runtime catalog the launches come from). Operating detail lives in
`docs/tool-result-budget.md`.

## Evidence and Reconstruction

Sources: `997ae78a2` (mechanism, boundaries, sanitization rules and port notes,
read in full), `5049f50a0`, and `docs/tool-result-budget.md` (limits, identity
and coverage sections). The user supplied the trigger and the reason for the
downstream-first history on 2026-10-25. The alternatives are reconstructed, and
the prior state described in the context section is inferred from what the change
added.

---
*Reconstructed from commits `997ae78a2`..`5049f50a0` (2026-09-24 to 2026-09-26).*
