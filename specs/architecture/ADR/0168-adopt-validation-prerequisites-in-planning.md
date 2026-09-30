# ADR-0168: Adopt Validation Prerequisites in Code Planning

## Status

ACCEPTED — 2026-09-30.

## Context

[ADR-0136](0136-validation-session-prerequisites.md) lets a task declare
`validation_prerequisites` that an agent session must pass before it claims or
launches work, but nothing produced declarations. No planner prompt named the
field, `init` never wrote the required `validation_execution: local` policy, and
a failed preflight only released the claim and retried silently.

In a downstream run, coders finished and committed work, then blocked at
`submit-for-review` on `pytest: not found` and `pre-commit: command not found`;
nineteen coding tasks queued behind them. The run declared no prerequisites, so
preflight never ran. `init` had also required a committed
`.pre-commit-config.yaml` without checking for the executable that runs it.

## Decision

Adopt the existing mechanism without inferring dependencies from shell commands,
which ADR-0136 rejected:

1. **Init requires `pre-commit` on PATH** once it has confirmed the committed
   config, and names the toolchain install command when it is missing.
2. **Init records the operator's assertion.** `--validation-execution local`, or
   `y` at the interactive prompt, writes `agent_tools.<cli>.validation_execution:
   local` for every CLI the pipeline's roles launch with by default (models.yaml,
   then the default CLI chain), listed before it is written. The assertion is a
   property of the tool, so it covers every resolved CLI, not only the coding
   pair's. `--yes` and non-interactive runs never make it.
3. **Planners declare when every consumer can check.** When every doer and
   reviewer role of the pairs consuming a code plan's `output[]` resolves by
   default to a CLI asserting `local`, the code-planner prompt requires one
   declaration per validation command, limited to what exists after worktree
   setup and before implementation (never an artifact the task builds), and the
   code-plan review checklist checks it. Otherwise nothing is rendered, because
   declarations would fail closed at the coder's or the reviewer's claim. An
   explicit `--cli`/`--profile` at agent start is not predictable at planning
   time and remains a claim/launch check.
4. **A new failed preflight observation raises an alert**
   (`VALIDATION PREFLIGHT FAILED`), deduplicated on the sanitized record fields
   (result, code, command and check indices, variable name, contract digest),
   and a stall diagnosis names current failed observations instead of pointing
   at supervisor logs.

## Consequences

- A missing validation tool is refused at claim, before any work, with a named
  alert, instead of blocking finished work at submission.
- Adoption is prompt-driven and depends on planner compliance, backed by the
  plan reviewer. A wrong declaration fails closed with an alert, not silently.
- Projects that do not assert `local` keep the legacy behaviour.
- Init now fails on hosts without `pre-commit`; tests stub the lookup.

## Alternatives Considered

- **Infer the executable from each command's first word.** Rejected by
  ADR-0136: stack-specific guessing, and the supervisor's PATH may differ from
  the agent's.
- **Schema requirement:** `set-task-output` rejects coding outputs with
  validation but no prerequisites. Stronger, but couples the planner's payload
  validation to the consumer tools' policy. Deferred until prompt-driven
  adoption shows gaps.
- **Default `validation_execution: local` in the provider catalog.** Rejected:
  ADR-0136 makes it an operator assertion.

## Related Documents

- [Validation prerequisite protocol](../../protocols/validation-prerequisites.md)
- [Configuration reference](../../../support-docs/CONFIGURATION.md#validation-execution-prerequisites)
