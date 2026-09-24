# 135 - Validation Session Prerequisites

Date: 2026-09-12

## Context and Problem Statement

Issue #154 reports sessions claiming canonical validation work without the
required variables, interpreter dependencies or executables. A successful
worktree setup child cannot export environment changes into a persistent
supervisor. A dependency merge also does not prove an existing provider session
can execute the newly declared validation commands.

[ADR-0072](0072-declared-validation-commands.md) records the commands,
[ADR-0117](0117-fail-closed-worktree-readiness.md) enforces worktree setup, and
[ADR-0130](0130-generation-fenced-agent-authority.md) fences current registration
authority. Session capability is an additional boundary spanning all three.

## Considered Options

1. Probe only in worktree setup: checks a child context and misses retained
   sessions and await-based reclaim.
2. Gate only supervisor launch: prevents a provider start but leaves direct
   assignment and ownership renewal claiming work in an unsuitable context.
3. Declare cheap command-specific prerequisites and enforce them at assignment
   and launch using the effective session environment. Chosen.

Running the full canonical suite before each claim would be expensive and can
have destructive effects. Inferring dependencies from arbitrary shell commands
would introduce unsupported stack-specific assumptions.

## Decision Outcome

Add optional task/output `validation_prerequisites`, associating variable names,
executable lookup and bounded argv probes with exact canonical command text.
Require complete, unique, non-vacuous associations when declared. Retain legacy
behavior for undeclared tasks. Structured declarations propagate through task
input, output and generated children with shared model/state validation.

Require an explicit per-tool `validation_execution: local` assertion before
checking a declared task. This states that provider tools use the supplied local
environment/worktree; it does not weaken permissions or infer sandbox equivalence.
Unset/unsupported contexts fail closed. `artifact-only` remains unsupported until
the separately deferred signed-artifact trust contract exists.

Models own declarations and safe evidence; `internal/sessionvalidation` resolves
environments and runs probes; ops owns assignment identity/revalidation; agent
adapters freeze and consume launch snapshots. No ops-to-agent dependency is
introduced. CLI and ACPX, including interactive paths, consistently refresh
env-file overlays. Explicit unreadable overlays fail; missing embedded or fetched
catalog defaults remain optional.

Check fresh/resumed doer and reviewer ownership, await-based executable reclaim,
and final provider start. Probes run outside blackboard locks; final transactions
revalidate generation, contract, worktree/review commit and integration revision.
Preserve lifecycle-lock ordering and work/review evidence on failure. Operator
direct assignment cannot establish another session's context; requeue and let
the intended supervisor preflight. In-session ops use current branded identity
and generation within the existing trusted-agent model, not cryptographic
process authentication.

Keep one latest sanitized observation per agent/task. Fingerprint environments
with a random process-private HMAC key; cross-process/restart comparisons are
unverified. Records are audit evidence, never reusable success authorization.
Retry unchanged failures after a bounded interval, exclude recent failures only
for their task, and report capability-blocked work without equivalent spawn loops.
Protected retained ACPX sessions use the current process's scoped identity.

## Consequences

- Required capability becomes an explicit project contract; runtime code remains
  stack-agnostic and does not execute full suites or acquire credentials.
- Incorrect or unavailable execution contexts fail before consuming executable
  ownership or provider work. Repairs require a fresh check, including after
  environment refresh or dependency merges.
- Operators must declare local execution truthfully and supply cheap repeatable
  probes. Unsupported contexts can stall the pool with safe named causes.
- Probe output is discarded, limiting diagnosis to fixed codes and indices until
  an operator investigates in the intended context.
- Optional signed validation artifacts are deliberately deferred; see
  [the debt entry](../../../TECH_DEBT.md#signed-validation-artifacts-for-unavailable-execution-contexts).

## Related Documents

- [Validation prerequisite protocol](../../protocols/validation-prerequisites.md)
- [Blackboard schema](../blackboard-schema.md)
- [Configuration reference](../../../support-docs/CONFIGURATION.md#validation-execution-prerequisites)
