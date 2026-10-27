# C6 slice integration analysis

## Boundary and disposition

Frozen source: `ca8d76a3ebe3a908bd4bc867f5a95844c860444e`.
Plan: `arm-1-ar-21-cp-0`; attributed roots: `code-0` at
`8f10c7d73258cc1fccf6ee55487372f5628050da`, `code-1` at
`5cf35b32626cb6c68fb6330e9eee72263eecba68`, and `code-2` at
`2f5944f0978258bc28a24009679b8f7787fd15e8`.

Disposition: one bounded oracle-isolation correction (F1). The inspected named
operation/caller seams and focused preservation checks compose successfully;
the C6-added exit-release oracle consumers do not satisfy the shared D48
process-ownership rule. Passing normal runs do not prove failure-path cleanup.

Review mode: Standard composition review, with targeted P0-P2 and architecture
checks. The approved C6 architecture is the authority; no alternative mutation
architecture is proposed. Later C1-C3 behavior, goal-wide closure and unrelated
issues are outside this slice. No production or existing test is edited here.

## Sources and composition evidence

Based on the inherited pinned C6 carrier and direct references, the legacy goal,
the plan's operation inventory/common rules, the three committed C6 preservation
records, targeted relocation diffs, and the source/test output named below.
The current all-agent and C6 human notes were queried, including omitted notes.
The supplied INVARIANTS concurrency span has later amendments; this analysis uses
the pinned C6 lock/fencing contract and introduces no interpretation of those
later amendments. The registry's Update Policy/Open Issues Summary and ADR index
were consulted; no architectural issue is closed by this analysis.

| Composition seam | Checked evidence and assessment |
|---|---|
| Registration and exit ownership (O1-O6) | `agent/registration.go` retains project then agent lifecycle locks, passes the existing handle and typed `RegisterAgentInput`, and clears stale reviewer claims after registration. `ops/agent_registration.go` retains collision/capacity checks, generation creation and the locked liveness sweep. `agent_exit_release.go` supplies the release witness; `agent/supervisor_block.go` passes that witness or the observed owner/transition to `ops/supervisor_block.go`, which compares inside the transaction. `exit42_restart.go` keeps its distinct release behavior. Focused C6 and D61 tests pass. |
| Runtime bookkeeping (O7-O14) | The `58b5c6cf7..263298679` caller diffs and `reviewer_worktree_records.go`, `clean_terminal_finalize.go`, `orchestrator_turn_records.go` preserve filesystem/Git work outside the transaction, forced-reviewer blocking without new transition validation, the matching-agent release and local handoff timestamp, and pre-turn evidence compared with locked state. Typed counts/observations return to caller logging. C6 state-effect tests and selected generation-fence/human-note/cleanup caller tests pass. |
| Summary claim (O15) | `ops/checkpoint_summary_claim.go` constructs trigger/scope and clears the obligation in one unfenced transaction. `agent/checkpoint_summary.go` retains the in-flight slot, handles error/no-claim release, and emits in the background after the claim. Focused claim and caller tests pass. The existing scope-builder tests are mapped in the bookkeeping preservation record; their original historical execution was not repeated here. |
| Presentation (O16-O18) | `5cf35b326..4bd986841` call-site diffs and `init_state.go`, `migrate_state.go`, `toggle_auto_resume.go` show typed initialization inputs, whole-state Write, archive-aware raw migration with conditional Write, and one locked toggle returning its committed value. The TUI labels/error prefix remain at the caller. Focused ops/TUI tests, the command input-source test and real A7 init/migrate/TUI oracles pass. |
| Guard over all three units | `c6_mutation_guard_test.go` and `c6_mutation_load_test.go` classify the exported Blackboard method set and resolve method/callback uses over Linux, Darwin and Windows source configurations; uncovered production files fail closed. The C6 fixture/live-tree tests pass. The A9 data entry limits agent-to-db use to reads, handle policy and ADR-0177 side records; K4 and S1 pass. `make -n lint-archguard` confirms the existing lint target invokes this package. This is Linux execution of the source guard, not Windows CI evidence. |
| Oracle additions | `git diff --name-status 75d4084d4 2f5944f0978258bc28a24009679b8f7787fd15e8 -- internal/oracle` lists additions only: the three exit-release rows and assessed-note row with fixtures/goldens. All four pass together at the frozen Go tree. The note row uses serial `orchRun`/`orchStart`; the exit-release rows use parallel generic runners (F1). |

