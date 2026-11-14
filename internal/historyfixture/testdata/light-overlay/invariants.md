# Invariants

Properties that must always hold in this project, what each protects against and how it is
enforced. Read it when a guardrail's trigger sends you here; it is not an initialization read.

## Altitude rule

An invariant states a property of the system and what it protects against. Detail specific to one
bug belongs in the enforcement (a type, a lint, a test or a guard), not in the invariant.

## Invariants

An invariant may carry an ID: the first token of its Invariant cell, in backticks, matching
`[A-Za-z][A-Za-z0-9._-]*` (no `/`, no `#`). Other records cite the invariant by that ID.

| Invariant | Protects against | Enforced |
|-----------|------------------|----------|
| `T0.2` A closed store never accepts a write. | Lost or corrupted data after shutdown. | Enforced: the store's write guard and its negative test. |
| `T0.21` Every key is checked at the boundary that receives it. | Malformed keys reaching storage. | unenforced |
| `S1.1` Retries are bounded in count and in delay. | Unbounded load on a failing dependency. | Enforced: the retry policy type rejects an unbounded value. |
| `R1.1` Every report names the commit it describes. | Reports that cannot be reproduced. | Enforced: the report writer requires a commit. |
| Configuration is read from one place. | Two sources of truth for one setting. | Enforced: a single loader and its test. |

## Protection Matrix

| Threat | Primary defenses |
|--------|------------------|
