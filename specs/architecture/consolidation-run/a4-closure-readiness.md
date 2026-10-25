# A4 Closure Readiness

Evidence for `arm-1-ar-13-cp-5` Task 3 (`arm-1-ar-13-cp-5-code-2`): the reconciliation of chain-document
code references to the A4 files, and the G1–G3 evidence the A5 boundary unit consumes. It records; it
repairs nothing. A discrepancy below is closure debt or a finding, named with its owner. The boundary
unit owns the G1–G8 verdict and the `architectural-issues.md` closures (`issue-proceed` included).

## Coding HEAD

- Integration revision under test (`git rev-parse HEAD` of the task worktree before this record's commit;
  `git log HEAD..global-refactoring` empty): `202d531fc6d5438555f6d1f761ea7dacbbfa04f3`
  (`docs(arch): reconcile the A4 K7 inventory with the typed hand-off classes and named policy versions`).
- Observed on 2026-10-25 (UTC); the shim and exported-API audits and the archguard and regen runs finished by
  17:25Z. The worktree was clean before every command.
- Starting commit of the run, base of every comparison below: `96c9ad69aee983ce2b929dd681b6ba43bf920535`.
- A4 file set (60 production files in `internal/ops`): the 41 files of the area partition, plus the 19
  concern files the A4 scopes created: `add_tasks_delivery`, `contract_amendment_compare`,
  `delete_task_removal`, `dependency_repair_request`, `narrow_inherited_edges`, `obligation_drift_carrier`,
  `plan_amendment_begin`, `plan_check_decision`, `replace_task_admission`, `replace_task_candidate`,
  `retarget_dependency_rewrite`, `set_task_output_checks`, `sprint_summary`, `task_retirement_cleanup`,
  `transition_children`, `transition_cohort`, `transition_inherited_deps`, `transition_pass`,
  `transition_recovery` (all `.go`; creating commits: `git log --diff-filter=A -- internal/ops/<file>.go`).
  `resume_transitions.go` is an A3 file (`09d0121af`) and is outside the set. The set equals the 60 files of
  `a4-k7-inventory.md`.

## 1. Reference reconciliation

### Method

1. Scope: `INVARIANTS.md`, `specs/architecture/{state-machines,blackboard-schema,supervision-model}.md`,
   every file of `specs/protocols/` and `lessons/`.
2. File citations: every `*.go` token in those documents (backticked names, markdown link text and link
   targets, path citations) was resolved against `git ls-files` at coding HEAD. Result: every token resolves
   to a tracked file except three that are not repository citations (`_test.go` as a naming pattern in
   `lessons/agents/large-test-file-reads.md`; `src/auth/refresh.go` and `src/middleware/auth.go` as
   illustrative fixture paths in `blackboard-schema.md` lines 350, 358, 367). No A4 file was deleted or renamed
   since the starting commit (`git diff --name-status -M 96c9ad69a HEAD -- internal/ops`: additions and
   modifications only).
3. Symbol citations, same-line: for each line citing an A4 file, every backticked identifier on that line
   was searched in the cited A4 files' source at coding HEAD. Two lines did not match (below).
4. Symbol citations, anywhere: every backticked Go-shaped identifier (`CamelCase`, `name()`, dotted) of the
   scoped documents was searched in all production and test Go source. Every one exists, except
   `ResumeSession` in `specs/protocols/await-primitives.md:240,242`, which is the subject of a historical
   removal rationale and is not an A4 symbol.
5. Row review of the 22 lines citing an A4 file by name (`git grep` of the 41 partition names): the cited file
   still holds the concern the row cites it for (spot-checked at `pipeline_ops.go` completion lock constants,
   `analyze.go` `AwaitWorkAdmissionBarrier`, `amend_plan.go` `validateBoundedContractCorrection` call,
   `set_task_output_checks.go` `validateDirectCodingAllocation`, `proceed.go` direct-coding generation check,
   `supersede_task.go` / `set_task_output.go` `checkBlockedRecovery`, `task_archive.go`).

