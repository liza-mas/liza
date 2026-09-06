# Cluster 0122 - Keep Task Authoring Outside the TUI

## Status

Not selected for the eight-record backfill approved on 2026-09-06. Analysis retained for possible future use; no ADR generated.

## Commit Set

- `39f2203e` — feat(tui)!: remove add-task command

Earliest author timestamp: 2026-07-29T12:08:10+02:00. Decision implementation range: 2026-07-29 to 2026-07-29.

## Reconstructed Decision

Keep TUI operational controls from duplicating orchestrator/operator task-authoring responsibilities.

Remove TUI add-task binding/form/adapters; retain add-task CLI and ordinary runtime controls.

**Rationale provenance:** Commit body explicitly says responsibility overlap and few useful cases; full alternatives/tradeoffs need confirmation.

## Evidence

- 39f2203e: internal/tui/update.go removes AddTask branch and Huh form machinery.
- 39f2203e: specs/goals/20260326-tui.md removes task creation from objective, controls, and input design; Huh stays in init wizard.

Source reads were targeted to the named commit diffs and current sections; they do not establish the human's historical alternatives or acceptance of tradeoffs.

## Related Decisions

ADR-0052. Existing IDs stay unchanged; this candidate is numbered chronologically within the post-0097 backfill batch.

## Human Context Needed

- Standalone product-boundary decision or note on ADR-0052?
- Was accepting CLI-only manual task creation the entire tradeoff?
- Correct the reconstructed trigger/rationale and name any rejected alternatives, accepted limitations, or additional related decisions.
