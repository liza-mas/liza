# ADR-0178: Claude Bash Is Not Rewritten, to Preserve Native Permissions

## Status

ACCEPTED — 2026-10-05. The maintainer explicitly accepted Bash capture
degradation for D-37(c), first for controlled modes, then for all modes once
`bypassPermissions` was shown to lose native deny rules too.

## Context

The Bash capture hook returned a shell wrapper as `updatedInput.command`.
Claude evaluates native permission rules against that replacement, so an
original-command allow can stop matching. An original-command deny can also
stop matching when a separate policy hook grants allow. Deny rules are checked
before `bypassPermissions` applies, so bypass sessions are exposed as well:
with installed Claude 2.1.287, a native deny blocked the original command but
let the wrapped one execute, with and without a policy-hook allow.
Instrumentation must not change the subject of a permission decision, and the
hook cannot see the merged deny/ask rules to know when rewriting is safe.

## Decision

Do not register a Claude `PreToolUse` boundary hook and never rewrite Bash
input, in any permission mode. Preserve the provider's original command and
its native and optional policy checks. Do not change the launch mode.

Keep the existing native Read/Grep/Glob/MCP output boundaries, which rewrite
output after permission evaluation.

## Alternatives

- Granting permission to the wrapper or adding a wildcard wrapper allow would
  weaken native deny/ask matching and is rejected.
- Capturing only in `bypassPermissions` (the first version of this decision)
  silently defeats native deny rules there and is rejected.
- Capturing only when no deny/ask Bash rule applies would require re-deriving
  Claude's merged settings and rule matching; a mismatch reopens the bypass
  silently, so it is rejected.
- A provider-supported execution-output boundary could preserve both capture
  and permissions, but needs separate lifecycle/platform and permission-parity
  validation. Post-only filtering cannot cover failed/background Bash output.

## Consequences and Validation

Claude Bash loses engine externalization, sanitization, deduplication, and
capture telemetry in every mode. That loss is explicitly accepted rather than
trading permission authority for output budgets. Other tools retain their
existing coverage.

Regression tests compare isolated installed-Claude sessions with and without
the boundary overlay, checking filesystem execution and the next model-facing
tool result for native allow/deny/ask and independent policy allow/manual/
deny/error cases in `dontAsk`, plus native deny, policy deny and native
deny with policy allow in `bypassPermissions`. Local deterministic model
responses validate real client behavior, not live model quality or future
provider versions.

Revisit when Claude offers a supported output boundary that preserves original
permission matching and failed/background output. Require native-client parity
in every permission mode before enabling capture there. Track this ceiling in
[TECH_DEBT.md](../../../TECH_DEBT.md).

## Related

[ADR-0164](0164-managed-codex-sandbox-posture.md) establishes the adjacent rule
that unsupported output capture degrades rather than weakening provider posture.
[Tool-result coverage](../../../docs/tool-result-budget.md) records current limits.
[Claude PreToolUse reference](https://code.claude.com/docs/en/hooks#pretooluse-decision-control)
documents permission evaluation against updated input.
