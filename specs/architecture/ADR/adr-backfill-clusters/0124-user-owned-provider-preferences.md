# Cluster 0124 - User-Owned Provider Preferences and Explicit MAS Launch Policy

## Status

ADR generated: [0124](../0124-user-owned-provider-preferences.md). Selection and grouping approved on 2026-09-06; user intent supplied on 2026-09-06.

## Commit Set

- `00111804` — refactor: remove codex model effort and personality from config
- `c7e99f19` — fix(permissions)!: let user settings own the Claude permission mode

Earliest author timestamp: 2026-08-09T18:36:41+02:00. Decision implementation range: 2026-08-09 to 2026-08-09.

## Reconstructed Decision

Prevent project defaults from shadowing user provider settings while retaining explicit unattended MAS launch behavior.

Stop injecting Codex model/effort/personality defaults; remove Claude project defaultMode and supply MAS permission mode in launch arguments.

**Rationale provenance:** User-confirmed intent: Respecting the user's own configuration is a default principle. Claude auto mode was chosen to avoid systematic approval gates on Bash commands. Other explanations remain source-derived or reconstructed.

## Evidence

- internal/embedded/embedded.go:1189-1213 documents and implements removal of defaultMode while merging other settings.
- 00111804 changes embedded Codex defaults and preserves existing user values.
- c7e99f19 separates user-owned settings from runtime launch arguments; support-docs/CONFIGURATION.md:205-250 documents the boundary.

Source reads were targeted to the named commit diffs and current sections; they do not establish the human's historical alternatives or acceptance of tradeoffs.

## Related Decisions

ADR-0085, ADR-0096, ADR-0111. Existing IDs stay unchanged; this candidate is numbered chronologically within the post-0097 backfill batch.

## User Context — 2026-09-06

Respecting the user's own configuration is a default principle. Claude auto mode was chosen to avoid systematic approval gates on Bash commands.

## Remaining Historical Gaps

Historical alternative evaluations were not supplied.
