# C6 exit-release D48 correction: evidence (F1)

Task `integration-slice-arm-1-ar-21-cp-0-fix-0`. Base: `09d2c4f58` (integration head
after the A9 lifecycle/reviewer correction `282db5c59`). Code commit tested:
`edaa0fd7ac8e1ad3d58c9a50f44d86d19a3050b2`; this document is added by the following
docs-only commit, which changes no executable file. All commands ran from the task
worktree under `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp`.

## Change

- `exit_release_test.go`: `TestOracleA9HaltRelease`, `TestOracleA9BlockReleasedStale`
  and `TestOracleA9BlockDoerStale` no longer call `t.Parallel`. Each starts its
  supervisor with `exitReleaseStart`, a wrapper of the existing `orchStart`
  (subreaper via `orchGuard` before launch, own process group, cleanup that cancels
  and waits the direct supervisor, kills its group, then `orchReap` signals, reaps
  with bounded waits and asserts no child remains, zombies included; D45 group
  termination and existing skips elsewhere). `BlockReleasedStale` used `p.run`; it
  now uses `exitReleaseStart` plus `p.finish` (90 s limit, below the 100 s scenario
  context so the deadline path is the observed one). Every assertion, scenario name,
  `p.check` call and the prompt-spacing barrier are untouched.
- `exit_release_cleanup_test.go` (new): `TestOracleA9ExitReleaseConsumersOwned` (source
  check: the three rows are serial, use `exitReleaseStart`, call no unowned start;
  `exitReleaseStart` uses `orchStart`) and `TestOracleA9ExitReleaseCleanup` (re-executes
  `TestOracleA9ExitReleaseCleanupProbe`, which holds a provider in
  `i05-block-doer-stale` and ends by assertion failure, cancellation and missed
  deadline; the outer test is the subreaper and requires that no child outlives the
  probe). Adopted-zombie coverage is reused from
  `TestOracleA9OrchestratorAdoptedZombie`/`TestOracleA9OrchestratorReap`, unchanged.
- `internal/oracle/PROVENANCE`: two append-only `determinism` records for the two
  driver files (a9 is a C package, so the D56 check reports an unrecorded change
  otherwise; precedent `7ffba17f6`). Declared as a scope extension in the checkpoint.
- No shared helper, kit runner, production file, fixture or golden is edited.

## Unchanged fixtures and goldens

`git diff --name-status 09d2c4f58..edaa0fd7a`: `M internal/oracle/PROVENANCE`,
`A internal/oracle/a9/exit_release_cleanup_test.go`, `M internal/oracle/a9/exit_release_test.go`.
Paths containing `testdata`: 0.

## Executions (new, this task)

| # | Command | Tree | Outcome |
|---|---|---|---|
| 1 | `go test -count=1 -run ExitRelease ./internal/oracle/a9/` | pre-rebase code | pass |
| 2 | Mutation: `exitReleaseStart` returns `p.start(...)` | pre-rebase code | `ConsumersOwned` red (needs `orchStart`); `ExitReleaseCleanup` red: no `A9PROBE adopted: cancelled` (held provider never adopted, no subreaper). Restored. |
| 3 | Mutation: remove `t.Cleanup(orchReap)` from `orchGuard` | pre-rebase code | `ExitReleaseCleanup` red: "3 children of the probe outlived it". Restored (`orchreap_test.go` not in the diff). |
| 4 | pre-commit on the two test files | pre-rebase code | all hooks passed |
| 5 | `go test -count=1 -timeout=15m ./internal/oracle/a9/...` | `29fd5ceb6` (pre-rebase) | ok, 174.5 s |
| 6 | `regen -mode check -pkg ./internal/oracle/a9/...` | `29fd5ceb6` | exit 1: two `change without record`. Fixed by the PROVENANCE records. |
| 7 | same regen | `285c00c6e` (amended) | exit 0, 522 ok |
| 8 | rebase onto `09d2c4f58` | conflict only in append-only PROVENANCE; union kept | `edaa0fd7a` |
| 9 | regen check | `edaa0fd7a` | exit 1 at first run: `error go-test 7ffba17f6` (the historical C3 commit's own `TestOracleA9QuotaRaise/i19-other`, "a started process outlived the test"), not this change; the immediate rerun exit 0, 522 ok. Both reported. |
| 10 | `go test -count=1 -run 'ExitRelease\|OrchestratorReap\|AdoptedZombie\|HaltRelease\|BlockReleasedStale\|BlockDoerStale' -v` | `edaa0fd7a` | all pass (HaltRelease 31.3 s, BlockReleasedStale 15.7 s, BlockDoerStale 3.0 s) |
| 11 | `go test -count=1 -timeout=15m ./internal/oracle/a9/...` | `edaa0fd7a` | **first run FAIL** (328 s): `TestOracleA9BlockDoerStale` listing golden differs at line 6 (two `agent-outputs` entries instead of three; host load average ~25). |
| 12 | `go test -count=8 -run '^TestOracleA9BlockDoerStale$'` | `edaa0fd7a` | 1 of 8 failed with the same listing difference |
| 13 | same, with the pre-change `exit_release_test.go` (base `09d2c4f58`) and the new file moved aside; restored afterwards | base | 1 of 8 failed with the same signature, so the flake predates this change (F1 already records an earlier full-package listing mismatch of this test) |
| 14 | `go test -count=1 -timeout=15m ./internal/oracle/a9/...` (rerun, no change) | `edaa0fd7a` | ok, 351.4 s |

The run-11 failure and its isolation reproduction (12, 13) are not explained or fixed
here: the cause is outside this scope (asserted listing, goldens and kit are frozen).
Run 14 is a rerun on unchanged state, reported as such, not as proof the flake is gone.
Historical results (F1's "four C6 rows passed together", the A9 slice artifact's standalone
passes) are not reused as evidence for this correction.
