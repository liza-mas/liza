# A9 D48 ownership correction: acceptance evidence

Task `integration-slice-arm-1-ar-9-cp-0-fix-0` closes slice finding F1 of
`integration-slice-arm-1-ar-9-cp-0.md`. Base: `2f38c8fe6`
(`global-refactoring` at claim time; `HEAD..global-refactoring` was empty).
Code commit: `654378219` (`git rev-parse HEAD` before this document).

## What changed

| Item | Where | Effect |
|---|---|---|
| Supervisor entry points | `kit_test.go`: `runSupervisor`, `startSupervisor`, `startNamedSupervisor` (replaces the now-unused `startNamed`) | Reuse the existing D48 signal-group helper (`signalLaunch`, split out of `signalStart` in `signals_test.go` without behaviour change): subreaper before start; on success, failure and cancellation kill the supervisor group and every child group, reap with bounded waits and assert no child remains, adopted zombies included |
| Isolation | `lifecycle_test.go`, `lifecycle_linux_test.go`, `review_test.go` | `t.Parallel` removed from Registration, AutoAssignID (parent, `success`, `capacity-refused`), DoerClaim, DoerClaimRefused, Restart42, Restart42Limit, SpinBlock, SurplusRetire (parent and subtests), RegistrationCollision, `oracleA9CapacityRefused`, ReviewerClaim, ReviewerClaimRefused, ReviewerWorktree (parent and subtests), Heartbeat. Each supervisor start/run in them now goes through an entry point; AuthorityLost (already serial) uses `startNamedSupervisor` |
| Unchanged on purpose | `lifecycle_other_test.go` (skip stub, D53 skip kept), `exit_release_test.go`, generic `scenario.run/execute/start/launch`, `TestOracleA9PendingMergeStall`, `TestOracleA9ReviewLeaseAssertion`, `TestOracleA9HeartbeatRefused` (parallel, no supervisor) | Out-of-slice parallel supervisor cases keep today's behaviour (residual D48 concern for global analysis) |
| New proofs | `supervisor_owner_linux_test.go` | `TestOracleA9SupervisorOwnership/{cancelled,live-adopted,adopted-zombie}` hold a provider on a wait file and end the supervisor by cancellation, by SIGKILL (provider adopted live) and by SIGKILL then provider kill (adopted zombie never registered anywhere); after each subtest's cleanup every observed pid is gone and the test process has no child. `TestOracleA9SupervisorOwnershipFailure` re-executes a probe that fails on purpose while its provider is held and requires its supervisor and provider to be gone |
| Provenance | `internal/oracle/PROVENANCE` | A9 is a C-changed package since C3 (D56): seven `changed <driver> determinism` records, one per edited or added driver, in the code commit. The edits change test process mechanics only; no asserted value moves |

## Commands and outcomes

All under `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp`, from the task worktree
(`go -C <worktree>` replaces the declared relative invocation; same arguments).

| # | Command | Tree | Outcome |
|---|---|---|---|
| 1 | `go test -count=1 -timeout=15m -run TestOracleA9SupervisorOwnership -v ./internal/oracle/a9/` | code commit sources | exit 0; Ownership 3/3 subtests, Failure probe passed |
| 2 | Negative check: `startSupervisor` temporarily delegated to the generic `p.start`, same command | modified, restored | exit 1: `A9 owner: a started process outlived its test` and `A9 owner probe: a probe process survived the cleanup`; restored, command 1 green again |
| 3 | `go test -count=1 -timeout=15m ./internal/oracle/a9/...` (first run) | code commit sources, before the PROVENANCE amend | exit 1, 281.7 s: sole failure `TestOracleA9BlockDoerStale` (`exit_release_test.go`, out of slice) golden `i05-block-doer-stale/listing.txt` line 6, `agent-outputs` expected, `agent-prompts` observed. The same test and line failed in the slice analysis at the frozen source (`ca8d76a3e`) before this change |
| 4 | `go test -count=3 -run '^TestOracleA9BlockDoerStale$' ./internal/oracle/a9/` and `-count=3 -run '^TestOracleA9(BlockDoerStale\|BlockReleasedStale\|HaltRelease)$'` | same | exit 0 both; the failure did not reproduce alone or with its parallel siblings |
| 5 | `go test -count=1 -timeout=15m -v ./internal/oracle/a9/...` (rerun) | same | exit 0, 287.2 s, 158 PASS lines, no FAIL |
| 6 | `go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a9/...` | `654378219`, clean tree | exit 0, 522 `ok` lines, no `unexplained`/`missing`/`extra` |

Before the amend that added the PROVENANCE records the same regen command on the
clean tree reported `unexplained <driver>: change without record` for the seven
drivers (all 521 goldens `ok`); that is the D56 rule the records answer.

The `BlockDoerStale` failure in row 3 is a pre-existing, intermittent
full-package failure outside this task's scope, not caused by or fixed by it: the
test is untouched, passes alone, with siblings and in the rerun, and failed with
the identical signature before this change. It is recorded, not suppressed.

## Unchanged surfaces

- `git diff --stat 2f38c8fe6..654378219 -- internal/oracle/a9/testdata`: empty
  (no fixture, golden or acceptance-manifest change).
- Files changed: `internal/oracle/PROVENANCE` and seven `internal/oracle/a9/*_test.go`
  files; no production file, no `exit_release_test.go`, no `lifecycle_other_test.go`.
- Asserted values: the diff of the three attributed consumer files removes only
  `t.Parallel()` lines and replaces `p.run(t, "agent", ...)`/`p.start(...)` by the
  entry points (`gofmt` re-indentation aside).

## Non-Linux

`signalReap` keeps the D45 best-effort group kill on hosts without a subreaper;
the new ownership tests are Linux-only (`//go:build linux`) and the package still
skips on Windows (`unixOnly`); the capacity-refused stub keeps its D53 skip.
