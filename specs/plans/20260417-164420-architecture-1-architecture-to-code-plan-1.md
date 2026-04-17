# Code Plan — Kind-Based Dedup Primitive in `proceed.go`

**Parent task:** `architecture-1-architecture-to-code-plan-1`
**Arch plan:** `specs/arch-plan/20260417-141659-architecture-1.md` (§3, §5.2–§5.8, §5.10)
**Goal spec:** `specs/goals/20260417-precommit-bootstrap.md` (Q2)
**Depends on:** `architecture-1-architecture-to-code-plan-0` (MERGED) — adds `Kind` field to `OutputEntry` and `Task`.

---

## 1. Scope

All changes live in two files:

1. `internal/ops/proceed.go` — add two helpers (`collectNonTerminalByKind`, `resolveKindDedup`); modify the `per-subtask` branch of `proceedInner` (lines ~313–366); modify the `per-subtask` branch of `recoverCrashedTransition` (lines ~373–406); extend the `TaskEventTransitionExecuted` history `Extra` map with a `skipped_entries` key.
2. `internal/ops/proceed_test.go` — eight new test functions covering §5.2–§5.8 and §5.10.

**Out of scope (MUST NOT modify):** `internal/models/task.go` (owned by sibling CP0), `internal/ops/replan.go` (owned by sibling CP0), the `one-to-one` and `many-to-one` branches of `proceedInner` / `recoverCrashedTransition`, `ExecuteAvailableTransitions` phase logic, `computeInheritedDeps`.

This is a single cohesive intent: "add repo-wide `Kind`-based dedup to the per-subtask output path." The helpers, both call-sites (normal + crash-recovery), the history-extra surface, and the test coverage are one change — splitting them would leave the invariant half-implemented between subtasks and create merge conflicts on the same file.

---

## 2. Task Decomposition — Single Child Task (CP1)

| CP | Files | Intent |
|----|-------|--------|
| CP1 | `internal/ops/proceed.go`, `internal/ops/proceed_test.go` | Add `Kind`-based dedup primitive + 8 scenario tests. |

One task. Two helpers + two call-site modifications + history extension + eight tests form one intent: the dedup primitive is unusable without any one of these. The unit of behavior change is "calls to `proceedInner` with `Kind`-bearing entries now dedupe repo-wide, and crash recovery re-applies that decision." The behavior test is the eight scenarios co-located with the behavior.

Not split further because:
- All work modifies one file (`proceed.go`) + its test — splitting creates shared-file serialization with no parallelism win.
- Both call-sites (normal path in `proceedInner`, crash path in `recoverCrashedTransition`) consume both helpers; neither site is meaningful on its own.
- History extra is the single observability surface for the skip decision; omitting it leaves the feature un-auditable (violates §3.4 / §6.1).

---

## 3. Helper Functions (§3.2 of arch plan)

Added in `internal/ops/proceed.go`, placed immediately after `perSubtaskChildID` / `oneToOneChildID` / `manyToOneChildID` helpers (around line 40) so all deterministic-ID and dedup helpers cluster together.

### 3.1 `collectNonTerminalByKind`

```go
// collectNonTerminalByKind scans s.Tasks repo-wide (all goals, all sprints) and
// returns a map of non-empty Kind → canonical non-terminal task ID.
// "Canonical" = lexicographically smallest ID among candidates, for determinism
// across crash-recovery runs. Tasks with empty Kind are ignored. Tasks whose
// Status.IsTerminal() returns true (MERGED | ABANDONED | SUPERSEDED) are ignored.
func collectNonTerminalByKind(s *models.State) map[string]string {
    byKind := map[string][]string{}
    for i := range s.Tasks {
        t := &s.Tasks[i]
        if t.Kind == "" {
            continue
        }
        if t.Status.IsTerminal() {
            continue
        }
        byKind[t.Kind] = append(byKind[t.Kind], t.ID)
    }
    out := map[string]string{}
    for k, ids := range byKind {
        slices.Sort(ids)
        out[k] = ids[0]
    }
    return out
}
```

Invariants:
- **Repo-wide, no goal/sprint filter** (cross-goal parallelism — §4.3).
- **BLOCKED is non-terminal** (`IsTerminal` returns true only for MERGED/ABANDONED/SUPERSEDED per `task.go:146`). BLOCKED tasks stay in-flight.
- **Lexicographic tie-break** ensures the same canonical incumbent is picked every call.

### 3.2 `resolveKindDedup`

