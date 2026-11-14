# Project Guardrails

Project-specific constraints for Liza agents.
Uses the tier system from the core contract (CORE.md).

**Troubleshooting reference:** See `.liza/SUPPORT.md` for task states, recovery commands, and common failure patterns.

## Tier 0 (Inviolable)
<!-- Constraints that must NEVER be violated. Triggers mandatory halt (RESET). -->

## Tier 1 (Hard Constraints)
<!-- Suspended only with explicit waiver. -->

### Invariant compliance

When a change touches one of the triggers below, check `INVARIANTS.md`: use its Protection Matrix
to find the invariants the change's blast radius reaches, and preserve each one. Surface a
conflict by naming the invariant and asking for a waiver or another direction; never proceed
silently. A new invariant follows the file's Altitude rule.

**Triggers:** <!-- List the areas of this project whose changes must be checked against INVARIANTS.md (for example persisted data, concurrency, authorization). -->

**Test:** "Does this change preserve every invariant it touches?"

## Tier 2 (Strong Defaults)
<!-- Best-effort under pressure. -->

## Tier 3 (Preferences)
<!-- Degraded gracefully. -->

---

Secret word: On-rails