The preservation records supply pre-move PB maps, commands, SHAs and results.
They are historical author evidence, not executions by this analyst. D61's
two accepted stale-block pins were independently run here; this does not
reproduce their pre-move runs. No all-oracle provenance regeneration, full
canonical suite, benchmark comparison or cross-platform CI run is claimed.

## F1 — C6 exit-release oracle consumers omit D48 isolation and cleanup

**Severity:** blocker, test isolation/correctness. **Authority:** D48 in the
run decision record, completing D43/D45. It requires every A9 supervisor-starting
test to run without parallel overlap and to own cleanup on failure/deadline
as well as success. This applies to C6's added preservation rows too.

**Verified at the frozen source:**

- `internal/oracle/a9/exit_release_test.go:69,102,123` calls `t.Parallel` in
  `TestOracleA9HaltRelease`, `TestOracleA9BlockReleasedStale`, and
  `TestOracleA9BlockDoerStale`. The first and third use `p.start`; the second
  uses `p.run`. This file was added by C6's oracle-only commit
  `3fe6f3cb50fd5cc41dc6112ca7f7ee89c1a4cab9` and remains unchanged at the frozen
  source, so these consumers are attributed to this slice.
- `kit_test.go:259-285,347-358` provides a context deadline and waits for the
  direct command. Its asynchronous cleanup is only `a.cancel(); <-a.done`.
  Neither runner establishes descendant ownership or reaps adopted children.
- C6's other added row, `orchestrator_notes_test.go:13`, calls `orchRun` without
  parallelism. `orchestrator_test.go:290` invokes `orchGuard`, detaches the
  supervisor and arranges direct-process cleanup before reaping.
  `orchreap_test.go:25` installs the process-wide subreaper before launch;
  `orchreap_linux_test.go:41,76` enumerates actual children, including zombies,
  and reaps them with bounded waits. These helpers explicitly require isolation.

**Failure path and composition risk:** an assertion or deadline can cancel an
exit-release supervisor while its detached provider/descendant survives. The
test may remove its temporary project after waiting only for the supervisor.
The note-row helper also establishes process-wide subreaper state in the same
test binary; the parallel exit-release consumers never adopt the corresponding
ownership discipline. Adding an unfiltered reaper to a parallel consumer would
instead risk killing another consumer's children. Isolation and cleanup must
therefore be adopted together. This is a source-proven D48 violation; no leaked
PID or production state corruption was observed in this analysis.

**Closure condition:** all three named exit-release tests are serial and use the
existing D48 supervisor ownership shape before launch and on normal completion,
cancellation, deadline and assertion failure. Direct supervisors are waited
before the Linux helper reaps all remaining actual children, zombies included,
and asserts none remain. Other platforms retain D45 best-effort group cleanup
and applicable existing skips. Add focused held-provider cancellation/failure
coverage proving these consumers actually use cleanup; reuse existing adopted
zombie coverage where it exercises the same helper. Preserve every assertion,
fixture, golden and production behavior, including the prompt-spacing barrier
and the halt/stale-owner observations. No process tracking or new cleanup
mechanism is needed.

**Bounded ownership:** the correction writes `exit_release_test.go`, new
C6-specific regression files if needed, and its evidence. It reuses existing
cleanup helpers without changing them or the generic kit runners. This keeps
the write set disjoint from the separately reviewed A9 oracle-slice correction.
If the existing helpers cannot satisfy the closure without shared-file edits,
the coder must request an ordered scope adjustment before writing those files.

The current `integration-slice-arm-1-ar-9-cp-0` record is MERGED and explicitly
excludes `exit_release_test.go` from its output. Its committed analysis also
records these three consumers as residual work. F1 is that residual, now
attributed to its C6 owner; it does not duplicate the lifecycle/reviewer repair.
No dependency on an invented correction-task ID is added.

## Earlier evidence reconciled

