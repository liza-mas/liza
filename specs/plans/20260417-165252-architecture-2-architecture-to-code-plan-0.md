# Code Plan — Pre-Commit Plan-Time Detection & Context Enrichment (architecture-2)

**Parent architecture task:** `architecture-2` (MERGED)
**Arch plan:** `specs/arch-plan/20260417-143319-architecture-2.md`
**Goal spec:** `specs/goals/20260417-precommit-bootstrap.md` (§Q2)
**This plan:** single coding task consolidating §7.1 of the arch plan.

## Scope Summary

One atomic coding task. The arch plan §7 concluded that helpers + role-context wiring + supervisor blocking share a tight test boundary (the architect prompt build) and splitting would duplicate fixture scaffolding for marginal parallelism benefit. The single child inherits the parent's `depends_on: ["architecture-1"]` via phase-gate inheritance (ADR-0048 / `computeInheritedDeps`) — by the time this child runs, `architecture-1-architecture-to-code-plan-0` (adds `Kind` to `internal/models/task.go`) has already merged, so `task.Kind` is available for compilation.

## MUST NOT modify

- `internal/models/task.go` (architecture-1 scope — `Kind` field is already landed by architecture-1's coding child; see arch plan §6.1 for the state-vs-branch split).
- `internal/ops/proceed.go`, `internal/ops/replan.go` (architecture-1 scope — dedup primitive and `Kind` propagation).
- `internal/prompts/templates/` (any file — sibling architecture-3 owns template authoring, including `base_prompt.tmpl:36` rewording and architect-prompt additions).
- `internal/prompts/role_context.go` outside the three struct field additions.
- `buildOrchestratorPromptContext` in `internal/agent/prompt.go` (orchestrator never calls `buildTaskRoleContextData`; per arch plan §3.4 no code change there).
- `specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md` (sibling architecture-4 scope).

---

## Task 1 — `internal/precommit/` package + `RoleContextData` three fields + architect-gated population + sentinel-guarded supervisor BLOCKED fallback + full test surface

### 1.1 `internal/precommit/precommit.go` (new file)

Create a new single-file package.

#### Package declaration and `Kind` constant

```go
// Package precommit provides repo-state detection helpers for the
// pre-commit bootstrap planning step: checking whether the integration
// branch already carries .pre-commit-config.yaml, and whether a
// bootstrap task is already in flight anywhere in the state.
package precommit

import (
    "bytes"
    "errors"
    "fmt"
    "os/exec"
    "strings"

    "github.com/liza-mas/liza/internal/gitenv"
    "github.com/liza-mas/liza/internal/models"
)

// Kind is the typed marker value (see architecture-1 §2.1) that the
// architect emits on output[].kind and that proceed.go treats as the
// authoritative dedup key. Exported so Go-side callers cannot drift on
// the literal. Go templates cannot import constants — the architect
// prompt template reads this value via RoleContextData.PreCommitKind
// (§3.1) rather than by package import.
const Kind = "bootstrap-precommit"

// ErrContextBuild is the sentinel returned (wrapped) by every error
// path in this package. Callers discriminate precommit-originated
// failures from other BuildPrompt failures via errors.Is — that
// discrimination lets the supervisor narrow its BLOCKED recovery to
// this package's error domain (§1.5) without misclassifying unrelated
// template/pipeline defects as task-local failures.
var ErrContextBuild = errors.New("precommit context build failed")
```

**Sentinel wrapping contract:** every error returned from this package MUST wrap `ErrContextBuild` via `%w` so `errors.Is(err, precommit.ErrContextBuild)` holds at every downstream layer. `BootstrapInFlight` is error-free by construction and does not participate.

#### `ConfigExistsOnIntegration`

```go
// ConfigExistsOnIntegration reports whether .pre-commit-config.yaml
// exists at the tip of the integration branch in the given project
// root. Reads committed state via git plumbing, not the working tree,
// so uncommitted human drift or in-progress worktree changes do not
// produce a false positive.
//
// Returns (true, nil) when the file is tracked on the branch.
// Returns (false, nil) when the branch exists and the file is not
// tracked on it. Returns (_, err) only for plumbing failures that
// prevent a reliable answer — empty inputs, invalid branch ref, or
// unexpected non-zero exit. Every error wraps ErrContextBuild.
func ConfigExistsOnIntegration(projectRoot, integrationBranch string) (bool, error) {
    if projectRoot == "" {
        return false, fmt.Errorf("precommit: projectRoot is empty: %w", ErrContextBuild)
    }
    if integrationBranch == "" {
        return false, fmt.Errorf("precommit: integrationBranch is empty: %w", ErrContextBuild)
    }

    // Step 1 — verify the ref exists. Isolates "branch invalid" as a
    // hard configuration error from "path absent on existing branch".
    verify := gitenv.Command("rev-parse", "--verify", "--quiet",
        integrationBranch+"^{commit}")
    verify.Dir = projectRoot
    if err := verify.Run(); err != nil {
        var exitErr *exec.ExitError
        if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
            return false, fmt.Errorf(
                "precommit: integration branch %q not found in %s: %w",
                integrationBranch, projectRoot, ErrContextBuild)
        }
        return false, fmt.Errorf("precommit: rev-parse --verify %q: %v: %w",
            integrationBranch, err, ErrContextBuild)
    }

    // Step 2 — read the tree entry. Exit 0 with empty stdout means
    // "branch has no such path"; non-empty means tracked.
    lsTree := gitenv.Command("ls-tree", integrationBranch, "--",
        ".pre-commit-config.yaml")
    lsTree.Dir = projectRoot
    var stdout, stderr bytes.Buffer
    lsTree.Stdout = &stdout
    lsTree.Stderr = &stderr
    if err := lsTree.Run(); err != nil {
        return false, fmt.Errorf(
            "precommit: ls-tree %q -- .pre-commit-config.yaml: %v (stderr: %s): %w",
            integrationBranch, err, strings.TrimSpace(stderr.String()), ErrContextBuild)
    }
    return stdout.Len() > 0, nil
}
```

**Contract (arch plan §2.3):**

| Aspect | Spec |
|--------|------|
| Path checked | Literal `.pre-commit-config.yaml` at branch root. Not configurable. |
| Empty-input guards | Empty `projectRoot` OR empty `integrationBranch` → descriptive error wrapping `ErrContextBuild`. |
| Ref-verify | `git rev-parse --verify --quiet <branch>^{commit}` via `gitenv.Command`, `cmd.Dir = projectRoot`. Exit 0 → proceed. Exit 1 → wrapped "integration branch not found" error. Any other exit → wrap via `%v` + `ErrContextBuild` via `%w`. |
| Path-check | `git ls-tree <branch> -- .pre-commit-config.yaml` via `gitenv.Command`, `cmd.Dir = projectRoot`. Success + empty stdout → `(false, nil)`. Success + non-empty stdout → `(true, nil)`. Non-zero exit → wrapped error including stderr AND `ErrContextBuild`. |
| Plumbing wrapper | `gitenv.Command` for both calls (locale-stable stderr). No filesystem fallback — deliberate divergence from `checkSpecFileExists` (`internal/statevalidate/validate.go:92`) because we must ignore working-tree drift (goal spec §Q2). |
| Logging | None inside the helper; diagnostics belong to the caller. |
| Every error path | Wraps `ErrContextBuild` via `%w` so `errors.Is` at every downstream layer is true. |

#### `BootstrapInFlight`

```go
// BootstrapInFlight reports whether any task in the given state has
// Kind == precommit.Kind and a non-terminal status. "Non-terminal"
// means Task.Status.IsTerminal() returns false — MERGED, ABANDONED,
// SUPERSEDED are terminal; every other status (including BLOCKED,
// DRAFT_*, IMPLEMENTING_*, IN_REVIEW, INTEGRATION_*, etc.) is in
// flight. A blocked bootstrap will eventually merge once rescoped,
// so it genuinely is in flight (goal spec §Q2 "Rescope invariant").
//
// Repo-wide: scans state.Tasks with no goal/sprint filter; cross-goal
// parallelism is covered by construction. Returns false if state is
// nil.
func BootstrapInFlight(state *models.State) bool {
    if state == nil {
        return false
    }
    for i := range state.Tasks {
        t := &state.Tasks[i]
        if t.Kind == "" {
            continue
        }
        if t.Kind != Kind {
            continue
        }
        if t.Status.IsTerminal() {
            continue
        }
        return true
    }
    return false
}
```

**Contract (arch plan §2.4):**

| Aspect | Spec |
|--------|------|
| Input | `*models.State`; `nil` → `false` (programmer-error guard, not a runtime condition; no panic, no error). |
| Scan cost | O(n) over `state.Tasks`; no new IO — the slice is already in memory. |
| Empty-`Kind` skip | Tasks with `Kind == ""` are not candidates (same invariant as `proceed.go`). |
| Terminal skip | `task.Status.IsTerminal()` — true means MERGED / ABANDONED / SUPERSEDED; skipped. |
| Early exit | Loop returns on first non-terminal match. |
| Error semantics | None — helper returns only `bool`. |

### 1.2 `internal/prompts/role_context.go` — three new fields

Add three fields to `RoleContextData` in the existing "Architecture-specific" group, immediately after `ParentTaskContexts` (role_context.go:66):

```go
    // Architecture-specific (populated for architect role)
    ParentTaskContexts         []ParentTaskContext
    PreCommitConfigExists      bool
    PreCommitBootstrapInFlight bool
    PreCommitKind              string // canonical marker string, mirrors precommit.Kind
```

**Contract (arch plan §3.1):**

| Aspect | Spec |
|--------|------|
| Field names | `PreCommitConfigExists`, `PreCommitBootstrapInFlight`, `PreCommitKind` (domain-prefixed). |
| Types | `bool`, `bool`, `string`. |
| `PreCommitKind` purpose | Bridges the Go-constant-to-template gap; architect prompt cannot `import` `precommit.Kind`, so the string must be surfaced through `RoleContextData`. Closes Concern 1 from review iteration 1. |
| Default | Zero values (`false`, `""`) for non-architect roles. |
| Placement | In the Architecture-specific group, right after `ParentTaskContexts`. |
| Rendering | Not owned by this plan. Template authoring is sibling architecture-3 scope. |

No changes elsewhere in `role_context.go`.

### 1.3 `internal/agent/prompt.go` — signature change and architect population block

#### Signature change

Change `buildTaskRoleContextData` at prompt.go:113 from:

```go
func buildTaskRoleContextData(task *models.Task, state *models.State, config SupervisorConfig, resolver *pipeline.Resolver) *prompts.RoleContextData
```

to:

```go
func buildTaskRoleContextData(task *models.Task, state *models.State, config SupervisorConfig, resolver *pipeline.Resolver) (*prompts.RoleContextData, error)
```

The single `return data` at end of function becomes `return data, nil`.

#### Architect-only population block

Add immediately after the existing "Architect-specific: parent task contexts" block (prompt.go:211-225). Because that block is already gated on `config.Role == roles.Architect`, the cleanest diff is to place the new block inside the same `if config.Role == roles.Architect { ... }` guard (extending it) OR as a new sibling `if` — coder chooses. Target shape (new sibling `if` shown for clarity):

```go
    // Architect-specific: pre-commit bootstrap context
    if config.Role == roles.Architect {
        exists, err := precommit.ConfigExistsOnIntegration(config.ProjectRoot, state.Config.IntegrationBranch)
        if err != nil {
            return nil, fmt.Errorf("precommit config check: %w", err)
        }
        data.PreCommitConfigExists = exists
        data.PreCommitBootstrapInFlight = precommit.BootstrapInFlight(state)
        data.PreCommitKind = precommit.Kind
    }
```

**Contract (arch plan §3.2):**

| Aspect | Spec |
|--------|------|
| Role gate | `config.Role == roles.Architect` (canonical constant from `internal/roles/roles.go:27`). Non-architect roles skip both helpers; all three new fields remain at zero value. |
| `ConfigExistsOnIntegration` inputs | `config.ProjectRoot`, `state.Config.IntegrationBranch` (same field already consumed at prompt.go:172 for the coder branch). |
| `BootstrapInFlight` input | `state` (the `*models.State` already in scope). |
| `PreCommitKind` assignment | `data.PreCommitKind = precommit.Kind` — the canonical string source. |
| Error wrap | `fmt.Errorf("precommit config check: %w", err)`. The `%w` preserves the `ErrContextBuild` sentinel through the wrap chain so `errors.Is(err, precommit.ErrContextBuild)` stays true at the supervisor. |
| On error | Return `(nil, wrappedErr)` — the function abandons the build; callers short-circuit. |
| Imports to add | `github.com/liza-mas/liza/internal/precommit`. (`fmt` already imported.) |

#### `buildPromptWithContext` error guard

Replace prompt.go:48 (current: `data := buildTaskRoleContextData(task, state, config, resolver)`) with:

```go
    data, err := buildTaskRoleContextData(task, state, config, resolver)
    if err != nil {
        return "", err
    }
```

Only a single two-line error guard added between the call and the subsequent `prompts.BuildRoleContext` call. No additional wrap at this layer — the helper already has `"precommit config check: "` prefix, and the supervisor re-wraps at its layer; double-wrapping adds noise without diagnostic value.

### 1.4 Mechanical rewrite of `buildTaskRoleContextData` call sites in `internal/agent/prompt_test.go`

Each existing call site of the form:

```go
data := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
```

becomes (happy-path tests):

```go
data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
if err != nil {
    t.Fatalf("buildTaskRoleContextData: %v", err)
}
```

And (for the second-call re-assignment pattern used in two tests):

```go
data, err = buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
if err != nil {
    t.Fatalf("buildTaskRoleContextData (second call): %v", err)
}
```

All existing call sites in `internal/agent/prompt_test.go` must be updated to absorb the new `(data, err)` return. Note the task description states 20 call sites; the coder verifies the actual count via grep at implementation time and updates every one — the exact count is a mechanical observation, not a test-coverage claim. **This rewrite is itemized mechanical work, not new test coverage**; the new tests are §1.7 below.

### 1.5 `internal/agent/supervisor.go` — sentinel-guarded BLOCKED fallback at L817-820

Current code (supervisor.go:817-820):

```go
prompt, err := strategy.BuildPrompt(stateBefore, config, taskID)
if err != nil {
    return fmt.Errorf("failed to build prompt: %w", err)
}
```

Replace with:

```go
prompt, err := strategy.BuildPrompt(stateBefore, config, taskID)
if err != nil {
    if claimedTaskID != "" && errors.Is(err, precommit.ErrContextBuild) {
        reason := fmt.Sprintf("prompt context build failed: %v", err)
        GetLogger().Warn("Task blocked due to prompt-context build failure",
            "agent_id", config.AgentID,
            "task_id", claimedTaskID,
            "error", err)
        blockTaskFromSupervisor(bb, config.ProjectRoot, claimedTaskID, config.AgentID, reason)
        spinTracker.reset(effectiveTask)
        continue
    }
    return fmt.Errorf("failed to build prompt: %w", err)
}
```

**Contract (arch plan §4.2):**

| Aspect | Spec |
|--------|------|
| Guard condition | **Both** `claimedTaskID != ""` AND `errors.Is(err, precommit.ErrContextBuild)` must hold. Either missing → fall through to the original wrapped-error return. |
| Blocked path (precommit domain) | Log `Warn` with `agent_id`, `task_id`, `error` → `blockTaskFromSupervisor(bb, config.ProjectRoot, claimedTaskID, config.AgentID, reason)` → `spinTracker.reset(effectiveTask)` → `continue`. |
| Non-blocked path (unrelated BuildPrompt failures: template-render, resolver `ContextSections`, pipeline errors, base-prompt render) | Sentinel does NOT match → returns the original wrapped `fmt.Errorf("failed to build prompt: %w", err)`. Existing crash-restart / system-stop machinery handles these as systemic defects. |
| `continue` vs `return nil` | `continue` mirrors the existing pre-exec BLOCKED patterns at L790-792 (spinning) and L917-919 (crash-restart loop). `return nil` would exit the supervisor session — the wrong recovery mode — so the supervisor must remain alive to observe BLOCKED on the next poll. |
| `reason` format | `"prompt context build failed: %v"` — full wrapped chain (e.g. `precommit: integration branch "main2" not found in /path: precommit context build failed`). Fires only for precommit-domain errors (enforced by the sentinel guard). |
| `spinTracker.reset` | Prevents a subsequent spinning detection from firing on the (now BLOCKED) task ID. Crash tracker not reset (agent never ran). |
| Reason prefix contract | Starts with `"prompt context build failed: precommit"` — the test at §1.7 / §5.10 asserts this prefix. |
| Import to add | `errors` (already imported via `github.com/liza-mas/liza/internal/errors` — use stdlib `errors` separately — or use `stdlib errors` aliased if collision). The coder resolves the import alias cleanly (stdlib `errors` vs. internal `errors` package); simplest: keep internal errors import alias-less, add stdlib `"errors"` and alias one of them if Go rejects the dup import. Also add `github.com/liza-mas/liza/internal/precommit`. |

**Path contrast (explicit, required by done_when facet 4):**

- *Precommit domain error (BuildPrompt failed because of `ConfigExistsOnIntegration` git-plumbing failure):* Sentinel guard matches → task becomes BLOCKED via `blockTaskFromSupervisor`, supervisor loop continues to next iteration.
- *Non-precommit domain error (template rendering, resolver `ContextSections`, pipeline wiring, base-prompt render):* Sentinel guard does not match → supervisor returns `fmt.Errorf("failed to build prompt: %w", err)`, escalating via the existing crash/systemic-failure pipeline. Task status is NOT mutated.

### 1.6 Internal-vs-stdlib `errors` import conflict

`internal/agent/supervisor.go` already imports `"github.com/liza-mas/liza/internal/errors"` (aliased or not — the coder verifies). Adding stdlib `"errors"` introduces a name collision. Resolve by aliasing the internal import (e.g. `liza_errors "github.com/liza-mas/liza/internal/errors"`) or by using the stdlib `errors.Is` through a local variable — simplest is alias. The coder picks whichever the package already uses elsewhere for consistency; if `internal/errors` is not yet referenced in supervisor.go outside this block, a direct `"errors"` stdlib import may suffice. Functional contract: `errors.Is(err, precommit.ErrContextBuild)` must compile and evaluate correctly.

### 1.7 Test surface — eleven named tests

All under internal/precommit/precommit_test.go (§1.7.1–§1.7.6), internal/agent/prompt_test.go (§1.7.7–§1.7.9), internal/agent/supervisor_test.go (§1.7.10–§1.7.11).

#### §1.7.1 `TestConfigExistsOnIntegration_Exists` (arch plan §5.1)

- Fixture: temporary git repo (`t.TempDir()` + `exec.Command("git", "init", "-b", "main")` + `-c user.email/name` + `git add .pre-commit-config.yaml` + `git commit -m ...`) with `.pre-commit-config.yaml` committed on branch `main`.
- Call `precommit.ConfigExistsOnIntegration(repoDir, "main")`.
- Assert `exists == true`, `err == nil`.

#### §1.7.2 `TestConfigExistsOnIntegration_Absent` (arch plan §5.2)

Two sub-cases in one test function (table-driven or sequenced):

- **Tracked-but-absent:** repo with a committed `README.md` but no `.pre-commit-config.yaml` tracked on `main`. Assert `(false, nil)`.
- **Uncommitted-working-tree variant:** repo with `.pre-commit-config.yaml` present in the working tree but NOT `git add`-ed / committed, on a commit that otherwise exists. Assert `(false, nil)` — confirms the helper ignores working-tree drift (arch plan §2.3 Divergence 1).

Neither variant may return an error: that would wrongly BLOCK the architect on the greenfield path.

#### §1.7.3 `TestConfigExistsOnIntegration_Error` (arch plan §5.3 — three required sub-cases)

Table-driven with at least three entries. Each asserts `err != nil` AND `errors.Is(err, precommit.ErrContextBuild) == true`.

- **Invalid branch:** repo scaffolding with no branch `nonexistent`. Call `ConfigExistsOnIntegration(repoDir, "nonexistent")`. Additionally assert the error message contains the canonical phrase `"integration branch"` and `"not found"` (the `rev-parse` error wrap path).
- **Empty `projectRoot`:** pass `""` for projectRoot. Additionally assert the message contains `"projectRoot is empty"`.
- **Empty `integrationBranch`:** pass `""` for branch. Additionally assert the message contains `"integrationBranch is empty"`.

Additionally the test MUST include a contrast-case reaffirming §1.7.2's contract: call the helper with a valid branch and an absent tracked path — assert `(false, nil)`, NOT `(_, err)`. This locks in the reviewer-flagged split between "invalid ref → error" and "absent path on valid branch → no error". (May be a separate sub-test function or a single-line inline check; the code-planner leaves the mechanic to the coder.)

#### §1.7.4 `TestBootstrapInFlight_Hit` (arch plan §5.4)

- Fixture: build a `models.State` with two tasks:
  - `t1`: `Kind: "bootstrap-precommit"`, `Status: TaskStatusDraftCode` (or any non-terminal status).
  - `t2`: `Kind: ""`, `Status: TaskStatusImplementingCode`.
- Call `precommit.BootstrapInFlight(&state)`.
- Assert `== true`.

#### §1.7.5 `TestBootstrapInFlight_BlockedCountsAsInFlight` (arch plan §5.5)

Two sub-cases:

- **Single BLOCKED:** one task with `Kind: "bootstrap-precommit"`, `Status: TaskStatusBlocked`, `BlockedReason` populated. Assert `BootstrapInFlight(&state) == true`. This is the explicit goal-spec requirement ("BLOCKED… genuinely is in flight").
- **Mixed SUPERSEDED/MERGED/BLOCKED variant:** three tasks, all with `Kind: "bootstrap-precommit"` — one `TaskStatusSuperseded`, one `TaskStatusMerged`, one `TaskStatusBlocked`. Assert `BootstrapInFlight(&state) == true` (only the BLOCKED one is non-terminal; the assertion specifically guards against terminal tasks leaking into the result).

#### §1.7.6 `TestBootstrapInFlight_Miss` (arch plan §5.6)

Three sub-cases:

- Empty `state.Tasks`.
- Tasks with non-matching `Kind` (e.g. `"something-else"`) on non-terminal statuses.
- Tasks with matching `Kind` but all-terminal statuses (`MERGED`, `ABANDONED`, `SUPERSEDED`).

Assert `BootstrapInFlight` returns `false` in all three.

Additionally: a dedicated sub-case or assertion for `BootstrapInFlight(nil) == false` (the nil-state guard per arch plan §2.4). May fold into the "Miss" function.

#### §1.7.7 `TestBuildTaskRoleContextData_PreCommitFields_Architect` (arch plan §5.7)

Two variants (table-driven or sequenced):

- **Bootstrap in flight, config absent:** real `t.TempDir()` git repo as `ProjectRoot`; `state.Config.IntegrationBranch` set to the branch with a valid commit but no `.pre-commit-config.yaml`. Plant one task in `state.Tasks` with `Kind: "bootstrap-precommit"` and a non-terminal status. `config.Role = roles.Architect`. Call `buildTaskRoleContextData`. Assert `err == nil`, `data.PreCommitConfigExists == false`, `data.PreCommitBootstrapInFlight == true`, `data.PreCommitKind == "bootstrap-precommit"` (equal to `precommit.Kind`).
- **Config present, no in-flight bootstrap:** same scaffolding but `git add .pre-commit-config.yaml && git commit`; drop the in-flight task. Assert `data.PreCommitConfigExists == true`, `data.PreCommitBootstrapInFlight == false`, `data.PreCommitKind == "bootstrap-precommit"`.

All three fields (including `PreCommitKind`) MUST be asserted in each variant — this is the explicit done_when facet (6) requirement.

#### §1.7.8 `TestBuildTaskRoleContextData_PreCommitFields_NonArchitect` (arch plan §5.8)

- Fixture: same state as §1.7.7 first variant (config absent, bootstrap in flight).
- `config.Role = roles.Coder` (run once); repeat for `roles.CodeReviewer`.
- Call `buildTaskRoleContextData`.
- Assert `err == nil`, AND all three fields remain at zero: `data.PreCommitConfigExists == false`, `data.PreCommitBootstrapInFlight == false`, `data.PreCommitKind == ""`.
- Gating contract: the helpers MUST NOT be called. Simplest implementation-level proof: configure `ProjectRoot` to a path with no `.git` directory — if the helper were called, `git rev-parse` would fail and the test would observe a non-nil error. The absence of error on a non-git path proves the gate held. (Alternative: inject a fake precommit seam — coder may prefer. Plan does not prescribe.)

#### §1.7.9 `TestBuildTaskRoleContextData_PreCommitFields_HelperError` (arch plan §5.9)

- Fixture: real `t.TempDir()` git repo in `ProjectRoot`; `state.Config.IntegrationBranch = "does-not-exist"` (no such branch); `config.Role = roles.Architect`.
- Call `buildTaskRoleContextData`.
- Assert `err != nil`.
- Assert `errors.Is(err, precommit.ErrContextBuild) == true` — proves the sentinel survives the `fmt.Errorf("precommit config check: %w", err)` wrap at the prompt layer.
- Assert `err.Error()` contains both `"precommit config check"` (outer wrap prefix) AND `"integration branch"` (inner `ConfigExistsOnIntegration` rev-parse wrap phrase).
- Assert `data == nil`.

#### §1.7.10 `TestSupervisor_BuildPromptFailure_BlocksTask` (arch plan §5.10)

Location: `internal/agent/supervisor_test.go` (or a companion `prompt_error_test.go` — coder picks per package layout).

- Fixture: a real blackboard with an architect task in `ARCHITECTING` status, `AssignedTo = "architect-1"`, `LeaseExpires` set. `state.Config.IntegrationBranch` set to a branch name that the helper will fail on (e.g. `"does-not-exist"` in a scaffolded real-git `ProjectRoot`). Stand up the minimal supervisor dependencies required to reach the `BuildPrompt` call — reuse existing test scaffolding helpers in `supervisor_test.go` if available.
- Drive one iteration of `RunSupervisor` (or the targeted call path that hits L817-820).
- Assert on the blackboard post-conditions:
  - `task.Status == TaskStatusBlocked`.
  - `task.BlockedReason != nil` and starts with `"prompt context build failed: precommit"`.
  - `task.AssignedTo == nil` and `task.LeaseExpires == nil` (cleared by `blockTaskFromSupervisor`).
  - Latest history entry has `Event == TaskEventBlocked`.
- Assert `executeAgent` was NOT invoked (inject a spy if the harness supports it, or assert that no agent-output file was written — whichever the existing supervisor_test scaffolding supports cleanly).
- Assert **the supervisor session did NOT exit** (iteration-1 Blocker-2 regression lock): drive a second loop iteration (e.g. via context cancellation terminating cleanly, or a "no actionable work" path) and observe it is reachable. Phrase positively: "a second loop iteration is reachable without restart".

#### §1.7.11 `TestSupervisor_BuildPromptFailure_NonPrecommit_DoesNotBlock` (arch plan §5.10a — regression lock)

Location: `internal/agent/supervisor_test.go`.

- Fixture: a real blackboard with a task (doer or reviewer — either suffices) in an executable status. Engineer a `BuildPrompt` failure that does NOT wrap `precommit.ErrContextBuild`. Two viable injection mechanisms (coder picks):
  - Inject a `pipeline.Resolver` that returns an error from `ContextSections` (rejects the role).
  - Use a template-render failure (e.g. pipeline configuration that points at a non-existent template file).
- Drive one iteration of `RunSupervisor`.
- Assert post-conditions:
  - Task status is NOT `TaskStatusBlocked` — it remains in the original status.
  - `task.BlockedReason == nil`.
  - The supervisor's outer-loop return is the original wrapped error whose message starts with `"failed to build prompt: "` (i.e. the pre-existing path). The `errors.Is(err, precommit.ErrContextBuild)` check on this return error MUST be false.
- This test locks in the iteration-2 Blocker-1 fix: uniform BLOCKED for every `BuildPrompt` error is NOT acceptable; only the precommit sentinel class triggers BLOCKED.

---

## Spec Compliance Matrix

Requirements extracted by re-reading task description, done_when (facets 1–8), and goal spec §Q2. The single output entry is "Task 1" (this plan's one atomic coding task).

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | `internal/precommit/precommit.go` new file with package doc, `Kind = "bootstrap-precommit"` constant, `ErrContextBuild` sentinel | done_when (1); arch plan §2.2 | Task 1 (§1.1) | Covered |
| 2 | Sentinel wrapping contract: every error path in package wraps `ErrContextBuild` via `%w` | done_when (1); arch plan §2.2 | Task 1 (§1.1, tests §1.7.3 asserting `errors.Is`) | Covered |
| 3 | `ConfigExistsOnIntegration(projectRoot, integrationBranch string) (bool, error)` — two-step plumbing: `git rev-parse --verify --quiet <branch>^{commit}` then `git ls-tree <branch> -- .pre-commit-config.yaml`, both via `gitenv.Command` with `cmd.Dir=projectRoot` | done_when (1); arch plan §2.3 | Task 1 (§1.1) | Covered |
| 4 | Empty `projectRoot` / empty `integrationBranch` guards return descriptive errors wrapping `ErrContextBuild` | done_when (1); arch plan §2.3 | Task 1 (§1.1, tests §1.7.3 two of three sub-cases) | Covered |
| 5 | Three-way error classification: invalid ref → error; plumbing failure → error; absent path on valid branch → (false, nil) | done_when (1, 5); arch plan §2.3 | Task 1 (§1.1, tests §1.7.2 and §1.7.3) | Covered |
| 6 | `BootstrapInFlight(state *models.State) bool` — O(n) scan, empty-`Kind` skip, `IsTerminal` skip, early exit, nil-state returns false | done_when (1); arch plan §2.4 | Task 1 (§1.1) | Covered |
| 7 | `RoleContextData` three-field addition (`PreCommitConfigExists bool`, `PreCommitBootstrapInFlight bool`, `PreCommitKind string`) in architect-specific region right after `ParentTaskContexts` | done_when (2); arch plan §3.1 | Task 1 (§1.2) | Covered |
| 8 | `buildTaskRoleContextData` signature change to `(*prompts.RoleContextData, error)` | done_when (3); arch plan §3.2 | Task 1 (§1.3) | Covered |
| 9 | Architect-gated population block (role gate via `roles.Architect`, `state.Config.IntegrationBranch` as branch, all three fields set including `data.PreCommitKind = precommit.Kind`, error wrap `fmt.Errorf("precommit config check: %w", err)`) | done_when (3); arch plan §3.2 | Task 1 (§1.3) | Covered |
| 10 | Two-line error guard in `buildPromptWithContext` before `prompts.BuildRoleContext` | done_when (3); arch plan §3.3 | Task 1 (§1.3) | Covered |
| 11 | Supervisor L817-820 modified: guard on **both** `claimedTaskID != ""` AND `errors.Is(err, precommit.ErrContextBuild)`; on match call `blockTaskFromSupervisor(...)` → `spinTracker.reset(effectiveTask)` → `continue`; otherwise keep existing wrapped-error return | done_when (4); arch plan §4.2 | Task 1 (§1.5) | Covered |
| 12 | Explicit contrast between precommit-domain BLOCKED path and non-precommit fall-through return | done_when (4) | Task 1 (§1.5 path contrast) | Covered |
| 13 | Six named tests in `internal/precommit/precommit_test.go` (Exists, Absent with uncommitted-working-tree variant, Error with three sub-cases, Hit, BlockedCountsAsInFlight with mixed SUPERSEDED/MERGED/BLOCKED variant, Miss) | done_when (5); arch plan §5.1–§5.6 | Task 1 (§1.7.1–§1.7.6) | Covered |
| 14 | Error sub-cases each assert `errors.Is(err, precommit.ErrContextBuild)`; "absent path on existing branch" asserts (false, nil) | done_when (5); arch plan §5.2, §5.3 | Task 1 (§1.7.2, §1.7.3) | Covered |
| 15 | Three named tests in `internal/agent/prompt_test.go`: Architect (three fields including `PreCommitKind`), NonArchitect (three zeros), HelperError (`"precommit config check"` prefix AND `errors.Is(err, precommit.ErrContextBuild) == true`) | done_when (6); arch plan §5.7–§5.9 | Task 1 (§1.7.7–§1.7.9) | Covered |
| 16 | Two named tests in `internal/agent/supervisor_test.go`: `BuildPromptFailure_BlocksTask` (BLOCKED state, reason prefix, cleared lease, no executeAgent, supervisor did not exit — second iteration reachable) and `BuildPromptFailure_NonPrecommit_DoesNotBlock` (non-precommit error leaves task unchanged, BlockedReason nil, original wrapped error surfaced) | done_when (7); arch plan §5.10, §5.10a | Task 1 (§1.7.10–§1.7.11) | Covered |
| 17 | Explicit statement that mechanical rewrite of existing `buildTaskRoleContextData` call sites in `prompt_test.go` is itemized but does NOT count as new test coverage | done_when (8) | Task 1 (§1.4 statement) | Covered |
| 18 | MUST NOT modify: `internal/models/task.go`, `internal/ops/proceed.go`, `internal/ops/replan.go`, any `internal/prompts/templates/` file, ADR-0036, `buildOrchestratorPromptContext`, `role_context.go` outside the three struct additions | task scope | Task 1 scope boundary (top of plan + §1.3 note) | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this change is internal plumbing (detection helpers, prompt-context data, supervisor error-routing). Observable end-to-end behavior lands only after the sibling template-authoring tasks (architecture-3) surface the fields in rendered prompts. Integration of the full precommit-bootstrap flow is exercised by the greenfield reproduction task `architecture-3-architecture-to-code-plan-3`, which is the goal-level e2e scope owner. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: no user-visible behavior surface changes in this task. The ADR amendment (architecture-4) and template-level documentation (architecture-3) are sibling tasks; goal spec and arch plan already document the design. | N/A |

No GAP rows.

---

## Output Entries

One entry — the single atomic coding task described above. No intra-`output[]` `depends_on` (sole entry). The goal-spec-level dependency on architecture-1's coding children is carried by this task's own `depends_on: ["architecture-1-architecture-to-code-plan-0", "architecture-1-architecture-to-code-plan-1"]` propagating through phase-gate inheritance to the coding child (ADR-0048).

### Task 1

- **desc:** Implement the `internal/precommit/` package (new `precommit.go` with package doc, `const Kind = "bootstrap-precommit"`, sentinel `var ErrContextBuild = errors.New("precommit context build failed")` wrapped via `%w` from every error path, `ConfigExistsOnIntegration(projectRoot, integrationBranch string) (bool, error)` using two-step `gitenv.Command` plumbing (`git rev-parse --verify --quiet <branch>^{commit}` then `git ls-tree <branch> -- .pre-commit-config.yaml`, both with `cmd.Dir=projectRoot`, empty-input guards, three-way error classification — invalid-ref error, plumbing error, absent-path (false,nil) — every error wrapping `ErrContextBuild`), and `BootstrapInFlight(state *models.State) bool` (O(n) scan, empty-`Kind` skip, `IsTerminal` skip, early exit, nil-state false)). Add three fields to `RoleContextData` in `internal/prompts/role_context.go` immediately after `ParentTaskContexts`: `PreCommitConfigExists bool`, `PreCommitBootstrapInFlight bool`, `PreCommitKind string`. Change `buildTaskRoleContextData` signature in `internal/agent/prompt.go` from `*prompts.RoleContextData` to `(*prompts.RoleContextData, error)`; add an architect-only (`config.Role == roles.Architect`) population block right after the existing ParentTaskContexts architect block that calls `precommit.ConfigExistsOnIntegration(config.ProjectRoot, state.Config.IntegrationBranch)` (wrap error with `fmt.Errorf("precommit config check: %w", err)` — `%w` preserves the sentinel), sets `data.PreCommitBootstrapInFlight = precommit.BootstrapInFlight(state)`, and sets `data.PreCommitKind = precommit.Kind`. Propagate the error through `buildPromptWithContext` via a single two-line error guard before `prompts.BuildRoleContext`. Mechanically rewrite every existing `buildTaskRoleContextData` call site in `internal/agent/prompt_test.go` to absorb the new `(data, err)` return (`t.Fatalf` on error in happy-path tests). In `internal/agent/supervisor.go` replace the L817-820 error branch with: on error, if `claimedTaskID != "" && errors.Is(err, precommit.ErrContextBuild)` log a Warn with `agent_id`/`task_id`/`error`, call `blockTaskFromSupervisor(bb, config.ProjectRoot, claimedTaskID, config.AgentID, fmt.Sprintf("prompt context build failed: %v", err))`, call `spinTracker.reset(effectiveTask)`, then `continue` (NOT `return nil`, NOT uniform blocking of every `BuildPrompt` error — the sentinel-gated guard is the scope contract); otherwise keep the existing wrapped-error return for non-precommit-domain failures (template/resolver/pipeline). Add all eleven tests: six unit tests in `internal/precommit/precommit_test.go` (Exists, Absent — including uncommitted-working-tree variant, Error — three sub-cases for invalid-branch / empty-projectRoot / empty-integrationBranch each asserting `errors.Is(err, precommit.ErrContextBuild)`, Hit, BlockedCountsAsInFlight — including mixed SUPERSEDED/MERGED/BLOCKED variant, Miss); three in `internal/agent/prompt_test.go` (`PreCommitFields_Architect` asserting all three fields including `PreCommitKind`, `PreCommitFields_NonArchitect` asserting all three zero, `PreCommitFields_HelperError` asserting `"precommit config check"` prefix AND `errors.Is(err, precommit.ErrContextBuild) == true`); two in `internal/agent/supervisor_test.go` — (a) `BuildPromptFailure_BlocksTask` asserting BLOCKED state, reason prefix `"prompt context build failed: precommit"`, `TaskEventBlocked` history, cleared `AssignedTo`/`LeaseExpires`, `executeAgent` not invoked, and a second loop iteration reachable (supervisor session did NOT exit), and (b) `BuildPromptFailure_NonPrecommit_DoesNotBlock` engineering a non-precommit `BuildPrompt` failure (resolver `ContextSections` error or template-render failure) and asserting task status is NOT `BLOCKED`, `BlockedReason` remains `nil`, and the supervisor returns the original wrapped `"failed to build prompt"` error through the non-BLOCKED path. MUST NOT modify `internal/models/task.go` (owned by architecture-1 — `Kind` already landed by its coding child; see arch plan §6.1), `internal/ops/proceed.go`, `internal/ops/replan.go`, any template under `internal/prompts/templates/`, `buildOrchestratorPromptContext`, or `specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`.
- **done_when:** (1) `internal/precommit/precommit.go` exists with package doc, `const Kind = "bootstrap-precommit"`, `var ErrContextBuild = errors.New("precommit context build failed")`, wrapping contract (every error path wraps `ErrContextBuild` via `%w`), and both function signatures matching §1.1 (rev-parse + ls-tree via `gitenv.Command` with `cmd.Dir=projectRoot`, empty-input guards, three-way error classification all wrapping `ErrContextBuild`); (2) `internal/prompts/role_context.go` has the three new fields `PreCommitConfigExists bool`, `PreCommitBootstrapInFlight bool`, `PreCommitKind string` in the architect-specific region right after `ParentTaskContexts`; (3) `buildTaskRoleContextData` has new signature `(*prompts.RoleContextData, error)` and the architect-gated population block sets all three fields including `data.PreCommitKind = precommit.Kind`, wraps the helper error with `fmt.Errorf("precommit config check: %w", err)`, and `buildPromptWithContext` has the two-line error guard; (4) `internal/agent/supervisor.go` L817-820 modified per §1.5 with the control-flow gated on **both** `claimedTaskID != ""` AND `errors.Is(err, precommit.ErrContextBuild)` — `blockTaskFromSupervisor(...)` → `spinTracker.reset(effectiveTask)` → `continue` inside the guard; unrelated `BuildPrompt` failures fall through to `return fmt.Errorf("failed to build prompt: %w", err)`; (5) six named passing tests in `internal/precommit/precommit_test.go` (`TestConfigExistsOnIntegration_Exists`, `TestConfigExistsOnIntegration_Absent` including uncommitted-working-tree variant, `TestConfigExistsOnIntegration_Error` split into at least three sub-cases each asserting `errors.Is(err, precommit.ErrContextBuild)` AND asserting "absent path on existing branch" returns `(false, nil)`, `TestBootstrapInFlight_Hit`, `TestBootstrapInFlight_BlockedCountsAsInFlight` with mixed SUPERSEDED/MERGED/BLOCKED variant, `TestBootstrapInFlight_Miss`); (6) three named passing tests in `internal/agent/prompt_test.go` (`TestBuildTaskRoleContextData_PreCommitFields_Architect` asserting all three fields including `PreCommitKind == "bootstrap-precommit"`, `TestBuildTaskRoleContextData_PreCommitFields_NonArchitect` asserting all three zero, `TestBuildTaskRoleContextData_PreCommitFields_HelperError` asserting `err.Error()` has `"precommit config check"` prefix AND `errors.Is(err, precommit.ErrContextBuild) == true`); (7) two named passing tests in `internal/agent/supervisor_test.go` — `TestSupervisor_BuildPromptFailure_BlocksTask` asserting `BlockedReason` prefix `"prompt context build failed: precommit"`, `TaskEventBlocked` history entry, cleared `AssignedTo`/`LeaseExpires`, `executeAgent` not invoked, AND supervisor did NOT exit (second iteration reachable) — AND `TestSupervisor_BuildPromptFailure_NonPrecommit_DoesNotBlock` asserting an engineered non-precommit `BuildPrompt` error leaves task status unchanged, `BlockedReason` nil, and surfaces the original `"failed to build prompt"` wrapped error; (8) the mechanical rewrite of every existing `buildTaskRoleContextData` call site in `prompt_test.go` is itemized (coder verifies count via grep) and not counted as new test coverage; pre-commit passes on touched files; `go test ./internal/precommit/... ./internal/agent/... ./internal/prompts/...` is green.
- **scope:** `internal/precommit/precommit.go` (new), `internal/precommit/precommit_test.go` (new), `internal/prompts/role_context.go` (add three bool/string fields to the existing struct only), `internal/agent/prompt.go` (modify `buildTaskRoleContextData` signature + architect-only population block of three fields + error guard in `buildPromptWithContext`), `internal/agent/prompt_test.go` (mechanical update of all existing call sites + three new tests), `internal/agent/supervisor.go` (modify L817-820 only — narrow sentinel-gated blocking + `spinTracker.reset` + `continue`; fall through to existing return for unrelated errors), `internal/agent/supervisor_test.go` (two new tests — blocks on precommit error, does NOT block on non-precommit error). MUST NOT modify `internal/models/task.go`, `internal/ops/proceed.go`, `internal/ops/replan.go`, `internal/prompts/templates/` (any file), `internal/prompts/role_context.go` outside of the three struct additions, `buildOrchestratorPromptContext`, or `specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`.
- **spec_ref:** `specs/goals/20260417-precommit-bootstrap.md#q2-idempotency--plan-time-dedup-execution-time`
- **plan_ref:** `specs/plans/20260417-165252-architecture-2-architecture-to-code-plan-0.md`
- **depends_on:** (none within this task's `output[]`; inter-task dependency on architecture-1's coding children is carried by the parent architecture-2 task's `depends_on`, propagating via phase-gate inheritance)

---

## Cross-reference audit

- §1.1 (precommit package) — referenced by §1.3 (imports `precommit`), §1.5 (imports `precommit.ErrContextBuild`), §1.7.1–§1.7.6 (tests).
- §1.2 (RoleContextData additions) — referenced by §1.3 (sets the three fields), §1.7.7–§1.7.9 (tests).
- §1.3 (`buildTaskRoleContextData` change) — referenced by §1.4 (call-site rewrite), §1.7.7–§1.7.9 (tests).
- §1.4 (mechanical rewrite) — declared as itemized, not new test coverage (done_when facet 8).
- §1.5 (supervisor change) — referenced by §1.7.10–§1.7.11 (tests). Path contrast explicit (BLOCKED vs. non-BLOCKED) per done_when facet 4.
- §1.6 (import collision) — implementation note owned by the coder, no cross-task dependency.
- §1.7 (tests) — each sub-section pins to a specific arch plan §5.x entry; coverage map in §5.11 of arch plan and Spec Compliance Matrix above align.

---

## Shared-file audit

Single coding task. All six modified files and two new files live entirely within this task's scope. No cross-task file sharing at this layer.

Inter-task file sharing with siblings:

- `internal/prompts/role_context.go` — this plan adds three architect-only fields. Sibling architecture-3 child tasks may render those fields in templates, but do not modify `role_context.go`. No conflict.
- `internal/agent/prompt.go` — only this task modifies. Sibling architecture-3 changes `base_prompt.tmpl` (template file), not `prompt.go`. No conflict.
- `internal/models/task.go` — owned by architecture-1 and already landed. This task consumes `task.Kind` (read-only) and does not modify the file.

No unresolved shared-file situation.
