# C1 Configured Lease Duration: Evidence

Task `arm-1-ar-22-cp-0-code-0`. Tree before the change: `2f5944f09` (integration head at claim). Recorded
2026-10-27T04:15Z. Acceptance manifest:
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-22-cp-0-c1.json`.

## Site list at coding HEAD

`ops.LeaseDurationSeconds(cfg models.Config) int` (`internal/ops/lease.go`) returns `lease_duration`, or
`models.DefaultLeaseDurationSeconds` (1800) when it is not positive. Callers convert to `time.Duration` at the
duration-taking call. Found with
`git grep -n 'LeaseDurationSeconds(' -- '*.go' ':!*_test.go'`:

| Site | File:line | Before | After |
|---|---|---|---|
| Registration | `internal/agent/supervisor_lifetime.go:141` (`register`) | literal `1800` | resolved once from `ReadSnapshot()` state; passed to `registerAgentWithAuthority` |
| Heartbeat | `internal/agent/supervisor_lifetime.go` (`start`, `HeartbeatConfig.LeaseDuration`) | field omitted, so `DefaultLeaseDuration` | `time.Duration(lt.leaseSeconds) * time.Second`, the value registration used |
| Reviewer claim | `internal/agent/strategy_reviewer.go:382` | literal `1800` | resolved from `bb.ReadSnapshot()` state |
| Re-review reclaim | `internal/ops/review_reclaim.go:74` | const `reclaimReviewLeaseDuration = 30 * time.Minute` (removed) | resolved from the state `readTaskState` already reads; clock instant `time.Now()` still read before the snapshot |
| Pool repair projection | `internal/commands/repair_agent_pool_start.go:158` | `models.DefaultLeaseDurationSeconds` | resolved from the supplied state |

Terminal: `const terminalPlaceholder = "terminal-1"` (`internal/agent/supervisor_lifetime.go:21`); stored value
unchanged. `NewHeartbeat` keeps its `DefaultLeaseDuration` fallback for standalone construction without a duration.
No hard-coded 1800 or `30 * time.Minute` remains at the five sites.

## Mutation check (each site)

For each site the hard-coded value was restored in the working tree, the named test run, then the file restored
(`/tmp/c1mut/run.py`, `run2.py`; build clean in each case).

| Site | Mutation | Test run | Result |
|---|---|---|---|
| Registration | `lt.leaseSeconds` back to `1800` | `TestC1RegistrationHonoursConfiguredLease` | FAIL `configured_600`; passes restored |
| Heartbeat | `LeaseDuration` field omitted | `TestC1Heartbeat*` | FAIL `configured_600` for owned task and active review; passes restored |
| Reviewer claim | duration back to `1800` | `TestC1ReviewerClaimHonoursConfiguredLease` | FAIL `configured_600`; passes restored |
| Re-review reclaim | duration back to `30 * time.Minute` | `TestC1Reclaim*` | FAIL `configured_600` (early and reservation-held); passes restored |
| Pool repair | back to `models.DefaultLeaseDurationSeconds` | `TestC1ReviewerStartPlannerRowsHonourConfiguredLease` | FAIL `configured_600` (spawn CLI and slot rows); passes restored |
| Terminal | constant set to `"terminal-2"` | `TestC1TerminalPlaceholderKeepsStoredValue`, registration | FAIL; passes restored |

Before wiring, the same `configured_600` cases failed on `2f5944f09` production code (registration 30m0s, task
lease, review lease 30 min, pool rows `now + 30m0s`); the `default_unset` cases passed.

## Changed white-box assertion

`internal/ops/await_resubmission_vocab_test.go` `TestReviewLeaseDurationsKeepTheirValues` asserted
`reclaimReviewLeaseDuration == 30*time.Minute`. The constant no longer exists (C1 value only). The assertion on
`reviewOwnershipLeaseMargin == 5*time.Minute` is unchanged; the reclaim value is now asserted by
`TestC1Reclaim*` for 600 and default 1800. `TestAwaitResubmission_ReviewLeaseExpires` (1800 fixture, about
now+30 min) is unmodified and passes.

## D33 / K1 count

No `exec.`, `git.` or `Run(` call is added in the diff (`git diff -U0 -- internal | grep` of added lines, empty).
`LeaseDurationSeconds` is a pure function of `models.Config`, called outside every `Modify` callback. The inherited
`CheckCurrent` and `validateReviewBoundaryForAssignment` calls stay inside `reclaimTransaction.admit/apply` in their
order. D33 inherited count unchanged.

## Brand scan

Added lines in the diff contain no raw default-brand identifier (`git diff -U0 | grep -E '^\+' | grep -ic liza`
excluding file headers returned 0). No presentation string changed.

## Oracle

No file under `internal/oracle` changed. The only oracle fixture with a non-1800 `lease_duration`
(`internal/oracle/a1/persist/testdata/fixtures/state.yaml:103`, 600) is a persistence round-trip, which exercises
none of the five sites. Canonical indexes 1 and 2 run at submission.

## Documentation re-grep

Edited: `specs/architecture/blackboard-schema.md` (config comment, lease rules, lease duration line),
`specs/architecture/supervision-model.md` (review-retention default), `specs/protocols/agent-initialization.md`
(heartbeat step), `specs/protocols/await-primitives.md` (task-lease row). Re-grep
`git grep -n -i -E 'lease_duration|lease duration|30 min|1800' -- specs/architecture/blackboard-schema.md
specs/architecture/supervision-model.md specs/protocols INVARIANTS.md` leaves only unrelated 1800 values
(wait budget, progress timeout, post-worktree timeout, setup timeout). Historical ADRs, `architectural-issues.md`
and `architecture-review.md` (closure owned by the boundary owner) are not edited.

## Command times

Focused matrix `go test -count=1 -run 'TestC1|LeaseDuration|Heartbeat|ReviewerStartPlanner|Reclaim|Resubmission|Regist'
./internal/ops/ ./internal/agent/ ./internal/commands/` on `2f5944f09` plus the working-tree change, 2026-10-27
local: ops 18.4 s, agent 6.5 s, commands 0.5 s, all `ok`; wall 19.1 s. Canonical indexes 1 to 6 are run by
`submit-for-review`; this task does not pre-run them (operator note 2026-10-25T11:20Z).