```go
// resolveKindDedup walks output entries in order and classifies each by Kind:
//   - empty Kind                    -> no effect.
//   - Kind in inFlight              -> skipped; remap[i] points at the in-flight task.
//   - Kind seen earlier in batch    -> skipped; remap[i] points at the first occurrence's
//                                      deterministic child ID.
// `taskID` and `transitionName` let the helper synthesize the first-occurrence
// sibling ID via `perSubtaskChildID`, so within-batch duplicates remap to the
// child that WILL be created this batch (not yet in s.Tasks when this runs).
func resolveKindDedup(
    entries []models.OutputEntry,
    inFlight map[string]string,
    taskID, transitionName string,
) (skip map[int]string, remap map[int]string) {
    skip = map[int]string{}
    remap = map[int]string{}
    emittedThisBatch := map[string]string{} // Kind -> first-occurrence sibling ID
    for i, e := range entries {
        if e.Kind == "" {
            continue
        }
        if existingID, hit := inFlight[e.Kind]; hit {
            skip[i] = fmt.Sprintf("kind %q already in flight on task %s", e.Kind, existingID)
            remap[i] = existingID
            continue
        }
        if firstID, dup := emittedThisBatch[e.Kind]; dup {
            skip[i] = fmt.Sprintf("kind %q emitted earlier in same output[] at sibling %s", e.Kind, firstID)
            remap[i] = firstID
            continue
        }
        emittedThisBatch[e.Kind] = perSubtaskChildID(taskID, transitionName, i)
    }
    return
}
```

Return semantics:
- `skip[i]` — a human-readable reason string; presence of key `i` means entry `i` was dedup'd.
- `remap[i]` — the task ID that takes entry `i`'s place for downstream dep resolution.
- Keys in `skip` and `remap` always match (every skipped entry is remapped).

---

## 4. `proceedInner` — Per-Subtask Branch (§3.1)

Modifications to the `per-subtask` case in `proceedInner` (lines ~314–322, 338–349, 357–364).

**Exact insertion point:** after the `validateOutputEntry` loop (line 322) and **before** the `task.TransitionsExecuted[transitionName] = true` assignment (line 335). Preserve the `one-to-one` branch untouched.

### 4.1 Compute dedup decision

Add after the validation loop within `case "per-subtask":`:

```go
case "per-subtask":
    if len(task.Output) == 0 {
        return fmt.Errorf("task %q has no output[] entries for per-subtask transition %q", taskID, transitionName)
    }
    for i, entry := range task.Output {
        if err := validateOutputEntry(entry, i, len(task.Output)); err != nil {
            return err
        }
    }
    // NEW: compute dedup decision. skipEntry[i] present => entry i is skipped.
    // remapSibling[i] is the incumbent/first-occurrence task ID for entry i.
    inFlightByKind := collectNonTerminalByKind(s)
    skipEntry, remapSibling := resolveKindDedup(task.Output, inFlightByKind, taskID, transitionName)
    _ = skipEntry // used below (history extra); silence unused-var until consumed
```

(The `_ = skipEntry` placeholder is cosmetic — the variable is consumed by the creation loop and history block below; no actual placeholder appears in the final code.)

### 4.2 Rewrite creation loop to honor remap + skip

Replace the existing per-subtask creation loop (current lines 345–349):

```go
case "per-subtask":
    for i, entry := range task.Output {
        if reID, dedupd := remapSibling[i]; dedupd {
            // Dep resolution still needs an ID; point at the incumbent so that
            // downstream siblings with depends_on: [i] resolve to the real
            // in-flight (or first-of-batch) task. DO NOT append a child for i.
            siblingIDs[i] = reID
            continue
        }
        child := buildChildTask(siblingIDs[i], taskID, entry, tDef.targetStatus, tDef.targetRolePair, tDef.taskType, siblingIDs, inheritedDeps, task.ArchRef, now)
        s.Tasks = append(s.Tasks, child)
        result.ChildTaskIDs = append(result.ChildTaskIDs, siblingIDs[i])
    }
```

Notes:
- `siblingIDs` was pre-computed over the full length of `task.Output` (line 338). For skipped entries, we overwrite `siblingIDs[i]` with the incumbent's ID so `buildChildTask` index-lookup for any later sibling whose `entry.DependsOn` contains `"i"` resolves correctly (§3.3).
- `result.ChildTaskIDs` only grows for created children — `ExecuteAvailableTransitions`' sprint-scope append (proceed.go:919) must not see the incumbent IDs in the child list, which would double-count pre-existing in-flight tasks into this sprint's scope.
- `transitions_executed[transitionName] = true` still fires (unchanged at line 335). The transition has executed; emitting zero children is a valid outcome when every entry dedup'd.

