# ADR-0213: Operator-Withheld Agent Environment Variables

## Status

ACCEPTED — 2026-10-19. Amends [ADR-0169](0169-runtime-input-provisioning.md)
§4 (scope of stripped names).

## Context

Agents inherit the launching host's environment wholesale. In a downstream run a
real provider key in the TUI's environment reached coder sessions: a reviewed
test contract that assumed the project's dummy key failed, a pytest assertion
rendered a key prefix into a provider log, and keyed code paths could make live
calls from tests (D-75). The project cannot fully defend itself: its tests run
wherever the engine runs them.

The only engine-side name filter was ADR-0169's runtime-input scrub, derived
from task declarations and applied at provider launch, gate commands and
`run-live`. Supervisors kept the raw host environment, and the project commands
they run outside a provider session (integration tests at merge, deferred
validation lanes, `post_worktree_cmd`, index refresh, the checkpoint-summary
provider call, git hooks) inherited it. An operator could not say "these host
variables never reach agents or project commands".

## Decision

1. **Operator list.** `config.agent_env_deny` names host variables to withhold.
   It is empty by default. Operators set it at init (`--agent-env-deny`,
   repeatable, or the `<PREFIX>_AGENT_ENV_DENY` comma-separated seed) or later
   with `config set config.agent_env_deny A,B` (whole-list replace, empty
   clears, `--replace --reason` to change a non-empty list). Agents cannot set
   it. Names must be identifiers; process variables, the engine namespace and
   the Windows process names `USERPROFILE`, `SYSTEMROOT` and `COMSPEC` are
   refused.
2. **Process boundary.** Supervisors, and the deferred-validation runner (which
   an operator may also start), remove the listed names from their own
   environment at startup, before starting any process. Every child they start
   inherits a clean environment, so no per-call-site filter is needed.
3. **Session boundary.** The names stripped at provider launch (fresh and
   frozen snapshots), gate commands and `run-live` become the runtime-input
   names plus the list, read per call. A root env file mentioning a listed name
   is not copied into a worktree. An agent env file setting one fails a fresh
   launch preparation with `agent_env_denied:<NAME>`.
4. **Consistency fences.** A listed name and a validation prerequisite are
   exclusive, fenced in `Modify` like runtime-input names. The list is part of
   the prepared-validation config digest, so a change invalidates a prepared
   context. Names compare case-insensitively on Windows (the scrub, collision
   and copied-file checks), case-sensitively elsewhere.

## Consequences

- The list controls host inheritance only. A runtime input still delivers its
  declared value to its declared commands, even under a listed name; the
  feature does not promise that a name never occurs in any process.
- Newly listed names reach the next session, gate and `run-live` at once, and
  other processes at the next supervisor or runner start. A name removed from
  the list returns only in a fresh supervisor: a running one cannot restore a
  value it removed. Running sessions are not cleaned retroactively.
- A supervisor's kernel `/proc/<pid>/environ` keeps its initial block; the
  processes it starts are clean. Commands an operator runs from their own shell
  keep that shell's environment.
- Provider authentication is not guaranteed. A CLI that authenticates through a
  listed variable must use another mechanism, such as a stored login; setting
  the name in its env file is refused, not silently stripped.
- On Windows, runtime-input scrubbing also becomes case-insensitive.

## Alternatives Considered

- **Launch-only scrub (ADR-0169 scope).** Leaves merge integration tests,
  deferred validation and setup commands holding the variable.
- **Scrub each subprocess call site.** About ten sites, easy to miss new ones.
- **Allowlist.** Breaks unknown project needs; far larger blast radius.
- **A built-in default such as `OPENAI_API_KEY`.** Silently breaks providers
  that authenticate through that variable.

## Related Documents

- [Runtime inputs protocol](../../protocols/runtime-inputs.md)
- [Worktree management](../../protocols/worktree-management.md)
