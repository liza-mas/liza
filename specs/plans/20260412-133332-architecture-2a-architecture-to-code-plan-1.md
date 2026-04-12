# Code Plan: Wire --json into All 28 Included CLI Commands

**Task:** architecture-2a-architecture-to-code-plan-1
**Architecture:** specs/arch-plan/20260412-110120-architecture-2a.md (Sections 3.5, 5.1-5.5, 6.1-6.7, 9.3, Scope 1)
**Spec:** specs/goals/20260412-cli-native-access-control.md (Section 2)
**Depends on (code):** architecture-2a-architecture-to-code-plan-0 (provides `internal/jsonout/` package and `cmd/liza/main.go` helpers `addJSONFlag`/`isJSON`/`ErrAlreadyWritten` handling)

## Overview

Register `--json` on all 28 included CLI commands and implement the JSON branch in each command's RunE. When `--json` is set: (1) a deferred error guard catches pre-ops errors and writes JSON error envelopes, (2) `log.SetOutput(io.Discard)` suppresses ops-level stderr noise, (3) the ops layer is called directly (bypassing `commands.*Command()`) and the typed result is wrapped in the standard `jsonout.Envelope`.

Three internal packages require preparatory changes: `internal/ops/*.go` result structs need `json:"snake_case"` tags for stable serialization, `internal/commands/status.go` needs the resolver-loading extracted from `buildStatusData` so warnings can be intercepted, and `internal/commands/validate.go` needs a `SetWarnWriter` function to redirect warning output to a buffer.

### Wiring Pattern (arch doc Section 3.5)

Every included command follows this pattern:

```go
RunE: func(cmd *cobra.Command, args []string) (retErr error) {
    if isJSON(cmd) {
        log.SetOutput(io.Discard)
        defer log.SetOutput(os.Stderr)
        defer func() {
            if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
                _, _ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
                retErr = jsonout.ErrAlreadyWritten
            }
        }()
    }

    // ... existing argument resolution, RBAC validation (shared path) ...

    if isJSON(cmd) {
        result, err := ops.SomeOperation(...)
        return jsonout.WriteResult(os.Stdout, result, nil, err)
    }
    return commands.SomeCommand(...)
}
```

**Key:** The named return `(retErr error)` is required for the deferred error guard. The argument resolution and RBAC validation are shared between JSON and non-JSON paths. Only the final call differs: JSON calls ops directly, non-JSON calls commands (human-formatted output).

**Envelope warnings vs result-level warnings:** Most commands pass `nil` for envelope warnings. The result struct itself may contain a `Warnings` field (e.g., `ClaimResult.Warnings`) which is serialized as part of the result. Envelope-level warnings are reserved for command-level concerns that don't belong in the result struct. Only three commands use envelope warnings: `update-sprint-metrics` (suspicious rate warnings from `ops.CheckSuspiciousRates`), `status` (resolver-load failure warnings), and `validate` (non-fatal validation warnings from `warnWriter`).

---

## CP1: Add `json:"snake_case"` tags to all ops result structs and SprintMetrics

**Intent:** Add explicit `json:"snake_case"` struct tags to all result types that appear in `--json` output, providing stable agent-facing field names instead of relying on Go's default PascalCase encoding.

**Files (all modify — tags only, no behavioral change):**

| File | Struct(s) |
|------|-----------|
| `internal/models/sprint.go` | `SprintMetrics` (match existing `yaml:"..."` names, add `json:"-"` to `Extra`) |
| `internal/ops/claim_task.go` | `ClaimResult` |
| `internal/ops/add_tasks.go` | `AddTaskResult`, `AddTasksResult`, `AddTaskItemResult` |
| `internal/ops/submit_review.go` | `SubmitForReviewResult` |
| `internal/ops/submit_verdict.go` | `VerdictResult` |
| `internal/ops/handoff.go` | `HandoffResult` |
| `internal/ops/mark_blocked.go` | `MarkBlockedResult` |
| `internal/ops/release_claim.go` | `ReleaseClaimResult` |
| `internal/ops/supersede_task.go` | `SupersedeResult` |
| `internal/ops/cancel_task.go` | `CancelResult` |
| `internal/ops/assess_blocked.go` | `AssessBlockedResult` |
| `internal/ops/assess_hypothesis_exhausted.go` | `AssessHypothesisExhaustedResult` |
| `internal/ops/wt_create.go` | `CreateWorktreeResult` |
| `internal/ops/wt_delete.go` | `DeleteWorktreeResult` |
| `internal/ops/wt_merge.go` | `MergeResult` |
| `internal/ops/analyze.go` | `AnalyzeResult` |
| `internal/ops/sprint_checkpoint.go` | `SprintCheckpointResult` |
| `internal/ops/await_verdict.go` | `AwaitVerdictResult` |
| `internal/ops/await_resubmission.go` | `AwaitResubmissionResult` |

