# ADR-0246: Degraded Outcomes as Checks with a Dedicated Exit Code

## Status

ACCEPTED — 2026-11-12. Ported from the execution-engine fork (its ADR-0247). Specified in
[Outcome Checks](../../protocols/outcome-checks.md).

## Context

`init` and `setup` reported an outcome that was complete but not what was
asked for, such as a contract activated in the repository because the user's
own file holds the provider's global location, only as a `Warning:` line on
stderr, and exited 0. Placement and setup functions returned `error` or nil,
so a partial outcome never reached the command boundary that chooses the exit
code. No caller could tell "done" from "done, but not what you asked for": a
script reads exit 0 as success, an agent does not relay its tool output, and
the animated TUI discards stderr.

Two structured formats already existed: the `--json` envelope and
`toolchain.DoctorCheck`. Whatever is chosen is what agents and scripts
parse, and a later read-only `doctor` command is to emit the same format.

## Decision

1. **One check type** in `internal/jsonout` (`Check`, `CheckDefinition`,
   `Report`), generalised from `DoctorCheck`, inside the unchanged envelope as
   `result.checks`. `toolchain doctor` emits it.
2. **Severity by definition.** `ok` is `info`; any other status takes the
   severity its definition declares.
3. **Exit 4 for degraded** (`ok:true`, a non-ok check), in human and JSON mode.
   Hard failures keep exit 1 and `ok:false`. A degraded outcome is never a
   `warnings` string.
4. **Collection without signature churn.** An optional `Report` field on the
   init and setup params (nil collects nothing) rather than new return types.
5. **Contract placement derived from final state.** One `contract.placement`
   check per provider compares where the contract ended up with what its
   action accepts, so a step that failed and was recovered is not misreported,
   and every placement path ends in a check.
6. **JSON-mode stdout is the envelope only.** The run's human stdout is routed
   to stderr.
7. **`toolchain doctor` stays report-only** (exit 0): manual-only tools are
   never `ok` by design.

## Consequences

- A caller can tell success, degraded and failure apart from the exit code
  alone, and read what was not applied from `--json`.
- A human `init` that degrades now exits 4; scripts that stop on a non-zero
  exit stop there. Users who keep their own `~/.claude/CLAUDE.md` get exit 4
  on every `init` until they choose `local` or `rename`; `warn` tells callers
  it does not block.
- The format is a public contract of the CLI. The golden test pins it; a
  change must be deliberate and announced to callers that parse it.
- Asset and setup checks are added at their warning sites, so a new warning
  site needs a check of its own; placement checks do not.

## Alternatives Considered

- **`(Report, error)` return types**: clearer ownership, about 300 call sites
  rewritten for no behavioural gain.
- **Checks emitted at each warning site** for placement: misreports a step a
  later one recovers (the local fallback after both locations are occupied).
- **Capturing stderr into `warnings`**: keeps semantics in prose and is
  excluded by the requirement that a degraded outcome be a check.
- **Applying exit 4 to `toolchain doctor`**: every default-profile run would
  be degraded because of manual-only tools.
