# 218 - Architecture Guard Package and Ratchet Data Seam

## Status

ACCEPTED.

## Context

The architecture-consolidation run refactors the production tree area by area.
Two structural limits must hold during and after it: no production function over
100 lines or file over 800 lines (S1), and the import-direction rule between
layers (K3, K4). Both need a mechanical signal that stays in the repository after
the run, and both start with known violations that may only shrink.

The run also has nine sequential area chains. Guard data held in one shared file
would be written by every chain; data split by area has one writer at a time.

## Decision

1. **One test-only guard package.** `internal/testguard/archguard/` holds every
   architecture guard as `_test.go` files, standard library only. It locates the
   repository from its own source file and reads the module path from `go.mod`.
   Shared helpers enumerate production Go files under `cmd/` and `internal/`
   (excluding `_test.go`, `internal/testhelpers/`, `internal/testguard/`,
   `internal/prompts/promptbench/` and any `testdata` directory; build
   constraints are ignored), parse them once, and expose the per-area data. Each
   guard is its own file, so a later guard needs no `Makefile` edit: `make
   lint-archguard` runs the whole package with `-count=1`, `lint-nosync` calls
   it (so `make lint` and CI's lint lane run it), and `make test` runs it
   through `./...`.
2. **Data is Go declarations, one file per area.** `area_a1_test.go` …
   `area_a9_test.go` each declare one `Area` value with four sections: layer
   entries, allowlist entries, ratchet entries and S2 exceptions. A guard-owned
   list (`areas_test.go`) names the nine values and is never edited after it is
   written. Area N's file is written only by area N's scopes; a ratchet entry or
   S2 entry sits in the file of the area owning the offender, a layer entry in
   the file of the area containing the package, an allowlist entry in the file of
   the area owning the crossing's resolution. Keys: file is the repo-relative
   slash path; function is the file plus `Type.Method` (type parameters dropped),
   with `#n` on the n-th declaration of a repeated name.
3. **S1 guard.** A function is measured from its declaration line to its closing
   brace, a file by physical lines; an offender is strictly above 100 / 800
   (named constants). The guard reports, in one run and naming key, measured
   value, limit and the area file to change: an offender in no list, a ceiling
   overrun, a stale entry (ratchet or S2 key no longer over threshold), a ratchet
   key absent from the start snapshot, a ceiling above its snapshot value, a key
   listed twice, and a malformed S2 entry. A renamed or moved offender surfaces
   as a stale old key plus an unlisted (or not-in-snapshot) new key. Messages
   carry repo-relative paths only, never environment values.
4. **Snapshot ceiling.** `s1_snapshot_test.go` records every offender measured at
   the starting commit (`96c9ad69aee983ce2b929dd681b6ba43bf920535`: 33 files, 93
   functions) and is never edited; it is part of the run baseline record. Ratchet
   data starts equal to it (126 entries). Without the snapshot a new or renamed
   offender could simply be added to ratchet data and pass. S2 data is separate
   and starts empty: entry = key + category (`table`, `registration`,
   `generated`, or a reference to the human ruling for any other) + rationale.
5. **Remaining shrink check is review.** The guard sees no history: a ceiling
   lowered and later raised again, still within its snapshot value, passes.
   Monotonic shrink between lowered values is therefore a review check of
   ratchet-data diffs; reviewers reject a diff that raises a ceiling.
6. **Layer rule and allowlist discipline (enforced by the import-direction guard
   that follows).** Every production package under `cmd/` and `internal/` has
   exactly one layer — domain, application (`ops`), runtime (`agent`,
   `process`), prompts, presentation, leaf or infrastructure — except test
   support, which is exempt. Forbidden edges: domain, infrastructure and leaf to
   application, runtime, presentation or prompts; application to runtime,
   presentation or prompts; prompts to application, runtime or presentation;
   runtime to presentation; presentation other than `cmd/liza` to `agent`;
   `jsonout` to application; `agent` to `db`; and a production package absent
   from the layer map. A violating edge passes only through an allowlist entry
   carrying importer, imported, owner scope and non-empty rationale; an entry
   whose edge no longer exists or no longer violates is stale and fails. The
   allowlist only shrinks unless an owner adds a rationale-carrying entry.

## Consequences

- Offenders can be removed, lowered or converted to S2 exceptions by the owning
  area, and nothing else: the ratchet cannot grow or move silently.
- At run close every area's ratchet data is empty; the guard stays and then
  enforces S1 absolutely, with S2 exceptions listed by rationale.
- Data files are plain Go, type-checked and gofmt-formatted; no parser to
  maintain. They are test-only, so jscpd and production builds ignore them.
- The package adds roughly a second of parsing to `make lint` and `make test`.
- A new production package or file must be placed in an area's data (layer
  entry, and an S1 entry if it is an offender) by the task creating it.

## Alternatives Considered

1. **One shared data file.** Rejected: every area chain would write it.
2. **Ratchet without a start snapshot.** Rejected: new or renamed offenders pass
   by being added to the ratchet.
3. **Data in YAML or JSON read by the guard.** Rejected: needs a parser and
   schema checks that Go declarations get from the compiler.
4. **A `go list`-based or type-checked analysis.** Rejected for S1: parsing alone
   measures lines, needs no build and works across all build constraints.
