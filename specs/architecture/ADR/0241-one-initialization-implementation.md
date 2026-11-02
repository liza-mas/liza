# 241 - One Initialization Implementation

## Status

ACCEPTED — implemented 2026-10-26 (behaviour change C4 of the architecture
consolidation); backfilled 2026-11-02.

## Context and Problem Statement

Project initialization had two implementations. The CLI path
(`internal/commands/init.go` and its helpers) ran every `liza init`.
`ops.InitProject` (`internal/ops/init_project.go`) duplicated it but had no
production caller; only tests and one integration fixture used it. The
architectural issues registry tracked the pair as *Duplicate Initialization
Implementations* (STRUCTURAL DEBT).

The init Git calls that validate and create the integration branch used
`gitenv.Command`, which sets the Git environment but no timeout and no working
directory. They ran in the process directory instead of the project root. A hung
Git call therefore hung `init`, and branch creation depended on where the
command was started.

The consolidation goal (`specs/goals/20261023-architecture-consolidation.md`)
froze behaviour except for a closed list of forced changes (decision D6). D7
listed this one as C4, "one init owner using `gitenv` and an explicit project
root". Review fix R5 then corrected the ruling.

## Considered Options

1. **Keep both implementations.** The duplicate keeps drifting, and the next
   feature run could extend either one.
2. **Converge on `ops.InitProject`.** This would move the production path onto
   code that no production caller exercised.
3. **Converge on the CLI path and delete `ops.InitProject`** (R5).

The original D7 ruling required an equivalence test across both entry points,
written before convergence. R5 dropped it, because the deleted path had no
production caller to stay equivalent to.

## Decision Outcome

Chose **Option 3**.

- `internal/ops/init_project.go`, its test and its A7 ratchet key are deleted.
  The A7 closure test now requires an empty ratchet, allowlist and S2 list.
- `check-ref-format --branch`, `rev-parse --verify <name>`,
  `rev-parse --verify HEAD` and `branch <name> HEAD` run through
  `gitenv.CombinedOutput`/`Output` in the discovered project root, with
  `gitenv`'s timeout. A `*gitenv.TimeoutError` is returned as such and is never
  read as an invalid name, an absent branch or an unborn HEAD.
- `newMASInit` discovers the project once, before branch-name validation.
  `resolveProject` reports a discovery failure at the same point and with the
  same text as before.
- Init output and the resulting state are otherwise unchanged, as the A7 oracle
  shows.

### Rationale

The CLI path is the one every user runs, so making it the reference changes no
observable init behaviour. *Derived from the R5 record and the absence of a
production caller; the choice is not otherwise explained in the sources.*
Running init Git through `gitenv`'s timed runners gives these calls the same
timeout hardening as the rest of the Git layer. Using the project root
explicitly makes branch creation independent of the caller's working directory.

R5 also corrected D7: the CLI path already used `gitenv`'s environment, so the
behaviour change is only the timeout and the explicit root.

### Consequences

**Positive:**
- One init implementation. Future init changes have a single place to go, and
  the *Duplicate Initialization Implementations* issue is resolved.
- A hung init Git call fails with a timeout error instead of hanging the
  command.
- Starting `init` from a subdirectory creates the branch in the project root.

**Limitations accepted:**
- The `--cwd` Git call in `cmd/liza` launch was ruled out of scope (Won't), so
  the *Raw Git Calls Bypass gitenv Hardening* issue stays open for that site.
  Only its initialization half is resolved.
- The deleted path's tests did not migrate. Coverage comes from the CLI path's
  tests and the new `init_git_root_test.go` cases (P1–P3). All of the
  root and timeout cases failed on the pre-C4 code.

## Related Decisions

- [ADR-0021](0021-ops-service-layer-for-mutations.md) (ops service layer) — the
  surviving init path writes state through `ops.PublishInitialState` after
  [ADR-0242](0242-blackboard-mutation-only-through-named-ops.md).
- [ADR-0218](0218-architecture-guard-package-and-ratchet-data-seam.md) — the A7
  ratchet entry for `InitProject` is removed here.
- [ADR-0226](0226-a7-presentation-concerns-and-classification-contract.md) —
  A7 left `ops.InitProject` in place for C4 to delete.

## Evidence and Reconstruction

Sources: goal decisions D6, D7 and R5
(`specs/goals/20261023-architecture-consolidation-decisions.md`), C4 in the goal
document, the commit body of `009d4835a`, the resolution entry in
`specs/architecture/architectural-issues.md`, and
`specs/architecture/consolidation-run/run-close.md`. No user input beyond the
approval of the record (2026-11-02).

---
*Reconstructed from commits `009d4835a`..`4cd8d7d78` (2026-10-26)*
