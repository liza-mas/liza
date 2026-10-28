# C6 Presentation Writes and Mutation Guard: Preservation Evidence

Evidence record of `arm-1-ar-21-cp-0-code-2` (plan Task 3). Manifest:
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-21-cp-0-guard.json`.
Test commands ran under `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp`. The canonical list of the manifest
(`make test`, `make lint`, `make test-e2e`, both regen checks, the a7/a9 oracle packages) is not pre-run by the
coding session; the submit receipt records it.

## Trees

| Name | Commit | Meaning |
|---|---|---|
| start | `5cf35b326` | integration head at claim; production tree before this unit |
| move | `4bd986841` | the C6 move O16-O18, the guard and the A9 rationale; `git diff 5cf35b326 4bd986841 -- internal/oracle` is empty (0 lines) |

Pre-edit runs used a disposable detached worktree at `5cf35b326` (embedded assets copied in), removed afterwards.

## Moved sites

| Op | Old site | New site | Caller keeps |
|---|---|---|---|
| O16 | `commands/init_mas_state.go` `composeState`, `composeConfig`, `initialSprint`, state constants, `bb.Write` | `ops.PublishInitialState` (`internal/ops/init_state.go`) | input resolution with the copy-flag environment read (`initialStateInputs`), `cleanupInit`, wrap `failed to write state file`, log file, output |
| O17 | `commands.MigrateCommand` (`commands/migrate.go`) | `ops.MigrateState` (`internal/ops/migrate_state.go`), file and tests moved with `git mv`, no forwarder | `cmd/liza/cmd_init.go` calls `ops.MigrateState` and prints the same two messages |
| O18 | `toggleAutoResumeCmd` closure (`tui/commands.go`) | `ops.ToggleAutoResume` (`internal/ops/toggle_auto_resume.go`) | message `Auto-resume: ON/OFF`, error prefix `toggle auto-resume:` |

`stringPtrOrNil` (`commands/init.go`) became unused and was removed. Init still uses `Write` (no `Modify`), no
Git or exec call enters any transaction (the D33 count is unchanged: none added), migrate keeps ReadRaw,
`DecodeUnnormalized`, normalization and the conditional `Write` in order with the same error wraps.

## PB map: green on the unchanged tree, and after

| PB item | Tests |
|---|---|
| Init values, versions, goal id, alignment, sprint, breaker, full default config | before: `commands` `TestMASInitStateKeepsFormerValues`; after: `ops` `TestC6InitStateKeepsFormerValues` (same assertions, `reflect.DeepEqual` of the whole `models.Config`), plus `commands` `TestMASInitStateInputsKeepFormerSources` (input mapping, copy flag from parameter and from `models.EnvEnableCopyWorktreeEnvFiles`) |
| Init publication is a whole-state `Write`, raw error returned | new `ops` `TestC6InitStatePublishesWholeStateWithWrite`, `TestC6InitStateReturnsRawWriteError`, `TestC6InitStateEmptyPostWorktreeCommandIsNil` |
| Migrate normalization, idempotence, unchanged state, invalid path, legacy fields, scrubs, terminal-archive logical evidence | before: 16 `commands` `TestMigrateCommand_*` and `TestTerminalArchiveMigrationRetainsUnnormalizedLogicalEvidence`; after: the same 17 bodies as `ops` `TestMigrateState_*` and the archive test (rename only, `git diff -M` shows `MigrateCommand` to `MigrateState` as the only identifier change) |
| Toggle message ON/OFF, error prefix | before and after: `tui` `TestC6ToggleAutoResumeCmdMessagesAndErrorPrefix` (calls `toggleAutoResumeCmd`, present before and after); oracle `TestOracleTUIAutoResume` |
| Toggle is one locked inversion, committed value returned, no extra history | new `ops` `TestC6ToggleAutoResumeInvertsAtomicallyAndReturnsCommittedValue` (state equal except `config.auto_resume` and one `mutation_sequence` step per toggle), `TestC6ToggleAutoResumeReturnsTransactionError` |
| State effects of init, migrate and the toggle through the real binary and TUI | oracle `TestOracleInitWorkspace` (12 cases), `TestOracleMigrate` (legacy, clean, unparseable), `TestOracleTUIAutoResume` |

| Claim | Command | Tree | Outcome |
|---|---|---|---|
| PB map green before any production edit | `go test -count=1 -v -run 'TestMASInitStateKeepsFormerValues\|TestMigrateCommand\|TestTerminalArchiveMigration\|TestC6ToggleAutoResumeCmd' ./internal/commands/ ./internal/tui/` (the toggle test appended to the tui test file of the disposable tree) | `5cf35b326` | `ok commands 0.113s` (17 `--- PASS`), `ok tui 0.031s` (1 `--- PASS`), 0 `--- FAIL` |
| Oracle rows before any production edit | `go test -count=1 -v -run 'TestOracleInitWorkspace\|TestOracleMigrate\|TestOracleTUIAutoResume' ./internal/oracle/a7/install/` | `5cf35b326` | `ok 2.064s`: three `--- PASS` |
| Moved and new tests after | `go test -count=1 -v -run 'TestC6\|Migrate\|TerminalArchiveMigration\|AutoResume\|InitState\|MASInit' ./internal/ops/ ./internal/commands/ ./internal/tui/` | `4bd986841` | `ok` for all three packages, 0 `--- FAIL`; ops: 4 init-state, 2 toggle, 16 `TestMigrateState_*` and the archive test |
| Oracle rows after | same oracle command | `4bd986841` | `ok 1.848s`: three `--- PASS` |
| No oracle file or golden edited | `git diff 5cf35b326 4bd986841 -- internal/oracle` | `4bd986841` | empty |

No oracle gap: O16-O18 state effects are covered by the three A7 rows above, so no A9 row was added.

## Guard

`TestC6MutationGuard` type-checks every production package outside `internal/ops`, `internal/db` and test support
with `go/types` over `go list -e -export -deps` export data for linux, darwin and windows (amd64, no cgo), so
aliases, renamed receivers, embedding and handle chains resolve by object identity.

| Claim | Command | Tree | Outcome |
|---|---|---|---|
| Live tree has no violation and no exception | `go test -count=1 -run TestC6MutationGuard -v ./internal/testguard/archguard/` | `4bd986841` | `0 violations`, pass |
| The guard detects the old sites | the two guard files copied into a `git archive` of `5cf35b326`, same command | `5cf35b326` | 3 violations: `internal/commands/init_mas_state.go:201:15 C6 M1 db.Blackboard.Write`, `internal/commands/migrate.go:77:15 C6 M1 db.Blackboard.Write`, `internal/tui/commands.go:330:13 C6 M1 db.Blackboard.Modify` |
| Each rule has an effective fixture | rule disabled one at a time (M1, M2, C1, B1 and the windows configuration), `go test -run 'TestC6Fixture\|TestC6Classification'` | `4bd986841` plus the temporary edit | `TestC6FixtureRejectsEveryRuleShape` failed in all five cases; edits reverted, package green |
| Fixture rejects every rule shape and accepts every legal case | `TestC6FixtureRejectsEveryRuleShape` (exact 16-line list: five mutators, renamed import, method value, method expression, handle chain, promoted method, interface, literal / named variable / variable-held callback API, windows-only file, unknown-tag file), `TestC6FixtureAcceptsEveryLegalCase` (read-only methods, lock callbacks, snapshot predicate, unrelated `Write` and `Modify`, liveness record, ops/db internals, `_test.go`, test support) | `4bd986841` | pass |
| K1, S1, listing and type failures never pass silently (listing failure also masks inherited environment values, see Environment) | `TestC6UnclassifiedBlackboardMethodFailsK1`, `TestC6StaleClassificationEntryFailsK1`, `TestC6SelfCheckFailsWhenBlackboardIsUnresolved`, `TestC6ListingFailureNamesConfigurationWithoutAbsolutePaths`, `TestC6TypeErrorFailsTheGuard` | `4bd986841` | pass |
| Reached by `make lint` | `make -n lint-archguard` runs `go test -count=1 ./internal/testguard/archguard/`, the package holding `TestC6MutationGuard`; no Makefile change | `4bd986841` | the submit receipt records `make lint` (validation index 7) |

## SI-5, size, N4

| Claim | Evidence |
|---|---|
| A9 `agent -> db` entry carries only the residual rationale | `area_a9_test.go` entry rewritten; agent's non-mutating `db` uses at `4bd986841`: `db.Blackboard` (83 type references), `db.For` (13), `db.LivenessRecord` (2), `db.WriteLivenessRecord` (1); methods `ReadSnapshot` (23), `ReadCached` (5), `Read` (4), `GetStatePath` (3), `WithLockRetryContext` (2), `WatchForChanges` (1), `Patient` (1). `TestK4ImportDirectionGuard` passes (entry still has the edge, not stale) |
| Size | `TestS1SizeGuard` passes; new ops production files `init_state.go` (147 lines) and `toggle_auto_resume.go` (22 lines), `migrate_state.go` moved unchanged in size class, no function near 100 lines, no ratchet entry added; ops imports no agent, commands or tui |
| N4 guard overhead | archguard package `ok` in 1.57-1.70 s warm (pre-C6 measurement 0.664 s); the guard alone 1.27-1.38 s warm; cold build cache (`GOCACHE` empty, all three configurations compiled) `ok 45.644s`. N4 is a Should judged at the boundary |

## Brand scan

`git diff HEAD~1 HEAD -U0 | grep '^+'` for `liza` (case-insensitive word) outside `github.com/liza-mas/liza/` import
paths: no hit. Every retained literal is a Go module import path (ADR-0092 category).

## Reference re-grep (C6-DOCS)

`rg 'MigrateCommand|commands/migrate|migrate\.go|toggleAutoResumeCmd|composeState|composeConfig|init_mas_state'` over
`INVARIANTS.md`, `specs/architecture/*.md`, `specs/protocols`, `lessons`, `docs`: no current reference made stale.
Remaining hits are records not edited by this scope: `architecture-review.md` (review record),
`consolidation-run/a7-k7-inventory.md` (A7 K7 audit of the tree at A7 time) and the goal spec. No edit needed.

## Environment

All commands ran from disposable fixtures and temporary modules under `env -i`; no provider, live state, install or
push. Guard diagnostics carry repo-relative paths only (`TestC6FixtureDiagnosticsNameOnlyRelativePaths`, listing
failure test).

Round-1 finding C6-ENV-1 (the `go list` child inherited the host environment and a failure could echo a value such
as an invalid `GOFLAGS`): `c6ChildEnv` now builds the child environment from an allowlist (`HOME`, `PATH`, `TMPDIR`,
`TEMP`, `TMP`, `GOROOT`, `GOPATH`, `GOCACHE`, `GOMODCACHE` and the Windows-required names) plus `GOOS`, `GOARCH` and
`CGO_ENABLED`; `c6Sanitize` replaces every non-empty retained value, however short, by `$NAME`, after making the
root relative (round 2: an earlier four-character floor still echoed `GOROOT=abc`). Regressions use an explicitly constructed synthetic environment (`GOROOT=/c6-synthetic-goroot-marker`,
`GOFLAGS=-c6-synthetic-flags-marker`, `C6_SYNTHETIC_SECRET=c6-synthetic-secret-marker`, no real value):

| Claim | Test | Red when |
|---|---|---|
| Child inherits only the allowlist | `TestC6ChildEnvWithholdsEverythingOutsideTheAllowlist` | the allowlist test is bypassed: fails naming `GOFLAGS`, the secret name and both marker values |
| A failure that echoes a retained value shows its name only, whatever the value length, and still names the configuration | `TestC6ListingFailureNeverEchoesInheritedEnvironmentValues` (bad `GOROOT` of `/c6-synthetic-goroot-marker`, `abc` and `x`: message `go list (GOOS=linux) exit status 2: go: cannot find GOROOT directory: $GOROOT`) | masking is disabled, or limited to values of four or more characters: fails with the message carrying the value |
| A withheld host `GOFLAGS` no longer breaks the listing | `TestC6HostGoflagsDoNotReachTheListing` | n/a (previously the listing failed with the value in the message) |

The reviewer's probe (`go test -exec 'env GOFLAGS=-C6_DIAGNOSTIC_ENV_MARKER' -run '^TestC6MutationGuard$' ./internal/testguard/archguard/`)
now passes with the marker withheld from the child (0 violations); `-exec 'env GOROOT=abc'` fails as it must (bad GOROOT) with `cannot find GOROOT directory: $GOROOT`.
