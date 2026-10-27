# A9 Closure: Preservation and Size Evidence

Evidence record of `arm-1-ar-20-cp-0-code-4` (plan Task 5). Manifest:
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-20-cp-0-closure.json`.
Decision record: ADR 0228. Each claim lists the command, the commit and tree it ran on, the outcome
and the before and after values. Test commands ran under `env -i HOME="$HOME" PATH="$PATH"
TMPDIR=/tmp`. The canonical list of the manifest (make test, lint, e2e, regen check, race) is not
run by the coding session; the submit receipt records it.

## Trees

| Name | Commit | Tree | Meaning |
|---|---|---|---|
| start | `3b9ba5d94` | `565771ee7048196fa6e1e8c2bd4a78c30a19077f` | A9 start (A8 closed) |
| pre-edit | `bf0cb8359` | `968e518800b9a5c6b81d5beebf7f6ef910298d74` | start plus the previous A9 units plus the first PB test file; no production edit of this unit |
| closure code | `633a39144` | `1e9dac39590240afe334c8f2dcba03d6578306b3` | last production commit of this unit; sweep line numbers refer to it |

## PB map on the unchanged tree and after

| Claim | Command | Tree | Outcome |
|---|---|---|---|
| PB map green before the first production edit | manifest command 0 (`go test -count=1 -run 'TestA9\|Claim\|ApprovedMerge\|Merge\|Restart\|Exit42\|WaitForWork\|ReviewerWorktree\|Worktree\|Breaker\|Surplus\|Background\|RuntimeFailure\|Block' ./internal/agent/`) | pre-edit | `ok github.com/liza-mas/liza/internal/agent 52.911s` (observed in the coding session, no log kept) |
| PB map green after | same command | closure code | `ok github.com/liza-mas/liza/internal/agent 52.523s` |
| Final PB file passes on the pre-edit production code | disposable detached worktree at `bf0cb8359` (removed afterwards) with the final `internal/agent/a9_concerns_pb_test.go` copied in; `go test -count=1 -v -run 'TestA9Claim\|TestA9Restart\|TestA9Wait' ./internal/agent/` | pre-edit plus final test file | every listed `TestA9Claim*`, `TestA9Restart*`, `TestA9Wait*` test PASS (reproduction; the batch test was revised after the historical run, see below) |
| Infrastructure and guard packages after | `go test -count=1 ./internal/testguard/archguard/ ./internal/agentlaunch/ ./internal/providersignal/ ./internal/process/` | closure code | all four `ok` (archguard 0.179s, agentlaunch 0.028s, providersignal 0.014s, process 0.858s); archguard includes `TestS1SizeGuard`, `TestAreaA9RatchetIsEmpty` and `TestK4ImportDirectionGuard` |

The first PB-3 fixture of the merge batch never reached the merge gate (the negative check found the
quorum scenario vacuous), so `TestA9ClaimPB3MergeBatchContinuesPastFailedCandidate` replaced it:
a failing candidate loses its worktree and branch, a later candidate still reaches MERGED.

Negative checks on the final code, each reverted after observing the failure:

| Mutation | Test that failed |
|---|---|
| a non-halt merge failure returns instead of continuing | `TestA9ClaimPB3MergeBatchContinuesPastFailedCandidate` |
| no polling fallback after a watcher setup or watcher error | `TestA9WaitPB6WatcherSetupFailureFallsBackToPolling`, `TestA9WaitPB6WatcherErrorFallsBackToPolling` |
| surplus check removed from the wait predicate | `TestA9WaitPB6SurplusPrecedesWork` |

## Size before and after

Measured with a `go/parser` program (function end line minus start line plus one, production files
only) over `internal/agent`, `internal/process`, `internal/agentlaunch`, `internal/providersignal`
at the closure code tree: 75 files, 646 functions.

| Function | Before (`3b9ba5d94`) | After (`633a39144`) |
|---|---|---|
| `claimDoerTaskWithOptionalAuthority` | 126 | 17 |
| `handleApprovedMergesWithOptionalAuthority` | 107 | 34 |
| `exit42RestartTracker.Handle` | 111 | 24 |
| `waitForWorkEventDriven` | 132 | 34 |
| `ensureReviewerWorktreeLocked` | 139 | 24 |
| largest A9 function (`releaseTaskClaim`) | 99 | 99 |
| largest A9 file (`registration.go`) | 677 | 678 (one K7 line) |
| `claiming.go` | 732 | 551 (`claiming_merge.go` 272 new) |

Ratchet and S2 of `area_a9_test.go`: 13 ratchet keys at `3b9ba5d94`, 5 before this unit, 0 after
(S2 empty throughout); the allowlist keeps 3 entries (`commands`, `tui`, `agent` to `db`).

## D33 and C6 sites

`git grep -n -E 'Modify[A-Za-z]*\(' <rev> -- 'internal/agent/*.go' ':!internal/agent/*_test.go'`:
19 lines at `3b9ba5d94`, `bf0cb8359` and `633a39144`; one is the test hook
`supervisorBlockBeforeModifyTestHook()` in `supervisor_block.go`, leaving 18 call sites.

| File | Sites at `3b9ba5d94` (line) | Sites at `633a39144` (line) |
|---|---|---|
| `checkpoint_summary.go` | 148 | 151 |
| `claiming.go` | 666 | 485 |
| `provider_audit.go` | 84 | 86 |
| `registration.go` | 82, 263, 324, 488, 528, 623, 654 | 83, 264, 325, 489, 529, 624, 655 |
| `strategy_orchestrator.go` | 143, 298, 413 | 144, 299, 414 |
| `strategy_reviewer.go` | 235 | 235 |
| `supervisor_block.go` | 151 | 152 |
| `supervisor_restart.go` | 74 (`Handle`) | 101 (`persistExit42Restart`) |
| `worktree_check.go` | 182 (`ensureReviewerWorktreeLocked`), 232 (`blockReviewerTask`) | 224 (`recordReviewerWorktreeRecovery`), 271 (`blockReviewerTask`) |

`git grep -h -E 'CheckCurrent\(|CheckLaunchCurrent\(' <rev> -- 'internal/agent/*.go'
':!internal/agent/*_test.go' | wc -l`: 1 at `3b9ba5d94`, `bf0cb8359` and `633a39144`. The callbacks of
the moved sites (`persistExit42Restart` and `blockExit42RestartLoop`, `recordReviewerWorktreeRecovery`,
`blockReviewerTask`, and the pre-existing `claiming.go` clean-terminal callback) were read in full: no
Git or `exec` call; `blockExit42RestartLoop` reads the pipeline configuration files as the original
closure did.

## Shim audit

`git diff -U0 3b9ba5d94 633a39144 -- internal/agent internal/agentlaunch internal/providersignal
internal/process internal/commands internal/tui ':!*_test.go'` piped to the alias and
package-qualified-assignment pattern recorded in ADR 0228: no match. The diff was read for
single-return delegations to `agentlaunch` or `providersignal`: none added.

## Vocabulary sweep

Program, class rules and the per-class `file:line` tables are embedded in ADR 0228. Run at the
closure code commit: 726 rows, 0 unclassified. The program is the single source; the numbers above
reproduce with the command in the ADR.

## Chain-document references

Searched `INVARIANTS.md`, `state-machines.md`, `blackboard-schema.md`, `supervision-model.md`,
`specs/protocols` and `lessons` for backticked Go file names and for every function or type that moved
or left the A9 packages since `3b9ba5d94`. No citation needed changing; the one name that moved
(`handleApprovedMerges`, cited without a path by `supervision-model.md:59`) keeps its name. Recorded
drift unrelated to A9: `blackboard-schema.md` lines 350 and 358 (`auth.go` inside a sample scope
value).

## Brand audit

`git diff -U0 80b3323f5 633a39144 -- internal INVARIANTS.md specs` filtered for added lines containing
`liza` (case-insensitive) outside `github.com/liza-mas/liza`: the only identifier is the existing test
helper `testhelpers.SetupLizaDir`, called by `a9_concerns_pb_test.go`. No presentation text, user-facing
string or documentation line of this unit contains a raw default-brand identifier; the documents of
this unit name the `cmd/liza` directory once (ADR 0228, D3) as a repository path.

## Existing test file changed

\`git diff 80b3323f5 HEAD --stat -- '*_test.go'\` lists new files (\`a9_concerns_pb_test.go\`, the four
\`a9_vocabulary_test.go\`), \`area_a9_test.go\` (closure test added) and one existing file,
\`cmd/liza/agent_generation_e2e_test.go\` (5 lines): the first canonical \`make test\` run failed in
\`TestCLIStaleSupervisorGenerationEndToEnd/exhaustive_operation_inventory\` because the inventory finds
each fenced call by declaration name and the claim and merge reductions had moved five of them. The
entries now name \`claimDoerCandidate\`, \`resumeDoerTask\`, \`clearDegradedAfterClaim\` and
\`mergeApprovedTask\` (\`claiming_merge.go\`); the operation, authority field and binding each entry
asserts are unchanged. After the update \`go test -count=1 -run TestCLIStaleSupervisorGenerationEndToEnd
./cmd/liza/\` printed \`ok\`. The frozen start snapshot \`s1_snapshot_test.go\` is not edited.

## Review iteration 1: typed stream vocabulary

The first review (blocker R1-K7) found the branch-selecting stream kinds named but untyped.
`streamTaskNotification` and `streamToolResult` now have the defined type `streamEventKind`, and
`acpSessionUpdateMessageChunk` has `acpSessionUpdateKind` (`internal/agent/stream_vocabulary.go`);
consumers convert explicitly (`supervisor_background.go`, `acpx_agent.go`). Values and unknown-value
behaviour are unchanged. Commands, all on the fix tree under `env -i`:

| Claim | Command | Outcome |
|---|---|---|
| typed vocabulary and its consumers keep behaviour | `go test -count=1 -run 'TestA9\|Abandoned\|Background\|AcpxAgent\|ACPX\|RuntimeFailure\|StreamEvent' ./internal/agent/` | `ok github.com/liza-mas/liza/internal/agent 3.489s` |
| the `string(...)` conversion at the ACP boundary matters | same package, `-run TestA9ACPUpdateKind`, with the conversion temporarily removed (reverted afterwards) | FAIL: the plain decoded message chunk returned "" and the boxed typed value matched |
| sweep unchanged | the ADR sweep program over the four packages | 726 rows; `file:line` and kind columns identical to the closure code commit |
