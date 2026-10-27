# A9 oracle slice integration analysis

## Boundary and disposition

Frozen source: `ca8d76a3ebe3a908bd4bc867f5a95844c860444e`.
Originating plan: `arm-1-ar-9-cp-0`; root lineages: its `code-0` through
`code-4` children. This analysis assesses their intra-plan composition and the
I-01–I-26 shared intent. It does not decide the later A9 boundary CI hold or
review the subsequent runtime refactor and C tasks.

Disposition: one bounded correction is required for the incomplete application
of D48 to the shared kit's lifecycle and reviewer consumers. The five groups
provide the named inventory tests, but their process ownership protocols do not
compose into the suite-wide isolation required by D43/D45/D48.

Review mode: Standard, focused on shared helpers, fixture boundaries, coverage
allocation and process cleanup. No alternative oracle architecture was evaluated;
the approved architecture is the authority. No production or oracle file is
changed by this analysis.

## Sources and coverage

Based on the inherited, pinned A9 Scope 1 carrier and direct references; the
legacy goal document; the five acceptance manifests; the named test drivers,
shared scenario/normaliser/snapshot helpers and adapters at the frozen source;
derived-fixture notes; the provenance ledger; and D40, D42–D48 and D50 in the
decision record. Current human notes were queried, including the earlier
all-agent notices omitted from bootstrap. The root `code-0` lifecycle and
`code-2` reviewer snapshots were also checked to establish finding attribution.

| Obligations | Consumer and composition evidence |
|---|---|
| I-01, I-02, I-04–I-07, I-16 | `lifecycle_test.go` uses the common recipe, stub and snapshots. Raw lease checks precede normalisation; restart cases count calls and claim receipts; surplus retirement distinguishes an idle registration from a held claim. Platform-specific collision/authority tests are present at the frozen source. |
| I-03, I-08–I-11 | `review_test.go` replays the derived branch recipe and verifies its recorded SHA. Reviewer lease checks distinguish the old coder receipt from the new reviewer receipt. Heartbeat success observes the first default beat; the refusal adapter checks authority error, absent liveness and byte-identical state. Recovery and missing-branch cases share the unrecorded-worktree fixture deliberately. |
| I-12–I-15 | `orchestrator_test.go` checks at-launch status, successful versus failed note consumption, trigger clearing/retention, plan disposition and summary emission/refusal. `orchStart` and the platform reapers provide serial process ownership. |
| I-17–I-21 | `signals_test.go` uses the same snapshot contract and seeds signals before capturing their input state. Positive and cross-provider negative cells check alerts, state and signal files; the quota cells distinguish dirty-worktree WIP from clean-worktree exit. `signalStart` owns cancellation and reaping. |
| I-22–I-25 | `launch_test.go` checks both exported spawn entry points through a helper with explicit environment names and a parent sentinel exclusion. Launch coverage is checked against all 16 catalog tools. Invocation documents retain argv, directory and prompt transport/equality while rejecting prompt content. Explain and missing-runner cases assert unchanged state and no provider calls. |
| I-26 | All binary groups call `scenario.check`, which compares before/launch/after state, prompt/output listings and signal/alert/liveness/usage files. Missing committed runtime artifacts are rejected. Per-document normalisers preserve timestamp relationships and equality; the harness retains fixed recipe SHAs. |

These are source-verified mappings, not a claim that each failure path was
independently fault-injected during this analysis. The acceptance manifests are
coverage indexes, not substitutes for execution evidence.

The original inventory's predicted outcomes have authoritative dispositions:

- D40 permits I-06 to pin two plain exit-42 restarts with released claims and no
  restart-loop block. The unreachable BLOCKED branch is explicitly unpinned.
- D42 permits I-11's fixture-driven anomaly read surfaces; its slow trigger stays
  a white-box obligation for the runtime refactor.
- D43 permits the host-dependent collision/capacity correlation suffix token.
- D44 permits the I-13 pure-guard adapter, documents I-14 fixture derivation, and
  replaces the predicted I-15 success alert with the observed starting outcome.
- D50 explains the Linux post-decision watcher barrier and atomic stub records.

The frozen source includes later C3 records for `signals_test.go` and the
`i21-unavailable-codex-run` golden family. Consequently, the historical claim
that every current byte comes directly from the start tree must be interpreted
through the approved D24/D56 chain. Those C3 changes are not attributed to this
oracle plan. Likewise, later additional A9 test files do not expand this slice's
inventory or finding scope.

## F1 — Lifecycle and reviewer consumers bypass suite-wide process ownership

**Severity:** blocker, test correctness and isolation. **Authority:** D48,
which explicitly applies to every A9 test starting supervisors, together with
D43/D45's cleanup requirement. No new production invariant is introduced.

**Verified locations at the frozen source:**

- `internal/oracle/a9/lifecycle_test.go:13`: registration calls `t.Parallel`
  before `p.run(..., "agent", ...)`. Claim, restart, spin and surplus tests also
  run in parallel; auto-assignment and surplus tests have parallel subtests.
- `internal/oracle/a9/review_test.go:180`: reviewer claim runs in parallel through
  `p.run`; refusal at line 202 and recovery at line 238 use the same original kit
  (including parallel subtests); heartbeat success at line 333 also starts a
  supervisor in a parallel test.
- `internal/oracle/a9/kit_test.go:259`: `command` uses a context deadline but does
  not establish descendant ownership. `execute` at line 265 waits for the direct
  command only. `launch` at line 347 registers cleanup consisting only of
  `a.cancel(); <-a.done`; it neither reaps adopted children nor asserts an empty
  child set.
