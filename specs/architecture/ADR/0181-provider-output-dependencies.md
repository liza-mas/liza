# 181 - Provider Output Dependencies

## Status

ACCEPTED

## Context

A Scope can require another architecture's code plans before their child tasks
exist. Existing concrete dependencies require existing IDs; same-transition
inheritance only sees an already expanded upstream. A dependency on the merged
architecture therefore admits the consumer too early. Ordinary downstream
dependencies are forbidden by pipeline direction, so a later concrete edge
cannot represent every intended cross-stage prerequisite.

## Decision

Amend [dependency-direction and claimability invariants](../../../INVARIANTS.md#33-claimability)
with a narrow explicit exception. Task, OutputEntry and AddTaskInput accept
optional JSON/YAML `provider_dependencies`:

```json
[{"provider_task":"provider-architecture","transition":"architecture-to-code-plan","outputs":[0,2]}]
```

The names are configured examples. Each entry names an existing provider, a
configured per-subtask transition from its role-pair, and a nonempty set of
distinct nonnegative output indexes. Duplicate declarations are refused. Output
may be unborn; bounds are checked once known, including later output writes.
Selected outputs with nonempty `kind` are refused: deduplication can remap the
expected child identity, so positional references cannot safely identify them.

One readiness interpretation serves discovery, diagnostics and committing
claim/reclaim/unblock checks. Admission requires the provider `MERGED`, its
named transition executed, and every selected child present with the configured
target role-pair, correct parent and `MERGED`. `APPROVED`, missing/partial
generation, invalid bounds, retired providers and unavailable resolution fail
closed. Unselected outputs do not delay admission.

Declarations stay visible after satisfaction. Generation deep-copies them;
crash recovery refuses conflicting or claimed-child changes. A missing declaration
copy keeps the affected child held and the producer output live even with an
executed transition marker, until recovery restores the reviewed copy. Consumer replan
and concrete-edge repairs preserve their intent. Combined cycle validation
includes provider waits, pending-parent production and projected future children
with sibling, concrete, inherited and provider dependencies. Later output
authoring and generation validate the same graph before publishing a candidate.

This field does not overload sibling `depends_on`, concrete `task_depends_on`
or selective `inherit_inputs` ([ADR-0137](0137-selective-dependency-generation.md)).
Ordinary downstream edges remain invalid. No blanket architecture-edge promotion
or arbitrary Scope-prose parsing is introduced. Producers, reviewers and
orchestrator plan-check reconcile every task/code-plan precondition with the
appropriate structured field before handoff.

## Recovery and Compatibility

Retiring/replanning a referenced provider or selected child is refused while
active tasks or live output declarations name it; historical terminal consumers
remain audit evidence. Do not guess equivalent positions on a replacement.
If the provider must change, cancel or replace the referencing consumer through
authorized lifecycle operations first. A reviewed replacement must explicitly
omit that reference or name another intended provider; retaining the same
reference retains the refusal. Retire any live producer declaration likewise,
then retry the provider change. Concrete `retarget-dependency` does not rewrite
provider declarations.

Omitted fields preserve existing runtime behavior. Legacy prose-only waits need
a reviewed replacement or reauthored plan carrying the field, followed by
authorized child creation/replacement. The orchestrator repairs the missing
declaration before restoring a blocked consumer; unblocking against a merged
architecture alone repeats the failure. No automatic migration or live-state
edits are supported.

## Consequences and Alternatives

- Another scheduling field is required to retain unborn, cross-transition
  prerequisite intent; a shared resolver limits inconsistent admission rules.
- Future-graph projection must match actual generation, including inherited and
  sibling edges. Focused tests must compare these behaviors.
- Kind selection and live-provider retirement are bounded refusals, with reviewed
  reauthoring as recovery rather than silent retargeting.
- Prompt guidance alone cannot enforce an unborn wait; prose parsing cannot
  establish reliable scheduling authority. Promoting all architecture edges
  would over-constrain independent scopes. Repair only after BLOCKED preserves
  the wasted claim that this declaration prevents.
