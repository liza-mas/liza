# Architecture Consolidation Run: Close Record

Close of the architecture consolidation run (goal `specs/goals/20261023-architecture-consolidation.md`), from the
arm-1-ar-25 plan, Scope 1, Task 2, task `arm-1-ar-25-cp-0-code-1`. The unit links the evidence of the nine area
boundaries and the six behaviour changes; it is not a second issue registry or a replacement specification. It changes
only documentation: `specs/architecture/overview.md`, `specs/architecture/architectural-issues.md`,
`specs/architecture/blackboard-schema.md` (one line), `lessons/humans/branch-switch-during-sprint.md`, this file and
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-25-cp-0-close.json`. It changes no production,
test, guard-data, oracle, baseline, ADR, contract, skill, support-doc or `Makefile` file. `tdd_not_required`:
documentation and evidence only.

## Status

**The run is complete: all nine areas (A1 to A9) are closed.** Each area's boundary record shows G1 to G7 passed and a
green G8 run (D52), A9 included. The run was not stopped early, so no narrowed scope applies. Merging
`global-refactoring` into `main` is outside this record: it is one pull request that the human opens and approves
(D52).

## Final revision

- Integration branch `global-refactoring` (D23), observed `git rev-parse global-refactoring` at 2026-10-27T12:28Z:
  **`6eb9f9fab4fd1f4b681d8b6d17a6ea419e9514d0`** (`docs(arch): record the A9 boundary G1-G8 and close the supervisor,
  lease and canonical provider issues`). The task worktree HEAD was the same SHA, and `git log HEAD..global-refactoring`
  printed nothing, at the start and again after the checks below. The submission of this unit adds only the documentation
  files listed above on top of it.
- Last SHA proven by CI: `7a44042ba68f09011d6bc92a77ea716c105f029a` (A9 G8, run 40492973663, below). The final
  revision differs from it only by documentation: `git diff --name-only 7a44042ba6 6eb9f9fab -- cmd internal plugin
  scripts Makefile go.mod go.sum .github` prints nothing (0 files), and the files that differ are the A9 boundary
  record, the registry, the A9 acceptance manifest and the amendment `arm-1-ar-25-cp-0-amend-1`.
- Starting commit of the run: `96c9ad69aee983ce2b929dd681b6ba43bf920535` (`baseline.md`).
- Environment of every command below: `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` (GUARDRAILS G1.4). No push,
  install, update, provider CLI or live `.liza/` state was involved.

## Area boundaries and G8 runs

One record per boundary; each G8 is a D52 `workflow_dispatch` of `ci.yml` whose tested SHA equals the record's proven
integration SHA, with Linux, Windows and macOS jobs in the same run.

| Area | Boundary record | G8 run | Tested SHA | Result |
|---|---|---|---|---|
| A1 State core | [boundary-a1.md](boundary-a1.md) | [40132329230](https://github.com/liza-mas/liza/actions/runs/40132329230) | `265a14b3ae563d01175eb68d8a98c3501d905f71` | success |
| A2 Platform adapters | [boundary-a2.md](boundary-a2.md) | [40169821321](https://github.com/liza-mas/liza/actions/runs/40169821321) | `3ffd032fcb24b9b914c42da29af3d93ed5cc55e5` | success after attempt 2 (maintainer ruling, 2026-10-25) |
| A3 Ops kernel | [boundary-a3.md](boundary-a3.md) | [40171327023](https://github.com/liza-mas/liza/actions/runs/40171327023) | `98dc9efc270749fb130ae5dbbababecf0bfc35d0` | success |
| A4 Ops planning | [boundary-a4.md](boundary-a4.md) | [40225434048](https://github.com/liza-mas/liza/actions/runs/40225434048) | `20fee7197f2548b8e9844154a874700a603b8aee` | success |
| A5 Ops review | [boundary-a5.md](boundary-a5.md) | [40254021568](https://github.com/liza-mas/liza/actions/runs/40254021568) | `9ce883e8a9af3f59fc3b6547aa63c9fc144e0bfe` | success |
| A6 Ops merge | [boundary-a6.md](boundary-a6.md) | [40283146712](https://github.com/liza-mas/liza/actions/runs/40283146712) | `7677139f664a83d9356029c223c16c0c5e2b6d6c` | success |
| A7 Presentation | [boundary-a7.md](boundary-a7.md) | [40355309126](https://github.com/liza-mas/liza/actions/runs/40355309126) | `4cd8d7d7811ea85b5de90484256ab162301f7169` | success |
| A8 Prompts | [boundary-a8.md](boundary-a8.md) | [40382272433](https://github.com/liza-mas/liza/actions/runs/40382272433) | `73d9f9ac82491471c45b99b835d57921d89903f5` | success |
| A9 Agent runtime (with C6, C1, C2, C3) | [a9-boundary.md](a9-boundary.md) | [40492973663](https://github.com/liza-mas/liza/actions/runs/40492973663) | `7a44042ba68f09011d6bc92a77ea716c105f029a` | success |

The first A9 run, 40480433464 at `225ed0af8`, was red on Windows and is not G8 evidence; the Windows portability
correction that fixed it is merged at `7a44042ba` ([a9-boundary.md](a9-boundary.md), G8). Supporting A9 evidence files:
[a9-supervisor-evidence.md](a9-supervisor-evidence.md), [a9-closure-evidence.md](a9-closure-evidence.md); the K7
occurrence inventories [a4](a4-k7-inventory.md), [a5](a5-k7-inventory.md), [a6](a6-k7-inventory.md),
[a7](a7-k7-inventory.md) and [a4-closure-readiness.md](a4-closure-readiness.md).

## Behaviour changes C1 to C6

| Change | Task (merge commit) | Acceptance manifest | Evidence and decision record |
|---|---|---|---|
| C1 configured lease duration | `arm-1-ar-22-cp-0-code-0` (`8124f434b053bd497133f97f87982d7ce8bf2abb`) | `acceptance/arm-1-ar-22-cp-0-c1.json` | [c1-lease-duration-evidence.md](c1-lease-duration-evidence.md) |
| C2 canonical provider keys | `arm-1-ar-23-cp-0-code-0` (`e63a111b1975471d998416b642e0ead262a83fc5`, with `1dd4045af1722607e1c4390d639b8d779afbc892`) | `acceptance/arm-1-ar-23-cp-0-c2.json` | [c2-provider-keys-evidence.md](c2-provider-keys-evidence.md); [ADR-0229](../ADR/0229-policy-time-canonical-provider-keys.md) |
| C3 canonical unavailable signals | `arm-1-ar-24-cp-0-code-0` (`225ed0af8c7734e67e83baadebc16817afed8e02`) | `acceptance/arm-1-ar-24-cp-0-c3.json` | [c3-unavailable-signals-evidence.md](c3-unavailable-signals-evidence.md); [ADR-0230](../ADR/0230-canonical-provider-unavailable-signals.md) |
| C4 one initialization implementation | `arm-1-ar-18-cp-0-code-0` (`4cd8d7d7811ea85b5de90484256ab162301f7169`; production commit `009d4835a6ab4f0b428a3e97aba13b63297ba9ba`) | `acceptance/arm-1-ar-18-cp-0-c4.json` | [boundary-a7.md](boundary-a7.md) |
| C5 UTC merge timestamps | `arm-1-ar-16-cp-0-code-0` (`7677139f664a83d9356029c223c16c0c5e2b6d6c`; production commit `66f6fa871`, golden `7e4e9f37d`) | `acceptance/arm-1-ar-16-cp-0-c5.json`; change-point record `acceptance/arm-1-ar-0-cp-2-d56-c5-provenance.json` | [boundary-a6.md](boundary-a6.md) |
| C6 mutation through named ops, enforced by a guard | `arm-1-ar-21-cp-0-code-0` (`8f10c7d73258cc1fccf6ee55487372f5628050da`), `-code-1` (`5cf35b32626cb6c68fb6330e9eee72263eecba68`), `-code-2` (`2f5944f0978258bc28a24009679b8f7787fd15e8`) | `acceptance/arm-1-ar-21-cp-0-ownership.json`, `-bookkeeping.json`, `-guard.json` | [c6-ownership-evidence.md](c6-ownership-evidence.md), [c6-bookkeeping-evidence.md](c6-bookkeeping-evidence.md), [c6-presentation-guard-evidence.md](c6-presentation-guard-evidence.md); guard `internal/testguard/archguard/c6_mutation_guard_test.go` |

Acceptance manifests live under `specs/plans/20261023-architecture-consolidation/`. C4, C5 and C6 have no ADR of their
own: C6 implements ADR-0021 and the approved SI-4 direction. The coverage-basis scope (Should) merged
`de166d2d83ab9d3a5c9bb940add7f8c2295aec46`; its manifest is [acceptance-coverage-basis.json](acceptance-coverage-basis.json).

## S1 ratchets and S2 exceptions

Source: the nine `internal/testguard/archguard/area_a1_test.go` to `area_a9_test.go` at `6eb9f9fab`, read directly. The
check `grep -c 'Ratchet: *\[\]RatchetEntry{},' area_a?_test.go` counts 1 in each of the nine files, and no file holds a
`Ceiling:` value. Neither `areas_test.go` nor `s1_snapshot_test.go` was touched: the snapshot is historical evidence of
the 33 files and 93 functions over the thresholds at the starting commit, not active debt.

| Group | `Ratchet` | `S2` | `Allowlist` | Boundary condition test |
|---|---|---|---|---|
| A1 | empty | empty | empty | none (A1 closed by data inspection) |
| A2 | empty | 1 entry (below) | empty | none |
| A3 | empty | empty | empty | none |
| A4 | empty | empty | empty | `TestAreaA4RatchetIsEmpty` |
| A5 | empty | empty | empty | `TestAreaA5RatchetIsEmpty` |
| A6 | empty | empty | empty | `TestAreaA6RatchetIsEmpty` |
| A7 | empty | empty | empty | `TestAreaA7RatchetAllowlistAndS2AreEmpty` |
| A8 | empty | empty | empty | `TestAreaA8DataIsEmpty` |
| A9 | empty | empty | 3 entries (below) | `TestAreaA9RatchetIsEmpty` |

All nine `Ratchet` slices are empty. The only S2 slice that is not empty is A2's.

### S2 list against the overview

The overview's S2 list ([overview.md](../overview.md#size-guard-and-s2-exceptions)) was compared side by side with the
nine slices (`grep -n 'S2: *\[\]S2Entry{$' area_a?_test.go` finds one non-empty block, `area_a2_test.go:38`, and
`grep -c 'Category: ' area_a?_test.go` counts 1 there and 0 in the other eight):

| Source | Key | Category | Rationale |
|---|---|---|---|
| `area_a2_test.go` | `internal/toolchain/catalog.go`, `Catalog` | `table` | declarative tool catalog: one literal entry per tool |
| overview S2 table | `internal/toolchain/catalog.go`, `Catalog` (153 lines, declaration line to closing brace, measured at `6eb9f9fab`) | table | declarative tool catalog: one literal entry per tool |

The lists are equal: one entry, no human ruling needed (a table is data-like), none in the eight other slices. The entry
moved from ratchet to S2 at the A2 boundary ([boundary-a2.md](boundary-a2.md)).

### Final allowlist

Three entries, all in `area_a9_test.go`, each with an owner and a non-empty rationale (full text in the data file and the
[overview](../overview.md#retained-import-crossings)); `TestK4ImportDirectionGuard` fails on a stale entry and passed:

| Importer → imported | Owner | Rationale in one line |
|---|---|---|
| `internal/commands` → `internal/agent` | Scope 20 | read-only wake projection used by `status_orchestrator.go`; no mutation authority |
| `internal/tui` → `internal/agent` | Scope 20 | snapshot-only `RolePauseReason`; no waiting, resume or mutation side effect |
| `internal/agent` → `internal/db` | Scope 21 | non-mutating reads, handle configuration passed to ops and ADR-0177 liveness records; no write authority, C6 rejects any mutation outside ops and db |

No stale or rationale-less entry exists. The Shim Register of every area is empty (A9 audited in
[a9-boundary.md](a9-boundary.md), G3; earlier areas in their records).

## Guard results

| Command | Tree | Result |
|---|---|---|
| `make lint-archguard` (canonical C0) | worktree at `6eb9f9fab` plus this unit's documentation, 2026-10-27T12:30Z | exit 0, `ok …/internal/testguard/archguard 3.467s` (whole package, `-count=1`, S1, K4 and C6 present) |
| `make lint` (canonical C1) | same, 2026-10-27T12:30Z to 12:31Z | exit 0, `GOOS=linux`, `windows` and `darwin` `go vet` and `lint-archguard` (`ok …/archguard 3.874s`) |
| `GOFLAGS=-count=1 go test -v -run 'TestAreaA\|TestS1SizeGuard\|TestK4ImportDirectionGuard\|TestC6MutationGuard' ./internal/testguard/archguard/` | same, 12:28Z | exit 0, `--- PASS` for `TestAreaA4RatchetIsEmpty`, `TestAreaA5RatchetIsEmpty`, `TestAreaA6RatchetIsEmpty`, `TestAreaA7RatchetAllowlistAndS2AreEmpty`, `TestAreaA8DataIsEmpty`, `TestAreaA9RatchetIsEmpty`, `TestC6MutationGuard` (21.61 s, first compile), `TestK4ImportDirectionGuard`, `TestS1SizeGuard`; `ok … 21.763s` |

The three guards exist in the tree: S1 (`s1_guard_test.go`, `TestS1SizeGuard`), K4 (`k4_guard_test.go`,
`TestK4ImportDirectionGuard`) and C6 (`c6_mutation_guard_test.go`, `TestC6MutationGuard`), run by `make lint-archguard`
(`Makefile`: `go test -count=1 ./internal/testguard/archguard/`), which `make lint` calls and `make test` covers through
`./...`. Submission re-runs the two canonical commands on the rebased commit. The wider suite is not repeated for a
documentation change: its last green run is [a9-boundary.md](a9-boundary.md) G4 at `7a44042ba` (`make lint-archguard`,
`make test`, `make lint`, `make test-e2e` all exit 0), valid for the final revision because the production, test and
guard trees are unchanged (see Final revision).

## Documentation reconciliation

Audited against the tree at `6eb9f9fab`. Method: every `*.go` and `internal/`/`cmd/` path cited in the documents was
resolved against tracked files (281 `.go` citations in 29 documents); every backticked camel-case identifier was looked
up in the Go sources; for every line citing a file and an identifier, the identifier was looked up in the cited file(s);
Markdown links and anchors were checked. The area chains had already updated the citations their moves invalidated
(for example ADR-0224 item 18 and ADR-0225 item 23, and the A9 and C6 units for `INVARIANTS.md` rows such as
`agent_registration.go`); this audit confirms none dangles at the end of the run.

| Document | Result |
|---|---|
| `INVARIANTS.md` | Reviewed. No path, symbol or ownership mismatch from a run move. Not edited |
| `specs/architecture/state-machines.md` | Reviewed. Not edited |
| `specs/architecture/blackboard-schema.md` | Reviewed. One line corrected (symbol and owner only, below) |
| `specs/architecture/supervision-model.md` | Reviewed. Not edited |
| `specs/protocols/` (14 documents: acceptance-evidence, agent-initialization, await-primitives, blocked-assessment-idempotency, circuit-breaker, lifecycle-results, payload-validation, replacement-transactions, runtime-inputs, sprint-governance, task-lifecycle, usage-attribution, validation-prerequisites, worktree-management) | Reviewed. Not edited |
| `lessons/` (9 agent lessons, 1 human lesson, 1 README) | One location corrected and one historical note added to the human lesson (below) |
| `specs/architecture/overview.md` | Rewritten for the delivered Go structure: [Go Code Structure](../overview.md#go-code-structure) |
| `specs/architecture/architectural-issues.md` | Run-close reconciliation, see Registry |

**Edits made.** (1) `lessons/humans/branch-switch-during-sprint.md`: `internal/git/worktree.go:283` became
`internal/git/merge.go:99`, where `MergeBranch` is defined (it was already there at the starting commit; the function
has no production caller today), and a dated historical-context note points at the current merge flow. (2)
`specs/architecture/blackboard-schema.md:627`: the predicate and owner of the checkpoint auto-trigger, meaning
unchanged. (3) The registry corrections below. (4) The overview rewrite. No invariant, schema or protocol semantics
changed.

**Intentionally historical or untouched references.**

- `specs/protocols/await-primitives.md` ("Rationale for `ResumeSession` removal") names `LLMAgentRunRequest.ResumeSession`,
  a field removed on purpose; the section records why. `specs/protocols/payload-validation.md` names a documentation-local
  argument `canonicalObject`.
- `specs/architecture/blackboard-schema.md` cites illustrative payload paths (`src/middleware/auth.go`,
  `src/auth/refresh.go`); `lessons/agents/large-test-file-reads.md` uses the `_test.go` glob.
- `specs/protocols/payload-validation.md:9` and `specs/protocols/blocked-assessment-idempotency.md:214` link plan files
  under `specs/plans/20260918-fix-gh-issues/`, absent from the repository at the starting commit and now; they predate
  the run and name no code.
- Historical ADRs, the A1 to A9 boundary records and the sizes in the registry's closure entries keep the values of the
  date they were written (for example `init.go` 516 lines at the A7 closure, 508 now: still far under the limit).

**Semantic discrepancies found and their dispositions.** Four document sentences name a file that exists but does not
hold the claim as worded. All four are identical at the starting commit, none comes from a run move, and none was
rewritten into agreement with different behaviour:

| Location | Discrepancy | Disposition |
|---|---|---|
| `specs/architecture/blackboard-schema.md:627` | said the checkpoint auto-trigger in `sprint_checkpoint.go` "still uses `IsUnconsumedPlanningOutput`" | **Resolved here, symbol and owner only.** `resolveCheckpointTrigger` (`sprint_checkpoint.go:153`) evaluates `PlanHandoffDomain.HasUnfailedHandoff` (`plan_handoff.go:132`), whose own comment says cycle admission "is evaluated separately" (`PlanningCompleteEligible`, `plan_handoff.go:103`, applies it); outside the plan-hand-off domain it reduces to `IsUnconsumedPlanningOutput` (`Pending`, `plan_handoff.go:82`). The line now names that predicate and keeps its meaning: the trigger applies no cycle-blocked exclusion. No decision was needed |
| `lessons/humans/branch-switch-during-sprint.md` | describes a porcelain `git merge` in the main checkout | **Resolved here by annotation.** A dated "Historical context" note states what the merge does now (`MergeTree` and `UpdateRef` in `merge_publication.go`, compare-and-swap, then `SyncMergedFiles` called by `publishForward` in `merge_forward.go` and `RestoreSyncedFiles` called by `restoreCheckout` in `merge_finalize.go`, each under the integration mutation lock, both defined in `internal/git/merge.go`, INVARIANTS §7) and that `MergeBranch` has no production caller; the failure mode and advice are kept as written and stated as not re-validated. The `MergeBranch` location in References is corrected to `internal/git/merge.go:99`. Whether the advice still holds in full is a lesson-content question the annotation does not answer and this record does not claim |
| `INVARIANTS.md:245` (§7 clean sync) | cites `submit_review.go`; only the strict acceptance gate checks a clean worktree | **Open finding, unchanged:** [INVARIANTS.md §7 Clean Worktree Not Enforced at Submission](../architectural-issues.md#invariantsmd-7-clean-worktree-not-enforced-at-submission) already records the conflict and its two directions (enforce, or reword); choosing is a behaviour decision outside this run. The row is not edited |
| `INVARIANTS.md:218` (§6 quorum) | cites `verdict_transition.go`; provider diversity is enforced at reviewer claim and preferred at merge readiness, not at verdict time | **Open finding, unchanged:** [INVARIANTS.md §6 Provider Diversity Not Enforced at Verdict Time](../architectural-issues.md#invariantsmd-6-provider-diversity-not-enforced-at-verdict-time), narrowed in this close to the current owners. The row is not edited |

The two open findings are in the registry with their owner decision pending; the registry is the lifecycle authority for
them and neither blocks the close (neither is in the run's change list, and ADR-0224 note 19 handed them to this record
unresolved).

## Registry

`specs/architecture/architectural-issues.md` stays the sole lifecycle authority. At the close it holds 59 open issues
(17 high, 27 medium, 15 low; the 59 active sections and the 59 summary rows were counted). Nine issues were closed
during the run, each with fixing commits, proof and resulting owner in `Completed Fixes`, `Fixed (Traceability)` and
`Fix Details`; every closure was re-verified against the tree at `6eb9f9fab`:

| Closed | Date | Re-verification at the final revision |
|---|---|---|
| [Split embedded.go by Artifact Family](../architectural-issues.md#split-embeddedgo-by-artifact-family-1530-loc) | 2026-10-25 | `internal/embedded/embedded.go` 128 lines |
| [Decompose proceed.go](../architectural-issues.md#decompose-proceedgo-1500-loc) | 2026-10-25 | `internal/ops/proceed.go` 638 lines, `TestAreaA4RatchetIsEmpty` passes |
| [Prompts Layer Imports Business Logic](../architectural-issues.md#prompts-layer-imports-business-logic) | 2026-10-26 | `git grep -n 'internal/ops' -- internal/prompts ':!*_test.go'` prints nothing; `TestAreaA8DataIsEmpty` passes |
| [Decompose init.go](../architectural-issues.md#decompose-initgo-1268-loc) | 2026-10-26 | `internal/commands/init.go` 508 lines |
| [Decompose watch.go](../architectural-issues.md#decompose-watchgo-1407-loc) | 2026-10-26 | `internal/commands/watch.go` 151 lines |
| [Duplicate Initialization Implementations](../architectural-issues.md#duplicate-initialization-implementations) | 2026-10-26 | no `InitProject` or `InitProjectParams` reference in `cmd` or `internal` outside the immutable start snapshot |
| [Decompose supervisor.go](../architectural-issues.md#decompose-supervisorgo-1129-loc) | 2026-10-27 | `internal/agent/supervisor.go` 198 lines, `TestAreaA9RatchetIsEmpty` passes |
| [Agent Lease Renewal Ignores Configured Lease Duration](../architectural-issues.md#agent-lease-renewal-ignores-configured-lease-duration) | 2026-10-27 | `LeaseDurationSeconds(` resolver at `ops/lease.go:13` with callers `supervisor_lifetime.go:141`, `strategy_reviewer.go:382`, `review_reclaim.go:74`, `repair_agent_pool_start.go:159`; `"terminal-1"` only as `terminalPlaceholder` (`supervisor_lifetime.go:21`) |
| [Canonical Provider Identity Lost Before Policy Enforcement](../architectural-issues.md#canonical-provider-identity-lost-before-policy-enforcement) | 2026-10-27 | `HasProviderDiversity` has no production caller outside `models` (`ops/provider_diversity.go` and `reviewer_claim_selection.go` compare `agentlaunch.ProviderKeys`); `providersignal` matches by canonical key |

This close made no further closure. It corrected the code references the run moved in six open entries (Commands
Layer Imports Agent Runtime, INVARIANTS.md §6 Provider Diversity, Custom Roles Render Empty Prompt Blocks, Index Discovery
Failures Are Unobservable, Provider Support Crosses the Catalog Boundary, Provider Stop-Signal Recovery Has Multiple
Owners) and recorded an Update with the residual in the partial fixes:

- [Commands Layer Imports Agent Runtime](../architectural-issues.md#commands-layer-imports-agent-runtime): narrowed from
  three importing files to `status_orchestrator.go`; the wake-diagnosis dependency remains.
- [Coverage Basis Is Not Reproducible by Repository Tooling](../architectural-issues.md#coverage-basis-is-not-reproducible-by-repository-tooling):
  the coverage-basis scope delivered `-coverpkg=./...` (`de166d2d8`); no deterministic summarization or retained artifact
  exists, so the entry stays open with that residual.
- [Control-Flow Vocabulary Bypasses Domain Ownership](../architectural-issues.md#control-flow-vocabulary-bypasses-domain-ownership):
  cardinalities and roles are typed; five raw `"orchestrator"` role sites remain in `internal/agent`.
- [Provider Stop-Signal Recovery Has Multiple Owners](../architectural-issues.md#provider-stop-signal-recovery-has-multiple-owners):
  primitives moved to `providersignal`; CLI and TUI resume still own detection, clearing and reporting separately.
- [INVARIANTS.md §6 Provider Diversity Not Enforced at Verdict Time](../architectural-issues.md#invariantsmd-6-provider-diversity-not-enforced-at-verdict-time):
  stays open; C2 changed which keys are compared, not where the rule is enforced.
- C1 closed with the terminal value unchanged (`"terminal-1"`); the registry's older recommendation to derive it from the
  launch context is superseded by the goal (C1 acceptance) and is not claimed.

## N3 and N4

Both are Should and block nothing.

**Reuse of the A9 boundary measurements.** The plan allows reusing a boundary's G5 data when the production tree, the
guard workload and the benchmark sources are unchanged between the two revisions. The measured revision is
`7a44042ba68f09011d6bc92a77ea716c105f029a` ([a9-boundary.md](a9-boundary.md), G5: fresh clone, sync-clean check,
warm-up and timed `make test`, three benchmark packages, `GOFLAGS=-count=1`). The check between it and the final
revision `6eb9f9fab`: `git diff --name-only 7a44042ba6 6eb9f9fab -- cmd internal plugin scripts Makefile go.mod go.sum
.github` prints nothing. That path set contains the production code, the guard package
(`internal/testguard/archguard`, the guard workload of `make test` and `make lint`) and every benchmark source
(`internal/db`, `internal/commands`, `internal/agent`). The documentation files this unit adds change none of it, so
the measurements are reused and not repeated.

**N4 (`make test` within +25% of its start time).**

| Run | Wall seconds | Exit | Load (1 min) |
|---|---|---|---|
| Baseline warm-up (`baseline.md`) | 444 | 0 | about 9 rising to about 30 |
| Baseline timed (`baseline.md`) | 523 | 2 (two `internal/agent` deadline tests failed under load) | about 30 |
| A9 boundary warm-up | 238 | 0 | 1.85 |
| A9 boundary timed | 196 | 0 | 5.99 |

The baseline's two observations stay distinct and neither is a clean comparable baseline, so **N4 is not claimed met
against a clean baseline**. Against each observation the final figures are inside the bound (+25% is 555 s of the
444 s warm-up and 653.75 s of the 523 s timed run; 196 s and 238 s are below both). Disposition: Should result, recorded
with the load caveat and the red baseline; a repeat of the baseline on a quiet host would supply a cleaner comparison.
The C6 guard costs `TestC6MutationGuard` 1.38 s inside a 26.8 s archguard package at the A9 boundary.

**N3 (benchmarks, indicative).** All 18 baseline benchmark rows (`internal/db` 14, `internal/commands` 2,
`internal/agent` 2) were captured at their baseline owners; none moved package and none is missing. Every ns/op figure is
below the baseline. Dispositions, from [a9-boundary.md](a9-boundary.md) G5:

- `internal/agent` `BenchmarkClaimReviewerTask` B/op +2.7%, allocs +1.2%: attributed by an A/B at five commits to C2
  loading the canonical provider keys once per reviewer claim; an intended cost of the behaviour change on a path whose
  time is 87% below the baseline. No correction commissioned.
- `internal/commands` `BenchmarkInspectionSnapshotDuringHeartbeat/status` and `/get` B/op and allocs/op above the baseline:
  the observation carried since the A3 boundaries (the benchmark overlaps a concurrent heartbeat writer, so work per
  iteration is not like-for-like; the excess is already present at the starting commit); not attributable to A9 or the C
  chain.
- `internal/db` `BlackboardModify` B/op +0.6%, allocs +0.7%: within noise of the benchmark's own setup.
- No benchmark of the supervisor loop, launch or signal code exists in the baseline set and none requiring a real
  provider was added; behaviour there is covered by the A9 oracle.

## Open `wont` items

The goal keeps these out of the run; their registry entries stay open, and consolidation of adjacent code did not close
them:

- Validation on the blackboard write path: [State Validation Composition Gap](../architectural-issues.md#state-validation-composition-gap).
- Worktree-intelligence refresh policy: [Worktree Intelligence Refresh Has Multiple Owners](../architectural-issues.md#worktree-intelligence-refresh-has-multiple-owners).
- Custom-role prompt behaviour: [Custom Roles Render Empty Prompt Blocks](../architectural-issues.md#custom-roles-render-empty-prompt-blocks).
- Sprint-metric preservation: [Sprint Metrics Lossy at Sprint Boundary](../architectural-issues.md#sprint-metrics-lossy-at-sprint-boundary).
- Git hardening of `liza launch --cwd`: [Raw Git Calls Bypass gitenv Hardening](../architectural-issues.md#raw-git-calls-bypass-gitenv-hardening)
  (one raw call remains, `cmd/liza/launch_resolve.go:127`).
- CI gate expansion beyond K4 and the Python utilities: [CI Does Not Enforce Cross-Language Quality Gates](../architectural-issues.md#ci-does-not-enforce-cross-language-quality-gates),
  [Python Skill Utilities Lack Quality Parity](../architectural-issues.md#python-skill-utilities-lack-quality-parity).
- `--json` error codes and fan-out priority have no distinct registry entry; `plugin/acp` was not touched.

## Process evidence

- Documentation-only diff: the unit's changes are the six files named at the top (the five
  documents and the close manifest). `git diff --name-only 6eb9f9fab6
  HEAD` at submission lists no path outside them.
- Frozen surfaces: no embedded, installed or rendered artifact, `state.yaml` schema, CLI surface or oracle golden was
  touched; `make sync-embedded` was not needed.
- Boundary of `Makefile`, ADRs and the ADR index: read only; the ADR index already lists ADRs 0218 to 0230.
