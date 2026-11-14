# Invariants

Properties that must always hold in this project, what each protects against and how it is
enforced. Read it when a guardrail's trigger sends you here; it is not an initialization read.

## Altitude rule

An invariant states a property of the system and what it protects against, at the level of a
principle. The bug-specific detail (the case, the input, the file) lives in its enforcement: a
type, a lint, a test or a guard. Every invariant records how it is enforced; one with no
enforcement yet says `unenforced` in its Enforced cell and stays listed, so the gap stays visible.

## Invariants

An invariant may carry an ID: the first token of its Invariant cell, in backticks, matching
`[A-Za-z][A-Za-z0-9._-]*` (no `/`, no `#`). Other records cite the invariant by that ID.

| Invariant | Protects against | Enforced |
|-----------|------------------|----------|

## Protection Matrix

| Threat | Primary defenses |
|--------|------------------|
