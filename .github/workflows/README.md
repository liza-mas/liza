# Shared CI workflow contract

`ci-core.yml` is the reusable CI implementation for Liza and downstream forks,
including `omni3ai/execution-engine`.

Availability and compatibility contract:

- Keep this repository public and `.github/workflows/ci-core.yml` at its current
  path while downstream callers reference it.
- Consumers must pin a full commit SHA that is reachable from reviewed `main`.
- Coordinate any repository rename, visibility change, workflow path change, or
  incompatible input/output change with downstream consumers before applying it.
- Update consumers only after this workflow passes Linux and macOS CI at the
  candidate SHA.
- If ownership must move, publish an identical immutable revision at the new
  location and repin every consumer before removing the old location.

These rules make the shared implementation immutable for each caller while
preventing silent drift between Liza and its forks.
