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

## Iteration 2: BlockDoerStale output-stream spacing (operator ruling 2026-10-27T16:45Z)

Code commit tested: `a6b016189` (base `global-refactoring` = `09d2c4f58`, no rebase needed).
This section is added by a docs-only commit after it.

**Cause (operator-confirmed against source).** `newAgentOutputLogs`
(`internal/agent/cli_agent_streams.go`) stamps `agent-outputs/<agent>-<second>` with
`time.Now().UTC()` at stream setup, independently of the prompt stamp, and `output.go`
opens it with `O_TRUNC`. Two runs of one agent whose stream setups share a UTC second
overwrite one log, so the listing loses an entry and line 6 shifts. Host load only
widens the gap between prompt save and stream setup. This supersedes the earlier
`awaitAgentOutputs` count barrier (dropped; it targeted the wrong mechanism) and the
"load-induced timeout" reading.

**Change** (test-side, `TestOracleA9BlockDoerStale` only; spacing-barrier variant, not the
per-call output-file wait). After call i's `recordDir/call-*.json` is observed, the test
notes that UTC second; it writes `.liza/oracle-release-i` only once the clock has left both
that second and the prompt's second. The prompt and output stamps of call i are fixed at
or before the observation, so call i+1's stamps fall in a later second, independent of
load and of output-file existence. The comment names both stamps and cites
`newAgentOutputLogs`. Assertions, goldens, fixtures, kit deadline (15 s) and production
code are unchanged. D56: `changed` PROVENANCE records for `exit_release_test.go` and
`kit_test.go`.

**`TestOracleA9BlockReleasedStale` unchanged.** Its two crash exits restart after 5 s and
10 s delays, so consecutive stream setups are at least 5 s apart and cannot share a second.
`TestOracleA9HaltRelease` runs one provider-less start.

**Scope extension (declared in the checkpoint): `kit_test.go`.** The iteration-1 move of the
exit-release rows to `orchStart` removed the last callers of `scenario.start` and
`scenario.launch`; staticcheck `U1000` ("func (*scenario).start is unused", `.launch`)
then failed the pre-commit hook for any commit. Both unused helpers and one stale comment
sentence were deleted; no behavior change. (The iteration-1 hook run predated the rebase
that exposed this.)

### Executions (new, this iteration)

| # | Command | Tree | Outcome |
|---|---|---|---|
| 15 | pre-commit (commit hook, all hooks incl. staticcheck) | `a6b016189` | first attempt failed `U1000` on `kit_test.go` (above); after the helper deletion all hooks passed |
| 16 | `/tmp` test binary of the package, `-test.run '^TestOracleA9BlockDoerStale$'`, 21 runs, 3 parallel workers x 7, `env -i` | `a6b016189` | 21/21 pass; load average 4.6 at start, 4.7 at end |
| 17 | same 24-run shape with `&& now != observed` disabled (prompt-second barrier only) | working tree, restored | 24/24 pass at load ~7: the collision was **not reproduced red**, so no empirical negative control. Argument instead: the extra condition is the only thing that keeps call i+1's stream second from equalling the observation second; the earlier reproduction (~1/8) needed load ~20-25 |
| 18 | `go test -count=1 -timeout=15m ./internal/oracle/a9/...` | pre-`kit_test.go`-edit tree (`b755000d5` + barrier) | ok, 263.4 s |
| 19 | `regen -mode check -pkg ./internal/oracle/a9/...` | `a6b016189` | first run: exit 1 (`exit status 1`), reason not captured (output tail only); rerun with full log: exit 0, 522 ok, 0 non-ok lines |
| 20 | `go test -count=1 -timeout=15m ./internal/oracle/a9/...` | `a6b016189` | ok, 292.3 s (load ~7) |

Run 19's first failure is unexplained; the rerun is reported as such, not as proof. The
earlier iteration recorded a transient `error go-test 7ffba17f6` of the same command.

`git diff --name-only 09d2c4f58..a6b016189`: paths containing `testdata`: 0. Goldens,
fixtures, assertions, kit deadline and production code are unchanged.

**Out of scope, recorded as a defect for follow-up:** the production log-name collision —
agent-output logs are overwritten when one agent starts two runs in the same second. A9
goldens freeze that behaviour; a fix would change behaviour.
