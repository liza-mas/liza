# 149 - Per-Role Instruction Budget Measured Through the Compositor

## Status

ACCEPTED — implemented 2026-09-14 to 2026-09-21; backfilled 2026-09-21.

## Context and Problem Statement

Two proposals depended on measuring rendered prompt bytes, and neither had an
instrument. A payload-reduction proposal set its acceptance criterion as "bytes per
rendered prompt per role before/after on identical fixtures"; without a harness, a
reduction could ship with no evidence it reduced anything, and the pre-change
baseline would be unrecoverable once the first change landed. Later, an
instruction-release goal capped growth at 5% per role variant, in both the rendered
prompt and the combined mandatory reads, measured against a baseline captured
before any instruction edit. Nothing measured either column per variant.

A first attempt at measuring this surface grepped one string, matched only prose
inside inlined carriers, and was wrong by four orders of magnitude. A second
attempt fed the carrier block to the benchmark as a pre-rendered string, so it
never passed through `reconcileAndRenderCarriers` — the function any payload
reduction would live in. A harness that cannot observe the change it exists to
measure is not a harness.

## User-Confirmed Intent

The 5% ceiling came from agent proposals rather than from an external requirement
or a measured threshold. The user confirms the overruns the release ultimately
reported — up to +12.2% for `us-reviewer`, and above 5% for five further role
variants — were accepted deliberately: the figures were reported for a scoped human
decision, and no binding rule was removed in order to fit the ceiling.

## Considered Options

1. **Review prompt size by inspection** — judge growth from the diff.
2. **Measure an aggregate prompt size** — one number for the pipeline, averaged
   across roles.
3. **Measure every role variant through the real compositor, against a committed
   baseline, with an opt-in assertion gate.**

## Decision Outcome

Chose **Option 3**, built in two packages.

`internal/prompts/promptbench` measures rendered payload. Its fixture is generated,
not vendored: it reproduces a real run's measured proportions — carrier count and
size, carrier heading structure, per-site dependency volume — with filler text, so
no external project's specification content enters the repository;
`testdata/run-calibration.json` records the measured figures as numbers only.
Carrier *internal structure* is calibrated, not just carrier size, because a
fixture whose carriers are structureless bulk can measure reference-by-path but not
section-scoped inclusion. Measurement enumerates all four dependency render sites
and reports sites measuring zero as zero, guarded by `TestSitesAreEnumerated`.

To measure the compositor rather than an imitation of it, carrier rendering moved
to `internal/referencecontract` as `RenderCarriers` over exported `Carrier` and
`Reference` types — a leaf package the benchmark can import without a cycle, with
byte-identical behavior.

`internal/prompts/rolebudget` measures instruction text. It renders every role in
the embedded pipeline through the same fixture, with the reference-context payload
blanked: that block is run data and promptbench's subject, and at roughly 265 KB it
would make a 5% instruction ceiling meaningless. Decomposition-root variants come
from the compositor's own rule, `agent.TaskContextSections`, exported rather than
re-derived. The orchestrator is measured per wake trigger through
`prompts.RenderWakeInstructions`, with `TestWakeTriggersCoverEveryWakeTemplate`
keeping the trigger list and the template set aligned. Mandatory reads are the
role's skills, mandatory docs, and the shared references those skills link, each
file counted once. Reports are per variant, never averaged, and flag variants
absent from either side.

`TestRoleBudget` asserts the ceiling only when `baseline.json` carries
`gate: true`. The baseline was first committed with `gate: false` so a release could
be assembled and reported package by package; the final package flipped it.
Baseline: 27 variants — 16 doer/reviewer including six decomposition-root variants,
and 9 orchestrator wakes.

## Rationale

The fixture is committed *before* the reduction it will measure, because
discovering a missing pattern afterwards would invalidate the baseline the fixture
exists to preserve. The same reasoning drove calibrating carrier structure and
enumerating every render site up front.

Per-variant reporting rather than an average follows from what the ceiling is for:
an average hides exactly the case the cap exists to catch, a role with a small read
set absorbing a large instruction addition. The observed spread — `us-reviewer` at
+12.2% while others stayed under — is the evidence for that choice.

The `gate` flag exists because measurement and enforcement have different
schedules. A multi-package release needs the instrument before it can decide what
the ceiling should permit.

## Consequences

- Payload and instruction changes carry byte evidence rather than assertion. The
  reductions this instrument measured are recorded separately
  ([ADR-0139](0139-assigned-carrier-reference-rendering.md)).
- The 5% ceiling is enforced in CI once `gate: true` is set, and a role variant
  added without a baseline entry is flagged rather than silently unmeasured.
- The fixture must be regenerated if the run proportions it reproduces stop being
  representative, and regeneration invalidates comparability with prior baselines.
- The ceiling is an agent-proposed number, not a derived one. It has already been
  exceeded by deliberate decision; treating it as inviolable would misread it.
- Blanking the reference payload means the two instruments measure disjoint
  columns and must not be summed.

## Evidence and Reconstruction

Sources: commits `19eb7554f`, `70c9b9445`, `6cd54e720`, `f2cce1888`, `fb4a0e8a2`,
with the gate flip recorded in `20ccc2858`. The user confirmed on 2026-09-21 that
the 5% figure came from agent proposals and that the overruns were a deliberate
scoped decision. Historical alternatives are reconstructed.

---
*Reconstructed from commits `19eb7554f`..`fb4a0e8a2` (2026-09-14 to 2026-09-21).*
