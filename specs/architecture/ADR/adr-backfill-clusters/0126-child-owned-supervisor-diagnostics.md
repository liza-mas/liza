# Cluster 0126 - Child-Owned Supervisor Logs and Bootstrap Readiness

## Status

Not selected for the eight-record backfill approved on 2026-09-06. Analysis retained for possible future use; no ADR generated.

## Commit Set

- `d1503309` — fix(supervisor): persist lifecycle diagnostics

Earliest author timestamp: 2026-08-16T23:17:41+02:00. Decision implementation range: 2026-08-16 to 2026-08-16.

## Reconstructed Decision

Make detached supervisor bootstrap and lifecycle failures observable without tying child lifetime to the parent.

Child opens masked logs, parent waits for bootstrap readiness then removes handshake, and shutdown joins diagnostic workers before closing logs.

**Rationale provenance:** Explicit commit body; standalone ownership ADR versus dated ADR-0029 amendment is a judgment call.

## Evidence

- cmd/liza/supervisor_logs.go:25 opens child-owned output files; 112 records panic/final failure before closure.
- internal/process/spawn.go:101-105 removes the readiness channel after bootstrap (delegated verified source read).

Source reads were targeted to the named commit diffs and current sections; they do not establish the human's historical alternatives or acceptance of tradeoffs.

## Related Decisions

ADR-0029, ADR-0053, ADR-0060. Existing IDs stay unchanged; this candidate is numbered chronologically within the post-0097 backfill batch.

## Human Context Needed

- Standalone process/log ownership decision or ADR-0029 amendment?
- Were parent piping and child files compared, and why was the readiness timeout chosen?
- Correct the reconstructed trigger/rationale and name any rejected alternatives, accepted limitations, or additional related decisions.
