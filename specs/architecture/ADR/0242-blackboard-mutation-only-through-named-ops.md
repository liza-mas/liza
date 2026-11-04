# 242 - Blackboard Mutation Only Through Named Ops Operations

## Status

ACCEPTED — implemented 2026-10-27 (behaviour change C6 of the architecture
consolidation); backfilled 2026-11-02. Completes
[ADR-0021](0021-ops-service-layer-for-mutations.md). Amended (2026-11-04):
module packages are type-checked from source instead of compiled for export
data.

## Context and Problem Statement

ADR-0021 moved mutation logic into `internal/ops`, but nothing stopped it from
growing back elsewhere. By October 2026, blackboard mutations also lived in:

- `internal/agent/registration.go` and `internal/agent/checkpoint_summary.go`;
- `internal/commands/init.go` and `internal/commands/migrate.go`;
- `internal/tui/commands.go` (the auto-resume toggle);
- the `ops.ModifyWithAgentAuthority` closures of eight `internal/agent` files,
  where `ops` held the lock and the fence but the caller supplied the mutation.

A reader could not answer "where does this state change happen" by looking in
`ops`. A new mutation could be written in any layer that held a `*db.Blackboard`.

The consolidation goal froze behaviour except for a closed list of forced
changes (decision D6). D7 listed this one as C6: "remaining direct writes routed
through `ops` without added validation". R8 widened it to the TUI site and the
`ModifyWithAgentAuthority` closures. N5 required the check to cover every
mutating `Blackboard` method.

## Considered Options

1. **Leave the remaining sites in place.** They would be a documented exception
   that the next feature run could copy.
2. **Move the sites into `ops`, with no enforcement.** The boundary holds only
   as long as reviewers catch new violations.
3. **Move every site into a named `ops` operation and enforce the boundary in
   `make lint`.** This is the D7/R8/N5 ruling.

Adding validation on the write path while moving the sites was ruled out of
this run (D7, Won't).

## Decision Outcome

Chose **Option 3**.

### Ownership

Every blackboard mutation lives in a named `ops` operation with typed inputs and
results. Callers keep detection, logging, alerts, filesystem effects and
in-memory trackers. Three moves did this:

| Move | Commit | Operations |
|---|---|---|
| Registration and exit | `bc6945b07` | `RegisterAgent`, `RetireSurplusAgent`, `UnregisterAgent`, `ResetAgentToIdle`, `ReleaseAgentAfterExit`, `ReleaseClaimForHalt`, `EnterOrchestration`, `BlockSupervisorTask`, `PersistExit42Restart` |
| Runtime bookkeeping (closures O7–O15) | `263298679` | `RecordWorktreeRecovered`, `ForceBlockReviewerTask`, `FinalizeCleanTerminalTask`, `RecordProviderAuditDegraded`, `RecordPendingMergeStall`, `ClearResumedTransitionCheckpoint`, `ConsumeRenderedHumanNotes`, `RecordUndecidedPlans`, `ClaimCheckpointSummary` |
| Presentation sites | `4bd986841` | `PublishInitialState`, `MigrateState` (replaces `commands.MigrateCommand`), `ToggleAutoResume` |

`stillExpected` closures become typed owner and transition expectations. Locks,
fencing, clocks, history and error chains are unchanged, and no validation or
authority check is added. The heartbeat liveness file
(`db.WriteLivenessRecord`) is not the blackboard and stays outside the rule (R8).

### Enforcement

`TestC6MutationGuard` in `internal/testguard/archguard` runs through
`lint-archguard`. For linux, darwin and windows it type-checks every production
package outside `ops` and `db` from source, resolving the standard library and
other modules from `go list` export data, and applies these rules to every use
the type checker resolves: calls, method values, method expressions, handle
chains and promoted methods.

| Rule | Rejects |
|---|---|
| M1 | A `*db.Blackboard` method that the guard's table classifies as mutating (`Modify`, `Write`, `UpdateTask`, `UpdateAgent`, `ArchiveTerminalTask`) |
| M2 | An interface method whose name and signature equal a mutating method |
| C1 | An `ops` or `db` function taking a func parameter over `*models.State`, `*models.Task` or `*models.Agent` (a mutation callback) |
| K1 | An exported `*db.Blackboard` method missing from the table, or a table entry naming no method |
| B1 | A production file that no configuration type-checks |
| S1 | `db.Blackboard` or the `ops` callback API unresolved, so the guard never passes vacuously |

There is no baseline exception. A fixture module proves each rule shape and
each legal case. The guard's `go list` child builds its environment from an
allowlist and masks every retained value in its diagnostics (`fd09e4b3f`,
`2f5944f09`). It also owns its Go cache so that it runs on Windows
(`f112f85ed`).

### Rationale

D6 asks for structure the next feature run can imitate. A visible rule ("state
changes are named `ops` operations") answers "where does new code go" for every
mutation. Only an automated check keeps that rule from eroding. Classifying
every exported `Blackboard` method (K1) means a new mutator cannot slip past the
guard unclassified.

*The sources do not record why the guard type-checks the code instead of using
a text search or a runtime validator.* The guard's own comment states only the
scope: a source check over `go/types`, with reflection, `unsafe` and generated
assembly outside it.

Two branches of the relocated supervisor block were reachable only through a
race window or a deadline. Operator ruling D61 kept their existing focused tests
as preservation evidence instead of black-box oracle rows. Adding a production
test seam was rejected, and so was leaving the operations in `internal/agent`.

### Consequences

**Positive:**
- `ops` is the single place to read for any blackboard state change. The
  INVARIANTS.md section 4 and `roles.md` citations point there.
- A mutation added outside `ops`/`db`, or a callback that passes the mutation
  into `ops`, fails `make lint` on every OS.
- The `agent` → `db` import allowlist entry now states that the runtime holds
  the `Blackboard` handle for non-mutating use only (snapshot, cached and watch
  reads), instead of a baseline crossing awaiting resolution.

**Limitations accepted:**
- No validation was added on the write path. That stays a separate decision.
- Rule C1 would also flag a future `ops` snapshot-predicate API taking
  `*models.State`. The guard comment names that API as the trigger to refine
  the rule.
- The guard sees only what `go/types` resolves. Reflection, `unsafe` and
  assembly could bypass it.

## Related Decisions

- [ADR-0021](0021-ops-service-layer-for-mutations.md) — established the ops
  service layer that this record completes and enforces.
- [ADR-0011](0011-script-enforced-agent-status.md) — structural enforcement
  over behavioural compliance. This record applies the same principle to the
  layer boundary.
- [ADR-0218](0218-architecture-guard-package-and-ratchet-data-seam.md) — the
  guard package and `lint-archguard` target that host this check.
- [ADR-0228](0228-a9-runtime-concerns-and-launch-signal-capabilities.md) — A9
  left the `agent` mutation closures in place for C6.
- [ADR-0241](0241-one-initialization-implementation.md) — the surviving init
  path publishes its initial state through `ops.PublishInitialState`.

## Evidence and Reconstruction

Sources: goal decisions D6, D7, R8, N5 and D61
(`specs/goals/20261023-architecture-consolidation-decisions.md`), C6 in the goal
document, the commit bodies listed above, the guard source
`internal/testguard/archguard/c6_mutation_guard_test.go`, and the evidence files
`specs/architecture/consolidation-run/c6-*-evidence.md`. No user input beyond the
approval of the record (2026-11-02).

---
*Reconstructed from commits `bc6945b07`..`f112f85ed` (2026-10-27)*
