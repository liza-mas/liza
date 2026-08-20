# Global Integration Analysis: Sliced Integration Analysis (generation 1)

## Scope and evidence

Aggregate scan of `91177b4b9299744861670ac8786645d922981692..HEAD` on the
integration branch: 111 files. Non-code files (goal spec, ADR-0113,
`INVARIANTS.md`, `specs/architecture/state-machines.md`,
`specs/protocols/task-lifecycle.md`, `support-docs/*`, planner artifacts) were
read for contract agreement; Go changes were read in full for
`internal/models/integration.go`, `internal/ops/integration_progress.go`,
`internal/ops/integration_reconcile.go`, `internal/ops/pipeline_ops.go`,
`internal/ops/integration_mutation_lock.go`, `internal/statevalidate/integration.go`,
and by diff for `internal/ops/submit_verdict.go`, `internal/ops/wt_merge.go`,
`internal/ops/mode_change.go`, `internal/ops/advance_sprint.go`,
`internal/ops/proceed.go`, `internal/ops/sprint_checkpoint.go`,
`internal/agent/prompt.go`, `internal/agent/workdetection.go`,
`internal/agent/systemctl.go`, `internal/agent/strategy_orchestrator.go`,
`internal/commands/status.go`, `internal/prompts/*`, `internal/pipeline/*`,
`internal/models/{config,task,history}.go`, `internal/embedded/pipeline.yaml`.

Baseline validation on this worktree:

- `make -C <worktree> sync-embedded` then `go build ./...` — exit 0.
- `make -C <worktree> test` — every package `ok`; no `FAIL`, no panic.

So the findings below are not test failures. They are cross-task composition
gaps that the current tests do not exercise.

Corrective round 2 rechecked the runtime capability boundary against
`integration_progress.go`, its zero-slice capability test, the authoritative
master plan, ADR-0113, the task-lifecycle protocol, and the operator guide. It
also reconciled the prior output against the requirement that every generated
fix task have one intent.

## Findings

### 1. Global analysis prompt context rejects the single-scope cohort (blocking)