### Result

No chain document needs a location correction at coding HEAD: the Scope 2–6 coding units already moved the
citations their extractions invalidated (`INVARIANTS.md` rows at lines 116, 181, 182, 185, 223, 232, 387, 388, 391 now name
`set_task_output_checks.go`, `replace_task_admission.go`, `replace_task_candidate.go`,
`transition_children.go`, `transition_inherited_deps.go`, `transition_recovery.go`, `transition_pass.go`,
`plan_check_decision.go`, `obligation_drift_carrier.go`; `blackboard-schema.md` names `transition_children.go`
for `buildChildTask` / `buildOneToOneChild`). This task therefore edits no chain document.

### Findings (pre-existing drift or observations; not rewritten)

| ID | Location | Finding | Evidence | Owner |
|---|---|---|---|---|
| F1 | `specs/architecture/blackboard-schema.md:627` | Pre-existing drift. States that the checkpoint auto-trigger (`sprint_checkpoint.go`) "still uses `IsUnconsumedPlanningOutput` today". It does not: `CheckpointPassedPlans` loads `PlanHandoffDomain` and admits through `PassedPlansAwaitingCheckpoint(s, domain)` (`orchestrator_wake.go`), and carry-forward uses `collectPendingPlanningHandoffs` / `PlanHandoffDomain.Pending` (`advance_sprint.go`). `IsUnconsumedPlanningOutput` remains in `advance_sprint.go` and is called by `plan_handoff.go` (`Pending`) and `replan.go`. The sentence is inexact at the starting commit too: `git show 96c9ad69a:internal/ops/sprint_checkpoint.go` contains no `Unconsumed` token. The file named resolves; the described behavior does not match. | `git grep -n IsUnconsumedPlanningOutput -- internal/ops`; `internal/ops/sprint_checkpoint.go:36-60`; `git show 96c9ad69a:internal/ops/sprint_checkpoint.go` | A5 boundary unit (record), Scope 25 (documentation rewrite). Not repaired here (`a4-probes` RP10). |
| F2 | `INVARIANTS.md:190` | Observation, no action. Row says apply-dependency-repair validates "inside one `Modify()` callback"; `apply_dependency_repair.go:122` calls `lifecycleMutation(run.bb, authority)(run.mutate)`, which wraps `bb.Modify` (`lifecycle_authority.go:22`). Same call shape at the starting commit (`:110`). The cited file holds the mutation; the wording names the primitive, not the helper. | `git show 96c9ad69a:internal/ops/apply_dependency_repair.go` | none |
| F3 | `INVARIANTS.md` (strict contract amendments row) | Observation, no action. The row cites `amend_plan.go`, `reviewed_range.go`, `acceptance_evidence.go`; the nested-CONTRACT check lives in `validateBoundedContractCorrection` (`contract_amendment.go`, called at `amend_plan.go:241`) with comparison helpers in `contract_amendment_compare.go`. Citation set identical at the starting commit; every cited file resolves and `amend_plan.go` invokes the check. | `git show 96c9ad69a:INVARIANTS.md`; `internal/ops/amend_plan.go:241` | Scope 25 may widen the citation. |

## 2. G1 — A4 ratchet and exceptions

`internal/testguard/archguard/area_a4_test.go` at coding HEAD (guard data, last written by `4d15b2df7`):

| Field | Value |
|---|---|
| `Ratchet` | empty (`[]RatchetEntry{}`) |
| `Layers` | empty (`[]LayerEntry{}`) |
| `Allowlist` | empty (`[]AllowEntry{}`) |
| `S2` | empty (`[]S2Entry{}`): no S2 ruling exists, none was needed |

