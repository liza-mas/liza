# Shared CI workflow contract

`ci-core.yml` is the reusable Linux and macOS CI implementation for Liza and
the forks that merge it, including `omni3ai/execution-engine`. Forks receive it
through upstream merges and call it as a local workflow
(`uses: ./.github/workflows/ci-core.yml`); nothing pins this repository.

Customize it without editing it, so upstream merges stay conflict-free:

- Repository policy goes through its inputs (`setup_python`, `macos_on_pr`).
- Suites go through the Makefile targets it calls: `ci-prepare`,
  `lint-nosync`, `ci-test-core-nosync`, `ci-test-oracle-nosync`,
  `ci-test-slow-race-nosync`, `ci-test-compat-nosync` (macOS),
  `test-python-nosync` (with `setup_python`), and `build-nosync`.
  A fork adds its own suites to these targets.
- Repository-specific jobs (Windows shards, publishing, releases) live in the
  caller's `ci.yml`.

Linux tests run as three independent matrix jobs: core, oracle, and
integration/CLI. Python tests run only in the integration job when enabled.
The `Linux tests` aggregate check requires every shard to succeed and forwards
their source SHA through the existing `verified_sha` output. Shards use separate
weekly Go caches and do not cancel each other on failure. The combined
`ci-test-nosync` and `ci-test-go-nosync` targets remain available for local runs.

If a fork needs a change these extension points cannot express, add an input or
target here upstream rather than diverging downstream.
