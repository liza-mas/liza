# ADR-0178: Claude Bash Capture Preserves Native Permissions

## Status

ACCEPTED — 2026-10-05. The maintainer explicitly accepted mode-specific capture
degradation for D-37(c).

## Context

The Bash capture hook returns a shell wrapper as `updatedInput.command`.
Claude evaluates native permission rules against that replacement, so an
original-command allow can stop matching. An original-command deny can also
stop matching when a separate policy hook grants allow. Instrumentation must
not change the subject of a permission decision.

## Decision

Rewrite Bash only when the provider hook reports exactly `bypassPermissions`.
For every controlled mode, including managed `auto` and `dontAsk`, and for
missing or unknown modes, return an empty hook response before opening capture
storage. Do not rewrite input, emit a permission decision, or impose capture
shell reservations in those modes. Preserve the provider's original command
and its native and optional policy checks. Do not change the launch mode.

Keep the existing bypass-mode FIFO capture, including failed/background output,
and the existing native Read/Grep/Glob/MCP output boundaries.

## Alternatives

- Granting permission to the wrapper or adding a wildcard wrapper allow would
  weaken native deny/ask matching and is rejected.
- A provider-supported execution-output boundary could preserve both capture
  and permissions, but needs separate lifecycle/platform and permission-parity
  validation. Post-only filtering cannot cover failed/background Bash output.

## Consequences and Validation

Controlled-mode Bash loses engine externalization, sanitization, deduplication,
and capture telemetry. This includes the managed `auto` baseline. That loss is
explicitly accepted rather than trading permission authority for output budgets.
Unknown modes degrade in the same way. Other tools retain their existing coverage.

Regression tests compare isolated installed-Claude sessions with and without
the hook, checking filesystem execution and the next model-facing tool result
for native allow/deny/ask and independent policy allow/manual/deny/error cases.
The retained capture path continues to require failure/background and native
shell lifecycle tests. Local deterministic model responses validate real client
behavior, not live model quality or future provider versions.

Revisit when Claude offers a supported output boundary that preserves original
permission matching and failed/background output. Require native-client parity
before enabling capture there. Track this ceiling in [TECH_DEBT.md](../../../TECH_DEBT.md).

## Related

[ADR-0164](0164-managed-codex-sandbox-posture.md) establishes the adjacent rule
that unsupported output capture degrades rather than weakening provider posture.
[Tool-result coverage](../../../docs/tool-result-budget.md) records current limits.
[Claude PreToolUse reference](https://code.claude.com/docs/en/hooks#pretooluse-decision-control)
documents permission evaluation against updated input.