The 23 A4 keys of the immutable starting snapshot (`s1_snapshot_test.go`: 2 file keys, 21 function keys) are
gone. Measured at coding HEAD over the 60 A4 files with `go/parser` (declaration line to closing brace; physical
lines): longest file `proceed.go` 638 lines (start 2052; `add_tasks.go` 417, start 810); longest function
`validatePlanningOutputAcceptance` 97 lines (limits 800 / 100). Former keys: 19 remain at their original
name and file at 24–84 lines (17 function keys; largest `narrowInheritedDependenciesWithOptionalAuthority` 84,
`setTaskOutputWithOptionalAuthority` 58, `proceedManyToOneInner` 53); `planDeliveryUnits` moved to
`add_tasks_delivery.go` (20 lines) and `recoverCrashedTransition` to `transition_recovery.go` (15 lines), both
after reduction in place; `cancelTaskLifecycle` and `supersedeTaskLifecycle` no longer exist as names (their
operations are `cancelTaskWithOptionalAuthority`, `(*cancelPass).retire` and the `task_retirement_cleanup.go`
concern).

Archguard result (`env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 -v -run TestAreaA4RatchetIsEmpty
./internal/testguard/archguard/`): `--- PASS: TestAreaA4RatchetIsEmpty`; whole package
`go test -count=1 ./internal/testguard/archguard/`: `ok`.

G2 note for the boundary unit: A4's `Allowlist` is empty, so no rationale-carrying entry exists to review, and
the SI-5 crossings are owned by A7 (`jsonout → ops`), A8 (`prompts → ops`) and A9 (`commands`/`tui`/`agent`),
not A4. A4 added no production package (every directory added since the starting commit holds oracle, guard
or test-support code), so its `Layers` stay empty.

## 3. G3 — shim audit

Base `96c9ad69a`, head `202d531fc`, over the 60 A4 files, by AST (`go/parser`, not text search):

| Check | Start (41 files) | Head (60 files) |
|---|---|---|
| Type aliases (`type X = Y`) | 0 | 0 |
| One-statement forwarders whose arguments equal its parameters | 4 | 4 |
| Added alias lines across all `internal/ops` production diff (`git diff 96c9ad69a HEAD -- 'internal/ops/*.go' ':!*_test.go'`, lines `^\+type \w+ =`) | n/a | 0 |

The four forwarders exist at the starting commit and are unchanged, so none was added: `(*handoffInputError).Error`
(`plan_handoff_failure.go`, error-wrapping accessor), `collectNonTerminalByKind` → `models.NonTerminalTasksByKind`
and `resolvePerSubtaskSiblings` → `models.ResolveOutputSiblings` (`proceed.go`), `syncArchiveFile` →
`archiveobject.SyncFile` (`task_archive.go`). The A4 Shim Register is empty and stays empty: **G3 holds, zero
shims added**.

## 4. Exported-API comparison

Method: top-level exported funcs, methods on exported receivers, types (full declaration), consts and vars of
the 41 starting A4 files at `96c9ad69a` versus the 60 A4 files at coding HEAD, printed with `go/printer` and
compared as normalised text.

- Exported declarations: 181 at the start, 181 at head.
- Removed: 0. Added: 0. Signature or declaration text changed: 0.

The exported A4 surface (including the `PlanHandoffDomain` methods A8 consumes until Scope 19, X3) is unchanged
from the starting commit; no X2-documented edit altered an A4 declaration.

## 5. Regeneration and canonical results

| Check | Command (run under `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp`) | Result |
|---|---|---|
| Regen check | `go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a4` | exit 0; 552 golden files reported `ok … H1`, no other line; worktree clean afterwards |
| Archguard | `go test -count=1 ./internal/testguard/archguard/` | `ok` |
| Whitespace | `git diff --check 768d4e16b HEAD -- <declared paths>` | run by submission (canonical command index 0) |
| `make lint`, `make test`, `make test-e2e` | canonical commands 3–5 (deferred) | run by submission on the clean rebased commit; not pre-run here |

The 552 count is one run of the check at coding HEAD; it is not a ceiling.

## 6. Closure debt

None for G1 and G3. F1 is a documentation finding for the boundary unit and Scope 25, not code debt; no
BLOCKED is raised.
