# Cluster 0120 - Path-Validated Sequential Initialization Gate

## Status

Not selected for the eight-record backfill approved on 2026-09-06. Analysis retained for possible future use; no ADR generated.

## Commit Set

- `5f9e788e` — fix(agent-tools): serialize session initialization
- `5d22b6fa` — fix(contract): move initialization discipline into core
- `029a64c6` — fix(hooks): harden session initialization guidance
- `b7b810ef` — fix(init): make branded contract and hook activation reliable

Earliest author timestamp: 2026-07-21T11:39:57+02:00. Decision implementation range: 2026-07-21 to 2026-08-06.

## Reconstructed Decision

Stop agents satisfying initialization by mentioning document names or racing initialization reads.

Contract requires sequential reads; enforcement recognizes allowed read command shapes and validated document paths instead of raw payload substrings.

**Rationale provenance:** Explicit in 5f9e788e, 029a64c6 and b7b810ef; whether this deserves a separate ADR rather than an ADR-0074 amendment remains a judgment call.

## Evidence

- b7b810ef: internal/embedded/hooks/enforce-init.sh replaces mark_doc_reads_from_input with mark_session_init_doc_path; cat restricted to one file; head/tail/wc removed from accepted initialization reads.
- ADR-0074 documents startup guidance, not path-recognized PreToolUse gate evidence.

Source reads were targeted to the named commit diffs and current sections; they do not establish the human's historical alternatives or acceptance of tradeoffs.

## Related Decisions

ADR-0074, ADR-0111. Existing IDs stay unchanged; this candidate is numbered chronologically within the post-0097 backfill batch.

## Human Context Needed

- Separate enforcement decision or implementation hardening under ADR-0074?
- If retained, what limitations of command/path recognition were knowingly accepted? It is not proof of comprehension or complete successful reads.
- Correct the reconstructed trigger/rationale and name any rejected alternatives, accepted limitations, or additional related decisions.
