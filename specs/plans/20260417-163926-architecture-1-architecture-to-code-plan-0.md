# Code Plan — Data-Model Kind Delta + Propagation

**Task:** architecture-1-architecture-to-code-plan-0
**Goal spec:** `specs/goals/20260417-precommit-bootstrap.md`
**Architecture plan:** `specs/arch-plan/20260417-141659-architecture-1.md`
**Scope anchor:** arch-plan §7.1 (data-model delta child)

Sibling `architecture-1-architecture-to-code-plan-1` owns arch-plan §7.2 (dedup helpers `collectNonTerminalByKind` / `resolveKindDedup`, `proceedInner` and `recoverCrashedTransition` modifications, history `skipped_entries`). This plan MUST NOT touch those.

---

## 1. Intent

Add a typed `Kind string` marker to `OutputEntry` and persisted `Task`, and propagate it along the two creation paths that carry author intent from an output entry to a child/replacement task: `buildChildTask` (per-subtask creation) and the `newTask` literal in `replan.go`. Cover with round-trip and propagation unit tests.

This is a **single observable behavior change**: after this task, an architect's `output[].kind` value survives into the corresponding persisted `Task.Kind`, and a replan preserves `Kind` onto the replacement. No dedup logic is introduced — the `Kind` field is behaviorally inert on its own.

---

## 2. Edits (exact)

### 2.1 `internal/models/task.go` — `OutputEntry` struct (L262)

Insert one field after `ArchRef`, before `DependsOn`:

```go
type OutputEntry struct {
    Desc      string   `yaml:"desc" json:"desc"`
    DoneWhen  string   `yaml:"done_when" json:"done_when"`
    Scope     string   `yaml:"scope" json:"scope"`
    SpecRef   string   `yaml:"spec_ref" json:"spec_ref"`
    PlanRef   string   `yaml:"plan_ref,omitempty" json:"plan_ref,omitempty"`
    ArchRef   string   `yaml:"arch_ref,omitempty" json:"arch_ref,omitempty"`
    Kind      string   `yaml:"kind,omitempty" json:"kind,omitempty"`
    DependsOn []string `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
}
```

Tag strings are character-identical to arch-plan §2.1. Empty string means "no marker"; `omitempty` on both YAML and JSON keeps existing fixtures byte-stable.

### 2.2 `internal/models/task.go` — `Task` struct (L187)

Insert one field immediately after `ArchRef` (L214), before `DoneWhen`:

```go
    SpecRef             string             `yaml:"spec_ref"`
    PlanRef             string             `yaml:"plan_ref,omitempty"`
    ArchRef             string             `yaml:"arch_ref,omitempty"`
    Kind                string             `yaml:"kind,omitempty"`
    DoneWhen            string             `yaml:"done_when"`
```

Tag string is character-identical to arch-plan §2.2. YAML-only (no JSON tag on `Task` — no existing field on `Task` has a JSON tag, so the new field follows suit). `omitempty` guarantees backward-compatible on-disk format: pre-existing YAML documents without `kind:` decode to `Kind == ""`, and empty-Kind tasks marshal without the key.

### 2.3 `internal/ops/proceed.go` — `buildChildTask` (L981)

Inside the returned `models.Task` literal (L994–L1010), add one line after `ArchRef:` (L1004), before `DoneWhen:` (L1005):

```go
    ArchRef:     paths.NormalizeSpecRef(archRef),
    Kind:        entry.Kind,
    DoneWhen:    entry.DoneWhen,
```

No other change to the function. `entry.Kind` is propagated verbatim — no normalization, no defaulting.

### 2.4 `internal/ops/replan.go` — `newTask` literal (L127)

Inside the `newTask := models.Task{…}` literal (L127–L145), add one line after `ArchRef:` (L138), before `DoneWhen:` (L139):

```go
    ArchRef:     task.ArchRef,
    Kind:        task.Kind,
    DoneWhen:    task.DoneWhen,
