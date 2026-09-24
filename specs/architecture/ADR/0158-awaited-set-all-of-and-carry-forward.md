# 158 - Awaited Set: All-Of Wait and Carry-Forward

## Status

ACCEPTED — implemented 2026-09-24. Amends ADR-0157: an awaited set is one
all-of wait that also covers the dependency records on its members' paths, and
it outlives an assessment that does not restate it.

## Context and Problem Statement

ADR-0157 fingerprinted each awaited task's outcome record, so a hold on N tasks
woke once per settle (its Consequences said so). In an operator run a coding
task was BLOCKED on three fix tasks it needed together. Each fix was also a
direct dependency, so every merge changed both the `dependencies` and the
`awaited` input: four wakes in 18 minutes, where the orchestrator could act only
once all fixes had merged. Scoping all-of to the `awaited` input alone would not
have removed them.

The set also lived for one assessment. A note-only re-check without `--awaits`
cleared it and silently widened the wake signal back to every generated
descendant. Restating the set on every re-check put the burden on the
orchestrator; in that run its last three re-checks carried no set, one of them
deliberately, to be woken by a plan's generated child.

## Considered Options

1. **All-of for every BLOCKED task's dependencies**, with or without a set.
2. **An opt-in all-of mode** (`--awaits-all`) beside today's per-member set.
3. **Make the declared set all-of**, let it cover the dependency records on its
   members' paths, and carry it forward within one BLOCKED episode.

## Decision Outcome

Chose **Option 3**.

- **All-of projection.** Each member is classified over its replacement path:
  failed if any task on it is blocked, abandoned, integration-failed,
  unreplaced-superseded, missing, of unknown status, or the path cycles;
  otherwise satisfied when the resolver satisfies it (every replacement of a
  split merged); otherwise pending. The `awaited` input is the sorted IDs plus
  the set's state: `pending` until all are satisfied, `satisfied`, or `failed`
  with every path record, so each distinct failure is its own digest.
- **Coverage.** The `dependencies` input drops records whose task lies on a
  member's resolved path. A dependency that is itself awaited drops out; a
  sibling replacement of a split dependency, and every unrelated dependency,
  still wake. `self`, `blocker`, `disposition` and `human` are unchanged.
- **Episode.** A set applies only while no status-transition event (or an
  unclassified event) follows its assessment in history order, so an unblock
  and re-block at the same instant still end it. The wake reader, carry,
  deadlock search and planning-output detection all use this rule.
- **Carry-forward.** An assessment without `--awaits` keeps the episode's set
  minus satisfied members; `--clear-awaits` drops it and conflicts with
  `--awaits`. The result reports the effective set and `awaited_carried`.
  Request identity holds only explicit inputs, never the carried set.
- **Validation** is shared by explicit and carried sets and adds one check to
  ADR-0157's: a member with failed work on its path is rejected, since the
  wait cannot complete until that work is reassessed. A carried set's
  rejection names `--awaits <still-pending members>` or `--clear-awaits`.

### Accepted limits

- An awaited dependency's own merge is no longer material while other members
  are pending. A mistaken set therefore delays that wake until the set
  completes; the orchestrator chose the set, and the result echoes it.
- Assessments that already carry a set get a new digest, so each currently
  awaiting BLOCKED task wakes once to rebaseline. Material without a set is
  byte-identical, so no other task wakes.
- A hold whose release also needs something outside the set (for example a
  human note) still wakes once when the set completes.

## Rationale

Option 1 rewrites ADR-0141 for every hold, including those where one dependency
merging does change the orchestrator's options. Option 2 keeps per-member
semantics that nothing needs: an orchestrator that can act on one member alone
declares that member alone, so a one-member set behaves as before. Option 3
scopes the change to holds that declare a set.

Coverage is per record, by resolved-path overlap rather than by equal IDs, so a
set that names a replacement does not hide its uncovered siblings. The episode
boundary uses history order because transitions and assessments can share a
timestamp. Applying it to the deadlock search also closes ADR-0157's second
accepted limit, where a stale set could reject another task's valid wait.

## Consequences

- A hold on N tasks wakes once when all have merged, or once when any fails,
  plus the usual human-note, self and uncovered-dependency changes.
- Re-checks keep the wait without restating it; the `BLOCKED_TASKS` prompt
  names `--clear-awaits` for waking on a plan's generated children again.
- Explicit sets with a failed branch on a split member are now rejected.

## Relationship to Prior Decisions

Amends ADR-0157 (projection, persistence lifetime, validation, stale-set limit)
and, through it, ADR-0141: the six-input fingerprint and shared read/write
predicate stand; with a declared set, `awaited` substitutes for `descendants`
and filters `dependencies`.

## Evidence and Reconstruction

Defect D70 in the operator notes of the September run. Design choices were made
in adversarial pairing (analysis, two plan revisions, red tests) recorded in
`specs/adversarial-pairing/20260924-D70-awaited-task-wake.md`.
