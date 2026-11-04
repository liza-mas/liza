# 235 - Shared CI Core Merged Upstream, and CI-Gated Releases

## Status

ACCEPTED — implemented 2026-10-22 to 2026-10-23; backfilled 2026-10-25. Amends
ADR-0017. Amended 2026-11-02 (Linux shards; Windows skipped on pull requests)
and 2026-11-04 (macOS shards).

## Context and Problem Statement

ADR-0017 set up CI on push and pull request and a release pipeline triggered by
any version tag. By October 2026, three problems had accumulated:

- This repository and its downstream forks each maintained their own CI
  definitions, and those definitions had diverged.
- A release ran on any tag push without proving that the tagged commit had passed
  CI.
- CI no longer enforced the race gate after `make test` dropped race and coverage
  instrumentation. This was recorded in `TECH_DEBT.md` with this change as its
  payback trigger.

CI time was also a cost. Windows had already been sharded (`bc82b2757`, DEV-783),
macOS was the slowest pull-request job, and the Actions cache sat over its 10 GB
limit.

## User-Confirmed Intent

The user confirms that maintaining CI along the upstream merge flow is easier than
the alternatives. Skipping macOS on pull requests was the user's call, to speed
up CI.

## Considered Options

1. **Keep per-repository CI definitions** — the status quo; forks diverge.
2. **A cross-repository reusable workflow that forks pin** — the contract first
   documented in this change.
3. **One `ci-core.yml` that forks receive through upstream merges and call as a
   local workflow, customized only through declared extension points.**

## Decision Outcome

Chose **Option 3**. The pinning rules first written for Option 2 were replaced in
the same change.

### Shared core

`ci-core.yml` runs the Linux and macOS jobs. Forks call it as
`uses: ./.github/workflows/ci-core.yml` and never edit it, so upstream merges stay
conflict-free. They customize it through:

- workflow inputs (`setup_python`, `macos_on_pr`);
- the Makefile targets it calls (`ci-prepare`, `lint-nosync`, `ci-test-*-nosync`,
  `build-nosync`), to which a fork adds its own suites;
- caller-owned jobs in `ci.yml` (Windows shards, publishing, releases).

A change the extension points cannot express goes upstream as a new input or
target, not into a downstream divergence.

### Release gating

`release.yml` becomes a `workflow_call`. It validates SemVer, checks that the tag
resolves to the CI-verified SHA and that the SHA is reachable from `main`, and pins
GoReleaser. `ci.yml` gates the release on CI, including Windows, passing for the
tagged commit.

### Execution policy

- Actions are pinned by SHA, and a check catches drift in generated artifacts.
- Linux runs the race suite beside integration. macOS runs the plain suite, on
  `main` pushes and tags only, not on pull requests.
- Integration tests run once, with the e2e tag. Both layouts list the same 5,776
  tests; the local run took 8m05s instead of 10m01s.
- Go caches use weekly keys per OS (per shard on Windows), saved only from the
  default branch, with `GOFLAGS=-count=1` so a restored cache never replays test
  results.
- `make vet` runs `go vet` for linux, windows and darwin from any host, through
  `lint-nosync` and the pre-commit hook. It replaces ci-core's Windows-only vet
  step, which local checks could not reproduce (`63596d743`, after `8337ca87f`
  broke Windows CI).

## Rationale

Forks already take upstream changes by merge. Delivering CI the same way means
one flow to maintain instead of a second, pinning-based distribution channel with
its own versioning. Option 2 would have required forks to track and bump a
reference separately from the code that CI tests. Option 1 is what produced the
divergence.

Gating releases on the CI result of the exact tagged SHA closes the gap ADR-0017
left: a tag could publish binaries from a commit CI had never seen.

## Consequences

- Fork CI tracks upstream with each merge. A fork needing something new has to
  ask upstream for an extension point instead of editing the core.
- A release can no longer publish an unverified commit or one not on `main`.
- The race gate is enforced again, and the TECH_DEBT entry was removed.
- Accepted trade-off (user's call): a macOS-only regression surfaces after merge,
  not in review. The release still waits for macOS.
- Vet results no longer depend on the contributor's OS.
- Codecov upload stays removed.

## Related Decisions

ADR-0017 (release infrastructure; tag-triggered releases now gated on CI), ADR-0118
(native Windows support, whose sharded jobs stay in the caller's `ci.yml`).

## Evidence and Reconstruction

Sources: the squashed commit series in `8e117e1fa` (read in full: shared core,
weekly caches, single integration run, macOS-on-PR, Windows weekly caches, the
merged-not-pinned docs change), `63596d743`, `bc82b2757` for context, and
`.github/workflows/README.md`. The user supplied the maintenance rationale and
confirmed the macOS decision on 2026-10-25.

## Amendment (2026-10-30 to 2026-11-02): Linux shards, and Windows skipped on pull requests

**Linux shards.** The single Linux race job became separate runners, first
three (`da93bfe08`), then five (`a267fd7f1`):

| Shard | Packages |
|---|---|
| core | everything except `internal/ops`, oracle, `internal/integration` and `cmd/liza` |
| ops | `internal/ops` |
| oracle | the oracle packages |
| integration | `internal/integration`, plus the Python tests |
| cli | `cmd/liza` |

The core shard ran under `-race -p 2` in a 30-minute job, with `internal/ops`
taking a large share of it. On a branch adding long-running decision tests it
hit the job limit twice, and the combined integration and CLI job finished at
28 of its 30 minutes. The shards still cover every package exactly once, keep
race detection and uncached runs, and isolate weekly caches by suite. The
Linux aggregate check and the `verified_sha` output are unchanged, so release
gating still reads one result. The new Makefile targets
(`ci-test-ops-nosync`, `ci-test-integration-nosync`, `ci-test-cli-nosync`, and
the core and oracle targets) are fork extension points under the rules above.
`ci-test-slow-race-nosync` stays for the combined local lane.

**Windows on pull requests.** Pull requests skip both the Windows shard matrix
and its aggregate job, matching the macOS policy, so a pull request runs Linux
checks only (`7acf3c9f7`). Windows still runs on `main` pushes, version tags
and manual runs, and the release still waits for it. The accepted macOS
trade-off now covers Windows as well: a Windows-only regression surfaces after
merge, not in review. *The commit gives matching the macOS policy as the
reason; no further rationale is recorded.*

Sources: the commit bodies of `da93bfe08`, `7acf3c9f7` and `a267fd7f1`.

## Amendment (2026-11-04): macOS shards

Once the Linux and Windows shards finished within 9 minutes, the single macOS
job (14 minutes on one 3-core runner, run 41762236975) became the slowest
check on `main`. It now runs the five Linux suites as a matrix with a
`macOS compatibility` aggregate check, which keeps the check name, the
non-race policy (integration still runs with race, as before) and the
pull-request skip. The user chose this together with a separate lane for
`internal/oracle/a9`: `go test` starts packages in list order, so a9, which
mostly waits, started about 7 minutes into the old job. Five shards stay
within GitHub's limit of five concurrent macOS jobs on Free, Pro and Team
plans (docs.github.com/en/actions/reference/limits, read 2026-11-04). Each shard
keeps its own weekly cache; `ci-test-compat-nosync` and `ci-test-slow-nosync`
were removed, and the new `ci-test-compat-*-nosync` targets are fork extension
points.

---
*Reconstructed from commits `8e117e1fa`..`63596d743` (2026-10-22 to 2026-10-23); context from `bc82b2757` (2026-09-24).*
