# C2 Canonical Provider Keys: Evidence

Task `arm-1-ar-23-cp-0-code-0`. Tree before the change: `8124f434b` (integration head at claim). Recorded
2026-10-27. Acceptance manifest:
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-23-cp-0-c2.json`. Decision:
[ADR-0229](../ADR/0229-policy-time-canonical-provider-keys.md).

## Site list at coding HEAD

The key query is `agentlaunch.NewProviderKeys(base, config).Key(tool)` (`internal/agentlaunch/provider_identity.go`);
`AgentToolRegistry` and the query share `overlayAgentTools` (`launch_resolver.go`). The plan's file names moved
under A9/C6 and were re-found by function.

| Concern | Site (function, file) | Before | After |
|---|---|---|---|
| Claim: doer filter | `isBlockedByDoerDiversityAt`, `ops/reviewer_claim_selection.go` | `claimerProvider != doerAgent.Provider`, `agent.Provider != doerAgent.Provider` | `keys.Same(...)` on both; emptiness checks stay on the recorded value |
| Claim: second-review preference | `hasDifferentProvider` | `a.Provider != claimerProvider` | `!keys.Same(a.Provider, claimerProvider)` |
| Claim: fresh satisfiability | `isDiversitySatisfiable` | `agent.Provider != claimerProvider` | `!keys.Same(...)` |
| Claim: input and selection | `ReviewerClaimEligibilityInput.Keys`, `selectBestCandidate`, `pickWith*Diversity`, `filterDoerProviderDiversity` | none | `agentlaunch.ProviderKeys` threaded; zero value compares recorded names |
| Pool projections | `reviewerStartPlanner.assign`, `matchIdleReviewer`, `FindMissingRolesWithClaimableWork` (`commands/repair_agent_pool_*.go`) | `ReviewerClaimEligible` without keys | `Keys` from `providerKeysOf(projected state)` |
| Merge gate | `evaluateMergeGate` (`agent/claiming_merge.go`) | `task.HasProviderDiversity()`; pool map keyed by `agent.Provider` | `ops.ApprovalsHaveProviderDiversity(approvals, keys)`; pool keyed by `keys.Key`, group named by smallest recorded provider (PD-1) |
| Provider-audit grouping | `checkProviderAuditDegradation`, `checkProviderAuditEvidence` (`analysis/patterns.go`) | `groupByField(..., "provider")` | `groupByProviderKey(..., keys)` |
| Health membership | `allExactProviderEpochsDegraded` | `agent.Provider != provider` | `keys.of(agent.Provider) != key`; the epoch test (`health.Provider != agent.Provider`, PID, registration time, `IsCurrentDegradedFor`) is unchanged |
| Analyze | `ops.Analyze` | `DetectUnacknowledgedPatterns(s)` | `DetectUnacknowledgedPatterns(s, NewProviderKeys(catalogTools, s.Config).Key)` |
| Watch | `checkCircuitBreakerEscalation` (`commands/watch_checks_sprint.go`) | `DetectUnacknowledgedPatterns(state)` | `DetectUnacknowledgedPatterns(state, providerKeyFor(state))` (`commands/provider_keys.go`) |

`models.Task.HasProviderDiversity` stays as the raw utility (PD-3); no production policy call remains on it
(`git grep HasProviderDiversity -- '*.go' ':!*_test.go'` finds its definition and a comment). No S1 ceiling rose;
changed files: reviewer_claim_selection.go 483, patterns.go 741, claiming_merge.go 259, repair_agent_pool_capacity.go
626, watch_checks_sprint.go 220, launch_resolver.go 622 lines.

## Catalog capture placement (C2-INPUTS)

| Entry point | Capture (once, no transaction open) | Config applied |
|---|---|---|
| Reviewer claim | `reviewerClaim.loadPolicy`, before `attempt` | `state.Config` of the locked transaction, per attempt |
| Merge batch | `handleApprovedMergesWithOptionalAuthority`, after `ReadSnapshot` | the snapshot's config, one derivation per batch |
| Analyze | after the preliminary `Read`, before `Modify` | `s.Config` inside `Modify` (locked) |
| Watch | `providerKeyFor`, lazily at the first provider lookup | the evaluated state's config |
| Pool repair | `providerKeysOf` in `newCapacityDemand`, `newReviewerStartPlanner`, `FindMissingRolesWithClaimableWork` | the projected state's config |

No loader, refresh or cache policy was added; no per-agent catalog work. Test packages that reach
`BuiltInAgentTools` newly (`ops`, `commands`) pin the embedded catalog in `TestMain` with the existing
`agentlaunch.UseRuntimeCatalog` (`agent` already did).

## Mutation check (each site)

Each consumer comparison was restored to the raw one (or its keys dropped) in the working tree, the named tests run,
then the file restored (script kept outside the repository; every mutation built and was red; the tree was clean
after each).

| # | Mutation | Test that failed |
|---|---|---|
| M1 | `hasDifferentProvider` raw | `TestC2PartiallyApprovedPreferenceUsesCurrentConfig` |
| M2 | `isDiversitySatisfiable` raw | `TestC2FreshSubmissionSatisfiabilityComparesCanonicalKeys` |
| M3 | doer/claimer comparison raw | `TestC2ClaimDoerDiversityComparesCanonicalKeys`, `TestC2ReviewerClaimEligibilityProjectionAgreesWithClaim`, `TestC2ClaimUsesLockedStateConfig` |
| M4 | alternative-reviewer comparison raw | `TestC2ClaimDoerDiversityComparesCanonicalKeys` (alias of the doer is no alternative) |
| M5 | `ApprovalsHaveProviderDiversity` raw | `TestC2ApprovalsHaveProviderDiversity` |
| M6 | merge pool keyed by `agent.Provider` | `TestC2MergeGateAliasOnlyPoolNamesSmallestRecordedProvider` |
| M7 | analysis grouping raw | `TestC2ProviderAuditGroupsAliasesAsOneProvider` |
| M8 | analysis membership raw | `TestC2ProviderAuditHealthMembershipSpansTheKeyGroup` |
| M9 | `ops.Analyze` keys dropped | `TestC2AnalyzeAliasAnomaliesHaltOnlyWhenEveryKeyGroupMemberIsDegraded` |
| M10 | watch keys dropped | `TestC2AnalyzeWatchAgreeOnCanonicalProviderGroups` |
| M11 | Analyze ignores the locked config | `TestC2AnalyzeUsesLockedConfigAfterPreliminaryRead` |
| M12 | claim ignores the locked config | `TestC2ClaimUsesLockedStateConfig`, `TestC2ClaimDoerDiversityComparesCanonicalKeys` |
| M13 | merge batch ignores the snapshot config | `TestC2MergeGateBatchUsesSnapshotConfig` |
| M14 | planner `Keys` dropped | `TestC2ReviewerStartPlannerDerivesKeysFromProjectedConfig` |
| M15 | `matchIdleReviewer` `Keys` dropped | `TestC2CapacityDemandMatchesIdleReviewersByCanonicalKey` |
| M16 | `FindMissingRolesWithClaimableWork` keys zeroed | `TestC2PoolRepairProjectsCanonicalDoerDiversity` |
| M17 | `selectBestCandidate` keys zeroed in the claim | `TestC2ClaimPrefersCanonicallyDiverseSecondReview` |

The first run left M4 and M17 green, which added the alias-of-the-doer case and the end-to-end second-review test;
M9 and M16 first failed to build (unused variable) and were redone with compiling mutations.

## Old-behaviour edits (old to new)

| Test or fixture | Old | New |
|---|---|---|
| `analysis/patterns_test.go` case "alias-only provider registration" (agent and health `codex-acp`, anomalies `codex`) | CHECKPOINT, health unknown | removed from the raw table; `TestC2ProviderAuditHealthMembershipSpansTheKeyGroup` asserts HALT with canonical keys and the unknown CHECKPOINT with a nil key func |
| A5 oracle `RC4` (`codex` doer, `codex-acp` claimer, `claude` reviewer, preferred) | claims (`claim` adapter, claimed state) | refused (`no-claim` adapter, unclaimed state) |

"Mismatched health provider" (`patterns_test.go`) and `ops/analyze_test.go` "exact provider identity change before
commit prevents stale halt" pass unmodified, as the plan required. All other existing-test edits add a nil or
zero-value key argument (`DetectPatterns`, `DetectUnacknowledgedPatterns`, `selectBestCandidate`,
`isDiversitySatisfiable`, `isBlockedByDoerDiversity(At)`, `filterDoerProviderDiversity`) with no asserted-value change;
`supervisor_merge_test.go` gains a parameterised variant of its helper that the old entry point calls unchanged.

Deviation from the plan: it expected only `step.exit` to change in `RC4.json`. The harness `exit` is the CLI-step
exit code (0 for the refusal in RC2 as well); the refusal is declared by the existing `no-claim` adapter, so the fixture
changes that field and `exit` stays 0.

## Goldens and ledger (C2-ORACLE)

The first `go test ./internal/oracle/...` after the production change failed only `TestOracleA5/RC4`. Every other
oracle package passed unedited. Changed golden: `internal/oracle/a5/testdata/golden/state/RC4.state.yaml`
(regenerated with `ORACLE_WRITE=1`: task stays `CODING_PLAN_MAIN_TO_REVIEW`, no lifecycle receipt, no `claimed`
event, reviewer `WAITING`, `mutation_sequence` absent). Ledger line appended in the same non-merge commit as the
golden and fixture:

```
changed internal/oracle/a5/testdata/golden/state/RC4.state.yaml C2
```

`regen -mode check -pkg ./internal/oracle/...` exits 0 and reports `RC4.state.yaml chain ... C2 <that commit>` and
`a5 package ... C2 <that commit>`; the only other C record is the existing C5 one. Merged `acceptance-a5.json` still
says RC4 "claims under the tool-name rule" and is not edited; this record supersedes that assertion.

## Preserved surfaces (C2-PRESERVE, C2-STOP)

- Stored `agents[].provider`, `approvals[].provider`, anomaly `Details["provider"]` and health records are written
  as before; tests assert the tool names after claim, merge and `analyze`, and that anomalies deep-equal the state's.
- `ReviewerSlotAdmits` and `AgentTools[...]` validation stay tool-keyed (`TestC2ReviewSlotStaysToolKeyed`).
- `git diff --name-only 8124f434b..HEAD` names no `providersignal`, quota, provider-unavailable, `internal/providers`,
  TUI, `Makefile` or embedded file; the signal suites and A9 signal goldens run unedited.
- Launch rendering and display: `launch_resolver.go` changes only by extracting the overlay loop; A9 launch goldens and
  all A1-A9 goldens other than RC4 pass unedited.

## Documents and re-grep (C2-DOCS)

Edited: ADR-0229 and its README row; `blackboard-schema.md` (`provider:` comment); `specs/protocols/circuit-breaker.md`
(pattern condition, HALT proof, two example explanations); `docs/liza-hardened-mas.md` ("Reviewer Diversity").
Re-grep for provider equality statements over `specs/architecture`, `specs/protocols`, `INVARIANTS.md`, `lessons/`,
`README.md`, `docs/` (`different provider|same provider|distinct provider|provider.diversity|HasProviderDiversity`):
the remaining hits are research and survey documents, release notes (historical), `README.md` deployment guidance
(no equality rule), `architecture-review.md` (the issue record that C2 resolves; closure belongs to Scope 25), and
`INVARIANTS.md:218`, whose verdict-time claim stays a recorded registry issue and is not edited here.

## Environment and measurements (C2-ENV)

Disposable repositories, supplied catalogs and `TestMain` pinning; no network, provider, live state, install or push;
no `go test -race` or `make test-race` (D55). G1.3 scan of added production and documentation lines: the only
matches are Go import paths. Focused run of validation command 1 (`agentlaunch`, `providersignal`, `analysis`) and 2
(`TestC2|Diversity|...` over `ops`, `agent`, `commands`): green, 16 s wall for command 2. The canonical commands
`make test`, `make lint`, `make test-e2e`, `go test ./internal/oracle/...` and the regen check are run by
`submit-for-review` on the rebased commit; `go test ./internal/oracle/...` and `regen -mode check` were also run here
once on the unchanged-then-changed tree as described above.