### 4.3 History extra — `skipped_entries` (§3.4)

Replace the existing history append (current lines 357–364):

```go
histExtra := map[string]any{
    "transition": transitionName,
    "children":   len(result.ChildTaskIDs),
}
if len(skipEntry) > 0 {
    entries := make([]map[string]any, 0, len(skipEntry))
    for i, reason := range skipEntry {
        entries = append(entries, map[string]any{
            "output_index": i,
            "kind":         task.Output[i].Kind,
            "reason":       reason,
            "remapped_to":  remapSibling[i],
        })
    }
    slices.SortFunc(entries, func(a, b map[string]any) int {
        return a["output_index"].(int) - b["output_index"].(int)
    })
    histExtra["skipped_entries"] = entries
}
task.History = append(task.History, models.TaskHistoryEntry{
    Time:  now,
    Event: models.TaskEventTransitionExecuted,
    Extra: histExtra,
})
```

Format (for each element of the `skipped_entries` slice):

| Key | Type | Value |
|-----|------|-------|
| `output_index` | `int` | index of the skipped entry in `task.Output` |
| `kind` | `string` | `task.Output[i].Kind` (non-empty by construction) |
| `reason` | `string` | human-readable diagnostic from `resolveKindDedup` |
| `remapped_to` | `string` | task ID that takes the entry's place |

Sort order: ascending by `output_index` for stable log diffs. Key omitted entirely when no entries are skipped (preserves current history shape for the common case, including the §5.3 miss test).

---

## 5. `recoverCrashedTransition` — Per-Subtask Branch (§3.5)

Modifications to the `per-subtask` case (current lines 373–406).

**Precedence rule (AUTHORITATIVE, SINGLE SOURCE OF TRUTH):** `skipped? → existing? → create?`.

1. **skipped?** — if `skipEntry[i]` is present, `continue` IMMEDIATELY. **Do NOT call `s.FindTask(siblingIDs[i])`.** **Do NOT call `patchInheritedDeps`.** For a skipped entry, `siblingIDs[i]` points at a foreign incumbent (possibly on a different goal); patching its `DependsOn` would corrupt a cross-goal task.
2. **existing?** — otherwise, `s.FindTask(siblingIDs[i])`; if present (our own child from a pre-crash run), call `patchInheritedDeps(existing, inheritedDeps)` and `continue`.
3. **create?** — otherwise, flag `i` as a missing child; creation happens in the existing downstream loop.

Replace the current per-subtask body (lines 374–406):

```go
case "per-subtask":
    // Pre-compute sibling IDs for DependsOn resolution.
    siblingIDs := make([]string, len(task.Output))
    for i := range task.Output {
        siblingIDs[i] = perSubtaskChildID(taskID, transitionName, i)
    }
    // Re-compute dedup decision against CURRENT repo-wide state.
    inFlightByKind := collectNonTerminalByKind(s)
    skipEntry, remapSibling := resolveKindDedup(task.Output, inFlightByKind, taskID, transitionName)
    for i, reID := range remapSibling {
        siblingIDs[i] = reID // point dep resolution at the incumbent
    }

    var missingChildren []int
    for i := range task.Output {
        // (1) SKIPPED — short-circuit BEFORE any FindTask / patchInheritedDeps.
        //     siblingIDs[i] points at a FOREIGN incumbent for skipped entries.
        //     Touching its DependsOn would corrupt a cross-goal task.
        if _, skipped := skipEntry[i]; skipped {
            continue
        }
        // (2) EXISTING — patch our OWN child's inherited deps.
        existing := s.FindTask(siblingIDs[i])
        if existing != nil {
            patchInheritedDeps(existing, inheritedDeps)
            continue
        }
        // (3) CREATE — flagged for creation below.
        missingChildren = append(missingChildren, i)
    }
    if len(missingChildren) == 0 {
        return fmt.Errorf("%w: %q on task %q", errTransitionAlreadyExecuted, transitionName, taskID)
    }
    for _, idx := range missingChildren {
        child := buildChildTask(siblingIDs[idx], taskID, task.Output[idx], tDef.targetStatus, tDef.targetRolePair, tDef.taskType, siblingIDs, inheritedDeps, task.ArchRef, now)
        s.Tasks = append(s.Tasks, child)
        result.ChildTaskIDs = append(result.ChildTaskIDs, siblingIDs[idx])
    }
    task.History = append(task.History, models.TaskHistoryEntry{
        Time:  now,
        Event: models.TaskEventTransitionCrashRecov,
        Extra: map[string]any{
            "transition":         transitionName,
            "recovered_children": len(missingChildren),
        },
    })
    return nil
```