**Tag convention:** `json:"field_name"` using snake_case derived from Go field names (e.g., `TaskID` -> `json:"task_id"`, `ReviewCommit` -> `json:"review_commit"`, `FastForward` -> `json:"fast_forward"`). For `SprintMetrics`, reuse existing `yaml:"..."` names (e.g., `yaml:"tasks_done"` -> `json:"tasks_done"`). For `SprintMetrics.Extra` (`map[string]any` with `yaml:",inline"`), add `json:"-"` since `encoding/json` has no inline equivalent and the field is for YAML forward-compatibility only.

**Done when:** All 20 files compile with explicit `json:"snake_case"` tags on every exported field of the listed structs. `SprintMetrics` has 11 `json:"..."` tags matching the existing `yaml:"..."` names, plus `json:"-"` on `Extra`. No behavioral change — existing YAML serialization and tests are unaffected. `go build ./internal/...` succeeds. `go test ./internal/ops/... ./internal/models/...` passes (existing tests still green).

**Scope:** `internal/models/sprint.go`, `internal/ops/claim_task.go`, `internal/ops/add_tasks.go`, `internal/ops/submit_review.go`, `internal/ops/submit_verdict.go`, `internal/ops/handoff.go`, `internal/ops/mark_blocked.go`, `internal/ops/release_claim.go`, `internal/ops/supersede_task.go`, `internal/ops/cancel_task.go`, `internal/ops/assess_blocked.go`, `internal/ops/assess_hypothesis_exhausted.go`, `internal/ops/wt_create.go`, `internal/ops/wt_delete.go`, `internal/ops/wt_merge.go`, `internal/ops/analyze.go`, `internal/ops/sprint_checkpoint.go`, `internal/ops/await_verdict.go`, `internal/ops/await_resubmission.go`.

---

## CP2: Refactor `buildStatusData` signature in `internal/commands/status.go`

**Intent:** Extract resolver loading from `buildStatusData` and export it as `BuildStatusData` so `cmd/liza/cmd_system.go` can call it with pre-loaded resolver and intercepted warnings for the `--json` warnings field. Preserve `projectRoot` in the signature for `buildOrchestratorStatus` and `ops.AvailableManualTransitions`.

**Files:**
- `internal/commands/status.go` (modify)

**Current state (line 130):**
```go
func buildStatusData(state *models.State, detailed bool, projectRoot string) statusData
```
Internally calls `ops.LoadResolverForModels(projectRoot)` at line 155, logging warnings via `log.Printf`.

**Target state:**
```go
func BuildStatusData(state *models.State, detailed bool, projectRoot string,
    pr models.PipelineResolver, prWarnings []string) statusData
```
Exported (capital B). No longer calls `ops.LoadResolverForModels` internally. Receives pre-loaded resolver and any load warnings from caller.

