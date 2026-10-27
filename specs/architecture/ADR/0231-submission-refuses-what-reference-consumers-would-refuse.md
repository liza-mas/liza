# 231 - Submission Refuses What Reference Consumers Would Refuse

## Status

ACCEPTED — implemented 2026-09-23 to 2026-10-23; backfilled 2026-10-25.

## Context and Problem Statement

ADR-0133 made planning artifacts reference-first: strict carriers declare Source
References and Acceptance Contracts, and downstream consumers — the reviewer's
prompt build, child prompt context, and claim-time acceptance — parse and resolve
them. Submission did not. A carrier was first read by the stage that needed it,
after the author's session had ended, so every authoring mistake surfaced as a
blocked consumer and an orchestrator repair:

- An epic planner wrote output fragments as heading slugs
  (`#capability-cap-001---task-creation`). Nothing resolved output fragments before
  merge; every child blocked when its prompt was built, and the replacement repair
  lost their fan-in lineage.
- A slug `spec_ref` into a marker-free story passed submit — the scalar resolver
  treated marker-free fragments as display hints — while claim-time acceptance
  resolved the heading before checking for a marker. Every claim of the resulting
  fix task was refused: 11 orchestrator repairs in one run (D101).
- A malformed Source References line, a missing or ambiguous heading, or a pin
  without its path passed submit and blocked the task at the reviewer claim.
- Claim bounded Direct References by size, but planning submission never read
  them. Plans citing an oversized section merged, and their coding children failed
  only at claim; on 13–14 Oct one was a critical-path repair (B-377, B-416).
- A pin naming no commit was reported as a retryable Git error with no cause.
  Planners retried, then blocked with "root cause unproven" (DEV-760, D-235).

The checks existed; they ran in the wrong place, and producer and consumer each
had their own reading of the same carrier.

## Commit-Evidenced Intent

The commits frame each fix as moving a consumer's refusal to submission while the
author still holds the task, and name divergence between two readings of one
carrier as the cause to remove. The user added no alternatives or trade-offs
beyond those the commits record.

## Considered Options

1. **Leave detection with consumers** — the reviewer prompt build and claim stay
   the only readers; repairs absorb the failures.
2. **Fail `validate` on unresolvable references** — catch pre-existing and later
   drift at every validation.
3. **Reimplement the consumer checks in `ops` for submission** — `agent` imports
   `ops`, so the producer cannot call the consumer's code directly.
4. **Submission runs the consumers' own checks through shared
   `referencecontract` code, and `validate` only warns.**

## Decision Outcome

Chose **Option 4**: whatever a downstream reader of a reference carrier would
refuse, submit-for-review refuses first, through the same implementation.

| Check | Consumer that refused late | Submission behavior |
|---|---|---|
| Output `ref` fragments (spec/epic/plan/arch) | child prompt context | Resolved against a strict carrier in the submitted commit, before rebase and on the rebased boundary, with the prompt resolver's Parse and exact-heading selection; errors tell the author to use exact heading text, not a slug |
| Strict vs legacy carrier | claim-time acceptance | One rule for both readers: a carrier is strict iff it has an eligible Source References or Acceptance Contract heading (presence, not validity); a marker-free carrier is legacy before heading resolution (D101) |
| Authored review carriers | reviewer prompt build | Built with the shared `LoadDiffCarriers` from the candidate's merge-base with integration, and again on the rebased range |
| Direct-reference size | claim | Shared `referenceBoundsRefusal`: cited section ≤ 256 KiB after line-ending normalization, file ≤ 4 MiB; carriers that are not acceptance carriers stay unbounded because nothing downstream refuses them |
| Pin naming no commit, or a path absent at integration HEAD, range base and review commit | reviewer prompt build | Correctable carrier refusal naming the reference, revision and path, or the three observed absences (D-235, DEV-853) |

Content failures are `InvalidCarrierError`, refused as `correct_input` with the
author keeping the task. Git failures stay operational and carry their bounded,
secret-masked cause, so a valid carrier is never reported as needing edits. The
reviewer-side and claim-side checks remain authoritative.

`validate` (CLI only, skippable with `--skip-spec-file-check`) resolves fragments
of non-terminal scalar refs and of unfired merged outputs at integration HEAD and
prints a warning for each failure. That covers state created before the submit
check and headings broken later on integration.

## Rationale

Option 1 is what produced the repairs: by the time a consumer refuses, the author
is gone and the fix costs a replacement task and, at worst, a lost lineage.

Option 2 was rejected in the commit: the same checks run inside state mutations,
so one stale anchor would lock out the repair that fixes it. A warning gives the
signal without blocking recovery.

Option 3 was rejected in the commit: a second copy is exactly the
producer/consumer divergence that let D101 through. Moving the code into
`referencecontract` (no behavior change, `e6504c085`) let both sides call one
implementation.

## Consequences

- Carrier mistakes surface while the author holds the task, as correctable input
  rather than blocked children or reviewer claims.
- A check added on the consumer side does not reach submission by itself; parity
  holds only while both sides call the shared function.
- Submission pays for parsing, resolution and reference reads, twice when it
  rebases.
- The content-versus-Git classification is load-bearing: a deterministic failure
  classed as operational invites retries (the D-235 history), and an operational one
  classed as content asks the author to edit a valid carrier.
- Accepted trade-off (`1a837e148`): an acceptance carrier pinning a file absent at
  its pin gets claim's wording at submission instead of the re-pin hint.
- Drift introduced on integration after merge is only warned by `validate`.

## Related Decisions

ADR-0133 (the carriers being checked), ADR-0134 (the same rule applied to coding
task creation: refuse what claim would refuse), ADR-0139, ADR-0160 (escalation of
the claim refusals this record moves earlier). `INVARIANTS.md` records the
submit-time carrier check in the reference-first row.

## Evidence and Reconstruction

Sources: commit bodies of all nine commits, read in full, with their measured
occurrences (D101: 11 repairs; B-377, B-416; DEV-760). Options 2 and 3 are
rejected in `e7e205ab9` and `e6504c085`; Option 1 is the prior state. The user
confirmed on 2026-10-25 that there were no other alternatives or trade-offs to
record.

---
*Reconstructed from commits `e0cbc16eb`..`4a715e0d0` (2026-09-23 to 2026-10-23).*
