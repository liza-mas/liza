# Shared CI workflow contract

`ci-core.yml` is the reusable Linux and macOS CI implementation for Liza and
the forks that merge it, including `omni3ai/execution-engine`. Forks receive it
through upstream merges and call it as a local workflow
(`uses: ./.github/workflows/ci-core.yml`); nothing pins this repository.

Customize it without editing it, so upstream merges stay conflict-free:

- Repository policy goes through its inputs (`setup_python`, `macos_on_pr`).
- Suites go through the Makefile targets it calls: `ci-prepare`,
  `lint-nosync`, `ci-test-core-nosync`, `ci-test-ops-nosync`,
  `ci-test-oracle-nosync`, `ci-test-integration-nosync`,
  `ci-test-cli-nosync`, the macOS `ci-test-compat-core-nosync`,
  `ci-test-compat-ops-nosync`, `ci-test-compat-oracle-nosync` and
  `ci-test-compat-cli-nosync`,
  `test-python-nosync` (with `setup_python`), and `build-nosync`.
  A fork adds its own suites to these targets.
- Repository-specific jobs (Windows shards, publishing, releases) live in the
  caller's `ci.yml`.

Linux tests run as five independent matrix jobs: core, ops, oracle,
integration, and CLI. `internal/ops` and `cmd/liza` have their own jobs because
each takes a large share of a job's 30-minute limit under race instrumentation.
Python tests run only in the integration job when enabled.
macOS runs the same five suites as matrix jobs, without race instrumentation
except integration, which reuses `ci-test-integration-nosync`. Both oracle
targets list `internal/oracle/a9` first so it starts first. The `macOS compatibility`
aggregate check requires every macOS shard to succeed.
The `Linux tests` aggregate check requires every shard to succeed and forwards
their source SHA through the existing `verified_sha` output. Shards use separate
weekly Go caches and do not cancel each other on failure. The combined
`ci-test-nosync` and `ci-test-go-nosync` targets remain available for local runs.

If a fork needs a change these extension points cannot express, add an input or
target here upstream rather than diverging downstream.
