# 219 - Oracle Harness and Chained Provenance

## Status

ACCEPTED.

## Context

The architecture-consolidation run freezes observable behavior while production
packages move. Oracle drivers must run against both the current tree and the
starting production tree, including after a call-site-only adapter edit. A C1–C6
behavior change intentionally changes a golden, so starting-tree reproduction
alone cannot prove its new value. [D24](../../goals/20261023-architecture-consolidation-decisions.md#d24--provenance-of-goldens-changed-by-a-c-task-run-2026-10-23)
settles that case with reproduction at every change point.

## Decision

1. **Test-only convention.** Suites are `internal/oracle/a<N>/**`, with tests
   named `TestOracle…`, fixtures in `testdata/fixtures/`, and goldens in
   `testdata/golden/<kind>/`. Discovery is `./internal/oracle/...`, without a
   registry. Golden paths are never renamed. The two `internal/oracle/sample/`
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
   `changed <golden-path> <reason>` records. Reasons are C1–C6, `determinism`
   or `fix`; a changed record never carries a SHA. No comments or blank lines.
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
   without a corresponding golden change. Merge commits that change a golden
   and changes without records are unexplained.
6. **Comparison.** A non-chained golden must equal its H1 reproduction byte
   for byte. For a chained golden, first reproduce its pre-change blob from
   the starting baseline using the oracle tree and fix records of the first
   point's first parent. Then reproduce each point's committed blob at that
   point itself, untagged. Finally require the working golden to equal the
   last reproduced blob. Report every successful link with reason and SHA.
   Missing/extra files, mismatches and broken history fail, naming paths.
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
8. **Entry point.** Run `go run ./internal/testhelpers/oracle/cmd/regen`.
   `-repo DIR` selects a repository; the default contains the current directory.
   `-mode check` is default and returns 0 only when all selected goldens pass,
   1 for unexplained/missing/extra goldens, and 2 for ledger/setup errors.
   `-mode write-new` creates only absent goldens from the H1 baseline, never
   overwriting an existing file. Repeatable `-pkg ./internal/oracle/<area>/...`
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

## Alternatives Considered

1. Chaining ledger claims without reproducing each point: rejected by D24,
   because the changed value would remain unproven.
2. Backporting each C change onto the starting tree: rejected by D24 as extra
   work outside the run's goal.
3. Naming a golden-changing commit in its own ledger record: rejected because
   the commit would name itself and rebases would invalidate the record.
