# Shared CI workflow contract

`ci-core.yml` is the reusable Linux and macOS CI implementation for Liza and
the forks that merge it, including `omni3ai/execution-engine`. Forks receive it
through upstream merges and call it as a local workflow
(`uses: ./.github/workflows/ci-core.yml`); nothing pins this repository.

Customize it without editing it, so upstream merges stay conflict-free:

- Repository policy goes through its inputs (`setup_python`, `macos_on_pr`).
- Suites go through the Makefile targets it calls: `ci-prepare`,
  `lint-nosync`, `ci-test-nosync` (with `setup_python`), `ci-test-go-nosync`
  (without it), `ci-test-compat-nosync` and `test-python-nosync` (macOS), and
  `build-nosync`. A fork adds its own suites to these targets.
- Repository-specific jobs (Windows shards, publishing, releases) live in the
  caller's `ci.yml`.

If a fork needs a change these extension points cannot express, add an input or
target here upstream rather than diverging downstream.
