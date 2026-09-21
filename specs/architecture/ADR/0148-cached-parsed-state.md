# 148 - Cached Parsed State with Per-Call Deep Copies

## Status

ACCEPTED — implemented 2026-09-14; backfilled 2026-09-21.

## Context and Problem Statement

Waiting supervisors re-read unchanged state on every tick. `ReadCached` cached the
raw YAML bytes only, so every call still ran `yaml.Unmarshal` over the whole state.
At megabyte scale, roughly twenty idle supervisors spent their CPU parsing
identical bytes, starving the processes that held the state lock and producing the
10-second lock timeouts seen during long runs.

The constraint that made this hard to fix is `ReadCached`'s contract: callers may
mutate the result. A cache that hands out the parsed value directly would let one
caller's mutation reach every other.

## Commit-Evidenced Intent

Stated in the commit: idle supervisors were starving lock holders and causing
observed timeouts. The measurements below were taken as part of the change rather
than reconstructed.

## Considered Options

1. **Keep caching bytes** — accept the reparse cost.
2. **Cache the parsed value and change the contract to read-only** — require every
   caller to copy when it needs to mutate.
3. **Cache the parsed value and return a deep copy per call** — keep the existing
   contract intact.

## Decision Outcome

Chose **Option 3**. `ReadCached` caches the parsed, normalized `models.State` keyed
by file mtime and returns a deep copy per call.

Cross-process coherence is unchanged: every call still stats the shared path, and
writes still invalidate. The clone is reflective rather than generated, so new
model fields need no matching copy code. Its one escape hatch — shallow-copying
structs with unexported fields — is machine-checked by
`TestStateModelShapeIsCloneable` rather than asserted in a comment.

Measured on a 2.4 MB fixture: 1.07 ms per cached read against 30.4 ms to reparse.
`BenchmarkStateParse` is retained alongside so the premise stays falsifiable.
Retained heap is 2.18 MB for that fixture, slightly below the byte cache it
replaces.

## Rationale

Option 2 would have been cheaper at runtime but turns one local change into an
audit of every caller, with a silent failure mode for any caller missed. Option 3
keeps the blast radius at one function.

Reflective cloning was chosen over generated copy code because the state model
grows: a generated clone that is not regenerated produces a partial copy, which is
worse than a slower one. The machine-checked shape test converts the one unsafe
case into a build-time failure.

## Consequences

- Idle supervisors no longer compete for CPU with lock holders on unchanged state.
- Callers keep the freedom to mutate what `ReadCached` returns.
- Every call pays a clone; the cache wins only while the clone stays cheaper than
  the parse, which the retained benchmarks make visible if that inverts.
- A new model field containing unexported struct state must keep
  `TestStateModelShapeIsCloneable` passing, or the clone silently shares it.

## Evidence and Reconstruction

Source: commit `f5b4da415`, including its benchmark figures. The user supplied no
separate intent for this record; the rationale above is taken from the commit body
and the retained benchmarks. Historical alternatives are reconstructed.

---
*Reconstructed from commit `f5b4da415` (2026-09-14).*