`EvaluateIntegrationProgress` short-circuits coverage when the frozen
contributing set has fewer than two scopes
(`internal/ops/integration_progress.go:399`), so `decision.Coverage` is empty and
`appendApprovalCoverage` persists no `goal.integration.coverage` records. That is
the documented and tested behaviour: `specs/architecture/state-machines.md`
("With fewer than two contributing scopes it bypasses slices and requests the
first global generation"), `specs/protocols/task-lifecycle.md:408`, and
`TestSlicedIntegrationLifecycle/"settled boundary and zero-slice bypass"`, which
asserts `len(state.Goal.Integration.Coverage) != 0` is a failure for both the
0-scope and 1-scope cohorts while `integration-global-1` is created.

`populateGlobalIntegrationContext` (`internal/agent/prompt.go:607`) then requires
a coverage record for **every** scope in the frozen contributing set and returns
`"global integration context for task %s lacks coverage for plan %q"` otherwise.
`buildTaskRoleContextData` calls it for both `roles.IntegrationAnalyst` and
`roles.IntegrationReviewer` (`internal/agent/prompt.go:402`), so for the
one-contributing-scope goal — the mainline single-code-plan case — neither the
analyst nor the reviewer prompt can be built and the global analysis task cannot
progress. The template already renders the empty case
(`branch_integration_context.tmpl:88`), so only the builder is over-strict.

`TestGlobalIntegrationContext` only covers a two-scope cohort, which is why the
suite is green.

### 2. Integration HEAD is resolved through two different ref spellings

The lifecycle's core equality checks compare a persisted `source_commit` against
"live integration HEAD" (`integration_progress.go:653`,
`integration_mutation_lock.go:64`, `mode_change.go:136`). Five producers of that
value disagree on how to resolve it:

- `internal/ops/integration_reconcile.go:56`, `internal/prompts/wake.go:164`,
  `internal/agent/workdetection.go:194`: `GetCommitSHA(state.Config.IntegrationBranch)`.
- `internal/ops/pipeline_ops.go:147-151`,
  `internal/ops/integration_mutation_lock.go:55-59`:
  `GetCommitSHA("refs/heads/" + branch)` with a `branch == "" -> "main"` default.

`GetCommitSHA` is `git rev-parse <ref>`, whose disambiguation order prefers
`refs/tags/<name>` over `refs/heads/<name>`. In a repository carrying a tag whose
name equals `config.integration_branch`, the bare-name sites resolve to the tag
and the `refs/heads/` sites resolve to the branch. Reconciliation would then mint
a global analysis whose `source_commit` is the tag commit; `verifyCleanIntegrationSource`
would compare it against the branch commit, set `Effective = false`, and refuse to
project closure, while `EvaluateIntegrationProgress` (bare-name HEAD) would see
`latest.SourceCommit == integrationHEAD` and wait forever on
`closure_projection_pending`. The `""` default is also unreachable —
`internal/statevalidate/validate_task.go:34` rejects an empty
`config.integration_branch` — so it only hides the divergence.

### 3. Duplicated live integration wake evaluator

`prompts.evaluateEffectiveIntegrationCompletion` (`internal/prompts/wake.go:158`)
and `agent.evaluateEffectiveIntegrationWakeProjection`
(`internal/agent/workdetection.go:189`) are verbatim duplicates of the same
four-step projection (load frozen pipeline, read integration HEAD, evaluate
progress, project). They were produced by two different coding tasks and now
both feed the authoritative wake vocabulary; a change to one silently diverges
the orchestrator dashboard from the supervisor's wake decision. The fix task
consolidates the live pipeline/HEAD/progress evaluation while retaining the
existing pure wake-vocabulary projection. It does not prescribe
`internal/prompts` as owner: that package already has a documented read-path
business-logic dependency, so the implementer must choose the narrowest
existing dependency-safe boundary.

### 4. Duplicated analysis phase-to-role-pair mapping

`ops.validateIntegrationAnalysisRolePair` (`internal/ops/submit_verdict.go:462`)
re-hardcodes `"slice-integration-pair"` / `"integration-pair"` although the same
package already owns that phase mapping in `ops.analysisRolePair` and the
`sliceIntegrationRolePair` / `globalIntegrationRolePair` constants
(`internal/ops/integration_reconcile.go:19`). This is independent of the wake
evaluator duplication and therefore has its own fix task with no dependency on
the integration-HEAD helper.

### 5. Frozen-pipeline documentation blocks the valid direct-global bypass

The runtime and master plan apply sliced-topology capability only when the
frozen cohort has at least two contributing scopes. `EvaluateIntegrationProgress`
returns the zero-slice decision before testing capability
(`internal/ops/integration_progress.go:399-400`), and
`TestEvaluateIntegrationProgress/fewer than two contributing scopes create no
slices` passes an unavailable capability while asserting the cohort is neither
blocked nor assigned slice work. The master plan states the same boundary: a
zero- or one-scope cohort may use its existing global pair because no slice
topology is required.

ADR-0113's global-readiness wording nevertheless requires complete coverage for
every contributing scope, and its frozen-compatibility section says any partial
legacy topology returns `pipeline_upgrade_required`
(`ADR/0113-sliced-integration-analysis-and-final-closure.md:58-60,104-108`). The
task-lifecycle persisted-evidence wording similarly says every scope has a
coverage record (`task-lifecycle.md:457-460`), while the operator guide says an
incomplete slice/global topology always requires upgrade
(`USAGE_MULTI_AGENTS.md:405-408`). Operators are therefore told the documented
direct-global path is blocked even though runtime and tests deliberately permit
it.

## Checked and not reported

- **Completion/mutation linearization.** The completion -> mutation -> read lock
  order, mutation receipts, `invalidateGoalCompleteStopForMutation`, and the
  post-write snapshot recheck in `StopForGoalCompletion` hold. A clean closure
  persisted against a HEAD that moved between `verifyCleanIntegrationSource` and
  `SubmitVerdict`'s commit is not authoritative: `EvaluateIntegrationProgress`
  re-derives completion against live HEAD and requests the next generation, and
  the next clean verdict overwrites the projection. No false success path found.
- **Frozen legacy pipeline runtime.** `SlicedIntegrationCapability` gates only
  required slice materialization (`integration_progress.go:489`), so a legacy
  `integration-pair`-only topology still reaches global analysis for a zero- or
  one-scope cohort. The contradictory documentation is reported as finding 5.
- **`verifyEffectiveIntegrationOutcome` rejecting `"waiting"`.** Deliberate and
  asserted by `internal/agent/systemctl_test.go:354`; the caller only logs a
  warning and self-heals. Not a defect.
- **Missing `--max-global-integration-generations` CLI flag.** Explicitly excluded
  by the owning plan's scope; the field is configurable through state.
- **`Goal.Integration` lacking a `json` tag.** Matches the `Goal` struct's
  existing yaml-only convention.

## Output

Five atomic fix tasks: finding 1 (correctness, independent), finding 2
(correctness, independent), finding 3 (wake-evaluation ownership, sequenced
after finding 2), finding 4 (role-pair mapping ownership, independent), and
finding 5 (documentation contract reconciliation, independent).