```

Rationale (verbatim from arch-plan §2.4): the rescope invariant requires SUPERSEDED + replacement to keep the bootstrap "in flight"; omitting this line would drop the replacement from the sibling task's `BootstrapInFlight` scan and proceed-time dedup, breaking the invariant.

### 2.5 Out-of-scope propagation paths (one-line justification)

`buildOneToOneChild` (`proceed.go:1016`) and `buildManyToOneChild` (`proceed.go:167`) do NOT receive `Kind: …` propagation because they synthesize children from parent-task metadata rather than consuming an `OutputEntry` — the `bootstrap-precommit` marker is authored exclusively via per-subtask `output[]` entries (arch-plan §2.3), so no one-to-one or many-to-one transition can ever produce a Kind-bearing child.

---

## 3. Tests

All five tests below are mandatory per the task's `done_when`. Assertions are enumerated verbatim from arch-plan §5.1 and §5.9.

### 3.1 `internal/models/task_test.go` (extend existing file)

The file already exists (16 KB). Append the four tests below at the end, following the existing table-driven style.

#### `TestOutputEntry_KindYAMLRoundTrip`

- Construct `OutputEntry{Desc: "x", DoneWhen: "y", Scope: "z", SpecRef: "s", Kind: "bootstrap-precommit"}`.
- Marshal via `gopkg.in/yaml.v3` (the package used elsewhere in this file).
- Unmarshal into a fresh `OutputEntry`.
- **Assert** decoded entry is `reflect.DeepEqual` to the original (including `Kind == "bootstrap-precommit"`).
- Marshal a second entry with `Kind: ""`; **assert** the serialized YAML contains no `kind:` key (`strings.Contains(string(out), "kind:") == false`). This is the `omitempty` guarantee.

#### `TestOutputEntry_KindJSONRoundTrip`

- Same two sub-cases as above, using `encoding/json` (Marshal / Unmarshal).
- **Assert** round-trip equality when `Kind: "bootstrap-precommit"`.
- **Assert** JSON output contains no `"kind"` key when `Kind: ""` (`strings.Contains(string(out), `"kind"`) == false`).
- This covers the `liza set-task-output` ingest path (CLI passes JSON payloads through to `OutputEntry`).

#### `TestTask_KindYAMLRoundTrip`

- Construct a minimal valid `Task` with `Kind: "bootstrap-precommit"` (populate required non-omitempty fields: `ID`, `Description`, `Status`, `Priority`, `SpecRef`, `DoneWhen`, `Scope`, `Created`, `History`).
- YAML-marshal then unmarshal.
- **Assert** decoded task's `Kind == "bootstrap-precommit"`.
- Construct a second task with `Kind: ""`; marshal; **assert** output does not contain `kind:`.

#### `TestTask_KindBackwardCompat`

- Define a YAML literal that represents a task written **before** this change — no `kind:` key present. Example inline string (populating the same minimal required fields).
- Unmarshal into `Task`.
- **Assert** `task.Kind == ""`.
- This is the "no migration required" guarantee from arch-plan §2.2: legacy state files load unchanged.

### 3.2 `internal/ops/replan_test.go` (extend existing file)

#### `TestReplan_PropagatesKind`

- Set up state with a single task eligible for replan, populated with `Kind: "bootstrap-precommit"` and any valid status for the replan entry-point (match an existing replan test's fixture shape).
- Call the replan path (mirror the invocation used by an existing `replan_test.go` test — e.g. `ReplanTask(...)` or the test helper already used in the file).
- **Assert** the old task is now `Status == SUPERSEDED`.
- **Assert** a new task exists with `Kind == "bootstrap-precommit"`.
- **Assert** the new task's `Supersedes` points at the old task's ID.

---

## 4. File survey & shared-file analysis

| File | This task | Sibling tasks | Coordination |
|------|-----------|---------------|--------------|
| `internal/models/task.go` | Yes (struct fields §2.1, §2.2) | No | — |
| `internal/models/task_test.go` | Yes (four round-trip tests) | No | — |
| `internal/ops/proceed.go` | Yes (single line `Kind: entry.Kind` in `buildChildTask` §2.3) | Sibling CP1 (`architecture-1-architecture-to-code-plan-1`) modifies `proceedInner` and `recoverCrashedTransition` and adds helpers | CP1 depends on this task (it reads `t.Kind` in `collectNonTerminalByKind`). Our edit is a one-line insertion in a different function — merge conflict risk is trivial but non-zero; enforcing `depends_on` from CP1 to this task sequences them. |
| `internal/ops/replan.go` | Yes (single line `Kind: task.Kind` in `newTask` §2.4) | No | — |
| `internal/ops/replan_test.go` | Yes (`TestReplan_PropagatesKind`) | No | — |

**Shared-file audit:** `proceed.go` is shared with CP1. CP1 already declares `depends_on: ["0"]` on this task per arch-plan §7.2 last line ("must land first"). The dependency is correctly recorded in the sibling task's existing blackboard entry — this plan introduces no new dependency edges.

---

## 5. Task decomposition

Single coding task. Rationale:

- **One intent**: "a `Kind` marker flows from output entry through to persisted task, including across replan." The struct additions, the two one-line propagations, and the five tests are the minimum coherent unit delivering that behavior — splitting the struct from its propagation or from the round-trip tests would land a dead field with no behavior, defeating the TDD colocation rule.
- **Single file-set of cohesive changes**: all edits are in five files that share one coherent intent. No independent concerns to split.
- **Atomic exception applies**: arch-plan §7.1 explicitly scopes this as one code-planning child ("exact field ordering, comment wording, any helper methods … and the test-file layout for §5.1 + §5.9").
- **Done-when is falsifiable by a concrete test set**: five named tests, each with enumerated assertions.

### Task 1 — Kind field: model delta + propagation + tests

- **desc**: Add `Kind string` field to `OutputEntry` (yaml:"kind,omitempty" json:"kind,omitempty", placed after ArchRef, before DependsOn) and to `Task` (yaml:"kind,omitempty", placed after ArchRef, before DoneWhen) in `internal/models/task.go`. Propagate `Kind: entry.Kind` in `buildChildTask` at `internal/ops/proceed.go:981` (single line between ArchRef and DoneWhen). Propagate `Kind: task.Kind` in the `newTask` literal at `internal/ops/replan.go:127` (single line between ArchRef and DoneWhen). Do NOT propagate in `buildOneToOneChild` or `buildManyToOneChild` (those paths never produce Kind-bearing children per arch-plan §2.3). Add four round-trip tests to `internal/models/task_test.go` per arch-plan §5.1: `TestOutputEntry_KindYAMLRoundTrip`, `TestOutputEntry_KindJSONRoundTrip`, `TestTask_KindYAMLRoundTrip`, `TestTask_KindBackwardCompat`. Add `TestReplan_PropagatesKind` to `internal/ops/replan_test.go` per arch-plan §5.9.

- **done_when**: All five named tests exist and pass: `go test ./internal/models -run 'TestOutputEntry_KindYAMLRoundTrip|TestOutputEntry_KindJSONRoundTrip|TestTask_KindYAMLRoundTrip|TestTask_KindBackwardCompat'` and `go test ./internal/ops -run 'TestReplan_PropagatesKind'` both exit 0. The `OutputEntry` struct in `internal/models/task.go` has a `Kind string` field with YAML tag `yaml:"kind,omitempty"` and JSON tag `json:"kind,omitempty"`, placed after `ArchRef`, before `DependsOn`. The `Task` struct has a `Kind string` field with YAML tag `yaml:"kind,omitempty"`, placed after `ArchRef`, before `DoneWhen`. `grep -n 'Kind:\s*entry.Kind' internal/ops/proceed.go` returns one line inside `buildChildTask`. `grep -n 'Kind:\s*task.Kind' internal/ops/replan.go` returns one line inside the `newTask` literal around L127. `grep -n 'Kind' internal/ops/proceed.go | grep -E 'buildOneToOneChild|buildManyToOneChild'` returns nothing (negative check that those paths were not modified). `go build ./...` exits 0 and full `go test ./...` exits 0 with no new failures vs pre-change baseline.

- **scope**: `internal/models/task.go` (two struct-field additions only — no other changes), `internal/models/task_test.go` (append four round-trip tests; may add imports for `encoding/json`, `reflect`, `strings`, `gopkg.in/yaml.v3` if not already imported), `internal/ops/proceed.go` (single-line addition `Kind: entry.Kind` inside `buildChildTask` — do NOT touch `buildOneToOneChild`, `buildManyToOneChild`, `proceedInner`, `recoverCrashedTransition`, `validateOutputEntry`, or add any helpers), `internal/ops/replan.go` (single-line addition `Kind: task.Kind` inside the `newTask` literal — do NOT touch retargeting, SUPERSEDED handling, or helpers), `internal/ops/replan_test.go` (append `TestReplan_PropagatesKind`). MUST NOT add the dedup helpers (`collectNonTerminalByKind`, `resolveKindDedup`), MUST NOT modify `proceedInner` or `recoverCrashedTransition`, MUST NOT add history `skipped_entries` bookkeeping — those are owned by sibling task `architecture-1-architecture-to-code-plan-1`.

- **spec_ref**: `specs/goals/20260417-precommit-bootstrap.md#q2-idempotency--plan-time-dedup-execution-time`

