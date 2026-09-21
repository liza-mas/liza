# Cluster 0154 - Decision Records in the Universal Contract

## Status

ADR generated: [0154](../0154-decision-records-in-core-contract.md). Selection approved on 2026-09-21; no separate user intent supplied.

## Commit Set

- `a91a9fb59` — feat(contracts): make decision records part of the universal contract

Earliest author timestamp: 2026-09-21T11:49:33+02:00. Decision implementation range: 2026-09-21.

## Reconstructed Decision

Split the practice rather than naming a path. CORE states the artifact class and when it applies, always qualified by "where the project keeps one": decision records join specs, docs and lessons as durable memory, and a structural decision that constrains later work becomes a Doc Impact category — declaring impact rather than instructing creation. `architecture-planning` takes the write half; survey reads prior decisions, and a pattern deviation or an upstream-named ADR candidate routes into the record. G2.4 keeps its trigger and its architectural-issues item.

**Rationale provenance:** Commit-evidenced. The body states the zero-mention audit, the G1.1 constraint that rules out naming a path in CORE, and the reasoning for reusing DoR/DoD gates over adding a rule or tier entry.

## Evidence

- `a91a9fb59` commit body, read in full.
- Self-referential note: this record exists because of the batch that produced it, and the decision it documents is the one that makes such records part of the contract rather than of one project's guardrails.

## Related Decisions

ADR-0153 (goal-writing elicits the ADR convention), ADR-0132 (human-owned goal decisions), ADR-0004 (dual-mode contract architecture). GUARDRAILS G1.1 is the binding constraint.

## User Context — 2026-09-21

None supplied.

## Remaining Historical Gaps

No record of whether a configurable ADR-path field was considered as a third option between guardrails and CORE.
