# ADR-0169: Runtime Input Provisioning

## Status

ACCEPTED — 2026-10-01. Amends ADR-0072, ADR-0134, ADR-0136 and ADR-0168.

## Context

Live validation commands need inputs the pipeline did not model: single-use
fixtures spent by each run, and credentials. In a downstream run the operator
regenerated consumptive fixtures by hand seven times in about 36 hours, outside
the pipeline. Reviewers re-running a canonical command spent the doer's fixture.
Credentials sat in agent env files, so a session could print them and a receipt
could capture them.

Three properties of the manual process must survive automation:

1. the validated agent never shapes its own evidence inputs;
2. credentials stay out of agent sessions and agent-visible output (accidental
   disclosure; deliberate reading by a same-user agent is an accepted residual);
3. a human produces the inputs and owns new credential scopes.

## Decision

1. **Declaration.** Strict acceptance tasks and planning `output[]` entries
   declare `runtime_inputs`: id, the canonical commands that use the input, a
   registry recipe, `single_use` or `reusable`, `secret`, `env` names, `files`
   and `after`. The key set is closed. Declarations join the allocation
   predicate, so only an independently reviewed planning output, or a same-pair
   replacement keeping them equal, can carry them. Recipes must exist in the
   registry named by `config.runtime_input_registry`, read at the integration
   commit.
2. **Ledger.** The operator records hand-produced instances with
   `provision --record` from a `KEY=VALUE` envelope outside the repository.
   `state.runtime_inputs` keys each instance by an HMAC, under a persistent
   operator key, of its canonical materialization (names with values; file
   artifacts by content). It stores names, the envelope path and state, never
   values or unkeyed digests. Instances are permanent: `consumed` and
   `invalidated` are terminal, re-recording a materialization returns the
   existing instance, and `Modify` refuses any other transition.
3. **Gate acquisition.** The strict gate acquires inputs after its
   non-executing checks and before the first command. One transaction consumes
   every `single_use` instance durably before launch, or records a refusal
   (`consumed`, `invalidated`, `missing`, `unavailable`) that runs nothing and
   spends no review cycle. A changed artifact invalidates its instance.
4. **Broker and masking.** Each gate command receives only the variables of
   inputs that list it. Secret values are masked in receipts, failure excerpts
   and archives. Every runtime-input name is stripped from provider launches,
   gate commands and `run-live`; an env file mentioning one is not copied into
   a worktree, and an agent env file setting one fails the launch. Runtime-input
   names exclude process and engine variables, and they and session
   prerequisite names are exclusive classes, fenced in `Modify`. Only a planning
   task's output consumed by coding pairs may declare runtime inputs.
5. **`run-live`.** Doers and reviewers run local live subsets with the task's
   `reusable` inputs only. A canonical command that uses a `single_use` input is
   refused before launch.
6. **Routing.** A doer claim or submission refusal blocks the task with the
   operator action (record, then `unblock-task`). An `update-review-commit`
   refusal preserves the task and releases the calling reviewer's claim. Every
   refusal appends a deduplicated `runtime_input_unavailable` anomaly, which the
   circuit breaker does not count: it signals missing provisioning, not agent
   failure.

The [runtime protocol](../../protocols/runtime-inputs.md) specifies limits,
codes and boundaries.

### Amendments

- **ADR-0072.** Doers and reviewers still share the canonical commands, but
  nobody runs a command that uses a `single_use` input outside the gate;
  reviewers rely on its receipt, and use `run-live` for the rest.
- **ADR-0134.** Acquisition and its pre-launch refusal are part of the strict
  gate on both `submit-for-review` and `update-review-commit`.
- **ADR-0136 / ADR-0168.** Runtime-input readiness (resolvable instances, no
  session value) is checked at the same claim and launch points, without a
  `validation_execution` policy. Prerequisites cannot name a runtime-input
  variable; a legacy one that does fails as `runtime_input_scrubbed`.

## Consequences

- Consumption is at-most-once and durable before launch: a failing or timed-out
  run still spends its fixtures, and the next attempt needs a new instance.
- A runtime-input task is normally BLOCKED at its first claim until the
  operator records instances; recording before the claim avoids it.
- The operator key is a recovery dependency. Losing it fails closed while
  instances exist; operators back it up with their secret sources. Rotation is
  deferred.
- A `files` artifact is verified before launch but opened by the command, so an
  operator-owned file changing mid-run is an accepted residual.
- A gate that relied on an undeclared ambient variable loses it once that name
  is declared as a runtime input anywhere.
- The ledger grows without bound; see TECH_DEBT.

## Alternatives Considered

- **Envelope digest as identity.** Rejected: reformatting an envelope, or moving
  a file artifact, would mint a fresh identity for a spent fixture.
- **Unkeyed SHA-256.** Rejected: a low-entropy value could be guessed offline
  from state (INVARIANTS §9).
- **Consume per command, lazily.** Spends fewer fixtures on early failure but
  loses "refused before any command runs" for later inputs.
- **Keep credentials in agent env files, mask output.** Leaves the session
  holding them, the harm being removed.
- **Provisioner, approvals, source-bound invalidation, churn budget.** Deferred
  (TECH_DEBT); the recorded-instance path stands alone.

## Related Documents

- [Runtime inputs protocol](../../protocols/runtime-inputs.md)
- [Acceptance evidence admission](../../protocols/acceptance-evidence.md)
- [Validation session prerequisites](../../protocols/validation-prerequisites.md)
- [ADR-0134](0134-acceptance-evidence-admission.md), [ADR-0136](0136-validation-session-prerequisites.md)
