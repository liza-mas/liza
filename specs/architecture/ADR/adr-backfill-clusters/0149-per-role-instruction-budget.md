# Cluster 0149 - Per-Role Instruction Budget Measured Through the Compositor

## Status

ADR generated: [0149](../0149-per-role-instruction-budget.md). Selection approved on 2026-09-21; user intent supplied on 2026-09-21.

## Commit Set

- `19eb7554f` — feat(prompts): add prompt payload benchmark with calibrated fixture
- `70c9b9445` — refactor(prompts): measure the compositor, not an imitation of it
- `6cd54e720` — test(prompts): capture per-role prompt and mandatory-read baseline
- `f2cce1888` — test(promptbench): calibrate ancestor-carrier references
- `fb4a0e8a2` — test(prompts): measure assigned-peer payload savings per role

Gap commits (baseline rebases): `c80ed0f90`, `528d64c95`. Gate flip: `20ccc2858` (recorded under cluster 0150).

Earliest author timestamp: 2026-09-14T13:36:13+02:00. Decision implementation range: 2026-09-14 to 2026-09-21.

## Reconstructed Decision

Build two instruments rather than judging prompt size by inspection: `promptbench` for rendered payload (generated calibrated fixture, all four render sites enumerated, carrier rendering extracted to `internal/referencecontract` so the benchmark measures the real compositor) and `rolebudget` for instruction text (every role variant, reference payload blanked, per-variant reporting, ceiling asserted only when `baseline.json` carries `gate: true`).

**Rationale provenance:** User-confirmed: the 5% ceiling came from agent proposals, and the recorded overruns (to +12.2%) were a deliberate scoped decision rather than an oversight. Commit bodies supply the instrument design reasoning.

## Evidence

- Commit bodies for all five commits, read in full.
- `19eb7554f` records the earlier measurement that was wrong by four orders of magnitude; `70c9b9445` records the pre-rendered-string flaw that made the harness blind to its own subject.
- Baseline figures: 218,537 bytes pre-reduction; 27 measured variants (16 doer/reviewer, 9 orchestrator wakes).

## Related Decisions

ADR-0139 records the reductions this instrument measured; ADR-0150 flipped the gate as part of the W0–W5a release. ADR-0027 (contract compression for MAS context) is the earlier, unmeasured ancestor of this concern.

## User Context — 2026-09-21

Agent proposals [origin of the 5% ceiling]. Overruns to +12.2% accepted deliberately; no binding rule removed to fit.

## Remaining Historical Gaps

No record of why 5% rather than another figure, beyond its origin in agent proposals. `2c23acddf` (duplicate-reference elision) is treated as a reduction under ADR-0139 rather than part of this instrument cluster; that boundary is a judgment call.
