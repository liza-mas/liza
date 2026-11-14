# Project Guardrails

## Tier 1 (Hard Constraints)

### Invariant compliance

When a change touches one of the triggers below, check `INVARIANTS.md`: use its Protection Matrix
to find the invariants the change's blast radius reaches, and preserve each one. Surface a
conflict by naming the invariant and asking for a waiver or another direction; never proceed
silently. A new invariant follows the file's Altitude rule.

**Triggers:** `internal/api/`

**Test:** "Does this change preserve every invariant it touches?"
