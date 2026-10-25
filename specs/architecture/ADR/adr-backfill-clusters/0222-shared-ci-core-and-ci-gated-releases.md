# Cluster 0222 - Shared CI Core Merged Upstream, and CI-Gated Releases

## Status

ADR generated: [0222](../0222-shared-ci-core-and-ci-gated-releases.md). Selected 2026-10-25.

## Commit Set

- `8e117e1fa` — ci: share and optimize workflow execution (#99) (squash: shared core, weekly caches, single integration run, macOS off PRs, Windows weekly caches, merged-not-pinned docs, Semble test gate)
- `63596d743` — ci(lint): vet linux, windows and darwin from any host
- Context only: `bc82b2757` — ci(windows): sharding, parallel ops tests and git batching (2026-09-24, DEV-783)

Earliest author timestamp: 2026-10-22T12:49:11+02:00. Range: 2026-10-22 to 2026-10-23.

## Reconstructed Decision

`ci-core.yml` delivered to forks through upstream merges, customized through declared extension points; releases gated on the CI-verified tagged SHA reachable from `main`.

## Evidence

- `8e117e1fa` and `63596d743` bodies read in full; `.github/workflows/README.md` read; ADR-0017 read.
- Option 2 (pinned cross-repository workflow) is evidenced by the squash's "describe ci-core as merged, not pinned" commit, which replaced the pinning rules.

## Related Decisions

ADR-0017 (amended), ADR-0118.

## User Context — 2026-10-25

Merge-upstream delivery is easier to maintain. Skipping macOS on PRs was the user's call to speed up CI.
