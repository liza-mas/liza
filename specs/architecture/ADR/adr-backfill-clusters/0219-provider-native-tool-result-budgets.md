# Cluster 0219 - Provider-Native Tool-Result Budgets

## Status

ADR generated: [0219](../0219-provider-native-tool-result-budgets.md). Selected 2026-10-25.

## Commit Set

- `997ae78a2` — feat(tool-results): enforce provider-native result budgets (DEV-778, ported from the downstream fork)
- `5049f50a0` — fix(toolresult): stop batch guard killing sessions on native results

Earliest author timestamp: 2026-09-24T04:06:51+02:00. Range: 2026-09-24 to 2026-09-26.

## Reconstructed Decision

A local, content-addressed tool-result boundary at each provider's native interception point: sanitized artifacts above a byte threshold, bounded digest to the model, slice retrieval by hash.

## Evidence

- `997ae78a2` body read in full; `docs/tool-result-budget.md` coverage, limits and identity sections read.
- ADR-0164 status states it was implemented "as part of the tool-result budget change (DEV-778)"; ADR-0178 later removed Claude Bash capture. Neither records the parent decision.

## Related Decisions

ADR-0164, ADR-0178, ADR-0121.

## User Context — 2026-10-25

Trigger: context flooding. Built downstream first by mistake. Alternatives not supplied (reconstructed).