- **plan_ref**: `specs/plans/20260417-163926-architecture-1-architecture-to-code-plan-0.md`

- **depends_on**: none (this task must land first per arch-plan §7.1).

---

## 6. Spec Compliance Matrix

Every requirement extracted from the parent task's `done_when` (architecture-1-architecture-to-code-plan-0):

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Struct field addition on `OutputEntry` with exact tags `yaml:"kind,omitempty" json:"kind,omitempty"` matching arch-plan §2.1 | task `done_when` (1); arch-plan §2.1 | Task 1 | Covered |
| 2 | Struct field addition on `Task` with exact tag `yaml:"kind,omitempty"` matching arch-plan §2.2 | task `done_when` (1); arch-plan §2.2 | Task 1 | Covered |
| 3 | Both struct additions placed after `ArchRef` | task description; arch-plan §2.1, §2.2 | Task 1 | Covered |
| 4 | Propagation edit in `buildChildTask` (proceed.go:981) setting `Kind: entry.Kind` | task `done_when` (2); arch-plan §2.3 | Task 1 | Covered |
| 5 | Propagation edit in `replan.go` newTask (around L127) setting `Kind: task.Kind` | task `done_when` (3); arch-plan §2.4 | Task 1 | Covered |
| 6 | Explicit one-line justification why `buildOneToOneChild` and `buildManyToOneChild` do NOT receive propagation | task `done_when` (4); arch-plan §2.3 | Task 1 (this plan §2.5 supplies the justification; coder carries the same reasoning into a review-visible location — task does not require an in-code comment) | Covered |
| 7 | Unit test `TestOutputEntry_KindYAMLRoundTrip` covering YAML round-trip + empty-Kind omitempty | task `done_when` (5); arch-plan §5.1 | Task 1 | Covered |
| 8 | Unit test `TestOutputEntry_KindJSONRoundTrip` covering JSON round-trip + empty-Kind omitempty | task `done_when` (5); arch-plan §5.1 | Task 1 | Covered |
| 9 | Unit test `TestTask_KindYAMLRoundTrip` covering Task YAML round-trip + empty-Kind omitempty | task `done_when` (5); arch-plan §5.1 | Task 1 | Covered |
| 10 | Unit test `TestTask_KindBackwardCompat` covering pre-existing YAML without `kind:` decoding to empty | task `done_when` (5); arch-plan §5.1 | Task 1 | Covered |
| 11 | Unit test `TestReplan_PropagatesKind` covering replan propagates Kind (old SUPERSEDED; new has Kind; Supersedes points at old) | task `done_when` (5); arch-plan §5.9 | Task 1 | Covered |
| 12 | MUST NOT add dedup helpers or modify `proceedInner` / `recoverCrashedTransition` (scope exclusion) | task SCOPE | Task 1 (explicit scope exclusion) | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this task adds an inert data-model field + pure struct-to-struct propagation. The authoritative dedup behavior that makes `Kind` user-visible is owned by sibling CP1; e2e coverage belongs on that sibling, not here. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: the `kind` field's user-visible contract (ADR-0036 amendment, architect prompt doc) is owned by sibling tasks `architecture-4-architecture-to-code-plan-0` (ADR) and `architecture-3-architecture-to-code-plan-1` (architect prompt). This task only adds a behaviorally-inert struct field with no user-visible effect on its own. | N/A |
