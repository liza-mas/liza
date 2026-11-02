# Cluster 0244 - The Init Sequence Is Defined by CORE and Enforced by the Hook

## Status

ADR generated: [0244](../0244-init-sequence-defined-by-core-enforced-by-hook.md). Selected 2026-11-02.

## Commit Set

- `319fad665` — fix(init): stop requiring a CORE read at session start (also amended ADR-0111)
- `cd2f8d6dc` — fix(init): list project init docs by absolute path
- `965b22155` — fix(init): list unread docs when rejecting an init read
- `282b3ca65` — refactor(prompts): defer init sequence to CORE and the init hook

Earliest author timestamp: 2026-10-31T02:07:52+01:00. Range: 2026-10-31.

## Reconstructed Decision

CORE defines the init sequence, `enforce-init.sh` enforces it and reports unread documents, and the base prompt only points to CORE.

## Evidence

- All four commit bodies read; ADR-0111 2026-10-30 amendment read.
- `enforce-init.sh` at HEAD: `collect_missing_init_docs`, 'Mandatory documents still unread', `$project_dir` paths present; `base_prompt.tmpl` step 1 verified.

## Related Decisions

ADR-0111 (amendment covers the CORE-read part), ADR-0236.

## User Context — 2026-11-02

User chose a new ADR scoped to single ownership, citing the 0111 amendment, over folding into 0111 (2026-11-02).