Why step 1 MUST short-circuit BEFORE step 2:
- After `for i, reID := range remapSibling { siblingIDs[i] = reID }`, `siblingIDs[i]` for a skipped entry is the **foreign incumbent's** task ID.
- `s.FindTask(siblingIDs[i])` would therefore **succeed** and return that foreign task.
- `patchInheritedDeps(foreign, inheritedDeps)` would then append THIS recovering task's inherited deps onto a task belonging to a different parent / possibly different goal, corrupting the dependency graph.
- The explicit `if _, skipped := skipEntry[i]; skipped { continue }` ahead of `FindTask` is the single guard that prevents this. It is not a cosmetic ordering — it is a safety invariant.

Edge-case coverage is identical to §3.5 of the arch plan (three bullet points): a dedup decision that diverges between original `proceedInner` and `recoverCrashedTransition` runs resolves correctly through the same precedence chain. No additional logic needed here.

The `one-to-one` and `many-to-one` branches of `recoverCrashedTransition` are **not modified**.

---

## 6. Test Plan — `internal/ops/proceed_test.go`

Eight new test functions. All follow existing patterns (`testhelpers.CreateValidState` + `testhelpers.WriteInitialState`). Fixtures assemble minimal state with explicit `Kind` values; none of the tests rely on pipeline YAML loading beyond what the existing proceed-tests already exercise.

### 6.1 `TestProceed_PerSubtask_KindDedup_Hit` (§5.2)

**Fixture sketch:**
- Pre-existing non-terminal task `incumbent` with `Kind: "bootstrap-precommit"`, status any non-terminal value (e.g. `DRAFT_CODE`). Any `RolePair`.
- Architect-like trigger task with two `output[]` entries:
  - `output[0]`: `Kind: "bootstrap-precommit"`, full required fields.
  - `output[1]`: `Kind: ""`, `DependsOn: ["0"]`.

**Trigger:** `Proceed(projectRoot, triggerID, transitionName)` for a per-subtask transition (same wiring used by existing `TestProceed_CreatesChildTasks`).

**Assertions:**
1. `result.ChildTaskIDs` has length **1** — only the entry-1 child was created.
2. The sole created child has `DependsOn == [incumbent.ID, ...inheritedDeps]` — the `depends_on: ["0"]` reference remapped to the incumbent task ID.
3. State contains `incumbent` unchanged (same `DependsOn`, same `Status`) — the incumbent must not be mutated by the remap.
4. Trigger task's last history entry has `Event == TaskEventTransitionExecuted`, `Extra["children"] == 1`, and `Extra["skipped_entries"]` is a one-element slice with `output_index: 0`, `kind: "bootstrap-precommit"`, `remapped_to: incumbent.ID`, non-empty `reason`.
5. `trigger.TransitionsExecuted[transitionName] == true`.

### 6.2 `TestProceed_PerSubtask_KindDedup_Miss` (§5.3)

**Fixture sketch:**
- State contains no task with `Kind: "bootstrap-precommit"`, OR only tasks with that `Kind` at `MERGED` / `ABANDONED` / `SUPERSEDED` status (covers all three terminal values in sub-cases or one representative).
- Trigger task with `output[0].Kind: "bootstrap-precommit"`.

