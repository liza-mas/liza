# Cluster 0231 - Submission Refuses What Reference Consumers Would Refuse

## Status

ADR generated: [0231](../0231-submission-refuses-what-reference-consumers-would-refuse.md). Selected on 2026-10-25 as one record despite a one-month span (user decision).

## Commit Set

- `e0cbc16eb` — fix(ops): reject unresolvable strict ref fragments at submit-for-review
- `e7e205ab9` — feat(validate): warn about ref fragments that fail to resolve
- `fab54f2da` — fix(referencecontract): judge ref fragments by one strict-carrier rule (D101)
- `e6504c085` — refactor(referencecontract): share review-carrier discovery with the producer
- `1830329de` — fix(submit): refuse carriers the reviewer's prompt could not build
- `5a0295ea2` — fix(acceptance): bound a reference's cited section, not its whole file (D-169)
- `1a837e148` — fix(acceptance): refuse at submission the references claim would refuse (D-171)
- `b720929dc` — fix(submit): refuse a pin naming no commit and expose carrier causes (D-235)
- `4a715e0d0` — fix(referencecontract): reject unintegrated direct reference pins (#183)

Earliest author timestamp: 2026-09-23T09:17:45+02:00. Range: 2026-09-23 to 2026-10-23.

## Reconstructed Decision

Whatever a downstream reader of a reference carrier would refuse, submit-for-review refuses first, through the same `referencecontract` implementation. Content faults are correctable input; Git faults stay operational with their cause. `validate` warns rather than fails.

## Evidence

- All nine commit bodies read in full. Rejected options are stated in `e7e205ab9` (failing validate locks out repair) and `e6504c085` (a second copy recreates divergence).
- ADR-0133 grep: no submit-time carrier rule. ADR-0134 records the parallel rule for coding-task creation (`306bad23a`).
- INVARIANTS.md reference-first row already lists the submit-time carrier check.

## Related Decisions

ADR-0133, ADR-0134, ADR-0139, ADR-0160.

## User Context — 2026-10-25

One record. No other alternatives or trade-offs to record.
