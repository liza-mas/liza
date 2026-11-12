# ADR-0246: Doctor Verifies What Setup and Init Produced

## Status

ACCEPTED — 2026-11-12. Ported from the execution-engine fork (its ADR-0248).
Builds on [ADR-0246](0246-degraded-outcomes-as-checks.md); specified in
[Outcome Checks](../../protocols/outcome-checks.md#doctor).

## Context

A missing global contract link went unnoticed on a user's machine: nothing
reports what `setup` and `init` failed to produce, or what was removed since.
`toolchain doctor` covers satellite tools only. A user, an agent or a script
needs a check it can run at any time, so the check must be fast and must not
change the machine it inspects.

## Decision

1. **A separate, read-only command.** `doctor` reports the end state as
   ADR-0246 checks and writes, repairs and fetches nothing: it reads the
   provider catalog cache instead of loading it (loading refreshes it), and it
   skips Semble's offline model query, which writes a temporary fixture.
2. **No automatic runs.** Neither `setup` nor `init` runs `doctor`. Each
   command reports only what it was asked to apply (ADR-0246); the user, an
   agent or a script runs `doctor`. With doctor inside `init`, an incomplete
   setup would make every `init` exit 4, and callers that read a non-zero exit
   as failure would stop.
3. **Shared read-only inspection, no copies.** Doctor never calls placement or
   repair code. Link state comes from `inspectLink`, which contract placement
   and setup use too; asset paths come from functions the asset
   writers use; index hooks from `pairingindex.CheckInstalled`, which
   `CheckActivation` runs first. A writer and its check cannot drift apart.
4. **Scope by ownership.** Global checks always run; a provider's contract is
   checked only when named with `--provider`; repository checks only with
   `--repo`; toolchain checks only for the tools a configured profile
   selected, manual-only tools excluded.
5. **Severity and exit as ADR-0246.** Every doctor check is `warn`.
   Exit 0, 4 or 1, like `init` and `setup`: unlike `toolchain doctor`, the
   checks are of things that should be in place on a configured machine.

## Consequences

- Every new `setup` or `init` step that installs something adds its `doctor`
  check in the same change, reading the path through the function its writer
  uses.
- Presence, not content: a hook set or global file that is present but stale,
  and a prompt the user declined, are not told apart from a correct install.
- Doctor statuses repeat `setup.*`, `init.*` and `contract.placement` step
  checks for the same paths only when both are run; nothing merges them.

## Alternatives Considered

- **Doctor at the end of `setup` and `init`**: every `init` degraded by state
  another command owns.
- **Calling `verifyPlacement`**: it places links and writes the repository
  exclude file.
- **Reusing `providers.Load`**: it fetches the catalog and rewrites the cache.
- **Content drift for index hooks** (`CheckActivation`): its expected plan
  depends on configuration doctor does not hold for every repository, so a
  false "drifted" is likely; the orchestrator already repairs drift for MAS.