**Assertions:**
1. `result.ChildTaskIDs` has length equal to `len(trigger.Output)` — no skips.
2. The child for entry 0 has `Kind == "bootstrap-precommit"` persisted (relies on CP0's propagation via `buildChildTask`).
3. Trigger's history event has `Extra["children"] == len(trigger.Output)` and **no** `skipped_entries` key present in the map.

### 6.3 `TestProceed_PerSubtask_KindDedup_BlockedInFlight` (§5.4)

**Fixture sketch:**
- Pre-existing task `blocked` with `Kind: "bootstrap-precommit"`, `Status: BLOCKED`, `BlockedReason` populated.
- Trigger with `output[0].Kind: "bootstrap-precommit"`, `output[1]: depends_on: ["0"]`.

**Assertions:**
1. Entry 0 is skipped (BLOCKED is non-terminal); `result.ChildTaskIDs` length 1.
2. Child for entry 1 has `DependsOn` containing `blocked.ID`.
3. `blocked` task is unchanged (same status, same `DependsOn`).
4. `checkDependencies` (or equivalent dep-satisfaction predicate used in the codebase) on the created entry-1 child returns `false` against current state — BLOCKED is neither MERGED nor SUPERSEDED, so the coding cohort is held until rescope. Assertion form: call `models.ChildTaskDependencies(state, entry1ChildID)` (or equivalent) and assert not-satisfied, OR inspect `blocked.Status != MERGED && blocked.Status != SUPERSEDED` and assert the child's `DependsOn` contains a non-satisfied dep.

### 6.4 `TestProceed_PerSubtask_KindDedup_SupersededUnblocks` (§5.5)

**Fixture sketch (primary):**
- Pre-existing task `oldBoot` with `Kind: "bootstrap-precommit"`, `Status: SUPERSEDED`.
- Trigger with `output[0].Kind: "bootstrap-precommit"`.

**Assertions (primary):**
1. Entry 0 **is created** — SUPERSEDED is terminal → not in-flight.
2. `result.ChildTaskIDs` length equals `len(trigger.Output)`.
3. The created child for entry 0 has `Kind == "bootstrap-precommit"`.
4. No `skipped_entries` in the history event.

**Second sub-fixture in same test (table-driven):**
- State has two SUPERSEDED bootstraps + one non-terminal (`DRAFT_CODE`) bootstrap `live`.
- Trigger emits bootstrap.
- Assertions: entry 0 is **skipped** (the non-terminal `live` wins), `remapped_to == live.ID`.

### 6.5 `TestProceed_PerSubtask_KindDedup_DuplicateWithinBatch` (§5.6)

**Fixture sketch:**
- No in-flight bootstrap anywhere.
- Trigger output: `output[0].Kind = "bootstrap-precommit"`, `output[1].Kind = "bootstrap-precommit"`, `output[2].Kind = ""` with `DependsOn: ["0"]`.

**Assertions:**
1. Entry 0 is created; entry 1 is skipped; entry 2 is created.
2. `result.ChildTaskIDs` length 2.
3. Entry-2 child's `DependsOn` contains entry-0's deterministic child ID (`perSubtaskChildID(triggerID, transitionName, 0)`) — proving remap for index 0 resolved to the first-occurrence child, not the skipped entry 1.
4. Trigger's history `skipped_entries` has exactly one element with `output_index: 1`, `remapped_to: perSubtaskChildID(triggerID, transitionName, 0)`.

### 6.6 `TestProceed_PerSubtask_KindDedup_CrossGoal` (§5.7)

**Fixture sketch:**
- Goal A task `archA` (architect-like) that previously emitted a bootstrap child `A-bootstrap` with `Kind: "bootstrap-precommit"`, non-terminal status. `A-bootstrap` has no common parent with `archB`.
- Goal B task `archB` — the trigger — with `output[0].Kind: "bootstrap-precommit"`.

The two goals are simulated by distinct `SpecRef` strings / disjoint `ParentTasks` trees. No per-goal filter exists in `collectNonTerminalByKind`, so the scan is flat over `s.Tasks`.

**Assertions:**
1. Entry 0 from `archB` is **skipped**; `remapped_to: A-bootstrap.ID`.
2. `A-bootstrap.DependsOn` unchanged after the call — confirms no cross-goal dep mutation in the normal (non-crash) path.
3. History event on `archB` records the skip with `remapped_to: A-bootstrap.ID`.

### 6.7 `TestProceed_PerSubtask_KindDedup_CrashRecovery` (§5.8)

**Fixture sketch:**
- Pre-existing `foreignIncumbent` with `Kind: "bootstrap-precommit"`, non-terminal status, belonging to a **different goal** / with a distinct parent chain from the trigger.
- Trigger `archB` with three output entries:
  - `output[0].Kind: "bootstrap-precommit"` — originally skipped.
  - `output[1].Kind: ""` — originally created.
  - `output[2].Kind: ""` — originally NOT created (simulating the crash).
- `archB.TransitionsExecuted[transitionName] = true` (transition was marked executed pre-crash).
- `s.Tasks` contains the entry-1 child from the pre-crash run, but **not** the entry-2 child.
- Capture `foreignIncumbent.DependsOn` into `before` variable before calling.

**Trigger:** any path that invokes `recoverCrashedTransition` for this task (the existing `TestProceed_CrashRecovery_CreatesMissingChildren` pattern establishes the call path — re-use).

**Assertions:**
1. After the call, entry-2's child exists (was missing, recovered).
2. Entry-0 child does **NOT** exist — skip decision re-applied.
3. Entry-1 child still exists (was present pre-call).
4. **Crash-recovery safety invariant:** `foreignIncumbent.DependsOn` is **byte-equal** to `before`. `patchInheritedDeps` was NOT called on the foreign task. This is the primary assertion establishing that step-1 short-circuit fires before `s.FindTask(siblingIDs[0])` / `patchInheritedDeps`.
5. `archB` history has a `TaskEventTransitionCrashRecov` entry with `recovered_children: 1`.

This assertion (#4) is the most important of the whole test plan — it is the observable witness that `skipped? → existing? → create?` ordering is honored. A regression that swaps steps 1 and 2 would fail this test and nothing else.

### 6.8 `TestProceed_PerSubtask_NoKind_UnaffectedByDedup` (§5.10)

**Fixture sketch:**
- State contains an in-flight task `somethingElse` with `Kind: "bootstrap-precommit"`.
- Trigger output: all entries have `Kind == ""` (pre-existing per-subtask behavior; any number of entries, e.g. 3).

**Assertions:**
1. Zero entries are skipped; `result.ChildTaskIDs` length equals `len(trigger.Output)`.
2. History event has no `skipped_entries` key.
3. `somethingElse` is unchanged — the dedup scan collected it but no output entry matched, so no interaction occurred.
4. Confirms the dedup is strictly **opt-in per entry** via non-empty `Kind` — the presence of in-flight `Kind`-bearing tasks does not affect entries that declare no `Kind`.

---

## 7. Validation Commands

Run from `internal/ops/`:

```
go test ./internal/ops/ -run TestProceed_PerSubtask_KindDedup -v
go test ./internal/ops/ -run TestProceed_PerSubtask_NoKind_UnaffectedByDedup -v
go test ./internal/ops/ -v
go vet ./internal/ops/
```

All eight new tests must pass. The full `./internal/ops/` suite must pass with no regressions.

---

## 8. Dependency Graph

- **Upstream:** `architecture-1-architecture-to-code-plan-0` (MERGED) — provides `OutputEntry.Kind` and `Task.Kind`; this plan reads both. Without CP0, Go compilation fails.
- **Downstream:** none of the sibling code-planning tasks depend on this one. `architecture-3-architecture-to-code-plan-1` (architect prompt update) and `architecture-2-architecture-to-code-plan-0` (`internal/precommit/` package) are independent surfaces that compose with this dedup at runtime but do not share source files.
- **Shared files:** `internal/ops/proceed.go` is touched here only. `internal/ops/proceed_test.go` is touched here only.

No shared-file conflict audit needed beyond CP0 precedence (already declared via `depends_on`).

---

## 9. Spec Compliance Matrix

Requirements extracted from the task's `done_when`, the arch plan §3 and §5.2–§5.8 + §5.10, and the goal spec Q2 invariants. "Task(s)" uses the plan's single task label `CP1`.

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Helper `collectNonTerminalByKind(s) map[string]string`: repo-wide scan of `s.Tasks`; skip empty `Kind`; skip `task.Status.IsTerminal()`; return Kind → lexicographically smallest task ID | done_when (1); arch §3.2 | CP1 | Covered |
| 2 | Helper `resolveKindDedup(entries, inFlight, taskID, transitionName) (skip, remap)`: fixed four-arg signature; within-batch dedup included | done_when (1); arch §3.2 | CP1 | Covered |
| 3 | Within-batch first-occurrence semantics: duplicates remap to deterministic sibling ID of first occurrence via `perSubtaskChildID(taskID, transitionName, i)` | done_when (1); arch §3.2 | CP1 | Covered |
| 4 | Insertion point in `proceedInner` per-subtask branch: after `validateOutputEntry` loop, before `task.TransitionsExecuted[transitionName] = true` (~line 335) | done_when (2); arch §3.1 | CP1 | Covered |
| 5 | Modified per-subtask creation loop: honor `remapSibling[i]` by overwriting `siblingIDs[i]`; skip creation for dedup'd entries; append `ChildTaskIDs` only for created children | done_when (3); arch §3.1 | CP1 | Covered |
| 6 | Modified `recoverCrashedTransition` per-subtask branch with precedence `skipped? → existing? → create?` | done_when (4); arch §3.5 | CP1 | Covered |
| 7 | Step 1 (skipped) MUST `continue` BEFORE any `s.FindTask(siblingIDs[i])` or `patchInheritedDeps(existing, inheritedDeps)` call — safety invariant preventing cross-goal dep corruption | done_when (4); arch §3.5 | CP1 | Covered |
| 8 | History `Extra["skipped_entries"]` key on `TaskEventTransitionExecuted`: slice of `{output_index, kind, reason, remapped_to}` maps, sorted ascending by `output_index`, omitted when empty | done_when (5); arch §3.4 | CP1 | Covered |
| 9 | `TestProceed_PerSubtask_KindDedup_Hit` (§5.2 assertions) | done_when (6); arch §5.2 | CP1 | Covered |
| 10 | `TestProceed_PerSubtask_KindDedup_Miss` (§5.3 assertions) | done_when (6); arch §5.3 | CP1 | Covered |
| 11 | `TestProceed_PerSubtask_KindDedup_BlockedInFlight` (§5.4 assertions) | done_when (6); arch §5.4 | CP1 | Covered |
| 12 | `TestProceed_PerSubtask_KindDedup_SupersededUnblocks` (§5.5 assertions, including two-SUPERSEDED-plus-one-live sub-variant) | done_when (6); arch §5.5 | CP1 | Covered |
| 13 | `TestProceed_PerSubtask_KindDedup_DuplicateWithinBatch` (§5.6 assertions) | done_when (6); arch §5.6 | CP1 | Covered |
| 14 | `TestProceed_PerSubtask_KindDedup_CrossGoal` (§5.7 assertions — repo-wide scan semantics) | done_when (6); arch §5.7 | CP1 | Covered |
| 15 | `TestProceed_PerSubtask_KindDedup_CrashRecovery` (§5.8 assertions) including explicit assertion that `patchInheritedDeps` is NOT called on the foreign incumbent (foreign incumbent `DependsOn` unchanged after recovery) | done_when (6); arch §5.8 + done_when final clause | CP1 | Covered |
| 16 | `TestProceed_PerSubtask_NoKind_UnaffectedByDedup` (§5.10 assertions — dedup opt-in per entry) | done_when (6); arch §5.10 | CP1 | Covered |
| 17 | Do NOT modify `one-to-one` or `many-to-one` branches of `proceedInner` / `recoverCrashedTransition` | scope | CP1 | Covered |
| 18 | Do NOT modify `internal/models/task.go`, `internal/ops/replan.go`, `ExecuteAvailableTransitions` phase logic, `computeInheritedDeps` | scope | CP1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: the new behavior is an internal proceed-time primitive with no user-visible CLI or UI surface. Observability is via task history (`skipped_entries` extra), which is exercised by unit tests (§6.1, §6.5, §6.6). No user command behavior changes — dedup is transparent. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: ADR-0036 amendment and goal-spec Q2 already describe the dedup semantics and are owned by sibling `architecture-4-architecture-to-code-plan-0`. History-extra schema is not part of any user-facing doc — history `Extra` is a free-form map by design (`models.TaskHistoryEntry.Extra map[string]any`). | N/A |

---

## 10. CP1 — Verbatim Task Definition

The text below is what will be emitted via `liza set-task-output` for the single child task (CP1). Parity rule: this block is the authoritative source for the `desc`, `done_when`, `scope`, and `spec_ref` fields of `output[0]` in the submission payload.

### 10.1 `desc`

Add repo-wide Kind-based dedup primitive to the per-subtask branch of proceed.go. Introduce helpers `collectNonTerminalByKind(s *models.State) map[string]string` (repo-wide scan of s.Tasks; skip empty Kind; skip task.Status.IsTerminal(); return map Kind → lexicographically smallest task ID for determinism) and `resolveKindDedup(entries []models.OutputEntry, inFlight map[string]string, taskID, transitionName string) (skip map[int]string, remap map[int]string)` per §3.2 of the arch plan — within-batch duplicates resolve to the first occurrence via `perSubtaskChildID(taskID, transitionName, i)`. Modify `proceedInner` per-subtask branch: after the `validateOutputEntry` loop and before the `task.TransitionsExecuted[transitionName] = true` assignment (~line 335), compute skip/remap; in the child-creation loop, rewrite `siblingIDs[i]` to the incumbent ID for skipped entries and skip child creation (do not append to `s.Tasks` or `result.ChildTaskIDs`). Modify `recoverCrashedTransition` per-subtask branch with precedence **`skipped? → existing? → create?`** — step 1 (skipped) MUST short-circuit via `continue` BEFORE any `s.FindTask(siblingIDs[i])` or `patchInheritedDeps(existing, inheritedDeps)` call because `siblingIDs[i]` points at a foreign incumbent for skipped entries. Extend the `TaskEventTransitionExecuted` history `Extra` map with an optional `skipped_entries` key: slice of `{output_index, kind, reason, remapped_to}` maps sorted ascending by `output_index`; omit when empty. Do NOT modify the one-to-one or many-to-one branches, `ExecuteAvailableTransitions` phase logic, `computeInheritedDeps`, `internal/models/task.go`, or `internal/ops/replan.go`. Add eight test functions in `internal/ops/proceed_test.go`: TestProceed_PerSubtask_KindDedup_Hit, _Miss, _BlockedInFlight, _SupersededUnblocks, _DuplicateWithinBatch, _CrossGoal, _CrashRecovery, and TestProceed_PerSubtask_NoKind_UnaffectedByDedup — TDD inside this task. The crash-recovery test MUST assert that `patchInheritedDeps` is not called on the foreign incumbent (e.g., by asserting the incumbent's `DependsOn` is byte-equal before and after recovery).

### 10.2 `done_when`

1. Helper `collectNonTerminalByKind(s *models.State) map[string]string` present with exact body per §3.2: repo-wide scan, skip empty Kind, skip `Status.IsTerminal()`, lexicographic tie-break. 2. Helper `resolveKindDedup(entries, inFlight, taskID, transitionName) (skip, remap)` present with exact signature and within-batch semantics per §3.2; within-batch first occurrence synthesizes sibling ID via `perSubtaskChildID(taskID, transitionName, i)`. 3. `proceedInner` per-subtask branch calls both helpers after `validateOutputEntry` loop and before `transitions_executed` write; creation loop rewrites `siblingIDs[i]` via remap and skips appending for dedup'd entries; `transitions_executed[transitionName]` still set to true. 4. `recoverCrashedTransition` per-subtask branch re-runs `collectNonTerminalByKind` + `resolveKindDedup` and walks entries with precedence `skipped? → existing? → create?`; the skipped-check `continue`s BEFORE any `s.FindTask` or `patchInheritedDeps` call — verifiable by source inspection (step-1 guard appears above step-2 in the modified function body). 5. History `Extra` on `TaskEventTransitionExecuted` gains key `skipped_entries` when any entry was dedup'd: `[]map[string]any{{"output_index": int, "kind": string, "reason": string, "remapped_to": string}}` sorted ascending by `output_index`; key absent when no skips. 6. Eight new test functions pass in `internal/ops/proceed_test.go` with names TestProceed_PerSubtask_KindDedup_Hit, _Miss, _BlockedInFlight, _SupersededUnblocks, _DuplicateWithinBatch, _CrossGoal, _CrashRecovery, and TestProceed_PerSubtask_NoKind_UnaffectedByDedup; assertions match §5.2–§5.8 and §5.10 of the arch plan. TestProceed_PerSubtask_KindDedup_CrashRecovery MUST include an explicit assertion that the foreign incumbent task's `DependsOn` field is unchanged (byte-equal) before and after the recovery call. 7. `go test ./internal/ops/` passes with zero regressions. 8. `go vet ./internal/ops/` clean. 9. Files `internal/models/task.go`, `internal/ops/replan.go`, and the one-to-one / many-to-one branches of `proceedInner` and `recoverCrashedTransition` are unchanged (diff review).

### 10.3 `scope`

internal/ops/proceed.go (two new helpers + modifications to `proceedInner` per-subtask branch + modifications to `recoverCrashedTransition` per-subtask branch + `skipped_entries` history extra key), internal/ops/proceed_test.go (eight new test functions). MUST NOT modify internal/models/task.go, internal/ops/replan.go, the one-to-one or many-to-one branches of proceedInner / recoverCrashedTransition, ExecuteAvailableTransitions phase logic, or computeInheritedDeps.

### 10.4 `spec_ref`

specs/goals/20260417-precommit-bootstrap.md#q2-idempotency--plan-time-dedup-execution-time

### 10.5 `plan_ref`

specs/plans/20260417-164420-architecture-1-architecture-to-code-plan-1.md

### 10.6 `depends_on`

None within this batch (CP1 is the only output). The batch-level dependency on `architecture-1-architecture-to-code-plan-0` is declared at the parent task level (already MERGED) and is inherited automatically by phase-gate inheritance in `computeInheritedDeps`.

---

## 11. Summary

One coding task (**CP1**). Exactly one file of production code (`internal/ops/proceed.go`) and one file of tests (`internal/ops/proceed_test.go`). All work is a single intent: introduce repo-wide `Kind`-based dedup for per-subtask transitions, with crash-recovery parity and observable history.

No split. No parallelism win available (single file). No additional e2e or doc task (see matrix).
