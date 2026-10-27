# 229 - Policy-Time Canonical Provider Keys

## Status

ACCEPTED.

## Context

Reviewer provider diversity and provider-audit anomaly grouping compared the
tool name recorded for an agent (`agents[].provider`, `approvals[].provider`,
anomaly `Details["provider"]`). Two tools can launch one provider: the catalog
gives `codex` and its virtual variant `codex-acp` the key `codex`
([ADR-0121](0121-unified-provider-runtime-catalog.md)), and `agent_tools` may
declare one `provider_key` for several custom tools
([ADR-0096](0096-catalog-backed-provider-registry.md)). Approvals from such
tools satisfied diversity although they share a backend's blind spots
([ADR-0046](0046-review-quorum.md)), and provider-audit anomalies of the
aliases were counted as separate groups, so a provider-wide degradation could
stay below the threshold or be judged on one alias's health only. The goal's
behaviour change C2 asks for comparison on the effective provider key, derived
when a policy is evaluated.

## Decision

### D1 - One pure key query below ops

`internal/agentlaunch` exports `NewProviderKeys(base, config)` and
`ProviderKeys.Key(tool)`. `base` is the catalog-derived registry as
`BuiltInAgentTools` returns it; `config` supplies the `agent_tools`
overrides, applied by the registry's own overlay (`overlayAgentTools`, shared
with `AgentToolRegistry`), so the catalog key, the override (including its
empty-versus-nonempty rule) and the tool-name default are those the launch
resolver computes. `Key` trims whitespace like `ResolveLaunchPlan`'s
`ProviderKey`; it does no lowercasing, suffix stripping or alias table. An
unregistered tool keeps its own name and an empty recorded identity stays
empty. The query reads no catalog, environment, executable or state.

### D2 - Two identities

The recorded tool name keeps addressing configuration, review-slot CLI/model
bindings, launch, display and persistence; the key decides provider equality
and grouping only. Nothing persists or migrates the key, and no stored
provider changes. Missing-provider checks (an empty claimer or doer provider)
stay on the recorded identity.

### D3 - Consumers

Equality is the only policy delta. The consumers are the reviewer claim
(partially-approved preference, fresh-submission satisfiability, doer filter
and alternative-reviewer pool; also pool-repair projections), the merge gate
(approval diversity and reviewer-pool cardinality) and provider-audit analysis
(grouping, and which registered agents belong to a group). Preferred diversity
stays a soft preference: aliases stop counting as distinct providers, and no
merge or verdict is refused. In analysis the exact health epoch stays raw and
generation-specific (agent ID, stored tool name, PID, registration time), so
the key never makes another tool's health record valid for an agent.

### D4 - Inputs are captured outside transactions

The catalog-derived base is captured once per evaluation before any blackboard
transaction (`BuiltInAgentTools` keeps its loader, cache and fallback); the
keys are built from the configuration of the state actually evaluated: the
locked state in a claim or `analyze`, the read snapshot in a merge batch, the
projected state in pool repair and watch. `internal/analysis` stays pure and
receives a `ProviderKeyFunc`; a nil func compares recorded values exactly.

### D5 - Display of a mixed group

Text naming a provider group (analysis evidence and explanations, the merge
`diversity_reason`) uses the lexicographically smallest recorded value of the
group, so a single-value group renders as before and a mixed alias group is
deterministic. Original anomalies and approvals are never rewritten.

## Consequences

Existing state evaluates under the rule: persisted approvals and anomalies are
compared by current configuration, even when their agents are no longer
registered, and an `agent_tools` change takes effect at the next evaluation.
Stop-signal matching is unchanged here; behaviour change C3 consumes the same
query for it.

This refines the registry boundary of ADR-0096 and ADR-0121 and keeps
ADR-0046's enforcement points; it supersedes none of them.