**Changes required:**
1. Rename `buildStatusData` -> `BuildStatusData` (export for use by `cmd/liza/cmd_system.go`'s JSON branch).
2. Change signature to add `pr models.PipelineResolver` and `prWarnings []string` parameters.
3. Remove the `ops.LoadResolverForModels(projectRoot)` call and `log.Printf` warning from inside `BuildStatusData` (lines 155-158).
4. Use the `pr` parameter where `BuildStatusData` currently uses its local `pr` variable (lines 159, 162).
5. Update `StatusCommand` (line 117) to load the resolver before calling `BuildStatusData`:
   ```go
   pr, prErr := ops.LoadResolverForModels(opts.ProjectRoot)
   if prErr != nil {
       log.Printf("WARNING: status: failed to load pipeline resolver: %v", prErr)
   }
   status := BuildStatusData(state, opts.Detailed, opts.ProjectRoot, pr, nil)
   ```
6. `prWarnings` parameter: `BuildStatusData` does not use it internally (warnings are passed through the envelope by the JSON caller). It exists in the signature so the JSON caller in `cmd_system.go` can pass warnings through. For `StatusCommand` (non-JSON path), pass `nil`.

**Done when:** `BuildStatusData` (exported) has signature `(state *models.State, detailed bool, projectRoot string, pr models.PipelineResolver, prWarnings []string) statusData`. `BuildStatusData` no longer calls `ops.LoadResolverForModels` — the caller (`StatusCommand`) loads the resolver and passes it in. `StatusCommand` preserves existing behavior: loads resolver, logs warning on failure, calls `BuildStatusData` with pre-loaded resolver and `nil` warnings. `go test ./internal/commands/...` passes (existing `status_integration_test.go` tests still green). `go build ./internal/commands/` succeeds.

**Scope:** `internal/commands/status.go`.

---

## CP3: Wire `--json` into `cmd/liza/cmd_task.go` (11 commands)

**Intent:** Register `--json` flag and implement the JSON branch for all 11 task commands: `claim-task`, `add-task`, `add-tasks`, `supersede-task`, `cancel-task`, `mark-blocked`, `assess-blocked`, `assess-hypothesis-exhausted`, `write-checkpoint`, `set-task-output`, `set-discovery-disposition`.

**Files:**
- `cmd/liza/cmd_task.go` (modify)

**Per-command mapping (ops functions called in JSON mode):**

| Command | Ops Function | Result Type | Notes |
|---------|-------------|-------------|-------|
| `claim-task` | `ops.ClaimTask(projectRoot, taskID, agentID)` | `*ops.ClaimResult` | Positional args[0]=taskID, args[1]=agentID |
| `add-task` | `ops.AddTask(statePath, logPath, opsInput, orchestratorID)` | `*ops.AddTaskResult` | Resolve flags into `commands.TaskInput`, convert to `ops.AddTaskInput` (same conversion as `commands.AddTaskCommand` line 43-53). Pass result.Warnings via result struct, not envelope. |
| `add-tasks` | `ops.AddTasks(statePath, logPath, input)` | `*ops.AddTasksResult` | Resolve --tasks-file flag. The current RunE (line 496-524) already builds `*ops.AddTasksInput`. |
| `supersede-task` | `ops.SupersedeTask(projectRoot, taskID, replacementIDs, reason, agentID)` | `*ops.SupersedeResult` | Parse replacementIDs from args[1:], --reason flag, orchestrator ID. |
| `cancel-task` | `ops.CancelTask(projectRoot, taskID, reason, agentID)` | `*ops.CancelResult` | args[0]=taskID, --reason flag (optional), orchestrator ID. |
| `mark-blocked` | `ops.MarkBlocked(projectRoot, taskID, reason, questions, agentID)` | `*ops.MarkBlockedResult` | args[0]=taskID, --reason, --questions, --agent-id flags. |
| `assess-blocked` | `ops.AssessBlocked(projectRoot, taskID, note, agentID)` | `*ops.AssessBlockedResult` | args[0]=taskID, --note flag, orchestrator ID. |
| `assess-hypothesis-exhausted` | `ops.AssessHypothesisExhausted(projectRoot, taskID, note, agentID)` | `*ops.AssessHypothesisExhaustedResult` | args[0]=taskID, --note flag, orchestrator ID. |
| `write-checkpoint` | `ops.WriteCheckpoint(projectRoot, input)` | `null` (void) | Resolve flags into `*ops.WriteCheckpointInput`. Void success: `jsonout.WriteResult(os.Stdout, nil, nil, err)`. |
| `set-task-output` | `ops.SetTaskOutput(projectRoot, input)` | `null` (void) | Resolve --output-file flag into `*ops.SetTaskOutputInput`. Void success. |
| `set-discovery-disposition` | `ops.SetDiscoveryDisposition(projectRoot, discoveryID, disposition)` | `null` (void) | Positional args[0]=discoveryID, args[1]=disposition. Void success. |

**Flag registration:** Add `addJSONFlag(...)` calls in the `init()` function (after line 550) for all 11 commands: `claimTaskCmd`, `addTaskCmd`, `addTasksCmd`, `supersedeTaskCmd`, `cancelTaskCmd`, `markBlockedCmd`, `assessBlockedCmd`, `assessHypothesisExhaustedCmd`, `writeCheckpointCmd`, `setTaskOutputCmd`, `setDiscoveryDispositionCmd`.

**Implementation notes:**
- All 11 RunE functions need the named return `(retErr error)` for the deferred error guard.
- The existing argument resolution and any RBAC validation (if present from sibling task merges) are shared between JSON and non-JSON paths.
- For `add-task`: the `commands.TaskInput` -> `ops.AddTaskInput` conversion in `commands.AddTaskCommand` (lines 43-53 of `internal/commands/add_task.go`) must be replicated in the JSON branch. This is the same field-by-field struct copy.
- For `add-tasks`: the RunE already constructs `*ops.AddTasksInput` before calling `commands.AddTasksCommand`. The JSON branch uses the same input.
- For void-success commands (`write-checkpoint`, `set-task-output`, `set-discovery-disposition`): the ops function returns only `error`. On success, write `jsonout.WriteResult(os.Stdout, nil, nil, nil)`.
- New imports needed: `"errors"`, `"io"`, `"log"`, `"os"`, `"github.com/liza-mas/liza/internal/jsonout"`, `"github.com/liza-mas/liza/internal/ops"`.

**Done when:** All 11 commands in `cmd_task.go` accept `--json` flag. When `--json` is set: RunE has named return `(retErr error)`, deferred error guard catches pre-ops errors and writes JSON error envelope, `log.SetOutput(io.Discard)` suppresses ops-level logs, ops function is called directly and result is wrapped in `jsonout.Envelope` via `jsonout.WriteResult`. Void-success commands return `{"ok": true, "result": null}`. Flag registration added in `init()`. `go build ./cmd/liza/` succeeds. Existing tests in `cmd/liza/mutation_wiring_test.go` still pass (non-JSON path unchanged).

**Scope:** `cmd/liza/cmd_task.go`.

**Depends on:** CP1 (json tags on result structs).

---

## CP4: Wire `--json` into `cmd/liza/cmd_review.go` (6 commands)

**Intent:** Register `--json` flag and implement the JSON branch for all 6 review commands: `submit-for-review`, `handoff`, `submit-verdict`, `release-claim`, `await-verdict`, `await-resubmission`.

**Files:**
- `cmd/liza/cmd_review.go` (modify)

**Per-command mapping:**

| Command | Ops Function | Result Type | Notes |
|---------|-------------|-------------|-------|
| `submit-for-review` | `ops.SubmitForReview(projectRoot, taskID, commitSHA, agentID)` | `*ops.SubmitForReviewResult` | args[0]=taskID, args[1]=commitSHA, --agent-id flag. |
| `handoff` | `ops.Handoff(input)` | `*ops.HandoffResult` | Resolve flags into `*ops.HandoffInput`. Reference `commands.HandoffCommand` for input construction. |
| `submit-verdict` | `ops.SubmitVerdict(projectRoot, taskID, verdict, reason, agentID, impact)` | `*ops.VerdictResult` | args[0]=taskID, args[1]=verdict, args[2]=reason (optional), --agent-id, --impact flags. |
| `release-claim` | `ops.ReleaseClaim(projectRoot, taskID, role, force, reason, agentID)` | `*ops.ReleaseClaimResult` | args[0]=taskID, --role, --force, --reason, --changed-by flags. |
| `await-verdict` | `ops.AwaitVerdict(ctx, projectRoot, taskID, agentID, timeout)` | `*ops.AwaitVerdictResult` | args[0]=taskID, --agent-id, --timeout flags. Needs `context.Background()`. |
| `await-resubmission` | `ops.AwaitResubmission(ctx, projectRoot, taskID, agentID, timeout)` | `*ops.AwaitResubmissionResult` | args[0]=taskID, --agent-id, --timeout flags. Needs `context.Background()`. |

**Flag registration:** Add `addJSONFlag(...)` calls in `init()` for all 6 commands.

**Implementation notes:**
- Same wiring pattern as CP3 (named return, deferred error guard, log suppression).
- `await-verdict` and `await-resubmission` need `context.Background()` for their ops calls. Check how the current RunE resolves the context — the `commands.AwaitVerdictCommand` likely creates it internally.
- `release-claim` uses `--changed-by` (not `--agent-id`). The `resolveChangedBy` function returns the identity. In JSON mode, this identity is passed to the ops function.
- New imports: `"context"`, `"errors"`, `"io"`, `"log"`, `"os"`, `"github.com/liza-mas/liza/internal/jsonout"`, `"github.com/liza-mas/liza/internal/ops"`.

**Done when:** All 6 commands in `cmd_review.go` accept `--json` flag. When `--json` is set: deferred error guard, log suppression, ops called directly, result wrapped in envelope. `go build ./cmd/liza/` succeeds. Existing tests still pass.

**Scope:** `cmd/liza/cmd_review.go`.

**Depends on:** CP1 (json tags on result structs).

---

## CP5: Wire `--json` into `cmd/liza/cmd_worktree.go` (3 commands)

**Intent:** Register `--json` flag and implement the JSON branch for all 3 worktree commands: `wt-create`, `wt-delete`, `wt-merge`.

**Files:**
- `cmd/liza/cmd_worktree.go` (modify)

**Per-command mapping:**

| Command | Ops Function | Result Type | Notes |
|---------|-------------|-------------|-------|
| `wt-create` | `ops.CreateWorktree(projectRoot, taskID, fresh)` | `*ops.CreateWorktreeResult` | args[0]=taskID, --fresh flag. |
| `wt-delete` | `ops.DeleteWorktree(projectRoot, taskID)` | `*ops.DeleteWorktreeResult` | args[0]=taskID. |
| `wt-merge` | `ops.MergeWorktree(projectRoot, taskID, agentID)` | `*ops.MergeResult` | args[0]=taskID, --agent-id flag. |

**Flag registration:** Add `addJSONFlag(...)` calls in `init()` for all 3 commands.

**Implementation notes:**
- Same wiring pattern. `wt-merge` already has RBAC validation (role type check) — the JSON branch's deferred error guard catches RBAC errors.
- New imports: `"errors"`, `"io"`, `"log"`, `"os"`, `"github.com/liza-mas/liza/internal/jsonout"`, `"github.com/liza-mas/liza/internal/ops"`.

**Done when:** All 3 commands in `cmd_worktree.go` accept `--json` flag. When `--json` is set: deferred error guard, log suppression, ops called directly, result wrapped in envelope. `go build ./cmd/liza/` succeeds. Existing tests still pass.

**Scope:** `cmd/liza/cmd_worktree.go`.

**Depends on:** CP1 (json tags on result structs).

---

## CP6: Wire `--json` into `cmd/liza/cmd_system.go` (6 commands)

**Intent:** Register `--json` flag and implement the JSON branch for all 6 system commands: `analyze`, `update-sprint-metrics`, `clear-stale-review-claims`, `sprint-checkpoint`, `get`, `status`. Includes the special handling for `update-sprint-metrics` (typed `SprintMetrics` result + `CheckSuspiciousRates` warnings) and `status` (pre-loaded resolver with intercepted warnings via refactored `buildStatusData`).

**Files:**
- `cmd/liza/cmd_system.go` (modify)

**Per-command mapping:**

| Command | Ops/Commands Function | Result Type | Special Handling |
|---------|----------------------|-------------|-----------------|
| `analyze` | `ops.Analyze(projectRoot)` | `*ops.AnalyzeResult` | Standard pattern. |
| `update-sprint-metrics` | `ops.UpdateSprintMetrics(projectRoot)` + `ops.CheckSuspiciousRates(metrics)` | `models.SprintMetrics` | **Typed result** — returns full SprintMetrics, not void. Suspicious rate warnings go in envelope `warnings` field. See arch doc Section 6.1. |
| `clear-stale-review-claims` | `ops.ClearStaleReviewClaims(projectRoot)` | `map[string]int{"cleared": N}` | ops returns `(int, error)`. Wrap count in map. See arch doc Section 6.6. |
| `sprint-checkpoint` | `ops.SprintCheckpoint(projectRoot, "manual")` | `*ops.SprintCheckpointResult` | Standard pattern. Check how `commands.SprintCheckpointCommand` calls ops for the trigger parameter. |
| `get` | `commands.InspectCommand(args, opts)` | Dynamic (query-dependent) | Force `opts.Format = "json"`, parse result string into `any` via `json.Unmarshal`, wrap in envelope. See arch doc Section 6.3. |
| `status` | `commands.buildStatusData(state, detailed, projectRoot, pr, warnings)` | `statusData` | **Resolver-warning capture**: caller loads resolver via `ops.LoadResolverForModels`, intercepts error as warning, passes pre-loaded resolver + warnings to `buildStatusData`. See arch doc Section 6.2. Needs to read state first via `db.For(statePath).Read()` (same as `StatusCommand` does). |

**Flag registration:** Add `addJSONFlag(...)` calls in `init()` for all 6 commands.

**Implementation notes — `update-sprint-metrics --json` (arch doc Section 6.1):**
```go
if isJSON(cmd) {
    metrics, err := ops.UpdateSprintMetrics(projectRoot)
    if err != nil {
        return err // deferred guard handles JSON
    }
    warnings := ops.CheckSuspiciousRates(metrics)
    return jsonout.WriteResult(os.Stdout, metrics, warnings, nil)
}
```

**Implementation notes — `status --json` (arch doc Section 6.2):**
```go
if isJSON(cmd) {
    statePath := paths.New(projectRoot).StatePath()
    bb := db.For(statePath)
    state, err := bb.Read()
    if err != nil {
        return err // deferred guard handles JSON
    }
    pr, prErr := ops.LoadResolverForModels(projectRoot)
    var warnings []string
    if prErr != nil {
        warnings = append(warnings, fmt.Sprintf("failed to load pipeline resolver: %v", prErr))
    }
    data := commands.BuildStatusData(state, detailed, projectRoot, pr, warnings)
    return jsonout.WriteResult(os.Stdout, data, warnings, nil)
}
```
Note: `buildStatusData` is accessed as `commands.buildStatusData` is unexported — but the JSON branch is in `cmd/liza/cmd_system.go` (package `main`), not in `internal/commands/`. The function is in a different package. This means the JSON branch for `status` must either: (a) call `StatusCommand` and work with its string result, or (b) duplicate the state-reading logic since `buildStatusData` is unexported.

**Resolution:** `buildStatusData` is in `internal/commands/status.go` (package `commands`) and is unexported. CP2 exports it as `BuildStatusData` so `cmd_system.go` (package `main`) can call it directly with pre-loaded resolver and intercepted warnings.

**Implementation notes — `get --json` (arch doc Section 6.3):**
```go
if isJSON(cmd) {
    opts.Format = "json"
    resultStr, err := commands.InspectCommand(args, opts)
    if err != nil {
        return err // deferred guard handles JSON
    }
    var parsed any
    json.Unmarshal([]byte(resultStr), &parsed)
    return jsonout.WriteResult(os.Stdout, parsed, nil, nil)
}
```

**New imports:** `"encoding/json"`, `"errors"`, `"fmt"`, `"io"`, `"log"`, `"os"`, `"github.com/liza-mas/liza/internal/db"`, `"github.com/liza-mas/liza/internal/jsonout"`, `"github.com/liza-mas/liza/internal/models"`, `"github.com/liza-mas/liza/internal/ops"`, `"github.com/liza-mas/liza/internal/paths"`.

**Done when:** All 6 commands in `cmd_system.go` accept `--json` flag. `update-sprint-metrics --json` returns the full `models.SprintMetrics` as the typed result (all 11 fields with snake_case json keys), with `CheckSuspiciousRates` warnings in the envelope warnings field. `status --json` loads the resolver in the RunE, intercepts load errors as warnings, calls exported `commands.BuildStatusData(state, detailed, projectRoot, pr, warnings)`, and writes the envelope with warnings. `get --json` wraps `InspectCommand` result in envelope. `clear-stale-review-claims --json` returns `{"ok": true, "result": {"cleared": N}}`. `analyze` and `sprint-checkpoint` use standard pattern. `go build ./cmd/liza/` succeeds. Existing tests still pass.

**Scope:** `cmd/liza/cmd_system.go`.

**Depends on:** CP1 (json tags on result structs), CP2 (refactored and exported `BuildStatusData`).

---

## CP7: Wire `--json` into `cmd/liza/cmd_init.go` (version, validate) + add `SetWarnWriter`

**Intent:** Register `--json` flag and implement the JSON branch for `version` and `validate`. Add `SetWarnWriter` to `internal/commands/validate.go` to allow `validate --json` to redirect warnings to a buffer.

**Files:**
- `cmd/liza/cmd_init.go` (modify)
- `internal/commands/validate.go` (modify — add `SetWarnWriter`)

**Per-command mapping:**

| Command | Source | Result Type | Special Handling |
|---------|--------|-------------|-----------------|
| `version` | Inline (`Version`, `GitCommit`, `BuildDate` vars) | `map[string]string` | Uses `Run` (not `RunE`). No deferred error guard needed. See arch doc Section 6.4. |
| `validate` | `commands.ValidateCommand(statePath, skipSpecCheck)` | `map[string]bool{"valid": true}` | Redirect `warnWriter` via `SetWarnWriter`. See arch doc Section 6.5. |

**Implementation notes — `SetWarnWriter` (added to `internal/commands/validate.go`):**
```go
// SetWarnWriter sets the destination for non-fatal validation warnings.
func SetWarnWriter(w io.Writer) {
    warnWriter = w
}
```
This is a one-line function added to `internal/commands/validate.go` (which already declares `var warnWriter io.Writer = os.Stderr` at line 14).

**Implementation notes — `version --json` (arch doc Section 6.4):**
The `versionCmd` uses `Run` (not `RunE`). It stays as `Run`. The JSON branch writes inline:
```go
Run: func(cmd *cobra.Command, args []string) {
    if isJSON(cmd) {
        result := map[string]string{
            "version": Version,
            "commit":  GitCommit,
            "built":   BuildDate,
        }
        jsonout.WriteResult(os.Stdout, result, nil, nil)
        return
    }
    // existing behavior unchanged
}
```

**Implementation notes — `validate --json` (arch doc Section 6.5):**
```go
RunE: func(cmd *cobra.Command, args []string) (retErr error) {
    // ... existing statePath resolution ...
    if isJSON(cmd) {
        log.SetOutput(io.Discard)
        defer log.SetOutput(os.Stderr)
        defer func() {
            if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
                _, _ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
                retErr = jsonout.ErrAlreadyWritten
            }
        }()

        var warnBuf bytes.Buffer
        commands.SetWarnWriter(&warnBuf)
        defer commands.SetWarnWriter(os.Stderr)

        err := commands.ValidateCommand(statePath, skipSpecCheck)
        var warnings []string
        if warnBuf.Len() > 0 {
            for _, line := range strings.Split(strings.TrimSpace(warnBuf.String()), "\n") {
                if line != "" {
                    warnings = append(warnings, line)
                }
            }
        }
        if err != nil {
            return err // deferred guard classifies as validation error
        }
        return jsonout.WriteResult(os.Stdout, map[string]bool{"valid": true}, warnings, nil)
    }
    // ... existing non-JSON path unchanged ...
}
```

**Flag registration:** Add `addJSONFlag(versionCmd)` and `addJSONFlag(validateCmd)` in the relevant `init()` function in `cmd_init.go`.

**New imports for cmd_init.go:** `"bytes"`, `"errors"`, `"io"`, `"log"`, `"os"`, `"strings"`, `"github.com/liza-mas/liza/internal/jsonout"`.

**Done when:** `version --json` returns `{"ok": true, "result": {"version": "...", "commit": "...", "built": "..."}}`. `validate --json` on valid state returns `{"ok": true, "result": {"valid": true}}` with warnings captured from `warnWriter`. `validate --json` on invalid state returns error envelope with `validation` code. `SetWarnWriter` function exists in `internal/commands/validate.go`. `go build ./cmd/liza/` succeeds. `go test ./internal/commands/...` passes (existing validate tests still green).

**Scope:** `cmd/liza/cmd_init.go`, `internal/commands/validate.go`.

**Depends on:** None (version and validate don't use ops result structs).

---

## CP8: Integration tests for `--json` wiring

**Intent:** Verify end-to-end `--json` behavior across command categories: typed results, error classification, warning capture, log suppression, and void-success commands.

**Files:**
- `cmd/liza/json_wiring_test.go` (new)
- `cmd/liza/rootcmd_test_helpers_test.go` (modify — add `"json"` to `resetRootCmdForTest` flag reset list)

**Test cases (per arch doc Section 9.3):**

| Test Function | What It Validates |
|---------------|-------------------|
| `TestJSON_ClaimTask_Success` | `claim-task T1 coder-1 --json` produces envelope with `ClaimResult` fields (task_id, agent_id, etc. with snake_case keys) |
| `TestJSON_ClaimTask_Error` | `claim-task nonexistent coder-1 --json` produces error envelope with `not_found` code |
| `TestJSON_Status_WithWarnings` | `status --json` with broken/missing pipeline config produces envelope with `warnings` array containing resolver-load warning message |
| `TestJSON_Status_NoWarnings` | `status --json` with valid config produces envelope with no `warnings` field |
| `TestJSON_UpdateSprintMetrics_TypedPayload` | `update-sprint-metrics --json` produces envelope where `result` contains all 11 `SprintMetrics` fields with snake_case JSON keys (tasks_done, tasks_in_progress, tasks_blocked, iterations_total, review_cycles_total, review_verdict_approvals, review_verdict_rejections, review_verdict_count, review_verdict_approval_rate_percent, task_submitted_for_review_count, task_outcome_approval_rate_percent) |
| `TestJSON_UpdateSprintMetrics_WithWarnings` | `update-sprint-metrics --json` with >95% approval rate produces warnings in envelope |
| `TestJSON_Version` | `version --json` produces envelope with version/commit/built string fields |
| `TestJSON_Validate_Valid` | `validate --json` on valid state produces `{"ok": true, "result": {"valid": true}}` |
| `TestJSON_Validate_Invalid` | `validate --json` on invalid state (e.g., missing required fields) produces error envelope with `validation` code |
| `TestJSON_RBACError` | Command with RBAC failure under `--json` produces error envelope (deferred guard catches pre-ops errors). E.g., `claim-task T1 orchestrator-1 --json` where orchestrator role is not allowed to claim. |
| `TestJSON_GetWrapsExisting` | `get tasks --json` wraps result in envelope (distinct from `get tasks --format json` which returns raw JSON) |
| `TestJSON_VoidSuccess` | `write-checkpoint --json` success produces `{"ok": true, "result": null}` |
| `TestJSON_Validate_WithWarnings` | `validate --json` on valid state with expired agent lease produces warnings array from redirected `warnWriter` |
| `TestJSON_LogSuppression` | Command with ops-level `log.Printf` produces no stderr output when `--json` is set. Capture stderr via `rootCmd.SetErr(&buf)` and verify empty. |

**Test infrastructure:**
- Reuse `setupMutationTestProject`, `executeRootCommand`, `readState`, `mustFindTask` helpers from `mutation_wiring_test.go`.
- Capture stdout: modify `executeRootCommand` usage or create a variant `executeRootCommandWithOutput` that captures stdout into a `bytes.Buffer` (set `rootCmd.SetOut(&buf)`) and returns `(string, error)`. Alternatively, redirect `os.Stdout` to a pipe.
- Parse JSON output: unmarshal stdout into `map[string]any` and assert on `ok`, `result`, `error`, `warnings` fields.

**`rootcmd_test_helpers_test.go` change:** Add `"json"` to the flag reset list in `resetRootCmdForTest` (around line 100-105) to prevent `--json` flag's `Changed` state from leaking between tests:
```go
resetFlagIfPresent(child, "json")
```

**Done when:** All 14 test cases in `json_wiring_test.go` pass. `TestJSON_UpdateSprintMetrics_TypedPayload` asserts all 11 `SprintMetrics` fields are present with snake_case JSON keys. `TestJSON_Status_WithWarnings` asserts resolver failure produces `warnings` array. `TestJSON_Validate_WithWarnings` asserts `warnWriter` output captured in warnings. `TestJSON_RBACError` asserts pre-ops RBAC error produces JSON error envelope. `TestJSON_LogSuppression` asserts stderr is empty when `--json` is set. `TestJSON_VoidSuccess` asserts `result` is `null`. `rootcmd_test_helpers_test.go` resets `--json` flag between tests. `go test ./cmd/liza/...` passes.

**Scope:** `cmd/liza/json_wiring_test.go` (new), `cmd/liza/rootcmd_test_helpers_test.go` (modify — add "json" to reset list).

**Depends on:** CP3, CP4, CP5, CP6, CP7 (all wiring must be in place for integration tests).

---

## Dependency Graph

```
CP1 (json tags on result structs)     CP2 (buildStatusData refactor)     CP7 (version + validate + SetWarnWriter)
   │                                     │                                  │
   ├── CP3 (cmd_task.go, 11 cmds)        │                                  │
   ├── CP4 (cmd_review.go, 6 cmds)       │                                  │
   ├── CP5 (cmd_worktree.go, 3 cmds)     │                                  │
   └── CP6 (cmd_system.go, 6 cmds) ──────┘                                  │
                                                                             │
       CP3 ── CP4 ── CP5 ── CP6 ── CP7 ─────────────────────────────────────┘
                         │                                                   │
                         └───────────────── CP8 (integration tests) ─────────┘
```

**Parallelism:** CP1, CP2, CP7 have no dependencies and can run in parallel. CP3, CP4, CP5 depend only on CP1 and can run in parallel after CP1. CP6 depends on CP1 + CP2. CP8 depends on all wiring tasks (CP3-CP7).

**Maximum parallelism at each stage:**
1. Stage 1: CP1, CP2, CP7 (3 parallel)
2. Stage 2: CP3, CP4, CP5, CP6 (4 parallel — CP6 starts when both CP1 and CP2 are done)
3. Stage 3: CP8 (1 — waits for all wiring)

---

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | All 28 included commands accept `--json` flag | Arch doc Section 3.4, Scope 1 | CP3, CP4, CP5, CP6, CP7 | Covered |
| 2 | When `--json` is set, stdout contains exactly one JSON envelope | Arch doc Section 3.1, 3.5 | CP3, CP4, CP5, CP6, CP7 | Covered |
| 3 | When `--json` is set, stderr is silent (log suppression) | Arch doc Section 6.7 | CP3, CP4, CP5, CP6, CP7 | Covered |
| 4 | Exit code matches `ok` field (0 for success, 1 for error) | Arch doc Section 8 | CP3, CP4, CP5, CP6, CP7 | Covered |
| 5 | Deferred error guard catches pre-ops errors (RBAC, projectRoot) | Arch doc Section 3.5 | CP3, CP4, CP5, CP6, CP7 | Covered |
| 6 | `update-sprint-metrics --json` returns typed `SprintMetrics` (all 11 fields with snake_case json tags) | Arch doc Section 6.1 | CP6, CP1 | Covered |
| 7 | `update-sprint-metrics --json` includes `CheckSuspiciousRates` warnings | Arch doc Section 6.1 | CP6 | Covered |
| 8 | `status --json` captures resolver-load warnings via refactored `BuildStatusData` | Arch doc Section 6.2 | CP6, CP2 | Covered |
| 9 | `buildStatusData` refactored: caller loads resolver, passes pr + prWarnings | Arch doc Section 6.2 | CP2 | Covered |
| 10 | `validate --json` captures `warnWriter` output via `SetWarnWriter` redirect | Arch doc Section 6.5, 6.7 | CP7 | Covered |
| 11 | `SetWarnWriter` function added to `internal/commands/validate.go` | Arch doc Section 6.5 | CP7 | Covered |
| 12 | Void-success commands return `{"ok": true, "result": null}` | Arch doc Section 5.4 | CP3 | Covered |
| 13 | `get --json` wraps `InspectCommand` result in envelope | Arch doc Section 6.3 | CP6 | Covered |
| 14 | `version --json` returns version/commit/built in envelope | Arch doc Section 6.4 | CP7 | Covered |
| 15 | `clear-stale-review-claims --json` returns `{"cleared": N}` | Arch doc Section 6.6 | CP6 | Covered |
| 16 | All ops result structs have `json:"snake_case"` tags | Arch doc Section 5.5 | CP1 | Covered |
| 17 | `SprintMetrics` has `json:"..."` tags matching yaml names | Arch doc Section 5.5, 6.1 | CP1 | Covered |
| 18 | Integration tests: update-sprint-metrics payload content | Arch doc Section 9.3 | CP8 | Covered |
| 19 | Integration tests: status warning path | Arch doc Section 9.3 | CP8 | Covered |
| 20 | Integration tests: validate warning capture | Arch doc Section 9.3 | CP8 | Covered |
| 21 | Integration tests: RBAC error produces JSON error envelope | Arch doc Section 9.3 | CP8 | Covered |
| 22 | Integration tests: log suppression | Arch doc Section 9.3 | CP8 | Covered |
| 23 | Integration tests: void-success commands | Arch doc Section 9.3 | CP8 | Covered |
| E2E | E2E test coverage for new behavior | Cross-cutting | CP8 | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: `--json` flag self-documents via `--help`. User-facing docs are updated by architecture-4a tasks (sibling scope). | N/A |