- In contrast, `orchreap_linux_test.go:29,76` establishes the process-wide
  subreaper and enumerates/reaps every actual child, including zombies;
  `orchestrator_test.go:290` registers the supervisor cleanup. The signal and
  launch groups have their own serial ownership paths.

The omissions are attributable to root descendants: the `code-0` snapshot
`35f43754b930cf3c6d605b08b32b900ae9a188e7` already contains the parallel lifecycle
calls, and `code-2` snapshot `d9e91e3307230606d1f6e44ddbd6a6aaf75b685a` contains
the parallel reviewer calls. They remain at the frozen source.

**Failure path:** an assertion/deadline can end a kit-launched supervisor while
its detached provider or descendant is still alive. Direct-command cancellation
and waiting do not implement the required adopted-child cleanup. In a combined
suite, subreaper state established by another group is process-wide, while these
parallel consumers have no exclusive ownership of the test process's children.
Adding an unfiltered reaper alone would endanger concurrently running siblings;
both isolation and cleanup must be applied together. This is a source-proven
D48 violation; this analysis does not claim an observed leaked PID.

**Closure condition:** the attributed lifecycle and reviewer supervisor paths
run without parallel overlap and use the existing D48 cleanup shape on success,
failure and cancellation. A controlled held-provider failure/cancellation test
must prove that detached live children and already-adopted zombies are reaped,
with no children left. Preserve fixtures, goldens, assertions and production
behavior. Pure normaliser/lease tests and adapter-only cases need not be
serialised merely because they share these files. Reuse the existing helper
shape; do not introduce another ownership mechanism.

One downstream task is defined in the adjacent JSON. It repairs this shared
boundary across `code-0` and `code-2`; separate per-consumer repairs would leave
the global subreaper contract vulnerable to partial adoption.

## Validation evidence

Validation is run in the assigned worktree at the frozen source. Only analysis
artifacts are subsequently added; no tested source is changed.

- Full package command below: exit 1, package duration 146.732 s. The sole
  reported failure was `TestOracleA9BlockDoerStale` in
  `testdata/golden/state/i05-block-doer-stale/listing.txt`, line 6:
  expected `agent-outputs/coder-1-<STAMP>.txt`, observed
  `agent-prompts/coder-1-<STAMP>.txt`. This test is in `exit_release_test.go`,
  last changed at later commit `3fe6f3cb50fd5cc41dc6112ca7f7ee89c1a4cab9`, outside
  the supplied descendants/paths. It is not evidence against F1 and is not
  assigned to this plan. No retry of that failed test was performed.
- The narrower attributed inventory/helper command below: exit 0,
  `ok github.com/liza-mas/liza/internal/oracle/a9 148.680s`. This is the evidence
  that the named inventory cases compose on the frozen Linux tree; it does not
  close F1's unexercised failure-path isolation gap or make the full package green.
- Package-filtered provenance check below: exit 0. The captured output reports
  successful A9 reproduction, including chain-pre links at
  `f751d845c3d48d4a01d330cfd80c37ba41b30116`. The execution service retained
  64,647 output bytes under artifact ID
  `67cf3835156b7dbbc2e2df6123a501157804497068f624d943c393e96d439df8`;
  the displayed excerpt and authoritative exit status were inspected, not the
  entire retained transcript. This checks the approved provenance chain and does
  not erase the separate live-package failure recorded above.
- `omni-ee validate-payload set-task-output --payload
  specs/integration-analysis/integration-slice-arm-1-ar-9-cp-0.json --json`:
  COMPLETED, safe_action `continue`, schema_version 1. The executed invocation
  used the absolute worktree payload path and `-C` selected the project root.
- `pre-commit run --files
  specs/integration-analysis/integration-slice-arm-1-ar-9-cp-0.md
  specs/integration-analysis/integration-slice-arm-1-ar-9-cp-0.json`: passed
  applicable conflict/JSON/EOF/whitespace/CRLF hooks; code-only hooks skipped.

Commands executed with the `rtk` prefix from the assigned worktree, at source
`ca8d76a3ebe3a908bd4bc867f5a95844c860444e`:

```sh
env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 -timeout=15m ./internal/oracle/a9/...
env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 -timeout=15m -run '^TestOracleA9(Kit|Stub|Normaliser|LeaseAssertion|ArtifactSet|Registration|RegistrationCollision|AutoAssignID|Heartbeat|HeartbeatRefused|DoerClaim|DoerClaimRefused|AuthorityLost|Restart42|Restart42Limit|SpinBlock|ReviewerClaim|ReviewerClaimRefused|ReviewerWorktree|PendingMergeStall|OrchestratorTurn|OrchestratorPlans|CheckpointSummary|SurplusRetire|AuditAndCleanup|QuotaRaise|UnavailableRaise|SignalMatch|SpawnGate|LaunchLines|ExplainLaunch|LaunchRefused|ReviewLeaseAssertion|OrchestratorReap|OrchestratorAdoptedZombie|LaunchCleanup|LaunchReapsZombie)$' ./internal/oracle/a9/...
env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a9/...
```

## Assessment limits and value

No whole-repository green or cross-platform CI claim is made. The operator's
later Windows C6/A9 runtime findings are outside the slice and are not duplicated.
Existing fixture provenance notes are read as committed evidence; their original
manual derivation commands were not independently repeated.

As submitted, the suite provides broad behavioral preservation coverage but
retains an incomplete failure-path cleanup contract in two consumer groups.
With F1 resolved, those groups share the established process isolation rule,
preserving the oracle's value without changing production behavior or weakening
its assertions. Confidence is high in the source-proven F1; execution confidence
is limited to the commands recorded above.
