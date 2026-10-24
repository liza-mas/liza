# 219 - Oracle Harness and Chained Provenance

## Status

ACCEPTED. Amended by [D56](../../goals/20261023-architecture-consolidation-decisions.md#d56--d24-extended-c-tasks-may-flip-in-driver-assertions-and-delete-goldens-regen-must-reproduce-both-run-2026-10-24): §§4–8 gain
package-scoped change points (M1–M9); D24's text and §1's D53 text are unchanged.

## Context

The architecture-consolidation run freezes observable behavior while production
packages move. Oracle drivers must run against both the current tree and the
starting production tree, including after a call-site-only adapter edit. A C1–C6
behavior change intentionally changes a golden, so starting-tree reproduction
alone cannot prove its new value. [D24](../../goals/20261023-architecture-consolidation-decisions.md#d24--provenance-of-goldens-changed-by-a-c-task-run-2026-10-23)
settles that case with reproduction at every change point.
[D56](../../goals/20261023-architecture-consolidation-decisions.md#d56--d24-extended-c-tasks-may-flip-in-driver-assertions-and-delete-goldens-regen-must-reproduce-both-run-2026-10-24)
extends it: a C task may also flip an oracle driver assertion that encodes the
behavior it changes and delete a golden the changed behavior no longer
produces. Both are C change points, and regeneration must reproduce them.

## Decision

1. **Test-only convention.** Suites are `internal/oracle/a<N>/**`, with tests
   named `TestOracle…`, fixtures in `testdata/fixtures/`, and goldens in
   `testdata/golden/<kind>/`. Discovery is `./internal/oracle/...`, without a
   registry. Golden paths are never renamed, except storage-name-only renames that
   [D53](../../goals/20261023-architecture-consolidation-decisions.md) requires,
   with content unchanged. The two `internal/oracle/sample/`
   goldens are harness proof outside every area's surface inventory.
2. **Shared harness.** `internal/testhelpers/oracle/` uses only the standard
   library and imports no other module package. Prompt and artifact goldens
   retain raw bytes; artifacts reflect the starting build's rendered output.
   JSON is canonical, with sorted keys and normalized volatile string values,
   preserving codes. Human output loses ANSI and layout whitespace, preserving
   token and row order. State is a normalized before/after pair. Shared
   normalizers handle timestamps including local offsets, disposable paths,
   volatile identifiers and explicitly requested line sorting. Live comparison
   never writes and missing goldens fail. `ORACLE_WRITE=1` writes normalized
   bytes. CLI helpers build once per test binary and run in disposable projects.
3. **Adapter rule.** Drivers import only standard library, harness and their
   own adapter functions; other imports live in adapters. An adapter starts
   untagged. At its first call-site-only edit, its pre-edit content moves
   verbatim into a twin tagged `oraclestart`, while the original becomes
   `!oraclestart`. Twins are never edited. Suites added after refactors begin
   ship adapters that compile against the starting tree with `oraclestart`.
4. **Append-only provenance.** `internal/oracle/PROVENANCE` contains exactly
   one first-line `start <40-hex>` (the run starts at
   `96c9ad69aee983ce2b929dd681b6ba43bf920535`), then `fix <40-hex>` and
   `changed <path> <reason>` records. Reasons are C1–C6, `determinism`
   or `fix`; a changed record never carries a SHA. No comments or blank lines.
   **M1 (D56).** `<path>` names a golden, or another tracked path of an oracle
   package (driver, adapter, fixture); never `PROVENANCE`, a path outside a
   package, or a golden path of an unknown kind. A driver-only C change records
   the driver path; a commit that also changes a golden may rely on that golden's
   record. Every record must name a path its commit changes.
   Each commit changing a chained golden appends exactly one changed record
   for that golden in that same commit. Repeated changes append repeated
   records. Determinism/fix records are required only after the first C record;
   earlier changes remain covered by H1. Review verifies the permitted oracle
   exception. Task branches merge with `--no-ff`; SHA-free changed records
   survive rebasing before merge.
5. **D24 change points.** For a golden with a C record at HEAD, inspect
   `git log --reverse --format=%H -- <golden>` from HEAD. The chain begins at
   the first candidate adding its C record; every later golden-changing
   candidate is a change point, including determinism and urgent-fix changes.
   Each point must be non-merge and add exactly one matching record against
   its first parent. Independently inspect ledger history to reject a record
   without a corresponding change. Merge commits that change a golden
   and changes without records are unexplained.
   **M2, M5 (D56).** A selected package is a *C package* when HEAD's ledger holds
   a C1–C6 record for one of its paths. The package of a path is the prefix
   before its first `/testdata/`, else its directory; nested packages are
   distinct, so a record in `a3/control` does not make `a3` a C package. The
   package's first C point is the earliest commit in its path history adding
   such a record; its *pre-C commit* is that point's first parent. Every
   commit after it that changes a package path is a point: non-merge
   (`merge changes golden` / `merge changes oracle files`), adding a record for
   a package path it changes, and exactly one record for each package golden it
   adds, modifies or deletes (`change without record`, `change has multiple
   records`). A golden first committed after the pre-C commit has no
   reproducible pre-change blob (`pre-change blob absent`).
6. **Comparison.** A non-chained golden must equal its H1 reproduction byte
   for byte. For a chained golden, first reproduce its pre-change blob from
   the starting baseline using the oracle tree and fix records of the first
   point's first parent. Then reproduce each point's committed blob at that
   point itself, untagged. Finally require the working golden to equal the
   last reproduced blob. Report every successful link with reason and SHA.
   Missing/extra files, mismatches and broken history fail, naming paths.
   **M4, M6, M7 (D56).** For a C package the pre-C reproduction is the whole
   package: its goldens committed at the pre-C commit must equal the baseline
   reproduction of that commit, drivers included, over starting production
   (`pre-change reproduction differs`). Each distinct point runs once, untagged,
   over the C packages it is a point of, and the goldens committed there must
   equal the reproduction (`change point reproduction differs`); a golden
   deleted at a point must not be reproduced (`deletion not reproduced`).
   At HEAD a working golden equal to HEAD's is `ok`; differing is `working tree
   differs from last change point`; present at HEAD and absent in the working
   tree is `missing`; present only in the working tree is `extra`. A golden
   absent at HEAD is accepted only when its last point deleted it, reproduced,
   under a C1–C6 record; otherwise it is `missing` (R3). A non-golden package
   path with an uncommitted or untracked change is `working tree differs from
   HEAD`. The report is `ok <golden> chain pre <pre> [<reason> <sha>]…` with a
   trailing ` deleted` for a reproduced deletion, and `ok <package> package pre
   <pre> [<reasons> <sha>]…` for every point of the package.
7. **Materialisation and isolation.** The regeneration command creates only
   temporary detached worktrees in system temporary storage. A baseline is
   start plus ledger-ordered fix diffs excluding `internal/oracle/**`, overlaid
   with the selected oracle and harness trees. It runs that tree's own
   `sync-embedded`, then write-mode `go test -count=1`, tagged `oraclestart`
   for baselines only. Trees are reused within one invocation. Cleanup uses
   `git worktree remove --force` on success, failure and cancellation. No
   source branch/ref or live runtime state is changed. Every child uses the
   harness's `ChildEnv` policy: explicit HOME/PATH/TMPDIR, platform-required
   Windows names, Go caches and `GOFLAGS=-buildvcs=false` for Go children,
   and `ORACLE_WRITE=1` for tests. Diagnostics never print environment values.
   **M3 (D56).** The working baseline runs only the selected packages that are
   not C packages: the regenerator receives the selected patterns and the
   excluded package directories, lists packages with `go list` under the Go
   `ChildEnv`, and runs no `go test` when none remain. A C package runs from its
   pre-C commit (tagged baseline) and from each point (untagged); a failing run
   is the runtime form of D56's disproof and exits 2, never bypassed.
8. **Entry point.** Run `go run ./internal/testhelpers/oracle/cmd/regen`.
   `-repo DIR` selects a repository; the default contains the current directory.
   `-mode check` is default and returns 0 only when all selected goldens pass,
   1 for unexplained/missing/extra goldens, and 2 for ledger/setup errors.
   `-mode write-new` creates only absent goldens from the H1 baseline, never
   overwriting an existing file, and never writes into a C package (**M8**,
   D56). Repeatable `-pkg ./internal/oracle/<area>/...`
   filters bound both packages run and golden comparison.

## Consequences

- Golden changes carry reviewable provenance in the same commit without a
  self-referential commit SHA. Rebase and ordinary no-fast-forward merges
  preserve the recording workflow.
- Drivers keep enough adapter history to compile against the starting tree;
  changed behavior is proven at its actual commit, without backports.
- Regeneration costs one build/test run per distinct required tree. Disposable
  Git scenario tests inject only regeneration, preserving real history and
  worktree behavior; a real sample test proves the build/write composition.
- The harness adds no production dependency and never uses provider CLIs.
- **M9 (D56).** Flags, exit codes, ledger checks, `ChildEnv`, cleanup, stdlib-only
  imports, D53 storage names and all behavior while no selected package has a C
  record are unchanged.
- D56 ceilings fail loud, never accept falsely: a golden added after a
  package's first C point is `pre-change blob absent`, and after that point
  every change to the package needs a record, so a call-site-only adapter edit
  there has no fitting reason and a later determinism edit records an edited
  path. Writers beyond the known graph are unchecked; the upgrade trigger is the
  first unsupported need, raised by its owner as a correction.
- Each C package adds one run for its pre-C commit (shared by packages with the
  same pre-C commit) and one per distinct point, besides the H1 run.

## Alternatives Considered

1. Chaining ledger claims without reproducing each point: rejected by D24,
   because the changed value would remain unproven.
2. Backporting each C change onto the starting tree: rejected by D24 as extra
   work outside the run's goal.
3. Naming a golden-changing commit in its own ledger record: rejected because
   the commit would name itself and rebases would invalidate the record.