- The 2026-10-27T09:57Z Windows note predates correction
  `f112f85ed8b2f4dcfacff5537f5305f806ca5dfc`, already in the frozen source.
  `c6_mutation_check_test.go:309-324` now supplies test-owned GOCACHE/TMP/TEMP
  through `c6OwnedGoEnviron`; the selected C6 tests passed here. The earlier
  missing-cache condition is corrected in source and is not commissioned again.
  This analysis does not certify the separate G8 boundary or Windows execution.
- D61 replaces the original plan's D40-only exception for exactly two block
  branches. The ownership evidence records both pre-move and submitted runs;
  both existing tests passed in this analysis. No new production seam is needed.
- The A9 oracle-slice artifact reports an earlier full-package
  `TestOracleA9BlockDoerStale` listing mismatch and later standalone passes.
  The four C6 rows passed together here. That does not establish a cause for the
  historical mismatch or erase it; it is not used as evidence for F1 and no
  retry-until-green or golden change was performed.

## Validation evidence

All commands below ran with the `rtk` prefix from the assigned worktree at
`3bf391dc9537eccc9ec0ba6bfcc85443d129a78d`, before this analysis commit.
`git diff --name-only ca8d76a3ebe3a908bd4bc867f5a95844c860444e HEAD -- cmd internal`
was empty: the tested production/test tree equals the frozen source. Differences
after the frozen source were documentation only and were not attributed to C6.

```sh
env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 -timeout=5m -run 'TestC6|TestBlockExecutingTaskFromSupervisorLeavesAMovedTask|TestSupervisorBlockLeavesALifecycleMovedDuringTheSave|TestMigrate|TestToggleAutoResume' ./internal/ops/ ./internal/agent/ ./internal/commands/ ./internal/tui/ ./internal/testguard/archguard/
```

Exit 0: ops 1.505s, agent 0.558s, tui 0.086s, archguard 36.765s.
Commands reported **no tests to run** for this selection; the final selection
below provides command-layer evidence. No claim relies on that empty selection.

```sh
env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 -timeout=5m -run '^TestOracleA9(HaltRelease|BlockReleasedStale|BlockDoerStale|OrchestratorNotes)$' ./internal/oracle/a9/
```

Exit 0: A9 34.322s. Normal-path preservation passes; F1's failure-path ownership
requirement remains unexercised by these success cases.

```sh
env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 -timeout=5m -run 'TestK4ImportDirectionGuard|TestS1SizeGuard|TestMASInitStateInputsKeepFormerSources|TestOracleInitWorkspace|TestOracleMigrate|TestOracleTUIAutoResume|TestMaybeEmitCheckpointSummary|TestOrchestratorHumanNote|TestSupervisorOwnedMutationGenerationFence|TestCleanTaskCleanupRejectsReplacementPublishedDuringLockWait' ./internal/testguard/archguard/ ./internal/commands/ ./internal/oracle/a7/install/ ./internal/agent/
```

Exit 0: archguard 0.493s, commands 0.242s, A7 install 3.186s, agent 1.108s.
`make -n lint-archguard` exited 0 and printed
`go test -count=1 ./internal/testguard/archguard/`. This is target reachability
evidence, not a claim of executing full `make lint`.

Doc Impact: this analysis and adjacent output JSON. Test Impact: no test edits
for this analysis; F1's child owns the focused cleanup regression. Existing
focused tests validate the composed tree. D55 and the operator's submission
policy retain full canonical execution with submission/area-boundary owners.

Artifact checks: touched-file `pre-commit run --files` passed the applicable
conflict, JSON, EOF, whitespace and CRLF hooks; code-only hooks skipped.
`omni-ee validate-payload set-task-output --payload` on the adjacent JSON
returned COMPLETED, safe_action `continue`, schema_version 1.

## Value and limits

As submitted, C6 establishes named mutation ownership and an executable boundary
guard, but its new exit-release oracle consumers retain unsafe failure cleanup.
With F1 resolved, those preservation tests also compose with the established
process-isolation contract without altering runtime behavior or oracle values.
Confidence is high in the source-proven F1 and the recorded focused results;
this is not a repeat of every descendant review or a goal-wide green verdict.
